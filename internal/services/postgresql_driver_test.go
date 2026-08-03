package services

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"
	"testing"
)

type postgreSQLScript struct {
	mu sync.Mutex

	queries   []string
	execs     []string
	begins    int
	commits   int
	rollbacks int

	queryErr      error
	terminationOK bool
	failExecMatch string
	execErr       error
	beginErr      error
	commitErr     error
}

func (s *postgreSQLScript) snapshot() (queries, execs []string, begins, commits, rollbacks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.queries...), append([]string(nil), s.execs...),
		s.begins, s.commits, s.rollbacks
}

type postgreSQLScriptConnector struct {
	state *postgreSQLScript
}

func (c *postgreSQLScriptConnector) Connect(context.Context) (driver.Conn, error) {
	return &postgreSQLScriptConn{state: c.state}, nil
}

func (c *postgreSQLScriptConnector) Driver() driver.Driver {
	return &postgreSQLScriptDriver{state: c.state}
}

type postgreSQLScriptDriver struct {
	state *postgreSQLScript
}

func (d *postgreSQLScriptDriver) Open(string) (driver.Conn, error) {
	return &postgreSQLScriptConn{state: d.state}, nil
}

type postgreSQLScriptConn struct {
	state *postgreSQLScript
}

func (c *postgreSQLScriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("script driver does not prepare statements")
}

func (c *postgreSQLScriptConn) Close() error { return nil }

func (c *postgreSQLScriptConn) Begin() (driver.Tx, error) {
	return c.BeginTx(context.Background(), driver.TxOptions{})
}

func (c *postgreSQLScriptConn) BeginTx(
	_ context.Context,
	_ driver.TxOptions,
) (driver.Tx, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	if c.state.beginErr != nil {
		return nil, c.state.beginErr
	}
	c.state.begins++
	return &postgreSQLScriptTx{state: c.state}, nil
}

func (c *postgreSQLScriptConn) ExecContext(
	_ context.Context,
	query string,
	_ []driver.NamedValue,
) (driver.Result, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.execs = append(c.state.execs, query)
	if c.state.failExecMatch != "" && strings.Contains(query, c.state.failExecMatch) {
		return nil, c.state.execErr
	}
	return driver.RowsAffected(1), nil
}

func (c *postgreSQLScriptConn) QueryContext(
	_ context.Context,
	query string,
	_ []driver.NamedValue,
) (driver.Rows, error) {
	c.state.mu.Lock()
	defer c.state.mu.Unlock()
	c.state.queries = append(c.state.queries, query)
	if c.state.queryErr != nil {
		return nil, c.state.queryErr
	}
	return &postgreSQLBoolRows{value: c.state.terminationOK}, nil
}

type postgreSQLScriptTx struct {
	state *postgreSQLScript
}

func (tx *postgreSQLScriptTx) Commit() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	if tx.state.commitErr != nil {
		return tx.state.commitErr
	}
	tx.state.commits++
	return nil
}

func (tx *postgreSQLScriptTx) Rollback() error {
	tx.state.mu.Lock()
	defer tx.state.mu.Unlock()
	tx.state.rollbacks++
	return nil
}

type postgreSQLBoolRows struct {
	value bool
	done  bool
}

func (r *postgreSQLBoolRows) Columns() []string { return []string{"terminated"} }
func (r *postgreSQLBoolRows) Close() error      { return nil }
func (r *postgreSQLBoolRows) Next(values []driver.Value) error {
	if r.done {
		return io.EOF
	}
	r.done = true
	values[0] = r.value
	return nil
}

func newScriptedPostgreSQLDriver(state *postgreSQLScript) *PostgreSQLDriver {
	driver := NewPostgreSQLDriver(DriverConfig{
		Host:         "127.0.0.1",
		Port:         5432,
		RootPassword: "test-secret",
	})
	driver.openDB = func(string) (*sql.DB, error) {
		return sql.OpenDB(&postgreSQLScriptConnector{state: state}), nil
	}
	return driver
}

func TestPostgreSQLDSNEscapesCredentialsHostAndDatabase(t *testing.T) {
	dsn := postgreSQLDSN("2001:db8::42", 5544, "tenant/database", "p@ss:/?#[]")
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, ok := parsed.User.Password()
	if !ok || password != "p@ss:/?#[]" {
		t.Fatalf("password round trip failed: ok=%v password=%q dsn=%s", ok, password, dsn)
	}
	if parsed.Hostname() != "2001:db8::42" || parsed.Port() != "5544" {
		t.Fatalf("host=%q port=%q", parsed.Hostname(), parsed.Port())
	}
	if parsed.Path != "/tenant/database" || parsed.Query().Get("sslmode") != "disable" {
		t.Fatalf("path=%q query=%q", parsed.Path, parsed.RawQuery)
	}
}

func TestPostgreSQLDeleteDatabaseRefusesTerminationQueryFailure(t *testing.T) {
	state := &postgreSQLScript{
		queryErr: errors.New("cannot inspect pg_stat_activity"),
	}
	driver := newScriptedPostgreSQLDriver(state)

	err := driver.DeleteDatabase("tenant_db")
	if err == nil || !strings.Contains(err.Error(), "terminate database connections") {
		t.Fatalf("expected termination error, got %v", err)
	}
	_, execs, _, _, _ := state.snapshot()
	if len(execs) != 0 {
		t.Fatalf("DROP ran after termination query failure: %v", execs)
	}
}

