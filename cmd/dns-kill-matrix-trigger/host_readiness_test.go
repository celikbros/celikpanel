package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Batch 8 c08 recorded only code and message of a HOST_MUTATION_BUSY refusal;
// the Agent's typed reason must be printed and recorded as well.
func TestBusyBeginRecordsTheAgentReason(t *testing.T) {
	request := mustScenarioRequest(t, standaloneScenario(
		"bind", transport.DNSEnginePowerDNS, transport.DNSEngineBIND, 1,
	))
	switchCalls := 0
	call := func(_ context.Context, method string, _, output any) error {
		if method != "Agent.BeginServiceMutation" {
			switchCalls++
			return nil
		}
		response := output.(*transport.ServiceMutationResponse)
		response.ErrorCode = transport.HostMutationBusy
		response.Reason = transport.HostMutationReasonAgentMutation
		response.Error = "another server change or package-manager task is still running"
		return nil
	}
	_, err := runRPCSwitch(
		context.Background(), testRequestID, testOwnerID, request, false, time.Hour, call,
	)
	if err == nil || switchCalls != 0 {
		t.Fatalf("busy begin: err=%v switch calls=%d", err, switchCalls)
	}
	want := `begin DNS switch mutation: agent response code="HOST_MUTATION_BUSY" ` +
		`reason="agent_mutation_active" error="another server change or package-manager task is still running"`
	if err.Error() != want {
		t.Fatalf("error text:\n got %s\nwant %s", err, want)
	}
	event := triggerEvent{Event: "rpc-switch-ended", Error: err.Error()}.withAgentRefusal(err)
	encoded, _ := json.Marshal(event)
	for _, field := range []string{
		`"agent_error_code":"HOST_MUTATION_BUSY"`, `"agent_reason":"agent_mutation_active"`,
	} {
		if !strings.Contains(string(encoded), field) {
			t.Fatalf("event %s lacks %s", encoded, field)
		}
	}
	if strings.Contains(string(encoded), "mutation_hold") {
		t.Fatalf("an empty mutation hold was recorded: %s", encoded)
	}
}

func TestAgentRefusalWithoutReasonSaysSoAndKeepsTheHold(t *testing.T) {
	err := responseError(transport.ServiceMutationResponse{
		ErrorCode: "X", Error: "refused", MutationHold: "ledger_unavailable",
	})
	if err == nil || err.Error() != `agent response code="X" reason="" mutation_hold="ledger_unavailable" error="refused"` {
		t.Fatalf("refusal text: %v", err)
	}
	if responseError(transport.ServiceMutationResponse{}) != nil {
		t.Fatal("an empty response became an error")
	}
	plain := triggerEvent{Event: "e"}.withAgentRefusal(errors.New("transport failed"))
	if plain.AgentErrorCode != "" || plain.AgentReason != "" {
		t.Fatalf("a transport error was recorded as an Agent refusal: %+v", plain)
	}
}

func TestHostReadinessIsOneReadOnlyCall(t *testing.T) {
	cases := []struct {
		name     string
		response transport.HostMutationReadinessResponse
		ready    bool
		wantErr  bool
	}{
		{"idle", transport.HostMutationReadinessResponse{Ready: true}, true, false},
		{"busy", transport.HostMutationReadinessResponse{
			Code: transport.HostMutationBusy, Reason: transport.HostMutationReasonPackageManager,
		}, false, false},
		{"ready with a code", transport.HostMutationReadinessResponse{
			Ready: true, Code: transport.HostMutationBusy,
		}, false, true},
		{"not ready without a code", transport.HostMutationReadinessResponse{}, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var methods []string
			call := func(_ context.Context, method string, input, output any) error {
				methods = append(methods, method)
				if _, ok := input.(*transport.Empty); !ok {
					return fmt.Errorf("unexpected input %T", input)
				}
				*output.(*transport.HostMutationReadinessResponse) = tc.response
				return nil
			}
			result, err := runHostReadiness(context.Background(), call)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v", err)
			}
			if len(methods) != 1 || methods[0] != "Agent.ServiceMutationReadiness" {
				t.Fatalf("calls %v: the probe must make exactly one readiness call", methods)
			}
			if !tc.wantErr && (result.Ready != tc.ready || result.Schema != hostReadinessSchema ||
				result.Reason != tc.response.Reason) {
				t.Fatalf("result %+v", result)
			}
		})
	}
	if _, err := runHostReadiness(context.Background(), func(context.Context, string, any, any) error {
		return errors.New("socket gone")
	}); err == nil {
		t.Fatal("an RPC failure was reported as an answer")
	}
}

func TestAgentSocketAbsentTellsStoppedAgentFromUncertainFailure(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "agent.sock")
	if !agentSocketAbsent(missing, nil) {
		t.Fatal("a missing socket was not reported as an absent Agent")
	}
	present := filepath.Join(directory, "present.sock")
	if err := os.WriteFile(present, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if agentSocketAbsent(present, nil) {
		t.Fatal("an existing socket path was reported absent before any call")
	}
	refused := fmt.Errorf("cannot reach agent socket: %w", &net.OpError{
		Op: "dial", Net: "unix", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED),
	})
	if !agentSocketAbsent(present, refused) {
		t.Fatal("a refused connection was not reported as an absent Agent")
	}
	if agentSocketAbsent(present, errors.New("handshake failed")) {
		t.Fatal("another RPC failure was reported as an absent Agent")
	}
	result := gateProbeAgentAbsent("cell", "q", missing, nil)
	if result.Gate != gateUnknown || !strings.Contains(result.Detail,
		"the Agent is not running on this prepared guest; the controller probes the gate itself after it starts the Agent") {
		t.Fatalf("absent-Agent gate probe answer: %+v", result)
	}
	if exitAgentAbsent == exitUncertain || exitAgentAbsent == exitUsage || exitAgentAbsent == 0 {
		t.Fatal("the absent-Agent exit code is not distinct")
	}
}
