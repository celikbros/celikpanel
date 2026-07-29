package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Entitlements record rights only. Store offerings live in the same panel
// database as typed rows; billing and host installation are separate systems.
// Haklar yalnız yetkiyi kaydeder. Mağaza teklifleri aynı panel veritabanında
// tipli satırlar olarak yaşar; faturalandırma ve host kurulumu ayrı sistemlerdir.

type product struct {
	ID                string `json:"id"`
	Name              string `json:"name"`
	Description       string `json:"description"`
	Category          string `json:"category"`
	MonthlyPriceCents *int   `json:"monthly_price_cents"`
	ReleaseState      string `json:"release_state"`
}

// hasEntitlement fails closed for unknown offerings, unavailable offerings,
// suspended grants and every malformed or expired timestamp.
// hasEntitlement bilinmeyen/kullanılamayan teklifler, askıdaki haklar ve bozuk
// ya da süresi dolmuş tüm zaman damgaları için kapalı davranır.
func (p *Panel) hasEntitlement(ctx context.Context, subscriptionID int, productID string) bool {
	var status string
	var expires sql.NullString
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT e.status, e.expires_at
		FROM subscription_entitlements e
		JOIN store_offerings o ON o.id = e.product_id
		WHERE e.subscription_id = ? AND e.product_id = ?
		  AND o.release_state = 'available'
		  AND o.entitlement_mode = 'grant'`,
		subscriptionID, productID,
	).Scan(&status, &expires)
	if err != nil || status != "active" {
		return false
	}
	if expires.Valid {
		expiry, err := time.Parse(time.RFC3339, expires.String)
		if err != nil || !expiry.After(time.Now().UTC()) {
			return false
		}
	}
	return true
}

func (p *Panel) requireEntitlement(w http.ResponseWriter, r *http.Request, subscriptionID int, productID string) bool {
	if caller := currentCaller(r); caller != nil && caller.Role == roleAdmin {
		return true
	}
	if p.hasEntitlement(r.Context(), subscriptionID, productID) {
		return true
	}
	name := productID
	if offerings, err := p.loadStoreOfferings(r.Context(), productID); err == nil && len(offerings) == 1 {
		name = localizedStoreText(offerings[0].Metadata.Name, "en")
	}
	writeCodedError(w, http.StatusPaymentRequired, errCodeEntitlement,
		"this feature requires the \""+name+"\" add-on", "/addons")
	return false
}

// handleProducts is a compatibility projection for older clients. It is read
// from Store rows and deliberately exposes no invented price.
// handleProducts eski istemciler için uyumluluk görünümüdür. Mağaza
// satırlarından okunur ve bilerek uydurma fiyat yayınlamaz.
func (p *Panel) handleProducts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path != "/api/v1/products" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if len(r.URL.Query()) != 0 {
		writeClientError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}
	offerings, err := p.loadStoreOfferings(r.Context(), "")
	if err != nil {
		writeServerError(w, err)
		return
	}
	products := make([]product, 0, len(offerings))
	for _, offering := range offerings {
		products = append(products, product{
			ID: offering.ID, Name: offering.Metadata.Name.EN,
			Description: offering.Metadata.Description.EN, Category: offering.Category,
			MonthlyPriceCents: nil, ReleaseState: offering.ReleaseState,
		})
	}
	json.NewEncoder(w).Encode(map[string]any{"products": products})
}

type entitlementPath struct {
	SubscriptionID int
	ProductID      string
	Collection     bool
}

func parseEntitlementPath(path string) (entitlementPath, bool) {
	const prefix = "/api/v1/subscriptions/"
	if !strings.HasPrefix(path, prefix) {
		return entitlementPath{}, false
	}
	parts := strings.Split(strings.TrimPrefix(path, prefix), "/")
	if len(parts) != 2 && len(parts) != 3 {
		return entitlementPath{}, false
	}
	if parts[0] == "" || parts[1] != "entitlements" {
		return entitlementPath{}, false
	}
	subscriptionID, err := strconv.Atoi(parts[0])
	if err != nil || subscriptionID <= 0 {
		return entitlementPath{}, false
	}
	result := entitlementPath{SubscriptionID: subscriptionID, Collection: len(parts) == 2}
	if len(parts) == 3 {
		if parts[2] == "" {
			return entitlementPath{}, false
		}
		result.ProductID = parts[2]
	}
	return result, true
}

// handleSubscriptionEntitlements gives every visible caller read access, but
// mutation is admin-only until a typed reseller entitlement-pool model exists.
// handleSubscriptionEntitlements görünür çağıranlara okuma verir; tipli bayi
// hak havuzu modeli gelene kadar değişiklik yalnız admin içindir.
func (p *Panel) handleSubscriptionEntitlements(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	path, ok := parseEntitlementPath(r.URL.Path)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if (r.Method == http.MethodGet || r.Method == http.MethodPost) && !path.Collection {
		http.NotFound(w, r)
		return
	}
	if r.Method == http.MethodDelete && path.Collection {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodPost && r.Method != http.MethodDelete {
		w.Header().Set("Allow", "GET, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if len(r.URL.Query()) != 0 {
		writeClientError(w, http.StatusBadRequest, "query parameters are not supported")
		return
	}

	caller := currentCaller(r)
	if err := p.canAccessSubscription(r.Context(), caller, path.SubscriptionID); err != nil {
		if errors.Is(err, errNotFound) {
			writeClientError(w, http.StatusNotFound, "subscription not found")
		} else {
			writeServerError(w, err)
		}
		return
	}
	if r.Method != http.MethodGet && (caller == nil || caller.Role != roleAdmin) {
		writeClientError(w, http.StatusForbidden, "administrator access required")
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.listEntitlements(w, r, path.SubscriptionID)
	case http.MethodPost:
		p.grantEntitlement(w, r, path.SubscriptionID)
	case http.MethodDelete:
		p.revokeEntitlement(w, r, path.SubscriptionID, path.ProductID)
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

func (p *Panel) listEntitlements(w http.ResponseWriter, r *http.Request, subscriptionID int) {
	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT e.product_id, e.status, e.granted_at, e.expires_at,
		       COALESCE(o.category, ''), COALESCE(o.metadata_json, '')
		FROM subscription_entitlements e
		LEFT JOIN store_offerings o ON o.id = e.product_id
		WHERE e.subscription_id = ?
		ORDER BY e.granted_at DESC, e.product_id`,
		subscriptionID,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	list := []entitlementRow{}
	for rows.Next() {
		var row entitlementRow
		var rawMetadata string
		if err := rows.Scan(&row.ProductID, &row.Status, &row.GrantedAt, &row.ExpiresAt,
			&row.Category, &rawMetadata); err != nil {
			writeServerError(w, err)
			return
		}
		row.Name = row.ProductID
		if rawMetadata != "" {
			metadata, err := decodeStoreMetadata(rawMetadata)
			if err != nil {
				writeServerError(w, err)
				return
			}
			row.Name = metadata.Name.EN
			row.Description = metadata.Description.EN
		}
		list = append(list, row)
	}
	if err := rows.Err(); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"entitlements": list})
}

