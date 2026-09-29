#!/usr/bin/env bash
# Prove that the ordinary release carries no acceptance evidence or harness
# tests: prune-release-harness.sh removes exactly those paths from a staged
# copy of this repository's deploy/ tree, release-content-guard.sh refuses
# them in a tree or a .tar.gz archive, write-release-manifest.sh refuses to
# write a manifest while they are present, and the pruned tree still packs
# reproducibly with make dist's exact tar flags under umask 022 and 077.
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

# 1. Synthetic trees: the rule itself.
mk() { mkdir -p -- "$(dirname -- "$1")"; printf 'x\n' > "$1"; }
base=$tmp/synthetic/celikpanel-test
mk "$base/deploy/systemd/celikpanel-agent.service"
mk "$base/deploy/e2e/dns-kill-matrix/fixture.py"
mk "$base/deploy/test-release-sequence-policy.sh"
mk "$base/deploy/e2e/notes/evidence.txt"
mk "$base/deploy/test_outside_e2e.py"
expect 0 "clean tree" bash "$guard" "$base"
mk "$base/deploy/e2e/dns-pair-acceptance/evidence/run/result.json"
expect 1 "evidence file in tree" bash "$guard" "$base"
grep -Fq 'evidence: deploy/e2e/dns-pair-acceptance/evidence' "$tmp/out" || fail "refusal does not name the evidence path"
rm -rf -- "$base/deploy/e2e/dns-pair-acceptance/evidence"
mkdir -p "$base/web/dist/evidence"
expect 1 "empty evidence directory anywhere" bash "$guard" "$base"
rmdir "$base/web/dist/evidence"
mk "$base/deploy/e2e/release-recovery/test_worker.py"
expect 1 "harness test in tree" bash "$guard" "$base"
grep -Fq 'harness test: deploy/e2e/release-recovery/test_worker.py' "$tmp/out" || fail "refusal does not name the harness test"
rm -f -- "$base/deploy/e2e/release-recovery/test_worker.py"
expect 0 "tree clean again" bash "$guard" "$base"

# Archives: the first component is the release root.
pack 022 "$tmp/synthetic" celikpanel-test "$tmp/clean"
expect 0 "clean archive" bash "$guard" "$tmp/clean.tar.gz"
mk "$base/deploy/e2e/dns-kill-matrix/evidence/batch/raw.txt"
pack 022 "$tmp/synthetic" celikpanel-test "$tmp/evidence"
expect 1 "evidence in archive" bash "$guard" "$tmp/evidence.tar.gz"
rm -rf -- "$base/deploy/e2e/dns-kill-matrix/evidence"
mk "$base/deploy/e2e/dns-pair-acceptance/sub/test_sequence.py"
pack 022 "$tmp/synthetic" celikpanel-test "$tmp/harness"
expect 1 "harness test in archive" bash "$guard" "$tmp/harness.tar.gz"
rm -f -- "$base/deploy/e2e/dns-pair-acceptance/sub/test_sequence.py"
expect 2 "usage" bash "$guard"
expect 2 "not a tree or archive" bash "$guard" "$base/deploy/test_outside_e2e.py"

# 2. This repository's deploy/ tree, staged as make dist stages it.
stage=$tmp/stage/celikpanel-test
mkdir -p "$stage/deploy" "$stage/bin"
cp -r "$repo_root/deploy/." "$stage/deploy/"
find "$stage" -type d -name __pycache__ -prune -exec rm -rf -- {} +
printf 'fixture\n' > "$stage/release.commit"
(cd "$stage" && find . -type f | sort) > "$tmp/before.list"
if grep -Eq '(^|/)evidence/|^\./deploy/e2e/(.*/)?test_[^/]*\.py$' "$tmp/before.list"; then
    expect 1 "staged repository tree before pruning" bash "$guard" "$stage"
    expect 1 "manifest refused before pruning" bash "$stage/deploy/write-release-manifest.sh" "$stage"
    [[ ! -e "$stage/SHA256SUMS" ]] || fail "a manifest was written for a tree with evidence"
fi
expect 0 "prune" bash "$prune" "$stage"
(cd "$stage" && find . -type f | sort) > "$tmp/after.list"
grep -Ev '(^|/)evidence/|^\./deploy/e2e/(.*/)?test_[^/]*\.py$' "$tmp/before.list" > "$tmp/expected.list" || true
cmp -s "$tmp/expected.list" "$tmp/after.list" || { diff "$tmp/expected.list" "$tmp/after.list" | head -20 >&2; fail "prune removed a path outside its rule"; }
ok
! find "$stage" -type d -name evidence | grep -q . || fail "an evidence directory survived pruning"
ok
# Every path an installed server reads from deploy/ is still present.
for needed in deploy/recovery/agent-checker.sources deploy/recovery/panel-checker.sources deploy/release-recovery-runner.sh deploy/release-recovery.protocol \
    deploy/release-sequence-policy deploy/release-transaction-start-guard.sh deploy/write-release-manifest.sh \
    deploy/release-acceptance-license-guard.sh deploy/release-content-guard.sh \
    deploy/systemd/celikpanel-agent.service deploy/systemd/celikpanel-panel.service \
    deploy/systemd/celikpanel-release-recovery.service deploy/systemd/celikpanel-release-recovery.timer; do
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
