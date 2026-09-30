#!/bin/bash
# Batch 10, read-only ledger sampler on the primary. Args: OUTDIR [SECONDS]
# Plain file copy of /var/lib/celikpanel-agent-private/service-mutations.json whenever its bytes change (poll 0.1 s),
# plus one summary line per change: every dns_zone_sync job's status, phase state, attempt, lease and error code.
# Stops when /var/tmp/cp-b12-ledwatch.stop exists or after SECONDS (default 7200).
OUT=$1; SECS=${2:-7200}
mkdir -p "$OUT/copies"
T=$OUT/timeline.log
src=/var/lib/celikpanel-agent-private/service-mutations.json
lsha=none
end=$(( $(date +%s) + SECS ))
echo "$(date -u +%T.%N) start boot_id=$(cat /proc/sys/kernel/random/boot_id)" >> "$T"
while [ "$(date +%s)" -lt "$end" ] && [ ! -e /var/tmp/cp-b12-ledwatch.stop ]; do
  if [ -f "$src" ]; then cur=$(sha256sum "$src" | cut -c1-64); else cur=absent; fi
  if [ "$cur" != "$lsha" ]; then
    ts=$(date -u +%H%M%S.%N | cut -c1-10)
    [ "$cur" != absent ] && cp -p "$src" "$OUT/copies/$ts-ledger-${cur:0:12}.json" 2>/dev/null
    sum=$(python3 - "$OUT/copies/$ts-ledger-${cur:0:12}.json" <<'PY' 2>&1
import json, sys
try:
    d = json.load(open(sys.argv[1]))
except Exception as e:
    print("unreadable", e); sys.exit()
out = []
for rid, j in sorted(d.get("jobs", {}).items(), key=lambda kv: kv[1].get("started_at", "")):
    if j.get("kind") != "dns_zone_sync":
        continue
    ph = j.get("phase", "").split("/")
    out.append(f'{rid[:8]} {j.get("status")} {ph[3] if len(ph) > 3 else j.get("phase")} att={j.get("attempt")} lease={j.get("lease_expires_at")} code={j.get("error_code","")} upd={j.get("updated_at")}')
print(" | ".join(out))
PY
)
    echo "$(date -u +%T.%N) CHANGE ledger ${lsha:0:12} -> ${cur:0:12} agent=$(systemctl show celikpanel-agent.service -p ActiveState,MainPID --value 2>/dev/null | tr '\n' '/') :: $sum" >> "$T"
    lsha=$cur
  fi
  sleep 0.1
done
echo done >> "$T"
