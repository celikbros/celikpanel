#!/usr/bin/env python3
"""Component tests only; these do not establish native migration interruption."""
from __future__ import annotations

import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import sqlite3
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('populated_database_under_test', HERE / 'populated_database.py')
p = importlib.util.module_from_spec(spec)
spec.loader.exec_module(p)
MIGRATION_DIR = HERE.parents[2] / 'internal' / 'db' / 'migrations'
LEDGER_SQL = '''
\tCREATE TABLE IF NOT EXISTS schema_migrations (
\t\tversion INTEGER PRIMARY KEY,
\t\tfilename TEXT,
\t\tsha256 TEXT,
\t\tapplied_at TEXT DEFAULT (datetime('now'))
\t)'''


def encoded(value):
    return json.dumps(value, sort_keys=True, ensure_ascii=True, separators=(',', ':')).encode()


def digest(value):
    return hashlib.sha256(value).hexdigest()


def migrations():
    # Clean archive and Windows checkout both work; exact LF source identity is
    # pinned against the real Alpha64 commit, not fabricated test migrations.
    result = []
    for path in sorted(MIGRATION_DIR.glob('*.sql')):
        raw = path.read_bytes().replace(b'\r\n', b'\n')
        result.append((int(path.name[:3]), path.name, digest(raw), raw.decode()))
    return result


def apply(connection, records):
    for version, filename, sha256, sql in records:
        connection.executescript('BEGIN;\n' + sql)
        connection.execute('INSERT INTO schema_migrations(version,filename,sha256,applied_at) VALUES(?,?,?,?)',
                           (version, filename, sha256, '2000-01-01 00:00:00'))
        connection.commit()


