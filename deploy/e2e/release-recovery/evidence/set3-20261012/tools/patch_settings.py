# set3: settings-writes cell after the second round of corrections (P3, P3b, O11) and the collection-time shapes.
ROOT = r'C:\CELIKBROS PROJECTS\celikpanel\deploy\e2e\release-recovery' + '\\'


class Patch:
    def __init__(self, name):
        self.path = ROOT + name
        self.s = open(self.path, encoding='utf-8', newline='').read()
        assert '\r\n' not in self.s, name

    def rep(self, a, b, n=1):
        assert self.s.count(a) == n, (a[:90], self.s.count(a))
        self.s = self.s.replace(a, b)

    def save(self):
        open(self.path, 'w', encoding='utf-8', newline='').write(self.s)


# ---- owner_update_trial.py: shape rules applied by every driver at collection time -----------------------------
p = Patch('owner_update_trial.py')
p.rep('''def provenance_for(variant: str) -> dict:''', '''# set3: collection-time shape rules beside the pair redactor's own (structural keys, registered values, PEM blocks).
# No retained file may hold a hash-shaped credential value (crypt, Dovecot scheme, SCRAM verifier, MariaDB native
# hash) or a WireGuard private or preshared key; set2 had to be redacted after collection for the first.
HASH_SHAPED = re.compile(
    r"(?:\\{[A-Z][A-Z0-9.-]{1,24}\\})?\\$(?:1|2[abxy]?|5|6|7|y|gy|sha1|argon2(?:id|i|d)|scrypt|pbkdf2(?:-sha(?:1|256|512))?)"
    r"\\$[./A-Za-z0-9$=,+-]{8,}"
    r"|\\{(?:SSHA(?:256|512)?|SHA(?:256|512)?|SMD5|PLAIN|CRYPT|CRAM-MD5|[A-Z0-9]+-CRYPT|ARGON2ID?|PBKDF2)\\}[^\\s\\"'<>\\\\]{4,}"
    r"|SCRAM-SHA-256\\$\\d+:[A-Za-z0-9+/=]+\\$[A-Za-z0-9+/=]+:[A-Za-z0-9+/=]+"
    r"|(?<![0-9A-Za-z])\\*[0-9A-F]{40}(?![0-9A-Fa-f])")
HASH_SHAPED_MARK = "[REDACTED hash-shaped value]"
WIREGUARD_SECRET = re.compile(r"(?i)\\b(Private_?Key|Preshared_?Key)(\\\\?\\"?\\s*[:=]\\s*\\\\?\\"?)([A-Za-z0-9+/]{43}=)")


def hash_shaped_count(text: str | bytes | None) -> int:
    """How many hash-shaped credential values a text holds (the count only; the driver asserts on it)."""
    if text is None:
        return 0
    if isinstance(text, bytes):
        text = text.decode("utf-8", "replace")
    return len(HASH_SHAPED.findall(text))


def shape_text(text: str) -> str:
    text = HASH_SHAPED.sub(HASH_SHAPED_MARK, text)
    return WIREGUARD_SECRET.sub(lambda match: match.group(1) + match.group(2) + "[REDACTED]", text)


def shape_redactor(redactor: Any) -> Any:
    """The pair redactor with the set3 shape rules added to every text it handles (JSON strings, journals, logs)."""
    plain = redactor.text
    redactor.text = lambda value: shape_text(plain(value))
    return redactor


def provenance_for(variant: str) -> dict:''')
p.rep('''        self.redactor = self.p["redaction"].Redactor()''', '''        self.redactor = shape_redactor(self.p["redaction"].Redactor())''')
p.save()

