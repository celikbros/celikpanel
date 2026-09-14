#!/usr/bin/env python3
"""Read-only native observations inside an explicitly marked disposable QEMU guest.

Salt okunur yerel gözlemler; yalnız açıkça işaretlenmiş geçici QEMU konuğunda.
This does not install, update, repair, stop services, or read private credentials.
Bu araç kurmaz, güncellemez, onarmaz, servis durdurmaz, özel kimlik bilgisi okumaz.
"""
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import re
import selectors
import signal
import socket
import sqlite3
import ssl
import stat
import subprocess
import sys
import time
import uuid

SCHEMA = "celikpanel/release-recovery-observation/v1"
MARKER_SCHEMA = "celikpanel-release-recovery-lab/v1"
MARKER = Path("/etc/celikpanel-release-recovery-lab")
PANEL_DB = Path("/var/lib/celikpanel/celikpanel.db")
TLS_ROOT = Path("/var/lib/celikpanel/tls")
WEB_ROOT = Path("/opt/celikpanel/web")
TRANSACTION = Path("/var/lib/celikpanel-release-transaction/active")
HEX64 = re.compile(r"[0-9a-f]{64}\Z")
HEX32 = re.compile(r"[0-9a-f]{32}\Z")
CELL = re.compile(r"[A-Za-z0-9][A-Za-z0-9_.-]{0,239}\Z")
SERVICE_PROPERTIES = (
    "Id", "LoadState", "ActiveState", "SubState", "MainPID", "Result",
    "ControlGroup", "ExecMainCode", "ExecMainStatus", "InvocationID",
)
TIMER_PROPERTIES = ("Id", "LoadState", "ActiveState", "SubState", "Unit",
                    "LastTriggerUSec", "NextElapseUSecRealtime", "NextElapseUSecMonotonic")
SERVICES = {
    "celikpanel-agent.service": Path("/opt/celikpanel/bin/agent"),
    "celikpanel-panel.service": Path("/opt/celikpanel/bin/panel"),
}
TIMERS = (
    "celikpanel-release-recovery.timer", "celikpanel-release-recovery.service",
    "certbot.timer", "celikpanel-mail-certificate-renewal.timer",
    "celikpanel-firewall-restore.service",
)


class ProbeError(Exception):
    """A missing or unsafe observation is unknown, never evidence of success."""


def utc_now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat(timespec="microseconds")


def unknown(reason: str) -> dict:
    return {"status": "unknown", "reason": reason}


def collect_observation(observer, *args) -> dict:
    """One malformed subsystem must not erase other independently observed facts."""
    try:
        result = observer(*args)
        if not isinstance(result, dict):
            return unknown("observer returned an invalid result")
        return result
    except Exception as exc:
        return unknown(f"{observer.__name__} observation failed: {exc.__class__.__name__}")


def metadata(st: os.stat_result) -> tuple:
    return st.st_dev, st.st_ino, st.st_size, st.st_mtime_ns, st.st_ctime_ns


def bounded_file(path: Path, limit: int, *, marker: bool = False, virtual: bool = False) -> bytes:
    try:
        fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC)
        with os.fdopen(fd, "rb") as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1:
                raise ProbeError("file is not a single-link regular file")
            if marker and (before.st_uid != 0 or stat.S_IMODE(before.st_mode) != 0o444):
                raise ProbeError("lab marker must be root-owned mode 0444")
            if not virtual and before.st_size > limit:
                raise ProbeError("file exceeds observation size limit")
            result = stream.read(limit + 1)
            if len(result) > limit or metadata(before) != metadata(os.fstat(stream.fileno())):
                raise ProbeError("file changed or exceeded observation size limit")
            return result
    except OSError as exc:
        raise ProbeError(f"file unavailable: {exc.__class__.__name__}") from exc


def strict_object(raw: bytes) -> dict:
    def pairs(items):
        result = {}
        for key, value in items:
            if key in result:
                raise ProbeError("duplicate JSON field")
            result[key] = value
        return result
    try:
        value = json.loads(raw, object_pairs_hook=pairs)
    except (ValueError, UnicodeError) as exc:
        raise ProbeError("invalid JSON") from exc
    if not isinstance(value, dict):
        raise ProbeError("expected JSON object")
    return value


