-- One global, case-insensitive hostname namespace for every tenant.
--
-- A primary domain, a top-level domain's implicit www name, every domain's
-- implicit mail name and an explicit alias all reserve their public hostname
-- here. The source tables keep their
-- existing shape for backward compatibility; triggers make the reservation
-- part of the same SQLite statement/transaction, so check-then-insert races
-- cannot create two owners.
CREATE TABLE hostname_reservations (
    hostname TEXT NOT NULL COLLATE NOCASE PRIMARY KEY,
    domain_id INTEGER NOT NULL REFERENCES domains(id) ON DELETE CASCADE,
    source_kind TEXT NOT NULL CHECK(source_kind IN ('primary', 'implicit_www', 'implicit_mail', 'alias')),
    source_id INTEGER NOT NULL,
    created_at TEXT DEFAULT (datetime('now'))
);

CREATE UNIQUE INDEX idx_hostname_reservations_source
    ON hostname_reservations(source_kind, source_id);
CREATE INDEX idx_hostname_reservations_domain
    ON hostname_reservations(domain_id);

-- This trigger is deliberately installed before the legacy backfill. Any
-- unsafe historic hostname aborts the whole migration with a useful message;
-- no source row is deleted or overwritten.
CREATE TRIGGER trg_hostname_reservations_reject_invalid
BEFORE INSERT ON hostname_reservations
WHEN NEW.hostname = ''
  OR length(NEW.hostname) > 253
  OR instr(NEW.hostname, '.') = 0
  OR substr(NEW.hostname, 1, 1) IN ('.', '-')
  OR substr(NEW.hostname, -1, 1) IN ('.', '-')
  OR instr(NEW.hostname, '..') > 0
  OR instr(NEW.hostname, '.-') > 0
  OR instr(NEW.hostname, '-.') > 0
  OR NEW.hostname GLOB '*[^a-z0-9.-]*'
  OR EXISTS (
      WITH RECURSIVE labels(label, rest) AS (
          SELECT
              CASE
                  WHEN instr(NEW.hostname, '.') = 0 THEN NEW.hostname
                  ELSE substr(NEW.hostname, 1, instr(NEW.hostname, '.') - 1)
              END,
              CASE
                  WHEN instr(NEW.hostname, '.') = 0 THEN ''
                  ELSE substr(NEW.hostname, instr(NEW.hostname, '.') + 1)
              END
          UNION ALL
          SELECT
              CASE
                  WHEN instr(rest, '.') = 0 THEN rest
                  ELSE substr(rest, 1, instr(rest, '.') - 1)
              END,
              CASE
                  WHEN instr(rest, '.') = 0 THEN ''
                  ELSE substr(rest, instr(rest, '.') + 1)
              END
          FROM labels
          WHERE rest <> ''
      ),
      validation AS (
          SELECT
              MAX(length(label) > 63) AS has_long_label,
              COUNT(*) AS label_count,
              SUM(
                  CASE
                      WHEN label <> ''
                       AND label NOT GLOB '*[^0-9]*'
                       AND length(label) <= 3
                       AND CAST(label AS INTEGER) BETWEEN 0 AND 255
                       AND (length(label) = 1 OR substr(label, 1, 1) <> '0')
                      THEN 1
                      ELSE 0
                  END
              ) AS ipv4_label_count
          FROM labels
      )
      SELECT 1
      FROM validation
      WHERE has_long_label = 1
         OR (label_count = 4 AND ipv4_label_count = 4)
  )
BEGIN
    SELECT RAISE(
        ABORT,
        'hostname namespace invalid: names and their implicit www/mail hostnames must be canonical lowercase FQDNs within DNS length limits; fix the legacy domain or alias before upgrading'
    );
END;

-- A fixed trigger error is clearer than whichever UNIQUE index SQLite happens
-- to report. It also makes legacy conflicts fail before any normalization.
CREATE TRIGGER trg_hostname_reservations_reject_conflict
BEFORE INSERT ON hostname_reservations
WHEN EXISTS (
    SELECT 1
    FROM hostname_reservations
    WHERE hostname = NEW.hostname COLLATE NOCASE
)
BEGIN
    SELECT RAISE(
        ABORT,
        'hostname namespace conflict: a primary domain, implicit www name, implicit mail name, or alias already owns this hostname; resolve the legacy duplicate before upgrading'
    );
END;

-- Backfill first. The migration transaction rolls back in full if any primary,
-- implicit-www, implicit-mail or alias candidate collides.
INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
SELECT
    lower(
        CASE
            WHEN substr(trim(name), -1, 1) = '.'
                THEN substr(trim(name), 1, length(trim(name)) - 1)
            ELSE trim(name)
        END
    ),
    id,
    'primary',
    id
FROM domains
ORDER BY id;

INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
SELECT
    'www.' || lower(
        CASE
            WHEN substr(trim(name), -1, 1) = '.'
                THEN substr(trim(name), 1, length(trim(name)) - 1)
            ELSE trim(name)
        END
    ),
    id,
    'implicit_www',
    id
FROM domains
WHERE parent_domain_id IS NULL
ORDER BY id;

INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
SELECT
    'mail.' || lower(
        CASE
            WHEN substr(trim(name), -1, 1) = '.'
                THEN substr(trim(name), 1, length(trim(name)) - 1)
            ELSE trim(name)
        END
    ),
    id,
    'implicit_mail',
    id
FROM domains
ORDER BY id;

INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
SELECT
    lower(
        CASE
            WHEN substr(trim(alias), -1, 1) = '.'
                THEN substr(trim(alias), 1, length(trim(alias)) - 1)
            ELSE trim(alias)
        END
    ),
    domain_id,
    'alias',
    id
FROM domain_aliases
ORDER BY id;

-- DNS names are case-insensitive. Lowercasing plus trimming outer whitespace
-- and one root-label dot is a deterministic, semantics-preserving repair.
UPDATE domains
SET name = lower(
    CASE
        WHEN substr(trim(name), -1, 1) = '.'
            THEN substr(trim(name), 1, length(trim(name)) - 1)
        ELSE trim(name)
    END
);

UPDATE domain_aliases
SET alias = lower(
    CASE
        WHEN substr(trim(alias), -1, 1) = '.'
            THEN substr(trim(alias), 1, length(trim(alias)) - 1)
        ELSE trim(alias)
    END
);

-- Keep fast same-table lookups and provide a second line of defence for code
-- that bypasses the reservation lookup.
CREATE UNIQUE INDEX idx_domains_name_canonical
    ON domains(name COLLATE NOCASE);
CREATE UNIQUE INDEX idx_domain_aliases_alias_canonical
    ON domain_aliases(alias COLLATE NOCASE);

-- Source rows themselves must stay in canonical form. Application handlers
-- canonicalize first; these guards protect imports, maintenance SQL and future
-- write paths from silently storing a second representation.
CREATE TRIGGER trg_domains_hostname_canonical_insert
BEFORE INSERT ON domains
WHEN NEW.name <> lower(
    CASE
        WHEN substr(trim(NEW.name), -1, 1) = '.'
            THEN substr(trim(NEW.name), 1, length(trim(NEW.name)) - 1)
        ELSE trim(NEW.name)
    END
)
BEGIN
    SELECT RAISE(ABORT, 'hostname must be stored as a canonical lowercase FQDN');
END;

CREATE TRIGGER trg_domains_hostname_canonical_update
BEFORE UPDATE OF name ON domains
WHEN NEW.name <> lower(
    CASE
        WHEN substr(trim(NEW.name), -1, 1) = '.'
            THEN substr(trim(NEW.name), 1, length(trim(NEW.name)) - 1)
        ELSE trim(NEW.name)
    END
)
BEGIN
    SELECT RAISE(ABORT, 'hostname must be stored as a canonical lowercase FQDN');
END;

CREATE TRIGGER trg_domain_aliases_hostname_canonical_insert
BEFORE INSERT ON domain_aliases
WHEN NEW.alias <> lower(
    CASE
        WHEN substr(trim(NEW.alias), -1, 1) = '.'
            THEN substr(trim(NEW.alias), 1, length(trim(NEW.alias)) - 1)
        ELSE trim(NEW.alias)
    END
)
BEGIN
    SELECT RAISE(ABORT, 'hostname must be stored as a canonical lowercase FQDN');
END;

CREATE TRIGGER trg_domain_aliases_hostname_canonical_update
BEFORE UPDATE OF alias ON domain_aliases
WHEN NEW.alias <> lower(
    CASE
        WHEN substr(trim(NEW.alias), -1, 1) = '.'
            THEN substr(trim(NEW.alias), 1, length(trim(NEW.alias)) - 1)
        ELSE trim(NEW.alias)
    END
)
BEGIN
    SELECT RAISE(ABORT, 'hostname must be stored as a canonical lowercase FQDN');
END;

-- Domain writes reserve the primary name, every domain's implicit mail name
-- and (only for a top-level domain) its implicit www name.
CREATE TRIGGER trg_domains_hostname_reserve_insert
AFTER INSERT ON domains
BEGIN
    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    VALUES (NEW.name, NEW.id, 'primary', NEW.id);

    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    SELECT 'www.' || NEW.name, NEW.id, 'implicit_www', NEW.id
    WHERE NEW.parent_domain_id IS NULL;

    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    VALUES ('mail.' || NEW.name, NEW.id, 'implicit_mail', NEW.id);
END;

CREATE TRIGGER trg_domains_hostname_reserve_update
AFTER UPDATE OF name, parent_domain_id ON domains
BEGIN
    DELETE FROM hostname_reservations
    WHERE source_id = OLD.id
      AND source_kind IN ('primary', 'implicit_www', 'implicit_mail');

    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    VALUES (NEW.name, NEW.id, 'primary', NEW.id);

    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    SELECT 'www.' || NEW.name, NEW.id, 'implicit_www', NEW.id
    WHERE NEW.parent_domain_id IS NULL;

    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    VALUES ('mail.' || NEW.name, NEW.id, 'implicit_mail', NEW.id);
END;

CREATE TRIGGER trg_domain_aliases_hostname_reserve_insert
AFTER INSERT ON domain_aliases
BEGIN
    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    VALUES (NEW.alias, NEW.domain_id, 'alias', NEW.id);
END;

CREATE TRIGGER trg_domain_aliases_hostname_reserve_update
AFTER UPDATE OF alias, domain_id ON domain_aliases
BEGIN
    DELETE FROM hostname_reservations
    WHERE source_kind = 'alias' AND source_id = OLD.id;

    INSERT INTO hostname_reservations (hostname, domain_id, source_kind, source_id)
    VALUES (NEW.alias, NEW.domain_id, 'alias', NEW.id);
END;

CREATE TRIGGER trg_domain_aliases_hostname_reserve_delete
AFTER DELETE ON domain_aliases
BEGIN
    DELETE FROM hostname_reservations
    WHERE source_kind = 'alias' AND source_id = OLD.id;
END;
