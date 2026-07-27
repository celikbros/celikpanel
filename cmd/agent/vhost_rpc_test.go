package main

import (
	"fmt"
	"path"
	"reflect"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
)

func TestRenderValidatedVhostBatchRejectsAmbiguousIdentitySets(t *testing.T) {
	nginxGenerator, err := services.NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{nginxGen: nginxGenerator}

	first := validApplyVhostRequest(t)
	first.Domain = "first-batch.example"
	first.TempDomain = ""
	first.ServerNames = nil

	second := first
	second.SiteID = first.SiteID + 1
	second.DomainID = first.DomainID + 1
	second.Domain = "second-batch.example"
	second.PHPSocket = "/var/run/php/php8.3-fpm-site43.sock"
	second.DocumentRoot, err = hostingpath.DocumentRoot(
		second.SubscriptionID,
		second.DomainID,
	)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		mutate func(*ApplyVhostRequest)
		want   string
	}{
		{
			name: "duplicate canonical domain",
			mutate: func(request *ApplyVhostRequest) {
				request.Domain = "FIRST-BATCH.EXAMPLE."
			},
			want: "duplicate domain",
		},
		{
			name: "duplicate domain identity",
			mutate: func(request *ApplyVhostRequest) {
				request.DomainID = first.DomainID
				request.DocumentRoot = first.DocumentRoot
			},
			want: "duplicate domain identity",
		},
		{
			name: "duplicate site identity",
			mutate: func(request *ApplyVhostRequest) {
				request.SiteID = first.SiteID
				request.PHPSocket = first.PHPSocket
			},
			want: "duplicate site identity",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := second
			test.mutate(&candidate)
			rendered, err := agent.renderValidatedVhostBatch(
				[]ApplyVhostRequest{first, candidate},
			)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("batch error = %v, want %q", err, test.want)
			}
			if rendered != nil {
				t.Fatalf("ambiguous batch returned renderable vhosts: %#v", rendered)
			}
		})
	}
}

func TestApplyVhostsRejectsOversizedBatchBeforeGeneratorUse(t *testing.T) {
	requests := make([]ApplyVhostRequest, maxApplyVhostBatch+1)
	var response ApplyVhostsResponse
	if err := (&Agent{}).ApplyVhosts(
		&ApplyVhostsRequest{Vhosts: requests},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "safe limit") {
		t.Fatalf("oversized batch error = %q", response.Error)
	}
	if response.Applied != 0 {
		t.Fatalf("oversized batch applied count = %d", response.Applied)
	}
}

