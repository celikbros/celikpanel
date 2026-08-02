package main

import (
	"fmt"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

// ApplyVhost regenerates a domain's nginx vhost from explicit data (used
// when the project type or hosting settings change). Write → validate →
// reload; a config that fails `nginx -t` is rolled back so nginx never
// stays broken.
//
// ApplyVhost, bir domain'in nginx vhost'unu açık verilerden yeniden üretir
// (proje tipi ya da barındırma ayarları değişince kullanılır). Yaz → doğrula
// → yeniden yükle; `nginx -t`ten geçemeyen yapılandırma geri alınır, nginx
// asla bozuk kalmaz.

type ApplyVhostRequest = transport.ApplyVhostRequest
type ApplyVhostResponse = transport.ApplyVhostResponse
type ApplyVhostsRequest = transport.ApplyVhostsRequest
type ApplyVhostsResponse = transport.ApplyVhostsResponse

const maxApplyVhostBatch = 4096

func (a *Agent) ApplyVhost(req *ApplyVhostRequest, resp *ApplyVhostResponse) error {
	if req == nil {
		resp.Error = "vhost request is required"
		return nil
	}
	if err := requireExpectedBuildCommit(
		req.ExpectedBuildCommit,
		"applying a vhost",
	); err != nil {
		resp.Error = err.Error()
		return nil
	}
	rendered, err := a.renderValidatedVhost(req)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := prepareValidatedVhostChallengeRoot(req); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if err := a.nginxGen.ApplyVhost(rendered.Domain, rendered.Config); err != nil {
		resp.Error = err.Error()
		return nil
	}

	resp.Config = rendered.Config
	return nil
}

// ApplyVhosts validates and renders the entire request before touching any
// nginx vhost. The generator then snapshots and writes every file under one
// global mutation lock, validates once and reloads once. There is no
// compatibility fallback to repeated ApplyVhost calls: an older agent must
// fail closed during a rolling upgrade instead of partially applying a batch.
func (a *Agent) ApplyVhosts(
	req *ApplyVhostsRequest,
	resp *ApplyVhostsResponse,
) error {
	if req == nil {
		resp.Error = "vhost batch request is required"
		return nil
	}
	if err := requireExpectedVhostBatchBuild(req.ExpectedBuildCommit); err != nil {
		resp.Error = err.Error()
		return nil
	}
	if len(req.Vhosts) == 0 {
		return nil
	}
	if len(req.Vhosts) > maxApplyVhostBatch {
		resp.Error = fmt.Sprintf(
			"vhost batch exceeds safe limit %d",
			maxApplyVhostBatch,
		)
		return nil
	}

	rendered, err := a.renderValidatedVhostBatch(req.Vhosts)
	if err != nil {
		resp.Error = err.Error()
		return nil
	}
	for index := range req.Vhosts {
		if err := prepareValidatedVhostChallengeRoot(&req.Vhosts[index]); err != nil {
			resp.Error = fmt.Sprintf(
				"prepare vhost batch item %d: %v",
				index,
				err,
			)
			return nil
		}
	}

	if err := a.nginxGen.ApplyVhosts(rendered); err != nil {
		resp.Error = err.Error()
		return nil
	}
	resp.Applied = len(rendered)
	return nil
}

func requireExpectedVhostBatchBuild(expectedRaw string) error {
	expected := strings.TrimSpace(expectedRaw)
	actual := strings.TrimSpace(buildCommit)
	expectedDevelopment := expected == "" || expected == "unknown"
	actualDevelopment := actual == "" || actual == "unknown"
	if expectedDevelopment && actualDevelopment {
		return nil
	}
	if expectedDevelopment {
		return fmt.Errorf(
			"expected panel build commit is required for this agent build",
		)
	}
	if actualDevelopment || actual != expected {
		return fmt.Errorf(
			"panel/agent build mismatch (panel %s, agent %s); finish the paired upgrade before applying startup vhosts",
			expected,
			actual,
		)
	}
	return nil
}

func (a *Agent) renderValidatedVhostBatch(
	requests []ApplyVhostRequest,
) ([]services.RenderedVhost, error) {
	rendered := make([]services.RenderedVhost, 0, len(requests))
	domains := make(map[string]struct{}, len(requests))
	domainIDs := make(map[int]struct{}, len(requests))
	siteIDs := make(map[int]struct{}, len(requests))
	for index := range requests {
		item, err := a.renderValidatedVhost(&requests[index])
		if err != nil {
			return nil, fmt.Errorf("vhost batch item %d: %w", index, err)
		}
		if _, exists := domains[item.Domain]; exists {
			return nil, fmt.Errorf(
				"vhost batch contains duplicate domain %q",
				item.Domain,
			)
		}
		if _, exists := domainIDs[requests[index].DomainID]; exists {
			return nil, fmt.Errorf(
				"vhost batch contains a duplicate domain identity",
			)
		}
		if _, exists := siteIDs[requests[index].SiteID]; exists {
			return nil, fmt.Errorf(
				"vhost batch contains a duplicate site identity",
			)
		}
		domains[item.Domain] = struct{}{}
		domainIDs[requests[index].DomainID] = struct{}{}
		siteIDs[requests[index].SiteID] = struct{}{}
		rendered = append(rendered, item)
	}
	return rendered, nil
}

func (a *Agent) renderValidatedVhost(
	req *ApplyVhostRequest,
) (services.RenderedVhost, error) {
	data, err := validatedVhostData(req)
	if err != nil {
		return services.RenderedVhost{}, err
	}
	config, err := a.nginxGen.Render(data)
	if err != nil {
		return services.RenderedVhost{}, err
	}
	return services.RenderedVhost{
		Domain: data.Domain,
		Config: config,
	}, nil
}

func prepareValidatedVhostChallengeRoot(req *ApplyVhostRequest) error {
	if req == nil {
		return fmt.Errorf("vhost request is required")
	}
	expectedChallengeRoot, err := hostingpath.ACMEChallengeRoot(
		req.SubscriptionID,
		req.DomainID,
	)
	if err != nil {
		return fmt.Errorf("derive ACME challenge root: %w", err)
	}
	preparedChallengeRoot, err := prepareACMEChallengeRoot(
		req.SubscriptionID,
		req.DomainID,
	)
	if err != nil {
		return fmt.Errorf(
			"prepare ACME challenge root: %w",
			err,
		)
	}
	if preparedChallengeRoot != expectedChallengeRoot {
		return fmt.Errorf(
			"prepared ACME challenge root did not match immutable site identity",
		)
	}
	return nil
}

const (
	maxVhostServerNames   = 128
	maxACMEChallengeNames = 100
	maxForwardTargetLen   = 2048
	maxNginxPathLen       = 4096
	maxHSTSMaxAge         = 63072000
)

var (
	phpSocketPattern = regexp.MustCompile(
		`^/(?:var/)?run/php/php[0-9]{1,2}\.[0-9]{1,2}-fpm-site([1-9][0-9]*)\.sock$`,
	)
	certificateVersionPattern = regexp.MustCompile(`^sha256-[0-9a-f]{64}$`)
)

// validatedVhostData is the agent-side trust boundary for every scalar that
// reaches the nginx template. The panel validates for usability; the agent
// validates again for safety because an authenticated RPC caller must not be
// able to turn a template field into another nginx directive.
func validatedVhostData(req *ApplyVhostRequest) (services.VhostData, error) {
	if req == nil {
		return services.VhostData{}, fmt.Errorf("vhost request is required")
	}
	if req.SiteID <= 0 {
		return services.VhostData{}, fmt.Errorf("a valid site identity is required")
	}
	if err := hostingpath.ValidateDocumentRoot(
		req.DocumentRoot, req.SubscriptionID, req.DomainID,
	); err != nil {
		return services.VhostData{}, fmt.Errorf(
			"refusing a document root outside the site's immutable home",
		)
	}

	domain, err := hostname.CanonicalFQDN(req.Domain)
	if err != nil {
		return services.VhostData{}, fmt.Errorf("a valid canonical domain is required")
	}
	tempDomain := ""
	if strings.TrimSpace(req.TempDomain) != "" {
		tempDomain, err = hostname.CanonicalFQDN(req.TempDomain)
		if err != nil {
			return services.VhostData{}, fmt.Errorf("temporary domain is invalid")
		}
	}
	serverNames, err := normalizeVhostServerNames(req.ServerNames)
	if err != nil {
		return services.VhostData{}, err
	}
	if req.RedirectWWW {
		wwwName := "www." + domain
		if !containsVhostServerName(serverNames, domain) ||
			!containsVhostServerName(serverNames, wwwName) {
			return services.VhostData{}, fmt.Errorf(
				"www redirection requires the canonical domain and its managed www hostname",
			)
		}
	}
	challengeNames, err := normalizeACMEChallengeNames(domain, req.ACMEChallengeNames)
	if err != nil {
		return services.VhostData{}, err
	}
	acmeChallengeRoot, err := hostingpath.ACMEChallengeRoot(
		req.SubscriptionID, req.DomainID,
	)
	if err != nil {
		return services.VhostData{}, fmt.Errorf("derive ACME challenge root: %w", err)
	}

	projectType := strings.ToLower(strings.TrimSpace(req.ProjectType))
	if projectType == "" {
		projectType = "php"
	}
	switch projectType {
	case "php", "static", "node", "proxy", "forwarding":
	default:
		return services.VhostData{}, fmt.Errorf("unsupported project type")
	}
	if req.ForwardCode != 0 && req.ForwardCode != 301 && req.ForwardCode != 302 {
		return services.VhostData{}, fmt.Errorf("forwarding status must be 301 or 302")
	}
	if req.HSTSMaxAge < 0 || req.HSTSMaxAge > maxHSTSMaxAge {
		return services.VhostData{}, fmt.Errorf("HSTS max-age is outside the supported range")
	}

	data := services.VhostData{
		SiteID:             req.SiteID,
		Domain:             domain,
		TempDomain:         tempDomain,
		DocumentRoot:       req.DocumentRoot,
		ServerNames:        serverNames,
		ACMEChallengeNames: challengeNames,
		ACMEChallengeRoot:  acmeChallengeRoot,
		RedirectWWW:        req.RedirectWWW,
		ProjectType:        projectType,
	}

	switch projectType {
	case "php":
		if err := validatePHPSocket(req.PHPSocket, req.SiteID); err != nil {
			return services.VhostData{}, err
		}
		data.PHPSocket = req.PHPSocket
	case "node":
		if req.AppPort < 1024 || req.AppPort > 65535 {
			return services.VhostData{}, fmt.Errorf("node application port is outside the supported range")
		}
		data.AppPort = req.AppPort
	case "proxy", "forwarding":
		forwardTarget, err := normalizeForwardTarget(req.ForwardTo)
		if err != nil {
			return services.VhostData{}, err
		}
		data.ForwardTo = forwardTarget
		if projectType == "forwarding" {
			data.ForwardCode = req.ForwardCode
			if data.ForwardCode == 0 {
				data.ForwardCode = 301
			}
		}
	}

	sslType := strings.ToLower(strings.TrimSpace(defaultStr(req.SSLType, "none")))
	switch sslType {
	case "none":
		if strings.TrimSpace(req.SSLCert) != "" ||
			strings.TrimSpace(req.SSLKey) != "" ||
			req.ForceHTTPS ||
			req.HSTSEnabled ||
			req.HSTSMaxAge != 0 {
			return services.VhostData{}, fmt.Errorf("HTTPS settings require an active certificate")
		}
		data.SSLType = "none"
	case "custom", "letsencrypt":
		if err := validateVhostCertificatePaths(domain, req.SSLCert, req.SSLKey); err != nil {
			return services.VhostData{}, err
		}
		if req.HSTSEnabled && (!req.ForceHTTPS || req.HSTSMaxAge <= 0) {
			return services.VhostData{}, fmt.Errorf("HSTS requires HTTPS redirection and a positive max-age")
		}
		data.SSLType = sslType
		data.SSLCert = req.SSLCert
		data.SSLKey = req.SSLKey
		data.ForceHTTPS = req.ForceHTTPS
		data.HSTSEnabled = req.HSTSEnabled
		data.HSTSMaxAge = req.HSTSMaxAge
	default:
		return services.VhostData{}, fmt.Errorf("unsupported SSL type")
	}

	return data, nil
}

func containsVhostServerName(names []string, target string) bool {
	for _, name := range names {
		if name == target {
			return true
		}
	}
	return false
}

func normalizeVhostServerNames(raw []string) ([]string, error) {
	if len(raw) > maxVhostServerNames {
		return nil, fmt.Errorf("too many server names")
	}
	names := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, candidate := range raw {
		if strings.TrimSpace(candidate) == "" {
			continue
		}
		name, err := hostname.CanonicalFQDN(candidate)
		if err != nil {
			return nil, fmt.Errorf("invalid server name")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func validatePHPSocket(socket string, siteID int) error {
	if socket != strings.TrimSpace(socket) || containsNginxUnsafeScalar(socket) {
		return fmt.Errorf("PHP-FPM socket path is invalid")
	}
	match := phpSocketPattern.FindStringSubmatch(socket)
	if len(match) != 2 {
		return fmt.Errorf("PHP-FPM socket must be the per-site managed socket")
	}
	socketSiteID, err := strconv.Atoi(match[1])
	if err != nil || socketSiteID != siteID {
		return fmt.Errorf("PHP-FPM socket does not belong to this site")
	}
	return nil
}

func validateVhostCertificatePaths(domain, certPath, keyPath string) error {
	if !safeAbsoluteNginxPath(certPath) || !safeAbsoluteNginxPath(keyPath) {
		return fmt.Errorf("certificate paths are invalid")
	}
	if path.Base(certPath) != "fullchain.pem" || path.Base(keyPath) != "privkey.pem" {
		return fmt.Errorf("certificate paths must name the managed full chain and private key")
	}
	certDir := path.Dir(certPath)
	if certDir != path.Dir(keyPath) {
		return fmt.Errorf("certificate and private key must come from the same managed version")
	}

	managedRoot, err := customCertificateDirectory(domain)
	if err != nil {
		return fmt.Errorf("certificate domain is invalid")
	}
	managedRoot = path.Clean(filepath.ToSlash(managedRoot))
	if path.Dir(certDir) == managedRoot &&
		certificateVersionPattern.MatchString(path.Base(certDir)) {
		return nil
	}

	for _, legacyRoot := range []string{
		path.Join(legacyCertbotConfigDir, "live", domain),
		path.Join(siteCertbotConfigDir, "live", domain),
	} {
		if certDir == legacyRoot {
			return nil
		}
	}
	return fmt.Errorf("certificate paths are outside this domain's managed certificate stores")
}

func safeAbsoluteNginxPath(candidate string) bool {
	return candidate != "" &&
		len(candidate) <= maxNginxPathLen &&
		strings.HasPrefix(candidate, "/") &&
		path.Clean(candidate) == candidate &&
		!containsNginxUnsafeScalar(candidate)
}

func normalizeForwardTarget(raw string) (string, error) {
	if raw == "" ||
		raw != strings.TrimSpace(raw) ||
		len(raw) > maxForwardTargetLen ||
		containsNginxUnsafeScalar(raw) {
		return "", fmt.Errorf("forwarding target is invalid")
	}
	target, err := url.Parse(raw)
	if err != nil ||
		target.Opaque != "" ||
		target.User != nil ||
		target.Host == "" ||
		target.Fragment != "" {
		return "", fmt.Errorf("forwarding target must be an absolute HTTP or HTTPS URL")
	}
	target.Scheme = strings.ToLower(target.Scheme)
	if target.Scheme != "http" && target.Scheme != "https" {
		return "", fmt.Errorf("forwarding target must use HTTP or HTTPS")
	}

	host := strings.ToLower(strings.TrimSuffix(target.Hostname(), "."))
	if host == "" {
		return "", fmt.Errorf("forwarding target hostname is invalid")
	}
	if net.ParseIP(host) == nil {
		if err := hostname.Validate(host); err != nil {
			return "", fmt.Errorf("forwarding target hostname is invalid")
		}
	}
	port := target.Port()
	if port != "" {
		number, err := strconv.Atoi(port)
		if err != nil || number <= 0 || number > 65535 {
			return "", fmt.Errorf("forwarding target port is invalid")
		}
		target.Host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		target.Host = "[" + host + "]"
	} else {
		target.Host = host
	}

	normalized := target.String()
	if len(normalized) > maxForwardTargetLen || containsNginxUnsafeScalar(normalized) {
		return "", fmt.Errorf("forwarding target contains unsafe nginx syntax")
	}
	return normalized, nil
}

func containsNginxUnsafeScalar(value string) bool {
	for _, r := range value {
		if unicode.IsControl(r) || unicode.IsSpace(r) {
			return true
		}
		switch r {
		case ';', '{', '}', '"', '\'', '$', '`', '\\':
			return true
		}
	}
	return false
}

func normalizeACMEChallengeNames(domain string, raw []string) ([]string, error) {
	if len(raw) > maxACMEChallengeNames {
		return nil, fmt.Errorf("too many ACME challenge hostnames")
	}
	names := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, candidate := range raw {
		name, canonicalErr := hostname.CanonicalFQDN(candidate)
		if canonicalErr != nil {
			return nil, fmt.Errorf("invalid ACME challenge hostname: %w", canonicalErr)
		}
		if name == domain {
			return nil, fmt.Errorf("primary domain must use the main ACME challenge location")
		}
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	return names, nil
}

func defaultStr(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
