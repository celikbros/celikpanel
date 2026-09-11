import subprocess, json, concurrent.futures, hashlib, sys
from pathlib import Path

ROOT=Path(sys.argv[1] if len(sys.argv)>1 else '/var/tmp/cp-setup-profiles-20260911')
assert str(ROOT) in ('/var/tmp/cp-setup-profiles-20260911','/var/tmp/cp-setup-profiles-20260911-v2','/var/tmp/cp-setup-profiles-20260911-v3')
SOURCE=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
nodes=json.loads((ROOT/'plan.json').read_text())
assert set(nodes)=={'web','application','webmail'}
options=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts']
guest=r'''
import os,json,ssl,urllib.request,urllib.error,time,hashlib,sys,subprocess
from pathlib import Path
profile=sys.argv[1]
assert profile in ('web','application','webmail')
folder=Path('/var/lib/celikpanel-profile-vm')
assert (folder/'fixture').read_text()=='fresh-setup-profile-acceptance-v1\n'
assert not Path('/opt/celikpanel/bin/panel').exists()
url='https://panel.'+profile+'.setup.test:2083'
# The fresh panel intentionally starts with its local self-signed certificate.
# Production completion independently verifies system trust on the actual
# listener. The final read below switches this driver to the system trust store.
context=ssl._create_unverified_context()
def api(path,body=None,method=None):
    token=(folder/'session').read_text()
    req=urllib.request.Request(url+path,data=None if body is None else json.dumps(body).encode(),method=method)
    req.add_header('Cookie','celikpanel_session='+token)
    req.add_header('Origin',url)
    req.add_header('Content-Type','application/json')
    with urllib.request.urlopen(req,context=context,timeout=40) as response:
        return json.load(response)
def saved(name,value):
    (folder/(name+'.json')).write_text(json.dumps(value,indent=2))
for attempt in range(90):
    try: state=api('/api/v1/setup');break
    except (OSError,urllib.error.URLError): time.sleep(2)
else: raise RuntimeError('fixture HTTPS daemon did not become ready')
if state['status'] not in ('new','draft'):
    raise RuntimeError('requires an unused profile fixture: '+state['status'])
draft={'purpose':'web_mail' if profile=='webmail' else profile,'panel_domain':'panel.'+profile+'.setup.test','dns_mode':'external','database':'mariadb' if profile!='application' else ''}
if profile=='application':draft['node_version']='22.14.0'
if profile=='webmail':draft['mail_hostname']='mail.webmail.setup.test'
state=api('/api/v1/setup',{'revision':state['revision'],'draft':draft},'PUT');saved('draft',state)
plan=api('/api/v1/setup/plan',{'revision':state['revision']});saved('review',plan)
if not plan['can_start']:raise RuntimeError('review blockers: '+str(plan['blockers']))
assert plan['purpose']==draft['purpose'] and plan['draft']['dns_mode']=='external'
request_id=hashlib.sha256(('disposable-purpose-v1:'+profile).encode()).hexdigest()[:32]
start={'plan_id':plan['id'],'confirmed':True,'request_id':request_id}
execution=api('/api/v1/setup/start',start);saved('start',execution)
# Exact explicit start retry must return the same durable execution even if a
# preceding response was lost; it cannot create another series of child jobs.
repeated=api('/api/v1/setup/start',start)
assert repeated['id']==execution['id'] and repeated['plan_id']==plan['id']
saved('start-replay',repeated)
deadline=time.monotonic()+2700
previous=None
while time.monotonic()<deadline:
    try: current=api('/api/v1/setup/operation?request_id='+request_id)
    except (OSError,urllib.error.URLError):time.sleep(3);continue
    assert current and current['id']==execution['id'] and current['plan_id']==plan['id']
    saved('execution',current)
    status=(current['status'],current['phase'],json.dumps(current.get('error')))
    if status!=previous:print(profile,status,flush=True);previous=status
    if current['status']=='failed':raise RuntimeError('purpose failed: '+json.dumps(current))
    if current['status']=='succeeded':break
    time.sleep(3)
else:
    saved('readiness-timeout',api('/api/v1/setup?check=1'))
    raise RuntimeError('purpose execution did not finish')
context=ssl.create_default_context()
state=api('/api/v1/setup?check=1');saved('ready',state)
assert state['status']=='ready' and state['checks'] and all(check['state']=='ready' for check in state['checks'])
print('PROFILE_EXECUTION_PASSED '+profile,flush=True)
'''

