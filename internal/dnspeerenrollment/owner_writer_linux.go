//go:build linux

package dnspeerenrollment

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/alicelik/celikpanel/internal/dnspeerproof"
	"golang.org/x/crypto/ssh"
	"golang.org/x/sys/unix"
)

// PreparedOwner contains public material only. The private key stays under the
// root-owned Agent state directory and is inactive until ActivateOwner runs.
type PreparedOwner struct {
	CredentialID    string
	PublicKey       string
	PublicKeySHA256 string
}

type OwnerActivation struct {
	CredentialID  string
	PrimaryIP     string
	PeerIP        string
	CatalogName   string
	SSHUsername   string
	HostKeySHA256 string
}

func PrepareOwner() (PreparedOwner, error)                  { return prepareOwnerAt("/") }
func ActivateOwner(input OwnerActivation) (RecordV1, error) { return activateOwnerAt("/", input) }
func RevokeOwner() error                                    { return revokeOwnerAt("/") }

// PowerDNSEnrollmentPath is pdnspeerenrollment.EnrollmentPath. It is repeated
// here because that package imports this one; its test pins the equality.
const PowerDNSEnrollmentPath = "/var/lib/celikpanel-agent-private/pdns-peer-inspection-v1.json"

// refusePowerDNSEnrollmentAt keeps the two native engine enrollments mutually
// exclusive. The Agent never chooses between two enrollments by priority; it
// reports both as a changed enrollment and keeps the deletion pending.
func refusePowerDNSEnrollmentAt(root string) error {
	if _, err := os.Lstat(ownerRootPath(root, PowerDNSEnrollmentPath)); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return errors.New("the PowerDNS peer enrollment path cannot be inspected; review it locally before enrolling BIND")
	}
	return errors.New("a PowerDNS peer enrollment exists at " + PowerDNSEnrollmentPath + "; the Agent accepts one native peer engine enrollment. Run primary-status --engine pdns, and primary-revoke --engine pdns if the secondary now runs BIND, before enrolling BIND")
}

func prepareOwnerAt(root string) (PreparedOwner, error) {
	var zero PreparedOwner
	if err := ownerEnrollmentPrivateDir(root, true); err != nil {
		return zero, err
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		return zero, errors.New("an active or unsafe peer enrollment already exists")
	}
	if err := refusePowerDNSEnrollmentAt(root); err != nil {
		return zero, err
	}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return zero, err
	}
	der, err := x509.MarshalPKCS8PrivateKey(private)
	for i := range private {
		private[i] = 0
	}
	if err != nil {
		return zero, err
	}
	key := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	for i := range der {
		der[i] = 0
	}
	publicKey, err := ssh.NewPublicKey(public)
	if err != nil {
		return zero, err
	}
	id, err := ownerRandomID()
	if err != nil {
		return zero, err
	}
	keyPath := ownerRootPath(root, CredentialDir+"/"+id+".key")
	if err := ownerWriteNew(keyPath, key); err != nil {
		return zero, err
	}
	for i := range key {
		key[i] = 0
	}
	digest := sha256.Sum256(publicKey.Marshal())
	return PreparedOwner{
		CredentialID:    id,
		PublicKey:       strings.TrimSpace(string(ssh.MarshalAuthorizedKey(publicKey))),
		PublicKeySHA256: hex.EncodeToString(digest[:]),
	}, nil
}

func activateOwnerAt(root string, input OwnerActivation) (RecordV1, error) {
	var zero RecordV1
	if err := ownerEnrollmentPrivateDir(root, false); err != nil {
		return zero, err
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		return zero, errors.New("an active or unsafe peer enrollment already exists")
	}
	if err := refusePowerDNSEnrollmentAt(root); err != nil {
		return zero, err
	}
	if !id16(input.CredentialID) || input.SSHUsername != "celikpeer" {
		return zero, errors.New("prepared credential or dedicated SSH account is invalid")
	}
	keyPath := CredentialDir + "/" + input.CredentialID + ".key"
	key, _, err := readSecure(root, keyPath, maxKeyBytes, 0600, false)
	if err != nil {
		return zero, errors.New("prepared credential cannot be verified")
	}
	signer, err := ssh.ParsePrivateKey(key)
	for i := range key {
		key[i] = 0
	}
	if err != nil || signer.PublicKey().Type() != ssh.KeyAlgoED25519 {
		return zero, errors.New("prepared credential is not Ed25519")
	}
	pub := sha256.Sum256(signer.PublicKey().Marshal())
	id, err := ownerRandomID()
	if err != nil {
		return zero, err
	}
	record := RecordV1{
		Schema: SchemaV1, EnrollmentID: id, Revision: 1,
		PrimaryIP: input.PrimaryIP, PeerIP: input.PeerIP,
		CatalogName: input.CatalogName, View: dnspeerproof.DefaultView,
		SSHUsername: input.SSHUsername, HostKeySHA256: input.HostKeySHA256,
		CredentialID: input.CredentialID, ClientPublicKeySHA256: hex.EncodeToString(pub[:]),
	}
	if !record.validate() {
		return zero, errors.New("reviewed peer identity or catalog is invalid")
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return zero, err
	}
	if err := ownerWriteNew(ownerRootPath(root, EnrollmentPath), raw); err != nil {
		return zero, err
	}
	// A failed readback leaves the exact record in place for owner inspection;
	// it never silently creates another enrollment or retries a DNS mutation.
	readback, err := readAt(root)
	if err != nil || readback.Record != record {
		return zero, errors.New("new enrollment could not be verified; inspect it before retrying")
	}
	return record, nil
}

