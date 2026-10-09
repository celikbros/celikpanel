#!/usr/bin/env python3
"""set5: collection-time rules for digests of credentials and tokens, beside the pair redactor and set3's shape rules.

set3 and set4 retained ``transaction_token_sha256`` (the digest of a one-shot update-transaction token) and the same
digest as a directory name under ``.release-db-migrations/`` in a sudo journal line: the pair redactor treats a key
that ends in ``_sha256`` as public, and a path is not a key at all. set4 had to be redacted after collection.

Rules (every one keeps the key or the path prefix and replaces only the value):

  token-named field   a value of 32 to 128 hexadecimal characters (optionally ``sha256:``-prefixed) under a key whose
                      name contains ``token``: JSON members, ``key=value`` and ``key: value`` pairs, also inside a
                      JSON string that was escaped once or twice;
  migration directory the hexadecimal directory name directly under ``.release-db-migrations/``;
  learned value       every value the two rules above have removed is remembered by this process and removed wherever
                      it appears again (a journal line, a path, a command line), whatever stands before it.

A value is never printed, logged or stored by this module: only counts by class and file leave it.

  set5_redact.py sweep [--learn-from DIR]... DIR...   rewrite the files of DIR in place; print the counts as JSON
  set5_redact.py count DIR...                         read only: what the rules would still remove (expected: nothing)

set5: kimlik bilgisi ve belirteç özetleri için toplama anı kuralları. Hiçbir değer yazdırılmaz; yalnız sayılar çıkar.
"""
from __future__ import annotations

import json
import os
from pathlib import Path
import re
import sys
from typing import Any, Mapping

MARK = "[REDACTED-SHA256]"
HEX = r"[0-9a-fA-F]{32,128}"
TOKEN_KEY = re.compile(r"(?i)token")
# key (containing "token"), then up to two levels of escaped quote, a colon or an equals sign, an optional quote and an
# optional "sha256:" prefix, then the digest. The value must end at a non-hexadecimal character.
TOKEN_PAIR = re.compile(r"(?i)([A-Za-z0-9_.-]*token[A-Za-z0-9_.-]*)((?:\\{0,3}[\"'])?\s*[:=]\s*(?:\\{0,3}[\"'])?(?:sha256:)?)"
                        r"(" + HEX + r")(?![0-9a-fA-F])")
MIGRATION_DIR = re.compile(r"(\.release-db-migrations/)(" + HEX + r")(?![0-9a-fA-F])")
DIGEST_VALUE = re.compile(r"(?:sha256:)?(" + HEX + r")\Z")
# H46 (set5, first cell): the first class was called "token_named_field"; the pair redactor blanks the value of any
# JSON key whose name contains "token", so the count itself read "[REDACTED]" in the sweep report.
CLASSES = ("named_field", "migration_directory", "learned_value")


class TokenDigests:
    """The rules with their memory of removed values. ``counts`` holds numbers only."""

    def __init__(self) -> None:
        self._values: set[str] = set()
        self.counts = {name: 0 for name in CLASSES}

    @property
    def learned(self) -> int:
        return len({value.lower() for value in self._values})

    def _learn(self, value: str) -> None:
        self._values.add(value)
        self._values.add(value.lower())

    def learn_text(self, text: str) -> None:
        """Remember the values of a text without changing it (used on a lab's own raw files before a sweep)."""
        for match in TOKEN_PAIR.finditer(text):
            self._learn(match.group(3))
        for match in MIGRATION_DIR.finditer(text):
            self._learn(match.group(2))

    def text(self, text: str) -> str:
        def pair(match: re.Match) -> str:
            self._learn(match.group(3))
            self.counts["named_field"] += 1
            return match.group(1) + match.group(2) + MARK

        def directory(match: re.Match) -> str:
            self._learn(match.group(2))
            self.counts["migration_directory"] += 1
            return match.group(1) + MARK

        text = TOKEN_PAIR.sub(pair, text)
        text = MIGRATION_DIR.sub(directory, text)
        for value in sorted(self._values, key=len, reverse=True):
            found = text.count(value)
            if found:
                self.counts["learned_value"] += found
                text = text.replace(value, MARK)
        return text

    def value(self, value: Any) -> Any:
        """A JSON value: a digest under a token-named key goes (and is learned); every string passes ``text``."""
        if isinstance(value, str):
            return self.text(value)
        if isinstance(value, Mapping):
            result = {}
            for key, item in value.items():
                name = str(key)
                if TOKEN_KEY.search(name) and isinstance(item, str) and DIGEST_VALUE.match(item.strip()):
                    self._learn(DIGEST_VALUE.match(item.strip()).group(1))
                    self.counts["named_field"] += 1
                    result[name] = MARK
                else:
                    result[name] = self.value(item)
            return result
        if isinstance(value, (list, tuple)):
            return [self.value(item) for item in value]
        return value

    def would_remove(self, text: str) -> int:
        """Read only: how many places the rules would still change in ``text``."""
        return (len(TOKEN_PAIR.findall(text)) + len(MIGRATION_DIR.findall(text))
                + sum(text.count(value) for value in self._values))


