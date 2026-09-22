//go:build linux

package main

import (
	"context"
	"github.com/alicelik/celikpanel/internal/transport"
)

// observeSupport has one read-only RPC. It never constructs a mutation request,
// persists a review or retries. The caller must first enforce the QEMU guard.
func observeSupport(ctx context.Context, call rpcCall) (map[string]any, error) {
	var response transport.SystemUpdateCheckResponse
	if err := call(ctx, "Agent.CheckSystemUpdate", &transport.Empty{}, &response); err != nil {
		return nil, err
	}
	return map[string]any{
		"supported": response.Supported, "available": response.Available,
		"current_version": boundedDetail(response.CurrentVersion),
		"current_commit":  boundedDetail(response.CurrentCommit),
		"detail":          boundedDetail(response.Error),
	}, nil
}
