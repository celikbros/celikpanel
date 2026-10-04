//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

// The DNS worker already owns the host lock. A nonblocking release lease avoids
// reversing the update lock order while serializing the first v2 journal with
// runtime promotion. Keep it until the switch exits; promotion refuses any
// retained v2 journal after this lease is released.
func prepareBINDIndependentRuntime(ctx context.Context, profile hostplatform.Profile, scope dnsEngineSwitchJournal) (func() error, func(), error) {
	if !requiresBINDIndependentSourceProof(profile, scope) {
		return func() error { return nil }, func() {}, nil
	}
	return prepareBINDIndependentRuntimeWithCapability(ctx, recoveryruntime.CheckBINDSourceInverseSupport)
}

// Running adoption has its own no-stop inverse. A selected runtime that only
// understands PowerDNS-source switches cannot safely recover it.
func prepareBINDAdoptionIndependentRuntime(ctx context.Context) (func() error, func(), error) {
	return prepareBINDIndependentRuntimeWithCapability(ctx, recoveryruntime.CheckBINDAdoptionInverseSupport)
}

func prepareBINDIndependentRuntimeWithCapability(ctx context.Context, check func(context.Context, *recoveryruntime.Runtime) error) (func() error, func(), error) {
	release, err := hostmutationlock.AcquireExisting("/var/lib/celikpanel-release-transaction/transaction.lock", hostmutationlock.Owner{})
	if err != nil {
		if errors.Is(err, hostmutationlock.ErrBusy) {
			return nil, nil, fmt.Errorf("a release or recovery operation is active; wait for it to finish, then retry this DNS switch: %w", err)
		}
		return nil, nil, err
	}
	runtime, err := recoveryruntime.Resolve()
	if err != nil {
		release.Close()
		return nil, nil, fmt.Errorf("BIND switch needs the selected independent recovery runtime; ask the server owner to prepare it before retrying: %w", err)
	}
	closeProof := func() { runtime.Close(); release.Close() }
	if err := check(ctx, runtime); err != nil {
		closeProof()
		return nil, nil, err
	}
	verify := func() error {
		if err := hostmutationlock.VerifyInherited("/var/lib/celikpanel-release-transaction/transaction.lock", int(release.Fd()), hostmutationlock.Owner{}); err != nil {
			return err
		}
		return runtime.Revalidate()
	}
	return verify, closeProof, nil
}
