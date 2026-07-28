package main

import (
	"go/ast"
	"testing"
)

func TestMutationProcessDotImportedRawSyscallRecognition(t *testing.T) {
	file := parseMutationProcessFixture(t, `package main
import . "syscall"
func escape() { _, _, _ = RawSyscall(SYS_CLONE, 0, 0, 0) }
`)
	found := false
	ast.Inspect(file.node, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		ident, ok := call.Fun.(*ast.Ident)
		if ok && mutationDotSpawnSyscall(file, ident.Name, call) {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("dot-imported raw spawn syscall was not recognized")
	}
}
