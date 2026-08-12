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
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
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

func TestVPNSyncRPCPolicyRequiresV2(t *testing.T) {
	policy, err := agentRPCPolicyForMethod("Agent.SyncVPNPeersV2")
	if err != nil {
		t.Fatal(err)
	}
	if policy.timeout != agentRPCMutationTimeout ||
		policy.effect != agentRPCEffectHostMutation ||
		policy.capability != agentRPCCapabilityVPN {
		t.Fatalf("SyncVPNPeersV2 policy = %+v", policy)
	}
	if _, err := agentRPCPolicyForMethod("Agent.SyncVPNPeers"); !errors.Is(
		err,
		errAgentRPCPolicyMissing,
	) {
		t.Fatalf("legacy SyncVPNPeers policy error = %v, want missing", err)
	}
}

func TestFirewallApplyRPCPolicyRequiresV2(t *testing.T) {
	policy, err := agentRPCPolicyForMethod("Agent.ApplyFirewallV2")
	if err != nil {
		t.Fatal(err)
	}
	if policy.timeout != agentRPCMutationTimeout ||
		policy.effect != agentRPCEffectHostMutation ||
		policy.capability != agentRPCCapabilityFirewall {
		t.Fatalf("ApplyFirewallV2 policy = %+v", policy)
	}
	if _, err := agentRPCPolicyForMethod("Agent.ApplyFirewall"); !errors.Is(
		err,
		errAgentRPCPolicyMissing,
	) {
		t.Fatalf("legacy ApplyFirewall policy error = %v, want missing", err)
	}
}

func TestDNSZoneSyncRPCPolicyRequiresV2(t *testing.T) {
	policy, err := agentRPCPolicyForMethod("Agent.SyncDNSZoneV2")
	if err != nil {
		t.Fatal(err)
	}
	if policy.timeout != agentRPCDatabaseTimeout ||
		policy.effect != agentRPCEffectHostMutation ||
		policy.capability != agentRPCCapabilityDNS {
		t.Fatalf("SyncDNSZoneV2 policy = %+v", policy)
	}
	if _, err := agentRPCPolicyForMethod("Agent.SyncDNSZone"); !errors.Is(
		err,
		errAgentRPCPolicyMissing,
	) {
		t.Fatalf("legacy SyncDNSZone policy error = %v, want missing", err)
	}
	if len(rhelPreviewAgentRPCMethodGrants) != 0 {
		t.Fatalf("DNS V2 changed dormant RHEL grants: %v", rhelPreviewAgentRPCMethodGrants)
	}
}

