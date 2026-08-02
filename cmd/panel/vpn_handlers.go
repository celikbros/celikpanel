package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	vpnProductID = "vpn"
	vpnFixedPort = 51820
)

var errVPNAddressPoolFull = errors.New("the VPN address pool is full")

// vpnMutationMu serializes every peer-set mutation. WireGuard receives a full
// desired-state snapshot, so concurrent snapshots must never overtake one another.
// vpnMutationMu tüm peer kümesi değişikliklerini sıraya alır. WireGuard istenen
// durumun tamamını aldığı için eş zamanlı anlık görüntüler birbirini geçmemelidir.
var vpnMutationMu sync.Mutex

type vpnPeerRow struct {
	ID             int     `json:"id"`
	SubscriptionID *int    `json:"subscription_id,omitempty"`
	Name           string  `json:"name"`
	PublicKey      string  `json:"public_key"`
	IP             string  `json:"ip"`
	CreatedAt      string  `json:"created_at"`
	LastHandshake  int64   `json:"last_handshake"`
	RxBytes        int64   `json:"rx_bytes"`
	TxBytes        int64   `json:"tx_bytes"`
	Subscription   *string `json:"subscription,omitempty"`
	DesiredState   string  `json:"desired_state"`
	SyncState      string  `json:"sync_state"`
	SyncError      *string `json:"sync_error,omitempty"`
}

type vpnPeerSpec = transport.VPNPeerSpec

// handleVPNStatus returns the live server state. The endpoint and public key
// are not secrets; both are present in every issued client configuration.
// handleVPNStatus canlı sunucu durumunu döndürür. Uç nokta ve genel anahtar
// gizli değildir; verilen her istemci yapılandırmasında bulunur.
func (p *Panel) handleVPNStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setVPNPrivateResponseHeaders(w)
	if r.Method != http.MethodGet {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	caller := currentCaller(r)
	if caller == nil {
		writeClientError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	var status transport.VPNStatusResponse
	if err := p.callAgent("Agent.VPNStatus", &transport.Empty{}, &status); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	peerCount, pendingCount, errorCount, err := p.vpnVisibleSyncSummary(r.Context(), caller)
	if err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"installed": status.Installed, "configured": status.Configured,
		"running": status.Running, "server_public_key": status.ServerPublicKey,
		"port": status.Port, "endpoint": status.Endpoint, "peer_count": peerCount,
		"sync": map[string]any{
			"in_sync": pendingCount == 0 && errorCount == 0,
			"pending": pendingCount,
			"errors":  errorCount,
		},
		"policy": map[string]any{
			"interface":         "wg0",
			"network":           "10.8.0.0/24",
			"server_address":    "10.8.0.1",
			"listen_protocol":   "UDP",
			"listen_port":       vpnFixedPort,
			"client_dns":        "1.1.1.1",
			"allowed_ips":       "0.0.0.0/0",
			"full_tunnel":       true,
			"nat_required":      true,
			"forward_required":  true,
			"firewall_required": true,
		},
	})
}

