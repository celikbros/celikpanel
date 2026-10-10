import sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
old = '"length(ds.admin_password_encrypted) AS sealed_bytes FROM database_servers ds "'
assert s.count(old) == 1
s = s.replace(old, '"length(ds.root_password_encrypted) AS sealed_bytes FROM database_servers ds "')
old = '''        state["audit_max_id"] = (query(connection, "SELECT coalesce(max(id), 0) AS id FROM audit_logs") or [{}])[0].get("id")
'''
new = '''        state["audit_max_id"] = (query(connection, "SELECT coalesce(max(id), 0) AS id FROM audit_logs") or [{}])[0].get("id")
        counts = query(connection, "SELECT action, COUNT(*) AS n FROM audit_logs GROUP BY action ORDER BY action")
        state["audit_counts"] = {row["action"][:120]: row["n"] for row in counts if "action" in row}
'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("ok")
