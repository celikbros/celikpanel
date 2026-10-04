package main

import (
	"errors"
	"os"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
)

func decodeServiceMutationLedger(raw []byte) (serviceMutationLedger, error) {
	return servicemutationledger.Decode(raw)
}

func payloadBoundDirectMutationPublishedPhase(
	job *ServiceMutationJob,
) (string, bool, error) {
	return servicemutationledger.PublishedPhase(job)
}

func validatePayloadBoundDirectMutationSuccess(job *ServiceMutationJob) error {
	return servicemutationledger.ValidateSuccess(job)
}

func validateServiceMutationLedger(ledger *serviceMutationLedger) error {
	return servicemutationledger.Validate(ledger)
}

func serviceMutationStatusActive(status string) bool {
	return servicemutationledger.StatusActive(status)
}

func validMutationIdentity(value string) bool {
	return servicemutationledger.ValidIdentity(value)
}

const serviceMutationLedgerVersion = servicemutationledger.Version

const serviceMutationLedgerMaxSize = servicemutationledger.MaxSize

const serviceMutationStatusRunning = servicemutationledger.StatusRunning

const serviceMutationStatusCancelling = servicemutationledger.StatusCancelling

const serviceMutationStatusOrphaned = servicemutationledger.StatusOrphaned

const serviceMutationStatusPending = servicemutationledger.StatusPending

const serviceMutationStatusSucceeded = servicemutationledger.StatusSucceeded

const serviceMutationStatusFailed = servicemutationledger.StatusFailed

type ServiceMutationJob = servicemutationledger.ServiceMutationJob
type serviceMutationLedger = servicemutationledger.Ledger

var errServiceMutationHostBusy = errors.New("the host package manager or mutation lock is busy")

func encodeServiceMutationLedger(ledger *serviceMutationLedger) ([]byte, error) {
	return servicemutationledger.Encode(ledger)
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
