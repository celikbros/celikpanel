//go:build !linux

package main

import (
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func TestServiceMutationSecurityPolicyPreflightFailsClosedOnUnsupportedHost(t *testing.T) {
	previous := verifyServiceMutationSecurityPolicy
	verifyServiceMutationSecurityPolicy = hostplatform.VerifyLiveSecurityPolicy
	t.Cleanup(func() {
		verifyServiceMutationSecurityPolicy = previous
	})

	err := serviceMutationSecurityPolicyPreflight()
	if err == nil || !strings.Contains(err.Error(), "unsupported outside Linux") {
		t.Fatalf("unsupported-host mutation error = %v", err)
	}
	if !strings.Contains(err.Error(), "security-policy preflight") {
		t.Fatalf("unsupported-host mutation error lacks central preflight context: %v", err)
	}
}
