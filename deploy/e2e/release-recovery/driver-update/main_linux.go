//go:build linux

// release-recovery-update drives the installed Agent's real signed updater only
// inside a nonce-bound disposable QEMU guest. It is not a deployment utility.
package main

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net/rpc"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
	"golang.org/x/sys/unix"
)

const labMarkerPath = "/etc/celikpanel-release-recovery-lab"
const labSchema = "celikpanel-release-recovery-lab/v1"
const signingKeyPath = "/etc/celikpanel/release-signing-ed25519.pem"
const fixtureReviewRoot = "/var/lib/celikpanel-release-recovery-driver"

var hex64 = regexp.MustCompile("^[0-9a-f]{64}$")
var hex40 = regexp.MustCompile("^[0-9a-f]{40}$")
var hex32 = regexp.MustCompile("^[0-9a-f]{32}$")
var uuidPattern = regexp.MustCompile("^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$")
var labelPattern = regexp.MustCompile("^[a-z0-9][a-z0-9_-]{0,95}$")

type markerIdentity struct {
	CellID, Node, Nonce, VMUUID string
}

type manifest struct {
	Sequence, Version, Commit, PublishedAt, OS, Arch, Archive, ArchiveSHA256, ArchiveSize string
}

func protectedRead(path string, maximum int64, marker bool) ([]byte, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	defer file.Close()
	var before, after unix.Stat_t
	if err := unix.Fstat(fd, &before); err != nil {
		return nil, err
	}
	if before.Mode&unix.S_IFMT != unix.S_IFREG || before.Uid != 0 || before.Nlink != 1 ||
		before.Mode&0o022 != 0 || before.Size <= 0 || before.Size > maximum {
		return nil, errors.New("unsafe fixture input type, owner, links, mode or size")
	}
	if marker && (before.Gid != 0 || before.Mode&0o7777 != 0o444) {
		return nil, errors.New("guest marker must be root:root mode 0444")
	}
	raw, err := io.ReadAll(io.LimitReader(file, maximum+1))
	if err != nil {
		return nil, err
	}
	if err := unix.Fstat(fd, &after); err != nil {
		return nil, err
	}
	if before.Dev != after.Dev || before.Ino != after.Ino || before.Size != after.Size ||
		before.Mode != after.Mode || before.Uid != after.Uid || before.Gid != after.Gid ||
		before.Nlink != after.Nlink || before.Mtim != after.Mtim || before.Ctim != after.Ctim ||
		int64(len(raw)) != before.Size {
		return nil, errors.New("fixture input changed during read")
	}
	return raw, nil
}

func parseMarker(raw []byte, nonce, uuid, vendor string) (markerIdentity, error) {
	var values map[string]string
	if err := json.Unmarshal(raw, &values); err != nil {
		return markerIdentity{}, errors.New("invalid guest marker")
	}
	if len(values) != 5 || values["schema"] != labSchema || !hex64.MatchString(nonce) || values["nonce"] != nonce ||
		!uuidPattern.MatchString(values["vm_uuid"]) || values["vm_uuid"] != strings.ToLower(strings.TrimSpace(uuid)) ||
		strings.TrimSpace(vendor) != "QEMU" || !labelPattern.MatchString(values["cell_id"]) || !labelPattern.MatchString(values["node"]) {
		return markerIdentity{}, errors.New("marker, nonce, QEMU vendor or DMI UUID does not match")
	}
	return markerIdentity{CellID: values["cell_id"], Node: values["node"], Nonce: nonce, VMUUID: values["vm_uuid"]}, nil
}

func guestGuard(nonce string) (markerIdentity, error) {
	if os.Geteuid() != 0 {
		return markerIdentity{}, errors.New("fixture requires root inside its marked disposable QEMU guest")
	}
	if os.Getenv("CELIKPANEL_AGENT_SOCKET") != "" || os.Getenv("CELIKPANEL_AGENT_TOKEN_FILE") != "" ||
		transport.AgentSocketPath() != "/run/celikpanel/agent.sock" || transport.AgentTokenPath() != "/etc/celikpanel/agent.token" {
		return markerIdentity{}, errors.New("Agent socket and token overrides are forbidden")
	}
	raw, err := protectedRead(labMarkerPath, 4096, true)
	if err != nil {
		return markerIdentity{}, fmt.Errorf("disposable guest marker: %w", err)
	}
	uuid, err := os.ReadFile("/sys/class/dmi/id/product_uuid")
	if err != nil {
		return markerIdentity{}, errors.New("guest DMI UUID unavailable")
	}
	vendor, err := os.ReadFile("/sys/class/dmi/id/sys_vendor")
	if err != nil {
		return markerIdentity{}, errors.New("guest vendor unavailable")
	}
	return parseMarker(raw, nonce, string(uuid), string(vendor))
}

