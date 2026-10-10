#!/bin/bash
# set6: a positive control of the secret scan's base64 class and of the base64 pass of set6_redact. The evidence of
# this run holds no base64 text at all, so class 10 found nothing to search; this shows on a made-up record that it
# does find what it is for. The control folder is made here, scanned, swept, scanned again, and removed; the digest in
# it is the SHA-256 of the text "set6 scan control, not a credential" and is printed nowhere.
# usage: scancontrol.sh COPY   -> prints the report (finalize.sh keeps it as host/secret-scan-control.txt)
J=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)
R=/var/tmp/cp-set6-run; copy=${1:-c}
H=$R/harness-$copy/deploy/e2e/release-recovery
D=$R/scan-control
rm -rf -- "${D:?}"; mkdir -p $D/evidence/host
python3 -I - $D/evidence/host/recovery-fault-collection-control.json <<'PY'
import base64, hashlib, json, sys
digest = hashlib.sha256(b"set6 scan control, not a credential").hexdigest()
events = "\n".join(json.dumps({"event": name, "transaction_token_sha256": digest}) for name in ("armed", "released"))
json.dump({"schema": "control", "events_base64": base64.b64encode(events.encode()).decode(), "events_sha256": hashlib.sha256(events.encode()).hexdigest()},
          open(sys.argv[1], "w"), indent=2)
PY
echo "positive control, $(date -u +%FT%TZ): one made-up host-side record whose events_base64 holds a digest under transaction_token_sha256 in two lines"
echo "--- 1. the scan over the control folder as made (expected: hits inside base64 text, exit 1)"
SET6_HARNESS=$H python3 -I -B $J/secretscan.py $D/evidence > $D/scan-1.txt 2> $D/scan-1.err; echo "exit $?"
grep -E "^base64 text|INSIDE-BASE64|^token digests by shape|^RESULT" $D/scan-1.txt
echo "--- 2. set6_redact.py count (expected: the file would change inside base64 text, exit 1)"
python3 -I -B $H/set6_redact.py count $D/evidence > $D/count-1.json; echo "exit $?"
python3 -I $J/stagecount.py $D/count-1.json
echo "--- 3. set6_redact.py sweep, then the record"
python3 -I -B $H/set6_redact.py sweep $D/evidence > $D/sweep.json; echo "exit $?"
python3 -I - $D/sweep.json $D/evidence/host/recovery-fault-collection-control.json <<'PY'
import base64, json, sys
report, record = json.load(open(sys.argv[1])), json.load(open(sys.argv[2]))
print("sweep report:", {k: report[k] for k in ("base64_runs_replaced", "places_inside_base64_text", "json_objects_marked", "files_changed_inside_base64_text")})
inner = base64.b64decode(record["events_base64"]).decode()
print("decoded events after the sweep:", [json.loads(line) for line in inner.splitlines()])
print("the note added to the record:", json.dumps(record.get("set6_base64_redaction"), sort_keys=True))
print("events_sha256 was left as it was (it is the digest of the text before the replacement): present =", "events_sha256" in record)
PY
echo "--- 4. the scan over the control folder after the sweep (expected: no hit, exit 0)"
SET6_HARNESS=$H python3 -I -B $J/secretscan.py $D/evidence > $D/scan-2.txt 2> $D/scan-2.err; echo "exit $?"
grep -E "^base64 text|INSIDE-BASE64|^token digests by shape|^\`\[REDACTED-SHA256\]|^RESULT" $D/scan-2.txt
rm -rf -- "${D:?}"
echo "the control folder was removed"
