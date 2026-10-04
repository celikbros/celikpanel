#!/usr/bin/env python3
"""Kill only a bound disposable worker after its candidate firewall unit exists.

This adds a stricter checkpoint to the existing signed-worker/snapshot guard;
it never writes product state, receipts, unit files, or helper bytes.
"""
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys

spec=importlib.util.spec_from_file_location('firewall_bound_worker',Path(__file__).with_name('guest_bound_worker.py'))
bound=importlib.util.module_from_spec(spec);spec.loader.exec_module(bound)
OriginalNative=bound.Native

def validate_checkpoint(value,operation):
    fields={'schema','operation_id','generation','unit_sha256','helper_sha256','manifest_sha256'}
    if (not isinstance(value,dict) or set(value)!=fields or value['schema']!='celikpanel/lab-firewall-unit-fault/v1'
            or value['operation_id']!=operation or not re.fullmatch('[a-f0-9]{32}',operation)
            or any(not isinstance(value[k],str) or not re.fullmatch('[a-f0-9]{64}',value[k]) for k in ('generation','unit_sha256','helper_sha256','manifest_sha256'))):
        raise bound.probe.ProbeError('invalid exact firewall checkpoint')
    return value

class Native(OriginalNative):
    def __init__(self,args,intent):
        super().__init__(args,intent)
        self.checkpoint=validate_checkpoint(bound.probe.strict_object(bound.protected_read(bound.files.PRIVATE_ROOT/('firewall-unit-'+args.operation_id+'.json'),2048)),args.operation_id)

    def firewall_proof(self):
        c=self.checkpoint;root=Path('/usr/libexec/celikpanel/firewall')/c['generation'];unit='celikpanel-firewall-restore.service'
        targets=[(Path('/etc/systemd/system')/unit,c['unit_sha256'],0o644,16384),
                 (root/unit,c['unit_sha256'],0o644,16384),(root/'restore',c['helper_sha256'],0o755,32*1024*1024),
                 (root/'runtime.manifest',c['manifest_sha256'],0o644,4096)]
        for path,digest,mode,limit in targets:
            if hashlib.sha256(bound.protected_read(path,limit,mode)).hexdigest()!=digest:
                raise bound.base.MissedCheckpoint('candidate-firewall-unit-or-helper-differs')
        return dict(c)

    def installed(self,tick=lambda:None):
        result=super().installed(tick)
        if result is None:return None
        try:self.firewall_proof();tick()
        except (OSError,bound.probe.ProbeError,bound.base.MissedCheckpoint):return None
        return result

    def full_proof(self,snapshot,tick):
        proof=super().full_proof(snapshot,tick)
        return dict(proof,firewall_unit=self.firewall_proof())

    def kill(self):
        self.firewall_proof()
        super().kill()

if __name__=='__main__':
    bound.Native=Native
    try:raise SystemExit(bound.main())
    except (OSError,bound.probe.ProbeError) as exc:
        print('firewall unit fault refused: '+type(exc).__name__,file=sys.stderr);raise SystemExit(2)
