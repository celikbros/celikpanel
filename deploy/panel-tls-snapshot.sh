#!/usr/bin/env bash
# Exact snapshot/restore support for the panel-certificate compatibility boundary.

_panel_tls_fail() { printf 'panel TLS snapshot: %s\n' "$*" >&2; return 1; }

_panel_tls_expected_paths() {
    local tls_dir=$1 pending=$2 hook=$3 test_root=${CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT:-}
    if [[ -n "$test_root" ]]; then
        [[ "$tls_dir" == "$test_root/tls" &&
           "$pending" == "$test_root/agent/panel-certificate-activation.json" &&
           "$hook" == "$test_root/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert" ]] \
            || { _panel_tls_fail "test paths escaped the declared root"; return 1; }
    else
        [[ "$tls_dir" == /var/lib/celikpanel/tls &&
           "$pending" == /var/lib/celikpanel-agent-private/panel-certificate-activation.json &&
           "$hook" == /etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert ]] \
            || { _panel_tls_fail "managed path identity mismatch"; return 1; }
    fi
}

_panel_tls_safe_parent() {
    local parent=$1 current metadata owner mode permissions boundary=${CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT:-/}
    [[ "$parent" == /* && -d "$parent" && ! -L "$parent" ]] || return 1
    current=$(readlink -e -- "$parent") || return 1
    [[ "$current" == "$parent" ]] || return 1
    [[ "$boundary" == / || "$parent" == "$boundary" || "$parent" == "$boundary/"* ]] || return 1
    while true; do
        metadata=$(stat -Lc '%u %a' -- "$current") || return 1
        read -r owner mode <<< "$metadata" || return 1
        [[ "$owner" =~ ^[0-9]+$ && "$mode" =~ ^[0-7]{3,4}$ ]] || return 1
        permissions=$((8#$mode))
        [[ "$owner" == 0 ]] && (( (permissions & 0022) == 0 )) || return 1
        [[ "$current" == "$boundary" || "$current" == / ]] && break
        current=$(dirname -- "$current") || return 1
    done
}

_panel_tls_regular() {
    local path=$1 max_size=${2:-0} metadata owner group mode links size permissions
    [[ -f "$path" && ! -L "$path" ]] || _panel_tls_fail "unsafe regular file: $path" || return
    metadata=$(stat -Lc '%u %g %a %h %s' -- "$path") || _panel_tls_fail "cannot inspect $path" || return
    read -r owner group mode links size <<< "$metadata" || return
    [[ "$owner" == 0 && "$group" =~ ^[0-9]+$ && "$links" == 1 &&
       "$mode" =~ ^[0-7]{3,4}$ && "$size" =~ ^[0-9]+$ ]] \
        || _panel_tls_fail "file metadata is unsafe: $path" || return
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) || _panel_tls_fail "file is group/other writable: $path" || return
    (( max_size == 0 || size <= max_size )) || _panel_tls_fail "file exceeds bound: $path" || return
}

_panel_tls_directory_metadata() {
    local path=$1 metadata owner group mode mtime permissions
    [[ -d "$path" && ! -L "$path" ]] || _panel_tls_fail "unsafe directory: $path" || return
    metadata=$(stat -Lc $'%u\t%g\t%a\t%y' -- "$path") || return
    IFS=$'\t' read -r owner group mode mtime <<< "$metadata" || return
    [[ "$owner" == 0 && "$group" =~ ^[0-9]+$ && "$mode" =~ ^[0-7]{3,4}$ && -n "$mtime" ]] || return 1
    permissions=$((8#$mode)); (( (permissions & 0022) == 0 )) || return 1
    printf '%s\n' "$metadata"
}

_panel_tls_write_directory_metadata() {
    local metadata; metadata=$(_panel_tls_directory_metadata "$1") || return
    printf '%s\n' "$metadata" > "$2" || return
}

_panel_tls_validate_directory_metadata_file() {
    local file=$1 value owner group mode mtime extra permissions
    _panel_tls_regular "$file" 512 || return
    value=$(<"$file") || return
    IFS=$'\t' read -r owner group mode mtime extra <<< "$value" || return
    [[ "$owner" == 0 && "$group" =~ ^[0-9]+$ && "$mode" =~ ^[0-7]{3,4}$ && -n "$mtime" && -z "${extra:-}" ]] || return 1
    permissions=$((8#$mode)); (( (permissions & 0022) == 0 )) || return 1
}

_panel_tls_directory_metadata_matches() {
    local expected actual
    _panel_tls_validate_directory_metadata_file "$1" || return
    expected=$(<"$1") || return; actual=$(_panel_tls_directory_metadata "$2") || return
    [[ "$expected" == "$actual" ]]
}

_panel_tls_file_metadata_matches() {
    local a b
    _panel_tls_regular "$1" || return; _panel_tls_regular "$2" || return
    a=$(stat -Lc $'%u\t%g\t%a\t%y\t%s' -- "$1") || return
    b=$(stat -Lc $'%u\t%g\t%a\t%y\t%s' -- "$2") || return
    [[ "$a" == "$b" ]]
}

_panel_tls_find0() {
    local output=$1; shift
    [[ ${CELIKPANEL_TLS_SNAPSHOT_TEST_FAIL_FIND:-0} != 1 ]] || _panel_tls_fail "injected find failure" || return
    : > "$output" || return
    find "$@" -print0 > "$output" || _panel_tls_fail "filesystem enumeration failed" || return
}

_panel_tls_mount_guard() {
    local requested=$1 canonical mountinfo=/proc/self/mountinfo list line mountpoint
    [[ -z ${CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT:-} ]] || mountinfo=${CELIKPANEL_TLS_SNAPSHOT_MOUNTINFO:-$mountinfo}
    [[ ${CELIKPANEL_TLS_SNAPSHOT_TEST_FAIL_MOUNTINFO:-0} != 1 ]] || _panel_tls_fail "injected mountinfo failure" || return
    [[ -f "$mountinfo" && ! -L "$mountinfo" && -r "$mountinfo" ]] || _panel_tls_fail "mountinfo unavailable" || return
    canonical=$(readlink -m -- "$requested") || return
    list=$(mktemp "${TMPDIR:-/tmp}/celikpanel-mountinfo.XXXXXXXX") || return
    if ! awk '{ print $5 }' "$mountinfo" > "$list"; then rm -f -- "$list" >/dev/null 2>&1 || true; return 1; fi
    while IFS= read -r line || [[ -n "$line" ]]; do
        mountpoint=${line//\\040/ }; mountpoint=${mountpoint//\\011/$'\t'}
        mountpoint=${mountpoint//\\012/$'\n'}; mountpoint=${mountpoint//\\134/\\}
        if [[ "$mountpoint" == "$canonical" || "$mountpoint" == "$canonical/"* ]]; then
            rm -f -- "$list" >/dev/null 2>&1 || true
            _panel_tls_fail "managed TLS root contains a mount: $mountpoint"; return
        fi
    done < "$list" || { rm -f -- "$list" >/dev/null 2>&1 || true; return 1; }
    rm -f -- "$list" || return
}

_panel_tls_validate_version_directory() {
    local directory=$1 listing entry base count=0
    [[ -d "$directory" && ! -L "$directory" ]] || return 1
    _panel_tls_directory_metadata "$directory" >/dev/null || return
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-version.XXXXXXXX") || return
    _panel_tls_find0 "$listing" "$directory" -mindepth 1 -maxdepth 1 || { rm -f -- "$listing" >/dev/null 2>&1 || true; return 1; }
    while IFS= read -r -d '' entry; do
        base=${entry##*/}; case "$base" in panel.crt|panel.key|panel.domain) ;; *) rm -f -- "$listing"; return 1 ;; esac
        _panel_tls_regular "$entry" || { rm -f -- "$listing"; return 1; }
        [[ -s "$entry" ]] || { rm -f -- "$listing"; return 1; }; count=$((count + 1))
    done < "$listing"
    rm -f -- "$listing" || return
    [[ "$count" -eq 3 && -f "$directory/panel.crt" && -f "$directory/panel.key" && -f "$directory/panel.domain" ]]
}

