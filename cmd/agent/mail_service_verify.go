package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/hostcmd"
)

// Telling a running Postfix or Dovecot about a change, and knowing what came of
// it (10 Oct 2026; D-024, D-025 invariant 2).
//
// Measured on Ubuntu 24.04: `systemctl reload-or-restart postfix` exited 0 and
// the mail policy save answered "saved", while the journal said `Reload failed
// for postfix@-.service` and Postfix kept the previous settings. There
// `postfix.service` is a oneshot wrapper (`ExecStart=/bin/true`,
// `ExecReload=/bin/true`); the daemon belongs to `postfix@-.service`, and the
// wrapper's job result is all `systemctl` reports. Debian 13 and Arch ship one
// real `postfix.service` whose `ExecReload` is `postfix reload`; Ubuntu's
// instance runs `postmulti -i - -p reload`, which is `postfix reload` for the
// default instance. So the outcome is never inferred from `systemctl`'s exit
// status here:
//
//   - `postfix check` first: Postfix's own program reads main.cf and master.cf
//     and exits non-zero on what it refuses. That is a verified failure, with
//     its own line;
//   - a running master is reloaded with `postfix reload`, the command every
//     packaging runs as the unit's reload. It parses the configuration again,
//     confirms the master holds its lock, and signals that master; its exit
//     status is the instance's, not a wrapper's;
//   - a start or restart still goes through systemd, so the unit keeps owning
//     the daemon, and is then judged by `postfix status` and the master's
//     process ID, never by the job result alone;
//   - afterwards the master must still be running (`postfix status`). A master
//     that read a file it cannot use exits, and that is seen here.
//
// Postfix has no interface that reports the values a running master holds, so
// "took the new settings" rests on those three facts together: its own check
// accepted the files, its own reload command delivered the signal to the
// master that holds the lock, and that master is still running afterwards.
//
// Dovecot is one real unit on Debian, Ubuntu and Arch (`dovecot.service`), but
// with `Type=simple` a restart job succeeds as soon as the program was
// executed, before a configuration error makes it exit. So `doveconf -n` reads
// the configuration first, and after the restart the unit must be running with
// a main process that stays the same across two readings.
//
// Çalışan bir Postfix ya da Dovecot'a bir değişikliği bildirmek ve sonucunu
// bilmek. Ubuntu 24.04'te ölçüldü: `systemctl reload-or-restart postfix` 0 ile
// çıktı ve kayıt "kaydedildi" diye yanıtlandı; oysa Postfix önceki ayarlarla
// çalışmayı sürdürdü, çünkü orada `postfix.service` bir sarmalayıcıdır. Bu
// yüzden sonuç burada asla `systemctl` çıkış durumundan çıkarılmaz: önce
// Postfix'in kendi denetimi, sonra çalışan ana sürece Postfix'in kendi yeniden
// yükleme komutu, sonra ana sürecin hâlâ çalıştığının doğrulanması.

// mailServiceRunner runs one fixed command and returns everything it printed.
type mailServiceRunner func(name string, args ...string) ([]byte, error)

type mailServiceMode int

const (
	// Reload a running daemon. A stopped one is left stopped: it reads the
	// files when its owner starts it.
	mailServiceReload mailServiceMode = iota + 1
	// Reload a running daemon, start a stopped one (setup).
	mailServiceReloadOrStart
	// Restart, or start when stopped (a change only a new process takes up).
	mailServiceRestart
	// Start a stopped daemon; a running one is left as it is (the owner's
	// "Start" on the Services page).
	mailServiceStart
)

// What a verified apply came to.
const (
	mailServiceReloaded   = "reloaded"
	mailServiceStarted    = "started"
	mailServiceRestarted  = "restarted"
	mailServiceNotRunning = "not_running"
	// The daemon was already running and nothing was done to it.
	mailServiceRunning = "running"
	mailServiceStopped = "stopped"
)

