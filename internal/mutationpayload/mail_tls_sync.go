package mutationpayload

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"hash"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/alicelik/celikpanel/internal/hostname"
	"github.com/alicelik/celikpanel/internal/transport"
)

const (
	mailTLSSyncSchema          = "mail-tls-sync/v1"
	mailTLSSyncQualifierPrefix = mailTLSSyncSchema + ":sha256:"
	mailTLSSyncMaxEntries      = 4096
	mailTLSSyncMaxNames        = 2
	mailTLSSyncMaxPathBytes    = 4096
)

var mailTLSSyncDigestFrames = [][]byte{
	[]byte("celikpanel/service-mutation-payload"),
	[]byte(mailTLSSyncSchema),
	[]byte("mail_tls_sync"),
	[]byte("mail-tls"),
	[]byte("Agent.SyncMailTLSV2"),
	[]byte("sync"),
}

// MailTLSSyncCommitment is the detached, canonical full-state mail TLS plan
// authorized by one direct durable mutation. It deliberately excludes the
// panel build commit: that value remains a separate compatibility gate.
type MailTLSSyncCommitment struct {
	ManagedRoot string
	Myhostname  string
	SNI         []transport.MailSNIEntry
	Qualifier   string
}

// CanonicalMailTLSSync validates and freezes every payload field that changes
// Postfix or Dovecot state. Filesystem ownership, modes and certificate/key
// matching remain agent-side checks performed on this exact frozen plan.
func CanonicalMailTLSSync(
	managedRoot string,
	myhostname string,
	entries []transport.MailSNIEntry,
) (MailTLSSyncCommitment, error) {
	if managedRoot == "" || len(managedRoot) > mailTLSSyncMaxPathBytes ||
		managedRoot != strings.TrimSpace(managedRoot) ||
		!strings.HasPrefix(managedRoot, "/") || path.Clean(managedRoot) != managedRoot ||
		managedRoot == "/" || strings.ContainsAny(managedRoot, "\x00\r\n\t") {
		return MailTLSSyncCommitment{}, errors.New("mail TLS managed root must be canonical")
	}
	canonicalHost, err := hostname.CanonicalFQDN(myhostname)
	if err != nil || canonicalHost != myhostname {
		return MailTLSSyncCommitment{}, errors.New("mail TLS hostname must be canonical")
	}
	if len(entries) > mailTLSSyncMaxEntries {
		return MailTLSSyncCommitment{}, errors.New("mail TLS snapshot exceeds the entry limit")
	}

	frozen := make([]transport.MailSNIEntry, 0, len(entries))
	claimedNames := make(map[string]struct{}, len(entries)*mailTLSSyncMaxNames)
	for index, entry := range entries {
		canonical, err := canonicalMailTLSSNIEntry(managedRoot, entry)
		if err != nil {
			return MailTLSSyncCommitment{}, errors.New("mail TLS snapshot entry " +
				strconv.Itoa(index+1) + ": " + err.Error())
		}
		for _, name := range canonical.Names {
			if _, exists := claimedNames[name]; exists {
				return MailTLSSyncCommitment{}, errors.New("mail TLS snapshot claims a name more than once")
			}
			claimedNames[name] = struct{}{}
		}
		frozen = append(frozen, canonical)
	}

	sort.Slice(frozen, func(left, right int) bool {
		if frozen[left].CertPath != frozen[right].CertPath {
			return frozen[left].CertPath < frozen[right].CertPath
		}
		if frozen[left].KeyPath != frozen[right].KeyPath {
			return frozen[left].KeyPath < frozen[right].KeyPath
		}
		return strings.Join(frozen[left].Names, "\x00") <
			strings.Join(frozen[right].Names, "\x00")
	})
	if len(frozen) == 0 {
		frozen = nil
	}

	digest := sha256.New()
	for _, frame := range mailTLSSyncDigestFrames {
		writeMailTLSSyncDigestFrame(digest, frame)
	}
	writeMailTLSSyncDigestFrame(digest, []byte(managedRoot))
	writeMailTLSSyncDigestFrame(digest, []byte(canonicalHost))
	var count [4]byte
	binary.BigEndian.PutUint32(count[:], uint32(len(frozen)))
	_, _ = digest.Write(count[:])
	for _, entry := range frozen {
		binary.BigEndian.PutUint32(count[:], uint32(len(entry.Names)))
		_, _ = digest.Write(count[:])
		for _, name := range entry.Names {
			writeMailTLSSyncDigestFrame(digest, []byte(name))
		}
		writeMailTLSSyncDigestFrame(digest, []byte(entry.CertPath))
		writeMailTLSSyncDigestFrame(digest, []byte(entry.KeyPath))
	}

	return MailTLSSyncCommitment{
		ManagedRoot: managedRoot,
		Myhostname:  canonicalHost,
		SNI:         frozen,
		Qualifier:   mailTLSSyncQualifierPrefix + hex.EncodeToString(digest.Sum(nil)),
	}, nil
}

