package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// fakeDebianBINDSystemd models the one Debian fact the rollback tripped over:
// bind9.service is an Alias= of named.service, so it exists only while
// named.service is enabled, and enabling it by name fails when the alias
// symlink is absent.
// fakeDebianBINDSystemd, geri almanın takıldığı tek Debian gerçeğini modeller:
// bind9.service named.service'in bir Alias='ıdır; yalnız named.service
// etkinken var olur ve takma ad bağı yokken adıyla etkinleştirmek başarısız
// olur.
type fakeDebianBINDSystemd struct {
	namedEnabled bool
	namedActive  bool
	masked       map[string]bool
	runtimeMask  map[string]bool
	commands     []string
}

func (f *fakeDebianBINDSystemd) aliasPresent() bool {
	return f.namedEnabled
}

func (f *fakeDebianBINDSystemd) run(_ context.Context, executable string, args ...string) ([]byte, error) {
	if executable != "/usr/bin/systemctl" {
		return nil, fmt.Errorf("unexpected executable %q", executable)
	}
	f.commands = append(f.commands, strings.Join(args, " "))
	if len(args) < 2 {
		return nil, errors.New("incomplete systemctl command")
	}
	command, unit, runtime := args[0], args[1], false
	if args[1] == "--runtime" {
		if len(args) != 3 {
			return nil, errors.New("incomplete runtime systemctl command")
		}
		unit, runtime = args[2], true
	}
	if unit != "named.service" && unit != "bind9.service" {
		return nil, fmt.Errorf("unknown unit %q", unit)
	}
	switch command {
	case "show":
		if f.masked[unit] {
			return []byte("LoadState=masked\nActiveState=inactive\nUnitFileState=masked\n"), nil
		}
		if f.runtimeMask[unit] {
			return []byte("LoadState=masked\nActiveState=inactive\nUnitFileState=masked-runtime\n"), nil
		}
		if unit == "bind9.service" && !f.aliasPresent() {
			return []byte("LoadState=not-found\nActiveState=inactive\nUnitFileState=\n"), nil
		}
		active, enablement := "inactive", "disabled"
		if f.namedActive {
			active = "active"
		}
		if f.namedEnabled {
			enablement = "enabled"
		}
		return []byte(fmt.Sprintf("LoadState=loaded\nActiveState=%s\nUnitFileState=%s\n", active, enablement)), nil
	case "mask":
		if runtime {
			f.runtimeMask[unit] = true
		} else {
			f.masked[unit] = true
		}
		return nil, nil
	case "unmask":
		if runtime {
			delete(f.runtimeMask, unit)
		} else {
			delete(f.masked, unit)
		}
		return nil, nil
	case "enable":
		if unit == "bind9.service" && !f.aliasPresent() {
			return []byte("Failed to enable unit: Unit bind9.service does not exist"), errors.New("exit status 1")
		}
		f.namedEnabled = true
		return nil, nil
	case "disable":
		f.namedEnabled = false
		return nil, nil
	case "start":
		if f.masked[unit] || f.runtimeMask[unit] {
			return []byte("Unit is masked"), errors.New("exit status 1")
		}
		if unit == "bind9.service" && !f.aliasPresent() {
			return []byte("Unit bind9.service not found"), errors.New("exit status 5")
		}
		f.namedActive = true
		return nil, nil
	case "stop", "reset-failed":
		f.namedActive = false
		return nil, nil
	default:
		return nil, fmt.Errorf("unexpected systemctl command %q", command)
	}
}

func newRetiredDebianBINDSystemd() *fakeDebianBINDSystemd {
	// The exact shape a BIND-to-PowerDNS switch leaves its source in before the
	// target is started: stopped, disabled and persistently masked under both
	// names. The alias symlink is gone because named.service was disabled.
	// BIND'dan PowerDNS'e geçişin hedefi başlatmadan önce kaynağını bıraktığı
	// tam biçim: iki ad altında da durdurulmuş, devre dışı ve kalıcı maskeli.
	// named.service devre dışı bırakıldığı için takma ad bağı yok.
	return &fakeDebianBINDSystemd{
		masked:      map[string]bool{"named.service": true, "bind9.service": true},
		runtimeMask: map[string]bool{},
	}
}

