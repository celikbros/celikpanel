"""set1 H25: (1) the unusable MariaDB value is one MariaDB cannot read as a number with a size suffix ('plenty' is
read as 0 with the peta suffix and adjusted); every "unchanged" comparison uses the file as it was just before
that request. (2) the catalogue sentences recorded with an answer are the ones the screen shows for that answer."""
from pathlib import Path
import re
import sys

ROOT = Path(sys.argv[1])


def sub(t, old, new, count=1):
    assert t.count(old) == count, (t.count(old), old[:70])
    return t.replace(old, new)


p = ROOT / "settings_writes_trial.py"
t = p.read_text(encoding="utf-8")

# (2) screen keys
t = sub(t, '''def section_verdict(checks: list, error: str | None = None) -> str:''',
        '''STALE_KEYS = {"cron": "cron.stale", "mailpolicy": "mailpolicy.stale", "backup": "backup.auto.stale",
              "config": "dbconf.stale", "catchall": "mail.catchAll.stale"}
UNKNOWN_KEYS = {"cron": "cron.unknown", "mailpolicy": "mailpolicy.unknown", "backup": "backup.auto.unknown",
                "config": "dbconf.unknown", "catchall": "mail.catchAll.unknown", "queue": "postfix.queue.unknown"}
SAVED_KEYS = {"reloaded": "dbconf.saved.reloaded", "restart_required": "dbconf.saved.restartRequired",
              "not_running": "dbconf.saved.notRunning"}


def screen_keys(kind: str | None, status: int, body: Any) -> list:
    """The catalogue keys the screen of ``kind`` shows for this answer (web/src: CurrentSettings, ConfigFileNotices,
    PostfixManagement, DomainCronManager, DomainBackupManager, the catch-all card, the queue tab)."""
    body = body if isinstance(body, dict) else {}
    code, reason = str(body.get("code") or ""), str(body.get("reason") or "")
    keys = ([f"err.{code}.{reason}"] if code and reason else []) + ([f"err.{code}"] if code else [])
    if code in ("SETTINGS_CHANGED", "SETTINGS_VERSION_REQUIRED") and kind in STALE_KEYS:
        keys.append(STALE_KEYS[kind])
    if code in ("CURRENT_SETTINGS_UNREADABLE", "MAIL_QUEUE_UNREADABLE") and kind in UNKNOWN_KEYS:
        keys.append(UNKNOWN_KEYS[kind])
    lock = reason if code == "MAIL_POLICY_RESTRICTIONS_UNMANAGED" else str(body.get("dnsbl_locked") or "") \\
        if kind == "mailpolicy" and status == 200 else ""
    if lock:
        keys += [f"mailpolicy.dnsblLocked.{lock}", "mailpolicy.dnsblLockedAction"]
    if code == "MAIL_POLICY_NOT_RELOADED":
        keys.append("mailpolicy.reloadSaid")
    if code == "CONFIG_INVALID":
        keys.append(f"dbconf.refused.{reason}")
    if code == "CONFIG_RELOAD_FAILED":
        keys.append("dbconf.reloadFailed." + ("restored" if reason == "restored" else "notRestored"))
    if kind == "config" and status == 200 and body.get("success"):
        if body.get("unchanged"):
            keys.append("dbconf.saved.unchanged")
        else:
            keys.append(SAVED_KEYS.get(body.get("applied"), "dbconf.saved.written"))
            if body.get("restart_required"):
                keys.append("dbconf.saved.waitsForRestart")
            if body.get("applied") == "reloaded" and body.get("daemon_check") == "not_checked":
                keys.append("dbconf.saved.notChecked")
            if body.get("backup"):
                keys.append("dbconf.saved.backup")
    return keys


def section_verdict(checks: list, error: str | None = None) -> str:''')

