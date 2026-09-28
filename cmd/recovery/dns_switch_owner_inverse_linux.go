//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// This command has one purpose: continue an already accepted PowerDNS adoption
// rollback using the installed, secured evidence. It cannot select a path,
// decide a new rollback, install software, or start another DNS switch.
const ownerPDNSAdoptionInverseCommand = "recover-dns-pdns-adoption"

func parseOwnerPDNSAdoptionInverseArgs(args []string) (requestID, lang string, valid bool) {
	lang = "en"
	if len(args) == 0 || args[0] != ownerPDNSAdoptionInverseCommand {
		return "", lang, false
	}
	haveLang := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--request-id":
			if requestID != "" || i+1 >= len(args) || !servicemutationledger.ValidIdentity(args[i+1]) {
				return "", lang, false
			}
			i++
			requestID = args[i]
		case "--lang":
			if haveLang || i+1 >= len(args) || (args[i+1] != "en" && args[i+1] != "tr") {
				return "", lang, false
			}
			i++
			haveLang, lang = true, args[i]
		default:
			return "", lang, false
		}
	}
	return requestID, lang, requestID != ""
}

func runOwnerPDNSAdoptionInverse(args []string, uid int, out, diagnostic io.Writer) int {
	return dispatchOwnerPDNSAdoptionInverse(args, uid, func(ctx context.Context, requestID string) error {
		runtime, err := recoveryruntime.VerifiedLauncherRuntime()
		if err != nil {
			return fmt.Errorf("verify protected selected recovery launcher: %w", err)
		}
		defer runtime.Close()
		if err := runtime.VerifyExecutingBinary(); err != nil {
			return fmt.Errorf("verify selected recovery executable: %w", err)
		}
		return completeInstalledPDNSAdoptionInverse(ctx, requestID)
	}, out, diagnostic)
}

func dispatchOwnerPDNSAdoptionInverse(
	args []string, uid int, inverse func(context.Context, string) error, out, diagnostic io.Writer,
) int {
	requestID, lang, valid := parseOwnerPDNSAdoptionInverseArgs(args)
	if !valid {
		fmt.Fprintln(diagnostic, translated(lang,
			"Usage: recovery recover-dns-pdns-adoption --request-id <32 lowercase hex characters> [--lang en|tr]",
			"Kullanım: recovery recover-dns-pdns-adoption --request-id <32 küçük harfli hex karakter> [--lang en|tr]"))
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, translated(lang,
			"Server owner action required: run this exact command from a root or authorized sudo session. No DNS operation was started.",
			"Sunucu sahibinin işlemi gerekiyor: bu komutu root veya yetkili sudo oturumunda çalıştırın. DNS işlemi başlatılmadı."))
		return exitNotOwner
	}
	if inverse == nil {
		fmt.Fprintln(diagnostic, translated(lang,
			"The independent DNS recovery executor is unavailable. Preserve the same request and its evidence; inspect the installed recovery runtime before retrying.",
			"Bağımsız DNS kurtarma yürütücüsü kullanılamıyor. Aynı işlemi ve kanıtlarını koruyun; yeniden denemeden önce kurulu kurtarma çalışma ortamını inceleyin."))
		return exitUnavailable
	}
	err := inverse(context.Background(), requestID)
	if err == nil {
		if _, writeErr := fmt.Fprintln(out, translated(lang,
			"The accepted PowerDNS adoption rollback reached its terminal verdict for request "+requestID+". Native PowerDNS was verified during recovery. The server owner should check current authoritative DNS health separately before starting another switch.",
			"Kabul edilmiş PowerDNS devralma geri alması "+requestID+" işlemi için nihai karara ulaştı. Yerel PowerDNS kurtarma sırasında doğrulandı. Sunucu sahibi yeni bir geçiş başlatmadan önce güncel yetkili DNS sağlığını ayrıca kontrol etmelidir.")); writeErr != nil {
			return exitOutput
		}
		return exitOK
	}
	if errors.Is(err, errPDNSInverseTerminalLedgerObserved) {
		fmt.Fprintln(diagnostic, translated(lang,
			"The same request has a verified historical rollback verdict, but its retired journal cannot prove current PowerDNS health. The server owner should check authoritative DNS and this request's ledger; preserve the evidence. No new mutation was started. ",
			"Aynı işlemin doğrulanmış geçmiş geri alma kararı var; ancak kaldırılmış günlük güncel PowerDNS sağlığını kanıtlamaz. Sunucu sahibi yetkili DNS'i ve bu işlemin kaydını kontrol edip kanıtları korumalıdır. Yeni değişiklik başlatılmadı. ")+err.Error())
		return exitUnavailable
	}
	fmt.Fprintln(diagnostic, translated(lang,
		"This PowerDNS adoption rollback could not be verified. The server owner should inspect `recovery dns-switch-status --quiesced --request-id "+requestID+"`, resolve the reported worker, lock, evidence or native DNS condition, then retry this same request. Preserve the journal and ledger; do not start another DNS switch. Reason: ",
		"Bu PowerDNS devralma geri alması doğrulanamadı. Sunucu sahibi `recovery dns-switch-status --quiesced --request-id "+requestID+"` çıktısını incelemeli, bildirilen çalışan, kilit, kanıt veya yerel DNS sorununu gidermeli ve aynı işlemi yeniden denemelidir. Günlüğü ve işlem kaydını koruyun; başka DNS geçişi başlatmayın. Neden: ")+err.Error())
	return exitUnavailable
}
