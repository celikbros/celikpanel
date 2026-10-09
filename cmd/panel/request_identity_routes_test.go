package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
	"github.com/alicelik/celikpanel/internal/core"
	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/transport"
)

// D-029, batch 1. Each of the eight routes is driven through the dispatcher
// the product uses, with its real handler and a fake Agent (or database
// driver), behind the guard. The same request is sent three times in a row
// and three times at once. What is counted is the harmful effect itself: the
// Agent call that restores, issues, creates or re-keys.
//
// D-029, birinci grup. Sekiz rotanın her biri ürünün kullandığı yönlendirici
// üzerinden, gerçek işleyicisi ve sahte bir Agent ile, korumanın arkasında
// sürülür. Aynı istek art arda üç kez ve aynı anda üç kez gönderilir. Sayılan
// şey zararlı etkinin kendisidir.

// requestIdentityRoutesMux registers the eight routes the way main does.
// TestRequestIdentityRoutesMuxMirrorsMain keeps the two the same.
func requestIdentityRoutesMux(p *Panel) *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/domains/", p.handleDomainSubroute)
	mux.HandleFunc("/api/v1/database-servers/", func(w http.ResponseWriter, r *http.Request) {
		p.handleDatabaseSubroute(w, r, "/api/v1/database-servers/")
	})
	mux.HandleFunc("/api/v1/vpn/peers", p.handleVPNPeers)
	mux.HandleFunc("/api/v1/import/cpanel/apply", p.handleImportApply)
	return mux
}

func TestRequestIdentityRoutesMuxMirrorsMain(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatal(err)
	}
	text := string(source)
	for _, registration := range []string{
		`http.HandleFunc("/api/v1/domains/", panel.handleDomainSubroute)`,
		"http.HandleFunc(\"/api/v1/database-servers/\", func(w http.ResponseWriter, r *http.Request) {\n\t\tpanel.handleDatabaseSubroute(w, r, \"/api/v1/database-servers/\")",
		`http.HandleFunc("/api/v1/vpn/peers", panel.handleVPNPeers)`,
		`http.HandleFunc("/api/v1/import/cpanel/apply", panel.handleImportApply)`,
		"panel.requireAuth(panel.requestIdentities.wrap(http.DefaultServeMux))",
		"panel.requestIdentities.markInterruptedAtStart(context.Background())",
		"panel.requestIdentities.sweepExpired(context.Background())",
	} {
		if !strings.Contains(text, registration) {
			t.Fatalf("main.go no longer contains: %s", registration)
		}
	}
}

// requestIdentityRoutesAgent is the fake Agent of the import and
// domain-database routes.
type requestIdentityRoutesAgent struct {
	verifiedAPTAgentRPCFixture

	mu                  sync.Mutex
	preview             transport.CpmoveInspectResponse
	inspectCalls        int
	installedCalls      int
	createDatabaseCalls int
	provisionCalls      int
	inspectEntered      chan struct{}
	inspectRelease      chan struct{}
}

func (a *requestIdentityRoutesAgent) InspectCpmove(
	_ *transport.CpmoveInspectRequest, resp *transport.CpmoveInspectResponse,
) error {
	a.mu.Lock()
	a.inspectCalls++
	first := a.inspectCalls == 1
	entered, release := a.inspectEntered, a.inspectRelease
	*resp = a.preview
	a.mu.Unlock()
	if first && entered != nil {
		close(entered)
		<-release
	}
	return nil
}

func (a *requestIdentityRoutesAgent) InstalledServiceIDsStrict(_ *transport.Empty, _ *[]string) error {
	a.mu.Lock()
	a.installedCalls++
	a.mu.Unlock()
	return errors.New("the fake Agent stops the import here")
}

func (a *requestIdentityRoutesAgent) GetServices(_ *transport.Empty, reply *[]core.Service) error {
	*reply = []core.Service{{Name: "mariadb.service", Version: "11.4", Status: "running"}}
	return nil
}

func (a *requestIdentityRoutesAgent) ProvisionDatabaseAdminAccount(
	_ *transport.ProvisionDatabaseAdminAccountRequest,
	resp *transport.ProvisionDatabaseAdminAccountResponse,
) error {
	a.mu.Lock()
	a.provisionCalls++
	a.mu.Unlock()
	resp.Username = transport.DatabaseAdminAccountName
	resp.Provisioned = true
	return nil
}

