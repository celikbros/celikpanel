package main

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestSignedUpdateBINDPackageStatusIsExactAndDeadlineBound(t *testing.T) {
	wantArgs := []string{"-W", "-f", "${Status}", "--", "bind9"}
	installed, err := exactBINDPackageInstalledForSignedUpdateWithRunner(
		context.Background(), "/usr/bin/dpkg-query", "bind9",
		func(_ context.Context, name string, args ...string) ([]byte, error) {
			if name != "/usr/bin/dpkg-query" || !reflect.DeepEqual(args, wantArgs) {
				t.Fatalf("command = %s %#v", name, args)
			}
			return []byte("install ok installed"), nil
		},
	)
	if err != nil || !installed {
		t.Fatalf("installed=%v err=%v", installed, err)
	}
	for _, output := range []string{
		"install ok installed\n", "deinstall ok config-files",
		"prefix install ok installed",
	} {
		if _, err := exactBINDPackageInstalledForSignedUpdateWithRunner(
			context.Background(), "/usr/bin/dpkg-query", "bind9",
			func(context.Context, string, ...string) ([]byte, error) {
				return []byte(output), nil
			},
		); err == nil {
			t.Fatalf("non-canonical package status accepted: %q", output)
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = exactBINDPackageInstalledForSignedUpdateWithRunner(
		ctx, "/usr/bin/dpkg-query", "bind9",
		func(commandCtx context.Context, _ string, _ ...string) ([]byte, error) {
			<-commandCtx.Done()
			return nil, commandCtx.Err()
		},
	)
	if !errors.Is(err, context.DeadlineExceeded) ||
		time.Since(started) > time.Second {
		t.Fatalf("package proof ignored caller deadline: %v", err)
	}
}

func signedUpdateBINDInstallReceipt(t *testing.T) dnsEngineInstallOwnershipReceipt {
	t.Helper()
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifest(
		transport.DNSEngineSwitchModeSwitch,
		transport.DNSEnginePowerDNS, transport.DNSEngineBIND,
		1, 2, 0, transport.DNSTopologyStandalone, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := newDNSEngineInstallOwnership(
		transport.DNSEngineBIND, hostplatform.PackageManagerAPT,
		[]string{"bind9"}, []string{"bind9"}, manifest,
		transport.ServiceMutationBinding{
			MutationRequestID: strings.Repeat("1", 32),
			MutationOwnerID:   strings.Repeat("2", 32),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	return receipt
}

func signedUpdateBINDPreparationOps(
	t *testing.T,
) (bindSignedUpdatePreparationOps, *[]string) {
	t.Helper()
	events := []string{}
	receipt := signedUpdateBINDInstallReceipt(t)
	ops := bindSignedUpdatePreparationOps{
		checkIdle: func() error {
			events = append(events, "idle")
			return nil
		},
		detectProfile: func() (hostplatform.Profile, error) {
			events = append(events, "profile")
			return testUbuntuBINDProfile(), nil
		},
		readJournal: func() (dnsEngineSwitchJournal, bool, error) {
			events = append(events, "journal")
			return dnsEngineSwitchJournal{}, false, nil
		},
		readInstall: func() (dnsEngineInstallOwnershipReceipt, bool, error) {
			events = append(events, "install")
			return receipt, true, nil
		},
		readState: func() (dnsEngineStateReceipt, bool, error) {
			events = append(events, "state")
			return dnsEngineStateReceipt{}, false, nil
		},
		readOwnership: func() (dnsEngineStateReceipt, bool, error) {
			events = append(events, "ownership")
			return dnsEngineStateReceipt{}, false, nil
		},
		packageInstalled: func(context.Context, hostplatform.Profile, string) (bool, error) {
			events = append(events, "package")
			return true, nil
		},
		parentExists: func() (bool, error) {
			events = append(events, "parent")
			return true, nil
		},
		prepare: func(context.Context) error {
			events = append(events, "prepare")
			return nil
		},
		hardenExisting: func(context.Context) error {
			events = append(events, "harden-existing")
			return nil
		},
		verifyExisting: func(_ context.Context, _ dnsEngineStateReceipt) error {
			events = append(events, "verify-existing")
			return nil
		},
	}
	return ops, &events
}

func TestSignedUpdateBINDPreparationExactTransitionalOwnership(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"idle", "profile", "journal", "install", "state", "ownership",
		"package", "parent", "prepare",
	}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("events = %#v, want %#v", *events, want)
	}
}

func TestSignedUpdateBINDPreparationNoProvenanceIsReadOnlyNoop(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install")
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range *events {
		if event == "package" || event == "parent" || event == "prepare" {
			t.Fatalf("unmanaged host reached mutation path: %#v", *events)
		}
	}
}

func TestSignedUpdateBINDPreparationAcceptsExactManagedStateOrOwnership(t *testing.T) {
	base := legacyDurableDNSState(transport.DNSEngineBIND)
	for _, source := range []string{"state", "ownership"} {
		t.Run(source, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				*events = append(*events, "install")
				return dnsEngineInstallOwnershipReceipt{}, false, nil
			}
			if source == "state" {
				ops.readState = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "state")
					return base, true, nil
				}
			} else {
				ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "ownership")
					return base, true, nil
				}
			}
			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err != nil {
				t.Fatal(err)
			}
			wantTail := []string{"harden-existing", "verify-existing"}
			if len(*events) < len(wantTail) ||
				!reflect.DeepEqual((*events)[len(*events)-len(wantTail):], wantTail) {
				t.Fatalf("managed provenance did not prove existing tree: %#v", *events)
			}
			for _, event := range *events {
				if event == "prepare" {
					t.Fatalf("state/ownership path created a BIND child: %#v", *events)
				}
			}
		})
	}
}

