//go:build linux

package recoveryruntime

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/agentnativecontract"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
)

func TestMailApplicationCompatibilityPreservesNativeOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	cases := []struct {
		name    string
		allowed bool
	}{
		{"absent", true}, {"missing-hook-parents", true}, {"legacy", true}, {"independent-compatible", true},
		{"independent-historical", false}, {"independent-agent-edited", false}, {"independent-contract-edited", false},
		{"foreign-hook", false}, {"hook-link", false}, {"hook-mode", false}, {"independent-helper-edited", false},
		{"independent-unit-edited", false}, {"independent-kit-missing", false}, {"absent-unit", false},
		{"legacy-unit", false}, {"absent-dangling-enable-link", false}, {"legacy-dangling-enable-link", false},
		{"wants-parent-link", false}, {"hook-parent-link", false},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			root, e := os.MkdirTemp("/run", "celikpanel-mail-app-")
			if e != nil {
				t.Fatal(e)
			}
			defer os.RemoveAll(root)
			bin, units, hooks, kit := filepath.Join(root, "bin"), filepath.Join(root, "units"), filepath.Join(root, "hooks"), filepath.Join(root, "kit")
			for _, p := range []string{bin, units, hooks, kit} {
				if e = os.Mkdir(p, 0755); e != nil {
					t.Fatal(e)
				}
			}
			put := func(p string, b []byte, mode os.FileMode) {
				t.Helper()
				if e := os.WriteFile(p, b, mode); e != nil {
					t.Fatal(e)
				}
				if e := os.Chmod(p, mode); e != nil {
					t.Fatal(e)
				}
			}
			agent := []byte("not an executable; historical Agents must never be invoked")
			put(filepath.Join(bin, "agent"), agent, 0755)
			hook := filepath.Join(hooks, mailrenewalkit.HookName)
			// No declaration in either absence or legacy fixtures: they must remain
			// upgradeable before enrollment. Native enrollment requires exact support.
			if strings.HasPrefix(tt.name, "independent-") {
				source := filepath.Join(root, "source")
				generation := mailRenewalFixture(t, source, []byte("native fixture helper"))
				final := filepath.Join(kit, generation)
				if e = os.Rename(source, final); e != nil {
					t.Fatal(e)
				}
				for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName, mailrenewalkit.HookName} {
					raw, e := os.ReadFile(filepath.Join(final, name))
					if e != nil {
						t.Fatal(e)
					}
					target, mode := filepath.Join(units, name), os.FileMode(0644)
					if name == mailrenewalkit.HookName {
						target, mode = hook, 0755
					}
					put(target, raw, mode)
				}
				c, e := agentnativecontract.New(agent, strings.Repeat("a", 40))
				if e != nil {
					t.Fatal(e)
				}
				raw, e := agentnativecontract.Encode(c)
				if e != nil {
					t.Fatal(e)
				}
				if tt.name != "independent-historical" {
					put(filepath.Join(bin, agentnativecontract.FileName), raw, 0644)
				}
				switch tt.name {
				case "independent-agent-edited":
					put(filepath.Join(bin, "agent"), []byte("different agent"), 0755)
				case "independent-contract-edited":
					put(filepath.Join(bin, agentnativecontract.FileName), []byte("{}\n"), 0644)
				case "independent-helper-edited":
					put(filepath.Join(final, mailrenewalkit.BinaryName), []byte("owner helper"), 0755)
				case "independent-unit-edited":
					put(filepath.Join(units, mailrenewalkit.ServiceName), []byte("owner unit"), 0644)
				case "independent-kit-missing":
					os.Rename(final, final+".owner")
				}
			} else {
				if strings.HasPrefix(tt.name, "legacy") {
					put(hook, mailrenewalkit.LegacyHook(), 0755)
				}
				switch tt.name {
				case "missing-hook-parents":
					hook = filepath.Join(root, "absent", "nested", mailrenewalkit.HookName)
				case "foreign-hook":
					put(hook, []byte("owner hook"), 0755)
				case "hook-link":
					os.Symlink("/missing-owner-hook", hook)
				case "hook-mode":
					put(hook, mailrenewalkit.LegacyHook(), 0775)
				case "absent-unit", "legacy-unit":
					put(filepath.Join(units, mailrenewalkit.ServiceName), []byte("retained native unit"), 0644)
				case "absent-dangling-enable-link", "legacy-dangling-enable-link":
					wants := filepath.Join(units, "timers.target.wants")
					os.Mkdir(wants, 0755)
					os.Symlink("../"+mailrenewalkit.TimerName, filepath.Join(wants, mailrenewalkit.TimerName))
				case "wants-parent-link":
					os.Symlink(hooks, filepath.Join(units, "timers.target.wants"))
				case "hook-parent-link":
					os.Rename(hooks, hooks+".owner")
					os.Symlink(hooks+".owner", hooks)
				}
			}
			// Snapshot file bytes and identities: compatibility is observation only.
			before := map[string]os.FileInfo{}
			content := map[string]string{}
			filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
				if e != nil {
					t.Fatal(e)
				}
				before[p] = i
				if i.Mode().IsRegular() {
					b, e := os.ReadFile(p)
					if e != nil {
						t.Fatal(e)
					}
					content[p] = string(b)
				}
				return nil
			})
			err := checkMailApplicationCompatibilityAt(bin, hook, units, kit)
			if (err == nil) != tt.allowed {
				t.Fatalf("allowed=%v: %v", tt.allowed, err)
			}
			seen := 0
			filepath.Walk(root, func(p string, i os.FileInfo, e error) error {
				if e != nil {
					t.Fatal(e)
				}
				old, ok := before[p]
				if !ok || !os.SameFile(old, i) || old.Mode() != i.Mode() {
					t.Fatalf("observation mutated %s", p)
				}
				if i.Mode().IsRegular() {
					b, e := os.ReadFile(p)
					if e != nil || string(b) != content[p] {
						t.Fatalf("changed bytes %s", p)
					}
				}
				seen++
				return nil
			})
			if seen != len(before) {
				t.Fatal("observation removed evidence")
			}
		})
	}
}

func TestMailCompatibilityAbsenceProofRejectsLateOwnerCreation(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("root descriptor fixture")
	}
	root, e := os.MkdirTemp("/run", "celikpanel-mail-absence-")
	if e != nil {
		t.Fatal(e)
	}
	defer os.RemoveAll(root)
	state := &runtimeState{config: resolveConfig{anchor: "/", uid: 0, gid: 0, protectedDirectoryGroups: true}}
	defer state.close()
	p, verify, e := optionalMailHookParent(state, filepath.Join(root, "missing", "nested"))
	if e != nil || p != nil || verify == nil {
		t.Fatalf("absence proof: %v", e)
	}
	if e = verify(); e != nil {
		t.Fatal(e)
	}
	if e = os.Mkdir(filepath.Join(root, "missing"), 0755); e != nil {
		t.Fatal(e)
	}
	if e = verify(); e == nil {
		t.Fatal("stale absence accepted")
	}
}
