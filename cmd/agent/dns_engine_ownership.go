package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const dnsEngineInstallOwnershipSchema = "celikpanel-dns-engine-install-ownership/v1"

// installingDNSEngineMutationMode names the modes that may put packages on the
// host. Adoption never does: it registers a PowerDNS that was already there.
//
// installingDNSEngineMutationMode, sunucuya paket koyabilecek kipleri
// adlandırır. Devralma bunu hiç yapmaz: zaten orada olan bir PowerDNS'i
// kaydeder.
func installingDNSEngineMutationMode(mode string) bool {
	return mode == transport.DNSEngineSwitchModeSwitch ||
		mode == transport.DNSEngineSwitchModeReinstall
}

// AdoptedPresent records the one way a mutation can take a target's packages
// under management without installing anything: they were already on the host.
// It is false on every receipt written before this field existed and on every
// receipt an install writes, so `omitempty` keeps those byte-identical - the
// receipt is compared against its own canonical re-encoding, and a field that
// appeared out of nowhere would make every stored receipt unreadable.
//
// AdoptedPresent, bir mutasyonun hedefin paketlerini hiçbir şey kurmadan
// yönetimine alabildiği tek yolu kaydeder: paketler sunucuda zaten vardı. Bu
// alan var olmadan önce yazılmış her makbuzda ve kurulumun yazdığı her makbuzda
// yanlıştır; `omitempty` bu yüzden onları bayt bayt aynı bırakır - makbuz kendi
// kanonik yeniden kodlamasıyla karşılaştırılır ve birdenbire beliren bir alan
// saklanan her makbuzu okunamaz kılardı.
type dnsEngineInstallOwnershipReceipt struct {
	Schema            string              `json:"schema"`
	Engine            transport.DNSEngine `json:"engine"`
	PackageManager    string              `json:"package_manager"`
	Packages          []string            `json:"packages"`
	MissingBefore     []string            `json:"missing_before"`
	ManifestQualifier string              `json:"manifest_qualifier"`
	MutationRequestID string              `json:"mutation_request_id"`
	MutationOwnerID   string              `json:"mutation_owner_id"`
	AdoptedPresent    bool                `json:"adopted_present,omitempty"`
}

func dnsEngineOwnershipPath(engine transport.DNSEngine) (string, error) {
	if !transport.ValidDNSEngine(engine) {
		return "", errors.New("DNS engine ownership receipt names an unsupported engine")
	}
	return filepath.Join(
		serviceMutationStateDirectory(),
		"dns-engine-ownership-"+string(engine)+".json",
	), nil
}

func readDNSEngineOwnership(
	engine transport.DNSEngine,
) (dnsEngineStateReceipt, bool, error) {
	path, err := dnsEngineOwnershipPath(engine)
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	data, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineStateReceipt{}, false, nil
	}
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	state, err := decodeDNSEngineState(data)
	if err != nil {
		return dnsEngineStateReceipt{}, false, err
	}
	if state.Engine != engine {
		return dnsEngineStateReceipt{}, false,
			errors.New("DNS engine ownership receipt engine differs from its path")
	}
	return state, true, nil
}

func writeDNSEngineOwnership(state dnsEngineStateReceipt) error {
	path, err := dnsEngineOwnershipPath(state.Engine)
	if err != nil {
		return err
	}
	encoded, err := encodeDNSEngineState(state)
	if err != nil {
		return err
	}
	if err := secureWriteConfig(path, encoded, 0o600); err != nil {
		actual, exists, readErr := readDNSEngineOwnership(state.Engine)
		if readErr == nil && exists && actual == state {
			return nil
		}
		return errors.Join(err, readErr)
	}
	actual, exists, err := readDNSEngineOwnership(state.Engine)
	if err != nil || !exists || actual != state {
		if err == nil {
			err = errors.New("DNS engine ownership receipt readback mismatch")
		}
		return err
	}
	return nil
}

func publishDNSEngineSourceOwnership(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	state dnsEngineStateReceipt,
	stateExists bool,
) error {
	if manifest.SourceEngine == "" {
		if stateExists {
			return errors.New("uninitialized DNS source unexpectedly has an active state receipt")
		}
		return nil
	}
	if !stateExists || state.Engine != manifest.SourceEngine ||
		state.EngineEpoch != manifest.SourceEpoch {
		return errors.New("DNS source ownership differs from the active engine state")
	}
	return writeDNSEngineOwnership(state)
}

func sourceStateFromDNSSwitchJournal(
	journal dnsEngineSwitchJournal,
) (dnsEngineStateReceipt, bool, error) {
	if journal.SourceEngine == "" {
		if journal.StateBefore.Exists {
			return dnsEngineStateReceipt{}, false,
				errors.New("uninitialized DNS source journal unexpectedly snapshots active state")
		}
		return dnsEngineStateReceipt{}, false, nil
	}
	if !journal.StateBefore.Exists {
		return dnsEngineStateReceipt{}, false,
			errors.New("DNS switch journal is missing source engine state")
	}
	state, err := decodeDNSEngineState(journal.StateBefore.Data)
	if err != nil {
		return dnsEngineStateReceipt{}, false,
			fmt.Errorf("decode DNS switch source state: %w", err)
	}
	if state.Engine != journal.SourceEngine ||
		state.EngineEpoch != journal.SourceEpoch {
		return dnsEngineStateReceipt{}, false,
			errors.New("DNS switch journal source state identity differs from its manifest")
	}
	return state, true, nil
}

