# set3: request_identity_trial.py after the second round of corrections (S1, P4, P5, O9, O10, O14).
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


r = Patch('request_identity_trial.py')
r.rep('''No update is started. Every result carries ``native_evidence: false``.
''', '''No update is started. Every result carries ``native_evidence: false``.

set3 (2026-10-12), the cells ``rid3-*``: the expectations follow the corrections of 2026-10-11. The import preview and
every stored answer hold no hash-shaped value and an imported mailbox authenticates with its original password (S1);
an archive with the directory member ``homedir/public_html/`` imports completely, a failed files step answers ``200``
``partial`` with truthful lists, hostile members are refused (P4); a PHP site is created, served by PHP-FPM and
deleted (P5); a certbot run that cannot reach the authority answers ``502 CERTIFICATE_ISSUE_FAILED`` (O9); a sent
database password is not echoed and its answer is replayed (O10); the MariaDB version shown is the server's (O14).
''')
r.rep('''    ("C7-import", "cPanel import"),
''', '''    ("C6b-php-site", "a PHP site: created, served by PHP-FPM through the web server, deleted (set3, P5)"),
    ("C7-import", "cPanel import"),
    ("C7b-import-answers", "the imported mailbox's password, a failed files step, hostile archive members (set3, S1 and P4)"),
''')
r.rep('''    "rid-arch": sw.SettingsCell("rid-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
}''', '''    "rid-arch": sw.SettingsCell("rid-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
    # set3: the same guests and profiles, measured after the corrections of 2026-10-11.
    "rid3-debian13": sw.SettingsCell("rid3-debian13", "debian13", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "rid3-ubuntu": sw.SettingsCell("rid3-ubuntu", "ubuntu", "web_mail", sw.MAIL_PRESET + ("postgresql",), True),
    "rid3-arch": sw.SettingsCell("rid3-arch", "arch", "web", sw.WEB_PRESET + ("postgresql",), False),
}
PHP_SITE_DOMAIN = "set3-php.test"
PHP_PROBE_PATH = "/set3-probe.php"
PHP_PROBE_MARK = "set3-php-executed:42:"
HOSTILE_KINDS = ("dotdot", "absolute", "symlink")
MAILBOX_KEYS = ["domain", "has_password", "quota_mb", "user"]     # cmd/panel/import_handlers.go importPreviewMailbox


def version_number(text: Any) -> str | None:
    """The dotted number a version string starts its first number with (10.11.14 of 10.11.14-MariaDB-0ubuntu0...)."""
    found = re.search(r"\\d+\\.\\d+(?:\\.\\d+)?", str(text or ""))
    return found.group(0) if found else None


def partial_lists(steps: list) -> tuple:
    """The `imported` and `not_imported` lists an import answer must carry for its steps (finalize is not a part)."""
    parts = [s for s in steps or [] if isinstance(s, dict) and s.get("step") != "finalize"]
    return [s.get("step") for s in parts if s.get("ok")], [s.get("step") for s in parts if not s.get("ok")]''')
r.rep('''        self.identities: dict[str, str] = {}      # identity -> what it was used for
        self.times: dict[str, dict] = {}''', '''        self.identities: dict[str, str] = {}      # identity -> what it was used for
        self.times: dict[str, dict] = {}
        self.answer_shapes: list[dict] = []       # set3: per guarded answer, hash-shaped values and echoed sent secrets
        self.import_password: str | None = None   # set3: the imported mailboxes' original password (never recorded)''')
r.rep('''        digest = hashlib.sha256(value.body).hexdigest()
        self.raw_digests[entry["n"]] = digest''', '''        digest = hashlib.sha256(value.body).hexdigest()
        self.raw_digests[entry["n"]] = digest
        # set3: counted on the raw bytes before anything is redacted; only the counts are kept.
        secret_key = self.p["redaction"].secret_key
        sent = [inner for key, inner in body.items() if isinstance(inner, str) and inner and secret_key(key)] \\
            if isinstance(body, dict) else []
        entry["hash_shaped_values_in_the_raw_answer"] = base.hash_shaped_count(value.body)
        entry["sent_secret_values_in_the_raw_answer"] = sum(1 for item in sent if item.encode() in value.body)
        self.answer_shapes.append({"n": entry["n"], "label": label, "path": path,
                                   "hash_shaped": entry["hash_shaped_values_in_the_raw_answer"],
                                   "sent_secret_echoed": entry["sent_secret_values_in_the_raw_answer"]})''')
