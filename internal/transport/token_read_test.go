package transport

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadTokenBoundedRegularFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "token")
	for _, tc := range []struct {
		name string
		raw  string
		want string
	}{
		{"existing token whitespace", " \tfixture-token\r\n", "fixture-token"},
		{"empty", "", ""},
		{"whitespace only", "\n \t", ""},
		{"oversize content", strings.Repeat("s", maxTokenLineLen+1), ""},
		{"oversize padding", strings.Repeat(" ", maxTokenLineLen) + "fixture-token\n", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := os.WriteFile(path, []byte(tc.raw), 0o640); err != nil {
				t.Fatal(err)
			}
			got, err := ReadToken(path)
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatalf("invalid token accepted: got length=%d err=%v", len(got), err)
				}
			} else if err != nil || got != tc.want {
				t.Fatalf("existing token not preserved: got=%q err=%v", got, err)
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil || string(data) != tc.raw {
				t.Fatal("token read changed the file")
			}
		})
	}
	if _, err := ReadToken(root); err == nil {
		t.Fatal("directory accepted as token")
	}
	if _, err := ReadToken(filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing-file identity lost: %v", err)
	}
}

func TestLoadOrCreateTokenPreservesInvalidExistingFile(t *testing.T) {
	for _, raw := range []string{"", "\n \t", strings.Repeat("x", maxTokenLineLen+1)} {
		path := filepath.Join(t.TempDir(), "token")
		if err := os.WriteFile(path, []byte(raw), 0o640); err != nil {
			t.Fatal(err)
		}
		before, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if token, err := LoadOrCreateToken(path); err == nil || token != "" {
			t.Fatalf("invalid existing token was regenerated: length=%d err=%v", len(token), err)
		}
		after, err := os.Stat(path)
		if err != nil || !os.SameFile(before, after) || before.Mode() != after.Mode() || !before.ModTime().Equal(after.ModTime()) {
			t.Fatal("existing token inode or metadata changed")
		}
		data, err := os.ReadFile(path)
		if err != nil || string(data) != raw {
			t.Fatal("existing token bytes changed")
		}
	}
}
