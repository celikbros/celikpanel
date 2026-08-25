#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd -- "$(dirname -- "$0")/.." && pwd)
helper=$repo_root/deploy/enroll-signed-release-trust.sh

fail() {
  printf 'signed release enrollment contract failed: %s\n' "$1" >&2
  exit 1
}

[[ $(id -u) -eq 0 ]] || fail 'this contract must run as root'
bash -n "$helper" || fail 'enrollment helper has invalid syntax'
grep -Fq 'trap cleanup EXIT' "$helper" \
  || fail 'enrollment helper lacks its normal cleanup trap'
grep -Fq "trap 'on_signal 130' INT" "$helper" \
  || fail 'enrollment interrupt handler is not fail-stop'
grep -Fq "trap 'on_signal 143' TERM" "$helper" \
  || fail 'enrollment terminate handler is not fail-stop'
for command in cmp flock openssl readlink setpriv sha256sum stat; do
  command -v "$command" >/dev/null 2>&1 || fail "$command is required"
done
if grep -Eq '(^|[^[:alnum:]_])(curl|wget|ssh|systemctl)([^[:alnum:]_]|$)' "$helper"; then
  fail 'enrollment helper contains a network or service-control command'
fi

test_root=$(mktemp -d /tmp/celikpanel-enrollment-contract.XXXXXXXX)
trap 'rm -rf -- "$test_root"' EXIT
chown root:root -- "$test_root"
chmod 0700 -- "$test_root"

panel_bin=$test_root/opt/celikpanel/bin/panel
agent_bin=$test_root/opt/celikpanel/bin/agent
conf_dir=$test_root/etc/celikpanel
key_target=$conf_dir/release-signing-ed25519.pem
state_parent=$test_root/var/lib
state_dir=$state_parent/celikpanel-release-state
floor=$state_dir/sequence.floor
lock=$state_dir/update.lock
key_dir=$test_root/operator-key
public_key=$key_dir/release-ed25519.pem
other_public_key=$key_dir/other-release-ed25519.pem
version=v0.1.0-alpha.13
commit=f568c566faf141ec2814e86eabe4e382ee490790
sequence=13

install -d -o root -g root -m 0755 \
  "$test_root/opt" "$test_root/opt/celikpanel" "$test_root/opt/celikpanel/bin" \
  "$test_root/etc" "$test_root/var" "$state_parent" "$key_dir"
install -d -o root -g 65534 -m 0750 "$conf_dir"

make_identity_binary() {
  local path=$1 identity_version=$2 identity_commit=$3
  cat > "$path" <<EOF
#!/bin/sh
[ "\$#" -eq 1 ] && [ "\$1" = --inspect-build-identity ] || exit 64
if [ -n "\${CELIKPANEL_ENROLLMENT_TEST_COUNT_FILE:-}" ]; then
  count=0
  [ ! -s "\$CELIKPANEL_ENROLLMENT_TEST_COUNT_FILE" ] || \
    count=\$(cat -- "\$CELIKPANEL_ENROLLMENT_TEST_COUNT_FILE")
  count=\$((count + 1))
  printf '%s\n' "\$count" > "\$CELIKPANEL_ENROLLMENT_TEST_COUNT_FILE"
  if [ "\$count" -eq "\${CELIKPANEL_ENROLLMENT_TEST_BLOCK_AT:-0}" ]; then
    printf '%s\n' "\$\$" > "\$CELIKPANEL_ENROLLMENT_TEST_READY_FILE"
    # Stay inside this process so killing the recorded PID cannot leave a
    # descendant holding the inherited enrollment lock descriptor.
    while :; do :; done
  fi
fi
printf 'version=%s\\ncommit=%s\\n' '$identity_version' '$identity_commit'
EOF
  chown root:root -- "$path"
  chmod 0755 -- "$path"
}