def validate_identity(raw: bytes, nonce: str, vm_uuid: str, cell_id: str,
                      node: str, dmi_uuid: str, vendor: str, product: str) -> dict:
    value = strict_object(raw)
    if set(value) != {"schema", "nonce", "vm_uuid", "cell_id", "node"}:
        raise ProbeError("lab marker fields differ")
    if not HEX64.fullmatch(nonce) or not CELL.fullmatch(cell_id):
        raise ProbeError("invalid expected lab identity")
    try:
        expected_uuid = str(uuid.UUID(vm_uuid))
        actual_uuid = str(uuid.UUID(dmi_uuid.strip()))
    except (ValueError, AttributeError) as exc:
        raise ProbeError("invalid VM UUID") from exc
    if vm_uuid != expected_uuid or value != {
        "schema": MARKER_SCHEMA, "nonce": nonce, "vm_uuid": vm_uuid,
        "cell_id": cell_id, "node": node,
    }:
        raise ProbeError("lab marker does not match requested identity")
    if node not in {"debian13", "arch"} or actual_uuid != expected_uuid:
        raise ProbeError("guest UUID or node differs")
    if vendor.strip().lower() not in {"qemu", "kvm"}:
        raise ProbeError("guest DMI vendor is not QEMU/KVM")
    if not any(part in product.lower() for part in ("qemu", "kvm", "standard pc")):
        raise ProbeError("guest DMI product is not recognized QEMU/KVM")
    return {"nonce": nonce, "vm_uuid": vm_uuid, "cell_id": cell_id, "node": node}


def guard_guest(args) -> dict:
    if sys.platform != "linux" or os.geteuid() != 0:
        raise ProbeError("collector requires root inside the marked Linux QEMU guest")
    return validate_identity(
        bounded_file(MARKER, 2048, marker=True), args.lab_nonce, args.vm_uuid,
        args.cell_id, args.node,
        bounded_file(Path("/sys/class/dmi/id/product_uuid"), 128, virtual=True).decode(),
        bounded_file(Path("/sys/class/dmi/id/sys_vendor"), 128, virtual=True).decode(),
        bounded_file(Path("/sys/class/dmi/id/product_name"), 256, virtual=True).decode(),
    )


def command(argv: list[str], *, timeout: float = 8, limit: int = 131072,
            input_bytes: bytes | None = None) -> dict:
    """Bounded subprocess output. Only this collector's child is terminated."""
    if not argv or not os.path.isabs(argv[0]):
        return unknown("command executable must be absolute")
    try:
        proc = subprocess.Popen(argv, stdin=subprocess.PIPE if input_bytes is not None else subprocess.DEVNULL,
                                stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                env={"PATH": "/usr/sbin:/usr/bin:/sbin:/bin", "LC_ALL": "C"},
                                start_new_session=True)
    except OSError as exc:
        return unknown(f"command unavailable: {exc.__class__.__name__}")
    output = bytearray()
    reason = None
    selector = selectors.DefaultSelector()
    try:
        if input_bytes is not None:
            if len(input_bytes) > 32768:
                raise ProbeError("command input exceeds public-certificate limit")
            proc.stdin.write(input_bytes)
            proc.stdin.close()
        selector.register(proc.stdout, selectors.EVENT_READ)
        deadline = time.monotonic() + timeout
        while selector.get_map():
            if time.monotonic() >= deadline:
                reason = "command timed out"
                break
            for key, _ in selector.select(min(0.25, max(0, deadline - time.monotonic()))):
                chunk = os.read(key.fileobj.fileno(), min(16384, limit + 1 - len(output)))
                if not chunk:
                    selector.unregister(key.fileobj)
                    continue
                output.extend(chunk)
                if len(output) > limit:
                    reason = "command output exceeded limit"
                    break
            if reason:
                break
        if reason:
            os.killpg(proc.pid, signal.SIGKILL)
        proc.wait(timeout=2)
    except (OSError, subprocess.SubprocessError, ProbeError) as exc:
        reason = f"command observation interrupted: {exc.__class__.__name__}"
        if proc.poll() is None:
            os.killpg(proc.pid, signal.SIGKILL)
            proc.wait(timeout=2)
    finally:
        selector.close()
        proc.stdout.close()
    if reason:
        return unknown(reason)
    return {"status": "ok", "returncode": proc.returncode,
            "output": output.decode("utf-8", errors="replace")}


