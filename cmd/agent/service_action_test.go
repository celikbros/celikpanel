package main

import (
	"context"
	"errors"
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

func TestGenericDNSEngineActionsRejectBeforeLeaseOrSystemdResolution(t *testing.T) {
	originalResolver := serviceMutationSystemctlResolver
	resolverCalls := 0
	serviceMutationSystemctlResolver = func() (string, error) {
		resolverCalls++
		return "", errors.New("systemd resolver must not run")
	}
	t.Cleanup(func() { serviceMutationSystemctlResolver = originalResolver })

	agent := &Agent{}
	for _, test := range []struct {
		unit, action string
	}{
		{unit: "pdns.service", action: "start"},
		{unit: "bind9.service", action: "stop"},
		{unit: "named.service", action: "restart"},
		{unit: "pdns", action: "reload"},
	} {
		t.Run(test.unit+"/"+test.action, func(t *testing.T) {
			var direct transport.ServiceActionResult
			if err := agent.serviceActionContext(
				context.Background(), test.unit, test.action, &direct,
			); err != nil {
				t.Fatal(err)
			}
			if direct.Success || direct.Error != genericDNSEngineWorkflowRequired {
				t.Fatalf("direct action reply = %+v", direct)
			}

			var rpcReply transport.ServiceActionResult
			if err := agent.ServiceMutationAction(
				&ServiceMutationActionRequest{
					ServiceName: test.unit,
					Action:      test.action,
				},
				&rpcReply,
			); err != nil {
				t.Fatal(err)
			}
			if rpcReply.Success || rpcReply.Error != genericDNSEngineWorkflowRequired {
				t.Fatalf("RPC action reply = %+v", rpcReply)
			}
		})
	}

	var started bool
	if err := agent.StartServiceMutation(
		&ServiceMutationServiceRequest{ServiceName: "bind9.service"}, &started,
	); err == nil || err.Error() != genericDNSEngineWorkflowRequired || started {
		t.Fatalf("generic DNS start result = (%v, %v)", started, err)
	}
	var reset bool
	if err := agent.ResetFailedUnitMutation(
		&ServiceMutationServiceRequest{ServiceName: "pdns.service"}, &reset,
	); err == nil || err.Error() != genericDNSEngineWorkflowRequired || reset {
		t.Fatalf("generic DNS reset-failed result = (%v, %v)", reset, err)
	}
	if resolverCalls != 0 {
		t.Fatalf("generic DNS actions resolved systemd %d times", resolverCalls)
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
