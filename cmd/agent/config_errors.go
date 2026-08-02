package main

import (
	"errors"
	"fmt"

	"github.com/alicelik/celikpanel/internal/transport"
)

var (
	errConfigPathRefused    = errors.New("configuration path refused")
	errConfigValidationFail = errors.New("config validation failed")
)

func configPathRefusal(format string, args ...any) error {
	return fmt.Errorf("%w: %s", errConfigPathRefused, fmt.Sprintf(format, args...))
}

func configRPCError(err error) *transport.ConfigRPCError {
	if err == nil {
		return nil
	}
	switch {
	case errors.Is(err, errConfigPathRefused):
		return &transport.ConfigRPCError{
			Code:    transport.ConfigErrorPathRefused,
			Message: err.Error(),
		}
	case errors.Is(err, errConfigValidationFail):
		return &transport.ConfigRPCError{
			Code:    transport.ConfigErrorValidationFail,
			Message: err.Error(),
		}
	default:
		return nil
	}
}
