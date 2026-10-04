package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const mailHostCertificateCommitPhasePrefix = servicemutationledger.MailHostCertificateCommitPhasePrefix
const mailHostCertificateCommitIntent = servicemutationledger.MailHostCertificateCommitIntent
const mailHostCertificateCommitPublished = servicemutationledger.MailHostCertificateCommitPublished

func formatMailHostCertificateCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	return servicemutationledger.FormatMailHostCertificateCommitPhase(state, requestID, domain, qualifier)
}

func parseMailHostCertificateCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	return servicemutationledger.ParseMailHostCertificateCommitPhase(value)
}
