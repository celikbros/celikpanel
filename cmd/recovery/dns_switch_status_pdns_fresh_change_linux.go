//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// The Agent refuses to continue or undo a fresh paired PowerDNS primary
// install (V3 journal) when something is not as the install wrote it, and
// records that in the ledger message. dns-switch-status --quiesced then
// makes the same comparisons read-only (dnsenginerecovery.
// ClassifyFreshPrimaryChangeV3) and says what differs now, where, who acts
// and how the install resumes. It prints paths only: never file contents,
// database rows or secrets.

// freshPrimaryChangeReader compares, read-only, what the install wrote with
// what is on the host now. pdns is the observed pdns.service unit.
type freshPrimaryChangeReader func(ctx context.Context, root string, owner servicemutationledger.FileOwner,
	policy dnsengineartifact.JournalPolicy, e dnsenginerecovery.SwitchEvidence,
	pdns dnsengineartifact.UnitSnapshot) (dnsenginerecovery.FreshPrimaryChangeV3, error)

func installedFreshPrimaryChange(ctx context.Context, root string, owner servicemutationledger.FileOwner,
	policy dnsengineartifact.JournalPolicy, e dnsenginerecovery.SwitchEvidence,
	pdns dnsengineartifact.UnitSnapshot) (dnsenginerecovery.FreshPrimaryChangeV3, error) {
	gid, err := localServiceGroupID("/etc/group", "pdns")
	if err != nil {
		return dnsenginerecovery.FreshPrimaryChangeV3{}, &dnsenginerecovery.FreshPrimaryUnknownV3{What: "the PowerDNS service group", Err: err}
	}
	return dnsenginerecovery.ClassifyFreshPrimaryChangeV3(ctx, e.Journal, pdns,
		dnsenginerecovery.InstalledFreshPrimaryChangeObserversV3(policy, root, owner, gid))
}

// freshPrimaryReleasedV3 selects the Agent's deliberate release of this exact
// fresh paired PowerDNS primary request with its journal kept.
func freshPrimaryReleasedV3(e dnsenginerecovery.SwitchEvidence) bool {
	j, o := e.Journal, e.Observation
	return j.Schema == dnsengineartifact.SwitchJournalSchemaV3 && j.PDNSFreshPlan != nil &&
		o.Status == dnsenginerecovery.EvidenceReleasedUndecided &&
		o.ReleaseReason == dnsengineartifact.ReleasedNativeUnknownCode &&
		o.RequestID == j.MutationRequestID && e.AcceptedJob.RequestID == j.MutationRequestID &&
		dnsenginerecovery.AgentDeliberateReleaseJob(e.AcceptedJob)
}

// freshPrimaryChangeStatus is the classified observation. err is an
// observation this check could not make (exit 3); otherwise the result is
// classified (exit 0).
type freshPrimaryChangeStatus struct {
	change dnsenginerecovery.FreshPrimaryChangeV3
	err    error
}

// observeFreshPrimaryChange returns nil when this status does not apply or
// when unit state is unknown (that is reported, with exit 3, by the caller's
// unit step).
func observeFreshPrimaryChange(ctx context.Context, readers dnsSwitchStatusReaders, root string,
	owner servicemutationledger.FileOwner, policy dnsengineartifact.JournalPolicy,
	e dnsenginerecovery.SwitchEvidence, units []dnsenginerecovery.NativeUnitObservation, unitErr error,
) *freshPrimaryChangeStatus {
	if readers.freshPrimaryChange == nil || unitErr != nil || !freshPrimaryReleasedV3(e) {
		return nil
	}
	seen := observedPDNSUnit(units, nil)
	if seen == nil {
		return &freshPrimaryChangeStatus{err: &dnsenginerecovery.FreshPrimaryUnknownV3{
			What: "the PowerDNS service state", Err: errors.New("pdns.service was not observed"),
		}}
	}
	pdns := dnsengineartifact.UnitSnapshot{
		Name: seen.Name, LoadState: seen.LoadState, ActiveState: seen.ActiveState, UnitFileState: seen.UnitFileState,
	}
	change, err := readers.freshPrimaryChange(ctx, root, owner, policy, e, pdns)
	return &freshPrimaryChangeStatus{change: change, err: err}
}

// replacesGuidance reports whether this observation's text replaces the
// generic owner-command and released-journal lines.
func (s *freshPrimaryChangeStatus) replacesGuidance(e dnsenginerecovery.SwitchEvidence) bool {
	if s == nil {
		return false
	}
	if s.err != nil || s.change.Changed() {
		return true
	}
	_, recorded := dnsenginerecovery.RecordedFreshPrimaryOwnerChangeV3(e.AcceptedJob.ErrorMessage)
	return s.change.Compared && recorded
}

