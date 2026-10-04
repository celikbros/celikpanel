package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
)

// A paired secondary reads a catalog that its primary produced. CelikPanel
// primaries use two producers: BIND serves the rendered RFC 9432 zone (TTL 60,
// SHA-224 member labels) and PowerDNS serves its native producer catalog
// (TTL 0 version/member records, base32hex member labels). The secondary
// cannot know the peer's engine without a remote panel (D-022), so every read
// of the peer's catalog accepts exactly one of the two known formats.
//
// Reads of the local engine's own catalog, and of the copy the peer serves of
// the local engine's catalog, keep their explicit producer: the Agent knows
// which producer it runs.

// errDNSPeerCatalogProducerChanged is returned when one operation reads the
// same peer catalog twice and the accepted format differs.
var errDNSPeerCatalogProducerChanged = errors.New(
	"peer catalog producer changed during the operation",
)

// dnsPeerCatalogFormatError names both refusals when the peer's catalog is in
// neither known format. It carries only fixed parser text, never peer bytes.
type dnsPeerCatalogFormatError struct {
	catalog    string
	bindReason error
	pdnsReason error
}

func (err *dnsPeerCatalogFormatError) Error() string {
	reason := func(value error) string {
		return strings.TrimPrefix(value.Error(), "BIND catalog AXFR ")
	}
	return fmt.Sprintf(
		"the paired primary's catalog %s matches neither supported catalog format (BIND format: %s; PowerDNS format: %s)",
		err.catalog, reason(err.bindReason), reason(err.pdnsReason),
	)
}

type dnsPeerCatalogSessionKey struct{}

// dnsPeerCatalogSession pins the accepted producer of each peer catalog for
// one operation. It lives only in the operation's context and is never
// persisted in a journal, ledger or state receipt.
type dnsPeerCatalogSession struct {
	operation string
	mu        sync.Mutex
	producers map[string]dnsCatalogAXFRProducer
}

// withDNSPeerCatalogSession starts the producer pin for one operation. A
// nested call keeps the outer operation's session.
func withDNSPeerCatalogSession(ctx context.Context, operation string) context.Context {
	if _, ok := ctx.Value(dnsPeerCatalogSessionKey{}).(*dnsPeerCatalogSession); ok {
		return ctx
	}
	return context.WithValue(ctx, dnsPeerCatalogSessionKey{}, &dnsPeerCatalogSession{
		operation: operation,
		producers: make(map[string]dnsCatalogAXFRProducer),
	})
}

func dnsPeerCatalogSessionFrom(ctx context.Context) *dnsPeerCatalogSession {
	if session, ok := ctx.Value(dnsPeerCatalogSessionKey{}).(*dnsPeerCatalogSession); ok {
		return session
	}
	// A read outside a declared operation is its own operation.
	return &dnsPeerCatalogSession{
		operation: "DNS peer catalog check",
		producers: make(map[string]dnsCatalogAXFRProducer),
	}
}

var logDNSPeerCatalogProducer = func(format string, args ...any) {
	log.Printf(format, args...)
}

func (session *dnsPeerCatalogSession) accept(
	address, catalog string, producer dnsCatalogAXFRProducer,
) error {
	session.mu.Lock()
	defer session.mu.Unlock()
	previous, seen := session.producers[catalog]
	if seen {
		if previous != producer {
			return fmt.Errorf(
				"%w: catalog %s was first read in the %s format and now in the %s format; check which DNS engine serves the paired primary, then retry the same operation",
				errDNSPeerCatalogProducerChanged, catalog, previous, producer,
			)
		}
		return nil
	}
	session.producers[catalog] = producer
	logDNSPeerCatalogProducer(
		"%s: the paired primary at %s serves catalog %s in the %s catalog format; this operation reads it in that format",
		session.operation, address, catalog, producer,
	)
	return nil
}

type dnsPeerCatalogRead func(
	context.Context, dnsCatalogAXFRProducer,
) (dnsCatalogAXFRResult, error)

// selectDNSPeerCatalogAXFR tries the BIND format first. Only a content
// refusal that is specific to the producer format is retried, on a fresh
// transfer, in the PowerDNS format; transport errors, refused transfers and
// refusals both formats share are returned unchanged. Both attempts keep the
// reader's strict bounds. The accepted producer is recorded in the result and
// pinned for the rest of the operation.
func selectDNSPeerCatalogAXFR(
	ctx context.Context, address, catalog string, read dnsPeerCatalogRead,
) (dnsCatalogAXFRResult, error) {
	if read == nil {
		return dnsCatalogAXFRResult{}, errors.New("peer catalog reader is unavailable")
	}
	producer := dnsCatalogAXFRBIND
	result, bindErr := read(ctx, dnsCatalogAXFRBIND)
	if bindErr != nil {
		if !errors.Is(bindErr, errDNSCatalogAXFRProducerFormat) {
			return dnsCatalogAXFRResult{}, bindErr
		}
		var pdnsErr error
		producer = dnsCatalogAXFRPowerDNS
		result, pdnsErr = read(ctx, dnsCatalogAXFRPowerDNS)
		if pdnsErr != nil {
			return dnsCatalogAXFRResult{}, &dnsPeerCatalogFormatError{
				catalog: catalog, bindReason: bindErr, pdnsReason: pdnsErr,
			}
		}
	}
	result.Producer = producer
	if err := dnsPeerCatalogSessionFrom(ctx).accept(address, catalog, producer); err != nil {
		return dnsCatalogAXFRResult{}, err
	}
	return result, nil
}

// queryDNSPeerCatalogAXFR reads the catalog a paired primary produced.
func queryDNSPeerCatalogAXFR(
	ctx context.Context, address, catalog string,
) (dnsCatalogAXFRResult, error) {
	return selectDNSPeerCatalogAXFR(ctx, address, catalog,
		func(ctx context.Context, producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
			if producer == dnsCatalogAXFRPowerDNS {
				return probeDNSPDNSCatalogAXFR(ctx, address, catalog)
			}
			return probeDNSCatalogAXFR(ctx, address, catalog)
		})
}

// queryDNSBoundPeerCatalogAXFR is queryDNSPeerCatalogAXFR from the host's own
// pair address, for proofs the primary's transfer ACL binds to that address.
func queryDNSBoundPeerCatalogAXFR(
	ctx context.Context, source, address, catalog string,
) (dnsCatalogAXFRResult, error) {
	return selectDNSPeerCatalogAXFR(ctx, address, catalog,
		func(ctx context.Context, producer dnsCatalogAXFRProducer) (dnsCatalogAXFRResult, error) {
			if producer == dnsCatalogAXFRPowerDNS {
				return probeDNSBoundPDNSCatalogAXFR(ctx, source, address, catalog)
			}
			return probeDNSBoundCatalogAXFR(ctx, source, address, catalog)
		})
}

// dnsPeerCatalogReadError keeps a caller's fixed message for transport errors
// and ordinary refusals, and adds the reason when the peer catalog is in no
// known format or its producer changed. Both reasons are fixed text.
func dnsPeerCatalogReadError(message string, err error) error {
	var formatErr *dnsPeerCatalogFormatError
	if errors.As(err, &formatErr) || errors.Is(err, errDNSPeerCatalogProducerChanged) {
		return fmt.Errorf("%s: %w", message, err)
	}
	return errors.New(message)
}
