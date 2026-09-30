# Read-only: condense a peer-dns-sampler.log to the lines where any catalog/child answer changes. usage: samp.py FILE
import sys, re
last = None; n = 0; first = None; lastts = None
for line in open(sys.argv[1]):
    line = line.rstrip("\n")
    if line.startswith("####"):
        print(line); last = None; continue
    parts = line.split(" ")
    ts = parts[0]; kv = dict(p.split("=", 1) for p in parts[1:] if "=" in p)
    key = tuple((k, kv[k]) for k in sorted(kv) if (":cat:" in k or ":s2soa:" in k or ":soa:udp" in k))
    n += 1; first = first or ts; lastts = ts
    if key != last:
        print(ts, " ".join(f"{k}={v}" for k, v in key))
        last = key
print(f"samples={n} first={first} last={lastts}")
