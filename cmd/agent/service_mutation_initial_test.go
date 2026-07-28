//go:build linux

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
)

func TestInitialServiceMutationLedgerCheckAcceptsOnlyInitializerOutput(t *testing.T) {
	t.Run("accepts initializer output without changing it", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("initialize service mutation ledger: %v", err)
		}
		ledgerPath := filepath.Join(stateDir, "service-mutations.json")
		before, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if err := checkInitialServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("exact initial ledger rejected: %v", err)
		}
		after, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("read-only initial check changed ledger: before=%q after=%q", before, after)
		}
	})

	t.Run("rejects terminal history even though it is idle", func(t *testing.T) {
		manager, root := newMutationTestManager(t)
		beginMutationTestJob(t, manager)
		if _, err := manager.finish(&ServiceMutationFinishRequest{
			RequestID: testMutationRequestID,
			OwnerID:   testMutationOwnerID,
			Success:   true,
		}); err != nil {
			t.Fatal(err)
		}
		err := checkInitialServiceMutationLedger(
			filepath.Join(root, "state"),
			filepath.Join(root, "service-mutation.lock"),
		)
		if !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("terminal history err=%v want exact-initial rejection", err)
		}
	})

	t.Run("rejects noncanonical bytes", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(stateDir, "service-mutations.json")
		if err := os.WriteFile(ledgerPath, []byte("{\"version\":1,\"jobs\":{}}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(ledgerPath, 0o600); err != nil {
			t.Fatal(err)
		}
		err := checkInitialServiceMutationLedger(stateDir, filepath.Join(root, "service-mutation.lock"))
		if !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("noncanonical bytes err=%v want exact-initial rejection", err)
		}
	})

	t.Run("rejects unsafe state and ledger objects", func(t *testing.T) {
		tests := []struct {
			name  string
			setup func(t *testing.T, root, stateDir, ledgerPath string)
		}{
			{
				name: "state directory symlink",
				setup: func(t *testing.T, root, stateDir, _ string) {
					target := filepath.Join(root, "real-state")
					if err := os.Mkdir(target, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(filepath.Join(target, "service-mutations.json"), []byte(`{"version":1,"jobs":{}}`), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, stateDir); err != nil {
						t.Fatal(err)
					}
				},
			},
			{
				name: "group readable state directory",
				setup: func(t *testing.T, _, stateDir, ledgerPath string) {
					if err := os.Mkdir(stateDir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(ledgerPath, []byte(`{"version":1,"jobs":{}}`), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(stateDir, 0o750); err != nil {
						t.Fatal(err)
					}
				},
			},
			{
				name: "ledger symlink",
				setup: func(t *testing.T, root, stateDir, ledgerPath string) {
					if err := os.Mkdir(stateDir, 0o700); err != nil {
						t.Fatal(err)
					}
					target := filepath.Join(root, "target-ledger")
					if err := os.WriteFile(target, []byte(`{"version":1,"jobs":{}}`), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Symlink(target, ledgerPath); err != nil {
						t.Fatal(err)
					}
				},
			},
			{
				name: "ledger mode is not 0600",
				setup: func(t *testing.T, _, stateDir, ledgerPath string) {
					if err := os.Mkdir(stateDir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(ledgerPath, []byte(`{"version":1,"jobs":{}}`), 0o640); err != nil {
						t.Fatal(err)
					}
					if err := os.Chmod(ledgerPath, 0o640); err != nil {
						t.Fatal(err)
					}
				},
			},
			{
				name: "ledger has multiple links",
				setup: func(t *testing.T, root, stateDir, ledgerPath string) {
					if err := os.Mkdir(stateDir, 0o700); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(ledgerPath, []byte(`{"version":1,"jobs":{}}`), 0o600); err != nil {
						t.Fatal(err)
					}
					if err := os.Link(ledgerPath, filepath.Join(root, "second-link")); err != nil {
						t.Fatal(err)
					}
				},
			},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				root := mutationTestRoot(t)
				stateDir := filepath.Join(root, "state")
				ledgerPath := filepath.Join(stateDir, "service-mutations.json")
				test.setup(t, root, stateDir, ledgerPath)
				err := checkInitialServiceMutationLedger(
					stateDir,
					filepath.Join(root, "service-mutation.lock"),
				)
				if !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
					t.Fatalf("unsafe initial state err=%v want exact-initial rejection", err)
				}
			})
		}
	})

	t.Run("rejects owner mismatch", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("initialize service mutation ledger: %v", err)
		}
		previousUID := serviceMutationRequiredOwnerUID
		serviceMutationRequiredOwnerUID = uint32(os.Getuid() + 1)
		t.Cleanup(func() { serviceMutationRequiredOwnerUID = previousUID })
		if err := checkInitialServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("owner mismatch err=%v want exact-initial rejection", err)
		}
	})

	t.Run("rejects held common mutation lock", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("initialize service mutation ledger: %v", err)
		}
		lock, err := acquireServiceMutationFileLock(lockPath)
		if err != nil {
			t.Fatal(err)
		}
		defer lock.Close()
		t.Setenv(serviceMutationExternalLockFDEnvironment, strconv.Itoa(int(lock.file.Fd())))
		if err := checkInitialServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("held common lock err=%v want exact-initial rejection", err)
		}
		if err := checkInitialServiceMutationLedgerUnderExternalLock(stateDir, lockPath); err != nil {
			t.Fatalf("external-lock exact-initial proof rejected safe state: %v", err)
		}
	})

	t.Run("rejects missing ledger and relative paths without creating them", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		ledgerPath := filepath.Join(stateDir, "service-mutations.json")
		if err := checkInitialServiceMutationLedger(stateDir, filepath.Join(root, "service-mutation.lock")); !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("missing ledger err=%v want exact-initial rejection", err)
		}
		if _, err := os.Lstat(ledgerPath); !os.IsNotExist(err) {
			t.Fatalf("initial checker created missing ledger: %v", err)
		}
		if err := checkInitialServiceMutationLedger("relative-state", "relative-lock"); !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("relative paths err=%v want exact-initial rejection", err)
		}
	})
}

