import sys
p = sys.argv[1]
s = open(p, encoding="utf-8").read()
old = '''seen = {}
for name, directory, result in runs():'''
new = '''# H37: for the answers below the driver's own list of screen keys was older than the screens (it attached
# `dbconf.reloadFailed.notRestored`, `cron.unknown`, `postfix.queue.unknown` and the stage-less mail policy sentence).
# The keys the screens of this build use (web/src/components at the candidate commit) are looked up here in the same
# catalogue, with the answer's own values.
REPO = os.path.abspath(os.path.join(E, "..", "..", "..", "..", ".."))
sys.path.insert(0, os.path.join(REPO, "deploy", "e2e", "dns-pair-acceptance"))
try:
    import guidance
    from pathlib import Path
    CATALOG = guidance.load_catalog(Path(REPO) / "web" / "src" / "i18n")
except Exception as exc:  # noqa: BLE001
    CATALOG = None
    print("catalogue not loaded:", exc)


def screen_keys(code, reason, detail):
    if code == "CONFIG_RELOAD_FAILED":
        return ["dbconf.reloadFailed." + (reason if reason in ("restored", "restored_unit_reload_failed", "restored_running_unknown") else "notRestored"),
                "dbconf.reloadSaid"]
    if code == "CURRENT_SETTINGS_UNREADABLE" and reason == "scheduled_tasks":
        return (["cron.unknown." + detail] if detail else ["cron.unknown"]) + ["cron.unknown.said"]
    if code == "MAIL_QUEUE_UNREADABLE":
        return ["postfix.queue.unreadable" + ("." + reason if reason else ""), "postfix.queue.said"]
    if code == "MAIL_POLICY_NOT_RELOADED":
        return ["err.MAIL_POLICY_NOT_RELOADED" + ("." + reason if reason else ""), "mailpolicy.postfixSaid"]
    return None


def corrected(answer, texts, values):
    keys = screen_keys(answer.get("code"), answer.get("reason"), answer.get("detail"))
    if not keys or CATALOG is None:
        return texts
    out = {}
    for key in keys:
        if key in CATALOG["en"]:
            out[key] = {}
            for language in ("en", "tr"):
                text = CATALOG[language].get(key, key)
                for name, value in values.items():
                    text = text.replace("{" + name + "}", str(value))
                out[key][language] = text
    return out


seen = {}
for name, directory, result in runs():'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''            for k, v in (call.get("catalogue_texts") or {}).items():
                entry["texts"].setdefault(k, v)'''
new = '''            values = dict(answer.get("vars") or {})
            values.setdefault("service", "PostgreSQL" if "postgresql" in str(values.get("unit", "")) else values.get("unit", ""))
            for k, v in corrected(answer, call.get("catalogue_texts") or {}, values).items():
                entry["texts"].setdefault(k, v)'''
assert s.count(old) == 1
s = s.replace(old, new)
old = '''         "screen. `seen in` names the runs and the first request label.", ""]'''
new = '''         "screen. `seen in` names the runs and the first request label.", "",
         "H37: for `CONFIG_RELOAD_FAILED`, `CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks`, `MAIL_QUEUE_UNREADABLE` and",
         "`MAIL_POLICY_NOT_RELOADED` the driver's own key list was older than the screens; the sentences below are the",
         "ones the screens of this build use for that answer (`ConfigFileNotices`, `DomainCronManager`, the queue tab, the",
         "mail policy card), looked up here in the same catalogue. The `catalogue_texts` inside the cells' `section.json`",
         "files keep what the driver attached.", ""]'''
assert s.count(old) == 1
s = s.replace(old, new)
open(p, "w", encoding="utf-8", newline="\n").write(s)
print("summary fixed")
