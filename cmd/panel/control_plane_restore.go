package main

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	paneldb "github.com/alicelik/celikpanel/internal/db"
)

// controlPlaneRestoreResult is the summary the caller prints.
type controlPlaneRestoreResult struct {
	Restored int
	// Kept counts the members this restore deliberately did not place because
	// the installer's copy won (see the precedence table below).
	Kept             int
	SchemaVersion    int
	MigrationVersion int
	Source           string
}

// controlPlaneFreshHostMarkers are the files whose presence means this host
// already has a control plane of its own. Restore is a fresh-host operation and
// nothing else, so there is never a merge and never a second identity
// (docs/DISASTER-RECOVERY.md section 5).
var controlPlaneFreshHostMarkers = []string{
	"service-mutations.json",
	"dns-engine-state.json",
}

// ---------------------------------------------------------------------------
// Precedence: who wins when the installer has already written a member
// ---------------------------------------------------------------------------
//
// docs/DISASTER-RECOVERY.md section 8 left this open for slice 2, and slice 1
// simply renamed the archived copy over whatever was there. That is wrong for
// exactly the members the installer writes from THIS host's answers, and right
// for the one member that is itself the operator's rule set. The table below is
// the whole rule; there is no second place where a member name is special.
//
// docs/DISASTER-RECOVERY.md 8. bölümü bu kararı dilim 2'ye bırakmıştı. Aşağıdaki
// tablo kuralın tamamıdır; bir üye adının özel işlendiği ikinci bir yer yoktur.

type controlPlaneRestorePrecedence int

const (
	// controlPlaneArchiveWins renames the archived copy over whatever the
	// installer left, because the archived file is the only record of what the
	// operator decided.
	controlPlaneArchiveWins controlPlaneRestorePrecedence = iota
	// controlPlaneInstallerWins keeps the file this installation just wrote and
	// says so once, because that file describes THIS host while the archived one
	// describes the host that died.
	controlPlaneInstallerWins
)

// controlPlaneMemberPolicy is one row of the precedence table.
type controlPlaneMemberPolicy struct {
	Basename string
	// Root selects the control-plane root the member lives under, so the rule
	// follows an installation that has moved that root by environment.
	Root       func(controlPlaneRoots) string
	Precedence controlPlaneRestorePrecedence
	// CompareKeys asks for a key-by-key report when the installer's file is
	// kept: one line per key whose value differs from the archived copy. Only
	// the KEY NAME is ever printed; see controlPlaneConfigurationDifferences.
	CompareKeys bool
	// KeptLine is the single sentence printed when the installer's file is kept
	// and no key-by-key report applies.
	KeptLine string
	// RestoredLine is the sentence printed when the archived copy is placed for
	// a member whose restoration the operator has to see in the summary.
	RestoredLine string
}

// controlPlaneMemberPolicies answers, per member, from what the product
// actually does with the file:
//
//   - panel.env: the INSTALLER wins. install.sh writes it from this host's own
//     answers (CELIKPANEL_LISTEN, CELIKPANEL_TLS and the TLS paths) and a listen
//     address inherited from the dead host would be wrong the moment it differs.
//     The archived copy is still opened, so every key that differs is named once
//     and the operator can reconcile it deliberately.
//   - agent.token: the INSTALLER wins. Nothing durable references the token: it
//     is a shared secret both units read from disk (internal/transport/socket.go,
//     LoadOrCreateToken and ReadToken), never a database row and never part of
//     the agent private state. In the install hook the restore runs before the
//     agent has ever started, so the file is normally absent and the archived
//     token is simply placed.
//   - firewall.nft: the ARCHIVE wins. The agent never regenerates this file from
//     panel database state. It is written only by an explicit firewall apply
//     (cmd/agent/firewall_rpc.go) and read back at boot by
//     celikpanel-firewall-restore.service; the file IS the operator's ruleset,
//     so a fresh install must not be allowed to leave a restored host disarmed.
func controlPlaneMemberPolicies() []controlPlaneMemberPolicy {
	confDir := func(roots controlPlaneRoots) string { return roots.ConfDir }
	return []controlPlaneMemberPolicy{
		{
			Basename:   "panel.env",
			Root:       confDir,
			Precedence: controlPlaneInstallerWins,
			// Without a sentence of its own, a panel.env whose keys happen to
			// match the archive was counted in kept=1 and named nowhere: the
			// summary reported that something had been kept and left the
			// operator to guess what. The key-by-key report is an addition to
			// this line, not a substitute for it — it says nothing at all when
			// nothing differs.
			//
			// Kendine ait bir cümlesi olmadığında, anahtarları arşivle
			// örtüşen bir panel.env kept=1 içinde sayılıp hiçbir yerde
			// adlandırılmıyordu: özet bir şeyin korunduğunu bildirip neyin
			// korunduğunu operatörün tahminine bırakıyordu. Anahtar anahtar
			// rapor bu satırın yerine değil ekidir — hiçbir şey farklı
			// değilken hiçbir şey söylemez.
			KeptLine:    "panel.env: the installer's configuration is kept",
			CompareKeys: true,
		},
		{
			Basename:   "agent.token",
			Root:       confDir,
			Precedence: controlPlaneInstallerWins,
			KeptLine:   "agent.token: the installer's token is kept",
		},
		{
			Basename:     "firewall.nft",
			Root:         confDir,
			Precedence:   controlPlaneArchiveWins,
			RestoredLine: "firewall.nft: restored from the archive",
		},
	}
}