// The stage a mail service apply stopped at.
const (
	mailServiceStageCheck  = "check"
	mailServiceStageReload = "reload"
	mailServiceStageStart  = "start"
	mailServiceStageStop   = "stop"
	mailServiceStageVerify = "verify"
)

// mailServiceError is why a mail service did not take a change. unknown is true
// when the outcome could not be established at all (a command could not be run
// or did not answer in time): that is not a verified failure and must not be
// reported as one, nor as success.
type mailServiceError struct {
	service string
	stage   string
	unknown bool
	// detail is one bounded line: what the service's own program said, or what
	// this check observed.
	detail string
}

func (e *mailServiceError) Error() string {
	what := map[string]string{
		mailServiceStageCheck:  "refuses its configuration",
		mailServiceStageReload: "could not be reloaded",
		mailServiceStageStart:  "could not be started",
		mailServiceStageStop:   "could not be stopped",
		mailServiceStageVerify: "is not running with the new configuration",
	}[e.stage]
	if e.unknown {
		what = "could not be checked (" + e.stage + ")"
	}
	if e.detail == "" {
		return e.service + " " + what
	}
	return e.service + " " + what + ": " + e.detail
}

// Swapped by tests.
// Testlerde değiştirilir.
var (
	mailServiceSleep    = time.Sleep
	mailServiceReadFile = os.ReadFile
	// mailServiceExited reports whether err is the command's own exit status.
	// Anything else (not found, killed at a deadline, a lost lease) says
	// nothing about the service.
	mailServiceExited = func(err error) bool {
		if err == nil || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return false
		}
		var exited *exec.ExitError
		return errors.As(err, &exited) && exited.ExitCode() > 0
	}
)

const (
	// How long a start or restart may take to show a running daemon, and how
	// long a reloaded one is given to exit if it is going to.
	mailServiceStartPolls   = 30
	mailServicePollInterval = 500 * time.Millisecond
	mailServiceSettle       = 400 * time.Millisecond
)

var mailServiceSecret = dbConfigSecret

// mailServiceLine picks the one line of a command's output worth showing: the
// first that names a failure, else the first that says anything. Password
// assignments are blanked and the line is bounded.
func mailServiceLine(out []byte) string {
	first := ""
	for _, raw := range strings.Split(string(out), "\n") {
		line := strings.Join(strings.Fields(raw), " ")
		if line == "" {
			continue
		}
		if first == "" {
			first = line
		}
		lower := strings.ToLower(line)
		if strings.Contains(lower, "fatal") || strings.Contains(lower, "error") || strings.Contains(lower, "failed") {
			first = line
			break
		}
	}
	if first == "" {
		return ""
	}
	return hostcmd.Bounded(mailServiceSecret.ReplaceAllString(first, "${1}…"), 300)
}

func mailServiceFailure(service, stage string, out []byte, err error) *mailServiceError {
	failure := &mailServiceError{service: service, stage: stage, detail: mailServiceLine(out)}
	if !mailServiceExited(err) {
		failure.unknown = true
		if failure.detail == "" && err != nil {
			failure.detail = hostcmd.Bounded(strings.Join(strings.Fields(err.Error()), " "), 300)
		}
	}
	return failure
}

type postfixMasterState struct {
	running bool
	// pid is the master's process ID, 0 when it could not be read.
	pid int
}

// postfixMaster asks Postfix whether its master is running. It must be called
// after `postfix check` accepted the configuration: then a non-zero exit of
// `postfix status` is its answer "not running", not a refusal of main.cf.
func postfixMaster(run mailServiceRunner) (postfixMasterState, *mailServiceError) {
	out, err := run("postfix", "status")
	if err != nil {
		if mailServiceExited(err) {
			return postfixMasterState{}, nil
		}
		return postfixMasterState{}, mailServiceFailure("Postfix", mailServiceStageVerify, out, err)
	}
	state := postfixMasterState{running: true}
	if queue, err := run("postconf", "-h", "queue_directory"); err == nil {
		directory := strings.TrimSpace(string(queue))
		if path.IsAbs(directory) {
			if data, err := mailServiceReadFile(path.Join(directory, "pid", "master.pid")); err == nil {
				state.pid, _ = strconv.Atoi(strings.TrimSpace(string(data)))
			}
		}
	}
	return state, nil
}

