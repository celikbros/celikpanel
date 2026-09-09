#!/usr/bin/env python3
"""Build an immutable central membership application; never includes secrets."""
import argparse, hashlib, io, json, tarfile
from pathlib import Path

FILES=('http.php','init.php','src/Service.php','terms.php','view.php')
def build(source):
    content={name:(source/name).read_bytes() for name in FILES}
    manifest={name:{'sha256':hashlib.sha256(data).hexdigest(),'size':len(data)} for name,data in content.items()}
    identity=hashlib.sha256(json.dumps(manifest,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    return identity,content
def main():
    p=argparse.ArgumentParser();p.add_argument('--output',type=Path);p.add_argument('--identity-only',action='store_true');a=p.parse_args()
    identity,files=build(Path(__file__).resolve().parent.parent/'portal-membership')
    if a.identity_only:print(identity);return
    if a.output is None:p.error('--output is required')
    with tarfile.open(a.output,'w',format=tarfile.USTAR_FORMAT) as archive:
        for name,data in sorted(files.items()):
            entry=tarfile.TarInfo(name);entry.size=len(data);entry.mode=0o600;entry.mtime=0
            archive.addfile(entry,io.BytesIO(data))
    print(json.dumps({'release':identity,'archive_sha256':hashlib.sha256(a.output.read_bytes()).hexdigest(),'size':a.output.stat().st_size}))
if __name__=='__main__':main()
