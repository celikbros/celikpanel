package core

import "testing"

func intPointer(value int) *int {
	return &value
}

func TestUserEffectiveIdentityPreservesAccountRoles(t *testing.T) {
	tests := []struct {
		name               string
		user               User
		wantRole           string
		wantCustomerID     int
		wantAdditionalUser bool
	}{
		{
			name:     "legacy admin",
			user:     User{ID: 1, Role: "admin"},
			wantRole: "admin",
		},
		{
			name:     "legacy reseller",
			user:     User{ID: 2, Role: "reseller"},
			wantRole: "reseller",
		},
		{
			name:           "legacy customer",
			user:           User{ID: 3, Role: "customer"},
			wantRole:       "customer",
			wantCustomerID: 3,
		},
		{
			name:     "account reseller",
			user:     User{ID: 4, Role: "reseller", AccountType: AccountTypeAccount},
			wantRole: "reseller",
		},
		{
			name:           "account customer",
			user:           User{ID: 5, Role: "customer", AccountType: AccountTypeAccount},
			wantRole:       "customer",
			wantCustomerID: 5,
		},
		{
			name:               "additional user",
			user:               User{ID: 6, Role: "customer", AccountType: AccountTypeAdditionalUser, ParentID: intPointer(5)},
			wantRole:           EffectiveRoleAdditionalUser,
			wantCustomerID:     5,
			wantAdditionalUser: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.user.IsAdditionalUser(); got != tt.wantAdditionalUser {
				t.Fatalf("IsAdditionalUser() = %v, want %v", got, tt.wantAdditionalUser)
			}
			if got := tt.user.EffectiveRole(); got != tt.wantRole {
				t.Fatalf("EffectiveRole() = %q, want %q", got, tt.wantRole)
			}

			identity, ok := tt.user.EffectiveIdentity()
			if !ok {
				t.Fatal("EffectiveIdentity() rejected a valid user")
			}
			if identity.UserID != tt.user.ID || identity.Role != tt.wantRole || identity.CustomerID != tt.wantCustomerID {
				t.Fatalf("EffectiveIdentity() = %+v, want UserID=%d Role=%q CustomerID=%d", identity, tt.user.ID, tt.wantRole, tt.wantCustomerID)
			}
		})
	}
}

func TestUserEffectiveIdentityFailsClosed(t *testing.T) {
	tests := []struct {
		name              string
		user              User
		roleMayBeResolved bool
	}{
		{
			name: "unknown storage role",
			user: User{ID: 10, Role: "owner", AccountType: AccountTypeAccount},
		},
		{
			name: "unknown account type",
			user: User{ID: 10, Role: "customer", AccountType: AccountType("unexpected")},
		},
		{
			name: "additional user with admin storage role",
			user: User{ID: 10, Role: "admin", AccountType: AccountTypeAdditionalUser, ParentID: intPointer(20)},
		},
		{
			name: "additional user without owner",
			user: User{ID: 10, Role: "customer", AccountType: AccountTypeAdditionalUser},
		},
		{
			name: "additional user with invalid owner",
			user: User{ID: 10, Role: "customer", AccountType: AccountTypeAdditionalUser, ParentID: intPointer(0)},
		},
		{
			name: "additional user owning itself",
			user: User{ID: 10, Role: "customer", AccountType: AccountTypeAdditionalUser, ParentID: intPointer(10)},
		},
		{
			name:              "unpersisted account",
			user:              User{Role: "customer", AccountType: AccountTypeAccount},
			roleMayBeResolved: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if role := tt.user.EffectiveRole(); role != "" && !tt.roleMayBeResolved {
				t.Fatalf("EffectiveRole() = %q, want empty", role)
			}
			if identity, ok := tt.user.EffectiveIdentity(); ok {
				t.Fatalf("EffectiveIdentity() = %+v, want rejection", identity)
			}
		})
	}
}
