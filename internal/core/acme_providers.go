package core

// ACME certificate authorities the panel can issue from (operator request,
// 23 Jul: "a few kinds of SSL, not only Let's Encrypt"). Every entry here is
// free and needs NO external account binding — certbot reaches it with a bare
// `--server <directory>`, so choosing a CA is one dropdown, not an account
// setup. CAs that require EAB (ZeroSSL, Google) are deliberately absent until
// the UI can collect a key id + HMAC; offering them without those fields
// would be a button that always fails.
//
// Panelin sertifika alabileceği ACME sertifika otoriteleri (operatör isteği,
// 23 Tem: "birkaç çeşit SSL, sadece Let's Encrypt olmasın"). Buradaki her
// kalem ücretsizdir ve dış hesap bağlama (EAB) GEREKTİRMEZ — certbot'a çıplak
// bir `--server <dizin>` yeter, yani CA seçmek hesap kurmak değil tek bir
// açılır menüdür. EAB isteyen CA'lar (ZeroSSL, Google), UI bir anahtar kimliği
// + HMAC toplayana dek bilerek yoktur; onları o alanlar olmadan sunmak, her
// zaman başarısız olan bir düğme olurdu.
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
	// Note: one honest line about who they are / trade-offs, shown in the UI.
	// Note: kim oldukları / ödünleşimler hakkında UI'da gösterilen tek dürüst satır.
	Note string `json:"note"`
}

// ACMEProviders — Let's Encrypt first (the default). Buypass is the EAB-free
// alternative that answers the operator's ask; it issues 180-day certs (vs
// LE's 90) and is a Norwegian CA independent of the US-based LE.
// ACMEProviders — Let's Encrypt başta (varsayılan). Buypass, operatörün
// isteğine cevap veren EAB-gerektirmeyen alternatif; 180 günlük sertifika
// verir (LE'nin 90'ına karşı) ve ABD merkezli LE'den bağımsız bir Norveç
// CA'sıdır.
var ACMEProviders = []ACMEProvider{
	{
		ID:        "letsencrypt",
		Name:      "Let's Encrypt",
		Directory: "", // certbot varsayılanı
		Note:      "Free, 90-day certificates. The default, most widely trusted CA.",
	},
	{
		ID:        "buypass",
		Name:      "Buypass Go SSL",
		Directory: "https://api.buypass.com/acme/directory",
		Note:      "Free, 180-day certificates from an independent European CA.",
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
