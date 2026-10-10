package main

import (
	"archive/tar"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Corrections from the final native round (12 Oct 2026; evidence
// deploy/e2e/release-recovery/evidence/set3-20261012).

// An application unit runs as the web server's account of this host: the
// `www-data` the Panel asks for exists on Debian and Ubuntu only.
func TestAppUnitRunsAsTheWebServerAccountThisHostHas(t *testing.T) {
	old := appUnitAccountExists
	t.Cleanup(func() { appUnitAccountExists = old })
	host := func(accounts ...string) {
		appUnitAccountExists = func(name string) bool {
			for _, account := range accounts {
				if account == name {
					return true
				}
			}
			return false
		}
	}
	host("www-data", "http")
	if got := appUnitAccount("www-data"); got != "www-data" {
		t.Fatalf("Debian: %q", got)
	}
	host("http")
	if got := appUnitAccount("www-data"); got != "http" {
		t.Fatalf("Arch: %q", got)
	}
	host("nginx", "http")
	if got := appUnitAccount("www-data"); got != "nginx" {
		t.Fatalf("RHEL family: %q", got)
	}
	// Nothing is invented, and another account is written as asked.
	host()
	if got := appUnitAccount("www-data"); got != "www-data" {
		t.Fatalf("no web server account: %q", got)
	}
	host("http")
	if got := appUnitAccount("deploy"); got != "deploy" {
		t.Fatalf("another account: %q", got)
	}
}

func loadedUnit(s *fakeSystemd, unit, active, sub, result string) {
	s.set(unit, "LoadState", "loaded")
	s.set(unit, "ActiveState", active)
	s.set(unit, "SubState", sub)
	s.set(unit, "Result", result)
}

// O16. A reload of anything that is not running is "not running, nothing was
// reloaded", read from the unit before anything is sent. Measured: a stopped
// nginx and the stopped PostgreSQL wrapper were answered with systemd's own
// refusal as a failed command; only Postfix and Dovecot were answered as not
// running.
func TestServicesPageReloadOfAnyStoppedUnitIsNotRunning(t *testing.T) {
	installFakeMailHost(t)
	refuses := func(s *fakeSystemd, action, unit string) ([]byte, error) {
		return []byte(unit + ".service is not active, cannot reload.\n"), fakeExit{"exit status 1"}
	}
	for _, c := range []struct {
		service, unit, active, sub, result string
	}{
		{"nginx", "nginx", "inactive", "dead", "success"},
		{"mariadb", "mariadb", "inactive", "dead", "success"},
		{"php-fpm", "php8.4-fpm", "failed", "failed", "exit-code"},
		{"postgresql", "postgresql", "inactive", "dead", "success"}, // Arch: a unit that runs the server itself
	} {
		s := &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{}, act: refuses}
		loadedUnit(s, c.unit+".service", c.active, c.sub, c.result)
		if c.service == "postgresql" {
			answerPostgreSQL(t, func(int) (string, error) { return "", errors.New("psql: could not connect") })
			s.set("postgresql.service", "Type", "notify")
			s.set("postgresql.service", "ExecStart", "{ path=/usr/bin/postgres ; argv[]=/usr/bin/postgres -D /var/lib/postgres/data ; ignore_errors=no ; start_time=[n/a] ; stop_time=[n/a] ; pid=0 ; code=(null) ; status=0/0 }")
		}
		got := verifiedServiceAction(s.run, c.service, c.unit, "reload")
		wantServiceAction(t, got, transport.ServiceActionFailed, transport.ServiceActionStageNotRunning)
		if calledWith(s.calls, "systemctl reload "+c.unit) {
			t.Fatalf("%s: a reload was sent to a stopped unit: %v", c.unit, s.calls)
		}
		if !strings.Contains(got.Detail, c.unit+".service is "+c.active) || !strings.Contains(got.Detail, "nothing was reloaded") || got.Unit != "" || claimsSettings(got) {
			t.Fatalf("%s: answer = %+v", c.unit, got)
		}
	}

	// The wrapper (Debian, Ubuntu): judged by the units its reload reaches, and
	// nothing is sent.
	s := debianPostgreSQL(t)
	s.act = refuses
	s.set(pgCluster, "ActiveState", "inactive")
	s.set(pgCluster, "SubState", "dead")
	got := verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, transport.ServiceActionStageNotRunning)
	if calledWith(s.calls, "systemctl reload postgresql") || got.Unit != pgCluster || !strings.Contains(got.Detail, pgCluster) {
		t.Fatalf("wrapper: answer = %+v, calls = %v", got, s.calls)
	}
	s = debianPostgreSQL(t)
	s.set(pgCluster, "ActiveState", "failed")
	got = verifiedServiceAction(s.run, "postgresql", "postgresql", "reload")
	wantServiceAction(t, got, transport.ServiceActionFailed, transport.ServiceActionStageNotRunning)
	if calledWith(s.calls, "systemctl reload postgresql") {
		t.Fatalf("wrapper with a failed unit behind it: calls = %v", s.calls)
	}
}

