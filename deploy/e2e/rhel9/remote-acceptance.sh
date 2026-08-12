#!/usr/bin/env bash
# Blocked-only root-side smoke probe. Streamed over pinned SSH.
set -euo pipefail
umask 077
PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH

die() { printf 'rhel9-blocked-remote: ERROR: %s\n' "$*" >&2; exit 1; }
[[ $# -eq 2 ]] || die "closed argument contract mismatch"
readonly PHASE=$1
readonly EXPECT_ID=$2
[[ "$PHASE" == inspect || "$PHASE" == blocked ]] || die "phase must be inspect or blocked"
[[ "$EXPECT_ID" == almalinux || "$EXPECT_ID" == rocky ]] || die "distro must be almalinux or rocky"
[[ "${CONTROLLER_EXPECTED_MACHINE_ID:-}" =~ ^[0-9a-f]{32}$ ]] ||
 die "controller machine identity is missing or invalid"
[[ "${CONTROLLER_EXPECTED_TARGET_NONCE:-}" =~ ^[0-9a-f]{64}$ ]] ||
 die "controller target nonce is missing or invalid"
[[ "${CONTROLLER_EXPECTED_MANIFEST_SHA256:-}" =~ ^[0-9a-f]{64}$ ]] ||
 die "controller manifest identity is missing or invalid"
readonly ROOT=/root/celikpanel-e2e
readonly RELEASE=$ROOT/release
readonly MANIFEST=$RELEASE/SHA256SUMS
readonly INSTALLER=$RELEASE/install.sh
readonly SENTINEL=$ROOT/DISPOSABLE_TARGET
readonly MOUNTINFO=/proc/$$/mountinfo
readonly REFUSAL='ERROR / HATA: AlmaLinux/Rocky Linux 9 bootstrap remains preview-only: prerequisite mapping is ready, but panel and agent activation under SELinux Enforcing is not certified; no host changes were made'

readonly -a PRODUCT=(
 /opt/celikpanel /etc/celikpanel /var/lib/celikpanel /var/lib/celikpanel-imports
 /var/lib/celikpanel-agent-private /var/lib/celikpanel-release-transaction
 /run/celikpanel /run/celikpanel-release-transaction /var/backups/celikpanel
 /usr/libexec/celikpanel /etc/systemd/system/celikpanel-agent.service
 /etc/systemd/system/celikpanel-firewall-restore.service
 /etc/systemd/system/celikpanel-panel.service
 /etc/systemd/system/celikpanel-agent.service.d
 /etc/systemd/system/celikpanel-panel.service.d
 /etc/systemd/system/multi-user.target.wants/celikpanel-agent.service
 /etc/systemd/system/multi-user.target.wants/celikpanel-firewall-restore.service
 /etc/systemd/system/multi-user.target.wants/celikpanel-panel.service
 /etc/letsencrypt/renewal-hooks/deploy/celikpanel-panel-cert
)
readonly -a EVIDENCE_TOOLS=(
 /usr/bin/bash /usr/bin/cat /usr/bin/cmp /usr/bin/cut /usr/bin/env
 /usr/bin/find /usr/bin/flock /usr/bin/getent /usr/bin/grep /usr/bin/head
 /usr/bin/mktemp /usr/bin/pgrep
 /usr/bin/readlink /usr/bin/rm /usr/bin/rpm /usr/bin/sed
 /usr/bin/sha256sum /usr/bin/sort /usr/bin/stat /usr/bin/systemctl
 /usr/bin/timeout /usr/bin/tr /usr/bin/uname /usr/sbin/getenforce
)
root_chain() {
 local c=${1%/*} p q u g m x
 [[ -n "$c" ]] || c=/
 while true; do
  [[ -d "$c" && ! -L "$c" ]] || die "unsafe ancestor: $c"
  q=$(/usr/bin/readlink -e -- "$c")
  [[ "$q" == "$c" ]] || die "noncanonical ancestor: $c"
  read -r u g m < <(/usr/bin/stat -Lc '%u %g %a' -- "$c")
  x=$((8#$m))
  [[ "$u" == 0 && "$g" == 0 ]] && (( (x & 0022) == 0 )) || die "untrusted ancestor: $c"
  [[ "$c" == / ]] && break
  p=${c%/*}
  [[ -n "$p" ]] || p=/
  [[ "$p" != "$c" ]] || die "ancestor walk stalled"
  c=$p
 done
}
root_file() {
 local f=$1 executable=$2 q u g m n x
 [[ -f "$f" && ! -L "$f" ]] || die "unsafe trusted file: $f"
 q=$(/usr/bin/readlink -e -- "$f")
 [[ "$q" == "$f" ]] || die "noncanonical file: $f"
 read -r u g m n < <(/usr/bin/stat -Lc '%u %g %a %h' -- "$f")
 x=$((8#$m))
 [[ "$u" == 0 && "$g" == 0 && "$n" == 1 ]] && (( (x & 0022) == 0 )) || die "untrusted file: $f"
 [[ "$executable" == 0 || -x "$f" ]] || die "nonexecutable trusted file: $f"
 root_chain "$f"
}
verify_tools() {
 local t
 for t in "${EVIDENCE_TOOLS[@]}"; do
  root_file "$t" 1
 done
}

acquire_probe_lock() {
 root_chain "$ROOT/probe-lock"
 exec {PROBE_LOCK_FD}<"$ROOT" || die "cannot open the fixed probe lock directory"
 /usr/bin/flock -n "$PROBE_LOCK_FD" || die "another RHEL9 probe is active"
}

reject_nested_mounts() {
 local requested=$1 canonical id parent device mount_root mountpoint options rest seen=0
 [[ -e "$requested" || -L "$requested" ]] || return 0
 canonical=$(/usr/bin/readlink -e -- "$requested") || return 1
 [[ -r "$MOUNTINFO" && ! -L "$MOUNTINFO" ]] || return 1
 while IFS=' ' read -r id parent device mount_root mountpoint options rest ||
       [[ -n "$id$parent$device$mount_root$mountpoint$options$rest" ]]; do
  [[ "$id" =~ ^[0-9]+$ && "$parent" =~ ^[0-9]+$ &&
     "$device" =~ ^[0-9]+:[0-9]+$ && "$mount_root" == /* &&
     "$mountpoint" == /* && -n "$options" && " $rest " == *' - '* ]] || return 1
  ((seen+=1))
  mountpoint=${mountpoint//\\040/ }
  mountpoint=${mountpoint//\\011/$'\t'}
  mountpoint=${mountpoint//\\012/$'\n'}
  mountpoint=${mountpoint//\\134/\\}
  if [[ "$mountpoint" == "$canonical" || "$mountpoint" == "$canonical/"* ]]; then
   return 1
  fi
 done < "$MOUNTINFO"
 ((seen > 0))
}

prepare_scratch() {
 local canonical owner group mode
 SCRATCH=$(/usr/bin/mktemp -d /run/celikpanel-rhel9-blocked.XXXXXXXX) ||
  die "cannot create bounded evidence scratch directory"
 [[ "$SCRATCH" =~ ^/run/celikpanel-rhel9-blocked\.[A-Za-z0-9]{8}$ &&
    -d "$SCRATCH" && ! -L "$SCRATCH" ]] ||
  die "scratch directory has an unsafe identity"
 canonical=$(/usr/bin/readlink -e -- "$SCRATCH") || die "cannot resolve scratch directory"
 [[ "$canonical" == "$SCRATCH" ]] || die "scratch directory is not canonical"
 read -r owner group mode < <(/usr/bin/stat -Lc '%u %g %a' -- "$SCRATCH") ||
  die "cannot inspect scratch directory"
 [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] ||
  die "scratch directory metadata is unsafe"
 root_chain "$SCRATCH/probe"
}

cleanup_scratch() {
 local canonical owner group mode
 [[ "${SCRATCH:-}" =~ ^/run/celikpanel-rhel9-blocked\.[A-Za-z0-9]{8}$ &&
    -d "$SCRATCH" && ! -L "$SCRATCH" ]] || return 1
 canonical=$(/usr/bin/readlink -e -- "$SCRATCH") || return 1
 [[ "$canonical" == "$SCRATCH" ]] || return 1
 read -r owner group mode < <(/usr/bin/stat -Lc '%u %g %a' -- "$SCRATCH") || return 1
 [[ "$owner" == 0 && "$group" == 0 && "$mode" == 700 ]] || return 1
 reject_nested_mounts "$SCRATCH" || return 1
 /usr/bin/rm -rf -- "$SCRATCH"
}

checked_tree_list() {
 local root=$1 raw=$2 sorted=$3
 : >"$raw" || return 1
 /usr/bin/find "$root" -xdev -print0 >"$raw" || return 1
 LC_ALL=C /usr/bin/sort -z -- "$raw" >"$sorted" || return 1
}

paths_hash() {
 local tag=$1 root object meta context raw sorted data digest_line value
 shift
 [[ "$tag" =~ ^[a-z0-9-]+$ ]] || return 1
 raw=$SCRATCH/tree.$tag.raw
 sorted=$SCRATCH/tree.$tag.sorted
 data=$SCRATCH/tree.$tag.data
 : >"$data" || return 1
 for root in "$@"; do
  printf 'ROOT\0%s\0' "$root" >>"$data" || return 1
  if [[ ! -e "$root" && ! -L "$root" ]]; then
   printf 'ABSENT\0' >>"$data" || return 1
   continue
  fi
  reject_nested_mounts "$root" || return 1
  checked_tree_list "$root" "$raw" "$sorted" || return 1
  while IFS= read -r -d '' object; do
   meta=$(/usr/bin/stat -c '%f:%u:%g:%s:%Y:%h:%d:%i' -- "$object") || return 1
   context=$(/usr/bin/stat -c '%C' -- "$object") || return 1
   [[ -n "$context" && "$context" != '?' && "$context" != *unknown* ]] || return 1
   printf 'OBJECT\0%s\0%s\0%s\0' "$object" "$meta" "$context" >>"$data" || return 1
   if [[ -f "$object" && ! -L "$object" ]]; then
    digest_line=$(/usr/bin/sha256sum -- "$object") || return 1
    value=${digest_line%% *}
    [[ "$value" =~ ^[0-9a-f]{64}$ ]] || return 1
    printf 'FILE\0%s\0' "$value" >>"$data" || return 1
   elif [[ -L "$object" ]]; then
    value=$(/usr/bin/readlink -- "$object") || return 1
    printf 'LINK\0%s\0' "$value" >>"$data" || return 1
   elif [[ -d "$object" ]]; then
    printf 'DIR\0' >>"$data" || return 1
   else
    return 1
   fi
  done <"$sorted"
 done
 digest_line=$(/usr/bin/sha256sum -- "$data") || return 1
 PATHS_HASH=${digest_line%% *}
 [[ "$PATHS_HASH" =~ ^[0-9a-f]{64}$ ]]
}
os_value() {
 local raw=$1 dq sq value
 dq=$(printf '\042')
 sq=$(printf '\047')
 value=$(printf '%s' "$raw" | /usr/bin/tr -d "$dq$sq")
 [[ "$raw" == "$value" || "$raw" == "$dq$value$dq" || "$raw" == "$sq$value$sq" ]] || die "malformed os-release scalar"
 [[ "$value" =~ ^[a-zA-Z0-9._-]+$ ]] || die "unsafe os-release scalar"
 OS_VALUE=$value
}
verify_platform() {
 local line key raw id= version= path u g m x
 local -A seen=()
 [[ $EUID -eq 0 ]] || die "probe is not root"
 path=$(/usr/bin/readlink -e -- /etc/os-release)
 [[ "$path" == /etc/os-release || "$path" == /usr/lib/os-release ]] || die "untrusted os-release"
 read -r u g m < <(/usr/bin/stat -Lc '%u %g %a' -- /etc/os-release)
 x=$((8#$m))
 [[ "$u" == 0 && "$g" == 0 ]] && (( (x & 0022) == 0 )) || die "writable os-release"
 if IFS= read -r -d '' _ < /etc/os-release; then die "NUL in os-release"; fi
 while IFS= read -r line || [[ -n "$line" ]]; do
  case "$line" in ''|'#'*) continue ;; esac
  [[ "$line" =~ ^([A-Z][A-Z0-9_]*)=(.*)$ ]] || die "bad os-release line"
  key=${BASH_REMATCH[1]}
  raw=${BASH_REMATCH[2]}
  case "$key" in
   ID|VERSION_ID)
    [[ -z "${seen[$key]+x}" ]] || die "duplicate os identity"
    seen[$key]=1
    os_value "$raw"
    if [[ "$key" == ID ]]; then id=$OS_VALUE; else version=$OS_VALUE; fi
    ;;
  esac
 done < /etc/os-release
 [[ "$id" == "$EXPECT_ID" && "$version" =~ ^9([.][0-9]+)*$ ]] || die "unexpected distro"
 [[ "$(/usr/bin/uname -m)" == x86_64 ]] || die "not x86_64"
 [[ -f /sys/fs/selinux/enforce && ! -L /sys/fs/selinux/enforce ]] || die "SELinux unavailable"
 [[ "$(< /sys/fs/selinux/enforce)" == 1 && "$(/usr/sbin/getenforce)" == Enforcing ]] || die "SELinux not Enforcing"
 [[ -d /run/systemd/system ]] || die "systemd inactive"
 DISTRO=$id
 VERSION=$version
}
verify_target() {
 local -a lines=()
 local machine
 root_file "$SENTINEL" 0
 root_file /etc/machine-id 0
 mapfile -t lines < "$SENTINEL"
 [[ ${#lines[@]} -eq 4 && "${lines[0]}" == CELIKPANEL_RHEL9_DISPOSABLE_V1 ]] || die "bad target sentinel"
 [[ "${lines[1]}" =~ ^distro=(almalinux|rocky)$ && "${lines[1]#*=}" == "$EXPECT_ID" ]] || die "sentinel distro mismatch"
 [[ "${lines[2]}" =~ ^machine_id=([0-9a-f]{32})$ ]] || die "bad sentinel machine ID"
 [[ "${lines[3]}" =~ ^nonce=([0-9a-f]{64})$ ]] || die "bad sentinel nonce"
 machine=$(< /etc/machine-id)
 machine=${machine//-/}
 [[ "$machine" =~ ^[0-9a-f]{32}$ && "${lines[2]#*=}" == "$machine" ]] || die "machine ID mismatch"
 MACHINE=$machine
 NONCE=${lines[3]#*=}
 [[ "$MACHINE" == "$CONTROLLER_EXPECTED_MACHINE_ID" &&
    "$NONCE" == "$CONTROLLER_EXPECTED_TARGET_NONCE" ]] ||
  die "controller target binding mismatch"
}
verify_release() {
 local dev line sum rel file got object relative q u g m links object_dev x
 local raw=$SCRATCH/release.raw sorted=$SCRATCH/release.sorted
 local installer_seen=0
 local -A listed=()
 local -a manifest_lines=() objects=()
 [[ -d "$RELEASE" && ! -L "$RELEASE" ]] || die "fixed release root missing"
 root_chain "$RELEASE/x"
 reject_nested_mounts "$RELEASE" || die "release tree contains or is a mountpoint"
 dev=$(/usr/bin/stat -Lc '%d' -- "$RELEASE") || die "cannot inspect release device"
 root_file "$MANIFEST" 0
 MANIFEST_SUM=$(/usr/bin/sha256sum -- "$MANIFEST") || die "cannot hash release manifest"
 MANIFEST_SUM=${MANIFEST_SUM%% *}
 [[ "$MANIFEST_SUM" =~ ^[0-9a-f]{64}$ &&
    "$MANIFEST_SUM" == "$CONTROLLER_EXPECTED_MANIFEST_SHA256" ]] ||
  die "controller manifest binding mismatch"
 mapfile -t manifest_lines <"$MANIFEST" || die "cannot read release manifest"
 for line in "${manifest_lines[@]}"; do
  [[ "$line" =~ ^([0-9a-f]{64})\ \ ([A-Za-z0-9._/@+-]+)$ ]] || die "bad manifest line"
  sum=${BASH_REMATCH[1]}
  rel=${BASH_REMATCH[2]}
  [[ "$rel" != /* && "$rel" != . && "$rel" != .. && "$rel" != SHA256SUMS && "$rel" != *//* ]] || die "unsafe manifest path"
  [[ "/$rel/" != *'/./'* && "/$rel/" != *'/../'* && -z "${listed[$rel]+x}" ]] || die "manifest traversal or duplicate"
  listed[$rel]=$sum
  file=$RELEASE/$rel
  root_file "$file" 0
  [[ "$(/usr/bin/stat -Lc '%d' -- "$file")" == "$dev" ]] || die "release mount mismatch"
  got=$(/usr/bin/sha256sum -- "$file") || die "cannot hash release file"
  got=${got%% *}
  [[ "$got" == "$sum" ]] || die "release digest mismatch"
  if [[ "$rel" == install.sh ]]; then installer_seen=1; INSTALLER_SUM=$got; fi
 done
 [[ "$installer_seen" == 1 ]] || die "install.sh absent from manifest"
 root_file "$INSTALLER" 1
 checked_tree_list "$RELEASE" "$raw" "$sorted" || die "cannot enumerate release tree"
 mapfile -d '' -t objects <"$sorted" || die "cannot read release tree"
 for object in "${objects[@]}"; do
  q=$(/usr/bin/readlink -e -- "$object") || die "cannot resolve release object"
  [[ "$q" == "$object" ]] || die "noncanonical release object"
  read -r u g m links object_dev < <(/usr/bin/stat -Lc '%u %g %a %h %d' -- "$object") ||
   die "cannot inspect release object"
  x=$((8#$m))
  [[ "$u" == 0 && "$g" == 0 && "$object_dev" == "$dev" ]] && (( (x & 0022) == 0 )) || die "untrusted release object"
  if [[ -d "$object" ]]; then :
  elif [[ -f "$object" ]]; then
   [[ "$links" == 1 ]] || die "release file must have one hard link"
   if [[ "$object" != "$MANIFEST" ]]; then
    relative=${object#"$RELEASE/"}
    [[ "$relative" != "$object" && "$relative" =~ ^[A-Za-z0-9._/@+-]+$ &&
       "$relative" != /* && "$relative" != . && "$relative" != .. &&
       "$relative" != *//* && "/$relative/" != *'/./'* &&
       "/$relative/" != *'/../'* ]] || die "unsafe unmanifested release file name"
    [[ -n "${listed[$relative]+x}" ]] || die "unmanifested release file"
   fi
  else die "unsafe release object"
  fi
 done
}
pristine() {
 local item account_status process_status
 for item in "${PRODUCT[@]}"; do
  [[ ! -e "$item" && ! -L "$item" ]] || die "product path exists: $item"
 done
 if /usr/bin/timeout --signal=TERM --kill-after=2s 10s /usr/bin/getent passwd celikpanel >/dev/null; then
  die "celikpanel user exists"
 else
  account_status=$?
  [[ "$account_status" == 2 ]] || die "user account inventory failed"
 fi
 if /usr/bin/timeout --signal=TERM --kill-after=2s 10s /usr/bin/getent group celikpanel >/dev/null; then
  die "celikpanel group exists"
 else
  account_status=$?
  [[ "$account_status" == 2 ]] || die "group account inventory failed"
 fi
 for item in dnf dnf-3 dnf5 yum microdnf rpm rpmdb packagekitd dnfdaemon-serve celikpanel-agent celikpanel-panel celikpanel-agen celikpanel-pane
 do
  if /usr/bin/pgrep -x "$item" >/dev/null 2>&1; then
   die "live checkpoint process: $item"
  else
   process_status=$?
   [[ "$process_status" == 1 ]] || die "process inventory failed: $item"
  fi
 done
}
append_paths_snapshot() {
 local state=$1 tag=$2 label=$3
 shift 3
 paths_hash "$tag" "$@" || return 1
 printf '%s=%s\n' "$label" "$PATHS_HASH" >>"$state"
}

state_hash() {
 local tag=$1 state=$SCRATCH/state.$tag raw=$SCRATCH/state.$tag.raw
 local sorted=$SCRATCH/state.$tag.sorted normalized=$SCRATCH/state.$tag.normalized
 local boot enforce reported digest_line
 [[ "$tag" =~ ^(before|after)$ ]] || return 1
 : >"$state" || return 1
 IFS= read -r boot < /proc/sys/kernel/random/boot_id || return 1
 boot=${boot//-/}
 [[ "$boot" =~ ^[0-9a-f]{32}$ ]] || return 1
 printf 'boot=%s\n' "$boot" >>"$state" || return 1

 append_paths_snapshot "$state" "$tag-product" product "${PRODUCT[@]}" || return 1
 append_paths_snapshot "$state" "$tag-staging" staging "$ROOT" /etc/machine-id /etc/os-release /usr/lib/os-release /sys/fs/selinux/enforce "$MOUNTINFO" "${EVIDENCE_TOOLS[@]}" /usr/sbin/nft /usr/sbin/iptables-save || return 1
 append_paths_snapshot "$state" "$tag-accounts" accounts /etc/passwd /etc/group /etc/shadow /etc/gshadow /etc/subuid /etc/subgid || return 1

 /usr/bin/rpm -qa --qf '%{NAME}\t%{EPOCHNUM}\t%{VERSION}\t%{RELEASE}\t%{ARCH}\n' >"$raw" || return 1
 LC_ALL=C /usr/bin/sort -- "$raw" >"$sorted" || return 1
 printf 'rpm-inventory\n' >>"$state" || return 1
 /usr/bin/cat -- "$sorted" >>"$state" || return 1
 append_paths_snapshot "$state" "$tag-rpmdb" rpmdb /usr/lib/sysimage/rpm /var/lib/rpm || return 1
 append_paths_snapshot "$state" "$tag-dnf" dnf /var/lib/dnf /var/cache/dnf /var/log/dnf.log /var/log/dnf.librepo.log /var/log/dnf.rpm.log || return 1

 append_paths_snapshot "$state" "$tag-systemd" systemd /etc/systemd/system /usr/lib/systemd/system /usr/local/lib/systemd/system /run/systemd/system || return 1
 /usr/bin/systemctl list-unit-files --all --no-legend --no-pager >"$raw" || return 1
 LC_ALL=C /usr/bin/sort -- "$raw" >"$sorted" || return 1
 printf 'systemd-unit-files\n' >>"$state" || return 1
 /usr/bin/cat -- "$sorted" >>"$state" || return 1
 /usr/bin/systemctl list-units --all --no-legend --no-pager --plain >"$raw" || return 1
 LC_ALL=C /usr/bin/sort -- "$raw" >"$sorted" || return 1
 printf 'systemd-live-units\n' >>"$state" || return 1
 /usr/bin/cat -- "$sorted" >>"$state" || return 1

 append_paths_snapshot "$state" "$tag-firewall" firewall /etc/firewalld /etc/nftables /etc/sysconfig/nftables.conf /etc/sysconfig/iptables /etc/sysconfig/ip6tables || return 1
 if [[ -e /usr/sbin/nft || -L /usr/sbin/nft ]]; then
  (root_file /usr/sbin/nft 1) || return 1
  /usr/sbin/nft -nn list ruleset >"$raw" || return 1
  /usr/bin/sed -E 's/counter packets [0-9]+ bytes [0-9]+/counter packets N bytes N/g' "$raw" >"$normalized" || return 1
  printf 'nft-rules\n' >>"$state" || return 1
  /usr/bin/cat -- "$normalized" >>"$state" || return 1
 else
  printf 'nft=absent\n' >>"$state" || return 1
 fi
 if [[ -e /usr/sbin/iptables-save || -L /usr/sbin/iptables-save ]]; then
  (root_file /usr/sbin/iptables-save 1) || return 1
  /usr/sbin/iptables-save >"$raw" || return 1
  /usr/bin/sed -E -e 's/^(# Generated by iptables-save .+ on) .+$/\1 TIME/' -e 's/^(# Completed on) .+$/\1 TIME/' "$raw" >"$normalized" || return 1
  printf 'iptables-rules\n' >>"$state" || return 1
  /usr/bin/cat -- "$normalized" >>"$state" || return 1
 else
  printf 'iptables-save=absent\n' >>"$state" || return 1
 fi

 IFS= read -r enforce < /sys/fs/selinux/enforce || return 1
 [[ "$enforce" == 1 ]] || return 1
 printf 'enforce=%s\n' "$enforce" >>"$state" || return 1
 /usr/sbin/getenforce >"$raw" || return 1
 IFS= read -r reported <"$raw" || return 1
 [[ "$reported" == Enforcing ]] || return 1
 /usr/bin/cat -- "$raw" >>"$state" || return 1
 append_paths_snapshot "$state" "$tag-selinux" selinux /etc/selinux /var/lib/selinux /sys/fs/selinux/policy || return 1

 digest_line=$(/usr/bin/sha256sum -- "$state") || return 1
 STATE_HASH=${digest_line%% *}
 [[ "$STATE_HASH" =~ ^[0-9a-f]{64}$ ]]
}
identity_results() {
 printf 'RESULT observation=%s\n' "$PHASE"
 printf 'RESULT distro=%s\n' "$DISTRO"
 printf 'RESULT version=%s\n' "$VERSION"
 printf 'RESULT architecture=x86_64\n'
 printf 'RESULT machine-id=%s\n' "$MACHINE"
 printf 'RESULT target-nonce=%s\n' "$NONCE"
 printf 'RESULT manifest-sha256=%s\n' "$MANIFEST_SUM"
 printf 'RESULT installer-sha256=%s\n' "$INSTALLER_SUM"
}
verify_tools
verify_platform
acquire_probe_lock
prepare_scratch
finish_probe() {
 local status=$?
 trap - EXIT
 cleanup_scratch || status=1
 exit "$status"
}
trap finish_probe EXIT
verify_target
verify_release
if [[ "$PHASE" == inspect ]]; then
 identity_results
 exit 0
fi
pristine
STATE_HASH=
state_hash before || die "cannot collect the pre-install durable state"
before=$STATE_HASH
raw=$SCRATCH/installer.raw
clean=$SCRATCH/installer.clean
control=$SCRATCH/installer.control
expected=$SCRATCH/installer.expected
set +e
/usr/bin/timeout --signal=TERM --kill-after=10s 120s \
 /usr/bin/env -i PATH=/usr/sbin:/usr/bin:/sbin:/bin LC_ALL=C LANG=C HOME=/root \
 /usr/bin/bash "$INSTALLER" 2>&1 | /usr/bin/head -c 65537 >"$raw"
pipe_status=("${PIPESTATUS[@]}")
set -e

# Collect every postcondition before interpreting status or output. A success,
# timeout, or different partial failure is precisely when evidence matters.
post_state_rc=0
STATE_HASH=
state_hash after || post_state_rc=$?
after=${STATE_HASH:-}
post_tools_rc=0
(verify_tools) || post_tools_rc=$?
post_platform_rc=0
(verify_platform) || post_platform_rc=$?
post_target_rc=0
(verify_target) || post_target_rc=$?
post_release_rc=0
(verify_release) || post_release_rc=$?
post_pristine_rc=0
(pristine) || post_pristine_rc=$?

[[ "$post_state_rc" == 0 && "$after" =~ ^[0-9a-f]{64}$ ]] || die "post-install durable state collection failed"
[[ "$post_tools_rc" == 0 && "$post_platform_rc" == 0 && "$post_target_rc" == 0 && "$post_release_rc" == 0 && "$post_pristine_rc" == 0 ]] || die "post-install trust, identity, release, or pristine verification failed"
[[ "$after" == "$before" ]] || die "enumerated durable state changed"

[[ ${#pipe_status[@]} == 2 && "${pipe_status[1]}" == 0 ]] || die "bounded installer output capture failed"
bytes=$(/usr/bin/stat -Lc '%s' -- "$raw") || die "cannot inspect installer output"
[[ "$bytes" =~ ^[0-9]+$ && "$bytes" -le 65536 ]] || die "installer output exceeded the bounded capture"
case "${pipe_status[0]}" in
 124|125|126|127|137) die "installer timed out or timeout infrastructure failed" ;;
esac
[[ "${pipe_status[0]}" -ne 0 ]] || die "preview installer succeeded"
/usr/bin/sed -E $'s/\x1B\\[[0-9;]*m//g' "$raw" >"$clean" || die "cannot normalize installer refusal"
LC_ALL=C /usr/bin/tr -d '\012\040-\176' <"$clean" >"$control" || die "cannot validate installer output characters"
[[ ! -s "$control" ]] || die "installer output contains unsafe control characters"
printf '%s\n' "$REFUSAL" >"$expected" || die "cannot prepare expected installer refusal"
/usr/bin/cmp -s -- "$clean" "$expected" || die "exact preview refusal is missing"
output_digest=$(/usr/bin/sha256sum -- "$raw") || die "cannot hash installer output"
output_digest=${output_digest%% *}
[[ "$output_digest" =~ ^[0-9a-f]{64}$ ]] || die "installer output digest is invalid"
identity_results
printf 'RESULT installer-refusal=RHEL9_PREVIEW_BLOCKED\n'
printf 'RESULT installer-output-bytes=%s\n' "$bytes"
printf 'RESULT installer-output-sha256=%s\n' "$output_digest"
printf 'RESULT before-state-sha256=%s\n' "$before"
printf 'RESULT after-state-sha256=%s\n' "$after"
printf 'RESULT durable-checkpoints=unchanged\n'
