import ast,json,subprocess,textwrap
from pathlib import Path
repo=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
root=Path('/var/tmp/cp-setup-dns-profile-20260911-v2')
p=repo/'.tmp-setup-dns-profile-drive-v2.py'
s=p.read_text()
s=s.replace("assert created.get('domain_id',0)>0 and not created.get('dns_pending',False),created", "assert created.get('DomainID',created.get('domain_id',0))>0 and not created.get('dns_pending',False),created")
p.write_bytes(s.encode())
tree=ast.parse(s)
guest=next(node.value.value for node in tree.body if isinstance(node,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='guest' for t in node.targets))
start=guest.index('    observations=[]\n')
end=guest.index("    print('DNS_PAIRED_MEMBER_TRANSFER_PASSED',flush=True)",start)+len("    print('DNS_PAIRED_MEMBER_TRANSFER_PASSED',flush=True)")
body=textwrap.dedent(guest[start:end])
read_only='''import json,time,subprocess
from pathlib import Path
folder=Path('/var/lib/celikpanel-profile-vm')
assert (folder/'fixture').read_text()=='fresh-setup-profile-acceptance-v1\\n'
assert not Path('/opt/celikpanel/bin/panel').exists()
state=json.loads((folder/'ready.json').read_text())
assert state['status']=='ready' and all(c['state']=='ready' for c in state['checks'])
created=json.loads((folder/'member-create.json').read_text())
assert created['DomainID']==1 and created['Domain']=='acceptance.dnsfixture.test'
def saved(name,value):(folder/(name+'.json')).write_text(json.dumps(value,indent=2))
'''+body
read_only=read_only.replace("'fresh-setup-profile-acceptance-v1\\n'", "'fresh-setup-profile-acceptance-v1\\n'")
opts=['-i','/var/tmp/cp-install-vm/key','-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={root}/ssh-known-hosts']
result=subprocess.run(['ssh',*opts,'-p','2284','celik@127.0.0.1','sudo python3 -'],input=read_only,capture_output=True,text=True,timeout=240)
(root/'dnsprimary'/'transfer-readonly-driver.log').write_text(result.stdout+result.stderr)
print(result.stdout+result.stderr)
assert result.returncode==0
snapshot=subprocess.run(['ssh',*opts,'-p','2284','celik@127.0.0.1',"sudo python3 -c 'import json,pathlib; p=pathlib.Path(\"/var/lib/celikpanel-profile-vm\"); print(json.dumps({f.name:json.loads(f.read_text()) for f in p.glob(\"*.json\") if f.name != \"pebble.json\"}))'"],capture_output=True,timeout=30)
assert snapshot.returncode==0
(root/'dnsprimary'/'final-execution-evidence.json').write_bytes(snapshot.stdout)
