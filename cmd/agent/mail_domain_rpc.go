package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

type mailDomainMapKind uint8

const (
	mailDomainDovecotUsers mailDomainMapKind = iota
	mailDomainPostfixMailbox
	mailDomainPostfixDomains
	mailDomainPostfixVirtual
)

type mailDomainMap struct {
	path    string
	mode    os.FileMode
	kind    mailDomainMapKind
	postmap bool
}

// DeleteMailDomain converges runtime mail state.
func (a *Agent) DeleteMailDomain(
	req *transport.DeleteMailDomainRequest,
	resp *transport.DeleteMailDomainResponse,
) error {
	if req == nil || resp == nil {
		return fmt.Errorf("mail domain delete request and response are required")
	}
	resp.Applied = false
	resp.Quarantined = false
	if err := requireExpectedBuildCommit(req.ExpectedBuildCommit, "deleting a mail domain"); err != nil {
		return err
	}
	if req.DomainID <= 0 {
		return fmt.Errorf("mail domain deletion requires a positive domain identity")
	}
	domain, err := canonicalAgentDomain(req.Domain)
	if err != nil || req.Domain != domain {
		return fmt.Errorf("mail domain deletion requires a canonical domain")
	}

	mailMutex.Lock()
	defer mailMutex.Unlock()

	maps := []mailDomainMap{
		{path: dovecotUsersPath, mode: 0o640, kind: mailDomainDovecotUsers},
		{path: postfixVBoxPath, mode: 0o644, kind: mailDomainPostfixMailbox, postmap: true},
		{path: postfixDomainsPath, mode: 0o644, kind: mailDomainPostfixDomains, postmap: true},
		{path: postfixVirtualPath, mode: 0o644, kind: mailDomainPostfixVirtual, postmap: true},
	}
	writes := make([]mailFileWrite, 0, len(maps))
	postmaps := make([]string, 0, 3)
	for _, mailMap := range maps {
		raw, exists, readErr := readExistingMailDomainMap(mailMap.path)
		if readErr != nil {
			return readErr
		}
		if !exists {
			if mailMap.postmap {
				if err := ensureNoOrphanMailMapIndexes(mailMap.path); err != nil {
					return err
				}
			}
			continue
		}
		next, removed, filterErr := removeMailDomainSourceLines(raw, domain, mailMap.kind)
		if filterErr != nil {
			return fmt.Errorf("filter mail domain map %s: %w", mailMap.path, filterErr)
		}
		if mailMap.postmap {
			// Rebuild every existing Postfix map even when the source text is
			// already clean. A previous process may have crashed after writing
			// the source but before replacing its compiled .db/.lmdb index.
			postmaps = append(postmaps, mailMap.path)
		}
		if removed {
			writes = append(writes, mailFileWrite{path: mailMap.path, content: next, mode: mailMap.mode})
		}
	}

	ctx, cancel := newMailMutationContext()
	defer cancel()
	quarantined := false
	if err := applyMailFileMutation(ctx, writes, postmaps, func() (func() error, error) {
		rollback, preserved, quarantineErr := quarantineMailDomainDirectory(domain, req.DomainID)
		if quarantineErr == nil {
			quarantined = preserved
		}
		return rollback, quarantineErr
	}); err != nil {
		return err
	}
	resp.Applied = true
	resp.Quarantined = quarantined
	return nil
}

func readExistingMailDomainMap(path string) ([]byte, bool, error) {
	if path == dovecotUsersPath {
		// A never-configured passwd-file is an idempotently clean domain state,
		// but an existing secret-bearing file must satisfy the strict metadata
		// contract even when no target-domain row will need rewriting.
		return readDovecotUsersFileForMutation(path, false)
	}
	content, err := secureReadConfig(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read mail domain map %s: %w", path, err)
	}
	return content, true, nil
}

func ensureNoOrphanMailMapIndexes(path string) error {
	for _, suffix := range []string{".db", ".lmdb"} {
		snapshot, err := snapshotMailFile(path + suffix)
		if err != nil {
			return fmt.Errorf("inspect compiled mail map %s: %w", path+suffix, err)
		}
		if snapshot.exists {
			return fmt.Errorf(
				"refusing orphan compiled mail map %s without source %s",
				path+suffix,
				path,
			)
		}
	}
	return nil
}

func removeMailDomainSourceLines(
	content []byte,
	domain string,
	kind mailDomainMapKind,
) ([]byte, bool, error) {
	lines := splitMailFile(content)
	out := make([]string, 0, len(lines))
	removed := false
	for _, line := range lines {
		belongs, err := mailDomainLineBelongsTo(line, domain, kind)
		if err != nil {
			return nil, false, err
		}
		if belongs {
			removed = true
			continue
		}
		out = append(out, line)
	}
	if !removed {
		return content, false, nil
	}
	return renderMailFile(out), true, nil
}

func mailDomainLineBelongsTo(line, domain string, kind mailDomainMapKind) (bool, error) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false, nil
	}
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return false, nil
	}
	key := fields[0]
	if kind == mailDomainDovecotUsers {
		if colon := strings.IndexByte(key, ':'); colon > 0 {
			key = key[:colon]
		}
	}

	var parsedDomain string
	var err error
	switch kind {
	case mailDomainDovecotUsers, mailDomainPostfixMailbox:
		_, _, parsedDomain, err = canonicalAgentMailbox(key)
	case mailDomainPostfixDomains:
		parsedDomain, err = canonicalAgentDomain(key)
	case mailDomainPostfixVirtual:
		var source string
		source, err = transport.CanonicalForwardSource(key)
		if err == nil {
			if strings.HasPrefix(source, "@") {
				parsedDomain = strings.TrimPrefix(source, "@")
			} else {
				_, _, parsedDomain, err = canonicalAgentMailbox(source)
			}
		}
	default:
		return false, fmt.Errorf("unknown mail domain map kind %d", kind)
	}
	if err != nil {
		if rawMailSourceTargetsDomain(key, domain) {
			return false, fmt.Errorf("refusing malformed source for target domain %q", key)
		}
		return false, nil
	}
	return parsedDomain == domain, nil
}

func rawMailSourceTargetsDomain(key, domain string) bool {
	raw := strings.TrimSpace(strings.TrimSuffix(key, "."))
	if at := strings.LastIndexByte(raw, '@'); at >= 0 {
		raw = raw[at+1:]
	}
	return strings.EqualFold(raw, domain)
}

func mailDomainQuarantineName(domain string, domainID int) string {
	digest := sha256.Sum256([]byte(domain))
	return fmt.Sprintf("domain-%d-%x", domainID, digest[:8])
}
