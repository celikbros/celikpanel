package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/hostplatform"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Adopting a BIND that is answering queries right now (register R-039).
//
// The first design for this said the agent would have to stop and seal a unit
// it does not own before its proofs could run, and would therefore need a
// durable pre-intent record it does not have. The product already contained the
// better answer: PowerDNS adoption (dns_engine_pdns_adopt.go) never stops or
// starts the unit. It writes its intent journal first, captures the existing
// configuration, proves at mutation time that the configuration is still
// exactly what it captured, and only then replaces it in place. There is no
// window in which the host is not serving, so there is nothing for a pre-intent
// record to recover. This file is the same shape for BIND, against a
// configuration the panel did not write.
//
// Adoption has its own evidence path. proveBINDTargetNotServing and the port-53
// pre-mutation guard are the switch's proofs, and they are the wrong proofs for
// an operation that is not a switch: this file neither calls them nor relaxes
// them. What it proves instead is the mirror image and is no weaker - the
// running BIND is the vendor unit, it is the sole public port-53 authority on
// this host, and the process that holds those sockets is the unit's own main
// process. verifyOnlyBINDActive is that proof; it is the switch's post-start
// proof, reused here before the mutation because a running adoption starts
// where a switch finishes.
//
// Two decisions this file makes, and why.
//
// RELOAD, NOT RESTART. Every option CelikPanel's generation writes into the
// server's own configuration is one BIND re-reads on reload: recursion,
// allow-recursion, allow-query-cache, allow-transfer, and an include of the
// generation's zones.conf. None of them is in the set that needs a restart -
// listen-on, listen-on-v6, port, query-source, directory - because the panel
// writes none of those. So the unit is reloaded, the process and the sockets it
// holds survive, and the host answers throughout. reloadAdoptedBIND proves that
// rather than assuming it: the unit's MainPID must be the same number after the
// reload as before, and its substate must still be "running". A changed MainPID
// means BIND restarted behind the reload, which is a gap in service the
// operator was not told about, so it fails the adoption and rolls back.
//
// THE FOREIGN ZONES ARE TAKEN OVER, NOT REFUSED. CelikPanel's BIND generation
// is additive: it adds an options block inside the server's options block and
// an include line to its zone anchor, and it deletes no zone declaration. A
// zone this server answers today therefore keeps being answered after the
// adoption - it is not orphaned, because nothing removes it. Refusing to adopt
// a server because it answers zones the panel does not know about would refuse
// the entire class of host this work exists for, a populated working DNS
// server, and refuse it for a harm that does not occur. So they are taken under
// the same acknowledgement, and the screen says what that means: they stay in
// the server's own files, CelikPanel will not manage them, and CelikPanel's
// authoritative options - no recursion, no transfers - now apply to the whole
// server.
//
// There is exactly one foreign zone that cannot survive, and it is refused by
// name before anything is touched: one whose name CelikPanel's own generation
// also declares. BIND refuses a configuration that declares a zone twice, so
// this would fail late, inside named-checkconf, with a message about a file the
// operator never wrote. foreignBINDZoneCollisions finds it first and says which
// zone it is.
//
// Sorgu yanıtlarken bir BIND'i devralmak (defter R-039).
//
// Bunun ilk tasarımı, agent'ın kanıtları koşabilmek için sahibi olmadığı bir
// birimi durdurup mühürlemesi ve bu yüzden sahip olmadığı kalıcı bir ön-niyet
// kaydına ihtiyaç duyması gerektiğini söylüyordu. Daha iyi cevap üründe zaten
// vardı: PowerDNS devralması (dns_engine_pdns_adopt.go) birimi hiç durdurmaz ve
// başlatmaz. Önce niyet günlüğünü yazar, mevcut yapılandırmayı yakalar,
// mutasyon anında yapılandırmanın hâlâ birebir yakaladığı şey olduğunu kanıtlar
// ve ancak o zaman onu yerinde değiştirir. Sunucunun hizmet vermediği bir
// pencere yoktur; dolayısıyla bir ön-niyet kaydının kurtaracağı bir şey de
// yoktur. Bu dosya, panelin yazmadığı bir yapılandırmaya karşı, BIND için aynı
// biçimdir.
//
// Devralmanın kendi kanıt yolu vardır. proveBINDTargetNotServing ve 53 numaralı
// bağlantı noktası ön-mutasyon koruması geçişin kanıtlarıdır ve geçiş olmayan
// bir işlem için yanlış kanıtlardır: bu dosya onları ne çağırır ne gevşetir.
// Bunun yerine kanıtladığı şey ayna görüntüsüdür ve daha zayıf değildir -
// çalışan BIND satıcı birimidir, bu sunucudaki tek genel 53 numaralı yetkedir
// ve o soketleri tutan süreç birimin kendi ana sürecidir. verifyOnlyBINDActive
// bu kanıttır; geçişin başlatma sonrası kanıtıdır ve burada mutasyondan önce
// yeniden kullanılır, çünkü çalışan bir devralma, bir geçişin bittiği yerde
// başlar.
//
// Bu dosyanın verdiği iki karar ve sebepleri.
//
// YENİDEN BAŞLATMA DEĞİL, YENİDEN YÜKLEME. CelikPanel'in neslinin sunucunun
// kendi yapılandırmasına yazdığı her seçenek, BIND'in yeniden yüklemede
// yeniden okuduğu bir seçenektir: recursion, allow-recursion,
// allow-query-cache, allow-transfer ve neslin zones.conf'unun include'u.
// Hiçbiri yeniden başlatma gerektiren kümede değildir - listen-on,
// listen-on-v6, port, query-source, directory - çünkü panel bunların hiçbirini
// yazmaz. Dolayısıyla birim yeniden yüklenir, süreç ve tuttuğu soketler yaşamayı
// sürdürür ve sunucu boyunca yanıt verir. reloadAdoptedBIND bunu varsaymaz,
// kanıtlar: birimin MainPID'i yeniden yüklemeden sonra öncekiyle aynı sayı
// olmalı ve alt durumu hâlâ "running" olmalıdır. Değişmiş bir MainPID, BIND'in
// yeniden yüklemenin arkasından yeniden başladığı anlamına gelir; bu,
// operatöre söylenmemiş bir hizmet boşluğudur, devralmayı düşürür ve geri alır.
//
// YABANCI BÖLGELER REDDEDİLMEZ, DEVRALINIR. CelikPanel'in BIND nesli
// eklemelidir: sunucunun seçenek bloğunun içine bir seçenek bloğu ve bölge
// çıpasına bir include satırı ekler, hiçbir bölge bildirimini silmez. Bu
// sunucunun bugün yanıtladığı bir bölge, devralmadan sonra da yanıtlanmayı
// sürdürür - öksüz kalmaz, çünkü onu kaldıran bir şey yoktur. Panelin
// bilmediği bölgeleri yanıtladığı için bir sunucuyu devralmayı reddetmek, bu
// işin var olma sebebi olan tüm sunucu sınıfını - dolu, çalışan bir DNS
// sunucusunu - ve onu gerçekleşmeyen bir zarar için reddetmek olurdu. Bu
// yüzden aynı onay altında devralınırlar ve ekran bunun ne demek olduğunu
// söyler: sunucunun kendi dosyalarında kalırlar, CelikPanel onları yönetmez ve
// CelikPanel'in yetkili seçenekleri - recursion yok, transfer yok - artık tüm
// sunucuya uygulanır.
//
// Yaşayamayacak tam olarak bir yabancı bölge vardır ve hiçbir şeye
// dokunulmadan adıyla reddedilir: adını CelikPanel'in kendi neslinin de
// bildirdiği bölge. BIND, bir bölgeyi iki kez bildiren bir yapılandırmayı
// reddeder; dolayısıyla bu, operatörün hiç yazmadığı üretilmiş bir dosya
// hakkında bir mesajla, named-checkconf'un içinde geç düşerdi.
// foreignBINDZoneCollisions onu önce bulur ve hangi bölge olduğunu söyler.

