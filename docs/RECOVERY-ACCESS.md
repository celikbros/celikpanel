# Recovery access and truthful observations

*[Türkçe](RECOVERY-ACCESS.tr.md) · P0.2 partial implementation · 2026-09-14*

An unavailable Agent no longer prevents the panel from starting its HTTPS recovery
surface. An unavailable authentication or license read no longer means that the
user has logged out or must activate a license. This is a scoped structural change
under D-025, not completion of the [resilience contract](RESILIENCE-CONTRACT.md).

## Access boundaries

The panel first opens its existing database, session store, secret key and TLS
material. It then binds one listener with a separate, fixed startup mux, before
connecting to Agent or starting ordinary reconciliation and background management.
This mux serves the initial interface, existing login/TOTP/logout and current-user
handlers, panel availability, license access and the configured access-address hint.
Only administrators can read an exact update observation. It does not admit host
management, webmail/database proxies, update starts, remote machine credentials or
license activation writes. The ordinary router remains inaccessible until startup
explicitly opens the atomic gate.

Agent connection retries have an individual deadline and bounded backoff. Missing
Agent does not make the panel exit after a global connection timeout. Listener
failure and owner cancellation close the pending connection; token reads reject
nonregular files and oversized input without blocking on a FIFO. Existing valid
regular-file aliases remain compatible. An unreadable existing token is not regenerated. The token format and workload
service configuration are unchanged; atomic first-token creation remains separate
work.

A missing/expired session or definitively invalid user remains a denial. Repository
and session-store read failures return `503 AUTH_STATUS_UNAVAILABLE`. The distinction
also covers additional-user parent resolution, the second current-user read and
sign-in/TOTP identity resolution; consumed TOTP attempts are not resurrected.

`GET /api/v1/license/access` retains `can_use_panel` and `valid_until`, adding
`state` and `observation`. Known missing, expired and invalid states may require
activation; local unreadability and remote verification unavailability cannot.
The existing one-minute verification deadline remains unchanged. The frontend may
retain a positive decision only until its exact deadline, and management still
requires the server's authorization on every request.

## Shared observation contract

| Surface | Contract |
| --- | --- |
| Availability | Authenticated `GET /api/v1/panel/availability`, schema `celikpanel-panel-availability/v1`, `starting` or `ready`; no operation details. |
| Recovery | Administrator-only `GET /api/v1/recovery/status?request_id=<32 lowercase hex>`, schema `celikpanel-recovery-status/v1`; no Agent RPC or version negotiation. |
| Producer record | `/var/lib/celikpanel-recovery-observations/<id>.status`, fixed-key `celikpanel-recovery-observation/v1`, at most 2 KiB, root:celikpanel 0640 under 0750. |
| Native binding | Root-only immutable association of exact request, target commit, update token and snapshot. Never returned to the browser and never used as mutation admission. |

The worker records accepted/running and its proven result. The native updater
binds the worker's kernel cgroup identity before quiesce; the recovery runner uses
only this exact binding. Terminal `update_verified`/`rollback_verified` observations
follow the existing final proofs. A late worker error cannot erase terminal proof.
Shell and Go producers share a root:root 0600 publication lock, including when the
Agent runs with a non-root primary group. Unsafe metadata, another request, a stale
record or unsupported layout is not silently repaired. Observation failure does
not alter the actual update/rollback result or authorize another operation.

The HTTP reader validates paths, metadata, regular file type, link count, size,
schema and record stability. It returns only bounded reason codes and timestamps,
not paths, credentials, transaction tokens or raw diagnostics. A missing/unsafe
record means `observation=unavailable`, no phase and no terminal proof. A known
record is the last producer observation, not current process liveness.

The eager recovery UI preserves the exact saved ID through reload and a failed lazy
update module. Unknown auth cannot reveal administrator observations; only a
confirmed 401 returns to sign-in. Failed or out-of-order polls retain the last
verified result and failure with its timestamp. Polling issues GET requests only.

## Acceptance evidence and limits

- Final pinned Go 1.26.5 package run: panel 2530, auth 27, licensing 19,
  repositories 25 and transport 57 tests passed. Four opt-in panel fixtures and
  two licensing environment-dependent cases skipped; the permission-denied read
  was separately tested as `nobody`. These skips are not native acceptance.
- Pinned Go 1.26.5: real separate production `main()` process with isolated SQLite,
  HTTPS, a real administrator password/session and no Agent. Login and same-session
  reads worked; management stayed 503; one process exited cleanly on SIGTERM.
- HTTP tests cover anonymous/tenant/additional-user denial, exact ID/query and method
  validation, startup route isolation and explicit admission. Dial tests cover
  cancellation, per-attempt expiry, listener failure and late-client closure.
- Native observation tests use actual shell/Go publication, inherited locks and
  different process groups. File and schema negatives cannot produce success.
  Worker tests require actual final proof before a successful observation and
  preserve the worker result when observation publication is rejected.
