package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
)

const authzMatrixAdditionalUserID = 9710

// authzUserRepositoryOverride lets these middleware tests model corrupt or
// concurrently changed account rows without weakening the database invariants.
type authzUserRepositoryOverride struct {
	repositories.UserRepository
	users    map[int]*core.User
	nilUsers map[int]bool
	errs     map[int]error
}

func (r *authzUserRepositoryOverride) GetByID(ctx context.Context, id int) (*core.User, error) {
	if err, ok := r.errs[id]; ok {
		return nil, err
	}
	if r.nilUsers[id] {
		return nil, nil
	}
	if user, ok := r.users[id]; ok {
		clone := *user
		return &clone, nil
	}
	return r.UserRepository.GetByID(ctx, id)
}

func seedAdditionalUserSession(t *testing.T, fixture *authzMatrixFixture) string {
	t.Helper()
	_, err := fixture.panel.db.GetDB().Exec(`
		INSERT INTO users
			(id, username, password_hash, email, role, account_type, parent_id, status)
		VALUES
			(?, 'matrix-member', 'x', 'matrix-member@example.test',
			 'customer', 'additional_user', ?, 'active')`,
		authzMatrixAdditionalUserID, authzMatrixCustomerID)
	if err != nil {
		t.Fatalf("seed additional user: %v", err)
	}
	token, err := fixture.panel.sessions.Create(context.Background(), authzMatrixAdditionalUserID)
	if err != nil {
		t.Fatalf("create additional-user session: %v", err)
	}
	fixture.tokens[authzMatrixAdditionalUserID] = token
	return token
}

func requestWithToken(handler http.Handler, method, path, token string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: token})
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	return recorder
}

func TestRequireAuthAdditionalUserDerivesRestrictedIdentity(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	token := seedAdditionalUserSession(t, &fixture)

	var got *Caller
	handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = currentCaller(r)
		w.WriteHeader(http.StatusNoContent)
	}))
	recorder := requestWithToken(handler, http.MethodGet, "/api/v1/auth/me", token)
	if recorder.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}
	if got == nil {
		t.Fatal("additional user reached handler without caller")
	}
	if got.ID != authzMatrixAdditionalUserID || got.Role != core.EffectiveRoleAdditionalUser ||
		got.AccountType != core.AccountTypeAdditionalUser || got.CustomerID != authzMatrixCustomerID {
		t.Fatalf("caller = %#v, want restricted identity for customer %d", got, authzMatrixCustomerID)
	}

	adminRecorder := requestWithToken(fixture.handler, http.MethodPost, "/api/v1/service/install", token)
	if adminRecorder.Code != http.StatusForbidden {
		t.Fatalf("admin-only status = %d, want %d; body=%s", adminRecorder.Code, http.StatusForbidden, adminRecorder.Body.String())
	}
}

func TestRequireAuthAdditionalUserCannotReadServerGlobalStats(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	token := seedAdditionalUserSession(t, &fixture)

	recorder := requestWithToken(fixture.handler, http.MethodGet, "/api/v1/system/stats", token)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusForbidden, recorder.Body.String())
	}
	if !strings.Contains(recorder.Body.String(), `"code":"`+errCodeAdditionalUserScope+`"`) {
		t.Fatalf("body = %s, want error code %q", recorder.Body.String(), errCodeAdditionalUserScope)
	}
}

func TestRequireAuthAdditionalUserParentMustRemainActiveCustomerAccount(t *testing.T) {
	tests := []struct {
		name       string
		parentUser *core.User
		parentErr  error
		nilParent  bool
		want       int
	}{
		{name: "missing parent", parentErr: errors.New("not found"), want: http.StatusUnauthorized},
		{name: "nil parent without repository error", nilParent: true, want: http.StatusUnauthorized},
		{name: "suspended parent", parentUser: &core.User{ID: authzMatrixCustomerID, Role: roleCustomer, AccountType: core.AccountTypeAccount, Status: "suspended"}, want: http.StatusForbidden},
		{name: "reseller parent", parentUser: &core.User{ID: authzMatrixCustomerID, Role: roleReseller, AccountType: core.AccountTypeAccount, Status: "active"}, want: http.StatusUnauthorized},
		{name: "additional-user parent", parentUser: &core.User{ID: authzMatrixCustomerID, Role: roleCustomer, AccountType: core.AccountTypeAdditionalUser, ParentID: authzIntPtr(authzMatrixOutsiderID), Status: "active"}, want: http.StatusUnauthorized},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAuthzMatrixFixture(t)
			token := seedAdditionalUserSession(t, &fixture)
			base := fixture.panel.users
			override := &authzUserRepositoryOverride{
				UserRepository: base,
				users:          map[int]*core.User{},
				nilUsers:       map[int]bool{},
				errs:           map[int]error{},
			}
			if test.parentUser != nil {
				override.users[authzMatrixCustomerID] = test.parentUser
			}
			if test.parentErr != nil {
				override.errs[authzMatrixCustomerID] = test.parentErr
			}
			if test.nilParent {
				override.nilUsers[authzMatrixCustomerID] = true
			}
			fixture.panel.users = override

			handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNoContent)
			}))
			recorder := requestWithToken(handler, http.MethodGet, "/api/v1/system/stats", token)
			if recorder.Code != test.want {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.want, recorder.Body.String())
			}
		})
	}
}

