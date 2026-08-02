package main

import (
	"context"
	"database/sql"
	"net"
	"net/http"
	"net/http/httptest"
	"net/rpc"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
	"github.com/alicelik/celikpanel/internal/transport"
)

type blockingBackupSchedulerAgent struct {
	mu            sync.Mutex
	blockedDomain int
	calls         []int
	jobKeys       []string
	release       chan struct{}
}

func (a *blockingBackupSchedulerAgent) CreateBackup(
	req *backupspec.CreateRequest,
	resp *backupspec.CreateResponse,
) error {
	a.mu.Lock()
	a.calls = append(a.calls, req.DomainID)
	a.jobKeys = append(a.jobKeys, req.JobKey)
	a.mu.Unlock()
	if req.DomainID == a.blockedDomain {
		<-a.release
	}
	resp.Success = true
	return nil
}

func (a *blockingBackupSchedulerAgent) ListBackups(
	_ *backupspec.ListRequest,
	resp *backupspec.ListResponse,
) error {
	*resp = backupspec.ListResponse{}
	return nil
}

func (a *blockingBackupSchedulerAgent) calledDomains() []int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]int(nil), a.calls...)
}

func (a *blockingBackupSchedulerAgent) calledJobKeys() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]string(nil), a.jobKeys...)
}

