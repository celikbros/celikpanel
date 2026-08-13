#!/usr/bin/env bash
set -euo pipefail

ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
HELPER=$ROOT/deploy/panel-tls-snapshot.sh
INSTALL=$ROOT/install.sh
UPDATE=$ROOT/update.sh
ROLLBACK=$ROOT/rollback.sh
FINALIZER=$ROOT/deploy/finalize-pending-update.sh

fail() {
    printf 'FAIL: %s\n' "$*" >&2
    exit 1
}

expect_tls_failure_with_stderr() {
    local description=$1 error_file=$TEST_ROOT/expected-error.log
    shift
    : >"$error_file"
    if ( "$@" ) 2>"$error_file"; then
        fail "$description"
    fi
    [[ -s "$error_file" ]] || fail "$description did not report an error"
}

expect_tls_success_without_stderr() {
    local description=$1 error_file=$TEST_ROOT/unexpected-error.log
    shift
    : >"$error_file"
    if ! ( "$@" ) 2>"$error_file"; then
        cat -- "$error_file" >&2
        fail "$description"
    fi
    [[ ! -s "$error_file" ]] || {
        cat -- "$error_file" >&2
        fail "$description emitted hidden errors"
    }
}

for script in "$HELPER" "$INSTALL" "$UPDATE" "$ROLLBACK" "$FINALIZER"; do
    bash -n "$script" || fail "syntax: $script"
done

grep -Fq 'SNAPSHOT_VERSION=6' "$UPDATE" || fail 'update snapshot version is not 6'
grep -Fq 'SUPPORTED_SNAPSHOT_VERSION=6' "$ROLLBACK" || fail 'rollback snapshot version is not 6'
grep -Fq 'SNAPSHOT_VERSION=6' "$FINALIZER" || fail 'finalizer snapshot version is not 6'
grep -Fq 'panel_tls_snapshot_capture' "$UPDATE" || fail 'update does not capture panel TLS state'
grep -Fq 'panel_tls_normalize_legacy_self_signed' "$INSTALL" || fail 'install does not protect its initial self-signed TLS pair'
grep -Fq 'panel_tls_normalize_legacy_self_signed' "$UPDATE" || fail 'update does not normalize the narrowly defined legacy TLS pair'
grep -Fq 'panel_tls_snapshot_validate' "$ROLLBACK" || fail 'rollback does not validate panel TLS state'
grep -Fq 'panel_tls_quiesce_certbot_scheduler' "$ROLLBACK" || fail 'rollback does not quiesce Certbot timers'
grep -Fq 'panel_tls_restore_snapshot' "$ROLLBACK" || fail 'rollback does not restore panel TLS state'
grep -Fq 'panel_tls_restore_certbot_scheduler' "$ROLLBACK" || fail 'rollback does not restore Certbot timers'
grep -Fq 'panel_tls_snapshot_validate "$pending_snapshot_path/panel-tls"' "$FINALIZER" \
    || fail 'pending finalizer does not enforce the v5 TLS payload'
grep -Fq 'panel_tls_capture_scheduler_states_to_service_ledger' "$UPDATE" \
    || fail 'update does not bind Certbot scheduler state to the service ledger'
grep -Fq 'panel_tls_snapshot_assert_source_unchanged' "$UPDATE" \
    || fail 'update does not prove the captured TLS source remained unchanged'
grep -Fq 'snapshot version 4 predates exact panel TLS rollback state' "$ROLLBACK" \
    || fail 'legacy v4 snapshots are not rejected with an actionable fail-closed reason'

line_of() {
    grep -n -m1 -F "$2" "$1" | cut -d: -f1
}
capture_line=$(line_of "$UPDATE" 'panel_tls_snapshot_capture')
manifest_line=$(line_of "$UPDATE" 'xargs -0 sha256sum > SHA256SUMS')
[[ "$capture_line" -lt "$manifest_line" ]] || fail 'TLS capture is outside the signed snapshot boundary'
manifest_verify_line=$(line_of "$ROLLBACK" 'snapshot checksum verification failed')
tls_validate_line=$(line_of "$ROLLBACK" 'panel TLS compatibility snapshot is missing or invalid')
[[ "$manifest_verify_line" -lt "$tls_validate_line" ]] \
    || fail 'rollback interprets TLS payload before the outer manifest is verified'
