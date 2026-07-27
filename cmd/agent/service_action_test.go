package main

import (
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServiceActionRejectsUnknownUnit(t *testing.T) {
	agent := &Agent{}
	var reply transport.ServiceActionResult
	if err := agent.ServiceAction(&transport.ServiceActionArgs{
		ServiceName: "definitely-not-a-managed-unit",
		Action:      "start",
	}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.Error != "unknown managed service" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestServiceActionRejectsInvalidAction(t *testing.T) {
	agent := &Agent{}
	var reply transport.ServiceActionResult
	if err := agent.ServiceAction(&transport.ServiceActionArgs{
		ServiceName: "nginx.service",
		Action:      "delete",
	}, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.Error != "invalid service action" {
		t.Fatalf("reply = %+v", reply)
	}
}
