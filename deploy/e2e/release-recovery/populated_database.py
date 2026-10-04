#!/usr/bin/env python3
"""Private SQL-data fixture for genuine Alpha64/schema38 copies.

This is not hosting, API, DNS provisioning or installed-server admission. There
is deliberately no CLI, canonical-path writer, restore or cleanup operation.
The caller must first obtain a standalone copy through its disposable-lab guard.
Both public functions require Linux, private owner-only directories/files and no
sidecars. The source is read through a descriptor, never opened with SQLite.
Failed output directories remain evidence; a missing manifest is not success.
"""
from __future__ import annotations

import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import stat
import struct
import sys
import tempfile
import time

BASELINE_COMMIT = '3ee8dac009c7e3db1d940f9b8be078e186693a80'
MIGRATIONS = {
    38: 'ef6821f3243832de5d19351a31e70db19c6df86f31679971ff77906c349db534',
    42: '4075629507fd66f6d028bafc373bbc97a1d249b513116865b432d022327101a0',
}
# Exact sqlite_schema SQL from the released SQL, including triggers and indexes.
# Tests reconstruct it from unmodified migration bytes, never a downgraded ledger.
SCHEMAS = {
    38: 'e19fe19deee2e280b90be04c15fe4c3bad69ed23e428d0e579c877e9d6eaedcf',
    42: '2754d89b20e2c724c277a7072ac2d837aa41eb2a2a67106b4921e2fa7f015f68',
}
ADMISSION_SCHEMA = 'celikpanel/lab-populated-database-admission/v1'
MANIFEST_SCHEMA = 'celikpanel/lab-populated-database-manifest/v1'
PURPOSE = 'private-sql-data-fixture-not-hosting'
SIDECARS = ('-wal', '-shm', '-journal')
NEW_TABLES_42 = {'server_setup_state', 'server_setup_plans', 'server_setup_executions',
                 'remote_dns_enrollments', 'remote_dns_clients', 'remote_dns_connections',
                 'remote_dns_zone_ownership', 'remote_dns_records', 'remote_dns_zones', 'remote_dns_origin_history'}
MAX_BYTES = 128 * 1024 * 1024
MAX_MANIFEST_BYTES = 16 * 1024 * 1024
MAX_ROWS = 100_000
STAMP = '2000-01-02 03:04:05'
HEX = re.compile(r'[0-9a-f]{64}\Z')
NAME = re.compile(r'[a-zA-Z_][a-zA-Z0-9_]*\Z')


class Refused(ValueError):
    """Evidence or a requested private-copy operation cannot be verified."""


def _json(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(',', ':'), allow_nan=False).encode()


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid,
            info.st_nlink, info.st_size, info.st_mtime_ns, info.st_ctime_ns)


def _private_directory(path):
    if sys.platform != 'linux':
        raise Refused('Linux private-copy fixture required')
    path = Path(path)
    if not path.is_absolute() or str(path) != os.path.normpath(str(path)):
        raise Refused('absolute canonical spelling required')
    # Even root must not use a product tree as this fixture output/source area.
    for forbidden in ('/var/lib/celikpanel', '/opt/celikpanel', '/run/celikpanel'):
        if path == Path(forbidden) or Path(forbidden) in path.parents:
            raise Refused('product paths are not fixture workspaces')
    chain = {}
    for parent in (*reversed(path.parents), path):
        info = parent.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid not in (0, os.geteuid()):
            raise Refused('unsafe directory owner or symlink')
        sticky_root = info.st_uid == 0 and bool(info.st_mode & stat.S_ISVTX)
        if stat.S_IMODE(info.st_mode) & 0o022 and not sticky_root:
            raise Refused('writable ancestor')
        chain[str(parent)] = (info.st_dev, info.st_ino, info.st_mode, info.st_uid, info.st_gid)
    info = path.lstat()
    if info.st_uid != os.geteuid() or stat.S_IMODE(info.st_mode) != 0o700:
        raise Refused('immediate parent must be private mode0700 and owned by caller')
    return chain


