package dnsenginerecovery

import (
	"context"
	"errors"
	"fmt"
)

// StoppedUnitObservation is a bounded native systemd observation, not an
// authorization to alter the service or its configuration.
type StoppedUnitObservation struct {
	Name          string
	LoadState     string
	ActiveState   string
	UnitFileState string
	MainPID       uint64
	ControlPID    uint64
	SubState      string
}

// VerifyStoppedUnit takes two matching observations before an inverse may
// rewrite a stopped target's native state. The caller supplies a trusted,
// fixed-name systemd adapter and holds the operation's host locks. The proof
// is point-in-time and cannot exclude an independent owner restart.
func VerifyStoppedUnit(
	ctx context.Context,
	name string,
	observe func(context.Context) (StoppedUnitObservation, error),
) error {
	return verifyStoppedUnitClass(ctx, name, observe, stoppedUnitLoaded, nil)
}

// VerifyStoppedPDNSPersistentMask proves the exact sealed package-install state.
// It is used only by the fresh V3 inverse after the frozen journal has admitted
// the package guard's owned persistent mask.
func VerifyStoppedPDNSPersistentMask(
	ctx context.Context,
	observe func(context.Context) (StoppedUnitObservation, error),
) error {
	return verifyStoppedUnitClass(ctx, "pdns.service", observe, stoppedUnitPDNSPersistentMask, nil)
}

// VerifyStoppedFreshSourceTarget proves the target of a switch journal that
// records no source engine (a first install: empty SourceEngine, SourceEpoch
// 0) stopped before its inverse restores database, config or state. Before it
// first starts, such a target truthfully passes through three unit states:
// absent before its packages exist (LoadState not-found, empty UnitFileState),
// the package guard's persistent mask after install (masked/masked), and
// loaded once activation unmasked it. Each is accepted only inactive/dead with
// zero systemd main and control PIDs, and each of the two matching
// observations also requires noListener to prove no public port-53 listener.
// A runtime-only mask is not the guard's seal and is refused. Journals with a
// source engine keep VerifyStoppedUnit's loaded requirement. The proof is
// point-in-time and cannot exclude an independent owner restart.
func VerifyStoppedFreshSourceTarget(
	ctx context.Context,
	name string,
	observe func(context.Context) (StoppedUnitObservation, error),
	noListener func(context.Context) error,
) error {
	if noListener == nil {
		return errors.New("fresh DNS target stopped proof requires a port-53 listener observer")
	}
	return verifyStoppedUnitClass(ctx, name, observe, stoppedUnitFreshSource, noListener)
}

type stoppedUnitClass uint8

const (
	stoppedUnitLoaded stoppedUnitClass = iota
	stoppedUnitPDNSPersistentMask
	stoppedUnitFreshSource
)

func freshSourceStoppedLoadState(seen StoppedUnitObservation) bool {
	switch seen.LoadState {
	case "not-found":
		return seen.UnitFileState == ""
	case "masked":
		return seen.UnitFileState == "masked"
	case "loaded":
		return true
	default:
		return false
	}
}

func verifyStoppedUnitClass(
	ctx context.Context,
	name string,
	observe func(context.Context) (StoppedUnitObservation, error),
	class stoppedUnitClass,
	noListener func(context.Context) error,
) error {
	if ctx == nil || observe == nil || (name != "named.service" && name != "pdns.service") ||
		(class == stoppedUnitFreshSource) != (noListener != nil) {
		return errors.New("DNS stopped proof requires a fixed native unit observer")
	}
	read := func() (StoppedUnitObservation, error) {
		if err := ctx.Err(); err != nil {
			return StoppedUnitObservation{}, err
		}
		seen, err := observe(ctx)
		if err != nil {
			return StoppedUnitObservation{}, err
		}
		if err := ctx.Err(); err != nil {
			return StoppedUnitObservation{}, err
		}
		if seen.Name != name || seen.ActiveState != "inactive" ||
			seen.MainPID != 0 || seen.ControlPID != 0 || seen.SubState != "dead" {
			return StoppedUnitObservation{}, errors.New("DNS target is not an inactive/dead unit with zero systemd main and control PIDs")
		}
		switch class {
		case stoppedUnitPDNSPersistentMask:
			if seen.LoadState != "masked" || seen.UnitFileState != "masked" {
				return StoppedUnitObservation{}, errors.New("PowerDNS target lacks its exact persistent mask")
			}
		case stoppedUnitFreshSource:
			if !freshSourceStoppedLoadState(seen) {
				return StoppedUnitObservation{}, errors.New("fresh DNS target is neither absent, persistently masked nor loaded")
			}
			if err := noListener(ctx); err != nil {
				return StoppedUnitObservation{}, fmt.Errorf("prove no public port-53 listener for the never-served DNS target: %w", err)
			}
			if err := ctx.Err(); err != nil {
				return StoppedUnitObservation{}, err
			}
		default:
			if seen.LoadState != "loaded" {
				return StoppedUnitObservation{}, errors.New("DNS target is not a loaded unit")
			}
		}
		return seen, nil
	}
	before, err := read()
	if err != nil {
		return err
	}
	after, err := read()
	if err != nil {
		return err
	}
	if before != after {
		return errors.New("DNS target unit or process changed during stopped proof")
	}
	return nil
}
