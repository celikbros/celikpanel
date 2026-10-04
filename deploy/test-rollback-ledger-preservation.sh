#!/usr/bin/env bash
# Execute the production preservation helper and independent Agent checker.
# These are local filesystem/SIGKILL tests, not a full native rollback drill.
set -euo pipefail
[[ $EUID == 0 ]] || { echo 'SKIP: rollback ledger preservation requires root'; exit 0; }
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d /var/lib/celikpanel-rollback-ledger-test.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
fail() { echo "FAIL: $*" >&2; exit 1; }
die() { echo "$*" >&2; exit 41; }
mapfile -t sources < "$ROOT/deploy/recovery/agent-checker.sources"
(cd "$ROOT" && GOTOOLCHAIN=${GOTOOLCHAIN:-go1.26.5} go build -o "$TEST_ROOT/agent-checker" "${sources[@]}")
PREFLIGHT_AGENT=$TEST_ROOT/agent-checker
CODE_ROOT=$ROOT
source "$ROOT/deploy/release-transaction-guard.sh"
eval "$(sed -n '/^restore_paired_agent_ledger() {$/,/^}$/p' "$ROOT/rollback.sh")"
group=$(getent group celikpanel | cut -d: -f3) || group=$(id -g)
[[ -n $group ]] || group=$(id -g)

# A genuine parsed idle ledger, built from the public persisted schema. Fixture
# bytes are not a native producer claim. The real checker validates them.
make_case() {
    local label=$1
    case_root=$TEST_ROOT/$label
    mkdir -m 0700 "$case_root" "$case_root/state" "$case_root/runtime" "$case_root/transaction" "$case_root/snapshot" "$case_root/snapshot/agent-state"
    AGENT_STATE_DIR=$case_root/state AGENT_LEDGER=$case_root/state/service-mutations.json
    MUTATION_LOCK=$case_root/runtime/service-mutation.lock
    printf '%s' '{"version":1,"jobs":{}}' > "$AGENT_LEDGER"
    : > "$MUTATION_LOCK"
    chmod 0600 "$AGENT_LEDGER" "$MUTATION_LOCK"
    chown 0:"$group" "$AGENT_STATE_DIR" "$case_root/runtime" "$AGENT_LEDGER" "$MUTATION_LOCK"
    snap=$case_root/snapshot
    cp -a -- "$AGENT_LEDGER" "$snap/agent-state/service-mutations.json"
    transition_state=normal agent_ledger_state=present
    RELEASE_TRANSACTION_ROOT=$case_root/transaction
    exec 9<>"$RELEASE_TRANSACTION_ROOT/transaction.lock"
    chmod 0600 "$RELEASE_TRANSACTION_ROOT/transaction.lock"
    flock -x 9
    RELEASE_TRANSACTION_FD=9
    rollback_transaction_token=$(release_txn_generate_token)
    snapshot_name=20260914T120000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
    release_txn_create_active_marker "$RELEASE_TRANSACTION_ROOT" 9 "$rollback_transaction_token" rollback "$snapshot_name"
}
metadata() { stat -Lc '%d:%i:%u:%g:%a:%h:%s:%y:%z' -- "$AGENT_LEDGER"; }
with_lock() (
    exec 8<>"$MUTATION_LOCK"
    flock -x 8
    MUTATION_LOCK_FD=8
    restore_paired_agent_ledger
)
check_readonly() {
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" --check-service-mutation-idle
}

make_case unchanged
before=$(metadata)
with_lock
with_lock
[[ $(metadata) == "$before" ]] || fail 'normal preservation replaced or changed the existing ledger'
check_readonly
printf 'PASS: exact normal ledger retains inode and metadata across repeated restore\n'

# Historical failure: unlink creates a missing ledger which normal admission
# rejects. This characterizes the old gap; no missing-state bypass is introduced.
make_case historical-gap
rm -f -- "$AGENT_LEDGER"
if check_readonly >"$case_root/check.log" 2>&1; then fail 'normal admission accepted the historical missing-ledger gap'; fi
[[ ! -e $AGENT_LEDGER ]] || fail 'read-only admission recreated the ledger'
printf 'PASS: historical deletion gap remains rejected, not silently repaired\n'

