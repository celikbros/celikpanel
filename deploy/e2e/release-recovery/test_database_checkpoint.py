"""Offline frozen database observations; never a VM or product lifecycle test."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import sqlite3
import stat
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest import mock

HERE=Path(__file__).resolve().parent
spec=importlib.util.spec_from_file_location('tested_database_checkpoint',HERE/'guest_database_checkpoint.py')
s=importlib.util.module_from_spec(spec);sys.modules[spec.name]=s;spec.loader.exec_module(s)

def encode(value):return (json.dumps(value,separators=(',',':'))+'\n').encode()
def flat(info):
    result={k:info[k] for k in ('dev','ino','mode','uid','gid','links','size')}
    for name in ('mtime','ctime'):
        result[name+'_sec']=info[name]['Sec'];result[name+'_nsec']=info[name]['Nsec']
    return result

@unittest.skipUnless(sys.platform=='linux' and getattr(os,'geteuid',lambda:-1)()==0,'root-owned Linux fixture required')
class DatabaseTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='frozen-db-',dir='/root');self.addCleanup(self.temp.cleanup);self.base=Path(self.temp.name)
        self.parent=self.base/'panel';self.parent.mkdir(mode=0o700)
        self.root=self.parent/'.release-db-migrations';self.root.mkdir(mode=0o700)
        self.token='a'*64;self.transaction_root=self.root/self.token;self.transaction_root.mkdir(mode=0o700)
        self.authority=self.transaction_root/'authority';self.authority.mkdir(mode=0o700)
        self.work=self.transaction_root/'work';self.work.mkdir(mode=0o700)
        self.snapshots=self.base/'snapshots';self.snapshots.mkdir(mode=0o700);self.snapshot='20260915T000000Z-from-old-to-'+'b'*40+'-'+'c'*32
        self.snap=self.snapshots/self.snapshot;self.snap.mkdir(mode=0o700)
        self.rows=[{'version':n,'filename':f'{n:03d}_fixture.sql','sha256':hashlib.sha256(str(n).encode()).hexdigest()} for n in range(1,39)]
        self.plan={'identity':{'fixture':'exact'},'baseline_profile':'alpha64-schema38','baseline_migration_identities_sha256':hashlib.sha256(json.dumps(self.rows,sort_keys=True,separators=(',',':')).encode()).hexdigest(),'candidate':{'manifest_sha256':'d'*64,'files':{'bin/panel':'e'*64,**{'internal/db/migrations/'+r['filename']:r['sha256'] for r in self.rows}}}}
        self.plan['candidate']={'commit':'4'*40,'tree':'5'*40,'manifest_sha256':'d'*64,'files':{'bin/panel':'e'*64}}
        migration_rows=[{'version':n,'filename':f'{n:03d}_fixture.sql','sha256':hashlib.sha256(str(n).encode()).hexdigest()} for n in range(1,43)]
        self.plan['candidate_migration_identities']={'schema':'celikpanel/lab-candidate-migration-identities/v1','source_commit':'4'*40,'source_tree':'5'*40,'migrations':migration_rows,'sha256':s.profiles.identities_digest(migration_rows)}
        db=self.parent/'celikpanel.db';c=sqlite3.connect(db)
        c.execute('CREATE TABLE schema_migrations(version INTEGER PRIMARY KEY,filename TEXT,sha256 TEXT)')
        c.executemany('INSERT INTO schema_migrations VALUES(?,?,?)',[(r['version'],r['filename'],r['sha256']) for r in self.rows])
        c.execute('CREATE TABLE secrets(value TEXT)');c.execute("INSERT INTO secrets VALUES('must-not-appear-in-evidence')");c.commit();c.close();db.chmod(0o600)
        for target in (self.work/'celikpanel.db',self.snap/'celikpanel.db'):shutil.copyfile(db,target);target.chmod(0o600)
        self.proof={'snapshot':self.snapshot,'manifest_sha256':'f'*64}
        self.transaction={'snapshot':self.snapshot,'transaction_operation':'update','transaction_phase':'active','transaction_token_sha256':self.token}
        before=s.file_proof(db,lambda:None,{0});initial=s.file_proof(self.work/'celikpanel.db',lambda:None,{0})
        self.material={'schema':'celikpanel/recovery-material/v3','snapshot_manifest_sha256':'f'*64,'record_sha256':'1'*64,'database_before':{'sha256':before['sha256'],'file':flat(before['identity'])}}
        self.admission={'schema':'celikpanel/database-migration-admission/v1','snapshot':self.snapshot,'transaction_token_sha256':self.token,'material_sha256':'1'*64,'snapshot_manifest_sha256':'f'*64,'candidate_panel_sha256':'e'*64,'parent':s.directory(self.parent,{0}),'before':before,'root':s.directory(self.root,{0}),'transaction':s.directory(self.transaction_root,{0}),'authority':s.directory(self.authority,{0}),'work':s.directory(self.work,{0}),'initial':initial}
        self.write('admission.json',self.admission)
        for name,value in (('PARENT',self.parent),('ROOT',self.root),('SNAPSHOTS',self.snapshots)):
            p=mock.patch.object(s,name,value);p.start();self.addCleanup(p.stop)
        p=mock.patch.object(s.forward,'material_proof',return_value=self.material);p.start();self.addCleanup(p.stop)
    def write(self,name,value):
        p=self.authority/name;p.write_bytes(encode(value));p.chmod(0o600)
    def inspect(self):return s.inspect(self.proof,self.plan,self.transaction,lambda:None)
    def fingerprint(self):
        return {str(p.relative_to(self.base)):(s.identity(p.lstat()),hashlib.sha256(p.read_bytes()).hexdigest()) for p in self.base.rglob('*') if p.is_file()}
    def test_exact_initial_images_observed_without_changing_files_or_leaking_rows(self):
        before=self.fingerprint();v=self.inspect();self.assertEqual(v['premigration_acceptance'],'PASS-state-only')
        self.assertTrue(v['canonical_matches_admitted_before']);self.assertTrue(v['work_matches_admitted_initial'])
        self.assertEqual(v['work_migration_history']['schema_version'],38);self.assertEqual(before,self.fingerprint())
        self.assertNotIn('must-not-appear',json.dumps(v));self.assertIn('no-BEGIN',str(v['limits']))
    def test_real_committed_work_migration_is_classified_without_premigration_claim(self):
        path=self.work/'celikpanel.db';c=sqlite3.connect(path);digest=hashlib.sha256(b'39').hexdigest()
        c.execute('INSERT INTO schema_migrations VALUES(39,?,?)',('039_fixture.sql',digest));c.commit();c.close()
        self.plan['candidate']['files']['internal/db/migrations/039_fixture.sql']=digest
        v=self.inspect();self.assertEqual(v['premigration_acceptance'],'INCONCLUSIVE');self.assertEqual(v['work_migration_history']['schema_version'],39)
        self.assertTrue(v['canonical_matches_admitted_before']);self.assertFalse(v['work_matches_admitted_initial'])
    def test_canonical_changes_not_silently_accepted_as_before(self):
        c=sqlite3.connect(self.parent/'celikpanel.db');c.execute('CREATE TABLE changed(id INTEGER)');c.commit();c.close()
        v=self.inspect();self.assertFalse(v['canonical_matches_admitted_before']);self.assertEqual(v['premigration_acceptance'],'INCONCLUSIVE')
    def test_sidecars_never_ignored_or_modified(self):
        p=self.work/'celikpanel.db-wal';p.write_bytes(b'partial-uncommitted-evidence');p.chmod(0o600)
        before=self.fingerprint();v=self.inspect();self.assertEqual(v['work_migration_history']['status'],'unavailable')
        self.assertEqual(v['premigration_acceptance'],'INCONCLUSIVE');self.assertEqual(before,self.fingerprint())
    def test_publication_record_prevents_initial_claim(self):
        self.write('publication.json',{'schema':'celikpanel/database-publication-intent/v1','admission_sha256':'2'*64})
        v=self.inspect();self.assertFalse(v['publication_records_absent']);self.assertEqual(v['premigration_acceptance'],'INCONCLUSIVE')
    def test_root_private_build_accepts_actual_panel_owned_database_payload(self):
        build=self.authority/('.build-'+'b'*32);build.mkdir(mode=0o700)
        target=build/'celikpanel.db';shutil.copyfile(self.parent/'celikpanel.db',target);target.chmod(0o600);os.chown(target,65534,65534)
        with mock.patch.object(s.probe,'database_owners',return_value={0,65534}):
            before=self.fingerprint();value=self.inspect()
            self.assertEqual(value['temporary_evidence'][build.name]['files']['celikpanel.db']['identity']['uid'],65534)
            self.assertEqual(value['premigration_acceptance'],'INCONCLUSIVE');self.assertEqual(before,self.fingerprint())
            for mode in (0o750,0o770):
                build.chmod(mode)
                with self.subTest(mode=mode),self.assertRaises((s.Unavailable,s.probe.ProbeError)):self.inspect()
            build.chmod(0o700);os.chown(build,65534,65534)
            with self.assertRaises((s.Unavailable,s.probe.ProbeError)):self.inspect()

    def test_actual_panel_owned_parent_and_work_with_strict_root_authority_leaves(self):
        with mock.patch.object(s.probe,'database_owners',return_value={0,65534}):
            os.chown(self.parent,65534,65534);self.parent.chmod(0o750)
            for path in (self.root,self.transaction_root):os.chown(path,0,65534);path.chmod(0o710)
            os.chown(self.work,65534,65534);self.work.chmod(0o700)
            for path in (self.parent/'celikpanel.db',self.work/'celikpanel.db'):os.chown(path,65534,65534)
            for key,path in (('parent',self.parent),('root',self.root),('transaction',self.transaction_root),('authority',self.authority),('work',self.work)):
                self.admission[key]=s.directory(path,{0,65534} if key in ('parent','work') else {0})
            self.admission['before']=s.file_proof(self.parent/'celikpanel.db',lambda:None,{0,65534})
            self.admission['initial']=s.file_proof(self.work/'celikpanel.db',lambda:None,{0,65534})
            self.material['database_before']={'sha256':self.admission['before']['sha256'],'file':flat(self.admission['before']['identity'])}
            self.write('admission.json',self.admission)
            before=self.fingerprint();value=self.inspect();self.assertEqual(value['premigration_acceptance'],'PASS-state-only');self.assertEqual(before,self.fingerprint())
            build=self.authority/('.build-'+'d'*32);build.mkdir(mode=0o700)
            target=build/'celikpanel.db';shutil.copyfile(self.parent/'celikpanel.db',target);target.chmod(0o600);os.chown(target,65534,65534)
            self.assertEqual(self.inspect()['temporary_evidence'][build.name]['files']['celikpanel.db']['identity']['uid'],65534)
            os.chown(self.authority/'admission.json',65534,65534)
            with self.assertRaises((s.Unavailable,s.probe.ProbeError)):self.inspect()

    def test_wrong_snapshot_token_material_and_candidate_binding_refused(self):
        for key in ('snapshot','transaction_token_sha256','material_sha256','snapshot_manifest_sha256','candidate_panel_sha256'):
            original=self.admission[key];self.admission[key]='wrong';self.write('admission.json',self.admission)
            with self.subTest(key=key),self.assertRaises(s.Unavailable):self.inspect()
            self.admission[key]=original
        self.write('admission.json',self.admission)
        self.material['database_before']['sha256']='3'*64
        with self.assertRaises(s.Unavailable):self.inspect()
    def test_sealed_candidate_map_change_refuses_before_database_capture(self):
        self.plan['candidate_migration_identities']['migrations'][0]['sha256']='9'*64
        with self.assertRaises(ValueError):self.inspect()

    def test_changed_snapshot_and_initial_hash_refused(self):
        (self.snap/'celikpanel.db').write_bytes(b'changed')
        with self.assertRaises(s.Unavailable):self.inspect()
    def test_symlink_fifo_permissions_and_owner_refused_without_block(self):
        p=self.work/'celikpanel.db';saved=p.read_bytes()
        for kind in ('symlink','fifo','mode','owner'):
            p.unlink()
            if kind=='symlink':p.symlink_to(self.parent/'celikpanel.db')
            elif kind=='fifo':os.mkfifo(p,0o600)
            else:p.write_bytes(saved);p.chmod(0o666 if kind=='mode' else 0o600)
            if kind=='owner':os.chown(p,65534,65534)
            with self.subTest(kind=kind),self.assertRaises((s.Unavailable,s.probe.ProbeError,OSError)):self.inspect()
    def test_missing_record_not_misreported_as_initial(self):
        (self.authority/'admission.json').unlink()
        with self.assertRaises(s.Unavailable):self.inspect()
    def test_immutable_read_rechecks_identity_after_read(self):
        original=s.history
        def mutate(path,*args):
            value=original(path,*args)
            if path==self.work/'celikpanel.db':path.write_bytes(path.read_bytes()+b'changed')
            return value
        with mock.patch.object(s,'history',side_effect=mutate),self.assertRaises(s.Unavailable):self.inspect()
    def test_pending_completion_never_claims_premigration_even_if_same_images(self):
        self.transaction['transaction_phase']='completion.pending';self.assertEqual(self.inspect()['premigration_acceptance'],'INCONCLUSIVE')
    def test_secret_or_unrecognized_migration_identity_is_not_emitted(self):
        c=sqlite3.connect(self.work/'celikpanel.db');c.execute("UPDATE schema_migrations SET filename='private-secret' WHERE version=1");c.commit();c.close()
        v=self.inspect();self.assertEqual(v['work_migration_history']['status'],'unavailable');self.assertNotIn('private-secret',json.dumps(v))

class GuardTests(unittest.TestCase):
    def test_unavailable_identity_or_unfrozen_worker_never_runs_observer(self):
        native=SimpleNamespace(worker_identity=mock.Mock(return_value={'pid':1}),revalidate=mock.Mock(side_effect=ValueError('not-frozen')))
        with mock.patch.object(s.probe,'guard_guest',return_value={'exact':1}),mock.patch.object(s,'inspect') as inspect:
            value=s.observe(None,{'identity':{'exact':1}},{},lambda:None,native)
        self.assertEqual(value['status'],'unavailable');inspect.assert_not_called()
    def test_changed_exact_transaction_after_observation_not_accepted(self):
        native=SimpleNamespace(worker_identity=lambda:{'pid':1},revalidate=lambda _:None)
        with mock.patch.object(s.probe,'guard_guest',return_value={'exact':1}),mock.patch.object(s.fault,'read_transaction',side_effect=[{'token':'a'},{'token':'b'}]),mock.patch.object(s,'inspect',return_value={'status':'observed'}):
            value=s.observe(None,{'identity':{'exact':1}},{},lambda:None,native)
        self.assertEqual(value['status'],'unavailable');self.assertEqual(value['premigration_acceptance'],'INCONCLUSIVE')

if __name__=='__main__':unittest.main()
