#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bootstrap="$repo_root/download-portal/get.sh"
installer="$repo_root/install.sh"
public_key="$repo_root/deploy/release-signing-ed25519.pem"

fail() {
  printf 'first-install signed trust contract failed: %s\n' "$1" >&2
  exit 1
}

line_of() {
  grep -Fn -- "$1" "$2" | tail -n 1 | cut -d: -f1
}

[[ "$(sha256sum "$public_key" | awk '{print $1}')" == \
   7eadeb0b156f1a821575c4293fe664b44b8004bcdb5e9e770122cb5c144c68bb ]] \
  || fail "tracked public-key digest changed"
openssl pkey -pubin -passin pass: -in "$public_key" -pubout 2>/dev/null \
  | cmp -s - "$public_key" || fail "tracked public key is not canonical PEM"
openssl pkey -pubin -passin pass: -in "$public_key" -text -noout 2>/dev/null \
  | LC_ALL=C grep -Eq '^ED25519 Public-Key:' || fail "tracked public key is not Ed25519"

dash -n "$bootstrap" || fail "download bootstrap is not POSIX shell"
bash -n "$installer" || fail "installer shell syntax is invalid"

key_fetch=$(line_of 'signed_fetch "$base_url/release-signing-ed25519.pem"' "$bootstrap")
manifest_fetch=$(line_of 'signed_fetch "$release_url/release-manifest-v2"' "$bootstrap")
archive_fetch=$(line_of 'signed_fetch "$release_url/$archive"' "$bootstrap")
lock_provision=$(line_of 'provision_first_install_signed_update_lock || fail' "$bootstrap")
bootstrap_lock=$(line_of 'acquire_signed_update_lock || fail' "$bootstrap")
bootstrap_preflight_count=$(grep -Fnc 'preflight_first_install_trust_state \' "$bootstrap")
bootstrap_preflight_first=$(grep -Fn 'preflight_first_install_trust_state \' "$bootstrap" | head -n 1 | cut -d: -f1)
bootstrap_preflight_locked=$(grep -Fn 'preflight_first_install_trust_state \' "$bootstrap" | tail -n 1 | cut -d: -f1)
installer_call=$(line_of 'bash "$installer"' "$bootstrap")
[[ "$bootstrap_preflight_count" -eq 2 &&
   "$key_fetch" -lt "$manifest_fetch" &&
   "$manifest_fetch" -lt "$bootstrap_preflight_first" &&
   "$bootstrap_preflight_first" -lt "$lock_provision" &&
   "$lock_provision" -lt "$bootstrap_lock" &&
   "$bootstrap_lock" -lt "$bootstrap_preflight_locked" &&
   "$bootstrap_preflight_locked" -lt "$archive_fetch" &&
   "$archive_fetch" -lt "$installer_call" ]] \
  || fail "bootstrap trust verification order is unsafe"
grep -Fq 'workdir=$(mktemp -d "$releases_root/.download.XXXXXXXX")' "$bootstrap" \
  || fail "first install is not staged below protected release storage"
! grep -Fq 'workdir=$(mktemp -d /tmp/celikpanel-install.' "$bootstrap" \
  || fail "first install still stages authenticated trust below /tmp"
grep -Fq 'CELIKPANEL_FIRST_INSTALL_PUBLIC_KEY_FILE="$signed_public_key_path"' "$bootstrap" \
  || fail "bootstrap does not pass the verified key source"
grep -Fq 'CELIKPANEL_FIRST_INSTALL_COMMIT="$signed_commit"' "$bootstrap" \
  || fail "bootstrap does not pass the signed installed identity"
grep -Fq 'CELIKPANEL_FIRST_INSTALL_LOCK_FD=9' "$bootstrap" \
  || fail "bootstrap does not pass its held update-lock descriptor"

panel_install=$(line_of 'install -m 0755 "$SRC/bin/panel"' "$installer")
agent_install=$(line_of 'install -m 0755 "$SRC/bin/agent"' "$installer")
lock_verify=$(line_of 'acquire_first_install_signed_update_lock' "$installer")
trust_preflight=$(line_of 'preflight_first_install_signed_release_trust' "$installer")
admin_admission=$(line_of 'preflight_first_administrator_admission' "$installer")
updater_install=$(line_of 'install_reviewed_release_updater' "$installer")
trust_enroll=$(line_of 'enroll_first_install_signed_release_trust' "$installer")
agent_start=$(line_of 'step "Starting the agent"' "$installer")
complete_publish=$(line_of 'install -m 0600 -o root -g root /dev/null "$INSTALL_COMPLETE"' "$installer")
[[ "$lock_verify" -lt "$trust_preflight" &&
   "$trust_preflight" -lt "$admin_admission" &&
   "$admin_admission" -lt "$panel_install" &&
   "$trust_preflight" -lt "$panel_install" &&
   "$trust_preflight" -lt "$agent_install" &&
   "$panel_install" -lt "$updater_install" &&
   "$agent_install" -lt "$updater_install" &&
   "$updater_install" -lt "$trust_enroll" &&
   "$trust_enroll" -lt "$agent_start" &&
   "$trust_enroll" -lt "$complete_publish" ]] \
  || fail "installed-identity enrollment order is unsafe"

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
bootstrap_sequence_probe="$tmp/bootstrap-sequence-probe.sh"
for function_name in decimal_not_greater_than valid_release_sequence; do
  awk -v function_name="$function_name" '
    $0 == function_name "() {" { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$bootstrap" >> "$tmp/bootstrap-sequence.function"
done
cat > "$bootstrap_sequence_probe" <<'EOF'
#!/bin/sh
set -eu
. "$2"
valid_release_sequence "$1"
EOF
chmod 0755 "$bootstrap_sequence_probe"
for accepted_sequence in 1 9223372036854775807; do
  "$bootstrap_sequence_probe" "$accepted_sequence" "$tmp/bootstrap-sequence.function" \
    || fail "bootstrap rejected canonical sequence $accepted_sequence"
done
for rejected_sequence in 0 041 9223372036854775808 9999999999999999999; do
  if "$bootstrap_sequence_probe" "$rejected_sequence" \
      "$tmp/bootstrap-sequence.function" >/dev/null 2>&1; then
    fail "bootstrap accepted invalid sequence $rejected_sequence"
  fi
done

validator="$tmp/validator.sh"
for function_name in valid_release_version valid_release_sequence validate_first_install_trust_contract; do
  awk -v function_name="$function_name" '
    $0 == function_name "() {" { capture=1 }
    capture { print }
    capture && /^}/ { exit }
  ' "$installer" >> "$tmp/validator.function"
done
cat > "$validator" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
APPLY_ONLY=$1
FIRST_INSTALL_TRUST_REQUESTED=$2
FIRST_INSTALL_PUBLIC_KEY_FILE=$3
FIRST_INSTALL_RELEASE_SEQUENCE=$4
FIRST_INSTALL_RELEASE_VERSION=$5
FIRST_INSTALL_RELEASE_COMMIT=$6
CELIKPANEL_RELEASE_PUBLIC_KEY_FILE=$7
FIRST_INSTALL_INHERITED_LOCK_FD=$8
FIRST_INSTALL_LOCK_FD=9
die() { exit 1; }
. "$9"
validate_first_install_trust_contract
EOF
chmod 0755 "$validator"

valid_commit=0123456789abcdef0123456789abcdef01234567
"$validator" 0 1 /trusted/release.pem 41 v0.1.0-alpha.41 "$valid_commit" '' 9 \
  "$tmp/validator.function" || fail "complete first-install trust contract was rejected"
"$validator" 0 1 /trusted/release.pem 9223372036854775807 v0.1.0-alpha.41 \
  "$valid_commit" '' 9 "$tmp/validator.function" \
  || fail "INT64_MAX first-install sequence was rejected"

expect_rejected() {
  local label=$1
  shift
  if "$@" >/dev/null 2>&1; then fail "$label was accepted"; fi
}
expect_rejected "partial trust contract" \
  "$validator" 0 1 '' 41 v0.1.0-alpha.41 "$valid_commit" '' 9 "$tmp/validator.function"
expect_rejected "trust fields without enable flag" \
  "$validator" 0 0 /trusted/release.pem 41 v0.1.0-alpha.41 "$valid_commit" '' 9 "$tmp/validator.function"
expect_rejected "apply-only trust enrollment" \
  "$validator" 1 1 /trusted/release.pem 41 v0.1.0-alpha.41 "$valid_commit" '' 9 "$tmp/validator.function"
expect_rejected "legacy and first-install enrollment combination" \
  "$validator" 0 1 /trusted/release.pem 41 v0.1.0-alpha.41 "$valid_commit" /legacy.pem 9 "$tmp/validator.function"
expect_rejected "non-canonical sequence" \
  "$validator" 0 1 /trusted/release.pem 041 v0.1.0-alpha.41 "$valid_commit" '' 9 "$tmp/validator.function"
expect_rejected "missing inherited lock descriptor" \
  "$validator" 0 1 /trusted/release.pem 41 v0.1.0-alpha.41 "$valid_commit" '' '' "$tmp/validator.function"
expect_rejected "sequence above INT64_MAX" \
  "$validator" 0 1 /trusted/release.pem 9223372036854775808 v0.1.0-alpha.41 \
  "$valid_commit" '' 9 "$tmp/validator.function"
expect_rejected "non-canonical numeric prerelease identifier" \
  "$validator" 0 1 /trusted/release.pem 41 v0.1.0-alpha.01 "$valid_commit" '' 9 \
  "$tmp/validator.function"

printf 'first-install signed trust contract passed\n'