// bindAdoptionRuntimeEvidence pins the running server this adoption is about to
// take over: which vendor units are loaded, what their systemd identity is,
// which process holds port 53, and the exact public listener set. Every
// mutation re-proves it, so an adoption that started against one running BIND
// can never finish against another.
//
// bindAdoptionRuntimeEvidence, bu devralmanın devralmak üzere olduğu çalışan
// sunucuyu sabitler: hangi satıcı birimleri yüklü, systemd kimlikleri ne, 53
// numaralı bağlantı noktasını hangi süreç tutuyor ve kesin genel dinleyici
// kümesi ne. Her mutasyon bunu yeniden kanıtlar; böylece bir çalışan BIND'e
// karşı başlayan devralma asla bir başkasına karşı bitemez.
type bindAdoptionRuntimeEvidence struct {
	topology  bindRuntimeTopologySnapshot
	listeners []string
}

// adoptableRunningBINDManifest is the exact manifest the panel's takeover sends
// for a running host, and nothing else may enter this path. It is the panel's
// adopt_unmanaged commitment: an initial standalone BIND activation with no
// source engine, epoch 0 to 1, dispatched as an ordinary switch because that is
// the transaction the takeover reuses.
//
// adoptableRunningBINDManifest, panelin devralmasının çalışan bir sunucu için
// gönderdiği kesin bildirgedir ve bu yola başka hiçbir şey giremez. Panelin
// adopt_unmanaged taahhüdüdür: kaynak motoru olmayan, 0'dan 1'e çağ, tek
// sunuculu bir ilk BIND etkinleştirmesi; sıradan bir geçiş olarak gönderilir,
// çünkü devralmanın yeniden kullandığı işlem odur.
func adoptableRunningBINDManifest(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	stateExists bool,
) bool {
	return !stateExists &&
		manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.TargetEngine == transport.DNSEngineBIND &&
		manifest.SourceEngine == "" &&
		manifest.SourceEpoch == 0 && manifest.TargetEpoch == 1 &&
		manifest.Topology == transport.DNSTopologyStandalone &&
		manifest.PairRole == "" && manifest.LocalIP == "" &&
		manifest.LocalNS == "" && manifest.PeerIP == "" && manifest.PeerNS == ""
}

// runningBINDAdoptionSelected decides between the two halves of the takeover on
// the one fact that separates them: whether the target is answering. A stopped
// target is the R-038 shape and takes the first-install transaction unchanged,
// switch proofs and all. A running target takes this file. Nothing else is
// inferred, and a manifest that is not the takeover's never reaches either
// question - it stays on the switch path, where a running BIND is refused
// exactly as it always was.
//
// runningBINDAdoptionSelected, devralmanın iki yarısı arasında onları ayıran
// tek olguya bakarak karar verir: hedef yanıt veriyor mu. Durmuş hedef R-038
// biçimidir ve ilk kurulum işlemini geçiş kanıtlarıyla birlikte değişmeden
// alır. Çalışan hedef bu dosyayı alır. Başka hiçbir şey çıkarsanmaz ve
// devralmanınki olmayan bir bildirge bu sorulardan hiçbirine ulaşmaz - geçiş
// yolunda kalır, orada çalışan bir BIND her zamanki gibi reddedilir.
func runningBINDAdoptionSelected(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	stateExists bool,
) (bool, error) {
	if !adoptableRunningBINDManifest(manifest, stateExists) {
		return false, nil
	}
	named, alias, err := inspectBINDTargetStates(ctx, systemctl)
	if err != nil {
		return false, err
	}
	return named.active() || alias.active(), nil
}

// stoppedBINDTakeoverSelected recognises the other half of the takeover: the
// panel offered adopt_unmanaged for a BIND that is installed but not answering,
// and that half runs the ordinary first-install transaction rather than this
// file. The transaction cannot tell it apart from a first install by the
// manifest alone - the two are byte-identical, deliberately - so it is told
// apart by the same fact the panel used: this host carries no durable
// CelikPanel authority over BIND. A first install the panel itself performed
// has an install-ownership receipt by the time its configuration is prepared; a
// server somebody else installed has neither receipt.
//
// It decides one thing only: whether the operator's own recursion,
// allow-recursion, allow-query-cache and allow-transfer are read as part of
// what is being replaced (register R-042) or refused. No proof branches on it.
//
// stoppedBINDTakeoverSelected, devralmanın öbür yarısını tanır: panel, kurulu
// ama yanıt vermeyen bir BIND için adopt_unmanaged sunmuştur ve o yarı, bu dosya
// yerine sıradan ilk kurulum işlemini çalıştırır. İşlem onu yalnız bildirgeye
// bakarak ilk kurulumdan ayıramaz - ikisi bilerek birebir aynıdır - bu yüzden
// panelin kullandığı olguyla ayırt edilir: bu sunucu BIND üzerinde kalıcı hiçbir
// CelikPanel yetkisi taşımıyor. Panelin kendi yaptığı bir ilk kurulumun,
// yapılandırması hazırlanırken bir kurulum-sahiplik makbuzu vardır; başkasının
// kurduğu bir sunucunun iki makbuzu da yoktur.
//
// Tek bir şeye karar verir: operatörün kendi recursion, allow-recursion,
// allow-query-cache ve allow-transfer direktiflerinin değiştirilenin parçası
// olarak mı okunacağı (defter R-042) yoksa reddedileceği mi. Hiçbir kanıt buna
// dallanmaz.
func stoppedBINDTakeoverSelected(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	stateExists bool,
) (bool, error) {
	if !adoptableRunningBINDManifest(manifest, stateExists) {
		return false, nil
	}
	if _, exists, err := readDNSEngineOwnership(
		transport.DNSEngineBIND,
	); err != nil || exists {
		return false, err
	}
	if _, exists, err := readDNSEngineInstallOwnership(
		transport.DNSEngineBIND,
	); err != nil || exists {
		return false, err
	}
	return true, nil
}

// bindSwitchOptionsAuthority is the one place the transaction decides whether a
// directive the operator wrote is refused or taken over.
//
// bindSwitchOptionsAuthority, işlemin, operatörün yazdığı bir direktifin
// reddedileceğine mi devralınacağına mı karar verdiği tek yerdir.
func bindSwitchOptionsAuthority(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	stateExists bool,
) (bindOptionsAuthority, error) {
	takeover, err := stoppedBINDTakeoverSelected(manifest, stateExists)
	if err != nil {
		return bindOptionsExclusive, err
	}
	if takeover {
		return bindOptionsTakeover, nil
	}
	return bindOptionsExclusive, nil
}

func enableAdoptedBINDUnitIfNeeded(
	ctx context.Context,
	systemctl string,
	unit string,
	before dnsUnitState,
) error {
	if before.UnitFileState == "enabled" {
		return nil
	}
	// A hand-started server may never have been enabled, and an adopted engine
	// that does not come back after a reboot is not adopted. Enabling starts
	// nothing and stops nothing; the journal records the unit-file state this
	// unit had, so a rollback puts it back.
	//
	// Elle başlatılmış bir sunucu hiç etkinleştirilmemiş olabilir ve yeniden
	// başlatmadan sonra geri gelmeyen bir motor devralınmış değildir.
	// Etkinleştirme hiçbir şeyi başlatmaz ve durdurmaz; günlük bu birimin
	// birim-dosya durumunu kaydeder, dolayısıyla geri alma onu geri koyar.
	return runBINDMutationWithMaskParentProof(
		verifyBINDMaskParentMetadata,
		func() error {
			return enableServiceForMutationWithExecutable(
				ctx, systemctl, unit, false,
			)
		},
	)
}

