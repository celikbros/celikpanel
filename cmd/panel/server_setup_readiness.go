package main

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/core"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

func setupCheck(id string, ready bool, err error) serverSetupCheck {
	check := serverSetupCheck{ID: id, State: "action_required", Code: id + "_required"}
	if err != nil {
		check.State = "unknown"
		check.Code = id + "_unavailable"
	} else if ready {
		check.State = "ready"
		check.Code = "ready"
	}
	return check
}

func setupTLSReady(ctx context.Context, domain string) bool {
	if domain == "" {
		return false
	}
	host, port, err := net.SplitHostPort(listenAddr())
	if err != nil {
		return false
	}
	if host == "" || host == "0.0.0.0" {
		host = "127.0.0.1"
	}
	if host == "::" {
		host = "::1"
	}
	// Dial only the configured listener; the name is used for certificate
	// verification, never as an arbitrary network target.
	// Yalniz panel dinleyicisine baglan; alan adi sertifika dogrulamasi icindir.
	dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 3 * time.Second}, Config: &tls.Config{ServerName: domain, MinVersion: tls.VersionTLS12}}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, port))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (p *Panel) serverSetupCompletionChecks(ctx context.Context, draft serverSetupDraft) ([]serverSetupCheck, error) {
	if p.serverSetupProbe != nil {
		return p.serverSetupProbe(ctx, draft)
	}
	ctx, cancel := context.WithTimeout(ctx, 18*time.Second)
	defer cancel()
	checks := []serverSetupCheck{}
	cert := currentPanelCert()
	tlsReady := cert.HTTPSEnabled && !cert.SelfSigned && cert.ExpiresAt.After(time.Now()) && setupTLSReady(ctx, draft.PanelDomain)
	checks = append(checks, setupCheck("panel_https", tlsReady, nil))
	var renewal transport.PanelRenewalReadinessResponse
	var renewalErr error
	if draft.PanelDomain != "" {
		renewalErr = p.callAgentContext(ctx, "Agent.PanelRenewalReadiness", &transport.PanelRenewalReadinessRequest{Domain: draft.PanelDomain}, &renewal)
	}
	checks = append(checks, setupCheck("panel_renewal", renewal.Ready, renewalErr))
	var firewall transport.FirewallStatusResponse
	firewallErr := p.callAgentContext(ctx, "Agent.FirewallStatus", &transport.Empty{}, &firewall)
	// Required ports are checked after the fresh installed-service inventory
	// below; a saved enabled flag cannot prove that current workloads are reachable.
	mode, modeErr := p.setupDNSManagementMode(ctx)
	dnsReady := false
	if modeErr == nil && mode == draft.DNSMode && !(draft.Purpose == "dns" && mode != "local") {
		dnsReady, modeErr = p.setupDNSModeReadiness(ctx, mode)
	}
	if dnsReady && mode == setupDNSModeExisting {
		var connectionID string
		connectionID, modeErr = p.defaultRemoteDNSConnectionID(ctx)
		dnsReady = modeErr == nil && connectionID == draft.RemoteDNSConnectionID
	}
	if dnsReady && mode == setupDNSModeLocal && serverSetupNeedsDNSPublisher(draft) {
		_, dnsReady, modeErr = p.activeDNSPublisher(ctx)
	}
	if dnsReady && mode == setupDNSModeLocal {
		dnsReady, modeErr = p.setupDNSNameserverReadiness(ctx)
	}
	dnsCheck := setupCheck("dns", dnsReady, modeErr)
	if errors.Is(modeErr, errSetupDNSPeerIPv6Unverified) {
		dnsCheck.State, dnsCheck.Code = "action_required", "dns_peer_ipv6_unverified"
	}
	checks = append(checks, dnsCheck)
	var installed []string
	installedErr := p.callAgentContext(ctx, "Agent.InstalledServiceIDsStrict", &transport.Empty{}, &installed)
	firewallReady := installedErr == nil && setupFirewallReady(firewall, installed, panelPort())
	if firewallErr == nil {
		firewallErr = installedErr
	}
	checks = append(checks, setupCheck("firewall", firewallReady, firewallErr))
	var units []core.Service
	unitsErr := p.callAgentContext(ctx, "Agent.GetServices", &transport.Empty{}, &units)
	required := []string{}
	switch draft.Purpose {
	case "web", "web_mail":
		required = []string{"nginx", "php-fpm", "mariadb"}
	case "application":
		required = []string{"nginx"}
		if draft.Database != "" && draft.Database != "none" {
			required = append(required, draft.Database)
		}
	case "dns":
		required = []string{draft.DNSEngine}
	}
	if draft.Purpose == "web_mail" {
		required = append(required, "postfix", "dovecot", "rspamd", "roundcube")
	}
	if draft.Customization != nil {
		var selectionErr error
		required, selectionErr = serverSetupResolvedComponents(draft)
		if selectionErr != nil {
			return nil, selectionErr
		}
		// A selected Node runtime is proven by its exact version, independently
		// of the distro package inventory.
		withoutNode := []string{}
		for _, id := range required {
			if id != "node" {
				withoutNode = append(withoutNode, id)
			}
		}
		required = withoutNode
		if draft.DNSMode == "local" {
			required = append(required, draft.DNSEngine)
		}
		required = append(required, serverSetupRequiredComponents...)
	}
	servicesReady := installedErr == nil && unitsErr == nil && len(required) > 0
	if draft.Customization != nil && len(draft.Customization.Components) == 0 && !(draft.DNSMode == "local" && stringIn(draft.Purpose, "dns", "custom")) {
		servicesReady = false
	}
	pkgFamily := ""
	var serviceHost core.ManagedServiceHostProfile
	if servicesReady {
		serviceHost = p.managedServiceHostProfile()
		pkgFamily = serviceHost.PackageFamily
	}
	installedSet := map[string]bool{}
	for _, id := range installed {
		installedSet[id] = true
	}
	for _, id := range required {
		present := false
		for _, installedID := range installed {
			if id == installedID {
				present = true
			}
		}
		managed := core.GetManagedServiceByID(id)
		running := false
		if managed != nil {
			if managed.Kind != core.KindService {
				running = present
			}
			for _, unit := range units {
				if core.UnitBelongsTo(unit.Name, managed) && managedServiceUnitReady(id, pkgFamily, unit.Name, unit.Status) {
					running = true
				}
			}
		}
		// PHP needs an actual running pool service despite its runtime kind.
		// PHP calisan havuz servisi gerektirir.
		if id == "php-fpm" {
			running = false
			if managed != nil {
				for _, unit := range units {
					if core.UnitBelongsTo(unit.Name, managed) && managedServiceUnitReady(id, pkgFamily, unit.Name, unit.Status) {
						running = true
					}
				}
			}
		}
		if draft.Customization != nil && managed != nil {
			_, unsupported := core.ManagedServiceInstallBlockForHost(managed, serviceHost)
			if unsupported != "" || len(core.RequirementsMissing(managed, installedSet)) > 0 || core.SeatTakenBy(managed, installedSet) != "" {
				running = false
			}
			for _, helper := range managed.HelperUnits {
				helperRunning := false
				for _, unit := range units {
					if strings.TrimSuffix(unit.Name, ".service") == strings.TrimSuffix(helper, ".service") && managedServiceUnitReady(id, pkgFamily, unit.Name, unit.Status) {
						helperRunning = true
					}
				}
				running = running && helperRunning
			}
		}
		servicesReady = servicesReady && present && running
	}
	if (draft.Customization == nil && draft.Purpose == "application") || (draft.Customization != nil && serverSetupHasComponent(draft, "node")) {
		var versions transport.NodeVersionsResponse
		err := p.callAgentContext(ctx, "Agent.ListNodeVersions", &transport.Empty{}, &versions)
		found := false
		for _, version := range versions.Installed {
			if strings.TrimPrefix(version, "v") == strings.TrimPrefix(draft.NodeVersion, "v") {
				found = true
			}
		}
		servicesReady = servicesReady && err == nil && found
		if draft.Customization != nil {
			_, unsupported := core.ManagedServiceInstallBlockForHost(core.GetManagedServiceByID("node"), serviceHost)
			servicesReady = servicesReady && unsupported == "" && nodeSemverRe.MatchString(draft.NodeVersion)
		}
	}
	serviceErr := installedErr
	if serviceErr == nil {
		serviceErr = unitsErr
	}
	checks = append(checks, setupCheck("services", servicesReady, serviceErr))
	if mailProfiles := serverSetupMailProfileIDs(draft); len(mailProfiles) > 0 {
		proofs, proofErr := p.latestMailProfileAttemptProofs(ctx)
		profilesReady := proofErr == nil
		for _, profileID := range mailProfiles {
			profilesReady = profilesReady && proofs[profileID].Verified
		}
		// Installation receipts may truthfully report a self-signed fallback.
		// Read the protected host certificate proof and verify the actual listeners rather
		// than treating that historical installation result as trusted mail TLS.
		mailTLSSyncMu.Lock()
		mailHost, _, snapshotErr := p.loadMailTLSSnapshotLocked(ctx, 0)
		mailTLSSyncMu.Unlock()
		var hostCertificate transport.MailHostCertificateStatusResponse
		hostErr := p.callAgentContext(ctx, "Agent.MailHostCertificateStatus", &transport.MailHostCertificateStatusRequest{Domain: draft.MailHostname}, &hostCertificate)
		if hostErr == nil && hostCertificate.Error != "" {
			hostErr = errors.New("host mail certificate proof unavailable")
		}
		mailTLSReady := profilesReady && snapshotErr == nil && mailHost == draft.MailHostname && hostErr == nil && setupMailHostCertificateReady(hostCertificate, draft.MailHostname)
		var tlsErr error
		if mailTLSReady {
			tlsErr = probeSetupMailTLS(ctx, "127.0.0.1:587", draft.MailHostname, true, nil)
			if tlsErr == nil {
				tlsErr = probeSetupMailTLS(ctx, "127.0.0.1:993", draft.MailHostname, false, nil)
			}
			mailTLSReady = tlsErr == nil
		}
		mailErr := proofErr
		if mailErr == nil {
			mailErr = hostErr
		}
		if mailErr == nil {
			mailErr = snapshotErr
		}
		if mailErr == nil {
			mailErr = tlsErr
		}
		checks = append(checks, setupCheck("mail_tls", mailTLSReady, mailErr))
		var health transport.MailHealthResponse
		healthErr := p.callAgentContext(ctx, "Agent.MailHealth", &transport.Empty{}, &health)
		if healthErr == nil && health.Error != "" {
			healthErr = errors.New("mail host health could not be read")
		}
		checks = append(checks, setupCheck("mail_identity", setupMailHostIdentityReady(health, draft.MailHostname), healthErr))
		checks = append(checks, setupCheck("mail_delivery", health.Error == "" && health.OutboundPort25 == "open", healthErr))
	}

	return checks, nil
}

