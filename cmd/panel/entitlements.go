package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The product/entitlement layer — what turns the hosting panel into a
// platform you can sell on. Products are a curated catalogue in code (a
// firewall, a business-email tier, the app installer…); a subscription's
// entitlements are the ledger of which products it holds. Enforcement asks
// one question, hasEntitlement, at each gated feature. Admins bypass — they
// own the server and grant everything. Money is never here: billing is an
// external system; this records the right, not the payment.
//
// Ürün/hak katmanı — hosting panelini üzerinde satış yapılabilen bir
// platforma çeviren şey. Ürünler kodda kürlü bir katalogdur (firewall,
// iş-e-postası kademesi, uygulama kurucu…); bir aboneliğin hakları, hangi
// ürünleri tuttuğunun defteridir. Uygulama, her kapılı özellikte tek bir
// soru sorar: hasEntitlement. Yöneticiler atlar — sunucunun sahibi onlardır
// ve her şeyi verir. Para asla burada değildir.

type product struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Category    string `json:"category"`
	// Informational only — the actual charge happens in an external billing
	// system. 0 means "included / grant-only".
	// Yalnız bilgi amaçlı — gerçek ücret dış faturalandırma sisteminde alınır.
	// 0, "dahil / yalnız-verilir" demektir.
	MonthlyPriceCents int `json:"monthly_price_cents"`
}

// productCatalog is the curated set of sellable add-ons. Adding a product is
// a data change, not new plumbing — the grant/enforce machinery is generic.
// productCatalog, satılabilir eklentilerin kürlü setidir. Bir ürün eklemek
// veri değişikliğidir, yeni tesisat değil.
var productCatalog = []product{
	{ID: "app_installer", Name: "Application installer", Description: "One-click installs for WordPress and other apps.", Category: "apps", MonthlyPriceCents: 0},
	{ID: "firewall", Name: "Managed firewall", Description: "Per-site firewall rules and brute-force protection.", Category: "security", MonthlyPriceCents: 500},
	{ID: "business_email", Name: "Business email", Description: "Larger mailboxes, priority delivery and archiving.", Category: "email", MonthlyPriceCents: 300},
	{ID: "vpn", Name: "VPN access", Description: "Private WireGuard VPN peers for secure remote access.", Category: "network", MonthlyPriceCents: 400},
	{ID: "extra_ip", Name: "Dedicated IP", Description: "A dedicated IPv4 address for this account.", Category: "network", MonthlyPriceCents: 200},
}

func productByID(id string) *product {
	for i := range productCatalog {
		if productCatalog[i].ID == id {
			return &productCatalog[i]
		}
	}
	return nil
}

