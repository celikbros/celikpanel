#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bootstrap="$repo_root/download-portal/get.sh"

fail() {
  printf 'download portal operation-selection test failed: %s\n' "$1" >&2
  exit 1
}

[[ "$(id -u)" -eq 0 ]] || fail "run this test as root"

tmp=$(mktemp -d /var/lib/celikpanel-get-policy-test.XXXXXXXX)
cleanup() {
  case "$tmp" in
    /var/lib/celikpanel-get-policy-test.*) ;;
    *) fail "refusing to clean unexpected fixture path: $tmp" ;;
  esac
  [[ -d "$tmp" && ! -L "$tmp" ]] || fail "fixture root is unsafe during cleanup"
  [[ "$(readlink -e -- "$tmp")" == "$tmp" ]] || fail "fixture root changed during cleanup"
  rm -rf -- "$tmp"
}
trap cleanup EXIT HUP INT TERM
chmod 0700 "$tmp"
chown root:root "$tmp"

policy="$tmp/download-operation-policy.sh"
awk '
  /^# BEGIN DOWNLOAD OPERATION POLICY$/ { capture = 1; next }
  /^# END DOWNLOAD OPERATION POLICY$/ { capture = 0; next }
  capture { print }
' "$bootstrap" > "$policy"
grep -Fq 'detect_known_interrupted_update_candidate_at()' "$policy" \
  || fail "production detector was not extracted"
grep -Fq 'select_download_operation()' "$policy" \
  || fail "production selector was not extracted"

policy_shells=(bash dash)
for policy_shell in "${policy_shells[@]}"; do
  command -v "$policy_shell" >/dev/null 2>&1 \
    || fail "$policy_shell is required for the production policy test"
  "$policy_shell" -n "$policy" \
    || fail "production policy is not valid in $policy_shell"
done

run_selection() {
  local policy_shell=$1
  shift
  "$policy_shell" -c '
    policy=$1
    shift
    # shellcheck source=/dev/null
    . "$policy"
    select_download_operation "$@"
  ' celikpanel-download-policy "$policy" "$@"
}

run_detection() {
  local policy_shell=$1
  "$policy_shell" -c '
    policy=$1
    transaction=$2
    # shellcheck source=/dev/null
    . "$policy"
    detect_known_interrupted_update_candidate_at "$transaction"
  ' celikpanel-download-policy "$policy" "$transaction"
}

expect_selection() {
  local expected=$1
  shift
  local actual policy_shell
  for policy_shell in "${policy_shells[@]}"; do
    actual=$(run_selection "$policy_shell" "$@")
    [[ "$actual" == "$expected" ]] \
      || fail "$policy_shell selection expected $expected, got $actual for: $*"
  done
}

expect_selection install auto absent 0 0 0 0 0
expect_selection update auto valid 1 1 0 0 0
expect_selection update auto absent 1 1 1 1 0
expect_selection ambiguous auto absent 0 1 0 0 0
expect_selection recovery-update auto absent 1 1 0 0 1
expect_selection ambiguous auto invalid 1 1 0 0 1
expect_selection install install invalid 0 1 1 1 0
expect_selection update update absent 0 1 0 0 0

transaction="$tmp/transaction"
install -d -m 0700 -o root -g root "$transaction"
install -m 0600 -o root -g root /dev/null "$transaction/transaction.lock"
token=$(printf 'a%.0s' {1..64})
target=8bbbac8b628fae4fca0e127e52c1c7835f56f8b8
snapshot="20260804T132650Z-from-unknown-to-${target}-d1d186528aec6c8426f56f437f96e58c"

write_marker() {
  printf 'version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n' \
    "$1" "$2" "$3" > "$transaction/active"
  chown root:root "$transaction/active"
  chmod 0600 "$transaction/active"
}

expect_rejected() {
  local label=$1
  local policy_shell
  for policy_shell in "${policy_shells[@]}"; do
    if run_detection "$policy_shell"; then
      fail "$policy_shell accepted $label"
    fi
  done
}

expect_detected() {
  local label=$1
  local policy_shell
  for policy_shell in "${policy_shells[@]}"; do
    run_detection "$policy_shell" \
      || fail "$policy_shell rejected $label"
  done
}

