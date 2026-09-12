"""Build an exact local source snapshot and stage only marked disposable guests."""
import hashlib
import json
import os
import shutil
import subprocess
from pathlib import Path

SOURCE = Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
ROOT = Path(os.environ.get('CELIKPANEL_SECONDARY_HOSTING_VM_ROOT', '/var/tmp/cp-secondary-hosting-20260912'))
assert ROOT.name in ('cp-secondary-hosting-20260912', 'cp-secondary-hosting-20260912-v2') and (ROOT / 'plan.json').is_file()
checkout = ROOT / 'source'
assert not checkout.exists(), 'retain exact prior candidate'
checkout.mkdir(mode=0o700)
for directory in ('cmd', 'internal', 'deploy'):
    shutil.copytree(SOURCE / directory, checkout / directory)
for filename in ('go.mod', 'go.sum'):
    shutil.copy2(SOURCE / filename, checkout / filename)
manifest = {str(path.relative_to(checkout)): hashlib.sha256(path.read_bytes()).hexdigest() for path in sorted(checkout.rglob('*')) if path.is_file()}
(ROOT / 'source-manifest.json').write_text(json.dumps(manifest, indent=2))
environment = dict(os.environ, GOENV='off', GOWORK='off', GOTOOLCHAIN='local', CGO_ENABLED='1')
environment['PATH'] = '/opt/celikpanel-test-toolchains/go1.26.5/go/bin:/usr/bin:/bin'
for artifact, args, prefix in [('agent', ['build'], 'main'), ('panel-test', ['test', '-c'], 'github.com/alicelik/celikpanel/cmd/panel')]:
    package = './cmd/agent' if artifact == 'agent' else './cmd/panel'
    flags = f'-X {prefix}.buildVersion=secondary-hosting-fixture -X {prefix}.buildCommit=64a0000000000000000000000000000000000000'
    with (ROOT / (artifact + '-build.log')).open('wb') as log:
        result = subprocess.run(['go', *args, '-trimpath', '-ldflags=' + flags, '-o', str(ROOT / artifact), package], cwd=checkout, env=environment, stdout=log, stderr=subprocess.STDOUT)
    assert result.returncode == 0, (ROOT / (artifact + '-build.log')).read_text()[-6000:]
    print(artifact + ' built', flush=True)
proof = {name: hashlib.sha256((ROOT / name).read_bytes()).hexdigest() for name in ('agent', 'panel-test')}
(ROOT / 'binary-sha256.json').write_text(json.dumps(proof, indent=2))
original = SOURCE / 'docs/validation/server-setup-profiles-20260911/fixtures/setup-dns-profile-agent-v2.py'
script = original.read_text().replace('/var/tmp/cp-setup-dns-profile-20260911-v2', str(ROOT)).replace("b'cp-profile65-'", "b'cp-secondary-hosting-'").replace("SOURCE=Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')", 'SOURCE=Path(' + repr(str(checkout)) + ')')
(ROOT / 'stage-agent.py').write_text(script)
result = subprocess.run(['python3', str(ROOT / 'stage-agent.py')])
assert result.returncode == 0
print('SECONDARY_HOSTING_AGENT_READY', flush=True)
