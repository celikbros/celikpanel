package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

type recordedFirewallCommand struct {
	name  string
	args  []string
	stdin string
}

type fakeFirewallCommandRunner struct {
	tablePresent          bool
	oldPolicy             string
	lookPathErr           error
	listErr               error
	tableReadErr          error
	ssOut                 string
	ssErr                 error
	trustedSSHPIDs        map[int]bool
	sshVerifyErr          error
	verifiedPIDs          []int
	configuredSSHPorts    []int
	configuredSSHErr      error
	configuredSocketPorts []int
	configuredSocketErr   error
	applyErr              error
	systemctlErr          error
	commands              []recordedFirewallCommand
	outputCalls           []string
}

func (f *fakeFirewallCommandRunner) LookPath(string) (string, error) {
	if f.lookPathErr != nil {
		return "", f.lookPathErr
	}
	return "/usr/sbin/nft", nil
}

func (f *fakeFirewallCommandRunner) Output(name string, args ...string) ([]byte, error) {
	f.outputCalls = append(f.outputCalls, name+" "+strings.Join(args, " "))
	if name == "nft" && strings.Join(args, " ") == "list tables" {
		if f.listErr != nil {
			return nil, f.listErr
		}
		if f.tablePresent {
			return []byte("table inet " + fwTable + "\n"), nil
		}
		return nil, nil
	}
	if name == "nft" && strings.Join(args, " ") == "list table inet "+fwTable {
		if f.tableReadErr != nil {
			return nil, f.tableReadErr
		}
		if !f.tablePresent {
			return nil, errors.New("table absent")
		}
		if f.oldPolicy == "" {
			f.oldPolicy = buildFirewallRuleset(false, []int{22, 2083}, nil)
		}
		return []byte(f.oldPolicy), nil
	}
	if name == "ss" {
		if f.ssErr != nil {
			return nil, f.ssErr
		}
		if f.ssOut != "" {
			return []byte(f.ssOut), nil
		}
		return []byte("LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((sshd,pid=7,fd=3))\n"), nil
	}
	return nil, fmt.Errorf("unexpected output command: %s %s", name, strings.Join(args, " "))
}

func (f *fakeFirewallCommandRunner) CombinedOutput(name string, args []string, stdin string) ([]byte, error) {
	f.commands = append(f.commands, recordedFirewallCommand{
		name:  name,
		args:  append([]string(nil), args...),
		stdin: stdin,
	})
	if name == "nft" && f.applyErr != nil {
		return []byte("forced transaction failure"), f.applyErr
	}
	if name == "systemctl" {
		if f.systemctlErr != nil {
			return []byte("forced systemctl failure"), f.systemctlErr
		}
		return nil, nil
	}
	if strings.Join(args, " ") == "--check -f -" {
		return nil, nil
	}
	if strings.Join(args, " ") == "delete table inet "+fwTable {
		f.tablePresent = false
		f.oldPolicy = ""
		return nil, nil
	}
	if strings.Join(args, " ") == "-f -" {
		trimmed := strings.TrimSpace(stdin)
		deleteLine := "delete table inet " + fwTable
		if strings.HasPrefix(trimmed, deleteLine) && !f.tablePresent {
			return []byte("No such file or directory"), errors.New("nft transaction failed")
		}
		if i := strings.Index(stdin, "table inet "+fwTable+" {"); i >= 0 {
			f.tablePresent = true
			f.oldPolicy = stdin[i:]
		} else {
			f.tablePresent = false
			f.oldPolicy = ""
		}
		return nil, nil
	}
	return nil, fmt.Errorf("unexpected mutation command: %s %s", name, strings.Join(args, " "))
}

func (f *fakeFirewallCommandRunner) VerifySSHDProcess(pid int) error {
	f.verifiedPIDs = append(f.verifiedPIDs, pid)
	if f.sshVerifyErr != nil {
		return f.sshVerifyErr
	}
	if f.trustedSSHPIDs == nil {
		if pid == 7 {
			return nil
		}
	} else if f.trustedSSHPIDs[pid] {
		return nil
	}
	return fmt.Errorf("PID %d is not a trusted sshd", pid)
}

func (f *fakeFirewallCommandRunner) ConfiguredSSHPorts() ([]int, error) {
	if f.configuredSSHErr != nil {
		return nil, f.configuredSSHErr
	}
	if f.configuredSSHPorts == nil {
		return []int{22}, nil
	}
	return append([]int(nil), f.configuredSSHPorts...), nil
}

func (f *fakeFirewallCommandRunner) ConfiguredSSHSocketPorts() ([]int, error) {
	if f.configuredSocketErr != nil {
		return nil, f.configuredSocketErr
	}
	return append([]int(nil), f.configuredSocketPorts...), nil
}

