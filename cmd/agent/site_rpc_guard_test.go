package main

import (
	"strings"
	"testing"
	"time"

	"github.com/alicelik/celikpanel/internal/hostingpath"
	"github.com/alicelik/celikpanel/internal/services"
	"github.com/alicelik/celikpanel/internal/transport"
)

func TestSiteMutationMutexSerializesTheSameSite(t *testing.T) {
	first := siteMutationMutex(104729)
	second := siteMutationMutex(104729)
	if first != second {
		t.Fatal("the same site ID mapped to different mutation locks")
	}

	first.Lock()
	acquired := make(chan struct{})
	go func() {
		second.Lock()
		close(acquired)
		second.Unlock()
	}()

	select {
	case <-acquired:
		first.Unlock()
		t.Fatal("a second mutation for the same site bypassed serialization")
	case <-time.After(25 * time.Millisecond):
	}

	first.Unlock()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("the serialized site mutation did not resume after unlock")
	}
}

func validCreateSiteGuardRequest(t *testing.T) transport.CreateSiteRequest {
	t.Helper()
	documentRoot, err := hostingpath.DocumentRoot(7, 11)
	if err != nil {
		t.Fatal(err)
	}
	return transport.CreateSiteRequest{
		ExpectedBuildCommit: "unknown",
		SiteID:              13,
		SubscriptionID:      7,
		DomainID:            11,
		Domain:              "example.com",
		DocumentRoot:        documentRoot,
		ProjectType:         "static",
		SSLType:             "none",
		Username:            services.SiteUsername("example.com"),
		Password:            "test-password",
	}
}

func TestCreateSiteRejectsBuildMismatchBeforeMutation(t *testing.T) {
	previousBuildCommit := buildCommit
	buildCommit = "agent-build"
	t.Cleanup(func() { buildCommit = previousBuildCommit })

	req := validCreateSiteGuardRequest(t)
	req.ExpectedBuildCommit = "panel-build"
	reply := &transport.CreateSiteResponse{}
	if err := (&Agent{}).CreateSite(req, reply); err != nil {
		t.Fatalf("CreateSite RPC error: %v", err)
	}
	if !strings.Contains(reply.ErrorMessage, "panel/agent build mismatch") {
		t.Fatalf("CreateSite error = %q, want build mismatch", reply.ErrorMessage)
	}
}

func TestCreateSiteRejectsUnsafeVhostInputsBeforeMutation(t *testing.T) {
	previousBuildCommit := buildCommit
	buildCommit = "unknown"
	t.Cleanup(func() { buildCommit = previousBuildCommit })

	tests := []struct {
		name    string
		mutate  func(*transport.CreateSiteRequest)
		wantErr string
	}{
		{
			name: "domain",
			mutate: func(req *transport.CreateSiteRequest) {
				req.Domain = "example.com;include"
			},
			wantErr: "canonical domain",
		},
		{
			name: "temporary domain",
			mutate: func(req *transport.CreateSiteRequest) {
				req.TempDomain = "temp.example.com\nserver"
			},
			wantErr: "temporary domain",
		},
		{
			name: "document root",
			mutate: func(req *transport.CreateSiteRequest) {
				req.DocumentRoot = "/tmp/tenant-controlled"
			},
			wantErr: "immutable home",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			req := validCreateSiteGuardRequest(t)
			test.mutate(&req)
			reply := &transport.CreateSiteResponse{}
			if err := (&Agent{}).CreateSite(req, reply); err != nil {
				t.Fatalf("CreateSite RPC error: %v", err)
			}
			if !strings.Contains(reply.ErrorMessage, test.wantErr) {
				t.Fatalf(
					"CreateSite error = %q, want %q",
					reply.ErrorMessage,
					test.wantErr,
				)
			}
		})
	}
}

func TestValidatedCreateSiteRequestCanonicalizesBeforeRendering(t *testing.T) {
	generator, err := services.NewNginxGenerator()
	if err != nil {
		t.Fatal(err)
	}
	agent := &Agent{nginxGen: generator}
	req := validCreateSiteGuardRequest(t)
	req.Domain = "Example.COM."
	req.TempDomain = "Preview.Example.COM."

	normalized, vhostReq, rendered, err := agent.validatedCreateSiteRequest(req)
	if err != nil {
		t.Fatalf("validatedCreateSiteRequest: %v", err)
	}
	if normalized.Domain != "example.com" ||
		normalized.TempDomain != "preview.example.com" {
		t.Fatalf(
			"normalized hostnames = %q, %q",
			normalized.Domain,
			normalized.TempDomain,
		)
	}
	if rendered.Domain != normalized.Domain ||
		vhostReq.DocumentRoot != normalized.DocumentRoot {
		t.Fatalf(
			"rendered vhost escaped validated identity: %#v %#v",
			rendered,
			vhostReq,
		)
	}
}