_panel_tls_validate_managed_tree() {
    local tls_dir=$1 listing entry base version current_seen=0 legacy_crt=0 legacy_key=0
    _panel_tls_mount_guard "$tls_dir" || return
    [[ -d "$tls_dir" && ! -L "$tls_dir" && "$(readlink -e -- "$tls_dir")" == "$tls_dir" ]] || return 1
    _panel_tls_directory_metadata "$tls_dir" >/dev/null || return
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-tree.XXXXXXXX") || return
    _panel_tls_find0 "$listing" "$tls_dir" -mindepth 1 -maxdepth 1 || { rm -f -- "$listing" >/dev/null 2>&1 || true; return 1; }
    while IFS= read -r -d '' entry; do
        base=${entry##*/}
        case "$base" in
            current) [[ "$current_seen" -eq 0 && -L "$entry" ]] || { rm -f -- "$listing"; return 1; }; version=$(readlink -- "$entry") || return; [[ "$version" =~ ^\.panel-cert-[0-9a-f]{32}$ ]] || return 1; current_seen=1 ;;
            panel.crt) _panel_tls_regular "$entry" || return; [[ -s "$entry" ]] || return 1; legacy_crt=1 ;;
            panel.key) _panel_tls_regular "$entry" || return; [[ -s "$entry" ]] || return 1; legacy_key=1 ;;
            .panel-cert-????????????????????????????????) [[ "$base" =~ ^\.panel-cert-[0-9a-f]{32}$ ]] || return 1; _panel_tls_validate_version_directory "$entry" || return ;;
            *) rm -f -- "$listing"; _panel_tls_fail "unknown managed TLS entry: $base"; return ;;
        esac
    done < "$listing"
    rm -f -- "$listing" || return
    [[ "$legacy_crt" -eq "$legacy_key" ]] || return 1
    [[ "$current_seen" -eq 0 ]] || _panel_tls_validate_version_directory "$tls_dir/$version"
}

_panel_tls_tree_manifest() (
    set -euo pipefail
    local root=${1:?managed tree required}
    local output=${2:?manifest output required}
    local listing path rel kind uid gid mode mtime size digest

    _panel_tls_validate_managed_tree "$root" || exit 1
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-manifest.XXXXXXXX") || exit 1
    trap 'rm -f -- "$listing"' EXIT HUP INT TERM
    : >"$output" || { _panel_tls_fail "cannot create managed-tree manifest"; return 1; }
    _panel_tls_find0 "$listing" "$root" -mindepth 1 ||
        { _panel_tls_fail "cannot enumerate managed TLS tree for manifest"; return 1; }

    while IFS= read -r -d '' path; do
        rel=${path#"$root"/}
        [[ $rel != "$path" && -n $rel && $rel != *$'\n'* && $rel != *$'\t'* ]] ||
            { _panel_tls_fail "unsafe managed TLS manifest path"; return 1; }
        if [[ -d $path && ! -L $path ]]; then
            kind=D
            read -r uid gid mode mtime < <(stat -Lc '%u %g %a %Y' -- "$path") ||
                { _panel_tls_fail "cannot stat managed TLS directory"; return 1; }
            printf 'D\t%s\t%s\t%s\t%s\t%s\n' "$rel" "$uid" "$gid" "$mode" "$mtime" >>"$output" ||
                { _panel_tls_fail "cannot write managed-tree manifest"; return 1; }
        elif [[ -f $path && ! -L $path ]]; then
            kind=F
            read -r uid gid mode mtime size < <(stat -Lc '%u %g %a %Y %s' -- "$path") ||
                { _panel_tls_fail "cannot stat managed TLS file"; return 1; }
            digest=$(sha256sum -- "$path") || { _panel_tls_fail "cannot hash managed TLS file"; return 1; }
            digest=${digest%% *}
            [[ $digest =~ ^[0-9a-f]{64}$ ]] || { _panel_tls_fail "invalid managed TLS file digest"; return 1; }
            printf 'F\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n' \
                "$rel" "$uid" "$gid" "$mode" "$mtime" "$size" "$digest" >>"$output" ||
                { _panel_tls_fail "cannot write managed-tree manifest"; return 1; }
        else
            _panel_tls_fail "unsupported object in managed TLS manifest"; return 1
        fi
    done <"$listing"
    LC_ALL=C sort -o "$output" -- "$output" || { _panel_tls_fail "cannot sort managed-tree manifest"; return 1; }
)

_panel_tls_sync_path() {
    local path=${1:?path required}
    sync -f -- "$path" || { _panel_tls_fail "cannot durably sync $path"; return 1; }
}

_panel_tls_systemctl_load_state() {
    local unit=${1:?unit required} value
    value=$(systemctl show --property=LoadState --value -- "$unit" 2>/dev/null) ||
        { _panel_tls_fail "cannot read LoadState for $unit"; return 1; }
    case "$value" in
        loaded|masked|not-found) printf '%s\n' "$value" ;;
        *) _panel_tls_fail "unsupported LoadState for $unit: ${value:-empty}"; return 1 ;;
    esac
}

_panel_tls_systemctl_enabled_state() {
    local unit=${1:?unit required} value rc=0
    if value=$(systemctl is-enabled "$unit" 2>/dev/null); then
        rc=0
    else
        rc=$?
    fi
    case "$value" in
        enabled|enabled-runtime|linked|linked-runtime|alias|static|indirect|generated|disabled|masked|masked-runtime|not-found)
            printf '%s\n' "$value"
            ;;
        *) _panel_tls_fail "unsupported enabled state for $unit (rc=$rc): ${value:-empty}"; return 1 ;;
    esac
}

_panel_tls_systemctl_active_state() {
    local unit=${1:?unit required} value rc=0
    if value=$(systemctl is-active "$unit" 2>/dev/null); then
        rc=0
    else
        rc=$?
    fi
    case "$value" in
        active|inactive) printf '%s\n' "$value" ;;
        *) _panel_tls_fail "scheduler is not in a stable state for $unit (rc=$rc): ${value:-empty}"; return 1 ;;
    esac
}

_panel_tls_validate_unit_state_tuple() {
    local kind=${1:?kind required} unit=${2:?unit required}
    local load=${3:?load state required} enabled=${4:?enabled state required}
    local active=${5:?active state required}
    case "$active" in
        active|inactive) ;;
        *) _panel_tls_fail "invalid $kind active state for $unit"; return 1 ;;
    esac
    case "$load" in
        loaded)
            case "$enabled" in
                enabled|enabled-runtime|linked|linked-runtime|alias|static|indirect|generated|disabled) ;;
                *) _panel_tls_fail "$kind has an enabled state inconsistent with loaded LoadState: $unit"; return 1 ;;
            esac
            ;;
        masked)
            case "$enabled" in
                masked|masked-runtime) ;;
                *) _panel_tls_fail "$kind has an enabled state inconsistent with masked LoadState: $unit"; return 1 ;;
            esac
            [[ $active == inactive ]] || { _panel_tls_fail "$kind is active although LoadState is masked: $unit"; return 1; }
            ;;
        not-found)
            [[ $enabled == not-found && $active == inactive ]] ||
                { _panel_tls_fail "$kind has an inconsistent not-found state: $unit"; return 1; }
            ;;
        *) _panel_tls_fail "invalid $kind LoadState for $unit"; return 1 ;;
    esac
}

_panel_tls_ledger_scheduler_state() {
    local ledger=${1:?ledger required} wanted=${2:?unit required}
    local unit enabled active extra found=0 rows=0 saved_enabled= saved_active=
    [[ -f $ledger && ! -L $ledger ]] || { _panel_tls_fail "service-state ledger is unavailable"; return 1; }
    while IFS=$'\t' read -r unit enabled active extra || [[ -n ${unit}${enabled}${active}${extra} ]]; do
        [[ -n $unit && -n $enabled && -n $active && -z $extra ]] ||
            { _panel_tls_fail "malformed service-state ledger row"; return 1; }
        rows=$((rows + 1))
        if [[ $unit == "$wanted" ]]; then
            (( found == 0 )) || { _panel_tls_fail "duplicate scheduler row in service-state ledger: $wanted"; return 1; }
            case "$enabled" in
                enabled|enabled-runtime|linked|linked-runtime|alias|static|indirect|generated|disabled|masked|masked-runtime|not-found) ;;
                *) _panel_tls_fail "invalid saved enabled state for $wanted"; return 1 ;;
            esac
            case "$active" in active|inactive) ;; *) _panel_tls_fail "invalid saved active state for $wanted"; return 1 ;; esac
            saved_enabled=$enabled
            saved_active=$active
            found=1
        fi
    done <"$ledger"
    (( rows >= 2 && found == 1 )) || { _panel_tls_fail "missing scheduler row in service-state ledger: $wanted"; return 1; }
    printf '%s\t%s\n' "$saved_enabled" "$saved_active"
}

