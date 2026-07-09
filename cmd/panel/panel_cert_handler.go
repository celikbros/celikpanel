package main

import (
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// The panel's own certificate, panel side. GET reports what the panel is
// serving right now (self-signed or real, expiry); POST asks the agent to
// issue a Let's Encrypt certificate for the given domain and install it where
// tlsSettings() loads from. The new certificate only becomes active on
// restart, so after replying the panel schedules its own restart via the
// agent — the browser shows "restarting" and reloads.
//
// Panelin kendi sertifikası, panel tarafı. GET panelin şu an ne sunduğunu
// bildirir (kendinden imzalı mı gerçek mi, bitiş); POST agent'tan verilen
// alan adı için Let's Encrypt sertifikası alıp tlsSettings()'in yüklediği
// yere kurmasını ister. Yeni sertifika ancak yeniden başlatmada etkinleşir;
// bu yüzden panel cevap verdikten sonra agent üzerinden kendi yeniden
// başlatmasını zamanlar — tarayıcı "yeniden başlıyor" gösterir ve yenilenir.

type panelCertInfo struct {
	HTTPSEnabled bool      `json:"https_enabled"`
	SelfSigned   bool      `json:"self_signed"`
	Subject      string    `json:"subject,omitempty"`
	Issuer       string    `json:"issuer,omitempty"`
	DNSNames     []string  `json:"dns_names,omitempty"`
	ExpiresAt    time.Time `json:"expires_at,omitempty"`
}

func (p *Panel) handlePanelCertificate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		json.NewEncoder(w).Encode(currentPanelCert())

	case http.MethodPost:
		var req struct {
			Domain string `json:"domain"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Domain == "" {
			writeClientError(w, http.StatusBadRequest, "domain is required")
			return
		}

		// Let's Encrypt wants a contact email; the admin's account email is
		// the honest default.
		// Let's Encrypt bir iletişim e-postası ister; yöneticinin hesap
		// e-postası dürüst varsayılandır.
		email := ""
		_ = p.db.GetDB().QueryRowContext(r.Context(),
			`SELECT email FROM users WHERE id = ?`, currentCaller(r).ID).Scan(&email)

		var resp struct {
			Issued    bool      `json:"issued"`
			ExpiresAt time.Time `json:"expires_at"`
			Detail    string    `json:"detail,omitempty"`
			Error     string    `json:"error,omitempty"`
		}
		err := p.agentClient.Call("Agent.IssuePanelCertificate", &struct {
			Domain string `json:"domain"`
			Email  string `json:"email"`
			TLSDir string `json:"tls_dir"`
		}{Domain: req.Domain, Email: email, TLSDir: tlsDir()}, &resp)
		if err != nil {
			writeAgentError(w, err, "panel certificate")
			return
		}
		if resp.Error != "" {
			writeClientError(w, http.StatusConflict, resp.Error)
			return
		}

		p.audit(r, "panel.certificate:"+req.Domain, "panel", 0)
		json.NewEncoder(w).Encode(map[string]any{
			"issued": true, "expires_at": resp.ExpiresAt, "detail": resp.Detail, "restarting": true,
		})

		// Activate after the response has left: the agent schedules a detached
		// restart a moment from now (systemd-run), so this reply survives.
		// Cevap gittikten sonra etkinleştir: agent ayrık bir yeniden başlatmayı
		// birazdan olacak şekilde zamanlar (systemd-run); bu cevap sağ kalır.
		go func() {
			time.Sleep(500 * time.Millisecond)
			var ok bool
			_ = p.agentClient.Call("Agent.RestartPanelSoon", &struct{}{}, &ok)
		}()

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// currentPanelCert parses the certificate the panel serves (tls dir pair).
// currentPanelCert, panelin sunduğu sertifikayı (tls dizini çifti) çözümler.
func currentPanelCert() panelCertInfo {
	info := panelCertInfo{}
	data, err := os.ReadFile(filepath.Join(tlsDir(), "panel.crt"))
	if err != nil {
		// Explicit cert pair via env, or HTTP-only dev mode — report honestly.
		// Env ile açık sertifika çifti ya da yalnız-HTTP dev modu — dürüst bildir.
		if c := os.Getenv("CELIKPANEL_TLS_CERT"); c != "" {
			data, err = os.ReadFile(c)
		}
		if err != nil {
			return info
		}
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return info
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return info
	}
	info.HTTPSEnabled = true
	info.SelfSigned = cert.Issuer.String() == cert.Subject.String()
	info.Subject = cert.Subject.CommonName
	info.Issuer = cert.Issuer.CommonName
	info.DNSNames = cert.DNSNames
	info.ExpiresAt = cert.NotAfter
	return info
}
