#!/usr/bin/env python3
"""Offline rules of the set1 settings-writes cell kind (no guest, no lab, no network)."""
import dataclasses
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import unittest

HERE = Path(__file__).resolve().parent


def load(name):
    spec = importlib.util.spec_from_file_location(name, HERE / (name + ".py"))
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


trial = load("settings_writes_trial")
native = load("guest_settings_native")

ARTIFACT = {"version": "v0.1.0-alpha.81", "commit": "a" * 40, "sha256": "b" * 64}


class CellTests(unittest.TestCase):
    def test_cells_cover_the_three_platforms_and_mail_follows_the_catalogue(self):
        self.assertEqual(sorted(trial.CELLS), ["set1-arch", "set1-debian13", "set1-ubuntu"])
        for name, cell in trial.CELLS.items():
            self.assertEqual(cell.name, name)
            self.assertIn("postgresql", cell.components)
            self.assertIn("mariadb", cell.components)
            self.assertEqual(cell.cell.variant, "good")
            self.assertIsNone(cell.cell.recovery_fault)
        self.assertFalse(trial.CELLS["set1-arch"].mail)
        self.assertEqual(trial.CELLS["set1-arch"].purpose, "web")
        self.assertNotIn("postfix", trial.CELLS["set1-arch"].components)
        self.assertEqual(trial.CELLS["set1-ubuntu"].platform, "ubuntu")
        self.assertEqual(trial.CELLS["set1-debian13"].platform, "debian13-arch")

    def test_the_cell_kind_adds_no_cell_to_the_update_matrix(self):
        self.assertFalse(set(trial.CELLS) & set(trial.base.CELLS))

    def test_plan_names_every_section_and_never_claims_native_evidence(self):
        plan = trial.build_plan(trial.CELLS["set1-arch"], {"baseline": ARTIFACT}, "/var/tmp/cp-release-drill-x", 18443)
        self.assertIs(plan["native_evidence"], False)
        self.assertEqual(plan["cell_kind"], "settings-writes")
        runs = {s["id"]: s["runs"] for s in plan["sections"]}
        self.assertEqual(runs, {"S1-cron": True, "S2-mail-policy": False, "S3-backup-schedule": True,
                                "S4-postgresql": True, "S5-mariadb": True, "S6-catchall-queue": False,
                                "S7-file-metadata": True})
        self.assertEqual(plan["setup"]["customization"]["components"], ["mariadb", "nginx", "php-fpm", "postgresql"])
        self.assertTrue(all(trial.build_plan(trial.CELLS["set1-debian13"], {"baseline": ARTIFACT}, "/x", 1)["sections"][i]["runs"]
                            for i in range(7)))
        json.dumps(plan)

    def test_site_username_follows_the_product_rule(self):
        self.assertEqual(trial.site_username("set1-owner.test"), "set1_owner_test")
        self.assertEqual(len(trial.site_username("a" * 40 + ".test")), 32)

    def test_section_verdict(self):
        self.assertEqual(trial.section_verdict([{"ok": True}]), "passed")
        self.assertEqual(trial.section_verdict([{"ok": True}, {"ok": None}]), "inconclusive")
        self.assertEqual(trial.section_verdict([{"ok": False}, {"ok": None}]), "failed")
        self.assertEqual(trial.section_verdict([{"ok": True}], "boom"), "inconclusive")
        checks = [{"name": "a: one", "ok": True}, {"name": "f (cron-allow): two", "ok": False},
                  {"name": "f: three", "ok": True}, {"name": "queue: x", "ok": None}, {"name": "no prefix", "ok": True}]
        self.assertEqual(trial.substep_verdicts(checks), {"a": "passed", "f": "failed", "queue": "inconclusive", "section": "passed"})


class ScreenKeyTests(unittest.TestCase):
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
        self.assertIn(trial.MARIADB_ADJUSTED_VALUE[0].lower(), "kmgtpe")
        self.assertIn("read-mariadb-check", native.MODES)