type fakeFirewallStateStore struct {
	data      []byte
	exists    bool
	loadErr   error
	saveErr   error
	removeErr error
	saves     int
	removes   int
}

func (s *fakeFirewallStateStore) Load() ([]byte, bool, error) {
	return append([]byte(nil), s.data...), s.exists, s.loadErr
}

func (s *fakeFirewallStateStore) Save(data []byte) error {
	s.saves++
	if s.saveErr != nil {
		return s.saveErr
	}
	s.data = append([]byte(nil), data...)
	s.exists = true
	return nil
}

func (s *fakeFirewallStateStore) Remove() error {
	s.removes++
	if s.removeErr != nil {
		return s.removeErr
	}
	s.data = nil
	s.exists = false
	return nil
}

func TestApplyFirewallFailurePreservesExistingTable(t *testing.T) {
	runner := &fakeFirewallCommandRunner{
		tablePresent: true,
		oldPolicy:    "known-good-old-policy",
		applyErr:     errors.New("forced nft failure"),
	}
	var resp FirewallStatusResponse

	if err := applyFirewallWithRunner(runner, &ApplyFirewallRequest{
		Enabled:  true,
		TCPPorts: []int{80, 443},
	}, &resp); err != nil {
		t.Fatalf("applyFirewallWithRunner error = %v", err)
	}

	if !strings.Contains(resp.Error, "nft apply failed") {
		t.Fatalf("response error = %q, want nft apply failure", resp.Error)
	}
	if !runner.tablePresent || runner.oldPolicy != "known-good-old-policy" {
		t.Fatalf("failed replacement changed old policy: present=%v policy=%q", runner.tablePresent, runner.oldPolicy)
	}
	if len(runner.commands) != 1 {
		t.Fatalf("mutation commands = %d, want one atomic nft batch", len(runner.commands))
	}
	call := runner.commands[0]
	if call.name != "nft" || strings.Join(call.args, " ") != "-f -" {
		t.Fatalf("mutation command = %s %v, want nft -f -", call.name, call.args)
	}
	if !strings.Contains(call.stdin, "delete table inet "+fwTable+"\n") ||
		!strings.Contains(call.stdin, "table inet "+fwTable+" {") {
		t.Fatalf("replacement batch does not contain delete and replacement together:\n%s", call.stdin)
	}
}

func TestApplyFirewallDiscoveryFailureDoesNotMutate(t *testing.T) {
	runner := &fakeFirewallCommandRunner{
		tablePresent: true,
		oldPolicy:    "known-good-old-policy",
		listErr:      errors.New("forced nft discovery failure"),
	}
	var resp FirewallStatusResponse

	if err := applyFirewallWithRunner(runner, &ApplyFirewallRequest{Enabled: true}, &resp); err != nil {
		t.Fatalf("applyFirewallWithRunner error = %v", err)
	}

	if !strings.Contains(resp.Error, "nft table discovery failed") {
		t.Fatalf("response error = %q, want discovery failure", resp.Error)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("mutation commands = %d after discovery failure, want 0", len(runner.commands))
	}
	if !runner.tablePresent || runner.oldPolicy != "known-good-old-policy" {
		t.Fatal("discovery failure changed the existing policy")
	}
}

func TestFirewallStatusDistinguishesAbsentTableFromCommandFailure(t *testing.T) {
	firewallLastRestoreError = ""
	store := &fakeFirewallStateStore{}

	absent := &fakeFirewallCommandRunner{}
	var absentResp FirewallStatusResponse
	if err := firewallStatusWithRunnerAndStore(absent, store, &absentResp); err != nil {
		t.Fatal(err)
	}
	if !absentResp.EngineAvailable || absentResp.Enabled || absentResp.Error != "" {
		t.Fatalf("absent status = %+v, want available engine and clean disabled state", absentResp)
	}

	broken := &fakeFirewallCommandRunner{listErr: errors.New("operation not permitted")}
	var brokenResp FirewallStatusResponse
	if err := firewallStatusWithRunnerAndStore(broken, store, &brokenResp); err != nil {
		t.Fatal(err)
	}
	if brokenResp.Enabled || !strings.Contains(brokenResp.Error, "operation not permitted") {
		t.Fatalf("broken status = %+v, want visible nft command error", brokenResp)
	}
}

func TestFirewallStatusKeepsEnabledWhenTableReadFails(t *testing.T) {
	firewallLastRestoreError = ""
	runner := &fakeFirewallCommandRunner{
		tablePresent: true,
		tableReadErr: errors.New("permission denied"),
	}
	var resp FirewallStatusResponse
	if err := firewallStatusWithRunnerAndStore(runner, &fakeFirewallStateStore{}, &resp); err != nil {
		t.Fatal(err)
	}
	if !resp.Enabled || !strings.Contains(resp.Error, "permission denied") {
		t.Fatalf("status = %+v, want enabled plus visible table-read error", resp)
	}
}

