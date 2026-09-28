//go:build !linux

package main

import (
	"context"
	"errors"
	"github.com/alicelik/celikpanel/internal/hostplatform"
)

func prepareBINDIndependentRuntime(context.Context, hostplatform.Profile, dnsEngineSwitchJournal) (func() error, func(), error) {
	return func() error { return nil }, func() {}, nil
}

func prepareBINDAdoptionIndependentRuntime(context.Context) (func() error, func(), error) {
	return nil, nil, errors.New("running BIND adoption requires a supported Linux independent recovery runtime")
}
