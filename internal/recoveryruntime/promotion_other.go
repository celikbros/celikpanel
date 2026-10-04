//go:build !linux

package recoveryruntime

func Promote(PromotionRequest, int) error        { return fail(ReasonPlatformUnsupported) }
func ResumePromotion(int) error                  { return fail(ReasonPlatformUnsupported) }
func ResumePromotionForOwner() error             { return fail(ReasonPlatformUnsupported) }
func PromotionPending() (bool, error)            { return false, fail(ReasonPlatformUnsupported) }
func IsLauncherEntry() (bool, error)             { return false, nil }
func VerifiedLauncherRuntime() (*Runtime, error) { return nil, fail(ReasonPlatformUnsupported) }
func (*Runtime) ExecSelected([]string) error     { return fail(ReasonPlatformUnsupported) }

func InspectPromotion() (PromotionStatus, error) {
	return PromotionStatus{}, fail(ReasonPlatformUnsupported)
}
