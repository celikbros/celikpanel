//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/pdnsnative"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

// VerifyFreshPrimaryPreparedConfigsV3 is the after-start configuration rule.
// An unchanged native file can match both frozen images. The secure probe
// reports "before" first; that is still the prepared target only when the
// complete frozen snapshots are identical. A changed file left at "before"
// remains pending, and an unknown file is never accepted.
func VerifyFreshPrimaryPreparedConfigsV3(configs []PDNSTargetConfigStateV4, j dnsengineartifact.SwitchJournalV1) error {
	if j.PDNSFreshPlan == nil || len(configs) != len(j.ConfigBefore) || len(configs) != len(j.PDNSFreshPlan.ConfigAfter) {
		return errors.New("v3 target config count changed")
	}
	for i := range configs {
		if !freshPrimaryPreparedConfigV3(configs[i], j, i) {
			return errors.New("v3 native PowerDNS config differs from prepared target")
		}
	}
	return nil
}

func freshPrimaryPreparedConfigV3(state PDNSTargetConfigStateV4, j dnsengineartifact.SwitchJournalV1, i int) bool {
	return state == PDNSTargetConfigAfterV4 ||
		(state == PDNSTargetConfigBeforeV4 && reflect.DeepEqual(j.ConfigBefore[i], j.PDNSFreshPlan.ConfigAfter[i]))
}

// PDNSFreshConfigFindingV3 is one fixed configuration path compared with the
// install's frozen before and after images. Differs means the file was read
// completely and is neither image; its content is never carried.
type PDNSFreshConfigFindingV3 struct {
	Path    string
	State   PDNSTargetConfigStateV4
	Differs bool
}

// ClassifyInstalledPDNSFreshConfigsV3 is the read-only per-path form of
// ProbeInstalledPDNSFreshConfigsV3: it uses the same secure file observer and
// images but tells a file that differs from one that could not be read. An
// unreadable or unstable file is returned as an error naming its path.
func ClassifyInstalledPDNSFreshConfigsV3(ctx context.Context, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32) ([]PDNSFreshConfigFindingV3, error) {
	root, err := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	defer unix.Close(root)
	return ClassifyPDNSFreshConfigsAtV3(ctx, root, policy, journal, pdnsGID)
}

func ClassifyPDNSFreshConfigsAtV3(ctx context.Context, root int, policy dnsengineartifact.JournalPolicy, journal dnsengineartifact.SwitchJournalV1, pdnsGID uint32) ([]PDNSFreshConfigFindingV3, error) {
	if ctx == nil || root < 0 || pdnsGID == 0 || pdnsGID > 1<<31-1 ||
		policy.PDNSMainPath != "/etc/powerdns/pdns.conf" ||
		policy.PDNSManagedPath != "/etc/powerdns/pdns.d/celikpanel.conf" ||
		policy.PDNSClusterPath != "/etc/powerdns/pdns.d/celikpanel-cluster.conf" {
		return nil, errors.New("v3 PowerDNS config comparison requires installed fixed paths and service group")
	}
	if err := policy.ValidateSwitchJournal(journal); err != nil {
		return nil, err
	}
	if journal.Schema != dnsengineartifact.SwitchJournalSchemaV3 || journal.PDNSFreshPlan == nil ||
		len(journal.ConfigBefore) != 3 || len(journal.PDNSFreshPlan.ConfigAfter) != 3 {
		return nil, errors.New("v3 fresh PowerDNS config commitment is incomplete")
	}
	before, after := journal.ConfigBefore, journal.PDNSFreshPlan.ConfigAfter
	for i := range before {
		if before[i].Path != after[i].Path {
			return nil, errors.New("v3 config path changed")
		}
		if before[i].Path == policy.PDNSMainPath &&
			((before[i].Exists && before[i].GID != 0 && before[i].GID != pdnsGID) ||
				(after[i].Exists && after[i].GID != 0 && after[i].GID != pdnsGID)) {
			return nil, errors.New("v3 PowerDNS main config group changed")
		}
	}
	if _, err := bindroot.ValidateInheritedAnchor(root, "v3 PowerDNS config comparison root"); err != nil {
		return nil, err
	}
	first, err := classifyPDNSFreshConfigPassV3(ctx, root, before, after)
	if err != nil {
		return nil, err
	}
	second, err := classifyPDNSFreshConfigPassV3(ctx, root, before, after)
	if err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(first, second) {
		return nil, errors.New("v3 PowerDNS configuration changed between two reads")
	}
	return second, ctx.Err()
}

