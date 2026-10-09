"""set2: the offline tests of the settings-writes cell follow the new cells, the new section and the new modes."""
import sys

p = sys.argv[1]
s = open(p, encoding="utf-8", newline="").read()
crlf = "\r\n" in s
s = s.replace("\r\n", "\n")


def rep(old, new, count=1):
    global s
    assert s.count(old) == count, (old[:70], s.count(old))
    s = s.replace(old, new)


rep('''        self.assertEqual(sorted(trial.CELLS), ["set1-arch", "set1-debian13", "set1-ubuntu"])''',
    '''        self.assertEqual(sorted(trial.CELLS), ["set1-arch", "set1-debian13", "set1-ubuntu",
                                               "set2-arch", "set2-debian13", "set2-ubuntu"])
        for platform in ("arch", "debian13", "ubuntu"):
            first, second = trial.CELLS["set1-" + platform], trial.CELLS["set2-" + platform]
            self.assertEqual(dataclasses.replace(second, name=first.name), first)''')
rep('''                                "S4-postgresql": True, "S5-mariadb": True, "S6-catchall-queue": False,
                                "S7-file-metadata": True})''',
    '''                                "S4-postgresql": True, "S5-mariadb": True, "S6-catchall-queue": False,
                                "S7-file-metadata": True, "S8-service-actions": True})''')
rep('''                            for i in range(7)))''', '''                            for i in range(8)))''')
rep('''        for unit in ("celikpanel-panel", "celikpanel-agent.service", "ssh", "nginx", "postgresql@17-main; reboot"):''',
    '''        for unit in ("dovecot", "nginx.service"):   # set2: the Services page's units
            self.assertEqual(native.unit_name(unit), unit.removesuffix(".service") + ".service")
        for unit in ("celikpanel-panel", "celikpanel-agent.service", "ssh", "sshd.service", "postgresql@17-main; reboot"):''')
rep('''                         ["owner-cron-fault", "owner-crontab", "owner-edit", "owner-reload-hook", "owner-systemctl"])''',
    '''                         ["owner-cron-fault", "owner-crontab", "owner-edit", "owner-pg-reread", "owner-reload-hook",
                          "owner-systemctl"])''')
rep('''        self.assertTrue(dataclasses.is_dataclass(trial.SettingsCell))
''',
    '''        self.assertTrue(dataclasses.is_dataclass(trial.SettingsCell))


class Set2RuleTests(unittest.TestCase):
    def test_the_agents_reread_batch_is_the_products_own(self):
        product = (HERE.parents[2] / "cmd" / "agent" / "db_config.go").read_text(encoding="utf-8")
        for statement in native.AGENT_REREAD_BATCH.strip().split("\\n"):
            head = statement.split(" FROM ")[0].split("'")[1]
            self.assertIn(head, product, statement)
        self.assertIn("pg_reload_conf()", native.AGENT_REREAD_BATCH)
        self.assertIn("pg_conf_load_time()", native.AGENT_REREAD_BATCH)

    def test_the_service_reader_reads_what_the_agent_reads(self):
        product = (HERE.parents[2] / "cmd" / "agent" / "service_action_verify.go").read_text(encoding="utf-8")
        for name in ("Type", "ExecStart", "Wants", "ConsistsOf", "PropagatesReloadTo", "ActiveState", "SubState", "MainPID",
                     "Result", "ReloadResult", "ExecReload"):
            self.assertIn("--property=" + name, product)
            self.assertIn(name, native.SERVICE_PROPERTIES)
        self.assertIn("read-service", native.MODES)

    def test_every_service_has_reload_markers_and_the_refused_value_is_not_a_size(self):
        self.assertEqual(sorted(trial.RELOAD_MARKERS), ["dovecot", "mariadb", "nginx", "postfix", "postgresql"])
        self.assertFalse(trial.PG_REFUSED_VALUE[0].isdigit())
        self.assertIn("unchanged_reloaded", (HERE / "settings_writes_trial.py").read_text(encoding="utf-8"))
''')
open(p, "w", encoding="utf-8", newline="").write(s.replace("\n", "\r\n") if crlf else s)
print("patched tests; crlf", crlf)
