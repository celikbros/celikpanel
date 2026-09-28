//go:build linux

// Package dnspeertransport obtains one bounded, authenticated, read-only BIND
// observation over an owner-enrolled SSH channel. It grants no mutation rights.
package dnspeertransport

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

const (
	FixedCommand         = "celikpanel-bind-peer-inspect-v1"
	FixedPowerDNSCommand = "celikpanel-pdns-peer-inspect-v1"
	maxWireSize          = 4096
	maxStderr            = 1024
	maxDuration          = 10 * time.Second
)

var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

// Enrollment is reviewed owner configuration, never populated from a request.
// The server must enroll a dedicated non-root account with an OpenSSH forced
// command, prohibit PTY/forwarding, and permit only this inspector. The client
// cannot attest server-side authorized_keys restrictions by itself.
type Enrollment struct {
	PeerIP         string
	Username       string
	HostKeySHA256  string // lowercase hex SHA-256 of ssh.PublicKey.Marshal()
	PrivateKeyPath string // root-owned regular file, mode 0400 or 0600
	Timeout        time.Duration
}

type Code string

const (
	CodeEnrollment  Code = "peer_enrollment_invalid"
	CodeUnavailable Code = "peer_inspection_unavailable"
	CodeMalformed   Code = "peer_inspection_malformed"
)

// Unknown never asserts absence, failure of the native service, or deletion.
// Details from SSH, including paths and stderr, are deliberately not returned.
type Unknown struct{ Code Code }

func (e Unknown) Error() string { return string(e.Code) }

func IsCode(err error, code Code) bool {
	var u Unknown
	return errors.As(err, &u) && u.Code == code
}

// Validate rejects an unreviewed SSH identity or credential location.
func (e Enrollment) Validate() error { return e.validate() }

func (e Enrollment) validate() error {
	ip := net.ParseIP(e.PeerIP)
	if ip == nil || ip.To4() == nil || ip.String() != e.PeerIP || !ip.IsGlobalUnicast() ||
		!usernamePattern.MatchString(e.Username) || e.Username == "root" ||
		len(e.HostKeySHA256) != 64 || !filepath.IsAbs(e.PrivateKeyPath) ||
		e.Timeout <= 0 || e.Timeout > maxDuration {
		return Unknown{CodeEnrollment}
	}
	b, err := hex.DecodeString(e.HostKeySHA256)
	if err != nil || len(b) != 32 || hex.EncodeToString(b) != e.HostKeySHA256 {
		return Unknown{CodeEnrollment}
	}
	return nil
}

// Exchanger makes the authenticated transport replaceable by a fixture. A
// production implementation must derive Authentication from its SSH handshake.
type Exchanger interface {
	Exchange(context.Context, Enrollment, []byte) ([]byte, dnspeerproof.PeerAuthentication, error)
}

// Inspect validates the exact request/enrollment binding, exchanges one proof
// document and rejects malformed output. A caller must still call
// dnspeerproof.Verify with a durable consume-once callback and current operation
// rechecks before treating the response as terminal evidence.
func Inspect(ctx context.Context, enrollment Enrollment, request dnspeerproof.RequestV1, exchanger Exchanger) (dnspeerproof.ResponseV1, dnspeerproof.PeerAuthentication, error) {
	if enrollment.validate() != nil || exchanger == nil || request.Validate() != nil ||
		request.PeerIP != enrollment.PeerIP || request.PeerIdentitySHA256 != enrollment.HostKeySHA256 {
		return dnspeerproof.ResponseV1{}, dnspeerproof.PeerAuthentication{}, Unknown{CodeEnrollment}
	}
	raw, err := dnspeerproof.EncodeRequest(request)
	if err != nil || len(raw) > maxWireSize {
		return dnspeerproof.ResponseV1{}, dnspeerproof.PeerAuthentication{}, Unknown{CodeEnrollment}
	}
	deadline := enrollment.Timeout
	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()
	answer, auth, err := exchanger.Exchange(ctx, enrollment, raw)
	if err != nil || !auth.Established || auth.PeerIP != enrollment.PeerIP || auth.IdentitySHA256 != enrollment.HostKeySHA256 {
		return dnspeerproof.ResponseV1{}, dnspeerproof.PeerAuthentication{}, Unknown{CodeUnavailable}
	}
	if len(answer) == 0 || len(answer) > maxWireSize+1 {
		return dnspeerproof.ResponseV1{}, dnspeerproof.PeerAuthentication{}, Unknown{CodeMalformed}
	}
	if answer[len(answer)-1] == '\n' {
		answer = answer[:len(answer)-1]
	}
	if len(answer) == 0 || len(answer) > maxWireSize {
		return dnspeerproof.ResponseV1{}, dnspeerproof.PeerAuthentication{}, Unknown{CodeMalformed}
	}
	response, err := dnspeerproof.DecodeResponse(answer)
	if err != nil {
		return dnspeerproof.ResponseV1{}, dnspeerproof.PeerAuthentication{}, Unknown{CodeMalformed}
	}
	return response, auth, nil
}

