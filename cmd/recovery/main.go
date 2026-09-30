// Recovery provides a native owner entrypoint independent of Panel/Agent.
// Status is read-only. Recovery resumes only an existing durable transaction;
// it cannot initiate a new installed-panel update.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/alicelik/celikpanel/internal/recoveryobs"
)

const recoveryProtocol = "celikpanel-recovery-protocol/v1"

var (
	buildVersion = "dev"
	buildCommit  = "unknown"
)

const (
	exitOK          = 0
	exitOutput      = 1
	exitUsage       = 2
	exitUnavailable = 3
	exitNotOwner    = 77
)

type cliOptions struct {
	command, requestID, lang string
	json                     bool
}

type cliRuntime struct {
	euid   func() int
	read   func(string) recoveryobs.Status
	stdout io.Writer
	stderr io.Writer
}

func main() {
	os.Exit(runEntry(os.Args[1:]))
}

// Parsing is closed: one command, no repeated flags and no implicit file paths.
// Ayrıştırma kapalıdır: tek komut, tekrarsız bayraklar, örtük dosya yolu yoktur.
func parseOptions(args []string) (cliOptions, bool) {
	opts := cliOptions{lang: "en"}
	if len(args) == 0 {
		return opts, false
	}
	opts.command = args[0]
	if opts.command != "status" && opts.command != "version" {
		return opts, false
	}
	seen := map[string]bool{}
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if seen[flag] {
			return opts, false
		}
		seen[flag] = true
		switch flag {
		case "--json":
			opts.json = true
		case "--lang", "--request-id":
			if i+1 == len(args) {
				return opts, false
			}
			i++
			if flag == "--lang" {
				if args[i] != "en" && args[i] != "tr" {
					return opts, false
				}
				opts.lang = args[i]
			} else {
				if opts.command != "status" || !recoveryobs.ValidRequestID(args[i]) {
					return opts, false
				}
				opts.requestID = args[i]
			}
		default:
			return opts, false
		}
	}
	return opts, opts.command == "version" || opts.requestID != ""
}

func run(args []string, rt cliRuntime) int {
	opts, valid := parseOptions(args)
	if !valid {
		fmt.Fprintln(rt.stderr, translated(opts.lang,
			"Usage: recovery status --request-id <32 lowercase hex characters> [--json] [--lang en|tr]\n       recovery version [--json] [--lang en|tr]",
			"Kullanım: recovery status --request-id <32 küçük harfli hex karakter> [--json] [--lang en|tr]\n          recovery version [--json] [--lang en|tr]"))
		return exitUsage
	}
	if rt.euid == nil || rt.euid() != 0 {
		fmt.Fprintln(rt.stderr, translated(opts.lang,
			"Owner authentication is required. Run this command from your root or authorized sudo session.",
			"Sunucu sahibi doğrulaması gerekiyor. Komutu root veya yetkili sudo oturumunuzdan çalıştırın."))
		return exitNotOwner
	}
	var err error
	result := exitOK
	if opts.command == "version" {
		if opts.json {
			err = json.NewEncoder(rt.stdout).Encode(struct {
				Protocol string `json:"protocol"`
				Version  string `json:"version"`
				Commit   string `json:"commit"`
			}{recoveryProtocol, buildVersion, buildCommit})
		} else {
			_, err = fmt.Fprintf(rt.stdout, "protocol=%s\nversion=%s\ncommit=%s\n", recoveryProtocol, buildVersion, buildCommit)
		}
	} else {
		status := unavailableStatus(opts.requestID)
		if rt.read != nil {
			observed := rt.read(opts.requestID)
			// The shared reader owns file/schema validation. Do not substitute a
			// different request or optimistically interpret a later wire schema.
			// Dosya/şema doğrulaması ortak okuyucunundur. Başka işlem kullanma;
			// sonraki bir ileti şemasını iyimser biçimde yorumlama.
			if observed.Schema == recoveryobs.StatusSchema && observed.RequestID == opts.requestID &&
				(observed.Observation == "known" || observed.Observation == "unavailable") {
				status = observed
			}
		}
		if status.Observation != "known" {
			status = unavailableStatus(opts.requestID)
			result = exitUnavailable
		}
		if opts.json {
			err = json.NewEncoder(rt.stdout).Encode(status)
		} else {
			err = writeStatus(rt.stdout, opts.lang, status)
		}
	}
	if err != nil {
		fmt.Fprintln(rt.stderr, translated(opts.lang, "Could not write the result.", "Sonuç yazılamadı."))
		return exitOutput
	}
	// Exit zero means a known observation was read, not that the operation
	// succeeded. Its phase and terminal proof carry that separate meaning.
	// Sıfır çıkış kodu bilinen gözlemin okunduğunu söyler; işlem başarısı değildir.
	// Bu ayrı anlamı phase ve terminal_proof alanları taşır.
	return result
}