func verifyDNSSwitchSourceOwnership(journal dnsEngineSwitchJournal) error {
	expected, exists, err := sourceStateFromDNSSwitchJournal(journal)
	if err != nil || !exists {
		return err
	}
	actual, actualExists, err := readDNSEngineOwnership(journal.SourceEngine)
	if err != nil {
		return err
	}
	if !actualExists || !reflect.DeepEqual(actual, expected) {
		return errors.New("DNS source ownership receipt is absent or differs from the switch journal")
	}
	return nil
}

func dnsEngineInstallOwnershipPath(engine transport.DNSEngine) (string, error) {
	if !transport.ValidDNSEngine(engine) {
		return "", errors.New("DNS engine install ownership names an unsupported engine")
	}
	return filepath.Join(
		serviceMutationStateDirectory(),
		"dns-engine-install-ownership-"+string(engine)+".json",
	), nil
}

func validateDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
) error {
	if receipt.Schema != dnsEngineInstallOwnershipSchema ||
		!transport.ValidDNSEngine(receipt.Engine) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(receipt.ManifestQualifier) ||
		!validMutationIdentity(receipt.MutationRequestID) ||
		!validMutationIdentity(receipt.MutationOwnerID) {
		return errors.New("DNS engine install ownership identity is invalid")
	}
	switch hostplatform.PackageManager(receipt.PackageManager) {
	case hostplatform.PackageManagerAPT, hostplatform.PackageManagerPacman,
		hostplatform.PackageManagerDNF:
	default:
		return errors.New("DNS engine install ownership package manager is unsupported")
	}
	if len(receipt.Packages) == 0 || len(receipt.Packages) > 32 ||
		len(receipt.MissingBefore) > len(receipt.Packages) {
		return errors.New("DNS engine install ownership package set is invalid")
	}
	// The two provenance kinds are told apart by one fact and cannot disagree
	// about it: an installation had something to install, an adoption had
	// nothing. A receipt claiming to be an adoption while naming packages it
	// installed - or claiming an installation that installed nothing - is not a
	// receipt this agent ever writes, and is refused here rather than believed
	// by whichever reader looks at only one of the two fields.
	//
	// İki köken türü tek bir olguyla ayrılır ve o olgu üzerinde çelişemezler:
	// kurulumun kuracak bir şeyi vardı, devralmanın yoktu. Kurduğu paketleri
	// adlandırırken devralma olduğunu iddia eden - ya da hiçbir şey kurmamış bir
	// kurulum olduğunu iddia eden - bir makbuzu bu ajan hiç yazmaz; iki alandan
	// yalnız birine bakan bir okuyucunun ona inanması yerine burada reddedilir.
	if receipt.AdoptedPresent != (len(receipt.MissingBefore) == 0) {
		return errors.New("DNS engine install ownership adoption disagrees with its missing set")
	}
	validateSorted := func(values []string) error {
		previous := ""
		for _, value := range values {
			if !validPackageName(value) || (previous != "" && value <= previous) {
				return errors.New("DNS engine install ownership packages are not canonical")
			}
			previous = value
		}
		return nil
	}
	if err := validateSorted(receipt.Packages); err != nil {
		return err
	}
	if err := validateSorted(receipt.MissingBefore); err != nil {
		return err
	}
	packageSet := make(map[string]struct{}, len(receipt.Packages))
	for _, packageName := range receipt.Packages {
		packageSet[packageName] = struct{}{}
	}
	for _, packageName := range receipt.MissingBefore {
		if _, ok := packageSet[packageName]; !ok {
			return errors.New("DNS engine install ownership missing set is not a package subset")
		}
	}
	return nil
}

func encodeDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
) ([]byte, error) {
	if err := validateDNSEngineInstallOwnership(receipt); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, fmt.Errorf("encode DNS engine install ownership: %w", err)
	}
	return append(encoded, '\n'), nil
}

func decodeDNSEngineInstallOwnership(
	data []byte,
) (dnsEngineInstallOwnershipReceipt, error) {
	if len(data) == 0 || len(data) > 64<<10 {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership has an invalid size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var receipt dnsEngineInstallOwnershipReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return dnsEngineInstallOwnershipReceipt{},
			fmt.Errorf("decode DNS engine install ownership: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership contains trailing JSON")
	}
	canonical, err := encodeDNSEngineInstallOwnership(receipt)
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, err
	}
	if !bytes.Equal(data, canonical) {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership is not canonical JSON")
	}
	return receipt, nil
}

func readDNSEngineInstallOwnership(
	engine transport.DNSEngine,
) (dnsEngineInstallOwnershipReceipt, bool, error) {
	path, err := dnsEngineInstallOwnershipPath(engine)
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, false, err
	}
	data, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return dnsEngineInstallOwnershipReceipt{}, false, nil
	}
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, false, err
	}
	receipt, err := decodeDNSEngineInstallOwnership(data)
	if err != nil {
		return dnsEngineInstallOwnershipReceipt{}, false, err
	}
	if receipt.Engine != engine {
		return dnsEngineInstallOwnershipReceipt{}, false,
			errors.New("DNS engine install ownership engine differs from its path")
	}
	return receipt, true, nil
}

func writeDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
) error {
	path, err := dnsEngineInstallOwnershipPath(receipt.Engine)
	if err != nil {
		return err
	}
	encoded, err := encodeDNSEngineInstallOwnership(receipt)
	if err != nil {
		return err
	}
	if err := secureWriteConfig(path, encoded, 0o600); err != nil {
		actual, exists, readErr := readDNSEngineInstallOwnership(receipt.Engine)
		if readErr == nil && exists && reflect.DeepEqual(actual, receipt) {
			return nil
		}
		return errors.Join(err, readErr)
	}
	actual, exists, err := readDNSEngineInstallOwnership(receipt.Engine)
	if err != nil || !exists || !reflect.DeepEqual(actual, receipt) {
		if err == nil {
			err = errors.New("DNS engine install ownership readback mismatch")
		}
		return err
	}
	return nil
}

func removeDNSEngineInstallOwnership(engine transport.DNSEngine) error {
	path, err := dnsEngineInstallOwnershipPath(engine)
	if err != nil {
		return err
	}
	removeErr := secureRemoveConfig(path)
	if removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		_, exists, readErr := readDNSEngineInstallOwnership(engine)
		if readErr != nil || exists {
			return errors.Join(removeErr, readErr)
		}
	}
	if err := syncAtomicParentDirectory(filepath.Dir(path)); err != nil {
		return err
	}
	_, exists, err := readDNSEngineInstallOwnership(engine)
	if err != nil {
		return err
	}
	if exists {
		return errors.New("DNS engine install ownership still exists after retirement")
	}
	return nil
}

func retireDNSEngineInstallOwnership(journal dnsEngineSwitchJournal) error {
	if journal.Phase != dnsSwitchPhaseCommitted ||
		!transport.ValidDNSEngine(journal.TargetEngine) {
		return errors.New("DNS engine install ownership can retire only for a committed target")
	}
	return removeDNSEngineInstallOwnership(journal.TargetEngine)
}

func publishCommittedDNSEngineTargetOwnership(
	journal dnsEngineSwitchJournal,
) error {
	if journal.Phase != dnsSwitchPhaseCommitted ||
		!transport.ValidDNSEngine(journal.TargetEngine) {
		return errors.New("DNS engine target ownership requires a committed journal")
	}
	state, exists, err := readDNSEngineState()
	if err != nil {
		return err
	}
	if !exists || !exactDNSEngineStateForJournal(state, journal) {
		return errors.New("DNS engine target state differs from its committed journal")
	}
	return writeDNSEngineOwnership(state)
}

