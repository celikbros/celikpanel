# set4b: read-only smoke of the guest helper's readers on the WSL host (no guard, no trace instance, nothing changed).
import importlib.util, json, subprocess, sys, tempfile, os
spec = importlib.util.spec_from_file_location("h", "/var/tmp/cp-set4b-run/t4b/deploy/e2e/release-recovery/guest_set4b_native.py")
h = importlib.util.module_from_spec(spec); spec.loader.exec_module(h)
v = h.read_agent_view({})
print(json.dumps({u: (x["returncode"], x["stdout"]) for u, x in v["readUnitFailure"].items()}))
print(json.dumps(v["postfixMasterProcess"])[:300])
print(json.dumps(v["units"]["postfix@-.service"])[:400])
d = tempfile.mkdtemp()
open(d + "/s.py", "w").write(h.SAMPLER)
subprocess.run([sys.executable, "-I", d + "/s.py", d + "/o.jsonl", "1.0", "1", "0.002"])
rows = [json.loads(l) for l in open(d + "/o.jsonl")]
print(len(rows), rows[0])
print("tracefs:", os.path.isdir("/sys/kernel/tracing/events/sched/sched_process_exec"), "strace:", h.shutil.which("strace"))
