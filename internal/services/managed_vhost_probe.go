package services

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

// Whether a kept file lets a certificate be validated is measured, not read
// from the file (D-031 step 1b, second round, 2026-10-10; D-025 invariant 2).
//
// The ACME HTTP-01 location (`location ^~ /.well-known/acme-challenge/` with
// `root <challenge root>` and `try_files $uri =404`, in the Panel's own
// include directory) serves whatever file is in
// <challenge root>/.well-known/acme-challenge/ at the moment of the request:
// nginx looks the file up per request, so a new file there needs no reload.
// The probe puts one file with a random name and a random body there, asks
// nginx on this server (127.0.0.1, port 80) for it under each validation name
// as the Host, the way the certificate authority will, and removes it. A name
// is ready when nginx answers 200 with exactly that body; a redirect is
// followed as the certificate authority follows it, but only to the same name.
// No answer at all on port 80 (nginx not running, connection refused, time
// limit) is "unknown", never "not ready".
//
// Kesinleşmiş hazırlık dosyadan okunmaz, ölçülür: rastgele adlı ve içerikli
// bir dosya doğrulama köküne konur, nginx'e her doğrulama adıyla sorulur ve
// dosya kaldırılır. nginx dosyayı her istekte aradığı için yeniden yükleme
// gerekmez. Hiç yanıt yoksa sonuç "bilinmiyor"dur, "hazır değil" değil.

// ValidationProbe names what to ask: SiteNames are the site's own names (the
// file's server blocks must answer them), ExtraNames the validation-only names
// such as mail.<domain> (CelikPanel's validation-only block answers them).
type ValidationProbe struct {
	SiteNames  []string
	ExtraNames []string
}

func (p *ValidationProbe) empty() bool {
	return p == nil || len(p.SiteNames)+len(p.ExtraNames) == 0
}

// ValidationProbeAnswer is what nginx answered for one name. Answered is
// false when no HTTP answer came at all on port 80.
type ValidationProbeAnswer struct {
	Answered bool
	Status   int
	Body     []byte
	Err      error
}

const (
	validationProbePrefix      = "celikpanel-probe-"
	validationProbeTimeout     = 5 * time.Second
	validationProbeMaxBody     = 4 << 10
	validationProbeMaxRedirect = 3
	acmeChallengeURLPath       = "/.well-known/acme-challenge/"
)

// ValidationProbeChallengeDir is where the probe file goes: the directory the
// challenge location serves.
func ValidationProbeChallengeDir(challengeRoot string) string {
	return filepath.Join(challengeRoot, ".well-known", "acme-challenge")
}

// localValidationProbeGet asks nginx on this server. Every connection goes to
// 127.0.0.1 on the URL's port; the Host header and the TLS server name are the
// name asked for. The certificate of a redirect target is not checked: the
// body is a random value only this probe knows, and nothing secret is sent.
func localValidationProbeGet(ctx context.Context, name, path string) ValidationProbeAnswer {
	dialer := &net.Dialer{Timeout: validationProbeTimeout}
	lastStatus := 0
	transportImpl := &http.Transport{
		Proxy: nil,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			_, port, err := net.SplitHostPort(address)
			if err != nil {
				return nil, err
			}
			return dialer.DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", port))
		},
		// #nosec G402 -- a local probe of a random body; see above.
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: true, MinVersion: tls.VersionTLS12},
		DisableKeepAlives:     true,
		ResponseHeaderTimeout: validationProbeTimeout,
	}
	defer transportImpl.CloseIdleConnections()
	client := &http.Client{
		Transport: transportImpl,
		Timeout:   validationProbeTimeout * 2,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if req.Response != nil {
				lastStatus = req.Response.StatusCode
			}
			// Only to the same name (on 80, 443 or the port first asked):
			// another name would be another site's answer.
			first := via[0].URL
			port := req.URL.Port()
			if len(via) > validationProbeMaxRedirect || !strings.EqualFold(req.URL.Hostname(), first.Hostname()) ||
				(port != "" && port != "80" && port != "443" && port != first.Port()) {
				return http.ErrUseLastResponse
			}
			return nil
		},
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+name+path, nil)
	if err != nil {
		return ValidationProbeAnswer{Err: err}
	}
	response, err := client.Do(request)
	if err != nil {
		// A redirect was answered, then its target failed: nginx answered,
		// the name is not served the probe.
		return ValidationProbeAnswer{Answered: lastStatus != 0, Status: lastStatus, Err: err}
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(response.Body, validationProbeMaxBody))
	return ValidationProbeAnswer{Answered: true, Status: response.StatusCode, Body: body}
}

func (ng *NginxGenerator) validationProbeGet(ctx context.Context, name, path string) ValidationProbeAnswer {
	if ng.probeGet != nil {
		return ng.probeGet(ctx, name, path)
	}
	return localValidationProbeGet(ctx, name, path)
}

// validationProbeResult is the measured validation of one kept file.
type validationProbeResult struct {
	validation string
	name       string
	status     int
	detail     string
}

func (r validationProbeResult) apply(result *transport.SiteFileResult) {
	result.Validation = r.validation
	result.ValidationName = r.name
	result.ValidationStatus = r.status
	if r.detail != "" {
		result.ValidationDetail = boundedSiteFileDetail(r.detail)
	}
}

