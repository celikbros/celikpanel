package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"net"
	"net/rpc"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestAgentRPCPoliciesAreExplicitAndValid(t *testing.T) {
	effects := map[agentRPCEffect]int{}
	for method, policy := range agentRPCPolicies {
		if err := policy.validate(); err != nil {
			t.Errorf("%s: %v", method, err)
		}
		effects[policy.effect]++
		if (policy.effect == agentRPCEffectHostMutation ||
			policy.effect == agentRPCEffectHostRepairMutation) &&
			policy.capability == "" {
			t.Errorf("%s: host mutation has no capability", method)
		}
	}
	for _, effect := range []agentRPCEffect{
		agentRPCEffectRead,
		agentRPCEffectControl,
		agentRPCEffectHostMutation,
		agentRPCEffectHostRepairMutation,
	} {
		if effects[effect] == 0 {
			t.Errorf("effect %d has no registered methods", effect)
		}
	}
	repoStatus, err := agentRPCPolicyForMethod("Agent.RepoStatus")
	if err != nil {
		t.Fatal(err)
	}
	if repoStatus.effect != agentRPCEffectHostRepairMutation ||
		repoStatus.capability != agentRPCCapabilityPackageLifecycle {
		t.Fatalf("RepoStatus policy = %+v, want package repair mutation", repoStatus)
	}
	if _, err := agentRPCPolicyForMethod("Agent.UnreviewedFutureMethod"); !errors.Is(err, errAgentRPCPolicyMissing) {
		t.Fatalf("unreviewed method error = %v, want fail-closed policy error", err)
	}
}

func TestProductionAgentRPCLiteralsHavePolicies(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve test source path")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	directories := []string{
		filepath.Join(repoRoot, "cmd", "panel"),
		filepath.Join(repoRoot, "internal", "services"),
	}
	found := map[string]string{}
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			name := entry.Name()
			if entry.IsDir() || !strings.HasSuffix(name, ".go") ||
				strings.HasSuffix(name, "_test.go") ||
				name == "agent_rpc_policy.go" {
				continue
			}
			path := filepath.Join(directory, name)
			file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
			if err != nil {
				t.Fatalf("parse %s: %v", path, err)
			}
			ast.Inspect(file, func(node ast.Node) bool {
				literal, ok := node.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					return true
				}
				value, err := strconv.Unquote(literal.Value)
				if err == nil && strings.HasPrefix(value, "Agent.") &&
					!strings.ContainsAny(value, " \t\r\n") {
					found[value] = path
				}
				return true
			})
		}
	}
	for method, path := range found {
		if _, err := agentRPCPolicyForMethod(method); err != nil {
			t.Errorf("%s in %s: %v", method, path, err)
		}
	}
	for method := range agentRPCPolicies {
		if _, ok := found[method]; !ok {
			t.Errorf("registered Agent RPC %s has no production caller", method)
		}
	}
	if _, ok := found["Agent.CreateSite"]; !ok {
		t.Fatal("internal/services Agent.CreateSite literal was not inventoried")
	}
}

func TestAgentRPCPlatformFirewall(t *testing.T) {
	read, _ := agentRPCPolicyForMethod("Agent.GetConfig")
	control, _ := agentRPCPolicyForMethod("Agent.FinishServiceMutation")
	mutation, _ := agentRPCPolicyForMethod("Agent.UpdateConfig")
	repair, _ := agentRPCPolicyForMethod("Agent.RepoStatus")

	for _, policy := range []agentRPCPolicy{read, control} {
		if err := authorizeAgentRPCPolicyForPackageFamily(policy, "dnf"); err != nil {
			t.Errorf("read/control blocked on dnf: %v", err)
		}
	}
	for _, family := range []string{"apt", "pacman"} {
		if err := authorizeAgentRPCPolicyForPackageFamily(mutation, family); err != nil {
			t.Errorf("mutation blocked on established family %s: %v", family, err)
		}
	}
	for _, policy := range []agentRPCPolicy{mutation, repair} {
		err := authorizeAgentRPCPolicyForPackageFamily(policy, "dnf")
		if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
			t.Errorf("dnf mutation error = %v, want capability denial", err)
		}
	}
	if err := authorizeAgentRPCPolicyForPackageFamily(mutation, ""); !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("empty identity error = %v, want identity unavailable", err)
	}
}

