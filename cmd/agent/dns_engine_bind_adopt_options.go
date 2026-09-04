package main

import (
	"errors"
	"fmt"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Taking over the directives an administrator wrote (register R-042).
//
// CelikPanel's BIND generation owns four options directives: recursion,
// allow-recursion, allow-query-cache and allow-transfer. Until now the
// generation refused outright when the server's own options block already
// defined any of them outside CelikPanel's markers. The instinct was right -
// the product must not silently overwrite a directive an administrator set -
// but the conclusion refused the very host the takeover exists for: an
// authoritative BIND configured by hand almost always carries "recursion no;".
// So the takeover worked on a stock server and was refused on a real one, with
// a message about ownership markers that told the operator nothing to do.
//
// The answer is not to relax the refusal for everybody. It is to read those
// directives as part of what the takeover is replacing: the preview shows the
// operator the value found and the value CelikPanel will set, for each one, and
// the takeover takes them under the acknowledgement that already exists. On
// every other path - a switch, a reinstall, a pair reconfiguration - the
// refusal stands exactly as written, because nobody consented to anything
// there.
//
// A refusal survives only where it is honest. A directive this file cannot read
// as a statement of its own, or cannot find the end of, is named: the
// directive, the file, the line, and what the operator can do about it.
//
// Bir yöneticinin yazdığı direktifleri devralmak (defter R-042).
//
// CelikPanel'in BIND nesli dört seçenek direktifinin sahibidir: recursion,
// allow-recursion, allow-query-cache ve allow-transfer. Bugüne kadar nesil,
// sunucunun kendi seçenek bloğu bunlardan birini CelikPanel'in işaretleri
// dışında zaten tanımlıyorsa doğrudan reddediyordu. Sezgi doğruydu - ürün, bir
// yöneticinin koyduğu direktifin üzerinden sessizce geçmemeli - ama sonuç,
// devralmanın var olma sebebi olan sunucuyu reddediyordu: elle yapılandırılmış
// yetkili bir BIND neredeyse her zaman "recursion no;" taşır. Böylece devralma
// stok bir sunucuda çalışıyor, gerçeğinde reddediliyordu; üstelik operatöre ne
// yapacağını söylemeyen, sahiplik işaretlerinden bahseden bir mesajla.
//
// Cevap, reddi herkes için gevşetmek değildir. Cevap, o direktifleri
// devralmanın değiştirdiği şeyin parçası olarak okumaktır: önizleme operatöre
// her biri için bulunan değeri ve CelikPanel'in koyacağı değeri gösterir ve
// devralma onları zaten var olan onayın altında alır. Diğer her yolda - geçiş,
// yeniden kurulum, eş yeniden yapılandırması - ret yazıldığı gibi durur; çünkü
// orada kimse hiçbir şeye rıza göstermemiştir.
//
// Bir ret, yalnız dürüst olduğu yerde yaşar. Bu dosyanın kendi başına bir deyim
// olarak okuyamadığı ya da sonunu bulamadığı bir direktif adıyla anılır:
// direktif, dosya, satır ve operatörün ne yapabileceği.

// bindManagedOptionDirectives is the one list of directives CelikPanel's
// options block owns. Every refusal, every capture and the generated block
// itself derive from it, so "what the panel manages" cannot mean two different
// things in two places.
//
// bindManagedOptionDirectives, CelikPanel'in seçenek bloğunun sahibi olduğu
// direktiflerin tek listesidir. Her ret, her yakalama ve üretilen bloğun
// kendisi ondan türer; böylece "panelin yönettiği" iki yerde iki ayrı şey
// anlamına gelemez.
var bindManagedOptionDirectives = transport.DNSManagedBINDOptionDirectives

// Refusal codes. They are machine codes, never operator text: the panel and the
// browser turn them into words, in the operator's own language.
//
// Ret kodları. Bunlar makine kodlarıdır, asla operatör metni değildir; panel ve
// tarayıcı onları operatörün kendi dilinde sözcüklere çevirir.
const (
	bindOptionRefusalNestedScope   = transport.DNSForeignOptionNestedScope
	bindOptionRefusalNotAStatement = transport.DNSForeignOptionNotAStatement
	bindOptionRefusalUnterminated  = transport.DNSForeignOptionUnterminated
)

// bindForeignOptionDirective is one directive this server's own options block
// sets that CelikPanel's generation also sets: what it says today, what
// CelikPanel will make it say, and where it is written. Refusal is empty when
// the takeover can replace it, and a machine code when it cannot.
//
// bindForeignOptionDirective, bu sunucunun kendi seçenek bloğunun koyduğu ve
// CelikPanel'in neslinin de koyduğu bir direktiftir: bugün ne diyor, CelikPanel
// ne dedirtecek ve nerede yazılı. Devralma onu değiştirebiliyorsa Refusal
// boştur, değiştiremiyorsa bir makine kodudur.
type bindForeignOptionDirective struct {
	Directive   string
	Found       string
	Replacement string
	File        string
	Line        int
	Refusal     string

	start int
	end   int
}

func (directive bindForeignOptionDirective) adoptable() bool {
	return directive.Refusal == ""
}

// managedBINDOptionAssignments is the exact directive/value set the managed
// block writes, in the order it writes them. The block is built from it and the
// preview's "CelikPanel will set" column is read from it, so the screen cannot
// promise a value the file does not get.
//
// managedBINDOptionAssignments, yönetilen bloğun yazdığı kesin direktif/değer
// kümesidir; yazdığı sırayla. Blok ondan kurulur ve önizlemenin "CelikPanel şu
// değeri koyacak" sütunu ondan okunur; böylece ekran, dosyanın almadığı bir
// değeri vaat edemez.
func managedBINDOptionAssignments(transferPeer string) ([][2]string, error) {
	transferACL := "none"
	if transferPeer != "" {
		acl, err := canonicalBINDTransferACL(transferPeer)
		if err != nil {
			return nil, err
		}
		transferACL = acl
	}
	return [][2]string{
		{"recursion", "no"},
		{"allow-recursion", "{ none; }"},
		{"allow-query-cache", "{ none; }"},
		{"allow-transfer", "{ " + transferACL + "; }"},
	}, nil
}

func managedBINDOptionReplacements(transferPeer string) (map[string]string, error) {
	assignments, err := managedBINDOptionAssignments(transferPeer)
	if err != nil {
		return nil, err
	}
	replacements := make(map[string]string, len(assignments))
	for _, assignment := range assignments {
		replacements[assignment[0]] = assignment[1]
	}
	return replacements, nil
}

// captureForeignBINDOptionDirectives reads the managed directives this
// configuration already sets. It reads the whole options block, not only the
// shapes it can replace: a directive it cannot take over is reported by name
// rather than skipped, because a refusal that names nothing is the defect this
// work exists to fix.
//
// captureForeignBINDOptionDirectives, bu yapılandırmanın zaten koyduğu
// yönetilen direktifleri okur. Yalnız değiştirebildiği biçimleri değil, tüm
// seçenek bloğunu okur: devralamadığı bir direktif atlanmaz, adıyla bildirilir;
// çünkü hiçbir şeyi adlandırmayan bir ret, bu işin düzeltmek için var olduğu
// kusurun ta kendisidir.
func captureForeignBINDOptionDirectives(
	config, file, transferPeer string,
) ([]bindForeignOptionDirective, error) {
	replacements, err := managedBINDOptionReplacements(transferPeer)
	if err != nil {
		return nil, err
	}
	open, closeAt, err := bindOptionsBlock(config)
	if err != nil {
		return nil, err
	}
	clean := []byte(stripBINDCommentsAndStrings(config))
	// A block CelikPanel already owns is not foreign. Its bytes are blanked so
	// the reader cannot mistake the panel's own directives for the operator's.
	//
	// CelikPanel'in zaten sahibi olduğu bir blok yabancı değildir. Baytları
	// boşaltılır; böylece okuyucu, panelin kendi direktiflerini operatörünki
	// sanamaz.
	if start, end, owned := managedBINDOptionsSpan(config, open, closeAt); owned {
		for index := start; index < end && index < len(clean); index++ {
			clean[index] = ' '
		}
	}
	found := []bindForeignOptionDirective{}
	depth := 0
	statementHead := true
	for index := open + 1; index < closeAt; {
		character := clean[index]
		switch {
		case character == '{':
			depth++
			index++
			statementHead = true
		case character == '}':
			depth--
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
			for index < closeAt && bindIdentifierPart(clean[index]) {
				index++
			}
			name := string(clean[start:index])
			replacement, managed := replacements[name]
			if !managed {
				statementHead = false
				continue
			}
			directive := bindForeignOptionDirective{
				Directive: name, Replacement: replacement,
				File: file, Line: bindConfigLineAt(config, start),
				start: start, end: index,
			}
			switch {
			case depth != 0:
				directive.Refusal = bindOptionRefusalNestedScope
			case !statementHead:
				directive.Refusal = bindOptionRefusalNotAStatement
			default:
				terminator, ok := bindStatementTerminator(
					string(clean), index, closeAt,
				)
				if !ok {
					directive.Refusal = bindOptionRefusalUnterminated
					break
				}
				directive.Found = bindOptionValueText(config[index:terminator])
				directive.end = terminator + 1
			}
			found = append(found, directive)
			if directive.adoptable() {
				index = directive.end
				statementHead = true
				continue
			}
			statementHead = false
		default:
			index++
			statementHead = false
		}
	}
	return found, nil
}

// managedBINDOptionsSpan reports CelikPanel's own marked block inside the
// options block, when exactly one complete pair of active marker comments sits
// there. Anything else is not an owned span and is judged as foreign text.
//
// managedBINDOptionsSpan, seçenek bloğunun içinde tam olarak bir eksiksiz etkin
// işaret çifti varsa CelikPanel'in kendi işaretli bloğunu bildirir. Başka her
// şey sahipli bir aralık değildir ve yabancı metin olarak yargılanır.
func managedBINDOptionsSpan(config string, open, closeAt int) (int, int, bool) {
	if strings.Count(config, bindOptionsMarkerBegin) != 1 ||
		strings.Count(config, bindOptionsMarkerEnd) != 1 {
		return 0, 0, false
	}
	start := strings.Index(config, bindOptionsMarkerBegin)
	endOffset := strings.Index(config[start:], bindOptionsMarkerEnd)
	if start < open || endOffset < 0 {
		return 0, 0, false
	}
	end := start + endOffset + len(bindOptionsMarkerEnd)
	if end > closeAt || !bindMarkerStartsActiveComment(config, start) ||
		!bindMarkerStartsActiveComment(config, start+endOffset) {
		return 0, 0, false
	}
	return start, end, true
}

// bindStatementTerminator finds the semicolon that closes a statement that
// began at the directive name, skipping any nested address-match list. It never
// leaves the options block: a statement whose semicolon is not inside the block
// is one this reader cannot claim to understand.
//
// bindStatementTerminator, direktif adında başlayan deyimi kapatan noktalı
// virgülü bulur; iç içe adres eşleşme listelerini atlar. Seçenek bloğunun
// dışına asla çıkmaz: noktalı virgülü blok içinde olmayan bir deyim, bu
// okuyucunun anladığını iddia edebileceği bir deyim değildir.
func bindStatementTerminator(clean string, from, limit int) (int, bool) {
	depth := 0
	for index := from; index < limit; index++ {
		switch clean[index] {
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return 0, false
			}
			depth--
		case ';':
			if depth == 0 {
				return index, true
			}
		}
	}
	return 0, false
}

