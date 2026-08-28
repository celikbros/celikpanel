#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
LIBRARY=$ROOT/deploy/release-transaction-guard.sh
START_GUARD=$ROOT/deploy/release-transaction-start-guard.sh

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

expect_failure() {
    local description=$1
    shift
    if "$@"; then
        fail "$description"
    fi
}

extract_shell_function() {
    local file=$1 function_name=$2
    sed -n "/^${function_name}() {$/,/^}$/p" "$file"
}

write_quiesce_coordinator_ledger() {
    local child=$1
    printf '%s\t%s\t%s\t%s\n' \
        celikpanel-agent.service active 4242 1 \
        celikpanel-panel.service active 4243 2 \
        > "$child/quiesce-coordinators.tsv"
    chmod 0600 -- "$child/quiesce-coordinators.tsv"
}

complete_scheduler_restore_obligation() {
    local root=$1 lock_fd=$2 token=$3 operation=$4 snapshot=$5
    release_txn_mark_scheduler_restore_pending "$root" "$lock_fd" "$token" "$operation" "$snapshot"
    release_txn_remove_completion_pending "$root" "$lock_fd" "$token" "$operation" "$snapshot"
    release_txn_remove_scheduler_restore_pending "$root" "$lock_fd" "$token" "$operation" "$snapshot"
}

bash -n "$LIBRARY" "$START_GUARD" "$0"

# Complete release manifests hash every regular payload file and exclude only
# the manifest itself, so trusted guard files cannot be omitted silently.
# Eksiksiz sürüm manifestleri her normal payload dosyasını hashler ve yalnız
# manifestin kendisini dışlar; güvenilir guard dosyaları sessizce atlanamaz.
for verifier in "$ROOT/bootstrap-update.sh" "$ROOT/update.sh" "$ROOT/rollback.sh"; do
    grep -Fq "LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0" "$verifier" \
        || fail "complete release manifest coverage missing in $verifier"
done
grep -Fq '_release_txn_running_library" != "$_release_txn_expected_root/$_release_txn_library_relative"' "$LIBRARY" \
    || fail "library does not pin its source path to the verified running release"
grep -Fq 'lock=$root/transaction.lock' "$LIBRARY" \
    || fail "library does not use the fixed persistent transaction.lock path"
grep -Fq 'ExecCondition=+%s %s %s' "$LIBRARY" \
    || fail "library does not publish the root-trusted ExecCondition"
if grep -Fq 'ConditionPathExists=' "$LIBRARY"; then
    fail "obsolete active-only ConditionPathExists guard remains"
fi

# The source itself rejects an alias or a library outside the verified release.
# Source'un kendisi, doğrulanmış sürüm dışındaki takma yolu veya kütüphaneyi reddeder.
expect_failure "library accepted a mismatched trusted release root" \
    env TRUSTED_RELEASE_ROOT=/nonexistent bash -c 'source "$1"' _ "$LIBRARY"

if [[ $EUID -ne 0 ]]; then
    printf 'SKIP: release transaction guard fixture requires root metadata\n'
    exit 0
fi

TRUSTED_RELEASE_ROOT=$ROOT
source "$LIBRARY"

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
foreign_lock_pid=
foreign_lock_ready=$tmp/foreign-lock.ready
foreign_lock_gate=$tmp/foreign-lock.gate

# Hold the same inode through a different OFD so wrong-descriptor ownership
# checks cannot pass merely because an independent probe observes EWOULDBLOCK.
# Aynı inode'u farklı bir OFD üzerinden tut; yanlış-descriptor sahiplik denetimi
# yalnız bağımsız probe EWOULDBLOCK gördüğü için geçemesin.
start_foreign_lock_holder() {
    rm -f -- "$foreign_lock_ready" "$foreign_lock_gate"
    mkfifo -- "$foreign_lock_gate"
    (
        exec {foreign_fd}<>"$transaction_root/transaction.lock"
        flock -n -x "$foreign_fd" || exit 70
        : > "$foreign_lock_ready"
        read -r _ < "$foreign_lock_gate" || true
    ) &
    foreign_lock_pid=$!
    for _ in $(seq 1 100); do
        [[ -e "$foreign_lock_ready" ]] && break
        kill -0 "$foreign_lock_pid" 2>/dev/null || break
        sleep 0.01
    done
    [[ -e "$foreign_lock_ready" ]] || fail "foreign lock holder did not become ready"
}

stop_foreign_lock_holder() {
    printf 'release\n' > "$foreign_lock_gate"
    wait "$foreign_lock_pid" || fail "foreign lock holder failed"
    foreign_lock_pid=
    rm -f -- "$foreign_lock_ready" "$foreign_lock_gate"
}

transaction_root=$tmp/transaction
runtime_parent=$tmp/run
runtime_root=$runtime_parent/celikpanel-release-transaction
systemd_root=$tmp/systemd
helper_parent=$tmp/usr-libexec
helper_path=$helper_parent/celikpanel/release-transaction-start-guard
mkdir -m 0700 -- "$transaction_root"
mkdir -m 0755 -- "$runtime_parent" "$systemd_root" "$tmp/bin"
chown root:root -- "$transaction_root" "$runtime_parent" "$systemd_root" "$tmp/bin"
: > "$transaction_root/transaction.lock"
chown root:root -- "$transaction_root/transaction.lock"
chmod 0600 -- "$transaction_root/transaction.lock"

exec {lock_fd}<>"$transaction_root/transaction.lock"
flock -n "$lock_fd" || fail "fixture could not acquire global transaction lock"
release_txn_verify_inherited_lock "$transaction_root" "$lock_fd"

# A second hard link makes the persistent lock identity ambiguous and fails.
# İkinci hard link kalıcı kilit kimliğini belirsizleştirir ve başarısız olur.
ln -- "$transaction_root/transaction.lock" "$transaction_root/transaction.lock.alias"
expect_failure "hard-linked transaction lock was accepted" \
    release_txn_verify_inherited_lock "$transaction_root" "$lock_fd"
rm -f -- "$transaction_root/transaction.lock.alias"

# The lock is an identity object and must remain empty.
# Lock bir kimlik nesnesidir ve boş kalmalıdır.
printf x > "$transaction_root/transaction.lock"
expect_failure "non-empty transaction lock was accepted" \
    release_txn_verify_inherited_lock "$transaction_root" "$lock_fd"
: > "$transaction_root/transaction.lock"

# Path-to-FD identity is mandatory even when another inode is correctly locked.
# Başka bir inode doğru kilitli olsa bile path-FD kimliği zorunludur.
mv -- "$transaction_root/transaction.lock" "$transaction_root/transaction.lock.old"
: > "$transaction_root/transaction.lock"
chown root:root -- "$transaction_root/transaction.lock"
chmod 0600 -- "$transaction_root/transaction.lock"
expect_failure "inherited descriptor/path inode mismatch was accepted" \
    release_txn_verify_inherited_lock "$transaction_root" "$lock_fd"
rm -f -- "$transaction_root/transaction.lock"
mv -- "$transaction_root/transaction.lock.old" "$transaction_root/transaction.lock"

# An inherited descriptor without an active flock must fail because an
# independent open can acquire the lock instead of receiving EWOULDBLOCK.
# Etkin flock taşımayan miras descriptor, bağımsız açılış EWOULDBLOCK yerine
# kilidi alabildiği için başarısız olmalıdır.
flock -u "$lock_fd"
expect_failure "unlocked inherited descriptor was accepted" \
    release_txn_verify_inherited_lock "$transaction_root" "$lock_fd"

