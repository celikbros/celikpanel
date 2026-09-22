//go:build linux

package recoverypublication

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
)

func addAgentContract(t *testing.T, root string, agent string) {
	t.Helper()
	c, e := agentnativecontract.New([]byte(agent), strings.Repeat("a", 40))
	if e != nil {
		t.Fatal(e)
	}
	raw, e := agentnativecontract.Encode(c)
	if e != nil {
		t.Fatal(e)
	}
	write(t, filepath.Join(root, "bin", agentnativecontract.FileName), raw, 0644)
}
func TestAgentDeclarationTravelsWithAtomicPublicationAndInverse(t *testing.T) {
	for _, prior := range []bool{false, true} {
		for _, point := range []string{"complete", "stage_file_written", "intent_durable", "exchange_done", "receipt_durable"} {
			name := point
			if prior {
				name += "-prior-contract"
			}
			t.Run(name, func(t *testing.T) {
				f := newFixture(t, "bin", "update")
				snap := filepath.Join(f.Root, "snapshots", f.Request.Snapshot)
				if prior {
					addAgentContract(t, snap, "old-agent")
					addAgentContract(t, filepath.Join(f.Root, "prefix"), "old-agent")
				}
				addAgentContract(t, f.Request.CandidateRoot, "new-agent")
				f.Request.SnapshotManifest = checksumManifest(t, snap)
				f.Request.CandidateManifest = checksumManifest(t, f.Request.CandidateRoot)
				old := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
				if point != "complete" {
					crash(t, f, point)
				}
				if e := publish(f.Request, "update", f.config()); e != nil {
					t.Fatal(e)
				}
				raw, e := os.ReadFile(filepath.Join(f.Root, "prefix/bin", agentnativecontract.FileName))
				if e != nil {
					t.Fatal(e)
				}
				agent, e := os.ReadFile(filepath.Join(f.Root, "prefix/bin/agent"))
				if e != nil {
					t.Fatal(e)
				}
				if _, e = agentnativecontract.Verify(raw, agent); e != nil {
					t.Fatal("new Agent/declaration mismatch", e)
				}
				info, e := os.Stat(filepath.Join(f.Root, "prefix/bin", agentnativecontract.FileName))
				if e != nil || info.Mode().Perm() != 0644 {
					t.Fatal("declaration became executable")
				}
				kept, e := os.ReadFile(filepath.Join(f.Root, "prefix/bin/owner-helper"))
				if e != nil || string(kept) != "owner-helper" {
					t.Fatal("owner helper lost")
				}
				f.marker(t, "rollback")
				f.Operation = "rollback"
				if e = publish(f.Request, "rollback", f.config()); e != nil {
					t.Fatal(e)
				}
				if got := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin")); got.semantic() != old.semantic() {
					t.Fatal("inverse did not restore exact old Agent/declaration presence")
				}
			})
		}
	}
}

func TestAgentDeclarationEditAfterForwardPublicationBlocksInverse(t *testing.T) {
	f := newFixture(t, "bin", "update")
	addAgentContract(t, f.Request.CandidateRoot, "new-agent")
	f.Request.CandidateManifest = checksumManifest(t, f.Request.CandidateRoot)
	if e := publish(f.Request, "update", f.config()); e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(f.Root, "prefix/bin", agentnativecontract.FileName)
	write(t, path, []byte("later owner edit"), 0644)
	f.marker(t, "rollback")
	f.Operation = "rollback"
	if e := publish(f.Request, "rollback", f.config()); e == nil {
		t.Fatal("owner edit overwritten")
	}
	raw, e := os.ReadFile(path)
	if e != nil || string(raw) != "later owner edit" {
		t.Fatal("owner evidence lost")
	}
}

func TestAgentDeclarationMaterialRollbackWithoutCandidate(t *testing.T) {
	f := newMaterialFixture(t)
	addAgentContract(t, f.Request.CandidateRoot, "new-agent")
	f.Request.CandidateManifest = checksumManifest(t, f.Request.CandidateRoot)
	prepareMaterial(t, f)
	applyMaterial(t, f, "bin")
	applyMaterial(t, f, "web")
	if e := os.Rename(f.Request.CandidateRoot, f.Request.CandidateRoot+".unavailable"); e != nil {
		t.Fatal(e)
	}
	f.marker(t, "rollback")
	f.Operation = "rollback"
	crash(t, f, "exchange_done")
	if e := publish(f.Request, "rollback", f.config()); e != nil {
		t.Fatal(e)
	}
	actual := readTree(t, f.Root, filepath.Join(f.Root, "prefix/bin"))
	old := readTree(t, f.Root, filepath.Join(f.Root, "snapshots", f.Request.Snapshot, "bin"))
	if actual.semantic() != old.semantic() {
		t.Fatal("candidate-independent retry lost exact old resource")
	}
}
