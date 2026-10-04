package main

import "github.com/alicelik/celikpanel/internal/servicemutationledger"

const mailTLSSyncCommitPhasePrefix = servicemutationledger.MailTLSSyncCommitPhasePrefix
const mailTLSSyncCommitIntent = servicemutationledger.MailTLSSyncCommitIntent
const mailTLSSyncCommitPublished = servicemutationledger.MailTLSSyncCommitPublished

func formatMailTLSSyncCommitPhase(state, requestID, qualifier string) (string, error) {
	return servicemutationledger.FormatMailTLSSyncCommitPhase(state, requestID, qualifier)
}

func parseMailTLSSyncCommitPhase(value string) (state, requestID, qualifier string, err error) {
	return servicemutationledger.ParseMailTLSSyncCommitPhase(value)
}
