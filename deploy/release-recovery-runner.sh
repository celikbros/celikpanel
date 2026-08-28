#!/usr/bin/env bash
# Recover only an already-published, durable CelikPanel release transaction.
# This helper never creates a transaction and never removes a marker itself.
set -Eeuo pipefail

PATH=/usr/sbin:/usr/bin:/sbin:/bin
export PATH
umask 077

TRANSACTION_ROOT=/var/lib/celikpanel-release-transaction
RELEASES_ROOT=/var/backups/celikpanel/releases
SNAPSHOT_ROOT=/var/backups/celikpanel/update-snapshots
TRUST_ANCHOR=/
EXPECTED_UID=0
EXPECTED_GID=0
TEST_ROOT=
RECOVERY_RUNNER=/usr/libexec/celikpanel/release-recovery
RECOVERY_SERVICE=/etc/systemd/system/celikpanel-release-recovery.service
RECOVERY_TIMER=/etc/systemd/system/celikpanel-release-recovery.timer
START_GUARD=/usr/libexec/celikpanel/release-transaction-start-guard
AGENT_DROPIN=/etc/systemd/system/celikpanel-agent.service.d/10-release-transaction-guard.conf
PANEL_DROPIN=/etc/systemd/system/celikpanel-panel.service.d/10-release-transaction-guard.conf
FOUNDATION_MANIFEST=/var/lib/celikpanel-release-state/recovery-foundation.v1
SYSTEMCTL_BIN=/usr/bin/systemctl
VERIFY_FINAL_STATE=0
EXPECTED_FINAL_VERSION=
EXPECTED_FINAL_COMMIT=
EXPECTED_FINAL_SEQUENCE=

case $# in
    0) ;;
    7)
        [[ $1 == --verify-final-state && $2 == --expected-version &&
           $4 == --expected-commit && $6 == --expected-sequence ]] || {
            echo '!! unsupported release recovery argument tuple' >&2
            exit 1
        }
        VERIFY_FINAL_STATE=1
        EXPECTED_FINAL_VERSION=$3
        EXPECTED_FINAL_COMMIT=$5
        EXPECTED_FINAL_SEQUENCE=$7
        ;;
    *)
        echo '!! release recovery accepts only its fixed recovery or final-proof arguments' >&2
        exit 1
        ;;
