package transport

type DNSEngine string

const (
	DNSEnginePowerDNS DNSEngine = "pdns"
	DNSEngineBIND     DNSEngine = "bind"

	DNSTopologyStandalone = "standalone"
	DNSTopologyPaired     = "paired"
	DNSPairRolePrimary    = "primary"
	DNSPairRoleSecondary  = "secondary"

	DNSEngineSwitchModeSwitch = "switch"
	DNSEngineSwitchModeAdopt  = "adopt"

	// DNSEngineSwitchModeReinstall reinstalls the engine that already owns the
	// host, at the epoch it already owns. It is not a switch: no authority
	// changes hands, no epoch advances, and there is no source to stop. It
	// exists for the one host shape a switch cannot express — the ledger says
	// an engine is active and the machine has no copy of it, which is what a
	// restored control plane looks like on a fresh server.
	//
	// DNSEngineSwitchModeReinstall, sunucunun sahibi olan motoru zaten sahip
	// olduğu çağda yeniden kurar. Bu bir geçiş değildir: yetki el değiştirmez,
	// çağ ilerlemez ve durdurulacak bir kaynak yoktur. Geçişin ifade
	// edemediği tek sunucu biçimi için vardır — defter bir motorun etkin
	// olduğunu söylerken makinede o motorun kopyası yoktur; geri yüklenmiş bir
	// kontrol düzleminin taze bir sunucuda göründüğü hâl budur.
	DNSEngineSwitchModeReinstall = "reinstall"

	DNSEngineSwitchPhasePlanned     = "planned"
	DNSEngineSwitchPhaseStaging     = "staging"
	DNSEngineSwitchPhaseStaged      = "staged"
	DNSEngineSwitchPhaseActivating  = "activating"
	DNSEngineSwitchPhaseVerifying   = "verifying"
	DNSEngineSwitchPhaseCommitted   = "committed"
	DNSEngineSwitchPhaseRollingBack = "rolling_back"
	DNSEngineSwitchPhaseRolledBack  = "rolled_back"
	DNSEngineSwitchPhaseFailed      = "failed"
)

func ValidDNSEngine(value DNSEngine) bool {
	return value == DNSEnginePowerDNS || value == DNSEngineBIND
}

// DNSClusterRequest configures whether one PowerDNS node serves zones alone
// or exchanges zones with one peer.
type DNSClusterRequest struct {
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

type DNSClusterResponse struct {
	Applied bool   `json:"applied"`
	Detail  string `json:"detail,omitempty"`
	Error   string `json:"error,omitempty"`
}

type ConfigureDNSClusterV2Request struct {
	ServiceMutationBinding
	Role   string `json:"role"`
	PeerIP string `json:"peer_ip"`
	PeerNS string `json:"peer_ns"`
}

type ConfigureDNSClusterV2Response = DNSClusterResponse

type DNSClusterReadinessResponse struct {
	Ready  bool   `json:"ready"`
	Detail string `json:"detail,omitempty"`
}

type ZoneRecord struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Content  string `json:"content"`
	TTL      int    `json:"ttl"`
	Prio     int    `json:"prio"`
	Disabled bool   `json:"disabled"`
}

type SyncDNSZoneRequest struct {
	ServiceMutationBinding
	DesiredGeneration int64        `json:"desired_generation"`
	Domain            string       `json:"domain"`
	Delete            bool         `json:"delete"`
	ZoneType          string       `json:"zone_type"`
	Records           []ZoneRecord `json:"records"`
}

type SyncDNSZoneResponse struct {
	Synced            bool   `json:"synced"`
	AppliedGeneration int64  `json:"applied_generation"`
	Error             string `json:"error,omitempty"`
}

// V2 binds the complete effective full-zone snapshot and its generation into
// the surrounding durable service-mutation qualifier. Keeping the wire shape
// shared with V1 makes mixed binaries fail by RPC method name instead of
// silently dropping a security-critical field during gob decoding.
type SyncDNSZoneV2Request = SyncDNSZoneRequest
type SyncDNSZoneV2Response = SyncDNSZoneResponse

