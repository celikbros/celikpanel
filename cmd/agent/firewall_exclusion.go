package main

import (
	"github.com/alicelik/celikpanel/internal/firewalllock"
	"io"
)

func (hostFirewallCommandRunner) AcquireFirewallLock() (io.Closer, error) {
	return firewalllock.Acquire()
}

func reportFirewallExclusion(response *FirewallStatusResponse, err error) {
	*response = FirewallStatusResponse{
		PersistenceState: firewallPersistenceUnverified,
		PersistenceError: err.Error(),
		Error:            "firewall state could not be observed exclusively: " + err.Error(),
	}
}
