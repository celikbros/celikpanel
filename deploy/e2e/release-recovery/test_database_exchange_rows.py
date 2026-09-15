#!/usr/bin/env python3
"""Private-copy semantic oracle regressions, not native cut acceptance."""
import importlib.util
import sqlite3
import sys
import unittest
from pathlib import Path

HERE = Path(__file__).resolve().parent

def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, HERE / filename)
    value = importlib.util.module_from_spec(spec); spec.loader.exec_module(value); return value

base = module('exchange_row_test_baseline', 'test_populated_database.py')
r = module('exchange_row_test_oracle', 'database_exchange_rows.py')

@unittest.skipUnless(sys.platform == 'linux', 'private Linux copies required')
class ExchangeRowsTests(unittest.TestCase):
    setUpClass = classmethod(base.PopulatedDatabaseTests.setUpClass.__func__)
    tearDownClass = classmethod(base.PopulatedDatabaseTests.tearDownClass.__func__)
    setUp = base.PopulatedDatabaseTests.setUp
    build = base.PopulatedDatabaseTests.build

    def pair(self):
        result = self.build()
        before = Path(result['database'])
        # These additions happen after the seed receipt, so comparing only seed
        # rows would miss their disappearance during real migration.
        connection = sqlite3.connect(before)
        connection.execute("INSERT INTO metrics_samples VALUES('2000-01-01T00:00:30Z',2.25,124,999,457,888,0.85)")
        connection.execute("UPDATE metrics_samples SET cpu=? WHERE rowid=2", (sqlite3.Binary(b'\x00typed\xff'),))
        connection.commit(); connection.close()
        after = before.with_name('migrated.db')
        after.write_bytes(before.read_bytes()); after.chmod(0o600)
        connection = sqlite3.connect(after)
        base.apply(connection, self.sql[38:42]); connection.close()
        return result, before, after

    def check(self, item):
        result, before, after = item
        return r.verify_pair(before, after, result['manifest'], manifest_sha256=result['manifest_sha256'])

    def mutate(self, item, sql):
        c = sqlite3.connect(item[2]); c.executescript(sql); c.commit(); c.close()

    def test_exact_real_migrations_all_rows_and_blob_values(self):
        item = self.pair(); before = [p.read_bytes() for p in item[1:]]
        proof = self.check(item)
        self.assertEqual(proof['historical_table_additions'], {'schema_migrations': 4})
        self.assertEqual(proof['tables']['metrics_samples']['before'], 2)
        self.assertEqual(proof['new_table_rows']['server_setup_state'], 1)
        self.assertEqual(sum(proof['new_table_rows'].values()), 1)
        self.assertEqual(proof['domain_defaults_verified'], 3)
        self.assertEqual([p.read_bytes() for p in item[1:]], before)

    def test_row_added_since_seed_cannot_disappear(self):
        item = self.pair(); self.mutate(item, 'DELETE FROM metrics_samples WHERE rowid=2')
        with self.assertRaisesRegex(r.pop.Refused, 'old row missing or changed: metrics_samples'): self.check(item)

    def test_rowid_relocation_refused(self):
        item = self.pair(); self.mutate(item, 'UPDATE metrics_samples SET rowid=999 WHERE rowid=2')
        with self.assertRaisesRegex(r.pop.Refused, 'old row missing or changed: metrics_samples'): self.check(item)

    def test_blob_to_text_same_display_refused(self):
        item = self.pair(); self.mutate(item, "UPDATE metrics_samples SET cpu=CAST(cpu AS TEXT) WHERE rowid=2")
        with self.assertRaises((r.pop.Refused, sqlite3.OperationalError)): self.check(item)

    def test_unexpected_old_table_addition_refused(self):
        item = self.pair(); self.mutate(item, "INSERT INTO metrics_samples VALUES('2000-01-01T00:01:00Z',2,1,2,3,4,5)")
        with self.assertRaisesRegex(r.pop.Refused, 'historical table changed: metrics_samples'): self.check(item)

    def test_new_setup_row_missing_or_modified_refused(self):
        for sql in ('DELETE FROM server_setup_state',
                    "UPDATE server_setup_state SET origin='fresh',status='new'",
                    "UPDATE server_setup_state SET status='ready'",
                    "UPDATE server_setup_state SET updated_at='2026-02-31 01:02:03'"):
            with self.subTest(sql=sql):
                item=self.pair();self.mutate(item,sql)
                with self.assertRaisesRegex(r.pop.Refused, 'migration-only setup'):self.check(item)

    def test_unexpected_new_table_row_refused(self):
        item=self.pair();self.mutate(item,"INSERT INTO remote_dns_enrollments VALUES('unrequested',999,'')")
        with self.assertRaisesRegex(r.pop.Refused,'new table contains unexpected rows'):self.check(item)

    def test_wrong_default_with_original_schema_restored_refused(self):
        item=self.pair();c=sqlite3.connect(item[2])
        original=c.execute("SELECT sql FROM sqlite_schema WHERE name='domain_dns_management_immutable'").fetchone()[0]
        c.execute('DROP TRIGGER domain_dns_management_immutable')
        c.execute("UPDATE domains SET dns_management='external' WHERE id=85")
        c.execute(original);c.commit()
        r.pop._schema(c,42);c.close()
        with self.assertRaisesRegex(r.pop.Refused,'defaults differ'):self.check(item)

    def test_same_file_and_sidecar_inputs_refused(self):
        item=self.pair();result,before,after=item
        with self.assertRaisesRegex(r.pop.Refused,'distinct private copies'):
            r.verify_pair(before,before,result['manifest'],manifest_sha256=result['manifest_sha256'])
        Path(str(after)+'-wal').write_bytes(b'preserved evidence')
        with self.assertRaisesRegex(r.pop.Refused,'sidecar evidence'):self.check(item)

if __name__ == '__main__':unittest.main()