def _no_sidecars(path):
    for suffix in SIDECARS:
        try:
            Path(str(path) + suffix).lstat()
        except FileNotFoundError:
            continue
        raise Refused('standalone copy required; sidecar evidence must be preserved')


def _read_private(path):
    """Read stable bytes without SQLite; reject links/devices/metadata changes."""
    path = Path(path)
    parents = _private_directory(path.parent)
    _no_sidecars(path)
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK | os.O_CLOEXEC)
    try:
        before = os.fstat(fd)
        if (not stat.S_ISREG(before.st_mode) or before.st_uid != os.geteuid()
                or stat.S_IMODE(before.st_mode) != 0o600 or before.st_nlink != 1
                or not 0 < before.st_size <= MAX_BYTES or os.listxattr(fd)):
            raise Refused('unsafe private file metadata or size')
        chunks, total = [], 0
        while chunk := os.read(fd, 1024 * 1024):
            total += len(chunk)
            if total > MAX_BYTES:
                raise Refused('file size bound exceeded')
            chunks.append(chunk)
        raw = b''.join(chunks)
        if (_identity(os.fstat(fd)) != _identity(before)
                or _identity(path.lstat()) != _identity(before)
                or _private_directory(path.parent) != parents):
            raise Refused('private source changed while reading')
        _no_sidecars(path)
        return raw, _identity(before)
    finally:
        os.close(fd)


def _write_new(path, raw):
    parents = _private_directory(path.parent)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    try:
        view = memoryview(raw)
        while view:
            written = os.write(fd, view)
            if written <= 0:
                raise Refused('short private output write')
            view = view[written:]
        os.fsync(fd)
    finally:
        os.close(fd)
    if _private_directory(path.parent) != parents:
        raise Refused('private output parent changed')
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW | os.O_CLOEXEC)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def _admission(value):
    fields = {'schema', 'purpose', 'baseline_commit', 'baseline_migrations_sha256', 'source_sha256', 'nonce'}
    if (not isinstance(value, dict) or set(value) != fields
            or value['schema'] != ADMISSION_SCHEMA or value['purpose'] != PURPOSE
            or value['baseline_commit'] != BASELINE_COMMIT
            or value['baseline_migrations_sha256'] != MIGRATIONS[38]
            or not isinstance(value['source_sha256'], str) or not HEX.fullmatch(value['source_sha256'])
            or not isinstance(value['nonce'], str) or not re.fullmatch(r'[0-9a-f]{32}', value['nonce'])):
        raise Refused('exact Alpha64 private-copy admission required')
    return json.loads(_json(value))


def _connect(path, *, writable=False):
    # path is always the newly-created private copy (or verifier-owned input).
    uri = Path(path).as_uri() + ('?mode=rw' if writable else '?mode=ro&immutable=1')
    connection = sqlite3.connect(uri, uri=True, timeout=0, isolation_level=None)
    connection.setlimit(sqlite3.SQLITE_LIMIT_LENGTH, 1024 * 1024)
    deadline = time.monotonic() + 15
    connection.set_progress_handler(lambda: int(time.monotonic() >= deadline), 1000)
    connection.execute('PRAGMA trusted_schema=OFF')
    connection.execute('PRAGMA foreign_keys=ON')
    connection.execute('PRAGMA temp_store=MEMORY')
    if not writable:
        connection.execute('PRAGMA query_only=ON')
    return connection


def _schema(connection, version):
    if type(version) is not int or version not in SCHEMAS:
        raise Refused('only exact schema38 or schema42 is supported')
    objects = connection.execute('SELECT type,name,tbl_name,sql FROM sqlite_schema ORDER BY type,name').fetchall()
    if _sha(json.dumps(objects, ensure_ascii=True, separators=(',', ':')).encode()) != SCHEMAS[version]:
        raise Refused('schema SQL differs from pinned released schema')
    rows = [{'version': v, 'filename': name, 'sha256': digest} for v, name, digest in connection.execute(
        'SELECT version,filename,sha256 FROM schema_migrations ORDER BY version')]
    if len(rows) != version or _sha(_json(rows)) != MIGRATIONS[version]:
        raise Refused('migration filenames or hashes differ from released source')
    if (connection.execute('PRAGMA integrity_check').fetchall() != [('ok',)]
            or connection.execute('PRAGMA foreign_key_check').fetchone() is not None):
        raise Refused('database integrity or foreign-key proof failed')


