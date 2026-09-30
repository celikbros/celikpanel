# Batch 10, read-only on the primary: boot, units, the zone-sync ledger jobs (full entries), inspection enrollment files (metadata only), agent journal this boot.
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id) uptime=$(cut -d' ' -f1 /proc/uptime)"
journalctl --list-boots --no-pager 2>&1 | tail -n 4
for u in celikpanel-agent.service celikpanel-panel.service pdns.service; do echo "== $u"; systemctl show $u -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ActiveEnterTimestamp,NRestarts 2>&1; done
echo "== binaries"; sha256sum /opt/celikpanel/bin/agent /opt/celikpanel/bin/agent.kill /opt/celikpanel/bin/panel /opt/celikpanel/bin/dns-kill-trigger 2>&1; stat -c '%n %U:%G %a %s %y' /opt/celikpanel/bin/dns-kill-trigger
echo "== zone-sync ledger jobs (full entries)"
python3 - <<'PY'
import json
d = json.load(open("/var/lib/celikpanel-agent-private/service-mutations.json"))
print("version:", d.get("version"))
for rid, j in sorted(d["jobs"].items(), key=lambda kv: kv[1].get("started_at", "")):
    if j.get("kind") == "dns_zone_sync" and j.get("status") != "succeeded":
        print(json.dumps(j, indent=1, sort_keys=True))
    else:
        print(json.dumps({k: j.get(k) for k in ("request_id", "kind", "target", "status", "phase", "attempt", "updated_at")}))
PY
echo "== agent-private (ls only; inspection keys are not read)"
ls -la --time-style=full-iso /var/lib/celikpanel-agent-private/ /var/lib/celikpanel-agent-private/dns-peer-inspection-keys 2>&1
sha256sum /var/lib/celikpanel-agent-private/service-mutations.json
echo "== dns-peer-enroll primary-status"; /root/dns-owner-tools/dns-peer-enroll primary-status 2>&1
echo "== agent journal, this boot"
journalctl -b -u celikpanel-agent.service --no-pager -o short-iso-precise 2>&1 | cut -c1-900 | tail -n 80