// applyPostfixVerified makes the running Postfix take up the configuration that
// is on disk and returns what happened (a mailService* state). A nil error
// means the answer is verified; a *mailServiceError says whether it is a
// verified failure or an unknown outcome.
func applyPostfixVerified(run mailServiceRunner, mode mailServiceMode) (string, error) {
	const service = "Postfix"
	if out, err := run("postfix", "check"); err != nil {
		return "", mailServiceFailure(service, mailServiceStageCheck, out, err)
	}
	before, unknown := postfixMaster(run)
	if unknown != nil {
		return "", unknown
	}

	if mode == mailServiceStart && before.running {
		return mailServiceRunning, nil
	}
	if mode == mailServiceRestart || (!before.running && (mode == mailServiceReloadOrStart || mode == mailServiceStart)) {
		action, state := "restart", mailServiceRestarted
		if !before.running {
			state = mailServiceStarted
		}
		if mode != mailServiceRestart {
			action = "start"
		}
		if out, err := run("systemctl", action, "postfix"); err != nil {
			return "", mailServiceFailure(service, mailServiceStageStart, out, err)
		}
		// The unit named postfix is a wrapper on some systems: its job result
		// says nothing about the daemon, and the instance may still be
		// starting when systemctl returns.
		var after postfixMasterState
		for attempt := 0; attempt < mailServiceStartPolls; attempt++ {
			if attempt > 0 {
				mailServiceSleep(mailServicePollInterval)
			}
			if after, unknown = postfixMaster(run); unknown != nil {
				return "", unknown
			}
			if after.running && !(before.running && before.pid != 0 && after.pid == before.pid) {
				return state, nil
			}
		}
		failure := &mailServiceError{service: service, stage: mailServiceStageStart,
			detail: "systemctl " + action + " postfix reported success, but the Postfix master process is not running"}
		if after.running {
			failure.detail = fmt.Sprintf("systemctl %s postfix reported success, but the same Postfix master process (%d) is still running: the restart did not reach it", action, after.pid)
		}
		return "", failure
	}

	if !before.running {
		return mailServiceNotRunning, nil
	}
	if out, err := run("postfix", "reload"); err != nil {
		return "", mailServiceFailure(service, mailServiceStageReload, out, err)
	}
	mailServiceSleep(mailServiceSettle)
	after, unknown := postfixMaster(run)
	if unknown != nil {
		return "", unknown
	}
	if !after.running {
		return "", &mailServiceError{service: service, stage: mailServiceStageVerify,
			detail: "the Postfix master process stopped while it was reloading"}
	}
	return mailServiceReloaded, nil
}

type dovecotUnitState struct {
	active bool
	pid    int
}

func dovecotUnit(run mailServiceRunner) (dovecotUnitState, *mailServiceError) {
	out, err := run("systemctl", "show", "dovecot.service",
		"--property=ActiveState", "--property=SubState", "--property=MainPID")
	if err != nil {
		failure := mailServiceFailure("Dovecot", mailServiceStageVerify, out, err)
		failure.unknown = true
		return dovecotUnitState{}, failure
	}
	values := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		if name, value, ok := strings.Cut(strings.TrimSpace(line), "="); ok {
			values[name] = value
		}
	}
	state := dovecotUnitState{}
	state.pid, _ = strconv.Atoi(values["MainPID"])
	state.active = values["ActiveState"] == "active" && values["SubState"] == "running" && state.pid > 0
	return state, nil
}

