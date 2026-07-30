#!/bin/bash
# CelikPanel update: capture and verify a complete rollback snapshot before the
# source tree or installed artifacts are changed.
#
# CelikPanel güncellemesi: kaynak ağacı veya kurulu ürünler değiştirilmeden önce
# eksiksiz bir geri alma snapshot'ı alır ve doğrular.
set -euo pipefail

# Ignore caller-controlled command lookup before every privileged read or write.
# Her ayrıcalıklı okuma veya yazmadan önce çağıranın denetlediği komut arama yolunu yok say.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

SNAPSHOT_VERSION=4
SNAP_ROOT=/var/backups/celikpanel/update-snapshots
RECOVERY_SNAPSHOT_ROOT=/var/backups/celikpanel/recovery-snapshots
RELEASES_ROOT=/var/backups/celikpanel/releases
PANEL_DB=/var/lib/celikpanel/celikpanel.db
BIN_DIR=/opt/celikpanel/bin
WEB_DIR=/opt/celikpanel/web
UNIT_DIR=/etc/systemd/system
AGENT_STATE_DIR=/var/lib/celikpanel-agent-private
AGENT_LEDGER="$AGENT_STATE_DIR/service-mutations.json"
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
MUTATION_LOCK_FD=
MUTATION_LOCK_IDENTITY=
RELEASE_TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
RELEASE_TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
RELEASE_TRANSACTION_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
PREFLIGHT_PANEL="${CELIKPANEL_PREFLIGHT_PANEL:-$BIN_DIR/panel}"
PREFLIGHT_AGENT="${CELIKPANEL_PREFLIGHT_AGENT:-$BIN_DIR/agent}"
SCHEMA17_BRIDGE="${CELIKPANEL_SCHEMA17_BRIDGE:-}"
KEEP_SNAPSHOTS=5

BOOTSTRAP_PRE_LEDGER=0
BOOTSTRAP_SCHEMA17=0
TRUSTED_RELEASE_ROOT="${CELIKPANEL_TRUSTED_RELEASE_ROOT:-}"
panel_frozen=0
agent_frozen=0
mutation_started=0
transaction_started=0
quiesce_abort_failed=0
transaction_completion_verified=0
release_transaction_token=
verified_snapshot=
recovery_snapshot_dir=
rescue_snapshot=
die() {
    echo "!! $*" >&2
    exit 1
}

# Take the fixed persistent release lock before parsing mode, reading state, or
# trusting release-controlled code. The descriptor remains open for this process.
# Modu ayrıştırmadan, state okumadan veya sürüm denetimli koda güvenmeden önce
# sabit kalıcı sürüm kilidini al. Descriptor bu süreç boyunca açık kalır.
prepare_and_acquire_release_transaction_lock() {
    local root=$RELEASE_TRANSACTION_ROOT parent lock owner group mode links size
    local path_identity fd_identity
    parent=$(dirname -- "$root")
    [[ "$parent" == /var/lib && -d "$parent" && ! -L "$parent" ]] || die "unsafe release transaction parent: $parent"
    [[ "$(readlink -e -- "$parent")" == "$parent" ]] || die "release transaction parent is not canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$parent") || die "cannot inspect release transaction parent"
    [[ "$owner" == 0 && "$group" == 0 ]] || die "release transaction parent must be root-owned"
    (( (8#$mode & 0022) == 0 )) || die "release transaction parent must not be group/other writable"
    if [[ -e "$root" || -L "$root" ]]; then
        [[ -d "$root" && ! -L "$root" ]] || die "unsafe release transaction root"
    else
        install -d -m 0700 -o root -g root -- "$root" || die "cannot create release transaction root"
        sync -f -- "$parent" || die "cannot make release transaction root durable"
    fi
    [[ "$(readlink -e -- "$root")" == "$root" ]] || die "release transaction root is not canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$root") || die "cannot inspect release transaction root"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] || die "release transaction root must be root:root mode 0700"
    lock=$root/transaction.lock
    if [[ -e "$lock" || -L "$lock" ]]; then
        [[ -f "$lock" && ! -L "$lock" ]] || die "unsafe release transaction lock"
    else
        (umask 077; set -o noclobber; : > "$lock") || die "cannot exclusively create release transaction lock"
        chown root:root -- "$lock" || die "cannot own release transaction lock"
        chmod 0600 -- "$lock" || die "cannot protect release transaction lock"
        sync -f -- "$lock" || die "cannot make release transaction lock durable"
        sync -f -- "$root" || die "cannot make release transaction lock entry durable"
    fi
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$lock") || die "cannot inspect release transaction lock"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 && "$size" == 0 ]] || die "release transaction lock must be empty root:root mode 0600 with one link"
    path_identity=$(stat -Lc '%d:%i' -- "$lock") || die "cannot identify release transaction lock"
    exec {RELEASE_TRANSACTION_FD}<>"$lock"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$RELEASE_TRANSACTION_FD") || die "cannot identify inherited release transaction lock"
    [[ "$fd_identity" == "$path_identity" ]] || die "release transaction lock changed while opening"
    flock -n "$RELEASE_TRANSACTION_FD" || die "another update or rollback transaction is active"
}

[[ $EUID -eq 0 ]] || die "Run as root / root olarak çalıştırın: use bootstrap-update.sh"
prepare_and_acquire_release_transaction_lock

[[ $# -eq 1 ]] || die "usage: update.sh --normal|--bootstrap-pre-ledger|--bootstrap-schema17"
case "$1" in
    --bootstrap-pre-ledger)
        BOOTSTRAP_PRE_LEDGER=1
        ;;
    --bootstrap-schema17)
        BOOTSTRAP_PRE_LEDGER=1
        BOOTSTRAP_SCHEMA17=1
        ;;
    --normal) ;;
    *) die "unknown update mode: $1" ;;
esac
[[ -n "$TRUSTED_RELEASE_ROOT" ]] \
    || die "$1 requires CELIKPANEL_TRUSTED_RELEASE_ROOT"
# Every component of a privileged path must stay inside root-owned,
# non-writable directories. Resolving a final file is not enough because an
# attacker could replace a writable parent between validation and use.
# Ayrıcalıklı bir yolun her bileşeni root sahipli ve başkalarınca yazılamayan
# dizinlerde kalmalıdır. Yalnız son dosyayı çözmek yeterli değildir; saldırgan
# doğrulama ile kullanım arasında yazılabilir bir üst dizini değiştirebilir.
validate_root_trusted_dir_chain() {
    local path=$1 canonical current owner mode permissions
    [[ "$path" == /* ]] || die "trusted directory path must be absolute: $path"
    canonical=$(readlink -e -- "$path") || die "trusted directory is unavailable: $path"
    [[ "$canonical" == "$path" ]] || die "trusted directory path contains a symlink or alias: $path"
    current=$path
    while true; do
        [[ -d "$current" && ! -L "$current" ]] || die "unsafe trusted directory: $current"
        read -r owner mode < <(stat -Lc '%u %a' -- "$current") || die "cannot inspect trusted directory: $current"
        [[ "$owner" == 0 ]] || die "trusted directory must be owned by root: $current"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) || die "trusted directory must not be group/other writable: $current"
        [[ "$current" == / ]] && break
        current=$(dirname "$current")
    done
}

# Snapshot storage is deliberately outside the panel-writable StateDirectory.
# Create and validate each missing root-only path component without following
# an existing symlink.
# Snapshot deposu bilerek panelin yazabildiği StateDirectory dışında tutulur.
# Eksik root-only yol bileşenlerini var olan sembolik bağlantıyı izlemeden ayrı
# ayrı oluştur ve doğrula.
prepare_snapshot_root() {
    local directory desired_mode owner group mode
    validate_root_trusted_dir_chain /var
    for directory in /var/backups /var/backups/celikpanel "$SNAP_ROOT" "$RECOVERY_SNAPSHOT_ROOT"; do
        desired_mode=0700
        [[ "$directory" != /var/backups ]] || desired_mode=0755
        if [[ -e "$directory" || -L "$directory" ]]; then
            [[ -d "$directory" && ! -L "$directory" ]] || die "unsafe snapshot directory: $directory"
        else
            install -d -m "$desired_mode" -o root -g root -- "$directory" || die "cannot create snapshot directory: $directory"
            sync -f -- "$(dirname -- "$directory")" || die "cannot make snapshot directory durable: $directory"
        fi
        validate_root_trusted_dir_chain "$directory"
        if [[ "$directory" == "$RECOVERY_SNAPSHOT_ROOT" ]]; then
            read -r owner group mode < <(stat -Lc '%u %g %a' -- "$directory") \
                || die "cannot inspect recovery snapshot root"
            [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
                || die "recovery snapshot root must be root:root mode 0700"
        fi
    done
}

prepare_recovery_snapshot_directory() {
    local canonical owner group mode
    [[ "$snapshot_name" != */* && -n "$snapshot_name" ]] \
        || die "unsafe recovery snapshot name"
    recovery_snapshot_dir=$RECOVERY_SNAPSHOT_ROOT/$snapshot_name
    if [[ -e "$recovery_snapshot_dir" || -L "$recovery_snapshot_dir" ]]; then
        [[ -d "$recovery_snapshot_dir" && ! -L "$recovery_snapshot_dir" ]] \
            || die "unsafe exact recovery snapshot directory"
    else
        install -d -m 0700 -o root -g root -- "$recovery_snapshot_dir" \
            || die "cannot create exact recovery snapshot directory"
        sync -f -- "$RECOVERY_SNAPSHOT_ROOT" \
            || die "cannot make exact recovery snapshot directory durable"
    fi
    canonical=$(readlink -e -- "$recovery_snapshot_dir") \
        || die "cannot resolve exact recovery snapshot directory"
    [[ "$canonical" == "$recovery_snapshot_dir" ]] \
        || die "exact recovery snapshot directory contains a symlink or alias"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$recovery_snapshot_dir") \
        || die "cannot inspect exact recovery snapshot directory"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "exact recovery snapshot directory must be root:root mode 0700"
    rescue_snapshot=$recovery_snapshot_dir/$(basename -- "$PANEL_DB")
}

verify_recovery_snapshot() {
    local path=$1 owner group mode links suffix
    [[ -f "$path" && ! -L "$path" ]] \
        || die "durable recovery snapshot is missing or unsafe: $path"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") \
        || die "cannot inspect durable recovery snapshot"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 ]] \
        || die "durable recovery snapshot must be root:root mode 0600 with one link"
    for suffix in -wal -shm -journal; do
        [[ ! -e "$path$suffix" && ! -L "$path$suffix" ]] \
            || die "durable recovery snapshot has a forbidden SQLite sidecar: $path$suffix"
    done
}

