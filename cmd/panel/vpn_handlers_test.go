package main

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/transport"
)

var vpnTestPeerSequence atomic.Int64

func vpnTestCanonicalKey(sequence int64, purpose byte) string {
	raw := make([]byte, 32)
	raw[0] = purpose
	binary.BigEndian.PutUint64(raw[24:], uint64(sequence))
	return base64.StdEncoding.EncodeToString(raw)
}

func newVPNSecurityFixture(
	t *testing.T,
) (serviceOperationTestFixture, int, int, int) {
	t.Helper()
	fixture := newServiceOperationTestFixture(t)
	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), "secrets.key"))
	if err != nil {
		t.Fatal(err)
	}
	fixture.panel.secrets = box

	insertUser := func(username string) int {
		result, err := fixture.database.GetDB().Exec(`
			INSERT INTO users (username,password_hash,email,role)
			VALUES (?, 'x', ?, 'customer')`,
			username, username+"@example.test",
		)
		if err != nil {
			t.Fatal(err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return int(id)
	}
	ownerID := insertUser("vpn-owner")
	otherID := insertUser("vpn-other")
	result, err := fixture.database.GetDB().Exec(`
		INSERT INTO subscriptions (owner_id, name, status)
		VALUES (?, 'VPN subscription', 'active')`, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	subscriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.GetDB().Exec(`
		INSERT INTO subscription_entitlements
			(subscription_id, product_id, status)
		VALUES (?, 'vpn', 'active')`, subscriptionID); err != nil {
		t.Fatal(err)
	}
	return fixture, ownerID, otherID, int(subscriptionID)
}

func insertVPNTestPeer(
	t *testing.T,
	fixture serviceOperationTestFixture,
	subscriptionID, createdBy int,
	name string,
	deliveryToken string,
	deliveryExpiry time.Time,
) int {
	t.Helper()
	sequence := vpnTestPeerSequence.Add(1)
	var tokenHash any
	var expiresAt any
	provisioningState := "issued"
	if deliveryToken != "" {
		tokenHash = vpnDeliveryTokenHash(deliveryToken)
		expiresAt = deliveryExpiry.UTC().Format("2006-01-02 15:04:05")
		provisioningState = "provisioning"
	}
	result, err := fixture.database.GetDB().Exec(`
		INSERT INTO vpn_peers
			(subscription_id, name, public_key, preshared_key, ip, created_by,
			 desired_state, sync_state, provisioning_state, delivery_token_hash,
			 delivery_expires_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'active', 'applied', ?, ?, ?, datetime('now'))`,
		subscriptionID, name, vpnTestCanonicalKey(sequence, 1),
		vpnTestCanonicalKey(sequence, 2),
		fmt.Sprintf("10.8.0.%d", 2+(sequence-1)%253), createdBy,
		provisioningState, tokenHash, expiresAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	return int(id)
}

func TestEncryptLegacyVPNPresharedKeysRejectsCorruptCiphertextWithoutPartialMigration(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	corruptID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, `corrupt-key`, ``, time.Time{},
	)
	legacyID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, `legacy-key`, ``, time.Time{},
	)
	const corrupt = `enc:v1:not-valid-ciphertext`
	const legacy = `legacy-preshared-key`
	if _, err := fixture.database.GetDB().Exec(
		`UPDATE vpn_peers SET preshared_key = ? WHERE id = ?`, corrupt, corruptID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.GetDB().Exec(
		`UPDATE vpn_peers SET preshared_key = ? WHERE id = ?`, legacy, legacyID,
	); err != nil {
		t.Fatal(err)
	}

	err := fixture.panel.encryptLegacyVPNPresharedKeys(context.Background())
	if err == nil {
		t.Fatal(`corrupt encrypted VPN key was accepted`)
	}
	if strings.Contains(err.Error(), corrupt) || strings.Contains(err.Error(), legacy) {
		t.Fatalf(`migration error leaked a VPN secret: %v`, err)
	}

	var storedCorrupt string
	var storedLegacy string
	if err := fixture.database.GetDB().QueryRow(
		`SELECT preshared_key FROM vpn_peers WHERE id = ?`, corruptID,
	).Scan(&storedCorrupt); err != nil {
		t.Fatal(err)
	}
	if err := fixture.database.GetDB().QueryRow(
		`SELECT preshared_key FROM vpn_peers WHERE id = ?`, legacyID,
	).Scan(&storedLegacy); err != nil {
		t.Fatal(err)
	}
	if storedCorrupt != corrupt || storedLegacy != legacy {
		t.Fatalf(
			`failed validation changed VPN keys: corrupt=%q legacy=%q`,
			storedCorrupt, storedLegacy,
		)
	}
}

func vpnCallerRequest(
	method, target, body string,
	callerID int,
	role string,
) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	return request.WithContext(context.WithValue(
		request.Context(), callerKey, &Caller{ID: callerID, Role: role},
	))
}

func TestVPNDeliveryAcknowledgementIsScopedExpiringAndSingleUse(t *testing.T) {
	fixture, ownerID, otherID, subscriptionID := newVPNSecurityFixture(t)
	token := "00112233445566778899aabbccddeeff"
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "owner-laptop",
		token, time.Now().Add(5*time.Minute),
	)

	ack := func(callerID int, role, receipt string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		fixture.panel.handleVPNPeerByID(
			recorder,
			vpnCallerRequest(
				http.MethodPost,
				fmt.Sprintf("/api/v1/vpn/peers/%d/ack", peerID),
				fmt.Sprintf(`{"delivery_token":%q}`, receipt),
				callerID,
				role,
			),
		)
		return recorder
	}

	first := ack(ownerID, roleCustomer, token)
	if first.Code != http.StatusOK {
		t.Fatalf("first ACK status=%d body=%s", first.Code, first.Body.String())
	}
	if first.Header().Get("Cache-Control") != "no-store, max-age=0" {
		t.Fatalf("ACK Cache-Control=%q", first.Header().Get("Cache-Control"))
	}
	var state string
	var tokenRows int
	if err := fixture.database.GetDB().QueryRow(`
		SELECT provisioning_state,
		       CASE WHEN delivery_token_hash IS NULL THEN 0 ELSE 1 END
		FROM vpn_peers WHERE id = ?`, peerID,
	).Scan(&state, &tokenRows); err != nil {
		t.Fatal(err)
	}
	if state != "issued" || tokenRows != 0 {
		t.Fatalf("issued state=%q retained-token=%d", state, tokenRows)
	}
	replay := ack(ownerID, roleCustomer, token)
	if replay.Code != http.StatusConflict {
		t.Fatalf("replay status=%d body=%s", replay.Code, replay.Body.String())
	}

	expiredToken := "ffeeddccbbaa99887766554433221100"
	expiredID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "expired-phone",
		expiredToken, time.Now().Add(-time.Minute),
	)
	expired := httptest.NewRecorder()
	fixture.panel.handleVPNPeerByID(
		expired,
		vpnCallerRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/vpn/peers/%d/ack", expiredID),
			fmt.Sprintf(`{"delivery_token":%q}`, expiredToken),
			ownerID,
			roleCustomer,
		),
	)
	if expired.Code != http.StatusConflict {
		t.Fatalf("expired ACK status=%d body=%s", expired.Code, expired.Body.String())
	}

	scopedToken := "0123456789abcdef0123456789abcdef"
	scopedID := insertVPNTestPeer(
		t, fixture, subscriptionID, otherID, "other-device",
		scopedToken, time.Now().Add(5*time.Minute),
	)
	denied := httptest.NewRecorder()
	fixture.panel.handleVPNPeerByID(
		denied,
		vpnCallerRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/vpn/peers/%d/ack", scopedID),
			fmt.Sprintf(`{"delivery_token":%q}`, scopedToken),
			ownerID,
			roleCustomer,
		),
	)
	if denied.Code != http.StatusNotFound {
		t.Fatalf("non-creator ACK status=%d body=%s", denied.Code, denied.Body.String())
	}
	admin := httptest.NewRecorder()
	fixture.panel.handleVPNPeerByID(
		admin,
		vpnCallerRequest(
			http.MethodPost,
			fmt.Sprintf("/api/v1/vpn/peers/%d/ack", scopedID),
			fmt.Sprintf(`{"delivery_token":%q}`, scopedToken),
			fixture.userID,
			roleAdmin,
		),
	)
	if admin.Code != http.StatusOK {
		t.Fatalf("admin ACK status=%d body=%s", admin.Code, admin.Body.String())
	}
}

