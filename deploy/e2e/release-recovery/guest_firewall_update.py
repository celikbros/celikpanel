#!/usr/bin/env python3
"""Read-only native update/unit proof; outer controller pins nonce and SSH."""
import argparse
import importlib.util
import json
import os
from pathlib import Path
import re
import subprocess
from datetime import datetime, timezone

spec=importlib.util.spec_from_file_location('firewall_generation',Path(__file__).with_name('guest_firewall_generation.py'))
source=importlib.util.module_from_spec(spec);spec.loader.exec_module(source)

def observe(operation):
    if not re.fullmatch('[a-f0-9]{32}',operation) or os.geteuid()!=0 or Path('/proc/1/comm').read_text().strip()!='systemd':
        raise ValueError('native exact operation required')
    identity=json.loads(Path('/etc/celikpanel-release-recovery-lab').read_text())
    if identity['schema']!='celikpanel-release-recovery-lab/v1' or identity['vm_uuid']!=Path('/sys/class/dmi/id/product_uuid').read_text().strip().lower():
        raise ValueError('not a registered disposable VM')
    units={}
    for name in ('celikpanel-agent.service','celikpanel-panel.service','celikpanel-firewall-restore.service'):
        raw=source.run('systemctl','show',name,'-p','ActiveState','-p','SubState','-p','Result','-p','UnitFileState','-p','FragmentPath')
        units[name]=dict(line.split('=',1) for line in raw.splitlines())
    generations={};root=Path('/usr/libexec/celikpanel/firewall')
    if root.exists():
        for path in sorted(root.iterdir()):
            if not re.fullmatch('[a-f0-9]{64}',path.name):raise ValueError('unexpected retained generation entry')
            generations[path.name]={n:source.digest(path/n) for n in ('restore','runtime.manifest','celikpanel-firewall-restore.service')}
    result=subprocess.run(['/usr/libexec/celikpanel/recovery','status','--request-id',operation,'--json'],capture_output=True,text=True,timeout=15)
    return {'schema':'celikpanel/native-firewall-update/v1','identity':identity,'at':datetime.now(timezone.utc).isoformat(),
            'request_id':operation,'boot_id':Path('/proc/sys/kernel/random/boot_id').read_text().strip(),
            'operation_status':json.loads(result.stdout),'operation_status_exit':result.returncode,
            'units':units,'unit':source.digest('/etc/systemd/system/celikpanel-firewall-restore.service'),
            'policy':source.digest('/etc/celikpanel/firewall.nft'),'generations':generations,
            'binaries':{n:source.digest('/opt/celikpanel/bin/'+n) for n in ('agent','panel')},
            'tables':{n:source.run('/usr/sbin/nft','list','table','inet',n) for n in ('celikpanel_fw','celikpanel_lab_other')},
            'https_http_code':source.run('curl','--insecure','--silent','--show-error','--max-time','10','--output','/dev/null','--write-out','%{http_code}','https://127.0.0.1:2083/'),
            'https_trust':'not claimed; self-signed lab baseline','systemd':source.run('systemctl','--version').splitlines()[0]}

if __name__=='__main__':
    parser=argparse.ArgumentParser();parser.add_argument('--request-id',required=True)
    print(json.dumps(observe(parser.parse_args().request_id),sort_keys=True))
