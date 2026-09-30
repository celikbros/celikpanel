echo "== utc"; date -u +%FT%TZ
echo "== self-update units"; systemctl list-units --all --no-legend 'celikpanel-self-update*' | head
for u in $(systemctl list-units --all --no-legend --plain 'celikpanel-self-update*' | awk '{print $1}'); do echo "---- journal $u"; journalctl -u "$u" --no-pager -o short-iso-precise | tail -n 120; done
echo "== panel journal around the start"; journalctl -u celikpanel-panel.service --no-pager -o short-iso-precise --since "2026-09-30 17:53:30" --until "2026-09-30 17:55:00" | grep -v 'GET /api' | tail -n 60
echo "== recovery foundation files"; ls -la /usr/libexec/celikpanel 2>/dev/null; ls -la /usr/libexec/celikpanel/recovery-foundation* 2>/dev/null | head -30
echo "== release-recovery journal"; journalctl -u celikpanel-release-recovery.service --no-pager -o short-iso-precise | tail -n 30
