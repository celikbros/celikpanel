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
		} {
			var st unix.Stat_t
			if e := unix.Fstatat(int(parent.file.Fd()), entry.name, &st, unix.AT_SYMLINK_NOFOLLOW); errors.Is(e, unix.ENOENT) {
				name := entry.name
				absences = append(absences, func() error { return requireMailPathAbsent(parent, name) })
				continue
			} else if e != nil {
				return asReadError(e)
			}
			file, e := state.openFileWithGID(parent, entry.name, 0600, 64<<10, st.Gid)
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
		}
	}
	var target *CompatibleMailAgent
	if required {
		target, err = InspectCompatibleMailAgent(bin)
		if err != nil {
			return err
		}
		defer target.Close()
		if target.Contract.DNSEvidencePolicy != agentnativecontract.DNSEvidencePolicy {
			return errDNSApplication
		}
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
