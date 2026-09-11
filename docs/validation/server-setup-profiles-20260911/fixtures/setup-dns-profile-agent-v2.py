import subprocess,json,concurrent.futures,hashlib
from pathlib import Path
ROOT=Path('/var/tmp/cp-setup-dns-profile-20260911-v2')
SOURCE=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
nodes=json.loads((ROOT/'plan.json').read_text())
assert (ROOT/'agent').is_file()
def install(item):
    name,row=item;port=str(row['port'])
    pid=int((ROOT/name/'qemu.pid').read_text())
    process=Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    assert b'cp-profile65-'+name.encode() in process
    assert any(('file='+str(ROOT/name/'overlay.qcow2')).encode() in arg for arg in process)
    options=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts']
    for source in [ROOT/'agent', SOURCE/'deploy/systemd/celikpanel-agent.service',SOURCE/'deploy/systemd/celikpanel-firewall-restore.service']:
        subprocess.run(['scp',*options,'-P',port,str(source),'celik@127.0.0.1:/tmp/'+source.name],check=True,capture_output=True)
    script=f'''set -euo pipefail
test "$(cat /etc/hostname)" = profile65-{name}
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel
test ! -e /etc/celikpanel
groupadd --system celikpanel
useradd --system --gid celikpanel --home-dir /var/lib/celikpanel --shell /usr/sbin/nologin celikpanel
install -d -m 0755 /opt/celikpanel/bin /opt/celikpanel/web
install -d -m 0775 -o root -g celikpanel /opt/celikpanel/runtimes
install -d -m 0750 -o celikpanel -g celikpanel /var/lib/celikpanel /var/lib/celikpanel/tls
install -d -m 0750 -o root -g celikpanel /etc/celikpanel /run/celikpanel
install -d -m 0700 -o root -g celikpanel /var/lib/celikpanel-agent-private
chgrp celikpanel /var/lib/celikpanel-profile-vm
chmod 0770 /var/lib/celikpanel-profile-vm
install -m 0755 /tmp/agent /opt/celikpanel/bin/agent
install -m 0644 /tmp/celikpanel-agent.service /etc/systemd/system/celikpanel-agent.service
install -m 0644 /tmp/celikpanel-firewall-restore.service /etc/systemd/system/celikpanel-firewall-restore.service
/opt/celikpanel/bin/agent --initialize-service-mutation-ledger
systemctl daemon-reload
systemctl enable --now celikpanel-agent.service
test ! -e /opt/celikpanel/bin/panel
for attempt in $(seq 1 30); do test -S /run/celikpanel/agent.sock && break; sleep 1; done
test -S /run/celikpanel/agent.sock
systemctl is-active celikpanel-agent.service
printf 'PROFILE_AGENT_READY {name}\\n'
'''
    result=subprocess.run(['ssh',*options,'-p',port,'celik@127.0.0.1','sudo bash -s'],input=script,text=True,capture_output=True,timeout=60)
    (ROOT/name/'agent-fixture.log').write_text(result.stdout+result.stderr)
    assert result.returncode==0,result.stdout+result.stderr
    print(result.stdout,flush=True)
with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:list(pool.map(install,nodes.items()))
(ROOT/'agent.sha256').write_text(hashlib.sha256((ROOT/'agent').read_bytes()).hexdigest()+'\n')
