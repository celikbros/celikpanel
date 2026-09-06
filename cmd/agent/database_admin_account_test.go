package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// stubEngineStatements replaces both engine clients for the duration of a test
// and records every statement they were given.
// stubEngineStatements, iki motor istemcisini de degistirir ve verilen her
// ifadeyi kaydeder.
func stubEngineStatements(t *testing.T, output []byte, failure error) *[]string {
	t.Helper()
	recorded := []string{}
	previousMySQL := mysqlExecStatement
	previousPostgreSQL := postgreSQLExecStatement
	run := func(statement string) ([]byte, error) {
		recorded = append(recorded, statement)
		return output, failure
	}
	mysqlExecStatement = run
	postgreSQLExecStatement = run
	t.Cleanup(func() {
		mysqlExecStatement = previousMySQL
		postgreSQLExecStatement = previousPostgreSQL
	})
	return &recorded
}

// R-061 is the rule this test exists for. The statement that opens the panel's
// account contains the panel's password, and both clients answer a refusal by
// quoting the statement they would not run. So the one thing that must never
// happen here is the engine's own words reaching the response - which is a
// response the browser renders.
//
// R-061, bu testin var olma sebebidir. Panelin hesabini acan ifade panelin
// parolasini tasir ve iki istemci de bir reddi, calistirmadiklari ifadeyi
// alintilayarak yanitlar.
func TestProvisioningNeverRepeatsTheEngineWordsThatCarryThePassword(t *testing.T) {
	const password = "a-password-nobody-should-see-twice"

	for _, engine := range []string{"mariadb", "postgresql"} {
		t.Run(engine, func(t *testing.T) {
			// What each client actually does when it refuses: it quotes back
			// the statement, password and all.
			// Her istemcinin bir reddi nasil yanitladigi: ifadeyi, parolasiyla
			// birlikte geri yazar.
			refusal := []byte(
				"ERROR:  syntax error at or near \"IDENTIFIED\"\n" +
					"LINE 1: ALTER USER \"celikpanel_admin\" IDENTIFIED BY '" + password + "'\n")
			stubEngineStatements(t, refusal, errors.New("exit status 1"))

			agent := &Agent{}
			resp := &transport.ProvisionDatabaseAdminAccountResponse{}
			if err := agent.ProvisionDatabaseAdminAccount(
				transport.ProvisionDatabaseAdminAccountRequest{Engine: engine, Password: password},
				resp,
			); err != nil {
				t.Fatal(err)
			}

			if resp.Provisioned {
				t.Fatal("a refused statement was reported as a provisioned account")
			}
			if strings.Contains(resp.Error, password) {
				t.Fatalf("the password came back in the response: %q", resp.Error)
			}
			if strings.Contains(resp.Error, "IDENTIFIED BY") {
				t.Fatalf("the engine's own statement came back in the response: %q", resp.Error)
			}
			if strings.TrimSpace(resp.Error) == "" {
				t.Fatal("the failure was reported with no reason at all")
			}
		})
	}
}

// The account the agent creates is not the caller's to choose. There is no
// field for it on the request, and this pins that: an agent that accepted an
// account name would accept a request to give full privileges to any name.
//
// Agent'in olusturdugu hesap, cagiranin secebilecegi bir sey degildir.
func TestTheProvisionedAccountIsTheOneTheAgentDecides(t *testing.T) {
	for _, engine := range []string{"mariadb", "postgresql"} {
		t.Run(engine, func(t *testing.T) {
			recorded := stubEngineStatements(t, nil, nil)

			agent := &Agent{}
			resp := &transport.ProvisionDatabaseAdminAccountResponse{}
			if err := agent.ProvisionDatabaseAdminAccount(
				transport.ProvisionDatabaseAdminAccountRequest{
					Engine: engine, Password: "generated-by-the-panel",
				}, resp,
			); err != nil {
				t.Fatal(err)
			}
			if !resp.Provisioned {
				t.Fatalf("provisioning failed: %s", resp.Error)
			}
			if resp.Username != transport.DatabaseAdminAccountName {
				t.Fatalf("provisioned %q, want %q", resp.Username, transport.DatabaseAdminAccountName)
			}
			for _, statement := range *recorded {
				if !strings.Contains(statement, transport.DatabaseAdminAccountName) {
					t.Errorf("a statement named no account at all: %q", statement)
				}
			}
		})
	}
}

// The operator's own account is the thing this whole design exists to leave
// alone, so no statement it composes may mention root or the postgres role.
// Operatorun kendi hesabi, bu tasarimin dokunmamak icin var oldugu seydir.
func TestProvisioningNeverTouchesTheOperatorsOwnAccount(t *testing.T) {
	for _, engine := range []string{"mariadb", "postgresql"} {
		t.Run(engine, func(t *testing.T) {
			recorded := stubEngineStatements(t, nil, nil)

			agent := &Agent{}
			resp := &transport.ProvisionDatabaseAdminAccountResponse{}
			if err := agent.ProvisionDatabaseAdminAccount(
				transport.ProvisionDatabaseAdminAccountRequest{
					Engine: engine, Password: "generated-by-the-panel",
				}, resp,
			); err != nil {
				t.Fatal(err)
			}

			for _, statement := range *recorded {
				lowered := strings.ToLower(statement)
				// pg_roles is the catalogue the existence check reads and is
				// not an account; everything else naming root or postgres
				// would be this design touching what it promised not to.
				// pg_roles bir katalogdur, hesap degil.
				stripped := strings.ReplaceAll(lowered, "pg_roles", "")
				for _, forbidden := range []string{"'root'", "\"root\"", "`root`", "postgres"} {
					if strings.Contains(stripped, forbidden) {
						t.Errorf("a statement names the operator's own account (%s): %q",
							forbidden, statement)
					}
				}
			}
		})
	}
}