func TestMailTLSSyncRPCPolicyRequiresV2(t *testing.T) {
	policy, err := agentRPCPolicyForMethod("Agent.SyncMailTLSV2")
	if err != nil {
		t.Fatal(err)
	}
	if policy.timeout != agentRPCMutationTimeout ||
		policy.effect != agentRPCEffectHostMutation ||
		policy.capability != agentRPCCapabilityMail {
		t.Fatalf("SyncMailTLSV2 policy = %+v", policy)
	}
	for _, legacy := range []string{
		"Agent.SecureMailTLS",
		"Agent.ReconcileMailTLSMutation",
	} {
		if _, err := agentRPCPolicyForMethod(legacy); !errors.Is(
			err, errAgentRPCPolicyMissing,
		) {
			t.Fatalf("legacy %s policy error = %v, want missing", legacy, err)
		}
	}
	if len(rhelPreviewAgentRPCMethodGrants) != 0 {
		t.Fatalf("Mail TLS V2 changed dormant RHEL grants: %v", rhelPreviewAgentRPCMethodGrants)
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
	// The canonical HostPlatform caller intentionally lives beside the policy
	// registry, which this broad literal scan excludes to avoid counting every
	// registry entry as a production call. The dedicated AST boundary tests
	// separately require exactly one literal HostPlatform raw dispatch inside
	// agentRPCHostIdentity, so include that reviewed caller in this inventory.
	found["Agent.HostPlatform"] = filepath.Join(
		repoRoot,
		"cmd",
		"panel",
		"agent_rpc_policy.go",
	)
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
		identity := agentRPCHostIdentity{
			host: core.ManagedServiceHostProfile{PackageFamily: "dnf"},
		}
		if err := authorizeAgentRPCPolicyForHost("Agent.GetConfig", policy, identity); err != nil {
			t.Errorf("read/control blocked on dnf: %v", err)
		}
	}
	for _, family := range []string{"apt", "pacman"} {
		identity := agentRPCHostIdentity{
			host: core.ManagedServiceHostProfile{PackageFamily: family},
		}
		if err := authorizeAgentRPCPolicyForHost("Agent.UpdateConfig", mutation, identity); err != nil {
			t.Errorf("mutation blocked on established family %s: %v", family, err)
		}
	}
	if len(rhelPreviewAgentRPCMethodGrants) != 0 {
		t.Fatalf("RHEL preview grant count = %d, want zero", len(rhelPreviewAgentRPCMethodGrants))
	}
	rhelIdentity := agentRPCHostIdentity{
		host: core.ManagedServiceHostProfile{
			DistroFamily: "rhel", PackageFamily: "dnf", ServiceManager: "systemd",
			DistroID: "rocky", VersionID: "9.6", Architecture: "amd64",
		},
		verified: true,
	}
	for _, test := range []struct {
		method string
		policy agentRPCPolicy
	}{
		{method: "Agent.UpdateConfig", policy: mutation},
		{method: "Agent.RepoStatus", policy: repair},
	} {
		err := authorizeAgentRPCPolicyForHost(test.method, test.policy, rhelIdentity)
		if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
			t.Errorf("dnf mutation error = %v, want capability denial", err)
		}
	}
	familyOnlyRHEL := agentRPCHostIdentity{
		host: core.ManagedServiceHostProfile{PackageFamily: "dnf"},
	}
	for method, policy := range agentRPCPolicies {
		if policy.effect != agentRPCEffectHostMutation &&
			policy.effect != agentRPCEffectHostRepairMutation {
			continue
		}
		for identityName, identity := range map[string]agentRPCHostIdentity{
			"verified candidate": rhelIdentity,
			"family only":        familyOnlyRHEL,
		} {
			err := authorizeAgentRPCPolicyForHost(method, policy, identity)
			if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
				t.Errorf("%s %s error = %v, want capability denial", method, identityName, err)
			}
		}
	}
	if err := authorizeAgentRPCPolicyForHost(
		"Agent.UpdateConfig",
		mutation,
		agentRPCHostIdentity{},
	); !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("empty identity error = %v, want identity unavailable", err)
	}
}

