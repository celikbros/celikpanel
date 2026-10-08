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

// configRefusal is an expected answer of the configuration editor that carries
// more than a code: which of several causes it was, the one line the service's
// own program said about it, and the line or setting it is about. It travels in
// the RPC answer, never in net/rpc's text-only error channel, so the Panel
// never reads a status out of a sentence.
//
// configRefusal, yapılandırma düzenleyicisinin koddan fazlasını taşıyan beklenen
// yanıtıdır: hangi neden, hizmetin kendi programının söylediği tek satır ve
// ilgili satır ya da ayar. RPC yanıtında taşınır.
type configRefusal struct {
	code    transport.ConfigErrorCode
	reason  string
	message string
	detail  string
	line    int
	name    string
}

func (r *configRefusal) Error() string {
	if r.detail != "" {
		return r.message + ": " + r.detail
	}
	return r.message
}

func configRPCError(err error) *transport.ConfigRPCError {
	if err == nil {
		return nil
	}
	var refusal *configRefusal
	switch {
	case errors.As(err, &refusal):
		return &transport.ConfigRPCError{
			Code:    refusal.code,
			Message: refusal.message,
			Reason:  refusal.reason,
			Detail:  refusal.detail,
			Line:    refusal.line,
			Name:    refusal.name,
		}
	case errors.Is(err, errConfigPathRefused):
		return &transport.ConfigRPCError{
			Code:    transport.ConfigErrorPathRefused,
			Message: err.Error(),
		}
	case errors.Is(err, errConfigValidationFail):
		return &transport.ConfigRPCError{
			Code:    transport.ConfigErrorValidationFail,
			Message: err.Error(),
			Reason:  transport.ConfigInvalidDaemon,
		}
	default:
		return nil
	}
}

func configUnreadable() error {
	return &configRefusal{
		code:    transport.ConfigErrorUnreadable,
		message: "the configuration file could not be read; nothing was changed",
	}
}

func configVersionRequired() error {
	return &configRefusal{
		code:    transport.ConfigErrorVersionRequired,
		message: "the configuration write carried no version of the file it was built from; nothing was changed",
	}
}

func configChanged() error {
	return &configRefusal{
		code:    transport.ConfigErrorChanged,
		message: "the configuration file is not the one the write was built from; nothing was changed",
	}
}

func configInvalid(reason, message string) *configRefusal {
	return &configRefusal{code: transport.ConfigErrorValidationFail, reason: reason, message: message}
}