// applyDovecotVerified restarts Dovecot (or reloads it, starting it when it is
// stopped) and returns only once the unit is running with a main process that
// stayed the same across two readings.
func applyDovecotVerified(run mailServiceRunner, mode mailServiceMode) (string, error) {
	const service = "Dovecot"
	if out, err := run("doveconf", "-n"); err != nil {
		return "", mailServiceFailure(service, mailServiceStageCheck, out, err)
	}
	before, unknown := dovecotUnit(run)
	if unknown != nil {
		return "", unknown
	}
	action, stage, state := "restart", mailServiceStageStart, mailServiceRestarted
	switch mode {
	case mailServiceRestart:
	case mailServiceStart:
		// The owner's "Start": a running Dovecot is left as it is.
		if before.active {
			return mailServiceRunning, nil
		}
		action = "start"
	case mailServiceReload:
		// The owner's "Reload": a stopped Dovecot is left stopped.
		if !before.active {
			return mailServiceNotRunning, nil
		}
		action, stage, state = "reload", mailServiceStageReload, mailServiceReloaded
	default:
		action, stage, state = "reload-or-restart", mailServiceStageReload, mailServiceReloaded
	}
	if !before.active {
		state = mailServiceStarted
	}
	if out, err := run("systemctl", action, "dovecot"); err != nil {
		return "", mailServiceFailure(service, stage, out, err)
	}
	var seen dovecotUnitState
	for attempt := 0; attempt < mailServiceStartPolls; attempt++ {
		mailServiceSleep(mailServicePollInterval)
		now, unknown := dovecotUnit(run)
		if unknown != nil {
			return "", unknown
		}
		// Two readings in a row with the same main process: a program that
		// exits on its configuration does not last that long.
		if now.active && seen.active && now.pid == seen.pid {
			if mode == mailServiceRestart && before.active && now.pid == before.pid {
				return "", &mailServiceError{service: service, stage: mailServiceStageVerify,
					detail: fmt.Sprintf("systemctl restart dovecot reported success, but the same main process (%d) is still running", now.pid)}
			}
			return state, nil
		}
		seen = now
	}
	return "", &mailServiceError{service: service, stage: mailServiceStageVerify,
		detail: "systemctl " + action + " dovecot reported success, but dovecot.service is not running afterwards"}
}

// postfixMasterProcess reads from the kernel whether the Postfix master is
// alive, without asking Postfix's own programs (11 Oct 2026).
//
// `postfix status` reads main.cf before it answers, so with a main.cf Postfix
// refuses it exits non-zero whatever the master does (measured on Debian 13 and
// Ubuntu 24.04: a Stop that had ended the master was answered as unknown). The
// master writes its process ID to `<queue_directory>/pid/master.pid` when it
// starts and leaves the file there when it exits, so the file alone says
// nothing; the process it names does: it is the master while `/proc/<pid>/comm`
// reads `master`, and it is gone when that entry does not exist or belongs to
// another program.
//
// known is false when one of these could not be read: the queue directory was
// not answered, or a file could not be read for a reason other than "it does
// not exist". Then nothing is claimed.
//
// postfixMasterProcess, Postfix ana sürecinin yaşayıp yaşamadığını Postfix'in
// kendi programlarına sormadan çekirdekten okur. Okunamayan bir şey varsa
// known false olur ve hiçbir şey ileri sürülmez.
func postfixMasterProcess(run mailServiceRunner) (alive bool, pid int, known bool) {
	queue, err := run("postconf", "-h", "queue_directory")
	if err != nil {
		return false, 0, false
	}
	directory := strings.TrimSpace(string(queue))
	if !path.IsAbs(directory) {
		return false, 0, false
	}
	gone := func(err error) bool {
		return errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ESRCH)
	}
	data, err := mailServiceReadFile(path.Join(directory, "pid", "master.pid"))
	if err != nil {
		// No file: no master ever started with this queue directory.
		return false, 0, gone(err)
	}
	pid, err = strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return false, 0, false
	}
	name, err := mailServiceReadFile("/proc/" + strconv.Itoa(pid) + "/comm")
	if err != nil {
		return false, pid, gone(err)
	}
	return strings.TrimSpace(string(name)) == "master", pid, true
}

