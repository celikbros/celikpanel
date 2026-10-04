//go:build linux

package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/transport"
	"testing"
)

func TestObserveSupportNeverMutatesOrRetries(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		calls := 0
		want := errors.New("unavailable")
		fields, err := observeSupport(context.Background(), func(_ context.Context, method string, request, reply any) error {
			calls++
			if method != "Agent.CheckSystemUpdate" {
				t.Fatalf("unexpected RPC %q", method)
			}
			if _, ok := request.(*transport.Empty); !ok {
				t.Fatal("nonempty request")
			}
			if unavailable {
				return want
			}
			*reply.(*transport.SystemUpdateCheckResponse) = transport.SystemUpdateCheckResponse{Supported: true, CurrentVersion: "fixture", CurrentCommit: "commit", Error: "line\ncontrol\x00", TargetVersion: "not exported"}
			return nil
		})
		if calls != 1 {
			t.Fatalf("calls=%d", calls)
		}
		if unavailable {
			if !errors.Is(err, want) || fields != nil {
				t.Fatalf("unknown became result: %v %v", fields, err)
			}
		} else if err != nil || len(fields) != 5 || fields["supported"] != true || fields["detail"] != "linecontrol" {
			t.Fatalf("unexpected bounded observation: %v %v", fields, err)
		}
	}
}
