package services

import (
	"strings"
	"testing"
)

// R-051 guard. The panel used to build the engine-side name of a database and
// of a database user as fmt.Sprintf("%d_%s", subscriptionID, requested), which
// always begins with a digit, while ValidateSQLIdentifier refuses exactly that.
// Live on a real VM the create answered HTTP 500 with
//
//	invalid database name: identifier "2_r003probe" must start with a letter or underscore
//
// This test locks the property the fix rests on: whatever a create request
// carries, the name the panel produces from it is one the drivers accept - for
// databases and for users, and on both engines.
//
// R-051 muhafızı: adın motor tarafındaki biçimi bir rakamla başlıyordu ve
// doğrulayıcı bunu reddediyordu. Bu test, üretilen adın her iki nesne türü ve
// her iki motor için de sürücülerce kabul edildiğini kilitler.

// databaseNameRequests are names a create request can realistically carry,
// including the exact one that failed on the drill's restored host and the
// ones whose first character used to be the whole defect.
var databaseNameRequests = []struct {
	subscriptionID int
	requested      string
}{
	{2, "r003probe"},    // the live failure, verbatim
	{1, "myapp_db"},     // the placeholder the add-database dialogue shows
	{7, "shop"},         //
	{42, "wp"},          //
	{1234, "Analytics"}, // mixed case survives untouched
	{3, "9lives"},       // the operator's own name may start with a digit
	{5, "_scratch"},     // ... or with an underscore
	{999999, "a"},       // a large subscription id and the shortest name
}

func TestSubscriptionScopedNamesAreAcceptedByBothDrivers(t *testing.T) {
	for _, request := range databaseNameRequests {
		dbName, err := SubscriptionDatabaseName(request.subscriptionID, request.requested)
		if err != nil {
			t.Fatalf(
				"SubscriptionDatabaseName(%d, %q) refused a legitimate request: %v",
				request.subscriptionID, request.requested, err,
			)
		}
		userName, err := SubscriptionUserName(request.subscriptionID, request.requested)
		if err != nil {
			t.Fatalf(
				"SubscriptionUserName(%d, %q) refused a legitimate request: %v",
				request.subscriptionID, request.requested, err,
			)
		}

		for _, generated := range []struct {
			kind string
			name string
		}{{"database", dbName}, {"user", userName}} {
			// The defect itself: the first character.
			first := rune(generated.name[0])
			if first >= '0' && first <= '9' {
				t.Errorf(
					"the %s name %q the panel generates begins with a digit; no engine will take it",
					generated.kind, generated.name,
				)
			}

			// The one validator every driver calls.
			if err := ValidateSQLIdentifier(generated.name); err != nil {
				t.Errorf("ValidateSQLIdentifier(%q) [%s]: %v", generated.name, generated.kind, err)
			}

			// PostgreSQLDriver.CreateDatabase and CreateUser both quote through
			// QuotePGIdentifier before they touch the engine.
			if _, err := QuotePGIdentifier(generated.name); err != nil {
				t.Errorf(
					"the PostgreSQL driver refuses the %s name %q: %v",
					generated.kind, generated.name, err,
				)
			}
		}

		// MariaDBDriver.CreateDatabase quotes the database as an identifier.
		if _, err := QuoteMySQLIdentifier(dbName); err != nil {
			t.Errorf("the MariaDB driver refuses the database name %q: %v", dbName, err)
		}

		// MariaDBDriver.CreateUser validates the account name as an identifier
		// and then quotes it as a string literal; both have to pass.
		if err := ValidateSQLIdentifier(userName); err != nil {
			t.Errorf("the MariaDB driver refuses the user name %q: %v", userName, err)
		}
		if _, err := QuoteMySQLStringLiteral(userName); err != nil {
			t.Errorf("the MariaDB driver cannot quote the user name %q: %v", userName, err)
		}

		// The limits the two engines impose on their own, which the validator
		// does not know about: a MySQL account name stops at 32 characters.
		if len(dbName) > 63 {
			t.Errorf("the database name %q is %d characters; PostgreSQL stops at 63", dbName, len(dbName))
		}
		if len(userName) > 32 {
			t.Errorf("the user name %q is %d characters; MySQL stops at 32", userName, len(userName))
		}
	}
}

// TestSubscriptionScopedNamesAreScopedByTheSubscription keeps the prefix doing
// the job it was there for: two subscriptions asking for the same word must
// still get two different objects.
func TestSubscriptionScopedNamesAreScopedByTheSubscription(t *testing.T) {
	first, err := SubscriptionDatabaseName(2, "shop")
	if err != nil {
		t.Fatal(err)
	}
	second, err := SubscriptionDatabaseName(3, "shop")
	if err != nil {
		t.Fatal(err)
	}
	if first == second {
		t.Fatalf("subscriptions 2 and 3 both got %q; the name is not scoped", first)
	}
	if !strings.Contains(first, "2") || !strings.Contains(second, "3") {
		t.Fatalf("the subscription is not visible in %q / %q", first, second)
	}
	// Databases and users of one subscription share one prefix, so an operator
	// reading `SHOW DATABASES` sees one tenant's objects together.
	user, err := SubscriptionUserName(2, "shop")
	if err != nil {
		t.Fatal(err)
	}
	dbPrefix := first[:strings.Index(first, "_")+1]
	if !strings.HasPrefix(user, dbPrefix) {
		t.Fatalf("the user name %q does not share the database prefix %q", user, dbPrefix)
	}
}

