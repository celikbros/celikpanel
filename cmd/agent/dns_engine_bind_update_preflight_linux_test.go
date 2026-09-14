//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

func signedUpdateBINDPreflightOps(t *testing.T) bindSignedUpdatePreflightOps {
	t.Helper()
	prepare, _ := signedUpdateBINDPreparationOps(t)
	return bindSignedUpdatePreflightOps{
		checkIdle: prepare.checkIdle, detectProfile: prepare.detectProfile,
		readJournal: prepare.readJournal,
		readInstall: func() (dnsEngineInstallOwnershipReceipt, bool, error) {
			return dnsEngineInstallOwnershipReceipt{}, false, nil
		},
		readState: prepare.readState, readOwnership: prepare.readOwnership,
		packageInstalled: prepare.packageInstalled, parentExists: prepare.parentExists,
		verifyExisting: prepare.verifyExisting,
	}
}

func preflightFileSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		var stat unix.Stat_t
		if err := unix.Lstat(path, &stat); err != nil {
			return err
		}
		data := []byte(nil)
		if entry.Type().IsRegular() {
			var err error
			data, err = os.ReadFile(path)
			if err != nil {
				return err
			}
		}
		result[path] = fmt.Sprintf("%d/%d/%d/%d/%d/%d/%d/%d:%x", stat.Uid, stat.Gid, stat.Mode, stat.Ino, stat.Nlink, stat.Size, stat.Mtim.Sec, stat.Mtim.Nsec, data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestBINDUpdatePreflightAfterRealPublicationIsReadOnly(t *testing.T) {
	for _, scenario := range []string{"valid", "corrupt-tree", "foreign-owner", "equal-receipts-corrupt-tree"} {
		t.Run(scenario, func(t *testing.T) {
			ownership, state, generation := publishedBINDOwnershipFixture(t)
			verify := publicationTreeVerifier(t, &generation)
			ops := signedUpdateBINDPreflightOps(t)
			ops.readState = readDNSEngineState
			ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) { return readDNSEngineOwnership(transport.DNSEngineBIND) }
			switch scenario {
			case "corrupt-tree", "equal-receipts-corrupt-tree":
				generation.Config = append(generation.Config, []byte("// changed")...)
			case "foreign-owner":
				ownership.MutationOwnerID = strings.Repeat("a", 32)
				if err := writeDNSEngineOwnership(ownership); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "equal-receipts-corrupt-tree" {
				if err := writeDNSEngineOwnership(state); err != nil {
					t.Fatal(err)
				}
			}
			verified := false
			ops.verifyExisting = func(_ context.Context, actual dnsEngineStateReceipt) error {
				verified = true
				if actual != state {
					return errors.New("preflight selected old acquisition generation")
				}
				return verify(actual)
			}
			before := preflightFileSnapshot(t, serviceMutationStateDirectory())
			err := checkBINDSignedUpdateCompatibleWithOps(context.Background(), ops)
			if scenario == "valid" && err != nil {
				t.Fatal(err)
			}
			if scenario != "valid" && err == nil {
				t.Fatal("incompatible state passed")
			}
			if scenario != "foreign-owner" && !verified {
				t.Fatal("actual tree was not verified")
			}
			if after := preflightFileSnapshot(t, serviceMutationStateDirectory()); !reflect.DeepEqual(before, after) {
				t.Fatal("read-only preflight changed private state")
			}
		})
	}
}

func TestBINDUpdatePreflightDefersExistingTransitionRecoveryWithoutMutation(t *testing.T) {
	for _, shape := range []string{"journal", "install", "corrupt-journal", "corrupt-install"} {
		t.Run(shape, func(t *testing.T) {
			ops := signedUpdateBINDPreflightOps(t)
			if strings.Contains(shape, "journal") {
				journal := testBINDSwitchJournal(t)
				if shape == "corrupt-journal" {
					journal.Schema = "wrong"
				}
				ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) { return journal, true, nil }
			} else {
				receipt := signedUpdateBINDInstallReceipt(t)
				if shape == "corrupt-install" {
					receipt.Schema = "wrong"
				}
				ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) { return receipt, true, nil }
			}
			ops.verifyExisting = func(context.Context, dnsEngineStateReceipt) error {
				t.Fatal("settled generation verification reached during transition")
				return nil
			}
			err := checkBINDSignedUpdateCompatibleWithOps(context.Background(), ops)
			if strings.HasPrefix(shape, "corrupt") {
				if err == nil || errors.Is(err, errBINDSignedUpdatePreflightDeferred) {
					t.Fatalf("corrupt transition deferred: %v", err)
				}
			} else if !errors.Is(err, errBINDSignedUpdatePreflightDeferred) {
				t.Fatalf("supported transition path blocked: %v", err)
			}
		})
	}
}

