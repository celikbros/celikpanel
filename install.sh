#!/bin/bash
#
# CelikPanel installer — one command from a fresh Debian 13 or Ubuntu 24.04
# acceptance target (or Arch Linux dev-test target) to a login screen. Install
# tagged prebuilt releases; existing installations use the reviewed updater.
#
# CelikPanel kurulumu — temiz Debian 13 veya Ubuntu 24.04 kabul hedefinden (ya
# da Arch Linux geliştirme-test hedefinden) giriş ekranına tek komut. Etiketli,
# önceden derlenmiş sürümü kurun; mevcut kurulumlarda incelenmiş updater'ı kullanın.
#
#   sudo ./install.sh
#
# Environment knobs / Ortam ayarları:
#   SKIP_DEPS=1     do not apt-install the tiny prerequisites (tar, xz, curl)
#   SKIP_ADMIN=1    do not prompt to create the first administrator
#   LISTEN=:2083    panel bind address
#   DEMO=1          R&D mode: quick-login accounts on the login screen
#                   (admin/reseller/customer @ demo1234) and cookies that
#                   work over plain HTTP. NEVER on an internet-facing server.
#                   AR-GE modu: giriş ekranında hızlı-giriş hesapları ve düz
#                   HTTP'de çalışan çerezler. İnternete açık sunucuda ASLA.

set -euo pipefail

# Ignore caller-controlled command lookup before privileged installation.
# Ayrıcalıklı kurulumdan önce çağıranın denetlediği komut arama yolunu yok say.
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export PATH

PREFIX=/opt/celikpanel
DATA_DIR=/var/lib/celikpanel
IMPORT_DIR=/var/lib/celikpanel-imports
CONF_DIR=/etc/celikpanel
PANEL_ENV="$CONF_DIR/panel.env"
INSTALL_COMPLETE=/etc/celikpanel/install.complete
AGENT_STATE_DIR=/var/lib/celikpanel-agent-private
AGENT_LEDGER="$AGENT_STATE_DIR/service-mutations.json"
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
RELEASE_TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
RELEASE_TRANSACTION_RUNTIME_ROOT=/run/celikpanel-release-transaction
RELEASE_TRANSACTION_HELPER=/usr/libexec/celikpanel/release-transaction-start-guard
SVC_USER=celikpanel
SVC_GROUP=celikpanel
LISTEN="${LISTEN:-:2083}"
case "${CELIKPANEL_APPLY_ONLY:-0}" in
    0|1) ;;
    *) printf '%s\n' "ERROR / HATA: CELIKPANEL_APPLY_ONLY must be 0 or 1 / yalnız 0 veya 1 olabilir" >&2; exit 1 ;;
esac
APPLY_ONLY=${CELIKPANEL_APPLY_ONLY:-0}
case "${DEMO:-0}" in
    0|1) ;;
    *) printf '%s\n' "ERROR / HATA: DEMO must be 0 or 1 / yalnız 0 veya 1 olabilir" >&2; exit 1 ;;
esac

SRC="$(cd "$(dirname "$(readlink -f "$0")")" && pwd)"

c() { printf '\033[%sm%s\033[0m\n' "$1" "$2"; }
bilingual() {
    local english=$1 turkish=${2:-}
    if [[ -n "$turkish" ]]; then
        printf '%s / %s' "$english" "$turkish"
    else
        printf '%s' "$english"
    fi
}
step() { c '1;36' "==> $(bilingual "$@")"; }
ok() { c '32' "    ✓ $(bilingual "$@")"; }
warn() { c '33' "    $(bilingual "$@")"; }
die() { c '1;31' "ERROR / HATA: $(bilingual "$@")" >&2; exit 1; }

valid_panel_listen() {
    local value=$1 host port
    case "$value" in
        :*)
            host=
            port=${value#:}
            ;;
        \[*\]:*)
            host=${value%:*}
            port=${value##*:}
            [[ "$host" =~ ^\[[0-9A-Fa-f:]+\]$ ]] || return 1
            ;;
        *:*)
            host=${value%:*}
            port=${value##*:}
            [[ "$host" =~ ^[A-Za-z0-9._-]+$ ]] || return 1
            ;;
        *) return 1 ;;
    esac
    [[ "$port" =~ ^[0-9]{1,5}$ ]] || return 1
    (( 10#$port >= 1 && 10#$port <= 65535 ))
}

valid_panel_config_path() {
    local value=$1
    [[ "$value" =~ ^/[A-Za-z0-9._/@+-]+$ &&
       "$value" != *[[:space:]]* &&
       "$value" != *'/../'* && "$value" != */.. &&
       "$value" != *'/./'* && "$value" != */. ]]
}

# Validate without sourcing: panel.env is data, never shell code. The strict
# key set also catches typos before systemd restarts the public panel.
# source etmeden doğrula: panel.env kabuk kodu değil veridir. Sıkı anahtar
# kümesi, systemd açık paneli yeniden başlatmadan önce yazım hatalarını yakalar.
validate_panel_env() {
    local file=$1 line key value line_number=0
    local listen= tls= cert= key_path= tls_dir= cookie_flag= demo_flag=
    declare -A seen=()

    [[ -f "$file" && ! -L "$file" ]] || die "panel ortam dosyası eksik veya güvensiz: $file"
    read -r env_owner env_group env_mode < <(stat -Lc '%u %g %a' -- "$file") \
        || die "panel ortam dosyası incelenemedi"
    [[ "$env_owner" == 0 && "$env_group" == 0 && "$env_mode" == 600 ]] \
        || die "panel ortam dosyası root:root mode 0600 olmalı"

    while IFS= read -r line || [[ -n "$line" ]]; do
        ((line_number += 1))
        case "$line" in ''|'#'*) continue ;; esac
        [[ "$line" == *=* ]] || die "panel.env:$line_number geçersiz satır"
        key=${line%%=*}
        value=${line#*=}
        [[ -z "${seen[$key]+set}" ]] || die "panel.env:$line_number yinelenen anahtar: $key"
        seen[$key]=1
        case "$key" in
            CELIKPANEL_LISTEN)
                valid_panel_listen "$value" || die "panel.env:$line_number geçersiz dinleme adresi"
                listen=$value
                ;;
            CELIKPANEL_TLS)
                [[ "$value" == 0 || "$value" == 1 ]] || die "panel.env:$line_number CELIKPANEL_TLS 0 veya 1 olmalı"
                tls=$value
                ;;
            CELIKPANEL_TLS_CERT)
                valid_panel_config_path "$value" || die "panel.env:$line_number geçersiz sertifika yolu"
                cert=$value
                ;;
            CELIKPANEL_TLS_KEY)
                valid_panel_config_path "$value" || die "panel.env:$line_number geçersiz özel anahtar yolu"
                key_path=$value
                ;;
            CELIKPANEL_TLS_DIR)
                valid_panel_config_path "$value" || die "panel.env:$line_number geçersiz TLS dizini"
                tls_dir=$value
                ;;
            CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG)
                [[ -z "$value" || "$value" == --insecure-cookies ]] \
                    || die "panel.env:$line_number geçersiz cookie bayrağı"
                cookie_flag=$value
                ;;
            CELIKPANEL_PANEL_DEMO_FLAG)
                [[ -z "$value" || "$value" == --demo ]] \
                    || die "panel.env:$line_number geçersiz demo bayrağı"
                demo_flag=$value
                ;;
            *) die "panel.env:$line_number desteklenmeyen anahtar: $key" ;;
        esac
    done < "$file"

    [[ -n "${seen[CELIKPANEL_LISTEN]+set}" &&
       -n "${seen[CELIKPANEL_TLS]+set}" &&
       -n "${seen[CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG]+set}" &&
       -n "${seen[CELIKPANEL_PANEL_DEMO_FLAG]+set}" ]] \
        || die "panel.env zorunlu anahtarları eksik"
    [[ (-n "$cert" && -n "$key_path") || (-z "$cert" && -z "$key_path") ]] \
        || die "panel.env sertifika ve özel anahtar yollarını birlikte tanımlamalı"
    if [[ -n "$demo_flag" ]]; then
        [[ "$cookie_flag" == --insecure-cookies && "$tls" == 0 && -z "$cert" ]] \
            || die "demo modu yalnız güvensiz cookie + TLS kapalı bileşimiyle kullanılabilir"
    else
        [[ -z "$cookie_flag" ]] || die "güvensiz cookie yalnız demo modunda kullanılabilir"
        [[ "$tls" == 1 || -n "$cert" ]] || die "normal panel modu TLS olmadan çalıştırılamaz"
    fi

    VALIDATED_PANEL_LISTEN=$listen
    VALIDATED_PANEL_HTTPS=1
    [[ -n "$demo_flag" ]] && VALIDATED_PANEL_HTTPS=0
    : "$tls_dir"
}

