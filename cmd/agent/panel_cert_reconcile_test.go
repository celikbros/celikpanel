//go:build linux

package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

type panelCertificateActivationMemoryStore struct {
	mu      sync.Mutex
	state   *panelCertificateActivationState
	writes  []panelCertificateActivationPhase
	removes int
}

func clonePanelCertificateActivationState(
	state panelCertificateActivationState,
) panelCertificateActivationState {
	clone := state
	if state.NotAfter != nil {
		value := *state.NotAfter
		clone.NotAfter = &value
	}
	if state.LastAttemptAt != nil {
		value := *state.LastAttemptAt
		clone.LastAttemptAt = &value
	}
	return clone
}

func installPanelCertificateActivationMemoryStore(
	t *testing.T,
) *panelCertificateActivationMemoryStore {
	t.Helper()
	store := &panelCertificateActivationMemoryStore{}
	originalRead := panelCertificateActivationReadState
	originalWrite := panelCertificateActivationWriteState
	originalRemove := panelCertificateActivationRemoveState
	t.Cleanup(func() {
		panelCertificateActivationReadState = originalRead
		panelCertificateActivationWriteState = originalWrite
		panelCertificateActivationRemoveState = originalRemove
	})
	panelCertificateActivationReadState = func() (
		panelCertificateActivationState, bool, error,
	) {
		store.mu.Lock()
		defer store.mu.Unlock()
		if store.state == nil {
			return panelCertificateActivationState{}, false, nil
		}
		return clonePanelCertificateActivationState(*store.state), true, nil
	}
	panelCertificateActivationWriteState = func(
		state panelCertificateActivationState,
	) error {
		if err := validatePanelCertificateActivationState(state); err != nil {
			return err
		}
		store.mu.Lock()
		defer store.mu.Unlock()
		clone := clonePanelCertificateActivationState(state)
		store.state = &clone
		store.writes = append(store.writes, state.Phase)
		return nil
	}
	panelCertificateActivationRemoveState = func() error {
		store.mu.Lock()
		defer store.mu.Unlock()
		store.state = nil
		store.removes++
		return nil
	}
	return store
}

func (store *panelCertificateActivationMemoryStore) seed(
	t *testing.T,
	state panelCertificateActivationState,
) {
	t.Helper()
	if err := validatePanelCertificateActivationState(state); err != nil {
		t.Fatal(err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	clone := clonePanelCertificateActivationState(state)
	store.state = &clone
}

func (store *panelCertificateActivationMemoryStore) snapshot() (
	panelCertificateActivationState,
	bool,
) {
	store.mu.Lock()
	defer store.mu.Unlock()
	if store.state == nil {
		return panelCertificateActivationState{}, false
	}
	return clonePanelCertificateActivationState(*store.state), true
}

func boundPanelCertificateActivationTestState(
	t *testing.T,
	phase panelCertificateActivationPhase,
) panelCertificateActivationState {
	t.Helper()
	state, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	state, err = bindPanelCertificateActivationMaterial(
		state,
		[]byte("exact leaf DER"),
		time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC),
	)
	if err != nil {
		t.Fatal(err)
	}
	state, err = panelCertificateActivationWithPhase(state, phase)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func installPanelCertificateReconcileTestSeams(t *testing.T) {
	t.Helper()
	originalSource := panelCertificateActivationReadSource
	originalPublish := panelCertificateActivationPublishMaterial
	originalRenewal := panelCertEnsureRenewal
	originalHook := panelCertWriteDeployHook
	originalLock := panelCertWithPublishLock
	originalRun := panelCertRunMutationCommand
	originalVerify := panelCertificateActivationVerifyServed
	originalActive := panelCertActiveIdentity
	originalNow := panelCertificateActivationNow
	t.Cleanup(func() {
		panelCertificateActivationReadSource = originalSource
		panelCertificateActivationPublishMaterial = originalPublish
		panelCertEnsureRenewal = originalRenewal
		panelCertWriteDeployHook = originalHook
		panelCertWithPublishLock = originalLock
		panelCertRunMutationCommand = originalRun
		panelCertificateActivationVerifyServed = originalVerify
		panelCertActiveIdentity = originalActive
		panelCertificateActivationNow = originalNow
	})
	panelCertWithPublishLock = func(action func() error) error { return action() }
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "", false, nil
	}
	panelCertificateActivationNow = func() time.Time {
		return time.Date(2029, time.January, 2, 3, 4, 5, 0, time.UTC)
	}
}

func TestRenewalDeployHookPreservesSameDomainInteractiveIssuanceIntent(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "panel.example.test", true, nil
	}
	intent, err := newInteractivePanelCertificateActivationState(
		"panel.example.test",
		"abababababababababababababababab",
		"panel-certificate-issue/v1:sha256:"+strings.Repeat("c", 64),
	)
	if err != nil {
		t.Fatal(err)
	}
	store.seed(t, intent)

	queued, err := enqueueRenewedPanelCertificateActivation(
		panelCertLineageName(intent.Domain),
	)
	if err != nil {
		t.Fatal(err)
	}
	if queued {
		t.Fatal("renewal deploy hook replaced an active interactive issuance intent")
	}
	retained, found := store.snapshot()
	if !found || retained != intent {
		t.Fatalf("interactive intent changed: retained=%+v found=%v want=%+v", retained, found, intent)
	}
	if len(store.writes) != 0 {
		t.Fatalf("renewal deploy hook wrote phases %v", store.writes)
	}
}