func TestSignedUpdateBINDManagedReceiptRejectsMissingOrDriftedExistingTree(t *testing.T) {
	for _, failure := range []string{"missing-child", "current-drift", "config-drift"} {
		t.Run(failure, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				*events = append(*events, "install")
				return dnsEngineInstallOwnershipReceipt{}, false, nil
			}
			state := legacyDurableDNSState(transport.DNSEngineBIND)
			ops.readState = func() (dnsEngineStateReceipt, bool, error) {
				*events = append(*events, "state")
				return state, true, nil
			}
			ops.verifyExisting = func(
				context.Context, dnsEngineStateReceipt,
			) error {
				*events = append(*events, "verify-existing")
				return errors.New(failure)
			}
			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err == nil || !strings.Contains(err.Error(), failure) {
				t.Fatalf("error = %v, want %s", err, failure)
			}
			if !containsString(*events, "harden-existing") {
				t.Fatalf("monotonic parent hardening did not run: %#v", *events)
			}
			for _, event := range *events {
				if event == "prepare" {
					t.Fatalf("existing-tree failure created a child: %#v", *events)
				}
			}
		})
	}
}

func TestSignedUpdateBINDPreparationRejectsTransitionalAndManagedCoexistence(t *testing.T) {
	for _, source := range []string{"state", "ownership", "matching-state-and-ownership"} {
		t.Run(source, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			managed := legacyDurableDNSState(transport.DNSEngineBIND)
			if source == "state" || source == "matching-state-and-ownership" {
				ops.readState = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "state")
					return managed, true, nil
				}
			}
			if source == "ownership" || source == "matching-state-and-ownership" {
				ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
					*events = append(*events, "ownership")
					return managed, true, nil
				}
			}
			if err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			); err == nil || !strings.Contains(err.Error(), "coexists") {
				t.Fatalf("error = %v, want mixed-provenance rejection", err)
			}
			for _, event := range *events {
				if event == "package" || event == "parent" || event == "prepare" ||
					event == "harden-existing" || event == "verify-existing" {
					t.Fatalf("mixed provenance reached host mutation/proof: %#v", *events)
				}
			}
		})
	}
}

