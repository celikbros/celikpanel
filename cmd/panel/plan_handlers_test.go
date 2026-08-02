package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func planAdminRequest(method, target, body string) *http.Request {
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	return req.WithContext(context.WithValue(
		req.Context(), callerKey, &Caller{ID: 1, Role: roleAdmin},
	))
}

func validPlanJSON(name string) string {
	return fmt.Sprintf(`{
		"name": %q,
		"max_domains": 5,
		"max_databases": 10,
		"max_email_accounts": 20,
		"disk_quota_mb": 4096,
		"bandwidth_quota_mb": 8192
	}`, name)
}

func seedPlanWithSubscriber(t *testing.T, p *Panel) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	userResult, err := p.db.GetDB().ExecContext(ctx, `
		INSERT INTO users (username, password_hash, email, role, status)
		VALUES ('plan-owner', 'test-hash', 'plan-owner@example.test', 'customer', 'active')`)
	if err != nil {
		t.Fatalf("insert plan owner: %v", err)
	}
	userID, err := userResult.LastInsertId()
	if err != nil {
		t.Fatalf("read plan owner id: %v", err)
	}
	planResult, err := p.db.GetDB().ExecContext(ctx, `
		INSERT INTO service_plans
			(name, max_domains, max_databases, max_email_accounts, disk_quota_mb, bandwidth_quota_mb)
		VALUES ('Original plan', 2, 3, 4, 1024, 2048)`)
	if err != nil {
		t.Fatalf("insert service plan: %v", err)
	}
	planID, err := planResult.LastInsertId()
	if err != nil {
		t.Fatalf("read service plan id: %v", err)
	}
	subscriptionResult, err := p.db.GetDB().ExecContext(ctx, `
		INSERT INTO subscriptions
			(owner_id, name, max_domains, max_databases, max_email_accounts,
			 disk_quota_mb, bandwidth_quota_mb, plan_id)
		VALUES (?, 'Plan subscriber', 2, 3, 4, 1024, 2048, ?)`, userID, planID)
	if err != nil {
		t.Fatalf("insert plan subscription: %v", err)
	}
	subscriptionID, err := subscriptionResult.LastInsertId()
	if err != nil {
		t.Fatalf("read plan subscription id: %v", err)
	}
	return planID, subscriptionID
}

