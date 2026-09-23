//go:build linux

package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"golang.org/x/sys/unix"
)

func enrollmentLedgerID() servicemutationledger.MailEnrollmentIdentity {
	return servicemutationledger.MailEnrollmentIdentity{RequestID: testMutationRequestID, OwnerID: testMutationOwnerID, ScopeSHA256: strings.Repeat("a", 64)}
}
func enrollmentLedgerFixture(t *testing.T) (string, *os.File, *os.File) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	oldUID, oldGID := serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, 0
	t.Cleanup(func() { serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = oldUID, oldGID })
	root, err := os.MkdirTemp("/run", "cp-enrollment-ledger-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(root) })
	if err = initializeServiceMutationLedger(filepath.Join(root, "state"), filepath.Join(root, "mutation.lock")); err != nil {
		t.Fatal(err)
	}
	release, err := os.OpenFile(filepath.Join(root, "transaction.lock"), os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	host, err := os.OpenFile(filepath.Join(root, "mutation.lock"), os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close(); release.Close() })
	for _, f := range []*os.File{release, host} {
		if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
			t.Fatal(err)
		}
	}
	bin := filepath.Join(root, "bin")
	if err = os.Mkdir(bin, 0755); err != nil {
		t.Fatal(err)
	}
	agent := []byte("fixture Agent is data; never execute it")
	c, _ := agentnativecontract.New(agent, strings.Repeat("b", 40))
	raw, _ := agentnativecontract.Encode(c)
	for name, value := range map[string][]byte{"agent": agent, agentnativecontract.FileName: raw} {
		mode := os.FileMode(0644)
		if name == "agent" {
			mode = 0755
		}
		if err = os.WriteFile(filepath.Join(bin, name), value, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err = os.WriteFile(filepath.Join(root, "accepted"), []byte(enrollmentLedgerID().ScopeSHA256), 0600); err != nil {
		t.Fatal(err)
	}
	return root, host, release
}
func enrollmentLedgerChild(root, next, scenario string, host, release *os.File) *exec.Cmd {
	c := exec.Command(os.Args[0], "-test.run=^TestMailEnrollmentLedgerChild$", "-test.v")
	c.Env = append(os.Environ(), "CP_ENROLL_LEDGER_ROOT="+root, "CP_ENROLL_LEDGER_NEXT="+next, "CP_ENROLL_LEDGER_CASE="+scenario)
	c.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, host, release}
	return c
}
func runEnrollmentLedgerChild(t *testing.T, root, next, scenario string, host, release *os.File) {
	t.Helper()
	out, err := enrollmentLedgerChild(root, next, scenario, host, release).CombinedOutput()
	if err != nil {
		t.Fatalf("%s/%s: %v %s", next, scenario, err, out)
	}
}
func readEnrollmentLedger(t *testing.T, root string) servicemutationledger.Ledger {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "state", "service-mutations.json"))
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	return ledger
}
func TestMailEnrollmentLedgerProcessCuts(t *testing.T) {
	for _, next := range []string{servicemutationledger.MailEnrollmentForward, servicemutationledger.MailEnrollmentRollback, servicemutationledger.MailEnrollmentPublished, servicemutationledger.MailEnrollmentRestored} {
		for _, point := range []string{serviceMutationWriteFaultBeforeRename, serviceMutationWriteFaultAfterRename, serviceMutationWriteFaultAfterSync} {
			t.Run(next+"/"+point, func(t *testing.T) {
				root, host, release := enrollmentLedgerFixture(t)
				if next != servicemutationledger.MailEnrollmentForward {
					runEnrollmentLedgerChild(t, root, servicemutationledger.MailEnrollmentForward, "", host, release)
				}
				if next == servicemutationledger.MailEnrollmentRestored {
					runEnrollmentLedgerChild(t, root, servicemutationledger.MailEnrollmentRollback, "", host, release)
				}
				cmd := enrollmentLedgerChild(root, next, "kill:"+point, host, release)
				out, err := cmd.CombinedOutput()
				if err == nil {
					t.Fatal("missing kill", string(out))
				}
				status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
				if !ok || !status.Signaled() || status.Signal() != syscall.SIGKILL {
					t.Fatal("wrong kill", err, string(out))
				}
				before := readEnrollmentLedger(t, root)
				if point == serviceMutationWriteFaultBeforeRename && next == servicemutationledger.MailEnrollmentForward {
					if before.ActiveRequestID != "" {
						t.Fatal("unpublished reservation adopted")
					}
				} else if next == servicemutationledger.MailEnrollmentForward || next == servicemutationledger.MailEnrollmentRollback || point == serviceMutationWriteFaultBeforeRename {
					if before.ActiveRequestID != testMutationRequestID {
						t.Fatal("early release after kill")
					}
				}
				runEnrollmentLedgerChild(t, root, next, "", host, release)
				after := readEnrollmentLedger(t, root)
				state, err := servicemutationledger.MailEnrollmentState(&after, enrollmentLedgerID())
				if err != nil || state != next {
					t.Fatal(state, err)
				}
				runEnrollmentLedgerChild(t, root, next, "", host, release)
				stable := readEnrollmentLedger(t, root)
				a, _ := servicemutationledger.Encode(&after)
				b, _ := servicemutationledger.Encode(&stable)
				if !bytes.Equal(a, b) {
					t.Fatal("exact retry reset operation or evidence")
				}
			})
		}
	}
}
func TestMailEnrollmentLedgerRefusesMissingOrChangedAuthority(t *testing.T) {
	for _, scenario := range []string{"no-host", "no-release", "historical-agent", "no-intent", "intent-changed", "late-intent", "late-agent", "late-ledger", "late-ledger-replaced", "late-publication", "result-unknown", "publication-busy"} {
		t.Run(scenario, func(t *testing.T) {
			root, host, release := enrollmentLedgerFixture(t)
			next := servicemutationledger.MailEnrollmentForward
			if scenario == "result-unknown" {
				runEnrollmentLedgerChild(t, root, next, "", host, release)
				next = servicemutationledger.MailEnrollmentPublished
			}
			runEnrollmentLedgerChild(t, root, next, scenario, host, release)
		})
	}
}
func TestMailEnrollmentLedgerChild(t *testing.T) {
	root := os.Getenv("CP_ENROLL_LEDGER_ROOT")
	if root == "" {
		t.Skip("subprocess only")
	}
	serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID = 0, 0
	next, scenario := os.Getenv("CP_ENROLL_LEDGER_NEXT"), os.Getenv("CP_ENROLL_LEDGER_CASE")
	bin := filepath.Join(root, "bin")
	if scenario == "historical-agent" {
		agent, e := os.ReadFile(filepath.Join(bin, "agent"))
		if e != nil {
			t.Fatal(e)
		}
		c, _ := agentnativecontract.New(agent, strings.Repeat("b", 40))
		c.MailEnrollmentPolicy = ""
		raw, _ := agentnativecontract.Encode(c)
		if e = os.WriteFile(filepath.Join(bin, agentnativecontract.FileName), raw, 0644); e != nil {
			t.Fatal(e)
		}
	}
	proof, err := recoveryruntime.InspectCompatibleMailAgent(bin)
	if err != nil {
		t.Fatal(err)
	}
	defer proof.Close()
	id := enrollmentLedgerID()
	authority := mailEnrollmentAuthority{identity: id, agent: proof, verifyIntent: func() error {
		raw, e := os.ReadFile(filepath.Join(root, "accepted"))
		if e != nil || string(raw) != id.ScopeSHA256 {
			return errors.New("owner intent changed")
		}
		return nil
	}, verifyResult: func() error {
		if scenario == "result-unknown" {
			return errors.New("native observation unknown")
		}
		return nil
	}}
	if strings.HasPrefix(scenario, "retained-") {
		m, files, e := mailrenewalkit.Payload([]byte("retained native helper fixture"))
		if e != nil {
			t.Fatal(e)
		}
		manifest, e := mailrenewalkit.Encode(m)
		if e != nil {
			t.Fatal(e)
		}
		files[mailrenewalkit.ManifestName] = manifest
		runtime := filepath.Join(root, "native-kit")
		kit := filepath.Join(runtime, m.Generation)
		if e = os.MkdirAll(kit, 0755); e != nil {
			t.Fatal(e)
		}
		for name, raw := range files {
			mode := os.FileMode(0644)
			if name == mailrenewalkit.BinaryName || name == mailrenewalkit.HookName {
				mode = 0755
			}
			if e = os.WriteFile(filepath.Join(kit, name), raw, mode); e != nil {
				t.Fatal(e)
			}
		}
		helper, e := recoveryruntime.InspectRetainedMailEnrollmentHelper(runtime, m.Generation)
		if e != nil {
			t.Fatal(e)
		}
		defer helper.Close()
		authority.agent, authority.retained = nil, helper
		if e = os.RemoveAll(bin); e != nil {
			t.Fatal(e)
		}
		if scenario == "retained-owner" {
			authority.identity.OwnerID = strings.Repeat("f", 32)
		}
		if scenario == "retained-source" {
			if e = os.WriteFile(filepath.Join(kit, mailrenewalkit.TimerName), []byte("changed"), 0644); e != nil {
				t.Fatal(e)
			}
		}
	}
	verifyRelease := func() error {
		return hostmutationlock.VerifyInherited(filepath.Join(root, "transaction.lock"), 9, hostmutationlock.Owner{})
	}
	switch scenario {
	case "no-host":
		unix.Close(8)
	case "no-release":
		unix.Close(9)
	case "no-intent":
		authority.verifyIntent = nil
	case "intent-changed":
		os.WriteFile(filepath.Join(root, "accepted"), []byte("other"), 0600)
	}
	var publication *serviceMutationFileLock
	if scenario == "publication-busy" {
		publication, err = acquireExistingServiceMutationFileLock(serviceMutationLedgerPublicationLockFile(filepath.Join(root, "mutation.lock")))
		if err != nil {
			t.Fatal(err)
		}
		defer publication.Close()
	}
	ledgerPath := filepath.Join(root, "state", "service-mutations.json")
	before, e := os.ReadFile(ledgerPath)
	if e != nil {
		t.Fatal(e)
	}
	fault := func(point string) error {
		if scenario == "kill:"+point {
			unix.Kill(os.Getpid(), syscall.SIGKILL)
			panic("kill returned")
		}
		if point == serviceMutationWriteFaultBeforeRename {
			switch scenario {
			case "late-intent":
				return os.WriteFile(filepath.Join(root, "accepted"), []byte("other"), 0600)
			case "late-agent":
				return os.WriteFile(filepath.Join(bin, "agent"), []byte("changed"), 0755)
			case "late-ledger":
				return os.WriteFile(ledgerPath, []byte("{}"), 0600)
			case "late-ledger-replaced":
				if e := os.Rename(ledgerPath, ledgerPath+".retained"); e != nil {
					return e
				}
				return os.WriteFile(ledgerPath, before, 0600)
			case "late-publication":
				path := serviceMutationLedgerPublicationLockFile(filepath.Join(root, "mutation.lock"))
				if e := os.Rename(path, path+".retained"); e != nil {
					return e
				}
				return os.WriteFile(path, nil, 0600)
			}
		}
		return nil
	}
	err = writeMailEnrollmentReservationAt(filepath.Join(root, "state"), filepath.Join(root, "mutation.lock"), authority, next, verifyRelease, fault)
	if scenario != "" && scenario != "retained-allowed" && !strings.HasPrefix(scenario, "kill:") {
		if err == nil {
			t.Fatal("unsafe reservation accepted", scenario)
		}
		after, e := os.ReadFile(ledgerPath)
		if e != nil {
			t.Fatal(e)
		}
		if scenario == "late-ledger" {
			if string(after) != "{}" {
				t.Fatal("overwrote owner edit")
			}
		} else if !bytes.Equal(before, after) {
			t.Fatal("refusal rewrote evidence")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Log("durable native enrollment reservation", next)
}

// Exercise the real publication writer with management files removed. A native
// kit is never sufficient for new admission or choosing a different direction.
func TestMailEnrollmentRetainedLedgerCannotAdmitOrReverse(t *testing.T) {
	for _, tc := range []struct{ state, next, scenario string }{
		{"", "forward", "retained-denied"},
		{"forward", "forward", "retained-allowed"},
		{"forward", "published", "retained-allowed"},
		{"forward", "rollback", "retained-denied"},
		{"rollback", "forward", "retained-denied"},
		{"rollback", "restored", "retained-allowed"},
		{"published", "forward", "retained-denied"},
		{"published", "published", "retained-allowed"},
		{"restored", "rollback", "retained-denied"},
		{"restored", "restored", "retained-allowed"},
		{"forward", "published", "retained-owner"},
		{"forward", "published", "retained-source"},
	} {
		t.Run(tc.state+"/"+tc.next+"/"+tc.scenario, func(t *testing.T) {
			root, host, release := enrollmentLedgerFixture(t)
			if tc.state != "" {
				runEnrollmentLedgerChild(t, root, "forward", "", host, release)
			}
			if tc.state == "rollback" || tc.state == "restored" {
				runEnrollmentLedgerChild(t, root, "rollback", "", host, release)
			}
			if tc.state == "restored" || tc.state == "published" {
				runEnrollmentLedgerChild(t, root, tc.state, "", host, release)
			}
			runEnrollmentLedgerChild(t, root, tc.next, tc.scenario, host, release)
		})
	}
}
