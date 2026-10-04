#!/usr/bin/env bash
# Prove that the ordinary release carries no development or test material:
# prune-release-harness.sh removes exactly its rule set from a staged copy of
# this repository's deploy/ tree, release-content-guard.sh refuses each rule in
# a tree or a .tar.gz archive, write-release-manifest.sh refuses to write a
# manifest while such paths are present, every run-time deploy/ file survives,
# and the pruned tree still packs reproducibly with make dist's exact tar flags
# under umask 022 and 077.
# Rule set (release-root-relative): any "evidence" directory; deploy/e2e;
# deploy/test-*; anywhere *_test.go, *_test.sh, test_*.py, *.test.mjs and
# __pycache__.
# Needs bash, GNU tar, gzip, find and sha256sum. Contacts nothing and installs
# nothing; make is not required (the dist steps are run as stand-ins).
set -euo pipefail
LC_ALL=C
export LC_ALL

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
guard="$repo_root/deploy/release-content-guard.sh"
prune="$repo_root/deploy/prune-release-harness.sh"
tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
fail() { printf 'release content guard contract failed: %s\n' "$1" >&2; exit 1; }
passed=0
ok() { passed=$((passed + 1)); }
expect() {
    local want=$1 label=$2 status=0
    shift 2
    "$@" > "$tmp/out" 2>&1 || status=$?
    [[ $status -eq $want ]] || { cat "$tmp/out" >&2; fail "$label: exit $status, want $want"; }
    ok
}
# The exact archive step of the Makefile dist recipe.
pack() {
    local umask_value=$1 root_parent=$2 name=$3 out=$4
    (umask "$umask_value" && tar --sort=name --mtime="@1700000000" --owner=0 --group=0 --numeric-owner \
        --mode='u=rwX,go=rX' --format=gnu -cf "$out.tar" -C "$root_parent" "$name" && gzip -n -f "$out.tar")
}
# The pruning rule as a path filter (find . -type f output), for set equality.
rule='(^|/)(evidence|__pycache__)/|^\./deploy/e2e/|^\./deploy/test-|(^|/)[^/]*_test\.(go|sh)$|(^|/)test_[^/]*\.py$|(^|/)[^/]*\.test\.mjs$'

# 1. Synthetic trees: each rule, positive and negative.
mk() { mkdir -p -- "$(dirname -- "$1")"; printf 'x\n' > "$1"; }
base=$tmp/synthetic/celikpanel-test
# Near misses that are run-time material and must stay accepted.
for keep in deploy/systemd/celikpanel-agent.service deploy/systemd/arm-firewall-restore.sh \
    deploy/release-transaction-guard.sh deploy/recover-frankfurt-alpha75.py \
    deploy/recovery/agent-checker.sources deploy/schema17bridge/main.go \
    deploy/e2e-notes.txt deploy/latest-test.sh deploy/contest-x.sh deploy/sub/test-helper.sh \
    deploy/notes/evidence.txt web/dist/assets/test.js web/dist/assets/contest_test.css \
    web/dist/assets/test_index.js web/dist/assets/a.test.js bin/test_tool \
    recovery-runtime/deploy/recovery/runtime-entry.sh libexec/get.sh; do
    mk "$base/$keep"