retire_recovery_snapshot_after_verified_publish() {
    local expected_final canonical owner group mode
    expected_final=$SNAP_ROOT/$snapshot_name
    [[ "$verified_snapshot" == "$expected_final" && "$snap" == "$expected_final" ]] \
        || die "refusing recovery retirement before the exact final snapshot is verified"
    validate_root_trusted_dir_chain "$verified_snapshot"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$verified_snapshot") \
        || die "cannot inspect the verified final snapshot"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "verified final snapshot must be root:root mode 0700 before recovery retirement"
    [[ -f "$verified_snapshot/SHA256SUMS" && ! -L "$verified_snapshot/SHA256SUMS" ]] \
        || die "verified final snapshot manifest is missing before recovery retirement"
    (
        cd "$verified_snapshot"
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "final snapshot checksum verification failed before recovery retirement"
    [[ -f "$verified_snapshot/$(basename -- "$PANEL_DB")" &&
       ! -L "$verified_snapshot/$(basename -- "$PANEL_DB")" ]] \
        || die "final snapshot database is missing before recovery retirement"

    [[ "$recovery_snapshot_dir" == "$RECOVERY_SNAPSHOT_ROOT/$snapshot_name" ]] \
        || die "refusing recovery retirement outside the exact snapshot directory"
    canonical=$(readlink -e -- "$recovery_snapshot_dir") \
        || die "cannot resolve recovery directory before retirement"
    [[ "$canonical" == "$recovery_snapshot_dir" ]] \
        || die "recovery directory changed before retirement"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$recovery_snapshot_dir") \
        || die "cannot inspect recovery directory before retirement"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] \
        || die "recovery directory must remain root:root mode 0700 before retirement"
    [[ "$rescue_snapshot" == "$recovery_snapshot_dir/$(basename -- "$PANEL_DB")" ]] \
        || die "refusing recovery retirement of an inexact database path"
    verify_recovery_snapshot "$rescue_snapshot"

    rm -f -- "$rescue_snapshot" \
        || die "cannot retire exact recovery snapshot after final verification"
    [[ ! -e "$rescue_snapshot" && ! -L "$rescue_snapshot" ]] \
        || die "exact recovery snapshot remained after retirement"
    sync -f -- "$recovery_snapshot_dir" \
        || die "cannot make exact recovery snapshot retirement durable"
    rmdir -- "$recovery_snapshot_dir" \
        || die "exact recovery directory is not empty after snapshot retirement"
    sync -f -- "$RECOVERY_SNAPSHOT_ROOT" \
        || die "cannot make recovery directory retirement durable"
    rescue_snapshot=
    recovery_snapshot_dir=
}

# Release preflights execute privileged code, so only an absolute, root-owned,
# non-writable regular binary is accepted. A first upgrade may point these at
# verified staging binaries through CELIKPANEL_PREFLIGHT_PANEL/AGENT.
# Sürüm ön kontrolleri ayrıcalıklı kod çalıştırır; bu yüzden yalnız mutlak yoldaki,
# root sahipli, yazılamayan normal binary kabul edilir. İlk yükseltme, doğrulanmış
# staging binary'lerini CELIKPANEL_PREFLIGHT_PANEL/AGENT ile gösterebilir.
validate_preflight_binary() {
    local path=$1 label=$2 owner mode permissions
    [[ "$path" == /* ]] || die "$label preflight binary path must be absolute: $path"
    validate_root_trusted_dir_chain "$(dirname "$path")"
    [[ -f "$path" && -x "$path" && ! -L "$path" ]] || die "$label preflight binary is unsafe or missing: $path"
    read -r owner mode < <(stat -Lc '%u %a' -- "$path") || die "cannot inspect $label preflight binary"
    [[ "$owner" == 0 ]] || die "$label preflight binary must be owned by root: $path"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) || die "$label preflight binary must not be group/other writable: $path"
}

# A trusted release update is deliberately offline: it may copy already-built
# artifacts, but it must not discover a missing host tool by mutating packages.
# Prove every command used by the staged installer and the fixed service
# identity before the panel is stopped or a snapshot directory is created.
# Güvenilir sürüm güncellemesi bilerek çevrimdışıdır: önceden derlenmiş ürünleri
# kopyalayabilir, fakat eksik host aracını paket değiştirerek tamamlayamaz.
# Panel durdurulmadan veya snapshot dizini oluşturulmadan önce staged kurucunun
# kullandığı her komutu ve sabit servis kimliğini kanıtla.
preflight_staged_installer_runtime() {
    local required_command
    local -a required_commands=(
        awk basename bash chmod chown cmp cp cut dirname find flock getent
        grep hostname id install ln mkdir mv od readlink rm rmdir sed seq
        env sha256sum sleep sort stat sudo sync systemctl tr uname xargs
    )
    for required_command in "${required_commands[@]}"; do
        command -v "$required_command" >/dev/null 2>&1 \
            || die "required update tool is missing: $required_command; install it explicitly before retrying"
    done
    if ! command -v apt-get >/dev/null 2>&1 &&
       ! command -v pacman >/dev/null 2>&1; then
        die "supported package manager metadata is unavailable (apt-get or pacman); update will not install one"
    fi
    getent group celikpanel >/dev/null \
        || die "celikpanel group is missing; repair the service identity explicitly before retrying"
    id celikpanel >/dev/null 2>&1 \
        || die "celikpanel user is missing; repair the service identity explicitly before retrying"
}


# Every update accepts only the direct, root-only release produced by
# bootstrap-update.sh. The manifest is recomputed so no entry can be hidden by
# editing SHA256SUMS together with a subset of files.
# Her güncelleme yalnız bootstrap-update.sh'nin ürettiği doğrudan, root-only
# sürümü kabul eder. Bir girdi SHA256SUMS ile birlikte dosya alt kümesi
# değiştirilerek gizlenemesin diye manifest yeniden hesaplanır.
validate_trusted_release() {
    local root canonical relative updater entry owner mode permissions version
    [[ "$TRUSTED_RELEASE_ROOT" == /* ]] || die "trusted release root must be absolute"
    canonical=$(readlink -e -- "$TRUSTED_RELEASE_ROOT") || die "trusted release root is unavailable"
    [[ "$canonical" == "$TRUSTED_RELEASE_ROOT" ]] || die "trusted release root contains an alias"
    root=$canonical
    [[ "$root" == "$RELEASES_ROOT/"* ]] || die "trusted release is outside release storage"
    relative=${root#"$RELEASES_ROOT/"}
    [[ -n "$relative" && "$relative" != */* ]] || die "trusted release must be a direct child"
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] \
        || die "trusted release directory name is invalid: $relative"
    validate_root_trusted_dir_chain "$root"
    read -r owner mode < <(stat -Lc '%u %a' -- "$root") || die "cannot inspect trusted release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] || die "trusted release root must be root-owned mode 0700"
    if find "$root" -type l -print -quit | grep -q .; then
        die "trusted release contains a symbolic link"
    fi
    if find "$root" ! -type d ! -type f -print -quit | grep -q .; then
        die "trusted release contains a special filesystem object"
    fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") || die "cannot inspect trusted release entry: $entry"
        [[ "$owner" == 0 ]] || die "trusted release entry must be owned by root: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) || die "trusted release entry must not be group/other writable: $entry"
    done < <(find "$root" -mindepth 1 -print0)
    [[ ! -e "$root/.git" && ! -L "$root/.git" ]] || die "trusted release must be a Git archive, not a mutable checkout"
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] || die "trusted release checksum manifest is missing"
    (
        cd "$root"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "trusted release checksum verification failed"
    [[ -f "$root/release.version" && -f "$root/release.commit" && -f "$root/release.tree" ]] \
        || die "trusted release provenance is incomplete"
    version=$(tr -d '[:space:]' < "$root/release.version")
    [[ "$version" == 1 ]] || die "unsupported trusted release version: $version"
    trusted_release_commit=$(tr -d '[:space:]' < "$root/release.commit")
    trusted_release_tree=$(tr -d '[:space:]' < "$root/release.tree")
    [[ "$trusted_release_commit" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid trusted release commit"
    [[ "$trusted_release_tree" =~ ^[0-9a-f]{40,64}$ ]] || die "invalid trusted release tree"
    [[ "$PREFLIGHT_PANEL" == "$root/bin/panel" ]] || die "panel preflight must come from the trusted release"
    [[ "$PREFLIGHT_AGENT" == "$root/bin/agent" ]] || die "agent preflight must come from the trusted release"
    SCHEMA17_BRIDGE="$root/bin/schema17-bridge"
    validate_preflight_binary "$SCHEMA17_BRIDGE" schema17-bridge
    [[ -x "$root/install.sh" && -f "$root/install.sh" ]] || die "trusted release installer is missing"
    [[ -x "$root/rollback.sh" && -f "$root/rollback.sh" ]] || die "trusted release rollback is missing"
    [[ -f "$root/web/dist/index.html" ]] || die "trusted release web artifact is missing"
    updater=$(readlink -e -- "$0") || die "cannot resolve running updater"
    [[ "$updater" == "$root/update.sh" ]] || die "updater must execute from the trusted release"
}

# Read every process in a systemd service cgroup, including nested cgroups.
# İç içe cgroup'lar dahil bir systemd servis cgroup'undaki her işlemi oku.
service_cgroup_pids() {
    local unit=$1 control_group cgroup_root procs_file pid
    control_group=$(systemctl show --property=ControlGroup --value "$unit") \
        || die "cannot inspect control group for $unit"
    [[ -n "$control_group" ]] || return 0
    [[ "$control_group" == /* && "$control_group" != *'/../'* ]] \
        || die "unsafe control group for $unit: $control_group"
    cgroup_root="/sys/fs/cgroup$control_group"
    [[ -d "$cgroup_root" ]] || return 0
    find "$cgroup_root" -type f -name cgroup.procs -print0 \
        | while IFS= read -r -d '' procs_file; do
            while IFS= read -r pid; do
                [[ -z "$pid" || "$pid" =~ ^[0-9]+$ ]] || die "invalid pid in $procs_file"
                [[ -z "$pid" ]] || printf '%s\n' "$pid"
            done < "$procs_file"
        done
}

# A frozen legacy agent may contain only its systemd MainPID. Any helper means
# an untracked mutation might still be in flight, so the transition is refused.
# Dondurulmuş eski agent yalnızca systemd MainPID'ini içerebilir. Her yardımcı
# işlem izlenmeyen bir mutasyonun sürdüğü anlamına gelebilir; geçiş reddedilir.
reject_extra_service_cgroup_processes() {
    local unit=$1 expected_main=$2 pid_output
    local -a pids=()
    pid_output=$(service_cgroup_pids "$unit") || die "cannot enumerate $unit control group"
    if [[ -n "$pid_output" ]]; then
        mapfile -t pids <<< "$pid_output"
    fi
    if [[ "$expected_main" -gt 1 ]]; then
        [[ ${#pids[@]} -eq 1 && "${pids[0]}" == "$expected_main" ]] \
            || die "$unit has extra or unexpected cgroup processes: ${pids[*]:-none}"
    else
        [[ ${#pids[@]} -eq 0 ]] || die "$unit has residual cgroup processes: ${pids[*]}"
    fi
}

unfreeze_release_services() {
    if [[ $panel_frozen -eq 1 ]]; then
        systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-panel.service >/dev/null 2>&1 || true
        panel_frozen=0
    fi
    if [[ $agent_frozen -eq 1 ]]; then
        systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-agent.service >/dev/null 2>&1 || true
        agent_frozen=0
    fi
}

# Freeze the exact MainPID before active publication, then prove the cgroup has
# no helper or child process that could mutate state outside the frozen leader.
# Active yayımlanmadan önce tam MainPID'yi askıya al; ardından cgroup'un, askıya
# alınmış lider dışında durumu değiştirebilecek yardımcı veya alt süreç içermediğini kanıtla.
freeze_release_service_cgroup() {
    local unit=$1 label=$2 flag_name=$3 state main_pid frozen_state
    verify_quiesce_coordinator_identity "$unit" either \
        || die "$label identity changed before freeze"
    state=$(systemctl show --property=ActiveState --value "$unit") \
        || die "cannot inspect $label state"
    case "$state" in
        active|activating|reloading|refreshing)
            main_pid=$(systemctl show --property=MainPID --value "$unit") \
                || die "cannot inspect $label MainPID"
            [[ "$main_pid" =~ ^[0-9]+$ && "$main_pid" -gt 1 ]] \
                || die "$label has no valid MainPID"
            printf -v "$flag_name" '%s' 1
            if ! systemctl kill --kill-whom=all --signal=SIGSTOP "$unit"; then
                printf -v "$flag_name" '%s' 0
                die "$label could not be frozen"
            fi
            reject_extra_service_cgroup_processes "$unit" "$main_pid"
            for _ in $(seq 1 50); do
                frozen_state=$(awk '/^State:/ {print $2}' "/proc/$main_pid/status" 2>/dev/null || true)
                [[ "$frozen_state" == T || "$frozen_state" == t ]] && break
                sleep 0.02
            done
            [[ "$frozen_state" == T || "$frozen_state" == t ]] \
                || die "$label did not enter a frozen state"
            reject_extra_service_cgroup_processes "$unit" "$main_pid"
            ;;
        inactive|failed)
            reject_extra_service_cgroup_processes "$unit" 0
            ;;
        *)
            die "$label is in an unstable state: $state"
            ;;
    esac
    verify_quiesce_coordinator_identity "$unit" frozen \
        || die "$label identity or frozen state changed after freeze"
}

# Only after the active marker is durable may a frozen coordinator be killed.
# Yalnızca active işaretçisi kalıcı olduktan sonra askıya alınmış bir koordinatör sonlandırılabilir.
terminate_frozen_release_service() {
    local unit=$1 label=$2 flag_name=$3
    verify_quiesce_coordinator_identity "$unit" frozen \
        || die "$label identity changed before stop"
    if [[ "${!flag_name}" -eq 1 ]]; then
        systemctl stop --no-block "$unit" \
            || die "$label stop could not be queued"
        systemctl kill --kill-whom=all --signal=SIGKILL "$unit" \
            || die "frozen $label could not be terminated"
        printf -v "$flag_name" '%s' 0
    fi
    systemctl stop "$unit" || die "$label could not be stopped"
    if systemctl is-active --quiet "$unit"; then
        die "$label is still active after stop"
    fi
    reject_extra_service_cgroup_processes "$unit" 0
    verify_quiesce_coordinator_stopped "$unit" \
        || die "$label identity was not fully removed after stop"
}

# A quiesce abort resumes the exact existing coordinators before removing the
# start barrier. A crash before removal therefore remains resumable, while a
# crash after removal leaves ordinary running services rather than frozen ones.
# Quiesce iptali, start barrier kaldırılmadan önce mevcut koordinatörleri aynen
# sürdürür. Böylece kaldırma öncesindeki çökme sürdürülebilir kalır; kaldırma
# sonrasındaki çökme ise servisleri donmuş değil normal çalışır durumda bırakır.
resume_quiesced_release_services() {
    # The durable quiesce marker may outlive the shell assignment that records a
    # successful SIGSTOP. Signal only a captured active-like identity; a canonical
    # inactive state/0/0 row has no process to resume.
    # Kalıcı quiesce işaretçisi, başarılı SIGSTOP kaydını yapan kabuk atamasından
    # uzun yaşayabilir. Yalnız kaydedilmiş aktif-benzeri kimliğe sinyal gönder;
    # kanonik pasif state/0/0 satırında sürdürülecek süreç yoktur.
    verify_quiesce_coordinator_identity celikpanel-panel.service either || return 1
    verify_quiesce_coordinator_identity celikpanel-agent.service either || return 1
    if service_state_is_active_like "${quiesce_active_states[celikpanel-panel.service]}"; then
        systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-panel.service >/dev/null 2>&1 \
            || return 1
    fi
    if service_state_is_active_like "${quiesce_active_states[celikpanel-agent.service]}"; then
        systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-agent.service >/dev/null 2>&1 \
            || return 1
    fi
    panel_frozen=0
    agent_frozen=0
    verify_quiesce_coordinator_identity celikpanel-panel.service unfrozen || return 1
    verify_quiesce_coordinator_identity celikpanel-agent.service unfrozen || return 1
}

verify_release_service_resumed() {
    local unit=$1 label=$2 expected state main_pid process_state pid_output
    local -a pids=()
    expected=${saved_active_states[$unit]:-}
    [[ -n "$expected" ]] || return 1
    state=$(systemctl show --property=ActiveState --value "$unit") || return 1
    if service_state_is_active_like "$expected"; then
        service_state_is_active_like "$state" || return 1
        main_pid=$(systemctl show --property=MainPID --value "$unit") || return 1
        [[ "$main_pid" =~ ^[0-9]+$ && "$main_pid" -gt 1 ]] || return 1
        for _ in $(seq 1 50); do
            process_state=$(awk '/^State:/ {print $2}' "/proc/$main_pid/status" 2>/dev/null || true)
            [[ -n "$process_state" && "$process_state" != T && "$process_state" != t ]] && break
            sleep 0.02
        done
        [[ -n "$process_state" && "$process_state" != T && "$process_state" != t ]] || return 1
        pid_output=$(service_cgroup_pids "$unit") || return 1
        if [[ -n "$pid_output" ]]; then
            mapfile -t pids <<< "$pid_output"
        fi
        [[ ${#pids[@]} -eq 1 && "${pids[0]}" == "$main_pid" ]] || return 1
    else
        case "$state" in
            inactive|failed) ;;
            *) return 1 ;;
        esac
        pid_output=$(service_cgroup_pids "$unit") || return 1
        [[ -z "$pid_output" ]] || return 1
    fi
}

stop_release_coordinators_fail_closed() {
    local unit
    for unit in celikpanel-panel.service celikpanel-agent.service; do
        systemctl stop --no-block "$unit" >/dev/null 2>&1 || true
        systemctl kill --kill-whom=all --signal=SIGKILL "$unit" >/dev/null 2>&1 || true
        systemctl stop "$unit" >/dev/null 2>&1 || true
    done
    panel_frozen=0
    agent_frozen=0
}

preserve_quiesce_recovery_marker() {
    if [[ ! -e "$RELEASE_TRANSACTION_ROOT/quiesce.pending" &&
          ! -L "$RELEASE_TRANSACTION_ROOT/quiesce.pending" &&
          ! -e "$RELEASE_TRANSACTION_ROOT/active" &&
          ! -L "$RELEASE_TRANSACTION_ROOT/active" &&
          ! -e "$RELEASE_TRANSACTION_ROOT/completion.pending" &&
          ! -L "$RELEASE_TRANSACTION_ROOT/completion.pending" ]]; then
        release_txn_create_quiesce_marker \
            "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
            "$release_transaction_token" update "$snapshot_name" \
            "$SNAP_ROOT" "$stage_root" >/dev/null 2>&1 || true
    fi
}

fail_closed_quiesce_abort() {
    quiesce_abort_failed=1
    transaction_started=1
    transaction_phase=quiesce-failed
    preserve_staging=1
    preserve_quiesce_recovery_marker
    stop_release_coordinators_fail_closed
    echo "!! Quiesce abort verification failed; recovery state was preserved and both coordinators were stopped." >&2
    return 1
}

abort_quiesce_before_active() {
    transaction_started=1
    transaction_phase=quiesce
    preserve_staging=1
    release_txn_validate_quiesce_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || { fail_closed_quiesce_abort; return 1; }
    resume_quiesced_release_services \
        || { fail_closed_quiesce_abort; return 1; }
    verify_release_service_resumed celikpanel-panel.service panel \
        || { fail_closed_quiesce_abort; return 1; }
    verify_release_service_resumed celikpanel-agent.service agent \
        || { fail_closed_quiesce_abort; return 1; }
    release_txn_validate_quiesce_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || { fail_closed_quiesce_abort; return 1; }
    transaction_phase=quiesce-removing
    release_txn_remove_quiesce_marker \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || { fail_closed_quiesce_abort; return 1; }
    transaction_phase=quiesce-removed
    transaction_started=0
    preserve_staging=0
    release_release_mutation_lock || true
    transaction_phase=none
}
# systemd removes RuntimeDirectory when the agent stops. Recreate only the
# exact root:celikpanel 0750 directory needed for the shared flock.
# Agent durunca systemd RuntimeDirectory'yi kaldırır. Ortak flock için gereken
# tam root:celikpanel 0750 dizinini yeniden oluştur.
prepare_runtime_mutation_lock_dir() {
    local lock_dir group_id owner group mode
    lock_dir=$(dirname "$MUTATION_LOCK")
    [[ "$lock_dir" == /run/celikpanel ]] || die "unexpected mutation lock directory: $lock_dir"
    group_id=$(getent group celikpanel | cut -d: -f3) || die "celikpanel group is unavailable"
    [[ "$group_id" =~ ^[0-9]+$ ]] || die "celikpanel group id is invalid"
    if [[ -e "$lock_dir" || -L "$lock_dir" ]]; then
        [[ -d "$lock_dir" && ! -L "$lock_dir" ]] || die "unsafe mutation lock directory"
        validate_root_trusted_dir_chain "$lock_dir"
    fi
    install -d -m 0750 -o root -g celikpanel -- "$lock_dir" \
        || die "cannot prepare mutation lock directory"
    validate_root_trusted_dir_chain "$lock_dir"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect prepared mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
}
# Snapshot runtime state accepts only documented systemd ActiveState values.
# Snapshot çalışma durumu yalnız belgelenmiş systemd ActiveState değerlerini kabul eder.
validate_service_active_state() {
    local unit=$1 state=$2
    case "$state" in
        active|activating|reloading|refreshing|inactive|failed|deactivating|maintenance) ;;
        *) die "unsupported active state for $unit: $state" ;;
    esac
}

# Active-like states are restored with a controlled start; every other valid
# state remains stopped. The exact enablement state is verified independently.
# Active-benzeri durumlar kontrollü başlangıçla geri getirilir; diğer tüm geçerli
# durumlar kapalı kalır. Tam etkinleştirme durumu ayrıca doğrulanır.
service_state_is_active_like() {
    case "$1" in
        active|activating|reloading|refreshing) return 0 ;;
        *) return 1 ;;
    esac
}

declare -A saved_enabled_states=()
declare -A saved_active_states=()
declare -A quiesce_active_states=()
declare -A quiesce_main_pids=()
declare -A quiesce_start_times=()

# Read Linux /proc field 22 without trusting the process name, which may contain
# spaces or parentheses. PID plus this start time is the durable process identity.
# Linux /proc alan 22'yi, boşluk veya parantez içerebilen süreç adına güvenmeden
# oku. PID ile bu başlangıç zamanı birlikte kalıcı süreç kimliğini oluşturur.
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

coordinator_cgroup_matches_pid() {
    local unit=$1 expected_pid=$2 pid_output
    local -a pids=()
    pid_output=$(service_cgroup_pids "$unit") || return 1
    if [[ -n "$pid_output" ]]; then
        mapfile -t pids <<< "$pid_output"
    fi
    if [[ "$expected_pid" -gt 1 ]]; then
        [[ ${#pids[@]} -eq 1 && "${pids[0]}" == "$expected_pid" ]]
    else
        [[ ${#pids[@]} -eq 0 ]]
    fi
}

# Capture a stable coordinator identity before quiesce publication. Active-like
# rows carry exact PID/starttime; every inactive-like row is canonical state/0/0.
# Quiesce yayımlanmadan önce kararlı koordinatör kimliğini kaydet. Aktif-benzeri
# satırlar tam PID/başlangıç zamanı taşır; pasif-benzeri satırlar state/0/0'dır.
capture_quiesce_coordinator_identity() {
    local unit=$1 expected_state=$2 state main_pid start_time process_state
    local state_after main_pid_after start_time_after
    state=$(systemctl show --property=ActiveState --value "$unit") \
        || die "cannot inspect coordinator state before quiesce: $unit"
    [[ "$state" == "$expected_state" ]] \
        || die "coordinator state changed during quiesce capture: $unit ($state != $expected_state)"
    if service_state_is_active_like "$state"; then
        main_pid=$(systemctl show --property=MainPID --value "$unit") \
            || die "cannot inspect coordinator MainPID before quiesce: $unit"
        [[ "$main_pid" =~ ^[0-9]+$ && "$main_pid" -gt 1 ]] \
            || die "coordinator has no stable MainPID before quiesce: $unit"
        start_time=$(coordinator_process_start_time "$main_pid") \
            || die "cannot inspect coordinator start time before quiesce: $unit"
        process_state=$(awk '/^State:/ {print $2}' "/proc/$main_pid/status" 2>/dev/null || true)
        [[ -n "$process_state" && "$process_state" != T && "$process_state" != t ]] \
            || die "coordinator is frozen before quiesce capture: $unit"
        coordinator_cgroup_matches_pid "$unit" "$main_pid" \
            || die "coordinator cgroup changed during quiesce capture: $unit"
        state_after=$(systemctl show --property=ActiveState --value "$unit") \
            || die "cannot recheck coordinator state before quiesce: $unit"
        main_pid_after=$(systemctl show --property=MainPID --value "$unit") \
            || die "cannot recheck coordinator MainPID before quiesce: $unit"
        start_time_after=$(coordinator_process_start_time "$main_pid_after") \
            || die "cannot recheck coordinator start time before quiesce: $unit"
        [[ "$state_after" == "$state" && "$main_pid_after" == "$main_pid" &&
           "$start_time_after" == "$start_time" ]] \
            || die "coordinator identity changed during quiesce capture: $unit"
        printf '%s\t%s\t%s\t%s\n' "$unit" "$state" "$main_pid" "$start_time"
    else
        main_pid=$(systemctl show --property=MainPID --value "$unit") \
            || die "cannot inspect inactive coordinator MainPID: $unit"
        [[ "$main_pid" == 0 ]] || die "inactive coordinator has a MainPID: $unit"
        coordinator_cgroup_matches_pid "$unit" 0 \
            || die "inactive coordinator has residual cgroup processes: $unit"
        state_after=$(systemctl show --property=ActiveState --value "$unit") \
            || die "cannot recheck inactive coordinator state: $unit"
        main_pid_after=$(systemctl show --property=MainPID --value "$unit") \
            || die "cannot recheck inactive coordinator MainPID: $unit"
        [[ "$state_after" == "$state" && "$main_pid_after" == 0 ]] \
            || die "inactive coordinator identity changed during quiesce capture: $unit"
        printf '%s\t%s\t0\t0\n' "$unit" "$state"
    fi
}

capture_quiesce_coordinator_ledger() {
    local ledger=$1 unit
    (umask 077; : > "$ledger") || die "cannot create quiesce coordinator ledger"
    chown root:root -- "$ledger" || die "cannot own quiesce coordinator ledger"
    chmod 0600 -- "$ledger" || die "cannot protect quiesce coordinator ledger"
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        capture_quiesce_coordinator_identity "$unit" "${saved_active_states[$unit]}" >> "$ledger" \
            || die "cannot capture quiesce coordinator identity: $unit"
    done
}

# Load exactly two root-only identity rows and bind each state to the durable
# service-state ledger. No PID-only or noncanonical inactive identity is accepted.
# Tam iki root-only kimlik satırını yükle ve her durumu kalıcı servis-durum
# defterine bağla. Yalnız PID içeren veya kanonik olmayan pasif kimlik kabul edilmez.
load_quiesce_coordinator_identities() {
    local ledger=$1 unit state main_pid start_time extra owner group mode links size count=0 expected_unit
    quiesce_active_states=()
    quiesce_main_pids=()
    quiesce_start_times=()
    [[ -f "$ledger" && ! -L "$ledger" ]] \
        || die "quiesce coordinator ledger is missing or unsafe: $ledger"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$ledger") \
        || die "cannot inspect quiesce coordinator ledger"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 1024 ]] \
        || die "quiesce coordinator ledger metadata is invalid"
    while IFS=$'\t' read -r unit state main_pid start_time extra; do
        [[ -n "$unit" && -n "$state" && -n "$main_pid" && -n "$start_time" && -z "${extra:-}" ]] \
            || die "malformed quiesce coordinator ledger"
        case "$unit" in
            celikpanel-agent.service|celikpanel-panel.service) ;;
            *) die "unexpected unit in quiesce coordinator ledger: $unit" ;;
        esac
        if [[ "$count" -eq 0 ]]; then
            expected_unit=celikpanel-agent.service
        else
            expected_unit=celikpanel-panel.service
        fi
        [[ "$unit" == "$expected_unit" ]] \
            || die "quiesce coordinator ledger order is not canonical: expected $expected_unit"
        [[ -z "${quiesce_active_states[$unit]+x}" ]] \
            || die "duplicate unit in quiesce coordinator ledger: $unit"
        validate_service_active_state "$unit" "$state"
        [[ "$state" == "${saved_active_states[$unit]:-}" ]] \
            || die "quiesce identity state differs from service ledger: $unit"
        if service_state_is_active_like "$state"; then
            [[ "$main_pid" =~ ^[0-9]+$ && "$main_pid" -gt 1 && "$start_time" =~ ^[0-9]+$ && "$start_time" -gt 0 ]] \
                || die "active quiesce coordinator identity is invalid: $unit"
        else
            [[ "$main_pid" == 0 && "$start_time" == 0 ]] \
                || die "inactive quiesce coordinator identity must be state/0/0: $unit"
        fi
        quiesce_active_states["$unit"]=$state
        quiesce_main_pids["$unit"]=$main_pid
        quiesce_start_times["$unit"]=$start_time
        count=$((count + 1))
    done < "$ledger"
    [[ "$count" -eq 2 ]] || die "quiesce coordinator ledger must contain exactly two rows"
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        [[ -n "${quiesce_active_states[$unit]:-}" ]] \
            || die "quiesce coordinator identity is missing for $unit"
    done
}

verify_quiesce_coordinator_identity() {
    local unit=$1 process_requirement=$2 expected_state expected_pid expected_start
    local state main_pid start_time process_state
    case "$process_requirement" in
        either|frozen|unfrozen) ;;
        *) return 1 ;;
    esac
    expected_state=${quiesce_active_states[$unit]:-}
    expected_pid=${quiesce_main_pids[$unit]:-}
    expected_start=${quiesce_start_times[$unit]:-}
    [[ -n "$expected_state" && -n "$expected_pid" && -n "$expected_start" ]] || return 1
    state=$(systemctl show --property=ActiveState --value "$unit") || return 1
    [[ "$state" == "$expected_state" ]] || return 1
    main_pid=$(systemctl show --property=MainPID --value "$unit") || return 1
    if service_state_is_active_like "$expected_state"; then
        [[ "$main_pid" == "$expected_pid" ]] || return 1
        start_time=$(coordinator_process_start_time "$main_pid") || return 1
        [[ "$start_time" == "$expected_start" ]] || return 1
        coordinator_cgroup_matches_pid "$unit" "$main_pid" || return 1
        process_state=$(awk '/^State:/ {print $2}' "/proc/$main_pid/status" 2>/dev/null || true)
        [[ -n "$process_state" ]] || return 1
        case "$process_requirement" in
            frozen) [[ "$process_state" == T || "$process_state" == t ]] || return 1 ;;
            unfrozen) [[ "$process_state" != T && "$process_state" != t ]] || return 1 ;;
        esac
    else
        [[ "$expected_pid" == 0 && "$expected_start" == 0 && "$main_pid" == 0 ]] || return 1
        coordinator_cgroup_matches_pid "$unit" 0 || return 1
    fi
}

verify_quiesce_coordinator_stopped() {
    local unit=$1 state main_pid prior_pid prior_start current_start
    state=$(systemctl show --property=ActiveState --value "$unit") || return 1
    case "$state" in
        inactive|failed) ;;
        *) return 1 ;;
    esac
    main_pid=$(systemctl show --property=MainPID --value "$unit") || return 1
    [[ "$main_pid" == 0 ]] || return 1
    coordinator_cgroup_matches_pid "$unit" 0 || return 1
    prior_pid=${quiesce_main_pids[$unit]:-0}
    prior_start=${quiesce_start_times[$unit]:-0}
    if [[ "$prior_pid" -gt 1 ]] && current_start=$(coordinator_process_start_time "$prior_pid" 2>/dev/null); then
        [[ "$current_start" != "$prior_start" ]] || return 1
    fi
}

# Active recovery never recreates a historical process. It accepts only a
# proven already-stopped unit or the exact captured process while still frozen.
# Active kurtarma geçmişteki bir süreci yeniden oluşturmaz. Yalnız kanıtlanmış
# biçimde durmuş unit'i veya hâlâ askıdaki tam kaydedilmiş süreci kabul eder.
recover_active_release_service() {
    local unit=$1 label=$2 flag_name=$3
    if verify_quiesce_coordinator_stopped "$unit"; then
        printf -v "$flag_name" '%s' 0
        return 0
    fi
    if service_state_is_active_like "${quiesce_active_states[$unit]:-}" &&
       verify_quiesce_coordinator_identity "$unit" frozen; then
        printf -v "$flag_name" '%s' 1
        terminate_frozen_release_service "$unit" "$label" "$flag_name"
        return 0
    fi
    die "$label is neither the exact frozen coordinator nor a proven stopped coordinator during active recovery"
}

# Load exactly the three versioned service-state rows covered by the snapshot
# manifest. Duplicate, missing or unknown rows are rejected before any start.
# Snapshot manifestinin kapsadığı tam üç sürümlü servis-durum satırını yükle.
# Yinelenen, eksik veya bilinmeyen satırlar başlangıçtan önce reddedilir.
load_saved_service_states() {
    local ledger=$1 unit enabled_state active_state extra count=0
    saved_enabled_states=()
    saved_active_states=()
    [[ -f "$ledger" && ! -L "$ledger" ]] \
        || die "service state ledger is missing or unsafe: $ledger"
    while IFS=$'\t' read -r unit enabled_state active_state extra; do
        [[ -n "$unit" && -n "$enabled_state" && -n "$active_state" && -z "${extra:-}" ]] \
            || die "malformed service state ledger"
        case "$unit" in
            celikpanel-agent.service|celikpanel-panel.service|celikpanel-firewall-restore.service) ;;
            *) die "unexpected unit in service state ledger: $unit" ;;
        esac
        [[ -z "${saved_enabled_states[$unit]+x}" ]] \
            || die "duplicate unit in service state ledger: $unit"
        case "$enabled_state" in
            enabled|enabled-runtime|disabled|static|indirect|not-found) ;;
            *) die "unsupported saved enable state for $unit: $enabled_state" ;;
        esac
        validate_service_active_state "$unit" "$active_state"
        saved_enabled_states["$unit"]=$enabled_state
        saved_active_states["$unit"]=$active_state
        count=$((count + 1))
    done < "$ledger"
    [[ "$count" -eq 3 ]] || die "service state ledger must contain exactly three rows"
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        [[ -n "${saved_enabled_states[$unit]:-}" ]] \
            || die "service state is missing for $unit"
    done
    if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}" && \
       ! service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
        die "saved runtime state is inconsistent: an active panel requires an active agent"
    fi
}

verify_saved_enablement() {
    local unit actual
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        actual=$(systemctl is-enabled "$unit" 2>/dev/null || true)
        [[ "${actual:-unknown}" == "${saved_enabled_states[$unit]}" ]] \
            || die "installed enablement mismatch for $unit: got ${actual:-unknown}, want ${saved_enabled_states[$unit]}"
    done
}

verify_saved_runtime_states() {
    local unit state
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        state=${saved_active_states[$unit]}
        if service_state_is_active_like "$state"; then
            systemctl is-active --quiet "$unit" \
                || die "saved active-like service is not active: $unit"
        elif systemctl is-active --quiet "$unit"; then
            die "saved inactive-like service became active: $unit"
        fi
    done
}

# Pending finalization trusts only a complete target-bound snapshot whose full
# manifest covers the exact provenance, transition mode and service-state ledger.
# Bekleyen sonuçlandırma yalnız tam manifesti kesin provenance, geçiş modu ve
# servis-durum ledger'ını kapsayan, hedefe bağlı eksiksiz snapshot'a güvenir.
validate_pending_update_snapshot() {
    local snapshot_name=$1 snapshot created target nonce entry owner group mode permissions
    local ledger identity_ledger transition links size
    IFS=$'\t' read -r created target nonce < <(release_txn_parse_update_snapshot_name "$snapshot_name") \
        || die "pending update snapshot name is not canonical"
    [[ "$target" == "$target_release_commit" ]] \
        || die "pending update targets $target, not trusted release $target_release_commit"
    snapshot="$SNAP_ROOT/$snapshot_name"
    [[ -d "$snapshot" && ! -L "$snapshot" ]] \
        || die "pending update final snapshot is missing or unsafe: $snapshot"
    validate_retention_snapshot "$snapshot"
    while IFS= read -r -d '' entry; do
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$entry") \
            || die "cannot inspect pending snapshot object: $entry"
        permissions=$((8#$mode))
        [[ "$owner" == 0 ]] && (( (permissions & 0022) == 0 )) \
            || die "pending snapshot objects must be root-owned and group/other non-writable"
    done < <(find "$snapshot" -mindepth 1 -print0)
    for entry in commit target-release.commit target-release.tree created-at-utc service-states.tsv quiesce-coordinators.tsv snapshot-transition.state; do
        [[ -f "$snapshot/$entry" && ! -L "$snapshot/$entry" ]] \
            || die "pending snapshot provenance file is missing or unsafe: $entry"
    done
    printf 'unknown\n' | cmp -s - "$snapshot/commit" \
        || die "pending snapshot source provenance is not exact"
    printf '%s\n' "$target_release_commit" | cmp -s - "$snapshot/target-release.commit" \
        || die "pending snapshot target commit does not match the trusted release"
    printf '%s\n' "$target_release_tree" | cmp -s - "$snapshot/target-release.tree" \
        || die "pending snapshot target tree does not match the trusted release"
    printf '%s\n' "$created" | cmp -s - "$snapshot/created-at-utc" \
        || die "pending snapshot timestamp does not match its name"
    ledger=$snapshot/service-states.tsv
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$ledger") \
        || die "cannot inspect pending snapshot service-state ledger"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 4096 ]] \
        || die "pending snapshot service-state ledger metadata is invalid"
    identity_ledger=$snapshot/quiesce-coordinators.tsv
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$identity_ledger") \
        || die "cannot inspect pending snapshot coordinator identity ledger"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 1024 ]] \
        || die "pending snapshot coordinator identity ledger metadata is invalid"
    transition=$snapshot/snapshot-transition.state
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$transition") \
        || die "cannot inspect pending snapshot transition state"
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 600 && "$links" == 1 &&
       "$size" -gt 0 && "$size" -le 32 ]] \
        || die "pending snapshot transition metadata is invalid"
    if printf 'normal\n' | cmp -s - "$transition"; then
        pending_snapshot_transition=normal
    elif printf 'pre-ledger\n' | cmp -s - "$transition"; then
        pending_snapshot_transition=pre-ledger
    elif printf 'schema17\n' | cmp -s - "$transition"; then
        pending_snapshot_transition=schema17
    else
        die "pending snapshot transition state is not canonical"
    fi
    load_saved_service_states "$ledger"
    load_quiesce_coordinator_identities "$identity_ledger"
    pending_snapshot_path=$snapshot
}

# Installed bytes must still equal the same immutable release before a pending
# transaction is allowed to start even one service.
# Bekleyen bir işlemin tek bir servisi bile başlatmasına izin verilmeden önce
# kurulu baytlar aynı değişmez sürümle hâlâ tam eşleşmelidir.
verify_installed_release_artifacts() {
    validate_preflight_binary "$BIN_DIR/panel" installed-panel
    validate_preflight_binary "$BIN_DIR/agent" installed-agent
    cmp -s "$TRUSTED_RELEASE_ROOT/bin/panel" "$BIN_DIR/panel" \
        || die "installed panel does not match the trusted release"
    cmp -s "$TRUSTED_RELEASE_ROOT/bin/agent" "$BIN_DIR/agent" \
        || die "installed agent does not match the trusted release"
    validate_root_trusted_dir_chain "$WEB_DIR"
    if find "$WEB_DIR" -type l -print -quit | grep -q .; then
        die "installed web tree contains a symbolic link"
    fi
    if find "$WEB_DIR" ! -type d ! -type f -print -quit | grep -q .; then
        die "installed web tree contains a special filesystem object"
    fi
    cmp -s \
        <(cd "$TRUSTED_RELEASE_ROOT/web/dist" && \
            LC_ALL=C find . -mindepth 1 -printf '%y\t%p\n' | LC_ALL=C sort) \
        <(cd "$WEB_DIR" && \
            LC_ALL=C find . -mindepth 1 -printf '%y\t%p\n' | LC_ALL=C sort) \
        || die "installed web tree structure does not match the trusted release"
    cmp -s \
        <(cd "$TRUSTED_RELEASE_ROOT/web/dist" && \
            LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
        <(cd "$WEB_DIR" && \
            LC_ALL=C find . -type f -print0 | LC_ALL=C sort -z | xargs -0 sha256sum) \
        || die "installed web tree does not match the trusted release"
}
# Retention may delete only a complete direct-child snapshot whose payload
# still matches the versioned checksum contract.
# Saklama temizliği yalnız sürümlü checksum sözleşmesi hâlâ doğrulanan eksiksiz
# bir doğrudan-alt snapshot'ı silebilir.
validate_retention_snapshot() {
    local snapshot=$1 relative version
    [[ "$snapshot" == "$SNAP_ROOT/"* ]] || die "unsafe retention path refused: $snapshot"
    relative=${snapshot#"$SNAP_ROOT/"}
    [[ -n "$relative" && "$relative" != */* ]] || die "nested retention path refused: $snapshot"
    validate_root_trusted_dir_chain "$snapshot"
    if find "$snapshot" -type l -print -quit | grep -q .; then
        die "retention snapshot contains a symbolic link: $snapshot"
    fi
    if find "$snapshot" ! -type d ! -type f -print -quit | grep -q .; then
        die "retention snapshot contains a special filesystem object: $snapshot"
    fi
    [[ -f "$snapshot/snapshot.version" && ! -L "$snapshot/snapshot.version" ]] \
        || die "retention snapshot version is missing or unsafe: $snapshot"
    version=$(tr -d '[:space:]' < "$snapshot/snapshot.version")
    [[ "$version" == "$SNAPSHOT_VERSION" ]] \
        || die "unsupported retention snapshot version at $snapshot: $version"
    [[ -f "$snapshot/SHA256SUMS" && ! -L "$snapshot/SHA256SUMS" ]] \
        || die "retention snapshot checksum manifest is missing or unsafe: $snapshot"
    (
        cd "$snapshot"
        LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "retention snapshot checksum verification failed: $snapshot"
}

# The shell holds the same flock used by privileged agent mutations through the
# final proofs. It hands the lock to a saved-active agent only long enough for
# startup reconciliation, then reacquires it before the panel may start.
# Shell son kanıtlar boyunca ayrıcalıklı agent işlemleriyle aynı flock kilidini
# tutar. Yalnız kayıtlı-aktif agent'ın başlangıç uzlaştırması için kilidi kısa
# süreliğine devreder ve panel başlamadan önce yeniden alır.
acquire_release_mutation_lock() {
    local acquire_mode=${1:-immediate}
    local lock_dir group_id owner group mode links size path_identity fd_identity
    [[ $# -le 1 ]] || die "release mutation lock accepts at most one acquisition mode"
    case "$acquire_mode" in
        immediate | handoff) ;;
        *) die "unsupported release mutation lock acquisition mode: $acquire_mode" ;;
    esac
    command -v flock >/dev/null || die "flock is required for a safe update"
    group_id=$(getent group celikpanel | cut -d: -f3) \
        || die "celikpanel group is unavailable for the mutation lock"
    [[ "$group_id" =~ ^[0-9]+$ ]] || die "celikpanel group id is invalid"
    lock_dir=$(dirname "$MUTATION_LOCK")
    validate_root_trusted_dir_chain "$lock_dir"
    [[ -d "$lock_dir" && ! -L "$lock_dir" ]] || die "unsafe mutation lock directory: $lock_dir"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$lock_dir") \
        || die "cannot inspect mutation lock directory"
    [[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 750 ]] \
        || die "mutation lock directory must be root:celikpanel mode 0750"
    if [[ -e "$MUTATION_LOCK" || -L "$MUTATION_LOCK" ]]; then
        [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] || die "unsafe mutation lock file"
    else
        [[ "$acquire_mode" == immediate ]] \
            || die "mutation lock pathname disappeared before controlled agent-start handoff reacquire"
        (umask 077; set -o noclobber; : > "$MUTATION_LOCK") \
            || die "cannot exclusively create mutation lock file"
        chown root:celikpanel -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock ownership"
        chmod 0600 -- "$MUTATION_LOCK" \
            || die "cannot set mutation lock permissions"
    fi
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$MUTATION_LOCK") \
        || die "cannot inspect mutation lock file"
    [[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 600 \
        && "$links" == 1 && "$size" == 0 ]] \
        || die "mutation lock file must be root:celikpanel mode 0600, single-link, and empty"
    path_identity=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") \
        || die "cannot identify mutation lock file"
    if [[ "$acquire_mode" == handoff ]]; then
        [[ -n "$MUTATION_LOCK_IDENTITY" ]] \
            || die "mutation lock identity is unavailable for controlled agent-start handoff"
        [[ "$path_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
            || die "mutation lock pathname changed during controlled agent-start handoff"
    else
        MUTATION_LOCK_IDENTITY=$path_identity
    fi
    exec {MUTATION_LOCK_FD}<>"$MUTATION_LOCK"
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD") \
        || die "cannot identify opened mutation lock"
    [[ "$fd_identity" == "$MUTATION_LOCK_IDENTITY" ]] \
        || die "mutation lock changed while it was opened"
    if ! flock -n -x "$MUTATION_LOCK_FD"; then
        if [[ "$acquire_mode" == handoff ]]; then
            die "mutation lock was not handed back after controlled agent start; update refused"
        fi
        die "a service/package mutation is active; update refused"
    fi
}
# Close the exact flock descriptor before rebuilding a RuntimeDirectory-backed
# lock path after agent shutdown or before the terminal idle proof.
# Agent kapandıktan sonra RuntimeDirectory tabanlı lock yolunu yeniden kurmadan
# veya son boşta kanıtından önce tam flock descriptor'ını kapat.
release_release_mutation_lock() {
    [[ -n "${MUTATION_LOCK_FD:-}" ]] || return 0
    flock -u "$MUTATION_LOCK_FD" || return 1
    exec {MUTATION_LOCK_FD}>&-
    MUTATION_LOCK_FD=
}

prepare_fresh_agent_socket_start() {
    local socket=/run/celikpanel/agent.sock state
    [[ -n "${MUTATION_LOCK_FD:-}" ]] || die "fresh socket preparation requires mutation lock"
    state=$(systemctl show -p ActiveState --value celikpanel-agent.service) \
        || die 'cannot inspect agent before fresh socket preparation'
    case $state in
        inactive|failed) ;;
        *) die 'fresh socket preparation requires the agent stopped' ;;
    esac
    reject_extra_service_cgroup_processes celikpanel-agent.service 0
    if [[ -e $socket || -L $socket ]]; then
        [[ -S $socket && ! -L $socket ]] || die 'unsafe stale agent socket refused'
        rm -f -- /run/celikpanel/agent.sock || die 'cannot remove verified stale agent socket'
    fi
    [[ ! -e $socket && ! -L $socket ]] || die 'agent socket path is not absent before controlled start'
}

wait_for_fresh_active_agent() {
    local socket=/run/celikpanel/agent.sock state _
    for _ in $(seq 1 40); do
        state=$(systemctl show -p ActiveState --value celikpanel-agent.service) \
            || die 'cannot inspect agent during controlled start'
        [[ ! -L $socket ]] || die 'agent socket became a symbolic link during controlled start'
        if [[ -e $socket && ! -S $socket ]]; then
            die 'agent socket path became unsafe during controlled start'
        fi
        if [[ $state == active && -S $socket ]]; then
            return 0
        fi
        [[ $state != failed ]] || die 'agent failed during controlled start'
        sleep 0.3
    done
    die 'agent did not become active with a fresh socket'
}

# Run embedded migrations with both coordinators stopped and the exact mutation
# lock held. No HTTP process starts before this durable proof succeeds.
# Gömülü migration'ları iki koordinatör kapalı ve tam mutation kilidi eldeyken
# çalıştır. Bu kalıcı kanıt başarılı olmadan hiçbir HTTP süreci başlamaz.
run_panel_migrations_offline() {
    local unit state
    [[ -n "${MUTATION_LOCK_FD:-}" ]] \
        || die "offline panel migration requires the release mutation lock"
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        state=$(systemctl show --property=ActiveState --value "$unit") \
            || die "cannot inspect service before offline panel migration: $unit"
        case "$state" in
            inactive|failed) ;;
            *) die "offline panel migration requires $unit stopped; found $state" ;;
        esac
        reject_extra_service_cgroup_processes "$unit" 0
    done
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "agent ledger is not idle before offline panel migration"
    sudo -u celikpanel -- env -i \
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
        HOME=/var/lib/celikpanel LC_ALL=C \
        CELIKPANEL_DATA_DIR="$(dirname "$PANEL_DB")" \
        "$BIN_DIR/panel" --migrate-only \
        || die "offline panel database migration failed"
    CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$BIN_DIR/panel" --check-service-operations-idle \
        || die "panel ledger is not idle after offline migration"
    sync -f -- "$PANEL_DB" "$(dirname "$PANEL_DB")" \
        || die "offline migrated panel database could not be made durable"
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "agent ledger changed during offline panel migration"
}

preflight_staged_installer_runtime
prepare_snapshot_root
validate_trusted_release
update_root=$TRUSTED_RELEASE_ROOT
cd "$update_root"

# Only the fully verified release may provide transaction marker and systemd
# guard logic. Install both drop-ins before any active marker is published.
# İşlem işaretçisi ve systemd koruma mantığını yalnız tamamen doğrulanmış sürüm
# sağlayabilir. Active işaretçisi yayımlanmadan önce iki drop-in'i de kur.
# shellcheck source=deploy/release-transaction-guard.sh
source "$TRUSTED_RELEASE_ROOT/deploy/release-transaction-guard.sh"
release_txn_verify_inherited_lock "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" || die "persistent release transaction lock verification failed"
release_txn_install_and_verify_unit_guards "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" "$UNIT_DIR" "$RELEASE_TRANSACTION_HELPER" "$RELEASE_TRANSACTION_FD" || die "release transaction service guards could not be installed"
release_txn_clear_stale_start_authorization "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" "$RELEASE_TRANSACTION_FD" || die "stale release start authorization could not be cleared"

source_commit=unknown
target_release_commit=$trusted_release_commit
target_release_tree=$trusted_release_tree
[[ "$target_release_commit" =~ ^[0-9a-f]{40}$ ]] \
    || die "trusted target commit must be a full 40-character object id"
validate_preflight_binary "$PREFLIGHT_PANEL" panel
validate_preflight_binary "$PREFLIGHT_AGENT" agent

# Exactly one durable phase may exist; choosing a branch before this proof could
# let a conflicting marker be ignored by the update finalizer.
# Tam olarak bir kalıcı aşama olabilir; bu kanıttan önce dal seçmek, çakışan bir
# markerın update finalizer tarafından yok sayılmasına yol açabilir.
release_marker_count=0
for release_marker_name in quiesce.pending active completion.pending; do
    [[ -e "$RELEASE_TRANSACTION_ROOT/$release_marker_name" ||
       -L "$RELEASE_TRANSACTION_ROOT/$release_marker_name" ]] \
        && release_marker_count=$((release_marker_count + 1))
done
[[ "$release_marker_count" -le 1 ]] \
    || die "multiple durable release transaction markers exist"

# The requested transition mode is durable snapshot identity, not a property
# inferred later from the target release.
# İstenen geçiş modu hedef release özelliği değil, kalıcı snapshot kimliğidir;
# daha sonra hedef release'den türetilmez.
snapshot_schema=normal
[[ $BOOTSTRAP_PRE_LEDGER -ne 1 ]] || snapshot_schema=pre-ledger
[[ $BOOTSTRAP_SCHEMA17 -ne 1 ]] || snapshot_schema=schema17
# A durable completion marker means bytes were already verified and only the
# guarded start/cleanup phase remains. Finish that phase and require a fresh run.
# Kalıcı completion işaretçisi baytların doğrulandığını ve yalnız korumalı
# başlatma/temizlik aşamasının kaldığını gösterir. Bu aşamayı bitir ve yeni koşu iste.
if [[ -e "$RELEASE_TRANSACTION_ROOT/completion.pending" || -L "$RELEASE_TRANSACTION_ROOT/completion.pending" ]]; then
    pending_finalization_succeeded=0
    pending_completion_verified=0
    pending_completion_removing=0

    # Any pending-finalization failure leaves both coordinators stopped and keeps
    # completion.pending for an exact retry with the same immutable release.
    # Bekleyen sonuçlandırmadaki her hata iki koordinatörü kapalı bırakır ve aynı
    # değişmez sürümle tam yeniden deneme için completion.pending'i korur.
    pending_finalization_exit() {
        local status=$?
        trap - EXIT
        if [[ "$status" -ne 0 && $pending_finalization_succeeded -eq 0 ]]; then
            if [[ $pending_completion_verified -eq 1 &&
                  $pending_completion_removing -eq 1 &&
                  ! -e "$RELEASE_TRANSACTION_ROOT/completion.pending" &&
                  ! -L "$RELEASE_TRANSACTION_ROOT/completion.pending" ]]; then
                release_release_mutation_lock >/dev/null 2>&1 || true
                return "$status"
            fi
            systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
            systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
            echo "!! Pending update finalization failed; marker preserved and both coordinators stopped." >&2
            echo "!! Bekleyen güncelleme sonuçlandırması başarısız; marker korundu ve iki koordinatör durduruldu." >&2
        fi
        return "$status"
    }
    trap pending_finalization_exit EXIT

    IFS=$'\t' read -r pending_token pending_operation pending_snapshot \
        < <(release_txn_read_pending_fields "$RELEASE_TRANSACTION_ROOT") \
        || die "cannot read pending release transaction"
    [[ "$pending_operation" == update ]] \
        || die "a pending rollback must be finalized by rollback.sh"
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$pending_token" update "$pending_snapshot" \
        || die "pending update marker changed before validation"
    validate_pending_update_snapshot "$pending_snapshot"
    verify_installed_release_artifacts
    verify_saved_enablement
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$pending_token" update "$pending_snapshot" \
        || die "pending update marker changed during validation"

    prepare_runtime_mutation_lock_dir
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$BIN_DIR/agent" --check-service-mutation-idle \
        || die "pending update agent/package mutations are not idle"
    acquire_release_mutation_lock
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "pending update agent/package state changed before the locked stop"

    systemctl stop celikpanel-panel.service \
        || die "pending update panel could not be stopped for finalization"
    systemctl stop celikpanel-agent.service \
        || die "pending update agent could not be stopped for finalization"
    systemctl is-active --quiet celikpanel-panel.service \
        && die "pending update panel is still active after stop"
    systemctl is-active --quiet celikpanel-agent.service \
        && die "pending update agent is still active after stop"
    reject_extra_service_cgroup_processes celikpanel-panel.service 0
    reject_extra_service_cgroup_processes celikpanel-agent.service 0

    release_release_mutation_lock \
        || die "cannot release stale pending-finalization mutation lock"
    prepare_runtime_mutation_lock_dir
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$BIN_DIR/agent" --check-service-mutation-idle \
        || die "pending update agent ledger changed while stopping"
    acquire_release_mutation_lock
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "pending update agent ledger is not idle under the rebuilt lock"

    if [[ "$pending_snapshot_transition" == normal ]]; then
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware \
            || die "pending normal update panel ledger is not idle"
    elif CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware; then
        :
    elif CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware; then
        :
    else
        die "pending pre-ledger update database is neither exact pre-ledger nor normal"
    fi
    verify_installed_release_artifacts
    verify_saved_enablement
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$pending_token" update "$pending_snapshot" \
        || die "pending update marker changed before offline migration"
    run_panel_migrations_offline
    verify_installed_release_artifacts
    verify_saved_enablement
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$pending_token" update "$pending_snapshot" \
        || die "pending update marker changed before controlled starts"

    if service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
        prepare_fresh_agent_socket_start
    fi
    release_txn_create_start_authorization \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
        "$RELEASE_TRANSACTION_FD" "$pending_token" update "$pending_snapshot" \
        || die "cannot authorize pending update controlled starts"
    if service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
        release_release_mutation_lock \
            || die "cannot hand the mutation lock to the pending update agent"
        systemctl start celikpanel-agent.service \
            || die "pending update agent could not be started"
        wait_for_fresh_active_agent
        acquire_release_mutation_lock handoff
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
            || die "pending update agent state is not idle after the startup lock handoff"
        verify_installed_release_artifacts
        verify_saved_enablement
        release_txn_validate_pending_token \
            "$RELEASE_TRANSACTION_ROOT" "$pending_token" update "$pending_snapshot" \
            || die "pending update marker changed during the startup lock handoff"
    fi
    if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}"; then
        systemctl start celikpanel-panel.service \
            || die "pending update panel could not be started"
        systemctl is-active --quiet celikpanel-panel.service \
            || die "pending update panel is not active"
    fi

    verify_saved_runtime_states
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "pending update agent ledger changed during controlled starts"
    verify_saved_enablement
    release_txn_remove_start_authorization \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
        "$RELEASE_TRANSACTION_FD" "$pending_token" update "$pending_snapshot" \
        || die "cannot remove pending update start authorization"
    verify_saved_runtime_states
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "pending update agent durable ledger is not ready before completion"
    verify_saved_enablement
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$pending_token" update "$pending_snapshot" \
        || die "pending update marker changed before durable completion"
    pending_completion_verified=1
    pending_completion_removing=1
    release_txn_remove_completion_pending \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$pending_token" update "$pending_snapshot" \
        || die "cannot complete pending update transaction"
    pending_finalization_succeeded=1
    release_release_mutation_lock \
        || die "cannot release pending update mutation lock"
    trap - EXIT
    echo "==> Previous pending update finalized from verified snapshot: $pending_snapshot_path"
    echo "==> Önceki bekleyen güncelleme doğrulanmış snapshot'tan tamamlandı: $pending_snapshot_path"
    exit 0
