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
		// R-067. A fresh install has no subscriptions at all - the placeholder
		// admin and its seed subscription are dropped by migration 006 - and
		// until now the only thing that made one was adding a domain. So an
		// administrator who installed MariaDB through this panel, on this
		// machine, and then opened Databases, was told "no database engine
		// installed" about an engine the panel had just installed and could
		// see running. Nothing was broken; there was simply nowhere to put it.
		//
		// The domain path already fixed exactly this, on the same golden path,
		// and left the comment saying so. This is that fix in the second place
		// it was always needed. Only for an administrator: a customer with no
		// subscription genuinely has nothing, and an empty list is the truth
		// for them.
		//
		// R-067. Taze bir kurulumda hic abonelik yoktur ve simdiye kadar bir
		// tane olusturan tek sey domain eklemekti. Dolayisiyla MariaDB'yi bu
		// panelden kuran bir yonetici, Veritabanlari'ni actiginda, panelin az
		// once kurdugu ve calistigini gordugu bir motor hakkinda "veritabani
		// motoru kurulu degil" cevabini aliyordu. Domain yolu ayni kusuru zaten
		// duzeltmisti; bu, o duzeltmenin her zaman gerektigi ikinci yerdeki
		// hali. Yalnizca yonetici icin.
		if c.Role == roleAdmin {
			id, created, err := p.ensureAdminSubscription(r.Context(), c.ID)
			if err == nil && created {
				p.audit(r, "subscription.bootstrap", "subscription", id)
			}
			return id, err
		}
		return 0, errNotFound
	}
	return subID, err
}

// ensureAdminSubscription finds the administrator's own subscription and
// creates it the first time it is needed.
//
// It writes inside a transaction that takes the write lock before it reads,
// so two requests arriving together cannot each decide the subscription is
// missing and each create one. Without that the administrator would end up
// owning two, and every later lookup takes the lowest id - which is a
// difference nobody would notice until the wrong one had a domain in it.
//
// ensureAdminSubscription, yoneticinin kendi aboneligini bulur ve ilk
// ihtiyac duyuldugunda olusturur. Okumadan once yazma kilidini alan bir islem
// icinde yazar; boylece ayni anda gelen iki istek, aboneligin eksik olduguna
// ayri ayri karar verip iki tane olusturamaz.
func (p *Panel) ensureAdminSubscription(ctx context.Context, callerID int) (int, bool, error) {
	tx, err := p.db.GetDB().BeginTx(ctx, nil)
	if err != nil {
		return 0, false, err
	}
	defer func() { _ = tx.Rollback() }()

	var subID int
	err = tx.QueryRowContext(ctx,
		`SELECT id FROM subscriptions WHERE owner_id = ? ORDER BY id LIMIT 1`, callerID).Scan(&subID)
	if err == nil {
		return subID, false, tx.Commit()
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, false, err
	}

	result, err := tx.ExecContext(ctx,
		`INSERT INTO subscriptions (owner_id, name, max_domains, max_databases, status)
		 VALUES (?, 'Admin Subscription', 999, 999, 'active')`, callerID)
	if err != nil {
		return 0, false, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(); err != nil {
		return 0, false, err
	}
	return int(id), true, nil
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
