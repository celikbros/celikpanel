import copy
import hashlib
import json
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch
import candidate_archive as archive

HERE = Path(__file__).resolve().parent
class GeneratedMailTests(unittest.TestCase):
    def setUp(self):
        kit = HERE.parents[2]/'internal/mailrenewalkit'
        self.templates = {n: (kit/n).read_bytes() for n in ['renewal.service', 'renewal.timer', 'deploy-hook']}
        # Real Go builder output from source-bound native preparation acceptance.
        self.candidate = {'commit': '1d515d39e00a248a936c52ee8ac4803d3884120e', 'files': {
            'mail-renewal-runtime/renew': 'a002a9c40506f242b38518871389ef98be700e801dec0077a257df3ee706d725',
            'mail-renewal-runtime/runtime.manifest': '6be961629e9f338782a85aa1224f83f833365c5ec46369ef24a33cdaa67d77dd',
            'mail-renewal-runtime/celikpanel-mail-renewal.service': 'dd0c8c78e69afd0ac10cf8847581a6eff4861241e0190022117daab1a54c29a2',
            'mail-renewal-runtime/celikpanel-mail-renewal.timer': '8f4a40a06ace0a4ea845c214504fb5ee2e77e020e0b377828a59d78e948bf8df',
            'mail-renewal-runtime/celikpanel-mail-host-cert': '4e5a376ba6b0b8a491a0012295f8502570418f29d4bd778c26d23f75e4fcb3be'}}
    def verify(self, value):
        def run(args, **kwargs):
            self.assertEqual(args[:5], ['git', '-C', '/fixture/repo', 'show', value['commit']+':internal/mailrenewalkit/'+args[4].split('/')[-1]])
            return subprocess.CompletedProcess(args, 0, stdout=self.templates[args[4].split('/')[-1]])
        with patch.object(archive.subprocess, 'run', side_effect=run): return archive.verify_generated_mail_renewal(value, Path('/fixture/repo'))
    def test_real_go_vector(self): self.assertEqual(self.verify(self.candidate), set(self.candidate['files']))
    def test_substitution_inventory_template(self):
        for name in self.candidate['files']:
            for change in ('missing', 'changed'):
                with self.subTest(name=name, change=change):
                    value = copy.deepcopy(self.candidate)
                    if change == 'missing': del value['files'][name]
                    else: value['files'][name] = '0'*64
                    with self.assertRaises(ValueError): self.verify(value)
        value = copy.deepcopy(self.candidate); value['files']['mail-renewal-runtime/extra'] = '0'*64
        with self.assertRaises(ValueError): self.verify(value)
        for name in self.templates:
            previous = self.templates[name]; self.templates[name] += b'# edit\n'
            with self.assertRaises(ValueError): self.verify(self.candidate)
            self.templates[name] = previous
    def test_legacy_absence(self):
        with patch.object(archive.subprocess, 'run', side_effect=AssertionError('legacy source needs no template')):
            self.assertEqual(archive.verify_generated_mail_renewal({'files': {'bin/agent': '0'*64}}, Path('/fixture/repo')), set())
