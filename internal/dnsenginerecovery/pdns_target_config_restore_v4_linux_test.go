//go:build linux

package dnsenginerecovery

import (
	"context"
	"errors"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func pdnsRestoreSnapshotsV4() ([]dnsengineartifact.FileSnapshot, []dnsengineartifact.FileSnapshot) {
	file := func(path, data string, exists bool) dnsengineartifact.FileSnapshot {
		s := dnsengineartifact.FileSnapshot{Path: path, Exists: exists}
		if exists {
			s.Mode = 0o644
			s.OwnerKnown = true
			s.UID = 0
			s.GID = 0
			s.Data = []byte(data)
			s.SHA256 = dnsengineartifact.DigestBytes(s.Data)
		}
		return s
	}
	before := []dnsengineartifact.FileSnapshot{
		file("/etc/powerdns/pdns.conf", "old-main", true),
		file("/etc/powerdns/pdns.d/celikpanel-cluster.conf", "", false),
		file("/etc/powerdns/pdns.d/celikpanel.conf", "", false),
	}
	after := []dnsengineartifact.FileSnapshot{
		file(before[0].Path, "new-main", true),
		file(before[1].Path, "", false),
		file(before[2].Path, "new-managed", true),
	}
	return before, after
}

func TestPDNSTargetConfigRestoreV4MixedReplayAndUnknownRefusal(t *testing.T) {
	before, after := pdnsRestoreSnapshotsV4()
	states := []PDNSTargetConfigStateV4{PDNSTargetConfigAfterV4, PDNSTargetConfigBeforeV4, PDNSTargetConfigAfterV4}
	calls := []string{}
	failAt := "remove"
	ops := PDNSTargetConfigRestoreOps{
		Guard: func(context.Context) error { calls = append(calls, "guard"); return nil },
		Read: func(context.Context) ([]PDNSTargetConfigStateV4, error) {
			out := append([]PDNSTargetConfigStateV4(nil), states...)
			return out, nil
		},
		Write: func(_ context.Context, current, desired dnsengineartifact.FileSnapshot) error {
			if current.Path != after[0].Path || desired.Path != before[0].Path {
				return errors.New("wrong write")
			}
			calls = append(calls, "write")
			states[0] = PDNSTargetConfigBeforeV4
			return nil
		},
		Remove: func(_ context.Context, current dnsengineartifact.FileSnapshot) error {
			if current.Path != after[2].Path {
				return errors.New("wrong removal")
			}
			calls = append(calls, "remove")
			if failAt == "remove" {
				return errors.New("interrupted")
			}
			states[2] = PDNSTargetConfigBeforeV4
			return nil
		},
	}
	if err := RestorePDNSTargetConfigCheckpointV4(context.Background(), before, after, ops); err == nil || states[0] != PDNSTargetConfigAfterV4 {
		t.Fatalf("failed first effect did not retain exact preimage: %v %v", err, states)
	}
	failAt = ""
	if err := RestorePDNSTargetConfigCheckpointV4(context.Background(), before, after, ops); err != nil {
		t.Fatal(err)
	}
	if states[0] != PDNSTargetConfigBeforeV4 || states[2] != PDNSTargetConfigBeforeV4 {
		t.Fatalf("restore incomplete: %v", states)
	}
	for _, call := range calls {
		if call == "write" {
			return
		}
	}
	t.Fatal("present before-image was never restored")
}

func TestPDNSTargetConfigRestoreV4PreservesOwnerEdit(t *testing.T) {
	before, after := pdnsRestoreSnapshotsV4()
	writes := 0
	ops := PDNSTargetConfigRestoreOps{
		Guard: func(context.Context) error { return nil },
		Read: func(context.Context) ([]PDNSTargetConfigStateV4, error) {
			return []PDNSTargetConfigStateV4{PDNSTargetConfigAfterV4, PDNSTargetConfigUnknownV4, PDNSTargetConfigAfterV4}, nil
		},
		Write: func(context.Context, dnsengineartifact.FileSnapshot, dnsengineartifact.FileSnapshot) error {
			writes++
			return nil
		},
		Remove: func(context.Context, dnsengineartifact.FileSnapshot) error { writes++; return nil },
	}
	if err := RestorePDNSTargetConfigCheckpointV4(context.Background(), before, after, ops); err == nil || writes != 0 {
		t.Fatalf("owner edit reached effect: %v writes=%d", err, writes)
	}
}
func TestPDNSTargetConfigRestoreV4ResumesAfterAppliedEffectError(t *testing.T) {
	before, after := pdnsRestoreSnapshotsV4()
	states := []PDNSTargetConfigStateV4{PDNSTargetConfigAfterV4, PDNSTargetConfigBeforeV4, PDNSTargetConfigBeforeV4}
	writes := 0
	interrupt := true
	ops := PDNSTargetConfigRestoreOps{
		Guard: func(context.Context) error { return nil },
		Read: func(context.Context) ([]PDNSTargetConfigStateV4, error) {
			return append([]PDNSTargetConfigStateV4(nil), states...), nil
		},
		Write: func(_ context.Context, current, desired dnsengineartifact.FileSnapshot) error {
			writes++
			states[0] = PDNSTargetConfigBeforeV4
			if interrupt {
				return errors.New("worker cut after publication")
			}
			return nil
		},
		Remove: func(context.Context, dnsengineartifact.FileSnapshot) error { return errors.New("unexpected removal") },
	}
	if err := RestorePDNSTargetConfigCheckpointV4(context.Background(), before, after, ops); err == nil {
		t.Fatal("post-publication cut was reported as success")
	}
	interrupt = false
	if err := RestorePDNSTargetConfigCheckpointV4(context.Background(), before, after, ops); err != nil {
		t.Fatal(err)
	}
	if writes != 1 {
		t.Fatalf("already restored config was rewritten: %d", writes)
	}
}
func TestPDNSTargetConfigRestoreV4RejectsUnsupportedMetadataTransitionBeforeEffects(t *testing.T) {
	before, after := pdnsRestoreSnapshotsV4()
	after[0].Mode = 0o600
	calls := 0
	ops := PDNSTargetConfigRestoreOps{
		Guard: func(context.Context) error { calls++; return nil },
		Read:  func(context.Context) ([]PDNSTargetConfigStateV4, error) { calls++; return nil, nil },
		Write: func(context.Context, dnsengineartifact.FileSnapshot, dnsengineartifact.FileSnapshot) error {
			calls++
			return nil
		},
		Remove: func(context.Context, dnsengineartifact.FileSnapshot) error { calls++; return nil },
	}
	if err := RestorePDNSTargetConfigCheckpointV4(context.Background(), before, after, ops); err == nil || calls != 0 {
		t.Fatalf("unsupported ownership/mode transition reached an effect: %v calls=%d", err, calls)
	}
}