// bindOptionValueText renders a directive's value the way the screen shows it:
// one line, single spaces, printable characters only, bounded. It is the
// operator's own text and is never interpreted - only displayed - so it is
// normalised here, once, rather than at every surface that carries it.
//
// bindOptionValueText, bir direktifin değerini ekranın gösterdiği biçimde
// yazar: tek satır, tek boşluklar, yalnız yazdırılabilir karakterler, sınırlı.
// Bu, operatörün kendi metnidir ve asla yorumlanmaz - yalnız gösterilir - bu
// yüzden onu taşıyan her yüzeyde değil, burada bir kez normalleştirilir.
func bindOptionValueText(raw string) string {
	const limit = 200
	var builder strings.Builder
	space := false
	for index := 0; index < len(raw); index++ {
		character := raw[index]
		if character < 0x20 || character > 0x7e {
			character = ' '
		}
		if character == ' ' {
			space = builder.Len() > 0
			continue
		}
		if space {
			builder.WriteByte(' ')
			space = false
		}
		builder.WriteByte(character)
	}
	value := builder.String()
	if len(value) > limit {
		value = value[:limit-3] + "..."
	}
	return value
}

func bindConfigLineAt(config string, offset int) int {
	if offset < 0 || offset > len(config) {
		return 0
	}
	return 1 + strings.Count(config[:offset], "\n")
}