class CronRuleTests(unittest.TestCase):
    def test_only_the_exact_no_crontab_answer_is_no_crontab(self):
        ok = {"status": "ok", "returncode": 1, "stdout": "", "stderr": "no crontab for site\n"}
        self.assertEqual(trial.crontab_native_kind(ok, "site"), "no-crontab")
        self.assertEqual(trial.crontab_native_kind(dict(ok, stderr="no crontab for other\n"), "site"), "failed")
        self.assertEqual(trial.crontab_native_kind(dict(ok, stderr="The user site cannot use this program (crontab)\n"), "site"), "failed")
        self.assertEqual(trial.crontab_native_kind(dict(ok, returncode=0, stdout="* * * * * x\n", stderr=""), "site"), "crontab")
        self.assertEqual(trial.crontab_native_kind({"status": "timeout"}, "site"), "failed")

    def test_owner_lines_are_a_comment_and_a_job(self):
        first, second, rest = trial.OWNER_CRON_LINES.split("\n")
        self.assertTrue(first.startswith("# ") and not first.startswith("# DISABLED:"))
        self.assertEqual(len(second.split()), 7)
        self.assertEqual(rest, "")


class MailRuleTests(unittest.TestCase):
    def test_split_follows_postfix(self):
        self.assertEqual(trial.split_restrictions("permit_mynetworks,\n  reject_rbl_client zen.example ,x"),
                         ["permit_mynetworks", "reject_rbl_client", "zen.example", "x"])
        self.assertEqual(trial.split_restrictions("a { b, c } d"), ["a", "{ b, c }", "d"])
        self.assertEqual(trial.split_restrictions(""), [])

    def test_owner_elements_keep_the_current_list_first(self):
        self.assertEqual(trial.owner_restriction_elements(""),
                         list(trial.POLICY_BASELINE) + ["reject_unknown_sender_domain", "check_policy_service",
                                                        "unix:private/policyd-spf"])
        self.assertEqual(trial.owner_restriction_elements("permit_mynetworks, reject")[:2], ["permit_mynetworks", "reject"])

    def test_block_and_parameter_replacement_round_trip(self):
        elements = trial.owner_restriction_elements("")
        block = trial.restriction_block("smtpd_recipient_restrictions", elements)
        self.assertEqual(block[0], "smtpd_recipient_restrictions =")
        self.assertEqual(block[-1], "    check_policy_service unix:private/policyd-spf")
        text = "a = 1\nsmtpd_recipient_restrictions = x,\n  y\nb = 2\n"
        new = trial.set_main_cf_parameter(text, "smtpd_recipient_restrictions", block)
        self.assertEqual(new.split("\n")[0], "a = 1")
        self.assertEqual(new.split("\n")[-2], "b = 2")
        self.assertNotIn("  y", new.split("\n"))
        self.assertEqual(trial.main_cf_logical_line(new, "smtpd_recipient_restrictions"), "\n".join(block))
        value = " ".join(line.strip() for line in block)[len("smtpd_recipient_restrictions ="):]
        self.assertEqual(trial.split_restrictions(value), elements)
        self.assertEqual(trial.set_main_cf_parameter(new, "smtpd_recipient_restrictions", None), "a = 1\nb = 2\n")
        self.assertTrue(trial.set_main_cf_parameter("a = 1\n", "z", ["z = 2"]).endswith("a = 1\nz = 2\n"))

    def test_typo_line_is_added_and_removed_exactly(self):
        text = "a = 1\n"
        with_typo = trial.add_line(text, trial.MAIN_CF_TYPO)
        self.assertEqual(with_typo, "a = 1\n" + trial.MAIN_CF_TYPO + "\n")
        self.assertEqual(trial.remove_line(with_typo, trial.MAIN_CF_TYPO), text)
        self.assertEqual(trial.add_line("a = 1", "b"), "a = 1\nb\n")


