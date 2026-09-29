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
	stoppedUnitNeverStartedTarget
)

// VerifyStoppedNeverStartedTarget proves the BIND target of a V2
// PowerDNS-to-BIND switch inverse stopped when the caller has established,
// from the journal (both target units frozen absent) and the native mask
// proof, that it may be in a state a target has only before its first start.
// A loaded unit gets exactly VerifyStoppedUnit's proof and no extra condition,
// because a V2 journal does not record whether the target started before the
// rollback decision. The two pre-start states are admitted only here: absent
// (not-found, empty unit-file state) and the package guard's persistent mask
// (masked/masked); a runtime-only mask is refused. For those two, each of the
// two identical inactive/dead zero-PID observations must also pass
// sourceOnly, which proves no named process exists and every public port-53
// listener belongs to the source PowerDNS unit (or that none exists while the
// source is stopped). The proof is point-in-time and cannot exclude an
// independent owner restart. It returns the second observation.
func VerifyStoppedNeverStartedTarget(
	ctx context.Context,
	name string,
	observe func(context.Context) (StoppedUnitObservation, error),
	sourceOnly func(context.Context) error,
) (StoppedUnitObservation, error) {
	if sourceOnly == nil {
		return StoppedUnitObservation{}, errors.New("never-started DNS target proof requires a source-only DNS observer")
	}
	return verifyStoppedUnitObservation(ctx, name, observe, stoppedUnitNeverStartedTarget, sourceOnly)
}

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
	_, err := verifyStoppedUnitObservation(ctx, name, observe, class, noListener)
	return err
}

func verifyStoppedUnitObservation(
	ctx context.Context,
	name string,
	observe func(context.Context) (StoppedUnitObservation, error),
	class stoppedUnitClass,
	noListener func(context.Context) error,
) (StoppedUnitObservation, error) {
	extra := class == stoppedUnitFreshSource || class == stoppedUnitNeverStartedTarget
	if ctx == nil || observe == nil || (name != "named.service" && name != "pdns.service") ||
		extra != (noListener != nil) ||
		(class == stoppedUnitNeverStartedTarget && name != "named.service") {
		return StoppedUnitObservation{}, errors.New("DNS stopped proof requires a fixed native unit observer")
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
		case stoppedUnitNeverStartedTarget:
			if seen.LoadState == "loaded" {
				break
			}
			if !(seen.LoadState == "not-found" && seen.UnitFileState == "") &&
				!(seen.LoadState == "masked" && seen.UnitFileState == "masked") {
				return StoppedUnitObservation{}, errors.New("never-started DNS target is neither absent, persistently masked nor loaded")
			}
			if err := noListener(ctx); err != nil {
				return StoppedUnitObservation{}, fmt.Errorf("prove only the source DNS authority serves beside the never-started target: %w", err)
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
		return StoppedUnitObservation{}, err
	}
	after, err := read()
	if err != nil {
		return StoppedUnitObservation{}, err
	}
	if before != after {
		return StoppedUnitObservation{}, errors.New("DNS target unit or process changed during stopped proof")
	}
	return after, nil
}
