"""set2: changes to settings_writes_trial.py (working tree, harness only): the set1 corrections' expectations
(group A) and the new section S8, service actions (group B)."""
import sys

p = sys.argv[1]
s = open(p, encoding="utf-8", newline="").read()
crlf = "\r\n" in s
s = s.replace("\r\n", "\n")


def rep(old, new, count=1):
    global s
    assert s.count(old) == count, (old[:70], s.count(old))
    s = s.replace(old, new)


def between(start, end, new):
    """Replace the text from ``start`` (inclusive) up to ``end`` (exclusive)."""
    global s
    assert s.count(start) == 1, (start[:70], s.count(start))
    a = s.index(start)
    b = s.index(end, a)
    s = s[:a] + new + s[b:]


# -- docstring, cells, sections ---------------------------------------------------------------------------------
rep('''  S7 owner, group and mode of every file the Panel wrote

No update is started.''',
    '''  S7 owner, group and mode of every file the Panel wrote
  S8 (set2) service actions on the real units: start, stop, restart and reload through the Services page's route

set2 (2026-10-11): the expectations of S1 f, S2 c/f/g/h, S4 g, S5 c and S6 follow the corrections the product made
after the first native run (set1), and the cells ``set2-*`` name that run. The ``set1-*`` names stay valid.

No update is started.''')

rep('''    "set1-arch": SettingsCell("set1-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
}''',
    '''    "set1-arch": SettingsCell("set1-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
    # set2: the same three guests and profiles, measured after the corrections of 2026-10-10.
    "set2-debian13": SettingsCell("set2-debian13", "debian13", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set2-ubuntu": SettingsCell("set2-ubuntu", "ubuntu", "web_mail", MAIL_PRESET + ("postgresql",), True),
    "set2-arch": SettingsCell("set2-arch", "arch", "web", WEB_PRESET + ("postgresql",), False),
}''')

rep('''    ("S7-file-metadata", "owner, group and mode of the files the Panel wrote", False),
)''',
    '''    ("S7-file-metadata", "owner, group and mode of the files the Panel wrote", False),
    ("S8-service-actions", "service actions on the real units (the Services page's route)", False),
)
# What each daemon writes to the journal when it has taken a reload (S8); PostgreSQL is asked for its load time.
RELOAD_MARKERS = {"postfix": ("reload -- version", "refreshing the Postfix mail system"),
                  "dovecot": ("SIGHUP received", "reloading configuration"),
                  "nginx": ("Reloaded nginx", "Reloaded A high performance", "Reloaded nginx.service", "signal process started"),
                  "postgresql": ("received SIGHUP",), "mariadb": ()}
PG_REFUSED_VALUE = "eight-megabytes"''')

# -- S1 f: every cause, each with the cause the answer carries ---------------------------------------------------
between('''        # (f) the crontab cannot be read
''', '''        self.check("f: one of the owner's realistic causes made the native crontab read fail", True if used else None,
''', '''        # (f) the crontab cannot be read. set2: every cause is applied in turn, and each is judged with the cause
        # the answer carries (cron.allow is a verified cause; a relocated spool is not one the Agent names).
        used = []
        for kind in CRON_FAULTS:
            applied = self.owner("f-fault-" + kind, "owner-cron-fault", action="apply", kind=kind)
            try:
                probe = self.crontab("f-crontab-under-" + kind)
                native_kind = crontab_native_kind(probe["list"], user)
                record = {"kind": kind, "applied": applied.get("applied"), "native": native_kind,
                          "returncode": probe["list"].get("returncode"), "stderr": probe["list"].get("stderr")}
                self.current.setdefault("cron_faults", []).append(record)
                listing = self.cron_list(f"S1f list under the owner's {kind}")
                lbody = listing["_parsed"] if isinstance(listing["_parsed"], dict) else {}
                record["list_status"] = listing["status"]
                record["list_code"] = lbody.get("code")
                record["answer_cause"] = lbody.get("detail")
                record["answer_said"] = (lbody.get("vars") or {}).get("detail")
                add = None
                if native_kind == "failed":
                    add = self.call(f"S1f add under the owner's {kind}", "POST", path,
                                    {"schedule": "7 7 * * *", "command": "/usr/bin/true set1-fault-job",
                                     "version": final_version}, **stale)
                    record["add_status"] = add["status"]
                # The native read is taken again after the Panel's calls: a cause the platform's own crontab
                # repairs by itself between two reads (cronie recreates a missing spool directory) is not a
                # stable unreadable state, and the Panel's answers are then recorded, not judged.
                after = self.crontab("f-crontab-under-" + kind + "-again")
                record["native_again"] = crontab_native_kind(after["list"], user)
                record["stderr_again"] = after["list"].get("stderr")
                if native_kind != "failed" or record["native_again"] != "failed":
                    self.note(f"{kind} does not keep root's `crontab -u <user> -l` failing on this platform (native "
                              f"answers: {native_kind}, then {record['native_again']}); the list answered HTTP "
                              f"{listing['status']}", record)
                    continue
                used.append(kind)
                self.refused(f"f ({kind}): the list answers 502 CURRENT_SETTINGS_UNREADABLE (scheduled_tasks), not an "
                             "empty list", listing, 502, "CURRENT_SETTINGS_UNREADABLE", "scheduled_tasks")
                self.check(f"f ({kind}): Add is refused", add["status"] != 200,
                           add.get("answer") or add["status"])
                self.refused(f"f ({kind}): Add answers 502 CURRENT_SETTINGS_UNREADABLE (scheduled_tasks)", add, 502,
                             "CURRENT_SETTINGS_UNREADABLE", "scheduled_tasks")
                said = str(record["answer_said"] or "")
                tool_line = next((l.strip() for l in str(record["stderr"] or "").splitlines() if l.strip()), "")
                shown = {"detail": record["answer_cause"], "vars.detail": said, "crontab_printed": tool_line}
                if kind == "cron-allow":
                    self.check("f (cron-allow): the answer carries the recognised cause (detail = cron_allow) and "
                               "crontab's own line", record["answer_cause"] == "cron_allow" and bool(said), shown)
                else:
                    self.check(f"f ({kind}): the answer names no cause (neutral) and carries the tool's own line",
                               not record["answer_cause"] and bool(said)
                               and (said in tool_line or tool_line in said or said[:40] in str(record["stderr"])), shown)
                self.current.setdefault("cron_texts", {})[kind] = {
                    key: {language: self.translator.text(key, {"detail": said, "user": user}, language=language)
                          for language in ("en", "tr")}
                    for key in ("cron.unknown", "cron.unknown." + str(record["answer_cause"] or "none"), "cron.unknown.said")
                    if key in self.translator.catalog["en"]}
            finally:
                self.owner("f-restore-" + kind, "owner-cron-fault", action="restore")
''')
rep('''        self.check("f: one of the owner's realistic causes made the native crontab read fail", True if used else None,
                   self.current.get("cron_faults"))''',
    '''        self.current["cron_faults_judged"] = used
        self.check("f: one of the owner's realistic causes made the native crontab read fail", True if used else None,
                   self.current.get("cron_faults"))''')

