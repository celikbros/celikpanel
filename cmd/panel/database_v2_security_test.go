package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	paneldb "github.com/alicelik/celikpanel/internal/db"
	"github.com/alicelik/celikpanel/internal/secrets"
	"github.com/alicelik/celikpanel/internal/services"
)

const (
	dbSecurityOwnerID       = 9101
	dbSecurityForeignID     = 9102
	dbSecuritySubscription  = 9201
	dbSecurityForeignSub    = 9202
	dbSecurityServer        = 9301
	dbSecurityForeignServer = 9302
	dbSecurityDomain        = 9401
	dbSecurityForeignDomain = 9402
	dbSecurityUser          = 9501
	dbSecurityForeignUser   = 9502
)

type recordingDatabaseDriver struct {
	factoryCalls        int
	createDatabaseCalls int
	deleteDatabaseCalls int
	createUserCalls     int
	deleteUserCalls     int
	grantCalls          int
	revokeCalls         int
	createdUser         string
	createdPassword     string
	events              []string
	createDatabaseErr   error
	deleteDatabaseErr   error
	createUserErr       error
	deleteUserErr       error
	grantErr            error
	revokeErr           error
}

func (d *recordingDatabaseDriver) TestConnection() error { return nil }
func (d *recordingDatabaseDriver) CreateDatabase(name string) error {
	d.createDatabaseCalls++
	d.events = append(d.events, "create-database:"+name)
	return d.createDatabaseErr
}
func (d *recordingDatabaseDriver) DeleteDatabase(name string) error {
	d.deleteDatabaseCalls++
	d.events = append(d.events, "delete-database:"+name)
	return d.deleteDatabaseErr
}
func (d *recordingDatabaseDriver) ListDatabases() ([]string, error) {
	return nil, nil
}
func (d *recordingDatabaseDriver) CreateUser(username, password string) error {
	d.createUserCalls++
	d.createdUser = username
	d.createdPassword = password
	d.events = append(d.events, "create-user:"+username)
	return d.createUserErr
}
func (d *recordingDatabaseDriver) DeleteUser(username string) error {
	d.deleteUserCalls++
	d.events = append(d.events, "delete-user:"+username)
	return d.deleteUserErr
}
func (d *recordingDatabaseDriver) ChangePassword(string, string) error {
	return nil
}
func (d *recordingDatabaseDriver) ListUsers() ([]string, error) { return nil, nil }
func (d *recordingDatabaseDriver) GrantPrivileges(database, username, privileges string) error {
	d.grantCalls++
	d.events = append(
		d.events,
		"grant:"+database+":"+username+":"+privileges,
	)
	return d.grantErr
}
func (d *recordingDatabaseDriver) RevokePrivileges(database, username string) error {
	d.revokeCalls++
	d.events = append(d.events, "revoke:"+database+":"+username)
	return d.revokeErr
}

type databaseV2SecurityFixture struct {
	panel *Panel
	sql   *sql.DB
	box   *secrets.Box
}

