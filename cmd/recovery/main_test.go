package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

const requestID = "0123456789abcdef0123456789abcdef"

func knownStatus(phase string) recoveryobs.Status {
	reason, proof := "", "none"
	switch phase {
	case "accepted":
		reason = "operation_accepted"
	case "running":
		reason = "update_running"
	case "recovering":
		reason = "recovery_running"
	case "failed":
		reason = "update_failed"
	case "recovery_required":
		reason = "recovery_incomplete"
	case "succeeded":
		reason, proof = "update_verified", "update_verified"
	case "recovered":
		reason, proof = "rollback_verified", "rollback_verified"
	}
	return (recoveryobs.Record{RequestID: requestID, TargetCommit: strings.Repeat("a", 40),
		Phase: phase, TerminalProof: proof, Reason: reason, ObservedAt: "2026-09-14T12:00:00Z", PreviousFailure: "none"}).Status()
}

func TestStatusReadsExactRequestOnceWithoutInterpretingExitAsOperationSuccess(t *testing.T) {
	for _, phase := range []string{"accepted", "running", "recovering", "failed", "recovery_required", "succeeded", "recovered"} {
		t.Run(phase, func(t *testing.T) {
			var output, diagnostics bytes.Buffer
			calls := 0
			want := knownStatus(phase)
			rt := cliRuntime{func() int { return 0 }, func(id string) recoveryobs.Status {
				calls++
				if id != requestID {
					t.Fatalf("read different operation: %q", id)
				}
				return want
			}, &output, &diagnostics}
			code := run([]string{"status", "--json", "--request-id", requestID}, rt)
			var got recoveryobs.Status
			if err := json.Unmarshal(output.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if code != exitOK || calls != 1 || got != want || diagnostics.Len() != 0 {
				t.Fatalf("status query code=%d calls=%d got=%#v diagnostics=%q", code, calls, got, diagnostics.String())
			}
			if strings.Contains(output.String(), "target_commit") || strings.Contains(output.String(), "token") {
				t.Fatal("private producer information exposed")
			}
		})
	}
}

func TestUnavailableObservationIsNeitherRecordedFailureNorSuccess(t *testing.T) {
	wrongID := knownStatus("recovered")
	wrongID.RequestID = strings.Repeat("b", 32)
	wrongSchema := knownStatus("recovered")
	wrongSchema.Schema = "celikpanel-recovery-status/v2"
	unknown := unavailableStatus(requestID)
	unknown.Phase, unknown.TerminalProof, unknown.PreviousFailure = "recovered", "rollback_verified", "private-diagnostic"
	for _, observed := range []recoveryobs.Status{{}, wrongID, wrongSchema, unknown} {
		var output, diagnostics bytes.Buffer
		code := run([]string{"status", "--request-id", requestID, "--json"}, cliRuntime{
			func() int { return 0 }, func(string) recoveryobs.Status { return observed }, &output, &diagnostics})
		var got recoveryobs.Status
		if err := json.Unmarshal(output.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if code != exitUnavailable || got != unavailableStatus(requestID) || diagnostics.Len() != 0 {
			t.Fatalf("unknown result was interpreted: code=%d got=%#v", code, got)
		}
	}
	var output, diagnostics bytes.Buffer
	if code := run([]string{"status", "--request-id", requestID, "--json"}, cliRuntime{
		func() int { return 0 }, nil, &output, &diagnostics}); code != exitUnavailable {
		t.Fatalf("nil observer: %d", code)
	}
}

func TestOwnerAuthenticationPrecedesEveryRead(t *testing.T) {
	for _, args := range [][]string{{"status", "--request-id", requestID, "--json"}, {"version"}, {"status", "--request-id", requestID, "--lang", "tr"}} {
		var output, diagnostics bytes.Buffer
		reads := 0
		code := run(args, cliRuntime{func() int { return 1000 }, func(string) recoveryobs.Status {
			reads++
			return knownStatus("recovered")
		}, &output, &diagnostics})
		if code != exitNotOwner || reads != 0 || output.Len() != 0 || !strings.Contains(diagnostics.String(), "sudo") {
			t.Fatalf("non-owner received status: code=%d reads=%d out=%q err=%q", code, reads, output.String(), diagnostics.String())
		}
	}
}

func TestCommandTupleRejectsAmbiguousOrUnsupportedActions(t *testing.T) {
	for _, args := range [][]string{
		nil, {"rollback"}, {"update"}, {"start"}, {"status"}, {"status", "--request-id"},
		{"status", "--request-id", strings.ToUpper(requestID)}, {"status", "--request-id", "../active"},
		{"status", "--request-id", requestID + "\n"}, {"status", "--request-id", requestID, "extra"},
		{"status", "--request-id=" + requestID}, {"status", "--request-id", requestID, "--json=true"},
		{"status", "--request-id", requestID, "--json", "--json"},
		{"status", "--request-id", requestID, "--request-id", requestID},
		{"status", "--request-id", requestID, "--lang", "en", "--lang", "tr"},
		{"status", "--request-id", requestID, "--lang", "de"}, {"status", "--request-id", requestID, "--lang"},
		{"status", "--request-id", requestID, "--root", "/tmp/other"},
		{"version", "--request-id", requestID}, {"version", "status"}, {"version", "--unknown"},
	} {
		var output, diagnostics bytes.Buffer
		reads := 0
		code := run(args, cliRuntime{func() int { return 0 }, func(string) recoveryobs.Status {
			reads++
			return knownStatus("running")
		}, &output, &diagnostics})
		if code != exitUsage || reads != 0 || output.Len() != 0 || diagnostics.Len() == 0 {
			t.Fatalf("ambiguous tuple accepted %q: code=%d reads=%d out=%q", args, code, reads, output.String())
		}
	}
}

func TestVersionReportsIndependentProtocolWithoutReadingOperation(t *testing.T) {
	for _, args := range [][]string{{"version"}, {"version", "--lang", "tr", "--json"}} {
		var output, diagnostics bytes.Buffer
		code := run(args, cliRuntime{func() int { return 0 }, func(string) recoveryobs.Status {
			t.Fatal("version read an operation")
			return recoveryobs.Status{}
		}, &output, &diagnostics})
		if code != exitOK || diagnostics.Len() != 0 || !strings.Contains(output.String(), recoveryProtocol) ||
			!strings.Contains(output.String(), buildVersion) || !strings.Contains(output.String(), buildCommit) {
			t.Fatalf("missing build/protocol identity: code=%d out=%q", code, output.String())
		}
		if args[len(args)-1] == "--json" {
			var fields map[string]string
			if err := json.Unmarshal(output.Bytes(), &fields); err != nil || len(fields) != 3 || fields["protocol"] != recoveryProtocol {
				t.Fatalf("version wire shape: %v %v", fields, err)
			}
		}
	}
}

func TestHumanGuidancePreservesRecordedFailureAndLanguage(t *testing.T) {
	for _, lang := range []string{"en", "tr"} {
		for _, phase := range []string{"accepted", "running", "recovering", "failed", "recovery_required", "succeeded", "recovered", "unavailable"} {
			var output, diagnostics bytes.Buffer
			observed := knownStatus(phase)
			if phase == "recovered" {
				observed.PreviousFailure = "recovery_failed"
			}
			code := run([]string{"status", "--request-id", requestID, "--lang", lang}, cliRuntime{
				func() int { return 0 }, func(string) recoveryobs.Status { return observed }, &output, &diagnostics})
			wantCode := exitOK
			if phase == "unavailable" {
				wantCode = exitUnavailable
			}
			if code != wantCode || !strings.Contains(output.String(), requestID) || diagnostics.Len() != 0 {
				t.Fatalf("guidance code=%d phase=%s lang=%s: %s", code, phase, lang, output.String())
			}
			for _, want := range []string{translated(lang, "Request:", "İşlem:"), translated(lang, "current service health", "güncel hizmet sağlığı")} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("missing localized observation boundary %q: %q", want, output.String())
				}
			}
			if phase == "recovered" && !strings.Contains(output.String(), "recovery_failed") {
				t.Fatal("verified restoration hid the previous failure")
			}
		}
	}
}

