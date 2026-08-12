package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
)

func createTeamMemberHandlerUser(
	t *testing.T,
	p *Panel,
	role string,
	accountType core.AccountType,
	parentID *int,
	suffix string,
) int {
	t.Helper()
	var storedParentID any
	if parentID != nil {
		storedParentID = *parentID
	}
	result, err := p.db.GetDB().Exec(`
		INSERT INTO users
			(username, password_hash, email, role, parent_id, status, account_type)
		VALUES (?, 'handler-hash', ?, ?, ?, 'active', ?)`,
		"handler-"+suffix,
		"handler-"+suffix+"@example.test",
		role,
		storedParentID,
		accountType,
	)
	if err != nil {
		t.Fatalf("create handler user %s: %v", suffix, err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("handler user %s ID: %v", suffix, err)
	}
	return int(id)
}

func createTeamMemberHandlerScope(t *testing.T, p *Panel, ownerID int, suffix string) (int, int) {
	t.Helper()
	result, err := p.db.GetDB().Exec(
		`INSERT INTO subscriptions (owner_id, name) VALUES (?, ?)`,
		ownerID,
		"handler-subscription-"+suffix,
	)
	if err != nil {
		t.Fatalf("create handler subscription: %v", err)
	}
	subscriptionID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("handler subscription ID: %v", err)
	}
	result, err = p.db.GetDB().Exec(
		`INSERT INTO domains (subscription_id, name) VALUES (?, ?)`,
		subscriptionID,
		"handler-domain-"+suffix+".example.test",
	)
	if err != nil {
		t.Fatalf("create handler domain: %v", err)
	}
	domainID, err := result.LastInsertId()
	if err != nil {
		t.Fatalf("handler domain ID: %v", err)
	}
	return int(subscriptionID), int(domainID)
}

func teamMemberHandlerRequest(method, target, body string, caller *Caller) *http.Request {
	request := httptest.NewRequest(method, target, strings.NewReader(body))
	if caller != nil {
		request = request.WithContext(context.WithValue(request.Context(), callerKey, caller))
	}
	return request
}

func decodeTeamMemberHandlerResponse(t *testing.T, recorder *httptest.ResponseRecorder) core.TeamMember {
	t.Helper()
	var response struct {
		TeamMember core.TeamMember `json:"team_member"`
	}
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode team member response: %v; body=%q", err, recorder.Body.String())
	}
	return response.TeamMember
}

func teamMemberCreateBody(ownerID *int, username string, subscriptionID, domainID int) string {
	request := map[string]any{
		"username": username,
		"email":    username + "@example.test",
		"password": "handler-password",
		"access": map[string]any{
			"subscription_permissions": []map[string]any{{
				"subscription_id": subscriptionID,
				"capability":      "files",
				"mode":            "view",
			}},
			"domain_permissions": []map[string]any{{
				"domain_id":  domainID,
				"capability": "dns",
				"mode":       "manage",
			}},
		},
	}
	if ownerID != nil {
		request["owner_id"] = *ownerID
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}

func TestTeamMemberHandlersCustomerCRUDAndDerivedOwner(t *testing.T) {
	p := newDNSPanelForTest(t)
	ownerID := createTeamMemberHandlerUser(t, p, roleCustomer, core.AccountTypeAccount, nil, "customer-crud")
	subscriptionID, domainID := createTeamMemberHandlerScope(t, p, ownerID, "customer-crud")
	caller := &Caller{ID: ownerID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: ownerID}

	createRecorder := httptest.NewRecorder()
	p.handleTeamMembers(createRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(nil, "customer-member", subscriptionID, domainID),
		caller,
	))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%q", createRecorder.Code, createRecorder.Body.String())
	}
	member := decodeTeamMemberHandlerResponse(t, createRecorder)
	if member.OwnerID != ownerID || member.Username != "customer-member" {
		t.Fatalf("created member = %#v", member)
	}
	if got := member.Access.SubscriptionPermissions; len(got) != 1 || got[0].SubscriptionName != "handler-subscription-customer-crud" {
		t.Fatalf("created subscription permissions = %#v", got)
	}
	if got := member.Access.DomainPermissions; len(got) != 1 || got[0].DomainName != "handler-domain-customer-crud.example.test" {
		t.Fatalf("created domain permissions = %#v", got)
	}

	listRecorder := httptest.NewRecorder()
	p.handleTeamMembers(listRecorder, teamMemberHandlerRequest(http.MethodGet, "/api/v1/team-members", "", caller))
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%q", listRecorder.Code, listRecorder.Body.String())
	}
	var listResponse struct {
		TeamMembers []core.TeamMember `json:"team_members"`
	}
	if err := json.NewDecoder(listRecorder.Body).Decode(&listResponse); err != nil {
		t.Fatal(err)
	}
	if len(listResponse.TeamMembers) != 1 || listResponse.TeamMembers[0].ID != member.ID {
		t.Fatalf("listed members = %#v", listResponse.TeamMembers)
	}

	getRecorder := httptest.NewRecorder()
	p.handleTeamMemberByID(getRecorder, teamMemberHandlerRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/team-members/%d", member.ID),
		"",
		caller,
	))
	if getRecorder.Code != http.StatusOK || decodeTeamMemberHandlerResponse(t, getRecorder).ID != member.ID {
		t.Fatalf("get status/body = %d/%q", getRecorder.Code, getRecorder.Body.String())
	}

	ownerOverrideRecorder := httptest.NewRecorder()
	p.handleTeamMembers(ownerOverrideRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(&ownerID, "owner-override", subscriptionID, domainID),
		caller,
	))
	if ownerOverrideRecorder.Code != http.StatusBadRequest {
		t.Fatalf("customer owner override status = %d", ownerOverrideRecorder.Code)
	}

	queryOverrideRecorder := httptest.NewRecorder()
	p.handleTeamMembers(queryOverrideRecorder, teamMemberHandlerRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/team-members?owner_id=%d", ownerID),
		"",
		caller,
	))
	if queryOverrideRecorder.Code != http.StatusBadRequest {
		t.Fatalf("customer query owner override status = %d", queryOverrideRecorder.Code)
	}

	nonExactRecorder := httptest.NewRecorder()
	p.handleTeamMembers(nonExactRecorder, teamMemberHandlerRequest(http.MethodGet, "/api/v1/team-members/", "", caller))
	if nonExactRecorder.Code != http.StatusNotFound {
		t.Fatalf("non-exact collection route status = %d", nonExactRecorder.Code)
	}
}

