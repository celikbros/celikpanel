# Actionable operation guidance

*Product requirement recorded September 13, 2026 · [Türkçe](OPERATION-GUIDANCE.tr.md)*

**Status: required across the product; implementation and audit are incomplete.**
The current source change covers setup progress with accepted-plan DNS role
context, existing panel-license states, and generic service-operation errors. It
does not certify every operation or provide third-party product-license adapters.

## Requirement

Every program, service, runtime and integration managed by CelikPanel must explain
its current operation, any prerequisite or failure, and the user's next action.
This applies to installation, configuration, updates, verification, recovery and
other supported lifecycle operations, both inside the wizard and on management
pages. A spinner, a generic error, or a message hidden below a long operation list
is insufficient when the user can or must act.

The primary message must identify:

1. What is happening or what is preventing progress, with the affected component,
   server or integration named when known.
2. Who must act and where: this server's administrator, the other server's owner,
   a DNS provider, a software vendor, or a credential/entitlement administrator.
3. The concrete next action, using reviewed names and addresses or verified
   observations where available.
4. What happens afterward: automatic checking, explicit verification, an existing
   recovery action, or a new review after a resolved failure. Promise automatic
   continuation only when the operation implements it.

Present that message and action before the detailed step list. Keep diagnostic
information available without forcing users to interpret internal codes. Turkish
and English messages, mobile layout and keyboard access are required.

## Truth and operation identity

Separate an observed failure from an unmet prerequisite and an unknown result.
An unreachable verification service does not prove that a license is invalid.
A DNS pair that has not passed verification does not prove that the other daemon
is uninstalled. Show the last verified observation and its time when available;
do not invent a diagnosis, progress percentage or completion estimate.

Use the accepted operation's immutable plan and exact child identities for
execution guidance. A later editable draft or an unrelated operation must not
change the explanation of an accepted job. Polling and page refresh are reads;
they must not install components, retry mutations or launch a duplicate job.

Keep a known failure visible while its outcome or recovery is being verified.
Explain that verification separately. Clear or supersede a failure only with
matching newer evidence, not merely because a poll started or a spinner appeared.
Lost responses and uncertain outcomes require reconciliation of the original
operation before a duplicate start or retry can be offered.

## Licensing, credentials and external dependencies

The rule includes programs supplied by other vendors, not just CelikPanel.
When an adapter supports a dependency it must distinguish its meaningful states,
including missing, rejected, expired or revoked licenses; verification-service
unavailability; missing or rejected credentials; insufficient permission;
connection failure; and unsupported or unknown results. Each supported state
needs a stable machine-readable code and a specific translated explanation and
next action. A generic error is an honest fallback for an unclassified result,
not evidence that all these integrations are implemented.

Request secrets through the appropriate protected flow. Never include license
keys, passwords, tokens, private keys or credential-bearing URLs in progress
messages, logs, audit details or diagnostics. Translate and sanitize vendor
responses; a raw response may contain secrets. An action to obtain or renew a
license must not purchase one or disclose credentials automatically.

License and credential checks must not silently stop or remove unrelated hosted
workloads. CelikPanel's own license loss restricts panel use while existing
services continue. Third-party vendor behavior must be reported accurately;
CelikPanel cannot claim control over a vendor's independent license enforcement.

## DNS examples and ordering

- A primary configured with a secondary should identify the expected secondary
  role, nameserver and IP from the reviewed plan. If that peer is not yet prepared,
  direct its owner to prepare it without waiting for this server's entire setup
  to finish. Initial DNS verification can already depend on the peer.
- A secondary should identify its primary and required transfer relationship.
  If the primary is not available, explain the missing prerequisite. If the child
  operation has already failed, show its actual recovery state instead of
  promising that starting the primary will automatically repair every failure.
- If both servers are configured, direct the user to relevant public records,
  addresses, DNS access and transfer permissions. Do not repeatedly prescribe an
  installation that is already complete.
- External DNS shows the records and provider-side action. Standard native DNS
  transfer must not require another CelikPanel or its HTTPS API. Optional remote
  record-management authorization is a separate step with separate guidance.

## Acceptance and remaining scope

For each supported flow, verify normal progress, user-action waiting, failure,
unknown evidence, recovery and completion. Cover reversed server setup order,
reload/reconnect, wrong/missing credentials, relevant license states, unreachable
providers and preserved exact operation identity. Adapter-specific cases must
pass before that adapter is claimed to support them.

The present setup change adds readonly context from the validated accepted plan,
role-aware DNS guidance, panel-license guidance and a service-error fallback.
Detailed vendor-license integration and an exhaustive audit of all lifecycle
screens remain future work. This document makes those requirements durable; it
is not a completed implementation claim.

[D-024](DECISIONS.md), [D-021 and the setup plan](SERVER-SETUP-PLAN.md), and
[D-022 owner independence](OWNER-INDEPENDENCE.md) apply together. This guidance
requirement does not authorize assistant-side live changes or panel updates.
The user must continue to initiate installed-panel updates in CelikPanel as
specified in [AGENTS.md](../AGENTS.md).