func TestApplyVhostsRejectsBuildMismatchBeforeGeneratorUse(t *testing.T) {
	previousCommit := buildCommit
	buildCommit = "agent-release-commit"
	t.Cleanup(func() { buildCommit = previousCommit })

	var response ApplyVhostsResponse
	if err := (&Agent{}).ApplyVhosts(
		&ApplyVhostsRequest{
			ExpectedBuildCommit: "panel-other-commit",
			Vhosts:              []ApplyVhostRequest{validApplyVhostRequest(t)},
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "build mismatch") {
		t.Fatalf("mismatched batch error = %q", response.Error)
	}
	if response.Applied != 0 {
		t.Fatalf("mismatched batch applied count = %d", response.Applied)
	}
}

func TestApplyVhostsProductionBuildRequiresPanelCommit(t *testing.T) {
	previousCommit := buildCommit
	buildCommit = "agent-release-commit"
	t.Cleanup(func() { buildCommit = previousCommit })

	var response ApplyVhostsResponse
	if err := (&Agent{}).ApplyVhosts(
		&ApplyVhostsRequest{
			Vhosts: []ApplyVhostRequest{validApplyVhostRequest(t)},
		},
		&response,
	); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(response.Error, "build commit is required") {
		t.Fatalf("missing expected commit error = %q", response.Error)
	}
	if response.Applied != 0 {
		t.Fatalf("missing-commit batch applied count = %d", response.Applied)
	}
}

func TestNormalizeACMEChallengeNamesAcceptsCanonicalValidationOnlyNames(t *testing.T) {
	names, err := normalizeACMEChallengeNames(
		"example.test",
		[]string{
			" MAIL.Example.TEST. ",
			"alias.example.test",
			"mail.example.test",
		},
	)
	if err != nil {
		t.Fatalf("normalize validation-only names: %v", err)
	}
	want := []string{"mail.example.test", "alias.example.test"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("challenge names = %#v, want %#v", names, want)
	}

	for _, candidate := range []string{
		"example.test",
		"bad_name.example.test",
		"*.example.test",
		"--config-dir",
	} {
		t.Run(candidate, func(t *testing.T) {
			_, err := normalizeACMEChallengeNames("example.test", []string{candidate})
			if err == nil {
				t.Fatalf("challenge name %q unexpectedly accepted", candidate)
			}
		})
	}
}

func TestNormalizeACMEChallengeNamesRejectsOversizedList(t *testing.T) {
	names := make([]string, maxACMEChallengeNames+1)
	for i := range names {
		names[i] = fmt.Sprintf("alias-%d.example.test", i)
	}
	if _, err := normalizeACMEChallengeNames("example.test", names); err == nil {
		t.Fatal("oversized ACME challenge list unexpectedly accepted")
	}
}

func validApplyVhostRequest(t *testing.T) ApplyVhostRequest {
	t.Helper()
	documentRoot, err := hostingpath.DocumentRoot(7, 19)
	if err != nil {
		t.Fatalf("derive test document root: %v", err)
	}
	return ApplyVhostRequest{
		SiteID:         42,
		SubscriptionID: 7,
		DomainID:       19,
		Domain:         "Example.TEST.",
		TempDomain:     " Preview.Example.TEST. ",
		ServerNames: []string{
			"WWW.Example.TEST.",
			"www.example.test",
		},
		DocumentRoot: documentRoot,
		PHPSocket:    "/var/run/php/php8.3-fpm-site42.sock",
		SSLType:      "none",
		ProjectType:  "php",
	}
}

func TestValidatedVhostDataAcceptsAndCanonicalizesSupportedShapes(t *testing.T) {
	managedVersion := path.Join(
		"/etc/ssl/celikpanel/example.test",
		"sha256-"+strings.Repeat("a", 64),
	)
	tests := []struct {
		name   string
		mutate func(*ApplyVhostRequest)
		check  func(*testing.T, ApplyVhostRequest)
	}{
		{
			name: "managed php defaults",
			check: func(t *testing.T, req ApplyVhostRequest) {
				data, err := validatedVhostData(&req)
				if err != nil {
					t.Fatalf("validatedVhostData: %v", err)
				}
				if data.Domain != "example.test" || data.TempDomain != "preview.example.test" {
					t.Fatalf("canonical names = %q, %q", data.Domain, data.TempDomain)
				}
				if len(data.ServerNames) != 1 || data.ServerNames[0] != "www.example.test" {
					t.Fatalf("server names = %#v", data.ServerNames)
				}
				wantChallengeRoot, err := hostingpath.ACMEChallengeRoot(
					req.SubscriptionID, req.DomainID,
				)
				if err != nil {
					t.Fatal(err)
				}
				if data.ACMEChallengeRoot != wantChallengeRoot ||
					data.ACMEChallengeRoot == data.DocumentRoot {
					t.Fatalf(
						"challenge root = %q, document root = %q, want %q",
						data.ACMEChallengeRoot, data.DocumentRoot, wantChallengeRoot,
					)
				}
			},
		},
		{
			name: "node port",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "node"
				req.AppPort = 3001
			},
		},
		{
			name: "proxy URL",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "proxy"
				req.ForwardTo = "HTTPS://Backend.Internal:8443/base"
			},
		},
		{
			name: "forwarding default code",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "forwarding"
				req.ForwardTo = "https://destination.example/path"
			},
		},
		{
			name: "managed certificate snapshot",
			mutate: func(req *ApplyVhostRequest) {
				req.SSLType = "custom"
				req.SSLCert = path.Join(managedVersion, "fullchain.pem")
				req.SSLKey = path.Join(managedVersion, "privkey.pem")
				req.ForceHTTPS = true
				req.HSTSEnabled = true
				req.HSTSMaxAge = 31536000
			},
		},
		{
			name: "legacy certificate lineage",
			mutate: func(req *ApplyVhostRequest) {
				req.SSLType = "letsencrypt"
				req.SSLCert = "/etc/letsencrypt/live/example.test/fullchain.pem"
				req.SSLKey = "/etc/letsencrypt/live/example.test/privkey.pem"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validApplyVhostRequest(t)
			if test.mutate != nil {
				test.mutate(&req)
			}
			if test.check != nil {
				test.check(t, req)
				return
			}
			if _, err := validatedVhostData(&req); err != nil {
				t.Fatalf("validatedVhostData: %v", err)
			}
		})
	}
}

