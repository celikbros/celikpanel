// Package firewallboot restores only the owner's persisted CelikPanel nft table.
// It has no management Agent, database, licensing or RPC dependency.
package firewallboot

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/firewallpolicy"
)

// snapshot includes stable filesystem identity, not just the same content.
type snapshot struct {
	data     []byte
	identity string
	exists   bool
}
type host interface {
	load() (snapshot, error)
	ssh(context.Context) ([]int, error)
	command(context.Context, string, []string, string) ([]byte, error)
}

// Result distinguishes an absent saved policy from a checked/applied policy.
type Result struct {
	Present bool
	Applied bool
}

func restore(ctx context.Context, h host, checkOnly bool) (Result, error) {
	saved, err := h.load()
	if err != nil {
		return Result{}, err
	}
	if !saved.exists {
		return Result{}, nil
	}
	result := Result{Present: true}
	if _, _, err = firewallpolicy.Decode(saved.data); err != nil {
		return result, err
	}
	ports, err := h.ssh(ctx)
	if err != nil {
		return result, fmt.Errorf("SSH access could not be verified: %w", err)
	}
	table, err := readTable(ctx, h)
	if err != nil {
		return result, err
	}
	batch, err := firewallpolicy.RestoreRuleset(saved.data, ports, table)
	if err != nil {
		return result, err
	}
	if _, err = h.command(ctx, "nft", []string{"--check", "-f", "-"}, batch); err != nil {
		return result, fmt.Errorf("saved firewall preflight failed: %w", err)
	}
	// Configuration/authority can change during a slow host probe. Never replace
	// newer owner intent with the policy read before the probe.
	current, err := h.load()
	if err != nil {
		return result, err
	}
	if !current.exists || current.identity != saved.identity || !bytes.Equal(current.data, saved.data) {
		return result, errors.New("saved firewall policy changed during verification")
	}
	freshPorts, err := h.ssh(ctx)
	if err != nil {
		return result, err
	}
	freshTable, err := readTable(ctx, h)
	if err != nil {
		return result, err
	}
	freshBatch, err := firewallpolicy.RestoreRuleset(current.data, freshPorts, freshTable)
	if err != nil || freshBatch != batch {
		return result, errors.New("SSH or firewall state changed during verification")
	}
	if checkOnly {
		return result, nil
	}
	if _, err = h.command(ctx, "nft", []string{"-f", "-"}, batch); err != nil {
		return result, fmt.Errorf("saved firewall atomic restore failed: %w", err)
	}
	result.Applied = true
	return result, nil
}

func readTable(ctx context.Context, h host) (bool, error) {
	out, err := h.command(ctx, "nft", []string{"list", "tables"}, "")
	if err != nil {
		return false, fmt.Errorf("nft table discovery failed: %w", err)
	}
	present := false
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if len(fields) != 3 || fields[0] != "table" {
			return false, errors.New("nft table discovery returned unsupported output")
		}
		if fields[1] == "inet" && fields[2] == firewallpolicy.Table {
			present = true
		}
	}
	return present, nil
}
