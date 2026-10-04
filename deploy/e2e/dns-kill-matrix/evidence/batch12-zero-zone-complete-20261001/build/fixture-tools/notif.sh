for s in z04-zero-committed-zl z05-zero-started-zl-rb; do
N=/var/tmp/cp-b12-1001/evidence/$s/pdns-notify-journal-and-also-notify.txt
echo "######## $s"; grep -E 'pdns_server|also-notify' $N | grep -v '^#\|pdns.conf:[0-9]*:#' | cut -c1-250
done