def _quote(name):
    if not isinstance(name, str) or not NAME.fullmatch(name):
        raise Refused('unsupported SQL identifier')
    return '"' + name + '"'


def _typed(value):
    if value is None:
        return ['null']
    if type(value) is int:
        return ['integer', str(value)]
    if type(value) is float:
        return ['real', struct.pack('>d', value).hex()]
    if type(value) is str:
        return ['text', value]
    if type(value) is bytes:
        return ['blob', value.hex()]
    raise Refused('unsupported SQLite value type')


def _row_digest(row):
    return _sha(_json([_typed(value) for value in row]))


def _projections(connection, expected=None):
    names = [row[0] for row in connection.execute("SELECT name FROM sqlite_schema WHERE type='table' ORDER BY name")]
    if expected is not None:
        names = sorted(expected)
    result, total = {}, 0
    for name in names:
        actual_columns = [row[1] for row in connection.execute('PRAGMA table_info(' + _quote(name) + ')')]
        columns = expected[name]['columns'] if expected is not None else actual_columns
        if not columns or any(column not in actual_columns for column in columns):
            raise Refused('old columns missing')
        sql = 'SELECT rowid,' + ','.join(map(_quote, columns)) + ' FROM ' + _quote(name) + ' ORDER BY rowid'
        rows = []
        for row in connection.execute(sql):
            total += 1
            if total > MAX_ROWS or type(row[0]) is not int:
                raise Refused('row bound or identity unsupported')
            rows.append([row[0], _row_digest(row)])
        result[name] = {'columns': columns, 'count': len(rows), 'rows': rows}
    return result


def _retained(before, after):
    changes = {}
    for name, proof in before.items():
        current = dict(after[name]['rows'])
        if any(current.get(rowid) != digest for rowid, digest in proof['rows']):
            raise Refused('old row missing or changed: ' + name)
        changes[name] = {'before': proof['count'], 'after': after[name]['count'],
                         'added': after[name]['count'] - proof['count']}
    return changes


def _names(nonce):
    main = 'fixture-' + nonce + '.example.test'
    return {'main': main, 'child': 'app.' + main, 'alias': 'alias-' + nonce + '.example.test',
            'username': 'lab_' + nonce, 'email': 'lab_' + nonce + '@example.test'}


def _insert_fixture(connection, nonce):
    names = _names(nonce)
    hosts = (names['main'], 'www.' + names['main'], 'mail.' + names['main'],
             names['child'], 'mail.' + names['child'], names['alias'])
    for host in hosts:
        if connection.execute('SELECT 1 FROM hostname_reservations WHERE hostname=?', (host,)).fetchone():
            raise Refused('fixture hostname already owned')
    for host in (names['main'], names['child'], names['alias']):
        if connection.execute('SELECT 1 FROM pdns_domains WHERE lower(name)=?', (host,)).fetchone():
            raise Refused('fixture name conflicts with existing DNS data')
    if connection.execute('SELECT 1 FROM users WHERE username=? OR email=?', (names['username'], names['email'])).fetchone():
        raise Refused('fixture user already exists')
    ip = connection.execute('SELECT id FROM ip_addresses ORDER BY id LIMIT 1').fetchone()
    if ip is None:
        raise Refused('baseline has no usable IP relation')
    user = connection.execute('INSERT INTO users(username,password_hash,email,role,created_at,updated_at) VALUES(?,?,?,?,?,?)',
                              (names['username'], '!celikpanel-lab-no-login', names['email'], 'customer', STAMP, STAMP)).lastrowid
    subscription = connection.execute('INSERT INTO subscriptions(owner_id,name,max_domains,disk_quota_mb,created_at,updated_at) VALUES(?,?,?,?,?,?)',
                                      (user, 'SQL fixture ' + nonce, 7, 12345, STAMP, STAMP)).lastrowid
    main = connection.execute('INSERT INTO domains(subscription_id,name,status,ip_address_id,created_at,updated_at) VALUES(?,?,?,?,?,?)',
                              (subscription, names['main'], 'active', ip[0], STAMP, STAMP)).lastrowid
    child = connection.execute('INSERT INTO domains(subscription_id,name,parent_domain_id,status,ip_address_id,created_at,updated_at) VALUES(?,?,?,?,?,?,?)',
                               (subscription, names['child'], main, 'suspended', ip[0], STAMP, STAMP)).lastrowid
    alias = connection.execute('INSERT INTO domain_aliases(domain_id,alias,created_at) VALUES(?,?,?)',
                               (main, names['alias'], STAMP)).lastrowid
    # All six reservations are created by genuine schema18 triggers. Only their
    # new-row timestamps become deterministic; never touch prior reservations.
    connection.execute('UPDATE hostname_reservations SET created_at=? WHERE domain_id IN (?,?)', (STAMP, main, child))
    return {'user': user, 'subscription': subscription, 'main': main, 'child': child, 'alias': alias}


