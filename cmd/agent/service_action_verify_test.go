package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func wantServiceAction(t *testing.T, got transport.ServiceActionResult, outcome, stage string) {
	t.Helper()
	if got.Outcome != outcome || got.Stage != stage {
		t.Fatalf("answer = %+v, want outcome %q stage %q", got, outcome, stage)
	}
	// Unknown and failed are never success, and always say something.
	if success := outcome == transport.ServiceActionVerified || outcome == ""; got.Success != success || (!success && got.Error == "") {
		t.Fatalf("answer = %+v: success and error do not match outcome %q", got, outcome)
	}
}

func calledWith(calls []string, call string) bool {
	for _, made := range calls {
		if made == call {
			return true
		}
	}
	return false
}

// The Services page's "Reload" on Postfix. On Ubuntu `systemctl reload postfix`
// exits 0 for the wrapper unit whatever the daemon does, so it is not asked:
// Postfix's own check refuses the configuration, and that is the answer.
func TestServicesPageReloadOfPostfixIsAnsweredByPostfixItself(t *testing.T) {
	host := installFakeMailHost(t)
	host.checkOutput = "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign\n"

	got := verifiedServiceAction(host.run, "postfix", "postfix", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageCheck)
	if !strings.Contains(got.Detail, "default_process_limit") {
		t.Fatalf("Postfix's own line is missing: %+v", got)
	}
	for _, call := range host.calls {
		if strings.HasPrefix(call, "systemctl") || call == "postfix reload" {
			t.Fatalf("a refused configuration still led to %q", call)
		}
	}

	host = installFakeMailHost(t)
	host.reloadOutput = "postfix/postfix-script: fatal: the Postfix mail system is not running\n"
	got = verifiedServiceAction(host.run, "postfix", "postfix", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageReload)

	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "postfix", "postfix", "reload")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceReloaded || !calledWith(host.calls, "postfix reload") || calledWith(host.calls, "systemctl reload postfix") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}

	// A stopped Postfix cannot be reloaded, and is not started by a reload.
	host = installFakeMailHost(t)
	host.masterPID = 0
	got = verifiedServiceAction(host.run, "postfix", "postfix", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageReload)
	if !strings.Contains(got.Detail, "not running") || calledWith(host.calls, "systemctl start postfix") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}
}

// "Restart", "Start" and "Stop" on a wrapper unit: systemctl exits 0 and the
// daemon is where it was. Each is a verified failure, never a success.
func TestServicesPageActionsOnPostfixAreJudgedByTheMasterProcess(t *testing.T) {
	host := installFakeMailHost(t)
	host.systemctlStarts = false
	got := verifiedServiceAction(host.run, "postfix", "postfix", "restart")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStart)
	if !strings.Contains(got.Detail, "the restart did not reach it") {
		t.Fatalf("answer = %+v", got)
	}

	host = installFakeMailHost(t)
	host.masterPID, host.systemctlStarts = 0, false
	got = verifiedServiceAction(host.run, "postfix", "postfix", "start")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStart)

	host = installFakeMailHost(t)
	host.stopLeavesMaster = true
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStop)
	if !strings.Contains(got.Detail, "still running") {
		t.Fatalf("answer = %+v", got)
	}

	// The healthy answers.
	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "postfix", "postfix", "restart")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceRestarted {
		t.Fatalf("answer = %+v", got)
	}
	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceStopped {
		t.Fatalf("answer = %+v", got)
	}
	host = installFakeMailHost(t)
	host.masterPID = 0
	got = verifiedServiceAction(host.run, "postfix", "postfix", "start")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceStarted {
		t.Fatalf("answer = %+v", got)
	}
	// "Start" on a running Postfix changes nothing and says so.
	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "postfix", "postfix", "start")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceRunning || calledWith(host.calls, "systemctl start postfix") || calledWith(host.calls, "postfix reload") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}
}

// What cannot be verified is unknown, with the reason; it is not success and
// it is not a verified failure.
func TestServicesPageActionOnPostfixIsUnknownWhenItCannotBeChecked(t *testing.T) {
	host := installFakeMailHost(t)
	host.statusCannotRun = true
	got := verifiedServiceAction(host.run, "postfix", "postfix", "restart")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageVerify)

	// A stop is never held back by a refused configuration, but then Postfix
	// cannot say whether its master stopped.
	host = installFakeMailHost(t)
	host.checkOutput = "postfix: fatal: /etc/postfix/main.cf, line 12: missing '=' after attribute name\n"
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageVerify)
	if !calledWith(host.calls, "systemctl stop postfix") || !strings.Contains(got.Detail, "line 12") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}
}

