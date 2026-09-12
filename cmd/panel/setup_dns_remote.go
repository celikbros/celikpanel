package main

// Source implementation and local testing explicitly authorized by the user:
// "herşeyi onaylıyorum devam et". This module never pairs a host implicitly.

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const remoteDNSMaxBytes = 1 << 20
const settingRemoteDNSConnection = "remote_dns_connection_id"

var remoteDNSExchange = remoteDNSHTTPJSON
var errRemoteDNSNotReady = errors.New("authorized remote DNS authority is not ready")

type remoteDNSAuthority struct {
	ClientID    string   `json:"client_id"`
	Ready       bool     `json:"ready"`
	Engine      string   `json:"engine"`
	Epoch       int64    `json:"epoch"`
	Nameservers []string `json:"nameservers"`
	PrimaryIP   string   `json:"primary_ip,omitempty"`
	SecondaryIP string   `json:"secondary_ip,omitempty"`
}

type remoteDNSConnection struct {
	ID             string   `json:"id"`
	Endpoint       string   `json:"endpoint"`
	Label          string   `json:"label"`
	Status         string   `json:"status"`
	Nameservers    []string `json:"nameservers"`
	CreatedAt      string   `json:"created_at"`
	credential     string
	enrollmentCode string
}

type remoteDNSAcceptRequest struct {
	Cancel     bool   `json:"cancel,omitempty"`
	ClientID   string `json:"client_id"`
	Credential string `json:"credential"`
	Label      string `json:"label"`
}

type remoteDNSStatusRequest struct {
	Revoke              bool `json:"revoke,omitempty"`
	IncludePairIdentity bool `json:"include_pair_identity,omitempty"`
}

type remoteDNSMachineClaim struct {
	path           string
	clientID       string
	credentialHash string
	enrollmentHash string
	status         *remoteDNSStatusRequest
	accept         *remoteDNSAcceptRequest
}
type remoteDNSMachineClaimKey struct{}

func remoteDNSMachineVerified(r *http.Request) bool {
	claim, ok := r.Context().Value(remoteDNSMachineClaimKey{}).(remoteDNSMachineClaim)
	return ok && claim.path == r.URL.Path && remoteDNSMachineRequest(r) && r.Method == http.MethodPost &&
		(claim.clientID != "" || claim.enrollmentHash != "")
}

func remoteDNSSecret() (string, error) {
	var secret [32]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(secret[:]), nil
}
func remoteDNSHash(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func remoteDNSValidSecret(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}
func remoteDNSHashEqual(left, right string) bool {
	return len(left) == 64 && len(right) == 64 && subtle.ConstantTimeCompare([]byte(left), []byte(right)) == 1
}

func decodeRemoteDNSBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, remoteDNSMaxBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid remote DNS request")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeClientError(w, http.StatusBadRequest, "invalid remote DNS request")
		return false
	}
	return true
}

func (p *Panel) authenticateRemoteDNSMachine(w http.ResponseWriter, r *http.Request) bool {
	deny := func() bool {
		writeCodedError(w, http.StatusUnauthorized, "REMOTE_DNS_UNAUTHORIZED", "Remote DNS authorization is invalid or revoked", "")
		return false
	}
	if !remoteDNSMachineRequest(r) || r.Method != http.MethodPost || r.URL.RawQuery != "" {
		return deny()
	}
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "Bearer ") || len(r.Header.Values("Authorization")) != 1 {
		return deny()
	}
	secret := strings.TrimPrefix(auth, "Bearer ")
	claim := remoteDNSMachineClaim{path: r.URL.Path}
	if r.URL.Path == "/api/v1/dns/remote/accept" {
		if !remoteDNSValidSecret(secret) {
			return deny()
		}
		var request remoteDNSAcceptRequest
		if !decodeRemoteDNSBody(w, r, &request) {
			return false
		}
		if !validServiceOperationID(request.ClientID) || !remoteDNSValidSecret(request.Credential) || !remoteDNSValidLabel(request.Label) {
			return deny()
		}
		var expires int64
		var consumed string
		codeHash := remoteDNSHash(secret)
		if err := p.db.GetDB().QueryRowContext(r.Context(), `SELECT expires_at,client_id FROM remote_dns_enrollments WHERE code_hash=?`, codeHash).Scan(&expires, &consumed); err != nil {
			return deny()
		}
		if consumed == "" {
			if time.Now().Unix() >= expires && !request.Cancel {
				return deny()
			}
		} else {
			// Consumed codes only reconcile their exact persisted client secret;
			// expiration never grants a second enrollment after a lost reply.
			if consumed != request.ClientID || !p.remoteDNSClientCredentialMatches(r.Context(), request.ClientID, remoteDNSHash(request.Credential), request.Cancel) {
				return deny()
			}
		}
		claim.enrollmentHash = codeHash
		claim.accept = &request
	} else {
		id, credential, ok := strings.Cut(secret, ".")
		if !ok || !validServiceOperationID(id) || !remoteDNSValidSecret(credential) {
			return deny()
		}
		claim.clientID = id
		claim.credentialHash = remoteDNSHash(credential)
		if r.URL.Path == "/api/v1/dns/remote/receiver/status" {
			var status remoteDNSStatusRequest
			if !decodeRemoteDNSBody(w, r, &status) {
				return false
			}
			claim.status = &status
		}
		var stored string
		var revoked bool
		if p.db.GetDB().QueryRowContext(r.Context(), `SELECT credential_hash,revoked FROM remote_dns_clients WHERE id=?`, id).Scan(&stored, &revoked) != nil || !remoteDNSHashEqual(stored, claim.credentialHash) || (revoked && (claim.status == nil || !claim.status.Revoke)) {
			return deny()
		}
	}
	*r = *r.WithContext(context.WithValue(r.Context(), remoteDNSMachineClaimKey{}, claim))
	return true
}

