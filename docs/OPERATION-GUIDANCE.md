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

### Candidate panel start failures during an update (2026-09-30)

Source state with component and contract tests; the native run is pending. Two
typed update causes now carry their own guidance in the root CLI
(`recovery status`), the owner SSH view and the recovery screen, EN and TR.

- **`candidate_panel_startup_check_failed`** (verified failure, phase `active`).
  The new version's panel failed its read-only start check before anything was
  switched on. The server is returned to the previous version automatically and
  the previous version keeps running.
  - Who acts: nobody on the server.
  - Next action: report the reason line shown for this update on the panel's
    update page.
  - Resume: while recovery runs, the same operation is checked again; after the
    verified rollback there is nothing to resume.
- **`panel_start_unverified`** (verified failure, phase `completion`). The update
  was applied, but the new version's panel did not stay running and answer on
  its own address within the bounded wait.
  - Who acts: the server owner.
  - Next action: read `sudo journalctl -u celikpanel-panel -n 50` on the server.
  - Resume: completion is retried automatically up to its limit; after that the
    recovery log (`sudo journalctl -u celikpanel-release-recovery.service
    --no-pager -n 50`) shows the one-time same-operation retry command. There is
    no supported return to the previous version from this point.

The cause is shown only while the update's own failure is the latest recorded
failure; a later recovery failure, a wait or paused recovery keeps its own text.
Unknown causes and older browsers or CLIs keep the generic `update_failed` text.
The offline page at the panel address now also shows the owner SSH view command
(`recovery view --request-id …` with the `127.0.0.1:2084` forward). It cannot show
live status while the panel is stopped.

Still open: a panel that passes the check, starts and fails later is completed
forward; no rollback after `completion.pending`; no live browser status at the
panel's address while the panel is stopped.

### After an update that was rolled back (P0.2, 2026-10-01)

Source state with component and contract tests; the native run is pending. From
the upd2 Debian 13 run (findings O1-O4), where a defective candidate failed in
phase `active` and the server returned to the old release by itself.

- **Update notice after a failed update.** The primary text now comes from the
  same request's recovery observation (`GET /api/v1/recovery/status`, a read that
  never starts or retries anything), not from the worker's raw summary.
  - Rollback verified (`recovered`): the update to the target version did not
    complete; the server was returned to the previous version automatically and
    runs it now. Then the cause: the typed code (`candidate_panel_startup_check_failed`,
    `panel_start_unverified`) or "failed before it completed; no more specific
    cause recorded". Who acts: nobody on the server. Next action: do not start
    the same version again until a corrected version is published; if the
    server's message names a problem on this server, fix it first. Resume:
    nothing resumes by itself; a newer version appears in "Check for updates".
  - Still recovering, waiting, paused or needing attention: the recovery
    screen's texts for that state, and the notice keeps reading the same record.
  - No record yet: "reading", then "not known which version the server runs";
    reviewed pre-mutation texts (package manager busy, not accepted) stay first.
  - The server's raw summary is shown only as a secondary "The server reported: …"
    line, never as the only or the primary text.
- **A version that already failed here.** `GET /api/v1/panel/update/check` has
  an optional `previous_attempt` (`request_id`, `phase`, `failure_code`,
  `finished_at`) for the offered target commit, read from the native recovery
  observations. It is present only when the latest recorded attempt to that
  commit ended `failed` or `recovered`. The update card then says, before the
  Start button, when this version was tried and that the server was returned
  to the previous version (or that it did not complete); that starting it again
  repeats the same update unless the cause was fixed; and the recorded cause if
  typed. Start stays available with the same confirmation.
- **Root CLI.** Every state opens with plain owner guidance (what happened,
  what the server runs, who acts, next action). "The producer recorded…",
  "Terminal proof: none", "Previous failure: update_failed" and "Recorded cause"
  are gone; internal tokens are on one final line, "Recorded state (for
  support): phase=… reason=… proof=…". `--json` is byte-identical, except that a paused recovery whose sidecar names a typed cause gains one final key (see below).
- **Rollback journal.** "Artifact source commit: unknown" is replaced by the
  restored release's source commit when the restored Agent's build record
  (`bin/agent-native-contract.json` in the manifest-verified snapshot) binds
  exactly the installed Agent bytes; otherwise it says the commit is not
  recorded in the snapshot. The snapshot name (`…-from-unknown-to-…`) and its
  `commit` file are unchanged: `rollback.sh` validates both as canonical.
- **Paused recovery with a typed first cause.** When automatic recovery is
  paused on its retry limit and the update's first-cause sidecar names a typed
  cause, the pause guidance (recovery journal and the one-time same-operation
  retry command) stays and is preceded by what is wrong and where to look. For
  `panel_start_unverified`: the new version's panel did not come up; read
  `sudo journalctl -u celikpanel-panel -n 50`; retrying repeats the same start
  until the cause is fixed; no supported return to the previous version. For
  `candidate_panel_startup_check_failed`: the automatic return to the previous
  version did not finish; the recovery journal shows what stopped it. Root CLI
  and recovery screen, EN and TR. The sidecar is only read; the recovery status
  gains an optional `first_failure_code`, present only while paused.
- **Start guard log.** While a release marker holds the Panel and Agent and no
  start authorization exists (for example after a reboot), the log says "held:
  a CelikPanel update or recovery is in progress; the unit starts when it
  finishes". A wrong-type path still logs "unsafe directory". Same exit code.

Not done: the offer itself is unchanged (the floor stays at the candidate's
sequence by design); no live status at the panel address while it is stopped.

### Hosting root traversal (upd2 finding P3, 2026-10-01)

Source state with component tests; the native re-run is pending. On a fresh Arch
`web` server a new static site answered 404 and its cron job never ran. A
disposable-guest reproduction proved the cause: `/var/www` does not exist on
Arch (`pacman -Qo /var/www`: no package owns it); the Agent's first site
creation made it through `MkdirAll` under the unit's `UMask=0027` and
`Group=celikpanel`, as `0750 root:celikpanel` (birth time equal to the site
home). Neither nginx (`http`) nor the site user could pass it.

- **Product-created parents.** Before any site change the Agent creates each
  missing directory above and including `/var/www/celikpanel` with exactly
  `0755 root:root`, set explicitly after `mkdir`, and records it in the
  root-only receipt `/var/lib/celikpanel-agent-private/hosting-root-v1.json`
  (schema `celikpanel/hosting-root-directories/v1`).
- **Owner directories are never changed.** An existing directory above the
  hosting base that the web server account or the site users cannot search
  refuses the site before any change: Agent code
  `hosting_root_not_traversable`, Panel `409 HOSTING_ROOT_NOT_TRAVERSABLE` with
  `vars` (directory, mode, owner, command).
  - Who acts: the server owner.
  - Next action: allow traversal, for example `sudo chmod 755 /var/www` (the
    blocking directory is named).
  - Resume: create the site again; nothing retries by itself.
- **Setup review.** Profiles that host sites run the same read-only proof at
  review. A blocking directory is the blocker
  `server_setup_hosting_root_not_traversable:<mode>:<owner>:<group>:<directory>`
  with the same owner action; the owner then reviews the plan again. A missing
  directory is not a blocker. An inspection error is logged and not shown as a
  verified block.
- **Existing servers.** Earlier builds recorded nothing, so a `/var/www 0750`
  they created cannot be told apart from an owner's choice and is refused with
  the command. A receipt never authorizes a repair: a product-created `0755`
  directory that later blocks was changed afterwards.
- **Node runtimes.** The same umask made `…/runtimes/node` `0750` and each
  version directory `0700` (from its staging directory), which site
  applications running as their site user cannot reach. Both are now set to
  `0755` explicitly.

Not done: POSIX ACLs are not read (a directory relying on an ACL is reported as
blocking); a symbolic link above the hosting base is followed, but its target's
parents are not proved. The native Arch re-run must still show the site
answering 200 with its marker, the cron stamp advancing and `namei -l` of the
document root.

### Update preflight stop, automatic retries and paused renewal (upd3 F1-F3, O6, 2026-10-01)

Source state with component and contract tests; the native run is pending. From
the upd3 run (findings F1, F2, F3 and observation O6).

- **The update stopped before changing the installed version (F1).** The
  selected recovery runtime's read-only preflight (`verify-compatibility`,
  recovery material and database support, database metadata) now reports its
  failed step and the checker's first diagnostic line, typed
  `recovery_runtime_preflight_failed` with `state=unchanged`, and records the
  request's failure sidecar with that code. The request is final.
  - Root CLI, recovery screen and the update notice: the update stopped in its
    read-only check before the installed version was changed or any service was
    stopped; the server runs the version it had, as before; the reason (the
    translated step on the notice, the checker's line as the secondary server
    line; the CLI names the worker journal
    `sudo journalctl -u celikpanel-self-update-<request>.service --no-pager -n 20`).
  - Who acts: nobody, unless the reason names a condition on this server (another
    operation still running, the package manager busy); then let it finish or fix
    it first. A package-manager refusal keeps the existing
    `package_manager_busy` text.
  - Resume: nothing resumes by itself; starting the update again is safe. The
    notice stops polling for this request.
  - "This version already failed" for such an attempt says instead that it
    stopped before changing anything installed and that starting again is safe.
- **Between automatic attempts (F3).** When an automatic recovery attempt fails
  and the timer admits another one, the failure record carries the optional hint
  `automatic_recovery=retry_scheduled`. CLI, recovery screen and notice say the
  server tries the same operation again by itself (normally about 30 seconds
  after the previous attempt ended, up to three attempts), that nothing is
  needed now and that the owner acts only if recovery pauses. The update's first
  typed cause (`panel_start_unverified`: the panel log command) stays visible
  during the scheduled retry and the next attempt. "The server owner must act"
  appears only at the pause or after the last attempt failed.
- **Certificate renewal at the pause (F2).** The pause text adds that automatic
  certificate renewal (Certbot) was stopped for the update and that the recovery
  journal says whether it was returned to its earlier state or stays stopped
  until the operation finishes (see the resilience contract entry of the same
  date for when each applies).
- **Server line after a verified rollback (O6).** The secondary "The server
  reported" line drops the updater's marker and its `code=`/`state=` tokens and
  the `reason=`/`detail=` labels; it is omitted when nothing readable remains.
  The Turkish notice's primary text is Turkish only.

