//go:build linux

package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alicelik/celikpanel/internal/bindrndckey"
	"github.com/alicelik/celikpanel/internal/bindroot"
	"github.com/alicelik/celikpanel/internal/dnsengineartifact"
	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

// This file holds the end-state rule of recover-dns-bind-switch for a V2
// PowerDNS-to-BIND journal that froze both BIND units absent (this operation
// created BIND): the target ends under the package guard's persistent mask,
// the staged generation this operation created is removed when it is exactly
// what the journal staged, and everything else that intentionally remains is
// recorded for the command's final output. A journal that froze an existing
// BIND keeps restoring its exact preimage and removes nothing.

// bindTargetEndState names the final BIND unit state the inverse proved.
type bindTargetEndState string

const (
	bindTargetGuardMask bindTargetEndState = "guard-mask"
	bindTargetAbsent    bindTargetEndState = "absent"
	bindTargetPreimage  bindTargetEndState = "preimage"
	// bindTargetLegacyDisabled is an unmasked, disabled target at a
	// rolled-back checkpoint an earlier recovery binary wrote.
	bindTargetLegacyDisabled bindTargetEndState = "legacy-disabled"
)

// Why a staged generation was not removed.
const (
	bindGenerationChanged = "changed"
	bindGenerationLegacy  = "legacy"
)

// bindRollbackRecord collects what one owner inverse run restored, removed
// and intentionally left. It is carried by the command's context and only
// read by the command's output; nothing here is persisted.
type bindRollbackRecord struct {
	mu                 sync.Mutex
	createdBIND        bool
	target             bindTargetEndState
	root               string
	removedGeneration  string
	retainedGeneration string
	retainedKind       string
	retainedDetail     string
	workingDirectory   string
	runtimeFiles       []string
	runtimeUnknown     bool
	rndcKey            bindRNDCKeyCLIOutcome
}

type bindRollbackRecordKey struct{}

func withBINDRollbackRecord(ctx context.Context) (context.Context, *bindRollbackRecord) {
	record := &bindRollbackRecord{}
	return context.WithValue(ctx, bindRollbackRecordKey{}, record), record
}

func bindRollbackRecordFrom(ctx context.Context) *bindRollbackRecord {
	if ctx == nil {
		return nil
	}
	record, _ := ctx.Value(bindRollbackRecordKey{}).(*bindRollbackRecord)
	return record
}

func (r *bindRollbackRecord) observeTarget(created bool, target bindTargetEndState) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.createdBIND, r.target = created, target
}

func (r *bindRollbackRecord) observeRemovedGeneration(root, generation string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.root, r.removedGeneration = root, generation
}

func (r *bindRollbackRecord) observeRetainedGeneration(root, generation, kind, detail string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.root, r.retainedGeneration, r.retainedKind, r.retainedDetail = root, generation, kind, detail
}

func (r *bindRollbackRecord) observeRNDCKey(outcome bindRNDCKeyCLIOutcome) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rndcKey = outcome
}

func (r *bindRollbackRecord) observeRuntimeFiles(directory string, files []string, unknown bool) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workingDirectory, r.runtimeFiles, r.runtimeUnknown = directory, append([]string(nil), files...), unknown
}

