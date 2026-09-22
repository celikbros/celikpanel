package servicemutationledger

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/alicelik/celikpanel/internal/mutationpayload"
	"github.com/alicelik/celikpanel/internal/transport"
)

const Version = 1

const StatusRunning = "running"

const StatusCancelling = "cancelling"

const StatusOrphaned = "orphaned"

const StatusPending = "pending"

const StatusSucceeded = "succeeded"

const StatusFailed = "failed"

const MaxSize = 1 << 20

type ServiceMutationJob = transport.ServiceMutationJob

type Ledger struct {
	Version         int                            `json:"version"`
	ActiveRequestID string                         `json:"active_request_id,omitempty"`
	Jobs            map[string]*ServiceMutationJob `json:"jobs"`
}

func Decode(raw []byte) (Ledger, error) {
	if len(raw) > MaxSize {
		return Ledger{}, errors.New("service mutation ledger exceeds size limit")
	}
	var ledger Ledger
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&ledger); err != nil {
		return Ledger{}, fmt.Errorf("decode service mutation ledger: %w", err)
	}
	var extra json.RawMessage
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Ledger{}, errors.New("service mutation ledger contains more than one JSON value")
		}
		return Ledger{}, fmt.Errorf("decode service mutation ledger trailer: %w", err)
	}
	if ledger.Version != Version || ledger.Jobs == nil {
		return Ledger{}, errors.New("service mutation ledger has an unsupported schema")
	}
	canonical, err := json.Marshal(&ledger)
	if err != nil {
		return Ledger{}, fmt.Errorf("canonicalize service mutation ledger: %w", err)
	}
	if !bytes.Equal(raw, canonical) {
		return Ledger{}, errors.New("service mutation ledger is not canonical")
	}
	if err := Validate(&ledger); err != nil {
		return Ledger{}, err
	}
	return ledger, nil
}

