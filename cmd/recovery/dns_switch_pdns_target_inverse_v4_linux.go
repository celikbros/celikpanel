//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

const ownerPDNSTargetInverseV4Command = "recover-dns-pdns-target-staged"

func parseOwnerPDNSTargetInverseV4Args(args []string) (request, lang string, ok bool) {
	lang = "en"
	if len(args) == 0 || args[0] != ownerPDNSTargetInverseV4Command {
		return "", lang, false
	}
	seenLang := false
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--request-id":
			if request != "" || i+1 >= len(args) || !servicemutationledger.ValidIdentity(args[i+1]) {
				return "", lang, false
			}
			i++
			request = args[i]
		case "--lang":
			if seenLang || i+1 >= len(args) || (args[i+1] != "en" && args[i+1] != "tr") {
				return "", lang, false
			}
			i++
			seenLang, lang = true, args[i]
		default:
			return "", lang, false
		}
	}
	return request, lang, request != ""
}

func dispatchOwnerPDNSTargetInverseV4(args []string, uid int, inverse func(context.Context, string) error, out, diagnostic io.Writer) int {
	request, lang, ok := parseOwnerPDNSTargetInverseV4Args(args)
	if !ok {
		fmt.Fprintln(diagnostic, translated(lang, "Usage: recovery recover-dns-pdns-target-staged --request-id <32 lowercase hex characters> [--lang en|tr]", "Kullanım: recovery recover-dns-pdns-target-staged --request-id <32 küçük harfli hex karakter> [--lang en|tr]"))
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, translated(lang, "Server owner action required: run this command as root or authorized sudo. No DNS operation was started.", "Sunucu sahibinin işlemi gerekiyor: komutu root veya yetkili sudo ile çalıştırın. DNS işlemi başlatılmadı."))
		return exitNotOwner
	}
	if inverse == nil {
		return exitUnavailable
	}
	if err := inverse(context.Background(), request); err != nil {
		fmt.Fprintln(diagnostic, translated(lang, "The staged PowerDNS target rollback could not be proved. Inspect recovery dns-switch-status --quiesced --request-id "+request+"; preserve the journal and ledger, resolve the reported condition and retry this same request. Reason: ", "Hazırlanmış PowerDNS hedefi geri alması kanıtlanamadı. recovery dns-switch-status --quiesced --request-id "+request+" çıktısını inceleyin; günlüğü ve işlem kaydını koruyun, bildirilen durumu giderip aynı isteği yeniden deneyin. Neden: ")+err.Error())
		return exitUnavailable
	}
	fmt.Fprintln(out, translated(lang, "The staged PowerDNS target rollback reached its terminal verdict for request "+request+". The restored BIND authority was verified during recovery.", "Hazırlanmış PowerDNS hedefi geri alması "+request+" işlemi için nihai karara ulaştı. Geri gelen BIND yetkili sunucusu kurtarma sırasında doğrulandı."))
	return exitOK
}

func runOwnerPDNSTargetInverseV4(args []string, uid int, out, diagnostic io.Writer) int {
	return dispatchOwnerPDNSTargetInverseV4(args, uid, func(ctx context.Context, request string) error {
		runtime, err := recoveryruntime.VerifiedLauncherRuntime()
		if err != nil {
			return fmt.Errorf("verify selected recovery launcher: %w", err)
		}
		defer runtime.Close()
		if err := runtime.VerifyExecutingBinary(); err != nil {
			return fmt.Errorf("verify selected recovery executable: %w", err)
		}
		return completeInstalledPDNSTargetInverseV4(ctx, request)
	}, out, diagnostic)
}

