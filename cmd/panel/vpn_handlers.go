package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// The panel side of the built-in VPN. The server is set up once by the admin
// (the WireGuard service comes from the managed-service catalogue like any
// other). Peers are the sellable unit: admins create them freely, customers
// need the "vpn" product on their subscription. The panel allocates addresses
// and keeps the ledger; every change pushes the full peer set to the agent.
//
// Yerleşik VPN'in panel tarafı. Sunucuyu yönetici bir kez kurar (WireGuard
// servisi, diğerleri gibi yönetilen-servis kataloğundan gelir). Peer'lar
// satılabilir birimdir: yöneticiler serbestçe oluşturur, müşterilerin
// aboneliğinde "vpn" ürünü olmalıdır. Adresleri panel tahsis eder ve defteri
// tutar; her değişiklik tam peer setini agent'a iter.

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
}

// handleVPNStatus returns the live server state (any signed-in user — the
// endpoint and public key are in every client config anyway).
// handleVPNStatus, canlı sunucu durumunu döndürür (giriş yapmış herkes —
// uç nokta ve genel anahtar zaten her istemci config'inde var).
func (p *Panel) handleVPNStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	var st struct {
		Installed       bool   `json:"installed"`
		Configured      bool   `json:"configured"`
		Running         bool   `json:"running"`
		ServerPublicKey string `json:"server_public_key,omitempty"`
		Port            int    `json:"port,omitempty"`
		Endpoint        string `json:"endpoint,omitempty"`
		Peers           []struct {
			PublicKey     string `json:"public_key"`
			LastHandshake int64  `json:"last_handshake"`
			RxBytes       int64  `json:"rx_bytes"`
			TxBytes       int64  `json:"tx_bytes"`
		} `json:"peers,omitempty"`
		Error string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.VPNStatus", &struct{}{}, &st); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	var peerCount int
	_ = p.db.GetDB().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM vpn_peers`).Scan(&peerCount)
	json.NewEncoder(w).Encode(map[string]any{
		"installed": st.Installed, "configured": st.Configured, "running": st.Running,
		"server_public_key": st.ServerPublicKey, "port": st.Port, "endpoint": st.Endpoint,
		"peer_count": peerCount,
	})
}

// handleVPNSetup (admin-only) brings the WireGuard server up and syncs any
// peers already in the ledger (e.g. after a reinstall).
// handleVPNSetup (yalnız yönetici) WireGuard sunucusunu kaldırır ve defterde
// zaten olan peer'ları senkronlar (örn. yeniden kurulum sonrası).
func (p *Panel) handleVPNSetup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}
	var req struct {
		Port int `json:"port"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	var resp struct {
		Created bool   `json:"created"`
		Detail  string `json:"detail,omitempty"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.SetupVPN", &struct {
		Port int `json:"port"`
	}{Port: req.Port}, &resp); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	if resp.Error != "" {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}
	if err := p.syncVPNPeers(r.Context()); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "vpn.setup", "", 0)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "created": resp.Created, "detail": resp.Detail})
}

// syncVPNPeers pushes the full ledger to the agent — the single write path
// for the server's peer section.
// syncVPNPeers, tam defteri agent'a iter — sunucunun peer bölümü için tek
// yazma yolu.
func (p *Panel) syncVPNPeers(ctx context.Context) error {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT public_key, preshared_key, ip FROM vpn_peers`)
	if err != nil {
		return err
	}
	type spec struct {
		PublicKey    string `json:"public_key"`
		PresharedKey string `json:"preshared_key"`
		IP           string `json:"ip"`
	}
	var peers []spec
	for rows.Next() {
		var s spec
		if rows.Scan(&s.PublicKey, &s.PresharedKey, &s.IP) == nil {
			peers = append(peers, s)
		}
	}
	rows.Close()
	var resp struct {
		Applied bool   `json:"applied"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.SyncVPNPeers", &struct {
		Peers []spec `json:"peers"`
	}{Peers: peers}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("peer sync: %s", resp.Error)
	}
	return nil
}

// handleVPNPeers lists (GET) and creates (POST) peers. Admins see and create
// everything; other roles are scoped to subscriptions they can access and
// need the "vpn" entitlement to create.
// handleVPNPeers, peer'ları listeler (GET) ve oluşturur (POST). Yöneticiler
// her şeyi görür ve oluşturur; diğer roller erişebildikleri aboneliklerle
// sınırlıdır ve oluşturmak için "vpn" hakkına ihtiyaç duyar.
func (p *Panel) handleVPNPeers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := currentCaller(r)
	if c == nil {
		writeClientError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	switch r.Method {
	case http.MethodGet:
		p.listVPNPeers(w, r, c)
	case http.MethodPost:
		p.createVPNPeer(w, r, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) listVPNPeers(w http.ResponseWriter, r *http.Request, c *Caller) {
	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT vp.id, vp.subscription_id, vp.name, vp.public_key, vp.ip, vp.created_at, s.name
		FROM vpn_peers vp LEFT JOIN subscriptions s ON s.id = vp.subscription_id
		ORDER BY vp.id`)
	if err != nil {
		writeServerError(w, err)
		return
	}
	var peers []vpnPeerRow
	for rows.Next() {
		var pr vpnPeerRow
		if rows.Scan(&pr.ID, &pr.SubscriptionID, &pr.Name, &pr.PublicKey, &pr.IP, &pr.CreatedAt, &pr.Subscription) != nil {
			continue
		}
		peers = append(peers, pr)
	}
	rows.Close()

	// Non-admins only see peers on subscriptions they can access.
	// Yönetici olmayanlar yalnız erişebildikleri aboneliklerin peer'larını görür.
	if c.Role != roleAdmin {
		var mine []vpnPeerRow
		for _, pr := range peers {
			if pr.SubscriptionID != nil && p.canAccessSubscription(r.Context(), c, *pr.SubscriptionID) == nil {
				mine = append(mine, pr)
			}
		}
		peers = mine
	}

	// Merge live kernel stats so "connected right now" is real, not implied.
	// Canlı çekirdek istatistiklerini birleştir; "şu an bağlı" gerçek olsun.
	var st struct {
		Peers []struct {
			PublicKey     string `json:"public_key"`
			LastHandshake int64  `json:"last_handshake"`
			RxBytes       int64  `json:"rx_bytes"`
			TxBytes       int64  `json:"tx_bytes"`
		} `json:"peers,omitempty"`
	}
	if p.agentClient.Call("Agent.VPNStatus", &struct{}{}, &st) == nil {
		live := map[string][3]int64{}
		for _, lp := range st.Peers {
			live[lp.PublicKey] = [3]int64{lp.LastHandshake, lp.RxBytes, lp.TxBytes}
		}
		for i := range peers {
			if v, ok := live[peers[i].PublicKey]; ok {
				peers[i].LastHandshake, peers[i].RxBytes, peers[i].TxBytes = v[0], v[1], v[2]
			}
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"peers": peers})
}

