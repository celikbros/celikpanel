package transport

import "time"

// Application process and one-click installer RPC contracts.
type AppApplyRequest struct {
	SiteID      int    `json:"site_id"`
	Description string `json:"description"`
	WorkDir     string `json:"work_dir"`
	Command     string `json:"command"`
	Port        int    `json:"port"`
	NodeVersion string `json:"node_version"`
	RunAsUser   string `json:"run_as_user,omitempty"`
}

type AppApplyResponse struct {
	Unit  string `json:"unit"`
	Error string `json:"error,omitempty"`
}

type AppControlRequest struct {
	SiteID int    `json:"site_id"`
	Action string `json:"action"`
}

type AppStatusResponse struct {
	Exists   bool   `json:"exists"`
	Active   string `json:"active"`
	PID      int    `json:"pid"`
	MemoryMB int64  `json:"memory_mb"`
	CPUUsec  int64  `json:"cpu_usec"`
	Uptime   string `json:"uptime"`
}

type AppLogsRequest struct {
	SiteID int `json:"site_id"`
	Lines  int `json:"lines"`
}

type AppLogsResponse struct {
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}

type InstallWordPressRequest struct {
	ExpectedBuildCommit string `json:"expected_build_commit"`
	OperationID         string `json:"operation_id"`
	SiteID              int    `json:"site_id"`
	SubscriptionID      int    `json:"subscription_id"`
	DomainID            int    `json:"domain_id"`
	Domain              string `json:"domain"`
	DBName              string `json:"db_name"`
	DBUser              string `json:"db_user"`
	DBPass              string `json:"db_pass"`
	DBHost              string `json:"db_host"`
	Username            string `json:"username"`
}

type InstallWordPressResponse struct {
	Installed        bool   `json:"installed"`
	CompensationSafe bool   `json:"compensation_safe"`
	Detail           string `json:"detail,omitempty"`
	Error            string `json:"error,omitempty"`
}

// Cron RPC contracts.
type CronJob struct {
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment,omitempty"`
}

type ListCronJobsRequest struct {
	CronTenant
}

type ListCronJobsResponse struct {
	Jobs []CronJob `json:"jobs"`
}

// CronTenant identifies WHOSE crontab an operation targets. It carries the
// durable identities rather than a username because the username is derived and
// NOT injective: SiteUsername maps both "." and "-" to "_" and truncates at 32,
// so "my-shop.com" and "my.shop.com" — which can belong to different tenants —
// produce the same Linux account name. A request that names only a username
// therefore cannot say which tenant it means, and ownership of one domain would
// reach the other's jobs.
//
// The agent re-derives the username from Domain and then requires the account's
// home directory to equal the home derived from SubscriptionID and DomainID.
// That second proof is the injective one: homes come from integer identities and
// cannot collide.
//
// CronTenant, bir işlemin KİMİN crontab'ını hedeflediğini tanımlar. Kullanıcı
// adı yerine kalıcı kimlikleri taşır, çünkü kullanıcı adı türetilmiştir ve TEK
// YÖNLÜ DEĞİLDİR: SiteUsername hem "." hem "-" karakterini "_" yapar ve 32'de
// keser; böylece farklı kiracılara ait olabilen "my-shop.com" ile "my.shop.com"
// aynı Linux hesap adını üretir. Yalnızca kullanıcı adı taşıyan bir istek hangi
// kiracıyı kastettiğini söyleyemez ve bir alan adının sahipliği diğerinin
// görevlerine uzanır.
//
// Agent, kullanıcı adını Domain'den yeniden türetir ve ardından hesabın ev
// dizininin SubscriptionID ile DomainID'den türetilen ev dizinine eşit olmasını
// şart koşar. Çakışmayan kanıt bu ikincisidir: ev dizinleri tam sayı
// kimliklerden gelir.
type CronTenant struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Domain         string `json:"domain"`
}

type AddCronJobRequest struct {
	CronTenant
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment,omitempty"`
}

type UpdateCronJobRequest struct {
	CronTenant
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment,omitempty"`
}

type DeleteCronJobRequest struct {
	CronTenant
	ID string `json:"id"`
}

// File manager RPC contracts.
type FileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	IsDir       bool      `json:"is_dir"`
	Size        int64     `json:"size"`
	Permissions string    `json:"permissions"`
	Owner       string    `json:"owner"`
	Group       string    `json:"group"`
	ModTime     time.Time `json:"mod_time"`
}

type ListFilesRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
}

type ListFilesResponse struct {
	Path  string     `json:"path"`
	Files []FileInfo `json:"files"`
}

type ReadFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
}

type ReadFileResponse struct {
	Path     string `json:"path"`
	Content  string `json:"content"`
	Size     int64  `json:"size"`
	IsBinary bool   `json:"is_binary"`
}

type WriteFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	Content        string `json:"content"`
}

type CreateFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	IsDir          bool   `json:"is_dir"`
}

type DeleteFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
}

type ChmodFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	Permissions    string `json:"permissions"`
}

type RenameFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	OldPath        string `json:"old_path"`
	NewPath        string `json:"new_path"`
}

type UploadFileRequest struct {
	SubscriptionID int    `json:"subscription_id"`
	DomainID       int    `json:"domain_id"`
	Path           string `json:"path"`
	Name           string `json:"name"`
	Content        string `json:"content"`
}

// Log viewer RPC contracts.
type GetLogsRequest struct {
	LogPath string `json:"log_path"`
	Lines   int    `json:"lines"`
	Filter  string `json:"filter"`
	// StartTime and EndTime are inclusive RFC3339 bounds with an explicit
	// timezone. The agent recognizes nginx access timestamps, nginx error
	// timestamps (server-local time), and bracketed PHP timestamps.
	StartTime string `json:"start_time"`
	EndTime   string `json:"end_time"`
}

// LogTimeFilterResult makes the limits of timestamp filtering observable to
// callers. Exact is false whenever part of the file was not scanned, matching
// lines were capped, or a line could not be assigned a timestamp using one of
// the documented nginx/PHP formats.
type LogTimeFilterResult struct {
	Applied         bool   `json:"applied"`
	Exact           bool   `json:"exact"`
	StartTime       string `json:"start_time,omitempty"`
	EndTime         string `json:"end_time,omitempty"`
	ParsedLines     int    `json:"parsed_lines"`
	UnparsedLines   int    `json:"unparsed_lines"`
	AssumedTimezone string `json:"assumed_timezone,omitempty"`
	Warning         string `json:"warning,omitempty"`
}

type GetLogsResponse struct {
	Success    bool                 `json:"success"`
	Lines      []string             `json:"lines"`
	Total      int                  `json:"total"`
	Truncated  bool                 `json:"truncated,omitempty"`
	Warning    string               `json:"warning,omitempty"`
	TimeFilter *LogTimeFilterResult `json:"time_filter,omitempty"`
	Error      string               `json:"error,omitempty"`
}

type ClearLogsRequest struct {
	LogPath string `json:"log_path"`
}

type ClearLogsResponse struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

type DomainLogPathsResponse struct {
	AccessLog string `json:"access_log"`
	ErrorLog  string `json:"error_log"`
	PHPLog    string `json:"php_log"`
}