func TestFirewallStatusReportsPersistenceStateAndSnapshotVersion(t *testing.T) {
	previousRestore, previousPersistence := firewallLastRestoreError, firewallLastPersistenceError
	firewallLastRestoreError, firewallLastPersistenceError = "", ""
	t.Cleanup(func() {
		firewallLastRestoreError, firewallLastPersistenceError = previousRestore, previousPersistence
	})

	for _, tc := range []struct {
		name        string
		live        bool
		store       *fakeFirewallStateStore
		wantState   string
		wantVersion int
	}{
		{name: "disabled", store: &fakeFirewallStateStore{}, wantState: firewallPersistenceDisabled},
		{name: "live unsaved", live: true, store: &fakeFirewallStateStore{}, wantState: firewallPersistenceMissing},
		{name: "live saved", live: true, store: &fakeFirewallStateStore{data: encodeFirewallSnapshot([]int{2083}, nil, []int{22}), exists: true}, wantState: firewallPersistenceReady, wantVersion: 2},
		{name: "saved but not live", store: &fakeFirewallStateStore{data: encodeFirewallSnapshot([]int{2083}, nil, []int{22}), exists: true}, wantState: firewallPersistenceStale, wantVersion: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeFirewallCommandRunner{tablePresent: tc.live}
			var resp FirewallStatusResponse
			if err := firewallStatusWithRunnerAndStore(runner, tc.store, &resp); err != nil {
				t.Fatal(err)
			}
			if resp.PersistenceState != tc.wantState || resp.SnapshotVersion != tc.wantVersion {
				t.Fatalf("status = %+v, want persistence %s version %d", resp, tc.wantState, tc.wantVersion)
			}
		})
	}
}

func TestApplyFirewallFailsClosedWhenSSHDiscoveryFailsOrIsEmpty(t *testing.T) {
	for _, tc := range []struct {
		name  string
		ssOut string
		ssErr error
	}{
		{name: "command error", ssErr: errors.New("ss permission denied")},
		{name: "empty result", ssOut: "LISTEN 0 128 0.0.0.0:80 0.0.0.0:* users:((nginx,pid=8,fd=3))\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeFirewallCommandRunner{ssOut: tc.ssOut, ssErr: tc.ssErr}
			store := &fakeFirewallStateStore{}
			var resp FirewallStatusResponse
			if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{Enabled: true, Persist: true}, &resp); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(resp.Error, "SSH listener discovery failed") {
				t.Fatalf("response error = %q", resp.Error)
			}
			if len(runner.commands) != 0 || store.saves != 0 {
				t.Fatalf("SSH discovery failure mutated firewall/state: commands=%d saves=%d", len(runner.commands), store.saves)
			}
		})
	}
}

func TestDetectSSHPortsRequiresVerifiedSSHDProcess(t *testing.T) {
	t.Run("fake sshd name is rejected", func(t *testing.T) {
		runner := &fakeFirewallCommandRunner{
			ssOut:          "LISTEN 0 128 0.0.0.0:2022 0.0.0.0:* users:((sshd,pid=41,fd=3))\n",
			trustedSSHPIDs: map[int]bool{},
		}
		if _, err := detectSSHPortsWithRunner(runner); err == nil {
			t.Fatal("untrusted process named sshd was accepted")
		}
		if len(runner.verifiedPIDs) != 1 || runner.verifiedPIDs[0] != 41 {
			t.Fatalf("verified PIDs = %v, want [41]", runner.verifiedPIDs)
		}
	})

	t.Run("verified custom SSH port is accepted without trusting name", func(t *testing.T) {
		runner := &fakeFirewallCommandRunner{
			ssOut:          "LISTEN 0 128 [::]:2222 [::]:* users:((not-sshd,pid=73,fd=3))\n",
			trustedSSHPIDs: map[int]bool{73: true},
		}
		got, err := detectSSHPortsWithRunner(runner)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0] != 2222 {
			t.Fatalf("SSH ports = %v, want [2222]", got)
		}
	})

	t.Run("malformed or missing PID fails closed", func(t *testing.T) {
		for _, line := range []string{
			"LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((sshd,pid=oops,fd=3))\n",
			"LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((sshd,fd=3))\n",
			"LISTEN 0 128 0.0.0.0:22 0.0.0.0:* users:((sshd,pid=0,fd=3))\n",
		} {
			runner := &fakeFirewallCommandRunner{
				ssOut:          line,
				trustedSSHPIDs: map[int]bool{7: true},
			}
			if _, err := detectSSHPortsWithRunner(runner); err == nil {
				t.Fatalf("malformed listener %q was accepted", strings.TrimSpace(line))
			}
			if len(runner.verifiedPIDs) != 0 {
				t.Fatalf("verifier called for malformed listener %q: %v", strings.TrimSpace(line), runner.verifiedPIDs)
			}
		}
	})
}

