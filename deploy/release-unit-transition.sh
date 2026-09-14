#!/usr/bin/env bash

# Source only after the complete immutable release manifest and the shared
# release-transaction-guard.sh have been verified. These helpers prove file
# transitions, not operation authority: callers must first verify the complete
# outer v6 snapshot, its exact target release, and the current active transaction
# token. Callers retain the coordinator/mutation barriers throughout publication.
# This library never reloads systemd, grants starts, or changes enablement.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    printf '%s\n' 'release unit transition library must be sourced' >&2
    exit 1
fi
_release_unit_source_root=${CODE_ROOT:-${TRUSTED_RELEASE_ROOT:-${CELIKPANEL_TRUSTED_RELEASE_ROOT:-}}}
if [[ -z $_release_unit_source_root || $_release_unit_source_root != /* ||
      $(readlink -e -- "$_release_unit_source_root") != "$_release_unit_source_root" ||
      $(readlink -e -- "${BASH_SOURCE[0]}") != "$_release_unit_source_root/deploy/release-unit-transition.sh" ]] ||
   ! declare -F release_txn_verify_inherited_lock >/dev/null; then
    printf '%s\n' 'release unit transition library requires its verified release and transaction guard' >&2
    return 1
fi
unset _release_unit_source_root

_release_unit_fail() {
    printf 'release unit transition: %s\n' "$1" >&2
    return 1
}

# A complete snapshot supplies the old state; the verified target release
# supplies the candidate state. Per-file mixtures are legitimate interrupted
# publications. Neither an active marker alone nor metadata alone proves bytes.
release_unit_validate_transition() {
    [[ $# -eq 7 ]] || { _release_unit_fail 'expected seven transition arguments'; return 1; }
    local transaction_root=$1 inherited_fd=$2 snapshot_units=$3 candidate_units=$4
    local systemd_root=$5 firewall_state=$6 root_identity=$7 unit target
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    release_txn_verify_systemd_unit_root_identity "$systemd_root" "$root_identity" || return 1
    release_txn_validate_celikpanel_unit_snapshot "$snapshot_units" "$firewall_state" || return 1
    _release_txn_validate_secure_parent_directory "$candidate_units" || return 1
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        _release_txn_validate_celikpanel_unit_file "$candidate_units/$unit" 'candidate systemd unit' || return 1
        target=$systemd_root/$unit
        if [[ ! -e $target && ! -L $target ]]; then
            [[ $unit == celikpanel-firewall-restore.service && $firewall_state == absent ]] ||
                { _release_unit_fail "installed unit is unexpectedly absent: $unit"; return 1; }
            continue
        fi
        _release_txn_validate_celikpanel_unit_file "$target" 'installed systemd unit' || return 1
        if cmp -s -- "$target" "$candidate_units/$unit"; then
            continue
        fi
        if [[ -f $snapshot_units/$unit ]] && cmp -s -- "$target" "$snapshot_units/$unit"; then
            continue
        fi
        _release_unit_fail "installed unit differs from both verified states; preserve owner changes: $unit"
        return 1
    done
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    release_txn_verify_systemd_unit_root_identity "$systemd_root" "$root_identity"
}

# Staging is beside the fixed destination, so rename cannot cross filesystems.
# The ignored staging filename is not a systemd unit. A crash may leave staging
# behind; a retry does not infer authority from, consume, or remove that file.
_release_unit_replace_transition_file() {
    local source=$1 unit=$2
    shift 2
    local systemd_root=$5 target stage
    target=$systemd_root/$unit
    release_unit_validate_transition "$@" || return 1
    if [[ -f $target ]] && cmp -s -- "$source" "$target"; then
        return 0
    fi
    stage=$(mktemp "$systemd_root/.celikpanel-unit.XXXXXXXX") ||
        { _release_unit_fail "cannot stage unit: $unit"; return 1; }
    if ! cp --no-preserve=mode,ownership,timestamps -- "$source" "$stage" ||
       ! chown root:root -- "$stage" || ! chmod 0644 -- "$stage" ||
       ! _release_txn_validate_celikpanel_unit_file "$stage" 'staged systemd unit' ||
       ! cmp -s -- "$source" "$stage" || ! sync -f -- "$stage"; then
        rm -f -- "$stage"
        _release_unit_fail "cannot prepare durable unit stage: $unit"
        return 1
    fi
    # Repeat after staging, immediately before replacing any live pathname.
    # A later owner edit must not be overwritten using the earlier admission.
    if ! release_unit_validate_transition "$@" ||
       ! _release_txn_validate_celikpanel_unit_file "$stage" 'staged systemd unit' ||
       ! cmp -s -- "$source" "$stage"; then
        rm -f -- "$stage"
        return 1
    fi
    if ! mv -T -- "$stage" "$target"; then
        rm -f -- "$stage"
        _release_unit_fail "cannot publish unit atomically: $unit"
        return 1
    fi
    sync -f -- "$target" "$systemd_root" ||
        { _release_unit_fail "published unit durability is not confirmed: $unit"; return 1; }
    _release_txn_validate_celikpanel_unit_file "$target" 'published systemd unit' || return 1
    cmp -s -- "$source" "$target" ||
        { _release_unit_fail "published unit differs from intended bytes: $unit"; return 1; }
    release_unit_validate_transition "$@"
}

release_unit_publish_transition() {
    release_unit_validate_transition "$@" || return 1
    local candidate_units=$4 unit
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        _release_unit_replace_transition_file "$candidate_units/$unit" "$unit" "$@" || return 1
    done
    release_unit_validate_transition "$@" || return 1
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        cmp -s -- "$candidate_units/$unit" "$5/$unit" ||
            { _release_unit_fail "candidate unit changed before publication completed: $unit"; return 1; }
    done
}

release_unit_restore_transition() {
    release_unit_validate_transition "$@" || return 1
    local snapshot_units=$3 systemd_root=$5 firewall_state=$6 unit
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        _release_unit_replace_transition_file "$snapshot_units/$unit" "$unit" "$@" || return 1
    done
    unit=celikpanel-firewall-restore.service
    if [[ $firewall_state == present ]]; then
        _release_unit_replace_transition_file "$snapshot_units/$unit" "$unit" "$@" || return 1
    elif [[ -e $systemd_root/$unit || -L $systemd_root/$unit ]]; then
        release_unit_validate_transition "$@" || return 1
        rm -f -- "$systemd_root/$unit" ||
            { _release_unit_fail 'cannot restore verified firewall-unit absence'; return 1; }
        sync -f -- "$systemd_root" ||
            { _release_unit_fail 'firewall-unit absence durability is not confirmed'; return 1; }
        [[ ! -e $systemd_root/$unit && ! -L $systemd_root/$unit ]] ||
            { _release_unit_fail 'firewall unit remains after restoring absence'; return 1; }
    fi
    release_unit_validate_transition "$@" || return 1
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        cmp -s -- "$snapshot_units/$unit" "$systemd_root/$unit" ||
            { _release_unit_fail "restored unit changed before restoration completed: $unit"; return 1; }
    done
    unit=celikpanel-firewall-restore.service
    if [[ $firewall_state == present ]]; then
        cmp -s -- "$snapshot_units/$unit" "$systemd_root/$unit" ||
            { _release_unit_fail 'restored firewall unit changed before restoration completed'; return 1; }
    else
        [[ ! -e $systemd_root/$unit && ! -L $systemd_root/$unit ]] ||
            { _release_unit_fail 'restored firewall absence changed before restoration completed'; return 1; }
    fi
}
