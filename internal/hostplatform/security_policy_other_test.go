//go:build !linux

package hostplatform

import "testing"

func TestVerifyLiveSecurityPolicyFailsClosedOutsideLinux(t *testing.T) {
	state, err := InspectLiveSecurityPolicy()
	if err != nil || state != SecurityPolicyInactive {
		t.Fatalf("non-Linux security-policy state = %q, %v; want inactive", state, err)
	}
	first := VerifyLiveSecurityPolicy()
	second := VerifyLiveSecurityPolicy()
	if first == nil || first != second ||
		first.Error() != "service mutations are unsupported outside Linux; no host changes were made" {
		t.Fatalf("non-Linux mutation gate errors = %v, %v", first, second)
	}
}
