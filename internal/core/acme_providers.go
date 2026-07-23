package core

// ACME certificate authorities the panel can issue from (operator request,
// 23 Jul: "a few kinds of SSL, not only Let's Encrypt"). Let's Encrypt needs
// nothing — a bare `--server`, one dropdown. The others need EAB (external
// account binding: a key id + HMAC the operator gets from their CA account),
// so their rows carry NeedsEAB and the UI asks for those two fields. Every
// directory URL here was verified reachable on a live server before shipping
// — Buypass Go SSL was in this list until its ACME service shut down on
// 15 Apr 2026 (verified 24 Jul: its directory 404s), and a dead CA in a
// dropdown is exactly the "installs but does not work" trap.
//
// Panelin sertifika alabileceği ACME sertifika otoriteleri (operatör isteği,
// 23 Tem: "birkaç çeşit SSL, sadece Let's Encrypt olmasın"). Let's Encrypt
// hiçbir şey istemez — çıplak `--server`, tek menü. Diğerleri EAB ister (dış
// hesap bağlama: operatörün CA hesabından aldığı anahtar kimliği + HMAC), bu
// yüzden satırları NeedsEAB taşır ve UI bu iki alanı sorar. Buradaki her
// dizin URL'si gönderilmeden önce canlı sunucuda erişilebilir doğrulandı —
// Buypass Go SSL, ACME hizmeti 15 Nis 2026'da kapanana dek bu listedeydi
// (24 Tem doğrulandı: dizini 404); menüde ölü bir CA, tam olarak "kurulur
// ama çalışmaz" tuzağıdır.
type ACMEProvider struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Directory is the ACME directory URL passed to certbot's --server.
	// Empty for the default (Let's Encrypt), so an old agent that predates
	// the --server flag still issues from LE with no change.
	// Directory, certbot'un --server'ına verilen ACME dizin URL'sidir.
	// Varsayılan (Let's Encrypt) için boştur; böylece --server bayrağından
	// önceki eski bir agent hiçbir değişiklik olmadan LE'den sertifika alır.
	Directory string `json:"directory"`
	// NeedsEAB: this CA rejects issuance without an external account binding.
	// The UI reveals key-id + HMAC fields, and the panel refuses to call the
	// agent without them — better a clear "enter your credentials" than a
	// cryptic certbot error.
	// NeedsEAB: bu CA, dış hesap bağlaması olmadan vermeyi reddeder. UI
	// anahtar-kimliği + HMAC alanlarını açar ve panel onlar olmadan agent'ı
	// çağırmayı reddeder — anlaşılmaz bir certbot hatasından çok, net bir
	// "bilgilerini gir" iyidir.
	NeedsEAB bool `json:"needs_eab"`
	// Note: one honest line about who they are / trade-offs, shown in the UI.
	// Note: kim oldukları / ödünleşimler hakkında UI'da gösterilen tek dürüst satır.
	Note string `json:"note"`
}

// ACMEProviders — Let's Encrypt first (the default, zero-config). The others
// are real alternatives but require a free account with the CA to get EAB
// credentials; that is an honest trade, not a hidden failure.
// ACMEProviders — Let's Encrypt başta (varsayılan, sıfır-ayar). Diğerleri
// gerçek alternatiflerdir ama EAB bilgisi için CA'da ücretsiz bir hesap
// gerektirir; bu gizli bir başarısızlık değil, dürüst bir ödünleşimdir.
var ACMEProviders = []ACMEProvider{
	{
		ID:        "letsencrypt",
		Name:      "Let's Encrypt",
		Directory: "", // certbot varsayılanı
		Note:      "Free, 90-day certificates. The default — no account needed.",
	},
	{
		ID:        "zerossl",
		Name:      "ZeroSSL",
		Directory: "https://acme.zerossl.com/v2/DV90",
		NeedsEAB:  true,
		Note:      "Free 90-day certificates. Needs EAB credentials from your ZeroSSL account.",
	},
	{
		ID:        "google",
		Name:      "Google Trust Services",
		Directory: "https://dv.acme-v02.api.pki.goog/directory",
		NeedsEAB:  true,
		Note:      "Free 90-day certificates. Needs EAB credentials from Google Cloud.",
	},
}

// ACMEProviderByID returns the provider or nil. An unknown id must never
// silently fall back to a different CA than the caller chose.
// ACMEProviderByID sağlayıcıyı ya da nil döner. Bilinmeyen bir id, çağıranın
// seçtiğinden farklı bir CA'ya asla sessizce düşmemelidir.
func ACMEProviderByID(id string) *ACMEProvider {
	for i := range ACMEProviders {
		if ACMEProviders[i].ID == id {
			return &ACMEProviders[i]
		}
	}
	return nil
}