func initialLedgerTestLinkCount(t *testing.T, path string) uint64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("inspect link count for %s", path)
	}
	return uint64(stat.Nlink)
}

func TestInitialServiceMutationLedgerPublishIsCrashSafeNoReplace(t *testing.T) {
	t.Run("success has one link and retry does not replace", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("initialize service mutation ledger: %v", err)
		}
		ledgerPath := filepath.Join(stateDir, "service-mutations.json")
		beforeInfo, err := os.Stat(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		before, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if links := initialLedgerTestLinkCount(t, ledgerPath); links != 1 {
			t.Fatalf("initial ledger links=%d want 1", links)
		}
		if err := initializeServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errServiceMutationLedgerAlreadyInitialized) {
			t.Fatalf("retry err=%v want already initialized", err)
		}
		afterInfo, err := os.Stat(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(ledgerPath)
		if err != nil {
			t.Fatal(err)
		}
		if !os.SameFile(beforeInfo, afterInfo) || !bytes.Equal(before, after) {
			t.Fatalf("retry replaced or changed initial ledger: before=%q after=%q", before, after)
		}
		if links := initialLedgerTestLinkCount(t, ledgerPath); links != 1 {
			t.Fatalf("retried initial ledger links=%d want 1", links)
		}
	})

	for _, point := range []string{
		initialLedgerPublishBeforeRename,
		initialLedgerPublishAfterRename,
	} {
		t.Run("fault "+point, func(t *testing.T) {
			root := mutationTestRoot(t)
			stateDir := filepath.Join(root, "state")
			lockPath := filepath.Join(root, "service-mutation.lock")
			ledgerPath := filepath.Join(stateDir, "service-mutations.json")
			injected := errors.New("injected initial ledger publish fault")
			previousFault := initialLedgerPublishFault
			initialLedgerPublishFault = func(got string) error {
				if got == point {
					return injected
				}
				return nil
			}
			t.Cleanup(func() { initialLedgerPublishFault = previousFault })

			err := initializeServiceMutationLedger(stateDir, lockPath)
			if !errors.Is(err, injected) {
				t.Fatalf("fault %s err=%v want injected error", point, err)
			}
			staged, globErr := filepath.Glob(filepath.Join(stateDir, ".service-mutations-initial-*.json"))
			if globErr != nil {
				t.Fatal(globErr)
			}
			if len(staged) != 0 {
				t.Fatalf("fault %s leaked staged paths: %v", point, staged)
			}

			initialLedgerPublishFault = previousFault
			switch point {
			case initialLedgerPublishBeforeRename:
				if _, err := os.Lstat(ledgerPath); !os.IsNotExist(err) {
					t.Fatalf("pre-rename fault published final ledger: %v", err)
				}
				if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
					t.Fatalf("retry after pre-rename fault: %v", err)
				}
			case initialLedgerPublishAfterRename:
				if err := checkInitialServiceMutationLedger(stateDir, lockPath); err != nil {
					t.Fatalf("post-rename fault left invalid final ledger: %v", err)
				}
				beforeInfo, err := os.Stat(ledgerPath)
				if err != nil {
					t.Fatal(err)
				}
				if err := initializeServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errServiceMutationLedgerAlreadyInitialized) {
					t.Fatalf("retry after post-rename fault err=%v want already initialized", err)
				}
				afterInfo, err := os.Stat(ledgerPath)
				if err != nil {
					t.Fatal(err)
				}
				if !os.SameFile(beforeInfo, afterInfo) {
					t.Fatal("retry after post-rename fault replaced final ledger")
				}
			}
			if links := initialLedgerTestLinkCount(t, ledgerPath); links != 1 {
				t.Fatalf("fault %s final ledger links=%d want 1", point, links)
			}
		})
	}
}