// PostgreSQL gets the password in a statement of its own, so it is quoted once
// rather than nested inside the dollar-quoted body that creates the role.
// PostgreSQL parolayi kendi ifadesinde alir; boylece rolu olusturan
// dolar-tirnakli govdenin icine gomulmek yerine bir kez tirnaklanir.
func TestPostgreSQLTakesThePasswordInAStatementOfItsOwn(t *testing.T) {
	const password = "generated-by-the-panel"
	recorded := stubEngineStatements(t, nil, nil)

	agent := &Agent{}
	resp := &transport.ProvisionDatabaseAdminAccountResponse{}
	if err := agent.ProvisionDatabaseAdminAccount(
		transport.ProvisionDatabaseAdminAccountRequest{Engine: "postgresql", Password: password},
		resp,
	); err != nil {
		t.Fatal(err)
	}

	if len(*recorded) != 2 {
		t.Fatalf("ran %d statements, want the create and the password as two: %q", len(*recorded), *recorded)
	}
	create, alter := (*recorded)[0], (*recorded)[1]
	if strings.Contains(create, password) {
		t.Errorf("the password was nested inside the role-creating block: %q", create)
	}
	if !strings.Contains(create, "$celikpanel_admin_account$") {
		t.Errorf("the block is not dollar-quoted with a named tag: %q", create)
	}
	if !strings.Contains(alter, password) {
		t.Errorf("the password statement does not carry the password: %q", alter)
	}
	// The smaller grant is real on this engine and is worth pinning, because
	// quietly widening it later would be invisible.
	// Bu motorda daha kucuk yetki gercektir ve sabitlemeye deger.
	if strings.Contains(strings.ToUpper(create+alter), "SUPERUSER") {
		t.Errorf("the PostgreSQL account was made a superuser: %q", *recorded)
	}
	for _, want := range []string{"CREATEDB", "CREATEROLE", "LOGIN"} {
		if !strings.Contains(create+alter, want) {
			t.Errorf("the account was not granted %s: %q", want, *recorded)
		}
	}
}

// A password the panel never generated is not something to send to an engine.
// Panelin hic uretmedigi bir parola, bir motora gonderilecek bir sey degildir.
func TestProvisioningRefusesAnEmptyPasswordBeforeReachingTheEngine(t *testing.T) {
	recorded := stubEngineStatements(t, nil, nil)

	agent := &Agent{}
	resp := &transport.ProvisionDatabaseAdminAccountResponse{}
	if err := agent.ProvisionDatabaseAdminAccount(
		transport.ProvisionDatabaseAdminAccountRequest{Engine: "mariadb", Password: "   "},
		resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Provisioned {
		t.Fatal("an empty password provisioned an account")
	}
	if len(*recorded) != 0 {
		t.Fatalf("the engine was contacted anyway: %q", *recorded)
	}
}

// An engine the agent does not know how to open an account on is refused by
// name rather than half-attempted.
// Agent'in hesap acmayi bilmedigi bir motor, yarim denenmek yerine reddedilir.
func TestProvisioningRefusesAnUnknownEngine(t *testing.T) {
	recorded := stubEngineStatements(t, nil, nil)

	agent := &Agent{}
	resp := &transport.ProvisionDatabaseAdminAccountResponse{}
	if err := agent.ProvisionDatabaseAdminAccount(
		transport.ProvisionDatabaseAdminAccountRequest{Engine: "mssql", Password: "x"},
		resp,
	); err != nil {
		t.Fatal(err)
	}
	if resp.Provisioned || strings.TrimSpace(resp.Error) == "" {
		t.Fatalf("an unknown engine was not refused: %+v", resp)
	}
	if len(*recorded) != 0 {
		t.Fatalf("an unknown engine was contacted anyway: %q", *recorded)
	}
}

// Removing the panel's account removes exactly that account.
// Panelin hesabini kaldirmak tam olarak o hesabi kaldirir.
func TestRemovingThePanelsAccountNamesOnlyThatAccount(t *testing.T) {
	for _, engine := range []string{"mariadb", "postgresql"} {
		t.Run(engine, func(t *testing.T) {
			recorded := stubEngineStatements(t, nil, nil)

			agent := &Agent{}
			resp := &transport.RemoveDatabaseAdminAccountResponse{}
			if err := agent.RemoveDatabaseAdminAccount(
				transport.RemoveDatabaseAdminAccountRequest{Engine: engine}, resp,
			); err != nil {
				t.Fatal(err)
			}
			if !resp.Removed {
				t.Fatalf("removal failed: %s", resp.Error)
			}
			if len(*recorded) != 1 {
				t.Fatalf("ran %d statements to remove one account: %q", len(*recorded), *recorded)
			}
			statement := (*recorded)[0]
			if !strings.Contains(statement, transport.DatabaseAdminAccountName) {
				t.Errorf("the removal names no account: %q", statement)
			}
			if strings.Contains(strings.ToLower(statement), "postgres") ||
				strings.Contains(statement, "root") {
				t.Errorf("the removal names the operator's own account: %q", statement)
			}
		})
	}
}