done
expect 0 "clean tree with near misses" bash "$guard" "$base"
# refuse_case LABEL PATH TAG: create PATH (a file; a trailing / makes an empty
# directory), require refusal naming "TAG: PATH" for the tree and an archive,
# then remove it and require acceptance again.
refuse_case() {
    local label=$1 path=$2 tag=$3 shown=${2%/}
    if [[ "$path" == */ ]]; then mkdir -p -- "$base/$path"; else mk "$base/$path"; fi
    expect 1 "$label in tree" bash "$guard" "$base"
    grep -Fq "$tag: $shown" "$tmp/out" || { cat "$tmp/out" >&2; fail "$label: refusal does not name $tag: $shown"; }
    grep -Fq 'Next: build the release with make dist' "$tmp/out" || fail "$label: refusal gives no next action"
    pack 022 "$tmp/synthetic" celikpanel-test "$tmp/case"
    expect 1 "$label in archive" bash "$guard" "$tmp/case.tar.gz"
    grep -Fq "$tag: $shown" "$tmp/out" || fail "$label: archive refusal does not name $shown"
    # Remove the created path and the empty directories it introduced; the
    # near-miss files keep their own directories.
    rm -rf -- "${base:?}/$shown"
    case "$shown" in
        deploy/e2e/*) rm -rf -- "${base:?}/deploy/e2e" ;;
    esac
    find "$base" -mindepth 1 -depth -type d -empty -delete
    expect 0 "$label removed" bash "$guard" "$base"
}
refuse_case "evidence file" deploy/e2e-like/evidence/run/result.json evidence
refuse_case "empty evidence directory anywhere" web/dist/evidence/ evidence
refuse_case "harness script" deploy/e2e/dns-kill-matrix/fixture.py "acceptance harness"
refuse_case "harness report" deploy/e2e/rhel9/REPORT.md "acceptance harness"
refuse_case "harness contract script" deploy/e2e/rhel9/test-contract.sh "acceptance harness"
refuse_case "empty harness directory" deploy/e2e/ "acceptance harness"
refuse_case "contract test .sh" deploy/test-release-sequence-policy.sh "contract test"
refuse_case "contract test .py" deploy/test-install-license-entry.py "contract test"
refuse_case "contract test .ps1" deploy/test-download-portal-publisher.ps1 "contract test"
refuse_case "contract test directory" deploy/test-fixtures/data.json "contract test"
refuse_case "Go test" deploy/schema17bridge/main_test.go "test file"
refuse_case "shell test" deploy/systemd/arm-firewall-restore_test.sh "test file"
refuse_case "Python test outside e2e" deploy/test_outside_e2e.py "test file"
refuse_case "Python test in runtime" recovery-runtime/deploy/test_x.py "test file"
refuse_case "web test" web/dist/assets/api-error.test.mjs "test file"
refuse_case "bytecode cache" deploy/__pycache__/x.cpython-313.pyc "bytecode cache"
expect 2 "usage" bash "$guard"
expect 2 "not a tree or archive" bash "$guard" "$base/deploy/release-transaction-guard.sh"

# The pruner on the synthetic tree removes exactly the rule set.
for bad in deploy/e2e/dns-kill-matrix/fixture.py deploy/e2e/rhel9/test-contract.sh \
    deploy/test-release-sequence-policy.sh deploy/test-install-license-entry.py \
    deploy/test-download-portal-publisher.ps1 deploy/schema17bridge/main_test.go \
    deploy/systemd/arm-firewall-restore_test.sh deploy/x/evidence/a.txt deploy/__pycache__/m.pyc \
    web/dist/a.test.mjs recovery-runtime/deploy/test_y.py; do
    mk "$base/$bad"
done
(cd "$base" && find . -type f | sort) > "$tmp/synthetic-before.list"
expect 0 "prune synthetic" bash "$prune" "$base"
(cd "$base" && find . -type f | sort) > "$tmp/synthetic-after.list"
grep -Ev "$rule" "$tmp/synthetic-before.list" > "$tmp/synthetic-expected.list" || true
cmp -s "$tmp/synthetic-expected.list" "$tmp/synthetic-after.list" \
    || { diff "$tmp/synthetic-expected.list" "$tmp/synthetic-after.list" >&2; fail "synthetic prune differs from the rule set"; }
ok
[[ ! -e "$base/deploy/e2e" && ! -e "$base/deploy/__pycache__" && ! -e "$base/deploy/x/evidence" ]] \
    || fail "prune left a rule directory behind"
ok
expect 0 "synthetic tree accepted after pruning" bash "$guard" "$base"

# 2. This repository's deploy/ tree, staged as make dist stages it.
stage=$tmp/stage/celikpanel-test
mkdir -p "$stage/deploy" "$stage/bin"
cp -r "$repo_root/deploy/." "$stage/deploy/"
printf 'fixture\n' > "$stage/release.commit"
(cd "$stage" && find . -type f | sort) > "$tmp/before.list"
grep -Eq "$rule" "$tmp/before.list" || fail "the repository deploy/ tree no longer exercises the rule set"
ok
expect 1 "staged repository tree before pruning" bash "$guard" "$stage"
expect 1 "manifest refused before pruning" bash "$stage/deploy/write-release-manifest.sh" "$stage"
[[ ! -e "$stage/SHA256SUMS" ]] || fail "a manifest was written for a tree with development material"
grep -Fq 'release tree contains development or test material; no checksum manifest was written' "$tmp/out" \
    || fail "manifest refusal does not name the reason"
ok
expect 0 "prune" bash "$prune" "$stage"
(cd "$stage" && find . -type f | sort) > "$tmp/after.list"
grep -Ev "$rule" "$tmp/before.list" > "$tmp/expected.list" || true
cmp -s "$tmp/expected.list" "$tmp/after.list" || { diff "$tmp/expected.list" "$tmp/after.list" | head -20 >&2; fail "prune removed a path outside its rule"; }
ok
[[ ! -e "$stage/deploy/e2e" ]] || fail "deploy/e2e survived pruning"
! find "$stage" -type d \( -name evidence -o -name __pycache__ \) | grep -q . || fail "an evidence or cache directory survived pruning"
! find "$stage/deploy" -maxdepth 1 -name 'test-*' | grep -q . || fail "a deploy/test-* entry survived pruning"
ok
# Every path an installed server, the release writers or the download-portal
# builder read from deploy/ is still present.
for needed in deploy/recovery/agent-checker.sources deploy/recovery/panel-checker.sources \
    deploy/release-recovery-runner.sh deploy/release-recovery-foundation.sh deploy/release-recovery.protocol \
    deploy/release-recovery-observation.sh deploy/release-sequence-policy deploy/release-signing-ed25519.pem \
    deploy/release-transaction-guard.sh deploy/release-transaction-start-guard.sh deploy/release-unit-transition.sh \
    deploy/panel-tls-snapshot.sh deploy/finalize-pending-update.sh deploy/finalize-pending-rollback.sh \
    deploy/abort-pre-mutation-active-update.sh deploy/recover-active-update-database.sh \
    deploy/enroll-signed-release-trust.sh deploy/write-release-manifest.sh deploy/write-signed-release-manifest.sh \
    deploy/release-acceptance-license-guard.sh deploy/release-content-guard.sh deploy/prune-release-harness.sh \
    deploy/systemd/celikpanel-agent.service deploy/systemd/celikpanel-panel.service \
    deploy/systemd/celikpanel-firewall-restore.service deploy/systemd/arm-firewall-restore.sh \
    deploy/systemd/enable-firewall-restore-if-saved.sh \
    deploy/systemd/celikpanel-release-recovery.service deploy/systemd/celikpanel-release-recovery.timer \
    deploy/schema17bridge/main.go deploy/agent-native-contract/main.go deploy/recovery/bundle/main.go; do
    [[ -f "$stage/$needed" ]] || fail "pruning removed $needed"
done
ok
expect 0 "pruned repository tree" bash "$guard" "$stage"
expect 0 "prune is idempotent" bash "$prune" "$stage"
find "$stage" -type d -exec chmod 0755 {} +
find "$stage" -type f -exec chmod 0644 {} +
expect 0 "manifest written after pruning" bash "$stage/deploy/write-release-manifest.sh" "$stage"
[[ -f "$stage/SHA256SUMS" ]] || fail "no manifest after pruning"
ok

# 3. Reproducibility: same bytes under umask 022 and 077 (the CI rebuild).
pack 022 "$tmp/stage" celikpanel-test "$tmp/first"
pack 077 "$tmp/stage" celikpanel-test "$tmp/second"
first=$(sha256sum "$tmp/first.tar.gz" | cut -d' ' -f1)
second=$(sha256sum "$tmp/second.tar.gz" | cut -d' ' -f1)
[[ "$first" == "$second" ]] || fail "pruned archive is not reproducible: $first != $second"
ok
listing=$(tar -tvzf "$tmp/first.tar.gz")
awk '{ if ($2 != "0/0") bad = 1 } END { exit bad }' <<< "$listing" || fail "archive owner is not 0/0"
ok
expect 0 "pruned archive" bash "$guard" "$tmp/first.tar.gz"

printf 'release content guard contract: %d checks passed\n' "$passed"
