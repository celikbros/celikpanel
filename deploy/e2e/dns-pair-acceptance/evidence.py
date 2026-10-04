"""Create-new evidence tree for one DNS pair acceptance run.

Layout (compatible with the kill-matrix evidence directories: one directory
per run, plain JSON/text files, a ``README``-free machine result and a
``SHA256SUMS`` over every retained file)::

    <evidence-root>/<run-id>/
      run.json                      inputs: topology, placement, artifacts, source
      steps/<NN>-<step>/api/<seq>-<method>-<slug>.json   redacted request/response
      steps/<NN>-<step>/dns-*.json                        UDP/TCP answers, both servers
      steps/<NN>-<step>/native-<node>.json                native loaded-zone state
      steps/<NN>-<step>/guidance.json                     what the Panel showed
      guests/<node>/journal-<unit>.txt, versions.json, identity-*.json
      result.json                   per-step verdicts and the overall status
      SHA256SUMS                    sha256 of every file above, sorted

Every write is create-new (``O_EXCL | O_NOFOLLOW``) and goes through the run's
:class:`redaction.Redactor` first; a registered secret that survives
redaction aborts the write rather than reaching disk.
"""

from __future__ import annotations

import datetime as dt
import hashlib
import json
import os
import re
from pathlib import Path
from typing import Any

from redaction import Redactor

RESULT_SCHEMA = "celikpanel/dns-pair-acceptance-result/v1"
RUN_SCHEMA = "celikpanel/dns-pair-acceptance-run/v1"
COMPONENT_RE = re.compile(r"[A-Za-z0-9][A-Za-z0-9._-]{0,127}")
RUN_ID_RE = re.compile(r"[a-z0-9][a-z0-9._-]{2,95}")
SLUG_RE = re.compile(r"[^a-z0-9]+")

# Step verdicts. "refused-by-product-gate" is a truthful product outcome, not
# a harness failure; "blocked-product" names a missing product capability the
# driver will not fake (for example an owner enrollment tool that does not
# exist for the secondary engine).
VERDICTS = (
    "passed",
    "failed",
    "refused-by-product-gate",
    "blocked-product",
    "skipped",
    "not-run",
)
OVERALL = ("passed", "failed", "refused-by-product-gate", "blocked-product", "incomplete")


class EvidenceError(RuntimeError):
    pass


RUN_TIMESTAMP_FORMAT = "%Y%m%dt%H%M%Sz"  # lowercase t/z: RUN_ID_RE admits no uppercase


def make_run_id(run_label: str, when: dt.datetime) -> str:
    """``<label>-<UTC yyyymmddThhmmssZ in lowercase>``; the one producer of run IDs.

    pair1 (H1): the driver produced ``...T...Z`` and ``EvidenceWriter`` refused
    it before any guest was touched. Producer and checker now share this rule.
    """

    if when.tzinfo is None:
        raise EvidenceError("run timestamp must be timezone-aware")
    run_id = f"{run_label}-{when.astimezone(dt.timezone.utc).strftime(RUN_TIMESTAMP_FORMAT)}"
    if RUN_ID_RE.fullmatch(run_id) is None:
        raise EvidenceError(f"invalid run id: {run_id!r}")
    return run_id


def slug(value: str, limit: int = 48) -> str:
    text = SLUG_RE.sub("-", value.lower()).strip("-")
    return (text or "x")[:limit].strip("-") or "x"


def safe_relative(relative: str) -> Path:
    parts = relative.split("/")
    if not parts or any(COMPONENT_RE.fullmatch(part) is None or part in {".", ".."} for part in parts):
        raise EvidenceError(f"unsafe evidence path: {relative!r}")
    return Path(*parts)


def overall_status(verdicts: list[str]) -> str:
    """Fold per-step verdicts into one overall status.

    A verified failure dominates; then a product blocker; then a product gate
    refusal (a valid outcome for that topology); ``incomplete`` when a step
    never ran; ``passed`` only when every executed step passed.
    """

    for verdict in verdicts:
        if verdict not in VERDICTS:
            raise EvidenceError(f"unknown verdict: {verdict}")
    if not verdicts:
        return "incomplete"
    if "failed" in verdicts:
        return "failed"
    if "blocked-product" in verdicts:
        return "blocked-product"
    if "refused-by-product-gate" in verdicts:
        return "refused-by-product-gate"
    if "not-run" in verdicts:
        return "incomplete"
    if all(verdict in {"passed", "skipped"} for verdict in verdicts) and "passed" in verdicts:
        return "passed"
    return "incomplete"


