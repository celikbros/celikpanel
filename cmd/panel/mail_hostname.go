package main

import (
	"context"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
)

// The mail stack answers as one fully qualified name. Until now that name was
// only ever read from the operating system, and nothing in the product could
// set it, so a server whose hostname was a bare machine name was told its
// hostname was invalid and given no field, no explanation and no action.
//
// The name now comes from the panel's own identity wherever the panel already
// holds one: the certificate this panel is reached at, or this server's own
// nameserver name from the saved DNS identity. When the panel holds none, the
// mail install screen asks the operator for it and the install gives the
// server that name as part of its work.
//
// Posta yığını tek bir tam nitelikli adla yanıt verir. Şimdiye dek bu ad
// yalnız işletim sisteminden okunuyordu ve üründe onu koyabilecek hiçbir şey
// yoktu; bu yüzden ana bilgisayar adı çıplak bir makine adı olan bir sunucuya
// adının geçersiz olduğu söyleniyor, ama ne bir alan, ne bir açıklama, ne de
// bir eylem veriliyordu.
//
// Ad artık, panelin zaten bir kimliği olduğu her yerde kendi kimliğinden
// gelir: bu panele erişilen sertifika ya da kayıtlı DNS kimliğindeki bu
// sunucunun kendi ad sunucusu adı. Panelin hiçbiri yoksa, posta kurulum ekranı
// operatöre sorar ve kurulum, işinin bir parçası olarak sunucuya o adı verir.

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
	// WillSetHostname is true when installing mail renames this server.
	WillSetHostname bool `json:"will_set_hostname"`
}

// mailHostnameIdentity resolves the mail hostname from everything the panel
// already knows, in the order of how deliberate each source is. A saved answer
// the operator gave wins; then the operating system, because a server that
// already carries a fully qualified name is not renamed by installing mail;
// then the panel's own certificate; then this server's nameserver name.
// mailHostnameIdentity, posta ana bilgisayar adını panelin zaten bildiği her
// şeyden, kaynakların ne kadar bilinçli olduğu sırasına göre çözer. Operatörün
// verdiği kayıtlı yanıt kazanır; sonra işletim sistemi, çünkü zaten tam
// nitelikli bir ad taşıyan bir sunucu posta kurulumuyla yeniden adlandırılmaz;
// sonra panelin kendi sertifikası; sonra bu sunucunun ad sunucusu adı.
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
	identity.WillSetHostname = identity.Hostname != "" && identity.Hostname != canonicalOS
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
