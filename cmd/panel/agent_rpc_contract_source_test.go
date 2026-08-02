package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Keep gob request and response structs in internal/transport. Anonymous
// structs can silently drop renamed fields because net/rpc matches gob fields
// by name instead of making the two endpoints compile against one contract.
func TestAgentRPCCallsDoNotUseAnonymousStructContracts(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}

	fset := token.NewFileSet()
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
			if !ok || !isCallAgentExpression(call.Fun) || len(call.Args) < 3 {
				return true
			}
			for _, argument := range call.Args[1:3] {
				if usesAnonymousStructContract(argument) {
					position := fset.Position(argument.Pos())
					t.Errorf("%s:%d uses an anonymous RPC contract; use internal/transport", position.Filename, position.Line)
				}
			}
			return true
		})
	}
}

func isCallAgentExpression(expression ast.Expr) bool {
	selector, ok := expression.(*ast.SelectorExpr)
	return ok && selector.Sel.Name == "callAgent"
}

func usesAnonymousStructContract(expression ast.Expr) bool {
	switch value := expression.(type) {
	case *ast.UnaryExpr:
		return value.Op == token.AND && usesAnonymousStructContract(value.X)
	case *ast.CompositeLit:
		_, ok := value.Type.(*ast.StructType)
		return ok
	case *ast.Ident:
		if value.Obj == nil {
			return false
		}
		spec, ok := value.Obj.Decl.(*ast.ValueSpec)
		if !ok {
			return false
		}
		if _, ok := spec.Type.(*ast.StructType); ok {
			return true
		}
		for _, initialValue := range spec.Values {
			if usesAnonymousStructContract(initialValue) {
				return true
			}
		}
	}
	return false
}
