package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// The S-6 T3 finding. The hold reached the panel in under ten milliseconds and
// the panel log named it, but the public body was an anonymous 502 with
// DNS_ENGINE_STATE_UNVERIFIED — the operator still could not tell a held agent
// from any other unverified outcome. The hold must survive the exact wrapping
// chain production applies and come out as a stable code.
// S-6 T3 bulgusu. Tutulma panele on milisaniyeden kısa sürede ulaştı ve panel
// günlüğü onu adlandırdı; ama halka açık gövde adsız bir 502 ve
// DNS_ENGINE_STATE_UNVERIFIED idi — operatör tutulan bir agent'ı başka herhangi
// bir doğrulanmamış sonuçtan yine ayırt edemedi. Tutulma, üretimin uyguladığı
// sarmalama zincirinden aynen geçip kararlı bir kod olarak çıkmalıdır.
func TestHeldAgentSurvivesTheTerminalWrappingChain(t *testing.T) {
	identity := agentMutationIdentity{
		RequestID:   strings.Repeat("a", 32),
		OwnerID:     strings.Repeat("b", 32),
		Kind:        dnsEngineSwitchKind,
		Target:      "bind",
		PackageName: "dns-engine-switch/v1:sha256:" + strings.Repeat("c", 64),
	}
	frozen := &transport.ServiceMutationJob{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: identity.Kind, Target: identity.Target,
		PackageName: identity.PackageName, Status: "running", Phase: "leased",
	}

	// Exactly what finishExpectedAgentMutationWithin produces when the wait
	// stops on a hold: the held error joined under a finish error, then
	// classified as terminal-uncertain because the observed job is active.
	// finishExpectedAgentMutationWithin'in bekleyiş bir tutulmada durduğunda
	// ürettiğinin tıpkısı: bitirme hatasının altına eklenmiş tutulma hatası,
	// sonra gözlenen iş etkin olduğu için uç-belirsiz olarak sınıflanmış.
	held := &agentMutationHeldError{Hold: transport.MutationHoldLedgerAmbiguous}
	err := payloadBoundMutationTerminalError(identity, frozen, fmt.Errorf(
		"finish durable agent mutation: %w",
		errors.Join(
			errors.New("service mutation manager is fail-closed after an ambiguous ledger write"),
			fmt.Errorf("reconcile terminal status: %w", held),
		),
	))

	if !mutationTerminalUncertain(err) {
		t.Fatal("a held outcome is terminal-uncertain and must not trigger a rollback attempt")
	}
	if !errors.Is(err, errAgentMutationHeld) {
		t.Fatal("errors.Is must still recognise the sentinel through the chain")
	}
	var extracted *agentMutationHeldError
	if !errors.As(err, &extracted) || extracted.Hold != transport.MutationHoldLedgerAmbiguous {
		t.Fatalf("the hold code must be extractable through the chain, got %v", err)
	}
}

// The public body names the hold with a stable code and carries the hold
// reason as a detail — and nothing else. No internal error text, per httperr.go.
// Halka açık gövde tutulmayı kararlı bir kodla adlandırır ve tutulma sebebini
// ayrıntı olarak taşır — başka hiçbir şey değil. httperr.go gereği iç hata
// metni yok.
func TestHeldResponseNamesTheHoldAndLeaksNothing(t *testing.T) {
	for _, hold := range []string{
		transport.MutationHoldLedgerAmbiguous,
		transport.MutationHoldLedgerUnavailable,
	} {
		t.Run(hold, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			writeDNSEngineMutationsHeld(recorder, hold)

			if recorder.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want 503", recorder.Code)
			}
			var body apiErrorBody
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatalf("body is not JSON: %v", err)
			}
			if body.Code != errCodeDNSEngineMutationsHeld {
				t.Fatalf("code = %q, want %q", body.Code, errCodeDNSEngineMutationsHeld)
			}
			if len(body.Details) != 1 || body.Details[0] != hold {
				t.Fatalf("details = %v, want exactly [%s]", body.Details, hold)
			}
			raw := recorder.Body.String()
			for _, internal := range []string{
				"fail-closed", "ambiguous ledger write", "reconcile terminal status",
				"finish durable agent mutation",
			} {
				if strings.Contains(raw, internal) {
					t.Fatalf("internal error text leaked to the client: %q", internal)
				}
			}
		})
	}
}
