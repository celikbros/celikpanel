import subprocess,json
from pathlib import Path
r=Path('/var/tmp/cp-setup-profiles-20260911-v3'); n=json.loads((r/'plan.json').read_text()); name='webmail'
pid=int((r/name/'qemu.pid').read_text());assert b'cp-profile65-webmail' in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
a=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={r}/ssh-known-hosts','-p',str(n[name]['port']),'celik@127.0.0.1','sudo bash -s']
s='''set -euo pipefail
test "$(cat /var/lib/celikpanel-profile-vm/fixture)" = fresh-setup-profile-acceptance-v1
test ! -e /opt/celikpanel/bin/panel
test "$(cat /etc/hostname)" = mail.webmail.setup.test
ip address add 192.0.2.15/32 dev lo
# This exact documentation-address source route stays on loopback. It does not
# route any test-network traffic to a public host or create public mail proof.
ip route add 1.1.1.1/32 dev lo src 192.0.2.15
python3 - <<'PY'
from pathlib import Path
p=Path('/var/lib/celikpanel-profile-vm/dnsmasq.conf')
s=p.read_text(); assert 'address=/mail.webmail.setup.test/10.0.2.15' in s
s=s.replace('address=/mail.webmail.setup.test/10.0.2.15','address=/mail.webmail.setup.test/192.0.2.15')
s+='\\nptr-record=15.2.0.192.in-addr.arpa,mail.webmail.setup.test\\n'
p.write_text(s)
PY
systemctl restart celikpanel-fixture-dns.service
ip -o route get 1.1.1.1
getent ahostsv4 mail.webmail.setup.test
getent hosts 192.0.2.15
printf 'ISOLATED_TEST_NET_MAIL_IDENTITY_CONFIGURED\\n'
'''
v=subprocess.run(['ssh',*a],input=s,text=True,capture_output=True,timeout=45);print(v.stdout+v.stderr);(r/name/'isolated-mail-identity.log').write_text(v.stdout+v.stderr);assert v.returncode==0
