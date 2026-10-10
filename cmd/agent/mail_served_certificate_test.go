package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeMailListeners is this host as the served-certificate check sees it:
// which address answers, with which protocol, presenting which certificate.
type fakeMailListeners struct {
	mu sync.Mutex
	// address ("127.0.0.1:465") -> the certificate presented there. A missing
	// address refuses the connection, as a stopped daemon does.
	certificates map[string]tls.Certificate
	// protocol spoken before TLS at an address ("smtp", "imap", "pop3"); TLS
	// from the first byte when absent.
	protocols map[string]string
	// swapAfter: after this many handshakes at an address, present next.
	swapAfter map[string]int
	next      map[string]tls.Certificate
	dialed    []string
	served    map[string]int
}

func installFakeMailListeners(t *testing.T) *fakeMailListeners {
	t.Helper()
	host := &fakeMailListeners{
		certificates: map[string]tls.Certificate{}, protocols: map[string]string{},
		swapAfter: map[string]int{}, next: map[string]tls.Certificate{}, served: map[string]int{},
	}
	oldDial, oldSleep, oldAddresses := mailServedDial, mailServedSleep, mailServedHostAddresses
	t.Cleanup(func() { mailServedDial, mailServedSleep, mailServedHostAddresses = oldDial, oldSleep, oldAddresses })
	mailServedSleep = func(context.Context, time.Duration) {}
	mailServedHostAddresses = func() []string { return nil }
	mailServedDial = func(_ context.Context, address string) (net.Conn, error) {
		host.mu.Lock()
		host.dialed = append(host.dialed, address)
		certificate, listening := host.certificates[address]
		if listening {
			if after, swaps := host.swapAfter[address]; swaps && host.served[address] >= after {
				certificate = host.next[address]
			}
			host.served[address]++
		}
		protocol := host.protocols[address]
		host.mu.Unlock()
		if !listening {
			return nil, errors.New("connect: connection refused")
		}
		client, server := net.Pipe()
		go serveFakeMailListener(server, protocol, certificate)
		return client, nil
	}
	return host
}

func serveFakeMailListener(conn net.Conn, protocol string, certificate tls.Certificate) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	expect := func(command string) bool {
		line, err := reader.ReadString('\n')
		return err == nil && strings.HasPrefix(strings.ToUpper(line), command)
	}
	say := func(text string) { _, _ = conn.Write([]byte(text + "\r\n")) }
	switch protocol {
	case "smtp":
		say("220 mail.example.test ESMTP Postfix")
		if !expect("EHLO ") {
			return
		}
		say("250-mail.example.test")
		say("250-PIPELINING")
		say("250 STARTTLS")
		if !expect("STARTTLS") {
			return
		}
		say("220 2.0.0 Ready to start TLS")
	case "imap":
		say("* OK [CAPABILITY IMAP4rev1 STARTTLS LOGINDISABLED] Dovecot ready.")
		if !expect("A STARTTLS") {
			return
		}
		say("a OK Begin TLS negotiation now.")
	case "pop3":
		say("+OK Dovecot ready.")
		if !expect("STLS") {
			return
		}
		say("+OK Begin TLS negotiation now.")
	case "endless":
		// Something that is not a mail service: a line that never ends.
		_, _ = conn.Write([]byte(strings.Repeat("x", 16384)))
		return
	case "plain":
		// A listener that does not offer TLS at all.
		say("220 mail.example.test ESMTP")
		if !expect("EHLO ") {
			return
		}
		say("250 mail.example.test")
		_ = expect("STARTTLS")
		say("502 5.5.1 Error: command not implemented")
		return
	}
	server := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	_ = server.Handshake()
	// Hold the connection until the client leaves.
	_, _ = server.Read(make([]byte, 1))
}

func wantNotServed(t *testing.T, err error, service string, unknown bool) *mailServedCertificateError {
	t.Helper()
	var notServed *mailServedCertificateError
	if !errors.As(err, &notServed) {
		t.Fatalf("err = %v, want a served-certificate answer for %s", err, service)
	}
	if notServed.service != service || notServed.unknown != unknown {
		t.Fatalf("answer = %+v, want %s unknown=%t", notServed, service, unknown)
	}
	return notServed
}

// The platform this exists for (Ubuntu 24.04): `systemctl reload
// postfix.service` exits 0 because that unit is a wrapper, and the daemon did
// not reload. Postfix's listeners still present the previous certificate, and
// that is the answer: not activated, verified.
func TestRenewalIsNotActivatedWhilePostfixStillPresentsThePreviousCertificate(t *testing.T) {
	host := installFakeMailListeners(t)
	previous, _ := testPanelCertificate(t, "mail.example.test")
	_, renewedLeaf := testPanelCertificate(t, "mail.example.test")
	host.certificates["127.0.0.1:465"] = previous
	host.certificates["127.0.0.1:587"] = previous
	host.protocols["127.0.0.1:587"] = "smtp"
	host.certificates["127.0.0.1:25"] = previous
	host.protocols["127.0.0.1:25"] = "smtp"

	err := verifyMailServiceServes(context.Background(), "postfix.service", panelCertificateLeafSHA256(renewedLeaf))
	wantNotServed(t, err, "Postfix", false)
	if host.served["127.0.0.1:465"] != mailServedRounds {
		t.Fatalf("the listener was asked %d times, want every round (%d)", host.served["127.0.0.1:465"], mailServedRounds)
	}
}

