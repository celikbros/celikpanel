"""Collect a bounded allowlist of redacted fixture evidence; never credentials."""
import hashlib
import json
import os
import shutil
from pathlib import Path

ROOT=Path(os.environ.get('CELIKPANEL_SECONDARY_HOSTING_VM_ROOT','/var/tmp/cp-secondary-hosting-20260912'))
assert ROOT.parent==Path('/var/tmp') and ROOT.name in ('cp-secondary-hosting-20260912','cp-secondary-hosting-20260912-v2')
DEST=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel/docs/validation/secondary-hosting-20260912')/('final' if ROOT.name.endswith('-v2') else 'attempt-1')
assert not DEST.exists(), 'preserve previous collection'
DEST.mkdir(parents=True)
root_files=['plan.json','binary-sha256.json','source-manifest.json','compiler-provenance.json','source-comparison.json','shutdown.json','probe-continuation.json','continuation.json','probe-before-proof.json','probe-after-proof.json','probe-delete-result.json','final-before-proof.json','final-after-proof.json','final-delete-result.json']
guest_files=['draft.json','review.json','start.json','execution.json','dns-before-secondary.json','publisher-gate.json','ready-execution.json','ready.json','connection.json','publisher-confirmation.json','domain-create.json','dns-records.json','probe-domain-create.json','probe-dns-records.json','inspection.json','renewal-failure-evidence.json','renewal-before.json','renewal-after.json','post-renewal-ready.json']
for relative in root_files+[name+'/'+filename for name in ('dnsprimary','dnssecondary') for filename in guest_files]:
 source=ROOT/relative
 if not source.exists():continue
 raw=source.read_bytes()
 # This allowlist excludes sessions, bearer credentials, enrollment codes and
 # all private certificate material, even when those exist in the fixture root.
 value=json.loads(raw)
 def check(item):
  if isinstance(item,dict):
   for key,value in item.items():
    assert key not in ('credential','enrollment_code','password','private_key'),key
    check(value)
  elif isinstance(item,list):
   for value in item:check(value)
 check(value)
 destination=DEST/relative;destination.parent.mkdir(parents=True,exist_ok=True)
 shutil.copy2(source,destination)
manifest={str(path.relative_to(DEST)):hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(DEST.rglob('*.json'))}
(DEST/'artifact-sha256.json').write_text(json.dumps(manifest,indent=2))
print(json.dumps({'destination':str(DEST),'files':len(manifest)}))