legacy_panel_unit_value() {
    local file=$1 key=$2 prefix="Environment=$2=" line value= count=0
    while IFS= read -r line; do
        [[ "$line" == "$prefix"* ]] || continue
        value=${line#"$prefix"}
        ((count += 1))
    done < "$file"
    (( count <= 1 )) || die "eski panel unitinde yinelenen $key ayarı"
    printf '%s' "$value"
}

ensure_panel_env() {
    local installed_unit=/etc/systemd/system/celikpanel-panel.service
    local listen=$LISTEN tls=1 cert= key_path= tls_dir= cookie_flag= demo_flag= migrated=
    local unit_owner unit_group unit_mode unit_permissions old_exec temp_env

    if [[ -e "$PANEL_ENV" || -L "$PANEL_ENV" ]]; then
        validate_panel_env "$PANEL_ENV"
        return
    fi

    valid_panel_listen "$listen" || die "geçersiz LISTEN değeri: $listen"
    if [[ "${DEMO:-0}" == 1 ]]; then
        tls=0
        cookie_flag=--insecure-cookies
        demo_flag=--demo
    fi

    # Migrate the exact settings written by older installers before replacing
    # their generated unit. Unknown command-line overrides stop the upgrade
    # instead of being silently discarded.
    # Eski kurucunun yazdığı ayarları üretilmiş unit değiştirilmeden önce taşı.
    # Bilinmeyen komut satırı geçersiz kılmaları sessizce kaybolmak yerine
    # yükseltmeyi durdurur.
    if [[ -e "$installed_unit" || -L "$installed_unit" ]]; then
        [[ -f "$installed_unit" && ! -L "$installed_unit" ]] \
            || die "eski panel systemd unit yolu güvensiz"
        read -r unit_owner unit_group unit_mode < <(stat -Lc '%u %g %a' -- "$installed_unit") \
            || die "eski panel systemd uniti incelenemedi"
        unit_permissions=$((8#$unit_mode))
        [[ "$unit_owner" == 0 && "$unit_group" == 0 ]] \
            && (( (unit_permissions & 0022) == 0 )) \
            || die "eski panel systemd uniti root sahipli ve yazmaya kapalı olmalı"

        migrated=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_LISTEN)
        [[ -z "$migrated" ]] || listen=$migrated
        migrated=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS)
        [[ -z "$migrated" ]] || tls=$migrated
        cert=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS_CERT)
        key_path=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS_KEY)
        tls_dir=$(legacy_panel_unit_value "$installed_unit" CELIKPANEL_TLS_DIR)
        old_exec=$(sed -n 's/^ExecStart=//p' "$installed_unit" | tail -n 1)
        case "$old_exec" in
            /opt/celikpanel/bin/panel|\
            '/opt/celikpanel/bin/panel $CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG $CELIKPANEL_PANEL_DEMO_FLAG'|\
            '') ;;
            '/opt/celikpanel/bin/panel --insecure-cookies --demo')
                tls=0
                cookie_flag=--insecure-cookies
                demo_flag=--demo
                ;;
            *) die "eski panel ExecStart ayarı otomatik taşınamıyor: $old_exec" ;;
        esac
    fi

    valid_panel_listen "$listen" || die "taşınan panel dinleme adresi geçersiz"
    [[ "$tls" == 0 || "$tls" == 1 ]] || die "taşınan TLS ayarı geçersiz"
    [[ -z "$cert" ]] || valid_panel_config_path "$cert" || die "taşınan sertifika yolu geçersiz"
    [[ -z "$key_path" ]] || valid_panel_config_path "$key_path" || die "taşınan özel anahtar yolu geçersiz"
    [[ -z "$tls_dir" ]] || valid_panel_config_path "$tls_dir" || die "taşınan TLS dizini geçersiz"

    temp_env=$(mktemp "$CONF_DIR/.panel.env.XXXXXXXX") || die "panel ortam geçici dosyası oluşturulamadı"
    chmod 0600 "$temp_env"
    chown root:root "$temp_env"
    {
        printf 'CELIKPANEL_LISTEN=%s\n' "$listen"
        printf 'CELIKPANEL_TLS=%s\n' "$tls"
        [[ -z "$cert" ]] || printf 'CELIKPANEL_TLS_CERT=%s\n' "$cert"
        [[ -z "$key_path" ]] || printf 'CELIKPANEL_TLS_KEY=%s\n' "$key_path"
        [[ -z "$tls_dir" ]] || printf 'CELIKPANEL_TLS_DIR=%s\n' "$tls_dir"
        printf 'CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG=%s\n' "$cookie_flag"
        printf 'CELIKPANEL_PANEL_DEMO_FLAG=%s\n' "$demo_flag"
    } > "$temp_env"
    mv -T --no-clobber -- "$temp_env" "$PANEL_ENV" \
        || { rm -f -- "$temp_env"; die "panel ortam dosyası yayınlanamadı"; }
    validate_panel_env "$PANEL_ENV"
}

service_group_id() {
    local group_id
    group_id=$(getent group "$SVC_GROUP" | cut -d: -f3) || return 1
    [[ "$group_id" =~ ^[0-9]+$ ]] || return 1
    printf '%s\n' "$group_id"
}

# Set the private mask after sudo changes identity so host sudoers policy
# cannot widen SQLite database or sidecar permissions during bootstrap.
# Kimlik sudo ile değiştirildikten sonra özel maskeyi ayarla; böylece sunucunun
# sudoers ilkesi bootstrap sırasında SQLite veritabanı veya yan dosya
# izinlerini genişletemez.
run_panel_as_service_user_with_private_umask() {
    sudo -u "$SVC_USER" CELIKPANEL_DATA_DIR="$DATA_DIR" \
        /bin/sh -c 'umask 077; exec "$@"' celikpanel-install "$PREFIX/bin/panel" "$@"
}

[ "$(id -u)" -eq 0 ] || die "root olarak çalıştırın (sudo ./install.sh)"
command -v systemctl >/dev/null || die "systemd gerekli"

