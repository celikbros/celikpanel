package main

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Item 4c (2026-09-30): after a first install the Agent rolled back, the
// engine card retries with a NEW request id and reaches the Agent with it; a
// repeat of the rolled-back request id is a replay that never reaches the
// Agent again. The Agent's ledger keeps the failed request terminal (a
// same-id begin returns the finished job, which the Panel refuses), so the
// new request is the product's retry. The card's web client mints a fresh
// request id per preview.
func TestDNSEngineRetryAfterRolledBackFirstInstallUsesANewRequest(t *testing.T) {
	panel := newDNSPanelForTest(t)
	setDNSIdentityForTest(t, panel, "standalone")
	agent := newDNSEngineTestAgent()
	agent.switchError = "DNS engine switch did not complete; inspect the agent log"
	// Rollback standby: the packages stay installed, BIND is not running.
	agent.switchErrorLeavesPackage = true
	attachDNSEngineTestAgent(t, panel, agent)

	preview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEngineBIND, nil, 0)
	if recorder.Code != http.StatusOK || len(preview.Blockers) != 0 {
		t.Fatalf("preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	first := strings.Repeat("a", 32)
	commit := commitDNSEngineSwitch(t, panel, first, transport.DNSEngineBIND, nil, 0, preview.PreviewToken, false)
	if commit.Code != http.StatusConflict ||
		!strings.Contains(commit.Body.String(), errCodeDNSEngineChangeNotCommitted) {
		t.Fatalf("rolled-back first install status=%d body=%s", commit.Code, commit.Body.String())
	}

	// The same request id again: a replay of the recorded outcome, no second
	// Agent mutation.
	replay := commitDNSEngineSwitch(t, panel, first, transport.DNSEngineBIND, nil, 0, preview.PreviewToken, false)
	agent.mu.Lock()
	calls := agent.switchCalls
	agent.mu.Unlock()
	if calls != 1 {
		t.Fatalf("a replayed rolled-back request reached the Agent again: calls=%d replay status=%d body=%s",
			calls, replay.Code, replay.Body.String())
	}

	// The retry: a new review and a new request id.
	agent.mu.Lock()
	agent.switchError = ""
	agent.mu.Unlock()
	state, err := readDNSEngineDBState(context.Background(), panel.db.GetDB())
	if err != nil {
		t.Fatal(err)
	}
	retryPreview, recorder := requestDNSEnginePreview(t, panel, transport.DNSEngineBIND, nil, state.Revision)
	if recorder.Code != http.StatusOK || len(retryPreview.Blockers) != 0 {
		t.Fatalf("retry preview status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	second := strings.Repeat("b", 32)
	retry := commitDNSEngineSwitch(t, panel, second, transport.DNSEngineBIND, nil, state.Revision, retryPreview.PreviewToken, false)
	if retry.Code != http.StatusOK {
		t.Fatalf("retry status=%d body=%s", retry.Code, retry.Body.String())
	}
	agent.mu.Lock()
	defer agent.mu.Unlock()
	if len(agent.switchRequests) != 2 ||
		agent.switchRequests[0].ServiceMutationBinding.MutationRequestID != first ||
		agent.switchRequests[1].ServiceMutationBinding.MutationRequestID != second {
		t.Fatalf("agent requests = %+v", agent.switchRequests)
	}
	// The retry is a new review: same install, bound to the revision the
	// rolled-back attempt advanced to.
	retried := agent.switchRequests[1]
	if retried.TargetEngine != transport.DNSEngineBIND || retried.SourceEngine != "" ||
		retried.TargetEpoch != agent.switchRequests[0].TargetEpoch ||
		retried.SourceRevision != state.Revision {
		t.Fatalf("the retry is not the same install at the current revision: %+v", retried)
	}
}
