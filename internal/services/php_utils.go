package services

import (
	"fmt"
	"os/exec"
)

// reloadPHPFPM is a shared utility to reload PHP-FPM
func reloadPHPFPM(version string) error {
	serviceName := fmt.Sprintf("php%s-fpm", version)
	cmd := exec.Command("systemctl", "reload", serviceName)
	return cmd.Run()
}
