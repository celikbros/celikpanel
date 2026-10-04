//go:build !linux

package recoveryruntime

type runtimeState struct{}

func Resolve() (*Runtime, error)   { return nil, fail(ReasonPlatformUnsupported) }
func (*Runtime) Revalidate() error { return fail(ReasonPlatformUnsupported) }
func (*Runtime) Close() error      { return nil }

func VerifyBundle(string, bool) (*Runtime, error) { return nil, fail(ReasonPlatformUnsupported) }

func VerifyPreflightBoundary(int) error { return fail(ReasonPlatformUnsupported) }