func TestParentSuspensionDoesNotReviveAdditionalUserSessionAfterReactivation(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	token := seedAdditionalUserSession(t, &fixture)
	handler := fixture.panel.requireAuth(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))

	if recorder := requestWithToken(handler, http.MethodGet, "/api/v1/auth/me", token); recorder.Code != http.StatusNoContent {
		t.Fatalf("initial status = %d, want %d; body=%s", recorder.Code, http.StatusNoContent, recorder.Body.String())
	}

	owner, err := fixture.panel.users.GetByID(context.Background(), authzMatrixCustomerID)
	if err != nil {
		t.Fatalf("load owner: %v", err)
	}
	owner.Status = "suspended"
	if err := fixture.panel.users.UpdateAndRevokeSessions(context.Background(), owner); err != nil {
		t.Fatalf("suspend owner: %v", err)
	}
	if recorder := requestWithToken(handler, http.MethodGet, "/api/v1/auth/me", token); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("suspended status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}

	owner.Status = "active"
	if err := fixture.panel.users.Update(context.Background(), owner); err != nil {
		t.Fatalf("reactivate owner: %v", err)
	}
	if recorder := requestWithToken(handler, http.MethodGet, "/api/v1/auth/me", token); recorder.Code != http.StatusUnauthorized {
		t.Fatalf("reactivated status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestRequireAuthRejectsUnknownOrInconsistentAccountMarkers(t *testing.T) {
	tests := []struct {
		name string
		user *core.User
	}{
		{name: "unknown account type", user: &core.User{ID: authzMatrixCustomerID, Role: roleCustomer, AccountType: core.AccountType("unknown"), Status: "active"}},
		{name: "additional user with admin role", user: &core.User{ID: authzMatrixCustomerID, Role: roleAdmin, AccountType: core.AccountTypeAdditionalUser, ParentID: authzIntPtr(authzMatrixOutsiderID), Status: "active"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newAuthzMatrixFixture(t)
			fixture.panel.users = &authzUserRepositoryOverride{
				UserRepository: fixture.panel.users,
				users:          map[int]*core.User{authzMatrixCustomerID: test.user},
			}
			recorder := fixture.request(t, http.MethodGet, "/api/v1/system/stats", authzMatrixCustomerID)
			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
			}
		})
	}
}

func TestRequireAuthRejectsNilUserWithoutPanic(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	fixture.panel.users = &authzUserRepositoryOverride{
		UserRepository: fixture.panel.users,
		nilUsers:       map[int]bool{authzMatrixCustomerID: true},
	}

	recorder := fixture.request(t, http.MethodGet, "/api/v1/auth/me", authzMatrixCustomerID)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnauthorized, recorder.Body.String())
	}
}

func TestAdditionalUserDoesNotInheritLegacyOwnershipBeforeGrantIntegration(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	caller := &Caller{
		ID:          authzMatrixAdditionalUserID,
		Role:        core.EffectiveRoleAdditionalUser,
		AccountType: core.AccountTypeAdditionalUser,
		CustomerID:  authzMatrixCustomerID,
	}

	ids, all, err := fixture.panel.visibleOwnerIDs(context.Background(), caller)
	if err != nil {
		t.Fatalf("visibleOwnerIDs: %v", err)
	}
	if all || len(ids) != 0 {
		t.Fatalf("visible owners = %#v, all=%v; want deny-all", ids, all)
	}
	if caller.hasAccountRole(roleAdmin) || caller.hasAccountRole(roleCustomer) {
		t.Fatal("additional user unexpectedly satisfies a real-account role")
	}
	for name, accessErr := range map[string]error{
		"self owner":          fixture.panel.ownerAllowed(context.Background(), caller, authzMatrixAdditionalUserID),
		"parent owner":        fixture.panel.ownerAllowed(context.Background(), caller, authzMatrixCustomerID),
		"parent domain":       fixture.panel.canAccessDomain(context.Background(), caller, authzMatrixCustomerDomainID),
		"parent subscription": fixture.panel.canAccessSubscription(context.Background(), caller, authzMatrixCustomerSubID),
	} {
		if !errors.Is(accessErr, errNotFound) {
			t.Errorf("%s error = %v, want errNotFound", name, accessErr)
		}
	}
}

func authzIntPtr(value int) *int { return &value }
