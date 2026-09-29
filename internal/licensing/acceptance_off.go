//go:build !acceptance_license

package licensing

import "crypto/ed25519"

// AcceptanceFixtureBuild is false in every ordinary build. The acceptance
// fixture license exists only in acceptance_fixture.go, which compiles solely
// with the acceptance_license test tag; release packaging refuses that tag
// (deploy/release-acceptance-license-guard.sh).
const AcceptanceFixtureBuild = false

// acceptanceStatus adds no fields, so ordinary license status JSON is unchanged.
type acceptanceStatus struct{}

// NewServer is the constructor the panel uses for its server license. In an
// ordinary build it is exactly New: no fixture license, no alternate verifier,
// no environment variable and no file lookup, so no configuration can switch
// an ordinary binary into acceptance mode.
func NewServer(file string, key ed25519.PublicKey, server string) (*Manager, error) {
	return New(file, key, server)
}
