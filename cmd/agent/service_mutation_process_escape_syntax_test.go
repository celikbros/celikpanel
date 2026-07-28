package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"testing"
)

// These syntax fixtures make the fail-closed cases explicit so future AST
// refactors cannot silently lose alias, dot-import, composite, or syscall
// coverage while the production tree still happens not to use those forms.
// Bu sözdizimi örnekleri fail-closed durumları açıklaştırır; böylece üretim
// ağacı bu biçimleri henüz kullanmıyorken gelecekteki AST değişiklikleri alias,
// dot-import, composite veya syscall kapsamını sessizce kaybedemez.
func TestMutationProcessEscapeSyntaxRecognition(t *testing.T) {
	t.Run("exec alias and function value", func(t *testing.T) {
		file := parseMutationProcessFixture(t, `package main
import process "os/exec"
func escape() { run := process.Command; cmd := run("touch", "/tmp/x"); _ = cmd.Run() }
`)
		state := mutationProcessFunctionState{
			execCommands:     map[string]bool{},
			execConstructors: map[string]bool{},
		}
		terminalFound := false
		ast.Inspect(file.node, func(node ast.Node) bool {
			switch typed := node.(type) {
			case *ast.AssignStmt:
				learnMutationProcessAssignment(file, typed, &state)
			case *ast.CallExpr:
				selector, ok := typed.Fun.(*ast.SelectorExpr)
				if ok && mutationRawExecTerminalCall(file, selector, state) {
					terminalFound = true
				}
			}
			return true
		})
		if !state.execConstructors["run"] || !state.execCommands["cmd"] || !terminalFound {
			t.Fatal("exec.Command alias/function-value escape was not recognized")
		}
	})

	t.Run("dot import and composite", func(t *testing.T) {
		dotFile := parseMutationProcessFixture(t, `package main
import . "os/exec"
func escape() { _ = Command("touch", "/tmp/x").Run() }
`)
		compositeFile := parseMutationProcessFixture(t, `package main
import process "os/exec"
func escape() { _ = &process.Cmd{} }
`)
		if !dotFile.dotImports["os/exec"] {
			t.Fatal("os/exec dot import was not recorded")
		}
		foundConstructor := false
		ast.Inspect(dotFile.node, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if ok && mutationExecConstructorCall(dotFile, call) {
				foundConstructor = true
			}
			return true
		})
		if !foundConstructor {
			t.Fatal("dot-imported exec.Command was not recognized")
		}
		foundComposite := false
		ast.Inspect(compositeFile.node, func(node ast.Node) bool {
			literal, ok := node.(*ast.CompositeLit)
			if ok && isMutationExecCmdType(compositeFile, literal.Type) {
				foundComposite = true
			}
			return true
		})
		if !foundComposite {
			t.Fatal("aliased exec.Cmd composite was not recognized")
		}
	})

	t.Run("os and syscall spawn", func(t *testing.T) {
		file := parseMutationProcessFixture(t, `package main
import host "os"
import spawn "syscall"
func escape() {
	_, _ = host.StartProcess("/bin/true", nil, nil)
	_, _ = spawn.ForkExec("/bin/true", nil, nil)
	_, _, _ = spawn.RawSyscall(spawn.SYS_CLONE, 0, 0, 0)
}
`)
		startFound := false
		forkFound := false
		rawFound := false
		ast.Inspect(file.node, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			startFound = startFound || mutationImportedSelector(file, selector, "os", "StartProcess")
			forkFound = forkFound || mutationImportedSelector(file, selector, "syscall", "ForkExec")
			rawFound = rawFound || mutationSpawnSyscall(file, selector, call)
			return true
		})
		if !startFound || !forkFound || !rawFound {
			t.Fatalf(
				"spawn recognition incomplete: start=%v fork=%v raw=%v",
				startFound,
				forkFound,
				rawFound,
			)
		}
	})

	t.Run("receiver false positive", func(t *testing.T) {
		file := parseMutationProcessFixture(t, `package main
import "sync"
func safe() { var group sync.WaitGroup; group.Wait() }
`)
		state := mutationProcessFunctionState{
			execCommands:     map[string]bool{},
			execConstructors: map[string]bool{},
		}
		ast.Inspect(file.node, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if ok && mutationRawExecTerminalCall(file, selector, state) {
				t.Fatal("sync.WaitGroup.Wait was misclassified as exec.Cmd.Wait")
			}
			return true
		})
	})
}

func parseMutationProcessFixture(t *testing.T, source string) *mutationProcessContractFile {
	t.Helper()
	node, err := parser.ParseFile(token.NewFileSet(), "fixture.go", source, 0)
	if err != nil {
		t.Fatal(err)
	}
	file := &mutationProcessContractFile{
		name:       "fixture.go",
		node:       node,
		imports:    map[string]string{},
		dotImports: map[string]bool{},
	}
	for _, spec := range node.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			t.Fatal(err)
		}
		alias := filepath.Base(path)
		if spec.Name != nil {
			alias = spec.Name.Name
		}
		if alias == "." {
			file.dotImports[path] = true
		} else if alias != "_" {
			file.imports[alias] = path
		}
	}
	return file
}
