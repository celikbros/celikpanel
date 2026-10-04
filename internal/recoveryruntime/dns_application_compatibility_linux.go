//go:build linux

package recoveryruntime

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

var errDNSSwitchJournalApplication = errors.New("active DNS switch journal requires an Agent that reads the exact v2 BIND inverse plan; preserve the evidence and choose a compatible release")
var errDNSSwitchJournalApplicationV4 = errors.New("active v4 PowerDNS target journal requires a proved independent recovery runtime; preserve the evidence and choose a compatible release")

var errDNSApplicationV3 = errors.New("v3 PowerDNS evidence requires an explicitly compatible Agent and independent recovery runtime; preserve the evidence and choose a compatible release")

var errDNSApplication = errors.New("DNS evidence requires an Agent with separated acquisition/publication support; preserve the evidence and choose a compatible release")

// CheckDNSApplicationCompatibility observes protected DNS evidence and target
// Agent bytes. It changes no files, starts no daemon and grants no mutation.
// The caller supplies trusted release/snapshot provenance and release/host
// exclusion, and repeats this check before application publication.
func CheckDNSApplicationCompatibility(binaryDirectory, stateDirectory string) error {
	return checkDNSApplicationCompatibility(binaryDirectory, stateDirectory, nil)
}

func checkDNSApplicationCompatibility(bin, root string, beforeRevalidate func()) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(bin) || filepath.Clean(bin) != bin || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return fail(ReasonUnsafeMetadata)
	}
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer state.close()
	parent, absence, err := optionalMailHookParent(state, root)
	if err != nil {
		return err
	}
	absences := []func() error{}
	if absence != nil {
		absences = append(absences, absence)
	}
	required := false
	requireSwitchV2 := false
	requireSwitchV4 := false
	requireFreshV3 := false
	requireSourceProof := false
	requireAdoptionProof := false
	if parent != nil {
		// Private evidence can retain its historical root:service group. Mode 0600
		// gives that group no access. Pin it exactly; never normalize group metadata.
		if parent.stat.Mode&07777 != 0700 {
			return fail(ReasonUnsafeMetadata)
		}
		for _, entry := range []struct {
			name   string
			engine transport.DNSEngine
		}{
			{"dns-engine-state.json", ""},
			{"dns-engine-ownership-bind.json", transport.DNSEngineBIND},
			{"dns-engine-ownership-pdns.json", transport.DNSEnginePowerDNS},
			{"dns-engine-switch-journal.json", ""},
		} {
			var st unix.Stat_t
			if e := unix.Fstatat(int(parent.file.Fd()), entry.name, &st, unix.AT_SYMLINK_NOFOLLOW); errors.Is(e, unix.ENOENT) {
				name := entry.name
				absences = append(absences, func() error { return requireMailPathAbsent(parent, name) })
				continue
			} else if e != nil {
				return asReadError(e)
			}
			limit := int64(64 << 10)
			if entry.name == "dns-engine-switch-journal.json" {
				limit = dnsengineartifact.SwitchJournalLimit
			}
			file, e := state.openFileWithGID(parent, entry.name, 0600, limit, st.Gid)
			if e != nil {
				return asReadError(e)
			}
			if !sameFile(st, file.stat) {
				return fail(ReasonChanged)
			}
			if e = refusePromotionXattrs(file); e != nil {
				return e
			}
			raw, e := file.readBounded()
			if e != nil {
				return e
			}
			file.digest = Digest(raw)
			if entry.name == "dns-engine-switch-journal.json" {
				policy := dnsengineartifact.JournalPolicy{
					StatePath: filepath.Join(root, "dns-engine-state.json"), StateUID: 0, StateGID: st.Gid, RequireOwner: true,
					PDNSMainPath:     "/etc/powerdns/pdns.conf",
					PDNSManagedPath:  "/etc/powerdns/pdns.d/celikpanel.conf",
					PDNSClusterPath:  "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
					PDNSDatabasePath: "/var/lib/powerdns/pdns.sqlite3",
				}
				journal, decodeErr := policy.DecodeSwitchJournal(raw)
				if decodeErr != nil {
					return fail(ReasonUnsupported)
				}
				requireSwitchV2 = journal.Schema == dnsengineartifact.SwitchJournalSchemaV2
				requireSwitchV4 = journal.Schema == dnsengineartifact.SwitchJournalSchemaV4
				requireFreshV3 = requireFreshV3 || journal.Schema == dnsengineartifact.SwitchJournalSchemaV3
				requireSourceProof = requireSwitchV2 && journal.InversePlan != nil && journal.InversePlan.SourcePDNS != nil
				requireAdoptionProof = requireSwitchV2 && journal.InversePlan != nil && journal.InversePlan.SourceBIND != nil
				continue
			}
			var receipt dnsengineartifact.StateV1
			var separated bool
			if entry.engine == "" {
				receipt, separated, e = dnsengineartifact.DecodeStateDocument(raw)
			} else {
				receipt, separated, e = dnsengineartifact.DecodeOwnershipDocument(raw)
			}
			if e != nil || (entry.engine != "" && receipt.Engine != entry.engine) {
				return fail(ReasonUnsupported)
			}
			required = required || separated
			requireFreshV3 = requireFreshV3 || receipt.NativeCatalogV3 != ""
		}
	}
	// V3 has no accepted application/recovery compatibility declaration yet.
	// A legacy separated-evidence marker cannot stand in for that contract.
	if requireFreshV3 {
		return errDNSApplicationV3
	}
	var target *CompatibleMailAgent
	if required || requireSwitchV2 {
		target, err = InspectCompatibleMailAgent(bin)
		if err != nil {
			return err
		}
		defer target.Close()
		if required && target.Contract.DNSEvidencePolicy != agentnativecontract.DNSEvidencePolicy {
			return errDNSApplication
		}
		if requireSwitchV2 && !supportsBINDJournalPolicy(target.Contract.DNSSwitchJournalPolicy, requireSourceProof, requireAdoptionProof) {
			return errDNSSwitchJournalApplication
		}
	}
	if requireSwitchV4 {
		return errDNSSwitchJournalApplicationV4
	}
	if beforeRevalidate != nil {
		beforeRevalidate()
	}
	if target != nil {
		if err = target.Revalidate(); err != nil {
			return err
		}
	}
	if err = state.revalidate(); err != nil {
		return err
	}
	for _, check := range absences {
		if err = check(); err != nil {
			return err
		}
	}
	return state.revalidate()
}

func supportsBINDJournalPolicy(policy string, sourceProof, adoptionProof bool) bool {
	switch policy {
	case agentnativecontract.DNSSwitchAdoptionJournalPolicy:
		return true
	case agentnativecontract.DNSSwitchSourceJournalPolicy:
		return !adoptionProof
	case agentnativecontract.DNSSwitchJournalPolicy:
		return !sourceProof && !adoptionProof
	default:
		return false
	}
}
