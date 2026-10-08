package transport

type DKIMStatusRequest struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
}

type DKIMStatusResponse struct {
	HasKey           bool   `json:"has_key"`
	PublicKeyB64     string `json:"public_key_b64"`
	SigningInstalled bool   `json:"signing_installed"`
	Error            string `json:"error,omitempty"`
}

type DKIMEnsureRequest struct {
	Domain   string `json:"domain"`
	Selector string `json:"selector"`
}

type DKIMEnsureResponse struct {
	Created      bool   `json:"created"`
	PublicKeyB64 string `json:"public_key_b64"`
	Error        string `json:"error,omitempty"`
}

type MailPolicy struct {
	MessageSizeMB     int      `json:"message_size_mb"`
	DNSBLZones        []string `json:"dnsbl_zones"`
	OutboundRateLimit int      `json:"outbound_rate_limit"`
	// Version identifies the exact Postfix values this policy was read from.
	// A read returns it and a write carries it back (8 Oct 2026); a write
	// without it, or with one that no longer matches, changes nothing.
	// Version, bu politikanın okunduğu Postfix değerlerini tanımlar. Okuma onu
	// döndürür, yazma geri taşır; yoksa ya da eşleşmiyorsa hiçbir şey değişmez.
	Version string `json:"version,omitempty"`
	// DNSBLLocked is set on a read when the Panel will not rewrite the
	// recipient restrictions found on the server (a MailPolicyLock* value).
	// The DNSBL setting is then read-only; size and rate stay editable.
	// DNSBLLocked, Panel sunucudaki alıcı kısıtlarını yeniden yazmayacaksa
	// okumada dolar. DNSBL ayarı salt okunur olur; boyut ve hız düzenlenebilir.
	DNSBLLocked string `json:"dnsbl_locked,omitempty"`
}

// Mail policy refusal codes, carried in MailPolicyResponse.Code. The Panel
// turns each into its typed HTTP answer. Error stays a fixed sentence and
// never carries postconf output.
// Posta politikası ret kodları; MailPolicyResponse.Code içinde taşınır. Error
// sabit bir cümledir ve postconf çıktısı taşımaz.
const (
	// A Postfix value could not be read or understood: the state is unknown,
	// so nothing is offered as a setting and nothing is written.
	MailPolicyUnreadable = "mail_policy_unreadable"
	// The write carried no version, or one that no longer matches the server.
	MailPolicyVersionRequired = "mail_policy_version_required"
	MailPolicyChanged         = "mail_policy_changed"
	// A requested value is outside what the Panel writes; Reason names it.
	MailPolicyInvalid = "mail_policy_invalid"
	// The DNSBL change needs a rewrite of the recipient restrictions that the
	// Panel will not make; Reason is a MailPolicyLock* value.
	MailPolicyRestrictionsUnmanaged = "mail_policy_restrictions_unmanaged"
	// postconf refused the write; this request left main.cf as it was.
	MailPolicyWriteFailed = "mail_policy_write_failed"
	// The values were written to main.cf, but Postfix could not be reloaded and
	// still runs with the previous ones. Policy is what main.cf holds now, with
	// its version; Reason is the first line the reload said, bounded.
	// Değerler main.cf'e yazıldı ancak Postfix yeniden yüklenemedi ve önceki
	// değerlerle çalışıyor. Policy, main.cf'in şimdi tuttuğudur.
	MailPolicyNotReloaded = "mail_policy_not_reloaded"
)

// Why the Panel will not rewrite smtpd_recipient_restrictions.
// Panel'in smtpd_recipient_restrictions değerini neden yeniden yazmadığı.
const (
	// The value refers to another parameter ($name); what that expands to is
	// not known from this value.
	MailPolicyLockVariable = "variable"
	// Unbalanced braces, or reject_rbl_client without a zone.
	MailPolicyLockMalformed = "malformed"
	// A list written by hand without both permit_mynetworks and
	// permit_sasl_authenticated: a DNSBL check could reject the owner's own
	// users, and the Panel does not add entries to a list it did not write.
	MailPolicyLockNoBaseline = "no_baseline"
	// The list ends with permit, reject or defer, so a DNSBL check added after
	// it would never run; its place is the owner's decision.
	MailPolicyLockTerminal = "terminal"
)

// Which requested value MailPolicyInvalid refused.
// MailPolicyInvalid'in reddettiği değer.
const (
	MailPolicyInvalidSize = "message_size_mb"
	MailPolicyInvalidZone = "dnsbl_zone"
	MailPolicyInvalidRate = "outbound_rate_limit"
)

// PostfixQueueUnreadable is the Agent's exact answer when the mail queue could
// not be read. The queue is then unknown; it is not an empty queue.
// PostfixQueueUnreadable, posta kuyruğu okunamadığında Agent'ın tam yanıtıdır.
const PostfixQueueUnreadable = "the mail queue could not be read; what it holds is unknown"

type MailPolicyResponse struct {
	Policy MailPolicy `json:"policy"`
	Error  string     `json:"error,omitempty"`
	// Code and Reason classify Error. Additive; empty from older Agents.
	// Code ve Reason, Error'ı sınıflandırır. Eklemelidir.
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type MailHealthResponse struct {
	ServerIP       string `json:"server_ip"`
	Myhostname     string `json:"myhostname"`
	HostnameFQDN   bool   `json:"hostname_fqdn"`
	PTR            string `json:"ptr"`
	FCrDNS         bool   `json:"fcrdns"`
	PTRAligned     bool   `json:"ptr_aligned"`
	TLSEnabled     bool   `json:"tls_enabled"`
	OutboundPort25 string `json:"outbound_port_25"`
	Error          string `json:"error,omitempty"`
}

type RBLResult struct {
	Zone   string `json:"zone"`
	Listed bool   `json:"listed"`
	Detail string `json:"detail,omitempty"`
}

type CheckRBLResponse struct {
	IP      string      `json:"ip"`
	Results []RBLResult `json:"results"`
	Error   string      `json:"error,omitempty"`
}