func TestServicesPageActionsOnDovecotAreVerified(t *testing.T) {
	// Reload: `systemctl reload`, never reload-or-restart, and the main
	// process must stay.
	host := installFakeMailHost(t)
	got := verifiedServiceAction(host.run, "dovecot", "dovecot", "reload")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceReloaded || !calledWith(host.calls, "systemctl reload dovecot") || calledWith(host.calls, "systemctl reload-or-restart dovecot") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}

	// A stopped Dovecot is not started by a reload.
	host = installFakeMailHost(t)
	host.dovecotPID = 0
	got = verifiedServiceAction(host.run, "dovecot", "dovecot", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageReload)
	for _, call := range host.calls {
		if strings.HasPrefix(call, "systemctl re") || strings.HasPrefix(call, "systemctl start") {
			t.Fatalf("a stopped Dovecot was sent %q", call)
		}
	}

	// Dovecot refuses its configuration: nothing is restarted.
	host = installFakeMailHost(t)
	host.doveconfOutput = "doveconf: Fatal: Error in configuration file /etc/dovecot/conf.d/10-ssl.conf line 14: Unknown setting: ssl_cert_filee\n"
	got = verifiedServiceAction(host.run, "dovecot", "dovecot", "restart")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageCheck)
	if calledWith(host.calls, "systemctl restart dovecot") || !strings.Contains(got.Detail, "Unknown setting") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}

	// The start job succeeded and the program exited afterwards.
	host = installFakeMailHost(t)
	host.dovecotPID, host.dovecotExitsAt = 0, 1
	got = verifiedServiceAction(host.run, "dovecot", "dovecot", "start")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageVerify)

	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "dovecot", "dovecot", "start")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceRunning || calledWith(host.calls, "systemctl start dovecot") {
		t.Fatalf("answer = %+v, calls = %v", got, host.calls)
	}

	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "dovecot", "dovecot", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceStopped {
		t.Fatalf("answer = %+v", got)
	}
	host = installFakeMailHost(t)
	host.dovecotStopLeaves = true
	got = verifiedServiceAction(host.run, "dovecot", "dovecot", "stop")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStop)
}

// fakeSystemd is the service manager as the generic service action sees it:
// what `systemctl show` reports for each unit, and what an action does.
type fakeSystemd struct {
	calls     []string
	units     map[string]map[string][]string
	showFails map[string]bool
	// act stands for `systemctl <action> <unit>`: it changes the units as the
	// real one would and returns that command's output and exit status.
	act func(s *fakeSystemd, action, unit string) ([]byte, error)
}

func (s *fakeSystemd) run(name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	s.calls = append(s.calls, call)
	if name != "systemctl" || len(args) < 2 {
		return nil, errors.New("unexpected command: " + call)
	}
	if args[0] != "show" {
		if s.act == nil {
			return nil, nil
		}
		return s.act(s, args[0], args[1])
	}
	unit := args[1]
	if s.showFails[unit] {
		return []byte("Failed to get properties: Connection timed out\n"), fakeExit{"exit status 1"}
	}
	var out strings.Builder
	for _, argument := range args[2:] {
		property := strings.TrimPrefix(argument, "--property=")
		values, known := s.units[unit][property]
		if !known {
			// systemctl prints nothing for a property the unit type lacks.
			continue
		}
		for _, value := range values {
			out.WriteString(property + "=" + value + "\n")
		}
	}
	return []byte(out.String()), nil
}

func (s *fakeSystemd) set(unit, property string, values ...string) {
	if s.units[unit] == nil {
		s.units[unit] = map[string][]string{}
	}
	s.units[unit][property] = values
}

const (
	// As `systemctl show -p ExecReload` printed it on Debian 13 (set1 evidence,
	// 11-s4-postgresql): before any reload, and after one that ran.
	pgReloadNeverRan = "{ path=/usr/bin/pg_ctlcluster ; argv[]=/usr/bin/pg_ctlcluster --skip-systemctl-redirect 17-main reload ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }"
	pgReloadRan      = "{ path=/usr/bin/pg_ctlcluster ; argv[]=/usr/bin/pg_ctlcluster --skip-systemctl-redirect 17-main reload ; ignore_errors=no ; start_time=[Thu 2026-10-08 21:27:40 UTC] ; stop_time=[Thu 2026-10-08 21:27:40 UTC] ; pid=9120 ; code=exited ; status=0 }"
	pgCluster        = "postgresql@17-main.service"
)

