//go:build !linux

package recoveryruntime

func PrepareFirewallRuntime(string, int) (string, error) { return "", fail(ReasonPlatformUnsupported) }