# ---- guest_settings_native.py: a reload hook that fails before it signals the server ------------------------------
g = Patch('guest_settings_native.py')
g.rep('''        HOOK_SCRIPT.write_text("#!/bin/sh\\n# owner hook: tell PostgreSQL to re-read its files, then reload the pooler\\n"
                               "kill -HUP \\"$MAINPID\\" || exit 1\\nexec /usr/bin/systemctl reload " + HOOK_POOLER + "\\n")''',
      '''        if args.get("variant") == "fail-before-signal":
            # set3: the owner's hook reloads the pooler FIRST and signals the server only when that worked, so a
            # failing pooler reload leaves the server unsignalled (it does not re-read its files).
            HOOK_SCRIPT.write_text("#!/bin/sh\\n# owner hook: reload the pooler first, then tell PostgreSQL to re-read its files\\n"
                                   "/usr/bin/systemctl reload " + HOOK_POOLER + " || exit 1\\nexec kill -HUP \\"$MAINPID\\"\\n")
        else:
            HOOK_SCRIPT.write_text("#!/bin/sh\\n# owner hook: tell PostgreSQL to re-read its files, then reload the pooler\\n"
                                   "kill -HUP \\"$MAINPID\\" || exit 1\\nexec /usr/bin/systemctl reload " + HOOK_POOLER + "\\n")''')
g.rep('''        return {"action": "owner-reload-hook", "applied": {"unit": unit, "dropin": str(dropin), "script": str(HOOK_SCRIPT)},''',
      '''        return {"action": "owner-reload-hook", "variant": args.get("variant") or "signal-then-fail",
                "script_text": HOOK_SCRIPT.read_text(),
                "applied": {"unit": unit, "dropin": str(dropin), "script": str(HOOK_SCRIPT)},''')
g.save()

# ---- settings_writes_trial.py ----------------------------------------------------------------------------------
s = Patch('settings_writes_trial.py')
s.rep('''    "set2-arch": SettingsCell("set2-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
}''', '''    "set2-arch": SettingsCell("set2-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
    # set3: the same guests and profiles, measured after the corrections of 2026-10-11 (P3, P3b, O11 in S8).
    "set3-debian13": SettingsCell("set3-debian13", "debian13", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set3-ubuntu": SettingsCell("set3-ubuntu", "ubuntu", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set3-arch": SettingsCell("set3-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
}
# set3: what the corrections of 2026-10-11 answer (cmd/panel/service_action_outcome.go, internal/transport/rpc.go).
NOT_RUNNING = (409, "SERVICE_ACTION_FAILED", "not_running")
RELOAD_REREAD = (502, "SERVICE_ACTION_FAILED", "reload_reread")
RELOAD_NOT_REREAD = (502, "SERVICE_ACTION_FAILED", "reload_not_reread")''')
s.rep('''set2 (2026-10-11): the expectations of S1 f''', '''set3 (2026-10-12): S8 follows the second round of corrections: a Reload of a stopped Postfix or Dovecot answers
409 ``not_running``; the PostgreSQL reload hook answers ``reload_reread`` (and a hook that fails before it signals the
server ``reload_not_reread``); Stop of Postfix with a refused main.cf answers success when the master is gone; no
service action is refused ``server_setup_busy`` while the setup waits at ``access_dns``. The cells ``set3-*`` name it.

set2 (2026-10-11): the expectations of S1 f''')
s.rep('''        for service in order:
            for action, situation in (("start", "already running"), ("reload", ""), ("restart", ""), ("stop", ""),
                                      ("reload", "while stopped"), ("start", "")):
                self.service_action(service, action, situation)''', '''        for service in order:
            for action, situation in (("start", "already running"), ("reload", ""), ("restart", ""), ("stop", ""),
                                      ("reload", "while stopped"), ("start", "")):
                # set3 (P3 b): a Reload of a stopped Postfix or Dovecot is an unmet prerequisite, answered 409.
                stopped_mail = situation == "while stopped" and service in ("postfix", "dovecot")
                done = self.service_action(service, action, situation, NOT_RUNNING if stopped_mail else None)
                if stopped_mail:
                    self.check(f"{service} (reload while stopped): the daemon was stopped before and is stopped after; nothing was started",
                               not done["daemon_before"]["running"] and not done["daemon_after"]["running"],
                               {"before": done["daemon_before"]["running"], "after": done["daemon_after"]["running"],
                                "vars": done.get("vars")})''')
