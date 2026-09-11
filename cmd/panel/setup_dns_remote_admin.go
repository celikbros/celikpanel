package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (p *Panel) handleRemoteDNSAdmin(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	if !requireServerSetupAdmin(w, r) || !p.allowLicensedPanel(w, r) {
		return
	}
	switch r.URL.Path {
	case "/api/v1/dns/remote/enrollments":
		if r.Method != http.MethodPost {
			rejectRouteMethod(w, []string{http.MethodPost})
			return
		}
		var request struct{}
		if !decodeRemoteDNSBody(w, r, &request) {
			return
		}
		if _, err := remoteDNSReadLocalAuthority(p, r.Context()); err != nil {
			remoteDNSWriteNotReady(w)
			return
		}
		code, err := remoteDNSSecret()
		if err != nil {
			writeServerError(w, err)
			return
		}
		expires := time.Now().UTC().Add(10 * time.Minute)
		if _, err = p.db.GetDB().ExecContext(r.Context(), `INSERT INTO remote_dns_enrollments(code_hash,expires_at) VALUES(?,?)`, remoteDNSHash(code), expires.Unix()); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"enrollment_code": code, "expires_at": expires.Format(time.RFC3339)})
	case "/api/v1/dns/remote/clients":
		p.handleRemoteDNSClients(w, r)
	case "/api/v1/dns/remote/connections":
		p.handleRemoteDNSConnections(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (p *Panel) handleRemoteDNSClients(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodDelete {
		id := r.URL.Query().Get("id")
		if !validServiceOperationID(id) {
			writeClientError(w, 400, "invalid publishing client")
			return
		}
		// Serialize with receiver publication: revocation cannot race between
		// its final permission check and the immutable zone mutation.
		p.serviceMutationMu.Lock()
		defer p.serviceMutationMu.Unlock()
		if _, err := p.db.GetDB().ExecContext(r.Context(), `UPDATE remote_dns_clients SET revoked=1 WHERE id=?`, id); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"revoked": true})
		return
	}
	if r.Method != http.MethodGet {
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodDelete})
		return
	}
	rows, err := p.db.GetDB().QueryContext(r.Context(), `SELECT id,label,created_at,revoked FROM remote_dns_clients ORDER BY created_at,id`)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()
	type client struct {
		ID        string `json:"id"`
		Label     string `json:"label"`
		CreatedAt string `json:"created_at"`
		Revoked   bool   `json:"revoked"`
	}
	list := []client{}
	for rows.Next() {
		var c client
		if err = rows.Scan(&c.ID, &c.Label, &c.CreatedAt, &c.Revoked); err != nil {
			writeServerError(w, err)
			return
		}
		list = append(list, c)
	}
	if err = rows.Err(); err != nil {
		writeServerError(w, err)
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"clients": list})
}

