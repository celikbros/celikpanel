package main

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/alicelik/celikpanel/internal/transport"
)

func writeConfigRPCError(w http.ResponseWriter, rpcErr *transport.ConfigRPCError) {
	if rpcErr == nil {
		writeServerError(w, fmt.Errorf("agent returned an empty configuration error"))
		return
	}

	message := strings.TrimSpace(rpcErr.Message)
	switch rpcErr.Code {
	case transport.ConfigErrorPathRefused:
		if message == "" {
			message = "configuration path refused"
		}
		writeCodedError(w, http.StatusForbidden, errCodeConfigPathRefused, message, "")
	case transport.ConfigErrorValidationFail:
		if message == "" {
			message = "configuration validation failed"
		}
		writeCodedError(w, http.StatusUnprocessableEntity, errCodeConfigInvalid, message, "")
	default:
		// Unknown codes are protocol failures. Never downgrade them because
		// their human-readable text happens to contain a familiar phrase.
		writeServerError(w, fmt.Errorf("agent returned unknown configuration error code %q", rpcErr.Code))
	}
}