def parse_properties(raw: str, *, timer: bool = False) -> dict:
    result = {}
    for line in raw.splitlines():
        if not line or "=" not in line:
            raise ProbeError("systemctl properties are malformed")
        key, value = line.split("=", 1)
        if key not in (TIMER_PROPERTIES if timer else SERVICE_PROPERTIES) or key in result:
            raise ProbeError("unexpected or duplicate systemctl property")
        result[key] = value
    required = {"Id", "LoadState", "ActiveState", "SubState"}
    if not timer:
        required.add("MainPID")
    if not required <= result.keys():
        raise ProbeError("systemctl properties are incomplete")
    if not timer and not re.fullmatch(r"[0-9]+", result["MainPID"]):
        raise ProbeError("MainPID is not numeric")
    return result


def service_properties(unit: str) -> dict:
    timer = unit.endswith(".timer")
    observed = command(["/usr/bin/systemctl", "show", unit, "--no-pager",
                        "--property=" + ",".join(TIMER_PROPERTIES if timer else SERVICE_PROPERTIES)])
    if observed["status"] != "ok":
        return observed
    if observed["returncode"] != 0:
        return unknown("systemctl show failed")
    try:
        return {"status": "ok", "properties": parse_properties(observed["output"], timer=timer)}
    except ProbeError as exc:
        return unknown(str(exc))


def hash_file(path: Path, *, proc_executable: bool = False) -> dict:
    try:
        flags = os.O_RDONLY | os.O_CLOEXEC | (0 if proc_executable else os.O_NOFOLLOW)
        fd = os.open(path, flags)
        digest = hashlib.sha256()
        with os.fdopen(fd, "rb") as stream:
            before = os.fstat(stream.fileno())
            if not stat.S_ISREG(before.st_mode) or before.st_size > 268435456:
                raise ProbeError("executable is not a bounded regular file")
            total = 0
            while chunk := stream.read(1048576):
                total += len(chunk)
                if total > 268435456:
                    raise ProbeError("executable grew beyond observation limit")
                digest.update(chunk)
            if metadata(before) != metadata(os.fstat(stream.fileno())):
                raise ProbeError("executable changed during hashing")
        return {"status": "ok", "sha256": digest.hexdigest(), "size": before.st_size}
    except (OSError, ProbeError) as exc:
        return unknown(f"executable could not be observed: {exc.__class__.__name__}")


def process_start(pid: int) -> str:
    raw = bounded_file(Path(f"/proc/{pid}/stat"), 16384).decode()
    tail = raw[raw.rfind(")") + 2:].split()
    if len(tail) < 20 or not tail[19].isdigit():
        raise ProbeError("process start identity is unavailable")
    return tail[19]


def observe_service(unit: str, binary: Path) -> dict:
    result = service_properties(unit)
    result["installed_executable"] = hash_file(binary)
    if result["status"] != "ok":
        result["running_executable"] = unknown("service identity unavailable")
        return result
    pid = int(result["properties"]["MainPID"])
    if pid <= 1:
        result["running_executable"] = {"status": "absent", "reason": "no running MainPID"}
        return result
    try:
        start = process_start(pid)
        executable = Path(f"/proc/{pid}/exe")
        target = os.readlink(executable)
        running = hash_file(executable, proc_executable=True)
        after = service_properties(unit)
        if process_start(pid) != start or after.get("properties", {}).get("MainPID") != str(pid):
            raise ProbeError("service process changed during observation")
        running.update({"pid": pid, "start_ticks": start, "target": target})
        result["running_executable"] = running
    except (OSError, ProbeError) as exc:
        result["running_executable"] = unknown(f"process identity unstable: {exc.__class__.__name__}")
    return result


