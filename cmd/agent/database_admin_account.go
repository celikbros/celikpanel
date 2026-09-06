package main

import (
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostcmd"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// R-057. The panel opens an account of its own on an engine rather than taking
// the operator's. This file is the only place that happens, and it is the only
// place that uses the machine's own privileged door to a database engine for
// anything other than a tenant's database.
//
// The door itself is not new and is not a trick. The agent runs as root, so
// `mysql` reaches a freshly packaged MariaDB as root@localhost over the unix
// socket, and `psql` reaches a freshly packaged PostgreSQL as the postgres
// role through peer authentication. That is exactly what a person
// administering this machine would type. What matters is the next step: having
// gone through that door, the panel creates a *new* identity for itself and
// uses that from then on. It does not set the operator's root password, does
// not change one, and does not record one.
//
// docs/DATABASE-ADMIN-ACCOUNT.md records the decision and what each engine's
// grant actually is - including that on MariaDB it is root-equivalent in
// power, which is said plainly there rather than dressed up.
//
// R-057. Panel, operatorun hesabini almak yerine motorda kendi hesabini acar.
// Bu dosya, bunun gerceklestigi tek yerdir. Kapinin kendisi yeni degil: agent
// kok olarak calisir, dolayisiyla `mysql` MariaDB'ye unix soketi uzerinden,
// `psql` ise PostgreSQL'e peer dogrulamasiyla ulasir - bu makineyi yoneten bir
// kisinin yazacaginin aynisi. Onemli olan sonraki adim: panel o kapidan gecip
// kendine YENI bir kimlik olusturur ve bundan sonra onu kullanir. Operatorun
// kok parolasini ne belirler, ne degistirir, ne de kaydeder.

// ProvisionDatabaseAdminAccountRequest / Response are the transport contract.
type ProvisionDatabaseAdminAccountRequest = transport.ProvisionDatabaseAdminAccountRequest

// ProvisionDatabaseAdminAccountResponse reports what the engine now has.
type ProvisionDatabaseAdminAccountResponse = transport.ProvisionDatabaseAdminAccountResponse

// RemoveDatabaseAdminAccountRequest / Response are the transport contract.
type RemoveDatabaseAdminAccountRequest = transport.RemoveDatabaseAdminAccountRequest

// RemoveDatabaseAdminAccountResponse reports whether the account is gone.
type RemoveDatabaseAdminAccountResponse = transport.RemoveDatabaseAdminAccountResponse

// databaseAdminAccountName is the only account this file will ever create. The
// name comes from the shared contract rather than a literal here so the panel
// and the agent cannot come to hold different beliefs about what the panel's
// account is called.
// databaseAdminAccountName, bu dosyanin olusturacagi tek hesaptir.
const databaseAdminAccountName = transport.DatabaseAdminAccountName

// ProvisionDatabaseAdminAccount makes the panel's own account exist on the
// named engine with the given password, and gives it the privileges the panel
// needs. Create and rotate are the same call: a panel that had to know which
// one it was doing would have to hold a belief about the engine that could be
// wrong, and being wrong would mean either refusing to fix a broken account or
// refusing to create a missing one.
//
// ProvisionDatabaseAdminAccount, panelin kendi hesabini adi gecen motorda
// verilen parolayla var eder. Olusturma ve degistirme ayni cagridir.
func (a *Agent) ProvisionDatabaseAdminAccount(
	req ProvisionDatabaseAdminAccountRequest,
	resp *ProvisionDatabaseAdminAccountResponse,
) error {
	if resp == nil {
		return fmt.Errorf("provision database admin account: no response to fill")
	}
	resp.Username = databaseAdminAccountName

	if strings.TrimSpace(req.Password) == "" {
		resp.Error = "CelikPanel did not supply a password for its own database account"
		return nil
	}

	switch req.Engine {
	case "mariadb":
		return a.provisionMariaDBAdminAccount(req, resp)
	case "postgresql":
		return a.provisionPostgreSQLAdminAccount(req, resp)
	default:
		resp.Error = "CelikPanel does not know how to open an account on this kind of database engine"
		return nil
	}
}

// RemoveDatabaseAdminAccount takes the panel's own account off the engine. The
// operator's own way in is untouched, which is the whole reason there is a
// separate account to remove.
// RemoveDatabaseAdminAccount, panelin kendi hesabini motordan kaldirir.
func (a *Agent) RemoveDatabaseAdminAccount(
	req RemoveDatabaseAdminAccountRequest,
	resp *RemoveDatabaseAdminAccountResponse,
) error {
	if resp == nil {
		return fmt.Errorf("remove database admin account: no response to fill")
	}
	switch req.Engine {
	case "mariadb":
		userLiteral, err := services.QuoteMySQLStringLiteral(databaseAdminAccountName)
		if err != nil {
			resp.Error = "CelikPanel could not compose the statement to remove its own account"
			return nil
		}
		statement := fmt.Sprintf(
			"DROP USER IF EXISTS %s@'localhost'; FLUSH PRIVILEGES;", userLiteral)
		if output, err := mysqlExecStatement(statement); err != nil {
			resp.Error = hostcmd.Fail(
				"failed to remove CelikPanel's own database account",
				output, err, databaseClientMeaning,
			).Error()
			return nil
		}
	case "postgresql":
		ident, err := services.QuotePGIdentifier(databaseAdminAccountName)
		if err != nil {
			resp.Error = "CelikPanel could not compose the statement to remove its own account"
			return nil
		}
		if output, err := postgreSQLExecStatement(
			fmt.Sprintf("DROP ROLE IF EXISTS %s;", ident),
		); err != nil {
			resp.Error = hostcmd.Fail(
				"failed to remove CelikPanel's own database account",
				output, err, databaseClientMeaning,
			).Error()
			return nil
		}
	default:
		resp.Error = "CelikPanel does not know how to open an account on this kind of database engine"
		return nil
	}
	resp.Removed = true
	return nil
}

// provisionMariaDBAdminAccount. CREATE USER IF NOT EXISTS followed by ALTER
// USER is create-or-rotate in two statements that are each idempotent: the
// first does nothing when the account is already there, and the second sets
// the password whether it was just created or has been there for a year.
//
// The grant is ALL PRIVILEGES ON *.* WITH GRANT OPTION because MariaDB has no
// smaller shape that still lets an account create databases, create users, and
// grant those users rights - which is the whole of what the panel does. That
// is root-equivalent in power and docs/DATABASE-ADMIN-ACCOUNT.md says so
// rather than claiming least privilege it does not have. What it still buys is
// a separate, revocable identity and an operator whose own root keeps working.
//
// provisionMariaDBAdminAccount. Yetki, ALL PRIVILEGES ON *.* WITH GRANT
// OPTION'dir; cunku MariaDB'de hesap olusturup bu hesaplara yetki verebilen
// daha kucuk bir bicim yoktur. Bu, guc bakimindan kok esdegeridir ve belge
// bunu oldugu gibi soyler.
func (a *Agent) provisionMariaDBAdminAccount(
	req ProvisionDatabaseAdminAccountRequest,
	resp *ProvisionDatabaseAdminAccountResponse,
) error {
	userLiteral, err := services.QuoteMySQLStringLiteral(databaseAdminAccountName)
	if err != nil {
		resp.Error = "CelikPanel could not compose the statement to open its own account"
		return nil
	}
	passwordLiteral, err := services.QuoteMySQLStringLiteral(req.Password)
	if err != nil {
		resp.Error = "CelikPanel generated a password its own database engine will not accept"
		return nil
	}

	statement := strings.Join([]string{
		fmt.Sprintf("CREATE USER IF NOT EXISTS %s@'localhost' IDENTIFIED BY %s;",
			userLiteral, passwordLiteral),
		fmt.Sprintf("ALTER USER %s@'localhost' IDENTIFIED BY %s;",
			userLiteral, passwordLiteral),
		fmt.Sprintf("GRANT ALL PRIVILEGES ON *.* TO %s@'localhost' WITH GRANT OPTION;",
			userLiteral),
		"FLUSH PRIVILEGES;",
	}, " ")

	// hostcmd.Fail, never Verbatim: the statement above contains the password
	// and the client quotes the statement back when it refuses it (R-061).
	// hostcmd.Fail, asla Verbatim degil: yukaridaki ifade parolayi tasir.
	if output, err := mysqlExecStatement(statement); err != nil {
		resp.Error = hostcmd.Fail(
			"failed to open CelikPanel's own account on this MariaDB server",
			output, err, databaseClientMeaning,
		).Error()
		return nil
	}

	resp.Provisioned = true
	return nil
}

// provisionPostgreSQLAdminAccount. The role is created without a password
// inside a DO block and then given one by a plain ALTER ROLE, so the password
// never has to be a string literal nested inside another quoted body - one
// level of quoting is one level of things that can go wrong.
//
// The grant is LOGIN CREATEDB CREATEROLE and deliberately not SUPERUSER. Here
// the smaller grant is real: the account cannot read arbitrary files, load
// extensions, or bypass row-level security. The panel creates every database
// it manages, so it owns them, and ownership carries the rest.
//
// provisionPostgreSQLAdminAccount. Rol, bir DO blogu icinde parolasiz
// olusturulur ve parolayi duz bir ALTER ROLE ile alir; boylece parola, baska
// bir tirnakli govdenin icine gomulu bir literal olmak zorunda kalmaz. Yetki
// LOGIN CREATEDB CREATEROLE'dur ve bilerek SUPERUSER degildir.
func (a *Agent) provisionPostgreSQLAdminAccount(
	req ProvisionDatabaseAdminAccountRequest,
	resp *ProvisionDatabaseAdminAccountResponse,
) error {
	ident, err := services.QuotePGIdentifier(databaseAdminAccountName)
	if err != nil {
		resp.Error = "CelikPanel could not compose the statement to open its own account"
		return nil
	}
	nameLiteral, err := services.QuotePGStringLiteral(databaseAdminAccountName)
	if err != nil {
		resp.Error = "CelikPanel could not compose the statement to open its own account"
		return nil
	}
	passwordLiteral, err := services.QuotePGStringLiteral(req.Password)
	if err != nil {
		resp.Error = "CelikPanel generated a password its own database engine will not accept"
		return nil
	}

	// A named dollar-quote tag rather than $$, so a body that happens to
	// contain $$ cannot end the block early.
	// $$ yerine adlandirilmis bir dolar-tirnak etiketi.
	create := fmt.Sprintf(`DO $celikpanel_admin_account$
BEGIN
    IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = %s) THEN
        CREATE ROLE %s LOGIN CREATEDB CREATEROLE;
    END IF;
END
$celikpanel_admin_account$;`, nameLiteral, ident)

	if output, err := postgreSQLExecStatement(create); err != nil {
		resp.Error = hostcmd.Fail(
			"failed to open CelikPanel's own account on this PostgreSQL server",
			output, err, databaseClientMeaning,
		).Error()
		return nil
	}

	// Separate statement, so the password is quoted once and only once.
	// Ayri ifade; boylece parola bir kez ve yalnizca bir kez tirnaklanir.
	alter := fmt.Sprintf("ALTER ROLE %s WITH LOGIN CREATEDB CREATEROLE PASSWORD %s;",
		ident, passwordLiteral)
	if output, err := postgreSQLExecStatement(alter); err != nil {
		resp.Error = hostcmd.Fail(
			"failed to set the password on CelikPanel's own PostgreSQL account",
			output, err, databaseClientMeaning,
		).Error()
		return nil
	}

	resp.Provisioned = true
	return nil
}
