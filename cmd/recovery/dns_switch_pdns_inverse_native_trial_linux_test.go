//go:build linux && dns_native_inverse_trial

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// This test is compiled only for the disposable native DNS kill-matrix guest.
// The installed recovery binary and ordinary package tests have no such entry.
const (
	pdnsInverseTrialCell              = "pdns-adopt__rolling-back__after-write__standalone__peer-reachable"
	pdnsInverseTrialArgv              = "/var/lib/celikpanel-dns-kill-matrix/controller-argv.json"
	pdnsInverseTrialEnv               = "CELIKPANEL_NATIVE_PDNS_INVERSE_REQUEST_ID"
	pdnsInverseTrialKillEnv           = "CELIKPANEL_NATIVE_PDNS_INVERSE_KILL_AFTER"
	pdnsInverseTrialExpectTerminalEnv = "CELIKPANEL_NATIVE_PDNS_INVERSE_EXPECT_TERMINAL_LEDGER"
	pdnsInverseTrialFixtureRoot       = "/var/lib/celikpanel-dns-kill-matrix"
)

func verifyPDNSInverseTrialArgv(raw []byte, requestID string) error {
	if !servicemutationledger.ValidIdentity(requestID) {
		return errors.New("native inverse trial requires an exact request ID")
	}
	var argv []string
	if err := json.Unmarshal(raw, &argv); err != nil || len(argv) < 5 || argv[0] != "/opt/celikpanel/libexec/dns-kill-run-cell.py" {
		return errors.New("native inverse trial has no valid prepared controller argv")
	}
	values := map[string]string{}
	for i := 1; i < len(argv); i++ {
		if argv[i] != "--cell-id" && argv[i] != "--request-id" {
			continue
		}
		if _, exists := values[argv[i]]; exists || i+1 >= len(argv) || strings.HasPrefix(argv[i+1], "--") {
			return errors.New("native inverse trial has ambiguous prepared identity")
		}
		values[argv[i]] = argv[i+1]
		i++
	}
	if values["--cell-id"] != pdnsInverseTrialCell || values["--request-id"] != requestID {
		return errors.New("native inverse trial belongs to another cell or request")
	}
	return nil
}

func selectedPDNSInverseTrialKillEffect(value string) (pdnsInverseDurableEffect, error) {
	switch pdnsInverseDurableEffect(value) {
	case "":
		return "", nil
	case pdnsInverseReceiptRemoved:
		return "", errors.New("this rolling-back adoption cell has no target receipt to remove; select checkpoint-published")
	case pdnsInverseCheckpointPublished, pdnsInverseVerdictPublished, pdnsInverseJournalRetired:
		return pdnsInverseDurableEffect(value), nil
	default:
		return "", fmt.Errorf("unsupported native inverse kill effect %q", value)
	}
}

func verifyPDNSInverseRetiredKillMarker(raw []byte, requestID string) error {
	fields := strings.Fields(string(raw))
	if len(fields) != 3 || fields[0] != requestID || fields[1] != string(pdnsInverseJournalRetired) {
		return errors.New("journal-retirement kill marker does not name the exact request and effect")
	}
	pid, err := strconv.ParseUint(fields[2], 10, 32)
	if err != nil || pid == 0 {
		return errors.New("journal-retirement kill marker has no valid process identity")
	}
	return nil
}
func pdnsInverseTrialKillMarker(requestID string, effect pdnsInverseDurableEffect) string {
	return filepath.Join(pdnsInverseTrialFixtureRoot, "inverse-kill-"+requestID+"-"+string(effect)+".marker")
}

