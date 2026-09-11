package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServerSetupFirewallReadinessIncludesServiceAndSSHPorts(t *testing.T) {
	base := transport.FirewallStatusResponse{Enabled: true, EngineAvailable: true, PersistenceState: "ready", SSHPorts: []int{2222}, TCPPorts: []int{80, 443, 53, 2222, 2083}, UDPPorts: []int{53}}
	if !setupFirewallReady(base, []string{"nginx", "pdns"}, 2083) {
		t.Fatal("complete policy not ready")
	}
	for _, test := range []struct {
		name   string
		change func(*transport.FirewallStatusResponse)
	}{
		{"web HTTPS", func(s *transport.FirewallStatusResponse) { s.TCPPorts = []int{80, 53, 2222, 2083} }},
		{"DNS TCP", func(s *transport.FirewallStatusResponse) { s.TCPPorts = []int{80, 443, 2222, 2083} }},
		{"DNS UDP", func(s *transport.FirewallStatusResponse) { s.UDPPorts = nil }},
		{"SSH", func(s *transport.FirewallStatusResponse) { s.TCPPorts = []int{80, 443, 53, 2083} }},
		{"persistence", func(s *transport.FirewallStatusResponse) { s.PersistenceState = "disabled" }},
		{"persistence failure", func(s *transport.FirewallStatusResponse) { s.PersistenceError = "write failed" }},
		{"SSH discovery", func(s *transport.FirewallStatusResponse) { s.SSHDiscoveryReason = transport.SSHDiscoveryProbeFailed }},
	} {
		t.Run(test.name, func(t *testing.T) {
			status := base
			test.change(&status)
			if setupFirewallReady(status, []string{"nginx", "pdns"}, 2083) {
				t.Fatal("incomplete firewall falsely ready")
			}
		})
	}
}

func TestServerSetupMailIdentityRequiresExactPublicFCrDNS(t *testing.T) {
	base := transport.MailHealthResponse{ServerIP: "203.0.113.42", Myhostname: "mail.example.test", HostnameFQDN: true, PTR: "mail.example.test", PTRAligned: true, FCrDNS: true}
	if !setupMailHostIdentityReady(base, "mail.example.test") {
		t.Fatal("verified identity not ready")
	}
	for _, test := range []struct {
		name   string
		change func(*transport.MailHealthResponse)
	}{
		{"wrong hostname", func(h *transport.MailHealthResponse) { h.Myhostname = "another.example.test" }},
		{"different PTR", func(h *transport.MailHealthResponse) { h.PTR = "another.example.test" }},
		{"no reverse alignment", func(h *transport.MailHealthResponse) { h.PTRAligned = false }},
		{"no forward confirmation", func(h *transport.MailHealthResponse) { h.FCrDNS = false }},
		{"private deployment", func(h *transport.MailHealthResponse) { h.ServerIP = "192.168.1.1" }},
		{"failed observation", func(h *transport.MailHealthResponse) { h.Error = "unknown" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			health := base
			test.change(&health)
			if setupMailHostIdentityReady(health, "mail.example.test") {
				t.Fatal("unverified mail identity falsely ready")
			}
		})
	}
}

func TestServerSetupMailHostCertificateRequiresIdentityValidityAndRenewal(t *testing.T) {
	proof := transport.MailHostCertificateStatusResponse{Ready: true, RenewalReady: true, Domain: "mail.example.test", ExpiresAt: time.Now().Add(time.Hour)}
	if !setupMailHostCertificateReady(proof, "mail.example.test") {
		t.Fatal("trusted current host proof rejected")
	}
	for _, change := range []func(*transport.MailHostCertificateStatusResponse){
		func(p *transport.MailHostCertificateStatusResponse) { p.Ready = false },
		func(p *transport.MailHostCertificateStatusResponse) { p.RenewalReady = false },
		func(p *transport.MailHostCertificateStatusResponse) { p.Domain = "wrong.example.test" },
		func(p *transport.MailHostCertificateStatusResponse) { p.ExpiresAt = time.Now().Add(-time.Second) },
		func(p *transport.MailHostCertificateStatusResponse) { p.Error = "unavailable" },
	} {
		bad := proof
		change(&bad)
		if setupMailHostCertificateReady(bad, "mail.example.test") {
			t.Fatalf("false ready: %+v", bad)
		}
	}
}