# C0: O14
r.rep('''        self.check("the Databases page lists a MariaDB and a PostgreSQL server", {"mariadb", "postgresql"} <= set(by_type), by_type)
''', '''        self.check("the Databases page lists a MariaDB and a PostgreSQL server", {"mariadb", "postgresql"} <= set(by_type), by_type)
        self.engine_versions("C0, as the engines were registered", listed)
''')
r.rep('''    # -- C1, C3: databases ------------------------------------------------------------------------------
''', '''    def engine_versions(self, when: str, listed: list | None = None) -> None:
        """set3 (O14): the version the Databases page shows for each engine beside what the engine itself reports."""
        if listed is None:
            servers = self.call(f"database servers (the Databases page): {when}", "GET", "/api/v1/database-servers")
            listed = servers["_parsed"] if isinstance(servers["_parsed"], list) else []
        tag = re.sub(r"[^a-z0-9]+", "-", when.lower()).strip("-")[:40]
        native = self.snap2("engine-versions-" + tag, "read-engine-versions")
        shown = {str(s.get("type_name")).lower(): s.get("version") for s in listed}
        maria, postgres = native["mariadb"], native["postgresql"]
        record = {"when": when, "shown": shown, "mariadb_select_version": maria["select_version"],
                  "mariadbd_version_line": maria["server_program_version"], "mariadb_client_version_line": maria["client_program_version"],
                  "postgresql_show_server_version": postgres["show_server_version"], "os": native.get("os_release"),
                  "mariadb_shown_is_exactly_select_version": shown.get("mariadb") == maria["select_version"]}
        self.current.setdefault("engine_versions", []).append(record)
        self.check(f"O14 ({when}): the MariaDB version the Databases page shows is the server's own",
                   bool(version_number(maria["select_version"])) and "VERSION" not in str(shown.get("mariadb"))
                   and version_number(shown.get("mariadb")) == version_number(maria["select_version"]), record)
        self.check(f"O14 ({when}): the PostgreSQL version shown is the server's own",
                   bool(version_number(postgres["show_server_version"]))
                   and version_number(shown.get("postgresql")) == version_number(postgres["show_server_version"]), record)

    # -- C1, C3: databases ------------------------------------------------------------------------------
''')
# C3: O10
r.rep('''        servers = self.state.get("database_servers") or {}
        for engine in ("mariadb", "postgresql"):
            route = "server-database-" + engine''', '''        servers = self.state.get("database_servers") or {}
        # O14: C2 provisioned the Panel's account on each engine again; the engine is asked for its version then.
        self.engine_versions("C3, after the Panel's account was provisioned again")
        for engine in ("mariadb", "postgresql"):
            route = "server-database-" + engine''')
r.rep('''            if engine == "postgresql":
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
                           and answer_code(again) == "REQUEST_COMPLETED_RESULT_NOT_RETAINED", self.current["sent_password"])''',
      '''            # set3 (O10): a new user with a password the owner sent. The answer does not send it back, so it is kept
            # and the replay is that answer byte for byte.
            sent = {"database_name": "srvsent", "new_username": "srvsentu", "new_password": self.password()}
            identity = self._own_identity(route + " (a password the owner sent)")
            before = self.database_effect(engine, lambda r, s=server_id: r.get("server_id") == s)(route + "-sent-before")
            first = self.post(f"{route}: a new user with a password the owner sent", path, sent, identity, 900)
            again = self.post(f"{route}: the same request again", path, sent, identity, 900)
            after = self.database_effect(engine, lambda r, s=server_id: r.get("server_id") == s)(route + "-sent-after")
            parsed = first.get("_parsed") if isinstance(first.get("_parsed"), dict) else {}
            record = {"first": brief(first), "answer_keys": sorted(parsed), "answer_carries_a_password_field": "password" in parsed,
                      "password_set": parsed.get("password_set"),
                      "sent_value_occurs_in_the_raw_answer": first.get("sent_secret_values_in_the_raw_answer"),
                      "replay": brief(again), "replay_sent_value_occurs": again.get("sent_secret_values_in_the_raw_answer"),
                      "same_bytes": self.raw_digests.get(first["n"]) is not None
                      and self.raw_digests.get(first["n"]) == self.raw_digests.get(again.get("n")),
                      "new_on_engine": one_new(before["fp"]["on_engine"], after["fp"]["on_engine"])}
            self.current.setdefault("sent_password", {})[engine] = record
            self.check(f"O10 ({engine}): a database created with a password the owner sent: the answer does not echo it "
                       "(no `password` field, `password_set: true`, the value is not in the answer's bytes)",
                       first.get("status") == 200 and not record["answer_carries_a_password_field"]
                       and parsed.get("password_set") is True and record["sent_value_occurs_in_the_raw_answer"] == 0, record)
            self.check(f"O10 ({engine}): its replay is the first answer byte for byte, marked as a replay, and one database was created",
                       again.get("status") == 200 and again.get("replayed") == "1" and record["same_bytes"]
                       and record["new_on_engine"] == ["srvsent"], record)''')
