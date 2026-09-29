#!/usr/bin/env bash
# Prove that release packaging refuses the acceptance_license test build and
# accepts the ordinary build. Builds both panel variants from this tree with the
# release flags; needs only a Go toolchain (CELIKPANEL_TEST_GO or go on PATH).
# Contacts nothing and installs nothing.
set -euo pipefail
LC_ALL=C
export LC_ALL

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
guard="$repo_root/deploy/release-acceptance-license-guard.sh"
GO=${CELIKPANEL_TEST_GO:-$(command -v go || true)}
[[ -n "$GO" && -x "$GO" ]] || { echo "a Go toolchain is required (CELIKPANEL_TEST_GO)" >&2; exit 2; }
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
fail() { printf 'acceptance license guard contract failed: %s\n' "$1" >&2; exit 1; }
passed=0
ok() { passed=$((passed + 1)); }

gobuild() {
    (cd "$repo_root" && env -i HOME="${HOME:-/root}" PATH="$PATH" GOCACHE="${GOCACHE:-$tmp/gocache}" \
        GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
        "$GO" build -trimpath -buildvcs=false "$@")
}
# guard_status EXPECTED LABEL [env...] -- PATH...
expect_guard() {
    local want=$1 label=$2 status=0
    shift 2
    env "$@" bash "$guard" "${targets[@]}" > "$tmp/guard.out" 2>&1 || status=$?
    [[ $status -eq $want ]] || { cat "$tmp/guard.out" >&2; fail "$label: exit $status, want $want"; }
    ok
}
without_go=(PATH=/usr/bin:/bin CELIKPANEL_GUARD_GO=)
with_go=(PATH=/usr/bin:/bin CELIKPANEL_GUARD_GO="$GO")

# Release-shaped trees: the real panel in both variants with the Makefile flags.
ldflags="-s -w -X main.buildVersion=v0.0.0-guard-test -X main.buildCommit=0123456789abcdef0123456789abcdef01234567"
for variant in ordinary tagged; do
    mkdir -p "$tmp/$variant/celikpanel-test/bin" "$tmp/$variant/celikpanel-test/deploy"
    cp -- "$guard" "$repo_root/deploy/write-release-manifest.sh" "$repo_root/deploy/release-content-guard.sh" "$tmp/$variant/celikpanel-test/deploy/"
    printf 'fixture\n' > "$tmp/$variant/celikpanel-test/release.commit"
done
gobuild -ldflags "$ldflags" -o "$tmp/ordinary/celikpanel-test/bin/panel" ./cmd/panel
gobuild -tags acceptance_license -ldflags "$ldflags" -o "$tmp/tagged/celikpanel-test/bin/panel" ./cmd/panel

# The build settings really differ, and only the tagged binary carries the label.
"$GO" version -m "$tmp/tagged/celikpanel-test/bin/panel" | grep -Fqx $'\tbuild\t-tags=acceptance_license' \
    || fail "tagged panel does not record -tags=acceptance_license"
! "$GO" version -m "$tmp/ordinary/celikpanel-test/bin/panel" | grep -Fq -- '-tags=' \
    || fail "ordinary panel records build tags"
holder="ACCEPTANCE FIXTURE $(printf '\342\200\224') NOT FOR PRODUCTION"
grep -a -Fq -- "$holder" "$tmp/tagged/celikpanel-test/bin/panel" || fail "tagged panel lacks the fixture label"
! grep -a -Fq -- "$holder" "$tmp/ordinary/celikpanel-test/bin/panel" || fail "ordinary panel carries the fixture label"
! grep -a -Fq -- "CPK-acce57f1c7" "$tmp/ordinary/celikpanel-test/bin/panel" || fail "ordinary panel carries the fixture key"
! grep -a -Fq -- "/etc/celikpanel-dns-kill-matrix" "$tmp/ordinary/celikpanel-test/bin/panel" \
    || fail "ordinary panel carries the guest marker path"
ok

targets=("$tmp/ordinary/celikpanel-test")
expect_guard 0 "ordinary tree without Go" "${without_go[@]}"
expect_guard 0 "ordinary tree with Go" "${with_go[@]}"
targets=("$tmp/tagged/celikpanel-test")
expect_guard 1 "tagged tree without Go" "${without_go[@]}"
grep -Fq 'bin/panel was built with -tags acceptance_license (embedded Go build settings)' "$tmp/guard.out" \
    || fail "build-info refusal not reported"
grep -Fq 'bin/panel contains the acceptance fixture license' "$tmp/guard.out" || fail "string refusal not reported"
grep -Fq 'Next: rebuild the release with the ordinary make dist' "$tmp/guard.out" || fail "refusal gives no next action"
[[ $(grep -c '^Next: ' "$tmp/guard.out") -eq 1 ]] || fail "next action repeated"
expect_guard 1 "tagged tree with Go" "${with_go[@]}"
grep -Fq 'bin/panel was built with -tags acceptance_license (go version -m)' "$tmp/guard.out" \
    || fail "go version -m refusal not reported"