func TestServerSetupMailTLSRequiresTrustedMatchingCertificate(t *testing.T) {
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	server.Config.ErrorLog = log.New(io.Discard, "", 0)
	server.StartTLS()
	defer server.Close()
	cert := server.Certificate()
	name := cert.DNSNames[0]
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	config := &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	address := server.Listener.Addr().String()
	if err := probeSetupMailTLS(context.Background(), address, name, false, config); err != nil {
		t.Fatalf("trusted matching listener: %v", err)
	}
	if err := probeSetupMailTLS(context.Background(), address, "wrong.example.test", false, config); err == nil {
		t.Fatal("wrong hostname accepted")
	}
	if err := probeSetupMailTLS(context.Background(), address, name, false, nil); err == nil {
		t.Fatal("untrusted self-signed listener accepted")
	}
	if err := probeSetupMailTLS(context.Background(), address, name, false, &tls.Config{InsecureSkipVerify: true}); err == nil {
		t.Fatal("verification bypass accepted")
	}
	if err := probeSetupMailTLS(context.Background(), "203.0.113.2:993", name, false, config); err == nil {
		t.Fatal("arbitrary remote probe accepted")
	}
}

func TestServerSetupSMTPProbeRequiresSTARTTLSAndAUTHWithoutSendingMail(t *testing.T) {
	certificateServer := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer certificateServer.Close()
	cert := certificateServer.Certificate()
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	for _, test := range []struct {
		name                      string
		startTLS, auth, wantReady bool
	}{{"ready", true, true, true}, {"no STARTTLS", false, true, false}, {"no authentication", true, false, false}} {
		t.Run(test.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			commands := make(chan []string, 1)
			go func() {
				var seen []string
				defer func() { commands <- seen }()
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				reader := bufio.NewReader(conn)
				_, _ = fmt.Fprint(conn, "220 mail.example.test ESMTP\r\n")
				secured := false
				for {
					line, err := reader.ReadString('\n')
					if err != nil {
						return
					}
					seen = append(seen, strings.TrimSpace(line))
					switch {
					case strings.HasPrefix(line, "EHLO "):
						_, _ = fmt.Fprint(conn, "250-mail.example.test\r\n")
						if test.startTLS && !secured {
							_, _ = fmt.Fprint(conn, "250-STARTTLS\r\n")
						}
						if test.auth {
							_, _ = fmt.Fprint(conn, "250 AUTH PLAIN\r\n")
						} else {
							_, _ = fmt.Fprint(conn, "250 SIZE 1000\r\n")
						}
					case strings.TrimSpace(line) == "STARTTLS":
						_, _ = fmt.Fprint(conn, "220 ready\r\n")
						conn = tls.Server(conn, &tls.Config{Certificates: certificateServer.TLS.Certificates, MinVersion: tls.VersionTLS12})
						if err := conn.(*tls.Conn).Handshake(); err != nil {
							return
						}
						reader = bufio.NewReader(conn)
						secured = true
					default:
						return
					}
				}
			}()
			err = probeSetupMailTLS(context.Background(), listener.Addr().String(), cert.DNSNames[0], true, &tls.Config{RootCAs: pool})
			if (err == nil) != test.wantReady {
				t.Fatalf("readiness=%v error=%v", test.wantReady, err)
			}
			for _, command := range <-commands {
				if strings.HasPrefix(command, "AUTH ") || strings.HasPrefix(command, "MAIL ") || strings.HasPrefix(command, "RCPT ") || command == "DATA" {
					t.Fatalf("readiness sent a message or credentials: %s", command)
				}
			}
		})
	}
}