# C5: O9
r.rep('''        self.check("no certificate was recorded for the domain", done["effects"]["ii"]["fp"]["certificates"] == 0,
                   done["effects"]["ii"]["fp"])
''', '''        self.check("no certificate was recorded for the domain", done["effects"]["ii"]["fp"]["certificates"] == 0,
                   done["effects"]["ii"]["fp"])
        # set3 (O9): the authority is unreachable from this guest (its names resolve to loopback): a typed answer.
        parsed = first.get("_parsed") if isinstance(first.get("_parsed"), dict) else {}
        self.current["certificate_failure"] = {"status": first.get("status"), "code": parsed.get("code"), "reason": parsed.get("reason"),
                                               "error": parsed.get("error"), "vars": parsed.get("vars"),
                                               "texts": first.get("catalogue_texts"), "one_entry_answer": brief(measured)}
        self.check("O9: with the certificate authority unreachable the answer is 502 CERTIFICATE_ISSUE_FAILED, reason "
                   "authority_unreachable", first.get("status") == 502 and parsed.get("code") == "CERTIFICATE_ISSUE_FAILED"
                   and parsed.get("reason") == "authority_unreachable", self.current["certificate_failure"])
''')
# imported(): more native facts
r.rep('''                       "database_row": [d.get("name") for d in native["databases_v2"]], "rows": table.get("rows")},
                "docroot": docroot, "site_account": native.get("site_account")}''',
      '''                       "database_row": [d.get("name") for d in native["databases_v2"]], "rows": table.get("rows")},
                "docroot": docroot, "site_account": native.get("site_account"), "escapes": native.get("escapes"),
                "site_home_entries": native.get("site_home_entries"), "file_list": docroot.get("file_list"),
                "non_regular": docroot.get("non_regular")}''')
r.rep('''        detail = {"native": fp, "expected_files": expected_files, "expected_rows": str(IMPORT_ROWS),
                  "answer_status": (answer or {}).get("status"), "import_status": parsed.get("status"),
                  "steps": [{k: s.get(k) for k in ("step", "ok", "code")} for s in steps]}
        return (fp["domain_row"] and fp["status"] == "active" and fp["files"] == expected_files and mail_ok''',
      '''        # set3 (P4): the document root holds exactly the archive's site files (names, sizes, digests).
        same_files = state.get("file_list") == fixture.get("docroot_expected") and not state.get("non_regular")
        answered = answer is None or ((answer or {}).get("status") == 200 and parsed.get("status") == "active")
        detail = {"native": fp, "expected_files": expected_files, "expected_rows": str(IMPORT_ROWS),
                  "answer_status": (answer or {}).get("status"), "import_status": parsed.get("status"),
                  "docroot_files_equal_the_archives": same_files, "answered_200_active": answered,
                  "steps": [{k: s.get(k) for k in ("step", "ok", "code")} for s in steps]}
        return (same_files and answered and fp["domain_row"] and fp["status"] == "active" and fp["files"] == expected_files and mail_ok''')
# C7: fixtures
r.rep('''        fixtures = {}
        for key, megabytes, sleep in (("i", 0, 0), ("ii", 0, 0), ("v", 16, IMPORT_DROP_SLEEP), ("tar", 0, 0)):
            tag = {"i": "seq", "ii": "con", "v": "drop", "tar": "tar"}[key] + IMPORT_SUFFIX
            made = self.owner2("cpmove-fixture-" + tag, "owner-cpmove-fixture", domain=f"set2-import-{tag}.test",
                               user="s2imp" + tag, database="s2imp" + tag + "_app", megabytes=megabytes, sleep_seconds=sleep,
                               rows=IMPORT_ROWS, public_html_directory_member=key == "tar")
            fixtures[key] = {k: made.get(k) for k in ("path", "bytes", "sha256", "domain", "user", "database", "members",
                                                      "public_html_directory_member")}
            fixtures[key].update(megabytes=megabytes, sleep_seconds=sleep)
        self.current["fixtures"] = fixtures''', '''        fixtures = {}
        # set3 (S1): every archive's mailbox carries the crypt hash of one password the lab knows. The password is
        # registered with the redactor and never written; its hash is made on the guest and never leaves it.
        self.import_password = self.password()
        self.redactor.register(base64.b64encode(self.import_password.encode()).decode())
        for key, megabytes, sleep in (("i", 0, 0), ("ii", 0, 0), ("v", 16, IMPORT_DROP_SLEEP), ("tar", 0, 0)):
            tag = {"i": "seq", "ii": "con", "v": "drop", "tar": "tar"}[key] + IMPORT_SUFFIX
            fixtures[key] = self.import_fixture(tag, f"set2-import-{tag}.test", "s2imp" + tag, megabytes, sleep)
        self.current["fixtures"] = fixtures
        self.state["import_fixtures"] = fixtures''')