// A reload that reached the daemon: a process that was still serving a client
// presents the previous certificate for a moment, the next one the renewed.
func TestRenewalIsActivatedOnceAListenerPresentsTheSelectedCertificate(t *testing.T) {
	for _, unit := range []string{"postfix.service", "dovecot.service"} {
		t.Run(unit, func(t *testing.T) {
			host := installFakeMailListeners(t)
			previous, _ := testPanelCertificate(t, "mail.example.test")
			renewed, renewedLeaf := testPanelCertificate(t, "mail.example.test")
			address := "127.0.0.1:465"
			if unit == "dovecot.service" {
				address = "127.0.0.1:993"
			}
			host.certificates[address] = previous
			host.swapAfter[address] = 2
			host.next[address] = renewed

			if err := verifyMailServiceServes(context.Background(), unit, panelCertificateLeafSHA256(renewedLeaf)); err != nil {
				t.Fatalf("a listener presenting the selected certificate was not accepted: %v", err)
			}
			if host.served[address] != 3 {
				t.Fatalf("handshakes = %d, want 3 (two with the previous certificate, then the renewed one)", host.served[address])
			}
		})
	}
}

// `doveadm reload` only delivers a signal and exits 0. A Dovecot that could
// not read its configuration again keeps the previous certificate.
func TestRenewalIsNotActivatedWhileDovecotKeepsThePreviousCertificate(t *testing.T) {
	host := installFakeMailListeners(t)
	previous, _ := testPanelCertificate(t, "mail.example.test")
	_, renewedLeaf := testPanelCertificate(t, "mail.example.test")
	host.certificates["127.0.0.1:993"] = previous
	host.certificates["127.0.0.1:143"] = previous
	host.protocols["127.0.0.1:143"] = "imap"

	err := verifyMailServiceServes(context.Background(), "dovecot.service", panelCertificateLeafSHA256(renewedLeaf))
	wantNotServed(t, err, "Dovecot", false)
}

// The other half of the wrapper: `systemctl is-active postfix.service` says
// "active" while the master is stopped. No listener answers, so which
// certificate Postfix presents is unknown: not a success, and not a verified
// failure of the reload either. The check gives up after a few silent rounds.
func TestRenewalIsUnknownWhenNoListenerAnswers(t *testing.T) {
	host := installFakeMailListeners(t)
	mailServedHostAddresses = func() []string { return []string{"192.0.2.10"} }
	_, renewedLeaf := testPanelCertificate(t, "mail.example.test")

	err := verifyMailServiceServes(context.Background(), "postfix.service", panelCertificateLeafSHA256(renewedLeaf))
	wantNotServed(t, err, "Postfix", true)
	// Three ports on two loopback addresses and one address of the host.
	if want := mailServedSilentRounds * 9; len(host.dialed) != want {
		t.Fatalf("connections = %d, want %d: %v", len(host.dialed), want, host.dialed)
	}
	for _, address := range host.dialed {
		ip := net.ParseIP(strings.TrimSuffix(strings.TrimPrefix(address[:strings.LastIndex(address, ":")], "["), "]"))
		if ip == nil || (!ip.IsLoopback() && ip.String() != "192.0.2.10") {
			t.Fatalf("an address that is not this host's was contacted: %s", address)
		}
	}
}

// A listener that answers but offers no TLS has presented no certificate.
func TestRenewalIsUnknownWhenTheListenerOffersNoTLS(t *testing.T) {
	host := installFakeMailListeners(t)
	unused, _ := testPanelCertificate(t, "mail.example.test")
	_, renewedLeaf := testPanelCertificate(t, "mail.example.test")
	host.certificates["127.0.0.1:25"] = unused
	host.protocols["127.0.0.1:25"] = "plain"

	err := verifyMailServiceServes(context.Background(), "postfix.service", panelCertificateLeafSHA256(renewedLeaf))
	wantNotServed(t, err, "Postfix", true)

	// Neither has one that sends a line without an end: the exchange stops at
	// the size of one buffer instead of reading on.
	host.protocols["127.0.0.1:25"] = "endless"
	err = verifyMailServiceServes(context.Background(), "postfix.service", panelCertificateLeafSHA256(renewedLeaf))
	wantNotServed(t, err, "Postfix", true)
}

