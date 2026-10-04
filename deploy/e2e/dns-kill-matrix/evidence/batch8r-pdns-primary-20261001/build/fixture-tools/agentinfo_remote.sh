# Read-only: which Agent runs and where its socket is, on a prepared guest
systemctl show celikpanel-agent.service -p LoadState,UnitFileState,ActiveState,SubState,MainPID,FragmentPath --no-pager
systemctl cat celikpanel-agent.service 2>&1 | grep -iE 'Exec|Environment|Runtime|Socket' | head -20
ls -la /run/celikpanel 2>&1
ss -xlp 2>/dev/null | grep -i celik | head
pgrep -a agent
ls -la /var/lib/celikpanel-dns-kill-matrix/
cat /var/lib/celikpanel-dns-kill-matrix/controller.env 2>/dev/null | grep -iv token | head -30
grep -rn 'agent.sock\|AGENT_SOCKET' /var/lib/celikpanel-dns-kill-matrix/*.json 2>/dev/null | head -5
