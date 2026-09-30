echo "== all journal lines 17:54:10-17:54:35 except panel TLS handshake noise and GET /api"
journalctl --no-pager -o short-iso-precise --since "2026-09-30 17:54:10" --until "2026-09-30 17:54:35" | grep -v -E 'TLS handshake error|GET /api' | cut -c1-400 | head -n 200
echo "== panel journal 17:50-17:56 excluding TLS noise"
journalctl -u celikpanel-panel.service --no-pager -o short-iso-precise --since "2026-09-30 17:50:00" --until "2026-09-30 17:56:00" | grep -v -E 'TLS handshake error' | cut -c1-300 | tail -n 80
echo "== agent journal 17:50-17:56"
journalctl -u celikpanel-agent.service --no-pager -o short-iso-precise --since "2026-09-30 17:50:00" --until "2026-09-30 17:56:00" | cut -c1-300 | tail -n 60
echo "== self-update state (request file, secrets filtered)"
grep -v -i -E 'token|secret|password|key' /var/lib/celikpanel-release-state/self-update/df01bec5190476fd66baf46177314f54.json | head -c 3000; echo
echo "== observation status record"; cat /var/lib/celikpanel-recovery-observations/df01bec5190476fd66baf46177314f54.status