type policyDispatchTestAgent struct {
	hostResponse transport.HostPlatformResponse
	hostErr      error
	family       string
	hostCalls    int
	familyCalls  int
	readCalls    int
	controlCalls int
	updateCalls  int
	repairCalls  int
}

func (a *policyDispatchTestAgent) HostPlatform(_ *transport.Empty, out *transport.HostPlatformResponse) error {
	a.hostCalls++
	if a.hostErr != nil {
		return a.hostErr
	}
	*out = a.hostResponse
	return nil
}

func (a *policyDispatchTestAgent) PkgFamily(_ *transport.Empty, out *string) error {
	a.familyCalls++
	*out = a.family
	return nil
}

func (a *policyDispatchTestAgent) GetConfig(_ *transport.Empty, out *bool) error {
	a.readCalls++
	*out = true
	return nil
}

func (a *policyDispatchTestAgent) BeginServiceMutation(_ *transport.Empty, out *bool) error {
	a.controlCalls++
	*out = true
	return nil
}

func (a *policyDispatchTestAgent) UpdateConfig(_ *transport.Empty, out *bool) error {
	a.updateCalls++
	*out = true
	return nil
}

func (a *policyDispatchTestAgent) RepoStatus(_ *transport.Empty, out *bool) error {
	a.repairCalls++
	*out = true
	return nil
}

type legacyPolicyDispatchTestAgent struct {
	family      string
	familyCalls int
	updateCalls int
}

func (a *legacyPolicyDispatchTestAgent) PkgFamily(_ *transport.Empty, out *string) error {
	a.familyCalls++
	*out = a.family
	return nil
}

func (a *legacyPolicyDispatchTestAgent) UpdateConfig(_ *transport.Empty, out *bool) error {
	a.updateCalls++
	*out = true
	return nil
}

func newPolicyDispatchTestPanel(t *testing.T, agent any) *Panel {
	t.Helper()
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", agent); err != nil {
		t.Fatal(err)
	}
	connector := func(ctx context.Context) (*rpc.Client, error) {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		serverConn, clientConn := net.Pipe()
		go server.ServeConn(serverConn)
		return rpc.NewClient(clientConn), nil
	}
	initial, err := connector(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = initial.Close() })
	return &Panel{
		agentClient: transport.NewReconnectingClientWithContextConnector(initial, connector),
	}
}

func rhelPolicyTestIdentity() transport.HostPlatformResponse {
	return transport.HostPlatformResponse{
		DistroFamily: "rhel", PackageManager: "dnf", ServiceManager: "systemd",
		DistroID: "rocky", VersionID: "9.6", Architecture: "amd64",
	}
}

func debianPolicyTestIdentity() transport.HostPlatformResponse {
	return transport.HostPlatformResponse{
		DistroFamily: "debian", PackageManager: "apt", ServiceManager: "systemd",
		DistroID: "debian", VersionID: "12", Architecture: "amd64",
	}
}

func TestDNFMutationIsDeniedBeforeRawDispatch(t *testing.T) {
	agent := &policyDispatchTestAgent{hostResponse: rhelPolicyTestIdentity(), family: "dnf"}
	panel := newPolicyDispatchTestPanel(t, agent)
	var out bool
	err := panel.callAgentContext(context.Background(), "Agent.UpdateConfig", &transport.Empty{}, &out)
	if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("error = %v, want capability denial", err)
	}
	if agent.hostCalls != 1 || agent.familyCalls != 0 || agent.updateCalls != 0 {
		t.Fatalf("calls host=%d family=%d update=%d, want 1/0/0",
			agent.hostCalls, agent.familyCalls, agent.updateCalls)
	}
}

func TestDNFRepairIsDeniedBeforeRawDispatch(t *testing.T) {
	agent := &policyDispatchTestAgent{hostResponse: rhelPolicyTestIdentity()}
	panel := newPolicyDispatchTestPanel(t, agent)
	var out bool
	err := panel.callAgentContext(context.Background(), "Agent.RepoStatus", &transport.Empty{}, &out)
	if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("error = %v, want capability denial", err)
	}
	if agent.hostCalls != 1 || agent.repairCalls != 0 {
		t.Fatalf("calls host=%d repair=%d, want 1/0", agent.hostCalls, agent.repairCalls)
	}
}

