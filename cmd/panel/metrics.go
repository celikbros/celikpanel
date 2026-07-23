package main

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"
)

// Server monitoring history (operator request, 23 Jul). The sampler reuses
// the exact same readers the dashboard's health strip trusts — one source of
// measurement, two time horizons: the strip shows now, this shows the last
// 48 hours.
//
// Sunucu izleme geçmişi (operatör isteği, 23 Tem). Örnekleyici, pano sağlık
// şeridinin güvendiği okuyucuların AYNILARINI kullanır — tek ölçüm kaynağı,
// iki zaman ufku: şerit şimdiyi, burası son 48 saati gösterir.

const (
	metricsInterval  = 30 * time.Second
	metricsRetention = "-48 hours"
)

// startMetricsSampler runs for the panel's lifetime. Sampling must never
// fight a user request: one INSERT + one bounded DELETE every 30s.
// startMetricsSampler panel yaşadıkça koşar. Örnekleme kullanıcı isteğiyle
// asla yarışmamalı: 30 saniyede bir INSERT + bir sınırlı DELETE.
func (p *Panel) startMetricsSampler() {
	go func() {
		ticker := time.NewTicker(metricsInterval)
		defer ticker.Stop()
		for {
			p.sampleMetricsOnce()
			<-ticker.C
		}
	}()
}

func (p *Panel) sampleMetricsOnce() {
	// sampleCPUPercent blocks ~1s for its /proc/stat delta — that is why
	// this runs in the sampler goroutine, never in a request handler.
	// sampleCPUPercent, /proc/stat farkı için ~1sn bloklar — bu yüzden
	// istek işleyicide değil, örnekleyici goroutine'inde koşar.
	cpu := sampleCPUPercent()
	memUsed, memTotal := readMemory()
	diskUsed, diskTotal := readDisk("/")
	load1 := 0.0
	if l := readLoadAvg(); len(l) > 0 {
		load1 = l[0]
	}

	db := p.db.GetDB()
	if _, err := db.Exec(`
		INSERT INTO metrics_samples (ts, cpu, mem_used, mem_total, disk_used, disk_total, load1)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		time.Now().UTC().Format(time.RFC3339), cpu,
		int64(memUsed), int64(memTotal), int64(diskUsed), int64(diskTotal), load1); err != nil {
		log.Printf("metrics sample: %v", err)
		return
	}
	if _, err := db.Exec(`DELETE FROM metrics_samples WHERE ts < datetime('now', ?)`, metricsRetention); err != nil {
		log.Printf("metrics prune: %v", err)
	}
}

type metricsSample struct {
	TS        string  `json:"ts"`
	CPU       float64 `json:"cpu"`
	MemUsed   int64   `json:"mem_used"`
	MemTotal  int64   `json:"mem_total"`
	DiskUsed  int64   `json:"disk_used"`
	DiskTotal int64   `json:"disk_total"`
	Load1     float64 `json:"load1"`
}

// handleMetricsHistory: GET /api/v1/metrics/history?hours=24 (1..48, admin —
// enforced by the isAdminOnlyPath prefix, like every other server-wide read).
// handleMetricsHistory: GET /api/v1/metrics/history?hours=24 (1..48, admin —
// diğer tüm sunucu-geneli okumalar gibi isAdminOnlyPath önekiyle uygulanır).
func (p *Panel) handleMetricsHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	hours := 24
	if h, err := strconv.Atoi(r.URL.Query().Get("hours")); err == nil && h >= 1 && h <= 48 {
		hours = h
	}

	rows, err := p.db.GetDB().QueryContext(r.Context(), `
		SELECT ts, cpu, mem_used, mem_total, disk_used, disk_total, load1
		FROM metrics_samples
		WHERE ts >= datetime('now', ?)
		ORDER BY ts`,
		"-"+strconv.Itoa(hours)+" hours")
	if err != nil {
		writeServerError(w, err)
		return
	}
	defer rows.Close()

	samples := []metricsSample{}
	for rows.Next() {
		var s metricsSample
		if rows.Scan(&s.TS, &s.CPU, &s.MemUsed, &s.MemTotal, &s.DiskUsed, &s.DiskTotal, &s.Load1) == nil {
			samples = append(samples, s)
		}
	}
	json.NewEncoder(w).Encode(map[string]any{"hours": hours, "samples": samples})
}
