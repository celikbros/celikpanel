package main

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
)

// Account management: the layer that makes the role hierarchy usable.
// Admins create resellers and customers; resellers create their own
// customers (parent_id = reseller). Visibility everywhere reuses the same
// ownership rules the authz layer already enforces for resources.
//
// Hesap yönetimi: rol hiyerarşisini kullanılabilir yapan katman. Yöneticiler
// bayi ve müşteri oluşturur; bayiler kendi müşterilerini oluşturur
// (parent_id = bayi). Görünürlük her yerde, authz katmanının kaynaklar için
// zaten uyguladığı sahiplik kurallarını yeniden kullanır.

const minPasswordLen = 8

type userResponse struct {
	ID            int    `json:"id"`
	Username      string `json:"username"`
	Email         string `json:"email"`
	Role          string `json:"role"`
	Status        string `json:"status"`
	ParentID      *int   `json:"parent_id,omitempty"`
	ParentName    string `json:"parent_name,omitempty"`
	Subscriptions int    `json:"subscriptions"`
	Domains       int    `json:"domains"`
	CreatedAt     string `json:"created_at"`
}

// requireManager rejects callers that are not admin or reseller.
// requireManager, admin ya da bayi olmayan çağıranları reddeder.
func (p *Panel) requireManager(w http.ResponseWriter, r *http.Request) *Caller {
	c := currentCaller(r)
	if c == nil || (c.Role != roleAdmin && c.Role != roleReseller) {
		writeClientError(w, http.StatusForbidden, "administrator or reseller access required")
		return nil
	}
	return c
}

// canManageUser reports whether the caller may act on the target account.
// Admin-role targets are never manageable through this API: the panel's
// single administrator is managed via the CLI only.
// canManageUser, çağıranın hedef hesap üzerinde işlem yapıp yapamayacağını
// bildirir. Admin rollü hedefler bu API üzerinden asla yönetilemez: panelin
// tek yöneticisi yalnızca CLI ile yönetilir.
func canManageUser(c *Caller, target *core.User) bool {
	if target.Role == roleAdmin {
		return false
	}
	if c.Role == roleAdmin {
		return true
	}
	return target.ParentID != nil && *target.ParentID == c.ID
}

// handleSubscriptions lists subscriptions visible to the caller, with their
// owner — used by the import wizard and future subscription pickers.
// handleSubscriptions, çağıranın görebildiği abonelikleri sahipleriyle
// listeler — içe aktarım sihirbazı ve gelecekteki abonelik seçicileri için.
func (p *Panel) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := p.requireManager(w, r)
	if c == nil {
		return
	}

	query := `
		SELECT s.id, s.name, u.username
		FROM subscriptions s JOIN users u ON s.owner_id = u.id`
	args := []any{}
	if c.Role != roleAdmin {
		// Own subscriptions plus those of the reseller's customers.
		// Kendi abonelikleri artı bayinin müşterilerinin abonelikleri.
		query += ` WHERE s.owner_id = ? OR s.owner_id IN (SELECT id FROM users WHERE parent_id = ?)`
		args = append(args, c.ID, c.ID)
	}
	query += ` ORDER BY u.username, s.name`

	rows, err := p.db.GetDB().QueryContext(r.Context(), query, args...)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	type sub struct {
		ID    int                `json:"id"`
		Name  string             `json:"name"`
		Owner string             `json:"owner"`
		Usage *subscriptionUsage `json:"usage,omitempty"`
	}
	subs := []sub{}
	for rows.Next() {
		var s sub
		if rows.Scan(&s.ID, &s.Name, &s.Owner) == nil {
			subs = append(subs, s)
		}
	}
	// Attach the real usage picture per subscription (measured disk + counts
	// vs the plan limits). The list is short (a customer has one or a few),
	// and the numbers are cached reads — no probing.
	// Her aboneliğe gerçek kullanım tablosunu ekle (ölçülen disk + sayılar vs
	// plan limitleri). Liste kısadır ve sayılar önbellekli okumadır.
	for i := range subs {
		if u, err := p.subscriptionUsageFor(r.Context(), subs[i].ID); err == nil {
			subs[i].Usage = u
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"subscriptions": subs})
}