r.rep('''                  "database dump; the dropped import's dump ends with DO SLEEP so that the import lasts long enough to drop. "
                  "The archives of the arrivals i, ii and v leave out the one directory member `homedir/public_html/` "
                  "(H34); the archive `tar` holds it")

        def body(fixture: dict, **changes: Any) -> dict:
            value = {"path": fixture["path"], "subscription_id": subscription_id, "domain": fixture["domain"], "do_files": True,
                     "do_mail": self.settings.mail, "do_databases": True, "do_dns": False}
            value.update(changes)
            return value
''', 'UNUSED', 0)
r.rep('''                  "database dump; the dropped import's dump ends with DO SLEEP so that the import lasts long enough to drop. "
                  "The archives of the arrivals i, ii and v leave out the one directory member `homedir/public_html/` "
                  "(H34); the archive `tar` holds it")
''', '''                  "database dump; the dropped import's dump ends with DO SLEEP so that the import lasts long enough to drop. "
                  "set3: every archive holds the directory member `homedir/public_html/` as tar writes it (the import "
                  "now takes it, P4), and the mailbox's shadow entry is the sha512-crypt hash of a password the lab knows")
        body = self.import_body
''')
r.rep('''        def body(fixture: dict, **changes: Any) -> dict:
            value = {"path": fixture["path"], "subscription_id": subscription_id, "domain": fixture["domain"], "do_files": True,
                     "do_mail": self.settings.mail, "do_dns": False, "do_databases": True}
            value.update(changes)
            return value

        for key in ("i", "ii", "v", "tar"):
            preview = self.call(f"C7 inspect the archive of {fixtures[key]['domain']} (ImportPage preview)", "POST",
                                "/api/v1/import/cpanel/inspect", {"path": fixtures[key]["path"]}, timeout=300)
            seen = preview["_parsed"] if isinstance(preview["_parsed"], dict) else {}
            self.check(f"the preview of the {key} archive names its domain, its mailbox and its database",
                       preview["status"] == 200 and fixtures[key]["domain"] in (seen.get("domains") or seen.get("Domains") or [])
                       and bool(seen.get("databases") or seen.get("Databases")), preview.get("answer") or sorted(seen))
''', '''        for key in ("i", "ii", "v", "tar"):
            self.preview(key, fixtures[key])
''')
r.rep('''        self.check("an archive that holds the directory member `homedir/public_html/` as tar writes it has its site files imported",
                   files_step.get("ok") is True and state["fp"]["files"] == 2, self.current["archive_as_tar_writes_it"])''',
      '''        self.current["archive_as_tar_writes_it"].update(
            docroot_files=state.get("file_list"), archive_files=fixtures["tar"]["docroot_expected"],
            imported=parsed.get("imported"), not_imported=parsed.get("not_imported"))
        self.check("P4: an archive that holds the directory member `homedir/public_html/` as tar writes it imports completely "
                   "(200, active; the document root's files equal the archive's)",
                   whole.get("status") == 200 and parsed.get("status") == "active" and files_step.get("ok") is True
                   and state.get("file_list") == fixtures["tar"]["docroot_expected"] and not state.get("non_regular")
                   and not parsed.get("not_imported"), self.current["archive_as_tar_writes_it"])''')
