#!/usr/bin/env bash
# Remove acceptance-harness material that the installed product never reads
# from a staged release tree, before its checksum manifest and archive are
# written (make dist).
#
# Removed, and only these:
#   - every directory named "evidence" (retained native-run evidence; pair2
#     counted 6426 such files among 7154 archive entries), and
#   - deploy/e2e/**/test_*.py (the harness's offline unit tests).
# Nothing under deploy/ outside deploy/e2e is touched except an "evidence"
# directory, and no install, update, rollback or recovery path reads
# deploy/e2e: install.sh, update.sh, rollback.sh, bootstrap-*.sh, the deploy/
# helpers and the Go runtime name only deploy/recovery, deploy/systemd and the
# release-recovery files (internal/recoverypublication/material_linux.go).
# The harness itself always runs from a repository checkout or a git archive,
# never from an installed release.
#
# Deterministic: the result depends only on the staged tree, so the archive
# keeps its reproducible name order, mtime, owner and mode normalisation.
#
# usage: prune-release-harness.sh RELEASE_ROOT
# Kurulu ürünün hiç okumadığı kabul düzeneği malzemesini (evidence
# dizinleri ve deploy/e2e altındaki test_*.py) sürüm ağacından çıkarır.
set -euo pipefail
LC_ALL=C
export LC_ALL

[[ $# -eq 1 ]] || { echo "usage: prune-release-harness.sh RELEASE_ROOT" >&2; exit 2; }
root=$1
[[ -d "$root" && ! -L "$root" ]] || { echo "release root is unavailable or unsafe: $root" >&2; exit 2; }
if [[ -d "$root/deploy/e2e" && ! -L "$root/deploy/e2e" ]]; then
    find "$root/deploy/e2e" -xdev -type f -name 'test_*.py' -delete
fi
find "$root" -xdev -depth -type d -name evidence -exec rm -rf -- {} +
exit 0
