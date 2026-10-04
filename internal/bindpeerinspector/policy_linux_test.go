//go:build linux

package bindpeerinspector

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func policyFixture(t *testing.T) (string, OwnerPolicyV1) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("root-owned policy fixture requires root")
	}
	root := t.TempDir()
	etc := filepath.Join(root, "etc")
	dir := filepath.Join(etc, "bind-peer-inspector")
	if err := os.Mkdir(etc, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(dir, 0750); err != nil {
		t.Fatal(err)
	}
	r := request(t)
	policy := OwnerPolicyV1{Schema: PolicySchemaV1, PrimaryIP: r.PrimaryIP, PeerIP: r.PeerIP, CatalogName: r.CatalogName, View: r.View}
	raw, _ := json.Marshal(policy)
	if err := os.WriteFile(filepath.Join(dir, "policy.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return root, policy
}

func TestOwnerPolicySecureReadAndWrongRequest(t *testing.T) {
	root, want := policyFixture(t)
	got, hash, err := readOwnerPolicyAt(root)
	if err != nil || got != want || len(hash) != 64 {
		t.Fatalf("valid policy rejected: %+v %q %v", got, hash, err)
	}
	r := request(t)
	if err := got.ValidateRequest(r); err != nil {
		t.Fatal(err)
	}
	r.PrimaryIP = "192.0.2.12"
	if err := got.ValidateRequest(r); err == nil {
		t.Fatal("wrong primary accepted")
	}
}

func TestOwnerPolicyUnsafeObjectsFailClosed(t *testing.T) {
	t.Run("world writable directory", func(t *testing.T) {
		root, _ := policyFixture(t)
		dir := filepath.Join(root, "etc", "bind-peer-inspector")
		if err := os.Chmod(dir, 0777); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readOwnerPolicyAt(root); err == nil {
			t.Fatal("unsafe directory accepted")
		}
	})
	t.Run("symlinked directory", func(t *testing.T) {
		root, _ := policyFixture(t)
		dir := filepath.Join(root, "etc", "bind-peer-inspector")
		moved := dir + ".real"
		if err := os.Rename(dir, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, dir); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readOwnerPolicyAt(root); err == nil {
			t.Fatal("symlinked directory accepted")
		}
	})
	t.Run("symlinked file", func(t *testing.T) {
		root, _ := policyFixture(t)
		path := filepath.Join(root, "etc", "bind-peer-inspector", "policy.json")
		moved := path + ".real"
		if err := os.Rename(path, moved); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(moved, path); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readOwnerPolicyAt(root); err == nil {
			t.Fatal("symlinked file accepted")
		}
	})
	t.Run("unsafe file mode", func(t *testing.T) {
		root, _ := policyFixture(t)
		path := filepath.Join(root, "etc", "bind-peer-inspector", "policy.json")
		if err := os.Chmod(path, 0644); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readOwnerPolicyAt(root); err == nil {
			t.Fatal("world-readable policy accepted")
		}
	})
	t.Run("hardlink", func(t *testing.T) {
		root, _ := policyFixture(t)
		path := filepath.Join(root, "etc", "bind-peer-inspector", "policy.json")
		if err := os.Link(path, path+".other"); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readOwnerPolicyAt(root); err == nil {
			t.Fatal("hardlinked file accepted")
		}
	})
	t.Run("unknown JSON field", func(t *testing.T) {
		root, _ := policyFixture(t)
		path := filepath.Join(root, "etc", "bind-peer-inspector", "policy.json")
		raw, _ := os.ReadFile(path)
		raw = []byte(strings.TrimSuffix(string(raw), "}") + ",\"extra\":true}")
		if err := os.WriteFile(path, raw, 0600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := readOwnerPolicyAt(root); err == nil {
			t.Fatal("unknown policy field accepted")
		}
	})
}

type errPolicy struct{ reads int }

func (p *errPolicy) Read(context.Context) (OwnerPolicyV1, string, error) {
	p.reads++
	return OwnerPolicyV1{}, "", errors.New("missing")
}
func TestPolicyRefusalPrecedesNativeReads(t *testing.T) {
	r := request(t)
	reader := &sequenceReader{}
	policy := &errPolicy{}
	_, err := Inspect(context.Background(), r, reader, policy, func() time.Time { return time.Unix(testTime+2, 0) })
	if err == nil || policy.reads != 1 || reader.calls != 0 {
		t.Fatalf("missing policy reached native: %v reads=%d native=%d", err, policy.reads, reader.calls)
	}
}
func TestWrongOwnerPolicyStopsBeforeNativeReads(t *testing.T) {
	r := request(t)
	policy := policyFor(r)
	policy.value.PrimaryIP = "192.0.2.12"
	native := &sequenceReader{}
	_, err := Inspect(context.Background(), r, native, policy, func() time.Time { return time.Unix(testTime+2, 0) })
	if err == nil || policy.calls != 1 || native.calls != 0 {
		t.Fatalf("wrong policy reached native: err=%v policy=%d native=%d", err, policy.calls, native.calls)
	}
}

func TestOwnerPolicyEditAfterNativeReadsRefusesResponse(t *testing.T) {
	r := request(t)
	snapshot := Snapshot{ProcessID: 42, ProcessStartTicks: 900, ConfigSHA256: strings.Repeat("f", 64), ListenersVerified: true, CatalogSerial: r.CatalogSerial, CatalogMembers: []string{"other.example.test"}, CatalogTransferred: true, ZoneState: "unloaded"}
	reader := &sequenceReader{snapshots: []Snapshot{snapshot, snapshot}}
	policy := policyFor(r)
	policy.hashes[1] = "owner-edit"
	_, err := Inspect(context.Background(), r, reader, policy, func() time.Time { return time.Unix(testTime+2, 0) })
	if err == nil || reader.calls != 2 || policy.calls != 2 {
		t.Fatalf("owner policy edit accepted: %v", err)
	}
}
