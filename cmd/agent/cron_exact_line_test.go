package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// A crontab as an owner keeps one: a header that happens to stand directly
// above a job, a job the Panel disabled, blank lines, an environment line.
// Every change below names exactly one job, and every other line must come
// back byte for byte and in place.
const mixedCrontab = "MAILTO=owner@example.com\n" +
	"\n" +
	"# m h  dom mon dow   command\n" +
	"15 2 * * * /usr/local/bin/report --quiet\n" +
	"# DISABLED: 0 6 * * 1 /usr/local/bin/weekly\n" +
	"*/5 * * * * /usr/bin/php /var/www/example.com/cron.php\n" +
	"\n" +
	"# end of the owner's jobs\n"

func cronIDs(t *testing.T, content string) map[string]CronJob {
	t.Helper()
	jobs := map[string]CronJob{}
	for _, job := range parseCrontab(content) {
		jobs[job.Schedule+" "+job.Command] = job
	}
	return jobs
}

// The defect: the writers skipped every line that begins with `#`, so a job the
// Panel had disabled could not be found again. It was listed, and every action
// on it answered "cron job not found".
func TestADisabledCronJobCanBeEnabledChangedAndDeleted(t *testing.T) {
	weekly := cronIDs(t, mixedCrontab)["0 6 * * 1 /usr/local/bin/weekly"]
	if weekly.ID == "" || weekly.Enabled {
		t.Fatalf("the disabled job is not listed as a disabled job: %+v", weekly)
	}

	t.Run("enabled again", func(t *testing.T) {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		err := updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
			ID: weekly.ID, Schedule: "0 6 * * 1", Command: "/usr/local/bin/weekly", Enabled: true, Version: cronVersion(mixedCrontab),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(mixedCrontab, "# DISABLED: 0 6 * * 1 /usr/local/bin/weekly\n", "0 6 * * 1 /usr/local/bin/weekly\n", 1)
		if len(fake.installs) != 1 || fake.installs[0] != want {
			t.Fatalf("installed %q\n want %q", fake.installs, want)
		}
		// Enabled or disabled, it is the same job with the same ID.
		if again := cronIDs(t, want)["0 6 * * 1 /usr/local/bin/weekly"]; again.ID != weekly.ID || !again.Enabled {
			t.Fatalf("after enabling: %+v, want the same ID, enabled", again)
		}
	})

	t.Run("changed while it stays disabled", func(t *testing.T) {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		err := updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
			ID: weekly.ID, Schedule: "30 7 * * 1", Command: "/usr/local/bin/weekly --full", Enabled: false, Version: cronVersion(mixedCrontab),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(mixedCrontab, "# DISABLED: 0 6 * * 1 /usr/local/bin/weekly\n", "# DISABLED: 30 7 * * 1 /usr/local/bin/weekly --full\n", 1)
		if len(fake.installs) != 1 || fake.installs[0] != want {
			t.Fatalf("installed %q\n want %q", fake.installs, want)
		}
	})

	t.Run("deleted", func(t *testing.T) {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		if err := deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{ID: weekly.ID, Version: cronVersion(mixedCrontab)}); err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(mixedCrontab, "# DISABLED: 0 6 * * 1 /usr/local/bin/weekly\n", "", 1)
		if len(fake.installs) != 1 || fake.installs[0] != want {
			t.Fatalf("installed %q\n want %q", fake.installs, want)
		}
	})
}

// The defect: deleting a job also removed the line above it whenever that line
// began with `#` (the owner's header here, or a disabled job), and dropped
// every blank line of the crontab.
func TestDeletingACronJobRemovesOnlyItsOwnLine(t *testing.T) {
	jobs := cronIDs(t, mixedCrontab)
	cases := map[string]string{
		"the job under the owner's header": "15 2 * * * /usr/local/bin/report --quiet",
		"the job under a disabled job":     "*/5 * * * * /usr/bin/php /var/www/example.com/cron.php",
	}
	for name, line := range cases {
		t.Run(name, func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
			if err := deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{ID: jobs[line].ID, Version: cronVersion(mixedCrontab)}); err != nil {
				t.Fatal(err)
			}
			want := strings.Replace(mixedCrontab, line+"\n", "", 1)
			if len(fake.installs) != 1 || fake.installs[0] != want {
				t.Fatalf("installed %q\n want %q", fake.installs, want)
			}
		})
	}
}