func TestBeginPanelCertificateIssuanceReclaimsSameDomainPendingSource(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	existing, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2029, time.January, 2, 3, 4, 5, 0, time.UTC)
	existing.Attempts = 7
	existing.LastAttemptAt = &now
	existing.LastError = "previous source lookup failed"
	store.seed(t, existing)

	got, err := beginPanelCertificateIssuanceLocked("PANEL.EXAMPLE.TEST")
	if err != nil {
		t.Fatal(err)
	}
	want, err := newPanelCertificateActivationState("panel.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reclaimed intent = %+v, want %+v", got, want)
	}
	persisted, found := store.snapshot()
	if !found || !reflect.DeepEqual(persisted, want) {
		t.Fatalf("persisted reclaimed intent = %+v found=%v, want %+v", persisted, found, want)
	}
}

func TestBeginPanelCertificateIssuancePreservesOtherDomainIntent(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	existing, err := newPanelCertificateActivationState("other.example.test")
	if err != nil {
		t.Fatal(err)
	}
	store.seed(t, existing)

	_, err = beginPanelCertificateIssuanceLocked("panel.example.test")
	if !errors.Is(err, errPanelCertificateActivationPending) {
		t.Fatalf("error = %v, want activation-pending refusal", err)
	}
	persisted, found := store.snapshot()
	if !found || !reflect.DeepEqual(persisted, existing) {
		t.Fatalf("other-domain intent changed: got=%+v found=%v want=%+v", persisted, found, existing)
	}
}

func TestBeginPanelCertificateIssuancePreservesAdvancedIntent(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	existing := boundPanelCertificateActivationTestState(
		t, panelCertificateActivationPendingPublish,
	)
	store.seed(t, existing)

	_, err := beginPanelCertificateIssuanceLocked("panel.example.test")
	if !errors.Is(err, errPanelCertificateActivationPending) {
		t.Fatalf("error = %v, want activation-pending refusal", err)
	}
	persisted, found := store.snapshot()
	if !found || !reflect.DeepEqual(persisted, existing) {
		t.Fatalf("advanced intent changed: got=%+v found=%v want=%+v", persisted, found, existing)
	}
}

