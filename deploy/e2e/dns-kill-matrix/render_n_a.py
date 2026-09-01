#!/usr/bin/env python3
"""Render or verify the S-1 explicit N/A-cell appendix deterministically."""

from __future__ import annotations

import argparse
import hashlib
import json
import sys
from pathlib import Path
from typing import Any

import generate_manifest as matrix


def load_canonical_manifest(path: Path) -> tuple[dict[str, Any], bytes]:
    """Load a manifest and reject bytes that are not canonical generator output."""

    raw = path.read_bytes()
    manifest = json.loads(raw.decode("utf-8"))
    matrix.validate_manifest(manifest)
    expected = matrix.encoded_manifest(manifest).encode("utf-8")
    if raw != expected:
        raise ValueError(
            f"{path} is valid JSON but is not canonical deterministic manifest output"
        )
    return manifest, raw


def encoded_n_a_appendix(
    manifest: dict[str, Any], manifest_sha256: str
) -> str:
    """Return the complete ordered N/A appendix as LF-only UTF-8 text."""

    matrix.validate_manifest(manifest)
    n_a_cells = [cell for cell in manifest["cells"] if cell["status"] == "n/a"]
    lines = [
        "# S-1 explicit N/A cells",
        "",
        f"Manifest SHA-256: `{manifest_sha256}`",
        "",
        f"Count: {len(n_a_cells)}",
        "",
    ]
    for cell in n_a_cells:
        lines.extend((f"## {cell['id']}", ""))
        for reason in cell["n_a"]["reasons"]:
            lines.append(f"- {reason['code']}: {reason['detail']}")
            for evidence in reason["evidence"]:
                lines.append(
                    "  - "
                    f"{evidence['file_line']} ({evidence['symbol']}): "
                    f"{evidence['claim']}"
                )
        lines.append("")
    return "\n".join(lines)


def render_from_manifest(path: Path) -> tuple[str, str, int]:
    manifest, raw = load_canonical_manifest(path)
    digest = hashlib.sha256(raw).hexdigest()
    rendered = encoded_n_a_appendix(manifest, digest)
    return rendered, digest, manifest["summary"]["n_a"]


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument(
        "--manifest",
        type=Path,
        default=Path(__file__).with_name("manifest.json"),
        help="canonical generated manifest (default: sibling manifest.json)",
    )
    destination = parser.add_mutually_exclusive_group()
    destination.add_argument(
        "--output",
        type=Path,
        help="write the rendered appendix atomically instead of stdout",
    )
    destination.add_argument(
        "--check",
        type=Path,
        metavar="APPENDIX",
        help="fail unless APPENDIX exactly matches the deterministic rendering",
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    rendered, digest, count = render_from_manifest(args.manifest)
    if args.check is not None:
        try:
            actual = args.check.read_bytes()
        except OSError as exc:
            print(f"dns-kill-matrix N/A appendix check failed: {exc}", file=sys.stderr)
            return 1
        expected = rendered.encode("utf-8")
        if actual != expected:
            print(
                "dns-kill-matrix N/A appendix check failed: "
                f"{args.check} differs from manifest sha256={digest}",
                file=sys.stderr,
            )
            return 1
        print(
            "dns-kill-matrix N/A appendix: "
            f"ok count={count} manifest_sha256={digest}"
        )
        return 0
    if args.output is None:
        print(rendered, end="")
    else:
        matrix.write_atomic(args.output, rendered)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
