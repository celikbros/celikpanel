package transport

// Setup renewal evidence contains no key material or arbitrary file paths.
// Kurulum yenileme kaniti anahtar veya serbest dosya yolu tasimaz.
type PanelRenewalReadinessRequest struct{ Domain string }
type PanelRenewalReadinessResponse struct {
	Ready bool
	Code  string
}