func completeInstalledPDNSTargetInverseV4(ctx context.Context, request string) error {
	if ctx == nil || !servicemutationledger.ValidIdentity(request) || os.Geteuid() != 0 {
		return errors.New("staged PowerDNS target inverse requires root and an exact request ID")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	group, err := localCelikPanelGroupID("/etc/group")
	if err != nil {
		return err
	}
	owner := servicemutationledger.FileOwner{UID: 0, GID: group}
	releasePath := "/var/lib/celikpanel-release-transaction/transaction.lock"
	release, err := hostmutationlock.AcquireExisting(releasePath, hostmutationlock.Owner{})
	if err != nil {
		return fmt.Errorf("hold release lock: %w", err)
	}
	locks := &dnsObservationLocks{release: release}
	if err := hostmutationlock.VerifyInherited(releasePath, int(release.Fd()), hostmutationlock.Owner{}); err != nil {
		locks.Close()
		return err
	}
	hostOwner := hostmutationlock.Owner{UID: owner.UID, GID: owner.GID}
	locks.host, err = hostmutationlock.AcquireOrCreateOwnerRecovery(hostOwner)
	if err != nil {
		locks.Close()
		return err
	}
	defer locks.Close()
	verifyLocks := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := hostmutationlock.VerifyInherited(releasePath, int(locks.release.Fd()), hostmutationlock.Owner{}); err != nil {
			return err
		}
		return hostmutationlock.VerifyHeldOwnerRecovery(locks.host, hostOwner)
	}
	root := hostingpath.ServiceMutationStateRoot()
	policy := installedDNSJournalPolicy(owner.GID)
	read := func(ctx context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) {
		if err := verifyLocks(ctx); err != nil {
			return dnsenginerecovery.SwitchEvidence{}, false, err
		}
		e, present, err := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
		if err != nil {
			return e, present, err
		}
		if !present {
			return e, false, journalAbsentPDNSInverseOutcome(ctx, root, owner, request)
		}
		if e.Journal.MutationRequestID != request {
			return dnsenginerecovery.SwitchEvidence{}, true, errors.New("another DNS request owns the retained journal")
		}
		return e, true, nil
	}
	guard := func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error) {
		e, present, err := read(ctx)
		if err != nil || !present || !reflect.DeepEqual(e.Journal, j) {
			return dnsenginerecovery.PDNSTargetStageUnknown, errors.Join(errors.New("exact PowerDNS journal changed before native effect"), err)
		}
		if err := dnsenginerecovery.ValidateStagedPDNSTargetInverseEvidence(e); err != nil {
			return dnsenginerecovery.PDNSTargetStageUnknown, err
		}
		if err := excludeInstalledDNSInverseWorker(ctx, e); err != nil {
			return dnsenginerecovery.PDNSTargetStageUnknown, err
		}
		return assessInstalledPDNSTargetStageNativeV4(ctx, policy, j)
	}
	complete := func(ctx context.Context) error {
		return dnsenginerecovery.CompleteStagedPDNSTargetInverseV4(ctx, dnsenginerecovery.PDNSTargetStageInverseOps{
			Read:          read,
			ExcludeWorker: excludeInstalledDNSInverseWorker,
			AssessNative: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error) {
				return guard(ctx, j)
			},
			RestoreNative: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
				pdnsGID, err := localServiceGroupID("/etc/group", "pdns")
				if err != nil {
					return err
				}
				assess := func(ctx context.Context) (dnsenginerecovery.PDNSTargetStageState, error) {
					return guard(ctx, j)
				}
				proof := func(ctx context.Context) error {
					state, err := assess(ctx)
					if err != nil || state == dnsenginerecovery.PDNSTargetStageUnknown {
						return errors.Join(errors.New("PowerDNS target changed before native effect"), err)
					}
					return nil
				}
				return restorePDNSTargetNativeV4(ctx, pdnsTargetNativeInverseOpsV4{
					Assess: assess,
					DisableTarget: func(ctx context.Context) error {
						return restorePDNSTargetUnitV4(ctx, j.TargetUnitsBefore[0], proof)
					},
					RestoreConfigs: func(ctx context.Context) error {
						return dnsenginerecovery.RestoreInstalledPDNSTargetConfigsV4(ctx, policy, j, pdnsGID, proof)
					},
					ReturnRenamed: func(ctx context.Context) error {
						return dnsenginerecovery.RestoreRenamedPDNSTargetV4(policy, j, func() error { return proof(ctx) })
					},
					RestoreSourceUnits: func(ctx context.Context) error {
						return restorePDNSTargetBINDUnitsV4(ctx, j.SourceUnitsBefore, proof)
					},
					RemoveCandidate: func(ctx context.Context) error {
						return dnsenginerecovery.RemoveExactStagedPDNSTargetV4(policy, j, func() error { return proof(ctx) })
					},
				})
			},
			WritePhase: func(ctx context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
				if state, err := guard(ctx, before); err != nil || state != dnsenginerecovery.PDNSTargetStageRestored {
					return errors.Join(errors.New("PowerDNS target native state changed before rollback checkpoint"), err)
				}
				return dnsenginerecovery.ReplaceRollbackJournalPhase(policy, owner, before, after)
			},
			PublishFailed: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
				if state, err := guard(ctx, j); err != nil || state != dnsenginerecovery.PDNSTargetStageRestored {
					return errors.Join(errors.New("PowerDNS target native state changed before terminal verdict"), err)
				}
				return dnsenginerecovery.PublishExactDNSRollbackVerdict(policy, owner, j, time.Now().UTC())
			},
			RemoveJournal: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
				if state, err := guard(ctx, j); err != nil || state != dnsenginerecovery.PDNSTargetStageRestored {
					return errors.Join(errors.New("PowerDNS target native state changed before journal retirement"), err)
				}
				return dnsenginerecovery.RemoveExactRollbackJournal(policy, owner, j)
			},
			VerifyTerminalWithoutJournal: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
				if err := verifyLocks(ctx); err != nil {
					return err
				}
				return dnsenginerecovery.VerifyExactDNSRollbackTerminalWithoutJournal(policy, owner, j)
			},
		})
	}
	return decideAndCompletePDNSTargetRollbackV4(ctx, pdnsTargetRollbackDecisionOpsV4{
		Read:          read,
		ExcludeWorker: excludeInstalledDNSInverseWorker,
		AssessNative: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.PDNSTargetStageState, error) {
			if err := verifyLocks(ctx); err != nil {
				return dnsenginerecovery.PDNSTargetStageUnknown, err
			}
			return assessInstalledPDNSTargetStageNativeV4(ctx, policy, j)
		},
		VerifyTarget: func(j dnsengineartifact.SwitchJournalV1) error {
			if err := verifyLocks(ctx); err != nil {
				return err
			}
			if err := dnsenginerecovery.VerifyStagedPDNSTargetV4(policy, j); err == nil {
				return nil
			}
			return dnsenginerecovery.VerifyRenamedPDNSTargetV4(policy, j)
		},
		WritePhase: func(ctx context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := verifyLocks(ctx); err != nil {
				return err
			}
			return dnsenginerecovery.ReplaceRollbackJournalPhase(policy, owner, before, after)
		},
		Continue: complete,
	})
}
