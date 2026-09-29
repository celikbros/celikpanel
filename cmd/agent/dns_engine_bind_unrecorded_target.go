package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/transport"
)

// A BIND-target switch journal BEFORE target-verified whose DNS engine state
// receipt already names the BIND target, while BIND's generation pointer is
// absent, is left only by an Agent of an earlier release that crashed inside
// its own rollback: it wrote the target receipt, its target-verified
// checkpoint did not become durable, and its publisher removed the first
// pointer before the inverse that should have followed. The target cannot be
// verified without its pointer, and the pointer is never restored for a target
// that was not recorded as verified. The frozen source cannot be proved either,
// because the receipt names the target, so recovery used to dead-end.
//
// For a first install (V1, no source engine, no prior receipt, pointer or
// generation, no target unit active before) the prior state is "no DNS
// engine": there is no owner source to damage, and D-026 accepts the same-
// request inverse to that state. The Agent admits exactly that inverse; it
// still runs under the existing owner-aware configuration proof, the
// stopped-target proof before configuration restore and the no-authority
// (units and port-53 listener) proof after it. Every journal with a source is
// refused with the observation and the owner's next step.
//
// Önceki bir sürümün Agent'ı kendi geri alması sırasında çöktüğünde, hedef
// doğrulanmış olarak kaydedilmeden durum makbuzu BIND hedefini adlandıran ve
// nesil işaretçisi olmayan bir günlük kalabilir. İlk kurulumda (kaynak motor
// yok) önceki durum "DNS motoru yok"tur; Agent yalnız o ters işlemi, mevcut
// durdurulmuş-hedef ve dinleyici kanıtları altında kabul eder. Kaynağı olan
// her günlük reddedilir; gözlem ve sahibin sonraki adımı bildirilir.

type unrecordedBINDTargetOps struct {
	pointerPath           string
	current               func() (string, bool, error)
	anchorIncludesPointer func() (bool, error)
	logf                  func(string, ...any)
}

// bindUnrecordedTargetRefusal is the unknown result for a journal with a
// source: no automatic path and no owner command applies.
type bindUnrecordedTargetRefusal struct {
	requestID   string
	phase       string
	pointer     string
	source      transport.DNSEngine
	bootBlocked bool
}

func (refusal *bindUnrecordedTargetRefusal) Error() string {
	consequence := "Whether BIND can start after a reboot was not established."
	if refusal.bootBlocked {
		consequence = fmt.Sprintf("BIND's configuration includes %s, so named cannot start after a reboot or a BIND restart until the pointer is back; a named that is still running keeps answering until it stops.", filepath.ToSlash(filepath.Join(refusal.pointer, "zones.conf")))
	}
	noRollback := "this journal is not a first install without any previous DNS engine, state receipt, generation or running BIND, and the records no longer prove the prior state"
	if refusal.source != "" {
		noRollback = fmt.Sprintf("the previous %s engine exists and the records no longer prove its state", refusal.source)
	}
	return fmt.Sprintf(
		"DNS switch request %s stopped at phase %s, before its BIND target was recorded as verified, but the DNS engine records already name the BIND target and BIND's generation pointer %s is missing; an Agent of an earlier release can leave this state when it stopped inside its own rollback. The Agent does not restore the pointer of a target that was never recorded as verified, and it does not roll back automatically because %s. %s The server owner avoids rebooting or restarting BIND, keeps the journal, runs /usr/libexec/celikpanel/recovery dns-switch-status --quiesced --request-id %s, checks which DNS service is running (systemctl status named pdns), and contacts support with this request id; no owner recovery command applies to this state.",
		refusal.requestID, refusal.phase, refusal.pointer, noRollback, consequence, refusal.requestID,
	)
}

// ledgerMessage is the bounded, secret-free panel receipt text.
func (refusal *bindUnrecordedTargetRefusal) ledgerMessage() string {
	boot := " Whether BIND can start after a reboot is not known; avoid rebooting until this is resolved."
	if refusal.bootBlocked {
		boot = " BIND cannot start after a reboot or BIND restart until the pointer is back; avoid both."
	}
	return "The interrupted DNS switch stopped before its BIND target was recorded as verified, but the DNS records already name BIND and BIND's generation pointer is missing. No automatic repair or rollback applies because the prior DNS state cannot be proved." + boot + " New DNS changes are blocked. Run recovery dns-switch-status --quiesced and contact support with this request."
}

// admitUnrecordedBINDTargetWithoutPointer is consulted only after the frozen
// source state could not be proved. It returns true only for the first-install
// shape above; an error explains the refused shape with a source; false with no
// error keeps the generic uncertain verdict.
func admitUnrecordedBINDTargetWithoutPointer(
	journal dnsEngineSwitchJournal,
	state dnsEngineStateReceipt,
	stateExists bool,
	host func() (unrecordedBINDTargetOps, error),
) (bool, error) {
	if !dnsenginerecovery.UnrecordedBINDTargetJournal(journal) || !stateExists ||
		!exactDNSEngineStateForJournal(state, journal) || host == nil {
		return false, nil
	}
	ops, err := host()
	if err != nil {
		return false, err
	}
	if ops.pointerPath == "" || ops.current == nil || ops.anchorIncludesPointer == nil || ops.logf == nil {
		return false, errors.New("unrecorded BIND target check operations are incomplete")
	}
	if _, exists, err := ops.current(); err != nil || exists {
		// An unreadable pointer, or one that exists, is not this shape.
		return false, nil
	}
	bootBlocked, anchorErr := ops.anchorIncludesPointer()
	bootBlocked = bootBlocked && anchorErr == nil
	if dnsenginerecovery.FirstInstallBINDSwitchJournal(journal) {
		ops.logf(
			"DNS switch recovery for request %s: this first BIND install stopped at phase %s before its target was recorded as verified; the records already name the BIND target and its generation pointer %s is missing, which an Agent of an earlier release can leave behind. A first install has no previous DNS engine to damage, so the Agent rolls it back to no DNS engine under its stopped-target and port-53 listener proofs.",
			journal.MutationRequestID, journal.Phase, ops.pointerPath,
		)
		return true, nil
	}
	return false, &bindUnrecordedTargetRefusal{
		requestID: journal.MutationRequestID, phase: journal.Phase,
		pointer: ops.pointerPath, source: journal.SourceEngine,
		bootBlocked: bootBlocked,
	}
}

// hostUnrecordedBINDTargetOps reads the installed BIND layout's pointer and
// zone anchor. The caller holds the operation's host mutation lock.
func hostUnrecordedBINDTargetOps(ctx context.Context) (unrecordedBINDTargetOps, error) {
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return unrecordedBINDTargetOps{}, err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return unrecordedBINDTargetOps{}, err
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return unrecordedBINDTargetOps{}, err
	}
	return unrecordedBINDTargetOps{
		pointerPath:           filepath.Join(layout.GenerationRoot, "current"),
		current:               publisher.Current,
		anchorIncludesPointer: bindAnchorIncludesCurrentPointer(layout),
		logf:                  log.Printf,
	}, nil
}

// bindAnchorIncludesCurrentPointer reports whether BIND's zone anchor includes
// the managed current/zones.conf exactly.
func bindAnchorIncludesCurrentPointer(layout bindHostLayout) func() (bool, error) {
	includePath := filepath.ToSlash(filepath.Join(layout.GenerationRoot, "current", "zones.conf"))
	return func() (bool, error) {
		data, err := os.ReadFile(layout.AnchorConfig)
		if err != nil {
			return false, err
		}
		withInclude, err := managedBINDZoneInclude(string(data), includePath)
		return err == nil && withInclude == string(data), err
	}
}