func TestBINDUpdatePreflightNoManagedBINDNeedsNoPackageOrRoot(t *testing.T) {
	for _, manager := range []hostplatform.PackageManager{hostplatform.PackageManagerAPT, hostplatform.PackageManagerPacman} {
		ops := signedUpdateBINDPreflightOps(t)
		ops.detectProfile = func() (hostplatform.Profile, error) {
			p := testUbuntuBINDProfile()
			p.PackageManager = manager
			return p, nil
		}
		ops.packageInstalled = func(context.Context, hostplatform.Profile, string) (bool, error) {
			t.Fatal("unmanaged package checked")
			return false, nil
		}
		ops.parentExists = func() (bool, error) { t.Fatal("unmanaged root checked"); return false, nil }
		ops.verifyExisting = func(context.Context, dnsEngineStateReceipt) error { t.Fatal("unmanaged tree checked"); return nil }
		if err := checkBINDSignedUpdateCompatibleWithOps(context.Background(), ops); err != nil {
			t.Fatal(err)
		}
	}
}

func TestBINDUpdatePreflightRefusesBusyOrMissingInheritedLockBeforeDNSReads(t *testing.T) {
	ops := signedUpdateBINDPreflightOps(t)
	ops.checkIdle = func() error { return errors.New("active mutation") }
	ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
		t.Fatal("DNS read before idle proof")
		return dnsEngineSwitchJournal{}, false, nil
	}
	if err := checkBINDSignedUpdateCompatibleWithOps(context.Background(), ops); err == nil {
		t.Fatal("busy host accepted")
	}
	t.Setenv(serviceMutationExternalLockFDEnvironment, "")
	if err := checkBINDSignedUpdateCompatibleUnderExternalLock(context.Background(), t.TempDir(), filepath.Join(t.TempDir(), "lock"), false); err == nil || !strings.Contains(err.Error(), "inherited host lock") {
		t.Fatalf("missing inherited lock accepted: %v", err)
	}
}

