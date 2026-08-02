package main

import (
	"context"
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

	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/transport"
)

var vpnTestPeerSequence atomic.Int64

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
		subscriptionID, name, fmt.Sprintf("public-%d", sequence),
		fmt.Sprintf("psk-%d", sequence),
		fmt.Sprintf("10.8.0.%d", 2+sequence), createdBy,
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

type vpnBlockingTestAgent struct {
	*serviceOperationTestAgent
	started  chan struct{}
	release  chan struct{}
	startOne sync.Once

	snapshotMu sync.Mutex
	snapshots  []int
}

func (a *vpnBlockingTestAgent) SyncVPNPeers(
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
	return nil
}

func attachVPNBlockingTestAgent(
	t *testing.T,
	panel *Panel,
	agent *vpnBlockingTestAgent,
) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register VPN blocking agent: %v", err)
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
