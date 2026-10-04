#!/usr/bin/env python3
"""Prove the installed read-only owner view in a sealed exhausted-budget VM.

Only private lab evidence/credentials and a bounded owner-view process are added.
Normal services, product files, observations and recovery policy are not changed.
"""
import argparse
import fcntl
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pty
import re
import select
import signal
import subprocess
import sys
import termios
import time
import urllib.error
import urllib.request
spec=importlib.util.spec_from_file_location('owner_view_budget',Path(__file__).with_name('guest_dispatch_budget_result.py'))
r=importlib.util.module_from_spec(spec);sys.modules[spec.name]=r;spec.loader.exec_module(r)
b=r.f.boot


def services():
    result={}
    for name in ('panel','agent'):
        cp=b.run(['systemctl','show','celikpanel-'+name+'.service','-p','ActiveState','-p','MainPID'])
        if cp.returncode:raise ValueError('coordinator state unavailable')
        value=dict(line.split('=',1) for line in cp.stdout.decode().splitlines())
        if value.get('MainPID')!='0' or value.get('ActiveState') not in ('inactive','failed'):
            raise ValueError('normal coordinator is not stopped')
        result[name]=value
    return result


def request(token=None,origin=None):
    headers={}
    if token is not None:headers['Authorization']='Bearer '+token
    if origin is not None:headers['Origin']=origin
    req=urllib.request.Request('http://127.0.0.1:2084/status',headers=headers)
    try:
        with urllib.request.urlopen(req,timeout=3) as response:
            return response.status,response.read(16385)
    except urllib.error.HTTPError as exc:return exc.code,exc.read(16385)


def run(operation):
    before=r.sample(operation,'exhausted');normal=services()
    selection=b.record(r.f.fault.private_read(Path('/var/lib/celikpanel-release-state/recovery-runtime.v1')),('format','runtime'))
    runtime=selection['runtime'];proof=r.f.fault.verify_runtime(runtime)
    binary=r.f.fault.RUNTIME_ROOT/runtime/'bin/recovery'
    expected=hashlib.sha256(binary.read_bytes()).hexdigest()
    # The installed owner entry must already be this verified kit's CLI.
    entry=Path('/usr/libexec/celikpanel/recovery')
    if hashlib.sha256(entry.read_bytes()).hexdigest()!=expected:raise ValueError('installed owner entry differs from selected kit')
    b.once(b.ROOT/('owner-view-'+operation+'.admitted.json'),b.encoded({'before':before,'services':normal,'runtime':proof,'entry_sha256':expected}))
    master,slave=pty.openpty()
    def child():
        os.setsid();fcntl.ioctl(slave,termios.TIOCSCTTY,0)
    process=subprocess.Popen([str(entry),'view','--request-id',operation,'--port','2084'],stdin=slave,stdout=subprocess.PIPE,stderr=subprocess.PIPE,
                             preexec_fn=child,pass_fds=(slave,),env=b.ENV,cwd=b.ROOT)
    os.close(slave);raw=b'';end=time.monotonic()+8;success=False
    try:
        match=None
        while time.monotonic()<end and process.poll() is None:
            if select.select([master],[],[],.1)[0]:raw+=os.read(master,8192)
            if len(raw)>16384:raise ValueError('unexpected terminal volume')
            match=re.search(rb'(?m)^([0-9a-f]{64})\r?$',raw)
            if match:break
        if not match:raise ValueError('no controlling-terminal access code')
        token=match[1].decode()
        running=hashlib.sha256(Path('/proc/'+str(process.pid)+'/exe').read_bytes()).hexdigest()
        if running!=expected:raise ValueError('different owner view executable')
        anonymous,_=request();wrong,_=request('0'*64);foreign,_=request(token,'https://foreign.invalid')
        code,body=request(token);value=b.probe.strict_object(body)
        status=value.get('status',{})
        if (anonymous!=401 or wrong!=401 or foreign!=403 or code!=200 or status.get('request_id')!=operation
                or status.get('observation')!='known' or status.get('phase')!='recovery_required'
                or status.get('automatic_recovery')!='paused_retry_limit'):
            raise ValueError('authenticated native view result differs')
        # Test credential only, root-private, never part of public acceptance.
        b.once(b.ROOT/('owner-view-'+operation+'.credential'),token.encode())
        b.once(b.ROOT/('owner-view-'+operation+'.ready.json'),b.encoded({'schema':'celikpanel/native-owner-view/v1','identity':before['identity'],
            'operation_id':operation,'boot_id':b.boot_id(),'services':services(),'entry_sha256':expected,'running_sha256':running,
            'runtime_sha256':runtime,'anonymous_http':anonymous,'wrong_code_http':wrong,'foreign_origin_http':foreign,'authorized_http':code,'response':value}))
        stop=b.ROOT/('owner-view-'+operation+'.stop')
        end=time.monotonic()+180
        while time.monotonic()<end:
            if process.poll() is not None:raise ValueError('view exited before browser completed')
            if stop.exists():
                if b.read(stop)!=b'stop\n':raise ValueError('wrong stop authorization')
                success=True;break
            time.sleep(.2)
        if not success:raise ValueError('browser did not complete bounded capture')
        process.send_signal(signal.SIGTERM);out,err=process.communicate(timeout=5)
        if process.returncode!=0 or out or err:raise ValueError('owner exit or output differed')
        b.once(b.ROOT/('owner-view-'+operation+'.closed.json'),b.encoded({'operation_id':operation,'boot_id':b.boot_id(),
            'exit_code':process.returncode,'stdout_bytes':len(out),'stderr_bytes':len(err),'services':services(),'automatic_receipts':r.sample(operation,'exhausted')['receipts']}))
    finally:
        if process.poll() is None:process.kill();process.wait()
        os.close(master)
    return {'operation_id':operation,'result':'native-view-closed-without-mutation'}

if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps(run(a.operation_id),sort_keys=True))