func TestVPNEntitlementReconcileKeepsRetryableTombstoneOnSyncFailure(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "revoked-device", "", time.Time{},
	)
	if _, err := fixture.database.GetDB().Exec(`
		UPDATE subscription_entitlements
		SET status = 'suspended'
		WHERE subscription_id = ? AND product_id = 'vpn'`, subscriptionID); err != nil {
		t.Fatal(err)
	}
	fixture.agent.peerError = "simulated peer application failure"
	if err := fixture.panel.reconcileVPNEntitlements(context.Background()); err == nil {
		t.Fatal("reconcile succeeded despite agent failure")
	}
	var desiredState, syncState string
	if err := fixture.database.GetDB().QueryRow(`
		SELECT desired_state, sync_state FROM vpn_peers WHERE id = ?`, peerID,
	).Scan(&desiredState, &syncState); err != nil {
		t.Fatal(err)
	}
	if desiredState != "revoked" || syncState != "error" {
		t.Fatalf("failed revoke state=%s/%s, want revoked/error", desiredState, syncState)
	}

	fixture.agent.peerError = ""
	if err := fixture.panel.reconcileVPNEntitlements(context.Background()); err != nil {
		t.Fatalf("retry reconcile: %v", err)
	}
	var remaining int
	if err := fixture.database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM vpn_peers WHERE id = ?`, peerID,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("revoked peer remained after confirmed retry: %d", remaining)
	}
}

func TestRecordVPNSyncErrorSurvivesCancelledRequestContext(t *testing.T) {
	fixture, _, _, _ := newVPNSecurityFixture(t)
	const token = "claimed-sync-token"
	const generation = int64(7)
	if _, err := fixture.database.GetDB().Exec(`
		UPDATE vpn_sync_state
		SET lease_token = ?, lease_expires_at = datetime('now', '+2 minutes'),
		    status = 'pending', desired_generation = ?
		WHERE id = 1`, token, generation); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := fixture.panel.recordVPNSyncError(
		ctx,
		token,
		generation,
		errors.New("simulated transport failure"),
	); err != nil {
		t.Fatalf("record failure after request cancellation: %v", err)
	}
	var status string
	var lease *string
	if err := fixture.database.GetDB().QueryRow(`
		SELECT status, lease_token FROM vpn_sync_state WHERE id = 1`,
	).Scan(&status, &lease); err != nil {
		t.Fatal(err)
	}
	if status != "error" || lease != nil {
		t.Fatalf("failure state=%q lease=%v, want error/nil", status, lease)
	}
}

func TestRecordVPNSyncErrorReturnsLedgerFailure(t *testing.T) {
	fixture, _, _, _ := newVPNSecurityFixture(t)
	if _, err := fixture.database.GetDB().Exec(`DROP TABLE vpn_sync_state`); err != nil {
		t.Fatal(err)
	}
	if err := fixture.panel.recordVPNSyncError(
		context.Background(),
		"missing-ledger",
		0,
		errors.New("simulated transport failure"),
	); err == nil {
		t.Fatal("missing VPN synchronization ledger was silently accepted")
	}
}

func TestRevokeUndeliveredVPNPeerRemovesConfirmedPeer(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "undelivered-device", "", time.Time{},
	)
	if err := fixture.panel.revokeUndeliveredVPNPeer(
		context.Background(),
		int64(peerID),
		errors.New("response stream closed"),
	); err != nil {
		t.Fatalf("revoke undelivered peer: %v", err)
	}
	var remaining int
	if err := fixture.database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM vpn_peers WHERE id = ?`, peerID,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("undelivered peer remained after confirmed sync: %d", remaining)
	}
}

