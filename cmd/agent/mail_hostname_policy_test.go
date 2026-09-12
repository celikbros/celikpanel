package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func TestMailProfilesCannotRenameSystemHostname(t *testing.T) {
	for _, profile := range []string{core.MailProfileCore, core.MailProfileWebmail, core.MailProfileProtected} {
		job := mutationPolicyJob("mail_profile_install", profile, "")
		claim := mutationPolicyClaim(serviceMutationStepSetServerHostname, "server-hostname", "", "set")
		if err := authorizeServiceMutationStep(job, claim); err == nil {
			t.Fatalf("mail profile %s authorized an OS rename", profile)
		}
	}
}
