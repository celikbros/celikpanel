#!/usr/bin/env bash
# upd3 continuation path: every Panel/Agent start of an update, forward
# completion, rollback, owner retry or abort first clears exactly that unit's
# failed/start-limit state, and a start still refused on the start limit is
# reported with code unit_start_limit_hit and the owner's command. The real
# helper and wrapper are executed against a recording systemctl stand-in.
# Güncelleme, tamamlama, geri alma ve sahip yeniden denemesindeki her Panel/Agent
# başlatması önce yalnız o birimin başlatma sınırını temizler.
set -euo pipefail
repo_root=$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT

cat > "$work/systemctl" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$FAKE_TRACE"
case "$1" in
    start) exit "${FAKE_START_STATUS:-0}" ;;
    show) printf '%s\n' "${FAKE_RESULT:-success}" ;;
esac
exit 0
SH
chmod 0755 "$work/systemctl"
export FAKE_TRACE=$work/trace

scripts=(update.sh rollback.sh deploy/finalize-pending-update.sh deploy/finalize-pending-rollback.sh deploy/abort-pre-mutation-active-update.sh)
extract() {
    awk -v header="$2() {" '$0 == header { inside=1 } inside { print } inside && $0 == "}" { exit }' "$1"
}

for script in "${scripts[@]}"; do
    file=$repo_root/$script
    # Static: no Panel/Agent start bypasses the bound wrapper.
    if grep -En '("\$SYSTEMCTL_BIN"|/usr/bin/systemctl|command systemctl)[[:space:]]+start[[:space:]]+celikpanel-(panel|agent)\.service' "$file"; then
        fail "$script starts the Panel or Agent without the controlled-start wrapper"
    fi
    grep -Eq 'systemctl start celikpanel-(panel|agent)\.service' "$file" || fail "$script has no start site (fixture drift)"
    (
        SYSTEMCTL_BIN=$work/systemctl
        update_failure_code= update_failure_reason=
        eval "$(extract "$file" release_unit_controlled_start)"
        eval "$(extract "$file" systemctl)"
        declare -F release_unit_controlled_start >/dev/null && declare -F systemctl >/dev/null ||
            fail "$script lacks the controlled-start helper or wrapper"
        for unit in celikpanel-panel.service celikpanel-agent.service; do
            : > "$FAKE_TRACE"
            systemctl start "$unit" 2> "$work/stderr" || fail "$script: successful start of $unit reported failure"
            [[ $(cat "$FAKE_TRACE") == "reset-failed $unit"$'\n'"start $unit" ]] ||
                fail "$script: reset does not precede the start of $unit: $(cat "$FAKE_TRACE")"
            [[ ! -s $work/stderr ]] || fail "$script: successful start printed guidance"
            # A start still refused on the start limit names the unit and the command.
            : > "$FAKE_TRACE"
            status=0
            FAKE_START_STATUS=1 FAKE_RESULT=start-limit-hit systemctl start "$unit" 2> "$work/stderr" || status=$?
            [[ $status == 1 ]] || fail "$script: refused start of $unit returned $status"
            grep -F "code=unit_start_limit_hit" "$work/stderr" >/dev/null &&
                grep -F "sudo systemctl reset-failed $unit" "$work/stderr" >/dev/null &&
                grep -F "sudo systemctl reset-failed $unit komutunu" "$work/stderr" >/dev/null ||
                fail "$script: start-limit refusal of $unit lacks the EN/TR owner command"
            if [[ $script == update.sh ]]; then
                [[ $update_failure_code == unit_start_limit_hit ]] || fail 'update.sh did not type the start-limit refusal'
                update_failure_code=
            fi
            # Any other refusal keeps its own cause.
            status=0
            FAKE_START_STATUS=1 FAKE_RESULT=exit-code systemctl start "$unit" 2> "$work/stderr" || status=$?
            [[ $status == 1 && ! -s $work/stderr ]] || fail "$script: ordinary start failure of $unit misreported"
        done
        # Only the unit about to start is reset; other units and verbs pass through.
        for args in 'start nginx.service' 'stop celikpanel-panel.service' 'start celikpanel-panel.service celikpanel-agent.service' 'is-active --quiet celikpanel-agent.service'; do
            : > "$FAKE_TRACE"
            # shellcheck disable=SC2086
            systemctl $args 2>/dev/null || true
            [[ $(cat "$FAKE_TRACE") == "$args" ]] || fail "$script: '$args' was changed: $(cat "$FAKE_TRACE")"
        done
    )
done

# rollback.sh re-binds its wrapper after the guard installation; both bindings
# route controlled starts through the same helper.
[[ $(grep -c 'release_unit_controlled_start "$2"' "$repo_root/rollback.sh") == 2 ]] ||
    fail 'rollback.sh has a wrapper binding without the controlled start'
grep -F 'recovery_runtime_preflight_failed|unit_start_limit_hit) code=$update_failure_code' "$repo_root/update.sh" >/dev/null ||
    fail 'update.sh summary does not carry unit_start_limit_hit'

echo 'PASS: each Panel/Agent start resets only that unit first; a start-limit refusal names the unit and the owner command'
