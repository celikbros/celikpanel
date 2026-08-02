package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestParseSupportedLogTimestamp(t *testing.T) {
	tests := []struct {
		name       string
		line       string
		wantFormat string
		want       time.Time
	}{
		{
			name:       "nginx access",
			line:       `203.0.113.7 - - [02/Aug/2026:12:34:56 +0300] "GET / HTTP/1.1" 200 12`,
			wantFormat: "nginx-access",
			want:       time.Date(2026, time.August, 2, 9, 34, 56, 0, time.UTC),
		},
		{
			name:       "nginx error uses server local timezone",
			line:       `2026/08/02 12:34:56 [error] 1#1: failure`,
			wantFormat: "nginx-error",
			want:       time.Date(2026, time.August, 2, 12, 34, 56, 0, time.Local),
		},
		{
			name:       "php UTC",
			line:       `[02-Aug-2026 12:34:56 UTC] PHP Warning: failure`,
			wantFormat: "php",
			want:       time.Date(2026, time.August, 2, 12, 34, 56, 0, time.UTC),
		},
		{
			name:       "php numeric offset",
			line:       `[02-Aug-2026 12:34:56 +03:00] PHP Warning: failure`,
			wantFormat: "php",
			want:       time.Date(2026, time.August, 2, 9, 34, 56, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, format, ok := parseSupportedLogTimestamp(test.line)
			if !ok {
				t.Fatalf("timestamp was not recognized: %q", test.line)
			}
			if format != test.wantFormat {
				t.Fatalf("format = %q, want %q", format, test.wantFormat)
			}
			if !got.Equal(test.want) {
				t.Fatalf("timestamp = %s, want %s", got, test.want)
			}
		})
	}
}

func TestParseSupportedLogTimestampRejectsUnknownOrAmbiguousFormats(t *testing.T) {
	tests := []string{
		`plain application message without a timestamp`,
		`[02-Aug-2026 12:34:56 EST] ambiguous timezone abbreviation`,
		`02-Aug-2026 12:34:56 missing PHP brackets`,
	}
	for _, line := range tests {
		if _, _, ok := parseSupportedLogTimestamp(line); ok {
			t.Fatalf("unexpectedly accepted %q", line)
		}
	}
}

func TestParseRequestedLogTimeRangeRequiresRFC3339AndOrderedBounds(t *testing.T) {
	if _, err := parseRequestedLogTimeRange("2026-08-02 12:00:00", ""); err == nil {
		t.Fatal("timezone-free start_time was accepted")
	}
	if _, err := parseRequestedLogTimeRange("2026-08-02T13:00:00Z", "2026-08-02T12:00:00Z"); err == nil {
		t.Fatal("reversed time range was accepted")
	}

	rangeValue, err := parseRequestedLogTimeRange("2026-08-02T12:00:00Z", "2026-08-02T13:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if !rangeValue.contains(time.Date(2026, time.August, 2, 12, 0, 0, 0, time.UTC)) ||
		!rangeValue.contains(time.Date(2026, time.August, 2, 13, 0, 0, 0, time.UTC)) {
		t.Fatal("time range boundaries must be inclusive")
	}
}

func TestReadBoundedLogFiltersSupportedTimestampsAndReportsUnknownLines(t *testing.T) {
	content := strings.Join([]string{
		`x [02/Aug/2026:11:59:59 +0000] before`,
		`x [02/Aug/2026:12:10:00 +0000] access match`,
		`[02-Aug-2026 12:20:00 UTC] PHP Warning: php match`,
		`application continuation without a timestamp`,
		`x [02/Aug/2026:12:31:00 +0000] after`,
	}, "\n") + "\n"
	file := writeTempLog(t, content)

	rangeValue, err := parseRequestedLogTimeRange("2026-08-02T12:00:00Z", "2026-08-02T12:30:00Z")
	if err != nil {
		t.Fatal(err)
	}
	result, err := readBoundedLog(file, int64(len(content)), "", 100, rangeValue)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := result.lines, []string{
		`x [02/Aug/2026:12:10:00 +0000] access match`,
		`[02-Aug-2026 12:20:00 UTC] PHP Warning: php match`,
	}; strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("lines = %#v, want %#v", got, want)
	}
	if result.parsedLines != 4 || result.unparsedLines != 1 {
		t.Fatalf("parsed/unparsed = %d/%d, want 4/1", result.parsedLines, result.unparsedLines)
	}
	if warning := logReadWarning(result, 100, true); !strings.Contains(warning, "1 line(s)") {
		t.Fatalf("warning %q does not disclose the omitted line", warning)
	}
}

func TestReadBoundedLogReturnsNewestMatchingLinesWithinLimit(t *testing.T) {
	content := "one\ntwo\nthree\nfour\n"
	file := writeTempLog(t, content)
	result, err := readBoundedLog(file, int64(len(content)), "", 2, logTimeRange{})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(result.lines, ","); got != "three,four" {
		t.Fatalf("lines = %q, want newest two", got)
	}
	if !result.resultTruncated || result.matched != 4 {
		t.Fatalf("truncation/matches = %v/%d, want true/4", result.resultTruncated, result.matched)
	}
}

func TestLogReadBoundsAndPathBoundary(t *testing.T) {
	if offset, truncated := boundedLogScanOffset(maxLogScanBytes + 99); offset != 99 || !truncated {
		t.Fatalf("offset/truncated = %d/%v, want 99/true", offset, truncated)
	}
	if _, err := requestedLogLineLimit(maxLogLineLimit + 1); err == nil {
		t.Fatal("oversized line request was accepted")
	}
	if limit, err := requestedLogLineLimit(0); err != nil || limit != defaultLogLineLimit {
		t.Fatalf("default limit = %d, %v", limit, err)
	}

	roots := []string{"/var/log/nginx", "/var/log/php"}
	if _, err := allowedLogPath("/var/log/nginxevil/access.log", roots); err == nil {
		t.Fatal("prefix-confused path escaped the allowed root")
	}
	if _, err := allowedLogPath("/var/log/nginx/../secret.log", roots); err == nil {
		t.Fatal("parent traversal escaped the allowed root")
	}
	if _, err := allowedLogPath("/var/log/nginx/archive/example-access.log", roots); err == nil {
		t.Fatal("nested log path bypassed the trusted-root boundary")
	}
	if got, err := allowedLogPath("/var/log/nginx/example-access.log", roots); err != nil || got != "/var/log/nginx/example-access.log" {
		t.Fatalf("valid path = %q, %v", got, err)
	}
}

func writeTempLog(t *testing.T, content string) *os.File {
	t.Helper()
	path := t.TempDir() + "/test.log"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	return file
}