// controlPlaneMemberPolicyFor returns the row that governs one resolved target
// path, or false when the member is governed by nothing but slice-1 behaviour.
func controlPlaneMemberPolicyFor(
	targetPath string,
	target controlPlaneRoots,
) (controlPlaneMemberPolicy, bool) {
	cleaned := filepath.Clean(targetPath)
	for _, policy := range controlPlaneMemberPolicies() {
		root := strings.TrimSpace(policy.Root(target))
		if root == "" {
			continue
		}
		if cleaned == filepath.Join(filepath.Clean(root), policy.Basename) {
			return policy, true
		}
	}
	return controlPlaneMemberPolicy{}, false
}

// controlPlaneConfigurationMaxBytes bounds a configuration file before it is
// parsed for the key-by-key report. panel.env is a handful of short lines.
const controlPlaneConfigurationMaxBytes int64 = 64 * 1024

// parseControlPlaneConfigurationFile reads a KEY=VALUE file exactly the way
// install.sh validates panel.env: one assignment per line, blank and comment
// lines ignored, the first "=" separating the name from the value. It is
// deliberately not a shell parser; panel.env is data, never shell code.
func parseControlPlaneConfigurationFile(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, controlPlaneConfigurationMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	if int64(len(content)) > controlPlaneConfigurationMaxBytes {
		return nil, fmt.Errorf("%s is too large to compare with the archived copy", path)
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		name, value, found := strings.Cut(line, "=")
		name = strings.TrimSpace(name)
		if !found || name == "" {
			continue
		}
		values[name] = value
	}
	return values, nil
}

// controlPlaneConfigurationDifferences names every key whose value differs
// between the file this installation wrote and the archived copy, including a
// key that only one of the two carries: an absent key is a difference the
// operator has to know about.
//
// It returns NAMES ONLY. No value of any key, sensitive or not, can reach the
// report, because the warning line has nowhere to put one. That is stronger
// than filtering key names for SECRET, TOKEN, KEY or PASSWORD, and unlike such
// a filter it cannot rot as keys are added.
//
// Yalnızca ANAHTAR ADLARI döner; hiçbir anahtarın değeri rapora ulaşamaz.
func controlPlaneConfigurationDifferences(installer, archived map[string]string) []string {
	names := map[string]struct{}{}
	for name := range installer {
		names[name] = struct{}{}
	}
	for name := range archived {
		names[name] = struct{}{}
	}
	differing := make([]string, 0, len(names))
	for name := range names {
		installerValue, installerHas := installer[name]
		archivedValue, archivedHas := archived[name]
		if installerHas && archivedHas && installerValue == archivedValue {
			continue
		}
		differing = append(differing, name)
	}
	sort.Strings(differing)
	return differing
}

