package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/mailtlsconfig"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

var errMailEnrollmentWorkerBusy = errors.New("mail enrollment is waiting for existing host exclusion")

const independentMailPath = "/usr/sbin:/usr/bin:/sbin:/bin"

// This is a separately built, one-shot owner process. It has no RPC listener,
// panel database, license gate or general recovery entrypoint. New enrollment
// requires explicit root-owner dispatch with exact request, owner and kit IDs.
// Existing reviewed mail intent, common locks and versioned evidence still apply.
func runIndependentMailRenewal(args []string, euid int, environment []string) int {
	if err := validateIndependentMailEntry(args, euid, environment); err != nil {
		fmt.Fprintln(os.Stderr, "mail renewal refused: "+err.Error())
		return 2
	}
	if err := os.Setenv("PATH", independentMailPath); err != nil {
		return 2
	}
	var err error
	switch args[0] {
	case "--inspect-build-identity":
		fmt.Printf("component=mail-renewal\nversion=%s\ncommit=%s\n", buildVersion, buildCommit)
		return 0
	case "--start-enrollment", "--continue-enrollment":
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err = launchIndependentMailEnrollment(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, "Mail enrollment handoff is not confirmed. The server owner must inspect celikpanel-mail-enrollment-"+args[1]+".service and the same request before retrying; no completion is claimed.")
			return 1
		}
		fmt.Fprintln(os.Stdout, "Mail enrollment worker accepted: "+args[1]+". Completion is not yet verified. Observe celikpanel-mail-enrollment-"+args[1]+".service; after interruption, continue this same request.")
		return 0
	case "--enrollment-worker":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err = runIndependentMailEnrollmentWorker(ctx, args[1:]); err != nil {
			fmt.Fprintln(os.Stderr, mailEnrollmentWorkerGuidance(err)+" Request: "+args[1])
			return 1
		}
		return 0
	case "--enroll-under-lock":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err = runIndependentMailEnrollment(ctx, args[1], args[2], args[3]); err != nil {
			fmt.Fprintln(os.Stderr, mailEnrollmentWorkerGuidance(err)+" Request: "+args[1])
			return 1
		}
		fmt.Fprintln(os.Stdout, "Mail enrollment result verified for request: "+args[1])
		return 0
	case "--resume-enrollment":
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err = resumeIndependentMailEnrollment(ctx, args[1]); err != nil {
			fmt.Fprintln(os.Stderr, mailEnrollmentResumeGuidance(err)+" Request: "+args[1])
			return 1
		}
		fmt.Fprintln(os.Stdout, "Recorded mail renewal enrollment result verified: "+args[1])
		return 0
	case "--queue":
		err = queueMailHostCertificateRenewal(args[1])
	case "--process-pending", "--retry-selected", "--retry-failed":
		// A no-work probe does not create volatile runtime or durable enrollment.
		raw, found, readErr := readSecureServiceMutationLedger(mailHostRenewalPendingPath(), 512)
		if readErr != nil {
			err = readErr
			break
		}
		if !found {
			if args[0] == "--retry-failed" {
				err = errors.New("no pending failed renewal matches the owner retry")
				break
			}
			return 0
		}
		var pending mailHostRenewal
		if pending, err = decodeMailHostRenewal(raw); err != nil {
			break
		}
		if err = prepareIndependentMailRuntime(); err != nil {
			break
		}
		if args[0] == "--retry-failed" {
			err = deployPendingMailHostCertificateWithRetry(args[1])
			break
		}
		if args[0] == "--retry-selected" {
			err = recoverIndependentSelectedMailRenewal(pending, args[1])
			if err == nil {
				err = deployPendingMailHostCertificate()
			}
			break
		}
		err = deployPendingMailHostCertificate()
		if errors.Is(err, errMailRenewalCompletionUnverified) || errors.Is(err, errMailRenewalRecoveryRequired) {
			// Reconcile this exact interrupted request. An unselected renewal also
			// requires its immutable admission before-image; never general recovery.
			if recoveryErr := recoverIndependentPendingMailRenewal(pending); recoveryErr != nil {
				err = errors.Join(err, recoveryErr)
			} else {
				err = deployPendingMailHostCertificate()
			}
		}
	}
	if err != nil {
		if independentMailRenewalWait(err) {
			fmt.Fprintln(os.Stderr, "mail renewal is waiting for the current host operation; pending work is retained and the native timer will check it again; no renewal was marked complete")
			return 0
		}
		var budget *mailRenewalRecoveryBudgetError
		if errors.As(err, &budget) {
			fmt.Fprintln(os.Stderr, budget.Error())
			return 1
		}
		var failedBudget *mailRenewalFailedBudgetError
		if errors.As(err, &failedBudget) {
			fmt.Fprintln(os.Stderr, failedBudget.Error())
			return 1
		}
		// Native output/configuration never becomes a command-line error response.
		fmt.Fprintln(os.Stderr, "mail renewal remains pending; the server owner must review the retained operation and native mail service status before retrying; no general recovery was started")
		return 1
	}
	return 0
}

