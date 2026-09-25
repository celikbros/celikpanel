package dnsenginerecovery

import (
	"context"
	"errors"
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
	if ctx == nil || observe == nil || (name != "named.service" && name != "pdns.service") {
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
		if seen.Name != name || seen.ActiveState != "inactive" ||
			seen.MainPID != 0 || seen.ControlPID != 0 || seen.SubState != "dead" {
			return StoppedUnitObservation{}, errors.New("DNS target is not inactive/dead with zero systemd main and control PIDs")
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
