package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// SystemStats is a point-in-time view of server health for the dashboard.
// The panel reads it straight from /proc and syscalls as the unprivileged
// user — no root, no agent round-trip.
//
// SystemStats, pano için sunucu sağlığının anlık görünümüdür. Panel bunu
// düşük yetkili kullanıcı olarak doğrudan /proc ve sistem çağrılarından
// okur — root yok, agent'a gidiş yok.
type SystemStats struct {
	Hostname       string    `json:"hostname"`
	OS             string    `json:"os"`
	UptimeSeconds  int64     `json:"uptime_seconds"`
	CPUPercent     float64   `json:"cpu_percent"`
	CPUCores       int       `json:"cpu_cores"`
	LoadAvg        []float64 `json:"load_avg"`
	MemUsedBytes   uint64    `json:"mem_used_bytes"`
	MemTotalBytes  uint64    `json:"mem_total_bytes"`
	DiskUsedBytes  uint64    `json:"disk_used_bytes"`
	DiskTotalBytes uint64    `json:"disk_total_bytes"`
}

func (p *Panel) handleSystemStats(w http.ResponseWriter, r *http.Request) {
	stats := SystemStats{
		Hostname: hostnameOrEmpty(),
		OS:       prettyOSName(),
		CPUCores: numCPU(),
	}
	stats.UptimeSeconds = readUptime()
	stats.LoadAvg = readLoadAvg()
	stats.CPUPercent = sampleCPUPercent()
	stats.MemUsedBytes, stats.MemTotalBytes = readMemory()
	stats.DiskUsedBytes, stats.DiskTotalBytes = readDisk("/")

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(stats)
}

func hostnameOrEmpty() string {
	h, err := os.Hostname()
	if err != nil {
		return ""
	}
	return h
}

// prettyOSName reads PRETTY_NAME from /etc/os-release for a human label.
// prettyOSName, insan-okur bir etiket için /etc/os-release'ten
// PRETTY_NAME'i okur.
func prettyOSName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return ""
}

func numCPU() int {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "processor\t")
}

func readUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	seconds, _ := strconv.ParseFloat(fields[0], 64)
	return int64(seconds)
}

func readLoadAvg() []float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return []float64{0, 0, 0}
	}
	fields := strings.Fields(string(data))
	load := make([]float64, 0, 3)
	for i := 0; i < 3 && i < len(fields); i++ {
		v, _ := strconv.ParseFloat(fields[i], 64)
		load = append(load, v)
	}
	return load
}

// readMemory returns used and total RAM. "Used" excludes reclaimable
// caches by using MemAvailable, matching what tools like `free` report.
// readMemory, kullanılan ve toplam RAM'i döndürür. "Kullanılan",
// MemAvailable üzerinden geri alınabilir önbellekleri hariç tutar; bu
// `free` gibi araçların bildirdiğiyle örtüşür.
func readMemory() (used, total uint64) {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	defer f.Close()

	var memTotal, memAvailable uint64
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		// Values are in kB.
		value, _ := strconv.ParseUint(fields[1], 10, 64)
		switch fields[0] {
		case "MemTotal:":
			memTotal = value * 1024
		case "MemAvailable:":
			memAvailable = value * 1024
		}
	}
	if memAvailable > memTotal {
		memAvailable = memTotal
	}
	return memTotal - memAvailable, memTotal
}

func readDisk(path string) (used, total uint64) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return 0, 0
	}
	bs := uint64(st.Bsize)
	total = st.Blocks * bs
	free := st.Bavail * bs
	if free > total {
		free = total
	}
	return total - free, total
}

// sampleCPUPercent measures busy CPU over a short window by diffing two
// /proc/stat reads. A single request can afford ~200ms for an accurate
// instantaneous figure.
// sampleCPUPercent, iki /proc/stat okumasının farkını alarak kısa bir
// pencerede meşgul CPU'yu ölçer. Tek bir istek, doğru bir anlık değer için
// ~200ms'yi karşılayabilir.
func sampleCPUPercent() float64 {
	idle1, total1, ok1 := readCPUTimes()
	if !ok1 {
		return 0
	}
	time.Sleep(200 * time.Millisecond)
	idle2, total2, ok2 := readCPUTimes()
	if !ok2 || total2 == total1 {
		return 0
	}
	idleDelta := float64(idle2 - idle1)
	totalDelta := float64(total2 - total1)
	percent := (1 - idleDelta/totalDelta) * 100
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}
	// One decimal is enough for a dashboard gauge.
	// Bir pano göstergesi için bir ondalık yeterlidir.
	return float64(int(percent*10)) / 10
}

func readCPUTimes() (idle, total uint64, ok bool) {
	f, err := os.Open("/proc/stat")
	if err != nil {
		return 0, 0, false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	if !scanner.Scan() {
		return 0, 0, false
	}
	fields := strings.Fields(scanner.Text())
	if len(fields) < 5 || fields[0] != "cpu" {
		return 0, 0, false
	}
	for i := 1; i < len(fields); i++ {
		v, err := strconv.ParseUint(fields[i], 10, 64)
		if err != nil {
			continue
		}
		total += v
		// Field 4 is idle, field 5 is iowait (both count as not-busy).
		// Alan 4 idle, alan 5 iowait (ikisi de meşgul-değil sayılır).
		if i == 4 || i == 5 {
			idle += v
		}
	}
	return idle, total, true
}
