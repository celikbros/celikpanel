#!/usr/bin/env python3
"""set6: set5's collection-time rules for digests of tokens, and the same rules inside base64 text.

set5's intake found the digest of an update-transaction token base64-encoded inside ``events_base64`` of a host-side
record (``recovery-fault-collection-*.json``): ``set5_redact`` and the secret scan read plain text only. This module
keeps set5's rules unchanged (``TokenDigests``, ``wrap``) and adds one pass:

  base64 text   every run of 40 or more base64 characters that decodes (strictly) to UTF-8 text is decoded, also when
                that text holds base64 text again (three levels at most), and passed through the same rules. Where
                the rules change the decoded text, the run is replaced by the base64 of the redacted text. A JSON
                object that was changed this way gets a member ``set6_base64_redaction`` that says so and names
                every ``*_sha256`` member of the same object, because a digest the record holds of the encoded or
                decoded text no longer matches the stored text.

A value is never printed, logged or stored by this module: only counts by class and file leave it.

  set6_redact.py sweep [--learn-from DIR]... DIR...   rewrite the files of DIR in place; print the counts as JSON
  set6_redact.py count [--learn-from DIR]... DIR...   read only: what the rules would still remove (expected: nothing)

set6: set5'in belirteç özeti kuralları, base64 metnin içinde de. Hiçbir değer yazdırılmaz; yalnız sayılar çıkar.
"""
from __future__ import annotations

import base64
import binascii
import json
import os
from pathlib import Path
import re
import sys
from typing import Any

HERE = Path(__file__).resolve().parent
if str(HERE) not in sys.path:
    sys.path.insert(0, str(HERE))
import set5_redact  # noqa: E402

MARK = set5_redact.MARK
TokenDigests = set5_redact.TokenDigests
wrap = set5_redact.wrap
files_of = set5_redact.files_of
BASE64_RUN = re.compile(r"(?<![A-Za-z0-9+/=])[A-Za-z0-9+/]{40,}={0,2}(?![A-Za-z0-9+/=])")
NOTE_KEY = "set6_base64_redaction"
MAX_DEPTH = 3


def decoded_text(run: str) -> str | None:
    """The UTF-8 text a base64 run holds, or None (not base64, not text, or mostly not printable)."""
    if len(run) % 4:
        return None
    try:
        raw = base64.b64decode(run, validate=True)
        text = raw.decode("utf-8")
    except (binascii.Error, ValueError, UnicodeDecodeError):
        return None
    if not text or sum(ch.isprintable() or ch in "\r\n\t" for ch in text) < 0.95 * len(text):
        return None
    return text


def learn_base64(rules: TokenDigests, text: str, depth: int = 0) -> None:
    """Remember the values that stand inside base64 text, without changing anything."""
    for match in BASE64_RUN.finditer(text):
        inner = decoded_text(match.group(0))
        if inner is None:
            continue
        rules.learn_text(inner)
        if depth + 1 < MAX_DEPTH:
            learn_base64(rules, inner, depth + 1)


def redact_base64(rules: TokenDigests, text: str, counter: dict, depth: int = 0) -> str:
    """``text`` with every base64 run whose decoded text the rules change replaced by the base64 of the redacted
    text. ``counter['places']`` counts the places changed inside decoded text; ``counter['runs']`` the runs replaced."""
    def one(match: re.Match) -> str:
        run = match.group(0)
        inner = decoded_text(run)
        if inner is None:
            return run
        before = sum(rules.counts.values())
        changed = rules.text(inner)
        if depth + 1 < MAX_DEPTH:
            changed = redact_base64(rules, changed, counter, depth + 1)
        if changed == inner:
            return run
        counter["places"] += max(1, sum(rules.counts.values()) - before)
        counter["runs"] += 1
        return base64.b64encode(changed.encode("utf-8")).decode("ascii")
    return BASE64_RUN.sub(one, text)


def would_remove_base64(rules: TokenDigests, text: str, depth: int = 0) -> int:
    """Read only: how many places inside base64 text the rules would still change."""
    found = 0
    for match in BASE64_RUN.finditer(text):
        inner = decoded_text(match.group(0))
        if inner is None:
            continue
        found += rules.would_remove(inner)
        if depth + 1 < MAX_DEPTH:
            found += would_remove_base64(rules, inner, depth + 1)
    return found


