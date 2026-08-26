package main

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	systemUpdateStateVersion = 1
	systemUpdateQueued       = "queued"
	systemUpdateRunning      = "running"
	systemUpdateSucceeded    = "succeeded"
	systemUpdateFailed       = "failed"
	systemUpdateLaunchGrace  = 30 * time.Second
)

var errSystemUpdateNotFound = errors.New("system update request was not found")

type systemUpdateState struct {
	Version                int    `json:"version"`
	RequestID              string `json:"request_id"`
	Status                 string `json:"status"`
	TargetVersion          string `json:"target_version"`
	TargetCommit           string `json:"target_commit"`
	TargetSequence         string `json:"target_sequence"`
	TargetOS               string `json:"target_os"`
	TargetArch             string `json:"target_arch"`
	TargetArchiveSHA256    string `json:"target_archive_sha256"`
	TargetArchiveSize      string `json:"target_archive_size"`
	ExpectedCurrentVersion string `json:"expected_current_version"`
	ExpectedCurrentCommit  string `json:"expected_current_commit"`
	CreatedAt              string `json:"created_at"`
	UpdatedAt              string `json:"updated_at"`
	Error                  string `json:"error,omitempty"`
}

func (state *systemUpdateState) active() bool {
	return state != nil && (state.Status == systemUpdateQueued || state.Status == systemUpdateRunning)
}

func validateSystemUpdateState(state *systemUpdateState) error {
	if state == nil || state.Version != systemUpdateStateVersion || !systemUpdateRequestRE.MatchString(state.RequestID) {
		return errors.New("system update state identity is invalid")
	}
	if state.Status != systemUpdateQueued && state.Status != systemUpdateRunning && state.Status != systemUpdateSucceeded && state.Status != systemUpdateFailed {
		return errors.New("system update state status is invalid")
	}
	request := transport.SystemUpdateStartRequest{
		RequestID: state.RequestID, TargetVersion: state.TargetVersion, TargetCommit: state.TargetCommit,
		TargetSequence: state.TargetSequence, TargetOS: state.TargetOS, TargetArch: state.TargetArch,
		TargetArchiveSHA256: state.TargetArchiveSHA256, TargetArchiveSize: state.TargetArchiveSize,
		ExpectedCurrentVersion: state.ExpectedCurrentVersion, ExpectedCurrentCommit: state.ExpectedCurrentCommit,
	}
	if err := validateSystemUpdateStartRequest(&request); err != nil {
		return err
	}
	created, err := time.Parse(time.RFC3339Nano, state.CreatedAt)
	if err != nil || created.UTC().Format(time.RFC3339Nano) != state.CreatedAt {
		return errors.New("system update creation time is invalid")
	}
	updated, err := time.Parse(time.RFC3339Nano, state.UpdatedAt)
	if err != nil || updated.UTC().Format(time.RFC3339Nano) != state.UpdatedAt || updated.Before(created) {
		return errors.New("system update modification time is invalid")
	}
	if state.Status == systemUpdateFailed {
		if state.Error == "" || len(state.Error) > systemUpdateMaxErrorSize {
			return errors.New("failed system update lacks a bounded error")
		}
	} else if state.Error != "" {
		return errors.New("non-failed system update carries an error")
	}
	return nil
}

func validateSystemUpdateStartRequest(request *transport.SystemUpdateStartRequest) error {
	if request == nil || !systemUpdateRequestRE.MatchString(request.RequestID) {
		return errors.New("request_id must be 32 lowercase hexadecimal characters")
	}
	manifest := systemUpdateManifest{
		Sequence: request.TargetSequence, Version: request.TargetVersion, Commit: request.TargetCommit,
		PublishedAt: "2000-01-01T00:00:00Z", OS: request.TargetOS, Arch: request.TargetArch,
		Archive:       "celikpanel-" + request.TargetVersion + "-" + request.TargetOS + "-" + request.TargetArch + ".tar.gz",
		ArchiveSHA256: request.TargetArchiveSHA256, ArchiveSize: request.TargetArchiveSize,
	}
	if err := validateSystemUpdateManifest(manifest); err != nil {
		return fmt.Errorf("invalid requested target: %w", err)
	}
	if _, err := parseSystemUpdateSemver(request.ExpectedCurrentVersion); err != nil {
		return errors.New("expected current version is not a release build")
	}
	if !systemUpdateCommitRE.MatchString(request.ExpectedCurrentCommit) {
		return errors.New("expected current commit is invalid")
	}
	return nil
}

