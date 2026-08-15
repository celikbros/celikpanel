package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicelik/celikpanel/internal/core"
)

func clearMailProfileDNSIdentity(t *testing.T, fixture serviceOperationTestFixture) {
	t.Helper()
	if _, err := fixture.database.GetDB().Exec(`
		DELETE FROM panel_settings
		WHERE key IN (?, ?, ?, ?, ?)`,
		settingNS1, settingNS2, settingDNSRole, settingDNSPeerIP, settingDNSPeerNS,
	); err != nil {
		t.Fatalf("clear mail profile DNS identity: %v", err)
	}
}

func removeMailProfileDNSServers(agent *mailProfileTestAgent) {
	agent.serviceOperationTestAgent.mu.Lock()
	defer agent.serviceOperationTestAgent.mu.Unlock()
	delete(agent.serviceOperationTestAgent.installed, "pdns")
	delete(agent.serviceOperationTestAgent.installed, "bind")
}

func TestMailProfileDNSIdentityReadinessRequiresServerAndSavedIdentity(t *testing.T) {
	fixture, agent := newMailProfileTestFixture(t)
	ready, err := fixture.panel.mailProfileDNSIdentityReady(context.Background())
	if err != nil || !ready {
		t.Fatalf("seeded DNS identity ready=%v err=%v", ready, err)
	}

	removeMailProfileDNSServers(agent)
	ready, err = fixture.panel.mailProfileDNSIdentityReady(context.Background())
	if err != nil || ready {
		t.Fatalf("identity without installed DNS server ready=%v err=%v", ready, err)
	}

	seedInstalledServices(agent.serviceOperationTestAgent, "bind")
	ready, err = fixture.panel.mailProfileDNSIdentityReady(context.Background())
	if err != nil || !ready {
		t.Fatalf("BIND-backed DNS identity ready=%v err=%v", ready, err)
	}

	clearMailProfileDNSIdentity(t, fixture)
	ready, err = fixture.panel.mailProfileDNSIdentityReady(context.Background())
	if err != nil || ready {
		t.Fatalf("installed DNS server without saved identity ready=%v err=%v", ready, err)
	}
}

func TestMailProfileDNSIdentityPreflightFailsBeforePackageMutation(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*testing.T, serviceOperationTestFixture, *mailProfileTestAgent)
	}{
		{
			name: "supported DNS server missing",
			setup: func(_ *testing.T, _ serviceOperationTestFixture, agent *mailProfileTestAgent) {
				removeMailProfileDNSServers(agent)
			},
		},
		{
			name: "saved DNS identity missing",
			setup: func(t *testing.T, fixture serviceOperationTestFixture, _ *mailProfileTestAgent) {
				clearMailProfileDNSIdentity(t, fixture)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture, agent := newMailProfileTestFixture(t)
			test.setup(t, fixture, agent)
			profile, _ := mailProfileByID(core.MailProfileCore)
			if _, err := fixture.panel.preflightMailProfileInstall(
				serviceOperationBoundContext(), profile,
			); !errors.Is(err, errMailProfileDNSIdentityNotReady) {
				t.Fatalf("preflight error=%v, want DNS identity category", err)
			}

			recorder, queued := postMailProfile(
				t, fixture, core.MailProfileCore, mustServiceOperationRequestID(t),
			)
			if recorder.Code != http.StatusAccepted || queued == nil {
				t.Fatalf("profile status=%d operation=%+v body=%s", recorder.Code, queued, recorder.Body.String())
			}
			failed, body := waitForServiceOperation(
				t, fixture.panel, fixture.userID, queued.ID, serviceOperationFailed,
			)
			if failed.Error == nil ||
				failed.Error.Code != errCodeMailProfileDNSIdentityNotReady ||
				failed.Error.Message != mailProfileDNSIdentityMessage {
				t.Fatalf("failed profile=%+v body=%s", failed, body)
			}
			if agent.installCalls.Load() != 0 {
				t.Fatal("DNS identity preflight failure reached InstallService")
			}
		})
	}
}

func TestManagedServicesPayloadPublishesStrictDNSIdentityAndBlocksProfiles(t *testing.T) {
	fixture, _ := newMailProfileTestFixture(t)
	if _, err := fixture.panel.scanManagedServices(context.Background()); err != nil {
		t.Fatalf("seed managed-service scan: %v", err)
	}

	readPayload := func() (managedServicesPayload, map[string]json.RawMessage) {
		recorder := httptest.NewRecorder()
		fixture.panel.handleManagedServices(
			recorder,
			httptest.NewRequest(http.MethodGet, "/api/v1/managed-services", nil),
		)
		if recorder.Code != http.StatusOK {
			t.Fatalf("managed-services status=%d body=%s", recorder.Code, recorder.Body.String())
		}
		var payload managedServicesPayload
		if err := json.Unmarshal(recorder.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode managed-services payload: %v", err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(recorder.Body.Bytes(), &fields); err != nil {
			t.Fatalf("decode managed-services fields: %v", err)
		}
		return payload, fields
	}

	payload, fields := readPayload()
	if _, ok := fields["dns_identity_ready"]; !ok {
		t.Fatal("managed-services omitted dns_identity_ready")
	}
	if !payload.DNSIdentityReady {
		t.Fatal("ready DNS identity reported false")
	}
	profile := profileViewByID(t, payload.Profiles, core.MailProfileCore)
	if !profile.Available || profile.Status == mailProfileStatusBlocked {
		t.Fatalf("ready DNS profile=%+v", profile)
	}

	clearMailProfileDNSIdentity(t, fixture)
	payload, fields = readPayload()
	if _, ok := fields["dns_identity_ready"]; !ok {
		t.Fatal("false dns_identity_ready was omitted")
	}
	if payload.DNSIdentityReady {
		t.Fatal("missing DNS identity reported ready")
	}
	profile = profileViewByID(t, payload.Profiles, core.MailProfileCore)
	if profile.Available || profile.Status != mailProfileStatusBlocked ||
		profile.BlockedReason != mailProfileDNSIdentityMessage {
		t.Fatalf("DNS-blocked profile=%+v", profile)
	}
}