// reportControlPlaneKeptMember prints why the installer's file was kept. It is
// called only when the target file exists and its policy says so.
func reportControlPlaneKeptMember(
	policy controlPlaneMemberPolicy,
	placement controlPlanePlacement,
	report io.Writer,
) error {
	if strings.TrimSpace(policy.KeptLine) != "" {
		fmt.Fprintln(report, policy.KeptLine)
	}
	if !policy.CompareKeys {
		return nil
	}
	installer, err := parseControlPlaneConfigurationFile(placement.TargetPath)
	if err != nil {
		return err
	}
	archived, err := parseControlPlaneConfigurationFile(placement.StagedPath)
	if err != nil {
		return err
	}
	for _, name := range controlPlaneConfigurationDifferences(installer, archived) {
		fmt.Fprintf(
			report,
			"%s: %s differs from the archive; the installer's value is kept\n",
			policy.Basename,
			name,
		)
	}
	return nil
}

// restoreControlPlaneArchive places one archive onto a fresh host. Nothing is
// written to the target tree until the last ciphertext chunk has authenticated,
// every member digest has matched and every recorded account has resolved.
//
// restoreControlPlaneArchive bir arşivi temiz bir makineye yerleştirir. Son
// şifreli parça doğrulanmadan, her üye özeti eşleşmeden ve kayıtlı her hesap
// çözülmeden hedef ağaca hiçbir şey yazılmaz.
func restoreControlPlaneArchive(
	sourcePath string,
	keyText string,
	target controlPlaneRoots,
	report io.Writer,
) (controlPlaneRestoreResult, error) {
	key, err := parseControlPlaneKey(keyText)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	defer zeroControlPlaneKey(key)

	if !filepath.IsAbs(sourcePath) {
		return controlPlaneRestoreResult{}, errors.New("the archive path must be absolute")
	}
	sourcePath = filepath.Clean(sourcePath)
	if err := requireFreshControlPlaneHost(target); err != nil {
		return controlPlaneRestoreResult{}, err
	}

	file, err := os.Open(sourcePath)
	if err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("read %s: %w", sourcePath, err)
	}
	defer file.Close()

	header, preamble, err := readControlPlaneArchivePreamble(file)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	aead, err := newControlPlaneArchiveAEAD(key, header)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}

	// The staging area sits beside the archive, which is already a root-only
	// path, so plaintext never lands in a shared temporary directory.
	stagingDirectory, err := os.MkdirTemp(
		filepath.Dir(sourcePath),
		".celikpanel-control-plane-restore-",
	)
	if err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("create the restore staging directory: %w", err)
	}
	defer os.RemoveAll(stagingDirectory)
	if err := os.Chmod(stagingDirectory, 0o700); err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("secure the restore staging directory: %w", err)
	}

	stream := newControlPlaneStreamReader(file, aead, preamble, header.Chunk)
	manifest, staged, err := extractControlPlaneArchive(stream, stagingDirectory)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	if err := verifyControlPlaneManifest(manifest, staged); err != nil {
		return controlPlaneRestoreResult{}, err
	}
	placements, err := planControlPlanePlacement(manifest, staged, target)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	restored, kept, err := placeControlPlaneMembers(placements, report)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}

	restoredDatabase := filepath.Join(target.DataDir, controlPlaneDatabaseBasename)
	if err := validateServiceOperationSnapshotSchema(
		restoredDatabase,
		manifest.SchemaVersion,
		false,
	); err != nil {
		return controlPlaneRestoreResult{}, fmt.Errorf("verify the restored panel database: %w", err)
	}
	cleared, err := clearRestoredServiceScanCache(restoredDatabase)
	if err != nil {
		return controlPlaneRestoreResult{}, err
	}
	if cleared {
		fmt.Fprintln(
			report,
			"component state: the archived scan describes the host that died and was discarded",
		)
	}

	result := controlPlaneRestoreResult{
		Restored:         restored,
		Kept:             kept,
		SchemaVersion:    manifest.SchemaVersion,
		MigrationVersion: manifest.DatabaseMigrationVersion,
		Source:           sourcePath,
	}
	fmt.Fprintf(
		report,
		"restored=%d schema=%d migration=%d from=%s kept=%d\n",
		result.Restored,
		result.SchemaVersion,
		result.MigrationVersion,
		result.Source,
		result.Kept,
	)
	return result, nil
}

