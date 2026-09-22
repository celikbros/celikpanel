#!/usr/bin/env bash
# Executes production preflight glue with private readers. Native filesystem
# and process-kill coverage belongs to Go/VM tests; no installed paths are used.
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd -P)
TEST_ROOT=$(mktemp -d)
trap 'rm -rf -- "$TEST_ROOT"' EXIT
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
extract() { sed -n "/^$2() {$/,/^}$/p" "$ROOT/$1"; }
export TEST_ROOT
mkdir -p "$TEST_ROOT/candidate/recovery-runtime/bin" "$TEST_ROOT/selected/bin" "$TEST_ROOT/installed/bin" "$TEST_ROOT/candidate/bin"
cat > "$TEST_ROOT/candidate/recovery-runtime/bin/recovery" <<'SH'
#!/usr/bin/env bash
set -euo pipefail
[[ $# == 3 && $2 == --bin ]] || exit 80
printf '%s %s\n' "$1" "$3" >> "$TEST_ROOT/calls"
case "$1:$3" in
 verify-agent-native-contract:"$TEST_ROOT/candidate/bin") [[ ${REFUSE:-} != candidate ]] || { echo 'candidate declaration missing' >&2; exit 81; } ;;
 verify-mail-application:"$TEST_ROOT/installed/bin") [[ ${REFUSE:-} != historical ]] || { echo 'historical support unverified' >&2; exit 82; } ;;
 verify-mail-application:"$TEST_ROOT/candidate/bin") [[ ${REFUSE:-} != native ]] || { echo 'native owner evidence changed' >&2; exit 83; } ;;
 *) exit 84 ;;
esac
SH
chmod 0755 "$TEST_ROOT/candidate/recovery-runtime/bin/recovery"
cp "$TEST_ROOT/candidate/recovery-runtime/bin/recovery" "$TEST_ROOT/selected/bin/recovery"
# Historical/candidate management binaries are never capability probes.
for p in "$TEST_ROOT/installed/bin/agent" "$TEST_ROOT/candidate/bin/agent"; do
 printf '#!/bin/sh\necho unexpected-agent-execution >&2\nexit 92\n' > "$p"
 chmod 0755 "$p"
done
eval "$(extract update.sh run_update_idle_probe)"
eval "$(extract update.sh check_mail_application_compatibility)"
TRUSTED_RELEASE_ROOT=$TEST_ROOT/candidate
BIN_DIR=$TEST_ROOT/installed/bin
CODE_ROOT=$TEST_ROOT/selected
for mode in candidate selected; do
 RECOVERY_RUNTIME_ROOT=
 [[ $mode != selected ]] || RECOVERY_RUNTIME_ROOT=$CODE_ROOT
 for REFUSE in none candidate historical native; do
  export REFUSE
  : > "$TEST_ROOT/calls"
  result=0
  check_mail_application_compatibility > "$TEST_ROOT/output" 2>&1 || result=$?
  count=$(wc -l < "$TEST_ROOT/calls")
  case $REFUSE in
   none) [[ $result == 0 && $count == 3 ]] || fail 'successful checks changed' ;;
   candidate) [[ $result != 0 && $count == 1 && $update_failure_detail == 'candidate declaration missing' ]] || fail 'missing candidate fell through' ;;
   historical) [[ $result != 0 && $count == 2 && $update_failure_detail == 'historical support unverified' ]] || fail 'unsupported baseline fell through' ;;
   native) [[ $result != 0 && $count == 3 && $update_failure_detail == 'native owner evidence changed' ]] || fail 'native uncertainty lost' ;;
  esac
 done
done
# The two shell boundaries call the actual reader before stop and after locked
# reconciliation. This ordering assertion complements executed reader tests.
python3 - "$ROOT" <<'PY'
from pathlib import Path
import sys
r=Path(sys.argv[1]);s=(r/'rollback.sh').read_text()
pre=s.index('"$mail_compatibility_inspector" verify-mail-application')
stop=s.index('systemctl stop celikpanel-panel.service',pre)
final=s.index('snapshot Agent compatibility changed before the locked restore',stop)
publication=s.index('rollback_mutation_started=1',final)
assert pre<stop<final<publication
s=(r/'update.sh').read_text()
assert s.index('check_mail_application_compatibility || die')<s.index('if ! check_mail_application_compatibility; then')<s.index('==> Freezing panel and agent')
PY
printf 'PASS: candidate/baseline compatibility uses verified readers, stops on uncertainty, and precedes publication\n'
