package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

// controlPlaneRoots names the six directories the control-plane inventory is
// read from. Production fills it from the panel configuration; tests point every
// field at one temporary tree, which is why each root is a field rather than a
// call to a path helper buried inside the walk.
//
// controlPlaneRoots, kontrol düzlemi envanterinin okunduğu altı dizini
// adlandırır. Üretim bunları panel yapılandırmasından doldurur; testler her
// alanı tek bir geçici ağaca yöneltir.
type controlPlaneRoots struct {
	DataDir       string `json:"data_dir"`
	ConfDir       string `json:"conf_dir"`
	AgentStateDir string `json:"agent_state_dir"`
	DKIMDir       string `json:"dkim_dir"`
	WireGuardDir  string `json:"wireguard_dir"`
	// TLSDir is a field of its own because CELIKPANEL_TLS_DIR already lets an
	// installation move the panel certificate away from the data directory.
	TLSDir string `json:"tls_dir"`
}

const (
	controlPlaneConfDirDefault      = "/etc/celikpanel"
	controlPlaneDKIMDirDefault      = "/var/lib/celikpanel-dkim"
	controlPlaneWireGuardDirDefault = "/etc/wireguard"
	// controlPlaneAgentStateSnapshotsDir is excluded on purpose: it is the
	// transient five-minute system SQLite snapshot area, not durable state
	// (docs/DISASTER-RECOVERY.md section 2).
	controlPlaneAgentStateSnapshotsDir = "system-sqlite-snapshots"
	controlPlaneDatabaseBasename       = "celikpanel.db"
	controlPlaneSecretKeyBasename      = "secret.key"
)

// productionControlPlaneRoots resolves the live layout.
//
// Every root can be moved by environment ONLY, never by a request or a flag, so
// that a hostile caller can never redirect the inventory:
//
//   - CELIKPANEL_DATA_DIR      the SQLite database and secret.key (dataDir)
//   - CELIKPANEL_TLS_DIR       the panel certificate and key (tlsDir)
//   - CELIKPANEL_AGENT_STATE_DIR  the agent private state, else hostingpath
//   - CELIKPANEL_CONF_DIR      panel.env, agent.token, firewall.nft
//   - CELIKPANEL_DKIM_DIR      the DKIM key tree (keys/ below it)
//   - CELIKPANEL_WIREGUARD_DIR the VPN identity and peers
func productionControlPlaneRoots() controlPlaneRoots {
	return controlPlaneRoots{
		DataDir:       dataDir(),
		ConfDir:       controlPlaneEnvironmentRoot("CELIKPANEL_CONF_DIR", controlPlaneConfDirDefault),
		AgentStateDir: controlPlaneEnvironmentRoot("CELIKPANEL_AGENT_STATE_DIR", hostingpath.ServiceMutationStateRoot()),
		DKIMDir:       controlPlaneEnvironmentRoot("CELIKPANEL_DKIM_DIR", controlPlaneDKIMDirDefault),
		WireGuardDir:  controlPlaneEnvironmentRoot("CELIKPANEL_WIREGUARD_DIR", controlPlaneWireGuardDirDefault),
		TLSDir:        tlsDir(),
	}
}

func controlPlaneEnvironmentRoot(variable string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(variable)); value != "" {
		return value
	}
	return fallback
}

type controlPlaneMemberKind int

const (
	// controlPlaneMemberDatabase is the one member that is never copied with a
	// file read: it goes through the online backup primitive.
	controlPlaneMemberDatabase controlPlaneMemberKind = iota
	// controlPlaneMemberFile is one fixed absolute path.
	controlPlaneMemberFile
	// controlPlaneMemberFlatScan is every regular file directly under a
	// directory, without descending.
	controlPlaneMemberFlatScan
	// controlPlaneMemberTreeScan is every regular file under a directory,
	// recursively.
	controlPlaneMemberTreeScan
)

// controlPlaneMember is one row of the inventory table in
// docs/DISASTER-RECOVERY.md section 2 and nothing else.
type controlPlaneMember struct {
	Component string
	Path      string
	Kind      controlPlaneMemberKind
	Required  bool
}

// controlPlaneInventory is a pure description of what the panel brain is. It
// resolves no path and touches no disk; collectControlPlaneMembers expands the
// scans.
func controlPlaneInventory(roots controlPlaneRoots) []controlPlaneMember {
	return []controlPlaneMember{
		{
			Component: "panel database",
			Path:      filepath.Join(roots.DataDir, controlPlaneDatabaseBasename),
			Kind:      controlPlaneMemberDatabase,
			Required:  true,
		},
		{
			Component: "secret key",
			Path:      filepath.Join(roots.DataDir, controlPlaneSecretKeyBasename),
			Kind:      controlPlaneMemberFile,
			Required:  true,
		},
		{
			Component: "panel configuration",
			Path:      filepath.Join(roots.ConfDir, "panel.env"),
			Kind:      controlPlaneMemberFile,
		},
		{
			Component: "agent token",
			Path:      filepath.Join(roots.ConfDir, "agent.token"),
			Kind:      controlPlaneMemberFile,
		},
		{
			Component: "firewall snapshot",
			Path:      filepath.Join(roots.ConfDir, "firewall.nft"),
			Kind:      controlPlaneMemberFile,
		},
		{
			Component: "agent private state",
			Path:      roots.AgentStateDir,
			Kind:      controlPlaneMemberFlatScan,
		},
		{
			Component: "DKIM keys",
			Path:      filepath.Join(roots.DKIMDir, "keys"),
			Kind:      controlPlaneMemberTreeScan,
		},
		{
			Component: "WireGuard",
			Path:      roots.WireGuardDir,
			Kind:      controlPlaneMemberTreeScan,
		},
		{
			Component: "panel TLS",
			Path:      roots.TLSDir,
			Kind:      controlPlaneMemberTreeScan,
		},
	}
}

