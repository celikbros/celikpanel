package main

import (
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// A server configured with views cannot be taken over yet, and says so before
// the operator asks for anything (register R-044).
//
// The takeover owns four directives in the options block. BIND lets a view set
// the same four, where they win, and once one view exists BIND requires every
// zone to live inside a view. So on a host with views the takeover would either
// report a setting it does not control, or write zones the configuration check
// refuses at the end - a clean restore, no outage, and a refusal that arrives
// after the work rather than before it.
//
// Putting CelikPanel's zones and directives inside the right view is the real
// feature; it belongs with the zone-placement work and is not here. What is
// here is the honest refusal: the host looks for views while readiness is being
// gathered, and if it finds one the preview is blocked by name, with the file
// and the line, before a preview token exists. The screen says what the server
// is, that CelikPanel cannot manage a server configured that way yet, and what
// the operator can do.
//
// The same treatment covers a configuration CelikPanel could not read whole.
// "No views found" would be a guess when a file the configuration includes
// could not be read, and a guess is what the whole takeover is built to avoid.
// It is a different blocker with different words, because it asks the operator
// for a different thing.
//
// View ile yapılandırılmış bir sunucu henüz devralınamaz ve bunu operatör bir
// şey istemeden önce söyler (defter R-044).
//
// Devralma, seçenek bloğundaki dört direktifin sahibidir. BIND bir view'in aynı
// dördünü koymasına izin verir ve orada view kazanır; üstelik bir view var
// olduğu anda BIND her bölgenin bir view içinde olmasını şart koşar. Yani view'i
// olan bir sunucuda devralma ya denetlemediği bir ayarı bildirir ya da
// yapılandırma denetiminin en sonda reddettiği bölgeler yazar - temiz bir geri
// alma, kesinti yok ve işten önce değil sonra gelen bir ret.
//
// CelikPanel'in bölgelerini ve direktiflerini doğru view'in içine koymak asıl
// özelliktir; bölge yerleşimi işine aittir ve burada değildir. Burada olan,
// dürüst rettir: sunucu, hazırlık toplanırken view arar ve bulursa önizleme,
// daha bir önizleme belirteci yokken, dosyası ve satırıyla adlandırılarak
// engellenir. Ekran sunucunun ne olduğunu, CelikPanel'in böyle yapılandırılmış
// bir sunucuyu henüz yönetemediğini ve operatörün ne yapabileceğini söyler.
//
// Aynı muamele, CelikPanel'in bütünüyle okuyamadığı bir yapılandırmayı da
// kapsar. Yapılandırmanın dahil ettiği bir dosya okunamadığında "view
// bulunamadı" bir tahmin olurdu; tahmin ise tüm devralmanın kaçınmak için
// kurulduğu şeydir. Farklı sözcüklerle farklı bir engelleyicidir, çünkü
// operatörden farklı bir şey ister.

// dnsEngineViewsBlocker: this server's DNS configuration declares views.
// dnsEngineConfigUnreadableBlocker: part of it could not be read, so views
// cannot be ruled out. Both are machine codes; the browser owns the words.
//
// dnsEngineViewsBlocker: bu sunucunun DNS yapılandırması view bildiriyor.
// dnsEngineConfigUnreadableBlocker: bir kısmı okunamadı, dolayısıyla view'ler
// elenemiyor. İkisi de makine kodudur; sözcükler tarayıcınındır.
const (
	dnsEngineViewsBlocker            = "dns_views_unadoptable"
	dnsEngineConfigUnreadableBlocker = "dns_config_unreadable"
)

// dnsEngineViewFinding is what the screen needs to name the refusal: which of
// the two it is, and the one place in the operator's own files to look.
//
// dnsEngineViewFinding, ekranın reddi adlandırmak için ihtiyaç duyduğudur:
// ikisinden hangisi olduğu ve operatörün kendi dosyalarında bakılacak tek yer.
type dnsEngineViewFinding struct {
	Finding string `json:"finding"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}

func dnsEngineViewFindingOf(
	runtime transport.DNSBackendRuntimeState,
) *dnsEngineViewFinding {
	if runtime.ForeignViews == nil {
		return nil
	}
	return &dnsEngineViewFinding{
		Finding: runtime.ForeignViews.Finding,
		File:    runtime.ForeignViews.File,
		Line:    runtime.ForeignViews.Line,
	}
}

func dnsEngineViewBlocker(finding *dnsEngineViewFinding) string {
	if finding == nil {
		return ""
	}
	switch finding.Finding {
	case transport.DNSForeignViewDeclared:
		return dnsEngineViewsBlocker
	case transport.DNSForeignViewUnreadable:
		return dnsEngineConfigUnreadableBlocker
	default:
		return ""
	}
}

// validateDNSForeignEngineViews keeps the host's answer inside the shape the
// panel promised the browser: one of the two findings, an absolute file path,
// and a real line. Anything else makes readiness unavailable rather than
// reaching a screen as a half-understood fact - and, because this one decides
// whether a takeover may happen at all, a malformed answer must never be able
// to read as "no views".
//
// validateDNSForeignEngineViews, sunucunun cevabını panelin tarayıcıya söz
// verdiği biçimin içinde tutar: iki bulgudan biri, mutlak bir dosya yolu ve
// gerçek bir satır. Başka her şey, yarım anlaşılmış bir olgu olarak bir ekrana
// ulaşmak yerine hazırlığı erişilemez kılar - ve bir devralmanın hiç olup
// olmayacağına karar veren bu cevap olduğu için, bozuk bir cevap asla "view
// yok" diye okunamamalıdır.
func validateDNSForeignEngineViews(
	runtime transport.DNSBackendRuntimeState,
) bool {
	views := runtime.ForeignViews
	if views == nil {
		return true
	}
	// Only a BIND that is on this host and is not ours has a configuration to
	// be taken over; a view report for anything else is a contradiction.
	//
	// Yalnız bu sunucuda olan ve bizim olmayan bir BIND'in devralınacak bir
	// yapılandırması vardır; başkası için gelen bir view bildirimi çelişkidir.
	if runtime.Engine != transport.DNSEngineBIND ||
		!runtime.Installed || runtime.Managed {
		return false
	}
	known := false
	for _, finding := range transport.DNSForeignViewFindings {
		if views.Finding == finding {
			known = true
			break
		}
	}
	if !known {
		return false
	}
	if views.Line < 1 || views.Line > 1<<24 {
		return false
	}
	return strings.HasPrefix(views.File, "/") && len(views.File) <= 512 &&
		printableDNSOptionText(views.File)
}