func TestVerifyRootSSHDProcessRejectsCurrentNonSSHDProcess(t *testing.T) {
	if err := verifyRootSSHDProcess(os.Getpid()); err == nil {
		t.Fatal("current non-sshd test process was trusted")
	}
}

func TestExplicitSaveForRebootPersistsCustomSSHAndSyncOnlyUpdatesExistingSnapshot(t *testing.T) {
	runner := &fakeFirewallCommandRunner{
		ssOut: "LISTEN 0 128 0.0.0.0:2222 0.0.0.0:* users:((sshd,pid=7,fd=3))\n",
	}
	store := &fakeFirewallStateStore{}
	var resp FirewallStatusResponse
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: true, TCPPorts: []int{2083}, Persist: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" || !resp.Enabled || store.saves != 1 {
		t.Fatalf("explicit enable = %+v, saves=%d", resp, store.saves)
	}
	if len(runner.commands) != 2 || runner.commands[1].name != "systemctl" ||
		strings.Join(runner.commands[1].args, " ") != "enable "+firewallRestoreUnitName {
		t.Fatalf("save-for-reboot commands = %+v, want nft apply then systemctl enable", runner.commands)
	}
	policy, legacy, err := decodeFirewallSnapshot(store.data)
	if err != nil || legacy {
		t.Fatalf("decode V2 snapshot = legacy %v, error %v", legacy, err)
	}
	if fmt.Sprint(policy.TCPPorts) != "[2083]" || fmt.Sprint(policy.SSHPortsAtSave) != "[2222]" {
		t.Fatalf("snapshot did not separate requested and SSH ports: %+v", policy)
	}

	// Background sync updates the already-explicit snapshot.
	// Arka plan eşitlemesi, daha önce açıkça oluşturulmuş snapshot'ı günceller.
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: true, TCPPorts: []int{80, 2083}, Persist: false,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	policy, _, err = decodeFirewallSnapshot(store.data)
	if store.saves != 2 || err != nil || fmt.Sprint(policy.TCPPorts) != "[80 2083]" {
		t.Fatalf("sync did not update existing snapshot: saves=%d policy=%+v error=%v", store.saves, policy, err)
	}
	if len(runner.commands) != 3 || runner.commands[2].name != "nft" {
		t.Fatalf("background sync enabled the restore unit: %+v", runner.commands)
	}

	// The same sync request cannot create persistence on a transient table.
	// Aynı eşitleme isteği geçici bir tabloda kalıcılık oluşturamaz.
	transientRunner := &fakeFirewallCommandRunner{}
	transientStore := &fakeFirewallStateStore{}
	if err := applyFirewallWithRunnerAndStore(transientRunner, transientStore, &ApplyFirewallRequest{Enabled: true}, &resp); err != nil {
		t.Fatal(err)
	}
	if transientStore.saves != 0 || transientStore.exists {
		t.Fatal("background sync created a persistent firewall snapshot")
	}
	for _, call := range transientRunner.commands {
		if call.name == "systemctl" {
			t.Fatalf("background sync changed restore-unit activation: %+v", transientRunner.commands)
		}
	}
}

func TestSaveForRebootEnableFailureRollsBackFirstSnapshotAndReportsError(t *testing.T) {
	previousPersistenceError := firewallLastPersistenceError
	firewallLastPersistenceError = ""
	t.Cleanup(func() { firewallLastPersistenceError = previousPersistenceError })

	runner := &fakeFirewallCommandRunner{systemctlErr: errors.New("read-only unit directory")}
	store := &fakeFirewallStateStore{}
	var resp FirewallStatusResponse
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: true, TCPPorts: []int{2083}, Persist: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "enable firewall restore unit failed") ||
		resp.PersistenceState != firewallPersistenceUnverified || resp.PersistenceError == "" {
		t.Fatalf("enable failure response = %+v", resp)
	}
	if store.exists || store.saves != 1 || store.removes != 1 {
		t.Fatalf("failed first enable left a saved snapshot: %+v", store)
	}
	if !resp.Enabled || !runner.tablePresent {
		t.Fatal("boot-unit failure unexpectedly disabled the live firewall")
	}
	if len(runner.commands) != 2 || runner.commands[1].name != "systemctl" ||
		strings.Join(runner.commands[1].args, " ") != "enable "+firewallRestoreUnitName {
		t.Fatalf("enable failure commands = %+v", runner.commands)
	}
}

