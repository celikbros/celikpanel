//go:build linux

package recoverycheckpoint

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func fixture(t *testing.T) (config, *os.File) {
	t.Helper()
	if os.Geteuid() != 0 {
		t.Skip("native root filesystem metadata")
	}
	root := t.TempDir()
	tx := filepath.Join(root, "transaction")
	if err := os.Mkdir(tx, 0700); err != nil {
		t.Fatal(err)
	}
	lock, err := os.OpenFile(filepath.Join(tx, "transaction.lock"), os.O_RDWR|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { lock.Close() })
	if unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB) != nil {
		t.Fatal("lock")
	}
	raw := []byte("version=1\ntoken=" + strings.Repeat("9", 64) + "\noperation=rollback\nsnapshot=" + validRecord().Snapshot + "\n")
	if err := os.WriteFile(filepath.Join(tx, "active"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	r := runtimeProof{root: "/expected/runtime", digest: strings.Repeat("e", 64), revalidate: func() error { return nil }, close: func() error { return nil }}
	cfg := config{anchor: root, transaction: tx, output: filepath.Join(root, "observations"), fd: int(lock.Fd()), resolve: func() (runtimeProof, error) { return r, nil }, observe: func(runtimeProof) (worker, error) {
		return worker{44, "55", strings.Repeat("d", 32), "67139a2c-7b23-4387-95ad-45f9e2b831ea"}, nil
	}, now: func() time.Time { return time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC) }}
	return cfg, lock
}
func recordPath(cfg config) string {
	return filepath.Join(cfg.output, digest([]byte(strings.Repeat("9", 64)))+".json")
}
func TestPublishWithRealHeldFlockAtomicMetadataAndSequence(t *testing.T) {
	cfg, _ := fixture(t)
	if err := publish("restore_admitted", cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(recordPath(cfg))
	if err != nil {
		t.Fatal(err)
	}
	r, err := decode(raw)
	if err != nil || r.Sequence != 1 {
		t.Fatal(r, err)
	}
	if strings.Contains(string(raw), strings.Repeat("9", 64)) {
		t.Fatal("raw token leaked")
	}
	if err := publish("payload_restored", cfg); err != nil {
		t.Fatal(err)
	}
	raw, _ = os.ReadFile(recordPath(cfg))
	r, err = decode(raw)
	if err != nil || r.Sequence != 2 {
		t.Fatal(r, err)
	}
	st, err := os.Stat(recordPath(cfg))
	if err != nil || st.Mode().Perm() != 0600 {
		t.Fatal(st, err)
	}
	dir, _ := os.Stat(cfg.output)
	if dir.Mode().Perm() != 0700 {
		t.Fatal("directory mode")
	}
	entries, _ := os.ReadDir(cfg.output)
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".checkpoint-") {
			t.Fatal("temporary stage remained")
		}
	}
}
func TestUnlockedSharedAndWrongDescriptorRefuse(t *testing.T) {
	for _, kind := range []string{"unlocked", "shared", "wrong"} {
		t.Run(kind, func(t *testing.T) {
			cfg, lock := fixture(t)
			switch kind {
			case "unlocked":
				unix.Flock(int(lock.Fd()), unix.LOCK_UN)
			case "shared":
				unix.Flock(int(lock.Fd()), unix.LOCK_SH)
			case "wrong":
				other, err := os.CreateTemp(cfg.anchor, "other")
				if err != nil {
					t.Fatal(err)
				}
				defer other.Close()
				unix.Flock(int(other.Fd()), unix.LOCK_EX)
				cfg.fd = int(other.Fd())
			}
			if err := publish("restore_admitted", cfg); !errors.Is(err, ErrUnavailable) {
				t.Fatal("unsafe lock accepted", err)
			}
			if _, err := os.Lstat(cfg.output); !os.IsNotExist(err) {
				t.Fatal("output created without lock proof")
			}
		})
	}
}
func TestUnsafeMarkerAndOutputPathsRefuseWithoutNormalization(t *testing.T) {
	for _, kind := range []string{"marker-mode", "marker-symlink", "output-symlink", "output-mode"} {
		t.Run(kind, func(t *testing.T) {
			cfg, _ := fixture(t)
			switch kind {
			case "marker-mode":
				os.Chmod(filepath.Join(cfg.transaction, "active"), 0644)
			case "marker-symlink":
				os.Rename(filepath.Join(cfg.transaction, "active"), filepath.Join(cfg.transaction, "saved"))
				os.Symlink("saved", filepath.Join(cfg.transaction, "active"))
			case "output-symlink":
				os.Mkdir(filepath.Join(cfg.anchor, "outside"), 0700)
				os.Symlink(filepath.Join(cfg.anchor, "outside"), cfg.output)
			case "output-mode":
				os.Mkdir(cfg.output, 0755)
			}
			if err := publish("restore_admitted", cfg); !errors.Is(err, ErrUnavailable) {
				t.Fatal("unsafe metadata accepted", err)
			}
		})
	}
}
func TestChangedTupleWorkerOrRuntimeBeforeCommitPreservesPriorRecord(t *testing.T) {
	for _, kind := range []string{"tuple", "worker", "runtime", "directory"} {
		t.Run(kind, func(t *testing.T) {
			cfg, _ := fixture(t)
			if err := publish("restore_admitted", cfg); err != nil {
				t.Fatal(err)
			}
			before, _ := os.ReadFile(recordPath(cfg))
			cfg.beforeCommit = func() {
				switch kind {
				case "tuple":
					os.WriteFile(filepath.Join(cfg.transaction, "active"), []byte("changed"), 0600)
				case "worker":
					cfg.observe = func(runtimeProof) (worker, error) { return worker{}, ErrUnavailable }
				case "runtime": // resolver proof is retained, so inject the changed proof directly below
				case "directory":
					os.Rename(cfg.transaction, cfg.transaction+"-old")
					os.Mkdir(cfg.transaction, 0700)
				}
			}
			if kind == "worker" {
				calls := 0
				cfg.observe = func(runtimeProof) (worker, error) {
					calls++
					if calls > 1 {
						return worker{}, ErrUnavailable
					}
					return worker{44, "55", strings.Repeat("d", 32), "67139a2c-7b23-4387-95ad-45f9e2b831ea"}, nil
				}
			}
			if kind == "runtime" {
				calls := 0
				cfg.resolve = func() (runtimeProof, error) {
					return runtimeProof{root: "/expected/runtime", digest: strings.Repeat("e", 64), revalidate: func() error {
						calls++
						if calls > 1 {
							return ErrUnavailable
						}
						return nil
					}, close: func() error { return nil }}, nil
				}
			}
			if err := publish("payload_restored", cfg); !errors.Is(err, ErrUnavailable) {
				t.Fatal("changed proof accepted", err)
			}
			after, _ := os.ReadFile(recordPath(cfg))
			if string(before) != string(after) {
				t.Fatal("prior proof erased")
			}
		})
	}
}
func TestCanonicalCompletionPairAcceptedButAmbiguousMarkersRefuse(t *testing.T) {
	cfg, _ := fixture(t)
	active := filepath.Join(cfg.transaction, "active")
	raw, _ := os.ReadFile(active)
	os.Remove(active)
	for _, name := range []string{"completion.pending", "scheduler-restore.pending"} {
		os.WriteFile(filepath.Join(cfg.transaction, name), raw, 0600)
	}
	if err := publish("runtime_verified", cfg); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(cfg.transaction, "active"), raw, 0600)
	if err := publish("schedulers_restored", cfg); !errors.Is(err, ErrUnavailable) {
		t.Fatal("ambiguous phase accepted", err)
	}
}

func TestFixedNativeUnitPropertiesRejectUnknownOrReplacedProcess(t *testing.T) {
	raw := "Id=" + Unit + "\nLoadState=loaded\nActiveState=activating\nSubState=start\nMainPID=123\nControlGroup=/system.slice/" + Unit + "\nInvocationID=" + strings.Repeat("a", 32) + "\n"
	if _, err := parseUnitProperties([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{strings.Replace(raw, "Id="+Unit, "Id=celikpanel-agent.service", 1), strings.Replace(raw, "LoadState=loaded", "LoadState=not-found", 1), strings.Replace(raw, "ActiveState=activating", "ActiveState=inactive", 1), strings.Replace(raw, "InvocationID="+strings.Repeat("a", 32), "InvocationID=", 1), raw + "MainPID=999\n"} {
		if _, err := parseUnitProperties([]byte(bad)); err == nil {
			t.Fatal("unknown/replaced unit accepted")
		}
	}
}
