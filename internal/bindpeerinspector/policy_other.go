//go:build !linux

package bindpeerinspector

import (
	"context"
	"errors"
)

const OwnerPolicyPath = "/etc/bind-peer-inspector/policy.json"

type OwnerPolicyReader struct{}

func (OwnerPolicyReader) Read(context.Context) (OwnerPolicyV1, string, error) {
	return OwnerPolicyV1{}, "", errors.New("BIND peer policy is available only on Linux")
}