func TestRevokeUndeliveredVPNPeerKeepsErrorTombstoneWhenAgentFails(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "ambiguous-device", "", time.Time{},
	)
	fixture.agent.peerError = "simulated cleanup failure"
	if err := fixture.panel.revokeUndeliveredVPNPeer(
		context.Background(),
		int64(peerID),
		errors.New("response stream closed"),
	); err == nil {
		t.Fatal("agent cleanup failure was silently accepted")
	}
	var desiredState, syncState string
	if err := fixture.database.GetDB().QueryRow(`
		SELECT desired_state, sync_state FROM vpn_peers WHERE id = ?`, peerID,
	).Scan(&desiredState, &syncState); err != nil {
		t.Fatal(err)
	}
	if desiredState != "revoked" || syncState != "error" {
		t.Fatalf(
			"failed cleanup state=%s/%s, want revoked/error",
			desiredState,
			syncState,
		)
	}
}

type vpnCommitmentTestAgent struct {
	*serviceOperationTestAgent

	captureMu          sync.Mutex
	beginRequests      []ServiceOperationMutationBeginRequest
	peerRequests       []ServiceOperationPeerRequest
	responseGeneration func(int64) int64
}

func (a *vpnCommitmentTestAgent) BeginServiceMutation(
	request *ServiceOperationMutationBeginRequest,
	response *ServiceOperationMutationResponse,
) error {
	a.captureMu.Lock()
	a.beginRequests = append(a.beginRequests, *request)
	a.captureMu.Unlock()
	return a.serviceOperationTestAgent.BeginServiceMutation(request, response)
}

