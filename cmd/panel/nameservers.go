package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// The server's nameserver pair — one pair for the whole machine, not one pair
// per hosted domain.
//
// This exists because the panel had it backwards. The zone template wrote
// `ns1.<domain>` and `ns2.<domain>` into every zone it created, which quietly
// declared each customer domain to be its own nameserver. The operator caught
// it immediately on the screen that showed them (25 Jul):
//
//	"the server is boston.celikhost.com — how can the nameservers be
//	 ns1.biovision.health and ns2.biovision.health? there is a logic error
//	 somewhere."
//
// There was. A hosting server has ONE nameserver pair. You register glue for
// it ONCE at the registrar that holds the panel's own domain, and after that
// every hosted domain is connected by simply pointing at those names — no glue
// per domain, no child nameservers per customer. Naming each zone after itself
// is the "vanity nameserver" feature some hosts sell as an extra; making it
// the default meant every single domain needed registrar work that should
// never have been asked for.
//
// Sunucunun ad sunucusu çifti — makinenin tamamı için tek çift, barındırılan
// alan adı başına bir çift değil.
//
// Bu, panelin işi tersinden yapması yüzünden var. Zone şablonu oluşturduğu her
// zone'a `ns1.<alanadı>` ve `ns2.<alanadı>` yazıyor, böylece her müşteri alan
// adını sessizce kendi ad sunucusu ilan ediyordu. Operatör bunu, kendisine
// gösterildiği ekranda anında yakaladı (25 Tem):
//
//	"sunucu boston.celikhost.com — ad sunucuları nasıl ns1.biovision.health ve
//	 ns2.biovision.health olabilir? bir yerlerde bir mantık hatası var."
//
// Vardı. Bir barındırma sunucusunun TEK ad sunucusu çifti olur. Glue kaydını,
// panelin kendi alan adını tutan kayıtçıda BİR KEZ yaptırırsınız; ondan sonra
// barındırılan her alan adı yalnızca o adları göstererek bağlanır — alan adı
// başına glue yok, müşteri başına alt ad sunucusu yok. Her zone'u kendi adıyla
// adlandırmak, bazı barındırıcıların ek olarak sattığı "vanity nameserver"
// özelliğidir; onu varsayılan yapmak, hiç istenmemesi gereken bir kayıtçı
// işini her alan adına yüklemek demekti.

const (
	settingNS1 = "nameserver1"
	settingNS2 = "nameserver2"
)

var validHostname = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)

// panelBaseDomain is the panel host's own domain: boston.celikhost.com →
// celikhost.com. This is what the default nameserver names are built from,
// because it is the domain whose registrar entry the operator already
// controls.
// panelBaseDomain, panel makinesinin kendi alan adıdır: boston.celikhost.com →
// celikhost.com. Varsayılan ad sunucusu adları bundan kurulur, çünkü kayıtçı
// kaydını operatörün zaten yönettiği alan adı odur.
func panelBaseDomain() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return baseDomainOf(h)
}

// baseDomainOf strips the host label: boston.celikhost.com → celikhost.com.
// Separated so the rule is testable without a machine name.
// baseDomainOf makine etiketini atar: boston.celikhost.com → celikhost.com.
// Kural makine adı olmadan test edilebilsin diye ayrıdır.
func baseDomainOf(hostname string) string {
	h := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	parts := strings.Split(h, ".")
	if len(parts) < 2 {
		return "" // an unqualified name tells us nothing
	}
	return strings.Join(parts[len(parts)-2:], ".")
}

// serverNameservers returns the pair every hosted zone should delegate to.
// Configured value wins; otherwise it is derived from the panel's own domain;
// if even that is unknown the pair is empty and the UI says so rather than
// inventing names.
// serverNameservers, barındırılan her zone'un devredeceği çifti döndürür.
// Ayarlanmış değer kazanır; yoksa panelin kendi alan adından türetilir; o da
// bilinmiyorsa çift boştur ve arayüz ad uydurmak yerine bunu söyler.
func (p *Panel) serverNameservers(ctx context.Context) (string, string) {
	ns1 := strings.TrimSpace(p.setting(ctx, settingNS1))
	ns2 := strings.TrimSpace(p.setting(ctx, settingNS2))
	if ns1 != "" && ns2 != "" {
		return ns1, ns2
	}
	if base := panelBaseDomain(); base != "" {
		return "ns1." + base, "ns2." + base
	}
	return "", ""
}

func (p *Panel) setting(ctx context.Context, key string) string {
	var v string
	_ = p.db.GetDB().QueryRowContext(ctx, `SELECT value FROM panel_settings WHERE key = ?`, key).Scan(&v)
	return v
}

func (p *Panel) setSetting(ctx context.Context, key, value string) error {
	_, err := p.db.GetDB().ExecContext(ctx,
		`INSERT INTO panel_settings (key, value, updated_at) VALUES (?, ?, CURRENT_TIMESTAMP)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = CURRENT_TIMESTAMP`,
		key, value)
	return err
}

type nameserverSettings struct {
	NS1 string `json:"ns1"`
	NS2 string `json:"ns2"`
	// Derived reports whether these are the defaults built from the panel's
	// hostname rather than a deliberate choice — the UI says "we guessed this
	// from your server's name, confirm it" instead of implying it was set.
	// Derived, bunların bilinçli bir seçim değil, panelin makine adından
	// kurulmuş varsayılanlar olup olmadığını bildirir — arayüz "bunu
	// sunucunuzun adından tahmin ettik, doğrulayın" der; ayarlanmış gibi
	// göstermez.
	Derived  bool   `json:"derived"`
	ServerIP string `json:"server_ip"`
}

// handleNameserverSettings serves and updates the pair (admin only: it changes
// what every hosted zone advertises).
// handleNameserverSettings çifti sunar ve günceller (yalnız yönetici: her
// barındırılan zone'un ilan ettiği şeyi değiştirir).
func (p *Panel) handleNameserverSettings(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "admin only")
		return
	}

	switch r.Method {
	case http.MethodGet:
		ns1, ns2 := p.serverNameservers(r.Context())
		json.NewEncoder(w).Encode(nameserverSettings{
			NS1:      ns1,
			NS2:      ns2,
			Derived:  p.setting(r.Context(), settingNS1) == "",
			ServerIP: serverPrimaryIP(),
		})

	case http.MethodPut:
		var req struct {
			NS1 string `json:"ns1"`
			NS2 string `json:"ns2"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request")
			return
		}
		req.NS1 = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(req.NS1, ".")))
		req.NS2 = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(req.NS2, ".")))
		for _, ns := range []string{req.NS1, req.NS2} {
			if !validHostname.MatchString(ns) {
				writeClientError(w, http.StatusBadRequest, "a nameserver must be a full host name, for example ns1.example.com")
				return
			}
		}
		if req.NS1 == req.NS2 {
			writeClientError(w, http.StatusBadRequest, "the two nameservers must have different names")
			return
		}
		if err := p.setSetting(r.Context(), settingNS1, req.NS1); err != nil {
			writeServerError(w, err)
			return
		}
		if err := p.setSetting(r.Context(), settingNS2, req.NS2); err != nil {
			writeServerError(w, err)
			return
		}
		p.audit(r, "settings.nameservers:"+req.NS1+","+req.NS2, "settings", 0)
		json.NewEncoder(w).Encode(nameserverSettings{NS1: req.NS1, NS2: req.NS2, ServerIP: serverPrimaryIP()})

	default:
		writeClientError(w, http.StatusMethodNotAllowed, "method not allowed")
	}
}
