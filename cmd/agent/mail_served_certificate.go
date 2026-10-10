package main

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Which certificate a running Postfix or Dovecot presents, asked of the daemon
// itself (10 Oct 2026; D-022, D-024, D-025 invariant 2, P0.4/P0.5).
//
// A renewal publishes the new host certificate and then reloads the two mail
// services with `systemctl reload`. That command's exit status is not evidence
// on every platform. Measured on Ubuntu 24.04 (set1, 2026-10-08):
// `postfix.service` is a oneshot wrapper (`ExecStart=/bin/true`,
// `ExecReload=/bin/true`, `RemainAfterExit=yes`) and the daemon belongs to
// `postfix@-.service`. So there `systemctl reload postfix.service` exits 0
// whatever the instance did, and `systemctl is-active postfix.service` answers
// "active" while the master is stopped. Debian 13 and Arch ship one real
// `postfix.service`. Dovecot is one real unit on all three, but its reload is
// `doveadm reload`, which only delivers a signal: a master that could not read
// its configuration again keeps the previous certificate and the command still
// exits 0.
//
// The renewal path may not answer this with more commands. The independent
// helper's command scope is a closed list (`validateIndependentMailCommand`):
// it must not run `postfix check`, which creates missing queue directories, and
// the helper an enrolled server already has is immutable. What the helper can
// always do is open a connection: after each reload it performs a TLS handshake
// with the service's own listeners on this host and compares the certificate
// presented with the one that is selected. That is the fact the renewal exists
// for, it needs no new command, and it is the same on every platform.
//
// Three answers, never two:
//
//   - a listener presents the selected certificate: verified;
//   - listeners answer and none presents it within the wait: a verified "not
//     activated";
//   - no listener of the service answers with TLS: unknown. It is not reported
//     as activated, and it is not reported as a failed reload either.
//
// The handshake sends no server name, so the daemon presents its default
// certificate (`smtpd_tls_cert_file`, Dovecot's global `ssl_cert`), which is
// the host certificate; a customer name in the SNI map cannot answer instead.
// Only addresses of this host are contacted and no name is resolved.
//
// Çalışan bir Postfix ya da Dovecot'un hangi sertifikayı sunduğu, hizmetin
// kendisine sorulur. Ubuntu 24.04'te `postfix.service` bir sarmalayıcıdır:
// `systemctl reload` her durumda 0 ile çıkar, `is-active` ana süreç durmuşken
// de "active" der. Yenileme yolu buna yeni komutla yanıt veremez (bağımsız
// yardımcının komut kapsamı kapalı bir listedir). Bu yüzden her yeniden
// yüklemeden sonra hizmetin bu sunucudaki kendi dinleyicileriyle TLS el
// sıkışması yapılır ve sunulan sertifika seçili olanla karşılaştırılır. Üç
// yanıt vardır: doğrulandı, doğrulanmış "etkin değil", bilinmiyor.

// mailServedEndpoint is one listener of a mail service: its port and how TLS
// begins on it.
type mailServedEndpoint struct {
	port int
	// "tls": TLS from the first byte. "smtp", "imap", "pop3": the protocol's
	// own STARTTLS command first.
	start string
}

// The listeners CelikPanel's mail stack opens (catalogue FirewallPorts), in the
// order they are asked. Implicit TLS first: it needs no protocol exchange.
var mailServedEndpoints = map[string][]mailServedEndpoint{
	"postfix.service": {{465, "tls"}, {587, "smtp"}, {25, "smtp"}},
	"dovecot.service": {{993, "tls"}, {995, "tls"}, {143, "imap"}, {110, "pop3"}},
}

var mailServedNames = map[string]string{"postfix.service": "Postfix", "dovecot.service": "Dovecot"}

// Swapped by tests.
// Testlerde değiştirilir.
var (
	mailServedDial = func(ctx context.Context, address string) (net.Conn, error) {
		dialer := net.Dialer{Timeout: mailServedDialTimeout}
		return dialer.DialContext(ctx, "tcp", address)
	}
	mailServedSleep = func(ctx context.Context, wait time.Duration) {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-ctx.Done():
		case <-timer.C:
		}
	}
	// mailServedHostAddresses lists the addresses of this host besides
	// loopback, for a service its owner bound to one address only.
	mailServedHostAddresses = func() []string {
		addresses, err := net.InterfaceAddrs()
		if err != nil {
			return nil
		}
		var out []string
		for _, address := range addresses {
			network, ok := address.(*net.IPNet)
			if !ok || network.IP.IsLoopback() || !network.IP.IsGlobalUnicast() {
				continue
			}
			out = append(out, network.IP.String())
		}
		sort.Strings(out)
		if len(out) > mailServedHostAddressLimit {
			out = out[:mailServedHostAddressLimit]
		}
		return out
	}
)

