package services

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTLSVhostRuntimeSettings(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}

	base := VhostData{
		SiteID:            12,
		Domain:            "biovision.health",
		ServerNames:       []string{"biovision.health", "www.biovision.health", "alias.example"},
		ACMEChallengeRoot: testACMEChallengeRoot,
		DocumentRoot:      "/var/www/biovision.health",
		ProjectType:       "static",
		SSLType:           "letsencrypt",
		SSLCert:           "/etc/letsencrypt/live/biovision.health/fullchain.pem",
		SSLKey:            "/etc/letsencrypt/live/biovision.health/privkey.pem",
		ForceHTTPS:        true,
		HSTSEnabled:       true,
		HSTSMaxAge:        31536000,
	}

	out, err := ng.Render(base)
	if err != nil {
		t.Fatal(err)
	}
	serverNames := "server_name biovision.health www.biovision.health alias.example;"
	if got := strings.Count(out, serverNames); got != 2 {
		t.Fatalf("all managed names must appear in HTTP and HTTPS server_name directives; got %d\n%s", got, out)
	}
	hsts := `add_header Strict-Transport-Security "max-age=31536000" always;`
	if got := strings.Count(out, hsts); got != 1 {
		t.Fatalf("HSTS must appear exactly once in the HTTPS server; got %d\n%s", got, out)
	}
	if strings.Contains(out, "includeSubDomains") || strings.Contains(out, "preload") {
		t.Error("HSTS must not opt customers into includeSubDomains or preload")
	}
	challenge := "location ^~ /.well-known/acme-challenge/"
	redirect := "return 301 https://$host$request_uri;"
	if !strings.Contains(out, challenge) || !strings.Contains(out, "root "+testACMEChallengeRoot+";") {
		t.Error("forced HTTPS must serve HTTP-01 from the root-owned challenge root")
	}
	if challengeAt, redirectAt := strings.Index(out, challenge), strings.Index(out, redirect); challengeAt < 0 || redirectAt < 0 || challengeAt > redirectAt {
		t.Error("the ACME exception must be rendered before the catch-all HTTPS redirect")
	}

	base.ForceHTTPS = false
	base.HSTSEnabled = false
	out, err = ng.Render(base)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, redirect) {
		t.Error("HTTP must not redirect when ForceHTTPS is disabled")
	}
	if got := strings.Count(out, "try_files $uri $uri/ =404;"); got != 2 {
		t.Fatalf("with a certificate and ForceHTTPS off, content must be served on HTTP and HTTPS; got %d serving blocks", got)
	}
	if !strings.Contains(out, `Strict-Transport-Security "max-age=0"`) {
		t.Error("disabling HSTS while HTTPS remains available must clear the browser policy")
	}
}

func TestNoCertificateNeverEmitsHTTPSControls(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	out, err := ng.Render(VhostData{
		Domain:            "example.test",
		ServerNames:       []string{"www.example.test", "EXAMPLE.TEST"},
		ACMEChallengeRoot: testACMEChallengeRoot,
		DocumentRoot:      "/var/www/example.test",
		ProjectType:       "static",
		SSLType:           "none",
		ForceHTTPS:        true,
		HSTSEnabled:       true,
		HSTSMaxAge:        63072000,
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "listen 443") || strings.Contains(out, "Strict-Transport-Security") || strings.Contains(out, "return 301 https://") {
		t.Fatalf("HTTPS-only controls must not be rendered without a certificate\n%s", out)
	}
	if !strings.Contains(out, "server_name example.test www.example.test;") {
		t.Fatalf("managed names must still be served over HTTP\n%s", out)
	}
}

func TestInitialNodeAndProxyVhostsExposeACMEChallenge(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}

	tests := []VhostData{
		{
			Domain:            "node.example",
			ACMEChallengeRoot: testACMEChallengeRoot,
			DocumentRoot:      "/var/www/node.example",
			ProjectType:       "node",
			AppPort:           3000,
			SSLType:           "none",
		},
		{
			Domain:            "proxy.example",
			ACMEChallengeRoot: testACMEChallengeRoot,
			DocumentRoot:      "/var/www/proxy.example",
			ProjectType:       "proxy",
			ForwardTo:         "http://127.0.0.1:8080",
			SSLType:           "none",
		},
	}

	for _, test := range tests {
		t.Run(test.ProjectType, func(t *testing.T) {
			out, err := ng.Render(test)
			if err != nil {
				t.Fatal(err)
			}
			challenge := "location ^~ /.well-known/acme-challenge/"
			if got := strings.Count(out, challenge); got != 1 {
				t.Fatalf("initial %s vhost must expose one HTTP-01 location; got %d\n%s", test.ProjectType, got, out)
			}
			if !strings.Contains(out, "root "+test.ACMEChallengeRoot+";") {
				t.Fatalf("initial %s ACME location must use the root-owned challenge root\n%s", test.ProjectType, out)
			}
			if challengeAt, appAt := strings.Index(out, challenge), strings.Index(out, "location / {"); challengeAt < 0 || appAt < 0 || challengeAt > appAt {
				t.Fatalf("initial %s ACME location must precede the application catch-all\n%s", test.ProjectType, out)
			}
		})
	}
}

