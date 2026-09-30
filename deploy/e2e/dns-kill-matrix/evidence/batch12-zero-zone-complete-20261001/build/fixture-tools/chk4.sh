for s in z04-zero-committed-zl z05-zero-started-zl-rb; do E=/var/tmp/cp-b12-1001/evidence/$s
echo "== $s"; grep -rl 'post-publication\|pre-publication' $E | head; grep -rhoE '"(serial_rule|rule)": "(pre|post)-publication"' $E | sort | uniq -c
grep -rhoE '"zero_zone_restamp": \{[^}]*\}' $E | sort -u | head -3
done
