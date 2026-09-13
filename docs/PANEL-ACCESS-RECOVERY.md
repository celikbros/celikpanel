# Panel connection recovery

September 13, 2026 · [Türkçe](PANEL-ACCESS-RECOVERY.tr.md)

Status: Alpha76 release source. Installed panels are updated by their owner.

An interrupted access-status request used to become a denied license decision,
redirect the browser to `/activate`, and mount a license-key form. An operation
could still be running on the server while the browser displayed an old phase.

The access gate now separates failed reads from confirmed denial. It retains an
already verified decision only for the same username and only until the original
server deadline. Unknown or expired access still blocks management, but keeps the
current URL and offers connection recovery. Explicit denial and the backend
`license_required` response still lock access. Focus and periodic checks share
an in-flight request. Failed initial license-status reads do not show a key form.

Disconnected operation overlays offer a full page reload. Reloading preserves the
existing durable operation lookup; it neither dismisses the mutation lock nor
starts another operation. This also allows a top-level browser navigation to
surface a certificate warning that a failed background request cannot display.

For automatically managed panel TLS, the original usable self-signed bootstrap
certificate remains the default for clients without SNI (including literal IPs).
Hostname SNI receives the managed certificate. Explicit TLS certificates and
custom TLS callbacks are respected. Missing, expired or unusable bootstrap
material is never regenerated here and does not block the managed certificate.
IP access can still produce a browser certificate warning; no browser checks,
session isolation or API authorization are disabled.

The public GET `/api/v1/panel/access-address` returns only the hostname from the
active managed certificate, or an empty hostname. The login and recovery screens
can offer that address without redirecting automatically. Request Host headers,
query parameters and browser storage cannot supply the destination. Other methods
are not public and the handler rejects them. No license/agent request is needed.

## Validation and limits

- 441 frontend tests passed, including cached-decision deadlines, rejection,
  request races, safe address links and operation reload behavior.
- Frontend build and bundle budgets passed.
- Focused panel TLS, licensing, authentication and startup tests passed on Linux
  with Go 1.26.5. The broader Windows run exposed the existing directory-sync
  limitation in `TestLicenseActivityAcrossAuthenticatedRoles`; the Linux run passed.
- Eight local Chrome fixtures covered EN/TR login and unavailable-access recovery
  at 1440px and 390px, including retry, reload, URL retention and no mutation calls.
  These use simulated API replies, not production servers.

The supplied Frankfurt journal shows work progressing through PostgreSQL, Redis
and mail after the browser showed a phpMyAdmin disconnect. It does not establish
the cause of the lost HTTP response or prove final setup completion. This change
fixes recovery behavior; it does not claim every source of network interruption
has been diagnosed. Installed panels are updated only by the user through the
panel update interface.