func unavailableStatus(id string) recoveryobs.Status {
	return recoveryobs.Status{Schema: recoveryobs.StatusSchema, RequestID: id,
		Observation: "unavailable", TerminalProof: "none", Reason: "observation_unavailable"}
}

func translated(lang, en, tr string) string {
	if lang == "tr" {
		return tr
	}
	return en
}

// failureCodeGuidance returns reviewed owner guidance for the two typed update
// causes. The candidate start check runs before completion is marked, so its
// failure is returned to the old release; a panel that did not come up after
// the switch is completed forward only.
func failureCodeGuidance(status recoveryobs.Status) (string, string, bool) {
	if status.Observation != "known" || status.PreviousFailure != "update_failed" {
		return "", "", false
	}
	switch status.FailureCode {
	case "candidate_panel_startup_check_failed":
		if status.Phase == "recovered" && status.TerminalProof == "rollback_verified" {
			return "The new version's panel failed its start check before anything was switched on, so the server was returned to the previous version automatically. The previous version keeps running. Nothing needs to be done on the server. When you report this, include the reason line shown for this update on the panel's update page.",
				"Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi; bu yüzden sunucu otomatik olarak önceki sürüme döndürüldü. Önceki sürüm çalışmaya devam ediyor. Sunucuda yapmanız gereken bir şey yok. Bunu bildirirken panelin güncelleme sayfasında bu güncelleme için gösterilen neden satırını ekleyin.", true
		}
		if status.TerminalProof == "none" && (status.Phase == "failed" || status.Phase == "recovering") {
			return "The new version's panel failed its start check before anything was switched on. The server is being returned to the previous version automatically; nothing needs to be done on the server. Check this same request again for the verified result; do not start another update.",
				"Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi. Sunucu otomatik olarak önceki sürüme döndürülüyor; sunucuda yapmanız gereken bir şey yok. Doğrulanmış sonuç için aynı işlemi yeniden sorgulayın; başka güncelleme başlatmayın.", true
		}
	case "panel_start_unverified":
		if status.TerminalProof == "none" && (status.Phase == "failed" || status.Phase == "recovering") {
			return "The update was applied, but the new version's panel did not come up. The server owner should read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. The update's completion is retried automatically up to its limit; after that, sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 shows a one-time retry command for this operation. There is no supported return to the previous version from this point.",
				"Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı. Sunucu sahibi sunucudaki panel günlüğünü okumalı: sudo journalctl -u celikpanel-panel -n 50. Güncellemenin tamamlanması sınırına kadar otomatik olarak yeniden denenir; sonrasında sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 bu işlem için tek seferlik yeniden deneme komutunu gösterir. Bu noktadan önceki sürüme desteklenen bir dönüş yok.", true
		}
	}
	return "", "", false
}

