package main

import (
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func unmanagedBINDRuntimesWithViews(
	views *transport.DNSForeignEngineViews,
) map[transport.DNSEngine]transport.DNSBackendRuntimeState {
	runtimes := stoppedUnmanagedBINDRuntimes()
	bind := runtimes[transport.DNSEngineBIND]
	bind.ForeignViews = views
	runtimes[transport.DNSEngineBIND] = bind
	return runtimes
}

// A server configured with views is refused on the screen the operator is
// standing on, before a preview token exists - not by the configuration check
// at the end of the work (register R-044).
//
// View ile yapılandırılmış bir sunucu, operatörün üzerinde durduğu ekranda,
// daha bir önizleme belirteci yokken reddedilir - işin sonundaki yapılandırma
// denetimi tarafından değil (defter R-044).
func TestDNSEnginePreviewBlocksAServerConfiguredWithViews(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = unmanagedBINDRuntimesWithViews(
		&transport.DNSForeignEngineViews{
			Finding: transport.DNSForeignViewDeclared,
			File:    "/etc/bind/named.conf.local", Line: 12,
		},
	)
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if preview.Action != dnsEngineActionAdoptUnmanaged {
		t.Fatalf("action=%q, want a takeover", preview.Action)
	}
	if !hasDNSEngineBlocker(preview, dnsEngineViewsBlocker) {
		t.Fatalf("blockers=%+v, want %s", preview.Blockers, dnsEngineViewsBlocker)
	}
	if preview.PreviewToken != "" {
		t.Fatal("a blocked preview must hand out no token")
	}
	// A refusal the operator cannot act on is the defect this work exists to
	// fix, so the one place to look travels with it.
	//
	// Operatörün üzerinde işlem yapamayacağı bir ret, bu işin düzeltmek için var
	// olduğu kusurdur; bu yüzden bakılacak tek yer retle birlikte yol alır.
	if preview.ViewFinding == nil ||
		preview.ViewFinding.Finding != transport.DNSForeignViewDeclared ||
		preview.ViewFinding.File != "/etc/bind/named.conf.local" ||
		preview.ViewFinding.Line != 12 {
		t.Fatalf("view finding=%+v, want the file and line of the view",
			preview.ViewFinding)
	}
}

// A configuration CelikPanel could not read whole is its own refusal with its
// own words: "no views found" would be a guess, and the takeover is built to
// make none.
//
// CelikPanel'in bütünüyle okuyamadığı bir yapılandırma, kendi sözcükleriyle
// kendi reddidir: "view bulunamadı" bir tahmin olurdu ve devralma hiç tahmin
// yapmamak için kurulmuştur.
func TestDNSEnginePreviewBlocksAConfigurationItCouldNotReadWhole(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = unmanagedBINDRuntimesWithViews(
		&transport.DNSForeignEngineViews{
			Finding: transport.DNSForeignViewUnreadable,
			File:    "/etc/bind/named.conf", Line: 3,
		},
	)
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if !hasDNSEngineBlocker(preview, dnsEngineConfigUnreadableBlocker) {
		t.Fatalf("blockers=%+v, want %s",
			preview.Blockers, dnsEngineConfigUnreadableBlocker)
	}
	if hasDNSEngineBlocker(preview, dnsEngineViewsBlocker) {
		t.Fatal("an unreadable include is not a declared view")
	}
	if preview.PreviewToken != "" {
		t.Fatal("a blocked preview must hand out no token")
	}
	if preview.ViewFinding == nil || preview.ViewFinding.Line != 3 {
		t.Fatalf("view finding=%+v, want the include statement to look at",
			preview.ViewFinding)
	}
}

// The takeover R-042 built must still work. A host that declares no views is
// exactly the host that whole feature exists for, and it must reach a token.
//
// R-042'nin kurduğu devralma hâlâ çalışmalıdır. Hiç view bildirmeyen bir
// sunucu, tam da o özelliğin var olma sebebidir ve bir belirtece ulaşmalıdır.
func TestDNSEnginePreviewIsUnchangedWhenNoViewsAreDeclared(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.runtimes = unmanagedBINDRuntimesWithViews(nil)
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(
		t, panel, transport.DNSEngineBIND, nil, 0,
	)
	if recorder.Code != http.StatusOK {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if len(preview.Blockers) != 0 || preview.PreviewToken == "" {
		t.Fatalf("preview=%+v, want an unblocked takeover", preview)
	}
	if preview.ViewFinding != nil {
		t.Fatalf("view finding=%+v, want nothing", preview.ViewFinding)
	}
	if body := recorder.Body.String(); strings.Contains(body, `"view_finding"`) {
		t.Fatalf("a finding that is not there must not reach the browser: %s", body)
	}
}

// A view report the panel cannot describe must make readiness unavailable
// rather than reach a screen as a half-understood fact. This one decides
// whether a takeover may happen at all, so a malformed answer must never be
// able to read as "no views".
//
// Panelin anlatamayacağı bir view bildirimi, yarım anlaşılmış bir olgu olarak
// bir ekrana ulaşmak yerine hazırlığı erişilemez kılmalıdır. Bu cevap bir
// devralmanın hiç olup olmayacağına karar verir; dolayısıyla bozuk bir cevap
// asla "view yok" diye okunamamalıdır.
func TestDNSBackendReadinessRefusesAnImpossibleViewReport(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		mutate  func(*transport.DNSBackendRuntimeState)
		refused bool
	}{
		{
			name: "a finding the panel has no words for",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignViews.Finding = "probably"
			},
			refused: true,
		},
		{
			name: "a file that is not an absolute path",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignViews.File = "named.conf.local"
			},
			refused: true,
		},
		{
			name: "a line that is not a line",
			mutate: func(state *transport.DNSBackendRuntimeState) {
				state.ForeignViews.Line = 0
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
				Installed: true,
				ForeignViews: &transport.DNSForeignEngineViews{
					Finding: transport.DNSForeignViewDeclared,
					File:    "/etc/bind/named.conf.local", Line: 12,
				},
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