func sanitizedSystemUpdateError(err error) string {
	if err == nil {
		return ""
	}
	raw := strings.ReplaceAll(err.Error(), "\r\n", "\n")
	raw = strings.ReplaceAll(raw, "\r", "\n")
	value := ""
	lines := strings.Split(raw, "\n")
	for index := len(lines) - 1; index >= 0; index-- {
		var cleaned strings.Builder
		for _, char := range strings.TrimSpace(lines[index]) {
			if char >= 0x20 && char <= 0x7e {
				cleaned.WriteRune(char)
			}
		}
		candidate := strings.TrimSpace(cleaned.String())
		if candidate != "" {
			value = candidate
			break
		}
	}
	if value == "" {
		value = "system update failed"
	}
	if len(value) > systemUpdateMaxErrorSize {
		value = value[:systemUpdateMaxErrorSize]
	}
	return value
}

type systemUpdateBackend interface {
	ReadFloor() (*systemUpdateFloor, error)
	QueueAndLaunch(context.Context, *systemUpdateState) (*systemUpdateState, error)
	Status(context.Context, string) (*systemUpdateState, error)
	RunWorker(context.Context, string, systemUpdateManifestFetcher) error
	Reconcile(context.Context) error
}

type systemUpdateService struct {
	fetcher           systemUpdateManifestFetcher
	backend           systemUpdateBackend
	now               func() time.Time
	os                string
	arch              string
	unsupportedReason string
}

func newSystemUpdateService(fetcher systemUpdateManifestFetcher, backend systemUpdateBackend, platformOS, platformArch string) *systemUpdateService {
	return &systemUpdateService{fetcher: fetcher, backend: backend, now: func() time.Time { return time.Now().UTC() }, os: platformOS, arch: platformArch}
}

func (service *systemUpdateService) supported() bool {
	return service != nil && service.unsupportedReason == "" && service.fetcher != nil && service.backend != nil &&
		service.os == "linux" && (service.arch == "amd64" || service.arch == "arm64")
}

func (service *systemUpdateService) unsupportedError() error {
	if service != nil && service.unsupportedReason != "" {
		return errors.New(service.unsupportedReason)
	}
	return errors.New("system updates are supported only on Linux amd64 and arm64")
}

func (service *systemUpdateService) check(ctx context.Context) (transport.SystemUpdateCheckResponse, error) {
	response := transport.SystemUpdateCheckResponse{Supported: service.supported(), CurrentVersion: buildVersion, CurrentCommit: buildCommit}
	if !response.Supported {
		return response, service.unsupportedError()
	}
	version, err := service.fetcher.Discover(ctx)
	if err != nil {
		return response, err
	}
	manifest, err := service.fetcher.Fetch(ctx, version, service.os, service.arch)
	if err != nil {
		return response, err
	}
	floor, err := service.backend.ReadFloor()
	if err != nil {
		return response, err
	}
	if err := systemUpdateFloorAllows(floor, manifest); err != nil {
		return response, err
	}
	response.TargetVersion = manifest.Version
	response.TargetCommit = manifest.Commit
	response.TargetSequence = manifest.Sequence
	response.TargetOS = manifest.OS
	response.TargetArch = manifest.Arch
	response.TargetArchiveSHA256 = manifest.ArchiveSHA256
	response.TargetArchiveSize = manifest.ArchiveSize
	response.PublishedAt = manifest.PublishedAt
	compared, err := compareSystemUpdateSemver(manifest.Version, buildVersion)
	if err != nil || !systemUpdateCommitRE.MatchString(buildCommit) {
		return response, errors.New("current agent is not an identifiable release build")
	}
	response.Available = compared > 0 && manifest.Commit != buildCommit
	return response, nil
}

