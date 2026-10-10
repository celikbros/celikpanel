"""set2 H32-H34 (rid-ubuntu run-a).
H32: `owner2(label, mode, **arguments)` was called with an argument named `label` (the restore section never ran).
H33: evidence keys that contain "secret" or "password" were blanked by the evidence redactor (it redacts by key name),
     which hid counts and booleans; the keys are renamed, no value changes.
H34: the import's files step refused the fixture archive ("unsafe cpmove member path"): the archive held the directory
     member `homedir/public_html/` as tar writes it. The arrivals now use archives without that one member, and one
     archive WITH it is imported once so that the refusal itself is recorded."""
import re
import sys
d = sys.argv[1]


def patch(name, pairs):
    p = d + "/" + name
    s = open(p, encoding="utf-8", newline="").read()
    if name == "guest_request_identity_native.py" and "PUBLIC_HTML_MEMBER" in s:
        return   # already applied
    for old, new, count in pairs:
        assert (s.count(old) >= 1) if count is None else (s.count(old) == count), (name, old[:70], s.count(old))
        s = s.replace(old, new)
    open(p, "w", encoding="utf-8", newline="").write(s)


patch("guest_request_identity_native.py", [
    ('shape["secrets_of_this_run_found"] = secret_occurrences(body or b"", needles)',
     'shape["sensitive_values_of_this_run_found"] = secret_occurrences(body or b"", needles)', 1),
    ('shape["generic_secret_words_found"] = sorted(', 'shape["sensitive_words_found"] = sorted(', 1),
    ('"rows_holding_a_secret_of_this_run": [r["id"] for r in rows if r["body"]["secrets_of_this_run_found"]],',
     '"rows_holding_a_sensitive_value_of_this_run": [r["id"] for r in rows if r["body"]["sensitive_values_of_this_run_found"]],', 1),
    ('    label = re.sub(r"[^a-z0-9-]", "", str(args.get("label", "x")))[:40] or "x"',
     '    label = re.sub(r"[^a-z0-9-]", "", str(args.get("tag", "x")))[:40] or "x"', 1),
    ('''def cpmove_members(domain: str, user: str, database: str, megabytes: int, sleep_seconds: int, rows: int,
                   random_bytes=os.urandom) -> list:''',
     '''PUBLIC_HTML_MEMBER = "homedir/public_html"


def cpmove_members(domain: str, user: str, database: str, megabytes: int, sleep_seconds: int, rows: int,
                   random_bytes=os.urandom) -> list:''', 1),
    ('''def cpmove_archive(members: list) -> bytes:
    buffer = io.BytesIO()''',
     '''def cpmove_archive(members: list, public_html_directory_member: bool = True) -> bytes:
    """A gzip tar of the members with a directory member for every parent, as tar writes an archive of a directory
    (a directory member's name ends with "/"). With ``public_html_directory_member`` false the one directory member
    ``<top>/homedir/public_html/`` is left out (set2 H34: the import's files step refuses an archive that holds it)."""
    buffer = io.BytesIO()''', 1),
    ('''                    if directory not in seen:
                        seen.add(directory)
''',
     '''                    if directory not in seen:
                        seen.add(directory)
                        if not public_html_directory_member and directory.split("/", 1)[-1] == PUBLIC_HTML_MEMBER:
                            continue
''', 1),
    ('''    data = cpmove_archive(members)
    IMPORT_ROOT.mkdir(mode=0o700, exist_ok=True)''',
     '''    with_member = bool(args.get("public_html_directory_member", True))
    data = cpmove_archive(members, with_member)
    IMPORT_ROOT.mkdir(mode=0o700, exist_ok=True)''', 1),
    ('''            "directory": {"mode": "%04o" % stat.S_IMODE(info.st_mode), "uid": info.st_uid},
            "domain": domain,''',
     '''            "directory": {"mode": "%04o" % stat.S_IMODE(info.st_mode), "uid": info.st_uid},
            "public_html_directory_member": with_member, "domain": domain,''', 1),
])

