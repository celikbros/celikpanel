package licensing

import (
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// Runs in both build variants: it inspects which sources each variant compiles.
func acceptanceVariantFiles(t *testing.T, tags ...string) []string {
	t.Helper()
	ctx := build.Default
	ctx.BuildTags = tags
	ctx.CgoEnabled = false
	pkg, err := ctx.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.GoFiles
}

func TestAcceptanceSeamSourcesAreConfinedToTheTaggedBuild(t *testing.T) {
	ordinary := acceptanceVariantFiles(t)
	tagged := acceptanceVariantFiles(t, "acceptance_license")
	var onlyOrdinary, onlyTagged []string
	for _, name := range ordinary {
		if !slices.Contains(tagged, name) {
			onlyOrdinary = append(onlyOrdinary, name)
		}
	}
	for _, name := range tagged {
		if !slices.Contains(ordinary, name) {
			onlyTagged = append(onlyTagged, name)
		}
	}
	owner := "acceptance_owner_linux.go"
	if runtime.GOOS != "linux" {
		owner = "acceptance_owner_other.go"
	}
	if !slices.Equal(onlyOrdinary, []string{"acceptance_off.go"}) ||
		!slices.Equal(onlyTagged, []string{"acceptance_fixture.go", owner}) {
		t.Fatalf("variant files: ordinary-only %v, tagged-only %v", onlyOrdinary, onlyTagged)
	}

	// The ordinary stub has no fixture, verifier, environment or file lookup.
	fset := token.NewFileSet()
	stub, err := parser.ParseFile(fset, "acceptance_off.go", nil, parser.ParseComments)
	if err != nil {
		t.Fatal(err)
	}
	var imports []string
	for _, spec := range stub.Imports {
		path, _ := strconv.Unquote(spec.Path.Value)
		imports = append(imports, path)
	}
	if !slices.Equal(imports, []string{"crypto/ed25519"}) {
		t.Fatalf("ordinary stub imports %v", imports)
	}
	for _, decl := range stub.Decls {
		if gen, ok := decl.(*ast.GenDecl); ok && gen.Tok == token.IMPORT {
			continue
		}
		ast.Inspect(decl, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BasicLit:
				if v.Kind == token.STRING {
					t.Errorf("ordinary stub contains a string literal %s", v.Value)
				}
			case *ast.FuncDecl:
				if v.Name.Name != "NewServer" {
					t.Errorf("ordinary stub declares %s", v.Name.Name)
				}
			case *ast.Ident:
				switch v.Name {
				case "os", "Getenv", "LookupEnv", "ReadFile", "Open", "Stat", "Lstat", "seam":
					t.Errorf("ordinary stub references %s", v.Name)
				}
			}
			return true
		})
	}

	// No ordinary source assigns the seam or carries the fixture's identity.
	forbidden := []string{"ACCEPTANCE FIXTURE", "CPK-acce57f1c7", "/etc/celikpanel-dns-kill-matrix", "acceptance-fixture-license"}
	for _, name := range ordinary {
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			switch v := n.(type) {
			case *ast.BasicLit:
				for _, text := range forbidden {
					if strings.Contains(v.Value, text) {
						t.Errorf("%s: ordinary source carries %q", name, text)
					}
				}
			case *ast.AssignStmt:
				for _, lhs := range v.Lhs {
					if selector, ok := lhs.(*ast.SelectorExpr); ok && selector.Sel.Name == "seam" {
						t.Errorf("%s: ordinary source assigns the acceptance seam", name)
					}
				}
			case *ast.KeyValueExpr:
				if key, ok := v.Key.(*ast.Ident); ok && key.Name == "seam" {
					t.Errorf("%s: ordinary source sets the acceptance seam", name)
				}
			}
			return true
		})
	}
}
