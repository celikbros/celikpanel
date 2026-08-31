#!/usr/bin/env bash
set -euo pipefail

# Guest-side half of the S-1 DNS kill-matrix bootstrap. This script is copied
# into a fresh fixture VM by guest_bootstrap.py and is never sourced.

die() {
    printf 'guest bootstrap: %s\n' "$*" >&2
    exit 1
}

[[ ${EUID:-$(id -u)} -eq 0 ]] || die "must run as root"

STATE_DIR=/var/lib/celikpanel-agent-private
FIXTURE_DIR=/var/lib/celikpanel-dns-kill-matrix
MUTATION_LOCK=/run/celikpanel/service-mutation.lock
AGENT_SOCKET=/run/celikpanel/agent.sock
COORDINATOR_STOP_PROOF=$FIXTURE_DIR/coordinator-stop-proof.json
TOKEN_FILE=/etc/celikpanel/agent.token
SCENARIO_FILE=$FIXTURE_DIR/scenario.json
SOURCE_SETUP_FILE=$FIXTURE_DIR/source-setup-pdns.json
SOURCE_PROOF_FILE=$FIXTURE_DIR/source-proof.json
SOURCE_SETUP_IDENTITY=$FIXTURE_DIR/source-setup-trigger-identity.json
SOURCE_PREINSTALL_PROOF=$FIXTURE_DIR/source-preinstall-pdns.json
SOURCE_ADOPTION_PROOF=$FIXTURE_DIR/source-adoption-pdns.json
EXTERNAL_PDNS_PREIMAGE_PROOF=$FIXTURE_DIR/source-external-pdns-preimage.json
SOURCE_NORMALIZATION_IDENTITY=$FIXTURE_DIR/source-normalization-pdns-identity.json
MEASURED_IDENTITY_DIR=$FIXTURE_DIR/measured
MEASURED_IDENTITY=$MEASURED_IDENTITY_DIR/trigger-identity.json

readonly -a SOURCE_FIXTURE_POLICIES=(
    driver-specific
    uninitialized-permitted-noncritical
    managed-pdns-required
)
readonly -a EARLY_UNINITIALIZED_PHASES=(
    pre-intent
    intent
    target-staged
)
readonly -a CRITICAL_MANAGED_PDNS_PHASES=(
    source-stopped
    target-started
)

array_contains() {
    local needle=$1
    shift
    local candidate
    for candidate in "$@"; do
        [[ $candidate == "$needle" ]] && return 0
    done
    return 1
}

require_simple_value() {
    local label=$1 value=$2 pattern=$3
    [[ $value =~ $pattern ]] || die "$label is not canonical: $value"
}

require_regular() {
    local path=$1
    [[ -f $path && ! -L $path ]] || die "expected a regular non-symlink file: $path"
    [[ $(stat -Lc '%h' -- "$path") == 1 ]] || die "expected a single-link file: $path"
}

inactive_unit_evidence() {
    local unit=$1 active_state sub_state main_pid control_pid
    active_state=$(systemctl show "$unit" --property=ActiveState --value) \
        || die "cannot inspect $unit ActiveState"
    sub_state=$(systemctl show "$unit" --property=SubState --value) \
        || die "cannot inspect $unit SubState"
    main_pid=$(systemctl show "$unit" --property=MainPID --value) \
        || die "cannot inspect $unit MainPID"
    control_pid=$(systemctl show "$unit" --property=ControlPID --value) \
        || die "cannot inspect $unit ControlPID"
    [[ $active_state == inactive ]] \
        || die "$unit is not inactive after stop (ActiveState=$active_state)"
    [[ $sub_state =~ ^[a-z-]+$ ]] \
        || die "$unit returned a non-canonical SubState"
    [[ $main_pid == 0 && $control_pid == 0 ]] \
        || die "$unit retained a service process after stop (MainPID=$main_pid ControlPID=$control_pid)"
    printf '%s:%s:%s:%s\n' "$active_state" "$sub_state" "$main_pid" "$control_pid"
}

remove_verified_stale_agent_socket() {
    local agent_evidence=$1 panel_evidence=$2 celikpanel_gid temporary cleanup_status
    [[ $AGENT_SOCKET == /run/celikpanel/agent.sock ]] \
        || die "refuse cleanup outside the exact production agent socket"
    [[ ! -e $COORDINATOR_STOP_PROOF && ! -L $COORDINATOR_STOP_PROOF ]] \
        || die "coordinator stop proof already exists"
    celikpanel_gid=$(getent group celikpanel | cut -d: -f3)
    [[ $celikpanel_gid =~ ^[0-9]+$ ]] \
        || die "cannot resolve the exact celikpanel group id for socket cleanup"
    temporary=$(mktemp "$FIXTURE_DIR/.coordinator-stop-proof.XXXXXXXX")
    chmod 0600 "$temporary"

    if STALE_AGENT_SOCKET_PATH=$AGENT_SOCKET \
       STALE_AGENT_SOCKET_PROC_NET_UNIX=/proc/net/unix \
       STALE_AGENT_SOCKET_EXPECTED_UID=0 \
       STALE_AGENT_SOCKET_EXPECTED_GID=$celikpanel_gid \
       STALE_AGENT_SOCKET_EXPECTED_GROUP=celikpanel \
       STALE_AGENT_SOCKET_AGENT_UNIT=$agent_evidence \
       STALE_AGENT_SOCKET_PANEL_UNIT=$panel_evidence \
       python3 - "$temporary" <<'PY'
# STALE_AGENT_SOCKET_CLEANUP
import errno
import json
import os
from pathlib import Path
import stat
import sys

socket_path = os.environ["STALE_AGENT_SOCKET_PATH"]
proc_net_unix = Path(os.environ["STALE_AGENT_SOCKET_PROC_NET_UNIX"])
proof_path = Path(sys.argv[1])
expected_uid = int(os.environ["STALE_AGENT_SOCKET_EXPECTED_UID"])
expected_gid = int(os.environ["STALE_AGENT_SOCKET_EXPECTED_GID"])
expected_group = os.environ["STALE_AGENT_SOCKET_EXPECTED_GROUP"]


def parse_unit(value):
    fields = value.split(":")
    if len(fields) != 4:
        raise ValueError("unit evidence is malformed")
    active_state, sub_state, main_pid, control_pid = fields
    if active_state != "inactive" or not main_pid.isdecimal() or not control_pid.isdecimal():
        raise ValueError("unit evidence did not prove an inactive coordinator")
    if int(main_pid) != 0 or int(control_pid) != 0:
        raise ValueError("unit evidence retained a coordinator process")
    return {
        "active_state": active_state,
        "sub_state": sub_state,
        "main_pid": int(main_pid),
        "control_pid": int(control_pid),
    }


proof = {
    "schema": "celikpanel/dns-kill-coordinator-stop-proof/v1",
    "socket_path": socket_path,
    "expected_socket": {
        "uid": expected_uid,
        "gid": expected_gid,
        "owner": "root",
        "group": expected_group,
        "mode": "0660",
        "type": "socket",
        "link_count": 1,
    },
    "units": {
        "celikpanel-agent.service": parse_unit(
            os.environ["STALE_AGENT_SOCKET_AGENT_UNIT"]
        ),
        "celikpanel-panel.service": parse_unit(
            os.environ["STALE_AGENT_SOCKET_PANEL_UNIT"]
        ),
    },
}


def socket_description(value):
    file_type = "socket" if stat.S_ISSOCK(value.st_mode) else "other"
    return {
        "device": value.st_dev,
        "inode": value.st_ino,
        "uid": value.st_uid,
        "gid": value.st_gid,
        "mode": f"{stat.S_IMODE(value.st_mode):04o}",
        "type": file_type,
        "link_count": value.st_nlink,
    }


def active_path_entries():
    try:
        lines = proc_net_unix.read_text(encoding="utf-8").splitlines()
    except OSError as exc:
        finish("refused", f"cannot inspect active Unix sockets: {exc}", 1)
    entries = []
    for line in lines[1:]:
        fields = line.split(maxsplit=7)
        if len(fields) == 8 and fields[7] == socket_path:
            entries.append({
                "flags": fields[3],
                "type": fields[4],
                "state": fields[5],
                "kernel_inode": fields[6],
                "path": fields[7],
            })
    return entries


def finish(decision, reason, status):
    proof["decision"] = decision
    proof["reason"] = reason
    encoded = (json.dumps(proof, indent=2, sort_keys=True) + "\n").encode()
    with proof_path.open("wb") as handle:
        handle.write(encoded)
        handle.flush()
        os.fsync(handle.fileno())
    print(
        f"guest bootstrap: agent socket cleanup decision={decision}: {reason}",
        file=sys.stderr,
    )
    raise SystemExit(status)


entries_before = active_path_entries()
proof["active_kernel_entries_before"] = entries_before
try:
    initial = os.lstat(socket_path)
except OSError as exc:
    if exc.errno != errno.ENOENT:
        finish("refused", f"cannot inspect exact agent socket: {exc}", 1)
    proof["socket_before"] = None
    if entries_before:
        finish(
            "refused",
            "agent socket path is absent but an active kernel socket still names it",
            1,
        )
    proof["socket_after"] = None
    proof["active_kernel_entries_after"] = active_path_entries()
    if proof["active_kernel_entries_after"]:
        finish("refused", "an active agent socket appeared during absence proof", 1)
    finish("already-absent", "no filesystem or active kernel socket remained", 0)

proof["socket_before"] = socket_description(initial)
if entries_before:
    finish(
        "refused",
        "an active listener or process still owns the exact agent socket",
        1,
    )
if not stat.S_ISSOCK(initial.st_mode):
    finish("refused", "the exact agent socket path is not a socket", 1)
if (
    initial.st_uid != expected_uid
    or initial.st_gid != expected_gid
    or stat.S_IMODE(initial.st_mode) != 0o660
    or initial.st_nlink != 1
):
    finish("refused", "the stale socket metadata differs from root:celikpanel 0660", 1)

try:
    stable = os.lstat(socket_path)
except OSError as exc:
    finish("refused", f"agent socket changed during stale-socket proof: {exc}", 1)
if socket_description(stable) != socket_description(initial):
    finish("refused", "agent socket identity changed during stale-socket proof", 1)
entries_pre_unlink = active_path_entries()
proof["active_kernel_entries_pre_unlink"] = entries_pre_unlink
if entries_pre_unlink:
    finish("refused", "an active agent socket appeared before unlink", 1)

try:
    os.unlink(socket_path)
except OSError as exc:
    finish("refused", f"cannot remove verified stale agent socket: {exc}", 1)
try:
    after = os.lstat(socket_path)
except OSError as exc:
    if exc.errno != errno.ENOENT:
        finish("refused", f"cannot prove agent socket absence: {exc}", 1)
    proof["socket_after"] = None
else:
    proof["socket_after"] = socket_description(after)
    finish("refused", "agent socket path reappeared after unlink", 1)
proof["active_kernel_entries_after"] = active_path_entries()
if proof["active_kernel_entries_after"]:
    finish("refused", "an active agent socket appeared after unlink", 1)
finish(
    "removed-verified-stale-socket",
    "removed exact inactive root:celikpanel 0660 socket and proved absence",
    0,
)
PY
    then
        cleanup_status=0
    else
        cleanup_status=$?
    fi
    mv -T --no-clobber "$temporary" "$COORDINATOR_STOP_PROOF" \
        || { rm -f "$temporary"; die "coordinator stop proof already exists"; }
    require_regular "$COORDINATOR_STOP_PROOF"
    [[ $(stat -Lc '%U:%G:%a' "$COORDINATOR_STOP_PROOF") == root:root:600 ]] \
        || die "coordinator stop proof metadata mismatch"
    (( cleanup_status == 0 )) \
        || die "refused unsafe agent socket cleanup; see $COORDINATOR_STOP_PROOF"
}

wait_for_socket() {
    local deadline=$((SECONDS + 30))
    while (( SECONDS < deadline )); do
        [[ -S $AGENT_SOCKET && ! -L $AGENT_SOCKET ]] && return 0
        sleep 0.1
    done
    die "agent socket did not become ready"
}

wait_for_panel() {
    local deadline=$((SECONDS + 60))
    local stable=0
    while (( SECONDS < deadline )); do
        if systemctl is-active --quiet celikpanel-panel.service && \
           python3 - <<'PY'
import socket
with socket.create_connection(("127.0.0.1", 2083), timeout=1):
    pass
PY
        then
            ((stable += 1))
            if (( stable >= 20 )); then
                return 0
            fi
        else
            stable=0
        fi
        sleep 0.25
    done
    journalctl -u celikpanel-panel.service --no-pager -n 80 >&2 || true
    die "panel did not become active"
}

verify_fixture_identity() {
    local cell_id=$1 node=$2 marker=/etc/celikpanel-dns-kill-matrix
    require_regular "$marker"
    grep -Fxq 'schema=celikpanel/dns-kill-fixture-plan/v1' "$marker" \
        || die "fixture marker schema mismatch"
    grep -Fxq "cell_id=$cell_id" "$marker" || die "fixture cell mismatch"
    grep -Fxq "node=$node" "$marker" || die "fixture node mismatch"
}

verify_os() {
    local node=$1
    # os-release is distribution-owned data. Refuse an unexpected image rather
    # than trying to make its package/service layout look compatible.
    # shellcheck disable=SC1091
    . /etc/os-release
    case $node in
        arch) [[ ${ID:-} == arch ]] || die "Arch node booted a non-Arch image" ;;
        debian13)
            [[ ${ID:-} == debian && ${VERSION_ID:-} == 13* ]] \
                || die "Debian 13 node booted an unexpected image"
            ;;
        *) die "unsupported fixture node: $node" ;;
    esac
}

ensure_service_identity() {
    if getent group celikpanel >/dev/null; then
        [[ $(getent group celikpanel | cut -d: -f1) == celikpanel ]] \
            || die "celikpanel group lookup is ambiguous"
    else
        groupadd --system celikpanel
    fi
    if id celikpanel >/dev/null 2>&1; then
        [[ $(id -gn celikpanel) == celikpanel ]] \
            || die "existing celikpanel user has the wrong primary group"
    else
        local nologin
        nologin=$(command -v nologin) || die "nologin executable is unavailable"
        useradd --system --gid celikpanel --home-dir /var/lib/celikpanel \
            --shell "$nologin" celikpanel
    fi
}

