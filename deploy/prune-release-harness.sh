#!/usr/bin/env bash
# Remove development and test material that the installed product never reads
# from a staged release tree, before its checksum manifest and archive are
# written (make dist). release-content-guard.sh refuses exactly these paths.
#
# Removed, and only these (paths relative to the release root):
#   1. every directory named "evidence" (retained native-run evidence),
#   2. the whole deploy/e2e tree (acceptance harnesses: their scripts,
#      fixtures, Python modules, reports and offline tests),
#   3. every deploy/test-* entry, whatever its extension (the repository's
#      contract tests: .sh, .py, .ps1),
#   4. anywhere: files named *_test.go, *_test.sh, test_*.py and *.test.mjs,
#      and __pycache__ directories.
# Nothing else is touched. No install, update, rollback or recovery path reads
# any of them: install.sh, update.sh, rollback.sh, bootstrap-*.sh,
# libexec/get.sh, the deploy/ helpers and the Go runtime name only the release
# helpers, deploy/recovery, deploy/systemd units and scripts, and the
# release-recovery files (internal/recoverypublication/material_linux.go). An
# installed older release checks a candidate only against its own SHA256SUMS
# (written after this pruning) and named run-time files; it requires none of
# the removed paths (verified against v0.1.0-alpha.80 on 2026-10-01). The
# harnesses always run from a repository checkout or a git archive.
#
# Deterministic: the result depends only on the staged tree, so the archive
# keeps its reproducible name order, mtime, owner and mode normalisation.
#
# usage: prune-release-harness.sh RELEASE_ROOT
# Kurulu ürünün hiç okumadığı geliştirme ve test malzemesini (evidence
# dizinleri, deploy/e2e ağacı, deploy/test-* dosyaları, *_test.go, *_test.sh,
# test_*.py, *.test.mjs ve __pycache__) sürüm ağacından çıkarır.
set -euo pipefail
LC_ALL=C
export LC_ALL

[[ $# -eq 1 ]] || { echo "usage: prune-release-harness.sh RELEASE_ROOT" >&2; exit 2; }
root=$1
[[ -d "$root" && ! -L "$root" ]] || { echo "release root is unavailable or unsafe: $root" >&2; exit 2; }
if [[ -d "$root/deploy" && ! -L "$root/deploy" ]]; then
    # Rules 2 and 3: deploy/e2e and deploy/test-* (files or directories).
    find "$root/deploy" -xdev -mindepth 1 -maxdepth 1 \( -name e2e -o -name 'test-*' \) \
        -exec rm -rf -- {} +
fi
# Rule 1 and the directory part of rule 4.
find "$root" -xdev -depth -type d \( -name evidence -o -name __pycache__ \) -exec rm -rf -- {} +
# Rule 4: test files anywhere.
find "$root" -xdev -type f \( -name '*_test.go' -o -name '*_test.sh' -o -name 'test_*.py' -o -name '*.test.mjs' \) -delete
exit 0
