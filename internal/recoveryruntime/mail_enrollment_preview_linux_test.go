//go:build linux

package recoveryruntime

import (
	"context"
	"fmt"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"os"
	"path/filepath"
	"testing"
)

func TestMailEnrollmentPreviewPreservesNativeBeforeState(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("protected native paths require root fixture")
	}
	for _, scenario := range []string{"fresh", "missing-ancestor", "legacy", "independent", "owner-disabled", "foreign-hook", "stray-service", "dangling-enable", "loaded-stray", "native-unknown", "native-busy", "native-override", "late-hook-created", "late-parent-created", "late-parent-symlink", "parent-symlink", "parent-file", "writable-parent", "foreign-parent", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			root, err := os.MkdirTemp("/run", "mail-enrollment-preview-")
			if err != nil {
				t.Fatal(err)
			}
			defer os.RemoveAll(root)
			units, hooks, runtime := filepath.Join(root, "units"), filepath.Join(root, "hooks"), filepath.Join(root, "runtime")
			for _, path := range []string{units, hooks, runtime} {
				if err := os.Mkdir(path, 0755); err != nil {
					t.Fatal(err)
				}
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
			independent := scenario == "independent" || scenario == "owner-disabled" || scenario == "native-busy" || scenario == "native-override"
			generation := ""
			if independent {
				source := filepath.Join(root, "source")
				generation = mailRenewalFixture(t, source, []byte("helper fixture"))
				target := filepath.Join(runtime, generation)
				if err := os.Rename(source, target); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{mailrenewalkit.ServiceName, mailrenewalkit.TimerName, mailrenewalkit.HookName} {
					raw, err := os.ReadFile(filepath.Join(target, name))
					if err != nil {
						t.Fatal(err)
					}
					if name == mailrenewalkit.HookName {
						put(hook, raw, 0755)
					} else {
						put(filepath.Join(units, name), raw, 0644)
					}
				}
			}
			switch scenario {
			case "legacy":
				put(hook, mailrenewalkit.LegacyHook(), 0755)
			case "foreign-hook":
				put(hook, []byte("owner script"), 0755)
			case "stray-service":
				put(filepath.Join(units, mailrenewalkit.ServiceName), []byte("owner unit"), 0644)
			case "dangling-enable":
				os.Mkdir(filepath.Join(units, "timers.target.wants"), 0755)
				os.Symlink("/absent-owner-unit", filepath.Join(units, "timers.target.wants", mailrenewalkit.TimerName))
			case "missing-ancestor", "late-parent-created", "late-parent-symlink":
				hook = filepath.Join(hooks, "missing", "deploy", mailrenewalkit.HookName)
			case "parent-symlink":
				os.Remove(hooks)
				os.Symlink(units, hooks)
			case "parent-file":
				os.Remove(hooks)
				put(hooks, []byte("owner file"), 0644)
			case "writable-parent":
				os.Chmod(hooks, 0777)
			case "foreign-parent":
				os.Chown(hooks, 65534, 0)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if scenario == "cancelled" {
				cancel()
			}
			calls := 0
			preview, err := inspectMailEnrollmentPreviewAt(ctx, hook, units, runtime, func(_ context.Context, unit string) ([]byte, error) {
				calls++
				if scenario == "native-unknown" {
					return []byte("LoadState=not-found\n"), nil
				}
				load, fragment, drop, reload, active, enabled := "not-found", "", "", "no", "inactive", ""
				if independent || scenario == "loaded-stray" {
					load = "loaded"
					fragment = "/etc/systemd/system/" + unit
					enabled = "static"
					if unit == mailrenewalkit.TimerName {
						enabled = "enabled"
						active = "active"
						if scenario == "owner-disabled" {
							enabled = "disabled"
							active = "inactive"
						}
					}
					if scenario == "native-busy" && unit == mailrenewalkit.ServiceName {
						active = "active"
					}
					if scenario == "native-override" {
						drop = "/etc/systemd/system/owner.conf"
					}
				}
				return []byte(fmt.Sprintf("LoadState=%s\nFragmentPath=%s\nDropInPaths=%s\nNeedDaemonReload=%s\nActiveState=%s\nUnitFileState=%s\n", load, fragment, drop, reload, active, enabled)), nil
			})
			good := scenario == "fresh" || scenario == "missing-ancestor" || scenario == "legacy" || scenario == "independent" || scenario == "owner-disabled" || scenario == "late-hook-created" || scenario == "late-parent-created" || scenario == "late-parent-symlink"
			if !good {
				if err == nil {
					preview.Close()
					t.Fatal("unverified native state was accepted")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			if calls != 2 {
				t.Fatal("native observation incomplete")
			}
			if preview.Generation != generation {
				t.Fatal("wrong native generation")
			}
			want := MailRenewalHookAbsent
			if independent {
				want = MailRenewalHookIndependent
			} else if scenario == "legacy" {
				want = MailRenewalHookLegacy
			}
			if preview.Mode != want {
				t.Fatal("wrong native mode", preview.Mode)
			}
			if scenario == "owner-disabled" && preview.Timer != (mailrenewalkit.TimerState{Enablement: "disabled", Activity: "inactive"}) {
				t.Fatal("owner choice lost")
			}
			switch scenario {
			case "late-hook-created":
				put(hook, mailrenewalkit.LegacyHook(), 0755)
			case "late-parent-created":
				os.Mkdir(filepath.Join(hooks, "missing"), 0755)
			case "late-parent-symlink":
				os.Symlink(units, filepath.Join(hooks, "missing"))
			}
			late := scenario == "late-hook-created" || scenario == "late-parent-created" || scenario == "late-parent-symlink"
			if (preview.Revalidate() != nil) != late {
				t.Fatal("changed absence was accepted")
			}
			if scenario == "missing-ancestor" {
				if _, err := os.Lstat(filepath.Join(hooks, "missing")); !os.IsNotExist(err) {
					t.Fatal("review provisioned missing ancestor")
				}
			}
			if scenario == "fresh" {
				if _, err := os.Lstat(hook); !os.IsNotExist(err) {
					t.Fatal("review created hook")
				}
			}
		})
	}
}