func (a *vpnCommitmentTestAgent) SyncVPNPeersV2(
	request *ServiceOperationPeerRequest,
	response *ServiceOperationPeerResponse,
) error {
	requestCopy := *request
	requestCopy.Peers = append([]ServiceOperationPeerSpec(nil), request.Peers...)
	a.captureMu.Lock()
	a.peerRequests = append(a.peerRequests, requestCopy)
	a.captureMu.Unlock()

	response.Applied = true
	response.AppliedGeneration = request.DesiredGeneration
	if a.responseGeneration != nil {
		response.AppliedGeneration = a.responseGeneration(request.DesiredGeneration)
	}
	return nil
}

func (a *vpnCommitmentTestAgent) capturedRequests() (
	[]ServiceOperationMutationBeginRequest,
	[]ServiceOperationPeerRequest,
) {
	a.captureMu.Lock()
	defer a.captureMu.Unlock()
	begins := append([]ServiceOperationMutationBeginRequest(nil), a.beginRequests...)
	peers := make([]ServiceOperationPeerRequest, len(a.peerRequests))
	for index, request := range a.peerRequests {
		peers[index] = request
		peers[index].Peers = append(
			[]ServiceOperationPeerSpec(nil),
			request.Peers...,
		)
	}
	return begins, peers
}

