import subprocess, json, time, hashlib, concurrent.futures, os, shutil
from pathlib import Path

OLD=Path('/var/tmp/cp-install-vm')
ROOT=Path('/var/tmp/cp-setup-profiles-20260911-v3')
SOURCE=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
ROOT.mkdir(mode=0o700,exist_ok=False)
original=json.loads((OLD/'cells/febe481be1059907aca89ccd/fixture-plan.json').read_text())['nodes']['debian13']
base=Path(original['base']['path'])
with base.open('rb') as f:
    assert hashlib.file_digest(f,original['base']['digest']['algorithm']).hexdigest()==original['base']['digest']['value']

def run(args,**kwargs):
    result=subprocess.run(args,stdout=subprocess.PIPE,stderr=subprocess.STDOUT,**kwargs)
    if result.returncode: raise RuntimeError(f'{args[0]} exit {result.returncode}: {result.stdout.decode()[-6000:]}')
    return result.stdout

# A pinned checkout is recorded before building the isolated ACME fixture.
previous=Path('/var/tmp/cp-setup-profiles-20260911')
pebble_commit=json.loads((previous/'plan.json').read_text())['web']['pebble_commit']
shutil.copy2(previous/'pebble',ROOT/'pebble')
shutil.copy2(previous/'agent',ROOT/'agent')
nodes={}
for index,name in enumerate(['web','application','webmail'],1):
    d=ROOT/name; d.mkdir(mode=0o700)
    port=2270+index; mac=f'52:54:00:68:00:{index:02d}'
    for key,value in original['cloud_init'].items():
        value=value.replace(original['management']['mac'],mac).replace('dns-kill-','profile65-').replace('dns-debian13','profile65-'+name)
        if key=='network-config':value=value.split('  peer:')[0]
        (d/key).write_text(value)
    run(['genisoimage','-quiet','-output',str(d/'seed.iso'),'-volid','cidata','-joliet','-rock',*[str(d/n) for n in ('user-data','meta-data','network-config')]])
    run(['qemu-img','create','-f','qcow2','-F','qcow2','-b',str(base),str(d/'overlay.qcow2'),'24G'])
    command=['qemu-system-x86_64','-name','cp-profile65-'+name,'-accel','kvm','-cpu','host','-smp','2','-m','2048','-drive',f'file={d}/overlay.qcow2,if=virtio,format=qcow2,cache=none','-drive',f'file={d}/seed.iso,if=virtio,format=raw,media=cdrom,readonly=on','-netdev',f'user,id=mgmt,hostfwd=tcp:127.0.0.1:{port}-:22','-device',f'virtio-net-pci,netdev=mgmt,mac={mac}','-qmp',f'unix:{d}/qmp.sock,server=on,wait=off','-pidfile',str(d/'qemu.pid'),'-serial',f'file:{d}/serial.log','-display','none','-monitor','none','-daemonize']
    run(command)
    nodes[name]={'port':port,'qemu_command':command,'base_digest':original['base']['digest'],'pebble_commit':pebble_commit,'panel_domain':f'panel.{name}.setup.test','mail_domain':f'mail.{name}.setup.test'}
(ROOT/'plan.json').write_text(json.dumps(nodes,indent=2))

def ssh(port):
    return ['ssh','-i',str(OLD/'key'),'-p',str(port),'-o','BatchMode=yes','-o','ConnectTimeout=5','-o','StrictHostKeyChecking=accept-new','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts','celik@127.0.0.1']

def provision(item):
    name,node=item; port=node['port']; d=ROOT/name
    for _ in range(90):
        attempt=subprocess.run(ssh(port)+['cloud-init status --wait'],stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=90)
        if attempt.returncode in (0,2):break
        time.sleep(3)
    else:raise RuntimeError(name+' cloud-init timeout')
    print(name+' cloud-init ready',flush=True)
    run(['scp','-i',str(OLD/'key'),'-P',str(port),'-o','BatchMode=yes','-o','StrictHostKeyChecking=yes','-o',f'UserKnownHostsFile={ROOT}/ssh-known-hosts',str(ROOT/'pebble'),'celik@127.0.0.1:/tmp/pebble'])
    script=(SOURCE/'.tmp-setup-profile-guest.sh').read_text().replace('__PROFILE__',name)
    output=run(ssh(port)+['sudo bash -s'],input=script.encode(),timeout=600)
    (d/'infra.log').write_bytes(output)
    print(name+' isolated ACME + DNS ready',flush=True)

with concurrent.futures.ThreadPoolExecutor(max_workers=3) as pool:list(pool.map(provision,nodes.items()))