def observe_database() -> dict:
    # immutable avoids creating/writing SQLite SHM. Never silently ignore a WAL.
    # immutable SHM oluşturmaz/yazmaz. WAL varsa içerik yok sayılarak başarı verilmez.
    try:
        before = PANEL_DB.lstat()
        if not stat.S_ISREG(before.st_mode) or before.st_nlink != 1:
            raise ProbeError("database is not a single-link regular file")
        wal = Path(str(PANEL_DB) + "-wal")
        if wal.exists() or wal.is_symlink():
            info = wal.lstat()
            if not stat.S_ISREG(info.st_mode) or info.st_size != 0:
                return unknown("nonempty or unsafe WAL requires a separately verified consistent snapshot")
        connection = sqlite3.connect(PANEL_DB.as_uri() + "?mode=ro&immutable=1", uri=True, timeout=2)
        deadline = time.monotonic() + 8
        connection.set_progress_handler(lambda: int(time.monotonic() > deadline), 1000)
        try:
            connection.execute("PRAGMA query_only=ON")
            integrity = [row[0] for row in connection.execute("PRAGMA integrity_check(10)")]
            version = connection.execute("PRAGMA user_version").fetchone()[0]
            names = {row[0] for row in connection.execute("SELECT name FROM sqlite_schema WHERE type='table'")}
            migrations = None
            if "schema_migrations" in names:
                columns = {row[1] for row in connection.execute("PRAGMA table_info(schema_migrations)")}
                if "version" in columns:
                    migrations = [row[0] for row in connection.execute("SELECT version FROM schema_migrations ORDER BY version LIMIT 1000")]
        finally:
            connection.close()
        if metadata(before) != metadata(PANEL_DB.lstat()) or (wal.exists() and wal.stat().st_size != 0):
            return unknown("database or WAL changed during observation")
        return {"status": "ok", "integrity_check": integrity, "user_version": version,
                "schema_version": max(migrations) if migrations else None, "migrations": migrations}
    except (OSError, sqlite3.Error, ProbeError) as exc:
        return unknown(f"database observation failed: {exc.__class__.__name__}")


def observe_web() -> dict:
    try:
        if WEB_ROOT.is_symlink() or not WEB_ROOT.is_dir():
            raise ProbeError("web root is not a real directory")
        digest = hashlib.sha256()
        count = total = 0
        directory_metadata = {}
        for root, dirs, files in os.walk(WEB_ROOT, followlinks=False):
            directory_metadata[Path(root)] = metadata(Path(root).stat())
            for name in dirs:
                if (Path(root) / name).is_symlink():
                    raise ProbeError("web tree contains a directory symlink")
            dirs.sort()
            for name in sorted(files):
                path = Path(root) / name
                raw = bounded_file(path, 16777216)
                count += 1
                total += len(raw)
                if count > 10000 or total > 268435456:
                    raise ProbeError("web tree exceeds observation limit")
                digest.update(path.relative_to(WEB_ROOT).as_posix().encode() + b"\0")
                digest.update(hashlib.sha256(raw).hexdigest().encode() + b"\n")
        if not count:
            raise ProbeError("web tree is empty")
        if any(metadata(path.stat()) != before for path, before in directory_metadata.items()):
            raise ProbeError("web directory changed during observation")
        return {"status": "ok", "tree_sha256": digest.hexdigest(), "file_count": count,
                "byte_count": total, "algorithm": "sha256(sorted-traversal(relative-path,NUL,file-sha256,LF))"}
    except (OSError, ProbeError) as exc:
        return unknown(f"web tree unavailable or unstable: {exc.__class__.__name__}")


def parse_certificate(raw: str) -> dict:
    fingerprint = re.search(r"(?im)^sha256 Fingerprint=([0-9a-f:]+)$", raw)
    begin = re.search(r"(?m)^notBefore=(.+)$", raw)
    end = re.search(r"(?m)^notAfter=(.+)$", raw)
    san = re.findall(r"(?:DNS|IP Address):[^,\r\n]+", raw)
    if not fingerprint or not begin or not end:
        raise ProbeError("public certificate properties are incomplete")
    sha = fingerprint.group(1).replace(":", "").lower()
    if not HEX64.fullmatch(sha):
        raise ProbeError("certificate fingerprint is malformed")
    return {"status": "ok", "sha256_fingerprint": sha, "subject_alt_name": san,
            "not_before": begin.group(1), "not_after": end.group(1)}


def certificate_details(der: bytes) -> dict:
    result = command(["/usr/bin/openssl", "x509", "-inform", "DER", "-noout",
                      "-fingerprint", "-sha256", "-ext", "subjectAltName", "-dates"], input_bytes=der)
    if result["status"] != "ok" or result.get("returncode") != 0:
        return unknown("public certificate could not be decoded")
    try:
        return parse_certificate(result["output"])
    except ProbeError as exc:
        return unknown(str(exc))


