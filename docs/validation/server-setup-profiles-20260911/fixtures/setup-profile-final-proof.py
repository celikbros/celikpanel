import subprocess,json,concurrent.futures
from pathlib import Path
r=Path('/var/tmp/cp-setup-profiles-20260911-v3');nodes=json.loads((r/'plan.json').read_text())
a=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={r}/ssh-known-hosts']
code='''import json,sqlite3,subprocess,sys,hashlib
from pathlib import Path
profile=sys.argv[1];p=Path('/var/lib/celikpanel-profile-vm')
assert (p/'fixture').read_text()=='fresh-setup-profile-acceptance-v1\\n'
assert not Path('/opt/celikpanel/bin/panel').exists()
c=sqlite3.connect('/var/lib/celikpanel/celikpanel.db')
children=c.execute('select id,status,kind,request_id from service_operations order by id').fetchall()
assert c.execute('select status from server_setup_state where id=1').fetchone()[0]=='ready'
assert all(row[1]=='succeeded' for row in children)
if profile=='webmail':
 old=json.loads((p/'before-mail-dns-recovery.json').read_text())
 assert json.loads(json.dumps(children))==old['children'],'mail DNS proof recovery replayed child operations'
services={}
for unit in ['nginx','php8.4-fpm','mariadb','postfix','dovecot','rspamd','certbot.timer']:
 v=subprocess.run(['systemctl','show','-p','LoadState','-p','ActiveState',unit],text=True,capture_output=True)
 services[unit]=dict(line.split('=',1) for line in v.stdout.splitlines() if '=' in line)
if profile in ('web','application'):
 assert all(services[unit]['LoadState']=='not-found' for unit in ['postfix','dovecot','rspamd'])
if profile=='application':
 assert all(services[unit]['LoadState']=='not-found' for unit in ['php8.4-fpm','mariadb'])
assert services['certbot.timer']['ActiveState']=='active'
result={'profile':profile,'ready':True,'unchanged_successful_children':len(children),'services':services,'agent_sha256':hashlib.sha256(Path('/opt/celikpanel/bin/agent').read_bytes()).hexdigest()}
(p/'final-profile-proof.json').write_text(json.dumps(result,indent=2));print(json.dumps(result,indent=2))
'''
def run(name):
 pid=int((r/name/'qemu.pid').read_text());assert b'cp-profile65-'+name.encode() in Path(f'/proc/{pid}/cmdline').read_bytes().split(bytes([0]))
 v=subprocess.run(['ssh',*a,'-p',str(nodes[name]['port']),'celik@127.0.0.1','sudo python3 - '+name],input=code,text=True,capture_output=True,timeout=40)
 (r/name/'final-profile-proof.log').write_text(v.stdout+v.stderr);print(name,v.returncode,v.stdout+v.stderr);assert v.returncode==0
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:list(pool.map(run,nodes))