// controlPlaneCollectedMember is one concrete file the archive will carry.
type controlPlaneCollectedMember struct {
	Component string
	Path      string
	Database  bool
}

// controlPlaneCollection is the result of reading the live host once.
type controlPlaneCollection struct {
	Members     []controlPlaneCollectedMember
	Directories []string
	Skipped     []controlPlaneSkippedMember
}

type controlPlaneSkippedMember struct {
	Component string
	Path      string
	Reason    string
}

// collectControlPlaneMembers reads the live host and returns exactly the files
// the archive carries. A symlink anywhere in the inventory is an error and is
// never followed: on a control-plane host a symlink where a key belongs is a
// redirection attempt, not a convenience.
func collectControlPlaneMembers(roots controlPlaneRoots) (controlPlaneCollection, error) {
	collection := controlPlaneCollection{}
	directories := map[string]struct{}{}
	for _, member := range controlPlaneInventory(roots) {
		switch member.Kind {
		case controlPlaneMemberDatabase, controlPlaneMemberFile:
			info, err := os.Lstat(member.Path)
			if err != nil {
				if os.IsNotExist(err) {
					if member.Required {
						return controlPlaneCollection{}, fmt.Errorf(
							"the %s at %s is missing and the archive cannot be taken without it",
							member.Component,
							member.Path,
						)
					}
					collection.Skipped = append(collection.Skipped, controlPlaneSkippedMember{
						Component: member.Component,
						Path:      member.Path,
						Reason:    "absent",
					})
					continue
				}
				return controlPlaneCollection{}, fmt.Errorf("inspect %s: %w", member.Path, err)
			}
			if err := requireControlPlaneRegularFile(member.Path, info); err != nil {
				return controlPlaneCollection{}, err
			}
			directories[filepath.Dir(member.Path)] = struct{}{}
			collection.Members = append(collection.Members, controlPlaneCollectedMember{
				Component: member.Component,
				Path:      member.Path,
				Database:  member.Kind == controlPlaneMemberDatabase,
			})
		case controlPlaneMemberFlatScan, controlPlaneMemberTreeScan:
			found, err := scanControlPlaneDirectory(
				member,
				member.Kind == controlPlaneMemberTreeScan,
				directories,
			)
			if err != nil {
				return controlPlaneCollection{}, err
			}
			if found == nil {
				collection.Skipped = append(collection.Skipped, controlPlaneSkippedMember{
					Component: member.Component,
					Path:      member.Path,
					Reason:    "absent",
				})
				continue
			}
			collection.Members = append(collection.Members, found...)
		default:
			return controlPlaneCollection{}, fmt.Errorf("unsupported control-plane member kind %d", member.Kind)
		}
	}
	collection.Directories = sortedControlPlaneDirectories(directories)
	return collection, nil
}

func scanControlPlaneDirectory(
	member controlPlaneMember,
	recursive bool,
	directories map[string]struct{},
) ([]controlPlaneCollectedMember, error) {
	info, err := os.Lstat(member.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect %s: %w", member.Path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf(
			"the %s directory %s is a symbolic link; the control-plane archive never follows one",
			member.Component,
			member.Path,
		)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("the %s path %s is not a directory", member.Component, member.Path)
	}
	directories[member.Path] = struct{}{}
	found := []controlPlaneCollectedMember{}
	walk := func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("read %s: %w", current, walkErr)
		}
		if current == member.Path {
			return nil
		}
		entryInfo, err := entry.Info()
		if err != nil {
			return fmt.Errorf("inspect %s: %w", current, err)
		}
		if entryInfo.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf(
				"%s is a symbolic link; the control-plane archive never follows one",
				current,
			)
		}
		if entry.IsDir() {
			// A flat scan reads one directory only. The agent state directory
			// is the flat one, and the two exclusions below are its own: the
			// transient system SQLite snapshot subtree, which has a five-minute
			// lifetime, and the dot-prefixed staging names the ledger writes
			// while it publishes. Every other tree is archived whole.
			if !recursive {
				return fs.SkipDir
			}
			directories[current] = struct{}{}
			return nil
		}
		if !entryInfo.Mode().IsRegular() {
			return fmt.Errorf(
				"%s is neither a regular file nor a directory and cannot be archived",
				current,
			)
		}
		if !recursive && strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		directories[filepath.Dir(current)] = struct{}{}
		found = append(found, controlPlaneCollectedMember{
			Component: member.Component,
			Path:      current,
		})
		return nil
	}
	if recursive {
		if err := filepath.WalkDir(member.Path, walk); err != nil {
			return nil, err
		}
	} else {
		entries, err := os.ReadDir(member.Path)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", member.Path, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			if err := walk(filepath.Join(member.Path, entry.Name()), entry, nil); err != nil {
				return nil, err
			}
		}
	}
	sort.Slice(found, func(left, right int) bool {
		return found[left].Path < found[right].Path
	})
	return found, nil
}

func requireControlPlaneRegularFile(path string, info os.FileInfo) error {
	if info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf(
			"%s is a symbolic link; the control-plane archive never follows one",
			path,
		)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file and cannot be archived", path)
	}
	return nil
}

func sortedControlPlaneDirectories(directories map[string]struct{}) []string {
	sorted := make([]string, 0, len(directories))
	for directory := range directories {
		sorted = append(sorted, directory)
	}
	sort.Strings(sorted)
	return sorted
}
