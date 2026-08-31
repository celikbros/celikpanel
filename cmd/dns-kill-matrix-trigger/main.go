package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/rpc"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	scenarioSchema                     = "celikpanel-dns-kill-matrix-trigger/v1"
	identityReceiptSchema              = "celikpanel-dns-kill-matrix-trigger-identity/v1"
	normalizationIdentityReceiptSchema = "celikpanel-dns-kill-matrix-pdns-normalization-identity/v1"

	environmentCellID    = "CELIKPANEL_S1_CELL_ID"
	environmentDriver    = "CELIKPANEL_S1_DRIVER"
	environmentRequestID = "CELIKPANEL_S1_REQUEST_ID"

	mutationKind              = "dns_engine_switch"
	mutationKindPDNSConfigure = "pdns_configure"
	mutationKindDNSZoneSync   = "dns_zone_sync"
	mutationRunning           = "running"
	mutationSucceeded         = "succeeded"
	mutationFailed            = "failed"
	mutationLeasedPhase       = "leased"
	mutationInterruptedPhase  = "interrupted"

	restartBeforeSwitchCommitCode       = "agent_restarted_before_dns_engine_switch_commit"
	restartBeforeSwitchCommitMessage    = "The agent restarted before the DNS engine switch reached a verified target."
	switchRolledBackAfterRestartCode    = "dns_engine_switch_rolled_back_after_restart"
	switchRolledBackAfterRestartMessage = "The interrupted DNS engine switch was rolled back to the verified previous state."
	triggerFailureCode                  = "dns_kill_matrix_trigger_failed"
	triggerFailureMessage               = "The DNS kill-matrix trigger ended before a verified target receipt."

	heartbeatIntervalDefault = 5 * time.Second
	operationTimeoutDefault  = 45 * time.Minute
	controlTimeout           = 15 * time.Second

	exitUsage       = 64
	exitUncertain   = 75
	exitDefiniteErr = 1
)

var errSwitchOutcomeUncertain = errors.New(
	"DNS engine switch RPC ended without an exact terminal receipt",
)

type scenario struct {
	Schema         string                                  `json:"schema"`
	Driver         string                                  `json:"driver"`
	SourceFixture  string                                  `json:"source_fixture"`
	Mode           string                                  `json:"mode"`
	SourceEngine   transport.DNSEngine                     `json:"source_engine,omitempty"`
	TargetEngine   transport.DNSEngine                     `json:"target_engine"`
	SourceEpoch    int64                                   `json:"source_epoch"`
	TargetEpoch    int64                                   `json:"target_epoch"`
	SourceRevision int64                                   `json:"source_revision"`
	Topology       string                                  `json:"topology"`
	PairRole       string                                  `json:"pair_role,omitempty"`
	LocalIP        string                                  `json:"local_ip,omitempty"`
	LocalNS        string                                  `json:"local_ns,omitempty"`
	PeerIP         string                                  `json:"peer_ip,omitempty"`
	PeerNS         string                                  `json:"peer_ns,omitempty"`
	Zones          []transport.DNSEngineSwitchZoneSnapshot `json:"zones"`
}

type triggerEvent struct {
	Event             string `json:"event"`
	Driver            string `json:"driver,omitempty"`
	SourceFixture     string `json:"source_fixture,omitempty"`
	IdentityReceipt   string `json:"identity_receipt,omitempty"`
	RequestID         string `json:"request_id,omitempty"`
	OwnerID           string `json:"owner_id,omitempty"`
	ManifestQualifier string `json:"manifest_qualifier,omitempty"`
	TargetEngine      string `json:"target_engine,omitempty"`
	HeartbeatCount    int    `json:"heartbeat_count,omitempty"`
	Error             string `json:"error,omitempty"`
}

type identityReceipt struct {
	Schema            string `json:"schema"`
	CellID            string `json:"cell_id"`
	Driver            string `json:"driver"`
	SourceFixture     string `json:"source_fixture"`
	RequestID         string `json:"request_id"`
	OwnerID           string `json:"owner_id"`
	ManifestQualifier string `json:"manifest_qualifier"`
}

type normalizationMutationIdentity struct {
	Method        string `json:"method"`
	RequestID     string `json:"request_id"`
	OwnerID       string `json:"owner_id"`
	Kind          string `json:"kind"`
	Target        string `json:"target"`
	PackageName   string `json:"package_name"`
	TerminalPhase string `json:"terminal_phase"`
}

type normalizationZoneSyncIdentity struct {
	normalizationMutationIdentity
	Engine            string `json:"engine"`
	EngineEpoch       int64  `json:"engine_epoch"`
	DesiredGeneration int64  `json:"desired_generation"`
	Domain            string `json:"domain"`
	Delete            bool   `json:"delete"`
	ZoneType          string `json:"zone_type"`
	Qualifier         string `json:"qualifier"`
}

type normalizationIdentityReceipt struct {
	Schema        string                          `json:"schema"`
	CellID        string                          `json:"cell_id"`
	Driver        string                          `json:"driver"`
	SourceFixture string                          `json:"source_fixture"`
	BaseRequestID string                          `json:"base_request_id"`
	SourceEngine  string                          `json:"source_engine"`
	SourceEpoch   int64                           `json:"source_epoch"`
	Configure     normalizationMutationIdentity   `json:"configure"`
	ZoneSyncs     []normalizationZoneSyncIdentity `json:"zone_syncs"`
}

type rpcCallFunc func(context.Context, string, any, any) error

type heartbeatResult struct {
	count    int
	terminal *transport.ServiceMutationJob
	err      error
}

type triggerResult struct {
	heartbeatCount int
	terminal       *transport.ServiceMutationJob
}

func main() {
	if len(os.Args) < 2 {
		usageError("a subcommand is required")
	}
	switch os.Args[1] {
	case "rpc-switch":
		runRPCSwitchCommand(os.Args[2:], false)
	case "rpc-retry":
		runRPCSwitchCommand(os.Args[2:], true)
	case "rpc-normalize-pdns":
		runRPCNormalizePDNSCommand(os.Args[2:])
	default:
		usageError(fmt.Sprintf("unsupported subcommand %q", os.Args[1]))
	}
}

func usageError(message string) {
	_, _ = fmt.Fprintln(os.Stderr, message)
	_, _ = fmt.Fprintln(
		os.Stderr,
		"usage: dns-kill-matrix-trigger {rpc-switch|rpc-retry} --scenario FILE --identity-receipt FILE [--timeout 45m] | rpc-normalize-pdns --scenario FILE --normalization-receipt FILE [--timeout 45m]",
	)
	os.Exit(exitUsage)
}