func TestSaveForRebootDoesNotEnableAfterUnverifiedDirectorySync(t *testing.T) {
	runner := &fakeFirewallCommandRunner{}
	store := &fakeFirewallStateStore{saveErr: &firewallStateCommittedError{
		operation: "save firewall state",
		err:       errors.New("forced directory sync failure"),
	}}
	var resp FirewallStatusResponse
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: true, TCPPorts: []int{2083}, Persist: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.PersistenceState != firewallPersistenceUnverified || resp.Error == "" {
		t.Fatalf("directory-sync failure response = %+v", resp)
	}
	for _, call := range runner.commands {
		if call.name == "systemctl" {
			t.Fatalf("unverified snapshot enabled boot restore: %+v", runner.commands)
		}
	}
}

func TestSnapshotSaveFailureRollsBackLivePolicyAndPreservesSnapshot(t *testing.T) {
	oldRules := buildFirewallRuleset(false, []int{22, 2083}, nil)
	oldSnapshot := []byte(oldRules)
	runner := &fakeFirewallCommandRunner{tablePresent: true, oldPolicy: oldRules}
	store := &fakeFirewallStateStore{
		data: oldSnapshot, exists: true, saveErr: errors.New("disk full"),
	}
	var resp FirewallStatusResponse
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: true, TCPPorts: []int{80, 443}, Persist: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "disk full") {
		t.Fatalf("response error = %q", resp.Error)
	}
	if !runner.tablePresent || runner.oldPolicy != oldRules {
		t.Fatalf("live rollback failed: present=%v policy=%q", runner.tablePresent, runner.oldPolicy)
	}
	if !store.exists || !bytes.Equal(store.data, oldSnapshot) {
		t.Fatalf("old snapshot changed after failed save: %q", store.data)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("mutation calls = %d, want apply then rollback", len(runner.commands))
	}
}

func TestDisableFailurePreservesLivePolicyAndSnapshot(t *testing.T) {
	oldRules := buildFirewallRuleset(false, []int{22, 2083}, nil)
	for _, tc := range []struct {
		name      string
		applyErr  error
		removeErr error
	}{
		{name: "nft delete", applyErr: errors.New("delete denied")},
		{name: "snapshot remove", removeErr: errors.New("filesystem read-only")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runner := &fakeFirewallCommandRunner{tablePresent: true, oldPolicy: oldRules, applyErr: tc.applyErr}
			store := &fakeFirewallStateStore{data: []byte(oldRules), exists: true, removeErr: tc.removeErr}
			var resp FirewallStatusResponse
			if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{Enabled: false, Persist: true}, &resp); err != nil {
				t.Fatal(err)
			}
			if resp.Error == "" {
				t.Fatal("disable failure was not reported")
			}
			if !runner.tablePresent || runner.oldPolicy != oldRules || !store.exists {
				t.Fatalf("disable failure lost old state: live=%v policy=%q snapshot=%v", runner.tablePresent, runner.oldPolicy, store.exists)
			}
			if tc.applyErr != nil && store.removes != 0 {
				t.Fatal("snapshot was removed after nft delete failed")
			}
			if tc.removeErr != nil {
				rollback := runner.commands[len(runner.commands)-1]
				if strings.Contains(rollback.stdin, "delete table inet "+fwTable) {
					t.Fatalf("rollback after a completed delete tried to delete the absent table again: %q", rollback.stdin)
				}
			}
		})
	}
}

func TestExplicitDisableRemovesOnlyOwnTableThenPersistentSnapshot(t *testing.T) {
	oldRules := buildFirewallRuleset(false, []int{22, 2083}, []int{53})
	runner := &fakeFirewallCommandRunner{tablePresent: true, oldPolicy: oldRules}
	store := &fakeFirewallStateStore{data: []byte(oldRules), exists: true}
	var resp FirewallStatusResponse
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: false, Persist: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Error != "" || resp.Enabled {
		t.Fatalf("disable response = %+v", resp)
	}
	if runner.tablePresent || store.exists || store.removes != 1 {
		t.Fatalf("disable left state behind: live=%v snapshot=%v removes=%d", runner.tablePresent, store.exists, store.removes)
	}
	if len(runner.commands) != 2 {
		t.Fatalf("mutation calls = %d, want nft delete then systemctl disable", len(runner.commands))
	}
	call := runner.commands[0]
	if call.name != "nft" || strings.Join(call.args, " ") != "delete table inet "+fwTable || call.stdin != "" {
		t.Fatalf("disable command = %+v, want deletion of CelikPanel's table only", call)
	}
	unitCall := runner.commands[1]
	if unitCall.name != "systemctl" || strings.Join(unitCall.args, " ") != "disable "+firewallRestoreUnitName {
		t.Fatalf("disable unit command = %+v", unitCall)
	}
}

