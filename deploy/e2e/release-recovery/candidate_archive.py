#!/usr/bin/env python3
"""Read-only verification for explicitly unpublished local candidate artifacts."""
from __future__ import annotations
import hashlib
import os
from pathlib import Path, PurePosixPath
import re
import stat
import subprocess
import tarfile

HEX=re.compile(r'[0-9a-f]{40}(?:[0-9a-f]{24})?\Z')
SHA=re.compile(r'[0-9a-f]{64}\Z')
MAX_ARCHIVE=100*1024*1024
MAX_PAYLOAD=512*1024*1024
REQUIRED=('bin/agent','bin/panel','bin/schema17-bridge','install.sh','update.sh','rollback.sh','bootstrap-prebuilt-update.sh','deploy/release-transaction-guard.sh','deploy/release-transaction-start-guard.sh','deploy/release-recovery-foundation.sh','deploy/release-recovery-runner.sh','deploy/release-recovery.protocol','deploy/systemd/celikpanel-agent.service','deploy/systemd/celikpanel-panel.service','libexec/get.sh','web/dist/index.html')


def member_path(name,root):
    value=name.rstrip('/')
    parsed=PurePosixPath(value)
    if (not value or str(parsed)!=value or parsed.is_absolute() or '..' in parsed.parts
            or any(ord(char)<32 or char=='\\' for char in value)
            or (value!=root and not value.startswith(root+'/'))):
        raise ValueError('unsafe candidate member path')
    return value[len(root)+1:] if value!=root else ''


def inspect_archive(path,expected_sha256):
    if not SHA.fullmatch(expected_sha256):raise ValueError('candidate archive hash must be exact')
    fd=os.open(path,os.O_RDONLY|os.O_NOFOLLOW|os.O_CLOEXEC)
    with os.fdopen(fd,'rb') as stream:
        before=os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_nlink!=1 or not 0<before.st_size<=MAX_ARCHIVE:raise ValueError('candidate archive is not a bounded regular file')
        digest=hashlib.sha256()
        for chunk in iter(lambda:stream.read(1048576),b''):digest.update(chunk)
        if digest.hexdigest()!=expected_sha256:raise ValueError('candidate archive hash differs')
        stream.seek(0)
        files={};small={};seen=set();total=0;root=None
        with tarfile.open(fileobj=stream,mode='r:gz') as archive:
            for member in archive:
                if root is None:
                    root=member.name.split('/',1)[0]
                    if not re.fullmatch(r'celikpanel-v[0-9A-Za-z.+_-]{1,100}',root):raise ValueError('candidate root name is not explicit versioned artifact')
                relative=member_path(member.name,root)
                if relative in seen or len(seen)>=20000 or not (member.isdir() or member.isfile()):raise ValueError('duplicate or special candidate member')
                seen.add(relative)
                if not relative and not member.isdir():raise ValueError('candidate root is not directory')
                if not member.isfile():continue
                total+=member.size
                if member.size<0 or member.size>128*1024*1024 or total>MAX_PAYLOAD:raise ValueError('candidate payload exceeds bound')
                data=archive.extractfile(member)
                digest=hashlib.sha256()
                captured=bytearray() if relative in ('SHA256SUMS','release.version','release.commit','release.tree') else None
                for chunk in iter(lambda:data.read(1048576),b''):
                    digest.update(chunk)
                    if captured is not None:
                        captured.extend(chunk)
                        if len(captured)>8*1024*1024:raise ValueError('candidate metadata exceeds bound')
                files[relative]=digest.hexdigest()
                if captured is not None:small[relative]=bytes(captured)
        after=os.fstat(stream.fileno())
    if (before.st_dev,before.st_ino,before.st_size,before.st_mtime_ns,before.st_ctime_ns)!=(after.st_dev,after.st_ino,after.st_size,after.st_mtime_ns,after.st_ctime_ns):raise ValueError('candidate archive changed while reading')
    if any(name not in files for name in REQUIRED) or small.get('release.version')!=b'1\n':raise ValueError('candidate native payload/provenance is incomplete')
    commit=small.get('release.commit',b'').decode('ascii').strip();tree=small.get('release.tree',b'').decode('ascii').strip()
    if not HEX.fullmatch(commit) or not HEX.fullmatch(tree):raise ValueError('candidate commit/tree identity is invalid')
    canonical=''.join(files[name]+'  ./'+name+'\n' for name in sorted(files) if name!='SHA256SUMS').encode()
    if small.get('SHA256SUMS')!=canonical:raise ValueError('candidate full checksum inventory differs')
    return {'schema':'celikpanel/local-candidate-artifact/v1','provenance':'unpublished-local-build-not-signed-agent-admission','archive_sha256':expected_sha256,'archive_size':before.st_size,'root_name':root,'version':root.removeprefix('celikpanel-'),'commit':commit,'tree':tree,'manifest_sha256':files['SHA256SUMS'],'files':files,'payload_bytes':total}


def verify_committed_source(candidate,repository):
    commit=candidate['commit']
    actual=subprocess.run(['git','-C',str(repository),'rev-parse',commit+'^{tree}'],check=True,capture_output=True,text=True).stdout.strip()
    if actual!=candidate['tree']:raise ValueError('candidate source tree differs from real committed source')
    names=[];queries=[]
    for name in candidate['files']:
        if name=='SHA256SUMS' or name.startswith(('bin/','web/dist/','release.')):continue

        # The kit embeds reviewed static sources under a separate data root.
        # Its binaries and generated manifest are covered by the full archive
        # inventory and runtime admission, not fictitious Git source blobs.
        if name.startswith('recovery-runtime/bin/') or name=='recovery-runtime/runtime.manifest':continue
        source='download-portal/get.sh' if name=='libexec/get.sh' else name
        if name.startswith('recovery-runtime/'):
            source=name.removeprefix('recovery-runtime/')
            if source=='deploy/recovery/runtime-entry.sh':source='deploy/release-recovery-runner.sh'

        names.append(name);queries.append(commit+':'+source)
    result=subprocess.run(['git','-C',str(repository),'cat-file','--batch'],input=('\n'.join(queries)+'\n').encode(),check=True,capture_output=True)
    raw=result.stdout
    if len(raw)>64*1024*1024:raise ValueError('committed source proof exceeds bound')
    position=0
    for name in names:
        end=raw.find(b'\n',position)
        if end<0:raise ValueError('committed source proof truncated')
        fields=raw[position:end].split();position=end+1
        if len(fields)!=3 or fields[1]!=b'blob' or not fields[2].isdigit():raise ValueError('candidate contains uncommitted source: '+name)
        size=int(fields[2]);data=raw[position:position+size];position+=size
        if len(data)!=size or raw[position:position+1]!=b'\n':raise ValueError('committed source blob proof malformed')
        position+=1
        if hashlib.sha256(data).hexdigest()!=candidate['files'][name]:raise ValueError('candidate source bytes differ from commit: '+name)
    if position!=len(raw):raise ValueError('extra committed source proof data')
    return {'commit':commit,'tree':actual,'verified_static_files':len(names),'method':'real Git commit/tree and every packaged static source blob'}
