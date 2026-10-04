"""Component evidence tests only; fake proc records never represent native acceptance."""
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import stat
import sys
import tempfile
import time
from types import SimpleNamespace
import unittest
from unittest import mock

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('tested_guest_wal_checkpoint', HERE / 'guest_wal_checkpoint.py')
s = importlib.util.module_from_spec(spec); sys.modules[spec.name] = s; spec.loader.exec_module(s)
framespec = importlib.util.spec_from_file_location('checkpoint_frame_fixtures', HERE / 'test_wal_frames.py')
f = importlib.util.module_from_spec(framespec); framespec.loader.exec_module(f)

def encode(value):
    return (json.dumps(value, separators=(',', ':')) + '\n').encode()

def flat(proof):
    item = proof['identity']; result = {k: item[k] for k in ('dev','ino','mode','uid','gid','links','size')}
    for name in ('mtime','ctime'):
        result[name + '_sec'] = item[name + '_ns'] // 1000000000
        result[name + '_nsec'] = item[name + '_ns'] % 1000000000
    return result

ROOT_ONLY = sys.platform == 'linux' and getattr(os, 'geteuid', lambda: -1)() == 0

@unittest.skipUnless(ROOT_ONLY, 'root-owned no-follow component filesystem')
class FilesystemTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='wal-checkpoint-', dir='/root')
        self.addCleanup(self.temp.cleanup); self.root = Path(self.temp.name)
        self.uid = self.gid = 65534
        def directory(name, uid=0, gid=0, mode=0o700):
            p = self.root / name; p.mkdir(parents=True, mode=mode); os.chown(p, uid, gid); p.chmod(mode); return p
        self.parent = directory('panel', self.uid, self.gid, 0o750)
        self.migration = directory('panel/.release-db-migrations', 0, self.gid, 0o710)
        self.token_raw = b'a' * 64; self.token = s.sha(self.token_raw)
        self.base = directory('panel/.release-db-migrations/' + self.token, 0, self.gid, 0o710)
        self.authority = directory('panel/.release-db-migrations/' + self.token + '/authority')
        self.work = directory('panel/.release-db-migrations/' + self.token + '/work', self.uid, self.gid)
        self.transactions = directory('transactions'); self.snapshots = directory('snapshots')
        self.proc = directory('proc'); self.cgroup = directory('cgroup'); self.installed = directory('installed')
        self.material_root = directory('material')
        self.commit = 'b' * 40; self.operation = 'c' * 32
        self.snapshot = '20260915T010203Z-from-unknown-to-' + self.commit + '-' + 'd' * 32
        self.snap = directory('snapshots/' + self.snapshot)
        self.guest = {'nonce':'1'*64, 'vm_uuid':'00000000-0000-0000-0000-000000000001','cell_id':'wal-fixture','node':'arch'}
        self.args = SimpleNamespace(operation_id=self.operation)
        self.plan = {'identity':self.guest, 'operation_id':self.operation, 'source_root':'/sealed/source',
                     'candidate':{'commit':self.commit, 'manifest_sha256':'e'*64,
                                  'files':{'bin/panel':s.sha(b'candidate-panel'), 'bin/agent':s.sha(b'candidate-agent')}}}
        for name, value in [('PARENT',self.parent),('MIGRATIONS',self.migration),('TRANSACTIONS',self.transactions),
                            ('SNAPSHOTS',self.snapshots),('PROC',self.proc),('CGROUP',self.cgroup),('INSTALLED',self.installed)]:
            patch = mock.patch.object(s,name,value); patch.start(); self.addCleanup(patch.stop)
        patch = mock.patch.object(s,'account',return_value=(self.uid,self.gid)); patch.start(); self.addCleanup(patch.stop)
        patch = mock.patch.object(s.probe,'guard_guest',return_value=self.guest); patch.start(); self.addCleanup(patch.stop)
        self.write(self.parent/'celikpanel.db', b'private-table-row-never-emitted', self.uid, self.gid)
        self.write(self.work/'celikpanel.db', b'private-table-row-never-emitted', self.uid, self.gid)
        self.write(self.snap/'celikpanel.db', b'private-table-row-never-emitted')
        self.write(self.snap/'snapshot.version', b'6\n')
        sums = b''.join((s.sha(p.read_bytes()) + '  ./' + p.name + '\n').encode() for p in sorted(self.snap.iterdir()))
        self.write(self.snap/'SHA256SUMS', sums)
        self.write(self.transactions/'active', b'version=1\ntoken=' + self.token_raw + b'\noperation=update\nsnapshot=' + self.snapshot.encode() + b'\n')
        self.write(self.transactions/'transaction.lock', b'')
        self.before = s._read(self.parent/'celikpanel.db', self.budget(), {self.uid})
        self.initial = s._read(self.work/'celikpanel.db', self.budget(), {self.uid})
        self.admitted = {'schema':'celikpanel/database-migration-admission/v1','snapshot':self.snapshot,
            'transaction_token_sha256':self.token,'material_sha256':'f'*64,'snapshot_manifest_sha256':s.sha(sums),
            'candidate_panel_sha256':self.plan['candidate']['files']['bin/panel'],
            'before':s.product_file(self.before),'initial':s.product_file(self.initial)}
        for name,path in [('parent',self.parent),('root',self.migration),('transaction',self.base),('authority',self.authority),('work',self.work)]:
            self.admitted[name] = s.directory_id(path.stat())
        self.write(self.authority/'admission.json', encode(self.admitted))
        self.material = {'schema':'celikpanel/recovery-material/v3','snapshot_manifest_sha256':s.sha(sums),'record_sha256':'f'*64,
                         'database_before':{'file':flat(self.before),'sha256':self.before['sha256'],
                         'parent':{**self.admitted['parent'],'links':0,'size':0,'mtime_sec':0,'mtime_nsec':0,'ctime_sec':0,'ctime_nsec':0}} }
        patch = mock.patch.object(s.checkpoint.forward,'material_proof',return_value=self.material); patch.start(); self.addCleanup(patch.stop)
        self.prior = f.image((2,))
        self.raw = f.image((2,0))
        self.write(self.work/'celikpanel.db-wal', self.raw, self.uid,self.gid)
        self.write(self.work/'celikpanel.db-shm', b'private-shm',self.uid,self.gid)
        self.write(self.installed/'panel',b'candidate-panel')
        self.worker = {'unit':'celikpanel-self-update-'+self.operation+'.service','pid':1001,'start_ticks':10,
                       'invocation_id':'1'*32,'cgroup':'/system.slice/celikpanel-self-update-'+self.operation+'.service',
                       'running_executable_sha256':s.sha(b'bash'),'boot_id':'11111111-1111-1111-1111-111111111111'}
        self.trace = {'tracer_pid':os.getpid(),'tasks':[{'tid':1001,'tgid':1001,'start_ticks':10},{'tid':1002,'tgid':1002,'start_ticks':11}],
                      'writer':{'pid':1002,'tid':1002},'write':{'number':18,'fd':8,'offset':len(self.prior)+24,
                       'requested_bytes':512,'returned_bytes':512,'is_error':False,'arch':0xc000003e,'entry_exit_matched':True}}
    def budget(self): return s.Budget(lambda:None)
    def write(self,path,raw,uid=0,gid=0,mode=0o600):
        path.write_bytes(raw); os.chown(path,uid,gid); path.chmod(mode)
    def tx(self): return s.transaction(self.budget())
    def state(self): return s.database_state(self.plan,self.tx(),self.budget(),self.uid,self.gid)[0]
    def fingerprint(self):
        return {str(p.relative_to(self.root)):(s.identity(p.lstat()),s.sha(p.read_bytes())) for p in self.root.rglob('*') if p.is_file() and not p.is_symlink()}
    def test_actual_panel_owned_chain_root_authority_and_advanced_work_file(self):
        with (self.work/'celikpanel.db').open('ab') as out: out.write(b'expected-work-change')
        before = self.fingerprint(); result=self.state()
        self.assertEqual(result['canonical_before'],self.before)
        self.assertNotEqual(s.product_file(result['work_files']['celikpanel.db']),self.admitted['initial'])
        self.assertEqual(result['work_directory'],self.admitted['work'])
        self.assertEqual(before,self.fingerprint())
        self.assertNotIn('private-table-row-never-emitted',json.dumps(result))
    def test_malformed_initial_identity_and_material_parent_are_not_observed(self):
        value=copy.deepcopy(self.admitted);value['initial']['identity']['ctime']['Nsec']=False
        self.write(self.authority/'admission.json',encode(value))
        with self.assertRaises(s.Inconclusive):self.state()
        self.write(self.authority/'admission.json',encode(self.admitted))
        self.material['database_before']['parent']['ino']+=1
        with self.assertRaises(s.Inconclusive):self.state()

    def test_complete_inspect_composes_every_native_boundary_and_rechecks(self):
        self.fake_processes()
        worker={**self.worker,'executable':{'sha256':self.worker['running_executable_sha256']}}
        lock=[{'pid':1001,'fd':9,'kernel_lock_type':'FLOCK-ADVISORY-WRITE'}]
        with mock.patch.object(s,'worker_proof',return_value=worker),mock.patch.object(s,'exclusive_lock',return_value=lock),mock.patch.object(s.matcher,'match_writer',return_value={'status':'matched'}) as match:
            before=self.fingerprint()
            result=s.inspect(self.args,self.plan,self.worker,self.trace,prior_wal=self.prior)
            self.assertEqual(result['status'],'verified',result)
            self.assertEqual(result['classification'],'physical-noncommit-WAL-write-held')
            self.assertTrue(result['wal']['growth_after_prior_commit'])
            self.assertEqual(before,self.fingerprint());match.assert_called_once()
            changed=copy.deepcopy(self.trace);changed['write']['offset']=0
            self.assertEqual(s.inspect(self.args,self.plan,self.worker,changed,prior_wal=self.prior)['status'],'inconclusive')
            self.assertEqual(s.inspect(self.args,self.plan,self.worker,self.trace,prior_wal=f.image((2,),salts=(1,2)))['status'],'inconclusive')
        with mock.patch.object(s,'worker_proof',return_value=worker),mock.patch.object(s,'exclusive_lock',side_effect=s.Inconclusive('updater-exclusive-lock-unproved')),mock.patch.object(s,'database_state') as database:
            result=s.inspect(self.args,self.plan,self.worker,self.trace,prior_wal=self.prior)
            self.assertEqual(result['status'],'inconclusive');database.assert_not_called()
    def test_same_token_hint_is_non_authorizing_and_missing_is_none(self):
        expected=s.writer_expected(self.args,self.plan,10)
        self.assertEqual(expected['work'],self.admitted['work'])
        self.assertEqual(expected['token_sha256'],self.token)
        (self.authority/'admission.json').unlink()
        self.assertIsNone(s.writer_expected(self.args,self.plan,10))
        (self.transactions/'active').unlink()
        self.assertIsNone(s.writer_expected(self.args,self.plan,10))
    def test_wrong_tuple_duplicate_json_and_extra_admission_field_refuse(self):
        for key in ('snapshot','transaction_token_sha256','candidate_panel_sha256','snapshot_manifest_sha256','material_sha256'):
            value=copy.deepcopy(self.admitted);value[key]='wrong'
            self.write(self.authority/'admission.json',encode(value))
            with self.subTest(key=key),self.assertRaises((s.Inconclusive,s.probe.ProbeError)):self.state()
        self.write(self.authority/'admission.json',encode({**self.admitted,'unknown':1}))
        with self.assertRaises(s.Inconclusive):self.state()
        self.write(self.authority/'admission.json',b'{"schema":1,"schema":2}')
        with self.assertRaises(s.probe.ProbeError):self.state()
    def test_work_inode_replacement_and_root_owned_work_refuse(self):
        path=self.work/'celikpanel.db'; old=path.with_suffix('.kept');path.rename(old)
        self.write(path,b'private-table-row-never-emitted',self.uid,self.gid)
        with self.assertRaises(s.Inconclusive):self.state()
        path.unlink();old.rename(path);os.chown(self.work,0,0)
        with self.assertRaises(s.Inconclusive):self.state()
    def test_seal_publication_partial_record_and_journal_never_qualify(self):
        for name in ('seal.json','publication.json','.record-'+'a'*32):
            path=self.authority/name; self.write(path,b'{}')
            with self.subTest(name=name),self.assertRaises(s.Inconclusive):self.state()
            path.unlink()
        self.write(self.work/'celikpanel.db-journal',b'journal',self.uid,self.gid)
        with self.assertRaises(s.Inconclusive):self.state()
    def test_canonical_change_sidecar_snapshot_corruption_never_qualify(self):
        self.write(self.parent/'celikpanel.db-wal',b'no-ignore',self.uid,self.gid)
        with self.assertRaises(s.Inconclusive):self.state()
        (self.parent/'celikpanel.db-wal').unlink()
        self.write(self.snap/'celikpanel.db',b'corrupt')
        with self.assertRaises(s.Inconclusive):self.state()
    def test_all_nonactive_markers_refused(self):
        for phase in ('completion.pending','scheduler-restore.pending','quiesce.pending'):
            self.write(self.transactions/phase,(self.transactions/'active').read_bytes())
            with self.subTest(phase=phase),self.assertRaises(s.Inconclusive):self.tx()
            self.assertIsNone(s.writer_expected(self.args,self.plan,10))
            (self.transactions/phase).unlink()
    def test_nofollow_fifo_hardlink_xattrs_and_changed_during_read(self):
        path=self.authority/'admission.json';raw=path.read_bytes();path.unlink();os.mkfifo(path,0o600)
        started=time.monotonic()
        with self.assertRaises(s.Inconclusive):s._read(path,self.budget(),{0},1048576)
        self.assertLess(time.monotonic()-started,.5);path.unlink();path.symlink_to(self.snap/'celikpanel.db')
        with self.assertRaises(OSError):s._read(path,self.budget(),{0})
        path.unlink();self.write(path,raw);os.link(path,self.authority/'alias')
        with self.assertRaises(s.Inconclusive):s._read(path,self.budget(),{0})
        (self.authority/'alias').unlink();os.setxattr(path,'user.fixture',b'private')
        with self.assertRaises(s.Inconclusive):s._read(path,self.budget(),{0})
        os.removexattr(path,'user.fixture')
        original=os.read
        def change(fd,size):
            value=original(fd,size)
            if value: path.chmod(0o640)
            return value
        with mock.patch.object(s.os,'read',side_effect=change),self.assertRaises(s.Inconclusive):s._read(path,self.budget(),{0})
    def test_symlink_ancestor_refused_before_outside_read(self):
        kept=self.root/'kept';self.authority.rename(kept);self.authority.symlink_to(kept,target_is_directory=True)
        with self.assertRaises(OSError):self.state()
    def test_real_kernel_exclusive_lock_required_and_not_merely_open(self):
        import fcntl
        with mock.patch.object(s,'PROC',Path('/proc')):
            tasks=[{'tgid':os.getpid()}]
            with (self.transactions/'transaction.lock').open('rb') as locked:
                with self.assertRaises(s.Inconclusive):s.exclusive_lock(self.worker,tasks,self.budget(),self.uid)
                fcntl.flock(locked,fcntl.LOCK_SH)
                with self.assertRaises(s.Inconclusive):s.exclusive_lock(self.worker,tasks,self.budget(),self.uid)
                fcntl.flock(locked,fcntl.LOCK_EX)
                result=s.exclusive_lock(self.worker,tasks,self.budget(),self.uid)
                self.assertTrue(any(v['fd']==locked.fileno() for v in result))
                self.assertEqual(result[0]['kernel_lock_type'],'FLOCK-ADVISORY-WRITE')
                fcntl.flock(locked,fcntl.LOCK_UN)
                with self.assertRaises(s.Inconclusive):s.exclusive_lock(self.worker,tasks,self.budget(),self.uid)
    def fake_lock_kernel(self, *, filesystem='btrfs', stat_minor=34, mount_minor=33):
        root=self.proc/'1001';root.mkdir();(root/'fd').mkdir();(root/'fdinfo').mkdir();(root/'ns').mkdir()
        descriptor=root/'fd'/'9';descriptor.symlink_to(self.transactions/'transaction.lock')
        (root/'ns'/'mnt').symlink_to('mnt:[4026531841]')
        self.write(root/'fdinfo'/'9',f'pos:\t0\nflags:\t02100000\nmnt_id:\t39\nino:\t76858\nlock:\t1: FLOCK  ADVISORY  WRITE 21714 00:{mount_minor:02x}:76858 0 EOF\n'.encode())
        self.write(root/'mountinfo',f'39 1 0:{mount_minor} / / rw,relatime shared:1 - {filesystem} /dev/vda3 rw\n'.encode())
        actual_read=s._read;actual_stat=Path.stat
        proof=actual_read(self.transactions/'transaction.lock',self.budget(),{0},1,private=True)
        proof['identity'].update(dev=os.makedev(0,stat_minor),ino=76858)
        held=SimpleNamespace(**{'st_'+{'ino':'ino','dev':'dev','mode':'mode','uid':'uid','gid':'gid','links':'nlink','size':'size','mtime_ns':'mtime_ns','ctime_ns':'ctime_ns'}[k]:v for k,v in proof['identity'].items()})
        def read(path,*args,**kwargs):
            return copy.deepcopy(proof) if path==self.transactions/'transaction.lock' else actual_read(path,*args,**kwargs)
        def stat_path(path,*args,**kwargs):
            return held if path==descriptor else actual_stat(path,*args,**kwargs)
        patch=mock.patch.object(s,'_read',side_effect=read);patch.start();self.addCleanup(patch.stop)
        patch=mock.patch.object(Path,'stat',stat_path);patch.start();self.addCleanup(patch.stop)
        return root,held
    def test_btrfs_fd_lock_binds_mount_superblock_not_subvolume_stat_device(self):
        self.fake_lock_kernel()
        result=s.exclusive_lock(self.worker,[{'tgid':1001}],self.budget(),self.uid)
        self.assertEqual(result[0]['fd'],9)
        self.assertEqual(result[0]['kernel_lock_type'],'FLOCK-ADVISORY-WRITE')
        self.assertEqual(result[0]['mount']['superblock_device'],{'major':0,'minor':33})
        self.assertEqual(result[0]['file']['identity']['dev'],34)
    def test_lock_mount_metadata_drift_and_replaced_descriptor_are_refused(self):
        root,held=self.fake_lock_kernel()
        original=s._virtual
        for change in ('mount','fdinfo','namespace','descriptor'):
            reads={'mount':0,'fdinfo':0,'namespace':0}
            original_link=os.readlink
            def read(path,*args,**kwargs):
                value=original(path,*args,**kwargs)
                key='mount' if path==root/'mountinfo' else 'fdinfo' if path==root/'fdinfo'/'9' else None
                if key:
                    reads[key]+=1
                    if reads[key]==2 and change==key:
                        return value.replace(b'0:33',b'0:35') if key=='mount' else value.replace(b'pos:\t0',b'pos:\t1')
                if change=='descriptor' and key=='mount':held.st_ino=76859
                return value
            def link(path,*args,**kwargs):
                value=original_link(path,*args,**kwargs)
                if path==root/'ns'/'mnt':
                    reads['namespace']+=1
                    if change=='namespace' and reads['namespace']==2:return 'mnt:[4026531999]'
                return value
            held.st_ino=76858
            with self.subTest(change=change),mock.patch.object(s,'_virtual',side_effect=read),mock.patch.object(s.os,'readlink',side_effect=link),self.assertRaises(s.Inconclusive):
                s.exclusive_lock(self.worker,[{'tgid':1001}],self.budget(),self.uid)
    def fake_processes(self):
        group=self.cgroup/self.worker['cgroup'].lstrip('/');group.mkdir(parents=True)
        self.write(group/'cgroup.procs',b'1001\n1002\n');self.write(group/'cgroup.threads',b'1001\n1002\n')
        for pid,started,owner in ((1001,10,0),(1002,11,self.uid)):
            root=self.proc/str(pid);root.mkdir();(root/'fd').mkdir();(root/'task').mkdir()
            self.write(root/'status',f'Name:\tfixture\nState:\tt (tracing stop)\nTgid:\t{pid}\nPid:\t{pid}\nTracerPid:\t{os.getpid()}\nUid:\t{owner} {owner} {owner} {owner}\nGid:\t{owner} {owner} {owner} {owner}\n'.encode())
            tail=['0']*22;tail[0]='t';tail[19]=str(started)
            self.write(root/'stat',(str(pid)+' (fixture) '+' '.join(tail)).encode())
            self.write(root/'cgroup',('0::'+self.worker['cgroup']+'\n').encode())
        writer=self.proc/'1002';(writer/'fd'/'8').symlink_to(self.work/'celikpanel.db-wal');(writer/'exe').symlink_to(self.installed/'panel')
        self.write(writer/'cmdline',s.matcher.COMMAND)
        env={**s.matcher.ENVIRONMENT,b'CELIKPANEL_DATA_DIR':str(self.work).encode()}
        self.write(writer/'environ',b'\0'.join(k+b'='+v for k,v in env.items())+b'\0')
    def test_stopped_inventory_rejects_missing_extra_running_reused_or_foreign_tracer(self):
        self.fake_processes();result=s.stopped_inventory(self.worker,self.trace,self.budget(),self.uid)
        self.assertEqual(len(result),2)
        for field,value in [('tracer_pid',os.getpid()+1),('tasks',self.trace['tasks'][:-1])]:
            changed=copy.deepcopy(self.trace);changed[field]=value
            with self.subTest(field=field),self.assertRaises(s.Inconclusive):s.stopped_inventory(self.worker,changed,self.budget(),self.uid)
        path=self.proc/'1002/status';raw=path.read_bytes()
        for changed in (raw.replace(b't (tracing stop)',b'S (sleeping)'),raw.replace(str(os.getpid()).encode(),b'99')):
            self.write(path,changed)
            with self.assertRaises(s.Inconclusive):s.stopped_inventory(self.worker,self.trace,self.budget(),self.uid)
        self.write(path,raw);changed=copy.deepcopy(self.trace);changed['tasks'][1]['start_ticks']+=1
        with self.assertRaises(s.Inconclusive):s.stopped_inventory(self.worker,changed,self.budget(),self.uid)
    def test_writer_fd_causal_shape_and_executable_binding(self):
        self.fake_processes()
        value,raw,proof=s.writer_observation(self.worker,self.trace,self.work,self.budget(),self.uid,self.gid,self.plan['candidate']['files']['bin/panel'])
        self.assertEqual(raw,self.raw);self.assertEqual(value['wal_entry'],proof['identity'])
        for field,wrong in [('number',1),('is_error',True),('entry_exit_matched',False),('returned_bytes',0),('arch',0),('fd',True)]:
            trace=copy.deepcopy(self.trace);trace['write'][field]=wrong
            with self.subTest(field=field),self.assertRaises(s.Inconclusive):s.writer_observation(self.worker,trace,self.work,self.budget(),self.uid,self.gid,self.plan['candidate']['files']['bin/panel'])
        target=self.proc/'1002/fd/8';target.unlink();target.symlink_to(self.parent/'celikpanel.db')
        with self.assertRaises(s.Inconclusive):s.writer_observation(self.worker,self.trace,self.work,self.budget(),self.uid,self.gid,self.plan['candidate']['files']['bin/panel'])

