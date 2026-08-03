package main

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

func TestValidateSSLSettingsRequiresCertificate(t *testing.T) {
	previous := SSLSettings{HSTSMaxAge: 31536000}
	if err := validateSSLSettings(SSLSettings{ForceHTTPS: true, HSTSMaxAge: 31536000}, previous, false); err == nil {
		t.Fatal("validateSSLSettings() allowed HTTPS redirect without a certificate")
	}
	if err := validateSSLSettings(SSLSettings{ForceHTTPS: true, HSTSEnabled: true, HSTSMaxAge: 31536000}, previous, false); err == nil {
		t.Fatal("validateSSLSettings() allowed HSTS without a certificate")
	}
	if err := validateSSLSettings(
		SSLSettings{HSTSMaxAge: 31536000},
		SSLSettings{ForceHTTPS: true, HSTSMaxAge: 31536000},
		false,
	); err != nil {
		t.Fatalf("validateSSLSettings() blocked disabling HTTPS without a usable certificate: %v", err)
	}
}

func TestValidateSSLSettingsBoundsHSTSMaxAge(t *testing.T) {
	for _, maxAge := range []int{-1, 63072001} {
		if err := validateSSLSettings(SSLSettings{HSTSMaxAge: maxAge}, SSLSettings{}, true); err == nil {
			t.Fatalf("validateSSLSettings() accepted max-age %d", maxAge)
		}
	}
	if err := validateSSLSettings(
		SSLSettings{ForceHTTPS: true, HSTSEnabled: true, HSTSMaxAge: 31536000},
		SSLSettings{},
		true,
	); err != nil {
		t.Fatalf("validateSSLSettings() rejected valid settings: %v", err)
	}
}

func TestNextHSTSRetirementUsesPreviouslyAdvertisedMaxAge(t *testing.T) {
	now := time.Date(2026, time.July, 27, 4, 5, 6, 789, time.UTC)
	previous := SSLSettings{
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	}
	next := SSLSettings{
		ForceHTTPS:  true,
		HSTSEnabled: false,
		HSTSMaxAge:  86400,
	}

	got := nextHSTSRetirement(previous, next, now)
	want := time.Date(2026, time.July, 27, 4, 10, 6, 0, time.UTC)
	if got == nil || !got.Equal(want) {
		t.Fatalf("retire-after = %v, want %s", got, want)
	}
}

func TestNextHSTSRetirementPreservesLaterWindowWhenHSTSIsReenabled(t *testing.T) {
	now := time.Date(2026, time.July, 27, 4, 5, 6, 0, time.UTC)
	retireAfter := now.Add(24 * time.Hour)
	previous := SSLSettings{
		ForceHTTPS:      true,
		HSTSEnabled:     false,
		HSTSMaxAge:      300,
		HSTSRetireAfter: &retireAfter,
	}
	next := SSLSettings{
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	}

	got := nextHSTSRetirement(previous, next, now)
	if got == nil || !got.Equal(retireAfter) {
		t.Fatalf("re-enabled HSTS retire-after = %v, want preserved %s", got, retireAfter)
	}
}

func TestNextHSTSRetirementDoesNotShortenWhenMaxAgeDecreases(t *testing.T) {
	now := time.Date(2026, time.July, 27, 4, 5, 6, 0, time.UTC)
	previous := SSLSettings{
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  31536000,
	}
	next := SSLSettings{
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	}
	got := nextHSTSRetirement(previous, next, now)
	want := now.Add(31536000 * time.Second)
	if got == nil || !got.Equal(want) {
		t.Fatalf("decreased max-age retire-after = %v, want %s", got, want)
	}
}