// What is not "stopped" keeps the service manager's own answer: a running
// unit that cannot reload (MariaDB, O12), a unit in between two states, a unit
// that does not exist on this server, and a state that could not be read.
func TestServicesPageReloadKeepsSystemdsAnswerForAUnitThatIsNotStopped(t *testing.T) {
	installFakeMailHost(t)
	for name, c := range map[string]struct {
		load, active, sub string
		showFails         bool
		output            string
	}{
		"running, reload not applicable": {"loaded", "active", "running", false, "Failed to reload mariadb.service: Job type reload is not applicable for unit mariadb.service.\n"},
		"stopping":                       {"loaded", "deactivating", "stop-sigterm", false, "Job for mariadb.service failed.\n"},
		"starting":                       {"loaded", "activating", "start", false, "Job for mariadb.service failed.\n"},
		"no such unit":                   {"not-found", "inactive", "dead", false, "Failed to reload mariadb.service: Unit mariadb.service not found.\n"},
		"state not read":                 {"loaded", "inactive", "dead", true, "mariadb.service is not active, cannot reload.\n"},
	} {
		s := &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{"mariadb.service": c.showFails}}
		s.set("mariadb.service", "LoadState", c.load)
		s.set("mariadb.service", "ActiveState", c.active)
		s.set("mariadb.service", "SubState", c.sub)
		s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
			return []byte(c.output), fakeExit{"exit status 1"}
		}
		got := verifiedServiceAction(s.run, "mariadb", "mariadb", "reload")
		wantServiceAction(t, got, transport.ServiceActionFailed, transport.ServiceActionStageCommand)
		if !calledWith(s.calls, "systemctl reload mariadb") || got.Detail != strings.TrimSpace(c.output) {
			t.Fatalf("%s: answer = %+v, calls = %v", name, got, s.calls)
		}
	}
	// Only a reload asks: a start of a stopped unit is a start.
	s := &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{}}
	loadedUnit(s, "nginx.service", "inactive", "dead", "success")
	if got := verifiedServiceAction(s.run, "nginx", "nginx", "start"); !got.Success || !calledWith(s.calls, "systemctl start nginx") {
		t.Fatalf("start: answer = %+v, calls = %v", got, s.calls)
	}
}

// postfixUnits answers `systemctl show` for Postfix's units around a fake mail
// host; a stop moves each unit to the state the test gives it.
type postfixUnits struct {
	host  *fakeMailHost
	now   map[string][2]string // unit -> ActiveState, Result
	after map[string][2]string
	calls []string
}

func (p *postfixUnits) run(name string, args ...string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	p.calls = append(p.calls, call)
	if name == "systemctl" && len(args) > 1 && args[0] == "show" && strings.HasPrefix(args[1], "postfix") {
		state, loaded := p.now[args[1]]
		if !loaded {
			return []byte("LoadState=not-found\nActiveState=inactive\nResult=success\n"), nil
		}
		return []byte("LoadState=loaded\nActiveState=" + state[0] + "\nResult=" + state[1] + "\n"), nil
	}
	out, err := p.host.run(name, args...)
	if call == "systemctl stop postfix" && err == nil {
		for unit, state := range p.after {
			p.now[unit] = state
		}
	}
	return out, err
}

const refusedMainCf = "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign\n"