def _relations(connection, nonce, ids, version):
    names = _names(nonce)
    if (not isinstance(ids, dict) or set(ids) != {'user', 'subscription', 'main', 'child', 'alias'}
            or any(type(v) is not int or v <= 0 for v in ids.values())):
        raise Refused('fixture ID evidence malformed')
    if connection.execute('SELECT username,email,role,password_hash FROM users WHERE id=?', (ids['user'],)).fetchone() != (
            names['username'], names['email'], 'customer', '!celikpanel-lab-no-login'):
        raise Refused('fixture user relation differs')
    if connection.execute('SELECT owner_id FROM subscriptions WHERE id=?', (ids['subscription'],)).fetchone() != (ids['user'],):
        raise Refused('fixture subscription owner differs')
    for key, parent, status in (('main', None, 'active'), ('child', ids['main'], 'suspended')):
        if connection.execute('SELECT name,subscription_id,parent_domain_id,status,dns_zone_id FROM domains WHERE id=?', (ids[key],)).fetchone() != (
                names[key], ids['subscription'], parent, status, None):
            raise Refused('fixture domain relation differs')
    if connection.execute('SELECT domain_id,alias FROM domain_aliases WHERE id=?', (ids['alias'],)).fetchone() != (ids['main'], names['alias']):
        raise Refused('fixture alias differs')
    expected = sorted([(names['main'], ids['main'], 'primary', ids['main']),
                       ('www.' + names['main'], ids['main'], 'implicit_www', ids['main']),
                       ('mail.' + names['main'], ids['main'], 'implicit_mail', ids['main']),
                       (names['child'], ids['child'], 'primary', ids['child']),
                       ('mail.' + names['child'], ids['child'], 'implicit_mail', ids['child']),
                       (names['alias'], ids['main'], 'alias', ids['alias'])])
    actual = connection.execute('SELECT hostname,domain_id,source_kind,source_id FROM hostname_reservations WHERE domain_id IN (?,?) ORDER BY hostname',
                                (ids['main'], ids['child'])).fetchall()
    if actual != expected:
        raise Refused('fixture hostname reservations differ')