// probeValidation writes the probe file, asks nginx for it under every name
// and removes it. The caller holds nginxMutationMu.
func (ng *NginxGenerator) probeValidation(challengeRoot string, probe *ValidationProbe) validationProbeResult {
	if challengeRoot == "" || probe.empty() {
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: "no challenge root or no names to probe"}
	}
	directory := ValidationProbeChallengeDir(challengeRoot)
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() {
		detail := "the challenge directory is not a directory"
		if err != nil {
			detail = "the challenge directory: " + err.Error()
		}
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: detail}
	}
	random := make([]byte, 16)
	token := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: "probe name: " + err.Error()}
	}
	if _, err := rand.Read(token); err != nil {
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: "probe body: " + err.Error()}
	}
	fileName := validationProbePrefix + hex.EncodeToString(random)
	body := []byte("celikpanel validation probe " + hex.EncodeToString(token) + "\n")
	path := filepath.Join(directory, fileName)
	// O_EXCL also refuses a link already at that name.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: "write the probe file: " + err.Error()}
	}
	defer func() { _ = os.Remove(path) }()
	_, writeErr := file.Write(body)
	closeErr := file.Close()
	if err := errors.Join(writeErr, closeErr); err != nil {
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: "write the probe file: " + err.Error()}
	}
	if err := os.Chmod(path, 0o644); err != nil {
		return validationProbeResult{validation: transport.SiteFileValidationUnknown, detail: "the probe file's mode: " + err.Error()}
	}

	urlPath := acmeChallengeURLPath + fileName
	var firstSite, firstExtra *validationProbeResult
	ask := func(name string) (validationProbeResult, bool, bool) {
		ctx, cancel := context.WithTimeout(context.Background(), validationProbeTimeout*2)
		defer cancel()
		answer := ng.validationProbeGet(ctx, name, urlPath)
		if !answer.Answered {
			detail := "nginx did not answer on port 80"
			if answer.Err != nil {
				detail += ": " + answer.Err.Error()
			}
			return validationProbeResult{validation: transport.SiteFileValidationUnknown, name: name, detail: detail}, false, false
		}
		served := answer.Status == http.StatusOK && bytes.Equal(answer.Body, body)
		return validationProbeResult{name: name, status: answer.Status}, served, true
	}
	for _, name := range probe.SiteNames {
		result, served, answered := ask(name)
		if !answered {
			return result
		}
		if !served && firstSite == nil {
			result.validation = transport.SiteFileValidationIncludeMissing
			firstSite = &result
		}
	}
	for _, name := range probe.ExtraNames {
		result, served, answered := ask(name)
		if !answered {
			return result
		}
		if !served && firstExtra == nil {
			result.validation = transport.SiteFileValidationNamesMissing
			firstExtra = &result
		}
	}
	switch {
	case firstSite != nil:
		return *firstSite
	case firstExtra != nil:
		return *firstExtra
	}
	return validationProbeResult{validation: transport.SiteFileValidationReady}
}

// measureKeptFileValidation is a kept file's validation for an operation that
// asked for the probe: CelikPanel's challenge file first (kept or failed is
// known without asking nginx), then the probe.
func (ng *NginxGenerator) measureKeptFileValidation(item ManagedVhostItem, challenge string) validationProbeResult {
	switch challenge {
	case transport.SiteFileChallengeKept:
		return validationProbeResult{validation: transport.SiteFileValidationChallengeKept}
	case transport.SiteFileChallengeFailed:
		return validationProbeResult{validation: transport.SiteFileValidationChallengeFailed}
	}
	return ng.probeValidation(item.ACMEChallengeRoot, item.Probe)
}

// publishChallengeForProbe is the site-config read's part of the probe: when
// CelikPanel's challenge file is absent or holds other inputs, it is written
// (CelikPanel's own file in its own directory, exactly what any certificate
// operation writes first), nginx checks the configuration and is reloaded
// once; a refusal puts the file back. Without it the probe could not measure
// the owner's file. The caller holds nginxMutationMu.
func (ng *NginxGenerator) publishChallengeForProbe(item ManagedVhostItem) (string, string) {
	plan, state := challengeFileState(item)
	switch state {
	case transport.SiteFileChallengeAbsent, transport.SiteFileChallengeDiffers:
	default:
		return state, ""
	}
	if err := ensurePanelManagedDir(item.Domain); err != nil {
		return transport.SiteFileChallengeFailed, "challenge file: " + err.Error()
	}
	if err := writeManagedSiteFile(plan.path, plan.sealed, plan.inspected); err != nil {
		return transport.SiteFileChallengeFailed, "challenge file: " + err.Error()
	}
	written := []plannedSiteFileWrite{{domain: item.Domain, available: plan.path, sealed: plan.sealed, inspected: plan.inspected}}
	if err := ng.ValidateNginx(); err != nil {
		detail := err.Error()
		var refused *NginxConfigRefusedError
		if errors.As(err, &refused) {
			detail = refused.FirstLine()
		}
		rollbackErr := restorePlannedSiteFiles(written)
		return transport.SiteFileChallengeFailed, fmt.Sprintf("challenge file: %v", ng.finishManagedRollback(errors.New(detail), rollbackErr))
	}
	if err := ng.ReloadNginx(); err != nil {
		rollbackErr := restorePlannedSiteFiles(written)
		return transport.SiteFileChallengeFailed, fmt.Sprintf("challenge file: %v", ng.finishManagedRollback(err, rollbackErr))
	}
	return transport.SiteFileChallengeWritten, ""
}
