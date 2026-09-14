#!/usr/bin/env python3
"""Arm one active-checkpoint worker crash in a registered disposable QEMU lab."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import shlex
import sys
import time

HERE=Path(__file__).resolve().parent
SPEC=importlib.util.spec_from_file_location("kill_controller_shared",HERE/"arm_port_fault.py")
shared=importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name]=shared
SPEC.loader.exec_module(shared)
trial=shared.trial
lab=shared.lab
SCHEMA="celikpanel/release-update-kill-intent/v1"
EVENT_SCHEMA="celikpanel/release-update-kill/v1"
KIND="update-kill"


def validate_events(raw,identity,operation):
    if len(raw)>shared.MAXIMUM or (raw and not raw.endswith(b"\n")):
        raise ValueError("kill event stream is incomplete or oversized")
    events=[json.loads(line) for line in raw.splitlines()]
    order={"armed":0,"worker_frozen":1,"candidate_installed_checkpoint":2,"kill_requested":3,"kill_sent":4,"released":5}
    previous=-1
    for event in events:
        kind=event.get("event")
        if (event.get("schema")!=EVENT_SCHEMA or event.get("identity")!=identity or event.get("operation_id")!=operation
                or kind not in order or order[kind]<=previous or not isinstance(event.get("at"),str)):
            raise ValueError("kill event identity or sequence differs")
        previous=order[kind]
    kinds=[event["event"] for event in events]
    progress=kinds[:-1] if kinds and kinds[-1]=="released" else kinds
    expected=["armed","worker_frozen","candidate_installed_checkpoint","kill_requested","kill_sent"]
    if progress!=expected[:len(progress)] or (kinds and not progress):
        raise ValueError("kill stream is missing a required prior checkpoint")
    return events


def collect(root,record,plan,node,intent):
    saved=json.loads(trial.read_private(root/"evidence"/node/"update-kill-intent.json"))
    if saved.get("schema")!=SCHEMA or saved.get("identity")!=intent["identity"] or saved.get("operation_id")!=intent["request_id"]:
        raise ValueError("kill intent differs from exact fixture operation")
    state,raw=shared.read_guest(root,record,plan,node,intent["request_id"],KIND)
    events=validate_events(raw,intent["identity"],intent["request_id"])
    label="update-kill-collection-"+str(time.time_ns())
    evidence={"events":trial.save(root,node,label+".jsonl",raw),"state":trial.save(root,node,label+".state.json",(json.dumps(state,sort_keys=True)+"\n").encode())}
    summary={"action":"update-kill-collected","node":node,"operation_id":intent["request_id"],"events":[e["event"] for e in events],"properties":state["properties"],"evidence":evidence}
    print(json.dumps(summary,sort_keys=True),flush=True)
    return summary


def launch_argv(intent,assets,candidates,node):
    ident=intent["identity"]
    operation=intent["request_id"]
    shared.unit_name(operation,KIND)  # validate before any command construction
    cleanup="-/usr/bin/systemctl thaw celikpanel-self-update-"+operation+".service"
    return ["systemd-run","--unit="+shared.unit_name(operation,KIND),"--no-block","--property=RuntimeMaxSec=650","--property=TimeoutStopSec=10","--property=UMask=0077","--property=ExecStopPost="+cleanup,
            "python3","-I",assets["guest_update_kill.py"]["guest_path"],"--lab-nonce",ident["nonce"],"--vm-uuid",ident["vm_uuid"],"--cell-id",ident["cell_id"],"--node",node,
            "--operation-id",operation,"--candidate-agent",candidates["agent"],"--candidate-panel",candidates["panel"]]


def arm(root,record,plan,node,intent):
    shared.assert_not_started(root,node)
    trial.validate_preview(root,record,plan,node,intent)
    candidates=shared.candidate_artifacts(intent)
    path=root/"evidence"/node/"update-kill-intent.json"
    if path.exists() or path.is_symlink():
        raise ValueError("a kill intent already exists; never rearm")
    state,raw=shared.read_guest(root,record,plan,node,intent["request_id"],KIND)
    if raw or state.get("present") or state["properties"].get("LoadState")!="not-found":
        raise ValueError("kill fixture unit or evidence already exists")
    assets={}
    for name in ("guest_probe.py","guest_port_fault.py","guest_update_kill.py"):
        guest,digest=lab.put_file(root,record,plan,node,HERE/name,name)
        assets[name]={"guest_path":guest,"sha256":digest}
    shared.assert_not_started(root,node)
    saved={"schema":SCHEMA,"identity":intent["identity"],"operation_id":intent["request_id"],"created_at":trial.now(),"candidate_artifacts":candidates,"assets":assets,
           "update_intent_sha256":hashlib.sha256(trial.read_private(root/"evidence"/node/"update-intent.json")).hexdigest()}
    trial.save(root,node,"update-kill-intent.json",(json.dumps(saved,sort_keys=True)+"\n").encode())
    lab.guarded_script(root,record,plan,node,shlex.join(launch_argv(intent,assets,candidates,node)),timeout=30)
    deadline=time.monotonic()+10
    while time.monotonic()<deadline:
        state,raw=shared.read_guest(root,record,plan,node,intent["request_id"],KIND)
        events=validate_events(raw,intent["identity"],intent["request_id"])
        if events:
            if events[-1]["event"]=="released" or state["properties"].get("ActiveState")!="active":
                raise ValueError("kill fixture stopped before the updater was started")
            collect(root,record,plan,node,intent)
            print(json.dumps({"action":"update-kill-armed","node":node,"operation_id":intent["request_id"],"candidate_artifacts":candidates},sort_keys=True),flush=True)
            return
        if state["properties"].get("ActiveState")=="failed":
            raise ValueError("kill fixture failed before the armed event")
        time.sleep(0.2)
    raise TimeoutError("kill armed event unconfirmed; preserve intent and collect without rearming")


def main(argv=None):
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--work-root",required=True)
    parser.add_argument("--node",choices=("arch","debian13"),default="arch")
    parser.add_argument("--mode",choices=("arm","collect"),required=True)
    parser.add_argument("--execute",action="store_true")
    args=parser.parse_args(argv)
    if args.mode=="arm" and not args.execute:
        parser.error("arming requires --execute and the registered disposable VM")
    root=lab.checked_root(args.work_root)
    record,plan=lab.load(root)
    intent=shared.load_intent(root,record,plan,args.node)
    (arm if args.mode=="arm" else collect)(root,record,plan,args.node,intent)


if __name__=="__main__":
    try:main()
    except (ValueError,OSError,TimeoutError) as exc:
        print("update kill controller refused: "+type(exc).__name__+": "+str(exc),file=sys.stderr)
        sys.exit(2)