// SyncDNSZoneV3 binds an exact publication engine and its monotonic activation
// epoch in addition to every V2 full-zone field. V1/V2 intentionally remain
// PowerDNS-only and retain their original wire shape and qualifier.
type SyncDNSZoneV3Request struct {
	ServiceMutationBinding
	Engine            DNSEngine    `json:"engine"`
	EngineEpoch       int64        `json:"engine_epoch"`
	DesiredGeneration int64        `json:"desired_generation"`
	Domain            string       `json:"domain"`
	Delete            bool         `json:"delete"`
	ZoneType          string       `json:"zone_type"`
	Records           []ZoneRecord `json:"records"`
}

type SyncDNSZoneV3Response struct {
	Synced            bool      `json:"synced"`
	RecoveryPending   bool      `json:"recovery_pending,omitempty"`
	Engine            DNSEngine `json:"engine"`
	EngineEpoch       int64     `json:"engine_epoch"`
	AppliedGeneration int64     `json:"applied_generation"`
	Error             string    `json:"error,omitempty"`
}

// RecoverDNSZoneV3 re-drives peer propagation from the immutable host receipt
// of an exact engine-bound publication. It never accepts a replacement zone
// snapshot: the original request/owner/domain/qualifier remain the authority.
type RecoverDNSZoneV3Request struct {
	ServiceMutationBinding
	Domain    string `json:"domain"`
	Qualifier string `json:"qualifier"`
}

type RecoverDNSZoneV3Response struct {
	Recovered       bool   `json:"recovered"`
	RecoveryPending bool   `json:"recovery_pending,omitempty"`
	Error           string `json:"error,omitempty"`
}

// DNSEngineSwitchZoneSnapshot is one canonical full-zone member of a durable
// engine-switch manifest. ZoneQualifier is the target-engine V3 commitment.
type DNSEngineSwitchZoneSnapshot struct {
	Ordinal           int          `json:"ordinal"`
	Domain            string       `json:"domain"`
	DesiredGeneration int64        `json:"desired_generation"`
	Delete            bool         `json:"delete"`
	ZoneType          string       `json:"zone_type"`
	Records           []ZoneRecord `json:"records"`
	ZoneQualifier     string       `json:"zone_qualifier"`
}

// SwitchDNSEngineV1Request carries the exact snapshot authorized by the panel.
// SourceEngine is empty only while resolving a legacy/uninitialized host.
type SwitchDNSEngineV1Request struct {
	ServiceMutationBinding
	Mode              string                        `json:"mode"`
	SourceEngine      DNSEngine                     `json:"source_engine,omitempty"`
	TargetEngine      DNSEngine                     `json:"target_engine"`
	SourceEpoch       int64                         `json:"source_epoch"`
	TargetEpoch       int64                         `json:"target_epoch"`
	SourceRevision    int64                         `json:"source_revision"`
	Topology          string                        `json:"topology"`
	PairRole          string                        `json:"pair_role,omitempty"`
	LocalIP           string                        `json:"local_ip,omitempty"`
	LocalNS           string                        `json:"local_ns,omitempty"`
	PeerIP            string                        `json:"peer_ip,omitempty"`
	PeerNS            string                        `json:"peer_ns,omitempty"`
	Zones             []DNSEngineSwitchZoneSnapshot `json:"zones"`
	SnapshotBytes     int64                         `json:"snapshot_bytes"`
	ManifestQualifier string                        `json:"manifest_qualifier"`
}

type SwitchDNSEngineV1Response struct {
	Applied      bool      `json:"applied"`
	ActiveEngine DNSEngine `json:"active_engine,omitempty"`
	ActiveEpoch  int64     `json:"active_epoch,omitempty"`
	AppliedZones int       `json:"applied_zones,omitempty"`
	Detail       string    `json:"detail,omitempty"`
	Error        string    `json:"error,omitempty"`
}

// DNSEngineRollbackEvidenceRequest replays the frozen canonical switch
// manifest as comparison-only input. It grants no mutation lease and accepts
// no host path or caller-selected recovery action. The response contains only
// a bounded outcome and a fixed-size terminal-receipt commitment.
type DNSEngineRollbackEvidenceRequest SwitchDNSEngineV1Request

type DNSEngineRollbackEvidenceResponse struct {
	Outcome           string `json:"outcome"`
	ReceiptCommitment string `json:"receipt_commitment,omitempty"`
}