s.rep('''                self.service_action("postfix", "stop", situation)
                self.service_action("postfix", "start", situation, (502, "SERVICE_ACTION_FAILED", "check"))''',
      '''                stopped = self.service_action("postfix", "stop", situation)
                # set3 (P3b, was O8): Stop is judged by the master's process, so it is answered as done.
                self.check("postfix (refused configuration): Stop answers success and the master is gone",
                           stopped["status"] == 200 and stopped["said"] == "success" and not stopped["daemon_after"]["running"],
                           {k: stopped[k] for k in ("status", "code", "reason", "answer", "vars")}
                           | {"master_running_after": stopped["daemon_after"]["running"],
                              "units_after": stopped["daemon_after"]["units"]})
                self.service_action("postfix", "start", situation, (502, "SERVICE_ACTION_FAILED", "check"))''')
s.rep('''            failed = self.service_action("postgresql", "reload", situation)
            moved = "pg_conf_load_time() moved" in failed["reload_evidence"]''',
      '''            failed = self.service_action("postgresql", "reload", situation, RELOAD_REREAD)
            moved = "pg_conf_load_time() moved" in failed["reload_evidence"]
            self.check("postgresql (reload hook): the server did re-read its files natively (pg_conf_load_time() moved, "
                       "same postmaster)", moved and failed["daemon_before"]["pid"] == failed["daemon_after"]["pid"],
                       {"conf_load_time": [failed["daemon_before"]["conf_load_time"], failed["daemon_after"]["conf_load_time"]],
                        "pid": [failed["daemon_before"]["pid"], failed["daemon_after"]["pid"]]})''')
s.rep('''        self.service_action("postgresql", "reload", "after the hook was removed")
''', '''        # set3 (P3 a, the other reading): a hook that fails BEFORE it signals the server. The unit reports the reload
        # as failed and the server was never signalled, so it did not re-read its files.
        situation = "the owner's reload hook fails before it signals the server"
        hook = self.owner("postgresql-reload-hook-before-signal", "owner-reload-hook", action="apply", unit=instance,
                          variant="fail-before-signal")
        self.current["reload_hook_before_signal"] = {"unit": instance, "exec_reload": hook.get("exec_reload"),
                                                     "script": hook.get("script_text")}
        try:
            unread = self.service_action("postgresql", "reload", situation, RELOAD_NOT_REREAD)
            still = unread["daemon_before"]["conf_load_time"] == unread["daemon_after"]["conf_load_time"] \\
                and bool(unread["daemon_after"]["conf_load_time"])
            self.current["reload_hook_before_signal"].update(
                server_did_not_reread=still, reload_result=unread["daemon_after"]["reload_result"],
                answer_detail=str((unread.get("vars") or {}).get("detail") or ""))
            self.check("postgresql (hook fails before the signal): natively the server did not re-read its files "
                       "(pg_conf_load_time() unchanged, same postmaster)",
                       still and unread["daemon_before"]["pid"] == unread["daemon_after"]["pid"],
                       {"conf_load_time": [unread["daemon_before"]["conf_load_time"], unread["daemon_after"]["conf_load_time"]],
                        "pid": [unread["daemon_before"]["pid"], unread["daemon_after"]["pid"]]})
        finally:
            self.owner("postgresql-reload-hook-before-signal-removed", "owner-reload-hook", action="restore", unit=instance)
        self.service_action("postgresql", "reload", "after the hook was removed")
''')
s.rep('''            self.check("postgresql: answers again at the end", now["running"], now)
''', '''            self.check("postgresql: answers again at the end", now["running"], now)

        # set3 (O11): while the setup waits at access_dns no service action is refused `server_setup_busy`.
        actions = self.current.get("actions") or []
        refusals = [dict(item, service=a["service"], action=a["action"], situation=a["situation"])
                    for a in actions for item in a["refused_while_busy"]]
        waiting = self.state.get("setup_waiting") or {}
        self.current["busy_refusals"] = {"actions": len(actions), "refusals": refusals,
                                         "server_setup_busy": sum(1 for r in refusals if r.get("code") == "server_setup_busy"),
                                         "setup_waiting": {k: waiting.get(k) for k in ("phase", "code", "status") if k in waiting}}
        self.check("no service action of this section was refused `server_setup_busy` while the setup waits at access_dns",
                   self.current["busy_refusals"]["server_setup_busy"] == 0, self.current["busy_refusals"])
''')
s.save()

