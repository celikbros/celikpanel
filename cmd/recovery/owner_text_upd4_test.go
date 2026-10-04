package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

// upd4 F4/F6/O8: a refused update preflight is final and safe to start again;
// after the last admitted attempt fails and before the pause is recorded the
// owner is told recovery is finishing, with the typed cause kept and no "must
// act"; the pause's renewal sentence follows the recorded scheduler state.
// EN and TR, tokens only on the support line, JSON additive.
func TestUpd4RefusedPreflightPausePendingAndRenewalGuidance(t *testing.T) {
	status := func(phase, reason, previous, code, automatic, first, renewal string) recoveryobs.Status {
		s := knownStatus(phase)
		if reason != "" {
			s.Reason = reason
		}
		s.PreviousFailure, s.FailureCode, s.AutomaticRecovery, s.FirstFailureCode, s.RenewalBeforeUpdate = previous, code, automatic, first, renewal
		return s
	}
	journal := "sudo journalctl -u celikpanel-self-update-" + requestID + ".service --no-pager -n 20"
	pending := "The update was applied, but the new version's panel did not come up. The server owner should read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50."
	pendingTR := "Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı. Sunucu sahibi sunucudaki panel günlüğünü okumalı: sudo journalctl -u celikpanel-panel -n 50."
	stopped := "Automatic certificate renewal (Certbot) was stopped for this update"
	for _, test := range []struct {
		name     string
		observed recoveryobs.Status
		en, tr   []string
		notEN    []string
		notTR    []string
		support  string
	}{
		{"refused-preflight", status("failed", "", "update_failed", "update_preflight_refused", "", "", ""),
			[]string{"The update stopped before changing the installed version or stopping any service", "one of its read-only checks refused to continue", "another panel operation was still running", "the panel database changed while it was being read", "keeps running the version it had before", journal, "Starting the update again is safe."},
			[]string{"kurulu sürümü değiştirmeden ve hiçbir hizmeti durdurmadan durdu", "salt-okur denetimlerinden biri", journal, "Güncellemeyi yeniden başlatmak güvenlidir."},
			[]string{"check this same request again", "do not start another update", "must act", "inspect the update log", "recovery runtime"},
			[]string{"kurtarma çalışma ortamı"},
			"phase=failed reason=update_failed proof=none previous_failure=update_failed failure_code=update_preflight_refused"},
		{"pause-pending/cause", status("recovery_required", "recovery_failed", "recovery_failed", "", "pause_pending", "panel_start_unverified", ""),
			[]string{pending, "The last admitted recovery attempt did not finish, and no automatic attempt remains.", "Recovery is finishing this attempt", "the one-time retry command, is recorded within about a minute", "Nothing to do yet"},
			[]string{pendingTR, "Kurtarma bu denemeyi kapatıyor", "yaklaşık bir dakika içinde kaydedilir", "Henüz yapılacak bir şey yok"},
			[]string{"must act", "Keep the server's files", "Certbot"},
			[]string{"işlem yapması gerekiyor"},
			"phase=recovery_required reason=recovery_failed proof=none previous_failure=recovery_failed automatic_recovery=pause_pending first_failure_code=panel_start_unverified"},
		{"pause-pending/generic", status("recovery_required", "recovery_failed", "recovery_failed", "", "pause_pending", "", ""),
			[]string{"The last admitted recovery attempt did not finish", "Nothing to do yet"},
			[]string{"İzin verilen son kurtarma denemesi tamamlanmadı"},
			[]string{"must act"}, nil,
			"phase=recovery_required reason=recovery_failed proof=none previous_failure=recovery_failed automatic_recovery=pause_pending"},
		{"paused/renewal-off", status("recovery_required", "", "recovery_failed", "", "paused_retry_limit", "panel_start_unverified", "off"),
			[]string{"The server owner must act", "Automatic certificate renewal (Certbot) was already off before this update, so the update did not stop it; check whether it should be on."},
			[]string{"Otomatik sertifika yenileme (Certbot) bu güncellemeden önce zaten kapalıydı"},
			[]string{stopped}, []string{"bu güncelleme için durduruldu"},
			"automatic_recovery=paused_retry_limit first_failure_code=panel_start_unverified renewal_before_update=off"},
		{"paused/renewal-on", status("recovery_required", "", "recovery_failed", "", "paused_retry_limit", "panel_start_unverified", "on"),
			[]string{"The server owner must act", stopped},
			[]string{"bu güncelleme için durduruldu"},
			[]string{"already off"}, nil,
			"automatic_recovery=paused_retry_limit first_failure_code=panel_start_unverified renewal_before_update=on"},
		{"paused/renewal-unrecorded", status("recovery_required", "", "recovery_failed", "", "paused_retry_limit", "", ""),
			[]string{stopped}, []string{"bu güncelleme için durduruldu"}, []string{"already off"}, nil,
			"automatic_recovery=paused_retry_limit"},
	} {
		en := ownerStatusText(t, test.observed, "en")
		tr := ownerStatusText(t, test.observed, "tr")
		for _, want := range test.en {
			if !strings.Contains(en[0], want) {
				t.Fatalf("%s EN lacks %q: %s", test.name, want, en[0])
			}
		}
		for _, want := range test.tr {
			if !strings.Contains(tr[0], want) {
				t.Fatalf("%s TR lacks %q: %s", test.name, want, tr[0])
			}
		}
		for _, unwanted := range test.notEN {
			if strings.Contains(en[0], unwanted) {
				t.Fatalf("%s EN says %q: %s", test.name, unwanted, en[0])
			}
		}
		for _, unwanted := range test.notTR {
			if strings.Contains(tr[0], unwanted) {
				t.Fatalf("%s TR says %q: %s", test.name, unwanted, tr[0])
			}
		}
		if strings.Contains(tr[0], "The ") {
			t.Fatalf("%s: English in Turkish output: %s", test.name, tr[0])
		}
		for _, line := range [][]string{en, tr} {
			plain := strings.NewReplacer("celikpanel-release-recovery.service", "", "celikpanel-panel", "", "celikpanel-self-update-"+requestID+".service", "").Replace(line[0])
			if token := internalToken.FindString(plain); token != "" {
				t.Fatalf("%s: internal token %q in owner text: %s", test.name, token, line[0])
			}
			if !strings.HasSuffix(line[len(line)-1], test.support) {
				t.Fatalf("%s support line: %q", test.name, line[len(line)-1])
			}
		}
	}
	// A refused preflight outside a failed record is never read as a terminal stop.
	odd := status("recovering", "", "update_failed", "update_preflight_refused", "", "", "")
	if lines := ownerStatusText(t, odd, "en"); strings.Contains(lines[0], "Starting the update again is safe") {
		t.Fatalf("preflight text outside its state: %s", lines[0])
	}
	// The JSON wire shape gains values only; the renewal key is appended last.
	for _, test := range []struct {
		observed recoveryobs.Status
		want     string
	}{
		{status("recovery_required", "recovery_failed", "recovery_failed", "", "pause_pending", "panel_start_unverified", ""),
			`{"automatic_recovery":"pause_pending","schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"known","phase":"recovery_required","terminal_proof":"none","reason":"recovery_failed","observed_at":"2026-09-14T12:00:00Z","previous_failure":"recovery_failed","first_failure_code":"panel_start_unverified"}` + "\n"},
		{status("recovery_required", "recovery_incomplete", "recovery_failed", "", "paused_retry_limit", "panel_start_unverified", "off"),
			`{"automatic_recovery":"paused_retry_limit","schema":"celikpanel-recovery-status/v1","request_id":"` + requestID + `","observation":"known","phase":"recovery_required","terminal_proof":"none","reason":"recovery_incomplete","observed_at":"2026-09-14T12:00:00Z","previous_failure":"recovery_failed","first_failure_code":"panel_start_unverified","renewal_before_update":"off"}` + "\n"},
	} {
		var output, diagnostics bytes.Buffer
		run([]string{"status", "--request-id", requestID, "--json"}, cliRuntime{func() int { return 0 }, func(string) recoveryobs.Status { return test.observed }, &output, &diagnostics})
		if output.String() != test.want {
			t.Fatalf("JSON:\n got %s\nwant %s", output.String(), test.want)
		}
	}
}
