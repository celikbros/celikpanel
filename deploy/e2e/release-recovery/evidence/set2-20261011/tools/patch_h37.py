"""set2 H37 (found while writing the texts, after the cells): the settings driver's list of screen keys was older than
the screens of this build for four answers, and its `answer` record did not keep the answer's `detail` token. No cell
ran with this change; the verbatim-text file of the evidence was generated with the same corrected keys."""
import sys
d = sys.argv[1]
p = d + "/settings_writes_trial.py"
s = open(p, encoding="utf-8", newline="").read()


def rep(old, new):
    global s
    assert s.count(old) == 1, (old[:70], s.count(old))
    s = s.replace(old, new)


rep('''    if code in ("CURRENT_SETTINGS_UNREADABLE", "MAIL_QUEUE_UNREADABLE") and kind in UNKNOWN_KEYS:
        keys.append(UNKNOWN_KEYS[kind])
''',
    '''    if code == "CURRENT_SETTINGS_UNREADABLE" and kind == "cron":
        # H37 (set2): DomainCronManager shows the verified cause, or the neutral sentence, and crontab's own line
        keys += ["cron.unknown." + str(body["detail"]) if body.get("detail") else "cron.unknown", "cron.unknown.said"]
    elif code == "MAIL_QUEUE_UNREADABLE" and kind == "queue":
        keys += ["postfix.queue.unreadable" + ("." + reason if reason else ""), "postfix.queue.said"]
    elif code in ("CURRENT_SETTINGS_UNREADABLE", "MAIL_QUEUE_UNREADABLE") and kind in UNKNOWN_KEYS:
        keys.append(UNKNOWN_KEYS[kind])
''')
rep('''    if code == "MAIL_POLICY_NOT_RELOADED":
        keys.append("mailpolicy.reloadSaid")
''',
    '''    if code == "MAIL_POLICY_NOT_RELOADED":
        keys.append("mailpolicy.postfixSaid" if reason == "check" else "mailpolicy.reloadSaid")
''')
rep('''    if code == "CONFIG_RELOAD_FAILED":
        keys.append("dbconf.reloadFailed." + ("restored" if reason == "restored" else "notRestored"))
''',
    '''    if code == "CONFIG_RELOAD_FAILED":
        # H37 (set2): ConfigFileNotices has a sentence per reason; only an unknown reason falls back to notRestored
        keys.append("dbconf.reloadFailed." + (reason if reason in ("restored", "restored_unit_reload_failed",
                                                                  "restored_running_unknown") else "notRestored"))
        keys.append("dbconf.reloadSaid")
''')
rep('''            entry["answer"] = {k: parsed.get(k) for k in ("code", "reason", "error", "vars", "partial_success",
                                                          "mutation_applied") if parsed.get(k) is not None}
''',
    '''            entry["answer"] = {k: parsed.get(k) for k in ("code", "reason", "detail", "error", "vars", "partial_success",
                                                          "mutation_applied") if parsed.get(k) is not None}
''')
open(p, "w", encoding="utf-8", newline="").write(s)

p = d + "/test_settings_writes_trial.py"
s = open(p, encoding="utf-8", newline="").read()
rep('''        self.assertIn("unchanged_reloaded", (HERE / "settings_writes_trial.py").read_text(encoding="utf-8"))
''',
    '''        self.assertIn("unchanged_reloaded", (HERE / "settings_writes_trial.py").read_text(encoding="utf-8"))

    def test_screen_keys_of_the_corrected_answers_are_the_screens_own(self):
        keys = trial.screen_keys
        self.assertIn("dbconf.reloadFailed.restored_unit_reload_failed",
                      keys("config", 502, {"code": "CONFIG_RELOAD_FAILED", "reason": "restored_unit_reload_failed"}))
        self.assertIn("dbconf.reloadFailed.notRestored", keys("config", 502, {"code": "CONFIG_RELOAD_FAILED", "reason": "not_restored"}))
        self.assertIn("cron.unknown.cron_allow", keys("cron", 502, {"code": "CURRENT_SETTINGS_UNREADABLE", "reason": "scheduled_tasks",
                                                                   "detail": "cron_allow"}))
        neutral = keys("cron", 502, {"code": "CURRENT_SETTINGS_UNREADABLE", "reason": "scheduled_tasks"})
        self.assertIn("cron.unknown", neutral)
        self.assertIn("cron.unknown.said", neutral)
        self.assertIn("postfix.queue.unreadable.postfix_config", keys("queue", 502, {"code": "MAIL_QUEUE_UNREADABLE", "reason": "postfix_config"}))
        self.assertIn("mailpolicy.postfixSaid", keys("mailpolicy", 502, {"code": "MAIL_POLICY_NOT_RELOADED", "reason": "check"}))
        components = (HERE.parents[2] / "web" / "src" / "components" / "ConfigFileNotices.tsx").read_text(encoding="utf-8")
        self.assertIn("'dbconf.reloadFailed.restored_unit_reload_failed'", components)
''')
open(p, "w", encoding="utf-8", newline="").write(s)
print("H37 patched")