func requireFreshControlPlaneHost(target controlPlaneRoots) error {
	databasePath := filepath.Join(target.DataDir, controlPlaneDatabaseBasename)
	if _, err := os.Lstat(databasePath); err == nil {
		return fmt.Errorf(
			"this host already has a panel database at %s; a control-plane archive is restored onto a fresh host only",
			databasePath,
		)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("inspect %s: %w", databasePath, err)
	}
	for _, marker := range controlPlaneFreshHostMarkers {
		markerPath := filepath.Join(target.AgentStateDir, marker)
		if _, err := os.Lstat(markerPath); err == nil {
			return fmt.Errorf(
				"this host already has agent state at %s; a control-plane archive is restored onto a fresh host only",
				markerPath,
			)
		} else if !os.IsNotExist(err) {
			return fmt.Errorf("inspect %s: %w", markerPath, err)
		}
	}
	return nil
}

// extractControlPlaneArchive decrypts the whole stream into the staging area.
// It reads the manifest from the first entry and refuses an archive whose
// durable schema contract is newer than this binary before it stores a single
// member, so an archive from a future release costs nothing but a temporary
// directory.
func extractControlPlaneArchive(
	stream io.Reader,
	stagingDirectory string,
) (controlPlaneManifest, map[string]string, error) {
	reader := tar.NewReader(stream)
	var manifest controlPlaneManifest
	manifestSeen := false
	manifestDigest := ""
	var manifestJSON []byte
	staged := map[string]string{}

	for {
		header, err := reader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			if errors.Is(err, errControlPlaneArchiveAuthentication) {
				return controlPlaneManifest{}, nil, errControlPlaneArchiveAuthentication
			}
			return controlPlaneManifest{}, nil, fmt.Errorf("read the archive: %w", err)
		}
		switch header.Name {
		case controlPlaneManifestName:
			if manifestSeen {
				return controlPlaneManifest{}, nil, errors.New("the archive carries more than one manifest")
			}
			manifestJSON, err = io.ReadAll(io.LimitReader(reader, controlPlaneArchiveMaxManifestBytes+1))
			if err != nil {
				return controlPlaneManifest{}, nil, fmt.Errorf("read the archive manifest: %w", err)
			}
			if int64(len(manifestJSON)) > controlPlaneArchiveMaxManifestBytes {
				return controlPlaneManifest{}, nil, errors.New("the archive manifest is too large")
			}
			if err := json.Unmarshal(manifestJSON, &manifest); err != nil {
				return controlPlaneManifest{}, nil, fmt.Errorf("decode the archive manifest: %w", err)
			}
			if manifest.SchemaVersion > durableServiceOperationSchemaVersion {
				return controlPlaneManifest{}, nil, fmt.Errorf(
					"the archive was written for schema version %d and this panel understands %d; install the matching release or a newer one before restoring",
					manifest.SchemaVersion,
					durableServiceOperationSchemaVersion,
				)
			}
			if err := requireRestorableControlPlaneMigrationVersion(
				manifest.DatabaseMigrationVersion,
			); err != nil {
				return controlPlaneManifest{}, nil, err
			}
			manifestSeen = true
		case controlPlaneManifestDigestName:
			raw, err := io.ReadAll(io.LimitReader(reader, 128))
			if err != nil {
				return controlPlaneManifest{}, nil, fmt.Errorf("read the archive manifest digest: %w", err)
			}
			manifestDigest = strings.TrimSpace(string(raw))
		default:
			if !manifestSeen {
				return controlPlaneManifest{}, nil, errors.New("the archive does not start with its manifest")
			}
			if err := validateControlPlaneMemberName(header.Name); err != nil {
				return controlPlaneManifest{}, nil, err
			}
			if header.Typeflag == tar.TypeDir {
				continue
			}
			if header.Typeflag != tar.TypeReg {
				return controlPlaneManifest{}, nil, fmt.Errorf(
					"the archive entry %s is not a regular file",
					header.Name,
				)
			}
			stagedPath, err := stageControlPlaneArchiveEntry(
				stagingDirectory,
				header.Name,
				reader,
			)
			if err != nil {
				return controlPlaneManifest{}, nil, err
			}
			staged[header.Name] = stagedPath
		}
	}
	if !manifestSeen {
		return controlPlaneManifest{}, nil, errors.New("the archive has no manifest")
	}
	digest := sha256.Sum256(manifestJSON)
	if manifestDigest != hex.EncodeToString(digest[:]) {
		return controlPlaneManifest{}, nil, errors.New("the archive manifest does not match its recorded digest")
	}
	return manifest, staged, nil
}