func decodeStrictJSON(w http.ResponseWriter, r *http.Request, target any) error {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("unexpected JSON value")
		}
		return err
	}
	return nil
}

func normalizedFutureExpiry(raw string, now time.Time) (*string, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	expiry, err := time.Parse(time.RFC3339, raw)
	if err != nil || !expiry.After(now) {
		return nil, errors.New("expires_at must be a future RFC3339 timestamp")
	}
	normalized := expiry.UTC().Format(time.RFC3339)
	return &normalized, nil
}

func (p *Panel) grantEntitlement(w http.ResponseWriter, r *http.Request, subscriptionID int) {
	var request struct {
		ProductID string `json:"product_id"`
		ExpiresAt string `json:"expires_at,omitempty"`
	}
	if err := decodeStrictJSON(w, r, &request); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	request.ProductID = strings.TrimSpace(request.ProductID)
	offerings, err := p.loadStoreOfferings(r.Context(), request.ProductID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if request.ProductID == "" || len(offerings) != 1 {
		writeClientError(w, http.StatusBadRequest, "unknown offering")
		return
	}
	offering := offerings[0]
	if offering.EntitlementMode != "grant" {
		writeClientError(w, http.StatusConflict, "offering is not grantable")
		return
	}

	now := time.Now().UTC()
	expiry, err := normalizedFutureExpiry(request.ExpiresAt, now)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	var currentStatus string
	var currentExpiry sql.NullString
	err = p.db.GetDB().QueryRowContext(r.Context(), `
		SELECT status, expires_at FROM subscription_entitlements
		WHERE subscription_id = ? AND product_id = ?`,
		subscriptionID, offering.ID,
	).Scan(&currentStatus, &currentExpiry)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		writeServerError(w, err)
		return
	}
	if err == nil && currentStatus == "active" && nullableStringEqual(currentExpiry, expiry) {
		json.NewEncoder(w).Encode(map[string]any{"success": true, "idempotent": true})
		return
	}

	// An exact retry of an already-active grant is a read-only success even if
	// the offering was retired or the component cache became stale after the
	// original response was lost. Release and runtime gates protect only real
	// entitlement state changes.
	// Zaten etkin olan aynı hak isteğinin birebir tekrarı, ilk yanıt kaybolduktan
	// sonra teklif kullanımdan kaldırılmış veya bileşen önbelleği bayatlamış olsa
	// bile salt okunur bir başarıdır. Yayın ve çalışma zamanı denetimleri yalnız
	// gerçek hak durumu değişikliklerini korur.
	if offering.ReleaseState != "available" {
		writeClientError(w, http.StatusConflict, "offering is not grantable")
		return
	}
	snapshot := p.loadStoreRuntimeSnapshot(r.Context(), now)
	platform, runtime, _ := offeringPlatformAndRuntime(offering, snapshot)
	if platform != "supported" ||
		(runtime != "running" && runtime != "installed" && runtime != "not_applicable") {
		writeClientError(w, http.StatusConflict, "required components are not freshly verified and usable")
		return
	}

	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()

	var expiryValue any
	if expiry != nil {
		expiryValue = *expiry
	}
	result, err := tx.ExecContext(r.Context(), `
		INSERT INTO subscription_entitlements (subscription_id, product_id, status, expires_at)
		VALUES (?, ?, 'active', ?)
		ON CONFLICT(subscription_id, product_id)
		DO UPDATE SET status = 'active', expires_at = excluded.expires_at,
		              granted_at = datetime('now')
		WHERE subscription_entitlements.status != 'active'
		   OR subscription_entitlements.expires_at IS NOT excluded.expires_at`,
		subscriptionID, offering.ID, expiryValue,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if affected == 0 {
		if err := tx.Commit(); err != nil {
			writeServerError(w, err)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "idempotent": true})
		return
	}
	if err := insertEntitlementAudit(r, tx, "entitlement.grant:"+offering.ID, subscriptionID); err != nil {
		writeServerError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"success": true})
}

