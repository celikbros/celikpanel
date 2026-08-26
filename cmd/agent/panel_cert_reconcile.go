package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	panelCertificateActivationInterval = 10 * time.Second
	panelCertificateLoopbackAddress    = "127.0.0.1:2083"
	panelCertificateActivationKind     = "panel-certificate-activation"
)

var errPanelCertificateActivationBusy = errors.New(
	"panel certificate activation is waiting for the durable mutation lease",
)

var (
	panelCertificateActivationReadState   = readPanelCertificateActivationState
	panelCertificateActivationWriteState  = writePanelCertificateActivationState
	panelCertificateActivationRemoveState = removePanelCertificateActivationState
	panelCertificateActivationReadSource  = readPanelCertificateSource

	// Linux replaces this fallback during package initialization so the exact
	// bytes bound into the durable intent are the bytes atomically published.
	// Unsupported platforms fail earlier in their activation-state backend.
	panelCertificateActivationPublishMaterial = func(
		domain, tlsDir string,
		certificate, privateKey []byte,
	) error {
		return panelCertInstallFiles(domain, tlsDir)
	}
)

type panelCertificateActivationController struct {
	once sync.Once
	wake chan struct{}
}

var globalPanelCertificateActivationController = &panelCertificateActivationController{wake: make(chan struct{}, 1)}

func startPanelCertificateActivationReconciler(
	manager *serviceMutationManager,
) {
	if manager == nil {
		log.Printf("Panel certificate activation reconciler is unavailable: durable mutation manager is nil")
		return
	}
	globalPanelCertificateActivationController.once.Do(func() {
		go runPanelCertificateActivationReconciler(
			context.Background(),
			manager,
			globalPanelCertificateActivationController.wake,
		)
	})
}

func wakePanelCertificateActivationReconciler() {
	select {
	case globalPanelCertificateActivationController.wake <- struct{}{}:
	default:
	}
}

func runPanelCertificateActivationReconciler(
	ctx context.Context,
	manager *serviceMutationManager,
	wake <-chan struct{},
) {
	reconcile := func() {
		if err := reconcilePanelCertificateActivationOnce(ctx, manager); err != nil &&
			!errors.Is(err, errPanelCertificateActivationBusy) {
			log.Printf("Panel certificate activation remains pending: %v", err)
		}
	}
	reconcile()
	ticker := time.NewTicker(panelCertificateActivationInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			reconcile()
		case <-wake:
			reconcile()
		}
	}
}

