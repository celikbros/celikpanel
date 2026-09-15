#!/usr/bin/env bash
# Execute extracted production shell orchestration with private process doubles.
# SQLite durability/crash behavior is tested separately with real databases.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
extract_function() { sed -n "/^$1() {$/,/^}$/p" "$ROOT/update.sh"; }
fail() { echo "FAIL: $*" >&2; exit 1; }
for name in read_database_migration_policy verify_database_publication_if_required run_panel_migrations_offline; do
    extract_function "$name" >> "$TEST_ROOT/functions.sh"
done
# Run the real normal-update tail through the observable DB-ready boundary.
sed -n '/^# New normal updates remain active through isolated migration/,/^verify_saved_enablement$/p' "$ROOT/update.sh" | sed '$d' > "$TEST_ROOT/tail.sh"
[[ -s "$TEST_ROOT/tail.sh" ]] || fail 'production migration tail missing'
mkdir "$TEST_ROOT/bin"
cat > "$TEST_ROOT/bin/agent" <<'SH'
#!/usr/bin/env bash
[[ $# == 1 && $1 == --check-service-mutation-idle-under-external-lock ]] || exit 91
printf 'agent-idle\n' >> "$TRACE"
SH
cat > "$TEST_ROOT/bin/panel" <<'SH'
#!/usr/bin/env bash
case "$*" in
    --check-service-operations-idle) printf 'panel-idle\n' >> "$TRACE";;
    --restore-service-operation-snapshot=*) printf 'legacy-restore\n' >> "$TRACE";;
    *) exit 92;;
esac
SH
chmod 0755 "$TEST_ROOT/bin/agent" "$TEST_ROOT/bin/panel"
source "$TEST_ROOT/functions.sh"
die() { printf '%s\n' "$*" >&2; exit 41; }
export TRACE
run_case() (
    set -euo pipefail
    local_case=$1
    TRACE=$TEST_ROOT/$local_case.trace
    : > "$TRACE"
    RECOVERY_RUNTIME_ROOT= MUTATION_LOCK_FD=8 AGENT_STATE_DIR=$TEST_ROOT MUTATION_LOCK=$TEST_ROOT/lock
    BIN_DIR=$TEST_ROOT/bin PANEL_DB=$TEST_ROOT/celikpanel.db
    snapshot_name=20260915T000000Z-from-unknown-to-$(printf 'a%.0s' {1..40})-$(printf 'b%.0s' {1..32})
    RELEASE_TRANSACTION_ROOT=$TEST_ROOT/transaction RELEASE_TRANSACTION_FD=9 release_transaction_token=fixture-token
    isolated_database_work=/var/lib/celikpanel/.release-db-migrations/$(printf 'c%.0s' {1..64})/work
    [[ $local_case != legacy ]] || isolated_database_work=
    [[ $local_case != missing-lock ]] || MUTATION_LOCK_FD=
    [[ $local_case != invalid-work ]] || isolated_database_work=/tmp/untrusted-work
    systemctl() {
        [[ $# == 4 && $1 == show && $2 == --property=ActiveState && $3 == --value ]] || exit 93
        printf 'inspect:%s\n' "$4" >> "$TRACE"
        if [[ $local_case == service-active ]]; then printf 'active\n'; else printf 'inactive\n'; fi
    }
    reject_extra_service_cgroup_processes() { [[ $2 == 0 ]]; printf 'cgroup:%s\n' "$1" >> "$TRACE"; }
    sudo() {
        [[ $# == 11 && $1 == -u && $2 == celikpanel && $3 == -- && $4 == env && $5 == -i &&
            $6 == PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin &&
            $7 == HOME=/var/lib/celikpanel && $8 == LC_ALL=C &&
            ${10} == "$BIN_DIR/panel" && ${11} == --migrate-only ]] || exit 94
        printf 'migrate:%s\n' "$9" >> "$TRACE"
        [[ $local_case != migrate-failed ]]
    }
    sync() { [[ $# == 4 && $1 == -f && $2 == -- && $3 == "$PANEL_DB" && $4 == "$TEST_ROOT" ]] || exit 95; printf 'durable\n' >> "$TRACE"; }
    run_database_recovery_command() {
        [[ $# == 3 && $2 == --snapshot && $3 == "$snapshot_name" ]] || exit 96
        case "$1" in
            database-policy) printf 'policy\n' >> "$TRACE"; printf 'required\n';;
            publish-update-database) printf 'publish\n' >> "$TRACE"; [[ $local_case != publish-failed ]];;
            verify-update-database) printf 'verify-database\n' >> "$TRACE"; [[ $local_case != verify-failed ]];;
            *) exit 97;;
        esac
    }
    verify_installed_release_artifacts() { printf 'verify-artifacts\n' >> "$TRACE"; }
    release_txn_mark_completion_pending() {
        [[ $# == 5 && $1 == "$RELEASE_TRANSACTION_ROOT" && $2 == 9 && $3 == fixture-token && $4 == update && $5 == "$snapshot_name" ]] || exit 98
        printf 'completion-marker\n' >> "$TRACE"
    }
    source "$TEST_ROOT/tail.sh"
)
for name in isolated legacy; do
    run_case "$name" > "$TEST_ROOT/$name.log" 2>&1 || { cat "$TEST_ROOT/$name.log"; fail "$name tail failed"; }
    grep -q '^CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start$' "$TEST_ROOT/$name.log" || fail 'missing ready checkpoint'
done
python3 - "$TEST_ROOT" <<'PY'
from pathlib import Path
import sys
root=Path(sys.argv[1])
normal=(root/'isolated.trace').read_text().splitlines()
legacy=(root/'legacy.trace').read_text().splitlines()
work='migrate:CELIKPANEL_DATA_DIR=/var/lib/celikpanel/.release-db-migrations/'+'c'*64+'/work'
assert work in normal
assert normal.index(work)<normal.index('publish')<normal.index('verify-database')<normal.index('completion-marker')
assert normal.index('panel-idle')<normal.index('completion-marker')
assert normal.count('completion-marker')==1 and normal.count(work)==1
assert legacy.index('completion-marker')<legacy.index('migrate:CELIKPANEL_DATA_DIR='+str(root))
assert not any(x in legacy for x in ('publish','verify-database'))
PY
for name in migrate-failed publish-failed verify-failed service-active missing-lock invalid-work; do
    status=0
    run_case "$name" > "$TEST_ROOT/$name.log" 2>&1 || status=$?
    [[ $status == 41 ]] || { cat "$TEST_ROOT/$name.log"; fail "$name did not preserve failure: $status"; }
    ! grep -q '^completion-marker$' "$TEST_ROOT/$name.trace" || fail "$name published completion before verified database"
done
# Every unrecognized policy is unknown, never implicit legacy authorization.
for name in required legacy error malformed wrong-exit unexpected-output empty; do
    status=0
    (
        run_database_recovery_command() {
            case "$name" in
                required) printf 'required\n';;
                legacy) return 6;;
                error) return 1;;
                malformed) printf 'legacy\n';;
                wrong-exit) printf 'required\n'; return 6;;
                unexpected-output) printf 'unexpected\n'; return 6;;
                empty) return 0;;
            esac
        }
        read_database_migration_policy fixture
    ) > "$TEST_ROOT/policy-$name.log" 2>&1 || status=$?
    case "$name" in
        required|legacy) [[ $status == 0 && $(cat "$TEST_ROOT/policy-$name.log") == "$name" ]] || fail "$name policy rejected";;
        *) [[ $status == 41 ]] || fail "$name policy downgraded uncertainty";;
    esac
