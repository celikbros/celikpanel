package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"path"
	"sort"
	"time"

	"github.com/alicelik/celikpanel/internal/backupspec"
)

// Scheduled backups. A background loop wakes periodically, finds domains whose
// schedule is due, runs the backup through the agent and prunes old copies to
// the configured retention. The panel owns the timing (SQLite state) and the
// agent does the privileged file/db work — the same split as everything else.
//
// Zamanlanmış yedekler. Bir arka plan döngüsü periyodik uyanır, zamanlaması
// gelmiş domain'leri bulur, yedeği agent üzerinden koşar ve eski kopyaları
// ayarlanan saklamaya budar. Zamanlamayı panel tutar (SQLite durumu), ayrıcalıklı
// dosya/db işini agent yapar.

const (
	backupScheduleQueryTimeout = 5 * time.Second
	backupScheduleJobTimeout   = 2 * time.Hour
	backupScheduleWriteTimeout = 5 * time.Second
)

type backupSchedulerLimits struct {
	query time.Duration
	job   time.Duration
	write time.Duration
}

var productionBackupSchedulerLimits = backupSchedulerLimits{
	query: backupScheduleQueryTimeout,
	job:   backupScheduleJobTimeout,
	write: backupScheduleWriteTimeout,
}

// startBackupScheduler runs the due-schedule check on a fixed cadence. The
// first run is immediate-ish (after one interval) so a fresh panel does not
// wait long, and the cadence is coarse (backups are daily/weekly, not urgent).
// startBackupScheduler, gecikmiş-zamanlama kontrolünü sabit aralıkla koşar.
func (p *Panel) startBackupScheduler() {
	go func() {
		// Coarse cadence: 30 min is far finer than daily/weekly, so a due
		// backup runs within half an hour of becoming due.
		// Kaba tempo: 30 dk, günlük/haftalıktan çok daha ince; gecikmiş bir
		// yedek, gecikmesinden yarım saat içinde koşar.
		ticker := time.NewTicker(30 * time.Minute)
		defer ticker.Stop()
		p.runDueBackups()
		for range ticker.C {
			p.runDueBackups()
		}
	}()
}

// runDueBackups backs up every domain whose schedule is enabled and due.
// runDueBackups, zamanlaması etkin ve gecikmiş her domain'i yedekler.
func (p *Panel) runDueBackups() {
	p.runDueBackupsWithLimits(context.Background(), productionBackupSchedulerLimits)
}