func runRPCSwitchCommand(arguments []string, retry bool) {
	commandName := "rpc-switch"
	if retry {
		commandName = "rpc-retry"
	}
	flags := flag.NewFlagSet(commandName, flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scenarioPath := flags.String("scenario", "", "strict JSON scenario manifest")
	identityPath := flags.String("identity-receipt", "", "durable trigger identity receipt")
	timeout := flags.Duration("timeout", operationTimeoutDefault, "whole switch timeout")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*scenarioPath) == "" ||
		strings.TrimSpace(*identityPath) == "" || *timeout <= 0 {
		usageError(commandName + " arguments are invalid")
	}

	cellID, driver, requestID, err := environmentIdentity()
	if err != nil {
		emitAndExit(triggerEvent{Event: "identity-rejected", Error: err.Error()}, exitUsage)
	}
	loaded, request, err := loadScenario(*scenarioPath, driver)
	if err != nil {
		emitAndExit(triggerEvent{
			Event: "scenario-rejected", Driver: driver, RequestID: requestID,
			Error: err.Error(),
		}, exitUsage)
	}
	receipt, err := triggerIdentity(
		*identityPath, retry, cellID, driver, loaded.SourceFixture,
		requestID, request.ManifestQualifier,
	)
	if err != nil {
		emitAndExit(triggerEvent{
			Event: "identity-receipt-rejected", Driver: driver, RequestID: requestID,
			IdentityReceipt: *identityPath,
			Error:           err.Error(),
		}, exitDefiniteErr)
	}
	ownerID := receipt.OwnerID
	emit(triggerEvent{
		Event: commandName + "-start", Driver: loaded.Driver,
		SourceFixture:   loaded.SourceFixture,
		IdentityReceipt: *identityPath,
		RequestID:       requestID, OwnerID: ownerID,
		ManifestQualifier: request.ManifestQualifier,
		TargetEngine:      string(request.TargetEngine),
	})

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	result, err := runRPCSwitch(
		ctx, requestID, ownerID, request, retry, heartbeatIntervalDefault,
		callProductionAgent,
	)
	if err != nil {
		code := exitDefiniteErr
		if errors.Is(err, errSwitchOutcomeUncertain) {
			code = exitUncertain
		}
		emitAndExit(triggerEvent{
			Event: commandName + "-ended", Driver: loaded.Driver,
			SourceFixture:   loaded.SourceFixture,
			IdentityReceipt: *identityPath,
			RequestID:       requestID, OwnerID: ownerID,
			ManifestQualifier: request.ManifestQualifier,
			TargetEngine:      string(request.TargetEngine),
			HeartbeatCount:    result.heartbeatCount,
			Error:             err.Error(),
		}, code)
	}
	emit(triggerEvent{
		Event: commandName + "-complete", Driver: loaded.Driver,
		SourceFixture:   loaded.SourceFixture,
		IdentityReceipt: *identityPath,
		RequestID:       requestID, OwnerID: ownerID,
		ManifestQualifier: request.ManifestQualifier,
		TargetEngine:      string(request.TargetEngine),
		HeartbeatCount:    result.heartbeatCount,
	})
}

func runRPCNormalizePDNSCommand(arguments []string) {
	flags := flag.NewFlagSet("rpc-normalize-pdns", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	scenarioPath := flags.String("scenario", "", "strict JSON scenario manifest")
	receiptPath := flags.String(
		"normalization-receipt", "", "durable PowerDNS normalization identity receipt",
	)
	timeout := flags.Duration("timeout", operationTimeoutDefault, "whole normalization timeout")
	if err := flags.Parse(arguments); err != nil || flags.NArg() != 0 ||
		strings.TrimSpace(*scenarioPath) == "" || strings.TrimSpace(*receiptPath) == "" ||
		*timeout <= 0 {
		usageError("rpc-normalize-pdns arguments are invalid")
	}
	if err := requireNewIdentityReceiptPath(*receiptPath); err != nil {
		emitAndExit(triggerEvent{
			Event: "normalization-receipt-rejected", IdentityReceipt: *receiptPath,
			Error: err.Error(),
		}, exitDefiniteErr)
	}
	cellID, driver, baseRequestID, err := environmentIdentity()
	if err != nil {
		emitAndExit(triggerEvent{Event: "identity-rejected", Error: err.Error()}, exitUsage)
	}
	loaded, request, err := loadScenario(*scenarioPath, driver)
	if err != nil {
		emitAndExit(triggerEvent{
			Event: "scenario-rejected", Driver: driver, RequestID: baseRequestID,
			Error: err.Error(),
		}, exitUsage)
	}
	receipt, err := buildPDNSNormalizationIdentity(
		cellID, driver, baseRequestID, loaded, request,
	)
	if err != nil {
		emitAndExit(triggerEvent{
			Event: "normalization-scenario-rejected", Driver: driver,
			SourceFixture: loaded.SourceFixture, RequestID: baseRequestID,
			Error: err.Error(),
		}, exitUsage)
	}
	emit(triggerEvent{
		Event: "rpc-normalize-pdns-start", Driver: driver,
		SourceFixture: loaded.SourceFixture, IdentityReceipt: *receiptPath,
		RequestID: baseRequestID, TargetEngine: string(transport.DNSEnginePowerDNS),
	})
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	heartbeats, err := runPDNSNormalization(
		ctx, receipt, request, heartbeatIntervalDefault, callProductionAgent,
	)
	if err != nil {
		emitAndExit(triggerEvent{
			Event: "rpc-normalize-pdns-ended", Driver: driver,
			SourceFixture: loaded.SourceFixture, IdentityReceipt: *receiptPath,
			RequestID: baseRequestID, TargetEngine: string(transport.DNSEnginePowerDNS),
			HeartbeatCount: heartbeats, Error: err.Error(),
		}, exitDefiniteErr)
	}
	if err := publishNormalizationIdentityReceipt(*receiptPath, receipt); err != nil {
		emitAndExit(triggerEvent{
			Event: "normalization-receipt-publication-failed", Driver: driver,
			SourceFixture: loaded.SourceFixture, IdentityReceipt: *receiptPath,
			RequestID: baseRequestID, HeartbeatCount: heartbeats, Error: err.Error(),
		}, exitDefiniteErr)
	}
	emit(triggerEvent{
		Event: "rpc-normalize-pdns-complete", Driver: driver,
		SourceFixture: loaded.SourceFixture, IdentityReceipt: *receiptPath,
		RequestID: baseRequestID, TargetEngine: string(transport.DNSEnginePowerDNS),
		HeartbeatCount: heartbeats,
	})
}

func buildPDNSNormalizationIdentity(
	cellID, driver, baseRequestID string,
	loaded scenario,
	request transport.SwitchDNSEngineV1Request,
) (normalizationIdentityReceipt, error) {
	if !validCellID(cellID) || !validMutationIdentity(baseRequestID) ||
		driver != "bind" || loaded.Driver != driver || loaded.SourceFixture != "managed-pdns" ||
		loaded.Mode != transport.DNSEngineSwitchModeSwitch ||
		loaded.SourceEngine != transport.DNSEnginePowerDNS || loaded.SourceEpoch <= 0 ||
		loaded.TargetEngine != transport.DNSEngineBIND ||
		loaded.Topology != transport.DNSTopologyStandalone || len(request.Zones) == 0 ||
		request.SourceEngine != transport.DNSEnginePowerDNS ||
		request.SourceEpoch != loaded.SourceEpoch || request.TargetEngine != transport.DNSEngineBIND {
		return normalizationIdentityReceipt{}, errors.New(
			"PowerDNS normalization requires an exact managed-pdns standalone BIND switch source",
		)
	}
	configureRequestID := deriveNormalizationRequestIdentity(baseRequestID, "configure")
	configureOwnerID, err := deterministicOwnerIdentity(cellID, configureRequestID)
	if err != nil {
		return normalizationIdentityReceipt{}, err
	}
	receipt := normalizationIdentityReceipt{
		Schema: normalizationIdentityReceiptSchema, CellID: cellID, Driver: driver,
		SourceFixture: loaded.SourceFixture, BaseRequestID: baseRequestID,
		SourceEngine: string(transport.DNSEnginePowerDNS), SourceEpoch: loaded.SourceEpoch,
		Configure: normalizationMutationIdentity{
			Method: "Agent.ConfigurePowerDNSSQLite", RequestID: configureRequestID,
			OwnerID: configureOwnerID, Kind: mutationKindPDNSConfigure,
			Target: "pdns", PackageName: "", TerminalPhase: "completed",
		},
		ZoneSyncs: make([]normalizationZoneSyncIdentity, 0, len(request.Zones)),
	}
	for index, zone := range request.Zones {
		commitment, err := mutationpayload.CanonicalDNSZoneSyncV3(
			transport.DNSEnginePowerDNS, loaded.SourceEpoch, zone.DesiredGeneration,
			zone.Domain, zone.Delete, zone.ZoneType, zone.Records,
		)
		if err != nil {
			return normalizationIdentityReceipt{}, errors.New(
				"PowerDNS normalization source zone is not its exact canonical V3 snapshot",
			)
		}
		purpose := fmt.Sprintf("zone-sync/%d/%s", index, commitment.Domain)
		requestID := deriveNormalizationRequestIdentity(baseRequestID, purpose)
		ownerID, err := deterministicOwnerIdentity(cellID, requestID)
		if err != nil {
			return normalizationIdentityReceipt{}, err
		}
		terminalPhase := "commit/dns-zone-sync/v3/published/" + requestID + "/" +
			commitment.Domain + "/" + commitment.Qualifier
		receipt.ZoneSyncs = append(receipt.ZoneSyncs, normalizationZoneSyncIdentity{
			normalizationMutationIdentity: normalizationMutationIdentity{
				Method: "Agent.SyncDNSZoneV3", RequestID: requestID, OwnerID: ownerID,
				Kind: mutationKindDNSZoneSync, Target: commitment.Domain,
				PackageName: commitment.Qualifier, TerminalPhase: terminalPhase,
			},
			Engine: string(transport.DNSEnginePowerDNS), EngineEpoch: loaded.SourceEpoch,
			DesiredGeneration: commitment.DesiredGeneration, Domain: commitment.Domain,
			Delete: commitment.Delete, ZoneType: commitment.ZoneType,
			Qualifier: commitment.Qualifier,
		})
	}
	return receipt, nil
}

func deriveNormalizationRequestIdentity(baseRequestID, purpose string) string {
	digest := sha256.Sum256([]byte(
		"celikpanel/dns-kill-matrix-pdns-normalization-request/v1\x00" +
			baseRequestID + "\x00" + purpose,
	))
	return hex.EncodeToString(digest[:16])
}

func emit(event triggerEvent) {
	encoded, err := json.Marshal(event)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "encode trigger event: %v\n", err)
		return
	}
	_, _ = os.Stdout.Write(append(encoded, '\n'))
}