func TestSSLDurableContextIgnoresBrowserCancellationButIsBounded(t *testing.T) {
	parent, cancelParent := context.WithCancel(context.Background())
	cancelParent()

	ctx, cancel := sslDurableContext(parent)
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("durable context inherited browser cancellation: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("durable context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= sslMutationTimeout-time.Second || remaining > sslMutationTimeout {
		t.Fatalf("durable context deadline is %s away, want approximately %s", remaining, sslMutationTimeout)
	}
}

func TestSSLCompensationContextIsDetachedAndBounded(t *testing.T) {
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	cancelRequest()
	if requestCtx.Err() == nil {
		t.Fatal("request context was not canceled")
	}

	ctx, cancel := sslCompensationContext()
	defer cancel()
	if err := ctx.Err(); err != nil {
		t.Fatalf("compensation context inherited request cancellation: %v", err)
	}
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("compensation context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining <= 29*time.Second || remaining > 30*time.Second {
		t.Fatalf(
			"compensation context deadline is %s away, want approximately 30s",
			remaining,
		)
	}
}

func multipartCertificateRequest(t *testing.T, fields map[string][]byte) *http.Request {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for name, content := range fields {
		part, err := writer.CreateFormFile(name, name+".pem")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(content); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("POST", "/ssl/upload", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(512 << 10); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if request.MultipartForm != nil {
			request.MultipartForm.RemoveAll()
		}
	})
	return request
}

func TestReadCustomCertificatePartEnforcesHardLimit(t *testing.T) {
	request := multipartCertificateRequest(t, map[string][]byte{
		"certificate": bytes.Repeat([]byte("x"), 33),
	})
	if _, err := readCustomCertificatePart(request, "certificate", true, 32); err == nil {
		t.Fatal("oversized certificate part was accepted")
	}
}

func TestReadCustomCertificatePartHandlesRequiredAndOptionalParts(t *testing.T) {
	request := multipartCertificateRequest(t, map[string][]byte{
		"certificate": []byte("certificate"),
	})
	got, err := readCustomCertificatePart(request, "certificate", true, 32)
	if err != nil {
		t.Fatalf("read required certificate: %v", err)
	}
	if string(got) != "certificate" {
		t.Fatalf("certificate = %q", got)
	}
	optional, err := readCustomCertificatePart(request, "chain", false, 32)
	if err != nil {
		t.Fatalf("read missing optional chain: %v", err)
	}
	if optional != nil {
		t.Fatalf("missing optional chain = %q, want nil", optional)
	}
}

func newSSLStateFixture(t *testing.T) (*Panel, int, int64) {
	t.Helper()
	p := newDNSPanelForTest(t)
	ctx := context.Background()
	db := p.db.GetDB()

	userResult, err := db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, email, role)
		VALUES ('ssl-owner', 'hash', 'ssl-owner@example.test', 'customer')`)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("user id: %v", err)
	}
	subscriptionResult, err := db.ExecContext(ctx, `
		INSERT INTO subscriptions (owner_id, name)
		VALUES (?, 'SSL test')`, userID)
	if err != nil {
		t.Fatalf("insert subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("subscription id: %v", err)
	}
	domainResult, err := db.ExecContext(ctx, `
		INSERT INTO domains (subscription_id, name)
		VALUES (?, 'ssl-state.example')`, subscriptionID)
	if err != nil {
		t.Fatalf("insert domain: %v", err)
	}
	domainID64, err := domainResult.LastInsertId()
	if err != nil {
		t.Fatalf("domain id: %v", err)
	}
	domainID := int(domainID64)
	documentRoot, err := hostingpath.DocumentRoot(int(subscriptionID), domainID)
	if err != nil {
		t.Fatalf("derive document root: %v", err)
	}
	if _, err := db.ExecContext(ctx, `
		INSERT INTO sites (
			domain_id, document_root, ssl_enabled, ssl_type,
			ssl_cert_path, ssl_key_path, force_https, hsts_enabled, hsts_max_age
		) VALUES (?, ?, true, 'custom',
		          '/certs/old/fullchain.pem', '/certs/old/privkey.pem',
		          true, true, 300)`, domainID, documentRoot); err != nil {
		t.Fatalf("insert site: %v", err)
	}
	oldCertificateResult, err := db.ExecContext(ctx, `
		INSERT INTO ssl_certificates (
			domain_id, type, cert_path, key_path, chain_path,
			issuer, subject, issued_at, expires_at,
			auto_renew, secure_mail, status
		) VALUES (?, 'custom', '/certs/old/fullchain.pem', '/certs/old/privkey.pem',
		          '/certs/old/chain.pem', 'Old CA', 'ssl-state.example',
		          '2026-01-01T00:00:00Z', '2027-01-01T00:00:00Z',
		          false, true, 'active')`, domainID)
	if err != nil {
		t.Fatalf("insert old certificate: %v", err)
	}
	oldCertificateID, err := oldCertificateResult.LastInsertId()
	if err != nil {
		t.Fatalf("old certificate id: %v", err)
	}
	return p, domainID, oldCertificateID
}

func TestCertificateActivationRollbackRestoresPreviousState(t *testing.T) {
	p, domainID, oldCertificateID := newSSLStateFixture(t)
	ctx := context.Background()

	activation, err := p.activateCertificate(ctx, domainID, certificateInstall{
		Type:       "custom",
		CertPath:   "/certs/new/fullchain.pem",
		KeyPath:    "/certs/new/privkey.pem",
		ChainPath:  "/certs/new/chain.pem",
		Issuer:     "New CA",
		Subject:    "ssl-state.example",
		IssuedAt:   time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2027, time.July, 27, 0, 0, 0, 0, time.UTC),
		AutoRenew:  false,
		SecureMail: false,
	})
	if err != nil {
		t.Fatalf("activate certificate: %v", err)
	}

	assertCertificateStatus(t, p, oldCertificateID, "revoked")
	assertCertificateStatus(t, p, activation.NewCertID, "active")
	assertCertificateRenewalStatus(
		t, p, activation.NewCertID, sslPendingActivation,
	)
	assertSiteSSLState(t, p, domainID, siteSSLState{
		Enabled:     true,
		Type:        "custom",
		CertPath:    sql.NullString{String: "/certs/new/fullchain.pem", Valid: true},
		KeyPath:     sql.NullString{String: "/certs/new/privkey.pem", Valid: true},
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	})

	if err := p.rollbackCertificateActivation(ctx, activation); err != nil {
		t.Fatalf("rollback activation: %v", err)
	}
	assertCertificateStatus(t, p, oldCertificateID, "active")
	var newCount int
	if err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT COUNT(*) FROM ssl_certificates WHERE id = ?`, activation.NewCertID).
		Scan(&newCount); err != nil {
		t.Fatalf("count rolled-back certificate: %v", err)
	}
	if newCount != 0 {
		t.Fatalf("rolled-back certificate count = %d, want 0", newCount)
	}
	assertSiteSSLState(t, p, domainID, siteSSLState{
		Enabled:     true,
		Type:        "custom",
		CertPath:    sql.NullString{String: "/certs/old/fullchain.pem", Valid: true},
		KeyPath:     sql.NullString{String: "/certs/old/privkey.pem", Valid: true},
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	})
}