// runDueBackupsWithLimits is the bounded scheduler implementation. The short
// query/write contexts protect SQLite pool availability, while every backup
// receives a fresh, longer context so one stuck agent call cannot block this
// scheduler forever or consume the next job's budget.
func (p *Panel) runDueBackupsWithLimits(parent context.Context, limits backupSchedulerLimits) {
	queryCtx, cancelQuery := context.WithTimeout(parent, limits.query)
	rows, err := p.db.GetDB().QueryContext(queryCtx, `
		SELECT bs.id, d.id, d.subscription_id, d.name,
		       COALESCE((SELECT s.document_root FROM sites s
		                 WHERE s.domain_id=d.id ORDER BY s.id LIMIT 1), ''),
		       bs.frequency, bs.backup_type, bs.retention, bs.last_run,
		       bs.active_job_key
		FROM backup_schedules bs JOIN domains d ON d.id = bs.domain_id
		WHERE bs.enabled = 1
		ORDER BY bs.id`)
	if err != nil {
		cancelQuery()
		log.Printf("backup scheduler query: %v", err)
		return
	}
	type job struct {
		scheduleID   int
		domain       backupDomain
		freq, btype  string
		retention    int
		lastRun      *string
		activeJobKey *string
	}
	var jobs []job
	var scanErr error
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.scheduleID, &j.domain.ID, &j.domain.SubscriptionID, &j.domain.Name,
			&j.domain.DocumentRoot, &j.freq, &j.btype, &j.retention, &j.lastRun,
			&j.activeJobKey); err != nil {
			scanErr = err
			break
		}
		if j.domain.DocumentRoot == "" {
			j.domain.DocumentRoot = path.Join("/var/www", j.domain.Name)
		}
		jobs = append(jobs, j)
	}
	rowsErr := rows.Err()
	closeErr := rows.Close()
	cancelQuery()
	if scanErr != nil {
		log.Printf("backup scheduler scan: %v", scanErr)
		return
	}
	if rowsErr != nil {
		log.Printf("backup scheduler rows: %v", rowsErr)
		return
	}
	if closeErr != nil {
		log.Printf("backup scheduler close rows: %v", closeErr)
		return
	}

	now := time.Now()
	for _, j := range jobs {
		if j.activeJobKey == nil && !backupDue(j.freq, j.lastRun, now) {
			continue
		}
		jobKey, err := p.claimBackupScheduleJobKey(
			parent, limits.write, j.scheduleID, j.activeJobKey,
		)
		if err != nil {
			log.Printf("scheduled backup %s job claim: %v", j.domain.Name, err)
			continue
		}
		attemptedAt := time.Now().UTC().Format(time.RFC3339)
		if err := p.updateBackupScheduleStatus(parent, limits.write, j.scheduleID, jobKey,
			attemptedAt, `running`, ``, false); err != nil {
			log.Printf(`scheduled backup %s status running: %v`, j.domain.Name, err)
			continue
		}
		jobCtx, cancelJob := context.WithTimeout(parent, limits.job)
		err = p.runScheduledBackup(jobCtx, j.domain, j.btype, j.retention, jobKey)
		cancelJob()
		if err != nil {
			failureCode := `BACKUP_JOB_FAILED`
			if errors.Is(err, context.DeadlineExceeded) {
				failureCode = `BACKUP_JOB_TIMED_OUT`
			}
			if statusErr := p.updateBackupScheduleStatus(parent, limits.write, j.scheduleID, jobKey,
				attemptedAt, `failed`, failureCode, false); statusErr != nil {
				log.Printf(`scheduled backup %s failure status: %v`, j.domain.Name, statusErr)
			}
			log.Printf("scheduled backup %s: %v", j.domain.Name, err)
			continue
		}

		if err := p.updateBackupScheduleStatus(parent, limits.write, j.scheduleID, jobKey,
			attemptedAt, `success`, ``, true); err != nil {
			log.Printf(`scheduled backup %s success status: %v`, j.domain.Name, err)
		}
	}
}

func (p *Panel) claimBackupScheduleJobKey(
	parent context.Context,
	timeout time.Duration,
	scheduleID int,
	existing *string,
) (string, error) {
	if existing != nil {
		if !backupspec.ValidJobKey(*existing) {
			return "", errors.New("stored backup job key is invalid")
		}
		return *existing, nil
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return "", fmt.Errorf("generate backup job key: %w", err)
	}
	candidate := "schedule:" + hex.EncodeToString(random)
	writeCtx, cancelWrite := context.WithTimeout(parent, timeout)
	defer cancelWrite()
	if _, err := p.db.GetDB().ExecContext(writeCtx, `
		UPDATE backup_schedules
		SET active_job_key = ?
		WHERE id = ? AND enabled = 1
		  AND (active_job_key IS NULL OR active_job_key = '')`,
		candidate, scheduleID); err != nil {
		return "", err
	}
	var saved string
	if err := p.db.GetDB().QueryRowContext(writeCtx, `
		SELECT active_job_key
		FROM backup_schedules
		WHERE id = ? AND enabled = 1`, scheduleID).Scan(&saved); err != nil {
		return "", err
	}
	if !backupspec.ValidJobKey(saved) {
		return "", errors.New("stored backup job key is invalid")
	}
	return saved, nil
}

func (p *Panel) updateBackupScheduleStatus(
	parent context.Context,
	timeout time.Duration,
	scheduleID int,
	jobKey string,
	attemptedAt, status, errorCode string,
	succeeded bool,
) error {
	writeCtx, cancelWrite := context.WithTimeout(parent, timeout)
	defer cancelWrite()
	if succeeded {
		result, err := p.db.GetDB().ExecContext(writeCtx, `
			UPDATE backup_schedules
			SET last_attempt = ?, last_run = ?, last_status = ?, last_error = NULL,
			    active_job_key = NULL
			WHERE id = ? AND active_job_key = ?`,
			attemptedAt, time.Now().UTC().Format(time.RFC3339), status, scheduleID, jobKey)
		return verifyBackupScheduleStatusUpdate(result, err)
	}
	var safeError any
	if errorCode != `` {
		safeError = errorCode
	}
	result, err := p.db.GetDB().ExecContext(writeCtx, `
		UPDATE backup_schedules
		SET last_attempt = ?, last_status = ?, last_error = ?
		WHERE id = ? AND active_job_key = ?`,
		attemptedAt, status, safeError, scheduleID, jobKey)
	return verifyBackupScheduleStatusUpdate(result, err)
}