func emitAndExit(event triggerEvent, code int) {
	emit(event)
	os.Exit(code)
}

func environmentIdentity() (string, string, string, error) {
	cellID := os.Getenv(environmentCellID)
	driver := os.Getenv(environmentDriver)
	requestID := os.Getenv(environmentRequestID)
	if !validCellID(cellID) {
		return "", "", "", fmt.Errorf("%s is missing or noncanonical", environmentCellID)
	}
	if strings.TrimSpace(driver) != driver || driver == "" {
		return "", "", "", fmt.Errorf("%s is missing or noncanonical", environmentDriver)
	}
	if !validMutationIdentity(requestID) {
		return "", "", "", fmt.Errorf(
			"%s must be exactly 32 lowercase hexadecimal characters",
			environmentRequestID,
		)
	}
	return cellID, driver, requestID, nil
}

func validCellID(value string) bool {
	if len(value) == 0 || len(value) > 192 ||
		!((value[0] >= 'a' && value[0] <= 'z') ||
			(value[0] >= '0' && value[0] <= '9')) {
		return false
	}
	for index := 0; index < len(value); index++ {
		character := value[index]
		if (character >= 'a' && character <= 'z') ||
			(character >= '0' && character <= '9') ||
			character == '.' || character == '_' || character == ':' ||
			character == '-' {
			continue
		}
		return false
	}
	return true
}

func validMutationIdentity(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func deterministicOwnerIdentity(cellID, requestID string) (string, error) {
	if !validCellID(cellID) || !validMutationIdentity(requestID) {
		return "", errors.New("cannot derive owner from an invalid cell or request identity")
	}
	digest := sha256.Sum256([]byte(
		"celikpanel/dns-kill-matrix-owner/v1\x00" + requestID + "\x00" + cellID,
	))
	return hex.EncodeToString(digest[:16]), nil
}

func triggerIdentity(
	path string,
	retry bool,
	cellID, driver, sourceFixture, requestID, qualifier string,
) (identityReceipt, error) {
	ownerID, err := deterministicOwnerIdentity(cellID, requestID)
	if err != nil {
		return identityReceipt{}, err
	}
	expected := identityReceipt{
		Schema:            identityReceiptSchema,
		CellID:            cellID,
		Driver:            driver,
		SourceFixture:     sourceFixture,
		RequestID:         requestID,
		OwnerID:           ownerID,
		ManifestQualifier: qualifier,
	}
	if !mutationpayload.ValidDNSEngineSwitchQualifier(qualifier) {
		return identityReceipt{}, errors.New("trigger identity has an invalid manifest qualifier")
	}
	if retry {
		observed, err := readIdentityReceipt(path)
		if err != nil {
			return identityReceipt{}, fmt.Errorf("read retry identity receipt: %w", err)
		}
		if observed != expected {
			return identityReceipt{}, errors.New("retry identity receipt differs from the exact cell manifest")
		}
		return expected, nil
	}
	if err := publishIdentityReceipt(path, expected); err != nil {
		return identityReceipt{}, fmt.Errorf("publish initial identity receipt: %w", err)
	}
	return expected, nil
}

func canonicalIdentityReceipt(receipt identityReceipt) ([]byte, error) {
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func validateIdentityReceiptPath(path string) (string, os.FileInfo, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path {
		return "", nil, errors.New("identity receipt path must be clean and absolute")
	}
	parent := filepath.Dir(clean)
	parentInfo, err := os.Lstat(parent)
	if err != nil {
		return "", nil, fmt.Errorf("inspect identity receipt parent: %w", err)
	}
	if parentInfo.Mode()&os.ModeSymlink != 0 || !parentInfo.IsDir() {
		return "", nil, errors.New("identity receipt parent is not a real directory")
	}
	return clean, parentInfo, nil
}

func requireNewIdentityReceiptPath(path string) error {
	clean, _, err := validateIdentityReceiptPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(clean); err == nil {
		return errors.New("identity receipt already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect identity receipt target: %w", err)
	}
	return nil
}

func publishIdentityReceipt(path string, receipt identityReceipt) error {
	clean, parentInfo, err := validateIdentityReceiptPath(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(clean); err == nil {
		return errors.New("initial identity receipt already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect identity receipt target: %w", err)
	}
	encoded, err := canonicalIdentityReceipt(receipt)
	if err != nil {
		return fmt.Errorf("encode identity receipt: %w", err)
	}
	parent := filepath.Dir(clean)
	directory, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open identity receipt parent: %w", err)
	}
	defer directory.Close()
	openedParent, err := directory.Stat()
	if err != nil || !os.SameFile(parentInfo, openedParent) {
		return errors.New("identity receipt parent changed while opening")
	}
	stage, err := os.CreateTemp(parent, ".dns-kill-trigger-identity-*")
	if err != nil {
		return fmt.Errorf("create identity receipt stage: %w", err)
	}
	stagePath := stage.Name()
	stageClosed := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
		_ = os.Remove(stagePath)
	}()
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("secure identity receipt stage: %w", err)
	}
	if _, err := stage.Write(encoded); err != nil {
		return fmt.Errorf("write identity receipt stage: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync identity receipt stage: %w", err)
	}
	if err := stage.Close(); err != nil {
		stageClosed = true
		return fmt.Errorf("close identity receipt stage: %w", err)
	}
	stageClosed = true
	if err := os.Link(stagePath, clean); err != nil {
		return fmt.Errorf("publish identity receipt without replacement: %w", err)
	}
	if err := os.Remove(stagePath); err != nil {
		return fmt.Errorf("retire identity receipt stage: %w", err)
	}
	if current, err := os.Lstat(parent); err != nil || !os.SameFile(parentInfo, current) {
		return errors.New("identity receipt parent changed during publication")
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync identity receipt parent: %w", err)
	}
	observed, err := readIdentityReceipt(clean)
	if err != nil {
		return fmt.Errorf("reread identity receipt: %w", err)
	}
	if observed != receipt {
		return errors.New("identity receipt changed after publication")
	}
	return nil
}

