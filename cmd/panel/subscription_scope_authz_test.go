package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sort"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func subscriptionScopeRequest(method string, caller *Caller) *http.Request {
	request := httptest.NewRequest(method, "/api/v1/subscriptions", nil)
	return request.WithContext(context.WithValue(request.Context(), callerKey, caller))
}

func TestHandleSubscriptionsAllowsOnlyOwnedCustomerScopes(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	tests := []struct {
		name       string
		caller     *Caller
		want       []int
		wantStatus int
	}{
		{
			name:       "admin sees all",
			caller:     &Caller{ID: authzMatrixAdminID, Role: roleAdmin, AccountType: core.AccountTypeAccount},
			want:       []int{authzMatrixResellerSubID, authzMatrixCustomerSubID, authzMatrixOutsiderSubID},
			wantStatus: http.StatusOK,
		},
		{
			name:       "reseller sees own and customer",
			caller:     &Caller{ID: authzMatrixResellerID, Role: roleReseller, AccountType: core.AccountTypeAccount},
			want:       []int{authzMatrixResellerSubID, authzMatrixCustomerSubID},
			wantStatus: http.StatusOK,
		},
		{
			name: "customer sees only own",
			caller: &Caller{
				ID: authzMatrixCustomerID, Role: roleCustomer,
				AccountType: core.AccountTypeAccount, CustomerID: authzMatrixCustomerID,
			},
			want:       []int{authzMatrixCustomerSubID},
			wantStatus: http.StatusOK,
		},
		{
			name: "additional user denied",
			caller: &Caller{
				ID: authzMatrixAdditionalUserID, Role: core.EffectiveRoleAdditionalUser,
				AccountType: core.AccountTypeAdditionalUser, CustomerID: authzMatrixCustomerID,
			},
			wantStatus: http.StatusForbidden,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			fixture.panel.handleSubscriptions(recorder, subscriptionScopeRequest(http.MethodGet, test.caller))
			if recorder.Code != test.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", recorder.Code, test.wantStatus, recorder.Body.String())
			}
			if test.wantStatus != http.StatusOK {
				return
			}
			var payload struct {
				Subscriptions []struct {
					ID int `json:"id"`
				} `json:"subscriptions"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			got := make([]int, 0, len(payload.Subscriptions))
			for _, subscription := range payload.Subscriptions {
				got = append(got, subscription.ID)
			}
			sort.Ints(got)
			sort.Ints(test.want)
			if !reflect.DeepEqual(got, test.want) {
				t.Fatalf("subscription IDs = %v, want %v", got, test.want)
			}
		})
	}
}

func TestHandleSubscriptionsRejectsMutationMethods(t *testing.T) {
	fixture := newAuthzMatrixFixture(t)
	caller := &Caller{
		ID: authzMatrixCustomerID, Role: roleCustomer,
		AccountType: core.AccountTypeAccount, CustomerID: authzMatrixCustomerID,
	}
	recorder := httptest.NewRecorder()
	fixture.panel.handleSubscriptions(recorder, subscriptionScopeRequest(http.MethodPost, caller))
	if recorder.Code != http.StatusMethodNotAllowed || recorder.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("status/Allow = %d/%q, want 405/GET", recorder.Code, recorder.Header().Get("Allow"))
	}
}
