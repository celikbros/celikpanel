//go:build linux

package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/mailrenewalkit"
	"github.com/alicelik/celikpanel/internal/transport"
	"path/filepath"
	"strings"
	"testing"
)

func TestMailEnrollmentRPCSourceReportsOnlyPinnedReleasedGeneration(t *testing.T) {
	for _, scenario := range []string{"verified", "missing", "partial-error", "nil-source", "wrong-build", "development", "foreign-declaration", "wrong-running-bytes", "invalid-digest", "invalid-generation", "wrong-path", "changed", "cancelled", "cancel-after-read"} {
		t.Run(scenario, func(t *testing.T) {
			commit := strings.Repeat("a", 40)
			generation := strings.Repeat("b", 64)
			digest := strings.Repeat("c", 64)
			req := transport.MailEnrollmentSourceRequest{ExpectedBuildCommit: commit}
			source := &enrollmentRPCProof{commit: commit, digest: digest}
			proof := mailEnrollmentRPCInspection{source, filepath.Join(mailrenewalkit.InstalledRoot, generation, mailrenewalkit.BinaryName), generation, digest}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch scenario {
			case "wrong-build":
				req.ExpectedBuildCommit = strings.Repeat("f", 40)
			case "development":
				commit = "unknown"
				req.ExpectedBuildCommit = commit
			case "foreign-declaration":
				source.commit = strings.Repeat("f", 40)
			case "wrong-running-bytes":
				proof.runningDigest = strings.Repeat("f", 64)
			case "invalid-digest":
				source.digest = "bad"
				proof.runningDigest = "bad"
			case "invalid-generation":
				proof.generation = "../kit"
			case "wrong-path":
				proof.helper = "/opt/celikpanel/bin/agent"
			case "changed":
				source.changed = true
			case "cancelled":
				cancel()
			}
			inspected := false
			out := transport.MailEnrollmentSourceResponse{State: "verified", Generation: generation, BuildCommit: commit}
			err := inspectMailEnrollmentSourceRPC(ctx, &req, &out, commit, func() (mailEnrollmentRPCInspection, error) {
				inspected = true
				switch scenario {
				case "missing":
					return mailEnrollmentRPCInspection{}, errors.New("private source location")
				case "partial-error":
					return proof, errors.New("private source detail")
				case "nil-source":
					return mailEnrollmentRPCInspection{}, nil
				case "cancel-after-read":
					cancel()
				}
				return proof, nil
			})
			if scenario == "verified" {
				if err != nil || out.State != "verified" || out.Generation != generation || out.BuildCommit != commit || out.ObservedAt.IsZero() || out.Reason != "" {
					t.Fatalf("%+v %v", out, err)
				}
			} else {
				if out.State != "unknown" || out.Generation != "" || out.BuildCommit != "" || !out.ObservedAt.IsZero() {
					t.Fatalf("unverified source became ready: %+v", out)
				}
				if err != nil && strings.Contains(err.Error(), "private") {
					t.Fatal("private details leaked")
				}
			}
			early := scenario == "wrong-build" || scenario == "development" || scenario == "cancelled"
			if inspected == early {
				t.Fatal("incorrect inspection boundary")
			}
			if inspected && scenario != "missing" && scenario != "nil-source" && !source.closed {
				t.Fatal("pinned source leaked")
			}
		})
	}
}
