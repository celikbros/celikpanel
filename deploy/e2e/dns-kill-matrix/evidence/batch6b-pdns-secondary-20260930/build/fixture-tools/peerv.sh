E=/var/tmp/cp-b6b-0930/evidence/$1
python3 - $E/paired-secondary-peer/peer-verdict.json <<'PY'
import json,sys
v=json.load(open(sys.argv[1]))
print("peer keys", list(v.keys()))
for k in ("status","combined_exit","failures","unknown","peer_catalog_format","expected_producer","guest_exit"):
    print(k, json.dumps(v.get(k))[:600])
a=v.get("after_recovery") or {}
print("after_recovery producer", a.get("catalog_producer"), a.get("agent_catalog_format_name"), a.get("catalog_serial"), a.get("catalog_members"))
PY
grep -h 'catalog format' $E/secondary-state-post-collect.txt | cut -c1-400 | head -5
grep -o 'serves catalog[^"]*format[^"]*' $E/raw/results/*/transcript.jsonl | sort | uniq -c | cut -c1-300