func TestRHELPreviewExactMethodPrefilterRequiresCapabilityAndIdentity(t *testing.T) {
	if len(rhelPreviewAgentRPCMethodGrants) != 0 {
		t.Fatalf("production RHEL preview grants = %v, want empty", rhelPreviewAgentRPCMethodGrants)
	}
	original := rhelPreviewAgentRPCMethodGrants
	rhelPreviewAgentRPCMethodGrants = map[string]agentRPCCapability{
		"Agent.EnsureNginxReady": agentRPCCapabilityServiceLifecycle,
	}
	t.Cleanup(func() { rhelPreviewAgentRPCMethodGrants = original })

	policy, err := agentRPCPolicyForMethod("Agent.EnsureNginxReady")
	if err != nil {
		t.Fatal(err)
	}
	candidate := agentRPCHostIdentity{
		host: core.ManagedServiceHostProfile{
			DistroFamily: "rhel", PackageFamily: "dnf", ServiceManager: "systemd",
			DistroID: "almalinux", VersionID: "9.7", Architecture: "arm64",
		},
		verified: true,
	}
	if err := authorizeAgentRPCPolicyForHost("Agent.EnsureNginxReady", policy, candidate); err != nil {
		t.Fatalf("synthetic exact-method prefilter entry denied: %v", err)
	}
	for _, test := range []struct {
		name     string
		method   string
		policy   agentRPCPolicy
		identity agentRPCHostIdentity
	}{
		{
			name:   "different method",
			method: "Agent.StartServiceMutation", policy: policy, identity: candidate,
		},
		{
			name:   "different capability",
			method: "Agent.EnsureNginxReady",
			policy: agentRPCPolicy{
				timeout: agentRPCMutationTimeout, effect: agentRPCEffectHostMutation,
				capability: agentRPCCapabilityHostConfig,
			},
			identity: candidate,
		},
		{
			name:   "family only",
			method: "Agent.EnsureNginxReady", policy: policy,
			identity: agentRPCHostIdentity{
				host: core.ManagedServiceHostProfile{PackageFamily: "dnf"},
			},
		},
		{
			name:   "uncertified distro",
			method: "Agent.EnsureNginxReady", policy: policy,
			identity: agentRPCHostIdentity{
				host: core.ManagedServiceHostProfile{
					DistroFamily: "rhel", PackageFamily: "dnf", ServiceManager: "systemd",
					DistroID: "fedora", VersionID: "42", Architecture: "amd64",
				},
				verified: true,
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := authorizeAgentRPCPolicyForHost(test.method, test.policy, test.identity)
			if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
				t.Fatalf("error = %v, want capability denial", err)
			}
		})
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

type slowPolicyIdentityTestAgent struct {
	hostResponse transport.HostPlatformResponse
	hostCalls    atomic.Int32
	delay        time.Duration
}

func (a *slowPolicyIdentityTestAgent) HostPlatform(
	_ *transport.Empty,
	out *transport.HostPlatformResponse,
) error {
	a.hostCalls.Add(1)
	time.Sleep(a.delay)
	*out = a.hostResponse
	return nil
}

type blockingPolicyIdentityTestAgent struct {
	hostResponse transport.HostPlatformResponse
	release      <-chan struct{}
	entered      chan<- struct{}
	hostCalls    atomic.Int32
	familyCalls  atomic.Int32
}

type failingThenHealthyPolicyIdentityTestAgent struct {
	firstEntered chan<- struct{}
	firstRelease <-chan struct{}
	hostCalls    atomic.Int32
}

func (a *failingThenHealthyPolicyIdentityTestAgent) HostPlatform(
	_ *transport.Empty,
	out *transport.HostPlatformResponse,
) error {
	call := a.hostCalls.Add(1)
	if call == 1 {
		a.firstEntered <- struct{}{}
		<-a.firstRelease
		return errors.New("shared HostPlatform failure")
	}
	*out = debianPolicyTestIdentity()
	return nil
}

func (a *blockingPolicyIdentityTestAgent) HostPlatform(
	_ *transport.Empty,
	out *transport.HostPlatformResponse,
) error {
	a.hostCalls.Add(1)
	if a.entered != nil {
		a.entered <- struct{}{}
	}
	<-a.release
	*out = a.hostResponse
	return nil
}

func (a *blockingPolicyIdentityTestAgent) PkgFamily(_ *transport.Empty, out *string) error {
	a.familyCalls.Add(1)
	*out = "apt"
	return nil
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
	panel.pkgFamilyVal = "dnf"
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

func TestNearMatchHostPlatformErrorDoesNotEnableLegacyFallback(t *testing.T) {
	agent := &policyDispatchTestAgent{
		hostErr: rpc.ServerError("rpc: can't find method Agent.HostPlatform trailing-data"),
		family:  "apt",
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	var out bool
	err := panel.callAgentContext(
		context.Background(),
		"Agent.UpdateConfig",
		&transport.Empty{},
		&out,
	)
	if !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("error = %v, want identity unavailable", err)
	}
	if agent.hostCalls != 1 || agent.familyCalls != 0 || agent.updateCalls != 0 {
		t.Fatalf("calls host=%d family=%d update=%d, want 1/0/0",
			agent.hostCalls, agent.familyCalls, agent.updateCalls)
	}
}

func TestHostPlatformTimeoutDoesNotEnableLegacyFallback(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	agent := &blockingPolicyIdentityTestAgent{
		hostResponse: debianPolicyTestIdentity(),
		release:      release,
		entered:      entered,
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	err := panel.authorizeAgentRPCContext(ctx, "Agent.UpdateConfig")
	<-entered
	panel.hostPlatformResolutionMu.Lock()
	resolution := panel.hostPlatformResolution
	panel.hostPlatformResolutionMu.Unlock()
	if resolution == nil {
		t.Fatal("HostPlatform flight completed before the blocked worker was released")
	}
	close(release)
	<-resolution.done
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want deadline exceeded", err)
	}
	if calls := agent.familyCalls.Load(); calls != 0 {
		t.Fatalf("legacy PkgFamily calls = %d, want zero", calls)
	}
}

func TestHostPlatformResolutionIsSingleFlight(t *testing.T) {
	agent := &slowPolicyIdentityTestAgent{
		hostResponse: rhelPolicyTestIdentity(),
		delay:        50 * time.Millisecond,
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	const callers = 12
	start := make(chan struct{})
	errs := make(chan error, callers)
	var wg sync.WaitGroup
	wg.Add(callers)
	for range callers {
		go func() {
			defer wg.Done()
			<-start
			errs <- panel.authorizeAgentRPCContext(context.Background(), "Agent.UpdateConfig")
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		if !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
			t.Errorf("authorization error = %v, want capability denial", err)
		}
	}
	if calls := agent.hostCalls.Load(); calls != 1 {
		t.Fatalf("HostPlatform calls = %d, want one", calls)
	}
}

func TestWaitingHostPlatformResolutionHonorsCanceledContext(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWorker()
	entered := make(chan struct{}, 1)
	agent := &blockingPolicyIdentityTestAgent{
		hostResponse: rhelPolicyTestIdentity(),
		release:      release,
		entered:      entered,
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	firstErr := make(chan error, 1)
	go func() {
		firstErr <- panel.authorizeAgentRPCContext(
			context.Background(),
			"Agent.UpdateConfig",
		)
	}()
	<-entered

	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	secondErr := make(chan error, 1)
	go func() {
		secondErr <- panel.authorizeAgentRPCContext(ctx, "Agent.UpdateConfig")
	}()
	<-ctx.Done()
	select {
	case err := <-secondErr:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("waiting authorization error = %v, want deadline exceeded", err)
		}
	case <-time.After(250 * time.Millisecond):
		t.Fatal("waiting authorization did not honor its context before leader release")
	}
	select {
	case err := <-firstErr:
		t.Fatalf("leader returned before its HostPlatform RPC was released: %v", err)
	default:
	}
	releaseWorker()

	if err := <-firstErr; !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("first authorization error = %v, want capability denial", err)
	}
	if calls := agent.hostCalls.Load(); calls != 1 {
		t.Fatalf("HostPlatform calls = %d, want one", calls)
	}
	if err := panel.authorizeAgentRPCContext(
		context.Background(),
		"Agent.UpdateConfig",
	); !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("cached authorization error = %v, want capability denial", err)
	}
	if calls := agent.hostCalls.Load(); calls != 1 {
		t.Fatalf("HostPlatform calls after cached authorization = %d, want one", calls)
	}
}

func TestHostPlatformFlightCreatorCancellationDoesNotPoisonWaiter(t *testing.T) {
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(release) }) }
	defer releaseWorker()
	entered := make(chan struct{}, 1)
	agent := &blockingPolicyIdentityTestAgent{
		hostResponse: rhelPolicyTestIdentity(),
		release:      release,
		entered:      entered,
	}
	panel := newPolicyDispatchTestPanel(t, agent)

	creatorCtx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	creatorErr := make(chan error, 1)
	go func() {
		creatorErr <- panel.authorizeAgentRPCContext(creatorCtx, "Agent.UpdateConfig")
	}()
	<-entered

	waiterErr := make(chan error, 1)
	go func() {
		waiterErr <- panel.authorizeAgentRPCContext(context.Background(), "Agent.UpdateConfig")
	}()
	if err := <-creatorErr; !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("creator error = %v, want deadline exceeded", err)
	}
	if calls := agent.hostCalls.Load(); calls != 1 {
		t.Fatalf("HostPlatform calls before release = %d, want one", calls)
	}
	select {
	case err := <-waiterErr:
		t.Fatalf("waiter returned before the independent worker was released: %v", err)
	default:
	}

	releaseWorker()
	if err := <-waiterErr; !errors.Is(err, errAgentRPCPlatformCapabilityDenied) {
		t.Fatalf("waiter error = %v, want capability denial", err)
	}
	if calls := agent.hostCalls.Load(); calls != 1 {
		t.Fatalf("HostPlatform calls = %d, want one", calls)
	}
}

