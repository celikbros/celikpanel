//go:build !linux

package recoveryruntime

func (*Runtime) VerifyExecutingBinary() error { return fail(ReasonPlatformUnsupported) }
