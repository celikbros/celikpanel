package main

import (
	"errors"
	"testing"
)

func TestMailEnrollmentRPCPoliciesSeparateObservationAndMutation(t *testing.T) {
	for _, method := range []string{"Agent.MailEnrollmentStatusV1", "Agent.MailEnrollmentPreviewV1"} {
		read, err := agentRPCPolicyForMethod(method)
		if err != nil || read.effect != agentRPCEffectRead || read.capability != "" || read.timeout != agentRPCQuickReadTimeout {
			t.Fatalf("read policy: %+v %v", read, err)
		}
	}
	if _, err := agentRPCPolicyForMethod("Agent.MailEnrollmentSourceV1"); !errors.Is(err, errAgentRPCPolicyMissing) {
		t.Fatalf("unused source RPC unexpectedly granted a panel policy: %v", err)
	}
	for _, method := range []string{"Agent.StartMailEnrollmentV1", "Agent.ContinueMailEnrollmentV1"} {
		start, err := agentRPCPolicyForMethod(method)
		if err != nil || start.effect != agentRPCEffectHostMutation || start.capability != agentRPCCapabilityMail || start.timeout != agentRPCQuickReadTimeout {
			t.Fatalf("start policy: %+v %v", start, err)
		}
	}
}