panel_tls_capture_scheduler_states_to_service_ledger() {
    local ledger=${1:?service-state ledger required}
    local unit load enabled active extra rows=0 timer_rows=0
    [[ -f $ledger && ! -L $ledger ]] || { _panel_tls_fail "service-state ledger is unavailable"; return 1; }
    while IFS=$'\t' read -r unit enabled active extra || [[ -n ${unit}${enabled}${active}${extra} ]]; do
        [[ -n $unit && -n $enabled && -n $active && -z $extra ]] ||
            { _panel_tls_fail "malformed service-state ledger row"; return 1; }
        rows=$((rows + 1))
        case "$unit" in certbot.timer|certbot-renew.timer) timer_rows=$((timer_rows + 1)) ;; esac
    done <"$ledger"
    (( rows == 3 && timer_rows == 0 )) ||
        { _panel_tls_fail "service-state ledger must contain exactly the three CelikPanel units before scheduler capture"; return 1; }
    for unit in certbot.timer certbot-renew.timer; do
        load=$(_panel_tls_systemctl_load_state "$unit") || return 1
        enabled=$(_panel_tls_systemctl_enabled_state "$unit") || return 1
        active=$(_panel_tls_systemctl_active_state "$unit") || return 1
        _panel_tls_validate_unit_state_tuple scheduler "$unit" "$load" "$enabled" "$active" || return 1
        printf '%s\t%s\t%s\n' "$unit" "$enabled" "$active" >>"$ledger" ||
            { _panel_tls_fail "cannot append scheduler state to service-state ledger"; return 1; }
    done
    chmod 0600 -- "$ledger" || { _panel_tls_fail "cannot protect service-state ledger"; return 1; }
}

_panel_tls_capture_timer() {
    local root=${1:?snapshot root required} unit=${2:?unit required} ledger=${3:-}
    local load enabled active definition current_load current_enabled current_active
    load=$(_panel_tls_systemctl_load_state "$unit") || return 1
    if [[ -n $ledger ]]; then
        IFS=$'\t' read -r enabled active < <(_panel_tls_ledger_scheduler_state "$ledger" "$unit") || return 1
        current_enabled=$(_panel_tls_systemctl_enabled_state "$unit") || return 1
        current_active=$(_panel_tls_systemctl_active_state "$unit") || return 1
        _panel_tls_validate_unit_state_tuple timer "$unit" "$load" "$enabled" "$active" || return 1
        current_load=$(_panel_tls_systemctl_load_state "$unit") || return 1
        _panel_tls_validate_unit_state_tuple timer "$unit" "$current_load" "$current_enabled" "$current_active" || return 1
        [[ $current_load == "$load" && $current_enabled == "$enabled" && $current_active == "$active" ]] ||
            { _panel_tls_fail "scheduler state changed after service-state ledger capture: $unit"; return 1; }
    else
        enabled=$(_panel_tls_systemctl_enabled_state "$unit") || return 1
        active=$(_panel_tls_systemctl_active_state "$unit") || return 1
        _panel_tls_validate_unit_state_tuple timer "$unit" "$load" "$enabled" "$active" || return 1
    fi
    printf '%s\t%s\t%s\t%s\n' "$unit" "$load" "$enabled" "$active" >>"$root/certbot-timers.tsv" ||
        { _panel_tls_fail "cannot record Certbot scheduler state"; return 1; }
    if [[ $load == loaded ]]; then
        definition="$root/timer-effective/$unit.cat"
        systemctl cat "$unit" >"$definition" || { _panel_tls_fail "cannot capture effective definition for $unit"; return 1; }
        [[ -s $definition ]] || { _panel_tls_fail "empty effective timer definition for $unit"; return 1; }
        chmod 0600 -- "$definition" || { _panel_tls_fail "cannot protect timer definition"; return 1; }
    fi
}

panel_tls_snapshot_scheduler_matches_service_ledger() {
    local root=${1:?snapshot root required} ledger=${2:?service-state ledger required}
    local unit load enabled active extra ledger_enabled ledger_active rows=0
    [[ -f $root/certbot-timers.tsv && ! -L $root/certbot-timers.tsv ]] ||
        { _panel_tls_fail "Certbot scheduler snapshot is unavailable"; return 1; }
    while IFS=$'\t' read -r unit load enabled active extra || [[ -n ${unit}${load}${enabled}${active}${extra} ]]; do
        [[ -n $unit && -n $load && -n $enabled && -n $active && -z $extra ]] ||
            { _panel_tls_fail "malformed Certbot scheduler snapshot"; return 1; }
        case "$unit" in certbot.timer|certbot-renew.timer) ;; *) _panel_tls_fail "unexpected scheduler unit: $unit"; return 1 ;; esac
        _panel_tls_validate_unit_state_tuple timer "$unit" "$load" "$enabled" "$active" || return 1
        rows=$((rows + 1))
        IFS=$'\t' read -r ledger_enabled ledger_active < <(_panel_tls_ledger_scheduler_state "$ledger" "$unit") || return 1
        [[ $enabled == "$ledger_enabled" && $active == "$ledger_active" ]] ||
            { _panel_tls_fail "scheduler snapshot disagrees with service-state ledger for $unit"; return 1; }
    done <"$root/certbot-timers.tsv"
    (( rows == 2 )) || { _panel_tls_fail "Certbot scheduler snapshot must contain exactly two timers"; return 1; }
}

_panel_tls_write_metadata() {
    local source=${1:?source required} output=${2:?output required} value
    value=$(stat -Lc '%u %g %a %Y' -- "$source") || { _panel_tls_fail "cannot capture metadata for $source"; return 1; }
    printf '%s\n' "$value" >"$output" || { _panel_tls_fail "cannot write metadata snapshot"; return 1; }
    chmod 0600 -- "$output" || { _panel_tls_fail "cannot protect metadata snapshot"; return 1; }
}

_panel_tls_validate_metadata_record() {
    local record=${1:?metadata record required} uid gid mode mtime extra
    read -r uid gid mode mtime extra <<<"$record"
    [[ $uid =~ ^[0-9]+$ && $gid =~ ^[0-9]+$ && $mode =~ ^[0-7]{3,4}$ &&
       $mtime =~ ^-?[0-9]+$ && -z $extra ]] || { _panel_tls_fail "invalid TLS metadata record"; return 1; }
}

_panel_tls_write_symlink_metadata() {
    local source=${1:?symlink required} output=${2:?output required} value
    [[ -L $source ]] || { _panel_tls_fail "current metadata source is not a symlink"; return 1; }
    value=$(stat -c '%u %g %Y' -- "$source") || { _panel_tls_fail "cannot capture current symlink metadata"; return 1; }
    printf '%s\n' "$value" >"$output" || { _panel_tls_fail "cannot write current symlink metadata"; return 1; }
    chmod 0600 -- "$output" || { _panel_tls_fail "cannot protect current symlink metadata"; return 1; }
}

_panel_tls_validate_symlink_metadata_record() {
    local record=${1:?record required} uid gid mtime extra
    read -r uid gid mtime extra <<<"$record"
    [[ $uid =~ ^[0-9]+$ && $gid =~ ^[0-9]+$ && $mtime =~ ^-?[0-9]+$ && -z $extra ]] ||
        { _panel_tls_fail "invalid current symlink metadata record"; return 1; }
}

_panel_tls_snapshot_regular() {
    local path=${1:?path required} expected_mode=${2:-} value
    [[ -f $path && ! -L $path ]] || { _panel_tls_fail "snapshot file is missing or unsafe: $path"; return 1; }
    value=$(stat -Lc '%u:%g' -- "$path") || { _panel_tls_fail "cannot stat snapshot file"; return 1; }
    [[ $value == 0:0 ]] || { _panel_tls_fail "snapshot file is not root-owned: $path"; return 1; }
    if [[ -n $expected_mode ]]; then
        value=$(stat -Lc '%a' -- "$path") || { _panel_tls_fail "cannot stat snapshot file mode"; return 1; }
        [[ $value == "$expected_mode" ]] || { _panel_tls_fail "unexpected snapshot file mode: $path"; return 1; }
    fi
}

