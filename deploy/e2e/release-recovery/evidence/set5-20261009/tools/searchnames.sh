#!/bin/bash
# set5 read-only: does a name or address of an installed server appear in the staged evidence? The terms are the ones
# set4's README lists. Searched: every file of the evidence folder except this report, the README, its template and
# parts, and this script (which name the terms themselves). This is an absence of names, not a capture of traffic.
# usage: searchnames.sh EVIDENCE_DIR   -> writes EVIDENCE_DIR/host/installed-server-names-search.txt
E=$1
out="$E/host/installed-server-names-search.txt"
{
  echo "searched at $(date -u +%FT%TZ): every file under the evidence folder except README.md, tools/README.template.md,"
  echo "tools/readme-parts/, tools/searchnames.sh and this report; case-insensitive, fixed strings"
  for term in celikhost boston frankfurt 2.25.80.4 72.62.38.15 185.95.0.123; do
    files=$(grep -rilF -- "$term" "$E" 2>/dev/null | grep -v -E '/README\.md$|/tools/README\.template\.md$|/tools/readme-parts/|/tools/searchnames\.sh$|/host/installed-server-names-search\.txt$' || true)
    n=$(printf '%s' "$files" | grep -c . || true)
    echo "term \"$term\": $n file(s)"
    printf '%s\n' "$files" | sed "s#^$E/#    #" | grep . | head -n 12
  done
} > "$out"
cat "$out"
