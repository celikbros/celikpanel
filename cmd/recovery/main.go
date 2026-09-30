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
	case "recovery_runtime_preflight_failed":
		// Written only before any durable release marker: nothing changed and
		// no recovery follows, so this failed record is final for the request.
		if status.Phase == "failed" && status.TerminalProof == "none" {
			return preflightStoppedGuidance(status.RequestID)
		}
	case "candidate_panel_startup_check_failed":
		if status.Phase == "recovered" && status.TerminalProof == "rollback_verified" {
			return "The new version's panel failed its start check before anything was switched on, so the server was returned to the previous version automatically. The previous version keeps running. Nothing needs to be done on the server. Do not start the same version again until a corrected version is published. When you report this, include the reason line shown for this update on the panel's update page.",
				"Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi; bu yüzden sunucu otomatik olarak önceki sürüme döndürüldü. Önceki sürüm çalışmaya devam ediyor. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın. Bunu bildirirken panelin güncelleme sayfasında bu güncelleme için gösterilen neden satırını ekleyin.", true
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

// preflightStoppedGuidance explains a request that ended in the updater's
// read-only recovery runtime preflight. The typed reason line itself is kept
// in the worker's journal and on the panel's update page; this record does
// not carry it.
func preflightStoppedGuidance(requestID string) (string, string, bool) {
	journal := "sudo journalctl -u celikpanel-self-update-" + requestID + ".service --no-pager -n 20"
	return "The update stopped before changing the installed version or stopping any service: its read-only check with the recovery runtime could not verify the current installation. The server keeps running the version it had before, as before, and nothing more happens for this request. The recorded reason is on the panel's update page and in the update log: " + journal + " (the line containing CELIKPANEL_UPDATE_FAILURE). The server owner needs to act only if that reason names a condition on this server, such as the package manager running or another operation still in progress: let it finish or fix it first. Starting the update again is safe.",
		"Güncelleme, kurulu sürümü değiştirmeden ve hiçbir hizmeti durdurmadan durdu: kurtarma çalışma ortamıyla yapılan salt-okur denetim mevcut kurulumu doğrulayamadı. Sunucu önceki sürümünü eskisi gibi çalıştırmaya devam ediyor ve bu işlem için başka bir şey olmayacak. Kaydedilen neden panelin güncelleme sayfasında ve güncelleme günlüğündedir: " + journal + " (CELIKPANEL_UPDATE_FAILURE içeren satır). Sunucu sahibinin yalnız bu neden sunucudaki bir durumu belirtiyorsa işlem yapması gerekir; örneğin paket yöneticisi çalışıyorsa ya da başka bir işlem sürüyorsa bitmesini bekleyin veya önce sorunu giderin. Güncellemeyi yeniden başlatmak güvenlidir.", true
}

// retryingCauseGuidance keeps the update's first typed cause visible while
// automatic recovery still has attempts left, after a recovery attempt failure
// hid the update's own failure code. It reuses the reviewed pending text.
func retryingCauseGuidance(status recoveryobs.Status) (string, string, bool) {
	if !recoveryobs.ValidFailureCode(status.FirstFailureCode) {
		return "", "", false
	}
	pending := status
	pending.Phase, pending.PreviousFailure, pending.FailureCode = "recovering", "update_failed", status.FirstFailureCode
	return failureCodeGuidance(pending)
}

// Automatic certificate renewal is stopped for the whole update transaction.
// At the pause the runner returns it to its recorded state when the retry
// cannot undo a renewal (forward completion); a pending rollback keeps it
// stopped. The recovery journal names which applies.
const (
	pausedRenewalEN = " Automatic certificate renewal (Certbot) was stopped for this update; the same recovery journal says whether it was returned to how it was before the update or stays stopped until this operation finishes."
	pausedRenewalTR = " Otomatik sertifika yenileme (Certbot) bu güncelleme için durduruldu; aynı kurtarma günlüğü, güncellemeden önceki hâline döndürülüp döndürülmediğini ya da bu işlem bitene kadar durdurulmuş kalacağını söyler."
)

func writeStatus(w io.Writer, lang string, status recoveryobs.Status) error {
	// The first lines speak to the server owner: what happened, what the server
	// runs now, who acts and the next action. Internal tokens appear only on the
	// final support line; --json is a separate, unchanged wire shape.
	// İlk satırlar sunucu sahibine konuşur; iç belirteçler yalnız son destek
	// satırındadır. --json ayrı ve değişmemiş bir biçimdir.
	en, tr := "The result of this update could not be read, so it is unknown. Nothing is known yet about which version the server runs. Check this same request again in a minute; keep the server as it is and do not start another update meanwhile.",
		"Bu güncellemenin sonucu okunamadı; sonuç bilinmiyor. Sunucunun hangi sürümü çalıştırdığı henüz bilinmiyor. Bir dakika sonra aynı işlemi yeniden sorgulayın; bu arada sunucuya dokunmayın ve başka güncelleme başlatmayın."
	if status.Observation == "known" {
		switch status.Phase {
		case "accepted":
			en, tr = "The update was accepted and is about to start. The server still runs its current version. Nothing to do now: check this same request again in a minute and do not start another update.",
				"Güncelleme kabul edildi ve başlamak üzere. Sunucu hâlâ mevcut sürümünü çalıştırıyor. Şimdi yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme başlatmayın."
		case "running":
			en, tr = "The update is being applied; the panel may be unavailable for a short time. Nothing to do now: check this same request again in a minute. Checking does not restart the update.",
				"Güncelleme uygulanıyor; panel kısa bir süre erişilemeyebilir. Şimdi yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın. Sorgulamak güncellemeyi yeniden başlatmaz."
		case "recovering":
			en, tr = "The update did not finish normally, and automatic recovery is running: the server is either being returned to the previous version or the update is being completed safely. Nothing to do now: check this same request again in a minute and do not start another update.",
				"Güncelleme normal biçimde tamamlanmadı ve otomatik kurtarma çalışıyor: sunucu ya önceki sürüme döndürülüyor ya da güncelleme güvenle tamamlanıyor. Şimdi yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme başlatmayın."
		case "failed":
			en, tr = "The update failed. If it stopped before anything was changed, the server keeps running the version it had; otherwise automatic recovery takes over and this status changes to recovery. Nothing to do on the server now: check this same request again in a few minutes and do not start another update. The panel's update page shows the reason when the panel is reachable.",
				"Güncelleme başarısız oldu. Hiçbir şey değişmeden durduysa sunucu önceki sürümünü çalıştırmaya devam eder; aksi hâlde otomatik kurtarma devreye girer ve bu durum kurtarmaya geçer. Şimdi sunucuda yapmanız gereken bir şey yok: birkaç dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme başlatmayın. Panel erişilebilir olduğunda güncelleme sayfası nedeni gösterir."
		case "recovery_required":
			en, tr = "Automatic recovery could not finish this update, so the server may be between versions. The server owner must act: read the recorded reason and next step with sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50. Keep the server's files as they are and do not start another update.",
				"Otomatik kurtarma bu güncellemeyi tamamlayamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: kaydedilen nedeni ve sonraki adımı sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 ile okuyun. Sunucudaki dosyalara dokunmayın ve başka güncelleme başlatmayın."
		case "succeeded":
			en, tr = "The update completed and the new version was verified when it finished. Nothing else is needed on the server. Open the panel to check that everything works now.",
				"Güncelleme tamamlandı ve yeni sürüm bittiği anda doğrulandı. Sunucuda başka bir işlem gerekmiyor. Her şeyin şu an çalıştığını görmek için paneli açın."
		case "recovered":
			en, tr = "The update did not complete, and the server was returned automatically to the version it ran before; that restoration was verified. Nothing needs to be done on the server. Do not start the same version again until a corrected version is published; the panel's update page shows what is known about the cause.",
				"Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri dönüş doğrulandı. Sunucuda yapmanız gereken bir şey yok. Düzeltilmiş bir sürüm yayımlanana kadar aynı sürümü yeniden başlatmayın; nedenle ilgili bilinenler panelin güncelleme sayfasında gösterilir."
		}
	}
	// A typed update cause refines the phase guidance only while the update's own
	// failure is the latest recorded failure. Waits and pauses below still win.
	// Tipli güncelleme nedeni, yalnız son kayıtlı hata güncellemenin kendi hatası
	// olduğunda aşama yönlendirmesini inceltir; bekleme ve duraklama önceliklidir.
	if en2, tr2, ok := failureCodeGuidance(status); ok {
		en, tr = en2, tr2
	} else if status.Observation == "known" && status.Phase == "recovering" && status.TerminalProof == "none" &&
		status.PreviousFailure == "recovery_failed" {
		// The next automatic attempt runs after an earlier one failed; the
		// update's first typed cause stays the owner's reference point.
		if en2, tr2, ok := retryingCauseGuidance(status); ok {
			en, tr = en2, tr2
		}
	}
	if status.Phase == "recovering" && status.TerminalProof == "none" && recoveryobs.ValidWaitingFor(status.WaitingFor) {
		en, tr = "The update did not finish normally, and recovery is waiting for the server to finish starting or stopping. Nothing to do now: the server's recovery timer checks this same operation again by itself when the system is ready. Recovery is not yet complete.",
			"Güncelleme normal biçimde tamamlanmadı ve kurtarma, sunucunun açılmasını ya da kapanmasını bitirmesini bekliyor. Şimdi yapmanız gereken bir şey yok: sunucunun kurtarma zamanlayıcısı, sistem hazır olduğunda aynı işlemi kendiliğinden yeniden kontrol eder. Kurtarma henüz tamamlanmadı."
	}
	if status.Observation == "known" && status.Phase == "recovery_required" && status.TerminalProof == "none" && status.AutomaticRecovery == "paused_retry_limit" {
		en, tr = "Automatic recovery used all three attempts without finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation retry command shown there. Checking status does not retry recovery.",
			"Otomatik kurtarma üç denemenin hepsini kullandı ve tamamlanamadı; sunucu iki sürüm arasında kalmış olabilir. Sunucu sahibinin işlem yapması gerekiyor: sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50 çıktısını okuyun, belirtilen nedeni giderin ve orada aynı işlem için gösterilen tek seferlik yeniden deneme komutunu çalıştırın. Durum sorgusu kurtarmayı yeniden başlatmaz."
		// The pause guidance stays; a typed first cause adds, before it, what is
		// wrong and where to look.
		// Duraklatma yönlendirmesi kalır; tipli ilk neden, öncesine neyin yanlış
		// olduğunu ve nereye bakılacağını ekler.
		if causeEN, causeTR, ok := pausedCauseGuidance(status.FirstFailureCode); ok {
			en, tr = causeEN+" "+en, causeTR+" "+tr
		}
		en, tr = en+pausedRenewalEN, tr+pausedRenewalTR
	}
	// The last admitted automatic attempt failed, and the native timer admits
	// another one: the owner is not asked to act before the pause.
	// Son otomatik deneme başarısız oldu ve zamanlayıcı bir deneme daha yapacak.
	if status.Observation == "known" && status.Phase == "recovery_required" && status.TerminalProof == "none" && status.AutomaticRecovery == "retry_scheduled" {
		en, tr = "The last automatic recovery attempt did not finish, and automatic recovery tries this same operation again by itself: the next attempt normally starts about 30 seconds after the previous one ended, up to three attempts in total. Nothing is needed on the server now: check this same request again in a minute and do not start another update. The server owner has to act only if automatic recovery pauses.",
			"Son otomatik kurtarma denemesi tamamlanmadı ve otomatik kurtarma aynı işlemi kendiliğinden yeniden deniyor: sonraki deneme normalde bir öncekinin bitişinden yaklaşık 30 saniye sonra başlar, toplamda en çok üç deneme yapılır. Şimdi sunucuda yapmanız gereken bir şey yok: bir dakika sonra aynı işlemi yeniden sorgulayın ve başka güncelleme başlatmayın. Sunucu sahibinin ancak otomatik kurtarma durursa işlem yapması gerekir."
		if causeEN, causeTR, ok := retryingCauseGuidance(status); ok {
			en, tr = causeEN+" "+en, causeTR+" "+tr
		}
	}
	if _, err := fmt.Fprintln(w, translated(lang, en, tr)); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(w, "%s: %s\n", translated(lang, "Request", "İşlem"), status.RequestID); err != nil {
		return err
	}
	if status.Observation == "known" {
		if _, err := fmt.Fprintf(w, "%s: %s\n", translated(lang, "Recorded at", "Kayıt zamanı"), status.ObservedAt); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, translated(lang,
		"This is a recorded observation; current service health was not checked.",
		"Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi.")); err != nil {
		return err
	}
	_, err := fmt.Fprintf(w, "%s: %s\n", translated(lang, "Recorded state (for support)", "Kayıtlı durum (destek için)"), supportState(status))
	return err
}

// pausedCauseGuidance names the update's first typed cause while automatic
// recovery is paused on its retry limit.
func pausedCauseGuidance(code string) (string, string, bool) {
	switch code {
	case "panel_start_unverified":
		return "The update was applied, but the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed. There is no supported return to the previous version from this point.",
			"Güncelleme uygulandı, ancak yeni sürümün paneli açılmadı ve güncellemenin tamamlanması sınırına kadar yeniden denendi. Sunucudaki panel günlüğünü okuyun: sudo journalctl -u celikpanel-panel -n 50. Bu neden giderilene kadar yeniden deneme aynı başlatmayı tekrarlar. Bu noktadan önceki sürüme desteklenen bir dönüş yok.", true
	case "candidate_panel_startup_check_failed":
		return "The new version's panel failed its start check before anything was switched on, and the automatic return to the previous version did not finish. The recovery journal shows what stopped it; retrying continues the same return to the previous version.",
			"Yeni sürümün paneli, hiçbir şey devreye alınmadan önce başlangıç denetiminden geçemedi ve önceki sürüme otomatik dönüş tamamlanmadı. Kurtarma günlüğü neyin durdurduğunu gösterir; yeniden deneme önceki sürüme aynı dönüşü sürdürür.", true
	}
	return "", "", false
}

// supportState is the only human line carrying internal tokens. Every value is
// already a closed reader allowlist; unknown optional values are omitted.
func supportState(status recoveryobs.Status) string {
	if status.Observation != "known" {
		return "observation=unavailable reason=" + status.Reason + " proof=" + status.TerminalProof
	}
	line := "phase=" + status.Phase + " reason=" + status.Reason + " proof=" + status.TerminalProof
	if status.PreviousFailure != "" {
		line += " previous_failure=" + status.PreviousFailure
	}
	if status.PreviousFailure == "update_failed" && recoveryobs.ValidFailureCode(status.FailureCode) {
		line += " failure_code=" + status.FailureCode
	}
	if status.Phase == "recovering" && recoveryobs.ValidWaitingFor(status.WaitingFor) {
		line += " waiting_for=" + status.WaitingFor
	}
	if status.Phase == "recovery_required" && recoveryobs.ValidAutomatic(status.AutomaticRecovery) {
		line += " automatic_recovery=" + status.AutomaticRecovery
		if recoveryobs.ValidFailureCode(status.FirstFailureCode) {
			line += " first_failure_code=" + status.FirstFailureCode
		}
	}
	if status.Phase == "recovering" && status.PreviousFailure == "recovery_failed" && recoveryobs.ValidFailureCode(status.FirstFailureCode) {
		line += " first_failure_code=" + status.FirstFailureCode
	}
	return line
}
