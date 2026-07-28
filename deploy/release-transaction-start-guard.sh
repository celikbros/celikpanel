#!/usr/bin/env bash
set -euo pipefail

# This root-trusted ExecCondition permits ordinary starts when no release
# marker exists. During a release it accepts only an exact, live-holder-bound
# authorization published under the root-only runtime directory.
# Bu root-trusted ExecCondition, release işaretçisi yokken normal başlangıçlara
# izin verir. Release sırasında yalnız root-only runtime dizininde yayımlanan,
# canlı holder'a bağlı tam yetkilendirmeyi kabul eder.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

deny() {
    printf 'celikpanel release start guard: %s\n' "$1" >&2
    exit 1
}

[[ $# -eq 2 ]] || deny "expected persistent and runtime transaction roots"
transaction_root=$1
runtime_root=$2
for guarded_path in "$transaction_root" "$runtime_root"; do
    [[ "$guarded_path" =~ ^/[A-Za-z0-9._/-]+$ &&
       "$guarded_path" != *'/../'* && "$guarded_path" != */.. ]] \
        || deny "unsafe transaction guard path"
done

validate_root_directory() {
    local path=$1 expected_mode=$2 owner group mode
    [[ -d "$path" && ! -L "$path" ]] || deny "unsafe directory: $path"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$path") \
        || deny "cannot inspect directory: $path"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == "$expected_mode" ]] \
        || deny "directory metadata mismatch: $path"
}

validate_root_file() {
    local path=$1 expected_mode=$2 maximum_size=$3 owner group mode links size
    [[ -f "$path" && ! -L "$path" ]] || deny "unsafe file: $path"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$path") \
        || deny "cannot inspect file: $path"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == "$expected_mode" &&
       "$links" == 1 && "$size" -gt 0 && "$size" -le "$maximum_size" ]] \
        || deny "file metadata mismatch: $path"
}

validate_root_directory "$transaction_root" 700
transaction_lock=$transaction_root/transaction.lock
[[ -f "$transaction_lock" && ! -L "$transaction_lock" ]] \
    || deny "unsafe transaction lock"
read -r lock_owner lock_group lock_mode lock_links lock_size \
    < <(stat -Lc '%u %g %a %h %s' -- "$transaction_lock") \
    || deny "cannot inspect transaction lock"
[[ "$lock_owner" == 0 && "$lock_group" == 0 && "$lock_mode" == 600 &&
   "$lock_links" == 1 && "$lock_size" == 0 ]] \
    || deny "transaction lock metadata mismatch"

quiesce_present=0
active_present=0
pending_present=0
[[ -e "$transaction_root/quiesce.pending" || -L "$transaction_root/quiesce.pending" ]] && quiesce_present=1
[[ -e "$transaction_root/active" || -L "$transaction_root/active" ]] && active_present=1
[[ -e "$transaction_root/completion.pending" || -L "$transaction_root/completion.pending" ]] && pending_present=1
[[ $((quiesce_present + active_present + pending_present)) -le 1 ]] \
    || deny "multiple release transaction markers exist"
if [[ $quiesce_present -eq 0 && $active_present -eq 0 && $pending_present -eq 0 ]]; then
    exit 0
fi
if [[ $quiesce_present -eq 1 ]]; then
    marker=$transaction_root/quiesce.pending
elif [[ $active_present -eq 1 ]]; then
    marker=$transaction_root/active
else
    marker=$transaction_root/completion.pending
fi
validate_root_file "$marker" 600 512

