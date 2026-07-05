package main

import (
	"context"
	"fmt"
	"net/http"
)

// Quota enforcement: subscription limits are checked at every resource
// creation point (the API layer — the UI merely mirrors what the API would
// reject). Admins bypass quotas on their own subscription only in the sense
// that the admin seed subscription is created with effectively unlimited
// values; the checks themselves run for everyone.
//
// Kota uygulaması: abonelik sınırları her kaynak oluşturma noktasında
// denetlenir (API katmanı — arayüz yalnızca API'nin reddedeceğini yansıtır).
// Denetimler herkes için çalışır; admin'in seed aboneliği zaten fiilen
// sınırsız değerlerle kurulmuştur.

type quotaKind string

const (
	quotaDomains quotaKind = "domains"
	quotaDBs     quotaKind = "databases"
	quotaMail    quotaKind = "email accounts"
)

// checkSubscriptionQuota returns nil when one more resource of the given
// kind fits in the subscription, or a descriptive error when the limit is
// reached.
// checkSubscriptionQuota, verilen türden bir kaynak daha aboneliğe
// sığıyorsa nil; sınıra ulaşıldıysa açıklayıcı bir hata döndürür.
func (p *Panel) checkSubscriptionQuota(ctx context.Context, subID int, kind quotaKind) error {
	var limit, used int
	var err error

	switch kind {
	case quotaDomains:
		err = p.db.GetDB().QueryRowContext(ctx, `
			SELECT s.max_domains, (SELECT COUNT(*) FROM domains d WHERE d.subscription_id = s.id)
			FROM subscriptions s WHERE s.id = ?`, subID).Scan(&limit, &used)
	case quotaDBs:
		// databases_v2 is the live table (v1 "databases" is written by a
		// handler whose INSERT does not match the schema — flagged separately).
		// databases_v2 canlı tablodur (v1 "databases"e yazan handler'ın
		// INSERT'ü şemayla uyuşmuyor — ayrıca işaretlendi).
		err = p.db.GetDB().QueryRowContext(ctx, `
			SELECT s.max_databases,
			       (SELECT COUNT(*) FROM databases_v2 db WHERE db.subscription_id = s.id)
			FROM subscriptions s WHERE s.id = ?`, subID).Scan(&limit, &used)
	case quotaMail:
		err = p.db.GetDB().QueryRowContext(ctx, `
			SELECT s.max_email_accounts,
			       (SELECT COUNT(*) FROM email_accounts ea JOIN domains d ON ea.domain_id = d.id WHERE d.subscription_id = s.id)
			FROM subscriptions s WHERE s.id = ?`, subID).Scan(&limit, &used)
	default:
		return nil
	}
	if err != nil {
		return fmt.Errorf("subscription not found")
	}
	if used >= limit {
		return fmt.Errorf("subscription limit reached: %d of %d %s in use", used, limit, kind)
	}
	return nil
}

// domainSubscriptionID resolves the owning subscription of a domain.
// domainSubscriptionID, bir domain'in bağlı olduğu aboneliği çözer.
func (p *Panel) domainSubscriptionID(ctx context.Context, domainID int) (int, error) {
	var subID int
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT subscription_id FROM domains WHERE id = ?`, domainID).Scan(&subID)
	return subID, err
}

// enforceDomainQuota is the HTTP-flavoured guard for per-domain resources:
// resolves the subscription and writes a 409 when the limit is hit.
// enforceDomainQuota, domain-başına kaynaklar için HTTP korumasıdır:
// aboneliği çözer ve sınıra gelindiyse 409 yazar.
func (p *Panel) enforceDomainQuota(w http.ResponseWriter, r *http.Request, domainID int, kind quotaKind) bool {
	subID, err := p.domainSubscriptionID(r.Context(), domainID)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "domain not found")
		return false
	}
	if err := p.checkSubscriptionQuota(r.Context(), subID, kind); err != nil {
		writeClientError(w, http.StatusConflict, err.Error())
		return false
	}
	return true
}