func TestPostCommitPendingWritesIgnoreCanceledOrExpiredCaller(t *testing.T) {
	p, domainID, _ := newSSLStateFixture(t)
	activation, err := p.activateCertificate(
		context.Background(),
		domainID,
		certificateInstall{
			Type:      "custom",
			CertPath:  "/certs/detached/fullchain.pem",
			KeyPath:   "/certs/detached/privkey.pem",
			ChainPath: "/certs/detached/chain.pem",
			Issuer:    "Detached CA",
			Subject:   "ssl-state.example",
			IssuedAt: time.Date(
				2026, time.July, 27, 0, 0, 0, 0, time.UTC,
			),
			ExpiresAt: time.Date(
				2027, time.July, 27, 0, 0, 0, 0, time.UTC,
			),
		},
	)
	if err != nil {
		t.Fatalf("activate certificate: %v", err)
	}

	expiredCtx, cancelExpired := context.WithDeadline(
		context.Background(), time.Now().Add(-time.Second),
	)
	defer cancelExpired()
	if err := p.markCertificatePendingDetached(
		expiredCtx, domainID, sslPendingDependents, false,
	); err != nil {
		t.Fatalf("mark pending after caller deadline: %v", err)
	}
	assertCertificateRenewalStatus(
		t, p, activation.NewCertID, sslPendingDependents,
	)

	canceledCtx, cancelCanceled := context.WithCancel(context.Background())
	cancelCanceled()
	if err := p.clearCertificatePendingDetached(
		canceledCtx, domainID,
	); err != nil {
		t.Fatalf("clear pending after caller cancellation: %v", err)
	}
	assertCertificateRenewalStatus(t, p, activation.NewCertID, "")

	const attempt = "2026-07-27T14:15:16Z"
	if err := p.markCertificatePendingDetached(
		canceledCtx, domainID, sslPendingActivation, false,
	); err != nil {
		t.Fatalf("restore pending after caller cancellation: %v", err)
	}
	if err := p.completeCertificateRenewal(
		expiredCtx, activation.NewCertID, attempt,
	); err != nil {
		t.Fatalf("complete renewal after caller deadline: %v", err)
	}
	assertCertificateRenewalStatus(t, p, activation.NewCertID, "renewed")
	var lastAttempt string
	if err := p.db.GetDB().QueryRow(`
		SELECT COALESCE(last_renewal_attempt, '')
		FROM ssl_certificates WHERE id = ?`, activation.NewCertID).
		Scan(&lastAttempt); err != nil {
		t.Fatalf("read completed renewal attempt: %v", err)
	}
	if lastAttempt != attempt {
		t.Fatalf("last renewal attempt = %q, want %q", lastAttempt, attempt)
	}
}