func (p *Panel) handleUsers(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := p.requireManager(w, r)
	if c == nil {
		return
	}

	switch r.Method {
	case http.MethodGet:
		p.handleListUsers(w, r, c)
	case http.MethodPost:
		p.handleCreateUser(w, r, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleUserByID routes /api/v1/users/{id}[/impersonate].
// handleUserByID, /api/v1/users/{id}[/impersonate] yönlendirmesini yapar.
func (p *Panel) handleUserByID(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	c := p.requireManager(w, r)
	if c == nil {
		return
	}

	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/users/")
	idStr, action, _ := strings.Cut(rest, "/")
	id, err := strconv.Atoi(idStr)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid user id")
		return
	}

	target, err := p.users.GetByID(r.Context(), id)
	if err != nil || !canManageUser(c, target) {
		// Same 404 for "missing" and "not yours" — no probing.
		// "Yok" ile "senin değil" aynı 404 — yoklamaya izin yok.
		writeClientError(w, http.StatusNotFound, "user not found")
		return
	}

	switch {
	case action == "impersonate" && r.Method == http.MethodPost:
		p.handleImpersonate(w, r, c, target)
	case action == "" && r.Method == http.MethodPut:
		p.handleUpdateUser(w, r, target)
	case action == "" && r.Method == http.MethodDelete:
		p.handleDeleteUser(w, r, target)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (p *Panel) handleListUsers(w http.ResponseWriter, r *http.Request, c *Caller) {
	// One query with parent name and resource counts; filtered by the same
	// visibility rule as everything else.
	// Üst kullanıcı adı ve kaynak sayılarıyla tek sorgu; diğer her şeyle aynı
	// görünürlük kuralıyla süzülür.
	query := `
		SELECT u.id, u.username, u.email, u.role, COALESCE(u.status,'active'),
		       u.parent_id, COALESCE(pu.username,''),
		       (SELECT COUNT(*) FROM subscriptions s WHERE s.owner_id = u.id),
		       (SELECT COUNT(*) FROM domains d JOIN subscriptions s2 ON d.subscription_id = s2.id WHERE s2.owner_id = u.id),
		       COALESCE(u.created_at,'')
		FROM users u
		LEFT JOIN users pu ON u.parent_id = pu.id`
	args := []any{}
	if c.Role != roleAdmin {
		query += ` WHERE u.parent_id = ?`
		args = append(args, c.ID)
	}
	query += ` ORDER BY u.created_at DESC`

	rows, err := p.db.GetDB().QueryContext(r.Context(), query, args...)
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	users := make([]userResponse, 0)
	for rows.Next() {
		var u userResponse
		var parentID *int
		if err := rows.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.Status,
			&parentID, &u.ParentName, &u.Subscriptions, &u.Domains, &u.CreatedAt); err != nil {
			continue
		}
		u.ParentID = parentID
		users = append(users, u)
	}
	json.NewEncoder(w).Encode(map[string]any{"users": users})
}

func (p *Panel) handleCreateUser(w http.ResponseWriter, r *http.Request, c *Caller) {
	var req struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Role     string `json:"role"`
		PlanID   int    `json:"plan_id,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	// Role rules: admins create resellers or customers; resellers create
	// customers only (scope narrows going down, never widens — ROLES.md).
	// Rol kuralları: yöneticiler bayi ya da müşteri oluşturur; bayiler yalnız
	// müşteri oluşturur (kapsam aşağı indikçe daralır — ROLES.md).
	switch {
	case c.Role == roleAdmin && (req.Role == roleReseller || req.Role == roleCustomer):
	case c.Role == roleReseller && req.Role == roleCustomer:
	default:
		writeClientError(w, http.StatusBadRequest, "role not allowed for your account")
		return
	}

	if err := auth.ValidateUsername(req.Username); err != nil {
		writeClientError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.Password) < minPasswordLen {
		writeClientError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeClientError(w, http.StatusBadRequest, "valid email required")
		return
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		writeServerError(w, err)
		return
	}

	parent := c.ID
	user := &core.User{
		Username:     req.Username,
		PasswordHash: hash,
		Email:        req.Email,
		Role:         req.Role,
		ParentID:     &parent,
	}
	if err := p.users.Create(r.Context(), user); err != nil {
		// UNIQUE violations are the caller's mistake, not a server fault.
		// UNIQUE ihlalleri sunucu hatası değil, çağıranın hatasıdır.
		if strings.Contains(err.Error(), "UNIQUE") {
			writeClientError(w, http.StatusConflict, "username or email already exists")
			return
		}
		writeServerError(w, err)
		return
	}

	// Optional: create the first subscription from a plan in the same step
	// (one obvious way to onboard a customer).
	// İsteğe bağlı: aynı adımda plandan ilk aboneliği oluştur (bir müşteriyi
	// devreye almanın tek belirgin yolu).
	if req.PlanID > 0 {
		if err := p.createSubscriptionFromPlan(r.Context(), user.ID, req.PlanID); err != nil {
			writeServerError(w, err)
			return
		}
	}

	log.Printf("[audit] user %d (%s) created user %d (%s, role=%s)", c.ID, c.Role, user.ID, user.Username, user.Role)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "id": user.ID})
}

func (p *Panel) handleUpdateUser(w http.ResponseWriter, r *http.Request, target *core.User) {
	var req struct {
		Email    *string `json:"email,omitempty"`
		Password *string `json:"password,omitempty"`
		Status   *string `json:"status,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if req.Email != nil {
		if !strings.Contains(*req.Email, "@") {
			writeClientError(w, http.StatusBadRequest, "valid email required")
			return
		}
		target.Email = *req.Email
	}
	if req.Password != nil {
		if len(*req.Password) < minPasswordLen {
			writeClientError(w, http.StatusBadRequest, "password must be at least 8 characters")
			return
		}
		hash, err := auth.HashPassword(*req.Password)
		if err != nil {
			writeServerError(w, err)
			return
		}
		target.PasswordHash = hash
	}
	if req.Status != nil {
		if *req.Status != "active" && *req.Status != "suspended" {
			writeClientError(w, http.StatusBadRequest, "status must be active or suspended")
			return
		}
		target.Status = *req.Status
	}

	if err := p.users.Update(r.Context(), target); err != nil {
		writeServerError(w, err)
		return
	}

	// Suspension takes effect immediately: kill the target's sessions.
	// Askıya alma anında etkilidir: hedefin oturumlarını öldür.
	if req.Status != nil && *req.Status == "suspended" {
		_, _ = p.db.GetDB().ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = ?`, target.ID)
	}

	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

func (p *Panel) handleDeleteUser(w http.ResponseWriter, r *http.Request, target *core.User) {
	// A reseller with customers cannot be deleted — no orphaned accounts.
	// Müşterisi olan bir bayi silinemez — sahipsiz hesap kalmaz.
	var children int
	_ = p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT COUNT(*) FROM users WHERE parent_id = ?`, target.ID).Scan(&children)
	if children > 0 {
		writeClientError(w, http.StatusConflict, "this account still has sub-accounts; delete or move them first")
		return
	}

	// Deleting a user cascades subscriptions → domains → sites via foreign
	// keys. The UI confirms loudly; the API states it plainly here.
	// Kullanıcı silmek, yabancı anahtarlarla abonelikler → domain'ler →
	// siteler kaskadını tetikler. Arayüz yüksek sesle onaylatır.
	if err := p.users.Delete(r.Context(), target.ID); err != nil {
		writeServerError(w, err)
		return
	}
	_, _ = p.db.GetDB().ExecContext(r.Context(), `DELETE FROM sessions WHERE user_id = ?`, target.ID)

	log.Printf("[audit] deleted user %d (%s, role=%s)", target.ID, target.Username, target.Role)
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

const impersonatorCookieName = "celikpanel_impersonator"

// handleImpersonate switches the session to the target account while
// keeping the original session in a second HttpOnly cookie so the operator
// can come back. Plesk's "log in as customer" — the support killer-feature.
// handleImpersonate, oturumu hedef hesaba geçirir; asıl oturumu ikinci bir
// HttpOnly çerezde tutar ki operatör geri dönebilsin. Plesk'in "müşteri
// olarak giriş"i — destek için kilit özellik.
func (p *Panel) handleImpersonate(w http.ResponseWriter, r *http.Request, c *Caller, target *core.User) {
	if target.Status == "suspended" {
		writeClientError(w, http.StatusConflict, "account is suspended")
		return
	}

	token, err := p.sessions.Create(r.Context(), target.ID)
	if err != nil {
		writeServerError(w, err)
		return
	}

	// Preserve the operator's own session for the way back.
	// Operatörün kendi oturumunu dönüş yolu için sakla.
	if orig, err := r.Cookie(sessionCookieName); err == nil {
		http.SetCookie(w, p.cookie(impersonatorCookieName, orig.Value, 0))
	}
	http.SetCookie(w, p.cookie(sessionCookieName, token, 0))

	log.Printf("[audit] user %d (%s) impersonated user %d (%s)", c.ID, c.Role, target.ID, target.Username)
	json.NewEncoder(w).Encode(map[string]any{"success": true, "username": target.Username, "role": target.Role})
}

// handleUnimpersonate restores the operator's original session.
// handleUnimpersonate, operatörün asıl oturumunu geri yükler.
func (p *Panel) handleUnimpersonate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	imp, err := r.Cookie(impersonatorCookieName)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "not impersonating")
		return
	}
	// The stored token must still be a valid session.
	// Saklanan token hâlâ geçerli bir oturum olmalı.
	if _, err := p.sessions.Validate(r.Context(), imp.Value); err != nil {
		http.SetCookie(w, p.cookie(impersonatorCookieName, "", -1))
		writeClientError(w, http.StatusUnauthorized, "original session expired; sign in again")
		return
	}

	// Drop the impersonated session, restore the original.
	// Taklit oturumu bırak, aslını geri yükle.
	if cur, err := r.Cookie(sessionCookieName); err == nil {
		_ = p.sessions.Delete(r.Context(), cur.Value)
	}
	http.SetCookie(w, p.cookie(sessionCookieName, imp.Value, 0))
	http.SetCookie(w, p.cookie(impersonatorCookieName, "", -1))
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// handleChangeOwnPassword lets the signed-in user rotate their password
// after proving the current one.
// handleChangeOwnPassword, oturumdaki kullanıcının mevcut parolasını
// kanıtladıktan sonra parolasını değiştirmesini sağlar.
func (p *Panel) handleChangeOwnPassword(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if len(req.NewPassword) < minPasswordLen {
		writeClientError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	user, err := p.users.GetByID(r.Context(), currentUserID(r))
	if err != nil {
		writeServerError(w, err)
		return
	}
	ok, err := auth.VerifyPassword(req.CurrentPassword, user.PasswordHash)
	if err != nil || !ok {
		writeClientError(w, http.StatusForbidden, "current password is incorrect")
		return
	}

	hash, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		writeServerError(w, err)
		return
	}
	user.PasswordHash = hash
	if err := p.users.Update(r.Context(), user); err != nil {
		writeServerError(w, err)
		return
	}
	json.NewEncoder(w).Encode(map[string]bool{"success": true})
}

// cookie builds a session-style cookie consistent with the login handler's
// flags (HttpOnly, SameSite=Lax, Secure per deployment).
// cookie, giriş işleyicisinin bayraklarıyla tutarlı (HttpOnly, SameSite=Lax,
// dağıtıma göre Secure) oturum tarzı bir çerez üretir.
func (p *Panel) cookie(name, value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   p.secureCookies,
		MaxAge:   maxAge,
	}
}

// createSubscriptionFromPlan copies a plan's quotas into a new subscription.
// createSubscriptionFromPlan, bir planın kotalarını yeni bir aboneliğe kopyalar.
func (p *Panel) createSubscriptionFromPlan(ctx context.Context, ownerID, planID int) error {
	var plan core.ServicePlan
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT id, name, max_domains, max_databases, max_email_accounts, disk_quota_mb, bandwidth_quota_mb
		FROM service_plans WHERE id = ?`, planID).
		Scan(&plan.ID, &plan.Name, &plan.MaxDomains, &plan.MaxDatabases, &plan.MaxEmailAccounts, &plan.DiskQuotaMB, &plan.BandwidthQuotaMB)
	if err != nil {
		return errors.New("plan not found")
	}

	_, err = p.db.GetDB().ExecContext(ctx, `
		INSERT INTO subscriptions (owner_id, name, plan_id, max_domains, max_databases, max_email_accounts, disk_quota_mb, bandwidth_quota_mb, status)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'active')`,
		ownerID, plan.Name, plan.ID, plan.MaxDomains, plan.MaxDatabases, plan.MaxEmailAccounts, plan.DiskQuotaMB, plan.BandwidthQuotaMB)
	return err
}
