"""Private component fixtures, not native acceptance or permission to signal."""
import copy
import ctypes
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('tested_exchange_checkpoint', HERE / 'guest_exchange_checkpoint.py')
s = importlib.util.module_from_spec(spec); sys.modules[spec.name] = s; spec.loader.exec_module(s)
spec = importlib.util.spec_from_file_location('exchange_component_filesystem', HERE / 'test_guest_wal_checkpoint.py')
b = importlib.util.module_from_spec(spec); spec.loader.exec_module(b)
b.s = s.w


def encode(value):
    return (json.dumps(value, separators=(',', ':')) + '\n').encode()


@unittest.skipUnless(b.ROOT_ONLY, 'root-owned component filesystem; not native acceptance')
class ExchangeFilesystemTests(unittest.TestCase):
    write = b.FilesystemTests.write
    budget = b.FilesystemTests.budget
    fingerprint = b.FilesystemTests.fingerprint

    def setUp(self):
        b.FilesystemTests.setUp(self)
        self.material['database_before']['attribute_counts'] = {'attributes': 0, 'parent_attributes': 0}
        self.admitted = {key: self.admitted[key] for key in s.ADMISSION}
        self.write(self.authority / 'admission.json', encode(self.admitted))
        os.chown(self.work, 0, 0)
        self.seal = {'schema': 'celikpanel/database-migration-seal/v1',
                     'admission_sha256': s.w.sha(encode(self.admitted)),
                     'files': {p.name: s.w.product_file(s.w._read(p, self.budget(), {self.uid})) for p in sorted(self.work.iterdir())}}
        self.write(self.authority / 'seal.json', encode(self.seal))
        self.build = self.authority / ('.build-' + '4' * 32); self.build.mkdir(mode=0o700)
        self.write(self.build / s.DB, b'private-migrated-rows-not-exported', self.uid, self.gid)
        self.publication = {'schema': 'celikpanel/database-publication-intent/v1',
                            'admission_sha256': s.w.sha(encode(self.admitted)), 'seal_sha256': s.w.sha(encode(self.seal)),
                            'build': self.build.name, 'directory': s.w.directory_id(self.build.stat()),
                            'before': self.admitted['before'],
                            'after': s.w.product_file(s.w._read(self.build / s.DB, self.budget(), {self.uid}))}
        self.write(self.authority / 'publication.json', encode(self.publication))
        self.make_kit()
        b.FilesystemTests.fake_processes(self)
        publisher = self.proc / '1002'
        raw = (publisher / 'status').read_bytes()
        self.write(publisher / 'status', raw.replace(str(self.uid).encode(), b'0'))
        self.write(publisher / 'cmdline', (str(self.kit / 'bin/panel-checker') + '\0--publish-update-database=' + self.snapshot + '\0').encode())
        self.write(publisher / 'environ', b'\0'.join(k.encode() + b'=' + v.encode() for k, v in s.ENV.items()) + b'\0')
        (publisher / 'exe').unlink(); (publisher / 'exe').symlink_to(self.kit / 'bin/panel-checker')
        (publisher / 'fd/20').symlink_to(self.parent, target_is_directory=True)
        (publisher / 'fd/21').symlink_to(self.build, target_is_directory=True)
        self.trace = {'tracer_pid': os.getpid(), 'tasks': self.trace['tasks'], 'publisher': {'pid': 1002, 'tid': 1002},
                      'exchange': {'number': 316, 'old_dirfd': 20, 'new_dirfd': 21, 'old_name': s.DB, 'new_name': s.DB,
                                   'flags': 2, 'arch': 0xc000003e, 'stage': 'entry', 'entry_exit_matched': False,
                                   'result': None, 'is_error': False}}
        self.worker_result = dict(self.worker, executable={'sha256': self.worker['running_executable_sha256']})
        self.locks = [{'pid': 1002, 'fd': 9, 'kernel_lock_type': 'FLOCK-ADVISORY-WRITE'}]
        # Only external admission/lock seams are fake. File bytes, rename, dirs,
        # process records and complete ptrace inventory checks are real reads.
        for name, value in [('worker_proof', self.worker_result), ('exclusive_lock', self.locks)]:
            patch = mock.patch.object(s.w, name, return_value=value); patch.start(); self.addCleanup(patch.stop)

    def make_kit(self):
        selected = self.root / 'selected'; runtimes = self.root / 'runtimes'; runtimes.mkdir(mode=0o700)
        content = {n: ('fixture-kit-' + n).encode() for n in s.KIT_MODES if n != 'runtime.manifest'}
        hashes = {n: s.w.sha(raw) for n, raw in content.items()}
        raw = ('format=celikpanel-recovery-runtime-v1\nprotocol=1\nsnapshot=6\n' + ''.join(hashes[n] + '  ' + n + '\n' for n in sorted(hashes))).encode()
        self.kit = runtimes / s.w.sha(raw); self.kit.mkdir(mode=0o700)
        content['runtime.manifest'] = raw
        for n, raw in content.items():
            path = self.kit / n
            path.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            self.write(path, raw, mode=s.KIT_MODES[n])
            self.plan['candidate']['files']['recovery-runtime/' + n] = s.w.sha(raw)
        for directory in self.kit.rglob('*'):
            if directory.is_dir(): directory.chmod(0o700)
        self.write(selected, ('format=celikpanel-recovery-selection-v1\nruntime=' + self.kit.name + '\n').encode())
        launcher = self.root / 'launcher'; self.write(launcher, content['bin/recovery'], mode=0o755)
        for name, path in [('SELECTION', selected), ('RUNTIMES', runtimes), ('LAUNCHER', launcher)]:
            patch = mock.patch.object(s, name, path); patch.start(); self.addCleanup(patch.stop)

    def state(self):
        return s.database_state(self.plan, s.w.transaction(self.budget()), self.budget(), self.uid, self.gid)

    def entry(self):
        return s.inspect_entry(self.args, self.plan, self.worker, self.trace)

    def exchange(self):
        native = ctypes.CDLL(None, use_errno=True)
        native.renameat2.argtypes = [ctypes.c_int, ctypes.c_char_p, ctypes.c_int, ctypes.c_char_p, ctypes.c_uint]
        rc = native.renameat2(-100, os.fsencode(self.parent / s.DB), -100, os.fsencode(self.build / s.DB), 2)
        self.assertEqual(rc, 0, os.strerror(ctypes.get_errno()))
        self.trace['exchange'].update(stage='exit', entry_exit_matched=True, result=0)

    def exit(self, entry):
        return s.inspect(self.args, self.plan, self.worker, self.trace, entry)

    def test_complete_actual_exchange_and_read_only_double_proof(self):
        initial = self.fingerprint(); entry = self.entry()
        self.assertEqual(entry['status'], 'verified', entry)
        self.assertEqual(initial, self.fingerprint())
        self.exchange(); before = self.fingerprint(); result = self.exit(entry)
        self.assertEqual(result['status'], 'verified', result)
        self.assertEqual(before, self.fingerprint())
        self.assertEqual(result['entry_proof_sha256'], s.digest(entry))
        self.assertEqual(result['database']['canonical']['identity']['ino'], entry['database']['build']['identity']['ino'])
        self.assertEqual(result['database']['build']['identity']['ino'], entry['database']['canonical']['identity']['ino'])
        self.assertNotIn('private-migrated-rows-not-exported', json.dumps(result))
        self.assertNotIn('private-table-row-never-emitted', json.dumps(result))

    def test_actual_normalized_material_attribute_counts_are_required_zero(self):
        self.assertEqual(self.entry()['status'], 'verified')
        self.material['database_before']['attribute_counts']['attributes'] = 1
        self.assertEqual(self.entry()['reason'], 'material-before-or-initial-differs')
        self.material['database_before'].pop('attribute_counts')
        self.assertEqual(self.entry()['reason'], 'material-before-or-initial-differs')
    def test_expected_selected_checker_before_publication_and_missing_transaction(self):
        (self.authority / 'publication.json').unlink()
        expected = s.writer_expected(self.args, self.plan, 10)
        self.assertEqual(expected['checker_path'], str(self.kit / 'bin/panel-checker'))
        self.assertEqual(set(expected), {'operation_id', 'token_sha256', 'worker_start_ticks', 'snapshot', 'checker_path', 'checker_sha256'})
        (self.transactions / 'active').unlink()
        self.assertIsNone(s.writer_expected(self.args, self.plan, 10))

    def test_failed_or_unmatched_syscall_never_qualifies(self):
        entry = self.entry(); self.exchange()
        for field, value in [('result', -1), ('result', True), ('is_error', True), ('entry_exit_matched', False),
                             ('number', 1), ('flags', 1), ('old_dirfd', True), ('old_name', '../celikpanel.db'), ('arch', 0)]:
            trace = copy.deepcopy(self.trace); trace['exchange'][field] = value
            with self.subTest(field=field):
                self.assertEqual(s.inspect(self.args, self.plan, self.worker, trace, entry)['status'], 'inconclusive')

    def test_exit_without_actual_exchange_or_entry_proof_refused(self):
        entry = self.entry(); self.trace['exchange'].update(stage='exit', entry_exit_matched=True, result=0)
        self.assertEqual(self.exit(entry)['reason'], 'postexchange-pair-differs')
        self.assertEqual(self.exit(None)['status'], 'inconclusive')

    def test_every_receipt_and_partial_authority_refused_read_only(self):
        entry = self.entry(); self.exchange()
        for name in ('published.json', 'restoration.json', 'restored.json', '.record-' + '1' * 32, 'owner-note'):
            path = self.authority / name; self.write(path, b'{}\n'); before = self.fingerprint()
            with self.subTest(name=name):
                self.assertEqual(self.exit(entry)['status'], 'inconclusive')
                self.assertEqual(before, self.fingerprint())
            path.unlink()

    def test_same_content_replacement_after_exchange_is_not_recorded_inode(self):
        entry = self.entry(); self.exchange()
        path = self.parent / s.DB; raw = path.read_bytes(); path.rename(self.root / 'retained-owner')
        self.write(path, raw, self.uid, self.gid)
        self.assertEqual(self.exit(entry)['reason'], 'postexchange-pair-differs')

    def test_mtime_change_is_not_ctime_relaxation(self):
        entry = self.entry(); self.exchange(); path = self.parent / s.DB
        info = path.stat(); os.utime(path, ns=(info.st_atime_ns, info.st_mtime_ns + 1))
        self.assertEqual(self.exit(entry)['reason'], 'postexchange-pair-differs')

    def test_changed_records_and_bad_links_refused(self):
        entry = self.entry(); self.exchange()
        for name, obj, field in [('publication.json', self.publication, 'seal_sha256'), ('seal.json', self.seal, 'admission_sha256'),
                                 ('admission.json', self.admitted, 'transaction_token_sha256')]:
            value = copy.deepcopy(obj); value[field] = '0' * 64
            self.write(self.authority / name, encode(value))
            with self.subTest(name=name): self.assertEqual(self.exit(entry)['status'], 'inconclusive')
            self.write(self.authority / name, encode(obj))

    def test_missing_record_or_duplicate_json_not_absent_success(self):
        path = self.authority / 'publication.json'; raw = path.read_bytes()
        path.unlink(); self.assertEqual(self.entry()['status'], 'inconclusive')
        self.write(path, b'{"schema":1,"schema":2}')
        self.assertEqual(self.entry()['status'], 'inconclusive')
        self.write(path, raw.replace(b',', b', ', 1))
        self.assertEqual(self.entry()['reason'], 'record-not-canonical')

    def test_sealed_work_change_root_owner_or_build_sidecar_refused(self):
        path = self.work / s.DB; self.write(path, b'changed', self.uid, self.gid)
        self.assertEqual(self.entry()['reason'], 'sealed-work-changed')

    def test_unsafe_record_fifo_link_xattr_and_mode_refused(self):
        path = self.authority / 'publication.json'; path.unlink(); os.mkfifo(path, 0o600)
        self.assertEqual(self.entry()['status'], 'inconclusive')
        path.unlink(); path.symlink_to(self.snap / s.DB)
        self.assertEqual(self.entry()['status'], 'inconclusive')
        path.unlink(); self.write(path, encode(self.publication)); os.setxattr(path, 'user.fixture', b'private')
        self.assertEqual(self.entry()['status'], 'inconclusive')
        os.removexattr(path, 'user.fixture'); path.chmod(0o640)
        self.assertEqual(self.entry()['status'], 'inconclusive')

    def test_root_authority_panel_parent_and_leaf_ownership_not_interchangeable(self):
        self.assertEqual(self.entry()['status'], 'verified')
        os.chown(self.authority, self.uid, self.gid)
        self.assertEqual(self.entry()['status'], 'inconclusive')

    def test_publisher_argv_environment_executable_and_dirfd_binding(self):
        publisher = self.proc / '1002'
        for name, bad in [('cmdline', b'/bin/bash\0'), ('environ', b'HOME=/root\0')]:
            path = publisher / name; raw = path.read_bytes(); self.write(path, bad)
            with self.subTest(name=name): self.assertEqual(self.entry()['status'], 'inconclusive')
            self.write(path, raw)
        fd = publisher / 'fd/21'; fd.unlink(); fd.symlink_to(self.work, target_is_directory=True)
        self.assertEqual(self.entry()['reason'], 'exchange-dirfd-target-differs')

    def test_kernel_lock_must_be_held_on_publishers_fd9(self):
        self.locks[0]['fd'] = 10
        self.assertEqual(self.entry()['reason'], 'publisher-fd9-lock-unproved')

    def test_changed_live_inventory_never_qualifies(self):
        entry = self.entry(); self.exchange(); trace = copy.deepcopy(self.trace); trace['tasks'].pop()
        result = s.inspect(self.args, self.plan, self.worker, trace, entry)
        self.assertEqual(result['status'], 'inconclusive')

    def test_altered_entry_cannot_relabel_the_recorded_before_after_pair(self):
        entry = self.entry(); self.assertEqual(entry['status'], 'verified'); self.exchange()
        changed = copy.deepcopy(entry)
        changed['database']['canonical'], changed['database']['build'] = changed['database']['build'], changed['database']['canonical']
        self.assertEqual(self.exit(changed)['reason'], 'preexchange-pair-differs')
    def test_final_revalidation_changes_refuse(self):
        actual = s.database_state; calls = 0
        def observe(*args):
            nonlocal calls
            result = actual(*args); calls += 1
            if calls == 2: result['records']['publication.json']['sha256'] = '0' * 64
            return result
        with mock.patch.object(s, 'database_state', side_effect=observe):
            self.assertEqual(self.entry()['reason'], 'final-exchange-proof-changed')

    def test_selected_kit_all_files_pinned_no_extra_or_missing(self):
        path = self.kit / 'deploy/release-unit-transition.sh'; self.write(path, b'changed', mode=0o644)
        self.assertEqual(self.entry()['reason'], 'kit-inventory-differs')


class ClosedProofTests(unittest.TestCase):
    def test_unknown_reason_is_sanitized_before_process_access(self):
        with mock.patch.object(s.w, 'validate_plan', side_effect=ValueError('private row')), mock.patch.object(s.w, 'worker_proof') as worker:
            result = s.inspect_entry(None, {}, {}, {})
        self.assertEqual(result['status'], 'inconclusive'); self.assertEqual(result['reason'], 'ValueError')
        self.assertNotIn('private row', json.dumps(result)); worker.assert_not_called()

    def test_only_ctime_is_relaxed_and_path_label_not_inode_identity(self):
        value = {'path': '/fixed/a', 'identity': {'ino': 5, 'ctime_ns': 1, 'mtime_ns': 2}, 'sha256': 'a' * 64}
        renamed = copy.deepcopy(value); renamed['path'] = '/fixed/b'; renamed['identity']['ctime_ns'] = 3
        self.assertEqual(s.file_without_ctime(value), s.file_without_ctime(renamed))
        renamed['identity']['ino'] += 1
        self.assertNotEqual(s.file_without_ctime(value), s.file_without_ctime(renamed))


if __name__ == '__main__':
    unittest.main()