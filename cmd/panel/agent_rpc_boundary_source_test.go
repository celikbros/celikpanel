package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestProductionAgentRPCDispatchIsCentralized(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	rawDispatches := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		allowedAgentClientReferences := make(map[token.Pos]struct{})
		ast.Inspect(file, func(node ast.Node) bool {
			binary, ok := node.(*ast.BinaryExpr)
			if !ok {
				return true
			}
			if selector, ok := agentClientAvailabilitySelector(binary); ok {
				allowedAgentClientReferences[selector.Pos()] = struct{}{}
			}
			return true
		})
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			functionName := enclosingProductionFunctionName(file, selector)
			if selector.Sel.Name == "agentClient" && functionName != "rawAgentCallContext" {
				if _, allowed := allowedAgentClientReferences[selector.Pos()]; !allowed {
					position := fset.Position(selector.Pos())
					t.Errorf(
						"%s:%d accesses the raw agent client outside a nil availability check or rawAgentCallContext",
						position.Filename,
						position.Line,
					)
				}
			}
			if !isRawPanelAgentClientCall(selector) {
				return true
			}
			rawDispatches++
			if name != "agent_rpc_policy.go" || functionName != "rawAgentCallContext" {
				position := fset.Position(selector.Pos())
				t.Errorf(
					"%s:%d bypasses the reviewed Agent RPC boundary; raw client dispatch belongs only in rawAgentCallContext",
					position.Filename,
					position.Line,
				)
			}
			return true
		})
	}

	if rawDispatches != 1 {
		t.Fatalf("found %d raw panel Agent client dispatches; want exactly the reviewed low-level dispatch", rawDispatches)
	}
}

func isRawPanelAgentClientCall(selector *ast.SelectorExpr) bool {
	if selector.Sel.Name != "Call" && selector.Sel.Name != "CallContext" {
		return false
	}
	receiver, ok := selector.X.(*ast.SelectorExpr)
	return ok && receiver.Sel.Name == "agentClient"
}

func TestAgentRPCHostIdentityResolutionCannotCreateAnotherDispatchBoundary(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "agent_rpc_policy.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	resolver := productionFunctionNamed(file, "agentRPCHostIdentity")
	if resolver == nil {
		t.Fatal("agentRPCHostIdentity resolver was not found")
	}
	wantIdentityDispatches := map[string]int{
		"Agent.HostPlatform": 0,
		"Agent.PkgFamily":    0,
	}
	agentClientReferences := 0
	agentClientAvailabilityChecks := 0
	ast.Inspect(resolver.Body, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok && isPanelAgentClientSelector(selector) {
			agentClientReferences++
		}
		if binary, ok := node.(*ast.BinaryExpr); ok && isPanelAgentClientAvailabilityCheck(binary) {
			agentClientAvailabilityChecks++
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "callAgent", "callAgentContext", "authorizeAgentRPCContext":
			position := fset.Position(call.Pos())
			t.Errorf(
				"%s:%d identity resolution must not recurse through an authorized dispatch path",
				position.Filename,
				position.Line,
			)
		case "rawAgentCallContext":
			method, ok := exactStringCallArgument(call, 1)
			if !ok {
				position := fset.Position(call.Pos())
				t.Errorf("%s:%d identity dispatch method must be an exact string literal", position.Filename, position.Line)
				return true
			}
			if _, expected := wantIdentityDispatches[method]; !expected {
				position := fset.Position(call.Pos())
				t.Errorf("%s:%d identity resolver dispatches unexpected method %q", position.Filename, position.Line, method)
				return true
			}
			wantIdentityDispatches[method]++
		}
		return true
	})
	for method, count := range wantIdentityDispatches {
		if count != 1 {
			t.Errorf("identity resolver dispatch count for %s = %d, want exactly one raw dispatch", method, count)
		}
	}
	if agentClientReferences != 1 || agentClientAvailabilityChecks != 1 {
		t.Errorf(
			"identity resolver agentClient references/checks = %d/%d, want only the single nil availability check",
			agentClientReferences,
			agentClientAvailabilityChecks,
		)
	}

	authorizer := productionFunctionNamed(file, "authorizeAgentRPCContext")
	if authorizer == nil {
		t.Fatal("authorizeAgentRPCContext was not found")
	}
	identityResolverCalls := 0
	ast.Inspect(authorizer.Body, func(node ast.Node) bool {
		if selector, ok := node.(*ast.SelectorExpr); ok && isPanelAgentClientSelector(selector) {
			position := fset.Position(selector.Pos())
			t.Errorf("%s:%d authorization must not access the raw agent client", position.Filename, position.Line)
		}
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "agentRPCHostIdentity":
			identityResolverCalls++
		case "rawAgentCallContext", "callAgent", "callAgentContext":
			position := fset.Position(call.Pos())
			t.Errorf(
				"%s:%d authorization must resolve identity without opening another dispatch path",
				position.Filename,
				position.Line,
			)
		}
		return true
	})
	if identityResolverCalls != 1 {
		t.Fatalf("authorizeAgentRPCContext calls agentRPCHostIdentity %d times; want exactly once", identityResolverCalls)
	}
}