@unittest.skipUnless(sys.platform == 'linux', 'private Linux filesystem contract')
class PopulatedDatabaseTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.sql = migrations()
        old_identities = [{'version': v, 'filename': n, 'sha256': h} for v, n, h, _ in cls.sql[:38]]
        if len(old_identities) != 38 or digest(encoded(old_identities)) != p.MIGRATIONS[38]:
            raise AssertionError('test input must be the exact released Alpha64 migration bytes')
        cls.seed_root = tempfile.TemporaryDirectory(prefix='cp-populated-tests-')
        base = Path(cls.seed_root.name) / 'base.db'
        connection = sqlite3.connect(base)
        connection.execute('PRAGMA foreign_keys=ON')
        connection.execute(LEDGER_SQL)
        apply(connection, cls.sql[:38])
        # Prior nonempty owner rows make preservation non-vacuous. This remains
        # a SQL fixture; it does not pretend to create any native service.
        connection.execute("INSERT INTO users(id,username,password_hash,email,role,created_at) VALUES(81,'prior-owner','PRIVATE-ADMIN-VALUE','prior@example.test','customer','1999-01-01')")
        connection.execute("INSERT INTO subscriptions(id,owner_id,name) VALUES(83,81,'Existing subscription')")
        connection.execute("INSERT INTO domains(id,subscription_id,name,status) VALUES(85,83,'prior.example.test','active')")
        connection.execute("INSERT INTO domain_aliases(id,domain_id,alias) VALUES(87,85,'prior-alias.example.test')")
        connection.execute("INSERT INTO metrics_samples VALUES('2000-01-01T00:00:00Z',1.25,123,999,456,888,0.75)")
        connection.commit()
        p._schema(connection, 38)
        connection.close()
        cls.baseline_bytes = base.read_bytes()

    @classmethod
    def tearDownClass(cls):
        cls.seed_root.cleanup()

    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='cp-populated-case-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / 'original.db'
        self.source.write_bytes(self.baseline_bytes)
        self.source.chmod(0o600)
        self.output = self.root / 'outputs'
        self.output.mkdir(mode=0o700)
        self.admission = {'schema': p.ADMISSION_SCHEMA, 'purpose': p.PURPOSE,
                          'baseline_commit': p.BASELINE_COMMIT, 'baseline_migrations_sha256': p.MIGRATIONS[38],
                          'source_sha256': digest(self.baseline_bytes), 'nonce': 'a1' * 16}

    def build(self):
        return p.build_private_copy(self.source, self.output, self.admission)

    def verify(self, result, version=38, path=None):
        return p.verify_copy(Path(path or result['database']), result['manifest'],
                             manifest_sha256=result['manifest_sha256'], expected_version=version)

    def change_source(self, sql, parameters=()):
        connection = sqlite3.connect(self.source)
        connection.execute(sql, parameters)
        connection.commit()
        connection.close()
        self.admission['source_sha256'] = digest(self.source.read_bytes())

    def test_all_old_rows_relations_private_output_and_no_secret_values(self):
        before = self.source.stat()
        result = self.build()
        proof = self.verify(result)
        self.assertEqual(proof['old_table_count'], 55)
        self.assertEqual(proof['table_count'], 55)
        self.assertEqual(proof['fixture_domain_count'], 2)
        self.assertEqual(proof['fixture_hostname_reservations'], 6)
        self.assertEqual(proof['old_rows_missing_or_changed'], 0)
        self.assertEqual(proof['excluded_tables'], [])
        self.assertTrue(proof['global_bytes_equal_to_populated'])
        self.assertEqual(self.source.read_bytes(), self.baseline_bytes)
        self.assertEqual(p._identity(self.source.stat()), p._identity(before))
        directory = Path(result['directory'])
        self.assertEqual(directory.stat().st_mode & 0o777, 0o700)
        self.assertEqual({f.name for f in directory.iterdir()}, {'admission.json', 'celikpanel.db', 'manifest.json'})
        self.assertTrue(all((f.stat().st_mode & 0o777) == 0o600 for f in directory.iterdir()))
        manifest = (directory / 'manifest.json').read_bytes()
        self.assertNotIn(b'PRIVATE-ADMIN-VALUE', manifest)
        self.assertNotIn(b'prior@example.test', manifest)
        self.assertNotIn(b'Existing subscription', manifest)
        self.assertEqual(digest(manifest), result['manifest_sha256'])
        self.assertEqual(len(result['manifest']['tables']['domains']['rows']), 3)

    def test_actual_39_through_42_migrations_preserve_all_old_columns_and_defaults(self):
        result = self.build()
        connection = sqlite3.connect(result['database'])
        connection.execute('PRAGMA foreign_keys=ON')
        apply(connection, self.sql[38:42])
        connection.close()
        proof = self.verify(result, 42)
        self.assertEqual(proof['schema_version'], 42)
        self.assertEqual(proof['table_count'], 65)
        self.assertTrue(proof['domain_defaults_40_42_verified'])
        self.assertFalse(proof['global_bytes_equal_to_populated'])
        self.assertEqual(proof['tables']['schema_migrations']['added'], 4)
        self.assertEqual(proof['tables']['domains']['before'], 3)

    def test_old_row_modification_and_deletion_are_both_refused(self):
        for sql in ("UPDATE users SET password_hash='changed' WHERE id=81", 'DELETE FROM domain_aliases WHERE id=87'):
            with self.subTest(sql=sql):
                result = self.build()
                connection = sqlite3.connect(result['database'])
                connection.execute(sql)
                connection.commit()
                connection.close()
                with self.assertRaises(p.Refused):
                    self.verify(result)

    def test_existing_sequence_counter_is_not_excluded_from_verification(self):
        result = self.build()
        connection = sqlite3.connect(result['database'])
        connection.execute("INSERT INTO users(username,password_hash,email,role) VALUES('later','!','later@example.test','customer')")
        connection.commit()
        connection.close()
        # sqlite_sequence is existing old state too: unapproved advancement is
        # reported as a changed old row, never silently excluded from proof.
        with self.assertRaisesRegex(p.Refused, 'sqlite_sequence'):
            self.verify(result)

    def test_appended_metrics_reported_and_prior_typed_row_is_still_verified(self):
        result = self.build()
        connection = sqlite3.connect(result['database'])
        connection.execute("INSERT INTO metrics_samples VALUES('2000-01-01T00:00:30Z',2.25,124,999,457,888,0.85)")
        connection.commit()
        connection.close()
        proof = self.verify(result)
        self.assertEqual(proof['tables']['metrics_samples'], {'before': 1, 'after': 2, 'added': 1})
        self.assertFalse(proof['global_bytes_equal_to_populated'])
        connection = sqlite3.connect(result['database'])
        connection.execute('UPDATE metrics_samples SET cpu=9 WHERE rowid=1')
        connection.commit()
        connection.close()
        with self.assertRaisesRegex(p.Refused, 'metrics_samples'):
            self.verify(result)

    def test_actual_builder_sigkill_preserves_source_and_unfinished_output(self):
        child = os.fork()
        if child == 0:
            def kill_after_write(connection, nonce):
                connection.execute("INSERT INTO users(username,password_hash,email,role) VALUES('partial','!','partial@example.test','customer')")
                os.kill(os.getpid(), signal.SIGKILL)
            p._insert_fixture = kill_after_write
            try:
                self.build()
            except BaseException:
                os._exit(91)
            os._exit(92)
        waited, status = os.waitpid(child, 0)
        self.assertEqual(waited, child)
        self.assertTrue(os.WIFSIGNALED(status))
        self.assertEqual(os.WTERMSIG(status), signal.SIGKILL)
        directory, = self.output.iterdir()
        self.assertFalse((directory / 'manifest.json').exists())
        self.assertTrue((directory / 'admission.json').is_file())
        self.assertTrue((directory / 'celikpanel.db-journal').is_file())
        with self.assertRaisesRegex(p.Refused, 'sidecar'):
            p._read_private(directory / 'celikpanel.db')
        self.assertEqual(self.source.read_bytes(), self.baseline_bytes)

    def test_newly_sealed_malformed_manifest_or_omitted_columns_still_refused(self):
        result = self.build()
        for change in ('columns', 'duplicate-row', 'malformed-column', 'wrong-ids', 'bad-hash'):
            with self.subTest(change=change):
                modified = json.loads(encoded(result['manifest']))
                if change == 'columns':
                    modified['tables']['users']['columns'].remove('totp_secret')
                elif change == 'duplicate-row':
                    row = modified['tables']['users']['rows'][0]
                    modified['tables']['users']['rows'].append(row)
                    modified['tables']['users']['count'] += 1
                elif change == 'malformed-column':
                    modified['tables']['users']['columns'][0] = []
                elif change == 'wrong-ids':
                    modified['fixture_ids'] = None
                else:
                    modified['populated_sha256'] = None
                with self.assertRaises(p.Refused):
                    p.verify_copy(Path(result['database']), modified, manifest_sha256=digest(encoded(modified)), expected_version=38)

    def test_manifest_tamper_is_refused_before_reading_database(self):
        result = self.build()
        result['manifest']['tables']['users']['rows'] = []
        with self.assertRaisesRegex(p.Refused, 'sealed manifest'):
            self.verify(result)

    def test_type_preserving_row_hash_distinguishes_blob_text_null_float_integer(self):
        values = [b'1', '1', None, 1, 1.0]
        self.assertEqual(len({p._row_digest([1, v]) for v in values}), len(values))

    def test_wrong_source_digest_or_admission_refuses_without_creating_output(self):
        for key, value in (('source_sha256', '0' * 64), ('baseline_commit', 'b' * 40),
                           ('baseline_migrations_sha256', 'f' * 64), ('nonce', '../escape'),
                           ('purpose', 'install-on-live-server')):
            with self.subTest(key=key):
                admission = dict(self.admission, **{key: value})
                with self.assertRaises(p.Refused):
                    p.build_private_copy(self.source, self.output, admission)
                self.assertEqual(list(self.output.iterdir()), [])

    def test_exact38_ledger_hash_filename_and_coverage_are_required(self):
        for sql in ("UPDATE schema_migrations SET sha256='bad' WHERE version=38",
                    "UPDATE schema_migrations SET filename='038_fake.sql' WHERE version=38",
                    'DELETE FROM schema_migrations WHERE version=38'):
            with self.subTest(sql=sql):
                self.source.write_bytes(self.baseline_bytes)
                self.change_source(sql)
                before = self.source.read_bytes()
                with self.assertRaises(p.Refused):
                    self.build()
                self.assertEqual(self.source.read_bytes(), before)
                self.assertTrue(all(not (d / 'manifest.json').exists() for d in self.output.iterdir()))

    def test_ledger_spoof_with_modified_trigger_is_refused(self):
        self.change_source('DROP TRIGGER trg_domains_hostname_reserve_insert')
        with self.assertRaisesRegex(p.Refused, 'schema SQL'):
            self.build()

    def test_other_schema_and_unknown_objects_refused(self):
        for alteration in ('CREATE TABLE owner_extension(id INTEGER)', 'DROP INDEX idx_users_email'):
            with self.subTest(alteration=alteration):
                self.source.write_bytes(self.baseline_bytes)
                self.change_source(alteration)
                with self.assertRaisesRegex(p.Refused, 'schema SQL'):
                    self.build()

    def test_real_schema39_is_not_admitted_as_old38(self):
        connection = sqlite3.connect(self.source)
        apply(connection, self.sql[38:39])
        connection.close()
        self.admission['source_sha256'] = digest(self.source.read_bytes())
        with self.assertRaises(p.Refused):
            self.build()

    def test_existing_hostname_user_and_dns_conflicts_are_preserved(self):
        names = p._names(self.admission['nonce'])
        mutations = [
            ('INSERT INTO domain_aliases(domain_id,alias) VALUES(85,?)', (names['main'],)),
            ('INSERT INTO users(username,password_hash,email,role) VALUES(?,?,?,?)',
             (names['username'], '!', names['email'], 'customer')),
            ("INSERT INTO pdns_domains(name,type) VALUES(?,'NATIVE')", (names['main'],)),
        ]
        for sql, args in mutations:
            with self.subTest(sql=sql):
                self.source.write_bytes(self.baseline_bytes)
                self.change_source(sql, args)
                before = self.source.read_bytes()
                with self.assertRaises(p.Refused):
                    self.build()
                self.assertEqual(self.source.read_bytes(), before)
                self.assertTrue(all(not (d / 'manifest.json').exists() for d in self.output.iterdir()))

    def test_sql_failure_after_first_insert_rolls_back_private_copy_only(self):
        def fail_after_write(connection, nonce):
            connection.execute("INSERT INTO users(username,password_hash,email,role) VALUES('partial','!','partial@example.test','customer')")
            raise p.Refused('injected fixture-only failure')
        with mock.patch.object(p, '_insert_fixture', fail_after_write):
            with self.assertRaises(p.Refused):
                self.build()
        directory, = self.output.iterdir()
        self.assertFalse((directory / 'manifest.json').exists())
        connection = sqlite3.connect((directory / 'celikpanel.db').as_uri() + '?mode=ro&immutable=1', uri=True)
        self.assertEqual(connection.execute("SELECT count(*) FROM users WHERE username='partial'").fetchone(), (0,))
        connection.close()
        self.assertEqual(self.source.read_bytes(), self.baseline_bytes)

    def test_source_is_never_opened_with_sqlite(self):
        original = sqlite3.connect
        opened = []
        def watched(path, *args, **kwargs):
            opened.append(str(path))
            self.assertNotIn(str(self.source), str(path))
            self.assertIn(str(self.output), str(path))
            return original(path, *args, **kwargs)
        with mock.patch.object(p.sqlite3, 'connect', watched):
            self.build()
        self.assertGreaterEqual(len(opened), 3)

    def test_all_sidecars_even_symlinks_refuse_source_and_verifier(self):
        result = self.build()
        for source in (self.source, Path(result['database'])):
            for suffix in p.SIDECARS:
                with self.subTest(source=source.name, suffix=suffix):
                    sidecar = Path(str(source) + suffix)
                    sidecar.symlink_to('/nonexistent-private-evidence')
                    try:
                        with self.assertRaises(p.Refused):
                            self.build() if source == self.source else self.verify(result)
                        self.assertTrue(sidecar.is_symlink())
                    finally:
                        sidecar.unlink()

    def test_symlink_hardlink_and_permissive_source_metadata_refused(self):
        alias = self.root / 'alias.db'
        alias.symlink_to(self.source)
        with self.assertRaises((p.Refused, OSError)):
            p.build_private_copy(alias, self.output, self.admission)
        alias.unlink()
        os.link(self.source, alias)
        with self.assertRaises(p.Refused):
            self.build()
        alias.unlink()
        self.source.chmod(0o640)
        with self.assertRaises(p.Refused):
            self.build()
        self.assertEqual(list(self.output.iterdir()), [])

    def test_private_parent_and_explicit_product_output_refusal(self):
        self.output.chmod(0o755)
        with self.assertRaises(p.Refused):
            self.build()
        self.output.chmod(0o700)
        for forbidden in ('/var/lib/celikpanel', '/opt/celikpanel/test', '/run/celikpanel/test'):
            with self.subTest(forbidden=forbidden), self.assertRaisesRegex(p.Refused, 'product paths'):
                p.build_private_copy(self.source, Path(forbidden), self.admission)
        self.root.chmod(0o755)
        try:
            with self.assertRaises(p.Refused):
                self.build()
        finally:
            self.root.chmod(0o700)

    def test_verifier_is_read_only_and_retains_file_identity(self):
        result = self.build()
        database = Path(result['database'])
        before = p._identity(database.stat())
        raw = database.read_bytes()
        self.verify(result)
        self.assertEqual(p._identity(database.stat()), before)
        self.assertEqual(database.read_bytes(), raw)
        self.assertFalse(any(Path(str(database) + s).exists() for s in p.SIDECARS))

    def test_wrong_migrated_defaults_refuse_even_if_only_new_column_changed(self):
        result = self.build()
        connection = sqlite3.connect(result['database'])
        apply(connection, self.sql[38:42])
        # The real trigger itself refuses this owner-policy rewrite. A fake
        # direct ALTER/trigger deletion also fails the pinned schema proof.
        with self.assertRaises(sqlite3.IntegrityError):
            connection.execute("UPDATE domains SET dns_management='external' WHERE id=85")
        connection.rollback()
        connection.execute('DROP TRIGGER domain_dns_management_immutable')
        connection.execute("UPDATE domains SET dns_management='external' WHERE id=85")
        connection.commit()
        connection.close()
        with self.assertRaises(p.Refused):
            self.verify(result, 42)

    def test_same_bytes_source_inode_replacement_before_final_proof_refused(self):
        original = p._insert_fixture
        def replace(connection, nonce):
            result = original(connection, nonce)
            replacement = self.root / 'replacement.db'
            replacement.write_bytes(self.baseline_bytes)
            replacement.chmod(0o600)
            os.replace(replacement, self.source)
            return result
        with mock.patch.object(p, '_insert_fixture', replace):
            with self.assertRaisesRegex(p.Refused, 'source changed'):
                self.build()
        self.assertTrue(all(not (d / 'manifest.json').exists() for d in self.output.iterdir()))


if __name__ == '__main__':
    unittest.main()