// verifyRunningBINDAdoptionSource is the adoption's source proof, and it is
// the third place where the switch's proof is the wrong one. An uninitialized
// switch source proves that NOTHING authoritative is running and that nothing
// holds port 53, because a first install is about to start the engine and
// anything already there would be a stranger it might trample. A running
// adoption is the exact opposite premise: the engine it is adopting is the
// thing that is running, and it is holding port 53 on purpose.
//
// What has to be true is the rest of that proof, unchanged. No other engine may
// be running. Nothing but the target may hold the public port. And the host may
// carry no durable authority of ours: no engine state receipt, and no engine
// ownership receipt naming a tenure the panel already had - because then this
// would be a repair of our own engine and not a takeover of somebody else's,
// and it would have a different path and a different provenance.
//
// verifyRunningBINDAdoptionSource, devralmanın kaynak kanıtıdır ve geçişin
// kanıtının yanlış olduğu üçüncü yerdir. Başlatılmamış bir geçiş kaynağı,
// yetkili HİÇBİR ŞEYİN çalışmadığını ve 53 numaralı bağlantı noktasını hiçbir
// şeyin tutmadığını kanıtlar; çünkü bir ilk kurulum motoru başlatmak üzeredir
// ve orada olan her şey, üzerinden geçebileceği bir yabancı olurdu. Çalışan bir
// devralmanın öncülü tam tersidir: devraldığı motor, çalışan şeyin ta
// kendisidir ve 53 numaralı bağlantı noktasını bilerek tutmaktadır.
//
// Doğru olması gereken şey, o kanıtın geri kalanıdır; değişmeden. Başka hiçbir
// motor çalışmıyor olmalı. Genel bağlantı noktasını hedeften başka hiçbir şey
// tutmamalı. Ve sunucu bizim hiçbir kalıcı yetkimizi taşımamalı: ne motor durum
// makbuzu, ne de panelin zaten sahip olduğu bir dönemi adlandıran motor
// sahiplik makbuzu - çünkü o zaman bu, bir başkasının sunucusunun devralınması
// değil kendi motorumuzun onarımı olurdu; başka bir yolu ve başka bir kökeni
// vardır.
func verifyRunningBINDAdoptionSource(
	ctx context.Context,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	stateExists bool,
) error {
	if ctx == nil {
		return errors.New("BIND adoption source proof requires a context")
	}
	if !adoptableRunningBINDManifest(manifest, stateExists) {
		return errors.New("BIND adoption source proof received a switch transaction")
	}
	pdns, err := captureDNSUnitState(ctx, systemctl, "pdns.service")
	if err != nil {
		return err
	}
	if pdns.active() {
		return errors.New("BIND adoption found another authoritative DNS engine running")
	}
	named, alias, err := inspectBINDTargetStates(ctx, systemctl)
	if err != nil {
		return err
	}
	if !named.active() && !alias.active() {
		return errors.New("BIND adoption target is not serving")
	}
	ownership, exists, err := readDNSEngineOwnership(transport.DNSEngineBIND)
	if err != nil {
		return err
	}
	if exists {
		return fmt.Errorf(
			"BIND adoption found a durable CelikPanel authority at epoch %d",
			ownership.EngineEpoch,
		)
	}
	conflict, err := dnsPort53ConflictCheck(ctx, true, false)
	if err != nil {
		return err
	}
	if conflict {
		return errors.New("BIND adoption found another public port-53 authority")
	}
	return nil
}

func captureBINDAdoptionRuntimeEvidence(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) (bindAdoptionRuntimeEvidence, error) {
	if ctx == nil {
		return bindAdoptionRuntimeEvidence{},
			errors.New("BIND adoption runtime evidence requires a context")
	}
	if err := verifyOnlyBINDActive(ctx, profile, systemctl); err != nil {
		return bindAdoptionRuntimeEvidence{}, fmt.Errorf(
			"prove the adopted BIND is the only DNS authority on this host: %w", err,
		)
	}
	proofCtx, cancel := context.WithTimeout(ctx, dnsRuntimeInspectionTimeout)
	defer cancel()
	topology, err := inspectVerifiedBINDRuntimeTopology(proofCtx, profile, systemctl)
	if err != nil {
		return bindAdoptionRuntimeEvidence{}, err
	}
	if topology.namedProcesses.MainPID == 0 ||
		topology.namedProcesses.SubState != "running" {
		return bindAdoptionRuntimeEvidence{},
			errors.New("the adopted BIND has no running main process")
	}
	listeners, err := adoptedBINDPublicListeners(
		proofCtx, topology.namedProcesses.MainPID,
	)
	if err != nil {
		return bindAdoptionRuntimeEvidence{}, err
	}
	if len(listeners) == 0 {
		return bindAdoptionRuntimeEvidence{},
			errors.New("the adopted BIND holds no public port-53 listener")
	}
	return bindAdoptionRuntimeEvidence{topology: topology, listeners: listeners}, nil
}

func adoptedBINDPublicListeners(
	ctx context.Context,
	mainPID uint64,
) ([]string, error) {
	ss, err := firstTrustedExecutable(
		[]string{"/usr/sbin/ss", "/usr/bin/ss"}, "ss",
	)
	if err != nil {
		return nil, err
	}
	output, err := serviceMutationCommand(
		ctx, ss, "-H", "-lntup", "sport = :53",
	).CombinedOutputLimited(64 << 10)
	if err != nil {
		return nil, fmt.Errorf(
			"inspect adopted BIND listeners: %w: %s", err, firstLine(string(output)),
		)
	}
	return canonicalBINDPublicListeners(string(output), mainPID)
}

// verify re-proves the captured running server. It is the running adoption's
// answer to "is this still the same host I looked at", and it is called before
// every mutation, exactly as the PowerDNS adoption re-proves its configuration.
//
// verify, yakalanan çalışan sunucuyu yeniden kanıtlar. Çalışan devralmanın
// "bu hâlâ baktığım sunucu mu" sorusuna cevabıdır ve tıpkı PowerDNS
// devralmasının yapılandırmasını yeniden kanıtlaması gibi her mutasyondan önce
// çağrılır.
func (evidence bindAdoptionRuntimeEvidence) verify(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
) error {
	actual, err := captureBINDAdoptionRuntimeEvidence(ctx, profile, systemctl)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(actual.topology, evidence.topology) {
		return errors.New("the adopted BIND unit identity or process changed")
	}
	if !reflect.DeepEqual(actual.listeners, evidence.listeners) {
		return errors.New("the adopted BIND public listener set changed")
	}
	return nil
}

// mutateBINDAdoptionAfterProof is the running adoption's mutation gate: the
// captured configuration must still be exactly what is on disk, and the running
// server must still be exactly the one that was captured, or nothing is
// written. It is the BIND twin of mutatePDNSAdoptionAfterConfigProof.
//
// mutateBINDAdoptionAfterProof, çalışan devralmanın mutasyon kapısıdır:
// yakalanan yapılandırma hâlâ birebir diskteki olmalı ve çalışan sunucu hâlâ
// birebir yakalanan olmalıdır; aksi hâlde hiçbir şey yazılmaz.
// mutatePDNSAdoptionAfterConfigProof'un BIND ikizidir.
func mutateBINDAdoptionAfterProof(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	configs bindConfigMutation,
	evidence bindAdoptionRuntimeEvidence,
	mutation func() error,
) error {
	if ctx == nil || mutation == nil {
		return errors.New("BIND adoption mutation callback is required")
	}
	if err := verifyBINDConfigMutationPreimage(ctx, configs); err != nil {
		return err
	}
	if err := evidence.verify(ctx, profile, systemctl); err != nil {
		return err
	}
	return mutation()
}

