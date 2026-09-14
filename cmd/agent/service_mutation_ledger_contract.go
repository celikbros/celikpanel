package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const serviceMutationLedgerVersion = 1

const serviceMutationStatusRunning = "running"

const serviceMutationStatusCancelling = "cancelling"

const serviceMutationStatusOrphaned = "orphaned"

const serviceMutationStatusPending = "pending"

const serviceMutationStatusSucceeded = "succeeded"

const serviceMutationStatusFailed = "failed"

const serviceMutationLedgerMaxSize = 1 << 20

var errServiceMutationHostBusy = errors.New("the host package manager or mutation lock is busy")

type ServiceMutationJob = transport.ServiceMutationJob

type serviceMutationLedger struct {
	Version         int                            `json:"version"`
	ActiveRequestID string                         `json:"active_request_id,omitempty"`
	Jobs            map[string]*ServiceMutationJob `json:"jobs"`
}

func serviceMutationStateDirectory() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_AGENT_STATE_DIR")); value != "" {
		return value
	}
	return hostingpath.ServiceMutationStateRoot()
}

func serviceMutationLockFile() string {
	if value := strings.TrimSpace(os.Getenv("CELIKPANEL_MUTATION_LOCK")); value != "" {
		return value
	}
	return "/run/celikpanel/service-mutation.lock"
}

func decodeServiceMutationLedger(raw []byte) (serviceMutationLedger, error) {
	var ledger serviceMutationLedger
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return serviceMutationLedger{}, fmt.Errorf("decode service mutation ledger: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return serviceMutationLedger{}, errors.New("service mutation ledger contains more than one JSON value")
		}
		return serviceMutationLedger{}, fmt.Errorf("decode service mutation ledger trailer: %w", err)
	}
	if ledger.Version != serviceMutationLedgerVersion || ledger.Jobs == nil {
		return serviceMutationLedger{}, errors.New("service mutation ledger has an unsupported schema")
	}
	canonical, err := json.Marshal(&ledger)
	if err != nil {
		return serviceMutationLedger{}, fmt.Errorf("canonicalize service mutation ledger: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return serviceMutationLedger{}, errors.New("service mutation ledger is not canonical")
	}
	if err := validateServiceMutationLedger(&ledger); err != nil {
		return serviceMutationLedger{}, err
	}
	return ledger, nil
}

func payloadBoundDirectMutationPublishedPhase(
	job *ServiceMutationJob,
) (string, bool, error) {
	if job == nil {
		return "", false, errors.New("payload-bound mutation job is required")
	}
	var phase string
	var err error
	switch job.Kind {
	case "vpn_peer_sync":
		if job.Target != "wireguard" ||
			!mutationpayload.ValidVPNPeerSyncQualifier(job.PackageName) {
			return "", true, errors.New("invalid VPN peer sync publication identity")
		}
		phase, err = formatVPNPeerSyncCommitPhase(
			vpnPeerSyncCommitPublished, job.RequestID, job.PackageName,
		)
	case "firewall_apply", "firewall_sync":
		if job.Target != "nftables" ||
			!mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
			return "", true, errors.New("invalid firewall publication identity")
		}
		phase, err = formatFirewallApplyCommitPhase(
			firewallApplyCommitPublished, job.RequestID, job.PackageName,
		)
	case "mail_tls_sync":
		if job.Target != "mail-tls" ||
			!mutationpayload.ValidMailTLSSyncQualifier(job.PackageName) {
			return "", true, errors.New("invalid mail TLS publication identity")
		}
		phase, err = formatMailTLSSyncCommitPhase(
			mailTLSSyncCommitPublished, job.RequestID, job.PackageName,
		)
	case "dns_cluster_configure":
		if job.Target != "pdns" ||
			!mutationpayload.ValidDNSClusterConfigQualifier(job.PackageName) {
			return "", true, errors.New("invalid DNS cluster publication identity")
		}
		phase, err = formatDNSClusterConfigCommitPhase(
			dnsClusterConfigCommitPublished, job.RequestID, job.PackageName,
		)
	case "dns_zone_sync":
		if !serviceMutationCanonicalFQDN(job.Target) {
			return "", true, errors.New("invalid DNS zone publication identity")
		}
		switch {
		case mutationpayload.ValidDNSZoneSyncQualifier(job.PackageName):
			phase, err = formatDNSZoneSyncCommitPhase(
				dnsZoneSyncCommitPublished, job.RequestID, job.Target, job.PackageName,
			)
		case mutationpayload.ValidDNSZoneSyncV3Qualifier(job.PackageName):
			phase, err = formatDNSZoneSyncV3PublishedPhase(
				job.RequestID, job.Target, job.PackageName,
			)
		default:
			return "", true, errors.New("invalid DNS zone publication identity")
		}
	case "panel_certificate_issue":
		if !serviceMutationCanonicalFQDN(job.Target) ||
			!mutationpayload.ValidPanelCertificateIssueQualifier(job.PackageName) {
			return "", true, errors.New("invalid panel certificate publication identity")
		}
		phase, err = formatPanelCertificateIssueCommitPhase(
			panelCertificateIssueCommitPublished,
			job.RequestID,
			job.Target,
			job.PackageName,
		)
	case "mail_host_certificate":
		if !serviceMutationCanonicalFQDN(job.Target) ||
			!mutationpayload.ValidMailHostCertificateQualifier(job.PackageName) {
			return "", true, errors.New("invalid mail host certificate publication identity")
		}
		phase, err = formatMailHostCertificateCommitPhase(
			mailHostCertificateCommitPublished,
			job.RequestID,
			job.Target,
			job.PackageName,
		)
	default:
		return "", false, nil
	}
	if err != nil {
		return "", true, err
	}
	return phase, true, nil
}