func (service *systemUpdateService) start(ctx context.Context, request *transport.SystemUpdateStartRequest) (transport.SystemUpdateStartResponse, error) {
	if !service.supported() {
		return transport.SystemUpdateStartResponse{}, service.unsupportedError()
	}
	if err := validateSystemUpdateStartRequest(request); err != nil {
		return transport.SystemUpdateStartResponse{}, err
	}
	existing, statusErr := service.backend.Status(ctx, request.RequestID)
	if statusErr == nil && existing != nil {
		if !systemUpdateStateMatchesRequest(existing, request) {
			return transport.SystemUpdateStartResponse{}, errors.New("request_id belongs to another system update")
		}
		// An exact retry identifies the already durable operation before
		// comparing the now-installed build. This keeps Start idempotent after
		// an agent restart has successfully installed the requested target.
		return transport.SystemUpdateStartResponse{Accepted: true, Status: existing.Status}, nil
	}
	if statusErr != nil && !errors.Is(statusErr, errSystemUpdateNotFound) {
		return transport.SystemUpdateStartResponse{}, statusErr
	}
	if request.ExpectedCurrentVersion != buildVersion || request.ExpectedCurrentCommit != buildCommit {
		return transport.SystemUpdateStartResponse{}, errors.New("installed build changed after update discovery")
	}
	compared, err := compareSystemUpdateSemver(request.TargetVersion, buildVersion)
	if err != nil || !systemUpdateCommitRE.MatchString(buildCommit) {
		return transport.SystemUpdateStartResponse{}, errors.New("current agent is not an identifiable release build")
	}
	if compared <= 0 || request.TargetCommit == buildCommit {
		return transport.SystemUpdateStartResponse{}, errors.New("requested target is not newer than the installed build")
	}
	manifest, err := service.fetcher.Fetch(ctx, request.TargetVersion, service.os, service.arch)
	if err != nil {
		return transport.SystemUpdateStartResponse{}, err
	}
	if !systemUpdateRequestMatchesManifest(request, manifest) {
		return transport.SystemUpdateStartResponse{}, errors.New("requested target does not match the freshly verified manifest")
	}
	floor, err := service.backend.ReadFloor()
	if err != nil {
		return transport.SystemUpdateStartResponse{}, err
	}
	if err := systemUpdateFloorAllows(floor, manifest); err != nil {
		return transport.SystemUpdateStartResponse{}, err
	}
	now := service.now().UTC().Format(time.RFC3339Nano)
	state := &systemUpdateState{
		Version: systemUpdateStateVersion, RequestID: request.RequestID, Status: systemUpdateQueued,
		TargetVersion: request.TargetVersion, TargetCommit: request.TargetCommit, TargetSequence: request.TargetSequence,
		TargetOS: request.TargetOS, TargetArch: request.TargetArch, TargetArchiveSHA256: request.TargetArchiveSHA256,
		TargetArchiveSize: request.TargetArchiveSize, ExpectedCurrentVersion: request.ExpectedCurrentVersion,
		ExpectedCurrentCommit: request.ExpectedCurrentCommit, CreatedAt: now, UpdatedAt: now,
	}
	queued, err := service.backend.QueueAndLaunch(ctx, state)
	if err != nil {
		return transport.SystemUpdateStartResponse{}, err
	}
	return transport.SystemUpdateStartResponse{Accepted: true, Status: queued.Status}, nil
}

func systemUpdateStateMatchesRequest(state *systemUpdateState, request *transport.SystemUpdateStartRequest) bool {
	if state == nil || request == nil {
		return false
	}
	return state.RequestID == request.RequestID &&
		state.TargetVersion == request.TargetVersion &&
		state.TargetCommit == request.TargetCommit &&
		state.TargetSequence == request.TargetSequence &&
		state.TargetOS == request.TargetOS &&
		state.TargetArch == request.TargetArch &&
		state.TargetArchiveSHA256 == request.TargetArchiveSHA256 &&
		state.TargetArchiveSize == request.TargetArchiveSize &&
		state.ExpectedCurrentVersion == request.ExpectedCurrentVersion &&
		state.ExpectedCurrentCommit == request.ExpectedCurrentCommit
}