func TestVPNSyncCommitsCanonicalSnapshotAndGeneration(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	firstPeerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "later-address", "", time.Time{},
	)
	secondPeerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "earlier-address", "", time.Time{},
	)
	if _, err := fixture.database.GetDB().Exec(
		"UPDATE vpn_peers SET ip = ? WHERE id = ?", "10.8.0.200", firstPeerID,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.database.GetDB().Exec(
		"UPDATE vpn_peers SET ip = ? WHERE id = ?", "10.8.0.3", secondPeerID,
	); err != nil {
		t.Fatal(err)
	}
	var desiredGeneration int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT desired_generation FROM vpn_sync_state WHERE id = 1",
	).Scan(&desiredGeneration); err != nil {
		t.Fatal(err)
	}

	agent := &vpnCommitmentTestAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
	}
	attachVPNTestAgent(t, fixture.panel, agent)
	if err := fixture.panel.syncVPNPeers(context.Background()); err != nil {
		t.Fatalf("sync canonical VPN snapshot: %v", err)
	}

	begins, peerRequests := agent.capturedRequests()
	if len(begins) != 1 || len(peerRequests) != 1 {
		t.Fatalf(
			"mutation begins=%d peer requests=%d, want 1/1",
			len(begins),
			len(peerRequests),
		)
	}
	begin := begins[0]
	request := peerRequests[0]
	if begin.Kind != "vpn_peer_sync" || begin.Target != "wireguard" {
		t.Fatalf("mutation identity=%q/%q", begin.Kind, begin.Target)
	}
	if request.DesiredGeneration != desiredGeneration {
		t.Fatalf(
			"request generation=%d, want %d",
			request.DesiredGeneration,
			desiredGeneration,
		)
	}
	if len(request.Peers) != 2 ||
		request.Peers[0].IP != "10.8.0.3" ||
		request.Peers[1].IP != "10.8.0.200" {
		t.Fatalf("canonical peer order=%v", request.Peers)
	}
	transportPeers := make([]transport.VPNPeerSpec, len(request.Peers))
	for index, peer := range request.Peers {
		transportPeers[index] = transport.VPNPeerSpec{
			PublicKey:    peer.PublicKey,
			PresharedKey: peer.PresharedKey,
			IP:           peer.IP,
		}
	}
	commitment, err := mutationpayload.CanonicalVPNPeerSync(
		request.DesiredGeneration,
		transportPeers,
	)
	if err != nil {
		t.Fatalf("recompute VPN peer commitment: %v", err)
	}
	if !mutationpayload.ValidVPNPeerSyncQualifier(begin.PackageName) {
		t.Fatalf("invalid VPN peer qualifier %q", begin.PackageName)
	}
	if begin.PackageName != commitment.Qualifier {
		t.Fatalf(
			"mutation qualifier=%q, want %q",
			begin.PackageName,
			commitment.Qualifier,
		)
	}

	var status string
	var appliedGeneration int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT status, applied_generation FROM vpn_sync_state WHERE id = 1",
	).Scan(&status, &appliedGeneration); err != nil {
		t.Fatal(err)
	}
	if status != "applied" || appliedGeneration != desiredGeneration {
		t.Fatalf(
			"committed sync state=%q generation=%d, want applied/%d",
			status,
			appliedGeneration,
			desiredGeneration,
		)
	}
}

type vpnCommittedResponseLossAgent struct {
	*serviceOperationTestAgent
	committedGeneration atomic.Int64
	finishSawFailure    atomic.Bool
	cancelPanelContext  context.CancelFunc
}

func (a *vpnCommittedResponseLossAgent) SyncVPNPeersV2(
	request *ServiceOperationPeerRequest,
	_ *ServiceOperationPeerResponse,
) error {
	a.mu.Lock()
	job := a.mutationJobs[a.mutationActive]
	if job != nil {
		job.Status = agentMutationSucceeded
		job.Phase = "completed"
		a.mutationActive = ""
	}
	a.mu.Unlock()
	a.committedGeneration.Store(request.DesiredGeneration)
	if a.cancelPanelContext != nil {
		a.cancelPanelContext()
	}
	return errors.New("simulated VPN response loss after durable commit")
}

func (a *vpnCommittedResponseLossAgent) FinishServiceMutation(
	request *ServiceOperationMutationFinishRequest,
	response *ServiceOperationMutationResponse,
) error {
	a.finishSawFailure.Store(!request.Success)
	a.mu.Lock()
	defer a.mu.Unlock()
	job := a.mutationJobs[request.RequestID]
	if job == nil || job.OwnerID != request.OwnerID {
		response.Error = "service mutation owner mismatch"
		response.Job = cloneServiceOperationMutationJob(job)
		return nil
	}
	response.Job = cloneServiceOperationMutationJob(job)
	return nil
}