func TestRenewalActivationFailurePersistsNewCertificateForRetry(t *testing.T) {
	p, domainID, oldCertificateID := newSSLStateFixture(t)
	ctx := context.Background()

	activation, err := p.activateCertificate(ctx, domainID, certificateInstall{
		Type:       "letsencrypt",
		CertPath:   "/certs/new/fullchain.pem",
		KeyPath:    "/certs/new/privkey.pem",
		ChainPath:  "/certs/new/chain.pem",
		Issuer:     "Let's Encrypt",
		Subject:    "ssl-state.example",
		IssuedAt:   time.Date(2026, time.July, 27, 0, 0, 0, 0, time.UTC),
		ExpiresAt:  time.Date(2026, time.October, 25, 0, 0, 0, 0, time.UTC),
		AutoRenew:  true,
		SecureMail: true,
	})
	if err != nil {
		t.Fatalf("activate renewed certificate: %v", err)
	}

	const attempt = "2026-07-27T12:34:56Z"
	if err := p.persistCertificateRenewalPending(
		domainID,
		activation.NewCertID,
		attempt,
		sslPendingActivation,
		true,
	); err != nil {
		t.Fatalf("persist pending renewal: %v", err)
	}

	assertCertificateStatus(t, p, oldCertificateID, "revoked")
	assertCertificateStatus(t, p, activation.NewCertID, "active")
	assertSiteSSLState(t, p, domainID, siteSSLState{
		Enabled:     false,
		Type:        "letsencrypt",
		CertPath:    sql.NullString{String: "/certs/new/fullchain.pem", Valid: true},
		KeyPath:     sql.NullString{String: "/certs/new/privkey.pem", Valid: true},
		ForceHTTPS:  true,
		HSTSEnabled: true,
		HSTSMaxAge:  300,
	})

	var renewalStatus, lastAttempt string
	if err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(renewal_status, ''), COALESCE(last_renewal_attempt, '')
		FROM ssl_certificates WHERE id = ?`, activation.NewCertID).
		Scan(&renewalStatus, &lastAttempt); err != nil {
		t.Fatalf("read pending renewal state: %v", err)
	}
	if renewalStatus != sslPendingActivation {
		t.Fatalf("renewal status = %q, want %q", renewalStatus, sslPendingActivation)
	}
	if lastAttempt != attempt {
		t.Fatalf("last renewal attempt = %q, want %q", lastAttempt, attempt)
	}
}

func TestRenewalFailurePersistsAfterJobContextExpires(t *testing.T) {
	p, _, certificateID := newSSLStateFixture(t)
	jobCtx, cancel := context.WithCancel(context.Background())
	cancel()

	const attempt = "2026-07-27T13:14:15Z"
	p.recordCertificateRenewalFailure(
		jobCtx,
		int(certificateID),
		"ssl-state.example",
		attempt,
		"agent operation exceeded its deadline",
	)

	var renewalStatus, lastAttempt string
	if err := p.db.GetDB().QueryRow(`
		SELECT COALESCE(renewal_status, ''), COALESCE(last_renewal_attempt, '')
		FROM ssl_certificates WHERE id = ?`, certificateID).
		Scan(&renewalStatus, &lastAttempt); err != nil {
		t.Fatalf("read failed renewal state: %v", err)
	}
	if renewalStatus != "failed" {
		t.Fatalf("renewal status = %q, want failed", renewalStatus)
	}
	if lastAttempt != attempt {
		t.Fatalf("last renewal attempt = %q, want %q", lastAttempt, attempt)
	}
}

func TestSSLRenewalSettingUpdatesActiveACMECertificate(t *testing.T) {
	p, domainID, certificateID := newSSLStateFixture(t)
	if _, err := p.db.GetDB().Exec(
		`UPDATE ssl_certificates SET type = 'letsencrypt', auto_renew = false WHERE id = ?`,
		certificateID,
	); err != nil {
		t.Fatalf("prepare ACME certificate: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/domains/1/ssl/renewal",
		bytes.NewBufferString(`{"auto_renew":true}`),
	)
	recorder := httptest.NewRecorder()
	p.handleSSLRenewalSetting(recorder, request, domainID)
	if recorder.Code != http.StatusOK {
		t.Fatalf("renewal setting status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var enabled bool
	if err := p.db.GetDB().QueryRow(
		`SELECT auto_renew FROM ssl_certificates WHERE id = ?`,
		certificateID,
	).Scan(&enabled); err != nil {
		t.Fatalf("read auto-renew setting: %v", err)
	}
	if !enabled {
		t.Fatal("auto-renew setting remained disabled")
	}
}

func TestSSLRenewalSettingRejectsCustomCertificate(t *testing.T) {
	p, domainID, certificateID := newSSLStateFixture(t)
	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/domains/1/ssl/renewal",
		bytes.NewBufferString(`{"auto_renew":true}`),
	)
	recorder := httptest.NewRecorder()
	p.handleSSLRenewalSetting(recorder, request, domainID)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("custom renewal setting status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var enabled bool
	if err := p.db.GetDB().QueryRow(
		`SELECT auto_renew FROM ssl_certificates WHERE id = ?`,
		certificateID,
	).Scan(&enabled); err != nil {
		t.Fatalf("read custom auto-renew setting: %v", err)
	}
	if enabled {
		t.Fatal("custom certificate auto-renew was enabled")
	}
}

func TestSSLRenewalSettingRejectsMissingAutoRenew(t *testing.T) {
	p, domainID, certificateID := newSSLStateFixture(t)
	if _, err := p.db.GetDB().Exec(
		`UPDATE ssl_certificates SET type = 'letsencrypt', auto_renew = true WHERE id = ?`,
		certificateID,
	); err != nil {
		t.Fatalf("prepare ACME certificate: %v", err)
	}

	request := httptest.NewRequest(
		http.MethodPut,
		"/api/v1/domains/1/ssl/renewal",
		bytes.NewBufferString(`{}`),
	)
	recorder := httptest.NewRecorder()
	p.handleSSLRenewalSetting(recorder, request, domainID)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("missing auto_renew status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var enabled bool
	if err := p.db.GetDB().QueryRow(
		`SELECT auto_renew FROM ssl_certificates WHERE id = ?`,
		certificateID,
	).Scan(&enabled); err != nil {
		t.Fatalf("read auto-renew setting: %v", err)
	}
	if !enabled {
		t.Fatal("missing auto_renew silently disabled automatic renewal")
	}
}

func TestSSLSettingsDisableAndReenableManageHSTSRetirement(t *testing.T) {
	p, domainID, _ := newSSLStateFixture(t)
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{
			"/certs/old/fullchain.pem": {
				Valid: true, Trusted: true, TrustChecked: true,
				DNSNames: []string{"ssl-state.example", "www.ssl-state.example"},
			},
		},
	}
	attachMailTLSIsolationAgent(t, p, agent)

	beforeDisable := time.Now().UTC()
	disableRequest := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/settings", domainID),
		bytes.NewBufferString(`{"force_https":true,"hsts_enabled":false,"hsts_max_age":300}`),
	)
	disableRecorder := httptest.NewRecorder()
	p.handleSSLSettings(disableRecorder, disableRequest)
	if disableRecorder.Code != http.StatusOK {
		t.Fatalf("disable HSTS status = %d, body = %s", disableRecorder.Code, disableRecorder.Body.String())
	}

	var enabled bool
	var retireAfterRaw sql.NullString
	if err := p.db.GetDB().QueryRow(`
		SELECT hsts_enabled, hsts_retire_after FROM sites WHERE domain_id = ?`,
		domainID,
	).Scan(&enabled, &retireAfterRaw); err != nil {
		t.Fatalf("read disabled HSTS state: %v", err)
	}
	if enabled {
		t.Fatal("HSTS remained enabled")
	}
	retireAfter, err := parseOptionalDBTime(retireAfterRaw)
	if err != nil {
		t.Fatalf("parse HSTS retirement: %v", err)
	}
	if retireAfter == nil {
		t.Fatal("disabling HSTS did not persist a retirement timestamp")
	}
	minimum := beforeDisable.Truncate(time.Second).Add(300 * time.Second)
	maximum := time.Now().UTC().Truncate(time.Second).Add(300 * time.Second)
	if retireAfter.Before(minimum) || retireAfter.After(maximum) {
		t.Fatalf("retire-after = %s, want between %s and %s", retireAfter, minimum, maximum)
	}

	reenableRequest := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/settings", domainID),
		bytes.NewBufferString(`{"force_https":true,"hsts_enabled":true,"hsts_max_age":300}`),
	)
	reenableRecorder := httptest.NewRecorder()
	p.handleSSLSettings(reenableRecorder, reenableRequest)
	if reenableRecorder.Code != http.StatusOK {
		t.Fatalf("re-enable HSTS status = %d, body = %s", reenableRecorder.Code, reenableRecorder.Body.String())
	}
	if err := p.db.GetDB().QueryRow(`
		SELECT hsts_enabled, hsts_retire_after FROM sites WHERE domain_id = ?`,
		domainID,
	).Scan(&enabled, &retireAfterRaw); err != nil {
		t.Fatalf("read re-enabled HSTS state: %v", err)
	}
	if !enabled {
		t.Fatal("HSTS remained disabled")
	}
	reEnabledRetireAfter, err := parseOptionalDBTime(retireAfterRaw)
	if err != nil {
		t.Fatalf("parse re-enabled HSTS retirement: %v", err)
	}
	if reEnabledRetireAfter == nil || reEnabledRetireAfter.Before(*retireAfter) {
		t.Fatalf(
			"re-enabling HSTS shortened retirement from %s to %v",
			retireAfter,
			reEnabledRetireAfter,
		)
	}
}

func TestSSLSettingsApplyRollbackRestoresPriorHSTSRetirement(t *testing.T) {
	p, domainID, _ := newSSLStateFixture(t)
	agent := &mailTLSIsolationRPCAgent{
		certificates: map[string]MailTLSInspectRPCResponse{
			"/certs/old/fullchain.pem": {
				Valid: true, Trusted: true, TrustChecked: true,
				DNSNames: []string{"ssl-state.example", "www.ssl-state.example"},
			},
		},
		applyVhostErrorOnce: "forced vhost rejection",
	}
	attachMailTLSIsolationAgent(t, p, agent)

	request := httptest.NewRequest(
		http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/ssl/settings", domainID),
		bytes.NewBufferString(`{"force_https":true,"hsts_enabled":false,"hsts_max_age":300}`),
	)
	recorder := httptest.NewRecorder()
	p.handleSSLSettings(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("settings rollback status = %d, body = %s", recorder.Code, recorder.Body.String())
	}

	var forceHTTPS, hstsEnabled bool
	var maxAge int
	var retireAfter sql.NullString
	if err := p.db.GetDB().QueryRow(`
		SELECT force_https, hsts_enabled, hsts_max_age, hsts_retire_after
		FROM sites WHERE domain_id = ?`, domainID).
		Scan(&forceHTTPS, &hstsEnabled, &maxAge, &retireAfter); err != nil {
		t.Fatalf("read rolled-back settings: %v", err)
	}
	if !forceHTTPS || !hstsEnabled || maxAge != 300 || retireAfter.Valid {
		t.Fatalf(
			"rolled-back settings = https:%t hsts:%t max-age:%d retire:%v",
			forceHTTPS, hstsEnabled, maxAge, retireAfter,
		)
	}
}

func TestCertificateDetachBlocksActiveHSTSRetirement(t *testing.T) {
	p, domainID, certificateID := newSSLStateFixture(t)
	retireAfter := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := p.db.GetDB().Exec(`
		UPDATE sites
		SET force_https = true, hsts_enabled = false, hsts_retire_after = ?
		WHERE domain_id = ?`,
		retireAfter.Format(time.RFC3339), domainID,
	); err != nil {
		t.Fatalf("prepare HSTS retirement: %v", err)
	}

	_, err := p.detachCertificate(context.Background(), domainID)
	var retirementErr *hstsRetirementActiveError
	if !errors.As(err, &retirementErr) {
		t.Fatalf("detach error = %v, want active HSTS retirement", err)
	}
	if !retirementErr.Until.Equal(retireAfter) {
		t.Fatalf("retirement error until = %s, want %s", retirementErr.Until, retireAfter)
	}
	assertCertificateStatus(t, p, certificateID, "active")
}

func TestDomainDeletionBlocksHSTSEnabledHostnameRemoval(t *testing.T) {
	p, domainID, certificateID := newSSLStateFixture(t)
	request := httptest.NewRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/domains/%d", domainID),
		nil,
	)
	recorder := httptest.NewRecorder()
	p.handleDeleteDomain(recorder, request)
	if recorder.Code != http.StatusConflict {
		t.Fatalf("HSTS domain delete status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	assertCertificateStatus(t, p, certificateID, "active")
	var domainCount int
	if err := p.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM domains WHERE id = ?`, domainID,
	).Scan(&domainCount); err != nil {
		t.Fatalf("count guarded domain: %v", err)
	}
	if domainCount != 1 {
		t.Fatalf("guarded domain count = %d, want 1", domainCount)
	}
}

