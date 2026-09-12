"""Read real wire/SQL evidence, then delete only the fixture's exact domain."""
import json
import os
import subprocess
import sys
from pathlib import Path

ROOT=Path(os.environ.get('CELIKPANEL_SECONDARY_HOSTING_VM_ROOT', '/var/tmp/cp-secondary-hosting-20260912'))
assert ROOT.name in ('cp-secondary-hosting-20260912', 'cp-secondary-hosting-20260912-v2')
nodes=json.loads((ROOT/'plan.json').read_text())
label='probe' if len(sys.argv)>1 and sys.argv[1]=='probe' else 'final'
continuation=json.loads((ROOT/('probe-continuation.json' if label=='probe' else 'continuation.json')).read_text())
domain=continuation['domain'];identifier=continuation['domain_id']
assert domain in ('probe.secondaryfixture.test','customer.secondaryfixture.test') and isinstance(identifier,int) and identifier>0
options=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts']

def execute(name,remote,data):
 row=nodes[name];folder=ROOT/name;pid=int((folder/'qemu.pid').read_text());command=Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
 assert row['qemu_name'].encode() in command and any(('file='+str(folder/'overlay.qcow2')).encode() in arg for arg in command)
 result=subprocess.run(['ssh',*options,'-p',str(row['port']),'celik@127.0.0.1',remote],input=data,capture_output=True,timeout=180)
 assert result.returncode==0,result.stdout.decode()+result.stderr.decode()
 return result.stdout

guest=r'''
from pathlib import Path
import json,sqlite3,subprocess,sys
domain=sys.argv[1];stage=sys.argv[2];profile=sys.argv[3]
assert domain in ('probe.secondaryfixture.test','customer.secondaryfixture.test')
assert Path('/var/lib/celikpanel-profile-vm/secondary-hosting-fixture').read_text()=='secondary-hosting-20260912\n'
assert not Path('/opt/celikpanel/bin/panel').exists()
database=sqlite3.connect('file:/var/lib/celikpanel/celikpanel.db?mode=ro',uri=True)
out={'stage':stage,'profile':profile,'domain':domain}
out['panel_zones']=database.execute('SELECT name,type FROM pdns_domains WHERE name=?',(domain,)).fetchall()
out['hosting']=database.execute('SELECT id,name,dns_management,dns_remote_connection_id FROM domains WHERE name=?',(domain,)).fetchall()
out['local_writes']=database.execute('SELECT zone_name FROM dns_zone_sync_state WHERE zone_name=?',(domain,)).fetchall()
out['engine']=database.execute('SELECT active_engine,active_epoch,revision FROM dns_engine_state').fetchall()
out['pair']=database.execute('SELECT pair_role,local_ip,local_ns,peer_ip,peer_ns FROM dns_bind_pair_state').fetchall()
if profile=='dnssecondary':
 runtime=sqlite3.connect('file:/var/lib/powerdns/pdns.sqlite3?mode=ro',uri=True)
 out['runtime_zones']=runtime.execute('SELECT name,type,master FROM domains WHERE name=?',(domain,)).fetchall()
 out['runtime_records']=runtime.execute('SELECT r.name,r.type,r.content FROM records r JOIN domains d ON d.id=r.domain_id WHERE d.name=? ORDER BY r.name,r.type,r.content',(domain,)).fetchall()
out['wire']={}
for host in ('192.0.2.10','192.0.2.20'):
 for protocol in ('+tcp','+notcp'):
  for name,kind in [(domain,'SOA'),(domain,'A'),(domain,'MX'),(domain,'TXT'),('mail.'+domain,'A'),('_dmarc.'+domain,'TXT')]:
   args=['dig','@'+host,name,kind,protocol,'+norecurse','+noall','+comments','+answer','+time=3','+tries=1']
   result=subprocess.run(args,capture_output=True,text=True,timeout=8)
   out['wire'][' '.join([host,protocol,name,kind])]={'code':result.returncode,'stdout':result.stdout,'stderr':result.stderr}
if stage=='before':
 result=subprocess.run(['curl','--silent','--show-error','--max-time','10','--resolve',domain+':80:192.0.2.20','--output','/dev/null','--write-out','%{http_code}','http://'+domain+'/'],capture_output=True,text=True)
 out['http']={'code':result.returncode,'status':result.stdout}
print(json.dumps(out))
'''

def evidence(stage):
 out={}
 for name in nodes:
  out[name]=json.loads(execute(name,'sudo python3 - '+domain+' '+stage+' '+name,guest.encode()))
 (ROOT/(label+'-'+stage+'-proof.json')).write_text(json.dumps(out,indent=2))
 return out

before=evidence('before')
secondary=before['dnssecondary'];primary=before['dnsprimary']
assert not secondary['panel_zones'] and not secondary['local_writes']
assert secondary['hosting'][0][1:3]==[domain,'existing']
assert secondary['runtime_zones']==[[domain,'SLAVE','192.0.2.10']],secondary['runtime_zones']
assert primary['panel_zones']==[[domain,'MASTER']] and not primary['hosting']
assert secondary['pair'][0][0]=='secondary' and primary['pair'][0][0]=='primary'
assert secondary['http']=={'code':0,'status':'200'},secondary['http']
serials=set()
for key,result in secondary['wire'].items():
 text=result['stdout'];assert result['code']==0 and 'status: NOERROR' in text and ' aa;' in text,(key,result)
 lines=[line for line in text.splitlines() if line and not line.startswith(';')]
 assert lines,(key,result)
 kind=key.split()[-1]
 if kind=='SOA':serials.add(lines[0].split()[6])
 if kind=='A':assert all(line.split()[-1]=='192.0.2.20' for line in lines),(key,result)
 if kind=='MX':assert any('mail.'+domain+'.' in line for line in lines),(key,result)
 if kind=='TXT' and '_dmarc.' in key:assert 'v=DMARC1' in text
 elif kind=='TXT':assert 'secondary-hosting-validation' in text and 'v=spf1' in text
assert len(serials)==1,serials
print('REAL_MIXED_DNS_TRANSFER_AND_SECONDARY_HOSTING_PASSED',flush=True)
request={'profile':'dnssecondary','path':f'/api/v1/domains/{identifier}','body':None,'method':'DELETE','trusted':True}
deleted=json.loads(execute('dnssecondary','sudo python3 /var/lib/celikpanel-profile-vm/driver-api.py',json.dumps(request).encode()))
(ROOT/(label+'-delete-result.json')).write_text(json.dumps(deleted,indent=2))
after=evidence('after')
assert not after['dnsprimary']['panel_zones']
assert not after['dnssecondary']['runtime_zones'] and not after['dnssecondary']['hosting'] and not after['dnssecondary']['panel_zones']
assert before['dnssecondary']['engine']==after['dnssecondary']['engine'] and before['dnssecondary']['pair']==after['dnssecondary']['pair']
for key,result in after['dnssecondary']['wire'].items():
 assert not [line for line in result['stdout'].splitlines() if line and not line.startswith(';')],(key,result)
print('EXACT_REMOTE_DELETE_PRESERVES_SECONDARY_ENGINE_PASSED',flush=True)
