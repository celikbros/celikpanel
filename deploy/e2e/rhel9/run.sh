#!/usr/bin/env bash
# Blocked-only AlmaLinux/Rocky Linux 9 smoke controller. It implements no
# certification, successful-install, reboot, update, rollback, or hook flow.
set -euo pipefail
umask 077
PATH=/usr/bin:/bin
LC_ALL=C
LANG=C
export PATH LC_ALL LANG

readonly HERE="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd -P)"
readonly REMOTE="$HERE/remote-acceptance.sh"
readonly RELEASE_ROOT=/root/celikpanel-e2e/release
# These four strings are the entire remote command grammar. No environment
# value is interpolated into them.
readonly CMD_INSPECT_ALMA='/usr/bin/sudo -n /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C LANG=C HOME=/root /usr/bin/bash -s -- inspect almalinux'
readonly CMD_INSPECT_ROCKY='/usr/bin/sudo -n /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C LANG=C HOME=/root /usr/bin/bash -s -- inspect rocky'
readonly CMD_BLOCKED_ALMA='/usr/bin/sudo -n /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C LANG=C HOME=/root /usr/bin/bash -s -- blocked almalinux'
readonly CMD_BLOCKED_ROCKY='/usr/bin/sudo -n /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C LANG=C HOME=/root /usr/bin/bash -s -- blocked rocky'

die() { printf 'rhel9-blocked-smoke: ERROR: %s\n' "$*" >&2; exit 1; }

