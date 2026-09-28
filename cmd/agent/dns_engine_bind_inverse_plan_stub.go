//go:build !linux

package main

import (
	"context"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func prepareBINDIndependentInverseJournal(_ context.Context, _ hostplatform.Profile, base dnsEngineSwitchJournal, _ bindConfigMutation) (dnsEngineSwitchJournal, error) {
	return base, nil
}
func verifyBINDIndependentSourceProof(_ context.Context, _ dnsEngineSwitchJournal) error { return nil }