write_marker "$token" update "$snapshot"
expect_detected "the exact interrupted alpha.4 marker"

chmod 0644 "$transaction/transaction.lock"
expect_rejected "transaction lock with wrong mode"
chmod 0600 "$transaction/transaction.lock"

chown 1:0 "$transaction/transaction.lock"
expect_rejected "transaction lock with wrong owner"
chown root:root "$transaction/transaction.lock"

printf 'not-empty\n' > "$transaction/transaction.lock"
expect_rejected "non-empty transaction lock"
: > "$transaction/transaction.lock"

ln "$transaction/transaction.lock" "$transaction/transaction.lock.hardlink"
expect_rejected "hard-linked transaction lock"
rm -f -- "$transaction/transaction.lock.hardlink"

mv -- "$transaction/transaction.lock" "$transaction/transaction.lock.regular"
ln -s transaction.lock.regular "$transaction/transaction.lock"
expect_rejected "symbolic-link transaction lock"
rm -f -- "$transaction/transaction.lock"
mv -- "$transaction/transaction.lock.regular" "$transaction/transaction.lock"

chmod 0644 "$transaction/active"
expect_rejected "active marker with wrong mode"
chmod 0600 "$transaction/active"

chown 0:1 "$transaction/active"
expect_rejected "active marker with wrong group"
chown root:root "$transaction/active"

: > "$transaction/active"
expect_rejected "empty active marker"
write_marker "$token" update "$snapshot"

printf '%0513d' 0 > "$transaction/active"
expect_rejected "active marker larger than 512 bytes"
write_marker "$token" update "$snapshot"

ln "$transaction/active" "$transaction/active.hardlink"
expect_rejected "hard-linked active marker"
rm -f -- "$transaction/active.hardlink"

mv -- "$transaction/active" "$transaction/active.regular"
ln -s active.regular "$transaction/active"
expect_rejected "symbolic-link active marker"
rm -f -- "$transaction/active"
mv -- "$transaction/active.regular" "$transaction/active"

write_marker "$token" install "$snapshot"
expect_rejected "wrong operation"

wrong_target=0123456789abcdef0123456789abcdef01234567
write_marker "$token" update \
  "20260804T132650Z-from-unknown-to-${wrong_target}-d1d186528aec6c8426f56f437f96e58c"
expect_rejected "wrong target commit"

write_marker "${token%a}" update "$snapshot"
expect_rejected "short token"

uppercase_token=$(printf 'A%.0s' {1..64})
write_marker "$uppercase_token" update "$snapshot"
expect_rejected "uppercase token"

write_marker "$token" update \
  "20260804T132650Z-from-unknown-to-${target}-D1D186528AEC6C8426F56F437F96E58C"
expect_rejected "uppercase snapshot nonce"

write_marker "$token" update "$snapshot"
printf 'unexpected=fifth-line\n' >> "$transaction/active"
expect_rejected "extra marker line"

printf 'version=1\ntoken=%s\noperation=update\nsnapshot=%s' \
  "$token" "$snapshot" > "$transaction/active"
chown root:root "$transaction/active"
chmod 0600 "$transaction/active"
expect_rejected "marker without a final newline"

write_marker "$token" update "$snapshot"
ln -s /dev/null "$transaction/quiesce.pending"
expect_rejected "coexisting phase-marker symlink"
rm -f -- "$transaction/quiesce.pending"

for phase_marker in \
  quiesce.pending completion.pending scheduler-restore.pending; do
  install -m 0600 -o root -g root /dev/null "$transaction/$phase_marker"
  expect_rejected "coexisting regular $phase_marker"
  rm -f -- "$transaction/$phase_marker"
done

chmod 0755 "$transaction"
expect_rejected "transaction root with wrong mode"
chmod 0700 "$transaction"

chown 1:0 "$transaction"
expect_rejected "transaction root with wrong owner"
chown root:root "$transaction"

chmod 0720 "$tmp"
expect_rejected "group-writable parent directory"
chmod 0700 "$tmp"

expect_detected "the restored exact marker"

printf 'download portal operation-selection test passed\n'
