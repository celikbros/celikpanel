# set3 part 2: the start-check candidate in the published-baseline build, and the facts an owner meets after the
# update from the published release (ledger, guard, version tokens, deferred mail, agreement) or after the return.
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


b = Patch('build-upd1-artifacts.sh')
b.rep('''    defective=$(commit_fixture defective "test(fixture): upd7 defective candidate - migrate-only fails (unpublished, disposable)")
    build_web "$work/web-dist-candidate"''', '''    defective=$(commit_fixture defective "test(fixture): upd7 defective candidate - migrate-only fails (unpublished, disposable)")
    startcheck=
    if [[ $BASELINE_REF == v0.1.0-alpha.81 ]]; then
        # set3: the start-check candidate as in the default mode: one commit over G changing its one reviewed file.
        git -C "$clone" checkout --quiet --detach "$good"
        startcheck=$(commit_fixture start-check "test(fixture): set3 start-check candidate - shared panel TLS preparation fails (unpublished, disposable)")
        [[ $(git -C "$clone" diff --name-only "$good" "$startcheck") == cmd/panel/server_lifecycle.go ]] \\
            || { echo "start-check fixture changed more than cmd/panel/server_lifecycle.go" >&2; exit 1; }
        git -C "$clone" checkout --quiet --detach "$defective"
    fi
    build_web "$work/web-dist-candidate"''')
b.rep('''    s_json= r_json= startcheck= realstart=''', '''    s_json= r_json= realstart=
    [[ -z $startcheck ]] || s_json=$(build "$startcheck" "$c_version")''')
b.save()

