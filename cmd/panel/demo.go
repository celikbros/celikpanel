package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostingpath"
)

// Demo mode is a development-only convenience: it seeds one account per role
// with a known password and exposes them so the login screen can offer
// one-click sign-in. It is gated entirely behind the --demo flag; without
// it, the seed never runs and the endpoint returns nothing, so production
// builds carry no baked-in credentials.
//
// Demo modu yalnızca geliştirme kolaylığıdır: her rol için bilinen parolalı
// birer hesap oluşturur ve login ekranı tek tıkla giriş sunabilsin diye
// bunları açığa çıkarır. Tamamen --demo bayrağının arkasındadır; bayrak
// yoksa seed hiç çalışmaz ve uç nokta boş döner, dolayısıyla üretim
// derlemelerinde gömülü kimlik bilgisi bulunmaz.

const demoPassword = "demo1234"

// demoAccounts are the roles seeded in demo mode. additional_user is left
// out on purpose: it is a scoped login that only exists under a customer
// once the authorization model lands.
// demoAccounts, demo modunda oluşturulan rollerdir. additional_user bilerek
// dışarıda: yetkilendirme modeli gelince bir müşterinin altında var olan
// kapsamlı bir giriştir.
var demoAccounts = []struct {
	Username string
	Role     string
}{
	{"admin", "admin"},
	{"reseller", "reseller"},
	{"customer", "customer"},
}

// seedDemoAccounts creates or resets the demo accounts to a known password.
// seedDemoAccounts, demo hesaplarını bilinen bir parolaya oluşturur ya da
// sıfırlar.
func (p *Panel) seedDemoAccounts() {
	ctx := context.Background()
	hash, err := auth.HashPassword(demoPassword)
	if err != nil {
		log.Printf("demo: failed to hash password: %v", err)
		return
	}
	for _, acc := range demoAccounts {
		existing, err := p.users.GetByUsername(ctx, acc.Username)
		if err == nil {
			existing.PasswordHash = hash
			existing.Role = acc.Role
			_ = p.users.Update(ctx, existing)
			continue
		}
		_ = p.users.Create(ctx, &core.User{
			Username:     acc.Username,
			PasswordHash: hash,
			Email:        acc.Username + "@demo.local",
			Role:         acc.Role,
		})
	}
	log.Printf("demo: seeded %d demo accounts (password %q)", len(demoAccounts), demoPassword)

	p.seedDemoData()
}

// seedDemoData wires a realistic ownership hierarchy so the role model is
// visible in demo mode: the customer belongs to the reseller, the customer
// owns a subscription, and each of admin/customer gets one sample domain. It
// is idempotent — safe to run on every --demo start. All inserts are raw
// rows (no orchestration), enough to exercise ownership filtering in the UI.
//
// seedDemoData, rol modelinin demo modunda görünür olması için gerçekçi bir
// sahiplik hiyerarşisi kurar: müşteri bayiye bağlıdır, müşterinin bir aboneliği
// vardır ve admin/müşteriden her biri birer örnek domain alır. Bağımsızdır —
// her --demo başlangıcında güvenle çalışır.
func (p *Panel) seedDemoData() {
	ctx := context.Background()
	db := p.db.GetDB()

	reseller, err1 := p.users.GetByUsername(ctx, "reseller")
	customer, err2 := p.users.GetByUsername(ctx, "customer")
	if err1 != nil || err2 != nil {
		return
	}

	// Customer belongs to the reseller (the hierarchy edge).
	// Müşteri bayiye bağlıdır (hiyerarşi kenarı).
	if _, err := db.ExecContext(ctx, `UPDATE users SET parent_id = ? WHERE id = ?`, reseller.ID, customer.ID); err != nil {
		log.Printf("demo: parent link failed: %v", err)
	}

	// Ensure the customer owns a subscription.
	// Müşterinin bir aboneliği olduğundan emin ol.
	var subID int
	err := db.QueryRowContext(ctx, `SELECT id FROM subscriptions WHERE owner_id = ? ORDER BY id LIMIT 1`, customer.ID).Scan(&subID)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := db.ExecContext(ctx,
			`INSERT INTO subscriptions (owner_id, name, max_domains, max_databases, status) VALUES (?, 'Demo Customer Plan', 25, 25, 'active')`,
			customer.ID)
		if err != nil {
			log.Printf("demo: subscription seed failed: %v", err)
			return
		}
		id, _ := res.LastInsertId()
		subID = int(id)
	}

	// One domain under admin (subscription 1) and one under the customer, so
	// each role sees a different slice. Idempotent via the UNIQUE name.
	// Admin (abonelik 1) altında bir, müşteri altında bir domain; böylece her
	// rol farklı bir dilim görür. UNIQUE ad sayesinde bağımsızdır.
	_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO domains (subscription_id, name, status) VALUES (1, 'admin-site.local', 'active')`)
	_, _ = db.ExecContext(ctx, `INSERT OR IGNORE INTO domains (subscription_id, name, status) VALUES (?, 'customer-site.local', 'active')`, subID)

	// Demo sites use the exact same identity-derived layout as production.
	// This keeps the development shortcut from becoming a second source of
	// filesystem truth.
	for _, name := range []string{"admin-site.local", "customer-site.local"} {
		var domID, subscriptionID int
		if err := db.QueryRowContext(ctx,
			`SELECT id, subscription_id FROM domains WHERE name = ?`, name,
		).Scan(&domID, &subscriptionID); err != nil {
			continue
		}
		docroot, err := hostingpath.DocumentRoot(subscriptionID, domID)
		if err != nil {
			log.Printf("demo: derive docroot for %s: %v", name, err)
			continue
		}
		if err := os.MkdirAll(docroot, 0o755); err != nil {
			log.Printf("demo: mkdir %s: %v", docroot, err)
			continue
		}
		indexPath := filepath.Join(docroot, "index.html")
		if _, err := os.Stat(indexPath); os.IsNotExist(err) {
			_ = os.WriteFile(indexPath, []byte("<!doctype html>\n<title>"+name+"</title>\n<h1>"+name+"</h1>\n<p>CelikPanel demo site.</p>\n"), 0o644)
		}
		_, _ = db.ExecContext(ctx, `
			INSERT INTO sites (domain_id, document_root, web_server, php_version)
			SELECT ?, ?, 'nginx', '8.3'
			WHERE NOT EXISTS (SELECT 1 FROM sites WHERE domain_id = ?)`,
			domID, docroot, domID)
	}

	log.Printf("demo: seeded ownership hierarchy (customer→reseller, subscription, 2 domains + identity-derived docroots)")
}

// handleDemoAccounts lists the demo credentials — but only in demo mode.
// handleDemoAccounts, demo kimlik bilgilerini listeler — yalnızca demo modunda.
func (p *Panel) handleDemoAccounts(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	type demoCred struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	creds := []demoCred{}
	if p.demoMode {
		for _, acc := range demoAccounts {
			creds = append(creds, demoCred{Username: acc.Username, Password: demoPassword, Role: acc.Role})
		}
	}
	_ = json.NewEncoder(w).Encode(creds)
}