// adoptForeignBINDOptions removes the managed directives this server's own
// options block sets, so CelikPanel's block can define them, and reports every
// one it removed with the value it found. It refuses by name the moment a
// directive cannot be replaced, before a single byte is planned.
//
// adoptForeignBINDOptions, bu sunucunun kendi seçenek bloğunun koyduğu
// yönetilen direktifleri kaldırır - böylece CelikPanel'in bloğu onları
// tanımlayabilir - ve kaldırdığı her birini bulduğu değerle bildirir. Bir
// direktif değiştirilemediği anda, tek bir bayt planlanmadan önce, adıyla
// reddeder.
func adoptForeignBINDOptions(
	config, file, transferPeer string,
) (string, []bindForeignOptionDirective, error) {
	found, err := captureForeignBINDOptionDirectives(config, file, transferPeer)
	if err != nil {
		return "", nil, err
	}
	for _, directive := range found {
		if !directive.adoptable() {
			return "", nil, errors.New(bindForeignOptionRefusal(directive))
		}
	}
	adopted := config
	for index := len(found) - 1; index >= 0; index-- {
		adopted = removeBINDStatementSpan(
			adopted, found[index].start, found[index].end,
		)
	}
	// The reader is exhaustive over the options block, so nothing it accepted
	// may still be there. Proving it costs one pass and turns a reader bug into
	// a refusal instead of a silently half-taken configuration.
	//
	// Okuyucu seçenek bloğu üzerinde tümdür; dolayısıyla kabul ettiği hiçbir şey
	// hâlâ orada olamaz. Bunu kanıtlamak bir geçiş eder ve bir okuyucu hatasını,
	// sessizce yarım devralınmış bir yapılandırma yerine bir rette çevirir.
	open, closeAt, err := bindOptionsBlock(adopted)
	if err != nil {
		return "", nil, err
	}
	body := stripBINDCommentsAndStrings(adopted)[open+1 : closeAt]
	for _, directive := range bindManagedOptionDirectives {
		if bindContainsDirective(body, directive) {
			return "", nil, fmt.Errorf(
				"BIND options in %s still define %s after the takeover read them; "+
					"CelikPanel will not take over a configuration it cannot read exactly",
				file, directive,
			)
		}
	}
	return adopted, found, nil
}

