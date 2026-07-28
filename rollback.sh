#!/usr/bin/env bash
# CelikPanel rollback restores one complete snapshot produced by update.sh.
# Nothing is stopped or overwritten until the snapshot contract and every
# checksum have been verified.
#
# CelikPanel geri alma, update.sh tarafından üretilmiş tek bir eksiksiz
# snapshot'ı geri yükler. Snapshot sözleşmesi ve bütün checksum'lar doğrulanana
# kadar hiçbir şey durdurulmaz veya üzerine yazılmaz.
set -euo pipefail

SUPPORTED_SNAPSHOT_VERSION=3
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

die() {
    echo "!! $*" >&2
    exit 1
}

# Every privileged path component must be root-owned and non-writable so a
# pathname cannot be redirected between verification and restore.
# Her ayrıcalıklı yol bileşeni root sahipli ve başkalarınca yazılamaz olmalıdır;
# böylece doğrulama ile geri yükleme arasında yol başka yere yönlendirilemez.
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

# Rollback preflights run as root. Accept only an absolute, root-owned,
# non-writable regular executable; verified staging binaries may be selected
# explicitly during the one-time pre-ledger transition.
# Geri alma ön kontrolleri root olarak çalışır. Yalnız mutlak yoldaki, root
# sahipli, yazılamayan normal executable kabul edilir; ledger öncesi tek seferlik
# geçişte doğrulanmış staging binary'leri açıkça seçilebilir.
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

# Saved runtime state accepts only documented systemd ActiveState values.
# Kayıtlı çalışma durumu yalnız belgelenmiş systemd ActiveState değerlerini kabul eder.
validate_service_active_state() {
    local unit=$1 state=$2
    case "$state" in
        active|activating|reloading|refreshing|inactive|failed|deactivating|maintenance) ;;
        *) die "unsupported saved active state for $unit: $state" ;;
    esac
}

# Active-like states are restored by starting the service; every other valid
# state is inactive-like and must remain stopped.
# Active-benzeri durumlar servis başlatılarak geri yüklenir; diğer tüm geçerli
# durumlar inactive-benzeridir ve kapalı kalmalıdır.
service_state_is_active_like() {
    case "$1" in
        active|activating|reloading|refreshing) return 0 ;;
        *) return 1 ;;
    esac
}

# Compare one restored service with the active-like/inactive-like contract.
# Geri yüklenen tek servisi active-benzeri/inactive-benzeri sözleşmeyle karşılaştır.
verify_restored_service_active_state() {
    local unit=$1 state=$2
    if service_state_is_active_like "$state"; then
        systemctl is-active --quiet "$unit" \
            || die "restored active-like service is not active: $unit"
    elif systemctl is-active --quiet "$unit"; then
        die "restored inactive-like service became active: $unit"
    fi
}

# Hold the privileged mutation flock from the last idle proof until restored
# services have reached their saved state.
# Son boşta doğrulamasından geri yüklenen servisler kayıtlı durumuna ulaşana
# kadar ayrıcalıklı işlem flock kilidini tut.
acquire_release_mutation_lock() {
    local lock_dir owner mode permissions before after
    command -v flock >/dev/null || die "flock is required for a safe rollback"
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
    flock -n "$MUTATION_LOCK_FD" || die "a service/package mutation is active; rollback refused"
}

if [[ $EUID -ne 0 ]]; then
    die "Run as root / root olarak çalıştırın: sudo ./rollback.sh"
fi

validate_root_trusted_dir_chain "$SNAP_ROOT"
requested=${1:-}
if [[ -z "$requested" ]]; then
    requested=$(find "$SNAP_ROOT" -mindepth 1 -maxdepth 1 -type d ! -name '.*' -printf '%T@ %f\n' \
        | LC_ALL=C sort -nr | awk 'NR == 1 { print $2 }')
