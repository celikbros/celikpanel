package recoveryruntime

// PromotionRequest admits only a verified packaged runtime under the updater's
// existing native lock. It neither starts a release transaction nor stops services.
type PromotionRequest struct{ Source, Mode string }

const PromotionRoot = "/var/lib/celikpanel-release-state/recovery-promotions/v1"

// PromotionStatus is nonauthorizing observation of a fully verified current
// promotion. An unreadable or conflicting record returns an error, not "none".
type PromotionStatus struct {
	Phase    string `json:"phase"`
	Previous string `json:"previous,omitempty"`
	Target   string `json:"target,omitempty"`
}