func canonicalMailTLSSNIEntry(managedRoot string, entry transport.MailSNIEntry) (transport.MailSNIEntry, error) {
	if len(entry.Names) == 0 || len(entry.Names) > mailTLSSyncMaxNames {
		return transport.MailSNIEntry{}, errors.New("name count is invalid")
	}
	certDomain, certDirectory, err := canonicalMailTLSSnapshotPath(managedRoot, entry.CertPath, "fullchain.pem")
	if err != nil {
		return transport.MailSNIEntry{}, errors.New("certificate path is invalid")
	}
	keyDomain, keyDirectory, err := canonicalMailTLSSnapshotPath(managedRoot, entry.KeyPath, "privkey.pem")
	if err != nil {
		return transport.MailSNIEntry{}, errors.New("private-key path is invalid")
	}
	if certDomain != keyDomain || certDirectory != keyDirectory {
		return transport.MailSNIEntry{}, errors.New("certificate and key are not from the same snapshot")
	}
	mailName, err := hostname.MailFQDN(certDomain)
	if err != nil {
		return transport.MailSNIEntry{}, errors.New("certificate domain is invalid")
	}

	names := make([]string, 0, len(entry.Names))
	seen := make(map[string]struct{}, len(entry.Names))
	hasMailName := false
	for _, raw := range entry.Names {
		name, err := hostname.CanonicalFQDN(raw)
		if err != nil || name != raw {
			return transport.MailSNIEntry{}, errors.New("SNI name is not canonical")
		}
		if name != certDomain && name != mailName {
			return transport.MailSNIEntry{}, errors.New("SNI name does not belong to the certificate domain")
		}
		hasMailName = hasMailName || name == mailName
		if _, exists := seen[name]; exists {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if !hasMailName {
		return transport.MailSNIEntry{}, errors.New("managed mail hostname is missing")
	}
	sort.Strings(names)

	return transport.MailSNIEntry{
		Names:    names,
		CertPath: entry.CertPath,
		KeyPath:  entry.KeyPath,
	}, nil
}

func canonicalMailTLSSnapshotPath(managedRoot, raw, filename string) (string, string, error) {
	if raw == "" || len(raw) > mailTLSSyncMaxPathBytes || raw != strings.TrimSpace(raw) ||
		!strings.HasPrefix(raw, "/") || path.Clean(raw) != raw || path.Base(raw) != filename ||
		strings.ContainsAny(raw, "\x00\r\n\t") {
		return "", "", errors.New("invalid snapshot path")
	}
	relative := strings.TrimPrefix(raw, managedRoot+"/")
	if relative == raw {
		return "", "", errors.New("snapshot path is outside the managed root")
	}
	parts := strings.Split(relative, "/")
	if len(parts) != 3 || parts[2] != filename {
		return "", "", errors.New("snapshot path is not an immutable managed snapshot")
	}
	directory := path.Dir(raw)
	version := path.Base(directory)
	domain := path.Base(path.Dir(directory))
	canonicalDomain, err := hostname.CanonicalFQDN(domain)
	if err != nil || canonicalDomain != domain || domain != parts[0] ||
		version != parts[1] || !validMailTLSVersion(version) {
		return "", "", errors.New("invalid snapshot identity")
	}
	return domain, directory, nil
}

func validMailTLSVersion(value string) bool {
	if len(value) != len("sha256-")+sha256.Size*2 || !strings.HasPrefix(value, "sha256-") {
		return false
	}
	for _, character := range strings.TrimPrefix(value, "sha256-") {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func writeMailTLSSyncDigestFrame(destination hash.Hash, value []byte) {
	var length [4]byte
	binary.BigEndian.PutUint32(length[:], uint32(len(value)))
	_, _ = destination.Write(length[:])
	_, _ = destination.Write(value)
}

// ValidMailTLSSyncQualifier accepts only the canonical v1 lowercase SHA-256
// commitment stored in ServiceMutationJob.PackageName.
func ValidMailTLSSyncQualifier(value string) bool {
	if len(value) != len(mailTLSSyncQualifierPrefix)+sha256.Size*2 ||
		!strings.HasPrefix(value, mailTLSSyncQualifierPrefix) {
		return false
	}
	for _, character := range strings.TrimPrefix(value, mailTLSSyncQualifierPrefix) {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}
