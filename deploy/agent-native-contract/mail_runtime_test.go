package main

import (
	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeBuildKit(t *testing.T) (string, string) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "mail-renewal-runtime")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	m, files, _ := mailrenewalkit.Payload([]byte("build runtime data"))
	files[mailrenewalkit.ManifestName], _ = mailrenewalkit.Encode(m)
	for name, raw := range files {
		if err := os.WriteFile(filepath.Join(root, name), raw, 0644); err != nil {
			t.Fatal(err)
		}
	}
	return root, m.Generation
}
func TestBuildRuntimeInputRefusal(t *testing.T) {
	for _, kind := range []string{"exact", "extra", "missing", "changed", "symbolic-file", "symbolic-root", "oversize", "directory"} {
		t.Run(kind, func(t *testing.T) {
			root, generation := writeBuildKit(t)
			timer := filepath.Join(root, mailrenewalkit.TimerName)
			must := func(err error) {
				t.Helper()
				if err != nil {
					t.Fatal(err)
				}
			}
			switch kind {
			case "extra":
				must(os.WriteFile(filepath.Join(root, "owner"), []byte("preserve"), 0600))
			case "missing":
				must(os.Remove(timer))
			case "changed":
				must(os.WriteFile(timer, []byte("owner unit"), 0644))
			case "symbolic-file":
				saved := filepath.Join(t.TempDir(), "timer")
				must(os.Rename(timer, saved))
				must(os.Symlink(saved, timer))
			case "symbolic-root":
				saved := root + "-saved"
				must(os.Rename(root, saved))
				must(os.Symlink(saved, root))
			case "oversize":
				must(os.Truncate(timer, 16385))
			case "directory":
				must(os.Remove(timer))
				must(os.Mkdir(timer, 0755))
			}
			c, _ := agentnativecontract.New([]byte("agent"), strings.Repeat("a", 40))
			got, err := bindBuildMailRuntime(c, root)
			if kind == "exact" {
				if err != nil || got.MailRenewalGeneration != generation {
					t.Fatal(got, err)
				}
			} else if err == nil {
				t.Fatal("unsafe kit gained release binding")
			}
		})
	}
}
