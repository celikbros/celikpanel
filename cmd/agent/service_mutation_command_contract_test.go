package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"strconv"
	"testing"
)

const anyExecArgument = "\x00"

// This contract guards the privileged handler surface, not every diagnostic
// command in the agent. Durable handlers may still make explicitly read-only
// probes, but host mutations must use serviceMutationCommand (or its helpers)
// so the worker identity is persisted and the host flock survives a crash.
// Bu sözleşme agent içindeki her tanılama komutunu değil, ayrıcalıklı handler
// yüzeyini korur. Kalıcı handler'lar açıkça salt-okunur kontroller yapabilir;
// ancak worker kimliği kaydedilsin ve host flock bir çökmeden sağ çıksın diye
// host değişiklikleri serviceMutationCommand (veya yardımcılarını) kullanmalıdır.
func TestDurableServiceMutationHandlersDoNotBypassTrackedCommands(t *testing.T) {
	files := []string{
		"dbtools_rpc.go",
		"dkim_signing_rpc.go",
		"dns_sync_rpc.go",
		"firewall_rpc.go",
		"install_rpc.go",
		"installed_repo_packages_rpc.go",
		"mail_stack_rpc.go",
		"mail_submission_rpc.go",
		"main.go",
		"nginx_ready.go",
		"pkg_rpc.go",
		"repo_rpc.go",
		"runtime_rpc.go",
		"service_prepare.go",
		"service_mutation_systemd.go",
		"vpn_rpc.go",
		"webmail_rpc.go",
	}
	readOnlyExecPatterns := [][]string{
		{"apt-cache", "policy", anyExecArgument},
		{"apt-cache", "search", "--names-only", anyExecArgument},
		{"dpkg-query", "-W", "-f", "${Status}", anyExecArgument},
		{"ip", "-o", "route", "show", "default"},
		{"ip", "-o", "route", "get", "1.1.1.1"},
		{"node", "--version"},
		{"pacman", "-Q", anyExecArgument},
		{"php", "-r", `echo in_array("sqlite", PDO::getAvailableDrivers()) ? "yes" : "no";`},
		{"postconf", "-h", anyExecArgument},
		{"postconf", "-x", "-h", anyExecArgument},
		{"systemctl", "list-unit-files", anyExecArgument, "--no-legend"},
		{"systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager"},
		{"systemctl", "show", anyExecArgument, "-p", "Id", "--value"},
		{"systemctl", "show", "spamass-milter.service", "-p", "ExecStart", "--value"},
		{"wg", "show", anyExecArgument, "dump"},
	}
	untrackedMutationHelpers := map[string]bool{
		"installPackages":         true,
		"prepareInstalledService": true,
		"refreshAptListsIfStale":  true,
	}
	scopedFunctions := map[string]map[string]bool{
		"main.go": {
			"ResetFailedUnitMutation": true,
			"ServiceMutationAction":   true,
			"StartServiceMutation":    true,
			"serviceActionContext":    true,
		},
	}

	for _, name := range files {
		name := name
		t.Run(name, func(t *testing.T) {
			set := token.NewFileSet()
			file, err := parser.ParseFile(set, name, nil, 0)
			if err != nil {
				t.Fatal(err)
			}
			seenFunctions := map[string]bool{}
			ast.Inspect(file, func(node ast.Node) bool {
				if functions := scopedFunctions[name]; functions != nil {
					if declaration, ok := node.(*ast.FuncDecl); ok {
						if !functions[declaration.Name.Name] {
							return false
						}
						seenFunctions[declaration.Name.Name] = true
					}
				}
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				if ident, ok := call.Fun.(*ast.Ident); ok && untrackedMutationHelpers[ident.Name] {
					t.Errorf("%s: durable handler uses untracked %s at %s", name, ident.Name, set.Position(call.Pos()))
					return true
				}
				selector, ok := call.Fun.(*ast.SelectorExpr)
				if !ok {
					return true
				}
				if selector.Sel.Name == "Start" || selector.Sel.Name == "Wait" {
					t.Errorf("%s: durable handler exposes an asynchronous raw child via %s at %s", name, selector.Sel.Name, set.Position(call.Pos()))
				}
				if owner, ok := selector.X.(*ast.SelectorExpr); ok && owner.Sel.Name == "systemdMgr" {
					switch selector.Sel.Name {
					case "Start", "Stop", "Restart", "Reload":
						t.Errorf("%s: durable handler bypasses tracked systemd execution at %s", name, set.Position(call.Pos()))
					}
				}
				pkg, ok := selector.X.(*ast.Ident)
				if !ok || pkg.Name != "exec" || (selector.Sel.Name != "Command" && selector.Sel.Name != "CommandContext") {
					return true
				}
				nameArg := 0
				if selector.Sel.Name == "CommandContext" {
					nameArg = 1
				}
				if len(call.Args) <= nameArg+1 {
					t.Errorf("%s: unclassified direct exec at %s", name, set.Position(call.Pos()))
					return true
				}
				command, commandOK := stringLiteral(call.Args[nameArg])
				firstArg, argOK := stringLiteral(call.Args[nameArg+1])
				if !commandOK || !argOK || call.Ellipsis.IsValid() ||
					!matchesReadOnlyExecPattern(call.Args[nameArg:], readOnlyExecPatterns) {
					t.Errorf("%s: direct mutating or unclassified exec %q %q at %s", name, command, firstArg, set.Position(call.Pos()))
				}
				return true
			})
			for function := range scopedFunctions[name] {
				if !seenFunctions[function] {
					t.Errorf("%s: durable handler function %s was not found", name, function)
				}
			}
		})
	}
}