esac
if [[ $VERIFY_FINAL_STATE == 1 ]]; then
    [[ $EXPECTED_FINAL_VERSION =~ ^v[0-9A-Za-z.-]+$ &&
       $EXPECTED_FINAL_COMMIT =~ ^[0-9a-f]{40}$ &&
       $EXPECTED_FINAL_SEQUENCE =~ ^[1-9][0-9]*$ &&
       ${#EXPECTED_FINAL_SEQUENCE} -le 19 ]] || {
        echo '!! release recovery final-proof identity is invalid' >&2
        exit 1
    }
fi

# Tests exercise the real classifier and dispatcher in an isolated root.  The
# installed systemd unit has a fixed, empty environment and can never select it.
if [[ ${CELIKPANEL_RELEASE_RECOVERY_TESTING:-0} == 1 ]]; then
    TEST_ROOT=${CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT:-}
    [[ $TEST_ROOT == /* && $TEST_ROOT != / && $TEST_ROOT != *'/../'* &&
       $TEST_ROOT != */.. && -d $TEST_ROOT && ! -L $TEST_ROOT ]] \
        || { echo '!! unsafe release recovery test root' >&2; exit 1; }
    TEST_ROOT=$(readlink -e -- "$TEST_ROOT") \
        || { echo '!! cannot canonicalize release recovery test root' >&2; exit 1; }
    TRANSACTION_ROOT=$TEST_ROOT/var/lib/celikpanel-release-transaction
    RELEASES_ROOT=$TEST_ROOT/var/backups/celikpanel/releases
    SNAPSHOT_ROOT=$TEST_ROOT/var/backups/celikpanel/update-snapshots
    TRUST_ANCHOR=$TEST_ROOT
    EXPECTED_UID=$(id -u)
    EXPECTED_GID=$(id -g)
    RECOVERY_RUNNER=$TEST_ROOT$RECOVERY_RUNNER
    RECOVERY_SERVICE=$TEST_ROOT$RECOVERY_SERVICE
    RECOVERY_TIMER=$TEST_ROOT$RECOVERY_TIMER
    START_GUARD=$TEST_ROOT$START_GUARD
    AGENT_DROPIN=$TEST_ROOT$AGENT_DROPIN
    PANEL_DROPIN=$TEST_ROOT$PANEL_DROPIN
    FOUNDATION_MANIFEST=$TEST_ROOT$FOUNDATION_MANIFEST
    SYSTEMCTL_BIN=$TEST_ROOT/usr/bin/systemctl
fi
unset CELIKPANEL_RELEASE_RECOVERY_TESTING CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT
readonly PATH TRANSACTION_ROOT RELEASES_ROOT SNAPSHOT_ROOT TRUST_ANCHOR \
    EXPECTED_UID EXPECTED_GID TEST_ROOT RECOVERY_RUNNER RECOVERY_SERVICE \
    RECOVERY_TIMER START_GUARD AGENT_DROPIN PANEL_DROPIN FOUNDATION_MANIFEST \
    SYSTEMCTL_BIN VERIFY_FINAL_STATE EXPECTED_FINAL_VERSION \
    EXPECTED_FINAL_COMMIT EXPECTED_FINAL_SEQUENCE

die() {
    echo "!! $*" >&2
    exit 1
}

validate_trusted_directory_chain() {
    local path=$1 current parent canonical owner group mode permissions
    [[ $path == /* ]] || die "trusted path must be absolute: $path"
    current=$path
    while true; do
        [[ -d $current && ! -L $current ]] \
            || die "trusted directory is missing or symbolic: $current"
        canonical=$(readlink -e -- "$current") \
            || die "cannot canonicalize trusted directory: $current"
        [[ $canonical == "$current" ]] \
            || die "trusted directory contains an alias: $current"
        read -r owner group mode < <(stat -Lc '%u %g %a' -- "$current") \
            || die "cannot inspect trusted directory: $current"
        [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" ]] \
            || die "trusted directory has an unexpected owner: $current"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "trusted directory is group/other writable: $current"
        [[ $current == "$TRUST_ANCHOR" ]] && break
        parent=$(dirname -- "$current")
        [[ $parent != "$current" ]] \
            || die "trusted directory escaped its anchor: $path"
        case "$TRUST_ANCHOR" in
            /) [[ $parent == /* ]] || die "trusted directory escaped root: $path" ;;
            *) [[ $parent == "$TRUST_ANCHOR" || $parent == "$TRUST_ANCHOR/"* ]] \
                   || die "trusted directory escaped its test anchor: $path" ;;
        esac
        current=$parent
    done
}

validate_transaction_root_and_lock() {
    local owner group mode links size
    validate_trusted_directory_chain "$TRANSACTION_ROOT"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$TRANSACTION_ROOT") \
        || die 'cannot inspect release transaction root'
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" && $mode == 700 ]] \
        || die 'release transaction root must be owned by the trusted principal and mode 0700'
    [[ -f $TRANSACTION_ROOT/transaction.lock &&
       ! -L $TRANSACTION_ROOT/transaction.lock ]] \
        || die 'release transaction lock is missing or unsafe'
    read -r owner group mode links size \
        < <(stat -Lc '%u %g %a %h %s' -- "$TRANSACTION_ROOT/transaction.lock") \
        || die 'cannot inspect release transaction lock'
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" &&
       $mode == 600 && $links == 1 && $size == 0 ]] \
        || die 'release transaction lock metadata is unsafe'
}

acquire_transaction_lock() {
    local path_identity fd_identity
    [[ ! -e "/proc/$BASHPID/fd/9" ]] \
        || die 'release recovery fixed transaction descriptor is already open'
    path_identity=$(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/transaction.lock") \
        || die 'cannot identify release transaction lock'
    exec 9<>"$TRANSACTION_ROOT/transaction.lock"
    TRANSACTION_FD=9
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/9") \
        || die 'cannot identify opened release transaction lock'
    [[ $path_identity == "$fd_identity" ]] \
        || die 'release transaction lock changed while opening'
    if ! flock -n -x "$TRANSACTION_FD"; then
        exec 9>&-
        unset TRANSACTION_FD
        return 1
    fi
}

verify_held_transaction_lock() {
    local path_identity fd_identity probe_fd probe_rc
    local label sequence kind advisory access remainder
    local -a lock_rows=()
    path_identity=$(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/transaction.lock") ||
        die 'cannot identify fixed transaction lock path'
    fd_identity=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$TRANSACTION_FD") ||
        die 'cannot identify held transaction lock descriptor'
    [[ $path_identity == "$fd_identity" ]] ||
        die 'held descriptor does not name the fixed transaction lock'
    mapfile -t lock_rows < <(grep '^lock:' "/proc/$BASHPID/fdinfo/$TRANSACTION_FD" 2>/dev/null || true)
    [[ ${#lock_rows[@]} -eq 1 ]] || die 'held descriptor has no exact single flock record'
    read -r label sequence kind advisory access remainder <<< "${lock_rows[0]}"
    [[ $label == lock: && $sequence =~ ^[0-9]+:$ && $kind == FLOCK &&
       $advisory == ADVISORY && $access == WRITE && -n $remainder ]] ||
        die 'held descriptor flock record is not exclusive'
    exec {probe_fd}<>"$TRANSACTION_ROOT/transaction.lock" ||
        die 'cannot open independent transaction lock probe'
    probe_rc=0
    flock -n -x "$probe_fd" || probe_rc=$?
    exec {probe_fd}>&-
    [[ $probe_rc -eq 1 ]] || die 'held transaction lock is not independently exclusive'
}

release_transaction_lock() {
    flock -u "$TRANSACTION_FD" || die 'cannot release transaction lock'
    exec 9>&-
    unset TRANSACTION_FD
}

read_canonical_marker() {
    local marker_name=$1 marker owner group mode links size token operation snapshot
    local -a lines=()
    marker=$TRANSACTION_ROOT/$marker_name
    [[ -f $marker && ! -L $marker ]] \
        || die "transaction marker is missing or unsafe: $marker_name"
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$marker") \
        || die "cannot inspect transaction marker: $marker_name"
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" &&
       $mode == 600 && $links == 1 && $size -gt 0 && $size -le 512 ]] \
        || die "transaction marker metadata is unsafe: $marker_name"
    mapfile -t lines < "$marker"
    [[ ${#lines[@]} -eq 4 && ${lines[0]} == version=1 ]] \
        || die "transaction marker is not canonical: $marker_name"
    token=${lines[1]#token=}
    operation=${lines[2]#operation=}
    snapshot=${lines[3]#snapshot=}
    [[ ${lines[1]} == "token=$token" && $token =~ ^[0-9a-f]{64}$ ]] \
        || die "transaction marker token is malformed: $marker_name"
    [[ ${lines[2]} == "operation=$operation" &&
       ( $operation == update || $operation == rollback ) ]] \
        || die "transaction marker operation is malformed: $marker_name"
    [[ ${lines[3]} == "snapshot=$snapshot" &&
       $snapshot =~ ^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$ ]] \
        || die "transaction marker snapshot is malformed: $marker_name"
    cmp -s -- "$marker" <(printf 'version=1\ntoken=%s\noperation=%s\nsnapshot=%s\n' \
        "$token" "$operation" "$snapshot") \
        || die "transaction marker bytes are not canonical: $marker_name"
    MARKER_TOKEN=$token
    MARKER_OPERATION=$operation
    MARKER_SNAPSHOT=$snapshot
}

classify_transaction() {
    local entry name marker_count=0
    local quiesce=0 active=0 completion=0 scheduler=0
    while IFS= read -r -d '' entry; do
        name=$(basename -- "$entry")
        case "$name" in
            transaction.lock) ;;
            quiesce.pending) quiesce=1; marker_count=$((marker_count + 1)) ;;
            active) active=1; marker_count=$((marker_count + 1)) ;;
            completion.pending) completion=1; marker_count=$((marker_count + 1)) ;;
            scheduler-restore.pending) scheduler=1; marker_count=$((marker_count + 1)) ;;
            *) die "unexpected release transaction entry: $name" ;;
        esac
    done < <(find "$TRANSACTION_ROOT" -xdev -mindepth 1 -maxdepth 1 -print0)

    if [[ $marker_count -eq 0 ]]; then
        TRANSACTION_PHASE=none
        MARKER_TOKEN=
        MARKER_OPERATION=
        MARKER_SNAPSHOT=
        return 0
    fi
    if [[ $quiesce -eq 1 && $marker_count -eq 1 ]]; then
        TRANSACTION_PHASE=quiesce
        read_canonical_marker quiesce.pending
        [[ $MARKER_OPERATION == update ]] \
            || die 'quiesce recovery is defined only for updates'
        return 0
    fi
    if [[ $active -eq 1 && $marker_count -eq 1 ]]; then
        TRANSACTION_PHASE=active
        read_canonical_marker active
        return 0
    fi
    if [[ $completion -eq 1 && $marker_count -eq 1 ]]; then
        TRANSACTION_PHASE=completion
        read_canonical_marker completion.pending
        return 0
    fi
    if [[ $completion -eq 1 && $scheduler -eq 1 && $marker_count -eq 2 ]]; then
        TRANSACTION_PHASE=completion-scheduler
        cmp -s -- "$TRANSACTION_ROOT/completion.pending" \
            "$TRANSACTION_ROOT/scheduler-restore.pending" \
            || die 'completion and scheduler markers do not describe the same transaction'
        read_canonical_marker completion.pending
        return 0
    fi
    if [[ $scheduler -eq 1 && $marker_count -eq 1 ]]; then
        TRANSACTION_PHASE=scheduler
        read_canonical_marker scheduler-restore.pending
        return 0
    fi
    die 'durable release transaction topology is ambiguous'
}

validate_immutable_release() {
    local root=$1 expected_commit=$2 canonical relative entry owner group mode links permissions
    local version commit tree
    canonical=$(readlink -e -- "$root") || die 'retained recovery release is unavailable'
    [[ $canonical == "$root" && $canonical == "$RELEASES_ROOT/"* ]] \
        || die 'retained recovery release is outside immutable storage'
    relative=${canonical#"$RELEASES_ROOT/"}
    [[ $relative =~ ^[0-9a-f]{12}-[0-9a-f]{24}$ &&
       ${relative:0:12} == "${expected_commit:0:12}" ]] \
        || die 'retained recovery release basename does not match the transaction target'
    validate_trusted_directory_chain "$canonical"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$canonical") \
        || die 'cannot inspect retained recovery release root'
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" && $mode == 700 ]] \
        || die 'retained recovery release root metadata is unsafe'
    if find "$canonical" -xdev -type l -print -quit | grep -q .; then
        die 'retained recovery release contains a symbolic link'
    fi
    if find "$canonical" -xdev ! -type d ! -type f -print -quit | grep -q .; then
        die 'retained recovery release contains a special filesystem object'
    fi
    while IFS= read -r -d '' entry; do
        read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$entry") \
            || die "cannot inspect retained recovery release entry: $entry"
        [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" ]] \
            || die "retained recovery release entry has an unexpected owner: $entry"
        permissions=$((8#$mode))
        (( (permissions & 0022) == 0 )) \
            || die "retained recovery release entry is group/other writable: $entry"
        [[ -d $entry || $links == 1 ]] \
            || die "retained recovery release file has multiple links: $entry"
    done < <(find "$canonical" -xdev -mindepth 1 -print0)
    [[ ! -e $canonical/.git && ! -L $canonical/.git ]] \
        || die 'retained recovery release is a mutable checkout'
    [[ -f $canonical/SHA256SUMS && ! -L $canonical/SHA256SUMS ]] \
        || die 'retained recovery release checksum manifest is missing'
    (
        cd "$canonical"
        LC_ALL=C find . -xdev -type f ! -path './SHA256SUMS' -print0 \
            | LC_ALL=C sort -z \
            | xargs -0 sha256sum \
            | cmp -s - SHA256SUMS
    ) || die 'retained recovery release checksum verification failed'
    for entry in release.version release.commit release.tree install.sh update.sh rollback.sh \
        bin/panel bin/agent deploy/release-transaction-guard.sh \
        deploy/release-transaction-start-guard.sh deploy/panel-tls-snapshot.sh \
        deploy/release-recovery-runner.sh deploy/release-recovery-foundation.sh \
        deploy/release-recovery.protocol deploy/release-sequence-policy \
        deploy/systemd/celikpanel-release-recovery.service \
        deploy/systemd/celikpanel-release-recovery.timer; do
        [[ -f $canonical/$entry && ! -L $canonical/$entry ]] \
            || die "retained recovery release is missing $entry"
    done
    cmp -s -- "$canonical/deploy/release-recovery.protocol" \
        <(printf '%s\n' format=celikpanel-release-recovery-protocol-v1 protocol=1) \
        || die 'retained recovery release protocol is unsupported or noncanonical'
    [[ -x $canonical/install.sh && -x $canonical/update.sh &&
       -x $canonical/rollback.sh && -x $canonical/bin/panel &&
       -x $canonical/bin/agent ]] \
        || die 'retained recovery release executables are unsafe'
    version=$(tr -d '[:space:]' < "$canonical/release.version")
    commit=$(tr -d '[:space:]' < "$canonical/release.commit")
    tree=$(tr -d '[:space:]' < "$canonical/release.tree")
    [[ $version == 1 && $commit == "$expected_commit" &&
       ( $tree =~ ^[0-9a-f]{40}$ || $tree =~ ^[0-9a-f]{64}$ ) ]] \
        || die 'retained recovery release provenance does not match the transaction'
    VALIDATED_RELEASE_TREE=$tree
    # SHA256SUMS was just regenerated from every payload byte and compared
    # exactly. Bind duplicate retained releases from that canonical manifest
    # without rereading the archive-sized payload a second or third time.
    VALIDATED_RELEASE_PAYLOAD_DIGEST=$(
        sed '/  \.\/release\.created-at-utc$/d' "$canonical/SHA256SUMS" | sha256sum
    ) || die 'cannot bind retained recovery release payload'
    VALIDATED_RELEASE_PAYLOAD_DIGEST=${VALIDATED_RELEASE_PAYLOAD_DIGEST%% *}
    [[ $VALIDATED_RELEASE_PAYLOAD_DIGEST =~ ^[0-9a-f]{64}$ ]] \
        || die 'retained recovery release payload digest is invalid'
}

find_exact_release() {
    local target_commit=$1 candidate selected= expected_tree= expected_payload=
    local -a candidates=()
    [[ -d $RELEASES_ROOT && ! -L $RELEASES_ROOT ]] \
        || die 'retained release storage is missing or unsafe'
    validate_trusted_directory_chain "$RELEASES_ROOT"
    while IFS= read -r -d '' candidate; do
        candidates+=("$candidate")
    done < <(find "$RELEASES_ROOT" -xdev -mindepth 1 -maxdepth 1 \
        -name "${target_commit:0:12}-*" -print0 | LC_ALL=C sort -z)
    [[ ${#candidates[@]} -gt 0 ]] \
        || die 'no retained release matches the transaction target'
    for candidate in "${candidates[@]}"; do
        [[ -d $candidate && ! -L $candidate ]] \
            || die 'matching retained release candidate is not a safe directory'
        validate_immutable_release "$candidate" "$target_commit"
        if [[ -z $selected ]]; then
            selected=$candidate
            expected_tree=$VALIDATED_RELEASE_TREE
            expected_payload=$VALIDATED_RELEASE_PAYLOAD_DIGEST
        else
            [[ $VALIDATED_RELEASE_TREE == "$expected_tree" &&
               $VALIDATED_RELEASE_PAYLOAD_DIGEST == "$expected_payload" ]] \
                || die 'retained releases for the target have conflicting payloads'
        fi
    done
    RECOVERY_RELEASE=$selected
}

read_foundation_manifest() {
    local owner group mode links size protocol_sha
    local -a lines=()
    validate_trusted_directory_chain "$(dirname -- "$FOUNDATION_MANIFEST")"
    [[ -f $FOUNDATION_MANIFEST && ! -L $FOUNDATION_MANIFEST ]] \
        || die 'recovery foundation manifest is missing or unsafe'
    read -r owner group mode links size \
        < <(stat -Lc '%u %g %a %h %s' -- "$FOUNDATION_MANIFEST") \
        || die 'cannot inspect recovery foundation manifest'
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" &&
       $mode == 600 && $links == 1 && $size -gt 0 && $size -le 2048 ]] \
        || die 'recovery foundation manifest metadata is unsafe'
    mapfile -t lines < "$FOUNDATION_MANIFEST"
    [[ ${#lines[@]} -eq 12 &&
       ${lines[0]} == format=celikpanel-release-recovery-foundation-v1 &&
       ${lines[1]} == protocol=1 &&
       ${lines[2]} =~ ^sequence=[1-9][0-9]*$ &&
       ${lines[3]} =~ ^release-version=v[0-9A-Za-z.-]+$ &&
       ${lines[4]} =~ ^release-commit=[0-9a-f]{40}$ &&
       ${lines[5]} =~ ^runner-sha256=[0-9a-f]{64}$ &&
       ${lines[6]} =~ ^service-sha256=[0-9a-f]{64}$ &&
       ${lines[7]} =~ ^timer-sha256=[0-9a-f]{64}$ &&
       ${lines[8]} =~ ^start-guard-sha256=[0-9a-f]{64}$ &&
       ${lines[9]} =~ ^agent-dropin-sha256=[0-9a-f]{64}$ &&
       ${lines[10]} =~ ^panel-dropin-sha256=[0-9a-f]{64}$ &&
       ${lines[11]} =~ ^protocol-sha256=[0-9a-f]{64}$ ]] \
        || die 'recovery foundation manifest identity is noncanonical'
    FOUNDATION_TARGET_SEQUENCE=${lines[2]#sequence=}
    FOUNDATION_TARGET_VERSION=${lines[3]#release-version=}
    FOUNDATION_TARGET_COMMIT=${lines[4]#release-commit=}
    FOUNDATION_RUNNER_SHA=${lines[5]#runner-sha256=}
    FOUNDATION_SERVICE_SHA=${lines[6]#service-sha256=}
    FOUNDATION_TIMER_SHA=${lines[7]#timer-sha256=}
    FOUNDATION_GUARD_SHA=${lines[8]#start-guard-sha256=}
    FOUNDATION_AGENT_DROPIN_SHA=${lines[9]#agent-dropin-sha256=}
    FOUNDATION_PANEL_DROPIN_SHA=${lines[10]#panel-dropin-sha256=}
    FOUNDATION_PROTOCOL_SHA=${lines[11]#protocol-sha256=}
    protocol_sha=$(printf '%s\n' format=celikpanel-release-recovery-protocol-v1 protocol=1 | sha256sum) ||
        die 'cannot identify the fixed recovery protocol'
    protocol_sha=${protocol_sha%% *}
    [[ $FOUNDATION_PROTOCOL_SHA == "$protocol_sha" ]] ||
        die 'foundation protocol digest is unsupported'
}

verify_installed_foundation_file() {
    local path=$1 expected_mode=$2 expected_sha=$3 owner group mode links actual_sha
    validate_trusted_directory_chain "$(dirname -- "$path")"
    [[ -f $path && ! -L $path ]] || die "installed foundation file is missing or unsafe: $path"
    read -r owner group mode links < <(stat -Lc '%u %g %a %h' -- "$path") ||
        die "cannot inspect installed foundation file: $path"
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" &&
       $mode == "${expected_mode#0}" && $links == 1 ]] ||
        die "installed foundation file metadata mismatch: $path"
    actual_sha=$(sha256sum -- "$path") || die "cannot hash installed foundation file: $path"
    actual_sha=${actual_sha%% *}
    [[ $actual_sha == "$expected_sha" ]] || die "installed foundation file digest mismatch: $path"
}

verify_installed_foundation_systemd() {
    local unit path expected reload dropins
    for unit in celikpanel-release-recovery.service celikpanel-release-recovery.timer; do
        [[ $("$SYSTEMCTL_BIN" show --property=LoadState --value "$unit") == loaded ]] ||
            die "$unit is not loaded"
        path=$("$SYSTEMCTL_BIN" show --property=FragmentPath --value "$unit") ||
            die "cannot inspect $unit fragment path"
        if [[ $unit == *.service ]]; then expected=$RECOVERY_SERVICE; else expected=$RECOVERY_TIMER; fi
        [[ $path == "$expected" ]] || die "$unit fragment path is not exact"
        reload=$("$SYSTEMCTL_BIN" show --property=NeedDaemonReload --value "$unit") ||
            die "cannot inspect $unit reload state"
        [[ $reload == no ]] || die "$unit has unconsumed on-disk changes"
        dropins=$("$SYSTEMCTL_BIN" show --property=DropInPaths --value "$unit") ||
            die "cannot inspect $unit drop-in paths"
        [[ -z $dropins ]] || die "$unit has an unverified systemd drop-in"
    done
    [[ $("$SYSTEMCTL_BIN" is-enabled celikpanel-release-recovery.service 2>/dev/null || true) == enabled &&
       $("$SYSTEMCTL_BIN" is-enabled celikpanel-release-recovery.timer 2>/dev/null || true) == enabled ]] ||
        die 'recovery units are not durably enabled'
    "$SYSTEMCTL_BIN" is-active --quiet celikpanel-release-recovery.timer ||
        die 'recovery timer is not active'
}

verify_installed_foundation_final() {
    [[ ! -e ${FOUNDATION_MANIFEST}.intent && ! -L ${FOUNDATION_MANIFEST}.intent ]] ||
        die 'foundation publication intent is still pending'
    read_foundation_manifest
    [[ $FOUNDATION_TARGET_VERSION == "$EXPECTED_FINAL_VERSION" &&
       $FOUNDATION_TARGET_COMMIT == "$EXPECTED_FINAL_COMMIT" &&
       $FOUNDATION_TARGET_SEQUENCE == "$EXPECTED_FINAL_SEQUENCE" ]] ||
        die 'foundation identity does not match the requested update target'
    verify_installed_foundation_file "$RECOVERY_RUNNER" 0755 "$FOUNDATION_RUNNER_SHA"
    verify_installed_foundation_file "$RECOVERY_SERVICE" 0644 "$FOUNDATION_SERVICE_SHA"
    verify_installed_foundation_file "$RECOVERY_TIMER" 0644 "$FOUNDATION_TIMER_SHA"
    verify_installed_foundation_file "$START_GUARD" 0755 "$FOUNDATION_GUARD_SHA"
    verify_installed_foundation_file "$AGENT_DROPIN" 0644 "$FOUNDATION_AGENT_DROPIN_SHA"
    verify_installed_foundation_file "$PANEL_DROPIN" 0644 "$FOUNDATION_PANEL_DROPIN_SHA"
    verify_installed_foundation_systemd
}

validate_snapshot_storage() {
    local owner group mode
    [[ -d $SNAPSHOT_ROOT && ! -L $SNAPSHOT_ROOT ]] \
        || die 'snapshot storage is missing or unsafe'
    validate_trusted_directory_chain "$SNAPSHOT_ROOT"
    read -r owner group mode < <(stat -Lc '%u %g %a' -- "$SNAPSHOT_ROOT") \
        || die 'cannot inspect snapshot storage'
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" && $mode == 700 ]] \
        || die 'snapshot storage metadata is unsafe'
}

read_transition_mode() {
    local snapshot=$1 transition stage nonce owner group mode links size value
    local -a stages=()
    if [[ -d $SNAPSHOT_ROOT/$snapshot && ! -L $SNAPSHOT_ROOT/$snapshot ]]; then
        transition=$SNAPSHOT_ROOT/$snapshot/snapshot-transition.state
    else
        nonce=${snapshot##*-}
        while IFS= read -r -d '' stage; do
            stages+=("$stage")
        done < <(find "$SNAPSHOT_ROOT" -xdev -mindepth 1 -maxdepth 1 \
            -name ".release-snapshot.incomplete.*.$nonce" -print0)
        [[ ${#stages[@]} -eq 1 && -d ${stages[0]} && ! -L ${stages[0]} ]] \
            || die 'exactly one durable staged snapshot is required for update recovery'
        transition=${stages[0]}/$snapshot/snapshot-transition.state
    fi
    validate_trusted_directory_chain "$(dirname -- "$transition")"
    RECOVERY_SNAPSHOT_DIR=$(dirname -- "$transition")
    [[ -f $transition && ! -L $transition ]] \
        || die 'snapshot transition state is missing or unsafe'
    read -r owner group mode links size < <(stat -Lc '%u %g %a %h %s' -- "$transition") \
        || die 'cannot inspect snapshot transition state'
    [[ $owner == "$EXPECTED_UID" && $group == "$EXPECTED_GID" &&
       $mode == 600 && $links == 1 && $size -gt 0 && $size -le 32 ]] \
        || die 'snapshot transition state metadata is unsafe'
    value=$(tr -d '\n' < "$transition")
    case "$value" in
        normal) UPDATE_MODE=--normal ;;
        pre-ledger) UPDATE_MODE=--bootstrap-pre-ledger ;;
        schema17) UPDATE_MODE=--bootstrap-schema17 ;;
        *) die 'snapshot transition state is not canonical' ;;
    esac
    printf '%s\n' "$value" | cmp -s - "$transition" \
        || die 'snapshot transition state has noncanonical bytes'
}

markers_still_present() {
    local marker
    for marker in quiesce.pending active completion.pending scheduler-restore.pending; do
        [[ ! -e $TRANSACTION_ROOT/$marker && ! -L $TRANSACTION_ROOT/$marker ]] \
            || return 0
    done
    return 1
}

state_is_active_like() {
    case "$1" in
        active|activating|reloading|refreshing) return 0 ;;
        *) return 1 ;;
    esac
}

verify_coordinator_result() {
    local unit=$1 expected=$2 actual
    actual=$("$SYSTEMCTL_BIN" show --property=ActiveState --value "$unit") ||
        die "cannot inspect recovered coordinator: $unit"
    if state_is_active_like "$expected"; then
        [[ $actual == active ]] ||
            die "recovered active-like coordinator is not active: $unit"
    else
        ! state_is_active_like "$actual" ||
            die "recovered inactive-like coordinator is unexpectedly active: $unit"
    fi
}

[[ $EUID -eq 0 ]] || die 'release recovery must run as root'

# Absence is the normal steady state.  Do not create coordination storage merely
# because the boot unit ran.
if [[ ! -e $TRANSACTION_ROOT && ! -L $TRANSACTION_ROOT ]]; then
    [[ $VERIFY_FINAL_STATE == 0 ]] ||
        die 'final-state proof requires the exact persistent transaction root and lock'
    exit 0
fi
validate_transaction_root_and_lock
if ! acquire_transaction_lock; then
    # A live updater owns the exact fixed flock. Never wait here: otherwise a
    # recovery job can delay the updater's controlled agent/panel starts. The
    # 30-second timer retries after the holder exits or is killed; future
    # transient workers additionally trigger this unit via OnFailure.
    [[ $VERIFY_FINAL_STATE == 0 ]] \
        || die 'final-state proof cannot acquire the exact free transaction lock'
    exit 0
fi
verify_held_transaction_lock
DISPATCH_ROOT_IDENTITY=$(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT") ||
    die 'cannot identify dispatch transaction root'
DISPATCH_LOCK_IDENTITY=$(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/transaction.lock") ||
    die 'cannot identify dispatch transaction lock'
DISPATCH_FD_IDENTITY=$(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$TRANSACTION_FD") ||
    die 'cannot identify dispatch transaction descriptor'
[[ $DISPATCH_LOCK_IDENTITY == "$DISPATCH_FD_IDENTITY" ]] ||
    die 'dispatch descriptor does not name the fixed transaction lock'
classify_transaction
if [[ $TRANSACTION_PHASE == none ]]; then
    if [[ $VERIFY_FINAL_STATE == 1 ]]; then
        verify_installed_foundation_final
        verify_coordinator_result celikpanel-agent.service active
        verify_coordinator_result celikpanel-panel.service active
        validate_transaction_root_and_lock
        [[ $(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT") == "$DISPATCH_ROOT_IDENTITY" &&
           $(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/transaction.lock") == "$DISPATCH_LOCK_IDENTITY" &&
           $(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$TRANSACTION_FD") == "$DISPATCH_FD_IDENTITY" ]] ||
            die 'final-state transaction identity changed during proof'
        verify_held_transaction_lock
        classify_transaction
        [[ $TRANSACTION_PHASE == none ]] || die 'a transaction marker appeared during final proof'
    fi
    release_transaction_lock
    exit 0
fi

snapshot_pattern='^([0-9]{8}T[0-9]{6}Z)-from-unknown-to-([0-9a-f]{40})-([0-9a-f]{32})$'
[[ $MARKER_SNAPSHOT =~ $snapshot_pattern ]] \
    || die 'transaction snapshot name does not bind a canonical target commit'
TARGET_COMMIT=${BASH_REMATCH[2]}
find_exact_release "$TARGET_COMMIT"
validate_snapshot_storage

ACTION=
UPDATE_MODE=
RECOVERY_SNAPSHOT_DIR=
case "$MARKER_OPERATION:$TRANSACTION_PHASE" in
    update:quiesce)
        [[ ! -e $SNAPSHOT_ROOT/$MARKER_SNAPSHOT &&
           ! -L $SNAPSHOT_ROOT/$MARKER_SNAPSHOT ]] \
            || die 'quiesce marker unexpectedly coexists with a final snapshot'
        read_transition_mode "$MARKER_SNAPSHOT"
        ACTION=update
        ;;
    update:active)
        if [[ -d $SNAPSHOT_ROOT/$MARKER_SNAPSHOT &&
              ! -L $SNAPSHOT_ROOT/$MARKER_SNAPSHOT ]]; then
            RECOVERY_SNAPSHOT_DIR=$SNAPSHOT_ROOT/$MARKER_SNAPSHOT
            ACTION=rollback
        else
            read_transition_mode "$MARKER_SNAPSHOT"
            ACTION=update
        fi
        ;;
    update:completion|update:completion-scheduler|update:scheduler)
        [[ -d $SNAPSHOT_ROOT/$MARKER_SNAPSHOT &&
           ! -L $SNAPSHOT_ROOT/$MARKER_SNAPSHOT ]] \
            || die 'completed update recovery requires its final snapshot'
        read_transition_mode "$MARKER_SNAPSHOT"
        ACTION=update
        ;;
    rollback:active|rollback:completion|rollback:completion-scheduler|rollback:scheduler)
        [[ -d $SNAPSHOT_ROOT/$MARKER_SNAPSHOT &&
           ! -L $SNAPSHOT_ROOT/$MARKER_SNAPSHOT ]] \
            || die 'rollback recovery requires its exact final snapshot'
        RECOVERY_SNAPSHOT_DIR=$SNAPSHOT_ROOT/$MARKER_SNAPSHOT
        ACTION=rollback
        ;;
    *) die 'transaction operation and durable phase cannot be recovered automatically' ;;
esac

[[ -n $RECOVERY_SNAPSHOT_DIR ]] ||
    die 'recovery did not bind an exact snapshot directory'
TRUSTED_RELEASE_ROOT=$RECOVERY_RELEASE
source "$RECOVERY_RELEASE/deploy/release-transaction-guard.sh"
source "$RECOVERY_RELEASE/deploy/release-recovery-foundation.sh"
release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$TRANSACTION_FD" ||
    die 'dispatch transaction lock proof failed'
release_txn_validate_service_states "$RECOVERY_SNAPSHOT_DIR/service-states.tsv" ||
    die 'recovery service-state ledger is invalid'
IFS=$'\t' read -r _ _ EXPECTED_AGENT_STATE _ \
    < "$RECOVERY_SNAPSHOT_DIR/service-states.tsv"
IFS=$'\t' read -r _ _ EXPECTED_PANEL_STATE _ \
    < <(sed -n '2p' "$RECOVERY_SNAPSHOT_DIR/service-states.tsv")
[[ -n $EXPECTED_AGENT_STATE && -n $EXPECTED_PANEL_STATE ]] ||
    die 'recovery coordinator expectations are missing'

# Keep fixed descriptor 9 and the same locked open file description
# continuously across dispatch.  The signed target updater/rollback entrypoint
# accepts only that descriptor and re-proves its inode, fdinfo flock ownership,
# independent exclusion and exact durable transaction tuple before recovery.
child_status=0
lock_identity=$(stat -Lc '%d:%i' -- \
    "/proc/$BASHPID/fd/$TRANSACTION_FD") \
    || die 'cannot identify held recovery transaction lock'
common_env=(
    PATH=/usr/sbin:/usr/bin:/sbin:/bin HOME=/root USER=root LOGNAME=root
    SHELL=/bin/bash LANG=C LC_ALL=C CELIKPANEL_RECOVER_EXISTING_TRANSACTION=1
    CELIKPANEL_RELEASE_TRANSACTION_FD=9
    CELIKPANEL_RECOVERY_LOCK_IDENTITY="$lock_identity"
    CELIKPANEL_RECOVERY_EXPECTED_TOKEN="$MARKER_TOKEN"
    CELIKPANEL_RECOVERY_EXPECTED_OPERATION="$MARKER_OPERATION"
    CELIKPANEL_RECOVERY_EXPECTED_SNAPSHOT="$MARKER_SNAPSHOT"
    CELIKPANEL_RECOVERY_EXPECTED_PHASE="$TRANSACTION_PHASE"
)
if [[ -n $TEST_ROOT ]]; then
    common_env+=(CELIKPANEL_RELEASE_RECOVERY_TEST_ROOT="$TEST_ROOT")
fi
if [[ $ACTION == update ]]; then
    env -i "${common_env[@]}" \
        CELIKPANEL_TRUSTED_RELEASE_ROOT="$RECOVERY_RELEASE" \
        CELIKPANEL_PREFLIGHT_PANEL="$RECOVERY_RELEASE/bin/panel" \
        CELIKPANEL_PREFLIGHT_AGENT="$RECOVERY_RELEASE/bin/agent" \
        /bin/bash "$RECOVERY_RELEASE/update.sh" "$UPDATE_MODE" || child_status=$?
else
    env -i "${common_env[@]}" /bin/bash "$RECOVERY_RELEASE/rollback.sh" \
        "$SNAPSHOT_ROOT/$MARKER_SNAPSHOT" || child_status=$?
fi

# The runner retained the same OFD while waiting for the child.
validate_transaction_root_and_lock
[[ $(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT") == "$DISPATCH_ROOT_IDENTITY" ]] ||
    die 'transaction root identity changed during recovery'
[[ $(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/transaction.lock") == "$DISPATCH_LOCK_IDENTITY" ]] ||
    die 'transaction lock path identity changed during recovery'
[[ $(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$TRANSACTION_FD") == "$DISPATCH_FD_IDENTITY" &&
   $DISPATCH_FD_IDENTITY == "$DISPATCH_LOCK_IDENTITY" ]] ||
    die 'transaction descriptor identity changed during recovery'
release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$TRANSACTION_FD" ||
    die 'final transaction flock proof failed'
verify_held_transaction_lock
if [[ $child_status -ne 0 ]]; then
    release_transaction_lock
    die "release recovery child failed with status $child_status"
fi
classify_transaction
if [[ $TRANSACTION_PHASE == none ]]; then
    release_recovery_verify_foundation "$RECOVERY_RELEASE" \
        "$RECOVERY_RUNNER" "$RECOVERY_SERVICE" "$RECOVERY_TIMER" \
        "$START_GUARD" "$AGENT_DROPIN" "$PANEL_DROPIN" \
        "$FOUNDATION_MANIFEST" "$SYSTEMCTL_BIN" ||
        die 'recovery foundation final proof failed'
    verify_coordinator_result celikpanel-agent.service "$EXPECTED_AGENT_STATE"
    verify_coordinator_result celikpanel-panel.service "$EXPECTED_PANEL_STATE"
    validate_transaction_root_and_lock
    [[ $(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT") == "$DISPATCH_ROOT_IDENTITY" ]] ||
        die 'transaction root identity changed during final recovery verification'
    [[ $(stat -Lc '%d:%i' -- "$TRANSACTION_ROOT/transaction.lock") == "$DISPATCH_LOCK_IDENTITY" ]] ||
        die 'transaction lock path identity changed during final recovery verification'
    [[ $(stat -Lc '%d:%i' -- "/proc/$BASHPID/fd/$TRANSACTION_FD") == "$DISPATCH_FD_IDENTITY" &&
       $DISPATCH_FD_IDENTITY == "$DISPATCH_LOCK_IDENTITY" ]] ||
        die 'transaction descriptor identity changed during final recovery verification'
    release_txn_verify_inherited_lock "$TRANSACTION_ROOT" "$TRANSACTION_FD" ||
        die 'final transaction flock proof changed during final recovery verification'
    verify_held_transaction_lock
    classify_transaction
    [[ $TRANSACTION_PHASE == none ]] ||
        die 'a release transaction marker appeared during final recovery verification'
    release_transaction_lock
    exit 0
fi
release_transaction_lock
die 'release recovery child returned success while a verified marker remains'