func verifyBackupScheduleStatusUpdate(result sql.Result, execErr error) error {
	if execErr != nil {
		return execErr
	}
	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("inspect backup schedule status update: %w", err)
	}
	if rowsAffected != 1 {
		return fmt.Errorf(
			"backup schedule status update lost job ownership: rows affected %d",
			rowsAffected,
		)
	}
	return nil
}

// backupDue reports whether a schedule that last ran at lastRun is due now.
// A never-run schedule is always due.
// backupDue, en son lastRun'da koşan bir zamanlamanın şimdi gecikmiş olup
// olmadığını bildirir. Hiç koşmamış zamanlama her zaman gecikmiştir.
func backupDue(freq string, lastRun *string, now time.Time) bool {
	if lastRun == nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, *lastRun)
	if err != nil {
		return true
	}
	interval := 24 * time.Hour
	if freq == "weekly" {
		interval = 7 * 24 * time.Hour
	}
	return now.Sub(last) >= interval
}

// runScheduledBackup creates one backup for a domain and prunes older copies
// beyond the retention count.
// runScheduledBackup, bir domain için bir yedek oluşturur ve saklama sayısını
// aşan eski kopyaları budar.
func (p *Panel) runScheduledBackup(
	ctx context.Context,
	d backupDomain,
	btype string,
	retention int,
	jobKey string,
) error {
	req := backupspec.CreateRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, Type: btype,
		Origin: backupspec.OriginScheduled, JobKey: jobKey, SourceDir: d.DocumentRoot,
	}
	switch btype {
	case backupspec.TypeFiles:
	case backupspec.TypeFull:
		databases, err := p.domainDatabaseIdentities(ctx, d)
		if err != nil {
			p.auditBackupSystem(ctx, "backup.create.failed:scheduled — "+auditReason(err.Error()), d.ID)
			return err
		}
		req.Databases = databases
	default:
		err := &backupError{"unsupported scheduled backup type"}
		p.auditBackupSystem(ctx, "backup.create.failed:scheduled — "+auditReason(err.Error()), d.ID)
		return err
	}
	var resp backupspec.CreateResponse
	if err := p.callAgentContext(ctx, "Agent.CreateBackup", &req, &resp); err != nil {
		p.auditBackupSystem(ctx, "backup.create.failed:scheduled — "+auditReason(err.Error()), d.ID)
		return err
	}
	if !resp.Success || resp.Error != "" {
		err := &backupError{resp.Error}
		p.auditBackupSystem(ctx, "backup.create.failed:scheduled — "+auditReason(err.Error()), d.ID)
		return err
	}
	p.auditBackupSystem(ctx, "backup.create:scheduled:"+btype, d.ID)
	if err := p.pruneBackups(ctx, d, retention); err != nil {
		log.Printf("scheduled backup retention %s: %v", d.Name, err)
	}
	return nil
}

type backupError struct{ msg string }

func (e *backupError) Error() string { return e.msg }

// pruneBackups keeps the newest `retention` file/full backups for a domain and
// deletes the rest. Database backups are left alone — retention here is about
// the scheduled file/full snapshots.
// pruneBackups, bir domain için en yeni `retention` dosya/tam yedeğini tutar,
// gerisini siler.
func (p *Panel) pruneBackups(ctx context.Context, d backupDomain, retention int) error {
	if retention < 1 {
		retention = 1
	}
	req := backupspec.ListRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name,
	}
	var resp backupspec.ListResponse
	if err := p.callAgentContext(ctx, "Agent.ListBackups", &req, &resp); err != nil {
		return err
	}
	var scheduled []backupspec.Info
	for _, b := range resp.Backups {
		if b.Origin == backupspec.OriginScheduled &&
			(b.Type == backupspec.TypeFiles || b.Type == backupspec.TypeFull) {
			scheduled = append(scheduled, b)
		}
	}
	sort.Slice(scheduled, func(i, j int) bool {
		if scheduled[i].CreatedAt.Equal(scheduled[j].CreatedAt) {
			return scheduled[i].Name > scheduled[j].Name
		}
		return scheduled[i].CreatedAt.After(scheduled[j].CreatedAt)
	})
	if len(scheduled) <= retention {
		return nil
	}
	for _, backup := range scheduled[retention:] {
		deleteReq := backupspec.DeleteRequest{
			ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
			DomainID: d.ID, DomainName: d.Name, BackupName: backup.Name,
		}
		var ok bool
		if err := p.callAgentContext(ctx, "Agent.DeleteBackup", &deleteReq, &ok); err != nil {
			p.auditBackupSystem(ctx, "backup.delete.failed:retention — "+auditReason(err.Error()), d.ID)
			return err
		}
		if !ok {
			p.auditBackupSystem(ctx, "backup.delete.failed:retention — agent refused deletion", d.ID)
			return &backupError{"agent refused retention deletion"}
		}
		p.auditBackupSystem(ctx, "backup.delete:retention", d.ID)
	}
	return nil
}