// createVPNPeer allocates the next free address, has the agent generate keys,
// records the peer and returns the complete client config — private key
// included, shown exactly once and never stored.
// createVPNPeer, sıradaki boş adresi tahsis eder, anahtarları agent'a
// ürettirir, peer'ı kaydeder ve eksiksiz istemci config'ini döndürür — özel
// anahtar dahil, tam bir kez gösterilir ve asla saklanmaz.
func (p *Panel) createVPNPeer(w http.ResponseWriter, r *http.Request, c *Caller) {
	var req struct {
		Name           string `json:"name"`
		SubscriptionID int    `json:"subscription_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" || len(req.Name) > 60 {
		writeClientError(w, http.StatusBadRequest, "peer name is required (max 60 chars)")
		return
	}

	var subID *int
	if req.SubscriptionID > 0 {
		if p.canAccessSubscription(r.Context(), c, req.SubscriptionID) != nil {
			writeClientError(w, http.StatusNotFound, "subscription not found")
			return
		}
		subID = &req.SubscriptionID
	}
	if c.Role != roleAdmin {
		if subID == nil {
			writeClientError(w, http.StatusBadRequest, "subscription_id is required")
			return
		}
		if !p.requireEntitlement(w, r, *subID, "vpn") {
			return
		}
	}

	// The server must be up before issuing configs that point at it.
	// Ona işaret eden config'ler vermeden önce sunucu ayakta olmalı.
	var st struct {
		Running         bool   `json:"running"`
		ServerPublicKey string `json:"server_public_key"`
		Port            int    `json:"port"`
		Endpoint        string `json:"endpoint"`
	}
	if err := p.agentClient.Call("Agent.VPNStatus", &struct{}{}, &st); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	if !st.Running || st.ServerPublicKey == "" {
		writeClientError(w, http.StatusConflict, "the VPN server is not running — set it up first")
		return
	}

	ip, err := p.allocateVPNIP(r.Context())
	if err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return
	}

	var keys struct {
		PrivateKey   string `json:"private_key"`
		PublicKey    string `json:"public_key"`
		PresharedKey string `json:"preshared_key"`
		Error        string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.GenerateVPNKeys", &struct{}{}, &keys); err != nil {
		writeAgentError(w, err, "VPN")
		return
	}
	if keys.Error != "" {
		writeServerError(w, fmt.Errorf("key generation: %s", keys.Error))
		return
	}

	res, err := p.db.GetDB().ExecContext(r.Context(), `
		INSERT INTO vpn_peers (subscription_id, name, public_key, preshared_key, ip, created_by)
		VALUES (?, ?, ?, ?, ?, ?)`,
		subID, req.Name, keys.PublicKey, keys.PresharedKey, ip, c.ID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	peerID, _ := res.LastInsertId()

	if err := p.syncVPNPeers(r.Context()); err != nil {
		// Do not leave a ledger row the server does not serve.
		// Sunucunun sunmadığı bir defter satırı bırakma.
		p.db.GetDB().ExecContext(r.Context(), `DELETE FROM vpn_peers WHERE id = ?`, peerID)
		writeServerError(w, err)
		return
	}

	clientConf := fmt.Sprintf(`[Interface]
PrivateKey = %s
Address = %s/32
DNS = 1.1.1.1

[Peer]
PublicKey = %s
PresharedKey = %s
AllowedIPs = 0.0.0.0/0
Endpoint = %s:%d
PersistentKeepalive = 25
`, keys.PrivateKey, ip, st.ServerPublicKey, keys.PresharedKey, st.Endpoint, st.Port)

	p.audit(r, "vpn.peer.add:"+req.Name, "vpn_peer", int(peerID))
	json.NewEncoder(w).Encode(map[string]any{
		"success": true, "id": peerID, "ip": ip, "public_key": keys.PublicKey,
		"client_config": clientConf,
	})
}

// allocateVPNIP hands out the lowest free host address in 10.8.0.0/24
// (server holds .1), reusing addresses freed by deleted peers.
// allocateVPNIP, 10.8.0.0/24 içindeki en düşük boş adresi verir (.1
// sunucunundur); silinen peer'ların boşalttığı adresleri yeniden kullanır.
func (p *Panel) allocateVPNIP(ctx context.Context) (string, error) {
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT ip FROM vpn_peers`)
	if err != nil {
		return "", err
	}
	used := map[string]bool{}
	for rows.Next() {
		var ip string
		if rows.Scan(&ip) == nil {
			used[ip] = true
		}
	}
	rows.Close()
	for n := 2; n <= 254; n++ {
		ip := fmt.Sprintf("10.8.0.%d", n)
		if !used[ip] {
			return ip, nil
		}
	}
	return "", fmt.Errorf("the VPN address pool is full (253 peers)")
}

