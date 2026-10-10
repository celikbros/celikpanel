package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

const cronTestUser = "example_com"

// fakeCrontab stands in for `crontab -u <user> -l` and `crontab -u <user> -`.
type fakeCrontab struct {
	content  string
	stderr   string
	exitCode int // 0 answers content
	installs []string
}

func (f *fakeCrontab) list(string) ([]byte, string, int, error) {
	if f.exitCode == 0 {
		return []byte(f.content), "", 0, nil
	}
	return []byte(f.content), f.stderr, f.exitCode, errors.New("exit status")
}

func (f *fakeCrontab) install(_ string, content string) error {
	f.installs = append(f.installs, content)
	f.content, f.stderr, f.exitCode = content, "", 0
	return nil
}

func installFakeCrontab(t *testing.T, fake *fakeCrontab) *fakeCrontab {
	t.Helper()
	oldList, oldInstall, oldLook, oldAccess := cronListCrontab, cronInstallCrontab, cronLookPath, cronAccessFile
	t.Cleanup(func() {
		cronListCrontab, cronInstallCrontab, cronLookPath, cronAccessFile = oldList, oldInstall, oldLook, oldAccess
	})
	// The test host's own /etc/cron.allow and cron.deny are not part of a test.
	cronAccessFile = func(string) ([]byte, error) { return nil, errors.New("no such file") }
	cronListCrontab = fake.list
	cronInstallCrontab = fake.install
	cronLookPath = func(string) (string, error) { return "/usr/bin/crontab", nil }
	return fake
}

func noCrontabYet() *fakeCrontab {
	return &fakeCrontab{stderr: "no crontab for " + cronTestUser + "\n", exitCode: 1}
}

const ownerCrontab = "MAILTO=owner@example.com\n" +
	"# nightly report\n" +
	"15 2 * * * /usr/local/bin/report --quiet\n" +
	"\n" +
	"*/5 * * * * /usr/bin/php /var/www/example.com/cron.php\n"

// "This user has no crontab" is the one legitimate empty answer. It is
// recognised only as exit status 1, no output and exactly the line Debian/
// Ubuntu cron and cronie print; everything else leaves the crontab unknown.
func TestReadCrontabTellsNoCrontabFromAFailedRead(t *testing.T) {
	cases := []struct {
		name    string
		fake    fakeCrontab
		content string
		unknown bool
	}{
		{name: "a crontab", fake: fakeCrontab{content: ownerCrontab}, content: ownerCrontab},
		{name: "an existing but empty crontab", fake: fakeCrontab{content: ""}, content: ""},
		{name: "no crontab for this user", fake: *noCrontabYet(), content: ""},
		{name: "no crontab, without the trailing newline", fake: fakeCrontab{stderr: "no crontab for " + cronTestUser, exitCode: 1}, content: ""},
		{name: "the spool cannot be opened", fake: fakeCrontab{stderr: "crontab: can't open your crontab file: Permission denied\n", exitCode: 1}, unknown: true},
		{name: "the user is not allowed", fake: fakeCrontab{stderr: "You (" + cronTestUser + ") are not allowed to use this program (crontab)\nSee crontab(1) for more information\n", exitCode: 1}, unknown: true},
		{name: "exit status 1 and nothing said", fake: fakeCrontab{exitCode: 1}, unknown: true},
		{name: "no crontab for a different user", fake: fakeCrontab{stderr: "no crontab for root\n", exitCode: 1}, unknown: true},
		{name: "the no-crontab line with something else beside it", fake: fakeCrontab{stderr: "crontab: warning: spool is read-only\nno crontab for " + cronTestUser + "\n", exitCode: 1}, unknown: true},
		{name: "the no-crontab line with a different exit status", fake: fakeCrontab{stderr: "no crontab for " + cronTestUser + "\n", exitCode: 2}, unknown: true},
		{name: "the no-crontab line beside printed output", fake: fakeCrontab{content: "0 3 * * * true\n", stderr: "no crontab for " + cronTestUser + "\n", exitCode: 1}, unknown: true},
		{name: "the command did not exit (killed or not started)", fake: fakeCrontab{exitCode: -1}, unknown: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := tc.fake
			installFakeCrontab(t, &fake)
			content, err := readCrontab(cronTestUser)
			if tc.unknown {
				if !cronUnreadableAnswer(err) || content != "" {
					t.Fatalf("readCrontab = %q, %v; want the fixed unreadable error", content, err)
				}
				return
			}
			if err != nil || content != tc.content {
				t.Fatalf("readCrontab = %q, %v; want %q", content, err, tc.content)
			}
		})
	}
}

