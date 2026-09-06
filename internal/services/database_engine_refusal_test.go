package services

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

// R-053. The classifier decides which of two very different instructions an
// operator is given, so the words each engine actually uses are the test.
func TestClassifyDatabaseEngineRefusal(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want DatabaseEngineRefusal
	}{
		{"nil is not a refusal", nil, DatabaseEngineRefusalNone},
		{
			"MariaDB refuses the empty root password",
			errors.New(`MariaDB connection failed: ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: NO)`),
			DatabaseEngineRefusalCredential,
		},
		{
			"MariaDB refuses a wrong root password",
			errors.New(`ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: YES)`),
			DatabaseEngineRefusalCredential,
		},
		{
			"PostgreSQL refuses the postgres role",
			errors.New(`PostgreSQL connection failed: pq: password authentication failed for user "postgres"`),
			DatabaseEngineRefusalCredential,
		},
		{
			"PostgreSQL was given no password at all",
			errors.New(`pq: no password supplied`),
			DatabaseEngineRefusalCredential,
		},
		{
			"MariaDB is not running",
			errors.New(`ERROR 2002 (HY000): Can't connect to local server through socket '/run/mysqld/mysqld.sock' (2)`),
			DatabaseEngineRefusalUnreachable,
		},
		{
			"PostgreSQL is not listening",
			errors.New(`failed to open connection: dial tcp 127.0.0.1:5432: connect: connection refused`),
			DatabaseEngineRefusalUnreachable,
		},
		{
			"an unrecognised failure stays unnamed",
			errors.New("invalid database name: identifier contains a quote"),
			DatabaseEngineRefusalNone,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := ClassifyDatabaseEngineRefusal(tc.err); got != tc.want {
				t.Fatalf("classification = %d, want %d", got, tc.want)
			}
		})
	}
}

// A refusal the engine explained is answered by naming what the host needs.
// The panel's own engine identifier picks the wording, so a wrapped error
// still selects the right one.
func TestClassifyDatabaseEngineRefusalThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf(
		"create physical database user: %w",
		errors.New(`ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: NO)`),
	)
	if got := ClassifyDatabaseEngineRefusal(wrapped); got != DatabaseEngineRefusalCredential {
		t.Fatalf("wrapped credential refusal = %d, want %d", got, DatabaseEngineRefusalCredential)
	}
}

func TestDatabaseEngineRefusalMessage(t *testing.T) {
	tests := []struct {
		name       string
		driverType string
		refusal    DatabaseEngineRefusal
		wantSubstr string
	}{
		{"MariaDB says the operator's root is left alone", "mariadb", DatabaseEngineRefusalCredential, "root password alone"},
		{"PostgreSQL names its postgres role", "postgresql", DatabaseEngineRefusalCredential, "postgres role"},
		{"an unknown engine still gets an instruction", "mongodb", DatabaseEngineRefusalCredential, "CelikPanel's own account"},
		{"an unreachable engine is told to start", "mariadb", DatabaseEngineRefusalUnreachable, "Start the engine"},
		{"an unnamed refusal has no sentence", "mariadb", DatabaseEngineRefusalNone, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DatabaseEngineRefusalMessage(tc.driverType, tc.refusal)
			if tc.wantSubstr == "" {
				if got != "" {
					t.Fatalf("message = %q, want empty", got)
				}
				return
			}
			if !strings.Contains(got, tc.wantSubstr) {
				t.Fatalf("message %q does not contain %q", got, tc.wantSubstr)
			}
		})
	}
}

