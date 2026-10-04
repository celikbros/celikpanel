#!/usr/bin/env python3
"""DNS document transition trial in registered disposable QEMU guests only.

This fixture swaps test Agent executables explicitly; it is not a signed update,
installer acceptance, automatic rollback, or installed-server administration tool.
It never writes DNS receipts or zone configuration; authenticated RPCs do that.
"""
from pathlib import Path
import argparse
import importlib.util
import json
import shlex
import subprocess

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location("dns_document_lab", HERE / "lab.py")
lab = importlib.util.module_from_spec(spec)
spec.loader.exec_module(lab)


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument("command",choices=("prepare","advance","observe","detach-management","reset"))
    p.add_argument("--work-root",required=True)
    p.add_argument("--node",choices=("debian13","arch"),required=True)
    p.add_argument("--agent",type=Path)
    p.add_argument("--unit",type=Path)
    p.add_argument("--driver",type=Path)
    p.add_argument("--execute",action="store_true")
    a=p.parse_args()
    root=lab.checked_root(a.work_root)
    record,plan=lab.load(root)
    if a.command!="observe" and not a.execute:
        print(json.dumps({"action":a.command,"execute":False}));return
    evidence=root/"evidence"/a.node
    evidence.mkdir(mode=0o700,parents=True,exist_ok=True)
    def run(script,label,timeout=120):
        dest=evidence/("dns-doc-"+label+".json")
        if dest.exists(): raise ValueError("trial evidence exists; inspect same attempt")
        try:
            r=lab.guarded_script(root,record,plan,a.node,script,timeout=timeout)
        except subprocess.CalledProcessError as e:
            dest.write_text(json.dumps({"returncode":e.returncode,"stdout":e.stdout,"stderr":e.stderr}));dest.chmod(0o600)
            raise
        dest.write_text(json.dumps({"stdout":r.stdout,"stderr":r.stderr}));dest.chmod(0o600)
        print(json.dumps({"action":label,"node":a.node,"evidence":str(dest)}),flush=True)
        return r
    if a.command=="detach-management":
        run("""set -eu
systemctl stop celikpanel-agent.service
systemctl disable celikpanel-agent.service
test ! -e /opt/celikpanel/bin/panel
test ! -e /root/celikpanel-release-recovery-lab/dns-agent-retained
mv -- /opt/celikpanel/bin/agent /root/celikpanel-release-recovery-lab/dns-agent-retained
sync
""","management-absent")
        return
    if a.command=="reset":
        run("""set -eu
test ! -e /opt/celikpanel/bin/agent
test ! -e /opt/celikpanel/bin/panel
test "$(systemctl show celikpanel-agent.service -p ActiveState --value)" = inactive
systemctl is-active --quiet named.service
cat /proc/sys/kernel/random/boot_id
""","before-reset")
        spec=importlib.util.spec_from_file_location("dns_trial_qmp",HERE/"recovery_fault_trial.py")
        qmp=importlib.util.module_from_spec(spec);spec.loader.exec_module(qmp)
        node=plan["nodes"][a.node]
        connection=qmp.QMP(node,qmp.qemu_identity(node))
        try: connection.reset()
        finally: connection.close()
        print(json.dumps({"action":"reset-requested","node":a.node,"postboot_verification_required":True}))
        return
    if a.command=="observe":
        r=lab.guarded_script(root,record,plan,a.node,"""python3 - <<'PY'
from pathlib import Path
import hashlib,json,subprocess
out={"boot_id":Path('/proc/sys/kernel/random/boot_id').read_text().strip(),"receipts":{}}
for name in ('dns-engine-state.json','dns-engine-ownership-bind.json','service-mutations.json'):
 p=Path('/var/lib/celikpanel-agent-private')/name
 raw=p.read_bytes();st=p.stat()
 out['receipts'][name]={"sha256":hashlib.sha256(raw).hexdigest(),"mode":oct(st.st_mode&0o7777),"uid":st.st_uid,"gid":st.st_gid}
 if name!='service-mutations.json':out['receipts'][name]['document']=json.loads(raw)
for tcp in (False,True):
 args=['dig','@127.0.0.1','recovery-fixture.test','A','+norecurse','+time=2','+tries=1','+noall','+comments','+answer']
 if tcp:args.append('+tcp')
 r=subprocess.run(args,capture_output=True,text=True)
 out['tcp' if tcp else 'udp']={"returncode":r.returncode,"answer":r.stdout}
for unit in ('celikpanel-agent.service','celikpanel-panel.service','named.service'):
 out[unit]=subprocess.run(['systemctl','show',unit,'-p','ActiveState','-p','MainPID'],capture_output=True,text=True).stdout
print(json.dumps(out,sort_keys=True))
PY""",timeout=30)
        print(r.stdout.strip());return
    if a.agent is None or a.driver is None:raise ValueError("Agent and driver required")
    agent,agent_hash=lab.put_file(root,record,plan,a.node,a.agent,"dns-agent-"+a.command,mode=0o700)
    driver,driver_hash=lab.put_file(root,record,plan,a.node,a.driver,"dns-document-driver",mode=0o700)
    q=shlex.quote
    if a.command=="prepare":
        if a.unit is None:raise ValueError("historical production unit required")
        unit,unit_hash=lab.put_file(root,record,plan,a.node,a.unit,"dns-agent.service")
        script="""set -eu
[ ! -e /opt/celikpanel/bin/agent ]
[ ! -e /var/lib/celikpanel-agent-private ]
groupadd --system celikpanel
useradd --system --gid celikpanel --no-create-home --home-dir /var/lib/celikpanel --shell /usr/sbin/nologin celikpanel
install -d -m 0755 /opt/celikpanel/bin
install -d -m 0750 -o root -g celikpanel /etc/celikpanel /run/celikpanel
install -d -m 0750 -o celikpanel -g celikpanel /var/lib/celikpanel
install -d -m 0700 -o root -g celikpanel /var/lib/celikpanel-agent-private
install -d -m 0775 -o root -g celikpanel /opt/celikpanel/runtimes
"""+f"install -m 0755 -o root -g root {q(agent)} /opt/celikpanel/bin/agent\ninstall -m 0644 -o root -g root {q(unit)} /etc/systemd/system/celikpanel-agent.service\n"+"""
/opt/celikpanel/bin/agent --initialize-service-mutation-ledger
systemctl daemon-reload
systemctl start celikpanel-agent.service
"""
        run(script,"prepare")
    else:
        # Existing native state must be present. This is a disposable fixture
        # binary handoff, never the installed-panel update implementation.
        run("set -eu\ntest -f /var/lib/celikpanel-agent-private/dns-engine-state.json\nsystemctl stop celikpanel-agent.service\n"+f"install -m 0755 -o root -g root {q(agent)} /opt/celikpanel/bin/agent\n"+"systemctl start celikpanel-agent.service\n","handoff")
    run(f"{q(driver)} --nonce {q(record['nonce'])}"+(" --advance" if a.command=="advance" else ""),a.command+"-rpc",timeout=930)
    print(json.dumps({"agent_sha256":agent_hash,"driver_sha256":driver_hash,"native_trial_only":True}))

if __name__=="__main__":
    main()
