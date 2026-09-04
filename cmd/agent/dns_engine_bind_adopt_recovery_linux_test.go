//go:build linux

package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

const operatorBINDZoneAnchor = "// the operator's own zones\n" +
	"zone \"operator.test\" {\n\ttype master;\n" +
	"\tfile \"/var/lib/bind/operator.test.zone\";\n};\n"

func takeoverRecoveryLayout(t *testing.T) bindHostLayout {
	t.Helper()
	directory := t.TempDir()
	return bindHostLayout{
		GenerationRoot: filepath.Join(directory, "generations"),
		OptionsConfig:  filepath.Join(directory, "named.conf.options"),
		AnchorConfig:   filepath.Join(directory, "named.conf.local"),
	}
}

func writeOperatorBINDConfiguration(
	t *testing.T,
	layout bindHostLayout,
) map[string]string {
	t.Helper()
	digests := map[string]string{}
	for path, content := range map[string][]byte{
		layout.OptionsConfig: []byte(handConfiguredBINDOptions),
		layout.AnchorConfig:  []byte(operatorBINDZoneAnchor),
	} {
		if err := os.WriteFile(path, content, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, 0o644); err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(content)
		digests[filepath.Clean(path)] = hex.EncodeToString(sum[:])
	}
	return digests
}

func fileDigest(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// takeoverRecoveryJournal builds the durable record a running takeover writes
// before it touches a byte: the operator's own two files with their exact
// digests, the target unit preimage taken while the server was answering, and
// an absent engine state receipt.
//
// takeoverRecoveryJournal, çalışan bir devralmanın tek bayta dokunmadan önce
// yazdığı kalıcı kaydı kurar: operatörün kendi iki dosyası ve tam özetleri,
// sunucu yanıt verirken alınan hedef birim ön-görüntüsü ve bulunmayan bir motor
// durum makbuzu.
func takeoverRecoveryJournal(
	t *testing.T,
	layout bindHostLayout,
	phase string,
) (dnsEngineSwitchJournal, bindConfigMutation) {
	t.Helper()
	reader := func(
		path string, mode os.FileMode, allowAbsent bool,
	) (dnsFileSnapshot, error) {
		if allowAbsent {
			return dnsFileSnapshot{}, errors.New("unexpected absent BIND config")
		}
		return captureBINDConfigSnapshot(path, mode)
	}
	captured, err := prepareBINDConfigMutationWithSnapshotReader(
		layout, "", bindOptionsTakeover, reader,
	)
	if err != nil {
		t.Fatalf("the takeover refuses the operator's own configuration: %v", err)
	}
	state, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		t.Fatal(err)
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: phase,
		Mode:              transport.DNSEngineSwitchModeSwitch,
		TargetEngine:      transport.DNSEngineBIND,
		SourceEpoch:       0,
		TargetEpoch:       1,
		Topology:          transport.DNSTopologyStandalone,
		StateBefore:       state,
		ConfigBefore:      bindConfigMutationSnapshots(captured),
		TargetUnitsBefore: runningBINDUnitSnapshots(),
		SourceUnitsBefore: []dnsUnitSnapshot{},
	}
	return journal, captured
}

type recordedTakeoverRecoveryHost struct {
	steps []string
}

func (host *recordedTakeoverRecoveryHost) record(step string) func() error {
	return func() error {
		host.steps = append(host.steps, step)
		return nil
	}
}

