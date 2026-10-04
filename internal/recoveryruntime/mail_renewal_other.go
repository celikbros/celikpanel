//go:build !linux

package recoveryruntime

func PrepareMailRenewalRuntime(string, int) (string, error) {
	return "", fail(ReasonPlatformUnsupported)
}

func InspectMailRenewalHook() (*MailRenewalHook, error) { return nil, fail(ReasonPlatformUnsupported) }
