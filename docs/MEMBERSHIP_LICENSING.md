# Membership and annual server licensing

Approved product model: one CelikPanel product, four existing authorization
roles, and a celikpanel.net member account independent of panel administrators.
Initially a verified member may issue free server licenses. Each license binds
to one verified public server IP and lasts one calendar year from creation.
Existing activated licenses retain their already-granted expiry; legacy unactivated
licenses receive creation plus one calendar year during the additive migration.
Retrying activation preserves the original term. Server transfer preserves the
term and requires the member's password. No payments or automatic billing yet.

The central service uses PHP 8.3/PDO SQLite on the existing portal host. Its
database, sessions and Ed25519 license signing secret live outside httpdocs and
outside atomic website releases. Each portal wrapper pins an immutable private
application release by its source manifest hash, so website rollback selects
the prior application too. Stage it with deploy/stage-membership.py before the
supported portal publisher; never put the private state in a portal archive. Release-signing and license-signing keys are
separate. Clients receive signed entitlements, never database access. Mail is
sent through the configured authenticated SMTP transport; verification/reset
tokens are short-lived, single-use, hashed and never written to access URLs.

Member UI: registration, email verification, sign-in, password reset, licenses,
issue, copy key, renewal and server release. Match the portal's existing
navy/white typography and provide Turkish and English. License secrets can be shown again after owner-password verification. Stored hashes
authenticate activation; authenticated secretbox encryption permits redisplay. The
storage key is domain-separated from the private license signing secret. A signing
secret rotation must re-encrypt existing keys before retiring the old secret.
Legacy hash-only keys cannot be reconstructed: valid activation stores the supplied
key, or the owner can explicitly save their existing key without rotating it. Panel credentials
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


## Activity and IP binding (Alpha 59)

The panel has no startup/hourly licensing poll. Authorized API requests from admin,
reseller, customer and additional-user sessions call one shared activity hook. A
stored session alone does nothing. The first request after the signed daily refresh
deadline starts one coalesced, bounded refresh. Failures back off for 15 minutes;
there is no retry timer. Explicit administrator refresh may bypass the backoff.
Provisioning uses the local signed receipt, and attempts a bounded refresh when
the receipt can no longer authorize the operation. Login and maintenance stay open.
The backend performs this enforcement; it does not depend on browser timers.

The central API takes the source IP only from web-server REMOTE_ADDR, never client
JSON or forwarded headers. Its front proxy must reliably restore the real peer IP.
IPv4-mapped IPv6 is normalized. Private/reserved source addresses are rejected.
The explicit loopback development mail fixture maps its local peer to a fixed test
public IP; this is not enabled on the production HTTPS origin.

The fixed public IP identifies a license's server assignment, not physical hardware.
Shared NAT is unsuitable for distinguishing servers; use a dedicated fixed egress
IP. Dual-stack egress changes require a binding change just like any IP change.
Receipts remain v1 and machine-bound locally for compatibility with installed
clients. Reinstalling at the same bound IP with the key replaces the machine binding
and invalidates the previous refresh credential, preserving key and expiry. Cached
receipts still expire within their original seven-day window or annual expiry.
Legacy machine-bound licenses bind the observed IP after proving the original
machine and key/refresh credential; an already reinstalled legacy server requires
the owner to release its old binding first.

Changing server/IP requires the member password, clears the binding and increments
credential generation, but keeps the key and term. Key replacement is separate and
preserves IP and term. None of these operations stop existing workloads.