def observe_tls() -> dict:
    try:
        current = TLS_ROOT / "current"
        candidate = current / "panel.crt" if current.exists() or current.is_symlink() else TLS_ROOT / "panel.crt"
        resolved = candidate.resolve(strict=True)
        if not resolved.is_relative_to(TLS_ROOT) or resolved.name != "panel.crt":
            raise ProbeError("public certificate escapes managed TLS root")
        pem = bounded_file(resolved, 32768).decode("ascii")
        # Only the public certificate is opened; no .key or receipt is read.
        first = re.search(r"-----BEGIN CERTIFICATE-----[\s\S]*?-----END CERTIFICATE-----", pem)
        if not first:
            raise ProbeError("public certificate PEM block is absent")
        der = ssl.PEM_cert_to_DER_cert(first.group(0))
        result = certificate_details(der)
    except (OSError, ValueError, UnicodeError, ProbeError) as exc:
        result = unknown(f"installed public certificate unavailable: {exc.__class__.__name__}")
    try:
        context = ssl.SSLContext(ssl.PROTOCOL_TLS_CLIENT)
        context.check_hostname = False
        context.verify_mode = ssl.CERT_NONE
        with socket.create_connection(("127.0.0.1", 2083), timeout=5) as tcp:
            with context.wrap_socket(tcp) as tls:
                der = tls.getpeercert(binary_form=True)
        result["served"] = certificate_details(der)
    except (OSError, ValueError) as exc:
        result["served"] = unknown(f"loopback TLS unavailable: {exc.__class__.__name__}")
    return result


