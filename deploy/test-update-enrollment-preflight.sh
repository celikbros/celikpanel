#!/usr/bin/env bash
# Execute early idle admission with a private probe; no installed paths/services.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
extract() { sed -n "/^$2() {$/,/^}$/p" "$ROOT/$1"; }
export TEST_ROOT
cat > "$TEST_ROOT/probe" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 1 && $1 == --check-service-mutation-idle ]] || exit 90
[[ $CELIKPANEL_AGENT_STATE_DIR == "$TEST_ROOT/state" && $CELIKPANEL_MUTATION_LOCK == "$TEST_ROOT/lock" ]] || exit 91
printf 'probe\n' >> "$TEST_ROOT/calls"
case $PROBE_STATE in
 idle) exit 0 ;;
 enrollment) echo 'privileged mutation aabb is active' >&2; exit 1 ;;
 unknown) echo 'mutation evidence changed' >&2; exit 2 ;;
 *) exit 92 ;;
esac
SH
chmod 0755 "$TEST_ROOT/probe"
eval "$(extract update.sh run_update_idle_probe)"
eval "$(extract update.sh preflight_mutations_before_quiesce)"
# upd4: the refusal is reported typed through the updater's own helper.
eval "$(extract update.sh update_probe_diagnostic)"
eval "$(extract update.sh fail_update_preflight)"
die() { printf 'refused: %s; detail=%s\n' "$*" "$update_failure_detail" >&2; exit 73; }
PREFLIGHT_AGENT=$TEST_ROOT/probe
AGENT_STATE_DIR=$TEST_ROOT/state
MUTATION_LOCK=$TEST_ROOT/lock
for PROBE_STATE in idle enrollment unknown; do
 export PROBE_STATE
 BOOTSTRAP_PRE_LEDGER=0
 : > "$TEST_ROOT/calls"
 status=0
 (preflight_mutations_before_quiesce; printf 'reached-quiesce\n') > "$TEST_ROOT/output" 2>&1 || status=$?
 [[ $(wc -l < "$TEST_ROOT/calls") == 1 ]] || fail 'probe was not called exactly once'
 case $PROBE_STATE in
 idle) [[ $status == 0 ]] && grep -q 'reached-quiesce' "$TEST_ROOT/output" || fail 'idle was not admitted' ;;
 *) [[ $status == 73 ]] && ! grep -q 'reached-quiesce' "$TEST_ROOT/output" || fail 'non-idle reached quiesce'
    grep -q 'coordinators remain available' "$TEST_ROOT/output" || fail 'refusal lost recovery guidance'
    if [[ $PROBE_STATE == enrollment ]]; then grep -q 'privileged mutation aabb is active' "$TEST_ROOT/output" || fail 'reservation detail lost'
    else grep -q 'mutation evidence changed' "$TEST_ROOT/output" || fail 'unknown became idle'; fi ;;
 esac
done
# Historical pre-ledger bootstrap retains its existing, separately strict probes.
BOOTSTRAP_PRE_LEDGER=1
: > "$TEST_ROOT/calls"
preflight_mutations_before_quiesce
[[ ! -s "$TEST_ROOT/calls" ]] || fail 'pre-ledger compatibility changed'
python3 - "$ROOT" <<'PY'
from pathlib import Path
import sys
s=(Path(sys.argv[1])/'update.sh').read_text()
early=s.index('    preflight_mutations_before_quiesce\n')
stage=s.index('    mkdir -m 0700 -- "$stage_root"',early)
marker=s.index('    release_txn_create_quiesce_marker',stage)
locked=s.index('--check-service-mutation-idle-under-external-lock',marker)
freeze=s.index('    freeze_release_service_cgroup celikpanel-panel.service',locked)
assert early<stage<marker<locked<freeze
PY
printf 'PASS: pending or unknown mutation refuses before update barriers; final locked proof retained\n'
