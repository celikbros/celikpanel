#!/bin/bash
# Create the canonical checksum manifest after every release byte and mode has
# been staged. SHA256SUMS deliberately excludes itself.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

[[ $# -eq 1 ]] || { echo "usage: write-release-manifest.sh RELEASE_ROOT" >&2; exit 2; }
guard=$(readlink -e -- "$(dirname -- "${BASH_SOURCE[0]}")/release-acceptance-license-guard.sh") \
    || { echo "release acceptance-license guard is unavailable" >&2; exit 1; }
content_guard=$(readlink -e -- "$(dirname -- "${BASH_SOURCE[0]}")/release-content-guard.sh") \
    || { echo "release content guard is unavailable" >&2; exit 1; }
root=$(readlink -e -- "$1") || { echo "release root is unavailable" >&2; exit 1; }
[[ -d "$root" && ! -L "$root" ]] || { echo "release root is unsafe" >&2; exit 1; }
if find "$root" -xdev -type l -print -quit | grep -q .; then
    echo "release tree contains a symbolic link" >&2
    exit 1
fi
if find "$root" -xdev ! -type d ! -type f -print -quit | grep -q .; then
    echo "release tree contains a special filesystem object" >&2
    exit 1
fi
if find "$root" -xdev -type f -links +1 -print -quit | grep -q .; then
    echo "release tree contains a hard-linked file" >&2
    exit 1
fi
# An acceptance_license test build is never a release (make dist included).
if ! bash "$guard" "$root"; then
    echo "release tree contains an acceptance_license test build; no checksum manifest was written" >&2
    exit 1
fi
# Acceptance evidence and harness tests are never part of a release.
if ! bash "$content_guard" "$root"; then
    echo "release tree contains acceptance evidence or harness tests; no checksum manifest was written" >&2
    exit 1
fi
(
    cd "$root"
    LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 sha256sum > SHA256SUMS
    sha256sum -c SHA256SUMS >/dev/null
)