// bindForeignOptionRefusal names what cannot be taken over and what the
// operator can do about it: the directive, the file, the line. "Ownership
// markers" said none of those, which is why it is gone.
//
// bindForeignOptionRefusal, devralınamayanı ve operatörün bu konuda ne
// yapabileceğini adlandırır: direktif, dosya, satır. "Sahiplik işaretleri"
// bunların hiçbirini söylemiyordu; bu yüzden artık yok.
func bindForeignOptionRefusal(directive bindForeignOptionDirective) string {
	switch directive.Refusal {
	case bindOptionRefusalNestedScope:
		return fmt.Sprintf(
			"%s is set inside a nested block in %s line %d; CelikPanel takes over "+
				"only a directive written directly in the options block. Move it to "+
				"the options block itself, or remove it, then take this server over "+
				"again",
			directive.Directive, directive.File, directive.Line,
		)
	case bindOptionRefusalNotAStatement:
		return fmt.Sprintf(
			"%s appears in %s line %d where CelikPanel cannot read it as a directive "+
				"of its own. Write it as its own statement in the options block, or "+
				"remove it, then take this server over again",
			directive.Directive, directive.File, directive.Line,
		)
	case bindOptionRefusalUnterminated:
		return fmt.Sprintf(
			"%s in %s line %d has no terminating semicolon inside the options block, "+
				"so CelikPanel cannot tell where it ends. Correct it, or remove it, "+
				"then take this server over again",
			directive.Directive, directive.File, directive.Line,
		)
	default:
		return fmt.Sprintf(
			"%s in %s line %d cannot be taken over",
			directive.Directive, directive.File, directive.Line,
		)
	}
}

// removeBINDStatementSpan deletes a statement and, when that leaves its line
// holding nothing but whitespace, the line with it. What is left is a file a
// person still recognises as theirs.
//
// removeBINDStatementSpan bir deyimi siler ve bu, satırında boşluktan başka bir
// şey bırakmıyorsa satırı da siler. Geriye, bir insanın hâlâ kendisininki diye
// tanıdığı bir dosya kalır.
func removeBINDStatementSpan(config string, start, end int) string {
	if start < 0 || end > len(config) || start >= end {
		return config
	}
	trimmed := config[:start] + config[end:]
	lineStart := strings.LastIndexByte(trimmed[:start], '\n') + 1
	lineEnd := strings.IndexByte(trimmed[lineStart:], '\n')
	tail := len(trimmed)
	if lineEnd >= 0 {
		tail = lineStart + lineEnd
	}
	if strings.TrimLeft(trimmed[lineStart:tail], " \t\r") != "" {
		return trimmed
	}
	if lineEnd < 0 {
		return trimmed[:lineStart]
	}
	return trimmed[:lineStart] + trimmed[tail+1:]
}