func TestValidatedVhostDataRejectsUnsafeTemplateInputs(t *testing.T) {
	if _, err := validatedVhostData(nil); err == nil {
		t.Fatal("nil request unexpectedly accepted")
	}

	tests := []struct {
		name   string
		mutate func(*ApplyVhostRequest)
	}{
		{
			name: "missing site identity",
			mutate: func(req *ApplyVhostRequest) {
				req.SiteID = 0
			},
		},
		{
			name: "missing subscription identity",
			mutate: func(req *ApplyVhostRequest) {
				req.SubscriptionID = 0
			},
		},
		{
			name: "foreign document root",
			mutate: func(req *ApplyVhostRequest) {
				req.DocumentRoot = "/var/www/other/public_html"
			},
		},
		{
			name: "directive in primary domain",
			mutate: func(req *ApplyVhostRequest) {
				req.Domain = "example.test; return 200"
			},
		},
		{
			name: "directive in temporary domain",
			mutate: func(req *ApplyVhostRequest) {
				req.TempDomain = "preview.example.test\nlisten 9000"
			},
		},
		{
			name: "directive in server name",
			mutate: func(req *ApplyVhostRequest) {
				req.ServerNames = []string{"www.example.test;return 200"}
			},
		},
		{
			name: "too many server names",
			mutate: func(req *ApplyVhostRequest) {
				req.ServerNames = make([]string, maxVhostServerNames+1)
			},
		},
		{
			name: "unsafe ACME name",
			mutate: func(req *ApplyVhostRequest) {
				req.ACMEChallengeNames = []string{"bad_name.example.test"}
			},
		},
		{
			name: "generic PHP socket",
			mutate: func(req *ApplyVhostRequest) {
				req.PHPSocket = "/run/php/php8.3-fpm.sock"
			},
		},
		{
			name: "another site's PHP socket",
			mutate: func(req *ApplyVhostRequest) {
				req.PHPSocket = "/var/run/php/php8.3-fpm-site41.sock"
			},
		},
		{
			name: "directive in PHP socket",
			mutate: func(req *ApplyVhostRequest) {
				req.PHPSocket = "/var/run/php/php8.3-fpm-site42.sock;include /tmp/x"
			},
		},
		{
			name: "unknown project type",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "php\ninclude"
			},
		},
		{
			name: "privileged node port",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "node"
				req.AppPort = 443
			},
		},
		{
			name: "oversized node port",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "node"
				req.AppPort = 65536
			},
		},
		{
			name: "missing proxy target",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "proxy"
			},
		},
		{
			name: "non HTTP proxy target",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "proxy"
				req.ForwardTo = "file:///etc/passwd"
			},
		},
		{
			name: "credential-bearing proxy target",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "proxy"
				req.ForwardTo = "https://user:pass@backend.example"
			},
		},
		{
			name: "fragment-bearing proxy target",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "proxy"
				req.ForwardTo = "https://backend.example/#fragment"
			},
		},
		{
			name: "directive in proxy target",
			mutate: func(req *ApplyVhostRequest) {
				req.ProjectType = "proxy"
				req.ForwardTo = "https://backend.example;include"
			},
		},
		{
			name: "invalid forwarding status",
			mutate: func(req *ApplyVhostRequest) {
				req.ForwardCode = 307
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validApplyVhostRequest(t)
			test.mutate(&req)
			if _, err := validatedVhostData(&req); err == nil {
				t.Fatal("unsafe request unexpectedly accepted")
			}
		})
	}
}

func TestValidatedVhostDataRejectsUnsafeTLSState(t *testing.T) {
	versionA := path.Join(
		"/etc/ssl/celikpanel/example.test",
		"sha256-"+strings.Repeat("a", 64),
	)
	versionB := path.Join(
		"/etc/ssl/celikpanel/example.test",
		"sha256-"+strings.Repeat("b", 64),
	)
	activate := func(req *ApplyVhostRequest) {
		req.SSLType = "custom"
		req.SSLCert = path.Join(versionA, "fullchain.pem")
		req.SSLKey = path.Join(versionA, "privkey.pem")
	}
	tests := []struct {
		name   string
		mutate func(*ApplyVhostRequest)
	}{
		{
			name: "unknown SSL type",
			mutate: func(req *ApplyVhostRequest) {
				req.SSLType = "custom;include"
			},
		},
		{
			name: "certificate attached to disabled TLS",
			mutate: func(req *ApplyVhostRequest) {
				req.SSLCert = path.Join(versionA, "fullchain.pem")
			},
		},
		{
			name: "HTTPS redirect without TLS",
			mutate: func(req *ApplyVhostRequest) {
				req.ForceHTTPS = true
			},
		},
		{
			name: "missing certificate pair",
			mutate: func(req *ApplyVhostRequest) {
				req.SSLType = "custom"
			},
		},
		{
			name: "cross-domain certificate path",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.SSLCert = "/etc/letsencrypt/live/other.test/fullchain.pem"
				req.SSLKey = "/etc/letsencrypt/live/other.test/privkey.pem"
			},
		},
		{
			name: "mismatched certificate versions",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.SSLKey = path.Join(versionB, "privkey.pem")
			},
		},
		{
			name: "directive in certificate path",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.SSLCert += ";include /tmp/x"
			},
		},
		{
			name: "oversized certificate path",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.SSLCert = "/" + strings.Repeat("a", maxNginxPathLen)
			},
		},
		{
			name: "wrong certificate leaf name",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.SSLCert = path.Join(versionA, "cert.pem")
			},
		},
		{
			name: "negative HSTS max age",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.HSTSMaxAge = -1
			},
		},
		{
			name: "oversized HSTS max age",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.HSTSMaxAge = maxHSTSMaxAge + 1
			},
		},
		{
			name: "HSTS without HTTPS redirect",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.HSTSEnabled = true
				req.HSTSMaxAge = 31536000
			},
		},
		{
			name: "HSTS without positive max age",
			mutate: func(req *ApplyVhostRequest) {
				activate(req)
				req.ForceHTTPS = true
				req.HSTSEnabled = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validApplyVhostRequest(t)
			test.mutate(&req)
			if _, err := validatedVhostData(&req); err == nil {
				t.Fatal("unsafe TLS state unexpectedly accepted")
			}
		})
	}
}
