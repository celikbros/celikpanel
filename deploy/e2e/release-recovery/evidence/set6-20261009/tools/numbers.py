# set6 read-only: the values the README quotes, printed from facts.json. usage: numbers.py EVIDENCE_DIR
import json
import sys

f = json.load(open(sys.argv[1].rstrip("/") + "/facts.json", encoding="utf-8"))
print("== runs")
for key, r in f["runs"].items():
    print(key, r["lab"], r["copy"], r["wrapper"], r["overall"], r["outcome"], r["final"], r["not_passed"], r["installed"])
print("== pinning")
for key, p in f["pinning"].items():
    a, b = p["first"], p["last"]
    print(key, p["first_verdict"], a["at"], a["uptime_seconds"], a["product_paths_present"], a["nsswitch_hosts"], a["systemd_resolved"],
          a["resolved_total_transactions"]["before_the_default_path_lookups"], a["resolved_total_transactions"]["after_them"],
          "| end:", p["last_verdict"], b["at"], (b.get("resolved_total_transactions") or {}).get("before_the_default_path_lookups"),
          (b.get("resolved_total_transactions") or {}).get("after_them"), "boot changed", b["boot_id"] != b["boot_id_at_the_pinning"],
          [(d["path"].split("/")[-1], d["entries"]) for d in b.get("certbot_directories") or []], b.get("certbot_program"))
    print("   ", {n: (v.get("hosts"), sorted(set(l.split()[0] for l in (v.get("ahosts") or "").splitlines() if l.split()))) for n, v in a["default_path_answers"].items()})
print("== part b")
for label, r in f["part_b"].items():
    print(label, r["run"], r["panel"], r["at"], r["verdict"], r["panel_ports"], r["listening_80_443_panel"], r["tls_leaf_sha256"])
    print("   https:", {n: (v["status"], v["Strict-Transport-Security"]) for n, v in r["https"].items()})
    print("   plain:", {n: (v["status_line"], v["Strict-Transport-Security"], v["server"], v["location"], v["body_start"][:60]) for n, v in r["plain"].items()})
print("== update")
for key, u in f["update"].items():
    print(key, json.dumps({k: u[k] for k in ("ledger", "guard", "versioned", "versioned_not_measured")}, sort_keys=True)[:1500])
    print("   builds", json.dumps(u["builds"], sort_keys=True)[:600])
    print("   steps", u["steps"])
print("== exchanges")
for key, parts in f["exchanges"].items():
    for part, e in parts.items():
        print(key, "|", part, "|", e["exchanges"], e["values"], e["status"])
print("== part a")
for item, per in f["part_a"].items():
    if item == "readings":
        continue
    print(item, {p: v["verdict"] for p, v in per.items()})
for platform, r in f["part_a"].get("readings", {}).items():
    print("readings", platform, r["run"])
    for k in ("m10",):
        if k in r:
            print("   m10", json.dumps(r[k], sort_keys=True)[:1800])
    if "m2" in r:
        print("   m2", json.dumps({k: r["m2"][k] for k in ("status", "import_status", "imported", "not_imported", "left_out", "steps")}, sort_keys=True))
    if "m5" in r:
        print("   m5", json.dumps({k: r["m5"][k] for k in ("status", "import_status", "code", "domain_status", "imported", "not_imported", "left_out")}, sort_keys=True))
    if "m1" in r:
        print("   m1", r["m1"]["runs_as"], r["m1"]["php_version_of_the_page"], r["m1"]["recorded_php_version"], r["m1"]["recorded_php_fpm_socket"], json.dumps(r["m1"]["socket"]),
              json.dumps({k: r["m1"]["delete"][k] for k in ("site_home", "site_home_existed_with_the_site", "site_home_left", "socket_left", "sockets_after", "acme_challenge_root_left")}), r["m1"]["request_statuses"])
    for k, v in (r.get("m6") or {}).items():
        print("   m6", k, v["status"], v["code"], v["reason"], v["details"], v["home_named_by_useradd_in_the_journal"], v["that_home_is_listed_after"], v["claims"], v["not_claimed_by_the_answer"])
    for k, v in (r.get("m4") or {}).items():
        print("   m4", k, v["status"], v["code"], v["reason"], (v["vars"] or {}).get("detail"), v["unit_properties_unchanged"], v["journal_lines_of_the_units"])