func canonicalNormalizationIdentityReceipt(
	receipt normalizationIdentityReceipt,
) ([]byte, error) {
	if receipt.Schema != normalizationIdentityReceiptSchema ||
		!validCellID(receipt.CellID) || receipt.Driver != "bind" ||
		receipt.SourceFixture != "managed-pdns" ||
		!validMutationIdentity(receipt.BaseRequestID) ||
		receipt.SourceEngine != string(transport.DNSEnginePowerDNS) ||
		receipt.SourceEpoch <= 0 || len(receipt.ZoneSyncs) == 0 {
		return nil, errors.New("normalization identity receipt envelope is invalid")
	}
	configureRequestID := deriveNormalizationRequestIdentity(
		receipt.BaseRequestID, "configure",
	)
	configureOwnerID, err := deterministicOwnerIdentity(receipt.CellID, configureRequestID)
	if err != nil {
		return nil, err
	}
	if receipt.Configure != (normalizationMutationIdentity{
		Method: "Agent.ConfigurePowerDNSSQLite", RequestID: configureRequestID,
		OwnerID: configureOwnerID, Kind: mutationKindPDNSConfigure,
		Target: "pdns", PackageName: "", TerminalPhase: "completed",
	}) {
		return nil, errors.New("normalization configuration identity is invalid")
	}
	for index, zone := range receipt.ZoneSyncs {
		requestID := deriveNormalizationRequestIdentity(
			receipt.BaseRequestID, fmt.Sprintf("zone-sync/%d/%s", index, zone.Domain),
		)
		ownerID, err := deterministicOwnerIdentity(receipt.CellID, requestID)
		if err != nil {
			return nil, err
		}
		expectedPhase := "commit/dns-zone-sync/v3/published/" + requestID + "/" +
			zone.Domain + "/" + zone.Qualifier
		if zone.Method != "Agent.SyncDNSZoneV3" || zone.RequestID != requestID ||
			zone.OwnerID != ownerID || zone.Kind != mutationKindDNSZoneSync ||
			zone.Target != zone.Domain || zone.PackageName != zone.Qualifier ||
			zone.TerminalPhase != expectedPhase ||
			zone.Engine != string(transport.DNSEnginePowerDNS) ||
			zone.EngineEpoch != receipt.SourceEpoch || zone.Domain == "" ||
			zone.DesiredGeneration < 0 ||
			(zone.ZoneType != "NATIVE" && zone.ZoneType != "MASTER") ||
			!mutationpayload.ValidDNSZoneSyncV3Qualifier(zone.Qualifier) {
			return nil, errors.New("normalization zone-sync identity is invalid")
		}
	}
	encoded, err := json.Marshal(receipt)
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func publishNormalizationIdentityReceipt(
	path string,
	receipt normalizationIdentityReceipt,
) error {
	clean, parentInfo, err := validateIdentityReceiptPath(path)
	if err != nil {
		return err
	}
	if err := requireNewIdentityReceiptPath(clean); err != nil {
		return err
	}
	encoded, err := canonicalNormalizationIdentityReceipt(receipt)
	if err != nil {
		return fmt.Errorf("encode normalization identity receipt: %w", err)
	}
	parent := filepath.Dir(clean)
	directory, err := os.Open(parent)
	if err != nil {
		return fmt.Errorf("open normalization receipt parent: %w", err)
	}
	defer directory.Close()
	openedParent, err := directory.Stat()
	if err != nil || !os.SameFile(parentInfo, openedParent) {
		return errors.New("normalization receipt parent changed while opening")
	}
	stage, err := os.CreateTemp(parent, ".dns-kill-pdns-normalization-*")
	if err != nil {
		return fmt.Errorf("create normalization receipt stage: %w", err)
	}
	stagePath := stage.Name()
	stageClosed := false
	defer func() {
		if !stageClosed {
			_ = stage.Close()
		}
		_ = os.Remove(stagePath)
	}()
	if err := stage.Chmod(0o600); err != nil {
		return fmt.Errorf("secure normalization receipt stage: %w", err)
	}
	if _, err := stage.Write(encoded); err != nil {
		return fmt.Errorf("write normalization receipt stage: %w", err)
	}
	if err := stage.Sync(); err != nil {
		return fmt.Errorf("sync normalization receipt stage: %w", err)
	}
	if err := stage.Close(); err != nil {
		stageClosed = true
		return fmt.Errorf("close normalization receipt stage: %w", err)
	}
	stageClosed = true
	if err := os.Link(stagePath, clean); err != nil {
		return fmt.Errorf("publish normalization receipt without replacement: %w", err)
	}
	if err := os.Remove(stagePath); err != nil {
		return fmt.Errorf("retire normalization receipt stage: %w", err)
	}
	if current, err := os.Lstat(parent); err != nil || !os.SameFile(parentInfo, current) {
		return errors.New("normalization receipt parent changed during publication")
	}
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync normalization receipt parent: %w", err)
	}
	observed, err := readNormalizationIdentityReceipt(clean)
	if err != nil {
		return fmt.Errorf("reread normalization receipt: %w", err)
	}
	observedEncoded, err := canonicalNormalizationIdentityReceipt(observed)
	if err != nil || !bytes.Equal(encoded, observedEncoded) {
		return errors.New("normalization identity receipt changed after publication")
	}
	return nil
}

