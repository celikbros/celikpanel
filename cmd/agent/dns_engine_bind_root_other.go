//go:build !linux

package main

import (
	"context"
	"errors"
)

var errBINDAbandonedGenerationRoot = errors.New(
	"the unreleased APT BIND generation root is unsupported",
)

func resolveBINDGroupGID(context.Context) (uint32, error) {
	return 0, errors.New("BIND service group proof requires Linux")
}

func prepareHostBINDGenerationRoot(
	ctx context.Context,
	layout bindHostLayout,
) error {
	return verifyHostBINDGenerationRoot(ctx, layout)
}

func hardenExistingHostBINDGenerationRoot(
	ctx context.Context,
	layout bindHostLayout,
) error {
	return verifyHostBINDGenerationRoot(ctx, layout)
}

func verifyHostBINDGenerationRoot(
	_ context.Context,
	layout bindHostLayout,
) error {
	switch layout.GenerationRoot {
	case aptBINDGenerationRoot:
		return errors.New("secure APT BIND generation root access requires Linux")
	case abandonedAPTBindGenerationRoot:
		return errBINDAbandonedGenerationRoot
	default:
		return nil
	}
}
