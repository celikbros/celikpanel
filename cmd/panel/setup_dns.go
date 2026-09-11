package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	setupDNSModeLocal         = "local"
	setupDNSModeExternal      = "external"
	setupDNSModeExisting      = "existing"
	settingSetupDNSMode       = "dns_management_mode"
	errCodeExternalDNSManaged = "EXTERNAL_DNS_MANAGED"
)

func setupDNSModeSupported(mode string) bool {
	return mode == setupDNSModeLocal || mode == setupDNSModeExternal || mode == setupDNSModeExisting
}

// No saved default means a legacy installation. Its original local authority
// remains binding; an inferred public nameserver is never a mode selection.
func (p *Panel) setupDNSManagementMode(ctx context.Context) (string, error) {
	var mode string
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT value FROM panel_settings WHERE key = ?`, settingSetupDNSMode).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return setupDNSModeLocal, nil
	}
	if err != nil {
		return "", fmt.Errorf("read DNS management mode: %w", err)
	}
	if !setupDNSModeSupported(mode) {
		return "", errors.New("saved DNS management mode is unsupported")
	}
	return mode, nil
}

// Called only from a user-started reviewed setup operation. This changes a
// default for future domains; the immutable per-domain ledger is unaffected.
func (p *Panel) saveSetupDNSManagementMode(ctx context.Context, mode string) error {
	if mode == setupDNSModeExisting {
		id, err := p.defaultRemoteDNSConnectionID(ctx)
		if err != nil {
			return err
		}
		return p.saveSetupRemoteDNSConnection(ctx, id)
	}
	if !setupDNSModeSupported(mode) {
		return errors.New("DNS management mode is not implemented")
	}
	_, err := p.db.GetDB().ExecContext(ctx, `INSERT INTO panel_settings (key,value)
		VALUES (?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`, settingSetupDNSMode, mode)
	return err
}

// External infrastructure needs no local publisher. Public per-domain records
// are verified independently before the corresponding site/mail is ready.
func (p *Panel) setupDNSModeReadiness(ctx context.Context, mode string) (bool, error) {
	if mode == setupDNSModeExternal {
		return true, nil
	}
	if mode == setupDNSModeExisting {
		id, err := p.defaultRemoteDNSConnectionID(ctx)
		if err != nil {
			return false, err
		}
		proof, err := p.remoteDNSConnectionReadiness(ctx, id)
		return proof.Ready && err == nil, err
	}
	if mode != setupDNSModeLocal {
		return false, errors.New("DNS management mode is unsupported")
	}
	state, err := readDNSEngineDBState(ctx, p.db.GetDB())
	if err != nil {
		return false, err
	}
	if state.ActiveEngine == "" || state.CurrentSwitchID != "" || state.Topology != transport.DNSTopologyPaired ||
		state.PeerIP == "" || state.PeerIP == state.LocalIP {
		return false, nil
	}
	runtimes, conflict, hold, err := p.readDNSBackendRuntime(ctx)
	if err != nil {
		return false, err
	}
	runtime := runtimes[state.ActiveEngine]
	if conflict || hold != "" || !runtime.Installed || !runtime.Running || !runtime.Managed {
		return false, nil
	}
	if (state.PairRole != transport.DNSPairRolePrimary && state.PairRole != transport.DNSPairRoleSecondary) ||
		(state.PairRole == transport.DNSPairRolePrimary && !runtime.PairReady) ||
		(state.PairRole == transport.DNSPairRoleSecondary && !runtime.SecondaryReady) {
		return false, nil
	}
	return p.savedDNSIdentityConfiguredStrict(ctx)
}

func (p *Panel) domainDNSManagementMode(ctx context.Context, name string) (string, error) {
	var mode string
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT dns_management FROM domains WHERE name = ?`, name).Scan(&mode)
	if errors.Is(err, sql.ErrNoRows) {
		return setupDNSModeLocal, nil
	} // Infrastructure zones have no tenant domain.
	if err != nil {
		return "", err
	}
	if !setupDNSModeSupported(mode) {
		return "", errors.New("domain DNS ownership is invalid")
	}
	return mode, nil
}

func writeExternalDNSManaged(w http.ResponseWriter) {
	writeCodedError(w, http.StatusConflict, errCodeExternalDNSManaged,
		"DNS records for this domain are managed at your external DNS provider; copy the required records there", "")
}

func (p *Panel) requireLocalDomainDNS(w http.ResponseWriter, ctx context.Context, name string) bool {
	mode, err := p.domainDNSManagementMode(ctx, name)
	if err != nil {
		writeServerError(w, err)
		return false
	}
	if mode == setupDNSModeExisting {
		writeCodedError(w, http.StatusConflict, "REMOTE_DNS_MANAGED", "This operation belongs to the authorized receiving DNS server", "")
		return false
	}
	if mode != setupDNSModeLocal {
		writeExternalDNSManaged(w)
		return false
	}
	return true
}

// External records are instructions, not a shadow zone and not proof of public
// publication. No SOA or NS is invented for a provider the panel does not own.
func externalDomainDNSRecords(name, ipv4, ipv6 string) []DNSRecord {
	records := []DNSRecord{}
	for _, address := range []struct{ kind, value string }{{"A", ipv4}, {"AAAA", ipv6}} {
		if address.value != "" {
			records = append(records, DNSRecord{Name: name, Type: address.kind, Content: address.value, TTL: 3600})
		}
	}
	if !strings.HasPrefix(name, "www.") {
		records = append(records, DNSRecord{Name: "www." + name, Type: "CNAME", Content: name, TTL: 3600})
	}
	return records
}

func (p *Panel) handleExternalDomainDNS(w http.ResponseWriter, r *http.Request, domain string) {
	w.Header().Set("X-CelikPanel-DNS-Management", setupDNSModeExternal)
	if r.Method != http.MethodGet {
		writeExternalDNSManaged(w)
		return
	}
	switch {
	case strings.HasSuffix(r.URL.Path, "/records"):
		_ = json.NewEncoder(w).Encode(map[string]any{"records": externalDomainDNSRecords(domain, serverPrimaryIP(), serverPrimaryIPv6()), "management": setupDNSModeExternal, "published": false})
	case strings.HasSuffix(r.URL.Path, "/zone"):
		_ = json.NewEncoder(w).Encode(map[string]any{"name": domain, "type": "EXTERNAL", "management": setupDNSModeExternal, "managed": false})
	default:
		http.NotFound(w, r)
	}
}
