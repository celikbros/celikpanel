//go:build !linux

package dnsenginerecovery

import (
	"context"
	"errors"
)

type NativeCgroupUnitRunner func(context.Context, string) ([]byte, error)
type NativeCgroupEventsReader func(context.Context, string) ([]byte, bool, error)

func SystemdCgroupUnitRunner(context.Context, string) ([]byte, error) {
	return nil, errors.New("DNS cgroup proof requires Linux")
}
func NativeCgroupEvents(context.Context, string) ([]byte, bool, error) {
	return nil, false, errors.New("DNS cgroup proof requires Linux")
}
func ProbeEmptyUnitCgroup(context.Context, string, NativeCgroupUnitRunner, NativeCgroupEventsReader) error {
	return errors.New("DNS cgroup proof requires Linux")
}