func attachBackupSchedulerAgent(t *testing.T, p *Panel, agent any) {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatalf("register scheduler agent: %v", err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	client, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	p.agentClient = transport.NewReconnectingClientWithContextConnector(client, connector)
	t.Cleanup(func() { _ = client.Close() })
}

func seedBackupSchedule(t *testing.T, p *Panel, domainID int, retention any) {
	t.Helper()
	if _, err := p.db.GetDB().Exec(`
		INSERT INTO backup_schedules
			(domain_id, frequency, backup_type, retention, enabled)
		VALUES (?, 'daily', 'files', ?, 1)`, domainID, retention); err != nil {
		t.Fatalf("seed backup schedule: %v", err)
	}
}

func TestUpdateBackupScheduleStatusRejectsLostJobOwnership(t *testing.T) {
	f := newPanelBackupFixture(t)
	seedBackupSchedule(t, f.panel, f.domainID, 7)

	const currentJobKey = "schedule:current-owner"
	var scheduleID int
	if err := f.panel.db.GetDB().QueryRow(
		`UPDATE backup_schedules
		 SET active_job_key = ?
		 WHERE domain_id = ?
		 RETURNING id`,
		currentJobKey, f.domainID,
	).Scan(&scheduleID); err != nil {
		t.Fatal(err)
	}

	err := f.panel.updateBackupScheduleStatus(
		context.Background(),
		time.Second,
		scheduleID,
		"schedule:stale-owner",
		time.Now().UTC().Format(time.RFC3339),
		"success",
		"",
		true,
	)
	if err == nil {
		t.Fatal("stale job key status update succeeded")
	}

	var lastRun, lastStatus sql.NullString
	var activeJobKey string
	if err := f.panel.db.GetDB().QueryRow(
		`SELECT last_run, last_status, active_job_key
		 FROM backup_schedules WHERE id = ?`, scheduleID,
	).Scan(&lastRun, &lastStatus, &activeJobKey); err != nil {
		t.Fatal(err)
	}
	if lastRun.Valid || lastStatus.Valid || activeJobKey != currentJobKey {
		t.Fatalf(
			"lost ownership mutated schedule: last_run=%v last_status=%v active_job_key=%q",
			lastRun, lastStatus, activeJobKey,
		)
	}
}

func TestRunDueBackupsAbortsOnScheduleScanError(t *testing.T) {
	f := newPanelBackupFixture(t)
	seedBackupSchedule(t, f.panel, f.domainID, "invalid-integer")
	seedBackupSchedule(t, f.panel, f.otherDomain, 7)

	f.panel.runDueBackupsWithLimits(context.Background(), backupSchedulerLimits{
		query: time.Second,
		job:   time.Second,
		write: time.Second,
	})

	if len(f.agent.createReqs) != 0 {
		t.Fatalf("agent calls after malformed schedule=%d, want 0", len(f.agent.createReqs))
	}
}

func TestRunDueBackupsTimesOutOneJobAndContinues(t *testing.T) {
	f := newPanelBackupFixture(t)
	seedBackupSchedule(t, f.panel, f.domainID, 7)
	seedBackupSchedule(t, f.panel, f.otherDomain, 7)

	agent := &blockingBackupSchedulerAgent{
		blockedDomain: f.domainID,
		release:       make(chan struct{}),
	}
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(agent.release) }) })
	attachBackupSchedulerAgent(t, f.panel, agent)

	started := time.Now()
	f.panel.runDueBackupsWithLimits(context.Background(), backupSchedulerLimits{
		query: time.Second,
		job:   100 * time.Millisecond,
		write: time.Second,
	})
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("scheduler took %s after bounded job timeout", elapsed)
	}

	if got, want := agent.calledDomains(), []int{f.domainID, f.otherDomain}; !reflect.DeepEqual(got, want) {
		t.Fatalf("called domains=%v, want %v", got, want)
	}

	var blockedLastRun, blockedAttempt, blockedStatus, blockedError, blockedJobKey sql.NullString
	if err := f.panel.db.GetDB().QueryRow(
		`SELECT last_run, last_attempt, last_status, last_error, active_job_key
		 FROM backup_schedules WHERE domain_id = ?`, f.domainID,
	).Scan(&blockedLastRun, &blockedAttempt, &blockedStatus, &blockedError, &blockedJobKey); err != nil {
		t.Fatal(err)
	}
	var successfulLastRun, successfulAttempt, successfulStatus, successfulError, successfulJobKey sql.NullString
	if err := f.panel.db.GetDB().QueryRow(
		`SELECT last_run, last_attempt, last_status, last_error, active_job_key
		 FROM backup_schedules WHERE domain_id = ?`, f.otherDomain,
	).Scan(&successfulLastRun, &successfulAttempt, &successfulStatus, &successfulError, &successfulJobKey); err != nil {
		t.Fatal(err)
	}
	if blockedLastRun.Valid {
		t.Fatalf("timed-out backup last_run=%q, want NULL", blockedLastRun.String)
	}
	if !successfulLastRun.Valid || successfulLastRun.String == "" {
		t.Fatal("successful backup did not update last_run")
	}
	if !blockedAttempt.Valid || blockedAttempt.String == "" {
		t.Fatal("timed-out backup did not update last_attempt")
	}
	if !blockedStatus.Valid || blockedStatus.String != "failed" {
		t.Fatalf("timed-out backup status=%q, want failed", blockedStatus.String)
	}
	if !blockedError.Valid || blockedError.String != "BACKUP_JOB_TIMED_OUT" {
		t.Fatalf("timed-out backup error=%q, want safe timeout code", blockedError.String)
	}
	if !blockedJobKey.Valid || !backupspec.ValidJobKey(blockedJobKey.String) {
		t.Fatalf("timed-out backup job key=%q, want durable valid key", blockedJobKey.String)
	}
	if !successfulAttempt.Valid || successfulAttempt.String == "" {
		t.Fatal("successful backup did not update last_attempt")
	}
	if !successfulStatus.Valid || successfulStatus.String != "success" {
		t.Fatalf("successful backup status=%q, want success", successfulStatus.String)
	}
	if successfulError.Valid {
		t.Fatalf("successful backup error=%q, want NULL", successfulError.String)
	}
	if successfulJobKey.Valid {
		t.Fatalf("successful backup retained job key=%q", successfulJobKey.String)
	}
	for _, key := range agent.calledJobKeys() {
		if !backupspec.ValidJobKey(key) {
			t.Fatalf("agent received invalid job key %q", key)
		}
	}
}

type latePublishingBackupSchedulerAgent struct {
	mu           sync.Mutex
	calls        []string
	published    map[string]backupspec.Info
	physicalRuns int
	started      chan struct{}
	release      chan struct{}
	lateDone     chan struct{}
}

func (a *latePublishingBackupSchedulerAgent) CreateBackup(
	req *backupspec.CreateRequest,
	resp *backupspec.CreateResponse,
) error {
	a.mu.Lock()
	a.calls = append(a.calls, req.JobKey)
	if info, ok := a.published[req.JobKey]; ok {
		a.mu.Unlock()
		resp.Success = true
		resp.Backup = info
		return nil
	}
	a.physicalRuns++
	first := a.physicalRuns == 1
	a.mu.Unlock()
	if first {
		close(a.started)
		<-a.release
	}
	info := backupspec.Info{
		Name: "scheduled-once.cpbak", Type: req.Type,
		Origin: req.Origin, CreatedAt: time.Now().UTC(), Restorable: true,
	}
	a.mu.Lock()
	a.published[req.JobKey] = info
	a.mu.Unlock()
	resp.Success = true
	resp.Backup = info
	if first {
		close(a.lateDone)
	}
	return nil
}