class ClosedResultTests(unittest.TestCase):
    def test_guard_failure_has_closed_reason_and_never_reads_processes(self):
        with mock.patch.object(s,'validate_plan',side_effect=ValueError('secret-example')) as guard, mock.patch.object(s,'worker_proof') as worker:
            result=s.inspect(None,{}, {},{}, prior_wal=b'')
        self.assertEqual(result['status'],'inconclusive');self.assertEqual(result['reason'],'ValueError');worker.assert_not_called()
        self.assertNotIn('secret-example',json.dumps(result))
    def test_bad_prior_fails_before_process_observation(self):
        with mock.patch.object(s,'validate_plan',return_value={}),mock.patch.object(s,'worker_proof') as worker:
            result=s.inspect(None,{}, {},{}, prior_wal=bytearray())
        self.assertEqual(result['status'],'inconclusive');worker.assert_not_called()
    def test_budget_is_bounded_and_read_only(self):
        budget=s.Budget(lambda:None)
        with self.assertRaises(s.Inconclusive):budget(2*1024**3+1)

class LockMountParserTests(unittest.TestCase):
    RAW='pos:\t0\nflags:\t02100000\nmnt_id:\t39\nino:\t76858\nlock:\t1: FLOCK  ADVISORY  WRITE 21714 00:21:76858 0 EOF\n'
    MOUNT='39 1 0:33 / / rw,relatime shared:1 - btrfs /dev/vda3 rw\n'
    def test_fd_specific_superblock_tuple_parses_mixed_numeric_bases(self):
        result=s.lock_mount_identity(self.RAW,self.MOUNT,76858)
        self.assertEqual(result['superblock_device'],{'major':0,'minor':33})
        ext4=s.lock_mount_identity(self.RAW.replace('00:21','fe:01'),self.MOUNT.replace('0:33','254:1').replace('btrfs','ext4'),76858)
        self.assertEqual(ext4['superblock_device'],{'major':254,'minor':1})
    def test_missing_duplicate_malformed_or_foreign_mount_and_lock_refused(self):
        cases=[
            (self.RAW.replace('mnt_id:\t39\n',''),self.MOUNT,76858),
            (self.RAW+'mnt_id:\t39\n',self.MOUNT,76858),
            (self.RAW+'ino:\t76858\n',self.MOUNT,76858),
            (self.RAW.replace('ino:\t76858','ino:\t76859'),self.MOUNT,76858),
            (self.RAW,self.MOUNT,76859),
            (self.RAW,self.MOUNT.replace('39 1','40 1'),76858),
            (self.RAW,self.MOUNT+self.MOUNT,76858),
            (self.RAW,self.MOUNT.replace('0:33','0:34'),76858),
            (self.RAW,self.MOUNT.replace('0:33','00:21'),76858),
            (self.RAW,self.MOUNT.replace('0:33','0:0x21'),76858),
            (self.RAW.replace('00:21:76858','00:21:76859'),self.MOUNT,76858),
            (self.RAW.replace('00:21:76858','00:21:12c3a'),self.MOUNT,76858),
            (self.RAW.replace('1: FLOCK','1: -> FLOCK'),self.MOUNT,76858),
            (self.RAW.replace('WRITE','READ'),self.MOUNT,76858),
            (self.RAW.replace('0 EOF','1 EOF'),self.MOUNT,76858),
            (self.RAW+self.RAW.splitlines()[-1]+'\n',self.MOUNT,76858),
        ]
        for raw,mount,inode in cases:
            with self.subTest(raw_sha=s.sha(raw.encode()),mount_sha=s.sha(mount.encode())),self.assertRaises(s.Inconclusive):
                s.lock_mount_identity(raw,mount,inode)

if __name__ == '__main__':
    unittest.main()
