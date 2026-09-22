import copy
import hashlib
import json
from pathlib import Path
import unittest
from verify_mail_runtime import verify


class RuntimeEvidenceTests(unittest.TestCase):
    def fixture(self):
        return json.loads(Path(__file__).with_name('MAIL-RUNTIME-BE.json').read_text())

    def edit(self, record, name, old, new):
        item = record['logs'][name]
        self.assertIn(old, item['text'])
        item['text'] = item['text'].replace(old, new)
        item['sha256'] = hashlib.sha256(item['text'].encode()).hexdigest()

    def test_accepts_bounded_evidence(self):
        self.assertEqual(verify(self.fixture())['postboot_new_renewal'], 'verified')

    def test_rejects_semantic_loss_even_with_new_digest(self):
        edits = [('be-renewal', 'empty_queue=no_runtime_initialization', 'empty_queue=unknown'), ('be-renewal', 'ExecMainStatus=0', 'ExecMainStatus=1'), ('be-renewal', 'exact_receipt_and_ledger=matched', 'exact_receipt_and_ledger=unknown'), ('bd-owner', 'owner_content=preserved', 'owner_content=erased'), ('bd-postboot', 'same_leaf_pending=acknowledged', 'same_leaf_pending=unknown')]
        for name, old, new in edits:
            with self.subTest(name=name, field=old):
                r = self.fixture(); self.edit(r, name, old, new)
                with self.assertRaises(ValueError): verify(r)

    def test_rejects_same_boot_or_bad_listener(self):
        r = self.fixture()
        before = r['logs']['be-prepare']['text'].splitlines()[1]
        after = r['logs']['be-renewal']['text'].splitlines()[1]
        self.edit(r, 'be-renewal', after, before)
        with self.assertRaises(ValueError): verify(r)
        r = self.fixture(); self.edit(r, 'be-renewal', 'sha256 Fingerprint=DB:2A', 'sha256 Fingerprint=00:00')
        with self.assertRaises(ValueError): verify(r)

    def test_rejects_overclaim_or_binary_mismatch(self):
        r = self.fixture(); r['scope']['automatic_boot_scheduling'] = True
        with self.assertRaises(ValueError): verify(r)
        r = self.fixture(); r['helper_binary_sha256'] = 'a' * 64
        with self.assertRaises(ValueError): verify(r)