quiesce_line=$(line_of "$ROLLBACK" 'panel_tls_quiesce_certbot_scheduler')
restore_line=$(line_of "$ROLLBACK" 'panel_tls_restore_snapshot')
first_install_line=$(line_of "$ROLLBACK" 'rm -rf -- "$BIN_DIR"')
[[ "$quiesce_line" -lt "$restore_line" && "$restore_line" -lt "$first_install_line" ]] \
    || fail 'TLS restore ordering is not fail-closed before release byte restore'

[[ $(id -u) -eq 0 ]] || {
    printf 'SKIP: functional TLS filesystem contract requires root\n'
    exit 0
}

TEST_ROOT=$(mktemp -d /tmp/celikpanel-tls-contract.XXXXXXXX)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
chmod 0700 "$TEST_ROOT"
export CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT=$TEST_ROOT

TLS_DIR=$TEST_ROOT/tls
PENDING=$TEST_ROOT/agent/panel-certificate-activation.json
HOOK=$TEST_ROOT/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert
SNAPSHOT=$TEST_ROOT/snapshot
TLS_SNAPSHOT=$SNAPSHOT/panel-tls
LEDGER=$SNAPSHOT/service-states.tsv
VERSION=.panel-cert-0123456789abcdef0123456789abcdef

install -d -m 0700 "$TEST_ROOT/agent" "$SNAPSHOT" "$TLS_SNAPSHOT"
printf '%s\t%s\t%s\n' \
    celikpanel-agent.service enabled active \
    celikpanel-panel.service enabled active \
    celikpanel-firewall-restore.service enabled inactive >"$LEDGER"
chmod 0600 "$LEDGER"
install -d -m 0755 "$TEST_ROOT/etc" "$TEST_ROOT/etc/letsencrypt" \
    "$TEST_ROOT/etc/letsencrypt/renewal-hooks" \
    "$TEST_ROOT/etc/letsencrypt/renewal-hooks/deploy"
install -d -m 0750 "$TLS_DIR" "$TLS_DIR/$VERSION"
printf 'CERT-A\n' > "$TLS_DIR/$VERSION/panel.crt"
printf 'KEY-A\n' > "$TLS_DIR/$VERSION/panel.key"
printf 'boston.celikhost.com\n' > "$TLS_DIR/$VERSION/panel.domain"
chmod 0640 "$TLS_DIR/$VERSION/panel.crt" "$TLS_DIR/$VERSION/panel.key"
chmod 0600 "$TLS_DIR/$VERSION/panel.domain"
ln -s "$VERSION" "$TLS_DIR/current"
printf '#!/bin/sh\nexit 0\n' > "$HOOK"
chmod 0755 "$HOOK"
printf '{"version":1,"phase":"pending_source"}\n' > "$PENDING"
chmod 0600 "$PENDING"

TIMER_CONTENT='[Timer]\nOnCalendar=*-*-* 00,12:00:00\n'
TIMER_ACTIVE=active
TIMER_ENABLED=enabled
systemctl() {
    local command=$1
    shift
    case "$command" in
        show)
            local property= unit=${*: -1}
            for argument in "$@"; do
                case "$argument" in --property=*) property=${argument#--property=} ;; esac
            done
            case "$property:$unit" in
                LoadState:certbot.timer) printf 'loaded\n' ;;
                LoadState:certbot-renew.timer) printf 'not-found\n' ;;
                LoadState:certbot.service) printf 'not-found\n' ;;
                LoadState:certbot-renew.service) printf 'not-found\n' ;;
                *) return 1 ;;
            esac
            ;;
        is-enabled)
            case "${*: -1}" in certbot.timer) printf '%s\n' "$TIMER_ENABLED" ;; *) printf 'not-found\n'; return 1 ;; esac
            ;;
        is-active)
            case "${*: -1}" in
                certbot.timer) printf '%s\n' "$TIMER_ACTIVE" ;;
                certbot-renew.timer|certbot.service|certbot-renew.service) printf 'inactive\n'; return 3 ;;
                *) return 4 ;;
            esac
            ;;
        cat)
            [[ "${*: -1}" == certbot.timer ]] || return 1
            printf '%b' "$TIMER_CONTENT"
            ;;
        stop) [[ "${*: -1}" != certbot.timer ]] || TIMER_ACTIVE=inactive ;;
        start) [[ "${*: -1}" != certbot.timer ]] || TIMER_ACTIVE=active ;;
        enable) TIMER_ENABLED=enabled ;;
        disable) TIMER_ENABLED=disabled ;;
        unmask) : ;;
        mask) TIMER_ENABLED=masked ;;
        *) return 1 ;;
    esac
}