// O19. Postfix with a main.cf it refuses is stopped: the master is gone, the
// answer is a success, and systemd shows the unit as failed because the unit's
// own stop command (`postfix stop`) read main.cf and exited 1. The mark is
// systemd's record and is left as it is; the success says it.
func TestServicesPageStopSaysWhenItLeftTheUnitMarkedFailed(t *testing.T) {
	// Debian 13: postfix.service runs the daemon itself.
	host := installFakeMailHost(t)
	host.checkOutput = refusedMainCf
	units := &postfixUnits{host: host,
		now:   map[string][2]string{"postfix.service": {"active", "success"}, "postfix@-.service": {"inactive", "success"}},
		after: map[string][2]string{"postfix.service": {"failed", "exit-code"}}}
	got := verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Applied != mailServiceStopped || got.Notice != transport.ServiceActionNoticeUnitFailed ||
		got.NoticeUnit != "postfix.service" || got.NoticeResult != "exit-code" ||
		!strings.Contains(got.NoticeDetail, "bad numerical configuration") {
		t.Fatalf("answer = %+v", got)
	}
	for _, call := range units.calls {
		if strings.Contains(call, "reset-failed") {
			t.Fatalf("the unit's mark was cleared: %q", call)
		}
	}

	// Ubuntu 24.04: the daemon is the instance unit behind the wrapper.
	host = installFakeMailHost(t)
	host.checkOutput = refusedMainCf
	units = &postfixUnits{host: host,
		now:   map[string][2]string{"postfix.service": {"active", "success"}, "postfix@-.service": {"active", "success"}},
		after: map[string][2]string{"postfix.service": {"inactive", "success"}, "postfix@-.service": {"failed", "exit-code"}}}
	got = verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != transport.ServiceActionNoticeUnitFailed || got.NoticeUnit != "postfix@-.service" {
		t.Fatalf("answer = %+v", got)
	}

	// A stop that ended cleanly says nothing more.
	host = installFakeMailHost(t)
	units = &postfixUnits{host: host,
		now:   map[string][2]string{"postfix.service": {"active", "success"}},
		after: map[string][2]string{"postfix.service": {"inactive", "success"}}}
	got = verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != "" || got.NoticeUnit != "" || got.NoticeDetail != "" {
		t.Fatalf("a clean stop carries a notice: %+v", got)
	}

	// A mark that was there before the stop is not this action's.
	host = installFakeMailHost(t)
	host.checkOutput = refusedMainCf
	units = &postfixUnits{host: host,
		now:   map[string][2]string{"postfix.service": {"failed", "exit-code"}},
		after: map[string][2]string{"postfix.service": {"failed", "exit-code"}}}
	got = verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != "" {
		t.Fatalf("an earlier mark was reported as this stop's: %+v", got)
	}

	// The unit could not be read before the stop: nothing is claimed.
	host = installFakeMailHost(t)
	got = verifiedServiceAction(host.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionVerified, "")
	if got.Notice != "" {
		t.Fatalf("a notice without a reading: %+v", got)
	}

	// A stop that failed is a failure and carries no notice.
	host = installFakeMailHost(t)
	host.stopLeavesMaster = true
	units = &postfixUnits{host: host,
		now:   map[string][2]string{"postfix.service": {"active", "success"}},
		after: map[string][2]string{"postfix.service": {"failed", "exit-code"}}}
	got = verifiedServiceAction(units.run, "postfix", "postfix", "stop")
	wantServiceAction(t, got, transport.ServiceActionFailed, mailServiceStageStop)
	if got.Notice != "" {
		t.Fatalf("a failed stop carries a notice: %+v", got)
	}

	// Any other unit: the same reading, without a line of the service's own.
	installFakeMailHost(t)
	s := &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{}}
	loadedUnit(s, "nginx.service", "active", "running", "success")
	s.act = func(s *fakeSystemd, action, unit string) ([]byte, error) {
		loadedUnit(s, "nginx.service", "failed", "failed", "timeout")
		return nil, nil
	}
	got = verifiedServiceAction(s.run, "nginx", "nginx", "stop")
	if !got.Success || got.Notice != transport.ServiceActionNoticeUnitFailed || got.NoticeUnit != "nginx.service" ||
		got.NoticeResult != "timeout" || got.NoticeDetail != "" {
		t.Fatalf("nginx: answer = %+v", got)
	}
	// Only a stop reads it.
	s = &fakeSystemd{units: map[string]map[string][]string{}, showFails: map[string]bool{}}
	loadedUnit(s, "nginx.service", "failed", "failed", "exit-code")
	if got = verifiedServiceAction(s.run, "nginx", "nginx", "restart"); !got.Success || got.Notice != "" {
		t.Fatalf("restart: answer = %+v", got)
	}
}

