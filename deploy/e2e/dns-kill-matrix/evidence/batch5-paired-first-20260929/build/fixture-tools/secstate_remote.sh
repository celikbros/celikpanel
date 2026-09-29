# Read-only capture of the guest's secondary state (BIND or PowerDNS). No writes outside /tmp.
CAT=catalog-c000020b.celikpanel.invalid
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
for u in named.service bind9.service pdns.service celikpanel-agent.service celikpanel-panel.service; do
  echo "== $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ExecMainStartTimestamp 2>&1
done
echo "== ss 53"; ss -H -lntup 'sport = :53'
if command -v rndc >/dev/null 2>&1 && systemctl is-active --quiet named.service; then
  echo "== rndc status"; timeout 10 rndc status 2>&1
  for z in $CAT s1-kill.test; do
    echo "== rndc zonestatus $z"; timeout 10 rndc zonestatus $z 2>&1
    echo "== rndc showzone $z"; timeout 10 rndc showzone $z 2>&1
  done
else
  echo "== rndc: not run (rndc absent or named inactive)"
fi
echo "== named-checkconf -l"; command -v named-checkconf >/dev/null && named-checkconf -l 2>&1 | head -30
echo "== managed BIND files (/var/cache/bind, no follow)"
find /var/cache/bind -xdev -printf '%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p -> %l\n' 2>&1 | sort -k5 | head -100
echo "== secondary zone files (regular files under /var/cache/bind), metadata, hash, text"
find /var/cache/bind -xdev -type f 2>/dev/null | sort | while read -r f; do
  echo "-- $f"; stat -c 'size=%s mode=%a owner=%U:%G mtime=%y' "$f"; sha256sum "$f"
  case "$f" in
    *.conf) ;;
    *)
      if command -v named-compilezone >/dev/null; then
        case "$f" in *catalog*) z=$CAT;; *) z=s1-kill.test;; esac
        for fmt in raw text; do
          if named-compilezone -q -f $fmt -F text -o /tmp/cp-b5-zone.txt $z "$f" >/dev/null 2>&1; then echo "(zone $z, format $fmt)"; head -40 /tmp/cp-b5-zone.txt; rm -f /tmp/cp-b5-zone.txt; break; fi
        done
      fi ;;
  esac
done
if [ -e /var/lib/powerdns/pdns.sqlite3 ]; then
  echo "== PowerDNS database"; ls -la --time-style=full-iso /var/lib/powerdns; sha256sum /var/lib/powerdns/pdns.sqlite3
  python3 - <<'PY'
import sqlite3, json
c = sqlite3.connect("file:/var/lib/powerdns/pdns.sqlite3?mode=ro", uri=True, timeout=5)
cols = [r[1] for r in c.execute("PRAGMA table_info(domains)")]
print("domains columns:", cols)
for row in c.execute("SELECT * FROM domains ORDER BY id"):
    print("domain:", json.dumps(dict(zip(cols, row))))
for row in c.execute("SELECT d.name, r.name, r.type, r.content, r.ttl FROM records r JOIN domains d ON d.id=r.domain_id ORDER BY d.name, r.name, r.type"):
    print("record:", json.dumps(row))
c.close()
PY
else
  echo "== PowerDNS database absent"
fi
echo "== pdns journal (transfer/notify/catalog lines, all boots)"
journalctl -u pdns.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'xfr|transfer|notif|catalog|consumer|slave|secondary' | tail -n 80
echo "== named journal (transfer/notify/catalog/zone lines, all boots)"
journalctl -u named.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'xfr|transfer|notif|catalog|zone' | tail -n 100
echo "== agent journal: catalog/producer lines (all boots)"
journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'catalog|producer|policy' | cut -c1-700 | tail -n 40