def build_private_copy(source: Path, output_parent: Path, admission: dict) -> dict:
    """Produce a new private copy; never seed an existing or canonical DB.

    admission keys: schema, purpose, baseline_commit,
    baseline_migrations_sha256, source_sha256, nonce (32 lowercase hex).
    Result paths and manifest are private fixture evidence, not publication data.
    """
    admission = _admission(admission)
    source, output_parent = Path(source), Path(output_parent)
    raw, identity = _read_private(source)
    if _sha(raw) != admission['source_sha256']:
        raise Refused('source bytes differ from explicit admission')
    output_parents = _private_directory(output_parent)
    output = Path(tempfile.mkdtemp(prefix='populated-' + admission['nonce'] + '-', dir=output_parent))
    if _private_directory(output_parent) != output_parents:
        raise Refused('output admission parent changed')
    _write_new(output / 'admission.json', _json(admission))
    database = output / 'celikpanel.db'
    _write_new(database, raw)
    connection = _connect(database)
    try:
        _schema(connection, 38)
        before = _projections(connection)
    finally:
        connection.close()
    if len(before) != 55:
        raise Refused('expected all55 historical tables')
    # writable SQLite is confined to the fresh copy, after full schema proof.
    connection = _connect(database, writable=True)
    try:
        if connection.execute('PRAGMA journal_mode=DELETE').fetchone()[0] != 'delete':
            raise Refused('private copy could not use standalone transaction')
        connection.execute('PRAGMA synchronous=FULL')
        connection.execute('BEGIN IMMEDIATE')
        ids = _insert_fixture(connection, admission['nonce'])
        _relations(connection, admission['nonce'], ids, 38)
        after = _projections(connection)
        # AUTOINCREMENT necessarily advances its own existing sequence rows.
        # Verify exact max(id) for only the four inserted AUTOINCREMENT tables;
        # every other preexisting row (including other sequence rows) is exact.
        sequence_before = before['sqlite_sequence']
        changed_ids = {r[0] for r in connection.execute(
            "SELECT rowid FROM sqlite_sequence WHERE name IN ('users','subscriptions','domains','domain_aliases')")}
        filtered = dict(before)
        filtered['sqlite_sequence'] = dict(sequence_before, rows=[r for r in sequence_before['rows'] if r[0] not in changed_ids])
        _retained(filtered, after)
        for table in ('users', 'subscriptions', 'domains', 'domain_aliases'):
            if connection.execute('SELECT seq FROM sqlite_sequence WHERE name=?', (table,)).fetchone() != connection.execute(
                    'SELECT max(id) FROM ' + _quote(table)).fetchone():
                raise Refused('fixture sequence counter differs')
        _schema(connection, 38)
        connection.execute('COMMIT')
    except BaseException:
        if connection.in_transaction:
            connection.execute('ROLLBACK')
        raise
    finally:
        connection.close()
    result_bytes, _ = _read_private(database)
    reread, current_identity = _read_private(source)
    if current_identity != identity or reread != raw:
        raise Refused('source changed during fixture construction')
    manifest = {'schema': MANIFEST_SCHEMA, 'purpose': PURPOSE, 'admission': admission,
                'schema38_sha256': SCHEMAS[38], 'fixture_ids': ids, 'tables': after,
                'source_sha256': _sha(raw), 'populated_sha256': _sha(result_bytes),
                'table_count': 55, 'prior_data_rows_preserved': True,
                'sequence_adjustment': ['domain_aliases', 'domains', 'subscriptions', 'users']}
    encoded = _json(manifest)
    if len(encoded) > MAX_MANIFEST_BYTES:
        raise Refused('manifest exceeds bounded private evidence size')
    # The manifest is only published after independent read-only verification.
    verify_copy(database, manifest, manifest_sha256=_sha(encoded), expected_version=38)
    _write_new(output / 'manifest.json', encoded)
    return {'directory': str(output), 'database': str(database), 'manifest': manifest, 'manifest_sha256': _sha(encoded)}


