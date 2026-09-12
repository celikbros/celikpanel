"""Exercise single reviewed secondary-hosting setup and real mixed DNS transfer."""
import concurrent.futures
import hashlib
import json
import os
import subprocess
import time
from pathlib import Path

ROOT = Path(os.environ.get('CELIKPANEL_SECONDARY_HOSTING_VM_ROOT', '/var/tmp/cp-secondary-hosting-20260912'))
assert ROOT.name in ('cp-secondary-hosting-20260912', 'cp-secondary-hosting-20260912-v2')
nodes = json.loads((ROOT / 'plan.json').read_text())
OPTIONS = ['-i', '/var/tmp/cp-install-vm/key', '-o', 'BatchMode=yes', '-o', 'StrictHostKeyChecking=yes', '-o', f'UserKnownHostsFile={ROOT}/ssh-known-hosts']

def command(name):
    row, folder = nodes[name], ROOT / name
    pid = int((folder / 'qemu.pid').read_text())
    args = Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    assert row['qemu_name'].encode() in args
    assert any(('file=' + str(folder / 'overlay.qcow2')).encode() in arg for arg in args)
    return ['ssh', *OPTIONS, '-p', str(row['port']), 'celik@127.0.0.1']

def execute(name, remote, data=None, timeout=90):
    result = subprocess.run(command(name) + [remote], input=data, capture_output=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(name + ': ' + result.stdout.decode()[-3000:] + result.stderr.decode()[-3000:])
    return result.stdout

api_script = r'''
import json,ssl,urllib.request,urllib.error,sys
from pathlib import Path
r=json.load(sys.stdin);folder=Path('/var/lib/celikpanel-profile-vm')
assert (folder/'secondary-hosting-fixture').read_text()=='secondary-hosting-20260912\n'
url='https://panel.'+r['profile']+'.setup.test:2083'
token=(folder/'session').read_text()
context=ssl.create_default_context() if r.get('trusted') else ssl._create_unverified_context()
body=r.get('body');request=urllib.request.Request(url+r['path'],data=None if body is None else json.dumps(body).encode(),method=r.get('method'))
request.add_header('Cookie','celikpanel_session='+token);request.add_header('Origin',url);request.add_header('Content-Type','application/json')
try:
 with urllib.request.urlopen(request,context=context,timeout=150) as response: value=json.load(response);status=response.status
except urllib.error.HTTPError as error:
 print(json.dumps({'status':error.code,'error_body':error.read().decode()}));sys.exit(1)
print(json.dumps(value))
'''
(ROOT / 'api.py').write_text(api_script)

def save(name, label, value):
    (ROOT / name / (label + '.json')).write_text(json.dumps(value, indent=2))

def api(name, path, body=None, method=None, trusted=False):
    payload = json.dumps({'profile': name, 'path': path, 'body': body, 'method': method, 'trusted': trusted}).encode()
    return json.loads(execute(name, 'sudo python3 /var/lib/celikpanel-profile-vm/driver-api.py', payload, timeout=180))

def stage(name):
    row = nodes[name]
    for source in [ROOT / 'panel-test', ROOT / 'api.py', ROOT / 'source/deploy/panel-tls-snapshot.sh']:
        subprocess.run(['scp', *OPTIONS, '-P', str(row['port']), str(source), 'celik@127.0.0.1:/tmp/' + source.name], check=True, capture_output=True)
    script = f'''set -euo pipefail
test "$(cat /etc/hostname)" = profile65-{name}
test "$(cat /var/lib/celikpanel-profile-vm/secondary-hosting-fixture)" = secondary-hosting-20260912
test ! -e /opt/celikpanel/bin/panel
test ! -e /var/lib/celikpanel-profile-vm/panel-test
install -o root -g celikpanel -m 0750 /tmp/panel-test /var/lib/celikpanel-profile-vm/panel-test
install -o root -g root -m 0700 /tmp/api.py /var/lib/celikpanel-profile-vm/driver-api.py
cat > /etc/systemd/system/celikpanel-panel.service <<'EOF'
[Unit]
Description=Disposable secondary-hosting acceptance daemon
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
ExecStart=/var/lib/celikpanel-profile-vm/panel-test -test.run ^TestSecondaryHostingDisposableDaemon$ -test.v -test.timeout 2h
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
source /tmp/panel-tls-snapshot.sh
panel_tls_normalize_legacy_self_signed /var/lib/celikpanel/tls "$(id -u celikpanel)" "$(getent group celikpanel | cut -d: -f3)"
systemctl start celikpanel-panel.service
'''
    (ROOT / name / 'daemon-stage.log').write_bytes(execute(name, 'sudo bash -s', script.encode(), timeout=100))
    for attempt in range(30):
        try:
            state = api(name, '/api/v1/setup')
            assert state['status'] in ('new', 'draft')
            return
        except Exception:
            time.sleep(2)
    raise RuntimeError(name + ' did not expose setup API')

def start(name):
    primary = name == 'dnsprimary'
    state = api(name, '/api/v1/setup')
    draft = {'purpose': 'dns' if primary else 'custom', 'panel_domain': 'panel.' + name + '.setup.test', 'dns_mode': 'local', 'dns_engine': 'bind' if primary else 'pdns', 'dns_role': 'primary' if primary else 'secondary', 'ns1': 'ns1.setup.test', 'ns2': 'ns2.setup.test', 'local_ip': '192.0.2.10' if primary else '192.0.2.20', 'peer_ip': '192.0.2.20' if primary else '192.0.2.10', 'peer_ns': 'ns2.setup.test' if primary else 'ns1.setup.test'}
    if not primary:
        draft.update({'customization': {'components': ['nginx']}, 'dns_publisher_endpoint': 'https://panel.dnsprimary.setup.test:2083'})
    state = api(name, '/api/v1/setup', {'revision': state['revision'], 'draft': draft}, 'PUT')
    save(name, 'draft', state)
    plan = api(name, '/api/v1/setup/plan', {'revision': state['revision']})
    save(name, 'review', plan)
    assert plan['can_start'], plan.get('blockers')
    rid = hashlib.sha256(('secondary-hosting-20260912:' + name).encode()).hexdigest()[:32]
    request = {'plan_id': plan['id'], 'confirmed': True, 'request_id': rid}
    execution = api(name, '/api/v1/setup/start', request)
    replay = api(name, '/api/v1/setup/start', request)
    assert replay['id'] == execution['id'] and replay['plan_id'] == plan['id']
    save(name, 'start', execution)
    return rid, execution['id']

def wait_for(name, rid, predicate, label, seconds=1200):
    deadline, previous = time.monotonic() + seconds, None
    while time.monotonic() < deadline:
        try:
            current = api(name, '/api/v1/setup/operation?request_id=' + rid)
        except Exception:
            time.sleep(3)
            continue
        save(name, 'execution', current)
        status = (current['status'], current['phase'])
        if status != previous:
            print(name, status, flush=True)
            previous = status
        assert current['status'] != 'failed', current
        if predicate(current):
            save(name, label, current)
            return current
        time.sleep(3)
    save(name, 'readiness-timeout', api(name, '/api/v1/setup?check=1'))
    raise RuntimeError(name + ' timeout waiting for ' + label)

with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
    list(pool.map(stage, nodes))
primary_request, primary_execution = start('dnsprimary')
wait_for('dnsprimary', primary_request, lambda row: any(step['kind'] == 'dns' and step['status'] == 'succeeded' for step in row['steps']), 'dns-before-secondary')
secondary_request, secondary_execution = start('dnssecondary')
wait_for('dnssecondary', secondary_request, lambda row: row['status'] == 'waiting' and row['phase'] == 'dns_publisher', 'publisher-gate')
wait_for('dnsprimary', primary_request, lambda row: row['status'] == 'succeeded', 'ready-execution')
enrollment = api('dnsprimary', '/api/v1/dns/remote/enrollments', {}, trusted=True)
connection = api('dnssecondary', '/api/v1/dns/remote/connections', {'endpoint': 'https://panel.dnsprimary.setup.test:2083', 'enrollment_code': enrollment['enrollment_code'], 'label': 'Disposable secondary hosting'}, trusted=True)
assert connection['verified']
save('dnssecondary', 'connection', connection)
binding = {'execution_id': secondary_execution, 'connection_id': connection['connection']['id']}
confirmed = api('dnssecondary', '/api/v1/setup/publisher', binding, trusted=True)
replayed = api('dnssecondary', '/api/v1/setup/publisher', binding, trusted=True)
assert confirmed['id'] == replayed['id'] == secondary_execution
save('dnssecondary', 'publisher-confirmation', confirmed)
wait_for('dnssecondary', secondary_request, lambda row: row['status'] == 'succeeded', 'ready-execution')
for name in nodes:
    state = api(name, '/api/v1/setup?check=1', trusted=True)
    assert state['status'] == 'ready' and all(check['state'] == 'ready' for check in state['checks']), state
    save(name, 'ready', state)
print('SINGLE_SETUP_SECONDARY_HOSTING_READY', flush=True)

domain = 'customer.secondaryfixture.test'
created = api('dnssecondary', '/api/v1/domains/create', {'domain': domain, 'project_type': 'static'}, trusted=True)
save('dnssecondary', 'domain-create', created)
domain_id = created.get('DomainID', created.get('domain_id', 0))
assert domain_id > 0, created
api('dnssecondary', f'/api/v1/domains/{domain_id}/dns/records', {'name': '@', 'type': 'TXT', 'content': 'secondary-hosting-validation', 'ttl': 300}, trusted=True)
api('dnssecondary', f'/api/v1/domains/{domain_id}/mail/auth/apply', {'record': 'spf'}, trusted=True)
api('dnssecondary', f'/api/v1/domains/{domain_id}/mail/auth/apply', {'record': 'dmarc'}, trusted=True)
save('dnssecondary', 'dns-records', api('dnssecondary', f'/api/v1/domains/{domain_id}/dns/records', trusted=True))
print('SECONDARY_HOSTING_REMOTE_RECORDS_PUBLISHED', flush=True)
(ROOT / 'continuation.json').write_text(json.dumps({'domain': domain, 'domain_id': domain_id, 'primary_request': primary_request, 'secondary_request': secondary_request, 'secondary_execution': secondary_execution}, indent=2))
