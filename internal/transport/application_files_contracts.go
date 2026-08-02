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
	Username string `json:"username"`
}

type ListCronJobsResponse struct {
	Jobs []CronJob `json:"jobs"`
}

type AddCronJobRequest struct {
	Username string `json:"username"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Comment  string `json:"comment,omitempty"`
}

type UpdateCronJobRequest struct {
	Username string `json:"username"`
	ID       string `json:"id"`
	Schedule string `json:"schedule"`
	Command  string `json:"command"`
	Enabled  bool   `json:"enabled"`
	Comment  string `json:"comment,omitempty"`
}

type DeleteCronJobRequest struct {
	Username string `json:"username"`
	ID       string `json:"id"`
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