// bindManagedOptionsRefusal keeps the refusal that must survive - the one on
// every path nobody consented to a takeover on - and makes it honest. It used
// to say only "BIND options already define recursion outside CelikPanel
// ownership": true, and useless. It now names the file and the line, and says
// what the operator can do. A refusal that names nothing is the defect this
// work exists to fix, and it must not survive here either.
//
// bindManagedOptionsRefusal, yaşaması gereken reddi - kimsenin devralmaya rıza
// göstermediği her yoldaki reddi - korur ve dürüst kılar. Eskiden yalnız "BIND
// options already define recursion outside CelikPanel ownership" diyordu:
// doğru ve işe yaramaz. Artık dosyayı ve satırı adlandırır ve operatörün ne
// yapabileceğini söyler. Hiçbir şeyi adlandırmayan bir ret, bu işin düzeltmek
// için var olduğu kusurdur ve burada da yaşamamalıdır.
func bindManagedOptionsRefusal(
	file, config, transferPeer string,
	cause error,
) error {
	if cause == nil {
		return nil
	}
	wrapped := fmt.Errorf("prepare BIND authoritative options: %w", cause)
	if !strings.Contains(cause.Error(), "already define") {
		return wrapped
	}
	found, err := captureForeignBINDOptionDirectives(config, file, transferPeer)
	if err != nil {
		return wrapped
	}
	for _, directive := range found {
		if !strings.Contains(cause.Error(), directive.Directive) {
			continue
		}
		return fmt.Errorf(
			"prepare BIND authoritative options: %s is already set to %s in %s "+
				"line %d, and CelikPanel manages that directive. Take this DNS "+
				"server over from the DNS infrastructure screen, which shows every "+
				"such directive and the value CelikPanel will set, or remove the "+
				"directive from %s",
			directive.Directive, directive.Found, directive.File, directive.Line,
			directive.File,
		)
	}
	return wrapped
}

// reportForeignBINDOptions is the readiness surface of the same reader. The
// panel asks the host what a takeover would replace, and the host answers with
// facts: the directive, what it says now, what CelikPanel will make it say, the
// file and the line. The preview turns that into the list the operator reads
// before agreeing, and a directive that cannot be taken over travels with its
// refusal code so the screen can name it instead of failing at commit time.
//
// The transfer peer is empty because the takeover is only ever offered for a
// standalone identity, which is what the panel's own gate enforces.
//
// reportForeignBINDOptions, aynı okuyucunun hazırlık yüzeyidir. Panel sunucuya
// bir devralmanın neyi değiştireceğini sorar, sunucu olgularla cevaplar:
// direktif, şu anda ne dediği, CelikPanel'in ne dedirteceği, dosya ve satır.
// Önizleme bunu, operatörün rıza göstermeden önce okuduğu listeye çevirir ve
// devralınamayan bir direktif kendi ret koduyla yol alır; böylece ekran onu
// commit anında düşmek yerine adlandırabilir.
//
// Aktarım eşi boştur, çünkü devralma yalnız tek sunuculu bir kimlik için
// sunulur; panelin kendi kapısının dayattığı şey budur.
func reportForeignBINDOptions(
	layout bindHostLayout,
) ([]transport.DNSForeignEngineOption, error) {
	data, err := secureReadConfig(layout.OptionsConfig)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", layout.OptionsConfig, err)
	}
	found, err := captureForeignBINDOptionDirectives(
		string(data), layout.OptionsConfig, "",
	)
	if err != nil {
		return nil, err
	}
	report := make([]transport.DNSForeignEngineOption, 0, len(found))
	for _, directive := range found {
		report = append(report, transport.DNSForeignEngineOption{
			Directive: directive.Directive, Found: directive.Found,
			Replacement: directive.Replacement, File: directive.File,
			Line: directive.Line, Refusal: directive.Refusal,
		})
	}
	return report, nil
}