func TestExplicitDisableUnitFailureRestoresLivePolicyAndSnapshot(t *testing.T) {
	previousPersistenceError := firewallLastPersistenceError
	firewallLastPersistenceError = ""
	t.Cleanup(func() { firewallLastPersistenceError = previousPersistenceError })

	oldRules := buildFirewallRuleset(false, []int{22, 2083}, []int{53})
	oldSnapshot := encodeFirewallSnapshot([]int{2083}, []int{53}, []int{22})
	runner := &fakeFirewallCommandRunner{
		tablePresent: true, oldPolicy: oldRules,
		systemctlErr: errors.New("unit links are read-only"),
	}
	store := &fakeFirewallStateStore{data: append([]byte(nil), oldSnapshot...), exists: true}
	var resp FirewallStatusResponse
	if err := applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{
		Enabled: false, Persist: true,
	}, &resp); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(resp.Error, "disable firewall restore unit failed") ||
		resp.PersistenceState != firewallPersistenceUnverified {
		t.Fatalf("unit-disable failure response = %+v", resp)
	}
	if !runner.tablePresent || runner.oldPolicy != oldRules || !store.exists || !bytes.Equal(store.data, oldSnapshot) {
		t.Fatalf("unit-disable failure lost previous state: live=%v policy=%q store=%+v", runner.tablePresent, runner.oldPolicy, store)
	}
	if len(runner.commands) != 3 || runner.commands[1].name != "systemctl" || runner.commands[2].name != "nft" {
		t.Fatalf("unit-disable rollback commands = %+v", runner.commands)
	}
}

func TestRestoreFirewallSnapshotUsesCurrentConfiguredSSHPort(t *testing.T) {
	v2 := encodeFirewallSnapshot([]int{2083}, []int{53}, []int{2222})
	runner := &fakeFirewallCommandRunner{configuredSSHPorts: []int{2022}}
	if err := restoreFirewallSnapshotLocked(runner, &fakeFirewallStateStore{data: v2, exists: true}); err != nil {
		t.Fatalf("V2 restore: %v", err)
	}
	want := buildFirewallRuleset(false, []int{2022, 2083, 2222}, []int{53})
	if len(runner.commands) != 1 || runner.commands[0].stdin != want {
		t.Fatalf("restore command = %+v, want current plus transition SSH ports", runner.commands)
	}
	for _, call := range runner.outputCalls {
		if strings.HasPrefix(call, "ss ") {
			t.Fatalf("boot restore depended on a live listener: %s", call)
		}
	}

	// A legacy ruleset remains accepted, and the current configured SSH port is
	// added conservatively because the old format cannot identify its SSH port.
	// Eski kural kümesi kabul edilmeye devam eder; eski biçim SSH portunu ayırt
	// edemediğinden güncel yapılandırılmış SSH portu temkinli biçimde eklenir.
	legacy := []byte(buildFirewallRuleset(false, []int{2083, 2222}, []int{53}))
	legacyRunner := &fakeFirewallCommandRunner{configuredSSHPorts: []int{2022}}
	if err := restoreFirewallSnapshotLocked(legacyRunner, &fakeFirewallStateStore{data: legacy, exists: true}); err != nil {
		t.Fatalf("legacy restore: %v", err)
	}
	if len(legacyRunner.commands) != 1 || legacyRunner.commands[0].stdin != want {
		t.Fatalf("legacy restore command = %+v, want conservative current SSH port", legacyRunner.commands)
	}
}

func TestRestoreFirewallSnapshotFailsClosedBeforeMutation(t *testing.T) {
	valid := encodeFirewallSnapshot([]int{2083}, nil, []int{22})
	runner := &fakeFirewallCommandRunner{configuredSSHErr: errors.New("invalid sshd configuration")}
	if err := restoreFirewallSnapshotLocked(runner, &fakeFirewallStateStore{data: valid, exists: true}); err == nil {
		t.Fatal("SSH configuration failure was accepted")
	}
	if len(runner.commands) != 0 {
		t.Fatal("SSH configuration failure reached nft mutation")
	}

	badRunner := &fakeFirewallCommandRunner{}
	legacy := []byte(buildFirewallRuleset(false, []int{22, 2083}, nil))
	bad := append(append([]byte(nil), legacy...), []byte("flush ruleset\n")...)
	if err := restoreFirewallSnapshotLocked(badRunner, &fakeFirewallStateStore{data: bad, exists: true}); err == nil {
		t.Fatal("arbitrary nft command in snapshot was accepted")
	}
	if len(badRunner.commands) != 0 {
		t.Fatal("invalid snapshot reached nft")
	}
}

func TestFirewallRestorePreflightChecksExactBatchWithoutMutation(t *testing.T) {
	runner := &fakeFirewallCommandRunner{
		configuredSSHPorts: []int{22}, configuredSocketPorts: []int{2222},
	}
	store := &fakeFirewallStateStore{
		data: encodeFirewallSnapshot([]int{2083}, []int{53}, []int{2022}), exists: true,
	}
	if err := checkFirewallRestoreLocked(runner, store); err != nil {
		t.Fatal(err)
	}
	if len(runner.commands) != 1 || strings.Join(runner.commands[0].args, " ") != "--check -f -" {
		t.Fatalf("preflight commands = %+v, want one nft --check batch", runner.commands)
	}
	want := buildFirewallRuleset(false, []int{22, 2022, 2083, 2222}, []int{53})
	if runner.commands[0].stdin != want || runner.tablePresent || store.saves != 0 || store.removes != 0 {
		t.Fatalf("preflight mutated state or checked wrong batch: command=%+v store=%+v", runner.commands[0], store)
	}
}