func writeStatus(w io.Writer, lang string, status recoveryobs.Status) error {
	en, tr := "The state could not be verified. Check this same request again; preserve evidence and do not start another update.",
		"Durum doğrulanamadı. Aynı işlemi yeniden sorgulayın; kanıtları koruyun ve başka güncelleme başlatmayın."
	if status.Observation == "known" {
		switch status.Phase {
		case "accepted":
			en, tr = "The update was accepted. Follow this same request; do not start another update.", "Güncelleme kabul edilmiş. Aynı işlemi takip edin; başka güncelleme başlatmayın."
		case "running":
			en, tr = "The last recorded state is updating. Check this same request again.", "Son kayıtta güncelleme sürüyor. Aynı işlemi yeniden sorgulayın."
		case "recovering":
			en, tr = "The last recorded state is recovering. Check this same request again.", "Son kayıtta kurtarma sürüyor. Aynı işlemi yeniden sorgulayın."
		case "failed":
			en, tr = "The update reported a failure. Review this request in the panel when available; do not start another update.", "Güncelleme hata bildirmiş. Panel erişilebilir olduğunda bu işlemi inceleyin; başka güncelleme başlatmayın."
		case "recovery_required":
			en, tr = "Recovery needs attention. Preserve evidence. Inspect the recorded reason and next action with sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50; do not start another update.", "Kurtarma için işlem gerekiyor. Kanıtları koruyun. Kaydedilen neden ve sonraki eylem için sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 komutunu kullanın; başka güncelleme başlatmayın."
		case "succeeded":
			en, tr = "The producer recorded verified update completion. Check current service health separately.", "Üretici, güncellemenin doğrulanmış tamamlanmasını kaydetmiş. Güncel hizmet sağlığını ayrıca kontrol edin."
		case "recovered":
			en, tr = "The producer recorded verified restoration. Check current service health separately.", "Üretici, doğrulanmış geri yükleme kaydetmiş. Güncel hizmet sağlığını ayrıca kontrol edin."
		}
	}
	// A typed update cause refines the phase guidance only while the update's own
	// failure is the latest recorded failure. Waits and pauses below still win.
	// Tipli güncelleme nedeni, yalnız son kayıtlı hata güncellemenin kendi hatası
	// olduğunda aşama yönlendirmesini inceltir; bekleme ve duraklama önceliklidir.
	if en2, tr2, ok := failureCodeGuidance(status); ok {
		en, tr = en2, tr2
	}
	if status.Phase == "recovering" && status.TerminalProof == "none" && recoveryobs.ValidWaitingFor(status.WaitingFor) {
		en, tr = "The last recorded state is waiting for the operating system transition. No owner action is needed for this wait; the native timer will check the same operation again when ready. Recovery is not yet complete.", "Son kayıtta işletim sistemi geçişi bekleniyor. Bu bekleme için kullanıcı işlemi gerekmiyor; yerel zamanlayıcı hazır olduğunda aynı işlemi yeniden kontrol edecek. Kurtarma henüz tamamlanmadı."
	}
	if status.Observation == "known" && status.Phase == "recovery_required" && status.TerminalProof == "none" && status.AutomaticRecovery == "paused_retry_limit" {
		en, tr = "Automatic recovery last reported that all three attempts were used without verified completion. The server owner must inspect sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, resolve the cause, then use the one-time same-operation retry command shown there. Checking status does not retry recovery.", "Son kayıtta üç otomatik kurtarma denemesi doğrulanmış tamamlanma olmadan kullanılmış. Sunucu sahibi sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 ile günlüğü incelemeli, nedeni gidermeli ve orada aynı işlem için gösterilen tek seferlik yeniden deneme komutunu kullanmalıdır. Durum sorgusu kurtarmayı yeniden başlatmaz."
	}
	if _, err := fmt.Fprintln(w, translated(lang, en, tr)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s: %s\n", translated(lang, "Request", "İşlem"), status.RequestID); err != nil {
		return err
	}
	if status.Observation == "known" {
		if _, err := fmt.Fprintf(w, "%s: %s\n%s: %s\n",
			translated(lang, "Recorded at", "Kayıt zamanı"), status.ObservedAt,
			translated(lang, "Terminal proof", "Nihai kanıt"), status.TerminalProof); err != nil {
			return err
		}
		if status.PreviousFailure != "" {
			if _, err := fmt.Fprintf(w, "%s: %s\n", translated(lang, "Previous failure", "Önceki hata"), status.PreviousFailure); err != nil {
				return err
			}
		}
		if status.PreviousFailure == "update_failed" && recoveryobs.ValidFailureCode(status.FailureCode) {
			if _, err := fmt.Fprintf(w, "%s: %s\n", translated(lang, "Recorded cause", "Kaydedilen neden"), status.FailureCode); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(w, translated(lang,
		"This is a recorded observation; current service health was not checked.",
		"Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi."))
	return err
}