// vpnVisibleSyncSummary scopes desired-state counters to subscriptions the
// caller can access. Server-wide counts are reserved for administrators.
// vpnVisibleSyncSummary istenen durum sayaçlarını çağıranın erişebildiği
// aboneliklerle sınırlar. Sunucu geneli sayaçlar yalnız yöneticilere açıktır.
func (p *Panel) vpnVisibleSyncSummary(
	ctx context.Context, caller *Caller,
) (active, pending, failed int, err error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT subscription_id, desired_state, sync_state FROM vpn_peers`)
	if err != nil {
		return 0, 0, 0, err
	}
	defer rows.Close()
	for rows.Next() {
		var subscriptionID *int
		var desiredState, syncState string
		if err := rows.Scan(&subscriptionID, &desiredState, &syncState); err != nil {
			return 0, 0, 0, err
		}
		if caller.Role != roleAdmin &&
			(subscriptionID == nil ||
				p.canAccessSubscription(ctx, caller, *subscriptionID) != nil) {
			continue
		}
		if desiredState == "active" && subscriptionID != nil {
			active++
		}
		if syncState == "pending" {
			pending++
		}
		if syncState == "error" {
			failed++
		}
	}
	if err := rows.Err(); err != nil {
		return 0, 0, 0, err
	}
	if caller.Role == roleAdmin {
		var globalState string
		if err := p.db.GetDB().QueryRowContext(ctx, `
			SELECT status FROM vpn_sync_state WHERE id = 1`,
		).Scan(&globalState); err != nil {
			return 0, 0, 0, err
		}
		switch globalState {
		case "pending":
			if pending == 0 {
				pending = 1
			}
		case "error":
			if failed == 0 {
				failed = 1
			}
		}
	}
	return active, pending, failed, nil
}

// handleVPNSetup starts the fixed WireGuard service and restores the complete
// desired peer set. The public product deliberately exposes no custom port.
// handleVPNSetup sabit WireGuard hizmetini başlatır ve istenen peer kümesinin
// tamamını geri yükler. Ürün bilinçli olarak özel port seçeneği sunmaz.
func (p *Panel) handleVPNSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if caller := currentCaller(r); caller == nil || caller.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	var request struct{}
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	var response transport.SetupVPNResponse
	err := p.withStandaloneAgentMutation(
		r.Context(), "vpn_setup", "wireguard", "",
		func(callCtx context.Context, binding agentMutationBinding) error {
			if err := p.agentClient.CallContext(callCtx, "Agent.SetupVPN", &transport.SetupVPNRequest{
				ServiceMutationBinding: transport.ServiceMutationBinding{
					MutationRequestID: binding.MutationRequestID,
					MutationOwnerID:   binding.MutationOwnerID,
				},
				Port: vpnFixedPort,
			}, &response); err != nil {
				return err
			}
			if response.Error != "" {
				return fmt.Errorf("VPN setup: %s", response.Error)
			}
			return nil
		},
	)
	if err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	// Setup owns one durable agent mutation. Reconciliation starts only after
	// that lease is released, while the panel-level VPN lock still serializes it.
	// Kurulum tek bir kalıcı agent mutasyonuna sahiptir. Eşitleme bu kira
	// bırakıldıktan sonra, panel düzeyindeki VPN kilidi sıralamayı sürdürürken başlar.
	if err := p.syncVPNPeersLocked(r.Context()); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	p.audit(r, "vpn.setup", "", 0)
	json.NewEncoder(w).Encode(map[string]any{
		"success": true, "created": response.Created, "detail": response.Detail,
	})
}

// syncVPNPeers is the public serialized entry point used by service lifecycle
// code. Call syncVPNPeersLocked only while vpnMutationMu is held.
// syncVPNPeers servis yaşam döngüsü kodunun kullandığı sıralı giriş noktasıdır.
// syncVPNPeersLocked yalnız vpnMutationMu tutulurken çağrılmalıdır.
func (p *Panel) syncVPNPeers(ctx context.Context) error {
	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()
	return p.syncVPNPeersLocked(ctx)
}

func (p *Panel) syncVPNPeersLocked(ctx context.Context) error {
	return p.syncVPNPeersGenerationLocked(ctx, 4)
}

func (p *Panel) syncVPNPeersGenerationLocked(ctx context.Context, retries int) error {
	if p.secrets == nil {
		return errors.New("VPN secret storage is unavailable")
	}
	token, err := newServiceOperationID()
	if err != nil {
		return errors.New("could not reserve VPN synchronization")
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(ctx, `
		UPDATE vpn_sync_state
		SET lease_token = ?, lease_expires_at = datetime('now', '+2 minutes'),
		    status = 'pending', updated_at = datetime('now')
		WHERE id = 1
		  AND (
			lease_token IS NULL
			OR lease_expires_at IS NULL
			OR julianday(lease_expires_at) <= julianday('now')
		  )`, token)
	if err != nil {
		return err
	}
	claimed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if claimed != 1 {
		return errors.New("VPN synchronization is already in progress")
	}
	var generation int64
	if err := tx.QueryRowContext(ctx, `
		SELECT desired_generation FROM vpn_sync_state WHERE id = 1`,
	).Scan(&generation); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `
		SELECT vp.id, vp.public_key, vp.preshared_key, vp.ip
		FROM vpn_peers vp
		WHERE vp.desired_state = 'active'
		  AND (
			vp.subscription_id IS NOT NULL
			AND EXISTS (
				SELECT 1
				FROM subscription_entitlements e
				JOIN store_offerings o ON o.id = e.product_id
				WHERE e.subscription_id = vp.subscription_id
				  AND e.product_id = 'vpn'
				  AND e.status = 'active'
				  AND o.release_state = 'available'
				  AND o.entitlement_mode = 'grant'
				  AND (
					e.expires_at IS NULL
					OR (
						julianday(e.expires_at) IS NOT NULL
						AND julianday(e.expires_at) > julianday('now')
					)
				  )
			)
		  )
		ORDER BY vp.id`)
	if err != nil {
		return err
	}

	var peerIDs []int
	var peers []vpnPeerSpec
	for rows.Next() {
		var id int
		var peer vpnPeerSpec
		var storedPSK string
		if err := rows.Scan(&id, &peer.PublicKey, &storedPSK, &peer.IP); err != nil {
			rows.Close()
			return err
		}
		peer.PresharedKey, err = p.secrets.Decrypt(storedPSK)
		if err != nil {
			rows.Close()
			return fmt.Errorf("decrypt VPN preshared key for peer %d: %w", id, err)
		}
		peerIDs = append(peerIDs, id)
		peers = append(peers, peer)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}

	var response transport.SyncVPNPeersResponse
	err = p.withStandaloneAgentMutation(
		ctx, "vpn_peer_sync", "wireguard", "",
		func(callCtx context.Context, binding agentMutationBinding) error {
			if err := p.agentClient.CallContext(callCtx, "Agent.SyncVPNPeers", &transport.SyncVPNPeersRequest{
				ServiceMutationBinding: transport.ServiceMutationBinding{
					MutationRequestID: binding.MutationRequestID,
					MutationOwnerID:   binding.MutationOwnerID,
				},
				Peers: peers,
			}, &response); err != nil {
				return err
			}
			if response.Error != "" {
				return fmt.Errorf("peer sync: %s", response.Error)
			}
			if !response.Applied {
				return errors.New("agent did not confirm peer synchronization")
			}
			return nil
		},
	)
	if err != nil {
		log.Printf("VPN peer synchronization failed: %v", err)
		if stateErr := p.recordVPNSyncError(ctx, token, generation, err); stateErr != nil {
			return errors.Join(
				err,
				fmt.Errorf("persist VPN synchronization failure state: %w", stateErr),
			)
		}
		return err
	}

	tx, err = p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	result, err = tx.ExecContext(ctx, `
		UPDATE vpn_sync_state
		SET applied_generation = ?, status = 'applied', last_error = NULL,
		    lease_token = NULL, lease_expires_at = NULL,
		    updated_at = datetime('now')
		WHERE id = 1 AND lease_token = ? AND desired_generation = ?`,
		generation, token, generation,
	)
	if err != nil {
		return err
	}
	current, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if current != 1 {
		released, releaseErr := tx.ExecContext(ctx, `
			UPDATE vpn_sync_state
			SET status = 'pending', lease_token = NULL, lease_expires_at = NULL,
			    updated_at = datetime('now')
			WHERE id = 1 AND lease_token = ?`, token)
		if releaseErr != nil {
			return fmt.Errorf("release stale VPN synchronization lease: %w", releaseErr)
		}
		releasedRows, releaseErr := released.RowsAffected()
		if releaseErr != nil {
			return fmt.Errorf("verify stale VPN synchronization lease release: %w", releaseErr)
		}
		if releasedRows != 1 {
			return errors.New("VPN synchronization lease was lost while desired state changed")
		}
		if err := tx.Commit(); err != nil {
			return err
		}
		if retries <= 0 {
			return errors.New("VPN desired state kept changing during synchronization")
		}
		return p.syncVPNPeersGenerationLocked(ctx, retries-1)
	}
	for _, id := range peerIDs {
		if _, err := tx.ExecContext(ctx, `
			UPDATE vpn_peers
			SET sync_state = 'applied', sync_error = NULL, updated_at = datetime('now')
			WHERE id = ? AND desired_state = 'active'`, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM vpn_peers WHERE desired_state = 'revoked'`,
	); err != nil {
		return err
	}
	return tx.Commit()
}