class EvidenceWriter:
    def __init__(self, root: Path, run_id: str, redactor: Redactor) -> None:
        if RUN_ID_RE.fullmatch(run_id) is None:
            raise EvidenceError(f"invalid run id: {run_id!r}")
        root = Path(root)
        if not root.is_absolute():
            raise EvidenceError("evidence root must be absolute")
        if root.is_symlink() or not root.is_dir():
            raise EvidenceError(f"evidence root must be an existing real directory: {root}")
        self.redactor = redactor
        self.directory = root / run_id
        # Fails if the run directory exists: evidence is never merged or reused.
        os.mkdir(self.directory, 0o700)
        self._sequence = 0
        self._finalized = False
        self.files: list[str] = []

    # -- low level --------------------------------------------------------

    def _path(self, relative: str) -> Path:
        if self._finalized:
            raise EvidenceError("evidence is finalized; no further writes")
        path = self.directory / safe_relative(relative)
        current = self.directory
        for part in path.relative_to(self.directory).parts[:-1]:
            current = current / part
            if current.is_symlink():
                raise EvidenceError(f"evidence directory is a symlink: {current}")
            if not current.exists():
                os.mkdir(current, 0o700)
        return path

    def _write_bytes(self, relative: str, data: bytes) -> str:
        text = data.decode("utf-8", errors="replace")
        if self.redactor.contains_secret(text):
            raise EvidenceError(f"refusing to write a registered secret to {relative}")
        path = self._path(relative)
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
        descriptor = os.open(path, flags, 0o600)
        with os.fdopen(descriptor, "wb") as handle:
            handle.write(data)
            handle.flush()
            os.fsync(handle.fileno())
        self.files.append(relative)
        return relative

    # -- public writers ---------------------------------------------------

    def write_json(self, relative: str, value: Any) -> str:
        redacted = self.redactor.value(value)
        data = (json.dumps(redacted, indent=2, sort_keys=True, ensure_ascii=False) + "\n").encode()
        return self._write_bytes(relative, data)

    def write_text(self, relative: str, text: str) -> str:
        redacted = self.redactor.text(text)
        if not redacted.endswith("\n"):
            redacted += "\n"
        return self._write_bytes(relative, redacted.replace("\r\n", "\n").encode("utf-8"))

    def next_sequence(self) -> int:
        self._sequence += 1
        return self._sequence

    def api_exchange(self, step_dir: str, exchange: dict[str, Any]) -> str:
        """Record one HTTP request/response pair (redacted at capture time)."""

        sequence = self.next_sequence()
        request = exchange.get("request", {})
        name = (
            f"{step_dir}/api/{sequence:04d}-{slug(str(request.get('method', 'x')), 8)}"
            f"-{slug(str(request.get('path', 'x')))}.json"
        )
        return self.write_json(name, exchange)

    def finalize(self, result: dict[str, Any]) -> dict[str, Any]:
        """Write ``result.json`` and ``SHA256SUMS``; the tree is then closed."""

        if result.get("schema") != RESULT_SCHEMA:
            raise EvidenceError("result.json must carry the result schema")
        verdicts = [step["verdict"] for step in result.get("steps", [])]
        expected = overall_status(verdicts)
        if result.get("overall") != expected:
            raise EvidenceError(
                f"overall status {result.get('overall')!r} disagrees with step verdicts ({expected!r})"
            )
        self.write_json("result.json", result)
        lines = []
        for relative in sorted(self.files):
            digest = hashlib.sha256((self.directory / safe_relative(relative)).read_bytes()).hexdigest()
            lines.append(f"{digest}  {relative}\n")
        sums = self._path("SHA256SUMS")
        flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL | getattr(os, "O_NOFOLLOW", 0)
        descriptor = os.open(sums, flags, 0o600)
        with os.fdopen(descriptor, "w", encoding="ascii", newline="\n") as handle:
            handle.writelines(lines)
        self._finalized = True
        return result


def verify_sums(directory: Path) -> list[str]:
    """Re-hash a finalized run; returns the list of mismatching/missing files."""

    problems: list[str] = []
    listed: set[str] = set()
    for line in (directory / "SHA256SUMS").read_text(encoding="ascii").splitlines():
        digest, _, relative = line.partition("  ")
        listed.add(relative)
        path = directory / safe_relative(relative)
        if not path.is_file() or hashlib.sha256(path.read_bytes()).hexdigest() != digest:
            problems.append(relative)
    for path in directory.rglob("*"):
        if path.is_file() and path.name != "SHA256SUMS":
            relative = path.relative_to(directory).as_posix()
            if relative not in listed:
                problems.append(f"unlisted:{relative}")
    return problems