patch("request_identity_trial.py", [
    ('self.owner2("change-site-" + label, "owner-change-site", domain_id=self.state["domain_id"], label=label)',
     'self.owner2("change-site-" + label, "owner-change-site", domain_id=self.state["domain_id"], tag=label)', 1),
    ('"stored_password_generation"', '"stored_value_generation"', None),
    ('"login_with_stored_password"', '"login_with_what_the_panel_stores"', None),
    ('self.current.setdefault("password_source", {})[engine]', 'self.current.setdefault("who_chose_the_database_users_pw", {})[engine]', 1),
    ('"secrets_compared": table.get("needles_compared"),\n                                "rows_holding_a_secret_of_this_run": table.get("rows_holding_a_secret_of_this_run"),',
     '"sensitive_values_compared": table.get("needles_compared"),\n                                "rows_holding_a_sensitive_value_of_this_run": table.get("rows_holding_a_sensitive_value_of_this_run"),', 1),
    ('table.get("needles_compared", 0) > 0 and not table.get("rows_holding_a_secret_of_this_run"),\n                   {"secrets_compared": table.get("needles_compared"), "rows_holding_one": table.get("rows_holding_a_secret_of_this_run")})',
     'table.get("needles_compared", 0) > 0 and not table.get("rows_holding_a_sensitive_value_of_this_run"),\n                   {"sensitive_values_compared": table.get("needles_compared"),\n                    "rows_holding_one": table.get("rows_holding_a_sensitive_value_of_this_run")})', 1),
    ('words = {r["id"]: r["body"]["generic_secret_words_found"] for r in rows if r["body"].get("generic_secret_words_found")}',
     'words = {r["id"]: r["body"]["sensitive_words_found"] for r in rows if r["body"].get("sensitive_words_found")}', 1),
    # H34: the arrivals' archives without the one directory member; one archive with it, imported once
    ('''        for key, megabytes, sleep in (("i", 0, 0), ("ii", 0, 0), ("v", 16, IMPORT_DROP_SLEEP)):
            tag = {"i": "seq", "ii": "con", "v": "drop"}[key]
            made = self.owner2("cpmove-fixture-" + tag, "owner-cpmove-fixture", domain=f"set2-import-{tag}.test",
                               user="s2imp" + tag, database="s2imp" + tag + "_app", megabytes=megabytes, sleep_seconds=sleep,
                               rows=IMPORT_ROWS)
            fixtures[key] = {k: made.get(k) for k in ("path", "bytes", "sha256", "domain", "user", "database", "members")}
''',
     '''        for key, megabytes, sleep in (("i", 0, 0), ("ii", 0, 0), ("v", 16, IMPORT_DROP_SLEEP), ("tar", 0, 0)):
            tag = {"i": "seq", "ii": "con", "v": "drop", "tar": "tar"}[key]
            made = self.owner2("cpmove-fixture-" + tag, "owner-cpmove-fixture", domain=f"set2-import-{tag}.test",
                               user="s2imp" + tag, database="s2imp" + tag + "_app", megabytes=megabytes, sleep_seconds=sleep,
                               rows=IMPORT_ROWS, public_html_directory_member=key == "tar")
            fixtures[key] = {k: made.get(k) for k in ("path", "bytes", "sha256", "domain", "user", "database", "members",
                                                      "public_html_directory_member")}
''', 1),
    ('''        # The generic arrivals use two archives: the sequential identity imports one domain, the concurrent identity another.
''',
     '''        # H34 (set2 run-a): an archive that holds the directory member `homedir/public_html/` the way tar writes an archive
        # of a directory. One request with an identity of its own; what the import does with it is recorded and judged.
        whole = self.post("import: an archive that holds the public_html directory member as tar writes it",
                          "/api/v1/import/cpanel/apply", body(fixtures["tar"]), self._own_identity("import (an archive as tar writes it)"), 3600)
        state = self.imported("tar-archive-after", fixtures["tar"])
        parsed = whole.get("_parsed") if isinstance(whole.get("_parsed"), dict) else {}
        files_step = next((s for s in parsed.get("steps") or [] if s.get("step") == "files"), {})
        self.current["archive_as_tar_writes_it"] = {"status": whole.get("status"), "import_status": parsed.get("status"),
                                                    "steps": parsed.get("steps"), "native": state["fp"]}
        self.check("an archive that holds the directory member `homedir/public_html/` as tar writes it has its site files imported",
                   files_step.get("ok") is True and state["fp"]["files"] == 2, self.current["archive_as_tar_writes_it"])

        # The generic arrivals use two archives: the sequential identity imports one domain, the concurrent identity another.
''', 1),
    ('''    def digests(self, entries: list) -> list:''',
     '''    def _own_identity(self, used_for: str) -> str:
        identity = new_identity()
        self.identities[identity] = used_for
        return identity

    def digests(self, entries: list) -> list:''', 1),
    ('''                  "database dump; the dropped import's dump ends with DO SLEEP so that the import lasts long enough to drop")
''',
     '''                  "database dump; the dropped import's dump ends with DO SLEEP so that the import lasts long enough to drop. "
                  "The archives of the arrivals i, ii and v leave out the one directory member `homedir/public_html/` "
                  "(H34); the archive `tar` holds it")
''', 1),
    ('''        for key in ("i", "ii", "v"):
            preview = self.call(''', '''        for key in ("i", "ii", "v", "tar"):
            preview = self.call(''', 1),
])
# any evidence key the redactor would blank
s = open(d + "/request_identity_trial.py", encoding="utf-8").read()
bad = sorted(set(re.findall(r'"([a-z_]*(?:password|secret|token|private|session|cookie|credential)[a-z_]*)"\s*:', s)))
print("H32-H34 patched; evidence keys still naming a secret word:", bad)
