package main

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	panelCertificateActivationStateVersion = 1
	panelCertificateActivationStateName    = "panel-certificate-activation.json"
	panelCertificateActivationStatePath    = "/var/lib/celikpanel-agent-private/" + panelCertificateActivationStateName
	panelCertificateActivationStateMaxSize = 4 * 1024
	panelCertificateActivationErrorMaxSize = 1024
	panelCertificateActivationMaxAttempts  = 1_000_000

	panelCertificateActivationInitialBackoff = 15 * time.Second
	panelCertificateActivationMaximumBackoff = 5 * time.Minute
	panelCertificateActivationVerifyTimeout  = 10 * time.Second
)

type panelCertificateActivationPhase string

const (
	panelCertificateActivationPendingSource  panelCertificateActivationPhase = "pending_source"
	panelCertificateActivationPendingPublish panelCertificateActivationPhase = "pending_publish"
	panelCertificateActivationPendingRestart panelCertificateActivationPhase = "pending_restart"
	panelCertificateActivationPendingVerify  panelCertificateActivationPhase = "pending_verify"
)

var errPanelCertificateActivationUnsupported = errors.New(
	"durable panel certificate activation is unsupported on this platform",
)

var errPanelCertificateActivationPending = errors.New(
	"panel certificate activation is already pending",
)

// panelCertificateActivationState is the durable activation intent. Phase is
// advisory for recovery: callers must still compare source, published, and
// served fingerprints before deciding which idempotent step is next.
//
// panelCertificateActivationState kalıcı etkinleştirme niyetidir. Phase,
// kurtarma için yalnız bir ipucudur: çağıranlar sonraki idempotent adıma karar
// vermeden önce kaynak, yayımlanmış ve sunulan parmak izlerini karşılaştırır.
type panelCertificateActivationState struct {
	Version       int                             `json:"version"`
	Domain        string                          `json:"domain"`
	LineageName   string                          `json:"lineage_name"`
	LeafSHA256    string                          `json:"leaf_sha256,omitempty"`
	NotAfter      *time.Time                      `json:"not_after,omitempty"`
	Phase         panelCertificateActivationPhase `json:"phase"`
	Attempts      uint32                          `json:"attempts"`
	LastAttemptAt *time.Time                      `json:"last_attempt_at,omitempty"`
	LastError     string                          `json:"last_error,omitempty"`
}

type panelCertificateServedVerifier func(
	context.Context,
	string,
	string,
	string,
) error

// This seam lets reconciliation tests prove that state is retained until the
// running listener serves the expected leaf. Production uses the pinned
// loopback verifier below.
var panelCertificateActivationVerifyServed panelCertificateServedVerifier = verifyServedPanelCertificate

var (
	panelCertificateActivationDialContext = func(
		ctx context.Context,
		network, address string,
	) (net.Conn, error) {
		var dialer net.Dialer
		return dialer.DialContext(ctx, network, address)
	}
	panelCertificateActivationNow = time.Now
)

func newPanelCertificateActivationState(
	domain string,
) (panelCertificateActivationState, error) {
	domain = strings.ToLower(strings.TrimSpace(domain))
	state := panelCertificateActivationState{
		Version:     panelCertificateActivationStateVersion,
		Domain:      domain,
		LineageName: panelCertLineageName(domain),
		Phase:       panelCertificateActivationPendingSource,
	}
	if err := validatePanelCertificateActivationState(state); err != nil {
		return panelCertificateActivationState{}, err
	}
	return state, nil
}

func bindPanelCertificateActivationMaterial(
	state panelCertificateActivationState,
	leafDER []byte,
	notAfter time.Time,
) (panelCertificateActivationState, error) {
	if err := validatePanelCertificateActivationState(state); err != nil {
		return panelCertificateActivationState{}, err
	}
	if len(leafDER) == 0 {
		return panelCertificateActivationState{}, errors.New(
			"panel certificate leaf DER is required",
		)
	}
	normalizedExpiry, err := normalizePanelCertificateActivationTime(notAfter)
	if err != nil {
		return panelCertificateActivationState{}, fmt.Errorf(
			"normalize panel certificate expiry: %w", err,
		)
	}
	state.LeafSHA256 = panelCertificateLeafSHA256(leafDER)
	state.NotAfter = &normalizedExpiry
	state.Phase = panelCertificateActivationPendingPublish
	state.Attempts = 0
	state.LastAttemptAt = nil
	state.LastError = ""
	if err := validatePanelCertificateActivationState(state); err != nil {
		return panelCertificateActivationState{}, err
	}
	return state, nil
}

func panelCertificateActivationWithPhase(
	state panelCertificateActivationState,
	phase panelCertificateActivationPhase,
) (panelCertificateActivationState, error) {
	state.Phase = phase
	if err := validatePanelCertificateActivationState(state); err != nil {
		return panelCertificateActivationState{}, err
	}
	return state, nil
}

