#!/bin/bash
# set6 read-only: the header readings of one lab in short. usage: showhdr.sh LABNAME
for f in /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*/headers-*.json; do
  [ -f "$f" ] || { echo "no header reading yet"; exit 0; }
  echo "== $f"
  python3 -I - "$f" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
print("expected", d.get("expected_strict_transport_security"), "| ports", d.get("panel_ports"), "| unit", d.get("panel_unit"), "| owner login held", d.get("owner_login_held"))
print("listening", d.get("listening_tcp_of_the_panel_process"), d.get("listening_tcp_on_80_443_and_the_panel_port"))
for r in d["requests"]:
    print(" ", r["name"], "|", r.get("status"), r.get("status_line"), "|", r.get("strict_transport_security"), "|", r.get("error") or "", "|", (r.get("body_start") or "")[:90].replace("\n", " "), "|", r.get("panel") or "")
PY
done
for f in /var/tmp/cp-release-drill-$1/evidence/*/upd1/*/steps/*b-headers*/section.json; do
  [ -f "$f" ] || continue
  python3 -I -c "import json,sys;d=json.load(open(sys.argv[1]));print(d['verdict'], d.get('error'));[print(' ', c['ok'], c['name'][:150]) for c in d['checks']]" "$f"
done
