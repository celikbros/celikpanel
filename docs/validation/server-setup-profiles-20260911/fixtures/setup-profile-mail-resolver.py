import subprocess,json
from pathlib import Path
r=Path('/var/tmp/cp-setup-profiles-20260911-v3');n=json.loads((r/'plan.json').read_text());name='webmail'
pid=int((r/name/'qemu.pid').read_text());assert b'cp-profile65-webmail' in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
a=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={r}/ssh-known-hosts','-p',str(n[name]['port']),'celik@127.0.0.1','sudo bash -s']
s='''set -euo pipefail
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel/bin/panel
ip address add 1.1.1.1/32 dev lo
ip route replace local 1.1.1.1/32 dev lo table local src 192.0.2.15
python3 - <<'PY'
from pathlib import Path
p=Path('/var/lib/celikpanel-profile-vm/dnsmasq.conf');s=p.read_text();assert 'listen-address=127.0.0.1' in s
p.write_text(s.replace('listen-address=127.0.0.1','listen-address=127.0.0.1,1.1.1.1'))
p=Path('/etc/systemd/resolved.conf.d');p.mkdir(exist_ok=True)
(p/'90-celikpanel-disposable-fixture.conf').write_text('[Resolve]\\nDNS=127.0.0.1\\nDomains=~.\\n')
PY
systemctl restart celikpanel-fixture-dns.service systemd-resolved.service
ip -o route get 1.1.1.1
getent ahostsv4 panel.webmail.setup.test
getent ahostsv4 mail.webmail.setup.test
getent hosts 192.0.2.15
systemctl is-active celikpanel-fixture-dns.service systemd-resolved.service
printf 'ISOLATED_FIXED_RESOLVER_LISTENER_READY\\n'
'''
v=subprocess.run(['ssh',*a],input=s,text=True,capture_output=True,timeout=45);print(v.stdout+v.stderr);(r/name/'isolated-resolver-persistence.log').write_text(v.stdout+v.stderr);assert v.returncode==0
