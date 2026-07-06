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
	quotaDisk    quotaKind = "disk"
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
	case quotaDisk:
		// Disk is a byte comparison, not a count: the gate refuses new
		// resources once the subscription is already at/over its measured
		// usage. Uses the cached per-site measurements (the same numbers the
		// domain pages show); 0 disk_quota_mb means unlimited.
		// Disk sayı değil bayt karşılaştırmasıdır: kapı, abonelik ölçülen
		// kullanımına ulaştıysa/aştıysa yeni kaynağı reddeder. Önbellekli
		// site ölçümlerini kullanır (domain sayfalarının gösterdiği aynı
		// sayılar); disk_quota_mb 0 ise sınırsız.
		var limitMB, usedBytes int64
		err = p.db.GetDB().QueryRowContext(ctx, `
			SELECT s.disk_quota_mb,
			       COALESCE((SELECT SUM(si.disk_usage_bytes)
			                 FROM sites si JOIN domains d ON si.domain_id = d.id
			                 WHERE d.subscription_id = s.id), 0)
			FROM subscriptions s WHERE s.id = ?`, subID).Scan(&limitMB, &usedBytes)
		if err != nil {
			return fmt.Errorf("subscription not found")
		}
		if limitMB <= 0 {
			return nil // unlimited / sınırsız
		}
		if usedBytes >= limitMB*1024*1024 {
			return fmt.Errorf("disk quota reached: %d of %d MB in use", usedBytes/(1024*1024), limitMB)
		}
		return nil
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

// subscriptionUsage is the real, aggregate resource picture for one
// subscription: measured disk against the plan limit plus the counted
// resources. Disk bytes are the sum of the cached per-site measurements.
// subscriptionUsage, bir aboneliğin gerçek, toplu kaynak tablosudur: plan
// limitine karşı ölçülen disk artı sayılan kaynaklar.
type subscriptionUsage struct {
	DiskUsedBytes  int64 `json:"disk_used_bytes"`
	DiskLimitBytes int64 `json:"disk_limit_bytes"` // 0 = unlimited
	Domains        int   `json:"domains"`
	DomainsLimit   int   `json:"domains_limit"`
	Databases      int   `json:"databases"`
	DatabasesLimit int   `json:"databases_limit"`
	MailAccounts   int   `json:"mail_accounts"`
	MailLimit      int   `json:"mail_limit"`
}

func (p *Panel) subscriptionUsageFor(ctx context.Context, subID int) (*subscriptionUsage, error) {
	var u subscriptionUsage
	var diskLimitMB int64
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT
		  s.max_domains, s.max_databases, s.max_email_accounts, s.disk_quota_mb,
		  (SELECT COUNT(*) FROM domains d WHERE d.subscription_id = s.id),
		  (SELECT COUNT(*) FROM databases_v2 db WHERE db.subscription_id = s.id),
		  (SELECT COUNT(*) FROM email_accounts ea JOIN domains d ON ea.domain_id = d.id WHERE d.subscription_id = s.id),
		  COALESCE((SELECT SUM(si.disk_usage_bytes) FROM sites si JOIN domains d ON si.domain_id = d.id WHERE d.subscription_id = s.id), 0)
		FROM subscriptions s WHERE s.id = ?`, subID).Scan(
		&u.DomainsLimit, &u.DatabasesLimit, &u.MailLimit, &diskLimitMB,
		&u.Domains, &u.Databases, &u.MailAccounts, &u.DiskUsedBytes)
	if err != nil {
		return nil, err
	}
	u.DiskLimitBytes = diskLimitMB * 1024 * 1024
	return &u, nil
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