func classifyPDNSFreshConfigPassV3(ctx context.Context, root int, before, after []dnsengineartifact.FileSnapshot) ([]PDNSFreshConfigFindingV3, error) {
	findings := make([]PDNSFreshConfigFindingV3, len(before))
	for i := range before {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		findings[i].Path = before[i].Path
		_, beforeErr := probePDNSAdoptionConfigFile(ctx, root, before[i])
		if beforeErr == nil {
			findings[i].State = PDNSTargetConfigBeforeV4
			continue
		}
		_, afterErr := probePDNSAdoptionConfigFile(ctx, root, after[i])
		if afterErr == nil {
			findings[i].State = PDNSTargetConfigAfterV4
			continue
		}
		if !pdnsConfigDiffers(beforeErr) {
			return nil, fmt.Errorf("PowerDNS config %s could not be read: %w", before[i].Path, beforeErr)
		}
		if !pdnsConfigDiffers(afterErr) {
			return nil, fmt.Errorf("PowerDNS config %s could not be read: %w", before[i].Path, afterErr)
		}
		findings[i].Differs = true
	}
	return findings, nil
}

// ClassifyPDNSTargetLiveV4 is the read-only form of VerifyPDNSTargetLiveV4
// for status: differs is true when the observation completed and the live
// database is not the sealed candidate renamed in place; an error means it
// could not be observed or compared.
func ClassifyPDNSTargetLiveV4(proof dnsengineartifact.PDNSTargetCandidateProofV4, livePath string) (bool, error) {
	if !validPDNSTargetPath(proof.Path) || !validPDNSTargetPath(livePath) ||
		proof.Path == livePath ||
		!proof.NoSidecars || proof.Device == 0 || proof.Inode == 0 || proof.Size == 0 ||
		!dnsengineartifact.ValidGeneration(proof.SHA256) {
		return false, errors.New("PowerDNS target proof is not bound to a safe candidate and live path")
	}
	if _, err := os.Lstat(proof.Path); err == nil {
		return true, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("inspect renamed PowerDNS candidate: %w", err)
	}
	if _, err := observePDNSTargetDirectory(proof.Path, true); err != nil {
		return false, err
	}
	liveDir, err := observePDNSTargetDirectory(livePath, false)
	if err != nil {
		return false, err
	}
	if uint64(liveDir.Dev) != proof.Device {
		return true, nil
	}
	actual, err := probePDNSTargetFileV4(livePath, false, nil)
	if err != nil {
		return false, err
	}
	actual.Path = proof.Path
	return actual != proof, nil
}

// FreshPrimaryChangeV3 is what a read-only comparison found not as the fresh
// paired PowerDNS primary install wrote it. It names paths only; it never
// carries file contents, database rows or secrets.
type FreshPrimaryChangeV3 struct {
	// Prestart is the Agent's route for this journal: undo (PowerDNS never
	// started) or forward only (it may have started).
	Prestart bool
	// Compared is false when the Agent's own route stops before these
	// comparisons (the unit is not a stopped, never-started target of a
	// pre-start journal, or the phase has no after-start comparison); the
	// other fields are then empty and nothing was concluded.
	Compared        bool
	ConfigPaths     []string
	Database        bool
	StateRecord     bool
	OwnershipRecord bool
}

// Changed reports whether any compared thing differs.
func (c FreshPrimaryChangeV3) Changed() bool {
	return len(c.ConfigPaths) != 0 || c.Database || c.StateRecord || c.OwnershipRecord
}

// FreshPrimaryUnknownV3 names the one observation the comparison could not
// make. What is a plain phrase for the owner; Err keeps the technical cause.
type FreshPrimaryUnknownV3 struct {
	What string
	Err  error
}

func (e *FreshPrimaryUnknownV3) Error() string {
	return e.What + " could not be compared with what the install wrote: " + fmt.Sprint(e.Err)
}

