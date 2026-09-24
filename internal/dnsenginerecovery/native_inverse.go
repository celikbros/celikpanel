package dnsenginerecovery

import (
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

// NativeInverseKind is the native compensation required by the frozen switch,
// not an authorization to perform it. In particular, an already-running owner
// BIND is restored by reload, never by the initial-install unit stop path.
type NativeInverseKind string

const (
	NativeInverseBINDRunningAdoption NativeInverseKind = "bind-running-adoption"
	NativeInverseBINDSwitch          NativeInverseKind = "bind-switch"
	NativeInversePDNSAdoption        NativeInverseKind = "pdns-adoption"
	NativeInversePDNSSwitch          NativeInverseKind = "pdns-switch"
)

// PlanNativeInverse selects the exact native inverse from a previously
// validated, accepted journal. It reconstructs the immutable manifest and
// fails closed if it disagrees. The caller must still hold the operation locks,
// exclude a live worker, prove owner/native state and retain all checkpoints.
func PlanNativeInverse(journal dnsengineartifact.SwitchJournalV1) (NativeInverseKind, error) {
	manifest, err := dnsengineartifact.SwitchJournalManifest(journal)
	if err != nil {
		return "", err
	}
	switch journal.TargetEngine {
	case transport.DNSEngineBIND:
		if journal.Mode != transport.DNSEngineSwitchModeSwitch &&
			journal.Mode != transport.DNSEngineSwitchModeReinstall {
			return "", errors.New("BIND journal mode has no native inverse")
		}
		running, err := dnsengineartifact.RunningBINDAdoptionJournal(manifest, journal)
		if err != nil {
			return "", err
		}
		if running {
			return NativeInverseBINDRunningAdoption, nil
		}
		return NativeInverseBINDSwitch, nil
	case transport.DNSEnginePowerDNS:
		switch journal.Mode {
		case transport.DNSEngineSwitchModeAdopt:
			return NativeInversePDNSAdoption, nil
		case transport.DNSEngineSwitchModeSwitch, transport.DNSEngineSwitchModeReinstall:
			return NativeInversePDNSSwitch, nil
		default:
			return "", errors.New("PowerDNS journal mode has no native inverse")
		}
	default:
		return "", errors.New("DNS engine rollback target is unsupported")
	}
}