// R-057. These sentences used to end with "register the server in CelikPanel
// again with that password", and this test used to require exactly that
// phrase. It was an instruction with nowhere to be carried out: the server
// list is filled by autodiscovery and there is no register-a-server screen.
// What the sentences must do now is name an action the operator can actually
// take, in CelikPanel, and none of them may send the operator off to set a
// root password again.
//
// R-057. Bu cumleler eskiden "sunucuyu CelikPanel'de o parolayla yeniden
// kaydedin" diye bitiyordu ve bu test tam olarak o ifadeyi zorunlu tutuyordu.
// Yerine getirilecek yeri olmayan bir talimatti. Cumleler artik operatorun
// gercekten yapabilecegi bir eylemi adlandirmalidir.
func TestDatabaseCredentialMessagesNameAnActionInsideTheProduct(t *testing.T) {
	for _, driverType := range []string{"mariadb", "postgresql", "mongodb"} {
		message := DatabaseEngineRefusalMessage(driverType, DatabaseEngineRefusalCredential)
		if !strings.Contains(message, "CelikPanel's own account") {
			t.Fatalf("%s message does not offer the panel's own account: %q", driverType, message)
		}
		if !strings.Contains(message, "username and password") {
			t.Fatalf("%s message does not offer the other way in: %q", driverType, message)
		}
		// The dead end this entry exists to remove.
		// Bu kaydin ortadan kaldirmak icin var oldugu cikmaz.
		for _, gone := range []string{
			"register the server in CelikPanel again",
			"Give this server a root password",
			"Give the postgres role a password",
			"Give the server a root password",
		} {
			if strings.Contains(message, gone) {
				t.Fatalf("%s message still sends the operator nowhere: %q", driverType, message)
			}
		}
	}
}

// R-053. The mysql client's own words never leave the driver, so the driver
// has to read them. What travels is the meaning, and the meaning survives the
// wrapping every caller adds on the way out.
func TestWrapDatabaseEngineFailureReadsTheCommandOutput(t *testing.T) {
	accessDenied := []byte("ERROR 1045 (28000): Access denied for user 'root'@'localhost' (using password: NO)\n")
	cause := errors.New("MariaDB command failed: exit status 1")

	wrapped := WrapDatabaseEngineFailure(cause, accessDenied)
	if got := ClassifyDatabaseEngineRefusal(wrapped); got != DatabaseEngineRefusalCredential {
		t.Fatalf("classification = %d, want %d", got, DatabaseEngineRefusalCredential)
	}
	// The command's text is not carried: it can echo the statement it failed
	// on, and a statement can carry a password.
	if strings.Contains(wrapped.Error(), "Access denied") {
		t.Fatalf("the command's own text travelled: %q", wrapped.Error())
	}
	if !errors.Is(wrapped, cause) {
		t.Fatal("the cause is no longer reachable")
	}
	outer := fmt.Errorf("create physical database user: %w", wrapped)
	if got := ClassifyDatabaseEngineRefusal(outer); got != DatabaseEngineRefusalCredential {
		t.Fatalf("classification through wrapping = %d", got)
	}
}

func TestWrapDatabaseEngineFailureLeavesUnreadableOutputAlone(t *testing.T) {
	cause := errors.New("MariaDB command failed: exit status 1")
	if got := WrapDatabaseEngineFailure(cause, []byte("ERROR 1064: You have an error in your SQL syntax")); got != cause {
		t.Fatalf("an unrecognised output was wrapped: %v", got)
	}
	if got := WrapDatabaseEngineFailure(cause, nil); got != cause {
		t.Fatalf("empty output was wrapped: %v", got)
	}
	if got := WrapDatabaseEngineFailure(nil, []byte("Access denied for user")); got != nil {
		t.Fatalf("a nil cause produced an error: %v", got)
	}
}

func TestWrapDatabaseEngineFailureNamesAnEngineThatIsNotRunning(t *testing.T) {
	notRunning := []byte("ERROR 2002 (HY000): Can't connect to local server through socket '/run/mysqld/mysqld.sock' (2)\n")
	wrapped := WrapDatabaseEngineFailure(errors.New("MariaDB command failed: exit status 1"), notRunning)
	if got := ClassifyDatabaseEngineRefusal(wrapped); got != DatabaseEngineRefusalUnreachable {
		t.Fatalf("classification = %d, want %d", got, DatabaseEngineRefusalUnreachable)
	}
}