# shellcheck source=deploy/panel-tls-snapshot.sh
source "$HELPER"

# A fresh install historically created this exact two-file layout as the panel
# service user. Reject any extra object, then prove normalization changes only
# metadata, preserves bytes, and is safe to repeat after an interrupted retry.
ATOMIC_TLS_DIR=$TEST_ROOT/atomic-tls
mv -- "$TLS_DIR" "$ATOMIC_TLS_DIR"
install -d -m 0700 "$TLS_DIR"
printf 'FRESH-CERT\n' >"$TLS_DIR/panel.crt"
printf 'FRESH-KEY\n' >"$TLS_DIR/panel.key"
chmod 0600 "$TLS_DIR/panel.crt" "$TLS_DIR/panel.key"
chown -R 65534:65534 -- "$TLS_DIR"
printf 'unexpected\n' >"$TLS_DIR/unexpected"
chown 65534:65534 "$TLS_DIR/unexpected"
chmod 0600 "$TLS_DIR/unexpected"
expect_tls_failure_with_stderr 'legacy TLS normalization accepted an extra object' panel_tls_normalize_legacy_self_signed "$TLS_DIR" 65534 65534
[[ $(stat -Lc '%u:%g:%a' "$TLS_DIR") == 65534:65534:700 ]] || fail 'rejected legacy TLS normalization changed root metadata'
rm -f -- "$TLS_DIR/unexpected"
LEGACY_CERT_SHA=$(sha256sum "$TLS_DIR/panel.crt")
LEGACY_CERT_SHA=${LEGACY_CERT_SHA%% *}
LEGACY_KEY_SHA=$(sha256sum "$TLS_DIR/panel.key")
LEGACY_KEY_SHA=${LEGACY_KEY_SHA%% *}
panel_tls_normalize_legacy_self_signed "$TLS_DIR" 65534 65534 || fail 'fresh legacy TLS normalization failed'
[[ $(stat -Lc '%u:%g:%a' "$TLS_DIR") == 0:65534:750 ]] || fail 'normalized legacy TLS root metadata is wrong'
[[ $(stat -Lc '%u:%g:%a' "$TLS_DIR/panel.crt") == 0:65534:640 ]] || fail 'normalized legacy certificate metadata is wrong'
[[ $(stat -Lc '%u:%g:%a' "$TLS_DIR/panel.key") == 0:65534:640 ]] || fail 'normalized legacy key metadata is wrong'
NORMALIZED_CERT_SHA=$(sha256sum "$TLS_DIR/panel.crt")
NORMALIZED_CERT_SHA=${NORMALIZED_CERT_SHA%% *}
NORMALIZED_KEY_SHA=$(sha256sum "$TLS_DIR/panel.key")
NORMALIZED_KEY_SHA=${NORMALIZED_KEY_SHA%% *}
[[ $NORMALIZED_CERT_SHA == "$LEGACY_CERT_SHA" && $NORMALIZED_KEY_SHA == "$LEGACY_KEY_SHA" ]] || fail 'legacy TLS normalization changed certificate bytes'
panel_tls_normalize_legacy_self_signed "$TLS_DIR" 65534 65534 || fail 'legacy TLS normalization is not retry-safe'
LEGACY_TLS_SNAPSHOT=$SNAPSHOT/legacy-panel-tls
install -d -m 0700 "$LEGACY_TLS_SNAPSHOT"
panel_tls_snapshot_capture "$LEGACY_TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" \
    || fail 'normalized legacy TLS snapshot capture failed'
[[ $(stat -Lc '%u:%g' "$LEGACY_TLS_SNAPSHOT/managed/panel.crt") == 0:65534 ]] \
    || fail 'legacy certificate snapshot did not preserve its trusted group'
