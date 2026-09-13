package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/alicelik/celikpanel/internal/hostname"
)

var errServerSetupAccessDNSRequired = errors.New("the reviewed service hostname must resolve publicly before certificate issuance")
var errServerSetupAccessDNSMismatch = fmt.Errorf("%w: public addresses differ from this server", errServerSetupAccessDNSRequired)

// A prerequisite check never starts ACME or edits DNS. Unlike an OS resolver,
// the fixed public resolvers cannot succeed from /etc/hosts or private DNS.
func (p *Panel) runServerSetupAccessDNS(ctx context.Context, plan serverSetupPlan, step serverSetupExecutionStep) (bool, error) {
	if err := p.requireServerSetupAdmission(); err != nil {
		return false, err
	}
	actual, valid := canonicalIPv4(serverPrimaryIP())
	if !valid || actual != plan.ServerIP || step.Qualifier != plan.ServerIP {
		return false, &serverSetupChildFailure{Code: "server_setup_access_dns_address_changed", Message: "This server's address changed after review. Review a new plan before preparing DNS or certificates."}
	}
	return serverSetupAccessDNSWithResolvers(ctx, step.Target, plan.ServerIP, serverPrimaryIPv6(), setupDNSPublicResolvers())
}

func serverSetupAccessDNSWithResolvers(ctx context.Context, domain, ipv4, ipv6 string, resolvers []hostResolver) (bool, error) {
	canonical, err := hostname.CanonicalFQDN(domain)
	expected, valid := canonicalIPv4(ipv4)
	if err != nil || canonical != domain || !valid || expected != ipv4 || len(resolvers) == 0 {
		return false, errors.New("reviewed public DNS identity is incomplete")
	}
	checkCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	type result struct {
		addresses []string
		err       error
	}
	results := make(chan result, len(resolvers))
	for _, resolver := range resolvers {
		go func(resolver hostResolver) {
			addresses, err := resolver.LookupHost(checkCtx, domain)
			results <- result{addresses, err}
		}(resolver)
	}
	for range resolvers {
		var answer result
		select {
		case answer = <-results:
		case <-checkCtx.Done():
			return false, errServerSetupAccessDNSRequired
		}
		if answer.err != nil || len(answer.addresses) == 0 {
			return false, errServerSetupAccessDNSRequired
		}
		found := false
		for _, raw := range answer.addresses {
			ip := net.ParseIP(raw)
			if ip == nil {
				return false, errServerSetupAccessDNSMismatch
			}
			if ip.Equal(net.ParseIP(ipv4)) {
				found = true
				continue
			}
			if ip.To4() == nil && ipv6 != "" && ip.Equal(net.ParseIP(ipv6)) {
				continue
			}
			return false, errServerSetupAccessDNSMismatch
		}
		if !found {
			return false, errServerSetupAccessDNSMismatch
		}
	}
	return true, nil
}

func serverSetupDNSWaitMessage(plan serverSetupPlan, step serverSetupExecutionStep, cause error) *serviceOperationError {
	switch {
	case errors.Is(cause, errServerSetupPrimaryDNSRequired):
		return &serviceOperationError{Code: "server_setup_primary_dns_required", Message: fmt.Sprintf("The primary DNS server %s (%s) has not provided its authoritative catalog yet. Start its DNS setup and check TCP port 53. This setup checks again automatically; no primary panel connection is required.", plan.Draft.PeerNS, plan.Draft.PeerIP)}
	case errors.Is(cause, errServerSetupInfrastructureDNSWaiting):
		return &serviceOperationError{Code: "server_setup_infrastructure_dns_required", Message: fmt.Sprintf("The reviewed DNS records are waiting for the native DNS pair or its exact zone transfer. Prepare the secondary %s (%s) and check TCP/UDP port 53. This setup checks the same operation automatically.", plan.Draft.PeerNS, plan.Draft.PeerIP)}
	default:
		code := "server_setup_access_dns_required"
		if errors.Is(cause, errServerSetupAccessDNSMismatch) {
			code = "server_setup_access_dns_mismatch"
		}
		return &serviceOperationError{Code: code, Message: fmt.Sprintf("Public DNS must resolve %s to %s before a certificate is requested. Check this hostname's A/AAAA records and its zone delegation. For DNS managed here, review the infrastructure DNS records in this wizard; otherwise correct them at the primary DNS server or provider. This check resumes automatically when DNS is verified.", step.Target, step.Qualifier)}
	}
}
