//go:build linux

package dnspeerenrollment

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/alicelik/celikpanel/internal/binddns"
	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"github.com/alicelik/celikpanel/internal/dnspeertransport"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

const (
	SchemaV1       = "celikpanel-dns-peer-inspection/v1"
	EnrollmentPath = "/var/lib/celikpanel-agent-private/dns-peer-inspection-v1.json"
	CredentialDir  = "/var/lib/celikpanel-agent-private/dns-peer-inspection-keys"
	maxRecordBytes = 4096
	maxKeyBytes    = 16384
)

type Code string

const (
	// Disabled means only that the optional enrollment file is absent.
	Disabled Code = "peer_inspection_disabled"
	// Unknown includes unsafe storage, malformed enrollment and missing credentials.
	Unknown Code = "peer_inspection_enrollment_unknown"
	Changed Code = "peer_inspection_enrollment_changed"
)

type StateError struct{ Code Code }

func (e StateError) Error() string { return string(e.Code) }
func IsCode(err error, code Code) bool {
	var state StateError
	return errors.As(err, &state) && state.Code == code
}

// RecordV1 is owner-reviewed optional authority. The SSH identity digest uses
// lowercase hex of SHA256(ssh.PublicKey.Marshal()), matching dnspeertransport.
// The credential identifier is a filename stem, never a supplied path.
type RecordV1 struct {
	Schema                string `json:"schema"`
	EnrollmentID          string `json:"enrollment_id"`
	Revision              uint64 `json:"revision"`
	PrimaryIP             string `json:"primary_ip"`
	PeerIP                string `json:"peer_ip"`
	CatalogName           string `json:"catalog_name"`
	View                  string `json:"view"`
	SSHUsername           string `json:"ssh_username"`
	HostKeySHA256         string `json:"host_key_sha256"`
	CredentialID          string `json:"credential_id"`
	ClientPublicKeySHA256 string `json:"client_public_key_sha256"`
}

type fileIdentity struct {
	dev, ino uint64
}

// Snapshot binds a canonical enrollment and its credential to one read.
// Consumers must recheck immediately before and after exchange, then recheck
// current DNS operation/pair evidence independently before accepting a proof.
type Snapshot struct {
	Record              RecordV1
	RecordSHA256        string
	CredentialSHA256    string
	Transport           dnspeertransport.Enrollment
	recordFile, keyFile fileIdentity
}

var usernamePattern = regexp.MustCompile(`^[a-z_][a-z0-9_-]{0,31}$`)

func hex32(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 32
}
func id16(s string) bool {
	if len(s) != 32 || strings.ToLower(s) != s {
		return false
	}
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == 16
}
func ipv4(s string) bool {
	ip := net.ParseIP(s)
	return ip != nil && ip.To4() != nil && ip.String() == s && ip.IsGlobalUnicast()
}
func (r RecordV1) validate() bool {
	if r.Schema != SchemaV1 || !id16(r.EnrollmentID) || r.Revision == 0 ||
		!ipv4(r.PrimaryIP) || !ipv4(r.PeerIP) || r.PrimaryIP == r.PeerIP ||
		r.View != dnspeerproof.DefaultView || !usernamePattern.MatchString(r.SSHUsername) ||
		r.SSHUsername == "root" || !hex32(r.HostKeySHA256) ||
		!id16(r.CredentialID) || !hex32(r.ClientPublicKeySHA256) {
		return false
	}
	catalog, err := binddns.CatalogDomain(r.PrimaryIP)
	return err == nil && catalog == r.CatalogName
}

// Read is read-only. It never creates the enrollment, credential directory,
// key, SSH trust, or a remote connection.
func Read() (Snapshot, error) { return readAt("/") }

// Recheck rejects owner edits or replacement since the caller's read. A caller
// still needs to validate current operation identity and native DNS evidence.
func Recheck(previous Snapshot) error { return recheckAt("/", previous) }

func recheckAt(root string, previous Snapshot) error {
	current, err := readAt(root)
	if err != nil || current.RecordSHA256 != previous.RecordSHA256 ||
		current.CredentialSHA256 != previous.CredentialSHA256 ||
		current.recordFile != previous.recordFile || current.keyFile != previous.keyFile ||
		current.Transport != previous.Transport {
		return StateError{Changed}
	}
	return nil
}

