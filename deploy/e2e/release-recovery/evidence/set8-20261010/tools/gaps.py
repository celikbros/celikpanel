"""set8: gaps between the Windows watcher's readings (read only). usage: gaps.py c-drive-watch.txt"""
import datetime, sys
t = [datetime.datetime.strptime(l.split()[0], "%Y-%m-%dT%H:%M:%SZ") for l in open(sys.argv[1]) if l.strip()]
g = [((b - a).total_seconds(), a, b) for a, b in zip(t, t[1:])]
print(f"readings {len(t)} from {t[0]:%H:%M:%SZ} to {t[-1]:%H:%M:%SZ}; largest gap {max(g)[0]:.0f} s ({max(g)[1]:%H:%M:%S}-{max(g)[2]:%H:%M:%S}); gaps over 31 s: {sum(1 for x in g if x[0] > 31)}")
lo = min((float(l.split('free_GiB=')[1]), l.split()[0]) for l in open(sys.argv[1]) if 'free_GiB=' in l)
print(f"lowest reading {lo[0]} GiB at {lo[1]}")
