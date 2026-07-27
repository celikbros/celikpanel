package main

import (
	"context"
	"encoding/json"
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
	ctx := context.Background()
	rows, err := p.db.GetDB().QueryContext(ctx, `
		SELECT d.id, d.subscription_id, d.name,
		       COALESCE((SELECT s.document_root FROM sites s
		                 WHERE s.domain_id=d.id ORDER BY s.id LIMIT 1), ''),
		       bs.frequency, bs.backup_type, bs.retention, bs.last_run
		FROM backup_schedules bs JOIN domains d ON d.id = bs.domain_id
		WHERE bs.enabled = 1`)
	if err != nil {
		log.Printf("backup scheduler: %v", err)
		return
	}
	type job struct {
		domain      backupDomain
		freq, btype string
		retention   int
		lastRun     *string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.domain.ID, &j.domain.SubscriptionID, &j.domain.Name,
			&j.domain.DocumentRoot, &j.freq, &j.btype, &j.retention, &j.lastRun) == nil {
			if j.domain.DocumentRoot == "" {
				j.domain.DocumentRoot = path.Join("/var/www", j.domain.Name)
			}
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	now := time.Now()
	for _, j := range jobs {
		if !backupDue(j.freq, j.lastRun, now) {
			continue
		}
		if err := p.runScheduledBackup(ctx, j.domain, j.btype, j.retention); err != nil {
			log.Printf("scheduled backup %s: %v", j.domain.Name, err)
			continue
		}
		p.db.GetDB().ExecContext(ctx,
			`UPDATE backup_schedules SET last_run = ? WHERE domain_id = ?`,
			now.UTC().Format(time.RFC3339), j.domain.ID)
	}
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
func (p *Panel) runScheduledBackup(ctx context.Context, d backupDomain, btype string, retention int) error {
	req := backupspec.CreateRequest{
		ProtocolVersion: backupspec.ProtocolVersion, SubscriptionID: d.SubscriptionID,
		DomainID: d.ID, DomainName: d.Name, Type: btype,
		Origin: backupspec.OriginScheduled, SourceDir: d.DocumentRoot,
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
	if err := p.agentClient.CallContext(ctx, "Agent.CreateBackup", &req, &resp); err != nil {
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
	if err := p.agentClient.CallContext(ctx, "Agent.ListBackups", &req, &resp); err != nil {
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
		if err := p.agentClient.CallContext(ctx, "Agent.DeleteBackup", &deleteReq, &ok); err != nil {
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
func (p *Panel) handleBackupSchedule(w http.ResponseWriter, r *http.Request, domainID int) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		var freq, btype string
		var retention, enabled int
		var lastRun *string
		err := p.db.GetDB().QueryRowContext(r.Context(), `
			SELECT frequency, backup_type, retention, enabled, last_run
			FROM backup_schedules WHERE domain_id = ?`, domainID).Scan(&freq, &btype, &retention, &enabled, &lastRun)
		if err != nil {
			json.NewEncoder(w).Encode(map[string]any{"enabled": false})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"enabled": enabled == 1, "frequency": freq, "backup_type": btype,
			"retention": retention, "last_run": lastRun,
		})

	case http.MethodPut:
		var req struct {
			Frequency  string `json:"frequency"`
			BackupType string `json:"backup_type"`
			Retention  int    `json:"retention"`
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
		if _, err := p.db.GetDB().ExecContext(r.Context(), `
			INSERT INTO backup_schedules (domain_id, frequency, backup_type, retention, enabled)
			VALUES (?, ?, ?, ?, 1)
			ON CONFLICT(domain_id) DO UPDATE SET
			  frequency = excluded.frequency, backup_type = excluded.backup_type,
			  retention = excluded.retention, enabled = 1`,
			domainID, req.Frequency, req.BackupType, req.Retention); err != nil {
			writeServerError(w, err)
			return
		}
		p.audit(r, "backup.schedule:"+req.Frequency, "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{"success": true})

	case http.MethodDelete:
		p.db.GetDB().ExecContext(r.Context(), `DELETE FROM backup_schedules WHERE domain_id = ?`, domainID)
		p.audit(r, "backup.schedule.off", "domain", domainID)
		json.NewEncoder(w).Encode(map[string]any{"success": true})

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