// reconcilePanelCertificateActivationOnce owns a fresh durable service
// mutation lease before taking the publication lock. This lock order is also
// used by interactive issuance, so an update can never overlook a privileged
// restart or race a certificate publication.
func reconcilePanelCertificateActivationOnce(
	ctx context.Context,
	manager *serviceMutationManager,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if manager == nil {
		return errors.New("durable service mutation manager is unavailable")
	}
	blocked, gateErr := manager.releaseTransactionBlocksMutations()
	if gateErr != nil {
		return errors.Join(
			errPanelCertificateActivationBusy,
			fmt.Errorf("verify panel certificate release transaction gate: %w", gateErr),
		)
	}
	if blocked {
		return errPanelCertificateActivationBusy
	}
	target, ready, err := inspectPanelCertificateActivationCandidate(ctx)
	if err != nil {
		return err
	}
	if !ready {
		return nil
	}

	requestID, err := newMutationOwnerID()
	if err != nil {
		return fmt.Errorf("create panel certificate activation request identity: %w", err)
	}
	ownerID, err := newMutationOwnerID()
	if err != nil {
		return fmt.Errorf("create panel certificate activation owner identity: %w", err)
	}
	begin := &ServiceMutationBeginRequest{
		RequestID: requestID,
		OwnerID:   ownerID,
		Kind:      panelCertificateActivationKind,
		Target:    target,
	}
	if _, err := manager.begin(begin); err != nil {
		if errors.Is(err, errServiceMutationBusy) ||
			errors.Is(err, errServiceMutationHostBusy) {
			return errPanelCertificateActivationBusy
		}
		return fmt.Errorf("begin durable panel certificate activation: %w", err)
	}

	binding := ServiceMutationBinding{
		MutationRequestID: requestID,
		MutationOwnerID:   ownerID,
	}
	stepCtx, finishStep, err := manager.acquireStep(
		binding,
		newServiceMutationStepClaim(
			serviceMutationStepActivatePanelCertificate,
			target,
			"",
			"activate",
		),
	)
	if err != nil {
		_, _ = manager.finish(&ServiceMutationFinishRequest{
			RequestID:   requestID,
			OwnerID:     ownerID,
			Success:     false,
			FailureCode: "panel_certificate_activation_step_failed",
			Message:     err.Error(),
		})
		return fmt.Errorf("acquire durable panel certificate activation step: %w", err)
	}

	heartbeatDone := make(chan struct{})
	go heartbeatPanelCertificateActivation(
		manager,
		requestID,
		ownerID,
		heartbeatDone,
	)
	reconcileErr := panelCertWithPublishLock(func() error {
		if err := preparePanelCertificateActivationLocked(stepCtx, target); err != nil {
			return err
		}
		return reconcilePanelCertificateActivationLocked(stepCtx)
	})
	close(heartbeatDone)
	finishStep()

	finishRequest := &ServiceMutationFinishRequest{
		RequestID: requestID,
		OwnerID:   ownerID,
		Success:   reconcileErr == nil,
	}
	if reconcileErr != nil {
		finishRequest.FailureCode = "panel_certificate_activation_failed"
		finishRequest.Message = sanitizePanelCertificateActivationError(reconcileErr)
	}
	_, finishErr := manager.finish(finishRequest)
	if finishErr != nil {
		finishErr = fmt.Errorf("finish durable panel certificate activation: %w", finishErr)
	}
	return errors.Join(reconcileErr, finishErr)
}

func heartbeatPanelCertificateActivation(
	manager *serviceMutationManager,
	requestID, ownerID string,
	done <-chan struct{},
) {
	ticker := time.NewTicker(serviceMutationLeaseDuration / 3)
	defer ticker.Stop()
	for {
		select {
		case <-done:
			return
		case <-ticker.C:
			_, _ = manager.heartbeat(&ServiceMutationHeartbeatRequest{
				RequestID: requestID,
				OwnerID:   ownerID,
				Phase:     panelCertificateActivationKind,
			})
		}
	}
}