func TestAliasDeletionBlocksActiveHSTSRetirement(t *testing.T) {
	p, domainID, _ := newSSLStateFixture(t)
	retireAfter := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := p.db.GetDB().Exec(`
		UPDATE sites
		SET hsts_enabled = false, hsts_retire_after = ?
		WHERE domain_id = ?`,
		retireAfter.Format(time.RFC3339), domainID,
	); err != nil {
		t.Fatalf("prepare HSTS retirement: %v", err)
	}
	const alias = "retiring-alias.example"
	if _, err := p.addDomainAlias(
		context.Background(),
		domainID,
		alias,
		nil,
		func(context.Context, int) error { return nil },
	); err != nil {
		t.Fatalf("add alias fixture: %v", err)
	}

	err := p.deleteDomainAlias(
		context.Background(),
		domainID,
		alias,
		func(context.Context, int) error { return nil },
	)
	var retirementErr *hstsRetirementActiveError
	if !errors.As(err, &retirementErr) {
		t.Fatalf("alias deletion error = %v, want active HSTS retirement", err)
	}
	var aliasCount int
	if err := p.db.GetDB().QueryRow(`
		SELECT COUNT(*) FROM domain_aliases
		WHERE domain_id = ? AND alias = ?`, domainID, alias).
		Scan(&aliasCount); err != nil {
		t.Fatalf("count guarded alias: %v", err)
	}
	if aliasCount != 1 {
		t.Fatalf("guarded alias count = %d, want 1", aliasCount)
	}
}