func debianSourceBINDJournalSnapshots() []dnsUnitSnapshot {
	// Journal order is by name, so the alias comes first. That is exactly the
	// order S-8's rollback restored them in.
	// Günlük sırası ada göredir; takma ad önce gelir. S-8'in geri alması onları
	// tam bu sırayla geri yükledi.
	return []dnsUnitSnapshot{
		{Name: "bind9.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
		{Name: "named.service", LoadState: "loaded", ActiveState: "active", UnitFileState: "enabled"},
	}
}

func aliasRestoreTestGuard(systemd *fakeDebianBINDSystemd) *bindPackageInstallGuard {
	return &bindPackageInstallGuard{
		systemctl: "/usr/bin/systemctl",
		ops: bindInstallGuardOps{
			verifyMaskParent: func() error { return nil },
			runSystemd:       systemd.run,
		},
		ownedMask: map[string]bool{},
	}
}

// S-8 T5, register R-031. The rollback of a BIND-to-PowerDNS switch restores
// the source units from the journal in name order, so it enabled the
// bind9.service alias before re-enabling named.service and failed with
// "Unit bind9.service does not exist". The source BIND never came back, the
// recovery poisoned the ledger, and the host was left with no engine serving.
//
// S-8 T5, defter R-031. BIND'dan PowerDNS'e geçişin geri alması kaynak
// birimleri günlükten ad sırasıyla geri yükler; bu yüzden named.service'i
// yeniden etkinleştirmeden önce bind9.service takma adını etkinleştirmeye
// çalıştı ve "Unit bind9.service does not exist" ile düştü. Kaynak BIND geri
// gelmedi, kurtarma defteri zehirledi ve sunucu hizmet veren motorsuz kaldı.
func TestRollbackRestoresTheBINDAliasAfterTheUnitItAliases(t *testing.T) {
	systemd := newRetiredDebianBINDSystemd()
	err := restoreDNSUnitSnapshotsWithGuard(
		context.Background(), aliasRestoreTestGuard(systemd), debianSourceBINDJournalSnapshots(),
	)
	if err != nil {
		t.Fatalf("restoring the source BIND from its journal snapshots failed: %v", err)
	}
	named := commandIndex(systemd.commands, "enable named.service")
	alias := commandIndex(systemd.commands, "enable bind9.service")
	if named < 0 || alias < 0 || named > alias {
		t.Fatalf("named.service must be re-enabled before its alias: %v", systemd.commands)
	}
	if !systemd.namedEnabled || !systemd.namedActive || !systemd.aliasPresent() ||
		len(systemd.masked) != 0 || len(systemd.runtimeMask) != 0 {
		t.Fatalf("source BIND was not restored exactly: %+v", systemd)
	}
}

// The ordering is the only change: a journal that already lists named.service
// first restores identically.
// Değişen yalnız sıralamadır: named.service'i zaten önce listeleyen bir günlük
// aynı biçimde geri yüklenir.
func TestRollbackRestoreIsIndifferentToJournalOrder(t *testing.T) {
	snapshots := debianSourceBINDJournalSnapshots()
	snapshots[0], snapshots[1] = snapshots[1], snapshots[0]
	systemd := newRetiredDebianBINDSystemd()
	if err := restoreDNSUnitSnapshotsWithGuard(
		context.Background(), aliasRestoreTestGuard(systemd), snapshots,
	); err != nil {
		t.Fatalf("restore with named.service first failed: %v", err)
	}
	if !systemd.namedEnabled || !systemd.namedActive {
		t.Fatalf("source BIND was not restored: %+v", systemd)
	}
}

func TestRestoreOrderingPutsOnlyTheAliasLast(t *testing.T) {
	ordered := orderDNSUnitSnapshotsForRestore([]dnsUnitSnapshot{
		{Name: "bind9.service"}, {Name: "named.service"}, {Name: "pdns.service"},
	})
	got := []string{ordered[0].Name, ordered[1].Name, ordered[2].Name}
	want := []string{"named.service", "pdns.service", "bind9.service"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("restore order=%v, want %v", got, want)
	}
	if orderDNSUnitSnapshotsForRestore(nil) != nil {
		t.Fatal("an empty snapshot list must stay empty")
	}
}