func nullableStringEqual(current sql.NullString, wanted *string) bool {
	if !current.Valid {
		return wanted == nil
	}
	return wanted != nil && current.String == *wanted
}

func (p *Panel) revokeEntitlement(w http.ResponseWriter, r *http.Request, subscriptionID int, productID string) {
	offerings, err := p.loadStoreOfferings(r.Context(), productID)
	if err != nil {
		writeServerError(w, err)
		return
	}
	if len(offerings) != 1 {
		writeClientError(w, http.StatusBadRequest, "unknown offering")
		return
	}
	offering := offerings[0]
	if offering.EntitlementMode != "grant" {
		writeClientError(w, http.StatusConflict, "offering is not revocable")
		return
	}

	tx, err := p.db.GetDB().BeginTx(r.Context(), nil)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer tx.Rollback()
	result, err := tx.ExecContext(r.Context(),
		`DELETE FROM subscription_entitlements WHERE subscription_id = ? AND product_id = ?`,
		subscriptionID, offering.ID,
	)
	if err != nil {
		writeServerError(w, err)
		return
	}
	affected, err := result.RowsAffected()
	if err != nil {
		writeServerError(w, err)
		return
	}
	if affected > 0 {
		if err := insertEntitlementAudit(r, tx, "entitlement.revoke:"+offering.ID, subscriptionID); err != nil {
			writeServerError(w, err)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]any{"success": true, "idempotent": affected == 0})
}

// insertEntitlementAudit shares the entitlement transaction, so a grant is
// never committed without its audit row.
// insertEntitlementAudit hak işlemiyle aynı transaction'ı kullanır; böylece
// denetim satırı olmadan bir hak değişikliği commit edilmez.
func insertEntitlementAudit(r *http.Request, tx *sql.Tx, action string, subscriptionID int) error {
	caller := currentCaller(r)
	if caller == nil {
		return errors.New("authenticated caller required")
	}
	userAgent := r.UserAgent()
	if len(userAgent) > 300 {
		userAgent = userAgent[:300]
	}
	_, err := tx.ExecContext(r.Context(), `
		INSERT INTO audit_logs
			(user_id, action, resource_type, resource_id, ip_address, user_agent)
		VALUES (?, ?, 'subscription', ?, ?, ?)`,
		caller.ID, action, subscriptionID, clientIP(r), userAgent,
	)
	return err
}
