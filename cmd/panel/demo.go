package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"

	"github.com/alicelik/celikpanel/internal/auth"
	"github.com/alicelik/celikpanel/internal/core"
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
