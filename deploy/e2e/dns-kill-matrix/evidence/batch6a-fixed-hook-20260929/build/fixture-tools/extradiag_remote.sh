# Read-only diagnostics after a failed cell. No writes outside /tmp.
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
for f in /etc/bind/named.conf /etc/bind/named.conf.local /etc/bind/named.conf.options /etc/named.conf; do echo "== $f"; cat "$f"; done
echo "== /var/cache/bind/celikpanel (ls -la, no follow)"; ls -la --time-style=full-iso /var/cache/bind/celikpanel /var/cache/bind/celikpanel/generations /var/named/celikpanel /var/named/celikpanel/generations 2>&1
echo "== find /var/cache/bind /var/named -maxdepth 3 (symlinks shown)"; find /var/cache/bind /var/named -maxdepth 3 -printf '%M %u:%g %p -> %l\n' 2>&1
echo "== /run candidates"; ls -la --time-style=full-iso /run/named /run/celikpanel* /run/bind 2>&1
echo "== zones.conf of staged generation"; cat /var/cache/bind/celikpanel/generations/*/zones.conf /var/named/celikpanel/generations/*/zones.conf 2>&1
echo "== agent journal (DNS lines, all boots)"; journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise 2>&1 | grep -iE 'dns|bind|named|journal|retry|switch' | cut -c1-600 | tail -n 60
echo "== ledger job"; python3 -c 'import json;d=json.load(open("/var/lib/celikpanel-agent-private/service-mutations.json"));print(json.dumps(d,indent=1)[:4000])'
echo "== journal phase"; python3 -c 'import json;d=json.load(open("/var/lib/celikpanel-agent-private/dns-engine-switch-journal.json"));print(d.get("schema"),d.get("phase"),d.get("request_id"))' 2>&1
