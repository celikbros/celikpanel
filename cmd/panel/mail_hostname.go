package main

import (
	"context"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
)

// Mail identity is configured independently of the operating-system hostname.
// Existing host, panel and DNS names are suggestions; installation never renames the OS.

const settingMailHostname = "mail_hostname"

const (
	mailHostnameSourceNone        = ""
	mailHostnameSourceSaved       = "saved"
	mailHostnameSourceOS          = "os_hostname"
	mailHostnameSourceCertificate = "panel_certificate"
	mailHostnameSourceNameserver  = "dns_identity"
)

// MailHostnameIdentity is what the mail install screen needs to decide whether
// to ask. It is admin-only, like every managed-services payload.
// MailHostnameIdentity, posta kurulum ekranının sormaya gerek olup olmadığına
// karar vermek için ihtiyaç duyduğu şeydir. Her yönetilen-servis yükü gibi
// yalnız yöneticiye açıktır.
type MailHostnameIdentity struct {
	// Current is this server's operating-system hostname exactly as it is now.
	Current string `json:"current,omitempty"`
	// CurrentUsable is true when Current is already a fully qualified name.
	CurrentUsable bool `json:"current_usable"`
	// Hostname is the name the install would use. Empty means the panel holds
	// no identity to derive one from and the operator must supply it.
	Hostname string `json:"hostname,omitempty"`
	// Source names where Hostname came from, so the screen can say it.
	Source string `json:"source,omitempty"`
	// WillSetHostname is retained for wire compatibility and is always false.
	WillSetHostname bool `json:"will_set_hostname"`
}

// mailHostnameIdentity prefers the saved mail identity. Other existing names
// are suggestions when no mail identity has been saved, never rename requests.
func (p *Panel) mailHostnameIdentity(ctx context.Context) MailHostnameIdentity {
	identity := MailHostnameIdentity{}
	rawHostname, err := readMailProfileHostname()
	if err == nil {
		identity.Current = strings.TrimSpace(rawHostname)
	}
	canonicalOS := ""
	if identity.Current != "" {
		if canonical, err := hostname.CanonicalFQDN(identity.Current); err == nil {
			canonicalOS = canonical
			identity.CurrentUsable = true
		}
	}

	for _, candidate := range []struct {
		value  string
		source string
	}{
		{p.setting(ctx, settingMailHostname), mailHostnameSourceSaved},
		{canonicalOS, mailHostnameSourceOS},
		{panelCertificateHostname(), mailHostnameSourceCertificate},
		{p.dnsIdentityHostname(ctx), mailHostnameSourceNameserver},
	} {
		canonical, err := hostname.CanonicalFQDN(strings.TrimSpace(candidate.value))
		if err != nil {
			continue
		}
		identity.Hostname = canonical
		identity.Source = candidate.source
		break
	}
	return identity
}

// panelCertificateHostname reads the name this panel is reached at from its
// own certificate. A subject alternative name is preferred over the common
// name because that is the name browsers actually verify.
// panelCertificateHostname, bu panele erişilen adı kendi sertifikasından
// okur. Konu alternatif adı, ortak ada yeğlenir; çünkü tarayıcıların gerçekten
// doğruladığı ad odur.
func panelCertificateHostname() string {
	info := currentPanelCert()
	for _, name := range info.DNSNames {
		if canonical, err := hostname.CanonicalFQDN(strings.TrimSpace(name)); err == nil {
			return canonical
		}
	}
	if canonical, err := hostname.CanonicalFQDN(strings.TrimSpace(info.Subject)); err == nil {
		return canonical
	}
	return ""
}

// dnsIdentityHostname returns this server's own nameserver name. In a paired
// topology the peer's name is excluded, so the name returned is always a name
// this very server answers to.
// dnsIdentityHostname, bu sunucunun kendi ad sunucusu adını döndürür. Eşli bir
// topolojide eşin adı dışlanır; böylece döndürülen ad her zaman tam olarak bu
// sunucunun yanıt verdiği bir addır.
func (p *Panel) dnsIdentityHostname(ctx context.Context) string {
	ns1 := canonicalDNSName(p.setting(ctx, settingNS1))
	ns2 := canonicalDNSName(p.setting(ctx, settingNS2))
	peerNS := canonicalDNSName(p.setting(ctx, settingDNSPeerNS))
	if normalizeDNSRole(strings.TrimSpace(p.setting(ctx, settingDNSRole))) != "paired" {
		peerNS = ""
	}
	for _, candidate := range []string{ns1, ns2} {
		if candidate == "" || (peerNS != "" && candidate == peerNS) {
			continue
		}
		return candidate
	}
	return ""
}