# -- S2 c: applied = reloaded ------------------------------------------------------------------------------------
rep('''        got = split_restrictions(p3["_values"]["smtpd_recipient_restrictions"])
        self.check("c: the owner's restrictions are all there in the same order and only `reject_rbl_client <zone>` was added",''',
    '''        self.check("c: the answer says what Postfix did with it (applied = reloaded)",
                   isinstance(c["_parsed"], dict) and c["_parsed"].get("applied") == "reloaded",
                   (c["_parsed"] or {}).get("applied") if isinstance(c["_parsed"], dict) else None)
        got = split_restrictions(p3["_values"]["smtpd_recipient_restrictions"])
        self.check("c: the owner's restrictions are all there in the same order and only `reject_rbl_client <zone>` was added",''')
rep('''        s1 = self.smtp("c-smtp-after-save")''',
    '''        master_lines = [l for l in log.splitlines() if "postfix/master" in l and "reload" in l]
        self.check("c: the journal has a `postfix/master ... reload` line from the same master process",
                   bool(master_lines) and f"postfix/master[{p3.get('master_pid')}]" in master_lines[-1], master_lines[-3:])
        s1 = self.smtp("c-smtp-after-save")''')

# -- S2 f, g, h --------------------------------------------------------------------------------------------------
between('''        # (f) the reload fails
''', '''    # -- S3 ----''', '''        # (f) Postfix's own check refuses the configuration (the owner's unfinished edit)
        p8 = self.postfix("f-postfix-before-typo")
        typo_text = add_line(p8["main_cf"]["text"], MAIN_CF_TYPO)
        fixed = False
        try:
            self.owner_main_cf("f-owner-typo", typo_text, reload=False)
            p9 = self.postfix("f-postfix-with-typo")
            effective = p9["check"].get("returncode") not in (0, None)
            self.check("f: the owner's unfinished edit makes Postfix refuse its configuration (`postfix check` fails)",
                       True if effective else None, p9["check"])
            f0 = self.policy_get("S2f read with the owner's unfinished edit")
            current = f0["_parsed"] if isinstance(f0["_parsed"], dict) else {}
            self.current["read_under_typo"] = {"status": f0["status"], "answer": f0.get("answer"), "version": current.get("version")}
            if not effective:
                self.note("the owner's typo did not make Postfix refuse its configuration on this platform, so no "
                          "reload failure was caused and no save was sent")
            elif f0["status"] != 200:
                self.note("with the owner's typo in main.cf the policy read itself is refused, so the reload-failure "
                          "answer could not be reached this way", f0.get("answer"))
                self.check("f: MAIL_POLICY_NOT_RELOADED reached", None, f0.get("answer"))
            else:
                t0 = self.guest_clock()
                f1 = self.call("S2f save while Postfix cannot reload", "PUT", url,
                               {"message_size_mb": current.get("message_size_mb"), "dnsbl_zones": current.get("dnsbl_zones") or [],
                                "outbound_rate_limit": 46, "version": current.get("version")}, **refusal)
                p10 = self.postfix("f-postfix-after-save")
                flog = self.journal("f-postfix-journal", [], t0, postfix_log)
                self.keep_text("native-text/postfix-journal-f.txt", flog or "(no lines)")
                answer = f1["_parsed"] if isinstance(f1["_parsed"], dict) else {}
                self.refused("f: the save answers 502 MAIL_POLICY_NOT_RELOADED with the reason `check`", f1, 502,
                             "MAIL_POLICY_NOT_RELOADED", "check")
                self.check("f: the answer says the change was applied (mutation_applied)", answer.get("mutation_applied") is True,
                           f1.get("answer"))
                written = answer.get("policy") if isinstance(answer.get("policy"), dict) else None
                self.check("f: the body carries the written policy (rate 46) with its new version",
                           bool(written) and written.get("outbound_rate_limit") == 46
                           and str(written.get("version", "")).startswith("mp1-") and written.get("version") != current.get("version"),
                           written)
                said = str((answer.get("vars") or {}).get("detail") or "")
                self.check("f: the answer carries Postfix's own line", "bad numerical configuration" in said
                           or (bool(said) and said[:30] in (p9["check"].get("stderr", "") + p9["check"].get("stdout", ""))),
                           {"vars.detail": said, "postfix_check": (p9["check"].get("stderr") or "")[-300:]})
                self.check("f: the value is written in main.cf (46) and Postfix keeps running with its previous process",
                           p10["_values"]["smtpd_client_message_rate_limit"] == "46" and p10.get("master_pid") == p8.get("master_pid")
                           and p10.get("master_pid") is not None and p10["status"].get("returncode") == 0,
                           {"rate": p10["_values"]["smtpd_client_message_rate_limit"], "master_pid": [p8.get("master_pid"), p10.get("master_pid")],
                            "postfix_status": p10["status"], "units": p10.get("units")})
                self.check("f: nothing was reloaded (the journal has no reload line since the save)",
                           not [l for l in flog.splitlines() if "postfix/master" in l and "reload" in l], flog[-600:])
                self.current["not_reloaded_answer_carries_policy"] = "policy" in answer
                f2 = self.policy_get("S2f read after the save")
                shown = f2["_parsed"] if isinstance(f2["_parsed"], dict) else {}
                self.check("f: the saved values are the ones a reload of the page shows, with the version the answer carried",
                           f2["status"] == 200 and shown.get("outbound_rate_limit") == 46
                           and (not written or shown.get("version") == written.get("version")), shown)
        finally:
            # set2: the owner corrects the line and does NOT reload; sub-step g is the page's save without a change.
            now = self.postfix("f-postfix-before-restore")
            self.owner_main_cf("f-owner-removes-typo", remove_line(now["main_cf"]["text"], MAIN_CF_TYPO), reload=False)
            p11 = self.postfix("f-postfix-after-owner-correction")
            fixed = p11["check"].get("returncode") == 0
            self.check("f: after the owner's correction `postfix check` succeeds (Postfix was not reloaded by the owner)",
                       fixed, p11["check"])

        # (g) a save without a change after the owner corrected main.cf: every accepted save ends with the reload
        if fixed:
            g0 = self.policy_get("S2g read after the owner's correction")
            seen_g = g0["_parsed"] if isinstance(g0["_parsed"], dict) else {}
            t0 = self.guest_clock()
            g1 = self.call("S2g save without a change", "PUT", url,
                           {"message_size_mb": seen_g.get("message_size_mb"), "dnsbl_zones": seen_g.get("dnsbl_zones") or [],
                            "outbound_rate_limit": seen_g.get("outbound_rate_limit"), "version": seen_g.get("version")}, **refusal)
            p12 = self.postfix("g-postfix-after-unchanged-save")
            glog = self.journal("g-postfix-journal", [], t0, postfix_log)
            self.keep_text("native-text/postfix-journal-g.txt", glog or "(no lines)")
            gbody = g1["_parsed"] if isinstance(g1["_parsed"], dict) else {}
            self.check("g: a save without a change answers 200 with applied = unchanged_reloaded",
                       g1["status"] == 200 and gbody.get("applied") == "unchanged_reloaded",
                       g1.get("answer") or {k: gbody.get(k) for k in ("success", "applied")})
            self.check("g: main.cf is byte-identical (nothing was written)",
                       p12["main_cf"].get("sha256") == p11["main_cf"].get("sha256"),
                       [p11["main_cf"].get("sha256"), p12["main_cf"].get("sha256")])
            glines = [l for l in glog.splitlines() if "postfix/master" in l and "reload" in l]
            self.check("g: Postfix was reloaded (a `postfix/master ... reload` line; the same master process)",
                       bool(glines) and p12.get("master_pid") == p11.get("master_pid") and p12.get("master_pid") is not None,
                       {"reload_lines": glines[-3:], "master_pid": [p11.get("master_pid"), p12.get("master_pid")]})
        else:
            self.check("g: run", None, "not run: main.cf is not accepted by `postfix check` after the correction")

        # (h) Postfix is stopped: a save leaves it stopped and says so
        self.owner("h-stop-postfix", "owner-systemctl", action="stop", unit="postfix")
        try:
            p13 = self.postfix("h-postfix-stopped")
            stopped = p13["status"].get("returncode") not in (0, None)
            self.check("h: the owner stopped Postfix (`postfix status` says it is not running)", True if stopped else None,
                       {"status": p13["status"], "units": p13.get("units")})
            h0 = self.policy_get("S2h read while Postfix is stopped")
            seen_h = h0["_parsed"] if isinstance(h0["_parsed"], dict) else {}
            if stopped and h0["status"] == 200:
                wanted_h = {"message_size_mb": seen_h.get("message_size_mb"), "dnsbl_zones": seen_h.get("dnsbl_zones") or [],
                            "outbound_rate_limit": 47}
                h1 = self.call("S2h save while Postfix is stopped", "PUT", url, dict(wanted_h, version=seen_h.get("version")), **refusal)
                p14 = self.postfix("h-postfix-after-save")
                hbody = h1["_parsed"] if isinstance(h1["_parsed"], dict) else {}
                self.check("h: the save answers 200 with applied = not_running",
                           h1["status"] == 200 and hbody.get("applied") == "not_running",
                           h1.get("answer") or {k: hbody.get(k) for k in ("success", "applied")})
                self.check("h: the value is written (47) and Postfix stays stopped (not started by the save)",
                           p14["_values"]["smtpd_client_message_rate_limit"] == "47"
                           and p14["status"].get("returncode") not in (0, None),
                           {"rate": p14["_values"]["smtpd_client_message_rate_limit"], "status": p14["status"], "units": p14.get("units")})
                h2v = (self.policy_get("S2h read after the save")["_parsed"] or {}).get("version")
                h2 = self.call("S2h save without a change while Postfix is stopped", "PUT", url, dict(wanted_h, version=h2v), **refusal)
                p15 = self.postfix("h-postfix-after-unchanged-save")
                h2body = h2["_parsed"] if isinstance(h2["_parsed"], dict) else {}
                self.check("h: a save without a change while stopped answers 200 with applied = unchanged, still stopped",
                           h2["status"] == 200 and h2body.get("applied") == "unchanged"
                           and p15["status"].get("returncode") not in (0, None),
                           {"answer": h2.get("answer") or h2body.get("applied"), "status": p15["status"]})
            else:
                self.check("h: the save was sent", None, {"stopped": stopped, "read": h0.get("answer") or h0["status"]})
        finally:
            started = self.owner("h-start-postfix", "owner-systemctl", action="start", unit="postfix")
        restored = self.owner("h-owner-reload-postfix", "owner-systemctl", action="reload", unit="postfix")
        p16 = self.postfix("h-postfix-end")
        self.keep_text("native-text/postconf-n-end.txt", p16["postconf_n"].get("stdout", ""))
        self.check("h: the owner starts Postfix again; `postfix check`, `postfix status` and `systemctl reload postfix` succeed",
                   started["result"].get("returncode") == 0 and p16["check"].get("returncode") == 0
                   and p16["status"].get("returncode") == 0 and restored["result"].get("returncode") == 0,
                   {"start": started["result"], "check": p16["check"], "status": p16["status"], "reload": restored["result"]})
        s2 = self.smtp("h-smtp-end")
        self.check("h: mail is accepted on 25 and 587 answers at the end", s2["25"].get("ok") and s2["587"].get("ok"), s2)

''')

