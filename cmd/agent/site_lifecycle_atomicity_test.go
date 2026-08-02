package main

import (
	"errors"
	"os"
	"os/user"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

type fakeSiteLifecycle struct {
	home                     string
	userExists               bool
	userHome                 string
	calls                    []string
	failures                 map[string]error
	expectedSocket           string
	userCreatedOnCreateError bool
}

func (fake *fakeSiteLifecycle) call(name string) error {
	fake.calls = append(fake.calls, name)
	return fake.failures[name]
}

func (fake *fakeSiteLifecycle) operations() *siteLifecycleOps {
	return &siteLifecycleOps{
		prepareChallengeRoot: func(*ApplyVhostRequest) error {
			return fake.call("prepare-challenge")
		},
		pathExists: func(string) (bool, error) {
			if err := fake.call("path-exists"); err != nil {
				return false, err
			}
			return false, nil
		},
		mkdirAll: func(string, os.FileMode) error {
			return fake.call("mkdir-all")
		},
		lookupUser: func(name string) (*user.User, error) {
			fake.calls = append(fake.calls, "lookup-user")
			if err := fake.failures["lookup-user"]; err != nil {
				return nil, err
			}
			if !fake.userExists {
				return nil, user.UnknownUserError(name)
			}
			home := fake.userHome
			if home == "" {
				home = fake.home
			}
			return &user.User{Username: name, Uid: "2001", HomeDir: home}, nil
		},
		createUser: func(string, string, string) (bool, error) {
			if err := fake.call("create-user"); err != nil {
				if fake.userCreatedOnCreateError {
					fake.userExists = true
				}
				return fake.userCreatedOnCreateError, err
			}
			fake.userExists = true
			return true, nil
		},
		deleteUser: func(string) error {
			if err := fake.call("delete-user"); err != nil {
				return err
			}
			fake.userExists = false
			return nil
		},
		killUser: func(string) error {
			return fake.call("kill-user")
		},
		setOwnership: func(string, string) error {
			return fake.call("set-ownership")
		},
		createPool: func(int, string, string) (string, error) {
			if err := fake.call("create-pool"); err != nil {
				return "", err
			}
			return fake.expectedSocket, nil
		},
		deletePool: func(int, string) error {
			return fake.call("delete-pool")
		},
		writeFileExclusive: func(string, []byte, os.FileMode) error {
			return fake.call("write-placeholder")
		},
		applyLayout: func(string, string) error {
			return fake.call("apply-layout")
		},
		applyVhost: func(string, string) error {
			return fake.call("apply-vhost")
		},
		removeVhost: func(string) error {
			return fake.call("remove-vhost")
		},
		removeAppUnit: func(int) error {
			return fake.call("remove-app-unit")
		},
		removeAll: func(string) error {
			return fake.call("remove-all")
		},
	}
}

func lifecycleTestAgent(t *testing.T, fake *fakeSiteLifecycle) *Agent {
	t.Helper()
	generator, err := services.NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	return &Agent{nginxGen: generator, siteOps: fake.operations()}
}

func withLifecycleTestBuild(t *testing.T) {
	t.Helper()
	previous := buildCommit
	buildCommit = "lifecycle-test"
	t.Cleanup(func() { buildCommit = previous })
}

func validLifecycleCreateRequest(t *testing.T) transport.CreateSiteRequest {
	t.Helper()
	documentRoot, err := hostingpath.DocumentRoot(7, 11)
	if err != nil {
		t.Fatal(err)
	}
	return transport.CreateSiteRequest{
		ExpectedBuildCommit: "lifecycle-test",
		SiteID:              13,
		SubscriptionID:      7,
		DomainID:            11,
		Domain:              "example.test",
		DocumentRoot:        documentRoot,
		ProjectType:         "php",
		PHPVersion:          "8.3",
		SSLType:             "none",
		Username:            services.SiteUsername("example.test"),
		Password:            "test-password",
	}
}

func validLifecycleDeleteRequest(t *testing.T) DeleteSiteRequest {
	t.Helper()
	home, err := hostingpath.SiteHome(7, 11)
	if err != nil {
		t.Fatal(err)
	}
	return DeleteSiteRequest{
		ExpectedBuildCommit: "lifecycle-test",
		SiteID:              13,
		SubscriptionID:      7,
		DomainID:            11,
		Domain:              "example.test",
		Username:            services.SiteUsername("example.test"),
		PHPVersion:          "8.3",
		SiteHome:            home,
	}
}

func TestCreateSiteRefusesAnExistingSystemUserBeforeMutation(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{
		home:       home,
		userExists: true,
		failures:   map[string]error{},
	}
	reply := &transport.CreateSiteResponse{}
	if err := lifecycleTestAgent(t, fake).CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || !strings.Contains(reply.ErrorMessage, "system user already exists") {
		t.Fatalf("CreateSite reply = %#v", reply)
	}
	if strings.Contains(strings.Join(fake.calls, ","), "create-user") {
		t.Fatalf("existing user was mutated: %v", fake.calls)
	}
}

func TestCreateSiteRollsBackEveryPossibleResourceOnVhostFailure(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{
		home:           home,
		failures:       map[string]error{"apply-vhost": errors.New("sensitive /etc/nginx detail")},
		expectedSocket: "/var/run/php/php8.3-fpm-site13.sock",
	}
	reply := &transport.CreateSiteResponse{}
	if err := lifecycleTestAgent(t, fake).CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success {
		t.Fatal("failed nginx activation reported success")
	}
	if strings.Contains(reply.ErrorMessage, "/etc/nginx") {
		t.Fatalf("privileged detail leaked to client: %q", reply.ErrorMessage)
	}
	joined := strings.Join(fake.calls, ",")
	for _, required := range []string{"delete-pool", "kill-user", "delete-user", "remove-all"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("missing rollback %q in %v", required, fake.calls)
		}
	}
}

