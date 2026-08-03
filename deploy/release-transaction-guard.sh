#!/usr/bin/env bash

# This library is sourced only after the caller has verified the complete
# immutable release manifest. It must never be executed as a standalone tool.
# Bu kütüphane yalnız çağıran eksiksiz değişmez sürüm manifestini doğruladıktan
# sonra source edilir. Asla bağımsız bir araç olarak çalıştırılmamalıdır.
if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
    printf '%s\n' "release transaction guard must be sourced / release işlem koruması source edilmelidir" >&2
    exit 1
fi

# Pin the sourced bytes to the already verified running release. A mutable
# checkout, alias or sibling copy is never accepted after privilege is gained.
# Source edilen baytları önceden doğrulanmış çalışan sürüme sabitle. Ayrıcalık
# alındıktan sonra değişebilir checkout, takma yol veya kardeş kopya kabul edilmez.
_release_txn_expected_root=${TRUSTED_RELEASE_ROOT:-${CELIKPANEL_TRUSTED_RELEASE_ROOT:-}}
_release_txn_library_relative=deploy/release-transaction-guard.sh
if [[ -z "$_release_txn_expected_root" || "$_release_txn_expected_root" != /* ]]; then
    printf '%s\n' "trusted release root is missing while sourcing release guard" >&2
    return 1
fi
_release_txn_canonical_root=$(readlink -e -- "$_release_txn_expected_root") || {
    printf '%s\n' "trusted release root cannot be resolved while sourcing release guard" >&2
    return 1
}
_release_txn_running_library=$(readlink -e -- "${BASH_SOURCE[0]}") || {
    printf '%s\n' "release transaction guard source cannot be resolved" >&2
    return 1
}
if [[ "$_release_txn_canonical_root" != "$_release_txn_expected_root" ||
      "$_release_txn_running_library" != "$_release_txn_expected_root/$_release_txn_library_relative" ]]; then
    printf '%s\n' "release transaction guard is outside the verified running release" >&2
    return 1
fi
unset _release_txn_expected_root _release_txn_library_relative
unset _release_txn_canonical_root _release_txn_running_library

_release_txn_fail() {
    printf 'release transaction guard: %s\n' "$1" >&2
    return 1
}

release_txn_validate_token() {
    local token=${1:-}
    [[ "$token" =~ ^[0-9a-f]{64}$ ]] \
        || _release_txn_fail "transaction token must be exactly 64 lowercase hexadecimal characters"
}

release_txn_validate_operation() {
    local operation=${1:-}
    [[ "$operation" == update || "$operation" == rollback ]] \
        || _release_txn_fail "transaction operation must be update or rollback"
}

release_txn_validate_snapshot_name() {
    local snapshot=${1:-}
    [[ "$snapshot" != . && "$snapshot" != .. &&
       "$snapshot" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] \
        || _release_txn_fail "transaction snapshot must be a safe basename"
}

# Update snapshot names bind the durable timestamp, full target commit and the
# staging nonce. Generic marker basenames are not sufficient for recovery.
# Update snapshot adları kalıcı zaman damgasını, tam hedef commit'i ve staging
# nonce'unu bağlar. Genel marker basename doğrulaması kurtarma için yeterli değildir.
release_txn_parse_update_snapshot_name() {
    local snapshot=${1:-}
    local pattern='^([0-9]{8}T[0-9]{6}Z)-from-unknown-to-([0-9a-f]{40})-([0-9a-f]{32})$'
    [[ "$snapshot" =~ $pattern ]] \
        || { _release_txn_fail "update snapshot name is not canonical"; return 1; }
    printf '%s\t%s\t%s\n' "${BASH_REMATCH[1]}" "${BASH_REMATCH[2]}" "${BASH_REMATCH[3]}"
}

# Validate coordinator identity.
# Koordinatör kimliğini doğrula.
_release_txn_validate_quiesce_coordinators() {
    local path=$1 unit state pid start_time extra expected
    local owner group mode links size index
    local -a rows=()
    [[ -f "$path" && ! -L "$path" ]] || { _release_txn_fail "quiesce coordinator ledger is missing or unsafe"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$path") || { _release_txn_fail "cannot inspect quiesce coordinator ledger"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 && "$size" -gt 0 && "$size" -le 1024 ]] || { _release_txn_fail "quiesce coordinator ledger metadata is invalid"; return 1; }
    mapfile -t rows < "$path" || { _release_txn_fail "cannot read quiesce coordinator ledger"; return 1; }
    [[ ${#rows[@]} -eq 2 ]] || { _release_txn_fail "quiesce coordinator ledger must contain exactly two rows"; return 1; }
    for index in 0 1; do
        case "$index" in
            0) expected=celikpanel-agent.service ;;
            1) expected=celikpanel-panel.service ;;
        esac
        IFS=$'\t' read -r unit state pid start_time extra <<< "${rows[$index]}"
        [[ -n "$unit" && -n "$state" && -n "$pid" && -n "$start_time" && -z "${extra:-}" && "$unit" == "$expected" ]] || { _release_txn_fail "quiesce coordinator ledger is malformed or out of order"; return 1; }
        case "$state" in
            active|activating|reloading|refreshing)
                [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 1 && "$start_time" =~ ^[0-9]+$ && "$start_time" -gt 0 ]] || { _release_txn_fail "active quiesce coordinator identity is invalid: $unit"; return 1; }
                ;;
            inactive|failed)
                [[ "$pid" == 0 && "$start_time" == 0 ]] || { _release_txn_fail "inactive quiesce coordinator must use canonical 0/0 identity: $unit"; return 1; }
                ;;
            *) _release_txn_fail "unsupported quiesce coordinator state: $unit: $state"; return 1 ;;
        esac
    done
    cmp -s "$path" <(printf '%s\n' "${rows[@]}") || { _release_txn_fail "quiesce coordinator ledger bytes are not canonical"; return 1; }
}

# A resumable pre-publish tree is one exact root-only direct child containing
# only the target snapshot child. Every partial payload object is non-link,
# non-special, root-owned and not writable by group or others.
# Devam ettirilebilir yayın-öncesi ağaç yalnız hedef snapshot child'ını içeren tam
# bir root-only doğrudan alt dizindir. Her kısmi payload nesnesi bağlantısız,
# özel olmayan, root sahipli ve group/other tarafından yazılamazdır.
release_txn_validate_update_snapshot_stage() {
    local snapshot_root=$1 snapshot=$2 stage=$3 relative stage_name child ledger transition coordinators
    local created target nonce entries entry owner group mode links size permissions
    _release_txn_validate_root_directory "$snapshot_root" 700 || return 1
    IFS=$'\t' read -r created target nonce < <(release_txn_parse_update_snapshot_name "$snapshot") \
        || return 1
    [[ -n "$created" && -n "$target" && -n "$nonce" ]] \
        || { _release_txn_fail "update snapshot name fields are incomplete"; return 1; }
    [[ "$stage" == "$snapshot_root/"* ]] \
        || { _release_txn_fail "snapshot staging path is outside snapshot storage"; return 1; }
    relative=${stage#"$snapshot_root/"}
    [[ -n "$relative" && "$relative" != */* ]] \
        || { _release_txn_fail "snapshot staging path is not a direct child"; return 1; }
    stage_name=$relative
    [[ "$stage_name" =~ ^\.release-snapshot\.incomplete\.([1-9][0-9]*)\.$nonce$ ]] \
        || { _release_txn_fail "snapshot staging basename is not bound to its nonce"; return 1; }
    _release_txn_validate_root_directory "$stage" 700 || return 1
    child=$stage/$snapshot
    _release_txn_validate_root_directory "$child" 700 || return 1
    entries=$(find "$stage" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort) \
        || { _release_txn_fail "cannot inspect snapshot staging entries"; return 1; }
    [[ "$entries" == "$snapshot" ]] \
        || { _release_txn_fail "snapshot staging root contains unexpected entries"; return 1; }
    if find "$stage" -type l -print -quit | grep -q .; then
        _release_txn_fail "snapshot staging tree contains a symbolic link"
        return 1
    fi
    if find "$stage" ! -type d ! -type f -print -quit | grep -q .; then
        _release_txn_fail "snapshot staging tree contains a special object"
        return 1
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$entry") \
            || { _release_txn_fail "cannot inspect snapshot staging object"; return 1; }
        permissions=$((8#$mode))
        [[ "$owner" == 0 ]] && (( (permissions & 0022) == 0 )) \
            || { _release_txn_fail "snapshot staging objects must be root-owned and group/other non-writable"; return 1; }
    done < <(find "$stage" -mindepth 1 -print0)
    ledger=$child/service-states.tsv
    [[ -f "$ledger" && ! -L "$ledger" ]] \
        || { _release_txn_fail "snapshot staging service-state ledger is missing or unsafe"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$ledger") \
        || { _release_txn_fail "cannot inspect snapshot staging service-state ledger"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 4096 ]] \
        || { _release_txn_fail "snapshot staging service-state ledger metadata is invalid"; return 1; }
    coordinators=$child/quiesce-coordinators.tsv
    _release_txn_validate_quiesce_coordinators "$coordinators" || return 1
    transition=$child/snapshot-transition.state
    [[ -f "$transition" && ! -L "$transition" ]] \
        || { _release_txn_fail "snapshot staging transition state is missing or unsafe"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$transition") \
        || { _release_txn_fail "cannot inspect snapshot staging transition state"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 32 ]] \
        || { _release_txn_fail "snapshot staging transition state metadata is invalid"; return 1; }
    if ! printf 'normal\n' | cmp -s - "$transition" &&
       ! printf 'pre-ledger\n' | cmp -s - "$transition" &&
       ! printf 'schema17\n' | cmp -s - "$transition"; then
        _release_txn_fail "snapshot staging transition state is not canonical"
        return 1
    fi
}

release_txn_find_update_snapshot_stage() {
    local snapshot_root=$1 snapshot=$2 created target nonce candidate found= count=0
    IFS=$'\t' read -r created target nonce < <(release_txn_parse_update_snapshot_name "$snapshot") \
        || return 1
    [[ ! -e "$snapshot_root/$snapshot" && ! -L "$snapshot_root/$snapshot" ]] \
        || { _release_txn_fail "final snapshot already exists; explicit rollback is required"; return 1; }
    while IFS= read -r -d '' candidate; do
        release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "$candidate" \
            || return 1
        found=$candidate
        count=$((count + 1))
    done < <(find "$snapshot_root" -mindepth 1 -maxdepth 1 \
        -name ".release-snapshot.incomplete.*.$nonce" -print0)
    [[ "$count" -eq 1 ]] \
        || { _release_txn_fail "exactly one resumable snapshot staging tree is required"; return 1; }
    printf '%s\n' "$found"
}

# Reset only verified direct children while preserving the canonical durable
# service-state ledger and transition mode. SIGKILL remains safely retryable.
# Yalnız doğrulanmış doğrudan alt girdileri temizlerken kanonik kalıcı servis-durum
# ledger'ı ile geçiş modunu koru. SIGKILL güvenle yeniden denenebilir kalır.
release_txn_reset_update_snapshot_stage() {
    local snapshot_root=$1 snapshot=$2 stage=$3 child entry
    local -a removable=()
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "$stage" \
        || return 1
    child=$stage/$snapshot
    while IFS= read -r -d '' entry; do
        case "$(basename -- "$entry")" in
            service-states.tsv|quiesce-coordinators.tsv|snapshot-transition.state|panel-tls) continue ;;
        esac
        removable+=("$entry")
    done < <(find "$child" -mindepth 1 -maxdepth 1 -print0)
    if [[ ${#removable[@]} -gt 0 ]]; then
        rm -rf -- "${removable[@]}" \
            || { _release_txn_fail "cannot reset resumable snapshot staging payload"; return 1; }
    fi
    if [[ -e "$child/panel-tls" || -L "$child/panel-tls" ]]; then
        [[ -d "$child/panel-tls" && ! -L "$child/panel-tls" ]] || return 1
        sync -f -- "$child/panel-tls" || return 1
    fi
    sync -f -- "$child/service-states.tsv" "$child/quiesce-coordinators.tsv" \
        "$child/snapshot-transition.state" \
        "$child" "$stage" "$snapshot_root" \
        || { _release_txn_fail "cannot make resumable snapshot staging reset durable"; return 1; }
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "$stage"
}

# A single markerless canonical stage is removable only as an empty post-publish
# shell or as a complete pre-publish recovery scaffold. Everything else fails closed.
# Tek işaretçisiz kanonik stage yalnız boş bir yayın-sonrası kabuk ya da eksiksiz
# bir yayın-öncesi kurtarma iskeleti olarak kaldırılabilir. Diğer her durum fail-closed olur.
release_txn_cleanup_unmarked_update_snapshot_stage() {
    local transaction_root=$1 inherited_fd=$2 snapshot_root=$3 candidate candidate_child stage_name nonce
    local snapshot created target snapshot_nonce marker
    local -a candidates=() children=()
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    _release_txn_validate_root_directory "$transaction_root" 700 || return 1
    _release_txn_validate_root_directory "$snapshot_root" 700 || return 1
    _release_txn_require_scheduler_restore_absent "$transaction_root" || return 1
    for marker in quiesce.pending active completion.pending scheduler-restore.pending; do
        [[ ! -e "$transaction_root/$marker" && ! -L "$transaction_root/$marker" ]] \
            || { _release_txn_fail "unmarked snapshot cleanup requires no transaction marker"; return 1; }
    done
    while IFS= read -r -d '' candidate; do
        candidates+=("$candidate")
    done < <(find "$snapshot_root" -mindepth 1 -maxdepth 1 \
        -name '.release-snapshot.incomplete*' -print0)
    [[ ${#candidates[@]} -le 1 ]] \
        || { _release_txn_fail "multiple unmarked snapshot staging entries require manual repair"; return 1; }
    [[ ${#candidates[@]} -eq 1 ]] || return 0
    candidate=${candidates[0]}
    [[ -d "$candidate" && ! -L "$candidate" ]] \
        || { _release_txn_fail "unmarked snapshot staging entry is not a safe directory; manual repair is required"; return 1; }
    stage_name=${candidate#"$snapshot_root/"}
    [[ -n "$stage_name" && "$stage_name" != */* &&
       "$stage_name" =~ ^\.release-snapshot\.incomplete\.([1-9][0-9]*)\.([0-9a-f]{32})$ ]] \
        || { _release_txn_fail "unmarked snapshot staging basename is not canonical; manual repair is required"; return 1; }
    nonce=${BASH_REMATCH[2]}
    _release_txn_validate_root_directory "$candidate" 700 \
        || { _release_txn_fail "unmarked snapshot staging metadata is unsafe; manual repair is required"; return 1; }
    children=()
    while IFS= read -r -d '' candidate_child; do
        children+=("$candidate_child")
    done < <(find "$candidate" -mindepth 1 -maxdepth 1 -print0)
    if [[ ${#children[@]} -eq 0 ]]; then
        rmdir -- "$candidate" \
            || { _release_txn_fail "cannot remove empty canonical unmarked snapshot staging"; return 1; }
        sync -f -- "$snapshot_root" \
            || { _release_txn_fail "cannot make empty unmarked snapshot cleanup durable"; return 1; }
        [[ ! -e "$candidate" && ! -L "$candidate" ]] \
            || { _release_txn_fail "empty unmarked snapshot staging remains after cleanup"; return 1; }
        return 0
    fi
    [[ ${#children[@]} -eq 1 ]] \
        || { _release_txn_fail "unmarked snapshot staging must contain exactly one snapshot child; manual repair is required"; return 1; }
    snapshot=$(basename -- "${children[0]}")
    IFS=$'\t' read -r created target snapshot_nonce < <(release_txn_parse_update_snapshot_name "$snapshot") \
        || { _release_txn_fail "unmarked snapshot child name is not canonical; manual repair is required"; return 1; }
    [[ "$snapshot_nonce" == "$nonce" ]] \
        || { _release_txn_fail "unmarked snapshot staging nonce does not match its child; manual repair is required"; return 1; }
    [[ ! -e "$snapshot_root/$snapshot" && ! -L "$snapshot_root/$snapshot" ]] \
        || { _release_txn_fail "final snapshot already exists; explicit rollback is required"; return 1; }
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "${candidates[0]}" \
        || { _release_txn_fail "unmarked snapshot staging failed canonical validation; manual repair is required"; return 1; }
    rm -rf -- "${candidates[0]}" \
        || { _release_txn_fail "cannot remove canonical unmarked snapshot staging"; return 1; }
    sync -f -- "$snapshot_root" \
        || { _release_txn_fail "cannot make unmarked snapshot cleanup durable"; return 1; }
}
_release_txn_print_marker() {
    local token=$1 operation=$2 snapshot=$3
    release_txn_validate_token "$token" || return 1
    release_txn_validate_operation "$operation" || return 1
    release_txn_validate_snapshot_name "$snapshot" || return 1
    printf 'version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n' \
        "$token" "$operation" "$snapshot"
}
# The fixed transaction root and lock are root-only persistent coordination
# objects. A hard link, symlink or permission drift is a fail-closed condition.
# Sabit işlem kökü ve kilidi yalnız root'a açık kalıcı koordinasyon nesneleridir.
# Hard link, sembolik bağ veya izin sapması fail-closed durumudur.
release_txn_validate_root() {
    local root=$1 owner group mode
    [[ "$root" == /* && -d "$root" && ! -L "$root" ]] \
        || { _release_txn_fail "unsafe transaction root: $root"; return 1; }
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$root") \
        || { _release_txn_fail "cannot inspect transaction root: $root"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || { _release_txn_fail "transaction root must be root:root mode 0700"; return 1; }
}

release_txn_validate_lock_file() {
    local root=$1 lock owner group mode links size
    release_txn_validate_root "$root" || return 1
    lock=$root/transaction.lock
    [[ -f "$lock" && ! -L "$lock" ]] \
        || { _release_txn_fail "transaction lock is missing or unsafe"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$lock") \
        || { _release_txn_fail "cannot inspect transaction lock"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 && "$size" == 0 ]] \
        || { _release_txn_fail "transaction lock must be an empty root:root mode 0600 file with one link"; return 1; }
}

# The inherited descriptor must prove identity, exclusion and ownership: an
# independent open receives EWOULDBLOCK, then re-locking this exact OFD succeeds.
# Miras descriptor kimlik, dışlama ve sahipliği kanıtlamalıdır: bağımsız açılış
# EWOULDBLOCK alır, ardından bu tam OFD üzerinde yeniden kilitleme başarılı olur.
release_txn_verify_inherited_lock() {
    local root=$1 inherited_fd=$2 lock path_identity fd_identity probe_fd probe_rc
    [[ "$inherited_fd" =~ ^[0-9]+$ && "$inherited_fd" -ge 3 ]] \
        || { _release_txn_fail "invalid inherited transaction lock descriptor"; return 1; }
    release_txn_validate_lock_file "$root" || return 1
    lock=$root/transaction.lock
    [[ -e "/proc/$BASHPID/fd/$inherited_fd" ]] \
        || { _release_txn_fail "inherited transaction lock descriptor is closed"; return 1; }
    path_identity=$(stat -Lc '%d:%i' -- "$lock") \
        || { _release_txn_fail "cannot identify transaction lock path"; return 1; }
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$inherited_fd") \
        || { _release_txn_fail "cannot identify inherited transaction lock descriptor"; return 1; }
    [[ "$fd_identity" == "$path_identity" ]] \
        || { _release_txn_fail "inherited descriptor does not name the fixed transaction lock"; return 1; }

    exec {probe_fd}<>"$lock" \
        || { _release_txn_fail "cannot open an independent transaction lock probe"; return 1; }
    if flock -n -E 75 "$probe_fd" 2>/dev/null; then
        flock -u "$probe_fd" >/dev/null 2>&1 || true
        exec {probe_fd}>&-
        _release_txn_fail "inherited transaction lock is not held"
        return 1
    else
        probe_rc=$?
    fi
    exec {probe_fd}>&-
    [[ "$probe_rc" -eq 75 ]] \
        || { _release_txn_fail "independent transaction lock probe failed unexpectedly"; return 1; }
    # Busy alone is insufficient: a foreign process could own the flock while
    # this caller merely opened an unlocked descriptor to the same inode.
    # Yalnız busy kanıtı yetersizdir: yabancı süreç flock sahibi iken bu çağıran
    # aynı inode'a yalnız kilitsiz bir descriptor açmış olabilir.
    flock -n -x "$inherited_fd" 2>/dev/null \
        || { _release_txn_fail "inherited descriptor does not own the transaction flock"; return 1; }
}

release_txn_generate_token() {
    local token
    token=$(od -An -N32 -tx1 /dev/urandom | tr -d '[:space:]') \
        || { _release_txn_fail "cannot generate transaction token"; return 1; }
    release_txn_validate_token "$token" || return 1
    printf '%s\n' "$token"
}

# Read only canonical marker fields after exact root-owned metadata and byte
# validation; callers never parse an untrusted marker on their own.
# Yalnız tam root metadata ve bayt doğrulamasından sonra kanonik işaretçi
# alanlarını oku; çağıranlar güvenilmez işaretçiyi kendileri ayrıştırmaz.
_release_txn_read_marker_fields() {
    local root=$1 marker_name=$2 marker owner group mode links size
    local token operation snapshot
    local -a marker_lines
    release_txn_validate_root "$root" || return 1
    [[ "$marker_name" == quiesce.pending || "$marker_name" == active || "$marker_name" == completion.pending || "$marker_name" == scheduler-restore.pending ]] \
        || { _release_txn_fail "unsupported transaction marker name"; return 1; }
    marker=$root/$marker_name
    [[ -f "$marker" && ! -L "$marker" ]] \
        || { _release_txn_fail "transaction marker is missing or unsafe: $marker_name"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$marker") \
        || { _release_txn_fail "cannot inspect transaction marker: $marker_name"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 512 ]] \
        || { _release_txn_fail "transaction marker metadata is invalid: $marker_name"; return 1; }
    mapfile -t marker_lines < "$marker"
    [[ ${#marker_lines[@]} -eq 4 && "${marker_lines[0]}" == version=1 ]] \
        || { _release_txn_fail "transaction marker is not canonical: $marker_name"; return 1; }
    token=${marker_lines[1]#token=}
    operation=${marker_lines[2]#operation=}
    snapshot=${marker_lines[3]#snapshot=}
    [[ "${marker_lines[1]}" == "token=$token" ]] \
        || { _release_txn_fail "transaction marker token field is malformed"; return 1; }
    [[ "${marker_lines[2]}" == "operation=$operation" ]] \
        || { _release_txn_fail "transaction marker operation field is malformed"; return 1; }
    [[ "${marker_lines[3]}" == "snapshot=$snapshot" ]] \
        || { _release_txn_fail "transaction marker snapshot field is malformed"; return 1; }
    release_txn_validate_token "$token" || return 1
    release_txn_validate_operation "$operation" || return 1
    release_txn_validate_snapshot_name "$snapshot" || return 1
    cmp -s "$marker" <(_release_txn_print_marker "$token" "$operation" "$snapshot") \
        || { _release_txn_fail "transaction marker bytes are not canonical: $marker_name"; return 1; }
    printf '%s\t%s\t%s\n' "$token" "$operation" "$snapshot"
}

release_txn_read_quiesce_fields() {
    _release_txn_read_marker_fields "$1" quiesce.pending
}

release_txn_read_active_fields() {
    _release_txn_read_marker_fields "$1" active
}

release_txn_read_pending_fields() {
    _release_txn_read_marker_fields "$1" completion.pending
}

release_txn_read_scheduler_restore_fields() {
    _release_txn_read_marker_fields "$1" scheduler-restore.pending
}
_release_txn_validate_marker() {
    local root=$1 marker_name=$2 expected_token=$3 expected_operation=$4 expected_snapshot=$5
    local marker owner group mode links size
    release_txn_validate_token "$expected_token" || return 1
    release_txn_validate_operation "$expected_operation" || return 1
    release_txn_validate_snapshot_name "$expected_snapshot" || return 1
    release_txn_validate_root "$root" || return 1
    [[ "$marker_name" == quiesce.pending || "$marker_name" == active || "$marker_name" == completion.pending || "$marker_name" == scheduler-restore.pending ]] \
        || { _release_txn_fail "unsupported transaction marker name"; return 1; }
    marker=$root/$marker_name
    [[ -f "$marker" && ! -L "$marker" ]] \
        || { _release_txn_fail "transaction marker is missing or unsafe: $marker_name"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$marker") \
        || { _release_txn_fail "cannot inspect transaction marker: $marker_name"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 512 ]] \
        || { _release_txn_fail "transaction marker metadata is invalid: $marker_name"; return 1; }
    cmp -s "$marker" <(_release_txn_print_marker \
        "$expected_token" "$expected_operation" "$expected_snapshot") \
        || { _release_txn_fail "transaction marker fields mismatch: $marker_name"; return 1; }
}

release_txn_validate_quiesce_token() {
    [[ "$3" == update ]] || { _release_txn_fail "quiesce coordinator identity is defined only for update"; return 1; }
    release_txn_validate_update_snapshot_stage "$5" "$4" "$6" || return 1
    _release_txn_validate_marker "$1" quiesce.pending "$2" "$3" "$4"
}

release_txn_validate_active_token() {
    _release_txn_validate_marker "$1" active "$2" "$3" "$4"
}

release_txn_validate_pending_token() {
    _release_txn_validate_marker "$1" completion.pending "$2" "$3" "$4"
}

release_txn_validate_scheduler_restore_token() {
    _release_txn_validate_marker "$1" scheduler-restore.pending "$2" "$3" "$4"
}

_release_txn_require_scheduler_restore_absent() {
    local root=$1
    [[ ! -e "$root/scheduler-restore.pending" && ! -L "$root/scheduler-restore.pending" ]] \
        || { _release_txn_fail "scheduler restore must be finalized before another transaction transition"; return 1; }
}
# A failed update may be taken over by rollback only for the exact same
# snapshot. Preserve the token and atomically rewrite update to rollback; a
# rerun after either side of the rename is idempotent.
# Başarısız update yalnız tam aynı snapshot için rollback tarafından devralınır.
# Token'ı koru ve update'i atomik olarak rollback'e çevir; rename'in iki
# tarafındaki yeniden çalıştırma idempotenttir.
release_txn_takeover_active_for_rollback() {
    local root=$1 inherited_fd=$2 requested_snapshot=$3
    local marker token operation snapshot tmp
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    release_txn_validate_snapshot_name "$requested_snapshot" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" ]] \
        || { _release_txn_fail "quiesce.pending must be recovered by update before rollback takeover"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion.pending must be finalized before rollback takeover"; return 1; }
    [[ -e "$root/active" || -L "$root/active" ]] \
        || { _release_txn_fail "no active transaction is available for rollback takeover"; return 1; }
    IFS=$'\t' read -r token operation snapshot \
        < <(release_txn_read_active_fields "$root") \
        || { _release_txn_fail "cannot read active transaction for rollback takeover"; return 1; }
    [[ "$snapshot" == "$requested_snapshot" ]] \
        || { _release_txn_fail "rollback snapshot differs from the active transaction"; return 1; }
    case "$operation" in
        rollback)
            release_txn_validate_active_token "$root" "$token" rollback "$snapshot" || return 1
            printf '%s\n' "$token"
            return 0
            ;;
        update) ;;
        *)
            _release_txn_fail "active transaction operation cannot be taken over by rollback"
            return 1
            ;;
    esac

    marker=$root/active
    tmp=$(mktemp -p "$root" '.rollback-takeover.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot stage rollback takeover marker"; return 1; }
    if ! _release_txn_print_marker "$token" rollback "$snapshot" > "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0600 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T -- "$tmp" "$marker" ||
       ! sync -f -- "$root"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        _release_txn_fail "cannot durably publish rollback takeover marker"
        return 1
    fi
    release_txn_validate_active_token "$root" "$token" rollback "$snapshot" || return 1
    printf '%s\n' "$token"
}
# Publish a durable pre-active phase before freezing either coordinator.
# Koordinatörlerden biri dondurulmadan önce kalıcı bir active-öncesi aşama yayımla.
release_txn_create_quiesce_marker() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 snapshot_root=$6 stage=$7 marker tmp
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    release_txn_validate_token "$token" || return 1
    release_txn_validate_operation "$operation" || return 1
    release_txn_validate_snapshot_name "$snapshot" || return 1
    [[ "$operation" == update ]] || { _release_txn_fail "quiesce coordinator identity is defined only for update"; return 1; }
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "$stage" || return 1
    marker=$root/quiesce.pending
    [[ ! -e "$marker" && ! -L "$marker" ]] \
        || { _release_txn_fail "quiesce transaction marker already exists"; return 1; }
    [[ ! -e "$root/active" && ! -L "$root/active" ]] \
        || { _release_txn_fail "active transaction marker already exists"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion-pending transaction marker already exists"; return 1; }
    tmp=$(mktemp -p "$root" '.quiesce.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot create quiesce transaction marker staging file"; return 1; }
    if ! _release_txn_print_marker "$token" "$operation" "$snapshot" > "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0600 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T --no-clobber -- "$tmp" "$marker" ||
       ! sync -f -- "$root"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        _release_txn_fail "cannot durably publish quiesce transaction marker"
        return 1
    fi
    release_txn_validate_quiesce_token "$root" "$token" "$operation" "$snapshot" "$snapshot_root" "$stage"
}

# Promote the exact durable phase to active with one atomic rename; there is
# no gap in which ordinary service starts can escape the systemd guard.
# Tam kalıcı aşamayı tek atomik rename ile active yap; iki marker arasında
# normal servis başlangıcının systemd korumasından kaçabileceği boşluk yoktur.
release_txn_promote_quiesce_to_active() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 snapshot_root=$6 stage=$7
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    release_txn_validate_quiesce_token "$root" "$token" "$operation" "$snapshot" "$snapshot_root" "$stage" || return 1
    [[ ! -e "$root/active" && ! -L "$root/active" ]] \
        || { _release_txn_fail "active transaction marker already exists"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion-pending transaction marker already exists"; return 1; }
    mv -T --no-clobber -- "$root/quiesce.pending" "$root/active" \
        || { _release_txn_fail "cannot promote quiesce marker to active"; return 1; }
    sync -f -- "$root" \
        || { _release_txn_fail "cannot make quiesce-to-active rename durable"; return 1; }
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" ]] \
        || { _release_txn_fail "quiesce marker remains after active promotion"; return 1; }
    release_txn_validate_active_token "$root" "$token" "$operation" "$snapshot"
}

# Abort only the exact pre-active phase; callers must first resume and verify
# frozen services so marker removal cannot strand a stopped coordinator.
# Yalnızca tam active öncesi aşamayı iptal et; çağıran önce dondurulmuş servisleri
# sürdürüp doğrulamalıdır; böylece marker silme bir koordinatörü durmuş durumda
# bırakamaz.
release_txn_remove_quiesce_marker() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 snapshot_root=$6 stage=$7
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    release_txn_validate_quiesce_token "$root" "$token" "$operation" "$snapshot" "$snapshot_root" "$stage" || return 1
    [[ ! -e "$root/active" && ! -L "$root/active" ]] \
        || { _release_txn_fail "active marker cannot coexist with quiesce abort"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion marker cannot coexist with quiesce abort"; return 1; }
    rm -f -- "$root/quiesce.pending" \
        || { _release_txn_fail "cannot remove quiesce transaction marker"; return 1; }
    sync -f -- "$root" \
        || { _release_txn_fail "cannot make quiesce marker removal durable"; return 1; }
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" ]] \
        || { _release_txn_fail "quiesce transaction marker still exists"; return 1; }
}

# Remove only the exact active marker for an update that has not crossed its
# caller-proven mutation boundary. This primitive deliberately does not infer
# "pre-mutation" from a transition label: the trusted recovery helper must first
# prove the exact stage allowlist, saved service state, stopped coordinators and
# unchanged installed bytes while holding both coordination locks.
#
# The final snapshot must still be absent and the supplied stage must be the
# one canonical stage bound to this marker. Marker removal is made durable
# before returning so a caller can restore services without exposing traffic
# while an active transaction still exists.
release_txn_remove_pre_mutation_active_marker() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5
    local snapshot_root=$6 stage=$7 found marker
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    [[ "$operation" == update ]] \
        || { _release_txn_fail "pre-mutation active abort is defined only for update"; return 1; }
    release_txn_validate_active_token "$root" "$token" "$operation" "$snapshot" || return 1
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" ]] \
        || { _release_txn_fail "quiesce marker cannot coexist with pre-mutation active abort"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion marker cannot coexist with pre-mutation active abort"; return 1; }
    found=$(release_txn_find_update_snapshot_stage "$snapshot_root" "$snapshot") \
        || return 1
    [[ "$found" == "$stage" ]] \
        || { _release_txn_fail "pre-mutation active abort stage does not match the canonical stage"; return 1; }
    release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "$stage" || return 1

    marker=$root/active
    rm -f -- "$marker" \
        || { _release_txn_fail "cannot remove pre-mutation active transaction marker"; return 1; }
    sync -f -- "$root" \
        || { _release_txn_fail "cannot make pre-mutation active marker removal durable"; return 1; }
    [[ ! -e "$marker" && ! -L "$marker" ]] \
        || { _release_txn_fail "pre-mutation active transaction marker still exists"; return 1; }
}

# Publish one marker only after its bytes are durable, then fsync the parent
# directory after the no-clobber rename.
# Tek işaretçiyi yalnız baytları dayanıklı olduktan sonra yayımla; no-clobber
# rename sonrasında üst dizini fsync et.
release_txn_create_active_marker() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 marker tmp
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    release_txn_validate_token "$token" || return 1
    release_txn_validate_operation "$operation" || return 1
    release_txn_validate_snapshot_name "$snapshot" || return 1
    marker=$root/active
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" ]] \
        || { _release_txn_fail "quiesce transaction marker already exists"; return 1; }
    [[ ! -e "$marker" && ! -L "$marker" ]] \
        || { _release_txn_fail "active transaction marker already exists"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion-pending transaction marker already exists"; return 1; }
    tmp=$(mktemp -p "$root" '.active.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot create active transaction marker staging file"; return 1; }
    if ! _release_txn_print_marker "$token" "$operation" "$snapshot" > "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0600 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T --no-clobber -- "$tmp" "$marker" ||
       ! sync -f -- "$root"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        _release_txn_fail "cannot durably publish active transaction marker"
        return 1
    fi
    release_txn_validate_active_token "$root" "$token" "$operation" "$snapshot"
}
# Completion is a durable rename, never an unlink of the active evidence.
# Tamamlama active kanıtını silmek değil, dayanıklı bir rename işlemidir.
release_txn_mark_completion_pending() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    _release_txn_require_scheduler_restore_absent "$root" || return 1
    release_txn_validate_active_token "$root" "$token" "$operation" "$snapshot" || return 1
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" ]] \
        || { _release_txn_fail "quiesce marker cannot coexist with active completion"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion-pending transaction marker already exists"; return 1; }
    mv -T --no-clobber -- "$root/active" "$root/completion.pending" \
        || { _release_txn_fail "cannot move transaction marker to completion-pending"; return 1; }
    sync -f -- "$root" \
        || { _release_txn_fail "cannot make completion-pending rename durable"; return 1; }
    release_txn_validate_pending_token "$root" "$token" "$operation" "$snapshot"
}

# Persist the remaining Certbot scheduler obligation before runtime completion
# evidence is removed. Re-publishing the exact marker is idempotent.
release_txn_mark_scheduler_restore_pending() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 marker tmp
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    release_txn_validate_pending_token "$root" "$token" "$operation" "$snapshot" || return 1
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" &&
       ! -e "$root/active" && ! -L "$root/active" ]] \
        || { _release_txn_fail "scheduler restore requires completion.pending as the runtime phase"; return 1; }
    marker=$root/scheduler-restore.pending
    if [[ -e "$marker" || -L "$marker" ]]; then
        release_txn_validate_scheduler_restore_token "$root" "$token" "$operation" "$snapshot"
        return
    fi
    tmp=$(mktemp -p "$root" '.scheduler-restore.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot stage scheduler-restore marker"; return 1; }
    if ! _release_txn_print_marker "$token" "$operation" "$snapshot" > "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0600 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T --no-clobber -- "$tmp" "$marker" ||
       ! sync -f -- "$root"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        _release_txn_fail "cannot durably publish scheduler-restore marker"
        return 1
    fi
    release_txn_validate_scheduler_restore_token "$root" "$token" "$operation" "$snapshot"
}
# Only the fully verified success path may remove completion.pending.
# Yalnız tamamen doğrulanmış başarı yolu completion.pending işaretçisini silebilir.
release_txn_remove_completion_pending() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    release_txn_validate_pending_token "$root" "$token" "$operation" "$snapshot" || return 1
    release_txn_validate_scheduler_restore_token "$root" "$token" "$operation" "$snapshot" || return 1
    rm -f -- "$root/completion.pending" \
        || { _release_txn_fail "cannot remove completion-pending transaction marker"; return 1; }
    sync -f -- "$root" \
        || { _release_txn_fail "cannot make completion-pending removal durable"; return 1; }
    [[ ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "completion-pending transaction marker still exists"; return 1; }
}

release_txn_remove_scheduler_restore_pending() {
    local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5
    release_txn_verify_inherited_lock "$root" "$inherited_fd" || return 1
    release_txn_validate_scheduler_restore_token "$root" "$token" "$operation" "$snapshot" || return 1
    [[ ! -e "$root/quiesce.pending" && ! -L "$root/quiesce.pending" &&
       ! -e "$root/active" && ! -L "$root/active" &&
       ! -e "$root/completion.pending" && ! -L "$root/completion.pending" ]] \
        || { _release_txn_fail "scheduler-restore marker cannot be removed before runtime completion"; return 1; }
    rm -f -- "$root/scheduler-restore.pending" \
        || { _release_txn_fail "cannot remove scheduler-restore marker"; return 1; }
    sync -f -- "$root" \
        || { _release_txn_fail "cannot make scheduler-restore marker removal durable"; return 1; }
    [[ ! -e "$root/scheduler-restore.pending" && ! -L "$root/scheduler-restore.pending" ]] \
        || { _release_txn_fail "scheduler-restore marker still exists"; return 1; }
}
_release_txn_validate_root_directory() {
    local path=$1 expected_mode=$2 owner group mode canonical
    _release_txn_validate_safe_path "$path" || return 1
    [[ -d "$path" && ! -L "$path" ]] \
        || { _release_txn_fail "unsafe root-owned directory: $path"; return 1; }
    canonical=$(readlink -e -- "$path") \
        || { _release_txn_fail "cannot resolve root-owned directory: $path"; return 1; }
    [[ "$canonical" == "$path" ]] \
        || { _release_txn_fail "root-owned directory path is not canonical: $path"; return 1; }
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$path") \
        || { _release_txn_fail "cannot inspect root-owned directory: $path"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == "$expected_mode" ]] \
        || { _release_txn_fail "root-owned directory metadata mismatch: $path"; return 1; }
}

_release_txn_validate_safe_path() {
    local path=${1:-} framed
    [[ "$path" != / && "$path" =~ ^/[A-Za-z0-9._/-]+$ && "$path" != *'//'* ]] \
        || { _release_txn_fail "unsafe fixed path: $path"; return 1; }
    framed=/${path#/}/
    [[ "$framed" != *'/./'* && "$framed" != *'/../'* ]] \
        || { _release_txn_fail "fixed path contains traversal components: $path"; return 1; }
}

_release_txn_validate_secure_parent_directory() {
    local path=$1 owner group mode canonical permissions
    _release_txn_validate_safe_path "$path" || return 1
    [[ -d "$path" && ! -L "$path" ]] \
        || { _release_txn_fail "unsafe secure parent directory: $path"; return 1; }
    canonical=$(readlink -e -- "$path") \
        || { _release_txn_fail "cannot resolve secure parent directory: $path"; return 1; }
    [[ "$canonical" == "$path" ]] \
        || { _release_txn_fail "secure parent directory path is not canonical: $path"; return 1; }
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$path") \
        || { _release_txn_fail "cannot inspect secure parent directory: $path"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" =~ ^[0-7]{3,4}$ ]] \
        || { _release_txn_fail "secure parent directory must be owned by root:root"; return 1; }
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) \
        || { _release_txn_fail "secure parent directory is group/other writable: $path"; return 1; }
}

# Some supported distributions do not create /usr/libexec until the first
# package needs it. Create exactly one missing trusted level, but only below an
# already canonical root-owned parent that is not group/other writable.
# Bazı desteklenen dağıtımlar /usr/libexec dizinini ilk paket gerektirene kadar
# oluşturmaz. Yalnız kanonik, root sahipliğinde ve güvenli bir üst dizinin
# altında eksik olan tek seviyeyi oluştur.
_release_txn_prepare_secure_parent_directory() {
    local path=$1 parent
    _release_txn_validate_safe_path "$path" || return 1
    parent=$(dirname -- "$path")
    [[ "$parent" != "$path" ]] \
        || { _release_txn_fail "cannot prepare secure parent directory at filesystem root"; return 1; }
    _release_txn_validate_secure_parent_directory "$parent" || return 1
    if [[ -e "$path" || -L "$path" ]]; then
        _release_txn_validate_secure_parent_directory "$path" || return 1
        sync -f -- "$path" "$parent" \
            || { _release_txn_fail "cannot prove secure parent directory durability: $path"; return 1; }
        return
    fi

    mkdir -m 0755 -- "$path" \
        || { _release_txn_fail "cannot create secure parent directory: $path"; return 1; }
    chown root:root -- "$path" \
        || { _release_txn_fail "cannot own secure parent directory: $path"; return 1; }
    sync -f -- "$path" "$parent" \
        || { _release_txn_fail "cannot make secure parent directory durable: $path"; return 1; }
    _release_txn_validate_secure_parent_directory "$path"
}

_release_txn_validate_start_helper() {
    local source=$1 target=$2 owner group mode links size source_hash target_hash canonical
    _release_txn_validate_safe_path "$target" || return 1
    [[ -f "$source" && ! -L "$source" ]] \
        || { _release_txn_fail "verified release start guard source is unsafe"; return 1; }
    canonical=$(readlink -e -- "$source") \
        || { _release_txn_fail "cannot resolve verified release start guard source"; return 1; }
    [[ "$canonical" == "$source" ]] \
        || { _release_txn_fail "verified release start guard source path is not canonical"; return 1; }
    [[ -f "$target" && ! -L "$target" ]] \
        || { _release_txn_fail "installed release start guard is unsafe"; return 1; }
    canonical=$(readlink -e -- "$target") \
        || { _release_txn_fail "cannot resolve installed release start guard"; return 1; }
    [[ "$canonical" == "$target" ]] \
        || { _release_txn_fail "installed release start guard path is not canonical"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$target") \
        || { _release_txn_fail "cannot inspect installed release start guard"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 755 && "$links" == 1 && "$size" -gt 0 ]] \
        || { _release_txn_fail "installed release start guard metadata is invalid"; return 1; }
    cmp -s -- "$source" "$target" \
        || { _release_txn_fail "installed release start guard bytes differ from the verified release"; return 1; }
    source_hash=$(sha256sum -- "$source") \
        || { _release_txn_fail "cannot hash verified release start guard"; return 1; }
    target_hash=$(sha256sum -- "$target") \
        || { _release_txn_fail "cannot hash installed release start guard"; return 1; }
    [[ "${source_hash%% *}" == "${target_hash%% *}" ]] \
        || { _release_txn_fail "installed release start guard hash mismatch"; return 1; }
}

# Install the ExecCondition helper outside retained releases, publish it
# atomically, and prove exact bytes, hash and root-only metadata.
# ExecCondition yardımcısını saklanan sürümlerin dışında atomik yayımla; tam
# baytları, hash'i ve yalnız root'a açık metadata'yı kanıtla.
_release_txn_install_start_helper() {
    local target=$1 source directory parent tmp owner group mode links
    source=$TRUSTED_RELEASE_ROOT/deploy/release-transaction-start-guard.sh
    _release_txn_validate_safe_path "$target" || return 1
    case "$target" in
        "$TRUSTED_RELEASE_ROOT"|"$TRUSTED_RELEASE_ROOT"/*)
            _release_txn_fail "release start guard target must be outside retained releases"
            return 1
            ;;
    esac
    [[ -f "$source" && ! -L "$source" ]] \
        || { _release_txn_fail "verified release start guard source is missing or unsafe"; return 1; }
    [[ "$(readlink -e -- "$source")" == "$source" ]] \
        || { _release_txn_fail "verified release start guard source path is not canonical"; return 1; }

    directory=$(dirname -- "$target")
    parent=$(dirname -- "$directory")
    _release_txn_prepare_secure_parent_directory "$parent" || return 1
    if [[ -e "$directory" || -L "$directory" ]]; then
        _release_txn_validate_root_directory "$directory" 755 || return 1
    else
        mkdir -m 0755 -- "$directory" \
            || { _release_txn_fail "cannot create release start guard directory"; return 1; }
        chown root:root -- "$directory" \
            || { _release_txn_fail "cannot own release start guard directory"; return 1; }
        sync -f -- "$parent" \
            || { _release_txn_fail "cannot make release start guard directory durable"; return 1; }
        _release_txn_validate_root_directory "$directory" 755 || return 1
    fi

    if [[ -e "$target" || -L "$target" ]]; then
        [[ -f "$target" && ! -L "$target" ]] \
            || { _release_txn_fail "existing release start guard is unsafe"; return 1; }
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$target") \
            || { _release_txn_fail "cannot inspect existing release start guard"; return 1; }
        [[ "$owner" == 0 && "$group" == 0 && "$mode" == 755 && "$links" == 1 ]] \
            || { _release_txn_fail "existing release start guard metadata is unsafe"; return 1; }
    fi

    tmp=$(mktemp -p "$directory" '.release-transaction-start-guard.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot stage release start guard"; return 1; }
    if ! cp --no-preserve=mode,ownership,timestamps -- "$source" "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0755 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T -- "$tmp" "$target" ||
       ! sync -f -- "$directory"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        _release_txn_fail "cannot durably install release start guard"
        return 1
    fi
    _release_txn_validate_start_helper "$source" "$target"
}

_release_txn_validate_dropin_dir() {
    _release_txn_validate_root_directory "$1" 755
}

_release_txn_validate_dropin_file() {
    local path=$1 transaction_root=$2 runtime_root=$3 helper_path=$4
    local owner group mode links size
    [[ -f "$path" && ! -L "$path" ]] \
        || { _release_txn_fail "unsafe release transaction systemd drop-in: $path"; return 1; }
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$path") \
        || { _release_txn_fail "cannot inspect release transaction systemd drop-in: $path"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 644 && "$links" == 1 && "$size" -gt 0 ]] \
        || { _release_txn_fail "release transaction systemd drop-in metadata is invalid: $path"; return 1; }
    cmp -s "$path" <(printf '[Service]\nExecCondition=+%s %s %s\n' \
        "$helper_path" "$transaction_root" "$runtime_root") \
        || { _release_txn_fail "release transaction systemd drop-in content is invalid: $path"; return 1; }
}

_release_txn_publish_dropin() {
    local directory=$1 transaction_root=$2 runtime_root=$3 helper_path=$4
    local target tmp owner group mode links
    target=$directory/10-release-transaction-guard.conf
    if [[ -e "$target" || -L "$target" ]]; then
        [[ -f "$target" && ! -L "$target" ]] \
            || { _release_txn_fail "existing release transaction drop-in is unsafe: $target"; return 1; }
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$target") \
            || { _release_txn_fail "cannot inspect existing release transaction drop-in: $target"; return 1; }
        [[ "$owner" == 0 && "$group" == 0 && "$mode" == 644 && "$links" == 1 ]] \
            || { _release_txn_fail "existing release transaction drop-in metadata is unsafe: $target"; return 1; }
    fi
    tmp=$(mktemp -p "$directory" '.10-release-transaction-guard.conf.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot create release transaction drop-in staging file"; return 1; }
    if ! printf '[Service]\nExecCondition=+%s %s %s\n' \
           "$helper_path" "$transaction_root" "$runtime_root" > "$tmp" ||
       ! chown root:root -- "$tmp" ||
       ! chmod 0644 -- "$tmp" ||
       ! sync -f -- "$tmp" ||
       ! mv -T -- "$tmp" "$target" ||
       ! sync -f -- "$directory"; then
        [[ ! -e "$tmp" && ! -L "$tmp" ]] || rm -f -- "$tmp"
        _release_txn_fail "cannot durably install release transaction systemd drop-in"
        return 1
    fi
    _release_txn_validate_dropin_file \
        "$target" "$transaction_root" "$runtime_root" "$helper_path"
}

# Install the same persistent activation guard for both coordinators, reload
# systemd, and prove that the manager reports each exact drop-in path.
# İki koordinatör için aynı kalıcı etkinleştirme korumasını kur, systemd'yi
# yeniden yükle ve yöneticinin her tam drop-in yolunu bildirdiğini kanıtla.
release_txn_install_and_verify_unit_guards() {
    local transaction_root=$1 runtime_root=$2 systemd_root=$3 helper_path=$4 inherited_fd=$5
    local unit directory dropin loaded_dropins loaded found owner group mode permissions source
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    _release_txn_validate_safe_path "$runtime_root" || return 1
    _release_txn_validate_safe_path "$systemd_root" || return 1
    _release_txn_validate_safe_path "$helper_path" || return 1
    [[ -d "$systemd_root" && ! -L "$systemd_root" ]] \
        || { _release_txn_fail "unsafe systemd unit root: $systemd_root"; return 1; }
    [[ "$(readlink -e -- "$systemd_root")" == "$systemd_root" ]] \
        || { _release_txn_fail "systemd unit root path is not canonical"; return 1; }
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$systemd_root") \
        || { _release_txn_fail "cannot inspect systemd unit root"; return 1; }
    [[ "$owner" == 0 && "$group" == 0 && "$mode" =~ ^[0-7]{3,4}$ ]] \
        || { _release_txn_fail "systemd unit root must be owned by root:root"; return 1; }
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) \
        || { _release_txn_fail "systemd unit root is group/other writable"; return 1; }

    _release_txn_install_start_helper "$helper_path" || return 1
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        directory=$systemd_root/$unit.d
        if [[ -e "$directory" || -L "$directory" ]]; then
            _release_txn_validate_dropin_dir "$directory" || return 1
        else
            mkdir -m 0755 -- "$directory" \
                || { _release_txn_fail "cannot create systemd drop-in directory: $directory"; return 1; }
            chown root:root -- "$directory" \
                || { _release_txn_fail "cannot own systemd drop-in directory: $directory"; return 1; }
            sync -f -- "$systemd_root" \
                || { _release_txn_fail "cannot make systemd drop-in directory durable"; return 1; }
            _release_txn_validate_dropin_dir "$directory" || return 1
        fi
        _release_txn_publish_dropin \
            "$directory" "$transaction_root" "$runtime_root" "$helper_path" || return 1
    done

    systemctl daemon-reload \
        || { _release_txn_fail "systemd daemon-reload failed after installing transaction guards"; return 1; }
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        dropin=$systemd_root/$unit.d/10-release-transaction-guard.conf
        _release_txn_validate_dropin_file \
            "$dropin" "$transaction_root" "$runtime_root" "$helper_path" || return 1
        loaded_dropins=$(systemctl show --property=DropInPaths --value "$unit") \
            || { _release_txn_fail "cannot inspect loaded systemd drop-ins for $unit"; return 1; }
        found=0
        for loaded in $loaded_dropins; do
            if [[ "$loaded" == "$dropin" ]]; then
                found=1
                break
            fi
        done
        [[ "$found" -eq 1 ]] \
            || { _release_txn_fail "systemd did not load the exact transaction guard for $unit"; return 1; }
    done
    source=$TRUSTED_RELEASE_ROOT/deploy/release-transaction-start-guard.sh
    _release_txn_validate_start_helper "$source" "$helper_path"
}

_release_txn_prepare_runtime_root() {
    local runtime_root=$1 parent
    _release_txn_validate_safe_path "$runtime_root" || return 1
    parent=$(dirname -- "$runtime_root")
    _release_txn_validate_secure_parent_directory "$parent" || return 1
    if [[ -e "$runtime_root" || -L "$runtime_root" ]]; then
        _release_txn_validate_root_directory "$runtime_root" 700 || return 1
    else
        mkdir -m 0700 -- "$runtime_root" \
            || { _release_txn_fail "cannot create release transaction runtime root"; return 1; }
        chown root:root -- "$runtime_root" \
            || { _release_txn_fail "cannot own release transaction runtime root"; return 1; }
        sync -f -- "$parent" \
            || { _release_txn_fail "cannot make release transaction runtime root durable"; return 1; }
        _release_txn_validate_root_directory "$runtime_root" 700 || return 1
    fi
}

_release_txn_current_marker() {
    local root=$1 token=$2 operation=$3 snapshot=$4
    local quiesce_present=0 active_present=0 pending_present=0 scheduler_present=0
    [[ -e "$root/quiesce.pending" || -L "$root/quiesce.pending" ]] && quiesce_present=1
    [[ -e "$root/active" || -L "$root/active" ]] && active_present=1
    [[ -e "$root/completion.pending" || -L "$root/completion.pending" ]] && pending_present=1
    [[ -e "$root/scheduler-restore.pending" || -L "$root/scheduler-restore.pending" ]] && scheduler_present=1
    [[ "$quiesce_present" -eq 0 && $((active_present + pending_present)) -eq 1 ]] \
        || { _release_txn_fail "start authorization requires exactly one active or completion marker"; return 1; }
    if [[ "$active_present" -eq 1 ]]; then
        [[ "$scheduler_present" -eq 0 ]] \
            || { _release_txn_fail "scheduler restore cannot coexist with active start authorization"; return 1; }
        release_txn_validate_active_token "$root" "$token" "$operation" "$snapshot" || return 1
        printf '%s\n' "$root/active"
    else
        release_txn_validate_pending_token "$root" "$token" "$operation" "$snapshot" || return 1
        if [[ "$scheduler_present" -eq 1 ]]; then
            release_txn_validate_scheduler_restore_token "$root" "$token" "$operation" "$snapshot" || return 1
        fi
        printf '%s\n' "$root/completion.pending"
    fi
}

_release_txn_holder_start_time() {
    local pid=$1 process_stat process_tail
    local -a process_fields
    [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 1 && -r "/proc/$pid/stat" ]] \
        || { _release_txn_fail "release transaction holder process is unavailable"; return 1; }
    process_stat=$(<"/proc/$pid/stat")
    process_tail=${process_stat##*) }
    read -r -a process_fields <<< "$process_tail"
    [[ ${#process_fields[@]} -ge 20 && "${process_fields[19]}" =~ ^[0-9]+$ ]] \
        || { _release_txn_fail "cannot read release transaction holder start time"; return 1; }
    printf '%s\n' "${process_fields[19]}"
}

_release_txn_print_holder() {
    local token=$1 holder_pid=$2 holder_start_time=$3 holder_fd=$4
    release_txn_validate_token "$token" || return 1
    [[ "$holder_pid" =~ ^[0-9]+$ && "$holder_pid" -gt 1 ]] \
        || { _release_txn_fail "invalid release transaction holder pid"; return 1; }
    [[ "$holder_start_time" =~ ^[0-9]+$ ]] \
        || { _release_txn_fail "invalid release transaction holder start time"; return 1; }
    [[ "$holder_fd" =~ ^[0-9]+$ && "$holder_fd" -ge 3 ]] \
        || { _release_txn_fail "invalid release transaction holder descriptor"; return 1; }
    printf 'version=1\ntoken=%s\nholder-pid=%s\nholder-start-time=%s\nholder-fd=%s\n' \
        "$token" "$holder_pid" "$holder_start_time" "$holder_fd"
}

_release_txn_validate_authorization_tree() {
    local runtime_root=$1 authorization owner group mode links size entries path
    _release_txn_validate_root_directory "$runtime_root" 700 || return 1
    authorization=$runtime_root/start.authorization
    _release_txn_validate_root_directory "$authorization" 700 || return 1
    entries=$(find "$authorization" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort) \
        || { _release_txn_fail "cannot inspect start authorization entries"; return 1; }
    [[ "$entries" == $'holder\nmarker' ]] \
        || { _release_txn_fail "start authorization contains unexpected entries"; return 1; }
    for path in "$authorization/holder" "$authorization/marker"; do
        [[ -f "$path" && ! -L "$path" ]] \
            || { _release_txn_fail "unsafe start authorization file: $path"; return 1; }
        read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$path") \
            || { _release_txn_fail "cannot inspect start authorization file: $path"; return 1; }
        [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
           "$size" -gt 0 && "$size" -le 512 ]] \
            || { _release_txn_fail "start authorization file metadata mismatch: $path"; return 1; }
    done
}

_release_txn_cleanup_staging_authorization() {
    local staging=$1
    [[ -d "$staging" && ! -L "$staging" ]] || return 0
    rm -f -- "$staging/holder" "$staging/marker" >/dev/null 2>&1 || true
    rmdir -- "$staging" >/dev/null 2>&1 || true
}

_release_txn_remove_authorization_tree() {
    local runtime_root=$1 authorization
    _release_txn_validate_authorization_tree "$runtime_root" || return 1
    authorization=$runtime_root/start.authorization
    rm -f -- "$authorization/holder" "$authorization/marker" \
        || { _release_txn_fail "cannot remove start authorization files"; return 1; }
    rmdir -- "$authorization" \
        || { _release_txn_fail "cannot remove start authorization directory"; return 1; }
    sync -f -- "$runtime_root" \
        || { _release_txn_fail "cannot make start authorization removal durable"; return 1; }
    [[ ! -e "$authorization" && ! -L "$authorization" ]] \
        || { _release_txn_fail "start authorization directory still exists"; return 1; }
}

# A new lock owner may clear only a structurally exact stale runtime grant.
# Yeni lock sahibi yalnız yapısal olarak tam bir eski runtime iznini temizleyebilir.
release_txn_clear_stale_start_authorization() {
    local transaction_root=$1 runtime_root=$2 inherited_fd=$3 authorization
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    _release_txn_validate_safe_path "$runtime_root" || return 1
    authorization=$runtime_root/start.authorization
    if [[ ! -e "$runtime_root" && ! -L "$runtime_root" ]]; then
        return 0
    fi
    _release_txn_validate_root_directory "$runtime_root" 700 || return 1
    if [[ ! -e "$authorization" && ! -L "$authorization" ]]; then
        return 0
    fi
    _release_txn_remove_authorization_tree "$runtime_root"
}

# Publish a root-only authorization bound to the exact marker and the live
# process that owns the inherited persistent transaction lock descriptor.
# Tam işaretçiye ve kalıcı işlem lock descriptor'ını elinde tutan canlı sürece
# bağlı yalnız root'a açık yetkilendirmeyi yayımla.
release_txn_create_start_authorization() {
    local transaction_root=$1 runtime_root=$2 inherited_fd=$3 token=$4 operation=$5 snapshot=$6
    local marker authorization staging holder_pid holder_start_time
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    marker=$(_release_txn_current_marker \
        "$transaction_root" "$token" "$operation" "$snapshot") || return 1
    _release_txn_prepare_runtime_root "$runtime_root" || return 1
    authorization=$runtime_root/start.authorization
    [[ ! -e "$authorization" && ! -L "$authorization" ]] \
        || { _release_txn_fail "start authorization already exists"; return 1; }
    holder_pid=$BASHPID
    holder_start_time=$(_release_txn_holder_start_time "$holder_pid") || return 1
    staging=$(mktemp -d -p "$runtime_root" '.start.authorization.tmp.XXXXXXXXXX') \
        || { _release_txn_fail "cannot stage start authorization"; return 1; }
    if ! chown root:root -- "$staging" ||
       ! chmod 0700 -- "$staging" ||
       ! cp --no-preserve=mode,ownership,timestamps -- "$marker" "$staging/marker" ||
       ! _release_txn_print_holder \
           "$token" "$holder_pid" "$holder_start_time" "$inherited_fd" > "$staging/holder" ||
       ! chown root:root -- "$staging/marker" "$staging/holder" ||
       ! chmod 0600 -- "$staging/marker" "$staging/holder" ||
       ! sync -f -- "$staging/marker" ||
       ! sync -f -- "$staging/holder" ||
       ! sync -f -- "$staging" ||
       ! mv -T --no-clobber -- "$staging" "$authorization" ||
       ! sync -f -- "$runtime_root"; then
        _release_txn_cleanup_staging_authorization "$staging"
        _release_txn_fail "cannot durably publish start authorization"
        return 1
    fi
    _release_txn_validate_authorization_tree "$runtime_root" || return 1
    cmp -s -- "$marker" "$authorization/marker" \
        || { _release_txn_fail "published start authorization marker mismatch"; return 1; }
    cmp -s "$authorization/holder" <(_release_txn_print_holder \
        "$token" "$holder_pid" "$holder_start_time" "$inherited_fd") \
        || { _release_txn_fail "published start authorization holder mismatch"; return 1; }
}

# Remove authorization while the persistent marker still blocks ordinary
# starts; completion.pending may be removed only afterwards.
# Kalıcı işaretçi normal başlangıçları engellerken yetkilendirmeyi kaldır;
# completion.pending ancak bundan sonra silinebilir.
release_txn_remove_start_authorization() {
    local transaction_root=$1 runtime_root=$2 inherited_fd=$3 token=$4 operation=$5 snapshot=$6
    local marker authorization holder_pid holder_start_time
    release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1
    marker=$(_release_txn_current_marker \
        "$transaction_root" "$token" "$operation" "$snapshot") || return 1
    _release_txn_validate_authorization_tree "$runtime_root" || return 1
    authorization=$runtime_root/start.authorization
    cmp -s -- "$marker" "$authorization/marker" \
        || { _release_txn_fail "start authorization marker mismatch during removal"; return 1; }
    holder_pid=$BASHPID
    holder_start_time=$(_release_txn_holder_start_time "$holder_pid") || return 1
    cmp -s "$authorization/holder" <(_release_txn_print_holder \
        "$token" "$holder_pid" "$holder_start_time" "$inherited_fd") \
        || { _release_txn_fail "start authorization is not owned by this lock holder"; return 1; }
    _release_txn_remove_authorization_tree "$runtime_root"
}
