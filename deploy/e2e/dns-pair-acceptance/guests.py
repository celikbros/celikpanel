"""Host-side access to the two disposable kill-matrix guests.

Everything goes through ``fixture.py``'s exact SSH policy (loopback-forwarded
management port, per-cell known-hosts file, BatchMode) and its guest identity
check: before any command that changes guest state, the guest must report the
plan's SMBIOS UUID, the cloud-init fixture marker (schema, cell ID, node) and
a boot ID, exactly like ``fixture.py reboot``. Anything else is refused.
"""

from __future__ import annotations

import contextlib
import json
import shlex
import socket
import subprocess
import sys
import time
from pathlib import Path
from typing import Any, Callable, Iterator

HERE = Path(__file__).resolve().parent
KILL_MATRIX = HERE.parent / "dns-kill-matrix"
if str(KILL_MATRIX) not in sys.path:
    sys.path.insert(0, str(KILL_MATRIX))

import fixture  # noqa: E402  (kill-matrix fixture lifecycle, reused unmodified)

PROBE = HERE / "guest_probe.py"
PANEL_PORT = 2083


class GuestError(RuntimeError):
    pass


def probe_bytes() -> bytes:
    data = PROBE.read_bytes().replace(b"\r\n", b"\n")
    if b"\r" in data or not data.startswith(b"#!"):
        raise GuestError("guest_probe.py has unsupported line endings")
    return data


class Guests:
    """SSH command runner bound to one validated fixture plan."""

    def __init__(
        self,
        plan: dict[str, Any],
        identity_file: Path,
        *,
        runner: Callable[..., Any] = subprocess.run,
        popen: Callable[..., Any] = subprocess.Popen,
    ) -> None:
        self.plan = plan
        self.identity = identity_file
        self.runner = runner
        self.popen = popen
        self.verified: dict[str, dict[str, str]] = {}

    # -- identity -----------------------------------------------------------

    def verify(self, node: str) -> dict[str, str]:
        """Refuse to act unless ``node`` is this cell's own disposable guest."""

        if node not in self.plan.get("nodes", {}):
            raise GuestError(f"fixture plan has no node {node!r}")
        observed = fixture.observe_guest_identity(self.plan, node, self.identity, self.runner)
        fixture.verify_guest_identity(self.plan, node, observed)
        self.verified[node] = observed
        return observed

    # -- commands -------------------------------------------------------------

    def argv(self, node: str, command: str) -> list[str]:
        return fixture.remote_command(self.plan["nodes"][node], self.identity, command)

    def run(
        self,
        node: str,
        command: str,
        *,
        mutating: bool,
        stdin: bytes | None = None,
        timeout: float = 120,
        check: bool = True,
    ) -> subprocess.CompletedProcess:
        if mutating:
            self.verify(node)
        completed = self.runner(
            self.argv(node, command),
            input=stdin,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=False,
            timeout=timeout,
        )
        if check and completed.returncode != 0:
            detail = (completed.stderr or b"")[-400:]
            if isinstance(detail, bytes):
                detail = detail.decode("utf-8", "replace")
            raise GuestError(f"{node}: {command.split()[0:3]} exited {completed.returncode}: {detail}")
        return completed

    def probe(self, node: str, *arguments: str, timeout: float = 90) -> dict[str, Any]:
        """Run guest_probe.py read-only as root, streamed on stdin."""

        command = "sudo -n /usr/bin/python3 - " + " ".join(shlex.quote(value) for value in arguments)
        completed = self.run(node, command, mutating=False, stdin=probe_bytes(), timeout=timeout, check=False)
        raw = completed.stdout.decode("utf-8", "replace") if isinstance(completed.stdout, bytes) else completed.stdout
        try:
            value = json.loads(raw.strip().splitlines()[-1]) if raw.strip() else {}
        except (ValueError, IndexError):
            value = {"error": "probe output is not JSON", "stdout_tail": raw[-400:]}
        value.setdefault("returncode", completed.returncode)
        return value

    def journal(self, node: str, unit: str, *, boots: str = "all") -> str:
        scope = "" if boots == "all" else " -b"
        completed = self.run(
            node,
            f"sudo -n /usr/bin/journalctl --no-pager -o short-iso-precise{scope} -u {shlex.quote(unit)}",
            mutating=False,
            timeout=60,
            check=False,
        )
        text = completed.stdout.decode("utf-8", "replace") if isinstance(completed.stdout, bytes) else completed.stdout
        return text

    # -- management lifecycle (D-022 independence) ----------------------------

    def disable_management(self, node: str) -> subprocess.CompletedProcess:
        return self.run(
            node,
            "sudo -n /usr/bin/systemctl disable --now celikpanel-panel.service celikpanel-agent.service",
            mutating=True,
        )

    def enable_management(self, node: str) -> subprocess.CompletedProcess:
        # Agent first: the Panel needs its socket, as the installer starts them.
        return self.run(
            node,
            "sudo -n /usr/bin/systemctl enable --now celikpanel-agent.service celikpanel-panel.service",
            mutating=True,
        )

    def reboot(self, node: str, timeout: int = 900) -> dict[str, Any]:
        # fixture.reboot_guest repeats the UUID/marker check itself.
        return fixture.reboot_guest(self.plan, node, self.identity, timeout, runner=self.runner)

    # -- panel access ---------------------------------------------------------

    @contextlib.contextmanager
    def tunnel(self, node: str, local_port: int) -> Iterator[str]:
        """Loopback-only SSH port forward to the guest panel (127.0.0.1:2083)."""

        base = fixture.ssh_command(self.plan["nodes"][node], self.identity)[:-1]
        argv = base[:1] + [
            "-N",
            "-o",
            "ExitOnForwardFailure=yes",
            "-L",
            f"127.0.0.1:{local_port}:127.0.0.1:{PANEL_PORT}",
        ] + base[1:]
        process = self.popen(argv, stdin=subprocess.DEVNULL, stdout=subprocess.DEVNULL, stderr=subprocess.PIPE)
        try:
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                if process.poll() is not None:
                    raise GuestError(f"SSH tunnel to {node} exited {process.returncode}")
                try:
                    with socket.create_connection(("127.0.0.1", local_port), timeout=1):
                        break
                except OSError:
                    time.sleep(0.25)
            else:
                raise GuestError(f"SSH tunnel to {node} did not open")
            yield f"https://127.0.0.1:{local_port}"
        finally:
            process.terminate()
            try:
                process.wait(timeout=10)
            except subprocess.TimeoutExpired:
                process.kill()
