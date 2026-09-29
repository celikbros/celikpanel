set -e
D=$(mktemp -d)
mkdir -p $D/results $D/fixture $D/state $D/watch $D/journald
cp -a /var/lib/celikpanel-dns-kill-matrix/results/. $D/results/
find /var/lib/celikpanel-dns-kill-matrix -maxdepth 1 -type f ! -name manifest.json -exec cp -a {} $D/fixture/ \;
[ -d /var/lib/celikpanel-dns-kill-matrix/measured ] && cp -a /var/lib/celikpanel-dns-kill-matrix/measured $D/fixture/
ls -laR --time-style=full-iso /var/lib/celikpanel-dns-kill-matrix > $D/fixture/ls.txt
for f in service-mutations.json dns-engine-state.json dns-engine-switch-journal.json; do [ -e /var/lib/celikpanel-agent-private/$f ] && cp -a /var/lib/celikpanel-agent-private/$f $D/state/; done
cp -a /var/lib/celikpanel-agent-private/*ownership* $D/state/ 2>/dev/null || true
cp -a /var/lib/celikpanel-agent-private/*archive* $D/state/ 2>/dev/null || true
ls -laR --time-style=full-iso /var/lib/celikpanel-agent-private > $D/state/ls.txt
for w in /var/tmp/cp-b4-watch*; do
  if [ -d "$w" ]; then mkdir -p $D/watch/$(basename $w); cp -a $w/. $D/watch/$(basename $w)/; fi
done
journalctl --list-boots --no-pager > $D/journald/list-boots.txt 2>&1 || true
for u in pdns.service named.service bind9.service celikpanel-agent.service celikpanel-panel.service; do
  journalctl -u $u --no-pager -o short-iso-precise > $D/journald/$u.txt 2>&1 || true
done
tar -C $D -cf - .
