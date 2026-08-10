package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"

	"github.com/alicelik/celikpanel/internal/core"
)

// Authorization model (see docs/ROLES.md). Every resource resolves to an
// owning user through the chain domain → subscription.owner_id → user, and a
// reseller additionally owns the customers it created (users.parent_id). A
// request is allowed only if the caller owns the resource, created its owner,
// or is the administrator.
//
// Yetkilendirme modeli (bkz. docs/ROLES.md). Her kaynak, domain →
// subscription.owner_id → user zinciriyle bir sahip kullanıcıya çözümlenir ve
// bir bayi, oluşturduğu müşterilerin de (users.parent_id) sahibidir. Bir istek
// yalnızca çağıran kaynağın sahibiyse, sahibini oluşturduysa ya da yönetici ise
// izinlidir.

const (
	roleAdmin    = "admin"
	roleReseller = "reseller"
	roleCustomer = "customer"
)

// Caller is the authenticated identity attached to every request in requireAuth.
// Caller, requireAuth içinde her isteğe iliştirilen kimliği doğrulanmış kimliktir.
type Caller struct {
	ID          int
	Role        string
	AccountType core.AccountType
	CustomerID  int
}

// normalizedAccountType keeps existing account sessions and older focused
// tests compatible while treating every explicit, unknown marker as invalid.
func (c *Caller) normalizedAccountType() core.AccountType {
	if c == nil {
		return ""
	}
	if c.AccountType == "" {
		return core.AccountTypeAccount
	}
	return c.AccountType
}

// validAuthorizationIdentity validates the effective identity carried in the
// request context. Additional users are intentionally a separate effective
// role; they must never become administrators or inherit their parent account's
// ownership merely by changing a stored role marker.
func (c *Caller) validAuthorizationIdentity() bool {
	if c == nil || c.ID <= 0 {
		return false
	}
	switch c.normalizedAccountType() {
	case core.AccountTypeAccount:
		switch c.Role {
		case roleAdmin, roleReseller:
			return c.CustomerID == 0
		case roleCustomer:
			// CustomerID == 0 is accepted for legacy in-process callers. The
			// authentication middleware always emits the canonical self ID.
			return c.CustomerID == 0 || c.CustomerID == c.ID
		default:
			return false
		}
	case core.AccountTypeAdditionalUser:
		return c.Role == core.EffectiveRoleAdditionalUser &&
			c.CustomerID > 0 && c.CustomerID != c.ID
	default:
		return false
	}
}

func (c *Caller) hasAccountRole(role string) bool {
	return c != nil && c.normalizedAccountType() == core.AccountTypeAccount &&
		c.validAuthorizationIdentity() && c.Role == role
}

func (c *Caller) isAdditionalUser() bool {
	return c != nil && c.normalizedAccountType() == core.AccountTypeAdditionalUser &&
		c.validAuthorizationIdentity()
}

// visibleOwnerIDs returns the set of user IDs whose resources the caller may
// see. For an administrator it returns (nil, true) meaning "everyone".
//
// visibleOwnerIDs, çağıranın kaynaklarını görebileceği kullanıcı kimlikleri
// kümesini döndürür. Yönetici için (nil, true) döner; yani "herkes".
func (p *Panel) visibleOwnerIDs(ctx context.Context, c *Caller) (ids map[int]bool, all bool, err error) {
	if c == nil || !c.validAuthorizationIdentity() {
		return map[int]bool{}, false, nil
	}
	if c.hasAccountRole(roleAdmin) {
		return nil, true, nil
	}
	// Team-member grants are not wired to resource scopes yet. Until they are,
	// fail closed: neither the member's row nor its parent customer's resources
	// are visible through the legacy ownership graph.
	if c.isAdditionalUser() {
		return map[int]bool{}, false, nil
	}
	// Self plus any users this caller created (reseller → customers,
	// customer → additional users).
	// Kendisi artı bu çağıranın oluşturduğu kullanıcılar (bayi → müşteriler,
	// müşteri → ek kullanıcılar).
	rows, err := p.db.GetDB().QueryContext(ctx, `SELECT id FROM users WHERE id = ? OR parent_id = ?`, c.ID, c.ID)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()

	ids = map[int]bool{}
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, false, err
		}
		ids[id] = true
	}
	return ids, false, rows.Err()
}

// errNotFound is returned when a resource does not exist or is not visible to
// the caller. We deliberately do not distinguish the two, so ownership can't be
// probed by ID enumeration.
// errNotFound, bir kaynak yoksa ya da çağıran için görünür değilse döner. İkisini
// bilerek ayırmayız; böylece sahiplik, kimlik denemesiyle yoklanamaz.
var errNotFound = errors.New("not found")

// domainOwnerID resolves a domain to its owning user via its subscription.
// domainOwnerID, bir domain'i aboneliği üzerinden sahip kullanıcısına çözer.
func (p *Panel) domainOwnerID(ctx context.Context, domainID int) (int, error) {
	var ownerID int
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.owner_id
		FROM domains d
		JOIN subscriptions s ON d.subscription_id = s.id
		WHERE d.id = ?`, domainID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNotFound
	}
	return ownerID, err
}

// canAccessDomain reports whether the caller may act on the given domain.
// canAccessDomain, çağıranın verilen domain üzerinde işlem yapıp
// yapamayacağını bildirir.
func (p *Panel) canAccessDomain(ctx context.Context, c *Caller, domainID int) error {
	ownerID, err := p.domainOwnerID(ctx, domainID)
	if err != nil {
		return err
	}
	return p.ownerAllowed(ctx, c, ownerID)
}

// canAccessSubscription reports whether the caller owns (or created the owner
// of) the given subscription.
// canAccessSubscription, çağıranın verilen aboneliğin sahibi olup olmadığını
// (ya da sahibini oluşturup oluşturmadığını) bildirir.
func (p *Panel) canAccessSubscription(ctx context.Context, c *Caller, subID int) error {
	var ownerID int
	err := p.db.GetDB().QueryRowContext(ctx, `SELECT owner_id FROM subscriptions WHERE id = ?`, subID).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	return p.ownerAllowed(ctx, c, ownerID)
}

// ownerAllowed checks a resolved owner ID against the caller's visible set.
// ownerAllowed, çözümlenmiş bir sahip kimliğini çağıranın görünür kümesine
// karşı denetler.
func (p *Panel) ownerAllowed(ctx context.Context, c *Caller, ownerID int) error {
	ids, all, err := p.visibleOwnerIDs(ctx, c)
	if err != nil {
		return err
	}
	if all || ids[ownerID] {
		return nil
	}
	return errNotFound
}

// authorizeDomain is the HTTP guard used by the domain dispatcher: it writes a
// 404 and returns false when the caller may not touch the domain.
// authorizeDomain, domain yönlendiricisinin kullandığı HTTP korumasıdır:
// çağıran domain'e dokunamıyorsa 404 yazar ve false döner.
func (p *Panel) authorizeDomain(w http.ResponseWriter, r *http.Request, domainID int) bool {
	err := p.canAccessDomain(r.Context(), currentCaller(r), domainID)
	if err == nil {
		return true
	}
	if errors.Is(err, errNotFound) {
		writeClientError(w, http.StatusNotFound, "domain not found")
	} else {
		writeServerError(w, err)
	}
	return false
}
