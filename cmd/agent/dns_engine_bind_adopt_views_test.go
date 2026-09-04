package main

import (
	"errors"
	"fmt"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func viewTestReader(files map[string]string) bindConfigReader {
	return func(path string) ([]byte, error) {
		data, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("open %s: no such file", path)
		}
		return []byte(data), nil
	}
}

const viewTestMain = "/etc/bind/named.conf"

// A view in the file the takeover would read is the plain case, and the one the
// register was written about: a recursion set inside it wins over the options
// block, so a takeover that only read options would report a setting it does
// not control.
//
// Devralmanın okuyacağı dosyadaki bir view en yalın durumdur ve deftere yazılan
// da odur: içinde koyulan bir recursion, seçenek bloğuna üstün gelir; yani
// yalnız seçenekleri okuyan bir devralma, denetlemediği bir ayarı bildirirdi.
func TestBINDViewDetectionNamesTheFirstViewAndItsLine(t *testing.T) {
	config := "options {\n" +
		"\tdirectory \"/var/cache/bind\";\n" +
		"\trecursion no;\n" +
		"};\n" +
		"\n" +
		"view \"internal\" {\n" +
		"\tmatch-clients { 10.0.0.0/8; };\n" +
		"\trecursion yes;\n" +
		"};\n"
	finding, err := findBINDViewDeclaration(
		[]string{viewTestMain},
		viewTestReader(map[string]string{viewTestMain: config}),
	)
	if err != nil {
		t.Fatal(err)
	}
	if finding == nil {
		t.Fatal("a view declaration was not found")
	}
	if finding.Finding != transport.DNSForeignViewDeclared ||
		finding.File != viewTestMain || finding.Line != 6 {
		t.Fatalf("finding = %+v, want declared at %s line 6", finding, viewTestMain)
	}
}

// A configuration with no views must not be refused, or the takeover R-042
// built would be refused on every server it was built for.
//
// View'i olmayan bir yapılandırma reddedilmemelidir; yoksa R-042'nin kurduğu
// devralma, kurulduğu her sunucuda reddedilirdi.
func TestBINDViewDetectionIsSilentOnAConfigurationWithoutViews(t *testing.T) {
	files := map[string]string{
		viewTestMain: "include \"/etc/bind/named.conf.options\";\n" +
			"include \"/etc/bind/named.conf.local\";\n",
		"/etc/bind/named.conf.options": "options {\n\trecursion no;\n};\n",
		"/etc/bind/named.conf.local": "zone \"legacy.test\" {\n" +
			"\ttype master;\n\tfile \"/etc/bind/db.legacy\";\n};\n",
	}
	finding, err := findBINDViewDeclaration(
		[]string{viewTestMain, "/etc/bind/named.conf.options"},
		viewTestReader(files),
	)
	if err != nil {
		t.Fatal(err)
	}
	if finding != nil {
		t.Fatalf("finding = %+v, want nothing on a configuration without views", finding)
	}
}

// The word is not the thing. `view` inside a comment, inside a quoted string,
// or as part of a longer identifier is not a view declaration, and refusing a
// server over one would be the same class of defect as missing a real one.
//
// Sözcük, şeyin kendisi değildir. Bir yorumun içindeki, tırnak içindeki ya da
// daha uzun bir tanımlayıcının parçası olan `view` bir view bildirimi değildir;
// bir sunucuyu bunun yüzünden reddetmek, gerçeğini kaçırmakla aynı sınıf bir
// kusur olurdu.
func TestBINDViewDetectionIgnoresTheWordWhereItIsNotADeclaration(t *testing.T) {
	cases := map[string]string{
		"line comment":  "// view \"internal\" { recursion yes; };\noptions {\n\trecursion no;\n};\n",
		"hash comment":  "# view \"internal\" {\noptions {\n\trecursion no;\n};\n",
		"block comment": "/*\nview \"internal\" {\n\trecursion yes;\n};\n*/\noptions { recursion no; };\n",
		"quoted string": "zone \"view.example.com\" {\n\ttype master;\n\tfile \"/etc/bind/view\";\n};\n",
		"longer identifier before": "options {\n\trecursion no;\n};\n" +
			"viewport \"internal\" {\n\trecursion yes;\n};\n",
		"longer identifier after": "options {\n\trecursion no;\n};\n" +
			"view-hint \"internal\" {\n\trecursion yes;\n};\n",
		"not at a statement head": "options {\n\trecursion no;\n};\n" +
			"acl view { 10.0.0.0/8; };\n",
		"nested in a block": "options {\n\trecursion no;\n};\n" +
			"zone \"legacy.test\" {\n\ttype master;\n\tview \"x\" { };\n};\n",
		"no block follows": "options {\n\trecursion no;\n};\nview;\n",
	}
	for name, config := range cases {
		t.Run(name, func(t *testing.T) {
			finding, err := findBINDViewDeclaration(
				[]string{viewTestMain},
				viewTestReader(map[string]string{viewTestMain: config}),
			)
			if err != nil {
				t.Fatal(err)
			}
			if finding != nil {
				t.Fatalf("finding = %+v, want nothing: this is not a view", finding)
			}
		})
	}
}