func TestVPNSyncFinalizesGenerationAfterCommittedResponseLoss(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "response-loss", "", time.Time{},
	)
	if _, err := fixture.database.GetDB().Exec(
		"UPDATE vpn_peers SET sync_state = 'pending' WHERE id = ?",
		peerID,
	); err != nil {
		t.Fatal(err)
	}
	var desiredGeneration int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT desired_generation FROM vpn_sync_state WHERE id = 1",
	).Scan(&desiredGeneration); err != nil {
		t.Fatal(err)
	}

	agent := &vpnCommittedResponseLossAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
	}
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	agent.cancelPanelContext = cancelRequest
	attachVPNTestAgent(t, fixture.panel, agent)
	if err := fixture.panel.syncVPNPeers(requestCtx); err != nil {
		t.Fatalf("sync after committed response loss: %v", err)
	}
	if !errors.Is(requestCtx.Err(), context.Canceled) {
		t.Fatalf("request context error=%v, want canceled", requestCtx.Err())
	}
	if got := agent.committedGeneration.Load(); got != desiredGeneration {
		t.Fatalf("agent committed generation=%d, want %d", got, desiredGeneration)
	}
	if !agent.finishSawFailure.Load() {
		t.Fatal("test did not exercise the failed-RPC Finish race")
	}

	var status string
	var leaseToken *string
	var appliedGeneration int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT status, lease_token, applied_generation FROM vpn_sync_state WHERE id = 1",
	).Scan(&status, &leaseToken, &appliedGeneration); err != nil {
		t.Fatal(err)
	}
	if status != "applied" || leaseToken != nil ||
		appliedGeneration != desiredGeneration {
		t.Fatalf(
			"sync state=%q lease=%v applied=%d, want applied/nil/%d",
			status,
			leaseToken,
			appliedGeneration,
			desiredGeneration,
		)
	}
	var peerState string
	if err := fixture.database.GetDB().QueryRow(
		"SELECT sync_state FROM vpn_peers WHERE id = ?",
		peerID,
	).Scan(&peerState); err != nil {
		t.Fatal(err)
	}
	if peerState != "applied" {
		t.Fatalf("peer sync state=%q, want applied", peerState)
	}
}

func TestVPNSyncRejectsMismatchedAppliedGeneration(t *testing.T) {
	tests := []struct {
		name               string
		responseGeneration func(int64) int64
	}{
		{
			name: "omitted",
			responseGeneration: func(int64) int64 {
				return 0
			},
		},
		{
			name: "wrong",
			responseGeneration: func(generation int64) int64 {
				return generation + 1
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
			peerID := insertVPNTestPeer(
				t, fixture, subscriptionID, ownerID,
				"generation-mismatch", "", time.Time{},
			)
			if _, err := fixture.database.GetDB().Exec(
				"UPDATE vpn_peers SET sync_state = 'pending' WHERE id = ?",
				peerID,
			); err != nil {
				t.Fatal(err)
			}
			var desiredGeneration int64
			if err := fixture.database.GetDB().QueryRow(
				"SELECT desired_generation FROM vpn_sync_state WHERE id = 1",
			).Scan(&desiredGeneration); err != nil {
				t.Fatal(err)
			}
			if desiredGeneration == 0 {
				t.Fatal("test requires a non-zero desired generation")
			}

			agent := &vpnCommitmentTestAgent{
				serviceOperationTestAgent: newServiceOperationTestAgent(),
				responseGeneration:        test.responseGeneration,
			}
			attachVPNTestAgent(t, fixture.panel, agent)
			err := fixture.panel.syncVPNPeers(context.Background())
			if err == nil || !strings.Contains(err.Error(), "generation") {
				t.Fatalf("generation mismatch error=%v", err)
			}

			begins, peerRequests := agent.capturedRequests()
			if len(begins) != 1 || len(peerRequests) != 1 {
				t.Fatalf(
					"mutation begins=%d peer requests=%d, want 1/1",
					len(begins),
					len(peerRequests),
				)
			}
			if !mutationpayload.ValidVPNPeerSyncQualifier(begins[0].PackageName) {
				t.Fatalf("invalid VPN peer qualifier %q", begins[0].PackageName)
			}

			var status string
			var leaseToken *string
			var appliedGeneration int64
			if err := fixture.database.GetDB().QueryRow(
				"SELECT status, lease_token, applied_generation "+
					"FROM vpn_sync_state WHERE id = 1",
			).Scan(&status, &leaseToken, &appliedGeneration); err != nil {
				t.Fatal(err)
			}
			if status != "error" || leaseToken != nil ||
				appliedGeneration == desiredGeneration {
				t.Fatalf(
					"sync state=%q lease=%v applied=%d desired=%d",
					status,
					leaseToken,
					appliedGeneration,
					desiredGeneration,
				)
			}
			var peerState string
			var peerError *string
			if err := fixture.database.GetDB().QueryRow(
				"SELECT sync_state, sync_error FROM vpn_peers WHERE id = ?",
				peerID,
			).Scan(&peerState, &peerError); err != nil {
				t.Fatal(err)
			}
			if peerState != "error" || peerError == nil {
				t.Fatalf("peer failure state=%q error=%v", peerState, peerError)
			}
		})
	}
}