### Optional BIND peer deletion guidance (P0.2/P0.4, September 26, 2026)

A locally committed V3 BIND deletion that needs native proof on a parentless
secondary retains the exact operation in `propagation-pending`. The Agent now
classifies selected owner enrollment, authenticated inspection, native
observation, challenge-journal and changed-evidence uncertainty with a bounded
`dns_peer_*` code. The code is stored in the existing service mutation ledger
v1 `error_code` field and returned in an optional V3 RPC `pending_code` field.
There is no ledger version or phase migration. Older generic pending receipts
and unrecognized codes remain **unknown**, never proof of a missing service or
deleted zone. Peer output, SSH errors and credentials are not persisted or shown.

The Panel exposes a reviewed reason only after reconciling the exact pending
Agent job; a typed transport response alone cannot authorize user guidance.
The DNS publication error and domain-deletion 202 response carry the optional
reason, with English/Turkish guidance naming the actor, action and
same-operation retry. The Domains screen treats 202 as pending, keeps an
on-screen explanation and offers a read-only status check. The status check
never retries deletion or starts another mutation; any later user-initiated
delete request must reconcile the accepted V3 lease and exact Agent job. A changed owner configuration
or unreconciled challenge remains pending for owner review. The selected
request/response and guidance tests do not prove the full native peer matrix.

The zone GET still returns 404 after a deleted zone leaves the panel database.
A separate tenant-authorized `GET /api/v1/domains/{id}/deletion-status` now
restores guidance on the Domains screen for a domain whose deletion marker
survives. It reads the marker, desired DNS deletion, current BIND engine,
V3 lease and exact pending Agent job without reconciling or mutating anything.
The endpoint rereads the saved identity after Agent status. Missing, mismatched,
foreign, stale and unrecognized evidence yields an explicit unknown status and
no reviewed reason; a missing deletion marker yields 204. It does not infer a missing service from a failed
probe or absent zone. The response adds no persisted schema fields. Once the
domain row is deleted, this endpoint is intentionally unavailable.

This bounded status path and component/authz tests do not establish the full
native peer matrix or close P0.2/P0.4. A running operation can still become
unknown while the Agent is unreachable, and no GET resumes mutation.

On an exact pending recovery, a reviewed reason is carried into the durable
recovering attempt. A lease expiry with no new peer result, or Agent startup
reconciliation with no newer classified result, retains that reason; a new
reviewed result may replace it. An unrecognized older code stays generic.
A completion wave permits one native challenge. If authenticated inspection
is inconclusive, it returns the reviewed pending reason immediately, rather
than letting later DNS polls mask it with a generic final check. These changes
reuse the ledger v1 fields and do not broaden mutation authority.

### Domain deletion mail stage (P0.2 guidance, September 30, 2026)

Pair 4 (finding P4-2, Arch BIND primary) showed a DNS-only domain deletion stop
at `mail_runtime_cleanup`: Arch ships `/var/mail -> spool/mail`, the Agent's
symlink-free open returned ELOOP, the 202 had no reason and the saved status read
`unknown`. The DNS deletion never started.

- **Scope from records.** The mail stage runs only when the Panel's own records
  show a mail runtime for the domain on this server: mailboxes, forwardings or a
  catch-all for that domain, or a succeeded Panel installation of Postfix/Dovecot
  or a mail profile in the service-operation ledger. Otherwise it is a no-op,
  logged and audited as "no mail runtime for this domain on this server". The
  filesystem is not probed to decide this. An owner-installed mail stack whose
  mailboxes were all deleted before the domain is not covered and keeps its
  files.
- **Root.** The product root remains `/var/mail/vhosts`, resolved through one
  permitted link: a root-owned `/var/mail` whose text is exactly `spool/mail` or
  `/var/spool/mail`, pointing at a root-owned directory reachable without links.
  Installation writes the resolved path into Postfix, Dovecot and the vmail home;
  cleanup resolves the same way and opens the result with no symbolic link.
  Existing configurations with the literal path name the same directory. Any
  other link is refused with a typed error; no separate root receipt is stored.
- **Verified failure.** An Agent-answered mail-stage error returns 202 with
  `reason: mail_runtime_cleanup_failed` and a bounded, redacted `error_line`.
  The failure is kept with the deletion marker in a versioned `panel_settings`
  record (no schema migration) and `GET …/deletion-status` reports
  `status: failed`. A lost reply clears that record and reads `unknown`.
  A passing or skipped mail stage and completed deletion clear it. The Domains
  screen names the failure, the server owner, the mail storage check and the
  existing “Retry this deletion” action, which repeats the same deletion from the
  mail stage. Failures in other stages (site, certificate, ledger) still read as
  `unknown`.

Evidence status: component tests only; native re-run pending (pair 5).

### Scheduled tasks and native cron states (2026-10-01)

Source state with component tests; the native re-run is pending. This comes from
native run upd1: on a fresh Debian 13 `web_mail` server, creating a scheduled task
returned `500 INTERNAL`. The Agent had correctly reported that cron was absent,
and the Panel masked that answer.