targets=("$tmp/tagged/celikpanel-test/bin/panel")
expect_guard 1 "a single tagged file" "${without_go[@]}"

# Each signal alone is enough; other tags and text mentions are not builds.
cat > "$tmp/main.go" <<'EOF'
package main

import "os"

func main() { os.Stdout.WriteString(label) }
EOF
printf 'package main\n\nconst label = "%s"\n' "$holder" > "$tmp/label.go"
mkdir -p "$tmp/signals/tag-only" "$tmp/signals/text-only" "$tmp/signals/clean"
(cd "$tmp" && printf 'module guardtest\n\ngo 1.26\n' > go.mod)
printf 'package main\n\nconst label = "clean"\n' > "$tmp/clean.go"
(cd "$tmp" && env -i HOME="${HOME:-/root}" PATH="$PATH" GOCACHE="${GOCACHE:-$tmp/gocache}" GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 \
    "$GO" build -tags acceptance_license -trimpath -buildvcs=false -ldflags "-s -w" -o signals/tag-only/tool main.go clean.go)
(cd "$tmp" && env -i HOME="${HOME:-/root}" PATH="$PATH" GOCACHE="${GOCACHE:-$tmp/gocache}" GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 \
    "$GO" build -trimpath -buildvcs=false -ldflags "-s -w" -o signals/text-only/tool main.go label.go)
for tags in celikpanel_mail_renewal acceptance_licensed dns_kill_matrix,celikpanel_mail_renewal; do
    (cd "$tmp" && env -i HOME="${HOME:-/root}" PATH="$PATH" GOCACHE="${GOCACHE:-$tmp/gocache}" GOTOOLCHAIN=local GOENV=off GOWORK=off CGO_ENABLED=0 \
        "$GO" build -tags "$tags" -trimpath -buildvcs=false -o "signals/clean/tool-${tags//,/-}" main.go clean.go)
done
printf 'Documentation may quote %s\nbuild\t-tags=acceptance_license\n' "$holder" > "$tmp/signals/clean/README.md"
targets=("$tmp/signals/tag-only")
expect_guard 1 "tag without fixture text" "${without_go[@]}"
expect_guard 1 "tag without fixture text, with Go" "${with_go[@]}"
targets=("$tmp/signals/text-only")
expect_guard 1 "fixture text without the tag" "${without_go[@]}"
targets=("$tmp/signals/clean")
expect_guard 0 "other tags and text mentions" "${without_go[@]}"
expect_guard 0 "other tags and text mentions, with Go" "${with_go[@]}"
targets=("$tmp/tagged/celikpanel-test" "$tmp/ordinary/celikpanel-test")
expect_guard 1 "any refused path refuses the run" "${without_go[@]}"

# Archives as make dist writes them, and an unsafe archive.
for variant in ordinary tagged; do
    tar --sort=name --owner=0 --group=0 --numeric-owner --format=gnu -czf "$tmp/$variant.tar.gz" -C "$tmp/$variant" celikpanel-test
done
targets=("$tmp/ordinary.tar.gz")
expect_guard 0 "ordinary archive" "${without_go[@]}"
targets=("$tmp/tagged.tar.gz")
expect_guard 1 "tagged archive" "${with_go[@]}"
grep -Fq 'tagged.tar.gz:celikpanel-test/bin/panel' "$tmp/guard.out" || fail "archive member not named"
mkdir -p "$tmp/link/celikpanel-test"
ln -s /etc/passwd "$tmp/link/celikpanel-test/escape"
tar -czf "$tmp/link.tar.gz" -C "$tmp/link" celikpanel-test
targets=("$tmp/link.tar.gz")
expect_guard 2 "archive with a symbolic link" "${without_go[@]}"
targets=("$tmp/does-not-exist")
expect_guard 2 "missing path" "${without_go[@]}"

# The ordinary dist recipe refuses: write-release-manifest.sh writes no manifest.
if bash "$tmp/tagged/celikpanel-test/deploy/write-release-manifest.sh" "$tmp/tagged/celikpanel-test" > "$tmp/manifest.out" 2>&1; then
    fail "write-release-manifest.sh accepted an acceptance_license build"
fi
[[ ! -e "$tmp/tagged/celikpanel-test/SHA256SUMS" ]] || fail "manifest written for an acceptance_license build"
grep -Fq 'no checksum manifest was written' "$tmp/manifest.out" || fail "manifest refusal not explained"
bash "$tmp/ordinary/celikpanel-test/deploy/write-release-manifest.sh" "$tmp/ordinary/celikpanel-test" \
    || fail "write-release-manifest.sh refused an ordinary build"
(cd "$tmp/ordinary/celikpanel-test" && sha256sum -c SHA256SUMS >/dev/null) || fail "ordinary manifest invalid"
grep -Fq ' ./deploy/release-acceptance-license-guard.sh' "$tmp/ordinary/celikpanel-test/SHA256SUMS" \
    || fail "guard not shipped in the release tree"
ok

printf 'Acceptance license guard: %d checks passed (ordinary accepted; tagged refused by build info and by fixture text, with and without Go; make dist manifest refused)\n' "$passed"
