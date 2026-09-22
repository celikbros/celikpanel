package recoveryruntime

// Hook mode is an observation, not permission to migrate or start a service.
type MailRenewalHookMode string

const (
	MailRenewalHookAbsent      MailRenewalHookMode = "absent"
	MailRenewalHookLegacy      MailRenewalHookMode = "legacy"
	MailRenewalHookIndependent MailRenewalHookMode = "independent"
)
const MailRenewalHookPath = "/etc/letsencrypt/renewal-hooks/deploy/celikpanel-mail-host-cert"

type MailRenewalHook struct {
	Mode       MailRenewalHookMode
	Generation string
	revalidate func() error
	close      func()
}

func (h *MailRenewalHook) Revalidate() error {
	if h == nil || h.revalidate == nil {
		return fail(ReasonReadFailed)
	}
	return h.revalidate()
}
func (h *MailRenewalHook) Close() {
	if h != nil && h.close != nil {
		h.close()
		h.close = nil
		h.revalidate = nil
	}
}
