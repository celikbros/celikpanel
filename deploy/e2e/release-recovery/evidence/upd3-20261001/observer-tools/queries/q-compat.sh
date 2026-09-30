echo "== utc"; date -u +%FT%TZ
echo "== agent private state dir"; ls -la /var/lib/celikpanel-agent-private 2>&1
f=/var/lib/celikpanel-agent-private/hosting-root-v1.json; [ -f "$f" ] && { echo "-- $f"; cat "$f"; }
echo "== hosting parents"; stat -c '%A %a %U:%G %n' /var/www /var/www/celikpanel 2>&1
RT=/usr/libexec/celikpanel/recovery-runtimes/v1/17c239bd1c65b8134f1190863782072b55d46ee2f7c62c7bc32b6cdb67dee05a
ENVV="PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root USER=root LOGNAME=root SHELL=/bin/bash LANG=C LC_ALL=C CELIKPANEL_DATA_DIR=/var/lib/celikpanel CELIKPANEL_AGENT_STATE_DIR=/var/lib/celikpanel-agent-private CELIKPANEL_MUTATION_LOCK=/run/celikpanel/service-mutation.lock"
echo "== read-only check: candidate runtime agent-checker --check-service-mutation-idle"
cd / && env -i $ENVV $RT/bin/agent-checker --check-service-mutation-idle; echo "rc=$?"
echo "== read-only check: installed agent --check-service-mutation-idle"
cd / && env -i $ENVV /opt/celikpanel/bin/agent --check-service-mutation-idle; echo "rc=$?"
echo "== read-only check: candidate runtime panel-checker --check-service-operations-idle-wal-aware"
cd / && env -i $ENVV $RT/bin/panel-checker --check-service-operations-idle-wal-aware; echo "rc=$?"
