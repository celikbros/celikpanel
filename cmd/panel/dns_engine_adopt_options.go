package main

import (
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The difference a takeover makes to the directives an administrator wrote
// (register R-042).
//
// The takeover already told the operator, in plain words, that this server's
// configuration becomes CelikPanel's. It did not say what that costs, because
// it did not know: the four directives CelikPanel manages - recursion,
// allow-recursion, allow-query-cache, allow-transfer - were a refusal, not a
// fact, so there was nothing to show. Now the host reports them, and the
// preview carries them as structured data: the directive, the value the server
// has today, and the value CelikPanel will set. The browser renders that as a
// list, under the acknowledgement the operator already gives. There is no
// second acknowledgement, because there is no second decision - the operator is
// consenting to the same thing, now knowing what it means.
//
// A value that already equals what CelikPanel would set is said to be
// unchanged rather than listed as a change. "recursion no;" on an
// authoritative server is the common case, and calling it a change would make
// the list read as a loss it is not.
//
// A directive the host could not read as its own statement travels with a
// refusal code. It becomes a named blocker rather than a failure at commit
// time, because the operator can act on a directive and a line, and cannot act
// on "the change failed".
//
// Bir devralmanın, bir yöneticinin yazdığı direktiflerde yaptığı fark
// (defter R-042).
//
// Devralma operatöre, düz sözlerle, bu sunucunun yapılandırmasının
// CelikPanel'inki olacağını zaten söylüyordu. Bunun neye mal olduğunu
// söylemiyordu, çünkü bilmiyordu: CelikPanel'in yönettiği dört direktif -
// recursion, allow-recursion, allow-query-cache, allow-transfer - bir olgu
// değil bir retti; gösterilecek bir şey yoktu. Artık sunucu onları bildiriyor
// ve önizleme onları yapılandırılmış veri olarak taşıyor: direktif, sunucunun
// bugünkü değeri ve CelikPanel'in koyacağı değer. Tarayıcı bunu, operatörün
// zaten verdiği onayın altında bir liste olarak çizer. İkinci bir onay yoktur,
// çünkü ikinci bir karar yoktur - operatör aynı şeye rıza gösterir, artık ne
// demek olduğunu bilerek.
//
// CelikPanel'in koyacağıyla zaten eşit olan bir değer, bir değişiklik olarak
// listelenmez, değişmediği söylenir. Yetkili bir sunucuda "recursion no;"
// olağan durumdur ve ona değişiklik demek, listeyi olmadığı bir kayıp gibi
// okuturdu.
//
// Sunucunun kendi deyimi olarak okuyamadığı bir direktif, bir ret koduyla yol
// alır. Commit anında bir başarısızlık yerine adı konmuş bir engelleyici olur;
// çünkü operatör bir direktif ile bir satır hakkında bir şey yapabilir, "değişim
// başarısız oldu" hakkında yapamaz.
type dnsEngineAdoptedDirective struct {
	Directive   string `json:"directive"`
	Found       string `json:"found"`
	Replacement string `json:"replacement"`
	// Unchanged says the server already sets exactly what CelikPanel would set.
	//
	// Unchanged, sunucunun CelikPanel'in koyacağının birebir aynısını zaten
	// koyduğunu söyler.
	Unchanged bool   `json:"unchanged"`
	File      string `json:"file"`
	Line      int    `json:"line"`
	Refusal   string `json:"refusal,omitempty"`
}

// dnsEngineAdoptionOptionsBlocker is raised when the host reported a directive
// the takeover cannot replace. The commit is refused before it starts, and the
// screen names the directive, the file and the line instead of the agent
// refusing halfway through with a message about a file the operator never
// opened.
//
// dnsEngineAdoptionOptionsBlocker, sunucu devralmanın değiştiremeyeceği bir
// direktif bildirdiğinde kaldırılır. Commit başlamadan reddedilir ve ekran,
// agent'ın yarı yolda operatörün hiç açmadığı bir dosya hakkında bir mesajla
// reddetmesi yerine direktifi, dosyayı ve satırı adlandırır.
const dnsEngineAdoptionOptionsBlocker = "dns_options_unadoptable"

func dnsEngineAdoptedDirectives(
	runtime transport.DNSBackendRuntimeState,
) []dnsEngineAdoptedDirective {
	if len(runtime.ForeignOptions) == 0 {
		return nil
	}
	directives := make([]dnsEngineAdoptedDirective, 0, len(runtime.ForeignOptions))
	for _, option := range runtime.ForeignOptions {
		directives = append(directives, dnsEngineAdoptedDirective{
			Directive: option.Directive, Found: option.Found,
			Replacement: option.Replacement,
			Unchanged:   option.Refusal == "" && option.Found == option.Replacement,
			File:        option.File, Line: option.Line, Refusal: option.Refusal,
		})
	}
	return directives
}

func dnsEngineAdoptionRefused(directives []dnsEngineAdoptedDirective) bool {
	for _, directive := range directives {
		if directive.Refusal != "" {
			return true
		}
	}
	return false
}

// validateDNSForeignEngineOptions keeps the host's report inside the shape the
// panel promised the browser: known directives, known refusal codes, an
// absolute file path, a real line, and a value that is one bounded printable
// line of the operator's own text. Anything else makes readiness unavailable
// rather than reaching a screen as a half-understood fact.
//
// validateDNSForeignEngineOptions, sunucunun bildirimini panelin tarayıcıya söz
// verdiği biçimin içinde tutar: bilinen direktifler, bilinen ret kodları,
// mutlak bir dosya yolu, gerçek bir satır ve operatörün kendi metninin tek,
// sınırlı, yazdırılabilir bir satırı. Başka her şey, yarım anlaşılmış bir olgu
// olarak bir ekrana ulaşmak yerine hazırlığı erişilemez kılar.
func validateDNSForeignEngineOptions(
	runtime transport.DNSBackendRuntimeState,
) bool {
	if len(runtime.ForeignOptions) == 0 {
		return true
	}
	// Only a BIND that is on this host and is not ours has anything to take
	// over from; a report for anything else is a contradiction, not a fact.
	//
	// Yalnız bu sunucuda olan ve bizim olmayan bir BIND'in devralınacak bir
	// şeyi vardır; başkası için gelen bir bildirim olgu değil çelişkidir.
	if runtime.Engine != transport.DNSEngineBIND ||
		!runtime.Installed || runtime.Managed ||
		len(runtime.ForeignOptions) > 32 {
		return false
	}
	directives := make(map[string]struct{}, len(transport.DNSManagedBINDOptionDirectives))
	for _, directive := range transport.DNSManagedBINDOptionDirectives {
		directives[directive] = struct{}{}
	}
	refusals := make(map[string]struct{}, len(transport.DNSForeignOptionRefusals))
	for _, refusal := range transport.DNSForeignOptionRefusals {
		refusals[refusal] = struct{}{}
	}
	for _, option := range runtime.ForeignOptions {
		if _, known := directives[option.Directive]; !known {
			return false
		}
		if option.Refusal != "" {
			if _, known := refusals[option.Refusal]; !known {
				return false
			}
		}
		if option.Line < 1 || option.Line > 1<<24 {
			return false
		}
		if !strings.HasPrefix(option.File, "/") || len(option.File) > 512 ||
			!printableDNSOptionText(option.File) {
			return false
		}
		if len(option.Found) > 200 || len(option.Replacement) > 200 ||
			!printableDNSOptionText(option.Found) ||
			!printableDNSOptionText(option.Replacement) {
			return false
		}
		// A directive that can be taken over has a value and a replacement; one
		// that cannot has neither, because the host could not read it.
		//
		// Devralınabilen bir direktifin değeri ve yerine konacağı vardır;
		// devralınamayanın ikisi de yoktur, çünkü sunucu onu okuyamamıştır.
		if option.Refusal == "" && (option.Found == "" || option.Replacement == "") {
			return false
		}
		if option.Refusal != "" && option.Found != "" {
			return false
		}
	}
	return true
}

func printableDNSOptionText(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] > 0x7e {
			return false
		}
	}
	return true
}