func TestPlanHandlerRejectsMalformedAndNegativeLimits(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{
			name: "negative bandwidth",
			body: `{"name":"Invalid","max_domains":1,"max_databases":0,"max_email_accounts":0,"disk_quota_mb":1,"bandwidth_quota_mb":-1}`,
		},
		{
			name: "unknown field",
			body: `{"name":"Invalid","max_domains":1,"max_databases":0,"max_email_accounts":0,"disk_quota_mb":1,"bandwidth_quota_mb":0,"surprise":true}`,
		},
		{
			name: "trailing document",
			body: validPlanJSON("Invalid") + ` {}`,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			p := newDNSPanelForTest(t)
			recorder := httptest.NewRecorder()
			p.handlePlans(recorder, planAdminRequest(http.MethodPost, "/api/v1/plans", testCase.body))
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusBadRequest, recorder.Body.String())
			}
			var count int
			if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM service_plans`).Scan(&count); err != nil {
				t.Fatal(err)
			}
			if count != 0 {
				t.Fatalf("invalid request created %d service plans", count)
			}
		})
	}
}

func TestPlanHandlerUpdateRollsBackWhenSubscriberRefreshFails(t *testing.T) {
	p := newDNSPanelForTest(t)
	planID, subscriptionID := seedPlanWithSubscriber(t, p)
	if _, err := p.db.GetDB().Exec(fmt.Sprintf(`
		CREATE TRIGGER reject_subscription_plan_refresh
		BEFORE UPDATE ON subscriptions WHEN OLD.id = %d
		BEGIN SELECT RAISE(ABORT, 'forced subscription refresh failure'); END`, subscriptionID)); err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}

	recorder := httptest.NewRecorder()
	p.handlePlanByID(recorder, planAdminRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/plans/%d", planID),
		validPlanJSON("Changed plan"),
	))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}

	var planName string
	var planMaxDomains int
	if err := p.db.GetDB().QueryRow(
		`SELECT name, max_domains FROM service_plans WHERE id = ?`, planID,
	).Scan(&planName, &planMaxDomains); err != nil {
		t.Fatal(err)
	}
	if planName != "Original plan" || planMaxDomains != 2 {
		t.Fatalf("plan survived a partial update: name=%q max_domains=%d", planName, planMaxDomains)
	}
	var subscriptionMaxDomains int
	if err := p.db.GetDB().QueryRow(
		`SELECT max_domains FROM subscriptions WHERE id = ?`, subscriptionID,
	).Scan(&subscriptionMaxDomains); err != nil {
		t.Fatal(err)
	}
	if subscriptionMaxDomains != 2 {
		t.Fatalf("subscription quota changed despite rollback: %d", subscriptionMaxDomains)
	}
}

func TestPlanHandlerUpdateAtomicallyRefreshesSubscriber(t *testing.T) {
	p := newDNSPanelForTest(t)
	planID, subscriptionID := seedPlanWithSubscriber(t, p)
	recorder := httptest.NewRecorder()
	p.handlePlanByID(recorder, planAdminRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/plans/%d", planID),
		validPlanJSON("Updated plan"),
	))
	if recorder.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusOK, recorder.Body.String())
	}
	var planName string
	var planMaxDomains, subscriptionMaxDomains int
	if err := p.db.GetDB().QueryRow(
		`SELECT name, max_domains FROM service_plans WHERE id = ?`, planID,
	).Scan(&planName, &planMaxDomains); err != nil {
		t.Fatal(err)
	}
	if err := p.db.GetDB().QueryRow(
		`SELECT max_domains FROM subscriptions WHERE id = ?`, subscriptionID,
	).Scan(&subscriptionMaxDomains); err != nil {
		t.Fatal(err)
	}
	if planName != "Updated plan" || planMaxDomains != 5 || subscriptionMaxDomains != 5 {
		t.Fatalf("atomic plan refresh mismatch: name=%q plan=%d subscription=%d", planName, planMaxDomains, subscriptionMaxDomains)
	}
}

func TestPlanHandlerDeleteDistinguishesConflictAndMissingPlan(t *testing.T) {
	p := newDNSPanelForTest(t)
	planID, _ := seedPlanWithSubscriber(t, p)

	conflict := httptest.NewRecorder()
	p.handlePlanByID(conflict, planAdminRequest(
		http.MethodDelete, fmt.Sprintf("/api/v1/plans/%d", planID), "",
	))
	if conflict.Code != http.StatusConflict {
		t.Fatalf("conflict status = %d, want %d; body=%s", conflict.Code, http.StatusConflict, conflict.Body.String())
	}
	var count int
	if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM service_plans WHERE id = ?`, planID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatal("subscribed plan was deleted")
	}

	missing := httptest.NewRecorder()
	p.handlePlanByID(missing, planAdminRequest(http.MethodDelete, "/api/v1/plans/999999", ""))
	if missing.Code != http.StatusNotFound {
		t.Fatalf("missing status = %d, want %d; body=%s", missing.Code, http.StatusNotFound, missing.Body.String())
	}
}

func TestPlanHandlerListFailsClosedOnCorruptQuota(t *testing.T) {
	p := newDNSPanelForTest(t)
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO service_plans
			(name, max_domains, max_databases, max_email_accounts, disk_quota_mb, bandwidth_quota_mb)
		VALUES ('Corrupt plan', 'not-an-integer', 1, 1, 1, 1)`); err != nil {
		t.Fatalf("insert corrupt fixture: %v", err)
	}
	recorder := httptest.NewRecorder()
	p.handlePlans(recorder, planAdminRequest(http.MethodGet, "/api/v1/plans", ""))
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusInternalServerError, recorder.Body.String())
	}
}
