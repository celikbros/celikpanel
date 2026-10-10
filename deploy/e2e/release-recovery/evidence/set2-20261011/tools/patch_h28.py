"""set2 H28, H29 (rid-ubuntu run-a): the Databases page's `type_name` is the display name ("MariaDB", "PostgreSQL");
and a domain's MariaDB and PostgreSQL databases carry the same name, so the Panel's rows are compared per server."""
import sys
p = sys.argv[1] + "/request_identity_trial.py"
s = open(p, encoding="utf-8", newline="").read()
pairs = [(
'''            by_type.setdefault(str(server.get("type_name")), server.get("id"))
''',
'''            by_type.setdefault(str(server.get("type_name")).lower(), server.get("id"))   # H28: "MariaDB", "PostgreSQL"
'''), (
'''            rows = [r["name"] for r in self.panel_state(label + "-rows")["databases_v2"] if where(r)]
''',
'''            # H29: one row is a database on one server (a domain's MariaDB and PostgreSQL databases share a name)
            rows = [f"{r['server_id']}:{r['name']}" for r in self.panel_state(label + "-rows")["databases_v2"] if where(r)]
'''), (
'''        new_rows = one_new(before["fp"]["panel_rows"], after["fp"]["panel_rows"])
        named = (answer or {}).get("_parsed", {}).get("name") if answer and isinstance(answer.get("_parsed"), dict) else None
''',
'''        new_rows = [row.split(":", 1)[1] for row in one_new(before["fp"]["panel_rows"], after["fp"]["panel_rows"])]
        named = (answer or {}).get("_parsed", {}).get("name") if answer and isinstance(answer.get("_parsed"), dict) else None
'''), (
'''            done = self.suite(route, path, bodies, self.database_effect(engine, lambda r: r.get("domain_id") == domain_id),
                              self.one_database, retained=True, timeout=900)
''',
'''            server_id = (self.state.get("database_servers") or {}).get(engine)
            done = self.suite(route, path, bodies,
                              self.database_effect(engine, lambda r, s=server_id: r.get("domain_id") == domain_id
                                                   and (s is None or r.get("server_id") == s)),
                              self.one_database, retained=True, timeout=900)
''')]
for old, new in pairs:
    assert s.count(old) == 1, (old[:60], s.count(old))
    s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="").write(s)
print("H28/H29 patched")