func TestTeamMemberHandlersAdminOwnerValidationAndTenantIsolation(t *testing.T) {
	p := newDNSPanelForTest(t)
	adminID := createTeamMemberHandlerUser(t, p, roleAdmin, core.AccountTypeAccount, nil, "admin")
	ownerOneID := createTeamMemberHandlerUser(t, p, roleCustomer, core.AccountTypeAccount, nil, "owner-one")
	ownerTwoID := createTeamMemberHandlerUser(t, p, roleCustomer, core.AccountTypeAccount, nil, "owner-two")
	ownedSubscriptionID, ownedDomainID := createTeamMemberHandlerScope(t, p, ownerOneID, "owner-one")
	foreignSubscriptionID, foreignDomainID := createTeamMemberHandlerScope(t, p, ownerTwoID, "owner-two")
	admin := &Caller{ID: adminID, Role: roleAdmin, AccountType: core.AccountTypeAccount}
	ownerOne := &Caller{ID: ownerOneID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: ownerOneID}
	ownerTwo := &Caller{ID: ownerTwoID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: ownerTwoID}

	missingOwnerRecorder := httptest.NewRecorder()
	p.handleTeamMembers(missingOwnerRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(nil, "admin-missing-owner", ownedSubscriptionID, ownedDomainID),
		admin,
	))
	if missingOwnerRecorder.Code != http.StatusBadRequest {
		t.Fatalf("admin create without owner status = %d", missingOwnerRecorder.Code)
	}

	missingOwnerListRecorder := httptest.NewRecorder()
	p.handleTeamMembers(missingOwnerListRecorder, teamMemberHandlerRequest(http.MethodGet, "/api/v1/team-members", "", admin))
	if missingOwnerListRecorder.Code != http.StatusBadRequest {
		t.Fatalf("admin list without owner status = %d", missingOwnerListRecorder.Code)
	}

	nonexistentOwnerID := 999999
	nonexistentOwnerRecorder := httptest.NewRecorder()
	p.handleTeamMembers(nonexistentOwnerRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(&nonexistentOwnerID, "admin-no-owner", ownedSubscriptionID, ownedDomainID),
		admin,
	))
	if nonexistentOwnerRecorder.Code != http.StatusNotFound {
		t.Fatalf("admin nonexistent owner status = %d, body=%q", nonexistentOwnerRecorder.Code, nonexistentOwnerRecorder.Body.String())
	}

	foreignScopeRecorder := httptest.NewRecorder()
	p.handleTeamMembers(foreignScopeRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(&ownerOneID, "admin-foreign-scope", foreignSubscriptionID, foreignDomainID),
		admin,
	))
	if foreignScopeRecorder.Code != http.StatusNotFound {
		t.Fatalf("admin foreign scope status = %d, body=%q", foreignScopeRecorder.Code, foreignScopeRecorder.Body.String())
	}

	createRecorder := httptest.NewRecorder()
	p.handleTeamMembers(createRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(&ownerOneID, "admin-created-member", ownedSubscriptionID, ownedDomainID),
		admin,
	))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("admin create status = %d, body=%q", createRecorder.Code, createRecorder.Body.String())
	}
	member := decodeTeamMemberHandlerResponse(t, createRecorder)

	adminListRecorder := httptest.NewRecorder()
	p.handleTeamMembers(adminListRecorder, teamMemberHandlerRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/team-members?owner_id=%d", ownerOneID),
		"",
		admin,
	))
	if adminListRecorder.Code != http.StatusOK {
		t.Fatalf("admin scoped list status = %d, body=%q", adminListRecorder.Code, adminListRecorder.Body.String())
	}

	foreignCustomerRecorder := httptest.NewRecorder()
	p.handleTeamMemberByID(foreignCustomerRecorder, teamMemberHandlerRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/team-members/%d", member.ID),
		"",
		ownerTwo,
	))
	if foreignCustomerRecorder.Code != http.StatusNotFound {
		t.Fatalf("foreign customer get status = %d", foreignCustomerRecorder.Code)
	}

	ownerRecorder := httptest.NewRecorder()
	p.handleTeamMemberByID(ownerRecorder, teamMemberHandlerRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/team-members/%d", member.ID),
		"",
		ownerOne,
	))
	if ownerRecorder.Code != http.StatusOK {
		t.Fatalf("own customer get status = %d, body=%q", ownerRecorder.Code, ownerRecorder.Body.String())
	}
}

