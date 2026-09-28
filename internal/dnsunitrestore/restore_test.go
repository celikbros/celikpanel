package dnsunitrestore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

func TestRestoreAliasAndOwnedMasks(t *testing.T) {
	enabled, active := false, false
	masks := map[string]bool{"named.service": true, "bind9.service": true}
	var commands []string
	proofs := 0
	run := func(_ context.Context, executable string, args ...string) ([]byte, error) {
		if executable != "/usr/bin/systemctl" {
			return nil, errors.New("wrong executable")
		}
		commands = append(commands, strings.Join(args, " "))
		name := args[len(args)-1]
		if args[0] == "show" {
			name = args[1]
		}
		switch args[0] {
		case "show":
			if masks[name] {
				return []byte("LoadState=masked\nActiveState=inactive\nUnitFileState=masked\n"), nil
			}
			if name == "bind9.service" && !enabled {
				return []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
			}
			state, file := "inactive", "disabled"
			if active {
				state = "active"
			}
			if enabled {
				file = "enabled"
			}
			return []byte(fmt.Sprintf("LoadState=loaded\nActiveState=%s\nUnitFileState=%s\n", state, file)), nil
		case "unmask":
			delete(masks, name)
		case "enable":
			if name == "bind9.service" && !enabled {
				return nil, errors.New("alias missing")
			}
			enabled = true
		case "start":
			active = true
		case "stop":
			active = false
		default:
			return nil, fmt.Errorf("unexpected command %s", args[0])
		}
		return nil, nil
	}
	snapshots := []dnsengineartifact.UnitSnapshot{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
	err := Restore(context.Background(), snapshots, map[string]bool{"named.service": true, "bind9.service": true}, Ops{
		Systemctl:        "/usr/bin/systemctl",
		VerifyMaskParent: func() error { proofs++; return nil },
		RunSystemd:       run,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !enabled || !active || len(masks) != 0 {
		t.Fatalf("not restored: enabled=%v active=%v masks=%v", enabled, active, masks)
	}
	named, alias := -1, -1
	for i, command := range commands {
		if command == "enable named.service" {
			named = i
		}
		if command == "enable bind9.service" {
			alias = i
		}
	}
	if named < 0 || alias < 0 || named > alias {
		t.Fatalf("alias order: %v", commands)
	}
	mutations := 0
	for _, command := range commands {
		if !strings.HasPrefix(command, "show ") {
			mutations++
		}
	}
	if proofs < mutations {
		t.Fatalf("only %d proofs for %d mutations", proofs, mutations)
	}
}

func TestRestoreRefusesMutationWithoutParentProof(t *testing.T) {
	mutations := 0
	err := Restore(context.Background(), []dnsengineartifact.UnitSnapshot{{
		Name: "named.service", LoadState: "loaded", ActiveState: "inactive", UnitFileState: "disabled",
	}}, nil, Ops{
		Systemctl:        "/usr/bin/systemctl",
		VerifyMaskParent: func() error { return errors.New("parent changed") },
		RunSystemd: func(_ context.Context, _ string, args ...string) ([]byte, error) {
			if args[0] != "show" {
				mutations++
			}
			return []byte("LoadState=loaded\nActiveState=inactive\nUnitFileState=disabled\n"), nil
		},
	})
	if err == nil || mutations != 0 {
		t.Fatalf("err=%v mutations=%d", err, mutations)
	}
}
