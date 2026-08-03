#!/bin/bash
# Static privileged-release contract: fail if immutable staging, fail-closed
# mutation handling or the one-shot ledger gate loses a mandatory invariant.
# Statik ayrıcalıklı-sürüm sözleşmesi: değişmez staging, kapalı hata veren
# mutation yönetimi veya tek seferlik ledger kapısı zorunlu invariant kaybederse dur.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
BOOTSTRAP="$ROOT/bootstrap-update.sh"
UPDATE="$ROOT/update.sh"
ROLLBACK="$ROOT/rollback.sh"
INSTALL="$ROOT/install.sh"
MAKEFILE="$ROOT/Makefile"
RELEASE_GUARD="$ROOT/deploy/release-transaction-guard.sh"
PANEL_UNIT="$ROOT/deploy/systemd/celikpanel-panel.service"
AGENT_UNIT="$ROOT/deploy/systemd/celikpanel-agent.service"

die() {
    echo "bootstrap update contract failed: $*" >&2
    exit 1
}

require_literal() {
    local file=$1 literal=$2
    grep -Fq -- "$literal" "$file" || die "$(basename "$file") is missing: $literal"
}

reject_literal() {
    local file=$1 literal=$2
    ! grep -Fq -- "$literal" "$file" || die "$(basename "$file") must not contain: $literal"
}

require_count() {
    local file=$1 literal=$2 expected=$3 actual
    actual=$(grep -F -c -- "$literal" "$file" || true)
    [[ "$actual" == "$expected" ]] \
        || die "$(basename "$file") count for '$literal' is $actual, want $expected"
}

require_regex_count() {
    local file=$1 regex=$2 expected=$3 actual
    actual=$(grep -E -c -- "$regex" "$file" || true)
    [[ "$actual" == "$expected" ]] \
        || die "$(basename "$file") regex count for '$regex' is $actual, want $expected"
}

# Find every ordered literal after the previous match so helper definitions do
# not satisfy a later runtime gate accidentally.
# Her sıralı literal'i önceki eşleşmeden sonra bul; helper tanımları sonraki
# runtime kapısını yanlışlıkla karşılamasın.
require_sequence() {
    local file=$1 cursor=0 literal line
    shift
    for literal in "$@"; do
        line=$({ grep -Fn -- "$literal" "$file" || true; } \
            | awk -F: -v cursor="$cursor" '$1 > cursor { print $1; exit }')
        [[ "$line" =~ ^[0-9]+$ ]] \
            || die "$(basename "$file") has no ordered marker after line $cursor: $literal"
        cursor=$line
    done
}

# Extract exactly one top-level shell function so rollback security assertions
# cannot be satisfied accidentally by a helper definition or another branch.
extract_function_source() {
    local file=$1 function_name=$2
    awk -v header="$function_name() {" '
        $0 == header {
            inside = 1
            found = 1
        }
        inside {
            print
        }
        inside && $0 == "}" {
            closed = 1
            exit
        }
        END {
            if (!found || !closed) {
                exit 1
            }
        }
    ' "$file"
}

require_function_literal() {
    local file=$1 function_name=$2 literal=$3 source
    source=$(extract_function_source "$file" "$function_name") \
        || die "$(basename "$file") has no complete function: $function_name"
    grep -Fq -- "$literal" <<< "$source" \
        || die "$function_name is missing: $literal"
}

reject_function_literal() {
    local file=$1 function_name=$2 literal=$3 source
    source=$(extract_function_source "$file" "$function_name") \
        || die "$(basename "$file") has no complete function: $function_name"
    ! grep -Fq -- "$literal" <<< "$source" \
        || die "$function_name must not contain: $literal"
}

require_function_sequence() {
    local file=$1 function_name=$2 source cursor=0 literal line
    shift 2
    source=$(extract_function_source "$file" "$function_name") \
        || die "$(basename "$file") has no complete function: $function_name"
    for literal in "$@"; do
        line=$({ grep -Fn -- "$literal" <<< "$source" || true; } \
            | awk -F: -v cursor="$cursor" '$1 > cursor { print $1; exit }')
        [[ "$line" =~ ^[0-9]+$ ]] \
            || die "$function_name has no ordered marker after line $cursor: $literal"
        cursor=$line
    done
}

require_exact_sequence() {
    local file=$1 cursor=0 literal line
    shift
    for literal in "$@"; do
        line=$({ grep -Fnx -- "$literal" "$file" || true; } \
            | awk -F: -v cursor="$cursor" '$1 > cursor { print $1; exit }')
        [[ "$line" =~ ^[0-9]+$ ]] \
            || die "$(basename "$file") has no exact ordered line after line $cursor: $literal"
        cursor=$line
    done
}