func TestTeamMemberHandlersDenyUnknownIdentitiesAndInvalidInput(t *testing.T) {
	p := newDNSPanelForTest(t)
	ownerID := createTeamMemberHandlerUser(t, p, roleCustomer, core.AccountTypeAccount, nil, "validation-owner")
	subscriptionID, domainID := createTeamMemberHandlerScope(t, p, ownerID, "validation-owner")
	resellerID := createTeamMemberHandlerUser(t, p, roleReseller, core.AccountTypeAccount, nil, "validation-reseller")
	additionalID := createTeamMemberHandlerUser(t, p, roleCustomer, core.AccountTypeAdditionalUser, &ownerID, "validation-additional")

	deniedCallers := []*Caller{
		nil,
		{ID: resellerID, Role: roleReseller, AccountType: core.AccountTypeAccount},
		{ID: additionalID, Role: core.EffectiveRoleAdditionalUser, AccountType: core.AccountTypeAdditionalUser, CustomerID: ownerID},
		{ID: ownerID, Role: "unknown", AccountType: core.AccountTypeAccount},
		{ID: ownerID, Role: roleCustomer, AccountType: core.AccountType("unknown")},
	}
	for index, caller := range deniedCallers {
		recorder := httptest.NewRecorder()
		p.handleTeamMembers(recorder, teamMemberHandlerRequest(http.MethodGet, "/api/v1/team-members", "", caller))
		if recorder.Code != http.StatusForbidden {
			t.Fatalf("denied caller %d status = %d", index, recorder.Code)
		}
	}

	customer := &Caller{ID: ownerID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: ownerID}
	invalidBodies := []string{
		fmt.Sprintf(`{"username":"invalid-capability","email":"invalid-capability@example.test","password":"handler-password","access":{"subscription_permissions":[{"subscription_id":%d,"capability":"shell","mode":"view"}],"domain_permissions":[]}}`, subscriptionID),
		fmt.Sprintf(`{"username":"invalid-mode","email":"invalid-mode@example.test","password":"handler-password","access":{"subscription_permissions":[],"domain_permissions":[{"domain_id":%d,"capability":"dns","mode":"owner"}]}}`, domainID),
		`{"username":"missing-access","email":"missing-access@example.test","password":"handler-password"}`,
		`{"username":"unknown-field","email":"unknown-field@example.test","password":"handler-password","unknown":true,"access":{"subscription_permissions":[],"domain_permissions":[]}}`,
		`{"username":"trailing","email":"trailing@example.test","password":"handler-password","access":{"subscription_permissions":[],"domain_permissions":[]}} {}`,
	}
	for index, body := range invalidBodies {
		recorder := httptest.NewRecorder()
		p.handleTeamMembers(recorder, teamMemberHandlerRequest(http.MethodPost, "/api/v1/team-members", body, customer))
		if recorder.Code != http.StatusBadRequest {
			t.Fatalf("invalid body %d status = %d, body=%q", index, recorder.Code, recorder.Body.String())
		}
	}

	methodRecorder := httptest.NewRecorder()
	p.handleTeamMembers(methodRecorder, teamMemberHandlerRequest(http.MethodPatch, "/api/v1/team-members", "", customer))
	if methodRecorder.Code != http.StatusMethodNotAllowed || methodRecorder.Header().Get("Allow") != "GET, POST" {
		t.Fatalf("collection method response = %d Allow=%q", methodRecorder.Code, methodRecorder.Header().Get("Allow"))
	}
}