func TestForwardingVhostHonorsTLSRedirectAndHSTS(t *testing.T) {
	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}

	data := VhostData{
		Domain:            "forward.example",
		ACMEChallengeRoot: testACMEChallengeRoot,
		DocumentRoot:      "/var/www/forward.example",
		ProjectType:       "forwarding",
		ForwardTo:         "https://destination.example",
		ForwardCode:       302,
		SSLType:           "letsencrypt",
		SSLCert:           "/etc/letsencrypt/live/forward.example/fullchain.pem",
		SSLKey:            "/etc/letsencrypt/live/forward.example/privkey.pem",
		ForceHTTPS:        true,
		HSTSEnabled:       true,
		HSTSMaxAge:        31536000,
	}

	out, err := ng.Render(data)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(out, "listen 80;"); got != 1 {
		t.Fatalf("TLS forwarding must have one IPv4 HTTP server; got %d\n%s", got, out)
	}
	if got := strings.Count(out, "listen 443 ssl;"); got != 1 {
		t.Fatalf("TLS forwarding must have one IPv4 HTTPS server; got %d\n%s", got, out)
	}
	if got := strings.Count(out, "location ^~ /.well-known/acme-challenge/"); got != 1 {
		t.Fatalf("forwarding HTTP server must keep one ACME location; got %d\n%s", got, out)
	}
	httpsAt := strings.Index(out, "listen 443 ssl;")
	redirectAt := strings.Index(out, "return 301 https://$host$request_uri;")
	targetAt := strings.Index(out, "return 302 https://destination.example$request_uri;")
	hstsAt := strings.Index(out, `add_header Strict-Transport-Security "max-age=31536000" always;`)
	if redirectAt < 0 || redirectAt > httpsAt {
		t.Fatalf("ForceHTTPS must redirect the forwarding HTTP catch-all before the HTTPS server\n%s", out)
	}
	if targetAt < httpsAt {
		t.Fatalf("with ForceHTTPS enabled, destination forwarding must happen in the HTTPS server\n%s", out)
	}
	if hstsAt < httpsAt {
		t.Fatalf("HSTS must be emitted only inside the HTTPS forwarding server\n%s", out)
	}

	data.ForceHTTPS = false
	data.HSTSEnabled = false
	out, err = ng.Render(data)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "return 301 https://$host$request_uri;") {
		t.Fatalf("forwarding HTTP must not redirect to HTTPS when ForceHTTPS is disabled\n%s", out)
	}
	if got := strings.Count(out, "return 302 https://destination.example$request_uri;"); got != 2 {
		t.Fatalf("without ForceHTTPS the destination must be served by HTTP and HTTPS; got %d\n%s", got, out)
	}
	if !strings.Contains(out, `Strict-Transport-Security "max-age=0"`) {
		t.Fatalf("disabled HSTS must clear the browser policy while HTTPS remains available\n%s", out)
	}
}

func TestApplyVhostReloadFailureRestoresAndReloadsPreviousConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create the production-style sites-enabled symlink")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	domain := "reload-rollback.example"
	if err := ng.WriteVhostFile(domain, "old config\n"); err != nil {
		t.Fatal(err)
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		if reloadCalls == 1 {
			return errors.New("reload refused")
		}
		return nil
	}

	err = ng.ApplyVhost(domain, "new config\n")
	if err == nil || !strings.Contains(err.Error(), "rollback restored and reloaded") {
		t.Fatalf("reload failure must report a successful rollback, got %v", err)
	}
	if validateCalls != 2 || reloadCalls != 2 {
		t.Fatalf("new and restored configs must each be validated and reloaded; validate=%d reload=%d", validateCalls, reloadCalls)
	}
	available, _ := vhostPaths(domain)
	content, err := os.ReadFile(available)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old config\n" {
		t.Fatalf("reload rollback did not restore previous config: %q", content)
	}
}