func readNormalizationIdentityReceipt(
	path string,
) (normalizationIdentityReceipt, error) {
	clean, _, err := validateIdentityReceiptPath(path)
	if err != nil {
		return normalizationIdentityReceipt{}, err
	}
	pathInfo, err := os.Lstat(clean)
	if err != nil {
		return normalizationIdentityReceipt{}, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() ||
		pathInfo.Mode().Perm() != 0o600 || pathInfo.Size() <= 0 ||
		pathInfo.Size() > 1<<20 {
		return normalizationIdentityReceipt{}, errors.New(
			"normalization receipt is not an exact mode-0600 bounded regular file",
		)
	}
	file, err := os.Open(clean)
	if err != nil {
		return normalizationIdentityReceipt{}, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) {
		return normalizationIdentityReceipt{}, errors.New(
			"normalization receipt changed while opening",
		)
	}
	raw, err := io.ReadAll(io.LimitReader(file, 1<<20+1))
	if err != nil {
		return normalizationIdentityReceipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var receipt normalizationIdentityReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return normalizationIdentityReceipt{}, err
	}
	var trailer json.RawMessage
	if err := decoder.Decode(&trailer); !errors.Is(err, io.EOF) {
		return normalizationIdentityReceipt{}, errors.New(
			"normalization receipt contains trailing JSON",
		)
	}
	canonical, err := canonicalNormalizationIdentityReceipt(receipt)
	if err != nil || !bytes.Equal(raw, canonical) {
		return normalizationIdentityReceipt{}, errors.New(
			"normalization identity receipt is not canonical",
		)
	}
	return receipt, nil
}

func readIdentityReceipt(path string) (identityReceipt, error) {
	clean, _, err := validateIdentityReceiptPath(path)
	if err != nil {
		return identityReceipt{}, err
	}
	pathInfo, err := os.Lstat(clean)
	if err != nil {
		return identityReceipt{}, err
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() ||
		pathInfo.Mode().Perm() != 0o600 || pathInfo.Size() <= 0 || pathInfo.Size() > 4096 {
		return identityReceipt{}, errors.New("identity receipt is not an exact mode-0600 bounded regular file")
	}
	file, err := os.Open(clean)
	if err != nil {
		return identityReceipt{}, err
	}
	defer file.Close()
	openedInfo, err := file.Stat()
	if err != nil || !os.SameFile(pathInfo, openedInfo) {
		return identityReceipt{}, errors.New("identity receipt changed while opening")
	}
	raw, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return identityReceipt{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var receipt identityReceipt
	if err := decoder.Decode(&receipt); err != nil {
		return identityReceipt{}, err
	}
	var trailer json.RawMessage
	if err := decoder.Decode(&trailer); !errors.Is(err, io.EOF) {
		return identityReceipt{}, errors.New("identity receipt contains trailing JSON")
	}
	canonical, err := canonicalIdentityReceipt(receipt)
	if err != nil || !bytes.Equal(raw, canonical) {
		return identityReceipt{}, errors.New("identity receipt is not canonical")
	}
	if receipt.Schema != identityReceiptSchema || !validCellID(receipt.CellID) ||
		!validMutationIdentity(receipt.RequestID) ||
		!validMutationIdentity(receipt.OwnerID) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(receipt.ManifestQualifier) {
		return identityReceipt{}, errors.New("identity receipt fields are invalid")
	}
	return receipt, nil
}

func loadScenario(path, expectedDriver string) (scenario, transport.SwitchDNSEngineV1Request, error) {
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) || clean != path {
		return scenario{}, transport.SwitchDNSEngineV1Request{},
			errors.New("scenario path must be clean and absolute")
	}
	file, err := os.Open(clean)
	if err != nil {
		return scenario{}, transport.SwitchDNSEngineV1Request{},
			fmt.Errorf("open scenario: %w", err)
	}
	defer file.Close()
	pathInfo, err := os.Lstat(clean)
	if err != nil {
		return scenario{}, transport.SwitchDNSEngineV1Request{},
			fmt.Errorf("inspect scenario: %w", err)
	}
	openedInfo, err := file.Stat()
	if err != nil {
		return scenario{}, transport.SwitchDNSEngineV1Request{},
			fmt.Errorf("inspect opened scenario: %w", err)
	}
	if pathInfo.Mode()&os.ModeSymlink != 0 || !pathInfo.Mode().IsRegular() ||
		!os.SameFile(pathInfo, openedInfo) || pathInfo.Mode().Perm()&0o022 != 0 ||
		pathInfo.Size() <= 0 || pathInfo.Size() > 65<<20 {
		return scenario{}, transport.SwitchDNSEngineV1Request{},
			errors.New("scenario must be an unchanged, non-writable regular file of bounded size")
	}
	decoder := json.NewDecoder(io.LimitReader(file, 65<<20))
	decoder.DisallowUnknownFields()
	var value scenario
	if err := decoder.Decode(&value); err != nil {
		return scenario{}, transport.SwitchDNSEngineV1Request{},
			fmt.Errorf("decode scenario: %w", err)
	}
	var trailer json.RawMessage
	if err := decoder.Decode(&trailer); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("scenario contains more than one JSON value")
		}
		return scenario{}, transport.SwitchDNSEngineV1Request{}, err
	}
	request, err := requestForScenario(value, expectedDriver)
	if err != nil {
		return scenario{}, transport.SwitchDNSEngineV1Request{}, err
	}
	return value, request, nil
}

func requestForScenario(
	value scenario,
	expectedDriver string,
) (transport.SwitchDNSEngineV1Request, error) {
	if value.Schema != scenarioSchema {
		return transport.SwitchDNSEngineV1Request{}, errors.New("scenario schema is unsupported")
	}
	if value.Driver != expectedDriver {
		return transport.SwitchDNSEngineV1Request{}, fmt.Errorf(
			"scenario driver %q differs from %s %q",
			value.Driver, environmentDriver, expectedDriver,
		)
	}
	manifest, err := mutationpayload.CanonicalDNSEngineSwitchManifestWithPairIdentity(
		value.Mode,
		value.SourceEngine,
		value.TargetEngine,
		value.SourceEpoch,
		value.TargetEpoch,
		value.SourceRevision,
		value.Topology,
		value.PairRole,
		value.LocalIP,
		value.LocalNS,
		value.PeerIP,
		value.PeerNS,
		value.Zones,
	)
	if err != nil {
		return transport.SwitchDNSEngineV1Request{}, fmt.Errorf(
			"canonicalize scenario manifest: %w", err,
		)
	}
	if err := validateDriverManifest(value.Driver, value.SourceFixture, manifest); err != nil {
		return transport.SwitchDNSEngineV1Request{}, err
	}
	return transport.SwitchDNSEngineV1Request{
		Mode:              manifest.Mode,
		SourceEngine:      manifest.SourceEngine,
		TargetEngine:      manifest.TargetEngine,
		SourceEpoch:       manifest.SourceEpoch,
		TargetEpoch:       manifest.TargetEpoch,
		SourceRevision:    manifest.SourceRevision,
		Topology:          manifest.Topology,
		PairRole:          manifest.PairRole,
		LocalIP:           manifest.LocalIP,
		LocalNS:           manifest.LocalNS,
		PeerIP:            manifest.PeerIP,
		PeerNS:            manifest.PeerNS,
		Zones:             manifest.Zones,
		SnapshotBytes:     manifest.SnapshotBytes,
		ManifestQualifier: manifest.Qualifier,
	}, nil
}

func validateDriverManifest(
	driver string,
	sourceFixture string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	if err := validateSourceFixture(sourceFixture, manifest); err != nil {
		return err
	}
	switch driver {
	case "bind":
		if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
			manifest.TargetEngine != transport.DNSEngineBIND ||
			(sourceFixture != "uninitialized" && sourceFixture != "managed-pdns") {
			return errors.New("bind driver requires a BIND switch manifest")
		}
	case "pdns-switch":
		if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
			manifest.TargetEngine != transport.DNSEnginePowerDNS ||
			pdnsSecondaryReconfigureManifest(manifest) ||
			(sourceFixture != "uninitialized" && sourceFixture != "managed-bind") {
			return errors.New("pdns-switch requires a non-reconfiguration PowerDNS switch manifest")
		}
	case "pdns-adopt":
		if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
			manifest.SourceEngine != "" ||
			manifest.TargetEngine != transport.DNSEnginePowerDNS ||
			sourceFixture != "external-pdns-adoption" {
			return errors.New("pdns-adopt requires an unresolved-source PowerDNS adoption manifest")
		}
	case "pdns-secondary-reconfigure":
		if sourceFixture != "legacy-pdns-secondary" ||
			!pdnsSecondaryReconfigureManifest(manifest) {
			return errors.New("pdns-secondary-reconfigure requires its exact zero-zone paired-secondary manifest")
		}
	case "signed-update-finalize":
		return errors.New("signed-update-finalize is a startup recovery path, not an RPC switch")
	default:
		return fmt.Errorf("unsupported matrix driver %q", driver)
	}
	return nil
}

