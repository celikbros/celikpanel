package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

type mutationProcessContractFile struct {
	name       string
	node       *ast.File
	imports    map[string]string
	dotImports map[string]bool
}

type mutationProcessContractFunction struct {
	file    *mutationProcessContractFile
	decl    *ast.FuncDecl
	callees map[string]bool
	root    bool
	exempt  bool
}

type mutationProcessFunctionState struct {
	execCommands     map[string]bool
	execConstructors map[string]bool
}

// This release gate discovers every non-test Go source in cmd/agent, derives
// the privileged call graph from requiredServiceMutationStep, and rejects raw
// child-process escape hatches throughout that graph. Only the narrow central
// supervisor boundary and exact read-only probes are exempt.
// Bu sürüm kapısı cmd/agent içindeki test dışı tüm Go kaynaklarını keşfeder,
// ayrıcalıklı çağrı grafiğini requiredServiceMutationStep köklerinden çıkarır ve
// bu grafikteki ham alt süreç kaçışlarını reddeder. Yalnızca dar merkezi
// supervisor sınırı ile tam eşleşen salt-okunur kontroller muaftır.
func TestDurableMutationCallGraphRejectsRawProcessEscapes(t *testing.T) {
	set := token.NewFileSet()
	files := parseMutationProcessContractFiles(t, set)
	functions := mutationProcessContractFunctions(files)
	reachable := reachableMutationProcessFunctions(functions)

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
		{"rpm", "-q", "--", anyExecArgument},
		{"systemctl", "list-unit-files", anyExecArgument, "--no-legend"},
		{"systemctl", "list-unit-files", "--type=service", "--no-legend", "--no-pager"},
		{"systemctl", "show", anyExecArgument, "-p", "Id", "--value"},
		{"systemctl", "show", "spamass-milter.service", "-p", "ExecStart", "--value"},
		{"wg", "show", anyExecArgument, "dump"},
	}

	audited := 0
	for _, fn := range functions {
		if !reachable[fn] {
			continue
		}
		audited++
		auditMutationProcessFunction(t, set, fn, functions, readOnlyExecPatterns)
	}
	if audited == 0 {
		t.Fatal("no durable service-mutation handlers were discovered")
	}
}

func parseMutationProcessContractFiles(
	t *testing.T,
	set *token.FileSet,
) []*mutationProcessContractFile {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	files := make([]*mutationProcessContractFile, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || filepath.Ext(name) != ".go" || strings.HasSuffix(name, "_test.go") {
			continue
		}
		node, err := parser.ParseFile(set, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		file := &mutationProcessContractFile{
			name:       name,
			node:       node,
			imports:    map[string]string{},
			dotImports: map[string]bool{},
		}
		for _, spec := range node.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatalf("decode import in %s: %v", name, err)
			}
			alias := filepath.Base(path)
			if spec.Name != nil {
				alias = spec.Name.Name
			}
			switch alias {
			case "_":
				continue
			case ".":
				file.dotImports[path] = true
			default:
				file.imports[alias] = path
			}
		}
		files = append(files, file)
	}
	return files
}

func mutationProcessContractFunctions(
	files []*mutationProcessContractFile,
) []*mutationProcessContractFunction {
	var functions []*mutationProcessContractFunction
	for _, file := range files {
		for _, declaration := range file.node.Decls {
			fn, ok := declaration.(*ast.FuncDecl)
			if !ok || fn.Body == nil {
				continue
			}
			info := &mutationProcessContractFunction{
				file:    file,
				decl:    fn,
				callees: map[string]bool{},
				exempt:  isCentralMutationProcessBoundary(file.name, fn),
			}
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				switch called := call.Fun.(type) {
				case *ast.Ident:
					info.callees[called.Name] = true
				case *ast.SelectorExpr:
					if called.Sel.Name == "requiredServiceMutationStep" {
						info.root = true
					}
					if owner, ok := called.X.(*ast.Ident); ok {
						if _, isPackage := file.imports[owner.Name]; !isPackage {
							info.callees[called.Sel.Name] = true
						}
					}
				}
				return true
			})
			functions = append(functions, info)
		}
	}
	return functions
}

