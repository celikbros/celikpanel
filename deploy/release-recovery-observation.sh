#!/usr/bin/env bash
# Best-effort observations only. Never use these records for mutation admission,
# transaction recovery selection, or authorization. The caller has verified this
# source as part of its complete retained release. Public bytes use the shared
# internal/recoveryobs schema; private bindings never become HTTP responses.
if [[ ${BASH_SOURCE[0]} == "$0" ]]; then exit 1; fi
RELEASE_OBSERVATION_ROOT=/var/lib/celikpanel-recovery-observations
RELEASE_OBSERVATION_BINDINGS=/var/lib/celikpanel-release-state/recovery-observation-bindings

_release_observation_file() {
    local path=$1 mode=$2 gid=$3 limit=$4 owner group actual links size
    [[ -f $path && ! -L $path && $(readlink -e -- "$path") == "$path" ]] || return 1
    read -r owner group actual links size < <(stat -Lc '%u %g %a %h %s' -- "$path") || return 1
    [[ $owner == 0 && $group == "$gid" && $actual == "$mode" && $links == 1 && $size -le $limit ]]
}

_release_observation_root() {
    local path=$1 mode=$2 gid=$3 owner group actual parent
    parent=$(dirname -- "$path") || return 1
    release_recovery_validate_root_chain "$parent" 2>/dev/null || return 1
    if [[ ! -e $path && ! -L $path ]]; then
        mkdir -m "$mode" -- "$path" || return 1
        chown "0:$gid" -- "$path" || return 1
        sync -f -- "$path" "$parent" || return 1
    fi
    [[ -d $path && ! -L $path && $(readlink -e -- "$path") == "$path" ]] || return 1
    read -r owner group actual < <(stat -Lc '%u %g %a' -- "$path") || return 1
    [[ $owner == 0 && $group == "$gid" && $actual == "${mode#0}" ]]
}

_release_observation_gid() {
    local group_line name password gid members
    group_line=$(getent group celikpanel) || return 1
    [[ $group_line != *$'\n'* ]] || return 1
    IFS=: read -r name password gid members <<< "$group_line"
    [[ $name == celikpanel && $gid =~ ^[1-9][0-9]*$ && ${#gid} -le 10 ]] || return 1
    printf '%s\n' "$gid"
}

_release_observation_valid_fields() {
    local phase=$1 proof=$2 reason=$3 previous=$4 observed=$5
    case "$phase:$proof:$reason" in
        accepted:none:operation_accepted|running:none:update_running|recovering:none:recovery_running|failed:none:update_failed|recovery_required:none:recovery_failed|recovery_required:none:recovery_incomplete|succeeded:update_verified:update_verified|recovered:rollback_verified:rollback_verified) ;;
        *) return 1 ;;
    esac
    case "$previous" in none|update_failed|recovery_failed|recovery_incomplete) ;; *) return 1 ;; esac
    [[ $observed =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]] || return 1
    [[ $(date -u -d "$observed" +%Y-%m-%dT%H:%M:%SZ) == "$observed" ]]
}

