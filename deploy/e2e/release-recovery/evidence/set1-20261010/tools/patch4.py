"""set1 H26: S5 records what MariaDB's own program makes of a value it reads as a number with a size suffix
('plenty'), next to the Panel's answer, and then corrects the file through the Panel again."""
from pathlib import Path
import sys

ROOT = Path(sys.argv[1])


def sub(t, old, new, count=1):
    assert t.count(old) == count, (t.count(old), old[:70])
    return t.replace(old, new)


p = ROOT / "guest_settings_native.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''def read_queue(args: dict) -> dict:''', '''def read_mariadb_check(args: dict) -> dict:
    """What the installed server program makes of the option file as it is on disk now: the same read-only run the
    Agent uses before it installs a file (``--help --verbose`` with a private, empty data directory: it reads the
    option files, prints the resulting variables and exits; it starts nothing)."""
    import tempfile
    path = editable_path(args["path"])
    variables = [v for v in args.get("variables", []) if SETTING_RE.fullmatch(v)]
    program = shutil.which("mariadbd", path=ENV["PATH"]) or shutil.which("mysqld", path=ENV["PATH"])
    if not program:
        return {"program": None, "at": utc()}
    with tempfile.TemporaryDirectory(prefix="set1-mariadb-check-") as private:
        done = run([program, "--defaults-file=" + path, "--datadir=" + private, "--help", "--verbose"], timeout=60)
    table = {}
    for line in done.get("stdout", "").splitlines():
        parts = line.split()
        if len(parts) == 2 and parts[0].replace("-", "_") in variables:
            table[parts[0].replace("-", "_")] = parts[1]
    return {"program": program, "path": path, "returncode": done.get("returncode"), "status": done.get("status"),
            "stderr": done.get("stderr", "")[-3000:], "resulting_variables": table, "at": utc()}


def read_queue(args: dict) -> dict:''')
t = sub(t, '''"read-postgres": read_postgres, "read-mariadb": read_mariadb, "read-queue": read_queue,''',
        '''"read-postgres": read_postgres, "read-mariadb": read_mariadb, "read-mariadb-check": read_mariadb_check,
    "read-queue": read_queue,''')
p.write_text(t, encoding="utf-8", newline="\n")

p = ROOT / "settings_writes_trial.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''        n2b = self.file("c-file-after-bad-value", path)
        self.unchanged("c (unusable value)", n2, n2b)''', '''        n2b = self.file("c-file-after-bad-value", path)
        self.unchanged("c (unusable value)", n2, n2b)

        # (c, observation) a value MariaDB itself reads as a number with a size suffix and adjusts
        adjusted, _, _ = set_mariadb_option(text1, "max_connections", MARIADB_ADJUSTED_VALUE)
        cx = self.config_post(f"S5c set max_connections = {MARIADB_ADJUSTED_VALUE}", path, adjusted, fresh("S5c read (adjusted value)"), service)
        own = self.snap("c-mariadbd-own-reading", "read-mariadb-check", path=path, variables=["max_connections"])
        xbody = cx["_parsed"] if isinstance(cx["_parsed"], dict) else {}
        self.current["adjusted_value"] = {
            "value": MARIADB_ADJUSTED_VALUE, "panel_status": cx["status"], "panel_applied": xbody.get("applied"),
            "panel_daemon_check": xbody.get("daemon_check"), "panel_answer": cx.get("answer"),
            "written": self.file("c-file-after-adjusted-value", path)["file"].get("text") == adjusted,
            "mariadbd_returncode_on_the_file_now": own.get("returncode"),
            "mariadbd_resulting_max_connections": (own.get("resulting_variables") or {}).get("max_connections"),
            "mariadbd_stderr_lines": [l for l in own.get("stderr", "").splitlines() if "max" in l.lower()][-4:]}
        if cx["status"] == 200:
            self.note(f"`max_connections = {MARIADB_ADJUSTED_VALUE}` was accepted and written: the Panel answered what "
                      "MariaDB's own program said about the file; MariaDB's reading of it is recorded",
                      self.current["adjusted_value"])
            self.check("c (adjusted value): where the Panel accepted the value, MariaDB's own program accepts the file "
                       "as written (exit 0)", own.get("returncode") == 0, self.current["adjusted_value"])
            back = self.config_post("S5c correct max_connections on the page again", path, text1,
                                    fresh("S5c read (before the correction)"), service)
            n2b = self.file("c-file-after-correction", path)
            self.check("c (adjusted value): the correction is saved (200) and the file is the one from sub-step b again",
                       back["status"] == 200 and n2b["file"].get("text") == text1, back.get("answer") or back["status"])
        else:
            self.refused("c (adjusted value): refused 422 CONFIG_INVALID (daemon)", cx, 422, "CONFIG_INVALID", "daemon")''')
t = sub(t, '''MARIADB_UNUSABLE_VALUE = "unlimited"''', '''MARIADB_UNUSABLE_VALUE = "unlimited"
MARIADB_ADJUSTED_VALUE = "plenty"''')
p.write_text(t, encoding="utf-8", newline="\n")

p = ROOT / "test_settings_writes_trial.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''        self.assertNotIn(trial.MARIADB_UNUSABLE_VALUE[0].lower(), "kmgtpe0123456789")''',
        '''        self.assertNotIn(trial.MARIADB_UNUSABLE_VALUE[0].lower(), "kmgtpe0123456789")
        self.assertIn(trial.MARIADB_ADJUSTED_VALUE[0].lower(), "kmgtpe")
        self.assertIn("read-mariadb-check", native.MODES)''')
p.write_text(t, encoding="utf-8", newline="\n")
print("patched H26")