func positiveDecimal(value string, maximum uint64) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0 && n <= maximum && strconv.FormatUint(n, 10) == value
}

// The fixture reads an unchanged published manifest. StartSystemUpdate still
// fetches and verifies it independently against the installed signing key and
// anti-rollback floor; this local review never replaces that admission check.
func parseManifest(raw []byte, targetArch string) (manifest, error) {
	var result manifest
	if len(raw) == 0 || len(raw) > 4096 || raw[len(raw)-1] != '\n' {
		return result, errors.New("invalid manifest size or terminator")
	}
	for _, char := range raw {
		if char != '\n' && (char < 0x20 || char > 0x7e) {
			return result, errors.New("manifest must use canonical ASCII and LF")
		}
	}
	lines := strings.Split(string(raw[:len(raw)-1]), "\n")
	keys := []string{"format", "sequence", "version", "commit", "published_at", "os", "arch", "archive", "archive_sha256", "archive_size"}
	if len(lines) != len(keys) {
		return result, errors.New("manifest must contain the ten canonical fields")
	}
	values := make([]string, len(keys))
	for i, key := range keys {
		if !strings.HasPrefix(lines[i], key+"=") {
			return result, errors.New("manifest field order is not canonical")
		}
		values[i] = strings.TrimPrefix(lines[i], key+"=")
	}
	result = manifest{Sequence: values[1], Version: values[2], Commit: values[3], PublishedAt: values[4], OS: values[5], Arch: values[6], Archive: values[7], ArchiveSHA256: values[8], ArchiveSize: values[9]}
	if values[0] != "celikpanel-release-manifest-v2" ||
		(result.Version != "v0.1.0-alpha.79" && result.Version != "v0.1.0-alpha.80") ||
		!positiveDecimal(result.Sequence, math.MaxInt64) || !hex40.MatchString(result.Commit) ||
		result.OS != "linux" || result.Arch != targetArch || (targetArch != "amd64" && targetArch != "arm64") ||
		result.Archive != "celikpanel-"+result.Version+"-"+result.OS+"-"+result.Arch+".tar.gz" ||
		!hex64.MatchString(result.ArchiveSHA256) || !positiveDecimal(result.ArchiveSize, 2147483648) {
		return result, errors.New("manifest is not a canonical Alpha79/Alpha80 target for this guest")
	}
	published, err := time.Parse("2006-01-02T15:04:05Z", result.PublishedAt)
	if err != nil || published.UTC().Format("2006-01-02T15:04:05Z") != result.PublishedAt {
		return result, errors.New("noncanonical manifest publication time")
	}
	return result, nil
}

