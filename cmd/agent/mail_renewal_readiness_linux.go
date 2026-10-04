//go:build linux

package main

import (
	"context"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

// Activity is checked separately from pinned disk bytes. Loaded overrides or a
// pending daemon reload cannot be mistaken for the reviewed native schedule.
func independentMailRenewalScheduleReady(ctx context.Context) bool {
	for _, unit := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName} {
		args := []string{"show"}
		for _, property := range mailrenewalkit.ScheduleProperties() {
			args = append(args, "--property="+property)
		}
		args = append(args, unit)
		raw, err := serviceMutationCommand(ctx, "systemctl", args...).Output()
		if err != nil || !independentMailRenewalUnitReady(unit, raw) {
			return false
		}
	}
	return true
}
func independentMailRenewalUnitReady(unit string, raw []byte) bool {
	observed, err := mailrenewalkit.ParseUnitObservation(unit, raw)
	return err == nil && observed.Ready()
}