p = Patch('owner_update_trial.py')
p.rep('''GUEST_HELPERS = ("guest_probe.py",''', '''# set3: read-only helpers uploaded by the steps that need them (the facts after an update from the published release).
SCHEMA_LEDGER_HELPER = "guest_schema_ledger.py"
BACKUP_READER_HELPER = "guest_request_identity_native.py"
REQUEST_ID_HEADER = "X-CelikPanel-Request-Id"
RELOAD_SENTENCE = "Reload the page"
LEDGER_AFTER_UPDATE, LEDGER_AFTER_RETURN = 43, 42
GUARDED_TABLE = "request_identities"
GUEST_HELPERS = ("guest_probe.py",''')
p.rep('''def provenance_for(variant: str) -> dict:''', '''def ledger_verdict(reading: dict | None, version: int, pins: dict) -> dict:
    """set3: one reading of guest_schema_ledger.py against the released ledger and schema that populated_database.py
    pins for ``version`` (the same canonical digests). ``request_identities`` exists exactly from 43 on."""
    if not isinstance(reading, dict) or reading.get("schema_version") is None:
        return {"verdict": "inconclusive", "reason": "the ledger could not be read", "expected_version": version}
    guarded = reading.get(GUARDED_TABLE) or {}
    schema_equal = pins["schemas"].get(version) in (reading.get("schema_sha256"), reading.get("schema_sha256_without_statistics"))
    facts = {"version": reading["schema_version"] == version and reading.get("ledger_rows") == version
             and reading.get("ledger_contiguous") is True,
             "ledger_is_the_released_one": reading.get("ledger_sha256") == pins["migrations"].get(version),
             "schema_is_the_released_one": schema_equal,
             "request_identities": bool(guarded.get("exists")) is (version >= LEDGER_AFTER_UPDATE),
             "integrity": reading.get("integrity_check") == ["ok"] and reading.get("foreign_key_check_clean") is True}
    return {"verdict": "as-expected" if all(facts.values()) else "different", "expected_version": version, "facts": facts,
            "schema_version": reading["schema_version"], "ledger_rows": reading.get("ledger_rows"),
            "ledger_sha256": reading.get("ledger_sha256"), "pinned_ledger_sha256": pins["migrations"].get(version),
            "schema_sha256": reading.get("schema_sha256"),
            "schema_sha256_without_statistics": reading.get("schema_sha256_without_statistics"),
            "pinned_schema_sha256": pins["schemas"].get(version), "statistics_tables": reading.get("statistics_tables"),
            "request_identities": guarded, "table_count": reading.get("table_count"), "ledger_last": reading.get("ledger_last")}


def ledger_pins() -> dict:
    module = _load("set3_populated_database", HERE / "populated_database.py")
    return {"migrations": dict(module.MIGRATIONS), "schemas": dict(module.SCHEMAS)}


def site_account_name(domain: str) -> str:
    """internal/services.SiteUsername: '.' and '-' become '_', cut at 32."""
    return domain.replace(".", "_").replace("-", "_")[:32]


def provenance_for(variant: str) -> dict:''')
p.rep('''    def terminal_real_start(self, checks: dict) -> str:''', '''    # -- set3: what an owner meets after the update from the published release ----------------------------------------

    def set3_helper(self, name: str) -> None:
        if name not in self.state.setdefault("set3_helpers", []):
            self.lab.put_file(self.root, self.record, self.plan, self.node_name, HERE / name, name)
            self.state["set3_helpers"].append(name)

    def schema_ledger(self, label: str, version: int) -> dict:
        self.set3_helper(SCHEMA_LEDGER_HELPER)
        reading = self.helper(SCHEMA_LEDGER_HELPER, "read", timeout=120)
        self.record_json(f"ledger-{label}.json", reading)
        return ledger_verdict(reading, version, ledger_pins())

    def status_views(self, label: str) -> dict:
        """One reading of the three owner views of this operation (update status, recovery reader, root CLI) with the
        card and the screen the served build renders."""
        sample = self.status_sample(None, 9000 + len(self.state.setdefault("set3_views", [])))
        self.state["set3_views"].append(label)
        self.record_json(f"views-{label}.json", sample)
        cli_raw = sample.get("cli") or {}
        cli = None
        try:
            cli = json.loads((cli_raw.get("json") or {}).get("stdout") or "null")
        except (ValueError, AttributeError):
            cli = None
        recovery = (sample.get("recovery_api") or {}).get("body")
        return {"update_status": sample.get("update_status"), "recovery_api": sample.get("recovery_api"),
                "cli": {"observation": (cli or {}).get("observation"), "phase": (cli or {}).get("phase"),
                        "terminal_proof": (cli or {}).get("terminal_proof"), "previous_failure": (cli or {}).get("previous_failure"),
                        "text": {lang: (cli_raw.get(lang) or {}).get("stdout") for lang in ("en", "tr")}},
                "api_pair": [recovery.get("phase"), recovery.get("terminal_proof")] if isinstance(recovery, dict) else None,
                "cli_pair": [cli.get("phase"), cli.get("terminal_proof")] if isinstance(cli, dict) else None,
                "agreement": sample.get("agreement"), "update_card": sample.get("update_card"),
                "recovery_screen": sample.get("guidance_api"), "cli_error": sample.get("cli_error"),
                "panel_error": sample.get("panel_error")}

    def bare_post(self, path: str, body: Any, identity: str | None, timeout: float = 900) -> Any:
        """A POST as a page opened before the update sends it: the session's own headers and no request identity
        (the Panel client adds one to every change, as the updated page does); with ``identity`` the header is sent."""
        payload = json.dumps(body, separators=(",", ":")).encode()
        headers = dict(self.panel_client()._headers("POST", payload))
        if identity:
            headers[REQUEST_ID_HEADER] = identity
        return self.transport("POST", path, headers, payload, timeout)

    def versioned_write(self, name: str, read_path: str, method: str, write_path: str, body: dict, record: dict) -> bool:
        """Read the current version as the screen does, send the change with it, read again."""
        before = self.api("GET", read_path, purpose=f"{name}: read before the change")
        seen = before.json() if before.status == 200 else None
        version = seen.get("version") if isinstance(seen, dict) else None
        without = self.api(method, write_path, dict(body), purpose=f"{name}: the change without a version (an old page)", timeout=180)
        refused = without.json()
        sent = self.api(method, write_path, dict(body, version=version), purpose=f"{name}: the change with the version", timeout=180)
        after = self.api("GET", read_path, purpose=f"{name}: read after the change")
        seen_after = after.json() if after.status == 200 else None
        record.update(read_http=before.status, version_before=version,
                      without_a_version={"http": without.status, "code": refused.get("code") if isinstance(refused, dict) else None,
                                         "reason": refused.get("reason") if isinstance(refused, dict) else None,
                                         "error": refused.get("error") if isinstance(refused, dict) else None},
                      with_the_version={"http": sent.status, "body": sent.json()},
                      version_after=seen_after.get("version") if isinstance(seen_after, dict) else None,
                      read_after=seen_after)
        return (before.status == 200 and bool(version) and sent.status == 200
                and record["version_after"] not in (None, version))

    def post_update_facts(self, checks: dict) -> str:
        """set3: after the verified update from the published release. (a) the ledger; (b) a guarded route without
        and with the request identity; (c) the writes that carry a version; (d) the deferred mail work; (e) the root
        CLI and the update card on the same terminal state."""
        failures, unknown = [], []
        seed = self.state["seed"]
        domain_id, domain = seed["domain_id"], seed.get("domain") or ""
        # (a)
        ledger = self.schema_ledger("after-update", LEDGER_AFTER_UPDATE)
        checks["a_ledger"] = ledger
        if ledger["verdict"] != "as-expected":
            (unknown if ledger["verdict"] == "inconclusive" else failures).append(f"(a) the ledger is not the released {LEDGER_AFTER_UPDATE}")
        # (b)
        self.set3_helper(BACKUP_READER_HELPER)

        def native(mode: str, **arguments: Any) -> dict:
            payload = base64.b64encode(json.dumps(arguments).encode()).decode()
            return self.helper(BACKUP_READER_HELPER, mode, "--args-b64", payload, timeout=300)

        def archives() -> list:
            listed = native("read-backups", domain_id=domain_id)
            return sorted(e["name"] for e in listed["entries"] if e["name"].endswith(".cpbak") and not e["name"].startswith("."))
        path = f"/api/v1/domains/{domain_id}/backups"
        guard: dict[str, Any] = {"route": "POST " + path, "body": {"type": "full"}}
        try:
            before, rows_before = archives(), native("read-identities").get("count")
            bare = self.bare_post(path, {"type": "full"}, None, 120)
            refused = bare.json() if isinstance(bare.json(), dict) else {}
            after_bare, rows_bare = archives(), native("read-identities").get("count")
            identity = secrets.token_hex(16)
            sent = self.bare_post(path, {"type": "full"}, identity, 2400)
            answered = sent.json() if isinstance(sent.json(), dict) else {}
            after_sent = archives()
            row = (native("read-identities", ids=[identity]).get("rows") or [None])[0]
            translator = self.candidate_translator()
            key = "err.REQUEST_ID_REQUIRED"
            guard.update(
                without_the_header={"http": bare.status, "code": refused.get("code"), "error": refused.get("error"),
                                    "screen_text": {lang: translator.text(key, {}, language=lang) for lang in ("en", "tr")}
                                    if key in translator.catalog["en"] else None},
                archives=[len(before), len(after_bare), len(after_sent)], identity_rows=[rows_before, rows_bare],
                with_the_header={"http": sent.status, "backup": (answered.get("backup") or {}).get("name"),
                                 "code": answered.get("code"), "error": answered.get("error")},
                new_archives=sorted(set(after_sent) - set(after_bare)),
                row={k: (row or {}).get(k) for k in ("route", "status", "response_status", "response_retained")})
            ok_refusal = (bare.status == 428 and refused.get("code") == "REQUEST_ID_REQUIRED"
                          and RELOAD_SENTENCE in str(refused.get("error")) and after_bare == before and rows_bare == rows_before)
            ok_works = (sent.status == 200 and len(guard["new_archives"]) == 1
                        and guard["with_the_header"]["backup"] == guard["new_archives"][0] and (row or {}).get("status") == "done")
            guard.update(refused_and_nothing_changed=ok_refusal, works_with_the_header=ok_works)
            if not ok_refusal:
                failures.append("(b) a guarded route without the request identity was not refused 428 with nothing changed")
            if not ok_works:
                failures.append("(b) the same route with the request identity did not make exactly one backup")
        except Exception as exc:  # noqa: BLE001 - recorded; the other facts are still measured
            guard["error"] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:400]
            unknown.append("(b) the guard could not be measured")
        checks["b_guard"] = guard
        # (c)
        tokens: dict[str, Any] = {}
        try:
            if (seed.get("cron") or {}).get("seeded"):
                cron: dict[str, Any] = {}
                command = "/usr/bin/true set3-after-update"
                ok = self.versioned_write("cron add", f"/api/v1/domains/{domain_id}/cron", "POST", f"/api/v1/domains/{domain_id}/cron",
                                          {"schedule": "*/30 * * * *", "command": command}, cron)
                try:
                    listed = self.guest("sudo crontab -u " + shlex.quote(site_account_name(domain)) + " -l", timeout=40).stdout
                    cron["native_crontab_lines_with_the_command"] = sum(1 for line in listed.splitlines() if command in line)
                except Exception as exc:  # noqa: BLE001
                    cron["native_crontab_error"] = type(exc).__name__
                cron["ok"] = ok and cron.get("native_crontab_lines_with_the_command") == 1
                tokens["cron"] = cron
            else:
                tokens["cron"] = {"measured": False, "reason": "cron was not seeded on this baseline"}
            if self.cell.mail_required and (seed.get("mail") or {}).get("listed"):
                policy: dict[str, Any] = {}
                current = self.api("GET", "/api/v1/mail/policy", purpose="mail policy: values to keep").json() or {}
                wanted = {"message_size_mb": current.get("message_size_mb"), "dnsbl_zones": current.get("dnsbl_zones") or [],
                          "outbound_rate_limit": 37}
                ok = self.versioned_write("mail policy save", "/api/v1/mail/policy", "PUT", "/api/v1/mail/policy", wanted, policy)
                try:
                    policy["native_postconf"] = self.guest("sudo postconf -h smtpd_client_message_rate_limit", timeout=40).stdout.strip()
                except Exception as exc:  # noqa: BLE001
                    policy["native_postconf_error"] = type(exc).__name__
                policy["ok"] = ok and policy.get("native_postconf") == "37"
                tokens["mail_policy"] = policy
            else:
                tokens["mail_policy"] = {"measured": False, "reason": "mail is not part of this cell's platform"}
            schedule: dict[str, Any] = {}
            url = f"/api/v1/domains/{domain_id}/backups/schedule"
            ok = self.versioned_write("backup schedule", url, "PUT", url, {"frequency": "weekly", "backup_type": "full", "retention": 30}, schedule)
            schedule["ok"] = ok and isinstance(schedule.get("read_after"), dict) and schedule["read_after"].get("enabled") is True
            tokens["backup_schedule"] = schedule
            for name, item in tokens.items():
                if item.get("measured") is False:
                    continue
                if not item.get("ok"):
                    failures.append(f"(c) {name}: the write with the version did not take effect")
                if (item.get("without_a_version") or {}).get("code") != "SETTINGS_VERSION_REQUIRED":
                    self.finding(f"post-update {name}: a write without a version answered "
                                 f"{(item.get('without_a_version') or {}).get('http')} {(item.get('without_a_version') or {}).get('code')}")
        except Exception as exc:  # noqa: BLE001
            tokens["error"] = self.redactor.text(f"{type(exc).__name__}: {exc}")[:400]
            unknown.append("(c) the versioned writes could not be measured")
        checks["c_version_tokens"] = tokens
        # (d)
        if deferred_mail_watched(self.cell):
            deferred = self.state.get("deferred_mail")
            last = (deferred or {}).get("last") or {}
            checks["d_deferred_mail"] = {"measured": True, "last": last, "completed": bool(last.get("finished")),
                                         "source": "the deferred-mail-watch step of this cell"}
            if not last.get("finished"):
                unknown.append("(d) the deferred mail work was not seen completed")
        else:
            checks["d_deferred_mail"] = {"measured": False, "reason": "no mail stack on this platform"}
        # (e)
        views = self.status_views("after-update")
        expected = ["succeeded", "update_verified"]
        card = views.get("update_card") or {}
        checks["e_agreement"] = {"cli_pair": views["cli_pair"], "api_pair": views["api_pair"], "expected": expected,
                                 "agreement": views.get("agreement"), "card_state": card.get("state") or card.get("kind"),
                                 "card": card, "cli_text": views["cli"]["text"], "recovery_screen": views.get("recovery_screen"),
                                 "update_status": views.get("update_status")}
        if views["cli_pair"] != expected or views["api_pair"] != expected:
            failures.append(f"(e) the root CLI ({views['cli_pair']}) and the recovery reader ({views['api_pair']}) are not both {expected}")
        judged = judge_update_card(views.get("update_card"), ("succeeded",))
        checks["e_agreement"]["card_judged"] = judged
        if judged.get("findings"):
            failures.append("(e) the update card does not show the verified update: " + "; ".join(judged["findings"])[:300])
        if failures:
            raise StepFailed("; ".join(failures))
        if unknown:
            raise StepInconclusive("; ".join(unknown))
        return "passed"

    def post_return_facts(self, checks: dict) -> str:
        """set3: after the automatic return to the published release. Its ledger is the released 42 again (the
        pre-update snapshot: no ``request_identities``), and what its update card and the root CLI say is recorded."""
        ledger = self.schema_ledger("after-return", LEDGER_AFTER_RETURN)
        checks["ledger"] = ledger
        views = self.status_views("after-return")
        expected = ["recovered", "rollback_verified"]
        checks["views"] = {"cli_pair": views["cli_pair"], "api_pair": views["api_pair"], "expected": expected,
                           "recovery_api_http": (views.get("recovery_api") or {}).get("http"),
                           "update_status": views.get("update_status"), "agreement": views.get("agreement"),
                           "update_card": views.get("update_card"), "recovery_screen": views.get("recovery_screen"),
                           "cli_text": views["cli"]["text"], "cli_previous_failure": views["cli"].get("previous_failure")}
        check = self.api("GET", "/api/v1/panel/update/check", purpose="update check after the return")
        version = self.api("GET", "/api/v1/panel/version", purpose="version after the return")
        checks["offered_again"] = {"http": check.status, "body": check.json()}
        checks["version"] = {"http": version.status, "body": version.json()}
        database = (self.state.get("terminal") or {}).get("database")
        checks["database_against_the_pre_update_digest"] = database
        failures = []
        if ledger["verdict"] == "inconclusive":
            raise StepInconclusive("the ledger could not be read after the return")
        if ledger["verdict"] != "as-expected":
            failures.append(f"the ledger after the return is not the released {LEDGER_AFTER_RETURN}")
        if views["cli_pair"] != expected:
            failures.append(f"the root CLI does not read {expected}: {views['cli_pair']}")
        if (views.get("recovery_api") or {}).get("http") == 200 and views["api_pair"] != expected:
            failures.append(f"the returned Panel's recovery reader does not read {expected}: {views['api_pair']}")
        if database not in ("equal", "equal-except-volatile"):
            failures.append(f"the database is not the pre-update one: {database}")
        if failures:
            raise StepFailed("; ".join(failures))
        return "passed"

    def terminal_real_start(self, checks: dict) -> str:''')
