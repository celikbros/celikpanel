"""Capture compiler identity and read-only post-renewal setup checks."""
import hashlib
import json
import os
import subprocess
from pathlib import Path

ROOT = Path(os.environ['CELIKPANEL_SECONDARY_HOSTING_VM_ROOT'])
assert ROOT == Path('/var/tmp/cp-secondary-hosting-20260912-v2')
SOURCE = Path('/mnt/c/CELIKBROS PROJECTS/celikpanel')
GO = '/opt/celikpanel-test-toolchains/go1.26.5/go/bin/go'
environment = dict(os.environ, GOENV='off', GOWORK='off', GOTOOLCHAIN='local')
def run(args):
    result = subprocess.run(args, capture_output=True, text=True, env=environment, check=True)
    return result.stdout.strip()
provenance = {'compiler': GO, 'compiler_version': run([GO, 'version']), 'environment': json.loads(run([GO, 'env', '-json', 'GOVERSION', 'GOROOT', 'GOOS', 'GOARCH']))}
assert provenance['environment']['GOVERSION'] == 'go1.26.5'
for name in ('agent', 'panel-test'):
    info = run([GO, 'version', '-m', str(ROOT / name)])
    assert info.splitlines()[0].endswith(': go1.26.5')
    provenance[name] = {'sha256': hashlib.sha256((ROOT / name).read_bytes()).hexdigest(), 'go_build_info': info}
(ROOT / 'compiler-provenance.json').write_text(json.dumps(provenance, indent=2))
manifest = json.loads((ROOT / 'source-manifest.json').read_text())
comparison = {'unchanged_count': 0, 'changed_files': [], 'missing_files': [], 'added_go_files': []}
for filename, digest in manifest.items():
    current = SOURCE / filename
    if not current.is_file():
        comparison['missing_files'].append(filename)
    elif hashlib.sha256(current.read_bytes()).hexdigest() != digest:
        comparison['changed_files'].append(filename)
    else:
        comparison['unchanged_count'] += 1
for directory in ('cmd', 'internal'):
    for current in sorted((SOURCE / directory).rglob('*.go')):
        relative = str(current.relative_to(SOURCE))
        if relative not in manifest:
            comparison['added_go_files'].append(relative)
(ROOT / 'source-comparison.json').write_text(json.dumps(comparison, indent=2))
# Load only the driver's helper definitions; this does not start or mutate setup.
driver = (SOURCE / 'docs/validation/secondary-hosting-20260912/fixtures/run.py').read_text()
exec(driver.split('\nwith concurrent.futures.ThreadPoolExecutor')[0])
for name in nodes:
    value = api(name, '/api/v1/setup?check=1', trusted=True)
    save(name, 'post-renewal-ready', value)
    assert value['status'] == 'ready', value
    assert all(check['state'] == 'ready' for check in value['checks']), value['checks']
print('POST_RENEWAL_BOTH_SETUP_CHECKS_READY')
print(json.dumps(comparison, indent=2))