// stopPostfixVerified stops Postfix through its unit and returns only once
// the master process is seen to be gone.
//
// A stop never depends on `postfix check` (11 Oct 2026): the master's own
// process is looked for (postfixMasterProcess), and a master that is gone is
// "stopped" whatever main.cf holds. Only when the process cannot be looked for
// is Postfix asked itself, as before: `postfix status` can be believed only
// when Postfix accepts its configuration, so with a refused configuration and
// no way to see the process the outcome is unknown.
//
// stopPostfixVerified, Postfix'i unit'i üzerinden durdurur ve ancak ana sürecin
// gittiği görüldüğünde döner. Durdurma hiçbir zaman `postfix check` sonucuna
// bağlı değildir; yalnızca süreç görülemiyorsa Postfix'in kendisine sorulur.
func stopPostfixVerified(run mailServiceRunner) (string, error) {
	const service = "Postfix"
	if out, err := run("systemctl", "stop", "postfix"); err != nil {
		return "", mailServiceFailure(service, mailServiceStageStop, out, err)
	}
	seen := 0
	for attempt := 0; attempt < mailServiceStartPolls; attempt++ {
		if attempt > 0 {
			mailServiceSleep(mailServicePollInterval)
		}
		alive, pid, known := postfixMasterProcess(run)
		if !known {
			seen = -1
			break
		}
		if !alive {
			return mailServiceStopped, nil
		}
		seen = pid
	}
	if seen > 0 {
		return "", &mailServiceError{service: service, stage: mailServiceStageStop,
			detail: fmt.Sprintf("systemctl stop postfix reported success, but the Postfix master process (%d) is still running", seen)}
	}
	// The process could not be looked for; Postfix is asked itself.
	checkOutput, checkErr := run("postfix", "check")
	if checkErr != nil {
		unknown := mailServiceFailure(service, mailServiceStageVerify, checkOutput, checkErr)
		unknown.unknown = true
		unknown.detail = strings.TrimSuffix("systemctl stop postfix reported success, but the master process could not be looked for and `postfix check` did not pass, so `postfix status` cannot say whether the master stopped: "+unknown.detail, ": ")
		return "", unknown
	}
	var after postfixMasterState
	for attempt := 0; attempt < mailServiceStartPolls; attempt++ {
		if attempt > 0 {
			mailServiceSleep(mailServicePollInterval)
		}
		var unknown *mailServiceError
		if after, unknown = postfixMaster(run); unknown != nil {
			return "", unknown
		}
		if !after.running {
			return mailServiceStopped, nil
		}
	}
	return "", &mailServiceError{service: service, stage: mailServiceStageStop,
		detail: fmt.Sprintf("systemctl stop postfix reported success, but the Postfix master process (%d) is still running", after.pid)}
}

// stopDovecotVerified stops Dovecot and returns once its unit has no main
// process left.
func stopDovecotVerified(run mailServiceRunner) (string, error) {
	const service = "Dovecot"
	if out, err := run("systemctl", "stop", "dovecot"); err != nil {
		return "", mailServiceFailure(service, mailServiceStageStop, out, err)
	}
	var now dovecotUnitState
	for attempt := 0; attempt < mailServiceStartPolls; attempt++ {
		if attempt > 0 {
			mailServiceSleep(mailServicePollInterval)
		}
		var unknown *mailServiceError
		if now, unknown = dovecotUnit(run); unknown != nil {
			return "", unknown
		}
		if !now.active && now.pid == 0 {
			return mailServiceStopped, nil
		}
	}
	return "", &mailServiceError{service: service, stage: mailServiceStageStop,
		detail: fmt.Sprintf("systemctl stop dovecot reported success, but its main process (%d) is still running", now.pid)}
}

// mailServiceLeaseRunner runs each command under a durable mutation lease.
func mailServiceLeaseRunner(ctx context.Context) mailServiceRunner {
	return func(name string, args ...string) ([]byte, error) {
		return runMailTLSMutationCommand(ctx, name, args...)
	}
}
