package transport

import "os"

const defaultWebmailSocketPath = "/run/celikpanel-webmail.sock"

// WebmailSocketPath is the authenticated local boundary between the panel
// proxy and nginx's Roundcube vhost. The production path lives directly below
// root-owned /run: an unprivileged site user cannot create, replace or remove
// its socket node and impersonate the webmail upstream.
//
// CELIKPANEL_WEBMAIL_SOCKET exists for isolated development and tests. The
// privileged agent validates the resolved value before placing it in nginx
// configuration.
func WebmailSocketPath() string {
	if path := os.Getenv("CELIKPANEL_WEBMAIL_SOCKET"); path != "" {
		return path
	}
	return defaultWebmailSocketPath
}
