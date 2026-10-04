//go:build !linux

package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func prepareBINDIndependentAdoptionJournal(context.Context, hostplatform.Profile, dnsEngineSwitchJournal, bindConfigMutation, string, bindAdoptionRuntimeEvidence) (dnsEngineSwitchJournal, error) {
	return dnsEngineSwitchJournal{}, errors.New("independent BIND adoption requires Linux")
}
func verifyBINDAdoptionFrozenSource(context.Context, dnsEngineSwitchJournal, bindAdoptionRuntimeEvidence) error {
	return errors.New("independent BIND adoption requires Linux")
}
func applyBINDAdoptionWithFrozenSource(context.Context, bindConfigMutation, dnsEngineSwitchJournal, bindAdoptionRuntimeEvidence, func() error) error {
	return errors.New("independent BIND adoption requires Linux")
}

func restoreBINDAdoptionWithSourceGuard(context.Context, bindConfigMutation, func() error) error {
	return errors.New("independent BIND adoption requires Linux")
}