- The real recovery runner is exercised by its disposable contract fixture with
  the observation helper in the verified retained release. A failing child records
  `recovery_required`; successful compensation and final proofs record `recovered`,
  retaining the previous failure and unchanged immutable binding. The child and
  systemd are fixtures here; this is not native rollback-body evidence.
- Focused panel access/startup race tests passed. A final mobile reload retained the
  exact operation ID while its observation endpoint returned 503, without activation
  redirection or any mutation request.
- Frontend: 458 tests and production build passed. Critical boot stayed within the
  existing budget (269.54 KiB raw / 84.68 KiB gzip). Local Chrome tested 16 combinations:
  EN/TR, 1440/390 px, unavailable auth, Agent-starting, unavailable license and failed
  lazy update module. No activation redirect, mutation request or horizontal overflow;
  a later 503 retained the previous verified rollback. Desktop/mobile screenshots
  were inspected. API replies in this browser matrix were fixtures, not live servers.

The separate HTTP process test and shell fixtures do not prove a complete native
upgrade/automatic-rollback lifecycle. No installed panel was updated for this work.
The initial panel executable, database migrations/session database, secret key,
TLS files and eager JS bundle still have to load. A separate versioned recovery
executor/access surface, recovery interruption/reboot, all workload probes and the
full P0.2/P0.3 native matrix remain open.

Legacy workers did not publish the initial exact-ID record. Their first upgrade
cannot retrospectively gain this observation binding: it remains unavailable.
No latest-snapshot inference is made. Existing signed-update admission, rollback
manifests, owner authority and owner-only installed-panel updates are unchanged.


## Operating-system transition guidance (2026-09-16)

D-025 invariants 2, 4 and 5 / P0.2: a bound recovery invocation that defers for
systemd `initializing`, `starting` or `stopping` now records an optional typed wait.
The owner sees the last recorded prerequisite, that no action is needed for this
wait, and that the native timer checks the **same operation** again. The UI and
owner CLI retain the previous failure, observation time and `terminal_proof=none`.
No polling request starts recovery or extends authorization.

The canonical eight-field `celikpanel-recovery-observation/v1` record, kit protocol
and database schema remain unchanged. A separate `<id>.wait` sidecar uses
`celikpanel-recovery-wait/v1`: exactly five newline-terminated fields (`schema`,
`request_id`, `observation_identity`, `observation_sha256`, `waiting_for`), at most
2 KiB with the same regular-file/owner/group/mode/link checks as the status record.
The identity is the status file's device, inode, size, nanosecond mtime and ctime,
formatted by GNU stat in UTC; SHA-256 binds its exact bytes. Both publications use
the existing producer lock and atomic rename. A later status replacement invalidates
the hint even if an old producer writes identical bytes in the same second.

The reader exposes only the allowlisted `waiting_for` value on a known recovering
status. It never exposes the file identity or digest. Missing, stale, unsafe or
unknown optional data is ignored while the verified v1 status remains available.
Optional publication failure does not prevent the native runner from releasing
its lock and deferring. Old readers ignore the sidecar; old producers need not
write or delete it. The additive HTTP/CLI JSON field is optional; old browsers
keep generic recovery guidance, and new browsers accept old v1-only responses.
A terminal proof always wins. A wait is a recorded observation, not a current
liveness guarantee or a new permission to run a mutation.

Scoped validation: real shell-to-Go publication covers all three waits, old v1
reads, identical-byte v1-only republication, terminal dominance and malformed,
oversized, wrong-operation, wrong-identity/hash, unsafe-mode, symlink and FIFO
sidecars. Runner contract fixtures verify unchanged markers and request binding,
previous failure, released locks, same-operation continuation and best-effort
publication failure. CLI and administrator-only HTTP tests preserve that meaning.
The 460 web tests and production build pass. Local Chrome checks EN/TR at 1440
and 390 px, reload and a failed subsequent read: same ID and failure retained,
GET-only requests, no page errors or horizontal overflow. API responses in this
browser check and systemd readiness in the runner check are fixtures.

The earlier native AB/AD trials do **not** establish this new UI binding; their
fixture updater did not publish a real worker association. The subsequent
[Debian AJ acceptance](../deploy/e2e/release-recovery/BOUND-WORKER.md) now proves
a genuine Go worker association through a verified native snapshot, worker kill,
recovery reboot, automatic rollback and matching root CLI/authenticated HTTP.
The actual HTTP-backed EN/TR browser reader preserves that terminal result and
exact request across reload after a declared lazy-bundle failure. This is a
schema42-to42 fixture-trust trial, not UI update-start or production-signing
admission. AJ observed no native `waiting_for` sidecar. The subsequent
[Debian AK trial](../deploy/e2e/release-recovery/BOOT-WAIT.md) proves actual
`starting` wait publication, root CLI guidance and a native timer retry of the
same request to verified rollback. The old wait remains but is not exposed after
terminal proof. Other native waits and HTTP/browser wait access remain open. See the [scoped evidence summary](../deploy/e2e/release-recovery/BOUND-WORKER-AJ.json).
This does not close P0.2 or the full resilience matrix. No production release or
installed owner-panel update is part of this change.


