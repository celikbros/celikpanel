package main

import (
	"context"
	"net"
	"net/rpc"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/transport"
)

func TestServiceActionRejectsUnknownUnit(t *testing.T) {
	agent := &Agent{}
	var reply transport.ServiceActionResult
	if err := agent.serviceActionContext(context.Background(), "definitely-not-a-managed-unit", "start", &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.Error != "unknown managed service" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestServiceActionRejectsInvalidAction(t *testing.T) {
	agent := &Agent{}
	var reply transport.ServiceActionResult
	if err := agent.serviceActionContext(context.Background(), "nginx.service", "delete", &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Success || reply.Error != "invalid service action" {
		t.Fatalf("reply = %+v", reply)
	}
}

func TestLegacyServiceRPCMethodsAreNotExposed(t *testing.T) {
	server := rpc.NewServer()
	if err := server.RegisterName("Agent", &Agent{}); err != nil {
		t.Fatal(err)
	}

	serverConn, clientConn := net.Pipe()
	defer serverConn.Close()
	client := rpc.NewClient(clientConn)
	defer client.Close()
	go server.ServeConn(serverConn)

	tests := []struct {
		method string
		args   any
		reply  any
	}{
		{
			method: "ReloadService",
			args:   &transport.ServiceArgs{ServiceName: "nginx"},
			reply:  new(bool),
		},
		{
			method: "StartService",
			args:   &transport.ServiceArgs{ServiceName: "nginx"},
			reply:  new(bool),
		},
		{
			method: "StopService",
			args:   &transport.ServiceArgs{ServiceName: "nginx"},
			reply:  new(bool),
		},
		{
			method: "RestartService",
			args:   &transport.ServiceArgs{ServiceName: "nginx"},
			reply:  new(bool),
		},
		{
			method: "ServiceAction",
			args:   &transport.ServiceActionArgs{ServiceName: "nginx", Action: "restart"},
			reply:  new(transport.ServiceActionResult),
		},
		{
			method: "ResetFailedUnit",
			args:   &transport.ServiceArgs{ServiceName: "nginx"},
			reply:  new(bool),
		},
	}

	for _, tt := range tests {
		err := client.Call("Agent."+tt.method, tt.args, tt.reply)
		if err == nil || !strings.Contains(err.Error(), "can't find method Agent."+tt.method) {
			t.Fatalf("%s error = %v", tt.method, err)
		}
	}
}