func remoteDNSValidLabel(label string) bool {
	return label != "" && len(label) <= 128 && strings.TrimSpace(label) == label && !strings.ContainsAny(label, "\x00\r\n")
}
func (p *Panel) remoteDNSClientAuthorized(ctx context.Context, id, hash string) bool {
	return p.remoteDNSClientCredentialMatches(ctx, id, hash, false)
}

func (p *Panel) remoteDNSClientCredentialMatches(ctx context.Context, id, hash string, allowRevoked bool) bool {
	var stored string
	var revoked bool
	return p.db.GetDB().QueryRowContext(ctx, `SELECT credential_hash,revoked FROM remote_dns_clients WHERE id=?`, id).Scan(&stored, &revoked) == nil && (!revoked || allowRevoked) && remoteDNSHashEqual(stored, hash)
}

func (p *Panel) remoteDNSLocalAuthority(ctx context.Context) (remoteDNSAuthority, error) {
	proof := remoteDNSAuthority{Nameservers: []string{}}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return proof, err
	}
	publisher, ready, err := p.activeDNSPublisher(ctx)
	if err != nil || !ready || publisher.PairRole != transport.DNSPairRolePrimary || publisher.Epoch < 1 {
		return proof, errRemoteDNSNotReady
	}
	if ready, err = remoteDNSNameserversReady(p, ctx); err != nil || !ready {
		return proof, errRemoteDNSNotReady
	}
	ns1, ns2 := p.configuredNameservers(ctx)
	current, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil || current != state || state.ActiveEngine != publisher.Engine || state.EngineEpoch != publisher.Epoch ||
		state.PairRole != transport.DNSPairRolePrimary || state.Topology != transport.DNSTopologyPaired ||
		state.CurrentSwitchID != "" || state.LocalNS != ns1 || state.PeerNS != ns2 {
		return proof, errRemoteDNSNotReady
	}
	primary, primaryValid := canonicalIPv4(state.LocalIP)
	secondary, secondaryValid := canonicalIPv4(state.PeerIP)
	if !primaryValid || !secondaryValid || primary == secondary {
		return proof, errRemoteDNSNotReady
	}
	proof = remoteDNSAuthority{Ready: true, Engine: string(publisher.Engine), Epoch: publisher.Epoch, Nameservers: []string{ns1, ns2}, PrimaryIP: primary, SecondaryIP: secondary}
	return proof, nil
}

// Kept injectable only at the package boundary for isolated HTTP/RPC fixtures.
// Production authority always uses managed runtime, pair proof and public DNS.
var remoteDNSReadLocalAuthority = (*Panel).remoteDNSLocalAuthority
var remoteDNSNameserversReady = (*Panel).setupDNSNameserverReadiness

