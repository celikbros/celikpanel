package transport

// Site files the Panel writes, and the owner's edits to them (D-031, 10 Oct
// 2026). Every field here is additive on the gob wire: an older Agent leaves
// them empty or nil, an older Panel ignores them. A Panel that receives no
// File/Items answer from an Agent says the file's state is unknown; it never
// reads "no answer" as "unchanged".
//
// Panel'in yazdığı site dosyaları ve sahibin onlarda yaptığı değişiklikler
// (D-031). Buradaki her alan gob hattında eklemelidir: eski bir Agent boş ya
// da nil bırakır, eski bir Panel yok sayar. Yanıt alamayan Panel dosyanın
// durumunu "bilinmiyor" diye söyler; "yanıt yok"u asla "değişmedi" okumaz.

// SiteFileKindNginxVhost is the site's nginx vhost in sites-available. The
// PHP-FPM pool and the application unit are the second step of D-031 and use
// the same states under their own kinds.
const (
	SiteFileKindNginxVhost = "nginx_vhost"
	SiteFileKindPHPPool    = "php_pool"
	SiteFileKindAppUnit    = "app_unit"
)

// The classifier's verdict on the file found on disk before an operation.
const (
	// SiteFileAbsent: no file at the path.
	SiteFileAbsent = "absent"
	// SiteFileManagedUnchanged: the first line is the render header and the
	// digest it declares is the digest of the body under it; or a file with
	// no header that is byte for byte what a frozen earlier release's
	// template renders from the same data (AdoptedFrom names that release).
	SiteFileManagedUnchanged = "managed_unchanged"
	// SiteFileOwnerEdited: the header is present, the body under it differs.
	SiteFileOwnerEdited = "owner_edited"
	// SiteFileForeign: no header, and the Panel has a record of having written
	// this file under the header: it was replaced wholesale.
	SiteFileForeign = "foreign"
	// SiteFileUnreadable: a symlink, not a regular file, too large, or the
	// read failed. Never followed, never written.
	SiteFileUnreadable = "unreadable"
	// SiteFileUnknownOrigin: no header, no record, and not byte-equal to any
	// known CelikPanel text: a file from before D-031 that differs from every
	// frozen render (the owner's edit, or a render whose inputs changed).
	SiteFileUnknownOrigin = "unknown_origin"
	// SiteFileStateUnknown is the Panel's word when the Agent did not report
	// a state (an older Agent, or the Agent could not be asked).
	SiteFileStateUnknown = "unknown"
	// SiteFileMissingState is the Panel's stored state after an operation
	// left an absent file absent (the owner removed it).
	SiteFileMissingState = "missing"
)

// What the operation did with the file.
const (
	SiteFileOutcomeWritten   = "written"
	SiteFileOutcomeUnchanged = "unchanged"
	SiteFileOutcomeKept      = "kept"
	SiteFileOutcomeMissing   = "missing"
	SiteFileOutcomeRefused   = "refused"
	SiteFileOutcomeFailed    = "failed"
	SiteFileOutcomeRecreated = "recreated"
	SiteFileOutcomeTaken     = "taken"
	SiteFileOutcomeInspected = "inspected"
)

// Why a render runs. Only create and recreate write an absent file; only take
// replaces a file the Panel did not leave unchanged.
const (
	SiteFileTriggerStartup  = "startup"
	SiteFileTriggerChange   = "change"
	SiteFileTriggerCreate   = "create"
	SiteFileTriggerRecreate = "recreate"
	SiteFileTriggerTake     = "take"
)

// Typed reasons for unreadable, refused and failed outcomes.
const (
	SiteFileReasonSymlink       = "symlink"
	SiteFileReasonNotRegular    = "not_regular"
	SiteFileReasonPermission    = "permission"
	SiteFileReasonTooLarge      = "too_large"
	SiteFileReasonReadFailed    = "read_failed"
	SiteFileReasonWriteRefused  = "write_refused"
	SiteFileReasonChanged       = "changed_since_shown"
	SiteFileReasonNginxRefused  = "nginx_refused"
	SiteFileReasonReloadFailed  = "reload_failed"
	SiteFileReasonRenderFailed  = "render_failed"
	SiteFileReasonNotApplicable = "not_applicable"
	SiteFileReasonPendingNotSet = "pending_not_written"
)

// SiteConfigExists: site creation found a vhost file at the site's path that
// CelikPanel did not leave unchanged; it was kept and the site was not
// created. Additive CreateSiteResponse.ErrorCode.
const SiteConfigExists = "site_config_exists"

