//go:build linux

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
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
