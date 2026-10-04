"""Negative scope and identity tests; no process access or signal operations."""
import copy
import json
import stat
import unittest

from wal_migration_identity import COMMAND, ENVIRONMENT, IdentityUnavailable, match_writer


def fixture():
    expected = {
        "operation_id": "a" * 32,
        "snapshot": "20260915T010203Z-from-old-to-" + "b" * 40 + "-" + "c" * 32,
        "token_sha256": "d" * 64, "admission_sha256": "e" * 64,
        "candidate_panel_sha256": "f" * 64,
        "uid": 991, "gid": 992, "worker_start_ticks": 100,
        "work": {"dev": 7, "ino": 800, "mode": stat.S_IFDIR | 0o700, "uid": 991, "gid": 992},
    }
    path = "/var/lib/celikpanel/.release-db-migrations/" + expected["token_sha256"] + "/work"
    wal = {"dev": 7, "ino": 801, "mode": stat.S_IFREG | 0o640, "uid": 991,
           "gid": 992, "links": 1, "size": 32768, "mtime_ns": 900, "ctime_ns": 901}
    value = {
        "pid": 1200, "tid": 1203, "start_ticks": 110, "thread_start_ticks": 112,
        "cmdline": COMMAND,
        "environ": b"\0".join(k + b"=" + v for k, v in
                                  {**ENVIRONMENT, b"CELIKPANEL_DATA_DIR": path.encode()}.items()) + b"\0",
        "cgroup_raw": b"0::/system.slice/celikpanel-self-update-" + b"a" * 32 + b".service\n",
        "uids": (991,) * 4, "gids": (992,) * 4,
        "executable_sha256": "f" * 64, "work": copy.deepcopy(expected["work"]),
        "wal_fd": 8, "wal_path": path + "/celikpanel.db-wal",
        "wal_descriptor": copy.deepcopy(wal), "wal_entry": copy.deepcopy(wal),
    }
    return expected, value


class WriterIdentityTests(unittest.TestCase):
    def assert_refused(self, expected, first, second=None):
        with self.assertRaises(IdentityUnavailable) as caught:
            match_writer(expected, first, first if second is None else second)
        self.assertRegex(str(caught.exception), r"^[a-z-]+$")
        return str(caught.exception)

    def test_stable_admitted_writer_has_explicit_evidence_limits(self):
        expected, value = fixture()
        result = match_writer(expected, value, copy.deepcopy(value))
        self.assertEqual(result["status"], "matched")
        self.assertIn("no-signal-authority", result["limits"])
        self.assertIn("no-open-transaction-or-causal-write-proof", result["limits"])
        encoded = json.dumps(result)
        for raw in ("CELIKPANEL_DATA_DIR", "/var/lib", "cmdline", "environ"):
            self.assertNotIn(raw, encoded)
        value["wal_entry"]["ino"] += 1
        self.assertEqual(result["wal"]["ino"], 801)

    def test_other_operation_binary_or_path_is_not_the_admitted_writer(self):
        cases = {
            "cgroup_raw": b"0::/system.slice/celikpanel-agent.service\n",
            "cmdline": b"/opt/celikpanel/bin/panel\0--migrate-only\0--extra\0",
            "executable_sha256": "0" * 64,
            "wal_path": "/var/lib/celikpanel/celikpanel.db-wal",
        }
        for key, wrong in cases.items():
            with self.subTest(key=key):
                expected, value = fixture()
                value[key] = wrong
                self.assert_refused(expected, value)
        for key in ("operation_id", "token_sha256", "candidate_panel_sha256"):
            with self.subTest(expectation=key):
                expected, value = fixture()
                expected[key] = "1" * len(expected[key])
                self.assert_refused(expected, value)

    def test_retained_root_credentials_and_numeric_coercion_are_refused(self):
        for key, wrong in (("uids", (991, 991, 0, 991)), ("gids", (992, 992, 992, 0)),
                           ("uids", [991] * 4), ("pid", True), ("tid", 1),
                           ("start_ticks", 99), ("thread_start_ticks", 109), ("wal_fd", -1)):
            with self.subTest(key=key, wrong=wrong):
                expected, value = fixture()
                value[key] = wrong
                self.assert_refused(expected, value)
        for key, wrong in (("uid", 0), ("gid", True), ("worker_start_ticks", 0)):
            expected, value = fixture()
            expected[key] = wrong
            self.assert_refused(expected, value)

    def test_environment_ambiguity_and_secrets_never_leak(self):
        for extra in (b"SECRET=do-not-report-this\0", b"LC_ALL=C\0", b"invalid\0"):
            expected, value = fixture()
            value["environ"] += extra
            reason = self.assert_refused(expected, value)
            self.assertNotIn("do-not-report-this", reason)
        expected, value = fixture()
        value["environ"] = value["environ"][:-1]
        self.assert_refused(expected, value)
        expected, value = fixture()
        value["environ"] = value["environ"].replace(b"/work", b"/work/../authority")
        self.assert_refused(expected, value)

    def test_wal_alias_replacement_and_unsafe_metadata_are_refused(self):
        for key, wrong in (("ino", 999), ("dev", 8), ("ctime_ns", 902)):
            expected, value = fixture()
            value["wal_entry"][key] = wrong
            self.assertEqual(self.assert_refused(expected, value), "wal-descriptor-entry-differ")
        for key, wrong in (("mode", stat.S_IFLNK | 0o600), ("mode", stat.S_IFREG | 0o660),
                           ("mode", (1 << 32) | stat.S_IFREG | 0o600), ("links", 2),
                           ("uid", 0), ("gid", 0), ("ino", 0), ("size", 256 * 1024 * 1024 + 1),
                           ("size", True)):
            with self.subTest(key=key, wrong=wrong):
                expected, value = fixture()
                value["wal_entry"][key] = value["wal_descriptor"][key] = wrong
                self.assert_refused(expected, value)

    def test_reused_process_or_changed_file_between_observations_is_refused(self):
        for key in ("pid", "tid", "start_ticks", "thread_start_ticks", "wal_fd"):
            expected, first = fixture()
            second = copy.deepcopy(first)
            second[key] += 1
            self.assertEqual(self.assert_refused(expected, first, second), "writer-observation-changed")
        expected, first = fixture()
        second = copy.deepcopy(first)
        second["wal_entry"]["size"] += 1
        second["wal_descriptor"]["size"] += 1
        self.assertEqual(self.assert_refused(expected, first, second), "writer-observation-changed")

    def test_work_directory_is_bound_to_admission(self):
        for key, wrong in (("ino", 802), ("dev", 8), ("mode", stat.S_IFDIR | 0o750),
                           ("mode", stat.S_IFLNK | 0o700), ("uid", 0)):
            expected, value = fixture()
            value["work"][key] = wrong
            self.assert_refused(expected, value)

    def test_missing_or_extra_observations_are_unknown(self):
        expected, value = fixture()
        for key in list(value):
            incomplete = copy.deepcopy(value)
            del incomplete[key]
            self.assertEqual(self.assert_refused(expected, incomplete), "observation-shape")
        value["secret"] = "not-a-supported-observation"
        self.assertEqual(self.assert_refused(expected, value), "observation-shape")
        expected, value = fixture()
        expected["snapshot"] = "../../other"
        self.assertEqual(self.assert_refused(expected, value), "snapshot-malformed")


if __name__ == "__main__":
    unittest.main()