# Count only saved-active agent branches that perform the complete controlled
# lock handoff. Because every handoff call is counted separately below, this
# also proves that no handoff can run on the saved-inactive path.
require_active_agent_handoff_blocks() {
    local file=$1 expected=$2 actual
    actual=$(awk '
        BEGIN { inside = 0; count = 0 }
        /^[[:space:]]*if service_state_is_active_like "\$\{saved_active_states\[celikpanel-agent\.service\]\}"; then[[:space:]]*$/ {
            inside = 1
            released = 0
            started = 0
            waited = 0
            handed_off = 0
            next
        }
        inside {
            if (index($0, "release_release_mutation_lock") > 0) released = 1
            if (index($0, "systemctl start celikpanel-agent.service") > 0) started = 1
            if ($0 ~ /^[[:space:]]*wait_for_fresh_active_agent[[:space:]]*$/) waited = 1
            if ($0 ~ /^[[:space:]]*acquire_release_mutation_lock handoff[[:space:]]*$/) handed_off = 1
            if ($0 ~ /^[[:space:]]*fi[[:space:]]*$/) {
                if (released && started && waited && handed_off) count++
                inside = 0
            }
        }
        END { print count }
    ' "$file")
    [[ "$actual" == "$expected" ]] \
        || die "$(basename "$file") active agent handoff block count is $actual, want $expected"
}

bash -n "$BOOTSTRAP" "$UPDATE" "$ROLLBACK" "$INSTALL" "$RELEASE_GUARD"

# Fresh bootstrap and all later panel starts must create the SQLite database
# and its WAL/SHM sidecars with private permissions.
# Temiz bootstrap ve sonraki tüm panel başlangıçları SQLite veritabanı ile
# WAL/SHM yan dosyalarını özel izinlerle oluşturmalıdır.
require_literal "$PANEL_UNIT" 'UMask=0077'
require_literal "$PANEL_UNIT" 'Wants=celikpanel-agent.service'
require_literal "$PANEL_UNIT" 'After=celikpanel-agent.service network.target'
reject_literal "$PANEL_UNIT" 'Requires=celikpanel-agent.service'
require_literal "$PANEL_UNIT" 'EnvironmentFile=-/etc/celikpanel/panel.env'
require_literal "$PANEL_UNIT" 'ExecStart=/opt/celikpanel/bin/panel $CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG $CELIKPANEL_PANEL_DEMO_FLAG'
require_literal "$AGENT_UNIT" 'RuntimeDirectoryPreserve=yes'
require_literal "$INSTALL" 'run_panel_as_service_user_with_private_umask() {'
require_literal "$INSTALL" '/bin/sh -c '\''umask 077; exec "$@"'\'' celikpanel-install "$PREFIX/bin/panel" "$@"'
require_count "$INSTALL" 'run_panel_as_service_user_with_private_umask --count-users' 1
require_count "$INSTALL" 'run_panel_as_service_user_with_private_umask --create-admin' 1
reject_literal "$INSTALL" 'CELIKPANEL_DATA_DIR="$DATA_DIR" "$PREFIX/bin/panel" --count-users'
reject_literal "$INSTALL" 'CELIKPANEL_DATA_DIR="$DATA_DIR" "$PREFIX/bin/panel" --create-admin'

# Operator choices are durable data, not generated vendor-unit bytes. A clean
# agent restart must not tear down the otherwise healthy web panel.
require_literal "$INSTALL" 'PANEL_ENV="$CONF_DIR/panel.env"'
require_literal "$INSTALL" 'validate_panel_env "$PANEL_ENV"'
require_literal "$INSTALL" 'mv -T --no-clobber -- "$temp_env" "$PANEL_ENV"'
require_literal "$INSTALL" 'install -m 0644 "$SRC/deploy/systemd/celikpanel-panel.service" /etc/systemd/system/'
require_sequence "$INSTALL" \
    'step "Panel configuration $PANEL_ENV" "Panel yapılandırması $PANEL_ENV"' \
    'ensure_panel_env' \
    '# 3. Build if artifacts are missing' \
    'step "Installing files under $PREFIX" "Dosyalar $PREFIX altına kuruluyor"'
reject_literal "$INSTALL" 'PANEL_ARGS='
reject_literal "$INSTALL" 'TLS_ENV='
reject_literal "$INSTALL" 's|^ExecStart=/opt/celikpanel/bin/panel'

# Arch package databases and the base system must advance in one transaction;
# neither the installer nor the agent may create a partial-upgrade window.
require_literal "$INSTALL" 'pacman -Syu --noconfirm --needed tar xz curl ca-certificates nftables'
reject_literal "$INSTALL" 'pacman -Sy --noconfirm'

# The Makefile artifact contains the complete offline initial-install payload.
# Updates and rollbacks still use the immutable bootstrap transaction path.
require_literal "$MAKEFILE" 'build: panel agent schema17-bridge web'
require_literal "$MAKEFILE" '$(NPM) ci --no-audit --no-fund'
reject_literal "$MAKEFILE" '$(NPM) install --no-audit --no-fund'
require_literal "$MAKEFILE" 'cp bin/panel bin/agent bin/schema17-bridge dist/$(DIST)/bin/'
require_literal "$MAKEFILE" 'cp -r deploy/. dist/$(DIST)/deploy/'
require_literal "$MAKEFILE" 'cp install.sh bootstrap-update.sh update.sh rollback.sh Makefile README.md SECURITY.md NOTICE dist/$(DIST)/'
require_literal "$MAKEFILE" 'sha256sum "$(DIST).tar.gz" > "$(DIST).tar.gz.sha256"'
require_literal "$MAKEFILE" 'dist-sign: dist'
require_literal "$MAKEFILE" 'SIGNING_KEY is required (GPG key ID or fingerprint)'
require_literal "$MAKEFILE" 'gpg --batch --yes --armor --local-user "$(SIGNING_KEY)" --detach-sign'

# Arch's compatibility sbin/bin aliases must not win command resolution over
# the canonical bin directories used by the trust-chain checker.
require_literal "$BOOTSTRAP" 'PATH=/usr/local/bin:/usr/bin:/usr/local/sbin:/usr/sbin:/bin:/sbin'
reject_literal "$BOOTSTRAP" 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'

# One clean reviewed commit must produce one root-only release for both modes.
# Tek temiz incelenmiş commit iki mod için tek root-only sürüm üretmelidir.
require_literal "$BOOTSTRAP" '#!/bin/bash'
require_literal "$BOOTSTRAP" 'RELEASES_ROOT=/var/backups/celikpanel/releases'
require_literal "$BOOTSTRAP" '[[ $# -eq 1 ]] || die "usage: bootstrap-update.sh --normal|--bootstrap-pre-ledger|--bootstrap-schema17"'
require_literal "$BOOTSTRAP" '--normal|--bootstrap-pre-ledger|--bootstrap-schema17) UPDATE_MODE=$1'
reject_literal "$BOOTSTRAP" 'UPDATE_MODE=--bootstrap-pre-ledger'
require_literal "$BOOTSTRAP" 'git_bin=$(trusted_command git)'
require_literal "$BOOTSTRAP" 'tar_bin=$(trusted_command tar)'
require_literal "$BOOTSTRAP" 'validate_root_owned_regular_tree "$source_root/.git"'
require_literal "$BOOTSTRAP" 'run_clean "$git_bin" status --porcelain=v1 --untracked-files=all'
require_literal "$BOOTSTRAP" 'run_clean "$git_bin" archive --format=tar HEAD'
require_literal "$BOOTSTRAP" 'run_clean "$tar_bin" -xf - -C "$incomplete_root"'
require_literal "$BOOTSTRAP" 'release_root="$RELEASES_ROOT/$release_short-$release_nonce"'
require_literal "$BOOTSTRAP" '[[ "$release_nonce" =~ ^[0-9a-f]{24}$ ]]'
require_literal "$BOOTSTRAP" 'version_flags="-X main.buildVersion=$release_version -X main.buildCommit=$release_commit"'
require_literal "$BOOTSTRAP" '! -path '\''./SHA256SUMS'\'' -print0'
require_literal "$BOOTSTRAP" '[[ -x "$root/rollback.sh" && -f "$root/rollback.sh" ]]'
require_literal "$BOOTSTRAP" '[[ -x "$root/bin/schema17-bridge" && -f "$root/bin/schema17-bridge" ]]'
require_literal "$BOOTSTRAP" '"$incomplete_root/rollback.sh"'
require_literal "$BOOTSTRAP" 'mv -T --no-clobber -- "$incomplete_root" "$release_root"'
require_literal "$BOOTSTRAP" '/bin/bash "$release_root/update.sh" "$UPDATE_MODE"'
reject_literal "$BOOTSTRAP" '/bootstrap-releases'
require_sequence "$BOOTSTRAP" \
    'validate_root_owned_regular_tree "$source_root/.git"' \
    'run_clean "$git_bin" status --porcelain=v1 --untracked-files=all' \
    'run_clean "$git_bin" archive --format=tar HEAD' \
    'run_clean "$tar_bin" -xf - -C "$incomplete_root"' \
    'build -trimpath -buildvcs=false -ldflags "-s -w $version_flags" -o bin/panel ./cmd/panel' \
    'build -trimpath -buildvcs=false -ldflags "-s -w" -o bin/schema17-bridge ./deploy/schema17bridge' \
    '"$npm_bin" run build' \
    'validate_release_tree "$incomplete_root"' \
    'mv -T --no-clobber -- "$incomplete_root" "$release_root"' \
    'validate_release_tree "$release_root"' \
    '/bin/bash "$release_root/update.sh" "$UPDATE_MODE"'

# The staged release and both publication directories must be durable before
# control is handed to update.sh.
# Staged sürüm ve iki yayın dizini de denetim update.sh'ye devredilmeden önce
# kalıcılaştırılmalıdır.
require_literal "$BOOTSTRAP" 'find "$root" -type f -exec sync -f -- {} \;'
require_literal "$BOOTSTRAP" 'find "$root" -depth -type d -exec sync -f -- {} \;'
require_sequence "$BOOTSTRAP" \
    'sync_release_tree_durably() {' \
    'validate_release_tree "$root"' \
    'find "$root" -type f -exec sync -f -- {} \;' \
    'find "$root" -depth -type d -exec sync -f -- {} \;' \
    'sync -f -- "$root" "$RELEASES_ROOT"' \
    'validate_release_tree "$root"' \
    'validate_release_tree "$incomplete_root"' \
    'sync_release_tree_durably "$incomplete_root"' \
    'mv -T --no-clobber -- "$incomplete_root" "$release_root"' \
    '[[ ! -e "$incomplete_root" && -d "$release_root" ]]' \
    'sync -f -- "$release_root" "$RELEASES_ROOT"' \
    'incomplete_root=""' \
    'validate_release_tree "$release_root"' \
    '/bin/bash "$release_root/update.sh" "$UPDATE_MODE"'

# update.sh consumes only the immutable staged release. All executable/tool
# preflight checks happen before snapshot-root mutation.
# update.sh yalnız değişmez staged sürümü tüketir. Tüm çalıştırılabilir/araç
# preflight kontrolleri snapshot-root mutation işleminden önce tamamlanır.
require_literal "$UPDATE" '#!/bin/bash'
require_literal "$UPDATE" 'RELEASES_ROOT=/var/backups/celikpanel/releases'
require_literal "$UPDATE" '[[ $# -eq 1 ]] || die "usage: update.sh --normal|--bootstrap-pre-ledger|--bootstrap-schema17"'
require_literal "$UPDATE" '--bootstrap-pre-ledger)'
require_literal "$UPDATE" '--bootstrap-schema17)'
require_literal "$UPDATE" '--normal) ;;'
require_literal "$UPDATE" '[[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]]'
require_literal "$UPDATE" '[[ "$updater" == "$root/update.sh" ]]'
require_literal "$UPDATE" '! -path '\''./SHA256SUMS'\'' -print0'
require_count "$UPDATE" 'preflight_staged_installer_runtime' 2
reject_literal "$UPDATE" 'grep hostname id install'
require_literal "$INSTALL" 'if command -v hostname >/dev/null 2>&1; then'
require_literal "$INSTALL" 'hostname -I 2>/dev/null | awk '\''{print $1}'\'' || true'
require_count "$UPDATE" 'prepare_snapshot_root' 2
require_count "$UPDATE" 'validate_trusted_release' 2
require_exact_sequence "$UPDATE" \
    'preflight_staged_installer_runtime' \
    'prepare_snapshot_root' \
    'validate_trusted_release' \
    'cd "$update_root"' \
    'validate_preflight_binary "$PREFLIGHT_PANEL" panel'
reject_literal "$UPDATE" 'git pull'
reject_literal "$UPDATE" 'repo_owner='
reject_literal "$UPDATE" ' ./install.sh'

# The database snapshot is created transactionally by the trusted staged panel;
# it is one standalone SQLite file and never a shell copy plus sidecars.
# Veritabanı snapshot'ı güvenilir staged panel tarafından transaction ile alınır;
# tek bağımsız SQLite dosyasıdır, shell kopyası ve sidecar birleşimi değildir.
require_literal "$UPDATE" 'snapshot_schema=normal'
require_literal "$UPDATE" '[[ $BOOTSTRAP_PRE_LEDGER -ne 1 ]] || snapshot_schema=pre-ledger'
require_literal "$UPDATE" '[[ $BOOTSTRAP_SCHEMA17 -ne 1 ]] || snapshot_schema=schema17'
require_literal "$UPDATE" 'RECOVERY_SNAPSHOT_ROOT=/var/backups/celikpanel/recovery-snapshots'
require_literal "$UPDATE" 'recovery snapshot root must be root:root mode 0700'
require_literal "$UPDATE" 'exact recovery snapshot directory must be root:root mode 0700'
require_literal "$UPDATE" 'if [[ $BOOTSTRAP_SCHEMA17 -eq 0 ]]; then'
require_count "$UPDATE" '--ensure-service-operation-rescue-snapshot="$rescue_snapshot"' 1
require_literal "$UPDATE" '--check-pre-ledger-service-mutation-idle-under-external-lock'
require_literal "$UPDATE" '--check-pre-ledger-service-operations-idle-wal-aware'
require_literal "$UPDATE" 'verify_recovery_snapshot "$rescue_snapshot"'
require_literal "$UPDATE" 'sync -f -- "$rescue_snapshot" "$recovery_snapshot_dir"'
require_literal "$UPDATE" '--create-service-operation-snapshot="$tmp_snap/$(basename "$PANEL_DB")"'
require_literal "$UPDATE" '--snapshot-schema="$snapshot_schema"'
require_literal "$UPDATE" '--release-transaction-fd="$RELEASE_TRANSACTION_FD"'
require_literal "$UPDATE" '--release-transaction-token="$release_transaction_token"'
require_literal "$UPDATE" '--release-transaction-operation=update'
require_literal "$UPDATE" '--release-transaction-snapshot="$snapshot_name"'
require_literal "$UPDATE" '$(basename "$PANEL_DB")-wal'
require_literal "$UPDATE" '$(basename "$PANEL_DB")-shm'
require_literal "$UPDATE" '$(basename "$PANEL_DB")-journal'
require_literal "$UPDATE" 'standalone without WAL/SHM/journal'
require_literal "$UPDATE" '"$SCHEMA17_BRIDGE" snapshot'
require_literal "$UPDATE" '"$SCHEMA17_BRIDGE" migrate'
reject_literal "$UPDATE" 'cp -a "$PANEL_DB"'
reject_literal "$UPDATE" 'cp -a "$PANEL_DB-wal"'
reject_literal "$UPDATE" 'cp -a "$PANEL_DB-shm"'
reject_literal "$UPDATE" 'cp -a "$PANEL_DB-journal"'
reject_literal "$RELEASE_GUARD" 'recovery-snapshots'
reject_literal "$RELEASE_GUARD" 'RECOVERY_SNAPSHOT_ROOT'
require_sequence "$UPDATE" \
    'prepare_recovery_snapshot_directory' \
    '--ensure-service-operation-rescue-snapshot="$rescue_snapshot"' \
    'release_txn_verify_inherited_lock "$RELEASE_TRANSACTION_ROOT" "$RELEASE_TRANSACTION_FD"' \
    'flock -n -x "$MUTATION_LOCK_FD"' \
    'active marker identity changed during durable recovery snapshot' \
    'verify_quiesce_coordinator_stopped celikpanel-panel.service' \
    'verify_quiesce_coordinator_stopped celikpanel-agent.service' \
    'agent/package mutation state changed during durable recovery snapshot' \
    'panel service-operation state changed during durable recovery snapshot' \
    'verify_recovery_snapshot "$rescue_snapshot"' \
    'sync -f -- "$rescue_snapshot" "$recovery_snapshot_dir"' \
    '--create-service-operation-snapshot="$tmp_snap/$(basename "$PANEL_DB")"'
require_literal "$UPDATE" 'retire_recovery_snapshot_after_verified_publish() {'
require_literal "$UPDATE" 'final snapshot checksum verification failed before recovery retirement'
require_literal "$UPDATE" 'rm -f -- "$rescue_snapshot"'
require_literal "$UPDATE" 'rmdir -- "$recovery_snapshot_dir"'
require_sequence "$UPDATE" \
    'retire_recovery_snapshot_after_verified_publish() {' \
    '[[ "$verified_snapshot" == "$expected_final" && "$snap" == "$expected_final" ]]' \
    'sha256sum -c SHA256SUMS >/dev/null' \
    'verify_recovery_snapshot "$rescue_snapshot"' \
    'rm -f -- "$rescue_snapshot"' \
    'sync -f -- "$recovery_snapshot_dir"' \
    'rmdir -- "$recovery_snapshot_dir"' \
    'sync -f -- "$RECOVERY_SNAPSHOT_ROOT"'
require_sequence "$UPDATE" \
    'mv -T --no-clobber -- "$tmp_snap" "$snap"' \
    'sync -f -- "$SNAP_ROOT"' \
    'verified_snapshot=$snap' \
    'retire_recovery_snapshot_after_verified_publish' \
    'mutation_started=1'

# Snapshot provenance describes old bytes as unknown and binds the separate
# target release inside the complete checksum manifest.
# Snapshot provenance eski baytları unknown olarak tanımlar ve ayrı hedef sürümü
# eksiksiz checksum manifestine bağlar.
require_literal "$UPDATE" 'source_commit=unknown'
require_literal "$UPDATE" 'target_release_commit=$trusted_release_commit'
require_literal "$UPDATE" 'target_release_tree=$trusted_release_tree'
require_literal "$UPDATE" 'snapshot_nonce=$(od -An -N16 -tx1 /dev/urandom | tr -d '\''[:space:]'\'')'
require_literal "$UPDATE" '[[ "$snapshot_nonce" =~ ^[0-9a-f]{32}$ ]]'
require_literal "$UPDATE" 'snapshot_name="$stamp-from-unknown-to-$target_release_commit-$snapshot_nonce"'
require_literal "$UPDATE" 'stage_root="$SNAP_ROOT/.release-snapshot.incomplete.$BASHPID.$snapshot_nonce"'
require_literal "$UPDATE" 'tmp_snap="$stage_root/$snapshot_name"'
require_literal "$UPDATE" '"$target_release_commit" > "$tmp_snap/target-release.commit"'
require_literal "$UPDATE" 'target-release-commit "$trusted_release_commit"'
require_literal "$UPDATE" 'target-release-tree "$trusted_release_tree"'
require_literal "$UPDATE" '"$target_release_tree" > "$tmp_snap/target-release.tree"'
require_literal "$UPDATE" 'mv -T --no-clobber -- "$tmp_snap" "$snap"'
require_sequence "$UPDATE" \
    'trap on_exit EXIT' \
    'mkdir -m 0700 -- "$stage_root"' \
    'mkdir -m 0700 -- "$tmp_snap"' \
    ': > "$tmp_snap/service-states.tsv"' \
    'load_saved_service_states "$tmp_snap/service-states.tsv"' \
    'capture_quiesce_coordinator_ledger "$tmp_snap/quiesce-coordinators.tsv"' \
    'load_quiesce_coordinator_identities "$tmp_snap/quiesce-coordinators.tsv"' \
    'release_txn_validate_update_snapshot_stage "$SNAP_ROOT" "$snapshot_name" "$stage_root"' \
    'sync -f -- "$tmp_snap/service-states.tsv" "$tmp_snap/quiesce-coordinators.tsv"' \
    'preserve_staging=1' \
    'transaction_phase=quiesce-publishing' \
    'release_txn_create_quiesce_marker \' \
    '"$SNAP_ROOT" "$stage_root"'