func (p *Panel) handleRemoteDNSMachine(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !remoteDNSMachineVerified(r) && !p.authenticateRemoteDNSMachine(w, r) {
		return
	}
	if !p.allowLicensedPanel(w, r) {
		return
	}
	claim := r.Context().Value(remoteDNSMachineClaimKey{}).(remoteDNSMachineClaim)
	if claim.accept != nil {
		p.acceptRemoteDNSClient(w, r, claim)
		return
	}
	if claim.status != nil && claim.status.Revoke {
		p.serviceMutationMu.Lock()
		defer p.serviceMutationMu.Unlock()
		var stored string
		if p.db.GetDB().QueryRowContext(r.Context(), `SELECT credential_hash FROM remote_dns_clients WHERE id=?`, claim.clientID).Scan(&stored) != nil || !remoteDNSHashEqual(stored, claim.credentialHash) {
			writeClientError(w, 401, "Remote DNS authorization unavailable")
			return
		}
		if _, err := p.db.GetDB().ExecContext(r.Context(), `UPDATE remote_dns_clients SET revoked=1 WHERE id=? AND credential_hash=?`, claim.clientID, stored); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"revoked": true, "client_id": claim.clientID})
		return
	}
	if !p.remoteDNSClientAuthorized(r.Context(), claim.clientID, claim.credentialHash) {
		writeClientError(w, http.StatusUnauthorized, "Remote DNS authorization was revoked")
		return
	}
	switch r.URL.Path {
	case "/api/v1/dns/remote/receiver/status":
		proof, err := remoteDNSReadLocalAuthority(p, r.Context())
		if err != nil {
			remoteDNSWriteNotReady(w)
			return
		}
		proof.ClientID = claim.clientID
		if claim.status == nil || !claim.status.IncludePairIdentity {
			// Older origins reject unknown response fields. Pair identity is an
			// explicit capability request, not an unsolicited wire extension.
			proof.PrimaryIP, proof.SecondaryIP = "", ""
		}
		_ = json.NewEncoder(w).Encode(proof)
	case "/api/v1/dns/remote/receiver/publish":
		p.handleRemoteDNSReceivePublication(w, r, claim)
	default:
		http.NotFound(w, r)
	}
}
func remoteDNSWriteNotReady(w http.ResponseWriter) {
	writeCodedError(w, http.StatusConflict, "REMOTE_DNS_AUTHORITY_NOT_READY", "The authorized DNS authority and its secondary must be ready", "")
}

func (p *Panel) acceptRemoteDNSClient(w http.ResponseWriter, r *http.Request, claim remoteDNSMachineClaim) {
	request := claim.accept
	proof := remoteDNSAuthority{}
	var err error
	if !request.Cancel {
		proof, err = remoteDNSReadLocalAuthority(p, r.Context())
		if err != nil {
			remoteDNSWriteNotReady(w)
			return
		}
	}
	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	var expires int64
	var consumed string
	err = tx.QueryRowContext(r.Context(), `SELECT expires_at,client_id FROM remote_dns_enrollments WHERE code_hash=?`, claim.enrollmentHash).Scan(&expires, &consumed)
	if err != nil {
		writeClientError(w, http.StatusUnauthorized, "Remote DNS enrollment unavailable")
		return
	}
	if consumed == "" {
		if time.Now().Unix() >= expires && !request.Cancel {
			writeClientError(w, http.StatusUnauthorized, "Remote DNS enrollment expired")
			return
		}
		_, err = tx.ExecContext(r.Context(), `INSERT INTO remote_dns_clients(id,credential_hash,label,created_at,revoked) VALUES(?,?,?,?,?)`, request.ClientID, remoteDNSHash(request.Credential), request.Label, time.Now().UTC().Format(time.RFC3339), request.Cancel)
		if err == nil {
			_, err = tx.ExecContext(r.Context(), `UPDATE remote_dns_enrollments SET client_id=? WHERE code_hash=? AND client_id=''`, request.ClientID, claim.enrollmentHash)
		}
	} else {
		var hash, label string
		var revoked bool
		err = tx.QueryRowContext(r.Context(), `SELECT credential_hash,label,revoked FROM remote_dns_clients WHERE id=?`, request.ClientID).Scan(&hash, &label, &revoked)
		if err == nil && (consumed != request.ClientID || (revoked && !request.Cancel) || label != request.Label || !remoteDNSHashEqual(hash, remoteDNSHash(request.Credential))) {
			err = errors.New("enrollment identity differs")
		}
	}
	if err != nil {
		writeClientError(w, http.StatusConflict, "Remote DNS enrollment identity is unavailable")
		return
	}
	if request.Cancel {
		if _, err = tx.ExecContext(r.Context(), `UPDATE remote_dns_clients SET revoked=1 WHERE id=?`, request.ClientID); err != nil {
			writeServerError(w, err)
			return
		}
	}
	if err = tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	if request.Cancel {
		_ = json.NewEncoder(w).Encode(map[string]any{"client_id": request.ClientID, "revoked": true})
		return
	}
	proof.ClientID = request.ClientID
	proof.PrimaryIP, proof.SecondaryIP = "", ""
	_ = json.NewEncoder(w).Encode(proof)
}

