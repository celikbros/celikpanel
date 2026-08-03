#!/bin/bash
# Keep the tagged binary release path immutable and fail-closed. This static
# contract deliberately checks the distribution boundary without publishing.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
WORKFLOW="$ROOT/.github/workflows/ci.yml"

die() {
    echo "release publish contract failed: $*" >&2
    exit 1
}

require_literal() {
    local literal=$1
    grep -Fq -- "$literal" "$WORKFLOW" || die "ci.yml is missing: $literal"
}

reject_literal() {
    local literal=$1
    ! grep -Fq -- "$literal" "$WORKFLOW" || die "ci.yml must not contain: $literal"
}

require_before() {
    local first=$1 second=$2 first_line second_line
    first_line=$(grep -Fn -- "$first" "$WORKFLOW" | head -n 1 | cut -d: -f1)
    second_line=$(grep -Fn -- "$second" "$WORKFLOW" | head -n 1 | cut -d: -f1)
    [[ -n "$first_line" && -n "$second_line" && "$first_line" -lt "$second_line" ]] \
        || die "ci.yml sequence is invalid: $first must precede $second"
}

test -f "$WORKFLOW"

# A tag is built only after the same commit passed every platform and web gate.
require_literal "tags: ['v*']"
require_literal 'needs: [go, web, windows-portability, freebsd-compile, darwin-compile]'
require_literal 'release_version: ${{ steps.release_version.outputs.value }}'
require_literal '[[ "$version" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]]'
require_literal 'git fetch --no-tags origin +refs/heads/main:refs/remotes/origin/main'
require_literal '[[ "$(git rev-parse refs/remotes/origin/main)" == "${GITHUB_SHA}" ]]'

# Reproducibility and an external checksum are mandatory before publication.
require_literal 'make dist VERSION="${RELEASE_VERSION}" COMMIT="${GITHUB_SHA}" SOURCE_DATE_EPOCH="$source_date_epoch"'
require_literal 'umask 077'
require_literal 'test "$first" = "$second"'
require_literal 'run: sha256sum -c "celikpanel-${RELEASE_VERSION}.tar.gz.sha256"'

# Permanent releases are tag-only, verify the tag, and refuse replacement.
require_literal "if: startsWith(github.ref, 'refs/tags/v')"
require_literal 'permissions:'
require_literal 'contents: write'
require_literal 'gh release view "${GITHUB_REF_NAME}" --repo "${GITHUB_REPOSITORY}"'
require_literal 'immutable assets will not be replaced'
require_literal 'gh release create "${GITHUB_REF_NAME}" "$archive" "$checksum"'
require_literal '--repo "${GITHUB_REPOSITORY}" --verify-tag --generate-notes'
reject_literal '--clobber'

require_before 'Rebuild and compare the release archive' 'Verify the external release checksum'
require_before 'Verify downloaded release bytes' 'Publish immutable GitHub release assets'

echo "release publish contract passed"