require_sequence "$UPDATE" \
    '"$target_release_commit" > "$tmp_snap/target-release.commit"' \
    'LC_ALL=C find . -type f ! -path '\''./SHA256SUMS'\'' -print0' \
    'find "$tmp_snap" -type f -exec sync -f -- {} \;' \
    'find "$tmp_snap" -depth -type d -exec sync -f -- {} \;' \
    'sync -f -- "$stage_root"' \
    'mv -T --no-clobber -- "$tmp_snap" "$snap"' \
    'sync -f -- "$SNAP_ROOT"' \
    'rmdir -- "$stage_root"' \
    'sync -f -- "$SNAP_ROOT"' \
    'verified_snapshot=$snap'

# The shell and agent share exact root:celikpanel lock metadata. Quiesce binds
# both coordinators to durable PID/start-time identities before either is frozen.
# Kabuk ve agent tam root:celikpanel kilit metadatasını paylaşır. Quiesce, iki
# koordinatörü de dondurmadan önce kalıcı PID/başlangıç-zamanı kimliklerine bağlar.
for script in "$UPDATE" "$ROLLBACK"; do
    require_literal "$script" '(umask 077; set -o noclobber; : > "$MUTATION_LOCK")'
    require_literal "$script" 'chown root:celikpanel -- "$MUTATION_LOCK"'
    require_literal "$script" 'chmod 0600 -- "$MUTATION_LOCK"'
done
require_literal "$ROLLBACK" 'local lock_dir group_id owner group mode before after'
require_literal "$ROLLBACK" '[[ "$owner" == 0 && "$group" == "$group_id" && "$mode" == 600 ]]'

# Immediate acquisition may create the canonical lock. A controlled agent-start
# handoff must instead reopen the exact recorded inode with exact metadata and
# acquire it exclusively without waiting. The saved-inactive path never releases
# the lock, while the two saved-active paths contain the complete handoff block.
require_literal "$UPDATE" 'MUTATION_LOCK_IDENTITY='
require_literal "$UPDATE" 'local acquire_mode=${1:-immediate}'
require_literal "$UPDATE" 'local lock_dir group_id owner group mode links size path_identity fd_identity'
require_sequence "$UPDATE" \
    'acquire_release_mutation_lock() {' \
    'immediate | handoff) ;;' \
    '[[ "$acquire_mode" == immediate ]]' \
    '(umask 077; set -o noclobber; : > "$MUTATION_LOCK")' \
    'read -r owner group mode links size < <(stat -Lc '\''%u %g %a %h %s'\'' -- "$MUTATION_LOCK")' \
    '&& "$links" == 1 && "$size" == 0 ]]' \
    'path_identity=$(stat -Lc '\''%d:%i'\'' -- "$MUTATION_LOCK")' \
    '[[ "$path_identity" == "$MUTATION_LOCK_IDENTITY" ]]' \
    'fd_identity=$(stat -Lc '\''%d:%i'\'' -- "/proc/$BASHPID/fd/$MUTATION_LOCK_FD")' \
    '[[ "$fd_identity" == "$MUTATION_LOCK_IDENTITY" ]]' \
    'if ! flock -n -x "$MUTATION_LOCK_FD"; then'
require_literal "$UPDATE" 'mutation lock file must be root:celikpanel mode 0600, single-link, and empty'
require_literal "$UPDATE" 'mutation lock pathname disappeared before controlled agent-start handoff reacquire'
require_literal "$UPDATE" 'mutation lock was not handed back after controlled agent start; update refused'
reject_literal "$UPDATE" 'flock -w'
reject_literal "$UPDATE" 'unset MUTATION_LOCK_IDENTITY'
require_regex_count "$UPDATE" '^[[:space:]]*acquire_release_mutation_lock$' 5
require_regex_count "$UPDATE" '^[[:space:]]*acquire_release_mutation_lock handoff$' 2
require_count "$UPDATE" 'cannot hand the mutation lock to' 2
require_active_agent_handoff_blocks "$UPDATE" 2
require_sequence "$UPDATE" \
    'release_release_mutation_lock() {' \
    'exec {MUTATION_LOCK_FD}>&-' \
    'MUTATION_LOCK_FD=' \
    '}'

