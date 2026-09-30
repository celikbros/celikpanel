package main

import (
	"bytes"
	"regexp"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

var internalToken = regexp.MustCompile(`[a-z]+_[a-z_]+|\bproducer\b|Üretici|Terminal proof|Nihai kanıt|\bnone\b`)

func ownerStatusText(t *testing.T, observed recoveryobs.Status, lang string) []string {
	t.Helper()
	var output, diagnostics bytes.Buffer
	run([]string{"status", "--request-id", requestID, "--lang", lang}, cliRuntime{
		func() int { return 0 }, func(string) recoveryobs.Status { return observed }, &output, &diagnostics})
	if diagnostics.Len() != 0 {
		t.Fatalf("diagnostics: %q", diagnostics.String())
	}
	return strings.Split(strings.TrimSuffix(output.String(), "\n"), "\n")
}

// Every state opens with plain owner guidance. Internal tokens appear only on
// the final "for support" line, in both languages.
func TestOwnerTextSpeaksPlainlyAndKeepsTokensOnTheSupportLine(t *testing.T) {
	variants := map[string]recoveryobs.Status{"unavailable": unavailableStatus(requestID)}
	for _, phase := range []string{"accepted", "running", "recovering", "failed", "recovery_required", "succeeded", "recovered"} {
		variants[phase] = knownStatus(phase)
	}
	wait := knownStatus("recovering")
	wait.WaitingFor, wait.PreviousFailure = "starting", "update_failed"
	variants["waiting"] = wait
	paused := knownStatus("recovery_required")
	paused.AutomaticRecovery, paused.PreviousFailure = "paused_retry_limit", "update_failed"
	variants["paused"] = paused
	for _, code := range []string{"candidate_panel_startup_check_failed", "panel_start_unverified"} {
		for _, phase := range []string{"failed", "recovering", "recovered"} {
			s := knownStatus(phase)
			s.PreviousFailure, s.FailureCode = "update_failed", code
			variants[phase+"/"+code] = s
		}
	}
	support := map[string]string{"en": "Recorded state (for support): ", "tr": "Kayıtlı durum (destek için): "}
	for name, observed := range variants {
		for _, lang := range []string{"en", "tr"} {
			lines := ownerStatusText(t, observed, lang)
			last := lines[len(lines)-1]
			if !strings.HasPrefix(last, support[lang]) {
				t.Fatalf("%s/%s: final line is not the support line: %q", name, lang, lines)
			}
			wantState := "phase=" + observed.Phase
			if observed.Observation != "known" {
				wantState = "observation=unavailable"
			}
			if !strings.Contains(last, wantState+" reason="+observed.Reason+" proof="+observed.TerminalProof) {
				t.Fatalf("%s/%s: support line %q", name, lang, last)
			}
			for _, line := range lines[:len(lines)-1] {
				if strings.HasPrefix(line, translated(lang, "Request: ", "İşlem: ")) ||
					strings.HasPrefix(line, translated(lang, "Recorded at: ", "Kayıt zamanı: ")) {
					continue
				}
				// Commands shown to the owner (journalctl units) are allowed.
				plain := strings.NewReplacer("celikpanel-release-recovery.service", "", "celikpanel-panel", "").Replace(line)
				if token := internalToken.FindString(plain); token != "" {
					t.Fatalf("%s/%s: internal token %q before the support line: %q", name, lang, token, line)
				}
			}
			if lang == "tr" && strings.Contains(strings.Join(lines[:len(lines)-1], " "), "The ") {
				t.Fatalf("%s: English text in Turkish output: %q", name, lines)
			}
		}
	}
	lines := ownerStatusText(t, variants["recovered/candidate_panel_startup_check_failed"], "en")
	if lines[len(lines)-1] != "Recorded state (for support): phase=recovered reason=rollback_verified proof=rollback_verified previous_failure=update_failed failure_code=candidate_panel_startup_check_failed" {
		t.Fatalf("support line: %q", lines[len(lines)-1])
	}
	if !strings.Contains(lines[0], "returned to the previous version automatically") || !strings.Contains(lines[0], "Do not start the same version again") {
		t.Fatalf("recovered guidance: %q", lines[0])
	}
	generic := ownerStatusText(t, knownStatus("recovered"), "tr")
	if !strings.Contains(generic[0], "otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü") || strings.Contains(generic[0], "Üretici") {
		t.Fatalf("generic recovered TR: %q", generic[0])
	}
}

// The machine-readable output is the recoveryobs.Status wire shape, byte for
// byte; the human wording change must not rename or reorder anything.
func TestStatusJSONBytesAreUnchanged(t *testing.T) {
	observed := knownStatus("recovered")
	observed.PreviousFailure, observed.FailureCode = "update_failed", "candidate_panel_startup_check_failed"
	wait := knownStatus("recovering")
	wait.WaitingFor, wait.PreviousFailure = "starting", "update_failed"
	paused := knownStatus("recovery_required")
	paused.AutomaticRecovery, paused.PreviousFailure = "paused_retry_limit", "update_failed"
	for _, test := range []struct {
		observed recoveryobs.Status
		want     string
	}{
		{observed, `{"schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"known","phase":"recovered","terminal_proof":"rollback_verified","reason":"rollback_verified","observed_at":"2026-09-14T12:00:00Z","previous_failure":"update_failed","failure_code":"candidate_panel_startup_check_failed"}` + "\n"},
		{wait, `{"schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"known","phase":"recovering","terminal_proof":"none","reason":"recovery_running","observed_at":"2026-09-14T12:00:00Z","previous_failure":"update_failed","waiting_for":"starting"}` + "\n"},
		{paused, `{"automatic_recovery":"paused_retry_limit","schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"known","phase":"recovery_required","terminal_proof":"none","reason":"recovery_incomplete","observed_at":"2026-09-14T12:00:00Z","previous_failure":"update_failed"}` + "\n"},
		{recoveryobs.Status{}, `{"schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"unavailable","terminal_proof":"none","reason":"observation_unavailable"}` + "\n"},
	} {
		for _, lang := range [][]string{nil, {"--lang", "tr"}} {
			var output, diagnostics bytes.Buffer
			args := append([]string{"status", "--request-id", requestID, "--json"}, lang...)
			run(args, cliRuntime{func() int { return 0 }, func(string) recoveryobs.Status { return test.observed }, &output, &diagnostics})
			if output.String() != test.want {
				t.Fatalf("JSON bytes changed:\n got %s\nwant %s", output.String(), test.want)
			}
		}
	}
}

// A paused recovery keeps its pause guidance; a typed first cause adds, before
// it, what is wrong and where to look. Without a cause the pause text is alone.
func TestPausedGuidanceNamesTheFirstTypedCauseBeforeThePause(t *testing.T) {
	paused := func(first string) recoveryobs.Status {
		s := knownStatus("recovery_required")
		s.AutomaticRecovery, s.PreviousFailure, s.FirstFailureCode = "paused_retry_limit", "recovery_failed", first
		return s
	}
	pauseEN := "Automatic recovery used all three attempts without finishing"
	pauseTR := "Otomatik kurtarma üç denemenin hepsini kullandı"
	for _, test := range []struct {
		first, en, tr string
	}{
		{"panel_start_unverified",
			"The update was applied, but the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no supported return to the previous version from this point. " + pauseEN,
			"Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen bir dönüş yok. " + pauseTR},
		{"candidate_panel_startup_check_failed",
			"The new version's panel failed its start check before anything was switched on, and the automatic return to the previous version did not finish. The recovery journal shows what stopped it; retrying continues the same return to the previous version. " + pauseEN,
			"Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi ve önceki sürüme otomatik dönüş tamamlanmadı. Kurtarma günlüğü neyin durdurduğunu gösterir; yeniden deneme önceki sürüme aynı dönüşü sürdürür. " + pauseTR},
		{"", pauseEN, pauseTR},
		{"private_diagnostic", pauseEN, pauseTR},
	} {
		en := ownerStatusText(t, paused(test.first), "en")
		tr := ownerStatusText(t, paused(test.first), "tr")
		if !strings.HasPrefix(en[0], test.en) || !strings.HasPrefix(tr[0], test.tr) {
			t.Fatalf("%q paused guidance:\n%s\n%s", test.first, en[0], tr[0])
		}
		for _, line := range []string{en[0], tr[0]} {
			if !strings.Contains(line, "sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50") ||
				!strings.Contains(line, translated(map[bool]string{true: "tr", false: "en"}[line == tr[0]], "same-operation retry command", "tek seferlik yeniden deneme komutunu")) {
				t.Fatalf("pause guidance lost: %q", line)
			}
		}
		wantSupport := "phase=recovery_required reason=recovery_incomplete proof=none previous_failure=recovery_failed automatic_recovery=paused_retry_limit"
		if recoveryobs.ValidFailureCode(test.first) {
			wantSupport += " first_failure_code=" + test.first
		}
		if !strings.HasSuffix(en[len(en)-1], wantSupport) || !strings.HasSuffix(tr[len(tr)-1], wantSupport) {
			t.Fatalf("support line: %q / %q", en[len(en)-1], tr[len(tr)-1])
		}
	}
	// The new key is appended after every existing one and only in this state.
	var output, diagnostics bytes.Buffer
	run([]string{"status", "--request-id", requestID, "--json"}, cliRuntime{func() int { return 0 },
		func(string) recoveryobs.Status { return paused("panel_start_unverified") }, &output, &diagnostics})
	want := `{"automatic_recovery":"paused_retry_limit","schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"known","phase":"recovery_required","terminal_proof":"none","reason":"recovery_incomplete","observed_at":"2026-09-14T12:00:00Z","previous_failure":"recovery_failed","first_failure_code":"panel_start_unverified"}` + "\n"
	if output.String() != want {
		t.Fatalf("paused JSON:\n got %s\nwant %s", output.String(), want)
	}
}
