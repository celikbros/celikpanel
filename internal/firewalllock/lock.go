// Package firewalllock serializes native boot restore and management firewall
// work without requiring either process to be available to the other.
package firewalllock

import "errors"

var ErrBusy = errors.New("another firewall operation is running; observe it before retrying")