func killNativePDNSInverseAfterEffect(t *testing.T, requestID string, effect pdnsInverseDurableEffect) {
	t.Helper()
	marker := pdnsInverseTrialKillMarker(requestID, effect)
	file, err := os.OpenFile(marker, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatalf("native inverse effect completed but its test kill marker could not be created: %v", err)
	}
	if _, err := fmt.Fprintf(file, "%s %s %d\n", requestID, effect, os.Getpid()); err != nil {
		t.Fatalf("native inverse kill marker write failed: %v", err)
	}
	if err := file.Sync(); err != nil {
		t.Fatalf("native inverse kill marker sync failed: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("native inverse kill marker close failed: %v", err)
	}
	directory, err := os.Open(pdnsInverseTrialFixtureRoot)
	if err != nil {
		t.Fatalf("native inverse kill marker directory could not be opened: %v", err)
	}
	if err := directory.Sync(); err != nil {
		t.Fatalf("native inverse kill marker directory sync failed: %v", err)
	}
	if err := directory.Close(); err != nil {
		t.Fatalf("native inverse kill marker directory close failed: %v", err)
	}
	if err := syscall.Kill(os.Getpid(), syscall.SIGKILL); err != nil {
		t.Fatalf("native inverse SIGKILL failed: %v", err)
	}
	t.Fatal("native inverse SIGKILL unexpectedly returned")
}

func TestNativePDNSAdoptionInverseDisposableGuest(t *testing.T) {
	requestID := os.Getenv(pdnsInverseTrialEnv)
	killEffect, err := selectedPDNSInverseTrialKillEffect(os.Getenv(pdnsInverseTrialKillEnv))
	if err != nil {
		t.Fatal(err)
	}
	expectTerminalValue := os.Getenv(pdnsInverseTrialExpectTerminalEnv)
	if expectTerminalValue != "" && expectTerminalValue != "1" {
		t.Fatal("native inverse terminal expectation must be exactly 1")
	}
	expectTerminal := expectTerminalValue == "1"
	if expectTerminal && killEffect != "" {
		t.Fatal("native inverse terminal verification cannot start a second kill")
	}
	if requestID == "" {
		if killEffect != "" || expectTerminal {
			t.Fatal("native inverse kill or terminal verification requires the exact request ID")
		}
		t.Skip("requires an explicitly selected disposable DNS native trial")
	}
	if !servicemutationledger.ValidIdentity(requestID) || os.Geteuid() != 0 {
		t.Fatal("native inverse trial requires root and an exact request ID")
	}
	// The fixture script writes this file as root:root in a root:root 0700
	// directory. Its root:celikpanel controller *process* has a different GID.
	// ReadFile verifies those exact file/directory owners and modes, single
	// link, no symlinks and stable bytes. This test creates no fixture authority.
	raw, present, err := servicemutationledger.ReadFile(
		pdnsInverseTrialArgv, 32*1024, servicemutationledger.FileOwner{UID: 0, GID: 0},
	)
	if err != nil || !present {
		t.Fatalf("native inverse trial lacks its secured QEMU fixture: %v", err)
	}
	if err := verifyPDNSInverseTrialArgv(raw, requestID); err != nil {
		t.Fatal(err)
	}
	if expectTerminal {
		marker, present, err := servicemutationledger.ReadFile(
			pdnsInverseTrialKillMarker(requestID, pdnsInverseJournalRetired),
			256, servicemutationledger.FileOwner{UID: 0, GID: 0},
		)
		if err != nil || !present {
			t.Fatalf("journal-retirement SIGKILL marker is absent or unsafe: %v", err)
		}
		if err := verifyPDNSInverseRetiredKillMarker(marker, requestID); err != nil {
			t.Fatal(err)
		}
	}
	var afterEffect pdnsInverseAfterEffect
	if killEffect != "" {
		marker := pdnsInverseTrialKillMarker(requestID, killEffect)
		if _, err := os.Lstat(marker); err == nil || !os.IsNotExist(err) {
			t.Fatalf("native inverse kill marker already exists or cannot be inspected: %v", err)
		}
		afterEffect = func(effect pdnsInverseDurableEffect) {
			if effect == killEffect {
				killNativePDNSInverseAfterEffect(t, requestID, effect)
			}
		}
	}
	err = completeInstalledPDNSAdoptionInverseWithAfterEffect(
		context.Background(), requestID, afterEffect,
	)
	if expectTerminal {
		if !errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
			t.Fatalf("journal-retired retry lacks its exact historical verdict: %v", err)
		}
		t.Log("same-request terminal ledger is proved; current native PowerDNS remains a separate check")
		return
	}
	if err != nil {
		t.Fatalf("same-request independent PowerDNS adoption inverse is unverified: %v", err)
	}
	if killEffect != "" {
		t.Fatalf("selected native inverse kill effect %q was not reached", killEffect)
	}
}