func TestPostgreSQLDeleteDatabaseRefusesIncompleteTermination(t *testing.T) {
	state := &postgreSQLScript{terminationOK: false}
	driver := newScriptedPostgreSQLDriver(state)

	err := driver.DeleteDatabase("tenant_db")
	if err == nil || !strings.Contains(err.Error(), "every connection") {
		t.Fatalf("expected incomplete termination error, got %v", err)
	}
	_, execs, _, _, _ := state.snapshot()
	if len(execs) != 0 {
		t.Fatalf("DROP ran after incomplete termination: %v", execs)
	}
}

func TestPostgreSQLDeleteDatabasePropagatesDropFailure(t *testing.T) {
	state := &postgreSQLScript{
		terminationOK: true,
		failExecMatch: "DROP DATABASE",
		execErr:       errors.New("drop rejected"),
	}
	driver := newScriptedPostgreSQLDriver(state)

	err := driver.DeleteDatabase("tenant_db")
	if err == nil || !strings.Contains(err.Error(), "drop rejected") {
		t.Fatalf("expected DROP error, got %v", err)
	}
}

func TestPostgreSQLGrantPrivilegesRollsBackPartialMutation(t *testing.T) {
	state := &postgreSQLScript{
		failExecMatch: "ALTER DEFAULT PRIVILEGES",
		execErr:       errors.New("default grant rejected"),
	}
	driver := newScriptedPostgreSQLDriver(state)

	err := driver.GrantPrivileges("tenant_db", "tenant_user", "ALL")
	if err == nil || !strings.Contains(err.Error(), "default grant rejected") {
		t.Fatalf("expected grant error, got %v", err)
	}
	_, _, begins, commits, rollbacks := state.snapshot()
	if begins != 1 || commits != 0 || rollbacks != 1 {
		t.Fatalf("transaction begin=%d commit=%d rollback=%d", begins, commits, rollbacks)
	}
}

func TestPostgreSQLRevokePrivilegesRollsBackPartialMutation(t *testing.T) {
	state := &postgreSQLScript{
		failExecMatch: "ALL TABLES",
		execErr:       errors.New("table revoke rejected"),
	}
	driver := newScriptedPostgreSQLDriver(state)

	err := driver.RevokePrivileges("tenant_db", "tenant_user")
	if err == nil || !strings.Contains(err.Error(), "table revoke rejected") {
		t.Fatalf("expected revoke error, got %v", err)
	}
	_, execs, begins, commits, rollbacks := state.snapshot()
	if begins != 1 || commits != 0 || rollbacks != 1 {
		t.Fatalf("transaction begin=%d commit=%d rollback=%d", begins, commits, rollbacks)
	}
	if len(execs) != 3 {
		t.Fatalf("revoke continued after failure: %v", execs)
	}
}

func TestPostgreSQLRevokePrivilegesCommitsCompleteMutation(t *testing.T) {
	state := &postgreSQLScript{}
	driver := newScriptedPostgreSQLDriver(state)

	if err := driver.RevokePrivileges("tenant_db", "tenant_user"); err != nil {
		t.Fatal(err)
	}
	_, execs, begins, commits, rollbacks := state.snapshot()
	if begins != 1 || commits != 1 || rollbacks != 0 {
		t.Fatalf("transaction begin=%d commit=%d rollback=%d", begins, commits, rollbacks)
	}
	if len(execs) != 5 {
		t.Fatalf("executed %d revoke statements, want 5: %v", len(execs), execs)
	}
}

func TestPostgreSQLRevokePrivilegesPropagatesOpenFailure(t *testing.T) {
	driver := NewPostgreSQLDriver(DriverConfig{})
	driver.openDB = func(string) (*sql.DB, error) {
		return nil, errors.New("target database unavailable")
	}

	err := driver.RevokePrivileges("tenant_db", "tenant_user")
	if err == nil || !strings.Contains(err.Error(), "target database unavailable") {
		t.Fatalf("expected target database error, got %v", err)
	}
}

func TestPostgreSQLRevokePrivilegesPropagatesCommitFailure(t *testing.T) {
	state := &postgreSQLScript{commitErr: errors.New("commit rejected")}
	driver := newScriptedPostgreSQLDriver(state)

	err := driver.RevokePrivileges("tenant_db", "tenant_user")
	if err == nil || !strings.Contains(err.Error(), "commit rejected") {
		t.Fatalf("expected commit error, got %v", err)
	}
	_, _, begins, commits, rollbacks := state.snapshot()
	if begins != 1 || commits != 0 || rollbacks != 0 {
		t.Fatalf("transaction begin=%d commit=%d rollback=%d", begins, commits, rollbacks)
	}
}

func Example_postgreSQLDSN() {
	fmt.Println(postgreSQLDSN("127.0.0.1", 5432, "postgres", "secret"))
	// Output: postgres://postgres:secret@127.0.0.1:5432/postgres?sslmode=disable
}
