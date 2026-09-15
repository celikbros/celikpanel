#!/usr/bin/env python3
"""Offline native SQLite and closed-profile contracts; no VM or installation."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sqlite3
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent

def module(name, file):
    spec = importlib.util.spec_from_file_location(name, HERE / file)
    value = importlib.util.module_from_spec(spec);sys.modules[name] = value;spec.loader.exec_module(value)
    return value

profiles = module('tested_baseline_profiles', 'baseline_profiles.py')
probe = module('baseline_test_probe', 'guest_probe.py')
trial = module('baseline_test_trial', 'update_trial.py')

class ProfileTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.profile = profiles.get_profile(profiles.ALPHA64)
        repository = HERE.parents[2]
        paths = subprocess.run(['git', '-C', str(repository), 'ls-tree', '-r', '--name-only', cls.profile.commit,
                                '--', 'internal/db/migrations'], check=True, capture_output=True, text=True).stdout.splitlines()
        cls.rows = []
        for path in paths:
            raw = subprocess.run(['git', '-C', str(repository), 'show', cls.profile.commit + ':' + path],
                                 check=True, capture_output=True).stdout
            name = path.rsplit('/', 1)[1]
            cls.rows.append({'version': int(name.split('_', 1)[0]), 'filename': name, 'sha256': hashlib.sha256(raw).hexdigest()})

    def database_result(self):
        return {'status': 'ok', 'read_mode': 'sqlite-read-only-transaction', 'schema_version': 38,
                'migrations': copy.deepcopy(self.rows), 'migration_identities_sha256': profiles.identities_digest(self.rows)}

    def result(self):
        hashes = {'agent': self.profile.agent_sha256, 'panel': self.profile.panel_sha256}
        return {'schema': 'celikpanel/release-baseline-install-result/v1', 'version': self.profile.version,
                'baseline_profile': profiles.ALPHA64, 'bootstrap_sha256': self.profile.bootstrap_sha256,
                'source_commit': self.profile.commit, 'expected_release_pin': profiles.public_pin(profiles.ALPHA64),
                'exit_code': 0, 'error_type': None, 'https_curl_exit': 0, 'https_http_code': '200',
                'installed_artifacts': hashes, 'running_artifacts': hashes.copy(),
                'services': {name: {'ActiveState': 'active'} for name in hashes}, 'database': self.database_result()}

    def test_real_released_migration_set_is_38_and_matches_fixed_digest(self):
        self.assertEqual([row['version'] for row in self.rows], list(range(1, 39)))
        self.assertEqual(profiles.identities_digest(self.rows), self.profile.migration_identities_sha256)

    def test_default_profile_remains_75_and_has_no_arbitrary_source(self):
        self.assertEqual(profiles.get_profile().version, 'v0.1.0-alpha.75')
        for name in (None, '', 'v0.1.0-alpha.64', 'alpha41', 'https://example.invalid', '../alpha64', {}, 64):
            with self.subTest(name=name), self.assertRaises(ValueError):profiles.get_profile(name)

    def test_frozen_profile_cannot_be_retargeted(self):
        with self.assertRaises(AttributeError):self.profile.commit = '0' * 40

    def test_explicit_profile_required_by_baseline_validator(self):
        value = self.result()
        with self.assertRaises(ValueError):trial.validate_baseline(value)
        trial.validate_baseline(value, profiles.ALPHA64)

    def test_missing_profile_cannot_be_inferred_from_version(self):
        value = self.result();del value['baseline_profile']
        with self.assertRaises(ValueError):trial.validate_baseline(value, profiles.ALPHA64)

    def test_changed_artifacts_source_bootstrap_and_pin_are_refused(self):
        for field in ('source_commit', 'bootstrap_sha256', 'expected_release_pin', 'installed_artifacts', 'running_artifacts'):
            value = self.result();value[field] = {} if isinstance(value[field], dict) else '0' * 64
            with self.subTest(field=field), self.assertRaises(ValueError):trial.validate_baseline(value, profiles.ALPHA64)

    def test_unavailable_partial_or_changed_migration_evidence_is_not_baseline(self):
        for change in ('unknown', 'gap', 'newer', 'identity', 'digest', 'filename', 'null'):
            value = self.result();db = value['database']
            if change == 'unknown':db['status'] = 'unknown'
            elif change == 'gap':db['migrations'].pop(12)
            elif change == 'newer':db['schema_version'] = 42
            elif change == 'identity':db['migrations'][0]['sha256'] = 'a' * 64
            elif change == 'digest':db['migration_identities_sha256'] = 'b' * 64
            elif change == 'filename':db['migrations'][0]['filename'] = 'secret unexpected value'
            else:value['database'] = None
            with self.subTest(change=change), self.assertRaises(ValueError):trial.validate_baseline(value, profiles.ALPHA64)

    def test_unknown_or_stopped_services_are_rejected(self):
        value = self.result();value['services']['panel']['ActiveState'] = 'inactive'
        with self.assertRaises(ValueError):trial.validate_baseline(value, profiles.ALPHA64)

@unittest.skipUnless(os.name == 'posix', 'native protected SQLite/WAL boundary')
class ReadOnlyDatabaseTests(unittest.TestCase):
    setUpClass = classmethod(ProfileTests.setUpClass.__func__)
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory();self.addCleanup(self.directory.cleanup)
        self.path = Path(self.directory.name) / 'canonical.db'
        self.writer = sqlite3.connect(self.path, isolation_level=None)
        self.addCleanup(self.writer.close)
        self.writer.execute('PRAGMA journal_mode=WAL')
        self.writer.execute('CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,filename TEXT,sha256 TEXT)')
        self.writer.executemany('INSERT INTO schema_migrations VALUES(?,?,?)', [(v['version'], v['filename'], v['sha256']) for v in self.rows])
        self.writer.execute('CREATE TABLE secrets(value TEXT)')
        self.writer.execute("INSERT INTO secrets VALUES('never-output-this-secret')")
        for path in (self.path, Path(str(self.path)+'-wal'), Path(str(self.path)+'-shm')):path.chmod(0o600)

    def observe(self):return profiles.observe_migration_identity(self.path, profiles.ALPHA64, probe)

    def test_actual_read_only_wal_includes_38_identities_without_row_secrets(self):
        value = self.observe()
        self.assertEqual(value['status'], 'ok');self.assertTrue(value['wal_included'])
        self.assertEqual(value['migrations'], self.rows)
        self.assertEqual(value['table_count'], 2)
        self.assertNotIn('never-output-this-secret', json.dumps(value))
        self.assertEqual(value['excluded_tables'], [])

    def test_committing_writer_during_observation_does_not_mix_ledger_views(self):
        original = probe._database_semantics
        def commit_after_fixed_snapshot(connection, deadline):
            observed = original(connection, deadline)
            self.writer.execute("INSERT INTO schema_migrations VALUES(39,'039_new.sql',?)", ('a'*64,))
            return observed
        with mock.patch.object(probe, '_database_semantics', side_effect=commit_after_fixed_snapshot):
            value = self.observe()
        self.assertEqual(value['status'], 'ok');self.assertEqual(len(value['migrations']), 38)
        self.assertEqual(self.writer.execute('SELECT max(version) FROM schema_migrations').fetchone()[0], 39)
        self.assertEqual(self.observe()['status'], 'unknown')

    def test_bad_identity_and_sql_errors_never_export_raw_values(self):
        self.writer.execute("UPDATE schema_migrations SET filename='do-not-output-sensitive-value' WHERE version=1")
        value = self.observe();self.assertEqual(value['status'], 'unknown')
        self.assertNotIn('do-not-output', json.dumps(value))
        self.writer.execute('DROP TABLE schema_migrations')
        self.assertEqual(self.observe()['status'], 'unknown')

    def test_missing_wal_sidecar_is_unknown_without_creation(self):
        shm = Path(str(self.path)+'-shm');shm.rename(shm.with_name('retained-shm'))
        value = self.observe();self.assertEqual(value['status'], 'unknown');self.assertFalse(shm.exists())

    def test_symlink_fifo_and_unsafe_metadata_are_unknown(self):
        link = self.path.with_name('alias');link.symlink_to(self.path)
        self.assertEqual(profiles.observe_migration_identity(link, profiles.ALPHA64, probe)['status'], 'unknown')
        fifo = self.path.with_name('fifo');os.mkfifo(fifo, 0o600)
        self.assertEqual(profiles.observe_migration_identity(fifo, profiles.ALPHA64, probe)['status'], 'unknown')
        self.path.chmod(0o666);self.assertEqual(self.observe()['status'], 'unknown')

    def test_path_replacement_during_read_is_unknown(self):
        original = probe._database_semantics
        def replace_after_read(connection, deadline):
            result = original(connection, deadline)
            self.path.rename(self.path.with_name('retained.db'));self.path.write_bytes(b'not a database')
            return result
        with mock.patch.object(probe, '_database_semantics', side_effect=replace_after_read):
            self.assertEqual(self.observe()['status'], 'unknown')



class CandidateMigrationTests(unittest.TestCase):
    def setUp(self):
        self.candidate={'commit':'a'*40,'tree':'b'*40}
        rows=[{'version':n,'filename':f'{n:03d}_fixture.sql','sha256':'c'*64} for n in range(1,43)]
        self.value={'schema':'celikpanel/lab-candidate-migration-identities/v1','source_commit':'a'*40,'source_tree':'b'*40,'migrations':rows,'sha256':profiles.identities_digest(rows)}
    def test_exact_source_map_accepted_and_source_identity_change_refused(self):
        self.assertEqual(profiles.validate_candidate_migrations(self.value,self.candidate),self.value)
        for key in ('source_commit','source_tree','schema','sha256'):
            value=copy.deepcopy(self.value);value[key]='d'*40
            with self.subTest(key=key),self.assertRaises(ValueError):profiles.validate_candidate_migrations(value,self.candidate)
    def test_sealed_row_mutation_and_noncanonical_or_incomplete_versions_refused(self):
        for key,changed in (('version',2),('version',True),('filename','../private'),('filename','002_wrong.sql'),('sha256','d'*64)):
            value=copy.deepcopy(self.value);value['migrations'][0][key]=changed
            with self.subTest(key=key,changed=changed),self.assertRaises(ValueError):profiles.validate_candidate_migrations(value,self.candidate)
        for rows in (self.value['migrations'][:38],self.value['migrations'][1:],self.value['migrations']*4):
            value={**self.value,'migrations':rows,'sha256':profiles.identities_digest(rows)}
            with self.assertRaises(ValueError):profiles.validate_candidate_migrations(value,self.candidate)
    def test_recomputed_digest_does_not_make_wrong_migration_order_or_filename_valid(self):
        value=copy.deepcopy(self.value);value['migrations'][0]['filename']='002_fixture.sql';value['sha256']=profiles.identities_digest(value['migrations'])
        with self.assertRaises(ValueError):profiles.validate_candidate_migrations(value,self.candidate)


if __name__ == '__main__':unittest.main()