// TestSubscriptionScopedNamesRefuseWhatNoEngineCanHold checks the other half:
// what the panel cannot make safe it must refuse in words the operator can act
// on, not hand to an engine and turn into a 500.
func TestSubscriptionScopedNamesRefuseWhatNoEngineCanHold(t *testing.T) {
	cases := []struct {
		what           string
		subscriptionID int
		requested      string
		wantSubstring  string
	}{
		{"a hyphen", 2, "my-db", "only letters, digits and underscores"},
		{"a space", 2, "my db", "only letters, digits and underscores"},
		{"a quote", 2, `my"db`, "only letters, digits and underscores"},
		{"an empty name", 2, "   ", "must not be empty"},
		{"an unknown subscription", 0, "shop", "subscription is unknown"},
		{"a database name past the limit", 2, strings.Repeat("a", 61), "too long"},
	}
	for _, c := range cases {
		if _, err := SubscriptionDatabaseName(c.subscriptionID, c.requested); err == nil {
			t.Errorf("SubscriptionDatabaseName accepted %s", c.what)
		} else if !strings.Contains(err.Error(), c.wantSubstring) {
			t.Errorf("SubscriptionDatabaseName(%s) said %q, want it to mention %q",
				c.what, err.Error(), c.wantSubstring)
		}
	}

	// An account name that PostgreSQL would hold but MySQL would not is refused
	// for both, because a subscription can move between engines.
	long := strings.Repeat("u", 40)
	if _, err := SubscriptionDatabaseName(2, long); err != nil {
		t.Errorf("a 40-character database name should fit: %v", err)
	}
	if _, err := SubscriptionUserName(2, long); err == nil {
		t.Error("SubscriptionUserName accepted a name MySQL cannot hold")
	}
}

// TestPostgreSQLDriverCreatesTheScopedNames drives the production driver
// functions, not just the validators, and reads back the SQL that reached the
// engine.
func TestPostgreSQLDriverCreatesTheScopedNames(t *testing.T) {
	dbName, err := SubscriptionDatabaseName(2, "r003probe")
	if err != nil {
		t.Fatal(err)
	}
	userName, err := SubscriptionUserName(2, "r003probe")
	if err != nil {
		t.Fatal(err)
	}

	state := &postgreSQLScript{}
	driver := newScriptedPostgreSQLDriver(state)
	if err := driver.CreateDatabase(dbName); err != nil {
		t.Fatalf("PostgreSQLDriver.CreateDatabase(%q): %v", dbName, err)
	}
	if err := driver.CreateUser(userName, "a-password"); err != nil {
		t.Fatalf("PostgreSQLDriver.CreateUser(%q): %v", userName, err)
	}
	_, execs, _, _, _ := state.snapshot()
	joined := strings.Join(execs, "\n")
	if !strings.Contains(joined, `CREATE DATABASE "`+dbName+`"`) {
		t.Errorf("the engine never saw the database create; statements were:\n%s", joined)
	}
	if !strings.Contains(joined, `CREATE USER "`+userName+`"`) {
		t.Errorf("the engine never saw the user create; statements were:\n%s", joined)
	}
}

// TestMariaDBDriverAcceptsTheScopedNames drives the production MariaDB driver
// against an endpoint that cannot answer. The create must fail on the wire and
// never on the name: a name refusal is R-051 returning.
func TestMariaDBDriverAcceptsTheScopedNames(t *testing.T) {
	dbName, err := SubscriptionDatabaseName(2, "r003probe")
	if err != nil {
		t.Fatal(err)
	}
	userName, err := SubscriptionUserName(2, "r003probe")
	if err != nil {
		t.Fatal(err)
	}

	// Port 1 on the loopback refuses immediately, so nothing is ever created
	// anywhere, on any machine this test runs on.
	driver := &MariaDBDriver{host: "127.0.0.1", port: 1, rootPassword: "unused"}

	if err := driver.CreateDatabase(dbName); err == nil {
		t.Fatalf("CreateDatabase(%q) reached an engine that should not exist", dbName)
	} else if strings.Contains(err.Error(), "invalid database name") {
		t.Errorf("the MariaDB driver refused the generated database name: %v", err)
	}

	if err := driver.CreateUser(userName, "a-password"); err == nil {
		t.Fatalf("CreateUser(%q) reached an engine that should not exist", userName)
	} else if strings.Contains(err.Error(), "invalid username") {
		t.Errorf("the MariaDB driver refused the generated user name: %v", err)
	}
}
