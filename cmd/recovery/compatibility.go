package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

var errRecoveryCompatibility = errors.New("selected recovery runtime compatibility could not be verified")

const compatibilityCheckTimeout = 45 * time.Second

// A live Panel may commit to its WAL or checkpoint while the read-only checker
// holds its pinned copy; the checker then refuses closed ("changed after
// pinning"). One bounded re-read after this pause is a retry of a read, never
// of a mutation, and a still-failing check keeps its own diagnostic.
// Canlı panel yazımı sabitlenmiş kopyayı geçersiz kılabilir; salt-okur denetim
// bu kısa beklemeden sonra bir kez yeniden okunur.
const compatibilityCheckRetryDelay = 2 * time.Second

// Bounded, product-authored step names. The update summary carries them to the
// Panel and the browser, which translate known steps and ignore others.
const (
	compatibilityStepMode            = "mode"
	compatibilityStepOwner           = "owner"
	compatibilityStepReleaseBoundary = "release_boundary"
	compatibilityStepRuntimeSelect   = "runtime_selection"
	compatibilityStepRuntimeCheck    = "runtime_revalidation"
	compatibilityStepPanelDatabase   = "panel_database_check"
	compatibilityStepAgentLedger     = "agent_ledger_check"
	compatibilityStepOwnerMetadata   = "owner_metadata_probe"
)

// compatibilityError names the failed step and the checker's first diagnostic
// line. It never includes environment, arguments or file contents.
type compatibilityError struct {
	step, detail string
}

func (e *compatibilityError) Error() string {
	if e.detail == "" {
		return "step=" + e.step + ": " + errRecoveryCompatibility.Error()
	}
	return "step=" + e.step + ": " + e.detail
}

func (e *compatibilityError) Unwrap() error { return errRecoveryCompatibility }

// compatibilityFailure names a non-checker step only. Internal runtime, lock
// and metadata errors stay private: only the step identifies them.
func compatibilityFailure(step string, _ error) error {
	return &compatibilityError{step: step}
}

// firstDiagnosticLine keeps the first non-empty printable ASCII line, bounded
// to 240 bytes. Checkers print product-authored reasons; nothing else is read.
func firstDiagnosticLine(raw string) string {
	for _, line := range strings.Split(strings.ReplaceAll(raw, "\r", "\n"), "\n") {
		var cleaned strings.Builder
		for _, char := range line {
			if char >= 0x20 && char <= 0x7e {
				cleaned.WriteRune(char)
			}
		}
		value := strings.TrimSpace(cleaned.String())
		if value == "" {
			continue
		}
		if len(value) > 240 {
			value = value[:240]
		}
		return value
	}
	return ""
}

type compatibilityCommand struct {
	step   string
	binary string
	args   []string
}

// These are existing source-read-only checks, never migration or restoration
// commands. The live panel checker uses a private SQLite copy; schema17 retains
// its existing read-only SQLite connection semantics.
// Bunlar mevcut kaynagi salt okuyan kontrollerdir; tasima veya geri yukleme
// komutlari degildir. Canli panel kontrolu ozel SQLite kopyasini kullanir;
// schema17 mevcut salt-okur SQLite baglantisi davranisini korur.
func recoveryCompatibilityCommands(mode string) ([]compatibilityCommand, error) {
	var panel compatibilityCommand
	agentFlag := "--check-pre-ledger-service-mutation-idle"
	switch mode {
	case "--normal":
		panel = compatibilityCommand{compatibilityStepPanelDatabase, "panel-checker", []string{"--check-service-operations-idle-wal-aware"}}
		agentFlag = "--check-service-mutation-idle"
	case "--bootstrap-pre-ledger":
		panel = compatibilityCommand{compatibilityStepPanelDatabase, "panel-checker", []string{"--check-pre-ledger-service-operations-idle-wal-aware"}}
	case "--bootstrap-schema17":
		panel = compatibilityCommand{compatibilityStepPanelDatabase, "schema17-bridge", []string{"check", "--db", "/var/lib/celikpanel/celikpanel.db"}}
	default:
		return nil, errRecoveryCompatibility
	}
	return []compatibilityCommand{panel, {compatibilityStepAgentLedger, "agent-checker", []string{agentFlag}}}, nil
}