# -- S4 g ----------------------------------------------------------------------------------------------------------
between('''            gbody = g["_parsed"] if isinstance(g["_parsed"], dict) else {}
            self.refused("g: the save answers 502 CONFIG_RELOAD_FAILED", g, 502, "CONFIG_RELOAD_FAILED")
''', '''        finally:
            self.owner("g-reload-hook-removed", "owner-reload-hook", action="restore", unit=unit)
''', '''            gbody = g["_parsed"] if isinstance(g["_parsed"], dict) else {}
            gvars = gbody.get("vars") if isinstance(gbody.get("vars"), dict) else {}
            self.refused("g: the save answers 502 CONFIG_RELOAD_FAILED with the reason restored_unit_reload_failed", g, 502,
                         "CONFIG_RELOAD_FAILED", "restored_unit_reload_failed")
            self.check("g: the previous file is back in place, byte for byte", n5["file"].get("text") == owner_text
                       and n5["file"].get("sha256") == n3["file"].get("sha256"),
                       {"sha256": [n3["file"].get("sha256"), n5["file"].get("sha256")]})
            self.check("g: the file keeps its owner, group and mode", same_metadata(n3["file"], n5["file"])["equal"],
                       same_metadata(n3["file"], n5["file"]))
            named = gvars.get("name")
            kept = next((x for x in n5["backups"] if x.get("path") == named), None)
            self.current["reload_failed"] = {
                "reason": gbody.get("reason"), "vars": gvars, "error": gbody.get("error"),
                "work_mem_after": s3["_work_mem"], "postmaster_pid": [s0.get("postmaster_pid"), s3.get("postmaster_pid")],
                "file_is_previous": n5["file"].get("text") == owner_text,
                "named_copy": named, "named_copy_exists": bool(kept),
                "backups_before": len(n4["backups"]), "backups_after": len(n5["backups"]),
                "candidates_left": n5.get("candidates_left"),
                "postgresql_version": (s3["answers"]["files"].get("stdout", "").strip().split("\\t") + [""] * 4)[3]}
            self.check("g: the answer names no copy", not named, gvars)
            self.check("g: no copy was left next to the file (as many kept copies as before this save; no validation copy)",
                       len(n5["backups"]) == len(n4["backups"]) and n5.get("candidates_left") == [],
                       {"backups": [len(n4["backups"]), len(n5["backups"])], "candidates_left": n5.get("candidates_left")})
            self.check("g: the answer names the unit whose reload fails", str(gvars.get("unit") or "").startswith("postgresql"),
                       gvars)
            self.check("g: `SHOW work_mem` is the previous value (8MB) and the server was not restarted (same postmaster PID)",
                       s3["_work_mem"] == "8MB" and s3.get("postmaster_pid") == s0.get("postmaster_pid"),
                       self.current["reload_failed"])
            # The queries the Agent uses for this answer, run the same way, with their raw output.
            reread = self.owner("g-agent-reread-batch", "owner-pg-reread")
            raw = reread["answer"]
            self.current["agent_queries"] = {"postgresql_version": reread.get("version"), "returncode": raw.get("returncode"),
                                             "stdout": raw.get("stdout"), "stderr": raw.get("stderr"), "batch": reread.get("batch")}
            self.keep_text("native-text/postgresql-agent-queries-g.txt",
                           "-- " + str(reread.get("version")) + "\\n-- batch (cmd/agent/db_config.go dbConfigPostgreSQLRereadVerified):\\n"
                           + str(reread.get("batch")) + "\\n-- raw stdout:\\n" + str(raw.get("stdout")) + "\\n-- stderr:\\n" + str(raw.get("stderr")))
            lines = dict(l.split("=", 1) for l in str(raw.get("stdout") or "").splitlines() if "=" in l)
            self.check("g: asked the same way, PostgreSQL confirms it (this file, signal sent, load time after the signal, "
                       "no error in the files)", lines.get("file") == conf and lines.get("signal") in ("true", "t")
                       and "error" not in lines and float(lines.get("loaded") or 0) >= float(lines.get("before") or 1e18),
                       lines)
''')