func setupPanelURL(domain string) string {
	return "https://" + net.JoinHostPort(domain, strconv.Itoa(panelPort()))
}

func setupFirewallReady(status transport.FirewallStatusResponse, installed []string, panelTCPPort int) bool {
	if status.Error != "" || !status.EngineAvailable || !status.Enabled || status.PersistenceState != "ready" || status.PersistenceError != "" || status.SSHDiscoveryReason != "" || len(status.SSHPorts) == 0 {
		return false
	}
	tcp, udp := map[int]bool{}, map[int]bool{}
	for _, port := range status.TCPPorts {
		tcp[port] = true
	}
	for _, port := range status.UDPPorts {
		udp[port] = true
	}
	if !tcp[panelTCPPort] || !tcp[80] {
		return false
	}
	for _, port := range status.SSHPorts {
		if !tcp[port] {
			return false
		}
	}
	for _, id := range installed {
		managed := core.GetManagedServiceByID(id)
		if managed == nil {
			continue
		}
		for _, port := range managed.FirewallPorts {
			if port.Proto == "udp" {
				if !udp[port.Port] {
					return false
				}
			} else if !tcp[port.Port] {
				return false
			}
		}
	}
	return true
}

func setupMailHostCertificateReady(proof transport.MailHostCertificateStatusResponse, domain string) bool {
	return proof.Error == "" && proof.Domain == domain && domain != "" && proof.Ready && proof.RenewalReady && proof.ExpiresAt.After(time.Now())
}

