package main

import (
	"context"
	"errors"
	"reflect"

	"github.com/alicelik/celikpanel/internal/binddns"
)

type bindControlRunner func(context.Context, ...string) error

func trustedBINDControl(ctx context.Context, args ...string) error {
	control, err := firstTrustedExecutable(
		[]string{"/usr/sbin/rndc", "/usr/bin/rndc"}, "rndc",
	)
	if err != nil {
		return errors.New("trusted BIND control executable is unavailable")
	}
	output, err := serviceMutationCommand(
		ctx, control, args...,
	).CombinedOutputLimited(64 << 10)
	_ = output
	if err != nil {
		return errors.New("BIND control command failed")
	}
	return nil
}

func bindV3PrimaryPropagationPlan(
	tree binddns.VerifiedTree,
	domain string,
) (dnsV3PrimaryPropagationPlan, bool, error) {
	receipt := tree.CurrentReceipt()
	zone, data, found := tree.Zone(domain)
	if !found {
		return dnsV3PrimaryPropagationPlan{}, false,
			errors.New("BIND V3 zone is absent from the verified current tree")
	}
	changed, err := expectedDNSZoneAuthorityFromBINDTree(zone, data)
	if err != nil {
		return dnsV3PrimaryPropagationPlan{}, false, err
	}
	if receipt.Pairing == nil {
		return dnsV3PrimaryPropagationPlan{Changed: changed}, false, nil
	}
	if receipt.Pairing.Role != binddns.PairRolePrimary {
		return dnsV3PrimaryPropagationPlan{}, false,
			errors.New("BIND secondary cannot propagate a panel-owned V3 zone")
	}
	evidence, primary, err := bindPrimaryCatalogEvidence(tree)
	if err != nil || !primary {
		if err == nil {
			err = errors.New("BIND primary catalog evidence is unavailable")
		}
		return dnsV3PrimaryPropagationPlan{}, false, err
	}
	plan := dnsV3PrimaryPropagationPlan{Evidence: evidence, Changed: changed}
	if err := validateDNSV3PrimaryPropagationPlan(plan); err != nil {
		return dnsV3PrimaryPropagationPlan{}, false, err
	}
	return plan, true, nil
}

// prepareBINDV3PrimaryPropagationAt is an idempotent notification kick used
// after the target generation is already durable and locally active. A lost
// response can therefore repeat the exact same catalog/member notifications.
func prepareBINDV3PrimaryPropagationAt(
	ctx context.Context,
	plan dnsV3PrimaryPropagationPlan,
	run bindControlRunner,
) error {
	if run == nil {
		return errors.New("BIND propagation control is unavailable")
	}
	if err := validateDNSV3PrimaryPropagationPlan(plan); err != nil {
		return err
	}
	runBounded := func(domain string) error {
		commandCtx, cancel := context.WithTimeout(
			context.WithoutCancel(ctx), dnsProbeTimeout,
		)
		defer cancel()
		return run(commandCtx, "notify", domain)
	}
	if err := runBounded(plan.Evidence.Domain); err != nil {
		return errors.New("BIND paired catalog notification failed")
	}
	if !plan.Changed.Delete {
		if err := runBounded(plan.Changed.Domain); err != nil {
			return errors.New("BIND paired member notification failed")
		}
	}
	return nil
}

func completeManagedBINDV3Propagation(
	ctx context.Context,
	tree binddns.VerifiedTree,
	domain string,
) error {
	return completeManagedBINDV3PropagationAt(
		ctx, tree, domain, trustedBINDControl,
		completeDNSV3PrimaryPropagation,
	)
}

type bindPrimaryPropagationCompleter func(
	context.Context, dnsV3PrimaryPropagationPlan,
) error

func completeManagedBINDV3PropagationAt(
	ctx context.Context,
	tree binddns.VerifiedTree,
	domain string,
	run bindControlRunner,
	complete bindPrimaryPropagationCompleter,
) error {
	plan, primary, err := bindV3PrimaryPropagationPlan(tree, domain)
	if err != nil || !primary {
		return err
	}
	if complete == nil {
		return errors.New("BIND peer propagation proof is unavailable")
	}
	if err := prepareBINDV3PrimaryPropagationAt(
		ctx, plan, run,
	); err != nil {
		return err
	}
	return complete(ctx, plan)
}