const (
	DNSEngineRollbackSafe                     = "rollback_safe"
	DNSEngineRollbackActiveOperation          = "active_operation"
	DNSEngineRollbackIdentityMismatch         = "identity_mismatch"
	DNSEngineRollbackJournalPresent           = "journal_present"
	DNSEngineRollbackCommittedEvidence        = "committed_evidence"
	DNSEngineRollbackInstallOwnershipMismatch = "install_ownership_mismatch"
	DNSEngineRollbackRuntimeUnsealed          = "runtime_unsealed"
	DNSEngineRollbackUnverified               = "unverified"
)

// DNSBackendReadinessResponse reports only bounded runtime facts. Detailed
// probe failures stay in agent logs and are not exposed as raw API errors.
type DNSBackendRuntimeState struct {
	Engine    DNSEngine `json:"engine"`
	Installed bool      `json:"installed"`
	Running   bool      `json:"running"`
	Managed   bool      `json:"managed"`
	PairReady bool      `json:"pair_ready"`
	Unit      string    `json:"unit"`
	// ForeignOptions names the directives CelikPanel manages that this server's
	// own configuration already sets, with the value it has today and the value
	// CelikPanel would set. It is a bounded runtime fact like the others, and it
	// exists so a takeover can show the operator the difference before the
	// operator agrees to it (register R-042). It is empty for an engine the
	// panel manages, and for one there is nothing to take over from.
	//
	// ForeignOptions, CelikPanel'in yönettiği ve bu sunucunun kendi
	// yapılandırmasının zaten koyduğu direktifleri, bugünkü değerleri ve
	// CelikPanel'in koyacağı değerle birlikte adlandırır. Diğerleri gibi sınırlı
	// bir çalışma zamanı olgusudur ve bir devralmanın, operatör rıza göstermeden
	// önce farkı ona gösterebilmesi için vardır (defter R-042). Panelin
	// yönettiği bir motor için ve devralınacak bir şey olmayan bir motor için
	// boştur.
	ForeignOptions []DNSForeignEngineOption `json:"foreign_options,omitempty"`
	// ForeignViews says whether this server's own BIND configuration declares
	// views, and where the first one is written - or that a file it includes
	// could not be read, which is the same answer for the operator: CelikPanel
	// will not take over a configuration it cannot read completely. It is nil
	// when the configuration was read whole and declares none (register R-044).
	//
	// ForeignViews, bu sunucunun kendi BIND yapılandırmasının view bildirip
	// bildirmediğini ve ilkinin nerede yazılı olduğunu söyler - ya da dahil
	// ettiği bir dosyanın okunamadığını; ki operatör için bu aynı cevaptır:
	// CelikPanel, bütünüyle okuyamadığı bir yapılandırmayı devralmaz.
	// Yapılandırma bütünüyle okundu ve hiçbir view bildirmiyorsa nil'dir
	// (defter R-044).
	ForeignViews *DNSForeignEngineViews `json:"foreign_views,omitempty"`
}

// The vocabulary of a takeover's difference list. The agent reads these
// directives out of the host's configuration, the panel puts them in the
// preview, and the browser has copy for every one of them in both locales. One
// list, three layers, and a contract test that fails the build when they drift
// - the lesson of R-041, where an API could return something the browser could
// not render.
//
// Bir devralmanın fark listesinin söz varlığı. Agent bu direktifleri
// sunucunun yapılandırmasından okur, panel onları önizlemeye koyar ve tarayıcı
// her biri için iki dilde de metne sahiptir. Tek liste, üç katman ve
// birbirlerinden ayrıldıklarında derlemeyi düşüren bir sözleşme testi - API'nin
// tarayıcının çizemediği bir şey döndürebildiği R-041'in dersi.
var DNSManagedBINDOptionDirectives = []string{
	"recursion", "allow-recursion", "allow-query-cache", "allow-transfer",
}

const (
	// DNSForeignOptionNestedScope: the directive sits inside a block within the
	// options block, where CelikPanel's own block does not govern it.
	DNSForeignOptionNestedScope = "nested_scope"
	// DNSForeignOptionNotAStatement: the name appears where the reader cannot
	// prove it is a directive of its own.
	DNSForeignOptionNotAStatement = "not_a_statement"
	// DNSForeignOptionUnterminated: no terminating semicolon can be found.
	DNSForeignOptionUnterminated = "unterminated"
)

