from pathlib import Path
import sys

ROOT = Path(sys.argv[1])


def sub(t, old, new, count=1):
    assert t.count(old) == count, (t.count(old), old[:70])
    return t.replace(old, new)


p = ROOT / "guest_settings_native.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''    units = [unit_name(u) if not u.startswith("celikpanel-") else u for u in args["units"]]
    since = int(args["since_epoch"])''', '''    units = [unit_name(u) for u in args.get("units", [])]
    since = int(args["since_epoch"])''')
t = sub(t, '''    for tag in args.get("identifiers", []):
        if re.fullmatch(r"[A-Za-z0-9_./-]{1,40}", tag):
            argv += ["-t", tag]
    return {"units": units, "since_epoch": since, "journal": run(argv), "at": utc()}''',
        '''    identifiers = [tag for tag in args.get("identifiers", []) if re.fullmatch(r"[A-Za-z0-9_./-]{1,40}", tag)]
    for tag in identifiers:
        argv += ["-t", tag]
    if not units and not identifiers:
        raise Refused("the journal reader needs a unit or an identifier")
    return {"units": units, "identifiers": identifiers, "since_epoch": since, "journal": run(argv), "at": utc()}''')
p.write_text(t, encoding="utf-8", newline="\n")

p = ROOT / "settings_writes_trial.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''def section_verdict(checks: list, error: str | None = None) -> str:''',
        '''def substep_verdicts(checks: list) -> dict:
    """Per sub-step (the check name's prefix before the first colon, without a bracketed variant): its verdict."""
    groups: dict[str, list] = {}
    for check in checks:
        prefix = check["name"].split(":", 1)[0].split(" (", 1)[0].strip() if ":" in check["name"] else "section"
        groups.setdefault(prefix, []).append(check)
    return {prefix: section_verdict(items) for prefix, items in groups.items()}


def section_verdict(checks: list, error: str | None = None) -> str:''')
t = sub(t, '''            current["verdict"] = section_verdict(current["checks"], error)
''', '''            current["verdict"] = section_verdict(current["checks"], error)
            current["substeps"] = substep_verdicts(current["checks"])
''')
t = sub(t, '''                                  "finished_at": current["finished_at"], "checks": len(current["checks"]),''',
        '''                                  "finished_at": current["finished_at"], "checks": len(current["checks"]),
                                  "substeps": current["substeps"],''')
t = sub(t, '''    def smtp(self, label: str) -> dict:
        return self.snap(label, "read-smtp", sender=f"probe@{SITE_DOMAIN}", recipient=self.state.get("mailbox"))
''', '''    def smtp(self, label: str) -> dict:
        value: dict = {}
        for attempt in range(1, 7):   # a just-started Postfix needs a moment before it listens
            value = self.snap(label + ("" if attempt == 1 else f"-try{attempt}"), "read-smtp",
                              sender=f"probe@{SITE_DOMAIN}", recipient=self.state.get("mailbox"))
            if value["25"].get("ok") and value["587"].get("ok"):
                break
            time.sleep(3)
        return value
''')
t = sub(t, '''    def journal(self, label: str, units: list, since: float) -> str:
        value = self.snap(label, "read-journal", units=units, since_epoch=int(since), lines=400)
        return value["journal"].get("stdout", "")
''', '''    def journal(self, label: str, units: list, since: float, identifiers: tuple = ()) -> str:
        value = self.snap(label, "read-journal", units=units, identifiers=list(identifiers), since_epoch=int(since), lines=400)
        return value["journal"].get("stdout", "")
''')
t = sub(t, '''        postfix_units = ["postfix.service", "postfix@-.service"]
''', '''        postfix_log = ("postfix/master", "postfix/postfix-script")   # whichever unit the platform runs Postfix in
''')
t = sub(t, '''        log = self.journal("c-postfix-journal", postfix_units, t0)''',
        '''        log = self.journal("c-postfix-journal", [], t0, postfix_log)''')
t = sub(t, '''self.journal("f-postfix-journal", postfix_units, t0)''', '''self.journal("f-postfix-journal", [], t0, postfix_log)''')
t = sub(t, '''            self.check("f: the owner's unfinished edit makes Postfix refuse its configuration (`postfix check` fails)",
                       True if p9["check"].get("returncode") not in (0, None) else None, p9["check"])
            f0 = self.policy_get("S2f read with the owner's unfinished edit")
            current = f0["_parsed"] if isinstance(f0["_parsed"], dict) else {}
            self.current["read_under_typo"] = {"status": f0["status"], "answer": f0.get("answer"), "version": current.get("version")}
            if f0["status"] != 200:''', '''            effective = p9["check"].get("returncode") not in (0, None)
            self.check("f: the owner's unfinished edit makes Postfix refuse its configuration (`postfix check` fails)",
                       True if effective else None, p9["check"])
            f0 = self.policy_get("S2f read with the owner's unfinished edit")
            current = f0["_parsed"] if isinstance(f0["_parsed"], dict) else {}
            self.current["read_under_typo"] = {"status": f0["status"], "answer": f0.get("answer"), "version": current.get("version")}
            if not effective:
                self.note("the owner's typo did not make Postfix refuse its configuration on this platform, so no "
                          "reload failure was caused and no save was sent")
            elif f0["status"] != 200:''')