// controlPlaneArchiveMaxManifestBytes bounds a hostile manifest. A real one
// lists a few dozen paths.
const controlPlaneArchiveMaxManifestBytes int64 = 8 * 1024 * 1024

// requireRestorableControlPlaneMigrationVersion refuses a database that has
// been migrated further than this release can run, BEFORE a single member is
// staged or placed. Without it the database would land and only fail later, or
// worse, be opened by an older panel that cannot read its own schema. The
// ceiling is read from the migrations this binary actually ships, never from a
// number written down here.
// requireRestorableControlPlaneMigrationVersion, bu yayının çalıştırabileceğinden
// daha ileri taşınmış bir veritabanını, hiçbir üye yerleştirilmeden reddeder.
func requireRestorableControlPlaneMigrationVersion(recorded int) error {
	shipped, err := paneldb.HighestEmbeddedMigrationVersion()
	if err != nil {
		return fmt.Errorf("read the migrations this release ships: %w", err)
	}
	if recorded < 1 {
		return errors.New(
			"the archive manifest records no database migration version; it was not written by this product",
		)
	}
	if recorded > shipped {
		return fmt.Errorf(
			"the archived database is migrated to version %d and this panel ships migrations up to %d; install that release or a newer one before restoring",
			recorded,
			shipped,
		)
	}
	return nil
}

func stageControlPlaneArchiveEntry(
	stagingDirectory string,
	name string,
	content io.Reader,
) (string, error) {
	stagedPath := filepath.Join(stagingDirectory, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(stagedPath), 0o700); err != nil {
		return "", fmt.Errorf("prepare the restore staging directory: %w", err)
	}
	file, err := os.OpenFile(
		stagedPath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|controlPlaneOpenNoFollow,
		0o600,
	)
	if err != nil {
		return "", fmt.Errorf("stage the archive entry %s: %w", name, err)
	}
	defer file.Close()
	if _, err := io.Copy(file, content); err != nil {
		if errors.Is(err, errControlPlaneArchiveAuthentication) {
			return "", errControlPlaneArchiveAuthentication
		}
		return "", fmt.Errorf("stage the archive entry %s: %w", name, err)
	}
	return stagedPath, nil
}

func verifyControlPlaneManifest(
	manifest controlPlaneManifest,
	staged map[string]string,
) error {
	if len(manifest.Members) == 0 {
		return errors.New("the archive manifest lists no members")
	}
	expected := map[string]struct{}{}
	for _, entry := range manifest.Members {
		name, err := controlPlaneMemberName(entry.Path)
		if err != nil {
			return err
		}
		if entry.Type == controlPlaneManifestEntryDirectory {
			continue
		}
		if entry.Type != controlPlaneManifestEntryFile {
			return fmt.Errorf("the archive manifest has an unknown member type %q", entry.Type)
		}
		expected[name] = struct{}{}
		stagedPath, ok := staged[name]
		if !ok {
			return fmt.Errorf("the archive manifest lists %s but the archive does not carry it", entry.Path)
		}
		digest, size, err := digestControlPlaneFile(stagedPath)
		if err != nil {
			return err
		}
		if size != entry.Size || digest != entry.SHA256 {
			return fmt.Errorf("the archived copy of %s does not match its recorded digest", entry.Path)
		}
	}
	for name := range staged {
		if _, ok := expected[name]; !ok {
			return fmt.Errorf("the archive carries %s, which its manifest does not list", name)
		}
	}
	return nil
}

// controlPlanePlacement is one resolved write: where the bytes come from, where
// they go, and with which owner and mode.
type controlPlanePlacement struct {
	TargetPath string
	StagedPath string
	Directory  bool
	Mode       os.FileMode
	UID        int
	GID        int
	// Policy is the precedence row governing this member, if any. It is
	// resolved while the target roots are in hand, so the placement loop never
	// has to know a member's name.
	Policy    controlPlaneMemberPolicy
	HasPolicy bool
}