// DNSForeignOptionRefusals is every reason a directive cannot be taken over.
//
// DNSForeignOptionRefusals, bir direktifin neden devralınamadığının her
// sebebidir.
var DNSForeignOptionRefusals = []string{
	DNSForeignOptionNestedScope,
	DNSForeignOptionNotAStatement,
	DNSForeignOptionUnterminated,
}

// DNSForeignEngineOption is one directive of the operator's that a takeover
// would replace: what it is, what it says now, what CelikPanel makes it say,
// and where it is written. Refusal is empty when the takeover can replace it,
// and a machine code naming why when it cannot - never operator text, because
// the panel and the browser own the wording.
//
// Found is the operator's own configuration text, normalised to one bounded
// printable line by the agent. It is displayed and never interpreted.
//
// DNSForeignEngineOption, bir devralmanın değiştireceği, operatöre ait tek bir
// direktiftir: ne olduğu, şu anda ne dediği, CelikPanel'in ne dedirteceği ve
// nerede yazılı olduğu. Devralma onu değiştirebiliyorsa Refusal boştur;
// değiştiremiyorsa nedenini adlandıran bir makine kodudur - asla operatör metni
// değil, çünkü sözcükler panelin ve tarayıcınındır.
//
// Found, operatörün kendi yapılandırma metnidir; agent tarafından tek, sınırlı ve
// yazdırılabilir bir satıra normalleştirilir. Gösterilir, asla yorumlanmaz.
type DNSForeignEngineOption struct {
	Directive   string `json:"directive"`
	Found       string `json:"found"`
	Replacement string `json:"replacement"`
	File        string `json:"file"`
	Line        int    `json:"line"`
	Refusal     string `json:"refusal,omitempty"`
}

// What a takeover found out about this server's view configuration
// (register R-044).
//
// BIND lets the same directives CelikPanel manages live inside a `view`, where
// they take precedence over the options block, and it requires every zone to
// sit inside a view once any view exists. A takeover that read only the options
// block would therefore report a recursion setting it does not actually
// control, and would have its generated zones refused at the very last moment,
// by the configuration check, after the work. Neither is honest. So the host
// looks for views before the preview exists, and a server that has them is
// refused by name, on the screen the operator is standing on.
//
// Bir devralmanın bu sunucunun view yapılandırması hakkında öğrendiği
// (defter R-044).
//
// BIND, CelikPanel'in yönettiği direktiflerin bir `view` içinde yaşamasına izin
// verir; orada seçeneklere üstün gelirler ve bir view var olduğu anda her
// bölgenin bir view içinde olmasını şart koşar. Yalnız seçenek bloğunu okuyan
// bir devralma bu yüzden gerçekte denetlemediği bir recursion ayarını bildirir
// ve ürettiği bölgeler en son anda, yapılandırma denetiminde, iş bittikten
// sonra reddedilir. İkisi de dürüst değildir. Bu yüzden sunucu, daha önizleme
// yokken view arar ve view'i olan bir sunucu, operatörün üzerinde durduğu
// ekranda adıyla reddedilir.
const (
	// DNSForeignViewDeclared: the configuration declares at least one view.
	DNSForeignViewDeclared = "declared"
	// DNSForeignViewUnreadable: a file the configuration includes could not be
	// read or followed, so the absence of views cannot be proved.
	DNSForeignViewUnreadable = "unreadable"
)

// DNSForeignViewFindings is every view finding the agent can report. It is a
// pinned list for the reason DNSForeignOptionRefusals is: the browser has copy
// for each one in both locales, and a contract test fails the build when the
// two drift (the lesson of R-041).
//
// DNSForeignViewFindings, agent'ın bildirebileceği her view bulgusudur.
// DNSForeignOptionRefusals ile aynı sebeple sabitlenmiş bir listedir:
// tarayıcının her biri için iki dilde metni vardır ve ikisi ayrıldığında bir
// sözleşme testi derlemeyi düşürür (R-041'in dersi).
var DNSForeignViewFindings = []string{
	DNSForeignViewDeclared,
	DNSForeignViewUnreadable,
}