func reachableMutationProcessFunctions(
	functions []*mutationProcessContractFunction,
) map[*mutationProcessContractFunction]bool {
	byName := map[string][]*mutationProcessContractFunction{}
	for _, fn := range functions {
		byName[fn.decl.Name.Name] = append(byName[fn.decl.Name.Name], fn)
	}
	reachable := map[*mutationProcessContractFunction]bool{}
	var visit func(*mutationProcessContractFunction)
	visit = func(fn *mutationProcessContractFunction) {
		if reachable[fn] {
			return
		}
		reachable[fn] = true
		for name := range fn.callees {
			for _, target := range byName[name] {
				visit(target)
			}
		}
	}
	for _, fn := range functions {
		if fn.root {
			visit(fn)
		}
	}
	return reachable
}

func isCentralMutationProcessBoundary(name string, fn *ast.FuncDecl) bool {
	switch name {
	case "mutation_command.go":
		return receiverName(fn) == "serviceMutationCmd" && fn.Name.Name == "execute"
	case "service_mutation_worker.go":
		return fn.Name.Name == "runTrackedServiceMutationCommandOutput"
	case "service_mutation_supervisor_linux.go":
		return fn.Name.Name == "superviseServiceMutationCommand" ||
			fn.Name.Name == "runServiceMutationSupervisor"
	case "service_mutation_supervisor_stub.go":
		return fn.Name.Name == "superviseServiceMutationCommand"
	}
	return false
}

func allowedCentralMutationProcessCall(
	fn *mutationProcessContractFunction,
	call *ast.CallExpr,
) bool {
	if !fn.exempt {
		return false
	}
	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	method := selector.Sel.Name
	switch fn.file.name {
	case "mutation_command.go":
		return mutationImportedSelector(fn.file, selector, "os/exec", "CommandContext") &&
			call.Ellipsis.IsValid() && len(call.Args) == 3 &&
			mutationSelectorName(call.Args[0], "c", "ctx") &&
			mutationSelectorName(call.Args[1], "c", "name") &&
			mutationSelectorName(call.Args[2], "c", "args")
	case "service_mutation_worker.go":
		owner, ok := selector.X.(*ast.Ident)
		return ok && owner.Name == "cmd" && (method == "Start" || method == "Wait")
	case "service_mutation_supervisor_linux.go":
		if fn.decl.Name.Name == "superviseServiceMutationCommand" {
			return mutationImportedSelector(fn.file, selector, "os/exec", "CommandContext") &&
				call.Ellipsis.IsValid() && len(call.Args) == 3 &&
				mutationIdentName(call.Args[0], "ctx") &&
				mutationIdentName(call.Args[1], "executable") &&
				mutationIdentName(call.Args[2], "args")
		}
		if fn.decl.Name.Name == "runServiceMutationSupervisor" {
			if owner, ok := selector.X.(*ast.Ident); ok && owner.Name == "child" &&
				(method == "Start" || method == "Wait") {
				return true
			}
			return mutationImportedSelector(fn.file, selector, "os/exec", "Command") &&
				call.Ellipsis.IsValid() && len(call.Args) == 2 &&
				mutationIdentName(call.Args[0], "commandPath") &&
				mutationSliceFrom(call.Args[1], "args", 2)
		}
	}
	return false
}

func mutationIdentName(expr ast.Expr, name string) bool {
	ident, ok := expr.(*ast.Ident)
	return ok && ident.Name == name
}