def drive(item):
    name,row=item;port=str(row['port'])
    pid=int((ROOT/name/'qemu.pid').read_text())
    assert b'cp-profile65-'+name.encode() in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    subprocess.run(['scp',*options,'-P',port,str(ROOT/'panel-test'),'celik@127.0.0.1:/tmp/profile-panel-test'],check=True,capture_output=True)
    subprocess.run(['scp',*options,'-P',port,str(SOURCE/'deploy/panel-tls-snapshot.sh'),'celik@127.0.0.1:/tmp/panel-tls-snapshot.sh'],check=True,capture_output=True)
    script=f'''set -euo pipefail
test "$(cat /etc/hostname)" = profile65-{name}
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel/bin/panel
test ! -e /var/lib/celikpanel/celikpanel.db
install -o root -g celikpanel -m 0750 /tmp/profile-panel-test /var/lib/celikpanel-profile-vm/panel-test
cat >/etc/systemd/system/celikpanel-panel.service <<'EOF'
[Unit]
Description=Disposable CelikPanel purpose acceptance daemon
After=celikpanel-agent.service
[Service]
User=celikpanel
Group=celikpanel
UMask=0077
WorkingDirectory=/opt/celikpanel
Environment=CELIKPANEL_SETUP_PROFILE_VM={name}
Environment=CELIKPANEL_DATA_DIR=/var/lib/celikpanel
Environment=CELIKPANEL_TLS=1
Environment=CELIKPANEL_TLS_DIR=/var/lib/celikpanel/tls
Environment=CELIKPANEL_LISTEN=:2083
Environment=CELIKPANEL_SERVER_IP=10.0.2.15
Environment=CELIKPANEL_AGENT_SOCKET=/run/celikpanel/agent.sock
Environment=CELIKPANEL_AGENT_TOKEN_FILE=/etc/celikpanel/agent.token
ExecStart=/var/lib/celikpanel-profile-vm/panel-test -test.run ^TestServerSetupDisposableProfileDaemon$ -test.v -test.timeout 2h
Restart=on-failure
RestartSec=3
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/celikpanel /var/lib/celikpanel-profile-vm
[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl start celikpanel-panel.service
# The fixture uses the same documented installer normalization after first
# self-signed generation, before admitting any setup operation.
for attempt in $(seq 1 90); do
  if test -s /var/lib/celikpanel/tls/panel.crt && test -s /var/lib/celikpanel/tls/panel.key; then break; fi
  sleep 1
done
test -s /var/lib/celikpanel/tls/panel.crt
test -s /var/lib/celikpanel/tls/panel.key
systemctl stop celikpanel-panel.service
source /tmp/panel-tls-snapshot.sh
panel_tls_normalize_legacy_self_signed /var/lib/celikpanel/tls "$(id -u celikpanel)" "$(id -g celikpanel)"
systemctl start celikpanel-panel.service
'''
    result=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo bash -s'],input=script,text=True,capture_output=True,timeout=180)
    (ROOT/name/'daemon-install.log').write_text(result.stdout+result.stderr)
    assert result.returncode==0,result.stdout+result.stderr
    print(name+' daemon staged',flush=True)
    with (ROOT/name/'execution-driver.log').open('w') as log:
        result=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo python3 - '+name],input=guest,text=True,stdout=log,stderr=subprocess.STDOUT,timeout=3000)
    evidence=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo journalctl -u celikpanel-panel.service -u celikpanel-agent.service -u celikpanel-fixture-acme.service --no-pager'],capture_output=True,timeout=45)
    (ROOT/name/'execution-journal.log').write_bytes(evidence.stdout+evidence.stderr)
    snapshot=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1',"sudo python3 -c 'import json,pathlib; p=pathlib.Path(\"/var/lib/celikpanel-profile-vm\"); print(json.dumps({f.name:json.loads(f.read_text()) for f in p.glob(\"*.json\") if f.name not in (\"pebble.json\",)}))'"],capture_output=True,timeout=45)
    (ROOT/name/'execution-evidence.json').write_bytes(snapshot.stdout)
    print(name+' driver exit '+str(result.returncode),flush=True)
    if result.returncode:print((ROOT/name/'execution-driver.log').read_text()[-5000:],flush=True)
    return result.returncode

(ROOT/'panel-test.sha256').write_text(hashlib.sha256((ROOT/'panel-test').read_bytes()).hexdigest()+'\n')
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:codes=list(pool.map(drive,nodes.items()))
if any(codes):raise SystemExit(1)
