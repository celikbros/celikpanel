#!/usr/bin/env bash
# CelikPanel update: capture and verify a complete rollback snapshot before the
# source tree or installed artifacts are changed.
#
# CelikPanel güncellemesi: kaynak ağacı veya kurulu ürünler değiştirilmeden önce
# eksiksiz bir geri alma snapshot'ı alır ve doğrular.
set -euo pipefail

SNAPSHOT_VERSION=3
SNAP_ROOT=/var/backups/celikpanel/update-snapshots
PANEL_DB=/var/lib/celikpanel/celikpanel.db
BIN_DIR=/opt/celikpanel/bin
WEB_DIR=/opt/celikpanel/web
UNIT_DIR=/etc/systemd/system
AGENT_STATE_DIR=/var/lib/celikpanel-agent
AGENT_LEDGER="$AGENT_STATE_DIR/service-mutations.json"
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
PREFLIGHT_PANEL="${CELIKPANEL_PREFLIGHT_PANEL:-$BIN_DIR/panel}"
PREFLIGHT_AGENT="${CELIKPANEL_PREFLIGHT_AGENT:-$BIN_DIR/agent}"
KEEP_SNAPSHOTS=5

die() {
    echo "!! $*" >&2
    exit 1
}

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
    local directory desired_mode
    validate_root_trusted_dir_chain /var
    for directory in /var/backups /var/backups/celikpanel "$SNAP_ROOT"; do
        desired_mode=0700
        [[ "$directory" != /var/backups ]] || desired_mode=0755
        if [[ -e "$directory" || -L "$directory" ]]; then
            [[ -d "$directory" && ! -L "$directory" ]] || die "unsafe snapshot directory: $directory"
        else
            install -d -m "$desired_mode" -o root -g root -- "$directory" || die "cannot create snapshot directory: $directory"
        fi
        validate_root_trusted_dir_chain "$directory"
    done
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

# Snapshot runtime state accepts only documented systemd ActiveState values.
# Snapshot çalışma durumu yalnız belgelenmiş systemd ActiveState değerlerini kabul eder.
validate_service_active_state() {
    local unit=$1 state=$2
    case "$state" in
        active|activating|reloading|refreshing|inactive|failed|deactivating|maintenance) ;;
        *) die "unsupported active state for $unit: $state" ;;
    esac
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
    [[ -f "$snapshot/snapshot.version" && ! -L "$snapshot/snapshot.version" ]] \
        || die "retention snapshot version is missing or unsafe: $snapshot"
    version=$(tr -d '[:space:]' < "$snapshot/snapshot.version")
    [[ "$version" == "$SNAPSHOT_VERSION" ]] \
        || die "unsupported retention snapshot version at $snapshot: $version"
    [[ -f "$snapshot/SHA256SUMS" && ! -L "$snapshot/SHA256SUMS" ]] \
        || die "retention snapshot checksum manifest is missing or unsafe: $snapshot"
    (
        cd "$snapshot"
        sha256sum -c SHA256SUMS >/dev/null
    ) || die "retention snapshot checksum verification failed: $snapshot"
}

