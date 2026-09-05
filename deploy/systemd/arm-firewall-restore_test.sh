#!/usr/bin/env bash
set -euo pipefail

# R-052 guard. The restore places /etc/celikpanel/firewall.nft and the boot slot
# that would have loaded it has already passed, so the install itself has to arm
# it. This test pins the whole contract of that arming: what it starts, what it
# refuses to start, and that it never lets the install finish quietly with an
# unprotected host.
#
# R-052 muhafızı. Geri yükleme kural setini yerine koyar ama onu yükleyecek
# açılış yuvası geçmiştir; devreye almayı kurulumun kendisi yapmalıdır. Bu test
# o devreye almanın sözleşmesini kilitler.

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
helper="$root/deploy/systemd/arm-firewall-restore.sh"
tmp="$(mktemp -d)"
# bash lets a successful EXIT trap overwrite the status a failing test exited
# with, which would turn every FAIL below into a silent pass. Carry it across.
# Başarılı bir EXIT tuzağı, düşen bir testin çıkış durumunu ezebilir; taşı.
trap 'cleanup_status=$?; rm -rf -- "$tmp"; exit "$cleanup_status"' EXIT

fail() {
    printf 'FAIL: %s\n' "$1" >&2
    exit 1
}

mkdir -p "$tmp/bin"
printf '%s\n' \
    '#!/usr/bin/env bash' \
    'printf "%s\n" "$*" >> "$SYSTEMCTL_LOG"' \
    'if [[ "$1" == is-active ]]; then' \
    '    printf "%s\n" "${UNIT_STATE:-active}"' \
    '    [[ "${UNIT_STATE:-active}" == active ]] || exit 3' \
    '    exit 0' \
    'fi' \
    'exit "${SYSTEMCTL_EXIT_CODE:-0}"' > "$tmp/bin/systemctl"
chmod 0755 "$tmp/bin/systemctl"
export PATH="$tmp/bin:$PATH"
export SYSTEMCTL_LOG="$tmp/systemctl.log"

agent="$tmp/agent"
printf '%s\n' \
    '#!/usr/bin/env bash' \
    'printf "%s\n" "$*" >> "$AGENT_LOG"' \
    'exit "${AGENT_EXIT_CODE:-0}"' > "$agent"
chmod 0755 "$agent"
export AGENT_LOG="$tmp/agent.log"

snapshot="$tmp/firewall.nft"
reset_logs() {
    : > "$SYSTEMCTL_LOG"
    : > "$AGENT_LOG"
}

# An archive that carried no firewall arms nothing and is not an error, but it
# must still say so instead of looking like a firewall that came back.
# Güvenlik duvarı taşımayan arşiv hiçbir şey devreye almaz; bu bir hata değildir
# ama geri gelmiş bir güvenlik duvarı gibi görünmemelidir.
reset_logs
report=$(bash "$helper" "$snapshot" "$agent") || fail "a missing snapshot was treated as a failure"
[[ "$report" == *"nothing to arm"* ]] || fail "a missing snapshot did not say that nothing was armed"
[[ ! -s "$SYSTEMCTL_LOG" ]] || fail "a missing snapshot still touched systemd"
[[ ! -s "$AGENT_LOG" ]] || fail "a missing snapshot still ran the agent preflight"

# The defect itself: a placed snapshot must be LOADED NOW, by starting the very
# unit the next boot would have run, and the result must be verified rather
# than assumed.
# Kusurun kendisi: yerine konmuş snapshot ŞİMDİ yüklenmelidir.
reset_logs
printf '{"version":2}\n' > "$snapshot"
report=$(bash "$helper" "$snapshot" "$agent") || fail "a restored snapshot was not armed"
expected_systemctl=$'start celikpanel-firewall-restore.service\nis-active celikpanel-firewall-restore.service'
[[ "$(<"$SYSTEMCTL_LOG")" == "$expected_systemctl" ]] || \
    fail "the arming did not start and then verify the boot unit: $(<"$SYSTEMCTL_LOG")"
[[ "$(<"$AGENT_LOG")" == "--check-firewall-restore" ]] || \
    fail "the arming did not run the unit's own preflight first"
[[ "$report" == *"without a reboot"* ]] || fail "the arming did not report what it did"

