# set6: put the README together: the text is written by hand (README.source.md); the tables that only repeat values of
# the runs are taken from the files tools/summary.py generated. usage: mkreadme.py EVIDENCE_DIR README.source.md OUT
import re
import sys

E, source, out = sys.argv[1].rstrip("/"), sys.argv[2], sys.argv[3]


def tables(name, start=None, stop=None):
    """The table lines (and the `##` headings between them) of a generated file, optionally between two headings."""
    lines = open(f"{E}/{name}", encoding="utf-8").read().splitlines()
    if start:
        index = next(i for i, line in enumerate(lines) if line.startswith(start))
        lines = lines[index + 1:]
    if stop:
        index = next((i for i, line in enumerate(lines) if line.startswith(stop)), len(lines))
        lines = lines[:index]
    kept = [line for line in lines if line.startswith("|")]
    return "\n".join(kept)


parts = {
    "CELLS": tables("cells.md"),
    "PART_A": tables("part-a-table.md"),
    "PART_B_FRESH": tables("part-b-headers.md", "## Fresh-install cells", "## Update cells"),
    "PART_B_UPDATE": tables("part-b-headers.md", "## Update cells", "## The readings in short"),
    "PART_B_SHORT": tables("part-b-headers.md", "## The readings in short"),
    "EXCHANGES": tables("hsts-in-recorded-exchanges.md"),
    "UPDATE": tables("update-cells.md", None, "## "),
    "PINNING": tables("pinning.md", None, "## "),
    "PACKAGES": tables("packages.md"),
}
text = open(source, encoding="utf-8").read().replace("\r\n", "\n")
missing = [name for name in re.findall(r"@@([A-Z_]+)@@", text) if name not in parts]
if missing:
    raise SystemExit("unknown placeholders: " + ", ".join(sorted(set(missing))))
for name, value in parts.items():
    text = text.replace(f"@@{name}@@", value)
with open(out, "w", encoding="utf-8", newline="\n") as stream:
    stream.write(text if text.endswith("\n") else text + "\n")
print("README written:", len(text.splitlines()), "lines;", sum(1 for name in parts if f"@@{name}@@" in open(source, encoding="utf-8").read()), "of", len(parts), "tables placed")