const (
	mailServedDialTimeout      = time.Second
	mailServedExchangeTimeout  = 3 * time.Second
	mailServedHostAddressLimit = 4
	// A reloaded Postfix lets a process that is serving a client finish; one
	// that waits for a client leaves at once. A few rounds cover the first.
	mailServedRounds   = 12
	mailServedInterval = 500 * time.Millisecond
	// One service's check never takes longer than this, whatever answers.
	mailServedServiceTimeout = 9 * time.Second
	// Rounds in which nothing answered at all before the answer is "unknown".
	mailServedSilentRounds = 3
)

// mailServedCertificateError says the selected certificate was not seen on a
// service's own listeners. unknown is true when no listener answered, so which
// certificate the service presents could not be established.
type mailServedCertificateError struct {
	service string
	unknown bool
}

// The sentence carries no native output, address or fingerprint: it is printed
// by the independent helper and logged by the Agent.
func (e *mailServedCertificateError) Error() string {
	// The daemon's own commands: on Ubuntu `systemctl is-active postfix` and
	// `systemctl reload postfix` answer for a wrapper unit.
	command, state := "postfix reload", "`postfix status`, which answers for the daemon where `systemctl is-active postfix` may answer for a wrapper unit"
	if e.service == "Dovecot" {
		command, state = "doveadm reload", "`systemctl status dovecot`"
	}
	const resumes = "and the renewal then retries the same operation, by itself up to its recorded limit and after that with the continuation command it prints; " +
		"the renewed certificate stays selected, and Postfix/Dovecot settings and certificate evidence are preserved"
	if e.unknown {
		return "mail certificate activation is not confirmed: no TLS listener of " + e.service +
			" answered on this server, so which certificate it presents is unknown and nothing is reported as activated; " +
			"the server owner checks that " + e.service + " is running and listening (" + state + "), starts it if it is stopped, " + resumes
	}
	return "mail certificate activation is not complete: " + e.service +
		" was asked to reload, but its listeners on this server still present another certificate than the selected one; " +
		"the server owner runs `" + command + "` as root, reads what it prints and the service's log, " + resumes
}

// mailServedLeaf performs one handshake and returns the SHA-256 of the
// certificate the listener presented.
func mailServedLeaf(ctx context.Context, address string, endpoint mailServedEndpoint) (string, error) {
	conn, err := mailServedDial(ctx, address)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(mailServedExchangeTimeout)
	if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
		deadline = limit
	}
	_ = conn.SetDeadline(deadline)
	if err := mailServedStartTLS(conn, endpoint.start); err != nil {
		return "", err
	}
	// The fingerprint comparison is the decision, exactly as for the Panel's
	// own certificate: chain and name are not what is being asked here.
	client := tls.Client(conn, &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12}) //nolint:gosec
	if err := client.HandshakeContext(ctx); err != nil {
		return "", err
	}
	certificates := client.ConnectionState().PeerCertificates
	if len(certificates) == 0 {
		return "", errors.New("the listener presented no certificate")
	}
	return panelCertificateLeafSHA256(certificates[0].Raw), nil
}