// planControlPlanePlacement rebases every recorded path from the roots the
// archive was taken under onto the roots this host uses, and resolves every
// recorded account. It returns only when every member can be placed.
func planControlPlanePlacement(
	manifest controlPlaneManifest,
	staged map[string]string,
	target controlPlaneRoots,
) ([]controlPlanePlacement, error) {
	rebase, err := newControlPlaneRebase(manifest.Roots, target)
	if err != nil {
		return nil, err
	}
	placements := make([]controlPlanePlacement, 0, len(manifest.Members))
	for _, entry := range manifest.Members {
		targetPath, err := rebase(entry.Path)
		if err != nil {
			return nil, err
		}
		mode, err := parseControlPlaneMode(entry.Mode)
		if err != nil {
			return nil, err
		}
		uid, gid, err := controlPlaneResolveOwnership(entry.Owner, entry.Group)
		if err != nil {
			return nil, err
		}
		placement := controlPlanePlacement{
			TargetPath: targetPath,
			Directory:  entry.Type == controlPlaneManifestEntryDirectory,
			Mode:       mode,
			UID:        uid,
			GID:        gid,
		}
		if !placement.Directory {
			name, err := controlPlaneMemberName(entry.Path)
			if err != nil {
				return nil, err
			}
			placement.StagedPath = staged[name]
			placement.Policy, placement.HasPolicy = controlPlaneMemberPolicyFor(targetPath, target)
		}
		placements = append(placements, placement)
	}
	// Directories first, shallowest first, so every parent exists with its
	// recorded owner and mode before a file lands in it.
	sort.SliceStable(placements, func(left, right int) bool {
		if placements[left].Directory != placements[right].Directory {
			return placements[left].Directory
		}
		return placements[left].TargetPath < placements[right].TargetPath
	})
	return placements, nil
}

// newControlPlaneRebase maps a recorded absolute path onto this host by the
// root it belongs to. When the archive was taken with the same layout every
// mapping is the identity.
func newControlPlaneRebase(
	source controlPlaneRoots,
	target controlPlaneRoots,
) (func(string) (string, error), error) {
	pairs := [][2]string{
		{source.DataDir, target.DataDir},
		{source.ConfDir, target.ConfDir},
		{source.AgentStateDir, target.AgentStateDir},
		{source.DKIMDir, target.DKIMDir},
		{source.WireGuardDir, target.WireGuardDir},
		{source.TLSDir, target.TLSDir},
	}
	cleaned := make([][2]string, 0, len(pairs))
	for _, pair := range pairs {
		if strings.TrimSpace(pair[0]) == "" || strings.TrimSpace(pair[1]) == "" {
			return nil, errors.New("the archive manifest does not record every control-plane root")
		}
		cleaned = append(cleaned, [2]string{filepath.Clean(pair[0]), filepath.Clean(pair[1])})
	}
	// Longest recorded root first: the TLS directory is normally inside the
	// data directory, and the more specific root has to win.
	sort.SliceStable(cleaned, func(left, right int) bool {
		return len(cleaned[left][0]) > len(cleaned[right][0])
	})
	return func(recorded string) (string, error) {
		if _, err := controlPlaneMemberName(recorded); err != nil {
			return "", err
		}
		candidate := filepath.Clean(recorded)
		for _, pair := range cleaned {
			if candidate == pair[0] {
				return pair[1], nil
			}
			prefix := pair[0] + string(os.PathSeparator)
			if strings.HasPrefix(candidate, prefix) {
				placed := filepath.Join(pair[1], strings.TrimPrefix(candidate, prefix))
				if err := requireControlPlaneContainment(pair[1], placed); err != nil {
					return "", err
				}
				return placed, nil
			}
		}
		return "", fmt.Errorf(
			"the archive records %s, which lies outside every control-plane root it names",
			recorded,
		)
	}, nil
}

func requireControlPlaneContainment(root string, candidate string) error {
	relative, err := filepath.Rel(filepath.Clean(root), filepath.Clean(candidate))
	if err != nil {
		return fmt.Errorf("place %s below %s: %w", candidate, root, err)
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(os.PathSeparator)) {
		return fmt.Errorf("the archive tried to place %s outside %s", candidate, root)
	}
	return nil
}

