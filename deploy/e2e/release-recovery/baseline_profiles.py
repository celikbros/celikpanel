#!/usr/bin/env python3
"""Closed historical baselines for disposable recovery fixtures, never deployment."""
from __future__ import annotations
from dataclasses import asdict, dataclass
import hashlib
import json
from pathlib import Path
import re
import sqlite3
import time

DEFAULT = 'alpha75'
ALPHA64 = 'alpha64-schema38'
CHOICES = (DEFAULT, ALPHA64)

@dataclass(frozen=True)
class Profile:
    name: str
    version: str
    sequence: int
    commit: str
    bootstrap_sha256: str
    unit: str
    bootstrap_name: str
    tree: str = ''
    archive_sha256: str = ''
    archive_size: int = 0
    agent_sha256: str = ''
    panel_sha256: str = ''
    schema_version: int = 0
    migration_identities_sha256: str = ''


def get_profile(name=DEFAULT):
    if name == DEFAULT:
        return Profile(DEFAULT, 'v0.1.0-alpha.75', 75,
                       '5aa03fd5b6775b21834ff7b1ce0695d92f50ae93',
                       '82b2674c103e347df471ec7e3f2f091d50006c957c54ac946b7c021ed19e041d',
                       'celikpanel-lab-alpha75-install.service', 'get-alpha75.sh')
    if name == ALPHA64:
        return Profile(ALPHA64, 'v0.1.0-alpha.64', 64,
                       '3ee8dac009c7e3db1d940f9b8be078e186693a80',
                       'e3033c0313ce5eab8ebac7e8a318de8fe2b694eb712a315c0b5a8dd435404150',
                       'celikpanel-lab-alpha64-install.service', 'get-alpha64.sh',
                       '5c0f569a97863cfbdc5794686e09369dbb883cd9',
                       'c669e52a6686865e9fc0515e2839f4cfea7501730877e48654a35def63557979', 23364971,
                       '396e2ce180925d064c61c4732f3d32cab2746abd2481e10e84194394de806340',
                       'c6e91e9e478d261c104397518fdae0145733832be7eeb3e53a123ef30c5d7460', 38,
                       'ef6821f3243832de5d19351a31e70db19c6df86f31679971ff77906c349db534')
    raise ValueError('unsupported fixed baseline profile')


def public_pin(name):
    return asdict(get_profile(name))


