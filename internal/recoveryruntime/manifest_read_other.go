//go:build !linux

package recoveryruntime

func (*Runtime) ManifestBytes() ([]byte, error) { return nil, fail(ReasonPlatformUnsupported) }