func newDatabaseV2SecurityFixture(t *testing.T) databaseV2SecurityFixture {
	t.Helper()
	database, err := paneldb.NewSQLiteDB(filepath.Join(t.TempDir(), `panel.sqlite`))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(database.Close)
	box, err := secrets.LoadOrCreate(filepath.Join(t.TempDir(), `secrets.key`))
	if err != nil {
		t.Fatal(err)
	}
	rootSecret, err := box.Encrypt(`root-secret`)
	if err != nil {
		t.Fatal(err)
	}
	targetSecret, err := box.Encrypt(`target-user-secret`)
	if err != nil {
		t.Fatal(err)
	}
	foreignSecret, err := box.Encrypt(`foreign-user-secret`)
	if err != nil {
		t.Fatal(err)
	}
	sqlDB := database.GetDB()
	_, err = sqlDB.Exec(`
		INSERT INTO users (id, username, password_hash, email, role, status)
		VALUES
			(9101, 'db-owner', 'x', 'db-owner@example.test', 'customer', 'active'),
			(9102, 'db-foreign', 'x', 'db-foreign@example.test', 'customer', 'active');
		INSERT INTO subscriptions (id, owner_id, name, status)
		VALUES
			(9201, 9101, 'DB owner subscription', 'active'),
			(9202, 9102, 'DB foreign subscription', 'active');
		INSERT INTO database_servers
			(id, subscription_id, type_id, name, version, host, port, root_password_encrypted, status)
		VALUES
			(9301, 9201, 2, 'owner-logical-server', 'test', '127.0.0.1', 3306, ?, 'active'),
			(9302, 9202, 2, 'foreign-logical-server', 'test', '127.0.0.1', 3306, ?, 'active');
		INSERT INTO domains (id, subscription_id, name, status)
		VALUES
			(9401, 9201, 'owner-db.example', 'active'),
			(9402, 9202, 'foreign-db.example', 'active');
		INSERT INTO database_users
			(id, server_id, subscription_id, username, password)
		VALUES
			(9501, 9301, 9201, '9201_owner', ?),
			(9502, 9302, 9202, '9202_foreign', ?);
	`, rootSecret, rootSecret, targetSecret, foreignSecret)
	if err != nil {
		t.Fatal(err)
	}
	return databaseV2SecurityFixture{
		panel: &Panel{db: database, secrets: box},
		sql:   sqlDB,
		box:   box,
	}
}

func (f databaseV2SecurityFixture) request(body string) *http.Request {
	return f.requestAsOwner(
		http.MethodPost,
		fmt.Sprintf(`/api/v2/database-servers/%d/databases`, dbSecurityServer),
		body,
	)
}

func (f databaseV2SecurityFixture) requestAsOwner(
	method string,
	path string,
	body string,
) *http.Request {
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	return request.WithContext(context.WithValue(
		request.Context(),
		callerKey,
		&Caller{ID: dbSecurityOwnerID, Role: roleCustomer},
	))
}

func useRecordingDatabaseDriver(t *testing.T) *recordingDatabaseDriver {
	t.Helper()
	driver := &recordingDatabaseDriver{}
	previous := newDatabaseDriver
	newDatabaseDriver = func(services.DriverConfig) (services.DatabaseDriver, error) {
		driver.factoryCalls++
		return driver, nil
	}
	t.Cleanup(func() {
		newDatabaseDriver = previous
	})
	return driver
}

func countDatabaseSecurityRows(t *testing.T, db *sql.DB) [3]int {
	t.Helper()
	var counts [3]int
	for index, table := range []string{`databases_v2`, `database_users`, `database_user_grants`} {
		if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&counts[index]); err != nil {
			t.Fatal(err)
		}
	}
	return counts
}

func installDatabaseSecurityTrigger(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}

func seedOwnedDatabase(t *testing.T, db *sql.DB) int {
	t.Helper()
	const databaseID = 9601
	_, err := db.Exec(`
		INSERT INTO databases_v2
			(id, server_id, subscription_id, domain_id, name)
		VALUES (?, ?, ?, ?, ?);
	`,
		databaseID,
		dbSecurityServer,
		dbSecuritySubscription,
		dbSecurityDomain,
		`9201_compensation`,
	)
	if err != nil {
		t.Fatal(err)
	}
	return databaseID
}

func seedOwnedDatabaseGrant(t *testing.T, db *sql.DB) (int, int) {
	t.Helper()
	const grantID = 9701
	databaseID := seedOwnedDatabase(t, db)
	_, err := db.Exec(`
		INSERT INTO database_user_grants
			(id, database_id, user_id, privileges)
		VALUES (?, ?, ?, ?);
	`, grantID, databaseID, dbSecurityUser, `SELECT,INSERT`)
	if err != nil {
		t.Fatal(err)
	}
	return databaseID, grantID
}

