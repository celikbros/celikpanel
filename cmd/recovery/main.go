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
			en, tr = "Recovery needs attention. Preserve evidence and follow the owner recovery guidance for this request.", "Kurtarma için işlem gerekiyor. Kanıtları koruyun ve bu işlem için sunucu sahibine yönelik kurtarma yönlendirmesini izleyin."
		case "succeeded":
			en, tr = "The producer recorded verified update completion. Check current service health separately.", "Üretici, güncellemenin doğrulanmış tamamlanmasını kaydetmiş. Güncel hizmet sağlığını ayrıca kontrol edin."
		case "recovered":
			en, tr = "The producer recorded verified restoration. Check current service health separately.", "Üretici, doğrulanmış geri yükleme kaydetmiş. Güncel hizmet sağlığını ayrıca kontrol edin."
		}
	}
	if status.Phase == "recovering" && status.TerminalProof == "none" && recoveryobs.ValidWaitingFor(status.WaitingFor) {
		en, tr = "The last recorded state is waiting for the operating system transition. No owner action is needed for this wait; the native timer will check the same operation again when ready. Recovery is not yet complete.", "Son kayıtta işletim sistemi geçişi bekleniyor. Bu bekleme için kullanıcı işlemi gerekmiyor; yerel zamanlayıcı hazır olduğunda aynı işlemi yeniden kontrol edecek. Kurtarma henüz tamamlanmadı."
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
	}
	_, err := fmt.Fprintln(w, translated(lang,
		"This is a recorded observation; current service health was not checked.",
		"Bu kaydedilmiş bir gözlemdir; güncel hizmet sağlığı kontrol edilmedi."))
	return err
}