// The three crash points a takeover has, driven against the operator's real
// files. Before the write there is nothing to put back and the recovery must
// finish cleanly anyway rather than hold the host; after the write and before
// the reload the file must come back byte for byte while the server is still
// answering the configuration it has in memory; after the reload the same
// restore runs and the server is told to re-read what it had. No step of any of
// them stops a unit, which is the whole of R-043.
//
// Bir devralmanın üç çökme noktası, operatörün gerçek dosyalarına karşı
// koşturulur. Yazmadan önce geri konacak bir şey yoktur ve kurtarma yine de
// sunucuyu tutmak yerine temizce bitmelidir; yazmadan sonra ve yeniden
// yüklemeden önce dosya birebir geri gelmelidir ve sunucu bellekteki
// yapılandırmayı yanıtlamayı sürdürür; yeniden yüklemeden sonra aynı geri
// yükleme koşar ve sunucuya sahip olduğunu yeniden okuması söylenir. Hiçbirinin
// hiçbir adımı bir birimi durdurmaz; R-043'ün tamamı budur.
func TestCrashedBINDTakeoverRecoveryRestoresEveryCrashPointWithoutStoppingAUnit(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("the BIND configuration restore writes managed files and requires root")
	}
	for _, crash := range []struct {
		name    string
		phase   string
		written bool
	}{
		{"before the configuration was written", dnsSwitchPhaseIntent, false},
		{"after the write, before the reload", dnsSwitchPhaseSourceStopped, true},
		{"after the reload, before the receipt", dnsSwitchPhaseTargetStarted, true},
	} {
		t.Run(crash.name, func(t *testing.T) {
			t.Setenv("CELIKPANEL_AGENT_STATE_DIR", t.TempDir())
			layout := takeoverRecoveryLayout(t)
			digests := writeOperatorBINDConfiguration(t, layout)
			journal, captured := takeoverRecoveryJournal(t, layout, crash.phase)

			manifest := takeoverAdoptionManifest(t)
			adoption, err := runningBINDAdoptionJournal(manifest, journal)
			if err != nil || !adoption {
				t.Fatalf(
					"the crashed takeover was not recognised: adoption=%v err=%v",
					adoption, err,
				)
			}

			ctx := context.Background()
			if crash.written {
				if err := captured.apply(ctx); err != nil {
					t.Fatal(err)
				}
				if fileDigest(t, layout.OptionsConfig) ==
					digests[filepath.Clean(layout.OptionsConfig)] {
					t.Fatal("the takeover did not change the options block")
				}
			}

			// The recovery reconstructs its mutation from the journal's own
			// bytes, exactly as the agent does at start. The owner policy knows
			// only the real vendor paths, so this fixture drives the same
			// mutation without it; the bytes, the digests and the takeover
			// authority are the production ones.
			//
			// Kurtarma, mutasyonunu günlüğün kendi baytlarından yeniden kurar;
			// tıpkı agent'ın başlangıçta yaptığı gibi. Sahiplik politikası
			// yalnız gerçek satıcı yollarını bilir, bu yüzden bu fikstür aynı
			// mutasyonu onsuz koşturur; baytlar, özetler ve devralma yetkisi
			// üretimdekilerdir.
			configs, err := bindConfigMutationFromJournal(layout, "", journal)
			if err != nil {
				t.Fatalf("the journal did not reconstruct its mutation: %v", err)
			}
			configs.ownerAware = false

			host := &recordedTakeoverRecoveryHost{}
			proveCurrent := func() error {
				for _, path := range configs.paths {
					data, readErr := os.ReadFile(path)
					if readErr != nil {
						return readErr
					}
					if bytes.Equal(data, configs.original[path]) ||
						bytes.Equal(data, configs.desired[path]) {
						continue
					}
					return fmt.Errorf(
						"BIND config changed outside the exact mutation preimage: %s",
						path,
					)
				}
				host.steps = append(host.steps, "prove-current")
				return nil
			}
			verifyConfigs := func() error {
				for path, digest := range digests {
					if fileDigest(t, path) != digest {
						return fmt.Errorf("%s did not come back exactly", path)
					}
				}
				host.steps = append(host.steps, "verify-configs")
				return nil
			}
			if err := recoverRunningBINDAdoptionJournalWithOps(
				bindAdoptionRecoveryOps{
					captureEvidence: func() (bindAdoptionRuntimeEvidence, error) {
						host.steps = append(host.steps, "capture-evidence")
						return bindAdoptionRuntimeEvidence{}, nil
					},
					proveCurrent: proveCurrent,
					rollback: func(bindAdoptionRuntimeEvidence) error {
						return rollbackRunningBINDAdoptionWithOps(
							bindAdoptionRollbackOps{
								restoreConfigs: func() error {
									host.steps = append(host.steps, "restore-configs")
									return configs.restore(ctx)
								},
								reload:        host.record("reload"),
								verifyConfigs: verifyConfigs,
								verifyRuntime: host.record("verify-runtime"),
								restoreState: func() error {
									host.steps = append(host.steps, "restore-state")
									return restoreDNSEngineStateSnapshot(journal.StateBefore)
								},
								restoreUnits: host.record("restore-units"),
							},
						)
					},
					restorePointer: host.record("restore-pointer"),
					verifyRestored: func(bindAdoptionRuntimeEvidence) error {
						if _, exists, stateErr := readDNSEngineState(); stateErr != nil ||
							exists {
							if stateErr == nil {
								stateErr = errors.New(
									"the recovery left an active DNS engine receipt",
								)
							}
							return stateErr
						}
						host.steps = append(host.steps, "verify-restored")
						return verifyConfigs()
					},
				},
			); err != nil {
				t.Fatalf("the crashed takeover was not recovered: %v", err)
			}

			for path, digest := range digests {
				if actual := fileDigest(t, path); actual != digest {
					data, _ := os.ReadFile(path)
					t.Fatalf(
						"%s came back as\n%s\nwant digest %s got %s",
						path, data, digest, actual,
					)
				}
			}
			want := []string{
				"capture-evidence", "prove-current", "restore-configs", "reload",
				"verify-configs", "verify-runtime", "restore-state",
				"restore-units", "restore-pointer", "verify-restored",
				"verify-configs",
			}
			if !reflect.DeepEqual(host.steps, want) {
				t.Fatalf("steps=%v want=%v", host.steps, want)
			}
			for _, step := range host.steps {
				for _, stopping := range []string{"stop", "disable", "mask", "restart"} {
					if strings.Contains(step, stopping) {
						t.Fatalf("the recovery reached %q", step)
					}
				}
			}
		})
	}
}