p.rep('''        if self.cell.variant == "mgmt-off-reboot":
            # upd4: only after the verified good update (terminal passed); management returns even when the''',
      '''        # set3: with a published baseline, the facts an owner meets after the update (good cells) or after the
        # automatic return (rollback cells). Read after the deferred mail watch, so that nothing races it.
        if LABEL_REF is not None and BASELINE_REFS[LABEL_REF].get("unpatched") and self.scenario() is None:
            if self.cell.variant == "good":
                self.step("post-update-facts", self.post_update_facts, needs=("terminal",))
            elif self.cell.variant in ROLLBACK_VARIANTS:
                self.step("post-return-facts", self.post_return_facts, needs=("terminal",))
        if self.cell.variant == "mgmt-off-reboot":
            # upd4: only after the verified good update (terminal passed); management returns even when the''')
p.save()

t = Patch('test_owner_update_trial.py')
t.rep('''        self.assertEqual(t.baseline_ref_patched("v0.1.0-alpha.81"), ())''', '''        self.assertIn('startcheck=$(commit_fixture start-check "test(fixture): set3 start-check candidate', text)
        self.assertIn('[[ -z $startcheck ]] || s_json=$(build "$startcheck" "$c_version")', text)
        self.assertEqual(t.baseline_ref_patched("v0.1.0-alpha.81"), ())''')