# Apply-only is accepted solely from a completely verified immutable release
# while the inherited persistent lock and exact active update marker are live.
# Apply-only yalnız tamamen doğrulanmış değişmez sürümden, miras kalıcı kilit ve
# tam active update işaretçisi canlıyken kabul edilir.
validate_apply_only_transaction() {
    local root canonical relative entry owner mode permissions state
    [[ "$APPLY_ONLY" -eq 1 ]] || return 0
    [[ "${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}" == 0 ]] \
        || die "apply-only cannot initialize the service mutation ledger"
    [[ "${SKIP_DEPS:-}" == 1 && "${SKIP_SECURITY_UPDATES:-}" == 1 && "${SKIP_ADMIN:-}" == 1 ]] \
        || die "apply-only requires dependency, security-update and admin work disabled"
    [[ "${DEMO:-0}" != 1 ]] || die "apply-only refuses demo mode"
    root=${CELIKPANEL_TRUSTED_RELEASE_ROOT:-}
    [[ "$root" == "$SRC" && "$root" == /* ]] || die "apply-only trusted release root mismatch"
    canonical=$(readlink -e -- "$root") || die "apply-only trusted release is unavailable"
    [[ "$canonical" == "$root" ]] || die "apply-only trusted release path is not canonical"
    [[ "$root" == /var/backups/celikpanel/releases/* ]] || die "apply-only release is outside retained storage"
    relative=${root#/var/backups/celikpanel/releases/}
    [[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]] || die "apply-only release name is invalid"
    [[ -d "$root" && ! -L "$root" && ! -e "$root/.git" ]] || die "apply-only release root is unsafe"
    read -r owner mode < <(stat -Lc '%u %a' -- "$root") || die "cannot inspect apply-only release root"
    [[ "$owner" == 0 && "$mode" == 700 ]] || die "apply-only release root must be root-owned mode 0700"
    if find "$root" -type l -print -quit | grep -q .; then die "apply-only release contains a symbolic link"; fi
    if find "$root" ! -type d ! -type f -print -quit | grep -q .; then die "apply-only release contains a special object"; fi
    while IFS= read -r -d '' entry; do
        read -r owner mode < <(stat -Lc '%u %a' -- "$entry") || die "cannot inspect apply-only release entry"
        [[ "$owner" == 0 ]] || die "apply-only release entry is not root-owned"
        permissions=$((8#$mode)); (( (permissions & 0022) == 0 )) || die "apply-only release entry is writable"
    done < <(find "$root" -mindepth 1 -print0)
    [[ -f "$root/SHA256SUMS" && ! -L "$root/SHA256SUMS" ]] || die "apply-only checksum manifest is missing"
    (cd "$root"; LC_ALL=C find . -type f ! -path './SHA256SUMS' -print0 | LC_ALL=C sort -z | xargs -0 sha256sum | cmp -s - SHA256SUMS; sha256sum -c SHA256SUMS >/dev/null) \
        || die "apply-only trusted release checksum verification failed"
    [[ -x "$root/bin/panel" && -x "$root/bin/agent" && -f "$root/web/dist/index.html" ]] \
        || die "apply-only release artifacts are incomplete"
    [[ "${CELIKPANEL_RELEASE_TRANSACTION_FD:-}" =~ ^[0-9]+$ ]] || die "apply-only transaction FD is missing"
    [[ "${CELIKPANEL_RELEASE_TRANSACTION_OPERATION:-}" == update ]] || die "apply-only operation must be update"
    TRUSTED_RELEASE_ROOT=$root
    # shellcheck source=deploy/release-transaction-guard.sh
    source "$root/deploy/release-transaction-guard.sh"
    release_txn_verify_inherited_lock "$RELEASE_TRANSACTION_ROOT" "$CELIKPANEL_RELEASE_TRANSACTION_FD" \
        || die "apply-only inherited transaction lock proof failed"
    release_txn_validate_active_token "$RELEASE_TRANSACTION_ROOT" \
        "${CELIKPANEL_RELEASE_TRANSACTION_TOKEN:-}" update \
        "${CELIKPANEL_RELEASE_TRANSACTION_SNAPSHOT:-}" \
        || die "apply-only active transaction marker proof failed"
    for unit in celikpanel-agent.service celikpanel-panel.service; do
        state=$(systemctl show --property=ActiveState --value "$unit") || die "cannot inspect $unit for apply-only"
        [[ "$state" == inactive || "$state" == failed ]] || die "apply-only requires $unit stopped"
    done
    release_txn_install_and_verify_unit_guards \
        "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_RUNTIME_ROOT" \
        /etc/systemd/system "$RELEASE_TRANSACTION_HELPER" \
        "$CELIKPANEL_RELEASE_TRANSACTION_FD" \
        || die "apply-only transaction service guard proof failed"
}
validate_apply_only_transaction

# Ledger initialization is one-shot. Permit it only for an explicitly audited
# pre-ledger transition or a proven fresh host with neither DB nor private state.
# Ledger başlatma tek seferliktir. Yalnız açıkça denetlenmiş pre-ledger geçişinde
# veya DB ve özel state bulunmayan kanıtlanmış temiz makinede izin ver.
case "${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}" in
    0|1) ;;
    *) die "INITIALIZE_SERVICE_MUTATION_LEDGER yalnız 0 veya 1 olabilir" ;;
esac
initialize_ledger=${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}
# Existing private state must already be the exact root-only directory. Reject
# aliases before install -d can follow or normalize them.
# Mevcut özel state önceden tam root-only dizin olmalıdır. install -d yolu
# izleyip normalleştirmeden önce alias'ları reddet.
if [[ -e "$AGENT_STATE_DIR" || -L "$AGENT_STATE_DIR" ]]; then
    [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]] \
        || die "güvensiz özel agent state yolu: $AGENT_STATE_DIR"
    read -r state_owner state_group state_mode < <(stat -Lc '%u %g %a' -- "$AGENT_STATE_DIR") \
        || die "özel agent state dizini incelenemedi"
    expected_state_group=$(service_group_id) \
        || die "mevcut özel agent state için $SVC_GROUP grubu bulunamadı"
    [[ "$state_owner" == 0 && "$state_group" == "$expected_state_group" && "$state_mode" == 700 ]] \
        || die "özel agent state dizini root:$SVC_GROUP mode 0700 olmalı"
fi
# A failed fresh install may have created only the exact empty private
# directory. Treat that one state as fresh so retry remains idempotent; any
# hidden or visible entry keeps automatic initialization fail-closed.
# Başarısız temiz kurulum yalnız tam boş özel dizini oluşturmuş olabilir.
# Yeniden deneme idempotent kalsın diye yalnız bu durumu temiz kabul et; gizli
# veya görünür herhangi bir girdi otomatik başlatmayı kapalı biçimde reddettirir.
if [[ ! -e "$DATA_DIR/celikpanel.db" && ! -L "$DATA_DIR/celikpanel.db" ]]; then
    if [[ ! -e "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" ]]; then
        initialize_ledger=1
    elif [[ -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" &&
            ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
        partial_state_entry=$(find "$AGENT_STATE_DIR" -mindepth 1 -maxdepth 1 -print -quit) \
            || die "özel agent state dizini boşluğu doğrulanamadı"
        if [[ -z "$partial_state_entry" ]]; then
            initialize_ledger=1
        fi
    fi
fi
if [[ $initialize_ledger -eq 1 &&
      -d "$AGENT_STATE_DIR" && ! -L "$AGENT_STATE_DIR" &&
      ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]]; then
    unexpected_initial_state=$(find "$AGENT_STATE_DIR" -mindepth 1 -maxdepth 1 -print -quit) \
        || die "özel agent state içeriği doğrulanamadı"
    [[ -z "$unexpected_initial_state" ]] \
        || die "ledger başlatma yalnız tamamen boş özel agent state dizininde yapılabilir"
fi
if [[ -e "$AGENT_LEDGER" || -L "$AGENT_LEDGER" ]]; then
    [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || die "güvensiz servis işlem ledger yolu: $AGENT_LEDGER"
    read -r ledger_owner ledger_group ledger_mode < <(stat -Lc '%u %g %a' -- "$AGENT_LEDGER") \
        || die "servis işlem ledger incelenemedi"
    expected_ledger_group=$(service_group_id) \
        || die "mevcut servis işlem ledger için $SVC_GROUP grubu bulunamadı"
    [[ "$ledger_owner" == 0 && "$ledger_group" == "$expected_ledger_group" && "$ledger_mode" == 600 ]] \
        || die "servis işlem ledger root:$SVC_GROUP mode 0600 olmalı"
    [[ "${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}" != 1 ]] \
        || die "servis işlem ledger zaten var; explicit başlatma reddedildi"
elif [[ $initialize_ledger -ne 1 ]]; then
    die "servis işlem ledger eksik; yalnız temiz kurulum veya denetlenmiş bootstrap başlatabilir"
fi

# Package manager: apt (Ubuntu/Debian, the first-class tested target) and
# pacman (Arch, dev-test target since Jul 16) are supported. Anything else
# fails honestly instead of guessing.
# Paket yöneticisi: apt (Ubuntu/Debian, birinci sınıf test hedefi) ve pacman
# (Arch, 16 Tem'den beri geliştirme-test hedefi) desteklenir. Gerisi tahmin
# etmek yerine dürüstçe durur.
if [[ $APPLY_ONLY -eq 1 ]]; then
    PKG_FAMILY=apply-only
elif command -v apt-get >/dev/null; then
    PKG_FAMILY=apt
elif command -v pacman >/dev/null; then
    PKG_FAMILY=pacman
else
    die "No supported package manager was found (apt or pacman is required)" \
        "Desteklenen paket yöneticisi bulunamadı (apt veya pacman gerekli)"
fi

# 1. Minimal prerequisites ---------------------------------------------------
# The panel and agent are self-contained (static Go binaries + embedded
# SQLite); we install NOTHING for hosting here. nginx / php / mariadb /
# postgresql / mail are added later from the panel, on demand, so the operator
# runs only what they actually want (constitution: what isn't installed is
# invisible). We ensure only the few tiny tools the agent itself uses.
#
# nftables belongs in this list, not in the on-demand catalog. It is the tool
# the agent shells out to for the firewall — plumbing, exactly like curl. The
# kernel packet filter (netfilter) is always present; only this userspace `nft`
# binary can be missing on a minimal image. Installing it changes nothing:
# it writes no rules and closes no ports until the operator hits "Turn on", and
# it never conflicts with ufw / firewalld / a cloud firewall (those drive the
# same nftables underneath). Having the tool ≠ enabling the firewall — so the
# firewall stays a clean on/off switch and this respects "never turn on a
# firewall by surprise."
#
# Panel ve agent kendi kendine yeter (statik Go binary + gömülü SQLite);
# barındırma için burada HİÇBİR ŞEY kurmayız. nginx / php / mariadb /
# postgresql / mail sonradan panelden, talep üzerine eklenir; böylece operatör
# yalnız gerçekten istediğini çalıştırır. Yalnız agent'ın kendi kullandığı
# birkaç küçük aracı sağlarız.
#
# nftables bu listede olmalı, talep-üzerine katalogda değil. Agent'ın firewall
# için çağırdığı araç budur — tıpkı curl gibi tesisat. Çekirdek paket süzgeci
# (netfilter) hep vardır; minimal imajda eksik olabilen yalnız bu kullanıcı-
# alanı `nft` ikilisidir. Kurmak hiçbir şeyi değiştirmez: operatör "Turn on"
# demeden tek kural yazmaz, tek port kapatmaz ve ufw / firewalld / bulut
# firewall ile ASLA çakışmaz (hepsi altta aynı nftables'ı sürer). Aracı kurmak
# ≠ firewall'u açmak — böylece firewall temiz bir aç/kapa düğmesi kalır ve bu,
# "firewall'u sürprizle açma" kuralına uyar.
if [[ $APPLY_ONLY -eq 0 ]] && [ "${SKIP_DEPS:-0}" != "1" ]; then
    step "Small prerequisites (curl, tar, xz, nftables)" \
        "Küçük ön gereksinimler (curl, tar, xz, nftables)"
    case "$PKG_FAMILY" in
    apt)
        export DEBIAN_FRONTEND=noninteractive
        # A broken third-party repo must not abort the install; the packages we
        # need come from the base archives and may already be cached.
        # Bozuk bir üçüncü parti depo kurulumu iptal etmemeli; ihtiyacımız olan
        # paketler ana arşivlerden gelir ve zaten önbellekte olabilir.
        apt-get update -qq || warn "apt-get update returned a warning — continuing" \
            "apt-get update uyarı verdi — devam ediliyor"
        apt-get install -y -qq tar xz-utils curl ca-certificates nftables >/dev/null
        ;;
    pacman)
        # Arch does not support partial upgrades. Refresh, upgrade and install
        # prerequisites in one transaction so the host is never left with a
        # new package database and an old base system.
        # Arch kısmi yükseltmeleri desteklemez. Makineyi yeni paket veritabanı
        # ve eski temel sistemle bırakmamak için tazeleme, yükseltme ve ön
        # gereksinim kurulumunu tek işlemde yap.
        pacman -Syu --noconfirm --needed tar xz curl ca-certificates nftables >/dev/null
        ;;
    esac
    ok "ready" "hazır"
else
    step "Prerequisite installation skipped (SKIP_DEPS=1)" \
        "Ön gereksinim kurulumu atlandı (SKIP_DEPS=1)"
fi

# 1b. Automatic security patches --------------------------------------------
# Every package the operator later installs from the panel (nginx, postfix,
# PowerDNS…) is attack surface; unattended-upgrades keeps that surface patched
# without anyone remembering to. Security origin only — never feature upgrades,
# so a hosting box is never surprised by a behaviour change, only by a fix.
# SKIP_SECURITY_UPDATES=1 opts out.
#
# 1b. Otomatik güvenlik yamaları. Operatörün panelden kurduğu her paket
# (nginx, postfix, PowerDNS…) saldırı yüzeyidir; unattended-upgrades bu yüzeyi
# kimse hatırlamak zorunda kalmadan yamalı tutar. Yalnız güvenlik kaynağı —
# asla özellik yükseltmesi; barındırma kutusu davranış değişikliğiyle
# şaşırmaz, yalnız düzeltmeyle. SKIP_SECURITY_UPDATES=1 devre dışı bırakır.
if [[ $APPLY_ONLY -eq 0 ]] && [ "${SKIP_DEPS:-0}" != "1" ] && [ "${SKIP_SECURITY_UPDATES:-0}" != "1" ] && [ "$PKG_FAMILY" = "apt" ]; then
    step "Automatic security patches (unattended-upgrades)" \
        "Otomatik güvenlik yamaları (unattended-upgrades)"
    export DEBIAN_FRONTEND=noninteractive
    if apt-get install -y -qq unattended-upgrades >/dev/null 2>&1; then
        # Enable the periodic timer: update lists + apply security upgrades daily.
        # Periyodik zamanlayıcıyı aç: listeleri güncelle + günlük güvenlik yaması.
        cat > /etc/apt/apt.conf.d/20celikpanel-auto-upgrades <<'AUTOCONF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
AUTOCONF
        systemctl enable --now unattended-upgrades >/dev/null 2>&1 || true
        ok "security patches enabled" "güvenlik yamaları etkin"
    else
        warn "unattended-upgrades could not be installed — skipped (it can be installed manually)" \
            "unattended-upgrades kurulamadı — atlandı (elle kurulabilir)"
    fi
elif [[ $APPLY_ONLY -eq 0 ]] && [ "${SKIP_DEPS:-0}" != "1" ] && [ "${SKIP_SECURITY_UPDATES:-0}" != "1" ] && [ "$PKG_FAMILY" = "pacman" ]; then
    # Arch is rolling release: there is no security-only patch channel to
    # subscribe to, so we say so instead of pretending.
    # Arch yuvarlanan sürümdür: abone olunacak güvenlik-yalnız yama kanalı
    # yoktur; öyleymiş gibi yapmak yerine bunu söyleriz.
    step "Automatic security patches" "Otomatik güvenlik yamaları"
    warn "Arch has no security-only channel — automatic patches were not configured; keep the system current with 'pacman -Syu'" \
        "Arch'ta güvenlik-yalnız kanal yok — otomatik yama kurulmadı; sistemi 'pacman -Syu' ile güncel tutun"
fi

# 2. Service user & group ----------------------------------------------------
step "Service user and group" "Servis kullanıcısı ve grubu"
if [[ $APPLY_ONLY -eq 1 ]]; then
    getent group "$SVC_GROUP" >/dev/null || die "apply-only requires existing $SVC_GROUP group"
    id "$SVC_USER" >/dev/null 2>&1 || die "apply-only requires existing $SVC_USER user"
else
    getent group "$SVC_GROUP" >/dev/null || groupadd --system "$SVC_GROUP"
    if ! id "$SVC_USER" >/dev/null 2>&1; then
        useradd --system --gid "$SVC_GROUP" --home-dir "$DATA_DIR" \
            --shell /usr/sbin/nologin "$SVC_USER"
    fi
fi
SVC_GROUP_ID=$(service_group_id) || die "$SVC_GROUP group ID could not be resolved" \
    "$SVC_GROUP grup kimliği çözülemedi"
ok "$SVC_USER:$SVC_GROUP"

# Validate or migrate durable operator choices before building or replacing any
# installed product bytes. A bad root-owned configuration must fail closed
# while the currently installed release is still untouched.
# Kalıcı operatör seçimlerini kurulu ürün baytlarını derlemeden veya
# değiştirmeden önce doğrula/taşı. Bozuk root-only yapılandırma, mevcut sürüme
# dokunulmadan güvenli biçimde kurulumu durdurmalıdır.
step "Panel configuration $PANEL_ENV" "Panel yapılandırması $PANEL_ENV"
if [[ -e "$CONF_DIR" || -L "$CONF_DIR" ]]; then
    [[ -d "$CONF_DIR" && ! -L "$CONF_DIR" ]] || die "panel yapılandırma dizini güvensiz"
    read -r conf_owner conf_group conf_mode < <(stat -Lc '%u %g %a' -- "$CONF_DIR") \
        || die "panel yapılandırma dizini incelenemedi"
    conf_permissions=$((8#$conf_mode))
    [[ "$conf_owner" == 0 ]] \
        && (( (conf_permissions & 0022) == 0 )) \
        || die "panel yapılandırma dizini root sahipli ve group/other yazılamaz olmalı"
fi
install -d -m 0750 -o root -g "$SVC_GROUP" "$CONF_DIR"
ensure_panel_env
ok "ready" "hazır"

# 3. Build if artifacts are missing ------------------------------------------
# A prebuilt release tarball already contains bin/ and web/dist, so this whole
# step is skipped there. From a bare git checkout we build from source,
# bootstrapping the Go and Node toolchains if the system lacks them — so
# "git clone && sudo ./install.sh" works on a stock Ubuntu with nothing else.
#
# Önceden derlenmiş bir release tarball zaten bin/ ve web/dist içerir; orada bu
# adım tümüyle atlanır. Çıplak bir git checkout'tan kaynaktan derleriz;
# sistemde yoksa Go ve Node araç zincirlerini indiririz — böylece stok bir
# Ubuntu'da başka hiçbir şey olmadan "git clone && sudo ./install.sh" çalışır.
GO_VERSION=1.26.5
NODE_VERSION=24.18.0
TOOLCHAIN=/opt/celikpanel/.toolchain
GO_SHA256_AMD64=5c2c3b16caefa1d968a94c1daca04a7ca301a496d9b086e17ad77bb81393f053
GO_SHA256_ARM64=fe4789e92b1f33358680864bbe8704289e7bb5fc207d80623c308935bd696d49
NODE_SHA256_AMD64=55aa7153f9d88f28d765fcdad5ae6945b5c0f98a36881703817e4c450fa76742
NODE_SHA256_ARM64=58c9520501f6ae2b52d5b210444e24b9d0c029a58c5011b797bc1fe7105886f6
TOOLCHAIN_ENV_BIN=/usr/bin/env
TOOLCHAIN_CURL_BIN=/usr/bin/curl
TOOLCHAIN_INSTALL_BIN=/usr/bin/install
TOOLCHAIN_MV_BIN=/usr/bin/mv
TOOLCHAIN_READLINK_BIN=/usr/bin/readlink
TOOLCHAIN_SHA256_BIN=/usr/bin/sha256sum
TOOLCHAIN_STAT_BIN=/usr/bin/stat
TOOLCHAIN_TAR_BIN=/usr/bin/tar

run_external_clean() {
    "$TOOLCHAIN_ENV_BIN" -i \
        HOME=/root \
        PATH=/usr/sbin:/usr/bin:/sbin:/bin \
        LC_ALL=C \
        LANG=C \
        "$@"
}

# Privileged source builds must not inherit Go, compiler or npm configuration
# from sudo's calling environment. Keep the allowlist explicit and disable
# per-user Go configuration/workspace discovery and CGO tool invocation.
run_go_clean() {
    run_external_clean \
        GOTOOLCHAIN=local \
        GOENV=off \
        GOWORK=off \
        GOPATH=/root/go \
        GOCACHE=/root/.cache/go-build \
        CGO_ENABLED=0 \
        "$@"
}

run_node_clean() {
    local node_bin_dir=$1
    shift
    "$TOOLCHAIN_ENV_BIN" -i \
        HOME=/root \
        PATH="$node_bin_dir:/usr/sbin:/usr/bin:/sbin:/bin" \
        LC_ALL=C \
        LANG=C \
        "$@"
}

# Downloaded toolchains are later trusted by the privileged updater. Archive
# metadata must never decide their owner, and an existing tree is reused only
# after every entry has passed the same ownership and write-permission checks.
validate_bootstrap_toolchain_tree() {
    local root=$1 canonical entry link_value target uid gid mode
    ensure_bootstrap_toolchain_root
    case "$root" in
        "$TOOLCHAIN/go"|"$TOOLCHAIN/node"|"$TOOLCHAIN"/.go-stage.*/go|"$TOOLCHAIN"/.node-stage.*) ;;
        *) die "refusing to validate unexpected toolchain path: $root" ;;
    esac
    [ -d "$root" ] && [ ! -L "$root" ] \
        || die "toolchain root must be a real directory: $root"
    canonical=$(readlink -e -- "$root") \
        || die "toolchain root is unavailable: $root"
    [[ "$canonical" == "$root" ]] \
        || die "toolchain root path is not canonical: $root"

    while IFS= read -r -d '' entry; do
        uid=$(stat -c '%u' -- "$entry")
        gid=$(stat -c '%g' -- "$entry")
        [ "$uid:$gid" = 0:0 ] \
            || die "toolchain entry must be owned by root:root: $entry"
        if [ -L "$entry" ]; then
            link_value=$(readlink -- "$entry") || die "cannot inspect toolchain symlink: $entry"
            [[ "$link_value" != /* ]] || die "absolute toolchain symlink is not relocatable: $entry"
            target=$(readlink -f -- "$entry") \
                || die "broken toolchain symlink: $entry"
            [[ "$target" == "$root"/* ]] \
                || die "toolchain symlink escapes its root: $entry"
            continue
        fi
        [ -f "$entry" ] || [ -d "$entry" ] \
            || die "unsupported object in toolchain: $entry"
        mode=$(stat -c '%a' -- "$entry")
        (( (8#$mode & 8#022) == 0 )) \
            || die "toolchain entry must not be group/other writable: $entry"
    done < <(find "$root" -xdev -print0)
}

seal_bootstrap_toolchain_tree() {
    local root=$1
    ensure_bootstrap_toolchain_root
    chown -R -h root:root -- "$root"
    chmod -R go-w -- "$root"
    validate_bootstrap_toolchain_tree "$root"
}

validate_bootstrap_trusted_directory() {
    local directory=$1 canonical uid gid mode
    [[ -d "$directory" && ! -L "$directory" ]] \
        || die "trusted toolchain directory must be a real directory: $directory"
    canonical=$("$TOOLCHAIN_READLINK_BIN" -e -- "$directory") \
        || die "trusted toolchain directory is unavailable: $directory"
    [[ "$canonical" == "$directory" ]] \
        || die "trusted toolchain directory path is not canonical: $directory"
    read -r uid gid mode < <("$TOOLCHAIN_STAT_BIN" -c '%u %g %a' -- "$directory") \
        || die "trusted toolchain directory metadata is unavailable: $directory"
    [[ "$uid:$gid" == 0:0 ]] \
        || die "trusted toolchain directory must be owned by root:root: $directory"
    (( (8#$mode & 8#022) == 0 )) \
        || die "trusted toolchain directory must not be group/other writable: $directory"
}

ensure_bootstrap_toolchain_root() {
    local command_path
    for command_path in "$TOOLCHAIN_ENV_BIN" "$TOOLCHAIN_CURL_BIN" \
        "$TOOLCHAIN_INSTALL_BIN" "$TOOLCHAIN_MV_BIN" "$TOOLCHAIN_READLINK_BIN" \
        "$TOOLCHAIN_SHA256_BIN" "$TOOLCHAIN_STAT_BIN" "$TOOLCHAIN_TAR_BIN"; do
        [[ -f "$command_path" && -x "$command_path" ]] ||
            die "required trusted toolchain command is unavailable: $command_path"
    done

    validate_bootstrap_trusted_directory /
    validate_bootstrap_trusted_directory /usr
    validate_bootstrap_trusted_directory /usr/bin

    if [[ ! -e /opt && ! -L /opt ]]; then
        "$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- /opt
    fi
    validate_bootstrap_trusted_directory /opt

    if [[ ! -e "$PREFIX" && ! -L "$PREFIX" ]]; then
        "$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$PREFIX"
    fi
    validate_bootstrap_trusted_directory "$PREFIX"

    if [[ ! -e "$TOOLCHAIN" && ! -L "$TOOLCHAIN" ]]; then
        "$TOOLCHAIN_INSTALL_BIN" -d -m 0755 -o root -g root -- "$TOOLCHAIN"
    fi
    validate_bootstrap_trusted_directory "$TOOLCHAIN"
}

toolchain_archive_sha256() {
    local product=$1 architecture=$2
    case "$product:$architecture" in
        go:amd64) printf '%s\n' "$GO_SHA256_AMD64" ;;
        go:arm64) printf '%s\n' "$GO_SHA256_ARM64" ;;
        node:amd64) printf '%s\n' "$NODE_SHA256_AMD64" ;;
        node:arm64) printf '%s\n' "$NODE_SHA256_ARM64" ;;
        *) die "unsupported toolchain checksum request: $product/$architecture" ;;
    esac
}

download_verified_toolchain_archive() {
    local url=$1 expected_sha256=$2 archive actual_sha256
    [[ "$expected_sha256" =~ ^[0-9a-f]{64}$ ]] || die "invalid pinned toolchain SHA-256"
    ensure_bootstrap_toolchain_root
    archive=$(mktemp "$TOOLCHAIN/.toolchain-download.XXXXXXXX") || die "cannot create private toolchain download"
    chmod 0600 "$archive"
    chown root:root "$archive"
    if ! run_external_clean "$TOOLCHAIN_CURL_BIN" --disable --proto '=https' --tlsv1.2 \
        --fail --location --retry 3 --show-error --silent --output "$archive" "$url"; then
        rm -f -- "$archive"
        die "toolchain archive could not be downloaded"
    fi
    read -r actual_sha256 _ <<<"$(run_external_clean "$TOOLCHAIN_SHA256_BIN" -- "$archive")" || {
        rm -f -- "$archive"
        die "toolchain archive could not be hashed"
    }
    if [[ "$actual_sha256" != "$expected_sha256" ]]; then
        rm -f -- "$archive"
        die "downloaded toolchain archive SHA-256 mismatch"
    fi
    printf '%s\n' "$archive"
}

# Toolchain download architecture, in Go/Node naming (amd64/arm64). uname -m
# instead of dpkg so this works on every distro.
# Araç zinciri indirme mimarisi, Go/Node adlandırmasıyla (amd64/arm64).
# dpkg yerine uname -m — böylece her dağıtımda çalışır.
dl_arch() {
    case "$(uname -m)" in
        x86_64)  echo amd64 ;;
        aarch64) echo arm64 ;;
        *) die "desteklenmeyen mimari: $(uname -m)" ;;
    esac
}

go_toolchain_version() {
    local candidate=$1
    run_go_clean "$candidate" env GOVERSION 2>/dev/null
}

go_toolchain_is_exact() {
    local candidate=$1 expected_root=$2 version reported_root
    local canonical_candidate canonical_root
    canonical_candidate=$(readlink -e -- "$candidate") || return 1
    canonical_root=$(readlink -e -- "$expected_root") || return 1
    [[ "$canonical_candidate" == "$canonical_root/bin/go" ]] || return 1
    version=$(go_toolchain_version "$candidate") || return 1
    [[ "$version" == "go$GO_VERSION" ]] || return 1
    reported_root=$(run_go_clean "$candidate" env GOROOT 2>/dev/null) || return 1
    [[ "$reported_root" == "$canonical_root" ]]
}

rollback_bootstrap_go_publication() {
    local active_root=$1 retired_root=$2 failed_root=$3
    [[ -d "$retired_root" && ! -L "$retired_root" ]] || return 0
    if [[ -e "$active_root" || -L "$active_root" ]]; then
        if [[ -e "$failed_root" || -L "$failed_root" ]] ||
            ! "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$active_root" "$failed_root"; then
            c '1;31' "URGENT: interrupted Go candidate could not be isolated; previous Go remains at $retired_root" >&2
            return 1
        fi
    fi
    if [[ ! -e "$active_root" && ! -L "$active_root" ]] &&
        ! "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$retired_root" "$active_root"; then
        c '1;31' "URGENT: interrupted Go publication could not restore $retired_root" >&2
        return 1
    fi
}

publish_bootstrap_go_candidate() (
    local candidate_root=$1 retired_root=$2 failed_root=$3 rollback_armed=0
    finish_go_publication() {
        local rc=$?
        trap - EXIT
        if (( rollback_armed == 1 )); then
            rollback_bootstrap_go_publication "$TOOLCHAIN/go" "$retired_root" "$failed_root" || rc=1
        fi
        exit "$rc"
    }
    trap finish_go_publication EXIT
    trap 'exit 129' HUP
    trap 'exit 130' INT
    trap 'exit 143' TERM

    # Arm before the first rename. Cleanup inspects the filesystem, so a
    # signal in either rename window restores the old tree.
    rollback_armed=1
    "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$TOOLCHAIN/go" "$retired_root" || {
        rollback_armed=0
        die "cached Go toolchain could not be safely retired"
    }
    "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$candidate_root" "$TOOLCHAIN/go" ||
        die "Go publication failed; cleanup will restore the cached toolchain"
    seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"
    go_toolchain_is_exact "$TOOLCHAIN/go/bin/go" "$TOOLCHAIN/go" ||
        die "published Go toolchain is not exact go$GO_VERSION"
    rollback_armed=0
    trap - EXIT HUP INT TERM
)

bootstrap_go() {
    local arch expected archive staging path_go cached_go cached_version
    local cached_present=0 retired_go= failed_go=

    # Privileged builds use only the toolchain extracted from the archive whose
    # SHA-256 is pinned above. A same-version PATH binary is not equivalent:
    # its GOROOT/pkg/tool tree is outside this installer's sealed trust root.
    if path_go=$(command -v go 2>/dev/null); then
        warn "PATH Go ignored for privileged build: $path_go" \
            "Ayrıcalıklı derlemede PATH üzerindeki Go yok sayıldı: $path_go" >&2
    fi
    ensure_bootstrap_toolchain_root

    cached_go="$TOOLCHAIN/go/bin/go"
    if [[ -e "$TOOLCHAIN/go" || -L "$TOOLCHAIN/go" ]]; then
        validate_bootstrap_toolchain_tree "$TOOLCHAIN/go"
        [[ -x "$cached_go" ]] || die "cached Go toolchain has no executable go command"
        if go_toolchain_is_exact "$cached_go" "$TOOLCHAIN/go"; then
            printf '%s\n' "$cached_go"
            return
        fi
        cached_present=1
        cached_version=$(go_toolchain_version "$cached_go" || printf '%s' unreadable)
        warn "Cached Go will be retired: $cached_version (required go$GO_VERSION)" \
            "Önbellekteki Go kaldırılacak: $cached_version (gereken go$GO_VERSION)" >&2
    fi
    warn "Downloading Go $GO_VERSION…" "Go $GO_VERSION indiriliyor…" >&2
    arch=$(dl_arch)
    expected=$(toolchain_archive_sha256 go "$arch")
    archive=$(download_verified_toolchain_archive "https://go.dev/dl/go${GO_VERSION}.linux-${arch}.tar.gz" "$expected")
    staging=$(mktemp -d "$TOOLCHAIN/.go-stage.XXXXXXXX") || {
        rm -f -- "$archive"
        die "cannot create Go staging directory"
    }
    chmod 0700 "$staging"
    chown root:root "$staging"
    if ! run_external_clean "$TOOLCHAIN_TAR_BIN" -xz --no-same-owner \
        -C "$staging" --file "$archive"; then
        rm -f -- "$archive"
        rm -rf -- "$staging"
        die "verified Go archive could not be extracted"
    fi
    rm -f -- "$archive"
    if [[ ! -d "$staging/go" || -L "$staging/go" ]]; then
        rm -rf -- "$staging"
        die "verified Go archive has an unexpected layout"
    fi
    if find "$staging" -mindepth 1 -maxdepth 1 ! -name go -print -quit | grep -q .; then
        rm -rf -- "$staging"
        die "verified Go archive contains unexpected top-level entries"
    fi
    seal_bootstrap_toolchain_tree "$staging/go"
    go_toolchain_is_exact "$staging/go/bin/go" "$staging/go" || {
        rm -rf -- "$staging"
        die "verified Go archive does not provide exact go$GO_VERSION"
    }

    # Never recursively delete a previously trusted compiler. Move it to a
    # root-owned retired name, publish the verified replacement atomically,
    # and roll back the move if publication fails. The operator may remove the
    # retired tree later after inspecting it.
    if (( cached_present )); then
        retired_go="$TOOLCHAIN/.go-retired.$(date -u +%Y%m%dT%H%M%SZ).$$"
        failed_go="$TOOLCHAIN/.go-failed.$(date -u +%Y%m%dT%H%M%SZ).$$"
        [[ ! -e "$retired_go" && ! -L "$retired_go" ]] \
            || die "refusing colliding retired Go path: $retired_go"
        [[ ! -e "$failed_go" && ! -L "$failed_go" ]] \
            || die "refusing colliding failed Go path: $failed_go"
        if ! publish_bootstrap_go_candidate "$staging/go" "$retired_go" "$failed_go"; then
            rm -rf -- "$staging"
            die "Go toolchain could not be published safely; inspect retained toolchain paths"
        fi
    else
        "$TOOLCHAIN_MV_BIN" -T --no-clobber -- "$staging/go" "$TOOLCHAIN/go" || {
            rm -rf -- "$staging"
            die "Go toolchain could not be published"
        }
    fi
    rmdir -- "$staging"
    seal_bootstrap_toolchain_tree "$TOOLCHAIN/go"
    go_toolchain_is_exact "$TOOLCHAIN/go/bin/go" "$TOOLCHAIN/go" \
        || die "published Go toolchain is not exact go$GO_VERSION"
    [[ -z "$retired_go" ]] \
        || c '33' "    Previous Go retained for operator review: $retired_go" >&2
    printf '%s\n' "$TOOLCHAIN/go/bin/go"
}

bootstrap_node() {
    local arch archive expected node_archive_arch staging
    command -v npm >/dev/null && { echo "$(command -v node | xargs dirname)"; return; }
    ensure_bootstrap_toolchain_root
    if [ -x "$TOOLCHAIN/node/bin/npm" ]; then
        validate_bootstrap_toolchain_tree "$TOOLCHAIN/node"
        echo "$TOOLCHAIN/node/bin"
        return
    fi
    [[ ! -e "$TOOLCHAIN/node" && ! -L "$TOOLCHAIN/node" ]] || die "incomplete Node toolchain already exists"
    warn "Downloading Node $NODE_VERSION…" "Node $NODE_VERSION indiriliyor…" >&2
    arch=$(dl_arch)
    node_archive_arch=$arch
    [[ "$node_archive_arch" != amd64 ]] || node_archive_arch=x64
    expected=$(toolchain_archive_sha256 node "$arch")
    archive=$(download_verified_toolchain_archive "https://nodejs.org/dist/v${NODE_VERSION}/node-v${NODE_VERSION}-linux-$node_archive_arch.tar.xz" "$expected")
    staging=$(mktemp -d "$TOOLCHAIN/.node-stage.XXXXXXXX") || {
        rm -f -- "$archive"
        die "cannot create Node staging directory"
    }
    chmod 0700 "$staging"
    chown root:root "$staging"
    if ! run_external_clean "$TOOLCHAIN_TAR_BIN" -xJ --no-same-owner \
        -C "$staging" --strip-components=1 --file "$archive"; then
        rm -f -- "$archive"
        rm -rf -- "$staging"
        die "verified Node archive could not be extracted"
    fi
    rm -f -- "$archive"
    chown -R -h root:root -- "$staging"
    chmod -R go-w -- "$staging"
    validate_bootstrap_toolchain_tree "$staging"
    mv -T --no-clobber -- "$staging" "$TOOLCHAIN/node" || {
        rm -rf -- "$staging"
        die "Node toolchain could not be published"
    }
    seal_bootstrap_toolchain_tree "$TOOLCHAIN/node"
    echo "$TOOLCHAIN/node/bin"
}

# Version stamped into BOTH binaries and the frontend, from one source: the
# git description of the commit being installed. Before this the footer showed
# a hand-typed "v0.1.0" that no build could change, so "which release is this
# server running?" had no honest answer.
# Sürüm, kurulan commit'in git tanımından TEK kaynaktan alınır ve HER İKİ
# binary ile ön yüze gömülür. Bundan önce footer, hiçbir derlemenin
# değiştiremediği elle yazılmış bir "v0.1.0" gösteriyordu; yani "bu sunucu
# hangi sürümü koşuyor?" sorusunun dürüst bir cevabı yoktu.
CP_VERSION=$(cd "$SRC" && git describe --tags --always --dirty 2>/dev/null || echo dev)
CP_COMMIT=$(cd "$SRC" && git rev-parse --short HEAD 2>/dev/null || echo unknown)
VER_FLAGS="-X main.buildVersion=$CP_VERSION -X main.buildCommit=$CP_COMMIT"

# A git checkout ALWAYS rebuilds. bin/ and web/dist are gitignored, so they
# survive `git pull` — and the old "skip the build if artifacts exist" test
# then installed the PREVIOUS build while update.sh printed "services
# restarted with the new build". A stale binary that reports the new release
# is worse than no version at all, especially when the new release is a
# security fix. Only a prebuilt release tarball (no .git) may skip.
# Bir git checkout HER ZAMAN yeniden derler. bin/ ve web/dist git'te olmadığı
# için `git pull`dan sağ çıkar — ve eski "ürünler varsa derlemeyi atla"
# denetimi, update.sh "servisler yeni yapıyla yeniden başladı" yazarken BİR
# ÖNCEKİ yapıyı kuruyordu. Yeni sürümü bildiren bayat bir binary, hiç sürüm
# olmamasından kötüdür; hele yeni sürüm bir güvenlik düzeltmesiyse. Yalnız
# önceden derlenmiş release tarball'ı (.git yok) atlayabilir.
if [ -d "$SRC/.git" ] || [ ! -x "$SRC/bin/panel" ] || [ ! -x "$SRC/bin/agent" ] || [ ! -f "$SRC/web/dist/index.html" ]; then
    step "Building from source (bin/panel, bin/agent, web/dist) — version $CP_VERSION" \
        "Kaynaktan derleme (bin/panel, bin/agent, web/dist) — sürüm $CP_VERSION"
    GO_BIN=$(bootstrap_go)
    NODE_BIN=$(bootstrap_node)
    ( cd "$SRC" && run_go_clean "$GO_BIN" build -trimpath -buildvcs=false -ldflags "-s -w $VER_FLAGS" -o bin/panel ./cmd/panel ) || die "Panel build failed" "Panel derlenemedi"
    ( cd "$SRC" && run_go_clean "$GO_BIN" build -trimpath -buildvcs=false -ldflags "-s -w $VER_FLAGS" -o bin/agent ./cmd/agent ) || die "Agent build failed" "Agent derlenemedi"
    ( cd "$SRC/web" && run_node_clean "$NODE_BIN" "$NODE_BIN/npm" ci --no-audit --no-fund >/dev/null 2>&1 ) || die "npm installation failed" "npm kurulumu başarısız"
    ( cd "$SRC/web" && run_node_clean "$NODE_BIN" "$NODE_BIN/npm" run build >/dev/null ) || die "Frontend build failed" "Frontend derlenemedi"
    ok "built ($CP_VERSION · $CP_COMMIT)" "derlendi ($CP_VERSION · $CP_COMMIT)"
else
    ok "Using the prebuilt release (bin/ + web/dist)" \
        "Önceden derlenmiş sürüm kullanılıyor (bin/ + web/dist)"
fi

# 4. Install files -----------------------------------------------------------
step "Installing files under $PREFIX" "Dosyalar $PREFIX altına kuruluyor"
install -d -m 0755 "$PREFIX/bin" "$PREFIX/web" "$PREFIX/runtimes"
install -m 0755 "$SRC/bin/panel" "$PREFIX/bin/panel"
install -m 0755 "$SRC/bin/agent" "$PREFIX/bin/agent"

# Replace the exact fixed web root, including hidden entries and empty stale
# directories. Canonical root-owned boundaries are proven before -delete runs.
# Gizli girdiler ve boş bayat dizinler dahil tam sabit web root'unu değiştir.
# -delete çalışmadan önce kanonik root-sahipli sınırlar kanıtlanır.
installed_web_root="$PREFIX/web"
[[ "$PREFIX" == /opt/celikpanel && "$installed_web_root" == /opt/celikpanel/web ]] \
    || die "web kurulum root sınırı beklenmedik"
for install_root in /opt "$PREFIX" "$installed_web_root"; do
    [[ -d "$install_root" && ! -L "$install_root" ]] \
        || die "güvensiz web kurulum sınırı: $install_root"
    install_canonical=$(readlink -e -- "$install_root") \
        || die "web kurulum sınırı çözümlenemedi: $install_root"
    [[ "$install_canonical" == "$install_root" ]] \
        || die "web kurulum sınırı alias içeriyor: $install_root"
    read -r install_owner install_group install_mode < <(stat -Lc '%u %g %a' -- "$install_root") \
        || die "web kurulum sınırı incelenemedi: $install_root"
    install_permissions=$((8#$install_mode))
    [[ "$install_owner" == 0 && "$install_group" == 0 ]] && \
        (( (install_permissions & 0022) == 0 )) \
        || die "web kurulum sınırı root:root ve group/other yazılamaz olmalı: $install_root"
done
[[ -d "$SRC/web/dist" && ! -L "$SRC/web/dist" ]] \
    || die "web kaynak ağacı eksik veya güvensiz"
if find "$SRC/web/dist" -type l -print -quit | grep -q .; then
    die "web kaynak ağacı sembolik bağlantı içeriyor"
fi
if find "$SRC/web/dist" ! -type d ! -type f -print -quit | grep -q .; then
    die "web kaynak ağacı özel dosya sistemi nesnesi içeriyor"
fi
find "$installed_web_root" -xdev -mindepth 1 -depth -delete \
    || die "eski web ağacı tam temizlenemedi"
if find "$installed_web_root" -mindepth 1 -print -quit | grep -q .; then
    die "eski web ağacı temizlendikten sonra girdi kaldı"
fi
cp -a "$SRC/web/dist/." "$installed_web_root/"
# Static assets are public product bytes, not secrets. Do not preserve a
# caller's restrictive umask (for example 0077) through the build directory
# and cp -a: the unprivileged panel process must traverse the complete tree
# and read every asset. Normalize the already proven symlink-free tree and
# verify it before any service is started.
chown -R root:root -- "$installed_web_root"
find "$installed_web_root" -xdev -type d -exec chmod 0755 -- {} + \
    || die "installed web directory permissions could not be normalized"
find "$installed_web_root" -xdev -type f -exec chmod 0644 -- {} + \
    || die "installed web file permissions could not be normalized"
if find "$installed_web_root" -xdev -type d ! -perm 0755 -print -quit | grep -q .; then
    die "installed web directory permissions could not be verified"
fi
if find "$installed_web_root" -xdev -type f ! -perm 0644 -print -quit | grep -q .; then
    die "installed web file permissions could not be verified"
fi
[[ -f "$installed_web_root/index.html" && ! -L "$installed_web_root/index.html" ]] \
    || die "kurulu web index ürünü eksik veya güvensiz"
# Runtimes dir is where the agent installs Node versions; group-owned so the
# root agent writes and the panel can stat.
# Runtimes dizini agent'ın Node sürümlerini kurduğu yerdir; grup-sahipli.
chown -R root:"$SVC_GROUP" "$PREFIX/runtimes"
chmod 0775 "$PREFIX/runtimes"
ok "installed" "kuruldu"

# 5. Data directory (SQLite lives here; StateDirectory also ensures it) ------
step "Data directory $DATA_DIR" "Veri dizini $DATA_DIR"
install -d -m 0750 -o "$SVC_USER" -g "$SVC_GROUP" "$DATA_DIR"
# Privileged imports must not live below the panel-owned data directory. The
# root agent accepts only owner-only regular files from this root; the
# unprivileged panel merely forwards the operator-selected path.
install -d -m 0700 -o root -g root "$IMPORT_DIR"
ok "ready" "hazır"

# 6. systemd units -----------------------------------------------------------
step "systemd services" "systemd servisleri"
install -m 0644 "$SRC/deploy/systemd/celikpanel-agent.service" /etc/systemd/system/
install -m 0644 "$SRC/deploy/systemd/celikpanel-firewall-restore.service" /etc/systemd/system/
install -m 0644 "$SRC/deploy/systemd/celikpanel-panel.service" /etc/systemd/system/
# Operator choices live in root-only /etc/celikpanel/panel.env. Reinstall and
# update always replace the vendor unit, never that durable configuration.
# Operatör seçimleri root-only /etc/celikpanel/panel.env içindedir. Yeniden
# kurulum ve güncelleme üretici unitini yeniler; kalıcı ayara dokunmaz.
if [[ "$VALIDATED_PANEL_HTTPS" == 0 ]]; then
    warn "R&D mode: demo accounts are enabled and cookies work over plain HTTP — do not expose this server to the internet" \
        "AR-GE modu: demo hesaplar açık, çerezler düz HTTP'de çalışır — internete açmayın"
fi
systemctl daemon-reload
ok "installed" "kuruldu"

# Apply-only ends after immutable layout and ledger metadata are verified. It
# never initializes state, reconciles firewall, enables/starts services, waits
# for sockets, creates admins, or runs panel migrations.
# Apply-only değişmez yerleşim ve ledger metadata doğrulamasından sonra biter.
# State başlatmaz, firewall uzlaştırmaz, servis açıp başlatmaz, socket beklemez,
# admin oluşturmaz veya panel migration çalıştırmaz.
if [[ $APPLY_ONLY -eq 1 ]]; then
    [[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] || die "apply-only durable agent ledger is missing"
    read -r ledger_owner ledger_group ledger_mode < <(stat -Lc '%u %g %a' -- "$AGENT_LEDGER") || die "apply-only cannot inspect agent ledger"
    [[ "$ledger_owner" == 0 && "$ledger_group" == "$SVC_GROUP_ID" && "$ledger_mode" == 600 ]] || die "apply-only agent ledger metadata mismatch"
    sync -f -- "$PREFIX/bin/panel" "$PREFIX/bin/agent" "$PREFIX/bin" "$PREFIX/web" \
        "$PANEL_ENV" "$CONF_DIR" /etc/systemd/system \
        || die "apply-only installed layout could not be made durable"
    ok "apply-only layout completed; services were left stopped" \
        "apply-only yerleşim tamamlandı; servisler kapalı bırakıldı"
    exit 0
fi

# 7. Start the agent (generates the shared token on first run) ---------------
# restart, not enable --now: an upgrade must actually load the new binary;
# --now is a no-op when the service is already running.
# enable --now değil restart: yükseltme yeni binary'yi gerçekten yüklemeli;
# servis zaten çalışıyorsa --now hiçbir şey yapmaz.
step "Starting the agent" "Agent başlatılıyor"
if [[ $initialize_ledger -eq 1 ]]; then
    [[ ! -e "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
        || die "servis işlem ledger başlatmadan önce mevcut"
    mutation_lock_dir=$(dirname "$MUTATION_LOCK")
    if [[ -e "$mutation_lock_dir" || -L "$mutation_lock_dir" ]]; then
        [[ -d "$mutation_lock_dir" && ! -L "$mutation_lock_dir" ]] \
            || die "güvensiz servis işlem kilit dizini: $mutation_lock_dir"
        read -r lock_owner lock_mode < <(stat -Lc '%u %a' -- "$mutation_lock_dir") \
            || die "servis işlem kilit dizini incelenemedi"
        lock_permissions=$((8#$lock_mode))
        [[ "$lock_owner" == 0 ]] && (( (lock_permissions & 0022) == 0 )) \
            || die "servis işlem kilit dizini root sahipli ve group/other yazılamaz olmalı"
    fi
    install -d -m 0700 -o root -g "$SVC_GROUP" -- "$AGENT_STATE_DIR"
    install -d -m 0750 -o root -g "$SVC_GROUP" -- "$mutation_lock_dir"
    CELIKPANEL_AGENT_STATE_DIR="$AGENT_STATE_DIR" \
    CELIKPANEL_MUTATION_LOCK="$MUTATION_LOCK" \
        "$PREFIX/bin/agent" --initialize-service-mutation-ledger \
        || die "servis işlem ledger başlatılamadı"
fi
[[ -f "$AGENT_LEDGER" && ! -L "$AGENT_LEDGER" ]] \
    || die "servis işlem ledger eksik veya güvensiz"
read -r ledger_owner ledger_group ledger_mode < <(stat -Lc '%u %g %a' -- "$AGENT_LEDGER") \
    || die "servis işlem ledger incelenemedi"
[[ "$ledger_owner" == 0 && "$ledger_group" == "$SVC_GROUP_ID" && "$ledger_mode" == 600 ]] \
    || die "servis işlem ledger root:$SVC_GROUP mode 0600 olmalı"
# A fresh install only lays down the restore unit. Existing persistence is
# re-enabled during upgrade, but no activation link is created before the
# operator explicitly chooses Save for reboot in the panel.
# Temiz kurulum yalnız restore unitini yerleştirir. Mevcut kalıcılık yükseltmede
# yeniden etkinleştirilir; operatör panelde açıkça Save for reboot seçmeden
# hiçbir etkinleştirme bağlantısı oluşturulmaz.
/bin/bash "$SRC/deploy/systemd/enable-firewall-restore-if-saved.sh" "$CONF_DIR/firewall.nft" || \
    die "firewall restore unit could not be reconciled"
systemctl enable celikpanel-agent.service >/dev/null 2>&1 || true
systemctl restart celikpanel-agent.service || \
    die "The agent could not be restarted — inspect 'journalctl -u celikpanel-agent'" \
        "Agent yeniden başlatılamadı — 'journalctl -u celikpanel-agent' inceleyin"
systemctl is-active --quiet celikpanel-agent.service || \
    die "The agent is not active — inspect 'journalctl -u celikpanel-agent'" \
        "Agent aktif değil — 'journalctl -u celikpanel-agent' inceleyin"
for _ in $(seq 1 20); do
    [ -S /run/celikpanel/agent.sock ] && break
    sleep 0.3
done
[ -S /run/celikpanel/agent.sock ] || die \
    "The agent socket was not created — inspect 'journalctl -u celikpanel-agent'" \
    "Agent socket oluşmadı — 'journalctl -u celikpanel-agent' inceleyin"
ok "agent is running" "agent çalışıyor"

# 8. First administrator -----------------------------------------------------
if [ "${SKIP_ADMIN:-0}" != "1" ]; then
    if run_panel_as_service_user_with_private_umask --count-users 2>/dev/null | grep -q '^0$'; then
        step "Creating the first administrator" "İlk yönetici oluşturuluyor"
        run_panel_as_service_user_with_private_umask --create-admin || \
            die "Administrator creation failed" "Yönetici oluşturma başarısız"
        ok "administrator is ready" "yönetici hazır"
    else
        ok "An administrator already exists — skipped" \
            "Yönetici zaten var — atlandı"
    fi
fi

# 9. Start the panel ---------------------------------------------------------
step "Starting the panel" "Panel başlatılıyor"
systemctl enable celikpanel-panel.service >/dev/null 2>&1 || true
systemctl restart celikpanel-panel.service || \
    die "The panel could not be restarted — inspect 'journalctl -u celikpanel-panel'" \
        "Panel yeniden başlatılamadı — 'journalctl -u celikpanel-panel' inceleyin"
sleep 1
systemctl is-active --quiet celikpanel-panel.service || \
    die "The panel did not start — inspect 'journalctl -u celikpanel-panel'" \
        "Panel başlamadı — 'journalctl -u celikpanel-panel' inceleyin"
ok "panel is running" "panel çalışıyor"

# Record completion only after both services and the administrator path have
# succeeded. The public bootstrapper uses this root-only marker to distinguish
# a finished installation from a failed first-admin attempt.
install -m 0600 -o root -g root /dev/null "$INSTALL_COMPLETE"
sync -f "$INSTALL_COMPLETE" "$CONF_DIR"

# 10. Done -------------------------------------------------------------------
IP=""
if command -v hostname >/dev/null 2>&1; then
    IP="$(hostname -I 2>/dev/null | awk '{print $1}' || true)"
fi
PORT="${VALIDATED_PANEL_LISTEN##*:}"
PANEL_SCHEME=https
[[ "$VALIDATED_PANEL_HTTPS" == 1 ]] || PANEL_SCHEME=http
echo
c '1;32' "CelikPanel was installed successfully. / CelikPanel başarıyla kuruldu."
echo "    Panel:  ${PANEL_SCHEME}://${IP:-SUNUCU_IP}:${PORT}"
echo "    Services / Servisler: systemctl status celikpanel-agent celikpanel-panel"
echo "    Logs / Günlükler: journalctl -u celikpanel-panel -f"