func mutationSelectorName(expr ast.Expr, ownerName, fieldName string) bool {
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != fieldName {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && owner.Name == ownerName
}

func mutationSliceFrom(expr ast.Expr, name string, low int64) bool {
	slice, ok := expr.(*ast.SliceExpr)
	if !ok || slice.High != nil || slice.Max != nil || !mutationIdentName(slice.X, name) {
		return false
	}
	literal, ok := slice.Low.(*ast.BasicLit)
	if !ok || literal.Kind != token.INT {
		return false
	}
	value, err := strconv.ParseInt(literal.Value, 0, 64)
	return err == nil && value == low
}

func auditMutationProcessFunction(
	t *testing.T,
	set *token.FileSet,
	fn *mutationProcessContractFunction,
	functions []*mutationProcessContractFunction,
	readOnlyPatterns [][]string,
) {
	t.Helper()
	state := mutationProcessFunctionState{
		execCommands:     map[string]bool{},
		execConstructors: map[string]bool{},
	}
	seedMutationProcessTypes(fn, functions, &state)

	ast.Inspect(fn.decl.Body, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.AssignStmt:
			learnMutationProcessAssignment(fn.file, typed, &state)
		case *ast.ValueSpec:
			learnMutationProcessValueSpec(fn.file, typed, &state)
		case *ast.CompositeLit:
			if isMutationExecCmdType(fn.file, typed.Type) {
				t.Errorf(
					"%s: durable handler constructs raw exec.Cmd at %s",
					fn.file.name,
					set.Position(typed.Pos()),
				)
			}
		case *ast.CallExpr:
			auditMutationProcessCall(t, set, fn, typed, state, readOnlyPatterns)
		}
		return true
	})
}

func seedMutationProcessTypes(
	fn *mutationProcessContractFunction,
	functions []*mutationProcessContractFunction,
	state *mutationProcessFunctionState,
) {
	if fn.decl.Type.Params != nil {
		for _, field := range fn.decl.Type.Params.List {
			if !isMutationExecCmdType(fn.file, field.Type) {
				continue
			}
			for _, name := range field.Names {
				state.execCommands[name.Name] = true
			}
		}
	}
	for _, candidate := range functions {
		if !candidate.exempt && mutationFunctionReturnsExecCmd(candidate) {
			state.execConstructors[candidate.decl.Name.Name] = true
		}
	}
}

func learnMutationProcessAssignment(
	file *mutationProcessContractFile,
	assign *ast.AssignStmt,
	state *mutationProcessFunctionState,
) {
	for index, right := range assign.Rhs {
		if index >= len(assign.Lhs) {
			break
		}
		left, ok := assign.Lhs[index].(*ast.Ident)
		if !ok {
			continue
		}
		if isMutationExecConstructorReference(file, right) {
			state.execConstructors[left.Name] = true
		}
		if mutationExpressionProducesExecCmd(file, right, *state) {
			state.execCommands[left.Name] = true
		}
	}
}

func learnMutationProcessValueSpec(
	file *mutationProcessContractFile,
	spec *ast.ValueSpec,
	state *mutationProcessFunctionState,
) {
	if isMutationExecCmdType(file, spec.Type) {
		for _, name := range spec.Names {
			state.execCommands[name.Name] = true
		}
	}
	for index, value := range spec.Values {
		if index >= len(spec.Names) {
			break
		}
		if isMutationExecConstructorReference(file, value) {
			state.execConstructors[spec.Names[index].Name] = true
		}
		if mutationExpressionProducesExecCmd(file, value, *state) {
			state.execCommands[spec.Names[index].Name] = true
		}
	}
}

func auditMutationProcessCall(
	t *testing.T,
	set *token.FileSet,
	fn *mutationProcessContractFunction,
	call *ast.CallExpr,
	state mutationProcessFunctionState,
	readOnlyPatterns [][]string,
) {
	t.Helper()
	if allowedCentralMutationProcessCall(fn, call) {
		return
	}
	if ident, ok := call.Fun.(*ast.Ident); ok {
		if state.execConstructors[ident.Name] || mutationDotExecConstructor(fn.file, ident.Name) {
			auditMutationDirectExec(t, set, fn, call, 0, readOnlyPatterns)
		}
		if mutationDotStartProcess(fn.file, ident.Name) || mutationDotSpawn(fn.file, ident.Name) ||
			mutationDotSpawnSyscall(fn.file, ident.Name, call) {
			t.Errorf(
				"%s: durable handler bypasses tracking with %s at %s",
				fn.file.name,
				ident.Name,
				set.Position(call.Pos()),
			)
		}
		return
	}

	selector, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return
	}
	if mutationImportedSelector(fn.file, selector, "os/exec", "Command", "CommandContext") {
		nameArg := 0
		if selector.Sel.Name == "CommandContext" {
			nameArg = 1
		}
		auditMutationDirectExec(t, set, fn, call, nameArg, readOnlyPatterns)
		return
	}
	if mutationImportedSelector(fn.file, selector, "os", "StartProcess") ||
		mutationImportedSelector(fn.file, selector, "syscall", "ForkExec", "StartProcess", "Exec") ||
		mutationImportedSelector(fn.file, selector, "golang.org/x/sys/unix", "ForkExec", "Exec") {
		t.Errorf(
			"%s: durable handler bypasses tracking with %s at %s",
			fn.file.name,
			selector.Sel.Name,
			set.Position(call.Pos()),
		)
		return
	}
	if mutationSpawnSyscall(fn.file, selector, call) {
		t.Errorf(
			"%s: durable handler uses a direct process-spawn syscall at %s",
			fn.file.name,
			set.Position(call.Pos()),
		)
		return
	}
	if mutationRawExecTerminalCall(fn.file, selector, state) {
		t.Errorf(
			"%s: durable handler invokes raw exec.Cmd.%s at %s",
			fn.file.name,
			selector.Sel.Name,
			set.Position(call.Pos()),
		)
	}
}

