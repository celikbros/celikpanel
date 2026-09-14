//go:build !linux

package recoveryobs

func Read(id string) Status { return unavailable(id) }
func Publish(Record) error  { return ErrUnavailable }