func TestCertificateDetachRollbackRestoresPreviousState(t *testing.T) {
	p, domainID, oldCertificateID := newSSLStateFixture(t)
	ctx := context.Background()
	const previousRetirement = "2000-01-01T00:00:00Z"
	if _, err := p.db.GetDB().ExecContext(ctx, `
		UPDATE sites
		SET force_https = false, hsts_enabled = false, hsts_retire_after = ?
		WHERE domain_id = ?`, previousRetirement, domainID); err != nil {
		t.Fatalf("prepare detachable site: %v", err)
	}

	detach, err := p.detachCertificate(ctx, domainID)
	if err != nil {
		t.Fatalf("detach certificate: %v", err)
	}
	assertCertificateStatus(t, p, oldCertificateID, "revoked")
	assertSiteSSLState(t, p, domainID, siteSSLState{
		Enabled:    false,
		Type:       "none",
		HSTSMaxAge: 300,
	})

	if err := p.rollbackCertificateDetach(ctx, detach); err != nil {
		t.Fatalf("rollback detach: %v", err)
	}
	assertCertificateStatus(t, p, oldCertificateID, "active")
	assertSiteSSLState(t, p, domainID, siteSSLState{
		Enabled:         true,
		Type:            "custom",
		CertPath:        sql.NullString{String: "/certs/old/fullchain.pem", Valid: true},
		KeyPath:         sql.NullString{String: "/certs/old/privkey.pem", Valid: true},
		ForceHTTPS:      false,
		HSTSEnabled:     false,
		HSTSMaxAge:      300,
		HSTSRetireAfter: sql.NullString{String: previousRetirement, Valid: true},
	})
}

