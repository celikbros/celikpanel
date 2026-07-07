package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"
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
		SELECT bs.domain_id, d.name, bs.frequency, bs.backup_type, bs.retention, bs.last_run
		FROM backup_schedules bs JOIN domains d ON d.id = bs.domain_id
		WHERE bs.enabled = 1`)
	if err != nil {
		log.Printf("backup scheduler: %v", err)
		return
	}
	type job struct {
		domainID          int
		name, freq, btype string
		retention         int
		lastRun           *string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if rows.Scan(&j.domainID, &j.name, &j.freq, &j.btype, &j.retention, &j.lastRun) == nil {
			jobs = append(jobs, j)
		}
	}
	rows.Close()

	now := time.Now()
	for _, j := range jobs {
		if !backupDue(j.freq, j.lastRun, now) {
			continue
		}
		if err := p.runScheduledBackup(ctx, j.domainID, j.name, j.btype, j.retention); err != nil {
			log.Printf("scheduled backup %s: %v", j.name, err)
			continue
		}
		p.db.GetDB().ExecContext(ctx,
			`UPDATE backup_schedules SET last_run = ? WHERE domain_id = ?`,
			now.UTC().Format(time.RFC3339), j.domainID)
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
func (p *Panel) runScheduledBackup(ctx context.Context, domainID int, domainName, btype string, retention int) error {
	docroot, err := p.siteDocroot(ctx, domainID)
	if err != nil {
		return err
	}
	var resp struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	if err := p.agentClient.Call("Agent.CreateBackup", &struct {
		DomainName string `json:"domain_name"`
		Type       string `json:"type"`
		SourceDir  string `json:"source_dir"`
	}{DomainName: domainName, Type: btype, SourceDir: docroot}, &resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return &backupError{resp.Error}
	}
	p.pruneBackups(domainName, retention)
	return nil
}

type backupError struct{ msg string }

func (e *backupError) Error() string { return e.msg }

// pruneBackups keeps the newest `retention` file/full backups for a domain and
// deletes the rest. Database backups are left alone — retention here is about
// the scheduled file/full snapshots.
// pruneBackups, bir domain için en yeni `retention` dosya/tam yedeğini tutar,
// gerisini siler.
func (p *Panel) pruneBackups(domainName string, retention int) {
	if retention < 1 {
		retention = 1
	}
	var resp struct {
		Backups []struct {
			Name      string    `json:"name"`
			Type      string    `json:"type"`
			CreatedAt time.Time `json:"created_at"`
		} `json:"backups"`
	}
	if err := p.agentClient.Call("Agent.ListBackups",
		&struct{ DomainName string }{DomainName: domainName}, &resp); err != nil {
		return
	}
	// Newest first; keep the first `retention`, delete older scheduled ones.
	// The agent already returns newest-first, but sort defensively by name
	// (names embed a sortable timestamp).
	// En yeni önce; ilk `retention`'ı tut, daha eski olanları sil.
	var scheduled []string
	for _, b := range resp.Backups {
		if b.Type == "files" || b.Type == "full" {
			scheduled = append(scheduled, b.Name)
		}
	}
	if len(scheduled) <= retention {
		return
	}
	for _, name := range scheduled[retention:] {
		var ok bool
		_ = p.agentClient.Call("Agent.DeleteBackup",
			&struct{ DomainName, BackupName string }{DomainName: domainName, BackupName: name}, &ok)
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
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeClientError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		if req.Frequency != "daily" && req.Frequency != "weekly" {
			writeClientError(w, http.StatusBadRequest, "frequency must be daily or weekly")
			return
		}
		if req.BackupType != "files" && req.BackupType != "full" {
			req.BackupType = "files"
		}
		if req.Retention < 1 || req.Retention > 60 {
			req.Retention = 7
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
