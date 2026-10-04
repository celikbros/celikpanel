"""Pure matching of already collected migration-writer identities.

This fixture module performs no process/file access, attach, signal or mutation.
A match is not permission to kill and does not establish an open transaction.
The native controller must separately prove VM, worker, lock, admission, stopped
threads and the causal successful WAL write. Unknown observations are refused.
"""
from __future__ import annotations
import re
import stat

HEX32 = re.compile(r"[0-9a-f]{32}\Z")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
SNAPSHOT = re.compile(r"[0-9]{8}T[0-9]{6}Z-from-[A-Za-z0-9._-]+-to-[0-9a-f]{40}-[0-9a-f]{32}\Z")
DIRECTORY = frozenset(("dev", "ino", "mode", "uid", "gid"))
FILE = DIRECTORY | {"links", "size", "mtime_ns", "ctime_ns"}
EXPECTED = frozenset(("operation_id", "snapshot", "token_sha256", "admission_sha256",
                      "candidate_panel_sha256", "uid", "gid", "worker_start_ticks", "work"))
OBSERVED = frozenset(("pid", "tid", "start_ticks", "thread_start_ticks", "cmdline", "environ",
                      "cgroup_raw", "uids", "gids", "executable_sha256", "work",
                      "wal_fd", "wal_path", "wal_descriptor", "wal_entry"))
COMMAND = b"/opt/celikpanel/bin/panel\0--migrate-only\0"
ENVIRONMENT = {
    b"PATH": b"/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
    b"HOME": b"/var/lib/celikpanel", b"LC_ALL": b"C",
}

class IdentityUnavailable(ValueError):
    """Fixed, non-secret reason code; never include raw proc/environment values."""

def require(condition, code):
    if not condition:
        raise IdentityUnavailable(code)

def integer(value, minimum=0, maximum=(1 << 63) - 1):
    return type(value) is int and minimum <= value <= maximum

def record(value, keys):
    return type(value) is dict and value.keys() == keys

def digest(value, expression=HEX64):
    return type(value) is str and expression.fullmatch(value) is not None

def directory(value, uid, gid):
    require(record(value, DIRECTORY), "work-metadata-shape")
    require(all(integer(value[k]) for k in DIRECTORY), "work-metadata-type")
    require(value["ino"] > 0 and value["mode"] <= 0xffff and stat.S_ISDIR(value["mode"])
            and stat.S_IMODE(value["mode"]) == 0o700
            and value["uid"] == uid and value["gid"] == gid, "work-metadata-differs")

def file_identity(value, uid, gid):
    require(record(value, FILE), "wal-metadata-shape")
    require(all(integer(value[k]) for k in FILE), "wal-metadata-type")
    require(value["ino"] > 0 and value["mode"] <= 0xffff and stat.S_ISREG(value["mode"])
            and stat.S_IMODE(value["mode"]) in (0o600, 0o640)
            and value["uid"] == uid and value["gid"] == gid
            and value["links"] == 1 and value["size"] <= 256 * 1024 * 1024,
            "wal-metadata-unsupported")

def environment(raw, work_path):
    require(type(raw) is bytes and len(raw) <= 16384 and raw.endswith(b"\0"), "environment-unavailable")
    actual = {}
    for item in raw[:-1].split(b"\0"):
        key, separator, value = item.partition(b"=")
        require(separator == b"=" and key and key not in actual, "environment-malformed")
        actual[key] = value
    require(actual == {**ENVIRONMENT, b"CELIKPANEL_DATA_DIR": work_path.encode("ascii")},
            "environment-differs")

def match_writer(expected, first, second):
    """Require two identical, bounded kernel observations of the admitted writer.

    Caller supplies *verified* admission/worker expectations. This pure function
    cannot authenticate those expectations or establish a stop/write event.
    """
    require(record(expected, EXPECTED), "expectation-shape")
    require(digest(expected["operation_id"], HEX32), "operation-malformed")
    require(type(expected["snapshot"]) is str and len(expected["snapshot"]) <= 255
            and SNAPSHOT.fullmatch(expected["snapshot"]) is not None, "snapshot-malformed")
    require(all(digest(expected[k]) for k in ("token_sha256", "admission_sha256", "candidate_panel_sha256")),
            "expectation-digest-malformed")
    uid, gid = expected["uid"], expected["gid"]
    require(integer(uid, 1, (1 << 32) - 2) and integer(gid, 1, (1 << 32) - 2), "panel-account-unavailable")
    require(integer(expected["worker_start_ticks"], 1), "worker-start-unavailable")
    directory(expected["work"], uid, gid)
    work_path = "/var/lib/celikpanel/.release-db-migrations/" + expected["token_sha256"] + "/work"
    cgroup = ("0::/system.slice/celikpanel-self-update-" + expected["operation_id"] + ".service\n").encode("ascii")
    for value in (first, second):
        require(record(value, OBSERVED), "observation-shape")
        require(integer(value["pid"], 2, (1 << 31) - 1) and integer(value["tid"], 2, (1 << 31) - 1),
                "process-identity-unavailable")
        require(integer(value["start_ticks"], expected["worker_start_ticks"])
                and integer(value["thread_start_ticks"], value["start_ticks"]), "process-start-differs")
        require(value["cmdline"] == COMMAND and type(value["cmdline"]) is bytes, "command-differs")
        require(value["cgroup_raw"] == cgroup and type(value["cgroup_raw"]) is bytes, "cgroup-differs")
        for key, owner in (("uids", uid), ("gids", gid)):
            require(type(value[key]) is tuple and len(value[key]) == 4
                    and all(type(n) is int and n == owner for n in value[key]), "credentials-differ")
        require(value["executable_sha256"] == expected["candidate_panel_sha256"], "executable-differs")
        environment(value["environ"], work_path)
        directory(value["work"], uid, gid)
        require(value["work"] == expected["work"], "work-directory-changed")
        require(integer(value["wal_fd"], 0, (1 << 20) - 1), "wal-descriptor-unavailable")
        require(type(value["wal_path"]) is str and value["wal_path"] == work_path + "/celikpanel.db-wal",
                "wal-path-differs")
        file_identity(value["wal_descriptor"], uid, gid)
        file_identity(value["wal_entry"], uid, gid)
        require(value["wal_descriptor"] == value["wal_entry"], "wal-descriptor-entry-differ")
    require(first == second, "writer-observation-changed")
    return {
        "schema": "celikpanel/lab-migration-writer-match/v1", "status": "matched",
        "operation_id": expected["operation_id"], "snapshot": expected["snapshot"],
        "admission_sha256": expected["admission_sha256"], "token_sha256": expected["token_sha256"],
        "candidate_panel_sha256": expected["candidate_panel_sha256"],
        "pid": first["pid"], "tid": first["tid"], "start_ticks": first["start_ticks"],
        "thread_start_ticks": first["thread_start_ticks"], "wal": dict(first["wal_descriptor"]),
        "limits": ["pure-observation-match-not-authentication", "no-signal-authority",
                   "no-open-transaction-or-causal-write-proof", "native-stop-and-wal-checks-still-required"],
    }
