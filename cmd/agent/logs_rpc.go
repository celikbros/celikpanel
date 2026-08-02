package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	defaultLogLineLimit = 100
	maxLogLineLimit     = 5000
	maxLogFilterBytes   = 1024
	maxLogScanBytes     = 64 << 20
	maxLogLineBytes     = 1 << 20
)

var (
	allowedLogDirectories = []string{"/var/log/nginx", "/var/log/php"}
	nginxAccessTimeRE     = regexp.MustCompile(`\[(\d{2}/[A-Za-z]{3}/\d{4}:\d{2}:\d{2}:\d{2} [+-]\d{4})\]`)
	nginxErrorTimeRE      = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2})(?:\s|$)`)
	phpTimeRE             = regexp.MustCompile(`^\[(\d{2}-[A-Za-z]{3}-\d{4} \d{2}:\d{2}:\d{2})(?: ([A-Za-z0-9_+./:-]{1,64}))?\](?:\s|$)`)
)

type logTimeRange struct {
	start *time.Time
	end   *time.Time
}

type boundedLogRead struct {
	lines           []string
	matched         int
	scanTruncated   bool
	resultTruncated bool
	parsedLines     int
	unparsedLines   int
}

// Log Viewer RPC Methods

// GetLogsRequest represents a request to get logs
type GetLogsRequest = transport.GetLogsRequest

// GetLogsResponse represents log data
type GetLogsResponse = transport.GetLogsResponse

// ClearLogsRequest represents a request to clear logs
type ClearLogsRequest = transport.ClearLogsRequest

// ClearLogsResponse represents the response from clearing logs
type ClearLogsResponse = transport.ClearLogsResponse

// GetAccessLogs retrieves nginx access logs for a domain
func (a *Agent) GetAccessLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	return a.getLogs(req, resp)
}

// GetErrorLogs retrieves nginx error logs for a domain
func (a *Agent) GetErrorLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	return a.getLogs(req, resp)
}

// GetPHPLogs retrieves PHP error logs for a domain
func (a *Agent) GetPHPLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	return a.getLogs(req, resp)
}

// getLogs is a generic log retrieval function
func (a *Agent) getLogs(req GetLogsRequest, resp *GetLogsResponse) error {
	if resp == nil {
		return fmt.Errorf("log response is required")
	}
	*resp = GetLogsResponse{}

	// Validate log path
	if req.LogPath == "" {
		resp.Success = false
		resp.Error = "log path is required"
		return nil
	}

	limit, err := requestedLogLineLimit(req.Lines)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}
	if len(req.Filter) > maxLogFilterBytes {
		resp.Success = false
		resp.Error = fmt.Sprintf("filter exceeds the %d-byte limit", maxLogFilterBytes)
		return nil
	}

	timeRange, err := parseRequestedLogTimeRange(req.StartTime, req.EndTime)
	if err != nil {
		resp.Success = false
		resp.Error = err.Error()
		return nil
	}

	file, err := openAllowedLogFile(req.LogPath, allowedLogDirectories, false)
	if os.IsNotExist(err) {
		resp.Success = false
		resp.Error = "log file not found"
		return nil
	}
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to securely open log file: %v", err)
		return nil
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to inspect open log file: %v", err)
		return nil
	}
	if !info.Mode().IsRegular() {
		resp.Success = false
		resp.Error = "log path must refer to a regular file"
		return nil
	}

	read, err := readBoundedLog(file, info.Size(), req.Filter, limit, timeRange)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("error reading log file: %v", err)
		return nil
	}

	resp.Success = true
	resp.Lines = read.lines
	resp.Total = len(read.lines)
	resp.Truncated = read.scanTruncated || read.resultTruncated
	resp.Warning = logReadWarning(read, limit, timeRange.active())
	if timeRange.active() {
		resp.TimeFilter = &transport.LogTimeFilterResult{
			Applied:         true,
			Exact:           !read.scanTruncated && !read.resultTruncated && read.unparsedLines == 0,
			StartTime:       req.StartTime,
			EndTime:         req.EndTime,
			ParsedLines:     read.parsedLines,
			UnparsedLines:   read.unparsedLines,
			AssumedTimezone: time.Local.String(),
			Warning:         resp.Warning,
		}
	}
	return nil
}

// ClearLogs truncates a log file
func (a *Agent) ClearLogs(req ClearLogsRequest, resp *ClearLogsResponse) error {
	if resp == nil {
		return fmt.Errorf("clear log response is required")
	}
	*resp = ClearLogsResponse{}

	// Validate log path
	if req.LogPath == "" {
		resp.Success = false
		resp.Error = "log path is required"
		return nil
	}

	file, err := openAllowedLogFile(req.LogPath, allowedLogDirectories, true)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to securely open log file: %v", err)
		return nil
	}
	defer file.Close()

	// Truncate the exact regular file descriptor that was opened and validated.
	// Never resolve the pathname again: doing so would reintroduce a TOCTOU race.
	err = file.Truncate(0)
	if err != nil {
		resp.Success = false
		resp.Error = fmt.Sprintf("failed to clear log file: %v", err)
		return nil
	}

	resp.Success = true
	return nil
}

// GetDomainLogPaths returns the log file paths for a domain
func (a *Agent) GetDomainLogPaths(domain string, resp *transport.DomainLogPathsResponse) error {
	// Standard nginx log paths
	resp.AccessLog = filepath.Join("/var/log/nginx", domain+"-access.log")
	resp.ErrorLog = filepath.Join("/var/log/nginx", domain+"-error.log")
	resp.PHPLog = filepath.Join("/var/log/php", domain+"-error.log")

	return nil
}

func requestedLogLineLimit(requested int) (int, error) {
	if requested < 0 {
		return 0, fmt.Errorf("lines must be between 1 and %d", maxLogLineLimit)
	}
	if requested == 0 {
		return defaultLogLineLimit, nil
	}
	if requested > maxLogLineLimit {
		return 0, fmt.Errorf("lines must be between 1 and %d", maxLogLineLimit)
	}
	return requested, nil
}

func allowedLogPath(raw string, roots []string) (string, error) {
	cleaned, _, _, err := allowedLogTarget(raw, roots)
	return cleaned, err
}

func allowedLogTarget(raw string, roots []string) (cleaned, root, relative string, err error) {
	if !filepath.IsAbs(raw) {
		return "", "", "", fmt.Errorf("access denied: log path must be absolute")
	}

	cleaned = filepath.Clean(raw)
	for _, candidateRoot := range roots {
		cleanRoot := filepath.Clean(candidateRoot)
		candidateRelative, relErr := filepath.Rel(cleanRoot, cleaned)
		if relErr != nil {
			continue
		}
		if candidateRelative == "." || candidateRelative == ".." || strings.HasPrefix(candidateRelative, ".."+string(filepath.Separator)) {
			continue
		}
		// CelikPanel creates domain logs as direct children of these trusted
		// directories. Reject nested paths so an attacker-controlled subdirectory
		// cannot weaken the ownership and permission guarantees of the root.
		if filepath.Dir(candidateRelative) != "." {
			continue
		}
		return cleaned, cleanRoot, candidateRelative, nil
	}

	return "", "", "", fmt.Errorf("access denied: log path not in allowed directories")
}

func openAllowedLogFile(raw string, roots []string, write bool) (*os.File, error) {
	_, root, relative, err := allowedLogTarget(raw, roots)
	if err != nil {
		return nil, err
	}
	return secureOpenLogFile(root, relative, write)
}

func parseRequestedLogTimeRange(startRaw, endRaw string) (logTimeRange, error) {
	var result logTimeRange
	parse := func(field, raw string) (*time.Time, error) {
		if raw == "" {
			return nil, nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return nil, fmt.Errorf("%s must be RFC3339 with an explicit timezone", field)
		}
		return &parsed, nil
	}

	var err error
	if result.start, err = parse("start_time", startRaw); err != nil {
		return logTimeRange{}, err
	}
	if result.end, err = parse("end_time", endRaw); err != nil {
		return logTimeRange{}, err
	}
	if result.start != nil && result.end != nil && result.start.After(*result.end) {
		return logTimeRange{}, fmt.Errorf("start_time must not be after end_time")
	}
	return result, nil
}

func (r logTimeRange) active() bool {
	return r.start != nil || r.end != nil
}

func (r logTimeRange) contains(timestamp time.Time) bool {
	return (r.start == nil || !timestamp.Before(*r.start)) &&
		(r.end == nil || !timestamp.After(*r.end))
}

func boundedLogScanOffset(size int64) (int64, bool) {
	if size <= maxLogScanBytes {
		return 0, false
	}
	return size - maxLogScanBytes, true
}

func readBoundedLog(file *os.File, size int64, filter string, limit int, timeRange logTimeRange) (boundedLogRead, error) {
	result := boundedLogRead{lines: make([]string, 0, limit)}
	offset, truncated := boundedLogScanOffset(size)
	result.scanTruncated = truncated
	if _, err := file.Seek(offset, 0); err != nil {
		return boundedLogRead{}, err
	}

	// LimitReader keeps the operation bounded even if the active log continues
	// to grow while it is being read.
	scanner := bufio.NewScanner(io.LimitReader(file, maxLogScanBytes))
	scanner.Buffer(make([]byte, 64<<10), maxLogLineBytes)
	if offset > 0 {
		// The bounded window usually starts in the middle of a line. Discard that
		// fragment instead of presenting it as a complete log entry.
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				return boundedLogRead{}, err
			}
			return result, nil
		}
	}

	lowerFilter := strings.ToLower(filter)
	ringStart := 0
	for scanner.Scan() {
		line := scanner.Text()
		if lowerFilter != "" && !strings.Contains(strings.ToLower(line), lowerFilter) {
			continue
		}

		if timeRange.active() {
			timestamp, _, ok := parseSupportedLogTimestamp(line)
			if !ok {
				result.unparsedLines++
				continue
			}
			result.parsedLines++
			if !timeRange.contains(timestamp) {
				continue
			}
		}

		result.matched++
		if len(result.lines) < limit {
			result.lines = append(result.lines, line)
			continue
		}
		result.resultTruncated = true
		result.lines[ringStart] = line
		ringStart = (ringStart + 1) % limit
	}
	if err := scanner.Err(); err != nil {
		return boundedLogRead{}, err
	}
	currentOffset, err := file.Seek(0, 1)
	if err != nil {
		return boundedLogRead{}, err
	}
	currentInfo, err := file.Stat()
	if err != nil {
		return boundedLogRead{}, err
	}
	if currentInfo.Size() > currentOffset {
		result.scanTruncated = true
	}

	if result.resultTruncated && ringStart > 0 {
		ordered := make([]string, 0, len(result.lines))
		ordered = append(ordered, result.lines[ringStart:]...)
		ordered = append(ordered, result.lines[:ringStart]...)
		result.lines = ordered
	}
	return result, nil
}

func logReadWarning(read boundedLogRead, limit int, timeFilter bool) string {
	warnings := make([]string, 0, 3)
	if read.scanTruncated {
		warnings = append(warnings, fmt.Sprintf("only the newest %d MiB of the log file was scanned", maxLogScanBytes>>20))
	}
	if read.resultTruncated {
		warnings = append(warnings, fmt.Sprintf("only the newest %d matching lines were returned", limit))
	}
	if timeFilter && read.unparsedLines > 0 {
		warnings = append(warnings, fmt.Sprintf("%d line(s) with missing or unsupported timestamps were omitted", read.unparsedLines))
	}
	return strings.Join(warnings, "; ")
}

func parseSupportedLogTimestamp(line string) (time.Time, string, bool) {
	if match := nginxErrorTimeRE.FindStringSubmatch(line); match != nil {
		parsed, err := time.ParseInLocation("2006/01/02 15:04:05", match[1], time.Local)
		return parsed, "nginx-error", err == nil
	}
	if match := phpTimeRE.FindStringSubmatch(line); match != nil {
		parsed, err := parsePHPLogTimestamp(match[1], match[2])
		return parsed, "php", err == nil
	}
	if match := nginxAccessTimeRE.FindStringSubmatch(line); match != nil {
		parsed, err := parseNginxTimestamp(match[1])
		return parsed, "nginx-access", err == nil
	}
	return time.Time{}, "", false
}

func parsePHPLogTimestamp(timestamp, zone string) (time.Time, error) {
	const layout = "02-Jan-2006 15:04:05"
	if zone == "" {
		return time.ParseInLocation(layout, timestamp, time.Local)
	}
	switch zone {
	case "UTC", "GMT", "Z":
		return time.ParseInLocation(layout, timestamp, time.UTC)
	}
	if matched, _ := regexp.MatchString(`^[+-]\d{4}$`, zone); matched {
		return time.Parse(layout+" -0700", timestamp+" "+zone)
	}
	if matched, _ := regexp.MatchString(`^[+-]\d{2}:\d{2}$`, zone); matched {
		return time.Parse(layout+" -07:00", timestamp+" "+zone)
	}
	if !strings.Contains(zone, "/") {
		return time.Time{}, fmt.Errorf("unsupported PHP log timezone %q", zone)
	}
	location, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	return time.ParseInLocation(layout, timestamp, location)
}

// parseNginxTimestamp parses nginx access-log timestamps, including their
// mandatory numeric offset. Error-log timestamps are parsed separately in the
// server's local timezone because nginx does not include an offset there.
func parseNginxTimestamp(timestamp string) (time.Time, error) {
	// nginx format: [01/Jan/2024:12:00:00 +0000]
	layout := "02/Jan/2006:15:04:05 -0700"
	timestamp = strings.Trim(timestamp, "[]")
	return time.Parse(layout, timestamp)
}