func panelCertificateActivationFailure(
	state panelCertificateActivationState,
	now time.Time,
	failure error,
) (panelCertificateActivationState, error) {
	if err := validatePanelCertificateActivationState(state); err != nil {
		return panelCertificateActivationState{}, err
	}
	normalizedNow, err := normalizePanelCertificateActivationTime(now)
	if err != nil {
		return panelCertificateActivationState{}, fmt.Errorf(
			"normalize panel certificate activation attempt time: %w", err,
		)
	}
	if state.Attempts < panelCertificateActivationMaxAttempts {
		state.Attempts++
	}
	state.LastAttemptAt = &normalizedNow
	state.LastError = sanitizePanelCertificateActivationError(failure)
	if err := validatePanelCertificateActivationState(state); err != nil {
		return panelCertificateActivationState{}, err
	}
	return state, nil
}

func panelCertificateActivationBackoff(attempts uint32) time.Duration {
	if attempts == 0 {
		return 0
	}
	delay := panelCertificateActivationInitialBackoff
	for remaining := attempts - 1; remaining > 0; remaining-- {
		if delay >= panelCertificateActivationMaximumBackoff/2 {
			return panelCertificateActivationMaximumBackoff
		}
		delay *= 2
	}
	if delay > panelCertificateActivationMaximumBackoff {
		return panelCertificateActivationMaximumBackoff
	}
	return delay
}

func panelCertificateActivationRetryAt(
	state panelCertificateActivationState,
) time.Time {
	if state.LastAttemptAt == nil {
		return time.Time{}
	}
	return state.LastAttemptAt.Add(panelCertificateActivationBackoff(state.Attempts))
}

func panelCertificateActivationRetryReady(
	state panelCertificateActivationState,
	now time.Time,
) bool {
	retryAt := panelCertificateActivationRetryAt(state)
	return retryAt.IsZero() || !now.Before(retryAt)
}

func panelCertificateLeafSHA256(leafDER []byte) string {
	sum := sha256.Sum256(leafDER)
	return hex.EncodeToString(sum[:])
}

func validatePanelCertificateActivationState(
	state panelCertificateActivationState,
) error {
	if state.Version != panelCertificateActivationStateVersion {
		return fmt.Errorf(
			"unsupported panel certificate activation state version %d",
			state.Version,
		)
	}
	if len(state.Domain) == 0 || len(state.Domain) > 253 ||
		state.Domain != strings.ToLower(state.Domain) ||
		state.Domain != strings.TrimSpace(state.Domain) ||
		!validPanelCertDomain.MatchString(state.Domain) {
		return errors.New("invalid panel certificate activation domain")
	}
	if state.LineageName != panelCertLineageName(state.Domain) {
		return errors.New("panel certificate activation lineage does not match domain")
	}
	if state.Attempts > panelCertificateActivationMaxAttempts {
		return errors.New("panel certificate activation attempt count exceeds limit")
	}
	if state.LastAttemptAt != nil {
		if state.Attempts == 0 {
			return errors.New("panel certificate activation attempt time has no attempt")
		}
		if err := validatePanelCertificateActivationTime(*state.LastAttemptAt); err != nil {
			return fmt.Errorf("invalid panel certificate activation attempt time: %w", err)
		}
	}
	if state.LastError != "" {
		if state.Attempts == 0 {
			return errors.New("panel certificate activation error has no attempt")
		}
		if err := validatePanelCertificateActivationError(state.LastError); err != nil {
			return err
		}
	}

	switch state.Phase {
	case panelCertificateActivationPendingSource:
		if state.LeafSHA256 != "" || state.NotAfter != nil {
			return errors.New("pending source state must not bind certificate material")
		}
	case panelCertificateActivationPendingPublish,
		panelCertificateActivationPendingRestart,
		panelCertificateActivationPendingVerify:
		if err := validatePanelCertificateLeafSHA256(state.LeafSHA256); err != nil {
			return err
		}
		if state.NotAfter == nil {
			return errors.New("panel certificate activation expiry is required")
		}
		if err := validatePanelCertificateActivationTime(*state.NotAfter); err != nil {
			return fmt.Errorf("invalid panel certificate activation expiry: %w", err)
		}
	default:
		return fmt.Errorf("invalid panel certificate activation phase %q", state.Phase)
	}
	return nil
}

func canonicalPanelCertificateActivationState(
	state panelCertificateActivationState,
) ([]byte, error) {
	if err := validatePanelCertificateActivationState(state); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("encode panel certificate activation state: %w", err)
	}
	raw = append(raw, '\n')
	if len(raw) > panelCertificateActivationStateMaxSize {
		return nil, fmt.Errorf(
			"panel certificate activation state exceeds %d bytes",
			panelCertificateActivationStateMaxSize,
		)
	}
	return raw, nil
}