[[ $(stat -Lc '%u:%g' "$LEGACY_TLS_SNAPSHOT/managed/panel.key") == 0:65534 ]] \
    || fail 'legacy key snapshot did not preserve its trusted group'
grep -Fq $'F\tpanel.key\t0\t65534\t640\t' "$LEGACY_TLS_SNAPSHOT/managed.manifest" \
    || fail 'legacy key source ownership was not preserved in the manifest'
rm -rf -- "$LEGACY_TLS_SNAPSHOT"
rm -rf -- "$TLS_DIR"
mv -- "$ATOMIC_TLS_DIR" "$TLS_DIR"

panel_tls_capture_scheduler_states_to_service_ledger "$LEDGER" \
    || fail 'scheduler ledger capture failed'
[[ $(wc -l <"$LEDGER") -eq 5 ]] || fail 'scheduler ledger is not canonical five rows'
panel_tls_snapshot_capture "$TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" "$LEDGER" \
    || fail 'atomic TLS snapshot capture failed'
panel_tls_snapshot_scheduler_matches_service_ledger "$TLS_SNAPSHOT" "$LEDGER" \
    || fail 'scheduler snapshot/ledger mismatch'
[[ ! -L "$TLS_SNAPSHOT/managed/$VERSION" ]] || fail 'snapshot encoded a managed version as a symlink'
[[ "$(cat "$TLS_SNAPSHOT/layout.state")" == atomic ]] || fail 'atomic layout marker missing'
HOOK_META=$(stat -Lc '%u:%g:%a:%Y:%y' "$HOOK")
PENDING_META=$(stat -Lc '%u:%g:%a:%Y:%y' "$PENDING")

panel_tls_quiesce_certbot_scheduler "$TLS_SNAPSHOT" || fail 'Certbot scheduler quiesce failed'
[[ "$TIMER_ACTIVE" == inactive ]] || fail 'Certbot timer remained active'
expect_tls_success_without_stderr 'unchanged published TLS source proof' panel_tls_snapshot_assert_source_unchanged "$TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" quiesced

fail_find_validation() {
    CELIKPANEL_TLS_SNAPSHOT_TEST_FAIL_FIND=1 panel_tls_snapshot_validate "$TLS_SNAPSHOT"
}
fail_mountinfo_source_proof() {
    CELIKPANEL_TLS_SNAPSHOT_TEST_FAIL_MOUNTINFO=1 panel_tls_snapshot_assert_source_unchanged "$TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" quiesced
}
expect_tls_failure_with_stderr 'injected find failure was accepted' fail_find_validation
expect_tls_failure_with_stderr 'injected mountinfo failure was accepted' fail_mountinfo_source_proof
expect_tls_failure_with_stderr 'not-found active tuple was accepted' _panel_tls_validate_unit_state_tuple scheduler certbot.timer not-found not-found active
expect_tls_failure_with_stderr 'masked enabled tuple was accepted' _panel_tls_validate_unit_state_tuple scheduler certbot.timer masked enabled inactive
# Replace all managed state with a legacy/different view, then prove exact
# rollback and scheduler restoration.
rm -rf -- "$TLS_DIR"
install -d -m 0750 "$TLS_DIR"
printf 'CERT-B\n' > "$TLS_DIR/panel.crt"
printf 'KEY-B\n' > "$TLS_DIR/panel.key"
chmod 0640 "$TLS_DIR/panel.crt" "$TLS_DIR/panel.key"
printf '#!/bin/sh\nexit 9\n' > "$HOOK"
chmod 0755 "$HOOK"
printf '{"version":1,"phase":"pending_verify"}\n' > "$PENDING"
chmod 0600 "$PENDING"

expect_tls_failure_with_stderr 'mutated TLS source was accepted' panel_tls_snapshot_assert_source_unchanged "$TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" quiesced
panel_tls_restore_snapshot "$TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" \
    || fail 'TLS snapshot restore failed'
[[ "$(readlink "$TLS_DIR/current")" == "$VERSION" ]] || fail 'atomic current link was not restored'
cmp -s "$TLS_DIR/$VERSION/panel.crt" "$TLS_SNAPSHOT/managed/$VERSION/panel.crt" \
    || fail 'active certificate bytes differ after restore'