_panel_tls_capture_service() {
    local root=${1:?snapshot root required} unit=${2:?unit required} load enabled active
    load=$(_panel_tls_systemctl_load_state "$unit") || return 1
    enabled=$(_panel_tls_systemctl_enabled_state "$unit") || return 1
    active=$(_panel_tls_systemctl_active_state "$unit") || return 1
    _panel_tls_validate_unit_state_tuple service "$unit" "$load" "$enabled" "$active" || return 1
    [[ $active == inactive ]] || { _panel_tls_fail "Certbot service is currently running; retry after it becomes idle: $unit"; return 1; }
    printf '%s\t%s\t%s\t%s\n' "$unit" "$load" "$enabled" "$active" >>"$root/certbot-services.tsv" ||
        { _panel_tls_fail "cannot record Certbot service state"; return 1; }
}

panel_tls_snapshot_capture() (
    set -euo pipefail
    local root=${1:?snapshot root required} tls_dir=${2:?TLS directory required}
    local pending_file=${3:?pending activation file required} hook_file=${4:?renewal hook required}
    local ledger=${5:-} listing path base layout=empty target metadata

    _panel_tls_expected_paths "$tls_dir" "$pending_file" "$hook_file" || exit 1
    _panel_tls_safe_parent "${root%/*}" || exit 1
    [[ -d $root && ! -L $root ]] || { _panel_tls_fail "snapshot root is unavailable"; return 1; }
    metadata=$(stat -Lc '%u:%g:%a' -- "$root") || { _panel_tls_fail "cannot stat snapshot root"; return 1; }
    [[ $metadata == 0:0:700 ]] || { _panel_tls_fail "snapshot root must be root-owned mode 0700"; return 1; }
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-capture.XXXXXXXX") || exit 1
    trap 'rm -f -- "$listing"' EXIT HUP INT TERM
    _panel_tls_find0 "$listing" "$root" -mindepth 1 || exit 1
    [[ ! -s $listing ]] || { _panel_tls_fail "snapshot root is not empty"; return 1; }
    printf '2\n' >"$root/snapshot.version" || { _panel_tls_fail "cannot write snapshot version"; return 1; }
    mkdir -m 0700 -- "$root/managed" "$root/hook" "$root/pending" "$root/timer-effective" ||
        { _panel_tls_fail "cannot create TLS snapshot directories"; return 1; }

    if [[ ! -e $tls_dir && ! -L $tls_dir ]]; then
        layout=absent
        printf 'absent\n' >"$root/root.meta" || { _panel_tls_fail "cannot record absent TLS root"; return 1; }
    else
        [[ -d $tls_dir && ! -L $tls_dir ]] || { _panel_tls_fail "TLS root is not a real directory"; return 1; }
        _panel_tls_mount_guard "$tls_dir" || exit 1
        _panel_tls_validate_managed_tree "$tls_dir" || exit 1
        _panel_tls_write_metadata "$tls_dir" "$root/root.meta" || exit 1
        _panel_tls_find0 "$listing" "$tls_dir" -mindepth 1 -maxdepth 1 || exit 1
        while IFS= read -r -d '' path; do
            base=${path##*/}
            if [[ $base == current ]]; then
                [[ -L $path ]] || { _panel_tls_fail "TLS current entry is not a symlink"; return 1; }
                target=$(readlink -- "$path") || { _panel_tls_fail "cannot read TLS current symlink"; return 1; }
                [[ $target =~ ^\.panel-cert-[A-Za-z0-9._-]+$ ]] || { _panel_tls_fail "unsafe TLS current target"; return 1; }
                printf '%s\n' "$target" >"$root/current.name" || { _panel_tls_fail "cannot record TLS current link"; return 1; }
                _panel_tls_write_symlink_metadata "$path" "$root/current.meta" || exit 1
                layout=atomic
            else
                cp -a -- "$path" "$root/managed/$base" ||
                    { _panel_tls_fail "cannot copy complete managed TLS entry: $base"; return 1; }
                [[ $layout == atomic ]] || layout=managed
            fi
        done <"$listing"
        [[ ! -f $root/current.name ]] || layout=atomic
    fi
    printf '%s\n' "$layout" >"$root/layout.state" || { _panel_tls_fail "cannot record TLS layout"; return 1; }
    _panel_tls_validate_managed_tree "$root/managed" || exit 1
    _panel_tls_tree_manifest "$root/managed" "$root/managed.manifest" || exit 1

    _panel_tls_safe_parent "${hook_file%/*}" || exit 1
    if [[ -e $hook_file || -L $hook_file ]]; then
        _panel_tls_regular "$hook_file" 1048576 || exit 1
        printf 'present\n' >"$root/hook.state" || { _panel_tls_fail "cannot record hook state"; return 1; }
        cp -a -- "$hook_file" "$root/hook/celikpanel-panel-cert" || { _panel_tls_fail "cannot capture renewal hook"; return 1; }
    else
        printf 'absent\n' >"$root/hook.state" || { _panel_tls_fail "cannot record hook state"; return 1; }
    fi
    _panel_tls_safe_parent "${pending_file%/*}" || exit 1
    if [[ -e $pending_file || -L $pending_file ]]; then
        _panel_tls_regular "$pending_file" 4096 || exit 1
        metadata=$(stat -Lc '%a' -- "$pending_file") || { _panel_tls_fail "cannot stat pending activation mode"; return 1; }
        [[ $metadata == 600 ]] || { _panel_tls_fail "pending activation file must be mode 0600"; return 1; }
        printf 'present\n' >"$root/pending.state" || { _panel_tls_fail "cannot record pending activation state"; return 1; }
        cp -a -- "$pending_file" "$root/pending/panel-certificate-activation.json" ||
            { _panel_tls_fail "cannot capture pending activation file"; return 1; }
    else
        printf 'absent\n' >"$root/pending.state" || { _panel_tls_fail "cannot record pending activation state"; return 1; }
    fi

    : >"$root/certbot-timers.tsv" || { _panel_tls_fail "cannot create timer snapshot"; return 1; }
    : >"$root/certbot-services.tsv" || { _panel_tls_fail "cannot create service snapshot"; return 1; }
    chmod 0600 -- "$root/certbot-timers.tsv" "$root/certbot-services.tsv" ||
        { _panel_tls_fail "cannot protect scheduler snapshot"; return 1; }
    _panel_tls_capture_timer "$root" certbot.timer "$ledger" || exit 1
    _panel_tls_capture_timer "$root" certbot-renew.timer "$ledger" || exit 1
    _panel_tls_capture_service "$root" certbot.service || exit 1
    _panel_tls_capture_service "$root" certbot-renew.service || exit 1
    chmod 0600 -- "$root/snapshot.version" "$root/layout.state" "$root/root.meta" \
        "$root/managed.manifest" "$root/hook.state" "$root/pending.state" \
        "$root/certbot-timers.tsv" "$root/certbot-services.tsv" ||
        { _panel_tls_fail "cannot protect generated TLS snapshot files"; return 1; }
    [[ ! -f $root/current.name ]] || chmod 0600 -- "$root/current.name" ||
        { _panel_tls_fail "cannot protect active-version snapshot"; return 1; }
    panel_tls_snapshot_validate "$root" || exit 1
)

panel_tls_snapshot_validate() (
    set -euo pipefail
    local root=${1:?snapshot root required} listing path rel state layout record current computed mode ownership
    local unit load enabled active extra rows=0 seen_certbot=0 seen_renew=0
    [[ -d $root && ! -L $root ]] || { _panel_tls_fail "TLS snapshot root is unsafe"; return 1; }
    ownership=$(stat -Lc '%u:%g:%a' -- "$root") || { _panel_tls_fail "cannot stat TLS snapshot root"; return 1; }
    [[ $ownership == 0:0:700 ]] || { _panel_tls_fail "TLS snapshot root must be root-owned mode 0700"; return 1; }
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-validate.XXXXXXXX") || exit 1
    computed=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-computed.XXXXXXXX") || exit 1
    trap 'rm -f -- "$listing" "$computed"' EXIT HUP INT TERM
    _panel_tls_find0 "$listing" "$root" -mindepth 1 || exit 1
    while IFS= read -r -d '' path; do
        rel=${path#"$root"/}
        case "$rel" in
            snapshot.version|layout.state|root.meta|current.name|current.meta|managed.manifest|hook.state|pending.state|\
            certbot-timers.tsv|certbot-services.tsv|managed|managed/*|hook|hook/celikpanel-panel-cert|\
            pending|pending/panel-certificate-activation.json|timer-effective|\
            timer-effective/certbot.timer.cat|timer-effective/certbot-renew.timer.cat) ;;
            *) _panel_tls_fail "unexpected TLS snapshot payload: $rel"; return 1 ;;
        esac
        [[ ! -L $path && ( -d $path || -f $path ) ]] || { _panel_tls_fail "unsafe TLS snapshot object: $rel"; return 1; }
        ownership=$(stat -Lc '%u:%g' -- "$path") || { _panel_tls_fail "cannot stat snapshot ownership"; return 1; }
        [[ $ownership == 0:0 ]] || { _panel_tls_fail "TLS snapshot object is not root-owned: $rel"; return 1; }
        mode=$(stat -Lc '%a' -- "$path") || { _panel_tls_fail "cannot stat TLS snapshot mode"; return 1; }
        [[ $mode =~ ^[0-7]{3,4}$ ]] || { _panel_tls_fail "invalid TLS snapshot mode: $rel"; return 1; }
        (( (0$mode & 022) == 0 )) || { _panel_tls_fail "TLS snapshot object is group/world writable: $rel"; return 1; }
    done <"$listing"

    _panel_tls_snapshot_regular "$root/snapshot.version" 600
    record=$(tr -d '[:space:]' <"$root/snapshot.version") || exit 1
    [[ $record == 2 ]] || { _panel_tls_fail "unsupported TLS snapshot version"; return 1; }
    _panel_tls_snapshot_regular "$root/layout.state" 600
    layout=$(tr -d '[:space:]' <"$root/layout.state") || exit 1
    case "$layout" in absent|empty|managed|atomic) ;; *) _panel_tls_fail "invalid TLS layout state"; return 1 ;; esac
    _panel_tls_snapshot_regular "$root/root.meta" 600
    record=$(cat -- "$root/root.meta") || { _panel_tls_fail "cannot read TLS root metadata"; return 1; }
    if [[ $layout == absent ]]; then [[ $record == absent ]] || { _panel_tls_fail "absent TLS layout has metadata"; return 1; };
    else _panel_tls_validate_metadata_record "$record" || exit 1; fi

    [[ -d $root/managed && ! -L $root/managed ]] || { _panel_tls_fail "managed snapshot tree is unavailable"; return 1; }
    _panel_tls_validate_managed_tree "$root/managed" || exit 1
    _panel_tls_snapshot_regular "$root/managed.manifest" 600
    _panel_tls_tree_manifest "$root/managed" "$computed" || exit 1
    cmp -s -- "$computed" "$root/managed.manifest" || { _panel_tls_fail "managed TLS snapshot manifest mismatch"; return 1; }
    if [[ $layout == atomic ]]; then
        _panel_tls_snapshot_regular "$root/current.name" 600
        _panel_tls_snapshot_regular "$root/current.meta" 600
        current=$(tr -d '\r\n' <"$root/current.name") || exit 1
        [[ $current =~ ^\.panel-cert-[A-Za-z0-9._-]+$ && -d $root/managed/$current ]] ||
            { _panel_tls_fail "atomic TLS snapshot has invalid active version"; return 1; }
        record=$(cat -- "$root/current.meta") || { _panel_tls_fail "cannot read current symlink metadata"; return 1; }
        _panel_tls_validate_symlink_metadata_record "$record" || exit 1
    else
        [[ ! -e $root/current.name && ! -e $root/current.meta ]] ||
            { _panel_tls_fail "non-atomic TLS snapshot has current metadata"; return 1; }
    fi

    for state in hook pending; do
        _panel_tls_snapshot_regular "$root/$state.state" 600
        record=$(tr -d '[:space:]' <"$root/$state.state") || exit 1
        case "$record" in present|absent) ;; *) _panel_tls_fail "invalid $state snapshot state"; return 1 ;; esac
        if [[ $state == hook ]]; then path=$root/hook/celikpanel-panel-cert; else path=$root/pending/panel-certificate-activation.json; fi
        if [[ $record == present ]]; then _panel_tls_snapshot_regular "$path";
        else [[ ! -e $path ]] || { _panel_tls_fail "absent $state snapshot contains payload"; return 1; }; fi
    done

    _panel_tls_snapshot_regular "$root/certbot-timers.tsv" 600
    while IFS=$'\t' read -r unit load enabled active extra || [[ -n ${unit}${load}${enabled}${active}${extra} ]]; do
        [[ -n $unit && -n $load && -n $enabled && -n $active && -z $extra ]] || { _panel_tls_fail "malformed timer snapshot"; return 1; }
        case "$unit" in certbot.timer) (( ++seen_certbot == 1 )) || { _panel_tls_fail "duplicate certbot.timer"; return 1; };; certbot-renew.timer) (( ++seen_renew == 1 )) || { _panel_tls_fail "duplicate certbot-renew.timer"; return 1; };; *) _panel_tls_fail "unexpected timer unit"; return 1;; esac
        _panel_tls_validate_unit_state_tuple timer "$unit" "$load" "$enabled" "$active" || exit 1
        path="$root/timer-effective/$unit.cat"
        if [[ $load == loaded ]]; then _panel_tls_snapshot_regular "$path" 600 || return 1; else [[ ! -e $path ]] || { _panel_tls_fail "unloaded timer has definition"; return 1; }; fi
        rows=$((rows + 1))
    done <"$root/certbot-timers.tsv"
    (( rows == 2 && seen_certbot == 1 && seen_renew == 1 )) || { _panel_tls_fail "incomplete timer snapshot"; return 1; }

    rows=0; seen_certbot=0; seen_renew=0
    _panel_tls_snapshot_regular "$root/certbot-services.tsv" 600
    while IFS=$'\t' read -r unit load enabled active extra || [[ -n ${unit}${load}${enabled}${active}${extra} ]]; do
        [[ -n $unit && -n $load && -n $enabled && -n $active && -z $extra ]] || { _panel_tls_fail "malformed service snapshot"; return 1; }
        case "$unit" in certbot.service) (( ++seen_certbot == 1 )) || { _panel_tls_fail "duplicate certbot.service"; return 1; };; certbot-renew.service) (( ++seen_renew == 1 )) || { _panel_tls_fail "duplicate certbot-renew.service"; return 1; };; *) _panel_tls_fail "unexpected Certbot service"; return 1;; esac
        _panel_tls_validate_unit_state_tuple service "$unit" "$load" "$enabled" "$active" || exit 1
        rows=$((rows + 1))
    done <"$root/certbot-services.tsv"
    (( rows == 2 && seen_certbot == 1 && seen_renew == 1 )) || { _panel_tls_fail "incomplete Certbot service snapshot"; return 1; }
)

_panel_tls_saved_unit_state() {
    local file=${1:?state file required} wanted=${2:?unit required}
    local unit load enabled active extra kind found=0 saved=
    [[ -f $file && ! -L $file ]] || { _panel_tls_fail "saved scheduler state is unavailable"; return 1; }
    while IFS=$'\t' read -r unit load enabled active extra || [[ -n ${unit}${load}${enabled}${active}${extra} ]]; do
        [[ -n $unit && -n $load && -n $enabled && -n $active && -z $extra ]] || { _panel_tls_fail "malformed scheduler state"; return 1; }
        if [[ $unit == "$wanted" ]]; then
            (( found == 0 )) || { _panel_tls_fail "duplicate saved scheduler unit: $wanted"; return 1; }
            kind=service
            [[ $wanted == *.timer ]] && kind=timer
            _panel_tls_validate_unit_state_tuple "$kind" "$wanted" "$load" "$enabled" "$active" || return 1
            saved="$load"$'\t'"$enabled"$'\t'"$active"; found=1
        fi
    done <"$file"
    (( found == 1 )) || { _panel_tls_fail "missing saved scheduler unit: $wanted"; return 1; }
    printf '%s\n' "$saved"
}

_panel_tls_assert_timer_definition() (
    set -euo pipefail
    local root=${1:?snapshot root required} unit=${2:?unit required} saved load enabled active current tmp
    saved=$(_panel_tls_saved_unit_state "$root/certbot-timers.tsv" "$unit") || exit 1
    IFS=$'\t' read -r load enabled active <<<"$saved"
    current=$(_panel_tls_systemctl_load_state "$unit") || exit 1
    [[ $current == "$load" ]] || { _panel_tls_fail "timer LoadState changed: $unit"; return 1; }
    if [[ $load == loaded ]]; then
        tmp=$(mktemp "${TMPDIR:-/tmp}/celikpanel-timer-definition.XXXXXXXX") || exit 1
        trap 'rm -f -- "$tmp"' EXIT HUP INT TERM
        systemctl cat "$unit" >"$tmp" || { _panel_tls_fail "cannot reread timer definition: $unit"; return 1; }
        cmp -s -- "$tmp" "$root/timer-effective/$unit.cat" || { _panel_tls_fail "timer definition changed: $unit"; return 1; }
    fi
)

_panel_tls_copy_source_without_current() (
    set -euo pipefail
    local source=${1:?source required} destination=${2:?destination required} listing path base
    [[ -d $destination && ! -L $destination ]] || { _panel_tls_fail "managed copy destination is unsafe"; return 1; }
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-source.XXXXXXXX") || exit 1
    trap 'rm -f -- "$listing"' EXIT HUP INT TERM
    _panel_tls_find0 "$listing" "$source" -mindepth 1 -maxdepth 1 || exit 1
    while IFS= read -r -d '' path; do
        base=${path##*/}; [[ $base == current ]] && continue
        cp -a -- "$path" "$destination/$base" || { _panel_tls_fail "cannot stage managed TLS entry: $base"; return 1; }
    done <"$listing"
)

_panel_tls_cleanup_private_stage() {
    local stage=${1:-}
    [[ -n $stage && -d $stage && ! -L $stage ]] || return 0
    _panel_tls_mount_guard "$stage" >/dev/null 2>&1 || return 1
    rm -rf --one-file-system -- "$stage"
}

_panel_tls_external_file_matches() {
    local saved=${1:?saved file required} live=${2:?live file required} max_size=${3:?maximum size required}
    local saved_metadata live_metadata
    _panel_tls_snapshot_regular "$saved" || return 1
    _panel_tls_safe_parent "${live%/*}" || return 1
    _panel_tls_regular "$live" "$max_size" || return 1
    cmp -s -- "$saved" "$live" || { _panel_tls_fail "transaction-owned file content changed: $live"; return 1; }
    saved_metadata=$(stat -Lc '%u %g %a %Y %s' -- "$saved") || { _panel_tls_fail "cannot stat saved external file"; return 1; }
    live_metadata=$(stat -Lc '%u %g %a %Y %s' -- "$live") || { _panel_tls_fail "cannot stat live external file"; return 1; }
    [[ $saved_metadata == "$live_metadata" ]] || { _panel_tls_fail "transaction-owned file metadata changed: $live"; return 1; }
}

_panel_tls_assert_scheduler_quiesced() {
    local root=${1:?snapshot root required} unit load saved enabled active
    local current_load current_enabled current_active
    for unit in certbot.timer certbot-renew.timer; do _panel_tls_assert_timer_definition "$root" "$unit" || return 1; done
    for unit in certbot.timer certbot-renew.timer certbot.service certbot-renew.service; do
        if [[ $unit == *.timer ]]; then saved=$(_panel_tls_saved_unit_state "$root/certbot-timers.tsv" "$unit") || return 1
        else saved=$(_panel_tls_saved_unit_state "$root/certbot-services.tsv" "$unit") || return 1; fi
        IFS=$'\t' read -r load enabled active <<<"$saved"
        current_load=$(_panel_tls_systemctl_load_state "$unit") || return 1
        current_enabled=$(_panel_tls_systemctl_enabled_state "$unit") || return 1
        current_active=$(_panel_tls_systemctl_active_state "$unit") || return 1
        _panel_tls_validate_unit_state_tuple scheduler "$unit" "$current_load" "$current_enabled" "$current_active" || return 1
        [[ $current_load == "$load" ]] || { _panel_tls_fail "scheduler LoadState changed while quiescing: $unit"; return 1; }
        [[ $current_enabled == "$enabled" ]] || { _panel_tls_fail "scheduler enable state changed while quiescing: $unit"; return 1; }
        [[ $current_active == inactive ]] || { _panel_tls_fail "Certbot scheduler is not quiesced: $unit"; return 1; }
    done
}

panel_tls_quiesce_certbot_scheduler() {
    local root=${1:?snapshot root required} unit saved load enabled active
    panel_tls_snapshot_validate "$root" || return 1
    for unit in certbot.timer certbot-renew.timer; do
        _panel_tls_assert_timer_definition "$root" "$unit" || return 1
        saved=$(_panel_tls_saved_unit_state "$root/certbot-timers.tsv" "$unit") || return 1
        IFS=$'\t' read -r load enabled active <<<"$saved"
        [[ $load == not-found || $load == masked ]] || systemctl stop "$unit" || { _panel_tls_fail "cannot stop $unit"; return 1; }
    done
    for unit in certbot.service certbot-renew.service; do
        saved=$(_panel_tls_saved_unit_state "$root/certbot-services.tsv" "$unit") || return 1
        IFS=$'\t' read -r load enabled active <<<"$saved"
        [[ $load == not-found || $load == masked ]] || systemctl stop "$unit" || { _panel_tls_fail "cannot stop $unit"; return 1; }
    done
    _panel_tls_assert_scheduler_quiesced "$root"
}

panel_tls_snapshot_assert_source_unchanged() (
    set -euo pipefail
    local root=${1:?snapshot root required} tls_dir=${2:?TLS directory required}
    local pending_file=${3:?pending file required} hook_file=${4:?hook required} require_quiesced=${5:-quiesced}
    local layout metadata live_metadata work current expected state mode
    _panel_tls_expected_paths "$tls_dir" "$pending_file" "$hook_file" || exit 1
    panel_tls_snapshot_validate "$root" || exit 1
    layout=$(tr -d '[:space:]' <"$root/layout.state") || exit 1
    if [[ $layout == absent ]]; then
        [[ ! -e $tls_dir && ! -L $tls_dir ]] || { _panel_tls_fail "TLS root appeared after snapshot"; return 1; }
    else
        [[ -d $tls_dir && ! -L $tls_dir ]] || { _panel_tls_fail "TLS root disappeared after snapshot"; return 1; }
        _panel_tls_mount_guard "$tls_dir" || exit 1
        _panel_tls_validate_managed_tree "$tls_dir" || exit 1
        metadata=$(cat -- "$root/root.meta") || { _panel_tls_fail "cannot read saved TLS metadata"; return 1; }
        live_metadata=$(stat -Lc '%u %g %a %Y' -- "$tls_dir") || { _panel_tls_fail "cannot stat live TLS root"; return 1; }
        [[ $metadata == "$live_metadata" ]] || { _panel_tls_fail "TLS root metadata changed after snapshot"; return 1; }
        work=$(mktemp -d "${TMPDIR:-/tmp}/celikpanel-tls-compare.XXXXXXXX") || exit 1
        chmod 0700 -- "$work" || exit 1
        trap '_panel_tls_cleanup_private_stage "$work"' EXIT HUP INT TERM
        mkdir -m 0700 -- "$work/managed" || exit 1
        _panel_tls_copy_source_without_current "$tls_dir" "$work/managed" || exit 1
        _panel_tls_tree_manifest "$work/managed" "$work/manifest" || exit 1
        cmp -s -- "$work/manifest" "$root/managed.manifest" || { _panel_tls_fail "managed TLS tree changed after snapshot"; return 1; }
        if [[ $layout == atomic ]]; then
            [[ -L $tls_dir/current ]] || { _panel_tls_fail "TLS current link disappeared"; return 1; }
            current=$(readlink -- "$tls_dir/current") || { _panel_tls_fail "cannot read current link"; return 1; }
            expected=$(tr -d '\r\n' <"$root/current.name") || exit 1
            [[ $current == "$expected" ]] || { _panel_tls_fail "TLS current link changed after snapshot"; return 1; }
            metadata=$(cat -- "$root/current.meta") || { _panel_tls_fail "cannot read saved current metadata"; return 1; }
            live_metadata=$(stat -c '%u %g %Y' -- "$tls_dir/current") || { _panel_tls_fail "cannot stat live current symlink"; return 1; }
            [[ $metadata == "$live_metadata" ]] || { _panel_tls_fail "TLS current symlink metadata changed"; return 1; }
        else
            [[ ! -e $tls_dir/current && ! -L $tls_dir/current ]] || { _panel_tls_fail "TLS current link appeared"; return 1; }
        fi
    fi

    _panel_tls_safe_parent "${hook_file%/*}" || exit 1
    state=$(tr -d '[:space:]' <"$root/hook.state") || exit 1
    if [[ $state == present ]]; then _panel_tls_external_file_matches "$root/hook/celikpanel-panel-cert" "$hook_file" 1048576 || exit 1
    else [[ ! -e $hook_file && ! -L $hook_file ]] || { _panel_tls_fail "renewal hook appeared after snapshot"; return 1; }; fi
    _panel_tls_safe_parent "${pending_file%/*}" || exit 1
    state=$(tr -d '[:space:]' <"$root/pending.state") || exit 1
    if [[ $state == present ]]; then
        _panel_tls_external_file_matches "$root/pending/panel-certificate-activation.json" "$pending_file" 4096 || exit 1
        mode=$(stat -Lc '%a' -- "$pending_file") || { _panel_tls_fail "cannot stat pending activation mode"; return 1; }
        [[ $mode == 600 ]] || { _panel_tls_fail "pending activation mode changed"; return 1; }
    else [[ ! -e $pending_file && ! -L $pending_file ]] || { _panel_tls_fail "pending activation file appeared"; return 1; }; fi
    for current in certbot.timer certbot-renew.timer; do _panel_tls_assert_timer_definition "$root" "$current" || exit 1; done
    [[ $require_quiesced != quiesced ]] || _panel_tls_assert_scheduler_quiesced "$root" || exit 1
)

_panel_tls_apply_metadata_record() {
    local record=${1:?metadata required} path=${2:?path required} uid gid mode mtime extra
    read -r uid gid mode mtime extra <<<"$record"; _panel_tls_validate_metadata_record "$record" || return 1
    chown -- "$uid:$gid" "$path" || { _panel_tls_fail "cannot restore TLS ownership"; return 1; }
    chmod -- "$mode" "$path" || { _panel_tls_fail "cannot restore TLS mode"; return 1; }
    touch -d "@$mtime" -- "$path" || { _panel_tls_fail "cannot restore TLS mtime"; return 1; }
}

_panel_tls_apply_symlink_metadata_record() {
    local record=${1:?metadata required} path=${2:?symlink required} uid gid mtime extra
    read -r uid gid mtime extra <<<"$record"; _panel_tls_validate_symlink_metadata_record "$record" || return 1
    [[ -L $path ]] || { _panel_tls_fail "current restore target is not a symlink"; return 1; }
    chown -h -- "$uid:$gid" "$path" || { _panel_tls_fail "cannot restore current symlink ownership"; return 1; }
    touch -h -d "@$mtime" -- "$path" || { _panel_tls_fail "cannot restore current symlink mtime"; return 1; }
}

_panel_tls_restore_file() (
    set -euo pipefail
    local saved=${1:?saved required} target=${2:?target required} max_size=${3:?max size required} required_mode=${4:-}
    local parent stage payload mode
    _panel_tls_snapshot_regular "$saved" || exit 1
    parent=${target%/*}; _panel_tls_safe_parent "$parent" || exit 1
    if [[ -e $target || -L $target ]]; then _panel_tls_regular "$target" "$max_size" || exit 1; fi
    stage=$(mktemp -d "$parent/.${target##*/}.restore.XXXXXXXX") || exit 1
    chmod 0700 -- "$stage" || exit 1; chown root:root -- "$stage" || exit 1
    trap '_panel_tls_cleanup_private_stage "$stage"' EXIT HUP INT TERM
    payload=$stage/payload
    cp -a -- "$saved" "$payload" || { _panel_tls_fail "cannot stage transaction-owned file"; return 1; }
    _panel_tls_regular "$payload" "$max_size" || exit 1
    mode=$(stat -Lc '%a' -- "$payload") || { _panel_tls_fail "cannot stat staged transaction-owned file"; return 1; }
    [[ -z $required_mode || $mode == "$required_mode" ]] || { _panel_tls_fail "staged file has unexpected mode"; return 1; }
    [[ ${CELIKPANEL_TLS_SNAPSHOT_TEST_FAIL_RESTORE_FILE:-0} != 1 ]] || { _panel_tls_fail "injected file restore failure"; return 1; }
    _panel_tls_sync_path "$payload" || exit 1; _panel_tls_sync_path "$stage" || exit 1
    _panel_tls_safe_parent "$parent" || exit 1
    mv -fT -- "$payload" "$target" || { _panel_tls_fail "cannot atomically publish transaction-owned file"; return 1; }
    _panel_tls_sync_path "$parent" || exit 1
    rmdir -- "$stage" || { _panel_tls_fail "cannot remove private restore stage"; return 1; }
    trap - EXIT HUP INT TERM
)

_panel_tls_remove_file() {
    local target=${1:?target required} max_size=${2:?max size required} parent
    parent=${target%/*}
    _panel_tls_safe_parent "$parent" || return 1
    if [[ -e $target || -L $target ]]; then
        _panel_tls_regular "$target" "$max_size" || return 1
        rm -f -- "$target" || { _panel_tls_fail "cannot remove transaction-owned file"; return 1; }
        _panel_tls_sync_path "$parent" || return 1
    fi
}

_panel_tls_sync_tree() (
    set -euo pipefail
    local root=${1:?tree required} listing path
    listing=$(mktemp "${TMPDIR:-/tmp}/celikpanel-tls-sync.XXXXXXXX") || exit 1
    trap 'rm -f -- "$listing"' EXIT HUP INT TERM
    _panel_tls_find0 "$listing" "$root" -mindepth 1 \( -type f -o -type d \) || exit 1
    while IFS= read -r -d '' path; do _panel_tls_sync_path "$path" || exit 1; done <"$listing"
    _panel_tls_sync_path "$root" || exit 1
)

_panel_tls_candidate_matches_snapshot() (
    set -euo pipefail
    local root=${1:?snapshot required} candidate=${2:?candidate required} work current expected metadata live
    _panel_tls_validate_managed_tree "$candidate" || exit 1
    work=$(mktemp -d "${TMPDIR:-/tmp}/celikpanel-tls-candidate.XXXXXXXX") || exit 1
    chmod 0700 -- "$work" || exit 1
    trap '_panel_tls_cleanup_private_stage "$work"' EXIT HUP INT TERM
    mkdir -m 0700 -- "$work/managed" || exit 1
    _panel_tls_copy_source_without_current "$candidate" "$work/managed" || exit 1
    _panel_tls_tree_manifest "$work/managed" "$work/manifest" || exit 1
    cmp -s -- "$work/manifest" "$root/managed.manifest" || { _panel_tls_fail "restored managed tree mismatch"; return 1; }
    metadata=$(cat -- "$root/root.meta") || { _panel_tls_fail "cannot read saved TLS metadata"; return 1; }
    live=$(stat -Lc '%u %g %a %Y' -- "$candidate") || { _panel_tls_fail "cannot stat restore candidate"; return 1; }
    [[ $metadata == "$live" ]] || { _panel_tls_fail "restored TLS root metadata mismatch"; return 1; }
    if [[ $(tr -d '[:space:]' <"$root/layout.state") == atomic ]]; then
        [[ -L $candidate/current ]] || { _panel_tls_fail "atomic restore lacks current link"; return 1; }
        current=$(readlink -- "$candidate/current") || { _panel_tls_fail "cannot read restored current link"; return 1; }
        expected=$(tr -d '\r\n' <"$root/current.name") || exit 1
        [[ $current == "$expected" ]] || { _panel_tls_fail "restored current link mismatch"; return 1; }
        metadata=$(cat -- "$root/current.meta") || { _panel_tls_fail "cannot read saved current metadata"; return 1; }
        live=$(stat -c '%u %g %Y' -- "$candidate/current") || { _panel_tls_fail "cannot stat restored current symlink"; return 1; }
        [[ $metadata == "$live" ]] || { _panel_tls_fail "restored current symlink metadata mismatch"; return 1; }
    else
        [[ ! -e $candidate/current && ! -L $candidate/current ]] || { _panel_tls_fail "non-atomic restore contains current link"; return 1; }
    fi
)

_panel_tls_restore_old_tree() {
    local previous=${1:?previous required} target=${2:?target required} parent=${target%/*}
    [[ ! -e $target && ! -L $target ]] || { _panel_tls_fail "cannot recover old TLS tree over existing target"; return 1; }
    _panel_tls_mount_guard "$previous" || return 1
    mv -T -- "$previous" "$target" || { _panel_tls_fail "cannot recover old TLS tree"; return 1; }
    _panel_tls_sync_path "$parent" || return 1
}

panel_tls_restore_snapshot() (
    set -euo pipefail
    local root=${1:?snapshot required} tls_dir=${2:?TLS directory required}
    local pending_file=${3:?pending file required} hook_file=${4:?hook required}
    local parent stage candidate previous layout metadata state current old_moved=0
    _panel_tls_expected_paths "$tls_dir" "$pending_file" "$hook_file" || exit 1
    panel_tls_snapshot_validate "$root" || exit 1
    _panel_tls_assert_scheduler_quiesced "$root" || exit 1
    parent=${tls_dir%/*}; _panel_tls_safe_parent "$parent" || exit 1
    _panel_tls_mount_guard "$tls_dir" || exit 1
    if [[ -e $tls_dir || -L $tls_dir ]]; then
        [[ -d $tls_dir && ! -L $tls_dir ]] || { _panel_tls_fail "live TLS root is unsafe"; return 1; }
        _panel_tls_validate_managed_tree "$tls_dir" || exit 1
    fi
    stage=$(mktemp -d "$parent/.panel-tls-restore.XXXXXXXX") || exit 1
    chmod 0700 -- "$stage" || exit 1; chown root:root -- "$stage" || exit 1
    trap '_panel_tls_cleanup_private_stage "$stage"' EXIT HUP INT TERM
    candidate=$stage/candidate; previous=$stage/previous
    layout=$(tr -d '[:space:]' <"$root/layout.state") || exit 1
    if [[ $layout != absent ]]; then
        mkdir -m 0700 -- "$candidate" || { _panel_tls_fail "cannot create restore candidate"; return 1; }
        cp -a -- "$root/managed/." "$candidate/" || { _panel_tls_fail "cannot stage complete managed tree"; return 1; }
        if [[ $layout == atomic ]]; then
            current=$(tr -d '\r\n' <"$root/current.name") || exit 1
            [[ -d $candidate/$current && ! -L $candidate/$current ]] || { _panel_tls_fail "active version absent from candidate"; return 1; }
            ln -s -- "$current" "$candidate/current" || { _panel_tls_fail "cannot create restored current link"; return 1; }
            metadata=$(cat -- "$root/current.meta") || { _panel_tls_fail "cannot read current symlink metadata"; return 1; }
            _panel_tls_apply_symlink_metadata_record "$metadata" "$candidate/current" || exit 1
        fi
        metadata=$(cat -- "$root/root.meta") || { _panel_tls_fail "cannot read TLS root metadata"; return 1; }
        _panel_tls_apply_metadata_record "$metadata" "$candidate" || exit 1
        _panel_tls_candidate_matches_snapshot "$root" "$candidate" || exit 1
        _panel_tls_sync_tree "$candidate" || exit 1; _panel_tls_sync_path "$stage" || exit 1
    fi

    # Mandatory second guard immediately before the first destructive rename.
    _panel_tls_mount_guard "$tls_dir" || exit 1
    if [[ -e $tls_dir || -L $tls_dir ]]; then
        [[ -d $tls_dir && ! -L $tls_dir ]] || { _panel_tls_fail "TLS root became unsafe before restore"; return 1; }
        mv -T -- "$tls_dir" "$previous" || { _panel_tls_fail "cannot quarantine previous TLS tree"; return 1; }
        old_moved=1
        _panel_tls_sync_path "$parent" || exit 1; _panel_tls_sync_path "$stage" || exit 1
    fi
    if [[ ${CELIKPANEL_TLS_SNAPSHOT_TEST_FAIL_TREE_PUBLISH:-0} == 1 ]]; then
        (( old_moved == 0 )) || _panel_tls_restore_old_tree "$previous" "$tls_dir" || exit 1
        _panel_tls_fail "injected TLS tree publish failure"; return 1
    fi
    if [[ $layout != absent ]]; then
        if ! mv -T -- "$candidate" "$tls_dir"; then
            (( old_moved == 0 )) || _panel_tls_restore_old_tree "$previous" "$tls_dir" || exit 1
            _panel_tls_fail "cannot atomically publish restored TLS tree"; return 1
        fi
        if ! _panel_tls_sync_path "$parent"; then
            mv -T -- "$tls_dir" "$candidate" || { _panel_tls_fail "cannot quarantine unsynced restored tree"; return 1; }
            (( old_moved == 0 )) || _panel_tls_restore_old_tree "$previous" "$tls_dir" || exit 1
            _panel_tls_fail "cannot durably publish restored TLS tree"; return 1
        fi
    fi
    if (( old_moved == 1 )); then
        _panel_tls_mount_guard "$previous" || exit 1
        rm -rf --one-file-system -- "$previous" || { _panel_tls_fail "cannot remove quarantined TLS tree"; return 1; }
        _panel_tls_sync_path "$stage" || exit 1
    fi
    rmdir -- "$stage" || { _panel_tls_fail "cannot remove TLS restore stage"; return 1; }
    trap - EXIT HUP INT TERM

    state=$(tr -d '[:space:]' <"$root/hook.state") || exit 1
    if [[ $state == present ]]; then _panel_tls_restore_file "$root/hook/celikpanel-panel-cert" "$hook_file" 1048576 || exit 1
    else _panel_tls_remove_file "$hook_file" 1048576 || exit 1; fi
    state=$(tr -d '[:space:]' <"$root/pending.state") || exit 1
    if [[ $state == present ]]; then _panel_tls_restore_file "$root/pending/panel-certificate-activation.json" "$pending_file" 4096 600 || exit 1
    else _panel_tls_remove_file "$pending_file" 4096 || exit 1; fi
)

_panel_tls_restore_enabled_state() {
    local unit=${1:?unit required} saved=${2:?state required}
    case "$saved" in
        enabled) systemctl enable "$unit" >/dev/null || { _panel_tls_fail "cannot enable $unit"; return 1; };;
        enabled-runtime) systemctl enable --runtime "$unit" >/dev/null || { _panel_tls_fail "cannot runtime-enable $unit"; return 1; };;
        disabled) systemctl disable "$unit" >/dev/null || { _panel_tls_fail "cannot disable $unit"; return 1; };;
        masked) systemctl mask "$unit" >/dev/null || { _panel_tls_fail "cannot mask $unit"; return 1; };;
        masked-runtime) systemctl mask --runtime "$unit" >/dev/null || { _panel_tls_fail "cannot runtime-mask $unit"; return 1; };;
        linked|linked-runtime|alias|static|indirect|generated|not-found) :;;
        *) _panel_tls_fail "unsupported scheduler enabled state: $saved"; return 1;;
    esac
}

_panel_tls_restore_active_state() {
    local unit=${1:?unit required} saved=${2:?state required}
    case "$saved" in active) systemctl start "$unit" || { _panel_tls_fail "cannot restart $unit"; return 1; };; inactive) systemctl stop "$unit" || { _panel_tls_fail "cannot keep $unit inactive"; return 1; };; *) _panel_tls_fail "unsupported active state: $saved"; return 1;; esac
}

panel_tls_restore_certbot_scheduler() {
    local root=${1:?snapshot required} unit file saved load enabled active
    local actual_load actual_enabled actual_active extra
    panel_tls_snapshot_validate "$root" || return 1
    _panel_tls_assert_scheduler_quiesced "$root" || return 1
    # Services first; timers last. Caller must hold the durable scheduler-restore
    # marker and already have released the mutation lock.
    for file in certbot-services.tsv certbot-timers.tsv; do
        while IFS=$'\t' read -r unit load enabled active extra || [[ -n ${unit}${load}${enabled}${active}${extra} ]]; do
            [[ $load == not-found || $load == masked ]] || {
                _panel_tls_restore_enabled_state "$unit" "$enabled" || return 1
                _panel_tls_restore_active_state "$unit" "$active" || return 1
            }
        done <"$root/$file"
    done
    for file in certbot-services.tsv certbot-timers.tsv; do
        while IFS=$'\t' read -r unit load enabled active extra || [[ -n ${unit}${load}${enabled}${active}${extra} ]]; do
            actual_load=$(_panel_tls_systemctl_load_state "$unit") || return 1
            actual_enabled=$(_panel_tls_systemctl_enabled_state "$unit") || return 1
            actual_active=$(_panel_tls_systemctl_active_state "$unit") || return 1
            _panel_tls_validate_unit_state_tuple scheduler "$unit" "$actual_load" "$actual_enabled" "$actual_active" || return 1
            [[ $actual_load == "$load" ]] || { _panel_tls_fail "LoadState not restored for $unit"; return 1; }
            [[ $actual_enabled == "$enabled" ]] || { _panel_tls_fail "enabled state not restored for $unit"; return 1; }
            [[ $actual_active == "$active" ]] || { _panel_tls_fail "active state not restored for $unit"; return 1; }
            [[ $unit != *.timer ]] || _panel_tls_assert_timer_definition "$root" "$unit" || return 1
        done <"$root/$file"
    done
}
