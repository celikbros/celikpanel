package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Finding out that this server is configured with views, before anything else
// (register R-044).
//
// The takeover reads and replaces four directives in the options block. BIND
// allows the same four inside a `view`, where they take precedence, so a
// `recursion yes;` written in a view would survive a takeover that reported
// recursion as managed - the panel would be claiming a setting it does not
// control. And once any view exists, BIND requires every zone to live inside
// one, so the zones CelikPanel generates would be refused by the configuration
// check: no outage and a clean restore, but a refusal that arrives after the
// work instead of before it.
//
// Placing CelikPanel's zones and directives inside the view that answers for
// them is the real feature, and it belongs with the zone-placement work. This
// file does the honest half: it finds out, before a preview token exists, that
// this server is configured in a way CelikPanel cannot manage yet, and says so
// by name.
//
// It is deliberately the same reader R-042 already built. The traps are the
// ones that reader handles - a `view` inside a comment, inside a quoted string,
// or as part of a longer identifier such as `view-hint` - and they are handled
// the same way, by judging the configuration with its comments and string
// bodies blanked out and by comparing whole identifier tokens rather than
// substrings. The one thing the options reader never had to do is follow an
// `include`, because a view hidden behind one is still a view. Where a file
// cannot be read or an include cannot be followed, that is reported too: not as
// "no views", which would be a guess, but as the fact that the configuration
// could not be read whole, naming the statement to look at.
//
// Bu sunucunun view ile yapılandırıldığını, her şeyden önce öğrenmek
// (defter R-044).
//
// Devralma, seçenek bloğundaki dört direktifi okur ve değiştirir. BIND aynı
// dördünün bir `view` içinde olmasına izin verir; orada üstün gelirler. Yani
// bir view'de yazılmış bir `recursion yes;`, recursion'ı yönetilen diye
// bildiren bir devralmadan sağ çıkardı - panel, denetlemediği bir ayarı iddia
// ediyor olurdu. Üstelik bir view var olduğu anda BIND her bölgenin bir view
// içinde olmasını şart koşar; böylece CelikPanel'in ürettiği bölgeler
// yapılandırma denetiminde reddedilirdi: kesinti yok, temiz bir geri alma var,
// ama ret işten önce değil sonra geliyor.
//
// CelikPanel'in bölgelerini ve direktiflerini, onlar için cevap veren view'in
// içine koymak asıl özelliktir ve bölge yerleşimi işine aittir. Bu dosya dürüst
// yarısını yapar: daha bir önizleme belirteci yokken, bu sunucunun CelikPanel'in
// henüz yönetemeyeceği bir biçimde yapılandırıldığını öğrenir ve bunu adıyla
// söyler.
//
// Bilerek R-042'nin zaten kurduğu okuyucudur. Tuzaklar o okuyucunun ele
// aldıklarıdır - yorum içinde, tırnak içinde ya da `view-hint` gibi daha uzun
// bir tanımlayıcının parçası olarak geçen bir `view` - ve aynı biçimde ele
// alınırlar: yapılandırma, yorumları ve dizge gövdeleri boşaltılmış hâliyle
// yargılanır ve alt dizge değil bütün tanımlayıcı belirteçleri karşılaştırılır.
// Seçenek okuyucusunun hiç yapmak zorunda kalmadığı tek şey bir `include`
// izlemektir; çünkü bir include'un arkasına saklanmış bir view de bir view'dir.
// Bir dosya okunamadığında ya da bir include izlenemediğinde bu da bildirilir:
// bir tahmin olacak "view yok" diye değil, yapılandırmanın bütünüyle
// okunamadığı olgusu olarak, bakılacak deyim adlandırılarak.

const (
	bindViewFindingDeclared   = transport.DNSForeignViewDeclared
	bindViewFindingUnreadable = transport.DNSForeignViewUnreadable
)

// The walk is bounded so a readiness probe can never be turned into work by a
// configuration that includes a thousand files or one enormous one. A bound
// that is reached is not silently ignored: it is reported as an include that
// could not be followed, which is exactly what it is.
//
// Yürüyüş sınırlıdır; böylece bir hazırlık yoklaması, bin dosya dahil eden ya da
// devasa tek bir dosya olan bir yapılandırma tarafından işe çevrilemez.
// Ulaşılan bir sınır sessizce yutulmaz: izlenemeyen bir include olarak
// bildirilir, ki tam olarak odur.
const (
	bindViewScanMaxFiles = 64
	bindViewScanMaxBytes = 4 << 20
)

