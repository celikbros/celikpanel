package main

import (
	"context"
	"errors"
	"path/filepath"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

var errRecoveryCompatibility = errors.New("selected recovery runtime compatibility could not be verified")

const compatibilityCheckTimeout = 45 * time.Second

type compatibilityCommand struct {
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
		panel = compatibilityCommand{"panel-checker", []string{"--check-service-operations-idle-wal-aware"}}
		agentFlag = "--check-service-mutation-idle"
	case "--bootstrap-pre-ledger":
		panel = compatibilityCommand{"panel-checker", []string{"--check-pre-ledger-service-operations-idle-wal-aware"}}
	case "--bootstrap-schema17":
		panel = compatibilityCommand{"schema17-bridge", []string{"check", "--db", "/var/lib/celikpanel/celikpanel.db"}}
	default:
		return nil, errRecoveryCompatibility
	}
	return []compatibilityCommand{panel, {"agent-checker", []string{agentFlag}}}, nil
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
// Bagimlilik enjeksiyonu yalniz testler icindir. Kurulu giris yalniz sabit sunucu
// yollarini, secili runtime'i ve FD 9 uzerindeki yerel release kilidini saglar.
type compatibilityDependencies struct {
	euid     func() int
	boundary func(int) error
	resolve  func() (string, compatibilityRuntime, error)
	run      func(context.Context, string, []string, []string) error
}

func verifyRecoveryCompatibility(mode string, deps compatibilityDependencies) (result error) {
	commands, err := recoveryCompatibilityCommands(mode)
	if err != nil || deps.euid() != 0 {
		return errRecoveryCompatibility
	}
	if err := deps.boundary(9); err != nil {
		return errRecoveryCompatibility
	}
	root, selected, err := deps.resolve()
	if err != nil || selected == nil {
		return errRecoveryCompatibility
	}
	defer func() {
		if err := selected.Close(); err != nil {
			result = errRecoveryCompatibility
		}
	}()
	for _, command := range commands {
		if err := selected.Revalidate(); err != nil {
			return errRecoveryCompatibility
		}
		if err := deps.boundary(9); err != nil {
			return errRecoveryCompatibility
		}
		ctx, cancel := context.WithTimeout(context.Background(), compatibilityCheckTimeout)
		err := deps.run(ctx, filepath.Join(root, "bin", command.binary), command.args, recoveryCompatibilityEnvironment())
		deadlineErr := ctx.Err()
		cancel()
		if err != nil || deadlineErr != nil {
			return errRecoveryCompatibility
		}
		if err := selected.Revalidate(); err != nil {
			return errRecoveryCompatibility
		}
		if err := deps.boundary(9); err != nil {
			return errRecoveryCompatibility
		}
	}
	return nil
}
