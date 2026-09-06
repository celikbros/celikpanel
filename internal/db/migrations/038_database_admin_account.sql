-- R-057. The panel opens an account of its own on an engine it installed
-- rather than telling the operator to set a root password it then cannot be
-- given anywhere. The credential column already existed; what was missing was
-- who the credential belongs to, because the account was assumed from the
-- engine type - root on MariaDB, postgres on PostgreSQL.
--
-- NULL keeps meaning exactly what every existing row means: the engine's own
-- superuser. Nothing is backfilled, so no registered server changes behaviour
-- when this runs. A row only stops meaning that when the panel has actually
-- opened an account and written its name here.
--
-- The column beside it keeps its released name, root_password_encrypted, even
-- though the credential it holds may no longer be root's. Two key-rotation
-- sweeps address it by that name and the migration ledger records the file
-- that created it; renaming would put those at risk to buy nothing they do
-- not already have. The Go field is what a developer touches and that one is
-- named honestly.

ALTER TABLE database_servers ADD COLUMN admin_username TEXT;
