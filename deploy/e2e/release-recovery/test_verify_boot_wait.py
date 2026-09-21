"""Reader/material mismatches must not become boot-recovery acceptance."""
import copy
import importlib.util
from pathlib import Path
import unittest

spec=importlib.util.spec_from_file_location('tested_wait_verifier',Path(__file__).with_name('verify_boot_wait.py'))
f=importlib.util.module_from_spec(spec);spec.loader.exec_module(f)


class TerminalAcceptanceTests(unittest.TestCase):
    def setUp(self):
        self.op='a'*32;self.boot='11111111-1111-4111-8111-111111111111'
        self.intent={'operation_id':self.op,'baseline':{'agent_sha256':'b'*64,'panel_sha256':'c'*64}}
        self.proof={'material':{'binding_sha256':'d'*64,'manifest_sha256':'e'*64},
                    'sample':{'hint_sha256':'f'*64,'cli':{'observed_at':'2026-09-21T18:00:00Z'}}}
        cli={'schema':'celikpanel-recovery-status/v1','request_id':self.op,'observation':'known','phase':'recovered',
             'terminal_proof':'rollback_verified','reason':'rollback_verified','observed_at':'2026-09-21T18:01:00Z','previous_failure':'update_failed'}
        self.terminal={'operation_id':self.op,'boot_id':self.boot,'cli':cli,'active_transaction_absent':True,
                       'wait_is_not_exposed':True,'binding_sha256':'d'*64,'manifest_sha256':'e'*64,'stale_wait_sha256':'f'*64,
                       'timer':{'ActiveState':'active'},'services':{}}
        for name,digest in self.intent['baseline'].items():
            self.terminal['services'][name.removesuffix('_sha256')]={'properties':{'ActiveState':'active','SubState':'running'},
                                                                  'installed_sha256':digest,'running_sha256':digest}
        self.readers=[{'cli':dict(cli),'http':{'body':dict(cli,panel_state='ready'),'status':200},'anonymous_status':401}]
        self.journal=[{'_BOOT_ID':self.boot.replace('-',''),'MESSAGE':f.wait.WAIT_MESSAGE,
                       '_SYSTEMD_INVOCATION_ID':'1'*32,'__MONOTONIC_TIMESTAMP':'100'},
                      {'_BOOT_ID':self.boot.replace('-',''),'MESSAGE':'==> Rollback complete / recorded',
                       '_SYSTEMD_INVOCATION_ID':'2'*32,'__MONOTONIC_TIMESTAMP':'200'}]

    def check(self):return f.check_terminal(self.proof,self.terminal,self.intent,self.boot,self.readers,self.journal)

    def test_actual_http_envelope_preserves_exact_cli_fields(self):
        self.assertEqual(self.check()['rollback_invocation'],'2'*32)

    def test_http_schema_operation_failure_proof_or_auth_disagreement_rejected(self):
        for key,value in [('request_id','9'*32),('previous_failure','none'),('terminal_proof','none'),('panel_state','agent_unavailable')]:
            original=copy.deepcopy(self.readers);self.readers[0]['http']['body'][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check()
            self.readers=original
        self.readers[0]['anonymous_status']=200
        with self.assertRaises(ValueError):self.check()

    def test_other_boot_same_invocation_or_reversed_order_is_not_retry(self):
        for key,value in [('_BOOT_ID','9'*32),('_SYSTEMD_INVOCATION_ID','1'*32),('__MONOTONIC_TIMESTAMP','99')]:
            original=copy.deepcopy(self.journal);self.journal[1][key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check()
            self.journal=original

    def test_changed_material_or_deleted_wait_is_not_preservation(self):
        for key,value in [('binding_sha256','9'*64),('manifest_sha256','9'*64),('stale_wait_sha256','9'*64),('active_transaction_absent',False)]:
            original=copy.deepcopy(self.terminal);self.terminal[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.check()
            self.terminal=original

    def test_stale_wait_or_wrong_running_binary_rejected(self):
        self.terminal['cli']['waiting_for']='starting'
        with self.assertRaises(ValueError):self.check()
        del self.terminal['cli']['waiting_for']
        self.terminal['services']['agent']['running_sha256']='9'*64
        with self.assertRaises(ValueError):self.check()


if __name__=='__main__':unittest.main()
