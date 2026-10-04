//go:build linux

package recoveryruntime

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func recordedObservationFixture(t *testing.T) (string, string, string) {
	t.Helper()
	root, target, host, release := enrollmentFixture(t, "absent")
	child := exec.Command(os.Args[0], "-test.run=^TestMailEnrollmentPreparationChild$", "-test.v")
	child.Env = append(os.Environ(), "CP_MAIL_PREPARE_ROOT="+root, "CP_MAIL_PREPARE_TARGET="+target, "CP_MAIL_PREPARE_CASE=recorded-forward")
	child.ExtraFiles = []*os.File{nil, nil, nil, nil, nil, host, release}
	if out, err := child.CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	host.Close()
	release.Close()
	return root, target, filepath.Join(root, "accepted-ledger", "service-mutations.json")
}
func TestMailEnrollmentObservationDoesNotNeedMutationLocksOrInstalledAgent(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	root, target, path := recordedObservationFixture(t)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	ledger, err := servicemutationledger.Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	id, _, err := servicemutationledger.RecordedMailEnrollment(&ledger, mailCaptureTestOperation)
	if err != nil {
		t.Fatal(err)
	}
	// Historical result observation survives removal of management and native kit.
	for _, dir := range []string{"agent-bin", "runtime", "transaction"} {
		if err = os.RemoveAll(filepath.Join(root, dir)); err != nil {
			t.Fatal(err)
		}
	}
	for _, state := range []string{"forward", "published", "rollback", "restored"} {
		current := ledger
		if state == "published" {
			current, err = servicemutationledger.AdvanceMailEnrollment(&current, id, state, time.Now().UTC())
		}
		if state == "rollback" || state == "restored" {
			current, err = servicemutationledger.AdvanceMailEnrollment(&current, id, "rollback", time.Now().UTC())
			if err == nil && state == "restored" {
				current, err = servicemutationledger.AdvanceMailEnrollment(&current, id, state, time.Now().UTC())
			}
		}
		if err != nil {
			t.Fatal(err)
		}
		bytes, err := servicemutationledger.Encode(&current)
		if err != nil {
			t.Fatal(err)
		}
		capturePut(t, path, bytes, 0600)
		got, err := ObserveMailEnrollment(context.Background(), path, servicemutationledger.FileOwner{}, filepath.Join(root, "journals"), id.RequestID, id.OwnerID, target)
		if err != nil || !got.Found || got.State != state || got.Identity != id || got.Generation != target {
			t.Fatalf("%s: %+v %v", state, got, err)
		}
		after, _ := os.ReadFile(path)
		if string(after) != string(bytes) {
			t.Fatal("observation changed ledger")
		}
	}
}
func TestMailEnrollmentObservationRefusesMixedEvidenceAndPreservesAbsence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"missing-id", "missing-ledger", "missing-parent", "wrong-owner", "wrong-target", "changed-capture", "changed-plan", "symlink", "metadata", "late-ledger", "late-capture", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, target, path := recordedObservationFixture(t)
			raw, _ := os.ReadFile(path)
			ledger, _ := servicemutationledger.Decode(raw)
			id, _, _ := servicemutationledger.RecordedMailEnrollment(&ledger, mailCaptureTestOperation)
			request, owner := id.RequestID, id.OwnerID
			journals := filepath.Join(root, "journals")
			capture := filepath.Join(journals, id.RequestID+".json")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var late func()
			switch scenario {
			case "missing-id":
				request = strings.Repeat("f", 32)
				journals = filepath.Join(root, "no-journals")
			case "missing-ledger":
				os.Remove(path)
			case "missing-parent":
				os.RemoveAll(filepath.Dir(path))
			case "wrong-owner":
				owner = strings.Repeat("f", 32)
			case "wrong-target":
				target = strings.Repeat("f", 64)
			case "changed-capture":
				capturePut(t, capture, []byte("{}\n"), 0600)
			case "changed-plan":
				capturePut(t, filepath.Join(journals, id.RequestID+".files.json"), []byte("{}\n"), 0600)
			case "symlink":
				os.Rename(capture, capture+".saved")
				os.Symlink(capture+".saved", capture)
			case "metadata":
				os.Chmod(capture, 0644)
			case "late-ledger":
				late = func() {
					next, e := servicemutationledger.AdvanceMailEnrollment(&ledger, id, "rollback", time.Now().UTC())
					if e != nil {
						t.Fatal(e)
					}
					data, e := servicemutationledger.Encode(&next)
					if e != nil {
						t.Fatal(e)
					}
					capturePut(t, path, data, 0600)
				}
			case "late-capture":
				late = func() { capturePut(t, capture, []byte("{}\n"), 0600) }
			case "cancelled":
				cancel()
			}
			got, err := observeMailEnrollmentAt(ctx, path, servicemutationledger.FileOwner{}, journals, request, owner, target, late)
			if scenario == "missing-id" {
				if err != nil || got.Found || got.State != "" {
					t.Fatalf("missing request: %+v %v", got, err)
				}
				if _, e := os.Lstat(journals); !errors.Is(e, os.ErrNotExist) {
					t.Fatal("read created journals")
				}
			} else if err == nil || got.Found || got.State != "" {
				t.Fatalf("unsafe evidence accepted: %+v %v", got, err)
			}
		})
	}
}
func TestMailEnrollmentHelperPinsWholeSourceWithoutExecutingIt(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	for _, scenario := range []string{"valid", "helper-edited", "template-edited", "agent-edited", "path-edited", "closed"} {
		t.Run(scenario, func(t *testing.T) {
			root, target, host, release := enrollmentFixture(t, "absent")
			host.Close()
			release.Close()
			agent := enrollmentRecordedAgent(t, root, target)
			agent.Close()
			proof, err := InspectMailEnrollmentHelper(filepath.Join(root, "recorded-agent"), filepath.Join(root, "runtime"))
			if err != nil {
				t.Fatal(err)
			}
			defer proof.Close()
			if proof.Generation != target || proof.Path != filepath.Join(root, "runtime", target, mailrenewalkit.BinaryName) {
				t.Fatal("wrong helper")
			}
			commit, digest := proof.AgentIdentity()
			if commit != strings.Repeat("a", 40) || !ValidDigest(digest) {
				t.Fatal("wrong Agent proof")
			}
			switch scenario {
			case "helper-edited":
				capturePut(t, proof.Path, []byte("changed helper"), 0755)
			case "template-edited":
				capturePut(t, filepath.Join(filepath.Dir(proof.Path), mailrenewalkit.TimerName), []byte("changed timer"), 0644)
			case "agent-edited":
				capturePut(t, filepath.Join(root, "recorded-agent", "agent"), []byte("changed agent"), 0755)
			case "path-edited":
				proof.Path = "/other/renew"
			case "closed":
				proof.Close()
			}
			if err = proof.Revalidate(); (err == nil) != (scenario == "valid") {
				t.Fatalf("source proof: %v", err)
			}
		})
	}
}