cmp -s "$PENDING" "$TLS_SNAPSHOT/pending/panel-certificate-activation.json" \
    || fail 'pending activation bytes differ after restore'
cmp -s "$HOOK" "$TLS_SNAPSHOT/hook/celikpanel-panel-cert" \
    || fail 'deploy hook bytes differ after restore'
[[ "$(stat -Lc '%u:%g:%a:%Y:%y' "$HOOK")" == "$HOOK_META" ]] \
    || fail 'deploy hook metadata differs after restore'
[[ "$(stat -Lc '%u:%g:%a:%Y:%y' "$PENDING")" == "$PENDING_META" ]] \
    || fail 'pending activation metadata differs after restore'
panel_tls_restore_certbot_scheduler "$TLS_SNAPSHOT" \
    || fail 'Certbot scheduler restore failed'
[[ "$TIMER_ACTIVE" == active && "$TIMER_ENABLED" == enabled ]] \
    || fail 'Certbot timer state differs after restore'

TIMER_CONTENT='[Timer]\nOnCalendar=hourly\n'
if panel_tls_quiesce_certbot_scheduler "$TLS_SNAPSHOT" 2>/dev/null; then
    fail 'effective Certbot timer content drift was accepted'
fi

# Absence is state, not an instruction to leave whatever a newer release
# created behind. Prove rollback removes a new TLS tree, hook and pending file.
TIMER_CONTENT='[Timer]\nOnCalendar=*-*-* 00,12:00:00\n'
ABSENT_SNAPSHOT=$TEST_ROOT/absent-snapshot
ABSENT_TLS_SNAPSHOT=$ABSENT_SNAPSHOT/panel-tls
ABSENT_LEDGER=$ABSENT_SNAPSHOT/service-states.tsv
rm -rf -- "$TLS_DIR"
rm -f -- "$HOOK" "$PENDING"
install -d -m 0700 "$ABSENT_SNAPSHOT" "$ABSENT_TLS_SNAPSHOT"
cp -- "$LEDGER" "$ABSENT_LEDGER"
chmod 0600 "$ABSENT_LEDGER"
panel_tls_snapshot_capture "$ABSENT_TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" "$ABSENT_LEDGER" \
    || fail 'absent TLS snapshot capture failed'
panel_tls_snapshot_scheduler_matches_service_ledger "$ABSENT_TLS_SNAPSHOT" "$ABSENT_LEDGER" \
    || fail 'absent scheduler snapshot/ledger mismatch'
[[ "$(cat "$ABSENT_TLS_SNAPSHOT/layout.state")" == absent ]] \
    || fail 'absent TLS layout marker missing'
install -d -m 0750 "$TLS_DIR"
printf 'NEW-CERT\n' > "$TLS_DIR/panel.crt"
printf 'NEW-KEY\n' > "$TLS_DIR/panel.key"
chmod 0640 "$TLS_DIR/panel.crt" "$TLS_DIR/panel.key"
printf '#!/bin/sh\nexit 7\n' > "$HOOK"
chmod 0755 "$HOOK"
printf '{"version":1,"phase":"pending_publish"}\n' > "$PENDING"
chmod 0600 "$PENDING"
panel_tls_quiesce_certbot_scheduler "$ABSENT_TLS_SNAPSHOT" \
    || fail 'absent-state scheduler quiesce failed'
panel_tls_restore_snapshot "$ABSENT_TLS_SNAPSHOT" "$TLS_DIR" "$PENDING" "$HOOK" \
    || fail 'absent TLS snapshot restore failed'
[[ ! -e "$TLS_DIR" && ! -L "$TLS_DIR" ]] || fail 'new TLS tree survived absent-state rollback'
[[ ! -e "$HOOK" && ! -L "$HOOK" ]] || fail 'new deploy hook survived absent-state rollback'
[[ ! -e "$PENDING" && ! -L "$PENDING" ]] || fail 'new pending state survived absent-state rollback'
panel_tls_restore_certbot_scheduler "$ABSENT_TLS_SNAPSHOT" \
    || fail 'absent-state scheduler restore failed'

printf 'PASS: panel TLS snapshot/rollback contract\n'
