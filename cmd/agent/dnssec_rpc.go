package main

import (
	"crypto/sha256"
	"crypto/x509"
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
	if _, err := exec.LookPath("pdnsutil"); err != nil {
		resp.Error = "pdnsutil is not installed"
		return nil
	}
	if !zoneSecured(zone) {
		if out, err := exec.Command("pdnsutil", "secure-zone", zone).CombinedOutput(); err != nil {
			resp.Error = "secure-zone: " + firstLine(string(out))
			return nil
		}
	}
	_ = exec.Command("pdnsutil", "rectify-zone", zone).Run()
	resp.Secured = true
	resp.DS = zoneDSRecords(zone)
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
	if _, err := exec.LookPath("pdnsutil"); err != nil {
		return nil
	}
	resp.Secured = zoneSecured(zone)
	if resp.Secured {
		resp.DS = zoneDSRecords(zone)
	}
	return nil
}

func zoneSecured(zone string) bool {
	out, err := exec.Command("pdnsutil", "show-zone", zone).Output()
	if err != nil {
		return false
	}
	// pdnsutil 4.8 prints one line per key: "ID = 1 (CSK), … tag = 36586, …"
	// and a "DS = " line per digest. Either proves the zone is signed.
	// pdnsutil 4.8 anahtar başına bir satır basar; "DS = " satırı da özet
	// başına gelir. İkisi de bölgenin imzalı olduğunu kanıtlar.
	s := string(out)
	return strings.Contains(s, "DS = ") || strings.Contains(s, "tag = ")
}

// zoneDSRecords extracts the DS lines from pdnsutil show-zone. They look
// like: "DS = example.com. IN DS 12345 13 2 ABCD…" — the registrar needs the
// part after "IN DS".
// zoneDSRecords, pdnsutil show-zone çıktısından DS satırlarını çıkarır.
func zoneDSRecords(zone string) []string {
	out, err := exec.Command("pdnsutil", "show-zone", zone).Output()
	if err != nil {
		return nil
	}
	var ds []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if idx := strings.Index(line, "IN DS "); strings.HasPrefix(line, "DS") && idx >= 0 {
			ds = append(ds, strings.TrimSpace(line[idx+len("IN DS "):]))
		}
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