func assertCertificateStatus(t *testing.T, p *Panel, certificateID int64, want string) {
	t.Helper()
	var got string
	if err := p.db.GetDB().QueryRow(
		`SELECT status FROM ssl_certificates WHERE id = ?`, certificateID).Scan(&got); err != nil {
		t.Fatalf("read certificate %d status: %v", certificateID, err)
	}
	if got != want {
		t.Fatalf("certificate %d status = %q, want %q", certificateID, got, want)
	}
}

func assertCertificateRenewalStatus(
	t *testing.T,
	p *Panel,
	certificateID int64,
	want string,
) {
	t.Helper()
	var got string
	if err := p.db.GetDB().QueryRow(`
		SELECT COALESCE(renewal_status, '')
		FROM ssl_certificates WHERE id = ?`, certificateID).
		Scan(&got); err != nil {
		t.Fatalf(
			"read certificate %d renewal status: %v",
			certificateID,
			err,
		)
	}
	if got != want {
		t.Fatalf(
			"certificate %d renewal status = %q, want %q",
			certificateID,
			got,
			want,
		)
	}
}

func assertSiteSSLState(t *testing.T, p *Panel, domainID int, want siteSSLState) {
	t.Helper()
	got, err := loadSiteSSLState(context.Background(), p.db.GetDB(), domainID)
	if err != nil {
		t.Fatalf("load site SSL state: %v", err)
	}
	if got != want {
		t.Fatalf("site SSL state = %#v, want %#v", got, want)
	}
}
