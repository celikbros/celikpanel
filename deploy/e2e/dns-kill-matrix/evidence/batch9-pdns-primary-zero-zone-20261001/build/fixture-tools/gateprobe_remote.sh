# Lease-free gate probe on the prepared guest (before run-prepared): the exact SwitchDNSEngineV1 call without a lease, through the
# ordinary Agent serving the socket. Private evidence is hashed before and after (the probe must not change it).
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
CELL=$(sed -n 's/^cell_id=//p' /etc/celikpanel-dns-kill-matrix)
echo "cell=$CELL"
systemctl show celikpanel-agent.service -p ActiveState,MainPID --no-pager
sha256sum /opt/celikpanel/bin/agent /opt/celikpanel/bin/dns-kill-trigger 2>&1
echo "== private evidence before"; (cd /var/lib/celikpanel-agent-private && sha256sum -- * 2>/dev/null)
RID=$(printf 'batch9-gate-probe-%s' "$CELL" | sha256sum | cut -c1-32)
echo "probe request id (unused by the probe RPC, required by the trigger's environment check): $RID"
cd /
/usr/sbin/runuser -u root -g celikpanel -- /usr/bin/env -i PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin LANG=C.UTF-8 \
  CELIKPANEL_AGENT_SOCKET=/run/celikpanel/agent.sock CELIKPANEL_AGENT_TOKEN_FILE=/etc/celikpanel/agent.token \
  CELIKPANEL_S1_CELL_ID="$CELL" CELIKPANEL_S1_DRIVER=pdns-switch CELIKPANEL_S1_REQUEST_ID="$RID" \
  /opt/celikpanel/bin/dns-kill-trigger rpc-gate-probe --scenario /var/lib/celikpanel-dns-kill-matrix/scenario.json --timeout 60s
echo "GATE_PROBE_RC=$?"
echo "== private evidence after"; (cd /var/lib/celikpanel-agent-private && sha256sum -- * 2>/dev/null)
echo "== units after"; systemctl show pdns.service -p LoadState,UnitFileState,ActiveState,MainPID --no-pager; ss -H -lntup 'sport = :53'
