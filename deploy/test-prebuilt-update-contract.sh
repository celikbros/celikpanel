#!/usr/bin/env bash
# Keep the public prebuilt update entry point inside the same immutable,
# transaction-safe boundary as the reviewed source-build updater.
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bootstrap="$repo_root/bootstrap-prebuilt-update.sh"
makefile="$repo_root/Makefile"
tls_helper="$repo_root/deploy/panel-tls-snapshot.sh"

fail() {
  printf 'prebuilt update contract failed: %s\n' "$1" >&2
  exit 1
}

require_literal() {
  local literal=$1 file=${2:-$bootstrap}
  grep -Fq -- "$literal" "$file" || fail "missing: $literal"
}

require_before() {
  local first=$1 second=$2 first_line second_line
  first_line=$(grep -Fn -- "$first" "$bootstrap" | head -n 1 | cut -d: -f1)
  second_line=$(grep -Fn -- "$second" "$bootstrap" | head -n 1 | cut -d: -f1)
  [[ -n "$first_line" && -n "$second_line" && "$first_line" -lt "$second_line" ]] \
    || fail "$first must precede $second"
}

extract_function_source() {
  local file=$1 function_name=$2
  awk -v header="$function_name() {" '
    $0 == header { inside = 1; found = 1 }
    inside { print }
    inside && $0 == "}" { closed = 1; exit }
    END { if (!found || !closed) exit 1 }
  ' "$file"
}

validate_real_alpha4_tls_capture_fixture() (
  set -euo pipefail
  local test_root child capture tls pending hook mountinfo

  [[ "$(id -u)" -eq 0 ]] || fail "real alpha.4 TLS capture fixture requires root"
  [[ -f "$tls_helper" && ! -L "$tls_helper" ]] || fail "TLS snapshot helper is missing or unsafe"

  test_root=$(mktemp -d /tmp/celikpanel-alpha4-tls-contract.XXXXXXXX)
  trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM
  chmod 0700 "$test_root"

  child="$test_root/update-snapshot"
  capture="$child/.panel-tls.capture.123"
  tls="$test_root/tls"
  pending="$test_root/agent/panel-certificate-activation.json"
  hook="$test_root/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert"
  mountinfo="$test_root/mountinfo"

  install -d -m 0700 "$child" "$capture" "$test_root/agent"
  install -d -m 0750 "$tls"
  install -d -m 0755 "${hook%/*}"
  : >"$mountinfo"
  chmod 0600 "$mountinfo"
  printf '%s\n' 'fixture certificate bytes' >"$tls/panel.crt"
  printf '%s\n' 'fixture private-key bytes' >"$tls/panel.key"
  chmod 0640 "$tls/panel.crt" "$tls/panel.key"

  systemctl() {
    case "${1:-}" in
      show)
        printf '%s\n' not-found
        ;;
      is-enabled)
        printf '%s\n' not-found
        return 1
        ;;
      is-active)
        printf '%s\n' inactive
        return 3
        ;;
      *)
        return 1
        ;;
    esac
  }

  export CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT="$test_root"
  export CELIKPANEL_TLS_SNAPSHOT_MOUNTINFO="$mountinfo"
  # Generate the fixture through the production capture path. This makes the
  # regression exercise the real manifest, timer and service ledgers rather
  # than a test-side imitation of their format.
  source "$tls_helper"
  panel_tls_snapshot_capture "$capture" "$tls" "$pending" "$hook" \
    || fail "production TLS snapshot helper could not create the alpha.4 fixture"

  [[ "$(wc -l <"$capture/managed.manifest")" -eq 2 ]] \
    || fail "real alpha.4 fixture must contain two managed manifest rows"
  [[ "$(wc -l <"$capture/certbot-timers.tsv")" -eq 2 ]] \
    || fail "real alpha.4 fixture must contain two timer rows"
  [[ "$(wc -l <"$capture/certbot-services.tsv")" -eq 2 ]] \
    || fail "real alpha.4 fixture must contain two service rows"

  eval "$(extract_function_source "$bootstrap" die)"
  eval "$(extract_function_source "$bootstrap" validate_known_alpha4_tls_capture)"
  PANEL_TLS_DIR="$tls"
  validate_known_alpha4_tls_capture "$capture" "$child" 0 0
)

