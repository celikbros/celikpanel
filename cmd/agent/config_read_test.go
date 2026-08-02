package main

import (
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