// bindRollbackSummaryText is the final-output account of a completed
// recover-dns-bind-switch run: what was restored, what was removed and what
// intentionally remains and why. It returns "" when no end state was proved.
func bindRollbackSummaryText(lang string, r *bindRollbackRecord) string {
	if r == nil {
		return ""
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.target == "" {
		return ""
	}
	var lines []string
	lines = append(lines, translated(lang,
		"Restored: the PowerDNS service, its DNS state receipt, the BIND configuration files this switch changed and the managed BIND pointer.",
		"Geri yüklenenler: PowerDNS hizmeti, DNS durum kaydı, bu geçişin değiştirdiği BIND yapılandırma dosyaları ve yönetilen BIND işaretçisi."))
	switch r.target {
	case bindTargetGuardMask:
		lines = append(lines, translated(lang,
			"BIND units: named.service and bind9.service are under the package guard's persistent mask, inactive and not enabled, so BIND cannot start by accident. A later switch lifts this mask itself.",
			"BIND birimleri: named.service ve bind9.service paket korumasının kalıcı maskesi altında; çalışmıyor ve açılışta başlamıyor, yani BIND kendiliğinden başlayamaz. Sonraki bir geçiş bu maskeyi kendisi kaldırır."))
	case bindTargetAbsent:
		lines = append(lines, translated(lang,
			"BIND units: named.service and bind9.service are absent, as before the switch.",
			"BIND birimleri: named.service ve bind9.service geçişten önceki gibi yok."))
	case bindTargetPreimage:
		lines = append(lines, translated(lang,
			"BIND units: named.service and bind9.service are back in the state recorded before the switch.",
			"BIND birimleri: named.service ve bind9.service geçişten önce kaydedilen duruma döndü."))
	case bindTargetLegacyDisabled:
		lines = append(lines, translated(lang,
			"BIND units: named.service is installed, inactive and disabled but not under the package guard's mask; an earlier recovery run left it this way.",
			"BIND birimleri: named.service kurulu, çalışmıyor ve açılışta başlamıyor, ancak paket korumasının maskesi altında değil; daha önceki bir kurtarma çalışması böyle bıraktı."))
	}
	if r.removedGeneration != "" {
		lines = append(lines, translated(lang,
			"Removed: the staged BIND generation "+r.removedGeneration+" that this switch created.",
			"Silinenler: bu geçişin hazırladığı "+r.removedGeneration+" BIND sürümü."))
	}
	switch {
	case r.retainedGeneration != "" && r.retainedKind == bindGenerationLegacy:
		lines = append(lines, translated(lang,
			"Not removed: the BIND generation "+r.retainedGeneration+" under "+r.root+"/generations. An earlier recovery run had already reached the rolled-back checkpoint and left it; it is no longer selected, so you may remove it after checking it.",
			"Silinmedi: "+r.root+"/generations altındaki "+r.retainedGeneration+" BIND sürümü. Daha önceki bir kurtarma çalışması geri alma noktasına ulaşmış ve onu bırakmıştı; artık kullanılmıyor, kontrol ettikten sonra silebilirsiniz."))
	case r.retainedGeneration != "":
		lines = append(lines, translated(lang,
			"Not removed: the BIND generation "+r.retainedGeneration+" under "+r.root+"/generations, because it is not exactly what this switch staged ("+r.retainedDetail+"). Check it before removing it yourself.",
			"Silinmedi: "+r.root+"/generations altındaki "+r.retainedGeneration+" BIND sürümü, çünkü içeriği bu geçişin hazırladığıyla birebir aynı değil. Kendiniz silmeden önce kontrol edin."))
	}
	switch {
	case r.createdBIND && r.rndcKey.Kind == bindRNDCKeyRemoved:
		lines = append(lines, translated(lang,
			"Intentionally kept as rollback standby: the installed bind9 packages and the BIND install-ownership record. This command does not remove packages.",
			"Yedek olarak bilerek bırakılanlar: kurulu bind9 paketleri ve BIND kurulum sahipliği kaydı. Bu komut paket kaldırmaz."))
	case r.createdBIND:
		lines = append(lines, translated(lang,
			"Intentionally kept as rollback standby: the installed bind9 packages, the rndc key and the BIND install-ownership record. This command does not remove packages.",
			"Yedek olarak bilerek bırakılanlar: kurulu bind9 paketleri, rndc anahtarı ve BIND kurulum sahipliği kaydı. Bu komut paket kaldırmaz."))
	}
	switch r.rndcKey.Kind {
	case bindRNDCKeyRemoved:
		lines = append(lines, translated(lang,
			"Removed: the rndc key "+r.rndcKey.Path+" that this switch created; it was unchanged.",
			"Silinenler: bu geçişin oluşturduğu "+r.rndcKey.Path+" rndc anahtarı; değişmemişti."))
	case bindRNDCKeyKeptChanged:
		lines = append(lines, translated(lang,
			"Not removed: the rndc key "+r.rndcKey.Path+". This switch created it, but it changed since then, so it is treated as yours.",
			"Silinmedi: "+r.rndcKey.Path+" rndc anahtarı. Bu geçiş onu oluşturdu, ancak o zamandan beri değişti; bu yüzden sizin anahtarınız sayılır."))
	case bindRNDCKeyKeptUnknown:
		lines = append(lines, translated(lang,
			"Not removed: the rndc key "+r.rndcKey.Path+", because its removal could not be completed safely: "+r.rndcKey.Detail,
			"Silinmedi: "+r.rndcKey.Path+" rndc anahtarı, çünkü güvenle silinemedi: "+r.rndcKey.Detail))
	}
	if len(r.runtimeFiles) > 0 {
		lines = append(lines, translated(lang,
			"Left in BIND's working directory "+r.workingDirectory+", which is outside the managed BIND root, so they were not removed: "+strings.Join(r.runtimeFiles, ", ")+".",
			"BIND'in çalışma dizini "+r.workingDirectory+" içinde bırakılanlar; bu dizin yönetilen BIND kökünün dışında olduğu için silinmedi: "+strings.Join(r.runtimeFiles, ", ")+"."))
	} else if r.runtimeUnknown {
		lines = append(lines, translated(lang,
			"BIND's working directory "+r.workingDirectory+" could not be listed; files BIND wrote there were not checked or removed.",
			"BIND'in çalışma dizini "+r.workingDirectory+" listelenemedi; BIND'in oraya yazdığı dosyalar kontrol edilmedi ve silinmedi."))
	}
	return strings.Join(lines, "\n")
}

// bindInverseTargetBothAbsent reports both BIND units absent (not-found).
func bindInverseTargetBothAbsent(units []dnsenginerecovery.NativeUnitObservation) bool {
	if len(units) < 2 || units[0].Name != "named.service" || units[1].Name != "bind9.service" {
		return false
	}
	for _, unit := range units[:2] {
		if unit.LoadState != "not-found" || unit.UnitFileState != "" || unit.ActiveState != "inactive" {
			return false
		}
	}
	return true
}

// sealTarget applies the package guard's own persistent mask to both BIND
// names, as bindPackageInstallGuard.ensurePersistentMasked does: the mask
// parent is proved before each systemctl call, the persistent mask is created
// before a possible runtime mask is removed, and the result must read
// masked/masked/inactive. The later vendor proof checks each link is a
// root-owned symlink to /dev/null. The caller has already stopped and
// disabled the target.
func (h bindSwitchNativeHost) sealTarget(ctx context.Context) error {
	for _, unit := range []string{"bind9.service", "named.service"} {
		for _, args := range [][]string{{"mask", unit}, {"unmask", "--runtime", unit}} {
			if err := h.maskParent(); err != nil {
				return fmt.Errorf("verify BIND mask parent before systemctl %s: %w", strings.Join(args, " "), err)
			}
			if out, err := h.runSystemd(ctx, "/usr/bin/systemctl", args...); err != nil {
				return fmt.Errorf("systemctl %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
			}
		}
	}
	units, err := h.units(ctx)
	if err != nil {
		return err
	}
	for _, unit := range units[:2] {
		if unit.LoadState != "masked" || unit.UnitFileState != "masked" || unit.ActiveState != "inactive" {
			return fmt.Errorf("%s did not reach the guard's persistent mask: load=%s active=%s unit-file=%s", unit.Name, unit.LoadState, unit.ActiveState, unit.UnitFileState)
		}
	}
	return ctx.Err()
}

// assessBINDSwitchResidue finishes the Restored classification of
// assessBINDSwitchNative. For a created-BIND journal at rolling-back, an
// exact staged generation still present means the inverse is incomplete
// (NeedsRestore), so an interrupted run resumes its removal. At the
// rolled-back checkpoint such a tree is left and recorded. A tree that is not
// exactly what the journal staged never blocks the rollback. The final unit
// state and BIND's runtime files are recorded for the command's output.
func assessBINDSwitchResidue(
	ctx context.Context, h bindSwitchNativeHost, j dnsengineartifact.SwitchJournalV1,
	units []dnsenginerecovery.NativeUnitObservation, layout bindroot.Layout, gid uint32,
) (dnsenginerecovery.BINDSwitchNativeState, error) {
	record := bindRollbackRecordFrom(ctx)
	if !dnsenginerecovery.BINDSwitchNeverStartedTargetJournal(j) {
		record.observeTarget(false, bindTargetPreimage)
		return dnsenginerecovery.BINDSwitchNativeRestored, nil
	}
	residue, detail, err := h.generation(ctx, j, layout, gid)
	if err != nil {
		return dnsenginerecovery.BINDSwitchNativeUnknown, fmt.Errorf("classify staged BIND generation: %w", err)
	}
	switch residue {
	case dnsenginerecovery.BINDGenerationStaged:
		if j.Phase == dnsengineartifact.SwitchPhaseRollingBack {
			return dnsenginerecovery.BINDSwitchNativeNeedsRestore, nil
		}
		record.observeRetainedGeneration(string(layout), j.TargetGeneration, bindGenerationLegacy, "")
	case dnsenginerecovery.BINDGenerationRetained:
		record.observeRetainedGeneration(string(layout), j.TargetGeneration, bindGenerationChanged, detail)
	}
	end := bindTargetLegacyDisabled
	switch {
	case bindInverseGuardSealedTarget(units, j):
		end = bindTargetGuardMask
	case bindInverseTargetBothAbsent(units):
		end = bindTargetAbsent
	}
	record.observeTarget(true, end)
	directory, files, listErr := h.runtimeFiles(layout)
	record.observeRuntimeFiles(directory, files, listErr != nil)
	return dnsenginerecovery.BINDSwitchNativeRestored, nil
}

// bindRNDCKeyCLIOutcome is what recover-dns-bind-switch did with the rndc key
// (the Agent's rule in cmd/agent/dns_engine_bind_rndc_key.go, same record).
type bindRNDCKeyCLIOutcome struct {
	Kind   string
	Path   string
	Detail string
}

const (
	// bindRNDCKeyNotOurs: no record for this switch, or the package or
	// owner provided the key; the key is not mentioned beyond the standby line.
	bindRNDCKeyNotOurs     = ""
	bindRNDCKeyRemoved     = "removed"
	bindRNDCKeyKeptChanged = "kept-changed"
	bindRNDCKeyKeptUnknown = "kept-unknown"
)

type bindRNDCKeyCLIOps struct {
	read   func() (bindrndckey.Record, bool, error)
	remove func(path, sha string) (bindrndckey.RemovalOutcome, error)
}

func retireBINDRNDCKeyForOwnerInverseWithOps(j dnsengineartifact.SwitchJournalV1, ops bindRNDCKeyCLIOps) bindRNDCKeyCLIOutcome {
	if ops.read == nil || ops.remove == nil {
		return bindRNDCKeyCLIOutcome{}
	}
	record, exists, err := ops.read()
	if err != nil {
		return bindRNDCKeyCLIOutcome{Kind: bindRNDCKeyKeptUnknown, Path: "(unknown path)", Detail: "its provenance record is unreadable."}
	}
	if !exists || record.Provenance != bindrndckey.ProvenanceProductCreated ||
		!record.SameTransaction(j.ManifestQualifier, j.MutationRequestID, j.MutationOwnerID) {
		return bindRNDCKeyCLIOutcome{}
	}
	outcome, err := ops.remove(record.Path, record.SHA256)
	switch {
	case err != nil:
		return bindRNDCKeyCLIOutcome{Kind: bindRNDCKeyKeptUnknown, Path: record.Path, Detail: err.Error()}
	case outcome == bindrndckey.RemovalRemoved || outcome == bindrndckey.RemovalAlreadyAbsent:
		return bindRNDCKeyCLIOutcome{Kind: bindRNDCKeyRemoved, Path: record.Path}
	default:
		return bindRNDCKeyCLIOutcome{Kind: bindRNDCKeyKeptChanged, Path: record.Path}
	}
}

func retireBINDRNDCKeyForOwnerInverse(_ context.Context, policy dnsengineartifact.JournalPolicy, j dnsengineartifact.SwitchJournalV1) bindRNDCKeyCLIOutcome {
	owner := servicemutationledger.FileOwner{UID: policy.StateUID, GID: policy.StateGID}
	path := filepath.Join(filepath.Dir(policy.StatePath), bindrndckey.RecordFileName)
	return retireBINDRNDCKeyForOwnerInverseWithOps(j, bindRNDCKeyCLIOps{
		read: func() (bindrndckey.Record, bool, error) {
			raw, present, err := servicemutationledger.ReadFile(path, 4<<10, owner)
			if err != nil || !present {
				return bindrndckey.Record{}, false, err
			}
			record, err := bindrndckey.Decode(raw)
			return record, err == nil, err
		},
		remove: bindrndckey.RemoveIfUnchanged,
	})
}