// bindConfigReader reads one configuration file. It exists so the detection can
// be exercised over a whole configuration tree without a filesystem: on the
// host it is secureReadConfig, under test it is a map.
//
// bindConfigReader tek bir yapılandırma dosyasını okur. Algılamanın bir dosya
// sistemi olmadan tüm bir yapılandırma ağacı üzerinde sınanabilmesi için vardır:
// sunucuda secureReadConfig'tir, testte bir eşlemedir.
type bindConfigReader func(path string) ([]byte, error)

// bindViewInclude is one file still to read, and the statement that named it.
// The statement travels with the file because it, not the file, is what the
// operator can act on when the file cannot be read.
//
// bindViewInclude, hâlâ okunacak bir dosya ve onu adlandıran deyimdir. Deyim
// dosyayla birlikte yol alır; çünkü dosya okunamadığında operatörün üzerinde
// işlem yapabileceği şey dosya değil, o deyimdir.
type bindViewInclude struct {
	Path     string
	FromFile string
	FromLine int
}

// findBINDViewDeclaration walks the configuration from its roots and reports the
// first view it finds, or the first include it could not follow. It reports
// nothing only when it read the whole tree and there were no views.
//
// findBINDViewDeclaration, yapılandırmayı köklerinden yürür ve bulduğu ilk
// view'i ya da izleyemediği ilk include'u bildirir. Hiçbir şey bildirmemesi,
// yalnız tüm ağacı okuduğu ve hiç view olmadığı durumdadır.
func findBINDViewDeclaration(
	roots []string,
	read bindConfigReader,
) (*transport.DNSForeignEngineViews, error) {
	if read == nil {
		return nil, errors.New("BIND view detection requires a configuration reader")
	}
	queue := make([]bindViewInclude, 0, len(roots))
	seen := make(map[string]struct{}, bindViewScanMaxFiles)
	for _, root := range roots {
		if root == "" {
			continue
		}
		if _, already := seen[root]; already {
			continue
		}
		seen[root] = struct{}{}
		queue = append(queue, bindViewInclude{Path: root})
	}
	files, bytesRead := 0, 0
	for index := 0; index < len(queue); index++ {
		entry := queue[index]
		// A root that cannot be read is a probe failure, not a finding: there
		// is no include statement to send the operator to, and the takeover
		// reads the same file for its own reasons and fails there anyway.
		//
		// Okunamayan bir kök, bir bulgu değil bir yoklama hatasıdır: operatörü
		// göndereceğimiz bir include deyimi yoktur ve devralma aynı dosyayı
		// kendi sebepleriyle zaten okur ve orada düşer.
		if entry.FromFile == "" {
			data, err := read(entry.Path)
			if err != nil {
				return nil, fmt.Errorf("read %s: %w", entry.Path, err)
			}
			files++
			bytesRead += len(data)
			if bytesRead > bindViewScanMaxBytes {
				return nil, fmt.Errorf(
					"BIND configuration %s is too large to read for view detection",
					entry.Path,
				)
			}
			finding, includes := scanBINDConfigForViews(string(data), entry.Path)
			if finding != nil {
				return finding, nil
			}
			queue = appendBINDViewIncludes(queue, seen, includes)
			continue
		}
		if files >= bindViewScanMaxFiles || bytesRead > bindViewScanMaxBytes {
			return unreadableBINDViewFinding(entry), nil
		}
		data, err := read(entry.Path)
		if err != nil {
			return unreadableBINDViewFinding(entry), nil
		}
		files++
		bytesRead += len(data)
		if bytesRead > bindViewScanMaxBytes {
			return unreadableBINDViewFinding(entry), nil
		}
		finding, includes := scanBINDConfigForViews(string(data), entry.Path)
		if finding != nil {
			return finding, nil
		}
		queue = appendBINDViewIncludes(queue, seen, includes)
	}
	return nil, nil
}

func unreadableBINDViewFinding(
	entry bindViewInclude,
) *transport.DNSForeignEngineViews {
	return &transport.DNSForeignEngineViews{
		Finding: bindViewFindingUnreadable,
		File:    entry.FromFile,
		Line:    entry.FromLine,
	}
}