func (p *Panel) auditBackupSystem(ctx context.Context, action string, domainID int) {
	if _, err := p.db.GetDB().ExecContext(ctx, `
		INSERT INTO audit_logs (action, resource_type, resource_id)
		VALUES (?, 'domain', ?)`, action, domainID); err != nil {
		log.Printf("audit write failed (%s): %v", action, err)
	}
}

// handleBackupSchedule handles GET/PUT/DELETE for a domain's backup schedule.
// handleBackupSchedule, bir domain'in yedek zamanlaması için GET/PUT/DELETE'i
// karşılar.
// backupScheduleSettings is what the owner set for one domain: whether a
// schedule exists and its four settings. Run status (last run, last attempt,
// the active job) is not part of it, so a background run never makes a save
// look stale.
// backupScheduleSettings, sahibin bir alan adı için ayarladığıdır. Koşu durumu
// buna dahil değildir; arka plan koşusu bir kaydı eskimiş göstermez.
type backupScheduleSettings struct {
	exists     bool
	frequency  string
	backupType string
	retention  int
	enabled    int
}

// version identifies these exact settings. The schedule GET returns it and the
// PUT and DELETE must carry it back (8 Oct 2026): a form that never loaded the
// schedule, or loaded an older one, cannot replace it. Before this a failed GET
// left the form on "daily / files / 7" and Turn on wrote those over the real
// schedule — a full backup became files-only and the next run pruned copies
// beyond 7.
// version tam bu ayarları tanımlar. Zamanlama GET'i onu döndürür; PUT ve DELETE
// geri taşımak zorundadır.
func (s backupScheduleSettings) version() string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%t\x00%s\x00%s\x00%d\x00%d",
		s.exists, s.frequency, s.backupType, s.retention, s.enabled)))
	return "bs1-" + hex.EncodeToString(sum[:])
}

type backupScheduleRunStatus struct {
	lastRun, lastAttempt, lastStatus, lastError *string
}

func (p *Panel) readBackupSchedule(ctx context.Context, domainID int) (backupScheduleSettings, backupScheduleRunStatus, error) {
	var settings backupScheduleSettings
	var status backupScheduleRunStatus
	err := p.db.GetDB().QueryRowContext(ctx, `
		SELECT frequency, backup_type, retention, enabled, last_run,
		       last_attempt, last_status, last_error
		FROM backup_schedules WHERE domain_id = ?`, domainID).Scan(
		&settings.frequency, &settings.backupType, &settings.retention, &settings.enabled,
		&status.lastRun, &status.lastAttempt, &status.lastStatus, &status.lastError,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return backupScheduleSettings{}, backupScheduleRunStatus{}, nil
	}
	if err != nil {
		return backupScheduleSettings{}, backupScheduleRunStatus{}, err
	}
	settings.exists = true
	return settings, status, nil
}

// backupScheduleWriteAdmitted reads the schedule a write would replace and
// refuses the write when the request does not carry its version. The caller
// then writes only where the row still holds these exact settings.
// backupScheduleWriteAdmitted, bir yazının değiştireceği zamanlamayı okur ve
// istek onun sürümünü taşımıyorsa yazıyı reddeder.
func (p *Panel) backupScheduleWriteAdmitted(w http.ResponseWriter, r *http.Request, domainID int, version string) (backupScheduleSettings, bool) {
	if version == "" {
		writeSettingsVersionRequired(w, settingsResourceBackupSchedule)
		return backupScheduleSettings{}, false
	}
	current, _, err := p.readBackupSchedule(r.Context(), domainID)
	if err != nil {
		writeServerError(w, err)
		return backupScheduleSettings{}, false
	}
	if version != current.version() {
		writeSettingsChanged(w, settingsResourceBackupSchedule)
		return backupScheduleSettings{}, false
	}
	return current, true
}