# -- S5 c: the adjusted value is refused ---------------------------------------------------------------------------
between('''        cx = self.config_post(f"S5c set max_connections = {MARIADB_ADJUSTED_VALUE}", path, adjusted, fresh("S5c read (adjusted value)"), service)
''', '''        # (d) empty
        v1 = fresh("S5d read")''', '''        cx = self.config_post(f"S5c set max_connections = {MARIADB_ADJUSTED_VALUE}", path, adjusted, fresh("S5c read (adjusted value)"), service)
        own = self.snap("c-mariadbd-own-reading", "read-mariadb-check", path=path, variables=["max_connections"],
                        content_b64=b64(adjusted))
        xbody = cx["_parsed"] if isinstance(cx["_parsed"], dict) else {}
        xvars = xbody.get("vars") if isinstance(xbody.get("vars"), dict) else {}
        after_x = self.file("c-file-after-adjusted-value", path)
        self.current["adjusted_value"] = {
            "value": MARIADB_ADJUSTED_VALUE, "panel_status": cx["status"], "panel_applied": xbody.get("applied"),
            "panel_daemon_check": xbody.get("daemon_check"), "panel_answer": cx.get("answer"),
            "written": after_x["file"].get("text") == adjusted,
            "mariadbd_returncode_on_that_text": own.get("returncode"),
            "mariadbd_resulting_max_connections": (own.get("resulting_variables") or {}).get("max_connections"),
            "mariadbd_stderr_lines": [l for l in own.get("stderr", "").splitlines() if "max" in l.lower()][-4:]}
        self.refused(f"c (adjusted value): `max_connections = {MARIADB_ADJUSTED_VALUE}` is refused 422 CONFIG_INVALID (daemon)",
                     cx, 422, "CONFIG_INVALID", "daemon")
        said = str(xvars.get("detail") or "")
        self.check("c (adjusted value): the answer carries MariaDB's own line (the adjustment) and the option's name",
                   "adjusted" in said and "max_connections" in (said + " " + str(xvars.get("name") or "")),
                   {"vars": xvars, "mariadbd_printed": self.current["adjusted_value"]["mariadbd_stderr_lines"]})
        if cx["status"] == 200:
            back = self.config_post("S5c correct max_connections on the page again", path, text1,
                                    fresh("S5c read (before the correction)"), service)
            n2b = self.file("c-file-after-correction", path)
            self.note("the adjusted value was written; the page saved the earlier value again so that the following "
                      "sub-steps compare against a known file", back.get("answer") or back["status"])
        else:
            self.unchanged("c (adjusted value)", n2b, after_x)
            n2b = after_x

''')