# new methods before C8
r.rep('''    # -- C8: restore ----------------------------------------------------------------------------------------------
''', '''    # -- set3: import helpers, C6b and C7b ------------------------------------------------------------------------

    def import_fixture(self, tag: str, domain: str, user: str, megabytes: int = 0, sleep: int = 0, hostile: str | None = None) -> dict:
        made = self.owner2("cpmove-fixture-" + tag, "owner-cpmove-fixture", domain=domain, user=user, database=user + "_app",
                           megabytes=megabytes, sleep_seconds=sleep, rows=IMPORT_ROWS, public_html_directory_member=True,
                           hostile=hostile, password_b64=base64.b64encode((self.import_password or "").encode()).decode()
                           if self.import_password else None)
        fixture = {k: made.get(k) for k in ("path", "bytes", "sha256", "domain", "user", "database", "members",
                                            "public_html_directory_member", "hostile", "hostile_members", "docroot_expected",
                                            "mailbox_shadow")}
        fixture.update(megabytes=megabytes, sleep_seconds=sleep)
        return fixture

    def import_body(self, fixture: dict, **changes: Any) -> dict:
        value = {"path": fixture["path"], "subscription_id": self.state.get("subscription_id"), "domain": fixture["domain"],
                 "do_files": True, "do_mail": self.settings.mail, "do_dns": False, "do_databases": True}
        value.update(changes)
        return value

    def preview(self, key: str, fixture: dict) -> dict:
        """The import page's preview of one archive. set3 (S1): the answer's raw bytes are searched for hash-shaped
        values before anything is recorded, and only each mailbox's four named fields are kept."""
        label = f"inspect the archive of {fixture['domain']} (ImportPage preview)"
        response = self.api("POST", "/api/v1/import/cpanel/inspect", {"path": fixture["path"]}, purpose=label, timeout=300)
        parsed = response.json()
        seen = parsed if isinstance(parsed, dict) else {}
        boxes = [box for box in seen.get("mail_accounts") or [] if isinstance(box, dict)]
        record = {"label": label, "archive": key, "at": base.utc_now(), "status": response.status, "keys": sorted(seen),
                  "domains": seen.get("domains"), "public_html": seen.get("public_html"),
                  "databases": [d.get("name") if isinstance(d, dict) else d for d in seen.get("databases") or []],
                  "mail_accounts": [{k: box.get(k) for k in MAILBOX_KEYS} for box in boxes],
                  "mailbox_keys": sorted({k for box in boxes for k in box}),
                  "hash_shaped_values_in_the_raw_answer": base.hash_shaped_count(response.text),
                  "raw_bytes": len(response.text.encode())}
        if response.status != 200:
            record["answer"] = {k: seen.get(k) for k in ("code", "error", "reason")}
        self.current.setdefault("previews", []).append(record)
        self.check(f"the preview of the {key} archive names its domain, its mailbox and its database",
                   response.status == 200 and fixture["domain"] in (seen.get("domains") or []) and bool(record["databases"])
                   and bool(boxes), record)
        self.check(f"S1: the preview of the {key} archive holds no hash-shaped value; a mailbox is its address, its quota and "
                   "`has_password` only", response.status == 200 and record["hash_shaped_values_in_the_raw_answer"] == 0
                   and record["mailbox_keys"] == MAILBOX_KEYS and all(box.get("has_password") is True for box in boxes), record)
        return record

    def c6b_php_site(self) -> None:
        """set3 (P5): a PHP site from the Domains page, one PHP page asked for through the web server, the site
        deleted again. The native PHP-FPM facts of the platform are recorded beside it."""
        domain = PHP_SITE_DOMAIN
        self.current["php_before"] = self.snap2("php-before", "read-php")
        created = self.call("C6b create a PHP site (AddDomainModal)", "POST", "/api/v1/domains/create",
                            {"domain": domain, "project_type": "php", "ssl_type": "none"}, timeout=600)
        answer = created["_parsed"] if isinstance(created["_parsed"], dict) else {}
        domain_id = answer.get("DomainID") or answer.get("domain_id")
        self.check("P5: a PHP site is created from the Domains page (200 with the new domain's id)",
                   created["status"] == 200 and isinstance(domain_id, int),
                   created.get("answer") or {k: answer.get(k) for k in sorted(answer) if "id" in k.lower() or "root" in k.lower()})
        if not isinstance(domain_id, int):
            return
        probe = self.owner2("php-probe", "owner-php-probe", domain_id=domain_id)
        page = self.snap2("php-page", "read-http", domain=domain, path=PHP_PROBE_PATH)
        text = str(page.get("body") or "")
        parts = text.strip().split(":")
        executed = page.get("status") == 200 and text.startswith(PHP_PROBE_MARK) and "<?php" not in text and len(parts) >= 5
        served = {"status": page.get("status"), "headers": page.get("headers"), "body": text[:200], "error": page.get("error"),
                  "php_version": parts[2] if executed else None, "server_api": parts[3] if executed else None,
                  "runs_as": parts[4] if executed else None, "site_account": probe.get("site_account")}
        self.current["php_page"] = served
        self.check("P5: the PHP page asked for through the web server was executed by PHP-FPM (the marker and 6 * 7 = 42, "
                   "no source text, server API fpm-fcgi)", executed and served["server_api"] == "fpm-fcgi", served)
        self.check("P5: the page runs as the site's own account", executed and served["runs_as"] == probe.get("site_account"), served)
        facts = self.snap2("php-with-site", "read-php")
        pool = next((p for p in facts["pools"] if p["settings"].get("user") == probe.get("site_account")), None)
        socket_path = ((pool or {}).get("settings") or {}).get("listen")
        native = {"units": {name: {k: unit.get(k) for k in ("LoadState", "ActiveState", "SubState", "FragmentPath", "ExecStart",
                                                            "ProtectHome", "ProtectSystem", "PrivateTmp", "ReadWritePaths")}
                            for name, unit in facts["units"].items()},
                  "pool_directories": facts["pool_directories"], "site_pool": pool,
                  "socket": facts["sockets"].get(socket_path) if socket_path else None,
                  "run_directories": facts["run_directories"], "program": facts["program"],
                  "program_version": facts["program_version"], "web_server_accounts": facts["web_server_accounts"]}
        self.current["php_native"] = native
        self.check("P5: the site's pool file exists under the platform's pool directory and its socket is a socket owned by "
                   "an account the web server runs as", bool(pool) and bool(native["socket"])
                   and native["socket"].get("kind") == "socket"
                   and native["socket"].get("owner") in (facts["web_server_accounts"] + [probe.get("site_account")]), native)
        deleted = self.call("C6b delete the PHP site (Domains page)", "DELETE", f"/api/v1/domains/{domain_id}", timeout=600)
        gone, started, listed_now = False, time.time(), None
        while time.time() - started < 240:
            listing = self.api("GET", "/api/v1/domains", purpose="Domains list after the delete")
            rows = listing.json() if listing.status == 200 else None
            listed_now = [d.get("domain_name") for d in rows] if isinstance(rows, list) else None
            if isinstance(listed_now, list) and domain not in listed_now:
                gone = True
                break
            time.sleep(3)
        left = self.snap2("php-after-delete", "read-imported", domain=domain)
        facts_after = self.snap2("php-facts-after-delete", "read-php")
        page_after = self.snap2("php-page-after-delete", "read-http", domain=domain, path=PHP_PROBE_PATH)
        after = {"delete_status": deleted["status"], "delete_answer": deleted.get("answer") or deleted["_parsed"],
                 "listed_after_seconds": round(time.time() - started, 1), "no_longer_listed": gone,
                 "domain_rows": left.get("rows"), "site_account": left.get("site_account"),
                 "pool_file_left": any(p["settings"].get("user") == probe.get("site_account") for p in facts_after["pools"]),
                 "socket_left": bool(socket_path) and socket_path in facts_after["sockets"]
                 and facts_after["sockets"][socket_path].get("exists"),
                 "page_after": {"status": page_after.get("status"), "marker": PHP_PROBE_MARK in str(page_after.get("body") or "")},
                 "php_fpm_units_after": {name: unit.get("ActiveState") for name, unit in facts_after["units"].items()}}
        self.current["php_deleted"] = after
        self.check("P5: the PHP site is deleted: no longer listed, no domain row, no site account, its pool file gone, the "
                   "page no longer served, and PHP-FPM still active",
                   gone and not after["domain_rows"] and after["site_account"] is None and not after["pool_file_left"]
                   and not after["page_after"]["marker"]
                   and all(state == "active" for name, state in after["php_fpm_units_after"].items()
                           if facts["units"].get(name, {}).get("ActiveState") == "active"), after)

    def c7b_import_answers(self) -> None:
        """set3: (S1) the imported mailboxes authenticate with the original password; (P4) an import whose files
        step fails answers 200 `partial` with truthful lists, and hostile members are never extracted."""
        fixtures = self.state.get("import_fixtures") or {}
        if self.settings.mail and self.import_password:
            secret = base64.b64encode(self.import_password.encode()).decode()
            for key in ("tar", "i", "v"):
                fixture = fixtures.get(key)
                if not fixture:
                    continue
                address = "info@" + fixture["domain"]
                login = self.snap2("mail-login-" + key, "read-mail-login", address=address, password_b64=secret)
                good, wrong = login["readings"]["the_original_password"], login["readings"]["a_wrong_password"]
                record = {"address": address, "the_original_password": good, "a_wrong_password": wrong,
                          "dovecot": login.get("dovecot_version"), "doveadm_user": login.get("doveadm_user")}
                self.current.setdefault("mail_logins", []).append(record)
                self.check(f"S1: the imported mailbox {address} authenticates with its original password (an IMAP LOGIN on "
                           "loopback and `doveadm auth test`)", good["imap"].get("logged_in") is True
                           and good["doveadm_auth_test"].get("returncode") == 0, record)
                self.check(f"S1: the same mailbox refuses a wrong password (both readings)",
                           wrong["imap"].get("logged_in") is False and wrong["doveadm_auth_test"].get("returncode") not in (0, None), record)
        else:
            self.note("mail is not supported on this platform: no mailbox was imported, so S1's login is not measured here")

        for kind in HOSTILE_KINDS:
            short = {"dotdot": "dd", "absolute": "ab", "symlink": "sl"}[kind]
            fixture = self.import_fixture("hostile-" + kind, f"set3-hostile-{kind}.test", "s3h" + short, hostile=kind)
            self.preview("hostile-" + kind, fixture)
            whole = kind == "dotdot"     # the one that also imports mail and the database, for the two lists
            request = self.import_body(fixture) if whole else self.import_body(fixture, do_mail=False, do_databases=False)
            identity = self._own_identity(f"import (hostile member: {kind})")
            before = self.imported(f"hostile-{kind}-before", fixture)
            answer = self.post(f"import: an archive with a hostile member ({kind})", "/api/v1/import/cpanel/apply", request, identity, 3600)
            state = self.imported(f"hostile-{kind}-after", fixture)
            parsed = answer.get("_parsed") if isinstance(answer.get("_parsed"), dict) else {}
            steps = parsed.get("steps") or []
            files_step = next((s for s in steps if s.get("step") == "files"), {})
            should_in, should_out = partial_lists(steps)
            archive_files = fixture["docroot_expected"]
            site_files = state.get("file_list") or {}
            from_archive = sorted(name for name, value in archive_files.items() if site_files.get(name) == value)
            record = {"kind": kind, "hostile_members": fixture.get("hostile_members"), "request": {k: request[k] for k in (
                          "do_files", "do_mail", "do_dns", "do_databases")},
                      "status": answer.get("status"), "import_status": parsed.get("status"), "code": parsed.get("code"),
                      "domain_status": parsed.get("domain_status"), "message": parsed.get("message"),
                      "imported": parsed.get("imported"), "not_imported": parsed.get("not_imported"), "steps": steps,
                      "native": state["fp"], "site_home_entries": state.get("site_home_entries"),
                      "docroot_files": sorted(site_files), "archive_files_in_the_docroot": from_archive,
                      "non_regular_in_the_docroot": state.get("non_regular"), "entries_outside": state.get("escapes"),
                      "entries_outside_before": before.get("escapes")}
            self.current.setdefault("hostile", {})[kind] = record
            self.check(f"P4 ({kind}): nothing of the hostile member exists anywhere on the server, the document root holds no "
                       "link and no stage directory was left in the site's home",
                       state.get("escapes") == [] and not state.get("non_regular")
                       and not any(str(name).startswith(".cpmove") for name in state.get("site_home_entries") or []), record)
            refused = files_step.get("ok") is False
            if kind == "absolute" and not refused:
                self.note("the member with an absolute path is not site payload: the files step left it out and imported the "
                          "site's own files; nothing was written at that path", record)
                self.check("P4 (absolute): the member was left out and the rest imported completely (200, active, the "
                           "document root's files equal the archive's)", answer.get("status") == 200
                           and parsed.get("status") == "active" and site_files == archive_files, record)
                continue
            self.check(f"P4 ({kind}): the files step refuses the archive", refused, files_step)
            self.check(f"P4 ({kind}): the import answers 200 with status `partial`, code IMPORT_PARTIAL and the domain left `pending`",
                       answer.get("status") == 200 and parsed.get("status") == "partial" and parsed.get("code") == "IMPORT_PARTIAL"
                       and parsed.get("domain_status") == "pending" and bool(parsed.get("message")), record)
            mail_ok = (not request["do_mail"]) or state["fp"]["mailboxes"] == ["info@" + fixture["domain"]]
            data_ok = (not request["do_databases"]) or (state["fp"]["database_on_engine"] and state["fp"]["rows"] == str(IMPORT_ROWS)
                                                        and state["fp"]["database_row"] == [fixture["database"]])
            self.check(f"P4 ({kind}): `imported` and `not_imported` are the steps' own results and are true on the server "
                       "(the domain row is `pending`; what is listed as imported is there; none of the archive's site files is "
                       "in the document root)",
                       parsed.get("imported") == should_in and parsed.get("not_imported") == should_out
                       and "files" in (parsed.get("not_imported") or []) and "domain" in (parsed.get("imported") or [])
                       and state["fp"]["domain_row"] and state["fp"]["status"] == "pending" and not from_archive
                       and mail_ok and data_ok, record)
            if whole:
                again = self.post("import: the same partial import again (the same identity)", "/api/v1/import/cpanel/apply",
                                  request, identity, 3600)
                later = self.imported(f"hostile-{kind}-after-replay", fixture)
                self.check("P4: the replay of a partial import is the stored answer byte for byte and imports nothing again",
                           again.get("status") == 200 and again.get("replayed") == "1"
                           and self.raw_digests.get(again["n"]) == self.raw_digests.get(answer["n"]) and later["fp"] == state["fp"],
                           {"replay": brief(again), "unchanged": later["fp"] == state["fp"]})

    # -- C8: restore ----------------------------------------------------------------------------------------------
''')
# C9: hash-shaped rows and answers
r.rep('''        self.judge("rows", "vii", "every row lives 24 hours and none is still `running`",''',
      '''        # set3 (S1): no stored row and no answer of a guarded route holds a hash-shaped value; no answer echoed a
        # secret the request sent (O10).
        self.judge("rows", "vii", "no request_identities row holds a hash-shaped value (stored answer and every other column)",
                   table.get("rows_holding_a_hash_shaped_value") == [] and len(rows) > 0,
                   {"rows": len(rows), "rows_holding_one": table.get("rows_holding_a_hash_shaped_value")})
        shaped = [a for a in self.answer_shapes if a["hash_shaped"]]
        echoed = [a for a in self.answer_shapes if a["sent_secret_echoed"]]
        self.current["answers_searched"] = {"answers": len(self.answer_shapes), "holding_a_hash_shaped_value": shaped,
                                            "echoing_a_sent_secret": echoed}
        self.judge("rows", "vii", "no answer of a guarded route held a hash-shaped value, and none echoed a secret its request sent",
                   bool(self.answer_shapes) and not shaped and not echoed, self.current["answers_searched"])
        start = self.state.get("identity_log_since")
        if start:
            log = self.native2("read-journal", units=["celikpanel-panel.service", "celikpanel-agent.service"],
                               since_epoch=start, lines=3000)["journal"].get("stdout", "")
            self.current["journal_searched"] = {"lines": len(log.splitlines()), "hash_shaped_values": base.hash_shaped_count(log)}
            self.judge("rows", "vii", "the Panel's and the Agent's journal lines of this cell hold no hash-shaped value",
                       self.current["journal_searched"]["hash_shaped_values"] == 0 and bool(log), self.current["journal_searched"])
        self.judge("rows", "vii", "every row lives 24 hours and none is still `running`",''')