fi
resume_quiescing_update=0
resume_active_update=0
transaction_phase=none
resumable_snapshot_created_at=
resumable_snapshot_target_commit=
resumable_snapshot_nonce=
resumable_snapshot=
resumable_token=
resumable_stage=
if [[ -e "$RELEASE_TRANSACTION_ROOT/quiesce.pending" || -L "$RELEASE_TRANSACTION_ROOT/quiesce.pending" ]]; then
    IFS=$'\t' read -r quiesce_token quiesce_operation quiesce_snapshot < <(release_txn_read_quiesce_fields "$RELEASE_TRANSACTION_ROOT") \
        || die "cannot read quiescing release transaction"
    [[ "$quiesce_operation" == update ]] \
        || die "quiesce marker has unsupported operation: $quiesce_operation"
    IFS=$'\t' read -r resumable_snapshot_created_at resumable_snapshot_target_commit resumable_snapshot_nonce \
        < <(release_txn_parse_update_snapshot_name "$quiesce_snapshot") \
        || die "quiescing update snapshot name is not canonical"
    [[ "$resumable_snapshot_target_commit" == "$target_release_commit" ]] \
        || die "quiescing update targets $resumable_snapshot_target_commit; rerun its exact trusted release"
    if [[ -e "$SNAP_ROOT/$quiesce_snapshot" || -L "$SNAP_ROOT/$quiesce_snapshot" ]]; then
        die "quiescing update already has final snapshot $SNAP_ROOT/$quiesce_snapshot; use explicit rollback"
    fi
    resumable_stage=$(release_txn_find_update_snapshot_stage "$SNAP_ROOT" "$quiesce_snapshot") \
        || die "quiescing update has no single canonical staging tree; phase preserved and recovery failed closed"
    resumable_snapshot=$quiesce_snapshot
    resumable_token=$quiesce_token
    resume_quiescing_update=1
    transaction_phase=quiesce