# -- S6: the queue's verified cause --------------------------------------------------------------------------------
rep('''            if q2.get("returncode") not in (0, None):
                self.refused("queue (owner's typo): the Panel answers 502 MAIL_QUEUE_UNREADABLE, not an empty queue", qc, 502,
                             "MAIL_QUEUE_UNREADABLE")''',
    '''            if q2.get("returncode") not in (0, None):
                self.refused("queue (owner's typo): the Panel answers 502 MAIL_QUEUE_UNREADABLE with the verified cause "
                             "postfix_config, not an empty queue", qc, 502, "MAIL_QUEUE_UNREADABLE", "postfix_config")
                qsaid = str(((qc["_parsed"] or {}).get("vars") or {}).get("detail") or "") if isinstance(qc["_parsed"], dict) else ""
                self.check("queue (owner's typo): the answer carries postqueue's own line",
                           bool(qsaid) and ("bad numerical configuration" in qsaid or qsaid[:30] in str(q2.get("stderr"))),
                           {"vars.detail": qsaid, "postqueue_printed": q2.get("stderr")})''')
rep('''            if failed:
                self.refused("queue (Postfix stopped): the native read fails and the Panel answers 502 MAIL_QUEUE_UNREADABLE",
                             qb, 502, "MAIL_QUEUE_UNREADABLE")''',
    '''            if failed:
                self.refused("queue (Postfix stopped): the native read fails and the Panel answers 502 MAIL_QUEUE_UNREADABLE",
                             qb, 502, "MAIL_QUEUE_UNREADABLE")
                self.check("queue (Postfix stopped): no cause is named that was not verified (not postfix_config)",
                           (qb["_parsed"] or {}).get("reason") != "postfix_config", qb.get("answer"))''')