start = t.index("    def catalogue(self, body: Any, prefixes: tuple = ()")
end = t.index("    def call(self, label: str, method: str, path: str")
t = t[:start] + '''    def catalogue(self, kind: str | None, status: int, body: Any, values: dict | None = None) -> dict:
        """The sentences the installed build's catalogues hold for this answer on its screen (EN and TR), beside the
        API's own English sentence. Looked up by key; the driver does not render a screen."""
        english = self.translator.catalog["en"]
        merged = dict((body or {}).get("vars") or {}) if isinstance(body, dict) else {}
        if isinstance(body, dict) and body.get("restart_required"):
            merged["names"] = ", ".join(body["restart_required"])
        merged.update(values or {})
        texts = {}
        for key in screen_keys(kind, status, body):
            if key in english and key not in texts:
                texts[key] = {language: self.translator.text(key, merged, language=language) for language in ("en", "tr")}
        return texts

''' + t[end:]
t = sub(t, '''             prefixes: tuple = (), needles: tuple = (), keys: tuple = (), values: dict | None = None) -> dict:''',
        '''             kind: str | None = None, values: dict | None = None) -> dict:''')
t = sub(t, '''        texts = self.catalogue(parsed, prefixes, needles, keys, values)''',
        '''        texts = self.catalogue(kind, response.status, parsed, values)''')
t = sub(t, '''f"/api/v1/domains/{self.state['domain_id']}/cron", prefixes=("cron.",),
                         needles=("unknown", "stale", "empty"))''',
        '''f"/api/v1/domains/{self.state['domain_id']}/cron", kind="cron")''')
t = sub(t, '''stale = dict(prefixes=("cron.",), needles=("stale", "changed", "unknown", "duplicate"))''', '''stale = dict(kind="cron")''')
t = sub(t, '''"/api/v1/mail/policy", prefixes=("mailpolicy.",), needles=("unknown", "locked", "lock"))''',
        '''"/api/v1/mail/policy", kind="mailpolicy")''')
t = sub(t, '''refusal = dict(prefixes=("mailpolicy.",), needles=("stale", "changed", "locked", "lock", "reload", "unknown"))''',
        '''refusal = dict(kind="mailpolicy")''')
t = sub(t, '''hints = dict(prefixes=("backup.auto.", "backup."), needles=("stale", "changed", "unknown"))''', '''hints = dict(kind="backup")''')
t = sub(t, '''quote(path, safe="/"), prefixes=("dbconf.",),
                         needles=("unknown",), values={"file": path.rsplit("/", 1)[-1]})''',
        '''quote(path, safe="/"), kind="config",
                         values={"file": path.rsplit("/", 1)[-1]})''')
t = sub(t, '''body, timeout=180, prefixes=("dbconf.",),
                         needles=("stale", "refused", "reloadFailed", "saved"),
                         values=''', '''body, timeout=180, kind="config",
                         values=''')
t = sub(t, '''hints = dict(prefixes=("mail.catchAll.",), needles=("stale", "unknown"))''', '''hints = dict(kind="catchall")''')
t = sub(t, '''qhints = dict(prefixes=("queue.", "postfix.", "mail.queue."), needles=("unknown", "unreadable", "empty"))''',
        '''qhints = dict(kind="queue")''')
assert "prefixes" not in t and "needles" not in t

# (1) baselines and the unusable value
t = sub(t, '''        self.refused("d: empty content is refused 422 CONFIG_INVALID (empty)", d, 422, "CONFIG_INVALID", "empty")
        self.unchanged("d", n1, self.file("d-conf-after-empty", conf))''',
        '''        self.refused("d: empty content is refused 422 CONFIG_INVALID (empty)", d, 422, "CONFIG_INVALID", "empty")
        self.unchanged("d", n2, self.file("d-conf-after-empty", conf))''')
t = sub(t, '''        self.unchanged("e (lockout)", h1, self.file("e-hba-after-lockout", hba))''',
        '''        self.unchanged("e (lockout)", h2, self.file("e-hba-after-lockout", hba))''')
t = sub(t, '''        bad, _, _ = set_mariadb_option(text1, "max_connections", "plenty")
        v1 = fresh("S5c read again")
        c2 = self.config_post("S5c set max_connections to a value MariaDB refuses", path, bad, v1, service)
        self.refused("c: an unusable value is refused 422 CONFIG_INVALID (daemon)", c2, 422, "CONFIG_INVALID", "daemon")
        self.unchanged("c (unusable value)", n1, self.file("c-file-after-bad-value", path))''',
        '''        bad, _, _ = set_mariadb_option(text1, "max_connections", MARIADB_UNUSABLE_VALUE)
        v1 = fresh("S5c read again")
        c2 = self.config_post("S5c set max_connections to a value MariaDB refuses", path, bad, v1, service)
        self.refused("c: an unusable value is refused 422 CONFIG_INVALID (daemon)", c2, 422, "CONFIG_INVALID", "daemon")
        n2b = self.file("c-file-after-bad-value", path)
        self.unchanged("c (unusable value)", n2, n2b)''')
