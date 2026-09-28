package bindconfigrestore

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func testPair() ([]dnsengineartifact.FileSnapshot, []dnsengineartifact.FileSnapshot) {
	before := []dnsengineartifact.FileSnapshot{
		{Path: "/etc/bind/named.conf.local", Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 44, Data: []byte("old local")},
		{Path: "/etc/bind/named.conf.options", Exists: true, Mode: 0o644, OwnerKnown: true, UID: 0, GID: 44, Data: []byte("old options")},
	}
	after := append([]dnsengineartifact.FileSnapshot(nil), before...)
	for i := range before {
		before[i].SHA256 = dnsengineartifact.DigestBytes(before[i].Data)
		after[i].Data = []byte("new " + before[i].Path)
		after[i].SHA256 = dnsengineartifact.DigestBytes(after[i].Data)
	}
	return before, after
}

func cloneSnapshots(input []dnsengineartifact.FileSnapshot) []dnsengineartifact.FileSnapshot {
	out := append([]dnsengineartifact.FileSnapshot(nil), input...)
	for i := range out {
		out[i].Data = append([]byte(nil), input[i].Data...)
	}
	return out
}

func TestRestoreMixedExactStatesAndReverseOrder(t *testing.T) {
	before, after := testPair()
	current := cloneSnapshots(after)
	current[0] = before[0]
	var writes []string
	err := Restore(context.Background(), before, after, Operations{
		Read: func(context.Context) ([]dnsengineartifact.FileSnapshot, error) { return cloneSnapshots(current), nil },
		Write: func(_ context.Context, from, to dnsengineartifact.FileSnapshot) error {
			writes = append(writes, to.Path)
			if !reflect.DeepEqual(current[1], from) {
				t.Fatal("write did not receive exact current file")
			}
			current[1] = to
			return nil
		},
	})
	if err != nil || !reflect.DeepEqual(current, before) || !reflect.DeepEqual(writes, []string{before[1].Path}) {
		t.Fatalf("restore = %v, current = %+v, writes = %v", err, current, writes)
	}
}

func TestRestoreRefusesOwnerEditBeforeEveryEffect(t *testing.T) {
	before, after := testPair()
	current := cloneSnapshots(after)
	writes := 0
	err := Restore(context.Background(), before, after, Operations{
		Read: func(context.Context) ([]dnsengineartifact.FileSnapshot, error) { return cloneSnapshots(current), nil },
		Write: func(_ context.Context, _, to dnsengineartifact.FileSnapshot) error {
			writes++
			current[1] = to
			current[0].Data = []byte("owner edit")
			current[0].SHA256 = dnsengineartifact.DigestBytes(current[0].Data)
			return nil
		},
	})
	if err == nil || !strings.Contains(err.Error(), "changed outside") || writes != 1 || string(current[0].Data) != "owner edit" {
		t.Fatalf("owner edit outcome = %v, writes = %d, current = %+v", err, writes, current)
	}
}

func TestRestoreRefusesUnknownBeforeMutation(t *testing.T) {
	before, after := testPair()
	current := cloneSnapshots(after)
	current[0].GID++
	calls := 0
	err := Restore(context.Background(), before, after, Operations{
		Read: func(context.Context) ([]dnsengineartifact.FileSnapshot, error) { return cloneSnapshots(current), nil },
		Write: func(context.Context, dnsengineartifact.FileSnapshot, dnsengineartifact.FileSnapshot) error {
			calls++
			return nil
		},
	})
	if err == nil || calls != 0 {
		t.Fatalf("unsafe file accepted: %v, writes %d", err, calls)
	}
}

func TestRestoreStopsAfterWriteFailureAndRetainsUnknownResult(t *testing.T) {
	before, after := testPair()
	current := cloneSnapshots(after)
	calls := 0
	marker := errors.New("ambiguous write")
	err := Restore(context.Background(), before, after, Operations{
		Read: func(context.Context) ([]dnsengineartifact.FileSnapshot, error) { return cloneSnapshots(current), nil },
		Write: func(context.Context, dnsengineartifact.FileSnapshot, dnsengineartifact.FileSnapshot) error {
			calls++
			return marker
		},
	})
	if !errors.Is(err, marker) || calls != 1 || !reflect.DeepEqual(current, after) {
		t.Fatalf("failed write outcome = %v, writes %d", err, calls)
	}
}