func PublishedPhase(
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
		phase, err = FormatVPNPeerSyncCommitPhase(
			VpnPeerSyncCommitPublished, job.RequestID, job.PackageName,
		)
	case "firewall_apply", "firewall_sync":
		if job.Target != "nftables" ||
			!mutationpayload.ValidFirewallApplyQualifier(job.PackageName) {
			return "", true, errors.New("invalid firewall publication identity")
		}
		phase, err = FormatFirewallApplyCommitPhase(
			FirewallApplyCommitPublished, job.RequestID, job.PackageName,
		)
	case "mail_tls_sync":
		if job.Target != "mail-tls" ||
			!mutationpayload.ValidMailTLSSyncQualifier(job.PackageName) {
			return "", true, errors.New("invalid mail TLS publication identity")
		}
		phase, err = FormatMailTLSSyncCommitPhase(
			MailTLSSyncCommitPublished, job.RequestID, job.PackageName,
		)
	case "dns_cluster_configure":
		if job.Target != "pdns" ||
			!mutationpayload.ValidDNSClusterConfigQualifier(job.PackageName) {
			return "", true, errors.New("invalid DNS cluster publication identity")
		}
		phase, err = FormatDNSClusterConfigCommitPhase(
			DnsClusterConfigCommitPublished, job.RequestID, job.PackageName,
		)
	case "dns_zone_sync":
		if !ServiceMutationCanonicalFQDN(job.Target) {
			return "", true, errors.New("invalid DNS zone publication identity")
		}
		switch {
		case mutationpayload.ValidDNSZoneSyncQualifier(job.PackageName):
			phase, err = FormatDNSZoneSyncCommitPhase(
				DnsZoneSyncCommitPublished, job.RequestID, job.Target, job.PackageName,
			)
		case mutationpayload.ValidDNSZoneSyncV3Qualifier(job.PackageName):
			phase, err = FormatDNSZoneSyncV3PublishedPhase(
				job.RequestID, job.Target, job.PackageName,
			)
		default:
			return "", true, errors.New("invalid DNS zone publication identity")
		}
	case "panel_certificate_issue":
		if !ServiceMutationCanonicalFQDN(job.Target) ||
			!mutationpayload.ValidPanelCertificateIssueQualifier(job.PackageName) {
			return "", true, errors.New("invalid panel certificate publication identity")
		}
		phase, err = FormatPanelCertificateIssueCommitPhase(
			PanelCertificateIssueCommitPublished,
			job.RequestID,
			job.Target,
			job.PackageName,
		)
	case "mail_host_certificate":
		if !ServiceMutationCanonicalFQDN(job.Target) ||
			!mutationpayload.ValidMailHostCertificateQualifier(job.PackageName) {
			return "", true, errors.New("invalid mail host certificate publication identity")
		}
		phase, err = FormatMailHostCertificateCommitPhase(
			MailHostCertificateCommitPublished,
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

func ValidateSuccess(job *ServiceMutationJob) error {
	if job == nil || job.Status != StatusSucceeded {
		return nil
	}
	expected, direct, err := PublishedPhase(job)
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

// Validate enforces identity and bidirectional active-pointer invariants for the complete ledger.
// Validate, ledger'ın tamamı için kimlik ve çift yönlü aktif işaretçi değişmezlerini uygular.
func Validate(ledger *Ledger) error {
	if ledger == nil || ledger.Version != Version || ledger.Jobs == nil {
		return errors.New("service mutation ledger has an unsupported schema")
	}
	activeRequestID := ""
	for requestID, job := range ledger.Jobs {
		if job == nil || job.RequestID != requestID {
			return errors.New("service mutation ledger job identity is inconsistent")
		}
		if !ValidIdentity(job.RequestID) || !ValidIdentity(job.OwnerID) {
			return errors.New("service mutation ledger job identity is invalid")
		}
		if strings.TrimSpace(job.Kind) == "" ||
			strings.TrimSpace(job.Target) == "" ||
			strings.TrimSpace(job.Phase) == "" ||
			job.Attempt <= 0 {
			return errors.New("service mutation ledger job metadata is incomplete")
		}
		if err := ValidateSuccess(job); err != nil {
			return fmt.Errorf("service mutation ledger job %s: %w", requestID, err)
		}
		if strings.HasPrefix(job.Phase, VpnPeerSyncCommitPhasePrefix) {
			state, requestID, qualifier, err := ParseVPNPeerSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID || qualifier != job.PackageName ||
				job.Kind != "vpn_peer_sync" || job.Target != "wireguard" {
				return errors.New("service mutation ledger has an invalid VPN peer commit receipt")
			}
			if (state == VpnPeerSyncCommitIntent &&
				job.Status != StatusRunning &&
				job.Status != StatusCancelling) ||
				(state == VpnPeerSyncCommitPublished && job.Status != StatusSucceeded) {
				return errors.New("service mutation ledger VPN peer commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, FirewallApplyCommitPhasePrefix) {
			state, requestID, qualifier, err := ParseFirewallApplyCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID || qualifier != job.PackageName ||
				(job.Kind != "firewall_apply" && job.Kind != "firewall_sync") ||
				job.Target != "nftables" {
				return errors.New("service mutation ledger has an invalid firewall commit receipt")
			}
			if (state == FirewallApplyCommitIntent &&
				!StatusActive(job.Status)) ||
				(state == FirewallApplyCommitPublished &&
					job.Status != StatusSucceeded) {
				return errors.New("service mutation ledger firewall commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, MailTLSSyncCommitPhasePrefix) {
			state, requestID, qualifier, err := ParseMailTLSSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				qualifier != job.PackageName ||
				job.Kind != "mail_tls_sync" || job.Target != "mail-tls" {
				return errors.New("service mutation ledger has an invalid mail TLS commit receipt")
			}
			if (state == MailTLSSyncCommitIntent &&
				!StatusActive(job.Status)) ||
				(state == MailTLSSyncCommitPublished &&
					job.Status != StatusSucceeded) {
				return errors.New("service mutation ledger mail TLS commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, DnsClusterConfigCommitPhasePrefix) {
			state, requestID, qualifier, err :=
				ParseDNSClusterConfigCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				qualifier != job.PackageName ||
				job.Kind != "dns_cluster_configure" || job.Target != "pdns" {
				return errors.New("service mutation ledger has an invalid DNS cluster commit receipt")
			}
			if (state == DnsClusterConfigCommitIntent &&
				!StatusActive(job.Status)) ||
				(state == DnsClusterConfigCommitPublished &&
					job.Status != StatusSucceeded) {
				return errors.New("service mutation ledger DNS cluster commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, DnsZoneSyncCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				ParseDNSZoneSyncCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "dns_zone_sync" ||
				!ServiceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid DNS zone commit receipt")
			}
			if ((state == DnsZoneSyncCommitIntent ||
				state == DnsZoneSyncCommitApplied) &&
				!StatusActive(job.Status)) ||
				(state == DnsZoneSyncCommitPublished &&
					job.Status != StatusSucceeded) {
				return errors.New("service mutation ledger DNS zone commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, DnsZoneSyncV3CommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				ParseDNSZoneSyncV3Phase(job.Phase)
			if err != nil || requestID != job.RequestID || domain != job.Target ||
				qualifier != job.PackageName || job.Kind != "dns_zone_sync" ||
				!ServiceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid DNS zone V3 receipt")
			}
			validStatus := false
			switch state {
			case DnsZoneSyncV3Applied:
				validStatus = StatusActive(job.Status)
			case DnsZoneSyncV3PropagationPending:
				validStatus = job.Status == StatusPending
			case DnsZoneSyncV3Recovering:
				validStatus = StatusActive(job.Status)
			case DnsZoneSyncV3Published:
				validStatus = job.Status == StatusSucceeded
			}
			if !validStatus {
				return errors.New("service mutation ledger DNS zone V3 receipt conflicts with job status")
			}
		}
		if job.Status == StatusPending &&
			!strings.HasPrefix(job.Phase, DnsZoneSyncV3CommitPhasePrefix) {
			return errors.New("pending service mutation lacks an exact DNS zone V3 receipt")
		}
		if strings.HasPrefix(job.Phase, PanelCertificateIssueCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				ParsePanelCertificateIssueCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "panel_certificate_issue" ||
				!ServiceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid panel certificate commit receipt")
			}
			if (state == PanelCertificateIssueCommitIntent &&
				job.Status != StatusRunning &&
				job.Status != StatusCancelling) ||
				(state == PanelCertificateIssueCommitPublished &&
					job.Status != StatusSucceeded) {
				return errors.New("service mutation ledger panel certificate commit receipt conflicts with job status")
			}
		}
		if strings.HasPrefix(job.Phase, MailHostCertificateCommitPhasePrefix) {
			state, requestID, domain, qualifier, err :=
				ParseMailHostCertificateCommitPhase(job.Phase)
			if err != nil || requestID != job.RequestID ||
				domain != job.Target || qualifier != job.PackageName ||
				job.Kind != "mail_host_certificate" ||
				!ServiceMutationCanonicalFQDN(job.Target) {
				return errors.New("service mutation ledger has an invalid mail host certificate commit receipt")
			}
			if (state == MailHostCertificateCommitIntent &&
				job.Status != StatusRunning &&
				job.Status != StatusCancelling) ||
				(state == MailHostCertificateCommitPublished &&
					job.Status != StatusSucceeded) {
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
		case StatusRunning,
			StatusCancelling,
			StatusOrphaned:
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
		case StatusPending,
			StatusSucceeded, StatusFailed:
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

func StatusActive(status string) bool {
	return status == StatusRunning ||
		status == StatusCancelling ||
		status == StatusOrphaned
}

func ValidIdentity(value string) bool {
	if len(value) != 32 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

// Encode preserves the historical v1 canonical bytes after complete validation.
// It does not acquire authority, inspect a host or write files.
func Encode(ledger *Ledger) ([]byte, error) {
	if err := Validate(ledger); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(ledger)
	if err != nil {
		return nil, err
	}
	if len(raw) > MaxSize {
		return nil, errors.New("service mutation ledger exceeds size limit")
	}
	return raw, nil
}
