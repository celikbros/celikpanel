#!/usr/bin/env bash
# Local shell/CLI glue only. The shared Go package owns real filesystem
# exchange/SIGKILL tests; this fixture does not claim native systemd recovery.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
fail() { echo "FAIL: $*" >&2; exit 1; }
extract() { sed -n "/^$2() {$/,/^}$/p" "$ROOT/$1"; }
die() { echo "$*" >&2; exit 71; }
mkdir "$TEST_ROOT/kit" "$TEST_ROOT/kit/bin"
export PUBLICATION_FIXTURE_ROOT=$TEST_ROOT
cat > "$TEST_ROOT/kit/bin/recovery" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 11 && ( $1 == restore-resource || $1 == publish-resource ) &&
   $2 == --resource && ( $3 == bin || $3 == web ) &&
   $4 == --snapshot && $5 == "$EXPECTED_SNAPSHOT" &&
   $6 == --snapshot-manifest && $7 == "$EXPECTED_SNAPSHOT_MANIFEST" &&
   $8 == --candidate-root && $9 == "$EXPECTED_CANDIDATE" &&
   ${10} == --candidate-manifest && ${11} == "$EXPECTED_CANDIDATE_MANIFEST" ]] || exit 81
[[ $(stat -Lc '%d:%i' /proc/self/fd/9) == "$EXPECTED_LOCK_ID" ]] || exit 82
flock -n -x 9 || exit 83
if flock -n -x "$PUBLICATION_FIXTURE_ROOT/transaction.lock" true; then exit 84; fi
printf '%s %s\n' "$1" "$3" >> "$PUBLICATION_FIXTURE_ROOT/calls"
[[ ${FAIL_RESOURCE:-} != "$3" ]] || exit 85
SH
chmod 0755 "$TEST_ROOT/kit/bin/recovery"
exec {fixture_fd}<>"$TEST_ROOT/transaction.lock"
flock -x "$fixture_fd"
export EXPECTED_LOCK_ID=$(stat -Lc '%d:%i' "$TEST_ROOT/transaction.lock")
export EXPECTED_SNAPSHOT=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
export EXPECTED_SNAPSHOT_MANIFEST=$(printf 'c%.0s' {1..64})
export EXPECTED_CANDIDATE=/var/backups/celikpanel/releases/$(printf 'a%.0s' {1..12})-$(printf 'd%.0s' {1..24})
export EXPECTED_CANDIDATE_MANIFEST=$(printf 'e%.0s' {1..64})
RELEASE_TRANSACTION_FD=$fixture_fd
CODE_ROOT=$TEST_ROOT/kit RECOVERY_RUNTIME_ROOT=$TEST_ROOT/kit
snapshot_name=$EXPECTED_SNAPSHOT TRUSTED_RELEASE_ROOT=$EXPECTED_CANDIDATE
rollback_snapshot_manifest_sha=$EXPECTED_SNAPSHOT_MANIFEST
rollback_candidate_manifest_sha=$EXPECTED_CANDIDATE_MANIFEST
eval "$(extract rollback.sh restore_product_resources)"
restore_product_resources
printf 'restore-resource bin\nrestore-resource web\n' | cmp -s - "$TEST_ROOT/calls" || fail 'rollback CLI identity or sequence changed'
[[ $(stat -Lc '%d:%i' "/proc/self/fd/$fixture_fd") == "$EXPECTED_LOCK_ID" ]] || fail 'parent dynamic FD changed'
: > "$TEST_ROOT/calls"
export FAIL_RESOURCE=bin
status=0
(restore_product_resources) > "$TEST_ROOT/failure.log" 2>&1 || status=$?
[[ $status == 71 ]] || fail 'failed resource did not stop rollback'
printf 'restore-resource bin\n' | cmp -s - "$TEST_ROOT/calls" || fail 'rollback proceeded after refusal'
unset FAIL_RESOURCE
# Substitute the fixed launcher only in this private extracted fixture; no
# production path or installed runtime is changed.
eval "$(extract install.sh publish_apply_only_resources | sed "s@/usr/libexec/celikpanel/recovery@$TEST_ROOT/kit/bin/recovery@g")"
validate_apply_only_transaction() { printf 'admitted\n' >> "$TEST_ROOT/calls"; }
APPLY_ONLY=1
CELIKPANEL_RELEASE_TRANSACTION_FD=$fixture_fd
CELIKPANEL_RELEASE_TRANSACTION_SNAPSHOT=$EXPECTED_SNAPSHOT
APPLY_ONLY_SNAPSHOT_MANIFEST_SHA=$EXPECTED_SNAPSHOT_MANIFEST
APPLY_ONLY_CANDIDATE_MANIFEST_SHA=$EXPECTED_CANDIDATE_MANIFEST
: > "$TEST_ROOT/calls"
publish_apply_only_resources
printf 'admitted\npublish-resource bin\npublish-resource web\n' | cmp -s - "$TEST_ROOT/calls" || fail 'forward publication lost admission or FD mapping'
: > "$TEST_ROOT/calls"
export FAIL_RESOURCE=bin
status=0
(publish_apply_only_resources) > "$TEST_ROOT/failure.log" 2>&1 || status=$?
[[ $status == 71 ]] || fail 'forward refusal did not stop installer'
printf 'admitted\npublish-resource bin\n' | cmp -s - "$TEST_ROOT/calls" || fail 'forward publication fell through after refusal'
python3 - "$ROOT" <<'PY'
from pathlib import Path
import sys
root=Path(sys.argv[1])
rollback=(root/'rollback.sh').read_text()
install=(root/'install.sh').read_text()
block=rollback.split('if [[ $rollback_pending_resume -eq 0 ]]; then\n',1)[1].split('    validate_root_trusted_dir_chain "$LIBEXEC_DIR"',1)[0]
assert 'restore_product_resources' in block
assert 'rm -rf' not in block and 'cp -a' not in block
block=install.split('# 4. Install files ',1)[1].split('# Runtimes dir is where',1)[0]
assert 'if [[ "$APPLY_ONLY" -eq 1 ]]; then\n    publish_apply_only_resources\nelse\n' in block
assert block.index('publish_apply_only_resources') < block.index('else\n') < block.index('install -m 0755 "$SRC/bin/panel"')
assert block.rstrip().endswith('fi')
PY
printf 'PASS: exact resource CLI tuples, dynamic FD9 mapping and refusal propagation\n'