// appendBINDViewIncludes queues the files a configuration includes, once each.
// A configuration that includes itself, directly or through a ring, is read
// once and does not spin.
//
// appendBINDViewIncludes, bir yapılandırmanın dahil ettiği dosyaları her biri
// bir kez olmak üzere sıraya koyar. Kendisini doğrudan ya da bir halka üzerinden
// dahil eden bir yapılandırma bir kez okunur ve dönüp durmaz.
func appendBINDViewIncludes(
	queue []bindViewInclude,
	seen map[string]struct{},
	includes []bindViewInclude,
) []bindViewInclude {
	for _, include := range includes {
		if _, already := seen[include.Path]; already {
			continue
		}
		seen[include.Path] = struct{}{}
		queue = append(queue, include)
	}
	return queue
}

// scanBINDConfigForViews reads one file: the first top-level `view` declaration
// it contains, and the files it includes at top level.
//
// Only the top level is examined, and that is not a shortcut. BIND permits a
// view only as a top-level statement, so a `view` token anywhere else is not a
// view declaration; and an `include` written inside a block splices its text
// into that block, where a view could not legally appear either. Reading the
// whole tree at every depth would find the same views and invent refusals on
// text that is not a view at all.
//
// scanBINDConfigForViews tek bir dosyayı okur: içerdiği ilk üst düzey `view`
// bildirimini ve üst düzeyde dahil ettiği dosyaları.
//
// Yalnız üst düzeye bakılır ve bu bir kestirme değildir. BIND bir view'e ancak
// üst düzey bir deyim olarak izin verir; dolayısıyla başka bir yerdeki bir
// `view` belirteci bir view bildirimi değildir. Bir bloğun içine yazılmış bir
// `include` ise metnini o bloğa ekler ve orada da bir view yasal olarak yer
// alamaz. Her derinlikte tüm ağacı okumak aynı view'leri bulur ve hiç view
// olmayan metinlere ret uydururdu.
func scanBINDConfigForViews(
	config, file string,
) (*transport.DNSForeignEngineViews, []bindViewInclude) {
	clean := stripBINDCommentsAndStrings(config)
	includes := []bindViewInclude{}
	depth := 0
	statementHead := true
	for index := 0; index < len(clean); {
		character := clean[index]
		switch {
		case character == '{':
			depth++
			index++
			statementHead = true
		case character == '}':
			if depth > 0 {
				depth--
			}
			index++
			statementHead = false
		case character == ';':
			index++
			statementHead = true
		case character == ' ' || character == '\t' ||
			character == '\n' || character == '\r':
			index++
		case bindIdentifierStart(character):
			start := index
			for index < len(clean) && bindIdentifierPart(clean[index]) {
				index++
			}
			name := clean[start:index]
			if depth != 0 || !statementHead {
				statementHead = false
				continue
			}
			switch name {
			case "view":
				if bindViewDeclarationFollows(clean, index) {
					return &transport.DNSForeignEngineViews{
						Finding: bindViewFindingDeclared,
						File:    file,
						Line:    bindConfigLineAt(config, start),
					}, nil
				}
			case "include":
				includes = append(includes, bindViewInclude{
					Path:     bindIncludePath(config, index),
					FromFile: file,
					FromLine: bindConfigLineAt(config, start),
				})
			}
			statementHead = false
		default:
			index++
			statementHead = false
		}
	}
	return nil, includes
}

// bindViewDeclarationFollows proves that the `view` token just read opens a
// view block, rather than being a word that happens to be spelled that way. A
// declaration is `view` followed by a name - quoted, which the reader has
// already blanked to spaces, or bare - an optional class, and then the block.
//
// bindViewDeclarationFollows, az önce okunan `view` belirtecinin öylece o
// şekilde yazılmış bir sözcük değil, bir view bloğu açtığını kanıtlar. Bir
// bildirim, `view` ardından bir ad - okuyucunun zaten boşluğa çevirdiği tırnaklı
// hâli ya da tırnaksız - isteğe bağlı bir sınıf ve sonra bloktur.
func bindViewDeclarationFollows(clean string, from int) bool {
	index := from
	for names := 0; names <= 2; names++ {
		for index < len(clean) && (clean[index] == ' ' || clean[index] == '\t' ||
			clean[index] == '\n' || clean[index] == '\r') {
			index++
		}
		if index >= len(clean) {
			return false
		}
		if clean[index] == '{' {
			return true
		}
		if !bindIdentifierStart(clean[index]) {
			return false
		}
		for index < len(clean) && bindIdentifierPart(clean[index]) {
			index++
		}
	}
	return false
}

