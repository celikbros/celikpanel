#!/usr/bin/env python3
"""Independent schema38→42 comparison of two private standalone DB copies.

No live paths, signal, update or SQLite access to captured originals. The caller
copies both frozen files into new private directories before invoking this API.
Unlike baseline subset retention, this boundary allows only the four new ledger
rows and the exact rows/defaults created by migrations039–042.
"""
from __future__ import annotations
import importlib.util
from pathlib import Path
import re
from datetime import datetime

_spec = importlib.util.spec_from_file_location('exchange_rows_population', Path(__file__).with_name('populated_database.py'))
pop = importlib.util.module_from_spec(_spec)
_spec.loader.exec_module(pop)


def _defaults(connection, rowids):
    for rowid in rowids:
        if connection.execute('SELECT dns_management,dns_remote_connection_id FROM domains WHERE rowid=?', (rowid,)).fetchone() != ('local', ''):
            raise pop.Refused('migration-only domain defaults differ')


def _new_rows(connection):
    expected = (1, 1, 1, 'legacy', 'legacy', 0, '{}', '')
    rows = connection.execute('SELECT rowid,id,version,origin,status,revision,draft_json,completed_at,updated_at FROM server_setup_state').fetchall()
    if len(rows) != 1 or rows[0][:-1] != expected:
        raise pop.Refused('migration-only setup singleton differs')
    stamp = rows[0][-1]
    if not isinstance(stamp, str) or not re.fullmatch(r'\d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2}', stamp):
        raise pop.Refused('migration-only setup timestamp malformed')
    try:
        datetime.strptime(stamp, '%Y-%m-%d %H:%M:%S')
    except ValueError as error:
        raise pop.Refused('migration-only setup timestamp malformed') from error
    for name in sorted(pop.NEW_TABLES_42 - {'server_setup_state'}):
        if connection.execute('SELECT 1 FROM ' + pop._quote(name) + ' LIMIT 1').fetchone() is not None:
            raise pop.Refused('migration-only new table contains unexpected rows: ' + name)
    return {'server_setup_state': 1, **{n: 0 for n in sorted(pop.NEW_TABLES_42 - {'server_setup_state'})}}


def verify_pair(before: Path, after: Path, manifest: dict, *, manifest_sha256: str) -> dict:
    """Verify all pre-exchange rows, not merely rows present when fixture was seeded."""
    before, after = Path(before), Path(after)
    before_raw, before_id = pop._read_private(before)
    after_raw, after_id = pop._read_private(after)
    if before_id[:2] == after_id[:2]:
        raise pop.Refused('two distinct private copies required')
    baseline = pop.verify_copy(before, manifest, manifest_sha256=manifest_sha256, expected_version=38)
    migrated = pop.verify_copy(after, manifest, manifest_sha256=manifest_sha256, expected_version=42)
    old, new = pop._connect(before), pop._connect(after)
    try:
        old.execute('BEGIN'); new.execute('BEGIN')
        prior = pop._projections(old)
        current = pop._projections(new, prior)
        report = pop._retained(prior, current)
        for name, proof in prior.items():
            if name == 'schema_migrations':
                additions = set(dict(current[name]['rows'])) - set(dict(proof['rows']))
                if additions != {39, 40, 41, 42}:
                    raise pop.Refused('migration-only ledger additions differ')
            elif proof != current[name]:
                raise pop.Refused('migration-only historical table changed: ' + name)
        _defaults(new, [rowid for rowid, _ in prior['domains']['rows']])
        new_rows = _new_rows(new)
        old.execute('ROLLBACK'); new.execute('ROLLBACK')
    finally:
        old.close(); new.close()
    if pop._read_private(before) != (before_raw, before_id) or pop._read_private(after) != (after_raw, after_id):
        raise pop.Refused('pair verification source changed')
    return {'schema': 'celikpanel/private-database-exchange-row-proof/v1', 'status': 'verified',
            'before_schema': 38, 'after_schema': 42, 'old_table_count': 55, 'new_table_count': 10,
            'before_rows': sum(v['count'] for v in prior.values()),
            'old_rows_missing_or_changed': 0, 'historical_table_additions': {'schema_migrations': 4},
            'new_table_rows': new_rows, 'domain_defaults_verified': len(prior['domains']['rows']),
            'tables': report, 'before_sha256': pop._sha(before_raw), 'after_sha256': pop._sha(after_raw),
            'baseline_proof': baseline, 'migrated_proof': migrated, 'input_bytes_and_metadata_unchanged': True,
            'limits': ['private copies only; native exchange and rollback causality verified separately',
                       'setup updated_at is a validated dynamic timestamp, not a pinned clock value',
                       'all old rowids and typed columns included; no table exclusions']}
