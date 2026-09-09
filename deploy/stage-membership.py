#!/usr/bin/env python3
"""Stage a verified immutable app outside httpdocs. Portal exchange selects it."""
import argparse, hashlib, json, os, re, subprocess, tarfile
from pathlib import Path

FILES={'http.php','init.php','src/Service.php','terms.php','view.php'}
def main():
    p=argparse.ArgumentParser();p.add_argument('--root',required=True,type=Path);p.add_argument('--archive',required=True,type=Path)
    p.add_argument('--sha256',required=True);p.add_argument('--release',required=True);p.add_argument('--public-key',required=True)
    p.add_argument('--php',required=True);a=p.parse_args();os.umask(0o077)
    for value in [a.sha256,a.release,a.public_key]:
        if not re.fullmatch('[a-f0-9]{64}',value):raise ValueError('Invalid pinned identity')
    if a.root.resolve(strict=True)!=a.root or a.root.stat().st_uid!=os.getuid():raise ValueError('Unsafe root')
    if a.archive.is_symlink() or not a.archive.is_file() or a.archive.stat().st_size>512*1024:raise ValueError('Unsafe archive')
    if hashlib.sha256(a.archive.read_bytes()).hexdigest()!=a.sha256:raise ValueError('Archive digest mismatch')
    with tarfile.open(a.archive,'r:') as archive:
        entries=archive.getmembers()
        if len(entries)!=len(FILES) or {e.name for e in entries}!=FILES or any(not e.isfile() or e.size>256*1024 for e in entries):raise ValueError('Unexpected archive members')
        data={e.name:archive.extractfile(e).read() for e in entries}
    manifest={name:{'sha256':hashlib.sha256(b).hexdigest(),'size':len(b)} for name,b in data.items()}
    identity=hashlib.sha256(json.dumps(manifest,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    if identity!=a.release:raise ValueError('App release identity mismatch')
    base=a.root/'membership-app'
    for path in [base,base/'releases',base/'releases'/identity,base/'releases'/identity/'src']:
        if path.is_symlink():raise ValueError('Symlink refused')
        path.mkdir(mode=0o700,exist_ok=True)
        if path.stat().st_uid!=os.getuid() or path.stat().st_mode&0o022:raise ValueError('Unsafe application owner/mode')
    release=base/'releases'/identity
    for name,b in data.items():
        target=release/name
        if target.is_symlink():raise ValueError('Symlink refused')
        if target.exists():
            if target.read_bytes()!=b:raise ValueError('Immutable app differs')
        else:
            with target.open('xb') as f:f.write(b);f.flush();os.fsync(f.fileno())
        subprocess.run([a.php,'-l',str(target)],check=True,capture_output=True,timeout=15)
    result=subprocess.run([a.php,str(release/'init.php'),str(a.root/'membership-private')],check=True,capture_output=True,text=True,timeout=30)
    if result.stdout.strip()!='license_public_key='+a.public_key:raise ValueError('Central signing identity mismatch')
    print(json.dumps({'status':'staged','release':identity,'path':str(release),'private_state':str(a.root/'membership-private')}))
if __name__=='__main__':main()
