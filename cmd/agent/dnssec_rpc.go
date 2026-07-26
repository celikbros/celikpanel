package main

import (
	"crypto/sha256"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// DNSSEC — zone signing through pdnsutil, which operates on the same
// dedicated sqlite backend PowerDNS serves from. Signing is one-way here:
// the panel signs and shows the DS records the operator enters at the
// registrar; without that DS the zone simply keeps working unsigned-style,
// so signing is safe to do eagerly. ComputeTLSA supports DANE: it hashes a
// certificate so the panel can publish TLSA records next to the mail
// endpoints Plesk-style.
//
// DNSSEC — PowerDNS'in sunduğu ayrılmış sqlite backend üzerinde çalışan
// pdnsutil ile zone imzalama. İmzalama burada tek yönlüdür: panel imzalar ve
// operatörün registrar'a gireceği DS kayıtlarını gösterir; o DS olmadan zone
// imzasızmış gibi çalışmaya devam eder, dolayısıyla erken imzalamak
// güvenlidir. ComputeTLSA, DANE'yi destekler: sertifikayı özetler ki panel
// Plesk usulü posta uçlarının yanında TLSA kayıtları yayımlayabilsin.

type DNSSECRequest struct {
	Zone string `json:"zone"`
}

type DNSSECStatusResponse struct {
	Secured bool     `json:"secured"`
	DS      []string `json:"ds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

var (
	dnssecLookPath = exec.LookPath
	dnssecCommand  = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
)

// SecureDNSZone signs a zone (idempotent) and rectifies it so NSEC ordering
// is correct. The DS records come back for the registrar.
// SecureDNSZone bir zone'u imzalar (idempotent) ve NSEC sıralaması doğru
// olsun diye düzeltir. DS kayıtları registrar için geri döner.
func (a *Agent) SecureDNSZone(req *DNSSECRequest, resp *DNSSECStatusResponse) error {
	zone := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Zone)), ".")
	if zone == "" || strings.ContainsAny(zone, " \t/;") {
		resp.Error = "invalid zone"
		return nil
	}
	kind, err := localDNSZoneKind(zone)
	if err != nil {
		resp.Error = fmt.Sprintf("cannot read local zone type: %v", err)
		return nil
	}
	if kind == "SLAVE" || kind == "SECONDARY" {
		resp.Error = "cannot sign a read-only secondary zone on this server; sign it on the DNS primary"
		return nil
	}
	if _, err := dnssecLookPath("pdnsutil"); err != nil {
		resp.Error = "pdnsutil is not installed"
		return nil
	}

	secured, _, out, err := zoneDNSSECState(zone)
	if err != nil {
		resp.Error = dnssecCommandError("show zone", out, err)
		return nil
	}
	if !secured {
		if out, err := runPDNSUtil(
			[]string{"zone", "secure", zone},
			[]string{"secure-zone", zone},
		); err != nil {
			resp.Error = dnssecCommandError("secure zone", out, err)
			return nil
		}
	}
	if out, err := runPDNSUtil(
		[]string{"zone", "rectify", zone},
		[]string{"rectify-zone", zone},
	); err != nil {
		resp.Error = dnssecCommandError("rectify zone", out, err)
		return nil
	}

	secured, ds, out, err := zoneDNSSECState(zone)
	if err != nil {
		resp.Error = dnssecCommandError("show signed zone", out, err)
		return nil
	}
	if !secured || len(ds) == 0 {
		resp.Error = "DNSSEC signing produced no DS records; nothing can be published at the registrar"
		return nil
	}
	// pdns caches packets and DNSSEC keys; without a purge a freshly signed
	// zone can keep answering unsigned for a while (seen live: RRSIG appeared
	// only after a restart). Purging the zone's cache entries makes the
	// signatures visible immediately; harmless if pdns_control is absent.
	// pdns paketleri ve DNSSEC anahtarlarını önbellekler; purge olmadan yeni
	// imzalanan zone bir süre imzasız cevap verebilir (canlı görüldü: RRSIG
	// ancak restart sonrası göründü). Zone önbelleğini boşaltmak imzaları
	// hemen görünür kılar; pdns_control yoksa zararsız.
	_, _ = dnssecCommand("pdns_control", "purge", zone+"$")
	resp.Secured = true
	resp.DS = ds
	return nil
}

// DNSSECStatus reports whether a zone is signed and its DS records.
// DNSSECStatus, bir zone'un imzalı olup olmadığını ve DS kayıtlarını bildirir.
func (a *Agent) DNSSECStatus(req *DNSSECRequest, resp *DNSSECStatusResponse) error {
	zone := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(req.Zone)), ".")
	if zone == "" {
		resp.Error = "invalid zone"
		return nil
	}
	if _, err := dnssecLookPath("pdnsutil"); err != nil {
		resp.Error = "pdnsutil is not installed"
		return nil
	}
	secured, ds, out, err := zoneDNSSECState(zone)
	if err != nil {
		resp.Error = dnssecCommandError("show zone", out, err)
		return nil
	}
	if secured && len(ds) == 0 {
		resp.Error = "DNSSEC keys exist but pdnsutil produced no DS records"
		return nil
	}
	resp.Secured = secured
	resp.DS = ds
	return nil
}

// localDNSZoneKind reads the zone's local PowerDNS role without modifying the
// database. A SLAVE/SECONDARY receives its data from another server and must
// never acquire an independent local key set.
func localDNSZoneKind(zone string) (string, error) {
	db, err := sql.Open("sqlite", "file:"+pdnsDBPath()+"?mode=ro&_busy_timeout=5000")
	if err != nil {
		return "", err
	}
	defer db.Close()

	var kind string
	if err := db.QueryRow(`SELECT type FROM domains WHERE name = ? COLLATE NOCASE`, zone).Scan(&kind); err != nil {
		return "", err
	}
	return strings.ToUpper(strings.TrimSpace(kind)), nil
}

// zoneDNSSECState uses PowerDNS 5's object/action syntax first and falls back
// to the pre-5.0 spelling only when the first failure is recognisably a command
// syntax mismatch.
func zoneDNSSECState(zone string) (bool, []string, []byte, error) {
	out, err := runPDNSUtil(
		[]string{"zone", "show", zone},
		[]string{"show-zone", zone},
	)
	if err != nil {
		return false, nil, out, err
	}

	s := string(out)
	secured := strings.Contains(s, "DS = ") || strings.Contains(s, "tag = ")
	return secured, parseZoneDSRecords(s), out, nil
}

// parseZoneDSRecords extracts the registrar-facing rdata from every DS line
// printed by pdnsutil zone show.
func parseZoneDSRecords(output string) []string {
	var ds []string
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "IN DS "); strings.HasPrefix(line, "DS") && idx >= 0 {
			ds = append(ds, strings.TrimSpace(line[idx+len("IN DS "):]))
		}
	}
	return ds
}

func runPDNSUtil(current, legacy []string) ([]byte, error) {
	out, err := dnssecCommand("pdnsutil", current...)
	if err == nil || len(legacy) == 0 || !pdnsutilSyntaxMismatch(out) {
		return out, err
	}
	return dnssecCommand("pdnsutil", legacy...)
}

func pdnsutilSyntaxMismatch(out []byte) bool {
	s := strings.ToLower(string(out))
	for _, marker := range []string{
		"unknown command",
		"unknown subcommand",
		"invalid command",
		"no such command",
		"usage:",
		"syntax:",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func dnssecCommandError(action string, out []byte, err error) string {
	detail := strings.TrimSpace(firstLine(string(out)))
	if detail == "" && err != nil {
		detail = err.Error()
	}
	if detail == "" {
		detail = "command failed"
	}
	return action + ": " + detail
}

func zoneSecured(zone string) bool {
	secured, _, _, err := zoneDNSSECState(zone)
	// pdnsutil 4.8 prints one line per key: "ID = 1 (CSK), … tag = 36586, …"
	// and a "DS = " line per digest. Either proves the zone is signed.
	// pdnsutil 4.8 anahtar başına bir satır basar; "DS = " satırı da özet
	// başına gelir. İkisi de bölgenin imzalı olduğunu kanıtlar.
	return err == nil && secured
}

// zoneDSRecords extracts the DS lines from pdnsutil show-zone. They look
// like: "DS = example.com. IN DS 12345 13 2 ABCD…" — the registrar needs the
// part after "IN DS".
// zoneDSRecords, pdnsutil show-zone çıktısından DS satırlarını çıkarır.
func zoneDSRecords(zone string) []string {
	_, ds, _, err := zoneDNSSECState(zone)
	if err != nil {
		return nil
	}
	return ds
}

type TLSARequest struct {
	CertPath string `json:"cert_path"`
}

type TLSAResponse struct {
	// Content is the TLSA rdata "3 0 1 <sha256hex>": DANE-EE, full
	// certificate, SHA-256 — the same selector Plesk publishes.
	// Content, TLSA rdata'sıdır "3 0 1 <sha256hex>": DANE-EE, tam
	// sertifika, SHA-256 — Plesk'in yayımladığı seçicinin aynısı.
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}

// ComputeTLSA hashes the leaf certificate at CertPath for a DANE TLSA record.
// ComputeTLSA, DANE TLSA kaydı için CertPath'teki uç sertifikayı özetler.
func (a *Agent) ComputeTLSA(req *TLSARequest, resp *TLSAResponse) error {
	raw, err := os.ReadFile(req.CertPath)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	block, _ := pem.Decode(raw)
	if block == nil || block.Type != "CERTIFICATE" {
		resp.Error = "not a PEM certificate"
		return nil
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		resp.Error = fmt.Sprintf("invalid certificate: %v", err)
		return nil
	}
	sum := sha256.Sum256(block.Bytes)
	resp.Content = "3 0 1 " + strings.ToUpper(hex.EncodeToString(sum[:]))
	return nil
}