func archRefusal() error {
	return &services.NginxConfigRefusedError{
		Output: "nginx: [emerg] open() \"/etc/nginx/snippets/fastcgi-php.conf\" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27\n" +
			"nginx: configuration file /etc/nginx/nginx.conf test failed",
		Cause: errors.New("exit status 1"),
	}
}

// P5b, the answer. nginx refused the configuration with the new site's vhost
// in it and the vhost was put back: the Agent says that this is what happened,
// with nginx's own line in a field of its own. The client's message still
// carries no path of the server.
func TestCreateSiteSaysWhenTheWebServerRefusedTheConfiguration(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	socket := services.PHPFPMSocketPath(req.PHPVersion, fmt.Sprintf("site%d", req.SiteID))
	create := func(failure error) (*transport.CreateSiteResponse, *fakeSiteLifecycle) {
		fake := &fakeSiteLifecycle{home: home, failures: map[string]error{"apply-vhost": failure}, expectedSocket: socket}
		reply := &transport.CreateSiteResponse{}
		if err := lifecycleTestAgent(t, fake).CreateSite(req, reply); err != nil {
			t.Fatal(err)
		}
		if reply.Success {
			t.Fatal("a refused vhost was reported as a created site")
		}
		return reply, fake
	}

	reply, fake := create(&services.VhostRestoredError{Cause: fmt.Errorf("nginx validation failed: %w", archRefusal())})
	if reply.ErrorCode != transport.WebServerRefusedConfig ||
		reply.ErrorDetail != `nginx: [emerg] open() "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set3-php.test.conf:27` {
		t.Fatalf("reply = %+v", reply)
	}
	if strings.Contains(reply.ErrorMessage, "/etc/nginx") || reply.ErrorMessage != "site provisioning failed during nginx vhost activation" {
		t.Fatalf("the client's message changed or names a path: %q", reply.ErrorMessage)
	}
	joined := strings.Join(fake.calls, ",")
	for _, required := range []string{"delete-pool", "kill-user", "delete-user", "remove-all"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing rollback %q in %v", required, fake.calls)
		}
	}

	// A long line is bounded.
	long := archRefusal().(*services.NginxConfigRefusedError)
	long.Output = "nginx: [emerg] " + strings.Repeat("ç", 900)
	reply, _ = create(&services.VhostRestoredError{Cause: long})
	if reply.ErrorCode != transport.WebServerRefusedConfig || len([]rune(reply.ErrorDetail)) != 300 {
		t.Fatalf("detail holds %d characters", len([]rune(reply.ErrorDetail)))
	}

	// nginx refused, and the vhost could not be put back: not this answer.
	reply, _ = create(fmt.Errorf("%w; rollback reload failed: exit status 1", fmt.Errorf("nginx validation failed: %w", archRefusal())))
	if reply.ErrorCode != "" || reply.ErrorDetail != "" {
		t.Fatalf("an inverse that failed was typed as restored: %+v", reply)
	}
	// The vhost was put back after a failure that is not nginx's refusal (the
	// reload failed, the test could not be run).
	reply, _ = create(&services.VhostRestoredError{Cause: errors.New("nginx reload failed: Job for nginx.service failed")})
	if reply.ErrorCode != "" || reply.ErrorDetail != "" {
		t.Fatalf("a reload failure was typed as a refused configuration: %+v", reply)
	}
}

