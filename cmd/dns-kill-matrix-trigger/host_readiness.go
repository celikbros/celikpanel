package main

// Read-only host observations for the kill-matrix controller.
//
//   - rpc-host-readiness asks the Agent's advisory Agent.ServiceMutationReadiness
//     whether the host is idle (the same idle check BeginServiceMutation repeats
//     under its lease: ledger active request, host lock, package manager). It
//     never begins, finishes or cancels a mutation. The controller polls it
//     before the measured BeginServiceMutation (batch 8 c08: Begin was refused
//     HOST_MUTATION_BUSY 0.1 s after the Agent started).
//   - Both this command and rpc-gate-probe tell "no Agent listens on the socket"
//     (exitAgentAbsent) apart from an uncertain RPC failure (exitUncertain).
//
// Denetleyici için salt-okur ana makine gözlemleri: ölçülen işlemden önce
// Agent'ın kendi hazır olma yanıtını sorar; hiçbir değişikliği başlatmaz ya da
// iptal etmez. Soket yoksa ayrı bir çıkış koduyla açıkça söyler.

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	hostReadinessSchema = "celikpanel/dns-kill-matrix-host-readiness/v1"

	agentAbsentDetail = "the Agent is not running on this prepared guest; the controller " +
		"probes the gate itself after it starts the Agent (guest_bootstrap.py run-prepared); " +
		"nothing was asked or changed"
)

// agentSocketAbsent reports whether no Agent can be listening at path: the
// socket file does not exist, or a connection to it was refused.
func agentSocketAbsent(path string, callErr error) bool {
	if _, err := os.Lstat(path); errors.Is(err, os.ErrNotExist) {
		return true
	}
	return callErr != nil && errors.Is(callErr, syscall.ECONNREFUSED)
}

type hostReadinessResult struct {
	Schema string `json:"schema"`
	Ready  bool   `json:"ready"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
	Error  string `json:"error,omitempty"`
}

// runHostReadiness makes exactly one read-only readiness call.
func runHostReadiness(ctx context.Context, call rpcCallFunc) (hostReadinessResult, error) {
	result := hostReadinessResult{Schema: hostReadinessSchema}
	if ctx == nil || call == nil {
		return result, errors.New("the readiness probe needs a context and a caller")
	}
	var response transport.HostMutationReadinessResponse
	if err := call(ctx, "Agent.ServiceMutationReadiness", &transport.Empty{}, &response); err != nil {
		return result, err
	}
	if response.Ready && (response.Code != "" || response.Reason != "") {
		return result, fmt.Errorf(
			"the Agent answered ready with a refusal code %q reason %q", response.Code, response.Reason)
	}
	if !response.Ready && response.Code == "" {
		return result, errors.New("the Agent answered not ready without a code")
	}
	result.Ready, result.Code, result.Reason = response.Ready, response.Code, response.Reason
	return result, nil
}

func runRPCHostReadinessCommand(arguments []string) {
	flags := flag.NewFlagSet("rpc-host-readiness", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	timeout := flags.Duration("timeout", controlTimeout, "bounded probe time")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 || *timeout <= 0 {
		usageError("rpc-host-readiness arguments are invalid")
	}
	socket := transport.AgentSocketPath()
	if agentSocketAbsent(socket, nil) {
		writeResultAndExit(hostReadinessResult{
			Schema: hostReadinessSchema, Error: agentAbsentDetail + " (socket " + socket + ")",
		}, exitAgentAbsent)
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := runHostReadiness(ctx, callProductionAgent)
	if err != nil {
		result.Error = err.Error()
		code := exitUncertain
		if agentSocketAbsent(socket, err) {
			result.Error = agentAbsentDetail + " (socket " + socket + "): " + err.Error()
			code = exitAgentAbsent
		}
		writeResultAndExit(result, code)
	}
	writeResultAndExit(result, 0)
}

// gateProbeAgentAbsent is the standalone probe's answer on a guest whose
// Agent is stopped (a prepared guest): gate unknown, a plain next step.
func gateProbeAgentAbsent(cellID, qualifier, socket string, err error) gateProbeResult {
	detail := agentAbsentDetail + " (socket " + socket + ")"
	if err != nil {
		detail += ": " + err.Error()
	}
	return gateProbeResult{
		Schema: gateProbeSchema, CellID: cellID, ManifestQualifier: qualifier,
		Gate: gateUnknown, Detail: detail,
	}
}

func writeResultAndExit(value any, code int) {
	encoded, _ := json.Marshal(value)
	_, _ = os.Stdout.Write(append(encoded, '\n'))
	if code == exitAgentAbsent {
		_, _ = fmt.Fprintln(os.Stderr, strings.TrimSpace(agentAbsentDetail))
	}
	os.Exit(code)
}
