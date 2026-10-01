#!/usr/bin/env bash
# Refuse release artifacts that carry development or test material the
# installed product never reads (the rule set of prune-release-harness.sh),
# paths relative to the release root:
#   - any path under a directory named "evidence",
#   - any path under deploy/e2e (acceptance harnesses),
#   - any deploy/test-* entry (contract tests, any extension),
#   - anywhere: *_test.go, *_test.sh, test_*.py, *.test.mjs, and any path
#     under a __pycache__ directory.
#
# usage: release-content-guard.sh PATH...
#   PATH: a release tree directory (its root is the release root) or a
#   .tar.gz release archive (its first path component is the release root).
# Exit 0: nothing found. Exit 1: refused. Exit 2: usage or inspection error.
# Contacts nothing and changes nothing.
set -euo pipefail
LC_ALL=C
export LC_ALL

usage() {
    echo "usage: release-content-guard.sh PATH..." >&2
    exit 2
}

refused=0
# check_paths LABEL PATHS: PATHS holds release-root-relative paths, one per line.
check_paths() {
    local label=$1 found count
    found=$(awk '
        { p = $0; sub(/^\.\//, "", p); sub(/\/$/, "", p) }
        p == "" || p == "." { next }
        p ~ /(^|\/)evidence(\/|$)/ { print "evidence: " p; next }
        p ~ /^deploy\/e2e(\/|$)/ { print "acceptance harness: " p; next }
        p ~ /^deploy\/test-[^\/]*(\/|$)/ { print "contract test: " p; next }
        p ~ /(^|\/)__pycache__(\/|$)/ { print "bytecode cache: " p; next }
        p ~ /(^|\/)[^\/]*_test\.(go|sh)$/ || p ~ /(^|\/)test_[^\/]*\.py$/ || p ~ /(^|\/)[^\/]*\.test\.mjs$/ { print "test file: " p }
    ' <<< "$2")
    [[ -n "$found" ]] || return 0
    refused=1
    count=$(wc -l <<< "$found" | tr -d ' ')
    printf 'refused: %s%s path(s) that no installed server uses, first:\n' "$label" "$count" >&2
    head -n 20 <<< "$found" | sed 's/^/  /' >&2
}

[[ $# -ge 1 ]] || usage
for target in "$@"; do
    if [[ -L "$target" ]]; then
        echo "refusing a symbolic link: $target" >&2
        exit 2
    elif [[ -d "$target" ]]; then
        paths=$(cd -- "$target" && find . -xdev -print) || { echo "cannot list release tree: $target" >&2; exit 2; }
        check_paths "$target: " "$paths"
    elif [[ -f "$target" && "$target" == *.tar.gz ]]; then
        listing=$(tar -tzf "$target") || { echo "cannot list archive: $target" >&2; exit 2; }
        # Strip the release root directory (the first path component).
        check_paths "$(basename -- "$target"): " "$(sed -E 's#^(\./)?[^/]+/?##' <<< "$listing")"
    else
        echo "not a release tree or .tar.gz archive: $target" >&2
        exit 2
    fi
done
if [[ "$refused" -ne 0 ]]; then
    printf '%s\n' \
        "Why: the release contains development or test material (acceptance harnesses, evidence, contract tests) that no installed server uses." \
        "Nothing was packaged or signed." \
        "Next: build the release with make dist, which prunes them (deploy/prune-release-harness.sh), then package again." >&2
    exit 1
fi
exit 0