// SiteFileResult is one site file's classification and what an operation did
// with it. Paths are this server's paths; the Panel shows them to an
// administrator. The body of a file never travels in this struct.
type SiteFileResult struct {
	Kind    string
	Path    string
	State   string
	Outcome string
	Reason  string
	// Detail is one bounded line (for nginx_refused: nginx's own line).
	Detail      string
	AdoptedFrom string
	// RenderSHA256 is the digest of the Panel's text (the body under the
	// header) for this operation's inputs.
	RenderSHA256 string
	// WrittenSHA256 is the body digest the file on disk now declares and
	// carries, when it is the Panel's text after this operation.
	WrittenSHA256 string
	// FileSHA256 is the digest of the whole file on disk after the operation
	// ("" when there is none).
	FileSHA256    string
	PendingPath   string
	PendingSHA256 string
	BackupPath    string
	// IncludeDir is the owner's include directory for the site.
	IncludeDir string
	// Enabled describes sites-enabled/<domain>.conf: "link" (the link to this
	// file), "absent", or "other" (anything else; never touched).
	Enabled string
	// Reloaded is true when this operation reloaded nginx.
	Reloaded bool

	// D-031 step 1b, additive. ManagedDir is the Panel's own include
	// directory of the site (the ACME HTTP-01 location lives there, not in
	// the vhost); ManagedInclude is the exact line a vhost needs to read it.
	ManagedDir     string
	ManagedInclude string
	// ChallengeFile is what happened to the challenge file in ManagedDir
	// (SiteFileChallenge*); "" when the operation did not look at it.
	ChallengeFile string
	// Validation is, for a kept file (owner-edited, foreign, unknown origin),
	// whether a certificate for the site can be validated without changing
	// the file (SiteFileValidation*); "" for any other file.
	Validation string
	// CertificateReferenced: the kept file has the line `ssl_certificate
	// <path>;` for the certificate path of this render.
	CertificateReferenced bool
}

// What happened to the Panel's challenge file (D-031 step 1b).
const (
	SiteFileChallengeWritten   = "written"
	SiteFileChallengeUnchanged = "unchanged"
	// SiteFileChallengeKept: the file in the Panel's directory is not the
	// Panel's unchanged text; it was kept and not replaced.
	SiteFileChallengeKept = "kept"
	// SiteFileChallengeFailed: it could not be written, or nginx refused the
	// configuration with it and it was put back.
	SiteFileChallengeFailed = "failed"
	// SiteFileChallengeAbsent and SiteFileChallengeDiffers are read-only
	// observations (the domain screen's inspection).
	SiteFileChallengeAbsent  = "absent"
	SiteFileChallengeDiffers = "differs"
)

// Whether a kept file lets a certificate be validated (D-031 step 1b).
const (
	SiteFileValidationReady = "ready"
	// SiteFileValidationIncludeMissing: the file does not include the Panel's
	// directory, so the challenge location cannot be published.
	SiteFileValidationIncludeMissing = "include_missing"
	// SiteFileValidationNamesMissing: the operation must also validate names
	// such as mail.<domain>, and the file does not hold their validation-only
	// server block exactly as CelikPanel writes it.
	SiteFileValidationNamesMissing = "names_missing"
	// SiteFileValidationChallengeKept: the challenge file in the Panel's
	// directory was changed outside CelikPanel and was kept.
	SiteFileValidationChallengeKept = "challenge_kept"
	// SiteFileValidationChallengeFailed: the challenge file could not be
	// written, or nginx refused the configuration with it.
	SiteFileValidationChallengeFailed = "challenge_failed"
)

// SiteFileBatchItem is one item of a start-up batch.
type SiteFileBatchItem struct {
	DomainID int
	SiteID   int
	File     SiteFileResult
	// Error is this item's own failure (render input refused, challenge root
	// not prepared); the other items went on.
	Error string
}

// SiteFileCounts is the start line's summary.
type SiteFileCounts struct {
	Written    int
	Unchanged  int
	Kept       int
	Foreign    int
	Unknown    int
	Unreadable int
	Missing    int
	Adopted    int
	Failed     int
}

// InspectSiteFileResponse is the read-only answer for the domain screen. Diff
// is a unified diff of the file on disk against the Panel's text, bounded in
// size, with lines that look like credentials hidden.
type InspectSiteFileResponse struct {
	File          SiteFileResult
	Diff          string
	DiffTruncated bool
	Error         string
}