make_identity_binary "$panel_bin" "$version" "$commit"
make_identity_binary "$agent_bin" "$version" "$commit"
openssl genpkey -algorithm ED25519 -out "$key_dir/private.pem" >/dev/null 2>&1
openssl pkey -in "$key_dir/private.pem" -pubout -out "$public_key" >/dev/null 2>&1
openssl genpkey -algorithm ED25519 -out "$key_dir/other-private.pem" >/dev/null 2>&1
openssl pkey -in "$key_dir/other-private.pem" -pubout -out "$other_public_key" >/dev/null 2>&1
chown root:root -- "$key_dir"/*.pem
chmod 0600 -- "$key_dir"/*.pem

run_enrollment() (
  # shellcheck disable=SC1090
  source "$helper"
  PANEL_BIN=$panel_bin
  AGENT_BIN=$agent_bin
  RELEASE_PUBLIC_KEY=$key_target
  RELEASE_STATE_DIR=$state_dir
  RELEASE_SEQUENCE_FLOOR=$floor
  SIGNED_UPDATE_LOCK=$lock
  BINARY_TRUST_ANCHOR=$test_root
  KEY_TRUST_ANCHOR=$test_root
  STATE_TRUST_ANCHOR=$test_root
  main "$@"
)

run_preflight() (
  # shellcheck disable=SC1090
  source "$helper"
  PANEL_BIN=$panel_bin
  AGENT_BIN=$agent_bin
  RELEASE_PUBLIC_KEY=$key_target
  RELEASE_STATE_DIR=$state_dir
  RELEASE_SEQUENCE_FLOOR=$floor
  SIGNED_UPDATE_LOCK=$lock
  BINARY_TRUST_ANCHOR=$test_root
  KEY_TRUST_ANCHOR=$test_root
  STATE_TRUST_ANCHOR=$test_root
  main "$@" --preflight-only
)

run_preflight_external() {
  bash -c '
    source "$1"
    PANEL_BIN=$2
    AGENT_BIN=$3
    RELEASE_PUBLIC_KEY=$4
    RELEASE_STATE_DIR=$5
    RELEASE_SEQUENCE_FLOOR=$6
    SIGNED_UPDATE_LOCK=$7
    BINARY_TRUST_ANCHOR=$8
    KEY_TRUST_ANCHOR=$8
    STATE_TRUST_ANCHOR=$8
    main "${@:9}" --preflight-only
  ' _ "$helper" "$panel_bin" "$agent_bin" "$key_target" "$state_dir" \
    "$floor" "$lock" "$test_root" "$@"
}

run_enrollment_with_group() {
  local effective_group=$1
  shift
  setpriv --reuid 0 --regid "$effective_group" --clear-groups \
    bash -c '
      source "$1"
      PANEL_BIN=$2
      AGENT_BIN=$3
      RELEASE_PUBLIC_KEY=$4
      RELEASE_STATE_DIR=$5
      RELEASE_SEQUENCE_FLOOR=$6
      SIGNED_UPDATE_LOCK=$7
      BINARY_TRUST_ANCHOR=$8
      KEY_TRUST_ANCHOR=$8
      STATE_TRUST_ANCHOR=$8
      main "${@:9}"
    ' _ "$helper" "$panel_bin" "$agent_bin" "$key_target" "$state_dir" \
      "$floor" "$lock" "$test_root" "$@"
}

start_signal_enrollment() {
  exec env CELIKPANEL_ENROLLMENT_TEST_COUNT_FILE=$test_root/signal.count \
    CELIKPANEL_ENROLLMENT_TEST_BLOCK_AT=3 \
    CELIKPANEL_ENROLLMENT_TEST_READY_FILE=$test_root/signal.ready \
    bash -c '
      source "$1"
      PANEL_BIN=$2
      AGENT_BIN=$3
      RELEASE_PUBLIC_KEY=$4
      RELEASE_STATE_DIR=$5
      RELEASE_SEQUENCE_FLOOR=$6
      SIGNED_UPDATE_LOCK=$7
      BINARY_TRUST_ANCHOR=$8
      KEY_TRUST_ANCHOR=$8
      STATE_TRUST_ANCHOR=$8
      main "${@:9}"
    ' _ "$helper" "$panel_bin" "$agent_bin" "$key_target" "$state_dir" \
      "$floor" "$lock" "$test_root" "${approved_args[@]}"
}

expect_rejected() {
  local label=$1
  shift
  if "$@" >"$test_root/rejected.out" 2>"$test_root/rejected.err"; then
    fail "$label was accepted"
  fi
}

approved_args=(
  --sequence "$sequence"
  --version "$version"
  --commit "$commit"
  --public-key-file "$public_key"
)

expect_locked_preflight_rejected() {
  local label=$1
  exec 8<>"$lock"
  flock -n 8 || fail "could not acquire $label preflight lock"
  if run_preflight_external "${approved_args[@]}" --locked-fd 8 \
      >"$test_root/rejected.out" 2>"$test_root/rejected.err"; then
    flock -u 8
    exec 8>&-
    fail "$label was accepted by locked preflight"
  fi
  flock -u 8
  exec 8>&-
}

# The first-install preflight is read-only and accepts only absent-or-exact
# partial trust state. It never requires the product binaries to be replaced.
panel_hash_before=$(sha256sum "$panel_bin" | awk '{print $1}')
agent_hash_before=$(sha256sum "$agent_bin" | awk '{print $1}')
run_preflight "${approved_args[@]}" >/dev/null \
  || fail 'absent first-install trust state was rejected'
[[ ! -e "$key_target" && ! -e "$state_dir" ]] \
  || fail 'absent-state preflight created trust state'

cp -- "$public_key" "$key_target"
chown root:root -- "$key_target"
chmod 0644 -- "$key_target"
run_preflight "${approved_args[@]}" >/dev/null \
  || fail 'exact key-only first-install state was rejected'
rm -- "$key_target"

install -d -o root -g root -m 0700 "$state_dir"
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$sequence" "version=$version" > "$floor"
chown root:root -- "$floor"
chmod 0600 -- "$floor"
run_preflight "${approved_args[@]}" >/dev/null \
  || fail 'exact floor-only first-install state was rejected'
cp -- "$public_key" "$key_target"
chown root:root -- "$key_target"
chmod 0644 -- "$key_target"
key_preflight_inode=$(stat -Lc '%d:%i' -- "$key_target")
floor_preflight_inode=$(stat -Lc '%d:%i' -- "$floor")
run_preflight "${approved_args[@]}" >/dev/null \
  || fail 'exact key-and-floor first-install state was rejected'
[[ "$(stat -Lc '%d:%i' -- "$key_target")" == "$key_preflight_inode" &&
   "$(stat -Lc '%d:%i' -- "$floor")" == "$floor_preflight_inode" ]] \
  || fail 'exact preflight replaced trusted state'

max_sequence=9223372036854775807
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$max_sequence" "version=$version" > "$floor"
max_args=(
  --sequence "$max_sequence"
  --version "$version"
  --commit "$commit"
  --public-key-file "$public_key"
)
run_preflight "${max_args[@]}" >/dev/null \
  || fail 'INT64 maximum first-install floor was rejected'
for invalid_sequence in 9223372036854775808 9999999999999999999; do
  expect_rejected "out-of-range first-install sequence $invalid_sequence" \
    run_preflight --sequence "$invalid_sequence" --version "$version" \
      --commit "$commit" --public-key-file "$public_key"
done
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$sequence" "version=$version" > "$floor"

printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$((sequence - 1))" "version=$version" > "$floor"
expect_rejected 'lower first-install floor' run_preflight "${approved_args[@]}"
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$((sequence + 1))" "version=$version" > "$floor"
expect_rejected 'higher first-install floor' run_preflight "${approved_args[@]}"
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$sequence" 'version=v0.1.0-alpha.14' > "$floor"
expect_rejected 'different-version first-install floor' run_preflight "${approved_args[@]}"
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$sequence" "version=$version" > "$floor"
cp -- "$other_public_key" "$key_target"
chmod 0644 -- "$key_target"
expect_rejected 'different first-install public key' run_preflight "${approved_args[@]}"
cp -- "$public_key" "$key_target"
chmod 0644 -- "$key_target"

: > "$lock"
chown root:root -- "$lock"
chmod 0600 -- "$lock"
exec 8<>"$lock"
flock -n 8 || fail 'could not acquire inherited preflight lock'
run_preflight_external "${approved_args[@]}" --locked-fd 8 >/dev/null \
  || fail 'held inherited preflight lock was rejected'
flock -u 8
expect_rejected 'unheld inherited preflight lock' \
  run_preflight_external "${approved_args[@]}" --locked-fd 8
exec 8>&-
[[ "$(sha256sum "$panel_bin" | awk '{print $1}')" == "$panel_hash_before" &&
   "$(sha256sum "$agent_bin" | awk '{print $1}')" == "$agent_hash_before" ]] \
  || fail 'first-install trust preflight changed installed binaries'
rm -- "$key_target" "$floor"

run_enrollment "${approved_args[@]}" > "$test_root/first.out" \
  || fail 'first enrollment failed'
grep -Fq "Signed release trust enrolled for $version at sequence $sequence ($commit)." \
  "$test_root/first.out" || fail 'first enrollment did not report exact identity'
[[ "$(stat -Lc '%u:%g:%a' -- "$state_dir")" == 0:0:700 ]] \
  || fail 'release-state directory metadata is not exact'
[[ "$(stat -Lc '%u:%g:%a:%h:%s' -- "$lock")" == 0:0:600:1:0 ]] \
  || fail 'update lock metadata is not exact'
[[ "$(stat -Lc '%u:%g:%a:%h' -- "$floor")" == 0:0:600:1 ]] \
  || fail 'sequence floor metadata is not exact'
[[ "$(stat -Lc '%u:%g:%a:%h' -- "$key_target")" == 0:0:644:1 ]] \
  || fail 'release public key metadata is not exact'
cmp -s -- "$public_key" "$key_target" || fail 'release public key bytes differ'
printf '%s\n' format=celikpanel-release-sequence-floor-v1 \
  "sequence=$sequence" "version=$version" | cmp -s - "$floor" \
  || fail 'sequence floor bytes are not canonical'

key_hash=$(sha256sum "$key_target" | awk '{print $1}')
floor_hash=$(sha256sum "$floor" | awk '{print $1}')
lock_inode=$(stat -Lc '%d:%i' -- "$lock")
run_enrollment "${approved_args[@]}" >/dev/null \
  || fail 'exact idempotent enrollment failed'
[[ "$(sha256sum "$key_target" | awk '{print $1}')" == "$key_hash" &&
   "$(sha256sum "$floor" | awk '{print $1}')" == "$floor_hash" &&
   "$(stat -Lc '%d:%i' -- "$lock")" == "$lock_inode" ]] \
  || fail 'idempotent enrollment replaced trusted state'

# A crash after either publication remains recoverable by the exact same
# operator-approved invocation.
rm -- "$floor"
sync -f -- "$state_dir"
run_enrollment "${approved_args[@]}" >/dev/null \
  || fail 'key-only partial enrollment did not converge'
rm -- "$key_target"
sync -f -- "$conf_dir"
run_enrollment "${approved_args[@]}" >/dev/null \
  || fail 'floor-only partial enrollment did not converge'
cmp -s -- "$public_key" "$key_target" || fail 'partial recovery changed key bytes'

# Exact safe staging artifacts from a killed writer are cleaned under lock.
printf 'stale-public-stage\n' > "$conf_dir/.release-signing-ed25519.pem.enroll.ABCDEFGH"
chown root:root -- "$conf_dir/.release-signing-ed25519.pem.enroll.ABCDEFGH"
chmod 0644 -- "$conf_dir/.release-signing-ed25519.pem.enroll.ABCDEFGH"
printf 'stale-floor-stage\n' > "$state_dir/.sequence.floor.enroll.ABCDEFGH"
chown root:root -- "$state_dir/.sequence.floor.enroll.ABCDEFGH"
chmod 0600 -- "$state_dir/.sequence.floor.enroll.ABCDEFGH"
exec 8<>"$lock"
flock -n 8 || fail 'could not acquire safe-stage preflight lock'
run_preflight_external "${approved_args[@]}" --locked-fd 8 >/dev/null \
  || fail 'safe staging artifacts were rejected by locked preflight'
flock -u 8
exec 8>&-
[[ -e "$conf_dir/.release-signing-ed25519.pem.enroll.ABCDEFGH" &&
   -e "$state_dir/.sequence.floor.enroll.ABCDEFGH" ]] \
  || fail 'read-only preflight removed safe staging artifacts'
run_enrollment "${approved_args[@]}" >/dev/null \
  || fail 'safe staging recovery failed'
[[ ! -e "$conf_dir/.release-signing-ed25519.pem.enroll.ABCDEFGH" &&
   ! -e "$state_dir/.sequence.floor.enroll.ABCDEFGH" ]] \
  || fail 'safe staging artifacts were retained'

# mktemp creates root:<effective-gid> 0600 before the writer can normalize the
# stage to root:root. A crash in that exact window must remain recoverable.
creator_group=65534
printf 'creator-public-stage\n' > "$conf_dir/.release-signing-ed25519.pem.enroll.GIDSTAGE"
chown "root:$creator_group" -- "$conf_dir/.release-signing-ed25519.pem.enroll.GIDSTAGE"
chmod 0600 -- "$conf_dir/.release-signing-ed25519.pem.enroll.GIDSTAGE"
printf 'creator-floor-stage\n' > "$state_dir/.sequence.floor.enroll.GIDSTAGE"
chown "root:$creator_group" -- "$state_dir/.sequence.floor.enroll.GIDSTAGE"
chmod 0600 -- "$state_dir/.sequence.floor.enroll.GIDSTAGE"
run_enrollment_with_group "$creator_group" "${approved_args[@]}" >/dev/null \
  || fail 'nonzero effective-GID staging recovery failed'
[[ ! -e "$conf_dir/.release-signing-ed25519.pem.enroll.GIDSTAGE" &&
   ! -e "$state_dir/.sequence.floor.enroll.GIDSTAGE" ]] \
  || fail 'nonzero effective-GID staging artifacts were retained'

# TERM after the persistent update lock is held must stop at once, preserve an
# uncommitted trust state, return the conventional signal status, and release
# the lock only through EXIT cleanup.
rm -- "$key_target" "$floor"
sync -f -- "$conf_dir" "$state_dir"
: > "$test_root/signal.count"
rm -f -- "$test_root/signal.ready"
start_signal_enrollment >"$test_root/signal.out" 2>"$test_root/signal.err" &
signal_pid=$!
for _ in $(seq 1 500); do
  [[ ! -e "$test_root/signal.ready" ]] || break
  sleep 0.01
done
[[ -e "$test_root/signal.ready" ]] \
  || fail 'signal regression did not reach the locked identity proof'
if flock -n "$lock" -c true; then
  fail 'signal regression reached its hook without holding the update lock'
fi
blocked_identity_pid=$(cat -- "$test_root/signal.ready")
kill -TERM -- "$signal_pid"
kill -TERM -- "$blocked_identity_pid" 2>/dev/null || true
set +e
wait "$signal_pid"
signal_status=$?
set -e
[[ "$signal_status" -eq 143 ]] \
  || fail "TERM returned $signal_status instead of 143"
[[ ! -e "$key_target" && ! -e "$floor" ]] \
  || fail 'TERM published trust after the operator aborted enrollment'
flock -n "$lock" -c true \
  || fail 'TERM did not release the persistent update lock'
unset CELIKPANEL_ENROLLMENT_TEST_COUNT_FILE \
  CELIKPANEL_ENROLLMENT_TEST_BLOCK_AT CELIKPANEL_ENROLLMENT_TEST_READY_FILE
run_enrollment "${approved_args[@]}" >/dev/null \
  || fail 'exact enrollment could not resume after TERM'

# An active updater/enrollment wins the persistent lock; no trusted bytes move.
exec 8<>"$lock"
flock -n 8 || fail 'test could not acquire update lock'
expect_rejected 'concurrent enrollment' run_enrollment "${approved_args[@]}"
exec 8>&-
[[ "$(sha256sum "$key_target" | awk '{print $1}')" == "$key_hash" &&
   "$(sha256sum "$floor" | awk '{print $1}')" == "$floor_hash" ]] \
  || fail 'lock contention changed trusted state'

expect_rejected 'lower sequence' run_enrollment \
  --sequence 12 --version "$version" --commit "$commit" --public-key-file "$public_key"
expect_rejected 'higher sequence replacement' run_enrollment \
  --sequence 14 --version "$version" --commit "$commit" --public-key-file "$public_key"
expect_rejected 'different public key' run_enrollment \
  --sequence "$sequence" --version "$version" --commit "$commit" \
  --public-key-file "$other_public_key"

other_version=v0.1.0-alpha.14
other_commit=0123456789abcdef0123456789abcdef01234567
make_identity_binary "$panel_bin" "$other_version" "$other_commit"
make_identity_binary "$agent_bin" "$other_version" "$other_commit"
expect_rejected 'same-sequence different-version enrollment' run_enrollment \
  --sequence "$sequence" --version "$other_version" --commit "$other_commit" \
  --public-key-file "$public_key"
make_identity_binary "$panel_bin" "$version" "$commit"
make_identity_binary "$agent_bin" "$version" "$other_commit"
expect_rejected 'mismatched installed build pair' run_enrollment "${approved_args[@]}"
make_identity_binary "$agent_bin" "$version" "$commit"

ln -s -- "$public_key" "$state_dir/.sequence.floor.enroll.BADSTAGE"
printf 'safe-mixed-stage\n' > "$conf_dir/.release-signing-ed25519.pem.enroll.MIXEDOK1"
chown root:root -- "$conf_dir/.release-signing-ed25519.pem.enroll.MIXEDOK1"
chmod 0644 -- "$conf_dir/.release-signing-ed25519.pem.enroll.MIXEDOK1"
expect_locked_preflight_rejected 'symbolic staging artifact'
[[ -e "$conf_dir/.release-signing-ed25519.pem.enroll.MIXEDOK1" ]] \
  || fail 'locked preflight removed a safe stage while rejecting an unsafe peer'
rm -- "$conf_dir/.release-signing-ed25519.pem.enroll.MIXEDOK1"
expect_rejected 'symbolic staging artifact' run_enrollment "${approved_args[@]}"
rm -- "$state_dir/.sequence.floor.enroll.BADSTAGE"

printf 'hardlink-stage\n' > "$state_dir/hardlink-stage-source"
chown root:root -- "$state_dir/hardlink-stage-source"
chmod 0600 -- "$state_dir/hardlink-stage-source"
ln -- "$state_dir/hardlink-stage-source" \
  "$state_dir/.sequence.floor.enroll.HARDLINK"
expect_locked_preflight_rejected 'hard-linked staging artifact'
rm -- "$state_dir/.sequence.floor.enroll.HARDLINK" \
  "$state_dir/hardlink-stage-source"

printf 'wrong-mode-stage\n' > "$state_dir/.sequence.floor.enroll.BADMODE1"
chown root:root -- "$state_dir/.sequence.floor.enroll.BADMODE1"
chmod 0660 -- "$state_dir/.sequence.floor.enroll.BADMODE1"
expect_locked_preflight_rejected 'wrong-mode staging artifact'
rm -- "$state_dir/.sequence.floor.enroll.BADMODE1"

printf 'wrong-owner-stage\n' > "$state_dir/.sequence.floor.enroll.BADOWNER"
chown 65534:65534 -- "$state_dir/.sequence.floor.enroll.BADOWNER"
chmod 0600 -- "$state_dir/.sequence.floor.enroll.BADOWNER"
expect_locked_preflight_rejected 'wrong-owner staging artifact'
rm -- "$state_dir/.sequence.floor.enroll.BADOWNER"

truncate -s 513 "$state_dir/.sequence.floor.enroll.OVERSIZE"
chown root:root -- "$state_dir/.sequence.floor.enroll.OVERSIZE"
chmod 0600 -- "$state_dir/.sequence.floor.enroll.OVERSIZE"
expect_locked_preflight_rejected 'oversize staging artifact'
rm -- "$state_dir/.sequence.floor.enroll.OVERSIZE"

chmod 0660 -- "$floor"
expect_rejected 'unsafe existing floor metadata' run_enrollment "${approved_args[@]}"
chmod 0600 -- "$floor"

[[ "$(sha256sum "$key_target" | awk '{print $1}')" == "$key_hash" &&
   "$(sha256sum "$floor" | awk '{print $1}')" == "$floor_hash" ]] \
  || fail 'a rejected enrollment changed trusted state'

printf 'signed release enrollment contract passed\n'