done
# Exercise the actual rollback dispatch; isolated evidence must skip old restore.
sed -n '/^    database_restore_policy=$(read_database_migration_policy/,/^    restore_paired_agent_ledger$/p' "$ROOT/rollback.sh" > "$TEST_ROOT/restore.sh"
[[ -s "$TEST_ROOT/restore.sh" ]] || fail 'production rollback dispatch missing'
for name in isolated legacy unknown restore-failed; do
    status=0
    (
        TRACE=$TEST_ROOT/restore-$name.trace
        : > "$TRACE"
        snapshot_name=fixture transition_state=normal PANEL_DB=$TEST_ROOT/celikpanel.db
        PREFLIGHT_PANEL=$TEST_ROOT/bin/panel snap=$TEST_ROOT/snapshot
        RELEASE_TRANSACTION_FD=9 rollback_transaction_token=fixture-token
        read_database_migration_policy() {
            case "$name" in legacy) printf 'legacy\n';; unknown) return 1;; *) printf 'required\n';; esac
        }
        run_database_recovery_command() {
            [[ $# == 3 && $1 == restore-update-database && $2 == --snapshot && $3 == fixture ]] || exit 99
            printf 'isolated-restore\n' >> "$TRACE"
            [[ $name != restore-failed ]]
        }
        restore_paired_agent_ledger() { printf 'agent-restore\n' >> "$TRACE"; }
        source "$TEST_ROOT/restore.sh"
    ) > "$TEST_ROOT/restore-$name.log" 2>&1 || status=$?
    case "$name" in
        isolated|legacy)
            [[ $status == 0 ]] || fail "$name restore failed"
            [[ $(cat "$TEST_ROOT/restore-$name.trace") == "$name-restore"$'\n''agent-restore' ]] || fail "$name restore used wrong path";;
        *) [[ $status == 41 ]] || fail "$name restore ignored unverified evidence"
           ! grep -q '^agent-restore$' "$TEST_ROOT/restore-$name.trace" || fail "$name continued restoring after failure";;
    esac
done
printf 'PASS: isolated migration publishes before completion; failed/unknown evidence never completes or falls back\n'