def identities_digest(rows):
    return hashlib.sha256(json.dumps(rows, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def validate_migration_identity(value, name):
    profile = get_profile(name)
    rows = value.get('migrations') if isinstance(value, dict) else None
    if (name != ALPHA64 or not isinstance(value, dict) or value.get('status') != 'ok'
            or value.get('read_mode') != 'sqlite-read-only-transaction'
            or value.get('schema_version') != profile.schema_version
            or not isinstance(rows, list) or len(rows) != profile.schema_version):
        raise ValueError('historical migration observation is incomplete')
    for version, row in enumerate(rows, 1):
        if (not isinstance(row, dict) or set(row) != {'version', 'filename', 'sha256'}
                or type(row['version']) is not int or row['version'] != version
                or not isinstance(row['filename'], str)
                or re.fullmatch(r'[0-9]{3}_[a-z0-9_]+\.sql', row['filename']) is None
                or int(row['filename'][:3]) != version
                or not isinstance(row['sha256'], str)
                or re.fullmatch(r'[0-9a-f]{64}', row['sha256']) is None):
            raise ValueError('historical migration identity is invalid')
    digest = identities_digest(rows)
    if digest != profile.migration_identities_sha256 or value.get('migration_identities_sha256') != digest:
        raise ValueError('historical migration identities differ from released source')
    return value


def validate_result(value, name=DEFAULT):
    profile = get_profile(name)
    declared = value.get('baseline_profile', DEFAULT)
    if declared != name or value.get('version') != profile.version:
        raise ValueError('baseline profile differs from explicit selection')
    if name == ALPHA64:
        if (value.get('bootstrap_sha256') != profile.bootstrap_sha256
                or value.get('source_commit') != profile.commit
                or value.get('expected_release_pin') != public_pin(name)
                or value.get('installed_artifacts') != {'agent': profile.agent_sha256, 'panel': profile.panel_sha256}
                or value.get('running_artifacts') != value.get('installed_artifacts')
                or any(value.get('services', {}).get(service, {}).get('ActiveState') != 'active' for service in ('agent', 'panel'))):
            raise ValueError('historical baseline does not match fixed released identity')
        validate_migration_identity(value.get('database', {}), name)


def observe_migration_identity(path: Path, name, probe):
    """Use the existing protected WAL reader in one transaction; never normalize."""
    profile = get_profile(name)
    if name != ALPHA64:
        raise ValueError('migration observation requires the explicit schema38 profile')
    try:
        identities = probe._database_files(path)
        before = {p: (p.stat().st_size, p.stat().st_mtime_ns, p.stat().st_ctime_ns)
                  for p, identity in identities.items() if identity is not None and p in
                  (path, Path(str(path) + '-wal'), Path(str(path) + '-shm'))}
        deadline = time.monotonic() + probe.DB_TIMEOUT
        connection = sqlite3.connect(path.as_uri() + '?mode=ro', uri=True, timeout=0, isolation_level=None)
        try:
            connection.setlimit(sqlite3.SQLITE_LIMIT_LENGTH, probe.DB_MAX_VALUE_BYTES)
            connection.set_progress_handler(lambda: int(time.monotonic() >= deadline), 1000)
            connection.execute('PRAGMA query_only=ON')
            connection.execute('PRAGMA trusted_schema=OFF')
            connection.execute('PRAGMA temp_store=MEMORY')
            connection.execute('BEGIN')
            semantic = probe._database_semantics(connection, deadline)
            rows = [{'version': row[0], 'filename': row[1], 'sha256': row[2]} for row in connection.execute(
                'SELECT version,filename,sha256 FROM main.schema_migrations ORDER BY version LIMIT 1001')]
            result = {'status': 'ok', 'read_mode': 'sqlite-read-only-transaction',
                      'schema_version': semantic['schema_version'], 'migrations': rows,
                      'migration_identities_sha256': identities_digest(rows),
                      'semantic_sha256': semantic['semantic']['sha256'],
                      'table_count': len(semantic['semantic']['tables']), 'excluded_tables': [],
                      'wal_included': identities[Path(str(path) + '-wal')] is not None}
            validate_migration_identity(result, name)
            probe._recheck_database_files(path, identities)
            connection.execute('ROLLBACK')
        finally:
            connection.close()
        probe._recheck_database_files(path, identities)
        result['source_metadata_changed'] = {
            'database' if p == path else p.name.rsplit('-', 1)[1]:
            (p.stat().st_size, p.stat().st_mtime_ns, p.stat().st_ctime_ns) != values
            for p, values in before.items()}
        return result
    except (probe.ProbeError, ValueError, OSError, sqlite3.Error, UnicodeError) as exc:
        return {'status': 'unknown', 'reason': 'historical database observation unavailable: ' + type(exc).__name__}


def validate_candidate_migrations(value,candidate):
    """Validate the host-sealed source inventory, never infer SQL from a package."""
    required={'schema','source_commit','source_tree','migrations','sha256'}
    if (not isinstance(value,dict) or set(value)!=required or value.get('schema')!='celikpanel/lab-candidate-migration-identities/v1'
            or value.get('source_commit')!=candidate.get('commit') or value.get('source_tree')!=candidate.get('tree')
            or not isinstance(value.get('source_commit'),str) or re.fullmatch(r'[0-9a-f]{40}',value['source_commit']) is None
            or not isinstance(value.get('source_tree'),str) or re.fullmatch(r'[0-9a-f]{40}',value['source_tree']) is None):
        raise ValueError('candidate migration source identity differs')
    rows=value['migrations']
    if not isinstance(rows,list) or not 39<=len(rows)<=128:
        raise ValueError('candidate migration inventory is incomplete or exceeds fixture bound')
    for version,row in enumerate(rows,1):
        if (not isinstance(row,dict) or set(row)!={'version','filename','sha256'} or type(row['version'])is not int
                or row['version']!=version or not isinstance(row['filename'],str)
                or re.fullmatch(r'[0-9]{3}_[a-z0-9_]+\.sql',row['filename']) is None or int(row['filename'][:3])!=version
                or not isinstance(row['sha256'],str) or re.fullmatch(r'[0-9a-f]{64}',row['sha256']) is None):
            raise ValueError('candidate migration entry is invalid')
    if identities_digest(rows)!=value['sha256']:
        raise ValueError('candidate migration sealed inventory changed')
    return value
