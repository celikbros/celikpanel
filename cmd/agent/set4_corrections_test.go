package main

import (
	"errors"
	"io/fs"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections after the set4 native measurement (2026-10-09; the measurement
// of what the Agent reads is in
// deploy/e2e/release-recovery/evidence/set4b-20261009, diagnostic/).

// What `postconf -h queue_directory` printed to the Agent on Ubuntu 24.04 with
// the owner's line in main.cf, as strace recorded it: the warning on the error
// stream first, then the directory on the output, both in the one buffer the
// Agent reads; exit status 0.
const measuredPostconfWithWarning = "/usr/sbin/postconf: warning: /etc/postfix/main.cf: #comment after other text is not allowed: # raised for the campa...\n" +
	"/var/spool/postfix\n"

// ubuntuPostfix is Postfix on Ubuntu 24.04 as it was measured around a Stop
// with a main.cf that `postfix check` refuses. Time is the test's own: every
// command takes 30 ms (the Agent's commands were 30 to 35 ms apart), a sleep
// takes what it was asked for.
//
//   - `postfix.service` is a wrapper; `systemctl stop postfix` returns when
//     its job ends, 1 to 4 ms after `postfix@-.service` left `active`.
//   - `postfix@-.service` then shows `deactivating` (its stop command refuses
//     main.cf and exits 1 only after Postfix's one-second pause). The master
//     ended 1009 ms after the unit left `active`, and systemd marked the unit
//     `failed`, `Result=exit-code`, 4 ms later.
//   - Any path that does not exist is answered "no such file" by the kernel.
type ubuntuPostfix struct {
	now, stopAt              time.Duration
	masterLives, unitSettles time.Duration
	neverSettles             bool
	endsClean                bool
	unreadableAfterStop      bool
	postconf                 string
	checkOutput              string
	calls                    []string
	// What the unit showed at each reading after the stop, with the time.
	readings []string
}

const ubuntuMasterPID = "39747"

func installUbuntuPostfix(t *testing.T) *ubuntuPostfix {
	t.Helper()
	host := &ubuntuPostfix{stopAt: -1, masterLives: 1009 * time.Millisecond, unitSettles: 1013 * time.Millisecond,
		postconf: measuredPostconfWithWarning, checkOutput: refusedMainCf}
	oldSleep, oldRead, oldExited, oldIsDirectory := mailServiceSleep, mailServiceReadFile, mailServiceExited, mailServiceIsDirectory
	t.Cleanup(func() {
		mailServiceSleep, mailServiceReadFile, mailServiceExited, mailServiceIsDirectory = oldSleep, oldRead, oldExited, oldIsDirectory
	})
	mailServiceSleep = func(d time.Duration) { host.now += d }
	mailServiceReadFile = func(name string) ([]byte, error) {
		switch name {
		case "/var/spool/postfix/pid/master.pid":
			// Postfix leaves the file when the master exits.
			return []byte("                           " + ubuntuMasterPID + "\n"), nil
		case "/proc/" + ubuntuMasterPID + "/comm":
			if host.masterAlive() {
				return []byte("master\n"), nil
			}
		}
		return nil, fs.ErrNotExist
	}
	mailServiceIsDirectory = func(name string) bool { return name == "/var/spool/postfix/pid" }
	mailServiceExited = func(err error) bool {
		var exit fakeExit
		return errors.As(err, &exit)
	}
	return host
}

func (h *ubuntuPostfix) masterAlive() bool {
	return h.stopAt < 0 || h.now-h.stopAt < h.masterLives
}

func (h *ubuntuPostfix) run(name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	h.calls = append(h.calls, call)
	h.now += 30 * time.Millisecond
	show := func(active, result string) ([]byte, error) {
		// The order systemd printed them in.
		return []byte("Result=" + result + "\nLoadState=loaded\nActiveState=" + active + "\n"), nil
	}
	switch call {
	case "systemctl show postfix.service --property=LoadState --property=ActiveState --property=Result":
		if h.stopAt < 0 {
			return show("active", "success")
		}
		return show("inactive", "success")
	case "systemctl show postfix@-.service --property=LoadState --property=ActiveState --property=Result":
		if h.stopAt < 0 {
			return show("active", "success")
		}
		if h.unreadableAfterStop {
			return []byte("Failed to get properties: Connection timed out\n"), fakeExit{"exit status 1"}
		}
		state, result := "deactivating", "success"
		if !h.neverSettles && h.now-h.stopAt >= h.unitSettles {
			state, result = "failed", "exit-code"
			if h.endsClean {
				state, result = "inactive", "success"
			}
		}
		h.readings = append(h.readings, state)
		return show(state, result)
	case "systemctl stop postfix":
		h.stopAt = h.now
		return nil, nil
	case "postconf -h queue_directory":
		return []byte(h.postconf), nil
	case "postfix check":
		if h.checkOutput != "" {
			return []byte(h.checkOutput), fakeExit{"exit status 1"}
		}
		return nil, nil
	}
	return nil, errors.New("unexpected command: " + call)
}

// after returns the commands sent after `systemctl stop postfix`.
func (h *ubuntuPostfix) after(t *testing.T) []string {
	t.Helper()
	for index, call := range h.calls {
		if call == "systemctl stop postfix" {
			return h.calls[index+1:]
		}
	}
	t.Fatalf("no stop was sent: %v", h.calls)
	return nil
}

// Nothing but readings may follow the stop: the wait sends no mutation.
func (h *ubuntuPostfix) onlyReadAfterTheStop(t *testing.T) {
	t.Helper()
	for _, call := range h.after(t) {
		reading := call == "postconf -h queue_directory" || call == "postfix check" || strings.HasPrefix(call, "systemctl show ")
		if !reading || strings.Contains(call, "reset-failed") {
			t.Fatalf("after the stop the Agent sent %q", call)
		}
	}
}

// The queue directory is the one line of postconf's answer that is not
// postconf's own message. Measured: the two lines were taken as one path.
func TestPostfixQueueDirectoryIsOneLineNotTheWholeAnswer(t *testing.T) {
	// Why the measured answer went wrong: the whole buffer is "absolute",
	// because the program's own name is, and names nothing that exists.
	whole := strings.TrimSpace(measuredPostconfWithWarning)
	if !path.IsAbs(whole) || !strings.Contains(path.Join(whole, "pid", "master.pid"), "\n") {
		t.Fatalf("the measured answer is not the shape that was misread: %q", whole)
	}
	for _, c := range []struct {
		name, out, want string
		known           bool
	}{
		{"measured on Ubuntu 24.04", measuredPostconfWithWarning, "/var/spool/postfix", true},
		{"the same started by its bare name", "postconf: warning: /etc/postfix/main.cf: unused parameter: campaign=1\n/var/spool/postfix\n", "/var/spool/postfix", true},
		{"the message after the value", "/var/spool/postfix\n/usr/sbin/postconf: warning: /etc/postfix/main.cf: unused parameter: campaign=1\n", "/var/spool/postfix", true},
		{"two messages", "postconf: warning: a\npostconf: warning: b\n/var/spool/postfix\n", "/var/spool/postfix", true},
		{"no message", "/var/spool/postfix\n", "/var/spool/postfix", true},
		{"a trailing slash", "/var/spool/postfix/\n", "/var/spool/postfix/", true},
		{"only a message", "/usr/sbin/postconf: fatal: open /etc/postfix/main.cf: No such file or directory\n", "", false},
		{"nothing", "", "", false},
		{"two lines that are not messages", "/var/spool/postfix\n/var/spool/postfix-2\n", "", false},
		{"a line of another program", "some wrapper said something\n/var/spool/postfix\n", "", false},
		{"a relative path", "var/spool/postfix\n", "", false},
		{"a path that needs resolving", "/var/spool/../spool/postfix\n", "", false},
		{"the root", "/\n", "", false},
	} {
		got, known := postconfOnePath([]byte(c.out))
		if got != c.want || known != c.known {
			t.Errorf("%s: got %q, %v; want %q, %v", c.name, got, known, c.want, c.known)
		}
	}
}

// The master is looked for in the queue directory Postfix names, also when
// postconf warns about main.cf, and "no master.pid" is a statement only where
// the directory that would hold it exists.
func TestPostfixMasterIsNotGoneBecauseAPathDoesNotExist(t *testing.T) {
	host := installUbuntuPostfix(t)
	alive, pid, known := postfixMasterProcess(host.run)
	if !alive || !known || itoa(pid) != ubuntuMasterPID {
		t.Fatalf("a running master behind a postconf warning: alive=%v pid=%d known=%v", alive, pid, known)
	}
	// An answer that is not one path: nothing is claimed.
	host.postconf = "/var/spool/postfix\n/var/spool/postfix-2\n"
	if alive, _, known = postfixMasterProcess(host.run); alive || known {
		t.Fatalf("an answer of two paths: alive=%v known=%v", alive, known)
	}
	// A queue directory that is not on this server: no file there, and that
	// says nothing about a master.
	host.postconf = "/srv/another-queue\n"
	if alive, _, known = postfixMasterProcess(host.run); alive || known {
		t.Fatalf("a directory that does not exist: alive=%v known=%v", alive, known)
	}
	// The directory is there and holds no master.pid: no master ever started.
	oldRead := mailServiceReadFile
	mailServiceReadFile = func(string) ([]byte, error) { return nil, fs.ErrNotExist }
	host.postconf = measuredPostconfWithWarning
	if alive, _, known = postfixMasterProcess(host.run); alive || !known {
		t.Fatalf("no master.pid in an existing directory: alive=%v known=%v", alive, known)
	}
	mailServiceReadFile = oldRead
}

// Item 10 of set4, Ubuntu 24.04: the Stop is answered once the master has
// ended, the unit is read once it has settled, and the note names it.
func TestServicesPageStopOnUbuntuReadsTheUnitOnceItHasSettled(t *testing.T) {
	host := installUbuntuPostfix(t)
	got := verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceStopped {
		t.Fatalf("answer = %+v", got)
	}
	// The answer is not given while the master lives (measured: it was given
	// about 100 ms after the stop, 900 ms before the master ended).
	if host.masterAlive() || host.now-host.stopAt < host.masterLives {
		t.Fatalf("answered %s after the stop, while the master lives for %s", host.now-host.stopAt, host.masterLives)
	}
	if got.Notice != transport.ServiceActionNoticeUnitFailed || got.NoticeUnit != "postfix@-.service" ||
		got.NoticeResult != "exit-code" || !strings.Contains(got.NoticeDetail, "bad numerical configuration") {
		t.Fatalf("the note of the failed unit is missing: %+v (readings of the unit after the stop: %v)", got, host.readings)
	}
	// The master was looked for more than once, and the unit was read as
	// failed, never taken from a reading between two states.
	lookups := 0
	for _, call := range host.after(t) {
		if call == "postconf -h queue_directory" {
			lookups++
		}
	}
	if lookups < 2 || len(host.readings) == 0 || host.readings[len(host.readings)-1] != "failed" {
		t.Fatalf("master lookups = %d, readings of the unit = %v", lookups, host.readings)
	}
	host.onlyReadAfterTheStop(t)

	// The unit is still between two states when the master is already seen as
	// gone (the 2 to 7 ms that were measured, here made long): it is read
	// again, not taken as clean.
	host = installUbuntuPostfix(t)
	host.unitSettles = host.masterLives + 1200*time.Millisecond
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != transport.ServiceActionNoticeUnitFailed || got.NoticeUnit != "postfix@-.service" {
		t.Fatalf("answer = %+v, readings = %v", got, host.readings)
	}
	if len(host.readings) < 2 || host.readings[0] != "deactivating" {
		t.Fatalf("the unit was not read while it was between two states: %v", host.readings)
	}
	host.onlyReadAfterTheStop(t)

	// A stop that ends cleanly after the same wait says nothing more.
	host = installUbuntuPostfix(t)
	host.checkOutput, host.endsClean = "", true
	host.unitSettles = host.masterLives + 700*time.Millisecond
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != "" || got.NoticeUnit != "" || got.NoticeResult != "" || got.NoticeDetail != "" {
		t.Fatalf("a clean stop carries a notice: %+v", got)
	}
	if host.readings[len(host.readings)-1] != "inactive" {
		t.Fatalf("readings = %v", host.readings)
	}
}

// A unit that does not settle within the bound is not passed over in silence:
// the answer says that its stop was not read. Nothing is sent to the unit
// while waiting, and the wait ends.
func TestServicesPageStopSaysWhenTheUnitDidNotSettle(t *testing.T) {
	host := installUbuntuPostfix(t)
	host.neverSettles = true
	got := verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	// The stop itself is verified: the master is gone.
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if host.masterAlive() {
		t.Fatal("answered while the master lives")
	}
	if got.Notice != transport.ServiceActionNoticeUnitNotSettled || got.NoticeUnit != "postfix@-.service" ||
		got.NoticeResult != "deactivating" || got.NoticeDetail != "" {
		t.Fatalf("answer = %+v", got)
	}
	// Bounded: the units of one stop share the readings every other
	// verification of this file is given.
	if len(host.readings) != mailServiceStartPolls-1 {
		t.Fatalf("the unit was read %d times; the bound is %d readings for both units together", len(host.readings), mailServiceStartPolls)
	}
	for _, state := range host.readings {
		if state != "deactivating" {
			t.Fatalf("readings = %v", host.readings)
		}
	}
	host.onlyReadAfterTheStop(t)
	for _, call := range host.after(t) {
		if call == "postfix check" {
			t.Fatal("Postfix's check was run for a unit that was not read as failed")
		}
	}

	// The unit cannot be read after the stop although it was read before it.
	host = installUbuntuPostfix(t)
	host.unreadableAfterStop = true
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != transport.ServiceActionNoticeUnitNotSettled || got.NoticeUnit != "postfix@-.service" || got.NoticeResult != "" {
		t.Fatalf("answer = %+v", got)
	}
	host.onlyReadAfterTheStop(t)

	// A unit that was not read before the stop (it is not on this server) is
	// still not this stop's to report, settled or not.
	units := &postfixUnits{host: installFakeMailHost(t),
		now:   map[string][2]string{"postfix.service": {"active", "success"}},
		after: map[string][2]string{"postfix.service": {"inactive", "success"}}}
	got = verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != "" {
		t.Fatalf("answer = %+v", got)
	}
}

// With an answer of postconf that names no single directory, the master
// cannot be looked for, and with a refused main.cf Postfix cannot be asked
// either: the Stop is unknown, as before, never a success.
func TestServicesPageStopStaysUnknownWhenTheMasterCannotBeLookedFor(t *testing.T) {
	host := installUbuntuPostfix(t)
	host.postconf = "/var/spool/postfix\n/var/spool/postfix-2\n"
	got := verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionUnknown, mailServiceStageVerify)
	if got.Notice != "" || !strings.Contains(got.Error, "could not be looked for") {
		t.Fatalf("answer = %+v", got)
	}
}

// Debian 13: the unit that is stopped runs the daemon, so `systemctl stop`
// returns after its whole stop and the first reading is `failed`. The note is
// given as before, with no wait.
func TestServicesPageStopOnDebianStillGivesTheNoteAtTheFirstReading(t *testing.T) {
	host := installFakeMailHost(t)
	host.checkOutput = refusedMainCf
	slept := 0
	mailServiceSleep = func(time.Duration) { slept++ }
	units := &postfixUnits{host: host,
		now:   map[string][2]string{"postfix.service": {"active", "success"}},
		after: map[string][2]string{"postfix.service": {"failed", "exit-code"}}}
	got := verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != transport.ServiceActionNoticeUnitFailed || got.NoticeUnit != "postfix.service" || got.NoticeResult != "exit-code" ||
		!strings.Contains(got.NoticeDetail, "bad numerical configuration") {
		t.Fatalf("answer = %+v", got)
	}
	if slept != 0 {
		t.Fatalf("a settled unit was waited for %d times", slept)
	}
	for _, call := range units.calls {
		if strings.Contains(call, "reset-failed") {
			t.Fatalf("the unit's mark was cleared: %q", call)
		}
	}
}