// The list no longer answers "no jobs" for a crontab it could not read.
func TestListCronJobsReportsAFailedReadAsAnError(t *testing.T) {
	installFakeCrontab(t, &fakeCrontab{stderr: "crontab: can't open your crontab file\n", exitCode: 1})
	var resp ListCronJobsResponse
	err := listCronJobsFor(cronTestUser, &resp)
	if !cronUnreadableAnswer(err) {
		t.Fatalf("err = %v, want %q", err, transport.CronStateUnreadable)
	}
	if resp.Jobs != nil || resp.Version != "" {
		t.Fatalf("a failed read still answered a list: %+v", resp)
	}
}

func TestListCronJobsAnswersTheJobsWithTheCrontabVersion(t *testing.T) {
	fake := installFakeCrontab(t, &fakeCrontab{content: ownerCrontab})
	var resp ListCronJobsResponse
	if err := listCronJobsFor(cronTestUser, &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Jobs) != 2 || resp.Version != cronVersion(ownerCrontab) || !strings.HasPrefix(resp.Version, "ct1-") {
		t.Fatalf("list = %+v", resp)
	}
	fake.content = ownerCrontab + "0 4 * * * /usr/bin/true\n"
	var changed ListCronJobsResponse
	_ = listCronJobsFor(cronTestUser, &changed)
	if changed.Version == resp.Version {
		t.Fatal("a changed crontab kept the same version")
	}

	// No crontab yet is a legitimate empty list, with a version a first job
	// can be added from.
	installFakeCrontab(t, noCrontabYet())
	var empty ListCronJobsResponse
	if err := listCronJobsFor(cronTestUser, &empty); err != nil {
		t.Fatal(err)
	}
	if empty.Jobs == nil || len(empty.Jobs) != 0 || empty.Version != cronVersion("") {
		t.Fatalf("no crontab answered %+v, want an empty list with the empty version", empty)
	}
}

// The defect: the read failed, getCrontab answered "", and adding one job
// installed a crontab of one line over the owner's file. A failed read now
// stops every change before anything is installed.
func TestCronChangesNeverInstallACrontabBuiltFromAFailedRead(t *testing.T) {
	version := cronVersion(ownerCrontab)
	changes := map[string]func() error{
		"add": func() error {
			return addCronJobFor(cronTestUser, &AddCronJobRequest{Schedule: "0 3 * * *", Command: "/usr/bin/true", Version: version})
		},
		"update": func() error {
			return updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
				ID: generateCronID("15 2 * * * /usr/local/bin/report --quiet"), Schedule: "0 3 * * *",
				Command: "/usr/bin/true", Enabled: true, Version: version,
			})
		},
		"delete": func() error {
			return deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{
				ID: generateCronID("15 2 * * * /usr/local/bin/report --quiet"), Version: version,
			})
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{stderr: "crontab: can't open your crontab file\n", exitCode: 1})
			err := change()
			if !cronUnreadableAnswer(err) {
				t.Fatalf("err = %v, want %q", err, transport.CronStateUnreadable)
			}
			if len(fake.installs) != 0 {
				t.Fatalf("a crontab was installed after a failed read: %q", fake.installs)
			}
		})
	}
}