func TestVPNSyncRejectsInvalidSnapshotBeforeAgentLease(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "invalid-key", "", time.Time{},
	)
	if _, err := fixture.database.GetDB().Exec(
		"UPDATE vpn_peers SET public_key = 'not-base64' WHERE id = ?",
		peerID,
	); err != nil {
		t.Fatal(err)
	}

	agent := &vpnCommitmentTestAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
	}
	attachVPNTestAgent(t, fixture.panel, agent)
	err := fixture.panel.syncVPNPeers(context.Background())
	if err == nil || !strings.Contains(err.Error(), "canonicalize VPN peer snapshot") {
		t.Fatalf("invalid snapshot error=%v", err)
	}
	begins, peerRequests := agent.capturedRequests()
	if len(begins) != 0 || len(peerRequests) != 0 {
		t.Fatalf(
			"invalid snapshot reached agent: begins=%d peer requests=%d",
			len(begins),
			len(peerRequests),
		)
	}
	var leaseToken *string
	if err := fixture.database.GetDB().QueryRow(
		"SELECT lease_token FROM vpn_sync_state WHERE id = 1",
	).Scan(&leaseToken); err != nil {
		t.Fatal(err)
	}
	if leaseToken != nil {
		t.Fatalf("invalid snapshot retained VPN sync lease %q", *leaseToken)
	}
}

type vpnLegacySyncMethodTestAgent struct {
	base    *serviceOperationTestAgent
	v1Calls atomic.Int32
}

func (a *vpnLegacySyncMethodTestAgent) PkgFamily(
	request *transport.Empty,
	response *string,
) error {
	return a.base.PkgFamily(request, response)
}

func (a *vpnLegacySyncMethodTestAgent) BeginServiceMutation(
	request *ServiceOperationMutationBeginRequest,
	response *ServiceOperationMutationResponse,
) error {
	return a.base.BeginServiceMutation(request, response)
}

func (a *vpnLegacySyncMethodTestAgent) FinishServiceMutation(
	request *ServiceOperationMutationFinishRequest,
	response *ServiceOperationMutationResponse,
) error {
	return a.base.FinishServiceMutation(request, response)
}

func (a *vpnLegacySyncMethodTestAgent) SyncVPNPeers(
	request *ServiceOperationPeerRequest,
	response *ServiceOperationPeerResponse,
) error {
	a.v1Calls.Add(1)
	response.Applied = true
	response.AppliedGeneration = request.DesiredGeneration
	return nil
}

func TestVPNSyncV2RejectsLegacyAgentWithoutStateCAS(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "legacy-agent", "", time.Time{},
	)
	if _, err := fixture.database.GetDB().Exec(
		"UPDATE vpn_peers SET sync_state = 'pending' WHERE id = ?",
		peerID,
	); err != nil {
		t.Fatal(err)
	}
	var desiredGeneration, appliedBefore int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT desired_generation, applied_generation "+
			"FROM vpn_sync_state WHERE id = 1",
	).Scan(&desiredGeneration, &appliedBefore); err != nil {
		t.Fatal(err)
	}
	if desiredGeneration <= appliedBefore {
		t.Fatalf(
			"test requires unapplied generation: desired=%d applied=%d",
			desiredGeneration,
			appliedBefore,
		)
	}

	agent := &vpnLegacySyncMethodTestAgent{
		base: newServiceOperationTestAgent(),
	}
	attachVPNTestAgent(t, fixture.panel, agent)
	err := fixture.panel.syncVPNPeers(context.Background())
	if err == nil ||
		!strings.Contains(err.Error(), "rpc: can't find method Agent.SyncVPNPeersV2") {
		t.Fatalf("legacy agent error=%v", err)
	}
	if calls := agent.v1Calls.Load(); calls != 0 {
		t.Fatalf("legacy SyncVPNPeers fallback calls=%d, want 0", calls)
	}

	var status string
	var leaseToken *string
	var desiredAfter, appliedAfter int64
	if err := fixture.database.GetDB().QueryRow(
		"SELECT status, lease_token, desired_generation, applied_generation "+
			"FROM vpn_sync_state WHERE id = 1",
	).Scan(&status, &leaseToken, &desiredAfter, &appliedAfter); err != nil {
		t.Fatal(err)
	}
	if status != "error" || leaseToken != nil ||
		desiredAfter != desiredGeneration || appliedAfter != appliedBefore {
		t.Fatalf(
			"legacy agent sync state=%q lease=%v desired=%d applied=%d, "+
				"want error/nil/%d/%d",
			status,
			leaseToken,
			desiredAfter,
			appliedAfter,
			desiredGeneration,
			appliedBefore,
		)
	}
	var peerState string
	if err := fixture.database.GetDB().QueryRow(
		"SELECT sync_state FROM vpn_peers WHERE id = ?",
		peerID,
	).Scan(&peerState); err != nil {
		t.Fatal(err)
	}
	if peerState != "error" {
		t.Fatalf("legacy agent peer state=%q, want error", peerState)
	}
}