func (p *Panel) handleRemoteDNSConnections(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		id := r.URL.Query().Get("id")
		if id != "" && r.URL.Query().Get("check") == "1" {
			c, err := p.readRemoteDNSConnection(r.Context(), id)
			if err != nil {
				writeClientError(w, 404, "DNS connection not found")
				return
			}
			proof, proofErr := p.remoteDNSConnectionReadiness(r.Context(), id)
			if proofErr != nil {
				_ = json.NewEncoder(w).Encode(map[string]any{"connection": remoteDNSPublicConnection(c), "verified": false, "proof_error": "REMOTE_DNS_AUTHORITY_NOT_READY"})
				return
			}
			c.Nameservers = proof.Nameservers
			_ = json.NewEncoder(w).Encode(map[string]any{"connection": remoteDNSPublicConnection(c), "verified": true})
			return
		}
		rows, err := p.db.GetDB().QueryContext(r.Context(), `SELECT id,endpoint,label,status,nameservers_json,created_at FROM remote_dns_connections ORDER BY created_at,id`)
		if err != nil {
			writeServerError(w, err)
			return
		}
		defer rows.Close()
		list := []remoteDNSConnection{}
		for rows.Next() {
			var c remoteDNSConnection
			var names string
			if err = rows.Scan(&c.ID, &c.Endpoint, &c.Label, &c.Status, &names, &c.CreatedAt); err != nil {
				writeServerError(w, err)
				return
			}
			if json.Unmarshal([]byte(names), &c.Nameservers) != nil {
				writeClientError(w, 500, "invalid saved nameservers")
				return
			}
			if c.Nameservers == nil {
				c.Nameservers = []string{}
			}
			list = append(list, c)
		}
		if err = rows.Err(); err != nil {
			writeServerError(w, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"connections": list})
	case http.MethodPost:
		var request struct {
			ID             string `json:"id"`
			Endpoint       string `json:"endpoint"`
			EnrollmentCode string `json:"enrollment_code"`
			Label          string `json:"label"`
		}
		if !decodeRemoteDNSBody(w, r, &request) {
			return
		}
		id := request.ID
		if id == "" {
			endpoint, err := canonicalRemoteDNSEndpoint(request.Endpoint)
			if err != nil || !remoteDNSValidSecret(request.EnrollmentCode) {
				writeClientError(w, 400, "invalid HTTPS DNS endpoint or enrollment code")
				return
			}
			label := strings.TrimSpace(request.Label)
			if label == "" {
				label = "CelikPanel publishing host"
			}
			if !remoteDNSValidLabel(label) {
				writeClientError(w, 400, "invalid publishing host label")
				return
			}
			id, err = p.prepareRemoteDNSConnection(r.Context(), endpoint, request.EnrollmentCode, label)
			if err != nil {
				writeServerError(w, err)
				return
			}
		} else if request.Endpoint != "" || request.EnrollmentCode != "" || request.Label != "" {
			writeClientError(w, 400, "resume only the persisted DNS connection identity")
			return
		}
		c, err := p.resumeRemoteDNSConnection(r.Context(), id)
		if err != nil {
			writeCodedError(w, 409, "REMOTE_DNS_CONNECTION_PENDING", "The saved DNS connection could not be verified; resume this connection after checking the receiving DNS server", "")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"connection": remoteDNSPublicConnection(c), "verified": true})
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		c, err := p.readRemoteDNSConnection(r.Context(), id)
		if err != nil {
			writeClientError(w, 404, "DNS connection not found")
			return
		}
		if c.Status != "revoked" {
			if c.Status == "pending" {
				var receipt struct {
					ClientID string `json:"client_id"`
					Revoked  bool   `json:"revoked"`
				}
				err = remoteDNSExchange(r.Context(), c.Endpoint, "/api/v1/dns/remote/accept", c.enrollmentCode, remoteDNSAcceptRequest{ClientID: c.ID, Credential: c.credential, Label: c.Label, Cancel: true}, &receipt)
				if err != nil || !receipt.Revoked || receipt.ClientID != id {
					writeCodedError(w, 409, "REMOTE_DNS_REVOCATION_PENDING", "The receiving DNS server has not confirmed cancellation; retry this connection", "")
					return
				}
				if _, err = p.db.GetDB().ExecContext(r.Context(), `UPDATE remote_dns_connections SET status='revoked',credential='',enrollment_code='' WHERE id=? AND credential=?`, id, c.credential); err != nil {
					writeServerError(w, err)
					return
				}
				_ = json.NewEncoder(w).Encode(map[string]bool{"revoked": true})
				return
			}
			var receipt struct {
				ClientID string `json:"client_id"`
				Revoked  bool   `json:"revoked"`
			}
			err = remoteDNSExchange(r.Context(), c.Endpoint, "/api/v1/dns/remote/receiver/status", c.ID+"."+c.credential, map[string]bool{"revoke": true}, &receipt)
			if err != nil || !receipt.Revoked || receipt.ClientID != id {
				writeCodedError(w, 409, "REMOTE_DNS_REVOCATION_PENDING", "The receiving DNS server has not confirmed revocation; retry this connection", "")
				return
			}
			_, err = p.db.GetDB().ExecContext(r.Context(), `UPDATE remote_dns_connections SET status='revoked',credential='',enrollment_code='' WHERE id=?`, id)
			if err != nil {
				writeServerError(w, err)
				return
			}
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"revoked": true})
	default:
		rejectRouteMethod(w, []string{http.MethodGet, http.MethodPost, http.MethodDelete})
	}
}

func (p *Panel) prepareRemoteDNSConnection(ctx context.Context, endpoint, code, label string) (string, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	var existing string
	err = tx.QueryRowContext(ctx, `SELECT id FROM remote_dns_connections WHERE endpoint=? AND enrollment_hash=?`, endpoint, remoteDNSHash(code)).Scan(&existing)
	if err == nil {
		return existing, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return "", err
	}
	id, err := remoteDNSSecret()
	if err != nil {
		return "", err
	}
	id = id[:32]
	credential, err := remoteDNSSecret()
	if err != nil {
		return "", err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO remote_dns_connections(id,endpoint,credential,enrollment_code,enrollment_hash,label,status,created_at) VALUES(?,?,?,?,?,?,'pending',?)`, id, endpoint, credential, code, remoteDNSHash(code), label, time.Now().UTC().Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	return id, tx.Commit()
}

func (p *Panel) resumeRemoteDNSConnection(ctx context.Context, id string) (remoteDNSConnection, error) {
	c, err := p.readRemoteDNSConnection(ctx, id)
	if err != nil {
		return c, err
	}
	if c.Status == "ready" {
		proof, err := p.remoteDNSConnectionReadiness(ctx, id)
		if err == nil {
			c.Nameservers = proof.Nameservers
		}
		return c, err
	}
	if c.Status != "pending" {
		return c, errRemoteDNSNotReady
	}
	var proof remoteDNSAuthority
	err = remoteDNSExchange(ctx, c.Endpoint, "/api/v1/dns/remote/accept", c.enrollmentCode, remoteDNSAcceptRequest{ClientID: id, Credential: c.credential, Label: c.Label}, &proof)
	if err != nil {
		return c, err
	}
	if err = validateRemoteDNSAuthority(proof, id); err != nil {
		return c, err
	}
	names, _ := json.Marshal(proof.Nameservers)
	result, err := p.db.GetDB().ExecContext(ctx, `UPDATE remote_dns_connections SET status='ready',enrollment_code='',nameservers_json=? WHERE id=? AND status='pending' AND credential=?`, string(names), id, c.credential)
	if err != nil {
		return c, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return c, err
	}
	if count == 0 {
		current, err := p.readRemoteDNSConnection(ctx, id)
		if err != nil || current.Status != "ready" {
			return current, errRemoteDNSNotReady
		}
		return current, nil
	}
	c.Status = "ready"
	c.enrollmentCode = ""
	c.Nameservers = proof.Nameservers
	return c, nil
}
