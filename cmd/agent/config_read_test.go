package main

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestConfigReadRefusesArbitraryRootFiles(t *testing.T) {
	agent := &Agent{}
	for _, path := range []string{
		`/etc/shadow`,
		`/etc/celikpanel/agent.token`,
		`/var/lib/celikpanel/secret.key`,
		`/root/.ssh/authorized_keys`,
		`/etc/../root/.ssh/id_ed25519`,
	} {
		reply := &transport.ConfigResponse{}
		err := agent.GetConfig(&transport.GetConfigArgs{Path: path}, reply)
		if err != nil {
			t.Errorf(`GetConfig(%q) returned a transport error: %v`, path, err)
		}
		if reply.Error == nil || reply.Error.Code != transport.ConfigErrorPathRefused {
			t.Errorf(`GetConfig(%q) error = %#v, want typed path refusal`, path, reply.Error)
		}
		if reply.Content != `` || reply.Parsed != `` {
			t.Errorf(`GetConfig(%q) leaked a response after refusal`, path)
		}
	}
}

func TestConfigAuthorizationRefusesUnmanagedPathBeforeFilesystemInspection(t *testing.T) {
	inspectionErr := errors.New(`permission denied while traversing parent`)
	inspected := false
	path, err := configWriteAllowedFrom(`/root/.ssh/authorized_keys`, func() []string {
		return nil
	}, func(string) error {
		inspected = true
		return inspectionErr
	})
	if path != `` {
		t.Fatalf(`authorized path = %q, want empty`, path)
	}
	if inspected {
		t.Fatal(`unmanaged path reached filesystem inspection`)
	}
	if !errors.Is(err, errConfigPathRefused) {
		t.Fatalf(`error = %v, want typed path refusal`, err)
	}
	if errors.Is(err, inspectionErr) {
		t.Fatalf(`error leaked incidental filesystem failure: %v`, err)
	}
}

func TestConfigAuthorizationRefusesProtectedPathBeforeDiscovery(t *testing.T) {
	discovered := false
	inspected := false
	path, err := configWriteAllowedFrom(`/etc/shadow`, func() []string {
		discovered = true
		return []string{`/etc/shadow`}
	}, func(string) error {
		inspected = true
		return nil
	})
	if path != `` {
		t.Fatalf(`authorized path = %q, want empty`, path)
	}
	if discovered {
		t.Fatal(`protected path triggered catalogue discovery`)
	}
	if inspected {
		t.Fatal(`protected path reached filesystem inspection`)
	}
	if !errors.Is(err, errConfigPathRefused) {
		t.Fatalf(`error = %v, want typed path refusal`, err)
	}
}

func TestConfigAuthorizationInspectsManagedPathFailClosed(t *testing.T) {
	inspectionErr := errors.New(`permission denied while traversing parent`)
	const managed = `/etc/example/managed.conf`
	expected := filepath.Clean(managed)
	path, err := configWriteAllowedFrom(managed, func() []string {
		return []string{managed}
	}, func(got string) error {
		if got != expected {
			t.Fatalf(`inspected path = %q, want %q`, got, expected)
		}
		return inspectionErr
	})
	if path != `` {
		t.Fatalf(`authorized path = %q after inspection failure, want empty`, path)
	}
	if !errors.Is(err, inspectionErr) {
		t.Fatalf(`error = %v, want inspection failure`, err)
	}
}

func TestConfigUpdateReturnsTypedPathRefusal(t *testing.T) {
	agent := &Agent{}
	reply := &transport.UpdateConfigResponse{}
	if err := agent.UpdateConfig(&transport.UpdateConfigArgs{
		Path:    `/etc/shadow`,
		Content: `replacement`,
	}, reply); err != nil {
		t.Fatalf(`UpdateConfig returned a transport error: %v`, err)
	}
	if reply.Success {
		t.Fatal(`UpdateConfig reported success for a protected path`)
	}
	if reply.Error == nil || reply.Error.Code != transport.ConfigErrorPathRefused {
		t.Fatalf(`UpdateConfig error = %#v, want typed path refusal`, reply.Error)
	}
}

func TestConfigReadRequiresRequestAndResponse(t *testing.T) {
	agent := &Agent{}
	reply := &transport.ConfigResponse{}
	if err := agent.GetConfig(nil, reply); err == nil {
		t.Error(`nil request must be refused`)
	}
	if err := agent.GetConfig(&transport.GetConfigArgs{}, nil); err == nil {
		t.Error(`nil response must be refused`)
	}
}