func TestBINDUpdatePreflightRootMigrationCandidateReadOnly(t *testing.T) {
	for _, mode := range []uint32{aptBINDStockCacheParentMode, aptBINDCacheParentMode} {
		for _, override := range []string{"absent", "exact", "conflicting"} {
			t.Run(fmt.Sprintf("%o-%s", mode, override), func(t *testing.T) {
				root, fd := newAPTBindRootFixture(t)
				parent := filepath.Join(root, "var/cache/bind")
				child := filepath.Join(parent, "celikpanel")
				if err := os.Mkdir(child, 0o755); err != nil {
					t.Fatal(err)
				}
				mustChownMode(t, parent, 0, int(testBINDGID), mode)
				mustChownMode(t, child, 0, 0, bindManagedRootMode)
				generation, err := binddns.RenderManifest(child, binddns.Manifest{EngineEpoch: 1})
				if err != nil {
					t.Fatal(err)
				}
				generationDir := filepath.Join(child, "generations", generation.ID)
				if err := os.MkdirAll(filepath.Join(generationDir, "zones"), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(generationDir, "receipt.json"), generation.Receipt, 0o444); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(generationDir, "zones.conf"), generation.Config, 0o444); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(filepath.Join(generationDir, "zones"), 0o555); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(generationDir, 0o555); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("generations/"+generation.ID, filepath.Join(child, "current")); err != nil {
					t.Fatal(err)
				}
				publisher, err := binddns.NewOSPublisher(child)
				if err != nil {
					t.Fatal(err)
				}
				ops := aptBINDStatOverrideOps{
					owner: func() ([]byte, error) { return []byte(aptBINDExactPackageOwnerLine), nil },
					list: func() ([]byte, error) {
						if override == "exact" {
							return []byte(aptBINDExactStatOverrideLine), nil
						}
						if override == "conflicting" {
							return []byte("bind bind 0775 /var/cache/bind\n"), nil
						}
						return nil, testBINDExitError(1)
					},
					add: func() ([]byte, error) { t.Fatal("read-only preflight added statoverride"); return nil, nil },
				}
				before := preflightFileSnapshot(t, root)
				verified := false
				err = verifyAPTBindRootMigrationCandidateAt(fd, testBINDGID, ops, func() error {
					verified = true
					tree, err := publisher.LoadCurrent()
					if err != nil {
						return err
					}
					if tree.CurrentReceipt().Generation != generation.ID {
						return errors.New("loaded generation changed")
					}
					return nil
				})
				if override == "conflicting" {
					if err == nil || verified {
						t.Fatalf("conflicting override accepted: %v", err)
					}
				} else if err != nil || !verified {
					t.Fatalf("exact migration candidate refused: %v", err)
				}
				if after := preflightFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
					t.Fatal("root migration preflight modified files or permissions")
				}
			})
		}
	}
}

func TestBINDUpdatePreflightRootCandidateRejectsUnsafeOrChangingMetadata(t *testing.T) {
	for _, scenario := range []string{"missing-child", "group-writable-child", "symlink-child", "wrong-package", "swap-child", "changed-override"} {
		t.Run(scenario, func(t *testing.T) {
			root, fd := newAPTBindRootFixture(t)
			child := filepath.Join(root, "var/cache/bind/celikpanel")
			if scenario != "missing-child" {
				if err := os.Mkdir(child, 0o755); err != nil {
					t.Fatal(err)
				}
				mustChownMode(t, child, 0, 0, bindManagedRootMode)
			}
			if scenario == "group-writable-child" {
				mustChownMode(t, child, 0, 0, 0o775)
			}
			if scenario == "symlink-child" {
				if err := os.Rename(child, child+".other"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("celikpanel.other", child); err != nil {
					t.Fatal(err)
				}
			}
			calls := 0
			ops := aptBINDStatOverrideOps{
				owner: func() ([]byte, error) {
					if scenario == "wrong-package" {
						return []byte("owner: /var/cache/bind\n"), nil
					}
					return []byte(aptBINDExactPackageOwnerLine), nil
				},
				list: func() ([]byte, error) {
					calls++
					if scenario == "changed-override" && calls > 1 {
						return []byte(aptBINDExactStatOverrideLine), nil
					}
					return nil, testBINDExitError(1)
				},
				add: func() ([]byte, error) { t.Fatal("override changed"); return nil, nil },
			}
			before := preflightFileSnapshot(t, root)
			err := verifyAPTBindRootMigrationCandidateAt(fd, testBINDGID, ops, func() error {
				if scenario == "swap-child" {
					if err := os.Rename(child, child+".old"); err != nil {
						return err
					}
					if err := os.Mkdir(child, 0o755); err != nil {
						return err
					}
					mustChownMode(t, child, 0, 0, bindManagedRootMode)
				}
				return nil
			})
			if err == nil {
				t.Fatal("unsafe or changing candidate accepted")
			}
			if scenario != "swap-child" {
				if after := preflightFileSnapshot(t, root); !reflect.DeepEqual(before, after) {
					t.Fatal("preflight repaired unsafe state")
				}
			}
		})
	}
}
