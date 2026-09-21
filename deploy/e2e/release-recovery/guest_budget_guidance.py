#!/usr/bin/env python3
"""Read-only guidance capture in the sealed, already interrupted budget lab."""
import argparse
import grp
import importlib.util
import json
from pathlib import Path
import stat
import sys
spec=importlib.util.spec_from_file_location('guidance_result',Path(__file__).with_name('guest_dispatch_budget_result.py'))
r=importlib.util.module_from_spec(spec);sys.modules[spec.name]=r;spec.loader.exec_module(r)
b=r.f.boot


def capture(operation,phase):
    result=r.sample(operation,phase)
    root=b.OBSERVATIONS;gid=grp.getgrnam('celikpanel').gr_gid
    info=root.lstat()
    if not stat.S_ISDIR(info.st_mode) or info.st_uid!=0 or info.st_gid!=gid or stat.S_IMODE(info.st_mode)!=0o750:
        raise ValueError('unsafe observation root')
    path=root/(operation+'.status')
    def identity():
        cp=b.run(['/usr/bin/stat','-Lc','%d:%i:%s:%y:%z','--',str(path)])
        if cp.returncode:raise ValueError('status identity unavailable')
        return cp.stdout.decode().strip()
    first=identity();raw=b.read(path,2048,0o640,gid)
    automatic=b.read(root/(operation+'.automatic'),2048,0o640,gid)
    texts={}
    for language in ('en','tr'):
        cp=b.run(['/usr/libexec/celikpanel/recovery','status','--request-id',operation,'--lang',language])
        if cp.returncode or len(cp.stdout)>8192:raise ValueError('native CLI text unavailable')
        texts[language]=cp.stdout.decode()
    cli=r.cli(operation)
    if identity()!=first or b.read(path,2048,0o640,gid)!=raw or b.read(root/(operation+'.automatic'),2048,0o640,gid)!=automatic:
        raise ValueError('observation republished during capture; sample again')
    if cli!=result['cli']:raise ValueError('CLI changed during capture')
    result.update(schema='celikpanel/native-budget-guidance/v1',status_raw=raw.decode('ascii'),
                  status_identity=first,automatic_raw=automatic.decode('ascii'),cli_text=texts)
    b.once(b.ROOT/('budget-guidance-'+phase+'-'+operation+'.json'),b.encoded(result))
    return result


if __name__=='__main__':
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('phase',choices=('exhausted','terminal'));p.add_argument('--operation-id',required=True);a=p.parse_args()
    print(json.dumps(capture(a.operation_id,a.phase),sort_keys=True))
