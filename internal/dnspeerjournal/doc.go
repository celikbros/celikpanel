// Package dnspeerjournal stores the one-time, operation-bound challenge used by
// optional native BIND peer deletion inspection. It is a separate v1 sidecar,
// not a DNS engine receipt or the service-mutation ledger.
//
// The caller must hold and verify the external host and publication locks and
// prove an active running/recovering ledger admission with the exact request,
// owner and ledger attempt. Pending with no ActiveRequestID is not authority.
// Publish must finish before any network exchange. Only an authenticated,
// validated native peer response may invoke ConsumeOnce. The caller then
// rechecks current ledger, enrollment and native state before terminal success.
//
// After a process restart or restored backup, the caller always mints a fresh
// cryptographically random nonce and increasing proof attempt under the active
// admission. Never reuse an outstanding request returned by Read: backup
// restoration can roll this sidecar backwards and this package cannot infer
// that from its own file. A consumed-before-terminal crash needs a fresh proof.
// Retire requires exact terminal-ledger proof. Any abandoned private stage blocks proof work until owner review; a crash
// during staging has no automatic cleanup or silent retry.
// Missing or unsafe storage is never interpreted as successful deletion.
//
// Direct privileged owner edits outside the external lock protocol may produce
// an unknown publication. Atomic exchange retains a displaced before-image
// under a private stage name if a changed file is detected; owner inspection is
// then required. The package cannot claim full power-loss durability on a
// filesystem that does not honor file and directory fsync.
package dnspeerjournal
