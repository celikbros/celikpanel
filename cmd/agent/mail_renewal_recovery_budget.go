package main

import (
	"errors"
	"fmt"
)

// Attempt already counts durable executions of this exact v1 operation. Recovery
// reserves its next attempt in the same intent write, before any native reload.
// The initial publication plus two automatic recoveries consume this budget.
const mailRenewalAutomaticAttemptLimit = 3

type mailRenewalRecoveryBudgetError struct{ RequestID string }

func (e *mailRenewalRecoveryBudgetError) Error() string {
	return fmt.Sprintf("mail renewal automatic recovery is paused after three recorded execution attempts for operation %s; the server owner must check Postfix/Dovecot status and configuration, resolve the reported cause, then run the installed renewal helper with --retry-selected %s for one explicit attempt; the selected certificate and pending work are preserved", e.RequestID, e.RequestID)
}

func admitMailRenewalSelectedRecovery(job *ServiceMutationJob, ownerRequest string) error {
	if job == nil || !validMutationIdentity(job.RequestID) || job.Attempt < 1 || job.Attempt == int(^uint(0)>>1) {
		return errors.New("invalid mail renewal recovery attempt evidence")
	}
	if ownerRequest != "" {
		if !validMutationIdentity(ownerRequest) || ownerRequest != job.RequestID {
			return errors.New("explicit mail recovery belongs to another operation")
		}
		return nil
	}
	if job.Attempt >= mailRenewalAutomaticAttemptLimit {
		return &mailRenewalRecoveryBudgetError{RequestID: job.RequestID}
	}
	return nil
}
