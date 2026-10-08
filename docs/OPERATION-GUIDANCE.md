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

Source state with unit, contract and mounted-component tests; no native run and
no browser pass. Found by an owner on an installed Ubuntu server running
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
  (review page and progress page, quiet guidance surface, no alarm styling):
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
  - `setup.handover.address` — EN "After that step, open the Panel at its
    secure address:" · TR "O adımdan sonra paneli güvenli adresinden açın:",
    followed by `https://{host}:<port>` as a link to the wizard there.
  - `setup.handover.addressHelp` — EN "You sign in again there, and setup shows
    the same progress. If the browser warns about the certificate at that
    address, the step has not finished yet; return to this page and wait." ·
    TR "Orada yeniden giriş yaparsınız; kurulum aynı ilerlemeyi gösterir.
    Tarayıcı o adreste sertifika uyarısı verirse adım henüz bitmemiştir; bu
    sayfaya dönüp bekleyin."
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
  the typed reason (`error.reason`), and the headline is selected from it; the
  Panel's sentence stays the fallback for older records. The Agent now names the
  case "another Agent job owns the lease" (`agent_mutation_active`) — the Panel's
  own short work after a start, a certificate activation or a renewal run as
  such jobs — so the owner reads the existing `setup.blocker.changeBusy` text
  ("Setup stopped because another CelikPanel change is still running on this
  server. Wait for it to finish, then choose Review a revised plan and start
  setup again. Steps that already finished are kept." / "Kurulum durdu, çünkü bu
  sunucuda başka bir CelikPanel değişikliği hâlâ sürüyor. Tamamlanmasını
  bekleyin, ardından Düzeltilmiş planı incele’yi seçip kurulumu yeniden
  başlatın. Tamamlanan adımlar korunur.") instead of the generic one.

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
- Not verified on a real server or in a browser: the texts in place, the timing
  of the restart against the step list, and the full page's ten-second recheck.

### Access and readiness checks keep the page: explained hold, checking state, ended session (2026-10-08)

Source state with component tests; no browser pass and no native run. Found by an
owner on an installed server running v0.1.0-alpha.81. Affected contract items:
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
  CelikPanel checks again by itself every few seconds. When access is confirmed,
  this page continues where it was, with what you typed. Until then nothing on
  this page can be changed." · TR "Şimdilik bir şey yapmanız gerekmiyor.
  CelikPanel birkaç saniyede bir kendiliğinden yeniden kontrol eder. Erişim
  doğrulandığında bu sayfa, yazdıklarınızla birlikte kaldığı yerden devam eder. O
  zamana kadar bu sayfada değişiklik yapılamaz." Action: the existing
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
  edilemedi") needs a read that failed.
- *The session ended under a page in use* (confirmed 401), above the sign-in
  form: `accessHold.sessionEnded`, EN "Your session ended. Sign in to return to
  the page you were on. Anything you had typed there and not saved was not
  kept." · TR "Oturumunuz sona erdi. Bulunduğunuz sayfaya dönmek için giriş
  yapın. Orada yazıp kaydetmediğiniz bilgiler korunmadı." Who acts: the owner.
  Resume: the same address after sign-in. A sign-out shows no reason.
- *A part of the interface failed to load after an update*, for 4 s before the
  page reloads: `accessHold.updateReload`, EN "A part of CelikPanel could not be
  loaded, most likely because CelikPanel was updated while this tab was open.
  This page reloads in a moment to load the current version." · TR "CelikPanel’in
  bir bölümü yüklenemedi; büyük olasılıkla bu sekme açıkken CelikPanel
  güncellendi. Güncel sürümü yüklemek için bu sayfa birazdan yeniden yüklenir."
  The cause is stated as likely, not as known.

**Known negative results are unchanged:** a license reported missing, expired or
invalid still shows the activation page (administrator) or the message to contact
the administrator (other roles).

**Not handled.**

- If the wording part has not arrived, the layer says only "Checking panel
  access" with the shell's help line and the check; the ended-session reason and
  the reload line are then not shown.
- The reload after a failed part is not offered as a choice; it is announced.
- A page whose own request is refused under the layer handles that itself; not
  audited page by page.
- Unsent input is not kept across a real sign-in.
- Not verified in a browser or on a real server: the texts in place, the 1.5 s
  and 30 s steps, focus return, and a hidden tab's timers.

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
