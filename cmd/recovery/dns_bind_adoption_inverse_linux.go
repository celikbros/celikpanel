//go:build linux

package main

import (
	"context"
	"fmt"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"io"
)

const ownerBINDAdoptionInverseCommand = "recover-dns-bind-adoption"

func parseOwnerBINDAdoptionInverseArgs(args []string) (string, string, bool) {
	lang := "en"
	request := ""
	seenLang := false
	if len(args) == 0 || args[0] != ownerBINDAdoptionInverseCommand {
		return "", lang, false
	}
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--request-id":
			if request != "" || i+1 >= len(args) || !servicemutationledger.ValidIdentity(args[i+1]) {
				return "", lang, false
			}
			i++
			request = args[i]
		case "--lang":
			if seenLang || i+1 >= len(args) || (args[i+1] != "en" && args[i+1] != "tr") {
				return "", lang, false
			}
			i++
			seenLang = true
			lang = args[i]
		default:
			return "", lang, false
		}
	}
	return request, lang, request != ""
}
func dispatchOwnerBINDAdoptionInverse(args []string, uid int, inverse func(context.Context, string) error, out, diagnostic io.Writer) int {
	request, lang, valid := parseOwnerBINDAdoptionInverseArgs(args)
	if !valid {
		fmt.Fprintln(diagnostic, translated(lang, "Usage: recovery recover-dns-bind-adoption --request-id <32 lowercase hex characters> [--lang en|tr]", "Kullanım: recovery recover-dns-bind-adoption --request-id <32 küçük harfli hex karakter> [--lang en|tr]"))
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, translated(lang, "Server owner action required: run this command as root or authorized sudo. No DNS operation was started.", "Sunucu sahibinin işlemi gerekiyor: bu komutu root veya yetkili sudo ile çalıştırın. DNS işlemi başlatılmadı."))
		return exitNotOwner
	}
	if inverse == nil {
		return exitUnavailable
	}
	if err := inverse(context.Background(), request); err != nil {
		if code, complete := writeCompletedDNSInverse(err, lang, request, out); complete {
			return code
		}
		fmt.Fprintln(diagnostic, translated(lang, "The accepted running BIND adoption rollback could not be verified. Inspect recovery dns-switch-status --quiesced --request-id "+request+"; resolve the reported evidence, worker, lock or native DNS condition and retry this same request. Preserve the journal and ledger. Reason: ", "Kabul edilmiş çalışan BIND devralma geri alması doğrulanamadı. recovery dns-switch-status --quiesced --request-id "+request+" çıktısını inceleyin; kanıt, çalışan, kilit veya yerel DNS sorununu giderip aynı işlemi yeniden deneyin. Günlüğü ve işlem kaydını koruyun. Neden: ")+err.Error())
		return exitUnavailable
	}
	fmt.Fprintln(out, translated(lang, "The accepted running BIND adoption rollback reached its terminal verdict for request "+request+". The original owner BIND zones were verified without stopping named. Check current authoritative DNS health before another DNS change.", "Kabul edilmiş çalışan BIND devralma geri alması "+request+" işlemi için nihai karara ulaştı. Özgün BIND bölgeleri named durdurulmadan doğrulandı. Başka geçişten önce güncel yetkili DNS sağlığını kontrol edin."))
	return exitOK
}
func runOwnerBINDAdoptionInverse(args []string, uid int, out, diagnostic io.Writer) int {
	return dispatchOwnerBINDAdoptionInverse(args, uid, func(ctx context.Context, request string) error {
		runtime, err := recoveryruntime.VerifiedLauncherRuntime()
		if err != nil {
			return fmt.Errorf("verify selected recovery launcher: %w", err)
		}
		defer runtime.Close()
		if err := runtime.VerifyExecutingBinary(); err != nil {
			return fmt.Errorf("verify selected recovery executable: %w", err)
		}
		return completeInstalledBINDAdoptionInverse(ctx, request)
	}, out, diagnostic)
}
