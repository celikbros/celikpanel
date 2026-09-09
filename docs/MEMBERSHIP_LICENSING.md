# Membership and annual server licensing

Approved product model: one CelikPanel product, four existing authorization
roles, and a celikpanel.net member account independent of panel administrators.
Initially a verified member may issue free server licenses. Each license binds
to one server identity and lasts one calendar year from its first activation.
Retrying activation preserves the original term. Server transfer preserves the
term and requires the member's password. No payments or automatic billing yet.

The central service uses PHP 8.3/PDO SQLite on the existing portal host. Its
database, sessions and Ed25519 license signing secret live outside httpdocs and
outside atomic website releases. Each portal wrapper pins an immutable private
application release by its source manifest hash, so website rollback selects
the prior application too. Stage it with deploy/stage-membership.py before the
supported portal publisher; never put the private state in a portal archive. Release-signing and license-signing keys are
separate. Clients receive signed entitlements, never database access. Mail is
sent through the host's existing local mail transport; verification/reset
tokens are short-lived, single-use, hashed and never written to access URLs.

Member UI: registration, email verification, sign-in, password reset, licenses,
issue, copy key, renewal and server release. Match the portal's existing
navy/white typography and provide Turkish and English. License secrets are shown
on issue/rotation only; stored hashes authenticate activation. Panel credentials
remain on the customer's server and are still created in the terminal.

License authentication is not panel authentication. The installer requests a
license without echo or command-line arguments. Activation persists before
continuing installation; an interrupted installation reuses its identity and
entitlement. Panel administrators may activate/refresh in Settings. License
restrictions apply to new provisioning, not login, renewal, backups, existing
site/mail serving, deletion, or security maintenance. Invalid/missing/expired
entitlements cannot authorize new provisioning. A central-service outage does
not invalidate a still-valid locally verified entitlement.

Server transfer invalidates the old binding centrally. Signed cached leases
remain usable only until their documented offline deadline. An unreachable
service after that deadline is reported as verification unavailable, not as an
expired annual license. Existing workloads are never stopped by this layer.

Before publication: transactional activation/transfer/renewal tests, auth/CSRF/
token abuse tests, signed entitlement and server-binding tests, provisioning
route coverage, interrupted installation, role isolation, and one bounded
desktop/mobile TR/EN browser inspection plus one correction pass. Production
mail delivery and PHP web-handler capabilities must be verified explicitly;
CLI PHP availability alone does not prove the web handler.