func validateSourceFixture(
	sourceFixture string,
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) error {
	switch sourceFixture {
	case "uninitialized":
		if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
			manifest.SourceEngine != "" || manifest.SourceEpoch != 0 ||
			manifest.SourceRevision != 0 {
			return errors.New("uninitialized source fixture requires an exact empty 0/0 source identity")
		}
	case "managed-pdns":
		if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
			manifest.SourceEngine != transport.DNSEnginePowerDNS ||
			manifest.SourceEpoch < 1 {
			return errors.New("managed-pdns source fixture requires a positive PowerDNS source identity")
		}
	case "managed-bind":
		if manifest.Mode != transport.DNSEngineSwitchModeSwitch ||
			manifest.SourceEngine != transport.DNSEngineBIND ||
			manifest.SourceEpoch < 1 {
			return errors.New("managed-bind source fixture requires a positive BIND source identity")
		}
	case "external-pdns-adoption":
		if manifest.Mode != transport.DNSEngineSwitchModeAdopt ||
			manifest.SourceEngine != "" || manifest.SourceEpoch != 0 {
			return errors.New("external-pdns-adoption fixture requires an unresolved adoption source")
		}
	case "legacy-pdns-secondary":
		if !pdnsSecondaryReconfigureManifest(manifest) {
			return errors.New("legacy-pdns-secondary fixture requires the exact reconfiguration manifest")
		}
	default:
		return fmt.Errorf("unsupported source fixture provenance %q", sourceFixture)
	}
	return nil
}

func pdnsSecondaryReconfigureManifest(
	manifest mutationpayload.DNSEngineSwitchManifestCommitment,
) bool {
	return manifest.Mode == transport.DNSEngineSwitchModeSwitch &&
		manifest.SourceEngine == "" && manifest.SourceEpoch == 0 &&
		manifest.TargetEngine == transport.DNSEnginePowerDNS &&
		manifest.TargetEpoch == 1 &&
		manifest.Topology == transport.DNSTopologyPaired &&
		manifest.PairRole == transport.DNSPairRoleSecondary &&
		len(manifest.Zones) == 0 && manifest.SnapshotBytes == 0
}

func runRPCSwitch(
	ctx context.Context,
	requestID, ownerID string,
	request transport.SwitchDNSEngineV1Request,
	retry bool,
	heartbeatInterval time.Duration,
	call rpcCallFunc,
) (triggerResult, error) {
	if ctx == nil || call == nil || !validMutationIdentity(requestID) ||
		!validMutationIdentity(ownerID) || heartbeatInterval <= 0 ||
		!transport.ValidDNSEngine(request.TargetEngine) ||
		!mutationpayload.ValidDNSEngineSwitchQualifier(request.ManifestQualifier) {
		return triggerResult{}, errors.New("invalid RPC switch trigger")
	}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: requestID,
		MutationOwnerID:   ownerID,
	}
	begin := transport.ServiceMutationBeginRequest{
		RequestID:   requestID,
		OwnerID:     ownerID,
		Kind:        mutationKind,
		Target:      string(request.TargetEngine),
		PackageName: request.ManifestQualifier,
		Resume:      retry,
	}
	if retry {
		var statusResponse transport.ServiceMutationResponse
		statusCtx, cancelStatus := context.WithTimeout(ctx, controlTimeout)
		err := call(
			statusCtx,
			"Agent.ServiceMutationStatus",
			&transport.ServiceMutationStatusRequest{RequestID: requestID},
			&statusResponse,
		)
		cancelStatus()
		if err != nil {
			return triggerResult{}, fmt.Errorf("inspect retry DNS switch mutation: %w", err)
		}
		if err := responseError(statusResponse); err != nil {
			return triggerResult{}, fmt.Errorf("inspect retry DNS switch mutation: %w", err)
		}
		if !jobIdentityMatches(statusResponse.Job, begin) {
			return triggerResult{}, errors.New("retry DNS switch has no exact durable mutation identity")
		}
		switch statusResponse.Job.Status {
		case mutationSucceeded:
			if err := validateSucceededJob(statusResponse.Job, begin); err != nil {
				return triggerResult{}, fmt.Errorf("validate retry terminal receipt: %w", err)
			}
			return triggerResult{terminal: cloneJob(statusResponse.Job)}, nil
		case mutationFailed:
			if err := validateRetryableFailedJob(statusResponse.Job, begin); err != nil {
				return triggerResult{}, fmt.Errorf("validate retry failed receipt: %w", err)
			}
		case mutationRunning:
			if err := validateRunningJob(statusResponse.Job, begin, false); err != nil {
				return triggerResult{}, fmt.Errorf("validate retry running lease: %w", err)
			}
		default:
			return triggerResult{}, fmt.Errorf(
				"retry DNS switch mutation has unsupported status %q",
				statusResponse.Job.Status,
			)
		}
	}
	var beginResponse transport.ServiceMutationResponse
	beginCtx, cancelBegin := context.WithTimeout(ctx, controlTimeout)
	err := call(beginCtx, "Agent.BeginServiceMutation", &begin, &beginResponse)
	cancelBegin()
	if err != nil {
		return triggerResult{}, fmt.Errorf("begin DNS switch mutation: %w", err)
	}
	if retry && beginResponse.Job != nil && beginResponse.Job.Status == mutationSucceeded {
		if err := validateSucceededJob(beginResponse.Job, begin); err != nil {
			return triggerResult{}, fmt.Errorf("validate raced retry terminal receipt: %w", err)
		}
		return triggerResult{terminal: cloneJob(beginResponse.Job)}, nil
	}
	if err := responseError(beginResponse); err != nil {
		return triggerResult{}, fmt.Errorf("begin DNS switch mutation: %w", err)
	}
	if err := validateRunningJob(beginResponse.Job, begin, true); err != nil {
		return triggerResult{}, fmt.Errorf("validate DNS switch lease: %w", err)
	}

	request.ServiceMutationBinding = binding
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := startHeartbeat(
		heartbeatCtx, begin, heartbeatInterval, call,
	)
	var switchResponse transport.SwitchDNSEngineV1Response
	switchErr := call(ctx, "Agent.SwitchDNSEngineV1", &request, &switchResponse)
	cancelHeartbeat()
	heartbeat := <-heartbeatDone
	result := triggerResult{
		heartbeatCount: heartbeat.count,
		terminal:       cloneJob(heartbeat.terminal),
	}
	if heartbeat.terminal != nil {
		if err := validateSucceededJob(heartbeat.terminal, begin); err != nil {
			return result, fmt.Errorf("validate heartbeat terminal receipt: %w", err)
		}
		return result, nil
	}
	if heartbeat.err != nil {
		return result, fmt.Errorf("%w: heartbeat: %v", errSwitchOutcomeUncertain, heartbeat.err)
	}
	if switchErr != nil {
		return result, fmt.Errorf("%w: %v", errSwitchOutcomeUncertain, switchErr)
	}
	if switchResponse.Error != "" {
		terminal, finishErr := finishMutationWithResponse(ctx, begin, false, call)
		if finishErr != nil {
			return result, fmt.Errorf(
				"%w: agent rejection %q; terminal reconciliation: %v",
				errSwitchOutcomeUncertain, switchResponse.Error, finishErr,
			)
		}
		if err := validateFailedJob(terminal, begin); err != nil {
			return result, fmt.Errorf(
				"%w: agent rejection %q; terminal receipt: %v",
				errSwitchOutcomeUncertain, switchResponse.Error, err,
			)
		}
		return result, fmt.Errorf("agent rejected DNS switch: %s", switchResponse.Error)
	}
	if !switchResponse.Applied ||
		switchResponse.ActiveEngine != request.TargetEngine ||
		switchResponse.ActiveEpoch != request.TargetEpoch ||
		switchResponse.AppliedZones != len(request.Zones) {
		terminal, finishErr := finishMutationWithResponse(ctx, begin, false, call)
		if finishErr != nil {
			return result, fmt.Errorf(
				"%w: exact switch receipt missing; terminal reconciliation: %v",
				errSwitchOutcomeUncertain, finishErr,
			)
		}
		if err := validateFailedJob(terminal, begin); err != nil {
			return result, fmt.Errorf(
				"%w: exact switch receipt missing; terminal receipt: %v",
				errSwitchOutcomeUncertain, err,
			)
		}
		return result, errors.New("agent did not return the exact DNS switch receipt")
	}
	terminal, err := finishMutationWithResponse(ctx, begin, true, call)
	if err != nil {
		return result, fmt.Errorf("finish DNS switch mutation: %w", err)
	}
	if err := validateSucceededJob(terminal, begin); err != nil {
		return result, fmt.Errorf("validate DNS switch terminal receipt: %w", err)
	}
	result.terminal = cloneJob(terminal)
	return result, nil
}

