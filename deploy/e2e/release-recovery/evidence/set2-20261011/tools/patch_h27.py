"""set2 H27: `postfix status` refuses to answer while main.cf is refused (it reads the configuration first), so
"Postfix is running" is read from the master's PID file and /proc; `postfix status` is recorded beside it."""
import sys
d = sys.argv[1]

def patch(name, pairs):
    p = d + "/" + name
    s = open(p, encoding="utf-8", newline="").read()
    for old, new in pairs:
        assert s.count(old) == 1, (name, old[:60], s.count(old))
        s = s.replace(old, new)
    open(p, "w", encoding="utf-8", newline="").write(s)

patch("guest_settings_native.py", [(
'''              "master_pid": _pid_file("/var/spool/postfix/pid/master.pid"),
              "status": run(["postfix", "status"]),
''',
'''              "master_pid": _pid_file("/var/spool/postfix/pid/master.pid"),
              "master_alive": _alive(_pid_file("/var/spool/postfix/pid/master.pid")),
              "status": run(["postfix", "status"]),
''')])
patch("settings_writes_trial.py", [(
'''                           p10["_values"]["smtpd_client_message_rate_limit"] == "46" and p10.get("master_pid") == p8.get("master_pid")
                           and p10.get("master_pid") is not None and p10["status"].get("returncode") == 0,
''',
'''                           p10["_values"]["smtpd_client_message_rate_limit"] == "46" and p10.get("master_pid") == p8.get("master_pid")
                           and p10.get("master_pid") is not None and p10.get("master_alive") is True,
'''), (
'''                           {"rate": p10["_values"]["smtpd_client_message_rate_limit"], "master_pid": [p8.get("master_pid"), p10.get("master_pid")],
                            "postfix_status": p10["status"], "units": p10.get("units")})
''',
'''                           {"rate": p10["_values"]["smtpd_client_message_rate_limit"], "master_pid": [p8.get("master_pid"), p10.get("master_pid")],
                            "master_alive": p10.get("master_alive"), "postfix_status": p10["status"], "units": p10.get("units")})
                if p10["status"].get("returncode") not in (0, None):
                    # H27 (set2 run-a): with a refused main.cf `postfix status` reads the configuration first and refuses
                    # to answer, so it cannot say whether the master runs; the PID file and /proc do.
                    self.note("while main.cf is refused, `postfix status` itself exits non-zero with Postfix's fatal line "
                              "and does not say whether the master runs", p10["status"])
'''), (
'''            running = state["postfix"]["status"].get("returncode") == 0
            pid = state["postfix"]["master_pid"] if state["postfix"]["master_alive"] else 0
            asked = "`postfix status` and the master's PID file"
''',
'''            # H27: the master's PID file and /proc; `postfix status` refuses to answer while main.cf is refused.
            running = bool(state["postfix"]["master_alive"])
            pid = state["postfix"]["master_pid"] if state["postfix"]["master_alive"] else 0
            asked = "the master's PID file and /proc (`postfix status` exit %s)" % state["postfix"]["status"].get("returncode")
''')])
print("H27 patched")
