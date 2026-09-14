#!/usr/bin/env python3
"""Stage and observe an unpublished artifact only in a registered disposable guest."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import tarfile

HERE=Path(__file__).resolve().parent

def module(name,filename):
    spec=importlib.util.spec_from_file_location(name,HERE/filename)
    value=importlib.util.module_from_spec(spec);sys.modules[name]=value;spec.loader.exec_module(value);return value

archive_tools=module('local_candidate_archive','candidate_archive.py')
kill=module('local_candidate_kill','guest_update_kill.py')
shared=kill.shared;probe=shared.probe
SCHEMA='celikpanel/local-candidate-intent/v1'
PRIVATE=shared.PRIVATE_ROOT
RELEASES=Path('/var/backups/celikpanel/releases')
INSTALLED={'deploy/systemd/celikpanel-agent.service':'/etc/systemd/system/celikpanel-agent.service','deploy/systemd/celikpanel-panel.service':'/etc/systemd/system/celikpanel-panel.service','deploy/release-transaction-start-guard.sh':'/usr/libexec/celikpanel/release-transaction-start-guard','deploy/release-recovery-runner.sh':'/usr/libexec/celikpanel/release-recovery'}


def private_json(path):
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    with os.fdopen(fd,'rb') as stream:
        info=os.fstat(stream.fileno())
        if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or info.st_nlink!=1 or stat.S_IMODE(info.st_mode)!=0o600 or info.st_size>4*1024*1024:raise ValueError('unsafe private candidate evidence')
        return json.load(stream)


def save_private(path,value):
    fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
    with os.fdopen(fd,'w') as stream:
        json.dump(value,stream,sort_keys=True);stream.write('\n');stream.flush();os.fsync(stream.fileno())


def names(operation):
    if not shared.HEX32.fullmatch(operation):raise ValueError('invalid local operation identity')
    return {'worker':'celikpanel-lab-local-update-'+operation+'.service','fault':'celikpanel-lab-local-kill-'+operation+'.service','plan':PRIVATE/('local-candidate-'+operation+'.json'),'proof':PRIVATE/('local-candidate-stage-'+operation+'.json'),'events':PRIVATE/('local-update-kill-'+operation+'.jsonl')}


def validate_plan(plan,identity,operation):
    if (plan.get('schema')!=SCHEMA or plan.get('identity')!=identity or plan.get('operation_id')!=operation
            or plan.get('provenance')!='unpublished-local-build-not-signed-agent-admission'
            or plan.get('boundary') not in ('candidate-installed','require-unit-reload','completion-database-verified')):raise ValueError('local candidate intent differs from exact disposable guest')
    if plan.get('boundary')=='completion-database-verified' and ('recovery_fault' in plan or plan.get('candidate_data_fault')!='quarantine-fixed-three'):
        raise ValueError('completion fault requires the exact data-loss plan and no second recovery fault')
    if 'recovery_fault' in plan:
        value=plan['recovery_fault']
        if (not isinstance(value,dict) or set(value)!={'action','checkpoint'} or value['action'] not in ('kill','reboot')
                or value['checkpoint'] not in ('restore_admitted','payload_restored','units_reloaded','runtime_verified','schedulers_restored')):
            raise ValueError('invalid opted-in recovery fault')
    if 'candidate_data_fault' in plan and plan['candidate_data_fault'] != 'quarantine-fixed-three':
        raise ValueError('invalid opted-in retained candidate data fault')
    candidate=plan['candidate']
    expected=str(RELEASES/('.download.lab-'+operation)/'payload'/candidate['root_name'])
    if plan.get('source_root')!=expected or plan.get('archive_path')!=str(PRIVATE/('local-candidate-'+operation+'.tar.gz')):raise ValueError('candidate staging boundary differs')


def trusted_chain(path):
    for item in (path,*path.parents):
        info=item.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or stat.S_IMODE(info.st_mode)&0o022:raise ValueError('candidate directory ancestry is not trusted')


def verify_tree(root,candidate,tick=lambda:None):
    trusted_chain(root)
    if stat.S_IMODE(root.stat().st_mode)!=0o700:raise ValueError('candidate root is not root-only')
    actual={}
    for parent,dirs,files in os.walk(root,followlinks=False):
        for name in dirs:
            path=Path(parent)/name;info=path.lstat()
            if not stat.S_ISDIR(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or stat.S_IMODE(info.st_mode)&0o022:raise ValueError('unsafe candidate directory')
        for name in files:
            path=Path(parent)/name;info=path.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or info.st_nlink!=1 or stat.S_IMODE(info.st_mode)&0o022:raise ValueError('unsafe candidate file')
            actual[path.relative_to(root).as_posix()]=shared.digest_file(path,tick)
    if actual!=candidate['files']:raise ValueError('retained candidate inventory differs from sealed artifact')
    return {'root':str(root),'manifest_sha256':candidate['manifest_sha256'],'verified_files':len(actual),'native_entrypoint_sha256':actual['update.sh'],'rollback_sha256':actual['rollback.sh'],'unit_and_helper_sha256':{name:actual[name] for name in actual if name.startswith('deploy/') and (name.endswith('.sh') or name.endswith('.service') or name.endswith('.timer'))}}


def stage(plan,paths):
    candidate=archive_tools.inspect_archive(Path(plan['archive_path']),plan['candidate']['archive_sha256'])
    if candidate!=plan['candidate']:raise ValueError('guest candidate artifact differs from host-sealed metadata')
    if shared.transaction() is not None:raise ValueError('existing transaction blocks fresh candidate preparation')
    for unit in ('celikpanel-agent.service','celikpanel-panel.service'):
        if shared.unit_state(unit)['ActiveState']!='active':raise ValueError('genuine baseline service is not active')
    for name in ('agent','panel'):
        if shared.digest_file(Path('/opt/celikpanel/bin')/name)!=plan['baseline_artifacts'][name]:raise ValueError('current binary differs from genuine baseline')
    trusted_chain(RELEASES)
    parent=RELEASES/('.download.lab-'+plan['operation_id'])
    parent.mkdir(mode=0o700);(parent/'payload').mkdir(mode=0o700)
    root=Path(plan['source_root']);root.mkdir(mode=0o700)
    with tarfile.open(plan['archive_path'],'r:gz') as bundle:
        for member in bundle:
            relative=archive_tools.member_path(member.name,candidate['root_name'])
            if not relative:continue
            path=root/relative
            if member.isdir():path.mkdir(mode=0o755,parents=True,exist_ok=True);continue
            if not member.isfile():raise ValueError('special archive member')
            path.parent.mkdir(mode=0o755,parents=True,exist_ok=True)
            fd=os.open(path,os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600)
            with os.fdopen(fd,'wb') as target,bundle.extractfile(member) as source:
                for chunk in iter(lambda:source.read(1048576),b''):target.write(chunk)
                target.flush();os.fsync(target.fileno())
            path.chmod(0o755 if member.mode&0o111 else 0o644)
    proof=verify_tree(root,candidate)
    proof.update({'schema':'celikpanel/local-candidate-stage/v1','identity':plan['identity'],'operation_id':plan['operation_id'],'bash_sha256':shared.digest_file(Path('/bin/bash').resolve())})
    for parent,dirs,files in os.walk(root,topdown=False):
        fd=os.open(parent,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(fd)
        finally:os.close(fd)
    for directory in (root.parent,root.parent.parent,RELEASES):
        fd=os.open(directory,os.O_RDONLY|os.O_DIRECTORY)
        try:os.fsync(fd)
        finally:os.close(fd)
    save_private(paths['proof'],proof)
    return proof


class LocalNative(kill.Native):
    def __init__(self,args,plan,proof):
        self.args=args;self.plan=plan;self.stage_proof=proof;self.unit=names(args.operation_id)['worker']
        self.args.candidate_agent=plan['candidate']['files']['bin/agent'];self.args.candidate_panel=plan['candidate']['files']['bin/panel']
        option=plan.get('recovery_fault',{})
        self.args.recovery_action=option.get('action');self.args.recovery_checkpoint=option.get('checkpoint')
        self.args.candidate_data_fault=plan.get('candidate_data_fault')

    def unit_reload(self):
        result={}
        for unit in ('celikpanel-agent.service','celikpanel-panel.service'):
            observed=subprocess.run(['/usr/bin/systemctl','show',unit,'-p','NeedDaemonReload','--value'],capture_output=True,text=True,timeout=2,check=True,env=kill.ENV).stdout.strip()
            if observed not in ('yes','no'):raise ValueError('coordinator reload state unavailable')
            result[unit]=observed
        return result

    def installed(self,tick=lambda:None):
        binaries=super().installed(tick)
        if binaries is None:return None
        for source,target in INSTALLED.items():
            try:digest=shared.digest_file(Path(target),tick)
            except (FileNotFoundError,probe.ProbeError):return None
            if digest!=self.plan['candidate']['files'][source]:return None
        if self.plan['boundary']=='require-unit-reload' and self.unit_reload()['celikpanel-agent.service']!='yes':return None
        return binaries

    def worker_identity(self):
        props=self.properties();pid=props.get('MainPID','0')
        if (props.get('Id')!=self.unit or props.get('LoadState')!='loaded' or props.get('ActiveState')!='active' or not pid.isdigit() or int(pid)<=1
                or props.get('ControlGroup')!='/system.slice/'+self.unit or not shared.HEX32.fullmatch(props.get('InvocationID',''))):raise kill.MissedCheckpoint('exact-local-worker-not-active')
        proc=Path('/proc')/pid;start=kill.process_start((proc/'stat').read_text())
        expected=b'/bin/bash\0'+(self.plan['source_root']+'/bootstrap-prebuilt-update.sh').encode()+b'\0--normal\0'
        if (proc/'cmdline').read_bytes()!=expected or (proc/'cgroup').read_text()!='0::'+props['ControlGroup']+'\n':raise kill.MissedCheckpoint('local-worker-command-or-cgroup-differs')
        digest=hashlib.sha256()
        with (proc/'exe').open('rb') as stream:
            info=os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_size>32*1024*1024:raise ValueError('unsafe local worker executable')
            for chunk in iter(lambda:stream.read(1048576),b''):digest.update(chunk)
        if digest.hexdigest()!=self.stage_proof['bash_sha256']:raise kill.MissedCheckpoint('local-worker-bash-executable-differs')
        after=self.properties()
        if any(after.get(key)!=props[key] for key in ('Id','MainPID','InvocationID','ControlGroup','ActiveState')) or kill.process_start((proc/'stat').read_text())!=start:raise kill.MissedCheckpoint('local-worker-identity-changed')
        return {'unit':self.unit,'pid':int(pid),'start_ticks':start,'invocation_id':props['InvocationID'],'cgroup':props['ControlGroup'],'running_executable_sha256':digest.hexdigest(),'bootstrap_sha256':self.plan['candidate']['files']['bootstrap-prebuilt-update.sh'],'entrypoint':'unpublished-local-prebuilt-bootstrap','boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip()}

    def full_proof(self,snapshot,tick):
        proof=super().full_proof(snapshot,tick)
        candidate=self.plan['candidate']
        prefix=candidate['commit'][:12]+'-'
        roots=[p for p in RELEASES.iterdir() if p.name.startswith(prefix) and shared.re.fullmatch(r'[0-9a-f]{12}-[0-9a-f]{24}',p.name)]
        if len(roots)!=1:raise kill.MissedCheckpoint('exact-retained-local-release-is-ambiguous')
        retained=verify_tree(roots[0],candidate,tick)
        if ('-to-'+candidate['commit']+'-') not in snapshot:raise kill.MissedCheckpoint('local-snapshot-target-commit-differs')
        proof.update({'retained_candidate':retained,'installed_unit_helpers':{target:shared.digest_file(Path(target),tick) for target in INSTALLED.values()},'coordinator_need_daemon_reload':self.unit_reload(),'provenance':self.plan['provenance']})
        if any(proof['installed_unit_helpers'][target]!=candidate['files'][source] for source,target in INSTALLED.items()):raise kill.MissedCheckpoint('installed-candidate-unit-or-helper-changed')
        return proof

    def candidate_data_fault(self, identity, proof, tick):
        helper=module('local_candidate_data_fault','guest_candidate_data_fault.py')
        return helper.apply(self.args,self.plan,proof,identity,tick,self.revalidate)


def run_fault(args,plan,paths):
    proof=private_json(paths['proof'])
    if proof.get('identity')!=plan['identity'] or proof.get('operation_id')!=plan['operation_id']:raise ValueError('candidate stage proof identity differs')
    fd=os.open(paths['events'],os.O_WRONLY|os.O_CREAT|os.O_EXCL|os.O_NOFOLLOW,0o600);stopped=False
    def stop(signum,frame):
        nonlocal stopped
        stopped=True
    previous={sig:signal.signal(sig,stop) for sig in (signal.SIGINT,signal.SIGTERM)}
    try:
        with os.fdopen(fd,'w') as stream:
            def emit(event,**fields):
                stream.write(json.dumps({'schema':kill.SCHEMA,'event':event,'at':probe.utc_now(),'identity':plan['identity'],**fields},sort_keys=True)+'\n');stream.flush();os.fsync(stream.fileno())
            native=LocalNative(args,plan,proof)
            if plan['boundary']=='completion-database-verified':
                forward=module('local_forward_completion','guest_forward_completion_fault.py')
                return forward.run_fault(args,emit,forward.CompletionNative(native),interrupted=lambda:stopped)
            return kill.run_kill(args,emit,native,interrupted=lambda:stopped)
    finally:
        for sig,handler in previous.items():signal.signal(sig,handler)


def collect(plan,paths):
    raw=b''
    try:
        fd=os.open(paths['events'],os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    except FileNotFoundError:pass
    else:
        with os.fdopen(fd,'rb') as stream:
            info=os.fstat(stream.fileno())
            if not stat.S_ISREG(info.st_mode) or info.st_uid!=0 or info.st_nlink!=1 or stat.S_IMODE(info.st_mode)!=0o600 or info.st_size>1048576:raise ValueError('unsafe local fault log')
            raw=stream.read(1048577)
            if len(raw)>1048576:raise ValueError('local fault log exceeds bound')
    states={}
    for unit in (paths['worker'],paths['fault'],'celikpanel-release-recovery.service','celikpanel-agent.service','celikpanel-panel.service'):
        result=subprocess.run(['/usr/bin/systemctl','show',unit,'-p','Id','-p','LoadState','-p','ActiveState','-p','SubState','-p','MainPID','-p','Result','-p','ExecMainStatus','-p','OnFailure'],capture_output=True,text=True,timeout=5,env=kill.ENV)
        values=dict(line.split('=',1) for line in result.stdout.splitlines() if '=' in line)
        if result.returncode not in (0,1) or (result.returncode==1 and values.get('LoadState')!='not-found'):raise ValueError('local unit observation unavailable')
        states[unit]=values
    import base64
    data_fault={}
    if plan.get('candidate_data_fault') is not None:
        helper=module('local_candidate_data_collect','guest_candidate_data_fault.py')
        try:data_fault=helper.collect(plan['operation_id'])
        except (ValueError,OSError):data_fault={'status':'unavailable','reason':'candidate-data-evidence-unavailable'}
    return {'candidate_data_fault':data_fault,'schema':'celikpanel/local-candidate-collection/v1','identity':plan['identity'],'operation_id':plan['operation_id'],'fault_jsonl_base64':base64.b64encode(raw).decode(),'states':states,'transaction':shared.transaction()}


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    for name in ('lab-nonce','vm-uuid','cell-id','node','operation-id'):parser.add_argument('--'+name,required=True)
    parser.add_argument('--mode',choices=('stage','fault','collect'),required=True)
    args=parser.parse_args(argv)
    identity=probe.guard_guest(args);paths=names(args.operation_id)
    info=PRIVATE.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid!=0 or info.st_gid!=0 or stat.S_IMODE(info.st_mode)!=0o700:raise ValueError('unsafe private fixture root')
    plan=private_json(paths['plan']);validate_plan(plan,identity,args.operation_id)
    if args.mode=='fault':return run_fault(args,plan,paths)
    value=stage(plan,paths) if args.mode=='stage' else collect(plan,paths)
    print(json.dumps(value,sort_keys=True));return 0


if __name__=='__main__':
    try:sys.exit(main())
    except (ValueError,OSError,probe.ProbeError) as exc:
        print('local candidate fixture refused: '+type(exc).__name__,file=sys.stderr);sys.exit(2)