func TestVhostMutationsAreSerializedAcrossGenerators(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create the production-style sites-enabled symlink")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	first, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}

	firstReload := make(chan struct{})
	releaseFirst := make(chan struct{})
	secondValidation := make(chan struct{})
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseFirst) }) }
	defer release()

	first.validateNginx = func() error { return nil }
	first.reloadNginx = func() error {
		close(firstReload)
		<-releaseFirst
		return nil
	}
	second.validateNginx = func() error {
		close(secondValidation)
		return nil
	}
	second.reloadNginx = func() error { return nil }

	firstResult := make(chan error, 1)
	go func() { firstResult <- first.ApplyVhost("first.example", "first config\n") }()
	<-firstReload

	secondStarted := make(chan struct{})
	secondResult := make(chan error, 1)
	go func() {
		close(secondStarted)
		secondResult <- second.ApplyVhost("second.example", "second config\n")
	}()
	<-secondStarted

	select {
	case <-secondValidation:
		t.Fatal("a second generator interleaved while the first mutation was waiting to reload")
	case <-time.After(100 * time.Millisecond):
	}
	release()
	if err := <-firstResult; err != nil {
		t.Fatal(err)
	}
	if err := <-secondResult; err != nil {
		t.Fatal(err)
	}
	select {
	case <-secondValidation:
	default:
		t.Fatal("second mutation never reached validation after the first released the global lock")
	}
}

func TestDeleteVhostPropagatesRemoveErrors(t *testing.T) {
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	available, _ := vhostPaths("remove-error.example")
	if err := os.MkdirAll(available, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(available, "keeps-directory-non-empty"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	err = ng.DeleteVhost("remove-error.example")
	if err == nil {
		t.Fatal("DeleteVhost swallowed a non-IsNotExist os.Remove error")
	}
	if !strings.Contains(err.Error(), "remove vhost file") {
		t.Fatalf("DeleteVhost returned an unhelpful error: %v", err)
	}
}

func TestWriteAndValidateVhostRestoresPreviousConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test users cannot create the production-style sites-enabled symlink")
	}
	previousDir := nginxDir
	nginxDir = t.TempDir()
	t.Cleanup(func() { nginxDir = previousDir })

	ng, err := NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	domain := "rollback.example"
	if err := ng.WriteVhostFile(domain, "old config\n"); err != nil {
		t.Fatal(err)
	}
	validateCalls := 0
	reloadCalls := 0
	ng.validateNginx = func() error {
		validateCalls++
		if validateCalls%2 == 1 {
			return errors.New("invalid generated config")
		}
		return nil
	}
	ng.reloadNginx = func() error {
		reloadCalls++
		return nil
	}

	if err := ng.WriteAndValidateVhost(domain, "new broken config\n"); err == nil || !strings.Contains(err.Error(), "rollback restored and reloaded") {
		t.Fatalf("expected validation failure with completed rollback, got %v", err)
	}
	available, enabled := vhostPaths(domain)
	content, err := os.ReadFile(available)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "old config\n" {
		t.Fatalf("previous vhost was not restored: %q", content)
	}
	target, err := os.Readlink(enabled)
	if err != nil {
		t.Fatal(err)
	}
	if target != available {
		t.Fatalf("enabled vhost link was not restored: got %q want %q", target, available)
	}

	missingDomain := "new-invalid.example"
	if err := ng.WriteAndValidateVhost(missingDomain, "broken config\n"); err == nil {
		t.Fatal("expected validation failure for a new vhost")
	}
	missingAvailable, missingEnabled := vhostPaths(missingDomain)
	for _, path := range []string{missingAvailable, missingEnabled} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Fatalf("a failed new vhost must be removed, path still exists: %s", path)
		}
	}
	if validateCalls != 4 || reloadCalls != 2 {
		t.Fatalf("each failed validation must validate and reload its restored state; validate=%d reload=%d", validateCalls, reloadCalls)
	}
}