func TestParseSSHDConfigurationPortsIsStrict(t *testing.T) {
	got, err := parseSSHDConfigurationPorts([]byte("addressfamily any\nport 2222\nport 22\nport 2222\n"))
	if err != nil || fmt.Sprint(got) != "[22 2222]" {
		t.Fatalf("ports = %v, error = %v", got, err)
	}
	for _, bad := range []string{"", "port 0\n", "port 65536\n", "port twenty-two\n", "port 22 extra\n"} {
		if _, err := parseSSHDConfigurationPorts([]byte(bad)); err == nil {
			t.Fatalf("malformed sshd output %q was accepted", bad)
		}
	}
	if err := validateFirewallSnapshot(bytes.Repeat([]byte{'x'}, maxFirewallSnapshotSize+1)); err == nil {
		t.Fatal("oversized snapshot was accepted")
	}
}

func TestParseSSHDConfigurationPortsUsesListenAddressAndPortFallback(t *testing.T) {
	out := []byte("port 22\nport 2022\nlistenaddress 0.0.0.0:2222\nlistenaddress [::]\nlistenaddress 2001:db8::1\n")
	got, err := parseSSHDConfigurationPorts(out)
	if err != nil || fmt.Sprint(got) != "[22 2022 2222]" {
		t.Fatalf("ports = %v, error = %v", got, err)
	}
	for _, bad := range []string{
		"port 22\nlistenaddress [::]:0\n",
		"port 22\nlistenaddress [::]extra\n",
		"port 22\nlistenaddress host:invalid\n",
	} {
		if _, err := parseSSHDConfigurationPorts([]byte(bad)); err == nil {
			t.Fatalf("invalid listenaddress %q was accepted", bad)
		}
	}
}

func TestParseSystemdSocketListenPortsIsStrict(t *testing.T) {
	got, err := parseSystemdSocketListenPorts([]byte("0.0.0.0:22 (Stream) [::]:2222 (Stream) /run/ssh.sock (Stream) 53 (Datagram)"))
	if err != nil || fmt.Sprint(got) != "[22 2222]" {
		t.Fatalf("socket ports = %v, error = %v", got, err)
	}
	for _, bad := range []string{"0.0.0.0:22", "[::] (Stream)", "0 (Stream)"} {
		if _, err := parseSystemdSocketListenPorts([]byte(bad)); err == nil {
			t.Fatalf("invalid systemd Listen %q was accepted", bad)
		}
	}
}

func TestConfiguredSSHDiscoveryUnionsSSHDAndSocketPorts(t *testing.T) {
	runner := &fakeFirewallCommandRunner{
		configuredSSHPorts: []int{22, 2022}, configuredSocketPorts: []int{2222, 22},
	}
	got, err := detectConfiguredSSHPortsWithRunner(runner)
	if err != nil || fmt.Sprint(got) != "[22 2022 2222]" {
		t.Fatalf("configured SSH ports = %v, error = %v", got, err)
	}
}

func TestFirewallSnapshotV2RejectsUnknownOrNonCanonicalData(t *testing.T) {
	valid := encodeFirewallSnapshot([]int{2083}, nil, []int{22})
	unknownSuffix := append([]byte(`,"command":"flush ruleset"}`), '\n')
	unknown := bytes.Replace(valid, []byte{'}', '\n'}, unknownSuffix, 1)
	if err := validateFirewallSnapshot(unknown); err == nil {
		t.Fatal("unknown V2 snapshot field was accepted")
	}
	nonCanonical := bytes.Replace(valid, []byte("[2083]"), []byte("[2083,2083]"), 1)
	if err := validateFirewallSnapshot(nonCanonical); err == nil {
		t.Fatal("non-canonical V2 snapshot was accepted")
	}
}