// DNSForeignEngineViews is where to look. For a declared view it is the file
// and line of the `view` keyword itself; for an unreadable one it is the file
// and line of the `include` statement CelikPanel could not follow, because that
// is the statement the operator has to act on.
//
// DNSForeignEngineViews, nereye bakılacağıdır. Bildirilmiş bir view için
// `view` sözcüğünün dosyası ve satırı; okunamayan biri için CelikPanel'in
// izleyemediği `include` deyiminin dosyası ve satırıdır; çünkü operatörün
// üzerinde işlem yapacağı deyim odur.
type DNSForeignEngineViews struct {
	Finding string `json:"finding"`
	File    string `json:"file"`
	Line    int    `json:"line"`
}

// MutationHold codes. Empty means the agent is accepting durable mutations.
// These are stable machine codes, never operator text and never an internal
// error string: the panel maps them to its own wording.
// MutationHold kodları. Boş değer, agent'ın kalıcı mutasyonları kabul ettiğini
// bildirir. Bunlar kararlı makine kodlarıdır; asla operatör metni ya da bir iç
// hata dizesi değildir — panel bunları kendi ifadesine eşler.
const (
	// MutationHoldLedgerUnavailable: the durable mutation ledger could not be
	// brought up at all, so nothing can be recorded and nothing may run.
	MutationHoldLedgerUnavailable = "ledger_unavailable"
	// MutationHoldLedgerAmbiguous: a ledger write may or may not have been
	// published, so the agent refuses every further mutation rather than build
	// on a state it cannot prove. This is the state a half-finished DNS engine
	// handover leaves behind.
	MutationHoldLedgerAmbiguous = "ledger_ambiguous"
)

type DNSBackendReadinessResponse struct {
	Engines        []DNSBackendRuntimeState `json:"engines"`
	Port53Conflict bool                     `json:"port_53_conflict"`
	// MutationHold names why the agent is refusing durable mutations, or is
	// empty when it is accepting them.
	//
	// It sits on the response rather than on each engine because the hold is
	// currently process-wide: one ledger and one poison field cover every
	// mutation kind (vpn_peer_sync, firewall_apply, mail_tls_sync,
	// panel_certificate_issue, dns_*), so an ambiguous write to that shared
	// ledger cannot be attributed to one engine. Claiming per-engine isolation
	// here would promise a containment the agent does not have. Narrowing the
	// hold to a single slot is D-021 work and depends on the ledger structure,
	// not on this field.
	//
	// Its purpose is to stop the panel misreporting a stuck transaction as a
	// foreign DNS server: "the panel's own change system is held" and "someone
	// else installed a DNS server" are opposite diagnoses with opposite fixes.
	//
	// MutationHold, agent'ın kalıcı mutasyonları neden reddettiğini adlandırır;
	// kabul ediyorsa boştur.
	//
	// Motor başına değil yanıt üzerinde durur, çünkü tutma şu an süreç
	// genelindedir: tek bir defter ve tek bir zehir alanı bütün mutasyon
	// türlerini kapsar, dolayısıyla o ortak deftere yapılan belirsiz bir yazım
	// tek bir motora atfedilemez. Burada motor başına izolasyon iddia etmek,
	// agent'ın sahip olmadığı bir sınırlamayı vaat etmek olurdu. Tutmayı tek bir
	// yuvaya daraltmak D-021 işidir ve bu alana değil defterin yapısına bağlıdır.
	//
	// Amacı, panelin takılmış bir işlemi yabancı bir DNS sunucusu diye
	// bildirmesini engellemektir: "panelin kendi değişiklik sistemi tutuluyor"
	// ile "başkası bir DNS sunucusu kurmuş" zıt teşhislerdir ve zıt çözümleri
	// vardır.
	MutationHold string `json:"mutation_hold,omitempty"`
	Error        string `json:"error,omitempty"`
}

type DNSSECRequest struct {
	Zone string `json:"zone"`
}

type SecureDNSZoneV2Request struct {
	ServiceMutationBinding
	Zone string `json:"zone"`
}

type DNSSECStatusResponse struct {
	Secured bool     `json:"secured"`
	DS      []string `json:"ds,omitempty"`
	Error   string   `json:"error,omitempty"`
}

type SecureDNSZoneV2Response = DNSSECStatusResponse

type TLSARequest struct {
	CertPath string `json:"cert_path"`
}

type TLSAResponse struct {
	Content string `json:"content"`
	Error   string `json:"error,omitempty"`
}
