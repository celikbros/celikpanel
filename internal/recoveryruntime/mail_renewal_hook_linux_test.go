//go:build linux

package recoveryruntime

import (
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"os"
	"path/filepath"
	"testing"
)

func TestMailRenewalHookObservationPreservesEvidence(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("native root fixture")
	}
	for _, scenario := range []string{"absent", "legacy", "independent", "foreign-hook", "hook-symlink", "hook-hardlink", "hook-mode", "unit-edited", "timer-missing", "helper-edited", "late-hook-edit", "late-helper-edit", "late-unit-edit", "late-absent-created", "late-parent-moved", "protected-parent-group", "writable-parent-group", "foreign-parent-owner", "late-parent-group", "kit-parent-group"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "celikpanel-hook-observation-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			units := filepath.Join(root, "units")
			hooks := filepath.Join(root, "hooks")
			runtimeRoot := filepath.Join(root, "runtime")
			for _, p := range []string{units, hooks, runtimeRoot} {
				if err = os.Mkdir(p, 0755); err != nil {
					t.Fatal(err)
				}
			}
			source := filepath.Join(root, "source")
			generation := mailRenewalFixture(t, source, []byte("native helper"))
			final := filepath.Join(runtimeRoot, generation)
			if err = os.Rename(source, final); err != nil {
				t.Fatal(err)
			}
			hook := filepath.Join(hooks, mailrenewalkit.HookName)
			put := func(path string, raw []byte, mode os.FileMode) {
				t.Helper()
				if err := os.WriteFile(path, raw, mode); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, mode); err != nil {
					t.Fatal(err)
				}
			}
			for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName, mailrenewalkit.HookName} {
				raw, e := os.ReadFile(filepath.Join(final, name))
				if e != nil {
					t.Fatal(e)
				}
				target := filepath.Join(units, name)
				mode := os.FileMode(0644)
				if name == mailrenewalkit.HookName {
					target = hook
					mode = 0755
				}
				put(target, raw, mode)
			}
			switch scenario {
			case "protected-parent-group", "late-parent-group":
				os.Chown(hooks, 0, 65534)
				os.Chmod(hooks, 0700)
			case "writable-parent-group":
				os.Chown(hooks, 0, 65534)
				os.Chmod(hooks, 0770)
			case "foreign-parent-owner":
				os.Chown(hooks, 65534, 0)
			case "kit-parent-group":
				os.Chown(final, 0, 65534)
			case "absent", "late-absent-created":
				os.Remove(hook)
			case "legacy":
				put(hook, mailrenewalkit.LegacyHook(), 0755)
			case "foreign-hook":
				put(hook, []byte("owner hook"), 0755)
			case "hook-symlink":
				os.Remove(hook)
				os.Symlink(filepath.Join(final, mailrenewalkit.HookName), hook)
			case "hook-hardlink":
				os.Link(hook, filepath.Join(root, "owner-link"))
			case "hook-mode":
				os.Chmod(hook, 0775)
			case "unit-edited":
				put(filepath.Join(units, mailrenewalkit.ServiceName), []byte("owner unit"), 0644)
			case "timer-missing":
				os.Remove(filepath.Join(units, mailrenewalkit.TimerName))
			case "helper-edited":
				put(filepath.Join(final, mailrenewalkit.BinaryName), []byte("owner helper"), 0755)
			}
			h, err := inspectMailRenewalHookAt(hook, units, runtimeRoot)
			bad := map[string]bool{"foreign-hook": true, "hook-symlink": true, "hook-hardlink": true, "hook-mode": true, "unit-edited": true, "timer-missing": true, "helper-edited": true, "writable-parent-group": true, "foreign-parent-owner": true, "kit-parent-group": true}[scenario]
			if bad {
				if err == nil {
					h.Close()
					t.Fatal("unsafe enrollment accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer h.Close()
			want := MailRenewalHookIndependent
			if scenario == "legacy" {
				want = MailRenewalHookLegacy
			}
			if scenario == "absent" || scenario == "late-absent-created" {
				want = MailRenewalHookAbsent
			}
			if h.Mode != want {
				t.Fatalf("mode %s", h.Mode)
			}
			if want == MailRenewalHookIndependent && h.Generation != generation {
				t.Fatal("wrong generation")
			}
			switch scenario {
			case "late-parent-group":
				os.Chown(hooks, 0, 65533)
			case "late-hook-edit", "late-absent-created":
				put(hook, []byte("later owner hook"), 0755)
			case "late-helper-edit":
				put(filepath.Join(final, mailrenewalkit.BinaryName), []byte("later owner helper"), 0755)
			case "late-unit-edit":
				put(filepath.Join(units, mailrenewalkit.ServiceName), []byte("later owner unit"), 0644)
			case "late-parent-moved":
				os.Rename(hooks, hooks+".owner")
				os.Mkdir(hooks, 0755)
			}
			err = h.Revalidate()
			late := len(scenario) > 5 && scenario[:5] == "late-"
			if (err != nil) != late {
				t.Fatalf("late=%v revalidate=%v", late, err)
			}
			if scenario == "late-hook-edit" || scenario == "late-absent-created" {
				raw, e := os.ReadFile(hook)
				if e != nil || string(raw) != "later owner hook" {
					t.Fatal("owner content changed")
				}
			}
		})
	}
}