type rejectedOutput struct{}

func (rejectedOutput) Write([]byte) (int, error) {
	return 0, errors.New("private path /secret/key and token must not be exposed")
}

func TestOutputErrorDoesNotLeakRawDiagnosticOrClaimSuccess(t *testing.T) {
	for _, args := range [][]string{{"version", "--json"}, {"status", "--request-id", requestID}, {"status", "--request-id", requestID, "--json"}} {
		var diagnostics bytes.Buffer
		code := run(args, cliRuntime{func() int { return 0 }, func(string) recoveryobs.Status { return knownStatus("recovered") }, rejectedOutput{}, &diagnostics})
		if code != exitOutput || strings.Contains(diagnostics.String(), "/secret") || strings.Contains(diagnostics.String(), "token") {
			t.Fatalf("output failure leaked or claimed success: %d %q", code, diagnostics.String())
		}
	}
}

func TestWaitingGuidancePreservesFailureWithoutReportingCompletion(t *testing.T) {
	for _, wait := range []string{"initializing", "starting", "stopping"} {
		for _, lang := range []string{"en", "tr"} {
			observed := knownStatus("recovering")
			observed.WaitingFor, observed.PreviousFailure = wait, "update_failed"
			var output, diagnostics bytes.Buffer
			code := run([]string{"status", "--request-id", requestID, "--lang", lang}, cliRuntime{
				func() int { return 0 }, func(string) recoveryobs.Status { return observed }, &output, &diagnostics})
			if code != exitOK || diagnostics.Len() != 0 || !strings.Contains(output.String(), "update_failed") || !strings.Contains(output.String(), "none") {
				t.Fatalf("wait lost observation: %d %q %q", code, output.String(), diagnostics.String())
			}
			if lang == "en" && (!strings.Contains(output.String(), "same operation") || !strings.Contains(output.String(), "not yet complete")) {
				t.Fatalf("wait guidance: %q", output.String())
			}
			output.Reset()
			code = run([]string{"status", "--request-id", requestID, "--json"}, cliRuntime{
				func() int { return 0 }, func(string) recoveryobs.Status { return observed }, &output, &diagnostics})
			var got recoveryobs.Status
			if json.Unmarshal(output.Bytes(), &got) != nil || code != exitOK || got != observed {
				t.Fatalf("wait JSON: %q", output.String())
			}
		}
	}
}