func TestServiceMutationCommandDoesNotExposeRawStartWait(t *testing.T) {
	set := token.NewFileSet()
	file, err := parser.ParseFile(set, "mutation_command.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	typeFound := false
	factoryFound := false
	for _, decl := range file.Decls {
		if declaration, ok := decl.(*ast.GenDecl); ok {
			for _, spec := range declaration.Specs {
				typeSpec, ok := spec.(*ast.TypeSpec)
				if !ok || typeSpec.Name.Name != "serviceMutationCmd" {
					continue
				}
				typeFound = true
				structType, ok := typeSpec.Type.(*ast.StructType)
				if !ok {
					t.Fatal("serviceMutationCmd must remain a guarded struct type")
				}
				for _, field := range structType.Fields.List {
					if isExecCmdType(field.Type) {
						t.Fatalf("serviceMutationCmd exposes raw exec.Cmd field at %s", set.Position(field.Pos()))
					}
				}
			}
		}
		fn, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}
		if receiverName(fn) == "serviceMutationCmd" {
			if fn.Name.Name == "Start" || fn.Name.Name == "Wait" {
				t.Fatalf("serviceMutationCmd exposes raw %s at %s", fn.Name.Name, set.Position(fn.Pos()))
			}
			if fn.Type.Results != nil {
				for _, result := range fn.Type.Results.List {
					if isExecCmdType(result.Type) {
						t.Fatalf("serviceMutationCmd method %s exposes raw exec.Cmd at %s", fn.Name.Name, set.Position(fn.Pos()))
					}
				}
			}
		}
		if fn.Name.Name != "serviceMutationCommand" {
			continue
		}
		factoryFound = true
		if fn.Type.Results == nil || len(fn.Type.Results.List) != 1 {
			t.Fatal("serviceMutationCommand must return exactly one guarded command type")
		}
		star, ok := fn.Type.Results.List[0].Type.(*ast.StarExpr)
		if !ok {
			t.Fatal("serviceMutationCommand must return a pointer to the guarded command type")
		}
		ident, ok := star.X.(*ast.Ident)
		if !ok || ident.Name != "serviceMutationCmd" {
			t.Fatalf("serviceMutationCommand exposes %T instead of *serviceMutationCmd", star.X)
		}
	}
	if !typeFound {
		t.Fatal("serviceMutationCmd declaration not found")
	}
	if !factoryFound {
		t.Fatal("serviceMutationCommand declaration not found")
	}
}

func receiverName(fn *ast.FuncDecl) string {
	if fn.Recv == nil || len(fn.Recv.List) != 1 {
		return ""
	}
	expr := fn.Recv.List[0].Type
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	ident, _ := expr.(*ast.Ident)
	if ident == nil {
		return ""
	}
	return ident.Name
}

func isExecCmdType(expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Cmd" {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == "exec"
}

func matchesReadOnlyExecPattern(args []ast.Expr, patterns [][]string) bool {
	for _, pattern := range patterns {
		if len(args) != len(pattern) {
			continue
		}
		matched := true
		for index, want := range pattern {
			if want == anyExecArgument {
				continue
			}
			got, ok := stringLiteral(args[index])
			if !ok || got != want {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

func stringLiteral(expr ast.Expr) (string, bool) {
	literal, ok := expr.(*ast.BasicLit)
	if !ok || literal.Kind != token.STRING {
		return "", false
	}
	value, err := strconv.Unquote(literal.Value)
	return value, err == nil
}
