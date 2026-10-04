import json, sys, glob, os
E = sys.argv[1]
R = glob.glob(os.path.join(E, "raw", "results", "*"))[0]
print(sorted(os.listdir(R)))
cp = json.load(open(os.path.join(R, "reboot-checkpoint-1.json")))
print("checkpoint keys:", sorted(cp.keys())[:60])
def find(o, keys, path=""):
    if isinstance(o, dict):
        for k, v in o.items():
            p = path + "." + k
            if k in keys and not isinstance(v, (dict, list)):
                print(p, "=", v)
            find(v, keys, p)
    elif isinstance(o, list):
        for i, v in enumerate(o[:50]):
            find(v, keys, path + f"[{i}]")
find(cp, {"status", "safety_status", "kill_proven", "exit_code", "boot_id", "stage", "marker_to_sigkill_ms", "state_at_kill", "outcome", "classification", "samples", "dns_only_samples"})
kp = json.load(open(os.path.join(R, "kill-proof.json")))
print("kill-proof:", json.dumps(kp)[:1500])
