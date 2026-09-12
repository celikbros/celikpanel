import json,subprocess,time,hashlib
from pathlib import Path
root=Path('/var/tmp/cp-native-dns-20260912'); nodes=json.loads((root/'plan.json').read_text())
zone='independent.example.test'; catalog='catalog-c000020a.celikpanel.invalid'
label=hashlib.sha224(zone.encode()).hexdigest()
def ssh(name,command,script=None):
    row=nodes[name]
    args=['ssh','-i','/var/tmp/cp-install-vm/key','-p',str(row['port']),'-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={root}/ssh-known-hosts','celik@127.0.0.1',command]
    r=subprocess.run(args,input=script,text=True,capture_output=True,timeout=30)
    if r.returncode:raise RuntimeError(name+': '+r.stdout+r.stderr)
    return r.stdout
config=f"""// Native owner-managed BIND configuration; no CelikPanel include.
zone "{catalog}" {{ type primary; file "/var/lib/bind/owner/catalog.zone"; allow-transfer {{ 192.0.2.20; }}; also-notify {{ 192.0.2.20; }}; notify explicit; }};
zone "{zone}" {{ type primary; file "/var/lib/bind/owner/website.zone"; allow-transfer {{ 192.0.2.20; }}; also-notify {{ 192.0.2.20; }}; notify explicit; }};
"""
def zone_text(serial,ip):
    return f"""$ORIGIN {zone}.
$TTL 30
@ IN SOA ns1.example.test. hostmaster.example.test. ( {serial} 10 5 3600 30 )
@ IN NS ns1.example.test.
@ IN NS ns2.example.test.
@ IN A {ip}
"""
def catalog_text(serial,member=True):
    return f"""$ORIGIN {catalog}.
$TTL 30
@ IN SOA invalid. invalid. ( {serial} 10 5 3600 30 )
@ IN NS invalid.
version IN TXT "2"
"""+(f"{label}.zones IN PTR {zone}.\n" if member else '')
def install_files(files,initial=False):
    code='from pathlib import Path\nimport os,shutil,subprocess\n'
    if initial:code+="p=Path('/var/lib/bind/owner');p.mkdir(exist_ok=True);shutil.chown(p,user='bind',group='bind')\nshutil.copy2('/etc/bind/named.conf.local','/root/native-dns-evidence/named.conf.local.before')\n"
    for path,text in files.items():
        code+=f'p=Path({path!r});p.write_text({text!r});os.chmod(p,0o644)\n'
    code+="subprocess.run(['named-checkconf'],check=True)\nsubprocess.run(['named-checkzone',"+repr(catalog)+",'/var/lib/bind/owner/catalog.zone'],check=True)\nsubprocess.run(['named-checkzone',"+repr(zone)+",'/var/lib/bind/owner/website.zone'],check=True)\nsubprocess.run(['rndc','reload'],check=True)\n"
    return ssh('dnsprimary','sudo python3 -',code)
proof={'target':'two disposable Debian 13 clones, BIND primary and PowerDNS secondary','management_stopped':{},'stages':[]}
for name in nodes:
    out=ssh(name,"test ! -e /opt/celikpanel/bin/agent && test ! -e /opt/celikpanel/bin/panel && ! systemctl is-active --quiet celikpanel-agent.service && ! systemctl is-active --quiet celikpanel-panel.service && printf 'management absent\n'")
    proof['management_stopped'][name]=out.strip()
    print(name,out.strip(),flush=True)
def wait_answer(serial,ip):
    for attempt in range(35):
        out=ssh('dnssecondary',f'dig @192.0.2.20 {zone} SOA +short; dig @192.0.2.20 {zone} A +short')
        if f' {serial} ' in out and ip in out:return out
        time.sleep(2)
    raise RuntimeError('native secondary convergence failed: '+out)
initial=install_files({'/etc/bind/named.conf.local':config,'/var/lib/bind/owner/catalog.zone':catalog_text(100),'/var/lib/bind/owner/website.zone':zone_text(100,'192.0.2.80')},True)
proof['stages'].append({'stage':'add zone through native catalog','answer':wait_answer(100,'192.0.2.80'),'validation':initial})
print('PASS native zone addition',flush=True)
changed=install_files({'/var/lib/bind/owner/website.zone':zone_text(101,'192.0.2.81')})
proof['stages'].append({'stage':'owner changes A record and SOA serial','answer':wait_answer(101,'192.0.2.81'),'validation':changed})
print('PASS native record update',flush=True)
for name,unit in [('dnsprimary','named'),('dnssecondary','pdns')]:
    ssh(name,'sudo systemctl restart '+unit)
proof['stages'].append({'stage':'restart both DNS daemons without management binaries','answer':wait_answer(101,'192.0.2.81')})
print('PASS native daemon restart',flush=True)
install_files({'/var/lib/bind/owner/catalog.zone':catalog_text(101,False)})
for attempt in range(35):
    out=ssh('dnssecondary',f'dig @192.0.2.20 {zone} SOA +norecurse +noall +comments +answer')
    if 'ANSWER: 0' in out and ' aa;' not in out:break
    time.sleep(2)
else:raise RuntimeError('native catalog did not remove member: '+out)
proof['stages'].append({'stage':'native catalog removes member','answer':out})
for name in nodes:
    ssh(name,"test ! -e /opt/celikpanel/bin/agent && ! systemctl is-active --quiet celikpanel-agent.service && ! systemctl is-active --quiet celikpanel-panel.service")
proof['passed']=True
(root/'native-proof.json').write_text(json.dumps(proof,indent=2)+'\n')
print('PASS native removal; all evidence saved',flush=True)
