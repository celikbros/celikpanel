#!/bin/bash
# set4b: the failing set of a `go test -json` log by name (package/test), and its difference from the baseline's.
# usage: failset.sh LOG.json OUT.txt [BASELINE.txt]
grep '"Action":"fail"' "$1" | sed -n 's/.*"Package":"\([^"]*\)".*"Test":"\([^"]*\)".*/\1 \2/p; /"Test"/!s/.*"Package":"\([^"]*\)".*/\1 (package)/p' | sort -u > "$2"
echo "fail entries: $(wc -l < "$2"); pass events: $(grep -c '"Action":"pass"' "$1")"
[ -n "${3:-}" ] && { diff "$3" "$2" > "$2.diff" && echo "same failing set as the baseline" || { echo "DIFFERENT from the baseline:"; cat "$2.diff"; }; }