func TestRawAgentCallContextHasOnlyReviewedProductionCallSites(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	identityDispatches := map[string]int{
		"Agent.HostPlatform": 0,
		"Agent.PkgFamily":    0,
	}
	guardedDispatches := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || selector.Sel.Name != "rawAgentCallContext" {
				return true
			}
			functionName := enclosingProductionFunctionName(file, call)
			switch functionName {
			case "agentRPCHostIdentity":
				method, ok := exactStringCallArgument(call, 1)
				if !ok {
					position := fset.Position(call.Pos())
					t.Errorf("%s:%d identity raw dispatch must use an exact method literal", position.Filename, position.Line)
					return true
				}
				if _, expected := identityDispatches[method]; !expected {
					position := fset.Position(call.Pos())
					t.Errorf("%s:%d identity resolver raw-dispatches unexpected method %q", position.Filename, position.Line, method)
					return true
				}
				identityDispatches[method]++
			case "callAgentContext":
				if len(call.Args) < 2 {
					position := fset.Position(call.Pos())
					t.Errorf("%s:%d guarded raw dispatch lacks its method argument", position.Filename, position.Line)
					return true
				}
				method, ok := call.Args[1].(*ast.Ident)
				if !ok || method.Name != "method" {
					position := fset.Position(call.Pos())
					t.Errorf("%s:%d callAgentContext raw dispatch must forward its authorized method", position.Filename, position.Line)
					return true
				}
				guardedDispatches++
			default:
				position := fset.Position(call.Pos())
				t.Errorf(
					"%s:%d %s must not call rawAgentCallContext directly",
					position.Filename,
					position.Line,
					functionName,
				)
			}
			return true
		})
	}
	for method, count := range identityDispatches {
		if count != 1 {
			t.Errorf("reviewed raw identity dispatch count for %s = %d, want one", method, count)
		}
	}
	if guardedDispatches != 1 {
		t.Errorf("reviewed post-authorization raw dispatch count = %d, want one", guardedDispatches)
	}
}

