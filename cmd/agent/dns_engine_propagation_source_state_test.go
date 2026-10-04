package main

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

// pair5 P5-1: a managed BIND primary built its propagation plan without the
// source engine state receipt, so after a completed owner-enrolled inspection
// the catalog probe selection failed and was reported as an owner edit.

func sameProbe(a, b any) bool {
	return reflect.ValueOf(a).Pointer() == reflect.ValueOf(b).Pointer()
}

func TestBINDPrimaryPlanCarriesSourceReceiptAndSelectsBINDCatalogProbes(t *testing.T) {
	tree, _ := testBINDV3PrimaryTree(
		t, 8, testBINDV3Snapshot(t, "gone.example.test", 3, 0, true),
	)
	source := testBINDV3SourceState()
	plan, primary, err := bindV3PrimaryPropagationPlan(tree, "gone.example.test", source)
	if err != nil || !primary {
		t.Fatalf("primary=%v err=%v", primary, err)
	}
	if plan.SourceState != source {
		t.Fatalf("BIND plan lost its source receipt: %+v", plan.SourceState)
	}
	// The same selection both native verifiers make before minting the
	// challenge and reuse after the inspection.
	local, peer, err := nativePeerProofCatalogProbes(plan)
	if err != nil {
		t.Fatalf("BIND plan could not select its catalog probes: %v", err)
	}
	if !sameProbe(local, probeDNSCatalogAXFR) || !sameProbe(peer, probeDNSBoundCatalogAXFR) {
		t.Fatal("BIND plan did not select the BIND catalog probes")
	}
	wantLocal, wantPeer, err := catalogAXFRProbesForSourceEngine(transport.DNSEngineBIND)
	if err != nil || !sameProbe(local, wantLocal) || !sameProbe(peer, wantPeer) {
		t.Fatalf("selection differs from the BIND engine's probes: %v", err)
	}

	for name, wrong := range map[string]dnsEngineStateReceipt{
		"missing receipt":   {},
		"PowerDNS receipt":  {Engine: transport.DNSEnginePowerDNS, Mode: transport.DNSEngineSwitchModeSwitch},
		"unknown engine":    {Engine: transport.DNSEngine("knot")},
		"empty with fields": {Mode: transport.DNSEngineSwitchModeSwitch, EngineEpoch: 1},
	} {
		if refused, primary, err := bindV3PrimaryPropagationPlan(tree, "gone.example.test", wrong); err == nil ||
			primary || !reflect.DeepEqual(refused, dnsV3PrimaryPropagationPlan{}) {
			t.Fatalf("%s: BIND plan built without the BIND receipt: %+v %v %v", name, refused, primary, err)
		}
	}
}

func TestPrimaryPropagationPlanConstructorRefusesMissingSourceEngine(t *testing.T) {
	evidence := testPDNSPrimaryPropagationEvidence(8, nil, nil)
	changed := expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true}
	for _, engine := range []transport.DNSEngine{"", "knot"} {
		plan, err := newDNSV3PrimaryPropagationPlan(
			dnsEngineStateReceipt{Engine: engine}, evidence, changed, false, dnsV3DeletionOperation{},
		)
		if err == nil || !reflect.DeepEqual(plan, dnsV3PrimaryPropagationPlan{}) {
			t.Fatalf("engine %q: plan constructed: %+v %v", engine, plan, err)
		}
	}
	for _, source := range []dnsEngineStateReceipt{testBINDV3SourceState(), testPDNSV3SourceState()} {
		plan, err := newDNSV3PrimaryPropagationPlan(source, evidence, changed, false, dnsV3DeletionOperation{})
		if err != nil || plan.SourceState != source || plan.Evidence.Domain != evidence.Domain ||
			plan.Changed != changed {
			t.Fatalf("engine %q: %+v %v", source.Engine, plan, err)
		}
	}
	// A known engine does not bypass the existing plan validation.
	if _, err := newDNSV3PrimaryPropagationPlan(testBINDV3SourceState(), evidence,
		expectedDNSZoneAuthority{Domain: evidence.Domain, Delete: true}, false, dnsV3DeletionOperation{}); err == nil {
		t.Fatal("invalid changed authority accepted")
	}
	// The PowerDNS prepare path validates through the same constructor.
	if err := validatePDNSPrimaryPropagationPlan(pdnsV3PropagationPlan{
		Primary: true, Evidence: evidence, Changed: changed,
	}); err == nil {
		t.Fatal("PowerDNS plan without its source receipt validated")
	}
}