func TestNativePDNSAdoptionInverseTrialKillSelectorGate(t *testing.T) {
	for _, value := range []pdnsInverseDurableEffect{
		pdnsInverseCheckpointPublished, pdnsInverseVerdictPublished,
		pdnsInverseJournalRetired,
	} {
		selected, err := selectedPDNSInverseTrialKillEffect(string(value))
		if err != nil || selected != value {
			t.Fatalf("exact effect %q was rejected: selected=%q err=%v", value, selected, err)
		}
	}
	selected, err := selectedPDNSInverseTrialKillEffect("")
	if err != nil || selected != "" {
		t.Fatalf("empty selector did not leave the kill hook disabled: %q %v", selected, err)
	}
	for _, value := range []string{"before-write", "receipt-removed", "receipt-removed ", "../receipt-removed", "all"} {
		if _, err := selectedPDNSInverseTrialKillEffect(value); err == nil {
			t.Fatalf("unexpected kill selector was admitted: %q", value)
		}
	}
}
func TestNativePDNSAdoptionInverseRetiredKillMarkerGate(t *testing.T) {
	request := strings.Repeat("a", 32)
	if err := verifyPDNSInverseRetiredKillMarker(
		[]byte(request+" journal-retired 1234\n"), request,
	); err != nil {
		t.Fatalf("exact marker was rejected: %v", err)
	}
	for _, raw := range []string{
		request + " verdict-published 1234\n",
		strings.Repeat("b", 32) + " journal-retired 1234\n",
		request + " journal-retired 0\n",
		request + " journal-retired not-a-pid\n",
		request + " journal-retired 1234 extra\n",
	} {
		if err := verifyPDNSInverseRetiredKillMarker([]byte(raw), request); err == nil {
			t.Fatalf("unsafe or foreign marker was accepted: %q", raw)
		}
	}
}
func TestNativePDNSAdoptionInverseTrialIdentityGate(t *testing.T) {
	requestID := strings.Repeat("a", 32)
	valid := []string{
		"/opt/celikpanel/libexec/dns-kill-run-cell.py",
		"--cell-id", pdnsInverseTrialCell,
		"--request-id", requestID,
	}
	encode := func(argv []string) []byte {
		t.Helper()
		raw, err := json.Marshal(argv)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	if err := verifyPDNSInverseTrialArgv(encode(valid), requestID); err != nil {
		t.Fatalf("exact fixture rejected: %v", err)
	}
	for _, altered := range [][]string{
		{valid[0], "--cell-id", "pdns-adopt__rolled-back__after-write__standalone__peer-reachable", "--request-id", requestID},
		{valid[0], "--cell-id", pdnsInverseTrialCell, "--request-id", strings.Repeat("b", 32)},
		{valid[0], "--cell-id", pdnsInverseTrialCell, "--request-id", requestID, "--request-id", requestID},
		{"/bin/sh", "--cell-id", pdnsInverseTrialCell, "--request-id", requestID},
	} {
		if err := verifyPDNSInverseTrialArgv(encode(altered), requestID); err == nil {
			t.Fatalf("foreign or ambiguous fixture admitted: %v", altered)
		}
	}
	if err := verifyPDNSInverseTrialArgv(encode(valid), "invalid"); err == nil {
		t.Fatal("invalid exact request was admitted")
	}
}