func TestFileFirewallStateStoreUsesPrivateAtomicSnapshot(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "firewall.nft")
	store := fileFirewallStateStore{
		path: path,
		ownerVerifier: func(os.FileInfo, string) error {
			return nil
		},
	}
	want := []byte(buildFirewallRuleset(false, []int{22, 2083}, nil))
	if err := store.Save(want); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("snapshot mode = %v, want regular 0600", info.Mode())
	}
	got, exists, err := store.Load()
	if err != nil || !exists || !bytes.Equal(got, want) {
		t.Fatalf("Load = %q, %v, %v", got, exists, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "firewall.nft" {
		t.Fatalf("temporary files remained after atomic publish: %v", entries)
	}
}

func TestFileFirewallStateStoreReportsDirectorySyncAfterCommit(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "firewall.nft")
	store := fileFirewallStateStore{
		path:          path,
		ownerVerifier: func(os.FileInfo, string) error { return nil },
		syncDirectory: func(string) error { return errors.New("forced directory sync failure") },
	}
	err := store.Save(encodeFirewallSnapshot([]int{2083}, nil, []int{22}))
	var committed *firewallStateCommittedError
	if !errors.As(err, &committed) {
		t.Fatalf("Save error = %v, want committed directory-sync error", err)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("committed snapshot is missing: %v", statErr)
	}
	err = store.Remove()
	if !errors.As(err, &committed) {
		t.Fatalf("Remove error = %v, want committed directory-sync error", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("committed removal left snapshot present: %v", statErr)
	}
}

func TestFileFirewallStateStoreRejectsNonRootOwnedState(t *testing.T) {
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "firewall.nft")
	want := []byte(buildFirewallRuleset(false, []int{22, 2083}, nil))
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(path, 65534, -1); err != nil {
			t.Fatal(err)
		}
	}
	fileStore := fileFirewallStateStore{
		path: path,
		ownerVerifier: func(info os.FileInfo, label string) error {
			if label == "firewall state directory" {
				return nil
			}
			return requireRootOwner(info, label)
		},
	}
	if _, _, err := fileStore.Load(); err == nil || !strings.Contains(err.Error(), "owner UID") {
		t.Fatalf("Load error = %v, want non-root ownership rejection", err)
	}
	if os.Geteuid() == 0 {
		if err := os.Chown(path, 0, -1); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(dir, 65534, -1); err != nil {
			t.Fatal(err)
		}
	}
	if err := (fileFirewallStateStore{path: path}).Save(want); err == nil || !strings.Contains(err.Error(), "owner UID") {
		t.Fatalf("Save error = %v, want non-root directory ownership rejection", err)
	}
}

type blockingFirewallRunner struct {
	*fakeFirewallCommandRunner
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *blockingFirewallRunner) CombinedOutput(name string, args []string, stdin string) ([]byte, error) {
	if strings.Join(args, " ") == "-f -" {
		r.once.Do(func() { close(r.started) })
		<-r.release
	}
	return r.fakeFirewallCommandRunner.CombinedOutput(name, args, stdin)
}

func TestAgentStatusWaitsForApplyTransaction(t *testing.T) {
	runner := &blockingFirewallRunner{
		fakeFirewallCommandRunner: &fakeFirewallCommandRunner{},
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	store := &fakeFirewallStateStore{}
	applyDone := make(chan struct{})
	go func() {
		defer close(applyDone)
		var resp FirewallStatusResponse
		_ = applyFirewallWithRunnerAndStore(runner, store, &ApplyFirewallRequest{Enabled: true}, &resp)
	}()
	<-runner.started
	statusDone := make(chan FirewallStatusResponse, 1)
	go func() {
		var resp FirewallStatusResponse
		_ = firewallStatusWithRunnerAndStore(runner, store, &resp)
		statusDone <- resp
	}()
	select {
	case <-statusDone:
		t.Fatal("status crossed the in-flight apply transaction")
	case <-time.After(40 * time.Millisecond):
	}
	close(runner.release)
	<-applyDone
	select {
	case st := <-statusDone:
		if !st.Enabled || st.Error != "" {
			t.Fatalf("post-apply status = %+v", st)
		}
	case <-time.After(time.Second):
		t.Fatal("status did not resume after apply")
	}
}

func TestFirewallRestoreUnitHasBootOrderingAndReadOnlyPreflight(t *testing.T) {
	unitPath := filepath.Join("..", "..", "deploy", "systemd", "celikpanel-firewall-restore.service")
	data, err := os.ReadFile(unitPath)
	if err != nil {
		t.Fatal(err)
	}
	unit := string(data)
	for _, required := range []string{
		"Requires=local-fs.target systemd-tmpfiles-setup.service",
		"After=local-fs.target systemd-tmpfiles-setup.service nftables.service ufw.service firewalld.service",
		"Before=network-pre.target celikpanel-agent.service",
		"RuntimeDirectory=sshd",
		"RuntimeDirectoryPreserve=yes",
		"ExecStartPre=/opt/celikpanel/bin/agent --check-firewall-restore",
		"ExecStart=/opt/celikpanel/bin/agent --restore-firewall",
		"RequiredBy=network-pre.target",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, required) {
			t.Fatalf("restore unit is missing %q", required)
		}
	}
	if strings.Contains(unit, "JobTimeoutSec=") {
		t.Fatal("restore unit still has a queue JobTimeoutSec")
	}
}