func (p *Panel) readRemoteDNSConnection(ctx context.Context, id string) (remoteDNSConnection, error) {
	var c remoteDNSConnection
	var nameservers string
	if !validServiceOperationID(id) {
		return c, errors.New("invalid DNS connection identity")
	}
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT id,endpoint,label,status,nameservers_json,created_at,credential,enrollment_code FROM remote_dns_connections WHERE id=?`, id).Scan(&c.ID, &c.Endpoint, &c.Label, &c.Status, &nameservers, &c.CreatedAt, &c.credential, &c.enrollmentCode)
	if err != nil {
		return c, err
	}
	err = json.Unmarshal([]byte(nameservers), &c.Nameservers)
	if c.Nameservers == nil {
		c.Nameservers = []string{}
	}
	return c, err
}
func validateRemoteDNSAuthority(proof remoteDNSAuthority, id string) error {
	if !proof.Ready || proof.ClientID != id || !transport.ValidDNSEngine(transport.DNSEngine(proof.Engine)) || proof.Epoch < 1 || len(proof.Nameservers) != 2 || proof.Nameservers[0] == proof.Nameservers[1] {
		return errRemoteDNSNotReady
	}
	for _, ns := range proof.Nameservers {
		if !validDNSHostname(ns) || canonicalDNSName(ns) != ns {
			return errRemoteDNSNotReady
		}
	}
	return nil
}
func (p *Panel) remoteDNSConnectionReadiness(ctx context.Context, id string) (remoteDNSAuthority, error) {
	return p.remoteDNSConnectionReadinessWithPair(ctx, id, false)
}

// The combined secondary/hosting setup binds its remote publisher to the exact
// local DNS peer. Ordinary connectors retain their original response contract.
func (p *Panel) remoteDNSConnectionPairReadiness(ctx context.Context, id string) (remoteDNSAuthority, error) {
	return p.remoteDNSConnectionReadinessWithPair(ctx, id, true)
}

func (p *Panel) remoteDNSConnectionReadinessWithPair(ctx context.Context, id string, includePairIdentity bool) (remoteDNSAuthority, error) {
	var proof remoteDNSAuthority
	c, err := p.readRemoteDNSConnection(ctx, id)
	if err != nil {
		return proof, err
	}
	if c.Status != "ready" || !remoteDNSValidSecret(c.credential) {
		return proof, errRemoteDNSNotReady
	}
	err = remoteDNSExchange(ctx, c.Endpoint, "/api/v1/dns/remote/receiver/status", c.ID+"."+c.credential, remoteDNSStatusRequest{IncludePairIdentity: includePairIdentity}, &proof)
	if err == nil {
		err = validateRemoteDNSAuthority(proof, id)
	}
	return proof, err
}
func (p *Panel) defaultRemoteDNSConnectionID(ctx context.Context) (string, error) {
	var id string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key=?`, settingRemoteDNSConnection).Scan(&id)
	if err == nil && !validServiceOperationID(id) {
		err = errors.New("invalid saved remote DNS connection")
	}
	return id, err
}
func (p *Panel) domainRemoteDNSConnectionID(ctx context.Context, name string) (string, error) {
	var id string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT dns_remote_connection_id FROM domains WHERE name=? AND dns_management='existing'`, name).Scan(&id)
	if err == nil && !validServiceOperationID(id) {
		err = errors.New("domain has no authorized DNS connection")
	}
	return id, err
}
func (p *Panel) saveSetupRemoteDNSConnection(ctx context.Context, id string) error {
	if _, err := p.remoteDNSConnectionReadiness(ctx, id); err != nil {
		return err
	}
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for key, value := range map[string]string{settingRemoteDNSConnection: id, settingSetupDNSMode: setupDNSModeExisting} {
		if _, err = tx.ExecContext(ctx, `INSERT INTO panel_settings(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Marshal roundtrip deliberately omits origin-held reusable credentials.
func remoteDNSPublicConnection(c remoteDNSConnection) remoteDNSConnection {
	c.credential = ""
	c.enrollmentCode = ""
	return c
}

func remoteDNSCanonicalJSON(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	if len(raw) > remoteDNSMaxBytes {
		return nil, errors.New("remote DNS payload too large")
	}
	return bytes.Clone(raw), nil
}
