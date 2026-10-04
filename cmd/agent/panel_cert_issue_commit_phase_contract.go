package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const panelCertificateIssueCommitPhasePrefix = servicemutationledger.PanelCertificateIssueCommitPhasePrefix
const panelCertificateIssueCommitIntent = servicemutationledger.PanelCertificateIssueCommitIntent
const panelCertificateIssueCommitPublished = servicemutationledger.PanelCertificateIssueCommitPublished

func formatPanelCertificateIssueCommitPhase(
	state, requestID, domain, qualifier string,
) (string, error) {
	return servicemutationledger.FormatPanelCertificateIssueCommitPhase(state, requestID, domain, qualifier)
}

func parsePanelCertificateIssueCommitPhase(value string) (
	state, requestID, domain, qualifier string,
	err error,
) {
	return servicemutationledger.ParsePanelCertificateIssueCommitPhase(value)
}