func auditMutationDirectExec(
	t *testing.T,
	set *token.FileSet,
	fn *mutationProcessContractFunction,
	call *ast.CallExpr,
	nameArg int,
	readOnlyPatterns [][]string,
) {
	t.Helper()
	if len(call.Args) <= nameArg || call.Ellipsis.IsValid() ||
		!matchesReadOnlyExecPattern(call.Args[nameArg:], readOnlyPatterns) {
		command := ""
		if len(call.Args) > nameArg {
			command, _ = stringLiteral(call.Args[nameArg])
		}
		t.Errorf(
			"%s: direct mutating or unclassified exec %q at %s",
			fn.file.name,
			command,
			set.Position(call.Pos()),
		)
	}
}

func mutationRawExecTerminalCall(
	file *mutationProcessContractFile,
	selector *ast.SelectorExpr,
	state mutationProcessFunctionState,
) bool {
	switch selector.Sel.Name {
	case "Run", "Output", "CombinedOutput", "Start", "Wait":
	default:
		return false
	}
	// A direct constructor call is classified separately against the exact
	// read-only command allowlist. Stored commands remain forbidden because
	// their Args field could be changed before the terminal method is called.
	// Doğrudan constructor çağrısı ayrıca tam salt-okunur komut izin listesine
	// göre sınıflandırılır. Saklanan komutlar, terminal yöntem çağrılmadan önce
	// Args alanları değiştirilebildiği için yasak kalır.
	if call, ok := selector.X.(*ast.CallExpr); ok && mutationExecConstructorCall(file, call) {
		return false
	}
	return mutationExpressionProducesExecCmd(file, selector.X, state)
}

func mutationExpressionProducesExecCmd(
	file *mutationProcessContractFile,
	expr ast.Expr,
	state mutationProcessFunctionState,
) bool {
	switch typed := expr.(type) {
	case *ast.Ident:
		return state.execCommands[typed.Name]
	case *ast.ParenExpr:
		return mutationExpressionProducesExecCmd(file, typed.X, state)
	case *ast.UnaryExpr:
		if typed.Op == token.AND {
			if literal, ok := typed.X.(*ast.CompositeLit); ok {
				return isMutationExecCmdType(file, literal.Type)
			}
		}
	case *ast.CompositeLit:
		return isMutationExecCmdType(file, typed.Type)
	case *ast.CallExpr:
		if mutationExecConstructorCall(file, typed) {
			return true
		}
		if ident, ok := typed.Fun.(*ast.Ident); ok {
			return state.execConstructors[ident.Name]
		}
	}
	return false
}

func isMutationExecConstructorReference(
	file *mutationProcessContractFile,
	expr ast.Expr,
) bool {
	switch typed := expr.(type) {
	case *ast.SelectorExpr:
		return mutationImportedSelector(file, typed, "os/exec", "Command", "CommandContext")
	case *ast.Ident:
		return mutationDotExecConstructor(file, typed.Name)
	}
	return false
}

func mutationExecConstructorCall(
	file *mutationProcessContractFile,
	call *ast.CallExpr,
) bool {
	switch called := call.Fun.(type) {
	case *ast.SelectorExpr:
		return mutationImportedSelector(file, called, "os/exec", "Command", "CommandContext")
	case *ast.Ident:
		return mutationDotExecConstructor(file, called.Name)
	}
	return false
}