# The shell holds the same flock used by privileged agent mutations from the
# final idle proof until the updated services have been verified.
# Shell, son boşta doğrulamasından güncel servisler doğrulanana kadar ayrıcalıklı
# agent işlemleriyle aynı flock kilidini tutar.
acquire_release_mutation_lock() {
    local lock_dir owner mode permissions before after
    command -v flock >/dev/null || die "flock is required for a safe update"
    lock_dir=$(dirname "$MUTATION_LOCK")
    validate_root_trusted_dir_chain "$lock_dir"
    [[ -d "$lock_dir" && ! -L "$lock_dir" ]] || die "unsafe mutation lock directory: $lock_dir"
    read -r owner mode < <(stat -Lc '%u %a' -- "$lock_dir") || die "cannot inspect mutation lock directory"
    [[ "$owner" == 0 ]] || die "mutation lock directory must be owned by root"
    permissions=$((8#$mode))
    (( (permissions & 0022) == 0 )) || die "mutation lock directory must not be group/other writable"
    if [[ -e "$MUTATION_LOCK" || -L "$MUTATION_LOCK" ]]; then
        [[ -f "$MUTATION_LOCK" && ! -L "$MUTATION_LOCK" ]] || die "unsafe mutation lock file"
    else
        (umask 077; : > "$MUTATION_LOCK") || die "cannot create mutation lock file"
    fi
    read -r owner mode < <(stat -Lc '%u %a' -- "$MUTATION_LOCK") || die "cannot inspect mutation lock file"
    [[ "$owner" == 0 ]] || die "mutation lock file must be owned by root"
    permissions=$((8#$mode))
    (( (permissions & 0077) == 0 )) || die "mutation lock file must be mode 0600 or stricter"
    before=$(stat -Lc '%d:%i' -- "$MUTATION_LOCK") || die "cannot identify mutation lock file"
    exec {MUTATION_LOCK_FD}<>"$MUTATION_LOCK"
    after=$(stat -Lc '%d:%i' -- "/proc/$$/fd/$MUTATION_LOCK_FD") || die "cannot identify opened mutation lock"
    [[ "$before" == "$after" ]] || die "mutation lock changed while it was opened"
    flock -n "$MUTATION_LOCK_FD" || die "a service/package mutation is active; update refused"
}

if [[ $EUID -ne 0 ]]; then
    die "Run as root / root olarak çalıştırın: sudo ./update.sh"
fi

cd "$(dirname "$0")"
umask 077
prepare_snapshot_root

validate_preflight_binary "$PREFLIGHT_PANEL" panel
validate_preflight_binary "$PREFLIGHT_AGENT" agent

panel_was_active=0
if systemctl is-active --quiet celikpanel-panel.service; then
    panel_was_active=1
fi
agent_was_active=0
if systemctl is-active --quiet celikpanel-agent.service; then
    agent_was_active=1
fi

# If snapshot creation or the build fails, do not leave a previously running
# panel stopped. This does not hide the update failure.
# Snapshot alma veya derleme başarısız olursa daha önce çalışan paneli kapalı
# bırakma. Bu işlem güncelleme hatasını gizlemez.
restart_previous_services() {
    local status=$1
    if [[ $agent_was_active -eq 1 ]] && ! systemctl is-active --quiet celikpanel-agent.service; then
        systemctl start celikpanel-agent.service >/dev/null 2>&1 || true
    fi
    if [[ $panel_was_active -eq 1 ]] && ! systemctl is-active --quiet celikpanel-panel.service; then
        systemctl start celikpanel-panel.service >/dev/null 2>&1 || true
    fi
    if [[ $status -ne 0 ]]; then
        echo "!! Update stopped. The verified snapshot, if announced below, can be used with rollback.sh." >&2
        echo "!! Güncelleme durdu. Aşağıda doğrulandığı bildirilen snapshot rollback.sh ile kullanılabilir." >&2
    fi
    return "$status"
}

source_commit=unknown
short_commit=unknown
if [[ -d .git ]]; then
    source_commit=$(git rev-parse HEAD) || die "cannot read source commit / kaynak commit okunamadı"
    short_commit=$(git rev-parse --short HEAD) || die "cannot read short commit / kısa commit okunamadı"
fi

stamp=$(date -u +%Y%m%dT%H%M%SZ)
snap="$SNAP_ROOT/$stamp-$short_commit"
tmp_snap="$SNAP_ROOT/.$stamp-$short_commit.incomplete.$$"
[[ ! -e "$snap" && ! -e "$tmp_snap" ]] || die "snapshot path already exists / snapshot yolu zaten var"
mkdir -m 0700 "$tmp_snap"

# Preserve enablement and runtime state before the panel is deliberately
# stopped for the SQLite copy.
# SQLite kopyası için panel bilerek durdurulmadan önce etkinleştirme ve çalışma
# durumunu kaydet.
: > "$tmp_snap/service-states.tsv"
for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
    enabled_state=$(systemctl is-enabled "$unit" 2>/dev/null || true)
    active_state=$(systemctl is-active "$unit" 2>/dev/null || true)
    enabled_state=${enabled_state:-unknown}
    active_state=${active_state:-unknown}
    validate_service_active_state "$unit" "$active_state"
    printf '%s\t%s\t%s\n' "$unit" "$enabled_state" "$active_state" >> "$tmp_snap/service-states.tsv"
    case "$enabled_state" in
        enabled|enabled-runtime|disabled|static|indirect|not-found) ;;
        *) die "unsupported enablement state for $unit: $enabled_state" ;;
    esac
    if [[ "$unit" == celikpanel-firewall-restore.service ]]; then
        firewall_enabled_state=$enabled_state
    fi
done

cleanup_incomplete() {
    if [[ -d "$tmp_snap" ]]; then
        rm -rf -- "$tmp_snap"
    fi
}
on_exit() {
    local status=$?
    cleanup_incomplete
    restart_previous_services "$status"
}
trap on_exit EXIT

echo "==> Stopping panel for a consistent SQLite snapshot"
echo "==> Tutarlı SQLite snapshot'ı için panel durduruluyor"
systemctl stop celikpanel-panel.service || die "panel could not be stopped / panel durdurulamadı"
if systemctl is-active --quiet celikpanel-panel.service; then
    die "panel is still active; snapshot refused / panel hâlâ çalışıyor; snapshot reddedildi"
fi

# The stopped panel proves its durable queue is empty. The staged/current agent
# then proves its privileged ledger, host package manager and flock are idle.
# Durdurulan panel kalıcı kuyruğunun boş olduğunu kanıtlar. Ardından staging/geçerli
# agent; ayrıcalıklı ledger, sistem paket yöneticisi ve flock'un boşta olduğunu kanıtlar.
CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
    "$PREFLIGHT_PANEL" --check-service-operations-idle \
    || die "panel service operations are not idle; update refused. For a first upgrade, point CELIKPANEL_PREFLIGHT_PANEL at the verified staged panel binary"
CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
    "$PREFLIGHT_AGENT" --check-service-mutation-idle \
    || die "agent/package mutations are not idle; update refused. A pre-ledger release remains blocked until a reviewed bootstrap transition exists"
acquire_release_mutation_lock

systemctl stop celikpanel-agent.service || die "agent could not be stopped / agent durdurulamadı"
if systemctl is-active --quiet celikpanel-agent.service; then
    die "agent is still active; snapshot refused / agent hâlâ çalışıyor; snapshot reddedildi"
fi

[[ -f "$PANEL_DB" ]] || die "panel database is missing: $PANEL_DB"
[[ -x "$BIN_DIR/panel" ]] || die "installed panel binary is missing"
[[ -x "$BIN_DIR/agent" ]] || die "installed agent binary is missing"
[[ -f "$WEB_DIR/index.html" ]] || die "installed web artifact is missing"
[[ -f "$UNIT_DIR/celikpanel-agent.service" ]] || die "installed agent unit is missing"
[[ -f "$UNIT_DIR/celikpanel-panel.service" ]] || die "installed panel unit is missing"

cp -a "$PANEL_DB" "$tmp_snap/"
[[ ! -f "$PANEL_DB-wal" ]] || cp -a "$PANEL_DB-wal" "$tmp_snap/"
[[ ! -f "$PANEL_DB-shm" ]] || cp -a "$PANEL_DB-shm" "$tmp_snap/"

# Only the service-mutation ledger belongs to the release transaction. Other
# agent state (for example ACME challenges) remains untouched.
# Sürüm işlemine yalnız servis-işlem ledger'ı dahildir. Diğer agent durumu
# (örneğin ACME challenge'ları) olduğu gibi bırakılır.
mkdir "$tmp_snap/agent-state"
if [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
    cp -a "$AGENT_LEDGER" "$tmp_snap/agent-state/"
    printf 'present\n' > "$tmp_snap/agent-ledger.state"
elif [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
    printf 'absent\n' > "$tmp_snap/agent-ledger.state"
else
    die "agent service-mutation ledger is unsafe: $AGENT_LEDGER"
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

# A verified snapshot cannot contain a symlink: restore must hash and copy the
# same privileged object.
# Doğrulanmış snapshot sembolik bağlantı içeremez: geri alma aynı root yetkili
# nesneyi hem özetlemeli hem kopyalamalıdır.
if find "$tmp_snap" -type l -print -quit | grep -q .; then
    die "snapshot payload contains a symbolic link / snapshot ürünü sembolik bağlantı içeriyor"
fi

printf '%s\n' "$SNAPSHOT_VERSION" > "$tmp_snap/snapshot.version"
printf '%s\n' "$source_commit" > "$tmp_snap/commit"
printf '%s\n' "$stamp" > "$tmp_snap/created-at-utc"

# Hash every payload file. A rollback will verify this manifest before it
# stops a service or replaces a byte.
# Her ürün dosyasını özetle. Geri alma bir servisi durdurmadan veya tek bayt
# değiştirmeden önce bu manifesti doğrular.
(
    cd "$tmp_snap"
    LC_ALL=C find . -type f ! -name SHA256SUMS -print0 \
        | LC_ALL=C sort -z \
        | xargs -0 sha256sum > SHA256SUMS
    sha256sum -c SHA256SUMS >/dev/null
)
mv "$tmp_snap" "$snap"
tmp_snap=""
validate_root_trusted_dir_chain "$snap"
echo "==> Verified rollback snapshot / Doğrulanmış geri alma snapshot'ı: $snap"

# Retain only complete, verified snapshot directories.
# Yalnız tamamlanmış ve doğrulanmış snapshot dizilerini sakla.
mapfile -t old_snapshots < <(ls -1dt "$SNAP_ROOT"/*/ 2>/dev/null | tail -n +$((KEEP_SNAPSHOTS + 1)) || true)
for old_snapshot in "${old_snapshots[@]}"; do
    old_snapshot=${old_snapshot%/}
    validate_retention_snapshot "$old_snapshot"
done
for old_snapshot in "${old_snapshots[@]}"; do
    old_snapshot=${old_snapshot%/}
    rm -rf -- "$old_snapshot"
done

if [[ -d .git ]]; then
    echo "==> Pulling latest source / Son kaynak çekiliyor"
    repo_owner=$(stat -c %U .git)
    if [[ "$repo_owner" != root ]]; then
        sudo -u "$repo_owner" -H git pull --ff-only
    else
        git pull --ff-only
    fi
fi

echo "==> Re-running the installer / Kurucu yeniden çalıştırılıyor"
current_listen=$(grep -h '^Environment=CELIKPANEL_LISTEN=' "$UNIT_DIR/celikpanel-panel.service" 2>/dev/null | cut -d= -f3- || true)
if [[ -n "$current_listen" ]]; then
    LISTEN="$current_listen" ./install.sh
else
    ./install.sh
fi

systemctl is-active --quiet celikpanel-agent.service || die "updated agent is not running"
systemctl is-active --quiet celikpanel-panel.service || die "updated panel is not running"

trap - EXIT
echo
echo "==> Update complete / Güncelleme tamamlandı"
echo "    Verified rollback snapshot / Doğrulanmış geri alma snapshot'ı: $snap"
echo "    Roll back if needed / Gerekirse geri alın: sudo ./rollback.sh '$snap'"