func readAt(root string) (Snapshot, error) {
	if os.Geteuid() != 0 {
		return Snapshot{}, StateError{Unknown}
	}
	recordRaw, recordID, err := readSecure(root, EnrollmentPath, maxRecordBytes, 0600, true)
	if err != nil {
		return Snapshot{}, err
	}
	var record RecordV1
	decoder := json.NewDecoder(bytes.NewReader(recordRaw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&record) != nil || !record.validate() {
		return Snapshot{}, StateError{Unknown}
	}
	canonical, err := json.Marshal(record)
	if err != nil || !bytes.Equal(recordRaw, canonical) {
		return Snapshot{}, StateError{Unknown}
	}
	keyPath := path.Join(CredentialDir, record.CredentialID+".key")
	keyRaw, keyID, err := readSecure(root, keyPath, maxKeyBytes, 0600, false)
	if err != nil {
		return Snapshot{}, StateError{Unknown}
	}
	keyDigest := sha256.Sum256(keyRaw)
	signer, err := ssh.ParsePrivateKey(keyRaw)
	for i := range keyRaw {
		keyRaw[i] = 0
	}
	if err != nil {
		return Snapshot{}, StateError{Unknown}
	}
	pubDigest := sha256.Sum256(signer.PublicKey().Marshal())
	if hex.EncodeToString(pubDigest[:]) != record.ClientPublicKeySHA256 {
		return Snapshot{}, StateError{Unknown}
	}
	recordDigest := sha256.Sum256(recordRaw)
	recordAgain, recordAgainID, err := readSecure(root, EnrollmentPath, maxRecordBytes, 0600, false)
	if err != nil || recordAgainID != recordID || !bytes.Equal(recordRaw, recordAgain) {
		return Snapshot{}, StateError{Unknown}
	}
	keyAgain, keyAgainID, err := readSecure(root, keyPath, maxKeyBytes, 0600, false)
	keyMatches := err == nil && keyAgainID == keyID && sha256.Sum256(keyAgain) == keyDigest
	for i := range keyAgain {
		keyAgain[i] = 0
	}
	if !keyMatches {
		return Snapshot{}, StateError{Unknown}
	}
	// CredentialSHA256 is the private-file byte digest, used only for owner
	// change detection. Never log or serialize the raw credential.
	return Snapshot{
		Record: record, RecordSHA256: hex.EncodeToString(recordDigest[:]),
		CredentialSHA256: hex.EncodeToString(keyDigest[:]), recordFile: recordID, keyFile: keyID,
		Transport: dnspeertransport.Enrollment{
			PeerIP: record.PeerIP, Username: record.SSHUsername,
			HostKeySHA256:  record.HostKeySHA256,
			PrivateKeyPath: keyPath, Timeout: 5 * time.Second,
		},
	}, nil
}

func readSecure(root, name string, maximum int, expectedMode uint32, optional bool) ([]byte, fileIdentity, error) {
	unknown := StateError{Unknown}
	if !strings.HasPrefix(name, "/") || path.Clean(name) != name ||
		!strings.HasPrefix(name, "/var/lib/celikpanel-agent-private/") {
		return nil, fileIdentity{}, unknown
	}
	fd, err := unix.Open(root, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, fileIdentity{}, unknown
	}
	defer unix.Close(fd)
	var rootStat unix.Stat_t
	if unix.Fstat(fd, &rootStat) != nil || rootStat.Uid != 0 ||
		rootStat.Mode&unix.S_IFMT != unix.S_IFDIR || rootStat.Mode&0022 != 0 {
		return nil, fileIdentity{}, unknown
	}
	parts := strings.Split(strings.TrimPrefix(path.Dir(name), "/"), "/")
	current := fd
	for i, part := range parts {
		next, openErr := unix.Openat(current, part, unix.O_PATH|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if current != fd {
			unix.Close(current)
		}
		if openErr != nil {
			return nil, fileIdentity{}, unknown
		}
		current = next
		var st unix.Stat_t
		if unix.Fstat(current, &st) != nil || st.Uid != 0 ||
			st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Mode&0022 != 0 ||
			(i == len(parts)-1 && (st.Mode&0077 != 0 && st.Mode&0077 != 0050)) {
			unix.Close(current)
			return nil, fileIdentity{}, unknown
		}
	}
	defer unix.Close(current)
	file, openErr := unix.Openat(current, path.Base(name), unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if openErr != nil {
		if optional && errors.Is(openErr, unix.ENOENT) {
			return nil, fileIdentity{}, StateError{Disabled}
		}
		return nil, fileIdentity{}, unknown
	}
	f := os.NewFile(uintptr(file), name)
	defer f.Close()
	var before, after unix.Stat_t
	if unix.Fstat(file, &before) != nil || before.Uid != 0 || before.Nlink != 1 ||
		before.Mode&unix.S_IFMT != unix.S_IFREG || before.Mode&0777 != expectedMode ||
		before.Size <= 0 || before.Size > int64(maximum) {
		return nil, fileIdentity{}, unknown
	}
	raw, err := io.ReadAll(io.LimitReader(f, int64(maximum)+1))
	if err != nil || len(raw) == 0 || len(raw) > maximum ||
		unix.Fstat(file, &after) != nil || before.Dev != after.Dev || before.Ino != after.Ino ||
		before.Size != after.Size || before.Mtim != after.Mtim || before.Ctim != after.Ctim {
		return nil, fileIdentity{}, unknown
	}
	return raw, fileIdentity{dev: uint64(before.Dev), ino: before.Ino}, nil
}