func placeControlPlaneMembers(
	placements []controlPlanePlacement,
	report io.Writer,
) (int, int, error) {
	restored := 0
	kept := 0
	for _, placement := range placements {
		if placement.Directory {
			if err := placeControlPlaneDirectory(placement); err != nil {
				return 0, 0, err
			}
			continue
		}
		place, err := decideControlPlanePlacement(placement, report)
		if err != nil {
			return 0, 0, err
		}
		if !place {
			kept++
			continue
		}
		if err := placeControlPlaneFile(placement); err != nil {
			return 0, 0, err
		}
		restored++
		fmt.Fprintf(
			report,
			"placed path=%s mode=%s\n",
			placement.TargetPath,
			formatControlPlaneMode(placement.Mode),
		)
		if placement.HasPolicy && strings.TrimSpace(placement.Policy.RestoredLine) != "" {
			fmt.Fprintln(report, placement.Policy.RestoredLine)
		}
	}
	return restored, kept, nil
}

// decideControlPlanePlacement applies one row of the precedence table to one
// member. A member with no row, or a member whose target does not exist yet, is
// always placed: the fresh-host case is unchanged from slice 1.
//
// decideControlPlanePlacement, öncelik tablosunun bir satırını tek bir üyeye
// uygular. Satırı olmayan ya da hedefi henüz var olmayan üye her zaman
// yerleştirilir.
func decideControlPlanePlacement(
	placement controlPlanePlacement,
	report io.Writer,
) (bool, error) {
	if !placement.HasPolicy {
		return true, nil
	}
	info, err := os.Lstat(placement.TargetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return true, nil
		}
		return false, fmt.Errorf("inspect %s: %w", placement.TargetPath, err)
	}
	if placement.Policy.Precedence == controlPlaneArchiveWins {
		return true, nil
	}
	// The installer wins, but only over a file. Anything else where a
	// control-plane member belongs is a redirection attempt, not a decision
	// this installation made, and it is never quietly honoured.
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf(
			"%s already exists and is not a regular file; restore refuses to reason about it",
			placement.TargetPath,
		)
	}
	return false, reportControlPlaneKeptMember(placement.Policy, placement, report)
}

func placeControlPlaneDirectory(placement controlPlanePlacement) error {
	if err := os.MkdirAll(placement.TargetPath, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", placement.TargetPath, err)
	}
	if err := controlPlaneApplyDirectoryMetadata(
		placement.TargetPath,
		placement.Mode,
		placement.UID,
		placement.GID,
	); err != nil {
		return err
	}
	return controlPlaneSyncDirectory(placement.TargetPath)
}

// placeControlPlaneFile writes into a temporary name in the final directory,
// gives it its recorded owner and mode while it is still private, flushes it,
// and only then renames it into place.
func placeControlPlaneFile(placement controlPlanePlacement) (returnErr error) {
	directory := filepath.Dir(placement.TargetPath)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", directory, err)
	}
	randomBytes := make([]byte, 16)
	if _, err := rand.Read(randomBytes); err != nil {
		return fmt.Errorf("name the restore staging file: %w", err)
	}
	stagePath := filepath.Join(
		directory,
		"."+filepath.Base(placement.TargetPath)+".restoring-"+hex.EncodeToString(randomBytes),
	)
	file, err := os.OpenFile(
		stagePath,
		os.O_WRONLY|os.O_CREATE|os.O_EXCL|controlPlaneOpenNoFollow,
		0o600,
	)
	if err != nil {
		return fmt.Errorf("create the restore staging file for %s: %w", placement.TargetPath, err)
	}
	published := false
	defer func() {
		if !published {
			if err := os.Remove(stagePath); err != nil && !os.IsNotExist(err) {
				returnErr = errors.Join(returnErr, err)
			}
		}
	}()

	if err := func() error {
		defer file.Close()
		if err := controlPlaneApplyFileMetadata(
			file,
			placement.Mode,
			placement.UID,
			placement.GID,
		); err != nil {
			return err
		}
		source, err := os.Open(placement.StagedPath)
		if err != nil {
			return fmt.Errorf("read the staged copy of %s: %w", placement.TargetPath, err)
		}
		defer source.Close()
		if _, err := io.Copy(file, source); err != nil {
			return fmt.Errorf("write %s: %w", placement.TargetPath, err)
		}
		if err := file.Sync(); err != nil {
			return fmt.Errorf("flush %s: %w", placement.TargetPath, err)
		}
		return nil
	}(); err != nil {
		return err
	}
	if err := os.Rename(stagePath, placement.TargetPath); err != nil {
		return fmt.Errorf("publish %s: %w", placement.TargetPath, err)
	}
	published = true
	if err := controlPlaneFinalizeFileMode(placement.TargetPath, placement.Mode); err != nil {
		return err
	}
	return controlPlaneSyncDirectory(directory)
}