fi
[[ -n "$requested" ]] || die "no update snapshot found under $SNAP_ROOT"
requested=${requested%/}
case "$requested" in
    "$SNAP_ROOT"/*) snapshot_name=${requested#"$SNAP_ROOT/"} ;;
    */*) die "snapshot path is outside $SNAP_ROOT" ;;
    *) snapshot_name=$requested ;;
esac
[[ "$snapshot_name" =~ ^[0-9]{8}T[0-9]{6}Z-([0-9a-f]+|unknown)$ ]] \
    || die "invalid snapshot name: $snapshot_name"
snap="$SNAP_ROOT/$snapshot_name"
[[ -d "$snap" && ! -L "$snap" ]] || die "snapshot does not exist or is unsafe: $snap"
validate_root_trusted_dir_chain "$snap"

# Snapshot payloads must be plain directories and regular files. Symlinks would
# make checksum verification and privileged restore target different objects.
# Snapshot ürünleri düz dizin ve normal dosya olmalıdır. Sembolik bağlantılar,
# checksum ile root yetkili geri yüklemenin farklı nesneleri görmesine yol açar.
if find "$snap" -type l -print -quit | grep -q .; then
    die "snapshot contains a symbolic link / snapshot sembolik bağlantı içeriyor"
fi

[[ -f "$snap/snapshot.version" ]] || die "snapshot.version is missing"
version=$(tr -d '[:space:]' < "$snap/snapshot.version")
[[ "$version" == "$SUPPORTED_SNAPSHOT_VERSION" ]] || die "unsupported snapshot version: $version"
[[ -f "$snap/SHA256SUMS" ]] || die "SHA256SUMS is missing"
[[ -f "$snap/commit" ]] || die "commit provenance is missing"
[[ -f "$snap/created-at-utc" ]] || die "creation time is missing"
[[ -f "$snap/$(basename "$PANEL_DB")" ]] || die "panel database is missing from snapshot"
[[ -x "$snap/bin/panel" ]] || die "panel binary is missing from snapshot"
[[ -x "$snap/bin/agent" ]] || die "agent binary is missing from snapshot"
[[ -f "$snap/web/index.html" ]] || die "web artifact is missing from snapshot"
[[ -f "$snap/units/celikpanel-agent.service" ]] || die "agent unit is missing from snapshot"
[[ -f "$snap/units/celikpanel-panel.service" ]] || die "panel unit is missing from snapshot"
[[ -f "$snap/firewall-unit.state" ]] || die "firewall unit presence marker is missing"
[[ -f "$snap/agent-ledger.state" ]] || die "agent ledger presence marker is missing"
[[ -d "$snap/agent-state" ]] || die "agent state payload directory is missing"
[[ -f "$snap/service-states.tsv" ]] || die "service state ledger is missing"

firewall_state=$(tr -d '[:space:]' < "$snap/firewall-unit.state")
case "$firewall_state" in
    present)
        [[ -f "$snap/units/celikpanel-firewall-restore.service" ]] || die "firewall unit is marked present but missing"
        ;;
    absent)
        [[ ! -e "$snap/units/celikpanel-firewall-restore.service" ]] || die "firewall unit is marked absent but included"
        ;;
    *)
        die "invalid firewall unit state: $firewall_state"
        ;;
esac

agent_ledger_state=$(tr -d '[:space:]' < "$snap/agent-ledger.state")
case "$agent_ledger_state" in
    present)
        [[ -f "$snap/agent-state/service-mutations.json" ]] || die "agent ledger is marked present but missing"
        ;;
    absent)
        [[ ! -e "$snap/agent-state/service-mutations.json" ]] || die "agent ledger is marked absent but included"
        ;;
    *)
        die "invalid agent ledger state: $agent_ledger_state"
        ;;
esac

