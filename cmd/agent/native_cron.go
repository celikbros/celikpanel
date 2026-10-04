package main

import (
	"os"

	"github.com/alicelik/celikpanel/internal/core"
)

// Native cron is the one catalogue component CelikPanel installs only when the
// host has none, and otherwise leaves exactly as the owner has it (D-022).
// Installing the distro `cron` package over another implementation is not
// harmless: apt resolves the package conflict by REMOVING the owner's
// systemd-cron, bcron or similar, and the jobs they ran stop. Re-enabling a
// unit the owner deliberately disabled is the same overwrite in a smaller
// form. So any trace of an existing cron — a known daemon unit or the
// `crontab` command every implementation ships — means "present, keep".
//
// Yerel cron, CelikPanel'in yalnız sunucuda hiç yokken kurduğu, aksi hâlde
// tam sahibinin bıraktığı gibi bıraktığı tek katalog bileşenidir (D-022).
// Dağıtımın `cron` paketini başka bir uygulamanın üstüne kurmak zararsız
// değildir: apt paket çakışmasını sahibin systemd-cron'unu, bcron'unu KALDIRARAK
// çözer ve çalıştırdıkları görevler durur. Sahibin bilerek kapattığı bir unit'i
// yeniden etkinleştirmek de aynı üzerine yazmanın küçük biçimidir. Bu yüzden
// var olan bir cron'un herhangi bir izi — bilinen bir daemon unit'i ya da her
// uygulamanın getirdiği `crontab` komutu — "var, koru" demektir.

// nativeCronUnitNames are daemon units of cron implementations the panel does
// not install but must recognise as the owner's.
// nativeCronUnitNames, panelin kurmadığı ama sahibinki olarak tanıması gereken
// cron uygulamalarının daemon unit'leridir.
var nativeCronUnitNames = []string{"cron", "cronie", "crond", "fcron", "dcron", "bcron", "systemd-cron"}

// nativeCronCommandPaths are the fixed system locations of `crontab`. The
// agent checks fixed paths rather than its own PATH so the answer does not
// depend on the environment it was started with.
// nativeCronCommandPaths, `crontab`ın sabit sistem konumlarıdır.
var nativeCronCommandPaths = []string{"/usr/bin/crontab", "/bin/crontab", "/usr/sbin/crontab", "/sbin/crontab", "/usr/local/bin/crontab"}

type nativeCronProbe struct {
	unitExists func(string) bool
	fileExists func(string) bool
}

// nativeCronPresent reports whether any cron implementation is already on the
// host. catalogueUnitPresent is the ordinary catalogue answer (cron.service or
// cronie.service), passed in so the rule stays testable without systemd.
// nativeCronPresent, sunucuda herhangi bir cron uygulamasının zaten olup
// olmadığını bildirir.
func nativeCronPresent(catalogueUnitPresent bool, probe nativeCronProbe) bool {
	if catalogueUnitPresent {
		return true
	}
	for _, path := range nativeCronCommandPaths {
		if probe.fileExists != nil && probe.fileExists(path) {
			return true
		}
	}
	for _, unit := range nativeCronUnitNames {
		if probe.unitExists != nil && probe.unitExists(unit) {
			return true
		}
	}
	return false
}

func hostNativeCronProbe(systemctl string) nativeCronProbe {
	return nativeCronProbe{
		unitExists: func(unit string) bool { return unitExistsWithExecutable(systemctl, unit) },
		fileExists: func(path string) bool {
			info, err := os.Stat(path)
			return err == nil && info.Mode().IsRegular()
		},
	}
}

// nativeCronRemovalRefusal is the Agent-side guard behind the Panel's refusal:
// the generic uninstall would stop the daemon and purge the package, ending
// every scheduled job on the host, including the owner's own.
// nativeCronRemovalRefusal, Panel'in reddinin arkasındaki Agent tarafı
// korumadır.
func nativeCronRemovalRefusal(service *core.ManagedService) string {
	if service != nil && service.ID == core.NativeCronServiceID {
		return core.NativeCronRemovalRefusal
	}
	return ""
}