func revokeOwnerAt(root string) error {
	if err := ownerEnrollmentPrivateDir(root, false); err != nil {
		return err
	}
	current, err := readAt(root)
	if err != nil {
		return errors.New("enrollment cannot be verified for revocation")
	}
	if err := recheckAt(root, current); err != nil {
		return errors.New("enrollment changed before revocation")
	}
	from := ownerRootPath(root, EnrollmentPath)
	to := ownerRootPath(root, "/var/lib/celikpanel-agent-private/dns-peer-inspection-revoked-"+current.Record.EnrollmentID+".json")
	if err := unix.Renameat2(unix.AT_FDCWD, from, unix.AT_FDCWD, to, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("could not revoke the exact peer enrollment: %w", err)
	}
	if err := ownerSyncDirectory(filepath.Dir(from)); err != nil {
		return err
	}
	if _, err := readAt(root); !IsCode(err, Disabled) {
		return errors.New("revoked enrollment still appears active")
	}
	return nil
}

func ownerRandomID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

func ownerRootPath(root, absolute string) string {
	return filepath.Join(root, strings.TrimPrefix(absolute, "/"))
}

func ownerEnrollmentPrivateDir(root string, createKeyDir bool) error {
	if os.Geteuid() != 0 || !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return errors.New("peer enrollment requires a safe root context")
	}
	for _, relative := range []string{"", "var", "var/lib", "var/lib/celikpanel-agent-private"} {
		name := filepath.Join(root, relative)
		info, err := os.Lstat(name)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0022 != 0 {
			return errors.New("peer enrollment state directory is unsafe")
		}
		stat, ok := info.Sys().(*syscall.Stat_t)
		if !ok || stat.Uid != 0 {
			return errors.New("peer enrollment state directory has wrong owner")
		}
	}
	private := ownerRootPath(root, "/var/lib/celikpanel-agent-private")
	info, err := os.Lstat(private)
	if err != nil || (info.Mode().Perm() != 0700 && info.Mode().Perm() != 0750) {
		return errors.New("peer enrollment private directory has wrong mode")
	}
	keyDir := ownerRootPath(root, CredentialDir)
	if createKeyDir {
		if err := os.Mkdir(keyDir, 0700); err != nil && !errors.Is(err, os.ErrExist) {
			return err
		}
	}
	info, err = os.Lstat(keyDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm() != 0700 {
		return errors.New("peer credential directory is unsafe")
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || stat.Uid != 0 {
		return errors.New("peer credential directory has wrong owner")
	}
	return nil
}

func ownerWriteNew(name string, raw []byte) error {
	if len(raw) == 0 || len(raw) > maxKeyBytes {
		return errors.New("owner enrollment material has invalid size")
	}
	id, err := ownerRandomID()
	if err != nil {
		return err
	}
	temp := filepath.Join(filepath.Dir(name), ".owner-stage-"+id)
	file, err := os.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer os.Remove(temp)
	if err := file.Chmod(0600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(raw); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := unix.Renameat2(unix.AT_FDCWD, temp, unix.AT_FDCWD, name, unix.RENAME_NOREPLACE); err != nil {
		return err
	}
	return ownerSyncDirectory(filepath.Dir(name))
}

func ownerSyncDirectory(name string) error {
	dir, err := os.Open(name)
	if err != nil {
		return err
	}
	defer dir.Close()
	return dir.Sync()
}