validate_partial_alpha4_tls_capture_fixture() (
  set -euo pipefail
  umask 077
  local test_root child capture tls pending hook mountinfo panel_uid panel_gid entries candidate

  [[ "$(id -u)" -eq 0 ]] || fail "partial alpha.4 TLS capture fixture requires root"
  [[ -f "$tls_helper" && ! -L "$tls_helper" ]] || fail "TLS snapshot helper is missing or unsafe"
  panel_uid=$(id -u nobody)
  panel_gid=$(id -g nobody)
  [[ "$panel_uid" -ne 0 && "$panel_gid" -ne 0 ]] \
    || fail "partial alpha.4 fixture requires a non-root service identity"

  test_root=$(mktemp -d /tmp/celikpanel-alpha4-partial-tls-contract.XXXXXXXX)
  trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM
  chmod 0700 "$test_root"

  child="$test_root/update-snapshot"
  capture="$child/.panel-tls.capture.123"
  tls="$test_root/tls"
  pending="$test_root/agent/panel-certificate-activation.json"
  hook="$test_root/etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert"
  mountinfo="$test_root/mountinfo"

  install -d -m 0700 "$child" "$capture" "$test_root/agent"
  install -d -m 0750 "$tls"
  install -d -m 0755 "${hook%/*}"
  : >"$mountinfo"
  chmod 0600 "$mountinfo"
  printf '%s\n' 'legacy certificate bytes' >"$tls/panel.crt"
  printf '%s\n' 'legacy private-key bytes' >"$tls/panel.key"
  chmod 0640 "$tls/panel.crt" "$tls/panel.key"
  chown -R "$panel_uid:$panel_gid" "$tls"

  export CELIKPANEL_TLS_SNAPSHOT_TEST_ROOT="$test_root"
  export CELIKPANEL_TLS_SNAPSHOT_MOUNTINFO="$mountinfo"
  source "$tls_helper"
  if panel_tls_snapshot_capture "$capture" "$tls" "$pending" "$hook"; then
    fail "panel-owned legacy TLS unexpectedly produced a complete alpha.4 capture"
  fi

  entries=$(find "$capture" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
  [[ "$entries" == $'hook\nmanaged\npending\nsnapshot.version\ntimer-effective' ]] \
    || fail "production capture did not leave the reviewed alpha.4 partial prefix"

  eval "$(extract_function_source "$bootstrap" die)"
  eval "$(extract_function_source "$bootstrap" validate_known_alpha4_tls_capture)"
  PANEL_TLS_DIR="$tls"
  validate_known_alpha4_tls_capture "$capture" "$child" "$panel_uid" "$panel_gid"

  candidate="$child/.panel-tls.capture.124"
  cp -a -- "$capture" "$candidate"
  printf 'unexpected\n' >"$candidate/extra"
  if (validate_known_alpha4_tls_capture "$candidate" "$child" "$panel_uid" "$panel_gid") >/dev/null 2>&1; then
    fail "partial alpha.4 capture with an extra entry was accepted"
  fi

  candidate="$child/.panel-tls.capture.125"
  cp -a -- "$capture" "$candidate"
  rm -rf -- "$candidate/hook"
  if (validate_known_alpha4_tls_capture "$candidate" "$child" "$panel_uid" "$panel_gid") >/dev/null 2>&1; then
    fail "incomplete alpha.4 partial capture was accepted"
  fi

  candidate="$child/.panel-tls.capture.126"
  cp -a -- "$capture" "$candidate"
  printf 'payload\n' >"$candidate/hook/celikpanel-panel-cert"
  if (validate_known_alpha4_tls_capture "$candidate" "$child" "$panel_uid" "$panel_gid") >/dev/null 2>&1; then
    fail "non-empty alpha.4 partial capture directory was accepted"
  fi

  candidate="$child/.panel-tls.capture.127"
  cp -a -- "$capture" "$candidate"
  printf '3\n' >"$candidate/snapshot.version"
  if (validate_known_alpha4_tls_capture "$candidate" "$child" "$panel_uid" "$panel_gid") >/dev/null 2>&1; then
    fail "alpha.4 partial capture with another version was accepted"
  fi
)

validate_alpha4_agent_state_root_contract() (
  set -euo pipefail
  local test_root expected legacy

  test_root=$(mktemp -d /tmp/celikpanel-alpha4-agent-root-contract.XXXXXXXX)
  trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM
  expected="$test_root/expected"
  legacy="$test_root/legacy"
  printf '%s\n' /var/lib/celikpanel-agent-private >"$expected"
  printf '%s\n' /var/lib/celikpanel-agent >"$legacy"

  eval "$(extract_function_source "$bootstrap" validate_known_alpha4_agent_state_root)"
  validate_known_alpha4_agent_state_root "$expected" \
    || fail "the exact alpha.4 private agent-state root was rejected"
  if validate_known_alpha4_agent_state_root "$legacy"; then
    fail "the legacy public agent-state root was accepted"
  fi
)

validate_alpha4_pre_snapshot_cleanup_contract() (
  set -euo pipefail
  local test_root stage snapshot child capture entries

  test_root=$(mktemp -d /tmp/celikpanel-alpha4-cleanup-contract.XXXXXXXX)
  trap 'rm -rf -- "$test_root"' EXIT HUP INT TERM
  UPDATE_SNAPSHOT_ROOT="$test_root/update-snapshots"
  snapshot=20260804T132650Z-from-unknown-to-8bbbac8b628fae4fca0e127e52c1c7835f56f8b8-d1d186528aec6c8426f56f437f96e58c
  stage="$UPDATE_SNAPSHOT_ROOT/.release-snapshot.incomplete.123.d1d186528aec6c8426f56f437f96e58c"
  child="$stage/$snapshot"
  capture="$child/.panel-tls.capture.123"

  install -d -m 0700 "$capture" "$child/agent-state"
  printf 'present\n' >"$child/agent-ledger.state"
  printf '/var/lib/celikpanel-agent-private\n' >"$child/agent-state-root"
  printf 'database snapshot\n' >"$child/celikpanel.db"
  printf 'enabled\n' >"$child/service-states.tsv"
  printf 'stopped\n' >"$child/quiesce-coordinators.tsv"
  printf 'normal\n' >"$child/snapshot-transition.state"

  release_txn_validate_update_snapshot_stage() {
    [[ "$1" == "$UPDATE_SNAPSHOT_ROOT" && "$2" == "$snapshot" && "$3" == "$stage" ]]
  }

  eval "$(extract_function_source "$bootstrap" die)"
  eval "$(extract_function_source "$bootstrap" remove_known_alpha4_pre_snapshot_payload)"
  remove_known_alpha4_pre_snapshot_payload "$stage" "$snapshot"

  entries=$(find "$child" -mindepth 1 -maxdepth 1 -printf '%f\n' | LC_ALL=C sort)
  [[ "$entries" == $'quiesce-coordinators.tsv\nservice-states.tsv\nsnapshot-transition.state' ]] \
    || fail "alpha.4 cleanup did not leave the exact pre-mutation ledger"
)

[[ -f "$bootstrap" && ! -L "$bootstrap" ]] || fail "bootstrap is missing or unsafe"
bash -n "$bootstrap"

require_literal 'RELEASES_ROOT=/var/backups/celikpanel/releases'
require_literal 'for required_command in awk bash chmod chown cmp cut dirname env find flock getent grep id mv od'
require_literal 'prebuilt source is outside the fixed download staging boundary'
require_literal 'find "$root" -xdev -type l'
require_literal 'find "$root" -xdev -type f -links +1'
require_literal 'cmp -s - SHA256SUMS'
require_literal 'sha256sum -c SHA256SUMS'
require_literal 'release.version release.commit release.tree'
require_literal 'published release name is invalid'
require_literal 'mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"'
require_literal 'CELIKPANEL_TRUSTED_RELEASE_ROOT="$FINAL_RELEASE"'
require_literal 'bash "$FINAL_RELEASE/update.sh" --normal'
require_literal 'release_txn_read_active_fields "$TRANSACTION_ROOT"'
require_literal 'release_txn_parse_update_snapshot_name "$active_snapshot"'
require_literal 'known_failed_target=8bbbac8b628fae4fca0e127e52c1c7835f56f8b8'
require_literal 'local TRUSTED_RELEASE_ROOT=$FINAL_RELEASE'
require_literal 'find "$RELEASES_ROOT" -xdev -mindepth 1 -maxdepth 1 -type d'
require_literal 'validate_release_tree "$known_release" 1'
require_literal 'the immutable alpha.4 release needed for recovery is unavailable'
require_literal 'release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$transaction_fd"'
require_literal 'find_known_alpha4_tls_failure_stage'
require_literal 'release_txn_validate_service_states "$child/service-states.tsv"'
require_literal 'captured alpha.4 coordinator process still exists'
require_literal 'validate_exact_alpha2_installed_artifacts'
require_literal 'validate_known_alpha4_tls_capture'
require_literal 'validate_known_alpha4_agent_state_root'
require_literal "printf '/var/lib/celikpanel-agent-private\\n'"
require_literal 'verify_active_marker_unchanged'
require_literal 'remove_known_alpha4_pre_snapshot_payload'
[[ "$(grep -Fc 'rows=0; seen_certbot=0; seen_renew=0' "$bootstrap")" -eq 3 ]] \
  || fail "alpha.4 TLS capture row-set counters must reset independently before manifest, timer and service validation"
require_literal 'systemctl start "${saved_units[0]}"'
require_literal 'systemctl start "${saved_units[1]}"'
require_literal 'release_txn_remove_pre_mutation_active_marker'
require_literal 'release_txn_cleanup_unmarked_update_snapshot_stage'
require_literal 'panel_tls_normalize_legacy_self_signed "$PANEL_TLS_DIR" "$panel_uid" "$panel_gid"'

require_before 'validate_release_tree "$SOURCE_ROOT" 0' 'chown -R root:root -- "$SOURCE_ROOT"'
require_before 'validate_release_tree "$SOURCE_ROOT" 0' 'mv -T --no-clobber -- "$SOURCE_ROOT" "$FINAL_RELEASE"'
require_before 'validate_release_tree "$FINAL_RELEASE" 1' 'bash "$FINAL_RELEASE/update.sh" --normal'
require_before 'abort_known_older_pre_mutation_update' 'bash "$FINAL_RELEASE/update.sh" --normal'
require_before 'validate_release_tree "$known_release" 1' 'release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$transaction_fd"'
require_before 'panel_tls_normalize_legacy_self_signed "$PANEL_TLS_DIR" "$panel_uid" "$panel_gid"' 'release_txn_remove_pre_mutation_active_marker'
require_before 'release_txn_remove_pre_mutation_active_marker' 'systemctl start "${saved_units[0]}"'
require_before 'systemctl start "${saved_units[0]}"' 'systemctl start "${saved_units[1]}"'
require_before 'release_txn_remove_pre_mutation_active_marker' 'release_txn_cleanup_unmarked_update_snapshot_stage'

require_literal 'cp install.sh bootstrap-update.sh bootstrap-prebuilt-update.sh update.sh rollback.sh' "$makefile"
require_literal 'deploy/write-release-manifest.sh' "$makefile"
require_literal 'find "$root" -xdev -type f -links +1' "$repo_root/deploy/write-release-manifest.sh"
require_literal 'release.commit' "$makefile"
require_literal 'release.tree' "$makefile"

validate_real_alpha4_tls_capture_fixture
validate_partial_alpha4_tls_capture_fixture
validate_alpha4_agent_state_root_contract
validate_alpha4_pre_snapshot_cleanup_contract

printf 'prebuilt update contract passed\n'