- **Cron absent (verified).** Every cron RPC first checks for the `crontab`
  command. When it is missing the Agent returns its fixed text, and the Panel
  answers `409 CRON_NOT_INSTALLED`. The `read` reason covers the list ("cannot be
  shown; no task runs until it is"). The `write` reason covers create, change and
  delete ("nothing was saved").
  - Who acts: the server owner.
  - Next action: install "Scheduled tasks (cron)" from Components, or run
    `sudo apt-get install cron` (Debian/Ubuntu) or `sudo pacman -S cronie` and
    `sudo systemctl enable --now cronie` (Arch).
  - Resume: create or change the task again. Nothing retries by itself.
  - The Agent's line goes to the Panel log only. The Scheduled tasks screen keeps
    this explanation on screen in place of the "no tasks" empty state.
  - Before this change, a missing cron listed as "no tasks" and a change reported
    "cron job not found". Both hid the reason.
- **Other cron failures** (tenant proof, crontab write) remain an unclassified
  `INTERNAL` answer. That is an honest fallback, not a diagnosis.
- **Setup.** Site-hosting profiles plan a `service` step for `cron`. It uses the
  same install and verify path, operation receipt and failure codes as the other
  components, for example `service_install_failed` with the component guidance.
  - When any cron implementation already exists, the Agent changes nothing and
    returns `preserved_existing`. The step then succeeds as kept without requiring
    the unit to be running.
  - An installed catalogue unit makes review show the component as kept, with no
    step.
  - The final setup readiness check does not re-verify cron. An accepted plan
    from an older build has no cron step and is not held back.
- **Removal.** The generic uninstall is refused in both the Panel and the Agent
  with `409 NATIVE_CRON_REMOVAL_REFUSED`, before any mutation.

The same audit of domain handlers mapped two fixed Agent "busy" answers to the
existing `409 HOST_MUTATION_BUSY` (`agent_mutation_active`: wait for the other
CelikPanel change, then retry) instead of `INTERNAL`:

- the mailbox password change, while mail configuration is locked;
- Let's Encrypt issuance, while another site certificate operation runs.

The other masked Agent conditions it found remain listed for follow-up. They are
not covered by this note.

### Component install failures in setup (upd1 finding P2, 2026-09-30)

Source state with component tests; the native re-run is pending (upd2). Native
run upd1 stopped a fresh Arch `web_mail` setup at `05-mail_profile` with only
`service_install_failed` ("The service could not be installed and verified.").
A read-only reproduction on one disposable Arch guest found the cause in the
Panel log only: `failed in profile/webmail/dovecot/configuring: … mail stack
configuration: dovecot: dovecot is not installed`. Dovecot was installed
(`dovecot 2.4.4-1`); Arch's package ships a single `/etc/dovecot/dovecot.conf`
and no `conf.d`, which the Agent's mail configuration requires.

- **Unmet prerequisite, refused up front.** Automatic Dovecot installation is
  closed for the `pacman` family with a specific catalogue reason
  (`core.DovecotPacmanLayoutReason`). Setup review therefore returns the typed
  blocker `server_setup_service_unsupported:dovecot` for any plan with mail on
  Arch, before any mutation. The wizard shows `setup.blocker.mailUnsupported`:
  mail cannot be set up automatically on this distribution; the server
  administrator chooses Web hosting or installs mail with the operating
  system's own tools. Web hosting on Arch is unchanged. Packages still observe an
  existing Arch Dovecot; removal is not affected.
- **Verified failure, named.** An install failure (`service_install_failed`,
  `mail_profile_install_failed`, `node_runtime_install_failed`) now carries the
  component, the step (`preflight`, `package_install`, `configure`,
  `unit_start`, `verify`, from the operation's last durable phase) and one host
  line: the package manager's `error:`/`E:` line for a package transaction,
  otherwise the first line of the reason. The line is bounded to 180 characters
  with URL user/path/query, hash-shaped tokens and `key=value` secrets removed.
  It is stored under `failure` in the failed row's existing `result_json` (no
  schema migration; older rows keep only code and message) and returned as
  optional `component`/`step`/`detail` on the service operation error and the
  setup execution error.
- **Wizard.** For such a failure the guidance names the component and step, shows
  "The server reported: …", names the server administrator and the action for
  that step (package manager problem, service status and log, or the reported
  reason), then "Review a revised plan" to continue with completed steps kept
  and no automatic retry, and for mail the Web hosting alternative. Layout is
  unchanged; a failure without guidance keeps the previous generic texts.
- **Agent text.** A Dovecot without `conf.d` now reports that layout instead of
  the false "dovecot is not installed", and leaves `dovecot.conf` unchanged.

Not done: Arch mail support itself. Creating `conf.d` is not enough because the
packaged main file sets mail storage, PAM login and TLS after its include;
supporting it means adopting the packaged main file, an owner-configuration
decision (D-022), plus native checks of mail TLS, submission and Roundcube PHP
extensions on Arch.
