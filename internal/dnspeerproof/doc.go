// Package dnspeerproof defines a versioned, transport-independent contract for
// an authenticated native inspector on a BIND catalog secondary to report that
// a deleted zone is no longer loaded. It does not authenticate peers, inspect
// BIND, grant mutation authority, or replace the existing catalog/AXFR checks.
package dnspeerproof
