package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

type fakeSystemUpdateFetcher struct {
	version     string
	manifest    systemUpdateManifest
	discoverErr error
	fetchErr    error
	fetchCalls  int
}

func (fake *fakeSystemUpdateFetcher) Discover(context.Context) (string, error) {
	return fake.version, fake.discoverErr
}
func (fake *fakeSystemUpdateFetcher) Fetch(_ context.Context, version, platformOS, platformArch string) (systemUpdateManifest, error) {
	fake.fetchCalls++
	if fake.fetchErr != nil {
		return systemUpdateManifest{}, fake.fetchErr
	}
	if version != fake.manifest.Version || platformOS != fake.manifest.OS || platformArch != fake.manifest.Arch {
		return systemUpdateManifest{}, errors.New("wrong fetch identity")
	}
	return fake.manifest, nil
}

type fakeSystemUpdateBackend struct {
	floor     *systemUpdateFloor
	floorErr  error
	queued    *systemUpdateState
	queueErr  error
	status    *systemUpdateState
	statusErr error
}

func (fake *fakeSystemUpdateBackend) ReadFloor() (*systemUpdateFloor, error) {
	return fake.floor, fake.floorErr
}
func (fake *fakeSystemUpdateBackend) QueueAndLaunch(_ context.Context, state *systemUpdateState) (*systemUpdateState, error) {
	fake.queued = state
	return state, fake.queueErr
}
func (fake *fakeSystemUpdateBackend) Status(context.Context, string) (*systemUpdateState, error) {
	return fake.status, fake.statusErr
}
func (*fakeSystemUpdateBackend) RunWorker(context.Context, string, systemUpdateManifestFetcher) error {
	return nil
}
func (*fakeSystemUpdateBackend) Reconcile(context.Context) error { return nil }

func withSystemUpdateBuild(t *testing.T, version, commit string) {
	t.Helper()
	oldVersion, oldCommit := buildVersion, buildCommit
	buildVersion, buildCommit = version, commit
	t.Cleanup(func() { buildVersion, buildCommit = oldVersion, oldCommit })
}

func testSystemUpdateStartRequest(manifest systemUpdateManifest, currentVersion, currentCommit string) transport.SystemUpdateStartRequest {
	return transport.SystemUpdateStartRequest{
		RequestID: strings.Repeat("1", 32), TargetVersion: manifest.Version, TargetCommit: manifest.Commit,
		TargetSequence: manifest.Sequence, TargetOS: manifest.OS, TargetArch: manifest.Arch,
		TargetArchiveSHA256: manifest.ArchiveSHA256, TargetArchiveSize: manifest.ArchiveSize,
		ExpectedCurrentVersion: currentVersion, ExpectedCurrentCommit: currentCommit,
	}
}

func TestSystemUpdateCheckAndStartBindFreshSignedTarget(t *testing.T) {
	currentCommit := strings.Repeat("c", 40)
	withSystemUpdateBuild(t, "v1.2.3-alpha.9", currentCommit)
	manifest := testSystemUpdateManifest()
	fetcher := &fakeSystemUpdateFetcher{version: manifest.Version, manifest: manifest}
	backend := &fakeSystemUpdateBackend{floor: &systemUpdateFloor{Sequence: "41", Version: "v1.2.3-alpha.9"}}
	service := newSystemUpdateService(fetcher, backend, "linux", "amd64")
	service.now = func() time.Time { return time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC) }
	check, err := service.check(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !check.Supported || !check.Available || check.TargetSequence != "42" || check.TargetArchiveSize != "2147483648" {
		t.Fatalf("unexpected check: %#v", check)
	}
	request := testSystemUpdateStartRequest(manifest, buildVersion, buildCommit)
	started, err := service.start(context.Background(), &request)
	if err != nil {
		t.Fatal(err)
	}
	if !started.Accepted || started.Status != systemUpdateQueued || backend.queued == nil {
		t.Fatalf("unexpected start: %#v", started)
	}
	if fetcher.fetchCalls != 2 {
		t.Fatalf("fetch calls = %d, want discovery fetch plus independent start re-fetch", fetcher.fetchCalls)
	}
	request.TargetCommit = strings.Repeat("d", 40)
	if _, err := service.start(context.Background(), &request); err == nil {
		t.Fatal("manifest mismatch accepted")
	}
}