def verify_copy(database: Path, manifest: dict, *, manifest_sha256: str, expected_version: int) -> dict:
    """Read-only all-old-row/column verification of a standalone private copy.

    Caller must pin manifest_sha256 outside the manifest (its sealed lab plan).
    Original frozen/live WAL files are not accepted or opened by this helper.
    """
    fields = {'schema', 'purpose', 'admission', 'schema38_sha256', 'fixture_ids', 'tables', 'source_sha256',
              'populated_sha256', 'table_count', 'prior_data_rows_preserved', 'sequence_adjustment'}
    if not isinstance(manifest, dict) or set(manifest) != fields:
        raise Refused('manifest shape differs')
    encoded = _json(manifest)
    if (len(encoded) > MAX_MANIFEST_BYTES or not isinstance(manifest_sha256, str)
            or not HEX.fullmatch(manifest_sha256) or _sha(encoded) != manifest_sha256
            or manifest['schema'] != MANIFEST_SCHEMA or manifest['purpose'] != PURPOSE
            or manifest['schema38_sha256'] != SCHEMAS[38] or manifest['table_count'] != 55
            or manifest['prior_data_rows_preserved'] is not True
            or manifest['sequence_adjustment'] != ['domain_aliases', 'domains', 'subscriptions', 'users']):
        raise Refused('sealed manifest differs')
    admission = _admission(manifest['admission'])
    if (manifest['source_sha256'] != admission['source_sha256']
            or not isinstance(manifest['populated_sha256'], str) or not HEX.fullmatch(manifest['populated_sha256'])):
        raise Refused('manifest source binding differs')
    tables = manifest['tables']
    if not isinstance(tables, dict) or len(tables) != 55:
        raise Refused('all55 table evidence required')
    total = 0
    for table, value in tables.items():
        _quote(table)
        if (not isinstance(value, dict) or set(value) != {'columns', 'count', 'rows'}
                or not isinstance(value['columns'], list) or not value['columns']
                or any(not isinstance(column, str) for column in value['columns'])
                or len(set(value['columns'])) != len(value['columns'])
                or not isinstance(value['rows'], list) or type(value['count']) is not int
                or value['count'] != len(value['rows'])):
            raise Refused('table evidence malformed')
        for column in value['columns']:
            _quote(column)
        previous = None
        for row in value['rows']:
            total += 1
            if (total > MAX_ROWS or not isinstance(row, list) or len(row) != 2
                    or type(row[0]) is not int or (previous is not None and row[0] <= previous)
                    or not isinstance(row[1], str) or not HEX.fullmatch(row[1])):
                raise Refused('row evidence malformed')
            previous = row[0]
    database = Path(database)
    raw, identity = _read_private(database)
    connection = _connect(database)
    try:
        connection.execute('BEGIN')
        _schema(connection, expected_version)
        actual_tables = {row[0] for row in connection.execute("SELECT name FROM sqlite_schema WHERE type='table'")}
        old_names = actual_tables - NEW_TABLES_42 if expected_version == 42 else actual_tables
        if set(tables) != old_names:
            raise Refused('exact historical table set required')
        for table, proof in tables.items():
            columns = [row[1] for row in connection.execute('PRAGMA table_info(' + _quote(table) + ')')]
            if expected_version == 42 and table == 'domains':
                columns = [name for name in columns if name not in ('dns_management', 'dns_remote_connection_id')]
            if proof['columns'] != columns:
                raise Refused('every old column must be included in order')
        _relations(connection, admission['nonce'], manifest['fixture_ids'], expected_version)
        projected = _projections(connection, tables)
        changes = _retained(tables, projected)
        if expected_version == 42:
            for rowid, _ in tables['domains']['rows']:
                if connection.execute('SELECT dns_management,dns_remote_connection_id FROM domains WHERE rowid=?', (rowid,)).fetchone() != ('local', ''):
                    raise Refused('old domain migration40/42 defaults differ')
        connection.execute('ROLLBACK')
    finally:
        connection.close()
    again, current_identity = _read_private(database)
    if current_identity != identity or again != raw:
        raise Refused('verification source changed')
    return {'status': 'verified', 'scope': PURPOSE, 'schema_version': expected_version,
            'table_count': len(actual_tables), 'old_table_count': 55, 'old_row_count': total,
            'old_rows_missing_or_changed': 0, 'tables': changes, 'excluded_tables': [],
            'fixture_domain_count': 2, 'fixture_alias_count': 1, 'fixture_hostname_reservations': 6,
            'domain_defaults_40_42_verified': expected_version == 42,
            'database_sha256': _sha(raw), 'manifest_sha256': manifest_sha256,
            'global_bytes_equal_to_populated': _sha(raw) == manifest['populated_sha256']}
