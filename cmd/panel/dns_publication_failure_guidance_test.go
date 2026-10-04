package main

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestDNSPublicationFailureReasonReachesDeletionGuidance(t *testing.T) {
	cause := fmt.Errorf("publish DNS zone deletion: %w", &dnsAgentPublicationError{
		Err: &dnsPublicationFailureReasonError{Reason: transport.DNSPublicationFailureBINDRNDCUnavailable},
	})
	body, ok := dnsPublicationGuidanceAPIError(cause)
	if !ok || body.Code != errCodeDNSPublicationFailed ||
		body.Reason != transport.DNSPublicationFailureBINDRNDCUnavailable {
		t.Fatalf("body=%+v ok=%v", body, ok)
	}
	for _, want := range []string{
		"no usable rndc key", "server owner", "sudo rndc-confgen -a",
		"sudo systemctl restart named", "briefly interrupts DNS answers",
		"/etc/bind/rndc.key", "“Retry this deletion”", "same publication",
		"deletion stays pending", "DNS answers are unaffected",
	} {
		if !strings.Contains(body.Error, want) {
			t.Fatalf("guidance lacks %q: %s", want, body.Error)
		}
	}
	if _, ok := dnsPublicationGuidanceAPIError(&dnsPublicationFailureReasonError{Reason: "private output"}); ok {
		t.Fatal("unreviewed reason accepted")
	}
	if _, ok := dnsPublicationGuidanceAPIError(errors.New("agent did not confirm the exact DNS publication")); ok {
		t.Fatal("generic failure gained a reason")
	}
	// Peer pending codes keep their own guidance.
	peer, ok := dnsPublicationGuidanceAPIError(&dnsZoneV3PropagationPendingError{
		Code: transport.DNSPeerPendingNativeUnknown, Exact: true,
	})
	if !ok || peer.Reason != transport.DNSPeerPendingNativeUnknown {
		t.Fatalf("peer=%+v", peer)
	}
}