def note_objects(value: Any, before: Any) -> int:
    """Mark every JSON object one of whose own string members changed in the base64 pass. Returns how many."""
    marked = 0
    if isinstance(value, dict) and isinstance(before, dict):
        own = [key for key, item in value.items() if isinstance(item, str) and isinstance(before.get(key), str)
               and item != before.get(key)]
        for key, item in value.items():
            if key in before and not isinstance(item, str):
                marked += note_objects(item, before[key])
        if own:
            digests = sorted(key for key in value if key.endswith("_sha256"))
            value[NOTE_KEY] = {"members_changed": sorted(own),
                               "what": "base64 text in these members held the digest of a credential; it was decoded, the "
                                       "digest was replaced by " + MARK + " and the text was encoded again",
                               "digests_of_this_object_that_no_longer_match_the_stored_text": digests,
                               "note": "a *_sha256 member of this object that was computed over the original text is the "
                                       "digest of the text before the replacement"}
            marked += 1
    elif isinstance(value, list) and isinstance(before, list):
        for item, old in zip(value, before):
            marked += note_objects(item, old)
    return marked


def sweep(directories: list, rules: TokenDigests | None = None, *, write: bool = True) -> dict:
    """set5's sweep (plain text) and then the base64 pass over the same files. Every value is learned first, from
    plain and from decoded text, so that a value named in one file is removed from every other. A JSON or JSON-lines
    file must still parse afterwards."""
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
            learn_base64(rules, text)
            listed.append((directory, path))
    changed: dict[str, int] = {}
    inside: dict[str, int] = {}
    counter = {"places": 0, "runs": 0}
    objects = 0
    for directory, path in listed:
        text = path.read_bytes().decode("utf-8")
        before_counts = dict(rules.counts)
        plain = rules.text(text)
        plain_places = sum(rules.counts[name] - before_counts[name] for name in set5_redact.CLASSES)
        places = counter["places"]
        result = redact_base64(rules, plain, counter)
        key = str(path.relative_to(directory)).replace(os.sep, "/")
        if result != plain:
            inside[key] = counter["places"] - places
            if path.suffix == ".json":
                try:
                    document, old = json.loads(result), json.loads(plain)
                except ValueError:
                    document = None
                if document is not None:
                    marked = note_objects(document, old)
                    if marked:
                        objects += marked
                        result = json.dumps(document, indent=2, sort_keys=True) + "\n"
        if result == text:
            continue
        if plain != text:
            changed[key] = plain_places
        if write:
            if path.suffix == ".json":
                json.loads(result)
            elif path.suffix == ".jsonl":
                for line in result.splitlines():
                    if line.strip():
                        json.loads(line)
            path.write_bytes(result.encode("utf-8"))
    return {"schema": "celikpanel/set6-digest-sweep/v1", "mark": MARK, "files_read": len(listed),
            "files_changed" if write else "files_that_would_change": changed,
            "files_changed_inside_base64_text" if write else "files_that_would_change_inside_base64_text": inside,
            "places_by_class": dict(rules.counts), "places_inside_base64_text": counter["places"],
            "base64_runs_replaced": counter["runs"], "json_objects_marked": objects,
            "distinct_values_known": rules.learned, "rewritten_in_place": write,
            "base64_rule": "runs of 40 or more base64 characters that decode strictly to UTF-8 text, three levels deep",
            "note": "counts only; no removed value is recorded anywhere"}


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
                    text = path.read_bytes().decode("utf-8")
                except (UnicodeDecodeError, OSError):
                    continue
                rules.learn_text(text)
                learn_base64(rules, text)
        else:
            directories.append(Path(item))
    report = sweep(directories, rules, write=mode == "sweep")
    print(json.dumps(report, indent=2, sort_keys=True))
    if mode == "sweep":
        return 0
    return 0 if not report["files_that_would_change"] and not report["files_that_would_change_inside_base64_text"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