# -- S7: O5 --------------------------------------------------------------------------------------------------------
rep('''        self.current["files"] = table
        if not table:
            self.check("files were recorded before the sections", None)
''',
    '''        self.current["files"] = table
        if not table:
            self.check("files were recorded before the sections", None)
        # set2 (O5): the component scan lists each configuration file once.
        listed = (self.state.get("config_files") or {}).get("listed") or {}
        duplicates = {service: sorted({p for p in paths if paths.count(p) > 1}) for service, paths in listed.items()}
        self.current["config_files_listed"] = listed
        self.check("O5: the component scan lists each configuration file once (PostgreSQL and MariaDB)",
                   bool(listed.get("postgresql")) and bool(listed.get("mariadb")) and not any(duplicates.values()),
                   {"listed": listed, "listed_more_than_once": duplicates})

    # -- S8 (set2): service actions ---------------------------------------------------------------------------------

    def service_units(self) -> dict:
        pg = pg_unit((self.state.get("config_files") or {}).get("pg_conf") or "")
        units = {"nginx": ["nginx.service"], "mariadb": ["mariadb.service"],
                 "postgresql": ["postgresql.service"] + ([pg] if pg != "postgresql.service" else [])}
        if self.settings.mail:
            units["dovecot"] = ["dovecot.service"]
            units["postfix"] = ["postfix.service", "postfix@-.service"]
        return units

    def service_state(self, label: str, service: str, cat: bool = False) -> dict:
        return self.snap(label, "read-service", units=self.service_units()[service], cat=cat,
                         postfix=service == "postfix", postgres=service == "postgresql", mariadb=service == "mariadb")

    def daemon(self, service: str, state: dict) -> dict:
        """The unit that runs the daemon (the last listed unit that is loaded and is not a oneshot), and whether the
        daemon runs as the daemon itself says where it can be asked."""
        units = state["units"]
        real = None
        for name in self.service_units()[service]:
            properties = units[name]["properties"]
            if properties.get("LoadState") == "loaded" and properties.get("Type") != "oneshot":
                real = name
        properties = units[real]["properties"] if real else {}
        pid = int(properties.get("MainPID") or 0) if str(properties.get("MainPID") or "0").isdigit() else 0
        running = properties.get("ActiveState") == "active" and pid > 0
        asked = "systemd: the unit is active with a main process"
        if service == "postfix":
            running = state["postfix"]["status"].get("returncode") == 0
            pid = state["postfix"]["master_pid"] if state["postfix"]["master_alive"] else 0
            asked = "`postfix status` and the master's PID file"
        elif service == "postgresql":
            running = bool(state["postgres"]["answers"])
            pid = state["postgres"]["postmaster_pid"] if state["postgres"]["postmaster_alive"] else 0
            asked = "a query over the local socket and postmaster.pid"
        elif service == "mariadb":
            running = bool(state["mariadb"]["answers"])
            pid = state["mariadb"]["server_pid"] if state["mariadb"]["server_alive"] else 0
            asked = "a query over the local socket and the server's PID file"
        return {"real_unit": real, "pid": pid or 0, "running": bool(running), "asked": asked,
                "exec_reload": properties.get("ExecReload"), "reload_result": properties.get("ReloadResult"),
                "conf_load_time": (state.get("postgres") or {}).get("conf_load_time"),
                "units": {name: {k: units[name]["properties"].get(k) for k in ("LoadState", "ActiveState", "SubState", "MainPID", "Result", "ReloadResult")}
                          for name in units}}

    def service_action(self, service: str, action: str, situation: str = "", expect: tuple | None = None) -> dict:
        tag = re.sub(r"[^a-z0-9]+", "-", f"{service}-{action}-{situation}".lower()).strip("-")[:60]
        before = self.service_state(tag + "-before", service)
        d0 = self.daemon(service, before)
        t0 = self.guest_clock()
        label = f"S8 {service} {action}" + (f" ({situation})" if situation else "")
        answer = self.call(label, "POST", "/api/v1/service/action", {"name": service, "action": action}, timeout=240,
                           kind="service")
        after = self.service_state(tag + "-after", service)
        d1 = self.daemon(service, after)
        units = [u for u in self.service_units()[service] if before["units"][u]["properties"].get("LoadState") == "loaded"]
        log = self.journal(tag + "-journal", units, t0)
        if service == "postfix":
            log += self.journal(tag + "-journal-postfix", [], t0, ("postfix/master", "postfix/postfix-script"))
        body = answer["_parsed"] if isinstance(answer["_parsed"], dict) else {}
        reloaded_by = [m for m in RELOAD_MARKERS.get(service, ()) if m in log]
        if d0["exec_reload"] != d1["exec_reload"] and d1["exec_reload"]:
            reloaded_by.append("the unit's ExecReload ran")
        if service == "postgresql" and d0["conf_load_time"] and d1["conf_load_time"] and d0["conf_load_time"] != d1["conf_load_time"]:
            reloaded_by.append("pg_conf_load_time() moved")
        unit_reload_ok = d1["reload_result"] in (None, "", "success")
        truth = {"start": d1["running"], "stop": not d1["running"],
                 "restart": d1["running"] and (not d0["running"] or d1["pid"] != d0["pid"]),
                 "reload": d1["running"] and d0["running"] and d1["pid"] == d0["pid"] and bool(reloaded_by) and unit_reload_ok}[action]
        code = body.get("code")
        said = "success" if answer["status"] == 200 and body.get("success") else \\
            "unknown" if code == "SERVICE_ACTION_UNKNOWN" else "failed"
        matches = None if said == "unknown" else (said == "success") == bool(truth)
        record = {"service": service, "action": action, "situation": situation, "at": answer["at"],
                  "status": answer["status"], "code": code, "reason": body.get("reason"), "vars": body.get("vars"),
                  "answer": {k: body.get(k) for k in ("success", "outcome", "applied", "unit", "error") if body.get(k) is not None},
                  "said": said, "truth_action_took_effect": bool(truth), "matches": matches,
                  "daemon_before": d0, "daemon_after": d1, "reload_evidence": reloaded_by,
                  "journal_lines": [l for l in log.splitlines() if l.strip()][-12:]}
        self.current.setdefault("actions", []).append(record)
        self.keep_text(f"journal/{tag}.txt", log or "(no lines)")
        self.check(f"{label}: the answer ({said}{', ' + str(code) + '/' + str(body.get('reason')) if code else ''}) "
                   "matches what the service shows", matches,
                   {k: record[k] for k in ("status", "code", "reason", "vars", "answer", "truth_action_took_effect", "reload_evidence")}
                   | {"before": {k: d0[k] for k in ("running", "pid", "units")}, "after": {k: d1[k] for k in ("running", "pid", "units")}})
        if expect is not None:
            self.refused(f"{label}: answers {expect[0]} {expect[1]}" + (f" ({expect[2]})" if len(expect) > 2 else ""), answer,
                         expect[0], expect[1], expect[2] if len(expect) > 2 else None)
        return record

    def s8_service_actions(self) -> None:
        units = self.service_units()
        order = [name for name in ("nginx", "mariadb", "dovecot", "postfix", "postgresql") if name in units]
        texts = {}
        for service in order:
            state = self.service_state(service + "-unit-text", service, cat=True)
            texts[service] = {name: {"cat": value.get("cat"), "show": value.get("properties"), "is_active": value.get("is_active")}
                              for name, value in state["units"].items()}
            self.keep_text(f"native-text/units-{service}.txt", "\\n".join(
                f"##### systemctl cat {name}\\n{value.get('cat') or '(not loaded)'}\\n##### systemctl show {name}\\n"
                + json.dumps(value.get("properties"), indent=1, sort_keys=True) for name, value in state["units"].items()))
        self.current["unit_facts"] = {service: {name: {k: value["show"].get(k) for k in (
            "LoadState", "Type", "RemainAfterExit", "ExecStart", "ExecReload", "ReloadResult", "ConsistsOf", "PartOf",
            "PropagatesReloadTo", "ReloadPropagatedFrom", "Wants", "FragmentPath", "DropInPaths")}
            for name, value in items.items()} for service, items in texts.items()}

        # healthy: every action on every service, in the order an owner would press them
        for service in order:
            for action, situation in (("start", "already running"), ("reload", ""), ("restart", ""), ("stop", ""),
                                      ("reload", "while stopped"), ("start", "")):
                self.service_action(service, action, situation)
            end = self.daemon(service, self.service_state(service + "-healthy-end", service))
            self.check(f"{service}: runs again after the healthy sequence", end["running"], end)

        # Postfix with a main.cf line its own check refuses
        if "postfix" in units:
            base_text = self.snap("postfix-main.cf-before-typo", "read-file", path=MAIN_CF)["file"]["text"]
            situation = "main.cf holds a line Postfix refuses"
            try:
                self.owner("postfix-typo-main-cf", "owner-edit", path=MAIN_CF, content_b64=b64(add_line(base_text, MAIN_CF_TYPO)))
                refused = self.postfix("postfix-with-typo")
                self.check("postfix (refused configuration): `postfix check` refuses main.cf",
                           True if refused["check"].get("returncode") not in (0, None) else None, refused["check"])
                self.service_action("postfix", "reload", situation, (502, "SERVICE_ACTION_FAILED", "check"))
                self.service_action("postfix", "restart", situation, (502, "SERVICE_ACTION_FAILED", "check"))
                self.service_action("postfix", "stop", situation)
                self.service_action("postfix", "start", situation, (502, "SERVICE_ACTION_FAILED", "check"))
            finally:
                self.owner("postfix-typo-removed", "owner-edit", path=MAIN_CF, content_b64=b64(base_text))
            self.service_action("postfix", "start", "after the owner's correction")
            mail = self.smtp("postfix-smtp-end")
            self.check("postfix: mail is accepted on 25 and 587 answers at the end", mail["25"].get("ok") and mail["587"].get("ok"), mail)

        # PostgreSQL with the owner's reload hook that fails after it signalled the server
        instance = units["postgresql"][-1]
        situation = "the owner's reload hook fails"
        hook = self.owner("postgresql-reload-hook", "owner-reload-hook", action="apply", unit=instance)
        self.current["reload_hook"] = {"unit": instance, "exec_reload": hook.get("exec_reload")}
        try:
            failed = self.service_action("postgresql", "reload", situation)
            moved = "pg_conf_load_time() moved" in failed["reload_evidence"]
            detail = str((failed.get("vars") or {}).get("detail") or "") + " " + str(failed["answer"].get("error") or "")
            self.current["reload_hook"].update(server_reread_its_files=moved, answer_detail=detail.strip(),
                                               reload_result=failed["daemon_after"]["reload_result"])
            self.check("postgresql (reload hook): the answer is a failure of the reload, not success",
                       failed["said"] == "failed", {k: failed[k] for k in ("status", "code", "reason", "vars")})
            self.check("postgresql (reload hook): the answer does not say the server keeps its previous settings when the "
                       "server did read its files again", not (moved and "previous settings" in detail),
                       {"server_reread_its_files": moved, "answer_detail": detail.strip()})
            self.service_action("postgresql", "restart", situation)
        finally:
            self.owner("postgresql-reload-hook-removed", "owner-reload-hook", action="restore", unit=instance)
        self.service_action("postgresql", "reload", "after the hook was removed")

        # PostgreSQL with a postgresql.conf the server refuses at start
        conf = (self.state.get("config_files") or {}).get("pg_conf")
        if conf:
            good_text = self.snap("postgresql.conf-before-refused-value", "read-file", path=conf)["file"]["text"]
            bad_text, _ = set_pg_setting(good_text, "work_mem", PG_REFUSED_VALUE)
            situation = "postgresql.conf holds a value the server refuses"
            try:
                self.owner("postgresql.conf-refused-value", "owner-edit", path=conf, content_b64=b64(bad_text))
                self.service_action("postgresql", "restart", situation)
                self.service_action("postgresql", "start", situation + ", after the failed restart")
            finally:
                self.owner("postgresql.conf-corrected", "owner-edit", path=conf, content_b64=b64(good_text))
            self.service_action("postgresql", "start", "after the owner's correction")
            now = self.daemon("postgresql", self.service_state("postgresql-after-correction", "postgresql"))
            if not now["running"]:
                self.service_action("postgresql", "restart", "after the owner's correction (Start left it stopped)")
                now = self.daemon("postgresql", self.service_state("postgresql-after-correction-restart", "postgresql"))
            if not now["running"]:
                self.owner("postgresql-owner-start-instance", "owner-systemctl", action="start", unit=instance)
                now = self.daemon("postgresql", self.service_state("postgresql-after-owner-start", "postgresql"))
                self.note("PostgreSQL was started by the owner on the server (`systemctl start` of the instance unit): "
                          "neither Start nor Restart on the Services page brought it back", now)
            self.check("postgresql: answers again at the end", now["running"], now)
''')