func TestSignedUpdateBINDPreparationFailsClosedBeforeMutation(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*bindSignedUpdatePreparationOps, *[]string)
	}{
		{name: "idle", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.checkIdle = func() error { return errors.New("lock lost") }
		}},
		{name: "profile", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.detectProfile = func() (hostplatform.Profile, error) {
				return hostplatform.Profile{}, errors.New("profile")
			}
		}},
		{name: "journal-present", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				return dnsEngineSwitchJournal{}, true, nil
			}
		}},
		{name: "journal-unreadable", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readJournal = func() (dnsEngineSwitchJournal, bool, error) {
				return dnsEngineSwitchJournal{}, false, errors.New("journal")
			}
		}},
		{name: "install-mismatch", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			original := ops.readInstall
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				receipt, exists, err := original()
				receipt.Packages = []string{"bind9", "bind9-utils"}
				return receipt, exists, err
			}
		}},
		{name: "install-corrupt", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			original := ops.readInstall
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				receipt, exists, err := original()
				receipt.MissingBefore = nil
				return receipt, exists, err
			}
		}},
		{name: "install-unreadable", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
				return dnsEngineInstallOwnershipReceipt{}, false, errors.New("install")
			}
		}},
		{name: "state-corrupt", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readState = func() (dnsEngineStateReceipt, bool, error) {
				return dnsEngineStateReceipt{Engine: transport.DNSEngineBIND}, true, nil
			}
		}},
		{name: "ownership-wrong-engine", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.readOwnership = func() (dnsEngineStateReceipt, bool, error) {
				return legacyDurableDNSState(transport.DNSEnginePowerDNS), true, nil
			}
		}},
		{name: "unsupported-profile", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.detectProfile = func() (hostplatform.Profile, error) {
				return hostplatform.Profile{
					DistroFamily:   hostplatform.DistroFamilyDebian,
					PackageManager: hostplatform.PackageManagerAPT,
					ServiceManager: "openrc",
					ID:             "operator-linux",
				}, nil
			}
		}},
		{name: "package-absent", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.packageInstalled = func(context.Context, hostplatform.Profile, string) (bool, error) {
				return false, nil
			}
		}},
		{name: "parent-absent", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.parentExists = func() (bool, error) { return false, nil }
		}},
		{name: "parent-unreadable", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.parentExists = func() (bool, error) { return false, errors.New("parent") }
		}},
		{name: "prepare", mutate: func(ops *bindSignedUpdatePreparationOps, _ *[]string) {
			ops.prepare = func(context.Context) error { return errors.New("prepare") }
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ops, events := signedUpdateBINDPreparationOps(t)
			test.mutate(&ops, events)
			err := prepareBINDGenerationRootForSignedUpdateWithOps(
				context.Background(), ops,
			)
			if err == nil {
				t.Fatal("unsafe signed-update state was accepted")
			}
			if test.name != "prepare" {
				for _, event := range *events {
					if event == "prepare" {
						t.Fatalf("mutation ran after %s failure: %#v", test.name, *events)
					}
				}
			}
		})
	}
}

func TestSignedUpdateBINDPreparationAcceptsDebian13Capabilities(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile")
		return testDebian13BINDProfile(), nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"idle", "profile", "journal", "install", "state", "ownership",
		"package", "parent", "prepare",
	}
	if !reflect.DeepEqual(*events, want) {
		t.Fatalf("Debian BIND preparation events = %#v, want %#v", *events, want)
	}
}

func TestSignedUpdateBINDPreparationUnmanagedDebian13IsReadOnlyNoop(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile")
		return testDebian13BINDProfile(), nil
	}
	ops.readInstall = func() (dnsEngineInstallOwnershipReceipt, bool, error) {
		*events = append(*events, "install")
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	for _, event := range *events {
		if event == "package" || event == "parent" || event == "prepare" ||
			event == "harden-existing" || event == "verify-existing" {
			t.Fatalf("unmanaged Debian reached host path: %#v", *events)
		}
	}
}

func TestSignedUpdateBINDPreparationNonAPTIsNoopAfterIdleProof(t *testing.T) {
	ops, events := signedUpdateBINDPreparationOps(t)
	ops.detectProfile = func() (hostplatform.Profile, error) {
		*events = append(*events, "profile")
		return hostplatform.Profile{PackageManager: hostplatform.PackageManagerPacman}, nil
	}
	if err := prepareBINDGenerationRootForSignedUpdateWithOps(
		context.Background(), ops,
	); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*events, []string{"idle", "profile"}) {
		t.Fatalf("non-APT hook events = %#v", *events)
	}
}
