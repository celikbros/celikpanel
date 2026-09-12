"""Trigger real installed Certbot timers only inside the two fresh test guests."""
import concurrent.futures
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(os.environ['CELIKPANEL_SECONDARY_HOSTING_VM_ROOT'])
assert ROOT == Path('/var/tmp/cp-secondary-hosting-20260912-v2')
nodes = json.loads((ROOT / 'plan.json').read_text())
options = ['-i', '/var/tmp/cp-install-vm/key', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes', '-o', f'UserKnownHostsFile={ROOT}/ssh-known-hosts']
guest = r'''
import hashlib,json,socket,ssl,sqlite3,subprocess,sys,time
from pathlib import Path
profile=sys.argv[1]
assert profile in ('dnsprimary','dnssecondary')
folder=Path('/var/lib/celikpanel-profile-vm')
assert (folder/'secondary-hosting-fixture').read_text()=='secondary-hosting-20260912\n'
assert not Path('/opt/celikpanel/bin/panel').exists()
db=sqlite3.connect('file:/var/lib/celikpanel/celikpanel.db?mode=ro',uri=True)
assert db.execute('SELECT status FROM server_setup_state WHERE id=1').fetchone()==('ready',)
domain='panel.'+profile+'.setup.test'
def run(*args):
 result=subprocess.run(args,capture_output=True,text=True,timeout=60)
 assert result.returncode==0,result.stdout+result.stderr
 return result.stdout.strip()
def leaf():
 with socket.create_connection((domain,2083),timeout=8) as raw:
  with ssl.create_default_context().wrap_socket(raw,server_hostname=domain) as tls:
   return hashlib.sha256(tls.getpeercert(binary_form=True)).hexdigest()
def state():
 return {'setup':db.execute('SELECT status,revision,completed_at FROM server_setup_state').fetchall(),'children':db.execute('SELECT id,status,kind,request_id FROM service_operations ORDER BY id').fetchall(),'engine':db.execute('SELECT active_engine,active_epoch,revision FROM dns_engine_state').fetchall(),'pair':db.execute('SELECT pair_role,local_ip,peer_ip FROM dns_bind_pair_state').fetchall()}
def snapshot():
 return {'leaf_sha256':leaf(),'state':state(),'panel_pid':run('systemctl','show','-p','MainPID','--value','celikpanel-panel.service'),'timer_enabled':run('systemctl','is-enabled','certbot.timer'),'timer_active':run('systemctl','is-active','certbot.timer'),'timer_trigger':run('systemctl','show','-p','LastTriggerUSec','--value','certbot.timer'),'service_result':run('systemctl','show','-p','Result','--value','certbot.service')}
before=snapshot()
(folder/'renewal-before.json').write_text(json.dumps(before,indent=2))
configs={p.name:p.read_text() for p in Path('/etc/letsencrypt/renewal').glob('*.conf')}
assert configs and all('https://acme.setup.test:14000/dir' in value for value in configs.values())
if profile=='dnssecondary':assert all('authenticator = webroot' in value for value in configs.values()),configs
timer=Path('/etc/systemd/system/certbot.timer.d');timer.mkdir(exist_ok=True)
service=Path('/etc/systemd/system/certbot.service.d');service.mkdir(exist_ok=True)
timer_override=timer/'90-secondary-hosting-fixture.conf'
service_override=service/'90-secondary-hosting-fixture.conf'
assert not timer_override.exists() and not service_override.exists()
timer_override.write_text('[Timer]\nOnCalendar=\nOnBootSec=\nOnUnitActiveSec=\nOnActiveSec=5s\nAccuracySec=1s\nRandomizedDelaySec=0\nPersistent=false\n')
service_override.write_text('[Service]\nExecStart=\nExecStart=/usr/bin/certbot -q renew --force-renewal --no-random-sleep-on-renew\n')
try:
 run('systemctl','daemon-reload');run('systemctl','restart','certbot.timer')
 deadline=time.monotonic()+240
 after={}
 while time.monotonic()<deadline:
  try:
   after=snapshot()
   if after['leaf_sha256']!=before['leaf_sha256'] and after['timer_trigger']!=before['timer_trigger'] and after['service_result']=='success' and run('systemctl','show','-p','ActiveState','--value','certbot.service')=='inactive':break
  except (OSError,AssertionError):pass
  time.sleep(3)
 else:raise AssertionError('timer renewal did not converge: '+json.dumps(after))
 assert before['state']==after['state'],'renewal changed setup, operations or DNS role'
 assert before['panel_pid']!=after['panel_pid'],'renewal deploy hook did not reload served certificate'
 after['renewal_configs']=configs
 after['active_workloads']={unit:run('systemctl','is-active',unit) for unit in (('bind9',) if profile=='dnsprimary' else ('pdns','nginx'))}
 (folder/'renewal-after.json').write_text(json.dumps(after,indent=2))
 print('REAL_CERTBOT_TIMER_RENEWAL_PASSED '+profile,flush=True)
finally:
 for path in (timer_override,service_override):
  assert path.resolve().is_relative_to(Path('/etc/systemd/system').resolve())
  path.unlink(missing_ok=True)
 run('systemctl','daemon-reload');run('systemctl','restart','certbot.timer')
 print('RESTORED_INSTALLED_TIMER '+run('systemctl','is-active','certbot.timer'),flush=True)
'''

def renew(name):
    folder, row = ROOT / name, nodes[name]
    pid = int((folder / 'qemu.pid').read_text())
    args = Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    assert row['qemu_name'].encode() in args
    assert any(('file=' + str(folder / 'overlay.qcow2')).encode() in arg for arg in args)
    ssh = ['ssh', *options, '-p', str(row['port']), 'celik@127.0.0.1']
    result = subprocess.run(ssh + ['sudo python3 - ' + name], input=guest, capture_output=True, text=True, timeout=360)
    (folder / 'renewal-driver.log').write_text(result.stdout + result.stderr)
    for filename in ('renewal-before.json', 'renewal-after.json'):
        evidence = subprocess.run(ssh + ['sudo cat /var/lib/celikpanel-profile-vm/' + filename], capture_output=True)
        if evidence.returncode == 0:
            json.loads(evidence.stdout)
            (folder / filename).write_bytes(evidence.stdout)
    print(name + ' renewal exit ' + str(result.returncode), flush=True)
    print(result.stdout + result.stderr, flush=True)
    return result.returncode

with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    codes = list(pool.map(renew, nodes))
if any(codes):
    raise SystemExit(1)
