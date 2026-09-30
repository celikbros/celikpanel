# Batch 11, read-only on the native BIND secondary after a resumed recover. Args: SINCE UNTIL ("YYYY-MM-DD HH:MM:SS", UTC)
echo "wall=$(date -u +%FT%T.%NZ) boot_id=$(cat /proc/sys/kernel/random/boot_id)"
echo "== journal this boot: sshd / celikpeer / inspect / sudo / rndc lines"
journalctl -b --no-pager -o short-iso-precise 2>/dev/null | grep -iE 'sshd|celikpeer|inspect|sudo|rndc|control' | grep -v 'celik : ' | cut -c1-700 | tail -n 120
echo "== journal this boot: all lines from $1 to $2 (UTC)"
TZ=UTC journalctl -b --no-pager -o short-iso-precise --since "$1" --until "$2" 2>/dev/null | cut -c1-700
echo "== dns-peer-enroll secondary-status"; /root/dns-owner-tools/dns-peer-enroll secondary-status 2>&1
echo "== inspector files (ls only)"
ls -la --time-style=full-iso /etc/bind-peer-inspector /etc/ssh/bind-peer-inspector /var/lib/bind-peer-inspector /usr/local/libexec/ 2>&1
ls -la --time-style=full-iso /etc/sudoers.d 2>&1
getent passwd celikpeer; id celikpeer 2>&1
echo "== sshd_config Include/Match lines"; grep -nE '^(Include|Match)|AuthorizedKeys|ForceCommand' /etc/ssh/sshd_config /etc/ssh/sshd_config.d/* 2>&1
echo "== rndc key file (ls only, content never read)"; ls -la --time-style=full-iso /etc/rndc.key /etc/rndc.conf 2>&1
