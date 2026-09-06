package transport

// R-057. The panel installs a database engine and then cannot use it: a
// freshly packaged MariaDB admits root only through the machine's unix socket
// and a freshly packaged PostgreSQL leaves the postgres role with no password,
// so the empty credential the panel holds is refused by both. The answer is
// that the panel opens an account of its own rather than taking the
// operator's. docs/DATABASE-ADMIN-ACCOUNT.md records why.
//
// R-057. Panel bir veritabani motoru kurar ve sonra onu kullanamaz. Yanit,
// panelin operatorun hesabini almasi degil kendi hesabini acmasidir.

// DatabaseAdminAccountName is the account the panel opens for itself. It is a
// constant rather than a field on the request on purpose: an agent that
// accepts an account name would accept a request to give full privileges to
// any name at all, and nothing needs that. The panel knows the name from here,
// the agent creates only this name, and neither can be talked into another.
//
// DatabaseAdminAccountName, panelin kendisi icin actigi hesaptir. Istek uzerinde
// bir alan degil sabit olmasi bilinclidir: hesap adi kabul eden bir agent,
// herhangi bir ada tam yetki verme istegini de kabul ederdi.
const DatabaseAdminAccountName = "celikpanel_admin"

// ProvisionDatabaseAdminAccountRequest asks the agent to make the panel's own
// account exist on a database engine on this machine, with this password.
//
// It is deliberately create-or-update. Rotating the password and creating the
// account for the first time are the same host statement, and a panel that
// had to know which one it was doing would have to keep a belief about the
// engine that could be wrong.
//
// ProvisionDatabaseAdminAccountRequest, panelin kendi hesabinin bu makinedeki
// bir veritabani motorunda, bu parolayla var olmasini ister. Bilerek
// olustur-veya-guncelle bicimindedir.
type ProvisionDatabaseAdminAccountRequest struct {
	// Engine is the panel's own engine identifier: "mariadb" or "postgresql".
	// Engine, panelin kendi motor kimligidir.
	Engine string
	// Password is the credential the panel has generated and sealed. It never
	// reaches a process argument list on the way to the engine (R-062).
	// Password, panelin uretip muhurledigi kimlik bilgisidir.
	Password string
}

// ProvisionDatabaseAdminAccountResponse reports what the engine now has.
// ProvisionDatabaseAdminAccountResponse, motorda artik ne oldugunu bildirir.
type ProvisionDatabaseAdminAccountResponse struct {
	// Provisioned is true only when the account exists with this password and
	// the privileges the panel needs.
	// Provisioned, yalnizca hesap bu parolayla ve panelin ihtiyaci olan
	// yetkilerle var oldugunda dogrudur.
	Provisioned bool
	// Username is the account the agent actually provisioned, so the panel
	// records what happened rather than what it assumed.
	// Username, agent'in gercekten olusturdugu hesaptir.
	Username string
	// Error is the operator's sentence when it did not happen. It never
	// carries the engine's own words: the statement those words would quote
	// contains the password (R-061).
	// Error, olmadiginda operatorun cumlesidir. Motorun kendi sozlerini asla
	// tasimaz.
	Error string
}

// RemoveDatabaseAdminAccountRequest asks the agent to take the panel's own
// account off an engine. The operator's own way in is untouched by this, which
// is the whole reason the panel has an account to remove.
//
// RemoveDatabaseAdminAccountRequest, panelin kendi hesabini bir motordan
// kaldirmasini ister. Operatorun kendi giris yolu bundan etkilenmez.
type RemoveDatabaseAdminAccountRequest struct {
	Engine string
}

// RemoveDatabaseAdminAccountResponse reports whether the account is gone.
// RemoveDatabaseAdminAccountResponse, hesabin gidip gitmedigini bildirir.
type RemoveDatabaseAdminAccountResponse struct {
	Removed bool
	Error   string
}
