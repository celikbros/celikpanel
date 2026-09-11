import subprocess, json, concurrent.futures, hashlib, sys, time
from pathlib import Path

ROOT=Path('/var/tmp/cp-setup-dns-profile-20260911-v2')
assert ROOT.name=='cp-setup-dns-profile-20260911-v2'
SOURCE=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
nodes=json.loads((ROOT/'plan.json').read_text())
assert set(nodes)=={'dnsprimary','dnssecondary'}
options=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts']
guest=r'''
import os,json,ssl,urllib.request,urllib.error,time,hashlib,sys,subprocess
from pathlib import Path
profile=sys.argv[1]
assert profile in ('dnsprimary','dnssecondary')
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
primary=profile=='dnsprimary'
draft={'purpose':'dns','panel_domain':'panel.'+profile+'.setup.test','dns_mode':'local','dns_engine':'bind','dns_role':'primary' if primary else 'secondary','ns1':'ns1.setup.test','ns2':'ns2.setup.test','local_ip':'192.0.2.10' if primary else '192.0.2.20','peer_ip':'192.0.2.20' if primary else '192.0.2.10','peer_ns':'ns2.setup.test' if primary else 'ns1.setup.test'}
state=api('/api/v1/setup',{'revision':state['revision'],'draft':draft},'PUT');saved('draft',state)
plan=api('/api/v1/setup/plan',{'revision':state['revision']});saved('review',plan)
if not plan['can_start']:raise RuntimeError('review blockers: '+str(plan['blockers']))
assert plan['purpose']==draft['purpose'] and plan['draft']['dns_mode']=='local'
request_id=hashlib.sha256(('disposable-dns-purpose-v2:'+profile).encode()).hexdigest()[:32]
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
if primary:
    # Both peers must be live for the primary's real PairReady proof. Create
    # through the real admin API and require transferred answers, not only a
    # package-install or configured-file assertion.
    created=api('/api/v1/domains/create',{'domain':'acceptance.dnsfixture.test','project_type':'dnsonly'})
    saved('member-create',created)
    assert created.get('DomainID',created.get('domain_id',0))>0 and not created.get('dns_pending',False),created
    observations=[]
    transfer_deadline=time.monotonic()+180
    while time.monotonic()<transfer_deadline:
        answers={};valid=True
        for host in ('192.0.2.10','192.0.2.20'):
            for kind in ('SOA','A'):
                result=subprocess.run(['dig','@'+host,'acceptance.dnsfixture.test',kind,'+tcp','+norecurse','+noall','+comments','+answer'],capture_output=True,text=True,timeout=15)
                answers[host+' '+kind]=result.stdout+result.stderr
                valid=valid and result.returncode==0 and 'status: NOERROR' in result.stdout and ' aa;' in result.stdout
                if kind=='A':valid=valid and '192.0.2.10' in result.stdout
        soa=[]
        for host in ('192.0.2.10','192.0.2.20'):
            lines=[line for line in answers[host+' SOA'].splitlines() if line and not line.startswith(';')]
            valid=valid and len(lines)==1
            soa.append(lines[0].split()[4:] if len(lines)==1 else [])
        valid=valid and bool(soa[0]) and soa[0]==soa[1]
        observations.append({'elapsed_remaining':round(transfer_deadline-time.monotonic(),2),'ready':valid,'answers':answers})
        saved('transferred-member-observations',observations)
        if valid:
            saved('transferred-member-answers',answers)
            break
        time.sleep(2)
    else:raise RuntimeError('created DNS member did not converge to exact authoritative SOA/A on both peers')
    print('DNS_PAIRED_MEMBER_TRANSFER_PASSED',flush=True)
'''

def drive(item):
    name,row=item;port=str(row['port'])
    pid=int((ROOT/name/'qemu.pid').read_text())
    process=Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    assert b'cp-profile65-'+name.encode() in process
    assert any(str(ROOT/name).encode()+b'/' in arg and b'.qcow2' in arg for arg in process),process
    subprocess.run(['scp',*options,'-P',port,str(ROOT/'panel-test'),'celik@127.0.0.1:/tmp/profile-panel-test'],check=True,capture_output=True)
    subprocess.run(['scp',*options,'-P',port,str(SOURCE/'deploy/panel-tls-snapshot.sh'),'celik@127.0.0.1:/tmp/profile-panel-tls-snapshot.sh'],check=True,capture_output=True)
    script=f'''set -euo pipefail
test "$(cat /etc/hostname)" = profile65-{name}
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel/bin/panel
test ! -e /var/lib/celikpanel-profile-vm/review.json
test ! -e /var/lib/celikpanel-profile-vm/start.json
systemctl stop celikpanel-panel.service 2>/dev/null || true
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
Environment=CELIKPANEL_SERVER_IP={row['peer_ip']}
Environment=CELIKPANEL_AGENT_SOCKET=/run/celikpanel/agent.sock
Environment=CELIKPANEL_AGENT_TOKEN_FILE=/etc/celikpanel/agent.token
ExecStart=/var/lib/celikpanel-profile-vm/panel-test -test.run ^TestServerSetupDisposableDNSProfileDaemon$ -test.v -test.timeout 2h
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
for attempt in $(seq 1 45); do test -f /var/lib/celikpanel/tls/panel.crt && break; sleep 1; done
test -f /var/lib/celikpanel/tls/panel.crt
systemctl stop celikpanel-panel.service
source /tmp/profile-panel-tls-snapshot.sh
panel_tls_normalize_legacy_self_signed /var/lib/celikpanel/tls "$(id -u celikpanel)" "$(getent group celikpanel | cut -d: -f3)"
systemctl start celikpanel-panel.service
'''
    result=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo bash -s'],input=script,text=True,capture_output=True,timeout=90)
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
with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    primary=pool.submit(drive,('dnsprimary',nodes['dnsprimary']))
    # The primary needs the secondary for final readiness. Start secondary
    # only once the primary's real durable DNS child has committed; do not
    # confuse completed package installation with full pair readiness.
    ready=False
    for attempt in range(400):
        command="sudo cat /var/lib/celikpanel-profile-vm/execution.json"
        readback=subprocess.run(['ssh',*options,'-p',str(nodes['dnsprimary']['port']),'celik@127.0.0.1',command],capture_output=True,text=True,timeout=15)
        if readback.returncode==0:
            try: current=json.loads(readback.stdout)
            except json.JSONDecodeError: current={}
            if current.get('status')=='failed':raise RuntimeError('primary failed before secondary was admitted')
            if any(step.get('kind')=='dns' and step.get('status')=='succeeded' for step in current.get('steps',[])):
                (ROOT/'primary-dns-before-secondary.json').write_text(json.dumps(current,indent=2))
                ready=True
                break
        if primary.done():
            raise RuntimeError('primary driver ended before its DNS child committed: '+str(primary.result()))
        time.sleep(3)
    if not ready:raise RuntimeError('primary DNS step did not become ready to accept a secondary')
    print('PRIMARY_DNS_COMMITTED_BEFORE_SECONDARY_START',flush=True)
    secondary=pool.submit(drive,('dnssecondary',nodes['dnssecondary']))
    codes=[primary.result(),secondary.result()]
if any(codes):raise SystemExit(1)
