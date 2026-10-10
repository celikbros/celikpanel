package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"syscall"
	"time"

	"github.com/alicelik/celikpanel/internal/dnswire"
	"github.com/alicelik/celikpanel/internal/transport"
)

// Mail identity is an external DNS fact. The hostname operation deliberately
// puts the local FQDN in /etc/hosts; NSS and net.Resolver address lookups can
// therefore neither prove nor disprove its public forward/reverse records.
func publicMailDNSIdentity(ctx context.Context, source, hostname string) (string, bool, bool, error) {
	return mailDNSIdentityAt(ctx, source, hostname, []string{"1.1.1.1:53", "8.8.8.8:53"})
}

// errMailDNSIdentityInvalid: the address or host name cannot be looked up, so no
// lookup was made. It is not a lookup failure.
// Adres ya da ad sorgulanamaz; sorgu yapilmadi, bu bir sorgu hatasi degildir.
var errMailDNSIdentityInvalid = errors.New("mail DNS identity is invalid")

// mailDNSLookupErrorClass names why no resolver answered, without the
// resolver's own text: timeout, no resolver reachable, refused, or other.
// Hicbir cozumleyicinin yanit vermeme nedeni, cozumleyicinin kendi metni olmadan.
func mailDNSLookupErrorClass(err error) string {
	var netErr net.Error
	switch {
	case err == nil:
		return ""
	case errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()):
		return transport.MailDNSLookupErrorTimeout
	case errors.Is(err, syscall.ECONNREFUSED) || strings.HasSuffix(err.Error(), "response code 5"):
		return transport.MailDNSLookupErrorRefused
	case errors.Is(err, errNoMailDNSResolver) || errors.Is(err, syscall.ENETUNREACH) || errors.Is(err, syscall.EHOSTUNREACH):
		return transport.MailDNSLookupErrorNoResolver
	default:
		return transport.MailDNSLookupErrorOther
	}
}

var errNoMailDNSResolver = errors.New("no public mail DNS resolver available")

// A name the resolver says does not exist (NXDOMAIN) is an answer: no PTR, or
// no address for the host name. Only a missing answer is a lookup failure.
// NXDOMAIN bir yanittir; yalniz yanitin olmamasi sorgu hatasidir.
func queryMailDNSAnswer(ctx context.Context, resolver, name string, qtype uint16) ([]string, error) {
	names, err := queryMailDNS(ctx, resolver, name, qtype)
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
		return nil, nil
	}
	return names, err
}

func mailDNSIdentityAt(ctx context.Context, source, hostname string, resolvers []string) (string, bool, bool, error) {
	ip := net.ParseIP(source)
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))
	if ip == nil || !serviceMutationCanonicalFQDN(hostname) {
		return "", false, false, errMailDNSIdentityInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	reverse := ""
	qtype := uint16(1)
	if v4 := ip.To4(); v4 != nil {
		reverse = fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa", v4[3], v4[2], v4[1], v4[0])
	} else {
		qtype = 28
		const digits = "0123456789abcdef"
		var out strings.Builder
		for i := len(ip) - 1; i >= 0; i-- {
			out.WriteByte(digits[ip[i]&15])
			out.WriteByte('.')
			out.WriteByte(digits[ip[i]>>4])
			out.WriteByte('.')
		}
		reverse = out.String() + "ip6.arpa"
	}
	var lastErr error
	for _, resolver := range resolvers {
		names, err := queryMailDNSAnswer(ctx, resolver, reverse, dnsTypePTR)
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		if len(names) == 0 {
			return "", false, false, nil
		}
		for _, name := range names {
			if name != hostname {
				continue
			}
			addresses, err := queryMailDNSAnswer(ctx, resolver, hostname, qtype)
			if err != nil {
				lastErr = err
				break
			}
			for _, address := range addresses {
				if ip.Equal(net.ParseIP(address)) {
					return hostname, true, true, nil
				}
			}
			return hostname, true, false, nil
		}
		if lastErr == nil {
			return names[0], false, false, nil
		}
	}
	if lastErr == nil {
		lastErr = errNoMailDNSResolver
	}
	return "", false, false, lastErr
}

func queryMailDNS(ctx context.Context, resolver, name string, qtype uint16) ([]string, error) {
	return dnswire.Query(ctx, resolver, name, qtype)
}

func parseMailDNSResponse(message []byte, id uint16, name string, qtype uint16) ([]string, error) {
	return dnswire.ParseResponse(message, id, name, qtype)
}
