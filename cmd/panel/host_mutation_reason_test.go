package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// R-068. The agent works out which of three things is blocking a host change -
// somebody else's package manager, another CelikPanel change, or a lock that
// nothing released - and the panel used to drop that and tell the operator one
// sentence covering all three.
//
// The three want different things of the operator, and one of them does not end
// by waiting. So what is tested here is not the wording: it is that the reason
// survives the trip from the agent's answer to the refusal the browser reads.
//
// R-068. Agent, bir makine degisikligini uc seyden hangisinin engelledigini
// hesaplar ve panel bunu dusurup operatore ucunu birden kapsayan tek bir cumle
// soyluyordu. Burada sinanan sey sozler degil, gerekcenin yolculugu tamamlamasi.
func TestTheRefusalCarriesWhichOfThreeItIs(t *testing.T) {
	for _, reason := range []string{
		transport.HostMutationReasonPackageManager,
		transport.HostMutationReasonAgentMutation,
		transport.HostMutationReasonHostLock,
	} {
		t.Run(reason, func(t *testing.T) {
			err := serviceMutationResponseError(agentMutationResponse{
				ErrorCode: transport.HostMutationBusy,
				Reason:    reason,
			})

			// Everything that tested for the sentinel still works: the reason
			// rides on the error, it does not replace it.
			// Nobetciyi sinayan her sey calismaya devam eder.
			if !errors.Is(err, errHostMutationBusy) {
				t.Fatalf("the refusal stopped being a host-mutation-busy error: %v", err)
			}

			recorder := httptest.NewRecorder()
			writeServerError(recorder, err)

			var body struct {
				Error  string `json:"error"`
				Code   string `json:"code"`
				Reason string `json:"reason"`
			}
			if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &body); decodeErr != nil {
				t.Fatalf("decode %q: %v", recorder.Body.String(), decodeErr)
			}
			if body.Code != errCodeHostMutationBusy {
				t.Fatalf("code = %q", body.Code)
			}
			if body.Reason != reason {
				t.Fatalf("reason = %q, want %q - the browser cannot say which without it", body.Reason, reason)
			}
			if body.Error == hostMutationBusyGenericMessage {
				t.Fatalf("a named reason still produced the sentence that covers all three: %q", body.Error)
			}
		})
	}
}

// A refusal that names no reason keeps the sentence that covers all of them.
// An older agent produces exactly this, and it must not become worse than it
// was.
// Gerekce adlandirmayan bir ret, hepsini kapsayan cumleyi korur.
func TestARefusalWithNoReasonKeepsTheSentenceThatCoversAll(t *testing.T) {
	err := serviceMutationResponseError(agentMutationResponse{
		ErrorCode: transport.HostMutationBusy,
	})
	recorder := httptest.NewRecorder()
	writeServerError(recorder, err)

	var body struct {
		Error  string `json:"error"`
		Code   string `json:"code"`
		Reason string `json:"reason"`
	}
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &body); decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if body.Reason != "" {
		t.Fatalf("a reason was invented: %q", body.Reason)
	}
	if body.Error != hostMutationBusyGenericMessage {
		t.Fatalf("error = %q, want the sentence that covers all three", body.Error)
	}
}

// The lock nobody released is the one that matters: waiting is the wrong
// instruction for it, so its sentence must not be a variation on "wait".
// Kimsenin birakmadigi kilit onemli olan: beklemek onun icin yanlis talimat.
func TestTheHoldThatDoesNotEndDoesNotTellTheOperatorToWait(t *testing.T) {
	held := hostMutationBusyMessages[transport.HostMutationReasonHostLock]
	if held == "" {
		t.Fatal("the held-lock refusal has no sentence of its own")
	}
	if !strings.Contains(strings.ToLower(held), "will not clear by waiting") {
		t.Fatalf("the held-lock sentence does not say waiting is the wrong move: %q", held)
	}
	for _, waiting := range []string{
		transport.HostMutationReasonPackageManager,
		transport.HostMutationReasonAgentMutation,
	} {
		message := hostMutationBusyMessages[waiting]
		if !strings.Contains(strings.ToLower(message), "try again") {
			t.Fatalf("%s does not tell the operator to try again: %q", waiting, message)
		}
	}
}

// Every reason the panel has words for is a reason the agent can actually
// send. A sentence for a code nothing produces is a sentence nobody reads.
// Panelin sozu olan her gerekce, agent'in gercekten gonderebilecegi bir gerekce.
func TestEverySentenceAnswersAReasonThatExists(t *testing.T) {
	known := map[string]bool{
		transport.HostMutationReasonPackageManager: true,
		transport.HostMutationReasonAgentMutation:  true,
		transport.HostMutationReasonHostLock:       true,
		transport.HostMutationReasonPanelOperation: true,
	}
	for reason := range hostMutationBusyMessages {
		if !known[reason] {
			t.Errorf("there are words for %q, which nothing sends", reason)
		}
	}
	if len(hostMutationBusyMessages) != len(known) {
		t.Errorf("%d sentences for %d reasons: %s",
			len(hostMutationBusyMessages), len(known),
			fmt.Sprint(hostMutationBusyMessages))
	}
}