func requireDatabaseDriverEvents(
	t *testing.T,
	driver *recordingDatabaseDriver,
	want ...string,
) {
	t.Helper()
	gotText := strings.Join(driver.events, `\n`)
	wantText := strings.Join(want, `\n`)
	if gotText != wantText {
		t.Fatalf("driver events:\n%s\nwant:\n%s", gotText, wantText)
	}
}

// The requested names below are plain identifiers on purpose. Since R-051 the
// handler resolves the engine-side name before it resolves any reference, so a
// name the engines cannot hold answers 400 on the name alone - which would stop
// these cases testing the thing they are about. The 404 oracle is unaffected:
// that 400 is a pure function of the caller's own string and says nothing about
// whether a user or a domain exists.
//
// Aşağıdaki adlar bilerek düz tanımlayıcıdır. R-051'den beri handler, motor
// tarafındaki adı referanslardan önce çözer; tutulamayacak bir ad yalnız ada
// bakılarak 400 döner ve bu durumda bu vakalar konularını sınamaz olurdu.
func TestCreateDatabaseV2RejectsForeignAndMissingReferencesBeforeSideEffects(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{
			name: `foreign user on same physical engine and different logical server`,
			body: fmt.Sprintf(
				`{"database_name":"blocked_user","user_id":%d}`,
				dbSecurityForeignUser,
			),
		},
		{
			name: `missing user`,
			body: `{"database_name":"blocked_user","user_id":999999}`,
		},
		{
			name: `foreign domain`,
			body: fmt.Sprintf(
				`{"database_name":"blocked_domain","user_id":%d,"domain_id":%d}`,
				dbSecurityUser,
				dbSecurityForeignDomain,
			),
		},
		{
			name: `missing domain`,
			body: fmt.Sprintf(
				`{"database_name":"blocked_domain","user_id":%d,"domain_id":999999}`,
				dbSecurityUser,
			),
		},
	}
	bodies := make([]string, len(cases))
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			fixture := newDatabaseV2SecurityFixture(t)
			driver := useRecordingDatabaseDriver(t)
			before := countDatabaseSecurityRows(t, fixture.sql)
			recorder := httptest.NewRecorder()

			fixture.panel.handleCreateDatabaseV2(
				recorder,
				fixture.request(testCase.body),
			)

			if recorder.Code != http.StatusNotFound {
				t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
			}
			bodies[index] = recorder.Body.String()
			if driver.factoryCalls != 0 ||
				driver.createDatabaseCalls != 0 ||
				driver.createUserCalls != 0 ||
				driver.grantCalls != 0 {
				t.Fatalf(`driver was reached on rejected reference: %+v`, driver)
			}
			if after := countDatabaseSecurityRows(t, fixture.sql); after != before {
				t.Fatalf(`repository rows changed: before=%v after=%v`, before, after)
			}
		})
	}
	for index := 1; index < len(bodies); index++ {
		if bodies[index] != bodies[0] {
			t.Fatalf(
				`foreign/missing reference oracle: first=%q case[%d]=%q`,
				bodies[0],
				index,
				bodies[index],
			)
		}
	}
}