func TestCreateSiteDoesNotDeleteAnUncreatedUserAfterAtomicUseraddFailure(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{
		home:     home,
		failures: map[string]error{"create-user": errors.New("useradd collision detail")},
	}
	reply := &transport.CreateSiteResponse{}
	if err := lifecycleTestAgent(t, fake).CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || !strings.Contains(reply.ErrorMessage, "system user creation") {
		t.Fatalf("CreateSite reply = %#v", reply)
	}
	joined := strings.Join(fake.calls, ",")
	if strings.Contains(joined, "delete-user") || strings.Contains(joined, "kill-user") {
		t.Fatalf("an uncreated user was touched during rollback: %v", fake.calls)
	}
	if !strings.Contains(joined, "remove-all") {
		t.Fatalf("created site home was not rolled back: %v", fake.calls)
	}
}

func TestCreateSiteRollsBackAUserCreatedBeforePasswordFailure(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{
		home:                     home,
		userCreatedOnCreateError: true,
		failures:                 map[string]error{"create-user": errors.New("password setup detail")},
	}
	reply := &transport.CreateSiteResponse{}
	if err := lifecycleTestAgent(t, fake).CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success {
		t.Fatalf("CreateSite reply = %#v", reply)
	}
	joined := strings.Join(fake.calls, ",")
	for _, required := range []string{"kill-user", "delete-user", "remove-all"} {
		if !strings.Contains(joined, required) {
			t.Fatalf("partially created user rollback missed %q: %v", required, fake.calls)
		}
	}
}

