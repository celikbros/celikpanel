package main

import (
	"strings"
	"testing"
)

func TestDomainLogsViewerExposesHonestTimeFilterControlsAndMetadata(t *testing.T) {
	viewer := frontendSourceFile(t, "web", "src", "components", "DomainLogsViewer.tsx")
	for _, required := range []string{
		`type="datetime-local"`,
		`buildLogTimeRangeQuery(requestedStartLocal, requestedEndLocal)`,
		`t('logs.timeApply')`,
		`void loadLogs('', '')`,
		`params.set('start_time', timeRange.startTime)`,
		`params.set('end_time', timeRange.endTime)`,
		`parseDomainLogsResponse(await res.json())`,
		`role="alert"`,
		`aria-live="polite"`,
		`timeFilter.exact`,
		`timeFilter.parsed_lines`,
		`timeFilter.unparsed_lines`,
		`timeFilter.assumed_timezone`,
		`result.truncated`,
		`result.warning`,
	} {
		if !strings.Contains(viewer, required) {
			t.Fatalf("domain log viewer is missing %q", required)
		}
	}

	helper := frontendSourceFile(t, "web", "src", "lib", "domainLogs.ts")
	for _, required := range []string{
		`localDateTimeToRFC3339`,
		`local.setFullYear(year, month, day)`,
		`local.getFullYear() !== year`,
		`return local.toISOString()`,
		`Date.parse(startTime) > Date.parse(endTime)`,
		`value.lines.every((line) => typeof line === 'string')`,
	} {
		if !strings.Contains(helper, required) {
			t.Fatalf("domain log frontend helper is missing %q", required)
		}
	}
}