func TestCronChangesRequireTheVersionOfTheCrontabTheyWereBuiltFrom(t *testing.T) {
	reportID := generateCronID("15 2 * * * /usr/local/bin/report --quiet")
	type change func(version string) error
	changes := map[string]change{
		"add": func(v string) error {
			return addCronJobFor(cronTestUser, &AddCronJobRequest{Schedule: "0 3 * * *", Command: "/usr/bin/true", Version: v})
		},
		"update": func(v string) error {
			return updateCronJobFor(cronTestUser, &UpdateCronJobRequest{ID: reportID, Schedule: "0 3 * * *", Command: "/usr/bin/true", Enabled: true, Version: v})
		},
		"delete": func(v string) error {
			return deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{ID: reportID, Version: v})
		},
	}
	for name, run := range changes {
		t.Run(name+" without a version", func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{content: ownerCrontab})
			if err := run(""); err == nil || err.Error() != transport.CronVersionRequired {
				t.Fatalf("err = %v, want %q", err, transport.CronVersionRequired)
			}
			if len(fake.installs) != 0 {
				t.Fatalf("installed %q", fake.installs)
			}
		})
		t.Run(name+" with the version of an older crontab", func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{content: ownerCrontab})
			stale := cronVersion("15 2 * * * /usr/local/bin/report --quiet\n")
			if err := run(stale); err == nil || err.Error() != transport.CronStateChanged {
				t.Fatalf("err = %v, want %q", err, transport.CronStateChanged)
			}
			if len(fake.installs) != 0 || fake.content != ownerCrontab {
				t.Fatalf("the owner's crontab was changed by a stale request: %q", fake.installs)
			}
		})
		t.Run(name+" with the current version", func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{content: ownerCrontab})
			if err := run(cronVersion(ownerCrontab)); err != nil {
				t.Fatal(err)
			}
			if len(fake.installs) != 1 || !strings.HasSuffix(fake.installs[0], "\n") {
				t.Fatalf("installs = %q, want one crontab ending in a newline", fake.installs)
			}
			if !strings.Contains(fake.installs[0], "MAILTO=owner@example.com\n") ||
				!strings.Contains(fake.installs[0], "*/5 * * * * /usr/bin/php /var/www/example.com/cron.php\n") {
				t.Fatalf("lines the change did not name were lost: %q", fake.installs[0])
			}
		})
	}
}

func TestAddCronJobAppendsToTheCrontabItRead(t *testing.T) {
	fake := installFakeCrontab(t, &fakeCrontab{content: ownerCrontab})
	err := addCronJobFor(cronTestUser, &AddCronJobRequest{
		Schedule: "0 3 * * *", Command: "/usr/bin/true", Comment: "nightly", Version: cronVersion(ownerCrontab),
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := ownerCrontab + "# nightly\n0 3 * * * /usr/bin/true\n"; len(fake.installs) != 1 || fake.installs[0] != want {
		t.Fatalf("installed %q\n want %q", fake.installs, want)
	}

	// The first job of a user who has no crontab yet.
	first := installFakeCrontab(t, noCrontabYet())
	if err := addCronJobFor(cronTestUser, &AddCronJobRequest{Schedule: "0 3 * * *", Command: "/usr/bin/true", Version: cronVersion("")}); err != nil {
		t.Fatal(err)
	}
	if len(first.installs) != 1 || first.installs[0] != "0 3 * * * /usr/bin/true\n" {
		t.Fatalf("installed %q", first.installs)
	}
}

func TestAddCronJobRefusesAnExactDuplicate(t *testing.T) {
	withDisabled := ownerCrontab + "# DISABLED: 0 6 * * 1 /usr/local/bin/weekly\n"
	cases := []struct {
		name, schedule, command string
	}{
		{"the same line", "*/5 * * * *", "/usr/bin/php /var/www/example.com/cron.php"},
		{"the same line with different spacing", "*/5  *  * * *", "/usr/bin/php   /var/www/example.com/cron.php"},
		{"a job that exists but is disabled", "0 6 * * 1", "/usr/local/bin/weekly"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{content: withDisabled})
			err := addCronJobFor(cronTestUser, &AddCronJobRequest{Schedule: tc.schedule, Command: tc.command, Version: cronVersion(withDisabled)})
			if err == nil || err.Error() != transport.CronJobDuplicate {
				t.Fatalf("err = %v, want %q", err, transport.CronJobDuplicate)
			}
			if len(fake.installs) != 0 {
				t.Fatalf("a duplicate was installed: %q", fake.installs)
			}
		})
	}

	// The same command on another schedule, or another command on the same
	// schedule, is a different job.
	for _, job := range [][2]string{
		{"*/10 * * * *", "/usr/bin/php /var/www/example.com/cron.php"},
		{"*/5 * * * *", "/usr/bin/php /var/www/example.com/other.php"},
	} {
		fake := installFakeCrontab(t, &fakeCrontab{content: withDisabled})
		if err := addCronJobFor(cronTestUser, &AddCronJobRequest{Schedule: job[0], Command: job[1], Version: cronVersion(withDisabled)}); err != nil {
			t.Fatalf("%v refused: %v", job, err)
		}
		if len(fake.installs) != 1 {
			t.Fatalf("%v was not installed", job)
		}
	}
}