t = Patch('test_settings_writes_trial.py')
t.rep('''        self.assertEqual(sorted(trial.CELLS), ["set1-arch", "set1-debian13", "set1-ubuntu",
                                               "set2-arch", "set2-debian13", "set2-ubuntu"])
        for platform in ("arch", "debian13", "ubuntu"):
            first, second = trial.CELLS["set1-" + platform], trial.CELLS["set2-" + platform]
            self.assertEqual(dataclasses.replace(second, name=first.name), first)''',
      '''        self.assertEqual(sorted(trial.CELLS), ["set1-arch", "set1-debian13", "set1-ubuntu",
                                               "set2-arch", "set2-debian13", "set2-ubuntu",
                                               "set3-arch", "set3-debian13", "set3-ubuntu"])
        for platform in ("arch", "debian13", "ubuntu"):
            first, second = trial.CELLS["set1-" + platform], trial.CELLS["set2-" + platform]
            self.assertEqual(dataclasses.replace(second, name=first.name), first)
            self.assertEqual(dataclasses.replace(trial.CELLS["set3-" + platform], name=first.name), first)''')
t.rep('''class CellTests(unittest.TestCase):''', '''class Set3Tests(unittest.TestCase):
    def test_the_answers_of_the_second_round_are_the_products(self):
        rpc = (REPO / "internal" / "transport" / "rpc.go").read_text(encoding="utf-8")
        outcome = (REPO / "cmd" / "panel" / "service_action_outcome.go").read_text(encoding="utf-8")
        for expected in (trial.NOT_RUNNING, trial.RELOAD_REREAD, trial.RELOAD_NOT_REREAD):
            self.assertIn('"' + expected[2] + '"', rpc)
            self.assertIn('"' + expected[1] + '"', outcome)
        self.assertEqual(trial.NOT_RUNNING[0], 409)
        self.assertIn("status = http.StatusConflict", outcome)

    def test_collection_time_shapes(self):
        base = trial.base
        fabricated = "$6$" + "saltsalt" + "$" + "A" * 86
        for text in (fabricated, "{SHA512-CRYPT}" + fabricated, "{SSHA256}" + "QUJD" * 12, "$2y$10$" + "b" * 53,
                     "SCRAM-SHA-256$4096:" + "c2FsdA==" + "$" + "QUJD" * 11 + ":" + "QUJD" * 11, "*" + "A1" * 20,
                     "$argon2id$v=19$m=65536,t=3,p=4$" + "c2FsdHNhbHQ" + "$" + "QUJD" * 10):
            self.assertEqual(base.hash_shaped_count("x " + text + " y"), 1, text[:12])
            self.assertNotIn(text, base.shape_text('{"k": "' + text + '"}'))
        for harmless in ("a 40-digit sha1 da39a3ee5e6b4b0d3255bfef95601890afd80709", "price $5 and $6", "{id}/admin-account",
                         "echo $1 $2", "2 * 3"):
            self.assertEqual(base.hash_shaped_count(harmless), 0, harmless)
            self.assertEqual(base.shape_text(harmless), harmless)
        key = "A" * 43 + "="
        self.assertEqual(base.shape_text("PrivateKey = " + key), "PrivateKey = [REDACTED]")
        self.assertEqual(base.shape_text('"preshared_key": "' + key + '"'), '"preshared_key": "[REDACTED]"')
        self.assertEqual(base.shape_text("PublicKey = " + key), "PublicKey = " + key)   # peers are named by it

        class Plain:
            def text(self, value):
                return value.replace("registered", "[REDACTED]")
        shaped = base.shape_redactor(Plain())
        self.assertEqual(shaped.text("registered " + fabricated), "[REDACTED] " + base.HASH_SHAPED_MARK)

    def test_the_hook_that_fails_before_the_signal(self):
        source = (HERE / "guest_settings_native.py").read_text(encoding="utf-8")
        self.assertIn('"fail-before-signal"', source)
        before, after = source.split('"fail-before-signal"', 1)[1].split("else:", 1)
        self.assertLess(before.index("systemctl reload"), before.index("kill -HUP"))
        self.assertLess(after.index("kill -HUP"), after.index("systemctl reload"))


class CellTests(unittest.TestCase):''')
t.save()
print('ok')