func (a *latePublishingBackupSchedulerAgent) ListBackups(
	_ *backupspec.ListRequest,
	resp *backupspec.ListResponse,
) error {
	*resp = backupspec.ListResponse{}
	return nil
}

func TestRunDueBackupsReusesTimedOutJobKeyAndReconcilesLateSuccess(t *testing.T) {
	f := newPanelBackupFixture(t)
	seedBackupSchedule(t, f.panel, f.domainID, 7)
	agent := &latePublishingBackupSchedulerAgent{
		published: make(map[string]backupspec.Info),
		started:   make(chan struct{}),
		release:   make(chan struct{}),
		lateDone:  make(chan struct{}),
	}
	attachBackupSchedulerAgent(t, f.panel, agent)

	f.panel.runDueBackupsWithLimits(context.Background(), backupSchedulerLimits{
		query: time.Second,
		job:   50 * time.Millisecond,
		write: time.Second,
	})
	<-agent.started
	var timedOutKey string
	if err := f.panel.db.GetDB().QueryRow(
		`SELECT active_job_key FROM backup_schedules WHERE domain_id = ?`,
		f.domainID,
	).Scan(&timedOutKey); err != nil {
		t.Fatal(err)
	}
	close(agent.release)
	select {
	case <-agent.lateDone:
	case <-time.After(time.Second):
		t.Fatal("late agent publication did not finish")
	}

	f.panel.runDueBackupsWithLimits(context.Background(), backupSchedulerLimits{
		query: time.Second,
		job:   time.Second,
		write: time.Second,
	})

	agent.mu.Lock()
	calls := append([]string(nil), agent.calls...)
	physicalRuns := agent.physicalRuns
	agent.mu.Unlock()
	if len(calls) != 2 || calls[0] != timedOutKey || calls[1] != timedOutKey {
		t.Fatalf("job keys=%v, want two retries of %q", calls, timedOutKey)
	}
	if physicalRuns != 1 {
		t.Fatalf("physical backup runs=%d, want 1", physicalRuns)
	}
	var lastRun, activeJobKey sql.NullString
	var lastStatus string
	if err := f.panel.db.GetDB().QueryRow(
		`SELECT last_run, last_status, active_job_key
		 FROM backup_schedules WHERE domain_id = ?`, f.domainID,
	).Scan(&lastRun, &lastStatus, &activeJobKey); err != nil {
		t.Fatal(err)
	}
	if !lastRun.Valid || lastStatus != "success" || activeJobKey.Valid {
		t.Fatalf("reconciled schedule last_run=%v status=%q active_key=%v",
			lastRun, lastStatus, activeJobKey)
	}
}

func TestHandleBackupScheduleGetReportsDatabaseFailure(t *testing.T) {
	f := newPanelBackupFixture(t)
	f.panel.db.Close()

	req := httptest.NewRequest(http.MethodGet, "/backup-schedule", nil)
	rec := httptest.NewRecorder()
	f.panel.handleBackupSchedule(rec, req, f.domainID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestHandleBackupScheduleDeleteReportsDatabaseFailure(t *testing.T) {
	f := newPanelBackupFixture(t)
	seedBackupSchedule(t, f.panel, f.domainID, 7)
	if _, err := f.panel.db.GetDB().Exec(`
		CREATE TRIGGER reject_backup_schedule_delete
		BEFORE DELETE ON backup_schedules
		BEGIN
			SELECT RAISE(ABORT, 'delete blocked');
		END`); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete, "/backup-schedule", nil)
	rec := httptest.NewRecorder()
	f.panel.handleBackupSchedule(rec, req, f.domainID)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}

	var count int
	if err := f.panel.db.GetDB().QueryRow(
		`SELECT COUNT(*) FROM backup_schedules WHERE domain_id = ?`, f.domainID,
	).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("schedule count=%d, want 1 after rejected delete", count)
	}
}
