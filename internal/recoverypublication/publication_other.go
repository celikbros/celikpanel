//go:build !linux

package recoverypublication

func RestoreExisting(Request) error { return ErrUnavailable }
func ApplyExisting(Request) error   { return ErrUnavailable }

func ProbeCurrentResources() error { return ErrUnavailable }
