"""Stop only exact overlay-bound local QEMU guests; retain all artifacts."""
import json
import os
import signal
import socket
import time
from pathlib import Path

ROOT=Path(os.environ.get('CELIKPANEL_SECONDARY_HOSTING_VM_ROOT','/var/tmp/cp-secondary-hosting-20260912'))
assert ROOT.parent==Path('/var/tmp') and ROOT.name in ('cp-secondary-hosting-20260912','cp-secondary-hosting-20260912-v2')
nodes=json.loads((ROOT/'plan.json').read_text())
report={'root':str(ROOT),'controller_stops':[],'guests':[]}
driver='/mnt/c/CELIKBROS PROJECTS/celikpanel/docs/validation/secondary-hosting-20260912/fixtures/run.py'
for process in Path('/proc').iterdir():
 if not process.name.isdigit():continue
 try:
  args=(process/'cmdline').read_bytes().split(b'\0')
  environment=(process/'environ').read_bytes().split(b'\0')
 except (FileNotFoundError,PermissionError):continue
 if driver.encode() not in args:continue
 configured=[value.split(b'=',1)[1].decode() for value in environment if value.startswith(b'CELIKPANEL_SECONDARY_HOSTING_VM_ROOT=')]
 actual=configured[0] if configured else '/var/tmp/cp-secondary-hosting-20260912'
 if actual!=str(ROOT):continue
 os.kill(int(process.name),signal.SIGTERM)
 report['controller_stops'].append(int(process.name))
for name,row in nodes.items():
 folder=ROOT/name;pid=int((folder/'qemu.pid').read_text())
 args=Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0')
 assert row['qemu_name'].encode() in args and any(('file='+str(folder/'overlay.qcow2')).encode() in arg for arg in args)
 with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as connection:
  connection.settimeout(5);connection.connect(str(folder/'qmp.sock'))
  stream=connection.makefile('rwb',buffering=0);hello=json.loads(stream.readline());assert 'QMP' in hello
  stream.write(b'{"execute":"qmp_capabilities"}\n')
  while True:
   response=json.loads(stream.readline())
   if 'return' in response:break
  stream.write(b'{"execute":"quit"}\n')
 for attempt in range(50):
  if not Path(f'/proc/{pid}/cmdline').exists():break
  try:
   if row['qemu_name'].encode() not in Path(f'/proc/{pid}/cmdline').read_bytes().split(b'\0'):break
  except FileNotFoundError:break
  time.sleep(.1)
 else:raise RuntimeError('fixture guest did not stop')
 report['guests'].append({'name':name,'pid':pid,'stopped':True,'overlay':str(folder/'overlay.qcow2')})
(ROOT/'shutdown.json').write_text(json.dumps(report,indent=2))
print(json.dumps(report))