elif [[ -e "$RELEASE_TRANSACTION_ROOT/active" || -L "$RELEASE_TRANSACTION_ROOT/active" ]]; then
    IFS=$'\t' read -r active_token active_operation active_snapshot < <(release_txn_read_active_fields "$RELEASE_TRANSACTION_ROOT") \
        || die "cannot read active release transaction"
    [[ "$active_operation" == update ]] \
        || die "active rollback transaction for snapshot $active_snapshot must be recovered by rollback.sh"
    IFS=$'\t' read -r resumable_snapshot_created_at resumable_snapshot_target_commit resumable_snapshot_nonce \
        < <(release_txn_parse_update_snapshot_name "$active_snapshot") \
        || die "active update snapshot name is not canonical"
    [[ "$resumable_snapshot_target_commit" == "$target_release_commit" ]] \
        || die "active update targets $resumable_snapshot_target_commit; rerun its exact trusted release or use explicit rollback"
    if [[ -e "$SNAP_ROOT/$active_snapshot" || -L "$SNAP_ROOT/$active_snapshot" ]]; then
        die "active update already has final snapshot $SNAP_ROOT/$active_snapshot; use explicit rollback before any new update"
    fi
    resumable_stage=$(release_txn_find_update_snapshot_stage "$SNAP_ROOT" "$active_snapshot") \
        || die "active update has no single canonical resumable staging tree; marker preserved and recovery failed closed"
    resumable_snapshot=$active_snapshot
    resumable_token=$active_token
    resume_active_update=1
    transaction_phase=active