# Reject manifest paths that could escape the verified snapshot directory.
# Doğrulanmış snapshot dizininin dışına çıkabilecek manifest yollarını reddet.
while IFS= read -r checksum_line; do
    manifest_path=${checksum_line#*  }
    [[ "$manifest_path" == ./* ]] || die "unsafe checksum path: $manifest_path"
    [[ "$manifest_path" != *'/../'* && "$manifest_path" != '../'* ]] || die "unsafe checksum traversal: $manifest_path"
done < "$snap/SHA256SUMS"
(
    cd "$snap"
    sha256sum -c SHA256SUMS >/dev/null
) || die "snapshot checksum verification failed / snapshot checksum doğrulaması başarısız"

# Validate the state ledger before service shutdown. Only the three units owned
# by this release may be changed by rollback.
# Servisleri durdurmadan önce durum defterini doğrula. Geri alma yalnız bu
# sürümün sahip olduğu üç unit'i değiştirebilir.
declare -A enabled_states=()
declare -A active_states=()
while IFS=$'\t' read -r unit enabled_state active_state extra; do
    [[ -n "$unit" && -n "$enabled_state" && -n "$active_state" && -z "${extra:-}" ]] || die "malformed service state ledger"
    case "$unit" in
        celikpanel-agent.service|celikpanel-panel.service|celikpanel-firewall-restore.service) ;;
        *) die "unexpected unit in service state ledger: $unit" ;;
    esac
    validate_service_active_state "$unit" "$active_state"
    enabled_states["$unit"]=$enabled_state
    active_states["$unit"]=$active_state
done < "$snap/service-states.tsv"
for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
    [[ -n "${enabled_states[$unit]:-}" ]] || die "service state is missing for $unit"
    case "${enabled_states[$unit]}" in
        enabled|enabled-runtime|disabled|static|indirect|not-found) ;;
        *) die "unsupported saved enable state for $unit: ${enabled_states[$unit]}" ;;
    esac
done
case "$firewall_state:${enabled_states[celikpanel-firewall-restore.service]}" in
    absent:not-found|present:*) ;;
    *) die "firewall unit presence and enablement state disagree" ;;
esac
if service_state_is_active_like "${active_states[celikpanel-panel.service]}" && \
   ! service_state_is_active_like "${active_states[celikpanel-agent.service]}"; then
    die "saved runtime state is inconsistent: an active panel requires an active agent"
fi

echo "==> Verified snapshot / Doğrulanmış snapshot: $snap"
validate_preflight_binary "$PREFLIGHT_PANEL" panel
validate_preflight_binary "$PREFLIGHT_AGENT" agent
echo "==> Stopping CelikPanel services / CelikPanel servisleri durduruluyor"
systemctl stop celikpanel-panel.service || die "panel could not be stopped"
if systemctl is-active --quiet celikpanel-panel.service; then
    die "panel is still active; rollback refused"
fi

# Refuse rollback while either durable coordinator or the host package manager
# can still be changing the machine. The common flock closes the race between
# this proof and stopping the privileged agent.
# Kalıcı koordinatörlerden biri veya sistem paket yöneticisi makineyi hâlâ
# değiştirirken geri almayı reddet. Ortak flock, bu kanıt ile ayrıcalıklı agent'ı
# durdurma arasındaki yarışı kapatır.
CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \
    "$PREFLIGHT_PANEL" --check-service-operations-idle \
    || die "panel service operations are not idle; rollback refused"
CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
    "$PREFLIGHT_AGENT" --check-service-mutation-idle \
    || die "agent/package mutations are not idle; rollback refused"
acquire_release_mutation_lock

systemctl stop celikpanel-agent.service || die "agent could not be stopped"
if systemctl is-active --quiet celikpanel-agent.service; then
    die "agent is still active; rollback refused"
fi

rm -rf -- "$BIN_DIR"
cp -a "$snap/bin" "$BIN_DIR"
rm -rf -- "$WEB_DIR"
cp -a "$snap/web" "$WEB_DIR"

# A stale WAL or SHM must never be attached to the older restored database.
# Bayat WAL veya SHM, geri yüklenen eski veritabanına asla bağlanmamalıdır.
rm -f -- "$PANEL_DB" "$PANEL_DB-wal" "$PANEL_DB-shm"
cp -a "$snap/$(basename "$PANEL_DB")" "$PANEL_DB"
[[ ! -f "$snap/$(basename "$PANEL_DB")-wal" ]] || cp -a "$snap/$(basename "$PANEL_DB")-wal" "$PANEL_DB-wal"
[[ ! -f "$snap/$(basename "$PANEL_DB")-shm" ]] || cp -a "$snap/$(basename "$PANEL_DB")-shm" "$PANEL_DB-shm"

# Restore only the paired durable ledger; unrelated agent state remains intact.
# Yalnız eşlenmiş kalıcı ledger'ı geri yükle; ilgisiz agent durumu olduğu gibi kalır.
install -d -m 0700 -o root -g root "$AGENT_STATE_DIR"
rm -f -- "$AGENT_LEDGER"
if [[ "$agent_ledger_state" == present ]]; then
    install -m 0600 -o root -g root \
        "$snap/agent-state/service-mutations.json" "$AGENT_LEDGER"
fi

for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
    systemctl disable "$unit" >/dev/null 2>&1 || true
    rm -f -- "$UNIT_DIR/$unit"
done
rm -f -- "$UNIT_DIR/multi-user.target.wants/celikpanel-firewall-restore.service"
rm -f -- "$UNIT_DIR/network-pre.target.requires/celikpanel-firewall-restore.service"
cp -a "$snap/units/." "$UNIT_DIR/"
systemctl daemon-reload

for unit in celikpanel-agent.service celikpanel-panel.service celikpanel-firewall-restore.service; do
    case "${enabled_states[$unit]}" in
        enabled)
            [[ -f "$UNIT_DIR/$unit" ]] || die "cannot enable absent restored unit: $unit"
            systemctl enable "$unit" >/dev/null
            ;;
        enabled-runtime)
            [[ -f "$UNIT_DIR/$unit" ]] || die "cannot runtime-enable absent restored unit: $unit"
            systemctl enable --runtime "$unit" >/dev/null
            ;;
        disabled)
            systemctl disable "$unit" >/dev/null 2>&1 || true
            ;;
        static|indirect|not-found) ;;
    esac
    actual_enabled_state=$(systemctl is-enabled "$unit" 2>/dev/null || true)
    [[ "${actual_enabled_state:-unknown}" == "${enabled_states[$unit]}" ]] \
        || die "restored enablement mismatch for $unit: got ${actual_enabled_state:-unknown}, want ${enabled_states[$unit]}"
done

# Agent starts before panel because the panel verifies the agent build and RPC
# socket during normal operation.
# Panel normal çalışmada agent derlemesini ve RPC soketini doğruladığı için önce
# agent başlatılır.
if service_state_is_active_like "${active_states[celikpanel-agent.service]}"; then
    systemctl start celikpanel-agent.service || die "restored agent did not start"
fi
if service_state_is_active_like "${active_states[celikpanel-panel.service]}"; then
    systemctl start celikpanel-panel.service || die "restored panel did not start"
fi

# Prove the final agent and panel runtime states before reporting success.
# Başarı bildirmeden önce agent ve panelin son çalışma durumlarını kanıtla.
for unit in celikpanel-agent.service celikpanel-panel.service; do
    verify_restored_service_active_state "$unit" "${active_states[$unit]}"
done

commit=$(tr -d '[:space:]' < "$snap/commit")
echo
echo "==> Rollback complete / Geri alma tamamlandı"
echo "    Artifact source commit / Ürün kaynak commit'i: $commit"
echo "    Source checkout was not changed / Kaynak çalışma ağacı değiştirilmedi"
echo "    Panel: $(systemctl is-active celikpanel-panel.service 2>/dev/null || true)"
echo "    Agent: $(systemctl is-active celikpanel-agent.service 2>/dev/null || true)"
