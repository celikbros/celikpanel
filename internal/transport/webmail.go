package transport

const defaultWebmailSocketPath = "/run/celikpanel-webmail.sock"

// WebmailSocketPath is the authenticated local boundary between the panel
// proxy and nginx's Roundcube vhost. The production path lives directly below
// root-owned /run: an unprivileged site user cannot create, replace or remove
// its socket node and impersonate the webmail upstream.
//
// This boundary is intentionally not configurable: both the panel and the
// privileged agent must agree on a path directly below root-owned /run. Tests
// that need a temporary socket inject that path into their local helper rather
// than changing the production boundary through process environment.
func WebmailSocketPath() string {
	return defaultWebmailSocketPath
}