# The guard accepts one canonical target-bound stage, preserves all three
# recovery files on reset, and exposes exact six/seven-argument quiesce APIs.
# Guard yalnız hedefe bağlı kanonik bir stage kabul eder, reset sırasında üç
# kurtarma dosyasını da korur ve tam altı/yedi argümanlı quiesce API'leri sunar.
require_literal "$RELEASE_GUARD" 'local pattern='\''^([0-9]{8}T[0-9]{6}Z)-from-unknown-to-([0-9a-f]{40})-([0-9a-f]{32})$'\'''
require_literal "$RELEASE_GUARD" '_release_txn_validate_quiesce_coordinators() {'
require_literal "$RELEASE_GUARD" '[[ ${#rows[@]} -eq 2 ]]'
require_literal "$RELEASE_GUARD" '0) expected=celikpanel-agent.service ;;'
require_literal "$RELEASE_GUARD" '1) expected=celikpanel-panel.service ;;'
require_literal "$RELEASE_GUARD" 'active|activating|reloading|refreshing)'
require_literal "$RELEASE_GUARD" '[[ "$pid" == 0 && "$start_time" == 0 ]]'
require_literal "$RELEASE_GUARD" '_release_txn_validate_quiesce_coordinators "$coordinators" || return 1'
require_literal "$RELEASE_GUARD" 'service-states.tsv|quiesce-coordinators.tsv|snapshot-transition.state|panel-tls) continue ;;'
require_literal "$RELEASE_GUARD" 'sync -f -- "$child/service-states.tsv" "$child/quiesce-coordinators.tsv"'
require_sequence "$RELEASE_GUARD" \
    'release_txn_validate_quiesce_token() {' \
    'release_txn_validate_update_snapshot_stage "$5" "$4" "$6" || return 1' \
    '_release_txn_validate_marker "$1" quiesce.pending "$2" "$3" "$4"'
require_sequence "$RELEASE_GUARD" \
    'release_txn_create_quiesce_marker() {' \
    'local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 snapshot_root=$6 stage=$7 marker tmp' \
    'release_txn_validate_update_snapshot_stage "$snapshot_root" "$snapshot" "$stage" || return 1' \
    'mv -T --no-clobber -- "$tmp" "$marker"' \
    'release_txn_validate_quiesce_token "$root" "$token" "$operation" "$snapshot" "$snapshot_root" "$stage"'
require_sequence "$RELEASE_GUARD" \
    'release_txn_promote_quiesce_to_active() {' \
    'local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 snapshot_root=$6 stage=$7' \
    'release_txn_validate_quiesce_token "$root" "$token" "$operation" "$snapshot" "$snapshot_root" "$stage" || return 1' \
    'mv -T --no-clobber -- "$root/quiesce.pending" "$root/active"'
require_sequence "$RELEASE_GUARD" \
    'release_txn_remove_quiesce_marker() {' \
    'local root=$1 inherited_fd=$2 token=$3 operation=$4 snapshot=$5 snapshot_root=$6 stage=$7' \
    'release_txn_validate_quiesce_token "$root" "$token" "$operation" "$snapshot" "$snapshot_root" "$stage" || return 1' \
    'rm -f -- "$root/quiesce.pending"'

