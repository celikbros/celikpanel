"""Admission and evidence negatives; native boot proof is recorded separately."""
import copy
import importlib.util
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest
from unittest.mock import patch

spec=importlib.util.spec_from_file_location('tested_boot_wait',Path(__file__).with_name('guest_boot_wait.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)


class BootWaitTests(unittest.TestCase):
    def setUp(self):
        self.op='a'*32;self.boot='11111111-1111-4111-8111-111111111111'
        self.intent={'operation_id':self.op,'identity':{'fixture':'identity'},'target':{'commit':'b'*40}}
        status={'schema':'celikpanel-recovery-observation/v1','request_id':self.op,'target_commit':'b'*40,
                'phase':'recovering','terminal_proof':'none','reason':'recovery_running',
                'observed_at':'2026-09-21T20:00:00Z','previous_failure':'update_failed'}
        hint={'schema':'celikpanel-recovery-wait/v1','request_id':self.op,'observation_identity':'sealed-file',
              'observation_sha256':'c'*64,'waiting_for':'starting'}
        cli=dict(status,schema='celikpanel-recovery-status/v1',observation='known',waiting_for='starting')
        cli.pop('target_commit')
        self.value={'status':status,'status_sha256':'c'*64,'hint':hint,'status_identity':'sealed-file',
                    'cli':cli,'readiness':'starting','readiness_exit':1,'lock_free':True,
                    'service':{'ActiveState':'inactive','MainPID':'0','ExecMainStatus':'0'},
                    'journal':[{'_BOOT_ID':self.boot.replace('-',''),'MESSAGE':f.WAIT_MESSAGE+' Details.'}]}
        self.raw=f.encoded(self.intent)
        self.config={'schema':f.SCHEMA,'identity':self.intent['identity'],'operation_id':self.op,
                     'armed_boot_id':'22222222-2222-4222-8222-222222222222','intent_sha256':f.sha(self.raw),
                     'script_sha256':'a'*64,'probe_sha256':'b'*64,'unit_sha256':'c'*64,'max_seconds':120}

    def test_exact_native_wait_and_prior_failure_pass(self):
        f.validate_sample(self.value,self.intent,self.boot)

    def test_no_prior_failure_is_not_invented(self):
        self.value['status']['previous_failure']='none';self.value['cli'].pop('previous_failure')
        f.validate_sample(self.value,self.intent,self.boot)

    def test_wait_requires_same_operation_status_identity_hash_and_time(self):
        for section,key,bad in [('hint','request_id','d'*32),('hint','observation_identity','stale'),
                                ('hint','observation_sha256','d'*64),('status','target_commit','d'*40),
                                ('cli','request_id','d'*32),('cli','observed_at','2026-09-21T19:59:00Z'),
                                ('cli','previous_failure','none'),('cli','waiting_for','stopping')]:
            value=copy.deepcopy(self.value);value[section][key]=bad
            with self.subTest(section=section,key=key),self.assertRaises(ValueError):f.validate_sample(value,self.intent,self.boot)

    def test_current_boot_real_deferral_required(self):
        for journal in ([],[{'_BOOT_ID':'2'*32,'MESSAGE':f.WAIT_MESSAGE}],
                        [{'_BOOT_ID':self.boot.replace('-',''),'MESSAGE':'unrelated status'}]):
            value=copy.deepcopy(self.value);value['journal']=journal
            with self.assertRaises(ValueError):f.validate_sample(value,self.intent,self.boot)

    def test_active_runner_held_lock_unknown_or_terminal_is_not_wait_acceptance(self):
        for section,key,bad in [(None,'lock_free',False),(None,'readiness','running'),(None,'readiness_exit',0),
                                ('service','ActiveState','activating'),('service','MainPID','123'),
                                ('service','ExecMainStatus','1'),('cli','observation','unknown'),
                                ('cli','terminal_proof','rollback_verified'),('status','phase','recovered')]:
            value=copy.deepcopy(self.value);(value if section is None else value[section])[key]=bad
            with self.subTest(section=section,key=key),self.assertRaises(ValueError):f.validate_sample(value,self.intent,self.boot)

    def test_only_sealed_changed_boot_is_admitted(self):
        f.verify_boot(self.config,self.intent,self.raw,self.boot)
        with self.assertRaises(ValueError):f.verify_boot(self.config,self.intent,self.raw,self.config['armed_boot_id'])
        for key,bad in [('intent_sha256','d'*64),('operation_id','d'*32),('max_seconds',True),('max_seconds',121),('schema','other')]:
            value=dict(self.config,**{key:bad})
            with self.subTest(key=key),self.assertRaises(ValueError):f.verify_boot(value,self.intent,self.raw,self.boot)

    def test_unit_is_bounded_real_job_without_product_dependencies_or_fake_readiness(self):
        raw=f.unit_bytes(self.op).decode()
        self.assertIn('Type=oneshot\n',raw);self.assertIn('Before=multi-user.target\n',raw)
        self.assertIn('TimeoutStartSec=150s\n',raw)
        self.assertNotIn('celikpanel-release-recovery.service',raw);self.assertNotIn('is-system-running',raw)
        for op in ('../owner',self.op+'\nExecStart=/bin/false'):
            with self.assertRaises(ValueError):f.unit_bytes(op)

    def test_once_never_replaces_existing_evidence_or_symlink(self):
        with TemporaryDirectory() as directory:
            path=Path(directory)/'proof';f.once(path,b'original')
            with self.assertRaises(FileExistsError):f.once(path,b'new')
            link=Path(directory)/'link';link.symlink_to(path)
            with self.assertRaises(FileExistsError):f.once(link,b'new')
            self.assertEqual(path.read_bytes(),b'original')

    def test_canonical_records_reject_ambiguous_bytes(self):
        self.assertEqual(f.record(b'a=1\nb=2\n',('a','b')),{'a':'1','b':'2'})
        for raw in (b'a=1\nb=2',b'a=1\r\nb=2\r\n',b'a=1\na=2\n',b'a=1\nb=2\nextra\n'):
            with self.assertRaises(ValueError):f.record(raw,('a','b'))

    def test_refused_identity_does_not_enable_or_write_unit(self):
        with patch.object(f,'guarded_intent',side_effect=ValueError('owner server')),patch.object(f,'once') as write,patch.object(f,'run') as command:
            with self.assertRaises(ValueError):f.arm(self.op)
            write.assert_not_called();command.assert_not_called()


if __name__=='__main__':unittest.main()