type vpnBlockingTestAgent struct {
	*serviceOperationTestAgent
	started  chan struct{}
	release  chan struct{}
	startOne sync.Once

	snapshotMu sync.Mutex
	snapshots  []int
}

func (a *vpnBlockingTestAgent) SyncVPNPeersV2(
	request *ServiceOperationPeerRequest,
	response *ServiceOperationPeerResponse,
) error {
	a.snapshotMu.Lock()
	call := len(a.snapshots)
	a.snapshots = append(a.snapshots, len(request.Peers))
	a.snapshotMu.Unlock()
	if call == 0 {
		a.startOne.Do(func() { close(a.started) })
		<-a.release
	}
	response.Applied = true
	response.AppliedGeneration = request.DesiredGeneration
	return nil
}

func attachVPNTestAgent(t *testing.T, panel *Panel, agent any) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register VPN test agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func attachVPNBlockingTestAgent(
	t *testing.T,
	panel *Panel,
	agent *vpnBlockingTestAgent,
) {
	t.Helper()
	attachVPNTestAgent(t, panel, agent)
}

func TestVPNSyncRetriesStaleGenerationAfterConcurrentRevoke(t *testing.T) {
	fixture, ownerID, _, subscriptionID := newVPNSecurityFixture(t)
	peerID := insertVPNTestPeer(
		t, fixture, subscriptionID, ownerID, "concurrent-device", "", time.Time{},
	)
	agent := &vpnBlockingTestAgent{
		serviceOperationTestAgent: newServiceOperationTestAgent(),
		started:                   make(chan struct{}),
		release:                   make(chan struct{}),
	}
	attachVPNBlockingTestAgent(t, fixture.panel, agent)

	result := make(chan error, 1)
	go func() {
		result <- fixture.panel.syncVPNPeers(context.Background())
	}()
	select {
	case <-agent.started:
	case <-time.After(3 * time.Second):
		t.Fatal("first peer snapshot did not reach the agent")
	}
	if _, err := fixture.database.GetDB().Exec(`
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    updated_at = datetime('now')
		WHERE id = ?`, peerID); err != nil {
		t.Fatal(err)
	}
	close(agent.release)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("stale generation retry failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stale generation retry timed out")
	}

	agent.snapshotMu.Lock()
	snapshots := append([]int(nil), agent.snapshots...)
	agent.snapshotMu.Unlock()
	if len(snapshots) < 2 || snapshots[0] != 1 || snapshots[len(snapshots)-1] != 0 {
		t.Fatalf("peer snapshots=%v, want first [1] and final [0]", snapshots)
	}
	var remaining int
	if err := fixture.database.GetDB().QueryRow(
		`SELECT COUNT(*) FROM vpn_peers WHERE id = ?`, peerID,
	).Scan(&remaining); err != nil {
		t.Fatal(err)
	}
	if remaining != 0 {
		t.Fatalf("concurrently revoked peer remained in ledger: %d", remaining)
	}
}