// text is the guidance line (newline-terminated) for this observation, or
// "" when the generic lines stay and nothing is added.
func (s *freshPrimaryChangeStatus) text(e dnsenginerecovery.SwitchEvidence, policy dnsengineartifact.JournalPolicy, root string) string {
	if s == nil {
		return ""
	}
	request, phase := e.Observation.RequestID, e.Journal.Phase
	rerun := "/usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id " + request
	if s.err != nil {
		what := "one of the things the install wrote"
		var unknown *dnsenginerecovery.FreshPrimaryUnknownV3
		if errors.As(s.err, &unknown) && unknown.What != "" {
			what = unknown.What
		}
		return fmt.Sprintf("The Agent held the first install of PowerDNS as the paired primary (journal phase %s) and kept its journal, which blocks new DNS changes; other server changes can continue. "+
			"This check could not tell whether something is still not as the install wrote it: %s could not be compared with what the install wrote (the reason is printed separately). Nothing was concluded, and this status check changed nothing. "+
			"Next step, for the server owner: rerun %s in a moment (a busy PowerDNS database or a file being written can cause this). If the result stays unknown, keep the journal and contact support with request id %s. The Agent's own reason is this operation's message in CelikPanel.\n",
			phase, what, rerun, request)
	}
	route := "the journal records no PowerDNS start"
	resume := "then re-checks this install and undoes it by itself when it proves that PowerDNS never started"
	if !s.change.Prestart {
		route = "PowerDNS may have started, so CelikPanel only completes this install and never removes it"
		resume = "then re-checks PowerDNS and continues this same install forward"
	}
	recorded, owner := dnsenginerecovery.RecordedFreshPrimaryOwnerChangeV3(e.AcceptedJob.ErrorMessage)
	if s.change.Changed() {
		reason := "; the Agent's reason is this operation's message in CelikPanel"
		if owner {
			reason = " because " + recorded + " was not as the install wrote it"
		}
		text := fmt.Sprintf("The Agent held the first install of PowerDNS as the paired primary (journal phase %s; %s)%s. This check finds now: %s. "+
			"CelikPanel kept that change and neither continued nor undid the install; the journal and the PowerDNS files are kept and block new DNS changes, and other server changes can continue. "+
			"Next step, for the server owner: either undo that change so it is again exactly as the install wrote it, rerun %s to confirm that no difference remains, and restart the Agent (systemctl restart celikpanel-agent), which %s; "+
			"or keep the change, keep the journal and contact support with request id %s, because CelikPanel will not overwrite it.",
			phase, route, reason, freshPrimaryChangeFindings(s.change, policy, root), rerun, resume, request)
		if s.change.StateRecord || s.change.OwnershipRecord {
			text += " Do not edit CelikPanel's records by hand to make them match; if you did not change them, contact support."
		}
		return text + " This status check changed nothing.\n"
	}
	if !s.change.Compared {
		return ""
	}
	if !owner {
		return "This check found the PowerDNS configuration, the database and CelikPanel's DNS records as the install wrote them; the reason the Agent held this install follows.\n"
	}
	text := fmt.Sprintf("The Agent held the first install of PowerDNS as the paired primary (journal phase %s; %s) because %s was not as the install wrote it. "+
		"This check now finds the PowerDNS configuration, the database and CelikPanel's DNS records as the install wrote them, so that change is no longer present. "+
		"The install has not continued or been undone yet; its journal still blocks new DNS changes. "+
		"Next step, for the server owner: restart the Agent (systemctl restart celikpanel-agent), which %s.",
		phase, route, recorded, resume)
	if s.change.Prestart && freshPDNSPrestartV3OwnerRecoveryCandidate(e) {
		text += fmt.Sprintf(" Without the Agent, /usr/libexec/celikpanel/recovery %s --request-id %s restores the state before the install; it proves again that PowerDNS never started and refuses a started or changed target.",
			ownerPDNSFreshPrestartV3Command, request)
	}
	return text + fmt.Sprintf(" If the Agent holds the install again, rerun %s. This status check changed nothing.\n", rerun)
}

// freshPrimaryChangeFindings names where each difference is, by path.
func freshPrimaryChangeFindings(c dnsenginerecovery.FreshPrimaryChangeV3, policy dnsengineartifact.JournalPolicy, root string) string {
	var found []string
	switch len(c.ConfigPaths) {
	case 0:
	case 1:
		found = append(found, "the PowerDNS configuration file "+c.ConfigPaths[0]+" is not as the install wrote it")
	default:
		found = append(found, "the PowerDNS configuration files "+strings.Join(c.ConfigPaths, ", ")+" are not as the install wrote them")
	}
	if c.Database {
		if c.Prestart {
			found = append(found, "the PowerDNS database files ("+policy.PDNSDatabasePath+" or the copy the install staged) are not as the install wrote them")
		} else {
			found = append(found, "the PowerDNS database "+policy.PDNSDatabasePath+" holds content that neither the install nor PowerDNS's own start-up wrote (it is not shown here)")
		}
	}
	if c.StateRecord {
		if c.Prestart {
			found = append(found, "CelikPanel's DNS state record "+policy.StatePath+" exists, although the install writes it only after PowerDNS has started")
		} else {
			found = append(found, "CelikPanel's DNS state record "+policy.StatePath+" is not as the install wrote it")
		}
	}
	if c.OwnershipRecord {
		found = append(found, "CelikPanel's PowerDNS ownership record "+dnsenginerecovery.FreshPrimaryOwnershipRecordPathV3(root)+" is not as the install wrote it")
	}
	return strings.Join(found, "; ")
}
