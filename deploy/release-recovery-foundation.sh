#!/usr/bin/env bash
# Shared, side-effect-free verification and atomic commit helpers for the
# monotonic release-recovery foundation. Callers already hold the persistent
# release transaction lock whenever an update or rollback is in progress.

release_recovery_foundation_fail() {
    printf 'release recovery foundation: %s\n' "$*" >&2
    return 1
}

release_recovery_valid_sequence() {
    local value=$1 LC_ALL=C
    [[ $value =~ ^[1-9][0-9]*$ && ${#value} -le 19 ]] || return 1
    [[ ${#value} -lt 19 || $value < 9223372036854775807 ||
       $value == 9223372036854775807 ]]
}

release_recovery_sequence_gt() {
    local left=$1 right=$2 LC_ALL=C
    release_recovery_valid_sequence "$left" &&
        release_recovery_valid_sequence "$right" || return 1
    [[ ${#left} -gt ${#right} ||
       ( ${#left} -eq ${#right} && $left > $right ) ]]
}

release_recovery_validate_root_chain() {
    local path=$1 current canonical owner group mode permissions
    [[ $path == /* ]] ||
        { release_recovery_foundation_fail "trusted path is not absolute: $path"; return 1; }
    current=$path
    while true; do
        [[ -d $current && ! -L $current ]] ||
            { release_recovery_foundation_fail "trusted directory is missing or symbolic: $current"; return 1; }
        canonical=$(readlink -e -- "$current") || return 1
        [[ $canonical == "$current" ]] ||
            { release_recovery_foundation_fail "trusted directory is not canonical: $current"; return 1; }
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") || return 1
        permissions=$((8#$mode))
        [[ $owner == 0 && $group == 0 ]] && (( (permissions & 0022) == 0 )) ||
            { release_recovery_foundation_fail "trusted directory metadata is unsafe: $current; stage the verified release below /var/backups/celikpanel in a canonical root-owned, non-group/other-writable directory"; return 1; }
        [[ $current == / ]] && break
        current=$(dirname -- "$current")
    done
}

release_recovery_source_identity() {
    local root=$1 policy protocol commit_file
    local -a lines
    policy=$root/deploy/release-sequence-policy
    protocol=$root/deploy/release-recovery.protocol
    commit_file=$root/release.commit
    [[ -f $policy && ! -L $policy && -f $protocol && ! -L $protocol &&
       -f $commit_file && ! -L $commit_file ]] ||
        release_recovery_foundation_fail 'trusted source identity files are missing' || return 1
    mapfile -t lines < "$policy"
    [[ ${#lines[@]} -eq 6 &&
       ${lines[0]} == format=celikpanel-release-sequence-policy-v1 &&
       ${lines[1]} == version=v* &&
       ${lines[2]} == current=* &&
       ${lines[3]} == previous=* &&
       ${lines[4]} == previous_version=v* &&
       ${lines[5]} == previous_commit=* ]] ||
        release_recovery_foundation_fail 'release sequence policy is noncanonical' || return 1
    RELEASE_RECOVERY_SOURCE_VERSION=${lines[1]#version=}
    RELEASE_RECOVERY_SOURCE_SEQUENCE=${lines[2]#current=}
    RELEASE_RECOVERY_SOURCE_PREVIOUS=${lines[3]#previous=}
    RELEASE_RECOVERY_SOURCE_COMMIT=$(tr -d '[:space:]' < "$commit_file")
    release_recovery_valid_sequence "$RELEASE_RECOVERY_SOURCE_SEQUENCE" &&
        release_recovery_valid_sequence "$RELEASE_RECOVERY_SOURCE_PREVIOUS" &&
        release_recovery_sequence_gt "$RELEASE_RECOVERY_SOURCE_SEQUENCE" \
            "$RELEASE_RECOVERY_SOURCE_PREVIOUS" ||
        { release_recovery_foundation_fail 'release sequence policy does not advance monotonically'; return 1; }
    [[ $RELEASE_RECOVERY_SOURCE_VERSION =~ ^v[0-9A-Za-z.-]+$ &&
       $RELEASE_RECOVERY_SOURCE_COMMIT =~ ^[0-9a-f]{40}$ &&
       ${lines[4]} == "previous_version=${lines[4]#previous_version=}" &&
       ${lines[5]} =~ ^previous_commit=[0-9a-f]{40}$ ]] ||
        { release_recovery_foundation_fail 'release source identity is invalid'; return 1; }
    cmp -s -- "$protocol" \
        <(printf '%s\n' format=celikpanel-release-recovery-protocol-v1 protocol=1) ||
        { release_recovery_foundation_fail 'release recovery protocol is unsupported'; return 1; }
}

release_recovery_validate_installed_file() {
    local path=$1 mode=$2 source=${3:-} owner group actual_mode links
    [[ -f $path && ! -L $path ]] ||
        { release_recovery_foundation_fail "unsafe installed file: $path"; return 1; }
    read -r owner group actual_mode links < <(stat -Lc '%u %g %a %h' -- "$path") ||
        { release_recovery_foundation_fail "cannot inspect installed file: $path"; return 1; }
    [[ $owner == 0 && $group == 0 && $actual_mode == "${mode#0}" && $links == 1 ]] ||
        { release_recovery_foundation_fail "installed metadata mismatch: $path"; return 1; }
    if [[ -n $source ]]; then
        [[ -f $source && ! -L $source ]] ||
            { release_recovery_foundation_fail "unsafe trusted source file: $source"; return 1; }
        cmp -s -- "$source" "$path" ||
            { release_recovery_foundation_fail "installed bytes differ from trusted source: $path"; return 1; }
    fi
}

release_recovery_sha256() {
    local path=$1 digest
    digest=$(sha256sum -- "$path") || return 1
    digest=${digest%% *}
    [[ $digest =~ ^[0-9a-f]{64}$ ]] || return 1
    printf '%s\n' "$digest"
}

release_recovery_validate_source_file() {
    local path=$1 owner group mode links permissions
    [[ -f $path && ! -L $path && $(readlink -e -- "$path") == "$path" ]] ||
        { release_recovery_foundation_fail "unsafe recovery source: $path"; return 1; }
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") || return 1
    permissions=$((8#$mode))
    [[ $owner == 0 && $group == 0 && $links == 1 ]] &&
        (( (permissions & 0022) == 0 )) ||
        { release_recovery_foundation_fail "recovery source metadata is unsafe: $path"; return 1; }
}

release_recovery_target_prefix() {
    local start_guard=$1 suffix=/usr/libexec/celikpanel/release-transaction-start-guard
    [[ $start_guard == *"$suffix" ]] ||
        { release_recovery_foundation_fail 'start-guard target is not canonical'; return 1; }
    RELEASE_RECOVERY_TARGET_PREFIX=${start_guard%"$suffix"}
    [[ -z $RELEASE_RECOVERY_TARGET_PREFIX || $RELEASE_RECOVERY_TARGET_PREFIX == /* ]] || return 1
}

release_recovery_emit_dropin() {
    local start_guard=$1
    release_recovery_target_prefix "$start_guard" || return 1
    printf '[Service]\nExecCondition=+%s %s %s\n' \
        "$start_guard" \
        "$RELEASE_RECOVERY_TARGET_PREFIX/var/lib/celikpanel-release-transaction" \
        "$RELEASE_RECOVERY_TARGET_PREFIX/run/celikpanel-release-transaction"
}

release_recovery_validate_candidate() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7 manifest=$8 source target mode parent
    release_recovery_validate_root_chain "$source_root" || return 1
    release_recovery_source_identity "$source_root" || return 1
    for source in \
        "$source_root/deploy/release-recovery-runner.sh" \
        "$source_root/deploy/systemd/celikpanel-release-recovery.service" \
        "$source_root/deploy/systemd/celikpanel-release-recovery.timer" \
        "$source_root/deploy/release-transaction-start-guard.sh" \
        "$source_root/deploy/release-recovery.protocol"; do
        release_recovery_validate_source_file "$source" || return 1
    done
    while IFS='|' read -r target mode; do
        parent=$(dirname -- "$target")
        while [[ ! -e $parent && ! -L $parent ]]; do parent=$(dirname -- "$parent"); done
        release_recovery_validate_root_chain "$parent" || return 1
        if [[ -e $target || -L $target ]]; then
            release_recovery_validate_installed_file "$target" "$mode" || return 1
        fi
    done <<EOF
$runner|0755
$service|0644
$timer|0644
$start_guard|0755
$agent_dropin|0644
$panel_dropin|0644
$manifest|0600
EOF
}

release_recovery_emit_candidate_manifest() {
    local source_root=$1 start_guard=$2
    local runner_sha service_sha timer_sha guard_sha dropin_sha protocol_sha
    release_recovery_source_identity "$source_root" || return 1
    runner_sha=$(release_recovery_sha256 "$source_root/deploy/release-recovery-runner.sh") &&
        service_sha=$(release_recovery_sha256 "$source_root/deploy/systemd/celikpanel-release-recovery.service") &&
        timer_sha=$(release_recovery_sha256 "$source_root/deploy/systemd/celikpanel-release-recovery.timer") &&
        guard_sha=$(release_recovery_sha256 "$source_root/deploy/release-transaction-start-guard.sh") &&
        dropin_sha=$(release_recovery_emit_dropin "$start_guard" | sha256sum) &&
        protocol_sha=$(release_recovery_sha256 "$source_root/deploy/release-recovery.protocol") || return 1
    dropin_sha=${dropin_sha%% *}
    printf '%s\n' \
        format=celikpanel-release-recovery-foundation-v1 protocol=1 \
        "sequence=$RELEASE_RECOVERY_SOURCE_SEQUENCE" \
        "release-version=$RELEASE_RECOVERY_SOURCE_VERSION" \
        "release-commit=$RELEASE_RECOVERY_SOURCE_COMMIT" \
        "runner-sha256=$runner_sha" "service-sha256=$service_sha" \
        "timer-sha256=$timer_sha" "start-guard-sha256=$guard_sha" \
        "agent-dropin-sha256=$dropin_sha" "panel-dropin-sha256=$dropin_sha" \
        "protocol-sha256=$protocol_sha"
}

release_recovery_emit_expected_manifest() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7
    local expected_dropin
    release_recovery_source_identity "$source_root" || return 1
    release_recovery_validate_installed_file "$runner" 0755 \
        "$source_root/deploy/release-recovery-runner.sh" || return 1
    release_recovery_validate_installed_file "$service" 0644 \
        "$source_root/deploy/systemd/celikpanel-release-recovery.service" || return 1
    release_recovery_validate_installed_file "$timer" 0644 \
        "$source_root/deploy/systemd/celikpanel-release-recovery.timer" || return 1
    release_recovery_validate_installed_file "$start_guard" 0755 \
        "$source_root/deploy/release-transaction-start-guard.sh" || return 1
    release_recovery_validate_installed_file "$agent_dropin" 0644 || return 1
    release_recovery_validate_installed_file "$panel_dropin" 0644 || return 1
    expected_dropin=$(release_recovery_emit_dropin "$start_guard") || return 1
    cmp -s -- "$agent_dropin" <(printf '%s\n' "$expected_dropin") &&
        cmp -s -- "$panel_dropin" <(printf '%s\n' "$expected_dropin") ||
        { release_recovery_foundation_fail 'installed recovery drop-in bytes are noncanonical'; return 1; }
    release_recovery_emit_candidate_manifest "$source_root" "$start_guard"
}

release_recovery_read_manifest_identity() {
    local manifest=$1 owner group mode links size
    local -a lines
    [[ -f $manifest && ! -L $manifest ]] ||
        { release_recovery_foundation_fail 'foundation manifest is missing or unsafe'; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$manifest") ||
        { release_recovery_foundation_fail 'cannot inspect foundation manifest'; return 1; }
    [[ $owner == 0 && $group == 0 && $mode == 600 && $links == 1 &&
       $size -gt 0 && $size -le 2048 ]] ||
        { release_recovery_foundation_fail 'foundation manifest metadata is unsafe'; return 1; }
    mapfile -t lines < "$manifest"
    [[ ${#lines[@]} -eq 12 &&
       ${lines[0]} == format=celikpanel-release-recovery-foundation-v1 &&
       ${lines[1]} == protocol=1 &&
       ${lines[2]} =~ ^sequence=[1-9][0-9]*$ &&
       ${lines[3]} =~ ^release-version=v[0-9A-Za-z.-]+$ &&
       ${lines[4]} =~ ^release-commit=[0-9a-f]{40}$ &&
       ${lines[5]} =~ ^runner-sha256=[0-9a-f]{64}$ &&
       ${lines[6]} =~ ^service-sha256=[0-9a-f]{64}$ &&
       ${lines[7]} =~ ^timer-sha256=[0-9a-f]{64}$ &&
       ${lines[8]} =~ ^start-guard-sha256=[0-9a-f]{64}$ &&
       ${lines[9]} =~ ^agent-dropin-sha256=[0-9a-f]{64}$ &&
       ${lines[10]} =~ ^panel-dropin-sha256=[0-9a-f]{64}$ &&
       ${lines[11]} =~ ^protocol-sha256=[0-9a-f]{64}$ ]] ||
        { release_recovery_foundation_fail 'foundation manifest is noncanonical'; return 1; }
    RELEASE_RECOVERY_MANIFEST_SEQUENCE=${lines[2]#sequence=}
    RELEASE_RECOVERY_MANIFEST_VERSION=${lines[3]#release-version=}
    RELEASE_RECOVERY_MANIFEST_COMMIT=${lines[4]#release-commit=}
    RELEASE_RECOVERY_MANIFEST_RUNNER_SHA=${lines[5]#runner-sha256=}
    RELEASE_RECOVERY_MANIFEST_SERVICE_SHA=${lines[6]#service-sha256=}
    RELEASE_RECOVERY_MANIFEST_TIMER_SHA=${lines[7]#timer-sha256=}
    RELEASE_RECOVERY_MANIFEST_GUARD_SHA=${lines[8]#start-guard-sha256=}
    RELEASE_RECOVERY_MANIFEST_AGENT_DROPIN_SHA=${lines[9]#agent-dropin-sha256=}
    RELEASE_RECOVERY_MANIFEST_PANEL_DROPIN_SHA=${lines[10]#panel-dropin-sha256=}
    RELEASE_RECOVERY_MANIFEST_PROTOCOL_SHA=${lines[11]#protocol-sha256=}
    release_recovery_valid_sequence "$RELEASE_RECOVERY_MANIFEST_SEQUENCE" ||
        { release_recovery_foundation_fail 'foundation manifest sequence is invalid'; return 1; }
}

release_recovery_file_state() {
    local path=$1 mode=$2 digest
    if [[ ! -e $path && ! -L $path ]]; then
        printf '%s\n' absent
        return 0
    fi
    release_recovery_validate_installed_file "$path" "$mode" || return 1
    digest=$(release_recovery_sha256 "$path") || return 1
    printf 'sha256:%s\n' "$digest"
}

release_recovery_state_is_canonical() {
    [[ $1 == absent || $1 =~ ^sha256:[0-9a-f]{64}$ ]]
}

release_recovery_verify_installed_hash() {
    local path=$1 mode=$2 expected=$3 actual
    release_recovery_validate_installed_file "$path" "$mode" || return 1
    actual=$(release_recovery_sha256 "$path") || return 1
    [[ $actual == "$expected" ]] || {
        release_recovery_foundation_fail "installed foundation digest mismatch: $path"
        return 1
    }
}

release_recovery_verify_installed_manifest_files() {
    local runner=$1 service=$2 timer=$3 start_guard=$4 agent_dropin=$5 panel_dropin=$6
    release_recovery_verify_installed_hash "$runner" 0755 "$RELEASE_RECOVERY_MANIFEST_RUNNER_SHA" &&
        release_recovery_verify_installed_hash "$service" 0644 "$RELEASE_RECOVERY_MANIFEST_SERVICE_SHA" &&
        release_recovery_verify_installed_hash "$timer" 0644 "$RELEASE_RECOVERY_MANIFEST_TIMER_SHA" &&
        release_recovery_verify_installed_hash "$start_guard" 0755 "$RELEASE_RECOVERY_MANIFEST_GUARD_SHA" &&
        release_recovery_verify_installed_hash "$agent_dropin" 0644 "$RELEASE_RECOVERY_MANIFEST_AGENT_DROPIN_SHA" &&
        release_recovery_verify_installed_hash "$panel_dropin" 0644 "$RELEASE_RECOVERY_MANIFEST_PANEL_DROPIN_SHA"
}

release_recovery_emit_intent() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7 manifest=$8
    local candidate_sha base_state runner_state service_state timer_state
    local guard_state agent_state panel_state
    candidate_sha=$(release_recovery_emit_candidate_manifest "$source_root" "$start_guard" | sha256sum) || return 1
    candidate_sha=${candidate_sha%% *}
    base_state=$(release_recovery_file_state "$manifest" 0600) &&
        runner_state=$(release_recovery_file_state "$runner" 0755) &&
        service_state=$(release_recovery_file_state "$service" 0644) &&
        timer_state=$(release_recovery_file_state "$timer" 0644) &&
        guard_state=$(release_recovery_file_state "$start_guard" 0755) &&
        agent_state=$(release_recovery_file_state "$agent_dropin" 0644) &&
        panel_state=$(release_recovery_file_state "$panel_dropin" 0644) || return 1
    printf '%s\n' \
        format=celikpanel-release-recovery-intent-v1 protocol=1 \
        "candidate-manifest-sha256=$candidate_sha" \
        "candidate-sequence=$RELEASE_RECOVERY_SOURCE_SEQUENCE" \
        "candidate-version=$RELEASE_RECOVERY_SOURCE_VERSION" \
        "candidate-commit=$RELEASE_RECOVERY_SOURCE_COMMIT" \
        "base-manifest-state=$base_state" \
        "old-runner-state=$runner_state" "old-service-state=$service_state" \
        "old-timer-state=$timer_state" "old-start-guard-state=$guard_state" \
        "old-agent-dropin-state=$agent_state" "old-panel-dropin-state=$panel_state"
}

release_recovery_read_intent() {
    local intent=$1 owner group mode links size value
    local -a lines
    release_recovery_validate_installed_file "$intent" 0600 || return 1
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$intent") || return 1
    [[ $size -gt 0 && $size -le 4096 ]] || {
        release_recovery_foundation_fail 'foundation intent size is invalid'
        return 1
    }
    mapfile -t lines < "$intent"
    [[ ${#lines[@]} -eq 13 &&
       ${lines[0]} == format=celikpanel-release-recovery-intent-v1 &&
       ${lines[1]} == protocol=1 &&
       ${lines[2]} =~ ^candidate-manifest-sha256=[0-9a-f]{64}$ &&
       ${lines[3]} =~ ^candidate-sequence=[1-9][0-9]*$ &&
       ${lines[4]} =~ ^candidate-version=v[0-9A-Za-z.-]+$ &&
       ${lines[5]} =~ ^candidate-commit=[0-9a-f]{40}$ ]] || {
        release_recovery_foundation_fail 'foundation intent identity is noncanonical'
        return 1
    }
    RELEASE_RECOVERY_INTENT_CANDIDATE_SHA=${lines[2]#candidate-manifest-sha256=}
    RELEASE_RECOVERY_INTENT_SEQUENCE=${lines[3]#candidate-sequence=}
    RELEASE_RECOVERY_INTENT_VERSION=${lines[4]#candidate-version=}
    RELEASE_RECOVERY_INTENT_COMMIT=${lines[5]#candidate-commit=}
    RELEASE_RECOVERY_INTENT_BASE=${lines[6]#base-manifest-state=}
    RELEASE_RECOVERY_INTENT_RUNNER=${lines[7]#old-runner-state=}
    RELEASE_RECOVERY_INTENT_SERVICE=${lines[8]#old-service-state=}
    RELEASE_RECOVERY_INTENT_TIMER=${lines[9]#old-timer-state=}
    RELEASE_RECOVERY_INTENT_GUARD=${lines[10]#old-start-guard-state=}
    RELEASE_RECOVERY_INTENT_AGENT=${lines[11]#old-agent-dropin-state=}
    RELEASE_RECOVERY_INTENT_PANEL=${lines[12]#old-panel-dropin-state=}
    [[ ${lines[6]} == base-manifest-state=* && ${lines[7]} == old-runner-state=* &&
       ${lines[8]} == old-service-state=* && ${lines[9]} == old-timer-state=* &&
       ${lines[10]} == old-start-guard-state=* &&
       ${lines[11]} == old-agent-dropin-state=* &&
       ${lines[12]} == old-panel-dropin-state=* ]] || return 1
    for value in "$RELEASE_RECOVERY_INTENT_BASE" "$RELEASE_RECOVERY_INTENT_RUNNER" \
        "$RELEASE_RECOVERY_INTENT_SERVICE" "$RELEASE_RECOVERY_INTENT_TIMER" \
        "$RELEASE_RECOVERY_INTENT_GUARD" "$RELEASE_RECOVERY_INTENT_AGENT" \
        "$RELEASE_RECOVERY_INTENT_PANEL"; do
        release_recovery_state_is_canonical "$value" || {
            release_recovery_foundation_fail 'foundation intent contains an invalid old state'
            return 1
        }
    done
    release_recovery_valid_sequence "$RELEASE_RECOVERY_INTENT_SEQUENCE" || return 1
}

release_recovery_verify_systemd_definition() {
    local systemctl_bin=$1 service=$2 timer=$3 unit path expected reload dropins
    for unit in celikpanel-release-recovery.service celikpanel-release-recovery.timer; do
        [[ $("$systemctl_bin" show --property=LoadState --value "$unit") == loaded ]] ||
            { release_recovery_foundation_fail "$unit is not loaded"; return 1; }
        path=$("$systemctl_bin" show --property=FragmentPath --value "$unit") || return 1
        if [[ $unit == *.service ]]; then expected=$service; else expected=$timer; fi
        [[ $path == "$expected" ]] ||
            { release_recovery_foundation_fail "$unit fragment path is not exact"; return 1; }
        reload=$("$systemctl_bin" show --property=NeedDaemonReload --value "$unit") || return 1
        [[ $reload == no ]] ||
            { release_recovery_foundation_fail "$unit has unconsumed on-disk changes"; return 1; }
        dropins=$("$systemctl_bin" show --property=DropInPaths --value "$unit") || return 1
        [[ -z $dropins ]] ||
            { release_recovery_foundation_fail "$unit has an unverified systemd drop-in"; return 1; }
    done
}

release_recovery_verify_systemd() {
    local systemctl_bin=$1 service=$2 timer=$3
    release_recovery_verify_systemd_definition "$systemctl_bin" "$service" "$timer" ||
        return 1
    [[ $("$systemctl_bin" is-enabled celikpanel-release-recovery.service 2>/dev/null || true) == enabled &&
       $("$systemctl_bin" is-enabled celikpanel-release-recovery.timer 2>/dev/null || true) == enabled ]] ||
        { release_recovery_foundation_fail 'recovery units are not durably enabled'; return 1; }
    "$systemctl_bin" is-active --quiet celikpanel-release-recovery.timer ||
        { release_recovery_foundation_fail 'recovery timer is not active'; return 1; }
}

release_recovery_verify_foundation() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7 manifest=$8 systemctl_bin=$9
    release_recovery_read_manifest_identity "$manifest" || return 1
    cmp -s -- "$manifest" <(release_recovery_emit_expected_manifest \
        "$source_root" "$runner" "$service" "$timer" "$start_guard" \
        "$agent_dropin" "$panel_dropin") ||
        { release_recovery_foundation_fail 'foundation manifest or installed bytes differ from the trusted release'; return 1; }
    release_recovery_verify_systemd "$systemctl_bin" "$service" "$timer"
}

# Prove that a candidate trusted release can replace the installed foundation
# before any runner, unit, start-guard, or drop-in byte is changed. A
# same-sequence candidate must already be byte-for-byte identical; a lower
# sequence or same-sequence/different-commit candidate is rejected.
release_recovery_preflight_publish() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7 manifest=$8 systemctl_bin=$9
    local intent=${manifest}.intent existing_sequence existing_commit
    local candidate_sha current_state new_state old_state target source mode
    release_recovery_validate_candidate "$source_root" "$runner" "$service" \
        "$timer" "$start_guard" "$agent_dropin" "$panel_dropin" "$manifest" || return 1
    candidate_sha=$(release_recovery_emit_candidate_manifest "$source_root" "$start_guard" | sha256sum) || return 1
    candidate_sha=${candidate_sha%% *}
    if [[ -e $intent || -L $intent ]]; then
        release_recovery_read_intent "$intent" || return 1
        [[ $RELEASE_RECOVERY_INTENT_CANDIDATE_SHA == "$candidate_sha" &&
           $RELEASE_RECOVERY_INTENT_SEQUENCE == "$RELEASE_RECOVERY_SOURCE_SEQUENCE" &&
           $RELEASE_RECOVERY_INTENT_VERSION == "$RELEASE_RECOVERY_SOURCE_VERSION" &&
           $RELEASE_RECOVERY_INTENT_COMMIT == "$RELEASE_RECOVERY_SOURCE_COMMIT" ]] || {
            release_recovery_foundation_fail 'another foundation publication intent is already active'
            return 1
        }
        current_state=$(release_recovery_file_state "$manifest" 0600) || return 1
        new_state=sha256:$candidate_sha
        [[ $current_state == "$RELEASE_RECOVERY_INTENT_BASE" || $current_state == "$new_state" ]] || {
            release_recovery_foundation_fail 'committed manifest changed outside the active intent'
            return 1
        }
        while IFS='|' read -r target source mode old_state; do
            current_state=$(release_recovery_file_state "$target" "$mode") || return 1
            if [[ $source == dropin ]]; then
                new_state=$(release_recovery_emit_dropin "$start_guard" | sha256sum) || return 1
                new_state=sha256:${new_state%% *}
            else
                new_state=sha256:$(release_recovery_sha256 "$source") || return 1
            fi
            [[ $current_state == "$old_state" || $current_state == "$new_state" ]] || {
                release_recovery_foundation_fail "foundation target changed outside the active intent: $target"
                return 1
            }
        done <<EOF
$runner|$source_root/deploy/release-recovery-runner.sh|0755|$RELEASE_RECOVERY_INTENT_RUNNER
$service|$source_root/deploy/systemd/celikpanel-release-recovery.service|0644|$RELEASE_RECOVERY_INTENT_SERVICE
$timer|$source_root/deploy/systemd/celikpanel-release-recovery.timer|0644|$RELEASE_RECOVERY_INTENT_TIMER
$start_guard|$source_root/deploy/release-transaction-start-guard.sh|0755|$RELEASE_RECOVERY_INTENT_GUARD
$agent_dropin|dropin|0644|$RELEASE_RECOVERY_INTENT_AGENT
$panel_dropin|dropin|0644|$RELEASE_RECOVERY_INTENT_PANEL
EOF
        return 0
    fi
    if [[ ! -e $manifest && ! -L $manifest ]]; then
        # Missing manifest is accepted only for the proven initial adoption.
        # Once any recovery-specific byte exists, the intent manifest must
        # already bind its sequence/commit before another candidate may act.
        for target in "$runner" "$service" "$timer"; do
            [[ ! -e $target && ! -L $target ]] || {
                release_recovery_foundation_fail \
                    'partial recovery foundation without an intent manifest is refused'
                return 1
            }
        done
        return 0
    fi
    release_recovery_read_manifest_identity "$manifest" || return 1
    existing_sequence=$RELEASE_RECOVERY_MANIFEST_SEQUENCE
    existing_commit=$RELEASE_RECOVERY_MANIFEST_COMMIT
    release_recovery_verify_installed_manifest_files "$runner" "$service" "$timer" \
        "$start_guard" "$agent_dropin" "$panel_dropin" || return 1
    if release_recovery_sequence_gt "$existing_sequence" "$RELEASE_RECOVERY_SOURCE_SEQUENCE"; then
        release_recovery_foundation_fail 'foundation downgrade is refused'
        return 1
    fi
    if [[ $existing_sequence == "$RELEASE_RECOVERY_SOURCE_SEQUENCE" ]]; then
        [[ $existing_commit == "$RELEASE_RECOVERY_SOURCE_COMMIT" ]] ||
            { release_recovery_foundation_fail 'same-sequence foundation commit conflict'; return 1; }
        if ! cmp -s -- "$manifest" \
            <(release_recovery_emit_candidate_manifest "$source_root" "$start_guard"); then
            release_recovery_foundation_fail 'same-sequence foundation intent differs from the candidate'
            return 1
        fi
    fi
}

release_recovery_publish_intent() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7 manifest=$8 systemctl_bin=$9
    local intent=${manifest}.intent state_dir stage
    state_dir=$(dirname -- "$manifest")
    release_recovery_validate_root_chain "$(dirname -- "$state_dir")" || return 1
    if [[ -e $state_dir || -L $state_dir ]]; then
        [[ -d $state_dir && ! -L $state_dir &&
           $(stat -Lc '%u:%g:%a' -- "$state_dir") == 0:0:700 ]] ||
            { release_recovery_foundation_fail 'release state directory is unsafe'; return 1; }
    else
        install -d -m 0700 -o root -g root -- "$state_dir" || return 1
        sync -f -- "$(dirname -- "$state_dir")" || return 1
    fi
    release_recovery_validate_root_chain "$state_dir" || return 1
    release_recovery_preflight_publish "$source_root" "$runner" "$service" "$timer" "$start_guard" "$agent_dropin" "$panel_dropin" "$manifest" "$systemctl_bin" || return 1
    if [[ -e $intent || -L $intent ]]; then
        return 0
    fi
    if [[ -e $manifest && ! -L $manifest ]] && cmp -s -- "$manifest" \
        <(release_recovery_emit_candidate_manifest "$source_root" "$start_guard"); then
        return 0
    fi
    stage=$(mktemp "$state_dir/.recovery-foundation-intent.XXXXXXXX") || return 1
    if ! release_recovery_emit_intent "$source_root" "$runner" "$service" "$timer" \
        "$start_guard" "$agent_dropin" "$panel_dropin" "$manifest" > "$stage"; then
        rm -f -- "$stage"
        return 1
    fi
    chown root:root -- "$stage" && chmod 0600 -- "$stage" &&
        sync -f -- "$stage" ||
        { rm -f -- "$stage"; return 1; }
    mv -T -- "$stage" "$intent" || { rm -f -- "$stage"; return 1; }
    sync -f -- "$intent" "$state_dir" || return 1
    release_recovery_preflight_publish "$source_root" "$runner" "$service" "$timer" \
        "$start_guard" "$agent_dropin" "$panel_dropin" "$manifest" "$systemctl_bin"
}

release_recovery_publish_manifest() {
    local source_root=$1 runner=$2 service=$3 timer=$4 start_guard=$5
    local agent_dropin=$6 panel_dropin=$7 manifest=$8 systemctl_bin=$9
    release_recovery_publish_intent "$source_root" "$runner" "$service" "$timer" \
        "$start_guard" "$agent_dropin" "$panel_dropin" "$manifest" "$systemctl_bin" || return 1
    local intent=${manifest}.intent state_dir stage
    state_dir=$(dirname -- "$manifest")
    release_recovery_emit_expected_manifest "$source_root" "$runner" "$service" \
        "$timer" "$start_guard" "$agent_dropin" "$panel_dropin" >/dev/null || return 1
    release_recovery_verify_systemd "$systemctl_bin" "$service" "$timer" || return 1
    stage=$(mktemp "$state_dir/.recovery-foundation-manifest.XXXXXXXX") || return 1
    if ! release_recovery_emit_candidate_manifest "$source_root" "$start_guard" > "$stage" ||
       ! chown root:root -- "$stage" || ! chmod 0600 -- "$stage" ||
       ! sync -f -- "$stage" || ! mv -T -- "$stage" "$manifest" ||
       ! sync -f -- "$manifest" "$state_dir"; then
        [[ ! -e $stage && ! -L $stage ]] || rm -f -- "$stage"
        return 1
    fi
    release_recovery_verify_foundation "$source_root" "$runner" "$service" \
        "$timer" "$start_guard" "$agent_dropin" "$panel_dropin" \
        "$manifest" "$systemctl_bin" || return 1
    rm -f -- "$intent" || return 1
    sync -f -- "$state_dir"
}
