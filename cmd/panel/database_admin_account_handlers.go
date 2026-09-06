package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/repositories"
	"github.com/alicelik/celikpanel/internal/transport"
)

// R-057. The panel used to tell the operator to "give this server a root
// password, then have an administrator register the server again with that
// password" - an instruction with nowhere to follow it, because the server
// list is filled by autodiscovery and there is no register-a-server screen.
//
// These handlers are the answer: the panel opens an account of its own on the
// engine, keeps the password sealed, and lets an administrator see it, change
// it, or take the account away. The operator's own root is not read, not
// written, and not depended on. docs/DATABASE-ADMIN-ACCOUNT.md records why
// that boundary is where it is.
//
// R-057. Panel eskiden operatore, takip edecek yeri olmayan bir talimat
// veriyordu. Bu isleyiciler yanittir: panel motorda kendi hesabini acar,
// parolayi muhurlu tutar ve bir yoneticinin onu gormesine, degistirmesine ya
// da kaldirmasina izin verir. Operatorun kendi kok hesabi okunmaz, yazilmaz.

// databaseAdminAccountAlphabet has no characters a person can mistake for
// another when they read the password off a screen and type it somewhere else,
// which an administrator is explicitly allowed to do here.
// databaseAdminAccountAlphabet, ekrandan okunup baska bir yere yazilirken
// birbirine karistirilabilecek karakterleri icermez.
const databaseAdminAccountAlphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"

// databaseAdminAccountPasswordLength gives about 232 bits over the alphabet
// above. This credential is never typed by a person under time pressure and
// never transmitted anywhere but a unix socket, so there is no reason for it
// to be short.
// databaseAdminAccountPasswordLength, yukaridaki alfabede yaklasik 232 bit
// verir.
const databaseAdminAccountPasswordLength = 40

// generateDatabaseAdminPassword draws from crypto/rand and refuses to return a
// password it could not draw completely. A short read here would be a weak
// credential that nothing downstream could tell from a strong one.
// generateDatabaseAdminPassword, crypto/rand'dan cizer ve tam cizemedigi bir
// parolayi dondurmeyi reddeder.
func generateDatabaseAdminPassword() (string, error) {
	limit := big.NewInt(int64(len(databaseAdminAccountAlphabet)))
	password := make([]byte, databaseAdminAccountPasswordLength)
	for i := range password {
		index, err := rand.Int(rand.Reader, limit)
		if err != nil {
			return "", fmt.Errorf("generate database admin password: %w", err)
		}
		password[i] = databaseAdminAccountAlphabet[index.Int64()]
	}
	return string(password), nil
}

// newSealedDatabaseAdminPassword draws a password and seals it, and refuses to
// hand back either half unless it has both. A panel with no key would
// otherwise generate a credential, set it on a host, and then have no way to
// use it - which is worse than not having the account at all, because the
// account would exist.
//
// newSealedDatabaseAdminPassword, bir parola cizer ve muhurler; ikisine
// birden sahip olmadikca hicbir yarisini vermez. Anahtari olmayan bir panel,
// aksi halde bir kimlik bilgisi uretip makineye yazar ve sonra kullanamaz.
func (p *Panel) newSealedDatabaseAdminPassword() (string, string, error) {
	if p.secrets == nil {
		return "", "", fmt.Errorf(
			"CelikPanel has no key to seal a database credential with, so it will not set one")
	}
	plain, err := generateDatabaseAdminPassword()
	if err != nil {
		return "", "", err
	}
	sealed, err := p.secrets.Encrypt(plain)
	if err != nil {
		return "", "", err
	}
	return plain, sealed, nil
}

// databaseEngineIsOnThisMachine. The agent's privileged door - the unix socket
// on MariaDB, peer authentication on PostgreSQL - exists only on the machine
// the agent runs on. A remote engine is somebody else's machine and the panel
// must not pretend it can open an account there; an administrator supplies a
// credential for those instead.
//
// databaseEngineIsOnThisMachine. Agent'in ayricalikli kapisi yalnizca agent'in
// calistigi makinede vardir. Uzak bir motor baskasinin makinesidir.
func databaseEngineIsOnThisMachine(host string) bool {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "", "localhost", "127.0.0.1", "::1", "[::1]":
		return true
	default:
		return false
	}
}

// loadDatabaseServerForAdminAccount does the checks every one of these
// handlers needs, in the order that makes a refusal say the right thing.
// loadDatabaseServerForAdminAccount, bu isleyicilerin her birinin ihtiyac
// duydugu denetimleri yapar.
func (p *Panel) loadDatabaseServerForAdminAccount(
	w http.ResponseWriter, r *http.Request,
) (*core.DatabaseServer, bool) {
	if c := currentCaller(r); c == nil || c.Role != roleAdmin {
		writeClientError(w, http.StatusForbidden, "administrator access required")
		return nil, false
	}
	serverID, err := getServerIDFromPath(r.URL.Path)
	if err != nil {
		writeClientError(w, http.StatusBadRequest, "invalid server ID")
		return nil, false
	}
	ctx := r.Context()
	if err := p.canAccessDBServer(ctx, currentCaller(r), serverID); err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return nil, false
	}
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	server, err := serverRepo.GetByID(ctx, serverID)
	if err != nil {
		writeClientError(w, http.StatusNotFound, "invalid request")
		return nil, false
	}
	return server, true
}