class DatabaseRuleTests(unittest.TestCase):
    def test_pg_setting_changes_only_its_line(self):
        text = "# head\n#work_mem = 4MB\t\t\t\t# min 64kB\nshared_buffers = 128MB\n"
        new, how = trial.set_pg_setting(text, "work_mem", "8MB")
        self.assertEqual(how, "commented-default")
        self.assertEqual(new, "# head\nwork_mem = 8MB\t\t\t\t# min 64kB\nshared_buffers = 128MB\n")
        again, how = trial.set_pg_setting(new, "work_mem", "eight")
        self.assertEqual((how, again.split("\n")[1]), ("active-line", "work_mem = eight\t\t\t\t# min 64kB"))
        added, how = trial.set_pg_setting("a = 1\n", "work_mem", "8MB")
        self.assertEqual((how, added), ("appended", "a = 1\nwork_mem = 8MB\n"))
        changes = trial.line_changes(text, new)
        self.assertEqual((changes["removed"], changes["added"]), (["#work_mem = 4MB\t\t\t\t# min 64kB"], ["work_mem = 8MB\t\t\t\t# min 64kB"]))

    def test_mariadb_option_changes_only_its_line(self):
        text = "[server]\n[mysqld]\n#max_connections        = 100\nbind-address = 127.0.0.1\n"
        new, how, index = trial.set_mariadb_option(text, "max_connections", "173")
        self.assertEqual((how, index, new.split("\n")[2]), ("commented-default", 2, "max_connections        = 173"))
        new, how, index = trial.set_mariadb_option("[client-server]\n!includedir /etc/my.cnf.d\n", "max_connections", "173")
        self.assertEqual(how, "added-new-group")
        self.assertTrue(new.endswith("\n[mysqld]\nmax_connections = 173\n"))
        self.assertEqual(new.split("\n")[index], "max_connections = 173")
        new, how, index = trial.set_mariadb_option("[mysqld]\nx = 1\n", "max_connections", "173")
        self.assertEqual((how, index, new), ("added-under-[mysqld]", 1, "[mysqld]\nmax_connections = 173\nx = 1\n"))
        self.assertEqual(trial.insert_after(new, index, "bad = 1").split("\n")[2], "bad = 1")

    def test_local_rules_are_removed_and_reported(self):
        text = "# c\nlocal   all   postgres   peer\nlocal all all peer\nhost all all 127.0.0.1/32 scram-sha-256\n"
        new, removed = trial.remove_local_rules(text)
        self.assertEqual(len(removed), 2)
        self.assertEqual(new, "# c\nhost all all 127.0.0.1/32 scram-sha-256\n")

    def test_unit_and_file_choice_follow_the_product(self):
        self.assertEqual(trial.pg_unit("/etc/postgresql/17/main/postgresql.conf"), "postgresql@17-main.service")
        self.assertEqual(trial.pg_unit("/var/lib/postgres/data/postgresql.conf"), "postgresql.service")
        scan = {"services": [
            {"id": "postgresql", "config_files": [{"path": "/etc/postgresql/17/main/postgresql.conf"},
                                                  {"path": "/etc/postgresql/17/main/pg_hba.conf"}]},
            {"id": "mariadb", "config_files": [{"path": "/etc/mysql/mariadb.cnf"},
                                               {"path": "/etc/mysql/mariadb.conf.d/50-server.cnf"}]},
            {"id": "nginx", "config_files": None}]}
        files = trial.choose_config_files(scan)
        self.assertEqual(files["pg_hba"], "/etc/postgresql/17/main/pg_hba.conf")
        self.assertEqual(files["mariadb_conf"], "/etc/mysql/mariadb.conf.d/50-server.cnf")
        self.assertEqual(trial.choose_config_files({"services": [{"id": "mariadb", "config_files": [{"path": "/etc/my.cnf"}]}]})
                         ["mariadb_conf"], "/etc/my.cnf")
        self.assertIsNone(trial.choose_config_files({"services": []})["pg_conf"])

    def test_metadata_comparison(self):
        before = {"exists": True, "type": "regular", "uid": 0, "gid": 0, "owner": "root", "group": "root", "mode": "0644"}
        self.assertTrue(trial.same_metadata(before, dict(before, inode=9))["equal"])
        self.assertEqual(trial.same_metadata(before, dict(before, mode="0600"))["differences"], {"mode": ["0644", "0600"]})
        self.assertFalse(trial.same_metadata(before, {"exists": False})["equal"])


