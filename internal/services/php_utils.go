package services

import (
	"fmt"
	"os/exec"
)

// reloadPHPFPM is a shared utility to reload PHP-FPM. A variable so tests can
// stub it — pool-writer tests must not depend on a real unit being present on
// the machine running them.
// reloadPHPFPM, PHP-FPM'i yeniden yüklemek için ortak yardımcıdır. Testler
// taklit edebilsin diye değişkendir — havuz yazıcısı testleri, koştukları
// makinede gerçek bir unit'in varlığına bağımlı olmamalıdır.
var reloadPHPFPM = func(version string) error {
	serviceName := fmt.Sprintf("php%s-fpm", version)
	cmd := exec.Command("systemctl", "reload", serviceName)
	return cmd.Run()
}