def valid_dns_name(value: str) -> str:
    value = value.rstrip(".").lower()
    if len(value) > 253 or not all(re.fullmatch(r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?", label) for label in value.split(".")):
        raise argparse.ArgumentTypeError("invalid DNS observation name")
    return value


def parse_dig(raw: str) -> dict:
    status_match = re.search(r"status: ([A-Z0-9]+),", raw)
    flags = re.search(r"flags: ([^;]*);", raw)
    if not status_match or not flags:
        raise ProbeError("DNS response header is missing")
    answers = []
    for line in raw.splitlines():
        if not line or line.startswith(";"):
            continue
        fields = line.split()
        if len(fields) < 5 or fields[2] != "IN" or fields[3] not in {"A", "SOA", "CNAME"}:
            raise ProbeError("DNS answer is outside the bounded observation format")
        answers.append({"name": fields[0], "ttl": fields[1], "type": fields[3], "data": " ".join(fields[4:])})
    return {"status": "ok", "rcode": status_match.group(1),
            "authoritative": "aa" in flags.group(1).split(), "answers": answers}


def observe_dns(zone: str, name: str) -> dict:
    result = {"zone": zone, "name": name, "server": "127.0.0.1"}
    for record, query in (("SOA", zone), ("A", name)):
        for protocol in ("udp", "tcp"):
            args = ["/usr/bin/dig", "@127.0.0.1", query, record, "+time=2", "+tries=1",
                    "+norecurse", "+noall", "+comments", "+answer"]
            if protocol == "tcp":
                args.append("+tcp")
            observed = command(args, timeout=4, limit=32768)
            if observed["status"] != "ok" or observed.get("returncode") != 0:
                value = unknown("loopback DNS query failed")
            else:
                try:
                    value = parse_dig(observed["output"])
                except ProbeError as exc:
                    value = unknown(str(exc))
            result[f"{record.lower()}_{protocol}"] = value
    return result


def parse_transaction(raw: bytes) -> dict:
    safe = {}
    seen = set()
    for line in raw.decode("ascii").splitlines():
        if "=" not in line:
            raise ProbeError("transaction descriptor is malformed")
        key, value = line.split("=", 1)
        if key in seen:
            raise ProbeError("transaction descriptor has duplicate fields")
        seen.add(key)
        # Tokens, lock identities and unrelated fields are deliberately omitted.
        if key in {"operation", "snapshot", "phase", "state"}:
            if not re.fullmatch(r"[A-Za-z0-9_.-]{1,256}", value):
                raise ProbeError("transaction observation field is unsafe")
            safe[key] = value
    if safe.get("operation") not in {"update", "rollback"} or "snapshot" not in safe:
        raise ProbeError("transaction identity is incomplete")
    return safe


def observe_transaction() -> dict:
    if not TRANSACTION.exists() and not TRANSACTION.is_symlink():
        return {"status": "ok", "present": False, "fields": {}}
    try:
        return {"status": "ok", "present": True, "fields": parse_transaction(bounded_file(TRANSACTION, 8192))}
    except (OSError, UnicodeError, ProbeError) as exc:
        return unknown(f"transaction observation failed: {exc.__class__.__name__}")


def journal_event(record: dict) -> dict | None:
    message = record.get("MESSAGE")
    unit = record.get("_SYSTEMD_UNIT", record.get("UNIT", ""))
    stamp = record.get("__REALTIME_TIMESTAMP")
    if not isinstance(message, str) or not isinstance(unit, str) or not str(stamp).isdigit():
        return None
    if not re.fullmatch(r"celikpanel-(?:release-recovery|self-update-[0-9a-f]{32})\.service", unit):
        return None
    # Emit only known public event text/fields, never arbitrary journal messages.
    lower = message.lower()
    if message.strip() == "==> Rollback complete / Geri alma tamamlandı":
        kind, safe = "rollback_completion_text", message.strip()
    elif "celikpanel_update_failure" in lower:
        kind = "update_failure"
        fields = re.findall(r"\b(code|state)=([a-z_]{1,64})\b", message)
        safe = "CELIKPANEL_UPDATE_FAILURE " + " ".join(f"{key}={value}" for key, value in fields)
    elif message.strip() in {
        "==> Stopping CelikPanel services / CelikPanel servisleri durduruluyor",
        "==> Resuming verified pending rollback / Doğrulanmış bekleyen geri alma sürdürülüyor",
    }:
        kind, safe = "recovery_transition", message.strip()
    else:
        return None
    return {"timestamp_us": str(stamp), "unit": unit, "kind": kind, "safe_message": safe}


def observe_journal(since: str, operation_id: str | None) -> dict:
    unit = f"celikpanel-self-update-{operation_id}.service" if operation_id else "celikpanel-self-update-*"
    result = command(["/usr/bin/journalctl", "-u", unit, "-u", "celikpanel-release-recovery.service",
                      "--since", since, "--no-pager", "-n", "250", "--output=json",
                      "--output-fields=MESSAGE,_SYSTEMD_UNIT,UNIT,__REALTIME_TIMESTAMP"], timeout=8, limit=262144)
    if result["status"] != "ok" or result.get("returncode") != 0:
        return unknown("bounded recovery journal could not be read")
    events, count = [], 0
    try:
        for line in result["output"].splitlines():
            if not line:
                continue
            count += 1
            record = strict_object(line.encode())
            event = journal_event(record)
            if event:
                events.append(event)
    except ProbeError:
        return unknown("recovery journal contains malformed records")
    return {"status": "ok", "truncated": count >= 250, "records_read": count,
            "operation_id": operation_id, "events": events}


def parse_since(value: str) -> str:
    try:
        stamp = dt.datetime.strptime(value, "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=dt.timezone.utc)
    except ValueError as exc:
        raise argparse.ArgumentTypeError("since must be UTC YYYY-MM-DDTHH:MM:SSZ") from exc
    if not 0 <= (dt.datetime.now(dt.timezone.utc) - stamp).total_seconds() <= 86400:
        raise argparse.ArgumentTypeError("since must be within the previous 24 hours")
    return stamp.strftime("%Y-%m-%d %H:%M:%S UTC")


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--lab-nonce", required=True)
    parser.add_argument("--vm-uuid", required=True)
    parser.add_argument("--cell-id", required=True)
    parser.add_argument("--node", choices=("debian13", "arch"), required=True)
    parser.add_argument("--zone", type=valid_dns_name, required=True)
    parser.add_argument("--name", type=valid_dns_name, required=True)
    parser.add_argument("--since", type=parse_since, required=True)
    parser.add_argument("--operation-id")
    args = parser.parse_args(argv)
    if args.operation_id and not HEX32.fullmatch(args.operation_id):
        parser.error("operation-id must be 32 lowercase hex characters")
    try:
        identity = guard_guest(args)
    except (ProbeError, UnicodeError) as exc:
        print(json.dumps({"schema": SCHEMA, "status": "refused", "reason": str(exc)}))
        return 2
    started = utc_now()
    result = {"schema": SCHEMA, "status": "observed", "identity": identity,
              "captured_at_utc": started,
              "services": {unit: collect_observation(observe_service, unit, path) for unit, path in SERVICES.items()},
              "database": collect_observation(observe_database), "web": collect_observation(observe_web), "tls": collect_observation(observe_tls),
              "dns": collect_observation(observe_dns, args.zone, args.name),
              "timers": {unit: collect_observation(service_properties, unit) for unit in TIMERS},
              "transaction": collect_observation(observe_transaction), "journal": collect_observation(observe_journal, args.since, args.operation_id),
              "completed_at_utc": utc_now()}
    print(json.dumps(result, sort_keys=True, indent=2))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
