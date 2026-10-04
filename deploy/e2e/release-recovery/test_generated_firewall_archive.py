#!/usr/bin/env python3
"""Fail-closed archive admission for the actual artifact-v1 build vector."""
import copy
import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import patch

HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('candidate_archive',HERE/'candidate_archive.py')
archive=importlib.util.module_from_spec(spec);spec.loader.exec_module(archive)

class GeneratedFirewallTests(unittest.TestCase):
    def setUp(self):
        self.template=(HERE.parents[2]/'internal/firewallruntime/firewall.service').read_bytes()
        self.candidate={'commit':'a'*40,'files':{
            'firewall-runtime/restore':'4937746c7ee74241f3dce97fe73843faa96e61fe57a4588e914327261d80d9da',
            'firewall-runtime/runtime.manifest':'54cd4e489fa9757047abd78cd28ea217fd42557778e115c4a123e264130c664f',
            'firewall-runtime/celikpanel-firewall-restore.service':'2b8e4403e8fc2457cf64b8f128fb11a71c4a6534e7d78fba29900c03624e8062',
            'deploy/systemd/celikpanel-firewall-restore.service':'2b8e4403e8fc2457cf64b8f128fb11a71c4a6534e7d78fba29900c03624e8062'}}

    def verify(self,candidate,template=None):
        result=subprocess.CompletedProcess([],0,stdout=self.template if template is None else template)
        with patch.object(archive.subprocess,'run',return_value=result) as call:
            value=archive.verify_generated_firewall(candidate,Path('/fixture/repo'))
            if value:
                self.assertEqual(call.call_args.args[0],['git','-C','/fixture/repo','show',candidate['commit']+':internal/firewallruntime/firewall.service'])
            return value

    def test_actual_go_artifact_vector(self):
        self.assertEqual(self.verify(self.candidate),set(self.candidate['files']))

    def test_substitution_missing_extra_and_unknown_template(self):
        for name in self.candidate['files']:
            for action in ('missing','changed'):
                with self.subTest(name=name,action=action):
                    bad=copy.deepcopy(self.candidate)
                    if action=='missing':del bad['files'][name]
                    else:bad['files'][name]='0'*64
                    with self.assertRaises(ValueError):self.verify(bad)
        bad=copy.deepcopy(self.candidate);bad['files']['firewall-runtime/extra']='0'*64
        with self.assertRaises(ValueError):self.verify(bad)
        for template in (b'',self.template+b'# edit\n',self.template.replace(b'@RESTORE@',b'/other')):
            with self.subTest(template=len(template)), self.assertRaises(ValueError):self.verify(self.candidate,template)

    def test_historical_archive_without_artifact(self):
        with patch.object(archive.subprocess,'run',side_effect=AssertionError('legacy source needs no template')):
            self.assertEqual(archive.verify_generated_firewall({'files':{'deploy/systemd/celikpanel-firewall-restore.service':'0'*64}},Path('/fixture/repo')),set())

if __name__=='__main__':unittest.main()