// debianPostgreSQL is `postgresql.service` as Debian and Ubuntu package it: a
// oneshot that runs /bin/true, with the cluster's unit behind it.
func debianPostgreSQL(t *testing.T) *fakeSystemd {
	t.Helper()
	installFakeMailHost(t) // the seams: no sleeping, fakeExit is an exit status
	s := &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{}}
	s.set("postgresql.service", "Type", "oneshot")
	s.set("postgresql.service", "ExecStart", "{ path=/bin/true ; argv[]=/bin/true ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }")
	s.set("postgresql.service", "Wants", pgCluster+" system.slice")
	s.set("postgresql.service", "ConsistsOf", pgCluster)
	s.set("postgresql.service", "PropagatesReloadTo", pgCluster)
	s.set(pgCluster, "ActiveState", "active")
	s.set(pgCluster, "SubState", "running")
	s.set(pgCluster, "MainPID", "5120")
	s.set(pgCluster, "Result", "success")
	s.set(pgCluster, "ReloadResult", "success")
	s.set(pgCluster, "ExecReload", pgReloadNeverRan)
	return s
}

// The wrapper's job succeeds; the cluster behind it is what is judged.
func TestServicesPageActionOnAWrapperUnitIsJudgedByTheUnitsBehindIt(t *testing.T) {
	// Restart: the cluster failed to come back.
	s := debianPostgreSQL(t)
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.set(pgCluster, "ActiveState", "failed")
		s.set(pgCluster, "SubState", "failed")
		s.set(pgCluster, "MainPID", "0")
		s.set(pgCluster, "Result", "exit-code")
		return nil, nil
	}
	got := verifiedServiceAction(s.run, "postgresql", "postgresql", "restart")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStart)
	if got.Unit != pgCluster || !strings.Contains(got.Detail, "failed") || !strings.Contains(got.Detail, "exit-code") {
		t.Fatalf("answer = %+v", got)
	}

	// Restart: the same server process is still there.
	s = debianPostgreSQL(t)
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "restart")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageVerify)
	if !strings.Contains(got.Detail, "5120") {
		t.Fatalf("answer = %+v", got)
	}

	// Restart: a new server process, waited for while it starts.
	s = debianPostgreSQL(t)
	readings := 0
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.set(pgCluster, "ActiveState", "activating")
		s.set(pgCluster, "MainPID", "0")
		return nil, nil
	}
	settle := s.run
	run := func(name string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[0] == "show" && args[1] == pgCluster && calledWith(s.calls, "systemctl restart postgresql") {
			if readings++; readings == 3 {
				s.set(pgCluster, "ActiveState", "active")
				s.set(pgCluster, "MainPID", "6240")
			}
		}
		return settle(name, args...)
	}
	got = verifiedServiceAction(run, "postgresql", "postgresql", "restart")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceRestarted || got.Unit != pgCluster {
		t.Fatalf("answer = %+v", got)
	}

	// Stop: the cluster is still running.
	s = debianPostgreSQL(t)
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "stop")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStop)

	// Start: the cluster is up.
	s = debianPostgreSQL(t)
	s.set(pgCluster, "ActiveState", "inactive")
	s.set(pgCluster, "SubState", "dead")
	s.set(pgCluster, "MainPID", "0")
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.set(pgCluster, "ActiveState", "active")
		s.set(pgCluster, "SubState", "running")
		s.set(pgCluster, "MainPID", "7001")
		return nil, nil
	}
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "start")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceStarted {
		t.Fatalf("answer = %+v", got)
	}
}