func runPDNSNormalization(
	ctx context.Context,
	receipt normalizationIdentityReceipt,
	switchRequest transport.SwitchDNSEngineV1Request,
	heartbeatInterval time.Duration,
	call rpcCallFunc,
) (int, error) {
	if ctx == nil || call == nil || heartbeatInterval <= 0 ||
		receipt.Schema != normalizationIdentityReceiptSchema ||
		receipt.SourceEngine != string(transport.DNSEnginePowerDNS) ||
		receipt.SourceEpoch <= 0 || len(receipt.ZoneSyncs) != len(switchRequest.Zones) {
		return 0, errors.New("invalid PowerDNS normalization trigger")
	}
	heartbeats := 0
	configure := receipt.Configure
	count, err := runRPCMutationOperation(
		ctx, configure, heartbeatInterval, call,
		func(operationCtx context.Context, binding transport.ServiceMutationBinding) error {
			request := transport.ServiceMutationRequest{ServiceMutationBinding: binding}
			var response transport.SyncDNSZoneResponse
			if err := call(
				operationCtx, configure.Method, &request, &response,
			); err != nil {
				return err
			}
			if response.Error != "" || !response.Synced || response.AppliedGeneration != 0 {
				return fmt.Errorf(
					"PowerDNS configuration lacks its exact success response: synced=%t generation=%d error=%q",
					response.Synced, response.AppliedGeneration, response.Error,
				)
			}
			return nil
		},
	)
	heartbeats += count
	if err != nil {
		return heartbeats, fmt.Errorf("normalize PowerDNS configuration: %w", err)
	}
	for index, identity := range receipt.ZoneSyncs {
		zone := switchRequest.Zones[index]
		count, err := runRPCMutationOperation(
			ctx, identity.normalizationMutationIdentity, heartbeatInterval, call,
			func(operationCtx context.Context, binding transport.ServiceMutationBinding) error {
				request := transport.SyncDNSZoneV3Request{
					ServiceMutationBinding: binding,
					Engine:                 transport.DNSEnginePowerDNS, EngineEpoch: receipt.SourceEpoch,
					DesiredGeneration: zone.DesiredGeneration, Domain: zone.Domain,
					Delete: zone.Delete, ZoneType: zone.ZoneType, Records: zone.Records,
				}
				var response transport.SyncDNSZoneV3Response
				if err := call(operationCtx, identity.Method, &request, &response); err != nil {
					return err
				}
				if response.Error != "" || !response.Synced || response.RecoveryPending ||
					response.Engine != transport.DNSEnginePowerDNS ||
					response.EngineEpoch != receipt.SourceEpoch ||
					response.AppliedGeneration != zone.DesiredGeneration {
					return fmt.Errorf(
						"PowerDNS V3 sync lacks its exact success response for %s",
						zone.Domain,
					)
				}
				return nil
			},
		)
		heartbeats += count
		if err != nil {
			return heartbeats, fmt.Errorf(
				"normalize PowerDNS source zone %s: %w", zone.Domain, err,
			)
		}
	}
	return heartbeats, nil
}

func runRPCMutationOperation(
	ctx context.Context,
	identity normalizationMutationIdentity,
	heartbeatInterval time.Duration,
	call rpcCallFunc,
	invoke func(context.Context, transport.ServiceMutationBinding) error,
) (int, error) {
	if ctx == nil || call == nil || invoke == nil || heartbeatInterval <= 0 ||
		!validMutationIdentity(identity.RequestID) ||
		!validMutationIdentity(identity.OwnerID) || identity.Method == "" ||
		identity.Kind == "" || identity.Target == "" || identity.TerminalPhase == "" {
		return 0, errors.New("invalid normalization service mutation operation")
	}
	begin := transport.ServiceMutationBeginRequest{
		RequestID: identity.RequestID, OwnerID: identity.OwnerID,
		Kind: identity.Kind, Target: identity.Target, PackageName: identity.PackageName,
	}
	var beginResponse transport.ServiceMutationResponse
	beginCtx, cancelBegin := context.WithTimeout(ctx, controlTimeout)
	err := call(beginCtx, "Agent.BeginServiceMutation", &begin, &beginResponse)
	cancelBegin()
	if err != nil {
		return 0, fmt.Errorf("begin normalization mutation: %w", err)
	}
	if err := responseError(beginResponse); err != nil {
		return 0, fmt.Errorf("begin normalization mutation: %w", err)
	}
	if err := validateRunningJob(beginResponse.Job, begin, true); err != nil {
		return 0, fmt.Errorf("validate normalization mutation lease: %w", err)
	}
	binding := transport.ServiceMutationBinding{
		MutationRequestID: identity.RequestID, MutationOwnerID: identity.OwnerID,
	}
	heartbeatCtx, cancelHeartbeat := context.WithCancel(ctx)
	heartbeatDone := startHeartbeat(
		heartbeatCtx, begin, heartbeatInterval, call,
	)
	operationErr := invoke(ctx, binding)
	cancelHeartbeat()
	heartbeat := <-heartbeatDone
	if heartbeat.terminal != nil {
		if err := validateSucceededJobAtPhase(
			heartbeat.terminal, begin, identity.TerminalPhase,
		); err != nil {
			return heartbeat.count, fmt.Errorf("validate normalization heartbeat terminal: %w", err)
		}
		if operationErr != nil {
			return heartbeat.count, fmt.Errorf(
				"normalization RPC failed after terminal success: %w", operationErr,
			)
		}
		return heartbeat.count, nil
	}
	if heartbeat.err != nil {
		return heartbeat.count, fmt.Errorf("normalization heartbeat: %w", heartbeat.err)
	}
	if operationErr != nil {
		terminal, finishErr := finishMutationWithResponse(ctx, begin, false, call)
		if finishErr != nil {
			return heartbeat.count, fmt.Errorf(
				"normalization RPC failed (%v) and failure receipt failed: %w",
				operationErr, finishErr,
			)
		}
		if err := validateFailedJob(terminal, begin); err != nil {
			return heartbeat.count, fmt.Errorf(
				"normalization RPC failed (%v) without exact failed receipt: %w",
				operationErr, err,
			)
		}
		return heartbeat.count, operationErr
	}
	terminal, err := finishMutationWithResponse(ctx, begin, true, call)
	if err != nil {
		return heartbeat.count, fmt.Errorf("finish normalization mutation: %w", err)
	}
	if err := validateSucceededJobAtPhase(terminal, begin, identity.TerminalPhase); err != nil {
		return heartbeat.count, fmt.Errorf("validate normalization terminal receipt: %w", err)
	}
	return heartbeat.count, nil
}

