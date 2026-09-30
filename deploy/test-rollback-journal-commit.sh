#!/usr/bin/env bash
# The rollback journal names the restored release's source commit only from the
# restored Agent's own build record, and only when that record's Agent digest
# equals the installed Agent. The snapshot name and its commit file are not
# read or changed here (they stay "unknown" by design).
# Geri alma günlüğü, geri yüklenen sürümün kaynak commit'ini yalnız Agent'ın
# yapı kaydından ve kayıtlı özet kurulu Agent ile eşleşirse gösterir.
set -euo pipefail

ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }

function_source=$(sed -n '/^restored_build_commit() {$/,/^}$/p' "$ROOT/rollback.sh")
[[ -n "$function_source" ]] || fail "restored_build_commit is missing from rollback.sh"
eval "$function_source"
grep -Fq 'Restored release source commit / Geri yüklenen sürümün kaynak commit'"'"'i: $commit_line' "$ROOT/rollback.sh" \
    || fail "the journal line does not print the resolved commit text"
grep -Fq 'snapshot_name_pattern='"'"'^([0-9]{8}T[0-9]{6}Z)-from-unknown-to-([0-9a-f]{40})-([0-9a-f]{32})$'"'" "$ROOT/rollback.sh" \
    || fail "the canonical snapshot name rule changed"
grep -Fq '[[ "$snapshot_commit" == unknown ]]' "$ROOT/rollback.sh" \
    || fail "the snapshot commit file rule changed"

tmp=$(mktemp -d)
trap 'rm -rf -- "$tmp"' EXIT
snap=$tmp/snapshot
BIN_DIR=$tmp/bin
mkdir -p -- "$snap/bin" "$BIN_DIR"
printf 'agent build bytes\n' > "$snap/bin/agent"
cp -- "$snap/bin/agent" "$BIN_DIR/agent"
digest=$(sha256sum -- "$BIN_DIR/agent" | awk '{print $1}')
source_commit=e3a569d527ec88c01e14c1df015fa3010ea7f803
write_contract() {
    printf '{"schema":"celikpanel-agent-native-contract/v1","source_commit":"%s","agent_sha256":"%s","mail_hook_policy":"preserve-independent-v1"}\n' \
        "$1" "$2" > "$snap/bin/agent-native-contract.json"
}

write_contract "$source_commit" "$digest"
[[ "$(restored_build_commit)" == "$source_commit" ]] || fail "matching build record was not used"

printf 'different agent\n' > "$BIN_DIR/agent"
if restored_build_commit >/dev/null 2>&1; then fail "a record for different Agent bytes was used"; fi
cp -- "$snap/bin/agent" "$BIN_DIR/agent"

write_contract unknown "$digest"
if restored_build_commit >/dev/null 2>&1; then fail "a non-commit value was used"; fi
printf '{"schema":"other","source_commit":"%s","agent_sha256":"%s",}\n' "$source_commit" "$digest" > "$snap/bin/agent-native-contract.json"
if restored_build_commit >/dev/null 2>&1; then fail "a foreign schema was used"; fi
head -c 3000 /dev/zero | tr '\0' 'x' > "$snap/bin/agent-native-contract.json"
if restored_build_commit >/dev/null 2>&1; then fail "an oversized record was used"; fi
rm -f -- "$snap/bin/agent-native-contract.json"
if restored_build_commit >/dev/null 2>&1; then fail "a missing record produced a commit"; fi
write_contract "$source_commit" "$digest"
mv -- "$snap/bin/agent-native-contract.json" "$tmp/contract"
ln -s -- "$tmp/contract" "$snap/bin/agent-native-contract.json"
if restored_build_commit >/dev/null 2>&1; then fail "a symlinked record was used"; fi

printf 'PASS: rollback journal names the restored commit only from a matching Agent build record\n'
