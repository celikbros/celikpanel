//go:build !linux

package recoveryruntime

func PrepareMailRenewalRuntime(string, int) (string, error) {
	return "", fail(ReasonPlatformUnsupported)
}
