#!/bin/bash
# set6 read-only: the entries of /var/tmp before the run (the `ls -la /var/tmp` of hostcheck-before.txt) against now.
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
before=$(mktemp); after=$(mktemp)
sed -n '/^--- \/var\/tmp entries/,/^--- builds/p' $J/hostcheck-before.txt | tr -d '\r' | awk 'NF >= 9 && $1 ~ /^[-dlcbps]/ {print $9}' | grep -v -x -F -e . -e .. | sort > $before
ls -A /var/tmp | sort > $after
echo "read at $(date -u +%FT%TZ): /var/tmp before the run ($(wc -l < $before) entries, from host/hostcheck-before.txt) against now ($(wc -l < $after) entries)"
echo "--- entries that were there before the run and are not there now: $(comm -23 $before $after | wc -l)"
comm -23 $before $after
echo "--- entries that are there now and were not there before the run: $(comm -13 $before $after | wc -l)"
comm -13 $before $after
rm -f $before $after
echo "--- the WSL host's own cleaner of temporary directories (tmpfiles.d: q /var/tmp 1777 root root 30d), its runs since the host's boot"
grep -h '/var/tmp' /usr/lib/tmpfiles.d/tmp.conf 2>/dev/null
journalctl -u systemd-tmpfiles-clean.service --no-pager --since "$(uptime -s)" 2>/dev/null | grep -E 'Starting|Finished' | sed -E 's/^([A-Z][a-z]{2} [0-9]+ [0-9:]+) [^ ]+ /    \1 /'
echo "    (journal times are the host's local time, UTC+3; boot at $(uptime -s) local)"
echo "    (the two systemd-private-* directories are systemd's own and are remade with another suffix by themselves; not of this run)"
