//go:build !linux

package main

import (
	"time"

	"github.com/alicelik/celikpanel/internal/transport"
)

func collectHostSecurityAudit(_ time.Time) transport.SecurityAuditAgentResponse {
	return unknownSecurityAuditResponse("platform_unsupported")
}
