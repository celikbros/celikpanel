# Read-only capture of the panel-free native BIND secondary (Arch): catalog members, zone status, transferred files, transfer/notify journal lines.
CAT=catalog-c000020a.celikpanel.invalid
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
echo "== named.service"; systemctl show named.service -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ExecMainStartTimestamp,NRestarts 2>&1
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== management binaries (must be absent)"; ls -la /opt/celikpanel 2>&1
echo "== /etc/named.conf"; ls -la --time-style=full-iso /etc/named.conf; sha256sum /etc/named.conf
if systemctl is-active --quiet named.service; then
  echo "== rndc status"; timeout 10 rndc status 2>&1
  for z in $CAT s1-kill.test s2.s1-kill.test; do echo "== rndc zonestatus $z"; timeout 10 rndc zonestatus $z 2>&1; echo "== rndc showzone $z"; timeout 10 rndc showzone $z 2>&1; done
  echo "== loaded catalog (AXFR from 127.0.0.1, read-only)"; dig +time=2 +tries=1 +norecurse +tcp @127.0.0.1 $CAT AXFR 2>&1 | grep -v '^;' | sed '/^$/d'
  echo "== catalog members (PTR rdata under zones.$CAT)"; dig +time=2 +tries=1 +norecurse +tcp @127.0.0.1 $CAT AXFR 2>/dev/null | awk '$4=="PTR" && $1 ~ /zones\./ {print $1, $5}'
  for z in s1-kill.test s2.s1-kill.test; do echo "== loaded $z SOA (127.0.0.1)"; dig +time=2 +tries=1 +norecurse @127.0.0.1 $z SOA 2>&1 | grep -E 'status:|flags:|IN.SOA'; done
else
  echo "== named inactive: rndc/dig not run"
fi
echo "== /var/named tree (no follow)"
find /var/named -xdev -printf '%M %u:%g %s %TY-%Tm-%TdT%TH:%TM:%TS %p -> %l\n' 2>&1 | sort -k5 | head -80
echo "== transferred zone files (newer than /etc/named.conf): metadata, hash, content"
find /var/named -xdev -type f -newer /etc/named.conf 2>/dev/null | sort | while read -r f; do
  echo "-- $f"; stat -c 'size=%s mode=%a owner=%U:%G mtime=%y' "$f"; sha256sum "$f"
  case "$f" in *catalog*) z=$CAT;; *) z=unknown;; esac
  for fmt in raw text; do
    if named-compilezone -q -f $fmt -F text -o /tmp/cp-b12-zone.txt $z "$f" >/dev/null 2>&1; then echo "(zone $z, format $fmt)"; head -40 /tmp/cp-b12-zone.txt; rm -f /tmp/cp-b12-zone.txt; break; fi
  done
done
echo "== named journal (transfer/notify/catalog/zone lines, all boots)"
journalctl -u named.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'xfr|transfer|notif|catalog|zone|serial' | tail -n 150