func validateIndependentMailEnvironment(euid int, environment []string) error {
	if euid != 0 {
		return errors.New("root owner authentication is required")
	}
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(name, "CELIKPANEL_") || strings.HasPrefix(name, "LD_") || strings.HasPrefix(name, "DYLD_") {
			return errors.New("development, fault-injection and loader environment overrides are not supported")
		}
	}
	return nil
}

func validateIndependentMailEntry(args []string, euid int, environment []string) error {
	if err := validateIndependentMailEnvironment(euid, environment); err != nil {
		return err
	}
	if len(args) == 4 && (args[0] == "--start-enrollment" || args[0] == "--enrollment-worker" || args[0] == "--enroll-under-lock") && validMutationIdentity(args[1]) && validMutationIdentity(args[2]) && recoveryruntime.ValidDigest(args[3]) {
		return nil
	}
	if len(args) == 2 && (args[0] == "--continue-enrollment" || args[0] == "--enrollment-worker") && validMutationIdentity(args[1]) {
		return nil
	}
	if len(args) == 1 && (args[0] == "--process-pending" || args[0] == "--inspect-build-identity") {
		return nil
	}
	if len(args) == 2 && (args[0] == "--retry-selected" || args[0] == "--retry-failed" || args[0] == "--resume-enrollment") && validMutationIdentity(args[1]) {
		return nil
	}
	if len(args) == 2 && args[0] == "--queue" {
		value := args[1]
		if len(value) == len("celikpanel-mail-")+24 && strings.HasPrefix(value, "celikpanel-mail-") {
			for _, c := range strings.TrimPrefix(value, "celikpanel-mail-") {
				if !(c >= 'a' && c <= 'f' || c >= '0' && c <= '9') {
					return errors.New("invalid managed mail lineage")
				}
			}
			return nil
		}
	}
	return errors.New("supported actions are --start-enrollment <request-id> <owner-id> <reviewed-kit-digest>, --continue-enrollment <recorded-id>, --process-pending, --queue <managed-mail-lineage>, --retry-selected <recorded-operation-id>, --retry-failed <recorded-operation-id>, --resume-enrollment <recorded-operation-id> (requires inherited release fd9 and host fd8), and --inspect-build-identity")
}

func validateIndependentMailSupervisor(args []string, euid int, environment []string) error {
	if err := validateIndependentMailEnvironment(euid, environment); err != nil {
		return err
	}
	if len(args) < 2 || args[0] != "/run/celikpanel/service-mutation.lock" {
		return errors.New("mail renewal supervisor requires the fixed host lock")
	}
	if err := validateIndependentMailCommand(args[1], args[2:]); err != nil {
		return err
	}
	return nil // The shared supervisor next proves the inherited locked descriptor.
}