// clearRestoredServiceScanCache discards the archived component scan.
//
// Every other row in the archive is durable authority: what the operator
// decided, and what the panel therefore intends. The scan cache is the exact
// opposite — an observation of ONE machine's installed packages and running
// units at one instant, and after a restore that machine is the one that died.
// Left in place it was served as fact: on the drill's fresh host the components
// screen reported BIND "active (running)" with the dead host's scan time, while
// no BIND existed at all. The panel cannot make a true statement about this
// host's packages until it has looked at this host, and until then the honest
// answer is that it has not looked.
//
// The fix is a deletion rather than a filter in the reader. A reader-side rule
// would have to decide, on every request forever, whether the row it is holding
// came from this machine or the last one — a question the row cannot answer
// once the panel restarts and its boot time is no longer a usable boundary.
// Deleting it at restore leaves nothing to be wrong about: the screen falls
// through to its ordinary "no scan yet" path and the first scan on this host
// fills it in.
//
// clearRestoredServiceScanCache, arşivlenmiş bileşen taramasını atar.
//
// Arşivdeki diğer her satır kalıcı yetkidir: operatörün kararı ve dolayısıyla
// panelin niyeti. Tarama önbelleği ise tam tersidir — TEK bir makinenin bir
// andaki kurulu paketlerinin ve çalışan birimlerinin gözlemi; geri yüklemeden
// sonra o makine ölmüş olandır. Yerinde bırakıldığında gerçek diye sunuluyordu:
// tatbikatın taze sunucusunda bileşenler ekranı, hiç BIND yokken, ölü sunucunun
// tarama zamanıyla BIND'i "etkin (çalışıyor)" bildiriyordu. Panel, bu sunucuya
// bakmadan onun paketleri hakkında doğru bir cümle kuramaz; o ana kadar dürüst
// cevap, bakmamış olduğudur.
//
// Çözüm, okuyucu tarafında bir süzgeç değil bir silmedir. Okuyucu tarafındaki
// bir kural, sonsuza dek her istekte, elindeki satırın bu makineden mi yoksa
// öncekinden mi geldiğine karar vermek zorunda kalırdı — panel yeniden
// başlayıp açılış zamanı kullanılabilir bir sınır olmaktan çıktığında satırın
// cevaplayamayacağı bir soru. Geri yüklemede silmek, yanlış olunacak bir şey
// bırakmaz: ekran olağan "henüz tarama yok" yoluna düşer ve bu sunucudaki ilk
// tarama onu doldurur.
func clearRestoredServiceScanCache(databasePath string) (bool, error) {
	database, err := sql.Open("sqlite", sqliteSnapshotURI(databasePath, false))
	if err != nil {
		return false, fmt.Errorf("open the restored panel database: %w", err)
	}
	defer database.Close()
	database.SetMaxOpenConns(1)
	database.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := database.ExecContext(ctx, `DELETE FROM service_scan_cache`)
	if err != nil {
		return false, fmt.Errorf(
			"discard the archived component scan: %w", err,
		)
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return false, fmt.Errorf(
			"discard the archived component scan: %w", err,
		)
	}
	var remaining int
	if err := database.QueryRowContext(
		ctx, `SELECT count(*) FROM service_scan_cache`,
	).Scan(&remaining); err != nil {
		return false, fmt.Errorf(
			"verify the discarded component scan: %w", err,
		)
	}
	if remaining != 0 {
		return false, errors.New(
			"the archived component scan survived its removal",
		)
	}
	return removed > 0, nil
}