func TestCreateSiteReportsIncompleteRollbackWithoutLeakingDetail(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleCreateRequest(t)
	home, _ := hostingpath.SiteHome(req.SubscriptionID, req.DomainID)
	fake := &fakeSiteLifecycle{
		home: home,
		failures: map[string]error{
			"create-pool": errors.New("pool create secret"),
			"delete-pool": errors.New("pool rollback secret"),
		},
		expectedSocket: "/var/run/php/php8.3-fpm-site13.sock",
	}
	reply := &transport.CreateSiteResponse{}
	if err := lifecycleTestAgent(t, fake).CreateSite(req, reply); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reply.ErrorMessage, "automatic rollback is incomplete") {
		t.Fatalf("CreateSite error = %q", reply.ErrorMessage)
	}
	if strings.Contains(reply.ErrorMessage, "secret") {
		t.Fatalf("rollback detail leaked to client: %q", reply.ErrorMessage)
	}
}

func TestDeleteSiteNeverReportsSuccessAfterPartialCleanup(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleDeleteRequest(t)
	fake := &fakeSiteLifecycle{
		home:     req.SiteHome,
		failures: map[string]error{"remove-app-unit": errors.New("systemd private detail")},
	}
	resp := &DeleteSiteResponse{}
	if err := lifecycleTestAgent(t, fake).DeleteSite(&req, resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.Contains(resp.Error, "application unit") {
		t.Fatalf("DeleteSite response = %#v", resp)
	}
	if strings.Contains(resp.Error, "private detail") {
		t.Fatalf("privileged detail leaked to client: %q", resp.Error)
	}
	if !strings.Contains(strings.Join(fake.calls, ","), "remove-all") {
		t.Fatalf("retry-friendly cleanup did not continue: %v", fake.calls)
	}
}

func TestDeleteSiteStopsBeforeTenantDestructionWhenVhostRemovalFails(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleDeleteRequest(t)
	fake := &fakeSiteLifecycle{
		home:     req.SiteHome,
		failures: map[string]error{"remove-vhost": errors.New("nginx private detail")},
	}
	resp := &DeleteSiteResponse{}
	if err := lifecycleTestAgent(t, fake).DeleteSite(&req, resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || resp.Error != "site cleanup incomplete: nginx vhost" {
		t.Fatalf("DeleteSite response = %#v", resp)
	}
	if strings.Contains(resp.Error, "private detail") {
		t.Fatalf("privileged detail leaked to client: %q", resp.Error)
	}
	joined := strings.Join(fake.calls, ",")
	for _, forbidden := range []string{"remove-app-unit", "delete-pool", "delete-user", "remove-all"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("tenant resource %q was mutated behind a live vhost: %v", forbidden, fake.calls)
		}
	}
}

func TestDeleteSiteRefusesMismatchedUserHome(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleDeleteRequest(t)
	fake := &fakeSiteLifecycle{
		home:       req.SiteHome,
		userExists: true,
		userHome:   "/var/www/celikpanel/subscriptions/999/sites/999",
		failures:   map[string]error{},
	}
	resp := &DeleteSiteResponse{}
	if err := lifecycleTestAgent(t, fake).DeleteSite(&req, resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.Contains(resp.Error, "system user") {
		t.Fatalf("DeleteSite response = %#v", resp)
	}
	if strings.Contains(strings.Join(fake.calls, ","), "delete-user") {
		t.Fatalf("mismatched user was deleted: %v", fake.calls)
	}
}

func TestDeleteSiteRejectsMismatchedImmutableHomeBeforeMutation(t *testing.T) {
	withLifecycleTestBuild(t)
	req := validLifecycleDeleteRequest(t)
	req.SiteHome = "/var/www/celikpanel/subscriptions/7/sites/12"
	fake := &fakeSiteLifecycle{home: req.SiteHome, failures: map[string]error{}}
	resp := &DeleteSiteResponse{}
	if err := lifecycleTestAgent(t, fake).DeleteSite(&req, resp); err != nil {
		t.Fatal(err)
	}
	if resp.Success || !strings.Contains(resp.Error, "immutable hosting identity") {
		t.Fatalf("DeleteSite response = %#v", resp)
	}
	if len(fake.calls) != 0 {
		t.Fatalf("mismatched identity reached privileged operations: %v", fake.calls)
	}
}