func TestHostPlatformFlightSharesFailureThenAllowsHealthyRetry(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{}, 1)
	agent := &failingThenHealthyPolicyIdentityTestAgent{
		firstEntered: entered,
		firstRelease: release,
	}
	panel := newPolicyDispatchTestPanel(t, agent)
	type result struct {
		identity agentRPCHostIdentity
		err      error
	}
	leaderResult := make(chan result, 1)
	go func() {
		identity, err := panel.agentRPCHostIdentity(context.Background())
		leaderResult <- result{identity: identity, err: err}
	}()
	<-entered

	waiterStarted := make(chan struct{})
	waiterResult := make(chan result, 1)
	go func() {
		close(waiterStarted)
		identity, err := panel.agentRPCHostIdentity(context.Background())
		waiterResult <- result{identity: identity, err: err}
	}()
	<-waiterStarted
	time.Sleep(25 * time.Millisecond)
	if calls := agent.hostCalls.Load(); calls != 1 {
		close(release)
		t.Fatalf("HostPlatform calls before failed flight release = %d, want one", calls)
	}
	close(release)

	leader := <-leaderResult
	waiter := <-waiterResult
	if leader.err == nil || waiter.err == nil {
		t.Fatalf("shared flight errors leader=%v waiter=%v, want both non-nil", leader.err, waiter.err)
	}
	if leader.err != waiter.err {
		t.Fatalf("flight did not share the same error instance: leader=%v waiter=%v", leader.err, waiter.err)
	}
	if leader.identity != (agentRPCHostIdentity{}) || waiter.identity != (agentRPCHostIdentity{}) {
		t.Fatalf("failed flight returned identities leader=%+v waiter=%+v", leader.identity, waiter.identity)
	}

	identity, err := panel.agentRPCHostIdentity(context.Background())
	if err != nil {
		t.Fatalf("healthy retry error: %v", err)
	}
	if !identity.verified || identity.host.PackageFamily != "apt" {
		t.Fatalf("healthy retry identity = %+v", identity)
	}
	if calls := agent.hostCalls.Load(); calls != 2 {
		t.Fatalf("HostPlatform calls after retry = %d, want two", calls)
	}
	if _, err := panel.agentRPCHostIdentity(context.Background()); err != nil {
		t.Fatalf("cached identity error: %v", err)
	}
	if calls := agent.hostCalls.Load(); calls != 2 {
		t.Fatalf("HostPlatform calls after cache = %d, want two", calls)
	}
}