// The owner bound the service to one address of the host; nothing listens on
// loopback. That address is asked, and no other.
func TestRenewalAsksTheHostAddressWhenLoopbackIsSilent(t *testing.T) {
	host := installFakeMailListeners(t)
	mailServedHostAddresses = func() []string { return []string{"192.0.2.10"} }
	renewed, renewedLeaf := testPanelCertificate(t, "mail.example.test")
	host.certificates["192.0.2.10:995"] = renewed

	if err := verifyMailServiceServes(context.Background(), "dovecot.service", panelCertificateLeafSHA256(renewedLeaf)); err != nil {
		t.Fatalf("the listener on the host's own address was not accepted: %v", err)
	}
}

// Each way TLS begins on a mail port ends in the same handshake and yields the
// certificate presented.
func TestMailServedLeafSpeaksEachStartTLS(t *testing.T) {
	for _, endpoint := range []mailServedEndpoint{{465, "tls"}, {587, "smtp"}, {143, "imap"}, {110, "pop3"}} {
		t.Run(endpoint.start, func(t *testing.T) {
			host := installFakeMailListeners(t)
			certificate, leaf := testPanelCertificate(t, "mail.example.test")
			host.certificates["127.0.0.1:1"] = certificate
			if endpoint.start != "tls" {
				host.protocols["127.0.0.1:1"] = endpoint.start
			}
			got, err := mailServedLeaf(context.Background(), "127.0.0.1:1", endpoint)
			if err != nil || got != panelCertificateLeafSHA256(leaf) {
				t.Fatalf("leaf = %q, %v", got, err)
			}
		})
	}
}

// An operation that lost its lease answers with that, not with a statement
// about the certificate.
func TestMailServedCheckReturnsTheCancelledOperation(t *testing.T) {
	installFakeMailListeners(t)
	_, renewedLeaf := testPanelCertificate(t, "mail.example.test")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := verifyMailServiceServes(ctx, "postfix.service", panelCertificateLeafSHA256(renewedLeaf))
	var notServed *mailServedCertificateError
	if !errors.Is(err, context.Canceled) || errors.As(err, &notServed) {
		t.Fatalf("err = %v, want the cancelled operation", err)
	}
	if err := verifyMailServiceServes(context.Background(), "nginx.service", panelCertificateLeafSHA256(renewedLeaf)); err == nil {
		t.Fatal("a unit that is not a mail service was accepted")
	}
}

// Both sentences say why, who acts, with which command, and how the same
// operation resumes; neither carries output, an address or a fingerprint. The
// wrapper around the cause shows the same sentence.
func TestMailServedSentencesAreActionable(t *testing.T) {
	for _, answer := range []*mailServedCertificateError{
		{service: "Postfix"}, {service: "Postfix", unknown: true},
		{service: "Dovecot"}, {service: "Dovecot", unknown: true},
	} {
		text := answer.Error()
		for _, fragment := range []string{answer.service, "server owner", "retries the same operation", "stays selected", "preserved"} {
			if !strings.Contains(text, fragment) {
				t.Fatalf("%+v: sentence lacks %q: %s", answer, fragment, text)
			}
		}
		command := "postfix reload"
		if answer.service == "Dovecot" {
			command = "doveadm reload"
		}
		if !answer.unknown && !strings.Contains(text, "`"+command+"`") {
			t.Fatalf("%+v: sentence lacks the command: %s", answer, text)
		}
		if answer.unknown != strings.Contains(text, "not confirmed") || answer.unknown == strings.Contains(text, "not complete") {
			t.Fatalf("%+v: unknown and verified are not told apart: %s", answer, text)
		}
		if strings.Contains(text, "127.0.0.1") || strings.Contains(text, "sha256") {
			t.Fatalf("%+v: sentence carries an address or fingerprint: %s", answer, text)
		}
		if wrapped := (&mailHostReloadUnverified{cause: answer}).Error(); wrapped != text {
			t.Fatalf("the activation answer does not show the cause's sentence: %s", wrapped)
		}
	}
}

// The check opens connections; it adds no command. The independent helper's
// scope stays exactly what it was, and the daemon's own control commands stay
// outside it.
func TestServedCertificateCheckDoesNotWidenTheRenewalCommandScope(t *testing.T) {
	for _, args := range [][]string{
		{"/usr/sbin/postfix", "reload"}, {"/usr/sbin/postfix", "status"}, {"/usr/sbin/postfix", "check"},
		{"/usr/bin/doveadm", "reload"}, {"/usr/bin/systemctl", "show", "postfix.service"},
		{"/usr/bin/systemctl", "reload", "postfix@-.service"}, {"/usr/bin/systemctl", "is-active", "--quiet", "postfix@-.service"},
		{"/usr/bin/openssl", "s_client", "-connect", "127.0.0.1:465"},
	} {
		if err := validateIndependentMailCommand(args[0], args[1:]); err == nil {
			t.Fatalf("the renewal command scope accepts %v", args)
		}
	}
}
