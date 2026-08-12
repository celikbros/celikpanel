package main

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCancellingRecoveryContextRequiresDurableTracker(t *testing.T) {
	ctx, cancel, err := serviceMutationCancellingRecoveryContext(
		context.Background(),
		time.Second,
	)
	if err == nil || ctx != nil || cancel != nil {
		t.Fatalf("untracked recovery context ctx=%v cancel=%v err=%v", ctx, cancel, err)
	}
}

func TestCancellingRecoveryContextIsRestrictedToVPNPeerRollback(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	uses := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		set := token.NewFileSet()
		file, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, declaration := range file.Decls {
			function, ok := declaration.(*ast.FuncDecl)
			if !ok || function.Body == nil {
				continue
			}
			ast.Inspect(function.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				identifier, ok := call.Fun.(*ast.Ident)
				if !ok || identifier.Name != "serviceMutationCancellingRecoveryContext" {
					return true
				}
				uses++
				if filepath.Base(name) != "vpn_rpc.go" || function.Name.Name != "SyncVPNPeersV2" {
					t.Errorf(
						"cancelling recovery context used outside VPN peer rollback at %s:%d in %s",
						name,
						set.Position(call.Pos()).Line,
						function.Name.Name,
					)
				}
				return true
			})
		}
	}
	if uses != 3 {
		t.Fatalf("cancelling recovery context uses=%d want=3 VPN rollback paths", uses)
	}
}