func reconcilePanelCertificateActivationLocked(ctx context.Context) error {
	state, found, err := panelCertificateActivationReadState()
	if err != nil {
		return fmt.Errorf("read locked panel certificate activation: %w", err)
	}
	if !found {
		return nil
	}
	if !panelCertificateActivationRetryReady(
		state,
		panelCertificateActivationNow().UTC(),
	) {
		return nil
	}
	if state.Origin == panelCertificateActivationOriginInteractive &&
		(state.Phase == panelCertificateActivationPendingSource ||
			state.Phase == panelCertificateActivationPendingPublish) {
		published, verifyErr := verifyPublishedPanelCertificateIssueReceipt(
			state.RequestID,
			state.Qualifier,
			state.Domain,
		)
		if verifyErr != nil {
			return verifyErr
		}
		if !published {
			// Interactive publication belongs only to its exact mutation gate.
			return nil
		}
		if state.Phase != panelCertificateActivationPendingPublish {
			return errors.New(
				"published interactive panel certificate requires startup recovery",
			)
		}
		state, err = panelCertificateActivationNextPhase(
			state,
			panelCertificateActivationPendingRestart,
		)
		if err != nil {
			return err
		}
		if err := panelCertificateActivationWriteState(state); err != nil {
			return fmt.Errorf(
				"persist published interactive panel certificate: %w", err,
			)
		}
	}

	if state.Phase == panelCertificateActivationPendingSource ||
		state.Phase == panelCertificateActivationPendingPublish {
		certificate, privateKey, leafDER, notAfter, sourceErr :=
			panelCertificateActivationReadSource(state.Domain)
		if sourceErr != nil {
			return retainPanelCertificateActivationFailure(state, sourceErr)
		}
		bound, bindErr := bindPanelCertificateActivationMaterial(
			state,
			leafDER,
			notAfter,
		)
		if bindErr != nil {
			return retainPanelCertificateActivationFailure(state, bindErr)
		}
		state = clearPanelCertificateActivationFailure(bound)
		if err := panelCertificateActivationWriteState(state); err != nil {
			return fmt.Errorf("bind durable panel certificate activation source: %w", err)
		}
		if err := panelCertEnsureRenewal(ctx); err != nil {
			return retainPanelCertificateActivationFailure(state, err)
		}
		if err := panelCertWriteDeployHook(state.Domain, managedPanelTLSDir); err != nil {
			return retainPanelCertificateActivationFailure(state, err)
		}
		if err := panelCertificateActivationPublishMaterial(
			state.Domain,
			managedPanelTLSDir,
			certificate,
			privateKey,
		); err != nil {
			return retainPanelCertificateActivationFailure(state, err)
		}
		state, err = panelCertificateActivationNextPhase(
			state,
			panelCertificateActivationPendingRestart,
		)
		if err != nil {
			return err
		}
		if err := panelCertificateActivationWriteState(state); err != nil {
			return fmt.Errorf("persist pending panel restart: %w", err)
		}
	}

	if state.Phase == panelCertificateActivationPendingRestart {
		output, restartErr := panelCertRunMutationCommand(
			ctx,
			panelCertSystemdTimeout,
			"systemctl",
			"restart",
			"celikpanel-panel",
		)
		if restartErr != nil {
			return retainPanelCertificateActivationFailure(
				state,
				panelCertCommandError("restart panel after certificate publish", output, restartErr),
			)
		}
		state, err = panelCertificateActivationNextPhase(
			state,
			panelCertificateActivationPendingVerify,
		)
		if err != nil {
			return err
		}
		if err := panelCertificateActivationWriteState(state); err != nil {
			return fmt.Errorf("persist pending served-certificate verification: %w", err)
		}
	}

	if state.Phase != panelCertificateActivationPendingVerify {
		return fmt.Errorf("unsupported panel certificate activation phase %q", state.Phase)
	}
	output, activeErr := panelCertRunMutationCommand(
		ctx,
		panelCertSystemdTimeout,
		"systemctl",
		"is-active",
		"--quiet",
		"celikpanel-panel",
	)
	if activeErr != nil {
		return retainPanelCertificateActivationFailure(
			state,
			panelCertCommandError("verify active panel service", output, activeErr),
		)
	}
	if err := panelCertificateActivationVerifyServed(
		ctx,
		panelCertificateLoopbackAddress,
		state.Domain,
		state.LeafSHA256,
	); err != nil {
		return retainPanelCertificateActivationFailure(state, err)
	}
	if err := panelCertificateActivationRemoveState(); err != nil {
		return fmt.Errorf("remove verified panel certificate activation: %w", err)
	}
	return nil
}

func panelCertificateActivationNextPhase(
	state panelCertificateActivationState,
	phase panelCertificateActivationPhase,
) (panelCertificateActivationState, error) {
	state = clearPanelCertificateActivationFailure(state)
	return panelCertificateActivationWithPhase(state, phase)
}

func clearPanelCertificateActivationFailure(
	state panelCertificateActivationState,
) panelCertificateActivationState {
	state.Attempts = 0
	state.LastAttemptAt = nil
	state.LastError = ""
	return state
}

func retainPanelCertificateActivationFailure(
	state panelCertificateActivationState,
	failure error,
) error {
	failed, err := panelCertificateActivationFailure(
		state,
		panelCertificateActivationNow().UTC(),
		failure,
	)
	if err != nil {
		return errors.Join(failure, err)
	}
	if err := panelCertificateActivationWriteState(failed); err != nil {
		return errors.Join(
			failure,
			fmt.Errorf("retain failed panel certificate activation: %w", err),
		)
	}
	return failure
}

