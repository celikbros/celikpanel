package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The directives a hand-configured authoritative BIND actually carries, as the
// host reports them. "recursion no;" is already what CelikPanel sets, which is
// the case the preview must say is unchanged rather than list as a loss.
//
// Elle yapılandırılmış yetkili bir BIND'in gerçekten taşıdığı direktifler,
// sunucunun bildirdiği hâliyle. "recursion no;" zaten CelikPanel'in koyduğu
// şeydir; önizlemenin bir kayıp diye listelemek yerine değişmiyor demesi
// gereken durum budur.
func handConfiguredBINDForeignOptions() []transport.DNSForeignEngineOption {
	return []transport.DNSForeignEngineOption{
		{
			Directive: "recursion", Found: "no", Replacement: "no",
			File: "/etc/bind/named.conf.options", Line: 4,
		},
		{
			Directive: "allow-transfer", Found: "{ 203.0.113.7; }",
			Replacement: "{ none; }",
			File:        "/etc/bind/named.conf.options", Line: 5,
		},
	}
}

func unmanagedBINDRuntimesWithForeignOptions(
	options []transport.DNSForeignEngineOption,
) map[transport.DNSEngine]transport.DNSBackendRuntimeState {
	runtimes := stoppedUnmanagedBINDRuntimes()
	bind := runtimes[transport.DNSEngineBIND]
	bind.ForeignOptions = options
	runtimes[transport.DNSEngineBIND] = bind
	return runtimes
}

func TestDNSEnginePreviewCarriesTheDifferenceTheTakeoverMakes(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = unmanagedBINDRuntimesWithForeignOptions(
		handConfiguredBINDForeignOptions(),
	)
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if preview.Action != dnsEngineActionAdoptUnmanaged ||
		len(preview.Blockers) != 0 || preview.PreviewToken == "" {
		t.Fatalf("preview=%+v", preview)
	}
	if len(preview.AdoptedDirectives) != 2 {
		t.Fatalf("the preview carries %d directives, want 2: %+v",
			len(preview.AdoptedDirectives), preview.AdoptedDirectives)
	}
	// A value that already equals what CelikPanel sets is said to be unchanged.
	// Listing it as a change would make a safe takeover read as a loss.
	//
	// CelikPanel'in koyduğuyla zaten eşit olan bir değerin değişmediği söylenir.
	// Onu bir değişiklik diye listelemek, güvenli bir devralmayı bir kayıp gibi
	// okuturdu.
	recursion := preview.AdoptedDirectives[0]
	if recursion.Directive != "recursion" || recursion.Found != "no" ||
		recursion.Replacement != "no" || !recursion.Unchanged ||
		recursion.File != "/etc/bind/named.conf.options" || recursion.Line != 4 ||
		recursion.Refusal != "" {
		t.Fatalf("recursion=%+v", recursion)
	}
	transfer := preview.AdoptedDirectives[1]
	if transfer.Directive != "allow-transfer" ||
		transfer.Found != "{ 203.0.113.7; }" ||
		transfer.Replacement != "{ none; }" || transfer.Unchanged ||
		transfer.Line != 5 || transfer.Refusal != "" {
		t.Fatalf("allow-transfer=%+v", transfer)
	}
	// The acknowledgement is the one that already exists. The difference list
	// informs it; it does not add a second decision.
	//
	// Onay, zaten var olan onaydır. Fark listesi onu bilgilendirir; ikinci bir
	// karar eklemez.
	if !preview.RequiresAdoptionAcknowledgement ||
		preview.RequiresDowntimeAcknowledgement {
		t.Fatalf("the takeover acknowledgement contract changed: %+v", preview)
	}
}

func TestDNSEnginePreviewCarriesNoDifferenceForAStockServer(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = stoppedUnmanagedBINDRuntimes()
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if preview.Action != dnsEngineActionAdoptUnmanaged ||
		len(preview.AdoptedDirectives) != 0 || len(preview.Blockers) != 0 {
		t.Fatalf("preview=%+v", preview)
	}
	if !strings.Contains(recorder.Body.String(), `"impacts"`) ||
		strings.Contains(recorder.Body.String(), `"adopted_directives"`) {
		t.Fatalf("an empty difference list must not reach the browser: %s",
			recorder.Body.String())
	}
}

// A directive the host could not read as its own statement blocks the takeover
// on the screen the operator is standing on, with the directive, the file and
// the line - not halfway through a commit, with a message about a generated
// file nobody has seen.
//
// Sunucunun kendi deyimi olarak okuyamadığı bir direktif, devralmayı operatörün
// üzerinde durduğu ekranda engeller; direktif, dosya ve satırla - bir commit'in
// yarısında, kimsenin görmediği üretilmiş bir dosya hakkında bir mesajla değil.
func TestDNSEnginePreviewBlocksAndNamesADirectiveItCannotTakeOver(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = unmanagedBINDRuntimesWithForeignOptions(
		[]transport.DNSForeignEngineOption{
			{
				Directive: "recursion", Replacement: "no",
				File: "/etc/bind/named.conf.options", Line: 9,
				Refusal: transport.DNSForeignOptionNestedScope,
			},
		},
	)
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !hasDNSEngineBlocker(preview, dnsEngineAdoptionOptionsBlocker) {
		t.Fatalf("blockers=%+v", preview.Blockers)
	}
	if preview.PreviewToken != "" {
		t.Fatal("a blocked preview must hand out no token")
	}
	if len(preview.AdoptedDirectives) != 1 ||
		preview.AdoptedDirectives[0].Refusal != transport.DNSForeignOptionNestedScope ||
		preview.AdoptedDirectives[0].Line != 9 ||
		preview.AdoptedDirectives[0].File != "/etc/bind/named.conf.options" {
		t.Fatalf("a blocked takeover must still name what it refused: %+v",
			preview.AdoptedDirectives)
	}
}

func TestDNSBackendReadinessRefusesAnImpossibleDifferenceReport(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		mutate  func(*transport.DNSBackendRuntimeState)
		refused bool
	}{
		{
			name: "a directive CelikPanel does not manage",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignOptions[0].Directive = "listen-on"
			},
			refused: true,
		},
		{
			name: "a refusal code the panel has no words for",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignOptions[0].Refusal = "because_i_said_so"
			},
			refused: true,
		},
		{
			name: "a value that is not one printable line",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignOptions[0].Found = "no\nrecursion yes;"
			},
			refused: true,
		},
		{
			name: "a file that is not an absolute path",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignOptions[0].File = "named.conf.options"
			},
			refused: true,
		},
		{
			name: "a report about an engine CelikPanel already manages",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.Running, state.Managed = true, true
			},
			refused: true,
		},
		{
			name:    "the honest report",
			mutate:  func(state *transport.DNSBackendRuntimeState) {},
			refused: false,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			bind := transport.DNSBackendRuntimeState{
				Engine: transport.DNSEngineBIND, Unit: "named.service",
				Installed: true, ForeignOptions: handConfiguredBINDForeignOptions(),
			}
			testCase.mutate(&bind)
			response := transport.DNSBackendReadinessResponse{
				Engines: []transport.DNSBackendRuntimeState{
					{Engine: transport.DNSEnginePowerDNS, Unit: "pdns.service"},
					bind,
				},
			}
			_, _, _, err := validateDNSBackendReadiness(response)
			if testCase.refused != (err != nil) {
				t.Fatalf("refused=%v err=%v", testCase.refused, err)
			}
		})
	}
}
