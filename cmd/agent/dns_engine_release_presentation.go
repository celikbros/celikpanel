package main

import (
	"fmt"
	"path/filepath"

	"github.com/alicelik/celikpanel/internal/dnsenginerecovery"
)

// releasedDNSSwitchReconciledMessage replaces, only in a status answer, the
// stored message of the Agent's deliberate DNS lease release once no journal
// is retained for that request. The stored message describes the moment of
// release ("journal remains … new DNS changes are blocked"); after the owner
// recovery command or a later Agent start retires the journal, repeating it
// as present state would be false.
const releasedDNSSwitchReconciledMessage = "An interrupted DNS switch (request %s) could not be recovered automatically when the Agent restarted and was reconciled later. Its journal is retired and it no longer blocks DNS changes. Check the current DNS engine status before another switch."

// presentReleasedDNSSwitchJob computes the status answer for one job at read
// time. The ledger job itself is never rewritten: its terminal verdict stays
// the one the operator was given. Only when the job is exactly the Agent's
// deliberate release and the journal read proves no journal of that request
// remains is the message replaced. An unreadable journal is not absence, so it
// keeps the stored message.
//
// presentReleasedDNSSwitchJob, durum yanıtını okuma anında hesaplar. Defterdeki
// iş hiçbir zaman yeniden yazılmaz. Yalnız iş Agent'ın bilinçli bırakması ise
// ve o isteğe ait günlüğün kalmadığı okunarak kanıtlanırsa ileti değiştirilir.
func (m *serviceMutationManager) presentReleasedDNSSwitchJob(job *ServiceMutationJob) *ServiceMutationJob {
	if m == nil || job == nil || !dnsenginerecovery.AgentDeliberateReleaseJob(*job) {
		return job
	}
	journal, exists, err := readDNSEngineSwitchJournalAt(
		filepath.Join(filepath.Dir(m.ledgerPath), dnsEngineSwitchJournalFile),
	)
	if err != nil || (exists && journal.MutationRequestID == job.RequestID) {
		return job
	}
	presented := *job
	presented.ErrorMessage = fmt.Sprintf(releasedDNSSwitchReconciledMessage, job.RequestID)
	return &presented
}