# Kill the actual shell while its real byte comparison has completed but the
# preservation helper has not returned. No current ledger unlink/copy occurs.
make_case killed-during-proof
before=$(metadata)
(
    exec 8<>"$MUTATION_LOCK"
    flock -x 8
    MUTATION_LOCK_FD=8
    cmp() {
        command cmp "$@" || return
        : > "$case_root/proof.ready"
        while :; do sleep 1; done
    }
    restore_paired_agent_ledger
    : > "$case_root/unexpected-return"
) >"$case_root/child.log" 2>&1 & child=$!
for attempt in {1..100}; do
    [[ ! -e $case_root/proof.ready ]] || break
    kill -0 "$child" 2>/dev/null || { cat "$case_root/child.log" >&2; fail 'proof child exited early'; }
    sleep 0.05
done
[[ -e $case_root/proof.ready ]] || { kill -KILL "$child" 2>/dev/null || true; fail 'proof child did not reach checkpoint'; }
kill -KILL "$child"
status=0
wait "$child" 2>/dev/null || status=$?
[[ $status == 137 && ! -e $case_root/unexpected-return ]] || fail 'process-death checkpoint not reached'
[[ $(metadata) == "$before" ]] || fail 'killed preservation changed ledger identity or metadata'
# The one bounded sleep can inherit the lock until it exits; reap that window.
for attempt in {1..40}; do
    if flock -n "$MUTATION_LOCK" true; then break; fi
    sleep 0.05
done
with_lock
check_readonly
[[ $(metadata) == "$before" ]] || fail 'retry after process death replaced the ledger'
printf 'PASS: SIGKILL during normal preservation leaves exact ledger and permits retry\n'

# A later terminal host mutation is valid and idle, but must not be forgotten.
make_case later-terminal
python3 - "$AGENT_LEDGER" <<'PY_LEDGER'
import json,sys
request='c'*32
job={'request_id':request,'owner_id':'d'*32,'kind':'service_install','target':'nginx','status':'failed','phase':'failed','attempt':1,'started_at':'2026-09-14T12:00:00Z','updated_at':'2026-09-14T12:02:00Z','lease_expires_at':'0001-01-01T00:00:00Z','deadline_at':'2026-09-14T12:10:00Z','finished_at':'2026-09-14T12:02:00Z'}
with open(sys.argv[1],'w') as out: out.write(json.dumps({'version':1,'jobs':{request:job}},separators=(',',':')))
PY_LEDGER
check_readonly
before=$(metadata)
status=0
with_lock >"$case_root/refused.log" 2>&1 || status=$?
[[ $status == 41 ]] || fail 'later idle terminal host mutation was forgotten'
grep -Fq 'preserve host mutations' "$case_root/refused.log" || fail 'later mutation did not reach exact snapshot comparison'
[[ $(metadata) == "$before" ]] || fail 'later idle terminal ledger metadata changed'
printf 'PASS: valid idle terminal history after snapshot is refused and preserved\n'

for scenario in missing corrupt mode hardlink symlink wrong-token missing-lock; do
    make_case "$scenario"
    case $scenario in
        missing) rm -- "$AGENT_LEDGER" ;;
        corrupt) printf 'owner changed ledger\n' > "$AGENT_LEDGER" ;;
        mode) chmod 0640 "$AGENT_LEDGER" ;;
        hardlink) ln "$AGENT_LEDGER" "$case_root/owner-link" ;;
        symlink) mv "$AGENT_LEDGER" "$case_root/owner-ledger"; ln -s "$case_root/owner-ledger" "$AGENT_LEDGER" ;;
        wrong-token) rollback_transaction_token=$(release_txn_generate_token) ;;
        missing-lock) ;;
    esac
    before_hash=$(find "$AGENT_STATE_DIR" -maxdepth 1 -type f -exec sha256sum {} \;)
    status=0
    if [[ $scenario == missing-lock ]]; then
        (MUTATION_LOCK_FD=; restore_paired_agent_ledger) >"$case_root/rejected.log" 2>&1 || status=$?
    else
        with_lock >"$case_root/rejected.log" 2>&1 || status=$?
    fi
    [[ $status == 41 ]] || { cat "$case_root/rejected.log" >&2; fail "$scenario was not rejected"; }
    [[ $(find "$AGENT_STATE_DIR" -maxdepth 1 -type f -exec sha256sum {} \;) == "$before_hash" ]] || fail "$scenario owner bytes changed"
    printf 'PASS: normal preservation refuses %s and preserves evidence\n' "$scenario"
done
printf 'rollback ledger preservation: ok\n'
