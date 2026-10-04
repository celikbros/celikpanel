#!/usr/bin/env bash
# Refuse release artifacts that contain a program built with the
# acceptance_license test tag. That build accepts a fixture license on a
# disposable acceptance guest (deploy/e2e/dns-pair-acceptance) and is never a
# CelikPanel release. It is produced only by
# deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh --acceptance-license.
#
# usage: release-acceptance-license-guard.sh PATH...
#   PATH: a release tree directory, a .tar.gz release archive, or one file.
# Exit 0: nothing found. Exit 1: refused. Exit 2: usage or inspection error.
#
# Every ELF file is checked, with no Go toolchain required, for
#   1. the Go build settings embedded in the binary: a "build<TAB>-tags=..."
#      module-info line naming acceptance_license;
#   2. the fixture license text compiled into the tagged panel.
# When a Go toolchain is available (CELIKPANEL_GUARD_GO, or go on PATH),
# `go version -m` must agree. Text files that merely mention these strings
# (documentation, this script, the acceptance driver) are not builds.
set -euo pipefail
LC_ALL=C
export LC_ALL

tag=acceptance_license
# Assembled from parts so no text file carries the compiled fixture strings.
holder="ACCEPTANCE FIXTURE $(printf '\342\200\224') NOT FOR PRODUCTION"
receipt_format="celikpanel-acceptance-fixture-license""/v1"
tags_line=$(printf '^"?build\t-tags=("?|[^[:space:]]*,)%s(,|"|$)' "$tag")

usage() {
    echo "usage: release-acceptance-license-guard.sh PATH..." >&2
    exit 2
}
refuse() {
    printf 'refused: %s\n' "$1" >&2
    refused=1
}

is_elf() {
    [[ "$(od -An -N4 -tx1 -- "$1" 2>/dev/null | tr -d ' \n')" == 7f454c46 ]]
}

scan_tree() {
    local root=$1 label=$2 file relative
    # One pass per signal; only ELF matches are builds.
    while IFS= read -r -d '' file; do
        is_elf "$file" || continue
        relative=${file#"$root"/}
        refuse "$label$relative was built with -tags $tag (embedded Go build settings)"
    done < <(grep -r -l -Z -a -E -e "$tags_line" -- "$root" 2>/dev/null || true)
    while IFS= read -r -d '' file; do
        is_elf "$file" || continue
        relative=${file#"$root"/}
        refuse "$label$relative contains the acceptance fixture license"
    done < <(grep -r -l -Z -a -F -e "$holder" -e "$receipt_format" -- "$root" 2>/dev/null || true)
    if [[ -n "$go_tool" ]]; then
        local metadata
        metadata=$(GOTOOLCHAIN=local GOENV=off GOWORK=off "$go_tool" version -m "$root" 2>/dev/null || true)
        while IFS= read -r file; do
            [[ -n "$file" ]] || continue
            relative=${file#"$root"/}
            refuse "$label$relative was built with -tags $tag (go version -m)"
        done < <(awk -v tag="$tag" '
            /^[^\t].*: go/ {
                # Path = everything before the first ": go<digit>" (the version may itself contain a colon).
                if (match($0, /: go[0-9]/) || match($0, /: go/)) current = substr($0, 1, RSTART - 1)
                next
            }
            /^\tbuild\t-tags=/ {
                value = $0; sub(/^\tbuild\t-tags=/, "", value); gsub(/"/, "", value)
                n = split(value, parts, ",")
                for (i = 1; i <= n; i++) if (parts[i] == tag) { print current; break }
            }' <<< "$metadata" | sort -u)
    fi
}

scan_archive() {
    local archive=$1 listing work
    listing=$(tar -tvzf "$archive") || { echo "cannot list archive: $archive" >&2; exit 2; }
    # Release archives hold only regular files and directories.
    if awk '{ t = substr($1, 1, 1); if (t != "-" && t != "d") bad = 1 } END { exit (bad ? 0 : 1) }' <<< "$listing"; then
        echo "archive holds links or special files; refusing to inspect it: $archive" >&2
        exit 2
    fi
    work=$(mktemp -d)
    workdirs+=("$work")
    tar --no-same-owner --no-same-permissions -xzf "$archive" -C "$work" \
        || { echo "cannot extract archive: $archive" >&2; exit 2; }
    scan_tree "$work" "$(basename -- "$archive"):"
}

[[ $# -ge 1 ]] || usage
go_tool=${CELIKPANEL_GUARD_GO:-}
if [[ -z "$go_tool" ]]; then
    go_tool=$(command -v go 2>/dev/null || true)
fi
[[ -z "$go_tool" || -x "$go_tool" ]] || { echo "CELIKPANEL_GUARD_GO is not executable: $go_tool" >&2; exit 2; }
refused=0
workdirs=()
cleanup() { [[ ${#workdirs[@]} -eq 0 ]] || rm -rf -- "${workdirs[@]}"; }
trap cleanup EXIT HUP INT TERM

for target in "$@"; do
    if [[ -L "$target" ]]; then
        echo "refusing a symbolic link: $target" >&2
        exit 2
    elif [[ -d "$target" ]]; then
        scan_tree "$(cd -- "$target" && pwd -P)" ""
    elif [[ -f "$target" && "$target" == *.tar.gz ]]; then
        scan_archive "$target"
    elif [[ -f "$target" ]]; then
        dir=$(mktemp -d)
        workdirs+=("$dir")
        cp -- "$target" "$dir/$(basename -- "$target")"
        scan_tree "$dir" ""
    else
        echo "not a release tree, archive or file: $target" >&2
        exit 2
    fi
done
if [[ "$refused" -ne 0 ]]; then
    printf '%s\n' \
        "Why: a program above was built with the $tag test tag, which accepts an acceptance fixture license." \
        "Acceptance builds are never CelikPanel releases, so nothing was packaged or signed." \
        "Next: rebuild the release with the ordinary make dist from a clean tree, then package again." \
        "Acceptance archives come only from deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh --acceptance-license." >&2
    exit 1
fi
