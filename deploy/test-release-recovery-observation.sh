#!/usr/bin/env bash
set -euo pipefail
ROOT=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
bash -n "$ROOT/deploy/release-recovery-observation.sh" "$0"
fail() { printf 'FAIL: %s\n' "$1" >&2; exit 1; }
expect_failure() { local why=$1; shift; if "$@"; then fail "$why"; fi; }
if [[ $EUID != 0 ]]; then printf 'SKIP: native observation fixture requires root metadata\n'; exit 0; fi
TRUSTED_RELEASE_ROOT=$ROOT
source "$ROOT/deploy/release-transaction-guard.sh"
source "$ROOT/deploy/release-recovery-foundation.sh"
source "$ROOT/deploy/release-recovery-observation.sh"
tmp=$(mktemp -d /var/lib/celikpanel-observation-test.XXXXXXXX)
trap 'rm -rf -- "$tmp"' EXIT
RELEASE_OBSERVATION_ROOT=$tmp/public
RELEASE_OBSERVATION_BINDINGS=$tmp/bindings
# The fixture does not create/change the host's product account or group.
_release_observation_gid() { printf '0\n'; }
id=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
commit=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb
token=cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc
snapshot=20260914T100000Z-from-unknown-to-$commit-dddddddddddddddddddddddddddddddd
[[ $(printf '0::/system.slice/celikpanel-self-update-%s.service\n' "$id" | _release_observation_parse_worker_request) == "$id" ]] || fail 'v2 identity'
[[ $(printf '1:name=systemd:/system.slice/celikpanel-self-update-%s.service\n' "$id" | _release_observation_parse_worker_request) == "$id" ]] || fail 'v1 identity'
expect_failure 'unrelated unit accepted' _release_observation_parse_worker_request <<< '0::/system.slice/celikpanel-agent.service'
expect_failure 'nested unit guessed' _release_observation_parse_worker_request <<< "0::/system.slice/celikpanel-self-update-$id.service/child"
expect_failure 'contradictory identities guessed' _release_observation_parse_worker_request <<< $'0::/system.slice/celikpanel-self-update-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa.service\n1:name=systemd:/system.slice/celikpanel-self-update-eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee.service'
release_observation_publish "$id" "$commit" accepted none operation_accepted
release_observation_publish "$id" "$commit" running none update_running
_release_observation_read "$id" 0
[[ $OBSERVATION_PHASE == running && $OBSERVATION_PROOF == none ]] || fail 'running not published'
expect_failure 'different commit accepted' release_observation_publish "$id" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa running none update_running
mkdir -m 0700 "$tmp/transaction"
: > "$tmp/transaction/transaction.lock"
chmod 0600 "$tmp/transaction/transaction.lock"
exec {fd}<>"$tmp/transaction/transaction.lock"
flock -n -x "$fd"
# Only the kernel observation is substituted; the real inherited-lock proof,
# strict schema, root metadata, fsync and immutable binding writer execute.
release_observation_worker_request() { printf '%s\n' aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; }
release_observation_bind_update "$tmp/transaction" "$fd" "$token" "$snapshot" "$commit"
before=$(sha256sum "$RELEASE_OBSERVATION_BINDINGS/$snapshot.binding")
release_observation_bind_update "$tmp/transaction" "$fd" "$token" "$snapshot" "$commit"
[[ $(sha256sum "$RELEASE_OBSERVATION_BINDINGS/$snapshot.binding") == "$before" ]] || fail 'same binding rewritten'
wrongtoken=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
expect_failure 'different update token replaced evidence' release_observation_bind_update "$tmp/transaction" "$fd" "$wrongtoken" "$snapshot" "$commit"
expect_failure 'recovery accepted wrong update token' release_observation_bind_recovery "$tmp/transaction" "$fd" "$wrongtoken" update "$snapshot" "$commit"
release_observation_bind_recovery "$tmp/transaction" "$fd" "$token" update "$snapshot" "$commit"
[[ $OBSERVATION_REQUEST == "$id" ]] || fail 'wrong request binding'
# Rollback takeover has its own token; the runner proves that current token
# independently, while this immutable association identifies its compensation.
release_observation_bind_recovery "$tmp/transaction" "$fd" "$wrongtoken" rollback "$snapshot" "$commit"
release_observation_publish "$id" "$commit" failed none update_failed
release_observation_publish "$id" "$commit" recovering none recovery_running
_release_observation_read "$id" 0
[[ $OBSERVATION_PREVIOUS == update_failed ]] || fail 'recovery erased known failure'
release_observation_publish "$id" "$commit" recovery_required none recovery_incomplete
_release_observation_read "$id" 0
[[ $OBSERVATION_PREVIOUS == update_failed ]] || fail 'incomplete recovery erased known failure'
release_observation_publish "$id" "$commit" recovered rollback_verified rollback_verified
before=$(sha256sum "$RELEASE_OBSERVATION_ROOT/$id.status")
release_observation_publish "$id" "$commit" failed none update_failed
[[ $(sha256sum "$RELEASE_OBSERVATION_ROOT/$id.status") == "$before" ]] || fail 'late worker erased native proof'
chmod 0660 "$RELEASE_OBSERVATION_ROOT/$id.status"
expect_failure 'unsafe record repaired' release_observation_publish "$id" "$commit" failed none update_failed
chmod 0640 "$RELEASE_OBSERVATION_ROOT/$id.status"
ln "$RELEASE_OBSERVATION_ROOT/$id.status" "$tmp/extra-link"
expect_failure 'hardlinked record overwritten' release_observation_publish "$id" "$commit" running none update_running
rm -f -- "$tmp/extra-link"
# A historical worker wrote no record: the updater's create-only initial record
# is the Agent's running record, byte for byte, and the binding accepts it.
legacy=eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee
legacy_snapshot=20260914T100000Z-from-unknown-to-$commit-ffffffffffffffffffffffffffffffff
release_observation_publish_initial "$legacy" "$commit" || fail 'initial record not created for a request without one'
_release_observation_read "$legacy" 0
[[ $OBSERVATION_PHASE:$OBSERVATION_PROOF:$OBSERVATION_REASON:$OBSERVATION_PREVIOUS == running:none:update_running:none ]] ||
    fail 'initial record is not the running record'