func verifiedManifest(raw, signature, keyPEM []byte, arch string) (manifest, error) {
	block, remainder := pem.Decode(keyPEM)
	if block == nil || block.Type != "PUBLIC KEY" || len(block.Headers) != 0 || len(bytes.TrimSpace(remainder)) != 0 {
		return manifest{}, errors.New("installed signing key is not one public PEM block")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return manifest{}, errors.New("installed signing key is invalid")
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok || len(publicKey) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize || !ed25519.Verify(publicKey, raw, signature) {
		return manifest{}, errors.New("published manifest signature does not match the installed signing key")
	}
	return parseManifest(raw, arch)
}

type rpcCall func(context.Context, string, any, any) error

func callAgent(ctx context.Context, method string, request, response any) error {
	client, err := transport.ConnectAgentContext(ctx)
	if err != nil {
		return errors.New("authenticated local Agent connection unavailable")
	}
	defer client.Close()
	done := client.Go(method, request, response, make(chan *rpc.Call, 1)).Done
	select {
	case result := <-done:
		if result.Error != nil {
			return fmt.Errorf("Agent RPC %s failed; reconcile the same operation", method)
		}
		return nil
	case <-ctx.Done():
		_ = client.Close()
		<-done
		return errors.New("Agent RPC timed out; operation outcome is unknown")
	}
}

func requestIdentity(marker markerIdentity, target manifest) string {
	digest := sha256.Sum256([]byte("celikpanel/release-recovery-update/v1\x00" + marker.Nonce + "\x00" + marker.CellID + "\x00" + marker.Node + "\x00" + target.Version + "\x00" + target.Commit + "\x00" + target.ArchiveSHA256))
	return hex.EncodeToString(digest[:16])
}

func requestFor(marker markerIdentity, target manifest, current transport.SystemUpdateCheckResponse, requestID string) (transport.SystemUpdateStartRequest, error) {
	if !current.Supported || current.Error != "" || current.CurrentVersion == "" || !hex40.MatchString(current.CurrentCommit) {
		return transport.SystemUpdateStartRequest{}, errors.New("Agent did not verify its current identifiable release/update support")
	}
	if !hex32.MatchString(requestID) {
		return transport.SystemUpdateStartRequest{}, errors.New("request ID must be 32 lowercase hexadecimal characters")
	}
	return transport.SystemUpdateStartRequest{
		RequestID: requestID, TargetVersion: target.Version, TargetCommit: target.Commit,
		TargetSequence: target.Sequence, TargetOS: target.OS, TargetArch: target.Arch,
		TargetArchiveSHA256: target.ArchiveSHA256, TargetArchiveSize: target.ArchiveSize,
		ExpectedCurrentVersion: current.CurrentVersion, ExpectedCurrentCommit: current.CurrentCommit,
	}, nil
}

// This is client-side fixture intent, not an Agent operation or lease record.
// It is durable before Start so an interrupted driver can query the same ID.
func persistReview(marker markerIdentity, request transport.SystemUpdateStartRequest, manifestSHA string) error {
	raw, err := json.Marshal(map[string]any{
		"schema": "celikpanel-release-recovery-client-review/v1", "cell_id": marker.CellID,
		"node": marker.Node, "vm_uuid": marker.VMUUID, "request": request, "manifest_sha256": manifestSHA,
	})
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.Mkdir(fixtureReviewRoot, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return err
	}
	dirFD, err := unix.Open(fixtureReviewRoot, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
	if err != nil {
		return err
	}
	defer unix.Close(dirFD)
	var stat unix.Stat_t
	if err := unix.Fstat(dirFD, &stat); err != nil {
		return err
	}
	if stat.Uid != 0 || stat.Gid != 0 || stat.Mode&0o7777 != 0o700 {
		return errors.New("client fixture review directory must be root:root mode 0700")
	}
	name := request.RequestID + ".json"
	fd, err := unix.Openat(dirFD, name, unix.O_WRONLY|unix.O_CREAT|unix.O_EXCL|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0o600)
	if errors.Is(err, unix.EEXIST) {
		existing, readErr := protectedRead(filepath.Join(fixtureReviewRoot, name), 16384, false)
		if readErr != nil || !bytes.Equal(existing, raw) {
			return errors.New("request ID already belongs to a different client fixture review")
		}
		return nil
	}
	if err != nil {
		return err
	}
	file := os.NewFile(uintptr(fd), name)
	if err := unix.Fchown(fd, 0, 0); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Fsync(dirFD); err != nil {
		return err
	}
	parent, err := os.Open(filepath.Dir(fixtureReviewRoot))
	if err != nil {
		return err
	}
	defer parent.Close()
	return parent.Sync()
}
func statusMatches(status transport.SystemUpdateStatusResponse, requestID string, target manifest) bool {
	return status.Found && status.RequestID == requestID && status.TargetVersion == target.Version &&
		status.TargetCommit == target.Commit && status.TargetSequence == target.Sequence &&
		status.TargetOS == target.OS && status.TargetArch == target.Arch &&
		status.TargetArchiveSHA256 == target.ArchiveSHA256 && status.TargetArchiveSize == target.ArchiveSize
}

func boundedDetail(value string) string {
	var cleaned strings.Builder
	for _, char := range value {
		if char >= 0x20 && char <= 0x7e {
			if cleaned.Len() >= 1024 {
				break
			}
			cleaned.WriteRune(char)
		}
	}
	return cleaned.String()
}

func emit(event string, marker markerIdentity, fields map[string]any) {
	if fields == nil {
		fields = make(map[string]any)
	}
	fields["schema"], fields["event"] = "celikpanel-release-recovery-update/v1", event
	fields["cell_id"], fields["node"] = marker.CellID, marker.Node
	_ = json.NewEncoder(os.Stdout).Encode(fields)
}

func inspect(ctx context.Context, call rpcCall, requestID string, target manifest) (transport.SystemUpdateStatusResponse, error) {
	var status transport.SystemUpdateStatusResponse
	if err := call(ctx, "Agent.SystemUpdateStatus", &transport.SystemUpdateStatusRequest{RequestID: requestID}, &status); err != nil {
		return status, err
	}
	if !status.Found {
		return status, errors.New("exact update status is not found; absence does not prove an unconfirmed start was rejected")
	}
	if !statusMatches(status, requestID, target) {
		return status, errors.New("Agent returned a different update identity")
	}
	switch status.Status {
	case "queued", "running", "succeeded", "failed":
	default:
		return status, errors.New("Agent returned an unsupported update status")
	}
	return status, nil
}

func reviewedStart(ctx context.Context, call rpcCall, request transport.SystemUpdateStartRequest) (transport.SystemUpdateStartResponse, error) {
	var response transport.SystemUpdateStartResponse
	// Deliberately one call: transport uncertainty never triggers another Start,
	// Abandon, a direct worker launch, or a fabricated operation record.
	if err := call(ctx, "Agent.StartSystemUpdate", &request, &response); err != nil {
		return response, err
	}
	if !response.Accepted || response.Error != "" {
		return response, fmt.Errorf("Agent did not accept the reviewed update: %s", boundedDetail(response.Error))
	}
	return response, nil
}

func run(ctx context.Context, nonce, manifestPath, signaturePath, mode, explicitRequestID string) error {
	marker, err := guestGuard(nonce)
	if err != nil {
		return err
	}
	raw, err := protectedRead(manifestPath, 4096, false)
	if err != nil {
		return fmt.Errorf("published target manifest: %w", err)
	}
	signature, err := protectedRead(signaturePath, 64, false)
	if err != nil {
		return fmt.Errorf("published target signature: %w", err)
	}
	key, err := protectedRead(signingKeyPath, 4096, false)
	if err != nil {
		return fmt.Errorf("installed signing key: %w", err)
	}
	target, err := verifiedManifest(raw, signature, key, runtime.GOARCH)
	if err != nil {
		return err
	}
	requestID := requestIdentity(marker, target)
	if explicitRequestID != "" {
		if !hex32.MatchString(explicitRequestID) {
			return errors.New("request ID must be 32 lowercase hexadecimal characters")
		}
		requestID = explicitRequestID
	}
	manifestDigest := sha256.Sum256(raw)
	base := map[string]any{"request_id": requestID, "target_version": target.Version, "target_commit": target.Commit, "target_archive_sha256": target.ArchiveSHA256, "manifest_sha256": hex.EncodeToString(manifestDigest[:])}
	if mode == "status" {
		status, err := inspect(ctx, callAgent, requestID, target)
		if err != nil {
			emit("unconfirmed", marker, base)
			return err
		}
		base["status"], base["updated_at"], base["detail"] = status.Status, status.UpdatedAt, boundedDetail(status.Error)
		emit("status", marker, base)
		return nil
	}
	var current transport.SystemUpdateCheckResponse
	if err := callAgent(ctx, "Agent.CheckSystemUpdate", &transport.Empty{}, &current); err != nil {
		return err
	}
	request, err := requestFor(marker, target, current, requestID)
	if err != nil {
		return err
	}
	base["request"] = request
	emit("reviewed", marker, base)
	if mode == "preview" {
		return nil
	}
	if err := persistReview(marker, request, hex.EncodeToString(manifestDigest[:])); err != nil {
		return fmt.Errorf("persist client fixture review before start: %w", err)
	}
	response, err := reviewedStart(ctx, callAgent, request)
	if err != nil {
		emit("unconfirmed", marker, map[string]any{"request_id": requestID, "detail": "Inspect this exact operation; no automatic retry or alternate launch was attempted."})
		return err
	}
	emit("accepted", marker, map[string]any{"request_id": requestID, "status": response.Status, "target_version": target.Version, "target_commit": target.Commit, "target_archive_sha256": target.ArchiveSHA256})
	return nil
}

func main() {
	nonce := flag.String("nonce", "", "exact disposable guest nonce")
	manifestPath := flag.String("manifest", "", "root-owned unchanged published release-manifest-v2")
	signaturePath := flag.String("signature", "", "root-owned published Ed25519 manifest signature")
	mode := flag.String("mode", "preview", "preview, start or status")
	requestID := flag.String("request-id", "", "controller-persisted exact request ID (32 lowercase hex); otherwise derived from guest and target")
	flag.Parse()
	if flag.NArg() != 0 || *manifestPath == "" || *signaturePath == "" || (*mode != "preview" && *mode != "start" && *mode != "status") {
		fmt.Fprintln(os.Stderr, "required: --nonce --manifest --signature [--request-id HEX32] [--mode preview|start|status]")
		os.Exit(2)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	if err := run(ctx, *nonce, *manifestPath, *signaturePath, *mode, *requestID); err != nil {
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"schema": "celikpanel-release-recovery-update/v1", "event": "stopped", "detail": boundedDetail(err.Error())})
		os.Exit(1)
	}
}