# Markerless recovery removes a published snapshot's empty canonical stage only
# after lock, root, marker, ownership, mode and direct-child checks, then fsyncs.
# İşaretçisiz kurtarma, yayımlanmış snapshot'ın boş kanonik stage dizinini yalnız
# kilit, kök, marker, sahiplik, kip ve doğrudan-alt kontrollerinden sonra silip fsync eder.
require_sequence "$RELEASE_GUARD" \
    'release_txn_cleanup_unmarked_update_snapshot_stage() {' \
    'release_txn_verify_inherited_lock "$transaction_root" "$inherited_fd" || return 1' \
    '_release_txn_validate_root_directory "$transaction_root" 700 || return 1' \
    '_release_txn_validate_root_directory "$snapshot_root" 700 || return 1' \
    'for marker in quiesce.pending active completion.pending scheduler-restore.pending; do' \
    '-name '\''.release-snapshot.incomplete*'\'' -print0)' \
    '[[ ${#candidates[@]} -le 1 ]] \' \
    '[[ -d "$candidate" && ! -L "$candidate" ]] \' \
    '"$stage_name" =~ ^\.release-snapshot\.incomplete\.([1-9][0-9]*)\.([0-9a-f]{32})$ ]] \' \
    '_release_txn_validate_root_directory "$candidate" 700 \' \
    'done < <(find "$candidate" -mindepth 1 -maxdepth 1 -print0)' \
    'if [[ ${#children[@]} -eq 0 ]]; then' \
    'rmdir -- "$candidate" \' \
    'sync -f -- "$snapshot_root" \' \
    '[[ ! -e "$candidate" && ! -L "$candidate" ]] \' \
    'return 0' \
    '[[ ${#children[@]} -eq 1 ]] \'

# A markerless cleanup finishes before a fresh nonce and stage can be created.
# İşaretçisiz temizlik, yeni nonce ve stage oluşturulmadan önce tamamlanır.
require_sequence "$UPDATE" \
    'if [[ $resume_quiescing_update -eq 0 && $resume_active_update -eq 0 ]]; then' \
    'release_txn_cleanup_unmarked_update_snapshot_stage \' \
    'snapshot_nonce=$(od -An -N16 -tx1 /dev/urandom | tr -d '\''[:space:]'\'')' \
    'stage_root="$SNAP_ROOT/.release-snapshot.incomplete.$BASHPID.$snapshot_nonce"'

# The updater records exactly two ordered identities. Active-like rows carry
# PID/start-time; inactive-like rows are canonical state/0/0 and receive no signal.
# Güncelleyici sıralı tam iki kimlik kaydeder. Aktif-benzeri satırlar PID/başlangıç
# zamanı taşır; pasif-benzeri satırlar kanonik state/0/0'dır ve sinyal almaz.
require_literal "$UPDATE" 'coordinator_process_start_time() {'
require_literal "$UPDATE" 'find "$cgroup_root" -type f -name cgroup.procs -print0 \'
require_literal "$UPDATE" '| while IFS= read -r -d '\'''''\'' procs_file; do'
reject_literal "$UPDATE" 'done < <(find "$cgroup_root" -type f -name cgroup.procs -print0)'
require_literal "$UPDATE" 'for unit in celikpanel-agent.service celikpanel-panel.service; do'
require_literal "$UPDATE" 'printf '\''%s\t%s\t%s\t%s\n'\'' "$unit" "$state" "$main_pid" "$start_time"'
require_literal "$UPDATE" 'printf '\''%s\t%s\t0\t0\n'\'' "$unit" "$state"'
require_literal "$UPDATE" '[[ "$state" == "${saved_active_states[$unit]:-}" ]]'
require_literal "$UPDATE" 'either|frozen|unfrozen) ;;'
require_literal "$UPDATE" 'verify_quiesce_coordinator_stopped() {'
require_count "$UPDATE" 'freeze_release_service_cgroup celikpanel-panel.service panel panel_frozen' 1
require_count "$UPDATE" 'freeze_release_service_cgroup celikpanel-agent.service agent agent_frozen' 1
require_count "$UPDATE" '--signal=SIGCONT' 4
require_sequence "$UPDATE" \
    'resume_quiesced_release_services() {' \
    'verify_quiesce_coordinator_identity celikpanel-panel.service either || return 1' \
    'verify_quiesce_coordinator_identity celikpanel-agent.service either || return 1' \
    'if service_state_is_active_like "${quiesce_active_states[celikpanel-panel.service]}"; then' \
    'systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-panel.service' \
    'if service_state_is_active_like "${quiesce_active_states[celikpanel-agent.service]}"; then' \
    'systemctl kill --kill-whom=all --signal=SIGCONT celikpanel-agent.service' \
    'verify_quiesce_coordinator_identity celikpanel-panel.service unfrozen || return 1' \
    'verify_quiesce_coordinator_identity celikpanel-agent.service unfrozen || return 1'

# Canonical panel checks tolerate a healthy non-empty WAL. Only the two cold
# postconditions below may use the immutable checker against the canonical DB.
# Kanonik panel kontrolleri sağlıklı dolu WAL'ı kabul eder. Kanonik DB'de yalnız
# aşağıdaki iki cold postcondition immutable checker kullanabilir.
require_literal "$UPDATE" 'healthy coordinator may retain a non-empty SQLite WAL'
require_count "$UPDATE" '--check-service-operations-idle-wal-aware' 8
require_count "$UPDATE" '--check-pre-ledger-service-operations-idle-wal-aware' 6
require_regex_count "$UPDATE" '^[[:space:]]*"\$BIN_DIR/panel" --check-service-operations-idle[[:space:]]*\\$' 1
require_regex_count "$UPDATE" '^[[:space:]]*"\$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle[[:space:]]*\\$' 1
require_regex_count "$UPDATE" '^[[:space:]]*"\$TRUSTED_RELEASE_ROOT/bin/panel" --check-(pre-ledger-)?service-operations-idle([[:space:]]*\\|; then)$' 0

# The normal quiesce path closes the enqueue race while holding the shared lock,
# promotes the exact six/seven-argument marker, and proves both cgroups stopped.
# Normal quiesce yolu ortak kilidi tutarken enqueue yarışını kapatır, tam altı/yedi
# argümanlı markerı yükseltir ve iki cgroup'un da durduğunu kanıtlar.
require_sequence "$UPDATE" \
    '"$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle' \
    'acquire_release_mutation_lock' \
    '"$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock'
require_sequence "$UPDATE" \
    '"$PREFLIGHT_AGENT" --check-service-mutation-idle' \
    'acquire_release_mutation_lock' \
    '"$PREFLIGHT_AGENT" --check-service-mutation-idle-under-external-lock'
require_sequence "$UPDATE" \
    'freeze_release_service_cgroup celikpanel-panel.service panel panel_frozen' \
    'freeze_release_service_cgroup celikpanel-agent.service agent agent_frozen' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware; then' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware; then' \
    'final frozen panel idle proof failed' \
    'final frozen agent idle proof failed' \
    'verify_quiesce_coordinator_identity celikpanel-panel.service frozen' \
    'verify_quiesce_coordinator_identity celikpanel-agent.service frozen' \
    'release_txn_validate_quiesce_token \' \
    '"$SNAP_ROOT" "$stage_root" \' \
    'release_txn_promote_quiesce_to_active \' \
    '"$SNAP_ROOT" "$stage_root" \' \
    'terminate_frozen_release_service celikpanel-panel.service panel panel_frozen' \
    'terminate_frozen_release_service celikpanel-agent.service agent agent_frozen' \
    'release_release_mutation_lock || die "cannot release stale mutation lock after coordinator stop"' \
    'prepare_runtime_mutation_lock_dir' \
    'acquire_release_mutation_lock' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware \' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware \' \
    'stopped panel idle proof failed' \
    'stopped agent idle proof failed'

# An active-marker retry either accepts an already proven stopped coordinator or
# terminates the exact still-frozen historical identity; it never freezes it again.
# Active-marker yeniden denemesi ya durmuş olduğu kanıtlanan koordinatörü kabul eder
# ya da hâlâ askıdaki tam tarihsel kimliği sonlandırır; onu yeniden dondurmaz.
require_sequence "$UPDATE" \
    'recover_active_release_service() {' \
    'if verify_quiesce_coordinator_stopped "$unit"; then' \
    'return 0' \
    'if service_state_is_active_like "${quiesce_active_states[$unit]:-}" &&' \
    'verify_quiesce_coordinator_identity "$unit" frozen; then' \
    'terminate_frozen_release_service "$unit" "$label" "$flag_name"' \
    'die "$label is neither the exact frozen coordinator nor a proven stopped coordinator during active recovery"'
require_sequence "$UPDATE" \
    'elif [[ "$transaction_phase" == active && $resume_active_update -eq 1 ]]; then' \
    'release_txn_validate_active_token \' \
    'recover_active_release_service celikpanel-panel.service panel panel_frozen' \
    'release_txn_validate_active_token \' \
    'recover_active_release_service celikpanel-agent.service agent agent_frozen' \
    'release_txn_validate_active_token \' \
    'release_release_mutation_lock || die "cannot release stale mutation lock after coordinator stop"' \
    'prepare_runtime_mutation_lock_dir' \
    'acquire_release_mutation_lock' \
    'stopped panel idle proof failed' \
    'stopped agent idle proof failed' \
    '--create-service-operation-snapshot="$tmp_snap/$(basename "$PANEL_DB")"' \
    'LC_ALL=C find . -type f ! -path '\''./SHA256SUMS'\'' -print0' \
    'mv -T --no-clobber -- "$tmp_snap" "$snap"' \
    'sync -f -- "$SNAP_ROOT"' \
    'rmdir -- "$stage_root"'

# Bootstrap releases the flock only for the one-shot trusted initializer, proves
# exact bytes unlocked and locked, then invokes install with initialization off.
# Bootstrap flock'u yalnız tek-seferlik güvenilir initializer için bırakır, tam
# baytları kilitsiz ve kilitli kanıtlar, sonra install'u initialization kapalı çağırır.
reject_literal "$UPDATE" 'INITIALIZE_SERVICE_MUTATION_LEDGER=1'
require_count "$UPDATE" 'INITIALIZE_SERVICE_MUTATION_LEDGER=0' 2
require_count "$UPDATE" 'CELIKPANEL_APPLY_ONLY=1' 2
require_count "$UPDATE" 'SKIP_DEPS=1 SKIP_SECURITY_UPDATES=1 SKIP_ADMIN=1' 2
require_literal "$UPDATE" '/bin/bash "$TRUSTED_RELEASE_ROOT/install.sh"'
require_sequence "$UPDATE" \
    'release_release_mutation_lock || die "cannot release bootstrap mutation lock before ledger initialization"' \
    '"$TRUSTED_RELEASE_ROOT/bin/agent" --initialize-service-mutation-ledger' \
    '"$TRUSTED_RELEASE_ROOT/bin/agent" --check-initial-service-mutation-ledger' \
    'acquire_release_mutation_lock' \
    '"$TRUSTED_RELEASE_ROOT/bin/agent" --check-initial-service-mutation-ledger-under-external-lock' \
    'INITIALIZE_SERVICE_MUTATION_LEDGER=0 CELIKPANEL_APPLY_ONLY=1' \
    '/bin/bash "$TRUSTED_RELEASE_ROOT/install.sh"'

# A post-mutation failure stops both coordinators. Successful apply-only and
# pending-finalization paths migrate offline and use one process-bound start grant.
# Mutasyon sonrası hata iki koordinatörü de durdurur. Başarılı apply-only ve
# pending-finalization yolları offline migrate eder ve sürece bağlı tek start izni kullanır.
require_literal "$UPDATE" 'mutation_started=0'
require_literal "$UPDATE" 'if [[ $mutation_started -eq 1 ]]; then'
require_literal "$UPDATE" 'systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true'
require_literal "$UPDATE" 'systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true'
require_literal "$UPDATE" 'sudo /bin/bash '\''$TRUSTED_RELEASE_ROOT/rollback.sh'\'' '\''$verified_snapshot'\'''
require_literal "$UPDATE" 'cmp -s "$TRUSTED_RELEASE_ROOT/bin/panel" "$BIN_DIR/panel"'
require_literal "$UPDATE" 'installed web tree does not match the trusted release'
require_literal "$UPDATE" '"$BIN_DIR/panel" --check-service-operations-idle'
require_literal "$UPDATE" '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock'
reject_literal "$UPDATE" 'systemctl restart celikpanel-panel.service'
reject_literal "$UPDATE" 'systemctl restart celikpanel-agent.service'

# Offline migration is allowed only while the common lock is held and both
# coordinator cgroups are proven stopped; both ledgers are rechecked afterward.
# Offline migration yalnız ortak kilit tutulurken ve iki koordinatör cgroup'u
# durmuş olarak kanıtlanmışken yapılabilir; ardından iki ledger da yeniden denetlenir.
require_sequence "$UPDATE" \
    'run_panel_migrations_offline() {' \
    '[[ -n "${MUTATION_LOCK_FD:-}" ]]' \
    'for unit in celikpanel-agent.service celikpanel-panel.service; do' \
    'inactive|failed) ;;' \
    'reject_extra_service_cgroup_processes "$unit" 0' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock' \
    '"$BIN_DIR/panel" --migrate-only' \
    '"$BIN_DIR/panel" --check-service-operations-idle' \
    'sync -f -- "$PANEL_DB" "$(dirname "$PANEL_DB")"' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock'

require_sequence "$UPDATE" \
    '"$SCHEMA17_BRIDGE" migrate \' \
    '"$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle \' \
    'schema17 bridge did not produce the exact idle schema20 state'

# A pre-ledger pending retry accepts either the exact old schema or an already
# completed normal migration; both successful elif bodies must be explicit no-ops.
# Ledger öncesi bekleyen yeniden deneme ya tam eski şemayı ya da tamamlanmış normal
# migration durumunu kabul eder; iki başarılı elif gövdesi de açık no-op olmalıdır.
require_sequence "$UPDATE" \
    'elif CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-pre-ledger-service-operations-idle-wal-aware; then' \
    '        :' \
    'elif CELIKPANEL_DATA_DIR=$(dirname "$PANEL_DB") \' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware; then' \
    '        :'

# Controlled agent starts require a proven-absent socket and both a fresh socket
# and active systemd state before the updater reacquires the mutation lock.
require_sequence "$UPDATE" \
    'prepare_fresh_agent_socket_start() {' \
    'fresh socket preparation requires mutation lock' \
    'reject_extra_service_cgroup_processes celikpanel-agent.service 0' \
    '[[ -S $socket && ! -L $socket ]]' \
    'rm -f -- /run/celikpanel/agent.sock' \
    '[[ ! -e $socket && ! -L $socket ]]' \
    'wait_for_fresh_active_agent() {' \
    '[[ ! -L $socket ]]' \
    '[[ $state == active && -S $socket ]]'

# Pending finalization keeps completion.pending until stopped-state validation,
# offline migration, controlled starts, post-grant proofs and durable removal pass.
# Bekleyen sonlandırma; durmuş-durum doğrulaması, offline migration, kontrollü
# başlangıçlar, izin-sonrası kanıtlar ve kalıcı kaldırma geçene dek completion.pending'i tutar.
require_sequence "$UPDATE" \
    'if [[ -e "$RELEASE_TRANSACTION_ROOT/completion.pending" || -L "$RELEASE_TRANSACTION_ROOT/completion.pending" ]]; then' \
    'pending_completion_verified=0' \
    'pending_completion_removing=0' \
    'pending_scheduler_restore_pending=0' \
    'pending_scheduler_restored=0' \
    'validate_pending_update_snapshot "$pending_snapshot"' \
    'systemctl stop celikpanel-panel.service \' \
    'systemctl stop celikpanel-agent.service \' \
    'release_release_mutation_lock \' \
    'prepare_runtime_mutation_lock_dir' \
    'acquire_release_mutation_lock' \
    'run_panel_migrations_offline' \
    'prepare_fresh_agent_socket_start' \
    'release_txn_create_start_authorization \' \
    'release_release_mutation_lock \' \
    'systemctl start celikpanel-agent.service \' \
    'wait_for_fresh_active_agent' \
    'acquire_release_mutation_lock handoff' \
    'pending update agent state is not idle after the startup lock handoff' \
    'verify_installed_release_artifacts' \
    'verify_saved_enablement' \
    'pending update marker changed during the startup lock handoff' \
    'systemctl start celikpanel-panel.service \' \
    'verify_saved_runtime_states' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock' \
    'verify_saved_enablement' \
    'release_txn_remove_start_authorization \' \
    'verify_saved_runtime_states' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock' \
    'verify_saved_enablement' \
    'release_txn_validate_pending_token \' \
    'pending_completion_verified=1' \
    'release_txn_mark_scheduler_restore_pending \' \
    'pending_scheduler_restore_pending=1' \
    'pending_completion_removing=1' \
    'release_txn_remove_completion_pending \' \
    'release_release_mutation_lock \' \
    'panel_tls_quiesce_certbot_scheduler "$pending_snapshot_path/panel-tls" \' \
    'release_txn_validate_scheduler_restore_token \' \
    'panel_tls_restore_certbot_scheduler "$pending_snapshot_path/panel-tls" \' \
    'pending_scheduler_restored=1' \
    'release_txn_remove_scheduler_restore_pending \' \
    'pending_scheduler_restore_pending=0' \
    'pending_finalization_succeeded=1' \
    'trap - EXIT'

# Normal success publishes a complete snapshot before apply-only, then follows
# the same completion marker, offline migration and controlled-start ordering.
# Normal başarı apply-only'den önce eksiksiz snapshot yayımlar; ardından aynı
# completion marker, offline migration ve kontrollü başlangıç sırasını izler.
require_sequence "$UPDATE" \
    'verified_snapshot=$snap' \
    'mutation_started=1' \
    '/bin/bash "$TRUSTED_RELEASE_ROOT/install.sh"' \
    'apply-only unexpectedly left $unit active' \
    'verify_saved_enablement' \
    'verify_installed_release_artifacts' \
    '"$TRUSTED_RELEASE_ROOT/bin/panel" --check-service-operations-idle-wal-aware' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock' \
    'release_txn_mark_completion_pending \' \
    'run_panel_migrations_offline' \
    'verify_installed_release_artifacts' \
    'release_txn_validate_pending_token \' \
    'prepare_fresh_agent_socket_start' \
    'release_txn_create_start_authorization \' \
    'release_release_mutation_lock || die "cannot hand the mutation lock to the verified agent"' \
    'systemctl start celikpanel-agent.service || die "verified agent could not be started"' \
    'wait_for_fresh_active_agent' \
    'acquire_release_mutation_lock handoff' \
    'verified agent state is not idle after the startup lock handoff' \
    'verify_installed_release_artifacts' \
    'verify_saved_enablement' \
    'update completion marker changed during the startup lock handoff' \
    'systemctl start celikpanel-panel.service || die "verified panel could not be started"' \
    'verify_saved_runtime_states' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock' \
    'verify_saved_enablement' \
    'release_txn_remove_start_authorization \' \
    'verify_saved_runtime_states' \
    '"$BIN_DIR/agent" --check-service-mutation-idle-under-external-lock' \
    'verify_saved_enablement' \
    'release_txn_validate_pending_token \' \
    'transaction_completion_verified=1' \
    'transaction_phase=scheduler-publishing' \
    'release_txn_mark_scheduler_restore_pending \' \
    'transaction_phase=completion-removing' \
    'release_txn_remove_completion_pending \' \
    'transaction_started=0' \
    'mutation_started=0' \
    'release_release_mutation_lock || die "cannot release update mutation lock"' \
    'transaction_phase=scheduler-restoring' \
    'release_txn_validate_scheduler_restore_token \' \
    'panel_tls_quiesce_certbot_scheduler "$snap/panel-tls" \' \
    'release_txn_validate_scheduler_restore_token \' \
    'panel_tls_restore_certbot_scheduler "$snap/panel-tls" \' \
    'scheduler_restore_verified=1' \
    'transaction_phase=scheduler-removing' \
    'release_txn_remove_scheduler_restore_pending \' \
    'trap - EXIT'

# The general installer retains strict fresh/explicit one-shot semantics and
# publishes the ledger as root:celikpanel 0600 before starting the agent.
# Genel installer sıkı fresh/explicit tek-seferlik anlamı korur ve agent'ı
# başlatmadan önce ledger'ı root:celikpanel 0600 yayımlar.
require_literal "$INSTALL" '#!/bin/bash'
require_literal "$INSTALL" 'PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin'
require_literal "$INSTALL" 'INITIALIZE_SERVICE_MUTATION_LEDGER'
require_literal "$INSTALL" '! -e "$DATA_DIR/celikpanel.db"'
require_literal "$INSTALL" '! -e "$AGENT_STATE_DIR"'
require_literal "$INSTALL" 'SVC_GROUP_ID=$(service_group_id)'
require_literal "$INSTALL" '"$PREFIX/bin/agent" --initialize-service-mutation-ledger'
require_literal "$INSTALL" '[[ "$ledger_owner" == 0 && "$ledger_group" == "$SVC_GROUP_ID" && "$ledger_mode" == 600 ]]'
require_sequence "$INSTALL" \
    'initialize_ledger=${INITIALIZE_SERVICE_MUTATION_LEDGER:-0}' \
    '! -e "$DATA_DIR/celikpanel.db"' \
    'elif [[ $initialize_ledger -ne 1 ]]; then' \
    'SVC_GROUP_ID=$(service_group_id)' \
    '"$PREFIX/bin/agent" --initialize-service-mutation-ledger' \
    '[[ "$ledger_owner" == 0 && "$ledger_group" == "$SVC_GROUP_ID" && "$ledger_mode" == 600 ]]' \
    'systemctl restart celikpanel-agent.service' \
    'systemctl is-active --quiet celikpanel-agent.service' \
    '[ -S /run/celikpanel/agent.sock ]' \
    'systemctl restart celikpanel-panel.service' \
    'systemctl is-active --quiet celikpanel-panel.service'

# Rollback validates standalone snapshots and the cold restore postcondition
# with the immutable checker. A controlled panel start may leave a healthy WAL,
# so the final canonical proof must use the WAL-aware checker.
# Rollback standalone snapshotları ve cold restore postcondition'ı immutable
# checker ile doğrular. Kontrollü panel başlangıcı sağlıklı bir WAL bırakabilir;
# son kanonik kanıt bu nedenle WAL-aware checker kullanmalıdır.
require_literal "$ROLLBACK" '#!/bin/bash'
require_literal "$ROLLBACK" 'RELEASES_ROOT=/var/backups/celikpanel/releases'
require_literal "$ROLLBACK" 'validate_running_release()'
require_literal "$ROLLBACK" '[[ "$relative" =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ ]]'
require_literal "$ROLLBACK" 'rollback release root must be root-owned mode 0700'
require_literal "$ROLLBACK" '! -path '\''./SHA256SUMS'\'' -print0'
require_literal "$ROLLBACK" '[[ -f "$snap/target-release.commit" ]]'
require_literal "$ROLLBACK" '[[ "$snapshot_commit" == unknown ]]'
require_literal "$ROLLBACK" 'snapshot_name_pattern='\''^([0-9]{8}T[0-9]{6}Z)-from-unknown-to-([0-9a-f]{40})-([0-9a-f]{32})$'\'''
require_literal "$ROLLBACK" '[[ "$snapshot_name" =~ $snapshot_name_pattern ]]'
require_literal "$ROLLBACK" 'panel database snapshot must be standalone without WAL/SHM/journal'
require_literal "$ROLLBACK" 'CELIKPANEL_DATA_DIR="$snap"'
require_literal "$ROLLBACK" '"$PREFLIGHT_PANEL" --check-service-operations-idle'
require_literal "$ROLLBACK" '"$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle'
require_count "$ROLLBACK" '--check-service-operations-idle-wal-aware' 1
require_count "$ROLLBACK" '--check-pre-ledger-service-operations-idle-wal-aware' 1
require_regex_count "$ROLLBACK" '^[[:space:]]*"\$PREFLIGHT_PANEL" --check-service-operations-idle[[:space:]]*\\$' 2
require_regex_count "$ROLLBACK" '^[[:space:]]*"\$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle[[:space:]]*\\$' 2
reject_literal "$ROLLBACK" 'cp -a "$snap/$(basename "$PANEL_DB")-wal"'
reject_literal "$ROLLBACK" 'cp -a "$snap/$(basename "$PANEL_DB")-shm"'
reject_literal "$ROLLBACK" 'cp -a "$snap/$(basename "$PANEL_DB")-journal"'
require_literal "$ROLLBACK" '--restore-service-operation-snapshot="$snap/$(basename "$PANEL_DB")"'
require_literal "$ROLLBACK" '--snapshot-schema="$transition_state"'
reject_literal "$ROLLBACK" 'rm -f -- "$PANEL_DB" "$PANEL_DB-wal" "$PANEL_DB-shm" "$PANEL_DB-journal"'
require_sequence "$ROLLBACK" \
    'validate_preflight_binary "$PREFLIGHT_PANEL" panel' \
    'CELIKPANEL_DATA_DIR="$snap"' \
    '"$PREFLIGHT_PANEL" --check-service-operations-idle' \
    'rollback_verified_snapshot=$snap' \
    'systemctl stop celikpanel-panel.service'

require_sequence "$ROLLBACK" \
    'cmp -s "$snap/bin/panel" "$BIN_DIR/panel"' \
    '"$PREFLIGHT_PANEL" --check-service-operations-idle \' \
    'release_txn_create_start_authorization \' \
    'systemctl start celikpanel-panel.service || die "restored panel did not start"' \
    'systemctl stop celikpanel-panel.service \' \
    '"$PREFLIGHT_PANEL" --check-service-operations-idle-wal-aware \'
require_sequence "$ROLLBACK" \
    'cmp -s "$snap/bin/panel" "$BIN_DIR/panel"' \
    '"$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle \' \
    'release_txn_create_start_authorization \' \
    'systemctl start celikpanel-panel.service || die "restored panel did not start"' \
    'systemctl stop celikpanel-panel.service \' \
    '"$PREFLIGHT_PANEL" --check-pre-ledger-service-operations-idle-wal-aware \'

# Rollback repeats normal/initial/pre-ledger proofs under the outer flock. The
# legacy cleanup accepts only one strict root-owned canonical-prefix stage.
# Rollback normal/initial/pre-ledger kanıtlarını dış flock altında yineler. Legacy
# cleanup yalnız tek sıkı root-owned kanonik-önek stage kabul eder.
require_literal "$ROLLBACK" '--check-service-mutation-idle-under-external-lock'
require_literal "$ROLLBACK" '--check-initial-service-mutation-ledger-under-external-lock'
require_literal "$ROLLBACK" '--check-pre-ledger-service-mutation-idle-under-external-lock'
require_sequence "$ROLLBACK" \
    'stop_new_agent_and_hold_mutation_lock \' \
    '--check-service-mutation-idle \' \
    '--check-service-mutation-idle-under-external-lock \'
require_sequence "$ROLLBACK" \
    'stop_new_agent_and_hold_mutation_lock \' \
    '--check-initial-service-mutation-ledger \' \
    '--check-initial-service-mutation-ledger-under-external-lock \'
require_sequence "$ROLLBACK" \
    '"$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle' \
    'acquire_release_mutation_lock' \
    '"$PREFLIGHT_AGENT" --check-pre-ledger-service-mutation-idle-under-external-lock' \
    'cleanup_verified_legacy_initial_stage'
require_literal "$ROLLBACK" '[[ "$stage_name" =~ ^\.service-mutations-initial-[0-9]+\.json$ ]]'
require_literal "$ROLLBACK" "local expected_initial_ledger='{\"version\":1,\"jobs\":{}}'"
require_literal "$ROLLBACK" "stat -Lc '%u %g %a %h %s' -- \"\$stage\""
require_literal "$ROLLBACK" '( "$group" == "$group_id" || "$group" == 0 )'
require_literal "$ROLLBACK" '(( stage_size <= ${#expected_initial_ledger} ))'
require_literal "$ROLLBACK" 'printf '\''%s'\'' "${expected_initial_ledger:0:stage_size}" | cmp -s - "$stage"'
require_sequence "$ROLLBACK" \
    'rm -f -- "$stage"' \
    'sync -d -- "$AGENT_STATE_DIR"'

# A pre-ledger agent frozen for the proof-to-stop handoff must never receive
# SIGCONT on a fail-closed error path. Its whole cgroup is killed, stopped and
# proved empty before the frozen flag can be cleared.
require_function_sequence "$ROLLBACK" freeze_and_stop_legacy_agent \
    'systemctl kill --kill-whom=all --signal=SIGSTOP celikpanel-agent.service' \
    'legacy_agent_frozen=1' \
    'reject_extra_service_cgroup_processes celikpanel-agent.service "$main_pid"' \
    'systemctl stop --no-block celikpanel-agent.service' \
    'systemctl kill --kill-whom=all --signal=SIGKILL celikpanel-agent.service' \
    'systemctl stop celikpanel-agent.service \' \
    'systemctl is-active --quiet celikpanel-agent.service' \
    'reject_extra_service_cgroup_processes celikpanel-agent.service 0' \
    'legacy_agent_frozen=0'
require_function_sequence "$ROLLBACK" terminate_frozen_legacy_agent_fail_closed \
    'systemctl stop --no-block celikpanel-agent.service' \
    'systemctl kill --kill-whom=all --signal=SIGKILL celikpanel-agent.service' \
    'systemctl stop --no-block celikpanel-agent.service' \
    'service_cgroup_pids celikpanel-agent.service' \
    'legacy_agent_frozen=0'
reject_function_literal "$ROLLBACK" freeze_and_stop_legacy_agent 'SIGCONT'
reject_function_literal "$ROLLBACK" terminate_frozen_legacy_agent_fail_closed 'SIGCONT'
reject_function_literal "$ROLLBACK" rollback_on_exit 'SIGCONT'
require_function_sequence "$ROLLBACK" rollback_on_exit \
    'local status=$? frozen_cleanup_failed=0' \
    'trap - EXIT' \
    'if [[ $legacy_agent_frozen -eq 1 ]]; then' \
    'if [[ $rollback_transaction_started -eq 0 &&' \
    '$rollback_mutation_started -eq 0 &&' \
    '$rollback_service_state_recorded -eq 1 ]]; then' \
    'if ! unfreeze_legacy_agent; then' \
    'terminate_frozen_legacy_agent_fail_closed || frozen_cleanup_failed=1' \
    'if [[ $frozen_cleanup_failed -eq 1 ]]; then' \
    'if [[ $status -eq 0 ]]; then'

# Rollback completion is split into two durable obligations: runtime rollback
# and exact Certbot scheduler restoration. Both a completion+scheduler retry
# and a scheduler-only retry must remain bound to one token and snapshot.
require_literal "$ROLLBACK" 'rollback_scheduler_only_resume=0'
require_literal "$ROLLBACK" 'rollback_scheduler_restore_pending=0'
require_literal "$ROLLBACK" 'rollback_scheduler_restore_completed=0'
require_literal "$ROLLBACK" 'rollback_completion_verified=0'
require_literal "$ROLLBACK" 'rollback_completion_removing=0'
require_literal "$ROLLBACK" 'release_txn_read_scheduler_restore_fields'
require_literal "$ROLLBACK" 'release_txn_validate_scheduler_restore_token \'
require_literal "$ROLLBACK" 'release_txn_mark_scheduler_restore_pending \'
require_literal "$ROLLBACK" 'release_txn_remove_scheduler_restore_pending \'
require_literal "$ROLLBACK" 'pending rollback scheduler marker does not match completion.pending'
require_literal "$ROLLBACK" 'pending rollback scheduler marker proof failed'
require_sequence "$ROLLBACK" \
    'rollback_verified_snapshot=$snap' \
    'if [[ $rollback_scheduler_only_resume -eq 1 ]]; then' \
    'release_txn_validate_scheduler_restore_token \' \
    'rollback_scheduler_restore_pending=1' \
    'panel_tls_quiesce_certbot_scheduler "$snap/panel-tls" \' \
    'release_txn_validate_scheduler_restore_token \' \
    'panel_tls_restore_certbot_scheduler "$snap/panel-tls" \' \
    'rollback_scheduler_restore_completed=1' \
    'release_txn_remove_scheduler_restore_pending \' \
    'rollback_scheduler_restore_pending=0' \
    'trap - EXIT' \
    'exit 0'
require_sequence "$ROLLBACK" \
    'release_txn_remove_start_authorization \' \
    'release_txn_validate_pending_token \' \
    'rollback_completion_verified=1' \
    'release_txn_mark_scheduler_restore_pending \' \
    'rollback_scheduler_restore_pending=1' \
    'release_txn_validate_scheduler_restore_token \' \
    'rollback_completion_removing=1' \
    'release_txn_remove_completion_pending \' \
    'rollback_transaction_started=0' \
    'rollback_mutation_started=0' \
    'rollback_completion_removing=0' \
    'rollback_completion_verified=0' \
    'release_release_mutation_lock || die "cannot release rollback mutation lock"' \
    'panel_tls_quiesce_certbot_scheduler "$snap/panel-tls" \' \
    'release_txn_validate_scheduler_restore_token \' \
    'panel_tls_restore_certbot_scheduler "$snap/panel-tls" \' \
    'rollback_scheduler_restore_completed=1' \
    'release_txn_remove_scheduler_restore_pending \' \
    'rollback_scheduler_restore_pending=0' \
    'trap - EXIT'

# Any rollback failure after mutation leaves both coordinators stopped and
# prints an exact immutable retry command.
# Mutation başladıktan sonraki rollback hatası iki koordinatörü durdurur ve tam
# değişmez retry komutunu yazdırır.
require_literal "$ROLLBACK" 'rollback_mutation_started=0'
require_literal "$ROLLBACK" 'if [[ $rollback_mutation_started -eq 1 ]]; then'
require_literal "$ROLLBACK" 'if [[ $rollback_scheduler_restore_pending -eq 1 &&'
require_literal "$ROLLBACK" 'if [[ $rollback_scheduler_restore_completed -eq 1 &&'
require_function_sequence "$ROLLBACK" rollback_on_exit \
    'if [[ $rollback_completion_verified -eq 1 &&' \
    '$rollback_completion_removing -eq 1 &&' \
    '$rollback_scheduler_restore_pending -eq 1 &&' \
    '$rollback_transaction_started -eq 1 &&' \
    '$rollback_mutation_started -eq 1 &&' \
    '! -e "$RELEASE_TRANSACTION_ROOT/completion.pending"' \
    '! -L "$RELEASE_TRANSACTION_ROOT/completion.pending"' \
    '-e "$RELEASE_TRANSACTION_ROOT/scheduler-restore.pending"' \
    'release_txn_validate_scheduler_restore_token \' \
    'release_release_mutation_lock' \
    'Restored runtime was left intact and exact scheduler recovery remains retryable.' \
    'if [[ $rollback_scheduler_restore_pending -eq 1 &&' \
    'if [[ $rollback_transaction_started -eq 1 ]]; then'
require_literal "$ROLLBACK" 'durable marker removal is uncertain. Runtime was left intact and rollback did not claim success.'
require_literal "$ROLLBACK" 'completion marker removal durability is uncertain. Restored runtime was left intact and exact scheduler recovery remains retryable.'
require_literal "$ROLLBACK" 'Rollback runtime is complete; exact Certbot scheduler restoration remains safely retryable.'
require_literal "$ROLLBACK" 'Scheduler restoration failed without an exact durable retry marker; both services were stopped.'
require_literal "$ROLLBACK" 'systemctl stop celikpanel-panel.service >/dev/null 2>&1 || true'
require_literal "$ROLLBACK" 'systemctl stop celikpanel-agent.service >/dev/null 2>&1 || true'
require_literal "$ROLLBACK" 'sudo /bin/bash '\''$TRUSTED_RELEASE_ROOT/rollback.sh'\'' '\''$rollback_verified_snapshot'\'''
require_literal "$ROLLBACK" '[[ "$target_release_commit" == "${transition_values[target-release-commit]}" ]]'
require_literal "$ROLLBACK" '[[ "$target_release_tree" == "${transition_values[target-release-tree]}" ]]'
require_sequence "$ROLLBACK" \
    '[[ $EUID -eq 0 ]] || die "Run as root / root olarak çalıştırın: use a trusted release rollback.sh"' \
    'validate_running_release' \
    'validate_root_trusted_dir_chain "$SNAP_ROOT"' \
    'LC_ALL=C find . -type f ! -path '\''./SHA256SUMS'\'' -print0' \
    'transition_state=$(cat "$snap/snapshot-transition.state")' \
    'rollback_verified_snapshot=$snap' \
    'rollback_service_state_recorded=1' \
    'systemctl stop celikpanel-panel.service' \
    'rollback_mutation_started=1' \
    'rm -rf -- "$BIN_DIR"' \
    'trap - EXIT'

# Exercise the privileged rollback EXIT state machine itself with injected
# systemd and marker failures. Static literals alone cannot prove that a
# failure after SIGSTOP avoids SIGCONT, or that the rm-visible/fsync-failed
# completion boundary preserves the already-restored runtime.
rollback_contract_tmp=$(mktemp -d)
trap 'rm -rf -- "$rollback_contract_tmp"' EXIT

legacy_trace="$rollback_contract_tmp/legacy-freeze.trace"
set +e
(
    set -euo pipefail
    eval "$(extract_function_source "$ROLLBACK" unfreeze_legacy_agent)"
    eval "$(extract_function_source "$ROLLBACK" terminate_frozen_legacy_agent_fail_closed)"
    eval "$(extract_function_source "$ROLLBACK" freeze_and_stop_legacy_agent)"
    eval "$(extract_function_source "$ROLLBACK" rollback_on_exit)"

    TRACE="$legacy_trace"
    legacy_stop_attempts=0
    die() {
        printf 'die %s\n' "$*" >> "$TRACE"
        exit 23
    }
    systemctl() {
        printf 'systemctl %s\n' "$*" >> "$TRACE"
        case "$*" in
            'show --property=ActiveState --value celikpanel-agent.service')
                printf 'active\n'
                ;;
            'show --property=MainPID --value celikpanel-agent.service')
                printf '4242\n'
                ;;
            'stop --no-block celikpanel-agent.service')
                legacy_stop_attempts=$((legacy_stop_attempts + 1))
                if [[ "$legacy_stop_attempts" -eq 1 ]]; then
                    return 1
                fi
                return 0
                ;;
            'is-active --quiet celikpanel-agent.service')
                return 1
                ;;
        esac
        return 0
    }
    reject_extra_service_cgroup_processes() {
        printf 'cgroup-proof %s %s\n' "$1" "$2" >> "$TRACE"
        return 0
    }
    service_cgroup_pids() {
        printf 'cgroup-empty %s\n' "$1" >> "$TRACE"
        return 0
    }
    awk() {
        printf 'T\n'
    }
    sleep() {
        :
    }
    release_release_mutation_lock() {
        printf 'release-mutation-lock\n' >> "$TRACE"
        return 0
    }
    release_txn_validate_scheduler_restore_token() {
        printf 'validate-scheduler %s\n' "$*" >> "$TRACE"
        return 0
    }

    legacy_agent_frozen=0
    rollback_transaction_started=1
    rollback_mutation_started=0
    rollback_service_state_recorded=1
    rollback_completion_verified=0
    rollback_completion_removing=0
    rollback_scheduler_restore_pending=0
    rollback_scheduler_restore_completed=0
    rollback_agent_was_active=1
    rollback_panel_was_active=1
    rollback_transaction_token=legacy-token
    rollback_verified_snapshot="$rollback_contract_tmp/legacy-snapshot"
    RELEASE_TRANSACTION_ROOT="$rollback_contract_tmp/legacy-transaction"
    TRUSTED_RELEASE_ROOT=/trusted/release

    trap rollback_on_exit EXIT
    freeze_and_stop_legacy_agent
) 2> "$rollback_contract_tmp/legacy-freeze.stderr"
legacy_status=$?
set -e
[[ "$legacy_status" -eq 23 ]] \
    || die "rollback SIGSTOP failure changed original status: $legacy_status"
require_sequence "$legacy_trace" \
    'systemctl kill --kill-whom=all --signal=SIGSTOP celikpanel-agent.service' \
    'systemctl stop --no-block celikpanel-agent.service' \
    'systemctl stop --no-block celikpanel-agent.service' \
    'systemctl kill --kill-whom=all --signal=SIGKILL celikpanel-agent.service' \
    'systemctl stop --no-block celikpanel-agent.service' \
    'systemctl is-active --quiet celikpanel-agent.service' \
    'cgroup-empty celikpanel-agent.service' \
    'systemctl stop celikpanel-panel.service' \
    'systemctl stop celikpanel-agent.service'
reject_literal "$legacy_trace" 'SIGCONT'
reject_literal "$legacy_trace" 'systemctl start '

run_rollback_completion_exit_case() {
    local name=$1 completion_shape=$2 scheduler_shape=$3 validator_ok=$4
    local completion_verified=$5 completion_removing=$6 expected=$7
    local case_root="$rollback_contract_tmp/$name"
    local case_trace="$case_root/trace"
    local case_stderr="$case_root/stderr"
    local case_status
    mkdir -p -- "$case_root/transaction" "$case_root/snapshot-name"

    case "$completion_shape" in
        absent) ;;
        regular) printf 'completion\n' > "$case_root/transaction/completion.pending" ;;
        symlink) ln -s nowhere "$case_root/transaction/completion.pending" ;;
        *) die "unknown rollback completion test shape: $completion_shape" ;;
    esac
    case "$scheduler_shape" in
        absent) ;;
        regular) printf 'scheduler\n' > "$case_root/transaction/scheduler-restore.pending" ;;
        symlink) ln -s nowhere "$case_root/transaction/scheduler-restore.pending" ;;
        *) die "unknown rollback scheduler test shape: $scheduler_shape" ;;
    esac

    set +e
    (
        set -euo pipefail
        eval "$(extract_function_source "$ROLLBACK" unfreeze_legacy_agent)"
        eval "$(extract_function_source "$ROLLBACK" terminate_frozen_legacy_agent_fail_closed)"
        eval "$(extract_function_source "$ROLLBACK" rollback_on_exit)"

        TRACE="$case_trace"
        VALIDATOR_OK="$validator_ok"
        systemctl() {
            printf 'systemctl %s\n' "$*" >> "$TRACE"
            case "$*" in
                'is-active --quiet '*) return 1 ;;
            esac
            return 0
        }
        release_release_mutation_lock() {
            printf 'release-mutation-lock\n' >> "$TRACE"
            return 0
        }
        release_txn_validate_scheduler_restore_token() {
            printf 'validate-scheduler %s\n' "$*" >> "$TRACE"
            [[ "$VALIDATOR_OK" -eq 1 ]]
        }
        service_cgroup_pids() {
            return 0
        }
        sleep() {
            :
        }

        legacy_agent_frozen=0
        rollback_transaction_started=1
        rollback_mutation_started=1
        rollback_service_state_recorded=1
        rollback_completion_verified="$completion_verified"
        rollback_completion_removing="$completion_removing"
        rollback_scheduler_restore_pending=1
        rollback_scheduler_restore_completed=0
        rollback_agent_was_active=1
        rollback_panel_was_active=1
        rollback_transaction_token=completion-token
        rollback_verified_snapshot="$case_root/snapshot-name"
        RELEASE_TRANSACTION_ROOT="$case_root/transaction"
        TRUSTED_RELEASE_ROOT=/trusted/release

        set +e
        (exit 37)
        rollback_on_exit 2> "$case_stderr"
        case_status=$?
        set -e
        [[ "$case_status" -eq 37 ]] || exit 91
    )
    case_status=$?
    set -e
    [[ "$case_status" -eq 0 ]] \
        || die "rollback completion EXIT case $name failed: $case_status"

    case "$expected" in
        preserve)
            require_literal "$case_trace" \
                "validate-scheduler $case_root/transaction completion-token rollback snapshot-name"
            require_literal "$case_trace" 'release-mutation-lock'
            reject_literal "$case_trace" 'systemctl '
            require_literal "$case_stderr" \
                'Restored runtime was left intact and exact scheduler recovery remains retryable.'
            ;;
        fail-closed)
            require_sequence "$case_trace" \
                'systemctl stop celikpanel-panel.service' \
                'systemctl stop celikpanel-agent.service'
            reject_literal "$case_trace" 'systemctl start '
            ;;
        *) die "unknown rollback completion test expectation: $expected" ;;
    esac
}

run_rollback_completion_exit_case \
    completion-absent-exact absent regular 1 1 1 preserve
run_rollback_completion_exit_case \
    completion-present regular regular 1 1 1 fail-closed
run_rollback_completion_exit_case \
    completion-symlink symlink regular 1 1 1 fail-closed
run_rollback_completion_exit_case \
    scheduler-absent absent absent 1 1 1 fail-closed
run_rollback_completion_exit_case \
    scheduler-symlink absent symlink 0 1 1 fail-closed
run_rollback_completion_exit_case \
    scheduler-validator-failed absent regular 0 1 1 fail-closed
run_rollback_completion_exit_case \
    completion-not-verified absent regular 1 0 1 fail-closed
run_rollback_completion_exit_case \
    completion-not-removing absent regular 1 1 0 fail-closed

rm -rf -- "$rollback_contract_tmp"
trap - EXIT

echo "bootstrap update contract: ok"
