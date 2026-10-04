echo "== utc"; date -u +%FT%TZ
echo "== /opt/celikpanel metadata (ProbeCurrentResources preconditions)"
stat -c '%A %a %U:%G %n' /opt /opt/celikpanel
find /opt/celikpanel/bin /opt/celikpanel/web \( ! -user root -o ! -group root -o -perm /7022 -o \( -type f -links +1 \) -o \( ! -type f ! -type d \) \) -printf '%M %u:%g %n %p\n' 2>&1 | head -20
echo "(end of non-conforming list)"
getfattr -R -d -m - /opt/celikpanel/bin /opt/celikpanel/web 2>/dev/null | grep -v -E '^(# file|user\.|$)' | head -5
echo "(end of non-user xattrs)"
RT=/usr/libexec/celikpanel/recovery-runtimes/v1/17c239bd1c65b8134f1190863782072b55d46ee2f7c62c7bc32b6cdb67dee05a
ENVV="PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root USER=root LOGNAME=root SHELL=/bin/bash LANG=C LC_ALL=C CELIKPANEL_DATA_DIR=/var/lib/celikpanel CELIKPANEL_AGENT_STATE_DIR=/var/lib/celikpanel-agent-private CELIKPANEL_MUTATION_LOCK=/run/celikpanel/service-mutation.lock"
for c in "panel-checker --check-service-operations-idle-wal-aware" "agent-checker --check-service-mutation-idle"; do
  ok=0; bad=0
  for i in $(seq 1 60); do
    out=$(cd / && env -i $ENVV $RT/bin/$c 2>&1); rc=$?
    if [ $rc -eq 0 ]; then ok=$((ok+1)); else bad=$((bad+1)); echo "  $(date -u +%T.%N | cut -c1-12) rc=$rc $(echo "$out" | tail -n 2 | tr '\n' ' ' | cut -c1-300)"; fi
    sleep 0.5
  done
  echo "== read-only loop: $c ok=$ok fail=$bad"
done
date -u +%FT%TZ