func newDNSEngineInstallOwnership(
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages, missing []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (dnsEngineInstallOwnershipReceipt, error) {
	// A reinstall installs the same packages the switch does, for the same
	// engine, under the same durable lease. The receipt records who put the
	// packages there, and that is equally true either way.
	//
	// Yeniden kurulum, geçişin kurduğu paketlerin aynısını, aynı motor için,
	// aynı kalıcı kira altında kurar. Makbuz paketleri oraya kimin koyduğunu
	// kaydeder ve bu her iki durumda da aynı ölçüde doğrudur.
	//
	// An empty missing set is the adoption: nothing had to be installed because
	// the target's packages were already there. It is the same event as far as
	// provenance is concerned - this mutation, at this manifest, took these
	// packages under management - so it is the same constructor, and the
	// adoption flag is derived here rather than passed in, which is why the
	// receipt can never claim an adoption that installed something.
	//
	// Boş eksik kümesi devralmadır: hedefin paketleri zaten orada olduğu için
	// kurulacak bir şey yoktu. Köken açısından bu aynı olaydır - bu mutasyon, bu
	// manifest altında, bu paketleri yönetimine aldı - bu yüzden yapıcı da
	// aynıdır; devralma bayrağı dışarıdan verilmez, burada türetilir. Makbuzun
	// bir şey kurmuş bir devralmayı iddia edememesinin nedeni budur.
	if !installingDNSEngineMutationMode(manifest.Mode) ||
		manifest.TargetEngine != engine {
		return dnsEngineInstallOwnershipReceipt{},
			errors.New("DNS engine install ownership differs from its switch target")
	}
	receipt := dnsEngineInstallOwnershipReceipt{
		Schema: dnsEngineInstallOwnershipSchema,
		Engine: engine, PackageManager: string(manager),
		Packages:          append([]string(nil), packages...),
		MissingBefore:     append([]string{}, missing...),
		ManifestQualifier: manifest.Qualifier,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		AdoptedPresent:    len(missing) == 0,
	}
	sort.Strings(receipt.Packages)
	sort.Strings(receipt.MissingBefore)
	return receipt, validateDNSEngineInstallOwnership(receipt)
}

func installOwnedDNSEnginePackages(
	receipt dnsEngineInstallOwnershipReceipt,
	install func() error,
) error {
	if install == nil {
		return errors.New("DNS engine package installer is required")
	}
	if err := writeDNSEngineInstallOwnership(receipt); err != nil {
		return err
	}
	return install()
}

type dnsEngineInstallOwnershipAssumeOps struct {
	read  func(transport.DNSEngine) (dnsEngineInstallOwnershipReceipt, bool, error)
	write func(dnsEngineInstallOwnershipReceipt) error
}

// assumeExistingDNSEnginePackageOwnership runs when the target's packages are
// already on the host and nothing will be installed. It is the only writer of
// provenance on that path, and it must leave exactly what an install leaves,
// because finalization does not ask how the packages arrived - it asks whether
// this transaction can show that it owns them.
//
// Two host shapes reach it. Either an earlier attempt's receipt survived, in
// which case it is rebound to this transaction and its original missing set is
// preserved (a rolled-back attempt restores config, state and units but touches
// neither the packages nor the receipt, so without the rebind the retry carries
// a receipt bound to a dead request id into finalize). Or there is no receipt at
// all - a rebuilt host, an operator who installed the package by hand, a
// harness that preinstalled it, or a first attempt that itself found nothing
// missing and therefore wrote nothing - and then this mutation is the first to
// take those packages under management, which is a real, provable event and is
// recorded as one.
//
// Leaving that second case unrecorded is risk R-026's live failure: the switch
// reached its verified target, finalization found neither an install receipt nor
// an active ownership receipt, and the operation was held with "committed DNS
// engine switch has no exact install or active ownership provenance" - on every
// retry, because every retry found the packages present again.
//
// It cannot launder a foreign receipt. The written receipt always names THIS
// manifest qualifier and THIS request and owner id, and finalization compares
// all three against the journal it is finalizing; a surviving receipt is only
// ever rebound after it has been proven to describe this engine, this package
// manager and this exact package set on this host.
//
// assumeExistingDNSEnginePackageOwnership, hedefin paketleri sunucuda zaten
// varken ve hiçbir şey kurulmayacakken çalışır. O yolda kökeni yazan tek yerdir
// ve kurulumun bıraktığının aynısını bırakmak zorundadır; çünkü sonlandırma
// paketlerin nasıl geldiğini sormaz - bu işlemin onlara sahip olduğunu
// gösterip gösteremediğini sorar.
//
// Buraya iki sunucu biçimi ulaşır. Ya önceki bir denemenin makbuzu hayatta
// kalmıştır; o zaman makbuz bu işleme yeniden bağlanır ve özgün eksik kümesi
// korunur (geri alınmış bir deneme yapılandırmayı, durumu ve unit'leri onarır
// ama ne paketlere ne makbuza dokunur; yeniden bağlama olmadan yeniden deneme,
// ölü bir istek kimliğine bağlı makbuzu sonlandırmaya taşır). Ya da hiç makbuz
// yoktur - yeniden kurulmuş bir sunucu, paketi elle kuran bir operatör, onu
// önceden kuran bir koşum ya da kendisi de eksik bir şey bulamadığı için hiçbir
// şey yazmamış bir ilk deneme - ve o zaman bu paketleri yönetimine alan ilk
// mutasyon budur; bu gerçek ve kanıtlanabilir bir olaydır, öyle de kaydedilir.
//
// İkinci durumu kaydetmemek R-026'nın canlı arızasıdır: geçiş doğrulanmış
// hedefine ulaştı, sonlandırma ne kurulum makbuzu ne de etkin sahiplik makbuzu
// buldu ve işlem "committed DNS engine switch has no exact install or active
// ownership provenance" ile askıya alındı - her yeniden denemede, çünkü her
// yeniden deneme paketleri yine orada buldu.
//
// Yabancı bir makbuzu aklayamaz. Yazılan makbuz her zaman BU manifest
// niteleyicisini ve BU istek ile sahip kimliğini adlandırır; sonlandırma
// üçünü de sonlandırdığı günlükle karşılaştırır. Hayatta kalan bir makbuz ise
// yalnızca bu motoru, bu paket yöneticisini ve bu sunucudaki tam bu paket
// kümesini tarif ettiği kanıtlandıktan sonra yeniden bağlanır.
func assumeExistingDNSEnginePackageOwnership(
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) error {
	return assumeExistingDNSEnginePackageOwnershipWithOps(
		engine, manager, packages, manifest, binding,
		dnsEngineInstallOwnershipAssumeOps{
			read: readDNSEngineInstallOwnership, write: writeDNSEngineInstallOwnership,
		},
	)
}

func assumeExistingDNSEnginePackageOwnershipWithOps(
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
	ops dnsEngineInstallOwnershipAssumeOps,
) error {
	if ops.read == nil || ops.write == nil ||
		!installingDNSEngineMutationMode(manifest.Mode) ||
		manifest.TargetEngine != engine {
		return errors.New("invalid DNS engine install ownership handoff")
	}
	existing, exists, err := ops.read(engine)
	if err != nil {
		return err
	}
	rebound := existing
	if !exists {
		adopted, adoptedErr := newDNSEngineInstallOwnership(
			engine, manager, packages, nil, manifest, binding,
		)
		if adoptedErr != nil {
			return adoptedErr
		}
		rebound = adopted
	} else {
		if !exactDNSEngineInstallOwnership(existing, true, engine, manager, packages) {
			return errors.New("existing DNS engine install ownership differs from the retry target")
		}
		rebound.ManifestQualifier = manifest.Qualifier
		rebound.MutationRequestID = binding.MutationRequestID
		rebound.MutationOwnerID = binding.MutationOwnerID
	}
	if err := validateDNSEngineInstallOwnership(rebound); err != nil {
		return err
	}
	if err := ops.write(rebound); err != nil {
		return err
	}
	actual, actualExists, err := ops.read(engine)
	if err != nil {
		return err
	}
	if !actualExists || !reflect.DeepEqual(actual, rebound) {
		return errors.New("DNS engine install ownership handoff readback mismatch")
	}
	return nil
}

func exactDNSEngineInstallOwnership(
	receipt dnsEngineInstallOwnershipReceipt,
	exists bool,
	engine transport.DNSEngine,
	manager hostplatform.PackageManager,
	packages []string,
) bool {
	if !exists || receipt.Engine != engine ||
		receipt.PackageManager != string(manager) {
		return false
	}
	want := append([]string(nil), packages...)
	sort.Strings(want)
	return reflect.DeepEqual(receipt.Packages, want)
}

func managedDNSEnginePackagesForProfile(
	profile hostplatform.Profile,
	engine transport.DNSEngine,
) ([]string, error) {
	var packages []string
	switch engine {
	case transport.DNSEngineBIND:
		layout, err := bindLayout(profile)
		if err != nil {
			return nil, err
		}
		packages = append(packages, layout.Packages...)
	case transport.DNSEnginePowerDNS:
		service := core.GetManagedServiceByID("pdns")
		if service == nil {
			return nil, errors.New("PowerDNS service definition is unavailable")
		}
		family := string(profile.PackageManager)
		if service.LifecycleInstallFamilies == nil {
			return nil, errors.New("PowerDNS lifecycle policy is unavailable")
		}
		if !service.LifecycleInstallFamilies[family] {
			return nil, errors.New("PowerDNS lifecycle is unavailable for this host")
		}
		packages = append(
			packages,
			service.Packages[family]...,
		)
	default:
		return nil, errors.New("DNS engine package provenance target is unsupported")
	}
	if len(packages) == 0 {
		return nil, errors.New("DNS engine packages are unavailable for this host")
	}
	sort.Strings(packages)
	for index, packageName := range packages {
		if !validPackageName(packageName) ||
			(index > 0 && packages[index-1] == packageName) {
			return nil, errors.New("DNS engine package set is not canonical")
		}
	}
	return packages, nil
}

// supersededDNSEngineOwnership reports whether an active ownership receipt is
// simply an older epoch of the engine the committed state now names.
//
// Ownership receipts are per-engine files (dns-engine-ownership-<engine>.json)
// and nothing retires them: publishDNSEngineSourceOwnership refreshes only the
// engine being switched away from, so leaving an engine always strands its
// receipt at that epoch. Returning to a previously used engine therefore always
// finds one, and it can never equal the new state — the epoch alone differs by
// construction.
//
// Such a receipt is still true provenance: this host did own this engine. The
// committed publish that runs a few lines later overwrites it with the current
// state, so accepting it here is the difference between "the switch completes
// and the receipt is refreshed" and "the transaction is poisoned on an ordinary
// operator action, on every subsequent boot, until someone deletes a JSON file
// over SSH".
//
// A receipt at an equal or higher epoch is a genuine contradiction — two states
// claiming one epoch, or a receipt from ahead of the committed journal — and
// still refuses.
//
// supersededDNSEngineOwnership, bir aktif sahiplik makbuzunun yalnızca
// committed durumun adlandırdığı motorun daha eski bir çağı olup olmadığını
// bildirir. Makbuzlar motor başına dosyalardır ve hiçbir şey onları emekliye
// ayırmaz: publishDNSEngineSourceOwnership yalnız ayrılınan motoru tazeler,
// dolayısıyla bir motordan ayrılmak onun makbuzunu o çağda bırakır. Daha önce
// kullanılmış bir motora dönmek her zaman böyle bir makbuz bulur ve bu makbuz
// yeni durumla asla eşit olamaz — yapı gereği yalnızca çağ farklıdır.
//
// Böyle bir makbuz yine de gerçek bir köken kanıtıdır: bu sunucu bu motora
// sahip olmuştu. Birkaç satır sonra çalışan committed yayım onu güncel durumla
// üzerine yazar. Yani burada kabul etmek, "geçiş tamamlanır ve makbuz tazelenir"
// ile "sıradan bir operatör hareketinde işlem zehirlenir ve biri SSH ile bir
// JSON dosyasını silene kadar her açılışta yeniden zehirlenir" arasındaki
// farktır.
//
// Eşit ya da daha yüksek çağdaki bir makbuz gerçek bir çelişkidir — tek çağı
// iddia eden iki durum ya da committed günlüğün ilerisinden gelen bir makbuz —
// ve reddedilmeye devam eder.
func supersededDNSEngineOwnership(ownership, state dnsEngineStateReceipt) bool {
	return ownership.Engine == state.Engine &&
		ownership.EngineEpoch > 0 &&
		ownership.EngineEpoch < state.EngineEpoch
}

// acceptableCommittedDNSEngineOwnership is the ONE place that decides whether an
// existing active ownership receipt may stand for a committed transaction. Two
// provenance checkers ask this question — the host path and the signed-update
// path — and they must answer it identically: the signed-update walker runs on
// every release via `agent --prepare-bind-generation-root-under-external-lock`,
// so a rule that holds in one and not the other means an ordinary engine
// switch-back succeeds and the host's next update dies instead.
//
// That divergence is the defect this project keeps reproducing: one repair
// applied to one of two hand-written copies of the same job. Both callers go
// through here so a third copy has nowhere to hide.
//
// acceptableCommittedDNSEngineOwnership, mevcut bir aktif sahiplik makbuzunun
// committed bir işlem için geçerli sayılıp sayılmayacağına karar veren TEK
// yerdir. Bu soruyu iki köken denetleyicisi soruyor — host yolu ve imzalı
// güncelleme yolu — ve ikisi aynı cevabı vermek zorunda: imzalı güncelleme
// yürüyücüsü her sürümde çalışır, dolayısıyla birinde geçerli olup diğerinde
// olmayan bir kural, sıradan bir motor geri dönüşünün başarılı olup sunucunun
// bir sonraki güncellemesinin ölmesi demektir.
//
// Bu ayrışma, bu projenin tekrar tekrar ürettiği kusurdur: aynı işin elle
// yazılmış iki kopyasından yalnız birine uygulanan bir onarım. Her iki çağıran
// da buradan geçer ki üçüncü bir kopyanın saklanacak yeri olmasın.
func acceptableCommittedDNSEngineOwnership(
	ownership, state dnsEngineStateReceipt,
	journal dnsEngineSwitchJournal,
) error {
	if validateDNSEngineState(ownership) != nil {
		return errors.New("committed DNS engine ownership is malformed")
	}
	if ownership != state && !supersededDNSEngineOwnership(ownership, state) &&
		!reinstalledDNSEngineOwnership(ownership, state, journal) {
		return errors.New("committed DNS engine ownership differs from its exact active state")
	}
	return nil
}

// reinstalledDNSEngineOwnership is the third way an ownership receipt can
// legitimately differ from the active state, and it exists only for a
// reinstall. The epoch rule above tells history from a contradiction by
// comparing epochs: older is history, equal-or-newer is two states claiming one
// epoch. A reinstall breaks that arithmetic on purpose — it puts the engine
// back at the epoch it already owns — so the receipt it replaces is at the SAME
// epoch and differs in the generation and mutation identity the new
// installation created. Without this the reinstall would be read as the
// contradiction the epoch rule was written to catch, and would fail after the
// packages were already on the host.
//
// It is not a loosening: the receipt must equal the exact source state the
// journal froze before the operation began. Nothing else at an equal epoch is
// accepted, so a genuine two-states-one-epoch conflict still refuses.
//
// reinstalledDNSEngineOwnership, bir sahiplik makbuzunun etkin durumdan meşru
// biçimde farklı olabileceği üçüncü yoldur ve yalnız yeniden kurulum için
// vardır. Yukarıdaki çağ kuralı tarihi çelişkiden çağları karşılaştırarak
// ayırır: daha eski olan tarihtir, eşit ya da daha yeni olan tek çağı iddia
// eden iki durumdur. Yeniden kurulum bu aritmetiği bilerek bozar — motoru
// zaten sahip olduğu çağa geri koyar — bu yüzden yerine geçtiği makbuz AYNI
// çağdadır ve yeni kurulumun ürettiği nesil ile mutasyon kimliğinde ayrışır.
// Bu olmadan yeniden kurulum, çağ kuralının yakalamak için yazıldığı çelişki
// sanılır ve paketler sunucuya çoktan indikten sonra düşerdi.
//
// Bu bir gevşetme değildir: makbuz, işlem başlamadan önce günlüğün dondurduğu
// tam kaynak durumuna eşit olmak zorundadır. Eşit çağda başka hiçbir şey kabul
// edilmez; dolayısıyla gerçek bir tek-çağ-iki-durum çelişkisi hâlâ reddedilir.
func reinstalledDNSEngineOwnership(
	ownership, state dnsEngineStateReceipt,
	journal dnsEngineSwitchJournal,
) bool {
	if journal.Mode != transport.DNSEngineSwitchModeReinstall ||
		journal.SourceEngine != journal.TargetEngine ||
		journal.SourceEpoch != journal.TargetEpoch ||
		state.Engine != journal.TargetEngine ||
		state.EngineEpoch != journal.TargetEpoch {
		return false
	}
	source, exists, err := sourceStateFromDNSSwitchJournal(journal)
	return err == nil && exists && ownership == source
}

func exactCommittedDNSEngineProvenanceOnHost(
	journal dnsEngineSwitchJournal,
	profile hostplatform.Profile,
) (dnsEngineStateReceipt, bool, bool, error) {
	if err := validateDNSEngineSwitchJournal(journal); err != nil ||
		journal.Phase != dnsSwitchPhaseCommitted {
		if err == nil {
			err = errors.New("DNS engine provenance requires a committed journal")
		}
		return dnsEngineStateReceipt{}, false, false, err
	}
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine state receipt: %w", err)
	}
	if !stateExists || validateDNSEngineState(state) != nil ||
		!exactDNSEngineStateForJournal(state, journal) {
		return dnsEngineStateReceipt{}, false, false,
			errors.New("committed DNS engine state receipt is absent or differs from its journal")
	}
	manifest, err := switchJournalManifest(journal)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false, err
	}
	request := signedUpdateRollbackEvidenceRequest(journal, manifest)
	install, installExists, err := readDNSEngineInstallOwnership(journal.TargetEngine)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine install ownership: %w", err)
	}
	if installExists {
		packages, err := managedDNSEnginePackagesForProfile(profile, journal.TargetEngine)
		if err != nil {
			return dnsEngineStateReceipt{}, false, false, err
		}
		if validateDNSEngineInstallOwnership(install) != nil ||
			!exactDNSEngineInstallEvidence(install, request, manifest) ||
			!exactDNSEngineInstallOwnership(
				install, true, journal.TargetEngine,
				profile.PackageManager, packages,
			) {
			return dnsEngineStateReceipt{}, false, false,
				errors.New("committed DNS engine install ownership differs from its journal")
		}
	}
	ownership, ownershipExists, err := readDNSEngineOwnership(journal.TargetEngine)
	if err != nil {
		return dnsEngineStateReceipt{}, false, false,
			fmt.Errorf("read committed DNS engine ownership: %w", err)
	}
	if ownershipExists {
		if err := acceptableCommittedDNSEngineOwnership(ownership, state, journal); err != nil {
			return dnsEngineStateReceipt{}, false, false, err
		}
	}
	if journal.Mode != transport.DNSEngineSwitchModeAdopt {
		if !installExists {
			if !ownershipExists {
				return dnsEngineStateReceipt{}, false, false,
					errors.New("committed DNS engine switch has no exact install or active ownership provenance")
			}
		}
	}
	return state, installExists, ownershipExists, nil
}