func (e *FreshPrimaryUnknownV3) Unwrap() error { return e.Err }

func freshPrimaryUnknown(what string, err error) error {
	if err == nil {
		err = errors.New("no observation")
	}
	return &FreshPrimaryUnknownV3{What: what, Err: err}
}

// FreshPrimaryChangeObserversV3 are read-only observers. Tests replace them.
type FreshPrimaryChangeObserversV3 struct {
	Policy dnsengineartifact.JournalPolicy
	// Configs compares the three fixed configuration paths.
	Configs func(context.Context, dnsengineartifact.SwitchJournalV1) ([]PDNSFreshConfigFindingV3, error)
	// Exists is a no-follow presence check; an error is unknown.
	Exists func(string) (bool, error)
	// Candidate captures the sealed candidate's identity.
	Candidate func(string) (dnsengineartifact.PDNSTargetCandidateProofV4, error)
	// Live compares the live database with the sealed candidate.
	Live func(dnsengineartifact.PDNSTargetCandidateProofV4, string) (bool, error)
	// Native captures the live database of a started target from a copy.
	Native func(context.Context, dnsengineartifact.SwitchJournalV1) (pdnsnative.Snapshot, error)
	// State and Ownership read CelikPanel's own records; nil means absent.
	State     func() (*dnsengineartifact.StateV1, error)
	Ownership func() (*dnsengineartifact.StateV1, error)
}

// ClassifyFreshPrimaryChangeV3 applies, read-only, the comparisons with which
// the Agent refuses to continue or undo a fresh paired PowerDNS primary
// install because something is not as the install wrote it: the pre-start
// ones of its undo and the after-start ones of its forward recovery. It
// routes a journal as the Agent does (pdns is the observed pdns.service
// unit). Unlike the Agent it does not stop at the first difference, and it
// reports a file it could not read as unknown rather than as a change. It
// does not repeat the Agent's other proofs (listeners, process identity,
// peer authority); those are not owner-change comparisons.
func ClassifyFreshPrimaryChangeV3(ctx context.Context, j dnsengineartifact.SwitchJournalV1, pdns dnsengineartifact.UnitSnapshot, obs FreshPrimaryChangeObserversV3) (FreshPrimaryChangeV3, error) {
	var result FreshPrimaryChangeV3
	if ctx == nil || obs.Configs == nil || obs.Exists == nil || obs.Candidate == nil || obs.Live == nil ||
		obs.Native == nil || obs.State == nil || obs.Ownership == nil {
		return result, errors.New("v3 change comparison observers are incomplete")
	}
	if err := obs.Policy.ValidateSwitchJournal(j); err != nil {
		return result, freshPrimaryUnknown("the install's journal", err)
	}
	if j.Schema != dnsengineartifact.SwitchJournalSchemaV3 || j.PDNSFreshPlan == nil {
		return result, errors.New("the journal is not a fresh paired PowerDNS primary install")
	}
	result.Prestart = FreshPrimaryPrestartShapeJournalV3(j) &&
		!(j.Phase == dnsengineartifact.SwitchPhaseTargetEnableIntent && pdns.ActiveState == "active")
	if result.Prestart {
		return classifyFreshPrimaryPrestartChangeV3(ctx, j, pdns, obs)
	}
	return classifyFreshPrimaryForwardChangeV3(ctx, j, obs)
}

