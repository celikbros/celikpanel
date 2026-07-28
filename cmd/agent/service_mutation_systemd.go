package main

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const serviceMutationUnitWait = 8 * time.Second

// enableServiceForMutation runs the systemctl client in the mutation process
// group, then verifies the durable systemd state before the ledger step is
// released. Verification deliberately uses a fresh bounded context: PID 1 may
// have accepted the request just before cancellation killed the client.
// enableServiceForMutation, systemctl istemcisini değişiklik süreç grubunda
// çalıştırır ve defter adımı bırakılmadan önce kalıcı systemd durumunu doğrular.
// Doğrulama bilerek yeni ve sınırlı bir bağlam kullanır: iptal istemciyi
// öldürmeden hemen önce PID 1 isteği kabul etmiş olabilir.
func enableServiceForMutation(ctx context.Context, serviceName string, start bool) error {
	args := []string{"enable", serviceName}
	if start {
		args = []string{"enable", "--now", serviceName}
	}
	out, commandErr := runServiceMutationCombinedOutput(ctx, "systemctl", args...)
	verifyErr := verifyServiceMutationUnit(ctx, serviceName, start)
	if verifyErr == nil {
		return nil
	}
	if commandErr != nil {
		return fmt.Errorf("systemctl-%s-failed:%v:%s; reconciliation: %v",
			strings.Join(args, "-"), commandErr, strings.TrimSpace(string(out)), verifyErr)
	}
	return verifyErr
}

func verifyServiceMutationUnit(ctx context.Context, serviceName string, wantActive bool) error {
	deadline := time.Now().Add(serviceMutationUnitWait)
	if ctx == nil {
		ctx = context.Background()
	}
	// Reconciliation must survive cancellation of the mutating client, but it is
	// only a bounded, read-only probe. Shadow the durable execution tracker so a
	// late probe cannot register a new privileged worker after the mutation has
	// already entered cancelling/failed state.
	// Uzlaştırma, değişiklik yapan istemcinin iptalinden sonra da sürebilmelidir;
	// ancak yalnızca süreyle sınırlı, salt-okunur bir kontroldür. Geç kalan bir
	// kontrol, mutation cancelling/failed durumuna girdikten sonra yeni ayrıcalıklı
	// worker kaydedemesin diye kalıcı yürütme tracker'ını gölgele.
	reconcileCtx := context.WithValue(
		context.WithoutCancel(ctx),
		serviceMutationExecutionTrackerKey{},
		(*serviceMutationExecutionTracker)(nil),
	)
	for {
		probeCtx, cancel := context.WithTimeout(reconcileCtx, 2*time.Second)
		if !wantActive {
			out, err := runServiceMutationCombinedOutput(probeCtx, "systemctl", "is-enabled", serviceName)
			cancel()
			if err == nil && strings.TrimSpace(string(out)) == "enabled" {
				return nil
			}
			return fmt.Errorf("%s did not become enabled", serviceName)
		}

		out, err := runServiceMutationCombinedOutput(
			probeCtx,
			"systemctl", "show", serviceName,
			"--property=ActiveState,SubState,Result,ConditionResult",
			"--no-pager",
		)
		cancel()
		state := string(out)
		if err == nil && strings.Contains(state, "ActiveState=active") {
			return nil
		}
		if strings.Contains(state, "ActiveState=failed") ||
			strings.Contains(state, "ConditionResult=no") {
			return fmt.Errorf("%s did not become active (%s)", serviceName, firstLine(state))
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not become active (%s)", serviceName, firstLine(state))
		}
		time.Sleep(200 * time.Millisecond)
	}
}
