for s in z04-zero-committed-zl z05-zero-started-zl-rb; do E=/var/tmp/cp-b12-1001/evidence/$s
echo "== $s"; python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(sorted(d)); [print(k, json.dumps(d[k])[:700]) for k in d if k in ("verdict","status","complete_verdict","catalog_restamp","pair","reboot_after_recovery","after_reboot","serial_rule","guest_verdict","pass_definition")]' $E/native-and-outage.json
grep -o '"serial_rule": "[^"]*"' $E/raw/results/*/result.json 2>/dev/null | sort | uniq -c
grep -o '"serial_rule": "[^"]*"' -r $E/fresh-primary-peer 2>/dev/null | sort | uniq -c
done