func TestUnknownFamilyAndNilClientFailClosedBeforeMutationDispatch(t *testing.T) {
	panel := &Panel{pkgFamilyVal: "zypper"}
	var out bool
	err := panel.callAgentContext(context.Background(), "Agent.UpdateConfig", &transport.Empty{}, &out)
	if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("unknown-family error = %v, want capability denial", err)
	}
	panel.pkgFamilyVal = "apt"
	err = panel.callAgentContext(context.Background(), "Agent.UpdateConfig", &transport.Empty{}, &out)
	if !errors.Is(err, errAgentRPCClientUnavailable) {
		t.Fatalf("nil-client error = %v, want client unavailable", err)
	}
}

func TestDNFReadAndDurableControlRemainAvailable(t *testing.T) {
	agent := &policyDispatchTestAgent{hostResponse: rhelPolicyTestIdentity()}
	panel := newPolicyDispatchTestPanel(t, agent)
	var out bool
	if err := panel.callAgentContext(context.Background(), "Agent.GetConfig", &transport.Empty{}, &out); err != nil {
		t.Fatal(err)
	}
	if err := panel.callAgentContext(context.Background(), "Agent.BeginServiceMutation", &transport.Empty{}, &out); err != nil {
		t.Fatal(err)
	}
	if agent.hostCalls != 0 || agent.readCalls != 1 || agent.controlCalls != 1 {
		t.Fatalf("calls host=%d read=%d control=%d, want 0/1/1",
			agent.hostCalls, agent.readCalls, agent.controlCalls)
	}
}

func TestVerifiedAPTMutationPreservesCurrentBehavior(t *testing.T) {
	agent := &policyDispatchTestAgent{hostResponse: debianPolicyTestIdentity()}
	panel := newPolicyDispatchTestPanel(t, agent)
	var out bool
	if err := panel.callAgentContext(context.Background(), "Agent.UpdateConfig", &transport.Empty{}, &out); err != nil {
		t.Fatal(err)
	}
	if agent.hostCalls != 1 || agent.updateCalls != 1 {
		t.Fatalf("calls host=%d update=%d, want 1/1", agent.hostCalls, agent.updateCalls)
	}
}

func TestLegacyFamilyFallbackAllowsAPTButBlocksDNF(t *testing.T) {
	for _, test := range []struct {
		family     string
		wantDenied bool
	}{
		{family: "apt"},
		{family: "dnf", wantDenied: true},
	} {
		t.Run(test.family, func(t *testing.T) {
			agent := &legacyPolicyDispatchTestAgent{family: test.family}
			panel := newPolicyDispatchTestPanel(t, agent)
			var out bool
			err := panel.callAgentContext(context.Background(), "Agent.UpdateConfig", &transport.Empty{}, &out)
			if test.wantDenied {
				if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
					t.Fatalf("error = %v, want capability denial", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
			wantUpdates := 1
			if test.wantDenied {
				wantUpdates = 0
			}
			if agent.familyCalls != 1 || agent.updateCalls != wantUpdates {
				t.Fatalf("calls family=%d update=%d, want 1/%d",
					agent.familyCalls, agent.updateCalls, wantUpdates)
			}
		})
	}
}

func TestInvalidVerifiedIdentityDoesNotDowngradeToFamilyOnly(t *testing.T) {
	agent := &policyDispatchTestAgent{
		hostResponse: transport.HostPlatformResponse{
			DistroFamily: "rhel", PackageManager: "apt", ServiceManager: "systemd",
			DistroID: "rocky", VersionID: "9.6", Architecture: "amd64",
		},
		family: "apt",
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	var out bool
	err := panel.callAgentContext(context.Background(), "Agent.UpdateConfig", &transport.Empty{}, &out)
	if !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("error = %v, want identity unavailable", err)
	}
	if agent.hostCalls != 1 || agent.familyCalls != 0 || agent.updateCalls != 0 {
		t.Fatalf("calls host=%d family=%d update=%d, want 1/0/0",
			agent.hostCalls, agent.familyCalls, agent.updateCalls)
	}
}