func beginPanelCertificateIssuanceLocked(
	domain string,
) (panelCertificateActivationState, error) {
	state, err := newPanelCertificateActivationState(domain)
	if err != nil {
		return panelCertificateActivationState{}, err
	}
	existing, found, err := panelCertificateActivationReadState()
	if err != nil {
		return panelCertificateActivationState{}, err
	}
	if found {
		// A source-less intent means the previous request stopped before it
		// produced certificate material. IssuePanelCertificate reaches this
		// point while holding both the durable mutation lease and Certbot's
		// per-site lock, so reclaiming that exact same-domain intent is safe.
		// Never overwrite an intent for another domain or one that has already
		// bound certificate material and advanced to publication.
		if existing.Domain != state.Domain ||
			existing.LineageName != state.LineageName ||
			existing.Phase != panelCertificateActivationPendingSource {
			return panelCertificateActivationState{}, fmt.Errorf(
				"%w for %s in phase %s",
				errPanelCertificateActivationPending,
				existing.Domain,
				existing.Phase,
			)
		}
	}
	if err := panelCertificateActivationWriteState(state); err != nil {
		return panelCertificateActivationState{}, fmt.Errorf(
			"persist pending panel certificate source: %w",
			err,
		)
	}
	return state, nil
}

func beginInteractivePanelCertificateIssuanceLocked(
	domain, requestID, qualifier string,
) (panelCertificateActivationState, error) {
	state, err := newInteractivePanelCertificateActivationState(
		domain, requestID, qualifier,
	)
	if err != nil {
		return panelCertificateActivationState{}, err
	}
	existing, found, err := panelCertificateActivationReadState()
	if err != nil {
		return panelCertificateActivationState{}, err
	}
	if found && (existing.Origin != state.Origin ||
		existing.RequestID != state.RequestID ||
		existing.Qualifier != state.Qualifier ||
		existing.Domain != state.Domain ||
		existing.LineageName != state.LineageName ||
		existing.Phase != panelCertificateActivationPendingSource) {
		return panelCertificateActivationState{}, fmt.Errorf(
			"%w for %s in phase %s",
			errPanelCertificateActivationPending,
			existing.Domain,
			existing.Phase,
		)
	}
	if err := panelCertificateActivationWriteState(state); err != nil {
		return panelCertificateActivationState{}, fmt.Errorf(
			"persist pending interactive panel certificate source: %w", err,
		)
	}
	return state, nil
}

func clearPanelCertificateIssuanceIntentLocked(
	expected panelCertificateActivationState,
) error {
	if err := requirePanelCertificateIssuanceIntentLocked(expected); err != nil {
		return err
	}
	if err := panelCertificateActivationRemoveState(); err != nil {
		return fmt.Errorf("remove failed panel certificate intent: %w", err)
	}
	return nil
}

func requirePanelCertificateIssuanceIntentLocked(
	expected panelCertificateActivationState,
) error {
	state, found, err := panelCertificateActivationReadState()
	if err != nil {
		return fmt.Errorf("inspect failed panel certificate intent: %w", err)
	}
	if !found {
		return errors.New("failed panel certificate intent disappeared")
	}
	if state.Version != expected.Version ||
		state.Origin != expected.Origin ||
		state.RequestID != expected.RequestID ||
		state.Qualifier != expected.Qualifier ||
		state.Domain != expected.Domain ||
		state.LineageName != expected.LineageName ||
		state.Phase != panelCertificateActivationPendingSource ||
		state.LeafSHA256 != "" ||
		state.NotAfter != nil ||
		state.Attempts != 0 ||
		state.LastAttemptAt != nil ||
		state.LastError != "" {
		return errors.New(
			"failed panel certificate intent changed and was retained",
		)
	}
	return nil
}