install_bundle() {
    local stage=$1 manifest_sha=$2 cell_id=$3 node=$4
    require_simple_value "bundle checksum" "$manifest_sha" '^[0-9a-f]{64}$'
    [[ $stage =~ ^/var/tmp/celikpanel-dns-kill-bootstrap-[0-9a-f]{16}$ ]] \
        || die "staging path is outside the fixed fixture namespace"
    [[ -d $stage && ! -L $stage ]] || die "staging directory is missing or unsafe"
    require_regular "$stage/SHA256SUMS"
    [[ $(sha256sum "$stage/SHA256SUMS" | cut -d' ' -f1) == "$manifest_sha" ]] \
        || die "bundle manifest checksum mismatch"
    local expected=(agent agent.kill panel dns-kill-trigger celikpanel-agent.service \
        celikpanel-panel.service web.tar guest_bootstrap.sh guest_recovery_probe.py \
        dns-kill-run-cell.py manifest.json)
    local name
    for name in "${expected[@]}"; do require_regular "$stage/$name"; done
    [[ $(wc -l < "$stage/SHA256SUMS") -eq ${#expected[@]} ]] \
        || die "bundle manifest has an unexpected member count"
    (cd "$stage" && sha256sum -c SHA256SUMS)

    verify_fixture_identity "$cell_id" "$node"
    verify_os "$node"
    ensure_service_identity

    install -d -m 0755 -o root -g root /opt/celikpanel /opt/celikpanel/bin
    install -d -m 0755 -o root -g root /opt/celikpanel/libexec
    install -d -m 0775 -o root -g celikpanel /opt/celikpanel/runtimes
    install -d -m 0750 -o celikpanel -g celikpanel /var/lib/celikpanel
    install -d -m 0700 -o root -g root /var/lib/celikpanel-imports
    install -d -m 0750 -o root -g celikpanel /etc/celikpanel /run/celikpanel
    install -d -m 0750 -o root -g celikpanel /etc/celikpanel/dkim
    install -d -m 0700 -o root -g celikpanel "$STATE_DIR"
    install -d -m 0700 -o root -g root "$FIXTURE_DIR"

    install -m 0755 -o root -g root "$stage/agent" /opt/celikpanel/bin/agent
    install -m 0755 -o root -g root "$stage/agent.kill" /opt/celikpanel/bin/agent.kill
    install -m 0755 -o root -g root "$stage/panel" /opt/celikpanel/bin/panel
    install -m 0755 -o root -g root "$stage/dns-kill-trigger" \
        /opt/celikpanel/bin/dns-kill-trigger
    install -m 0755 -o root -g root "$stage/guest_recovery_probe.py" \
        /opt/celikpanel/libexec/dns-kill-recovery-probe.py
    install -m 0755 -o root -g root "$stage/dns-kill-run-cell.py" \
        /opt/celikpanel/libexec/dns-kill-run-cell.py
    install -m 0600 -o root -g root "$stage/manifest.json" \
        "$FIXTURE_DIR/manifest.json"
    install -m 0644 -o root -g root "$stage/celikpanel-agent.service" \
        /etc/systemd/system/celikpanel-agent.service
    install -m 0644 -o root -g root "$stage/celikpanel-panel.service" \
        /etc/systemd/system/celikpanel-panel.service

    [[ ! -e /opt/celikpanel/web ]] || die "web root already exists on fresh fixture"
    install -d -m 0755 -o root -g root /opt/celikpanel/web
    tar -xf "$stage/web.tar" -C /opt/celikpanel/web --no-same-owner --no-same-permissions
    [[ -f /opt/celikpanel/web/index.html && ! -L /opt/celikpanel/web/index.html ]] \
        || die "installed web tree has no real index.html"
    [[ -z $(find /opt/celikpanel/web -xdev -type l -print -quit) ]] \
        || die "installed web tree contains a symlink"
    [[ -z $(find /opt/celikpanel/web -xdev ! -type d ! -type f -print -quit) ]] \
        || die "installed web tree contains a special file"
    chown -R root:root /opt/celikpanel/web
    find /opt/celikpanel/web -xdev -type d -exec chmod 0755 -- {} +
    find /opt/celikpanel/web -xdev -type f -exec chmod 0644 -- {} +

    local panel_env
    panel_env=$(mktemp /etc/celikpanel/.panel.env.XXXXXXXX)
    chmod 0600 "$panel_env"
    chown root:root "$panel_env"
    printf '%s\n' \
        'CELIKPANEL_LISTEN=:2083' \
        'CELIKPANEL_TLS=1' \
        'CELIKPANEL_TLS_DIR=/var/lib/celikpanel/tls' \
        'CELIKPANEL_PANEL_INSECURE_COOKIES_FLAG=' \
        'CELIKPANEL_PANEL_DEMO_FLAG=' > "$panel_env"
    mv -T --no-clobber "$panel_env" /etc/celikpanel/panel.env \
        || { rm -f "$panel_env"; die "panel.env already exists"; }

    [[ ! -e $STATE_DIR/service-mutations.json ]] \
        || die "mutation ledger already exists on fresh fixture"
    CELIKPANEL_AGENT_STATE_DIR=$STATE_DIR \
    CELIKPANEL_MUTATION_LOCK=$MUTATION_LOCK \
        /opt/celikpanel/bin/agent --initialize-service-mutation-ledger
    require_regular "$STATE_DIR/service-mutations.json"
    [[ $(stat -Lc '%U:%G:%a' "$STATE_DIR/service-mutations.json") == root:celikpanel:600 ]] \
        || die "mutation ledger metadata mismatch"
    require_regular "$MUTATION_LOCK"
    [[ $(stat -Lc '%U:%G:%a:%s' "$MUTATION_LOCK") == root:celikpanel:600:0 ]] \
        || die "mutation lock metadata mismatch"

    # The production panel refuses an empty user database. Create one
    # disposable administrator through the supported CLI. The password is
    # derived in-memory from the fixture identity, sent only on stdin, and
    # neither printed nor stored by this harness.
    local admin_password
    admin_password="S1-$(printf '%s\0fixture-admin' "$cell_id" | sha256sum | cut -c1-32)x"
    printf 's1-admin\ns1-admin@fixture.invalid\n%s\n' "$admin_password" | \
        runuser -u celikpanel -- env \
            CELIKPANEL_DATA_DIR=/var/lib/celikpanel \
            CELIKPANEL_WEB_DIR=/opt/celikpanel/web \
            /opt/celikpanel/bin/panel --create-admin >/dev/null
    unset admin_password
    [[ $(runuser -u celikpanel -- env CELIKPANEL_DATA_DIR=/var/lib/celikpanel \
        /opt/celikpanel/bin/panel --count-users) == 1 ]] \
        || die "fixture administrator was not created exactly once"

    systemctl daemon-reload
    systemctl enable celikpanel-agent.service celikpanel-panel.service >/dev/null
    systemctl start celikpanel-agent.service
    wait_for_socket
    require_regular "$TOKEN_FILE"
    [[ $(stat -Lc '%U:%G:%a' "$TOKEN_FILE") == root:celikpanel:640 ]] \
        || die "agent token metadata mismatch"
    systemctl start celikpanel-panel.service
    wait_for_panel

    sha256sum /opt/celikpanel/bin/agent /opt/celikpanel/bin/agent.kill \
        /opt/celikpanel/bin/panel /opt/celikpanel/bin/dns-kill-trigger \
        /opt/celikpanel/libexec/dns-kill-recovery-probe.py \
        /opt/celikpanel/libexec/dns-kill-run-cell.py "$FIXTURE_DIR/manifest.json" \
        > "$FIXTURE_DIR/installed-binaries.sha256"
    chmod 0600 "$FIXTURE_DIR/installed-binaries.sha256"
    chown root:root "$FIXTURE_DIR/installed-binaries.sha256"
    sync -f /opt/celikpanel/bin "$STATE_DIR" "$FIXTURE_DIR" /etc/systemd/system
}

dns_probe() {
    local address=$1 name=$2
    python3 - "$address" "$name" <<'PY'
import os, random, socket, struct, sys

address, name = sys.argv[1:]
identifier = random.SystemRandom().randrange(1, 65536)
labels = name.rstrip('.').split('.')
question = b''.join(bytes([len(x)]) + x.encode('ascii') for x in labels) + b'\0'
packet = struct.pack('!HHHHHH', identifier, 0x0100, 1, 0, 0, 0) + question + struct.pack('!HH', 1, 1)

def validate(reply):
    if len(reply) < 12:
        raise SystemExit('truncated DNS reply')
    rid, flags, qd, an, ns, ar = struct.unpack('!HHHHHH', reply[:12])
    if rid != identifier or flags & 0x8000 == 0 or flags & 0x0400 == 0:
        raise SystemExit('DNS reply lacks matching authoritative identity')
    if flags & 0x000f or qd != 1 or an < 1:
        raise SystemExit('DNS reply is not a successful authoritative answer')

family = socket.AF_INET6 if ':' in address else socket.AF_INET
with socket.socket(family, socket.SOCK_DGRAM) as sock:
    sock.settimeout(3)
    sock.sendto(packet, (address, 53))
    validate(sock.recvfrom(65535)[0])
with socket.socket(family, socket.SOCK_STREAM) as sock:
    sock.settimeout(3)
    sock.connect((address, 53))
    sock.sendall(struct.pack('!H', len(packet)) + packet)
    size = struct.unpack('!H', sock.recv(2))[0]
    reply = b''
    while len(reply) < size:
        chunk = sock.recv(size - len(reply))
        if not chunk:
            raise SystemExit('truncated TCP DNS reply')
        reply += chunk
    validate(reply)
PY
}

global_ipv4() {
    local value
    value=$(ip -o -4 addr show dev mgmt0 scope global | awk 'NR == 1 {split($4,a,"/"); print a[1]}')
    [[ $value =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]] \
        || die "could not resolve the fixture management IPv4 address"
    printf '%s\n' "$value"
}

assert_no_global_dns_listener() {
    local address=$1
    python3 - "$address" <<'PY'
import socket, sys
address = sys.argv[1]
for kind in (socket.SOCK_DGRAM, socket.SOCK_STREAM):
    with socket.socket(socket.AF_INET, kind) as sock:
        sock.bind((address, 53))
        if kind == socket.SOCK_STREAM:
            sock.listen(1)
PY
}

assert_no_source_engine() {
    local address=$1
    [[ ! -e $STATE_DIR/dns-engine-state.json && ! -L $STATE_DIR/dns-engine-state.json ]] \
        || die "uninitialized source unexpectedly has an engine receipt"
    [[ ! -e $STATE_DIR/dns-engine-switch-journal.json && \
       ! -L $STATE_DIR/dns-engine-switch-journal.json ]] \
        || die "uninitialized source unexpectedly has a switch journal"
    local unit
    for unit in bind9.service named.service pdns.service; do
        ! systemctl is-active --quiet "$unit" \
            || die "uninitialized source unexpectedly has active $unit"
    done
    local engine receipt
    for engine in bind pdns; do
        for receipt in \
            "$STATE_DIR/dns-engine-ownership-$engine.json" \
            "$STATE_DIR/dns-engine-install-ownership-$engine.json"
        do
            [[ ! -e $receipt && ! -L $receipt ]] \
                || die "uninitialized source unexpectedly has ownership residue: $receipt"
        done
    done
    # Ignore local resolver stubs such as 127.0.0.53: the production engines
    # bind global addresses. Prove both global UDP and TCP port 53 are free.
    assert_no_global_dns_listener "$address" \
        || die "uninitialized source global address already has a DNS listener"
}

require_apt_package_absent() {
    local package_name=$1 output status expected
    if output=$(LC_ALL=C /usr/bin/dpkg-query -W -f='${Status}' -- "$package_name" 2>&1); then
        die "source preparation expected absent package: $package_name"
    else
        status=$?
    fi
    expected="dpkg-query: no packages found matching $package_name"
    [[ $status -eq 1 && $output == "$expected" ]] || die "source preparation could not prove exact package absence: $package_name"
}

require_apt_package_installed() {
    local package_name=$1 output
    output=$(LC_ALL=C /usr/bin/dpkg-query -W -f='${Status}' -- "$package_name") || die "source preparation could not inspect installed package: $package_name"
    [[ $output == 'install ok installed' ]] || die "source preparation found a non-canonical package state: $package_name"
}

write_source_preinstall_proof() {
    local cell_id=$1 server_version=$2 sqlite_version=$3 unit_file_state=$4
    local purpose=$5
    [[ $purpose == bind || $purpose == pdns-adopt ]] || die "source preinstall purpose is unsupported: $purpose"
    [[ ! -e $SOURCE_PREINSTALL_PROOF && ! -L $SOURCE_PREINSTALL_PROOF ]] || die "source preinstall proof already exists"
    local temporary
    temporary=$(mktemp "$FIXTURE_DIR/.source-preinstall.XXXXXXXX")
    chmod 0600 "$temporary"
    SOURCE_PREINSTALL_CELL_ID=$cell_id PDNS_SERVER_VERSION=$server_version PDNS_SQLITE_VERSION=$sqlite_version PDNS_UNIT_FILE_STATE=$unit_file_state SOURCE_PREINSTALL_PURPOSE=$purpose python3 - "$temporary" <<'PY'
# SOURCE_PREINSTALL_PROOF_RENDERER
import json, os, sys

if os.environ["SOURCE_PREINSTALL_PURPOSE"] == "bind":
    scope = "managed-pdns-source-preparation-for-bind-only"
    measured_target_packages = [{"name": "bind9", "status": "absent"}]
else:
    scope = "external-pdns-source-preparation-for-measured-adoption-only"
    measured_target_packages = [
        {"name": "pdns-backend-sqlite3", "status": "preexisting-required-by-adoption"},
        {"name": "pdns-server", "status": "preexisting-required-by-adoption"},
    ]

value = {
    "schema": "celikpanel/dns-kill-source-preinstall/v1",
    "cell_id": os.environ["SOURCE_PREINSTALL_CELL_ID"],
    "scope": scope,
    "package_install_origin": "harness-source-preinstall",
    "source_packages": [
        {"name": "pdns-backend-sqlite3", "status": "install ok installed", "version": os.environ["PDNS_SQLITE_VERSION"]},
        {"name": "pdns-server", "status": "install ok installed", "version": os.environ["PDNS_SERVER_VERSION"]},
    ],
    "measured_target_packages": measured_target_packages,
    "install_guard": {
        "unit": "pdns.service",
        "persistent_mask_target": "/dev/null",
        "package_hooks_could_not_start": True,
    },
    "mask_removed_before_external_source_start": True,
    "source_unit_before_external_configuration": {
        "name": "pdns.service",
        "load_state": "loaded",
        "active_state": "inactive",
        "unit_file_state": os.environ["PDNS_UNIT_FILE_STATE"],
    },
    "dns_state_absent": True,
    "dns_journal_absent": True,
    "dns_ownership_receipts_absent": True,
    "global_udp_tcp_53_bindable": True,
    "production_pdns_adoption_pending": True,
}
with open(sys.argv[1], "w", encoding="utf-8", newline="\n") as handle:
    json.dump(value, handle, indent=2, sort_keys=True)
    handle.write("\n")
    handle.flush()
    os.fsync(handle.fileno())
PY
    mv -T --no-clobber "$temporary" "$SOURCE_PREINSTALL_PROOF" || { rm -f "$temporary"; die "source preinstall proof already exists"; }
    require_regular "$SOURCE_PREINSTALL_PROOF"
    [[ $(stat -Lc '%U:%G:%a' "$SOURCE_PREINSTALL_PROOF") == root:root:600 ]] || die "source preinstall proof metadata mismatch"
    python3 - "$SOURCE_PREINSTALL_PROOF" <<'PY'
import json, sys

path = sys.argv[1]
raw = open(path, "rb").read()
value = json.loads(raw)
expected_keys = {
    "schema", "cell_id", "scope", "package_install_origin", "source_packages",
    "measured_target_packages", "install_guard",
    "mask_removed_before_external_source_start", "source_unit_before_external_configuration",
    "dns_state_absent", "dns_journal_absent", "dns_ownership_receipts_absent",
    "global_udp_tcp_53_bindable", "production_pdns_adoption_pending",
}
if set(value) != expected_keys:
    raise SystemExit("source preinstall proof fields differ from the exact contract")
if value["schema"] != "celikpanel/dns-kill-source-preinstall/v1":
    raise SystemExit("source preinstall proof schema mismatch")
if value["scope"] == "managed-pdns-source-preparation-for-bind-only":
    expected_target = [{"name": "bind9", "status": "absent"}]
elif value["scope"] == "external-pdns-source-preparation-for-measured-adoption-only":
    expected_target = [
        {"name": "pdns-backend-sqlite3", "status": "preexisting-required-by-adoption"},
        {"name": "pdns-server", "status": "preexisting-required-by-adoption"},
    ]
else:
    raise SystemExit("source preinstall proof escaped its exact driver scope")
if value["package_install_origin"] != "harness-source-preinstall":
    raise SystemExit("source preinstall package origin mismatch")
if [item["name"] for item in value["source_packages"]] != ["pdns-backend-sqlite3", "pdns-server"]:
    raise SystemExit("source preinstall package set mismatch")
if any(item["status"] != "install ok installed" or not item["version"] for item in value["source_packages"]):
    raise SystemExit("source preinstall package evidence is incomplete")
if value["measured_target_packages"] != expected_target:
    raise SystemExit("source preinstall measured-target package contract differs")
unit = value["source_unit_before_external_configuration"]
if unit["name"] != "pdns.service" or unit["load_state"] != "loaded" or unit["active_state"] != "inactive" or unit["unit_file_state"] not in ("disabled", "enabled"):
    raise SystemExit("source preinstall unit terminal state is invalid")
for key in ("mask_removed_before_external_source_start", "dns_state_absent", "dns_journal_absent", "dns_ownership_receipts_absent", "global_udp_tcp_53_bindable", "production_pdns_adoption_pending"):
    if value[key] is not True:
        raise SystemExit("source preinstall boolean proof is false: " + key)
canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
if raw != canonical:
    raise SystemExit("source preinstall proof is not canonical sorted JSON")
PY
    sync -f "$SOURCE_PREINSTALL_PROOF" "$FIXTURE_DIR"
}

preinstall_pdns_source_packages() {
    local cell_id=$1 address=$2 purpose=$3
    local packages=(pdns-backend-sqlite3 pdns-server)
    [[ -x /usr/bin/apt-get && -x /usr/bin/dpkg-query ]] || die "managed PowerDNS source preinstall requires exact APT executables"
    assert_no_source_engine "$address"
    require_apt_package_absent bind9
    local package_name
    for package_name in "${packages[@]}"; do
        require_apt_package_absent "$package_name"
    done
    [[ ! -e $SOURCE_PREINSTALL_PROOF && ! -L $SOURCE_PREINSTALL_PROOF ]] || die "source preinstall proof unexpectedly exists"
    [[ ! -e /etc/systemd/system/pdns.service && ! -L /etc/systemd/system/pdns.service ]] || die "source preinstall found a preexisting PowerDNS systemd override"
    [[ $(stat -Lc '%U:%G:%a' /etc/systemd/system) == root:root:755 ]] || die "source preinstall found unsafe systemd parent metadata"

    DEBIAN_FRONTEND=noninteractive LC_ALL=C /usr/bin/apt-get update
    /usr/bin/systemctl mask pdns.service
    /usr/bin/systemctl daemon-reload
    [[ -L /etc/systemd/system/pdns.service && $(readlink /etc/systemd/system/pdns.service) == /dev/null ]] || die "source preinstall could not establish the exact persistent PowerDNS mask"
    DEBIAN_FRONTEND=noninteractive LC_ALL=C /usr/bin/apt-get install -y --no-install-recommends "${packages[@]}"

    [[ $(/usr/bin/systemctl show -p LoadState --value pdns.service) == masked ]] || die "PowerDNS package install escaped its load-state mask"
    [[ $(/usr/bin/systemctl show -p ActiveState --value pdns.service) == inactive ]] || die "PowerDNS package hook escaped its start mask"
    [[ $(/usr/bin/systemctl show -p UnitFileState --value pdns.service) == masked ]] || die "PowerDNS package install changed its unit-file mask"
    /usr/bin/systemctl unmask pdns.service
    /usr/bin/systemctl daemon-reload
    [[ ! -e /etc/systemd/system/pdns.service && ! -L /etc/systemd/system/pdns.service ]] || die "source preinstall retained its temporary PowerDNS mask"

    local unit_file_state
    [[ $(/usr/bin/systemctl show -p LoadState --value pdns.service) == loaded ]] || die "preinstalled PowerDNS vendor unit is not loaded"
    [[ $(/usr/bin/systemctl show -p ActiveState --value pdns.service) == inactive ]] || die "preinstalled PowerDNS unit is not inactive"
    unit_file_state=$(/usr/bin/systemctl show -p UnitFileState --value pdns.service)
    [[ $unit_file_state == disabled || $unit_file_state == enabled ]] || die "preinstalled PowerDNS unit file state is not exact"
    for package_name in "${packages[@]}"; do
        require_apt_package_installed "$package_name"
    done
    require_apt_package_absent bind9
    assert_no_source_engine "$address"

    local server_version sqlite_version
    server_version=$(LC_ALL=C /usr/bin/dpkg-query -W -f='${Version}' -- pdns-server)
    sqlite_version=$(LC_ALL=C /usr/bin/dpkg-query -W -f='${Version}' -- pdns-backend-sqlite3)
    [[ -n $server_version && -n $sqlite_version ]] || die "source preinstall package version proof is empty"
    write_source_preinstall_proof "$cell_id" "$server_version" "$sqlite_version" "$unit_file_state" "$purpose"
}

create_external_pdns_source() {
    local address=$1 scenario=$2
    local main=/etc/powerdns/pdns.conf
    local managed_dir=/etc/powerdns/pdns.d
    local managed=$managed_dir/celikpanel.conf
    local cluster=$managed_dir/celikpanel-cluster.conf
    local database=/var/lib/powerdns/pdns.sqlite3
    local schema=/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql

    assert_no_source_engine "$address"
    require_apt_package_absent bind9
    require_apt_package_installed pdns-server
    require_apt_package_installed pdns-backend-sqlite3
    require_regular "$main"
    require_regular "$schema"
    local main_metadata schema_owner
    main_metadata=$(stat -Lc '%U:%G:%a' "$main")
    [[ $main_metadata == root:pdns:640 || $main_metadata == root:root:640 ]] || die "external PowerDNS main config metadata is outside the production adoption contract"
    schema_owner=$(LC_ALL=C /usr/bin/dpkg-query -S -- "$schema") || die "external PowerDNS schema has no package owner"
    [[ $schema_owner == "pdns-backend-sqlite3: $schema" ]] || die "external PowerDNS schema is not owned by the exact source package"

    if [[ -e $managed_dir || -L $managed_dir ]]; then
        [[ -d $managed_dir && ! -L $managed_dir ]] || die "external PowerDNS managed config parent is not a real directory"
        [[ $(stat -Lc '%U:%G:%a' "$managed_dir") == root:root:755 ]] || die "external PowerDNS managed config parent metadata is unsafe"
        [[ -z $(find "$managed_dir" -mindepth 1 -maxdepth 1 -print -quit) ]] || die "external PowerDNS managed config parent is not empty"
    else
        install -d -m 0755 -o root -g root "$managed_dir"
    fi
    [[ ! -e $managed && ! -L $managed && ! -e $cluster && ! -L $cluster ]] || die "external PowerDNS managed config unexpectedly exists"

    local temporary
    temporary=$(mktemp "$managed_dir/.celikpanel.conf.XXXXXXXX")
    chmod 0600 "$temporary"
    EXTERNAL_PDNS_ADDRESS=$address python3 - "$temporary" <<'PY'
import os, sys

data = (
    "# Managed by CelikPanel; do not edit by hand.\n"
    "launch=gsqlite3\n"
    "gsqlite3-dnssec=yes\n"
    "gsqlite3-database=/var/lib/powerdns/pdns.sqlite3\n"
    f"local-address={os.environ['EXTERNAL_PDNS_ADDRESS']}\n"
    "zone-cache-refresh-interval=0\n"
    "webserver=no\n"
    "api=no\n"
).encode()
with open(sys.argv[1], "wb") as handle:
    handle.write(data)
    handle.flush()
    os.fsync(handle.fileno())
PY
    chown root:root "$temporary"
    chmod 0644 "$temporary"
    mv -T --no-clobber "$temporary" "$managed" || { rm -f "$temporary"; die "external PowerDNS managed config already exists"; }
    sync -f "$managed" "$managed_dir"

    [[ ! -e $database && ! -L $database ]] || die "external PowerDNS database unexpectedly exists"
    install -d -m 0755 -o pdns -g pdns /var/lib/powerdns
    EXTERNAL_PDNS_SCENARIO=$scenario EXTERNAL_PDNS_SCHEMA=$schema EXTERNAL_PDNS_DATABASE=$database python3 - <<'PY'
import json, os, sqlite3

scenario_path = os.environ["EXTERNAL_PDNS_SCENARIO"]
schema_path = os.environ["EXTERNAL_PDNS_SCHEMA"]
database_path = os.environ["EXTERNAL_PDNS_DATABASE"]
with open(scenario_path, encoding="utf-8") as handle:
    scenario = json.load(handle)
expected_header = {
    "schema": "celikpanel-dns-kill-matrix-trigger/v1",
    "driver": "pdns-adopt",
    "source_fixture": "external-pdns-adoption",
    "mode": "adopt",
    "source_engine": "",
    "target_engine": "pdns",
    "source_epoch": 0,
    "target_epoch": 1,
    "source_revision": 0,
    "topology": "standalone",
}
for key, expected in expected_header.items():
    if scenario.get(key) != expected:
        raise SystemExit(f"external PowerDNS scenario {key} differs")
if set(scenario) != set(expected_header) | {"zones"}:
    raise SystemExit("external PowerDNS scenario fields differ")
zones = scenario["zones"]
if not isinstance(zones, list) or len(zones) != 1:
    raise SystemExit("external PowerDNS scenario requires one exact zone")
zone = zones[0]
expected_zone = {
    "ordinal": 0,
    "domain": "s1-kill.test",
    "desired_generation": 1,
    "delete": False,
    "zone_type": "NATIVE",
    "records": [
        {
            "name": "s1-kill.test", "type": "SOA",
            "content": "ns1.s1-kill.test hostmaster.s1-kill.test 2026083101 10800 3600 604800 3600",
            "ttl": 3600, "prio": 0, "disabled": False,
        },
        {
            "name": "s1-kill.test", "type": "NS",
            "content": "ns1.s1-kill.test", "ttl": 3600, "prio": 0,
            "disabled": False,
        },
        {
            "name": "ns1.s1-kill.test", "type": "A",
            "content": "192.0.2.10", "ttl": 300, "prio": 0,
            "disabled": False,
        },
        {
            "name": "www.s1-kill.test", "type": "A",
            "content": "192.0.2.10", "ttl": 300, "prio": 0,
            "disabled": False,
        },
    ],
    "zone_qualifier": "",
}
if zone != expected_zone:
    raise SystemExit("external PowerDNS zone differs from the exact adoption fixture")
fd = os.open(database_path, os.O_CREAT | os.O_EXCL | os.O_RDWR, 0o600)
os.close(fd)
with open(schema_path, encoding="utf-8") as handle:
    schema = handle.read()
connection = sqlite3.connect("file:" + database_path + "?mode=rw", uri=True)
try:
    connection.executescript(schema)
    connection.execute("PRAGMA journal_mode=DELETE")
    connection.execute("PRAGMA synchronous=FULL")
    cursor = connection.execute(
        "INSERT INTO domains(name, type) VALUES (?, ?)",
        (zone["domain"], zone["zone_type"]),
    )
    domain_id = cursor.lastrowid
    for record in zone["records"]:
        connection.execute(
            """
            INSERT INTO records(
              domain_id, name, type, content, ttl, prio, disabled, ordername, auth
            ) VALUES (?, ?, ?, ?, ?, ?, ?, NULL, 1)
            """,
            (
                domain_id, record["name"], record["type"], record["content"],
                record["ttl"], record["prio"], int(record["disabled"]),
            ),
        )
    connection.commit()
    if connection.execute("PRAGMA quick_check").fetchone() != ("ok",):
        raise SystemExit("external PowerDNS database failed quick_check")
    if connection.execute("SELECT COUNT(*) FROM domains").fetchone() != (1,):
        raise SystemExit("external PowerDNS database has extra domains")
    if connection.execute("SELECT COUNT(*) FROM records").fetchone() != (4,):
        raise SystemExit("external PowerDNS database has extra records")
    if connection.execute("SELECT COUNT(*) FROM supermasters").fetchone() != (0,):
        raise SystemExit("external PowerDNS database has a supermaster")
finally:
    connection.close()
PY
    chown pdns:pdns "$database"
    chmod 0640 "$database"
    [[ $(stat -Lc '%U:%G:%a' "$database") == pdns:pdns:640 ]] || die "external PowerDNS database metadata mismatch"
    local suffix
    for suffix in -journal -wal -shm; do
        [[ ! -e $database$suffix && ! -L $database$suffix ]] || die "external PowerDNS database has an unresolved SQLite sidecar"
    done
    sync -f "$database" /var/lib/powerdns /etc/powerdns "$managed_dir"

    /usr/bin/systemctl enable --now pdns.service
    [[ $(/usr/bin/systemctl show -p LoadState --value pdns.service) == loaded ]] || die "external PowerDNS source unit is not loaded"
    [[ $(/usr/bin/systemctl show -p ActiveState --value pdns.service) == active ]] || die "external PowerDNS source unit is not active"
    [[ $(/usr/bin/systemctl show -p UnitFileState --value pdns.service) == enabled ]] || die "external PowerDNS source unit is not enabled"
    [[ $(/usr/bin/systemctl show -p SubState --value pdns.service) == running ]] || die "external PowerDNS source unit is not running"
    [[ $(/usr/bin/systemctl show -p ControlPID --value pdns.service) == 0 ]] || die "external PowerDNS source has a control process"
    [[ $(/usr/bin/systemctl show -p MainPID --value pdns.service) =~ ^[1-9][0-9]*$ ]] || die "external PowerDNS source has no main process"
    [[ ! -e $STATE_DIR/dns-engine-state.json && ! -L $STATE_DIR/dns-engine-state.json ]] || die "external PowerDNS source was created with a production state receipt"
    [[ ! -e $STATE_DIR/dns-engine-switch-journal.json && ! -L $STATE_DIR/dns-engine-switch-journal.json ]] || die "external PowerDNS source was created with a production switch journal"
    local engine receipt
    for engine in bind pdns; do
        for receipt in "$STATE_DIR/dns-engine-ownership-$engine.json" "$STATE_DIR/dns-engine-install-ownership-$engine.json"
        do
            [[ ! -e $receipt && ! -L $receipt ]] || die "external PowerDNS source was created with production ownership: $receipt"
        done
    done
    require_apt_package_absent bind9
    dns_probe "$address" www.s1-kill.test
}

write_external_pdns_preimage_proof() {
    local cell_id=$1 address=$2 preinstall_sha=$3
    require_simple_value "external PowerDNS preimage hash" "$preinstall_sha" '^[0-9a-f]{64}$'
    [[ ! -e $EXTERNAL_PDNS_PREIMAGE_PROOF && ! -L $EXTERNAL_PDNS_PREIMAGE_PROOF ]] || die "external PowerDNS preimage proof already exists"
    require_regular "$SCENARIO_FILE"
    require_regular "$SOURCE_PREINSTALL_PROOF"
    [[ $(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1) == "$preinstall_sha" ]] || die "external PowerDNS preinstall proof changed before sealing the preimage"
    [[ $(systemctl show -p LoadState --value pdns.service) == loaded ]] || die "external PowerDNS preimage unit is not loaded"
    [[ $(systemctl show -p ActiveState --value pdns.service) == active ]] || die "external PowerDNS preimage unit is not active"
    [[ $(systemctl show -p SubState --value pdns.service) == running ]] || die "external PowerDNS preimage unit is not running"
    [[ $(systemctl show -p UnitFileState --value pdns.service) == enabled ]] || die "external PowerDNS preimage unit is not enabled"
    dns_probe "$address" www.s1-kill.test

    local temporary
    temporary=$(mktemp "$FIXTURE_DIR/.external-pdns-preimage.XXXXXXXX")
    chmod 0600 "$temporary"
    EXTERNAL_PDNS_CELL_ID=$cell_id EXTERNAL_PDNS_ADDRESS=$address EXTERNAL_PDNS_SCENARIO=$SCENARIO_FILE EXTERNAL_PDNS_PREINSTALL=$SOURCE_PREINSTALL_PROOF EXTERNAL_PDNS_PREINSTALL_SHA=$preinstall_sha EXTERNAL_PDNS_OUTPUT=$temporary python3 - <<'PY'
# EXTERNAL_PDNS_PREIMAGE_PROOF_RENDERER
import grp
import hashlib
import json
import os
import pwd
import sqlite3
import stat

cell_id = os.environ["EXTERNAL_PDNS_CELL_ID"]
address = os.environ["EXTERNAL_PDNS_ADDRESS"]
scenario_path = os.environ["EXTERNAL_PDNS_SCENARIO"]
preinstall_path = os.environ["EXTERNAL_PDNS_PREINSTALL"]
preinstall_sha = os.environ["EXTERNAL_PDNS_PREINSTALL_SHA"]
output_path = os.environ["EXTERNAL_PDNS_OUTPUT"]
state_dir = "/var/lib/celikpanel-agent-private"


def canonical_hash(value):
    return hashlib.sha256(
        json.dumps(value, separators=(",", ":"), sort_keys=True).encode()
    ).hexdigest()


def read_regular(path, label, mode, owners, maximum):
    before = os.lstat(path)
    if (
        stat.S_ISLNK(before.st_mode)
        or not stat.S_ISREG(before.st_mode)
        or before.st_nlink != 1
        or stat.S_IMODE(before.st_mode) != mode
        or before.st_size <= 0
        or before.st_size > maximum
    ):
        raise SystemExit(f"{label} metadata is outside the preimage contract")
    flags = os.O_RDONLY | getattr(os, "O_CLOEXEC", 0) | getattr(os, "O_NOFOLLOW", 0)
    descriptor = os.open(path, flags)
    try:
        opened = os.fstat(descriptor)
        if (opened.st_dev, opened.st_ino) != (before.st_dev, before.st_ino):
            raise SystemExit(f"{label} changed while opening")
        chunks = []
        remaining = maximum + 1
        while remaining:
            chunk = os.read(descriptor, min(65536, remaining))
            if not chunk:
                break
            chunks.append(chunk)
            remaining -= len(chunk)
        raw = b"".join(chunks)
        after = os.fstat(descriptor)
        if len(raw) > maximum or (
            after.st_dev,
            after.st_ino,
            after.st_size,
            after.st_mode,
            after.st_uid,
            after.st_gid,
        ) != (
            opened.st_dev,
            opened.st_ino,
            opened.st_size,
            opened.st_mode,
            opened.st_uid,
            opened.st_gid,
        ):
            raise SystemExit(f"{label} changed while reading")
    finally:
        os.close(descriptor)
    owner = f"{pwd.getpwuid(opened.st_uid).pw_name}:{grp.getgrgid(opened.st_gid).gr_name}"
    if owner not in owners:
        raise SystemExit(f"{label} owner is outside the preimage contract")
    return raw, {
        "path": path,
        "sha256": hashlib.sha256(raw).hexdigest(),
        "owner": owner,
        "mode": f"{mode:04o}",
    }


with open(scenario_path, "rb") as handle:
    scenario_raw = handle.read()
scenario = json.loads(scenario_raw)
expected_scenario = {
    "schema": "celikpanel-dns-kill-matrix-trigger/v1",
    "driver": "pdns-adopt",
    "source_fixture": "external-pdns-adoption",
    "mode": "adopt",
    "source_engine": "",
    "target_engine": "pdns",
    "source_epoch": 0,
    "target_epoch": 1,
    "source_revision": 0,
    "topology": "standalone",
}
if any(scenario.get(key) != expected for key, expected in expected_scenario.items()):
    raise SystemExit("external PowerDNS preimage scenario identity differs")
if set(scenario) != set(expected_scenario) | {"zones"}:
    raise SystemExit("external PowerDNS preimage scenario fields differ")
zones = scenario["zones"]
if not isinstance(zones, list) or not zones:
    raise SystemExit("external PowerDNS preimage scenario has no zones")

with open(preinstall_path, "rb") as handle:
    preinstall_raw = handle.read()
if hashlib.sha256(preinstall_raw).hexdigest() != preinstall_sha:
    raise SystemExit("external PowerDNS preinstall hash differs")
preinstall = json.loads(preinstall_raw)
if (
    preinstall.get("schema") != "celikpanel/dns-kill-source-preinstall/v1"
    or preinstall.get("cell_id") != cell_id
    or preinstall.get("scope")
    != "external-pdns-source-preparation-for-measured-adoption-only"
    or preinstall.get("production_pdns_adoption_pending") is not True
):
    raise SystemExit("external PowerDNS preinstall identity differs")

main_raw, main = read_regular(
    "/etc/powerdns/pdns.conf",
    "external PowerDNS main configuration",
    0o640,
    {"root:pdns", "root:root"},
    1 << 20,
)
managed_raw, managed = read_regular(
    "/etc/powerdns/pdns.d/celikpanel.conf",
    "external PowerDNS managed configuration",
    0o644,
    {"root:root"},
    1 << 20,
)
active_main = [
    line.strip()
    for line in main_raw.decode().splitlines()
    if line.strip() and not line.lstrip().startswith("#")
]
if active_main.count("include-dir=/etc/powerdns/pdns.d") != 1:
    raise SystemExit("external PowerDNS main configuration lacks its exact include")
expected_managed = [
    "# Managed by CelikPanel; do not edit by hand.",
    "launch=gsqlite3",
    "gsqlite3-dnssec=yes",
    "gsqlite3-database=/var/lib/powerdns/pdns.sqlite3",
    f"local-address={address}",
    "zone-cache-refresh-interval=0",
    "webserver=no",
    "api=no",
]
if managed_raw.decode().splitlines() != expected_managed:
    raise SystemExit("external PowerDNS managed configuration differs")

schema_raw, schema = read_regular(
    "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql",
    "external PowerDNS package schema",
    0o644,
    {"root:root"},
    1 << 20,
)
database_raw, database_file = read_regular(
    "/var/lib/powerdns/pdns.sqlite3",
    "external PowerDNS database",
    0o640,
    {"pdns:pdns"},
    65 << 20,
)
pdns_uid = pwd.getpwnam("pdns").pw_uid
pdns_gid = grp.getgrnam("pdns").gr_gid
nofollow = getattr(os, "O_NOFOLLOW", 0)
if not nofollow:
    raise SystemExit("O_NOFOLLOW is unavailable for external PowerDNS sidecar capture")


def identity(value):
    return (
        value.st_dev,
        value.st_ino,
        value.st_mode,
        value.st_nlink,
        value.st_uid,
        value.st_gid,
        value.st_size,
    )


def inspect_sidecar(path, label, required_size, require_empty):
    try:
        before = os.lstat(path)
    except OSError as exc:
        raise SystemExit(f"inspect {label}: {exc}") from exc
    if stat.S_ISLNK(before.st_mode) or not stat.S_ISREG(before.st_mode):
        raise SystemExit(f"{label} is not a regular non-symlink file")
    try:
        descriptor = os.open(
            path, os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK | nofollow
        )
    except OSError as exc:
        raise SystemExit(f"open {label}: {exc}") from exc
    try:
        opened = os.fstat(descriptor)
        if not stat.S_ISREG(opened.st_mode) or identity(before) != identity(opened):
            raise SystemExit(f"{label} changed while opening")
        if opened.st_uid != pdns_uid or opened.st_gid != pdns_gid:
            raise SystemExit(f"{label} has the wrong owner")
        if stat.S_IMODE(opened.st_mode) != 0o640:
            raise SystemExit(f"{label} has the wrong mode")
        if opened.st_nlink != 1:
            raise SystemExit(f"{label} must have exactly one link")
        if opened.st_size != required_size:
            raise SystemExit(f"{label} has the wrong size")
        if require_empty and os.read(descriptor, 1) != b"":
            raise SystemExit(f"{label} must be empty")
        try:
            after_path = os.lstat(path)
        except OSError as exc:
            raise SystemExit(f"reinspect {label}: {exc}") from exc
        final = os.fstat(descriptor)
        if identity(after_path) != identity(opened) or identity(final) != identity(opened):
            raise SystemExit(f"{label} changed while inspecting")
        return final
    finally:
        os.close(descriptor)


database_status = os.lstat(database_file["path"])
journal_path = database_file["path"] + "-journal"
try:
    os.lstat(journal_path)
except FileNotFoundError:
    pass
except OSError as exc:
    raise SystemExit(f"inspect external PowerDNS rollback journal: {exc}") from exc
else:
    raise SystemExit("external PowerDNS rollback journal must be absent")
wal_path = database_file["path"] + "-wal"
shm_path = database_file["path"] + "-shm"
wal = inspect_sidecar(
    wal_path, "external PowerDNS write-ahead log", required_size=0, require_empty=True
)
shm = inspect_sidecar(
    shm_path, "external PowerDNS shared memory", required_size=32768, require_empty=False
)
if wal.st_dev != database_status.st_dev or shm.st_dev != database_status.st_dev:
    raise SystemExit("external PowerDNS sidecar device differs from the database")
if len({database_status.st_ino, wal.st_ino, shm.st_ino}) != 3:
    raise SystemExit("external PowerDNS database and sidecar inodes conflict")


def sidecar_metadata(path, value, content_policy):
    return {
        "path": path,
        "file_type": "regular",
        "owner": "pdns:pdns",
        "mode": "0640",
        "link_count": value.st_nlink,
        "device": value.st_dev,
        "inode": value.st_ino,
        "size": value.st_size,
        "content_policy": content_policy,
    }


sidecars = {
    "rollback_journal": {"path": journal_path, "status": "absent"},
    "write_ahead_log": sidecar_metadata(wal_path, wal, "empty"),
    "shared_memory": sidecar_metadata(shm_path, shm, "volatile-unhashed"),
}

expected_domains = sorted(
    (zone["domain"], zone["zone_type"]) for zone in zones if not zone["delete"]
)
expected_records = sorted(
    (
        zone["domain"],
        record["name"],
        record["type"],
        record["content"],
        record["ttl"],
        record["prio"],
        int(record["disabled"]),
    )
    for zone in zones
    if not zone["delete"]
    for record in zone["records"]
)
connection = sqlite3.connect(
    "file:/var/lib/powerdns/pdns.sqlite3?mode=ro",
    uri=True,
    timeout=5.0,
)
try:
    connection.execute("PRAGMA query_only=ON")
    query_only = connection.execute("PRAGMA query_only").fetchone()
    quick_check = connection.execute("PRAGMA quick_check").fetchall()
    journal_mode = connection.execute("PRAGMA journal_mode").fetchone()
    domains = connection.execute(
        "SELECT name, type FROM domains ORDER BY name, type"
    ).fetchall()
    records = connection.execute(
        "SELECT d.name, r.name, r.type, r.content, r.ttl, r.prio, r.disabled "
        "FROM records AS r JOIN domains AS d ON d.id = r.domain_id "
        "ORDER BY d.name, r.name, r.type, r.content, r.ttl, r.prio, r.disabled"
    ).fetchall()
    auxiliary_count = connection.execute(
        "SELECT "
        "(SELECT COUNT(*) FROM supermasters) + "
        "(SELECT COUNT(*) FROM comments) + "
        "(SELECT COUNT(*) FROM domainmetadata) + "
        "(SELECT COUNT(*) FROM cryptokeys) + "
        "(SELECT COUNT(*) FROM tsigkeys) + "
        "(SELECT COUNT(*) FROM records WHERE domain_id IS NULL OR "
        "domain_id NOT IN (SELECT id FROM domains))"
    ).fetchone()[0]
finally:
    connection.close()
if (
    query_only != (1,)
    or quick_check != [("ok",)]
    or journal_mode != ("wal",)
    or domains != expected_domains
    or records != expected_records
    or auxiliary_count != 0
):
    raise SystemExit("external PowerDNS database differs from the scenario")
try:
    os.lstat(journal_path)
except FileNotFoundError:
    pass
except OSError as exc:
    raise SystemExit(
        f"reinspect external PowerDNS rollback journal after query: {exc}"
    ) from exc
else:
    raise SystemExit(
        "external PowerDNS query created a rollback journal"
    )
try:
    database_after = os.lstat(database_file["path"])
except OSError as exc:
    raise SystemExit(f"reinspect external PowerDNS database: {exc}") from exc
wal_after = inspect_sidecar(
    wal_path,
    "external PowerDNS write-ahead log after query",
    required_size=0,
    require_empty=True,
)
shm_after = inspect_sidecar(
    shm_path,
    "external PowerDNS shared memory after query",
    required_size=32768,
    require_empty=False,
)
if (
    identity(database_after) != identity(database_status)
    or identity(wal_after) != identity(wal)
    or identity(shm_after) != identity(shm)
):
    raise SystemExit(
        "external PowerDNS database or sidecar identity changed while sealing the preimage"
    )

receipts = {
    "dns_engine_state": {
        "path": state_dir + "/dns-engine-state.json",
        "status": "absent",
    },
    "dns_engine_switch_journal": {
        "path": state_dir + "/dns-engine-switch-journal.json",
        "status": "absent",
    },
    "bind_engine_ownership": {
        "path": state_dir + "/dns-engine-ownership-bind.json",
        "status": "absent",
    },
    "bind_install_ownership": {
        "path": state_dir + "/dns-engine-install-ownership-bind.json",
        "status": "absent",
    },
    "pdns_engine_ownership": {
        "path": state_dir + "/dns-engine-ownership-pdns.json",
        "status": "absent",
    },
    "pdns_install_ownership": {
        "path": state_dir + "/dns-engine-install-ownership-pdns.json",
        "status": "absent",
    },
}
for receipt in receipts.values():
    if os.path.lexists(receipt["path"]):
        raise SystemExit("external PowerDNS preimage has a production DNS receipt")

value = {
    "schema": "celikpanel/dns-kill-external-pdns-adoption-preimage/v1",
    "cell_id": cell_id,
    "scope": "external-pdns-measured-adoption-preimage",
    "source_fixture": "external-pdns-adoption",
    "construction_origin": "harness-external-pdns",
    "production_adoption_driver": "pdns-adopt",
    "production_adoption_pending": True,
    "scenario_sha256": hashlib.sha256(scenario_raw).hexdigest(),
    "source_preinstall_proof_path": preinstall_path,
    "source_preinstall_proof_sha256": preinstall_sha,
    "source_packages": preinstall["source_packages"],
    "main_config": main,
    "managed_config": managed,
    "cluster_config": {
        "path": "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
        "status": "absent",
    },
    "database": {
        **database_file,
        "schema_path": schema["path"],
        "schema_sha256": schema["sha256"],
        "quick_check": "ok",
        "journal_mode": "wal",
        "zone_snapshot_sha256": canonical_hash(zones),
        "domain_count": len(domains),
        "record_count": len(records),
        "auxiliary_authority_count": auxiliary_count,
        "sidecars": sidecars,
    },
    "source_unit_before_tagged_agent": {
        "name": "pdns.service",
        "load_state": "loaded",
        "active_state": "active",
        "sub_state": "running",
        "unit_file_state": "enabled",
    },
    "authoritative_preflight": {
        "claimed": True,
        "address": address,
        "port": 53,
        "name": "www.s1-kill.test",
        "type": "A",
        "udp": True,
        "tcp": True,
    },
    "production_receipts_absent": receipts,
}
with open(output_path, "w", encoding="utf-8", newline="\n") as handle:
    json.dump(value, handle, indent=2, sort_keys=True)
    handle.write("\n")
    handle.flush()
    os.fsync(handle.fileno())
PY
    mv -T --no-clobber "$temporary" "$EXTERNAL_PDNS_PREIMAGE_PROOF" || { rm -f "$temporary"; die "external PowerDNS preimage proof already exists"; }
    require_regular "$EXTERNAL_PDNS_PREIMAGE_PROOF"
    [[ $(stat -Lc '%U:%G:%a' "$EXTERNAL_PDNS_PREIMAGE_PROOF") == root:root:600 ]] || die "external PowerDNS preimage proof metadata mismatch"
    python3 - "$EXTERNAL_PDNS_PREIMAGE_PROOF" <<'PY'
import json, sys

raw = open(sys.argv[1], "rb").read()
value = json.loads(raw)
expected_keys = {
    "schema", "cell_id", "scope", "source_fixture", "construction_origin",
    "production_adoption_driver", "production_adoption_pending",
    "scenario_sha256", "source_preinstall_proof_path",
    "source_preinstall_proof_sha256", "source_packages", "main_config",
    "managed_config", "cluster_config", "database",
    "source_unit_before_tagged_agent", "authoritative_preflight",
    "production_receipts_absent",
}
if set(value) != expected_keys:
    raise SystemExit("external PowerDNS preimage fields differ")
if value["schema"] != "celikpanel/dns-kill-external-pdns-adoption-preimage/v1":
    raise SystemExit("external PowerDNS preimage schema differs")
if value["production_adoption_pending"] is not True:
    raise SystemExit("external PowerDNS preimage does not prove pending adoption")
canonical = (json.dumps(value, indent=2, sort_keys=True) + "\n").encode()
if raw != canonical:
    raise SystemExit("external PowerDNS preimage is not canonical sorted JSON")
PY
    dns_probe "$address" www.s1-kill.test
    sync -f "$EXTERNAL_PDNS_PREIMAGE_PROOF" "$FIXTURE_DIR"
}

write_source_adoption_proof() {
    local cell_id=$1 server_version=$2 sqlite_version=$3
    local setup_scenario_sha=$4 setup_identity_sha=$5
    local main_sha=$6 managed_sha=$7 schema_sha=$8 database_sha=$9
    local main_owner=${10} state_sha=${11}
    [[ ! -e $SOURCE_ADOPTION_PROOF && ! -L $SOURCE_ADOPTION_PROOF ]] || die "source adoption proof already exists"
    local pdns_uid pdns_gid sidecars_json temporary
    pdns_uid=$(/usr/bin/id -u pdns)
    pdns_gid=$(/usr/bin/id -g pdns)
    sidecars_json=$(PDNS_EXPECTED_UID=$pdns_uid PDNS_EXPECTED_GID=$pdns_gid python3 - <<'PY'
# SOURCE_ADOPTION_SIDECAR_CAPTURE
import json
import os
import stat

database_path = "/var/lib/powerdns/pdns.sqlite3"
uid_text = os.environ["PDNS_EXPECTED_UID"]
gid_text = os.environ["PDNS_EXPECTED_GID"]
if not uid_text.isascii() or not uid_text.isdigit() or str(int(uid_text)) != uid_text:
    raise SystemExit("pdns uid is not canonical decimal")
if not gid_text.isascii() or not gid_text.isdigit() or str(int(gid_text)) != gid_text:
    raise SystemExit("pdns gid is not canonical decimal")
expected_uid = int(uid_text)
expected_gid = int(gid_text)
if expected_uid <= 0 or expected_gid <= 0:
    raise SystemExit("pdns owner identity is outside the safe range")
nofollow = getattr(os, "O_NOFOLLOW", 0)
if not nofollow:
    raise SystemExit("O_NOFOLLOW is unavailable for source sidecar capture")


def identity(value):
    return (
        value.st_dev,
        value.st_ino,
        value.st_mode,
        value.st_nlink,
        value.st_uid,
        value.st_gid,
        value.st_size,
    )


def inspect(path, label, required_size=None, require_empty=False):
    try:
        before = os.lstat(path)
    except OSError as exc:
        raise SystemExit(f"inspect {label}: {exc}") from exc
    if stat.S_ISLNK(before.st_mode) or not stat.S_ISREG(before.st_mode):
        raise SystemExit(f"{label} is not a regular non-symlink file")
    try:
        descriptor = os.open(
            path, os.O_RDONLY | os.O_CLOEXEC | os.O_NONBLOCK | nofollow
        )
    except OSError as exc:
        raise SystemExit(f"open {label}: {exc}") from exc
    try:
        opened = os.fstat(descriptor)
        if not stat.S_ISREG(opened.st_mode):
            raise SystemExit(f"{label} is not a regular non-symlink file")
        if identity(before) != identity(opened):
            raise SystemExit(f"{label} changed while opening")
        if opened.st_uid != expected_uid or opened.st_gid != expected_gid:
            raise SystemExit(f"{label} has the wrong owner")
        if stat.S_IMODE(opened.st_mode) != 0o640:
            raise SystemExit(f"{label} has the wrong mode")
        if opened.st_nlink != 1:
            raise SystemExit(f"{label} must have exactly one link")
        if required_size is not None and opened.st_size != required_size:
            raise SystemExit(f"{label} has the wrong size")
        if require_empty and os.read(descriptor, 1) != b"":
            raise SystemExit(f"{label} must be empty")
        try:
            after_path = os.lstat(path)
        except OSError as exc:
            raise SystemExit(f"reinspect {label}: {exc}") from exc
        final = os.fstat(descriptor)
        if identity(after_path) != identity(opened) or identity(final) != identity(opened):
            raise SystemExit(f"{label} changed while inspecting")
        return final
    finally:
        os.close(descriptor)


database = inspect(database_path, "PowerDNS database")
if database.st_size <= 0:
    raise SystemExit("PowerDNS database is empty")
journal_path = database_path + "-journal"
try:
    os.lstat(journal_path)
except FileNotFoundError:
    pass
except OSError as exc:
    raise SystemExit(f"inspect PowerDNS rollback journal: {exc}") from exc
else:
    raise SystemExit("PowerDNS rollback journal must be absent")
wal_path = database_path + "-wal"
shm_path = database_path + "-shm"
wal = inspect(wal_path, "PowerDNS write-ahead log", required_size=0, require_empty=True)
shm = inspect(shm_path, "PowerDNS shared memory", required_size=32768)
if wal.st_dev != database.st_dev or shm.st_dev != database.st_dev:
    raise SystemExit("PowerDNS sidecar device differs from the database")
if len({database.st_ino, wal.st_ino, shm.st_ino}) != 3:
    raise SystemExit("PowerDNS database and sidecar inodes conflict")


def metadata(path, value, content_policy):
    return {
        "path": path,
        "file_type": "regular",
        "owner": "pdns:pdns",
        "mode": "0640",
        "link_count": value.st_nlink,
        "device": value.st_dev,
        "inode": value.st_ino,
        "size": value.st_size,
        "content_policy": content_policy,
    }


value = {
    "rollback_journal": {"path": journal_path, "status": "absent"},
    "write_ahead_log": metadata(wal_path, wal, "empty"),
    "shared_memory": metadata(shm_path, shm, "volatile-unhashed"),
}
print(json.dumps(value, separators=(",", ":"), sort_keys=True))
PY
)
    temporary=$(mktemp "$FIXTURE_DIR/.source-adoption.XXXXXXXX")
    chmod 0600 "$temporary"
    SOURCE_ADOPTION_CELL_ID=$cell_id PDNS_SERVER_VERSION=$server_version PDNS_SQLITE_VERSION=$sqlite_version SETUP_SCENARIO_SHA=$setup_scenario_sha SETUP_IDENTITY_SHA=$setup_identity_sha PDNS_MAIN_SHA=$main_sha PDNS_MANAGED_SHA=$managed_sha PDNS_SCHEMA_SHA=$schema_sha PDNS_DATABASE_SHA=$database_sha PDNS_MAIN_OWNER=$main_owner PDNS_STATE_SHA=$state_sha PDNS_SIDECARS_JSON=$sidecars_json python3 - "$temporary" <<'PY'
# SOURCE_ADOPTION_PROOF_RENDERER
import json, os, sys

value = {
    "schema": "celikpanel/dns-kill-source-adoption/v2",
    "cell_id": os.environ["SOURCE_ADOPTION_CELL_ID"],
    "scope": "external-pdns-source-for-production-adoption-before-bind",
    "construction_origin": "harness-external-pdns",
    "production_adoption_driver": "pdns-adopt",
    "source_setup_scenario_sha256": os.environ["SETUP_SCENARIO_SHA"],
    "source_setup_identity_receipt_sha256": os.environ["SETUP_IDENTITY_SHA"],
    "source_packages": [
        {"name": "pdns-backend-sqlite3", "status": "install ok installed", "version": os.environ["PDNS_SQLITE_VERSION"]},
        {"name": "pdns-server", "status": "install ok installed", "version": os.environ["PDNS_SERVER_VERSION"]},
    ],
    "measured_target_packages": [{"name": "bind9", "status": "absent"}],
    "main_config": {
        "path": "/etc/powerdns/pdns.conf",
        "sha256": os.environ["PDNS_MAIN_SHA"],
        "owner": os.environ["PDNS_MAIN_OWNER"],
        "mode": "0640",
    },
    "managed_config": {
        "path": "/etc/powerdns/pdns.d/celikpanel.conf",
        "sha256": os.environ["PDNS_MANAGED_SHA"],
        "owner": "root:root",
        "mode": "0644",
    },
    "cluster_config": {
        "path": "/etc/powerdns/pdns.d/celikpanel-cluster.conf",
        "status": "absent",
    },
    "database": {
        "path": "/var/lib/powerdns/pdns.sqlite3",
        "sha256": os.environ["PDNS_DATABASE_SHA"],
        "owner": "pdns:pdns",
        "mode": "0640",
        "schema_path": "/usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql",
        "schema_sha256": os.environ["PDNS_SCHEMA_SHA"],
        "quick_check": "ok",
        "sidecars": json.loads(os.environ["PDNS_SIDECARS_JSON"]),
    },
    "source_unit_after_adoption": {
        "name": "pdns.service",
        "load_state": "loaded",
        "active_state": "active",
        "sub_state": "running",
        "unit_file_state": "enabled",
    },
    "production_receipts": {
        "state_sha256": os.environ["PDNS_STATE_SHA"],
        "active_ownership_sha256": os.environ["PDNS_STATE_SHA"],
        "source_install_ownership_absent": True,
        "measured_target_ownership_absent": True,
        "measured_target_install_ownership_absent": True,
        "switch_journal_absent": True,
    },
    "external_artifacts_unchanged_by_adoption": True,
}
with open(sys.argv[1], "w", encoding="utf-8", newline="\n") as handle:
    json.dump(value, handle, indent=2, sort_keys=True)
    handle.write("\n")
    handle.flush()
    os.fsync(handle.fileno())
PY
    mv -T --no-clobber "$temporary" "$SOURCE_ADOPTION_PROOF" || { rm -f "$temporary"; die "source adoption proof already exists"; }
    require_regular "$SOURCE_ADOPTION_PROOF"
    [[ $(stat -Lc '%U:%G:%a' "$SOURCE_ADOPTION_PROOF") == root:root:600 ]] || die "source adoption proof metadata mismatch"
    sync -f "$SOURCE_ADOPTION_PROOF" "$FIXTURE_DIR"
}

validate_normalized_pdns_source() {
    local cell_id=$1 address=$2 state_sha=$3
    require_regular "$SOURCE_NORMALIZATION_IDENTITY"
    [[ $(stat -Lc '%U:%G:%a' "$SOURCE_NORMALIZATION_IDENTITY") == root:root:600 ]] \
        || die "source normalization identity receipt metadata mismatch"
    require_regular "$STATE_DIR/dns-engine-state.json"
    [[ $(sha256sum "$STATE_DIR/dns-engine-state.json" | cut -d' ' -f1) == "$state_sha" ]] \
        || die "PowerDNS normalization changed the adopted engine state"
    require_regular "$STATE_DIR/dns-engine-ownership-pdns.json"
    cmp -s "$STATE_DIR/dns-engine-state.json" "$STATE_DIR/dns-engine-ownership-pdns.json" \
        || die "PowerDNS normalization changed active source ownership"
    [[ ! -e $STATE_DIR/dns-engine-install-ownership-pdns.json && ! -L $STATE_DIR/dns-engine-install-ownership-pdns.json ]] \
        || die "PowerDNS normalization created source install ownership"
    [[ ! -e $STATE_DIR/dns-engine-ownership-bind.json && ! -L $STATE_DIR/dns-engine-ownership-bind.json ]] \
        || die "PowerDNS normalization created target ownership"
    [[ ! -e $STATE_DIR/dns-engine-install-ownership-bind.json && ! -L $STATE_DIR/dns-engine-install-ownership-bind.json ]] \
        || die "PowerDNS normalization created target install ownership"
    [[ ! -e $STATE_DIR/dns-engine-switch-journal.json && ! -L $STATE_DIR/dns-engine-switch-journal.json ]] \
        || die "PowerDNS normalization created a switch journal"
    [[ ! -e /etc/powerdns/pdns.d/celikpanel-cluster.conf && ! -L /etc/powerdns/pdns.d/celikpanel-cluster.conf ]] \
        || die "PowerDNS normalization created cluster configuration"
    systemctl is-active --quiet pdns.service \
        || die "PowerDNS normalization did not leave the source active"
    dns_probe "$address" www.s1-kill.test
    python3 - "$SCENARIO_FILE" "$SOURCE_NORMALIZATION_IDENTITY" \
        /var/lib/powerdns/pdns.sqlite3 "$cell_id" <<'PY'
import json
import sqlite3
import sys

scenario_path, identity_path, database_path, cell_id = sys.argv[1:]
with open(scenario_path, encoding="utf-8") as handle:
    scenario = json.load(handle)
with open(identity_path, encoding="utf-8") as handle:
    identity = json.load(handle)
if set(identity) != {
    "schema", "cell_id", "driver", "source_fixture", "base_request_id",
    "source_engine", "source_epoch", "configure", "zone_syncs",
}:
    raise SystemExit("source normalization identity fields differ")
if (
    identity["schema"] != "celikpanel-dns-kill-matrix-pdns-normalization-identity/v1"
    or identity["cell_id"] != cell_id
    or identity["driver"] != "bind"
    or identity["source_fixture"] != "managed-pdns"
    or identity["source_engine"] != "pdns"
    or identity["source_epoch"] != scenario.get("source_epoch")
):
    raise SystemExit("source normalization identity envelope differs")
configure = identity.get("configure")
if not isinstance(configure, dict) or configure.get("method") != "Agent.ConfigurePowerDNSSQLite":
    raise SystemExit("source normalization configure identity differs")
zones = scenario.get("zones")
zone_syncs = identity.get("zone_syncs")
if not isinstance(zones, list) or not zones or not isinstance(zone_syncs, list) or len(zone_syncs) != len(zones):
    raise SystemExit("source normalization zone set differs")
expected_rows = []
for zone, operation in zip(zones, zone_syncs, strict=True):
    qualifier = operation.get("qualifier")
    if (
        not isinstance(qualifier, str)
        or not qualifier.startswith("dns-zone-sync/v3:sha256:")
        or len(qualifier) != len("dns-zone-sync/v3:sha256:") + 64
        or any(character not in "0123456789abcdef" for character in qualifier.removeprefix("dns-zone-sync/v3:sha256:"))
    ):
        raise SystemExit("source normalization zone qualifier is invalid")
    expected = {
        "domain": zone.get("domain"),
        "engine": "pdns",
        "engine_epoch": scenario.get("source_epoch"),
        "request_id": operation.get("request_id"),
        "owner_id": operation.get("owner_id"),
        "qualifier": qualifier,
        "desired_generation": zone.get("desired_generation"),
        "action": "delete" if zone.get("delete") else "sync",
        "zone_type": zone.get("zone_type"),
        "schema": "dns-zone-sync/v3",
    }
    if (
        operation.get("method") != "Agent.SyncDNSZoneV3"
        or operation.get("domain") != expected["domain"]
        or operation.get("target") != expected["domain"]
        or operation.get("qualifier") != expected["qualifier"]
        or operation.get("package_name") != expected["qualifier"]
        or operation.get("engine") != "pdns"
        or operation.get("engine_epoch") != expected["engine_epoch"]
        or operation.get("desired_generation") != expected["desired_generation"]
        or operation.get("delete") != zone.get("delete")
        or operation.get("zone_type") != expected["zone_type"]
    ):
        raise SystemExit("source normalization zone identity differs")
    expected_rows.append(expected)
connection = sqlite3.connect(f"file:{database_path}?mode=ro", uri=True)
try:
    if connection.execute("PRAGMA quick_check").fetchall() != [("ok",)]:
        raise SystemExit("normalized PowerDNS database quick_check failed")
    tables = {
        row[0]
        for row in connection.execute(
            "SELECT name FROM sqlite_schema WHERE type='table'"
        )
    }
    required = {
        "celikpanel_dns_zone_sync_receipts",
        "celikpanel_dns_zone_sync_v3_receipts",
        "celikpanel_dns_engine_manifest_receipt",
    }
    if not required.issubset(tables):
        raise SystemExit("normalized PowerDNS private schema is incomplete")
    if connection.execute(
        "SELECT COUNT(*) FROM celikpanel_dns_zone_sync_receipts"
    ).fetchone() != (0,):
        raise SystemExit("normalized PowerDNS legacy receipt table is not empty")
    if connection.execute(
        "SELECT COUNT(*) FROM celikpanel_dns_engine_manifest_receipt"
    ).fetchone() != (0,):
        raise SystemExit("normalized PowerDNS manifest receipt table is not empty")
    columns = (
        "domain", "engine", "engine_epoch", "request_id", "owner_id", "qualifier",
        "desired_generation", "action", "zone_type", "schema",
    )
    rows = [
        dict(zip(columns, row, strict=True))
        for row in connection.execute(
            "SELECT domain, engine, engine_epoch, request_id, owner_id, qualifier, "
            "desired_generation, action, zone_type, schema "
            "FROM celikpanel_dns_zone_sync_v3_receipts ORDER BY domain"
        )
    ]
    if rows != sorted(expected_rows, key=lambda value: value["domain"]):
        raise SystemExit("normalized PowerDNS V3 receipts differ from the source snapshot")
finally:
    connection.close()
PY
}

write_source_proof() {
    local source_fixture=$1 cell_id=$2 address=$3 source_revision=$4
    local state_sha= state_json=null state_path= serving=false engine= epoch=0
    local setup_scenario_sha=absent setup_identity_sha=absent
    local source_preinstall_path=absent source_preinstall_sha=absent
    local source_adoption_path=absent source_adoption_sha=absent
    local external_pdns_preimage_path=absent external_pdns_preimage_sha=absent
    local source_normalization_path=absent source_normalization_sha=absent
    local measured_scenario_sha
    measured_scenario_sha=$(sha256sum "$SCENARIO_FILE" | cut -d' ' -f1)
    if [[ $source_fixture == managed-pdns ]]; then
        require_regular "$STATE_DIR/dns-engine-state.json"
        state_sha=$(sha256sum "$STATE_DIR/dns-engine-state.json" | cut -d' ' -f1)
        state_path=$STATE_DIR/dns-engine-state.json
        state_json=$(python3 -c 'import json,sys; print(json.dumps(json.load(open(sys.argv[1])),separators=(",",":")))' \
            "$STATE_DIR/dns-engine-state.json")
        serving=true
        engine=pdns
        epoch=1
        require_regular "$SOURCE_SETUP_FILE"
        require_regular "$SOURCE_SETUP_IDENTITY"
        require_regular "$SOURCE_PREINSTALL_PROOF"
        require_regular "$SOURCE_ADOPTION_PROOF"
        require_regular "$SOURCE_NORMALIZATION_IDENTITY"
        setup_scenario_sha=$(sha256sum "$SOURCE_SETUP_FILE" | cut -d' ' -f1)
        setup_identity_sha=$(sha256sum "$SOURCE_SETUP_IDENTITY" | cut -d' ' -f1)
        source_preinstall_path=$SOURCE_PREINSTALL_PROOF
        source_preinstall_sha=$(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1)
        source_adoption_path=$SOURCE_ADOPTION_PROOF
        source_adoption_sha=$(sha256sum "$SOURCE_ADOPTION_PROOF" | cut -d' ' -f1)
        source_normalization_path=$SOURCE_NORMALIZATION_IDENTITY
        source_normalization_sha=$(sha256sum "$SOURCE_NORMALIZATION_IDENTITY" | cut -d' ' -f1)
    elif [[ $source_fixture == external-pdns-adoption ]]; then
        serving=true
        engine=pdns
        epoch=0
        require_regular "$SOURCE_PREINSTALL_PROOF"
        require_regular "$EXTERNAL_PDNS_PREIMAGE_PROOF"
        source_preinstall_path=$SOURCE_PREINSTALL_PROOF
        source_preinstall_sha=$(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1)
        external_pdns_preimage_path=$EXTERNAL_PDNS_PREIMAGE_PROOF
        external_pdns_preimage_sha=$(sha256sum "$EXTERNAL_PDNS_PREIMAGE_PROOF" | cut -d' ' -f1)
    fi
    local temporary
    temporary=$(mktemp "$FIXTURE_DIR/.source-proof.XXXXXXXX")
    chmod 0600 "$temporary"
    SOURCE_FIXTURE=$source_fixture CELL_ID=$cell_id ADDRESS=$address \
    SOURCE_REVISION=$source_revision STATE_SHA=$state_sha STATE_JSON=$state_json \
    STATE_PATH=$state_path \
    SERVING=$serving ENGINE=$engine EPOCH=$epoch \
    MEASURED_SCENARIO_SHA=$measured_scenario_sha \
    SETUP_SCENARIO_SHA=$setup_scenario_sha SETUP_IDENTITY_SHA=$setup_identity_sha \
    SOURCE_PREINSTALL_PATH=$source_preinstall_path SOURCE_PREINSTALL_SHA=$source_preinstall_sha \
    SOURCE_ADOPTION_PATH=$source_adoption_path SOURCE_ADOPTION_SHA=$source_adoption_sha \
    EXTERNAL_PDNS_PREIMAGE_PATH=$external_pdns_preimage_path \
    EXTERNAL_PDNS_PREIMAGE_SHA=$external_pdns_preimage_sha \
    SOURCE_NORMALIZATION_PATH=$source_normalization_path SOURCE_NORMALIZATION_SHA=$source_normalization_sha \
        python3 - "$temporary" <<'PY'
import json, os, sys
value = {
    "schema": "celikpanel/dns-kill-source-proof/v1",
    "cell_id": os.environ["CELL_ID"],
    "source_fixture": os.environ["SOURCE_FIXTURE"],
    "engine": os.environ["ENGINE"],
    "engine_epoch": int(os.environ["EPOCH"]),
    "source_revision": int(os.environ["SOURCE_REVISION"]),
    "serving_before_tagged_agent": os.environ["SERVING"] == "true",
    "scenario_sha256": os.environ["MEASURED_SCENARIO_SHA"],
    "identity_receipt_path": "/var/lib/celikpanel-dns-kill-matrix/measured/trigger-identity.json",
    "identity_receipt_preexisting": False,
    "engine_state_receipt_path": os.environ["STATE_PATH"],
    "engine_state_receipt_sha256": os.environ["STATE_SHA"],
    "engine_state_identity": None if os.environ["STATE_JSON"] == "null" else json.loads(os.environ["STATE_JSON"]),
    "authoritative_preflight": {
        "claimed": os.environ["SERVING"] == "true",
        "address": os.environ["ADDRESS"],
        "port": 53,
        "name": "www.s1-kill.test",
        "type": "A",
        "udp": os.environ["SERVING"] == "true",
        "tcp": os.environ["SERVING"] == "true",
    },
    "uninitialized_global_port53": {
        "udp_bindable": os.environ["SOURCE_FIXTURE"] == "uninitialized",
        "tcp_bindable": os.environ["SOURCE_FIXTURE"] == "uninitialized",
        "authoritative_answer_observed": False,
    },
    "receipt_origin": (
        "production-pdns-adopt-normalized"
        if os.environ["SOURCE_FIXTURE"] == "managed-pdns"
        else "harness-external-pdns-preimage"
        if os.environ["SOURCE_FIXTURE"] == "external-pdns-adoption"
        else "absent-by-proof"
    ),
    "source_setup_scenario_sha256": os.environ["SETUP_SCENARIO_SHA"],
    "source_setup_identity_receipt_sha256": os.environ["SETUP_IDENTITY_SHA"],
    "source_preinstall_proof_path": os.environ["SOURCE_PREINSTALL_PATH"],
    "source_preinstall_proof_sha256": os.environ["SOURCE_PREINSTALL_SHA"],
    "source_adoption_proof_path": os.environ["SOURCE_ADOPTION_PATH"],
    "source_adoption_proof_sha256": os.environ["SOURCE_ADOPTION_SHA"],
    "external_pdns_preimage_path": os.environ["EXTERNAL_PDNS_PREIMAGE_PATH"],
    "external_pdns_preimage_sha256": os.environ["EXTERNAL_PDNS_PREIMAGE_SHA"],
    "source_normalization_identity_receipt_path": os.environ["SOURCE_NORMALIZATION_PATH"],
    "source_normalization_identity_receipt_sha256": os.environ["SOURCE_NORMALIZATION_SHA"],
}
with open(sys.argv[1], "w", encoding="utf-8") as handle:
    json.dump(value, handle, indent=2, sort_keys=True)
    handle.write("\n")
    handle.flush()
    os.fsync(handle.fileno())
PY
    mv -T --no-clobber "$temporary" "$SOURCE_PROOF_FILE" \
        || { rm -f "$temporary"; die "source proof already exists"; }
    sync -f "$SOURCE_PROOF_FILE" "$FIXTURE_DIR"
}

write_controller_argv() {
    local cell_id=$1 address=$2
    local output=$FIXTURE_DIR/controller-argv.json
    local result_dir=$FIXTURE_DIR/results/$cell_id
    [[ ! -e $output && ! -L $output ]] \
        || die "controller argv already exists"
    install -d -m 0700 -o root -g root "$FIXTURE_DIR/results" "$result_dir"
    [[ -z $(find "$result_dir" -mindepth 1 -maxdepth 1 -print -quit) ]] \
        || die "controller result directory is not empty"
    local request_id nonce temporary
    request_id=$(printf '%s\0measured-request' "$cell_id" | sha256sum | cut -c1-32)
    nonce=$(printf '%s\0fault-nonce' "$cell_id" | sha256sum | cut -c1-64)
    temporary=$(mktemp "$FIXTURE_DIR/.controller-argv.XXXXXXXX")
    chmod 0600 "$temporary"
    CELL_ID=$cell_id DNS_ADDRESS=$address REQUEST_ID=$request_id NONCE=$nonce \
    RESULT_DIR=$result_dir python3 - "$temporary" <<'PY'
import json, os, sys

fixture = "/var/lib/celikpanel-dns-kill-matrix"
state = "/var/lib/celikpanel-agent-private"
scenario = fixture + "/scenario.json"
identity = fixture + "/measured/trigger-identity.json"
trigger = [
    "/opt/celikpanel/bin/dns-kill-trigger", "rpc-switch",
    "--scenario", scenario,
    "--identity-receipt", identity,
    "--timeout", "45m",
]
retry = list(trigger)
retry[1] = "rpc-retry"
probe = [
    "/opt/celikpanel/libexec/dns-kill-recovery-probe.py",
    "--cell-id", os.environ["CELL_ID"],
    "--scenario", scenario,
    "--identity-receipt", identity,
    "--ledger", state + "/service-mutations.json",
    "--state", state + "/dns-engine-state.json",
    "--journal", state + "/dns-engine-switch-journal.json",
]
compact = lambda value: json.dumps(value, separators=(",", ":"))
result = os.environ["RESULT_DIR"]
argv = [
    "/opt/celikpanel/libexec/dns-kill-run-cell.py",
    "--manifest", fixture + "/manifest.json",
    "--cell-id", os.environ["CELL_ID"],
    "--request-id", os.environ["REQUEST_ID"],
    "--nonce", os.environ["NONCE"],
    "--tagged-agent-command", compact(["/opt/celikpanel/bin/agent.kill"]),
    "--trigger-mode", "socket",
    "--trigger-command", compact(trigger),
    "--recovery-command", compact(retry),
    "--source-proof", fixture + "/source-proof.json",
    "--agent-restart-command",
    compact(["/usr/bin/systemctl", "restart", "celikpanel-agent.service"]),
    "--panel-restart-command",
    compact(["/usr/bin/systemctl", "restart", "celikpanel-panel.service"]),
    "--recovery-probe-command", compact(probe),
    "--command-cwd", "/",
    "--state-dir", state,
    "--mutation-lock", "/run/celikpanel/service-mutation.lock",
    "--agent-socket", "/run/celikpanel/agent.sock",
    "--agent-token-file", "/etc/celikpanel/agent.token",
    "--journal", state + "/dns-engine-switch-journal.json",
    "--marker", result + "/boundary-marker.json",
    "--proof", result + "/kill-proof.json",
    "--result", result + "/result.json",
    "--transcript", result + "/transcript.jsonl",
    "--dns-address", os.environ["DNS_ADDRESS"],
    "--dns-port", "53",
    "--dns-name", "www.s1-kill.test",
    "--dns-type", "A",
    "--panel-address", "127.0.0.1",
    "--panel-port", "2083",
    "--startup-timeout", "60",
    "--boundary-timeout", "2700",
    "--stop-timeout", "15",
    "--kill-timeout", "15",
    "--command-timeout", "2700",
    "--recovery-timeout", "2700",
    "--endpoint-timeout", "60",
    "--dns-timeout", "5",
    "--stability-seconds", "30",
    "--stability-interval", "1",
]
with open(sys.argv[1], "w", encoding="utf-8") as handle:
    json.dump(argv, handle, indent=2)
    handle.write("\n")
    handle.flush()
    os.fsync(handle.fileno())
PY
    mv -T --no-clobber "$temporary" "$output" \
        || { rm -f "$temporary"; die "controller argv already exists"; }
    sync -f "$output" "$result_dir" "$FIXTURE_DIR/results" "$FIXTURE_DIR"
}

prepare_bind() {
    local cell_id=$1 node=$2 boundary_phase=$3 source_fixture=$4
    local source_fixture_policy=$5 stage=$6
    require_simple_value "cell id" "$cell_id" '^[a-z0-9][a-z0-9_.-]{0,239}$'
    require_simple_value "boundary phase" "$boundary_phase" \
        '^(pre-intent|intent|target-staged|source-stopped|target-started|target-verified|committed|rolling-back|rolled-back)$'
    [[ $source_fixture == uninitialized || $source_fixture == managed-pdns ]] \
        || die "unsupported BIND source fixture: $source_fixture"
    array_contains "$source_fixture_policy" "${SOURCE_FIXTURE_POLICIES[@]}" \
        || die "unsupported BIND source fixture policy: $source_fixture_policy"
    verify_fixture_identity "$cell_id" "$node"
    verify_os "$node"
    require_regular "$stage/scenario.json"
    install -m 0600 -o root -g root "$stage/scenario.json" "$SCENARIO_FILE"
    install -d -m 0700 -o root -g root "$MEASURED_IDENTITY_DIR"
    [[ ! -e $MEASURED_IDENTITY && ! -L $MEASURED_IDENTITY ]] \
        || die "measured trigger identity receipt must not preexist"

    local address
    address=$(global_ipv4)
    if [[ $source_fixture == uninitialized ]]; then
        array_contains "$boundary_phase" "${EARLY_UNINITIALIZED_PHASES[@]}" \
            || die "uninitialized source cannot claim a stopped-source-or-later boundary"
        [[ $source_fixture_policy == driver-specific ||
           $source_fixture_policy == uninitialized-permitted-noncritical ]] \
            || die "uninitialized source fixture policy is incompatible"
        assert_no_source_engine "$address"
        write_source_proof uninitialized "$cell_id" "$address" 0
    else
        [[ $node == debian13 ]] \
            || die "managed PowerDNS source can only be established on certified Debian 13"
        array_contains "$boundary_phase" "${CRITICAL_MANAGED_PDNS_PHASES[@]}" \
            || die "managed PowerDNS source preinstall is restricted to critical BIND boundaries"
        [[ $source_fixture_policy == managed-pdns-required ]] \
            || die "managed PowerDNS source fixture policy is incompatible"
        require_regular "$stage/source-setup-pdns.json"
        install -m 0600 -o root -g root "$stage/source-setup-pdns.json" "$SOURCE_SETUP_FILE"
        assert_no_source_engine "$address"
        local preinstall_proof_sha
        preinstall_pdns_source_packages "$cell_id" "$address" bind
        preinstall_proof_sha=$(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1)
        create_external_pdns_source "$address" "$SOURCE_SETUP_FILE"
        local main_sha_before managed_sha_before schema_sha database_sha_before
        local main_owner_before main_identity_before managed_identity_before database_identity_before
        main_sha_before=$(sha256sum /etc/powerdns/pdns.conf | cut -d' ' -f1)
        managed_sha_before=$(sha256sum /etc/powerdns/pdns.d/celikpanel.conf | cut -d' ' -f1)
        schema_sha=$(sha256sum /usr/share/pdns-backend-sqlite3/schema/schema.sqlite3.sql | cut -d' ' -f1)
        database_sha_before=$(sha256sum /var/lib/powerdns/pdns.sqlite3 | cut -d' ' -f1)
        main_owner_before=$(stat -Lc '%U:%G' /etc/powerdns/pdns.conf)
        main_identity_before=$(stat -Lc '%d:%i:%f:%u:%g:%s:%Y:%Z' /etc/powerdns/pdns.conf)
        managed_identity_before=$(stat -Lc '%d:%i:%f:%u:%g:%s:%Y:%Z' /etc/powerdns/pdns.d/celikpanel.conf)
        database_identity_before=$(stat -Lc '%d:%i:%f:%u:%g:%s:%Y:%Z' /var/lib/powerdns/pdns.sqlite3)
        local setup_request
        setup_request=$(printf '%s\0source-pdns-adopt' "$cell_id" | sha256sum | cut -c1-32)
        CELIKPANEL_S1_DRIVER=pdns-adopt CELIKPANEL_S1_CELL_ID=$cell_id CELIKPANEL_S1_REQUEST_ID=$setup_request CELIKPANEL_AGENT_SOCKET=$AGENT_SOCKET CELIKPANEL_AGENT_TOKEN_FILE=$TOKEN_FILE /opt/celikpanel/bin/dns-kill-trigger rpc-switch --scenario "$SOURCE_SETUP_FILE" --identity-receipt "$SOURCE_SETUP_IDENTITY" --timeout 45m
        require_regular "$SOURCE_SETUP_IDENTITY"
        require_regular "$SOURCE_PREINSTALL_PROOF"
        [[ $(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1) == "$preinstall_proof_sha" ]] || die "source preinstall proof changed during production PowerDNS adoption"
        [[ $(stat -Lc '%U:%G:%a' "$SOURCE_SETUP_IDENTITY") == root:root:600 ]] || die "source setup trigger identity receipt metadata mismatch"
        python3 - "$SOURCE_SETUP_FILE" "$SOURCE_SETUP_IDENTITY" "$STATE_DIR/dns-engine-state.json" "$cell_id" <<'PY'
import json, sys
scenario, identity, state = (json.load(open(path, encoding="utf-8")) for path in sys.argv[1:4])
cell_id = sys.argv[4]
if identity.get("schema") != "celikpanel-dns-kill-matrix-trigger-identity/v1":
    raise SystemExit("source setup trigger identity schema mismatch")
if identity.get("cell_id") != cell_id or identity.get("driver") != "pdns-adopt":
    raise SystemExit("source setup trigger identity route mismatch")
if identity.get("source_fixture") != "external-pdns-adoption" or scenario.get("source_fixture") != "external-pdns-adoption":
    raise SystemExit("source setup fixture provenance mismatch")
if scenario.get("mode") != "adopt" or scenario.get("source_engine") != "":
    raise SystemExit("source setup scenario is not an unresolved-source adoption")
for state_key, identity_key in (
    ("mutation_request_id", "request_id"),
    ("mutation_owner_id", "owner_id"),
    ("manifest_qualifier", "manifest_qualifier"),
):
    if state.get(state_key) != identity.get(identity_key):
        raise SystemExit("source engine state is not bound to source setup trigger identity")
PY
        systemctl restart celikpanel-agent.service
        wait_for_socket
        [[ ! -e $STATE_DIR/dns-engine-switch-journal.json && ! -L $STATE_DIR/dns-engine-switch-journal.json ]] || die "PowerDNS adoption did not reconcile its committed journal"
        require_regular "$STATE_DIR/dns-engine-state.json"
        python3 - "$STATE_DIR/dns-engine-state.json" <<'PY'
import json, sys
value = json.load(open(sys.argv[1], encoding='utf-8'))
if value.get('mode') != 'adopt' or value.get('engine') != 'pdns' or value.get('engine_epoch') != 1 or value.get('source_revision') != 0:
    raise SystemExit('managed PowerDNS state receipt has the wrong source identity')
PY
        require_regular "$STATE_DIR/dns-engine-ownership-pdns.json"
        cmp -s "$STATE_DIR/dns-engine-state.json" "$STATE_DIR/dns-engine-ownership-pdns.json" || die "adopted PowerDNS ownership differs from active source state"
        [[ ! -e $STATE_DIR/dns-engine-install-ownership-pdns.json && ! -L $STATE_DIR/dns-engine-install-ownership-pdns.json ]] || die "adopted PowerDNS source gained install ownership"
        [[ ! -e /etc/systemd/system/pdns.service && ! -L /etc/systemd/system/pdns.service ]] || die "adopted PowerDNS source retained the source-preinstall mask"
        require_apt_package_absent bind9
        [[ ! -e $STATE_DIR/dns-engine-ownership-bind.json && ! -L $STATE_DIR/dns-engine-ownership-bind.json ]] || die "adopted PowerDNS source has an unclaimed BIND ownership receipt"
        [[ ! -e $STATE_DIR/dns-engine-install-ownership-bind.json && ! -L $STATE_DIR/dns-engine-install-ownership-bind.json ]] || die "adopted PowerDNS source has an unclaimed BIND install receipt"
        systemctl is-active --quiet pdns.service || die "production adoption did not leave PowerDNS active"
        dns_probe "$address" www.s1-kill.test
        [[ $(sha256sum /etc/powerdns/pdns.conf | cut -d' ' -f1) == "$main_sha_before" ]] || die "PowerDNS main config changed during adoption"
        [[ $(sha256sum /etc/powerdns/pdns.d/celikpanel.conf | cut -d' ' -f1) == "$managed_sha_before" ]] || die "PowerDNS managed config changed during adoption"
        [[ $(sha256sum /var/lib/powerdns/pdns.sqlite3 | cut -d' ' -f1) == "$database_sha_before" ]] || die "PowerDNS database changed during adoption"
        [[ $(stat -Lc '%d:%i:%f:%u:%g:%s:%Y:%Z' /etc/powerdns/pdns.conf) == "$main_identity_before" ]] || die "PowerDNS main config identity changed during adoption"
        [[ $(stat -Lc '%d:%i:%f:%u:%g:%s:%Y:%Z' /etc/powerdns/pdns.d/celikpanel.conf) == "$managed_identity_before" ]] || die "PowerDNS managed config identity changed during adoption"
        [[ $(stat -Lc '%d:%i:%f:%u:%g:%s:%Y:%Z' /var/lib/powerdns/pdns.sqlite3) == "$database_identity_before" ]] || die "PowerDNS database identity changed during adoption"
        local setup_scenario_sha setup_identity_sha server_version sqlite_version state_sha
        setup_scenario_sha=$(sha256sum "$SOURCE_SETUP_FILE" | cut -d' ' -f1)
        setup_identity_sha=$(sha256sum "$SOURCE_SETUP_IDENTITY" | cut -d' ' -f1)
        server_version=$(LC_ALL=C /usr/bin/dpkg-query -W -f='${Version}' -- pdns-server)
        sqlite_version=$(LC_ALL=C /usr/bin/dpkg-query -W -f='${Version}' -- pdns-backend-sqlite3)
        state_sha=$(sha256sum "$STATE_DIR/dns-engine-state.json" | cut -d' ' -f1)
        write_source_adoption_proof "$cell_id" "$server_version" "$sqlite_version" "$setup_scenario_sha" "$setup_identity_sha" "$main_sha_before" "$managed_sha_before" "$schema_sha" "$database_sha_before" "$main_owner_before" "$state_sha"
        local normalization_request
        normalization_request=$(printf '%s\0source-pdns-normalize' "$cell_id" | sha256sum | cut -c1-32)
        CELIKPANEL_S1_DRIVER=bind CELIKPANEL_S1_CELL_ID=$cell_id CELIKPANEL_S1_REQUEST_ID=$normalization_request CELIKPANEL_AGENT_SOCKET=$AGENT_SOCKET CELIKPANEL_AGENT_TOKEN_FILE=$TOKEN_FILE /opt/celikpanel/bin/dns-kill-trigger rpc-normalize-pdns --scenario "$SCENARIO_FILE" --normalization-receipt "$SOURCE_NORMALIZATION_IDENTITY" --timeout 45m
        validate_normalized_pdns_source "$cell_id" "$address" "$state_sha"
        write_source_proof managed-pdns "$cell_id" "$address" 0
    fi
    write_controller_argv "$cell_id" "$address"

    # The measured child is launched manually by run_cell.py. Stop only the
    # CelikPanel coordinators; a genuine DNS source intentionally remains up.
    systemctl stop celikpanel-panel.service celikpanel-agent.service
    local agent_stop_evidence panel_stop_evidence
    agent_stop_evidence=$(inactive_unit_evidence celikpanel-agent.service)
    panel_stop_evidence=$(inactive_unit_evidence celikpanel-panel.service)
    remove_verified_stale_agent_socket "$agent_stop_evidence" "$panel_stop_evidence"
    if [[ $source_fixture == managed-pdns ]]; then
        systemctl is-active --quiet pdns.service \
            || die "PowerDNS source stopped with the coordinators"
        dns_probe "$address" www.s1-kill.test
    fi
    sync -f "$SCENARIO_FILE" "$SOURCE_PROOF_FILE" "$COORDINATOR_STOP_PROOF" \
        "$FIXTURE_DIR" "$STATE_DIR"
    local source_preinstall_path= source_preinstall_sha=absent
    local source_adoption_path= source_adoption_sha=absent
    local source_normalization_path= source_normalization_sha=absent
    if [[ $source_fixture == managed-pdns ]]; then
        source_preinstall_path=$SOURCE_PREINSTALL_PROOF
        source_preinstall_sha=$(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1)
        source_adoption_path=$SOURCE_ADOPTION_PROOF
        source_adoption_sha=$(sha256sum "$SOURCE_ADOPTION_PROOF" | cut -d' ' -f1)
        source_normalization_path=$SOURCE_NORMALIZATION_IDENTITY
        source_normalization_sha=$(sha256sum "$SOURCE_NORMALIZATION_IDENTITY" | cut -d' ' -f1)
    fi
    printf '{"scenario":"%s","source_proof":"%s","source_preinstall_proof":"%s","source_preinstall_sha256":"%s","source_adoption_proof":"%s","source_adoption_sha256":"%s","source_normalization_identity_receipt":"%s","source_normalization_identity_receipt_sha256":"%s","coordinator_stop_proof":"%s","controller_argv":"%s","dns_address":"%s","dns_name":"www.s1-kill.test","controller_identity":"root:celikpanel"}\n' "$SCENARIO_FILE" "$SOURCE_PROOF_FILE" "$source_preinstall_path" "$source_preinstall_sha" "$source_adoption_path" "$source_adoption_sha" "$source_normalization_path" "$source_normalization_sha" "$COORDINATOR_STOP_PROOF" "$FIXTURE_DIR/controller-argv.json" "$address"
}

prepare_pdns_adopt() {
    local cell_id=$1 node=$2 boundary_phase=$3 source_fixture=$4
    local source_fixture_policy=$5 stage=$6
    require_simple_value "cell id" "$cell_id" '^[a-z0-9][a-z0-9_.-]{0,239}$'
    require_simple_value "boundary phase" "$boundary_phase" '^(pre-intent|intent|target-verified|committed|rolling-back|rolled-back)$'
    [[ $node == debian13 ]] || die "PowerDNS adoption can only run on certified Debian 13"
    [[ $source_fixture == external-pdns-adoption ]] || die "PowerDNS adoption requires the external-pdns-adoption fixture"
    [[ $source_fixture_policy == driver-specific ]] || die "PowerDNS adoption requires the driver-specific source policy"
    verify_fixture_identity "$cell_id" "$node"
    verify_os "$node"
    require_regular "$stage/scenario.json"
    install -m 0600 -o root -g root "$stage/scenario.json" "$SCENARIO_FILE"
    install -d -m 0700 -o root -g root "$MEASURED_IDENTITY_DIR"
    [[ ! -e $MEASURED_IDENTITY && ! -L $MEASURED_IDENTITY ]] || die "measured trigger identity receipt must not preexist"

    local address preinstall_sha preimage_sha
    address=$(global_ipv4)
    assert_no_source_engine "$address"
    preinstall_pdns_source_packages "$cell_id" "$address" pdns-adopt
    preinstall_sha=$(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1)
    create_external_pdns_source "$address" "$SCENARIO_FILE"
    write_external_pdns_preimage_proof "$cell_id" "$address" "$preinstall_sha"
    preimage_sha=$(sha256sum "$EXTERNAL_PDNS_PREIMAGE_PROOF" | cut -d' ' -f1)
    write_source_proof external-pdns-adoption "$cell_id" "$address" 0
    write_controller_argv "$cell_id" "$address"

    # The external PowerDNS instance is the measured adoption preimage. Stop
    # only CelikPanel's coordinators; never run an untagged setup adoption RPC.
    systemctl stop celikpanel-panel.service celikpanel-agent.service
    local agent_stop_evidence panel_stop_evidence
    agent_stop_evidence=$(inactive_unit_evidence celikpanel-agent.service)
    panel_stop_evidence=$(inactive_unit_evidence celikpanel-panel.service)
    remove_verified_stale_agent_socket "$agent_stop_evidence" "$panel_stop_evidence"
    systemctl is-active --quiet pdns.service || die "external PowerDNS source stopped with the coordinators"
    dns_probe "$address" www.s1-kill.test
    [[ $(sha256sum "$SOURCE_PREINSTALL_PROOF" | cut -d' ' -f1) == "$preinstall_sha" ]] || die "source preinstall proof changed after external source construction"
    [[ $(sha256sum "$EXTERNAL_PDNS_PREIMAGE_PROOF" | cut -d' ' -f1) == "$preimage_sha" ]] || die "external PowerDNS preimage proof changed after coordinator stop"
    [[ ! -e $STATE_DIR/dns-engine-state.json && ! -L $STATE_DIR/dns-engine-state.json ]] || die "measured adoption preimage gained a production engine-state receipt"
    [[ ! -e $STATE_DIR/dns-engine-switch-journal.json && ! -L $STATE_DIR/dns-engine-switch-journal.json ]] || die "measured adoption preimage gained a production switch journal"
    local engine receipt
    for engine in bind pdns; do
        for receipt in "$STATE_DIR/dns-engine-ownership-$engine.json" "$STATE_DIR/dns-engine-install-ownership-$engine.json"
        do
            [[ ! -e $receipt && ! -L $receipt ]] || die "measured adoption preimage gained production ownership: $receipt"
        done
    done
    sync -f "$SCENARIO_FILE" "$SOURCE_PROOF_FILE" "$SOURCE_PREINSTALL_PROOF" "$EXTERNAL_PDNS_PREIMAGE_PROOF" "$COORDINATOR_STOP_PROOF" "$FIXTURE_DIR" "$STATE_DIR"
    printf '{"scenario":"%s","source_proof":"%s","source_preinstall_proof":"%s","source_preinstall_sha256":"%s","external_pdns_preimage":"%s","external_pdns_preimage_sha256":"%s","source_adoption_proof":"","source_normalization_identity_receipt":"","coordinator_stop_proof":"%s","controller_argv":"%s","dns_address":"%s","dns_name":"www.s1-kill.test","controller_identity":"root:celikpanel"}\n' "$SCENARIO_FILE" "$SOURCE_PROOF_FILE" "$SOURCE_PREINSTALL_PROOF" "$preinstall_sha" "$EXTERNAL_PDNS_PREIMAGE_PROOF" "$preimage_sha" "$COORDINATOR_STOP_PROOF" "$FIXTURE_DIR/controller-argv.json" "$address"
}

case ${1:-} in
    install)
        [[ $# -eq 5 ]] || die "install expects STAGE MANIFEST_SHA CELL_ID NODE"
        install_bundle "$2" "$3" "$4" "$5"
        ;;
    prepare-bind)
        [[ $# -eq 7 ]] || die "prepare-bind expects CELL_ID NODE PHASE SOURCE_FIXTURE SOURCE_FIXTURE_POLICY STAGE"
        prepare_bind "$2" "$3" "$4" "$5" "$6" "$7"
        ;;
    prepare-pdns-adopt)
        [[ $# -eq 7 ]] || die "prepare-pdns-adopt expects CELL_ID NODE PHASE SOURCE_FIXTURE SOURCE_FIXTURE_POLICY STAGE"
        prepare_pdns_adopt "$2" "$3" "$4" "$5" "$6" "$7"
        ;;
    *) die "usage: guest_bootstrap.sh {install|prepare-bind|prepare-pdns-adopt} ..." ;;
esac