func TestPanelCertificateActivationReplaysEveryCrashPhase(t *testing.T) {
	for _, phase := range []panelCertificateActivationPhase{
		panelCertificateActivationPendingPublish,
		panelCertificateActivationPendingRestart,
		panelCertificateActivationPendingVerify,
	} {
		t.Run(string(phase), func(t *testing.T) {
			store := installPanelCertificateActivationMemoryStore(t)
			installPanelCertificateReconcileTestSeams(t)
			state := boundPanelCertificateActivationTestState(t, phase)
			store.seed(t, state)

			published := 0
			panelCertificateActivationReadSource = func(string) (
				[]byte, []byte, []byte, time.Time, error,
			) {
				return []byte("certificate"), []byte("private key"),
					[]byte("exact leaf DER"), *state.NotAfter, nil
			}
			panelCertificateActivationPublishMaterial = func(
				string, string, []byte, []byte,
			) error {
				published++
				return nil
			}
			panelCertEnsureRenewal = func(context.Context) error { return nil }
			panelCertWriteDeployHook = func(string, string) error { return nil }

			restarts := 0
			activeChecks := 0
			panelCertRunMutationCommand = func(
				ctx context.Context,
				_ time.Duration,
				name string,
				args ...string,
			) ([]byte, error) {
				if ctx.Value(serviceMutationExecutionTrackerKey{}) == nil {
					t.Fatal("privileged command lacks durable mutation process tracker")
				}
				if name != "systemctl" {
					t.Fatalf("unexpected command %q", name)
				}
				switch strings.Join(args, " ") {
				case "restart celikpanel-panel":
					restarts++
				case "is-active --quiet celikpanel-panel":
					activeChecks++
				default:
					t.Fatalf("unexpected systemctl args %#v", args)
				}
				return nil, nil
			}
			panelCertificateActivationVerifyServed = func(
				_ context.Context, address, domain, fingerprint string,
			) error {
				if address != panelCertificateLoopbackAddress ||
					domain != state.Domain || fingerprint != state.LeafSHA256 {
					t.Fatalf("verification target = %q %q %q", address, domain, fingerprint)
				}
				return nil
			}

			manager, _ := newMutationTestManager(t)
			if err := reconcilePanelCertificateActivationOnce(
				context.Background(), manager,
			); err != nil {
				t.Fatal(err)
			}
			if _, found := store.snapshot(); found {
				t.Fatal("activation state survived exact served-certificate proof")
			}
			wantPublish, wantRestart := 0, 0
			if phase == panelCertificateActivationPendingPublish {
				wantPublish = 1
			}
			if phase != panelCertificateActivationPendingVerify {
				wantRestart = 1
			}
			if published != wantPublish || restarts != wantRestart || activeChecks != 1 {
				t.Fatalf(
					"phase=%s publish=%d restart=%d active=%d",
					phase, published, restarts, activeChecks,
				)
			}
		})
	}
}

func TestPanelCertificateActivationBusyLeaseDoesNotConsumeRetry(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	want := boundPanelCertificateActivationTestState(
		t, panelCertificateActivationPendingRestart,
	)
	store.seed(t, want)
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		t.Fatal("busy reconciler executed a privileged command")
		return nil, nil
	}

	manager, _ := newMutationTestManager(t)
	beginMutationTestJob(t, manager)
	err := reconcilePanelCertificateActivationOnce(context.Background(), manager)
	if !errors.Is(err, errPanelCertificateActivationBusy) {
		t.Fatalf("busy reconciliation error = %v", err)
	}
	got, found := store.snapshot()
	if !found || !reflect.DeepEqual(got, want) {
		t.Fatalf("busy lease changed durable state: got=%+v found=%v", got, found)
	}
}

func TestPanelCertificateActivationCompletionPendingGateBlocksBeforeDiscovery(t *testing.T) {
	installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	manager, root := newMutationTestManager(t)
	transactionRoot := filepath.Join(root, "release-transaction")
	if err := os.Mkdir(transactionRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(transactionRoot, "completion.pending")
	if err := os.WriteFile(marker, []byte("pending\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager.releaseTransactionPresent = func() (bool, error) {
		return persistentReleaseTransactionPresent(transactionRoot)
	}
	panelCertActiveIdentity = func(string) (string, bool, error) {
		t.Fatal("completion-pending reconciler inspected certificate drift")
		return "", false, nil
	}

	if err := reconcilePanelCertificateActivationOnce(
		context.Background(), manager,
	); !errors.Is(err, errPanelCertificateActivationBusy) {
		t.Fatalf("completion-pending reconciliation error = %v", err)
	}
	if err := os.Remove(marker); err != nil {
		t.Fatal(err)
	}
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "", false, nil
	}
	if err := reconcilePanelCertificateActivationOnce(
		context.Background(), manager,
	); err != nil {
		t.Fatalf("marker-free idle reconciliation = %v", err)
	}
}

func TestPanelCertificateActivationDriftIsNotPublishedBeforeMutationGate(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	manager, _ := newMutationTestManager(t)
	gateChecks := 0
	manager.releaseTransactionPresent = func() (bool, error) {
		gateChecks++
		return gateChecks >= 2, nil
	}
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "panel.example.test", true, nil
	}
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		return []byte("certificate"), []byte("private key"),
			[]byte("exact leaf DER"),
			time.Date(2030, time.January, 2, 3, 4, 5, 0, time.UTC),
			nil
	}
	panelCertificateActivationVerifyServed = func(
		context.Context, string, string, string,
	) error {
		return errors.New("listener still serves the previous leaf")
	}

	if err := reconcilePanelCertificateActivationOnce(
		context.Background(), manager,
	); !errors.Is(err, errPanelCertificateActivationBusy) {
		t.Fatalf("gate-race reconciliation error = %v", err)
	}
	if gateChecks != 2 {
		t.Fatalf("release gate checks = %d, want preflight plus begin", gateChecks)
	}
	if _, found := store.snapshot(); found || len(store.writes) != 0 {
		t.Fatalf("drift was published before mutation lease: writes=%v", store.writes)
	}
	if len(manager.ledger.Jobs) != 0 || manager.ledger.ActiveRequestID != "" {
		t.Fatalf("blocked drift changed mutation ledger: %+v", manager.ledger)
	}
}