func systemUpdateStatusResponse(state *systemUpdateState) transport.SystemUpdateStatusResponse {
	if state == nil {
		return transport.SystemUpdateStatusResponse{}
	}
	return transport.SystemUpdateStatusResponse{
		Found: true, RequestID: state.RequestID, Status: state.Status, TargetVersion: state.TargetVersion,
		TargetCommit: state.TargetCommit, TargetSequence: state.TargetSequence, TargetOS: state.TargetOS,
		TargetArch: state.TargetArch, TargetArchiveSHA256: state.TargetArchiveSHA256,
		TargetArchiveSize: state.TargetArchiveSize, CreatedAt: state.CreatedAt, UpdatedAt: state.UpdatedAt, Error: state.Error,
	}
}

var (
	globalSystemUpdateMu       sync.Mutex
	globalSystemUpdateService  *systemUpdateService
	globalSystemUpdateErr      error
	systemUpdateServiceFactory = newPlatformSystemUpdateService
)

func agentSystemUpdateService() (*systemUpdateService, error) {
	globalSystemUpdateMu.Lock()
	defer globalSystemUpdateMu.Unlock()
	if globalSystemUpdateService == nil && globalSystemUpdateErr == nil {
		globalSystemUpdateService, globalSystemUpdateErr = systemUpdateServiceFactory()
	}
	return globalSystemUpdateService, globalSystemUpdateErr
}

func (a *Agent) CheckSystemUpdate(_ *transport.Empty, reply *transport.SystemUpdateCheckResponse) error {
	if reply == nil {
		return errors.New("system update check response is required")
	}
	*reply = transport.SystemUpdateCheckResponse{CurrentVersion: buildVersion, CurrentCommit: buildCommit}
	service, err := agentSystemUpdateService()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		*reply, err = service.check(ctx)
	}
	if err != nil {
		reply.Error = sanitizedSystemUpdateError(err)
	}
	return nil
}

func (a *Agent) StartSystemUpdate(request *transport.SystemUpdateStartRequest, reply *transport.SystemUpdateStartResponse) error {
	if reply == nil {
		return errors.New("system update start response is required")
	}
	*reply = transport.SystemUpdateStartResponse{}
	service, err := agentSystemUpdateService()
	if err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		*reply, err = service.start(ctx, request)
	}
	if err != nil {
		reply.Error = sanitizedSystemUpdateError(err)
	}
	return nil
}

func (a *Agent) SystemUpdateStatus(request *transport.SystemUpdateStatusRequest, reply *transport.SystemUpdateStatusResponse) error {
	if reply == nil {
		return errors.New("system update status response is required")
	}
	*reply = transport.SystemUpdateStatusResponse{}
	if request == nil || !systemUpdateRequestRE.MatchString(request.RequestID) {
		reply.Error = "request_id must be 32 lowercase hexadecimal characters"
		return nil
	}
	service, err := agentSystemUpdateService()
	if err == nil && service != nil && service.backend != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		defer cancel()
		var state *systemUpdateState
		state, err = service.backend.Status(ctx, request.RequestID)
		if errors.Is(err, errSystemUpdateNotFound) {
			err = nil
		} else if err == nil {
			*reply = systemUpdateStatusResponse(state)
		}
	} else if err == nil {
		err = errors.New("system update status storage is unavailable on this platform")
	}
	if err != nil {
		reply.Error = sanitizedSystemUpdateError(err)
	}
	return nil
}

func reconcileSystemUpdatesAtStartup() error {
	service, err := agentSystemUpdateService()
	if err != nil || service == nil || service.backend == nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	return service.backend.Reconcile(ctx)
}

func runtimeSystemUpdatePlatform() (string, string) { return runtime.GOOS, runtime.GOARCH }