func TestUpdatingACronJobRewritesOnlyItsOwnLine(t *testing.T) {
	jobs := cronIDs(t, mixedCrontab)

	t.Run("disabled: the header above it stays, no second description is written", func(t *testing.T) {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		report := jobs["15 2 * * * /usr/local/bin/report --quiet"]
		err := updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
			ID: report.ID, Schedule: "15 2 * * *", Command: "/usr/local/bin/report --quiet", Enabled: false,
			Comment: "another description", Version: cronVersion(mixedCrontab),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(mixedCrontab, "15 2 * * * /usr/local/bin/report --quiet\n", "# DISABLED: 15 2 * * * /usr/local/bin/report --quiet\n", 1)
		if len(fake.installs) != 1 || fake.installs[0] != want {
			t.Fatalf("installed %q\n want %q", fake.installs, want)
		}
	})

	t.Run("a description goes above a job that stands under a disabled job, not onto that job", func(t *testing.T) {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		php := jobs["*/5 * * * * /usr/bin/php /var/www/example.com/cron.php"]
		err := updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
			ID: php.ID, Schedule: "*/10 * * * *", Command: "/usr/bin/php /var/www/example.com/cron.php", Enabled: true,
			Comment: "site cron", Version: cronVersion(mixedCrontab),
		})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.Replace(mixedCrontab, "*/5 * * * * /usr/bin/php /var/www/example.com/cron.php\n",
			"# site cron\n*/10 * * * * /usr/bin/php /var/www/example.com/cron.php\n", 1)
		if len(fake.installs) != 1 || fake.installs[0] != want {
			t.Fatalf("installed %q\n want %q", fake.installs, want)
		}
	})

	t.Run("a request that changes nothing installs nothing", func(t *testing.T) {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		report := jobs["15 2 * * * /usr/local/bin/report --quiet"]
		err := updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
			ID: report.ID, Schedule: "15 2 * * *", Command: "/usr/local/bin/report --quiet", Enabled: true,
			Comment: report.Comment, Version: cronVersion(mixedCrontab),
		})
		if err != nil || len(fake.installs) != 0 {
			t.Fatalf("err = %v, installs = %q", err, fake.installs)
		}
	})
}

// A job is its whole text. The earlier ID was the 32-bit Java string hash,
// which is the same for "Aa" and "BB"; a change or delete then reached the
// first of the two jobs.
func TestCronJobsAreIdentifiedByTheirWholeText(t *testing.T) {
	crontab := "0 1 * * * /usr/local/bin/Aa.sh\n0 1 * * * /usr/local/bin/BB.sh\n"
	jobs := cronIDs(t, crontab)
	first, second := jobs["0 1 * * * /usr/local/bin/Aa.sh"], jobs["0 1 * * * /usr/local/bin/BB.sh"]
	if first.ID == "" || first.ID == second.ID {
		t.Fatalf("two different jobs share the ID %q", first.ID)
	}
	fake := installFakeCrontab(t, &fakeCrontab{content: crontab})
	if err := deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{ID: second.ID, Version: cronVersion(crontab)}); err != nil {
		t.Fatal(err)
	}
	if len(fake.installs) != 1 || fake.installs[0] != "0 1 * * * /usr/local/bin/Aa.sh\n" {
		t.Fatalf("installed %q, want only the first job left", fake.installs)
	}

	// An environment line and an ordinary comment are not jobs and cannot be
	// reached by an ID, even one computed from their own text.
	for _, line := range []string{"MAILTO=owner@example.com", "# m h  dom mon dow   command"} {
		fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
		err := deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{ID: generateCronID(line), Version: cronVersion(mixedCrontab)})
		if err == nil || len(fake.installs) != 0 {
			t.Fatalf("%q was deleted through a job ID (err = %v)", line, err)
		}
	}
}

// The same job on two lines (an owner wrote the second, or kept a disabled
// copy): which of them a request means cannot be known, so nothing changes.
func TestACronJobThatStandsTwiceIsNotChanged(t *testing.T) {
	twice := "0 1 * * * /usr/local/bin/nightly\n# DISABLED: 0 1 * * * /usr/local/bin/nightly\n"
	id := generateCronID("0 1 * * * /usr/local/bin/nightly")
	changes := map[string]func() error{
		"update": func() error {
			return updateCronJobFor(cronTestUser, &UpdateCronJobRequest{ID: id, Schedule: "0 2 * * *", Command: "/usr/local/bin/nightly", Enabled: true, Version: cronVersion(twice)})
		},
		"delete": func() error {
			return deleteCronJobFor(cronTestUser, &DeleteCronJobRequest{ID: id, Version: cronVersion(twice)})
		},
	}
	for name, change := range changes {
		t.Run(name, func(t *testing.T) {
			fake := installFakeCrontab(t, &fakeCrontab{content: twice})
			if err := change(); err == nil || err.Error() != transport.CronJobAmbiguous {
				t.Fatalf("err = %v, want %q", err, transport.CronJobAmbiguous)
			}
			if len(fake.installs) != 0 {
				t.Fatalf("installed %q", fake.installs)
			}
		})
	}
}

func TestChangingACronJobIntoACopyOfAnotherIsRefused(t *testing.T) {
	jobs := cronIDs(t, mixedCrontab)
	fake := installFakeCrontab(t, &fakeCrontab{content: mixedCrontab})
	err := updateCronJobFor(cronTestUser, &UpdateCronJobRequest{
		ID: jobs["15 2 * * * /usr/local/bin/report --quiet"].ID, Schedule: "0 6 * * 1", Command: "/usr/local/bin/weekly",
		Enabled: true, Version: cronVersion(mixedCrontab),
	})
	if err == nil || err.Error() != transport.CronJobDuplicate || len(fake.installs) != 0 {
		t.Fatalf("err = %v, installs = %q; want the duplicate refusal and no install", err, fake.installs)
	}
}
