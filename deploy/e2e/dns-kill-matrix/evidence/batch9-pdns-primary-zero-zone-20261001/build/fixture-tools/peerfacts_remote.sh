# Read-only capture on the native primary peer (Arch). Arg: ENGINE (bind|pdns)
E=$1
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
head -4 /etc/os-release; uname -r
echo "== packages"; if command -v pacman >/dev/null 2>&1; then pacman -Q bind powerdns python 2>&1; else LC_ALL=C dpkg-query -W -f='${Package} ${Version} ${db:Status-Abbrev}
' 'bind9*' 'pdns*' python3 2>&1; fi
for u in named.service pdns.service; do
  echo "== $u"; systemctl show "$u" -p LoadState,UnitFileState,ActiveState,SubState,MainPID,ExecMainStartTimestamp,NRestarts,User,FragmentPath 2>&1
done
echo "== ss 53"; ss -H -lntup 'sport = :53'
echo "== management binaries"; ls -la /opt/celikpanel/bin 2>&1
echo "== config and zone file hashes"
sha256sum /etc/named.conf /var/named/celikpanel-fixture-catalog.zone /var/named/celikpanel-fixture-member.zone /etc/powerdns/pdns.conf /var/lib/powerdns/pdns.sqlite3 2>&1
if [ "$E" = pdns ] && [ -e /var/lib/powerdns/pdns.sqlite3 ]; then
python3 - <<'PY'
import sqlite3, json
c = sqlite3.connect("file:/var/lib/powerdns/pdns.sqlite3?mode=ro", uri=True, timeout=5)
cols = [r[1] for r in c.execute("PRAGMA table_info(domains)")]
for row in c.execute("SELECT * FROM domains ORDER BY id"):
    print("domain:", json.dumps(dict(zip(cols, row))))
c.close()
PY
fi
echo "== peer DNS sampler state"; systemctl show cp-b9-peerloop1.service -p ActiveState,SubState,MainPID 2>&1; wc -l /var/tmp/cp-b9-peerloop/samples.log 2>&1