// handleProvisionDatabaseAdminAccount opens the panel's account on the engine,
// or gives it a new password if it is already there. Those are the same call
// on purpose: a panel that had to know which one it was doing would have to
// hold a belief about the engine that could be wrong, and being wrong would
// mean either refusing to repair a broken account or refusing to create a
// missing one.
//
// handleProvisionDatabaseAdminAccount, panelin hesabini motorda acar ya da
// zaten oradaysa ona yeni bir parola verir.
func (p *Panel) handleProvisionDatabaseAdminAccount(w http.ResponseWriter, r *http.Request) {
	server, ok := p.loadDatabaseServerForAdminAccount(w, r)
	if !ok {
		return
	}
	if !databaseEngineIsOnThisMachine(server.Host) {
		writeClientError(w, http.StatusConflict,
			"CelikPanel can only open an account for itself on a database engine "+
				"running on this machine. This server is recorded at another "+
				"address, so give CelikPanel a username and password for it instead.")
		return
	}

	// Sealed before the host is touched, so a panel that cannot keep the
	// credential never sets one on the engine. The reverse order would leave
	// an account on the host whose password nothing holds.
	// Makineye dokunmadan once muhurlenir; boylece kimlik bilgisini
	// saklayamayan bir panel motorda hicbir zaman parola belirlemez.
	password, sealed, err := p.newSealedDatabaseAdminPassword()
	if err != nil {
		writeServerError(w, err)
		return
	}

	var resp transport.ProvisionDatabaseAdminAccountResponse
	if err := p.callAgentContext(r.Context(), "Agent.ProvisionDatabaseAdminAccount",
		&transport.ProvisionDatabaseAdminAccountRequest{
			Engine:   server.TypeName,
			Password: password,
		}, &resp); err != nil {
		p.audit(r, "database.admin_account.provision.failed:"+server.TypeName,
			"database_server", server.ID)
		writeServerError(w, err)
		return
	}
	if !resp.Provisioned {
		p.audit(r, "database.admin_account.provision.failed:"+server.TypeName,
			"database_server", server.ID)
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}

	// The engine now has the account. If this write fails the panel holds a
	// credential it did not record, and the repair is to provision again -
	// which is exactly what this endpoint does, and why it is create-or-rotate.
	// Motorda hesap artik var. Bu yazma basarisiz olursa onarim, yeniden
	// olusturmaktir.
	server.AdminUsername = resp.Username
	server.AdminPasswordEncrypted = sealed
	p.recordEngineVersion(server)
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	if err := serverRepo.Update(r.Context(), server); err != nil {
		writeServerError(w, err)
		return
	}

	p.audit(r, "database.admin_account.provision:"+server.TypeName,
		"database_server", server.ID)

	w.Header().Set("Content-Type", "application/json")
	// The password is not in this response. An administrator who wants to see
	// it asks for it, and asking leaves a record.
	// Parola bu yanitta degil. Gormek isteyen yonetici onu ister ve istemek
	// kayit birakir.
	_ = json.NewEncoder(w).Encode(map[string]any{
		"status":   "provisioned",
		"username": resp.Username,
	})
}

// handleRevealDatabaseAdminAccountPassword gives an administrator the password
// the panel holds. The panel generated it, but it is not the panel's secret to
// keep from the person who owns the machine - and a credential that can be
// read without a trace is one nobody can account for, so reading it is audited
// exactly like changing it.
//
// handleRevealDatabaseAdminAccountPassword, yoneticiye panelin tuttugu
// parolayi verir. Okumak, degistirmek gibi denetim kaydina yazilir.
func (p *Panel) handleRevealDatabaseAdminAccountPassword(w http.ResponseWriter, r *http.Request) {
	server, ok := p.loadDatabaseServerForAdminAccount(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(server.AdminUsername) == "" {
		writeClientError(w, http.StatusNotFound,
			"CelikPanel has not opened an account of its own on this database server.")
		return
	}
	if p.secrets == nil {
		writeServerError(w, fmt.Errorf("CelikPanel has no key to open this credential with"))
		return
	}
	password, err := p.secrets.Decrypt(server.AdminPasswordEncrypted)
	if err != nil {
		writeServerError(w, err)
		return
	}

	p.audit(r, "database.admin_account.reveal:"+server.TypeName,
		"database_server", server.ID)

	w.Header().Set("Content-Type", "application/json")
	// A credential must not sit in a shared cache on its way to one screen.
	// Kimlik bilgisi, tek bir ekrana giderken paylasilan bir onbellekte durmaz.
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"username": server.AdminUsername,
		"password": password,
	})
}