t = sub(t, '''        self.refused("d: empty content is refused 422 CONFIG_INVALID (empty)", d, 422, "CONFIG_INVALID", "empty")
        self.unchanged("d", n1, self.file("d-file-after-empty", path))''',
        '''        self.refused("d: empty content is refused 422 CONFIG_INVALID (empty)", d, 422, "CONFIG_INVALID", "empty")
        self.unchanged("d", n2b, self.file("d-file-after-empty", path))''')
t = sub(t, '''HBA_GOOD_LINE = ''', '''# Not "plenty" or "many": MariaDB reads a leading size suffix (k, m, g, t, p, e) and takes them as 0 with a suffix,
# which it adjusts to the minimum and accepts (set1 Debian run-a). "unlimited" starts with no suffix letter.
MARIADB_UNUSABLE_VALUE = "unlimited"
HBA_GOOD_LINE = ''')
p.write_text(t, encoding="utf-8", newline="\n")

p = ROOT / "test_settings_writes_trial.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''class CronRuleTests(unittest.TestCase):''', '''class ScreenKeyTests(unittest.TestCase):
    def test_keys_follow_the_answer(self):
        keys = trial.screen_keys
        self.assertEqual(keys("cron", 409, {"code": "SETTINGS_CHANGED", "reason": "scheduled_tasks"}),
                         ["err.SETTINGS_CHANGED.scheduled_tasks", "err.SETTINGS_CHANGED", "cron.stale"])
        self.assertIn("backup.auto.stale", keys("backup", 409, {"code": "SETTINGS_VERSION_REQUIRED"}))
        self.assertIn("postfix.queue.unknown", keys("queue", 502, {"code": "MAIL_QUEUE_UNREADABLE"}))
        self.assertIn("mail.catchAll.stale", keys("catchall", 409, {"code": "SETTINGS_CHANGED"}))
        self.assertEqual(keys("mailpolicy", 200, {"dnsbl_locked": "variable"}),
                         ["mailpolicy.dnsblLocked.variable", "mailpolicy.dnsblLockedAction"])
        self.assertEqual(keys("mailpolicy", 200, {"dnsbl_zones": []}), [])
        self.assertIn("mailpolicy.dnsblLocked.variable",
                      keys("mailpolicy", 409, {"code": "MAIL_POLICY_RESTRICTIONS_UNMANAGED", "reason": "variable"}))
        self.assertIn("mailpolicy.reloadSaid", keys("mailpolicy", 502, {"code": "MAIL_POLICY_NOT_RELOADED"}))
        self.assertIn("dbconf.refused.lockout", keys("config", 422, {"code": "CONFIG_INVALID", "reason": "lockout"}))
        self.assertIn("dbconf.reloadFailed.notRestored", keys("config", 502, {"code": "CONFIG_RELOAD_FAILED", "reason": "not_restored"}))
        self.assertEqual(keys("config", 200, {"success": True, "applied": "restart_required", "backup": "/x"}),
                         ["dbconf.saved.restartRequired", "dbconf.saved.backup"])
        self.assertEqual(keys("config", 200, {"success": True, "unchanged": True}), ["dbconf.saved.unchanged"])
        self.assertEqual(keys("config", 200, {"Content": "x", "Version": "cf1-1"}), [])
        self.assertEqual(keys(None, 200, []), [])

    def test_the_unusable_mariadb_value_starts_with_no_size_suffix(self):
        self.assertNotIn(trial.MARIADB_UNUSABLE_VALUE[0].lower(), "kmgtpe0123456789")


class CronRuleTests(unittest.TestCase):''')
p.write_text(t, encoding="utf-8", newline="\n")
print("patched H25")
