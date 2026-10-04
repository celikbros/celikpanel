package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Batch 6b cell c6: apt-get failed with dpkg's own reason and the Panel and
// the ledger job said only that the switch did not complete. The Agent now
// sends an operator sentence behind a fixed prefix; the Panel records exactly
// that sentence in the ledger job and returns it as the refusal's detail.
// Unknown agent text is still omitted.
func TestDNSEngineAgentNamedRejectionReachesLedgerAndResponse(t *testing.T) {
	sentence := "The server's package manager did not install bind9 (exit status 100). " +
		"The server owner fixes the problem it names on the server, then starts the same change again. " +
		"Package manager: dpkg: unknown system group 'bind' in statoverride file; the system group got removed"
	classified := newDNSEngineAgentRejectedError(dnsEngineSwitchIncompleteNamedPrefix + sentence + "\n")
	if classified.diagnosticCode != "backend_switch_failed_named" ||
		classified.hostSentence != sentence {
		t.Fatalf("named rejection classified as %+v", classified)
	}
	if empty := newDNSEngineAgentRejectedError(dnsEngineSwitchIncompleteNamedPrefix + "  \n"); empty.hostSentence != "" ||
		empty.diagnosticCode != "unclassified_detail_omitted" {
		t.Fatalf("empty named rejection = %+v", empty)
	}

	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.switchError = dnsEngineSwitchIncompleteNamedPrefix + sentence
	attachDNSEngineTestAgent(t, panel, agent)
	preview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEngineBIND, nil, 0)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	requestID := strings.Repeat("c", 32)
	commit := commitDNSEngineSwitch(
		t, panel, requestID, transport.DNSEngineBIND, nil, 0, preview.PreviewToken, false,
	)
	var body apiErrorBody
	if commit.Code != http.StatusConflict ||
		json.Unmarshal(commit.Body.Bytes(), &body) != nil ||
		body.Code != errCodeDNSEngineChangeNotCommitted ||
		len(body.Details) != 1 || body.Details[0] != sentence ||
		!strings.HasSuffix(body.Error, sentence) ||
		body.PartialSuccess || body.MutationApplied {
		t.Fatalf("named rejection status=%d body=%s", commit.Code, commit.Body.String())
	}
	agent.durableMutationRPCFixture.mu.Lock()
	job := agent.durableMutationRPCFixture.jobs[requestID]
	agent.durableMutationRPCFixture.mu.Unlock()
	if job == nil || job.Status != agentMutationFailed ||
		job.ErrorCode != errCodeServiceOperationFailed || job.ErrorMessage != sentence {
		t.Fatalf("ledger job did not record the operator sentence: %+v", job)
	}
}