func validatePayloadBoundDirectMutationSuccess(job *ServiceMutationJob) error {
	if job == nil || job.Status != serviceMutationStatusSucceeded {
		return nil
	}
	expected, direct, err := payloadBoundDirectMutationPublishedPhase(job)
	if err != nil {
		return err
	}
	if direct && job.Phase != expected {
		return errors.New(
			"payload-bound direct mutation success lacks its exact canonical published receipt",
		)
	}
	return nil
}

// validateServiceMutationLedger enforces identity and bidirectional active-pointer invariants for the complete ledger.
// validateServiceMutationLedger, ledger'ın tamamı için kimlik ve çift yönlü aktif işaretçi değişmezlerini uygular.
func validateServiceMutationLedger(ledger *serviceMutationLedger) error {
	activeRequestID := ""
	for requestID, job := range ledger.Jobs {
		if job == nil || job.RequestID != requestID {
			return errors.New("service mutation ledger job identity is inconsistent")
		}
		if !validMutationIdentity(job.RequestID) || !validMutationIdentity(job.OwnerID) {
			return errors.New("service mutation ledger job identity is invalid")
		}
		if strings.TrimSpace(job.Kind) == "" ||
			strings.TrimSpace(job.Target) == "" ||
			strings.TrimSpace(job.Phase) == "" ||
			job.Attempt <= 0 {
			return errors.New("service mutation ledger job metadata is incomplete")
		}
		if err := validatePayloadBoundDirectMutationSuccess(job); err != nil {
			return fmt.Errorf("service mutation ledger job %s: %w", requestID, err)
		}
		if strings.HasPrefix(job.Phase, vpnPeerSyncCommitPhasePrefix) {
			state, requestID, qualifier, err := parseVPNPeerSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID || qualifier != job.PackageName ||
				job.Kind != "vpn_peer_sync" || job.Target != "wireguard" {
				return errors.New("service mutation ledger has an invalid VPN peer commit receipt")
			}
			if (state == vpnPeerSyncCommitIntent &&
				job.Status != serviceMutationStatusRunning &&
				job.Status != serviceMutationStatusCancelling) ||
				(state == vpnPeerSyncCommitPublished && job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger VPN peer commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, firewallApplyCommitPhasePrefix) {
			state, requestID, qualifier, err := parseFirewallApplyCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID || qualifier != job.PackageName ||
				(job.Kind != "firewall_apply" && job.Kind != "firewall_sync") ||
				job.Target != "nftables" {
				return errors.New("service mutation ledger has an invalid firewall commit receipt")
			}
			if (state == firewallApplyCommitIntent &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == firewallApplyCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger firewall commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, mailTLSSyncCommitPhasePrefix) {
			state, requestID, qualifier, err := parseMailTLSSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				qualifier != job.PackageName ||
				job.Kind != "mail_tls_sync" || job.Target != "mail-tls" {
				return errors.New("service mutation ledger has an invalid mail TLS commit receipt")
			}
			if (state == mailTLSSyncCommitIntent &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == mailTLSSyncCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger mail TLS commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, dnsClusterConfigCommitPhasePrefix) {
			state, requestID, qualifier, err :=
				parseDNSClusterConfigCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				qualifier != job.PackageName ||
				job.Kind != "dns_cluster_configure" || job.Target != "pdns" {
				return errors.New("service mutation ledger has an invalid DNS cluster commit receipt")
			}
			if (state == dnsClusterConfigCommitIntent &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == dnsClusterConfigCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger DNS cluster commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, dnsZoneSyncCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parseDNSZoneSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "dns_zone_sync" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid DNS zone commit receipt")
			}
			if ((state == dnsZoneSyncCommitIntent ||
				state == dnsZoneSyncCommitApplied) &&
				!serviceMutationStatusActive(job.Status)) ||
				(state == dnsZoneSyncCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger DNS zone commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, dnsZoneSyncV3CommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parseDNSZoneSyncV3Phase(job.Phase)
			if err != nil || requestID != job.RequestID || domain != job.Target ||
				qualifier != job.PackageName || job.Kind != "dns_zone_sync" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid DNS zone V3 receipt")
			}
			validStatus := false
			switch state {
			case dnsZoneSyncV3Applied:
				validStatus = serviceMutationStatusActive(job.Status)
			case dnsZoneSyncV3PropagationPending:
				validStatus = job.Status == serviceMutationStatusPending
			case dnsZoneSyncV3Recovering:
				validStatus = serviceMutationStatusActive(job.Status)
			case dnsZoneSyncV3Published:
				validStatus = job.Status == serviceMutationStatusSucceeded
			}
			if !validStatus {
				return errors.New("service mutation ledger DNS zone V3 receipt conflicts with job status")
			}
		}
		if job.Status == serviceMutationStatusPending &&
			!strings.HasPrefix(job.Phase, dnsZoneSyncV3CommitPhasePrefix) {
			return errors.New("pending service mutation lacks an exact DNS zone V3 receipt")
		}
		if strings.HasPrefix(job.Phase, panelCertificateIssueCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parsePanelCertificateIssueCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "panel_certificate_issue" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid panel certificate commit receipt")
			}
			if (state == panelCertificateIssueCommitIntent &&
				job.Status != serviceMutationStatusRunning &&
				job.Status != serviceMutationStatusCancelling) ||
				(state == panelCertificateIssueCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger panel certificate commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, mailHostCertificateCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				parseMailHostCertificateCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "mail_host_certificate" ||
				!serviceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid mail host certificate commit receipt")
			}
			if (state == mailHostCertificateCommitIntent &&
				job.Status != serviceMutationStatusRunning &&
				job.Status != serviceMutationStatusCancelling) ||
				(state == mailHostCertificateCommitPublished &&
					job.Status != serviceMutationStatusSucceeded) {
				return errors.New("service mutation ledger mail host certificate commit receipt conflicts with job status")
			}
		}
		hasWorkerPID := job.WorkerPID > 0
		hasWorkerStarted := strings.TrimSpace(job.WorkerStarted) != ""
		hasWorkerCommand := strings.TrimSpace(job.WorkerCommand) != ""
		if job.WorkerPID < 0 ||
			hasWorkerPID != hasWorkerStarted ||
			hasWorkerPID != hasWorkerCommand {
			return errors.New("service mutation ledger worker identity is inconsistent")
		}

		if job.StartedAt.IsZero() || job.UpdatedAt.IsZero() || job.DeadlineAt.IsZero() {
			return errors.New("service mutation ledger lifecycle timestamps are incomplete")
		}
		if job.UpdatedAt.Before(job.StartedAt) || job.DeadlineAt.Before(job.StartedAt) {
			return errors.New("service mutation ledger lifecycle timestamps are out of order")
		}
		if !job.LeaseExpiresAt.IsZero() &&
			(job.LeaseExpiresAt.Before(job.StartedAt) ||
				job.LeaseExpiresAt.After(job.DeadlineAt)) {
			return errors.New("service mutation ledger lease timestamp is out of range")
		}
		switch job.Status {
		case serviceMutationStatusRunning,
			serviceMutationStatusCancelling,
			serviceMutationStatusOrphaned:
			if job.LeaseExpiresAt.IsZero() {
				return errors.New("active service mutation ledger job has no lease timestamp")
			}
			if !job.FinishedAt.IsZero() {
				return errors.New("active service mutation ledger job has a finish timestamp")
			}
			if activeRequestID != "" {
				return errors.New("service mutation ledger contains multiple active jobs")
			}
			activeRequestID = requestID
		case serviceMutationStatusPending,
			serviceMutationStatusSucceeded, serviceMutationStatusFailed:
			if hasWorkerPID {
				return errors.New("terminal service mutation ledger job retains a worker")
			}
			if !job.LeaseExpiresAt.IsZero() {
				return errors.New("terminal service mutation ledger job retains a lease")
			}
			if job.FinishedAt.IsZero() ||
				job.FinishedAt.Before(job.StartedAt) ||
				job.UpdatedAt.After(job.FinishedAt) {
				return errors.New("terminal service mutation ledger timestamps are inconsistent")
			}
			// Terminal jobs remain as history and must not be selected by the active pointer.
			// Sonlandırılmış işler geçmiş olarak kalır ve aktif işaretçi tarafından seçilmemelidir.
		default:
			return errors.New("service mutation ledger job has an unsupported status")
		}
	}
	if ledger.ActiveRequestID != activeRequestID {
		return errors.New("service mutation ledger active pointer is inconsistent")
	}
	return nil
}

func serviceMutationStatusActive(status string) bool {
	return status == serviceMutationStatusRunning ||
		status == serviceMutationStatusCancelling ||
		status == serviceMutationStatusOrphaned
}

func validMutationIdentity(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}