// Every non-test builder goes through newDNSV3PrimaryPropagationPlan; only an
// empty zero value (an error or not-primary return) may be written directly.
func TestPrimaryPropagationPlanIsBuiltOnlyByItsConstructor(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("list sources: %v", err)
	}
	fset := token.NewFileSet()
	var offenders []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, source, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if ok && fn.Name.Name == "newDNSV3PrimaryPropagationPlan" {
				continue
			}
			ast.Inspect(decl, func(node ast.Node) bool {
				literal, ok := node.(*ast.CompositeLit)
				if !ok || len(literal.Elts) == 0 {
					return true
				}
				if ident, ok := literal.Type.(*ast.Ident); ok && ident.Name == "dnsV3PrimaryPropagationPlan" {
					offenders = append(offenders, fset.Position(literal.Pos()).String())
				}
				return true
			})
		}
	}
	if len(offenders) != 0 {
		t.Fatalf("primary propagation plan built outside its constructor: %v", offenders)
	}
}

func captureAgentLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buffer bytes.Buffer
	previous, flags := log.Writer(), log.Flags()
	log.SetOutput(&buffer)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(previous)
		log.SetFlags(flags)
	})
	return &buffer
}

func TestNativePeerProofInternalFailureIsNotAnOwnerEdit(t *testing.T) {
	logs := captureAgentLog(t)
	plan := dnsV3PrimaryPropagationPlan{
		Evidence: testPDNSPrimaryPropagationEvidence(8, nil, nil),
		Changed:  expectedDNSZoneAuthority{Domain: "gone.example.test", Delete: true},
	}
	local, peer, err := nativePeerProofCatalogProbes(plan)
	if local != nil || peer != nil {
		t.Fatal("probes selected for a plan without a source engine")
	}
	code := pendingDNSPeerCode(err)
	if code != transport.DNSPeerPendingProofInternal || code == transport.DNSPeerPendingOwnerEditUnknown ||
		dnsZoneV3PendingLedgerCode(code) != transport.DNSPeerPendingProofInternal {
		t.Fatalf("internal selection failure code=%q err=%v", code, err)
	}
	if pending := dnsZoneV3RecoveryPending(err); pendingDNSPeerCode(pending) != transport.DNSPeerPendingProofInternal {
		t.Fatalf("pending wrapper lost the internal code: %v", pending)
	}
	line := logs.String()
	if !strings.Contains(line, "DNS peer proof could not run") ||
		!strings.Contains(line, "catalog producer engine is unavailable") ||
		!strings.Contains(line, "gone.example.test") {
		t.Fatalf("underlying error was not logged: %q", line)
	}

	// A local recheck that could not run is internal; observed differing
	// evidence stays an owner edit.
	logs.Reset()
	ok := func() error { return nil }
	internal := func() error { return dnsPeerProofInternal(errors.New("plan source engine is unknown")) }
	if got := pendingDNSPeerCode(peerCurrentPendingCodeAt(ok, ok, internal)); got != transport.DNSPeerPendingProofInternal {
		t.Fatalf("internal local recheck code=%q", got)
	}
	if !strings.Contains(logs.String(), "plan source engine is unknown") {
		t.Fatalf("internal recheck was not logged: %q", logs.String())
	}
	changed := func() error { return errors.New("native BIND proof local deletion receipt changed") }
	if got := pendingDNSPeerCode(peerCurrentPendingCodeAt(ok, ok, changed)); got != transport.DNSPeerPendingOwnerEditUnknown {
		t.Fatalf("observed local change code=%q", got)
	}
}

func TestNativePeerProofLogTextIsBounded(t *testing.T) {
	long := errors.New("first line\nsecond\tline " + strings.Repeat("é", 600))
	text := boundedDNSPeerProofLogText(long)
	if strings.ContainsAny(text, "\n\t") || len(text) > dnsPeerProofLogLimit+len("...") ||
		!strings.HasSuffix(text, "...") || !strings.HasPrefix(text, "first line second line") {
		t.Fatalf("unbounded log text (%d): %q", len(text), text)
	}
	if boundedDNSPeerProofLogText(nil) == "" {
		t.Fatal("nil error produced empty log text")
	}
}