fi

# With no durable marker, clean at most one fully canonical pre-publish orphan
# before a new transaction can create another staging tree.
# Kalıcı marker yokken, yeni işlem başka bir staging ağacı oluşturmadan önce en
# fazla bir tamamen kanonik yayın-öncesi artığı temizle.
if [[ $resume_quiescing_update -eq 0 && $resume_active_update -eq 0 ]]; then
    release_txn_cleanup_unmarked_update_snapshot_stage \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" "$SNAP_ROOT" \
        || die "unmarked snapshot staging cleanup failed closed; inspect and repair it explicitly"
fi

# Resume availability decisions come from the durable pre-quiesce ledger; a
# frozen process observed after a crash is not the original requested state.
# Yeniden sürdürmede erişilebilirlik kararı kalıcı quiesce-öncesi defterden gelir;
# crash sonrası görülen frozen süreç ilk istenen durum değildir.
panel_was_active=0
agent_was_active=0
if [[ $resume_quiescing_update -eq 1 || $resume_active_update -eq 1 ]]; then
    load_saved_service_states "$resumable_stage/$resumable_snapshot/service-states.tsv"
    load_quiesce_coordinator_identities "$resumable_stage/$resumable_snapshot/quiesce-coordinators.tsv"
    service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}" && panel_was_active=1
    service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}" && agent_was_active=1
else
    systemctl is-active --quiet celikpanel-panel.service && panel_was_active=1
    systemctl is-active --quiet celikpanel-agent.service && agent_was_active=1