func (a *requestIdentityRoutesAgent) CreateDatabase(
	_ *transport.CreateDatabaseRequest, resp *transport.CreateDatabaseResponse,
) error {
	a.mu.Lock()
	a.createDatabaseCalls++
	a.mu.Unlock()
	// Long enough for two handlers to overlap if two were ever started.
	time.Sleep(3 * time.Millisecond)
	resp.Success = true
	return nil
}

func (a *requestIdentityRoutesAgent) counts() (inspect, installed, createDatabase int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.inspectCalls, a.installedCalls, a.createDatabaseCalls
}

func attachRequestIdentityAgent(t *testing.T, panel *Panel, agent any) {
	t.Helper()
	panel.pkgFamilyMu.Lock()
	panel.pkgFamilyVal = "apt"
	panel.hostPlatformVal = debianPolicyTestIdentity()
	panel.hostPlatformKnown = true
	panel.pkgFamilyMu.Unlock()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	panel.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

// newRequestIdentityHostingFixture: one administrator, one subscription, one
// domain, and the fake Agent above.
func newRequestIdentityHostingFixture(t *testing.T) (*Panel, *requestIdentityRoutesAgent, int, int, int) {
	t.Helper()
	directory := t.TempDir()
	database, err := paneldb.NewSQLiteDB(filepath.Join(directory, "panel.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	mustID := func(query string, args ...any) int {
		result, err := database.GetDB().Exec(query, args...)
		if err != nil {
			t.Fatalf("seed %q: %v", query, err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			t.Fatal(err)
		}
		return int(id)
	}
	userID := mustID(`INSERT INTO users (username,password_hash,email,role,status) VALUES ('request-admin','x','request@example.test','admin','active')`)
	subscriptionID := mustID(`INSERT INTO subscriptions (owner_id,name,status) VALUES (?, 'request-sub', 'active')`, userID)
	domainID := mustID(`INSERT INTO domains (subscription_id,name,status) VALUES (?, 'request.example', 'active')`, subscriptionID)
	box, err := secrets.LoadOrCreate(filepath.Join(directory, "panel.key"))
	if err != nil {
		t.Fatal(err)
	}
	panel := &Panel{db: database, secrets: box}
	agent := &requestIdentityRoutesAgent{}
	attachRequestIdentityAgent(t, panel, agent)
	return panel, agent, userID, subscriptionID, domainID
}

type requestIdentityVPNAgent struct {
	*serviceOperationTestAgent

	keyMu    sync.Mutex
	keyCalls int
}

func (a *requestIdentityVPNAgent) VPNStatus(_ *transport.Empty, out *transport.VPNStatusResponse) error {
	*out = transport.VPNStatusResponse{
		Installed: true, Configured: true, Running: true,
		ServerPublicKey: vpnTestCanonicalKey(900, 9), Port: vpnFixedPort, Endpoint: "203.0.113.10",
	}
	return nil
}

func (a *requestIdentityVPNAgent) GenerateVPNKeys(_ *transport.Empty, out *transport.VPNKeysResponse) error {
	a.keyMu.Lock()
	a.keyCalls++
	sequence := int64(1000 + a.keyCalls)
	a.keyMu.Unlock()
	*out = transport.VPNKeysResponse{
		PrivateKey:   vpnTestCanonicalKey(sequence, 3),
		PublicKey:    vpnTestCanonicalKey(sequence, 1),
		PresharedKey: vpnTestCanonicalKey(sequence, 2),
	}
	return nil
}

// requestIdentityRouteCase is one protected route with everything a test
// needs to send it and to count what it did.
type requestIdentityRouteCase struct {
	handler http.Handler
	// unguarded reaches the same dispatcher without the guard.
	unguarded http.Handler
	path      string
	body      string
	caller    *Caller
	// effects counts the harmful effect.
	effects func() int
	// wantStatus is the status of the one real answer.
	wantStatus int
	// oneTime: the real answer is given once; replays are told it is not kept.
	oneTime bool
	// secret must never appear in a replay or in the row.
	secret string
	// unguardedEffects is what three sends in a row did before D-029.
	unguardedEffects int
	// unguardedCheck names what else those three sends did, where the count
	// alone does not say it.
	unguardedCheck func(t *testing.T)
	// check runs after the sends, with the request identity that was used.
	check func(t *testing.T, id string)
	// db is the Panel database the guard writes its rows to.
	db *sql.DB
}

func (c requestIdentityRouteCase) send(handler http.Handler, id string) *httptest.ResponseRecorder {
	recorder := httptest.NewRecorder()
	request := requestIdentityTestRequest(c.path, c.body, id, c.caller)
	request.Header.Set("Origin", "https://"+request.Host)
	handler.ServeHTTP(recorder, request)
	return recorder
}

func newRequestIdentityRouteCases() map[string]func(t *testing.T) requestIdentityRouteCase {
	wrap := func(p *Panel) requestIdentityRouteCase {
		mux := requestIdentityRoutesMux(p)
		return requestIdentityRouteCase{
			handler: newRequestIdentityGuard(p.db.GetDB()).wrap(mux), unguarded: mux, db: p.db.GetDB(),
		}
	}
	firstUser := func(t *testing.T, p *Panel) int {
		t.Helper()
		var id int
		if err := p.db.GetDB().QueryRow(`SELECT id FROM users ORDER BY id LIMIT 1`).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	const backupName = "files-20261009T120000.000000000Z-0123456789abcdef.cpbak"

	return map[string]func(t *testing.T) requestIdentityRouteCase{
		"POST domains/{id}/backups/restore": func(t *testing.T) requestIdentityRouteCase {
			f := newPanelBackupFixture(t)
			f.agent.inspectResp = backupspec.InspectResponse{
				Success: true,
				Backup:  backupspec.Info{Name: backupName, Type: backupspec.TypeFiles, Restorable: true},
			}
			base := wrap(f.panel)
			c := requestIdentityRouteCase{
				path:   fmt.Sprintf("/api/v1/domains/%d/backups/restore", f.domainID),
				body:   `{"backup_name":"` + backupName + `"}`,
				caller: &Caller{ID: firstUser(t, f.panel), Role: roleAdmin},
				effects: func() int {
					f.agent.mu.Lock()
					defer f.agent.mu.Unlock()
					return len(f.agent.restoreReqs)
				},
				wantStatus: http.StatusOK, unguardedEffects: 3,
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST domains/{id}/backups": func(t *testing.T) requestIdentityRouteCase {
			f := newPanelBackupFixture(t)
			base := wrap(f.panel)
			c := requestIdentityRouteCase{
				path:   fmt.Sprintf("/api/v1/domains/%d/backups", f.domainID),
				body:   `{"type":"files"}`,
				caller: &Caller{ID: firstUser(t, f.panel), Role: roleAdmin},
				effects: func() int {
					f.agent.mu.Lock()
					defer f.agent.mu.Unlock()
					return len(f.agent.createReqs)
				},
				wantStatus: http.StatusOK, unguardedEffects: 3,
				check: func(t *testing.T, id string) {
					f.agent.mu.Lock()
					defer f.agent.mu.Unlock()
					// The Agent's own job lock and published-backup lookup are
					// keyed by the request.
					if got := f.agent.createReqs[0].JobKey; got != "request:"+id || !backupspec.ValidJobKey(got) {
						t.Fatalf("backup job key=%q, want the request identity", got)
					}
					if f.agent.createReqs[0].Origin != backupspec.OriginManual {
						t.Fatalf("origin=%q", f.agent.createReqs[0].Origin)
					}
				},
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST import/cpanel/apply": func(t *testing.T) requestIdentityRouteCase {
			panel, agent, userID, subscriptionID, _ := newRequestIdentityHostingFixture(t)
			agent.preview = transport.CpmoveInspectResponse{
				Username: "olduser", MainDomain: "imported.example", Domains: []string{"imported.example"},
			}
			base := wrap(panel)
			c := requestIdentityRouteCase{
				path:   "/api/v1/import/cpanel/apply",
				body:   fmt.Sprintf(`{"path":"/var/lib/celikpanel-imports/cpmove-olduser.tar.gz","subscription_id":%d,"domain":"imported.example","do_files":true}`, subscriptionID),
				caller: &Caller{ID: userID, Role: roleAdmin},
				effects: func() int {
					inspect, _, _ := agent.counts()
					return inspect
				},
				// The fake Agent ends the import at its second call; the
				// handler's own answer to that is what every arrival gets.
				wantStatus: http.StatusInternalServerError, unguardedEffects: 3,
				check: func(t *testing.T, _ string) {
					if _, installed, _ := agent.counts(); installed != 1 {
						t.Fatalf("the import went on to its second Agent call %d times, want once", installed)
					}
				},
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST domains/{id}/ssl/letsencrypt (reissue)": func(t *testing.T) requestIdentityRouteCase {
			const domain = "reissue-identity.example"
			p, domainID := newIncludeMailFixture(t, domain)
			if _, err := p.db.GetDB().Exec(`
				INSERT INTO ssl_certificates (
					domain_id, type, cert_path, key_path, issuer, subject,
					issued_at, expires_at, auto_renew, secure_mail, status
				) VALUES (?, 'custom', '/certs/custom/fullchain.pem',
				          '/certs/custom/privkey.pem', 'Private CA', ?,
				          '2026-07-01T00:00:00Z', '2027-07-01T00:00:00Z', 0, 0, 'active')`,
				domainID, domain); err != nil {
				t.Fatal(err)
			}
			if _, err := p.db.GetDB().Exec(`
				UPDATE sites SET ssl_enabled = 1, ssl_type = 'custom',
				    ssl_cert_path = '/certs/custom/fullchain.pem',
				    ssl_key_path = '/certs/custom/privkey.pem'
				WHERE domain_id = ?`, domainID); err != nil {
				t.Fatal(err)
			}
			agent := &validationRestoreAgent{domain: domain}
			attachValidationRestoreAgent(t, p, agent)
			base := wrap(p)
			c := requestIdentityRouteCase{
				path:   fmt.Sprintf("/api/v1/domains/%d/ssl/letsencrypt", domainID),
				body:   `{"email":"admin@reissue-identity.example","auto_renew":true,"reissue":true}`,
				caller: &Caller{ID: firstUser(t, p), Role: roleAdmin},
				effects: func() int {
					agent.mu.Lock()
					defer agent.mu.Unlock()
					return len(agent.issueCalls)
				},
				wantStatus: http.StatusOK, unguardedEffects: 3,
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST database-servers/{id}/admin-account": func(t *testing.T) requestIdentityRouteCase {
			agent := &databaseAdminAccountAgent{services: adminAccountServices()}
			panel, _ := newDatabaseAdminAccountFixture(t, agent)
			if err := panel.ensureInstalledDBServers(context.Background(), adminAccountSubscriptionID); err != nil {
				t.Fatal(err)
			}
			server := onlyDatabaseServer(t, panel)
			opened := len(agent.provisioned)
			base := wrap(panel)
			c := requestIdentityRouteCase{
				path:       "/api/v1/database-servers/" + strconv.Itoa(server.ID) + "/admin-account",
				body:       ``,
				caller:     &Caller{ID: adminAccountUserID, Role: roleAdmin},
				effects:    func() int { return len(agent.provisioned) - opened },
				wantStatus: http.StatusOK, oneTime: true, unguardedEffects: 3,
				check: func(t *testing.T, _ string) {
					// The engine and the Panel hold the same password.
					current := onlyDatabaseServer(t, panel)
					held, err := panel.secrets.Decrypt(current.AdminPasswordEncrypted)
					if err != nil {
						t.Fatal(err)
					}
					if last := agent.provisioned[len(agent.provisioned)-1].Password; held != last {
						t.Fatal("the Panel holds a password the engine was not last given")
					}
				},
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST vpn/peers": func(t *testing.T) requestIdentityRouteCase {
			fixture, _, _, subscriptionID := newVPNSecurityFixture(t)
			agent := &requestIdentityVPNAgent{serviceOperationTestAgent: fixture.agent}
			attachVPNTestAgent(t, fixture.panel, agent)
			base := wrap(fixture.panel)
			c := requestIdentityRouteCase{
				path:   "/api/v1/vpn/peers",
				body:   fmt.Sprintf(`{"name":"laptop","subscription_id":%d}`, subscriptionID),
				caller: &Caller{ID: fixture.userID, Role: roleAdmin},
				effects: func() int {
					var peers int
					if err := fixture.database.GetDB().QueryRow(`SELECT COUNT(*) FROM vpn_peers`).Scan(&peers); err != nil {
						t.Fatal(err)
					}
					return peers
				},
				wantStatus: http.StatusOK, oneTime: true, secret: "PrivateKey", unguardedEffects: 3,
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST database-servers/{id}/databases (existing user)": func(t *testing.T) requestIdentityRouteCase {
			fixture := newDatabaseV2SecurityFixture(t)
			driver := useRecordingDatabaseDriver(t)
			base := wrap(fixture.panel)
			c := requestIdentityRouteCase{
				path:       fmt.Sprintf("/api/v1/database-servers/%d/databases", dbSecurityServer),
				body:       fmt.Sprintf(`{"database_name":"shop","domain_id":%d,"user_id":%d}`, dbSecurityDomain, dbSecurityUser),
				caller:     &Caller{ID: dbSecurityOwnerID, Role: roleCustomer},
				effects:    func() int { return driver.createDatabaseCalls },
				wantStatus: http.StatusOK, unguardedEffects: 3,
				check: func(t *testing.T, _ string) {
					if driver.deleteDatabaseCalls != 0 {
						t.Fatalf("a compensation deleted the database %d times", driver.deleteDatabaseCalls)
					}
				},
				// The engine accepts the second CREATE (MariaDB: IF NOT
				// EXISTS), the metadata row already exists, and the replay's
				// compensation then drops the database the first request
				// created and recorded.
				unguardedCheck: func(t *testing.T) {
					var recorded int
					if err := fixture.sql.QueryRow(`SELECT COUNT(*) FROM databases_v2 WHERE name LIKE '%shop'`).Scan(&recorded); err != nil {
						t.Fatal(err)
					}
					if recorded != 1 || driver.deleteDatabaseCalls != 2 {
						t.Fatalf("recorded databases=%d, engine deletions=%d; want the recorded database deleted by each replay",
							recorded, driver.deleteDatabaseCalls)
					}
				},
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST database-servers/{id}/databases (new user, one-time password)": func(t *testing.T) requestIdentityRouteCase {
			fixture := newDatabaseV2SecurityFixture(t)
			driver := useRecordingDatabaseDriver(t)
			base := wrap(fixture.panel)
			c := requestIdentityRouteCase{
				path:       fmt.Sprintf("/api/v1/database-servers/%d/databases", dbSecurityServer),
				body:       `{"database_name":"shop","new_username":"shopper"}`,
				caller:     &Caller{ID: dbSecurityOwnerID, Role: roleCustomer},
				effects:    func() int { return driver.createDatabaseCalls },
				wantStatus: http.StatusOK, oneTime: true, secret: `"password"`, unguardedEffects: 3,
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
		"POST domains/{id}/databases": func(t *testing.T) requestIdentityRouteCase {
			panel, agent, userID, _, domainID := newRequestIdentityHostingFixture(t)
			base := wrap(panel)
			c := requestIdentityRouteCase{
				path:   fmt.Sprintf("/api/v1/domains/%d/databases", domainID),
				body:   `{"name":"shop","type":"mysql","password":"a-long-enough-password"}`,
				caller: &Caller{ID: userID, Role: roleAdmin},
				effects: func() int {
					_, _, created := agent.counts()
					return created
				},
				// In a row the second send finds the first one's row and is
				// refused; the harm of this route is the overlap.
				wantStatus: http.StatusOK, unguardedEffects: 1,
			}
			c.handler, c.unguarded, c.db = base.handler, base.unguarded, base.db
			return c
		},
	}
}

func assertRequestIdentityReplay(t *testing.T, c requestIdentityRouteCase, id string, first, replay *httptest.ResponseRecorder) {
	t.Helper()
	if c.secret != "" && strings.Contains(replay.Body.String(), c.secret) {
		t.Fatalf("a replay carried the one-time secret: %s", replay.Body.String())
	}
	if c.oneTime {
		body := decodeRequestIdentityRefusal(t, replay)
		if replay.Code != http.StatusConflict || body.Code != errCodeRequestResultNotKept ||
			body.Reason != "" || body.Vars["request_id"] != id {
			t.Fatalf("replay of a one-time answer: status=%d body=%s", replay.Code, replay.Body.String())
		}
		return
	}
	if replay.Code != first.Code || replay.Body.String() != first.Body.String() {
		t.Fatalf("replay differs from the first answer:\nfirst  %d %s\nreplay %d %s",
			first.Code, first.Body.String(), replay.Code, replay.Body.String())
	}
}

func TestRequestIdentityEightRoutesSentThreeTimesInARow(t *testing.T) {
	for name, build := range newRequestIdentityRouteCases() {
		t.Run(name, func(t *testing.T) {
			c := build(t)
			id := requestIdentityTestID(4096)
			first := c.send(c.handler, id)
			if first.Code != c.wantStatus {
				t.Fatalf("first answer: status=%d body=%s", first.Code, first.Body.String())
			}
			if c.secret != "" && !strings.Contains(first.Body.String(), c.secret) {
				t.Fatalf("the first arrival did not get its one-time result: %s", first.Body.String())
			}
			for attempt := 0; attempt < 2; attempt++ {
				assertRequestIdentityReplay(t, c, id, first, c.send(c.handler, id))
			}
			if got := c.effects(); got != 1 {
				t.Fatalf("the request took effect %d times, want once", got)
			}
			if c.check != nil {
				c.check(t, id)
			}
			// Without the header the route is refused before the handler.
			refused := c.send(c.handler, "")
			if refused.Code != http.StatusPreconditionRequired || decodeRequestIdentityRefusal(t, refused).Code != errCodeRequestIDRequired {
				t.Fatalf("no identity: status=%d body=%s", refused.Code, refused.Body.String())
			}
			if got := c.effects(); got != 1 {
				t.Fatalf("a request without identity took effect (%d effects)", got)
			}
			row := readRequestIdentityRow(t, c.db, id)
			if row.status != requestIdentityDone || row.code != c.wantStatus {
				t.Fatalf("row=%+v", row)
			}
			if c.oneTime && (row.retained != 0 || row.body != nil) {
				t.Fatalf("a one-time answer was stored: retained=%d bytes=%d", row.retained, len(row.body))
			}
			if !c.oneTime && string(row.body) != first.Body.String() {
				t.Fatalf("stored answer differs from the first answer: %q", row.body)
			}
		})
	}
}

func TestRequestIdentityEightRoutesSentThreeTimesAtOnce(t *testing.T) {
	for name, build := range newRequestIdentityRouteCases() {
		t.Run(name, func(t *testing.T) {
			c := build(t)
			id := requestIdentityTestID(8192)
			const arrivals = 3
			answers := make([]*httptest.ResponseRecorder, arrivals)
			start := make(chan struct{})
			var group sync.WaitGroup
			for index := 0; index < arrivals; index++ {
				group.Add(1)
				go func(index int) {
					defer group.Done()
					<-start
					answers[index] = c.send(c.handler, id)
				}(index)
			}
			close(start)
			group.Wait()

			if got := c.effects(); got != 1 {
				t.Fatalf("three arrivals at once took effect %d times, want once", got)
			}
			if c.oneTime {
				real := 0
				for _, answer := range answers {
					if answer.Code == c.wantStatus {
						real++
						continue
					}
					assertRequestIdentityReplay(t, c, id, nil, answer)
				}
				if real != 1 {
					t.Fatalf("%d arrivals got the one-time answer, want exactly one", real)
				}
			} else {
				for index, answer := range answers {
					if answer.Code != c.wantStatus {
						t.Fatalf("arrival %d: status=%d body=%s", index, answer.Code, answer.Body.String())
					}
					if answer.Body.String() != answers[0].Body.String() {
						t.Fatalf("arrival %d got a different answer:\n%s\n%s", index, answer.Body.String(), answers[0].Body.String())
					}
				}
			}
			if c.check != nil {
				c.check(t, id)
			}
		})
	}
}

// The confirmed bad outcome, kept as a test of the tests: the same three sends
// straight to the dispatcher, as before D-029, take effect more than once.
// Doğrulanan kötü sonuç: aynı üç gönderim, D-029 öncesindeki gibi doğrudan
// yönlendiriciye gittiğinde birden çok kez etki eder.
func TestRequestIdentityEightRoutesRepeatTheirEffectWithoutTheGuard(t *testing.T) {
	for name, build := range newRequestIdentityRouteCases() {
		t.Run(name, func(t *testing.T) {
			c := build(t)
			for attempt := 0; attempt < 3; attempt++ {
				c.send(c.unguarded, "")
			}
			if got := c.effects(); got != c.unguardedEffects {
				t.Fatalf("unguarded effects=%d, want %d", got, c.unguardedEffects)
			}
			if c.unguardedCheck != nil {
				c.unguardedCheck(t)
			}
		})
	}
}

// The first import is not cut when the browser's connection goes away: the
// handler goes on to its next Agent call on a context that is still alive, its
// answer is recorded, and the replay is answered from that record.
// Tarayıcının bağlantısı gittiğinde ilk içe aktarım kesilmez.
func TestImportApplyIsNotCutWhenTheConnectionGoesAway(t *testing.T) {
	panel, agent, userID, subscriptionID, _ := newRequestIdentityHostingFixture(t)
	agent.preview = transport.CpmoveInspectResponse{
		Username: "olduser", MainDomain: "imported.example", Domains: []string{"imported.example"},
	}
	agent.inspectEntered = make(chan struct{})
	agent.inspectRelease = make(chan struct{})
	guard := newRequestIdentityGuard(panel.db.GetDB())
	handler := guard.wrap(requestIdentityRoutesMux(panel))
	body := fmt.Sprintf(`{"path":"/var/lib/celikpanel-imports/cpmove-olduser.tar.gz","subscription_id":%d,"domain":"imported.example"}`, subscriptionID)
	id := requestIdentityTestID(12288)
	caller := &Caller{ID: userID, Role: roleAdmin}

	connection, hangUp := context.WithCancel(context.Background())
	request := requestIdentityTestRequest("/api/v1/import/cpanel/apply", body, id, nil)
	request = request.WithContext(context.WithValue(connection, callerKey, caller))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}()
	<-agent.inspectEntered
	hangUp()
	close(agent.inspectRelease)
	<-finished

	inspect, installed, _ := agent.counts()
	if inspect != 1 || installed != 1 {
		t.Fatalf("after the client left: inspect=%d next call=%d, want the import to go on once", inspect, installed)
	}
	row := readRequestIdentityRow(t, panel.db.GetDB(), id)
	if row.status != requestIdentityDone || row.code != http.StatusInternalServerError || row.retained != 1 {
		t.Fatalf("row=%+v", row)
	}
	replay := httptest.NewRecorder()
	handler.ServeHTTP(replay, requestIdentityTestRequest("/api/v1/import/cpanel/apply", body, id, caller))
	if replay.Code != http.StatusInternalServerError || replay.Header().Get(requestIdentityReplayedHeader) != "1" {
		t.Fatalf("replay: status=%d body=%s", replay.Code, replay.Body.String())
	}
	if inspect, installed, _ := agent.counts(); inspect != 1 || installed != 1 {
		t.Fatalf("the replay ran the import again: inspect=%d next=%d", inspect, installed)
	}
}

// Before D-029 the same cut left the Panel stopped between two Agent calls:
// the connection's context was the handler's context.
// D-029'dan önce aynı kesinti Panel'i iki Agent çağrısı arasında durduruyordu.
func TestImportApplyWithoutTheGuardStopsWhenTheConnectionGoesAway(t *testing.T) {
	panel, agent, userID, subscriptionID, _ := newRequestIdentityHostingFixture(t)
	agent.preview = transport.CpmoveInspectResponse{
		Username: "olduser", MainDomain: "imported.example", Domains: []string{"imported.example"},
	}
	agent.inspectEntered = make(chan struct{})
	agent.inspectRelease = make(chan struct{})
	body := fmt.Sprintf(`{"path":"/var/lib/celikpanel-imports/cpmove-olduser.tar.gz","subscription_id":%d,"domain":"imported.example"}`, subscriptionID)
	connection, hangUp := context.WithCancel(context.Background())
	request := requestIdentityTestRequest("/api/v1/import/cpanel/apply", body, "", nil)
	request = request.WithContext(context.WithValue(connection, callerKey, &Caller{ID: userID, Role: roleAdmin}))
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		requestIdentityRoutesMux(panel).ServeHTTP(httptest.NewRecorder(), request)
	}()
	<-agent.inspectEntered
	hangUp()
	close(agent.inspectRelease)
	<-finished
	if _, installed, _ := agent.counts(); installed != 0 {
		t.Fatalf("the unguarded handler went on after the connection was cancelled (%d calls)", installed)
	}
}

// Two changes of the same server's account with different identities are not
// the same request, so both run; they run one after the other, and the Panel
// ends up holding the password the engine was given last.
// Aynı sunucunun hesabında farklı kimlikli iki değişiklik sırayla çalışır.
func TestDatabaseAdminAccountChangesAreSerialisedPerServer(t *testing.T) {
	agent := &serialisingAdminAccountAgent{}
	panel, _ := newDatabaseAdminAccountFixture(t, &databaseAdminAccountAgent{services: adminAccountServices()})
	if err := panel.ensureInstalledDBServers(context.Background(), adminAccountSubscriptionID); err != nil {
		t.Fatal(err)
	}
	server := onlyDatabaseServer(t, panel)
	attachRequestIdentityAgent(t, panel, agent)
	handler := newRequestIdentityGuard(panel.db.GetDB()).wrap(requestIdentityRoutesMux(panel))
	path := "/api/v1/database-servers/" + strconv.Itoa(server.ID) + "/admin-account"
	caller := &Caller{ID: adminAccountUserID, Role: roleAdmin}

	const changes = 6
	var group sync.WaitGroup
	for index := 0; index < changes; index++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, requestIdentityTestRequest(path, ``, requestIdentityTestID(20000+index), caller))
			if recorder.Code != http.StatusOK {
				t.Errorf("change %d: status=%d body=%s", index, recorder.Code, recorder.Body.String())
			}
		}(index)
	}
	group.Wait()

	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.passwords) != changes {
		t.Fatalf("the engine was asked %d times, want %d", len(agent.passwords), changes)
	}
	if agent.overlapped {
		t.Fatal("two changes of the same account were inside the Agent at once")
	}
	held, err := panel.secrets.Decrypt(onlyDatabaseServer(t, panel).AdminPasswordEncrypted)
	if err != nil {
		t.Fatal(err)
	}
	if held != agent.passwords[len(agent.passwords)-1] {
		t.Fatal("the Panel holds a password that is not the one the engine was given last")
	}
}

type serialisingAdminAccountAgent struct {
	verifiedAPTAgentRPCFixture

	mu         sync.Mutex
	inside     int
	overlapped bool
	passwords  []string
}

func (a *serialisingAdminAccountAgent) ProvisionDatabaseAdminAccount(
	req *transport.ProvisionDatabaseAdminAccountRequest,
	resp *transport.ProvisionDatabaseAdminAccountResponse,
) error {
	a.mu.Lock()
	a.inside++
	if a.inside > 1 {
		a.overlapped = true
	}
	a.mu.Unlock()
	time.Sleep(2 * time.Millisecond)
	a.mu.Lock()
	a.inside--
	a.passwords = append(a.passwords, req.Password)
	a.mu.Unlock()
	resp.Username = transport.DatabaseAdminAccountName
	resp.Provisioned = true
	return nil
}

// The Agent's refusal of a second restore of the same domain is a named
// refusal with a next step, never an opaque 500.
// Agent'ın aynı alan adının ikinci geri yüklemesini reddi adlı bir rettir.
func TestRestoreRefusedByTheAgentWhileAnotherRunsIsANamedRefusal(t *testing.T) {
	f := newPanelBackupFixture(t)
	const backupName = "files-20261009T120000.000000000Z-0123456789abcdef.cpbak"
	f.agent.inspectResp = backupspec.InspectResponse{
		Success: true,
		Backup:  backupspec.Info{Name: backupName, Type: backupspec.TypeFiles, Restorable: true},
	}
	f.agent.restoreErr = nil
	f.agent.restoreOK = false
	busy := &restoreBusyAgent{panelBackupV2Agent: f.agent}
	attachRequestIdentityAgent(t, f.panel, busy)
	request := httptest.NewRequest(http.MethodPost,
		fmt.Sprintf("/api/v1/domains/%d/backups/restore", f.domainID),
		strings.NewReader(`{"backup_name":"`+backupName+`"}`))
	recorder := httptest.NewRecorder()
	f.panel.handleRestoreBackup(recorder, request)
	var body apiErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if recorder.Code != http.StatusConflict || body.Code != errCodeBackupRestoreInProgress ||
		body.Error != backupRestoreInProgressMessage {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !hasBackupAction(backupActions(t, f.panel), "backup.restore.failed") {
		t.Fatal("the refused restore left no audit entry")
	}
}

type restoreBusyAgent struct {
	*panelBackupV2Agent
}

func (a *restoreBusyAgent) RestoreBackup(_ *backupspec.RestoreRequest, resp *backupspec.RestoreResponse) error {
	resp.Error = backupspec.RestoreInProgress
	return nil
}
