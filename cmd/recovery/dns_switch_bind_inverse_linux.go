//go:build linux

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostmutationlock"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

const ownerBINDSwitchInverseCommand = "recover-dns-bind-switch"

var errBINDInverseTerminalLedgerObserved = errors.New("exact BIND rollback verdict is recorded but retired journal prevents current native source proof")

func parseOwnerBINDSwitchInverseArgs(args []string) (string, string, bool) {
	lang := "en"
	request := ""
	seenLang := false
	if len(args) == 0 || args[0] != ownerBINDSwitchInverseCommand {
		return "", lang, false
	}
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
			seenLang = true
			lang = args[i]
		default:
			return "", lang, false
		}
	}
	return request, lang, request != ""
}
func dispatchOwnerBINDSwitchInverse(args []string, uid int, inverse func(context.Context, string) error, out, diagnostic io.Writer) int {
	request, lang, valid := parseOwnerBINDSwitchInverseArgs(args)
	if !valid {
		fmt.Fprintln(diagnostic, translated(lang, "Usage: recovery recover-dns-bind-switch --request-id <32 lowercase hex characters> [--lang en|tr]", "Kullanım: recovery recover-dns-bind-switch --request-id <32 küçük harfli hex karakter> [--lang en|tr]"))
		return exitUsage
	}
	if uid != 0 {
		fmt.Fprintln(diagnostic, translated(lang, "Server owner action required: run this command as root or authorized sudo. No DNS operation was started.", "Sunucu sahibinin işlemi gerekiyor: bu komutu root veya yetkili sudo ile çalıştırın. DNS işlemi başlatılmadı."))
		return exitNotOwner
	}
	if inverse == nil {
		return exitUnavailable
	}
	if err := inverse(context.Background(), request); err != nil {
		fmt.Fprintln(diagnostic, translated(lang, "The accepted BIND switch rollback could not be verified. Inspect recovery dns-switch-status --quiesced --request-id "+request+"; resolve the reported evidence, worker, lock or native DNS condition and retry this same request. Preserve the journal and ledger. Reason: ", "Kabul edilmiş BIND geçişi geri alması doğrulanamadı. recovery dns-switch-status --quiesced --request-id "+request+" çıktısını inceleyin; kanıt, çalışan, kilit veya yerel DNS sorununu giderip aynı işlemi yeniden deneyin. Günlüğü ve işlem kaydını koruyun. Neden: ")+err.Error())
		return exitUnavailable
	}
	fmt.Fprintln(out, translated(lang, "The accepted BIND switch rollback reached its terminal verdict for request "+request+". Native PowerDNS was verified during recovery. Check current authoritative DNS health before another switch.", "Kabul edilmiş BIND geçişi geri alması "+request+" işlemi için nihai karara ulaştı. Yerel PowerDNS kurtarma sırasında doğrulandı. Başka geçişten önce güncel yetkili DNS sağlığını kontrol edin."))
	return exitOK
}
func runOwnerBINDSwitchInverse(args []string, uid int, out, diagnostic io.Writer) int {
	return dispatchOwnerBINDSwitchInverse(args, uid, func(ctx context.Context, request string) error {
		runtime, err := recoveryruntime.VerifiedLauncherRuntime()
		if err != nil {
			return fmt.Errorf("verify selected recovery launcher: %w", err)
		}
		defer runtime.Close()
		if err := runtime.VerifyExecutingBinary(); err != nil {
			return fmt.Errorf("verify selected recovery executable: %w", err)
		}
		return completeInstalledBINDSwitchInverse(ctx, request)
	}, out, diagnostic)
}
func classifyJournalAbsentBINDInverseLedger(ledger servicemutationledger.Ledger, request string) error {
	job := ledger.Jobs[request]
	if job == nil {
		return errors.New("exact BIND request absent from terminal ledger")
	}
	id := dnsengineartifact.SwitchIdentity{RequestID: job.RequestID, OwnerID: job.OwnerID, Target: transport.DNSEngine(job.Target), Qualifier: job.PackageName}
	if id.RequestID != request || id.Target != transport.DNSEngineBIND || !id.TerminalRolledBackJob(ledger) ||
		job.Phase != "interrupted" || job.ErrorCode != "dns_engine_switch_rolled_back_by_owner_recovery" ||
		job.ErrorMessage != "The interrupted DNS engine switch was rolled back to the verified previous state." {
		return errors.New("journal-absent BIND job lacks this exact owner recovery verdict")
	}
	return nil
}
func journalAbsentBINDInverseOutcome(ctx context.Context, root string, owner servicemutationledger.FileOwner, request string) error {
	ledger, err := readJournalAbsentDNSLedger(ctx, root, owner, request)
	if err != nil {
		return fmt.Errorf("journal-absent BIND result unknown: %w", err)
	}
	if err := classifyJournalAbsentBINDInverseLedger(ledger, request); err != nil {
		return err
	}
	return fmt.Errorf("%w: request %s; inspect native PowerDNS before treating current service as recovered", errBINDInverseTerminalLedgerObserved, request)
}
func completeInstalledBINDSwitchInverse(ctx context.Context, request string) error {
	return completeInstalledBINDInverse(ctx, request, false)
}
func completeInstalledBINDAdoptionInverse(ctx context.Context, request string) error {
	return completeInstalledBINDInverse(ctx, request, true)
}
func completeInstalledBINDInverse(ctx context.Context, request string, adoption bool) error {
	if ctx == nil || !servicemutationledger.ValidIdentity(request) {
		return errors.New("BIND inverse requires exact request ID and context")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	if os.Geteuid() != 0 {
		return errors.New("BIND inverse requires root or authorized sudo")
	}
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
	verifyLocks := func() error {
		if err := hostmutationlock.VerifyInherited(releasePath, int(locks.release.Fd()), hostmutationlock.Owner{}); err != nil {
			return err
		}
		return hostmutationlock.VerifyHeldOwnerRecovery(locks.host, hostOwner)
	}
	root := hostingpath.ServiceMutationStateRoot()
	policy := installedDNSJournalPolicy(owner.GID)
	guarded := func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return verifyLocks()
	}
	complete := dnsenginerecovery.CompleteInactiveBINDSwitchInverse
	var adoptionRuntime *bindAdoptionNativeSession
	if adoption {
		complete = dnsenginerecovery.CompleteRunningBINDAdoptionInverse
		adoptionRuntime = &bindAdoptionNativeSession{}
	}
	terminalProof := func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
		if !adoption {
			return nil
		}
		state, err := adoptionRuntime.assess(ctx, policy, j)
		if err != nil || state != dnsenginerecovery.BINDSwitchNativeRestored {
			return errors.Join(errors.New("owner BIND changed before terminal recovery publication"), err)
		}
		return nil
	}
	return complete(ctx, dnsenginerecovery.BINDSwitchInverseOps{
		Read: func(ctx context.Context) (dnsenginerecovery.SwitchEvidence, bool, error) {
			if err := guarded(ctx); err != nil {
				return dnsenginerecovery.SwitchEvidence{}, false, err
			}
			evidence, present, err := dnsenginerecovery.ReadSwitchEvidence(root, owner, policy, time.Now().UTC())
			if err != nil {
				return evidence, present, err
			}
			if !present {
				if adoption {
					_, err := readJournalAbsentDNSLedger(ctx, root, owner, request)
					return evidence, false, errors.Join(errors.New("BIND adoption journal is retired or absent; current native health is unknown, no recovery was attempted"), err)
				}
				return evidence, false, journalAbsentBINDInverseOutcome(ctx, root, owner, request)
			}
			if evidence.Journal.MutationRequestID != request {
				return dnsenginerecovery.SwitchEvidence{}, true, errors.New("another DNS operation owns retained journal")
			}
			return evidence, true, nil
		},
		ExcludeWorker: excludeInstalledDNSInverseWorker,
		AssessNative: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) (dnsenginerecovery.BINDSwitchNativeState, error) {
			if err := guarded(ctx); err != nil {
				return dnsenginerecovery.BINDSwitchNativeUnknown, err
			}
			if adoption {
				return adoptionRuntime.assess(ctx, policy, j)
			}
			return assessInstalledBINDSwitchNative(ctx, policy, j)
		},
		RestoreNative: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
			if err := guarded(ctx); err != nil {
				return err
			}
			if adoption {
				return adoptionRuntime.restore(ctx, policy, owner, j)
			}
			return restoreInstalledBINDSwitchNative(ctx, policy, owner, j)
		},
		WritePhase: func(ctx context.Context, before, after dnsengineartifact.SwitchJournalV1) error {
			if err := guarded(ctx); err != nil {
				return err
			}
			if err := terminalProof(ctx, before); err != nil {
				return err
			}
			return dnsenginerecovery.ReplaceRollbackJournalPhase(policy, owner, before, after)
		},
		PublishFailed: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
			if err := guarded(ctx); err != nil {
				return err
			}
			if err := terminalProof(ctx, j); err != nil {
				return err
			}
			return dnsenginerecovery.PublishExactDNSRollbackVerdict(policy, owner, j, time.Now().UTC())
		},
		RemoveJournal: func(ctx context.Context, j dnsengineartifact.SwitchJournalV1) error {
			if err := guarded(ctx); err != nil {
				return err
			}
			if err := terminalProof(ctx, j); err != nil {
				return err
			}
			return dnsenginerecovery.RemoveExactRollbackJournal(policy, owner, j)
		},
	})
}
