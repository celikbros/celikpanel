package main

import (
	"context"
	"testing"
)

type ServiceOperationRoundcubeResponse struct {
	Installed bool
	Version   string
	Error     string
}

type ServiceOperationWebmailResponse struct {
	Configured bool
	Present    bool
	Error      string
}

type ServiceOperationDBToolsResponse struct {
	Configured bool
	Tools      []string
	Error      string
}

type ServiceOperationRepoRequest struct {
	RepoID string
}

type ServiceOperationRepoResponse struct {
	Enabled bool
	Source  string
	Error   string
}

func (a *serviceOperationTestAgent) InstallRoundcube(
	_ *struct{},
	resp *ServiceOperationRoundcubeResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.installed["roundcube"] = true
	resp.Installed = !a.installNoop
	resp.Version = "test"
	return nil
}

func (a *serviceOperationTestAgent) ConfigureWebmail(
	_ *struct{},
	resp *ServiceOperationWebmailResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	resp.Configured = true
	resp.Present = !a.installNoop
	return nil
}

func (a *serviceOperationTestAgent) ConfigureDBTools(
	_ *struct{},
	resp *ServiceOperationDBToolsResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	resp.Configured = true
	if !a.installNoop {
		resp.Tools = []string{"phpmyadmin", "phppgadmin"}
	}
	return nil
}

func (a *serviceOperationTestAgent) RepoStatus(
	_ *ServiceOperationRepoRequest,
	resp *ServiceOperationRepoResponse,
) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	resp.Enabled = !a.installNoop
	return nil
}

func seedInstalledServices(agent *serviceOperationTestAgent, ids ...string) {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	for _, id := range ids {
		agent.installed[id] = true
	}
}

func installedForTest(agent *serviceOperationTestAgent, id string) bool {
	agent.mu.Lock()
	defer agent.mu.Unlock()
	return agent.installed[id]
}

func TestServiceInstallPreflightFailsBeforeAgentMutation(t *testing.T) {
	tests := []struct {
		name      string
		serviceID string
		setup     func(*serviceOperationTestAgent)
	}{
		{
			name:      "explicitly unsupported integration",
			serviceID: "apache",
		},
		{
			name:      "missing prerequisite",
			serviceID: "phpmyadmin",
		},
		{
			name:      "exclusive seat occupied",
			serviceID: "valkey",
			setup: func(agent *serviceOperationTestAgent) {
				seedInstalledServices(agent, "redis")
			},
		},
		{
			name:      "required repository disabled",
			serviceID: "netdata",
			setup: func(agent *serviceOperationTestAgent) {
				agent.installNoop = true
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newServiceOperationTestFixture(t)
			if test.setup != nil {
				test.setup(fixture.agent)
			}
			result, failure := fixture.panel.runServiceInstall(
				context.Background(),
				serviceInstallRequest{ServiceID: test.serviceID},
				func(string) error { return nil },
			)
			if failure == nil || result["success"] != false {
				t.Fatalf("result=%v failure=%+v", result, failure)
			}
			if installedForTest(fixture.agent, test.serviceID) {
				t.Fatalf("%s reached the mutating install RPC", test.serviceID)
			}
		})
	}
}

func TestNodeInstallRequiresWebServerBeforeMutation(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	result, failure := fixture.panel.runNodeInstall(
		context.Background(), "22.4.1", func(string) error { return nil },
	)
	if failure == nil || result["success"] != false {
		t.Fatalf("result=%v failure=%+v", result, failure)
	}
	fixture.agent.mu.Lock()
	installed := fixture.agent.nodeVersions["22.4.1"]
	fixture.agent.mu.Unlock()
	if installed {
		t.Fatal("Node install RPC ran without a web server")
	}
}

func TestRoundcubePreflightAndConfigurationConfirmation(t *testing.T) {
	t.Run("SMTP is required before mutation", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		seedInstalledServices(fixture.agent, "nginx", "php-fpm", "dovecot")
		result, failure := fixture.panel.runServiceInstall(
			context.Background(), serviceInstallRequest{ServiceID: "roundcube"}, func(string) error { return nil },
		)
		if failure == nil || result["success"] != false {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
		if installedForTest(fixture.agent, "roundcube") {
			t.Fatal("Roundcube install RPC ran without SMTP")
		}
	})

	t.Run("configured but absent is failure", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		seedInstalledServices(fixture.agent, "nginx", "php-fpm", "dovecot", "postfix")
		fixture.agent.installNoop = true
		result, failure := fixture.panel.runServiceInstall(
			context.Background(), serviceInstallRequest{ServiceID: "roundcube"}, func(string) error { return nil },
		)
		if failure == nil || result["success"] != false {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
	})

	t.Run("presence and readiness confirmed", func(t *testing.T) {
		fixture := newServiceOperationTestFixture(t)
		seedInstalledServices(fixture.agent, "nginx", "php-fpm", "dovecot", "postfix")
		result, failure := fixture.panel.runServiceInstall(
			context.Background(), serviceInstallRequest{ServiceID: "roundcube"}, func(string) error { return nil },
		)
		if failure != nil || result["success"] != true || result["installed"] != true {
			t.Fatalf("result=%v failure=%+v", result, failure)
		}
	})
}

func TestDatabaseToolMustBeNamedInConfigurationResponse(t *testing.T) {
	fixture := newServiceOperationTestFixture(t)
	seedInstalledServices(fixture.agent, "nginx", "php-fpm", "mariadb")
	fixture.agent.installNoop = true
	result, failure := fixture.panel.runServiceInstall(
		context.Background(), serviceInstallRequest{ServiceID: "phpmyadmin"}, func(string) error { return nil },
	)
	if failure == nil || result["success"] != false {
		t.Fatalf("result=%v failure=%+v", result, failure)
	}
}