func TestRHELPreviewAgentRPCExactMethodPrefilterRemainsSealed(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	declarations := 0
	indexedReads := 0
	allowedIdentifiers := make(map[token.Pos]struct{})
	type identifierOccurrence struct {
		position     token.Pos
		functionName string
	}
	var occurrences []identifierOccurrence

	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Clean(name), nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		for _, declaration := range file.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok {
				continue
			}
			for _, rawSpec := range general.Specs {
				spec, ok := rawSpec.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for index, identifier := range spec.Names {
					if identifier.Name != "rhelPreviewAgentRPCMethodGrants" {
						continue
					}
					declarations++
					allowedIdentifiers[identifier.Pos()] = struct{}{}
					if name != "agent_rpc_policy.go" || general.Tok != token.VAR {
						position := fset.Position(identifier.Pos())
						t.Errorf("%s:%d DNF/RHEL exact-method prefilter must be the reviewed package variable", position.Filename, position.Line)
					}
					if len(spec.Names) != 1 || len(spec.Values) != 1 || index != 0 {
						t.Error("DNF/RHEL exact-method prefilter must have one explicit empty map literal")
						continue
					}
					literal, ok := spec.Values[0].(*ast.CompositeLit)
					if !ok {
						t.Error("DNF/RHEL exact-method prefilter must be a map literal")
						continue
					}
					mapType, ok := literal.Type.(*ast.MapType)
					if !ok || !expressionNamesType(mapType.Key, "string") || !expressionNamesType(mapType.Value, "agentRPCCapability") {
						t.Error("DNF/RHEL exact-method prefilter must be map[string]agentRPCCapability")
					}
					if len(literal.Elts) != 0 {
						t.Errorf("DNF/RHEL exact-method prefilter has %d entries; this foundation must grant none", len(literal.Elts))
					}
				}
			}
		}

		ast.Inspect(file, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.Ident:
				if typed.Name == "rhelPreviewAgentRPCMethodGrants" {
					occurrences = append(occurrences, identifierOccurrence{
						position:     typed.Pos(),
						functionName: enclosingProductionFunctionName(file, typed),
					})
				}
			case *ast.IndexExpr:
				registry, ok := typed.X.(*ast.Ident)
				if !ok || registry.Name != "rhelPreviewAgentRPCMethodGrants" {
					return true
				}
				method, exactMethod := typed.Index.(*ast.Ident)
				functionName := enclosingProductionFunctionName(file, typed)
				if name != "agent_rpc_policy.go" || functionName != "authorizeAgentRPCPolicyForHost" || !exactMethod || method.Name != "method" {
					position := fset.Position(typed.Pos())
					t.Errorf(
						"%s:%d DNF/RHEL exact-method prefilter may only be indexed by method inside authorizeAgentRPCPolicyForHost",
						position.Filename,
						position.Line,
					)
					return true
				}
				indexedReads++
				allowedIdentifiers[registry.Pos()] = struct{}{}
			case *ast.AssignStmt:
				for _, expression := range typed.Lhs {
					if expressionNamesType(expression, "rhelPreviewAgentRPCMethodGrants") {
						position := fset.Position(expression.Pos())
						t.Errorf("%s:%d DNF/RHEL exact-method prefilter must never be assigned or written", position.Filename, position.Line)
					}
				}
			case *ast.IncDecStmt:
				if expressionNamesType(typed.X, "rhelPreviewAgentRPCMethodGrants") {
					position := fset.Position(typed.Pos())
					t.Errorf("%s:%d DNF/RHEL exact-method prefilter must never be mutated", position.Filename, position.Line)
				}
			}
			return true
		})
	}

	if declarations != 1 {
		t.Errorf("DNF/RHEL exact-method prefilter declaration count = %d, want one", declarations)
	}
	if indexedReads != 1 {
		t.Errorf("DNF/RHEL exact-method prefilter indexed-read count = %d, want one", indexedReads)
	}
	for _, occurrence := range occurrences {
		if _, allowed := allowedIdentifiers[occurrence.position]; allowed {
			continue
		}
		position := fset.Position(occurrence.position)
		t.Errorf(
			"%s:%d %s must not reference, alias, mutate, clear, delete, or replace the sealed DNF/RHEL exact-method prefilter",
			position.Filename,
			position.Line,
			occurrence.functionName,
		)
	}
}

func productionFunctionNamed(file *ast.File, name string) *ast.FuncDecl {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Name.Name == name {
			return function
		}
	}
	return nil
}

func enclosingProductionFunctionName(file *ast.File, node ast.Node) string {
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if ok && function.Pos() <= node.Pos() && node.End() <= function.End() {
			return function.Name.Name
		}
	}
	return ""
}

func exactStringCallArgument(call *ast.CallExpr, index int) (string, bool) {
	if index < 0 || index >= len(call.Args) {
		return "", false
	}
	literal, ok := call.Args[index].(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}

func isPanelAgentClientSelector(selector *ast.SelectorExpr) bool {
	receiver, ok := selector.X.(*ast.Ident)
	return ok && receiver.Name == "p" && selector.Sel.Name == "agentClient"
}

func isPanelAgentClientAvailabilityCheck(binary *ast.BinaryExpr) bool {
	if binary.Op != token.NEQ {
		return false
	}
	isNil := func(expression ast.Expr) bool {
		identifier, ok := expression.(*ast.Ident)
		return ok && identifier.Name == "nil"
	}
	isAgentClient := func(expression ast.Expr) bool {
		selector, ok := expression.(*ast.SelectorExpr)
		return ok && isPanelAgentClientSelector(selector)
	}
	return (isAgentClient(binary.X) && isNil(binary.Y)) ||
		(isNil(binary.X) && isAgentClient(binary.Y))
}

func agentClientAvailabilitySelector(binary *ast.BinaryExpr) (*ast.SelectorExpr, bool) {
	if binary.Op != token.EQL && binary.Op != token.NEQ {
		return nil, false
	}
	isNil := func(expression ast.Expr) bool {
		identifier, ok := expression.(*ast.Ident)
		return ok && identifier.Name == "nil"
	}
	isAgentClient := func(expression ast.Expr) (*ast.SelectorExpr, bool) {
		selector, ok := expression.(*ast.SelectorExpr)
		return selector, ok && selector.Sel.Name == "agentClient"
	}
	if selector, ok := isAgentClient(binary.X); ok && isNil(binary.Y) {
		return selector, true
	}
	if selector, ok := isAgentClient(binary.Y); ok && isNil(binary.X) {
		return selector, true
	}
	return nil, false
}

func TestSiteOrchestratorProductionWiringUsesGuardedAdapter(t *testing.T) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "main.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}

	wiringCalls := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != "NewSiteOrchestrator" {
			return true
		}
		wiringCalls++
		if len(call.Args) < 2 || !isGuardedSiteAgentAdapter(call.Args[1]) {
			position := fset.Position(call.Pos())
			t.Errorf(
				"%s:%d must inject panelSiteAgentClient{panel: panel} into SiteOrchestrator",
				position.Filename,
				position.Line,
			)
		}
		return true
	})

	if wiringCalls != 1 {
		t.Fatalf("found %d production SiteOrchestrator wiring calls; want exactly one", wiringCalls)
	}
}

