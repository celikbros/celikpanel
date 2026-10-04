# Batch 10, read-only on the native BIND secondary: RNDC prerequisite of bind-peer-inspect (key file, control listener, named's own start-up lines). No rndc command is run.
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
echo "== key/conf files"; ls -la --time-style=full-iso /etc/rndc.key /etc/rndc.conf /etc/named.conf 2>&1
echo "== pacman ownership"; pacman -Qo /etc/rndc.key 2>&1; pacman -Q bind 2>&1
echo "== TCP listeners :953"; ss -H -ltnp 'sport = :953' 2>&1; echo "(end)"
echo "== named-checkconf -p: controls/key statements"; named-checkconf -p 2>&1 | grep -n -iE 'controls|inet .* allow|keys|^key ' | head -20; echo "(end)"
echo "== named journal (all boots): command channel / rndc / control lines"
journalctl -u named.service --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'command channel|rndc|control|key file' | cut -c1-400
echo "== named journal this boot: first 25 lines"
journalctl -b -u named.service --no-pager -o short-iso-precise 2>/dev/null | head -n 25 | cut -c1-300