// A view behind an include is still a view. Debian's own layout puts the
// operator's own configuration in an included file, so this is where a real
// host would actually keep one.
//
// Bir include'un arkasındaki view de bir view'dir. Debian'ın kendi düzeni
// operatörün kendi yapılandırmasını dahil edilen bir dosyaya koyar; yani gerçek
// bir sunucu onu asıl burada tutar.
func TestBINDViewDetectionFollowsIncludes(t *testing.T) {
	files := map[string]string{
		viewTestMain: "include \"/etc/bind/named.conf.options\";\n" +
			"include \"/etc/bind/named.conf.local\";\n",
		"/etc/bind/named.conf.options": "options {\n\trecursion no;\n};\n",
		"/etc/bind/named.conf.local":   "include \"/etc/bind/views.conf\";\n",
		"/etc/bind/views.conf": "\nview \"external\" IN {\n" +
			"\tmatch-clients { any; };\n};\n",
	}
	finding, err := findBINDViewDeclaration(
		[]string{viewTestMain, "/etc/bind/named.conf.options"},
		viewTestReader(files),
	)
	if err != nil {
		t.Fatal(err)
	}
	if finding == nil {
		t.Fatal("a view two includes deep was not found")
	}
	if finding.Finding != transport.DNSForeignViewDeclared ||
		finding.File != "/etc/bind/views.conf" || finding.Line != 2 {
		t.Fatalf("finding = %+v, want declared at /etc/bind/views.conf line 2", finding)
	}
}

// An include the reader cannot follow is not "no views". It is a configuration
// CelikPanel could not read whole, and it is reported as the include statement
// the operator can go and look at.
//
// İzlenemeyen bir include "view yok" demek değildir. CelikPanel'in bütünüyle
// okuyamadığı bir yapılandırmadır ve operatörün gidip bakabileceği include
// deyimi olarak bildirilir.
func TestBINDViewDetectionReportsAnIncludeItCannotFollow(t *testing.T) {
	cases := []struct {
		name  string
		files map[string]string
		line  int
	}{
		{
			name: "unreadable file",
			files: map[string]string{
				viewTestMain: "options {\n\trecursion no;\n};\n" +
					"include \"/etc/bind/secret.conf\";\n",
			},
			line: 4,
		},
		{
			name: "a path that is not one absolute quoted filename",
			files: map[string]string{
				viewTestMain: "include \"views.conf\";\n",
			},
			line: 1,
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			finding, err := findBINDViewDeclaration(
				[]string{viewTestMain}, viewTestReader(testCase.files),
			)
			if err != nil {
				t.Fatal(err)
			}
			if finding == nil {
				t.Fatal("an unfollowable include was read as no views")
			}
			if finding.Finding != transport.DNSForeignViewUnreadable ||
				finding.File != viewTestMain || finding.Line != testCase.line {
				t.Fatalf(
					"finding = %+v, want unreadable at %s line %d",
					finding, viewTestMain, testCase.line,
				)
			}
		})
	}
}

// A configuration that includes itself is read once. A probe that spins is a
// readiness call that never answers.
//
// Kendisini dahil eden bir yapılandırma bir kez okunur. Dönüp duran bir
// yoklama, hiç cevap vermeyen bir hazırlık çağrısıdır.
func TestBINDViewDetectionReadsAnIncludeRingOnce(t *testing.T) {
	files := map[string]string{
		viewTestMain:       "include \"/etc/bind/a.conf\";\n",
		"/etc/bind/a.conf": "include \"/etc/bind/b.conf\";\n",
		"/etc/bind/b.conf": "include \"" + viewTestMain + "\";\n",
	}
	reads := 0
	read := func(path string) ([]byte, error) {
		reads++
		if reads > 16 {
			return nil, errors.New("the view probe is walking in a ring")
		}
		data, ok := files[path]
		if !ok {
			return nil, fmt.Errorf("open %s: no such file", path)
		}
		return []byte(data), nil
	}
	finding, err := findBINDViewDeclaration([]string{viewTestMain}, read)
	if err != nil {
		t.Fatal(err)
	}
	if finding != nil {
		t.Fatalf("finding = %+v, want nothing", finding)
	}
	if reads != 3 {
		t.Fatalf("reads = %d, want each file read exactly once", reads)
	}
}

// A root that cannot be read is a probe failure rather than a finding: there is
// no include statement to send the operator to, and the takeover reads the same
// file for its own reasons and refuses there.
//
// Okunamayan bir kök, bir bulgu değil bir yoklama hatasıdır: operatörü
// göndereceğimiz bir include deyimi yoktur ve devralma aynı dosyayı kendi
// sebepleriyle okur ve orada reddeder.
func TestBINDViewDetectionFailsWhenARootCannotBeRead(t *testing.T) {
	_, err := findBINDViewDeclaration(
		[]string{viewTestMain}, viewTestReader(map[string]string{}),
	)
	if err == nil {
		t.Fatal("an unreadable root config was reported as no views")
	}
}