Not done: the notice for a preflight stop appears only when the installed Panel
already contains this change (the card comes from the installed release; the
root CLI comes from the selected recovery kit, which the preflight has already
promoted to the candidate's). Other updater failures that occur before the EXIT
trap still keep only their die line.

- **Start limit on the continuation (same date).** If systemd refuses to start the Panel or Agent on its start limit, the recovery journal line names the unit and `sudo systemctl reset-failed <unit>`, then the retry (code `unit_start_limit_hit`). Every controlled start now clears only that unit's start limit first, so this should be rare. Who acts: the server owner. Resume: the same retry.

### Refused update check, the last attempt finishing, renewal already off (upd4 F4-F6, O7-O9, 2026-10-01)

Source state with component and contract tests; the native run is pending. From
the upd4 run (findings F4, F5, F6 and observations O7, O8, O9).

- **A read-only check refused the update before anything changed (F4).** The
  updater's own checks before the coordinators are frozen (panel operation queue,
  Agent operations, BIND and mail/DNS compatibility, the bootstrap state) now end
  typed `update_preflight_refused` with `state=unchanged`, the step, a reason class
  and the checker's line, and record the request's failure record. Any published
  quiesce is aborted first; if that abort fails, the outcome stays `update_failed`
  with recovery required.
  - Concurrent panel write: the live panel database or its `-wal`/`-shm` changed
    while the check read it (a panel commit or checkpoint, not a busy queue). The
    check is read once more after 2 s; only if it changes again does the update
    stop. A busy operation queue or any other refusal is never read again.
  - Root CLI, recovery screen and update notice: the update stopped before
    changing the installed version or stopping any service, and the server runs
    the version it had. The notice names the reason (concurrent write: "the panel
    was saving data while the check read its database ... Nothing is wrong on the
    server"; operation still queued or running: "let it finish before you start
    the update again"; other steps name the check); the CLI names the update log
    line that carries it.
  - Who acts: nobody, unless the reason names another operation; then let it finish.
  - Resume: nothing resumes by itself; starting the update again is safe. The
    notice stops polling.
  - The Panel no longer drops such a summary because it is long or contains a
    path: it shows a bounded form built from closed tokens (code, state, step,
    reason class). The full line stays in the Agent journal ("System update worker
    failed: …").
- **A failed panel database snapshot keeps its cause (F5).** The snapshot tool's
  first diagnostic line (without the log timestamp, printable, at most 240 bytes)
  is now in the failure line (`detail=`), therefore in the Agent journal and the
  update status record. Automatic rollback is unchanged. There is no re-read here.
- **The last attempt is finishing (F6).** When the third automatic attempt or an
  owner retry fails, the record carries `automatic_recovery=pause_pending` until
  the next timer run records the pause. Every reader keeps the update's first
  typed cause and says that recovery is finishing its last attempt and that the
  pause, with the next step and the one-time retry command, is recorded within
  about a minute; nothing is needed before then. "The server owner must act"
  appears only at the recorded pause.
- **Server line while recovery runs (O7).** The notice never shows the raw updater
  line: internal tokens are removed in every state, and the line is omitted when
  a reviewed translated summary says the same (package manager busy, a translated
  reason class). In Turkish it is labelled "Sunucunun İngilizce günlük satırı".
- **Renewal at the pause (O8).** The updater records whether the Certbot scheduler
  was on (a timer enabled or active) or off before the update. When it was off,
  the pause says renewal was already off and the update did not stop it (CLI also:
  check whether it should be on); otherwise, or when not recorded, the existing
  sentence stays.
- **Verified after an earlier failure (O9).** After a successful retry the recovery
  screen and notice show no failure title or "Recovery failed" label; the SSH owner
  view says the earlier attempts did not finish and a later attempt completed it.

Not done: the refused-check step and reason-class texts are in the server screen
catalogue (the boot catalogue has no room); until it has arrived the notice shows
the generic reason. A notice for this stop needs an installed Panel that contains
this change. The trigger of the upd4 snapshot failure is still unknown; the next
native run records it.

### Candidate review: unknown update status, aborted quiesce, early owner retry (2026-10-01)

Source state with component and contract tests; the native run is pending. See the
resilience contract entry "Candidate review corrections" of the same date.

- **Unknown update status.** When an update has no recorded status (it was started
  by an installed version that records none and the worker identity was not
  available, or it ran outside its worker), the root CLI still says the result is
  unknown and to check again, and now adds where to look if it stays unknown: the
  recovery journal `sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50`
  shows whether automatic recovery runs or paused and, at a pause, the one-time
  retry command. The updater's and the runner's journal lines say the same. An
  update started by v0.1.0-alpha.80 normally records its status itself now.
- **Aborted interrupted update (quiesce).** When recovery finds an update that was
  interrupted while the panel was frozen, it returns the panel and Agent to their
  state from before the update and the update ends: the status becomes "the update
  failed" and the journal says it stopped before the installed release or its data
  changed. Who acts: nobody. Resume: start the update again from the panel; nothing
  retries by itself. No "the timer tries again" or pending-pause text follows.
- **Owner retry with automatic attempts left.** If the owner retries before the
  three automatic attempts are used and it fails, the journal says how many
  automatic attempts remain and that the timer starts the next one; no owner action
  is needed yet and renewal is not changed. The pause and its retry command come
  only after the automatic attempts are used.
- **Panel environment the start check cannot read.** The update failure line keeps
  the generic code and says the check could not read the panel environment (a
  quote, backslash, unknown key or unsupported character in the unit's
  environment, its drop-ins or `panel.env`) and that the new panel was not checked;
  the update is still returned to the previous version. Who acts: the server owner,
  if the entry is theirs; then start the update again.
- **Missing curl.** A host without `/usr/bin/curl` stops the update before any
  change with "required update tool is missing: /usr/bin/curl; install it
  explicitly before retrying".
- **Recovery runtime preflight stop.** Its journal line now says the installed
  release and its data were not changed (EN and TR).

Not done: the web update screens keep their texts ("stopped before changing
anything installed", "stopped before changing installed files"); a successful
update started by alpha.80 shows "being applied" until the new Agent first checks
that request. Corrected on 2026-10-01 (`706c1c91`): the two web texts now say the
installed version and its data were not changed.

### Package manager busy during setup and before an update (upd8 F1/F2, 2026-10-01)

Source state with component and contract tests; the native Ubuntu run is pending.
See the resilience contract entry of the same date.

- **An idle PackageKit daemon no longer blocks.** On Ubuntu, apt starts
  PackageKit after every package operation and it stays idle for about five
  minutes. Setup steps, the update start, the services page readiness and the
  update/rollback checks no longer report "package manager busy" for that idle
  daemon. They still do while a package task really runs: apt, dpkg and the
  other listed tools, or PackageKit with a child process or an apt/dpkg lock.
- **What the owner sees when a package task really runs.** A setup step, mail
  profile or firewall step refused for it now shows `HOST_MUTATION_BUSY` with
  the existing sentence "This server's package manager is busy — something
  outside CelikPanel is installing or updating packages. Try again in a minute."
  instead of `mail_profile_install_failed` / `server_setup_firewall_failed`
  with the cause only in the panel log.
  - Who acts: nobody but the package task; the server owner only if it is
    theirs and does not finish.
  - Resume: setup does not resume by itself; after the task finishes, review
    and start a new setup plan (completed steps are kept). The update keeps its
    `package_manager_busy` text: start it again.

Not done: the setup wizard has no headline for `HOST_MUTATION_BUSY`; it shows its
generic "A required check needs attention" text and the sentence above under
"Details" (needs a web mapping). Corrected on 2026-10-01 (`fb04289b`). The
blocking task is not named. A refused setup step does not wait and retry by
itself.

Corrected on 2026-10-01 (upd9 F1-F3; component tests, native Ubuntu re-run
pending). On Ubuntu 24.04 the idle daemon above still blocked: the rule looked
for the wrong PackageKit backend file name; it now recognises Ubuntu's.

- **Setup step refused for real package activity.** The Panel's sentence (shown
  under "Details") is now "This server's package manager is busy — a package
  task is still running on this server. Wait for it to finish, then try again."
  The task can be CelikPanel's own previous step, and its length is not known,
  so the sentence no longer says "outside CelikPanel" or "a minute". The
  wizard headline is unchanged.
- **Update start refused for real package activity.** Instead of the generic,
  untranslated "the update service did not accept this request"
  (`PANEL_UPDATE_START_REFUSED`), the owner gets `HOST_MUTATION_BUSY` with
  reason `package_manager_active` and the package-manager sentence in their
  language. Who acts: nobody but the package task. Resume: wait until the update
  card shows the server ready, then start the update again; nothing was
  installed or recorded. An older Agent without the reason keeps the generic
  text.
- Not done: the browser's catalogue sentence for `HOST_MUTATION_BUSY` /
  `package_manager_active` (EN and TR) still says "outside CelikPanel" and "a
  minute"; it needs the same rewording in `web/`.

### Mail startup work while an update holds the server (upd11 F2, 2026-10-02)

Source state with component tests; the native run is pending. See the resilience
contract entry of the same date. Journal only (English, like every Panel journal
line); no screen changes.

- **Current reason.** A Panel started inside an update or rollback cannot publish
  the mail certificate (SNI) set or compose the Postfix mail filters, because the
  update still holds the server. The startup lines `certificate startup
  reconcile: certificate dependents: … still running; …` and `milter wiring at
  startup: … still running` now end with "the Panel retries this by itself every
  30 seconds for up to 10 minutes once no other server change is running;
  nothing needs to be done now". Mail keeps running with its current
  configuration meanwhile.
- **Who acts.** Nobody while the update finishes.
- **How it resumes.** Automatically: one line `startup mail work, attempt N of
  20: mail certificate publication completed (…)` / `mail filter wiring
  completed (…)`. A verified failure is named once in that line ("failed: …; it
  is not retried now and runs again at the next Panel start (sudo systemctl
  restart celikpanel-panel)") and is not repeated.
- **If it gives up.** After 10 minutes without a free server the line says the
  work is still not done, that these attempts changed nothing and that mail keeps
  running; the server administrator runs `sudo systemctl restart
  celikpanel-panel` once the other task has finished, and a domain whose SSL
  page says "mail TLS synchronization did not finish" can use "Retry
  activation" there.

Not done: no screen shows the deferral; a certificate renewal at Panel start
whose own mail step is refused still waits for the domain's "Retry activation"
or the next Panel start.

### Planned certificate handover during setup: guidance instead of unknown-result screens (2026-10-08)

Source state with unit, contract and mounted-component tests; no native run.
The screens were inspected in a real browser against a loopback mock on
2026-10-08 and corrected (see "Browser inspection" at the end of the next
entry). Found by an owner on an installed Ubuntu server running
v0.1.0-alpha.81, signed in at `https://<server-IP>:2083`. Affected contract
items: resilience invariants 2 and 6 (P0.2 scope); no acceptance item is closed.

**What the owner saw.**

1. First attempt: "Prepare component: Nginx" stopped with the generic headline
   "Setup stopped because another server change or package task is still
   running…". What was busy was not named. A second attempt minutes later ran.
2. During the second run an overlay titled "certbot kuruluyor" (Installing
   certbot) said "Bağlantı kesildi. Panel kilidi açılmadan yeniden
   bağlanılıyor…" and "Sunucunun son durumu doğrulanamadı…", then a full page
   "Panelin hazır olma durumu kontrol edilemedi" above an unrelated
   "Güncelleme ve kurtarma durumu: Güncelleme doğrulandı" block. The page then
   recovered by itself and showed "Sunucunuz hazır". Nothing was wrong on the
   server.

**What happens on the server (read from the code, not observed on that server).**
The step "Secure panel access" obtains the certificate; the Agent then restarts
the Panel once (`systemctl restart celikpanel-panel`) so it serves it, in a gap
between the step's own Agent jobs or right after the step. The Panel does not
swap certificates while running. After the restart a connection by host name
gets the new certificate; a connection by IP address keeps the original
self-signed certificate, so a page opened on the IP address reconnects by
itself. The overlay was not a lost connection at first: the browser had adopted
the finished certbot step and its follow-up catalogue scan was refused with
`server_setup_busy` for as long as setup ran, which the overlay reported as
"connection interrupted". The full page came from the restart
(`PANEL_STARTING`); its update block reads a saved update ID from the browser
and is shown to every administrator.

**What the product says now.** Keys are in `web/src/i18n`; `{host}` is the
reviewed panel host name.

- *Before and during the step, when the browser is not on that host name*
  (one block above the step list on the review page and on the progress page,
  quiet guidance surface, no alarm styling; on the review page it was after the
  step list, below the fold at 1440×900, until the browser inspection):
  - `setup.handover.title` — EN "The Panel restarts once during this setup" ·
    TR "Bu kurulum sırasında panel bir kez yeniden başlar"
  - `setup.handover.notice` — EN "At the step “Secure panel access: {host}”,
    the Panel gets its certificate and restarts once to start using it. This
    page then loses its connection for a short time. That is planned: setup
    continues on the server and this page reconnects by itself. You do not need
    to do anything." · TR "“Panel erişimini güvenceye al: {host}” adımında panel
    sertifikasını alır ve onu kullanmaya başlamak için bir kez yeniden başlar.
    Bu sayfanın bağlantısı o sırada kısa süreliğine kesilir. Bu planlı bir
    durumdur: kurulum sunucuda devam eder ve bu sayfa kendiliğinden yeniden
    bağlanır. Sizin bir şey yapmanız gerekmez."
  - `setup.handover.address` — EN "Once that step has finished, the Panel can
    also be opened at its secure address:" · TR "O adım bittikten sonra panel
    güvenli adresinden de açılabilir:", followed in the same sentence by
    `https://{host}:<port>` as a link to the wizard there. It is information,
    not a step: the notice has just said that nobody needs to act (corrected
    2026-10-08; it read "After that step, open the Panel at its secure
    address:" / "O adımdan sonra paneli güvenli adresinden açın:").
  - `setup.handover.addressHelp` — EN "You sign in again there, and setup shows
    the same progress. If the browser warns about the certificate at that
    address, the step has not finished yet, and this page keeps following setup
    by itself." · TR "Orada yeniden giriş yapılır ve kurulum aynı ilerlemeyi
    gösterir. Tarayıcı o adreste sertifika uyarısı verirse adım henüz
    bitmemiştir; bu sayfa kurulumu kendiliğinden izlemeyi sürdürür." (corrected
    2026-10-08; it ended "…; return to this page and wait." / "…; bu sayfaya
    dönüp bekleyin.")
  - `setup.handover.stepNote`, on the row of that step in the review list and
    in the progress list while the notice is shown — EN "The Panel restarts
    once here." · TR "Panel burada bir kez yeniden başlar."
- *When the connection drops and the last state read puts the certificate step
  at the front* (running; next to run with every earlier step finished; or
  finished with no later step finished). Replaces "Reconnecting to setup / The
  result has not been confirmed yet…":
  - `setup.handover.dropTitle` — EN "The Panel restarts at this step" · TR
    "Panel bu adımda yeniden başlar"
  - `setup.handover.drop` — EN "This page lost its connection while setup was
    securing panel access for {host}. That is expected: the Panel restarts once
    at this step to start using its new certificate. Setup continues on the
    server and this page reconnects by itself. Do not start setup again." · TR
    "Kurulum {host} için panel erişimini güvenceye alırken bu sayfanın
    bağlantısı kesildi. Bu beklenen bir durumdur: panel, yeni sertifikasını
    kullanmaya başlamak için bu adımda bir kez yeniden başlar. Kurulum sunucuda
    devam eder ve bu sayfa kendiliğinden yeniden bağlanır. Kurulumu yeniden
    başlatmayın."
  - `setup.handover.dropAddress` (other address only) — EN "If this page has not
    reconnected after a few minutes, continue at the Panel’s secure address:" ·
    TR "Bu sayfa birkaç dakika içinde yeniden bağlanmazsa panelin güvenli
    adresinden devam edin:", with the same link and help line. "Reconnect to
    this operation" stays as the secondary action; it only reads.
  - Who acts: nobody. Resume: automatic. A drop at any other step keeps the
    unknown-result text.
- *The full page while the Panel starts*, only when this browser started a setup
  whose plan had the step and the Panel itself reports a managed certificate for
  that host (`GET /api/v1/panel/access-address`):
  - `recovery.handoverTitle` — EN "The Panel restarts once during setup" · TR
    "Panel kurulum sırasında bir kez yeniden başlar"
  - `recovery.handoverHelp` — EN "Setup secured panel access for {host}, and the
    Panel restarts once to start using its new certificate. Setup continues on
    the server. This page checks by itself and opens when the Panel is ready;
    you do not need to do anything." · TR "Kurulum {host} için panel erişimini
    güvenceye aldı; panel yeni sertifikasını kullanmaya başlamak için bir kez
    yeniden başlar. Kurulum sunucuda devam eder. Bu sayfa kendiliğinden kontrol
    eder ve panel hazır olduğunda açılır; sizin bir şey yapmanız gerekmez."
  - `recovery.handoverAddress` (other address only) — EN "You can also continue
    at the Panel’s secure address:" · TR "Dilerseniz panelin güvenli adresinden
    de devam edebilirsiniz:", with the link.
  - The update and recovery status stays on the page, closed under its own
    title, instead of being shown as if it were the reason. Without that server
    report the page keeps its existing wording.
- *The overlay.* A setup step that has finished no longer holds the overlay for
  the rest of the setup with "Installing certbot / Connection interrupted". When
  its follow-up scan is refused with `server_setup_busy`, the page reads the scan
  the operation itself stored as its last phase (`GET /api/v1/managed-services`,
  no new probe of the server). The overlay is released only when that stored
  scan is no older than the second the operation started and shows the component
  installed; the catalogue it carries is published to the open pages and the
  usual "installed" message is shown. The stored operation is cleared in one
  place only, after a confirming snapshot (corrected 2026-10-08: the first
  version released the overlay on the refusal alone, without any snapshot). If
  the stored scan cannot be read or does not confirm the step, the overlay stays
  with its existing wording and is retried until a scan is accepted. Every other
  refused or failed scan reply is handled as before. No new overlay text.
- *A setup step refused because the server is busy.* The failed step now carries
  the typed reason (`error.reason`), and the text is selected from it; the
  Panel's sentence stays the fallback for older records. The Agent now names the
  case "another Agent job owns the lease" (`agent_mutation_active`) — the Panel's
  own short work after a start, a certificate activation or a renewal run as
  such jobs — so the owner reads the `setup.blocker.changeBusy` text instead of
  the generic one. Since the browser inspection of 2026-10-08 the reason is the
  one heading of the progress view and its body stands directly under it, on the
  neutral guidance surface (a held server: the caution tone), with the action
  "Review a revised plan" inside that block and "Technical details" closed under
  it. The generic heading, the two generic paragraphs and the failure colour are
  not shown for this case. Heading · body, EN then TR:
  - `setup.blocker.packageBusyTitle` "Setup stopped: another package task is
    running on this server" · `setup.blocker.packageBusy` "The other task may be
    an automatic update. Nothing is wrong. Wait for it to finish, then choose
    Review a revised plan and start setup again. Steps that already finished are
    kept." — "Kurulum durdu: bu sunucuda başka bir paket işlemi sürüyor" ·
    "Diğer işlem otomatik bir güncelleme olabilir. Bu bir arıza değil. Bitmesini
    bekleyin, ardından Düzeltilmiş planı incele’yi seçip kurulumu yeniden
    başlatın. Tamamlanan adımlar korunur."
  - `setup.blocker.changeBusyTitle` "Setup stopped: another CelikPanel change is
    still running on this server" · `setup.blocker.changeBusy` "Wait for that
    change to finish, then choose Review a revised plan and start setup again.
    Steps that already finished are kept." — "Kurulum durdu: bu sunucuda başka
    bir CelikPanel değişikliği sürüyor" · "O değişikliğin tamamlanmasını
    bekleyin, ardından Düzeltilmiş planı incele’yi seçip kurulumu yeniden
    başlatın. Tamamlanan adımlar korunur."
  - `setup.blocker.hostHeldTitle` "Setup stopped: an unfinished change is still
    holding this server" · `setup.blocker.hostHeld` "Waiting will not clear it.
    The server administrator restarts the server, then chooses Review a revised
    plan and starts setup again; steps that already finished are kept. If the
    hold returns after the restart, it needs investigating." — "Kurulum durdu:
    tamamlanmamış bir değişiklik bu sunucuyu hâlâ tutuyor" · "Bu durum
    beklemekle geçmez. Sunucu yöneticisi sunucuyu yeniden başlatır, ardından
    Düzeltilmiş planı incele’yi seçip kurulumu yeniden başlatır; tamamlanan
    adımlar korunur. Yeniden başlatmadan sonra yine olursa incelenmesi gerekir."
  - No typed reason: `setup.blocker.hostBusyTitle` "Setup stopped: this server
    is busy with another change" · `setup.blocker.hostBusy` "Another server
    change or package task is still running. Wait for it to finish, then choose
    Review a revised plan and start setup again. Steps that already finished are
    kept." — "Kurulum durdu: bu sunucu başka bir değişiklikle meşgul" · "Başka
    bir sunucu değişikliği veya paket işlemi hâlâ sürüyor. Tamamlanmasını
    bekleyin, ardından Düzeltilmiş planı incele’yi seçip kurulumu yeniden
    başlatın. Tamamlanan adımlar korunur."
- *One heading per state on the progress view* (2026-10-08). The section heading
  and the heading of the guidance block said nearly the same thing twice
  ("Setup needs attention" over "Action needed before continuing"). A stopped,
  waiting or confirming run is now named once, by the guidance title
  (`setup.guide.failedTitle`, `setup.guide.waitTitle`, `setup.guide.confirmTitle`,
  `setup.licenseWaiting`, `setup.guide.buildChangedTitle`); the block under it
  opens with the step it concerns. A run in progress keeps "Preparing your
  server" over "What happens at this step". For a stopped run the action
  "Review a revised plan" follows the reported error, above the step list
  instead of below it. The failure colour is used only for a stopped run; an
  unmet prerequisite and a result still being confirmed are drawn in the
  ordinary text colours.

**Stored shape.** `error.reason` is an optional field in the setup execution
record and API; older records and an older Panel reading a newer record are
unaffected. A child operation row still stores only code and sentence; its reason
is read back from the sentence on the Panel. The browser's setup start marker
gains an optional `handover` flag. No migration.

**Not handled.**

- What was busy on 2026-10-08 is not established; it needs that server's Agent
  journal. A lock held by something the Agent cannot identify, or an update's
  release gate, still gives the generic headline.
- A refused step still stops setup; it does not wait and resume by itself.
- The blocking job is named by kind only ("another CelikPanel change"), not by
  what it is doing.
- The overlay can still cover the wizard while a setup step is installing, if
  the tab regains focus during that step.
- If the original self-signed certificate is missing, expired or has no IP
  address, a page on the IP address cannot reconnect after the restart; the
  notice then relies on its secure-address link. Not exercised.
- On the full page, a second Panel restart in the same setup before a later step
  finishes is also explained as this restart.
- Not verified on a real server: the timing of the restart against the step
  list and the full page's ten-second recheck. In a browser the texts were seen
  in place against a mock only (see "Browser inspection" below).

### Access and readiness checks keep the page: explained hold, checking state, ended session (2026-10-08)

Source state with component tests; no native run. The screens were inspected in
a real browser against a loopback mock on 2026-10-08 and corrected (see "Browser
inspection" below). Found by an owner on an installed server running
v0.1.0-alpha.81. Affected contract items:
resilience invariants 2, 3 and 6 (P0.2 scope); no acceptance item is closed. The
mechanism and the full change are in the
[resilience contract](RESILIENCE-CONTRACT.md#a-mounted-page-is-replaced-only-by-a-known-negative-access-result-p02-2026-10-08).

**What the owner saw.** After leaving a page for a while: a full screen "Lisans
durumu kontrol edilemedi" with "Panel erişimini kontrol et", above "Güncelleme ve
kurtarma durumu: Güncelleme doğrulandı" for an update finished days earlier. On
return the panel opened again, without the open dialogue, the typed input and the
selected tab. The license was valid the whole time.

**What the product says now.** Keys are in `web/src/i18n/screens`; they are shown
only over or after a mounted page.

- *A decision that ran out while the tab was hidden, or a read that answers within
  1.5 s:* nothing is shown. The page cannot be used until the read has answered.
- *The read has not answered after 1.5 s* (layer over the page, nothing has
  failed): title `recovery.checkingTitle`, EN "Checking panel access" · TR "Panel
  erişimi kontrol ediliyor"; `accessHold.waitingHelp`, EN "The Panel has not
  answered yet. This page continues where it was as soon as it does." · TR "Panel
  henüz yanıt vermedi. Yanıt verir vermez bu sayfa kaldığı yerden devam eder."
- *A read answered without confirming access* (reason first):
  - License result unreadable: `accessHold.licenseTitle`, EN "Panel access could
    not be confirmed just now" · TR "Panel erişimi az önce doğrulanamadı";
    `accessHold.licenseHelp`, EN "CelikPanel could not read the license result
    for this server a moment ago. This does not mean your license is missing or
    expired." · TR "CelikPanel az önce bu sunucunun lisans sonucunu okuyamadı. Bu,
    lisansınızın eksik veya süresinin dolmuş olduğu anlamına gelmez."
  - Readiness unreadable: `accessHold.availabilityTitle`, EN "The Panel did not
    answer just now" · TR "Panel az önce yanıt vermedi";
    `accessHold.availabilityHelp`, EN "CelikPanel could not confirm that the Panel
    is ready. It may be restarting." · TR "CelikPanel panelin hazır olduğunu
    doğrulayamadı. Panel yeniden başlıyor olabilir."
  - Session unreadable: `accessHold.authTitle`, EN "Your session could not be
    confirmed just now" · TR "Oturumunuz az önce doğrulanamadı";
    `accessHold.authHelp`, EN "CelikPanel could not read your session a moment
    ago. This does not mean you were signed out." · TR "CelikPanel az önce
    oturumunuzu okuyamadı. Bu, oturumunuzun kapatıldığı anlamına gelmez."
  - The Panel reports that it is starting: the existing `recovery.startingTitle`,
    EN "The panel is starting" · TR "Panel başlatılıyor", with
    `recovery.startingHelp`, now EN "The server is preparing panel access. This
    page checks readiness automatically." · TR "Sunucu panel erişimini hazırlıyor.
    Bu sayfa hazır olma durumunu otomatik kontrol eder." (the sentence "You can
    inspect the last recorded result of your update below." / "Güncellemenizin
    kaydedilmiş son sonucunu aşağıda inceleyebilirsiniz." is removed, because the
    block is no longer always there).
  - During the planned certificate restart of setup, the existing
    `recovery.handoverTitle`, `recovery.handoverHelp` and
    `recovery.handoverAddress` are used over the wizard.
- *Who acts and how work resumes* (below the reason, except during the planned
  restart): `accessHold.resume`, EN "You do not need to do anything yet.
  CelikPanel checks again by itself. When access is confirmed, this page
  continues where it was, with what you typed. Until then nothing on this page
  can be changed." · TR "Şimdilik bir şey yapmanız gerekmiyor. CelikPanel
  kendiliğinden yeniden kontrol eder. Erişim doğrulandığında bu sayfa,
  yazdıklarınızla birlikte kaldığı yerden devam eder. O zamana kadar bu sayfada
  değişiklik yapılamaz." (No interval is stated since 2026-10-08: the text said
  "every few seconds", while the license read repeats every 5 s and the session
  and readiness read every 10 s.) Action: the existing
  `recovery.retry`, EN "Check panel access" · TR "Panel erişimini kontrol et"
  (`recovery.checking`, "Checking…" / "Kontrol ediliyor…", while a read the
  owner asked for is in flight). It reads; it starts nothing.
- *Still unknown after 30 s:* `accessHold.prolonged`, EN "This has taken longer
  than half a minute. You can keep waiting, and the automatic check continues, or
  you can reload CelikPanel. Reloading discards anything you typed on this page
  and did not save." · TR "Bu durum yarım dakikadan uzun sürdü. Beklemeyi
  sürdürebilirsiniz, otomatik kontrol devam eder; dilerseniz CelikPanel’i yeniden
  yükleyebilirsiniz. Yeniden yüklemek, bu sayfada yazıp kaydetmediğiniz her şeyi
  siler." with the existing `app.reload`, EN "Reload CelikPanel" · TR
  "CelikPanel’i yeniden yükle", beside the check.
- *Update and recovery status* (`recovery.operationTitle`) appears in the layer
  and on the access and readiness pages only for a saved operation that is still
  running, failed, rolled back after a failure, or unreadable. A verified update
  and a browser without a saved operation show nothing there.
- *First load and after sign-in:* while the session and readiness reads are in
  flight the page shows `recovery.checkingTitle` with `recovery.checkingHelp`
  (EN "Confirming your session and panel readiness. This check does not start a
  server operation." · TR "Oturumunuz ve panelin hazır olma durumu doğrulanıyor.
  Bu kontrol sunucuda bir işlem başlatmaz."). `recovery.availabilityTitle`
  ("Panel readiness could not be checked" / "Panelin hazır olma durumu kontrol
  edilemedi") needs a read that failed. These pages read again by themselves
  (session and readiness every 10 s, the license result every 5 s) and now say
  so instead of only asking the owner to check again (2026-10-08):
  - `recovery.authHelp` — EN "CelikPanel cannot confirm your session right now.
    This page checks again by itself; you can also check now. Management stays
    closed until your session and panel access are verified." · TR "CelikPanel
    şu anda oturumunuzu doğrulayamıyor. Bu sayfa kendiliğinden yeniden kontrol
    eder; dilerseniz şimdi de kontrol edebilirsiniz. Oturumunuz ve panel erişimi
    doğrulanana kadar yönetim kapalı kalır."
  - `recovery.availabilityHelp` — EN "Your session was verified, but panel
    readiness is unknown. This page checks again by itself and opens once the
    server confirms readiness and access. You can also check now or reload the
    page." · TR "Oturumunuz doğrulandı ancak panelin hazır olup olmadığı
    bilinmiyor. Bu sayfa kendiliğinden yeniden kontrol eder ve sunucu hazır
    olduğunu ve erişimi doğruladığında açılır. Dilerseniz şimdi kontrol edebilir
    veya sayfayı yenileyebilirsiniz."
  - `recovery.licenseHelp` — EN "The license result is unavailable. This does
    not establish that your license is missing or expired. This page checks
    again by itself, and management opens once the server confirms access. You
    can also check now." · TR "Lisans sonucu alınamıyor. Bu, lisansınızın eksik
    veya süresi dolmuş olduğunu göstermez. Bu sayfa kendiliğinden yeniden
    kontrol eder; sunucu erişimi doğruladığında yönetim açılır. Dilerseniz şimdi
    de kontrol edebilirsiniz."
- *The session ended under a page in use* (confirmed 401), above the sign-in
  form: `accessHold.sessionEnded`, EN "Your session ended. Sign in to return to
  the page you were on. Anything you had typed there and not saved was not
  kept." · TR "Oturumunuz sona erdi. Bulunduğunuz sayfaya dönmek için giriş
  yapın. Orada yazıp kaydetmediğiniz bilgiler korunmadı." Who acts: the owner.
  Resume: the same address after sign-in. A sign-out shows no reason.
- *A part of the interface failed to load after an update*, for 7 s before the
  page reloads, as the shared dialogue above everything else (corrected
  2026-10-08: it was one line at the top for 4 s, which covered the header of an
  open dialogue on a phone and did not say that unsaved input is lost):
  `accessHold.updateReloadTitle`, EN "This page is about to reload" · TR "Bu
  sayfa birazdan yeniden yüklenecek"; `accessHold.updateReload`, EN "A part of
  CelikPanel could not be loaded, most likely because CelikPanel was updated
  while this tab was open. This page reloads in a moment to load the current
  version. Anything you typed on this page and did not save is lost." · TR
  "CelikPanel’in bir bölümü yüklenemedi; büyük olasılıkla bu sekme açıkken
  CelikPanel güncellendi. Güncel sürümü yüklemek için bu sayfa birazdan yeniden
  yüklenir. Bu sayfada yazıp kaydetmediğiniz her şey kaybolur." Action: the
  existing `app.reload`, to reload at once. The page under it can no longer be
  used. The cause is stated as likely, not as known.

**Known negative results are unchanged:** a license reported missing, expired or
invalid still shows the activation page (administrator) or the message to contact
the administrator (other roles).

**Not handled.**

- If the wording part has not arrived, the layer says only "Checking panel
  access" with the shell's help line and the check; the ended-session reason and
  the reload line are then not shown.
- The reload after a failed part cannot be declined: it is announced, and can
  only be brought forward.
- A page whose own request is refused under the layer handles that itself; not
  audited page by page.
- Unsent input is not kept across a real sign-in.
- Not verified on a real server. In one browser, against a mock: the texts in
  place, the 1.5 s and 30 s steps, focus return and a hidden tab's timers (see
  below).
- The layer is centred, so its own box still covers the middle of a dialogue
  that is open under it; what is around it is dim and readable.

**Browser inspection (2026-10-08).** Both changes of this date were looked at in
a real, installed Chrome against a mock of the Panel API on the loopback address
(`web/tools/browser-inspect`): no installed server, no license service, no real
certificate and no real restart. The first pass, on commit `8a65d4ca`, found
eight defects that no component test had shown; they are corrected in the same
change, and the pass was repeated on the corrected source.

- *Found and corrected.* The planned-restart notice said "You do not need to do
  anything" and then told the owner to open another address; on the review page
  it was below the fold. The secure address was broken inside a word and, on a
  phone, inside `https://`; it is now one piece that wraps before a dot or the
  port. A step refused because the server was busy showed its reason third,
  under two near-identical headings and two generic paragraphs, in the failure
  colour, with its action below the step list. The title of the hold layer drew
  a focus ring like a control. The "checking" layer had an empty band between
  two lines. The layer over an open dialogue added a second scrim, so the page
  it says is kept could not be read; while the layer or the reload dialogue is
  drawn it is now the only scrim, and it is drawn above the component-operation
  overlay and the update lock and keeps keyboard focus against both. The
  activation page showed "Update and recovery status — No update operation ID
  is saved in this browser…" beside a license decision; like the other gates it
  now draws that block only for an unfinished operation.
- *Also corrected, not reachable with the current server.* A coded refusal
  answered by the access route itself no longer asks the gate to read that route
  again, and while access stays unknown a refused request cannot start a read
  more often than the 5 s re-check. With a synthetic 503 on the access route the
  browser had read it 10,581 times in 28.7 s; the server answers that route with
  200 and a typed body.
- *Covered.* Desktop 1440×900 and phone 390×844, Turkish and English, light and
  dark: review and progress with the notice, the restart and the continuation;
  a lost connection at another step; a busy server for each typed reason and for
  none; a failed install, a failed firewall step, an unmet DNS prerequisite, a
  license wait, a run in progress and a result being confirmed; the hold over a
  dialogue with typed input (quiet return, failed read, pointer and keyboard,
  the reload offer after 30 s, a slow read, a dropped connection, a refused
  request), over the component-operation overlay and with the update lock; the
  reload dialogue; a long host name in all four places that show the address;
  the known-negative gate; first load with slow and failed reads; an ended
  session.
- *Not covered.* Any real server, certificate or restart; Safari, Firefox, a
  screen reader, a touch device; the imitation skins; roles other than the
  administrator; pages other than Domains under the layer. The update lock is
  released when a hold begins (the tracker pauses, as before), so the layer was
  seen with the lock only while the lock was leaving. In the dark theme the page
  under the single scrim is dim: headings and controls can be read, small muted
  text only with effort.

### Current settings that could not be read, or changed after the page loaded (2026-10-08)

Source state with component tests; no native run. See the resilience contract
entry of the same date. Three screens: the server mail policy (Postfix page), a
domain's automatic backups and a domain's scheduled tasks. Before this, a failed
read showed defaults or "No scheduled tasks", and one Save or one added task
replaced what the owner really had.

The rule on these screens: nothing is shown as a setting, an "off" state or an
empty list until the server's current state is known, and no save is built from
defaults. Every refusal below comes before any change.

- **Reading (waiting, nobody acts).** In place of the form or list:
  - EN: "Reading the current settings from the server…"
  - TR: "Geçerli ayarlar sunucudan okunuyor…"
- **Could not be read (unknown result).** No form, no list, no Save. The notice
  names the screen, says nothing was changed and offers **Retry** / **Tekrar
  dene**, which reads again and changes nothing.
  - Mail policy, EN: "The current mail policy could not be read from the server,
    so no settings are shown and nothing can be saved here. Nothing was changed.
    Try again; if it keeps failing, check on the server that Postfix is running
    (sudo systemctl status postfix)."
  - Mail policy, TR: "Geçerli posta politikası sunucudan okunamadı; bu yüzden
    ayarlar gösterilmiyor ve buradan kayıt yapılamıyor. Hiçbir şey değiştirilmedi.
    Tekrar deneyin; sorun sürerse sunucuda Postfix’in çalıştığını denetleyin
    (sudo systemctl status postfix)."
  - Automatic backups, EN: "The automatic backup settings for this domain could
    not be loaded, so they are not shown and cannot be changed here. Nothing was
    changed; an existing schedule stays as it is. Try again."
  - Automatic backups, TR: "Bu domain'in otomatik yedek ayarları yüklenemedi; bu
    yüzden gösterilmiyor ve buradan değiştirilemiyor. Hiçbir şey değiştirilmedi;
    var olan bir zamanlama olduğu gibi duruyor. Tekrar deneyin."
  - Scheduled tasks, EN: "The scheduled tasks of this domain could not be read
    from the server, so the list is not shown and tasks cannot be added or
    changed here. Nothing was changed; the tasks already on the server are
    untouched. Try again."
  - Scheduled tasks, TR: "Bu domain'in zamanlanmış görevleri sunucudan
    okunamadı; bu yüzden liste gösterilmiyor ve buradan görev eklenemiyor ya da
    değiştirilemiyor. Hiçbir şey değiştirilmedi; sunucudaki görevlere dokunulmadı.
    Tekrar deneyin."
  - Cron missing keeps its own verified answer (`CRON_NOT_INSTALLED`, entry of
    2026-10-01), not this notice.
- **Changed on the server after the page loaded (verified refusal,
  `409 SETTINGS_CHANGED`; a save without a version, `409
  SETTINGS_VERSION_REQUIRED`, is shown the same way).** The notice stays on
  screen above the form, what was typed stays visible, Save is disabled, and the
  one action reloads: **Reload current settings** / **Geçerli ayarları yeniden
  yükle** (scheduled tasks: **Reload the list** / **Listeyi yeniden yükle**).
  Who acts: the person at the screen. Nothing retries by itself.
  - Mail policy, EN: "The mail policy changed on the server after this page
    loaded, so nothing was saved. What you entered is still shown below. Reload
    the current settings, then make your change again."
  - Mail policy, TR: "Posta politikası bu sayfa yüklendikten sonra sunucuda
    değişti; bu yüzden hiçbir şey kaydedilmedi. Girdikleriniz aşağıda duruyor.
    Geçerli ayarları yeniden yükleyin, sonra değişikliğinizi tekrar yapın."
  - Automatic backups, EN: "The automatic backup settings changed on the server
    after this page loaded, so nothing was saved. What you chose is still shown
    below. Reload the current settings, then make your change again."
  - Automatic backups, TR: "Otomatik yedek ayarları bu sayfa yüklendikten sonra
    sunucuda değişti; bu yüzden hiçbir şey kaydedilmedi. Seçtikleriniz aşağıda
    duruyor. Geçerli ayarları yeniden yükleyin, sonra değişikliğinizi tekrar
    yapın."
  - Scheduled tasks, EN: "The scheduled tasks changed on the server after this
    list loaded, so nothing was changed. Reload the list, then try again; a task
    you were typing stays in the form."
  - Scheduled tasks, TR: "Zamanlanmış görevler bu liste yüklendikten sonra
    sunucuda değişti; bu yüzden hiçbir şey değiştirilmedi. Listeyi yeniden
    yükleyin, sonra tekrar deneyin; yazmakta olduğunuz görev formda kalır."
- **DNSBL the Panel will not rewrite (unmet prerequisite, shown on load).** The
  DNSBL controls are replaced by the reason and the owner's action; message size
  and the rate limit stay editable. Who acts: the server owner, in `main.cf`.
  - Refers to another setting, EN: "DNSBL cannot be changed from this page: the
    recipient restrictions in Postfix refer to another setting ($name), so
    CelikPanel cannot tell which checks they contain and will not rewrite them."
    TR: "DNSBL bu sayfadan değiştirilemiyor: Postfix’teki alıcı kısıtları başka
    bir ayara ($ad) başvuruyor; CelikPanel hangi denetimleri içerdiklerini
    bilemediği için onları yeniden yazmaz."
  - Cannot be read with certainty, EN: "DNSBL cannot be changed from this page:
    CelikPanel could not read the recipient restrictions in Postfix with
    certainty (an unclosed brace, or reject_rbl_client without a zone) and will
    not rewrite them." TR: "DNSBL bu sayfadan değiştirilemiyor: CelikPanel,
    Postfix’teki alıcı kısıtlarını kesin olarak okuyamadı (kapanmamış bir süslü
    ayraç ya da bölgesi olmayan bir reject_rbl_client) ve onları yeniden yazmaz."
  - Written by hand without both permits, EN: "DNSBL cannot be changed from this
    page: the recipient restrictions in Postfix were written by hand without
    both permit_mynetworks and permit_sasl_authenticated, so a DNSBL check
    placed by CelikPanel could reject this server’s own users." TR: "DNSBL bu
    sayfadan değiştirilemiyor: Postfix’teki alıcı kısıtları elle yazılmış ve
    permit_mynetworks ile permit_sasl_authenticated girdilerinin ikisini birden
    içermiyor; CelikPanel’in koyacağı bir DNSBL denetimi bu sunucunun kendi
    kullanıcılarını reddedebilir."
  - Ends with a final action, EN: "DNSBL cannot be changed from this page: the
    recipient restrictions in Postfix end with permit, reject or defer, so a
    DNSBL check added after them would never run, and where it belongs is your
    decision." TR: "DNSBL bu sayfadan değiştirilemiyor: Postfix’teki alıcı
    kısıtları permit, reject ya da defer ile bitiyor; arkasına eklenen bir DNSBL
    denetimi hiç çalışmaz ve nereye konacağı sizin kararınızdır."
  - A reason this screen has no words for, EN: "DNSBL cannot be changed from
    this page: CelikPanel will not rewrite the recipient restrictions it found
    in Postfix." TR: "DNSBL bu sayfadan değiştirilemiyor: CelikPanel, Postfix’te
    bulduğu alıcı kısıtlarını yeniden yazmaz."
  - Action, EN: "To change it, edit the reject_rbl_client entries of
    smtpd_recipient_restrictions in /etc/postfix/main.cf, run sudo systemctl
    reload postfix, then reload this page. Message size and the rate limit can
    still be saved here."
  - Action, TR: "Değiştirmek için /etc/postfix/main.cf içindeki
    smtpd_recipient_restrictions değerinin reject_rbl_client girdilerini
    düzenleyin, sudo systemctl reload postfix komutunu çalıştırın, sonra bu
    sayfayı yenileyin. İleti boyutu ve hız sınırı buradan kaydedilebilir."
- **Other refusals (verified, shown as the error message; the form stays).**
  - Duplicate task (`409 CRON_JOB_DUPLICATE`), EN: "A task with the same schedule
    and command already exists, so nothing was added. Change the existing task
    instead, or enable it if it is disabled." TR: "Aynı zamanlama ve komutla bir
    görev zaten var; bu yüzden hiçbir şey eklenmedi. Var olan görevi değiştirin
    ya da devre dışıysa etkinleştirin."
  - A zone that is not a plain host name (`400 MAIL_POLICY_INVALID`,
    `dnsbl_zone`), EN: "Nothing was saved: a DNSBL zone must be a plain host name
    such as zen.spamhaus.org, with zones separated by commas. Correct the zones
    and save again." TR: "Hiçbir şey kaydedilmedi: DNSBL bölgesi zen.spamhaus.org
    gibi düz bir alan adı olmalı ve bölgeler virgülle ayrılmalıdır. Bölgeleri
    düzeltip yeniden kaydedin."
  - The state could not be read at the moment of a save
    (`502 CURRENT_SETTINGS_UNREADABLE`), EN: "CelikPanel could not read what is
    currently set on the server, so nothing was changed. Reload the page and try
    again." TR: "CelikPanel sunucuda şu an neyin ayarlı olduğunu okuyamadı; bu
    yüzden hiçbir şey değiştirilmedi. Sayfayı yenileyip tekrar deneyin."

The API answers carry an English sentence for the same states for anything that
reads the API directly; the Agent's and `postconf`'s own lines stay in the logs.

Not done: a failed Postfix reload after a saved policy and a failed `postconf`
write are not classified (the second is the `INTERNAL` fallback); the unknown
notice does not say why the read failed; the DNSBL refusal from the API
(`MAIL_POLICY_RESTRICTIONS_UNMANAGED`) has an English sentence only, because the
screen withdraws the control instead of meeting it; other screens still follow
the old pattern and are not audited here.

### No negative state unless it is known: checking, could not check, known (2026-10-09)

Source state with component tests and a browser inspection against a loopback
mock (see the end of this entry); no native run and no installed server. Reported
by an owner on an installed server on 2026-10-08. It applies the rule of "Truth
and operation identity" above (an unknown result is not an unmet prerequisite) to
every screen that reads the server. It changes how reads are shown; no lifecycle,
access gate, stored record or API changes, and no acceptance item is closed.

**What the owner saw.** Opening "Add domain" on a server that has DNS showed "An
active, panel-managed DNS server is required before adding domains… [Choose a DNS
engine]" with the form disabled, for the seconds the read of the server's
capabilities took. The Domains page under it disabled "Add domain" for the same
seconds. Nothing was missing; the answer had not arrived yet.

**Cause.** There was no shared way to read the server. 68 files read it with a
raw `fetch` into component state, and most of them turned "the read failed" or
"the read has not answered" into a value (`null`, an empty list, `false`) that the
screen then showed as a fact. The same address, `GET /api/v1/hosting/capabilities`,
was read in eight places with eight meanings for "no answer yet".

**The rule, for every screen from now on: no negative UI unless known.** What a
screen reads from the server is always in one of three states, and they never
look alike:

1. **Checking** (not known yet; nobody acts). One quiet line in the colour of
   ordinary text, in a place that does not move the rest of the screen when the
   answer arrives. No "missing", "not ready", "none", "off", no empty list.
2. **Could not check** (unknown result; the person at the screen acts). The
   screen's own sentence says what could not be read, that this does not mean the
   thing is missing, and that nothing was changed; **Retry** / **Tekrar dene**
   reads again and changes nothing. If an earlier answer exists it stays on
   screen under a notice that says it is the earlier answer and when it was read.
3. **Known.** Then, and only then, "missing", "not ready", "empty" or "off", with
   the guidance that state already had.

A control that submits or removes something is enabled only while what it acts on
is known. No form is built or saved from defaults. A read is never repeated as a
change: Retry, "Check again" and a refresh only read.

**The texts.** Keys are in `web/src/i18n/screens` unless noted.

- *An earlier answer is still shown after a failed refresh* (shell,
  `common.staleNotice`; `{time}` is the hour and minute it was read):
  - EN: "This could not be read again from the server just now, so what is shown
    below is as it was at {time}. Nothing was changed. Controls that remove or
    change something are off until it has been read again."
  - TR: "Bu, az önce sunucudan yeniden okunamadı; aşağıda gösterilen, saat {time}
    itibarıyla olan hâlidir. Hiçbir şey değiştirilmedi. Bir şeyi kaldıran ya da
    değiştiren denetimler, yeniden okunana dek kapalıdır."
- *Add domain dialogue, and the DNS records tab of a domain* — checking
  (`dns.checkingServer`): EN "Checking this server’s DNS…" · TR "Bu sunucunun DNS
  durumu kontrol ediliyor…". In the dialogue the form is shown and can be filled
  in, "Create domain" is disabled, and no blocker is drawn.
- *Add domain dialogue* — could not check (`domains.add.dnsUnknown`), with Retry
  and without "Choose a DNS engine":
  - EN: "CelikPanel could not check DNS on this server, so a domain cannot be
    added yet. This does not mean DNS is missing. Nothing was changed and what
    you typed is kept. Try again."
  - TR: "CelikPanel bu sunucunun DNS durumunu kontrol edemedi; bu yüzden şimdilik
    alan adı eklenemiyor. Bu, DNS’in eksik olduğu anlamına gelmez. Hiçbir şey
    değiştirilmedi ve yazdıklarınız duruyor. Tekrar deneyin."
- *Add domain dialogue and Domains page* — known negative: unchanged. No active
  engine: `domains.add.needsDns` with `err.DNS_SERVER_REQUIRED.action`; an engine
  without its identity: `err.DNS_SETTINGS_REQUIRED` with its action. While DNS
  readiness is being checked or could not be checked, the Domains page says
  nothing about DNS and "Add domain" stays available; the dialogue it opens shows
  the checking line or the notice above.
- *Domains page, the list* — checking (`domains.checking`): EN "Reading the
  domain list…" · TR "Alan adı listesi okunuyor…". Could not read
  (`domains.unknown`):
  - EN: "The domain list could not be read from the server, so it is not shown.
    This does not mean there are no domains. Nothing was changed. Try again."
  - TR: "Alan adı listesi sunucudan okunamadı; bu yüzden gösterilmiyor. Bu, alan
    adı olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - "No domains yet" / "Henüz alan adı yok" (`domains.empty`) only for a list the
    server answered with no rows.
- *Domains page, a row listed as pending whose saved deletion could not be read*
  (`domains.pendingUnknown`; before this the row silently had no notice):
  - EN: "{name} is listed as pending, but CelikPanel could not read whether a
    deletion is waiting for it. Nothing was changed. Try again."
  - TR: "{name} beklemede görünüyor, ancak CelikPanel onun için bekleyen bir silme
    olup olmadığını okuyamadı. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Domains page, subscription usage* (`quota.unknown`): EN "Subscription usage
  could not be read from the server, so it is not shown. Nothing was changed. Try
  again." · TR "Abonelik kullanımı sunucudan okunamadı; bu yüzden gösterilmiyor.
  Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Databases page, engines* — checking (`databases.checkingServers`): EN "Reading
  the database engines on this server…" · TR "Bu sunucudaki veritabanı motorları
  okunuyor…". Could not read (`databases.serversUnknown`):
  - EN: "The database engines on this server could not be read, so nothing is
    listed. This does not mean no engine is installed. Nothing was changed. Try
    again."
  - TR: "Bu sunucudaki veritabanı motorları okunamadı; bu yüzden hiçbir şey
    listelenmiyor. Bu, kurulu motor olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - "No database engine installed" with "Go to Services" only for a known empty
    answer.
- *Databases page, the databases and the users of one engine* — checking
  (`databases.checkingDatabases`, `databases.checkingUsers`): EN "Reading the
  databases on this engine…", "Reading the database users on this engine…" · TR
  "Bu motordaki veritabanları okunuyor…", "Bu motordaki veritabanı kullanıcıları
  okunuyor…". Could not read (`databases.databasesUnknown`,
  `databases.usersUnknown`):
  - EN: "The databases on this engine could not be read, so the list is not
    shown. This does not mean there are none. Nothing was changed. Try again." /
    "The database users on this engine could not be read, so the list is not
    shown. This does not mean there are none. Nothing was changed. Try again."
  - TR: "Bu motordaki veritabanları okunamadı; bu yüzden liste gösterilmiyor. Bu,
    veritabanı olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
    deneyin." / "Bu motordaki veritabanı kullanıcıları okunamadı; bu yüzden liste
    gösterilmiyor. Bu, kullanıcı olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - The count beside each tab is the number once the list is known, "…" while it
    is read and "–" when it could not be read. "Create database" needs both lists
    known, because its dialogue offers the existing users.
- *A domain's Databases tab* — checking (`db.checking`): EN "Reading this
  domain’s databases…" · TR "Bu alan adının veritabanları okunuyor…". Could not
  read (`db.unknown`):
  - EN: "The databases of this domain could not be read from the server, so the
    list is not shown. This does not mean there are none. Nothing was changed.
    Try again."
  - TR: "Bu alan adının veritabanları sunucudan okunamadı; bu yüzden liste
    gösterilmiyor. Bu, veritabanı olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - Engines — checking (`db.checkingEngines`): EN "Checking which database
    engines are installed…" · TR "Kurulu veritabanı motorları kontrol ediliyor…".
    Could not check (`db.enginesUnknown`): EN "CelikPanel could not check which
    database engines are installed on this server, so a database cannot be
    created here yet. Nothing was changed. Try again." · TR "CelikPanel bu
    sunucuda hangi veritabanı motorlarının kurulu olduğunu kontrol edemedi; bu
    yüzden şimdilik burada veritabanı oluşturulamıyor. Hiçbir şey değiştirilmedi.
    Tekrar deneyin." There is no default engine: the create form exists only for
    an engine the server named.
- *A domain's connection card* — checking (`conn.checking`): EN "Checking where
  this domain points…" · TR "Bu alan adının nereyi gösterdiği kontrol ediliyor…".
  The card's own read failed (`conn.readFailed`; before this the card vanished):
  - EN: "The connection of this domain could not be checked: CelikPanel did not
    get an answer from this server. This does not mean the domain is not
    connected. Try again."
  - TR: "Bu alan adının bağlantısı kontrol edilemedi: CelikPanel bu sunucudan
    yanıt alamadı. Bu, alan adının bağlı olmadığı anlamına gelmez. Tekrar
    deneyin."
  - The read succeeded and the server says it could not ask the public resolvers
    (`status: unknown`): the status line is the existing `conn.status.unknown`
    ("Public DNS could not be checked" / "Genel DNS kontrol edilemedi") in the
    colour of ordinary text; the two "right now" boxes say `conn.notChecked`, EN
    "could not be checked" · TR "kontrol edilemedi", instead of "nothing yet";
    and in place of "This domain does not point at this server yet…":
    `conn.unknownHelp`, EN "This server could not reach the public DNS resolvers
    just now, so CelikPanel does not know where this domain points. This does not
    mean it is not connected. Check again in a moment. If the domain is not
    connected yet, the values to enter at the company you bought it from are
    below." · TR "Bu sunucu az önce genel DNS çözümleyicilerine ulaşamadı; bu
    yüzden CelikPanel bu alan adının nereyi gösterdiğini bilmiyor. Bu, alan
    adının bağlı olmadığı anlamına gelmez. Biraz sonra tekrar kontrol edin. Alan
    adı henüz bağlı değilse, alan adını aldığınız firmada girilecek değerler
    aşağıdadır."
  - If the nameserver names could not be verified in the same answer, they are
    not called broken and delegating to them is not offered: `conn.routeAUnknown`,
    EN "Whether this server’s nameserver names answer could not be checked
    either, so handing DNS to them is not offered right now. Check again." · TR
    "Bu sunucunun ad sunucusu adlarının yanıt verip vermediği de kontrol
    edilemedi; bu yüzden DNS’i onlara devretmek şu anda sunulmuyor. Tekrar
    kontrol edin."
  - The certificate line: `conn.sslUnknown`, EN "Whether a certificate can be
    issued is not known until public DNS can be checked." · TR "Sertifika alınıp
    alınamayacağı, genel DNS kontrol edilene dek bilinmiyor." Action in all three
    cases: the existing "Check again" / "Tekrar kontrol et", which reads.
- *A domain's PHP settings, installed versions* — checking
  (`php.checkingVersions`): EN "Checking which PHP versions are installed…" · TR
  "Kurulu PHP sürümleri kontrol ediliyor…". Could not check
  (`php.versionsUnknown`): EN "The installed PHP versions could not be checked,
  so only the current version is listed and the version cannot be changed here
  yet. Nothing was changed. Try again." · TR "Kurulu PHP sürümleri kontrol
  edilemedi; bu yüzden yalnız geçerli sürüm listeleniyor ve sürüm şimdilik
  buradan değiştirilemiyor. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *A domain's hosting type* — "PHP-FPM is not installed" only for a known answer;
  could not check (`hosting.phpUnknown`): EN "CelikPanel could not check whether
  PHP-FPM is installed on this server, so the PHP type is not marked either way.
  Nothing was changed. If PHP-FPM is not installed, applying the PHP type is
  refused. Try again." · TR "CelikPanel bu sunucuda PHP-FPM’in kurulu olup
  olmadığını kontrol edemedi; bu yüzden PHP türü kullanılabilir ya da
  kullanılamaz diye işaretlenmedi. Hiçbir şey değiştirilmedi. PHP-FPM kurulu
  değilse PHP türünü uygulamak reddedilir. Tekrar deneyin."
- *A domain's tabs*: Mail and Databases are removed only when the server is known
  not to have the service; while that is checked or could not be checked they
  stay, and the panel under each says its own state.
- The three settings screens of 2026-10-08 (mail policy, automatic backups,
  scheduled tasks) keep their texts; they now stand on the same layer.

**Two defects corrected in the same screens.**

- *Databases page.* After "Remove account" (or any change to CelikPanel's own
  account on an engine) the strip went on showing the row the page was first
  loaded with: the removed account still "present", and "New password" created it
  again. The strip now always shows the server's latest answer, and its controls
  are off while that answer is being read again or could not be.
- *Connection card.* An answer with `status: unknown` carries `null` lists;
  reading them took the whole page of the domain down. They are read as lists
  now, and `unknown` no longer reads as "does not point here yet" (above).

**How it is kept from coming back.**

- `web/src/lib/remote.ts` is the one way to read: `useRemote(url, decode)` gives
  `loading | known(value, observedAt) | unknown(reason, previous?)`, shares one
  request among everything on screen that reads the same address, and never
  produces a default. `web/src/lib/hostingCapabilities.ts` is the one reader of
  the capabilities. `Checking`, `CouldNotCheck`, `RemoteGate` and `KnownEmpty`
  are in `web/src/components/ui.tsx`.
- *The ratchet* (`web/tests/remote-state-ratchet.test.mjs`) counts, per file, the
  old patterns: a failed read turned into a value (`x.ok ? … : null`), a
  swallowed failure (an empty `catch {}` around a read, `.catch(() => {})`), an
  ignored failure (`if (res.ok) {…}` with no else, `if (!res.ok) return;`), a raw
  read `fetch(` outside `src/lib`, and `<EmptyState>` used without the answer
  that proves it. The numbers are in `web/tests/remote-state-ratchet.json`; they
  may only go down, a file not on the list must have none, and the totals are
  pinned in the test. Before this entry: 46 / 68 / 16 / 126 / 32 in 68 files.
  After it: 37 / 58 / 14 / 109 / 29 in 62 files.
- *The mounted test* (`web/tests/remote-state-mounted.test.mjs`) renders each
  migrated screen with each read withheld — still on its way, dropped, refused,
  not the contract — and requires that none of the negative texts is drawn and no
  control that submits or removes is enabled.

**How to migrate a screen.** Read with `useRemote(url, decode)`; the decoder
throws on an answer that is not the contract instead of filling in a default.
Draw through `<RemoteGate remote checking failed onRetry>` (its children receive
only a value the server sent) or an explicit switch on `remote.state`. Give the
checking line a place whose height does not change when the answer arrives. Draw
"empty" with `<KnownEmpty of={…}>` and compute a requirement with `gateOn`, which
can say "blocked" only for a known answer. Disable every save and delete unless
the state is `known`. After the screen's own change call `retry()`. Add the
checking and could-not-check sentences in both languages; the second says what
could not be read, that this does not mean it is missing, and that nothing was
changed. Then run `node tests/remote-state-ratchet.mjs --tighten` in `web/` and
add the screen to the table of the mounted test.

**Not done.**

- 62 files still read the old way; they are the allow-list. Until a screen is
  migrated it can still show a negative state for an unknown one. (53 after the
  second batch, below.)
- Other readers of the same addresses are untouched: the navigation rail and the
  page of one domain read the domain list on their own. (Both were moved onto
  the shared read in the second batch, below.)
- The Domains page shows nothing of its own when DNS readiness could not be
  checked; the notice and Retry are in the dialogue.
- A could-not-check notice does not say why the read failed.
- Not verified on a real server; one Chrome against a mock.

**Browser inspection (2026-10-09).** In a real, installed Chrome against the
loopback mock (`web/tools/browser-inspect`, scenarios `adddomain`, `domainslist`,
`databases`, `connection`): desktop 1440×900 and phone 390×844, Turkish and
English, light and dark. Covered: the Domains page and the Add domain dialogue
with the capabilities slow (four frames during the read), failing then Retry,
known negative (no engine, no identity, and the dialogue's own blocker) and known
positive, and the dialogue opened over a page that already has the answer; the
domain list slow, failing then Retry, and known empty; the Databases page with
engines and one engine's lists slow, failing, empty and populated, a list that
could not be read again after a delete, and the panel's account after removal; a
domain's connection card slow, failing, `status: unknown` with every list `null`,
known negative and known positive; a domain's database list slow, failing, empty
and populated.

- *Measured in all eight configurations.* In each of the four frames taken while
  the capabilities were on their way, the dialogue showed the form, the checking
  line and a disabled "Create domain"; the name could be typed and both purposes
  could be chosen; no frame showed the blocker. When the answer arrived nothing
  in the dialogue changed place or size. The page and the dialogue together made
  one request; the dialogue opened over a page that had the answer made none and
  started with "Create domain" enabled. What was typed was kept across Retry.
  After a list could not be read again, both delete controls were disabled.
- *Found by looking and corrected.* The connection card was a strip of one line
  while it was checking and then pushed the rest of the Overview down by 186 px
  (83 px on a phone); it now keeps one least height in every state, and nothing
  below it moved in any configuration. "could not be checked" stood in the face
  for literal values; it is words and is set in the text face. "Create Database"
  on a domain's Databases tab was white on the dark theme's light primary; it is
  the shared primary button now.
- *Seen and not changed here.* On a phone the Domains and Databases tables
  scroll sideways, so the delete control is off screen until the table is
  scrolled. A failed read of the domain list leaves the navigation rail's own
  request for it open (the rail is not migrated). A disabled primary button is
  close to an enabled one in the dark theme.
- *Not covered.* Any real server; Safari, Firefox, a screen reader, a touch
  device; the imitation skins; roles other than the administrator. The DNS
  records tab, the PHP settings and the hosting type were not in the browser
  run; their three states are covered by the mounted test only.

#### Second batch: Settings, accounts, a domain's files, certificate and page, import, monitoring, components (2026-10-09)

Source state with component tests and a browser inspection against a loopback
mock; no native run and no installed server. The same rule, applied to the next
screens of the audit. It changes how reads and lost answers are shown in the
interface; no API, stored record, access gate or lifecycle changes, and no
acceptance item is closed. Three things in it need the server and are listed
under "Needs the server" below.

**What these screens showed before.**

- *Settings, two-factor sign-in.* A status read that failed was drawn as "off",
  with the form that sets it up. (The server refuses a second setup, so this was
  misleading, not dangerous.)
- *Settings, the certificate the Panel serves.* A failed read removed the state
  line and left only "Get certificate". An answer in which the Panel had found
  no certificate it could read was drawn as "a trusted certificate ()". A poll
  that got no answer said "Getting certificate…" without end, and a request the
  server had no record of after ten minutes was called failed. After a
  certificate was issued, any tab that had Settings open, on any section, was
  moved to the new address six seconds later, with a toast as the only notice.
- *Accounts.* "No accounts yet" and an empty plan list for a failed read, with
  no notice. A change whose answer was lost left an unhandled error and nothing
  on screen.
- *A domain's files.* "This folder is empty" for a failed read; the rows of the
  folder just left stayed under the new path while the new folder was read.
- *A domain's certificate.* After a successful request, if the read that follows
  it failed, the screen went on showing "No certificate" and the request form,
  unmarked. A request whose connection dropped was called failed.
- *Import.* An apply whose connection dropped showed nothing and offered "Start
  import" again.
- *Dashboard.* The license notice said "An active license is required" when the
  license could only not be verified. A failed read was "0 domains, 0
  databases, 0 users, 0 mail accounts", and with nothing installed the whole
  hosting section left the page. The navigation badge fell to none on a failed
  read.
- *Monitoring.* A poll that failed replaced the charts with "No samples yet".
- *A domain or a component opened by its address.* A lookup that failed, or a
  name the server does not list, returned to the list without a word.
- *A component's page.* A record that could not be read was drawn as "This
  server has not been checked yet", with the scan offered. Install said
  "Installing…" while the page was only asking whether an operation was
  running. Start and stop sent the component's id until the page's second copy
  of the record had loaded (BIND's id is `bind`, its unit `named`). A tool with
  no daemon was "Stopped". Its panels said "No configuration file was found" and
  read the log of a unit nobody had named yet.
- *The install dialogue of the components list.* When the read that says whether
  a component needs an additional repository failed, the repository block was
  hidden and Install was enabled; the version read as "distribution default".

**What they do now, beyond the three states.**

- *The certificate request.* What became of a request is one of: still asked
  about; **could not be confirmed** (polls have had no answer for nine seconds;
  the exact request stays remembered, the page goes on asking every five
  seconds, "Check again" asks at once, a second request is not offered, and the
  secure address is given as a link); **failed** (only when the server reports
  the request as failed; the server's reason is shown and stays on the card);
  **not recorded** (the server answers that it has no such request, still, ten
  minutes after it was sent: nothing was started, and the request can be made
  again); **issued**.
- *The move to the secure address.* Only the page that sent the request moves
  itself, only while its Panel HTTPS section is the one open and the tab is
  visible, and only after it has said where it will reopen and why, for ten
  seconds, with **Stay here**. Opening another section counts as staying. A tab
  that only found the request in the browser's storage follows it and is never
  moved. In every case the card then gives the secure address as a link.
- *A change whose answer did not arrive* (accounts, plans, files, a domain's
  certificate and its settings, two-factor sign-in, start and stop, the
  repository of a component): the screen says that it is not known whether the
  change was made, sends nothing a second time, and reads the state again so the
  person can look before repeating it. A refusal by the server is shown as the
  server's own reason.
- *Import.* An apply that loses its connection (or gets a gateway's answer in
  place of the Panel's: 408, 429, 502, 503, 504) leaves a notice on the page.
  The choices that made the request are frozen and "Start import" is gone.
  "Check {domain}" reads the domain list and nothing else. If the domain is
  there, the import created it; the page links to it and does not start the
  import again. If it is not there, the page says so, offers the check again,
  and only then offers "Start import again".
- *A component's page.* Start, stop and restart exist only for a record that
  names a unit, and send that unit. A record that names none shows no such
  control; a tool, or a runtime without a unit, reads "Installed".
- *A domain's tabs.* Which tabs exist follows the last answer the server gave. A
  later read of the capabilities that fails neither brings back a tab the
  server ruled out nor takes one away, and the person is moved only off a tab
  that is no longer there.
- *Counts.* The four dashboard counts, the totals above the account and plan
  lists and the navigation badge are a number only for an answer: "…" while it
  is read, "–" when it could not be read (no badge in the navigation).
- *The order on the certificate card.* The state of the certificate, or what
  became of the request, comes first; then the three preparation steps; then a
  request that is not settled, beside the form it was sent from. A notice that
  appears away from where the person is looking is scrolled into view (also on
  the import page).

**The texts.** Keys are in `web/src/i18n` (`common.*` in the shell catalogue,
the rest in `screens` and `screens/server`).

- *A change whose answer did not arrive, on any of these screens (shell)*
  - `common.resultUnknown`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. Nothing is sent a second time. What is shown
      is being read again; check it before repeating the action."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. Hiçbir şey ikinci kez gönderilmez. Gösterilen
      yeniden okunuyor; işlemi yinelemeden önce ona bakın."
- *Settings, two-factor sign-in: checking, could not check, a change whose
  answer was lost*
  - `settings.2fa.checking`
    - EN: "Checking whether two-factor authentication is on for this account…"
    - TR: "Bu hesapta iki faktörlü doğrulamanın açık olup olmadığı kontrol
      ediliyor…"
  - `settings.2fa.unknown`
    - EN: "CelikPanel could not check whether two-factor authentication is on
      for this account, so neither turning it on nor turning it off is offered.
      This does not mean it is off. Nothing was changed. Try again."
    - TR: "CelikPanel bu hesapta iki faktörlü doğrulamanın açık olup olmadığını
      kontrol edemedi; bu yüzden açma da kapatma da sunulmuyor. Bu, kapalı
      olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `settings.2fa.resultUnknown`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. CelikPanel is reading the current state
      again; nothing is sent a second time."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. CelikPanel güncel durumu yeniden okuyor; hiçbir
      şey ikinci kez gönderilmez."
- *Settings, the certificate the Panel serves: checking, could not read, the
  Panel found none it could read*
  - `panelCert.checking`
    - EN: "Reading the certificate the Panel serves…"
    - TR: "Panelin sunduğu sertifika okunuyor…"
  - `panelCert.unknown`
    - EN: "The certificate the Panel serves could not be read from the server,
      so its state is not shown and a new one cannot be requested yet. This does
      not mean the certificate is missing or invalid. Nothing was changed. Try
      again."
    - TR: "Panelin sunduğu sertifika sunucudan okunamadı; bu yüzden durumu
      gösterilmiyor ve şimdilik yenisi istenemiyor. Bu, sertifikanın eksik ya da
      geçersiz olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - `panelCert.notReadable`
    - EN: "The Panel answered, but it found no certificate it could read in its
      certificate folder, so it cannot say which one it serves. Nothing was
      changed."
    - TR: "Panel yanıt verdi, ancak sertifika klasöründe okuyabildiği bir
      sertifika bulamadı; bu yüzden hangisini sunduğunu söyleyemiyor. Hiçbir şey
      değiştirilmedi."
- *The same card, a request whose result could not be confirmed (with "Check
  again" and a link to the secure address)*
  - `panelCert.unconfirmed`
    - EN: "CelikPanel could not confirm the result of the certificate request
      for {domain}: this page is not getting an answer about it. The request may
      still be running, may have finished or may have failed. Nothing else was
      started, and a second request is not offered until this one is known. If
      the Panel has restarted with the new certificate, it now answers at
      {address}."
    - TR: "CelikPanel, {domain} için sertifika isteğinin sonucunu doğrulayamadı:
      bu sayfa onunla ilgili yanıt alamıyor. İstek hâlâ sürüyor, tamamlanmış ya
      da başarısız olmuş olabilir. Başka hiçbir şey başlatılmadı; bu isteğin
      sonucu bilinene dek ikinci bir istek sunulmaz. Panel yeni sertifikayla
      yeniden başladıysa artık {address} adresinde yanıt verir."
  - `panelCert.checkAgain`
    - EN: "Check again"
    - TR: "Tekrar kontrol et"
  - `panelCert.openSecure`
    - EN: "Open the secure address"
    - TR: "Güvenli adresi aç"
- *The same card, a request the server reports as failed, with and without a
  reason; a request the server never recorded*
  - `panelCert.failedDetail`
    - EN: "The certificate for {domain} was not issued. The server reported:
      {reason} The Panel keeps serving its current certificate. Correct the
      cause, then request the certificate again."
    - TR: "{domain} için sertifika alınamadı. Sunucunun bildirdiği: {reason}
      Panel mevcut sertifikasını sunmayı sürdürüyor. Nedeni giderin, sonra
      sertifikayı yeniden isteyin."
  - `panelCert.failedPlain`
    - EN: "The certificate for {domain} was not issued, and the server gave no
      reason. The Panel keeps serving its current certificate. Check the three
      steps above, then request the certificate again."
    - TR: "{domain} için sertifika alınamadı ve sunucu bir neden bildirmedi.
      Panel mevcut sertifikasını sunmayı sürdürüyor. Yukarıdaki üç adımı kontrol
      edin, sonra sertifikayı yeniden isteyin."
  - `panelCert.notRecorded`
    - EN: "The server has no record of the certificate request for {domain}, so
      it was not started and nothing was changed. The certificate state above
      was read again; you can request the certificate again."
    - TR: "Sunucuda {domain} için sertifika isteğinin kaydı yok; yani istek
      başlatılmadı ve hiçbir şey değiştirilmedi. Yukarıdaki sertifika durumu
      yeniden okundu; sertifikayı yeniden isteyebilirsiniz."
- *The same card after the certificate was issued: the page that asked, before
  it moves ({seconds} counts down)*
  - `panelCert.reopen.title`
    - EN: "Certificate issued for {domain}"
    - TR: "{domain} için sertifika alındı"
  - `panelCert.reopen.body`
    - EN: "The Panel is restarting to serve the new certificate. The certificate
      is valid for {domain} only, so this page will reopen at the Panel’s secure
      address, {address}, in {seconds} s. Because that is a different address,
      you may be asked to sign in again there."
    - TR: "Panel yeni sertifikayı sunmak için yeniden başlıyor. Sertifika yalnız
      {domain} için geçerli olduğundan bu sayfa {seconds} sn içinde Panelin
      güvenli adresinde, {address} adresinde yeniden açılacak. Bu farklı bir
      adres olduğu için orada yeniden oturum açmanız istenebilir."
  - `panelCert.reopen.reload`
    - EN: "The Panel is restarting to serve the new certificate. This page will
      reload in {seconds} s so that the browser uses it."
    - TR: "Panel yeni sertifikayı sunmak için yeniden başlıyor. Tarayıcının onu
      kullanması için bu sayfa {seconds} sn içinde yeniden yüklenecek."
  - `panelCert.reopen.stay`
    - EN: "Stay here"
    - TR: "Burada kal"
- *The same card after "Stay here", in another section, or in another tab*
  - `panelCert.reopen.stayed`
    - EN: "The Panel restarted to serve the new certificate, which is valid for
      {domain} only. This page was left where it is; the browser may warn about
      the certificate at this address. The Panel’s secure address is {address}."
    - TR: "Panel yeni sertifikayı sunmak için yeniden başladı; sertifika yalnız
      {domain} için geçerlidir. Bu sayfa olduğu yerde bırakıldı; tarayıcı bu
      adreste sertifika uyarısı gösterebilir. Panelin güvenli adresi: {address}"
- *Accounts: the list and the plans*
  - `users.checking`
    - EN: "Reading the accounts…"
    - TR: "Hesaplar okunuyor…"
  - `users.unknown`
    - EN: "The accounts could not be read from the server, so the list is not
      shown. This does not mean there are no accounts. Nothing was changed. Try
      again."
    - TR: "Hesaplar sunucudan okunamadı; bu yüzden liste gösterilmiyor. Bu,
      hesap olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - `users.plansUnknown`
    - EN: "The plans could not be read from the server, so an account cannot be
      created here yet. This does not mean there are no plans. Nothing was
      changed. Try again."
    - TR: "Planlar sunucudan okunamadı; bu yüzden şimdilik burada hesap
      oluşturulamıyor. Bu, plan olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `plans.checking`
    - EN: "Reading the plans…"
    - TR: "Planlar okunuyor…"
  - `plans.unknown`
    - EN: "The plans could not be read from the server, so the list is not
      shown. This does not mean there are no plans. Nothing was changed. Try
      again."
    - TR: "Planlar sunucudan okunamadı; bu yüzden liste gösterilmiyor. Bu, plan
      olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *A domain page opened by its address*
  - `domain.checking`
    - EN: "Reading this domain…"
    - TR: "Bu alan adı okunuyor…"
  - `domain.unknown`
    - EN: "This domain could not be read from the server, so its page is not
      shown. This does not mean the domain is gone. Nothing was changed. Try
      again."
    - TR: "Bu alan adı sunucudan okunamadı; bu yüzden sayfası gösterilmiyor. Bu,
      alan adının kaldırıldığı anlamına gelmez. Hiçbir şey değiştirilmedi.
      Tekrar deneyin."
  - `domain.absent`
    - EN: "This domain is not on this server"
    - TR: "Bu alan adı bu sunucuda değil"
  - `domain.absentHint`
    - EN: "The server answered, and its list has no such domain. It may have
      been removed, or the address may be mistyped. Nothing was changed."
    - TR: "Sunucu yanıt verdi ve listesinde böyle bir alan adı yok. Kaldırılmış
      ya da adres yanlış yazılmış olabilir. Hiçbir şey değiştirilmedi."
  - `domain.noAccess`
    - EN: "This account has no access to this domain"
    - TR: "Bu hesabın bu alan adına erişimi yok"
  - `domain.noAccessHint`
    - EN: "The domain is on this server, but no part of it is shared with this
      account. The account owner can grant access under Team members."
    - TR: "Alan adı bu sunucuda, ancak hiçbir bölümü bu hesapla paylaşılmamış.
      Hesap sahibi, Ekip üyeleri altından erişim verebilir."
- *A domain’s files*
  - `files.checking`
    - EN: "Reading this folder…"
    - TR: "Bu klasör okunuyor…"
  - `files.unknown`
    - EN: "This folder could not be read from the server, so its contents are
      not shown. This does not mean the folder is empty. Nothing was changed.
      Try again."
    - TR: "Bu klasör sunucudan okunamadı; bu yüzden içeriği gösterilmiyor. Bu,
      klasörün boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - `files.contentUnknown`
    - EN: "The contents of this file could not be read from the server, so it
      was not opened for editing. Nothing was changed. Try again."
    - TR: "Bu dosyanın içeriği sunucudan okunamadı; bu yüzden düzenlemek için
      açılmadı. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `files.uploadUnreadable`
    - EN: "The browser could not read {name} from this device, so nothing was
      uploaded. Choose the file again."
    - TR: "Tarayıcı {name} dosyasını bu cihazdan okuyamadı; bu yüzden hiçbir şey
      yüklenmedi. Dosyayı yeniden seçin."
- *A domain’s certificate*
  - `ssl.checking`
    - EN: "Reading this domain’s certificate…"
    - TR: "Bu alan adının sertifikası okunuyor…"
  - `ssl.unknown`
    - EN: "The certificate of this domain could not be read from the server, so
      its state is not shown and a certificate cannot be requested or removed
      here yet. This does not mean the domain has no certificate. Nothing was
      changed. Try again."
    - TR: "Bu alan adının sertifikası sunucudan okunamadı; bu yüzden durumu
      gösterilmiyor ve şimdilik buradan sertifika istenemiyor ya da
      kaldırılamıyor. Bu, alan adının sertifikası olmadığı anlamına gelmez.
      Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `ssl.rereading`
    - EN: "Reading the certificate again. What is shown below is the earlier
      answer; the controls are off until the server has answered…"
    - TR: "Sertifika yeniden okunuyor. Aşağıda gösterilen önceki yanıttır;
      sunucu yanıt verene dek denetimler kapalı…"
  - `ssl.checkingProviders`
    - EN: "Reading the certificate authorities this server offers…"
    - TR: "Bu sunucunun sunduğu sertifika yetkilileri okunuyor…"
  - `ssl.providersUnknown`
    - EN: "The certificate authorities this server offers could not be read, so
      a certificate cannot be requested here yet. Nothing was changed. Try
      again."
    - TR: "Bu sunucunun sunduğu sertifika yetkilileri okunamadı; bu yüzden
      şimdilik buradan sertifika istenemiyor. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
- *Import: the subscriptions and the archive*
  - `import.checkingSubs`
    - EN: "Reading the subscriptions…"
    - TR: "Abonelikler okunuyor…"
  - `import.subsUnknown`
    - EN: "The subscriptions could not be read from the server, so a target
      cannot be chosen and the import cannot be started yet. This does not mean
      there are no subscriptions. Nothing was changed. Try again."
    - TR: "Abonelikler sunucudan okunamadı; bu yüzden şimdilik hedef seçilemiyor
      ve içe aktarım başlatılamıyor. Bu, abonelik olmadığı anlamına gelmez.
      Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `import.noSubs`
    - EN: "There is no subscription to import into yet. Create an account with a
      plan under Accounts first."
    - TR: "İçe aktarılacak bir abonelik henüz yok. Önce Hesaplar altında planlı
      bir hesap oluşturun."
  - `import.inspectUnanswered`
    - EN: "The server did not answer, so the archive was not inspected.
      Inspecting only reads the archive; nothing was changed. Try again."
    - TR: "Sunucu yanıt vermedi; bu yüzden arşiv incelenmedi. İnceleme arşivi
      yalnız okur; hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Import: an apply whose answer was lost*
  - `import.unknown.title`
    - EN: "The result of this import is not known"
    - TR: "Bu içe aktarımın sonucu bilinmiyor"
  - `import.unknown.body`
    - EN: "The connection dropped before the server answered. The import may
      have run completely, in part, or not at all. Nothing is sent again by
      itself, and starting it again is not offered until you have checked. Check
      whether {domain} is on this server now; checking only reads."
    - TR: "Sunucu yanıt vermeden bağlantı koptu. İçe aktarım tamamen, kısmen
      çalışmış ya da hiç çalışmamış olabilir. Hiçbir şey kendiliğinden yeniden
      gönderilmez; siz kontrol edene dek yeniden başlatma sunulmaz. {domain}
      alan adının şu an bu sunucuda olup olmadığını kontrol edin; kontrol yalnız
      okur."
  - `import.unknown.check`
    - EN: "Check {domain}"
    - TR: "{domain} alan adını kontrol et"
  - `import.unknown.checkAgain`
    - EN: "Check {domain} again"
    - TR: "{domain} alan adını yeniden kontrol et"
  - `import.unknown.unreadable`
    - EN: "The domain list could not be read, so it is still not known whether
      the import ran. Nothing was changed. Check again."
    - TR: "Alan adı listesi okunamadı; bu yüzden içe aktarımın çalışıp
      çalışmadığı hâlâ bilinmiyor. Hiçbir şey değiştirilmedi. Yeniden kontrol
      edin."
  - `import.unknown.present`
    - EN: "{domain} is on this server now, so the import created it. Which of
      its files, mail, DNS records and databases were imported is not known
      here, and the import is not started again from this page. Open the domain
      and look at each of them."
    - TR: "{domain} şu an bu sunucuda; yani içe aktarım onu oluşturdu.
      Dosyalarından, postasından, DNS kayıtlarından ve veritabanlarından
      hangilerinin aktarıldığı burada bilinmiyor ve içe aktarım bu sayfadan
      yeniden başlatılmaz. Alan adını açıp her birine bakın."
  - `import.unknown.open`
    - EN: "Open {domain}"
    - TR: "{domain} alan adını aç"
  - `import.unknown.absent`
    - EN: "{domain} is not on this server at this moment, so the import has not
      created it. If the server is still working on the archive it can appear
      later: check again in a moment. If it is still absent, you can start the
      import again."
    - TR: "{domain} şu an bu sunucuda değil; yani içe aktarım onu oluşturmadı.
      Sunucu hâlâ arşiv üzerinde çalışıyorsa sonradan görünebilir: biraz sonra
      yeniden kontrol edin. Hâlâ yoksa içe aktarımı yeniden başlatabilirsiniz."
  - `import.runAgain`
    - EN: "Start import again"
    - TR: "İçe aktarımı yeniden başlat"
- *Dashboard: the license notice when the license could not be verified; a count
  that could not be read*
  - `license.noticeUnverified`
    - EN: "CelikPanel could not verify the license just now. This does not mean
      your license is missing or expired, and nothing was changed. Existing
      sites, mail, databases and scheduled tasks keep running. You can check
      again under License."
    - TR: "CelikPanel lisansı şu an doğrulayamadı. Bu, lisansınızın eksik ya da
      süresinin dolmuş olduğu anlamına gelmez ve hiçbir şey değiştirilmedi.
      Mevcut siteler, e-posta, veritabanları ve zamanlanmış görevler çalışmaya
      devam eder. Lisans bölümünden yeniden kontrol edebilirsiniz."
  - `license.noticeOpen`
    - EN: "Open License"
    - TR: "Lisans bölümünü aç"
  - `dashboard.countUnread`
    - EN: "could not be read"
    - TR: "okunamadı"
- *Monitoring*
  - `monitoring.checking`
    - EN: "Reading the recorded measurements…"
    - TR: "Kaydedilen ölçümler okunuyor…"
  - `monitoring.unknown`
    - EN: "The recorded measurements could not be read from the server, so no
      charts are shown. This does not mean nothing was recorded. Nothing was
      changed. Try again."
    - TR: "Kaydedilen ölçümler sunucudan okunamadı; bu yüzden grafik
      gösterilmiyor. Bu, hiçbir şey kaydedilmediği anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *A component’s page: its record*
  - `svc.checkingRecord`
    - EN: "Reading what is known about {name} on this server…"
    - TR: "Bu sunucuda {name} hakkında bilinen okunuyor…"
  - `svc.recordUnread`
    - EN: "Could not be read"
    - TR: "Okunamadı"
  - `svc.recordUnknown`
    - EN: "CelikPanel could not read what is known about {name} on this server,
      so its state is not shown and nothing is offered for it. This does not
      mean {name} is missing, stopped or unchecked. Nothing was changed. Try
      again."
    - TR: "CelikPanel bu sunucuda {name} hakkında bilineni okuyamadı; bu yüzden
      durumu gösterilmiyor ve onun için hiçbir işlem sunulmuyor. Bu, {name}
      bileşeninin eksik, durmuş ya da bakılmamış olduğu anlamına gelmez. Hiçbir
      şey değiştirilmedi. Tekrar deneyin."
  - `svc.recordStale`
    - EN: "The state of {name} could not be read again just now, so what is
      shown is the earlier answer. Nothing was changed. Start, stop and restart
      are off until it has been read again."
    - TR: "{name} durumu az önce yeniden okunamadı; bu yüzden gösterilen önceki
      yanıttır. Hiçbir şey değiştirilmedi. Başlat, durdur ve yeniden başlat,
      yeniden okunana dek kapalıdır."
  - `svc.installChecking`
    - EN: "Checking for a running operation…"
    - TR: "Süren bir işlem var mı, kontrol ediliyor…"
  - `component.checking`
    - EN: "Reading this component’s record…"
    - TR: "Bu bileşenin kaydı okunuyor…"
  - `component.unknown`
    - EN: "This component’s record could not be read from the server, so its
      unit, versions, ports, configuration files and log are not shown. This
      does not mean it has none. Nothing was changed. Try again."
    - TR: "Bu bileşenin kaydı sunucudan okunamadı; bu yüzden birimi, sürümleri,
      portları, ayar dosyaları ve günlüğü gösterilmiyor. Bu, bunların olmadığı
      anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `component.absent`
    - EN: "This server’s catalogue has no component “{id}”"
    - TR: "Bu sunucunun kataloğunda “{id}” bileşeni yok"
  - `component.absentHint`
    - EN: "The server answered, and its list has no such component. The address
      may be mistyped, or the component is not offered on this server. Nothing
      was changed."
    - TR: "Sunucu yanıt verdi ve listesinde böyle bir bileşen yok. Adres yanlış
      yazılmış olabilir ya da bileşen bu sunucuda sunulmuyor. Hiçbir şey
      değiştirilmedi."
- *A component’s page: its log*
  - `component.logsChecking`
    - EN: "Reading the log of {unit}…"
    - TR: "{unit} günlüğü okunuyor…"
  - `component.logsUnknown`
    - EN: "The log of {unit} could not be read from the server, so no lines are
      shown. This does not mean the log is empty. Nothing was changed. Try
      again."
    - TR: "{unit} günlüğü sunucudan okunamadı; bu yüzden hiçbir satır
      gösterilmiyor. Bu, günlüğün boş olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `component.logsNoMatch`
    - EN: "No line in the log contains “{filter}”."
    - TR: "Günlükte “{filter}” içeren satır yok."
- *The install dialogue of the components list*
  - `services.repo.checking`
    - EN: "Checking whether this component needs an additional package
      repository…"
    - TR: "Bu bileşenin ek bir paket deposuna ihtiyacı var mı, kontrol
      ediliyor…"
  - `services.repo.unknown`
    - EN: "CelikPanel could not check whether {name} needs an additional package
      repository on this server, so installing is not offered yet. This does not
      mean a repository is missing. Nothing was changed. Try again."
    - TR: "CelikPanel, {name} bileşeninin bu sunucuda ek bir paket deposuna
      ihtiyacı olup olmadığını kontrol edemedi; bu yüzden şimdilik kurulum
      sunulmuyor. Bu, deponun eksik olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `services.repo.stale`
    - EN: "The package repository of {name} could not be read again just now, so
      what is shown below is the earlier answer. Nothing was changed. Installing
      and changing the repository are off until it has been read again."
    - TR: "{name} bileşeninin paket deposu az önce yeniden okunamadı; bu yüzden
      aşağıda gösterilen önceki yanıttır. Hiçbir şey değiştirilmedi. Kurulum ve
      depo değişikliği, yeniden okunana dek kapalıdır."
  - `services.versionUnknown`
    - EN: "could not be checked"
    - TR: "kontrol edilemedi"
- *The help drawer*
  - `help.loading`
    - EN: "Fetching the help text…"
    - TR: "Yardım metni getiriliyor…"
  - `help.unknown`
    - EN: "The help text could not be fetched from the panel. Nothing was
      changed. Try again."
    - TR: "Yardım metni panelden getirilemedi. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
- *Setup guide, the sentence that names the button (the button reads "Edit plan"
  / "Planı düzenle")*
  - `setup.guide.accessDNSResume`
    - EN: "This prerequisite is checked automatically. After it passes, the same
      setup continues. To change the reviewed names or addresses, use Edit plan
      below."
    - TR: "Bu gereksinim otomatik kontrol edilir. Doğrulanınca aynı kurulum
      devam eder. İncelenen adları veya adresleri değiştirmek için aşağıdaki
      Planı düzenle düğmesini kullanın."

**Corrected in the same screens.**

- The sentence in the setup guide named a button "Edit setup plan" /
  "Kurulum planını düzenle"; the button reads "Edit plan" / "Planı düzenle". The
  sentence now names it as it is written.
- In the dark theme the scrim of the access hold and of the reload notice is
  drawn at half strength. The page was already navy, and at the strength the
  light theme needs, small muted text under those layers was near 1.5:1.
- On a phone the start, stop and restart controls of a component ran off the
  right edge; they wrap now.
- A log filter that matched no line said "No log entries"; it now says that no
  line contains the filter.

**How it is kept from coming back.**

- *The ratchet.* After this batch: 27 / 43 / 12 / 83 / 25 in 53 files (after the
  first: 37 / 58 / 14 / 109 / 29 in 62). With the database and mail
  configuration screens of the next entry in the same tree: 20 / 36 / 10 / 71 /
  23 in 47 files, which is what the test pins. Off the list for good: `Settings`,
  `UsersPage`, `DomainFileManager`, `DomainSSLSettings`, `DomainDetail`,
  `ImportPage`, `LicenseNotice`, `MonitoringPage`, `ComponentDetail`, and the
  new `ServiceRecordLookup`. Lower but still listed: `App` (one response
  observer), `ServiceList` (the install dialogue moved; the list itself did
  not), `Dashboard` (the three count reads moved), `Layout` (the domain badge
  moved), `ServiceShell` (no count changed: its read, its check and its install
  are pinned line by line by three safety contracts, so it got a "could not be
  read" state inside that structure instead of a new reader).
- *One reader per address.* `lib/managedServices.ts` (the stored component
  records, through the operation tracker's fail-closed decoder),
  `lib/subscriptions.ts`, `lib/accounts.ts`; the domain list is read by the
  Domains page, a domain's page, its lookup by name, the dashboard and the
  navigation rail through one address and one decoder, so they share a request.
  The stored component records have one decoder for every reader, the
  PostgreSQL and MariaDB pages of the next entry among them
  (`useComponentConfigFiles` is `useManagedServices` with one fact taken out):
  it also refuses a list of configuration files that is not a list. A section
  that mounts within 30 seconds of the page's own read uses that answer and
  sends nothing, as the hosting capabilities already did, so a component's page
  makes one shared request beside its header's own.
  `Remote` carries the HTTP status of a refusal, so "no such record" can be told
  from "no answer"; `countText` writes a count; `CouldNotCheck` takes a second
  way to look beside Retry.
- *The mounted test* gained every screen above: 12 more rows of the table (each
  read withheld four ways, then answered negatively) and 14 tests of what the
  screens do around their own change: the certificate request seen from the page
  that asked and from another tab; a poll with no answer; a lost answer in
  Accounts; a folder change; a certificate request whose re-read answers and one
  whose re-read fails; the import's lost apply with the domain present and
  absent; the license notice; a failed monitoring poll; a domain opened by
  name; a component's record, its unit and its Install label; and one test that
  reads the source of the four screens this file cannot mount.
- *Room.* The help texts (about 100 KiB for both languages) were a static part
  of every page that carries the Help button and had brought the Settings route
  to 0.29 KiB of its limit. They are fetched when a help drawer is opened; the
  drawer says that it is fetching, or that it could not, with Retry. The bundle
  check names that chunk, so merging it back fails the build. No limit was
  raised: Settings 272.90 → 180.67 KiB (79.71 → 48.35 gzip), a domain's page
  236.45 → 139.31 KiB.

**Needs the server; not changed here.**

- `GET /api/v1/panel/certificate` answers `https_enabled: false, self_signed:
  false` both when there is no certificate file and when the file could not be
  read or parsed. The interface now says that the Panel found no certificate it
  could read; it cannot say which of the two it is.
- The import's apply has no request identity and no operation record, and it
  runs on the request's own context. After a lost answer its result can only be
  inferred from the domain list. In the browser run, one send by the page
  reached the mock on six or seven connections (the browser repeats a request on
  a connection that is closed under it), which is harmless against a mock and
  is exactly what an idempotency key exists for.
- A change whose answer was lost (accounts, plans, files) has no identity
  either; the screen can only read the state again.

**Not done.**

- 47 files still read the old way (53 before the database and mail
  configuration screens of the next entry). Of this batch's screens: the components list
  outside its install dialogue, the dashboard outside its counts (system
  figures, firewall, audit trail, component summary), the navigation rail's
  version and component reads, and `ServiceShell`'s reader.
- The dashboard's "Needs attention" list depends on the domain list and appears
  when that arrives; with the list made slow on purpose, everything under it
  moved down by 118 px (158 px on a phone). In use the rail has already read the
  list when the dashboard opens.
- A count on the dashboard that could not be read has no Retry of its own; the
  page the card opens has it.
- The license notice says nothing when its own read fails.
- Roles other than the administrator were not exercised for these screens.

**Browser inspection of this batch (2026-10-09).** In a real, installed Chrome
against the loopback mock (`web/tools/browser-inspect`, scenarios `twofactor`,
`panelcert`, `accounts`, `files`, `domainssl`, `importer`, `dashboard`,
`monitoring`, `lookup`, `component`, `installdialog`, `scrim`): desktop 1440×900
and phone 390×844, Turkish and English, light and dark; 69 states in each of the
eight configurations.

- *Measured in all eight configurations.* No state drew a negative sentence
  before the server had said so, no checking state drew a notice, and no failed
  read still said that it was checking. After the certificate was issued, the
  page that asked moved once, to `https://panel.example.com:<port>/`, after its
  ten seconds on its own section; after "Stay here", from another section and
  in another tab nothing moved. While the result of a request could not be
  confirmed, "Get certificate" was disabled. The import page sent the apply
  once; the check requested `GET /api/v1/domains` and nothing else; "Start
  import again" was offered only after the domain was found absent. The
  dashboard counts read "…" and then numbers, and "–" for failed reads; the
  navigation badge was absent and then "2". After the poll that failed a minute
  later, every monitoring chart was still drawn under the notice. A folder
  opened while its read was slow showed none of the rows of the folder just
  left. The four pages opened by their address stayed on it. A component whose
  record could not be read showed "Could not be read" and only Help; one
  without a unit showed "Installed" and only Help. In the install dialogue
  Install was disabled while the repository was checked, when it could not be
  checked, and for a required repository that is not enabled. The help text was
  requested once, when the drawer was opened. A domain's tabs were the same
  before and after a capability read that failed. Between each checking state
  and the known one that followed it, nothing that was measured changed place
  or size, except on the dashboard (below).
- *Found by looking and corrected.* On a phone the certificate card began with
  its three preparation steps, so the state and "could not be confirmed" were
  below the fold; the state now comes first and a notice that appears is
  scrolled into view. The certificate's state line grew by 34 px on a phone
  when the answer arrived, and the two-factor card by 12 px; both keep a least
  height for their width. "Check again" and the link to the secure address were
  two stacked blocks; they share a row. While a domain's certificate was read
  again after a request, "No certificate" stood under a success toast with a
  line that only said it was reading; the line now says that what is shown is
  the earlier answer. A tool with no daemon read "Stopped" at the top of its
  page. On a phone the start, stop and restart controls ran off the right edge.
  The import's notice appeared at the end of a long column, out of view on a
  phone.
- *Seen and not changed here.* The dashboard's attention list (above, "Not
  done"). On a phone the account and file tables scroll sideways, so their row
  controls are off screen until the table is scrolled. A disabled primary
  button is close to an enabled one in the dark theme (the install dialogue).
  The success toast covers the theme and account controls for its few seconds.
- *Not covered.* Any real server, certificate or restart: the move to the secure
  address was recorded and stopped, never followed. Safari, Firefox, a screen
  reader, a touch device; the imitation skins; roles other than the
  administrator. The forms that create an account or a plan were not submitted.
  The setup-guide sentence was changed in the catalogue and not seen on screen.

### Database and mail configuration screens: a file is read before it is an editor, and a save says what the service did with it (2026-10-09)

Source state with component tests, the two validating programs run for real on a
development guest, and a browser inspection against a loopback mock (at the end
of this entry); no installed server. See the resilience contract entry of the
same date. It applies the rule of the entry above ("no negative state unless it
is known") to the PostgreSQL and MariaDB pages, a domain's mail tabs and the
Postfix page, and adds what a configuration save must say: the result of every
save is one of saved (with what happened to the running service), refused before
any change, failed after a change with the previous file back, or unknown.

**Who acts, in every case below.** The person at the screen, unless a text says
"on the server": then the server owner, with the command in the text. Nothing
retries by itself. Retry, "Reload the file" and "Reload the current address"
only read.

**The texts.** Keys are in `web/src/i18n/screens/server` (database, queue) and
`web/src/i18n/screens` (mail); `{file}` is the file's own name
(`postgresql.conf`), `{service}` the service's (`PostgreSQL`, `MariaDB`).

- *A configuration file: being read, could not be read (unknown result; no editor, no Save).*
  - `dbconf.files.checking` (which files the component has):
    - EN: "Reading which configuration files {service} has on this server…"
    - TR: "{service} hizmetinin bu sunucudaki yapılandırma dosyaları okunuyor…"
  - `dbconf.files.unknown` (the same read failed):
    - EN: "The configuration files of {service} could not be read from the
      server, so none is shown. This does not mean a file is missing. Nothing
      was changed. Try again."
    - TR: "{service} hizmetinin yapılandırma dosyaları sunucudan okunamadı; bu
      yüzden hiçbiri gösterilmiyor. Bu, bir dosyanın eksik olduğu anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `dbconf.checking` (the file itself):
    - EN: "Reading {file} from the server…"
    - TR: "{file} sunucudan okunuyor…"
  - `dbconf.unknown` (the file could not be read):
    - EN: "{file} could not be read from the server, so its settings are not
      shown and nothing can be saved here. This does not mean the file is empty
      or missing. Nothing was changed. Try again."
    - TR: "{file} sunucudan okunamadı; bu yüzden ayarları gösterilmiyor ve
      buradan kayıt yapılamıyor. Bu, dosyanın boş ya da eksik olduğu anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *A configuration file: known. What the editor says about itself.*
  - `dbconf.note`:
    - EN: "Only the lines you change are rewritten. Comments, includes and
      everything this screen does not show stay in the file exactly as they
      are."
    - TR: "Yalnız değiştirdiğiniz satırlar yeniden yazılır. Yorumlar, include
      satırları ve bu ekranın göstermediği her şey dosyada olduğu gibi kalır."
  - `dbconf.noSettings`:
    - EN: "CelikPanel found no setting it can show in {file}. The whole file can
      be read and edited under “Advanced: raw files”."
    - TR: "CelikPanel, {file} içinde gösterebileceği bir ayar bulamadı. Dosyanın
      tamamı “Gelişmiş: ham dosyalar” altında okunabilir ve düzenlenebilir."
  - `dbconf.hba.none`:
    - EN: "This file holds no access rule. PostgreSQL then refuses every
      connection."
    - TR: "Bu dosyada hiç erişim kuralı yok. PostgreSQL bu durumda her
      bağlantıyı reddeder."
  - `dbconf.hba.order`:
    - EN: "PostgreSQL uses the first rule that matches a connection, so the
      order matters. New rules are added at the end; to place a rule elsewhere,
      edit the file under “Advanced: raw files”."
    - TR: "PostgreSQL bir bağlantıyla eşleşen ilk kuralı kullanır; bu yüzden
      sıra önemlidir. Yeni kurallar sona eklenir; bir kuralı başka bir yere
      koymak için dosyayı “Gelişmiş: ham dosyalar” altında düzenleyin."
  - `dbconf.hba.asWritten`:
    - EN: "Shown as written in the file. This screen does not change or remove
      it; edit it under “Advanced: raw files”."
    - TR: "Dosyada yazıldığı gibi gösteriliyor. Bu ekran onu değiştirmez ya da
      kaldırmaz; “Gelişmiş: ham dosyalar” altında düzenleyin."
  - `dbconf.hba.incomplete`:
    - EN: "Fill in every field of the changed or new rule before saving."
    - TR: "Kaydetmeden önce değişen ya da yeni kuralın her alanını doldurun."
- *Save refused because the file changed after it was read (verified refusal, `409 SETTINGS_CHANGED` or `SETTINGS_VERSION_REQUIRED`; what was typed stays, Save is off, the one action reloads).*
  - `dbconf.stale`:
    - EN: "{file} changed on the server after this page read it, so nothing was
      saved. What you changed is still shown below. Reload the file, then make
      your change again."
    - TR: "{file}, bu sayfa onu okuduktan sonra sunucuda değişti; bu yüzden
      hiçbir şey kaydedilmedi. Değiştirdikleriniz aşağıda duruyor. Dosyayı
      yeniden yükleyin, sonra değişikliğinizi tekrar yapın."
  - `dbconf.reload` (the action):
    - EN: "Reload the file"
    - TR: "Dosyayı yeniden yükle"
- *Save refused before anything was written (`422 CONFIG_INVALID`; verified refusal, the person at the screen corrects and saves again). The line the service said is shown next to the field or rule it names, after "{service} says:" or "Not accepted:".*
  - `dbconf.refused.daemon` (reason `daemon`):
    - EN: "Nothing is changed: {service} read the new file and does not accept
      it. Correct what it names and save again."
    - TR: "Hiçbir şey değişmedi: {service} yeni dosyayı okudu ve kabul etmiyor.
      Adını verdiği yeri düzeltip yeniden kaydedin."
  - `dbconf.refused.syntax` (reason `syntax`):
    - EN: "Nothing was saved: a rule you changed or added is not one {service}
      accepts. Correct the rule marked below and save again."
    - TR: "Hiçbir şey kaydedilmedi: değiştirdiğiniz ya da eklediğiniz bir kural
      {service} tarafından kabul edilen bir kural değil. Aşağıda işaretlenen
      kuralı düzeltip yeniden kaydedin."
  - `dbconf.refused.lockout` (reason `lockout`):
    - EN: "Nothing was saved: this change would take away the local
      administrator access to PostgreSQL, the rule that lets the server’s own
      postgres account connect over the local socket. CelikPanel and your own
      console both use it. Keep a “local all postgres peer” rule above any rule
      that would refuse it, then save again."
    - TR: "Hiçbir şey kaydedilmedi: bu değişiklik PostgreSQL’e yerel yönetici
      erişimini, yani sunucunun kendi postgres hesabının yerel soket üzerinden
      bağlanmasını sağlayan kuralı kaldırırdı. Onu hem CelikPanel hem de sizin
      konsolunuz kullanır. Onu reddedecek her kuralın üstünde bir “local all
      postgres peer” kuralı bırakın, sonra yeniden kaydedin."
  - `dbconf.refused.empty` (reason `empty`):
    - EN: "Nothing was saved: the new content is empty, and CelikPanel does not
      replace a configuration file with nothing. Reload the file, then make the
      change again."
    - TR: "Hiçbir şey kaydedilmedi: yeni içerik boş ve CelikPanel bir
      yapılandırma dosyasını boş içerikle değiştirmez. Dosyayı yeniden yükleyin,
      sonra değişikliği tekrar yapın."
  - `dbconf.refused.shape` (reason `shape`):
    - EN: "Nothing was saved: the new content is not a configuration file
      CelikPanel writes (it is larger than 1 MB or holds a NUL byte)."
    - TR: "Hiçbir şey kaydedilmedi: yeni içerik CelikPanel’in yazdığı bir
      yapılandırma dosyası değil (1 MB’tan büyük ya da NUL baytı içeriyor)."
  - `dbconf.refused.no_validator` (reason `no_validator`; an unmet prerequisite on the server):
    - EN: "Nothing was saved: the program that checks this file before it
      replaces the current one ({name}) could not be run on this server, and
      CelikPanel does not install a database configuration it could not check.
      The file can still be edited on the server itself."
    - TR: "Hiçbir şey kaydedilmedi: bu dosyayı geçerli dosyanın yerine konmadan
      önce denetleyen program ({name}) bu sunucuda çalıştırılamadı ve CelikPanel
      denetleyemediği bir veritabanı yapılandırmasını kurmaz. Dosya sunucunun
      kendisinde yine düzenlenebilir."
  - `dbconf.refused.other` (a reason this screen has no words for):
    - EN: "Nothing is changed: the new file was refused. Correct it and save
      again."
    - TR: "Hiçbir şey değişmedi: yeni dosya reddedildi. Düzeltip yeniden
      kaydedin."
  - `dbconf.says`:
    - EN: "{service} says:"
    - TR: "{service} yanıtı:"
  - `dbconf.notAccepted`:
    - EN: "Not accepted:"
    - TR: "Kabul edilmedi:"
  - `dbconf.atLine`:
    - EN: "Line {line}:"
    - TR: "{line}. satır:"
- *The reload failed after the file was installed (`502 CONFIG_RELOAD_FAILED`; a verified failure, drawn on the failure surface with the service's line).*
  - `dbconf.reloadFailed.restored` (reason `restored`: nothing is changed now):
    - EN: "The change was not kept: {service} could not reload with the new
      file, so CelikPanel put the previous file back and {service} is running
      with it. Correct the setting and save again."
    - TR: "Değişiklik tutulmadı: {service} yeni dosyayla yeniden yüklenemedi; bu
      yüzden CelikPanel önceki dosyayı geri koydu ve {service} onunla çalışıyor.
      Ayarı düzeltip yeniden kaydedin."
  - `dbconf.reloadFailed.notRestored` (reason `not_restored`: the server owner acts, on the server):
    - EN: "{service} could not reload with the new file, and CelikPanel could
      not put the previous file back with certainty. What the server holds now
      is shown below. Check the file on the server, reload {service} there, then
      reload this page. The other version is kept on the server as:"
    - TR: "{service} yeni dosyayla yeniden yüklenemedi ve CelikPanel önceki
      dosyayı kesin olarak geri koyamadı. Sunucunun şu an tuttuğu dosya aşağıda
      gösteriliyor. Dosyayı sunucuda denetleyin, {service} hizmetini orada
      yeniden yükleyin, sonra bu sayfayı yenileyin. Diğer sürüm sunucuda şu adla
      duruyor:"
- *The answer to a save never arrived (unknown result; the file is read again before anything else).*
  - `dbconf.saveUnknown`:
    - EN: "The answer to this save did not arrive, so CelikPanel does not know
      whether the file was changed. Reload the file to see what the server holds
      before saving again."
    - TR: "Bu kaydın yanıtı gelmedi; bu yüzden CelikPanel dosyanın değişip
      değişmediğini bilmiyor. Yeniden kaydetmeden önce sunucunun ne tuttuğunu
      görmek için dosyayı yeniden yükleyin."
- *Saved. What happened to the running service is said, never assumed.*
  - `dbconf.saved.reloaded` (PostgreSQL):
    - EN: "Saved. {service} read the file again."
    - TR: "Kaydedildi. {service} dosyayı yeniden okudu."
  - `dbconf.saved.waitsForRestart` (PostgreSQL: settings that wait):
    - EN: "These settings take effect only after {service} restarts: {names}.
      Restart it from the top of this page when it suits you."
    - TR: "Şu ayarlar ancak {service} yeniden başlatıldıktan sonra geçerli olur:
      {names}. Size uygun olduğunda bu sayfanın üstünden yeniden başlatın."
  - `dbconf.saved.notChecked` (the server could not be asked):
    - EN: "CelikPanel could not ask {service} whether it accepts every line of
      the file. The reload itself reported no error."
    - TR: "CelikPanel, {service} hizmetine dosyanın her satırını kabul edip
      etmediğini soramadı. Yeniden yüklemenin kendisi hata bildirmedi."
  - `dbconf.saved.restartRequired` (MariaDB):
    - EN: "Saved. {service} checked the file and accepts it. {service} reads
      this file only when it starts, so the change takes effect after its next
      restart. Restart it from the top of this page when it suits you."
    - TR: "Kaydedildi. {service} dosyayı denetledi ve kabul ediyor. {service} bu
      dosyayı yalnız başlarken okur; bu yüzden değişiklik bir sonraki yeniden
      başlatmadan sonra geçerli olur. Size uygun olduğunda bu sayfanın üstünden
      yeniden başlatın."
  - `dbconf.saved.notRunning` (the service is stopped):
    - EN: "Saved. {service} is not running, so it will read the file when it
      starts."
    - TR: "Kaydedildi. {service} çalışmıyor; dosyayı başladığında okuyacak."
  - `dbconf.saved.unchanged`:
    - EN: "Nothing to save: the file on the server already holds exactly this."
    - TR: "Kaydedilecek bir şey yok: sunucudaki dosya zaten tam olarak bunu
      tutuyor."
  - `dbconf.saved.backup`:
    - EN: "The previous file is kept on the server as"
    - TR: "Önceki dosya sunucuda şu adla duruyor:"
- *A domain's mail: being read, could not be read.*
  - `mail.accounts.checking`:
    - EN: "Reading the email accounts of this domain…"
    - TR: "Bu alan adının e-posta hesapları okunuyor…"
  - `mail.accounts.unknown`:
    - EN: "The email accounts of this domain could not be read from the server,
      so the list is not shown. This does not mean there are none. Nothing was
      changed. Try again."
    - TR: "Bu alan adının e-posta hesapları sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, e-posta hesabı olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `mail.forwarders.checking`:
    - EN: "Reading the forwarders of this domain…"
    - TR: "Bu alan adının yönlendiricileri okunuyor…"
  - `mail.forwarders.unknown`:
    - EN: "The forwarders of this domain could not be read from the server, so
      the list is not shown. This does not mean there are none. Nothing was
      changed. Try again."
    - TR: "Bu alan adının yönlendiricileri sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, yönlendirici olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `mail.quota.unknown`:
    - EN: "How much each email account uses could not be read from the server,
      so the usage column says so. The accounts themselves are listed. Try
      again."
    - TR: "Her e-posta hesabının ne kadar yer kullandığı sunucudan okunamadı;
      kullanım sütunu bunu belirtiyor. E-posta hesaplarının kendisi
      listeleniyor. Tekrar deneyin."
  - `mail.setup.checking`:
    - EN: "Reading the connection settings…"
    - TR: "Bağlantı ayarları okunuyor…"
  - `mail.setup.unknown`:
    - EN: "The connection settings of this domain could not be read from the
      server, so they are not shown. Nothing was changed. Try again."
    - TR: "Bu alan adının bağlantı ayarları sunucudan okunamadı; bu yüzden
      gösterilmiyor. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `mail.webmail.unknown`:
    - EN: "CelikPanel could not check whether webmail is available on this
      server. This does not mean it is not. Nothing was changed. Try again."
    - TR: "CelikPanel bu sunucuda web postanın kullanılabilir olup olmadığını
      kontrol edemedi. Bu, kullanılamadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `mail.healthChecking`:
    - EN: "Checking deliverability…"
    - TR: "Teslim edilebilirlik kontrol ediliyor…"
  - `mail.healthUnknown`:
    - EN: "The deliverability checks of this domain could not be read from the
      server, so no result is shown. This does not mean a check failed. Nothing
      was changed. Try again."
    - TR: "Bu alan adının teslim edilebilirlik denetimleri sunucudan okunamadı;
      bu yüzden sonuç gösterilmiyor. Bu, bir denetimin başarısız olduğu anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `mail.rbl.unknown`:
    - EN: "The blocklist check did not complete, so no result is shown. This
      does not mean the address is listed, and it does not mean it is clean. Try
      again."
    - TR: "Kara liste kontrolü tamamlanmadı; bu yüzden sonuç gösterilmiyor. Bu,
      adresin listede olduğu anlamına da temiz olduğu anlamına da gelmez. Tekrar
      deneyin."
- *The catch-all address: shown before it can be changed.*
  - `mail.catchAll.checking`:
    - EN: "Reading the catch-all address of this domain…"
    - TR: "Bu alan adının catch-all adresi okunuyor…"
  - `mail.catchAll.unknown`:
    - EN: "The catch-all address of this domain could not be read from the
      server, so it is not shown and cannot be changed here. This does not mean
      none is set. Nothing was changed. Try again."
    - TR: "Bu alan adının catch-all adresi sunucudan okunamadı; bu yüzden
      gösterilmiyor ve buradan değiştirilemiyor. Bu, bir adres ayarlı olmadığı
      anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `mail.catchAll.none` (known: none set):
    - EN: "No catch-all address is set for this domain."
    - TR: "Bu alan adı için catch-all adresi ayarlı değil."
  - `mail.catchAll.stale` (`409 SETTINGS_CHANGED`):
    - EN: "The catch-all address changed on the server after this page read it,
      so nothing was saved. What you typed is still in the field. Reload the
      current address, then make your change again."
    - TR: "Catch-all adresi, bu sayfa onu okuduktan sonra sunucuda değişti; bu
      yüzden hiçbir şey kaydedilmedi. Yazdığınız adres alanda duruyor. Geçerli
      adresi yeniden yükleyin, sonra değişikliğinizi tekrar yapın."
  - `mail.catchAll.reload` (the action):
    - EN: "Reload the current address"
    - TR: "Geçerli adresi yeniden yükle"
  - `mail.catchAll.notSaved`:
    - EN: "The catch-all address was not saved. Nothing was changed. Try again."
    - TR: "Catch-all adresi kaydedilmedi. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
- *The mail queue.*
  - `postfix.queue.checking`:
    - EN: "Reading the mail queue…"
    - TR: "Mail kuyruğu okunuyor…"
  - `postfix.queue.unknown`:
    - EN: "The mail queue could not be read from Postfix, so it is not shown.
      This does not mean the queue is empty. Nothing was changed. Try again; if
      it keeps failing, check on the server that Postfix is running (sudo
      systemctl status postfix)."
    - TR: "Mail kuyruğu Postfix’ten okunamadı; bu yüzden gösterilmiyor. Bu,
      kuyruğun boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin; sorun sürerse sunucuda Postfix’in çalıştığını denetleyin (sudo
      systemctl status postfix)."
  - `postfix.actionFailed`:
    - EN: "The queue action was not confirmed by the server. The queue is read
      again below."
    - TR: "Kuyruk işlemi sunucu tarafından doğrulanmadı. Kuyruk aşağıda yeniden
      okunuyor."
- *Two refusals that are read by code from any screen* (shell, `err.<CODE>`):
  - `err.MAIL_POLICY_NOT_RELOADED` (`502`, `mutation_applied: true`; drawn above the saved values, with "The reload said:" and its line):
    - EN: "The mail policy was saved to /etc/postfix/main.cf, but Postfix could
      not be reloaded, so Postfix is still running with the previous settings.
      Nothing was rolled back. On the server, run sudo postfix check to see what
      Postfix objects to, correct it, then run sudo systemctl reload postfix.
      The values shown below are the saved ones."
    - TR: "Posta politikası /etc/postfix/main.cf dosyasına kaydedildi ancak
      Postfix yeniden yüklenemedi; bu yüzden Postfix hâlâ önceki ayarlarla
      çalışıyor. Hiçbir şey geri alınmadı. Sunucuda sudo postfix check komutuyla
      Postfix’in neye itiraz ettiğini görün, düzeltin, sonra sudo systemctl
      reload postfix komutunu çalıştırın. Aşağıda gösterilen değerler kaydedilen
      değerlerdir."
  - `err.CRON_JOB_AMBIGUOUS` (`409`):
    - EN: "This task stands twice in the crontab, so CelikPanel cannot tell
      which line to change and changed nothing. Remove one of the two lines on
      the server (sudo crontab -u <site user> -e), then reload this list."
    - TR: "Bu görev crontab’da iki kez duruyor; bu yüzden CelikPanel hangi
      satırı değiştireceğini bilemedi ve hiçbir şeyi değiştirmedi. Sunucuda iki
      satırdan birini kaldırın (sudo crontab -u <site kullanıcısı> -e), sonra bu
      listeyi yeniden yükleyin."

The API answers carry an English sentence for the same states (Panel:
`config_rpc_error.go`, `mail_policy_handlers.go`, `email_handlers.go`,
`domain_cron_errors.go`); the line a service said travels beside it in
`vars.detail`, bounded, with the validation copy's name replaced by the file's
and anything that looks like a password assignment blanked.

**What changed for the person at the screen.**

- *PostgreSQL and MariaDB pages.* While the list of the component's files is
  read: one quiet line. When it could not be read: the notice and Retry; "…
  not found." only for a scan that was read and names no such file. The editor
  exists only for a file that was read; it shows each setting with whether it is
  set or commented out, marks what was changed, and Save is on only for a known,
  changed, not stale file. Rules of `pg_hba.conf` the grid cannot hold (options,
  quoted names, a netmask, includes) are shown as written.
- *A domain's mail.* The number beside "Accounts" and "Forwarding" is a count
  once the list is known, "…" while it is read and "–" when it could not be
  read. "Create address" needs the list. Usage that could not be read says so in
  its column, and the mailboxes stay listed. The webmail card says "not
  available on this server" only for an answer the server gave.
- *Catch-all.* The current address is read and shown first; the field and both
  buttons are off until then. "Disable" is offered whenever an address is set.
- *Mail queue.* The counts and "The mail queue is empty" exist only for a queue
  that was read. Flush and delete are off until then and their answer is read:
  an action the server did not confirm is not announced as done.
- *Scheduled tasks.* A disabled task can be enabled, changed and deleted (the
  server refused all three before). No text changed.

**Not done.** A could-not-read notice does not say why the read failed. The
tabs of the mail manager wrap on a phone instead of scrolling. On a phone the
mailbox and queue tables scroll sideways. A description the Panel wrote above a
scheduled task stays after the task is deleted. Not migrated: the other 47
files of the allow-list (56 before the two parts of this batch stood in one
tree), the mail authentication panel among them.

**In one tree with the first part of this batch (same date).** The two parts
were written side by side and each read the stored component records its own
way. Now there is one decoder and one request (see "One reader per address"
above), and that changes three things on the PostgreSQL and MariaDB pages.

1. *A scan the decoder refuses is "could not be read".* The looser reader of
   this entry took any answer with a list of services for a scan, so a server
   that was never scanned, or an answer without the catalogue fields, read as
   "postgresql.conf not found". Both are now `dbconf.files.unknown` with Retry;
   "not found" needs a whole scan that names no such file.
2. *One read is announced once.* The overview and the log under the editor are
   drawn from the same read as the file list. They come with its answer; while
   it is on its way or has failed, the file card says so, with the one Retry.
   Before this was corrected a failed read showed two notices and two Retry
   controls, and a second request made by the late section could fail and take
   away an editor the first answer had just shown.
3. *Opened by its address, the page starts with the records.* The lookup above
   a component's page (first part) reads them before the page is drawn, so a
   slow or failed read there is the lookup's "reading" or "could not be read";
   the file card's own two states are seen when the page is opened from the
   Components list. The browser scenario `dbconfig` reaches the page that way.

The header buttons of a component page no longer run past a 390 px screen in
Turkish: the shell of the first part wraps them onto a second row.

**Browser inspection (2026-10-09).** In a real, installed Chrome against the
loopback mock (`web/tools/browser-inspect`, scenarios `dbconfig`, `mailscreens`,
`mailqueue`, `cron`): desktop 1440×900 and phone 390×844, Turkish and English,
light and dark; 45 states in each of the eight configurations.

- *Measured in all eight.* In every state where a read was on its way or had
  failed, none of the negative sentences was on screen ("not found.", "holds no
  access rule", "No email accounts yet", "is not available on this server", "The
  mail queue is empty", "No catch-all address is set" and their Turkish forms),
  and no editor field and no Save existed for a configuration file that was not
  known. Every failed read showed exactly its own notice with Retry. No state
  had horizontal page overflow. After a refused save the typed value was still
  in its field and the service's line stood next to it (`aria-invalid` with
  `aria-describedby`). After a 409 the fields and Save were off and the reload
  was the one action. The disabled scheduled task became enabled with one PUT
  and one re-read.
- *Found by looking and corrected.* The four tabs of the mail manager ran off a
  phone screen, so "Settings" (where the catch-all is) could not be reached, and
  scrolling a field into view moved the whole page sideways: they wrap now. On a
  phone the remove control of an access rule stood on a line of its own under
  each rule and the save row took three lines: the rule's number and its remove
  control share a line, and the state line stands above two shorter actions. The
  per-row column names of the access rules repeated on a wide screen: the first
  row names the columns there. The service's line next to a field was a second
  alert for the same refusal: the notice above the form is the one announcement.
  Two icon controls (edit quota, copy) were 22 px: 26 px now.
- *Seen and not changed here.* The component page's back link (another screen's
  file; its header buttons wrap since the first part of this batch). A disabled
  primary button is close to an enabled
  one in the dark theme (shared button). The editor card grows from one line to
  the whole editor when the file arrives; what stands under it moves.
- *Not covered.* Any real server; Safari, Firefox, a screen reader, a touch
  device; the imitation skins; roles other than the administrator; the raw file
  editor's refusal states (mounted test only).