func startHeartbeat(
	ctx context.Context,
	identity transport.ServiceMutationBeginRequest,
	interval time.Duration,
	call rpcCallFunc,
) <-chan heartbeatResult {
	done := make(chan heartbeatResult, 1)
	go func() {
		result := heartbeatResult{}
		defer func() { done <- result }()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
			request := transport.ServiceMutationHeartbeatRequest{
				RequestID: identity.RequestID,
				OwnerID:   identity.OwnerID,
			}
			var response transport.ServiceMutationResponse
			pingCtx, cancel := context.WithTimeout(ctx, interval)
			err := call(pingCtx, "Agent.HeartbeatServiceMutation", &request, &response)
			cancel()
			if err != nil {
				if ctx.Err() == nil {
					result.err = err
				}
				return
			}
			result.count++
			if err := responseError(response); err != nil {
				result.err = err
				return
			}
			if response.Job != nil && response.Job.Status == mutationSucceeded {
				result.terminal = cloneJob(response.Job)
				return
			}
			if err := validateRunningJob(response.Job, identity, false); err != nil {
				result.err = err
				return
			}
		}
	}()
	return done
}

func finishMutationWithResponse(
	ctx context.Context,
	identity transport.ServiceMutationBeginRequest,
	success bool,
	call rpcCallFunc,
) (*transport.ServiceMutationJob, error) {
	request := transport.ServiceMutationFinishRequest{
		RequestID: identity.RequestID,
		OwnerID:   identity.OwnerID,
		Success:   success,
	}
	if !success {
		request.FailureCode = triggerFailureCode
		request.Message = triggerFailureMessage
	}
	var response transport.ServiceMutationResponse
	finishCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), controlTimeout)
	defer cancel()
	if err := call(finishCtx, "Agent.FinishServiceMutation", &request, &response); err != nil {
		return response.Job, err
	}
	if err := responseError(response); err != nil {
		return response.Job, err
	}
	if response.Job == nil || !jobIdentityMatches(response.Job, identity) ||
		response.Job.WorkerPID != 0 || response.Job.WorkerStarted != "" ||
		response.Job.WorkerCommand != "" {
		return response.Job, errors.New("agent returned another terminal mutation identity")
	}
	return response.Job, nil
}

func validateRunningJob(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
	requireLeased bool,
) error {
	if !jobIdentityMatches(job, identity) || job.Status != mutationRunning ||
		job.WorkerPID != 0 || job.WorkerStarted != "" || job.WorkerCommand != "" ||
		job.Attempt <= 0 || job.StartedAt.IsZero() || job.UpdatedAt.IsZero() ||
		job.LeaseExpiresAt.IsZero() || job.DeadlineAt.IsZero() ||
		job.FinishedAt != (time.Time{}) {
		return errors.New("service mutation job is not the exact in-process DNS lease")
	}
	if requireLeased && job.Phase != mutationLeasedPhase {
		return fmt.Errorf("service mutation phase is %q, want leased", job.Phase)
	}
	return nil
}

func validateSucceededJob(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
) error {
	expectedPhase := "commit/dns-engine-switch/v2/finalized/" +
		identity.RequestID + "/" + identity.PackageName
	return validateSucceededJobAtPhase(job, identity, expectedPhase)
}

func validateSucceededJobAtPhase(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
	expectedPhase string,
) error {
	if !jobIdentityMatches(job, identity) || job.Status != mutationSucceeded ||
		job.Phase != expectedPhase || job.WorkerPID != 0 ||
		job.WorkerStarted != "" || job.WorkerCommand != "" ||
		job.FinishedAt.IsZero() || !job.LeaseExpiresAt.IsZero() {
		return errors.New("service mutation job lacks its exact finalized DNS switch receipt")
	}
	return nil
}

func validateFailedJob(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
) error {
	if err := validateFailedJobEnvelope(job, identity); err != nil {
		return err
	}
	if job.Phase != mutationFailed || job.ErrorCode != triggerFailureCode ||
		job.ErrorMessage != triggerFailureMessage {
		return errors.New("service mutation job lacks its exact terminal failed receipt")
	}
	return nil
}

func validateRetryableFailedJob(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
) error {
	if err := validateFailedJobEnvelope(job, identity); err != nil {
		return err
	}
	switch job.Phase {
	case mutationInterruptedPhase:
		switch job.ErrorCode {
		case restartBeforeSwitchCommitCode:
			if job.ErrorMessage == restartBeforeSwitchCommitMessage {
				return nil
			}
		case switchRolledBackAfterRestartCode:
			if job.ErrorMessage == switchRolledBackAfterRestartMessage {
				return nil
			}
		}
	case mutationFailed:
		if job.ErrorCode == triggerFailureCode &&
			job.ErrorMessage == triggerFailureMessage {
			return nil
		}
	}
	return errors.New("service mutation job is not an exact resumable DNS switch failure")
}

func validateFailedJobEnvelope(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
) error {
	if !jobIdentityMatches(job, identity) || job.Status != mutationFailed ||
		job.WorkerPID != 0 || job.WorkerStarted != "" ||
		job.WorkerCommand != "" || job.Attempt <= 0 ||
		job.StartedAt.IsZero() || job.UpdatedAt.IsZero() ||
		job.DeadlineAt.IsZero() || job.FinishedAt.IsZero() ||
		job.UpdatedAt.Before(job.StartedAt) ||
		job.DeadlineAt.Before(job.StartedAt) ||
		job.FinishedAt.Before(job.StartedAt) ||
		!job.UpdatedAt.Equal(job.FinishedAt) ||
		!job.LeaseExpiresAt.IsZero() {
		return errors.New("service mutation job lacks its exact terminal failed envelope")
	}
	return nil
}

func jobIdentityMatches(
	job *transport.ServiceMutationJob,
	identity transport.ServiceMutationBeginRequest,
) bool {
	return job != nil &&
		job.RequestID == identity.RequestID &&
		job.OwnerID == identity.OwnerID &&
		job.Kind == identity.Kind &&
		job.Target == identity.Target &&
		job.PackageName == identity.PackageName
}

func responseError(response transport.ServiceMutationResponse) error {
	if response.ErrorCode != "" || response.Error != "" {
		return fmt.Errorf("agent response code=%q error=%q", response.ErrorCode, response.Error)
	}
	return nil
}

func cloneJob(job *transport.ServiceMutationJob) *transport.ServiceMutationJob {
	if job == nil {
		return nil
	}
	copy := *job
	return &copy
}

func callProductionAgent(
	ctx context.Context,
	method string,
	request, response any,
) error {
	client, err := transport.ConnectAgentContext(ctx)
	if err != nil {
		return err
	}
	defer client.Close()
	return callRPCContext(ctx, client, method, request, response)
}

func callRPCContext(
	ctx context.Context,
	client *rpc.Client,
	method string,
	request, response any,
) error {
	if ctx == nil || client == nil {
		return errors.New("RPC context or client is nil")
	}
	done := client.Go(method, request, response, make(chan *rpc.Call, 1)).Done
	select {
	case call := <-done:
		return call.Error
	case <-ctx.Done():
		_ = client.Close()
		call := <-done
		return errors.Join(ctx.Err(), call.Error)
	}
}