local_chain_is_trusted() {
    local current=${1%/*} parent canonical uid mode permissions
    [[ -n "$current" ]] || current=/
    while true; do
        [[ -d "$current" && ! -L "$current" ]] || return 1
        canonical=$(/usr/bin/readlink -e -- "$current") || return 1
        [[ "$canonical" == "$current" ]] || return 1
        read -r uid mode < <(/usr/bin/stat -Lc '%u %a' -- "$current") || return 1
        [[ "$uid" == 0 || "$uid" == "$EUID" ]] || return 1
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) || return 1
        [[ "$current" == / ]] && return 0
        parent=${current%/*}; [[ -n "$parent" ]] || parent=/
        [[ "$parent" != "$current" ]] || return 1
        current=$parent
    done
}

validate_local_file() {
    local path=$1 role=$2 owner_rule=$3 canonical uid mode links permissions
    [[ "$path" == /* && -f "$path" && ! -L "$path" ]] || die "$role is not a safe regular file"
    canonical=$(/usr/bin/readlink -e -- "$path") || die "cannot resolve $role"
    [[ "$canonical" == "$path" ]] || die "$role is not canonical"
    read -r uid mode links < <(/usr/bin/stat -Lc '%u %a %h' -- "$path") || die "cannot inspect $role"
    case "$owner_rule" in
        self) [[ "$uid" == "$EUID" ]] || die "$role is not owned by the invoking user" ;;
        root-or-self) [[ "$uid" == 0 || "$uid" == "$EUID" ]] || die "$role has an untrusted owner" ;;
        *) die "internal owner rule mismatch" ;;
    esac
    [[ "$links" == 1 ]] || die "$role must have one hard link"
    permissions=$((8#$mode)); (( (permissions & 0022) == 0 )) || die "$role is group/other writable"
    [[ "$role" != 'identity file' || "$mode" == 400 || "$mode" == 600 ]] ||
        die "identity file mode must be 0400 or 0600"
    local_chain_is_trusted "$path" || die "$role has an untrusted ancestor chain"
}

reject_legacy_authority() {
    local name
    for name in CELIKPANEL_E2E_REMOTE_INSTALLER CELIKPANEL_E2E_REMOTE_RELEASE_ROOT \
        CELIKPANEL_E2E_REMOTE_INSTALL_HOOK CELIKPANEL_E2E_REMOTE_UPDATE_HOOK \
        CELIKPANEL_E2E_REMOTE_ROLLBACK_HOOK CELIKPANEL_E2E_REMOTE_DNF_LOCK_HOOK \
        CELIKPANEL_E2E_REMOTE_DEFAULT_DENY_HOOK CELIKPANEL_E2E_EXPECT_AGENT_SELINUX_TYPE \
        CELIKPANEL_E2E_EXPECT_PANEL_SELINUX_TYPE CELIKPANEL_E2E_CERTIFY_RHEL9 \
        CELIKPANEL_E2E_ALLOW_REBOOT
    do
        [[ ! -v $name ]] || die "$name is unsupported and must be unset"
    done
}

readonly MODE="${CELIKPANEL_E2E_MODE:-blocked}"
readonly TARGET="${CELIKPANEL_E2E_SSH_TARGET:-}"
readonly EXPECT_ID="${CELIKPANEL_E2E_EXPECT_ID:-}"
readonly KNOWN_HOSTS="${CELIKPANEL_E2E_KNOWN_HOSTS:-}"
readonly HOST_ALIAS="${CELIKPANEL_E2E_HOST_KEY_ALIAS:-}"
readonly HOST_FP="${CELIKPANEL_E2E_EXPECT_HOST_KEY_SHA256:-}"
readonly MANIFEST_FP="${CELIKPANEL_E2E_EXPECT_MANIFEST_SHA256:-}"
readonly MACHINE_ID="${CELIKPANEL_E2E_EXPECT_MACHINE_ID:-}"
readonly TARGET_NONCE="${CELIKPANEL_E2E_EXPECT_TARGET_NONCE:-}"
readonly IDENTITY="${CELIKPANEL_E2E_IDENTITY_FILE:-}"
readonly PORT="${CELIKPANEL_E2E_SSH_PORT:-22}"
readonly DRY_RUN="${CELIKPANEL_E2E_DRY_RUN:-1}"
readonly REMOTE_APPROVAL="${CELIKPANEL_E2E_ALLOW_REMOTE_BLOCKED_NONCE:-}"

reject_legacy_authority
[[ "$MODE" == blocked ]] || die "only blocked mode exists; certification and reboot are not implemented"
[[ "$DRY_RUN" == 0 || "$DRY_RUN" == 1 ]] || die "dry-run must be 0 or 1"
[[ "$TARGET" =~ ^([a-z_][a-z0-9_-]*@)?([A-Za-z0-9][A-Za-z0-9.-]*|\[[0-9A-Fa-f:]+\])$ ]] ||
    die "SSH target contains unsupported characters"
[[ "$EXPECT_ID" == almalinux || "$EXPECT_ID" == rocky ]] || die "expected distro must be almalinux or rocky"
[[ "$PORT" =~ ^[0-9]{1,5}$ ]] && ((10#$PORT >= 1 && 10#$PORT <= 65535)) || die "SSH port is invalid"
[[ "$HOST_ALIAS" =~ ^[A-Za-z0-9][A-Za-z0-9.-]{0,252}$ ]] || die "host-key alias is invalid"
[[ "$HOST_FP" =~ ^SHA256:[A-Za-z0-9+/]{43}$ ]] || die "host-key fingerprint is invalid"
[[ "$MANIFEST_FP" =~ ^[0-9a-f]{64}$ ]] || die "manifest digest is invalid"
[[ "$MACHINE_ID" =~ ^[0-9a-f]{32}$ ]] || die "machine ID is invalid"
[[ "$TARGET_NONCE" =~ ^[0-9a-f]{64}$ ]] || die "target nonce is invalid"
if [[ "$DRY_RUN" == 0 && "$REMOTE_APPROVAL" != "$TARGET_NONCE" ]]; then
    die "remote blocked execution requires the exact target nonce as explicit approval"
fi
[[ "$KNOWN_HOSTS" =~ ^/[A-Za-z0-9._@+/-]+$ ]] ||
    die "known_hosts must use a token-free absolute SSH path"
[[ -z "$IDENTITY" || "$IDENTITY" =~ ^/[A-Za-z0-9._@+/-]+$ ]] ||
    die "identity file must use a token-free absolute SSH path"
[[ -f "$REMOTE" && ! -L "$REMOTE" ]] || die "remote runner is missing or symbolic"
validate_local_file "$REMOTE" 'remote runner' root-or-self
validate_local_file "$KNOWN_HOSTS" known_hosts root-or-self
[[ -z "$IDENTITY" ]] || validate_local_file "$IDENTITY" 'identity file' self
validate_local_file /usr/bin/head 'local output limiter' root-or-self
validate_local_file /usr/bin/timeout 'local timeout' root-or-self

mapfile -t host_lines < "$KNOWN_HOSTS"
[[ ${#host_lines[@]} -eq 1 ]] || die "known_hosts must contain one dedicated line"
read -r khost ktype kblob kextra <<< "${host_lines[0]}"
[[ "${host_lines[0]}" == "$HOST_ALIAS ssh-ed25519 $kblob" &&
   "$khost" == "$HOST_ALIAS" && "$ktype" == ssh-ed25519 && -z "$kextra" &&
   "$kblob" =~ ^AAAAC3NzaC1lZDI1NTE5AAAAI[A-Za-z0-9+/]{43}$ ]] ||
    die "known_hosts must pin only the exact alias and one structurally valid ed25519 key"
base64_bin=
openssl_bin=
for candidate in /usr/bin/base64 /usr/sbin/base64; do
 [[ -n "$base64_bin" || ! -f "$candidate" || ! -x "$candidate" || -L "$candidate" ]] || base64_bin=$candidate
done
for candidate in /usr/bin/openssl /usr/sbin/openssl; do
 [[ -n "$openssl_bin" || ! -f "$candidate" || ! -x "$candidate" || -L "$candidate" ]] || openssl_bin=$candidate
done
[[ -n "$base64_bin" && -n "$openssl_bin" ]] || die "fixed base64/openssl fingerprint tools are unavailable"
actual_fp=$(printf '%s' "$kblob" | "$base64_bin" -d | "$openssl_bin" dgst -sha256 -binary | "$base64_bin")
actual_fp=${actual_fp//$'\n'/}
actual_fp=${actual_fp//=}
[[ "SHA256:$actual_fp" == "$HOST_FP" ]] || die "host-key fingerprint mismatch"

ssh_args=(-F /dev/null -o BatchMode=yes -o CheckHostIP=yes -o ClearAllForwardings=yes
    -o ConnectTimeout=10 -o ConnectionAttempts=1 -o GSSAPIAuthentication=no
    -o GlobalKnownHostsFile=/dev/null
    -o "HostKeyAlias=$HOST_ALIAS" -o HostKeyAlgorithms=ssh-ed25519
    -o HostbasedAuthentication=no -o IdentitiesOnly=yes -o KbdInteractiveAuthentication=no
    -o LogLevel=ERROR -o PasswordAuthentication=no -o PermitLocalCommand=no
    -o PreferredAuthentications=publickey -o ProxyCommand=none -o RequestTTY=no
    -o ServerAliveCountMax=3 -o ServerAliveInterval=10
    -o StrictHostKeyChecking=yes -o UpdateHostKeys=no
    -o "UserKnownHostsFile=$KNOWN_HOSTS" -o VerifyHostKeyDNS=no -p "$PORT")
[[ -z "$IDENTITY" ]] || ssh_args+=(-i "$IDENTITY")

remote_command() {
    case "$1:$EXPECT_ID" in
        inspect:almalinux) printf '%s\n' "$CMD_INSPECT_ALMA" ;;
        inspect:rocky) printf '%s\n' "$CMD_INSPECT_ROCKY" ;;
        blocked:almalinux) printf '%s\n' "$CMD_BLOCKED_ALMA" ;;
        blocked:rocky) printf '%s\n' "$CMD_BLOCKED_ROCKY" ;;
        *) die "closed remote command selection failed" ;;
    esac
}
remote_call() {
    local command
    command=$(remote_command "$1")
    {
        printf 'readonly CONTROLLER_EXPECTED_MACHINE_ID=%s\n' "$MACHINE_ID"
        printf 'readonly CONTROLLER_EXPECTED_TARGET_NONCE=%s\n' "$TARGET_NONCE"
        printf 'readonly CONTROLLER_EXPECTED_MANIFEST_SHA256=%s\n' "$MANIFEST_FP"
        /usr/bin/cat -- "$REMOTE"
    } | /usr/bin/timeout --signal=TERM --kill-after=10s 300s /usr/bin/ssh "${ssh_args[@]}" "$TARGET" "$command" 2>&1 | /usr/bin/head -c 4097
}
validate_remote_output() {
    local output=$1 phase=$2 expected line
    local -a lines=()
    [[ "${#output}" -le 4096 ]] || die "remote output exceeded the local bound"
    mapfile -t lines <<< "$output"
    case "$phase" in
        inspect) expected=8 ;;
        blocked) expected=14 ;;
        *) die "internal remote-output phase mismatch" ;;
    esac
    [[ "${#lines[@]}" -eq "$expected" ]] || die "remote output has an unexpected line count"
    for line in "${lines[@]}"; do
        [[ "$line" =~ ^RESULT\ [a-z][a-z0-9-]*=[A-Za-z0-9._/-]+$ ]] ||
            die "remote output violates the closed result grammar"
    done
}
result() {
    local matches count
    matches=$(printf '%s\n' "$1" | /usr/bin/grep -E "^RESULT $2=$3$" || true)
    count=$(printf '%s\n' "$matches" | /usr/bin/grep -Ec . || true)
    [[ "$count" == 1 ]] || die "remote output lacks one exact $2 result"
    printf '%s\n' "${matches#*=}"
}
verify_identity() {
    [[ "$(result "$1" observation '(inspect|blocked)')" == "$2" ]] || die "observation mismatch"
    [[ "$(result "$1" distro '(almalinux|rocky)')" == "$EXPECT_ID" ]] || die "distro mismatch"
    result "$1" version '9([.][0-9]+)*' >/dev/null
    [[ "$(result "$1" architecture 'x86_64')" == x86_64 ]] || die "architecture mismatch"
    [[ "$(result "$1" machine-id '[0-9a-f]{32}')" == "$MACHINE_ID" ]] || die "machine ID mismatch"
    [[ "$(result "$1" target-nonce '[0-9a-f]{64}')" == "$TARGET_NONCE" ]] || die "target nonce mismatch"
    [[ "$(result "$1" manifest-sha256 '[0-9a-f]{64}')" == "$MANIFEST_FP" ]] || die "manifest mismatch"
    result "$1" installer-sha256 '[0-9a-f]{64}' >/dev/null
}

if [[ "$DRY_RUN" == 1 ]]; then
    printf 'rhel9-blocked-smoke dry-run: phases=inspect,blocked target=%s distro=%s release-root=%s transport=static-command\n' \
        "$TARGET" "$EXPECT_ID" "$RELEASE_ROOT"
    printf 'rhel9-blocked-smoke dry-run: certification=not-implemented\n'
    exit 0
fi

if ! inspection=$(remote_call inspect); then
    die "inspect transport failed"
fi
validate_remote_output "$inspection" inspect
verify_identity "$inspection" inspect
if ! blocked=$(remote_call blocked); then
    die "blocked probe transport failed"
fi
validate_remote_output "$blocked" blocked
verify_identity "$blocked" blocked
[[ "$(result "$blocked" installer-refusal '[A-Z0-9_]+')" == RHEL9_PREVIEW_BLOCKED ]] ||
    die "exact preview refusal is missing"
before=$(result "$blocked" before-state-sha256 '[0-9a-f]{64}')
after=$(result "$blocked" after-state-sha256 '[0-9a-f]{64}')
[[ "$before" == "$after" ]] || die "enumerated durable state changed"
[[ "$(result "$blocked" durable-checkpoints 'unchanged')" == unchanged ]] || die "durable conclusion missing"
printf 'rhel9-blocked-smoke: enumerated durable checkpoints unchanged on %s (sha256=%s); no certification claim\n' "$TARGET" "$after"
