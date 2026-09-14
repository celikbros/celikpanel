#!/usr/bin/env python3
"""Offline tests for the unpublished native candidate fixture boundary."""
import argparse
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock

HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('tested_local_candidate',HERE/'local_candidate_trial.py')
controller=importlib.util.module_from_spec(spec);sys.modules[spec.name]=controller;spec.loader.exec_module(controller)
a=controller.guest.archive_tools;g=controller.guest

class ArtifactTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.root=Path(self.temp.name)
        self.files={name:('fixture '+name+'\n').encode() for name in a.REQUIRED}
        self.files.update({'release.version':b'1\n','release.commit':b'a'*40+b'\n','release.tree':b'b'*40+b'\n'})
    def bundle(self,extra=None,manifest=None):
        files=dict(self.files)
        files['SHA256SUMS']=manifest if manifest is not None else ''.join(hashlib.sha256(files[name]).hexdigest()+'  ./'+name+'\n' for name in sorted(files)).encode()
        path=self.root/'candidate.tar.gz'
        with tarfile.open(path,'w:gz') as bundle:
            info=tarfile.TarInfo('celikpanel-v0.1.0-lab.abc');info.type=tarfile.DIRTYPE;bundle.addfile(info)
            for name,data in files.items():
                info=tarfile.TarInfo('celikpanel-v0.1.0-lab.abc/'+name);info.size=len(data);info.mode=0o755;bundle.addfile(info,io.BytesIO(data))
            if extra is not None:bundle.addfile(extra)
        return path,hashlib.sha256(path.read_bytes()).hexdigest()
    def test_real_full_inventory(self):
        path,digest=self.bundle();value=a.inspect_archive(path,digest)
        self.assertEqual(value['provenance'],'unpublished-local-build-not-signed-agent-admission')
        self.assertEqual(len(value['files']),len(self.files)+1)
    def test_wrong_archive_digest(self):
        path,_=self.bundle()
        with self.assertRaises(ValueError):a.inspect_archive(path,'0'*64)
    def test_omitted_inventory_entry(self):
        path,digest=self.bundle(manifest=b'')
        with self.assertRaises(ValueError):a.inspect_archive(path,digest)
    def test_symlink_refused(self):
        member=tarfile.TarInfo('celikpanel-v0.1.0-lab.abc/link');member.type=tarfile.SYMTYPE;member.linkname='/etc/passwd'
        path,digest=self.bundle(member)
        with self.assertRaises(ValueError):a.inspect_archive(path,digest)
    def test_hardlink_refused(self):
        member=tarfile.TarInfo('celikpanel-v0.1.0-lab.abc/link');member.type=tarfile.LNKTYPE;member.linkname='celikpanel-v0.1.0-lab.abc/update.sh'
        path,digest=self.bundle(member)
        with self.assertRaises(ValueError):a.inspect_archive(path,digest)
    def test_traversal_and_duplicate_refused(self):
        for name in ('celikpanel-v0.1.0-lab.abc/../escape','celikpanel-v0.1.0-lab.abc/update.sh'):
            with self.subTest(name=name):
                path,digest=self.bundle(tarfile.TarInfo(name))
                with self.assertRaises(ValueError):a.inspect_archive(path,digest)
    def test_missing_native_payload_refused(self):
        del self.files['rollback.sh'];path,digest=self.bundle()
        with self.assertRaises(ValueError):a.inspect_archive(path,digest)
    def test_noncanonical_paths(self):
        for name in ('/root/x','root//x','root/./x','root/../x','root/x\\y','other/x','root/x\ny'):
            with self.subTest(name=name),self.assertRaises(ValueError):a.member_path(name,'root')
    @unittest.skipUnless(shutil.which('git'),'Git required for committed source test')
    def test_real_commit_blob_proof_and_dirty_payload_rejection(self):
        repo=self.root/'git';repo.mkdir()
        subprocess.run(['git','init',str(repo)],check=True,capture_output=True)
        (repo/'update.sh').write_bytes(b'actual committed source\n')
        subprocess.run(['git','-C',str(repo),'add','update.sh'],check=True,capture_output=True)
        subprocess.run(['git','-C',str(repo),'-c','user.name=Fixture','-c','user.email=fixture@example.invalid','commit','-m','fixture'],check=True,capture_output=True)
        commit=subprocess.run(['git','-C',str(repo),'rev-parse','HEAD'],check=True,capture_output=True,text=True).stdout.strip()
        tree=subprocess.run(['git','-C',str(repo),'rev-parse','HEAD^{tree}'],check=True,capture_output=True,text=True).stdout.strip()
        candidate={'commit':commit,'tree':tree,'files':{'update.sh':hashlib.sha256((repo/'update.sh').read_bytes()).hexdigest(),'bin/agent':'0'*64}}
        self.assertEqual(a.verify_committed_source(candidate,repo)['verified_static_files'],1)
        candidate['files']['update.sh']='0'*64
        with self.assertRaises(ValueError):a.verify_committed_source(candidate,repo)