// handleRemoveDatabaseAdminAccount takes the panel's account off the engine.
// The forgetting happens whether or not the engine could be reached, because
// the alternative is a panel that keeps a credential for an account it has
// been told to give up.
//
// handleRemoveDatabaseAdminAccount, panelin hesabini motordan kaldirir.
func (p *Panel) handleRemoveDatabaseAdminAccount(w http.ResponseWriter, r *http.Request) {
	server, ok := p.loadDatabaseServerForAdminAccount(w, r)
	if !ok {
		return
	}
	if strings.TrimSpace(server.AdminUsername) == "" {
		writeClientError(w, http.StatusNotFound,
			"CelikPanel has not opened an account of its own on this database server.")
		return
	}

	var resp transport.RemoveDatabaseAdminAccountResponse
	if err := p.callAgentContext(r.Context(), "Agent.RemoveDatabaseAdminAccount",
		&transport.RemoveDatabaseAdminAccountRequest{Engine: server.TypeName},
		&resp); err != nil {
		writeServerError(w, err)
		return
	}
	if !resp.Removed {
		writeClientError(w, http.StatusConflict, resp.Error)
		return
	}

	server.AdminUsername = ""
	server.AdminPasswordEncrypted = ""
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	if err := serverRepo.Update(r.Context(), server); err != nil {
		writeServerError(w, err)
		return
	}

	p.audit(r, "database.admin_account.remove:"+server.TypeName,
		"database_server", server.ID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"status": "removed"})
}

// recordEngineVersion asks the engine what it is, now that the panel has an
// account on it that works.
//
// The version on a registered server came from the service scan, which reports
// "unknown" for an engine it can see running - so the databases screen said
// "unknown" beside a MariaDB the panel had installed itself and was connected
// to. The panel could always have asked; until R-057 it had no credential to
// ask with.
//
// Deliberately best-effort, and deliberately not allowed to fail the thing it
// decorates: an account was just opened on a host, and refusing to record that
// because a version string could not be read would trade the important half
// for the cosmetic one. A version that cannot be read stays as it was.
//
// recordEngineVersion, panelin artik calisan bir hesabi oldugu icin motora ne
// oldugunu sorar. Bilerek en-iyi-caba ve bilerek susledigi seyi dusurmesine
// izin verilmez.
func (p *Panel) recordEngineVersion(server *core.DatabaseServer) {
	current := strings.ToLower(strings.TrimSpace(server.Version))
	if current != "" && current != "unknown" {
		return
	}
	driver, err := p.dbDriverFor(server)
	if err != nil {
		return
	}
	version, err := driver.ServerVersion()
	if err != nil {
		return
	}
	if version = strings.TrimSpace(version); version != "" {
		server.Version = version
	}
}

// provisionDatabaseAdminAccountsFor opens the panel's account on engines
// autodiscovery has just registered. It is deliberately the same work as the
// handler above, done without a request behind it, so an engine the panel
// claimed and an engine an administrator asked about end up in exactly the
// same state.
//
// It skips a server that already has an account and a server on another
// machine, and it stops at the first engine it cannot do, so the caller's log
// line names something true rather than the last of several failures.
//
// provisionDatabaseAdminAccountsFor, autodiscovery'nin az once kaydettigi
// motorlarda panelin hesabini acar. Yukaridaki isleyiciyle ayni istir.
func (p *Panel) provisionDatabaseAdminAccountsFor(
	ctx context.Context, subscriptionID int, engines []string,
) error {
	serverRepo := repositories.NewPostgresDatabaseServerRepository(p.db.GetDB())
	for _, engine := range engines {
		servers, err := serverRepo.ListByType(ctx, subscriptionID, engine)
		if err != nil {
			return fmt.Errorf("find the %s server that was just registered: %w", engine, err)
		}
		for _, server := range servers {
			if !databaseEngineIsOnThisMachine(server.Host) {
				continue
			}
			if strings.TrimSpace(server.AdminUsername) != "" {
				continue
			}
			password, sealed, err := p.newSealedDatabaseAdminPassword()
			if err != nil {
				return err
			}
			var resp transport.ProvisionDatabaseAdminAccountResponse
			if err := p.callAgentContext(ctx, "Agent.ProvisionDatabaseAdminAccount",
				&transport.ProvisionDatabaseAdminAccountRequest{
					Engine: engine, Password: password,
				}, &resp); err != nil {
				return err
			}
			if !resp.Provisioned {
				return fmt.Errorf("%s", resp.Error)
			}
			server.AdminUsername = resp.Username
			server.AdminPasswordEncrypted = sealed
			p.recordEngineVersion(server)
			if err := serverRepo.Update(ctx, server); err != nil {
				return err
			}
		}
	}
	return nil
}