t = sub(t, '''        v1 = body.get("version")

        # (c) a value the server refuses
        bad, _ = set_pg_setting(text1, "work_mem", "eight-megabytes")
        c = self.config_post("S4c set work_mem to a value PostgreSQL refuses", conf, bad, v1, service)''',
        '''        def fresh(label: str, path: str) -> Any:
            return (self.config_get(label, path)["_parsed"] or {}).get("Version")

        v1 = fresh("S4c read", conf)
        self.check("b: the version the save answered is the version of the file now on the server",
                   body.get("version") == v1 == "cf1-" + str(n1["file"].get("sha256")), [body.get("version"), v1])

        # (c) a value the server refuses
        bad, _ = set_pg_setting(text1, "work_mem", "eight-megabytes")
        c = self.config_post("S4c set work_mem to a value PostgreSQL refuses", conf, bad, v1, service)''')
t = sub(t, '''        d = self.config_post("S4d save empty content", conf, "", v1, service)''',
        '''        v1 = fresh("S4d read", conf)
        d = self.config_post("S4d save empty content", conf, "", v1, service)''')
t = sub(t, '''        hv1 = hbody.get("version")
        e2 = self.config_post(''', '''        hv1 = fresh("S4e read pg_hba.conf", hba)
        e2 = self.config_post(''')
t = sub(t, '''        without, removed = remove_local_rules(htext1)
        self.current["hba_local_rules_removed"] = removed
        e3 = self.config_post(''', '''        without, removed = remove_local_rules(htext1)
        self.current["hba_local_rules_removed"] = removed
        hv1 = fresh("S4e read pg_hba.conf again", hba)
        e3 = self.config_post(''')
t = sub(t, '''        owner_text = add_line(n1["file"]["text"], OWNER_NOTE)
        self.owner("f-postgresql.conf-note", "owner-edit", path=conf, content_b64=b64(owner_text))''',
        '''        v1 = fresh("S4f read before the owner's edit", conf)
        owner_text = add_line(n1["file"]["text"], OWNER_NOTE)
        self.owner("f-postgresql.conf-note", "owner-edit", path=conf, content_b64=b64(owner_text))''')
t = sub(t, '''        v1 = body.get("version")

        # (c) refused by mariadbd
        c1 = self.config_post(''', '''        def fresh(label: str) -> Any:
            return (self.config_get(label, path)["_parsed"] or {}).get("Version")

        v1 = fresh("S5c read")
        self.check("b: the version the save answered is the version of the file now on the server",
                   body.get("version") == v1 == "cf1-" + str(n1["file"].get("sha256")), [body.get("version"), v1])

        # (c) refused by mariadbd
        c1 = self.config_post(''')
t = sub(t, '''        bad, _, _ = set_mariadb_option(text1, "max_connections", "plenty")
        c2 = self.config_post(''', '''        bad, _, _ = set_mariadb_option(text1, "max_connections", "plenty")
        v1 = fresh("S5c read again")
        c2 = self.config_post(''')
t = sub(t, '''        d = self.config_post("S5d save empty content", path, "", v1, service)''',
        '''        v1 = fresh("S5d read")
        d = self.config_post("S5d save empty content", path, "", v1, service)''')
t = sub(t, '''        owner_text = add_line(n1["file"]["text"], OWNER_NOTE)
        self.owner("e-option-file-note", "owner-edit", path=path, content_b64=b64(owner_text))''',
        '''        v1 = fresh("S5e read before the owner's edit")
        owner_text = add_line(n1["file"]["text"], OWNER_NOTE)
        self.owner("e-option-file-note", "owner-edit", path=path, content_b64=b64(owner_text))''')
p.write_text(t, encoding="utf-8", newline="\n")

p = ROOT / "test_settings_writes_trial.py"
t = p.read_text(encoding="utf-8")
t = sub(t, '''        self.assertEqual(trial.section_verdict([{"ok": True}], "boom"), "inconclusive")
''', '''        self.assertEqual(trial.section_verdict([{"ok": True}], "boom"), "inconclusive")
        checks = [{"name": "a: one", "ok": True}, {"name": "f (cron-allow): two", "ok": False},
                  {"name": "f: three", "ok": True}, {"name": "queue: x", "ok": None}, {"name": "no prefix", "ok": True}]
        self.assertEqual(trial.substep_verdicts(checks), {"a": "passed", "f": "failed", "queue": "inconclusive", "section": "passed"})
''')
p.write_text(t, encoding="utf-8", newline="\n")
print("patched")