func exactFinalizedDNSEngineSwitchProvenanceOnHost(
	target transport.DNSEngine,
	qualifier string,
	binding transport.ServiceMutationBinding,
) (bool, error) {
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return false, fmt.Errorf("read finalized DNS engine state receipt: %w", err)
	}
	ownership, ownershipExists, err := readDNSEngineOwnership(target)
	if err != nil {
		return false, fmt.Errorf("read finalized DNS engine ownership: %w", err)
	}
	install, installExists, err := readDNSEngineInstallOwnership(target)
	if err != nil {
		return false, fmt.Errorf("read finalized DNS engine install ownership: %w", err)
	}
	if !stateExists {
		// An ownership receipt with no active state is a real contradiction.
		// An install receipt alone is not: on a host that has never had an
		// engine, a first install that failed after package install leaves
		// exactly this behind, and nothing ever served. Calling it an error
		// meant the abort proof failed, the ledger was poisoned, and the poison
		// was recomputed from that same install receipt on every boot - the
		// R-019 wedge, on a fresh host, on the very first DNS action (live on
		// Arch, 3 September 2026; register R-033). It is residue: fail the job
		// cleanly and let the retry adopt the installed packages.
		//
		// Etkin durumu olmayan sahiplik makbuzu gerçek bir çelişkidir. Tek
		// başına kurulum makbuzu değildir: hiç motoru olmamış bir sunucuda
		// paket kurulumundan sonra düşen ilk kurulum geride tam bunu bırakır
		// ve hiçbir şey hizmet vermemiştir. Buna hata demek iptal kanıtını
		// düşürüyor, defteri zehirliyor ve zehir her açılışta aynı kurulum
		// makbuzundan yeniden hesaplanıyordu - R-019 tıkanması, taze
		// sunucuda, ilk DNS hareketinde (3 Eylül 2026'da Arch'ta canlı; defter
		// R-033). Bu kalıntıdır: işi temizce düşür, yeniden deneme kurulu
		// paketleri devralsın.
		if ownershipExists {
			return false, errors.New(
				"finalized DNS engine provenance is inconsistent without active state",
			)
		}
		return false, nil
	}
	if err := validateDNSEngineState(state); err != nil {
		return false, fmt.Errorf("validate finalized DNS engine state receipt: %w", err)
	}
	exactTransaction := state.Engine == target &&
		state.ManifestQualifier == qualifier &&
		state.MutationRequestID == binding.MutationRequestID &&
		state.MutationOwnerID == binding.MutationOwnerID
	if !exactTransaction {
		currentOwnership := ownership
		currentOwnershipExists := ownershipExists
		if state.Engine != target {
			currentOwnership, currentOwnershipExists, err = readDNSEngineOwnership(state.Engine)
			if err != nil {
				return false, fmt.Errorf("read current DNS engine ownership: %w", err)
			}
		}
		if !currentOwnershipExists {
			return false, errors.New(
				"journal-free DNS engine state has no matching active ownership",
			)
		}
		if err := validateDNSEngineState(currentOwnership); err != nil {
			return false, fmt.Errorf("validate current DNS engine ownership: %w", err)
		}
		if currentOwnership != state {
			return false, errors.New(
				"journal-free DNS engine state differs from its active ownership",
			)
		}
		if installExists {
			// An install receipt on a target that owns nothing is residue, not
			// ambiguity.
			//
			// Everything above has already proven the source side intact: a
			// valid active state, an ownership receipt for the ACTIVE engine,
			// and the two exactly equal. If the target additionally holds no
			// ownership receipt of its own, then the interrupted switch got as
			// far as installing packages and no further — authority never
			// moved, and there is nothing to roll back. Reporting that as an
			// error made startup recovery poison the mutation manager, and
			// because the poison is recomputed from unchanged durable state,
			// every subsequent boot reproduced it: a host that could serve DNS
			// perfectly well but could never accept another mutation, with no
			// path out short of a rebuild (risk R-019).
			//
			// A target that DOES hold an ownership receipt while a different
			// engine is active is the genuinely ambiguous half-finished
			// handover — the Boston shape — and keeps failing closed.
			//
			// Sahibi olmadığı hâlde hedefte duran bir kurulum makbuzu
			// belirsizlik değil kalıntıdır.
			//
			// Yukarıdaki her şey kaynak tarafın sağlam olduğunu zaten
			// kanıtladı: geçerli bir aktif durum, AKTİF motor için bir sahiplik
			// makbuzu ve ikisinin birebir eşitliği. Hedefin ayrıca kendine ait
			// bir sahiplik makbuzu yoksa, kesintiye uğrayan geçiş yalnızca
			// paketleri kurmaya kadar gelmiştir — yetki hiç el değiştirmemiştir
			// ve geri alınacak bir şey yoktur. Bunu hata olarak bildirmek,
			// başlangıç kurtarmasının mutasyon yöneticisini zehirlemesine yol
			// açıyordu; zehir değişmemiş kalıcı durumdan yeniden hesaplandığı
			// için de sonraki her açılış onu yeniden üretiyordu: DNS'i gayet iyi
			// sunabilen ama bir daha hiçbir mutasyonu kabul edemeyen, yeniden
			// kurmaktan başka çıkışı olmayan bir sunucu (risk R-019).
			//
			// Başka bir motor etkinken hedefin sahiplik makbuzu DA taşıması,
			// gerçekten belirsiz olan yarım kalmış devirdir — Boston biçimi — ve
			// kapalı arıza vermeyi sürdürür.
			//
			// One more distinction, learned on the same host shape (S-8
			// Boston, register R-032). Ownership receipts are per-engine
			// files and nothing retires them (see
			// supersededDNSEngineOwnership): a host that ever ran the target
			// engine still holds its receipt from that older epoch. On the
			// BIND -> PowerDNS -> BIND path that stranded receipt sat beside
			// the fresh install receipt and was read as the Boston shape, so
			// an interrupted switch back to an engine the host had used before
			// poisoned the ledger on an ordinary operator action. The epoch
			// tells the two apart by construction: a target receipt OLDER than
			// the active state is history; a receipt at the same or a newer
			// epoch is a receipt from ahead of the committed state and stays
			// ambiguous.
			//
			// Aynı sunucu biçiminde öğrenilen bir ayrım daha (S-8 Boston,
			// defter R-032). Sahiplik makbuzları motor başına dosyalardır ve
			// hiçbir şey onları emekliye ayırmaz (bkz.
			// supersededDNSEngineOwnership): hedef motoru bir zamanlar
			// çalıştırmış bir sunucu, onun o eski çağdan makbuzunu hâlâ tutar.
			// BIND -> PowerDNS -> BIND yolunda o terk edilmiş makbuz taze
			// kurulum makbuzunun yanında durdu ve Boston biçimi sanıldı;
			// böylece daha önce kullanılmış bir motora geri dönen kesintili
			// geçiş, sıradan bir operatör hareketinde defteri zehirledi. Çağ
			// ikisini yapı gereği ayırır: etkin durumdan ESKİ bir hedef makbuzu
			// tarihtir; aynı ya da daha yeni çağdaki makbuz committed durumun
			// ilerisinden gelen bir makbuzdur ve belirsiz kalır.
			historicalOwnership := ownershipExists &&
				ownership.EngineEpoch < state.EngineEpoch
			if state.Engine != target && (!ownershipExists || historicalOwnership) {
				return false, nil
			}
			// A reinstall's target IS the active engine, so the install
			// receipt sits on the engine that already owns the host and the
			// two clauses above cannot classify it. What settles it is whose
			// receipt it is: this one names the exact transaction now being
			// aborted, and everything above has already proven the source side
			// intact — a valid active state and an ownership receipt equal to
			// it. So the interrupted reinstall got as far as installing
			// packages and no further. Authority never moved because a
			// reinstall never moves it, and there is nothing to roll back.
			//
			// Calling that a contradiction is how the drill's first live
			// reinstall ended: packages on the host, the abort proof failing,
			// the ledger poisoned, and the poison recomputed from unchanged
			// durable state on every boot — the R-019 wedge again, reached by
			// the one path built to get a host out of trouble.
			//
			// An install receipt on the active engine that names some OTHER
			// transaction is not ours to dismiss and still fails closed.
			//
			// Yeniden kurulumun hedefi zaten sunucunun sahibi olan motordur;
			// bu yüzden kurulum makbuzu o motorun üzerinde durur ve yukarıdaki
			// iki koşul onu sınıflandıramaz. Meseleyi çözen şey makbuzun kime
			// ait olduğudur: bu makbuz, şu anda iptal edilen işlemin ta
			// kendisini adlandırır ve yukarıdaki her şey kaynak tarafın sağlam
			// olduğunu zaten kanıtlamıştır — geçerli bir etkin durum ve ona
			// eşit bir sahiplik makbuzu. Demek ki kesintiye uğrayan yeniden
			// kurulum yalnızca paketleri kurmaya kadar gelmiştir. Yetki hiç
			// değişmemiştir, çünkü yeniden kurulum onu hiç değiştirmez; geri
			// alınacak bir şey de yoktur.
			//
			// Buna çelişki demek, tatbikatın ilk canlı yeniden kurulumunun
			// bitiş biçimidir: paketler sunucuda, iptal kanıtı düşüyor, defter
			// zehirleniyor ve zehir her açılışta değişmemiş kalıcı durumdan
			// yeniden hesaplanıyor — bir sunucuyu dertten çıkarmak için
			// yapılmış tek yolla ulaşılan R-019 tıkanması.
			//
			// Etkin motordaki BAŞKA bir işlemi adlandıran kurulum makbuzu bizim
			// göz ardı edeceğimiz bir şey değildir ve kapalı arıza vermeyi
			// sürdürür.
			if state.Engine == target &&
				install.ManifestQualifier == qualifier &&
				install.MutationRequestID == binding.MutationRequestID &&
				install.MutationOwnerID == binding.MutationOwnerID {
				return false, nil
			}
			return false, errors.New(
				"journal-free DNS engine target retains transitional install ownership",
			)
		}
		return false, nil
	}
	if !ownershipExists {
		return false, errors.New("finalized DNS engine ownership is absent")
	}
	if err := validateDNSEngineState(ownership); err != nil {
		return false, fmt.Errorf("validate finalized DNS engine ownership: %w", err)
	}
	if ownership != state {
		return false, errors.New("finalized DNS engine ownership differs from its active state")
	}
	if installExists {
		return false, errors.New("finalized DNS engine install ownership was not retired")
	}
	return true, nil
}