func TestPanelCertificateActivationRetainsUntilExactListenerProof(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	state := boundPanelCertificateActivationTestState(
		t, panelCertificateActivationPendingVerify,
	)
	store.seed(t, state)
	now := time.Date(2029, time.January, 2, 3, 4, 5, 0, time.UTC)
	panelCertificateActivationNow = func() time.Time { return now }
	panelCertRunMutationCommand = func(
		context.Context, time.Duration, string, ...string,
	) ([]byte, error) {
		return nil, nil
	}
	verifyFailure := errors.New("listener still serves old leaf")
	panelCertificateActivationVerifyServed = func(
		context.Context, string, string, string,
	) error {
		return verifyFailure
	}

	manager, _ := newMutationTestManager(t)
	if err := reconcilePanelCertificateActivationOnce(
		context.Background(), manager,
	); !errors.Is(err, verifyFailure) {
		t.Fatalf("verification error = %v", err)
	}
	failed, found := store.snapshot()
	if !found || failed.Attempts != 1 || failed.LastAttemptAt == nil {
		t.Fatalf("failed verification did not retain retry state: %+v", failed)
	}

	now = panelCertificateActivationRetryAt(failed)
	panelCertificateActivationVerifyServed = func(
		_ context.Context, address, domain, fingerprint string,
	) error {
		if address != panelCertificateLoopbackAddress ||
			domain != state.Domain || fingerprint != state.LeafSHA256 {
			t.Fatalf("verification target = %q %q %q", address, domain, fingerprint)
		}
		return nil
	}
	if err := reconcilePanelCertificateActivationOnce(
		context.Background(), manager,
	); err != nil {
		t.Fatal(err)
	}
	if _, found := store.snapshot(); found {
		t.Fatal("verified activation state was not removed")
	}
}

func TestPanelCertificateDriftDiscoveryPropagatesIntegrityFailure(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "panel.example.test", true, nil
	}
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		return nil, nil, nil, time.Time{}, os.ErrPermission
	}
	manager, _ := newMutationTestManager(t)
	err := reconcilePanelCertificateActivationOnce(context.Background(), manager)
	if !errors.Is(err, os.ErrPermission) {
		t.Fatalf("integrity failure = %v, want permission error", err)
	}
	if _, found := store.snapshot(); found {
		t.Fatal("integrity failure created an unbound activation intent")
	}
}

func TestPanelCertificateDriftDiscoveryIgnoresOnlyMissingLineage(t *testing.T) {
	store := installPanelCertificateActivationMemoryStore(t)
	installPanelCertificateReconcileTestSeams(t)
	panelCertActiveIdentity = func(string) (string, bool, error) {
		return "panel.example.test", true, nil
	}
	panelCertificateActivationReadSource = func(string) (
		[]byte, []byte, []byte, time.Time, error,
	) {
		return nil, nil, nil, time.Time{}, os.ErrNotExist
	}
	manager, _ := newMutationTestManager(t)
	if err := reconcilePanelCertificateActivationOnce(
		context.Background(), manager,
	); err != nil {
		t.Fatal(err)
	}
	if _, found := store.snapshot(); found {
		t.Fatal("missing lineage created an activation intent")
	}
}