t.rep('''class Upd3FixturePatchTests(''', '''class Set3PublishedBaselineTests(unittest.TestCase):
    def test_the_ledger_is_judged_against_the_pinned_released_digests(self):
        pins = t.ledger_pins()
        self.assertEqual(sorted(pins["migrations"]), [38, 42, 43])

        def reading(version, guarded):
            return {"schema_version": version, "ledger_rows": version, "ledger_contiguous": True,
                    "ledger_sha256": pins["migrations"][version], "schema_sha256": "x" * 64,
                    "schema_sha256_without_statistics": pins["schemas"][version], "statistics_tables": ["sqlite_stat1"],
                    "request_identities": {"exists": guarded}, "integrity_check": ["ok"], "foreign_key_check_clean": True}
        self.assertEqual(t.ledger_verdict(reading(43, True), 43, pins)["verdict"], "as-expected")
        self.assertEqual(t.ledger_verdict(reading(42, False), 42, pins)["verdict"], "as-expected")
        for wrong in (reading(43, True), reading(42, True)):
            self.assertEqual(t.ledger_verdict(wrong, 42, pins)["verdict"], "different")
        self.assertEqual(t.ledger_verdict(reading(42, False), 43, pins)["verdict"], "different")
        self.assertFalse(t.ledger_verdict(dict(reading(43, True), ledger_sha256="0" * 64), 43, pins)["facts"]["ledger_is_the_released_one"])
        self.assertEqual(t.ledger_verdict(None, 42, pins)["verdict"], "inconclusive")
        helper = (HERE / t.SCHEMA_LEDGER_HELPER).read_text(encoding="utf-8")
        self.assertIn("mode=ro", helper)
        self.assertIn("query_only=ON", helper)
        self.assertNotIn("INSERT", helper.upper().replace("INSERTS", ""))
        self.assertEqual(t.site_account_name("upd1-owner.test"), "upd1_owner_test")

    def test_the_guard_and_the_published_baseline(self):
        middleware = "".join(path.read_text(encoding="utf-8") for path in (REPO / "cmd" / "panel").glob("request_identity*.go"))
        self.assertIn(t.REQUEST_ID_HEADER, middleware)
        self.assertIn("REQUEST_ID_REQUIRED", middleware)
        self.assertIn(t.RELOAD_SENTENCE, middleware)
        self.assertTrue((REPO / "internal" / "db" / "migrations").glob("043_*"))
        self.assertEqual(len(list((REPO / "internal" / "db" / "migrations").glob("043_*.sql"))), 1)
        profile = t.BASELINE_REFS["v0.1.0-alpha.81"]
        self.assertEqual((profile["baseline"], profile["candidate"]), (("v0.1.0-alpha.81", 81), ("v0.1.0-alpha.82", 82)))
        self.assertTrue(profile["unpatched"])


class Upd3FixturePatchTests(''')
t.save()
print('ok')