func isGuardedSiteAgentAdapter(expression ast.Expr) bool {
	literal, ok := expression.(*ast.CompositeLit)
	if !ok {
		return false
	}
	typeName, ok := literal.Type.(*ast.Ident)
	if !ok || typeName.Name != "panelSiteAgentClient" {
		return false
	}
	for _, element := range literal.Elts {
		keyValue, ok := element.(*ast.KeyValueExpr)
		if !ok {
			continue
		}
		key, keyOK := keyValue.Key.(*ast.Ident)
		value, valueOK := keyValue.Value.(*ast.Ident)
		if keyOK && valueOK && key.Name == "panel" && value.Name == "panel" {
			return true
		}
	}
	return false
}

func TestPanelSiteAgentClientFailsClosedWithoutPanel(t *testing.T) {
	err := (panelSiteAgentClient{}).CallContext(
		context.Background(),
		"Agent.Version",
		&struct{}{},
		&struct{}{},
	)
	if !errors.Is(err, errPanelSiteAgentClientUnavailable) {
		t.Fatalf("nil-panel adapter error = %v, want %v", err, errPanelSiteAgentClientUnavailable)
	}
	err = (panelSiteAgentClient{}).AuthorizeContext(
		context.Background(),
		"Agent.CreateSite",
		&struct{}{},
	)
	if !errors.Is(err, errPanelSiteAgentClientUnavailable) {
		t.Fatalf("nil-panel preflight error = %v, want %v", err, errPanelSiteAgentClientUnavailable)
	}
}

func TestSiteOrchestratorStoresOnlyNarrowAgentInterface(t *testing.T) {
	servicesDir := filepath.Join("..", "..", "internal", "services")
	entries, err := os.ReadDir(servicesDir)
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
	foundAgentField := false
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		path := filepath.Join(servicesDir, name)
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(node ast.Node) bool {
			structure, ok := node.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range structure.Fields.List {
				if expressionNamesType(field.Type, "ReconnectingClient") {
					position := fset.Position(field.Pos())
					t.Errorf("%s:%d stores a raw ReconnectingClient; inject a narrow guarded interface", position.Filename, position.Line)
				}
				for _, fieldName := range field.Names {
					if name == "site_orchestrator.go" && fieldName.Name == "agentClient" {
						foundAgentField = true
						fieldType, ok := field.Type.(*ast.Ident)
						if !ok || fieldType.Name != "siteAgentRPCClient" {
							position := fset.Position(field.Pos())
							t.Errorf("%s:%d SiteOrchestrator agentClient must use siteAgentRPCClient", position.Filename, position.Line)
						}
					}
				}
			}
			return true
		})
	}
	if !foundAgentField {
		t.Fatal("SiteOrchestrator agentClient field was not found")
	}
}

func expressionNamesType(expression ast.Expr, typeName string) bool {
	found := false
	ast.Inspect(expression, func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if ok && selector.Sel.Name == typeName {
			found = true
			return false
		}
		identifier, ok := node.(*ast.Ident)
		if ok && identifier.Name == typeName {
			found = true
			return false
		}
		return true
	})
	return found
}