The [automatic recovery pause contract](RECOVERY-DISPATCH-BUDGET.md#compatible-pause-guidance-2026-09-22)
adds an optional exact-status hint for three spent automatic admissions. CLI and
administrator UI explain owner action and the supported one-time continuation.
Producer/reader, authorization, terminal precedence and local browser checks pass;
fresh native hint-to-browser and stopped-Panel access acceptance remain open.

[Debian AO native acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ao-native-pause-guidance)
now proves the new exact-status pause hint reaches the selected CLI in EN/TR after
three interrupted native attempts, and retained stale guidance loses to verified
rollback after one owner continuation. Old binaries are restored and authenticated
HTTP agrees afterwards. This closes native Debian hint-to-CLI acceptance; it does
not close browser access while Panel is stopped or the full P0.2/P0.3 matrix.

## Owner-operated independent browser view (source implementation)

D-025 invariants 2, 3, 5, 6 / P0.2. `recovery view` adds an optional, temporary
browser reader to the native owner CLI. It starts no update or recovery, opens no
application database and needs no Panel, Agent, license verifier, TLS material,
web directory or Node runtime. HTML/CSS/JS are embedded in the recovery binary.
The existing observation v1 and runtime protocol are unchanged; this is an
additive command. Older binaries reject it. The installed launcher must include
this command before it can be used; no installed owner panel was updated here.

The owner starts it through an interactive root/authorized-sudo SSH session with
loopback forwarding. Replace SERVER and OPERATION with the actual server and
exact 32-character operation ID:

```sh
ssh -t -o ExitOnForwardFailure=yes \
  -L 127.0.0.1:2084:127.0.0.1:2084 owner@SERVER \
  sudo /usr/libexec/celikpanel/recovery view --request-id OPERATION --lang en
```

Open `http://127.0.0.1:2084/` locally and enter the temporary code displayed in
that SSH terminal. Only the controlling terminal receives the 256-bit random
code; there is no stdout/stderr, journal, file, URL or browser-storage copy.
The browser holds it only in memory and sends it in an authorization header over
the SSH-forwarded connection. Reload or **Lock this view** clears browser access;
Ctrl+C/SIGHUP/SIGTERM closes the listener. Access ends after 30 minutes without
renewal. A fresh view needs a new owner command. Port conflicts fail before code
delivery; `--port` may choose another unprivileged port, with the same forwarding
port on both ends. There is no wildcard-address option or unattended secret path.

The listener binds IPv4 loopback only. Exact Host/origin checks, cross-site
rejection, a strict CSP, no-store responses, header/time limits and constant-time
code verification protect the reader. Before authorization no operation data is
read. Only GET for the embedded assets and the single fixed `/status` route is
accepted; query-based scope changes, mutations and proxy routes are rejected.
No credential, transaction token, snapshot path or raw service diagnostic is
returned. The current observation is not a live-health claim. Unknown reads retain
the browser's last known result; verified terminal results dominate stale later
reads. The view uses the same EN/TR owner guidance as the native CLI.

Local validation: race-enabled recovery package, closed option/host/origin/method/
expiry authorization tests, a separate real HTTP process with no database/Agent/
TLS, and actual production entry through a controlling pseudo-terminal. The latter
proved private code delivery, empty stdout/stderr, anonymous 401, honest unavailable
observation and clean SIGTERM exit. Chrome EN/TR at 1440/390 px exercised the real
HTTP handler, wrong-connection retention, lock/reload clearing, GET-only traffic,
empty storage and no overflow/page errors. Browser observations were fixtures.

**Open:** verified installed-kit execution during an actual stopped-Panel recovery,
SSH-tunnel end-to-end acceptance, upgrade/old-launcher compatibility and automatic
same-address browser access. This owner-operated fallback does not close those
items or P0.2. It changes no normal authentication, license policy, service start
guard, transaction lock, host firewall, DNS or workload lifecycle.


[Debian AP independent browser acceptance](../deploy/e2e/release-recovery/DISPATCH-BUDGET.md#debian-ap-independent-owner-browser-acceptance)
now verifies the installed recovery reader while both normal Panel and Agent are
stopped. An owner SSH tunnel serves the actual exhausted-operation observation
in EN/TR desktop/mobile, with temporary-code authorization and no budget change.
Subsequent supported owner continuation restores baseline services and matching
CLI/authenticated HTTP results. This closes that Debian fallback boundary only;
automatic normal-address access, Arch/historical-launcher compatibility and the
remaining P0.2/P0.3 fault matrix stay open. The earlier local-only statement above
is superseded for this exact AP boundary, not relabeled as full access acceptance.
