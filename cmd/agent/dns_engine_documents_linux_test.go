//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSEvidenceDocumentInterruptedPublication(t *testing.T) {
	if phase := os.Getenv("CELIKPANEL_TEST_DNS_DOCUMENT_CUT"); phase != "" {
		useTestServiceMutationOwner(t)
		target, err := os.ReadFile(filepath.Join(serviceMutationStateDirectory(), "target.json"))
		if err != nil {
			t.Fatal(err)
		}
		state, err := dnsengineartifact.DecodeV1(target)
		if err != nil {
			t.Fatal(err)
		}
		if phase == "before-rename" {
			data, err := dnsengineartifact.CanonicalStateDocumentV2(state)
			if err != nil {
				t.Fatal(err)
			}
			before, err := captureDNSEngineStateSnapshot(false)
			if err != nil {
				t.Fatal(err)
			}
			err = secureWriteConfigReplacingSnapshotWithOwnerAndHook(before.Path, data, 0600, &before, serviceMutationRequiredOwnerUID, serviceMutationRequiredOwnerGID, func() {
				_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
			})
			t.Fatalf("cut did not happen: %v", err)
		}
		if err := persistExactDNSEngineState(state); err != nil {
			t.Fatal(err)
		}
		_ = syscall.Kill(os.Getpid(), syscall.SIGKILL)
		t.Fatal("process survived SIGKILL")
	}
	if os.Geteuid() != 0 {
		t.Skip("native file ownership fixture")
	}
	for _, format := range []string{"legacy", "separated"} {
		for _, phase := range []string{"before-rename", "after-publication"} {
			t.Run(format+"/"+phase, func(t *testing.T) {
				root := prepareDNSEngineOwnershipTest(t)
				read := func(name string) []byte {
					t.Helper()
					data, err := os.ReadFile(filepath.Join("..", "..", "internal", "dnsengineartifact", "testdata", "alpha81-"+name+".json"))
					if err != nil {
						t.Fatal(err)
					}
					return data
				}
				oldOwner, oldCurrent, target := read("bind-acquisition"), read("bind-zone-add"), read("bind-zone-edit")
				if format == "separated" {
					plan, err := dnsengineartifact.PlanLegacySeparationV1(oldOwner, oldCurrent)
					if err != nil {
						t.Fatal(err)
					}
					oldOwner, oldCurrent, err = dnsengineartifact.SeparationDocumentsV2(plan)
					if err != nil {
						t.Fatal(err)
					}
				}
				ownershipPath, err := dnsEngineOwnershipPath(transport.DNSEngineBIND)
				if err != nil {
					t.Fatal(err)
				}
				for path, data := range map[string][]byte{dnsEngineStatePath(): oldCurrent, ownershipPath: oldOwner, filepath.Join(root, "target.json"): target} {
					if err := os.WriteFile(path, data, 0600); err != nil {
						t.Fatal(err)
					}
				}
				before, err := captureDNSEngineStateSnapshot(false)
				if err != nil {
					t.Fatal(err)
				}
				cmd := exec.Command(os.Args[0], "-test.run=^TestDNSEvidenceDocumentInterruptedPublication$")
				cmd.Env = append(os.Environ(), "CELIKPANEL_TEST_DNS_DOCUMENT_CUT="+phase)
				output, err := cmd.CombinedOutput()
				var exited *exec.ExitError
				if !errors.As(err, &exited) || !exited.ProcessState.Sys().(syscall.WaitStatus).Signaled() || exited.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
					t.Fatalf("expected real SIGKILL: %v %s", err, output)
				}
				actual, err := os.ReadFile(dnsEngineStatePath())
				if err != nil {
					t.Fatal(err)
				}
				if phase == "before-rename" {
					if !bytes.Equal(actual, oldCurrent) {
						t.Fatal("pre-publication cut changed current file")
					}
				} else {
					desired, _ := dnsengineartifact.DecodeV1(target)
					got, newFormat, err := dnsengineartifact.DecodeStateDocument(actual)
					if err != nil || !newFormat || got != desired {
						t.Fatal("committed file lost after process death", err)
					}
				}
				ownerAfter, err := os.ReadFile(ownershipPath)
				if err != nil || !bytes.Equal(oldOwner, ownerAfter) {
					t.Fatal("publication rewrote frozen acquisition checkpoint")
				}
				if err := restoreDNSEngineStateSnapshot(before); err != nil {
					t.Fatal(err)
				}
				restored, err := os.ReadFile(dnsEngineStatePath())
				if err != nil || !bytes.Equal(restored, oldCurrent) {
					t.Fatal("rollback changed before-image format", err)
				}
				if err := restoreDNSEngineStateSnapshot(before); err != nil {
					t.Fatal("rollback retry", err)
				}
			})
		}
	}
}

func TestDNSSignedUpdateFinalizationPreservesCurrentWireFormat(t *testing.T) {
	for _, separated := range []bool{false, true} {
		t.Run(fmt.Sprint(separated), func(t *testing.T) {
			prepareDNSEngineOwnershipTest(t)
			state := legacyDurableDNSState(transport.DNSEngineBIND)
			raw, err := dnsengineartifact.CanonicalV1(state)
			if separated {
				raw, err = dnsengineartifact.CanonicalStateDocumentV2(state)
			}
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(dnsEngineStatePath(), raw, 0600); err != nil {
				t.Fatal(err)
			}
			if err := writeDNSEngineOwnershipForSignedUpdate(state); err != nil {
				t.Fatal(err)
			}
			path, _ := dnsEngineOwnershipPath(state.Engine)
			got, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			decoded, v2, err := dnsengineartifact.DecodeOwnershipDocument(got)
			if err != nil || decoded != state || v2 != separated {
				t.Fatal("signed update changed wire compatibility", err)
			}
			if !separated && !bytes.Equal(raw, got) {
				t.Fatal("historical ownership bytes changed")
			}
			changed := state
			changed.EngineEpoch++
			if err := writeDNSEngineOwnershipForSignedUpdate(changed); err == nil {
				t.Fatal("changed source accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(got, after) {
				t.Fatal("refusal rewrote evidence")
			}
		})
	}
}
