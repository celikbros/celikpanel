package main

import (
	"context"
	"errors"

	"github.com/alicelik/celikpanel/internal/core"
)

var errInvalidCanonicalAuthIdentity = errors.New("invalid canonical authentication identity")

type canonicalAuthIdentity struct {
	user        *core.User
	identity    core.EffectiveIdentity
	accountType core.AccountType
}

// authIdentityResponse is the single authentication identity contract shared
// by password login, TOTP login and /auth/me. Role remains as a compatibility
// alias, but always contains the canonical effective role.
type authIdentityResponse struct {
	Username      string           `json:"username"`
	Role          string           `json:"role"`
	EffectiveRole string           `json:"effective_role"`
	AccountType   core.AccountType `json:"account_type"`
	Email         string           `json:"email"`
	Impersonating bool             `json:"impersonating"`
	Features      map[string]bool  `json:"features"`
}

func normalizedStoredAccountType(user *core.User) core.AccountType {
	if user == nil {
		return ""
	}
	if user.AccountType == "" {
		return core.AccountTypeAccount
	}
	return user.AccountType
}

// canonicalAuthIdentity reloads the complete account relationship before an
// authentication response is issued. This prevents stored customer roles on
// additional users, stale parents and malformed legacy rows from becoming an
// authorization identity.
func (p *Panel) canonicalAuthIdentity(ctx context.Context, userID int) (canonicalAuthIdentity, error) {
	user, err := p.users.GetByID(ctx, userID)
	if err != nil || user == nil || user.Status != "active" {
		return canonicalAuthIdentity{}, errInvalidCanonicalAuthIdentity
	}

	identity, ok := user.EffectiveIdentity()
	accountType := normalizedStoredAccountType(user)
	caller := Caller{
		ID:          identity.UserID,
		Role:        identity.Role,
		AccountType: accountType,
		CustomerID:  identity.CustomerID,
	}
	if !ok || identity.UserID != user.ID || !caller.validAuthorizationIdentity() {
		return canonicalAuthIdentity{}, errInvalidCanonicalAuthIdentity
	}

	if identity.Role == core.EffectiveRoleAdditionalUser {
		parent, err := p.users.GetByID(ctx, identity.CustomerID)
		if err != nil || parent == nil || parent.Status != "active" {
			return canonicalAuthIdentity{}, errInvalidCanonicalAuthIdentity
		}
		parentIdentity, ok := parent.EffectiveIdentity()
		if !ok || normalizedStoredAccountType(parent) != core.AccountTypeAccount ||
			parentIdentity.UserID != parent.ID || parentIdentity.Role != roleCustomer ||
			parentIdentity.CustomerID != parent.ID {
			return canonicalAuthIdentity{}, errInvalidCanonicalAuthIdentity
		}
	}

	return canonicalAuthIdentity{
		user:        user,
		identity:    identity,
		accountType: accountType,
	}, nil
}

func (identity canonicalAuthIdentity) response(impersonating bool) authIdentityResponse {
	return authIdentityResponse{
		Username:      identity.user.Username,
		Role:          identity.identity.Role,
		EffectiveRole: identity.identity.Role,
		AccountType:   identity.accountType,
		Email:         identity.user.Email,
		Impersonating: impersonating,
		Features: map[string]bool{
			"team_members": identity.accountType == core.AccountTypeAccount && identity.identity.Role == roleCustomer,
		},
	}
}
