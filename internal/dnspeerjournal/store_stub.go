//go:build !linux

package dnspeerjournal

import "github.com/alicelik/celikpanel/internal/dnspeerproof"

type VerifyCurrent func() error

func Read() (RecordV1, error)                                             { return RecordV1{}, StateError{Unknown} }
func Publish(dnspeerproof.RequestV1, string, uint64, VerifyCurrent) error { return StateError{Unknown} }
func ConsumeOnce(dnspeerproof.RequestV1, string, string, uint64, VerifyCurrent) error {
	return StateError{Unknown}
}

func Retire(dnspeerproof.RequestV1, string, string, uint64, VerifyCurrent) error {
	return StateError{Unknown}
}