func validateIndependentMailCommand(path string, args []string) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return errors.New("mail renewal command must use a canonical native executable")
	}
	name := filepath.Base(path)
	dir := filepath.Dir(path)
	if dir != "/usr/sbin" && dir != "/usr/bin" && dir != "/sbin" && dir != "/bin" {
		return errors.New("mail renewal command is outside native executable directories")
	}
	allowed := false
	switch name {
	case "dovecot":
		allowed = len(args) == 1 && args[0] == "--version"
	case "doveconf":
		allowed = len(args) == 1 && args[0] == "-n"
	case "postconf":
		if len(args) == 2 && args[0] == "-h" {
			allowed = args[1] == "tls_server_sni_maps"
			for _, setting := range mailtlsconfig.PostfixSettings("", "", "") {
				if args[1] == setting[0] {
					allowed = true
				}
			}
		}
	case "systemctl":
		if len(args) == 2 && args[0] == "reload" {
			allowed = args[1] == "postfix.service" || args[1] == "dovecot.service"
		}
		if len(args) == 3 && args[0] == "is-active" && args[1] == "--quiet" {
			allowed = args[2] == "postfix.service" || args[2] == "dovecot.service"
		}
	}
	if !allowed {
		return errors.New("command is outside mail renewal observation and reload scope")
	}
	return nil
}

// Only a wholly known exclusion wait is handled as a deferred timer invocation.
// Joined read failures, unknown worker state and terminal failures retain their
// error result even if one nested cause also reports a busy lock. Invocation exit
// zero does not acknowledge the pending queue or write a successful ledger job.
func independentMailRenewalWait(err error) bool {
	if err == nil {
		return false
	}
	if err == errServiceMutationBusy || err == errServiceMutationHostBusy {
		return true
	}
	switch e := err.(type) {
	case interface{ Unwrap() []error }:
		children := e.Unwrap()
		if len(children) == 0 {
			return false
		}
		for _, child := range children {
			if !independentMailRenewalWait(child) {
				return false
			}
		}
		return true
	case interface{ Unwrap() error }:
		return independentMailRenewalWait(e.Unwrap())
	}
	return false
}

// Do not expose raw command output, paths or persisted job messages at this
// boundary. Known evidence/timeout states retain their meaning; unknown remains
// unverified, not a claim that the native service stopped or enrollment failed.
func mailEnrollmentReason(err error) string {
	reason := "The recorded mail enrollment result could not be verified."
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		reason = "Mail enrollment observation was interrupted or timed out; the native result is unknown."
	case errors.Is(err, servicemutationledger.ErrMailEnrollment):
		reason = "Mail enrollment records do not match this accepted request; no replacement request was admitted."
	default:
		var runtime *recoveryruntime.Error
		if errors.As(err, &runtime) {
			switch runtime.Reason {
			case recoveryruntime.ReasonChanged, recoveryruntime.ReasonContentMismatch, recoveryruntime.ReasonInvalidManifest:
				reason = "Mail enrollment source or recovery evidence changed; the existing evidence was preserved."
			case recoveryruntime.ReasonUnsafeMetadata:
				reason = "Mail enrollment source, ownership or inherited lock evidence could not be verified."
			case recoveryruntime.ReasonUnsupported:
				reason = "This release does not verify the recorded mail enrollment source or format."
			}
		}
	}
	return reason
}

func mailEnrollmentResumeGuidance(err error) string {
	return mailEnrollmentReason(err) + " The server owner must inspect this request's journal and native renewal units, resolve the reported prerequisite, then resume the same request with release fd9 and host fd8 held. No new enrollment or update was started."
}

// Worker failures never ask owners to manipulate descriptors or invent evidence.
func mailEnrollmentWorkerGuidance(err error) string {
	reason := mailEnrollmentReason(err)
	if errors.Is(err, errMailEnrollmentWorkerBusy) {
		return "Mail enrollment is waiting because another operation holds the release or host lock. No enrollment work was started by this worker. The server owner must wait for that operation to finish, then continue the same recorded request or retry the original reviewed tuple if admission never occurred."
	}
	return reason + " The server owner must inspect this request's native worker journal and renewal units. Resolve the reported prerequisite, then use --continue-enrollment with the same recorded request; if admission never occurred, retry the original reviewed start tuple. Existing evidence is retained."
}
