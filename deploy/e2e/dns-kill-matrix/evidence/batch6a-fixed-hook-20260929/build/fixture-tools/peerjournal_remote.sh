# Read-only: the native primary's unit journal, all boots. Arg: ENGINE (bind|pdns)
E=$1; U=named.service; [ "$E" = pdns ] && U=pdns.service
journalctl --list-boots --no-pager 2>&1
echo "== journalctl -u $U"
journalctl -u $U --no-pager -o short-iso-precise 2>&1