func mutationDotExecConstructor(file *mutationProcessContractFile, name string) bool {
	return file.dotImports["os/exec"] && (name == "Command" || name == "CommandContext")
}

func mutationDotStartProcess(file *mutationProcessContractFile, name string) bool {
	return file.dotImports["os"] && name == "StartProcess"
}

func mutationDotSpawn(file *mutationProcessContractFile, name string) bool {
	if file.dotImports["syscall"] &&
		(name == "ForkExec" || name == "StartProcess" || name == "Exec") {
		return true
	}
	return file.dotImports["golang.org/x/sys/unix"] && (name == "ForkExec" || name == "Exec")
}

func mutationDotSpawnSyscall(
	file *mutationProcessContractFile,
	name string,
	call *ast.CallExpr,
) bool {
	if !file.dotImports["syscall"] && !file.dotImports["golang.org/x/sys/unix"] {
		return false
	}
	switch name {
	case "Syscall", "Syscall6", "RawSyscall", "RawSyscall6":
	default:
		return false
	}
	return len(call.Args) == 0 || mutationMentionsSpawnSyscall(call.Args[0])
}

func mutationImportedSelector(
	file *mutationProcessContractFile,
	selector *ast.SelectorExpr,
	importPath string,
	names ...string,
) bool {
	owner, ok := selector.X.(*ast.Ident)
	if !ok || file.imports[owner.Name] != importPath {
		return false
	}
	for _, name := range names {
		if selector.Sel.Name == name {
			return true
		}
	}
	return false
}

func mutationSpawnSyscall(
	file *mutationProcessContractFile,
	selector *ast.SelectorExpr,
	call *ast.CallExpr,
) bool {
	if !mutationImportedSelector(
		file,
		selector,
		"syscall",
		"Syscall",
		"Syscall6",
		"RawSyscall",
		"RawSyscall6",
	) && !mutationImportedSelector(
		file,
		selector,
		"golang.org/x/sys/unix",
		"Syscall",
		"Syscall6",
		"RawSyscall",
		"RawSyscall6",
	) {
		return false
	}
	if len(call.Args) == 0 {
		return true
	}
	found := false
	ast.Inspect(call.Args[0], func(node ast.Node) bool {
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "SYS_FORK", "SYS_VFORK", "SYS_CLONE", "SYS_CLONE3", "SYS_EXECVE", "SYS_EXECVEAT":
			found = true
			return false
		}
		return true
	})
	return found
}

func mutationMentionsSpawnSyscall(expr ast.Expr) bool {
	found := false
	ast.Inspect(expr, func(node ast.Node) bool {
		if ident, ok := node.(*ast.Ident); ok {
			switch ident.Name {
			case "SYS_FORK", "SYS_VFORK", "SYS_CLONE", "SYS_CLONE3", "SYS_EXECVE", "SYS_EXECVEAT":
				found = true
				return false
			}
		}
		selector, ok := node.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		switch selector.Sel.Name {
		case "SYS_FORK", "SYS_VFORK", "SYS_CLONE", "SYS_CLONE3", "SYS_EXECVE", "SYS_EXECVEAT":
			found = true
			return false
		}
		return true
	})
	return found
}

func mutationFunctionReturnsExecCmd(fn *mutationProcessContractFunction) bool {
	if fn.decl.Type.Results == nil {
		return false
	}
	for _, result := range fn.decl.Type.Results.List {
		if isMutationExecCmdType(fn.file, result.Type) {
			return true
		}
	}
	return false
}

func isMutationExecCmdType(file *mutationProcessContractFile, expr ast.Expr) bool {
	if star, ok := expr.(*ast.StarExpr); ok {
		expr = star.X
	}
	if ident, ok := expr.(*ast.Ident); ok {
		return ident.Name == "Cmd" && file.dotImports["os/exec"]
	}
	selector, ok := expr.(*ast.SelectorExpr)
	if !ok || selector.Sel.Name != "Cmd" {
		return false
	}
	owner, ok := selector.X.(*ast.Ident)
	return ok && file.imports[owner.Name] == "os/exec"
}