r.rep('''        self.current["units_before"] = self.snap2("units-before", "read-units")["units"]
''', '''        self.current["units_before"] = self.snap2("units-before", "read-units")["units"]
        self.state["identity_log_since"] = int(self.native2("read-clock")["epoch"])
''')
r.rep('''                     "C7-import": self.c7_import, "C8-restore": self.c8_restore, "C9-identities": self.c9_identities}''',
      '''                     "C7-import": self.c7_import, "C8-restore": self.c8_restore, "C9-identities": self.c9_identities,
                     "C6b-php-site": self.c6b_php_site, "C7b-import-answers": self.c7b_import_answers}''')
r.save()

t = Patch('test_request_identity_trial.py')
t.rep('''        self.assertEqual(sorted(trial.CELLS), ["rid-arch", "rid-debian13", "rid-ubuntu"])''',
      '''        self.assertEqual(sorted(trial.CELLS), ["rid-arch", "rid-debian13", "rid-ubuntu",
                                               "rid3-arch", "rid3-debian13", "rid3-ubuntu"])
        for platform in ("arch", "debian13", "ubuntu"):
            self.assertEqual(dataclasses.replace(trial.CELLS["rid3-" + platform], name="rid-" + platform),
                             trial.CELLS["rid-" + platform])''')
