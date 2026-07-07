package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/services"
)

// The application catalog and installers. An app is a recipe assembled from
// the panel's existing building blocks (a site + a database); the catalog is
// curated, not a third-party marketplace. WordPress is the first entry — more
// are just more recipes.
//
// Uygulama kataloğu ve kurucuları. Bir uygulama, panelin var olan yapı
// taşlarından (bir site + bir veritabanı) kurulan bir reçetedir; katalog
// kürlüdür, üçüncü parti pazar yeri değil. WordPress ilk giriştir — fazlası
// yalnızca daha fazla reçetedir.

type appCatalogEntry struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	RequiresDB  bool   `json:"requires_db"`
	RequiresPHP bool   `json:"requires_php"`
}

var appCatalog = []appCatalogEntry{
	{
		ID:          "wordpress",
		Name:        "WordPress",
		Description: "The world's most popular CMS — blogs, sites, shops.",
		Icon:        "wordpress",
		RequiresDB:  true,
		RequiresPHP: true,
	},
}

// handleAppCatalog returns the curated list of installable applications.
// handleAppCatalog, kurulabilir uygulamaların kürlü listesini döndürür.
func (p *Panel) handleAppCatalog(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"apps": appCatalog})
}

// handleAppInstall installs an application into a domain's site. It creates a
// dedicated database, then runs the app's installer on the agent. Only the
// curated catalog IDs are accepted.
// handleAppInstall, bir uygulamayı domain'in sitesine kurar. Ayrılmış bir
// veritabanı oluşturur, sonra uygulamanın kurucusunu agent'ta çalıştırır.
// Yalnız kürlü katalog kimlikleri kabul edilir.
func (p *Panel) handleAppInstall(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		App string `json:"app"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.App != "wordpress" {
		writeClientError(w, http.StatusBadRequest, "unknown application")
		return
	}

	// The app installer is a sellable add-on: the domain's subscription must
	// hold the "app_installer" entitlement (admins bypass). This is the one
	// real enforcement point that proves the product layer is not theater —
	// grant it and installs work, revoke it and they are blocked with 402.
	// Uygulama kurucu satılabilir bir eklentidir: domain'in aboneliği
	// "app_installer" hakkını tutmalı (yöneticiler atlar). Bu, ürün
	// katmanının tiyatro olmadığını kanıtlayan tek gerçek uygulama noktası —
	// ver, kurulumlar çalışır; geri al, 402 ile engellenir.
	if subID, err := p.domainSubscriptionID(r.Context(), domainID); err == nil {
		if !p.requireEntitlement(w, r, subID, "app_installer") {
			return
		}
	}

	docroot, err := p.siteDocroot(r.Context(), domainID)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "site not found")
		return
	}
	domainName := p.domainNameByID(r.Context(), domainID)

	// One dedicated database + user for this install. The names derive from
	// the site user so they are stable and readable.
	// Bu kurulum için ayrılmış bir veritabanı + kullanıcı. Adlar, kararlı ve
	// okunur olsun diye site kullanıcısından türetilir.
	base := sanitizeDBIdent(services.SiteUsername(domainName))
	dbName := base + "_wp"
	dbUser := base + "_wp"
	dbPass := randomDBPassword()

	var dbResp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.CreateDatabase", &struct {
		Type     string `json:"type"`
		Name     string `json:"name"`
		User     string `json:"user"`
		Password string `json:"password"`
	}{Type: "mysql", Name: dbName, User: dbUser, Password: dbPass}, &dbResp); err != nil {
		writeServerError(w, err)
		return
	}
	if dbResp.Error != "" {
		writeClientError(w, http.StatusConflict, "database creation failed: "+dbResp.Error)
		return
	}

	// Record the database in the ledger so it shows on the Databases page.
	// Best-effort: WordPress works off the real database regardless.
	// Veritabanını deftere kaydet ki Veritabanları sayfasında görünsün.
	// En-iyi-çaba: WordPress zaten gerçek veritabanıyla çalışır.
	p.recordAppDatabase(r.Context(), domainID, dbName)

	var wpResp struct {
		Installed bool   `json:"installed"`
		Detail    string `json:"detail,omitempty"`
		Error     string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.InstallWordPress", &struct {
		DocRoot  string `json:"doc_root"`
		DBName   string `json:"db_name"`
		DBUser   string `json:"db_user"`
		DBPass   string `json:"db_pass"`
		DBHost   string `json:"db_host"`
		Username string `json:"username"`
	}{
		DocRoot:  docroot,
		DBName:   dbName,
		DBUser:   dbUser,
		DBPass:   dbPass,
		DBHost:   "localhost",
		Username: services.SiteUsername(domainName),
	}, &wpResp); err != nil {
		writeServerError(w, err)
		return
	}
	if wpResp.Error != "" {
		writeClientError(w, http.StatusConflict, wpResp.Error)
		return
	}

	p.audit(r, "app.install:"+req.App, "domain", domainID)
	json.NewEncoder(w).Encode(map[string]any{
		"success":   true,
		"detail":    wpResp.Detail,
		"setup_url": "https://" + domainName + "/wp-admin/install.php",
	})
}

// recordAppDatabase writes the app's database into databases_v2 under the
// domain's subscription and the default MySQL/MariaDB server, if one is
// registered. Silent no-op when no server row exists.
// recordAppDatabase, uygulamanın veritabanını domain'in aboneliği ve
// varsayılan MySQL/MariaDB sunucusu altında databases_v2'ye yazar.
func (p *Panel) recordAppDatabase(ctx context.Context, domainID int, dbName string) {
	var subID, serverID int
	if p.db.GetDB().QueryRowContext(ctx,
		`SELECT subscription_id FROM domains WHERE id = ?`, domainID).Scan(&subID) != nil {
		return
	}
	p.ensureInstalledDBServers(ctx, subID)
	if p.db.GetDB().QueryRowContext(ctx, `
		SELECT ds.id FROM database_servers ds
		JOIN database_server_types dst ON ds.type_id = dst.id
		WHERE dst.name IN ('mysql','mariadb') ORDER BY ds.is_default DESC, ds.id LIMIT 1`).Scan(&serverID) != nil {
		return
	}
	p.db.GetDB().ExecContext(ctx,
		`INSERT INTO databases_v2 (server_id, subscription_id, domain_id, name) VALUES (?, ?, ?, ?)`,
		serverID, subID, domainID, dbName)
}

// sanitizeDBIdent keeps only the characters a MySQL identifier tolerates.
// sanitizeDBIdent, yalnız bir MySQL tanımlayıcısının hoş gördüğü karakterleri
// tutar.
func sanitizeDBIdent(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if len(out) > 48 {
		out = out[:48]
	}
	if out == "" {
		out = "app"
	}
	return out
}

// randomDBPassword returns a 24-char CSPRNG password (no shell-hostile chars).
// randomDBPassword, 24 karakterlik CSPRNG parola döndürür.
func randomDBPassword() string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 24)
	max := big.NewInt(int64(len(charset)))
	for i := range b {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
		}
		b[i] = charset[n.Int64()]
	}
	return string(b)
}
