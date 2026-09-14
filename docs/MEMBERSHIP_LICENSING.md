# Membership and annual server licensing

*Current-policy reconciliation: September 14, 2026 · [Türkçe](MEMBERSHIP_LICENSING.tr.md) · D-025*

This document distinguishes the annual license term, the current short access
verification window, and historical behavior. It records existing code and
required recovery work; it does not change license policy or authorize an update
on an installed panel.

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
entitlement. Panel administrators may activate/refresh in Settings. The current
HTTP access gate applies to normal panel management across all four roles, with
the exact recovery exceptions listed below. Older provisioning-only descriptions
and broad maintenance exemptions are historical, not the current API contract.
A central-service outage retains the existing signed receipt; it cannot extend
that receipt's effective access deadline.

Server transfer invalidates the old binding centrally. Signed cached leases
remain usable only until the effective access deadline described below. If the
annual term is still valid but that deadline has passed, an unreachable verifier
produces `verification_unavailable` in the license manager. The current access
endpoint reduces that state to `can_use_panel=false`, which routes the browser
to activation; the UI does not yet preserve the manager's distinction from
annual expiry. Existing workloads are not stopped by this HTTP gate.

Before publication: transactional activation/transfer/renewal tests, auth/CSRF/
token abuse tests, signed entitlement and server-binding tests, provisioning
route coverage, interrupted installation, role isolation, and one bounded
desktop/mobile TR/EN browser inspection plus one correction pass. Production
mail delivery and PHP web-handler capabilities must be verified explicitly;
CLI PHP availability alone does not prove the web handler.

## Current enforced access policy

Source: [license manager](../internal/licensing/license.go),
[panel access gate and exceptions](../cmd/panel/license.go), and
[browser access gate](../web/src/components/LicenseOnboarding.tsx).
The annual term and this access-verification window are separate:

| Decision | Current implementation |
|---|---|
| Effective authorization deadline | The earliest of annual expiry, the signed `offline_until`, and `issued_at + 60 seconds`. The signed format can carry a longer deadline; the current client still applies the one-minute cap. |
| Ordinary refresh eligibility | At the earlier of signed `refresh_after` and `issued_at + 45 seconds`. Requests share the manager's serialized refresh path. There is no server timer polling an idle installation. |
| Failed refresh retry | Ordinary requests back off for 30 seconds. Explicit administrator refresh may bypass this backoff; it does not bypass signature or binding verification. |
| HTTP refresh budget | The license client has a 15-second timeout; request cancellation can end it earlier. |
| Missing/expired/rejected/unverifiable entitlement | Normal management is denied. The manager retains distinct status values, but the access endpoint currently reduces them to `can_use_panel=false`. |
| Browser behavior | A failed access request retains only a still-valid decision for the same user, without extending its deadline. Unknown access shows connection recovery; a server denial or `license_required` response locks management. |

The current authenticated exceptions are deliberately narrow. All role,
ownership and method checks still apply:

- Session identity and access-status reads; logout, password change and ending
  impersonation remain available.
- License reads, activation and refresh remain available to administrators.
- Administrators can read panel version, update check/status and host-mutation
  readiness, and use the signed-update start/abandon endpoints. Release trust,
  operation identity and mutation admission remain enforced. The user must
  initiate every installed-panel update in the panel interface; API capability
  does not authorize assistant-side installation.

These exceptions do **not** provide general panel access to backups, deletion,
service configuration or other maintenance when the entitlement cannot authorize
management. Existing native workloads and their independent schedules are not
stopped by this HTTP license gate. Full operation after removal of management
binaries is a separate, unfinished [owner-independence requirement](OWNER-INDEPENDENCE.md).

An unreachable verifier after the effective deadline produces
`verification_unavailable` in the manager; it is not proof that the annual
license expired. Explicit rejection of this installation's refresh can invalidate
its local receipt. A mistyped replacement key must not revoke a currently valid
binding. No additional offline grace is granted by this document.

## Required recovery behavior — not yet fully implemented

[D-025 and the resilience contract](RESILIENCE-CONTRACT.md) require typed access
reasons and separately scoped authenticated status/recovery capabilities.
A failed observation must not be converted into a different diagnosis: verifier
unavailability must not imply that the owner needs a new license key; a failed
session lookup must not be treated as a confirmed unauthenticated response.

The current boolean access response and shared `license_required` denial do not
preserve that distinction across the frontend/backend boundary. An authenticated
recovery/status surface independent of the normal Agent and candidate startup
also remains open work. Its implementation must preserve identity, server and
operation scope, sensitive-data boundaries, current license deadlines and the
user-only panel update rule. This is not permission to extend leases, bypass
licensing or expose unrestricted privileged actions.

## Historical activity policy (Alpha 59) — superseded

The following timing and admission description records Alpha59 behavior. It is
retained to explain older installations and receipts; use the current policy
above for present code. The daily refresh, 15-minute failure backoff, seven-day
cached access and broad maintenance access are no longer the current client
contract.

Alpha59 had no startup/hourly licensing poll. Authorized API requests from admin,
reseller, customer and additional-user sessions used one shared activity hook.
The first request after the signed daily refresh deadline started one coalesced,
bounded refresh, with a 15-minute backoff after failure and no idle retry timer.
Provisioning used the local signed receipt; login and maintenance remained open.
This paragraph is historical evidence, not a promise of those exemptions today.

## IP binding and retained receipt compatibility

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
and invalidates the previous refresh credential, preserving key and expiry.
The signed v1 format still permits an offline deadline up to seven days after
issuance, bounded by annual expiry; current access additionally applies the
one-minute cap described above. Older receipt formats do not grant a current
client a longer authorization window.
Legacy machine-bound licenses bind the observed IP after proving the original
machine and key/refresh credential; an already reinstalled legacy server requires
the owner to release its old binding first.

Changing server/IP requires the member password, clears the binding and increments
credential generation, but keeps the key and term. Key replacement is separate and
preserves IP and term. None of these operations stop existing workloads.
