package main

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
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
		ast.Inspect(file, func(node ast.Node) bool {
			selector, ok := node.(*ast.SelectorExpr)
			if !ok || !isRawPanelAgentClientCall(selector) {
				return true
			}
			rawDispatches++
			if name != "agent_rpc_policy.go" {
				position := fset.Position(selector.Pos())
				t.Errorf(
					"%s:%d bypasses the reviewed Agent RPC boundary; use callAgentContext",
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
