//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

func parseDNSSwitchStatusArgs(args []string) (quiesced bool, requestID string, valid bool) {
	if len(args) == 0 || args[0] != "dns-switch-status" {
		return false, "", false
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--quiesced":
			if quiesced {
				return false, "", false
			}
			quiesced = true
		case "--request-id":
			if requestID != "" || i+1 >= len(args) || !servicemutationledger.ValidIdentity(args[i+1]) {
				return false, "", false
			}
			i++
			requestID = args[i]
		default:
			return false, "", false
		}
	}

	return quiesced, requestID, true
}

// readJournalAbsentDNSJob reports only the status of an exact, well-formed
// ledger job. It never infers the native DNS result from a missing journal.
// The quiesced caller holds release and host locks. Recorded-only status does
// not; it uses the same repeated secure reads for a point-in-time receipt.
func readJournalAbsentDNSJob(ctx context.Context, stateRoot string, owner servicemutationledger.FileOwner, requestID string) (string, error) {
	ledger, err := readJournalAbsentDNSLedger(ctx, stateRoot, owner, requestID)
	if err != nil {
		return "", err
	}
	return ledger.Jobs[requestID].Status, nil
}

// recordedDNSJobVerdict returns only an exact terminal ledger observation. It
// cannot prove native DNS state, grant recovery authority, or hide an active job.
func recordedDNSJobVerdict(ledger servicemutationledger.Ledger, requestID string) (string, error) {
	if ledger.ActiveRequestID != "" {
		return "", errors.New("a host mutation is active in the ledger")
	}
	job := ledger.Jobs[requestID]
	if job == nil || (job.Status != servicemutationledger.StatusFailed && job.Status != servicemutationledger.StatusSucceeded) {
		return "", errors.New("the exact DNS switch has no terminal ledger result")
	}
	if job.Status == servicemutationledger.StatusFailed &&
		job.ErrorCode == "dns_engine_switch_rolled_back_by_owner_recovery" &&
		job.ErrorMessage == "The interrupted DNS engine switch was rolled back to the verified previous state." {
		return "The original switch failed; owner recovery recorded a rollback to the verified previous state.", nil
	}
	return "The original switch has terminal status " + job.Status + ".", nil
}

// readJournalAbsentDNSLedger keeps the same secured, stable read used by status.
// It reports the canonical ledger only; callers must separately classify its
// exact terminal verdict and cannot infer native recovery from journal absence.
func readJournalAbsentDNSLedger(ctx context.Context, stateRoot string, owner servicemutationledger.FileOwner, requestID string) (servicemutationledger.Ledger, error) {
	if !servicemutationledger.ValidIdentity(requestID) {
		return servicemutationledger.Ledger{}, errors.New("invalid DNS request identity")
	}
	ledger, err := readStableJournalFreeDNSLedger(ctx, stateRoot, owner)
	if err != nil {
		return servicemutationledger.Ledger{}, err
	}
	if ledger.ActiveRequestID != "" && ledger.ActiveRequestID != requestID {
		return servicemutationledger.Ledger{}, errors.New("another host mutation is active in the ledger")
	}
	job := ledger.Jobs[requestID]
	if job == nil || job.Kind != "dns_engine_switch" {
		return servicemutationledger.Ledger{}, errors.New("exact DNS switch job is absent from the mutation ledger")
	}
	id := dnsengineartifact.SwitchIdentity{
		RequestID: job.RequestID,
		OwnerID:   job.OwnerID,
		Target:    transport.DNSEngine(job.Target),
		Qualifier: job.PackageName,
	}
	if err := id.Validate(); err != nil {
		return servicemutationledger.Ledger{}, errors.New("exact DNS switch job has an invalid operation identity")
	}
	return ledger, nil
}