func TestSystemUpdateStartRejectsUnknownCurrentBuildAndRollback(t *testing.T) {
	manifest := testSystemUpdateManifest()
	backend := &fakeSystemUpdateBackend{}
	fetcher := &fakeSystemUpdateFetcher{version: manifest.Version, manifest: manifest}
	withSystemUpdateBuild(t, "dev", "unknown")
	service := newSystemUpdateService(fetcher, backend, "linux", "amd64")
	request := testSystemUpdateStartRequest(manifest, "v1.2.3-alpha.9", strings.Repeat("c", 40))
	if _, err := service.start(context.Background(), &request); err == nil {
		t.Fatal("dev build started an update")
	}
	if backend.queued != nil || fetcher.fetchCalls != 0 {
		t.Fatal("dev build reached fetch or launch")
	}
}

func TestSystemUpdateCheckRejectsMissingTrustedFloor(t *testing.T) {
	currentCommit := strings.Repeat("c", 40)
	withSystemUpdateBuild(t, "v1.2.3-alpha.9", currentCommit)
	manifest := testSystemUpdateManifest()
	backend := &fakeSystemUpdateBackend{}
	service := newSystemUpdateService(&fakeSystemUpdateFetcher{version: manifest.Version, manifest: manifest}, backend, "linux", "amd64")
	if _, err := service.check(context.Background()); err == nil {
		t.Fatal("check accepted a release without pre-provisioned trusted floor")
	}
	request := testSystemUpdateStartRequest(manifest, buildVersion, buildCommit)
	if _, err := service.start(context.Background(), &request); err == nil {
		t.Fatal("start accepted a release without pre-provisioned trusted floor")
	}
	if backend.queued != nil {
		t.Fatal("missing-floor request reached durable queue")
	}
}

func TestSystemUpdateStartExactReplayPrecedesCurrentBuildComparison(t *testing.T) {
	manifest := testSystemUpdateManifest()
	oldVersion, oldCommit := "v1.2.3-alpha.9", strings.Repeat("c", 40)
	request := testSystemUpdateStartRequest(manifest, oldVersion, oldCommit)
	state := &systemUpdateState{
		Version: systemUpdateStateVersion, RequestID: request.RequestID, Status: systemUpdateSucceeded,
		TargetVersion: request.TargetVersion, TargetCommit: request.TargetCommit, TargetSequence: request.TargetSequence,
		TargetOS: request.TargetOS, TargetArch: request.TargetArch, TargetArchiveSHA256: request.TargetArchiveSHA256,
		TargetArchiveSize: request.TargetArchiveSize, ExpectedCurrentVersion: request.ExpectedCurrentVersion,
		ExpectedCurrentCommit: request.ExpectedCurrentCommit, CreatedAt: "2026-08-12T12:00:00Z",
		UpdatedAt: "2026-08-12T12:01:00Z",
	}
	backend := &fakeSystemUpdateBackend{status: state}
	fetcher := &fakeSystemUpdateFetcher{version: manifest.Version, manifest: manifest}
	withSystemUpdateBuild(t, manifest.Version, manifest.Commit)
	service := newSystemUpdateService(fetcher, backend, "linux", "amd64")
	response, err := service.start(context.Background(), &request)
	if err != nil {
		t.Fatal(err)
	}
	if !response.Accepted || response.Status != systemUpdateSucceeded {
		t.Fatalf("exact replay response = %#v", response)
	}
	if fetcher.fetchCalls != 0 || backend.queued != nil {
		t.Fatal("exact replay re-fetched or re-launched after the installed build changed")
	}
}

func TestSystemUpdateServiceUnsupportedAndStatusContracts(t *testing.T) {
	service := newSystemUpdateService(nil, nil, "windows", "amd64")
	if service.supported() {
		t.Fatal("Windows reported updater support")
	}
	state := &systemUpdateState{Version: systemUpdateStateVersion, RequestID: strings.Repeat("2", 32), Status: systemUpdateRunning, TargetVersion: "v1.2.3", TargetCommit: strings.Repeat("a", 40), TargetSequence: "7", TargetOS: "linux", TargetArch: "arm64", TargetArchiveSHA256: strings.Repeat("b", 64), TargetArchiveSize: "123", ExpectedCurrentVersion: "v1.2.2", ExpectedCurrentCommit: strings.Repeat("c", 40), CreatedAt: "2026-08-12T12:00:00Z", UpdatedAt: "2026-08-12T12:01:00Z"}
	response := systemUpdateStatusResponse(state)
	if !response.Found || response.TargetSequence != "7" || response.TargetArchiveSize != "123" || response.Status != systemUpdateRunning {
		t.Fatalf("status response = %#v", response)
	}
}
