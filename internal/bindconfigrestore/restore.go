package bindconfigrestore

import (
	"context"
	"errors"
	"fmt"
	"reflect"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

// Operations keeps filesystem authority with a native adapter. Read must
// securely verify the complete owner-aware file set; Write must compare the
// supplied current snapshot before replacing one file. Neither callback may
// accept a caller-selected path outside the frozen set.
type Operations struct {
	Read        func(context.Context) ([]dnsengineartifact.FileSnapshot, error)
	Write       func(context.Context, dnsengineartifact.FileSnapshot, dnsengineartifact.FileSnapshot) error
	BeforeFinal func()
}

// Restore replays only exact before/after file states. A third state is an
// owner edit or unknown effect and is never overwritten. The file set is
// checked before any write and each replacement is checked by secure readback.
func Restore(ctx context.Context, before, after []dnsengineartifact.FileSnapshot, ops Operations) error {
	if ctx == nil || ops.Read == nil || ops.Write == nil {
		return errors.New("BIND config inverse requires context, read and write operations")
	}
	if err := validatePair(before, after); err != nil {
		return err
	}
	read := func() ([]dnsengineartifact.FileSnapshot, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		current, err := ops.Read(ctx)
		if err != nil {
			return nil, err
		}
		if len(current) != len(before) {
			return nil, errors.New("BIND config current file set is incomplete")
		}
		for i := range current {
			if current[i].SHA256 != dnsengineartifact.DigestBytes(current[i].Data) ||
				!sameNativeMetadata(current[i], before[i]) ||
				(!reflect.DeepEqual(current[i], before[i]) && !reflect.DeepEqual(current[i], after[i])) {
				return nil, fmt.Errorf("BIND config changed outside the exact mutation preimage: %s", before[i].Path)
			}
		}
		return current, ctx.Err()
	}
	current, err := read()
	if err != nil {
		return err
	}
	for i := len(before) - 1; i >= 0; i-- {
		if reflect.DeepEqual(current[i], before[i]) {
			continue
		}
		if err := ops.Write(ctx, current[i], before[i]); err != nil {
			return fmt.Errorf("restore BIND config %s: %w", before[i].Path, err)
		}
		current, err = read()
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(current[i], before[i]) {
			return fmt.Errorf("BIND config replacement readback mismatch: %s", before[i].Path)
		}
	}
	if ops.BeforeFinal != nil {
		ops.BeforeFinal()
	}
	current, err = read()
	if err != nil {
		return fmt.Errorf("BIND config final readback: %w", err)
	}
	if !reflect.DeepEqual(current, before) {
		return errors.New("BIND config set changed during final readback")
	}
	return nil
}

func validatePair(before, after []dnsengineartifact.FileSnapshot) error {
	if len(before) == 0 || len(before) != len(after) {
		return errors.New("BIND config inverse has incomplete before/after file sets")
	}
	seen := map[string]bool{}
	for i := range before {
		b, a := before[i], after[i]
		if seen[b.Path] || b.Path == "" || !b.Exists || !a.Exists ||
			!b.OwnerKnown || !a.OwnerKnown || b.UID != 0 || !sameNativeMetadata(b, a) ||
			b.SHA256 != dnsengineartifact.DigestBytes(b.Data) ||
			a.SHA256 != dnsengineartifact.DigestBytes(a.Data) {
			return fmt.Errorf("BIND config inverse has an invalid frozen file pair at index %d", i)
		}
		seen[b.Path] = true
	}
	return nil
}

func sameNativeMetadata(a, b dnsengineartifact.FileSnapshot) bool {
	return a.Path == b.Path && a.Exists == b.Exists && a.Mode == b.Mode &&
		a.OwnerKnown == b.OwnerKnown && a.UID == b.UID && a.GID == b.GID
}
