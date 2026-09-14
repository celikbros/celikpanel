package main

import (
	"errors"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

// Preparing a kit runs only inside the already admitted update's empty-marker
// preflight boundary. It cannot start an installed-panel update.
// Kit hazırlığı, kabul edilmiş güncellemenin işaretçi bulunmayan ön kontrolünde
// çalışır. Kurulu panel için yeni bir güncelleme başlatamaz.
func dispatchRuntimePreparation(args []string, uid int, prepare func(string, string) error, report func(string)) int {
	if uid != 0 {
		return exitNotOwner
	}
	if len(args) != 7 || args[0] != "prepare-runtime" || args[1] != "--source" ||
		!filepath.IsAbs(args[2]) || filepath.Clean(args[2]) != args[2] ||
		args[3] != "--mode" || !runtimePreparationMode(args[4]) ||
		args[5] != "--transaction-fd" || args[6] != "9" {
		return exitUsage
	}
	if err := prepare(args[2], args[4]); err != nil {
		report("Recovery runtime preparation could not be verified before service downtime. Preserve its evidence. " + err.Error())
		return exitUnavailable
	}
	return exitOK
}

func runtimePreparationMode(mode string) bool {
	return mode == "--normal" || mode == "--bootstrap-pre-ledger" || mode == "--bootstrap-schema17"
}

type runtimePreparationDependencies struct {
	boundary func() error
	selected func() error
	enroll   func(string) error
	promote  func(string, string) error
}

func prepareRuntimeWith(source, mode string, deps runtimePreparationDependencies) error {
	if !runtimePreparationMode(mode) {
		return recoveryruntime.ErrUnavailable
	}
	if err := deps.boundary(); err != nil {
		return err
	}
	err := deps.selected()
	if errors.Is(err, recoveryruntime.ErrNotSelected) {
		// Only proved absence admits first enrollment. A broken selected kit
		// must never be replaced by treating an observation failure as absence.
		// Yalnız kanıtlanmış yokluk ilk kayda izin verir; bozuk seçim yokluk değildir.
		return deps.enroll(source)
	}
	if err != nil {
		return err
	}
	return deps.promote(source, mode)
}

// Read-only status/version must remain available even when kit proof fails.
// Enrollment/preparation is performed by the verified candidate entry, not by
// dispatching that new command to an older selected reader.
func launcherDispatchCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "recover", "--verify-final-state", "verify-compatibility",
		"verify-material-support", "prepare-recovery-material", "material-root", "completion-material-root", "verify-installed-completion",
		"restore-resource", "publish-resource":
		return true
	default:
		return false
	}
}

type launcherRuntime interface {
	VerifyExecutingBinary() error
	ExecSelected([]string) error
	Close() error
}
type launcherDependencies struct {
	isEntry  func() (bool, error)
	pending  func() (bool, error)
	resume   func() error
	selected func() (launcherRuntime, error)
}

func dispatchLauncherWith(args []string, deps launcherDependencies) (result error) {
	entry, err := deps.isEntry()
	if err != nil || !entry {
		return err
	}
	// Only explicit owner/native recovery resumes a promotion. Capability and
	// material reads never become implicit selection mutations.
	// Yalnız açık kurtarma çağrısı geçişi sürdürür; okumalar seçim değiştirmez.
	if len(args) == 1 && args[0] == "recover" {
		pending, err := deps.pending()
		if err != nil {
			return err
		}
		if pending {
			if err := deps.resume(); err != nil {
				return err
			}
		}
	}
	selected, err := deps.selected()
	if err != nil {
		return err
	}
	if selected == nil {
		return recoveryruntime.ErrUnavailable
	}
	defer func() {
		if err := selected.Close(); err != nil {
			result = err
		}
	}()
	if selected.VerifyExecutingBinary() == nil {
		return nil
	}
	return selected.ExecSelected(args)
}
