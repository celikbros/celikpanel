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
	if requestID != "" && !quiesced {
		return false, "", false
	}
	return quiesced, requestID, true
}

// readJournalAbsentDNSJob reports only the status of an exact, well-formed
// ledger job. It never infers the native DNS result from a missing journal.
// The caller holds the release and host locks; repeated secure reads reject
// an observed journal/ledger transition during this point-in-time report.
func readJournalAbsentDNSJob(ctx context.Context, stateRoot string, owner servicemutationledger.FileOwner, requestID string) (string, error) {
	if !servicemutationledger.ValidIdentity(requestID) {
		return "", errors.New("invalid DNS request identity")
	}
	journalPath := filepath.Join(stateRoot, "dns-engine-switch-journal.json")
	ledgerPath := filepath.Join(stateRoot, "service-mutations.json")
	journal, present, err := servicemutationledger.ReadFile(journalPath, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil {
		return "", fmt.Errorf("verify absent DNS switch journal: %w", err)
	}
	if present || len(journal) != 0 {
		return "", errors.New("DNS switch journal is present")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	raw, present, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
	if err != nil {
		return "", fmt.Errorf("read exact DNS mutation ledger: %w", err)
	}
	if !present {
		return "", errors.New("DNS mutation ledger is absent")
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		return "", fmt.Errorf("decode exact DNS mutation ledger: %w", err)
	}
	if ledger.ActiveRequestID != "" && ledger.ActiveRequestID != requestID {
		return "", errors.New("another host mutation is active in the ledger")
	}
	job := ledger.Jobs[requestID]
	if job == nil || job.Kind != "dns_engine_switch" {
		return "", errors.New("exact DNS switch job is absent from the mutation ledger")
	}
	id := dnsengineartifact.SwitchIdentity{
		RequestID: job.RequestID,
		OwnerID:   job.OwnerID,
		Target:    transport.DNSEngine(job.Target),
		Qualifier: job.PackageName,
	}
	if err := id.Validate(); err != nil {
		return "", errors.New("exact DNS switch job has an invalid operation identity")
	}
	again, againPresent, err := servicemutationledger.ReadFile(ledgerPath, servicemutationledger.MaxSize, owner)
	if err != nil || !againPresent || !bytes.Equal(raw, again) {
		return "", errors.New("DNS mutation ledger changed during observation")
	}
	_, journalPresent, err := servicemutationledger.ReadFile(journalPath, dnsengineartifact.SwitchJournalLimit, owner)
	if err != nil || journalPresent {
		return "", errors.New("DNS switch journal changed during observation")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return job.Status, nil
}