# The unit declares OnFailure=emergency.target with OnFailureJobMode=isolate. A
# unit started by hand and failing would isolate the machine into a rescue
# console under the operator running the install, so a snapshot that cannot be
# loaded has to be caught BEFORE the unit is ever started.
# Unit OnFailure=emergency.target ilan eder; yüklenemeyen bir snapshot bu yüzden
# unit hiç başlatılmadan yakalanmalıdır.
reset_logs
AGENT_EXIT_CODE=1 bash "$helper" "$snapshot" "$agent" 2>"$tmp/stderr" && \
    fail "a snapshot that failed its preflight was reported as armed"
[[ ! -s "$SYSTEMCTL_LOG" ]] || \
    fail "a failed preflight still started the unit, which would isolate emergency.target"
grep -Fq 'NOT loaded' "$tmp/stderr" || fail "a failed preflight did not say the ruleset was not loaded"

# A start that fails, and a start that succeeds while the unit did nothing -
# ConditionPathExists skipping is silent - are both an unprotected host.
# Başarısız start ve unit'in hiçbir şey yapmadığı sessiz durum, ikisi de
# korumasız makinedir.
reset_logs
SYSTEMCTL_EXIT_CODE=1 bash "$helper" "$snapshot" "$agent" 2>"$tmp/stderr" && \
    fail "a failed unit start was reported as armed"
grep -Fq 'NOT firewalled' "$tmp/stderr" || fail "a failed start did not say the host is unprotected"

reset_logs
UNIT_STATE=inactive bash "$helper" "$snapshot" "$agent" 2>"$tmp/stderr" && \
    fail "a unit that stayed inactive was reported as armed"
grep -Fq 'NOT firewalled' "$tmp/stderr" || fail "an inactive unit did not say the host is unprotected"

# A symlinked or empty snapshot is abnormal and fails before anything runs.
# Sembolik bağ ya da boş snapshot anormaldir; hiçbir şey çalışmadan düşer.
reset_logs
rm -f -- "$snapshot"
ln -s "$tmp/missing-target" "$snapshot"
bash "$helper" "$snapshot" "$agent" 2>/dev/null && fail "a dangling snapshot symlink was accepted"
[[ ! -s "$SYSTEMCTL_LOG" && ! -s "$AGENT_LOG" ]] || fail "an invalid snapshot still ran something"
rm -f -- "$snapshot"
: > "$snapshot"
reset_logs
bash "$helper" "$snapshot" "$agent" 2>/dev/null && fail "an empty snapshot was accepted"
[[ ! -s "$SYSTEMCTL_LOG" && ! -s "$AGENT_LOG" ]] || fail "an empty snapshot still ran something"

# The installer invariants: only a restore arms, the arming follows the
# reconcile that owns the unit's enablement, and a failure stops the install
# instead of finishing quietly over an unprotected host.
# Kurulum değişmezleri: yalnız geri yükleme devreye alır, devreye alma unit'in
# etkinleştirmesini uzlaştıran adımdan sonra gelir ve hata kurulumu durdurur.
grep -Fq 'arm-firewall-restore.sh" \' "$root/install.sh" || \
    fail "install.sh does not call the arming helper"
grep -Fq '"$CONF_DIR/firewall.nft" "$PREFIX/bin/agent") || die' "$root/install.sh" || \
    fail "install.sh does not stop the install when the arming fails"
reconcile_line=$(grep -n 'enable-firewall-restore-if-saved.sh' "$root/install.sh" | head -1 | cut -d: -f1)
arm_line=$(grep -n 'arm-firewall-restore.sh' "$root/install.sh" | head -1 | cut -d: -f1)
gate_line=$(grep -n 'if \[\[ "\$RESTORE_ARMED" == 1 \]\]; then' "$root/install.sh" | tail -1 | cut -d: -f1)
(( reconcile_line < arm_line )) || \
    fail "install.sh arms the firewall before reconciling the unit's enablement"
(( gate_line < arm_line && arm_line - gate_line < 6 )) || \
    fail "install.sh does not gate the arming on a control-plane restore"

bash -n "$root/install.sh" "$helper" "$0"
printf 'PASS: a restored firewall is armed before the install finishes\n'