func TestInvalidCachedVerifiedIdentityDoesNotDowngradeToFamilyOnly(t *testing.T) {
	panel := &Panel{
		hostPlatformKnown: true,
		hostPlatformVal: transport.HostPlatformResponse{
			DistroFamily: "rhel", PackageManager: "apt", ServiceManager: "systemd",
			DistroID: "rocky", VersionID: "9.6", Architecture: "amd64",
		},
		pkgFamilyVal: "apt",
	}
	var out bool
	err := panel.callAgentContext(
		context.Background(),
		"Agent.UpdateConfig",
		&transport.Empty{},
		&out,
	)
	if !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("error = %v, want identity unavailable", err)
	}
}

func TestConflictingCachedFamilyAndVerifiedIdentityFailClosed(t *testing.T) {
	panel := &Panel{
		hostPlatformKnown: true,
		hostPlatformVal:   rhelPolicyTestIdentity(),
		pkgFamilyVal:      "apt",
	}
	err := panel.authorizeAgentRPCContext(context.Background(), "Agent.UpdateConfig")
	if !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("cached conflict error = %v, want identity unavailable", err)
	}

	agent := &policyDispatchTestAgent{hostResponse: debianPolicyTestIdentity()}
	panel = newPolicyDispatchTestPanel(t, agent)
	panel.pkgFamilyVal = "dnf"
	err = panel.authorizeAgentRPCContext(context.Background(), "Agent.UpdateConfig")
	if !errors.Is(err, errAgentRPCPlatformIdentityUnavailable) {
		t.Fatalf("fresh conflict error = %v, want identity unavailable", err)
	}
	if agent.hostCalls != 1 || agent.updateCalls != 0 {
		t.Fatalf("calls host=%d update=%d, want 1/0", agent.hostCalls, agent.updateCalls)
	}
}