func writeInitialLedgerTestStage(t *testing.T, stateDir, name string, raw []byte) string {
	t.Helper()
	stagePath := filepath.Join(stateDir, name)
	if err := os.WriteFile(stagePath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(stagePath, 0o600); err != nil {
		t.Fatal(err)
	}
	return stagePath
}

func initialLedgerTestEntryNames(t *testing.T, stateDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	return names
}

func TestInitialServiceMutationLedgerAbandonedStageContract(t *testing.T) {
	t.Run("canonical stage is read-only proof then cleaned during retry", func(t *testing.T) {
		root := mutationTestRoot(t)
		stateDir := filepath.Join(root, "state")
		lockPath := filepath.Join(root, "service-mutation.lock")
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		expected, err := canonicalInitialServiceMutationLedger()
		if err != nil {
			t.Fatal(err)
		}
		stagePath := writeInitialLedgerTestStage(
			t,
			stateDir,
			".service-mutations-initial-123456789.json",
			expected,
		)

		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); err != nil {
			t.Fatalf("strict canonical abandoned stage rejected: %v", err)
		}
		if got, err := os.ReadFile(stagePath); err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("read-only pre-ledger check changed stage: got=%q err=%v", got, err)
		}
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("retry from canonical abandoned stage: %v", err)
		}
		if _, err := os.Lstat(stagePath); !os.IsNotExist(err) {
			t.Fatalf("retry left canonical abandoned stage: %v", err)
		}
		ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
		if got, err := os.ReadFile(ledgerPath); err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("retry final ledger got=%q err=%v", got, err)
		}
		if links := initialLedgerTestLinkCount(t, ledgerPath); links != 1 {
			t.Fatalf("retry final ledger links=%d want 1", links)
		}
		if err := checkInitialServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("retry final state rejected: %v", err)
		}

		writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-987654321.json", expected)
		if err := checkInitialServiceMutationLedger(stateDir, lockPath); !errors.Is(err, errInitialServiceMutationLedgerInvalid) {
			t.Fatalf("post-initial checker accepted staged entry: %v", err)
		}
	})

	tests := []struct {
		name  string
		setup func(t *testing.T, root, stateDir string, expected []byte)
	}{
		{
			name: "noncanonical payload",
			setup: func(t *testing.T, _, stateDir string, _ []byte) {
				writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-10001.json", []byte(`{"version":2`))
			},
		},
		{
			name: "nondecimal internal name",
			setup: func(t *testing.T, _, stateDir string, expected []byte) {
				writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-abandoned.json", expected)
			},
		},
		{
			name: "wrong mode",
			setup: func(t *testing.T, _, stateDir string, expected []byte) {
				stagePath := writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-10002.json", expected)
				if err := os.Chmod(stagePath, 0o640); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "multiple links",
			setup: func(t *testing.T, root, stateDir string, expected []byte) {
				stagePath := writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-10003.json", expected)
				if err := os.Link(stagePath, filepath.Join(root, "initial-stage-second-link")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "symlink",
			setup: func(t *testing.T, root, stateDir string, expected []byte) {
				target := filepath.Join(root, "initial-stage-target")
				if err := os.WriteFile(target, expected, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, filepath.Join(stateDir, ".service-mutations-initial-10004.json")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "multiple canonical stages",
			setup: func(t *testing.T, _, stateDir string, expected []byte) {
				writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-10005.json", expected)
				writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-10006.json", expected)
			},
		},
		{
			name: "unexpected entry",
			setup: func(t *testing.T, _, stateDir string, expected []byte) {
				writeInitialLedgerTestStage(t, stateDir, "untrusted.json", expected)
			},
		},
	}
	if os.Geteuid() == 0 {
		tests = append(tests, struct {
			name  string
			setup func(t *testing.T, root, stateDir string, expected []byte)
		}{
			name: "wrong group",
			setup: func(t *testing.T, _, stateDir string, expected []byte) {
				stagePath := writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-10007.json", expected)
				if err := os.Chown(stagePath, os.Getuid(), os.Getgid()+1); err != nil {
					t.Fatal(err)
				}
			},
		})
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := mutationTestRoot(t)
			stateDir := filepath.Join(root, "state")
			lockPath := filepath.Join(root, "service-mutation.lock")
			if err := os.Mkdir(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			expected, err := canonicalInitialServiceMutationLedger()
			if err != nil {
				t.Fatal(err)
			}
			test.setup(t, root, stateDir, expected)
			before := initialLedgerTestEntryNames(t, stateDir)

			if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
				t.Fatalf("unsafe pre-ledger stage check err=%v want not idle", err)
			}
			if err := initializeServiceMutationLedger(stateDir, lockPath); err == nil {
				t.Fatal("initializer accepted unsafe abandoned stage")
			}
			if _, err := os.Lstat(filepath.Join(stateDir, serviceMutationLedgerFileName)); !os.IsNotExist(err) {
				t.Fatalf("initializer published from unsafe stage: %v", err)
			}
			after := initialLedgerTestEntryNames(t, stateDir)
			if len(before) != len(after) {
				t.Fatalf("unsafe stage entries changed: before=%v after=%v", before, after)
			}
			for i := range before {
				if before[i] != after[i] {
					t.Fatalf("unsafe stage entries changed: before=%v after=%v", before, after)
				}
			}
		})
	}
}

func TestInitialServiceMutationLedgerRecoversOnlyStrictCrashResidues(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership transition requires root")
	}
	previousUID := serviceMutationRequiredOwnerUID
	previousGID := serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID = 0
	serviceMutationRequiredOwnerGID = uint32(os.Getgid() + 1)
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID = previousUID
		serviceMutationRequiredOwnerGID = previousGID
	})

	expected, err := canonicalInitialServiceMutationLedger()
	if err != nil {
		t.Fatal(err)
	}
	prepareLockDir := func(t *testing.T, root string) string {
		t.Helper()
		lockDir := filepath.Join(root, "run")
		if err := os.Mkdir(lockDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(lockDir, 0, int(serviceMutationRequiredOwnerGID)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(lockDir, 0o700); err != nil {
			t.Fatal(err)
		}
		return filepath.Join(lockDir, "service-mutation.lock")
	}
	assertDirectoryGroup := func(t *testing.T, path string, wantGID uint32) {
		t.Helper()
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok {
			t.Fatalf("inspect ownership for %s", path)
		}
		if !info.IsDir() || info.Mode().Perm() != 0o700 || stat.Uid != 0 || stat.Gid != wantGID {
			t.Fatalf("directory metadata uid=%d gid=%d mode=%#o want uid=0 gid=%d mode=0700", stat.Uid, stat.Gid, info.Mode().Perm(), wantGID)
		}
	}

	t.Run("empty root-owned directory is read-only proof then repaired", func(t *testing.T) {
		root := t.TempDir()
		stateDir := filepath.Join(root, "state")
		lockPath := prepareLockDir(t, root)
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(stateDir, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}

		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); err != nil {
			t.Fatalf("read-only pre-ledger proof rejected empty mkdir residue: %v", err)
		}
		assertDirectoryGroup(t, stateDir, 0)
		if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
			t.Fatalf("initializer did not recover empty mkdir residue: %v", err)
		}
		assertDirectoryGroup(t, stateDir, serviceMutationRequiredOwnerGID)
		ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
		if got, err := os.ReadFile(ledgerPath); err != nil || !bytes.Equal(got, expected) {
			t.Fatalf("recovered ledger got=%q err=%v", got, err)
		}
		assertInitialLedgerTestOwnership(t, ledgerPath, 0, serviceMutationRequiredOwnerGID)
	})

	t.Run("root-owned directory with content fails closed", func(t *testing.T) {
		root := t.TempDir()
		stateDir := filepath.Join(root, "state")
		lockPath := prepareLockDir(t, root)
		if err := os.Mkdir(stateDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(stateDir, 0, 0); err != nil {
			t.Fatal(err)
		}
		stagePath := writeInitialLedgerTestStage(t, stateDir, ".service-mutations-initial-20001.json", nil)
		if err := os.Chown(stagePath, 0, 0); err != nil {
			t.Fatal(err)
		}
		if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); !errors.Is(err, errServiceMutationNotIdle) {
			t.Fatalf("root-owned nonempty state err=%v want not idle", err)
		}
		if err := initializeServiceMutationLedger(stateDir, lockPath); err == nil {
			t.Fatal("initializer repaired a nonempty root-owned state directory")
		}
		if got, err := os.ReadFile(stagePath); err != nil || len(got) != 0 {
			t.Fatalf("unsafe root-owned residue changed: got=%q err=%v", got, err)
		}
	})

	stageCases := []struct {
		name  string
		raw   []byte
		group uint32
	}{
		{name: "crash after create", raw: nil, group: 0},
		{name: "crash after chown", raw: nil, group: serviceMutationRequiredOwnerGID},
		{name: "crash during write", raw: append([]byte(nil), expected[:len(expected)/2]...), group: serviceMutationRequiredOwnerGID},
	}
	for index, test := range stageCases {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			stateDir := filepath.Join(root, "state")
			lockPath := prepareLockDir(t, root)
			if err := os.Mkdir(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chown(stateDir, 0, int(serviceMutationRequiredOwnerGID)); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(stateDir, 0o700); err != nil {
				t.Fatal(err)
			}
			stagePath := writeInitialLedgerTestStage(
				t,
				stateDir,
				fmt.Sprintf(".service-mutations-initial-30%d.json", index),
				test.raw,
			)
			if err := os.Chown(stagePath, 0, int(test.group)); err != nil {
				t.Fatal(err)
			}
			beforeInfo, err := os.Stat(stagePath)
			if err != nil {
				t.Fatal(err)
			}

			if err := checkPreLedgerServiceMutationIdle(stateDir, lockPath); err != nil {
				t.Fatalf("read-only pre-ledger proof rejected strict crash residue: %v", err)
			}
			afterInfo, err := os.Stat(stagePath)
			if err != nil {
				t.Fatal(err)
			}
			if !os.SameFile(beforeInfo, afterInfo) {
				t.Fatal("read-only pre-ledger proof replaced the crash residue")
			}
			if got, err := os.ReadFile(stagePath); err != nil || !bytes.Equal(got, test.raw) {
				t.Fatalf("read-only pre-ledger proof changed residue: got=%q err=%v", got, err)
			}

			if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
				t.Fatalf("initializer did not recover strict crash residue: %v", err)
			}
			if _, err := os.Lstat(stagePath); !os.IsNotExist(err) {
				t.Fatalf("initializer left recovered stage: %v", err)
			}
			ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
			if got, err := os.ReadFile(ledgerPath); err != nil || !bytes.Equal(got, expected) {
				t.Fatalf("recovered ledger got=%q err=%v", got, err)
			}
			assertInitialLedgerTestOwnership(t, ledgerPath, 0, serviceMutationRequiredOwnerGID)
		})
	}
}
func assertInitialLedgerTestOwnership(t *testing.T, path string, wantUID, wantGID uint32) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		t.Fatalf("inspect ownership for %s", path)
	}
	if stat.Uid != wantUID || stat.Gid != wantGID || info.Mode().Perm() != 0o600 || stat.Nlink != 1 {
		t.Fatalf(
			"ledger metadata uid=%d gid=%d mode=%#o links=%d want uid=%d gid=%d mode=0600 links=1",
			stat.Uid,
			stat.Gid,
			info.Mode().Perm(),
			stat.Nlink,
			wantUID,
			wantGID,
		)
	}
}