// handleVPNPeerByID deletes a peer (owner or admin) and re-syncs the server.
// handleVPNPeerByID, bir peer'ı siler (sahibi veya yönetici) ve sunucuyu
// yeniden senkronlar.
func (p *Panel) handleVPNPeerByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := currentCaller(r)
	if c == nil {
		writeClientError(w, http.StatusUnauthorized, "sign-in required")
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id, err := strconv.Atoi(strings.TrimPrefix(r.URL.Path, "/api/v1/vpn/peers/"))
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid peer id")
		return
	}
	var subID *int
	var name string
	if p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT subscription_id, name FROM vpn_peers WHERE id = ?`, id).Scan(&subID, &name) != nil {
		writeClientError(w, http.StatusNotFound, "peer not found")
		return
	}
	if c.Role != roleAdmin {
		if subID == nil || p.canAccessSubscription(r.Context(), c, *subID) != nil {
			writeClientError(w, http.StatusNotFound, "peer not found")
			return
		}
	}
	if _, err := p.db.GetDB().ExecContext(r.Context(), `DELETE FROM vpn_peers WHERE id = ?`, id); err != nil {
		writeServerError(w, err)
		return
	}
	if err := p.syncVPNPeers(r.Context()); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "vpn.peer.remove:"+name, "vpn_peer", id)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}