# -- execute and collect ---------------------------------------------------------------------------------------------
rep('''                     "S6-catchall-queue": self.s6_catchall_queue, "S7-file-metadata": self.s7_metadata}''',
    '''                     "S6-catchall-queue": self.s6_catchall_queue, "S7-file-metadata": self.s7_metadata,
                     "S8-service-actions": self.s8_service_actions}''')
rep('''                  "services": ["postfix.service", "postfix@-.service", unit, "mariadb.service", "cron.service", "cronie.service"],''',
    '''                  "services": ["postfix.service", "postfix@-.service", unit, "postgresql.service", "mariadb.service",
                               "cron.service", "cronie.service", "dovecot.service", "nginx.service"],''')
rep('''                  "note": "set1 settings-writes: observations for the owner's review; no update is started and no P0 row is judged."}''',
    '''                  "note": "settings-writes: observations for the owner's review; no update is started and no P0 row is judged."}''')
rep('''                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting")},''',
    '''                  "setup": {"purpose": self.settings.purpose, "components": sorted(self.settings.components),
                            "waiting": self.state.get("setup_waiting"),
                            "step": next(({k: s.get(k) for k in ("verdict", "reason", "started_at", "finished_at")}
                                          for s in self.steps if s["name"] == "setup"), None)},''')

open(p, "w", encoding="utf-8", newline="").write(s.replace("\n", "\r\n") if crlf else s)
print("patched driver; crlf", crlf)
