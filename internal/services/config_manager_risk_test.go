package services

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUpdateOrAddSettingTreatsReplacementMetacharactersLiterally(t *testing.T) {
	input := "; include_path = old\nnext = kept\n"
	got := updateOrAddSetting(input, "include_path", `${HOME}/lib`)
	want := "include_path = ${HOME}/lib\nnext = kept\n"
	if got != want {
		t.Fatalf("literal replacement or line boundary was lost:\n got %q\nwant %q", got, want)
	}
}

func TestAdditionalDirectiveMarkersAndManagedKeyCaseAreRejected(t *testing.T) {
	for _, directives := range []string{
		additionalPHPBegin,
		additionalPHPEnd,
		"MEMORY_LIMIT = 512M",
	} {
		if err := validateAdditionalPHPDirectives(directives); err == nil {
			t.Fatalf("unsafe additional directive was accepted: %q", directives)
		}
	}
	if _, err := replaceAdditionalPHPBlock(
		additionalPHPBegin+"\nx = 1\n"+additionalPHPEnd+"\n"+additionalPHPBegin+"\ny = 2\n"+additionalPHPEnd,
		"z = 3",
	); err == nil {
		t.Fatal("duplicate managed blocks were silently rewritten")
	}
}

func TestPHPModuleMutationNoOpPreservesPriorState(t *testing.T) {
	root := t.TempDir()
	oldRoot := phpEtcDir
	phpEtcDir = root
	t.Cleanup(func() { phpEtcDir = oldRoot })

	available := filepath.Join(root, "8.3", "mods-available")
	enabled := filepath.Join(root, "8.3", "fpm", "conf.d")
	if err := os.MkdirAll(available, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(enabled, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(available, "curl.ini"), []byte("extension=curl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(enabled, "20-curl.ini"), []byte("extension=curl\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	manager, err := NewPHPFPMManager()
	if err != nil {
		t.Fatal(err)
	}
	// These desired states already hold, so no system command or reload should
	// run. In particular, rollback must never invert a pre-existing state.
	if err := manager.EnableExtension("8.3", "curl"); err != nil {
		t.Fatalf("enable existing extension: %v", err)
	}
	if err := os.Remove(filepath.Join(enabled, "20-curl.ini")); err != nil {
		t.Fatal(err)
	}
	if err := manager.DisableExtension("8.3", "curl"); err != nil {
		t.Fatalf("disable already-disabled extension: %v", err)
	}
	extensions, err := manager.ListExtensions("8.3")
	if err != nil {
		t.Fatal(err)
	}
	if len(extensions) != 1 || extensions[0].Enabled || !strings.EqualFold(extensions[0].Name, "curl") {
		t.Fatalf("unexpected final module state: %+v", extensions)
	}
}
