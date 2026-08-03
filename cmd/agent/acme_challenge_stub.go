//go:build !linux

package main

import (
	"fmt"

	"github.com/alicelik/celikpanel/internal/hostingpath"
)

func prepareACMEChallengeRoot(subscriptionID, domainID int) (string, error) {
	challengeRoot, err := hostingpath.ACMEChallengeRoot(subscriptionID, domainID)
	if err != nil {
		return "", err
	}
	return challengeRoot, fmt.Errorf("secure ACME challenge roots require Linux openat2")
}
