package transport

import (
	"time"

	"github.com/alicelik/celikpanel/internal/core"
)

// ServiceMutationJob is the durable state shared by the panel and the
// privileged agent while one host mutation is in progress. Keeping the full
// shape here prevents gob from silently dropping fields when either side is
// upgraded independently.
type ServiceMutationJob struct {
	RequestID      string    `json:"request_id"`
	OwnerID        string    `json:"owner_id"`
	Kind           string    `json:"kind"`
	Target         string    `json:"target"`
	PackageName    string    `json:"package_name,omitempty"`
	Status         string    `json:"status"`
	Phase          string    `json:"phase"`
	Attempt        int       `json:"attempt"`
	StartedAt      time.Time `json:"started_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	LeaseExpiresAt time.Time `json:"lease_expires_at"`
	DeadlineAt     time.Time `json:"deadline_at"`
	FinishedAt     time.Time `json:"finished_at,omitempty"`
	ErrorCode      string    `json:"error_code,omitempty"`
	ErrorMessage   string    `json:"error_message,omitempty"`
	WorkerPID      int       `json:"worker_pid,omitempty"`
	WorkerStarted  string    `json:"worker_started,omitempty"`
	WorkerCommand  string    `json:"worker_command,omitempty"`
}

type ServiceMutationBeginRequest struct {
	RequestID   string `json:"request_id"`
	OwnerID     string `json:"owner_id"`
	Kind        string `json:"kind"`
	Target      string `json:"target"`
	PackageName string `json:"package_name,omitempty"`
	Resume      bool   `json:"resume,omitempty"`
}

type ServiceMutationBinding struct {
	MutationRequestID string `json:"mutation_request_id,omitempty"`
	MutationOwnerID   string `json:"mutation_owner_id,omitempty"`
}

type ServiceMutationRequest struct {
	ServiceMutationBinding
}

// SetServerHostnameRequest asks the privileged agent to give this server the
// fully qualified name the mail stack will answer as. The panel sends only a
// canonical FQDN; the agent revalidates it and refuses anything else.
// SetServerHostnameRequest, ayrıcalıklı agent'tan bu sunucuya, posta yığınının
// adına yanıt vereceği tam nitelikli adı vermesini ister. Panel yalnız kanonik
// bir FQDN gönderir; agent onu yeniden doğrular ve başkasını reddeder.
type SetServerHostnameRequest struct {
	ServiceMutationBinding
	Hostname string `json:"hostname"`
}

type SetServerHostnameResponse struct {
	Hostname string `json:"hostname,omitempty"`
	Previous string `json:"previous,omitempty"`
	Changed  bool   `json:"changed"`
	Error    string `json:"error,omitempty"`
}

type ServiceMutationHeartbeatRequest struct {
	RequestID string `json:"request_id"`
	OwnerID   string `json:"owner_id"`
	Phase     string `json:"phase,omitempty"`
}

type ServiceMutationStatusRequest struct {
	RequestID string `json:"request_id,omitempty"`
}

type ServiceMutationCancelRequest struct {
	RequestID      string `json:"request_id"`
	ExpectedOwner  string `json:"expected_owner"`
	Reason         string `json:"reason,omitempty"`
	FailureCode    string `json:"failure_code,omitempty"`
	FailureMessage string `json:"failure_message,omitempty"`
}

type ServiceMutationFinishRequest struct {
	RequestID   string `json:"request_id"`
	OwnerID     string `json:"owner_id"`
	Success     bool   `json:"success"`
	FailureCode string `json:"failure_code,omitempty"`
	Message     string `json:"message,omitempty"`
}

type ServiceMutationResponse struct {
	Job       *ServiceMutationJob `json:"job,omitempty"`
	ErrorCode string              `json:"error_code,omitempty"`
	Error     string              `json:"error,omitempty"`

	// MutationHold carries why the agent is refusing every durable mutation,
	// as one of the stable MutationHold* codes, or "" when it is accepting
	// them. It rides on the response rather than on the job because the
	// condition is process-wide: it is a property of the agent, not of the
	// operation being asked about.
	//
	// A caller that is waiting for an operation to reach a terminal state MUST
	// stop when this is set. A held agent cannot move any job — heartbeat,
	// finish and cancel all refuse — so the job it reports stays exactly as it
	// is for as long as anyone is willing to poll. Without this field the only
	// signal is the poller's own timeout, which is how a five-second failure
	// became a thirty-minute silence.
	//
	// MutationHold, agent'ın her kalıcı mutasyonu neden reddettiğini kararlı
	// MutationHold* kodlarından biriyle taşır; kabul ediyorsa "" olur. İş
	// üzerinde değil yanıt üzerinde durur, çünkü koşul süreç geneliidir:
	// sorulan işlemin değil agent'ın bir özelliğidir.
	//
	// Bir işlemin uç duruma ulaşmasını bekleyen çağıran, bu ayarlıysa
	// DURMALIDIR. Tutulan bir agent hiçbir işi kımıldatamaz — kalp atışı,
	// bitirme ve iptal hepsi reddeder — dolayısıyla bildirdiği iş, kim ne kadar
	// yoklarsa yoklasın olduğu gibi kalır. Bu alan olmadan tek işaret,
	// yoklayanın kendi zaman aşımıdır; beş saniyelik bir arızanın otuz dakikalık
	// bir sessizliğe dönüşmesinin sebebi tam da budur.
	MutationHold string `json:"mutation_hold,omitempty"`
}

const (
	HostMutationBusy        = "HOST_MUTATION_BUSY"
	HostMutationUnavailable = "HOST_MUTATION_UNAVAILABLE"

	HostMutationReasonPanelOperation  = "panel_operation_active"
	HostMutationReasonAgentMutation   = "agent_mutation_active"
	HostMutationReasonHostLock        = "host_lock_busy"
	HostMutationReasonPackageManager  = "package_manager_active"
	HostMutationReasonStateUnverified = "state_unverified"
)

// HostMutationReadinessResponse is an advisory, read-only snapshot for
// disabling controls that cannot currently start. It never grants a mutation;
// BeginServiceMutation remains the authoritative admission boundary.
type HostMutationReadinessResponse struct {
	Ready  bool   `json:"ready"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type ServiceMutationActionRequest struct {
	ServiceMutationBinding
	ServiceName string `json:"service_name"`
	Action      string `json:"action"`
}

type ServiceMutationServiceRequest struct {
	ServiceMutationBinding
	ServiceName string `json:"service_name"`
}

type InstallServiceRequest struct {
	ServiceMutationBinding
	ID      string `json:"id"`
	Package string `json:"package,omitempty"`
}

type InstallServiceResponse struct {
	Installed bool   `json:"installed"`
	Detail    string `json:"detail,omitempty"`
	Unit      string `json:"unit,omitempty"`
	Error     string `json:"error,omitempty"`
}

type UninstallServiceResponse struct {
	Removed         bool   `json:"removed"`
	Detail          string `json:"detail,omitempty"`
	Error           string `json:"error,omitempty"`
	PartialSuccess  bool   `json:"partial_success,omitempty"`
	MutationApplied bool   `json:"mutation_applied,omitempty"`
}

type ConfigureDBToolsResponse struct {
	Configured bool     `json:"configured"`
	Tools      []string `json:"tools"`
	Error      string   `json:"error,omitempty"`
}

type ConfigureDKIMSigningResponse struct {
	Configured bool   `json:"configured"`
	Domains    int    `json:"domains"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

type ConfigureMailStackResponse struct {
	Configured bool   `json:"configured"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

type WireMailFiltersResponse struct {
	Wired  bool   `json:"wired"`
	Detail string `json:"detail,omitempty"`
	Error  string `json:"error,omitempty"`
}

type ConfigureMailSubmissionResponse struct {
	Configured bool   `json:"configured"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
}

type EnsureNginxReadyResponse struct {
	Ready   bool   `json:"ready"`
	Changed bool   `json:"changed"`
	Error   string `json:"error,omitempty"`
}

type WebmailMutationRequest struct {
	ServiceMutationBinding
}

type InstallRoundcubeResponse struct {
	Installed bool   `json:"installed"`
	Version   string `json:"version"`
	Error     string `json:"error,omitempty"`
}

type RemoveRoundcubeResponse struct {
	Removed         bool   `json:"removed"`
	MutationApplied bool   `json:"mutation_applied,omitempty"`
	Error           string `json:"error,omitempty"`
}

type ConfigureWebmailResponse struct {
	Configured bool   `json:"configured"`
	Present    bool   `json:"present"`
	Error      string `json:"error,omitempty"`
}

type InstalledRepoPackagesRequest struct {
	ServiceID string `json:"service_id"`
}

type InstalledRepoPackagesResponse struct {
	Packages []string `json:"packages"`
	Error    string   `json:"error,omitempty"`
}

type ServiceInstancesRequest struct {
	ID string `json:"id"`
}

type ServiceInstancesResponse struct {
	Instances []core.ServiceInstance `json:"instances"`
	Error     string                 `json:"error,omitempty"`
}

type NodeVersionsResponse struct {
	Installed     []string `json:"installed"`
	SystemVersion string   `json:"system_version"`
}

type NodeInstallRequest struct {
	ServiceMutationBinding
	Version string `json:"version"`
}

type NodeInstallResponse struct {
	Installed bool   `json:"installed"`
	Error     string `json:"error,omitempty"`
}

type NodeRemoveRequest struct {
	ServiceMutationBinding
	Version string `json:"version"`
}

type NodeRemoveResponse struct {
	Removed bool   `json:"removed"`
	Error   string `json:"error,omitempty"`
}

type NodeLTSRelease struct {
	Version string `json:"version"`
	Name    string `json:"name"`
}

type NodeLTSResponse struct {
	Releases []NodeLTSRelease `json:"releases"`
	Error    string           `json:"error,omitempty"`
}

type ServiceJournalRequest struct {
	Unit  string `json:"unit"`
	Lines int    `json:"lines"`
}

type ServiceJournalResponse struct {
	Unit  string   `json:"unit"`
	Lines []string `json:"lines"`
	Error string   `json:"error,omitempty"`
}
