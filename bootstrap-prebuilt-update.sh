#!/bin/bash
# Publish a verified prebuilt release archive into the fixed root-only release
# store, then enter the existing transactional updater. No compiler, package
# manager, or mutable Git checkout is used on the target server.
set -euo pipefail

PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

RELEASES_ROOT=/var/backups/celikpanel/releases
TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
UPDATE_SNAPSHOT_ROOT=/var/backups/celikpanel/update-snapshots
PANEL_TLS_DIR=/var/lib/celikpanel/tls
INSTALLED_ROOT=/opt/celikpanel
BIN_DIR=$INSTALLED_ROOT/bin
WEB_DIR=$INSTALLED_ROOT/web
UNIT_DIR=/etc/systemd/system
SOURCE_ROOT=$(cd -- "$(dirname -- "$(readlink -f -- "$0")")" && pwd)

die() {
    printf '!! %s\n' "$*" >&2
    exit 1
}

[[ $EUID -eq 0 ]] || die "Run as root / root olarak çalıştırın"
[[ $# -eq 1 && $1 == --normal ]] \
    || die "usage: bootstrap-prebuilt-update.sh --normal"

for required_command in awk bash chmod chown cmp cut dirname env find flock getent grep id mv od \
    readlink rm sha256sum sort stat sync systemctl tr wc xargs; do
    command -v "$required_command" >/dev/null 2>&1 \
        || die "$required_command is required / $required_command gereklidir"
done

validate_root_trusted_dir_chain() {
    local path=$1 canonical current owner group mode permissions
    [[ "$path" == /* ]] || die "trusted directory path must be absolute: $path"
    canonical=$(readlink -e -- "$path") || die "trusted directory is unavailable: $path"
    [[ "$canonical" == "$path" ]] || die "trusted directory contains a symlink or alias: $path"
    current=$path
    while true; do
        [[ -d "$current" && ! -L "$current" ]] || die "unsafe trusted directory: $current"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
            || die "cannot inspect trusted directory: $current"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "trusted directory must be owned by root: $current"
        (( (permissions & 0022) == 0 )) \
            || die "trusted directory must not be group/other writable: $current"
        [[ "$current" == / ]] && break
        current=$(dirname -- "$current")
    done
}

validate_release_tree() {
    local root=$1 require_direct=${2:-0} canonical relative entry owner group mode links permissions
    local version commit tree expected actual
    canonical=$(readlink -e -- "$root") || die "release tree is unavailable: $root"
    [[ "$canonical" == "$root" ]] || die "release tree contains a symlink or alias: $root"
    validate_root_trusted_dir_chain "$root"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$root") \
        || die "cannot inspect release root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "release root must be root:root mode 0700"

    if [[ "$require_direct" == 1 ]]; then
        [[ "$root" == "$RELEASES_ROOT/"* ]] || die "release is outside release storage"
        relative=${root#"$RELEASES_ROOT/"}
        [[ "$relative" != */* && "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] \
            || die "published release name is invalid: $relative"
    fi

    if find "$root" -xdev -type l -print -quit | grep -q .; then
        die "release contains a symbolic link"
    fi
    if find "$root" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "release contains a special filesystem object"
    fi
    if find "$root" -xdev -type f -links +1 -print -quit | grep -q .; then
        die "release contains a hard-linked file"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$entry") \
            || die "cannot inspect release entry: $entry"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "release entry must be owned by root: $entry"
        (( (permissions & 0022) == 0 )) \
            || die "release entry must not be group/other writable: $entry"
        [[ ! -f "$entry" || "$links" == 1 ]] \
            || die "release file must have exactly one link: $entry"
    done < <(find "$root" -xdev -mindepth 1 -print0)

    [[ ! -e "$root/.git" && ! -L "$root/.git" ]] \
        || die "release must not contain a mutable Git checkout"
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] \
        || die "release checksum manifest is missing"
    (
        cd "$root"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "release checksum verification failed"

    for expected in release.version release.commit release.tree bin/panel bin/agent \
        bin/schema17-bridge install.sh update.sh rollback.sh \
        deploy/panel-tls-snapshot.sh deploy/release-transaction-guard.sh \
        web/dist/index.html; do
        [[ -f "$root/$expected" && ! -L "$root/$expected" ]] \
            || die "required release file is missing: $expected"
    done
    version=$(tr -d '[:space:]' < "$root/release.version")
    commit=$(tr -d '[:space:]' < "$root/release.commit")
    tree=$(tr -d '[:space:]' < "$root/release.tree")
    [[ "$version" == 1 ]] || die "unsupported release manifest version: $version"
    [[ "$commit" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid release commit"
    [[ "$tree" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid release tree"
    [[ -x "$root/bin/panel" && -x "$root/bin/agent" && -x "$root/bin/schema17-bridge" ]] \
        || die "release binaries are not executable"
    [[ -x "$root/install.sh" && -x "$root/update.sh" && -x "$root/rollback.sh" ]] \
        || die "release entry scripts are not executable"
    VALIDATED_RELEASE_COMMIT=$commit
}

sync_release_tree_durably() {
    local root=$1 entry
    while IFS= read -r -d '' entry; do
        sync -f -- "$entry" || die "cannot make release file durable: $entry"
    done < <(find "$root" -xdev -type f -print0)
    while IFS= read -r -d '' entry; do
        sync -f -- "$entry" || die "cannot make release directory durable: $entry"
    done < <(find "$root" -xdev -depth -type d -print0)
}

service_state_is_active_like() {
    case "$1" in
        active|activating|reloading|refreshing) return 0 ;;
        *) return 1 ;;
    esac
}

service_cgroup_pids() {
    local unit=$1 control_group cgroup_root procs_file pid
    control_group=$(systemctl show --property=ControlGroup --value -- "$unit") \
        || die "cannot inspect control group for $unit"
    [[ -n "$control_group" ]] || return 0
    [[ "$control_group" == /system.slice/* && "$control_group" != *'/../'* ]] \
        || die "unsafe control group for $unit: $control_group"
    cgroup_root=/sys/fs/cgroup$control_group
    [[ -d "$cgroup_root" && ! -L "$cgroup_root" ]] || return 0
    find "$cgroup_root" -type f -name cgroup.procs -print0 \
        | while IFS= read -r -d '' procs_file; do
            while IFS= read -r pid; do
                [[ -z "$pid" || "$pid" =~ ^[0-9]+$ ]] \
                    || die "invalid pid in $procs_file"
                [[ -z "$pid" ]] || printf '%s\n' "$pid"
            done < "$procs_file"
        done
}

verify_unit_recursively_stopped() {
    local unit=$1 load state job main_pid pid_output
    load=$(systemctl show --property=LoadState --value -- "$unit") \
        || die "cannot inspect load state for $unit"
    [[ "$load" == loaded ]] || die "$unit must remain loaded"
    job=$(systemctl show --property=Job --value -- "$unit") \
        || die "cannot inspect queued job for $unit"
    [[ -z "$job" ]] || die "$unit has a queued systemd job: $job"
    state=$(systemctl show --property=ActiveState --value -- "$unit") \
        || die "cannot inspect active state for $unit"
    case "$state" in inactive|failed) ;; *) die "$unit must be inactive or failed; found $state" ;; esac
    main_pid=$(systemctl show --property=MainPID --value -- "$unit") \
        || die "cannot inspect MainPID for $unit"
    [[ "$main_pid" == 0 ]] || die "$unit MainPID must be zero"
    pid_output=$(service_cgroup_pids "$unit") \
        || die "cannot recursively inspect cgroup processes for $unit"
    [[ -z "$pid_output" ]] \
        || die "$unit has residual recursive cgroup processes: ${pid_output//$'\n'/,}"
}

coordinator_process_start_time() {
    local pid=$1 process_stat process_tail
    local -a process_fields=()
    [[ "$pid" =~ ^[0-9]+$ && "$pid" -gt 1 && -r "/proc/$pid/stat" ]] || return 1
    process_stat=$(<"/proc/$pid/stat") || return 1
    process_tail=${process_stat##*) }
    read -r -a process_fields <<< "$process_tail"
    [[ ${#process_fields[@]} -ge 20 && "${process_fields[19]}" =~ ^[0-9]+$ ]] || return 1
    printf '%s\n' "${process_fields[19]}"
}

validate_exact_alpha2_installed_artifacts() {
    local path expected actual owner group mode links permissions manifest count
    local -a exact_files=(
        "$BIN_DIR/panel:8d5bd24c6fe5efc63fab610a97abf949b5398844b14f85f1d2d833d7e6f09c06:755"
        "$BIN_DIR/agent:85a2b0419468b41d86c8d42030724432f8fbb392af0180813547ff396d086350:755"
        "$UNIT_DIR/celikpanel-agent.service:cf284852141c32c2efc3a1c7f4e8c1ee42f918cd2bb09a0d4b1d54777a6c6b70:644"
        "$UNIT_DIR/celikpanel-panel.service:fc0f084e5fd4b87b2622f4f7de6643c26ebc4bc44a2b752bbfd84bef82ccc783:644"
        "$UNIT_DIR/celikpanel-firewall-restore.service:dd176e36365bd0c681543ef1ac5922b52d3ad2126bc5c48b94342f689a27d602:644"
    )
    for path in "${exact_files[@]}"; do
        IFS=: read -r path expected mode <<< "$path"
        [[ -f "$path" && ! -L "$path" ]] || die "reviewed alpha.2 artifact is missing: $path"
        read -r owner group actual links < <(stat -Lc '%u %g %a %h' -- "$path") \
            || die "cannot inspect reviewed alpha.2 artifact: $path"
        [[ "$owner" == 0 && "$group" == 0 && "$actual" == "$mode" && "$links" == 1 ]] \
            || die "reviewed alpha.2 artifact metadata changed: $path"
        actual=$(sha256sum -- "$path"); actual=${actual%% *}
        [[ "$actual" == "$expected" ]] || die "reviewed alpha.2 artifact bytes changed: $path"
    done

    [[ -d "$WEB_DIR" && ! -L "$WEB_DIR" && "$(readlink -e -- "$WEB_DIR")" == "$WEB_DIR" ]] \
        || die "reviewed alpha.2 web root is missing or unsafe"
    if find "$WEB_DIR" -xdev -type l -print -quit | grep -q .; then
        die "reviewed alpha.2 web tree contains a symbolic link"
    fi
    if find "$WEB_DIR" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "reviewed alpha.2 web tree contains a special object"
    fi
    count=$(find "$WEB_DIR" -xdev -type f | wc -l)
    [[ "$count" -eq 67 ]] || die "reviewed alpha.2 web file count changed"
    while IFS= read -r -d '' path; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") \
            || die "cannot inspect reviewed alpha.2 web object"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 ]] \
            || die "reviewed alpha.2 web objects must be root-owned"
        (( (permissions & 0022) == 0 )) \
            || die "reviewed alpha.2 web objects must not be group/other writable"
        [[ -d "$path" || "$links" == 1 ]] \
            || die "reviewed alpha.2 web files must have one link"
    done < <(find "$WEB_DIR" -xdev -mindepth 1 -print0)
    manifest=$(
        cd "$WEB_DIR"
        LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | sha256sum
    ) || die "cannot calculate the reviewed alpha.2 web manifest"
    manifest=${manifest%% *}
    [[ "$manifest" == cb4bb6d235323dab0dd5bea98958f5253f80d64a9c402ac449f3133005df4ea8 ]] \
        || die "reviewed alpha.2 web bytes changed"
}

validate_known_alpha4_tls_capture() {
    local root=$1 child=$2 panel_uid=$3 panel_gid=$4 listing rel owner group mode links size permissions
    local entries state path kind saved_uid saved_gid saved_mode saved_mtime saved_size saved_digest extra
    local actual_digest rows=0 unit load enabled active seen_certbot=0 seen_renew=0 record
    [[ -d "$root" && ! -L "$root" && "$(readlink -e -- "$root")" == "$root" ]] \
        || die "alpha.4 private TLS capture root is unsafe"
    [[ "$(stat -Lc '%u:%g:%a' -- "$root")" == 0:0:700 ]] \
        || die "alpha.4 private TLS capture root metadata is unsafe"
    [[ "$root" == "$child/".panel-tls.capture.* && "${root##*/}" =~ ^\.panel-tls\.capture\.[1-9][0-9]*$ ]] \
        || die "alpha.4 private TLS capture name is not canonical"
    _panel_tls_mount_guard "$root" || die "alpha.4 private TLS capture contains a mount"
    if find "$root" -xdev -type l -print -quit | grep -q .; then
        die "alpha.4 private TLS capture contains a symbolic link"
    fi
    if find "$root" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "alpha.4 private TLS capture contains a special object"
    fi
    if find "$root" -xdev -type f -links +1 -print -quit | grep -q .; then
        die "alpha.4 private TLS capture contains a hard-linked file"
    fi
    while IFS= read -r -d '' path; do
        rel=${path#"$root"/}
        case "$rel" in
            snapshot.version|layout.state|root.meta|managed.manifest|hook.state|pending.state|certbot-timers.tsv|certbot-services.tsv|managed|managed/panel.crt|managed/panel.key|hook|hook/celikpanel-panel-cert|pending|pending/panel-certificate-activation.json|timer-effective|timer-effective/certbot.timer.cat|timer-effective/certbot-renew.timer.cat) ;;
            *) die "unexpected alpha.4 private TLS capture entry: $rel" ;;
        esac
        read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$path") \
            || die "cannot inspect alpha.4 private TLS capture entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "alpha.4 private TLS capture is group/other writable: $rel"
        [[ -d "$path" || "$links" == 1 ]] \
            || die "alpha.4 private TLS capture file has multiple links: $rel"
        if [[ "$rel" == managed/panel.crt || "$rel" == managed/panel.key ]]; then
            [[ ( "$owner" == "$panel_uid" || "$owner" == 0 ) && "$group" == "$panel_gid" ]] \
                || die "alpha.4 captured legacy TLS ownership is unexpected: $rel"
        else
            [[ "$owner" == 0 && "$group" == 0 ]] \
                || die "alpha.4 private TLS capture entry is not root-owned: $rel"
        fi
        [[ ! -f "$path" || "$size" -le 8388608 ]] \
            || die "alpha.4 private TLS capture file is unexpectedly large: $rel"
    done < <(find "$root" -xdev -mindepth 1 -print0)

    entries=$(find "$root" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    # alpha.4 creates the version marker and these four empty directories
    # before it validates the live TLS tree. Fresh alpha.2 installations can
    # still have the narrowly supported panel-owned self-signed pair, so that
    # validation stops at this exact prefix. Accept only the deterministic
    # prefix: the live pair is independently normalized and revalidated later
    # while both coordinators remain stopped.
    if [[ "$entries" == $'hook\nmanaged\npending\nsnapshot.version\ntimer-effective' ]]; then
        [[ "$(stat -Lc '%u:%g:%a:%h' -- "$root/snapshot.version")" == 0:0:600:1 ]] \
            || die "alpha.4 partial TLS capture version marker is not canonical"
        cmp -s -- "$root/snapshot.version" <(printf '2\n') \
            || die "alpha.4 partial TLS capture version differs"
        for path in managed hook pending timer-effective; do
            [[ "$(stat -Lc '%u:%g:%a' -- "$root/$path")" == 0:0:700 ]] \
                || die "alpha.4 partial TLS directory metadata is unsafe: $path"
            [[ -z "$(find "$root/$path" -mindepth 1 -print -quit)" ]] \
                || die "alpha.4 partial TLS directory is not empty: $path"
        done
        return 0
    fi
    [[ "$entries" == $'certbot-services.tsv\ncertbot-timers.tsv\nhook\nhook.state\nlayout.state\nmanaged\nmanaged.manifest\npending\npending.state\nroot.meta\nsnapshot.version\ntimer-effective' ]] \
        || die "alpha.4 private TLS capture top-level allowlist differs"
    for path in managed hook pending timer-effective; do
        [[ "$(stat -Lc '%u:%g:%a' -- "$root/$path")" == 0:0:700 ]] \
            || die "alpha.4 private TLS directory metadata is unsafe: $path"
    done
    [[ "$(tr -d '[:space:]' < "$root/snapshot.version")" == 2 ]] \
        || die "alpha.4 private TLS capture version differs"
    [[ "$(tr -d '[:space:]' < "$root/layout.state")" == managed ]] \
        || die "alpha.4 private TLS capture is not the reviewed legacy layout"
    record=$(<"$root/root.meta")
    _panel_tls_validate_metadata_record "$record" \
        || die "alpha.4 private TLS root metadata is invalid"
    for path in snapshot.version layout.state root.meta managed.manifest hook.state pending.state certbot-timers.tsv certbot-services.tsv; do
        [[ "$(stat -Lc '%u:%g:%a:%h' -- "$root/$path")" == 0:0:600:1 ]] \
            || die "alpha.4 private TLS metadata file is not canonical: $path"
    done
    entries=$(find "$root/managed" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    [[ "$entries" == $'panel.crt\npanel.key' ]] \
        || die "alpha.4 captured managed TLS allowlist differs"
    [[ -z "$(find "$root/managed" -mindepth 2 -print -quit)" ]] \
        || die "alpha.4 captured managed TLS contains nested payload"
    for path in panel.crt panel.key; do
        [[ -s "$root/managed/$path" && -f "$PANEL_TLS_DIR/$path" && ! -L "$PANEL_TLS_DIR/$path" ]] \
            || die "alpha.4 captured or live legacy TLS file is missing: $path"
        cmp -s -- "$root/managed/$path" "$PANEL_TLS_DIR/$path" \
            || die "live legacy TLS bytes changed after alpha.4 capture: $path"
    done

    rows=0; seen_certbot=0; seen_renew=0
    while IFS=$'\t' read -r kind rel saved_uid saved_gid saved_mode saved_mtime saved_size saved_digest extra ||
          [[ -n "$kind$rel$saved_uid$saved_gid$saved_mode$saved_mtime$saved_size$saved_digest${extra:-}" ]]; do
        [[ "$kind" == F && ( "$rel" == panel.crt || "$rel" == panel.key ) &&
           "$saved_uid" =~ ^[0-9]+$ && "$saved_gid" =~ ^[0-9]+$ &&
           "$saved_mode" =~ ^[0-7]{3,4}$ && "$saved_mtime" =~ ^[0-9]+$ &&
           "$saved_size" =~ ^[0-9]+$ && "$saved_digest" =~ ^[0-9a-f]{64}$ && -z "${extra:-}" ]] \
            || die "alpha.4 managed TLS manifest is not canonical"
        if [[ "$rel" == panel.crt ]]; then
            [[ "$seen_certbot" -eq 0 ]] || die "duplicate alpha.4 panel.crt manifest row"
            seen_certbot=1
        else
            [[ "$seen_renew" -eq 0 ]] || die "duplicate alpha.4 panel.key manifest row"
            seen_renew=1
        fi
        path=$root/managed/$rel
        [[ "$(stat -Lc '%u:%g:%a:%Y:%s' -- "$path")" == "$saved_uid:$saved_gid:$saved_mode:$saved_mtime:$saved_size" ]] \
            || die "alpha.4 managed TLS manifest metadata differs: $rel"
        actual_digest=$(sha256sum -- "$path"); actual_digest=${actual_digest%% *}
        [[ "$actual_digest" == "$saved_digest" ]] \
            || die "alpha.4 managed TLS manifest digest differs: $rel"
        rows=$((rows + 1))
    done < "$root/managed.manifest"
    [[ "$rows" -eq 2 && "$seen_certbot" -eq 1 && "$seen_renew" -eq 1 ]] \
        || die "alpha.4 managed TLS manifest row set differs"

    state=$(tr -d '[:space:]' < "$root/hook.state")
    [[ "$state" == present || "$state" == absent ]] \
        || die "alpha.4 renewal-hook state is invalid"
    [[ "$state" == present && -f "$root/hook/celikpanel-panel-cert" ]] ||
        [[ "$state" == absent && -z "$(find "$root/hook" -mindepth 1 -print -quit)" ]] \
        || die "alpha.4 renewal-hook capture disagrees with its state"
    state=$(tr -d '[:space:]' < "$root/pending.state")
    [[ "$state" == present || "$state" == absent ]] \
        || die "alpha.4 pending-certificate state is invalid"
    [[ "$state" == present && -f "$root/pending/panel-certificate-activation.json" ]] ||
        [[ "$state" == absent && -z "$(find "$root/pending" -mindepth 1 -print -quit)" ]] \
        || die "alpha.4 pending-certificate capture disagrees with its state"

    rows=0; seen_certbot=0; seen_renew=0
    while IFS=$'\t' read -r unit load enabled active extra || [[ -n "$unit$load$enabled$active${extra:-}" ]]; do
        case "$unit" in certbot.timer|certbot-renew.timer) ;; *) die "unexpected alpha.4 timer row" ;; esac
        if [[ "$unit" == certbot.timer ]]; then
            [[ "$seen_certbot" -eq 0 ]] || die "duplicate alpha.4 certbot.timer row"
            seen_certbot=1
        else
            [[ "$seen_renew" -eq 0 ]] || die "duplicate alpha.4 certbot-renew.timer row"
            seen_renew=1
        fi
        [[ -z "${extra:-}" ]] || die "malformed alpha.4 timer row"
        _panel_tls_validate_unit_state_tuple timer "$unit" "$load" "$enabled" "$active" \
            || die "invalid alpha.4 timer state"
        if [[ "$load" == loaded ]]; then
            [[ -f "$root/timer-effective/$unit.cat" && ! -L "$root/timer-effective/$unit.cat" ]] \
                || die "loaded alpha.4 timer definition is missing"
        else
            [[ ! -e "$root/timer-effective/$unit.cat" && ! -L "$root/timer-effective/$unit.cat" ]] \
                || die "unloaded alpha.4 timer has a definition"
        fi
        rows=$((rows + 1))
    done < "$root/certbot-timers.tsv"
    [[ "$rows" -eq 2 && "$seen_certbot" -eq 1 && "$seen_renew" -eq 1 ]] \
        || die "alpha.4 timer snapshot row count differs"
    rows=0; seen_certbot=0; seen_renew=0
    while IFS=$'\t' read -r unit load enabled active extra || [[ -n "$unit$load$enabled$active${extra:-}" ]]; do
        case "$unit" in certbot.service|certbot-renew.service) ;; *) die "unexpected alpha.4 service row" ;; esac
        if [[ "$unit" == certbot.service ]]; then
            [[ "$seen_certbot" -eq 0 ]] || die "duplicate alpha.4 certbot.service row"
            seen_certbot=1
        else
            [[ "$seen_renew" -eq 0 ]] || die "duplicate alpha.4 certbot-renew.service row"
            seen_renew=1
        fi
        [[ -z "${extra:-}" ]] || die "malformed alpha.4 service row"
        _panel_tls_validate_unit_state_tuple service "$unit" "$load" "$enabled" "$active" \
            || die "invalid alpha.4 service state"
        rows=$((rows + 1))
    done < "$root/certbot-services.tsv"
    [[ "$rows" -eq 2 && "$seen_certbot" -eq 1 && "$seen_renew" -eq 1 ]] \
        || die "alpha.4 service snapshot row count differs"
}

validate_known_alpha4_agent_state_root() {
    local path=$1
    cmp -s "$path" <(printf '/var/lib/celikpanel-agent-private\n')
}

find_known_alpha4_tls_failure_stage() {
    local snapshot=$1 nonce=$2 panel_uid=$3 panel_gid=$4 candidate child entries path owner group mode links permissions size
    local capture_base
    local capture= count=0
    while IFS= read -r -d '' candidate; do
        count=$((count + 1))
        capture=$candidate
    done < <(find "$UPDATE_SNAPSHOT_ROOT" -mindepth 1 -maxdepth 1 \
        -name ".release-snapshot.incomplete.*.$nonce" -print0)
    [[ "$count" -eq 1 ]] || die "exactly one alpha.4 TLS-failure snapshot stage is required"
    candidate=$capture
    [[ "${candidate##*/}" =~ ^\.release-snapshot\.incomplete\.[1-9][0-9]*\.$nonce$ ]] \
        || die "alpha.4 TLS-failure snapshot stage name is not canonical"
    validate_root_trusted_dir_chain "$candidate"
    [[ "$(stat -Lc '%u:%g:%a' -- "$candidate")" == 0:0:700 ]] \
        || die "alpha.4 TLS-failure snapshot stage metadata is unsafe"
    entries=$(find "$candidate" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    [[ "$entries" == "$snapshot" ]] || die "alpha.4 TLS-failure stage contains another snapshot"
    child=$candidate/$snapshot
    [[ "$(stat -Lc '%u:%g:%a' -- "$child")" == 0:0:700 ]] \
        || die "alpha.4 TLS-failure snapshot child metadata is unsafe"
    if find "$candidate" -xdev -type l -print -quit | grep -q .; then
        die "alpha.4 TLS-failure stage contains a symbolic link"
    fi
    if find "$candidate" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die "alpha.4 TLS-failure stage contains a special object"
    fi
    if find "$candidate" -xdev -type f -links +1 -print -quit | grep -q .; then
        die "alpha.4 TLS-failure stage contains a hard-linked file"
    fi
    capture=$(find "$child" -mindepth 1 -maxdepth 1 -type d -name '.panel-tls.capture.*' -print -quit)
    [[ -n "$capture" ]] || die "alpha.4 private TLS capture is missing"
    capture_base=${capture##*/}
    entries=$(find "$child" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    [[ "$entries" == "$capture_base"$'\nagent-ledger.state\nagent-state\nagent-state-root\ncelikpanel.db\nquiesce-coordinators.tsv\nservice-states.tsv\nsnapshot-transition.state' ]] \
        || die "alpha.4 TLS-failure payload allowlist differs"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$child/celikpanel.db") \
        || die "cannot inspect alpha.4 database snapshot"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 && "$size" -gt 0 && "$size" -le 1073741824 ]] \
        || die "alpha.4 database snapshot metadata is unsafe"
    release_txn_validate_service_states "$child/service-states.tsv" \
        || die "alpha.4 service-state ledger is unsafe"
    _release_txn_validate_quiesce_coordinators "$child/quiesce-coordinators.tsv" \
        || die "alpha.4 coordinator ledger is unsafe"
    cmp -s "$child/snapshot-transition.state" <(printf 'normal\n') \
        || die "alpha.4 snapshot transition state differs"
    validate_known_alpha4_agent_state_root "$child/agent-state-root" \
        || die "alpha.4 agent-state root differs"
    case "$(tr -d '[:space:]' < "$child/agent-ledger.state")" in
        present)
            entries=$(find "$child/agent-state" -mindepth 1 -maxdepth 1 -printf '%f\n')
            [[ "$entries" == service-mutations.json ]] \
                || die "alpha.4 agent-state payload differs"
            ;;
        absent)
            [[ -z "$(find "$child/agent-state" -mindepth 1 -print -quit)" ]] \
                || die "alpha.4 absent agent ledger has payload"
            ;;
        *) die "alpha.4 agent-ledger state differs" ;;
    esac
    for path in "$child/celikpanel.db" "$child/agent-ledger.state" "$child/agent-state-root" \
        "$child/agent-state" "$child/service-states.tsv" "$child/quiesce-coordinators.tsv" \
        "$child/snapshot-transition.state"; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") \
            || die "cannot inspect alpha.4 TLS-failure payload"
        permissions=$((8#$mode))
        [[ "$owner" == 0 && "$group" == 0 && ( -d "$path" || "$links" == 1 ) ]] \
            || die "alpha.4 TLS-failure payload ownership is unsafe"
        (( (permissions & 0022) == 0 )) \
            || die "alpha.4 TLS-failure payload is group/other writable"
    done
    if [[ -f "$child/agent-state/service-mutations.json" ]]; then
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$child/agent-state/service-mutations.json")
        permissions=$((8#$mode))
        [[ ( "$owner" == 0 || "$owner" == "$panel_uid" ) &&
           ( "$group" == 0 || "$group" == "$panel_gid" ) && "$links" == 1 ]] \
            || die "alpha.4 captured agent ledger ownership is unsafe"
        (( (permissions & 0022) == 0 )) \
            || die "alpha.4 captured agent ledger is group/other writable"
    fi
    validate_known_alpha4_tls_capture "$capture" "$child" "$panel_uid" "$panel_gid"
    printf '%s\n' "$candidate"
}

verify_interrupted_coordinators_gone() {
    local coordinators=$1 unit state pid start extra current_start rows=0
    while IFS=$'\t' read -r unit state pid start extra || [[ -n "$unit$state$pid$start${extra:-}" ]]; do
        [[ -z "${extra:-}" ]] || die "malformed alpha.4 coordinator row"
        case "$unit" in celikpanel-agent.service|celikpanel-panel.service) ;; *) die "unexpected alpha.4 coordinator" ;; esac
        if current_start=$(coordinator_process_start_time "$pid" 2>/dev/null); then
            [[ "$current_start" != "$start" ]] \
                || die "captured alpha.4 coordinator process still exists: $unit"
        fi
        rows=$((rows + 1))
    done < "$coordinators"
    [[ "$rows" -eq 2 ]] || die "alpha.4 coordinator ledger row count differs"
    verify_unit_recursively_stopped celikpanel-agent.service
    verify_unit_recursively_stopped celikpanel-panel.service
}

remove_known_alpha4_pre_snapshot_payload() {
    local stage=$1 snapshot=$2
    local child=$stage/$snapshot capture entries
    local -a removable=()
    capture=$(find "$child" -mindepth 1 -maxdepth 1 -type d -name '.panel-tls.capture.*' -print -quit)
    [[ -n "$capture" ]] || die "verified alpha.4 private TLS capture disappeared"
    removable=(
        "$capture"
        "$child/agent-ledger.state"
        "$child/agent-state"
        "$child/agent-state-root"
        "$child/celikpanel.db"
    )
    rm -rf -- "${removable[@]}" || die "cannot remove verified alpha.4 pre-snapshot payload"
    sync -f -- "$child" "$stage" "$UPDATE_SNAPSHOT_ROOT" \
        || die "cannot make alpha.4 pre-snapshot cleanup durable"
    release_txn_validate_update_snapshot_stage "$UPDATE_SNAPSHOT_ROOT" "$snapshot" "$stage" \
        || die "reduced alpha.4 pre-mutation stage failed canonical validation"
    entries=$(find "$child" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
    [[ "$entries" == $'quiesce-coordinators.tsv\nservice-states.tsv\nsnapshot-transition.state' ]] \
        || die "reduced alpha.4 pre-mutation stage contains payload"
}

verify_active_marker_unchanged() {
    local expected_identity=$1 expected_digest=$2 marker=$TRANSACTION_ROOT/active
    local actual_identity actual_digest
    [[ -f "$marker" && ! -L "$marker" ]] \
        || die "the active update marker disappeared or became unsafe"
    actual_identity=$(stat -Lc '%d:%i:%s:%Y:%h:%u:%g:%a' -- "$marker") \
        || die "cannot inspect the active update marker identity"
    actual_digest=$(sha256sum -- "$marker"); actual_digest=${actual_digest%% *}
    [[ "$actual_identity" == "$expected_identity" && "$actual_digest" == "$expected_digest" ]] \
        || die "the active update marker changed during recovery"
}

abort_known_older_pre_mutation_update() {
    local active_token active_operation active_snapshot active_created active_target active_nonce
    local stage repeated_stage ledger coordinator_ledger unit enabled_state active_state extra index actual_enabled
    local transaction_fd panel_uid panel_gid active_marker_identity active_marker_digest
    local known_release known_release_found=0
    local TRUSTED_RELEASE_ROOT=$FINAL_RELEASE
    local -a saved_units=() saved_enabled=() saved_active=()
    local -a known_releases=()
    local known_failed_target=8bbbac8b628fae4fca0e127e52c1c7835f56f8b8

    [[ -e "$TRANSACTION_ROOT/active" || -L "$TRANSACTION_ROOT/active" ]] || return 0
    validate_root_trusted_dir_chain "$TRANSACTION_ROOT"

    # Use only parsers and recovery primitives from the newly verified,
    # immutable release. This bridge is intentionally limited to the one
    # published release whose TLS snapshot bug stopped before snapshot
    # publication and before the installed-byte mutation boundary.
    # shellcheck source=deploy/release-transaction-guard.sh
    source "$FINAL_RELEASE/deploy/release-transaction-guard.sh"
    IFS=$'\t' read -r active_token active_operation active_snapshot \
        < <(release_txn_read_active_fields "$TRANSACTION_ROOT") \
        || die "cannot read the active update marker"
    [[ "$active_operation" == update ]] \
        || die "the active transaction is not an update; explicit recovery is required"
    IFS=$'\t' read -r active_created active_target active_nonce \
        < <(release_txn_parse_update_snapshot_name "$active_snapshot") \
        || die "the active update snapshot name is not canonical"
    [[ "$active_target" != "$NEW_RELEASE_COMMIT" ]] || return 0
    [[ "$active_target" == "$known_failed_target" ]] \
        || die "an older active update is not eligible for automatic pre-mutation recovery"

    # The marker must name a retained, fully verified immutable alpha.4
    # release. A copied marker or an unverified historical target is not
    # sufficient authority for this narrowly scoped compatibility bridge.
    mapfile -d '' -t known_releases < <(
        find "$RELEASES_ROOT" -xdev -mindepth 1 -maxdepth 1 -type d \
            -name "${known_failed_target:0:12}-*" -print0
    )
    for known_release in "${known_releases[@]}"; do
        validate_release_tree "$known_release" 1
        [[ "$VALIDATED_RELEASE_COMMIT" == "$known_failed_target" ]] \
            || die "the retained alpha.4 release identity is inconsistent"
        known_release_found=1
    done
    [[ "$known_release_found" -eq 1 ]] \
        || die "the immutable alpha.4 release needed for recovery is unavailable"
    VALIDATED_RELEASE_COMMIT=$NEW_RELEASE_COMMIT

    release_txn_validate_root "$TRANSACTION_ROOT" \
        || die "cannot validate the release transaction root"
    release_txn_validate_lock_file "$TRANSACTION_ROOT" \
        || die "cannot validate the release transaction lock"
    exec {transaction_fd}<>"$TRANSACTION_ROOT/transaction.lock" \
        || die "cannot open the release transaction lock"
    flock -n -x "$transaction_fd" \
        || die "another release transaction is active"
    release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$transaction_fd" \
        || die "cannot prove exclusive ownership of the release transaction lock"
    release_txn_validate_active_token \
        "$TRANSACTION_ROOT" "$active_token" update "$active_snapshot" \
        || die "the older active update marker changed while acquiring its lock"
    active_marker_identity=$(stat -Lc '%d:%i:%s:%Y:%h:%u:%g:%a' -- "$TRANSACTION_ROOT/active") \
        || die "cannot capture the active update marker identity"
    active_marker_digest=$(sha256sum -- "$TRANSACTION_ROOT/active")
    active_marker_digest=${active_marker_digest%% *}

    # The failed alpha.4 TLS capture was rejected by alpha.4 itself before it
    # became a canonical snapshot. Parse that one reviewed private layout with
    # the immutable alpha.5 helper, then reduce it to the ordinary stage only
    # after the installed alpha.2 bytes and stopped coordinators are proven.
    # shellcheck source=deploy/panel-tls-snapshot.sh
    source "$FINAL_RELEASE/deploy/panel-tls-snapshot.sh"
    panel_uid=$(id -u celikpanel) || die "celikpanel user is unavailable"
    panel_gid=$(getent group celikpanel | cut -d: -f3)
    [[ "$panel_gid" =~ ^[0-9]+$ ]] || die "celikpanel group is unavailable"
    stage=$(find_known_alpha4_tls_failure_stage \
        "$active_snapshot" "$active_nonce" "$panel_uid" "$panel_gid") \
        || die "the older update is not the reviewed alpha.4 TLS-failure stage"
    ledger=$stage/$active_snapshot/service-states.tsv
    coordinator_ledger=$stage/$active_snapshot/quiesce-coordinators.tsv

    index=0
    while IFS=$'\t' read -r unit enabled_state active_state extra ||
          [[ -n "$unit$enabled_state$active_state${extra:-}" ]]; do
        (( index < 3 )) || break
        saved_units[index]=$unit
        saved_enabled[index]=$enabled_state
        saved_active[index]=$active_state
        index=$((index + 1))
    done < "$ledger"
    [[ "$index" -eq 3 ]] || die "the older update service-state ledger is incomplete"
    for index in 0 1 2; do
        actual_enabled=$(systemctl is-enabled "${saved_units[index]}" 2>/dev/null || true)
        [[ "${actual_enabled:-unknown}" == "${saved_enabled[index]}" ]] \
            || die "service enablement changed during the interrupted update: ${saved_units[index]}"
    done
    if ! service_state_is_active_like "${saved_active[2]}" &&
       systemctl is-active --quiet "${saved_units[2]}"; then
        die "firewall restore service changed during the interrupted update"
    fi
    if service_state_is_active_like "${saved_active[2]}" &&
       ! systemctl is-active --quiet "${saved_units[2]}"; then
        die "firewall restore service stopped during the interrupted update"
    fi

    verify_interrupted_coordinators_gone "$coordinator_ledger"
    validate_exact_alpha2_installed_artifacts
    verify_active_marker_unchanged "$active_marker_identity" "$active_marker_digest"
    release_txn_validate_active_token \
        "$TRANSACTION_ROOT" "$active_token" update "$active_snapshot" \
        || die "the alpha.4 active marker changed before TLS normalization"

    # Normalize only the exact legacy self-signed layout while both
    # coordinators are still stopped. The operation is byte-preserving and
    # retry-safe; the active marker remains if it fails.
    panel_tls_normalize_legacy_self_signed "$PANEL_TLS_DIR" "$panel_uid" "$panel_gid" \
        || die "legacy panel TLS ownership could not be normalized safely"

    verify_interrupted_coordinators_gone "$coordinator_ledger"
    validate_exact_alpha2_installed_artifacts
    verify_active_marker_unchanged "$active_marker_identity" "$active_marker_digest"
    repeated_stage=$(find_known_alpha4_tls_failure_stage \
        "$active_snapshot" "$active_nonce" "$panel_uid" "$panel_gid") \
        || die "the alpha.4 recovery evidence changed after TLS normalization"
    [[ "$repeated_stage" == "$stage" ]] \
        || die "the alpha.4 recovery stage identity changed"

    remove_known_alpha4_pre_snapshot_payload "$stage" "$active_snapshot"
    verify_interrupted_coordinators_gone "$coordinator_ledger"
    validate_exact_alpha2_installed_artifacts
    verify_active_marker_unchanged "$active_marker_identity" "$active_marker_digest"
    release_txn_validate_active_token \
        "$TRANSACTION_ROOT" "$active_token" update "$active_snapshot" \
        || die "the alpha.4 active marker changed before durable closure"

    # Remove the exact pre-mutation marker before either network-facing
    # coordinator can start. The stage remains as recovery evidence until the
    # saved runtime state has been restored.
    release_txn_remove_pre_mutation_active_marker \
        "$TRANSACTION_ROOT" "$transaction_fd" "$active_token" update \
        "$active_snapshot" "$UPDATE_SNAPSHOT_ROOT" "$stage" \
        || die "cannot durably close the known pre-mutation update"

    if service_state_is_active_like "${saved_active[0]}"; then
        if ! systemctl is-active --quiet "${saved_units[0]}"; then
            systemctl start "${saved_units[0]}" \
                || die "cannot restore the agent after the interrupted update"
        fi
    elif systemctl is-active --quiet "${saved_units[0]}"; then
        die "the agent became active during the interrupted update"
    fi
    if service_state_is_active_like "${saved_active[1]}"; then
        if ! systemctl is-active --quiet "${saved_units[1]}"; then
            systemctl start "${saved_units[1]}" \
                || die "cannot restore the panel after the interrupted update"
        fi
    elif systemctl is-active --quiet "${saved_units[1]}"; then
        die "the panel became active during the interrupted update"
    fi
    for index in 0 1; do
        if service_state_is_active_like "${saved_active[index]}"; then
            systemctl is-active --quiet "${saved_units[index]}" \
                || die "service restoration did not reach the saved active state: ${saved_units[index]}"
        elif systemctl is-active --quiet "${saved_units[index]}"; then
            die "service restoration did not preserve the saved inactive state: ${saved_units[index]}"
        fi
    done

    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$TRANSACTION_ROOT" "$transaction_fd" "$UPDATE_SNAPSHOT_ROOT" \
        || die "cannot remove the closed pre-mutation snapshot stage"
    flock -u "$transaction_fd" || die "cannot release the transaction lock"
    exec {transaction_fd}>&-

    printf '%s\n' \
        "==> Closed the verified pre-mutation alpha.4 update and restored its saved services" \
        "==> Dogrulanmis mutation-oncesi alpha.4 guncellemesi kapatildi; kayitli servisler geri getirildi"
}

validate_root_trusted_dir_chain "$RELEASES_ROOT"
case "$SOURCE_ROOT" in
    "$RELEASES_ROOT"/.download.*/*/celikpanel-v*) ;;
    *) die "prebuilt source is outside the fixed download staging boundary: $SOURCE_ROOT" ;;
esac
validate_release_tree "$SOURCE_ROOT" 0

# Archive extraction deliberately does not preserve publisher-side ownership.
# Normalize only the already validated, root-only, symlink-free staging tree.
chown -R root:root -- "$SOURCE_ROOT"
find "$SOURCE_ROOT" -xdev -type d -exec chmod 0755 -- {} +
find "$SOURCE_ROOT" -xdev -type f -exec chmod 0644 -- {} +
chmod 0700 -- "$SOURCE_ROOT"
chmod 0755 -- "$SOURCE_ROOT/bin/panel" "$SOURCE_ROOT/bin/agent" \
    "$SOURCE_ROOT/bin/schema17-bridge" "$SOURCE_ROOT/install.sh" \
    "$SOURCE_ROOT/update.sh" "$SOURCE_ROOT/rollback.sh" \
    "$SOURCE_ROOT/bootstrap-prebuilt-update.sh"

validate_release_tree "$SOURCE_ROOT" 0
sync_release_tree_durably "$SOURCE_ROOT"

nonce=$(od -An -N12 -tx1 /dev/urandom | tr -d ' \n')
[[ "$nonce" =~ ^[0-9a-f]{24}$ ]] || die "could not create a safe release nonce"
release_name=${VALIDATED_RELEASE_COMMIT:0:12}-$nonce
FINAL_RELEASE=$RELEASES_ROOT/$release_name
[[ ! -e "$FINAL_RELEASE" && ! -L "$FINAL_RELEASE" ]] \
    || die "release destination already exists"
mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"
sync -f -- "$RELEASES_ROOT" || die "cannot make release publication durable"
validate_release_tree "$FINAL_RELEASE" 1

NEW_RELEASE_COMMIT=$VALIDATED_RELEASE_COMMIT
abort_known_older_pre_mutation_update

printf '==> Verified prebuilt release / Doğrulanmış hazır sürüm: %s\n' "$FINAL_RELEASE"
env -i PATH="$PATH" HOME=/root LC_ALL=C \
    CELIKPANEL_TRUSTED_RELEASE_ROOT="$FINAL_RELEASE" \
    CELIKPANEL_PREFLIGHT_PANEL="$FINAL_RELEASE/bin/panel" \
    CELIKPANEL_PREFLIGHT_AGENT="$FINAL_RELEASE/bin/agent" \
    bash "$FINAL_RELEASE/update.sh" --normal
