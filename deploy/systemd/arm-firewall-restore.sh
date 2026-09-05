#!/usr/bin/env bash
set -euo pipefail

# R-052. A control-plane restore takes the firewall from the archive precisely
# so that a fresh install cannot leave a restored host disarmed - but until this
# script existed the restore only placed /etc/celikpanel/firewall.nft as a FILE.
# The single thing that ever loaded that file was celikpanel-firewall-restore's
# slot in the next boot, and the boot the operator is standing in had already
# passed it. So `nft list ruleset` was empty, the unit was enabled and inactive,
# and the host ran unprotected until someone rebooted it. Nothing said so.
#
# This arms the snapshot the restore just placed: the same unit the boot would
# run, started once, and then reported.
#
# R-052. Geri yükleme, güvenlik duvarını arşivden tam da temiz bir kurulumun
# makineyi korumasız bırakmaması için alır; ama bu betikten önce yalnız DOSYAYI
# yerine koyuyordu. O dosyayı yükleyen tek şey unit'in bir sonraki açılıştaki
# yuvasıydı ve operatörün içinde bulunduğu açılış o yuvayı çoktan geçmişti.
# Böylece makine, biri yeniden başlatana kadar korumasız çalışıyordu ve bunu
# kimse söylemiyordu. Bu betik, geri yüklemenin az önce koyduğu snapshot'ı,
# açılışın çalıştıracağı aynı unit'i bir kez başlatarak devreye alır.

snapshot_path="${1:?firewall snapshot path is required}"
agent_binary="${2:?agent binary path is required}"
unit_name="celikpanel-firewall-restore.service"

# No snapshot at all is not a failure: the archive simply carried no firewall,
# which is what a host that never enabled one looks like. Saying so is still
# part of the report, because "nothing was armed" and "the firewall is on" must
# never look the same to the operator.
# Snapshot'ın hiç olmaması bir hata değildir: arşiv güvenlik duvarı taşımamıştır.
# Yine de raporlanır; "hiçbir şey devreye alınmadı" ile "güvenlik duvarı açık"
# operatöre asla aynı görünmemelidir.
if [[ ! -e "$snapshot_path" && ! -L "$snapshot_path" ]]; then
    printf 'the archive carried no firewall ruleset, so there was nothing to arm / arşiv güvenlik duvarı kuralı taşımıyordu, devreye alınacak bir şey yoktu\n'
    exit 0
fi
if [[ -L "$snapshot_path" || ! -f "$snapshot_path" || ! -s "$snapshot_path" ]]; then
    printf 'invalid firewall snapshot path: %s\n' "$snapshot_path" >&2
    exit 1
fi
if [[ ! -x "$agent_binary" ]]; then
    printf 'the agent binary is not executable: %s\n' "$agent_binary" >&2
    exit 1
fi

# The unit's OWN preflight, run here as a plain command BEFORE the unit is
# started. celikpanel-firewall-restore.service declares
# OnFailure=emergency.target with OnFailureJobMode=isolate, which is the right
# answer during boot - a host must not come up open - and the wrong one during
# an install the operator is standing in over SSH, where it would isolate the
# machine into a rescue console. The predictable failure (a snapshot nft will
# not take) is therefore caught out here, so the installer can stop with a
# sentence instead of taking the host away from the person running it.
#
# Unit'in KENDİ ön kontrolü, unit başlatılmadan önce düz bir komut olarak burada
# çalışır. Unit OnFailure=emergency.target ilan eder; bu açılışta doğrudur ama
# operatörün SSH ile içinde bulunduğu bir kurulumda makineyi kurtarma konsoluna
# atardı. Öngörülebilir hata bu yüzden burada yakalanır.
if ! "$agent_binary" --check-firewall-restore; then
    printf 'the restored firewall ruleset did not pass its preflight and was NOT loaded / geri yüklenen güvenlik duvarı kuralı ön kontrolden geçmedi ve YÜKLENMEDİ\n' >&2
    exit 1
fi

if ! systemctl start "$unit_name"; then
    printf 'the firewall restore unit could not be started; this host is NOT firewalled / güvenlik duvarı geri yükleme unit'"'"'i başlatılamadı; bu makine KORUMASIZ\n' >&2
    exit 1
fi

# RemainAfterExit=true, so a oneshot that did its work stays active. Any other
# state means the ruleset was not loaded - including the quiet one where
# ConditionPathExists skipped the unit and `systemctl start` still succeeded.
# RemainAfterExit=true olduğu için işini yapan oneshot etkin kalır. Başka her
# durum kural setinin yüklenmediği anlamına gelir.
unit_state=$(systemctl is-active "$unit_name") || true
if [[ "$unit_state" != active ]]; then
    printf 'the firewall restore unit is %s after being started; this host is NOT firewalled / güvenlik duvarı geri yükleme unit'"'"'i başlatıldıktan sonra %s; bu makine KORUMASIZ\n' \
        "$unit_state" "$unit_state" >&2
    exit 1
fi

printf 'the archived firewall ruleset is loaded now, without a reboot / arşivlenmiş güvenlik duvarı kuralı yeniden başlatma olmadan şimdi yüklü\n'
