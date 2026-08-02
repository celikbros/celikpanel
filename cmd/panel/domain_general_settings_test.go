package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func generalSiteState(t *testing.T, p *Panel, domainID int) (string, bool, string) {
	t.Helper()
	var webServer, documentRoot string
	var redirectWWW bool
	if err := p.db.GetDB().QueryRow(`
		SELECT COALESCE(web_server, ''), COALESCE(redirect_www, false), document_root
		FROM sites WHERE domain_id = ?`, domainID,
	).Scan(&webServer, &redirectWWW, &documentRoot); err != nil {
		t.Fatalf("read general site state: %v", err)
	}
	return webServer, redirectWWW, documentRoot
}

func TestUpdateDomainGeneralSettingsAppliesWWWRedirectToVhost(t *testing.T) {
	p, domainID := newDomainAliasFixture(t)
	_, _, documentRoot := generalSiteState(t, p, domainID)
	var applied []bool
	apply := func(ctx context.Context, gotDomainID int) error {
		if gotDomainID != domainID {
			t.Fatalf("apply domain ID = %d, want %d", gotDomainID, domainID)
		}
		req, err := p.buildVhostRequest(ctx, gotDomainID, nil)
		if err != nil {
			return err
		}
		applied = append(applied, req.RedirectWWW)
		return nil
	}

	err := p.updateDomainGeneralSettings(context.Background(), domainID, UpdateGeneralSettingsRequest{
		DocumentRoot: documentRoot,
		WebServer:    "nginx",
		RedirectWWW:  true,
	}, apply)
	if err != nil {
		t.Fatalf("update general settings: %v", err)
	}
	if len(applied) != 1 || !applied[0] {
		t.Fatalf("applied redirect states = %#v, want [true]", applied)
	}
	webServer, redirectWWW, _ := generalSiteState(t, p, domainID)
	if webServer != "nginx" || !redirectWWW {
		t.Fatalf("saved state = %q/%v, want nginx/true", webServer, redirectWWW)
	}
}

func TestUpdateDomainGeneralSettingsRestoresDatabaseAndVhostOnApplyFailure(t *testing.T) {
	p, domainID := newDomainAliasFixture(t)
	previousWebServer, previousRedirectWWW, documentRoot := generalSiteState(t, p, domainID)
	var applied []bool
	applyFailure := errors.New("nginx validation failed")
	apply := func(ctx context.Context, gotDomainID int) error {
		req, err := p.buildVhostRequest(ctx, gotDomainID, nil)
		if err != nil {
			return err
		}
		applied = append(applied, req.RedirectWWW)
		if len(applied) == 1 {
			return applyFailure
		}
		return nil
	}

	err := p.updateDomainGeneralSettings(context.Background(), domainID, UpdateGeneralSettingsRequest{
		DocumentRoot: documentRoot,
		WebServer:    "nginx",
		RedirectWWW:  true,
	}, apply)
	if !errors.Is(err, errGeneralVhostApplyRejected) {
		t.Fatalf("update error = %v, want compensated apply rejection", err)
	}
	if len(applied) != 2 || !applied[0] || applied[1] {
		t.Fatalf("applied redirect states = %#v, want [true false]", applied)
	}
	webServer, redirectWWW, _ := generalSiteState(t, p, domainID)
	if webServer != previousWebServer || redirectWWW != previousRedirectWWW {
		t.Fatalf(
			"restored state = %q/%v, want %q/%v",
			webServer, redirectWWW, previousWebServer, previousRedirectWWW,
		)
	}
}

func TestUpdateDomainGeneralSettingsRejectsUnimplementedOrDuplicateControls(t *testing.T) {
	redirectHTTPS := false
	tests := []struct {
		name   string
		mutate func(*UpdateGeneralSettingsRequest)
		want   error
	}{
		{
			name: "Apache adapter",
			mutate: func(req *UpdateGeneralSettingsRequest) {
				req.WebServer = "apache"
			},
			want: errGeneralUnsupportedWebServer,
		},
		{
			name: "foreign document root",
			mutate: func(req *UpdateGeneralSettingsRequest) {
				req.DocumentRoot = "/var/www/other/public_html"
			},
			want: errGeneralImmutableDocumentRoot,
		},
		{
			name: "HTTPS duplicate",
			mutate: func(req *UpdateGeneralSettingsRequest) {
				req.RedirectHTTPS = &redirectHTTPS
			},
			want: errGeneralHTTPSManagedBySSL,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			p, domainID := newDomainAliasFixture(t)
			beforeWebServer, beforeRedirectWWW, documentRoot := generalSiteState(t, p, domainID)
			req := UpdateGeneralSettingsRequest{
				DocumentRoot: documentRoot,
				WebServer:    "nginx",
				RedirectWWW:  true,
			}
			test.mutate(&req)
			called := false
			err := p.updateDomainGeneralSettings(
				context.Background(),
				domainID,
				req,
				func(context.Context, int) error {
					called = true
					return nil
				},
			)
			if !errors.Is(err, test.want) {
				t.Fatalf("update error = %v, want %v", err, test.want)
			}
			if called {
				t.Fatal("vhost apply ran for rejected settings")
			}
			afterWebServer, afterRedirectWWW, _ := generalSiteState(t, p, domainID)
			if afterWebServer != beforeWebServer || afterRedirectWWW != beforeRedirectWWW {
				t.Fatalf(
					"rejected settings changed state from %q/%v to %q/%v",
					beforeWebServer, beforeRedirectWWW, afterWebServer, afterRedirectWWW,
				)
			}
		})
	}
}

func TestGetDomainGeneralSettingsUsesCanonicalSSLStateAndArrayContract(t *testing.T) {
	p, domainID := newDomainAliasFixture(t)
	if _, err := p.db.GetDB().Exec(`
		UPDATE sites
		SET redirect_https = false, force_https = true
		WHERE domain_id = ?`, domainID); err != nil {
		t.Fatalf("set divergent legacy HTTPS state: %v", err)
	}

	req := httptest.NewRequest(
		http.MethodGet,
		fmt.Sprintf("/api/v1/domains/%d/general", domainID),
		nil,
	)
	rec := httptest.NewRecorder()
	p.handleDomainGeneralSettings(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rec.Code, rec.Body.String())
	}
	var settings DomainGeneralSettings
	if err := json.NewDecoder(rec.Body).Decode(&settings); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if !settings.RedirectHTTPS {
		t.Fatal("GET exposed stale redirect_https instead of canonical force_https")
	}
	if settings.Aliases == nil {
		t.Fatal("aliases encoded as null; want an empty array")
	}
	if !settings.RedirectWWWAvailable {
		t.Fatal("primary hosted domain did not expose its managed www hostname")
	}
}

func TestSupportedHostingWebServerNeverAdvertisesMissingApacheAdapter(t *testing.T) {
	tests := []struct {
		installed []string
		want      string
	}{
		{installed: nil, want: ""},
		{installed: []string{"apache"}, want: ""},
		{installed: []string{"nginx"}, want: "nginx"},
		{installed: []string{"apache", "nginx"}, want: "nginx"},
	}
	for _, test := range tests {
		if got := supportedHostingWebServer(test.installed); got != test.want {
			t.Fatalf("supportedHostingWebServer(%v) = %q, want %q", test.installed, got, test.want)
		}
	}
}
