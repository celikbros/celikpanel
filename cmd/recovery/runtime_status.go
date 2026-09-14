package main

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
)

type runtimeStatusResult struct {
	Schema      string `json:"schema"`
	Observation string `json:"observation"`
	Phase       string `json:"phase,omitempty"`
	Previous    string `json:"previous,omitempty"`
	Target      string `json:"target,omitempty"`
	Actor       string `json:"actor"`
	Action      string `json:"action"`
	Reason      string `json:"reason,omitempty"`
}

// This view is a fresh, nonauthorizing observation; it never enrolls, promotes,
// retries an update or infers workload health from a completed kit transition.
// Bu görünüm salt gözlemdir; kayıt, kit geçişi veya güncelleme başlatmaz ve
// tamamlanmış kit geçişinden servis sağlığı sonucu çıkarmaz.
func runRuntimeStatus(args []string, uid int, inspect func() (recoveryruntime.PromotionStatus, error), out, stderr io.Writer) int {
	if uid != 0 {
		return exitNotOwner
	}
	lang, asJSON := "en", false
	seen := map[string]bool{}
	if len(args) == 0 || args[0] != "runtime-status" {
		return exitUsage
	}
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if seen[flag] {
			return exitUsage
		}
		seen[flag] = true
		switch flag {
		case "--json":
			asJSON = true
		case "--lang":
			if i+1 >= len(args) || (args[i+1] != "en" && args[i+1] != "tr") {
				return exitUsage
			}
			i++
			lang = args[i]
		default:
			return exitUsage
		}
	}
	result := runtimeStatusResult{Schema: "celikpanel/recovery-runtime-status/v1", Observation: "unavailable", Actor: "server_owner", Action: "preserve_evidence", Reason: "runtime_read_failed"}
	status, err := inspect()
	code := exitUnavailable
	if err == nil {
		switch status.Phase {
		case "none", "prepared", "launcher_published", "selection_published", "committed":
			result.Observation = "known"
			result.Phase = status.Phase
			result.Previous = status.Previous
			result.Target = status.Target
			result.Reason = ""
			if status.Phase == "prepared" {
				result.Action = "review_same_release_in_panel"
			} else if status.Phase == "launcher_published" || status.Phase == "selection_published" {
				result.Action = "owner_recovery"
			} else {
				result.Actor = "none"
				result.Action = "none"
			}
			code = exitOK
		}
	} else {
		result.Reason = recoveryruntime.ReasonCode(err)
	}
	if asJSON {
		err = json.NewEncoder(out).Encode(result)
	} else {
		message := runtimeStatusMessage(result, lang)
		_, err = fmt.Fprintln(out, message)
	}
	if err != nil {
		fmt.Fprintln(stderr, translated(lang, "Could not write recovery runtime status.", "Kurtarma ortamı durumu yazılamadı."))
		return exitOutput
	}
	return code
}

func runtimeStatusMessage(result runtimeStatusResult, lang string) string {
	switch result.Phase {
	case "none":
		return translated(lang,
			"No recovery kit transition is recorded. This does not verify the selected kit, application update or service health.",
			"Kaydedilmiş kurtarma kiti geçişi yok. Bu sonuç seçili kiti, panel güncellemesini veya servis sağlığını doğrulamaz.")
	case "prepared":
		return translated(lang,
			"The replacement kit is prepared; the previous launcher and selection remain in use. The server owner can review the same release in CelikPanel to resume its verified preflight. The old launcher cannot automatically finish this early transition.",
			"Yeni kit hazır; önceki başlatıcı ve seçim kullanılıyor. Sunucu sahibi aynı sürümü CelikPanel'de inceleyerek doğrulanan ön kontrolü sürdürebilir. Eski başlatıcı bu erken geçişi otomatik tamamlayamaz.")
	case "launcher_published":
		return translated(lang,
			"The replacement launcher is installed; the previous recovery kit is still selected. The server owner can run sudo /usr/libexec/celikpanel/recovery recover to recheck the retained transition and existing operation. This does not start a new panel update.",
			"Yeni başlatıcı kuruldu; önceki kurtarma kiti hâlâ seçili. Sunucu sahibi sudo /usr/libexec/celikpanel/recovery recover komutuyla korunan geçişi ve mevcut işlemi yeniden denetleyebilir. Bu komut yeni panel güncellemesi başlatmaz.")
	case "selection_published":
		return translated(lang,
			"The replacement kit is selected; its final transition receipt is pending. The server owner can run sudo /usr/libexec/celikpanel/recovery recover to check the same transition and existing operation. An active release is recovered without changing its selected kit.",
			"Yeni kit seçildi; geçişin son doğrulama kaydı bekliyor. Sunucu sahibi sudo /usr/libexec/celikpanel/recovery recover komutuyla aynı geçişi ve mevcut işlemi denetleyebilir. Etkin sürüm işlemi, seçili kiti değiştirilmeden kurtarılır.")
	case "committed":
		return translated(lang,
			"The recovery kit transition is verified complete. Check the application update and current service health separately.",
			"Kurtarma kiti geçişinin tamamlandığı doğrulandı. Panel güncellemesini ve güncel servis sağlığını ayrıca kontrol edin.")
	default:
		return translated(lang,
			"Recovery kit transition evidence could not be verified. The server owner should preserve the files and update log and resolve the reported evidence problem before retrying. No selection was changed by this check.",
			"Kurtarma kiti geçişinin kanıtları doğrulanamadı. Sunucu sahibi dosyaları ve güncelleme günlüğünü korumalı, yeniden denemeden önce bildirilen kanıt sorununu çözmelidir. Bu kontrol seçim değiştirmedi.") + " (" + result.Reason + ")"
	}
}