t.rep('''class CellTests(unittest.TestCase):''', '''class Set3Tests(unittest.TestCase):
    def test_the_guest_helper_searches_the_shapes_the_driver_redacts(self):
        self.assertEqual(native.HASH_SHAPED, trial.base.HASH_SHAPED.pattern)
        fabricated = b"$6$" + b"saltsalt" + b"$" + b"A" * 86
        self.assertEqual(len(native.HASH_SHAPED_BYTES.findall(b'{"crypt_hash": "' + fabricated + b'"}')), 1)
        self.assertEqual(len(native.HASH_SHAPED_BYTES.findall(b'{"has_password": true, "code": "IMPORT_PARTIAL"}')), 0)

    def test_the_answers_the_corrections_define(self):
        handlers = (REPO / "cmd" / "panel" / "import_handlers.go").read_text(encoding="utf-8")
        for key in trial.MAILBOX_KEYS:
            self.assertIn('json:"' + key + '"', handlers.split("type importPreviewMailbox struct", 1)[1].split("}", 1)[0])
        for word in ('"partial"', '"IMPORT_PARTIAL"', 'json:"imported"', 'json:"not_imported"', 'json:"domain_status"'):
            self.assertIn(word, handlers)
        self.assertIn('"CERTIFICATE_ISSUE_FAILED"', (REPO / "cmd" / "panel" / "certificate_issue_failure.go").read_text(encoding="utf-8"))
        self.assertIn('"authority_unreachable"', (REPO / "cmd" / "agent" / "certbot_failure.go").read_text(encoding="utf-8"))
        self.assertIn('json:"password_set"', (REPO / "cmd" / "panel" / "database_v2_handlers.go").read_text(encoding="utf-8"))
        self.assertEqual(trial.version_number("10.11.14-MariaDB-0ubuntu0.24.04.1"), "10.11.14")
        self.assertEqual(trial.version_number("15.1"), "15.1")
        self.assertIsNone(trial.version_number("VERSION()"))
        steps = [{"step": "domain", "ok": True}, {"step": "files", "ok": False}, {"step": "mail", "ok": True},
                 {"step": "finalize", "ok": False}]
        self.assertEqual(trial.partial_lists(steps), (["domain", "mail"], ["files"]))

    def test_hostile_members_and_the_known_password(self):
        members = native.cpmove_members("a.test", "u", "d", 0, 0, 3, shadow_hash=b"$6$salt$" + b"h" * 86)
        relative = {name.split("/", 1)[1]: data for name, data in members}
        self.assertEqual(relative["homedir/etc/a.test/shadow"], b"info:$6$salt$" + b"h" * 86 + b":19000::::::\\n")
        self.assertEqual(sorted(native.payload_files(members)), ["assets/site.css", "index.html"])
        self.assertEqual(trial.HOSTILE_KINDS, native.HOSTILE_KINDS)
        for kind in native.HOSTILE_KINDS:
            with tarfile.open(fileobj=io.BytesIO(native.cpmove_archive(members, True, kind)), mode="r:gz") as archive:
                listed = {member.name: member for member in archive.getmembers()}
            hostile = [name for name in listed if native.ESCAPE_PREFIX in name]
            self.assertTrue(hostile, kind)
            if kind == "dotdot":
                self.assertTrue(any("/../../" in name for name in hostile))
            if kind == "absolute":
                self.assertIn("etc/" + native.ESCAPE_PREFIX + "-absolute.txt", [name.lstrip("/") for name in hostile])
            if kind == "symlink":
                self.assertTrue(any(listed[name].issym() and listed[name].linkname == "/etc" for name in hostile))
        self.assertIn("cpmove-u/homedir/public_html", listed)       # every set3 archive holds the directory member
        self.assertEqual(native.PHP_PROBE_MARK + ":42:", trial.PHP_PROBE_MARK)
        self.assertEqual("/" + native.PHP_PROBE, trial.PHP_PROBE_PATH)


class CellTests(unittest.TestCase):''')
t.save()
print('ok')