class ControllerTests(unittest.TestCase):
    def setUp(self):
        self.ident={'nonce':'a'*64,'vm_uuid':'80b9153c-fac7-503e-90e8-7c984b901a3d','cell_id':'release-recovery__abc','node':'arch'}
        self.op='1'*32;self.paths=g.names(self.op)
        self.intent={'schema':g.SCHEMA,'provenance':'unpublished-local-build-not-signed-agent-admission','identity':self.ident,'operation_id':self.op,'boundary':'require-unit-reload',
                     'candidate':{'root_name':'celikpanel-v0.1.0-lab.abc','files':{},'manifest_sha256':'a'*64}}
        self.intent.update({'source_root':str(g.RELEASES/('.download.lab-'+self.op)/'payload'/self.intent['candidate']['root_name']),'archive_path':str(g.PRIVATE/('local-candidate-'+self.op+'.tar.gz'))})
    def test_exact_staging_boundary(self):
        g.validate_plan(self.intent,self.ident,self.op)
        for field in ('source_root','archive_path'):
            value=dict(self.intent);value[field]='/tmp/untrusted'
            with self.assertRaises(ValueError):g.validate_plan(value,self.ident,self.op)
    def test_data_fault_requires_exact_explicit_option(self):
        g.validate_plan({**self.intent,'candidate_data_fault':'quarantine-fixed-three'},self.ident,self.op)
        for value in (True,False,'',{},'remove-any-path'):
            with self.subTest(value=value),self.assertRaises(ValueError):g.validate_plan({**self.intent,'candidate_data_fault':value},self.ident,self.op)

    def test_data_fault_option_cannot_be_changed_after_prepare(self):
        with mock.patch.object(controller.lab,'checked_root') as checked,self.assertRaises(SystemExit):
            controller.main(['--work-root','/var/tmp/cp-release-drill-test','--mode','start','--execute','--candidate-data-fault','quarantine-fixed-three'])
        checked.assert_not_called()

    def test_prepare_cli_forwards_data_fault_only_after_explicit_execute(self):
        with mock.patch.object(controller.lab,'checked_root',return_value=Path('/unused')),mock.patch.object(controller.lab,'load',return_value=({}, {'nodes':{'arch':{}}})),mock.patch.object(controller.lab,'process_guard'),mock.patch.object(controller,'prepare') as prepare:
            controller.main(['--work-root','/unused','--node','arch','--mode','prepare','--execute','--archive','/tmp/payload','--archive-sha256','a'*64,'--candidate-data-fault','quarantine-fixed-three'])
        self.assertEqual(prepare.call_args.kwargs['candidate_data_fault'],'quarantine-fixed-three')

    def test_wrong_guest_identity_rejected(self):
        with self.assertRaises(ValueError):g.validate_plan(self.intent,{**self.ident,'nonce':'b'*64},self.op)
    def test_no_signed_admission_claim(self):
        value={**self.intent,'provenance':'signed'}
        with self.assertRaises(ValueError):g.validate_plan(value,self.ident,self.op)
    def test_checkpoint_requires_candidate_unit_helpers_and_pending_reload(self):
        self.intent['candidate']['files'].update({'bin/agent':'a'*64,'bin/panel':'b'*64})
        self.intent['candidate']['files'].update({name:'c'*64 for name in g.INSTALLED})
        native=g.LocalNative(argparse.Namespace(operation_id=self.op),self.intent,{'bash_sha256':'d'*64})
        with mock.patch.object(g.kill.Native,'installed',return_value={'agent':'a'*64,'panel':'b'*64}),mock.patch.object(g.shared,'digest_file',return_value='c'*64),mock.patch.object(native,'unit_reload',return_value={'celikpanel-agent.service':'yes'}):
            self.assertIsNotNone(native.installed())
        with mock.patch.object(g.kill.Native,'installed',return_value={'agent':'a'*64,'panel':'b'*64}),mock.patch.object(g.shared,'digest_file',return_value='c'*64),mock.patch.object(native,'unit_reload',return_value={'celikpanel-agent.service':'no'}):
            self.assertIsNone(native.installed())
        with mock.patch.object(g.kill.Native,'installed',return_value={'agent':'a'*64,'panel':'b'*64}),mock.patch.object(g.shared,'digest_file',return_value='e'*64):
            self.assertIsNone(native.installed())
    def test_stage_proof_requires_full_manifest_and_exact_operation(self):
        proof={'schema':'celikpanel/local-candidate-stage/v1','identity':self.ident,'operation_id':self.op,'root':self.intent['source_root'],'manifest_sha256':'a'*64,'verified_files':0,'bash_sha256':'b'*64}
        controller.validate_stage(proof,self.intent)
        for key,value in (('verified_files',1),('operation_id','2'*32),('manifest_sha256','c'*64)):
            with self.subTest(key=key),self.assertRaises(ValueError):controller.validate_stage({**proof,key:value},self.intent)
    def test_native_start_argv_and_onfailure(self):
        argv=controller.launch_argv(self.intent,'start')
        self.assertIn('--property=OnFailure=celikpanel-release-recovery.service',argv)
        self.assertEqual(argv[-3:],['/bin/bash',self.intent['source_root']+'/bootstrap-prebuilt-update.sh','--normal'])
        self.assertFalse(any('--self-update-worker' in item for item in argv))
    def test_fault_cleanup_only_exact_unique_unit(self):
        argv=controller.launch_argv(self.intent,'arm')
        self.assertIn('--property=ExecStopPost=-/usr/bin/systemctl thaw '+self.paths['worker'],argv)
        self.assertIn('-I',argv);self.assertIn('--property=RuntimeMaxSec=650',argv)
    def test_invalid_operation_never_constructs_unit(self):
        for operation in ('abc','../agent','1'*32+';reboot'):
            with self.assertRaises(ValueError):g.names(operation)
    def test_armed_requires_fresh_exact_worker_and_no_transaction(self):
        state={'states':{self.paths['fault']:{'ActiveState':'active'},self.paths['worker']:{'LoadState':'not-found'}},'transaction':None}
        controller.require_armed(state,[{'event':'armed'}],self.intent)
        for events in ([],[{'event':'armed'},{'event':'released'}]):
            with self.assertRaises(ValueError):controller.require_armed(state,events,self.intent)
        state['transaction']={'operation':'update'}
        with self.assertRaises(ValueError):controller.require_armed(state,[{'event':'armed'}],self.intent)
    def test_existing_start_blocks_repeat_even_dangling_symlink(self):
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);evidence=root/'evidence'/'arch';evidence.mkdir(parents=True)
            controller.assert_absent(root,'arch',controller.START)
            (evidence/controller.START).write_text('once')
            with self.assertRaises(ValueError):controller.assert_absent(root,'arch',controller.START)
    def test_missing_execute_never_loads_lab(self):
        with mock.patch.object(controller.lab,'checked_root') as checked,self.assertRaises(SystemExit):
            controller.main(['--work-root','/var/tmp/cp-release-drill-test','--mode','start'])
        checked.assert_not_called()
    def test_timeout_is_saved_and_not_retried(self):
        with mock.patch.object(controller.lab,'guarded_script',side_effect=subprocess.TimeoutExpired('ssh',30)) as call,mock.patch.object(controller.trial,'save',return_value={'sha256':'a'*64}) as save:
            with self.assertRaises(ValueError):controller.launch_once(Path('/unused'),{}, {},'arch',self.intent,'start')
        self.assertEqual(call.call_count,1);self.assertEqual(save.call_count,3)
    def test_start_intent_written_before_only_native_submission(self):
        raw=controller.encoded(self.intent);armed={'identity':self.ident,'operation_id':self.op,'intent_sha256':hashlib.sha256(raw).hexdigest()}
        state={'states':{self.paths['fault']:{'ActiveState':'active'},self.paths['worker']:{'LoadState':'not-found'}},'transaction':None};order=[]
        with mock.patch.object(controller,'assert_absent'),mock.patch.object(controller.trial,'read_private',side_effect=[controller.encoded(armed),raw]),mock.patch.object(controller,'read_guest',return_value=(state,b'', [{'event':'armed'}])),mock.patch.object(controller.trial,'save',side_effect=lambda *a:order.append('intent')),mock.patch.object(controller,'launch_once',side_effect=lambda *a:order.append('launch')):
            controller.start(Path('/unused'),{}, {},'arch',self.intent)
        self.assertEqual(order,['intent','launch'])

if __name__=='__main__':unittest.main()