// hasEntitlement reports whether a subscription may use a product right now:
// an active, unexpired grant exists. Used at every gated feature.
// hasEntitlement, bir aboneliğin bir ürünü şu an kullanıp kullanamayacağını
// bildirir: aktif, süresi dolmamış bir hak var mı.
func (p *Panel) hasEntitlement(ctx context.Context, subscriptionID int, productID string) bool {
	var status string
	var expires *string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT status, expires_at FROM subscription_entitlements
		WHERE subscription_id = ? AND product_id = ?`, subscriptionID, productID).Scan(&status, &expires)
	if err != nil || status != "active" {
		return false
	}
	if expires != nil {
		if t, perr := time.Parse(time.RFC3339, *expires); perr == nil && time.Now().After(t) {
			return false
		}
	}
	return true
}

// requireEntitlement is the HTTP gate: admins pass, otherwise the
// subscription must hold the product. Returns false (and writes 402/403)
// when blocked.
// requireEntitlement, HTTP kapısıdır: yöneticiler geçer, yoksa aboneliğin
// ürünü tutması gerekir.
func (p *Panel) requireEntitlement(w http.ResponseWriter, r *http.Request, subscriptionID int, productID string) bool {
	if c := currentCaller(r); c != nil && c.Role == roleAdmin {
		return true
	}
	if p.hasEntitlement(r.Context(), subscriptionID, productID) {
		return true
	}
	// 402 Payment Required is the honest status: the feature exists, the
	// account just is not entitled to it.
	// 402, dürüst durumdur: özellik var, hesabın hakkı yok.
	prod := productByID(productID)
	name := productID
	if prod != nil {
		name = prod.Name
	}
	writeClientError(w, http.StatusPaymentRequired, "this feature requires the \""+name+"\" add-on")
	return false
}

// handleProducts returns the curated product catalogue (any signed-in user;
// the storefront is not secret).
// handleProducts, kürlü ürün kataloğunu döndürür.
func (p *Panel) handleProducts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"products": productCatalog})
}

// handleSubscriptionEntitlements handles GET (list) and POST (grant) at
// /api/v1/subscriptions/{id}/entitlements, and DELETE (revoke) with a
// trailing product id. Grant/revoke are manager-only and ownership-scoped;
// a reseller manages only their own customers' subscriptions.
// handleSubscriptionEntitlements, /api/v1/subscriptions/{id}/entitlements
// üzerinde GET (listele) ve POST (ver) ile sondaki ürün kimliğiyle DELETE
// (geri al) işler. Ver/geri-al yalnız yönetici rollerine ve sahiplik
// kapsamına bağlıdır.
func (p *Panel) handleSubscriptionEntitlements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Path: /api/v1/subscriptions/{id}/entitlements[/{productID}]
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/subscriptions/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 || parts[1] != "entitlements" {
		http.NotFound(w, r)
		return
	}
	subID, err := strconv.Atoi(parts[0])
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid subscription id")
		return
	}

	// Listing is allowed to anyone who can see the subscription; granting and
	// revoking require a manager.
	// Listeleme, aboneliği görebilen herkese açıktır; verme ve geri alma
	// yönetici ister.
	caller := currentCaller(r)
	if err := p.canAccessSubscription(r.Context(), caller, subID); err != nil {
		writeClientError(w, http.StatusNotFound, "subscription not found")
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.listEntitlements(w, r, subID)
	case http.MethodPost:
		if p.requireManager(w, r) == nil {
			return
		}
		p.grantEntitlement(w, r, subID)
	case http.MethodDelete:
		if p.requireManager(w, r) == nil {
			return
		}
		if len(parts) < 3 || parts[2] == "" {
			writeClientError(w, http.StatusBadRequest, "product id required")
			return
		}
		p.revokeEntitlement(w, r, subID, parts[2])
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

type entitlementRow struct {
	ProductID   string  `json:"product_id"`
	Name        string  `json:"name"`
	Category    string  `json:"category"`
	Status      string  `json:"status"`
	GrantedAt   string  `json:"granted_at"`
	ExpiresAt   *string `json:"expires_at,omitempty"`
	Description string  `json:"description"`
}

func (p *Panel) listEntitlements(w http.ResponseWriter, r *http.Request, subID int) {
	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT product_id, status, granted_at, expires_at
		FROM subscription_entitlements WHERE subscription_id = ?
		ORDER BY granted_at DESC`, subID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	list := []entitlementRow{}
	for rows.Next() {
		var e entitlementRow
		if rows.Scan(&e.ProductID, &e.Status, &e.GrantedAt, &e.ExpiresAt) != nil {
			continue
		}
		if prod := productByID(e.ProductID); prod != nil {
			e.Name, e.Category, e.Description = prod.Name, prod.Category, prod.Description
		} else {
			e.Name = e.ProductID
		}
		list = append(list, e)
	}
	json.NewEncoder(w).Encode(map[string]any{"entitlements": list})
}

func (p *Panel) grantEntitlement(w http.ResponseWriter, r *http.Request, subID int) {
	var req struct {
		ProductID string `json:"product_id"`
		ExpiresAt string `json:"expires_at,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if productByID(req.ProductID) == nil {
		writeClientError(w, http.StatusBadRequest, "unknown product")
		return
	}
	var expires any
	if req.ExpiresAt != "" {
		expires = req.ExpiresAt
	}
	// Granting again re-activates and updates expiry (idempotent upsert).
	// Yeniden vermek yeniden etkinleştirir ve süreyi günceller.
	if _, err := p.db.GetDB().ExecContext(r.Context(), `
		INSERT INTO subscription_entitlements (subscription_id, product_id, status, expires_at)
		VALUES (?, ?, 'active', ?)
		ON CONFLICT(subscription_id, product_id)
		DO UPDATE SET status = 'active', expires_at = excluded.expires_at`,
		subID, req.ProductID, expires); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "entitlement.grant:"+req.ProductID, "subscription", subID)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func (p *Panel) revokeEntitlement(w http.ResponseWriter, r *http.Request, subID int, productID string) {
	if _, err := p.db.GetDB().ExecContext(r.Context(),
		`DELETE FROM subscription_entitlements WHERE subscription_id = ? AND product_id = ?`,
		subID, productID); err != nil {
		writeServerError(w, err)
		return
	}
	p.audit(r, "entitlement.revoke:"+productID, "subscription", subID)
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}