fi
# Before installed bytes change, a failure restores only service availability.
# After mutation starts, mixed artifacts are stopped and require explicit rollback.
# Kurulu baytlar değişmeden önce hata yalnız servis erişilebilirliğini geri getirir.
# Mutasyon başladıktan sonra karışık ürünler durdurulur ve açık geri alma gerekir.
restart_previous_services() {
    local status=$1
    unfreeze_release_services
    if [[ $transaction_started -eq 1 ]]; then
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
        echo "!! Update transaction remains active; both services were left stopped for exact recovery." >&2
        echo "!! Güncelleme işlemi active kaldı; tam kurtarma için iki servis kapalı bırakıldı." >&2
        [[ -z "$verified_snapshot" ]] || echo "!! Verified snapshot / Doğrulanmış snapshot: $verified_snapshot" >&2
        return "$status"
    fi
    if [[ $mutation_started -eq 1 ]]; then
        systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true
        systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true
        echo "!! Update failed after installed mutation began; both services were left stopped." >&2
        echo "!! Kurulu mutasyon başladıktan sonra güncelleme başarısız oldu; iki servis kapalı bırakıldı." >&2
        if [[ -n "$verified_snapshot" ]]; then
            echo "!! Verified snapshot / Doğrulanmış snapshot: $verified_snapshot" >&2
            echo "!! Roll back / Geri alın: sudo /bin/bash '$TRUSTED_RELEASE_ROOT/rollback.sh' '$verified_snapshot'" >&2
        fi
        return "$status"
    fi
    if [[ $agent_was_active -eq 1 ]] && ! systemctl is-active --quiet celikpanel-agent.service; then
        systemctl start celikpanel-agent.service >/dev/null 2>&1 || true
    fi
    if [[ $panel_was_active -eq 1 ]] && ! systemctl is-active --quiet celikpanel-panel.service; then
        systemctl start celikpanel-panel.service >/dev/null 2>&1 || true
    fi
    if [[ $status -ne 0 ]]; then
        echo "!! Update stopped before installed bytes changed." >&2
        echo "!! Kurulu baytlar değişmeden önce güncelleme durdu." >&2
    fi
    return "$status"
}

# The snapshot describes the old installed artifacts. Their source commit is
# not inferred from the target release; the target has separate provenance.
# Snapshot eski kurulu ürünleri tanımlar. Kaynak commit hedef sürümden türetilmez;
# hedefin provenance bilgisi ayrı tutulur.
preserve_staging=0
if [[ $resume_quiescing_update -eq 1 || $resume_active_update -eq 1 ]]; then
    stamp=$resumable_snapshot_created_at
    snapshot_nonce=$resumable_snapshot_nonce
    snapshot_name=$resumable_snapshot
    snap="$SNAP_ROOT/$snapshot_name"
    stage_root=$resumable_stage
    tmp_snap="$stage_root/$snapshot_name"
    release_transaction_token=$resumable_token
    preserve_staging=1
    transaction_started=1
else
    stamp=$(date -u +%Y%m%dT%H%M%SZ)
    snapshot_nonce=$(od -An -N16 -tx1 /dev/urandom | tr -d '[:space:]')
    [[ "$snapshot_nonce" =~ ^[0-9a-f]{32}$ ]] \
        || die "cannot generate a safe snapshot nonce / güvenli snapshot nonce üretilemedi"
    snapshot_name="$stamp-from-unknown-to-$target_release_commit-$snapshot_nonce"
    snap="$SNAP_ROOT/$snapshot_name"
    stage_root="$SNAP_ROOT/.release-snapshot.incomplete.$BASHPID.$snapshot_nonce"
    tmp_snap="$stage_root/$snapshot_name"
    [[ ! -e "$snap" && ! -L "$snap" && ! -e "$stage_root" && ! -L "$stage_root" ]] \
        || die "snapshot path already exists / snapshot yolu zaten var"
    release_transaction_token=$(release_txn_generate_token) \
        || die "cannot generate release transaction token"
fi