func TestCreateDatabaseV2ExistingUserNeverReturnsStoredPassword(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	recorder := httptest.NewRecorder()
	requestBody := fmt.Sprintf(
		`{"database_name":"existing","user_id":%d,"domain_id":%d}`,
		dbSecurityUser,
		dbSecurityDomain,
	)

	fixture.panel.handleCreateDatabaseV2(recorder, fixture.request(requestBody))

	if recorder.Code != http.StatusOK {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if _, exists := response[`password`]; exists {
		t.Fatalf(`stored password field returned: %s`, recorder.Body.String())
	}
	if strings.Contains(recorder.Body.String(), `target-user-secret`) {
		t.Fatalf(`stored plaintext secret leaked: %s`, recorder.Body.String())
	}
	if driver.factoryCalls != 1 ||
		driver.createDatabaseCalls != 1 ||
		driver.createUserCalls != 0 ||
		driver.grantCalls != 1 {
		t.Fatalf(`unexpected driver calls: %+v`, driver)
	}
	var stored string
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE id = ?`,
		dbSecurityUser,
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if !secrets.IsEncrypted(stored) {
		t.Fatalf(`stored credential is not sealed: %q`, stored)
	}
}

func TestCreateDatabaseV2ReturnsOnlyNewSecretAndStoresItSealed(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	recorder := httptest.NewRecorder()
	requestBody := fmt.Sprintf(
		`{"database_name":"newdb","domain_id":%d,"new_username":"new_user","new_password":"new-clear-secret"}`,
		dbSecurityDomain,
	)

	fixture.panel.handleCreateDatabaseV2(recorder, fixture.request(requestBody))

	if recorder.Code != http.StatusOK {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response[`password`] != `new-clear-secret` {
		t.Fatalf(`new secret was not returned once: %v`, response[`password`])
	}
	if driver.createdUser != `s9201_new_user` ||
		driver.createdPassword != `new-clear-secret` {
		t.Fatalf(`unexpected engine credential: %+v`, driver)
	}
	var stored string
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE username = 's9201_new_user'`,
	).Scan(&stored); err != nil {
		t.Fatal(err)
	}
	if stored == `new-clear-secret` || !secrets.IsEncrypted(stored) {
		t.Fatalf(`new credential stored in plaintext: %q`, stored)
	}
	opened, err := fixture.box.Decrypt(stored)
	if err != nil {
		t.Fatal(err)
	}
	if opened != `new-clear-secret` {
		t.Fatalf(`opened secret = %q`, opened)
	}
}

func TestCreateDatabaseV2MetadataFailureRollsBackAndCompensatesEngine(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	installDatabaseSecurityTrigger(t, fixture.sql, `
		CREATE TRIGGER fail_database_grant_insert
		BEFORE INSERT ON database_user_grants
		BEGIN
			SELECT RAISE(ABORT, 'injected grant metadata failure');
		END;
	`)
	before := countDatabaseSecurityRows(t, fixture.sql)
	recorder := httptest.NewRecorder()
	body := fmt.Sprintf(
		`{"database_name":"tx_rollback","domain_id":%d,"new_username":"new_compensated","new_password":"new-secret"}`,
		dbSecurityDomain,
	)

	fixture.panel.handleCreateDatabaseV2(recorder, fixture.request(body))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	if after := countDatabaseSecurityRows(t, fixture.sql); after != before {
		t.Fatalf(`metadata transaction leaked rows: before=%v after=%v`, before, after)
	}
	requireDatabaseDriverEvents(t, driver,
		`create-database:s9201_tx_rollback`,
		`create-user:s9201_new_compensated`,
		`grant:s9201_tx_rollback:s9201_new_compensated:ALL`,
		`revoke:s9201_tx_rollback:s9201_new_compensated`,
		`delete-user:s9201_new_compensated`,
		`delete-database:s9201_tx_rollback`,
	)
}

func TestCreateDatabaseV2PhysicalGrantFailureCompensatesCompletedWork(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	driver.grantErr = errors.New(`injected physical grant failure`)
	before := countDatabaseSecurityRows(t, fixture.sql)
	recorder := httptest.NewRecorder()
	body := fmt.Sprintf(
		`{"database_name":"grant_failure","domain_id":%d,"new_username":"grant_user","new_password":"new-secret"}`,
		dbSecurityDomain,
	)

	fixture.panel.handleCreateDatabaseV2(recorder, fixture.request(body))

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	if after := countDatabaseSecurityRows(t, fixture.sql); after != before {
		t.Fatalf(`physical failure published metadata: before=%v after=%v`, before, after)
	}
	requireDatabaseDriverEvents(t, driver,
		`create-database:s9201_grant_failure`,
		`create-user:s9201_grant_user`,
		`grant:s9201_grant_failure:s9201_grant_user:ALL`,
		`revoke:s9201_grant_failure:s9201_grant_user`,
		`delete-user:s9201_grant_user`,
		`delete-database:s9201_grant_failure`,
	)
}