func classifyFreshPrimaryPrestartChangeV3(ctx context.Context, j dnsengineartifact.SwitchJournalV1, pdns dnsengineartifact.UnitSnapshot, obs FreshPrimaryChangeObserversV3) (FreshPrimaryChangeV3, error) {
	result := FreshPrimaryChangeV3{Prestart: true}
	frozen := j.TargetUnitsBefore[0]
	if !FreshPrimaryPrestartTargetUnitV3(pdns, frozen, j.Phase) {
		return result, nil
	}
	result.Compared = true
	configs, err := obs.Configs(ctx, j)
	if err != nil {
		return FreshPrimaryChangeV3{Prestart: true}, freshPrimaryUnknown(FreshPrimaryChangedConfigV3, err)
	}
	plan := j.PDNSFreshPlan
	allBefore := len(configs) > 0
	for _, config := range configs {
		allBefore = allBefore && config.State == PDNSTargetConfigBeforeV4
		// Before a candidate is sealed the install has written no
		// configuration yet, so the after-image is not the install's either.
		if config.Differs || (plan.Candidate == nil && config.State != PDNSTargetConfigBeforeV4) {
			result.ConfigPaths = append(result.ConfigPaths, config.Path)
		}
	}
	stateExists, err := obs.Exists(obs.Policy.StatePath)
	if err != nil {
		return FreshPrimaryChangeV3{Prestart: true}, freshPrimaryUnknown(FreshPrimaryChangedStateV3, err)
	}
	result.StateRecord = stateExists
	if plan.Candidate == nil {
		// The Agent compares no database before a candidate is sealed.
		return result, ctx.Err()
	}
	db := obs.Policy.PDNSDatabasePath
	present := map[string]bool{}
	for _, path := range []string{j.PDNSCandidatePath, db, db + "-wal", db + "-shm", db + "-journal"} {
		exists, err := obs.Exists(path)
		if err != nil {
			return FreshPrimaryChangeV3{Prestart: true}, freshPrimaryUnknown(FreshPrimaryChangedDatabaseV3, err)
		}
		present[path] = exists
	}
	sidecars := present[db+"-wal"] || present[db+"-shm"] || present[db+"-journal"]
	proof := *plan.Candidate
	switch {
	case FreshPrimaryPrestartRollbackPhaseV3(j.Phase) && allBefore && pdns == frozen &&
		!present[j.PDNSCandidatePath] && !present[db] && !sidecars:
		// Restored to the state before the install.
	case !present[db] && !sidecars:
		staged, err := obs.Exists(proof.Path)
		if err != nil {
			return FreshPrimaryChangeV3{Prestart: true}, freshPrimaryUnknown(FreshPrimaryChangedDatabaseV3, err)
		}
		if !staged {
			// The sealed database the install staged is gone.
			result.Database = true
			break
		}
		actual, err := obs.Candidate(proof.Path)
		if err != nil {
			return FreshPrimaryChangeV3{Prestart: true}, freshPrimaryUnknown(FreshPrimaryChangedDatabaseV3, err)
		}
		result.Database = actual != proof
	case !sidecars:
		differs, err := obs.Live(proof, db)
		if err != nil {
			return FreshPrimaryChangeV3{Prestart: true}, freshPrimaryUnknown(FreshPrimaryChangedDatabaseV3, err)
		}
		result.Database = differs
	default:
		result.Database = true
	}
	return result, ctx.Err()
}

func classifyFreshPrimaryForwardChangeV3(ctx context.Context, j dnsengineartifact.SwitchJournalV1, obs FreshPrimaryChangeObserversV3) (FreshPrimaryChangeV3, error) {
	var result FreshPrimaryChangeV3
	switch j.Phase {
	case dnsengineartifact.SwitchPhaseTargetEnableIntent, dnsengineartifact.SwitchPhaseTargetStarted,
		dnsengineartifact.SwitchPhaseTargetVerified, dnsengineartifact.SwitchPhaseCommitted:
	default:
		return result, nil
	}
	if j.PDNSFreshPlan.Staged == nil || j.PDNSFreshPlan.Candidate == nil {
		return result, freshPrimaryUnknown("the install's journal", errors.New("v3 journal lacks its staged database evidence"))
	}
	result.Compared = true
	configs, err := obs.Configs(ctx, j)
	if err != nil {
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown(FreshPrimaryChangedConfigV3, err)
	}
	if len(configs) != len(j.ConfigBefore) || len(configs) != len(j.PDNSFreshPlan.ConfigAfter) {
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown(FreshPrimaryChangedConfigV3, errors.New("v3 target config count changed"))
	}
	for i, config := range configs {
		if config.Differs || !freshPrimaryPreparedConfigV3(config.State, j, i) {
			result.ConfigPaths = append(result.ConfigPaths, config.Path)
		}
	}
	live, err := obs.Native(ctx, j)
	if err != nil {
		// A capture that could not complete (for example a busy database)
		// is unknown, as it is for the Agent.
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown(FreshPrimaryChangedDatabaseV3, err)
	}
	domain, err := binddns.CatalogDomain(j.LocalIP)
	if err != nil {
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown("the install's journal", err)
	}
	observed := FreshPrimaryNativeObservationViewV3(j)
	if observed.PDNSFreshPlan.Native == nil {
		if err := obs.Policy.ValidateSwitchJournal(observed); err != nil {
			return FreshPrimaryChangeV3{}, freshPrimaryUnknown("the install's journal", err)
		}
		observed, err = obs.Policy.AttachPDNSFreshNativeObservationV3(observed, domain, live)
		if err != nil {
			// Only the measured daemon start-up transform is admitted.
			result.Database = true
			return result, ctx.Err()
		}
	} else if err := pdnsnative.VerifyRecordedFreshPrimaryCatalogTransition(
		*j.PDNSFreshPlan.Staged, live, domain, j.PrimaryCatalogSerial, *j.PDNSFreshPlan.Native,
	); err != nil {
		result.Database = true
		return result, ctx.Err()
	}
	desired, err := dnsengineartifact.FreshPrimaryTargetStateV3(observed)
	if err != nil {
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown("the install's journal", err)
	}
	state, err := obs.State()
	if err != nil {
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown(FreshPrimaryChangedStateV3, err)
	}
	result.StateRecord = state != nil && *state != desired
	ownership, err := obs.Ownership()
	if err != nil {
		return FreshPrimaryChangeV3{}, freshPrimaryUnknown("CelikPanel's PowerDNS ownership record", err)
	}
	result.OwnershipRecord = ownership != nil && *ownership != desired
	return result, ctx.Err()
}