func expectedBINDTreeAuthorities(
	tree binddns.VerifiedTree,
) ([]expectedDNSZoneAuthority, error) {
	receipt := tree.CurrentReceipt()
	expected := make([]expectedDNSZoneAuthority, 0, len(receipt.Zones))
	for _, zoneReceipt := range receipt.Zones {
		verifiedReceipt, data, found := tree.Zone(zoneReceipt.Domain)
		if !found || verifiedReceipt != zoneReceipt {
			return nil, errors.New("BIND verified tree zone receipt changed")
		}
		authority, err := expectedDNSZoneAuthorityFromBINDTree(
			verifiedReceipt, data,
		)
		if err != nil {
			return nil, err
		}
		expected = append(expected, authority)
	}
	return expected, nil
}

type bindZoneAuthoritiesVerifier func(
	context.Context, []expectedDNSZoneAuthority,
) error

type bindPrimaryReadyVerifier func(
	context.Context, binddns.VerifiedTree,
) (bool, error)

type bindTargetAuthorityVerifier func(context.Context) error
type bindRollbackVerifier func(
	context.Context, binddns.VerifiedTree, binddns.Receipt,
) error
type bindStateMutation func() error

// applyVerifiedBINDV3GenerationAt owns the receipt-dependent portion of the
// Publisher.Switch callback. The daemon reload/runtime checks and LoadCurrent
// happen before this function. A rollback attempt can therefore only validate
// the exact prior tree and restore prior state; it cannot accidentally run the
// failed target commitment against that restored tree.
func applyVerifiedBINDV3GenerationAt(
	ctx context.Context,
	attempt int,
	tree binddns.VerifiedTree,
	previous, target binddns.Receipt,
	verifyTarget bindTargetAuthorityVerifier,
	verifyRollback bindRollbackVerifier,
	commitTarget, restorePrevious bindStateMutation,
) error {
	if attempt < 1 {
		return errors.New("BIND apply attempt is invalid")
	}
	if attempt > 1 {
		if verifyRollback == nil || restorePrevious == nil {
			return errors.New("BIND rollback verifier is unavailable")
		}
		// Publisher has already restored the prior pointer. Restore its matching
		// durable state before any potentially long pair proof so a crash cannot
		// leave the prior pointer paired with the target state receipt.
		if err := restorePrevious(); err != nil {
			return err
		}
		return verifyRollback(ctx, tree, previous)
	}
	if !reflect.DeepEqual(tree.CurrentReceipt(), target) {
		return errors.New("BIND activation did not select the exact target receipt")
	}
	if verifyTarget == nil || commitTarget == nil {
		return errors.New("BIND target verifier is unavailable")
	}
	if err := verifyTarget(ctx); err != nil {
		return err
	}
	return commitTarget()
}

// verifyRestoredBINDV3GenerationAt proves that Publisher.Switch selected and
// reloaded the exact prior content-addressed tree. It is deliberately separate
// from target validation: the rollback callback must never test the failed new
// commitment after the prior pointer has been restored.
func verifyRestoredBINDV3GenerationAt(
	ctx context.Context,
	tree binddns.VerifiedTree,
	previous binddns.Receipt,
	verifyLocal bindZoneAuthoritiesVerifier,
	verifyPrimary bindPrimaryReadyVerifier,
) error {
	current := tree.CurrentReceipt()
	if !reflect.DeepEqual(current, previous) {
		return errors.New("BIND rollback did not restore the exact prior receipt")
	}
	expected, err := expectedBINDTreeAuthorities(tree)
	if err != nil {
		return err
	}
	if verifyLocal == nil || verifyLocal(ctx, expected) != nil {
		return errors.New("BIND rollback did not restore exact local authority")
	}
	if current.Pairing == nil {
		return nil
	}
	if current.Pairing.Role != binddns.PairRolePrimary || verifyPrimary == nil {
		return errors.New("BIND rollback pairing authority is invalid")
	}
	ready, err := verifyPrimary(ctx, tree)
	if err != nil || !ready {
		return errors.New("BIND rollback did not restore exact paired authority")
	}
	return nil
}

func verifyRestoredBINDV3Generation(
	ctx context.Context,
	tree binddns.VerifiedTree,
	previous binddns.Receipt,
) error {
	return verifyRestoredBINDV3GenerationAt(
		ctx, tree, previous, verifyDNSZoneAuthorities, bindPrimaryPairReady,
	)
}