func TestCreateDatabaseV2UserMetadataFailureDeletesPhysicalUser(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	installDatabaseSecurityTrigger(t, fixture.sql, `
		CREATE TRIGGER fail_database_user_insert
		BEFORE INSERT ON database_users
		BEGIN
			SELECT RAISE(ABORT, 'injected user metadata failure');
		END;
	`)
	before := countDatabaseSecurityRows(t, fixture.sql)
	recorder := httptest.NewRecorder()
	request := fixture.requestAsOwner(
		http.MethodPost,
		fmt.Sprintf(`/api/v1/database-servers/%d/users`, dbSecurityServer),
		`{"username":"standalone","password":"new-secret"}`,
	)

	fixture.panel.handleCreateDatabaseV2User(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	if after := countDatabaseSecurityRows(t, fixture.sql); after != before {
		t.Fatalf(`failed user metadata changed rows: before=%v after=%v`, before, after)
	}
	requireDatabaseDriverEvents(t, driver,
		`create-user:s9201_standalone`,
		`delete-user:s9201_standalone`,
	)
}

func TestGrantDatabaseAccessMetadataFailureRevokesPhysicalGrant(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	databaseID := seedOwnedDatabase(t, fixture.sql)
	installDatabaseSecurityTrigger(t, fixture.sql, `
		CREATE TRIGGER fail_database_grant_insert
		BEFORE INSERT ON database_user_grants
		BEGIN
			SELECT RAISE(ABORT, 'injected grant metadata failure');
		END;
	`)
	recorder := httptest.NewRecorder()
	request := fixture.requestAsOwner(
		http.MethodPost,
		fmt.Sprintf(`/api/v1/databases/%d/grants`, databaseID),
		fmt.Sprintf(`{"user_id":%d,"privileges":"SELECT"}`, dbSecurityUser),
	)

	fixture.panel.handleGrantDatabaseAccess(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	var count int
	if err := fixture.sql.QueryRow(`SELECT count(*) FROM database_user_grants`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf(`grant metadata count = %d, want 0`, count)
	}
	requireDatabaseDriverEvents(t, driver,
		`grant:9201_compensation:9201_owner:SELECT`,
		`revoke:9201_compensation:9201_owner`,
	)
}

func TestRevokeDatabaseAccessPhysicalFailurePreservesMetadata(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	_, grantID := seedOwnedDatabaseGrant(t, fixture.sql)
	driver.revokeErr = errors.New(`injected physical revoke failure`)
	recorder := httptest.NewRecorder()
	request := fixture.requestAsOwner(
		http.MethodDelete,
		fmt.Sprintf(`/api/v1/database-grants/%d`, grantID),
		``,
	)

	fixture.panel.handleRevokeDatabaseAccess(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	var count int
	if err := fixture.sql.QueryRow(
		`SELECT count(*) FROM database_user_grants WHERE id = ?`,
		grantID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf(`grant metadata count = %d, want 1`, count)
	}
	requireDatabaseDriverEvents(t, driver,
		`revoke:9201_compensation:9201_owner`,
	)
}

func TestRevokeDatabaseAccessMetadataFailureRestoresPhysicalGrant(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	driver := useRecordingDatabaseDriver(t)
	_, grantID := seedOwnedDatabaseGrant(t, fixture.sql)
	installDatabaseSecurityTrigger(t, fixture.sql, `
		CREATE TRIGGER fail_database_grant_delete
		BEFORE DELETE ON database_user_grants
		BEGIN
			SELECT RAISE(ABORT, 'injected grant metadata delete failure');
		END;
	`)
	recorder := httptest.NewRecorder()
	request := fixture.requestAsOwner(
		http.MethodDelete,
		fmt.Sprintf(`/api/v1/database-grants/%d`, grantID),
		``,
	)

	fixture.panel.handleRevokeDatabaseAccess(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf(`status = %d, body = %s`, recorder.Code, recorder.Body.String())
	}
	var count int
	if err := fixture.sql.QueryRow(
		`SELECT count(*) FROM database_user_grants WHERE id = ?`,
		grantID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf(`grant metadata count = %d, want 1`, count)
	}
	requireDatabaseDriverEvents(t, driver,
		`revoke:9201_compensation:9201_owner`,
		`grant:9201_compensation:9201_owner:SELECT,INSERT`,
	)
}

func TestDatabaseMutationErrorPreservesCauseAndCompensation(t *testing.T) {
	cause := errors.New(`metadata failure`)
	compensation := errors.New(`compensation failure`)
	joined := databaseMutationError(cause, compensation)
	if !errors.Is(joined, cause) || !errors.Is(joined, compensation) {
		t.Fatalf(`joined error does not preserve both causes: %v`, joined)
	}
}

func TestGetDatabaseIDFromPathRejectsTruncatedPath(t *testing.T) {
	if _, err := getDatabaseIDFromPath("/api/v2/databases"); err == nil {
		t.Fatal("truncated database path was accepted")
	}
}

func TestListDatabaseGrantsRejectsCrossSubscriptionMetadata(t *testing.T) {
	f := newDatabaseV2SecurityFixture(t)
	databaseID := seedOwnedDatabase(t, f.sql)
	foreignSecret, err := f.box.Encrypt("cross-subscription-secret")
	if err != nil {
		t.Fatal(err)
	}
	const crossSubscriptionUserID = 9503
	if _, err := f.sql.Exec(`
		INSERT INTO database_users
			(id, server_id, subscription_id, username, password)
		VALUES (?, ?, ?, 'cross_subscription_user', ?)
	`,
		crossSubscriptionUserID,
		dbSecurityServer,
		dbSecurityForeignSub,
		foreignSecret,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := f.sql.Exec(`
		INSERT INTO database_user_grants
			(database_id, user_id, privileges)
		VALUES (?, ?, 'ALL')
	`, databaseID, crossSubscriptionUserID); err != nil {
		t.Fatal(err)
	}

	req := f.requestAsOwner(
		http.MethodGet,
		fmt.Sprintf("/api/v2/databases/%d/grants", databaseID),
		"",
	)
	rec := httptest.NewRecorder()
	f.panel.handleListDatabaseGrants(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s, want fail-closed 500", rec.Code, rec.Body.String())
	}
}

func TestEncryptLegacyDBPasswordsMigratesUsersAtomicallyAndIdempotently(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	if _, err := fixture.sql.Exec(
		`UPDATE database_servers SET root_password_encrypted = 'legacy-root' WHERE id = ?`,
		dbSecurityServer,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.sql.Exec(
		`UPDATE database_users SET password = 'legacy-user' WHERE id = ?`,
		dbSecurityUser,
	); err != nil {
		t.Fatal(err)
	}
	var untouchedServer string
	var untouchedUser string
	if err := fixture.sql.QueryRow(
		`SELECT root_password_encrypted FROM database_servers WHERE id = ?`,
		dbSecurityForeignServer,
	).Scan(&untouchedServer); err != nil {
		t.Fatal(err)
	}
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE id = ?`,
		dbSecurityForeignUser,
	).Scan(&untouchedUser); err != nil {
		t.Fatal(err)
	}

	if err := fixture.panel.encryptLegacyDBPasswords(context.Background()); err != nil {
		t.Fatal(err)
	}

	var migratedServer string
	var migratedUser string
	if err := fixture.sql.QueryRow(
		`SELECT root_password_encrypted FROM database_servers WHERE id = ?`,
		dbSecurityServer,
	).Scan(&migratedServer); err != nil {
		t.Fatal(err)
	}
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE id = ?`,
		dbSecurityUser,
	).Scan(&migratedUser); err != nil {
		t.Fatal(err)
	}
	for stored, want := range map[string]string{
		migratedServer: `legacy-root`,
		migratedUser:   `legacy-user`,
	} {
		if !secrets.IsEncrypted(stored) {
			t.Fatalf(`legacy credential was not sealed: %q`, stored)
		}
		opened, err := fixture.box.Decrypt(stored)
		if err != nil {
			t.Fatal(err)
		}
		if opened != want {
			t.Fatalf(`opened credential = %q, want %q`, opened, want)
		}
	}

	if err := fixture.panel.encryptLegacyDBPasswords(context.Background()); err != nil {
		t.Fatal(err)
	}
	var afterSecondServer string
	var afterSecondUser string
	if err := fixture.sql.QueryRow(
		`SELECT root_password_encrypted FROM database_servers WHERE id = ?`,
		dbSecurityServer,
	).Scan(&afterSecondServer); err != nil {
		t.Fatal(err)
	}
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE id = ?`,
		dbSecurityUser,
	).Scan(&afterSecondUser); err != nil {
		t.Fatal(err)
	}
	if afterSecondServer != migratedServer || afterSecondUser != migratedUser {
		t.Fatal(`idempotent migration rewrote an already sealed credential`)
	}

	var afterUntouchedServer string
	var afterUntouchedUser string
	if err := fixture.sql.QueryRow(
		`SELECT root_password_encrypted FROM database_servers WHERE id = ?`,
		dbSecurityForeignServer,
	).Scan(&afterUntouchedServer); err != nil {
		t.Fatal(err)
	}
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE id = ?`,
		dbSecurityForeignUser,
	).Scan(&afterUntouchedUser); err != nil {
		t.Fatal(err)
	}
	if afterUntouchedServer != untouchedServer || afterUntouchedUser != untouchedUser {
		t.Fatal(`migration rewrote an already sealed credential`)
	}
}

func TestEncryptLegacyDBPasswordsRejectsCorruptCiphertextWithoutPartialMigration(t *testing.T) {
	fixture := newDatabaseV2SecurityFixture(t)
	const corrupt = `enc:v1:not-valid-ciphertext`
	const legacyUser = `legacy-user-must-remain-plaintext`
	if _, err := fixture.sql.Exec(
		`UPDATE database_servers SET root_password_encrypted = ? WHERE id = ?`,
		corrupt, dbSecurityServer,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.sql.Exec(
		`UPDATE database_users SET password = ? WHERE id = ?`,
		legacyUser, dbSecurityUser,
	); err != nil {
		t.Fatal(err)
	}

	err := fixture.panel.encryptLegacyDBPasswords(context.Background())
	if err == nil {
		t.Fatal(`corrupt encrypted credential was accepted`)
	}
	if strings.Contains(err.Error(), corrupt) || strings.Contains(err.Error(), legacyUser) {
		t.Fatalf(`migration error leaked a credential: %v`, err)
	}

	var storedServer string
	var storedUser string
	if err := fixture.sql.QueryRow(
		`SELECT root_password_encrypted FROM database_servers WHERE id = ?`,
		dbSecurityServer,
	).Scan(&storedServer); err != nil {
		t.Fatal(err)
	}
	if err := fixture.sql.QueryRow(
		`SELECT password FROM database_users WHERE id = ?`, dbSecurityUser,
	).Scan(&storedUser); err != nil {
		t.Fatal(err)
	}
	if storedServer != corrupt || storedUser != legacyUser {
		t.Fatalf(
			`failed validation changed stored credentials: server=%q user=%q`,
			storedServer, storedUser,
		)
	}
}
