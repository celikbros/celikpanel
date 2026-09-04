package main

import (
	"errors"
	"os"
	"strings"
	"testing"
)

// handConfiguredBINDOptions is the file this whole change exists for: an
// authoritative BIND an administrator configured, carrying the directives
// CelikPanel manages. Before R-042 the generation refused it outright.
//
// handConfiguredBINDOptions, bu değişikliğin var olma sebebi olan dosyadır: bir
// yöneticinin yapılandırdığı, CelikPanel'in yönettiği direktifleri taşıyan
// yetkili bir BIND. R-042'den önce nesil onu doğrudan reddediyordu.
const handConfiguredBINDOptions = `options {
	directory "/var/cache/bind";

	recursion no;
	allow-transfer { 203.0.113.7; };
	allow-query-cache { none; };

	dnssec-validation auto;
	listen-on-v6 { any; };
};
`

func TestBINDTakeoverReadsTheDirectivesItReplaces(t *testing.T) {
	found, err := captureForeignBINDOptionDirectives(
		handConfiguredBINDOptions, "/etc/bind/named.conf.options", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	want := []bindForeignOptionDirective{
		{Directive: "recursion", Found: "no", Replacement: "no", Line: 4},
		{
			Directive: "allow-transfer", Found: "{ 203.0.113.7; }",
			Replacement: "{ none; }", Line: 5,
		},
		{
			Directive: "allow-query-cache", Found: "{ none; }",
			Replacement: "{ none; }", Line: 6,
		},
	}
	if len(found) != len(want) {
		t.Fatalf("captured %d directives, want %d: %+v", len(found), len(want), found)
	}
	for index, expected := range want {
		actual := found[index]
		if actual.Directive != expected.Directive ||
			actual.Found != expected.Found ||
			actual.Replacement != expected.Replacement ||
			actual.Line != expected.Line ||
			actual.File != "/etc/bind/named.conf.options" ||
			actual.Refusal != "" {
			t.Fatalf("directive %d = %+v, want %+v", index, actual, expected)
		}
	}
	// The value the operator already agrees with is reported as it is, so the
	// panel can say "unchanged" rather than list it as a loss.
	//
	// Operatörün zaten katıldığı değer olduğu gibi bildirilir; böylece panel onu
	// bir kayıp diye listelemek yerine "değişmiyor" diyebilir.
	if found[0].Found != found[0].Replacement {
		t.Fatal("recursion no is the same value CelikPanel sets and must read as such")
	}
	if found[1].Found == found[1].Replacement {
		t.Fatal("an operator transfer ACL is not what CelikPanel sets")
	}
}

func TestBINDTakeoverReplacesThemAndLeavesEverythingElseAlone(t *testing.T) {
	adopted, found, err := adoptForeignBINDOptions(
		handConfiguredBINDOptions, "/etc/bind/named.conf.options", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 3 {
		t.Fatalf("took over %d directives, want 3", len(found))
	}
	for _, keep := range []string{
		"directory \"/var/cache/bind\";",
		"dnssec-validation auto;",
		"listen-on-v6 { any; };",
	} {
		if !strings.Contains(adopted, keep) {
			t.Fatalf("the takeover removed %q, which is the operator's own setting", keep)
		}
	}
	for _, gone := range []string{
		"recursion no;", "allow-transfer { 203.0.113.7; };",
		"allow-query-cache { none; };",
	} {
		if strings.Contains(adopted, gone) {
			t.Fatalf("%q survived the takeover; CelikPanel's block would not govern it", gone)
		}
	}
	// The refusal this fix removes: the generation now accepts the very file it
	// used to refuse, and writes its own block into it.
	//
	// Bu düzeltmenin kaldırdığı ret: nesil artık eskiden reddettiği dosyanın ta
	// kendisini kabul eder ve kendi bloğunu ona yazar.
	managed, err := managedBINDOptions(adopted, "")
	if err != nil {
		t.Fatalf("the generation still refuses a hand-configured server: %v", err)
	}
	for _, want := range []string{
		bindOptionsMarkerBegin, "recursion no;", "allow-recursion { none; };",
		"allow-query-cache { none; };", "allow-transfer { none; };",
		bindOptionsMarkerEnd,
	} {
		if !strings.Contains(managed, want) {
			t.Fatalf("the managed options block is missing %q", want)
		}
	}
	// Applying the generation twice must be the same file, or every later
	// verification of a taken-over host would refuse it.
	//
	// Neslin iki kez uygulanması aynı dosya olmalıdır; yoksa devralınmış bir
	// sunucunun sonraki her doğrulaması onu reddederdi.
	again, err := managedBINDOptions(managed, "")
	if err != nil || again != managed {
		t.Fatalf("a taken-over configuration is not stable: %v", err)
	}
}

func TestBINDTakeoverKeepsAPairedTransferPeer(t *testing.T) {
	adopted, _, err := adoptForeignBINDOptions(
		handConfiguredBINDOptions, "/etc/bind/named.conf.options", "203.0.113.9",
	)
	if err != nil {
		t.Fatal(err)
	}
	managed, err := managedBINDOptions(adopted, "203.0.113.9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(managed, "allow-transfer { 203.0.113.9/32; };") {
		t.Fatal("the paired transfer ACL is not what the generation wrote")
	}
	found, err := captureForeignBINDOptionDirectives(
		handConfiguredBINDOptions, "/etc/bind/named.conf.options", "203.0.113.9",
	)
	if err != nil {
		t.Fatal(err)
	}
	// The value the screen promises and the value the file gets are read from
	// one list, so they cannot disagree.
	//
	// Ekranın vaat ettiği değer ile dosyanın aldığı değer tek bir listeden
	// okunur; bu yüzden anlaşmazlığa düşemezler.
	for _, directive := range found {
		if directive.Directive != "allow-transfer" {
			continue
		}
		if directive.Replacement != "{ 203.0.113.9/32; }" {
			t.Fatalf("the preview would promise %q", directive.Replacement)
		}
	}
}

func TestBINDTakeoverRefusesByNameWhatItCannotRead(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		config  string
		refusal string
		want    []string
	}{
		{
			name: "inside a nested block",
			config: "options {\n\tresponse-policy { zone \"rpz\" };\n" +
				"\tmasters celik {\n\t\trecursion no;\n\t};\n};\n",
			refusal: bindOptionRefusalNestedScope,
			want: []string{
				"recursion", "/etc/bind/named.conf.options", "line 4",
				"options block", "take this server over again",
			},
		},
		{
			name:    "not a statement of its own",
			config:  "options {\n\tcheck-names master recursion;\n};\n",
			refusal: bindOptionRefusalNotAStatement,
			want: []string{
				"recursion", "/etc/bind/named.conf.options", "line 2",
				"its own statement",
			},
		},
		{
			name:    "no terminating semicolon",
			config:  "options {\n\tdirectory \"/var/cache/bind\";\n\tallow-transfer { none; }\n};\n",
			refusal: bindOptionRefusalUnterminated,
			want: []string{
				"allow-transfer", "/etc/bind/named.conf.options", "line 3",
				"semicolon",
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			found, err := captureForeignBINDOptionDirectives(
				testCase.config, "/etc/bind/named.conf.options", "",
			)
			if err != nil {
				t.Fatal(err)
			}
			refused := 0
			for _, directive := range found {
				if directive.Refusal == testCase.refusal {
					refused++
				}
			}
			if refused != 1 {
				t.Fatalf("captured %+v, want one %s refusal", found, testCase.refusal)
			}
			_, _, err = adoptForeignBINDOptions(
				testCase.config, "/etc/bind/named.conf.options", "",
			)
			if err == nil {
				t.Fatal("a shape CelikPanel cannot read must be refused")
			}
			// A refusal that names nothing is the defect being fixed. Every one
			// that survives names the directive, the file, the line and a way
			// out.
			//
			// Hiçbir şeyi adlandırmayan bir ret, düzeltilen kusurdur. Yaşayan
			// her ret; direktifi, dosyayı, satırı ve bir çıkış yolunu adlandırır.
			for _, want := range testCase.want {
				if !strings.Contains(err.Error(), want) {
					t.Fatalf("refusal %q does not name %q", err.Error(), want)
				}
			}
		})
	}
}

func TestBINDTakeoverIgnoresCommentsAndStrings(t *testing.T) {
	config := "options {\n\t// recursion yes;\n\t/* allow-transfer { any; }; */\n" +
		"\tdirectory \"recursion no;\";\n};\n"
	found, err := captureForeignBINDOptionDirectives(
		config, "/etc/bind/named.conf.options", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("a comment or a quoted string is not a directive: %+v", found)
	}
	adopted, _, err := adoptForeignBINDOptions(
		config, "/etc/bind/named.conf.options", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if adopted != config {
		t.Fatal("the takeover rewrote a file that sets none of its directives")
	}
}

func TestBINDTakeoverLeavesTheManagedBlockAlone(t *testing.T) {
	managed, err := managedBINDOptions("options {\n};\n", "")
	if err != nil {
		t.Fatal(err)
	}
	found, err := captureForeignBINDOptionDirectives(
		managed, "/etc/bind/named.conf.options", "",
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("CelikPanel's own directives are not the operator's: %+v", found)
	}
}

// The refusal that must survive - every path nobody consented to a takeover on
// - now names the file and the line and says what to do.
//
// Yaşaması gereken ret - kimsenin devralmaya rıza göstermediği her yol - artık
// dosyayı ve satırı adlandırır ve ne yapılacağını söyler.
func TestBINDExclusiveRefusalNamesTheDirectiveAndTheLine(t *testing.T) {
	_, err := managedBINDOptions(handConfiguredBINDOptions, "")
	if err == nil {
		t.Fatal("a path without consent must still refuse")
	}
	refusal := bindManagedOptionsRefusal(
		"/etc/bind/named.conf.options", handConfiguredBINDOptions, "", err,
	)
	for _, want := range []string{
		"recursion", "already set to no", "/etc/bind/named.conf.options",
		"line 4", "DNS infrastructure screen",
	} {
		if !strings.Contains(refusal.Error(), want) {
			t.Fatalf("refusal %q does not name %q", refusal.Error(), want)
		}
	}
}

func TestManagedBINDOptionAssignmentsAreTheBlockTheFileGets(t *testing.T) {
	assignments, err := managedBINDOptionAssignments("")
	if err != nil {
		t.Fatal(err)
	}
	managed, err := managedBINDOptions("options {\n};\n", "")
	if err != nil {
		t.Fatal(err)
	}
	if len(assignments) != len(bindManagedOptionDirectives) {
		t.Fatalf("the managed block writes %d directives, the list names %d",
			len(assignments), len(bindManagedOptionDirectives))
	}
	for index, assignment := range assignments {
		if assignment[0] != bindManagedOptionDirectives[index] {
			t.Fatalf("assignment %d is %q, the managed list says %q",
				index, assignment[0], bindManagedOptionDirectives[index])
		}
		if !strings.Contains(managed, "\t"+assignment[0]+" "+assignment[1]+";\n") {
			t.Fatalf("the file does not carry %q %q", assignment[0], assignment[1])
		}
	}
}

func TestBINDOptionValueTextIsOneBoundedPrintableLine(t *testing.T) {
	value := bindOptionValueText("\t{\n\t\t198.51.100.4;\r\n\t\t198.51.100.5;\n\t}")
	if value != "{ 198.51.100.4; 198.51.100.5; }" {
		t.Fatalf("value = %q", value)
	}
	long := bindOptionValueText("{" + strings.Repeat("a", 400) + "}")
	if len(long) != 200 || !strings.HasSuffix(long, "...") {
		t.Fatalf("long value = %d bytes %q", len(long), long)
	}
	if control := bindOptionValueText("no\x00\x1b[31m"); control != "no [31m" {
		t.Fatalf("value = %q", control)
	}
}

// The R-042 behaviour end to end at the preparation layer, which is where the
// refusal lived: the exclusive authority still refuses a hand-configured
// server, and the takeover accepts it, records what it replaced, and writes
// CelikPanel's own block. On the tree before this change the takeover call
// answers "BIND options already define recursion outside CelikPanel
// ownership", which is the defect.
//
// R-042 davranışı, reddin yaşadığı yer olan hazırlık katmanında uçtan uca:
// dışlayıcı yetki elle yapılandırılmış bir sunucuyu hâlâ reddeder, devralma
// ise onu kabul eder, neyi değiştirdiğini kaydeder ve CelikPanel'in kendi
// bloğunu yazar. Bu değişiklikten önceki ağaçta devralma çağrısı "BIND options
// already define recursion outside CelikPanel ownership" cevabını verir; kusur
// budur.
func TestBINDTakeoverPreparationAcceptsAHandConfiguredServer(t *testing.T) {
	layout := bindHostLayout{
		GenerationRoot: "/opt/celikpanel/dns/bind",
		OptionsConfig:  "/etc/bind/named.conf.options",
		AnchorConfig:   "/etc/bind/named.conf.local",
	}
	contents := map[string]string{
		layout.OptionsConfig: handConfiguredBINDOptions,
		layout.AnchorConfig:  "// the operator's own zones\n",
	}
	reader := func(
		path string, mode os.FileMode, allowAbsent bool,
	) (dnsFileSnapshot, error) {
		if allowAbsent {
			return dnsFileSnapshot{}, errors.New("unexpected absent BIND config")
		}
		return dnsFileSnapshot{
			Path: path, Exists: true, Mode: uint32(mode.Perm()),
			Data: []byte(contents[path]),
		}, nil
	}
	if _, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsExclusive, reader,
	); err == nil {
		t.Fatal("a path without the operator's consent must still refuse")
	}
	mutation, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsTakeover, reader,
	)
	if err != nil {
		t.Fatalf("the takeover refuses the server it exists for: %v", err)
	}
	if len(mutation.adopted) != 3 {
		t.Fatalf("the takeover recorded %d directives, want 3: %+v",
			len(mutation.adopted), mutation.adopted)
	}
	desired := string(mutation.desired[layout.OptionsConfig])
	outside := strings.Split(desired, bindOptionsMarkerBegin)[0]
	for _, gone := range []string{
		"recursion no;", "allow-transfer { 203.0.113.7; };",
		"allow-query-cache { none; };",
	} {
		if strings.Contains(outside, gone) {
			t.Fatalf("%q is still outside CelikPanel's block", gone)
		}
	}
	if !strings.Contains(desired, "allow-transfer { none; };") ||
		!strings.Contains(desired, "directory \"/var/cache/bind\";") {
		t.Fatalf("the prepared options are not the takeover's:\n%s", desired)
	}
	// Nothing the operator wrote is lost from the snapshot the rollback uses.
	//
	// Operatörün yazdığı hiçbir şey, geri almanın kullandığı anlık görüntüden
	// kaybolmaz.
	if string(mutation.original[layout.OptionsConfig]) != handConfiguredBINDOptions {
		t.Fatal("the takeover did not capture the file exactly as it found it")
	}
}