func setupMailHostIdentityReady(health transport.MailHealthResponse, mailHostname string) bool {
	canonical, err := hostname.CanonicalFQDN(mailHostname)
	if err != nil || canonical != mailHostname || health.Error != "" {
		return false
	}
	ip := net.ParseIP(health.ServerIP)
	if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() || ip.IsLoopback() {
		return false
	}
	return health.HostnameFQDN && strings.EqualFold(strings.TrimSuffix(health.Myhostname, "."), mailHostname) && health.PTRAligned && health.FCrDNS && strings.EqualFold(strings.TrimSuffix(health.PTR, "."), mailHostname)
}

// Probes only authenticate the already-running local listener. They never log
// in, submit mail, change certificates or select an arbitrary remote target.
func probeSetupMailTLS(ctx context.Context, address, mailHostname string, submission bool, trust *tls.Config) error {
	canonical, err := hostname.CanonicalFQDN(mailHostname)
	if err != nil || canonical != mailHostname {
		return errors.New("invalid mail TLS hostname")
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("mail readiness may only probe a loopback listener")
	}
	config := &tls.Config{ServerName: mailHostname, MinVersion: tls.VersionTLS12}
	if trust != nil {
		config = trust.Clone()
		config.ServerName = mailHostname
		if config.MinVersion < tls.VersionTLS12 {
			config.MinVersion = tls.VersionTLS12
		}
	}
	if config.InsecureSkipVerify {
		return errors.New("mail readiness requires certificate verification")
	}
	probeCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	conn, err := (&net.Dialer{Timeout: 3 * time.Second}).DialContext(probeCtx, "tcp", address)
	if err != nil {
		return err
	}
	defer conn.Close()
	deadline, _ := probeCtx.Deadline()
	if err := conn.SetDeadline(deadline); err != nil {
		return err
	}
	if !submission {
		secured := tls.Client(conn, config)
		defer secured.Close()
		return secured.HandshakeContext(probeCtx)
	}
	client, err := smtp.NewClient(conn, mailHostname)
	if err != nil {
		return err
	}
	defer client.Close()
	if err := client.Hello("localhost"); err != nil {
		return err
	}
	if available, _ := client.Extension("STARTTLS"); !available {
		return errors.New("SMTP submission does not advertise STARTTLS")
	}
	if err := client.StartTLS(config); err != nil {
		return err
	}
	if available, _ := client.Extension("AUTH"); !available {
		return fmt.Errorf("SMTP submission does not advertise authenticated delivery after TLS")
	}
	return nil
}