func (p *Panel) recordVPNSyncError(
	ctx context.Context, token string, generation int64, syncErr error,
) error {
	stateCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	message := vpnSyncErrorText(syncErr)
	tx, err := p.db.GetDB().BeginTx(stateCtx, nil)
	if err != nil {
		return fmt.Errorf("begin failure-state transaction: %w", err)
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(stateCtx, `
		UPDATE vpn_sync_state
		SET status = CASE
				WHEN desired_generation = ? THEN 'error'
				ELSE 'pending'
			END,
		    last_error = CASE
				WHEN desired_generation = ? THEN ?
				ELSE NULL
			END,
		    lease_token = NULL, lease_expires_at = NULL,
		    updated_at = datetime('now')
		WHERE id = 1 AND lease_token = ?`,
		generation, generation, message, token,
	)
	if err != nil {
		return fmt.Errorf("record global VPN failure state: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify global VPN failure state: %w", err)
	}
	if affected != 1 {
		return errors.New("claimed VPN synchronization lease was not found")
	}
	if _, err := tx.ExecContext(stateCtx, `
		UPDATE vpn_peers
		SET sync_state = 'error', sync_error = ?, updated_at = datetime('now')
		WHERE sync_state = 'pending'
		  AND EXISTS (
			SELECT 1 FROM vpn_sync_state
			WHERE id = 1 AND desired_generation = ?
		  )`, message, generation); err != nil {
		return fmt.Errorf("record VPN peer failure state: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit VPN failure state: %w", err)
	}
	return nil
}

// handleVPNSync safely republishes the complete desired peer set. It accepts
// no raw configuration, command, path or port input.
// handleVPNSync istenen peer kümesinin tamamını güvenle yeniden yayımlar.
// Ham yapılandırma, komut, yol veya port girdisi kabul etmez.
func (p *Panel) handleVPNSync(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if caller := currentCaller(r); caller == nil || caller.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	var request struct{}
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := p.reconcileVPNEntitlements(r.Context()); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	if err := p.syncVPNPeers(r.Context()); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	p.audit(r, "vpn.sync", "", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// handleVPNPeers lists and creates peers. Every subscription-bound peer needs
// a currently active VPN entitlement, including peers issued by an admin.
// handleVPNPeers peer'ları listeler ve oluşturur. Yönetici tarafından verilse
// bile aboneliğe bağlı her peer için etkin bir VPN hakkı gerekir.
func (p *Panel) handleVPNPeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setVPNPrivateResponseHeaders(w)
	caller := currentCaller(r)
	if caller == nil {
		writeClientError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		p.listVPNPeers(w, r, caller)
	case http.MethodPost:
		p.createVPNPeer(w, r, caller)
	default:
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (p *Panel) listVPNPeers(w http.ResponseWriter, r *http.Request, caller *Caller) {
	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT vp.id, vp.subscription_id, vp.name, vp.public_key, vp.ip,
		       vp.created_at, s.name, vp.desired_state, vp.sync_state, vp.sync_error
		FROM vpn_peers vp
		LEFT JOIN subscriptions s ON s.id = vp.subscription_id
		ORDER BY vp.id`)
	if err != nil {
		writeServerError(w, err)
		return
	}
	var peers []vpnPeerRow
	for rows.Next() {
		var peer vpnPeerRow
		if err := rows.Scan(
			&peer.ID, &peer.SubscriptionID, &peer.Name, &peer.PublicKey, &peer.IP,
			&peer.CreatedAt, &peer.Subscription, &peer.DesiredState,
			&peer.SyncState, &peer.SyncError,
		); err != nil {
			rows.Close()
			writeServerError(w, err)
			return
		}
		peers = append(peers, peer)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		writeServerError(w, err)
		return
	}
	rows.Close()

	if caller.Role != roleAdmin {
		visible := make([]vpnPeerRow, 0, len(peers))
		for _, peer := range peers {
			if peer.SubscriptionID != nil &&
				p.canAccessSubscription(r.Context(), caller, *peer.SubscriptionID) == nil {
				// Tenant users receive state only. Agent, host and path details
				// from synchronization errors are intentionally admin-only.
				// Kiracı kullanıcılar yalnız durumu görür. Agent, ana makine ve
				// yol ayrıntıları içerebilen eşitleme hataları yalnız yöneticiyedir.
				peer.SyncError = nil
				visible = append(visible, peer)
			}
		}
		peers = visible
	} else {
		for i := range peers {
			if peers[i].SyncError == nil {
				continue
			}
			safe := vpnPublicSyncError(*peers[i].SyncError)
			peers[i].SyncError = &safe
		}
	}

	var status transport.VPNStatusResponse
	if p.callAgent("Agent.VPNStatus", &transport.Empty{}, &status) == nil {
		live := make(map[string][3]int64, len(status.Peers))
		for _, peer := range status.Peers {
			live[peer.PublicKey] = [3]int64{
				peer.LastHandshake, peer.RxBytes, peer.TxBytes,
			}
		}
		for i := range peers {
			if peers[i].DesiredState != "active" {
				continue
			}
			if counters, ok := live[peers[i].PublicKey]; ok {
				peers[i].LastHandshake = counters[0]
				peers[i].RxBytes = counters[1]
				peers[i].TxBytes = counters[2]
			}
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"peers": peers})
}

// createVPNPeer returns the private client configuration exactly once. The
// response is explicitly non-cacheable and the private key is never persisted.
// createVPNPeer özel istemci yapılandırmasını yalnız bir kez döndürür. Yanıt
// açıkça önbelleğe alınamaz ve özel anahtar hiçbir zaman kalıcılaştırılmaz.
func (p *Panel) createVPNPeer(w http.ResponseWriter, r *http.Request, caller *Caller) {
	setVPNPrivateResponseHeaders(w)
	var request struct {
		Name           string `json:"name"`
		SubscriptionID int    `json:"subscription_id"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.Name = strings.TrimSpace(request.Name)
	if !validVPNPeerName(request.Name) {
		writeClientError(w, http.StatusBadRequest, "peer name must be 1-60 visible characters")
		return
	}

	if request.SubscriptionID <= 0 {
		writeClientError(w, http.StatusBadRequest, "subscription_id is required")
		return
	}
	if p.canAccessSubscription(r.Context(), caller, request.SubscriptionID) != nil {
		writeClientError(w, http.StatusNotFound, "subscription not found")
		return
	}
	subscriptionID := &request.SubscriptionID

	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	if !p.requireActiveVPNEntitlement(w, r, *subscriptionID) {
		return
	}
	if p.secrets == nil {
		writeServerError(w, errors.New("VPN secret storage is unavailable"))
		return
	}

	var status transport.VPNStatusResponse
	if err := p.callAgent("Agent.VPNStatus", &transport.Empty{}, &status); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	if !status.Running || status.ServerPublicKey == "" || status.Endpoint == "" {
		writeClientError(w, http.StatusConflict, "the VPN server is not ready")
		return
	}
	if status.Port != vpnFixedPort {
		writeClientError(w, http.StatusConflict, "the VPN server must listen on UDP port 51820")
		return
	}

	ip, err := p.allocateVPNIP(r.Context())
	if err != nil {
		if errors.Is(err, errVPNAddressPoolFull) {
			writeClientError(w, http.StatusConflict, "the VPN address pool is full")
		} else {
			writeServerError(w, err)
		}
		return
	}
	var keys transport.VPNKeysResponse
	if err := p.callAgent("Agent.GenerateVPNKeys", &transport.Empty{}, &keys); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	if keys.Error != "" {
		writeServerError(w, fmt.Errorf("key generation: %s", keys.Error))
		return
	}
	deliveryToken, err := newServiceOperationID()
	if err != nil {
		writeServerError(w, errors.New("could not create VPN delivery receipt"))
		return
	}
	deliveryTokenHash := vpnDeliveryTokenHash(deliveryToken)
	sealedPSK, err := p.secrets.Encrypt(keys.PresharedKey)
	if err != nil {
		writeServerError(w, fmt.Errorf("encrypt VPN preshared key: %w", err))
		return
	}

	result, err := p.db.GetDB().ExecContext(r.Context(), `
		INSERT INTO vpn_peers
			(subscription_id, name, public_key, preshared_key, ip, created_by,
			 desired_state, sync_state, provisioning_state, delivery_token_hash,
			 delivery_expires_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, 'active', 'pending', 'provisioning', ?,
		        datetime('now', '+5 minutes'), datetime('now'))`,
		subscriptionID, request.Name, keys.PublicKey, sealedPSK, ip, caller.ID,
		deliveryTokenHash,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	peerID, err := result.LastInsertId()
	if err != nil {
		writeServerError(w, fmt.Errorf("read created VPN peer identity: %w", err))
		return
	}

	if err := p.syncVPNPeersLocked(r.Context()); err != nil {
		if cleanupErr := p.revokeUndeliveredVPNPeer(r.Context(), peerID, err); cleanupErr != nil {
			err = errors.Join(err, cleanupErr)
		}
		writeAgentError(w, err, "VPN")
		return
	}

	endpoint := net.JoinHostPort(strings.Trim(status.Endpoint, "[]"), strconv.Itoa(status.Port))
	clientConfig := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = 1.1.1.1

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0
Endpoint = %s
PersistentKeepalive = 25
`, keys.PrivateKey, ip, status.ServerPublicKey, keys.PresharedKey,
		endpoint)

	if err := json.NewEncoder(w).Encode(map[string]any{
		"success": true, "id": peerID, "ip": ip, "public_key": keys.PublicKey,
		"client_config": clientConfig, "delivery_token": deliveryToken,
	}); err != nil {
		log.Printf("deliver VPN client configuration for peer %d: %v", peerID, err)
		if cleanupErr := p.revokeUndeliveredVPNPeer(r.Context(), peerID, err); cleanupErr != nil {
			log.Printf(
				"revoke undelivered VPN peer %d after response failure: %v",
				peerID,
				cleanupErr,
			)
		}
		return
	}
	p.audit(r, "vpn.peer.add:"+request.Name, "vpn_peer", int(peerID))
}

// revokeUndeliveredVPNPeer durably excludes a peer whose private client
// configuration was not delivered. It deliberately outlives request
// cancellation: otherwise a disconnected browser could leave active access in
// WireGuard without possessing a recoverable delivery receipt.
func (p *Panel) revokeUndeliveredVPNPeer(
	ctx context.Context,
	peerID int64,
	deliveryErr error,
) error {
	cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	result, err := p.db.GetDB().ExecContext(cleanupCtx, `
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    sync_error = ?, delivery_token_hash = NULL,
		    delivery_expires_at = NULL, updated_at = datetime('now')
		WHERE id = ?`, vpnSyncErrorText(deliveryErr), peerID)
	if err != nil {
		return fmt.Errorf("mark undelivered VPN peer revoked: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("verify undelivered VPN peer revocation: %w", err)
	}
	if affected != 1 {
		return errors.New("undelivered VPN peer no longer exists in the ledger")
	}
	// A full desired-state sync excludes the unissued peer even when the first
	// agent call failed after applying an ambiguous snapshot. If this retry also
	// fails, syncVPNPeersLocked leaves a durable error tombstone for an operator.
	if err := p.syncVPNPeersLocked(cleanupCtx); err != nil {
		return fmt.Errorf("remove undelivered VPN peer from WireGuard: %w", err)
	}
	return nil
}

func vpnDeliveryTokenHash(token string) string {
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:])
}

func validVPNPeerName(value string) bool {
	count := utf8.RuneCountInString(value)
	if count < 1 || count > 60 {
		return false
	}
	for _, char := range value {
		if unicode.IsControl(char) || unicode.Is(unicode.Cf, char) {
			return false
		}
	}
	return true
}

func (p *Panel) requireActiveVPNEntitlement(
	w http.ResponseWriter, r *http.Request, subscriptionID int,
) bool {
	if p.hasEntitlement(r.Context(), subscriptionID, vpnProductID) {
		return true
	}
	writeCodedError(
		w, http.StatusPaymentRequired, errCodeEntitlement,
		`this subscription requires the "VPN access" add-on`, "/addons",
	)
	return false
}

func setVPNPrivateResponseHeaders(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

func vpnSyncErrorText(err error) string {
	if err == nil {
		return ""
	}
	return vpnPublicSyncError(err.Error())
}

// vpnPublicSyncError maps internal failures to a bounded message that cannot
// reveal hostnames, paths, commands, keys or RPC transport details.
// vpnPublicSyncError iç hataları ana makine, yol, komut, anahtar veya RPC
// taşıma ayrıntılarını açığa çıkarmayan sınırlı bir mesaja dönüştürür.
func vpnPublicSyncError(raw string) string {
	lower := strings.ToLower(strings.TrimSpace(raw))
	switch {
	case strings.Contains(lower, "timeout"),
		strings.Contains(lower, "deadline exceeded"):
		return "VPN synchronization timed out"
	case strings.Contains(lower, "unavailable"),
		strings.Contains(lower, "connection refused"),
		strings.Contains(lower, "not configured"),
		strings.Contains(lower, "secret storage"):
		return "VPN synchronization service is unavailable"
	default:
		return "VPN peer synchronization failed"
	}
}

// allocateVPNIP returns the lowest address not represented by any active or
// pending ledger row. A pending removal keeps its address until agent confirms it.
// allocateVPNIP defterdeki etkin ya da bekleyen hiçbir satırın kullanmadığı en
// düşük adresi verir. Bekleyen silme, agent doğrulayana kadar adresini korur.
func (p *Panel) allocateVPNIP(ctx context.Context) (string, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT ip FROM vpn_peers`)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for rows.Next() {
		var ip string
		if err := rows.Scan(&ip); err != nil {
			rows.Close()
			return "", err
		}
		used[ip] = true
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return "", err
	}
	rows.Close()
	for n := 2; n <= 254; n++ {
		ip := fmt.Sprintf("10.8.0.%d", n)
		if !used[ip] {
			return ip, nil
		}
	}
	return "", errVPNAddressPoolFull
}

// handleVPNPeerByID marks a peer revoked before asking the agent to remove it.
// A failed sync leaves a retryable tombstone instead of forgetting live access.
// handleVPNPeerByID agent'tan kaldırmasını istemeden önce peer'ı iptal edildi
// olarak işaretler. Başarısız eşitleme canlı erişimi unutmak yerine yeniden
// denenebilir bir iz bırakır.
func (p *Panel) handleVPNPeerByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	setVPNPrivateResponseHeaders(w)
	caller := currentCaller(r)
	if caller == nil {
		writeClientError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	suffix := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/vpn/peers/"), "/")
	segments := strings.Split(suffix, "/")
	if len(segments) < 1 || len(segments) > 2 {
		writeClientError(w, http.StatusNotFound, "peer not found")
		return
	}
	id, err := strconv.Atoi(segments[0])
	if err != nil || id <= 0 {
		writeClientError(w, http.StatusBadRequest, "invalid peer id")
		return
	}
	if len(segments) == 2 {
		if segments[1] != "ack" {
			writeClientError(w, http.StatusNotFound, "peer not found")
			return
		}
		if r.Method != http.MethodPost {
			writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		p.ackVPNPeerDelivery(w, r, caller, id)
		return
	}
	if r.Method != http.MethodDelete {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	var subscriptionID *int
	var name string
	err = p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT subscription_id, name FROM vpn_peers WHERE id = ?`, id,
	).Scan(&subscriptionID, &name)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "peer not found")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	if caller.Role != roleAdmin {
		if subscriptionID == nil ||
			p.canAccessSubscription(r.Context(), caller, *subscriptionID) != nil {
			writeClientError(w, http.StatusNotFound, "peer not found")
			return
		}
	}
	if _, err := p.db.GetDB().ExecContext(r.Context(), `
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    sync_error = NULL, updated_at = datetime('now')
		WHERE id = ?`, id); err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.syncVPNPeersLocked(r.Context()); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	p.audit(r, "vpn.peer.remove:"+name, "vpn_peer", id)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// ackVPNPeerDelivery converts a short-lived delivery receipt into durable
// issued state. The private client configuration itself is never persisted.
// ackVPNPeerDelivery kısa ömürlü teslim makbuzunu kalıcı verilmiş duruma
// dönüştürür. Özel istemci yapılandırmasının kendisi asla saklanmaz.
func (p *Panel) ackVPNPeerDelivery(
	w http.ResponseWriter, r *http.Request, caller *Caller, id int,
) {
	var request struct {
		DeliveryToken string `json:"delivery_token"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil ||
		len(request.DeliveryToken) != 32 {
		writeClientError(w, http.StatusBadRequest, "invalid delivery receipt")
		return
	}

	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	var subscriptionID *int
	var createdBy *int
	var name string
	err := p.db.GetDB().QueryRowContext(r.Context(), `
		SELECT subscription_id, created_by, name
		FROM vpn_peers WHERE id = ?`, id,
	).Scan(&subscriptionID, &createdBy, &name)
	if errors.Is(err, sql.ErrNoRows) {
		writeClientError(w, http.StatusNotFound, "peer not found")
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}
	if subscriptionID == nil ||
		p.canAccessSubscription(r.Context(), caller, *subscriptionID) != nil ||
		(caller.Role != roleAdmin && (createdBy == nil || *createdBy != caller.ID)) {
		writeClientError(w, http.StatusNotFound, "peer not found")
		return
	}

	result, err := p.db.GetDB().ExecContext(r.Context(), `
		UPDATE vpn_peers
		SET provisioning_state = 'issued', delivery_token_hash = NULL,
		    delivery_expires_at = NULL, updated_at = datetime('now')
		WHERE id = ?
		  AND desired_state = 'active'
		  AND provisioning_state = 'provisioning'
		  AND delivery_token_hash = ?
		  AND delivery_expires_at IS NOT NULL
		  AND julianday(delivery_expires_at) > julianday('now')`,
		id, vpnDeliveryTokenHash(request.DeliveryToken),
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if affected != 1 {
		writeClientError(w, http.StatusConflict, "delivery receipt expired or already used")
		return
	}
	p.audit(r, "vpn.peer.delivery_ack:"+name, "vpn_peer", id)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

// revokeVPNEntitlement suspends the right before removing peers. The grant is
// deleted only after the agent confirms the deny state, so failure stays closed.
// revokeVPNEntitlement peer'ları kaldırmadan önce hakkı askıya alır. Hak yalnız
// agent engelleme durumunu doğruladıktan sonra silinir; hata kapalı durumda kalır.
func (p *Panel) revokeVPNEntitlement(
	w http.ResponseWriter, r *http.Request, subscriptionID int,
) {
	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	var status string
	err := p.db.GetDB().QueryRowContext(r.Context(), `
		SELECT status FROM subscription_entitlements
		WHERE subscription_id = ? AND product_id = 'vpn'`,
		subscriptionID,
	).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		var orphanedPeers int
		if countErr := p.db.GetDB().QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM vpn_peers WHERE subscription_id = ?`,
			subscriptionID,
		).Scan(&orphanedPeers); countErr != nil {
			writeServerError(w, countErr)
			return
		}
		if orphanedPeers > 0 {
			if _, markErr := p.db.GetDB().ExecContext(r.Context(), `
				UPDATE vpn_peers
				SET desired_state = 'revoked', sync_state = 'pending',
				    sync_error = NULL, updated_at = datetime('now')
				WHERE subscription_id = ?`,
				subscriptionID,
			); markErr != nil {
				writeServerError(w, markErr)
				return
			}
			if syncErr := p.syncVPNPeersLocked(r.Context()); syncErr != nil {
				writeAgentError(w, syncErr, "VPN")
				return
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "idempotent": true})
		return
	}
	if err != nil {
		writeServerError(w, err)
		return
	}

	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(r.Context(), `
		UPDATE subscription_entitlements
		SET status = 'suspended'
		WHERE subscription_id = ? AND product_id = 'vpn'`,
		subscriptionID,
	); err != nil {
		writeServerError(w, err)
		return
	}
	_, err = tx.ExecContext(r.Context(), `
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    sync_error = NULL, updated_at = datetime('now')
		WHERE subscription_id = ? AND desired_state = 'active'`,
		subscriptionID,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}

	var peerCount int
	if err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM vpn_peers WHERE subscription_id = ?`,
		subscriptionID,
	).Scan(&peerCount); err != nil {
		writeServerError(w, err)
		return
	}
	if peerCount > 0 {
		if err := p.syncVPNPeersLocked(r.Context()); err != nil {
			writeAgentError(w, err, "VPN")
			return
		}
	}

	// A successful revoke must be proven by an empty desired-state ledger for
	// this subscription. This also makes retry after a failed first sync safe.
	// Başarılı iptal, bu aboneliğin istenen durum defterinin boş olmasıyla
	// kanıtlanır. Böylece ilk eşitleme hatasından sonraki deneme de güvenli olur.
	if err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM vpn_peers WHERE subscription_id = ?`,
		subscriptionID,
	).Scan(&peerCount); err != nil {
		writeServerError(w, err)
		return
	}
	if peerCount != 0 {
		writeServerError(w, errors.New("VPN peer revocation was not confirmed"))
		return
	}

	tx, err = p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(), `
		DELETE FROM subscription_entitlements
		WHERE subscription_id = ? AND product_id = 'vpn'`,
		subscriptionID,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if affected > 0 {
		if err := insertEntitlementAudit(
			r, tx, "entitlement.revoke:vpn", subscriptionID,
		); err != nil {
			writeServerError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{
		"success": true, "idempotent": affected == 0,
	})
}

// reconcileVPNEntitlements revokes peers whose subscription right disappeared,
// was suspended or expired. It is also run once immediately after startup.
// reconcileVPNEntitlements abonelik hakkı silinen, askıya alınan ya da süresi
// dolan peer'ları iptal eder. Başlangıçtan hemen sonra da bir kez çalışır.
func (p *Panel) reconcileVPNEntitlements(ctx context.Context) error {
	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	expired, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    sync_error = NULL, delivery_token_hash = NULL,
		    delivery_expires_at = NULL, updated_at = datetime('now')
		WHERE provisioning_state = 'provisioning'
		  AND delivery_expires_at IS NOT NULL
		  AND julianday(delivery_expires_at) <= julianday('now')`)
	if err != nil {
		return err
	}
	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    sync_error = NULL, updated_at = datetime('now')
		WHERE subscription_id IS NOT NULL
		  AND desired_state = 'active'
		  AND NOT EXISTS (
			SELECT 1
			FROM subscription_entitlements e
			JOIN store_offerings o ON o.id = e.product_id
			WHERE e.subscription_id = vpn_peers.subscription_id
			  AND e.product_id = 'vpn'
			  AND e.status = 'active'
			  AND o.release_state = 'available'
			  AND o.entitlement_mode = 'grant'
			  AND (
				e.expires_at IS NULL
				OR (
					julianday(e.expires_at) IS NOT NULL
					AND julianday(e.expires_at) > julianday('now')
				)
			  )
		  )`)
	if err != nil {
		return err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	expiredCount, err := expired.RowsAffected()
	if err != nil {
		return err
	}
	var pending int
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT COUNT(*) FROM vpn_peers
		WHERE desired_state = 'revoked'`,
	).Scan(&pending); err != nil {
		return err
	}
	if changed == 0 && expiredCount == 0 && pending == 0 {
		return nil
	}
	return p.syncVPNPeersLocked(ctx)
}

// recoverVPNProvisioningState removes peers whose one-time private config was
// not durably acknowledged before a restart. It runs synchronously before HTTP.
// recoverVPNProvisioningState, yeniden başlatmadan önce tek kullanımlık özel
// config'i kalıcı olarak onaylanmayan peer'ları HTTP başlamadan eşzamanlı kaldırır.
func (p *Panel) recoverVPNProvisioningState(ctx context.Context) error {
	vpnMutationMu.Lock()
	defer vpnMutationMu.Unlock()

	result, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE vpn_peers
		SET desired_state = 'revoked', sync_state = 'pending',
		    sync_error = NULL, updated_at = datetime('now')
		WHERE provisioning_state = 'provisioning'`)
	if err != nil {
		return err
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return nil
	}
	if err := p.syncVPNPeersLocked(ctx); err != nil {
		return fmt.Errorf("remove incomplete VPN provisioning: %w", err)
	}
	return nil
}

func (p *Panel) startVPNEntitlementReconciler() {
	go func() {
		run := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			if err := p.reconcileVPNEntitlements(ctx); err != nil {
				log.Printf("VPN entitlement reconciliation failed: %v", err)
			}
		}
		run()
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	}()
}