mapfile -t marker_lines < "$marker"
[[ ${#marker_lines[@]} -eq 4 && "${marker_lines[0]}" == version=1 ]] \
    || deny "release marker is not canonical"
token=${marker_lines[1]#token=}
operation=${marker_lines[2]#operation=}
snapshot=${marker_lines[3]#snapshot=}
[[ "${marker_lines[1]}" == "token=$token" && "$token" =~ ^[0-9a-f]{64}$ ]] \
    || deny "release marker token is invalid"
[[ "${marker_lines[2]}" == "operation=$operation" &&
   ( "$operation" == update || "$operation" == rollback ) ]] \
    || deny "release marker operation is invalid"
[[ "${marker_lines[3]}" == "snapshot=$snapshot" &&
   "$snapshot" != . && "$snapshot" != .. &&
   "$snapshot" =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] \
    || deny "release marker snapshot is invalid"
cmp -s "$marker" <(printf 'version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n' \
    "$token" "$operation" "$snapshot") \
    || deny "release marker bytes are not canonical"

# The pre-active phase never authorizes a start; both cgroups must first be
# frozen and atomically covered by the active marker.
# Active-öncesi aşama hiçbir başlangıca yetki vermez; önce iki cgroup da
# dondurulmalı ve atomik olarak active marker korumasına alınmalıdır.
[[ "$quiesce_present" -eq 0 ]] \
    || deny "quiesce phase blocks every service start"

validate_root_directory "$runtime_root" 700
authorization=$runtime_root/start.authorization
validate_root_directory "$authorization" 700
authorization_marker=$authorization/marker
authorization_holder=$authorization/holder
validate_root_file "$authorization_marker" 600 512
validate_root_file "$authorization_holder" 600 512
[[ "$(find "$authorization" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)" == $'holder\nmarker' ]] \
    || deny "start authorization contains unexpected entries"
cmp -s "$marker" "$authorization_marker" \
    || deny "start authorization does not exactly match the release marker"

mapfile -t holder_lines < "$authorization_holder"
[[ ${#holder_lines[@]} -eq 5 && "${holder_lines[0]}" == version=1 ]] \
    || deny "start authorization holder is not canonical"
holder_token=${holder_lines[1]#token=}
holder_pid=${holder_lines[2]#holder-pid=}
holder_start_time=${holder_lines[3]#holder-start-time=}
holder_fd=${holder_lines[4]#holder-fd=}
[[ "${holder_lines[1]}" == "token=$holder_token" && "$holder_token" == "$token" ]] \
    || deny "start authorization holder token mismatch"
[[ "${holder_lines[2]}" == "holder-pid=$holder_pid" &&
   "$holder_pid" =~ ^[0-9]+$ && "$holder_pid" -gt 1 ]] \
    || deny "start authorization holder pid is invalid"
[[ "${holder_lines[3]}" == "holder-start-time=$holder_start_time" &&
   "$holder_start_time" =~ ^[0-9]+$ ]] \
    || deny "start authorization holder start time is invalid"
[[ "${holder_lines[4]}" == "holder-fd=$holder_fd" &&
   "$holder_fd" =~ ^[0-9]+$ && "$holder_fd" -ge 3 ]] \
    || deny "start authorization holder fd is invalid"
cmp -s "$authorization_holder" <(printf \
    'version=1\ntoken=%s\nholder-pid=%s\nholder-start-time=%s\nholder-fd=%s\n' \
    "$holder_token" "$holder_pid" "$holder_start_time" "$holder_fd") \
    || deny "start authorization holder bytes are not canonical"

[[ -r "/proc/$holder_pid/stat" && -r "/proc/$holder_pid/status" &&
   -e "/proc/$holder_pid/fd/$holder_fd" ]] \
    || deny "authorized release transaction holder is no longer alive"
holder_uid=$(awk '/^Uid:/ {print $2; exit}' "/proc/$holder_pid/status") \
    || deny "cannot inspect authorized holder uid"
[[ "$holder_uid" == 0 ]] || deny "authorized release transaction holder is not root"
holder_stat=$(<"/proc/$holder_pid/stat")
holder_stat_tail=${holder_stat##*) }
read -r -a holder_stat_fields <<< "$holder_stat_tail"
[[ ${#holder_stat_fields[@]} -ge 20 &&
   "${holder_stat_fields[19]}" == "$holder_start_time" ]] \
    || deny "authorized release transaction holder identity changed"

lock_identity=$(stat -Lc '%d:%i' -- "$transaction_lock") \
    || deny "cannot identify persistent transaction lock"
holder_fd_identity=$(stat -Lc '%d:%i' -- "/proc/$holder_pid/fd/$holder_fd") \
    || deny "cannot identify authorized holder lock descriptor"
[[ "$holder_fd_identity" == "$lock_identity" ]] \
    || deny "authorized holder descriptor does not name transaction.lock"

# The holder fdinfo must itself expose one exclusive flock on this OFD. Merely
# observing the inode busy would let an unrelated process substitute as owner.
# Holder fdinfo bu OFD üzerinde tek bir exclusive flock göstermelidir. Yalnız
# inode'un busy olduğunu görmek ilgisiz bir sürecin sahip yerine geçmesine izin verir.
holder_fdinfo=/proc/$holder_pid/fdinfo/$holder_fd
[[ -r "$holder_fdinfo" ]] \
    || deny "cannot inspect authorized holder descriptor state"
holder_lock_count=$(awk \
    '$1 == "lock:" && $3 == "FLOCK" && $4 == "ADVISORY" && $5 == "WRITE" { count++ } END { print count + 0 }' \
    "$holder_fdinfo") \
    || deny "cannot inspect authorized holder flock ownership"
[[ "$holder_lock_count" == 1 ]] \
    || deny "authorized holder descriptor does not own exactly one exclusive flock"

exec {probe_fd}<>"$transaction_lock" \
    || deny "cannot open independent transaction lock probe"
if flock -n -E 75 "$probe_fd" 2>/dev/null; then
    flock -u "$probe_fd" >/dev/null 2>&1 || true
    exec {probe_fd}>&-
    deny "authorized release transaction holder does not hold the lock"
else
    probe_rc=$?
fi
exec {probe_fd}>&-
[[ "$probe_rc" -eq 75 ]] \
    || deny "independent transaction lock probe failed unexpectedly"
exit 0
