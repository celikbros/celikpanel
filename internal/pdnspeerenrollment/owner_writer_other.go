//go:build !linux

package pdnspeerenrollment

// Owner enrollment is Linux-only. These stubs create no key, record or trust.

const OwnerSSHUsername = "celikpeer"

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

type RecordV1 struct{ EnrollmentID string }

func PrepareOwner() (PreparedOwner, error) { return PreparedOwner{}, StateError{Unsupported} }
func ActivateOwner(OwnerActivation) (RecordV1, error) {
	return RecordV1{}, StateError{Unsupported}
}
func RevokeOwner() error { return StateError{Unsupported} }