// SSH is the production Exchanger. It never invokes a shell locally, opens a
// PTY, requests forwarding, uses agent/password auth or accepts a new host key.
type SSH struct{}

func (SSH) Exchange(ctx context.Context, enrollment Enrollment, request []byte) ([]byte, dnspeerproof.PeerAuthentication, error) {
	return exchangeFixed(ctx, enrollment, request, FixedCommand)
}

// PowerDNSSSH uses a separate owner-enrolled forced command. The command is
// selected by the type, never by the peer request or an untrusted response.
type PowerDNSSSH struct{}

func (PowerDNSSSH) Exchange(ctx context.Context, enrollment Enrollment, request []byte) ([]byte, dnspeerproof.PeerAuthentication, error) {
	return exchangeFixed(ctx, enrollment, request, FixedPowerDNSCommand)
}

func exchangeFixed(ctx context.Context, enrollment Enrollment, request []byte, command string) ([]byte, dnspeerproof.PeerAuthentication, error) {
	if enrollment.validate() != nil || len(request) == 0 || len(request) > maxWireSize {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeEnrollment}
	}
	key, err := readOwnerKey(enrollment.PrivateKeyPath)
	if err != nil {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeEnrollment}
	}
	signer, err := ssh.ParsePrivateKey(key)
	for i := range key {
		key[i] = 0
	}
	if err != nil {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeEnrollment}
	}
	var matched bool
	config := &ssh.ClientConfig{
		User: enrollment.Username,
		// V1 enrollment pins an Ed25519 host key digest. Never let the SSH
		// default preference select another key from the same server.
		HostKeyAlgorithms: []string{ssh.KeyAlgoED25519},
		Auth:              []ssh.AuthMethod{ssh.PublicKeys(signer)},
		HostKeyCallback: func(_ string, _ net.Addr, actual ssh.PublicKey) error {
			sum := sha256.Sum256(actual.Marshal())
			matched = hex.EncodeToString(sum[:]) == enrollment.HostKeySHA256
			if !matched {
				return errors.New("pinned SSH host identity mismatch")
			}
			return nil
		},
		Timeout: enrollment.Timeout,
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort(enrollment.PeerIP, "22"))
	if err != nil {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeUnavailable}
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(enrollment.Timeout)); err != nil {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeUnavailable}
	}
	closed := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = conn.Close()
		case <-closed:
		}
	}()
	defer close(closed)
	clientConn, chans, reqs, err := ssh.NewClientConn(conn, net.JoinHostPort(enrollment.PeerIP, "22"), config)
	if err != nil || !matched {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeUnavailable}
	}
	client := ssh.NewClient(clientConn, chans, reqs)
	defer client.Close()
	session, err := client.NewSession()
	if err != nil {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeUnavailable}
	}
	defer session.Close()
	var stdout limitedBuffer
	stdout.limit = maxWireSize + 1
	var stderr limitedBuffer
	stderr.limit = maxStderr
	session.Stdin = bytes.NewReader(request)
	session.Stdout = &stdout
	session.Stderr = &stderr
	if err := session.Run(command); err != nil || stdout.overflow || stderr.overflow {
		return nil, dnspeerproof.PeerAuthentication{}, Unknown{CodeUnavailable}
	}
	auth := dnspeerproof.PeerAuthentication{Established: true, IdentitySHA256: enrollment.HostKeySHA256, PeerIP: enrollment.PeerIP}
	return stdout.Bytes(), auth, nil
}

type limitedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if len(p) > b.limit-b.Len() {
		b.overflow = true
		return 0, io.ErrShortWrite
	}
	return b.Buffer.Write(p)
}

func readOwnerKey(path string) ([]byte, error) {
	if filepath.Clean(path) != path || !filepath.IsAbs(path) {
		return nil, fmt.Errorf("unsafe enrolled SSH credential")
	}
	for parent := filepath.Dir(path); parent != "/"; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
			return nil, fmt.Errorf("unsafe enrolled SSH credential")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return nil, fmt.Errorf("unsafe enrolled SSH credential")
		}
	}
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	f := os.NewFile(uintptr(fd), path)
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > 16*1024 ||
		info.Mode().Perm()&0077 != 0 || info.Mode().Perm()&0111 != 0 {
		return nil, fmt.Errorf("unsafe enrolled SSH credential")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 || stat.Nlink != 1 {
		return nil, fmt.Errorf("unsafe enrolled SSH credential")
	}
	return io.ReadAll(io.LimitReader(f, 16*1024+1))
}
