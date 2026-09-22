//go:build !linux

package recoveryruntime

func InspectCompatibleMailAgent(string) (*CompatibleMailAgent, error) {
	return nil, fail(ReasonPlatformUnsupported)
}

func CheckMailApplicationCompatibility(string) error { return fail(ReasonPlatformUnsupported) }
