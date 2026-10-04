# Batch 9, read-only capture on the PowerDNS primary at cell end:
# PowerDNS journal lines (all boots) containing notify / spurious / "failed after retries" / "does not resolve", with timestamps;
# also-notify settings in /etc/powerdns/pdns.conf and /etc/powerdns/pdns.d/*; ALSO-NOTIFY (and all) domainmetadata rows
# read from a private copy of the database (+WAL) in a temporary directory (the live database is not opened).
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
echo "== journalctl --list-boots"; journalctl --list-boots --no-pager 2>&1
echo "== pdns.service journal, all boots, lines matching notify|spurious|failed after retries|does not resolve (case-insensitive)"
journalctl -u pdns.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'notif|spurious|failed after retries|does not resolve'
echo "matching lines: $(journalctl -u pdns.service --no-pager -o short-iso-precise 2>/dev/null | grep -ciE 'notif|spurious|failed after retries|does not resolve')"
echo "== pdns.service journal line count (all boots): $(journalctl -u pdns.service --no-pager -q -o cat 2>/dev/null | wc -l)"
echo "== also-notify in /etc/powerdns/pdns.conf and /etc/powerdns/pdns.d/* (case-insensitive, with file and line)"
ls -la --time-style=full-iso /etc/powerdns/pdns.d 2>&1
grep -HniE 'also-notify|only-notify|notify' /etc/powerdns/pdns.conf /etc/powerdns/pdns.d/* 2>&1 || echo "(no also-notify/notify setting found)"
echo "== domainmetadata (private copy of the database)"
if [ -f /var/lib/powerdns/pdns.sqlite3 ]; then
  D=$(mktemp -d)
  cp -p /var/lib/powerdns/pdns.sqlite3 $D/db.sqlite3
  [ -f /var/lib/powerdns/pdns.sqlite3-wal ] && cp -p /var/lib/powerdns/pdns.sqlite3-wal $D/db.sqlite3-wal && echo "(copy includes the WAL)"
  python3 - $D/db.sqlite3 <<'PY'
import sqlite3, sys, json
c = sqlite3.connect(sys.argv[1], timeout=5)
print("ALSO-NOTIFY rows:", json.dumps(c.execute("SELECT d.name, m.kind, m.content FROM domainmetadata m LEFT JOIN domains d ON d.id=m.domain_id WHERE upper(m.kind)='ALSO-NOTIFY' ORDER BY d.name").fetchall()))
print("all domainmetadata rows:")
for r in c.execute("SELECT d.name, m.kind, m.content FROM domainmetadata m LEFT JOIN domains d ON d.id=m.domain_id ORDER BY d.name, m.kind"):
    print("  ", json.dumps(r))
print("domains:")
for r in c.execute("SELECT id,name,type,master,account,options,catalog,notified_serial,last_check FROM domains ORDER BY id"):
    print("  ", json.dumps(r))
PY
  rm -rf $D
else
  echo "/var/lib/powerdns/pdns.sqlite3 absent"
fi
