package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"
)

// Deferred startup mail work (upd11 F2, 2026-10-02).
//
// A Panel started inside an update or rollback finds the host lease held by
// the updater, so two startup steps are refused at admission ("another server
// change or package-manager task is still running") before any durable job or
// host change exists: the full mail SNI publication with the pending
// certificate outbox, and the Postfix milter/lookup-table wiring. Both used to
// run only at Panel start, so after a completed update they stayed undone until
// the next restart. This retries exactly those refused steps in the same
// process: only when startup reported the busy refusal, only after the advisory
// read-only readiness probe says the host is idle (the Agent's admission under
// the lease stays authoritative), at most startupDeferredRetryAttempts times,
// never again once a step completed, and with at most one journal line per
// attempt. Nothing is persisted and no privilege is added.
//
// Guncelleme ya da geri donus icinde baslayan Panel, iki baslangic adimini
// mesgul makine yuzunden atlar. Bu dosya yalniz o adimlari, makine bosaldiktan
// sonra, sinirli sayida ve her denemede en fazla bir gunluk satiriyla yeniden
// dener; hicbir sey kalici olarak yazilmaz.

const (
	startupDeferredRetryInterval = 30 * time.Second
	startupDeferredRetryAttempts = 20
	startupDeferredProbeTimeout  = 15 * time.Second

	startupDeferredRestartCommand = "sudo systemctl restart celikpanel-panel"
)

// startupDeferredRetryNote is appended to the startup line that reports the
// busy refusal, so an owner reading the journal knows nothing is lost and
// nothing is expected of them yet.
const startupDeferredRetryNote = "the Panel retries this by itself every 30 seconds " +
	"for up to 10 minutes once no other server change is running; nothing needs to be done now"

// startupCertificateDeferral is what the certificate startup pass hands to the
// deferred retry: whether its mail dependents step was refused as busy, and
// which pending-outbox domains that step would have completed.
type startupCertificateDeferral struct {
	deferred       bool
	pendingDomains []int
}

// startupWorkDeferredForBusyHost is the retry decision: only a refusal for a
// busy host is retried. A terminal-uncertain mutation is never retried here;
// it belongs to exact durable recovery.
func startupWorkDeferredForBusyHost(err error) bool {
	return err != nil &&
		errors.Is(err, errHostMutationBusy) &&
		!mutationTerminalUncertain(err)
}

type startupDeferredTask struct {
	name string
	// ownerAction is added to a failure or give-up line when the task has its
	// own owner-facing alternative to a Panel restart.
	ownerAction string
	run         func(context.Context) (string, error)
}

type startupDeferredRetry struct {
	interval time.Duration
	attempts int
	idle     func(context.Context) bool
	wait     func(context.Context, time.Duration) bool
	logf     func(string, ...any)
}

// run returns the names of the tasks that did not complete.
func (r startupDeferredRetry) run(
	ctx context.Context,
	tasks []startupDeferredTask,
) []string {
	pending := append([]startupDeferredTask(nil), tasks...)
	for attempt := 1; attempt <= r.attempts && len(pending) > 0; attempt++ {
		if !r.wait(ctx, r.interval) {
			return startupDeferredTaskNames(pending)
		}
		var outcomes []string
		if r.idle(ctx) {
			var still []startupDeferredTask
			for _, task := range pending {
				detail, err := task.run(ctx)
				switch {
				case err == nil:
					text := task.name + " completed"
					if detail != "" {
						text += " (" + detail + ")"
					}
					outcomes = append(outcomes, text)
				case startupWorkDeferredForBusyHost(err):
					still = append(still, task)
				default:
					outcomes = append(outcomes, fmt.Sprintf(
						"%s failed: %v; it is not retried now and runs again at the next Panel start (%s)%s",
						task.name, err, startupDeferredRestartCommand,
						startupDeferredOwnerActions([]startupDeferredTask{task}),
					))
				}
			}
			pending = still
		}
		if attempt == r.attempts && len(pending) > 0 {
			outcomes = append(outcomes, r.giveUpText(pending))
		}
		if len(outcomes) > 0 {
			r.logf(
				"startup mail work, attempt %d of %d: %s",
				attempt, r.attempts, strings.Join(outcomes, "; "),
			)
		}
	}
	return startupDeferredTaskNames(pending)
}

func (r startupDeferredRetry) giveUpText(pending []startupDeferredTask) string {
	window := r.interval * time.Duration(r.attempts)
	windowText := window.String()
	if window > 0 && window%time.Minute == 0 {
		windowText = fmt.Sprintf("%d minutes", int(window/time.Minute))
	}
	return fmt.Sprintf(
		"%s still not done after %s: the server did not become free for changes "+
			"(another server change or package-manager task was still running, or the "+
			"Agent could not confirm it was idle), and these attempts changed nothing. "+
			"Mail keeps running with its current configuration. When that task has "+
			"finished, the server administrator can run %s; the work runs at that start%s",
		strings.Join(startupDeferredTaskNames(pending), " and "),
		windowText,
		startupDeferredRestartCommand,
		startupDeferredOwnerActions(pending),
	)
}

func startupDeferredOwnerActions(tasks []startupDeferredTask) string {
	var actions []string
	for _, task := range tasks {
		if task.ownerAction != "" {
			actions = append(actions, task.ownerAction)
		}
	}
	if len(actions) == 0 {
		return ""
	}
	return "; " + strings.Join(actions, "; ")
}

func startupDeferredTaskNames(tasks []startupDeferredTask) []string {
	names := make([]string, 0, len(tasks))
	for _, task := range tasks {
		names = append(names, task.name)
	}
	return names
}