# A foreign holder must not make an unlocked same-inode descriptor look owned.
# Yabancı holder kilitsiz aynı-inode descriptor'ı sahipmiş gibi gösterememelidir.
start_foreign_lock_holder
expect_failure "unlocked descriptor was accepted while a foreign process held the lock" \
    release_txn_verify_inherited_lock "$transaction_root" "$lock_fd"
stop_foreign_lock_holder
flock -n "$lock_fd" || fail "fixture could not reacquire global transaction lock"

systemctl_trace=$tmp/systemctl.trace
: > "$systemctl_trace"
export FIXTURE_SYSTEMCTL_TRACE=$systemctl_trace
cat > "$tmp/bin/systemctl" <<'SYSTEMCTL'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "${FIXTURE_SYSTEMCTL_TRACE:?}"
if [[ "$1" == daemon-reload && $# -eq 1 ]]; then
    exit 0
fi
if [[ "$1" == show && "$2" == --property=DropInPaths && "$3" == --value && $# -eq 4 ]]; then
    if [[ ${FIXTURE_WRONG_DROPIN:-0} == 1 ]]; then
        printf '/wrong/drop-in.conf\n'
    else
        [[ ${FIXTURE_RUNTIME_PRESERVE:-0} != 1 || $4 != celikpanel-agent.service ]] ||
            printf '%s/%s.d/09-runtime-directory-preserve.conf\n' "$FIXTURE_SYSTEMD_ROOT" "$4"
        printf '%s/%s.d/10-release-transaction-guard.conf\n' "$FIXTURE_SYSTEMD_ROOT" "$4"
        [[ ${FIXTURE_EXTRA_DROPIN:-0} != 1 ]] ||
            printf '%s/%s.d/99-reset.conf\n' "$FIXTURE_SYSTEMD_ROOT" "$4"
    fi
    exit 0
fi
if [[ "$1" == show && "$2" == --property=NeedDaemonReload && "$3" == --value && $# -eq 4 ]]; then
    printf '%s\n' "${FIXTURE_NEEDS_RELOAD:-no}"
    exit 0
fi
exit 64
SYSTEMCTL
chmod 0755 "$tmp/bin/systemctl"
export FIXTURE_SYSTEMD_ROOT=$systemd_root
mkdir -m 0755 -- "$tmp/poison"
cat > "$tmp/poison/systemctl" <<'POISON_SYSTEMCTL'
#!/usr/bin/env bash
set -euo pipefail
: > "$POISON_SYSTEMCTL_SENTINEL"
exit 99
POISON_SYSTEMCTL
chmod 0755 -- "$tmp/poison/systemctl"
export POISON_SYSTEMCTL_SENTINEL=$tmp/poison-systemctl-called
PATH=$tmp/poison:$PATH
export PATH

# Every operator recovery entrypoint binds all legacy bare systemctl callsites
# through the same exact wrapper. A PATH shadow must never observe execution.
for entrypoint in \
    "$ROOT/deploy/abort-pre-mutation-active-update.sh" \
    "$ROOT/deploy/finalize-pending-update.sh" \
    "$ROOT/deploy/finalize-pending-rollback.sh" \
    "$ROOT/deploy/recover-active-update-database.sh"
do
    grep -Fqx 'SYSTEMCTL_BIN=/usr/bin/systemctl' "$entrypoint" ||
        fail "recovery entrypoint lacks exact systemctl identity: $entrypoint"
    (
        die() { fail "$1"; }
        eval "$(extract_shell_function "$entrypoint" validate_exact_systemctl)"
        eval "$(extract_shell_function "$entrypoint" systemctl)"
        SYSTEMCTL_BIN=$tmp/bin/systemctl
        validate_exact_systemctl
        systemctl daemon-reload
    )
done
[[ ! -e $POISON_SYSTEMCTL_SENTINEL ]] ||
    fail "a recovery entrypoint invoked PATH-shadow systemctl"

# Missing helper parents are created only below one already-trusted level;
# aliases, writable/non-root identities and missing grandparents fail closed.
unsafe_helper_parent=$tmp/unsafe-helper-parent
ln -s -- "$tmp/bin" "$unsafe_helper_parent"
expect_failure "symlink helper parent was accepted" \
    _release_txn_prepare_secure_parent_directory "$unsafe_helper_parent"
rm -- "$unsafe_helper_parent"

mkdir -m 0755 -- "$unsafe_helper_parent"
chmod 0777 -- "$unsafe_helper_parent"
expect_failure "group/other-writable helper parent was accepted" \
    _release_txn_prepare_secure_parent_directory "$unsafe_helper_parent"
chmod 0755 -- "$unsafe_helper_parent"
chown 65534:65534 -- "$unsafe_helper_parent"
expect_failure "non-root helper parent was accepted" \
    _release_txn_prepare_secure_parent_directory "$unsafe_helper_parent"
chown root:root -- "$unsafe_helper_parent"
rmdir -- "$unsafe_helper_parent"

expect_failure "helper parent with a missing grandparent was accepted" \
    _release_txn_prepare_secure_parent_directory "$tmp/missing-grandparent/child"
[[ ! -e "$tmp/missing-grandparent" ]] \
    || fail "missing grandparent path was mutated"

# The host systemd root has one exact supported identity. A private 0700 mode
# is not merely unusual: it previously made package subprocesses unable to
# traverse the unit hierarchy and must now fail closed.
chmod 0700 -- "$systemd_root"
expect_failure "mode-0700 systemd unit root was accepted" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
chmod 0755 -- "$systemd_root"

release_txn_install_and_verify_unit_guards \
    "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
[[ "$(stat -Lc '%u %g %a' -- "$helper_parent")" == "0 0 755" ]] \
    || fail "missing secure helper parent was not created safely"
[[ "$(stat -Lc '%u %g %a %h' -- "$helper_path")" == "0 0 755 1" ]] \
    || fail "installed start guard metadata mismatch"
cmp -s -- "$START_GUARD" "$helper_path" \
    || fail "installed start guard bytes differ from verified release"
for unit in celikpanel-agent.service celikpanel-panel.service; do
    dropin=$systemd_root/$unit.d/10-release-transaction-guard.conf
    [[ -f "$dropin" && ! -L "$dropin" ]] || fail "drop-in missing for $unit"
    [[ "$(stat -Lc '%u %g %a %h' -- "$dropin")" == "0 0 644 1" ]] \
        || fail "drop-in metadata mismatch for $unit"
    cmp -s "$dropin" <(printf '[Service]\nExecCondition=+%s %s %s\n' \
        "$helper_path" "$transaction_root" "$runtime_root") \
        || fail "drop-in content mismatch for $unit"
done
release_txn_verify_unit_guards \
    "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
[[ ! -e $POISON_SYSTEMCTL_SENTINEL ]] ||
    fail "PATH-shadow systemctl was invoked instead of the exact fixture"

# Recovery paths deliberately load the trusted runtime-directory preserve
# drop-in before the transaction guard. Only the exact ordered 09+10 set is
# accepted; any third drop-in still fails closed.
printf '[Service]\nRuntimeDirectoryPreserve=yes\n' \
    > "$systemd_root/celikpanel-agent.service.d/09-runtime-directory-preserve.conf"
chmod 0644 -- "$systemd_root/celikpanel-agent.service.d/09-runtime-directory-preserve.conf"
runtime_preserve_dropin=$systemd_root/celikpanel-agent.service.d/09-runtime-directory-preserve.conf
export FIXTURE_RUNTIME_PRESERVE=1
release_txn_install_and_verify_unit_guards \
    "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" \
    "$tmp/bin/systemctl" "" "$runtime_preserve_dropin"
# A normal next update does not carry recovery-only arguments. It must
# auto-detect and prove the persistent exact agent 09 drop-in.
release_txn_install_and_verify_unit_guards \
    "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" \
    "$tmp/bin/systemctl"

# Verification is strictly read-only. Prove both the explicit recovery call
# and the normal auto-detect call preserve the helper plus exact 09/10 files,
# including inode, nanosecond mtime and content hash, while systemd receives
# only the four required show queries.
guard_verify_artifacts=(
    "$helper_path"
    "$runtime_preserve_dropin"
    "$systemd_root/celikpanel-agent.service.d/10-release-transaction-guard.conf"
    "$systemd_root/celikpanel-panel.service.d/10-release-transaction-guard.conf"
)
guard_artifact_state() {
    local artifact=$1 hash
    hash=$(/usr/bin/sha256sum -- "$artifact")
    printf '%s\t%s\t%s\n' \
        "$(/usr/bin/stat -Lc '%d:%i' -- "$artifact")" \
        "$(/usr/bin/stat -Lc '%y' -- "$artifact")" \
        "${hash%% *}"
}
declare -A guard_verify_before=()
for artifact in "${guard_verify_artifacts[@]}"; do
    guard_verify_before["$artifact"]=$(guard_artifact_state "$artifact")
done
guard_verify_expected_trace=$tmp/guard-verify.expected
cat > "$guard_verify_expected_trace" <<'GUARD_VERIFY_TRACE'
show --property=DropInPaths --value celikpanel-agent.service
show --property=NeedDaemonReload --value celikpanel-agent.service
show --property=DropInPaths --value celikpanel-panel.service
show --property=NeedDaemonReload --value celikpanel-panel.service
GUARD_VERIFY_TRACE
assert_guard_verify_read_only() {
    local additional_dropin=${1:-} artifact after
    : > "$systemctl_trace"
    if [[ -n $additional_dropin ]]; then
        release_txn_verify_unit_guards \
            "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" \
            "$tmp/bin/systemctl" "$additional_dropin"
    else
        release_txn_verify_unit_guards \
            "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" \
            "$tmp/bin/systemctl"
    fi
    ! grep -Eq '^(daemon-reload|enable|start)( |$)' "$systemctl_trace" \
        || fail "verify-only guard call performed a systemd mutation"
    cmp -s -- "$guard_verify_expected_trace" "$systemctl_trace" \
        || fail "verify-only guard call issued commands other than exact show queries"
    for artifact in "${guard_verify_artifacts[@]}"; do
        after=$(guard_artifact_state "$artifact")
        [[ $after == "${guard_verify_before[$artifact]}" ]] \
            || fail "verify-only guard call mutated $artifact"
    done
}
assert_guard_verify_read_only "$runtime_preserve_dropin"
assert_guard_verify_read_only
export FIXTURE_EXTRA_DROPIN=1
expect_failure "unexpected third drop-in joined the trusted 09+10 set" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" \
        "$tmp/bin/systemctl" "" "$runtime_preserve_dropin"
unset FIXTURE_EXTRA_DROPIN FIXTURE_RUNTIME_PRESERVE
rm -f -- "$runtime_preserve_dropin"

# A later drop-in can clear ExecCondition while leaving the expected guard path
# visible. Exact-only DropInPaths makes that effective reset fail closed.
for unit in celikpanel-agent.service celikpanel-panel.service; do
    printf '[Service]\nExecCondition=\n' \
        >"$systemd_root/$unit.d/99-reset.conf"
    chmod 0644 "$systemd_root/$unit.d/99-reset.conf"
done
export FIXTURE_EXTRA_DROPIN=1
expect_failure "later reset drop-in bypassed guard installation proof" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
expect_failure "later reset drop-in bypassed guard recovery proof" \
    release_txn_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
unset FIXTURE_EXTRA_DROPIN
rm -f -- "$systemd_root"/*.service.d/99-reset.conf
release_txn_verify_unit_guards \
    "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
export FIXTURE_NEEDS_RELOAD=yes
expect_failure "pending manager reload bypassed guard installation proof" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
expect_failure "pending manager reload bypassed guard recovery proof" \
    release_txn_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
unset FIXTURE_NEEDS_RELOAD
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "start guard blocked an ordinary start without a release marker"

# A legacy v6 snapshot may have a secure 0700 units parent. Restore its exact
# fixed files without importing that parent metadata, without replacing the
# host directory inode, and without touching unrelated units.
snapshot_units=$tmp/snapshot-units
install -d -m 0700 -o root -g root -- "$snapshot_units"
printf 'snapshot agent\n' | install -m 0644 -o root -g root /dev/stdin \
    "$snapshot_units/celikpanel-agent.service"
printf 'snapshot panel\n' | install -m 0644 -o root -g root /dev/stdin \
    "$snapshot_units/celikpanel-panel.service"
printf 'snapshot firewall\n' | install -m 0644 -o root -g root /dev/stdin \
    "$snapshot_units/celikpanel-firewall-restore.service"
for unit in celikpanel-agent.service celikpanel-panel.service \
    celikpanel-firewall-restore.service
do
    printf 'old %s\n' "$unit" | install -m 0644 -o root -g root /dev/stdin \
        "$systemd_root/$unit"
done
printf 'unrelated unit\n' | install -m 0644 -o root -g root /dev/stdin \
    "$systemd_root/unrelated.service"
unit_root_identity=$(release_txn_systemd_unit_root_identity "$systemd_root")
release_txn_restore_celikpanel_unit_files \
    "$transaction_root" "$lock_fd" "$snapshot_units" "$systemd_root" present \
    "$unit_root_identity"
release_txn_verify_systemd_unit_root_identity "$systemd_root" "$unit_root_identity"
[[ "$(stat -Lc '%u %g %a' -- "$systemd_root")" == "0 0 755" ]] \
    || fail "fixed-file restore changed systemd unit root metadata"
for unit in celikpanel-agent.service celikpanel-panel.service \
    celikpanel-firewall-restore.service
do
    cmp -s -- "$snapshot_units/$unit" "$systemd_root/$unit" \
        || fail "fixed-file restore changed bytes for $unit"
    [[ "$(stat -Lc '%u %g %a %h' -- "$systemd_root/$unit")" == "0 0 644 1" ]] \
        || fail "fixed-file restore metadata mismatch for $unit"
done
cmp -s -- "$systemd_root/unrelated.service" <(printf 'unrelated unit\n') \
    || fail "fixed-file restore touched an unrelated systemd unit"

printf 'unexpected\n' | install -m 0644 -o root -g root /dev/stdin \
    "$snapshot_units/unexpected.service"
expect_failure "unit snapshot with an unexpected entry was accepted" \
    release_txn_restore_celikpanel_unit_files \
        "$transaction_root" "$lock_fd" "$snapshot_units" "$systemd_root" present \
        "$unit_root_identity"
rm -f -- "$snapshot_units/unexpected.service"
rm -f -- "$snapshot_units/celikpanel-firewall-restore.service"
release_txn_restore_celikpanel_unit_files \
    "$transaction_root" "$lock_fd" "$snapshot_units" "$systemd_root" absent \
    "$unit_root_identity"
[[ ! -e "$systemd_root/celikpanel-firewall-restore.service" &&
   ! -L "$systemd_root/celikpanel-firewall-restore.service" ]] \
    || fail "absent firewall snapshot left the fixed firewall unit installed"
cmp -s -- "$systemd_root/unrelated.service" <(printf 'unrelated unit\n') \
    || fail "absent-firewall restore touched an unrelated systemd unit"

mv -- "$snapshot_units/celikpanel-panel.service" "$tmp/panel.service.regular"
ln -s -- "$tmp/panel.service.regular" \
    "$snapshot_units/celikpanel-panel.service"
expect_failure "symbolic-link snapshot unit was accepted" \
    release_txn_restore_celikpanel_unit_files \
        "$transaction_root" "$lock_fd" "$snapshot_units" "$systemd_root" absent \
        "$unit_root_identity"
rm -f -- "$snapshot_units/celikpanel-panel.service"
mv -- "$tmp/panel.service.regular" "$snapshot_units/celikpanel-panel.service"

# Root-trusted helper and drop-ins reject hard-linked existing identities.
# Root-trusted yardımcı ve drop-in'ler hard-linkli mevcut kimlikleri reddeder.
ln -- "$helper_path" "$helper_path.alias"
expect_failure "hard-linked existing start guard was accepted" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
rm -f -- "$helper_path.alias"
agent_dropin=$systemd_root/celikpanel-agent.service.d/10-release-transaction-guard.conf
ln -- "$agent_dropin" "$agent_dropin.alias"
expect_failure "hard-linked existing systemd drop-in was accepted" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
rm -f -- "$agent_dropin.alias"

# daemon-reload is not enough: the manager must report each exact loaded path.
# daemon-reload tek başına yetmez; yönetici yüklenen her tam yolu bildirmelidir.
export FIXTURE_WRONG_DROPIN=1
expect_failure "systemd loaded-path mismatch was accepted" \
    release_txn_install_and_verify_unit_guards \
        "$transaction_root" "$runtime_root" "$systemd_root" "$helper_path" "$lock_fd" "$tmp/bin/systemctl"
unset FIXTURE_WRONG_DROPIN

token=$(release_txn_generate_token)
operation=update
quiesce_target=$(printf 'a%.0s' {1..40})
snapshot_name="20260728T000000Z-from-unknown-to-$quiesce_target-00112233445566778899aabbccddeeff"
wrong_token=ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff
[[ "$token" == "$wrong_token" ]] && wrong_token=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee

snapshot_root=$tmp/update-snapshots
mkdir -m 0700 -- "$snapshot_root"
quiesce_stage="$snapshot_root/.release-snapshot.incomplete.$BASHPID.00112233445566778899aabbccddeeff"
quiesce_child="$quiesce_stage/$snapshot_name"
mkdir -m 0700 -- "$quiesce_stage" "$quiesce_child"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$quiesce_child/service-states.tsv"
write_quiesce_coordinator_ledger "$quiesce_child"
printf 'normal\n' > "$quiesce_child/snapshot-transition.state"
chmod 0600 -- "$quiesce_child/service-states.tsv" "$quiesce_child/snapshot-transition.state"
sync -f -- "$quiesce_child/service-states.tsv" "$quiesce_child/quiesce-coordinators.tsv" "$quiesce_child/snapshot-transition.state" "$quiesce_child" "$quiesce_stage" "$snapshot_root"
release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
service_ledger=$quiesce_child/service-states.tsv
chmod 0644 -- "$service_ledger"
expect_failure "stage accepted a non-0600 service-state ledger" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
chmod 0600 -- "$service_ledger"
ln -- "$service_ledger" "$service_ledger.alias"
expect_failure "stage accepted a hard-linked service-state ledger" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
rm -- "$service_ledger.alias"
printf '%s\t%s\t%s\n' \
    celikpanel-panel.service enabled active \
    celikpanel-agent.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted out-of-order service-state rows" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-agent.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted duplicate service-state rows" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service unsupported active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted an unsupported service enablement state" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled unknown \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted an unsupported service active state" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled inactive \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted an active panel with an inactive agent" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    unexpected.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted a four-row service-state ledger" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    certbot.timer enabled active \
    certbot-renew.timer disabled inactive \
    > "$service_ledger"
release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage" \
    || fail "canonical five-row service-state ledger was rejected"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
printf '%s\t%s\t%s' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
expect_failure "stage accepted a service-state ledger without a final newline" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$service_ledger"
for transition_mode in pre-ledger schema17; do
    printf '%s\n' "$transition_mode" > "$quiesce_child/snapshot-transition.state"
    sync -f -- "$quiesce_child/snapshot-transition.state" "$quiesce_child" "$quiesce_stage" "$snapshot_root"
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage" \
        || fail "canonical $transition_mode transition mode was rejected"
done
printf 'unsupported\n' > "$quiesce_child/snapshot-transition.state"
sync -f -- "$quiesce_child/snapshot-transition.state" "$quiesce_child" "$quiesce_stage" "$snapshot_root"
expect_failure "unsupported transition mode was accepted" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf 'normal\n' > "$quiesce_child/snapshot-transition.state"
sync -f -- "$quiesce_child/snapshot-transition.state" "$quiesce_child" "$quiesce_stage" "$snapshot_root"
identity_ledger=$quiesce_child/quiesce-coordinators.tsv
mv -- "$identity_ledger" "$identity_ledger.saved"
expect_failure "stage accepted a missing coordinator ledger" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
mv -- "$identity_ledger.saved" "$identity_ledger"
chmod 0644 -- "$identity_ledger"
expect_failure "stage accepted a non-0600 coordinator ledger" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
chmod 0600 -- "$identity_ledger"
ln -- "$identity_ledger" "$identity_ledger.alias"
expect_failure "stage accepted a hard-linked coordinator ledger" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
rm -- "$identity_ledger.alias"
printf '%s\t%s\t%s\t%s\n' celikpanel-panel.service active 4243 2 celikpanel-agent.service active 4242 1 > "$identity_ledger"
expect_failure "stage accepted out-of-order coordinator rows" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\t%s\n' celikpanel-agent.service active 4242 1 celikpanel-agent.service active 4243 2 > "$identity_ledger"
expect_failure "stage accepted duplicate coordinator rows" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\t%s\n' celikpanel-agent.service active 4242 0 celikpanel-panel.service active 4243 2 > "$identity_ledger"
expect_failure "stage accepted an active coordinator with zero start time" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
printf '%s\t%s\t%s\t%s\n' unexpected.service active 4242 1 celikpanel-panel.service active 4243 2 > "$identity_ledger"
expect_failure "stage accepted an unknown coordinator unit" \
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"
write_quiesce_coordinator_ledger "$quiesce_child"
release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot_name" "$quiesce_stage"

# The durable pre-active phase blocks every start, accepts only its exact
# identity for removal and promotes to active without a marker gap.
# Kalıcı active-öncesi aşama bütün başlangıçları engeller, kaldırma için yalnız
# kendi tam kimliğini kabul eder ve marker boşluğu olmadan active'e yükselir.
release_txn_create_quiesce_marker \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
release_txn_validate_quiesce_token "$transaction_root" "$token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
cmp -s "$transaction_root/quiesce.pending" \
    <(printf 'version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n' \
        "$token" "$operation" "$snapshot_name") \
    || fail "quiesce marker bytes are not canonical"
cp -- "$transaction_root/quiesce.pending" "$transaction_root/scheduler-restore.pending"
chmod 0600 -- "$transaction_root/scheduler-restore.pending"
expect_failure "quiesce plus scheduler marker permitted an ordinary start" \
    "$helper_path" "$transaction_root" "$runtime_root"
rm -f -- "$transaction_root/scheduler-restore.pending"
expect_failure "quiesce marker permitted an ordinary start" \
    "$helper_path" "$transaction_root" "$runtime_root"
expect_failure "quiesce marker permitted controlled-start authorization" \
    release_txn_create_start_authorization \
        "$transaction_root" "$runtime_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
expect_failure "active marker was created beside quiesce" \
    release_txn_create_active_marker \
        "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
expect_failure "quiesce marker accepted a different exact token for removal" \
    release_txn_remove_quiesce_marker \
        "$transaction_root" "$lock_fd" "$wrong_token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
release_txn_remove_quiesce_marker \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
[[ ! -e "$transaction_root/quiesce.pending" && ! -L "$transaction_root/quiesce.pending" ]] \
    || fail "quiesce marker remains after exact removal"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "ordinary start remained blocked after quiesce removal"

release_txn_create_quiesce_marker \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
expect_failure "quiesce marker promoted with a different exact token" \
    release_txn_promote_quiesce_to_active \
        "$transaction_root" "$lock_fd" "$wrong_token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
mv -- "$identity_ledger" "$identity_ledger.saved"
expect_failure "quiesce marker promoted without its coordinator ledger" \
    release_txn_promote_quiesce_to_active \
        "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
[[ -e "$transaction_root/quiesce.pending" && ! -e "$transaction_root/active" ]] \
    || fail "failed promotion changed transaction markers"
mv -- "$identity_ledger.saved" "$identity_ledger"
release_txn_promote_quiesce_to_active \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name" "$snapshot_root" "$quiesce_stage"
[[ ! -e "$transaction_root/quiesce.pending" && ! -L "$transaction_root/quiesce.pending" ]] \
    || fail "quiesce marker remains after active promotion"
release_txn_validate_active_token "$transaction_root" "$token" "$operation" "$snapshot_name"
cmp -s "$transaction_root/active" \
    <(printf 'version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n' \
        "$token" "$operation" "$snapshot_name") \
    || fail "active marker bytes are not canonical"
expect_failure "active marker accepted a different exact token" \
    release_txn_validate_active_token "$transaction_root" "$wrong_token" "$operation" "$snapshot_name"
expect_failure "active marker accepted a different operation" \
    release_txn_validate_active_token "$transaction_root" "$token" rollback "$snapshot_name"
expect_failure "active marker accepted a different snapshot" \
    release_txn_validate_active_token "$transaction_root" "$token" "$operation" other-snapshot

# Canonical bytes include exactly one final newline; extra bytes are rejected.
# Kanonik baytlar tam bir son satır sonu içerir; ek baytlar reddedilir.
{
    _release_txn_print_marker "$token" "$operation" "$snapshot_name"
    printf '\n'
} > "$transaction_root/active"
expect_failure "active marker accepted non-canonical trailing bytes" \
    release_txn_validate_active_token "$transaction_root" "$token" "$operation" "$snapshot_name"
_release_txn_print_marker "$token" "$operation" "$snapshot_name" > "$transaction_root/active"
chown root:root -- "$transaction_root/active"
chmod 0600 -- "$transaction_root/active"

expect_failure "active marker without runtime authorization permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"
release_txn_create_start_authorization \
    "$transaction_root" "$runtime_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "exact live-holder authorization did not permit a controlled start"
cp -- "$transaction_root/active" "$transaction_root/scheduler-restore.pending"
chmod 0600 -- "$transaction_root/scheduler-restore.pending"
expect_failure "active plus scheduler marker bypassed a valid controlled-start authorization" \
    "$helper_path" "$transaction_root" "$runtime_root"
rm -f -- "$transaction_root/scheduler-restore.pending"

# Authorization must match the persistent marker and holder bytes exactly.
# Yetkilendirme kalıcı işaretçi ve holder baytlarıyla tam eşleşmelidir.
_release_txn_print_marker "$wrong_token" "$operation" "$snapshot_name" \
    > "$runtime_root/start.authorization/marker"
expect_failure "mismatched authorization marker permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"
cp -- "$transaction_root/active" "$runtime_root/start.authorization/marker"
chmod 0600 -- "$runtime_root/start.authorization/marker"
holder_pid=$BASHPID
holder_start_time=$(_release_txn_holder_start_time "$holder_pid")
wrong_start_time=$((holder_start_time + 1))
_release_txn_print_holder "$token" "$holder_pid" "$wrong_start_time" "$lock_fd" \
    > "$runtime_root/start.authorization/holder"
expect_failure "wrong holder start time permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"
_release_txn_print_holder "$token" "$holder_pid" "$holder_start_time" "$lock_fd" \
    > "$runtime_root/start.authorization/holder"
chmod 0600 -- "$runtime_root/start.authorization/holder"
ln -- "$runtime_root/start.authorization/holder" "$runtime_root/start.authorization/holder.alias"
expect_failure "hard-linked authorization holder permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"
rm -f -- "$runtime_root/start.authorization/holder.alias"

# A live PID and correct FD path are insufficient when the flock is released.
# Canlı PID ve doğru FD yolu, flock bırakılmışsa yeterli değildir.
flock -u "$lock_fd"
expect_failure "authorization without a held flock permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"

# Even a busy inode is denied when the authorized holder FD has no flock.
# Yetkili holder FD flock taşımıyorsa inode busy olsa bile başlangıç reddedilir.
start_foreign_lock_holder
expect_failure "foreign flock owner satisfied the authorized holder proof" \
    "$helper_path" "$transaction_root" "$runtime_root"
stop_foreign_lock_holder
flock -n "$lock_fd" || fail "fixture could not reacquire lock after authorization probe"

# completion.pending keeps blocking ordinary starts but preserves the exact
# controlled-start authorization until verification finishes.
# completion.pending normal başlangıçları engellemeyi sürdürür; doğrulama bitene
# kadar tam kontrollü-başlangıç yetkilendirmesini korur.
expect_failure "completion accepted a mismatched operation" \
    release_txn_mark_completion_pending \
        "$transaction_root" "$lock_fd" "$token" rollback "$snapshot_name"
release_txn_mark_completion_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "completion.pending blocked the exact controlled start"
release_txn_mark_scheduler_restore_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "matching completion and scheduler markers blocked the exact controlled start"
_release_txn_print_marker "$wrong_token" "$operation" "$snapshot_name" \
    > "$transaction_root/scheduler-restore.pending"
chmod 0600 -- "$transaction_root/scheduler-restore.pending"
expect_failure "mismatched scheduler marker permitted a controlled start" \
    "$helper_path" "$transaction_root" "$runtime_root"
_release_txn_print_marker "$token" "$operation" "$snapshot_name" \
    > "$transaction_root/scheduler-restore.pending"
chmod 0600 -- "$transaction_root/scheduler-restore.pending"
expect_failure "pending removal accepted a mismatched snapshot" \
    release_txn_remove_completion_pending \
        "$transaction_root" "$lock_fd" "$token" "$operation" other-snapshot
release_txn_remove_start_authorization \
    "$transaction_root" "$runtime_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
expect_failure "completion.pending without authorization permitted an ordinary start" \
    "$helper_path" "$transaction_root" "$runtime_root"
release_txn_remove_completion_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "exact scheduler-only obligation blocked an ordinary service start"
printf '\n' >> "$transaction_root/scheduler-restore.pending"
expect_failure "non-canonical scheduler-only marker permitted an ordinary start" \
    "$helper_path" "$transaction_root" "$runtime_root"
_release_txn_print_marker "$token" "$operation" "$snapshot_name" \
    > "$transaction_root/scheduler-restore.pending"
chmod 0600 -- "$transaction_root/scheduler-restore.pending"
release_txn_remove_scheduler_restore_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "start guard remained closed after verified completion"

# Both persistent marker names at once are ambiguous and fail closed.
# İki kalıcı işaretçi adının aynı anda bulunması belirsizdir ve fail-closed olur.
release_txn_create_active_marker \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
cp -- "$transaction_root/active" "$transaction_root/completion.pending"
chown root:root -- "$transaction_root/completion.pending"
chmod 0600 -- "$transaction_root/completion.pending"
expect_failure "simultaneous active and completion.pending markers permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"
rm -f -- "$transaction_root/completion.pending"
release_txn_mark_completion_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
complete_scheduler_restore_obligation \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"

# A child may publish while sharing the inherited lock, but after it exits its
# PID/start-time proof is stale and cannot authorize a start.
# Bir child miras lock'u paylaşırken yayımlayabilir; çıktıktan sonra PID/start-time
# kanıtı eskidir ve başlangıca yetki veremez.
release_txn_create_active_marker \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
(
    release_txn_create_start_authorization \
        "$transaction_root" "$runtime_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
)
expect_failure "stale holder authorization permitted a start" \
    "$helper_path" "$transaction_root" "$runtime_root"
release_txn_clear_stale_start_authorization "$transaction_root" "$runtime_root" "$lock_fd"
expect_failure "active marker permitted a start after stale authorization cleanup" \
    "$helper_path" "$transaction_root" "$runtime_root"
release_txn_mark_completion_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
complete_scheduler_restore_obligation \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"

# Runtime authorization disappears on reboot; a persistent marker must still
# keep both units closed until the transaction is recovered.
# Runtime yetkilendirmesi reboot'ta kaybolur; kalıcı işaretçi işlem kurtarılana
# kadar iki unit'i de kapalı tutmalıdır.
release_txn_create_active_marker \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
release_txn_create_start_authorization \
    "$transaction_root" "$runtime_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "fresh authorization did not permit a controlled start"
release_txn_remove_start_authorization \
    "$transaction_root" "$runtime_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
rmdir -- "$runtime_root"
expect_failure "persistent marker permitted a start after runtime state disappeared" \
    "$helper_path" "$transaction_root" "$runtime_root"
release_txn_mark_completion_pending \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
complete_scheduler_restore_obligation \
    "$transaction_root" "$lock_fd" "$token" "$operation" "$snapshot_name"
"$helper_path" "$transaction_root" "$runtime_root" \
    || fail "ordinary start remained blocked with no persistent marker"

# A failed update may be taken over only by rollback for the exact snapshot.
# Başarısız bir update yalnız tam aynı snapshot için rollback tarafından devralınabilir.
expect_failure "rollback takeover accepted a missing active transaction" \
    release_txn_takeover_active_for_rollback \
        "$transaction_root" "$lock_fd" "$snapshot_name"
release_txn_create_active_marker \
    "$transaction_root" "$lock_fd" "$token" update "$snapshot_name"
expect_failure "rollback takeover accepted a different snapshot" \
    release_txn_takeover_active_for_rollback \
        "$transaction_root" "$lock_fd" other-snapshot
release_txn_validate_active_token \
    "$transaction_root" "$token" update "$snapshot_name"

# This first takeover represents recovery before the durable rename completed;
# the same call represents an idempotent retry after the rename completed.
# İlk devralma dayanıklı rename tamamlanmadan önceki kurtarmayı, aynı çağrının
# tekrarı ise rename tamamlandıktan sonraki idempotent yeniden denemeyi temsil eder.
takeover_token=$(release_txn_takeover_active_for_rollback \
    "$transaction_root" "$lock_fd" "$snapshot_name")
[[ "$takeover_token" == "$token" ]] \
    || fail "rollback takeover did not preserve the update token"
release_txn_validate_active_token \
    "$transaction_root" "$token" rollback "$snapshot_name"
takeover_token=$(release_txn_takeover_active_for_rollback \
    "$transaction_root" "$lock_fd" "$snapshot_name")
[[ "$takeover_token" == "$token" ]] \
    || fail "idempotent rollback takeover changed the token"
release_txn_validate_active_token \
    "$transaction_root" "$token" rollback "$snapshot_name"

# completion.pending is completed evidence and must never be rewritten as a
# rollback takeover; callers must finalize it first.
# completion.pending tamamlanmış kanıttır ve rollback devralması olarak asla
# yeniden yazılmamalıdır; çağıran önce onu sonuçlandırmalıdır.
release_txn_mark_completion_pending \
    "$transaction_root" "$lock_fd" "$token" rollback "$snapshot_name"
expect_failure "rollback takeover accepted completion.pending" \
    release_txn_takeover_active_for_rollback \
        "$transaction_root" "$lock_fd" "$snapshot_name"
release_txn_validate_pending_token \
    "$transaction_root" "$token" rollback "$snapshot_name"
complete_scheduler_restore_obligation \
    "$transaction_root" "$lock_fd" "$token" rollback "$snapshot_name"
rm -rf -- "$quiesce_stage"

# An empty canonical post-publish stage is removable; unsafe or incomplete
# variants stay in place and fail closed.
# Boş kanonik yayın-sonrası stage kaldırılabilir; güvenli olmayan veya eksik
# türler yerinde kalır ve fail-closed olur.
empty_stage_nonce=$(printf '1%.0s' {1..32})
empty_stage="$snapshot_root/.release-snapshot.incomplete.201.$empty_stage_nonce"
mkdir -m 0700 -- "$empty_stage"
sync -f -- "$empty_stage" "$snapshot_root"
release_txn_cleanup_unmarked_update_snapshot_stage \
    "$transaction_root" "$lock_fd" "$snapshot_root" \
    || fail "empty canonical post-publish stage cleanup failed"
[[ ! -e "$empty_stage" && ! -L "$empty_stage" ]] \
    || fail "empty canonical post-publish stage remains after parent sync"

wrong_mode_nonce=$(printf '2%.0s' {1..32})
wrong_mode_stage="$snapshot_root/.release-snapshot.incomplete.202.$wrong_mode_nonce"
mkdir -m 0755 -- "$wrong_mode_stage"
expect_failure "wrong-mode empty stage was accepted for cleanup" \
    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$transaction_root" "$lock_fd" "$snapshot_root"
[[ -d "$wrong_mode_stage" ]] || fail "wrong-mode stage was removed"
chmod 0700 -- "$wrong_mode_stage"
rmdir -- "$wrong_mode_stage"

symlink_nonce=$(printf '3%.0s' {1..32})
symlink_stage="$snapshot_root/.release-snapshot.incomplete.203.$symlink_nonce"
symlink_target=$tmp/empty-stage-symlink-target
mkdir -m 0700 -- "$symlink_target"
ln -s -- "$symlink_target" "$symlink_stage"
expect_failure "symlink stage was accepted for cleanup" \
    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$transaction_root" "$lock_fd" "$snapshot_root"
[[ -L "$symlink_stage" && -d "$symlink_target" ]] \
    || fail "symlink stage cleanup changed the link or its target"
rm -f -- "$symlink_stage"
rmdir -- "$symlink_target"

nonempty_nonce=$(printf '5%.0s' {1..32})
nonempty_target=$(printf '4%.0s' {1..40})
nonempty_snapshot="20260728T115958Z-from-unknown-to-$nonempty_target-$nonempty_nonce"
nonempty_stage="$snapshot_root/.release-snapshot.incomplete.204.$nonempty_nonce"
mkdir -m 0700 -- "$nonempty_stage" "$nonempty_stage/$nonempty_snapshot"
sync -f -- "$nonempty_stage/$nonempty_snapshot" "$nonempty_stage" "$snapshot_root"
expect_failure "non-empty incomplete stage was accepted for cleanup" \
    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$transaction_root" "$lock_fd" "$snapshot_root"
[[ -d "$nonempty_stage/$nonempty_snapshot" ]] \
    || fail "non-empty incomplete stage was removed"
rmdir -- "$nonempty_stage/$nonempty_snapshot" "$nonempty_stage"

# The active marker is published only after the canonical recovery ledger and
# transition mode are durable. A SIGKILL immediately afterwards must resume the
# same target-bound stage, while an already-final snapshot requires rollback.
# Active marker yalnız kanonik kurtarma ledger'ı ve geçiş modu kalıcı olduktan
# sonra yayımlanır. Hemen sonraki SIGKILL aynı hedefe bağlı stage'den sürmeli;
# zaten final snapshot varsa rollback zorunlu olmalıdır.
# A SIGKILL after the recovery scaffold is durable but before marker publication
# leaves one markerless orphan. The next locked run may remove only that exact tree.
# Kurtarma iskeleti kalıcı olduktan fakat marker yayımlanmadan sonraki SIGKILL tek
# bir işaretçisiz artık bırakır. Sonraki kilitli koşu yalnız bu tam ağacı kaldırabilir.
orphan_target=$(printf 'c%.0s' {1..40})
orphan_nonce=$(printf 'd%.0s' {1..32})
orphan_snapshot="20260728T115959Z-from-unknown-to-$orphan_target-$orphan_nonce"
orphan_stage="$snapshot_root/.release-snapshot.incomplete.$BASHPID.$orphan_nonce"
orphan_child="$orphan_stage/$orphan_snapshot"
if (
    mkdir -m 0700 -- "$orphan_stage" "$orphan_child"
    printf '%s\t%s\t%s\n' \
        celikpanel-agent.service enabled active \
        celikpanel-panel.service enabled active \
        celikpanel-firewall-restore.service disabled inactive \
        > "$orphan_child/service-states.tsv"
    write_quiesce_coordinator_ledger "$orphan_child"
    printf 'normal\n' > "$orphan_child/snapshot-transition.state"
    chmod 0600 -- "$orphan_child/service-states.tsv" \
        "$orphan_child/snapshot-transition.state"
    sync -f -- "$orphan_child/service-states.tsv" \
        "$orphan_child/quiesce-coordinators.tsv" "$orphan_child/snapshot-transition.state" "$orphan_child" \
        "$orphan_stage" "$snapshot_root"
    release_txn_validate_update_snapshot_stage \
        "$snapshot_root" "$orphan_snapshot" "$orphan_stage"
    kill -KILL "$BASHPID"
); then
    fail "pre-marker SIGKILL boundary unexpectedly returned success"
else
    orphan_kill_status=$?
fi
[[ "$orphan_kill_status" -eq 137 ]] \
    || fail "pre-marker SIGKILL boundary returned $orphan_kill_status, want 137"
[[ ! -e "$transaction_root/active" && ! -L "$transaction_root/active" &&
   ! -e "$transaction_root/completion.pending" && ! -L "$transaction_root/completion.pending" ]] \
    || fail "pre-marker SIGKILL unexpectedly published a transaction marker"
release_txn_cleanup_unmarked_update_snapshot_stage \
    "$transaction_root" "$lock_fd" "$snapshot_root" \
    || fail "canonical markerless staging cleanup failed"
[[ ! -e "$orphan_stage" && ! -L "$orphan_stage" ]] \
    || fail "canonical markerless staging cleanup left the orphan"

# Ambiguous or malformed markerless entries are never guessed or deleted.
# Belirsiz veya bozuk işaretçisiz girdiler asla tahmin edilmez ya da silinmez.
ambiguous_one="$snapshot_root/.release-snapshot.incomplete.101.$(printf 'e%.0s' {1..32})"
ambiguous_two="$snapshot_root/.release-snapshot.incomplete.102.$(printf 'f%.0s' {1..32})"
mkdir -m 0700 -- "$ambiguous_one" "$ambiguous_two"
expect_failure "multiple markerless stages were accepted for cleanup" \
    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$transaction_root" "$lock_fd" "$snapshot_root"
[[ -d "$ambiguous_one" && -d "$ambiguous_two" ]] \
    || fail "ambiguous markerless cleanup deleted an entry"
rmdir -- "$ambiguous_one" "$ambiguous_two"
malformed_stage="$snapshot_root/.release-snapshot.incomplete.invalid"
mkdir -m 0700 -- "$malformed_stage"
expect_failure "malformed markerless stage was accepted for cleanup" \
    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$transaction_root" "$lock_fd" "$snapshot_root"
[[ -d "$malformed_stage" ]] \
    || fail "malformed markerless cleanup deleted the unsafe entry"
rmdir -- "$malformed_stage"

recovery_target=$(printf 'a%.0s' {1..40})
recovery_nonce=$(printf 'b%.0s' {1..32})
recovery_snapshot="20260728T120000Z-from-unknown-to-$recovery_target-$recovery_nonce"
recovery_stage="$snapshot_root/.release-snapshot.incomplete.$BASHPID.$recovery_nonce"
recovery_child="$recovery_stage/$recovery_snapshot"
mkdir -m 0700 -- "$recovery_stage" "$recovery_child"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    > "$recovery_child/service-states.tsv"
write_quiesce_coordinator_ledger "$recovery_child"
printf 'normal\n' > "$recovery_child/snapshot-transition.state"
printf 'partial\n' > "$recovery_child/.partial-payload"
chmod 0600 -- "$recovery_child/service-states.tsv" \
    "$recovery_child/snapshot-transition.state" "$recovery_child/.partial-payload"
sync -f -- "$recovery_child/service-states.tsv" \
    "$recovery_child/quiesce-coordinators.tsv" "$recovery_child/snapshot-transition.state" "$recovery_child" \
    "$recovery_stage" "$snapshot_root"
release_txn_validate_update_snapshot_stage \
    "$snapshot_root" "$recovery_snapshot" "$recovery_stage" \
    || fail "durable pre-marker recovery stage was rejected"

recovery_token=$(release_txn_generate_token)
if (
    release_txn_create_active_marker \
        "$transaction_root" "$lock_fd" "$recovery_token" update "$recovery_snapshot"
    kill -KILL "$BASHPID"
); then
    fail "SIGKILL recovery boundary unexpectedly returned success"
else
    recovery_kill_status=$?
fi
[[ "$recovery_kill_status" -eq 137 ]] \
    || fail "SIGKILL recovery boundary returned $recovery_kill_status, want 137"
IFS=$'\t' read -r recovered_token recovered_operation recovered_snapshot \
    < <(release_txn_read_active_fields "$transaction_root")
[[ "$recovered_token" == "$recovery_token" &&
   "$recovered_operation" == update &&
   "$recovered_snapshot" == "$recovery_snapshot" ]] \
    || fail "SIGKILL did not preserve the exact active marker"
found_recovery_stage=$(release_txn_find_update_snapshot_stage \
    "$snapshot_root" "$recovery_snapshot")
[[ "$found_recovery_stage" == "$recovery_stage" ]] \
    || fail "SIGKILL recovery did not find the exact staging tree"
release_txn_reset_update_snapshot_stage \
    "$snapshot_root" "$recovery_snapshot" "$recovery_stage" \
    || fail "SIGKILL recovery could not reset the partial staging payload"
[[ ! -e "$recovery_child/.partial-payload" && ! -L "$recovery_child/.partial-payload" ]] \
    || fail "SIGKILL recovery left a partial payload"
printf 'normal\n' | cmp -s - "$recovery_child/snapshot-transition.state" \
    || fail "SIGKILL recovery changed the transition mode"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service disabled inactive \
    | cmp -s - "$recovery_child/service-states.tsv" \
    || fail "SIGKILL recovery changed the service-state ledger"
printf '%s\t%s\t%s\t%s\n' \
    celikpanel-agent.service active 4242 1 \
    celikpanel-panel.service active 4243 2 \
    | cmp -s - "$recovery_child/quiesce-coordinators.tsv" \
    || fail "SIGKILL recovery changed the coordinator identity ledger"
mkdir -m 0700 -- "$snapshot_root/$recovery_snapshot"
expect_failure "final snapshot permitted pre-mutation update resume" \
    release_txn_find_update_snapshot_stage "$snapshot_root" "$recovery_snapshot"
rmdir -- "$snapshot_root/$recovery_snapshot"
release_txn_mark_completion_pending \
    "$transaction_root" "$lock_fd" "$recovery_token" update "$recovery_snapshot"
complete_scheduler_restore_obligation \
    "$transaction_root" "$lock_fd" "$recovery_token" update "$recovery_snapshot"

# The terminal active-marker primitive is intentionally smaller than the
# incident helper: it proves only lock/marker/stage identity and durable
# removal. The caller remains responsible for the exact pre-mutation payload,
# stopped coordinators and unchanged installed-byte proofs.
release_txn_cleanup_unmarked_update_snapshot_stage \
    "$transaction_root" "$lock_fd" "$snapshot_root" \
    || fail "completed recovery fixture stage cleanup failed"
[[ ! -e "$recovery_stage" && ! -L "$recovery_stage" ]] \
    || fail "completed recovery fixture stage remains"

pre_abort_target=$(printf '9%.0s' {1..40})
pre_abort_nonce=$(printf '8%.0s' {1..32})
pre_abort_snapshot="20260728T120001Z-from-unknown-to-$pre_abort_target-$pre_abort_nonce"
pre_abort_stage="$snapshot_root/.release-snapshot.incomplete.$BASHPID.$pre_abort_nonce"
pre_abort_child="$pre_abort_stage/$pre_abort_snapshot"
mkdir -m 0700 -- "$pre_abort_stage" "$pre_abort_child"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service not-found inactive \
    > "$pre_abort_child/service-states.tsv"
write_quiesce_coordinator_ledger "$pre_abort_child"
printf 'pre-ledger\n' > "$pre_abort_child/snapshot-transition.state"
chmod 0600 -- "$pre_abort_child/service-states.tsv" \
    "$pre_abort_child/snapshot-transition.state"
sync -f -- "$pre_abort_child/service-states.tsv" \
    "$pre_abort_child/quiesce-coordinators.tsv" \
    "$pre_abort_child/snapshot-transition.state" "$pre_abort_child" \
    "$pre_abort_stage" "$snapshot_root"
release_txn_validate_update_snapshot_stage \
    "$snapshot_root" "$pre_abort_snapshot" "$pre_abort_stage" \
    || fail "canonical pre-mutation abort fixture stage was rejected"

pre_abort_token=$(release_txn_generate_token)
release_txn_create_active_marker \
    "$transaction_root" "$lock_fd" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "cannot publish pre-mutation abort fixture marker"

expect_failure "rollback operation removed the pre-mutation update marker" \
    release_txn_remove_pre_mutation_active_marker \
        "$transaction_root" "$lock_fd" "$pre_abort_token" rollback \
        "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage"
release_txn_validate_active_token \
    "$transaction_root" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "wrong-operation abort did not preserve the exact active marker"

wrong_pre_abort_token=$(printf '7%.0s' {1..64})
expect_failure "wrong token removed the pre-mutation active marker" \
    release_txn_remove_pre_mutation_active_marker \
        "$transaction_root" "$lock_fd" "$wrong_pre_abort_token" update \
        "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage"
release_txn_validate_active_token \
    "$transaction_root" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "wrong-token abort did not preserve the exact active marker"

expect_failure "wrong stage removed the pre-mutation active marker" \
    release_txn_remove_pre_mutation_active_marker \
        "$transaction_root" "$lock_fd" "$pre_abort_token" update \
        "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage.not-reviewed"
release_txn_validate_active_token \
    "$transaction_root" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "wrong-stage abort did not preserve the exact active marker"

mkdir -m 0700 -- "$snapshot_root/$pre_abort_snapshot"
expect_failure "existing final snapshot permitted pre-mutation active abort" \
    release_txn_remove_pre_mutation_active_marker \
        "$transaction_root" "$lock_fd" "$pre_abort_token" update \
        "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage"
release_txn_validate_active_token \
    "$transaction_root" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "final-snapshot rejection did not preserve the exact active marker"
rmdir -- "$snapshot_root/$pre_abort_snapshot"

cp -- "$transaction_root/active" "$transaction_root/quiesce.pending"
chmod 0600 -- "$transaction_root/quiesce.pending"
sync -f -- "$transaction_root/quiesce.pending" "$transaction_root"
expect_failure "coexisting quiesce marker permitted pre-mutation active abort" \
    release_txn_remove_pre_mutation_active_marker \
        "$transaction_root" "$lock_fd" "$pre_abort_token" update \
        "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage"
release_txn_validate_active_token \
    "$transaction_root" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "coexisting-marker rejection did not preserve the exact active marker"
rm -f -- "$transaction_root/quiesce.pending"
sync -f -- "$transaction_root"

cp -- "$transaction_root/active" "$transaction_root/completion.pending"
chmod 0600 -- "$transaction_root/completion.pending"
sync -f -- "$transaction_root/completion.pending" "$transaction_root"
expect_failure "coexisting completion marker permitted pre-mutation active abort" \
    release_txn_remove_pre_mutation_active_marker \
        "$transaction_root" "$lock_fd" "$pre_abort_token" update \
        "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage"
release_txn_validate_active_token \
    "$transaction_root" "$pre_abort_token" update "$pre_abort_snapshot" \
    || fail "completion-marker rejection did not preserve the exact active marker"
rm -f -- "$transaction_root/completion.pending"
sync -f -- "$transaction_root"

release_txn_remove_pre_mutation_active_marker \
    "$transaction_root" "$lock_fd" "$pre_abort_token" update \
    "$pre_abort_snapshot" "$snapshot_root" "$pre_abort_stage" \
    || fail "exact pre-mutation active abort was rejected"
[[ ! -e "$transaction_root/active" && ! -L "$transaction_root/active" ]] \
    || fail "exact pre-mutation active abort left its marker"
[[ -d "$pre_abort_stage" && ! -L "$pre_abort_stage" ]] \
    || fail "terminal marker primitive removed the evidence stage"
release_txn_cleanup_unmarked_update_snapshot_stage \
    "$transaction_root" "$lock_fd" "$snapshot_root" \
    || fail "pre-mutation abort fixture stage cleanup failed"
[[ ! -e "$pre_abort_stage" && ! -L "$pre_abort_stage" ]] \
    || fail "pre-mutation abort fixture stage remains"

flock -u "$lock_fd"
exec {lock_fd}>&-
printf 'PASS: release transaction guard fixture\n'
