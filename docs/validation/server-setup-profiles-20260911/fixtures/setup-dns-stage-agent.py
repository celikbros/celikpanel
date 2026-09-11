import json,subprocess,hashlib,sys
from pathlib import Path
root=Path('/var/tmp/cp-setup-dns-profile-20260911')
source=Path(sys.argv[1]).resolve()
expected=sys.argv[2]
assert source.is_relative_to(Path('/var/tmp')) and len(expected)==64
assert hashlib.sha256(source.read_bytes()).hexdigest()==expected
nodes=json.loads((root/'plan.json').read_text())
assert set(nodes)=={'dnsprimary','dnssecondary'}
for name,row in nodes.items():
    pid=int((root/name/'qemu.pid').read_text())
    assert b'cp-profile65-'+name.encode() in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
    options=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={root}/ssh-known-hosts']
    subprocess.run(['scp',*options,'-P',str(row['port']),str(source),'celik@127.0.0.1:/tmp/dns-fixture-final-agent'],check=True,capture_output=True)
    script=f"""set -euo pipefail
test "$(cat /etc/hostname)" = profile65-{name}
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel/bin/panel
test ! -e /var/lib/celikpanel/celikpanel.db
test "$(sha256sum /tmp/dns-fixture-final-agent | cut -d' ' -f1)" = {expected}
systemctl stop celikpanel-agent.service
install -o root -g root -m 0755 /tmp/dns-fixture-final-agent /opt/celikpanel/bin/agent
systemctl start celikpanel-agent.service
systemctl is-active celikpanel-agent.service
sha256sum /opt/celikpanel/bin/agent
"""
    result=subprocess.run(['ssh',*options,'-p',str(row['port']),'celik@127.0.0.1','sudo bash -s'],input=script,capture_output=True,text=True,timeout=45)
    (root/name/'final-agent-stage.log').write_text(result.stdout+result.stderr)
    assert result.returncode==0,result.stdout+result.stderr
    print(name,result.stdout)
(root/'final-agent.sha256').write_text(expected+'\n')