// "Reload" on the wrapper: `ExecReload=/bin/true` exits 0. The cluster's own
// reload command must have run, and systemd must say it succeeded.
func TestServicesPageReloadOfAWrapperUnitNeedsTheReloadBehindIt(t *testing.T) {
	// The reload never reached the cluster.
	s := debianPostgreSQL(t)
	got := verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageReload)
	if got.Unit != pgCluster || !strings.Contains(got.Detail, "did not reach") {
		t.Fatalf("answer = %+v", got)
	}

	// It ran and failed (the owner's reload hook of the set1 run).
	s = debianPostgreSQL(t)
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.set(pgCluster, "ExecReload", pgReloadRan)
		s.set(pgCluster, "ReloadResult", "exit-code")
		return nil, nil
	}
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageReload)
	if !strings.Contains(got.Detail, "exit-code") || !strings.Contains(got.Detail, "previous settings") {
		t.Fatalf("answer = %+v", got)
	}

	// It ran and succeeded.
	s = debianPostgreSQL(t)
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.set(pgCluster, "ExecReload", pgReloadRan)
		return nil, nil
	}
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceReloaded {
		t.Fatalf("answer = %+v", got)
	}

	// A systemd that does not report the reload's result: unknown.
	s = debianPostgreSQL(t)
	delete(s.units[pgCluster], "ReloadResult")
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.set(pgCluster, "ExecReload", pgReloadRan)
		return nil, nil
	}
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageVerify)

	// Nothing behind the wrapper is running: nothing was reloaded.
	s = debianPostgreSQL(t)
	s.set(pgCluster, "ActiveState", "inactive")
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageReload)
	if !strings.Contains(got.Detail, "nothing was reloaded") {
		t.Fatalf("answer = %+v", got)
	}
}

// What stands behind a wrapper could not be read: nothing is sent, because its
// result could not be told afterwards. A wrapper with nothing behind it is
// unknown too, never a success.
func TestServicesPageActionOnAWrapperUnitIsUnknownWithoutEvidence(t *testing.T) {
	s := debianPostgreSQL(t)
	s.showFails[pgCluster] = true
	got := verifiedServiceAction(s.run, "postgresql", "postgresql", "restart")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageCheck)
	if calledWith(s.calls, "systemctl restart postgresql") || !strings.HasPrefix(got.Error, "nothing was changed") {
		t.Fatalf("answer = %+v, calls = %v", got, s.calls)
	}

	s = debianPostgreSQL(t)
	s.set("postgresql.service", "Wants", "system.slice")
	s.set("postgresql.service", "ConsistsOf")
	s.set("postgresql.service", "PropagatesReloadTo")
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "start")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageVerify)

	// The state cannot be read after the action.
	s = debianPostgreSQL(t)
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		s.showFails[pgCluster] = true
		return nil, nil
	}
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "stop")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageVerify)
}

// A unit that runs its own daemon keeps its job result, in the words it always
// had: Arch's `postgresql.service`, nginx, and an instance unit named directly
// (which is not even asked what it is).
func TestServicesPageActionOnARealUnitKeepsItsJobResult(t *testing.T) {
	installFakeMailHost(t)
	s := &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{}}
	s.set("postgresql.service", "Type", "notify")
	s.set("postgresql.service", "ExecStart", "{ path=/usr/bin/postgres ; argv[]=/usr/bin/postgres -D /var/lib/postgres/data ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }")
	got := verifiedServiceAction(s.run, "postgresql", "postgresql", "restart")
	if !got.Success || got.Outcome != "" || got.Error != "" {
		t.Fatalf("answer = %+v", got)
	}

	// A oneshot that runs a real program is not a wrapper (wg-quick).
	s.set("wg-quick@wg0.service", "Type", "oneshot")
	got = verifiedServiceAction(s.run, "wireguard", "wg-quick@wg0", "restart")
	if !got.Success || got.Outcome != "" || calledWith(s.calls, "systemctl show wg-quick@wg0.service --property=Type --property=ExecStart --property=Wants --property=ConsistsOf --property=PropagatesReloadTo") {
		t.Fatalf("answer = %+v, calls = %v", got, s.calls)
	}

	// systemd's own refusal, as before, now classified.
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		return []byte("Job for nginx.service failed because the control process exited with error code.\nSee \"systemctl status nginx.service\" for details.\n"), fakeExit{"exit status 1"}
	}
	got = verifiedServiceAction(s.run, "nginx", "nginx", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, transport.ServiceActionStageCommand)
	if got.Error != "exit status 1: Job for nginx.service failed because the control process exited with error code." {
		t.Fatalf("the answer's words changed: %q", got.Error)
	}

	// The command did not come back with its own exit status (a lost lease).
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		return nil, errors.New("signal: killed")
	}
	got = verifiedServiceAction(s.run, "nginx", "nginx", "restart")
	wantServiceAction(t, got, transport.ServiceActionUnknown, transport.ServiceActionStageCommand)

	// What a unit is could not be read: it keeps its own job result.
	s.act = nil
	s.showFails["nginx.service"] = true
	got = verifiedServiceAction(s.run, "nginx", "nginx", "restart")
	if !got.Success || got.Outcome != "" {
		t.Fatalf("answer = %+v", got)
	}
}
