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

// R-062. The password reached psql in its argument list, where every local
// account could read it out of the process table for as long as the command
// ran. Moving one call site would not have kept it moved: the next person to
// need a statement would copy the neighbouring line, and the neighbouring line
// was the defect. So the argument form is refused outright, and the only way
// to run a statement is the one that puts it on stdin.
//
// This reads the source rather than the behaviour on purpose. There is no
// PostgreSQL on the machine that runs the tests, so a test that called psql
// would prove nothing; what can be proved without one is that no line asks
// psql to take a statement as an argument.
//
// R-062. Parola psql'e argüman listesinde ulaşıyordu; komut çalıştığı sürece
// her yerel hesap onu süreç tablosundan okuyabilirdi. Tek bir çağrı yerini
// taşımak onu taşınmış tutmazdı: ifadeye ihtiyaç duyan bir sonraki kişi
// komşu satırı kopyalardı ve kusur o komşu satırdı. Bu yüzden argüman biçimi
// tümden reddedilir.
func TestNoPsqlInvocationTakesItsStatementAsAnArgument(t *testing.T) {
	// psql's own spellings for "here is the statement, on the command line".
	// psql'in "ifade burada, komut satırında" yazımları.
	statementFlags := map[string]bool{
		"-c":        true,
		"--command": true,
		"-f":        false, // a file, not a statement; listed so the intent is visible
	}

	root := ".."
	var offenders []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			switch info.Name() {
			case "artifacts", ".attic", ".git", "node_modules", "web":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		fileSet := token.NewFileSet()
		parsed, parseErr := parser.ParseFile(fileSet, path, nil, 0)
		if parseErr != nil {
			return nil // not this test's business to report a broken parse
		}
		ast.Inspect(parsed, func(node ast.Node) bool {
			call, ok := node.(*ast.CallExpr)
			if !ok {
				return true
			}
			selector, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := selector.X.(*ast.Ident)
			if !ok || pkg.Name != "exec" {
				return true
			}
			if selector.Sel.Name != "Command" && selector.Sel.Name != "CommandContext" {
				return true
			}
			literals := map[string]bool{}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, unquoteErr := strconv.Unquote(lit.Value)
				if unquoteErr != nil {
					continue
				}
				literals[value] = true
			}
			if !literals["psql"] {
				return true
			}
			for flag, isStatement := range statementFlags {
				if isStatement && literals[flag] {
					offenders = append(offenders, fileSet.Position(call.Pos()).String()+" passes "+flag)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(offenders) > 0 {
		t.Fatalf(
			"psql was given a statement in its argument list, where the process "+
				"table can read it; put the statement on stdin instead, as "+
				"newPostgreSQLStatementCommand does:\n\t%s",
			strings.Join(offenders, "\n\t"),
		)
	}
}

// The guard above is worth nothing if it cannot fail, and the shape it looks
// for no longer exists anywhere to prove it against. So the detection itself
// is exercised on a source file written here.
//
// Yukarıdaki muhafız, düşemiyorsa hiçbir şey ifade etmez; aradığı biçim ise
// artık hiçbir yerde yok. Bu yüzden tespit, burada yazılan bir kaynak dosya
// üzerinde sınanır.
func TestTheGuardWouldHaveCaughtTheDefectItClosed(t *testing.T) {
	source := `package main

import "os/exec"

func leak(statement string) {
	_ = exec.Command("sudo", "-u", "postgres", "psql", "-c", statement)
}
`
	path := filepath.Join(t.TempDir(), "leak.go")
	if err := os.WriteFile(path, []byte(source), 0o600); err != nil {
		t.Fatal(err)
	}

	fileSet := token.NewFileSet()
	parsed, err := parser.ParseFile(fileSet, path, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	ast.Inspect(parsed, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		pkg, ok := selector.X.(*ast.Ident)
		if !ok || pkg.Name != "exec" || selector.Sel.Name != "Command" {
			return true
		}
		literals := map[string]bool{}
		for _, arg := range call.Args {
			lit, ok := arg.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				continue
			}
			value, unquoteErr := strconv.Unquote(lit.Value)
			if unquoteErr == nil {
				literals[value] = true
			}
		}
		if literals["psql"] && literals["-c"] {
			found = true
		}
		return true
	})
	if !found {
		t.Fatal("the guard's own detection did not recognise the defect it was written for")
	}
}

// The statement path has to keep the two flags that make it correct, not just
// the stdin that makes it private. Without ON_ERROR_STOP a psql reading from
// stdin exits zero on a failed statement, and every caller here decides
// success from the exit status - so dropping it would turn failures into
// silent successes.
//
// İfade yolu, yalnızca onu gizli kılan stdin'i değil, onu doğru kılan iki
// bayrağı da korumalıdır. ON_ERROR_STOP olmadan stdin okuyan psql, başarısız
// bir ifadede sıfır döner.
func TestPostgreSQLStatementGoesOnStdinAndStopsOnError(t *testing.T) {
	command := newPostgreSQLStatementCommand("SELECT 1;")

	arguments := strings.Join(command.Args, "|")
	if strings.Contains(arguments, "SELECT 1") {
		t.Errorf("the statement reached the argument list: %v", command.Args)
	}
	if command.Stdin == nil {
		t.Fatal("the statement was not put on stdin")
	}
	for _, want := range []string{"ON_ERROR_STOP=on", "--no-psqlrc"} {
		if !strings.Contains(arguments, want) {
			t.Errorf("psql was launched without %s: %v", want, command.Args)
		}
	}
}