_release_observation_read() {
    local id=$1 gid=$2 path=$RELEASE_OBSERVATION_ROOT/$1.status
    local -a lines=()
    [[ $id =~ ^[0-9a-f]{32}$ ]] || return 1
    _release_observation_file "$path" 640 "$gid" 2048 || return 1
    mapfile -t lines < "$path" || return 1
    [[ ${#lines[@]} == 8 && ${lines[0]} == schema=celikpanel-recovery-observation/v1 &&
       ${lines[1]} == "request_id=$id" && ${lines[2]} =~ ^target_commit=[0-9a-f]{40}$ &&
       ${lines[3]} == phase=* && ${lines[4]} == terminal_proof=* && ${lines[5]} == reason=* &&
       ${lines[6]} == observed_at=* && ${lines[7]} == previous_failure=* ]] || return 1
    OBSERVATION_COMMIT=${lines[2]#target_commit=}
    OBSERVATION_PHASE=${lines[3]#phase=}
    OBSERVATION_PROOF=${lines[4]#terminal_proof=}
    OBSERVATION_REASON=${lines[5]#reason=}
    OBSERVATION_AT=${lines[6]#observed_at=}
    OBSERVATION_PREVIOUS=${lines[7]#previous_failure=}
    _release_observation_valid_fields "$OBSERVATION_PHASE" "$OBSERVATION_PROOF" \
        "$OBSERVATION_REASON" "$OBSERVATION_PREVIOUS" "$OBSERVATION_AT" || return 1
    cmp -s -- "$path" <(printf '%s\n' "${lines[@]}")
}

release_observation_publish() (
    local id=$1 commit=$2 phase=$3 proof=$4 reason=$5 gid now previous=none path stage lock_fd
    [[ $EUID == 0 && $id =~ ^[0-9a-f]{32}$ && $commit =~ ^[0-9a-f]{40}$ ]] || return 1
    now=$(date -u +%Y-%m-%dT%H:%M:%SZ) || return 1
    _release_observation_valid_fields "$phase" "$proof" "$reason" none "$now" || return 1
    gid=$(_release_observation_gid) || return 1
    _release_observation_root "$RELEASE_OBSERVATION_ROOT" 0750 "$gid" || return 1
    path=$RELEASE_OBSERVATION_ROOT/$id.status
    if [[ ! -e $RELEASE_OBSERVATION_ROOT/.publish.lock && ! -L $RELEASE_OBSERVATION_ROOT/.publish.lock ]]; then
        (umask 077; set -o noclobber; : > "$RELEASE_OBSERVATION_ROOT/.publish.lock") || return 1
    fi
    _release_observation_file "$RELEASE_OBSERVATION_ROOT/.publish.lock" 600 0 0 || return 1
    exec {lock_fd}<>"$RELEASE_OBSERVATION_ROOT/.publish.lock" || return 1
    flock -n -x "$lock_fd" || return 1
    [[ $(stat -Lc '%d:%i' -- "$RELEASE_OBSERVATION_ROOT/.publish.lock") == \
       $(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$lock_fd") ]] || return 1
    if [[ -e $path || -L $path ]]; then
        _release_observation_read "$id" "$gid" || return 1
        [[ $OBSERVATION_COMMIT == "$commit" ]] || return 1
        # Late worker failure cannot erase native terminal verification.
        [[ $OBSERVATION_PROOF == none ]] || return 0
        [[ $now > "$OBSERVATION_AT" || $now == "$OBSERVATION_AT" ]] || return 1
        previous=$OBSERVATION_PREVIOUS
    fi
    [[ $phase != failed && $phase != recovery_required ]] || previous=$reason
    stage=$(mktemp "$RELEASE_OBSERVATION_ROOT/.observation-XXXXXXXX") || return 1
    trap 'rm -f -- "$stage"' EXIT
    printf '%s\n' schema=celikpanel-recovery-observation/v1 "request_id=$id" \
        "target_commit=$commit" "phase=$phase" "terminal_proof=$proof" "reason=$reason" \
        "observed_at=$now" "previous_failure=$previous" > "$stage" || return 1
    chown "0:$gid" -- "$stage" && chmod 0640 -- "$stage" && sync -f -- "$stage" || return 1
    _release_observation_file "$stage" 640 "$gid" 2048 || return 1
    if [[ -e $path || -L $path ]]; then _release_observation_read "$id" "$gid" || return 1; fi
    mv -T -- "$stage" "$path" && sync -f -- "$RELEASE_OBSERVATION_ROOT"
)

# Native descendants retain their worker's cgroup across get.sh/bootstrap env -i.
# Accept only the exact system unit in the kernel's v2 or named-systemd hierarchy.
# An unsupported layout is unavailable, never a search for the latest worker.
_release_observation_parse_worker_request() {
    local line candidate id= matches=0
    while IFS= read -r line; do
        if [[ $line =~ ^(0::|[0-9]+:name=systemd:)/system.slice/celikpanel-self-update-([0-9a-f]{32})\.service$ ]]; then
            candidate=${BASH_REMATCH[2]}
            [[ -z $id || $id == "$candidate" ]] || return 1
            id=$candidate
            matches=$((matches + 1))
        fi
    done
    [[ $matches -gt 0 && $id =~ ^[0-9a-f]{32}$ ]] || return 1
    printf '%s\n' "$id"
}

release_observation_worker_request() {
    _release_observation_parse_worker_request < "/proc/$BASHPID/cgroup"
}
_release_observation_binding_read() {
    local snapshot=$1 path=$RELEASE_OBSERVATION_BINDINGS/$1.binding
    local -a lines=()
    release_txn_parse_update_snapshot_name "$snapshot" >/dev/null || return 1
    _release_observation_file "$path" 600 0 1024 || return 1
    mapfile -t lines < "$path" || return 1
    [[ ${#lines[@]} == 5 && ${lines[0]} == schema=celikpanel-recovery-binding/v1 &&
       ${lines[1]} =~ ^request_id=[0-9a-f]{32}$ && ${lines[2]} =~ ^target_commit=[0-9a-f]{40}$ &&
       ${lines[3]} == "snapshot=$snapshot" && ${lines[4]} =~ ^update_token=[0-9a-f]{64}$ ]] || return 1
    cmp -s -- "$path" <(printf '%s\n' "${lines[@]}") || return 1
    OBSERVATION_REQUEST=${lines[1]#request_id=}
    OBSERVATION_BINDING_COMMIT=${lines[2]#target_commit=}
    OBSERVATION_BINDING_TOKEN=${lines[4]#update_token=}
    [[ $snapshot == *-to-$OBSERVATION_BINDING_COMMIT-* ]]
}

# Create an immutable request/snapshot/token association before the first marker
# is published. Existing records must match byte-for-byte; no evidence repair.
release_observation_bind_update() (
    local transaction_root=$1 fd=$2 token=$3 snapshot=$4 commit=$5 id gid stage path
    release_txn_verify_inherited_lock "$transaction_root" "$fd" 2>/dev/null || return 1
    release_txn_validate_token "$token" || return 1
    release_txn_parse_update_snapshot_name "$snapshot" >/dev/null || return 1
    [[ $commit =~ ^[0-9a-f]{40}$ && $snapshot == *-to-$commit-* ]] || return 1
    id=$(release_observation_worker_request) || return 1
    gid=$(_release_observation_gid) || return 1
    _release_observation_root "$RELEASE_OBSERVATION_ROOT" 0750 "$gid" || return 1
    _release_observation_read "$id" "$gid" || return 1
    [[ $OBSERVATION_COMMIT == "$commit" && $OBSERVATION_PROOF == none &&
       ( $OBSERVATION_PHASE == accepted || $OBSERVATION_PHASE == running ) ]] || return 1
    _release_observation_root "$RELEASE_OBSERVATION_BINDINGS" 0700 0 || return 1
    path=$RELEASE_OBSERVATION_BINDINGS/$snapshot.binding
    if [[ -e $path || -L $path ]]; then
        _release_observation_binding_read "$snapshot" || return 1
        [[ $OBSERVATION_REQUEST == "$id" && $OBSERVATION_BINDING_COMMIT == "$commit" && $OBSERVATION_BINDING_TOKEN == "$token" ]]
        return
    fi
    stage=$(mktemp "$RELEASE_OBSERVATION_BINDINGS/.binding-XXXXXXXX") || return 1
    trap 'rm -f -- "$stage"' EXIT
    printf '%s\n' schema=celikpanel-recovery-binding/v1 "request_id=$id" \
        "target_commit=$commit" "snapshot=$snapshot" "update_token=$token" > "$stage" || return 1
    chmod 0600 -- "$stage" && chown 0:0 -- "$stage" && sync -f -- "$stage" || return 1
    release_txn_verify_inherited_lock "$transaction_root" "$fd" 2>/dev/null || return 1
    mv -T --no-clobber -- "$stage" "$path" || return 1
    [[ ! -e $stage ]] || return 1
    sync -f -- "$RELEASE_OBSERVATION_BINDINGS"
)

# A rollback token changes on takeover; its unique, manifest-bound snapshot is
# still the same compensation target. The caller separately proves the current
# token/operation/phase under its held lock. The immutable binding supplies only
# observation identity, never permission to dispatch that compensation.
release_observation_bind_recovery() {
    local transaction_root=$1 fd=$2 token=$3 operation=$4 snapshot=$5 commit=$6
    local owner group mode
    release_txn_verify_inherited_lock "$transaction_root" "$fd" 2>/dev/null || return 1
    release_txn_validate_token "$token" || return 1
    [[ $operation == update || $operation == rollback ]] || return 1
    [[ -d $RELEASE_OBSERVATION_BINDINGS && ! -L $RELEASE_OBSERVATION_BINDINGS &&
       $(readlink -e -- "$RELEASE_OBSERVATION_BINDINGS") == "$RELEASE_OBSERVATION_BINDINGS" ]] || return 1
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$RELEASE_OBSERVATION_BINDINGS") || return 1
    [[ $owner:$group:$mode == 0:0:700 ]] || return 1
    release_recovery_validate_root_chain "$RELEASE_OBSERVATION_BINDINGS" 2>/dev/null || return 1
    _release_observation_binding_read "$snapshot" || return 1
    [[ $OBSERVATION_BINDING_COMMIT == "$commit" ]] || return 1
    [[ $operation != update || $OBSERVATION_BINDING_TOKEN == "$token" ]]
}
