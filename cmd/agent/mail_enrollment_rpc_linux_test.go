//go:build linux

package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/recoveryruntime"
	"github.com/alicelik/celikpanel/internal/servicemutationledger"
	"github.com/alicelik/celikpanel/internal/transport"
)

type enrollmentRPCProof struct {
	commit, digest  string
	changed, closed bool
}

func (p *enrollmentRPCProof) Revalidate() error {
	if p.changed {
		return errors.New("private source detail")
	}
	return nil
}
func (p *enrollmentRPCProof) AgentIdentity() (string, string) { return p.commit, p.digest }
func (p *enrollmentRPCProof) Close()                          { p.closed = true }
func enrollmentRPCRequest() transport.MailEnrollmentStartRequest {
	return transport.MailEnrollmentStartRequest{MailEnrollmentRequest: transport.MailEnrollmentRequest{RequestID: strings.Repeat("a", 32), OwnerID: strings.Repeat("b", 32), Generation: strings.Repeat("c", 64)}, ExpectedBuildCommit: strings.Repeat("d", 40)}
}
func TestMailEnrollmentRPCDispatchIsBoundedToAcceptedIdentityAndSource(t *testing.T) {
	for _, scenario := range []string{"accepted", "lost-reply", "wrong-request", "wrong-owner", "wrong-generation", "reviewed-generation", "wrong-build", "development-build", "wrong-running-agent", "changed-source", "source-unavailable", "evidence-unavailable", "wrong-path", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			req := enrollmentRPCRequest()
			commit := req.ExpectedBuildCommit
			proof := &enrollmentRPCProof{commit: commit, digest: strings.Repeat("e", 64)}
			inspected, observed, started := 0, 0, 0
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "wrong-request":
				req.RequestID = "../request"
			case "wrong-owner":
				req.OwnerID = ""
			case "wrong-generation":
				req.Generation = ""
			case "wrong-build":
				req.ExpectedBuildCommit = strings.Repeat("f", 40)
			case "development-build":
				commit = "unknown"
				req.ExpectedBuildCommit = commit
			case "wrong-running-agent":
				proof.digest = strings.Repeat("f", 64)
			case "changed-source":
				proof.changed = true
			case "cancelled":
				cancel()
			}
			inspect := func() (mailEnrollmentRPCInspection, error) {
				inspected++
				if scenario == "source-unavailable" {
					return mailEnrollmentRPCInspection{}, errors.New("secret diagnostic")
				}
				path := filepath.Join(mailrenewalkit.InstalledRoot, req.Generation, mailrenewalkit.BinaryName)
				if scenario == "wrong-path" {
					path = "/opt/celikpanel/bin/agent"
				}
				generation := req.Generation
				if scenario == "reviewed-generation" {
					generation = strings.Repeat("f", 64)
				}
				return mailEnrollmentRPCInspection{proof, path, generation, strings.Repeat("e", 64)}, nil
			}
			observe := func(_ context.Context, r *transport.MailEnrollmentRequest) (recoveryruntime.MailEnrollmentObservation, error) {
				observed++
				if *r != req.MailEnrollmentRequest {
					t.Fatal("different accepted identity")
				}
				if scenario == "evidence-unavailable" {
					return recoveryruntime.MailEnrollmentObservation{}, errors.New("secret diagnostic")
				}
				return recoveryruntime.MailEnrollmentObservation{}, nil
			}
			launch := func(_ context.Context, path string, args []string) error {
				started++
				if path != filepath.Join(mailrenewalkit.InstalledRoot, req.Generation, mailrenewalkit.BinaryName) || strings.Join(args, "|") != strings.Join([]string{req.RequestID, req.OwnerID, req.Generation}, "|") {
					t.Fatal("expanded worker authority")
				}
				if scenario == "lost-reply" {
					return errors.New("secret diagnostic")
				}
				return nil
			}
			var out transport.MailEnrollmentStartResponse
			err := startMailEnrollmentRPC(ctx, &req, &out, commit, inspect, observe, launch)
			good := scenario == "accepted" || scenario == "lost-reply"
			if (err == nil) != good || started != map[bool]int{true: 1, false: 0}[good] {
				t.Fatalf("err=%v starts=%d", err, started)
			}
			if good && (observed != 1 || inspected != 1 || !proof.closed) {
				t.Fatal("missing proof or close")
			}
			if err != nil && strings.Contains(err.Error(), "secret") {
				t.Fatal("private detail leaked")
			}
			if scenario == "lost-reply" && (out.Handoff != "unknown" || out.Reason != "mail_enrollment_handoff_unknown") {
				t.Fatalf("lost reply: %+v", out)
			}
			if scenario == "accepted" && (out.Handoff != "accepted" || out.RequestID != req.RequestID) {
				t.Fatalf("handoff: %+v", out)
			}
		})
	}
}
func TestMailEnrollmentRPCStatusNeverDispatchesOrConvertsUnknown(t *testing.T) {
	for _, state := range []string{"forward", "rollback", "published", "restored", "not_recorded", "unknown", "conflict", "invalid-state"} {
		req := enrollmentRPCRequest().MailEnrollmentRequest
		observe := func(context.Context, *transport.MailEnrollmentRequest) (recoveryruntime.MailEnrollmentObservation, error) {
			if state == "unknown" {
				return recoveryruntime.MailEnrollmentObservation{}, errors.New("secret detail")
			}
			if state == "not_recorded" {
				return recoveryruntime.MailEnrollmentObservation{}, nil
			}
			id := servicemutationledger.MailEnrollmentIdentity{RequestID: req.RequestID, OwnerID: req.OwnerID, ScopeSHA256: strings.Repeat("f", 64)}
			if state == "conflict" {
				id.OwnerID = strings.Repeat("f", 32)
			}
			return recoveryruntime.MailEnrollmentObservation{Found: true, Identity: id, Generation: req.Generation, State: state}, nil
		}
		var out transport.MailEnrollmentStatusResponse
		if err := mailEnrollmentStatusRPC(&req, &out, observe); err != nil {
			t.Fatal(err)
		}
		want := state
		if state == "conflict" || state == "invalid-state" {
			want = "unknown"
		}
		if out.State != want || out.MailEnrollmentRequest != req || out.ObservedAt.IsZero() || strings.Contains(out.Reason, "secret") {
			t.Fatalf("%s: %+v", state, out)
		}
	}
}