func backupScheduleWriteChangedOneRow(result sql.Result, execErr error) (bool, error) {
	if execErr != nil {
		return false, execErr
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows == 1, nil
}

func (p *Panel) handleBackupSchedule(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		settings, status, err := p.readBackupSchedule(r.Context(), domainID)
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !settings.exists {
			json.NewEncoder(w).Encode(map[string]any{"enabled": false, "version": settings.version()})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"enabled": settings.enabled == 1, "frequency": settings.frequency,
			"backup_type": settings.backupType, "retention": settings.retention,
			"last_run": status.lastRun, "last_attempt": status.lastAttempt,
			"last_status": status.lastStatus, "last_error": status.lastError,
			"version": settings.version(),
		})

	case http.MethodPut:
		var req struct {
			Frequency  string `json:"frequency"`
			BackupType string `json:"backup_type"`
			Retention  int    `json:"retention"`
			Version    string `json:"version"`
		}
		if err := decodeBackupJSON(w, r, &req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Frequency != "daily" && req.Frequency != "weekly" {
			writeClientError(w, http.StatusBadRequest, "frequency must be daily or weekly")
			return
		}
		if req.BackupType != backupspec.TypeFiles && req.BackupType != backupspec.TypeFull {
			writeClientError(w, http.StatusBadRequest, "backup_type must be files or full")
			return
		}
		if req.Retention < 1 || req.Retention > 60 {
			writeClientError(w, http.StatusBadRequest, "retention must be between 1 and 60")
			return
		}
		current, admitted := p.backupScheduleWriteAdmitted(w, r, domainID, req.Version)
		if !admitted {
			return
		}
		// The write is conditional on the settings just read, so a schedule
		// that changed between the read and the write is not replaced either.
		// Yazı, az önce okunan ayarlara koşulludur.
		var written bool
		var err error
		if current.exists {
			written, err = backupScheduleWriteChangedOneRow(p.db.GetDB().ExecContext(r.Context(), `
				UPDATE backup_schedules SET
				  active_job_key = CASE WHEN backup_type = ? THEN active_job_key ELSE NULL END,
				  frequency = ?, backup_type = ?, retention = ?, enabled = 1
				WHERE domain_id = ? AND frequency = ? AND backup_type = ?
				  AND retention = ? AND enabled = ?`,
				req.BackupType, req.Frequency, req.BackupType, req.Retention,
				domainID, current.frequency, current.backupType, current.retention, current.enabled))
		} else {
			written, err = backupScheduleWriteChangedOneRow(p.db.GetDB().ExecContext(r.Context(), `
				INSERT INTO backup_schedules (domain_id, frequency, backup_type, retention, enabled)
				VALUES (?, ?, ?, ?, 1)
				ON CONFLICT(domain_id) DO NOTHING`,
				domainID, req.Frequency, req.BackupType, req.Retention))
		}
		if err != nil {
			writeServerError(w, err)
			return
		}
		if !written {
			writeSettingsChanged(w, settingsResourceBackupSchedule)
			return
		}
		p.audit(r, "backup.schedule:"+req.Frequency, "domain", domainID)
		saved := backupScheduleSettings{
			exists: true, frequency: req.Frequency, backupType: req.BackupType,
			retention: req.Retention, enabled: 1,
		}
		json.NewEncoder(w).Encode(map[string]any{"success": true, "version": saved.version()})

	case http.MethodDelete:
		current, admitted := p.backupScheduleWriteAdmitted(w, r, domainID, r.URL.Query().Get("version"))
		if !admitted {
			return
		}
		if current.exists {
			deleted, err := backupScheduleWriteChangedOneRow(p.db.GetDB().ExecContext(r.Context(), `
				DELETE FROM backup_schedules
				WHERE domain_id = ? AND frequency = ? AND backup_type = ?
				  AND retention = ? AND enabled = ?`,
				domainID, current.frequency, current.backupType, current.retention, current.enabled))
			if err != nil {
				writeServerError(w, err)
				return
			}
			if !deleted {
				writeSettingsChanged(w, settingsResourceBackupSchedule)
				return
			}
		}
		p.audit(r, "backup.schedule.off", "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{
			"success": true, "version": backupScheduleSettings{}.version(),
		})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