// enqueueRenewedPanelCertificateActivation is called by Certbot's deploy
// hook. Only the deterministic lineage for the root-authenticated active panel
// identity may replace a same-domain pending intent. Unrelated lineages are
// deliberately successful no-ops so a global Certbot renewal is not broken.
func enqueueRenewedPanelCertificateActivation(
	lineageName string,
) (bool, error) {
	lineageName = strings.ToLower(strings.TrimSpace(lineageName))
	if !validPanelCertLineage.MatchString(lineageName) {
		return false, errors.New("invalid panel certificate renewal lineage")
	}
	queued := false
	err := panelCertWithPublishLock(func() error {
		domain, found, err := panelCertActiveIdentity(managedPanelTLSDir)
		if err != nil {
			return err
		}
		if !found || panelCertLineageName(domain) != lineageName {
			return nil
		}
		existing, exists, err := panelCertificateActivationReadState()
		if err != nil {
			return err
		}
		// Certbot runs the deploy hook during an interactive V2 issuance too.
		// That hook must not replace the exact request/qualifier-bound intent
		// which the in-flight issuer will bind to the newly issued leaf.
		if exists && existing.Origin == panelCertificateActivationOriginInteractive {
			return nil
		}
		if exists && existing.Domain != domain {
			return nil
		}
		state, err := newPanelCertificateActivationState(domain)
		if err != nil {
			return err
		}
		if err := panelCertificateActivationWriteState(state); err != nil {
			return err
		}
		queued = true
		return nil
	})
	return queued, err
}

func inspectPanelCertificateActivationCandidate(
	ctx context.Context,
) (string, bool, error) {
	domain := ""
	ready := false
	err := panelCertWithPublishLock(func() error {
		state, found, err := panelCertificateActivationReadState()
		if err != nil {
			return fmt.Errorf("read durable panel certificate activation: %w", err)
		}
		if found {
			if panelCertificateActivationRetryReady(
				state,
				panelCertificateActivationNow().UTC(),
			) {
				domain = state.Domain
				ready = true
			}
			return nil
		}
		domain, ready, err = inspectPanelCertificateActivationDriftLocked(ctx)
		return err
	})
	return domain, ready, err
}

func inspectPanelCertificateActivationDriftLocked(
	ctx context.Context,
) (string, bool, error) {
	domain, active, err := panelCertActiveIdentity(managedPanelTLSDir)
	if err != nil {
		return "", false, err
	}
	if !active {
		return "", false, nil
	}
	_, _, leafDER, _, err := panelCertificateActivationReadSource(domain)
	if err != nil {
		// Only a genuinely absent lineage means there is no drift to
		// reconcile. Permission, ownership, chain, key, expiry and parsing
		// failures are security/integrity errors and must stay visible.
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf(
			"inspect active panel certificate source: %w",
			err,
		)
	}
	fingerprint := panelCertificateLeafSHA256(leafDER)
	if err := panelCertificateActivationVerifyServed(
		ctx,
		panelCertificateLoopbackAddress,
		domain,
		fingerprint,
	); err == nil {
		return "", false, nil
	}
	return domain, true, nil
}

func preparePanelCertificateActivationLocked(
	ctx context.Context,
	expectedDomain string,
) error {
	state, found, err := panelCertificateActivationReadState()
	if err != nil {
		return fmt.Errorf("read locked panel certificate activation: %w", err)
	}
	if found {
		if state.Domain != expectedDomain {
			return errors.New("panel certificate activation candidate changed before mutation lease")
		}
		return nil
	}
	domain, drifted, err := inspectPanelCertificateActivationDriftLocked(ctx)
	if err != nil {
		return err
	}
	if !drifted {
		return nil
	}
	if domain != expectedDomain {
		return errors.New("panel certificate activation identity changed before mutation lease")
	}
	state, err = newPanelCertificateActivationState(domain)
	if err != nil {
		return err
	}
	if err := panelCertificateActivationWriteState(state); err != nil {
		return err
	}
	return nil
}