# Before marker publication an exact generated stage may be removed on a caught
# failure. Once durable recovery material exists, every exit preserves it.
# Marker yayımlanmadan önce tam üretilmiş stage yakalanan hatada kaldırılabilir.
# Kalıcı kurtarma verisi oluştuğunda ise her çıkış onu korur.
cleanup_incomplete() {
    local relative owner group mode permissions entry
    [[ ${preserve_staging:-0} -eq 0 ]] || return 0
    [[ -n "${stage_root:-}" && ( -e "$stage_root" || -L "$stage_root" ) ]] || return 0
    [[ "$stage_root" == "$SNAP_ROOT/"* && -d "$stage_root" && ! -L "$stage_root" ]] || return 0
    relative=${stage_root#"$SNAP_ROOT/"}
    [[ -n "$relative" && "$relative" != */* &&
       "$relative" =~ ^\.release-snapshot\.incomplete\.[1-9][0-9]*\.[0-9a-f]{32}$ ]] || return 0
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$stage_root") || return 0
    [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] || return 0
    if find "$stage_root" -type l -print -quit | grep -q . ||
       find "$stage_root" ! -type d ! -type f -print -quit | grep -q .; then
        return 0
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$entry") || return 0
        permissions=$((8#$mode))
        [[ "$owner" == 0 ]] && (( (permissions & 0022) == 0 )) || return 0
    done < <(find "$stage_root" -mindepth 1 -print0)
    rm -rf -- "$stage_root" || return 0
    sync -f -- "$SNAP_ROOT" >/dev/null 2>&1 || true
}
classify_durable_update_marker() {
    local marker count=0 selected=none
    for marker in quiesce.pending active completion.pending; do
        if [[ -e "$RELEASE_TRANSACTION_ROOT/$marker" || -L "$RELEASE_TRANSACTION_ROOT/$marker" ]]; then
            count=$((count + 1))
            selected=$marker
        fi
    done
    [[ "$count" -le 1 ]] || return 1
    if [[ "$count" -eq 0 ]]; then
        printf '%s\n' none
        return 0
    fi
    case "$selected" in
        quiesce.pending)
            release_txn_validate_quiesce_token \
                "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
                "$SNAP_ROOT" "$stage_root" \
                || return 1
            printf '%s\n' quiesce
            ;;
        active)
            release_txn_validate_active_token \
                "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
                || return 1
            printf '%s\n' active
            ;;
        completion.pending)
            release_txn_validate_pending_token \
                "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
                || return 1
            printf '%s\n' completion
            ;;
        *) return 1 ;;
    esac
}

on_exit() {
    local status=$? final_status marker_phase
    final_status=$status
    trap - EXIT
    if ! marker_phase=$(classify_durable_update_marker); then
        preserve_staging=1
        stop_release_coordinators_fail_closed
        echo "!! Durable update marker is ambiguous or changed; recovery state was preserved." >&2
        exit 1
    fi
    case "$marker_phase" in
        quiesce)
            preserve_staging=1
            if [[ $mutation_started -eq 0 && $quiesce_abort_failed -eq 0 ]] && abort_quiesce_before_active; then
                [[ "$final_status" -ne 0 ]] || final_status=1
            else
                final_status=1
            fi
            ;;
        active|completion)
            preserve_staging=1
            stop_release_coordinators_fail_closed
            final_status=1
            ;;
        none)
            if [[ "$transaction_phase" == completion-removing && $transaction_completion_verified -eq 1 ]]; then
                # The exact marker disappeared only after all runtime and
                # enablement proofs passed while the common mutation lock was held.
                # Tam işaretçi ancak bütün çalışma zamanı ve
                # etkinleştirme kanıtları ortak mutasyon kilidi tutulurken geçtikten sonra kayboldu.
                transaction_started=0
                transaction_phase=none
                preserve_staging=0
                release_release_mutation_lock >/dev/null 2>&1 || true
            elif [[ "$transaction_phase" == quiesce-publishing ||
                    "$transaction_phase" == quiesce-removing ||
                    "$transaction_phase" == quiesce-removed ]] &&
                 [[ $quiesce_abort_failed -eq 0 && $mutation_started -eq 0 ]]; then
                transaction_started=0
                transaction_phase=none
                preserve_staging=0
                release_release_mutation_lock >/dev/null 2>&1 || true
                restart_previous_services "$final_status" || final_status=$?
            elif [[ $transaction_started -eq 1 || $mutation_started -eq 1 || $quiesce_abort_failed -eq 1 ]]; then
                preserve_staging=1
                stop_release_coordinators_fail_closed
                final_status=1
            else
                restart_previous_services "$final_status" || final_status=$?
            fi
            ;;
    esac
    cleanup_incomplete
    exit "$final_status"
}
trap on_exit EXIT

if [[ $resume_active_update -eq 1 ]]; then
    load_saved_service_states "$tmp_snap/service-states.tsv"
    load_quiesce_coordinator_identities "$tmp_snap/quiesce-coordinators.tsv"
    saved_transition_state=$(tr -d '[:space:]' < "$tmp_snap/snapshot-transition.state")
    [[ "$saved_transition_state" == "$snapshot_schema" ]] \
        || die "active update was staged for $saved_transition_state, not requested mode $snapshot_schema"
    release_txn_validate_active_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "active update marker changed before staging recovery"
    release_txn_reset_update_snapshot_stage "$SNAP_ROOT" "$snapshot_name" "$stage_root" \
        || die "cannot reset the canonical resumable snapshot staging payload"
    load_saved_service_states "$tmp_snap/service-states.tsv"
    load_quiesce_coordinator_identities "$tmp_snap/quiesce-coordinators.tsv"
    release_txn_validate_active_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "active update marker changed during staging recovery"
    echo "==> Resuming active pre-mutation snapshot transaction: $snapshot_name"
    echo "==> Active mutasyon-öncesi snapshot işlemi sürdürülüyor: $snapshot_name"
elif [[ $resume_quiescing_update -eq 1 ]]; then
    load_saved_service_states "$tmp_snap/service-states.tsv"
    load_quiesce_coordinator_identities "$tmp_snap/quiesce-coordinators.tsv"
    saved_transition_state=$(tr -d '[:space:]' < "$tmp_snap/snapshot-transition.state")
    [[ "$saved_transition_state" == "$snapshot_schema" ]] \
        || die "quiescing update was staged for $saved_transition_state, not requested mode $snapshot_schema"
    release_txn_validate_update_snapshot_stage "$SNAP_ROOT" "$snapshot_name" "$stage_root" \
        || die "quiescing update staging tree changed before recovery"
    release_txn_validate_quiesce_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || die "quiesce marker changed before recovery"
    # A recovered pre-active phase is safely aborted before a fresh retry. This
    # drains any queue row committed just before the previous panel freeze.
    # Kurtarılan active-öncesi aşama yeni denemeden önce güvenle iptal edilir. Bu,
    # önceki panel freeze işleminden hemen önce commit edilen queue satırını boşaltır.
    abort_quiesce_before_active \
        || die "quiesce recovery could not resume coordinators and remove the exact phase"
    die "previous quiesce phase was safely aborted and cleaned; rerun the exact trusted update"
else
    mkdir -m 0700 -- "$stage_root"
    chown root:root -- "$stage_root"
    mkdir -m 0700 -- "$tmp_snap"
    chown root:root -- "$tmp_snap"

    # Preserve exact enablement and runtime states before quiesce can block
    # starts or either coordinator can be frozen.
    # Quiesce başlangıçları engellemeden veya koordinatörlerden biri askıya alınmadan
    # önce tam enablement ve runtime durumlarını koru.
    : > "$tmp_snap/service-states.tsv"
    chown root:root -- "$tmp_snap/service-states.tsv"
    chmod 0600 -- "$tmp_snap/service-states.tsv"
    for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
        enabled_state=$(systemctl is-enabled "$unit" 2>/dev/null || true)
        active_state=$(systemctl is-active "$unit" 2>/dev/null || true)
        enabled_state=${enabled_state:-unknown}
        active_state=${active_state:-unknown}
        validate_service_active_state "$unit" "$active_state"
        case "$unit:$active_state" in
            celikpanel-agent.service:active|celikpanel-agent.service:activating|celikpanel-agent.service:reloading|celikpanel-agent.service:refreshing|celikpanel-panel.service:active|celikpanel-panel.service:activating|celikpanel-panel.service:reloading|celikpanel-panel.service:refreshing)
                initial_main_pid=$(systemctl show --property=MainPID --value "$unit") \
                    || die "cannot inspect initial coordinator MainPID: $unit"
                [[ "$initial_main_pid" =~ ^[0-9]+$ && "$initial_main_pid" -gt 1 ]] \
                    || die "coordinator has no stable MainPID before quiesce: $unit"
                initial_process_state=$(awk '/^State:/ {print $2}' "/proc/$initial_main_pid/status" 2>/dev/null || true)
                [[ -n "$initial_process_state" && "$initial_process_state" != T && "$initial_process_state" != t ]] \
                    || die "coordinator is already frozen before quiesce: $unit"
                ;;
        esac
        case "$enabled_state" in
            enabled|enabled-runtime|disabled|static|indirect|not-found) ;;
            *) die "unsupported enablement state for $unit: $enabled_state" ;;
        esac
        printf '%s\t%s\t%s\n' "$unit" "$enabled_state" "$active_state" >> "$tmp_snap/service-states.tsv"
    done
    printf '%s\n' "$snapshot_schema" > "$tmp_snap/snapshot-transition.state"
    chown root:root -- "$tmp_snap/snapshot-transition.state"
    chmod 0600 -- "$tmp_snap/snapshot-transition.state"
    load_saved_service_states "$tmp_snap/service-states.tsv"
    # Bind quiesce to the exact coordinator processes before publishing any
    # durable start barrier; recovery will accept only these identities.
    # Kalıcı başlangıç engeli yayımlanmadan önce quiesce işlemini tam koordinatör
    # süreçlerine bağla; kurtarma yalnızca bu kimlikleri kabul eder.
    capture_quiesce_coordinator_ledger "$tmp_snap/quiesce-coordinators.tsv"
    load_quiesce_coordinator_identities "$tmp_snap/quiesce-coordinators.tsv"
    release_txn_validate_update_snapshot_stage "$SNAP_ROOT" "$snapshot_name" "$stage_root" \
        || die "new snapshot staging recovery material is not canonical"
    sync -f -- "$tmp_snap/service-states.tsv" "$tmp_snap/quiesce-coordinators.tsv" \
        "$tmp_snap/snapshot-transition.state" \
        "$tmp_snap" "$stage_root" "$SNAP_ROOT" \
        || die "snapshot recovery material could not be made durable"

    # Preserve staging before atomic quiesce publication so a signal between
    # publication and local bookkeeping cannot destroy the only recovery path.
    # Atomik quiesce yayımından önce staging'i koru; yayım ile yerel kayıt
    # arasındaki bir sinyal tek kurtarma yolunu yok edemesin.
    preserve_staging=1
    transaction_started=1
    transaction_phase=quiesce-publishing
    release_txn_create_quiesce_marker \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || die "cannot publish quiesce update transaction marker"
    transaction_phase=quiesce
fi
firewall_enabled_state=${saved_enabled_states[celikpanel-firewall-restore.service]}

# Before active publication, a recoverable quiesce failure resumes the exact
# coordinators, removes the durable phase, and lets queued work drain.
# Active yayımından önce kurtarılabilir quiesce hatası tam koordinatörleri
# sürdürür, kalıcı aşamayı kaldırır ve queued işin boşalmasına izin verir.
fail_before_active() {
    local reason=$1
    if [[ "$transaction_phase" == quiesce ]]; then
        if abort_quiesce_before_active; then
            die "$reason; quiesce was safely aborted, rerun the exact trusted update"
        fi
        die "$reason; quiesce abort failed closed and the exact phase was preserved"
    fi
    die "$reason; active phase was preserved for exact recovery or explicit rollback"
}

# Preliminary checks reduce disruption. The panel proof is WAL-aware because a
# healthy coordinator may retain a non-empty SQLite WAL. It is repeated after
# both coordinators are frozen, which closes the panel enqueue race.
# Ön kontroller kesintiyi azaltır. Sağlıklı bir koordinatör dolu SQLite WAL
# tutabileceği için panel kanıtı WAL-aware'dır. Panel enqueue yarışını kapatmak
# üzere iki koordinatör de frozen olduktan sonra yeniden alınır.
prepare_runtime_mutation_lock_dir
if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    if [[ $BOOTSTRAP_SCHEMA17 -eq 1 ]]; then
        "$SCHEMA17_BRIDGE" check --db "$PANEL_DB" \
            || fail_before_active "panel is not at the exact supported schema version 17; schema17 bootstrap refused"
    elif ! CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware; then
        fail_before_active "panel is not at exact pre-ledger schema version 20; bootstrap refused"
    fi
    [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || fail_before_active "durable agent ledger already exists; pre-ledger bootstrap refused"
    if ! CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle; then
        fail_before_active "pre-ledger agent/package state is not idle; bootstrap refused"
    fi
else
    if ! CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware; then
        fail_before_active "panel service operations are not idle; update refused"
    fi
    if ! CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" --check-service-mutation-idle; then
        fail_before_active "agent/package mutations are not idle; update refused"
    fi
fi

acquire_release_mutation_lock
if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    if ! CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock; then
        fail_before_active "pre-ledger agent/package state changed before freeze"
    fi
else
    if ! CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock; then
        fail_before_active "agent/package state changed before freeze"
    fi
fi

if [[ "$transaction_phase" == quiesce ]]; then
    echo "==> Freezing panel and agent before the immutable final idle proof"
    echo "==> Değişmez son idle kanıtından önce panel ve agent askıya alınıyor"
    freeze_release_service_cgroup celikpanel-panel.service panel panel_frozen
    freeze_release_service_cgroup celikpanel-agent.service agent agent_frozen

    # These are the final mutable-state checks: the panel cannot enqueue another
    # row while frozen, and the agent cannot cross the held common flock.
    # Bunlar son değişebilir durum kontrolleridir: askıdaki panel yeni satır
    # ekleyemez ve agent elde tutulan ortak flock sınırını geçemez.
    if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
        if [[ $BOOTSTRAP_SCHEMA17 -eq 1 ]]; then
            "$SCHEMA17_BRIDGE" check --db "$PANEL_DB" \
                || fail_before_active "final frozen panel exact schema17 proof failed"
        elif ! CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware; then
            fail_before_active "final frozen panel pre-ledger idle proof failed"
        fi
        [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
            || fail_before_active "agent ledger appeared during pre-ledger freeze"
        if ! CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock; then
            fail_before_active "final frozen pre-ledger agent idle proof failed"
        fi
    else
        if ! CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware; then
            fail_before_active "final frozen panel idle proof failed"
        fi
        if ! CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
            CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
            "$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock; then
            fail_before_active "final frozen agent idle proof failed"
        fi
    fi

    # Reprove both exact identities immediately before marker promotion. The
    # helper also proves each active-like coordinator remains frozen alone.
    # Marker yükseltmesinden hemen önce iki tam kimliği yeniden kanıtla. Yardımcı,
    # aktif-benzeri her koordinatörün tek başına hâlâ askıda olduğunu da kanıtlar.
    verify_quiesce_coordinator_identity celikpanel-panel.service frozen \
        || die "panel identity changed before quiesce promotion"
    verify_quiesce_coordinator_identity celikpanel-agent.service frozen \
        || die "agent identity changed before quiesce promotion"

    # Atomic promotion has no start-guard gap. Both cgroups stay frozen until the
    # active marker is durable, then they are killed and proven empty.
    # Atomik yükseltme, başlangıç korumasında boşluk bırakmaz. Active marker kalıcı
    # olana kadar iki cgroup askıda kalır; ardından sonlandırılıp boş oldukları kanıtlanır.
    release_txn_validate_quiesce_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || die "quiesce marker changed after the final frozen idle proof"
    release_txn_promote_quiesce_to_active \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
        "$release_transaction_token" update "$snapshot_name" \
        "$SNAP_ROOT" "$stage_root" \
        || die "cannot atomically promote quiesce to active"
    transaction_phase=active
    terminate_frozen_release_service celikpanel-panel.service panel panel_frozen
    terminate_frozen_release_service celikpanel-agent.service agent agent_frozen
elif [[ "$transaction_phase" == active && $resume_active_update -eq 1 ]]; then
    # A retry never freezes a historical PID again. Validate the durable marker
    # around each idempotent coordinator recovery decision.
    # Yeniden deneme geçmişteki PID'yi tekrar askıya almaz. Her idempotent
    # koordinatör kurtarma kararının çevresinde kalıcı marker'ı doğrula.
    release_txn_validate_active_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "active marker changed before coordinator recovery"
    recover_active_release_service celikpanel-panel.service panel panel_frozen
    release_txn_validate_active_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "active marker changed after panel recovery"
    recover_active_release_service celikpanel-agent.service agent agent_frozen
    release_txn_validate_active_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "active marker changed after coordinator recovery"
else
    die "unexpected transaction phase before coordinator recovery: $transaction_phase"
fi

# Agent shutdown may remove RuntimeDirectory and unlink the locked inode. Rebuild
# the exact path, reacquire its new inode, and repeat stopped-state idle proofs.
# Agent kapanışı RuntimeDirectory'yi kaldırıp kilitli inode'u unlink edebilir.
# Tam yolu yeniden kur, yeni inode'u tekrar kilitle ve stopped-state idle kanıtlarını yinele.
release_release_mutation_lock || die "cannot release stale mutation lock after coordinator stop"
prepare_runtime_mutation_lock_dir
if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || die "durable agent ledger already exists after pre-ledger stop"
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle \
        || die "pre-ledger agent/package state changed while stopping"
else
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFLIGHT_AGENT" --check-service-mutation-idle \
        || die "agent/package mutations changed while stopping"
fi
acquire_release_mutation_lock
if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    if [[ $BOOTSTRAP_SCHEMA17 -eq 1 ]]; then
        "$SCHEMA17_BRIDGE" check --db "$PANEL_DB" \
            || die "stopped panel exact schema17 proof failed"
    else
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware \
            || die "stopped panel pre-ledger idle proof failed"
    fi
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock \
        || die "stopped pre-ledger agent idle proof failed"
else
    CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware \
        || die "stopped panel idle proof failed"
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock \
        || die "stopped agent idle proof failed"
fi

[[ -f "$PANEL_DB" ]] || die "panel database is missing: $PANEL_DB"
[[ -x "$BIN_DIR/panel" ]] || die "installed panel binary is missing"
[[ -x "$BIN_DIR/agent" ]] || die "installed agent binary is missing"
[[ -f "$WEB_DIR/index.html" ]] || die "installed web artifact is missing"
[[ -f "$UNIT_DIR/celikpanel-agent.service" ]] || die "installed agent unit is missing"
[[ -f "$UNIT_DIR/celikpanel-panel.service" ]] || die "installed panel unit is missing"

if [[ $BOOTSTRAP_SCHEMA17 -eq 1 ]]; then
    release_txn_validate_active_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "active marker changed before exact schema17 snapshot"
    "$SCHEMA17_BRIDGE" snapshot \
        --db "$PANEL_DB" \
        --out "$tmp_snap/$(basename "$PANEL_DB")" \
        || die "transaction-consistent exact schema17 panel database snapshot failed"
else
    if [[ $BOOTSTRAP_SCHEMA17 -eq 0 ]]; then
        prepare_recovery_snapshot_directory
        rescue_active_marker=$RELEASE_TRANSACTION_ROOT/active
        rescue_active_marker_identity=$(stat -Lc '%d:%i' -- "$rescue_active_marker") \
            || die "cannot identify active marker before durable rescue snapshot"
        rescue_active_marker_digest=$(sha256sum "$rescue_active_marker" | awk '{ print $1 }') \
            || die "cannot hash active marker before durable rescue snapshot"
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$TRUSTED_RELEASE_ROOT/bin/panel" \
            --ensure-service-operation-rescue-snapshot="$rescue_snapshot" \
            --snapshot-schema="$snapshot_schema" \
            --release-transaction-fd="$RELEASE_TRANSACTION_FD" \
            --release-transaction-token="$release_transaction_token" \
            --release-transaction-operation=update \
            --release-transaction-snapshot="$snapshot_name" \
            || die "transaction-consistent durable recovery snapshot failed; recovery path was retained"
        release_txn_verify_inherited_lock "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
            || die "persistent release lock changed during durable recovery snapshot"
        flock -n -x "$MUTATION_LOCK_FD" \
            || die "service mutation lock changed during durable recovery snapshot"
        release_txn_validate_active_token \
            "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
            || die "active marker changed during durable recovery snapshot"
        [[ "$(stat -Lc '%d:%i' -- "$rescue_active_marker")" == "$rescue_active_marker_identity" ]] \
            || die "active marker identity changed during durable recovery snapshot"
        [[ "$(sha256sum "$rescue_active_marker" | awk '{ print $1 }')" == "$rescue_active_marker_digest" ]] \
            || die "active marker bytes changed during durable recovery snapshot"
        verify_quiesce_coordinator_stopped celikpanel-panel.service \
            || die "panel coordinator changed during durable recovery snapshot"
        verify_quiesce_coordinator_stopped celikpanel-agent.service \
            || die "agent coordinator changed during durable recovery snapshot"
        if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
            [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
                || die "agent ledger appeared during pre-ledger durable recovery snapshot"
            CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
                CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
                "$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock \
                || die "pre-ledger agent/package state changed during durable recovery snapshot"
            CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
                "$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware \
                || die "pre-ledger panel service-operation state changed during durable recovery snapshot"
        else
            CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
                CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
                "$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock \
                || die "agent/package mutation state changed during durable recovery snapshot"
            CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
                "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware \
                || die "panel service-operation state changed during durable recovery snapshot"
        fi
        verify_recovery_snapshot "$rescue_snapshot"
        sync -f -- "$rescue_snapshot" "$recovery_snapshot_dir" \
            || die "durable recovery snapshot could not be synchronized"
    fi
    CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" \
        --create-service-operation-snapshot="$tmp_snap/$(basename "$PANEL_DB")" \
        --snapshot-schema="$snapshot_schema" \
        --release-transaction-fd="$RELEASE_TRANSACTION_FD" \
        --release-transaction-token="$release_transaction_token" \
        --release-transaction-operation=update \
        --release-transaction-snapshot="$snapshot_name" \
        || die "transaction-consistent panel database snapshot failed"
fi
[[ -f "$tmp_snap/$(basename "$PANEL_DB")" && ! -L "$tmp_snap/$(basename "$PANEL_DB")" ]] \
    || die "online panel database snapshot is missing or unsafe"
[[ ! -e "$tmp_snap/$(basename "$PANEL_DB")-wal" && \
   ! -L "$tmp_snap/$(basename "$PANEL_DB")-wal" && \
   ! -e "$tmp_snap/$(basename "$PANEL_DB")-shm" && \
   ! -L "$tmp_snap/$(basename "$PANEL_DB")-shm" && \
   ! -e "$tmp_snap/$(basename "$PANEL_DB")-journal" && \
   ! -L "$tmp_snap/$(basename "$PANEL_DB")-journal" ]] \
    || die "online panel database snapshot must be standalone without WAL/SHM/journal"

# Only the service-mutation ledger belongs to the release transaction. Other
# agent state (for example ACME challenges) remains untouched.
# Sürüm işlemine yalnız servis-işlem ledger'ı dahildir. Diğer agent durumu
# (örneğin ACME challenge'ları) olduğu gibi bırakılır.
mkdir "$tmp_snap/agent-state"
printf '%s\n' "$AGENT_STATE_DIR" > "$tmp_snap/agent-state-root"
if [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
    cp -a "$AGENT_LEDGER" "$tmp_snap/agent-state/"
    printf 'present\n' > "$tmp_snap/agent-ledger.state"
elif [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
    printf 'absent\n' > "$tmp_snap/agent-ledger.state"
else
    die "agent service-mutation ledger is unsafe: $AGENT_LEDGER"
fi

# Record the snapshot trust state explicitly. A pre-ledger snapshot also keeps
# the exact verified checker binaries needed to classify a partially completed
# upgrade during rollback; every file is covered by the complete manifest.
# Snapshot güven durumunu açıkça kaydet. Ledger öncesi snapshot ayrıca geri alma
# sırasında kısmen tamamlanmış yükseltmeyi sınıflandırmak için gereken tam
# doğrulanmış checker binary'lerini saklar; her dosyayı eksiksiz manifest kapsar.
if [[ $BOOTSTRAP_SCHEMA17 -eq 1 ]]; then
    printf 'schema17\n' | cmp -s - "$tmp_snap/snapshot-transition.state" \
        || die "schema17 transition state changed after active marker publication"
    mkdir "$tmp_snap/transition-preflight"
    cp -a "$PREFLIGHT_PANEL" "$tmp_snap/transition-preflight/panel"
    cp -a "$PREFLIGHT_AGENT" "$tmp_snap/transition-preflight/agent"
    cp -a "$SCHEMA17_BRIDGE" "$tmp_snap/transition-preflight/schema17-bridge"
    printf '%s\t%s\n' \
        transition-version 1 \
        mode bootstrap-schema17 \
        source-schema-version 17 \
        bridge-schema-version 20 \
        target-release-commit "$trusted_release_commit" \
        target-release-tree "$trusted_release_tree" \
        agent-state-root "$AGENT_STATE_DIR" \
        created-at-utc "$stamp" \
        > "$tmp_snap/schema17-transition.tsv"
    (
        cd "$tmp_snap"
        sha256sum schema17-transition.tsv > schema17-transition.sha256
        sha256sum -c schema17-transition.sha256 >/dev/null
    ) || die "schema17 transition marker checksum failed"
elif [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    printf 'pre-ledger\n' | cmp -s - "$tmp_snap/snapshot-transition.state" \
        || die "pre-ledger transition state changed after active marker publication"
    mkdir "$tmp_snap/transition-preflight"
    cp -a "$PREFLIGHT_PANEL" "$tmp_snap/transition-preflight/panel"
    cp -a "$PREFLIGHT_AGENT" "$tmp_snap/transition-preflight/agent"
    printf '%s\t%s\n' \
        transition-version 1 \
        mode bootstrap-pre-ledger \
        source-schema-version 20 \
        target-release-commit "$trusted_release_commit" \
        target-release-tree "$trusted_release_tree" \
        agent-state-root "$AGENT_STATE_DIR" \
        created-at-utc "$stamp" \
        > "$tmp_snap/pre-ledger-transition.tsv"
    (
        cd "$tmp_snap"
        sha256sum pre-ledger-transition.tsv > pre-ledger-transition.sha256
        sha256sum -c pre-ledger-transition.sha256 >/dev/null
    ) || die "pre-ledger transition marker checksum failed"
else
    printf 'normal\n' | cmp -s - "$tmp_snap/snapshot-transition.state" \
        || die "normal transition state changed after active marker publication"
fi
cp -a "$BIN_DIR" "$tmp_snap/bin"
cp -a "$WEB_DIR" "$tmp_snap/web"
mkdir "$tmp_snap/units"
cp -a "$UNIT_DIR/celikpanel-agent.service" "$tmp_snap/units/"
cp -a "$UNIT_DIR/celikpanel-panel.service" "$tmp_snap/units/"

if [[ -f "$UNIT_DIR/celikpanel-firewall-restore.service" ]]; then
    [[ "$firewall_enabled_state" != not-found ]] || die "firewall unit exists but systemd reports not-found"
    cp -a "$UNIT_DIR/celikpanel-firewall-restore.service" "$tmp_snap/units/"
    printf 'present\n' > "$tmp_snap/firewall-unit.state"
else
    [[ "$firewall_enabled_state" == not-found ]] || die "absent firewall unit has inconsistent enablement state: $firewall_enabled_state"
    printf 'absent\n' > "$tmp_snap/firewall-unit.state"
fi

# A verified snapshot cannot contain a symlink or special object: restore must
# hash and copy the same privileged regular files.
# Doğrulanmış snapshot sembolik bağlantı veya özel nesne içeremez: geri alma
# aynı root yetkili normal dosyaları hem özetlemeli hem kopyalamalıdır.
if find "$tmp_snap" -type l -print -quit | grep -q .; then
    die "snapshot payload contains a symbolic link / snapshot ürünü sembolik bağlantı içeriyor"
fi
if find "$tmp_snap" ! -type d ! -type f -print -quit | grep -q .; then
    die "snapshot payload contains a special filesystem object / snapshot ürünü özel dosya sistemi nesnesi içeriyor"
fi

printf '%s\n' "$SNAPSHOT_VERSION" > "$tmp_snap/snapshot.version"
printf '%s\n' "$source_commit" > "$tmp_snap/commit"
printf '%s\n' "$target_release_commit" > "$tmp_snap/target-release.commit"
printf '%s\n' "$target_release_tree" > "$tmp_snap/target-release.tree"
printf '%s\n' "$stamp" > "$tmp_snap/created-at-utc"

# Hash every payload file. A rollback will verify this manifest before it
# stops a service or replaces a byte.
# Her ürün dosyasını özetle. Geri alma bir servisi durdurmadan veya tek bayt
# değiştirmeden önce bu manifesti doğrular.
(
    cd "$tmp_snap"
    LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 sha256sum > SHA256SUMS
    sha256sum -c SHA256SUMS >/dev/null
)
find "$tmp_snap" -type f -exec sync -f -- {} \; \
    || die "snapshot payload files could not be made durable"
find "$tmp_snap" -depth -type d -exec sync -f -- {} \; \
    || die "snapshot payload directories could not be made durable"
sync -f -- "$stage_root" || die "snapshot staging root could not be made durable"
mv -T --no-clobber -- "$tmp_snap" "$snap" \
    || die "snapshot publish failed / snapshot yayımlanamadı"
sync -f -- "$SNAP_ROOT" || die "snapshot publish rename could not be made durable"
[[ ! -e "$tmp_snap" && -d "$snap" ]] \
    || die "snapshot publish collision / snapshot yayımlama çakışması"
rmdir -- "$stage_root" || die "snapshot staging root is not empty after publish"
sync -f -- "$SNAP_ROOT" || die "snapshot staging cleanup could not be made durable"
tmp_snap=""
stage_root=""
preserve_staging=0
validate_root_trusted_dir_chain "$snap"
verified_snapshot=$snap
if [[ $BOOTSTRAP_SCHEMA17 -eq 0 ]]; then
    retire_recovery_snapshot_after_verified_publish
fi
echo "==> Verified rollback snapshot / Doğrulanmış geri alma snapshot'ı: $snap"

# Retain five complete v4 snapshots. Older snapshot formats remain untouched:
# a new updater must never block or destroy the last rollback path merely
# because its own snapshot contract advanced.
# Beş eksiksiz v4 snapshot sakla. Eski snapshot biçimlerine dokunma: yeni bir
# updater yalnız kendi snapshot sözleşmesi ilerledi diye son geri alma yolunu
# engellememeli veya yok etmemelidir.
mapfile -t snapshot_candidates < <(
    find "$SNAP_ROOT" -mindepth 1 -maxdepth 1 -type d ! -name '.*' \
        -printf '%T@ %p\n' | LC_ALL=C sort -nr | cut -d' ' -f2-
)
current_snapshots=()
for snapshot_candidate in "${snapshot_candidates[@]}"; do
    if [[ ! -f "$snapshot_candidate/snapshot.version" || -L "$snapshot_candidate/snapshot.version" ]]; then
        echo "==> Preserving snapshot with unknown format: $snapshot_candidate"
        echo "==> Biçimi bilinmeyen snapshot korunuyor: $snapshot_candidate"
        continue
    fi
    candidate_version=$(tr -d '[:space:]' < "$snapshot_candidate/snapshot.version")
    if [[ "$candidate_version" != "$SNAPSHOT_VERSION" ]]; then
        echo "==> Preserving older snapshot format v$candidate_version: $snapshot_candidate"
        echo "==> Eski snapshot biçimi v$candidate_version korunuyor: $snapshot_candidate"
        continue
    fi
    current_snapshots+=("$snapshot_candidate")
done
old_snapshots=("${current_snapshots[@]:$KEEP_SNAPSHOTS}")
for old_snapshot in "${old_snapshots[@]}"; do
    validate_retention_snapshot "$old_snapshot"
    rm -rf -- "$old_snapshot"
done

# Both modes are pinned to the verified Git archive; update.sh never executes
# root code from a mutable checkout or advances Git itself.
# İki mod da doğrulanmış Git arşivine sabittir; update.sh değişebilir bir
# checkout'tan root kodu çalıştırmaz veya Git'i kendisi ilerletmez.
echo "==> Trusted release is pinned; Git pull is unavailable / Güvenilir sürüm sabit; Git pull kullanılamaz"

echo "==> Re-running the staged installer / Staged kurucu yeniden çalıştırılıyor"
current_listen=$(grep -h '^Environment=CELIKPANEL_LISTEN=' "$UNIT_DIR/celikpanel-panel.service" 2>/dev/null | cut -d= -f3- || true)
current_listen=${current_listen:-:2083}
if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    # Release the outer flock only for the trusted one-shot initializer. Prove
    # its exact empty ledger, recreate and reacquire the common lock, then run
    # the installer with initialization disabled so no request can enter the
    # agent between initialization and installation.
    # Dış flock'u yalnız güvenilir tek seferlik initializer için bırak. Tam boş
    # ledger'ı kanıtla, ortak kilidi yeniden oluşturup al ve initialization
    # kapalı kurucuyu çalıştır; böylece arada agent isteği giremez.
    if [[ $BOOTSTRAP_SCHEMA17 -eq 1 ]]; then
        release_txn_validate_active_token \
            "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
            || die "active marker changed before exact schema17 bridge migration"
        [[ "$verified_snapshot" == "$snap" ]] \
            || die "exact schema17 bridge requires the published verified snapshot"
        mutation_started=1
        "$SCHEMA17_BRIDGE" migrate \
            --db "$PANEL_DB" \
            --migrations-root "$TRUSTED_RELEASE_ROOT/internal/db/migrations" \
            || die "exact schema17 to schema20 bridge migration failed; use the printed trusted rollback command"
        CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
            "$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle \
            || die "schema17 bridge did not produce the exact idle schema20 state"
    fi
    release_release_mutation_lock || die "cannot release bootstrap mutation lock before ledger initialization"
    mutation_started=1
    env -i \
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
        HOME=/root LC_ALL=C \
        CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \
        CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$TRUSTED_RELEASE_ROOT/bin/agent" --initialize-service-mutation-ledger \
        || die "trusted bootstrap ledger initializer failed"
    prepare_runtime_mutation_lock_dir
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$TRUSTED_RELEASE_ROOT/bin/agent" --check-initial-service-mutation-ledger \
        || die "bootstrap ledger is not the exact canonical empty initial ledger"
    acquire_release_mutation_lock
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$TRUSTED_RELEASE_ROOT/bin/agent" --check-initial-service-mutation-ledger-under-external-lock \
        || die "bootstrap ledger changed before the locked installation"
    env -i \
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
        HOME=/root LC_ALL=C \
        SKIP_DEPS=1 SKIP_SECURITY_UPDATES=1 SKIP_ADMIN=1 \
        INITIALIZE_SERVICE_MUTATION_LEDGER=0 CELIKPANEL_APPLY_ONLY=1 \
        CELIKPANEL_TRUSTED_RELEASE_ROOT="$TRUSTED_RELEASE_ROOT" \
        CELIKPANEL_RELEASE_TRANSACTION_FD="$RELEASE_TRANSACTION_FD" \
        CELIKPANEL_RELEASE_TRANSACTION_TOKEN="$release_transaction_token" \
        CELIKPANEL_RELEASE_TRANSACTION_OPERATION=update \
        CELIKPANEL_RELEASE_TRANSACTION_SNAPSHOT="$snapshot_name" \
        LISTEN="$current_listen" \
        /bin/bash "$TRUSTED_RELEASE_ROOT/install.sh"
else
    mutation_started=1
    env -i \
        PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin \
        HOME=/root LC_ALL=C \
        SKIP_DEPS=1 SKIP_SECURITY_UPDATES=1 SKIP_ADMIN=1 \
        INITIALIZE_SERVICE_MUTATION_LEDGER=0 CELIKPANEL_APPLY_ONLY=1 \
        CELIKPANEL_TRUSTED_RELEASE_ROOT="$TRUSTED_RELEASE_ROOT" \
        CELIKPANEL_RELEASE_TRANSACTION_FD="$RELEASE_TRANSACTION_FD" \
        CELIKPANEL_RELEASE_TRANSACTION_TOKEN="$release_transaction_token" \
        CELIKPANEL_RELEASE_TRANSACTION_OPERATION=update \
        CELIKPANEL_RELEASE_TRANSACTION_SNAPSHOT="$snapshot_name" \
        LISTEN="$current_listen" \
        /bin/bash "$TRUSTED_RELEASE_ROOT/install.sh"
fi

# Apply-only returns with both coordinators stopped. Verify durable installed
# state before authorizing either unit to start.
# Apply-only iki koordinatör kapalıyken döner. İki unit'ten birini başlatmaya
# yetki vermeden önce dayanıklı kurulu durumu doğrula.
for unit in celikpanel-agent.service celikpanel-panel.service; do
    installed_state=$(systemctl show --property=ActiveState --value "$unit") \
        || die "cannot inspect apply-only service state: $unit"
    [[ "$installed_state" == inactive || "$installed_state" == failed ]] \
        || die "apply-only unexpectedly left $unit active"
done
verify_saved_enablement

# Every mode succeeds only when installed binaries and web exactly match the
# staged release and both installed durable coordinators prove idle.
# Her mod yalnız kurulu binary'ler ile web staged sürümle tam eşleştiğinde ve
# kurulu iki kalıcı koordinatör boşta olduğunu kanıtladığında başarılıdır.
verify_installed_release_artifacts

if [[ $BOOTSTRAP_PRE_LEDGER -eq 1 ]]; then
    CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware \
        || die "pre-ledger panel database changed before the controlled start"
else
    CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
        "$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware \
        || die "installed panel durable ledger is not ready before controlled start"
fi
CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
    CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
    "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
    || die "installed agent durable ledger is not ready under the release lock"
find "$BIN_DIR" "$WEB_DIR" -type f -exec sync -f -- {} \; \
    || die "installed release files could not be made durable"
sync -f -- "$BIN_DIR" "$WEB_DIR" "$PANEL_DB" \
    "$UNIT_DIR/celikpanel-agent.service" "$UNIT_DIR/celikpanel-panel.service" "$UNIT_DIR" \
    || die "installed release directories could not be made durable"

# Move active to completion.pending only after the stopped-state durable proof;
# then publish process-bound authorization for the exact controlled starts.
# Active işaretçisini yalnız kapalı-durum dayanıklılık kanıtından sonra
# completion.pending'e taşı; sonra tam kontrollü başlangıçlar için süreç bağlı yetki yayımla.
release_txn_mark_completion_pending \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$release_transaction_token" update "$snapshot_name" \
    || die "cannot mark update completion pending"
run_panel_migrations_offline
verify_installed_release_artifacts
verify_saved_enablement
release_txn_validate_pending_token \
    "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
    || die "update completion marker changed before controlled starts"
if service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
    prepare_fresh_agent_socket_start
fi
release_txn_create_start_authorization \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$RELEASE_TRANSACTION_FD" "$release_transaction_token" update "$snapshot_name" \
    || die "cannot authorize controlled update starts"
if service_state_is_active_like "${saved_active_states[celikpanel-agent.service]}"; then
    release_release_mutation_lock || die "cannot hand the mutation lock to the verified agent"
    systemctl start celikpanel-agent.service || die "verified agent could not be started"
    wait_for_fresh_active_agent
    acquire_release_mutation_lock handoff
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
        "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
        || die "verified agent state is not idle after the startup lock handoff"
    verify_installed_release_artifacts
    verify_saved_enablement
    release_txn_validate_pending_token \
        "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
        || die "update completion marker changed during the startup lock handoff"
fi
if service_state_is_active_like "${saved_active_states[celikpanel-panel.service]}"; then
    systemctl start celikpanel-panel.service || die "verified panel could not be started"
    systemctl is-active --quiet celikpanel-panel.service || die "verified panel is not running"
fi
verify_saved_runtime_states
CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
    CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
    "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
    || die "installed agent ledger changed during controlled starts"
verify_saved_enablement
release_txn_remove_start_authorization \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
    "$RELEASE_TRANSACTION_FD" "$release_transaction_token" update "$snapshot_name" \
    || die "cannot remove controlled update start authorization"
verify_saved_runtime_states
CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
    CELIKPANEL_MUTATION_LOCK_FD="$MUTATION_LOCK_FD" \
    "$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock \
    || die "installed agent durable ledger is not ready before completion"
verify_saved_enablement
release_txn_validate_pending_token \
    "$RELEASE_TRANSACTION_ROOT" "$release_transaction_token" update "$snapshot_name" \
    || die "update completion marker changed before durable removal"
transaction_completion_verified=1
transaction_phase=completion-removing
release_txn_remove_completion_pending \
    "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD" \
    "$release_transaction_token" update "$snapshot_name" \
    || die "cannot durably complete update transaction"
transaction_started=0
release_release_mutation_lock || die "cannot release update mutation lock"

trap - EXIT
transaction_phase=none
echo
echo "==> Update complete / Güncelleme tamamlandı"
echo "    Verified rollback snapshot / Doğrulanmış geri alma snapshot'ı: $snap"
echo "    Roll back if needed / Gerekirse geri alın: sudo /bin/bash '$TRUSTED_RELEASE_ROOT/rollback.sh' '$snap'"