// O17. What the files step leaves out is counted and named: a refused member
// by its own name, bounded; the members outside the site folder by the folder
// they are in.
func TestCpmoveLeftOutIsCountedAndBounded(t *testing.T) {
	var left cpmoveLeftOut
	left.refuse("/etc/set3-escape-absolute.txt", transport.CpmoveRefusedAbsolutePath)
	for i := 0; i < 40; i++ {
		left.refuse(fmt.Sprintf("/var/tmp/x-%02d", i), transport.CpmoveRefusedAbsolutePath)
	}
	file := func(name string) *tar.Header { return &tar.Header{Name: name, Typeflag: tar.TypeReg} }
	for _, name := range []string{
		"cpmove-user/homedir/mail/example.com/info/cur/1", "cpmove-user/homedir/mail/example.com/info/cur/2",
		"cpmove-user/homedir/mail/example.com/info/new/3", "cpmove-user/homedir/.bashrc", "cpmove-user/homedir/etc/example.com/shadow",
		"cpmove-user/mysql/shop.sql", "cpmove-user/mysql/shop.create", "cpmove-user/version", "./cpmove-user/cp/user",
		"backup-10.9.2026_user/homedir/public_html_old/index.php", "loose-file",
	} {
		left.outside(file(name))
	}
	// A folder and a global header are not members that hold anything.
	left.outside(&tar.Header{Name: "cpmove-user/homedir/mail/", Typeflag: tar.TypeDir})
	left.outside(&tar.Header{Name: "pax_global_header", Typeflag: tar.TypeXGlobalHeader})
	// Something that is not a file is still a member that was not imported.
	left.outside(&tar.Header{Name: "cpmove-user/homedir/tmp/link", Typeflag: tar.TypeSymlink})

	var resp transport.CpmoveExtractResponse
	left.report(&resp)
	if resp.RefusedCount != 41 || len(resp.Refused) != transport.CpmoveRefusedMemberLimit ||
		resp.Refused[0] != (transport.CpmoveRefusedMember{Name: "/etc/set3-escape-absolute.txt", Reason: transport.CpmoveRefusedAbsolutePath}) {
		t.Fatalf("refused = %d listed of %d: %+v", len(resp.Refused), resp.RefusedCount, resp.Refused)
	}
	if resp.OutsideCount != 12 {
		t.Fatalf("outside = %d", resp.OutsideCount)
	}
	want := []transport.CpmoveMemberGroup{
		{Name: "homedir/mail", Count: 3}, {Name: "(top folder of the archive)", Count: 2}, {Name: "mysql", Count: 2},
		{Name: "cp", Count: 1}, {Name: "homedir", Count: 1}, {Name: "homedir/etc", Count: 1},
		{Name: "homedir/public_html_old", Count: 1}, {Name: "homedir/tmp", Count: 1},
	}
	if fmt.Sprint(resp.OutsideGroups) != fmt.Sprint(want) {
		t.Fatalf("groups = %+v", resp.OutsideGroups)
	}
	total := 0
	for _, group := range resp.OutsideGroups {
		total += group.Count
	}
	if total != resp.OutsideCount {
		t.Fatalf("the groups hold %d of %d members", total, resp.OutsideCount)
	}

	// Many folders: the list is bounded and the rest is still counted.
	left = cpmoveLeftOut{}
	for i := 0; i < 400; i++ {
		left.outside(file(fmt.Sprintf("cpmove-user/folder-%03d/file", i)))
	}
	resp = transport.CpmoveExtractResponse{}
	left.report(&resp)
	last := resp.OutsideGroups[len(resp.OutsideGroups)-1]
	if resp.OutsideCount != 400 || len(resp.OutsideGroups) != transport.CpmoveOutsideGroupLimit+1 ||
		last.Name != cpmoveOtherFolders || last.Count != 400-transport.CpmoveOutsideGroupLimit {
		t.Fatalf("outside = %d, groups = %+v", resp.OutsideCount, resp.OutsideGroups)
	}

	// A name is shown bounded and without control characters.
	if got := cpmoveMemberName("/etc/a\x1b[31m\nb"); got != "/etc/a?[31m?b" {
		t.Fatalf("name = %q", got)
	}
	if got := []rune(cpmoveMemberName("/" + strings.Repeat("ş", 500))); len(got) != 201 || got[200] != '…' {
		t.Fatalf("a long name holds %d characters", len(got))
	}
	for typeflag, want := range map[byte]string{
		tar.TypeSymlink: "a symbolic link", tar.TypeLink: "a hard link", tar.TypeChar: "a device node",
		tar.TypeBlock: "a device node", tar.TypeFifo: "a named pipe",
	} {
		if got := cpmoveEntryKind(typeflag); got != want {
			t.Fatalf("kind of %q = %q", typeflag, got)
		}
	}
}