// FreshPrimaryOwnershipRecordPathV3 is where the Agent keeps its PowerDNS
// ownership record beside the DNS state record.
func FreshPrimaryOwnershipRecordPathV3(stateRoot string) string {
	return filepath.Join(stateRoot, freshPrimaryOwnershipRecordName)
}

// InstalledFreshPrimaryChangeObserversV3 binds the comparison to the fixed
// installed paths. Every observer reads; none writes, repairs or retries a
// mutation. The native capture reads a private copy of the database, never
// the live file through SQLite.
func InstalledFreshPrimaryChangeObserversV3(policy dnsengineartifact.JournalPolicy, stateRoot string, owner servicemutationledger.FileOwner, pdnsGID uint32) FreshPrimaryChangeObserversV3 {
	return FreshPrimaryChangeObserversV3{
		Policy: policy,
		Configs: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) ([]PDNSFreshConfigFindingV3, error) {
			return ClassifyInstalledPDNSFreshConfigsV3(ctx, policy, j, pdnsGID)
		},
		Exists: func(path string) (bool, error) {
			if path == "" {
				return false, errors.New("an expected path is empty")
			}
			_, err := os.Lstat(path)
			switch {
			case err == nil:
				return true, nil
			case errors.Is(err, os.ErrNotExist):
				return false, nil
			}
			return false, err
		},
		Candidate: CapturePDNSTargetCandidateV4,
		Live:      ClassifyPDNSTargetLiveV4,
		Native: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (pdnsnative.Snapshot, error) {
			return CaptureFreshPrimaryNativeV3(ctx, policy, j)
		},
		State: func() (*dnsengineartifact.StateV1, error) {
			raw, exists, err := servicemutationledger.ReadFile(policy.StatePath, 64<<10, owner)
			if err != nil || !exists {
				return nil, err
			}
			state, _, err := dnsengineartifact.DecodeStateDocument(raw)
			if err != nil {
				return nil, err
			}
			return &state, nil
		},
		Ownership: func() (*dnsengineartifact.StateV1, error) {
			raw, exists, err := servicemutationledger.ReadFile(FreshPrimaryOwnershipRecordPathV3(stateRoot), freshPrimaryOwnershipRecordLimit, owner)
			if err != nil || !exists {
				return nil, err
			}
			ownership, _, err := dnsengineartifact.DecodeOwnershipDocument(raw)
			if err != nil {
				return nil, err
			}
			if ownership.Engine != transport.DNSEnginePowerDNS {
				return nil, errors.New("PowerDNS ownership record names another engine")
			}
			return &ownership, nil
		},
	}
}