def wrap(redactor: Any, rules: TokenDigests | None = None) -> tuple:
    """The driver's redactor with these rules in front of every text and JSON value it handles."""
    rules = rules or TokenDigests()
    plain_text, plain_value = redactor.text, redactor.value
    redactor.text = lambda value: plain_text(rules.text(value))
    redactor.value = lambda value: plain_value(rules.value(value))
    return redactor, rules


def files_of(directory: Path) -> list:
    return sorted(path for path in directory.rglob("*") if path.is_file() and not path.is_symlink())


def sweep(directories: list, rules: TokenDigests | None = None, *, write: bool = True) -> dict:
    """Every file of ``directories`` through the rules. First all files are read once so that every value is known
    (a value named in one file is removed from every other); then each file is judged, and with ``write`` rewritten
    in place when it changes. A JSON file must still parse afterwards."""
    rules = rules or TokenDigests()
    listed = []
    for directory in directories:
        directory = Path(directory)
        for path in files_of(directory):
            try:
                text = path.read_bytes().decode("utf-8")
            except UnicodeDecodeError:
                continue
            rules.learn_text(text)
            listed.append((directory, path))
    changed: dict[str, int] = {}
    for directory, path in listed:
        text = path.read_bytes().decode("utf-8")
        before = dict(rules.counts)
        result = rules.text(text)
        if result == text:
            continue
        key = str(path.relative_to(directory)).replace(os.sep, "/")
        changed[key] = sum(rules.counts[name] - before[name] for name in CLASSES)
        if write:
            if path.suffix == ".json":
                json.loads(result)
            path.write_bytes(result.encode("utf-8"))
    return {"schema": "celikpanel/set5-token-digest-sweep/v1", "mark": MARK, "files_read": len(listed),
            "files_changed" if write else "files_that_would_change": changed,
            "places_by_class": dict(rules.counts), "distinct_values_known": rules.learned,
            "rewritten_in_place": write, "note": "counts only; no removed value is recorded anywhere"}


def main(argv: list | None = None) -> int:
    argv = list(sys.argv[1:] if argv is None else argv)
    if not argv or argv[0] not in ("sweep", "count"):
        print(__doc__, file=sys.stderr)
        return 2
    mode, rest = argv[0], argv[1:]
    rules = TokenDigests()
    directories = []
    while rest:
        item = rest.pop(0)
        if item == "--learn-from":
            source = Path(rest.pop(0))
            for path in files_of(source) if source.is_dir() else [source]:
                try:
                    rules.learn_text(path.read_bytes().decode("utf-8"))
                except (UnicodeDecodeError, OSError):
                    continue
        else:
            directories.append(Path(item))
    report = sweep(directories, rules, write=mode == "sweep")
    print(json.dumps(report, indent=2, sort_keys=True))
    return 0 if mode == "sweep" or not report["files_that_would_change"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