func recoveryCompatibilityEnvironment() []string {
	return []string{
		"PATH=/usr/sbin:/usr/bin:/sbin:/bin", "HOME=/root", "USER=root", "LOGNAME=root",
		"SHELL=/bin/bash", "LANG=C", "LC_ALL=C",
		"CELIKPANEL_DATA_DIR=/var/lib/celikpanel",
		"CELIKPANEL_AGENT_STATE_DIR=" + hostingpath.ServiceMutationStateRoot(),
		"CELIKPANEL_MUTATION_LOCK=/run/celikpanel/service-mutation.lock",
	}
}

type compatibilityRuntime interface {
	Revalidate() error
	Close() error
}

// Dependency injection is local to tests. The installed entry supplies only
// fixed host paths, the selected runtime and the native release lock on FD 9.
// run returns the checker's diagnostic output with its error.
// Bagimlilik enjeksiyonu yalniz testler icindir. Kurulu giris yalniz sabit sunucu
// yollarini, secili runtime'i ve FD 9 uzerindeki yerel release kilidini saglar.
type compatibilityDependencies struct {
	euid     func() int
	boundary func(int) error
	resolve  func() (string, compatibilityRuntime, error)
	run      func(context.Context, string, []string, []string) (string, error)
	sleep    func(time.Duration)
}

func verifyRecoveryCompatibility(mode string, deps compatibilityDependencies) error {
	return verifyRecoveryCompatibilityWithTimeout(mode, deps, compatibilityCheckTimeout)
}

func verifyRecoveryCompatibilityWithTimeout(mode string, deps compatibilityDependencies, timeout time.Duration) (result error) {
	commands, err := recoveryCompatibilityCommands(mode)
	if err != nil {
		return compatibilityFailure(compatibilityStepMode, nil)
	}
	if deps.euid() != 0 {
		return compatibilityFailure(compatibilityStepOwner, nil)
	}
	if err := deps.boundary(9); err != nil {
		return compatibilityFailure(compatibilityStepReleaseBoundary, err)
	}
	root, selected, err := deps.resolve()
	if err != nil || selected == nil {
		return compatibilityFailure(compatibilityStepRuntimeSelect, err)
	}
	defer func() {
		if err := selected.Close(); err != nil && result == nil {
			result = compatibilityFailure(compatibilityStepRuntimeCheck, err)
		}
	}()
	for _, command := range commands {
		var failure error
		for attempt := 0; attempt < 2; attempt++ {
			if attempt > 0 {
				if deps.sleep != nil {
					deps.sleep(compatibilityCheckRetryDelay)
				}
			}
			if err := selected.Revalidate(); err != nil {
				return compatibilityFailure(compatibilityStepRuntimeCheck, err)
			}
			if err := deps.boundary(9); err != nil {
				return compatibilityFailure(compatibilityStepReleaseBoundary, err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), timeout)
			output, err := deps.run(ctx, filepath.Join(root, "bin", command.binary), command.args, recoveryCompatibilityEnvironment())
			deadlineErr := ctx.Err()
			cancel()
			failure = nil
			if deadlineErr != nil {
				failure = &compatibilityError{step: command.step, detail: "the read-only check did not finish within its time limit"}
			} else if err != nil {
				failure = &compatibilityError{step: command.step, detail: firstDiagnosticLine(output)}
			}
			if err := selected.Revalidate(); err != nil {
				return compatibilityFailure(compatibilityStepRuntimeCheck, err)
			}
			if err := deps.boundary(9); err != nil {
				return compatibilityFailure(compatibilityStepReleaseBoundary, err)
			}
			// A timed-out check is not re-run: the bound stays one check time.
			if failure == nil || deadlineErr != nil {
				break
			}
		}
		if failure != nil {
			return failure
		}
	}
	return nil
}
