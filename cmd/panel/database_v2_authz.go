package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

// Tenant scoping for the v2 database API (AUTOPSY A3). These handlers used to
// hardcode `subscriptionID := 1`, ignoring auth entirely — that is why the
// whole /api/v2/ prefix had to stay admin-gated. Here the scope comes from
// the authenticated caller, and every server-scoped operation verifies
// ownership through the same chain the rest of the panel uses
// (server → subscription.owner_id → user).
//
// v2 veritabanı API'si için kiracı kapsamı (AUTOPSY A3). Bu handler'lar
// `subscriptionID := 1` sabitliyor, auth'u tümden yok sayıyordu — /api/v2/
// prefix'inin admin kilidinde kalma nedeni buydu. Artık kapsam kimliği
// doğrulanmış çağırandan gelir ve her sunucu-kapsamlı işlem sahipliği
// panelin geri kalanıyla aynı zincirden doğrular (sunucu → abonelik.owner_id
// → kullanıcı).

// callerSubscriptionID resolves the caller's primary owned subscription — the
// default scope for tenant-facing database operations.
// callerSubscriptionID, çağıranın birincil sahip olduğu aboneliği çözer —
// kiracı-yüzlü veritabanı işlemlerinin varsayılan kapsamı.
func (p *Panel) callerSubscriptionID(r *http.Request) (int, error) {
	c := currentCaller(r)
	if c == nil {
		return 0, errNotFound
	}
	var subID int
	err := p.db.GetDB().QueryRowContext(r.Context(),
		`SELECT id FROM subscriptions WHERE owner_id = ? ORDER BY id LIMIT 1`, c.ID).Scan(&subID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, errNotFound
	}
	return subID, err
}

// databaseUserReference deliberately excludes the stored password. Reference
// validation and API responses never need to load a reusable credential.
type databaseUserReference struct {
	ID       int
	Username string
}

// databaseUserForServerSubscription resolves an existing user only inside the
// exact logical server and subscription selected by the request. A missing ID
// and a foreign ID deliberately have the same result.
func (p *Panel) databaseUserForServerSubscription(
	ctx context.Context,
	c *Caller,
	userID int,
	serverID int,
	subscriptionID int,
) (*databaseUserReference, error) {
	var ref databaseUserReference
	var ownerID int
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT du.id, du.username, s.owner_id
		FROM database_users du
		JOIN subscriptions s ON s.id = du.subscription_id
		WHERE du.id = ? AND du.server_id = ? AND du.subscription_id = ?`,
		userID, serverID, subscriptionID,
	).Scan(&ref.ID, &ref.Username, &ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, errNotFound
	}
	if err != nil {
		return nil, err
	}
	if err := p.ownerAllowed(ctx, c, ownerID); err != nil {
		return nil, err
	}
	return &ref, nil
}

// databaseDomainInSubscription accepts a domain only when it belongs to the
// same subscription as the selected logical database server and is visible to
// the caller. Missing and foreign references remain indistinguishable.
func (p *Panel) databaseDomainInSubscription(
	ctx context.Context,
	c *Caller,
	domainID int,
	subscriptionID int,
) error {
	var ownerID int
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT s.owner_id
		FROM domains d
		JOIN subscriptions s ON s.id = d.subscription_id
		WHERE d.id = ? AND d.subscription_id = ?`,
		domainID, subscriptionID,
	).Scan(&ownerID)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	return p.ownerAllowed(ctx, c, ownerID)
}

func writeDatabaseReferenceError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotFound) {
		writeClientError(w, http.StatusNotFound, `invalid request`)
		return
	}
	writeServerError(w, err)
}

// canAccessDBServer verifies the caller may act on the given database server,
// resolving server → subscription → ownership. Returns errNotFound when the
// server is absent OR invisible (the two are deliberately indistinguishable,
// so ownership cannot be probed by ID enumeration).
// canAccessDBServer, çağıranın verilen veritabanı sunucusunda işlem yapıp
// yapamayacağını doğrular (sunucu → abonelik → sahiplik). Sunucu yoksa VEYA
// görünmezse errNotFound döner (ikisi bilerek ayırt edilemez; sahiplik
// kimlik denemesiyle yoklanamaz).
func (p *Panel) canAccessDBServer(ctx context.Context, c *Caller, serverID int) error {
	var subID int
	err := p.db.GetDB().QueryRowContext(ctx,
		`SELECT subscription_id FROM database_servers WHERE id = ?`, serverID).Scan(&subID)
	if errors.Is(err, sql.ErrNoRows) {
		return errNotFound
	}
	if err != nil {
		return err
	}
	return p.canAccessSubscription(ctx, c, subID)
}
