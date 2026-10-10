import sys
from datetime import datetime
rows = []
for line in open(sys.argv[1], encoding="utf-8-sig"):
    p = line.split()
    if len(p) < 4: continue
    t = datetime.strptime(p[0], "%Y-%m-%dT%H:%M:%SZ")
    gib = float(p[3].split("=")[1])
    rows.append((t, gib))
gaps = [((b[0] - a[0]).total_seconds(), a[0], b[0]) for a, b in zip(rows, rows[1:])]
big = max(gaps)
low = min(rows, key=lambda r: r[1])
print(f"readings={len(rows)} first={rows[0][0]:%H:%M:%SZ} last={rows[-1][0]:%H:%M:%SZ} largest_gap={big[0]:.0f}s at {big[1]:%H:%M:%S}-{big[2]:%H:%M:%S} lowest_free_GiB={low[1]} at {low[0]:%H:%M:%SZ} first_GiB={rows[0][1]} last_GiB={rows[-1][1]}")
print("gaps over 45 s:", [(f"{a:%H:%M:%S}", f"{b:%H:%M:%S}", g) for g, a, b in gaps if g > 45])
