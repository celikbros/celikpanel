package main

import (
	"context"
	"errors"
	"net"
	"sort"

	"github.com/alicelik/celikpanel/internal/dnswire"
)

type setupDNSWireResolver struct{ address string }

func setupDNSResolverAt(address string) hostResolver {
	return setupDNSWireResolver{address: address}
}

// A successful A answer cannot hide an unavailable AAAA answer: otherwise an
// unverified IPv6 nameserver could complete the public identity gate. Both
// queries share the caller's deadline and never fall back to hosts/NSS.
func (resolver setupDNSWireResolver) LookupHost(ctx context.Context, name string) ([]string, error) {
	type answer struct {
		values []string
		err    error
	}
	results := make(chan answer, 2)
	for _, kind := range []uint16{dnswire.TypeA, dnswire.TypeAAAA} {
		go func(kind uint16) {
			values, err := dnswire.Query(ctx, resolver.address, name, kind)
			results <- answer{values, err}
		}(kind)
	}
	values := map[string]bool{}
	var failures error
	for range 2 {
		select {
		case result := <-results:
			failures = errors.Join(failures, result.err)
			for _, value := range result.values {
				values[value] = true
			}
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if failures != nil {
		return nil, failures
	}
	if len(values) == 0 {
		return nil, &net.DNSError{Err: "public DNS has no address records", Name: name, IsNotFound: true}
	}
	out := make([]string, 0, len(values))
	for value := range values {
		out = append(out, value)
	}
	sort.Strings(out)
	return out, nil
}