func TestTeamMemberHandlersSecurityUpdateAndDeleteRevokeSessions(t *testing.T) {
	p := newDNSPanelForTest(t)
	ownerID := createTeamMemberHandlerUser(t, p, roleCustomer, core.AccountTypeAccount, nil, "security-owner")
	subscriptionID, domainID := createTeamMemberHandlerScope(t, p, ownerID, "security-owner")
	caller := &Caller{ID: ownerID, Role: roleCustomer, AccountType: core.AccountTypeAccount, CustomerID: ownerID}

	createRecorder := httptest.NewRecorder()
	p.handleTeamMembers(createRecorder, teamMemberHandlerRequest(
		http.MethodPost,
		"/api/v1/team-members",
		teamMemberCreateBody(nil, "security-member", subscriptionID, domainID),
		caller,
	))
	if createRecorder.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body=%q", createRecorder.Code, createRecorder.Body.String())
	}
	member := decodeTeamMemberHandlerResponse(t, createRecorder)

	if _, err := p.db.GetDB().Exec(
		`INSERT INTO sessions (token_hash, user_id, expires_at) VALUES ('handler-session', ?, '2099-01-01T00:00:00Z')`,
		member.ID,
	); err != nil {
		t.Fatalf("insert handler session: %v", err)
	}
	pendingMu.Lock()
	pendingStore["handler-pending"] = pendingLogin{userID: member.ID, authEpoch: 0, expires: time.Now().Add(time.Hour)}
	pendingMu.Unlock()
	t.Cleanup(func() {
		pendingMu.Lock()
		delete(pendingStore, "handler-pending")
		pendingMu.Unlock()
	})

	updateBody := fmt.Sprintf(`{
		"password":"replacement-password",
		"status":"suspended",
		"access":{
			"subscription_permissions":[{"subscription_id":%d,"capability":"backups","mode":"manage"}],
			"domain_permissions":[]
		}
	}`, subscriptionID)
	updateRecorder := httptest.NewRecorder()
	p.handleTeamMemberByID(updateRecorder, teamMemberHandlerRequest(
		http.MethodPut,
		fmt.Sprintf("/api/v1/team-members/%d", member.ID),
		updateBody,
		caller,
	))
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("security update status = %d, body=%q", updateRecorder.Code, updateRecorder.Body.String())
	}
	updated := decodeTeamMemberHandlerResponse(t, updateRecorder)
	if updated.Status != "suspended" || len(updated.Access.SubscriptionPermissions) != 1 || updated.Access.SubscriptionPermissions[0].Capability != core.TeamCapabilityBackups {
		t.Fatalf("updated member = %#v", updated)
	}
	var authEpoch int64
	var sessions int
	if err := p.db.GetDB().QueryRow(`SELECT auth_epoch FROM users WHERE id = ?`, member.ID).Scan(&authEpoch); err != nil {
		t.Fatal(err)
	}
	if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM sessions WHERE user_id = ?`, member.ID).Scan(&sessions); err != nil {
		t.Fatal(err)
	}
	if authEpoch != 1 || sessions != 0 {
		t.Fatalf("security state after update = epoch %d sessions %d", authEpoch, sessions)
	}
	pendingMu.Lock()
	_, pendingExists := pendingStore["handler-pending"]
	pendingMu.Unlock()
	if pendingExists {
		t.Fatal("pending login survived security update")
	}

	deleteRecorder := httptest.NewRecorder()
	p.handleTeamMemberByID(deleteRecorder, teamMemberHandlerRequest(
		http.MethodDelete,
		fmt.Sprintf("/api/v1/team-members/%d", member.ID),
		"",
		caller,
	))
	if deleteRecorder.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d, body=%q", deleteRecorder.Code, deleteRecorder.Body.String())
	}
	var count int
	if err := p.db.GetDB().QueryRow(`SELECT COUNT(*) FROM users WHERE id = ?`, member.ID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("team member row remains after delete: %d", count)
	}
}