// mailServedStartTLS speaks only as much of a protocol as its STARTTLS needs.
func mailServedStartTLS(conn net.Conn, start string) error {
	if start == "tls" {
		return nil
	}
	reader := bufio.NewReaderSize(conn, 4096)
	line := func() (string, error) {
		// ReadSlice never holds more than the buffer: a line longer than that
		// is an error, not something to keep reading.
		text, err := reader.ReadSlice('\n')
		if err != nil {
			return "", err
		}
		return strings.TrimRight(string(text), "\r\n"), nil
	}
	// smtpReply reads one reply, which may span lines ("250-..." then "250 ...").
	smtpReply := func(code string) error {
		for count := 0; count < 64; count++ {
			text, err := line()
			if err != nil {
				return err
			}
			if !strings.HasPrefix(text, code) {
				return errors.New("the listener refused the exchange")
			}
			if len(text) == 3 || text[3] == ' ' {
				return nil
			}
		}
		return errors.New("the listener's reply did not end")
	}
	send := func(text string) error {
		_, err := conn.Write([]byte(text + "\r\n"))
		return err
	}
	var err error
	switch start {
	case "smtp":
		if err = smtpReply("220"); err == nil {
			if err = send("EHLO localhost"); err == nil {
				if err = smtpReply("250"); err == nil {
					if err = send("STARTTLS"); err == nil {
						err = smtpReply("220")
					}
				}
			}
		}
	case "imap":
		var text string
		if text, err = line(); err == nil {
			if !strings.HasPrefix(text, "* OK") {
				return errors.New("the listener refused the exchange")
			}
			if err = send("a STARTTLS"); err == nil {
				// Untagged lines may come first; the tagged one is the answer.
				err = errors.New("the listener did not answer STARTTLS")
				for count := 0; count < 16; count++ {
					var readErr error
					if text, readErr = line(); readErr != nil {
						err = readErr
						break
					}
					if strings.HasPrefix(text, "a ") {
						err = nil
						if !strings.HasPrefix(text, "a OK") {
							err = errors.New("the listener refused STARTTLS")
						}
						break
					}
				}
			}
		}
	case "pop3":
		var text string
		if text, err = line(); err == nil {
			if !strings.HasPrefix(text, "+OK") {
				return errors.New("the listener refused the exchange")
			}
			if err = send("STLS"); err == nil {
				if text, err = line(); err == nil && !strings.HasPrefix(text, "+OK") {
					err = errors.New("the listener refused STLS")
				}
			}
		}
	default:
		return errors.New("unknown listener protocol")
	}
	if err != nil {
		return err
	}
	if reader.Buffered() != 0 {
		// Bytes after the go-ahead would be read as part of the handshake.
		return errors.New("the listener sent data before the handshake")
	}
	return nil
}

// verifyMailServiceServes waits, bounded, until one of the service's own
// listeners on this host presents the certificate whose SHA-256 is expected.
// nil: seen. *mailServedCertificateError: not seen, or unknown.
func verifyMailServiceServes(ctx context.Context, unit, expected string) error {
	endpoints, known := mailServedEndpoints[unit]
	if !known || len(expected) != 64 {
		return errors.New("mail certificate check requires a known mail service and a selected certificate")
	}
	if ctx == nil {
		return errors.New("mail certificate check requires an operation context")
	}
	operation := ctx
	ctx, cancel := context.WithTimeout(operation, mailServedServiceTimeout)
	defer cancel()

	// round asks every listener at the given addresses. answered: at least one
	// completed a handshake.
	round := func(addresses []string) (seen, answered bool) {
		for _, address := range addresses {
			for _, endpoint := range endpoints {
				if ctx.Err() != nil {
					return false, answered
				}
				leaf, err := mailServedLeaf(ctx, net.JoinHostPort(address, strconv.Itoa(endpoint.port)), endpoint)
				if err != nil {
					continue
				}
				if leaf == expected {
					return true, true
				}
				answered = true
			}
		}
		return false, answered
	}

	everAnswered, silent := false, 0
	for attempt := 0; attempt < mailServedRounds; attempt++ {
		if attempt > 0 {
			mailServedSleep(ctx, mailServedInterval)
		}
		seen, answered := round([]string{"127.0.0.1", "::1"})
		if !seen && !answered {
			// Nothing on loopback: the owner may have bound the service to one
			// of this host's other addresses.
			seen, answered = round(mailServedHostAddresses())
		}
		if seen {
			return nil
		}
		if answered {
			everAnswered, silent = true, 0
		} else {
			silent++
		}
		if ctx.Err() != nil || (!everAnswered && silent >= mailServedSilentRounds) {
			break
		}
	}
	// The operation itself was cancelled (a lost lease): that is its answer.
	if err := operation.Err(); err != nil {
		return err
	}
	// A wait that ran out says nothing about the certificate unless a listener
	// was heard presenting another one.
	return &mailServedCertificateError{service: mailServedNames[unit], unknown: !everAnswered}
}
