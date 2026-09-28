//go:build !linux

package main

import (
	"context"
	"errors"
)

func verifyEnrolledBINDPeerDeletion(context.Context, dnsPeerAXFRAuthority, dnsV3PrimaryPropagationPlan) error {
	return errors.New("native BIND peer deletion inspection is unavailable")
}

func retireTerminalBINDPeerChallenge(string, string, *ServiceMutationJob) error {
	return nil
}

func verifyEnrolledDNSPeerDeletion(context.Context, dnsPeerAXFRAuthority, dnsV3PrimaryPropagationPlan) error {
	return errors.New("native DNS peer deletion inspection is unavailable")
}
func retireTerminalPDNSPeerChallenge(string, string, *ServiceMutationJob) error { return nil }