// readStableJournalFreeDNSLedger proves no DNS switch journal is present,
// reads the canonical ledger twice with identical bytes and proves the journal
// is still absent. It is a point-in-time observation, never recovery authority.
func readStableJournalFreeDNSLedger(ctx context.Context, stateRoot string, owner servicemutationledger.FileOwner) (servicemutationledger.Ledger, error) {
	journalPath := filepath.Join(stateRoot, "dns-engine-switch-journal.json")
	ledgerPath := filepath.Join(stateRoot, "service-mutations.json")
	journal, present, err := servicemutationledger.ReadFile(journalPath, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil {
		return servicemutationledger.Ledger{}, fmt.Errorf("verify absent DNS switch journal: %w", err)
	}
	if present || len(journal) != 0 {
		return servicemutationledger.Ledger{}, errors.New("DNS switch journal is present")
	}
	if err := ctx.Err(); err != nil {
		return servicemutationledger.Ledger{}, err
	}
	raw, present, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
	if err != nil {
		return servicemutationledger.Ledger{}, fmt.Errorf("read exact DNS mutation ledger: %w", err)
	}
	if !present {
		return servicemutationledger.Ledger{}, errors.New("DNS mutation ledger is absent")
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		return servicemutationledger.Ledger{}, fmt.Errorf("decode exact DNS mutation ledger: %w", err)
	}
	again, againPresent, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
	if err != nil || !againPresent || !bytes.Equal(raw, again) {
		return servicemutationledger.Ledger{}, errors.New("DNS mutation ledger changed during observation")
	}
	_, journalPresent, err := servicemutationledger.ReadFile(journalPath, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil || journalPresent {
		return servicemutationledger.Ledger{}, errors.New("DNS switch journal changed during observation")
	}
	if err := ctx.Err(); err != nil {
		return servicemutationledger.Ledger{}, err
	}
	return ledger, nil
}

// journalFreeAgentReleasedDNSJob reports whether a ledger read with no DNS
// switch journal present holds, for requestID, exactly the Agent's deliberate
// lease release for the given target engine. The stored message describes the
// moment of release; read beside a retired journal it is historical, so
// callers print releasedDNSSwitchReconciledStatus instead of "unknown".
func journalFreeAgentReleasedDNSJob(ledger servicemutationledger.Ledger, requestID string, target transport.DNSEngine) bool {
	job := ledger.Jobs[requestID]
	return job != nil && job.RequestID == requestID &&
		(target == "" || job.Target == string(target)) &&
		dnsenginerecovery.AgentDeliberateReleaseJob(*job)
}

// agentReleasedDNSRequests lists, in order, every journal-free exact
// deliberate release in the ledger.
func agentReleasedDNSRequests(ledger servicemutationledger.Ledger) []string {
	var requests []string
	for requestID := range ledger.Jobs {
		if journalFreeAgentReleasedDNSJob(ledger, requestID, "") {
			requests = append(requests, requestID)
		}
	}
	sort.Strings(requests)
	return requests
}

// releasedDNSSwitchReconciledStatus is the read-time status text for a DNS
// switch the Agent released and whose journal has since been retired. No
// durable state records which actor retired it, so the text says so.
func releasedDNSSwitchReconciledStatus(requestID string) string {
	return "DNS switch request " + requestID + " was interrupted and the Agent could not recover it automatically when it restarted, so it released the lease. " +
		"The switch has since been reconciled and its journal is retired, either by the owner recovery command or by a later Agent start; this evidence cannot tell which. " +
		"This request no longer blocks DNS changes. The current DNS engine and its health are shown by the panel's DNS engine status, not by this record. This status check started nothing."
}

// errDNSInverseReleasedReconciled means an owner inverse command found no
// journal for its request and the ledger still holds the Agent's deliberate
// release: the operation was already reconciled and nothing was changed.
var errDNSInverseReleasedReconciled = errors.New("the Agent-released DNS switch is already reconciled and its journal is retired")

func releasedDNSInverseReconciledOutcome(requestID string) error {
	return fmt.Errorf("%w: request %s; no change was made", errDNSInverseReleasedReconciled, requestID)
}

// releasedDNSInverseReconciledText is the owner command's answer to a re-run
// after that reconciliation. The request is complete, so the command exits 0;
// the text still says that current DNS health is not asserted.
func releasedDNSInverseReconciledText(lang, requestID string) string {
	return translated(lang,
		"Request "+requestID+" is already reconciled: the Agent released this interrupted DNS switch and its journal has since been retired, by this command or by a later Agent start; the evidence cannot tell which. Nothing was changed now. This request no longer blocks DNS changes. This command does not check current DNS health. Before another switch, check the current DNS engine and authoritative DNS answers in the panel.",
		requestID+" işlemi zaten sonuçlanmış: Agent yarım kalan bu DNS geçişini bırakmıştı ve günlüğü daha sonra kaldırıldı. Bunu bu komutun mu yoksa Agent'ın sonraki açılışının mı yaptığı kayıtlardan anlaşılamıyor. Şimdi hiçbir şey değiştirilmedi. Bu işlem artık DNS değişikliklerini engellemiyor. Bu komut DNS'in şu anki sağlığını kontrol etmez. Yeni bir geçişten önce panelde güncel DNS motorunu ve yetkili DNS yanıtlarını kontrol edin.")
}

// terminalDNSInverseVerdictText is the owner command's answer to a re-run
// after an earlier owner recovery run recorded the exact rollback verdict for
// this request and retired its journal. The verdict proves what was verified
// then; it does not prove current DNS health.
func terminalDNSInverseVerdictText(lang, requestID string) string {
	return translated(lang,
		"Request "+requestID+" is already complete: an earlier owner recovery run rolled it back, recorded the verified rollback verdict and retired its journal. Nothing was changed now. This request no longer blocks DNS changes. This command does not check current DNS health: the previous DNS engine was verified when that rollback ran, not now. Before another switch, check the current DNS engine and authoritative DNS answers in the panel.",
		requestID+" işlemi zaten tamamlanmış: sahibin daha önce çalıştırdığı kurtarma komutu işlemi geri aldı, doğrulanmış geri alma sonucunu kaydetti ve günlüğü kaldırdı. Şimdi hiçbir şey değiştirilmedi. Bu işlem artık DNS değişikliklerini engellemiyor. Bu komut DNS'in şu anki sağlığını kontrol etmez: önceki DNS motoru o geri alma sırasında doğrulandı, şimdi değil. Yeni bir geçişten önce panelde güncel DNS motorunu ve yetkili DNS yanıtlarını kontrol edin.")
}

// writeOwnCompletedPDNSInverse is writeCompletedDNSInverse for the owner
// command that never admits the Agent's release (recover-dns-pdns-target-staged;
// recover-dns-pdns-fresh-prestart admits it since the Agent's own V3 pre-start
// inverse was added). Only an earlier owner recovery run's
// exact rollback verdict for this request, with the journal retired, is their
// own evidence of completion; the Agent's reconciled release keeps the
// caller's refusal.
func writeOwnCompletedPDNSInverse(err error, lang, requestID string, out io.Writer) (int, bool) {
	if !errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
		return 0, false
	}
	return writeCompletedDNSInverse(err, lang, requestID, out)
}

// writeCompletedDNSInverse answers an owner inverse whose request is already
// complete: the Agent's reconciled deliberate release or an earlier owner run's
// exact terminal verdict. Both exit 0 with the text on stdout. Any other error
// is not handled here and keeps the caller's refusal or unknown result.
func writeCompletedDNSInverse(err error, lang, requestID string, out io.Writer) (int, bool) {
	var text string
	switch {
	case errors.Is(err, errDNSInverseReleasedReconciled):
		text = releasedDNSInverseReconciledText(lang, requestID)
	case errors.Is(err, errBINDInverseTerminalLedgerObserved), errors.Is(err, errPDNSInverseTerminalLedgerObserved):
		text = terminalDNSInverseVerdictText(lang, requestID)
	default:
		return 0, false
	}
	if _, writeErr := fmt.Fprintln(out, text); writeErr != nil {
		return exitOutput, true
	}
	return exitOK, true
}
