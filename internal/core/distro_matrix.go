package core

import (
	"fmt"
	"sort"
	"strings"
)

// RenderDistroMatrix renders "what do we offer on which distro" as Markdown,
// entirely from the catalogue in this package. The document is committed to
// docs/ so the answer is readable without running anything, and a guard test
// fails the build the moment the committed copy no longer matches the
// catalogue — the operator asked for this list (25 Jul: "hangi dağıtımda
// neleri sunacağımıza ilişkin bir listemiz olmalı"), and a hand-written copy
// would be the two-lists-disagreeing failure this project keeps burying
// (scanner vs catalogue, nav vs middleware, capabilities vs services page).
// The catalogue is the single owner; documents are VIEWS of it.
//
// RenderDistroMatrix, "hangi dağıtımda ne sunuyoruz" sorusunu tümüyle bu
// paketteki katalogdan Markdown olarak çizer. Belge docs/ altına commit'lenir
// ki cevap hiçbir şey çalıştırmadan okunabilsin; bekçi test, commit'li kopya
// katalogla uyuşmadığı anda derlemeyi düşürür — bu listeyi operatör istedi
// (25 Tem: "hangi dağıtımda neleri sunacağımıza ilişkin bir listemiz olmalı")
// ve elle yazılmış bir kopya, bu projenin defalarca gömdüğü iki-listenin-
// anlaşamaması arızasının ta kendisi olurdu (tarayıcı↔katalog, nav↔middleware,
// capabilities↔servis sayfası). Katalog tek sahiptir; belgeler onun GÖRÜNÜMÜdür.
func RenderDistroMatrix(lang string) string {
	tr := lang == "tr"

	// Family columns come from the DATA: the first dnf entry added to the
	// catalogue grows the table by itself, no edit here.
	// Aile sütunları VERİDEN gelir: kataloğa eklenen ilk dnf girdisi tabloyu
	// kendiliğinden büyütür, burada düzenleme gerekmez.
	famSet := map[string]bool{}
	for i := range ManagedServices {
		for f := range ManagedServices[i].Packages {
			famSet[f] = true
		}
	}
	preferred := []string{"apt", "dnf", "pacman", "zypper", "apk"}
	families := []string{}
	for _, f := range preferred {
		if famSet[f] {
			families = append(families, f)
			delete(famSet, f)
		}
	}
	rest := make([]string, 0, len(famSet))
	for f := range famSet {
		rest = append(rest, f)
	}
	sort.Strings(rest)
	families = append(families, rest...)

	famLabel := map[string]string{
		"apt":    "Debian/Ubuntu (apt)",
		"dnf":    "RHEL/Fedora (dnf)",
		"pacman": "Arch (pacman)",
		"zypper": "openSUSE (zypper)",
		"apk":    "Alpine (apk)",
	}

	kindLabel := map[ServiceKind]string{
		KindService: "Service",
		KindRuntime: "Runtime",
		KindTool:    "Tool",
	}
	if tr {
		kindLabel = map[ServiceKind]string{
			KindService: "Servis",
			KindRuntime: "Çalışma ortamı",
			KindTool:    "Araç",
		}
	}

	// Seat and role names, matching the UI's wording.
	// Koltuk ve rol adları, arayüzdeki dille aynı.
	roleLabel := map[string]string{
		"web-server":  "web server",
		"dns-server":  "DNS server",
		"smtp-server": "SMTP server",
		"imap-server": "IMAP server",
		"spam-filter": "spam filter",
	}
	if tr {
		roleLabel = map[string]string{
			"web-server":  "web sunucusu",
			"dns-server":  "DNS sunucusu",
			"smtp-server": "SMTP sunucusu",
			"imap-server": "IMAP sunucusu",
			"spam-filter": "spam filtresi",
		}
	}
	role := func(token string) string {
		if l, ok := roleLabel[token]; ok {
			return l
		}
		// A Requires token is either a seat name or a component id; print the
		// component's display name, never the raw id.
		// Requires belirteci ya koltuk adı ya bileşen id'sidir; ham id değil
		// bileşenin görünen adı basılır.
		if svc := GetManagedServiceByID(token); svc != nil {
			return svc.Name
		}
		return token
	}

	catLabel := map[string]string{
		"web":        "Web",
		"database":   "Database",
		"email":      "E-mail",
		"security":   "Security",
		"dns":        "DNS",
		"cache":      "Cache",
		"ftp":        "FTP",
		"monitoring": "Monitoring",
	}
	if tr {
		catLabel = map[string]string{
			"web":        "Web",
			"database":   "Veritabanı",
			"email":      "E-posta",
			"security":   "Güvenlik",
			"dns":        "DNS",
			"cache":      "Önbellek",
			"ftp":        "FTP",
			"monitoring": "İzleme",
		}
	}
	cat := func(c string) string {
		if l, ok := catLabel[c]; ok {
			return l
		}
		return c
	}

	var b strings.Builder
	w := func(format string, a ...any) { fmt.Fprintf(&b, format+"\n", a...) }

	if tr {
		w("# Hangi dağıtımda ne sunuluyor?")
		w("")
		w("<!-- BU DOSYA ÜRETİLİR — elle düzenlemeyin. -->")
		w("<!-- Kaynak: internal/core/managed_services.go · Üretmek için: go run ./tools/gen-distro-matrix -->")
		w("<!-- Bekçi: internal/core/distro_matrix_test.go (katalog değişip bu dosya üretilmezse test düşer) -->")
		w("")
		w("Bu liste elle yazılmaz; bileşen kataloğundan üretilir. Panelin Bileşenler")
		w("sayfası aynı kataloğu okur — yani bu belge ile ekran birbirinden ayrı düşemez.")
		w("")
		w("Kurallar:")
		w("")
		w("- **Boş hücre (—) bir eksik değil, bir sözdür:** o dağıtımda \"kurulunca çalışır\"")
		w("  garantisi verilemiyorsa satır bilerek sunulmaz. Yarım kurulum kırıktır.")
		w("- **Koltuk:** aynı koltuktaki bileşenler aynı işi yapar; aynı anda yalnız biri")
		w("  kurulabilir (ör. iki web sunucusu 80 portunu paylaşamaz). Diğerini seçmek")
		w("  ekleme değil, değiştirmedir.")
		w("- **Gerekenler** ürünü değil ROLÜ adlandırır: \"SMTP sunucusu\" diyen bir satırı,")
		w("  o koltuğun kurulu herhangi bir üyesi tatmin eder.")
	} else {
		w("# What is offered on which distro?")
		w("")
		w("<!-- THIS FILE IS GENERATED — do not edit by hand. -->")
		w("<!-- Source: internal/core/managed_services.go · Regenerate: go run ./tools/gen-distro-matrix -->")
		w("<!-- Guard: internal/core/distro_matrix_test.go (the test fails when the catalogue changes without regenerating) -->")
		w("")
		w("This list is not written by hand; it is generated from the component catalogue.")
		w("The panel's Components page reads the same catalogue — this document and the")
		w("screen cannot drift apart.")
		w("")
		w("Rules:")
		w("")
		w("- **An empty cell (—) is a promise, not a gap:** when \"installed means working\"")
		w("  cannot be guaranteed on a distro, the row is deliberately not offered there.")
		w("  A half-install is broken.")
		w("- **Seat:** components in the same seat do the same job; only one can be")
		w("  installed at a time (two web servers cannot share port 80). Choosing the")
		w("  other one is a swap, not an addition.")
		w("- **Needs** names a ROLE, not a product: a row that needs \"an SMTP server\" is")
		w("  satisfied by any installed member of that seat.")
	}
	w("")

	// Split: distro-packaged vs portable (D-018). An empty Packages map is not
	// a gap — it is the portable install path (official release, same on every
	// distro), and listing it under per-distro columns would print a row of
	// misleading dashes.
	// Ayrım: dağıtım-paketli ↔ taşınabilir (D-018). Boş Packages haritası eksik
	// değildir — taşınabilir kurulum yoludur (resmi sürüm, her dağıtımda aynı);
	// onu dağıtım sütunlarında listelemek yanıltıcı bir tire dizisi basardı.
	if tr {
		w("## Dağıtım paketiyle kurulanlar")
	} else {
		w("## Installed from distro packages")
	}
	w("")

	header := "| " + or(tr, "Bileşen", "Component") + " | " + or(tr, "Tür", "Kind") + " |"
	sep := "|---|---|"
	for _, f := range families {
		header += " " + famLabel[f] + " |"
		sep += "---|"
	}
	header += " " + or(tr, "Koltuk", "Seat") + " | " + or(tr, "Gerekenler", "Needs") + " |"
	sep += "---|---|"

	currentCat := ""
	for i := range ManagedServices {
		svc := &ManagedServices[i]
		if len(svc.Packages) == 0 {
			continue
		}
		if svc.Category != currentCat {
			currentCat = svc.Category
			w("### %s", cat(currentCat))
			w("")
			w("%s", header)
			w("%s", sep)
		}
		row := fmt.Sprintf("| %s %s | %s |", svc.Icon, svc.Name, kindLabel[svc.Kind])
		for _, f := range families {
			pkgs := svc.Packages[f]
			cell := "—"
			if svc.InstallDisabledReason == "" && len(pkgs) > 0 {
				cell = "`" + strings.Join(pkgs, "` `") + "`"
				if svc.Repo != nil && f == "apt" {
					// Vendor repos are an apt mechanism today (Sury, PGDG):
					// that is where version CHOICE lives.
					// Vendor depoları bugün apt mekanizması (Sury, PGDG):
					// sürüm SEÇİMİ orada yaşar.
					cell += " · " + or(tr, "sürüm seçimi: "+svc.Repo.Name, "version choice: "+svc.Repo.Name)
				}
			}
			row += " " + cell + " |"
		}
		seat := "—"
		if svc.ConflictGroup != "" {
			seat = role(svc.ConflictGroup)
		}
		needs := "—"
		if len(svc.Requires) > 0 {
			names := make([]string, len(svc.Requires))
			for j, r := range svc.Requires {
				names[j] = role(r)
			}
			needs = strings.Join(names, ", ")
		}
		row += " " + seat + " | " + needs + " |"
		w("%s", row)
		// Blank line after each category's last row is added lazily by the
		// next header; add one at the very end below.
		if i+1 < len(ManagedServices) {
			next := nextPackaged(i + 1)
			if next == -1 || ManagedServices[next].Category != currentCat {
				w("")
			}
		} else {
			w("")
		}
	}

	if tr {
		w("## Resmi sürümden kurulanlar (her dağıtımda aynı)")
		w("")
		w("Bu bileşenler dağıtım paketine hiç bağlanmaz: panel, üreticinin yayınladığı")
		w("sürümü SHA-256 doğrulamasıyla indirir ve her Linux'ta aynı yoldan kurar")
		w("(D-018). Dağıtım sütunu yoktur çünkü cevap her sütunda aynıdır: evet.")
	} else {
		w("## Installed from the official release (identical on every distro)")
		w("")
		w("These components never bind to a distro package: the panel downloads the")
		w("vendor's release, verifies its SHA-256 and installs it the same way on every")
		w("Linux (D-018). There is no distro column because the answer is the same in")
		w("every column: yes.")
	}
	w("")
	w("| %s | %s | %s | %s |", or(tr, "Bileşen", "Component"), or(tr, "Tür", "Kind"), or(tr, "Koltuk", "Seat"), or(tr, "Gerekenler", "Needs"))
	w("|---|---|---|---|")
	for i := range ManagedServices {
		svc := &ManagedServices[i]
		if len(svc.Packages) > 0 || svc.InstallDisabledReason != "" {
			continue
		}
		seat := "—"
		if svc.ConflictGroup != "" {
			seat = role(svc.ConflictGroup)
		}
		needs := "—"
		if len(svc.Requires) > 0 {
			names := make([]string, len(svc.Requires))
			for j, r := range svc.Requires {
				names[j] = role(r)
			}
			needs = strings.Join(names, ", ")
		}
		w("| %s %s | %s | %s | %s |", svc.Icon, svc.Name, kindLabel[svc.Kind], seat, needs)
	}
	w("")

	return b.String()
}

// nextPackaged returns the index of the next catalogue entry that has a
// Packages map, or -1. / nextPackaged, Packages haritası olan bir sonraki
// katalog girdisinin dizinini döndürür; yoksa -1.
func nextPackaged(from int) int {
	for i := from; i < len(ManagedServices); i++ {
		if len(ManagedServices[i].Packages) > 0 {
			return i
		}
	}
	return -1
}

// or picks by language without repeating ternary-less Go if/else at every call
// site. / or, her çağrı yerinde if/else tekrarlamadan dile göre seçer.
func or(tr bool, trText, enText string) string {
	if tr {
		return trText
	}
	return enText
}
