package main

import (
	"context"
	"strings"
	"testing"
)

func TestPDNSPackageInstallMasksAndStopsBeforePackageHooks(t *testing.T) {
	systemd := newFakeBINDInstallSystemd(map[string]*fakeBINDInstallUnit{
		"pdns.service": {loadState: "not-found", unitFileState: ""},
	})
	recoveries := 0
	output, err := installPDNSPackagesWithGuardOps(
		context.Background(), "/usr/bin/systemctl",
		func() (string, error) {
			systemd.commands = append(systemd.commands, "PACKAGE_INSTALL")
			unit := systemd.units["pdns.service"]
			unit.loadState = "loaded"
			unit.unitFileState = "enabled"
			if _, err := systemd.run(context.Background(), "/usr/bin/systemctl", "start", "pdns.service"); err == nil {
				t.Fatal("package hook escaped the PowerDNS mask")
			}
			return "installed pdns", nil
		},
		fakeBINDInstallGuardOps(systemd, &recoveries),
	)
	if err != nil || output != "installed pdns" || recoveries != 1 {
		t.Fatalf("output=%q recoveries=%d err=%v", output, recoveries, err)
	}
	packageIndex := commandIndex(systemd.commands, "PACKAGE_INSTALL")
	maskIndex := commandIndex(systemd.commands, "mask pdns.service")
	if maskIndex < 0 || maskIndex >= packageIndex {
		t.Fatalf("PowerDNS mask did not precede install: %v", systemd.commands)
	}
	unit := systemd.units["pdns.service"]
	if !unit.masked || unit.runtimeMasked || unit.active {
		t.Fatalf("terminal state=%+v commands=%v", unit, systemd.commands)
	}
	for _, command := range systemd.commands {
		if strings.HasPrefix(command, "enable ") {
			t.Fatalf("package guard enabled PowerDNS: %v", systemd.commands)
		}
	}
}