// bindDeclaredZoneNames reads the zone names a BIND configuration declares.
// Comments are blanked by stripBINDCommentsAndStrings before the keyword is
// looked for, so a zone named only inside a comment is not a declaration; the
// quoted name itself is read from the original text, because the stripper
// blanks string bodies in place and leaves their offsets intact.
//
// bindDeclaredZoneNames, bir BIND yapılandırmasının bildirdiği bölge adlarını
// okur. Anahtar kelime aranmadan önce yorumlar stripBINDCommentsAndStrings ile
// silinir; dolayısıyla yalnız bir yorumun içinde adı geçen bölge bir bildirim
// değildir. Tırnaklı adın kendisi özgün metinden okunur, çünkü silici dize
// gövdelerini yerinde boşaltır ve konumlarını olduğu gibi bırakır.
func bindDeclaredZoneNames(config string) []string {
	stripped := stripBINDCommentsAndStrings(config)
	seen := map[string]struct{}{}
	names := []string{}
	const keyword = "zone"
	for index := 0; index+len(keyword) <= len(stripped); index++ {
		if stripped[index:index+len(keyword)] != keyword {
			continue
		}
		if index > 0 && bindIdentifierPart(stripped[index-1]) {
			continue
		}
		cursor := index + len(keyword)
		if cursor < len(stripped) && bindIdentifierPart(stripped[cursor]) {
			continue
		}
		for cursor < len(config) && (config[cursor] == ' ' ||
			config[cursor] == '\t' || config[cursor] == '\n' ||
			config[cursor] == '\r') {
			cursor++
		}
		if cursor >= len(config) || config[cursor] != '"' {
			continue
		}
		cursor++
		start := cursor
		for cursor < len(config) && config[cursor] != '"' {
			if config[cursor] == '\\' {
				cursor++
			}
			cursor++
		}
		if cursor >= len(config) {
			continue
		}
		name := strings.ToLower(strings.TrimSuffix(config[start:cursor], "."))
		if name == "" {
			continue
		}
		if _, duplicate := seen[name]; duplicate {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// foreignBINDZoneCollisions names the zones the adoption cannot take over: the
// ones this server already declares under a name CelikPanel's own generation
// declares too. BIND refuses a configuration that declares a zone twice, so
// without this the adoption would fail inside named-checkconf with a message
// about a generated file the operator has never seen. Every other zone the
// server declares is kept: it stays in the server's own files and keeps being
// answered.
//
// foreignBINDZoneCollisions, devralmanın devralamayacağı bölgeleri adlandırır:
// bu sunucunun, CelikPanel'in kendi neslinin de bildirdiği bir adla zaten
// bildirdiği bölgeler. BIND, bir bölgeyi iki kez bildiren bir yapılandırmayı
// reddeder; bu olmadan devralma, operatörün hiç görmediği üretilmiş bir dosya
// hakkında bir mesajla named-checkconf'un içinde düşerdi. Sunucunun bildirdiği
// diğer her bölge korunur: sunucunun kendi dosyalarında kalır ve yanıtlanmayı
// sürdürür.
func foreignBINDZoneCollisions(
	foreign []string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) []string {
	managed := make(map[string]struct{}, len(manifest.Zones))
	for _, zone := range manifest.Zones {
		if zone.Delete {
			continue
		}
		managed[strings.ToLower(strings.TrimSuffix(zone.Domain, "."))] = struct{}{}
	}
	collisions := []string{}
	for _, name := range foreign {
		if _, clash := managed[name]; clash {
			collisions = append(collisions, name)
		}
	}
	return collisions
}

func foreignBINDConfigurationZones(
	layout bindHostLayout,
	configs bindConfigMutation,
) ([]string, error) {
	seen := map[string]struct{}{}
	texts := []string{}
	for _, path := range configs.paths {
		if _, duplicate := seen[path]; duplicate {
			continue
		}
		seen[path] = struct{}{}
		texts = append(texts, string(configs.original[path]))
	}
	if _, duplicate := seen[layout.MainConfig]; !duplicate {
		data, err := secureReadConfig(layout.MainConfig)
		if err != nil {
			return nil, fmt.Errorf(
				"read %s before adoption: %w", layout.MainConfig, err,
			)
		}
		texts = append(texts, string(data))
	}
	names := map[string]struct{}{}
	for _, text := range texts {
		for _, name := range bindDeclaredZoneNames(text) {
			names[name] = struct{}{}
		}
	}
	ordered := make([]string, 0, len(names))
	for name := range names {
		ordered = append(ordered, name)
	}
	sort.Strings(ordered)
	return ordered, nil
}

// requireForeignBINDConfiguration refuses to "adopt" a configuration the panel
// already owns. If the generation's include is already anchored, the desired
// bytes equal the original bytes and there is nothing foreign here; that host
// is a managed one whose receipt is missing, which is a different problem with
// a different repair, and calling it an adoption would write an adoption
// provenance for a takeover that never happened.
//
// requireForeignBINDConfiguration, panelin zaten sahibi olduğu bir
// yapılandırmayı "devralmayı" reddeder. Neslin include'u çıpalanmışsa istenen
// baytlar özgün baytlara eşittir ve burada yabancı bir şey yoktur; o sunucu,
// makbuzu eksik yönetilen bir sunucudur, bu başka onarımı olan başka bir
// sorundur ve buna devralma demek, hiç yaşanmamış bir devralma için devralma
// kökeni yazmak olurdu.
func requireForeignBINDConfiguration(
	layout bindHostLayout,
	configs bindConfigMutation,
) error {
	anchor := layout.AnchorConfig
	if reflect.DeepEqual(configs.original[anchor], configs.desired[anchor]) {
		return errors.New(
			"BIND adoption found CelikPanel's own zone anchor already in place",
		)
	}
	return nil
}

// reloadAdoptedBIND is the running adoption's cutover, and the whole reason the
// host never stops answering. It proves what it claims: the unit's main process
// must be the same process after the reload, still running. A restarted process
// is a gap in service, and this operation promised there would not be one.
//
// reloadAdoptedBIND, çalışan devralmanın geçişidir ve sunucunun yanıt vermeyi
// hiç bırakmamasının bütün sebebidir. İddia ettiğini kanıtlar: birimin ana
// süreci yeniden yüklemeden sonra da aynı süreç olmalı ve hâlâ çalışmalıdır.
// Yeniden başlamış bir süreç hizmette bir boşluktur ve bu işlem böyle bir şey
// olmayacağına söz verdi.
func reloadAdoptedBIND(
	ctx context.Context,
	systemctl string,
	unit string,
	expected dnsUnitProcesses,
) error {
	if ctx == nil || unit != "named.service" {
		return errors.New("invalid adopted BIND reload")
	}
	return reloadAdoptedBINDWithOps(
		expected,
		func() error {
			output, err := runServiceMutationCombinedOutput(
				ctx, systemctl, "reload", unit,
			)
			if err != nil {
				return fmt.Errorf(
					"reload adopted BIND: %w: %s", err, firstLine(string(output)),
				)
			}
			return nil
		},
		func() (dnsUnitProcesses, error) {
			return inspectDNSUnitProcesses(ctx, systemctl, unit)
		},
	)
}

func reloadAdoptedBINDWithOps(
	expected dnsUnitProcesses,
	reload func() error,
	inspect func() (dnsUnitProcesses, error),
) error {
	if reload == nil || inspect == nil {
		return errors.New("invalid adopted BIND reload operations")
	}
	if expected.MainPID == 0 || expected.SubState != "running" {
		return errors.New("adopted BIND reload has no running process to preserve")
	}
	if err := reload(); err != nil {
		return err
	}
	after, err := inspect()
	if err != nil {
		return err
	}
	if after.MainPID == 0 || after.MainPID != expected.MainPID ||
		after.SubState != "running" {
		return fmt.Errorf(
			"adopted BIND did not stay up across its reload: main pid %d->%d substate %q",
			expected.MainPID, after.MainPID, after.SubState,
		)
	}
	return nil
}

type bindAdoptionRollbackOps struct {
	restoreConfigs func() error
	reload         func() error
	verifyConfigs  func() error
	verifyRuntime  func() error
	restoreState   func() error
	restoreUnits   func() error
}

// rollbackRunningBINDAdoptionWithOps restores the configuration the host had,
// exactly, and leaves the server answering what it answered before. The order
// matters: the files come back first, then the server is told to re-read them,
// and only then is the result proven. Nothing here stops BIND - stopping it is
// what a switch rollback does, and it would turn a failed adoption into the
// outage the adoption was designed not to have.
//
// rollbackRunningBINDAdoptionWithOps, sunucunun sahip olduğu yapılandırmayı
// birebir geri yükler ve sunucuyu önce yanıtladığını yanıtlar hâlde bırakır.
// Sıra önemlidir: önce dosyalar geri gelir, sonra sunucuya onları yeniden
// okuması söylenir ve ancak ondan sonra sonuç kanıtlanır. Burada hiçbir şey
// BIND'i durdurmaz - onu durdurmak geçiş geri almasının yaptığı şeydir ve
// başarısız bir devralmayı, devralmanın yaşanmasın diye tasarlandığı kesintiye
// çevirirdi.
func rollbackRunningBINDAdoptionWithOps(ops bindAdoptionRollbackOps) error {
	if ops.restoreConfigs == nil || ops.reload == nil ||
		ops.verifyConfigs == nil || ops.verifyRuntime == nil ||
		ops.restoreState == nil || ops.restoreUnits == nil {
		return errors.New("invalid BIND adoption rollback operations")
	}
	if err := ops.restoreConfigs(); err != nil {
		return err
	}
	if err := ops.reload(); err != nil {
		return err
	}
	if err := ops.verifyConfigs(); err != nil {
		return err
	}
	if err := ops.verifyRuntime(); err != nil {
		return err
	}
	if err := ops.restoreState(); err != nil {
		return err
	}
	return ops.restoreUnits()
}

func rollbackRunningBINDAdoption(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	configs bindConfigMutation,
	evidence bindAdoptionRuntimeEvidence,
	stateBefore dnsFileSnapshot,
	targetBefore map[string]dnsUnitState,
) error {
	if ctx == nil {
		return errors.New("rollback BIND adoption requires a bounded context")
	}
	return rollbackRunningBINDAdoptionWithOps(bindAdoptionRollbackOps{
		restoreConfigs: func() error {
			return runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error { return configs.restore(ctx) },
			)
		},
		reload: func() error {
			return reloadAdoptedBIND(
				ctx, systemctl, "named.service", evidence.topology.namedProcesses,
			)
		},
		verifyConfigs: func() error {
			return verifyBINDConfigMutationPreimage(ctx, configs)
		},
		verifyRuntime: func() error {
			return evidence.verify(ctx, profile, systemctl)
		},
		restoreState: func() error {
			return restoreDNSEngineStateSnapshot(stateBefore)
		},
		restoreUnits: func() error {
			return restoreDNSUnitStates(ctx, systemctl, targetBefore, true)
		},
	})
}

// verifyRestoredRunningBINDAdoption is the running adoption's answer to
// verifyRestoredDNSSwitchSource, which is the wrong proof here: an empty-source
// switch rollback proves that nothing is answering on port 53, and an adoption
// rollback must prove the opposite - the foreign server is answering again,
// from exactly the configuration it had.
//
// verifyRestoredRunningBINDAdoption, çalışan devralmanın
// verifyRestoredDNSSwitchSource'a cevabıdır; o burada yanlış kanıttır: kaynağı
// boş bir geçiş geri alması 53 numaralı bağlantı noktasında hiçbir şeyin yanıt
// vermediğini kanıtlar, bir devralma geri alması ise tersini kanıtlamalıdır -
// yabancı sunucu, sahip olduğu yapılandırmanın birebir kendisinden yeniden
// yanıt veriyor.
func verifyRestoredRunningBINDAdoption(
	ctx context.Context,
	profile hostplatform.Profile,
	systemctl string,
	configs bindConfigMutation,
	evidence bindAdoptionRuntimeEvidence,
) error {
	if ctx == nil {
		return errors.New("verify restored BIND adoption requires a context")
	}
	if err := verifyBINDConfigMutationPreimage(ctx, configs); err != nil {
		return err
	}
	if _, exists, err := readDNSEngineState(); err != nil || exists {
		if err == nil {
			err = errors.New(
				"rolled-back BIND adoption left an active DNS engine receipt",
			)
		}
		return err
	}
	return evidence.verify(ctx, profile, systemctl)
}

// runningBINDAdoptionJournal answers the one question a crash recovery has to
// settle before it touches anything: was the BIND this journal describes
// answering queries when the mutation began? Its answer decides which rollback
// is the right one, and getting it wrong in the safe-looking direction is the
// defect this function exists to remove (register R-043).
//
// It reads two durable facts and nothing else. The first is the manifest the
// journal reconstructs: the takeover's manifest is an initial standalone BIND
// activation with no source engine, epoch 0 to 1, and no other operation shares
// that shape. The second is the target unit preimage the journal froze before
// the first byte was written. A first install and the stopped half of the
// takeover both begin with named.service inactive - their rollback stops what
// their mutation started, and the switch rollback is exactly right for them. A
// running takeover begins with named.service active, because the operator's own
// server is answering, and stopping it would be the outage the takeover
// promised would not happen.
//
// Anything else is not classified. A preimage that is not the two units the
// takeover freezes, or one that says the unit and its distribution alias
// disagreed while both were loaded, is a state this code cannot read - and the
// R-019 family of wedges came from guessing at exactly that kind of state - so
// it fails closed and names what it found.
//
// runningBINDAdoptionJournal, bir çökme kurtarmasının herhangi bir şeye
// dokunmadan önce çözmesi gereken tek soruyu yanıtlar: bu günlüğün tarif ettiği
// BIND, mutasyon başladığında sorgu yanıtlıyor muydu? Cevabı hangi geri almanın
// doğru olduğuna karar verir ve bunu güvenli görünen yönde yanlış yapmak, bu
// işlevin ortadan kaldırmak için var olduğu kusurdur (defter R-043).
//
// İki kalıcı olgu okur, başka hiçbir şey değil. Birincisi günlüğün yeniden
// kurduğu bildirgedir: devralmanın bildirgesi, kaynak motoru olmayan, 0'dan 1'e
// çağ, tek sunuculu bir ilk BIND etkinleştirmesidir ve bu biçimi başka hiçbir
// işlem paylaşmaz. İkincisi, günlüğün ilk bayt yazılmadan önce dondurduğu hedef
// birim ön-görüntüsüdür. Bir ilk kurulum ve devralmanın durmuş yarısı,
// named.service etkin değilken başlar - geri almaları, mutasyonlarının
// başlattığı şeyi durdurur ve geçiş geri alması onlar için tam olarak doğrudur.
// Çalışan bir devralma, named.service etkinken başlar; çünkü operatörün kendi
// sunucusu yanıt vermektedir ve onu durdurmak, devralmanın olmayacağına söz
// verdiği kesinti olurdu.
//
// Başka her şey sınıflandırılmaz. Devralmanın dondurduğu iki birim olmayan bir
// ön-görüntü ya da birim ile dağıtım takma adının ikisi de yüklüyken
// uyuşmadığını söyleyen bir ön-görüntü, bu kodun okuyamadığı bir durumdur - ve
// R-019 ailesindeki çıkmazlar tam da böyle bir durumu tahmin etmekten doğdu -
// bu yüzden kapalı düşer ve bulduğunu adlandırır.
func runningBINDAdoptionJournal(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	journal dnsEngineSwitchJournal,
) (bool, error) {
	if journal.TargetEngine != transport.DNSEngineBIND ||
		!adoptableRunningBINDManifest(manifest, false) {
		return false, nil
	}
	units := make(map[string]dnsUnitSnapshot, len(journal.TargetUnitsBefore))
	names := make([]string, 0, len(journal.TargetUnitsBefore))
	for _, snapshot := range journal.TargetUnitsBefore {
		if _, duplicate := units[snapshot.Name]; duplicate {
			return false, fmt.Errorf(
				"BIND takeover journal cannot be classified: its target unit "+
					"preimage names %s twice", snapshot.Name,
			)
		}
		units[snapshot.Name] = snapshot
		names = append(names, snapshot.Name)
	}
	named, hasNamed := units["named.service"]
	alias, hasAlias := units["bind9.service"]
	if !hasNamed || !hasAlias || len(units) != 2 {
		found := "nothing"
		if len(names) != 0 {
			found = strings.Join(names, ", ")
		}
		return false, fmt.Errorf(
			"BIND takeover journal cannot be classified: its target unit "+
				"preimage names %s, not bind9.service and named.service", found,
		)
	}
	namedActive := named.ActiveState == "active"
	aliasActive := alias.ActiveState == "active"
	if !namedActive && !aliasActive {
		return false, nil
	}
	// named.service and bind9.service are one unit and its distribution alias
	// on every layout the product supports, so they agree unless one of them is
	// not on this host at all. A disagreement between two loaded units is not a
	// takeover this code can read.
	//
	// named.service ile bind9.service, ürünün desteklediği her yerleşimde bir
	// birim ve onun dağıtım takma adıdır; dolayısıyla biri bu sunucuda hiç
	// yoksa dışında uyuşurlar. Yüklü iki birim arasındaki bir uyuşmazlık, bu
	// kodun okuyabileceği bir devralma değildir.
	if namedActive != aliasActive {
		quiet := named
		if namedActive {
			quiet = alias
		}
		if quiet.LoadState != "not-found" {
			return false, fmt.Errorf(
				"BIND takeover journal cannot be classified: named.service was "+
					"%q and bind9.service was %q before the mutation, and %s is "+
					"loaded on this host", named.ActiveState, alias.ActiveState,
				quiet.Name,
			)
		}
	}
	return true, nil
}

type bindAdoptionRecoveryOps struct {
	captureEvidence func() (bindAdoptionRuntimeEvidence, error)
	proveCurrent    func() error
	rollback        func(bindAdoptionRuntimeEvidence) error
	restorePointer  func() error
	verifyRestored  func(bindAdoptionRuntimeEvidence) error
}

// recoverRunningBINDAdoptionJournalWithOps is the order a crashed takeover is
// put back in, and every step of it assumes the server is still answering.
//
// The running server is re-proved first, and it is proved fresh rather than
// read out of the journal: a crash and a reboot give BIND a new main process,
// and what this recovery has to hold still is the process it finds now, across
// its own restore and reload. Then the configuration on disk is proved to be
// either the bytes the takeover found or the bytes it wrote - anything else is
// a third state nobody wrote, and the recovery refuses it. Then the adoption
// rollback runs, which restores, reloads and re-proves, and stops nothing. Only
// after the server is answering its own configuration again is the generation
// pointer taken down, because the configuration BIND was serving until a moment
// ago included that generation's zones.
//
// A crash before the configuration was written needs none of that undone, and
// this is deliberately still the path it takes: the restore writes the bytes
// that are already there, the reload re-reads a file that did not change, and
// the ledger is released instead of being held for a host that has nothing
// wrong with it. One path, no phase to guess at.
//
// recoverRunningBINDAdoptionJournalWithOps, çökmüş bir devralmanın geri
// konduğu sıradır ve her adımı sunucunun hâlâ yanıt verdiğini varsayar.
//
// Çalışan sunucu önce yeniden kanıtlanır ve günlükten okunmak yerine taze
// kanıtlanır: bir çökme ve bir yeniden başlatma BIND'e yeni bir ana süreç
// verir; bu kurtarmanın sabit tutması gereken şey, kendi geri yüklemesi ve
// yeniden yüklemesi boyunca şimdi bulduğu süreçtir. Sonra diskteki
// yapılandırmanın ya devralmanın bulduğu baytlar ya da yazdığı baytlar olduğu
// kanıtlanır - başka her şey kimsenin yazmadığı üçüncü bir durumdur ve kurtarma
// onu reddeder. Sonra devralma geri alması koşar; geri yükler, yeniden yükler,
// yeniden kanıtlar ve hiçbir şeyi durdurmaz. Nesil işaretçisi ancak sunucu
// kendi yapılandırmasını yeniden yanıtlar hâle geldikten sonra indirilir; çünkü
// BIND az önceye dek o neslin bölgelerini içeren yapılandırmayı sunuyordu.
//
// Yapılandırma yazılmadan önceki bir çökmenin geri alınacak hiçbir şeyi yoktur
// ve bilerek yine bu yolu izler: geri yükleme zaten orada olan baytları yazar,
// yeniden yükleme değişmemiş bir dosyayı yeniden okur ve defter, hiçbir sorunu
// olmayan bir sunucu için tutulmak yerine bırakılır. Tek yol, tahmin edilecek
// bir aşama yok.
func recoverRunningBINDAdoptionJournalWithOps(
	ops bindAdoptionRecoveryOps,
) error {
	if ops.captureEvidence == nil || ops.proveCurrent == nil ||
		ops.rollback == nil || ops.restorePointer == nil ||
		ops.verifyRestored == nil {
		return errors.New("invalid BIND adoption recovery operations")
	}
	evidence, err := ops.captureEvidence()
	if err != nil {
		return fmt.Errorf(
			"prove the interrupted takeover's BIND is still answering: %w", err,
		)
	}
	if err := ops.proveCurrent(); err != nil {
		return err
	}
	if err := ops.rollback(evidence); err != nil {
		return err
	}
	if err := ops.restorePointer(); err != nil {
		return err
	}
	return ops.verifyRestored(evidence)
}

func recoverRunningBINDAdoptionJournal(
	ctx context.Context,
	profile hostplatform.Profile,
	layout bindHostLayout,
	systemctl string,
	journal dnsEngineSwitchJournal,
) error {
	if ctx == nil {
		return errors.New("recover BIND adoption requires a bounded context")
	}
	// The takeover's manifest is standalone, so there is no transfer peer to
	// reconstruct; the journal's own bytes carry the rest.
	//
	// Devralmanın bildirgesi tek sunuculudur, dolayısıyla yeniden kurulacak bir
	// aktarım eşi yoktur; gerisini günlüğün kendi baytları taşır.
	configs, err := bindConfigMutationFromJournal(layout, "", journal)
	if err != nil {
		return err
	}
	publisher, _, err := newHostBINDPublisher(ctx, layout)
	if err != nil {
		return err
	}
	return recoverRunningBINDAdoptionJournalWithOps(bindAdoptionRecoveryOps{
		captureEvidence: func() (bindAdoptionRuntimeEvidence, error) {
			return captureBINDAdoptionRuntimeEvidence(ctx, profile, systemctl)
		},
		proveCurrent: func() error {
			_, _, proofErr := configs.captureOwnerAwareCurrent(ctx, false)
			return proofErr
		},
		rollback: func(evidence bindAdoptionRuntimeEvidence) error {
			return rollbackRunningBINDAdoption(
				ctx, profile, systemctl, configs, evidence,
				journal.StateBefore,
				dnsUnitSnapshotsMap(journal.TargetUnitsBefore),
			)
		},
		restorePointer: func() error {
			return runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error {
					return publisher.RestorePointer(
						journal.TargetGeneration, journal.PreviousGeneration,
						journal.HadPrevious,
					)
				},
			)
		},
		verifyRestored: func(evidence bindAdoptionRuntimeEvidence) error {
			return verifyRestoredRunningBINDAdoption(
				ctx, profile, systemctl, configs, evidence,
			)
		},
	})
}

// verifyRestoredUnmanagedRunningBINDTarget is the takeover's answer to the
// switch's sealed-target proof, and it is proved rather than assumed. What it
// asserts is that CelikPanel owns nothing on this host that is live: no
// generation pointer, no managed options block inside the server's own options,
// no managed include inside its zone anchor - and that the thing answering is
// the vendor's own BIND, the sole public port-53 authority here. The receipts
// (the journal, the engine state, the engine ownership and the install
// ownership) are proved absent or exact by the caller before this runs, so what
// is left to prove is the configuration and the runtime.
//
// verifyRestoredUnmanagedRunningBINDTarget, geçişin mühürlü-hedef kanıtına
// devralmanın cevabıdır ve varsayılmaz, kanıtlanır. İddiası şudur: CelikPanel'in
// bu sunucuda canlı hiçbir şeyi yoktur - nesil işaretçisi yok, sunucunun kendi
// seçenek bloğunun içinde yönetilen blok yok, bölge çıpasında yönetilen include
// yok - ve yanıt veren şey, buradaki tek genel 53 numaralı yetke olan satıcının
// kendi BIND'idir. Makbuzlar (günlük, motor durumu, motor sahipliği ve kurulum
// sahipliği) bu koşmadan önce çağıran tarafından yok ya da birebir kanıtlanır;
// geriye kanıtlanacak olan yapılandırma ile çalışma zamanıdır.
func verifyRestoredUnmanagedRunningBINDTarget(
	ctx context.Context,
	systemctl string,
) error {
	if ctx == nil || systemctl == "" {
		return errors.New("invalid restored unmanaged BIND proof")
	}
	profile, err := verifiedHostProfileForAnyFamily()
	if err != nil {
		return err
	}
	layout, err := bindLayout(profile)
	if err != nil {
		return err
	}
	pointer := filepath.Join(layout.GenerationRoot, "current")
	if _, err := os.Lstat(pointer); err == nil {
		return fmt.Errorf(
			"the restored BIND still points at a CelikPanel generation at %s",
			pointer,
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, owned := range []struct {
		path   string
		marker string
	}{
		{layout.OptionsConfig, bindOptionsMarkerBegin},
		{layout.AnchorConfig, bindZonesMarkerBegin},
	} {
		data, readErr := os.ReadFile(owned.path)
		if readErr != nil {
			return readErr
		}
		if strings.Contains(string(data), owned.marker) {
			return fmt.Errorf(
				"the restored BIND configuration %s still carries %q",
				owned.path, owned.marker,
			)
		}
	}
	return verifyOnlyBINDActive(ctx, profile, systemctl)
}

func adoptRunningBIND(
	ctx context.Context,
	profile hostplatform.Profile,
	layout bindHostLayout,
	systemctl string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
	binding transport.ServiceMutationBinding,
) (transport.SwitchDNSEngineV1Response, error) {
	state, stateExists, err := readDNSEngineState()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if !adoptableRunningBINDManifest(manifest, stateExists) {
		return transport.SwitchDNSEngineV1Response{},
			errors.New("BIND adoption received a manifest that is not the takeover's")
	}
	if _, exists, journalErr := readDNSEngineSwitchJournal(); journalErr != nil || exists {
		if journalErr == nil {
			journalErr = errors.New(
				"a DNS engine adoption journal requires reconciliation",
			)
		}
		return transport.SwitchDNSEngineV1Response{}, journalErr
	}
	if err := verifyRunningBINDAdoptionSource(
		ctx, systemctl, manifest, stateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := verifyBINDMaskParentMetadata(); err != nil {
		return transport.SwitchDNSEngineV1Response{}, fmt.Errorf(
			"preflight BIND mask parent: %w", err,
		)
	}
	// Adoption installs nothing. A missing package is not this operation.
	//
	// Devralma hiçbir şey kurmaz. Eksik bir paket bu işlem değildir.
	for _, packageName := range layout.Packages {
		installed, packageErr := exactDNSEnginePackageInstalled(
			ctx, profile, packageName,
		)
		if packageErr != nil {
			return transport.SwitchDNSEngineV1Response{}, packageErr
		}
		if !installed {
			return transport.SwitchDNSEngineV1Response{}, fmt.Errorf(
				"BIND adoption requires %s to be installed already", packageName,
			)
		}
	}
	targetBefore, err := captureDNSUnitStates(
		ctx, systemctl, []string{"bind9.service", "named.service"},
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := enableAdoptedBINDUnitIfNeeded(
		ctx, systemctl, layout.Unit, targetBefore[layout.Unit],
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	evidence, err := captureBINDAdoptionRuntimeEvidence(ctx, profile, systemctl)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	configs, err := prepareBINDAdoptionConfigMutation(ctx, layout, "")
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := requireForeignBINDConfiguration(layout, configs); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	foreignZones, err := foreignBINDConfigurationZones(layout, configs)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if collisions := foreignBINDZoneCollisions(
		foreignZones, manifest,
	); len(collisions) != 0 {
		return transport.SwitchDNSEngineV1Response{}, fmt.Errorf(
			"this DNS server already declares %s, which CelikPanel also publishes; "+
				"remove those zone declarations from its own configuration before adopting it",
			strings.Join(collisions, ", "),
		)
	}
	primaryCatalogSerial, err := primaryCatalogSerialFromSource(
		ctx, profile, manifest, state, stateExists,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	plan, err := bindSwitchTreePlanWithPrimaryCatalogSerial(
		manifest, binding, primaryCatalogSerial,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	if err := publishDNSEngineSourceOwnership(
		manifest, state, stateExists,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	// The packages are already here; this mutation takes them under management
	// without installing anything, and that is the AdoptedPresent provenance
	// the stopped shape lands on too.
	//
	// Paketler zaten burada; bu mutasyon hiçbir şey kurmadan onları yönetimine
	// alır ve bu, durmuş biçimin de indiği AdoptedPresent kökenidir.
	if err := assumeExistingDNSEnginePackageOwnership(
		transport.DNSEngineBIND, profile.PackageManager,
		layout.Packages, manifest, binding,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	var publisher *binddns.Publisher
	var validator trackedBINDValidator
	var generation binddns.Generation
	if err := mutateBINDAdoptionAfterProof(
		ctx, profile, systemctl, configs, evidence,
		func() error {
			return runBINDMutationWithMaskParentProof(
				verifyBINDMaskParentMetadata,
				func() error {
					if err := prepareHostBINDGenerationRoot(ctx, layout); err != nil {
						return err
					}
					var stageErr error
					publisher, validator, stageErr = newHostBINDPublisher(ctx, layout)
					if stageErr != nil {
						return stageErr
					}
					generation, stageErr = publisher.StagePlan(ctx, plan)
					return stageErr
				},
			)
		},
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	stateBefore, err := captureDNSEngineStateSnapshot(true)
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	previousGeneration, hadPrevious, err := publisher.Current()
	if err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal := dnsEngineSwitchJournal{
		Schema: dnsEngineSwitchJournalSchema, Phase: dnsSwitchPhaseIntent,
		Mode:              manifest.Mode,
		MutationRequestID: binding.MutationRequestID,
		MutationOwnerID:   binding.MutationOwnerID,
		ManifestQualifier: manifest.Qualifier, SourceEngine: manifest.SourceEngine,
		TargetEngine: manifest.TargetEngine, SourceEpoch: manifest.SourceEpoch,
		TargetEpoch: manifest.TargetEpoch, SourceRevision: manifest.SourceRevision,
		Topology: manifest.Topology,
		PairRole: manifest.PairRole, LocalIP: manifest.LocalIP,
		LocalNS: manifest.LocalNS, PeerIP: manifest.PeerIP, PeerNS: manifest.PeerNS,
		PrimaryCatalogSerial: primaryCatalogSerial,
		SnapshotBytes:        manifest.SnapshotBytes, Zones: manifest.Zones,
		TargetGeneration: generation.ID, PreviousGeneration: previousGeneration,
		HadPrevious: hadPrevious, StateBefore: stateBefore,
		ConfigBefore:      bindConfigMutationSnapshots(configs),
		TargetUnitsBefore: dnsUnitStateMapSnapshots(targetBefore),
		SourceUnitsBefore: []dnsUnitSnapshot{},
	}
	if err := validateDNSEngineSwitchJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	writeJournal := func(journal dnsEngineSwitchJournal) error {
		return writeDNSEngineSwitchJournalForFaultDriver(
			dnsEngineSwitchFaultDriverBIND, journal,
		)
	}
	if err := runDNSEngineSwitchPreIntentFaultHook(
		dnsEngineSwitchFaultDriverBIND, manifest, binding,
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	// The intent journal is written first and under the same proof as every
	// other mutation, so a crash from here on has a durable record naming the
	// exact configuration to put back - and the server is still answering,
	// which is why there is nothing else to recover.
	//
	// Niyet günlüğü önce ve diğer her mutasyonla aynı kanıt altında yazılır;
	// böylece buradan sonraki bir çökmenin, geri konacak yapılandırmayı
	// adlandıran kalıcı bir kaydı olur - ve sunucu hâlâ yanıt veriyordur, zaten
	// bu yüzden kurtarılacak başka bir şey yoktur.
	if err := mutateBINDAdoptionAfterProof(
		ctx, profile, systemctl, configs, evidence,
		func() error { return writeJournal(journal) },
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	rollbackAndJournal := func(rollbackCtx context.Context) error {
		return runBINDRollbackWithJournal(&journal, bindSwitchRollbackJournalOps{
			write: writeJournal,
			rollback: func() error {
				return rollbackRunningBINDAdoption(
					rollbackCtx, profile, systemctl, configs, evidence,
					stateBefore, targetBefore,
				)
			},
			verify: func() error {
				return verifyRestoredRunningBINDAdoption(
					rollbackCtx, profile, systemctl, configs, evidence,
				)
			},
			remove: removeDNSEngineSwitchJournal,
		})
	}
	attempt := 0
	apply := func(applyCtx context.Context) error {
		attempt++
		if attempt > 1 {
			return rollbackAndJournal(applyCtx)
		}
		if err := mutateBINDAdoptionAfterProof(
			applyCtx, profile, systemctl, configs, evidence,
			func() error {
				return runBINDMutationWithMaskParentProof(
					verifyBINDMaskParentMetadata,
					func() error { return configs.apply(applyCtx) },
				)
			},
		); err != nil {
			return err
		}
		// Validation runs while the server is still serving the configuration
		// it had. A refusal here - a zone declared twice behind an include this
		// adoption could not read, a syntax error in the generated tree - costs
		// nothing but a restore, because BIND has not been asked to re-read
		// anything yet.
		//
		// Doğrulama, sunucu hâlâ sahip olduğu yapılandırmayı sunarken koşar.
		// Buradaki bir ret - bu devralmanın okuyamadığı bir include'un
		// arkasında iki kez bildirilmiş bir bölge, üretilen ağaçta bir sözdizim
		// hatası - bir geri yüklemeden başka hiçbir şeye mal olmaz, çünkü
		// BIND'den henüz hiçbir şeyi yeniden okuması istenmedi.
		if _, err := runTrackedBINDValidation(
			applyCtx, validator.checkConf, "named-checkconf",
			"-z", layout.MainConfig,
		); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseTargetStaged
		if err := writeJournal(journal); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseSourceStopped
		if err := writeJournal(journal); err != nil {
			return err
		}
		if err := reloadAdoptedBIND(
			applyCtx, systemctl, layout.Unit, evidence.topology.namedProcesses,
		); err != nil {
			return err
		}
		journal.Phase = dnsSwitchPhaseTargetStarted
		if err := writeJournal(journal); err != nil {
			return err
		}
		if err := evidence.verify(applyCtx, profile, systemctl); err != nil {
			return err
		}
		if err := verifyDNSZoneManifestAuthority(applyCtx, manifest.Zones); err != nil {
			return err
		}
		currentTree, err := publisher.LoadCurrent()
		if err != nil {
			return err
		}
		if err := verifyBINDPairingAuthority(
			applyCtx, currentTree.CurrentReceipt(),
		); err != nil {
			return err
		}
		nextState := dnsEngineStateReceipt{
			Schema: dnsEngineStateSchema,
			Mode:   dnsEngineTenureModeForManifest(manifest),
			Engine: transport.DNSEngineBIND, EngineEpoch: manifest.TargetEpoch,
			Generation: generation.ID, PairRole: pairRoleForEngineState(manifest),
			PairLocalIP: manifest.LocalIP, PairPeerIP: manifest.PeerIP,
			PrimaryCatalogSerial: primaryCatalogSerial,
			SourceRevision:       manifest.SourceRevision,
			ManifestQualifier:    manifest.Qualifier,
			MutationRequestID:    binding.MutationRequestID,
			MutationOwnerID:      binding.MutationOwnerID,
		}
		if err := verifyCompletedPrimaryCatalogTarget(
			applyCtx, profile, manifest, nextState,
		); err != nil {
			return err
		}
		if err := persistExactDNSEngineState(nextState); err != nil {
			return fmt.Errorf("publish active DNS engine state: %w", err)
		}
		journal.Phase = dnsSwitchPhaseTargetVerified
		return writeJournal(journal)
	}
	recoverEmpty := func(recoveryCtx context.Context) error {
		return rollbackAndJournal(recoveryCtx)
	}
	if err := runBINDMutationWithMaskParentProof(
		verifyBINDMaskParentMetadata,
		func() error {
			return publisher.Switch(ctx, generation.ID, apply, recoverEmpty)
		},
	); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	completed, exists, err := readDNSEngineState()
	if err != nil || !exists || completed.Generation != generation.ID ||
		completed.Engine != transport.DNSEngineBIND ||
		completed.EngineEpoch != manifest.TargetEpoch {
		if err == nil {
			err = errors.New(
				"active DNS engine receipt does not match the adopted BIND generation",
			)
		}
		return transport.SwitchDNSEngineV1Response{}, err
	}
	journal.Phase = dnsSwitchPhaseCommitted
	if err := writeJournal(journal); err != nil {
		return transport.SwitchDNSEngineV1Response{}, err
	}
	return transport.SwitchDNSEngineV1Response{
		Applied: true, ActiveEngine: transport.DNSEngineBIND,
		ActiveEpoch: manifest.TargetEpoch, AppliedZones: len(manifest.Zones),
		Detail: "the running unmanaged BIND was adopted in place without an interruption",
	}, nil
}
