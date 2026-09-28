//go:build !linux

package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// Fresh paired PowerDNS V3 evidence is supported only by the Linux native
// executor. Other platforms retain the exact journal for owner recovery.
func recoverFreshPrimaryForwardV3(context.Context, dnsengineartifact.SwitchIdentity, dnsEngineSwitchJournal) (dnsEngineSwitchRecoveryOutcome, error) {
	return dnsenginerecovery.OutcomeAbsent, errors.New("v3 fresh PowerDNS forward recovery requires the supported Linux native executor")
}