func waitStartupDeferredRetry(ctx context.Context, delay time.Duration) bool {
	if delay <= 0 {
		return ctx.Err() == nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

// deferredStartupMailTasks builds only the tasks startup reported as refused
// for a busy host. Nothing else is ever retried here.
func (p *Panel) deferredStartupMailTasks(
	certificates startupCertificateDeferral,
	milterWiring bool,
) []startupDeferredTask {
	var tasks []startupDeferredTask
	if certificates.deferred {
		tasks = append(tasks, p.deferredCertificateDependentsTask(certificates))
	}
	if milterWiring {
		tasks = append(tasks, p.deferredMilterWiringTask())
	}
	return tasks
}

// startDeferredStartupMailWork starts one bounded background retry when, and
// only when, startup deferred something. It reports whether it started.
func (p *Panel) startDeferredStartupMailWork(
	certificates startupCertificateDeferral,
	milterWiring bool,
) bool {
	tasks := p.deferredStartupMailTasks(certificates, milterWiring)
	if len(tasks) == 0 {
		return false
	}
	retry := startupDeferredRetry{
		interval: startupDeferredRetryInterval,
		attempts: startupDeferredRetryAttempts,
		idle:     p.startupDeferredHostIdle,
		wait:     waitStartupDeferredRetry,
		logf:     log.Printf,
	}
	go retry.run(context.Background(), tasks)
	return true
}

// startupDeferredHostIdle is the existing advisory readiness read, quiet so
// the retry keeps its own one-line-per-attempt budget.
func (p *Panel) startupDeferredHostIdle(ctx context.Context) bool {
	probeCtx, cancel := context.WithTimeout(ctx, startupDeferredProbeTimeout)
	defer cancel()
	return p.readHostMutationReadinessLogged(
		probeCtx, func(string, ...any) {},
	).Ready
}

// deferredCertificateDependentsTask repeats the refused startup step: the full
// mail SNI publication (and TLSA, a no-op in this release), then each pending
// outbox domain through the owner's own Retry path under that domain's SSL
// lock. Progress is kept in the closure, so a completed part never runs again.
func (p *Panel) deferredCertificateDependentsTask(
	deferral startupCertificateDeferral,
) startupDeferredTask {
	published := false
	publishedDetail := ""
	remaining := append([]int(nil), deferral.pendingDomains...)
	var failures []error
	return startupDeferredTask{
		name: "mail certificate publication",
		ownerAction: "a domain whose SSL page says mail TLS synchronization did not " +
			"finish can also use Retry activation there",
		run: func(ctx context.Context) (string, error) {
			if err := p.requireMatchingAgentBuild(ctx); err != nil {
				return "", fmt.Errorf("verify panel-agent build pair: %w", err)
			}
			if !published {
				publishCtx, cancel := context.WithTimeout(
					ctx, startupCertificateDependentTimeout,
				)
				count, err := p.reconcileCertificateDependentsAtStartup(publishCtx)
				cancel()
				if err != nil {
					return "", err
				}
				published = true
				publishedDetail = fmt.Sprintf(
					"mail SNI from %d active secure-mail certificates", count,
				)
			}
			for len(remaining) > 0 {
				domainID := remaining[0]
				err := p.retryDeferredStartupCertificate(ctx, domainID)
				if startupWorkDeferredForBusyHost(err) {
					// Keep this domain and the rest for the next attempt.
					return "", err
				}
				if err != nil {
					failures = append(failures, fmt.Errorf(
						"pending certificate for domain %d: %w", domainID, err,
					))
				}
				remaining = remaining[1:]
			}
			if len(failures) > 0 {
				return "", errors.Join(failures...)
			}
			detail := publishedDetail
			if len(deferral.pendingDomains) > 0 {
				detail += fmt.Sprintf(
					", %d pending certificate activations checked",
					len(deferral.pendingDomains),
				)
			}
			return detail, nil
		},
	}
}

// retryDeferredStartupCertificate finishes one outbox row only if it is still
// pending when this domain's SSL lock is held; a row another path already
// completed or replaced is left alone.
func (p *Panel) retryDeferredStartupCertificate(
	ctx context.Context,
	domainID int,
) error {
	unlock := lockDomainSSLOperation(domainID)
	defer unlock()
	var status string
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT COALESCE(renewal_status, '')
		FROM ssl_certificates
		WHERE domain_id = ? AND status = 'active'
		ORDER BY created_at DESC LIMIT 1`, domainID).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return err
	}
	if status != sslPendingActivation && status != sslPendingDependents {
		return nil
	}
	retryCtx, cancel := context.WithTimeout(ctx, sslMutationTimeout)
	defer cancel()
	return p.retryPendingCertificate(retryCtx, domainID)
}

// deferredMilterWiringTask repeats the refused startup wiring once. It holds
// the Panel's process mutation lock for the call, as an HTTP operation would,
// so it never interleaves with a composite Panel operation.
func (p *Panel) deferredMilterWiringTask() startupDeferredTask {
	return startupDeferredTask{
		name: "mail filter wiring",
		run: func(context.Context) (string, error) {
			present, detail := p.mailFilterWiringSubject()
			if !present {
				return "nothing to wire: " + detail, nil
			}
			if !p.serviceMutationMu.TryLock() {
				return "", errHostMutationBusy
			}
			defer p.serviceMutationMu.Unlock()
			response, err := p.wireMailFiltersOnce()
			if err != nil {
				return "", err
			}
			return response.Detail, nil
		},
	}
}
