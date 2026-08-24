//go:build !linux

package hostplatform

import "errors"

var errServiceMutationsUnsupported = errors.New(
	"service mutations are unsupported outside Linux; no host changes were made",
)

// InspectLiveSecurityPolicy reports inactive for portability-only builds.
func InspectLiveSecurityPolicy() (SecurityPolicyState, error) {
	return SecurityPolicyInactive, nil
}

// VerifyLiveSecurityPolicy fails closed on portability-only builds.
func VerifyLiveSecurityPolicy() error {
	return errServiceMutationsUnsupported
}
