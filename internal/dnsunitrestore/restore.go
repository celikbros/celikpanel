// Package dnsunitrestore reconciles saved native DNS systemd unit state. Callers
// supply a fixed, protected systemctl runner and prove the mask parent before
// each mutation; the package does not grant recovery authority itself.
package dnsunitrestore

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
)

type Ops struct {
	Systemctl        string
	VerifyMaskParent func() error
	RunSystemd       func(context.Context, string, ...string) ([]byte, error)
}

type restorer struct{ ops Ops }

var errParentProof = errors.New("DNS mask parent proof failed")

// Order restores named.service before its distro alias bind9.service. The
// alias may not exist until enabling named recreates its symlink.
func Order(snapshots []dnsengineartifact.UnitSnapshot) []dnsengineartifact.UnitSnapshot {
	ordered := append([]dnsengineartifact.UnitSnapshot(nil), snapshots...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].Name != "bind9.service" && ordered[j].Name == "bind9.service"
	})
	return ordered
}

// Restore reconciles exact before-state. ownedMask identifies temporary masks
// created by this operation; preexisting masks are restored in their original
// persistent or runtime class. The caller must supply a bounded context.
func Restore(ctx context.Context, snapshots []dnsengineartifact.UnitSnapshot, ownedMask map[string]bool, ops Ops) error {
	if ctx == nil || ops.Systemctl == "" || ops.VerifyMaskParent == nil || ops.RunSystemd == nil {
		return errors.New("DNS unit snapshot restore requires a systemd guard")
	}
	bounded, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	ctx = bounded
	ordered := Order(snapshots)
	seen := make(map[string]bool, len(ordered))
	for _, before := range ordered {
		if err := dnsengineartifact.ValidateUnitSnapshot(before); err != nil {
			return err
		}
		if seen[before.Name] {
			return fmt.Errorf("duplicate DNS unit snapshot %s", before.Name)
		}
		seen[before.Name] = true
	}
	r := restorer{ops: ops}
	if err := r.prove(); err != nil {
		return err
	}
	var errs []error
	for _, before := range ordered {
		if before.ActiveState != "active" {
			if err := r.ensureStopped(ctx, before.Name); err != nil {
				errs = append(errs, fmt.Errorf("stop %s: %w", before.Name, err))
			}
		}
	}
	for i := len(ordered) - 1; i >= 0; i-- {
		before := ordered[i]
		if ownedMask[before.Name] {
			if err := r.ensureUnmasked(ctx, before.Name); err != nil {
				errs = append(errs, fmt.Errorf("unmask %s: %w", before.Name, err))
			}
		}
	}
	for _, before := range ordered {
		if !masked(before) {
			if err := r.restoreUnitFile(ctx, before); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, before := range ordered {
		if masked(before) {
			if err := r.restoreMask(ctx, before); err != nil {
				errs = append(errs, err)
			}
		}
	}
	for _, before := range ordered {
		if err := r.restoreActive(ctx, before); err != nil {
			errs = append(errs, err)
		}
	}
	for _, before := range ordered {
		if err := r.verify(ctx, before); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func masked(s dnsengineartifact.UnitSnapshot) bool {
	return s.LoadState == "masked" || s.UnitFileState == "masked" || s.UnitFileState == "masked-runtime"
}

func (r restorer) prove() error {
	if err := r.ops.VerifyMaskParent(); err != nil {
		return fmt.Errorf("%w: verify BIND mask parent before systemd mutation: %w", errParentProof, err)
	}
	return nil
}

func (r restorer) run(ctx context.Context, args ...string) ([]byte, error) {
	if err := r.prove(); err != nil {
		return nil, err
	}
	return r.ops.RunSystemd(ctx, r.ops.Systemctl, args...)
}

func (r restorer) inspect(ctx context.Context, name string) (dnsengineartifact.UnitSnapshot, error) {
	out, err := r.ops.RunSystemd(ctx, r.ops.Systemctl, "show", name, "--property=LoadState,ActiveState,UnitFileState", "--no-pager")
	if err != nil {
		return dnsengineartifact.UnitSnapshot{}, fmt.Errorf("systemctl show failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	s := dnsengineartifact.UnitSnapshot{Name: name}
	seen := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok || seen[key] {
			return s, errors.New("systemctl show returned a malformed or duplicate unit state")
		}
		seen[key] = true
		switch key {
		case "LoadState":
			s.LoadState = value
		case "ActiveState":
			s.ActiveState = value
		case "UnitFileState":
			s.UnitFileState = value
		default:
			return s, errors.New("systemctl show returned an unexpected unit state property")
		}
	}
	if !seen["LoadState"] || !seen["ActiveState"] || !seen["UnitFileState"] {
		return s, errors.New("systemctl show returned incomplete unit state")
	}
	if !validObserved(s) {
		return s, fmt.Errorf("unsupported unit state load=%q active=%q unit-file=%q", s.LoadState, s.ActiveState, s.UnitFileState)
	}
	return s, nil
}

func validObserved(s dnsengineartifact.UnitSnapshot) bool {
	if s.ActiveState != "active" && s.ActiveState != "inactive" && s.ActiveState != "failed" {
		return false
	}
	switch s.LoadState {
	case "not-found":
		return s.UnitFileState == ""
	case "masked":
		return s.UnitFileState == "masked" || s.UnitFileState == "masked-runtime"
	case "loaded":
		return s.UnitFileState == "enabled" || s.UnitFileState == "enabled-runtime" || s.UnitFileState == "disabled"
	default:
		return false
	}
}

func (r restorer) result(args []string, out []byte, commandErr, readErr error, after dnsengineartifact.UnitSnapshot) error {
	parts := []string{fmt.Sprintf("systemctl %s did not reach the required state", strings.Join(args, " "))}
	if commandErr != nil {
		parts = append(parts, fmt.Sprintf("command: %v", commandErr))
	}
	if detail := strings.TrimSpace(string(out)); detail != "" {
		parts = append(parts, "output: "+detail)
	}
	if readErr != nil {
		parts = append(parts, fmt.Sprintf("readback: %v", readErr))
	} else {
		parts = append(parts, fmt.Sprintf("readback: load=%s active=%s unit-file=%s", after.LoadState, after.ActiveState, after.UnitFileState))
	}
	return errors.New(strings.Join(parts, "; "))
}

func (r restorer) action(ctx context.Context, name string, acceptable func(dnsengineartifact.UnitSnapshot) bool, args ...string) error {
	out, commandErr := r.run(ctx, args...)
	if errors.Is(commandErr, errParentProof) {
		// A proof failure never authorizes a readback-driven success.
		return commandErr
	}
	after, readErr := r.inspect(ctx, name)
	if readErr == nil && acceptable(after) {
		return nil
	}
	return r.result(args, out, commandErr, readErr, after)
}

func (r restorer) ensureStopped(ctx context.Context, name string) error {
	before, err := r.inspect(ctx, name)
	if err != nil {
		return err
	}
	if before.ActiveState == "inactive" {
		return nil
	}
	action := "stop"
	if before.ActiveState == "failed" {
		action = "reset-failed"
	}
	return r.action(ctx, name, func(s dnsengineartifact.UnitSnapshot) bool { return s.ActiveState == "inactive" }, action, name)
}

func (r restorer) ensureUnmasked(ctx context.Context, name string) error {
	first, firstErr := r.run(ctx, "unmask", name)
	if errors.Is(firstErr, errParentProof) {
		return firstErr
	}
	second, secondErr := r.run(ctx, "unmask", "--runtime", name)
	if errors.Is(secondErr, errParentProof) {
		return errors.Join(firstErr, secondErr)
	}
	after, readErr := r.inspect(ctx, name)
	if readErr == nil && !masked(after) {
		return nil
	}
	return errors.Join(r.result([]string{"unmask", name}, first, firstErr, readErr, after), r.result([]string{"unmask", "--runtime", name}, second, secondErr, readErr, after))
}

func (r restorer) restoreUnitFile(ctx context.Context, before dnsengineartifact.UnitSnapshot) error {
	if before.LoadState == "not-found" && before.UnitFileState == "" {
		after, err := r.inspect(ctx, before.Name)
		if err == nil && after.LoadState == "not-found" && after.UnitFileState == "" {
			return nil
		}
		return r.action(ctx, before.Name, func(s dnsengineartifact.UnitSnapshot) bool {
			return (s.LoadState == "not-found" && s.UnitFileState == "") || (s.UnitFileState == "disabled" && !masked(s))
		}, "disable", before.Name)
	}
	args := []string{"disable", before.Name}
	switch before.UnitFileState {
	case "enabled":
		args = []string{"enable", before.Name}
	case "enabled-runtime":
		args = []string{"enable", "--runtime", before.Name}
	case "disabled":
	default:
		return fmt.Errorf("restore %s: unit-file state %q has no exact inverse", before.Name, before.UnitFileState)
	}
	return r.action(ctx, before.Name, func(s dnsengineartifact.UnitSnapshot) bool {
		return s.UnitFileState == before.UnitFileState && !masked(s)
	}, args...)
}

func (r restorer) restoreMask(ctx context.Context, before dnsengineartifact.UnitSnapshot) error {
	switch before.UnitFileState {
	case "masked":
		first, firstErr := r.run(ctx, "mask", before.Name)
		if errors.Is(firstErr, errParentProof) {
			return firstErr
		}
		second, secondErr := r.run(ctx, "unmask", "--runtime", before.Name)
		if errors.Is(secondErr, errParentProof) {
			return errors.Join(firstErr, secondErr)
		}
		after, readErr := r.inspect(ctx, before.Name)
		if readErr == nil && after.LoadState == "masked" && after.UnitFileState == "masked" {
			return nil
		}
		return errors.Join(r.result([]string{"mask", before.Name}, first, firstErr, readErr, after), r.result([]string{"unmask", "--runtime", before.Name}, second, secondErr, readErr, after))
	case "masked-runtime":
		first, firstErr := r.run(ctx, "mask", "--runtime", before.Name)
		if errors.Is(firstErr, errParentProof) {
			return firstErr
		}
		second, secondErr := r.run(ctx, "unmask", before.Name)
		if errors.Is(secondErr, errParentProof) {
			return errors.Join(firstErr, secondErr)
		}
		after, readErr := r.inspect(ctx, before.Name)
		if readErr == nil && after.LoadState == "masked" && after.UnitFileState == "masked-runtime" {
			return nil
		}
		return errors.Join(r.result([]string{"mask", "--runtime", before.Name}, first, firstErr, readErr, after), r.result([]string{"unmask", before.Name}, second, secondErr, readErr, after))
	default:
		return fmt.Errorf("restore %s: invalid preexisting mask state %q", before.Name, before.UnitFileState)
	}
}

func (r restorer) restoreActive(ctx context.Context, before dnsengineartifact.UnitSnapshot) error {
	if before.ActiveState == "active" {
		return r.action(ctx, before.Name, func(s dnsengineartifact.UnitSnapshot) bool { return s.ActiveState == "active" }, "start", before.Name)
	}
	if err := r.ensureStopped(ctx, before.Name); err != nil {
		return fmt.Errorf("restore stopped %s: %w", before.Name, err)
	}
	return nil
}

func (r restorer) verify(ctx context.Context, before dnsengineartifact.UnitSnapshot) error {
	after, err := r.inspect(ctx, before.Name)
	if err != nil {
		return fmt.Errorf("verify restored %s: %w", before.Name, err)
	}
	if after.ActiveState != before.ActiveState {
		return fmt.Errorf("verify restored %s: active=%s, want %s", before.Name, after.ActiveState, before.ActiveState)
	}
	if before.LoadState == "not-found" && before.UnitFileState == "" {
		if (after.LoadState == "not-found" && after.UnitFileState == "") || (after.UnitFileState == "disabled" && !masked(after)) {
			return nil
		}
		return fmt.Errorf("verify safe compensation for %s: load=%s unit-file=%s", before.Name, after.LoadState, after.UnitFileState)
	}
	if after.UnitFileState != before.UnitFileState {
		return fmt.Errorf("verify restored %s: unit-file=%s, want %s", before.Name, after.UnitFileState, before.UnitFileState)
	}
	return nil
}
