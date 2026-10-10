"""set2 H35 (found in the debugging session): the database-server route answers with the new user's password also when
the owner sent it, and marks that answer as not kept; only a request for an existing user has a kept answer. The
PostgreSQL arrivals use an existing database user (kept answer); MariaDB keeps the minted password (status-only);
one more request with a sent password records that its replay is status-only too."""
import sys
p = sys.argv[1] + "/request_identity_trial.py"
s = open(p, encoding="utf-8", newline="").read()
a = s.index("            minted = engine == \"mariadb\"     # no password sent: the Panel mints one and shows it once")
b = s.index("    # -- C2: the Panel's own account")
new = '''            minted = engine == "mariadb"     # no password sent: the Panel mints one and shows it once
            existing = None
            if not minted:
                # H35: only a request for an existing database user has an answer without a password (a kept answer).
                users = [u for u in self.panel_state(route + "-users")["database_users"] if u.get("server_id") == server_id]
                existing = users[0]["id"] if users else None
            bodies = {}
            for key, name in (("i", "srvseq"), ("ii", "srvcon"), ("other", "srvoth")):
                bodies[key] = {"database_name": name, "user_id": existing} if existing else \
                    {"database_name": name, "new_username": name + "u"}
            self.current.setdefault("who_chose_the_database_users_pw", {})[engine] = \
                "an existing database user is given access: the answer carries no password and is kept" if existing \
                else "a new user without a password: the Panel mints one and shows it once (the answer is not kept)"
            self.suite(route, path, bodies, self.database_effect(engine, lambda r, s=server_id: r.get("server_id") == s),
                       self.one_database, retained=bool(existing), timeout=900)
            if engine == "postgresql":
                # A new user with a password the owner sent: recorded, with its replay.
                sent = {"database_name": "srvsent", "new_username": "srvsentu", "new_password": self.password()}
                identity = self._own_identity(route + " (a password the owner sent)")
                first = self.post(f"{route}: a new user with a password the owner sent", path, sent, identity, 900)
                again = self.post(f"{route}: the same request again", path, sent, identity, 900)
                carried = isinstance(first.get("_parsed"), dict) and "password" in first["_parsed"]
                self.current["sent_password"] = {"first": brief(first), "answer_carries_the_password_field": carried,
                                                 "replay": brief(again)}
                self.check("a database created with a password the owner sent: the answer carries that password, so it is "
                           "not kept and the replay is the status-only refusal",
                           first.get("status") == 200 and carried and again.get("status") == 409
                           and answer_code(again) == "REQUEST_COMPLETED_RESULT_NOT_RETAINED", self.current["sent_password"])

'''
s = s[:a] + new + s[b:]
open(p, "w", encoding="utf-8", newline="").write(s)
print("H35 patched")
