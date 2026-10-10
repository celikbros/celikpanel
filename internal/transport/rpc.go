package transport

import "time"

// The concrete RPC request and response structs in this package are the
// protocol contract. Both panel and agent import these exact types so a field
// rename or removal fails at compile time instead of being silently ignored by
// net/rpc's gob decoder. Do not replace this with a hand-written method
// interface: the RPC surface is registered by name and such an interface can
// drift without protecting the wire format.

// Service configuration requests/responses

type GetPHPConfigRequest struct {
	PHPVersion string `json:"php_version"` // e.g., "8.3"
}

type GetPHPConfigResponse struct {
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  int    `json:"max_execution_time"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	PostMaxSize       string `json:"post_max_size"`
	MaxInputVars      int    `json:"max_input_vars"`
}

type UpdatePHPConfigRequest struct {
	PHPVersion        string `json:"php_version"`
	MemoryLimit       string `json:"memory_limit"`
	MaxExecutionTime  int    `json:"max_execution_time"`
	UploadMaxFilesize string `json:"upload_max_filesize"`
	PostMaxSize       string `json:"post_max_size"`
	MaxInputVars      int    `json:"max_input_vars"`
}

type GetMySQLConfigResponse struct {
	MaxConnections   int    `json:"max_connections"`
	InnodbBufferPool string `json:"innodb_buffer_pool_size"`
	QueryCacheSize   string `json:"query_cache_size"`
	MaxAllowedPacket string `json:"max_allowed_packet"`
}

type UpdateMySQLConfigRequest struct {
	MaxConnections   int    `json:"max_connections"`
	InnodbBufferPool string `json:"innodb_buffer_pool_size"`
	QueryCacheSize   string `json:"query_cache_size"`
	MaxAllowedPacket string `json:"max_allowed_packet"`
}

// Database management RPC types

type CreateDatabaseRequest struct {
	Type         string `json:"type"` // "mysql" or "postgresql"
	Name         string `json:"name"`
	User         string `json:"user"`
	Password     string `json:"password"`
	OperationID  string `json:"operation_id,omitempty"`
	CleanupToken string `json:"cleanup_token,omitempty"`
}

type CreateDatabaseResponse struct {
	Success           bool   `json:"success"`
	OwnedByOperation  bool   `json:"owned_by_operation,omitempty"`
	CleanupIncomplete bool   `json:"cleanup_incomplete,omitempty"`
	Error             string `json:"error,omitempty"`
}

type DeleteDatabaseRequest struct {
	Type                  string `json:"type"` // "mysql" or "postgresql"
	Name                  string `json:"name"`
	User                  string `json:"user"`
	RequireUserCleanup    bool   `json:"require_user_cleanup,omitempty"`
	RequireOwnershipProof bool   `json:"require_ownership_proof,omitempty"`
	OperationID           string `json:"operation_id,omitempty"`
	CleanupToken          string `json:"cleanup_token,omitempty"`
}

type DeleteDatabaseResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type ChangeDatabasePasswordRequest struct {
	DatabaseType string `json:"database_type"` // "postgresql" or "mariadb"
	Username     string `json:"username"`
	NewPassword  string `json:"new_password"`
}

// CreateSiteRequest contains all data needed to create a site
type CreateSiteRequest struct {
	ExpectedBuildCommit string
	SiteID              int
	SubscriptionID      int
	DomainID            int
	Domain              string
	TempDomain          string
	DocumentRoot        string
	// ProjectType decides what the agent actually builds: "php" (FPM pool +
	// index.php) or "static" (no PHP at all). Empty means "php" for backward
	// compatibility. "dnsonly" never reaches the agent — there is nothing to
	// build for it.
	// ProjectType, agent'ın gerçekte ne kuracağına karar verir: "php" (FPM
	// havuzu + index.php) ya da "static" (hiç PHP yok). Boş, geriye uyumluluk
	// için "php" demektir. "dnsonly" agent'a hiç ulaşmaz — kurulacak şey yok.
	ProjectType string
	PHPVersion  string
	SSLType     string
	Username    string
	Password    string
	// ServerNames are the site's managed host names, from the same Panel
	// function the start and every save use (D-031; set8 measured that the
	// creation render named only the domain while the next start added
	// www.<domain>). When set, the creation vhost is the start render: no
	// temporary name. Additive: an older Panel leaves it empty and the Agent
	// renders the domain alone, as before.
	ServerNames []string
}

// CreateSiteResponse contains results of site creation
type CreateSiteResponse struct {
	Success      bool
	NginxConfig  string
	PHPSocket    string
	ErrorMessage string
	// ErrorCode classifies a refusal the Panel explains (today only
	// HostingRootNotTraversable). Additive: older Agents leave it empty and
	// the Panel keeps its generic answer.
	// ErrorCode, Panel'in açıkladığı bir reddi sınıflandırır. Eklemelidir.
	ErrorCode string
	// HostingRoot names the blocking directory for HostingRootNotTraversable.
	// HostingRoot, HostingRootNotTraversable için engelleyen dizini adlandırır.
	HostingRoot *HostingRootBlock
	// ErrorDetail is one bounded line for WebServerRefusedConfig: the line of
	// nginx's own output that names what it refused. It can name paths of this
	// server, so the Panel shows it to an administrator only. Additive.
	// ErrorDetail, WebServerRefusedConfig için tek ve sınırlı bir satırdır:
	// nginx'in neyi reddettiğini adlandıran kendi satırı. Eklemelidir.
	ErrorDetail string
	// SiteFile is the vhost's classification and outcome (D-031). Additive;
	// nil from an older Agent.
	SiteFile *SiteFileResult
}

// WebServerRefusedConfig: the site's parts were created, then nginx's own test
// (`nginx -t`) refused the configuration with the site's vhost in it. The
// Agent restored the vhost to what it was (absent), had nginx accept and reload
// the previous configuration, and removed the parts it had created (12 Oct
// 2026; measured on Arch: the answer was a bare `500 INTERNAL`).
// WebServerRefusedConfig: sitenin parçaları oluşturuldu, sonra nginx'in kendi
// sınaması sitenin sanal konağını içeren yapılandırmayı reddetti. Agent sanal
// konağı eski haline getirdi ve oluşturduğu parçaları kaldırdı.
const WebServerRefusedConfig = "web_server_refused_config"

// HostingRootNotTraversable: a directory above the hosting base
// (/var/www/celikpanel) exists with a mode or owner that keeps the web server
// or the site users from reaching their files. The Agent refuses the site
// before any change and never changes that directory; the server owner
// decides (D-022, D-024; native finding P3).
// HostingRootNotTraversable: barındırma kökünün üstündeki bir dizin, web
// sunucusunun ya da site kullanıcılarının dosyalarına ulaşmasını engelleyen
// bir kip ya da sahiple var. Agent siteyi hiçbir değişiklikten önce reddeder
// ve o dizini asla değiştirmez; karar sunucu sahibinindir.
const HostingRootNotTraversable = "hosting_root_not_traversable"

// HostingRootBlock is the observed blocking directory: its path, octal mode
// ("0750") and "owner:group".
// HostingRootBlock, gözlenen engelleyici dizindir.
type HostingRootBlock struct {
	Directory string
	Mode      string
	Owner     string
	// Account is the web server account that cannot pass, or "" when the
	// site users are the ones blocked.
	Account string
}

// Data structures for RPC calls

type Empty struct{}

type GetConfigArgs struct {
	Path string
}

// ConfigErrorCode is the machine-readable failure contract for the privileged
// configuration editor. Expected operator errors travel in the RPC response
// instead of net/rpc's string-only error channel, so the panel never has to
// infer an HTTP status from human-readable text.
type ConfigErrorCode string

const (
	ConfigErrorPathRefused    ConfigErrorCode = "path_refused"
	ConfigErrorValidationFail ConfigErrorCode = "validation_failed"
	// The file could not be read, so what it holds is unknown. It is never
	// answered as an empty file (9 Oct 2026).
	// Dosya okunamadı; içeriği bilinmiyor. Asla boş dosya diye yanıtlanmaz.
	ConfigErrorUnreadable ConfigErrorCode = "unreadable"
	// A write that does not say which bytes it was built from.
	// Hangi baytlardan kurulduğunu söylemeyen yazı.
	ConfigErrorVersionRequired ConfigErrorCode = "version_required"
	// The file is no longer the one the write was built from.
	// Dosya artık yazının kurulduğu dosya değil.
	ConfigErrorChanged ConfigErrorCode = "changed"
	// The new file was installed, the service refused it or its reload failed,
	// and the previous file was put back. Reason says whether that succeeded.
	// Yeni dosya kuruldu, hizmet reddetti ya da yeniden yükleme başarısız oldu
	// ve önceki dosya geri kondu. Reason bunun başarılı olup olmadığını söyler.
	ConfigErrorReloadFailed ConfigErrorCode = "reload_failed"
)

// Reasons that refine ConfigErrorValidationFail. Nothing was written for any
// of them.
// ConfigErrorValidationFail'i incelten gerekçeler. Hiçbirinde yazı yoktur.
const (
	// The content is empty, or holds nothing but blank space.
	ConfigInvalidEmpty = "empty"
	// The content is larger than a configuration file the Panel will write, or
	// holds a NUL byte.
	ConfigInvalidShape = "shape"
	// A line the Panel's own reading of the file format does not accept.
	ConfigInvalidSyntax = "syntax"
	// The service's own program refused the file. Detail is its first line.
	ConfigInvalidDaemon = "daemon"
	// pg_hba.conf: the change would take away the local administrator access
	// PostgreSQL's own account, and with it the Panel, connects through.
	ConfigInvalidLockout = "lockout"
	// The program that validates this kind of file is not on the server, so the
	// file cannot be checked before it replaces the current one.
	ConfigInvalidNoValidator = "no_validator"
)

// Reasons that refine ConfigErrorReloadFailed.
const (
	// The previous file is back in place and the service's unit reloaded it.
	ConfigReloadRestored = "restored"
	// The previous file is back in place. The unit's reload command failed with
	// it as well, so the server itself was sent the reload signal and asked
	// what it loaded: it re-read its files after the previous one was back and
	// reports no error in them, so it runs with the settings it had before the
	// change (10 Oct 2026). Unit names the unit whose reload fails.
	// Önceki dosya yerinde. Birimin yeniden yükleme komutu onunla da başarısız
	// oldu; sunucunun kendisine soruldu ve değişiklikten önceki ayarlarla
	// çalıştığı doğrulandı.
	ConfigReloadRestoredUnitFailed = "restored_unit_reload_failed"
	// The previous file is back in place, the unit's reload failed with it as
	// well, and the server could not be asked what it loaded. Which settings it
	// runs with is unknown: a reload command that fails part-way may already
	// have made it read the new file.
	// Önceki dosya yerinde; sunucunun hangi ayarlarla çalıştığı bilinmiyor.
	ConfigReloadRestoredUnknown = "restored_running_unknown"
	// The previous file could not be put back (the file on disk is no longer
	// the one this write installed, or the write failed). Name is the kept
	// copy of the previous file.
	ConfigReloadNotRestored = "not_restored"
)

// What happened to the running service after a file was written.
// Dosya yazıldıktan sonra çalışan hizmete ne olduğu.
const (
	// The service re-read the file.
	ConfigAppliedReloaded = "reloaded"
	// The service reads this file only when it starts; it was not restarted.
	ConfigAppliedRestartRequired = "restart_required"
	// The service is not running; it will read the file when it starts.
	ConfigAppliedNotRunning = "not_running"
)

// Whether the service itself was asked about the file that is now installed.
// Kurulan dosya hakkında hizmetin kendisine sorulup sorulmadığı.
const (
	ConfigDaemonAccepted   = "accepted"
	ConfigDaemonNotChecked = "not_checked"
)

type ConfigRPCError struct {
	Code    ConfigErrorCode `json:"code"`
	Message string          `json:"message"`
	// Reason refines Code. Detail is one bounded line from the service's own
	// program (or the path of a kept backup). Line is the line of the file a
	// refusal is about, 0 when it names none. Name is the setting it names.
	// Reason, Code'u inceltir. Detail hizmetin kendi programından tek, sınırlı
	// bir satırdır. Line reddin ilgili olduğu dosya satırıdır; Name ayardır.
	Reason string `json:"reason,omitempty"`
	Detail string `json:"detail,omitempty"`
	Line   int    `json:"line,omitempty"`
	Name   string `json:"name,omitempty"`
	// Unit is the systemd unit a failed reload is about. Additive.
	// Unit, başarısız yeniden yüklemenin ilgili olduğu systemd birimidir.
	Unit string `json:"unit,omitempty"`
}

type ConfigResponse struct {
	Content string `json:"Content"`
	Parsed  string `json:"Parsed"` // JSON string
	// Version identifies the exact bytes that were read. A write must carry it
	// back.
	// Version okunan baytları tanımlar. Yazı onu geri taşımak zorundadır.
	Version string          `json:"Version,omitempty"`
	Error   *ConfigRPCError `json:"Error,omitempty"`
}

type UpdateConfigArgs struct {
	Path    string
	Content string
	// Version is the Version of the read this content was built from.
	// Version, bu içeriğin kurulduğu okumanın Version değeridir.
	Version string
}

type UpdateConfigResponse struct {
	Success bool            `json:"success"`
	Error   *ConfigRPCError `json:"error,omitempty"`
	// Version of the file as it is on the server now.
	// Dosyanın sunucudaki şimdiki hâlinin sürümü.
	Version string `json:"version,omitempty"`
	// Unchanged: the content was already what the server holds; nothing was
	// written and nothing was reloaded.
	// Unchanged: içerik zaten sunucudakiydi; hiçbir şey yazılmadı.
	Unchanged bool `json:"unchanged,omitempty"`
	// Backup is the copy of the previous file kept next to it.
	// Backup, önceki dosyanın yanında tutulan kopyasıdır.
	Backup string `json:"backup,omitempty"`
	// Applied and DaemonCheck: see the ConfigApplied* and ConfigDaemon*
	// constants. RestartRequired names settings the service will only take up
	// when it is restarted.
	Applied         string   `json:"applied,omitempty"`
	DaemonCheck     string   `json:"daemon_check,omitempty"`
	RestartRequired []string `json:"restart_required,omitempty"`
}

type ServiceArgs struct {
	ServiceName string
}

type ServiceActionArgs struct {
	ServiceName string
	Action      string
}

// ServiceActionResult answers one start, stop, restart or reload of a managed
// unit. Success and Error are what they always were. The other fields are
// additive (10 Oct 2026): an Agent older than them leaves them empty.
//
// Outcome says how the answer was established, because `systemctl`'s exit
// status is the acted unit's job result, and for a wrapper unit (Ubuntu's
// `postfix.service`, Debian's and Ubuntu's `postgresql.service`) that says
// nothing about the daemon:
//
//   - ServiceActionVerified: the daemon was observed in the requested state;
//   - ServiceActionFailed: a verified failure, Success false, Stage and Detail
//     say where and carry the service's own line;
//   - ServiceActionUnknown: the action was sent but what came of it could not
//     be established. Success is false: unknown is never reported as success;
//   - empty: the unit's own job result, as systemd reported it.
//
// ServiceActionResult, yönetilen bir unit'in başlat, durdur, yeniden başlat ya
// da yeniden yükle işlemini yanıtlar. Outcome, yanıtın nasıl kurulduğunu
// söyler: doğrulandı, doğrulanmış hata ya da bilinmiyor. Bilinmeyen sonuç asla
// başarı diye bildirilmez.
type ServiceActionResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Outcome string `json:"outcome,omitempty"`
	// Stage: "check", "reload", "start", "stop", "verify", or "command" when
	// systemd itself refused or failed the action. Since 11 Oct 2026 a reload
	// has three more: "not_running" (the daemon is stopped, so there was
	// nothing to reload), and for PostgreSQL, whose running server can be
	// asked, "reload_reread" (the unit reported a failed reload but the server
	// did re-read its files) and "reload_not_reread" (it did not). "reload"
	// alone claims neither.
	Stage string `json:"stage,omitempty"`
	// Applied: what a verified action came to ("reloaded", "started",
	// "restarted", "stopped", "running" when it was already running).
	Applied string `json:"applied,omitempty"`
	// Detail is one bounded line: what the service's own program said, or what
	// the check observed.
	Detail string `json:"detail,omitempty"`
	// Unit names the unit that owns the daemon when it is not the one acted on.
	Unit string `json:"unit,omitempty"`
	// Notice is a fact about native state that a successful action left and
	// that its success does not say (12 Oct 2026):
	// ServiceActionNoticeUnitFailed, or (9 Oct 2026)
	// ServiceActionNoticeUnitNotSettled. NoticeUnit is the unit, NoticeResult
	// systemd's `Result` for a failed unit or the last `ActiveState` read of
	// one that had not settled (empty when it could not be read), NoticeDetail
	// one bounded line of the service's own words when it has any. Additive;
	// never set on a failure.
	// Notice, başarılı bir işlemin geride bıraktığı ve başarının söylemediği
	// yerel durum bilgisidir. Eklemelidir; hata yanıtında hiç doldurulmaz.
	Notice       string `json:"notice,omitempty"`
	NoticeUnit   string `json:"notice_unit,omitempty"`
	NoticeResult string `json:"notice_result,omitempty"`
	NoticeDetail string `json:"notice_detail,omitempty"`
}

// ServiceActionNoticeUnitFailed: the service was stopped as asked and is not
// running, and systemd now shows its unit as `failed` although it was not
// before the stop: a command of the unit exited with an error while it
// stopped. Measured on Debian 13 and Ubuntu 24.04 with a main.cf Postfix
// refuses: the unit's stop command is `postfix stop`, which reads main.cf
// first. The mark is systemd's own record and is left as it is.
// ServiceActionNoticeUnitFailed: hizmet istendiği gibi durduruldu ve çalışmıyor;
// systemd birimini, durdurmadan önce öyle olmadığı halde, `failed` gösteriyor.
// İşaret systemd'nin kendi kaydıdır ve olduğu gibi bırakılır.
const ServiceActionNoticeUnitFailed = "unit_marked_failed"

// ServiceActionNoticeUnitNotSettled: the service was stopped as asked and is
// not running, and how its unit's stop ended was not read: within the bound
// the Agent waits, systemd still showed the unit between two states
// (`deactivating`), or the unit could not be read after the stop although it
// was read before it. The unit may still end marked as failed; nothing is
// claimed about it either way (9 Oct 2026).
//
// Measured on Ubuntu 24.04: `systemctl stop postfix` returns when the wrapper
// unit's job ends, 1 to 4 ms after `postfix@-.service` left `active`; that
// unit then stays `deactivating` for about one second (its stop command
// `postmulti -p stop` refuses main.cf and exits 1 after Postfix's own
// one-second pause) and is marked `failed` 2 to 7 ms after the master ends.
// ServiceActionNoticeUnitNotSettled: hizmet istendiği gibi durduruldu ve
// çalışmıyor; biriminin durdurulmasının nasıl bittiği ise okunamadı. Birim
// yine de `failed` olarak işaretlenebilir; bu konuda hiçbir şey ileri sürülmez.
const ServiceActionNoticeUnitNotSettled = "unit_not_settled"

const (
	ServiceActionVerified = "verified"
	ServiceActionFailed   = "failed"
	ServiceActionUnknown  = "unknown"
	// ServiceActionStageCommand: systemd refused or failed the action itself.
	ServiceActionStageCommand = "command"
	// ServiceActionStageNotRunning: a reload of a daemon that is stopped.
	// Nothing was reloaded and nothing was started.
	ServiceActionStageNotRunning = "not_running"
	// The unit reported a failed reload; the running server was asked and
	// answered that it re-read its configuration files after the action.
	ServiceActionStageReloadReread = "reload_reread"
	// The same, and the server answered that it did not re-read them.
	ServiceActionStageReloadNotReread = "reload_not_reread"
)

// ApplyVhostRequest is the complete, explicit nginx vhost input shared by the
// panel and the privileged agent.
type ApplyVhostRequest struct {
	ExpectedBuildCommit string   `json:"expected_build_commit"`
	SiteID              int      `json:"site_id"`
	SubscriptionID      int      `json:"subscription_id"`
	DomainID            int      `json:"domain_id"`
	Domain              string   `json:"domain"`
	TempDomain          string   `json:"temp_domain"`
	ServerNames         []string `json:"server_names"`
	ACMEChallengeNames  []string `json:"acme_challenge_names,omitempty"`
	DocumentRoot        string   `json:"document_root"`
	PHPSocket           string   `json:"php_socket"`
	SSLType             string   `json:"ssl_type"`
	SSLCert             string   `json:"ssl_cert"`
	SSLKey              string   `json:"ssl_key"`
	RedirectWWW         bool     `json:"redirect_www"`
	ForceHTTPS          bool     `json:"force_https"`
	HSTSEnabled         bool     `json:"hsts_enabled"`
	HSTSMaxAge          int      `json:"hsts_max_age"`
	ProjectType         string   `json:"project_type"`
	AppPort             int      `json:"app_port"`
	ForwardTo           string   `json:"forward_to"`
	ForwardCode         int      `json:"forward_code"`
	// D-031, additive. FileTrigger says why this render runs (SiteFileTrigger*;
	// empty is "change"). RecordedSHA256 is the body digest the Panel's ledger
	// last recorded for this file ("" when it has none): with it a headerless
	// file is "foreign", without it "unknown origin". ExpectedFileSHA256 and
	// ExpectedRenderSHA256 bind "take CelikPanel's" to the file and the text
	// the owner was shown.
	FileTrigger          string `json:"file_trigger,omitempty"`
	RecordedSHA256       string `json:"recorded_sha256,omitempty"`
	ExpectedFileSHA256   string `json:"expected_file_sha256,omitempty"`
	ExpectedRenderSHA256 string `json:"expected_render_sha256,omitempty"`
	// ProbeValidation (D-031 step 1b, additive): for a kept file, measure
	// whether nginx serves the ACME challenge path for every validation name
	// (the server names and ACMEChallengeNames) instead of reading the file.
	// Set by certificate operations and the site-config read; nothing else.
	ProbeValidation bool `json:"probe_validation,omitempty"`
}

type ApplyVhostResponse struct {
	Config string `json:"config"`
	Error  string `json:"error,omitempty"`
	// File is the classification and outcome (D-031). Nil from an Agent
	// that predates it: the Panel then says the file's state is unknown.
	File *SiteFileResult `json:"file,omitempty"`
}

type ApplyVhostsRequest struct {
	ExpectedBuildCommit string              `json:"expected_build_commit"`
	Vhosts              []ApplyVhostRequest `json:"vhosts"`
}

type ApplyVhostsResponse struct {
	Applied int    `json:"applied"`
	Error   string `json:"error,omitempty"`
	// Items and Counts are per-site results (D-031). An Agent that predates
	// them leaves both empty; the Panel then says every file's state is
	// unknown.
	Items  []SiteFileBatchItem `json:"items,omitempty"`
	Counts *SiteFileCounts     `json:"counts,omitempty"`
}

// IssuePanelCertificateRequest and response are shared because the build
// commit gate is security-critical: silently omitting ExpectedBuildCommit
// would make every production request fail before certbot runs.
type IssuePanelCertificateRequest struct {
	MutationRequestID   string `json:"mutation_request_id,omitempty"`
	MutationOwnerID     string `json:"mutation_owner_id,omitempty"`
	Domain              string `json:"domain"`
	Email               string `json:"email"`
	TLSDir              string `json:"tls_dir"`
	ExpectedBuildCommit string `json:"expected_build_commit,omitempty"`
}

const IssuePanelCertificateErrorActivationPending = "panel_certificate_activation_pending"

type IssuePanelCertificateResponse struct {
	Issued    bool      `json:"issued"`
	ExpiresAt time.Time `json:"expires_at"`
	Detail    string    `json:"detail,omitempty"`
	ErrorCode string    `json:"error_code,omitempty"`
	Error     string    `json:"error,omitempty"`
}

// V2 binds the complete effective issuance payload into the surrounding
// service-mutation qualifier. The wire fields intentionally remain identical
// to V1 so mixed binaries fail by RPC method name rather than partial decoding.
type IssuePanelCertificateV2Request = IssuePanelCertificateRequest
type IssuePanelCertificateV2Response = IssuePanelCertificateResponse

type RestartPanelSoonRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit,omitempty"`
}