func TestInitialAndRewrittenServiceMutationLedgerUseRequiredOwnership(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("ownership transition requires root")
	}
	previousUID := serviceMutationRequiredOwnerUID
	previousGID := serviceMutationRequiredOwnerGID
	serviceMutationRequiredOwnerUID = 0
	serviceMutationRequiredOwnerGID = uint32(os.Getgid() + 1)
	t.Cleanup(func() {
		serviceMutationRequiredOwnerUID = previousUID
		serviceMutationRequiredOwnerGID = previousGID
	})

	root := t.TempDir()
	stateDir := filepath.Join(root, "state")
	lockDir := filepath.Join(root, "run")
	lockPath := filepath.Join(lockDir, "service-mutation.lock")
	for _, path := range []string{stateDir, lockDir} {
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chown(path, int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(lockPath, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chown(lockPath, int(serviceMutationRequiredOwnerUID), int(serviceMutationRequiredOwnerGID)); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockPath, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := initializeServiceMutationLedger(stateDir, lockPath); err != nil {
		t.Fatalf("initialize owned ledger: %v", err)
	}
	ledgerPath := filepath.Join(stateDir, serviceMutationLedgerFileName)
	assertInitialLedgerTestOwnership(
		t,
		ledgerPath,
		serviceMutationRequiredOwnerUID,
		serviceMutationRequiredOwnerGID,
	)

	manager, err := newServiceMutationManager(stateDir, lockPath)
	if err != nil {
		t.Fatal(err)
	}
	beginMutationTestJob(t, manager)
	if _, err := manager.finish(&ServiceMutationFinishRequest{
		RequestID: testMutationRequestID,
		OwnerID:   testMutationOwnerID,
		Success:   true,
	}); err != nil {
		t.Fatal(err)
	}
	assertInitialLedgerTestOwnership(
		t,
		ledgerPath,
		serviceMutationRequiredOwnerUID,
		serviceMutationRequiredOwnerGID,
	)
}
