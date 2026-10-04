//go:build !linux

package pdnspeerjournal

import "github.com/alicelik/celikpanel/internal/pdnspeerproof"

type VerifyCurrent func() error

func Read() (RecordV1, error) { return RecordV1{}, StateError{Unknown} }
func Publish(pdnspeerproof.RequestV1, string, uint64, VerifyCurrent) error {
	return StateError{Unknown}
}
func ConsumeOnce(pdnspeerproof.RequestV1, string, string, uint64, VerifyCurrent) error {
	return StateError{Unknown}
}

func Retire(pdnspeerproof.RequestV1, string, string, uint64, VerifyCurrent) error {
	return StateError{Unknown}
}