func decodePanelCertificateActivationState(
	raw []byte,
) (panelCertificateActivationState, error) {
	if len(raw) == 0 || len(raw) > panelCertificateActivationStateMaxSize {
		return panelCertificateActivationState{}, errors.New(
			"panel certificate activation state has invalid size",
		)
	}
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	var state panelCertificateActivationState
	if err := decoder.Decode(&state); err != nil {
		return panelCertificateActivationState{}, fmt.Errorf(
			"decode panel certificate activation state: %w", err,
		)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			err = errors.New("multiple JSON values")
		}
		return panelCertificateActivationState{}, fmt.Errorf(
			"decode panel certificate activation state trailer: %w", err,
		)
	}
	canonical, err := canonicalPanelCertificateActivationState(state)
	if err != nil {
		return panelCertificateActivationState{}, err
	}
	if subtle.ConstantTimeCompare(raw, canonical) != 1 {
		return panelCertificateActivationState{}, errors.New(
			"panel certificate activation state is not canonical JSON",
		)
	}
	return state, nil
}

func verifyServedPanelCertificate(
	ctx context.Context,
	address, serverName, expectedLeafSHA256 string,
) error {
	serverName = strings.ToLower(strings.TrimSpace(serverName))
	if len(serverName) == 0 || len(serverName) > 253 ||
		!validPanelCertDomain.MatchString(serverName) {
		return errors.New("invalid panel certificate verification server name")
	}
	expected, err := decodePanelCertificateLeafSHA256(expectedLeafSHA256)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("invalid panel certificate verification address: %w", err)
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	if ip == nil || !ip.IsLoopback() {
		return errors.New("panel certificate verification address must be loopback")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, panelCertificateActivationVerifyTimeout)
		defer cancel()
	}
	rawConn, err := panelCertificateActivationDialContext(ctx, "tcp", address)
	if err != nil {
		return fmt.Errorf("connect to panel TLS listener: %w", err)
	}
	defer rawConn.Close()
	// The expected leaf fingerprint is the trust decision. Chain verification is
	// deliberately skipped only for this loopback probe; hostname, validity, and
	// the exact pinned leaf are checked below.
	tlsConn := tls.Client(rawConn, &tls.Config{ //nolint:gosec
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
		ServerName:         serverName,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		return fmt.Errorf("handshake with panel TLS listener: %w", err)
	}
	peerCertificates := tlsConn.ConnectionState().PeerCertificates
	if len(peerCertificates) == 0 {
		return errors.New("panel TLS listener did not present a certificate")
	}
	leaf := peerCertificates[0]
	if err := leaf.VerifyHostname(serverName); err != nil {
		return fmt.Errorf("panel TLS listener certificate hostname: %w", err)
	}
	now := panelCertificateActivationNow()
	if now.Before(leaf.NotBefore) || !now.Before(leaf.NotAfter) {
		return errors.New("panel TLS listener certificate is not currently valid")
	}
	actual := sha256.Sum256(leaf.Raw)
	if subtle.ConstantTimeCompare(actual[:], expected) != 1 {
		return errors.New("panel TLS listener still serves a different certificate")
	}
	return nil
}

func validatePanelCertificateLeafSHA256(value string) error {
	_, err := decodePanelCertificateLeafSHA256(value)
	return err
}

func decodePanelCertificateLeafSHA256(value string) ([]byte, error) {
	if len(value) != sha256.Size*2 || value != strings.ToLower(value) {
		return nil, errors.New("invalid panel certificate leaf SHA-256")
	}
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return nil, errors.New("invalid panel certificate leaf SHA-256")
	}
	return decoded, nil
}

func normalizePanelCertificateActivationTime(value time.Time) (time.Time, error) {
	if value.IsZero() {
		return time.Time{}, errors.New("time is required")
	}
	return value.UTC().Truncate(time.Second), nil
}

func validatePanelCertificateActivationTime(value time.Time) error {
	if value.IsZero() || value.Location() != time.UTC || value.Nanosecond() != 0 {
		return errors.New("time must be non-zero UTC with one-second precision")
	}
	return nil
}

func sanitizePanelCertificateActivationError(failure error) string {
	detail := "panel certificate activation failed"
	if failure != nil {
		detail = failure.Error()
	}
	detail = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return ' '
		}
		return r
	}, detail)
	detail = strings.Join(strings.Fields(detail), " ")
	if detail == "" {
		detail = "panel certificate activation failed"
	}
	for len(detail) > panelCertificateActivationErrorMaxSize {
		_, size := utf8.DecodeLastRuneInString(detail)
		detail = detail[:len(detail)-size]
	}
	return detail
}

func validatePanelCertificateActivationError(value string) error {
	if !utf8.ValidString(value) || len(value) > panelCertificateActivationErrorMaxSize ||
		value != strings.TrimSpace(value) {
		return errors.New("invalid panel certificate activation error detail")
	}
	for _, r := range value {
		if r < 0x20 || r == 0x7f {
			return errors.New("invalid panel certificate activation error detail")
		}
	}
	return nil
}