class BackupRuleTests(unittest.TestCase):
    def test_would_prune_follows_the_product_rule(self):
        items = [{"name": f"s{i}", "origin": "scheduled", "type": "full", "created_at": f"2026-10-0{i}T00:00:00Z"} for i in range(1, 6)]
        items += [{"name": "m", "origin": "manual", "type": "full", "created_at": "2026-10-09T00:00:00Z"},
                  {"name": "d", "origin": "scheduled", "type": "database", "created_at": "2026-10-09T00:00:00Z"}]
        result = trial.would_prune({"backups": items}, 3)
        self.assertEqual(result["scheduled_files_or_full"], 5)
        self.assertEqual(result["would_keep"], ["s5", "s4", "s3"])
        self.assertEqual(result["would_prune"], ["s2", "s1"])
        self.assertEqual(trial.would_prune([], 14)["would_prune"], [])
        self.assertEqual(trial.would_prune(None, 14)["listed"], 0)


class GuestHelperRuleTests(unittest.TestCase):
    def test_owner_actions_touch_only_this_cells_files_and_units(self):
        for path in ("/etc/postfix/main.cf", "/etc/postgresql/17/main/pg_hba.conf", "/var/lib/postgres/data/postgresql.conf",
                     "/etc/mysql/mariadb.conf.d/50-server.cnf", "/etc/my.cnf"):
            self.assertEqual(native.editable_path(path), path)
        for path in ("/etc/passwd", "/etc/postfix/../passwd", "/etc/postfix/master.cf", "/root/.ssh/authorized_keys",
                     "/etc/postgresql/17/main/../../../shadow", "/opt/celikpanel/bin/panel", "/var/lib/celikpanel/celikpanel.db", 7):
            with self.assertRaises(native.Refused):
                native.editable_path(path)
        self.assertEqual(native.unit_name("postfix"), "postfix.service")
        self.assertEqual(native.unit_name("postgresql@17-main.service"), "postgresql@17-main.service")
        for unit in ("celikpanel-panel", "celikpanel-agent.service", "ssh", "nginx", "postgresql@17-main; reboot"):
            with self.assertRaises(native.Refused):
                native.unit_name(unit)
        with self.assertRaises(native.Refused):
            native.user_name("root")
        with self.assertRaises(native.Refused):
            native.user_name("a;b")

    def test_backup_and_candidate_names(self):
        names = ["pg_hba.conf", "pg_hba.conf.celikpanel-backup-20261009T010203Z", ".pg_hba.conf.celikpanel-candidate-ab12",
                 "postgresql.conf.celikpanel-backup-20261009T010203Z", "pg_hba.conf.celikpanel-backup-20261008T010203Z"]
        self.assertEqual(native.backup_names(names, "pg_hba.conf"),
                         ["pg_hba.conf.celikpanel-backup-20261008T010203Z", "pg_hba.conf.celikpanel-backup-20261009T010203Z"])
        self.assertEqual(native.candidate_names(names, "pg_hba.conf"), [".pg_hba.conf.celikpanel-candidate-ab12"])
        self.assertEqual(native.mode_text(0o100640), "0640")

    def test_every_mode_is_a_reader_or_a_named_owner_action(self):
        self.assertTrue(all(mode.startswith(("read-", "owner-")) for mode in native.MODES))
        self.assertEqual(sorted(m for m in native.MODES if m.startswith("owner-")),
                         ["owner-cron-fault", "owner-crontab", "owner-edit", "owner-reload-hook", "owner-systemctl"])
        source = (HERE / "guest_settings_native.py").read_text(encoding="utf-8")
        for forbidden in ("celikpanel.net", "curl", "urllib", "http.client", "requests"):
            self.assertNotIn(forbidden, source)
        self.assertIn("mode=ro", source)

    def test_driver_reaches_the_guest_only_through_the_lab_and_the_panel_api(self):
        source = (HERE / "settings_writes_trial.py").read_text(encoding="utf-8")
        self.assertNotIn("celikpanel.net", source)
        self.assertNotIn("subprocess", source)
        self.assertEqual(hashlib.sha256(b"").hexdigest(), "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855")
        self.assertTrue(dataclasses.is_dataclass(trial.SettingsCell))


if __name__ == "__main__":
    unittest.main()