before=$(sha256sum "$RELEASE_OBSERVATION_ROOT/$legacy.status")
status=0; release_observation_publish_initial "$legacy" "$commit" || status=$?
[[ $status == 3 && $(sha256sum "$RELEASE_OBSERVATION_ROOT/$legacy.status") == "$before" ]] || fail "existing record rewritten by the initial producer: $status"
release_observation_worker_request() { printf '%s\n' "$legacy"; }
release_observation_bind_update "$tmp/transaction" "$fd" "$token" "$legacy_snapshot" "$commit" || fail 'initial record not accepted by the binding'
# Any existing phase, even one naming another commit, is never touched.
for existing in "$id" "$legacy"; do
    before=$(sha256sum "$RELEASE_OBSERVATION_ROOT/$existing.status")
    status=0; release_observation_publish_initial "$existing" aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa || status=$?
    [[ $status == 3 && $(sha256sum "$RELEASE_OBSERVATION_ROOT/$existing.status") == "$before" ]] || fail "existing $existing record changed: $status"
done
release_observation_publish "$legacy" "$commit" failed none update_failed
_release_observation_read "$legacy" 0
[[ $OBSERVATION_PHASE == failed && $OBSERVATION_PREVIOUS == update_failed ]] || fail 'self-produced record lost the failed transition'
[[ -z $(find "$tmp" -name '.observation-*' -o -name '.binding-*') ]] || fail 'staging left behind'
printf 'PASS: real native observation publisher, immutable identity binding, create-only initial record and terminal preservation\n'
