package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

const agentMailMutationTimeout = 2 * time.Minute

var mailHashGenerator = generateDovecotHashContext

func newMailMutationContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), agentMailMutationTimeout)
}

func readMailFile(path string) ([]byte, error) {
	content, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return []byte{}, nil
	}
	return content, err
}

func splitMailFile(content []byte) []string {
	trimmed := strings.TrimRight(string(content), "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

func renderMailFile(lines []string) []byte {
	if len(lines) == 0 {
		return []byte{}
	}
	return []byte(strings.Join(lines, "\n") + "\n")
}

func mailFileHasKey(content []byte, key string) bool {
	for _, line := range splitMailFile(content) {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == key {
			return true
		}
		if strings.HasPrefix(line, key+":") {
			return true
		}
	}
	return false
}

func upsertMailLine(content []byte, key, replacement string) []byte {
	lines := splitMailFile(content)
	out := make([]string, 0, len(lines)+1)
	replaced := false
	for _, line := range lines {
		fields := strings.Fields(line)
		matches := len(fields) > 0 && fields[0] == key || strings.HasPrefix(line, key+":")
		if matches {
			if !replaced {
				out = append(out, replacement)
				replaced = true
			}
			continue
		}
		out = append(out, line)
	}
	if !replaced {
		out = append(out, replacement)
	}
	return renderMailFile(out)
}

func removeMailLine(content []byte, key string) ([]byte, bool) {
	lines := splitMailFile(content)
	out := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		fields := strings.Fields(line)
		matches := len(fields) > 0 && fields[0] == key || strings.HasPrefix(line, key+":")
		if matches {
			removed = true
			continue
		}
		out = append(out, line)
	}
	return renderMailFile(out), removed
}

func canonicalAgentMailbox(raw string) (email, local, domain string, err error) {
	email, err = transport.CanonicalMailAddress(raw)
	if err != nil {
		return "", "", "", err
	}
	parts := strings.SplitN(email, "@", 2)
	return email, parts[0], parts[1], nil
}

func canonicalAgentDomain(raw string) (string, error) {
	address, err := transport.CanonicalMailAddress("postmaster@" + strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	return strings.SplitN(address, "@", 2)[1], nil
}

func validateAgentQuota(quotaMB int) error {
	if quotaMB <= 0 || quotaMB > transport.MaxMailboxQuotaMB {
		return fmt.Errorf("mailbox quota must be between 1 and %d MB", transport.MaxMailboxQuotaMB)
	}
	return nil
}

func validateImportedCryptHash(hash string) error {
	if len(hash) < 13 || len(hash) > 4096 {
		return fmt.Errorf("invalid imported password hash")
	}
	for index := 0; index < len(hash); index++ {
		if hash[index] < 0x21 || hash[index] > 0x7e || hash[index] == ':' {
			return fmt.Errorf("invalid imported password hash")
		}
	}
	return nil
}

func canonicalAgentForwardings(forwardings []transport.MailForwarding) ([]transport.MailForwarding, error) {
	out := make([]transport.MailForwarding, 0, len(forwardings))
	seen := make(map[string]struct{}, len(forwardings))
	for _, forwarding := range forwardings {
		source, err := transport.CanonicalForwardSource(forwarding.Source)
		if err != nil {
			return nil, fmt.Errorf("invalid forwarding source: %w", err)
		}
		destination, err := transport.CanonicalMailAddress(forwarding.Destination)
		if err != nil {
			return nil, fmt.Errorf("invalid forwarding destination: %w", err)
		}
		if _, duplicate := seen[source]; duplicate {
			return nil, fmt.Errorf("duplicate forwarding source: %s", source)
		}
		seen[source] = struct{}{}
		out = append(out, transport.MailForwarding{Source: source, Destination: destination})
	}
	return out, nil
}
