# Batch 10, read-only on the z05 primary while the run is held: boot, results directory (checkpoint, no result), controller processes, ledger jobs, agent journal lines
CELL=$1
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id) uptime=$(cut -d' ' -f1 /proc/uptime)"
R=/var/lib/celikpanel-dns-kill-matrix/results/$CELL
echo "== results directory"; ls -la --time-style=full-iso $R 2>&1
for f in $R/reboot-checkpoint-*.json; do [ -f "$f" ] && { echo "== $f sha256"; sha256sum "$f"; python3 -c 'import json,sys;d=json.load(open(sys.argv[1]));print(json.dumps({k:d.get(k) for k in ("schema","stage","ordinal","boot_id","cell_id","request_id") if k in d}))' "$f"; }; done
echo "result.json present: $(test -e $R/result.json && echo yes || echo no)"
echo "== controller/trigger processes"; pgrep -af 'dns-kill-run-cell|dns-kill-trigger|agent.kill' || echo "(none)"
for u in celikpanel-agent.service celikpanel-panel.service pdns.service; do echo "== $u $(systemctl show $u -p ActiveState,UnitFileState,MainPID --value | tr '\n' '/')"; done
echo "== zone-sync ledger jobs"
python3 - <<'PY'
import json
d = json.load(open("/var/lib/celikpanel-agent-private/service-mutations.json"))
for rid, j in sorted(d["jobs"].items(), key=lambda kv: kv[1].get("started_at", "")):
    if j.get("kind") == "dns_zone_sync" and j.get("status") != "succeeded":
        print(json.dumps(j, indent=1, sort_keys=True))
    else:
        print(json.dumps({k: j.get(k) for k in ("request_id", "kind", "target", "status", "phase", "attempt", "error_code", "updated_at")}))
PY
echo "== agent journal this boot (zone/peer/pending lines)"
journalctl -b -u celikpanel-agent.service --no-pager -o short-iso-precise | grep -iE 'zone|peer|pending|delet|dns_' | cut -c1-600 | tail -n 20
