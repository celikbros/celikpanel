package core

// ServiceInstallEvidence names WHICH question the host is asked when the
// component scan decides whether a catalogue service is installed on it.
//
// It exists because two panel surfaces can legitimately answer differently
// about the same service at the same moment. The component scan asks systemd
// whether a unit file for the service exists; the DNS engine surface asks the
// package database (dpkg-query / pacman -Q) about one exact package. A masked
// or sealed BIND — a shape the DNS takeover workflow deliberately creates —
// is a real host state where those two answers diverge without either of them
// being wrong.
//
// The two questions are deliberately NOT merged into one. The unit answer is
// the same answer InstalledServiceIDsStrict hands to firewall policy: widening
// it to "the distro package is present" would open :53 on a host where systemd
// knows no DNS unit at all, and open every other service's ports on the same
// reasoning. That is a security change, and it must not travel disguised as an
// honesty fix. So the panel keeps the questions distinct and makes each answer
// say which question produced it — `installed_evidence` on the wire.
//
// ServiceInstallEvidence, bileşen taraması bir katalog servisinin bu makinede
// kurulu olup olmadığına karar verirken makineye HANGİ soruyu sorduğunu
// adlandırır. İki panel yüzeyi aynı servis için aynı anda meşru biçimde farklı
// yanıt verebilir: bileşen taraması systemd'ye unit dosyası var mı diye sorar,
// DNS motoru yüzeyi ise paket veritabanına tam bir paketi sorar. Maskeli ya da
// mühürlenmiş BIND — DNS devralma akışının bilerek ürettiği bir biçim — bu iki
// yanıtın ikisi de yanlış olmadan ayrıştığı gerçek bir makine durumudur.
//
// İki soru bilerek birleştirilmez: unit yanıtı, InstalledServiceIDsStrict'in
// güvenlik duvarı politikasına verdiği yanıtla aynıdır; onu "paket var" diye
// genişletmek, systemd'nin hiçbir DNS unit'i tanımadığı bir makinede :53'ü
// açardı. Bu bir güvenlik değişikliğidir ve dürüstlük düzeltmesi kılığında
// yolculuk edemez. Panel bu yüzden soruları ayrı tutar ve her yanıtın hangi
// sorudan geldiğini söyler — telde `installed_evidence`.
type ServiceInstallEvidence string

const (
	// EvidenceNone: this host cannot be asked about this service at all —
	// the catalogue entry has no unit names and no package mapping for this
	// package family, so the scan never reports it installed.
	// EvidenceNone: bu makineye bu servis hiç sorulamaz.
	EvidenceNone ServiceInstallEvidence = ""
	// EvidenceSystemdUnit: systemd was asked whether a unit file exists.
	// EvidenceSystemdUnit: systemd'ye unit dosyası var mı diye soruldu.
	EvidenceSystemdUnit ServiceInstallEvidence = "systemd_unit"
	// EvidencePackage: the package database was asked about this family's
	// packages for the service.
	// EvidencePackage: paket veritabanına bu ailenin paketleri soruldu.
	EvidencePackage ServiceInstallEvidence = "package"
	// EvidenceApplicationFiles: the application's own files on disk were
	// inspected (Roundcube installs as a web application, not as a unit or a
	// distro package).
	// EvidenceApplicationFiles: uygulamanın diskteki kendi dosyalarına
	// bakıldı (Roundcube bir web uygulaması olarak kurulur).
	EvidenceApplicationFiles ServiceInstallEvidence = "application_files"
)

// RoundcubeServiceID is the one catalogue entry whose installedness is decided
// from application files rather than from systemd or a package. It is named
// here so the agent's discovery and the panel's payload cannot disagree about
// which question was asked.
// RoundcubeServiceID, kurulu-oluşu systemd ya da pakete değil uygulama
// dosyalarına bakılarak belirlenen tek katalog kalemidir.
const RoundcubeServiceID = "roundcube"

// InstalledEvidenceFor reports which question discoverInstalledServiceIDsStrict
// (cmd/agent/service_state_rpc.go) will ask about this service on a host of
// this package family. The agent switches on exactly this value, so the label
// the panel puts on the wire is the label of the probe that actually ran.
// InstalledEvidenceFor, agent'ın bu servis için hangi soruyu soracağını
// bildirir. Agent tam bu değere göre dallanır; böylece panelin tele koyduğu
// etiket, gerçekten koşan yoklamanın etiketidir.
func InstalledEvidenceFor(service *ManagedService, packageFamily string) ServiceInstallEvidence {
	if service == nil {
		return EvidenceNone
	}
	switch {
	case service.ID == RoundcubeServiceID:
		return EvidenceApplicationFiles
	case len(service.SystemNames) > 0 || service.SystemNamePattern != "":
		return EvidenceSystemdUnit
	case len(service.Packages[packageFamily]) > 0:
		return EvidencePackage
	default:
		return EvidenceNone
	}
}
