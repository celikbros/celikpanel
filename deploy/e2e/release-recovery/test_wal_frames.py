#!/usr/bin/env python3
"""WAL-format evidence tests: synthetic adversaries and real private SQLite WALs."""
import base64
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sqlite3
import stat
import struct
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location("fixture_wal_frames", Path(__file__).with_name("wal_frames.py"))
wal = importlib.util.module_from_spec(spec)
spec.loader.exec_module(wal)


def checksum(raw, order, seed=(0, 0)):
    # Independent test encoder, using 32-bit word traversal instead of parser's
    # paired struct iteration. Real SQLite writers below arbitrate both encoders.
    words = struct.unpack(order + str(len(raw) // 4) + "I", raw)
    sums = list(seed)
    for i, word in enumerate(words):
        j = i % 2
        sums[j] = (sums[j] + word + sums[1 - j]) % (2 ** 32)
    return tuple(sums)


def image(pages=(0,), *, page_size=512, order="<", salts=(0x13579BDF, 0x02468ACE),
          sequence=7, payload=b"private-page-secret-never-return"):
    magic = 0x377F0682 if order == "<" else 0x377F0683
    header = struct.pack(">6I", magic, 3007000, page_size, sequence, *salts)
    sums = checksum(header, order)
    result = header + struct.pack(">2I", *sums)
    for index, dbsize in enumerate(pages, 1):
        data = (payload * ((page_size // len(payload)) + 1))[:page_size]
        first = struct.pack(">2I", index, dbsize)
        sums = checksum(first + data, order, sums)
        result += first + struct.pack(">4I", *salts, *sums) + data
    return result


def header_field(raw, offset, value, *, repair=True):
    changed = bytearray(raw)
    struct.pack_into(">I", changed, offset, value)
    if repair:
        order = "<" if struct.unpack_from(">I", changed)[0] == 0x377F0682 else ">"
        struct.pack_into(">2I", changed, 24, *checksum(changed[:24], order))
    return bytes(changed)


class WALFormatTests(unittest.TestCase):
    def unknown(self, value, reason=None, **kwargs):
        result = wal.inspect_wal(value, **kwargs)
        self.assertEqual(result['status'], 'unknown', result)
        self.assertEqual(result['classification'], 'unknown')
        self.assertFalse(result['growth_after_prior_commit'])
        self.assertNotIn('noncommit_suffix_frames', result)
        self.assertEqual(result['uncommitted_transaction'], 'not-established-by-bytes')
        if reason:
            self.assertEqual(result['reason'], reason)
        return result

    def test_checksum_reference_vectors_and_uint32_wrap(self):
        for order in ('<', '>'):
            raw = struct.pack(order + '8I', *range(1, 9))
            self.assertEqual(wal._checksum(memoryview(raw), order, (0, 0)), (79, 133))
            raw = struct.pack(order + '2I', 0xFFFFFFFF, 0xFFFFFFFF)
            self.assertEqual(wal._checksum(memoryview(raw), order, (0, 0)), (0xFFFFFFFF, 0xFFFFFFFE))
            self.assertEqual(wal._checksum(memoryview(raw), order, (0xFFFFFFFF, 0xFFFFFFFE)), (0xFFFFFFFC, 0xFFFFFFF9))

    def test_both_checksum_endians_validate_multiple_commits_and_suffix(self):
        for order in ('<', '>'):
            raw = image((0, 2, 0, 4, 0, 0), order=order)
            result = wal.inspect_wal(raw, expected_page_size=512)
            self.assertEqual(result['status'], 'verified')
            self.assertEqual(result['latest_commit_frame'], 4)
            self.assertEqual(result['latest_commit_database_size_pages'], 4)
            self.assertEqual(result['noncommit_suffix_frames'], 2)
            self.assertEqual(result['classification'], 'valid-noncommit-suffix')
            self.assertEqual(result['committed_prefix_bytes'], 32 + 4 * 536)
            self.assertEqual(result['committed_prefix_sha256'], hashlib.sha256(raw[:32 + 4 * 536]).hexdigest())
            self.assertEqual(result['header']['checksum_input_byteorder'], 'little' if order == '<' else 'big')
            self.assertEqual([f['is_commit'] for f in result['frames']], [False, True, False, True, False, False])
            self.assertEqual(result['frames'][-1]['end_offset'], len(raw))

    def test_all_supported_page_sizes_including_full_65536_wal_encoding(self):
        for exponent in range(9, 17):
            size = 1 << exponent
            with self.subTest(page_size=size):
                result = wal.inspect_wal(image((1,), page_size=size), expected_page_size=size)
                self.assertEqual(result['status'], 'verified')
                self.assertEqual(result['frame_size'], size + 24)

    def test_header_only_and_no_commit_do_not_claim_transaction(self):
        header = image(())
        result = wal.inspect_wal(header)
        self.assertEqual(result['classification'], 'header-only')
        self.assertEqual(result['latest_commit_frame'], 0)
        self.assertFalse(result['committed_prefix_has_transaction'])
        self.assertEqual(result['noncommit_suffix_frames'], 0)
        result = wal.inspect_wal(image((0, 0)), prior_prefix=header)
        self.assertEqual(result['classification'], 'valid-noncommit-suffix')
        self.assertEqual(result['latest_commit_frame'], 0)
        self.assertTrue(result['growth_after_prior_commit'])
        self.assertEqual(result['uncommitted_transaction'], 'not-established-by-bytes')

    def test_committed_only_and_exact_unchanged_prior_are_not_pending_growth(self):
        raw = image((0, 2))
        result = wal.inspect_wal(raw, prior_prefix=raw)
        self.assertEqual(result['classification'], 'committed-prefix-only')
        self.assertEqual(result['noncommit_suffix_frames'], 0)
        self.assertFalse(result['growth_after_prior_commit'])
        self.assertEqual(result['prior_prefix']['appended_frames'], 0)

    def test_exact_committed_prefix_extension_records_growth_without_new_commit(self):
        prior = image((0, 2))
        current = image((0, 2, 0, 0))
        result = wal.inspect_wal(current, prior_prefix=prior)
        self.assertTrue(result['growth_after_prior_commit'])
        self.assertEqual(result['prior_prefix']['appended_frames'], 2)
        self.assertEqual(result['prior_prefix']['appended_commit_frames'], 0)
        self.assertEqual(result['prior_prefix']['sha256'], hashlib.sha256(prior).hexdigest())

    def test_new_commit_since_prior_prevents_single_unfinished_growth_claim(self):
        result = wal.inspect_wal(image((2, 0, 4, 0)), prior_prefix=image((2,)))
        self.assertEqual(result['status'], 'verified')
        self.assertEqual(result['noncommit_suffix_frames'], 1)
        self.assertEqual(result['prior_prefix']['appended_commit_frames'], 1)
        self.assertFalse(result['growth_after_prior_commit'])

    def test_empty_partial_header_and_all_partial_frame_lengths_are_unknown(self):
        raw = image((2, 0))
        for count in range(32):
            self.unknown(raw[:count], 'empty-or-truncated-header')
        for remainder in (1, 4, 8, 23, 24, 25, 535):
            result = self.unknown(raw[:32 + 536 + remainder], 'trailing-incomplete-frame')
            self.assertEqual(result['diagnostic_valid_prefix']['latest_commit_frame'], 1)
            self.assertEqual(result['diagnostic_valid_prefix']['remaining_unverified_bytes'], remainder)

    def test_unsupported_header_fields_and_main_database_size_sentinel(self):
        raw = image((1,))
        for offset, value, reason in ((0, 0, 'unsupported-magic'), (0, 0x377F0684, 'unsupported-magic'),
                (4, 3007001, 'unsupported-version'), (4, 0, 'unsupported-version')):
            self.unknown(header_field(raw, offset, value), reason)
        for value in (0, 1, 256, 513, 1000, 65537, 131072, 0xFFFFFFFF):
            self.unknown(header_field(raw, 8, value), 'unsupported-page-size')
        self.unknown(raw, 'database-page-size-mismatch', expected_page_size=4096)

    def test_header_corruption_and_wrong_stored_checksum_endianness(self):
        raw = image((1,))
        for offset in (12, 16, 20, 24, 31):
            changed = bytearray(raw);changed[offset] ^= 1
            self.unknown(bytes(changed), 'header-checksum-mismatch')
        changed = bytearray(raw)
        values = struct.unpack_from('>2I', raw, 24)
        struct.pack_into('<2I', changed, 24, *values)
        self.unknown(bytes(changed), 'header-checksum-mismatch')

    def test_wrong_checksum_input_order_is_not_salvaged(self):
        # Big-endian magic with checksums computed little-endian is invalid.
        raw = image((1,))
        self.unknown(header_field(raw, 0, 0x377F0683, repair=False), 'header-checksum-mismatch')

    def test_salt_mismatch_even_after_valid_noncommit_prefix_is_unknown(self):
        raw = bytearray(image((2, 0, 0)))
        raw[32 + 2 * 536 + 8] ^= 1
        result = self.unknown(bytes(raw), 'stale-or-mismatched-frame-salts')
        self.assertEqual(result['diagnostic_valid_prefix']['frame_count'], 2)
        self.assertEqual(result['diagnostic_valid_prefix']['latest_commit_frame'], 1)

    def test_stale_tail_after_reset_is_unknown_even_if_previous_generation_valid(self):
        old = image((0, 3, 0))
        new = image((1,), salts=(8, 9), sequence=8)
        self.unknown(new + old[len(new):], 'stale-or-mismatched-frame-salts')
        self.unknown(image((1,), salts=(8, 9), sequence=8), 'prior-header-or-generation-changed', prior_prefix=image((1,)))

    def test_zero_padding_and_full_invalid_tail_are_not_ignored(self):
        result = self.unknown(image((1, 0)) + bytes(536), 'invalid-frame-page-number')
        self.assertEqual(result['diagnostic_valid_prefix']['frame_count'], 2)
        self.unknown(image((1, 0)) + b'garbage', 'trailing-incomplete-frame')

    def test_frame_header_payload_or_checksum_tampering_unknown(self):
        raw = image((1, 0))
        for offset in (32, 36, 48, 55, 56, 32 + 536 + 100):
            changed = bytearray(raw);changed[offset] ^= 1
            self.unknown(bytes(changed))

    def test_invalid_page_numbers_rejected_despite_recomputed_checksums(self):
        for page, dbsize in ((0, 0), (0xFFFFFFFF, 0), (1, 0xFFFFFFFF)):
            raw = image(())
            data = b'x' * 512
            sums = checksum(struct.pack('>2I', page, dbsize) + data, '<', struct.unpack_from('>2I', raw, 24))
            current = raw + struct.pack('>6I', page, dbsize, *struct.unpack_from('>2I', raw, 16), *sums) + data
            self.unknown(current, 'invalid-frame-page-number')

    def test_prior_reset_truncation_rewrite_and_noncommit_prior_unknown(self):
        self.unknown(image((1,)), 'prior-prefix-truncated', prior_prefix=image((1, 2)))
        self.unknown(image((1,)), 'prior-header-or-generation-changed', prior_prefix=image((1,), sequence=6))
        self.unknown(image((1,), payload=b'different'), 'prior-prefix-rewritten', prior_prefix=image((1,)))
        self.unknown(image((0, 0)), 'prior-prefix-not-committed-or-header-only', prior_prefix=image((0,)))
        self.unknown(image((0,)), 'prior-prefix-not-committed-or-header-only', prior_prefix=b'')
        self.unknown(image((0,)), 'prior-prefix-not-committed-or-header-only', prior_prefix=b'bad-prefix')

    def test_bounds_and_input_types_never_positive(self):
        raw = image((1, 0))
        self.unknown(raw, 'byte-limit', max_bytes=32)
        self.unknown(raw, 'frame-limit', max_frames=1)
        for key, values in {'max_bytes':(0, 31, -1, wal.MAX_BYTES + 1, True, 1024.0),
                            'max_frames':(0, -1, wal.MAX_FRAMES + 1, True, 1.0),
                            'expected_page_size':(0, 1, True, 4096.0)}.items():
            for value in values:self.unknown(raw, 'invalid-input', **{key:value})
        for value in ('secret', bytearray(raw), memoryview(raw), None):self.unknown(value, 'invalid-input')
        self.unknown(raw, 'invalid-input', prior_prefix=bytearray(image(())))

    def test_output_is_bounded_json_safe_without_page_contents(self):
        raw = image((1, 0), payload=b'private-page-secret-never-return')
        result = wal.inspect_wal(raw)
        serialized = json.dumps(result)
        self.assertNotIn('private-page-secret', serialized)
        self.assertEqual(result['sha256'], hashlib.sha256(raw).hexdigest())
        for frame in result['frames']:
            self.assertEqual(frame['frame_sha256'], hashlib.sha256(raw[frame['offset']:frame['end_offset']]).hexdigest())


@unittest.skipUnless(os.name == 'posix' and hasattr(os, 'pread'), 'POSIX read-only FD copy')
class WALDescriptorTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory();self.path = Path(self.temp.name) / 'copied.wal'
        self.raw = image((1, 0));self.path.write_bytes(self.raw)
        self.fd = os.open(self.path, os.O_RDONLY)

    def tearDown(self):
        os.close(self.fd);self.temp.cleanup()

    def test_pread_keeps_offset_and_descriptor_and_returns_identity(self):
        os.lseek(self.fd, 7, os.SEEK_SET)
        result = wal.inspect_wal_fd(self.fd)
        self.assertEqual(result['status'], 'verified')
        self.assertEqual(os.lseek(self.fd, 0, os.SEEK_CUR), 7)
        self.assertEqual(result['fd_copy']['identity']['ino'], os.fstat(self.fd).st_ino)
        self.assertEqual(self.path.read_bytes(), self.raw)
        self.assertEqual(result['fd_copy']['path_authority'], 'not-established')

    def test_nonregular_pipe_directory_and_write_descriptor_refuse(self):
        reader, writer = os.pipe();directory = os.open(self.path.parent, os.O_RDONLY)
        writable = os.open(self.path, os.O_RDWR)
        try:
            for fd in (reader, writer, directory, writable):
                self.assertEqual(wal.inspect_wal_fd(fd)['reason'], 'fd-not-readonly-single-link-regular')
        finally:
            for fd in (reader, writer, directory, writable):os.close(fd)

    def test_hardlinked_descriptor_refuses(self):
        os.link(self.path, self.path.with_suffix('.alias'))
        self.assertEqual(wal.inspect_wal_fd(self.fd)['status'], 'unknown')

    def test_short_read_closed_fd_and_bounds_unknown(self):
        with patch.object(wal.os, 'pread', return_value=b''):
            self.assertEqual(wal.inspect_wal_fd(self.fd)['reason'], 'fd-short-read')
        fd = os.dup(self.fd);os.close(fd)
        self.assertEqual(wal.inspect_wal_fd(fd)['reason'], 'fd-copy-unavailable')
        self.assertEqual(wal.inspect_wal_fd(self.fd, max_bytes=32)['reason'], 'fd-byte-limit')
        for invalid in (-1, True, 1.5):self.assertEqual(wal.inspect_wal_fd(invalid)['reason'], 'invalid-fd-input')

    def test_same_size_content_or_metadata_change_during_copy_unknown(self):
        original = os.pread
        changed = False
        def mutate(fd, count, offset):
            nonlocal changed
            result = original(fd, count, offset)
            if not changed:
                changed = True
                with self.path.open('r+b') as stream:stream.write(b'X')
                st = self.path.stat();os.utime(self.path, ns=(st.st_atime_ns, st.st_mtime_ns + 1000000))
            return result
        with patch.object(wal.os, 'pread', side_effect=mutate):
            self.assertEqual(wal.inspect_wal_fd(self.fd)['reason'], 'fd-changed-during-copy')


class RealSQLiteWALTests(unittest.TestCase):
    def test_real_c_sqlite_commits_spill_and_rollback_remnant(self):
        for page_size in (512, 4096, 65536):
            with self.subTest(page_size=page_size), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'private.sqlite';writer = sqlite3.connect(path, isolation_level=None)
                try:
                    writer.execute('PRAGMA page_size=' + str(page_size))
                    self.assertEqual(writer.execute('PRAGMA journal_mode=WAL').fetchone()[0], 'wal')
                    writer.execute('PRAGMA wal_autocheckpoint=0');writer.execute('PRAGMA cache_size=2')
                    writer.execute('PRAGMA cache_spill=ON');writer.execute('CREATE TABLE evidence(id INTEGER PRIMARY KEY, secret BLOB)')
                    wal_path = Path(str(path) + '-wal');prior = wal_path.read_bytes()
                    before = wal.inspect_wal(prior, expected_page_size=page_size)
                    self.assertEqual(before['classification'], 'committed-prefix-only', before)
                    writer.execute('BEGIN IMMEDIATE')
                    writer.execute('INSERT INTO evidence(secret) VALUES(zeroblob(?))', (2 * 1024 * 1024,))
                    pending = wal_path.read_bytes();result = wal.inspect_wal(pending, expected_page_size=page_size, prior_prefix=prior)
                    self.assertEqual(result['status'], 'verified', result)
                    self.assertEqual(result['classification'], 'valid-noncommit-suffix')
                    self.assertTrue(result['growth_after_prior_commit'])
                    self.assertTrue(writer.in_transaction)
                    self.assertEqual(result['latest_commit_frame'], before['latest_commit_frame'])
                    writer.execute('ROLLBACK')
                    remnant = wal_path.read_bytes()
                    self.assertEqual(remnant, pending, 'SQLite may preserve valid rollback remnants')
                    self.assertFalse(writer.in_transaction)
                    after = wal.inspect_wal(remnant, prior_prefix=prior)
                    self.assertEqual(after['classification'], 'valid-noncommit-suffix')
                    self.assertEqual(after['uncommitted_transaction'], 'not-established-by-bytes')
                finally:writer.close()

    def test_real_c_sqlite_checkpoint_reset_must_not_extend_prior_generation(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'private.sqlite';writer = sqlite3.connect(path, isolation_level=None)
            try:
                writer.execute('PRAGMA journal_mode=WAL');writer.execute('PRAGMA wal_autocheckpoint=0')
                writer.execute('CREATE TABLE evidence(value BLOB)')
                wal_path = Path(str(path) + '-wal');prior = wal_path.read_bytes()
                writer.execute('PRAGMA wal_checkpoint(TRUNCATE)')
                self.assertEqual(wal.inspect_wal(wal_path.read_bytes())['status'], 'unknown')
                writer.execute('INSERT INTO evidence VALUES(zeroblob(100))')
                result = wal.inspect_wal(wal_path.read_bytes(), prior_prefix=prior)
                self.assertEqual(result['status'], 'unknown')
                self.assertIn(result['reason'], ('prior-header-or-generation-changed', 'prior-prefix-truncated'))
            finally:writer.close()

    @unittest.skipUnless(os.name == 'posix' and shutil.which('go'), 'local Go modernc fixture compiler')
    def test_real_pinned_modernc_wal_copy_matches_parser(self):
        # This test writes only a private test DB. Go module downloads are off.
        repo = Path(__file__).resolve().parents[3]
        source = r'''package main
import("context";"database/sql";"os";"path/filepath";_ "modernc.org/sqlite")
func main(){
 root:=os.Args[1]; path:=filepath.Join(root,"private.sqlite")
 db,e:=sql.Open("sqlite",path);if e!=nil{panic(e)};defer db.Close()
 conn,e:=db.Conn(context.Background());if e!=nil{panic(e)};defer conn.Close()
 run:=func(s string){if _,e:=conn.ExecContext(context.Background(),s);e!=nil{panic(e)}}
 copyWal:=func(name string){b,e:=os.ReadFile(path+"-wal");if e!=nil{panic(e)};if e=os.WriteFile(filepath.Join(root,name),b,0600);e!=nil{panic(e)}}
 run("PRAGMA page_size=1024");run("PRAGMA journal_mode=WAL");run("PRAGMA wal_autocheckpoint=0");run("PRAGMA cache_size=2");run("PRAGMA cache_spill=ON")
 run("CREATE TABLE evidence(id INTEGER PRIMARY KEY, secret BLOB)");copyWal("prior.wal")
 run("BEGIN IMMEDIATE");run("INSERT INTO evidence(secret) VALUES(zeroblob(262144))");copyWal("pending.wal")
 run("ROLLBACK");copyWal("after-rollback.wal")
}
'''
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory);program = root / 'main.go';program.write_text(source, encoding='utf-8')
            binary = root / 'writer';env = {**os.environ, 'GOPROXY':'off', 'GOTOOLCHAIN':'local'}
            build = subprocess.run(['go', 'build', '-o', str(binary), str(program)], cwd=repo,
                                   env=env, capture_output=True, timeout=120)
            self.assertEqual(build.returncode, 0, build.stderr.decode(errors='replace')[:2000])
            run = subprocess.run([str(binary), str(root)], capture_output=True, timeout=30, env=env)
            self.assertEqual(run.returncode, 0, run.stderr.decode(errors='replace')[:2000])
            prior = (root / 'prior.wal').read_bytes();pending = (root / 'pending.wal').read_bytes()
            result = wal.inspect_wal(pending, expected_page_size=1024, prior_prefix=prior)
            self.assertEqual(result['status'], 'verified', result)
            self.assertEqual(result['classification'], 'valid-noncommit-suffix')
            self.assertTrue(result['growth_after_prior_commit'])
            self.assertEqual(pending, (root / 'after-rollback.wal').read_bytes())
            self.assertEqual(result['uncommitted_transaction'], 'not-established-by-bytes')


if __name__ == '__main__':
    unittest.main()
