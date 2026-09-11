package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

const errCodeRemoteDNSUnavailable = "REMOTE_DNS_UNAVAILABLE"

func (p *Panel) remoteDNSConnectionForCreation(ctx context.Context, parent string) (string, error) {
	var id string
	var err error
	if parent != "" {
		id, err = p.domainRemoteDNSConnectionID(ctx, parent)
	} else {
		id, err = p.defaultRemoteDNSConnectionID(ctx)
	}
	if err != nil {
		return "", err
	}
	if id == "" {
		return "", errors.New("remote DNS connection is not configured")
	}
	authority, err := p.remoteDNSConnectionReadiness(ctx, id)
	if err != nil {
		return "", err
	}
	if !authority.Ready {
		return "", errors.New("remote DNS authority is not ready")
	}
	return id, nil
}

func writeRemoteDNSUnavailable(w http.ResponseWriter) {
	writeCodedError(w, http.StatusConflict, errCodeRemoteDNSUnavailable,
		"the connected DNS authority could not be verified; restore the existing DNS connection and retry", "/settings?section=dns")
}

// Read the intended policy family rather than the first TXT at an owner. The
// canonical record store retains quoted DNS character strings and unrelated
// verification tokens. Multiple active policies remain unresolved.
func remoteMailDesiredTXT(records []DNSRecord, name, prefix string) string {
	value := ""
	for _, record := range records {
		if record.Disabled || record.Name != name || record.Type != "TXT" {
			continue
		}
		decoded, err := decodeDNSUserTXT(record.Content)
		if err != nil || !strings.HasPrefix(decoded, prefix) {
			continue
		}
		if value != "" {
			return ""
		}
		value = decoded
	}
	return value
}