// bindIncludePath reads the quoted filename of an include statement out of the
// original text. It cannot use the blanked copy the rest of this file judges by:
// blanking a string erases its quotes too, so the path would be invisible
// there. It therefore steps over whitespace and comments itself, which is the
// one place in this file where a comment has to be recognised rather than
// already removed.
//
// An include whose path is not one absolute quoted filename returns "", and the
// walk reports it as a file it could not follow: it is not going to guess at
// what a relative path resolves to on this host, and a guess is what this whole
// refusal exists to avoid.
//
// bindIncludePath, bir include deyiminin tırnaklı dosya adını özgün metinden
// okur. Bu dosyanın geri kalanının yargıladığı boşaltılmış kopyayı kullanamaz:
// bir dizgeyi boşaltmak tırnaklarını da siler, dolayısıyla yol orada görünmez
// olurdu. Bu yüzden boşlukları ve yorumları kendisi atlar; bu, bu dosyada bir
// yorumun zaten kaldırılmış olmak yerine tanınması gereken tek yerdir.
//
// Yolu tek, mutlak, tırnaklı bir dosya adı olmayan bir include "" döndürür ve
// yürüyüş bunu izleyemediği bir dosya olarak bildirir: göreli bir yolun bu
// sunucuda neye karşılık geldiğini tahmin etmeyecektir ve tüm bu reddin var
// olma sebebi zaten tahmindir.
func bindIncludePath(config string, from int) string {
	index := bindSkipTrivia(config, from)
	if index >= len(config) || config[index] != '"' {
		return ""
	}
	index++
	var builder strings.Builder
	for index < len(config) {
		character := config[index]
		if character == '\\' && index+1 < len(config) {
			builder.WriteByte(config[index+1])
			index += 2
			continue
		}
		if character == '"' {
			path := builder.String()
			if !strings.HasPrefix(path, "/") ||
				strings.ContainsAny(path, "\x00\n\r") ||
				len(path) > 512 {
				return ""
			}
			return path
		}
		builder.WriteByte(character)
		index++
	}
	return ""
}

// bindSkipTrivia advances past whitespace and configuration comments in the
// original text.
//
// bindSkipTrivia, özgün metinde boşlukları ve yapılandırma yorumlarını atlar.
func bindSkipTrivia(config string, from int) int {
	index := from
	for index < len(config) {
		character := config[index]
		switch {
		case character == ' ' || character == '\t' ||
			character == '\n' || character == '\r':
			index++
		case character == '#' ||
			(character == '/' && index+1 < len(config) && config[index+1] == '/'):
			for index < len(config) && config[index] != '\n' {
				index++
			}
		case character == '/' && index+1 < len(config) && config[index+1] == '*':
			closing := strings.Index(config[index+2:], "*/")
			if closing < 0 {
				return len(config)
			}
			index += closing + 4
		default:
			return index
		}
	}
	return index
}

// reportForeignBINDViews is the readiness surface. The panel asks the host
// whether a takeover is even possible on this configuration, and the host
// answers with a fact and a place to look, or with nothing at all.
//
// Both roots are walked, and the main configuration first: on Debian the
// options live in a file the main configuration includes, and on Red Hat both
// names are the same file. Reading the pair covers a host where the takeover's
// own options file is not reachable from the main one.
//
// reportForeignBINDViews, hazırlık yüzeyidir. Panel sunucuya bu yapılandırmada
// bir devralmanın mümkün olup olmadığını sorar; sunucu bir olgu ve bakılacak bir
// yerle ya da hiçbir şeyle cevap verir.
//
// İki kök de yürünür, önce ana yapılandırma: Debian'da seçenekler ana
// yapılandırmanın dahil ettiği bir dosyadadır, Red Hat'te iki ad da aynı
// dosyadır. Çifti okumak, devralmanın kendi seçenek dosyasının ana dosyadan
// erişilemediği bir sunucuyu da kapsar.
func reportForeignBINDViews(
	layout bindHostLayout,
) (*transport.DNSForeignEngineViews, error) {
	return findBINDViewDeclaration(
		[]string{layout.MainConfig, layout.OptionsConfig},
		secureReadConfig,
	)
}
