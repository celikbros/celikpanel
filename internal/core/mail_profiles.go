package core

const (
	MailProfileCore      = "core-mail"
	MailProfileWebmail   = "webmail"
	MailProfileProtected = "protected-mail"
)

// MailProfileServiceIDs returns the immutable release policy for one managed
// mail profile. The returned slice is a copy so callers cannot widen the
// privileged plan at runtime.
func MailProfileServiceIDs(profileID string) ([]string, bool) {
	var services []string
	switch profileID {
	case MailProfileCore:
		services = []string{"postfix", "dovecot"}
	case MailProfileWebmail:
		services = []string{"postfix", "dovecot", "nginx", "php-fpm", "roundcube"}
	case MailProfileProtected:
		services = []string{"postfix", "dovecot", "rspamd"}
	default:
		return nil, false
	}
	return services, true
}

// MailProfileContainsService reports whether serviceID belongs to the exact
// compiled plan for profileID.
func MailProfileContainsService(profileID, serviceID string) bool {
	services, ok := MailProfileServiceIDs(profileID)
	if !ok {
		return false
	}
	for _, candidate := range services {
		if candidate == serviceID {
			return true
		}
	}
	return false
}
