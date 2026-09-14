//go:build !linux

package recoverycheckpoint

func Publish(name string) error { return ErrUnavailable }
