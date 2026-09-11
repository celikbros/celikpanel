import subprocess,json,hashlib
from pathlib import Path
r=Path('/var/tmp/cp-setup-profiles-20260911-v3');n=json.loads((r/'plan.json').read_text());name='webmail'
pid=int((r/name/'qemu.pid').read_text());assert b'cp-profile65-webmail' in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
a=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={r}/ssh-known-hosts']
source=r/'candidate-recovery'/'agent';digest=hashlib.sha256(source.read_bytes()).hexdigest();assert digest=='64ad8615424efd30f344b94c1ca6c52b71f3858a03ad32e56b7b32b6d1dfabd5'
subprocess.run(['scp',*a,'-P',str(n[name]['port']),str(source),'celik@127.0.0.1:/tmp/profile-agent-mail-fix'],check=True,capture_output=True)
s='''set -euo pipefail
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel/bin/panel
test "$(cat /etc/hostname)" = mail.webmail.setup.test
python3 - <<'PY'
import json,sqlite3,hashlib
from pathlib import Path
p=Path('/var/lib/celikpanel-profile-vm')
c=sqlite3.connect('/var/lib/celikpanel/celikpanel.db')
assert c.execute("select count(*) from service_operations where status in ('queued','running')").fetchone()[0]==0
ledger=json.loads(Path('/var/lib/celikpanel-agent-private/service-mutations.json').read_text())
assert not ledger.get('active_request_id')
assert not [v for v in ledger['jobs'].values() if v.get('status') in ('accepted','running','queued')]
state={'children':c.execute('select id,status,kind,request_id from service_operations order by id').fetchall(),'setup':c.execute('select status,revision from server_setup_state where id=1').fetchone(),'agent_sha256':hashlib.sha256(Path('/opt/celikpanel/bin/agent').read_bytes()).hexdigest()}
assert state['setup']==('running',1)
(p/'before-mail-dns-recovery.json').write_text(json.dumps(state,indent=2))
print('NO_ACTIVE_CHILD_OR_AGENT_LEASE; same waiting setup revision1')
PY
install -o root -g root -m 0755 /tmp/profile-agent-mail-fix /opt/celikpanel/bin/agent-mail-fix
# Atomic candidate replacement inside this explicitly disposable fixture only.
mv /opt/celikpanel/bin/agent-mail-fix /opt/celikpanel/bin/agent
systemctl restart celikpanel-agent.service
systemctl is-active celikpanel-agent.service
sha256sum /opt/celikpanel/bin/agent
'''
v=subprocess.run(['ssh',*a,'-p',str(n[name]['port']),'celik@127.0.0.1','sudo bash -s'],input=s,text=True,capture_output=True,timeout=60)
(r/name/'mail-dns-candidate.log').write_text(v.stdout+v.stderr);print(v.stdout+v.stderr);assert v.returncode==0
(r/name/'mail-dns-candidate.json').write_text(json.dumps({'agent_sha256':digest,'source_snapshot':json.loads((r/'candidate-recovery'/'source-snapshot.json').read_text())},indent=2))
