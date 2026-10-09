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
  is exactly what an idempotency key exists for. Since 2026-10-10 the apply
  carries a request identity (D-029; the entry of that date below).
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

#### Third batch: service pages, the dashboard's attention list, the DNS settings and the DNS engine's stalled change (2026-10-09)

Source state with component tests and a browser inspection against a loopback
mock; no native run and no installed server. The same rule, applied to a smaller
set than planned: this batch was cut down so that what it changes could be
verified, and the rest of the allow-list is untouched (see "Not done"). It
changes how reads are shown in the interface and who sends one request of the
DNS engine card; no API, stored record, access gate or lifecycle changes, and no
acceptance item is closed.

**What these screens showed before.**

- *Fail2ban.* A read that failed or had not answered was an empty list: "No
  jails active" and "No banned IPs" on a server that had both, with 0 beside the
  two tabs, and no settings tab content at all.
- *Nginx.* Every value was "—" and the rate limits were "No rate-limit zones
  defined" while the three reads were on their way and after they failed.
- *PHP.* "No extensions found" for the same two cases; a switch was changed on
  screen before the server had answered and put back if it refused. The php.ini
  editor reported a failed read with a browser alert and then drew nothing.
- *Dovecot.* Both figures were "—" without a word.
- *PowerDNS.* The page read the component records on its own; when that read
  failed it showed no file list and said nothing.
- *DNS settings.* A failed first read was a red banner with no way to read again.
- *Dashboard.* Whatever `GET /api/v1/firewall` answered was stored as the
  firewall's state. An answer without `enabled` — the Agent's own error sent with
  a 200 — drew "Firewall is off — all ports are open" with "Turn on". The "Needs
  attention" section appeared when the slowest read answered and pushed the page
  down by 118 px (158 px on a phone).

**The texts.** Keys are in `web/src/i18n/screens/server` unless noted.

- *Fail2ban, the jails* — checking (`f2b.jails.checking`): EN "Reading Fail2ban’s
  jails…" · TR "Fail2ban’in hapishaneleri okunuyor…". Could not read
  (`f2b.jails.unknown`):
  - EN: "Fail2ban’s jails could not be read from the server, so they are not
    listed. This does not mean there are none. Nothing was changed. Try again."
  - TR: "Fail2ban’in hapishaneleri sunucudan okunamadı; bu yüzden listelenmiyor. Bu,
    hapishane olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Fail2ban, the banned addresses* — checking (`f2b.banned.checking`): EN
  "Reading the addresses Fail2ban has banned…" · TR "Fail2ban’in yasakladığı
  adresler okunuyor…". Could not read (`f2b.banned.unknown`):
  - EN: "The addresses Fail2ban has banned could not be read from the server, so
    they are not listed. This does not mean none is banned. Nothing was changed.
    Try again."
  - TR: "Fail2ban’in yasakladığı adresler sunucudan okunamadı; bu yüzden
    listelenmiyor. Bu, yasaklı adres olmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
  - "Unban" exists only for a row the server listed. After it, both lists are
    read again whatever the answer was; if that read fails the earlier list stays
    under the shared notice (`common.staleNotice`) and "Unban" is off.
- *Fail2ban, the settings* — checking (`f2b.config.checking`): EN "Reading
  Fail2ban’s settings…" · TR "Fail2ban’in ayarları okunuyor…". Could not read
  (`f2b.config.unknown`): EN "Fail2ban’s settings could not be read from the
  server, so they are not shown. Nothing was changed. Try again." · TR
  "Fail2ban’in ayarları sunucudan okunamadı; bu yüzden gösterilmiyor. Hiçbir şey
  değiştirilmedi. Tekrar deneyin."
- The count beside each Fail2ban tab is the number once the list is known, "…"
  while it is read and "–" when it could not be read.
- *Nginx* — checking (`nginx.global.checking`, `nginx.ssl.checking`,
  `nginx.rate.checking`): EN "Reading Nginx’s global settings…", "Reading Nginx’s
  TLS settings…", "Reading Nginx’s rate-limit zones…" · TR "Nginx’in genel
  ayarları okunuyor…", "Nginx’in TLS ayarları okunuyor…", "Nginx’in hız sınırı
  bölgeleri okunuyor…". Could not read (`nginx.global.unknown`,
  `nginx.ssl.unknown`, `nginx.rate.unknown`):
  - EN: "Nginx’s global settings could not be read from the server, so they are
    not shown. This does not mean they are not set. Nothing was changed. Try
    again." / "Nginx’s TLS settings could not be read from the server, so they
    are not shown. This does not mean they are not set. Nothing was changed. Try
    again." / "Nginx’s rate-limit zones could not be read from the server, so
    they are not listed. This does not mean none is defined. Nothing was
    changed. Try again."
  - TR: "Nginx’in genel ayarları sunucudan okunamadı; bu yüzden gösterilmiyor.
    Bu, ayarlanmadıkları anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
    deneyin." / "Nginx’in TLS ayarları sunucudan okunamadı; bu yüzden
    gösterilmiyor. Bu, ayarlanmadıkları anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin." / "Nginx’in hız sınırı bölgeleri sunucudan
    okunamadı; bu yüzden listelenmiyor. Bu, tanımlı bölge olmadığı anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - "—" is drawn only for a value a known answer leaves empty. The first column
    of the rate-limit table had the untranslated heading "Name"; it is
    `nginx.rl.name` (EN "Name" · TR "Ad").
- *PHP, the extensions of one version* — checking (`php.extensions.checking`): EN
  "Reading the extensions of PHP {version}…" · TR "PHP {version} eklentileri
  okunuyor…". Could not read (`php.extensions.unknown`):
  - EN: "The extensions of PHP {version} could not be read from the server, so
    they are not listed and cannot be switched here yet. This does not mean there
    are none. Nothing was changed. Try again."
  - TR: "PHP {version} eklentileri sunucudan okunamadı; bu yüzden listelenmiyor
    ve şimdilik buradan açılıp kapatılamıyor. Bu, eklenti olmadığı anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - A switch exists only for an extension the server listed. Pressing one sends
    the change once; the list is then read again and the switch shows what the
    server says, accepted or refused. The switches are off while that happens
    and while the list is an earlier answer.
- *PHP, php.ini of one version* — checking (`php.ini.checking`): EN "Reading
  php.ini of PHP {version}…" · TR "PHP {version} için php.ini okunuyor…". Could
  not read (`php.ini.unknown`): EN "php.ini of PHP {version} could not be read
  from the server, so its settings are not shown and cannot be changed here yet.
  Nothing was changed. Try again." · TR "PHP {version} için php.ini sunucudan
  okunamadı; bu yüzden ayarları gösterilmiyor ve şimdilik buradan
  değiştirilemiyor. Hiçbir şey değiştirilmedi. Tekrar deneyin." The form is built
  only from an answer; nothing read for another version stays in it.
- *Dovecot, uptime and connections*: "…" while they are read, "–" when they
  could not be read, and under the cards (`dovecot.statsUnknown`):
  - EN: "Dovecot’s uptime and connection count could not be read from the server,
    so they are not shown. This does not mean Dovecot is stopped or has no
    connections. Nothing was changed. Try again."
  - TR: "Dovecot’un çalışma süresi ve bağlantı sayısı sunucudan okunamadı; bu
    yüzden gösterilmiyor. Bu, Dovecot’un durduğu ya da bağlantı olmadığı anlamına
    gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *PowerDNS, its configuration files*: the same shared read and the same two
  sentences as the PostgreSQL and MariaDB pages (`dbconf.files.checking`,
  `dbconf.files.unknown`, with "PowerDNS").
- *DNS settings (Settings → DNS)* — checking (`dnssrv.checking`): EN "Reading
  this server’s DNS settings…" · TR "Bu sunucunun DNS ayarları okunuyor…". Could
  not read (`dnssrv.unknown`), with Retry; a refusal the server explained is
  shown under it:
  - EN: "The DNS settings of this server could not be read, so they are not shown
    and cannot be changed here yet. This does not mean DNS is not set up. Nothing
    was changed. Try again."
  - TR: "Bu sunucunun DNS ayarları okunamadı; bu yüzden gösterilmiyor ve şimdilik
    buradan değiştirilemiyor. Bu, DNS’in kurulmadığı anlamına gelmez. Hiçbir şey
    değiştirilmedi. Tekrar deneyin."
- *Dashboard, "Needs attention"* (keys in `web/src/i18n/screens`). The section is
  on the page from the first paint. An item is listed only from an answer the
  server gave. The list is built from four reads (expiring certificates, the
  domain list, the firewall, the component records):
  - while one is on its way (`dashboard.attentionChecking`): EN "Checking what
    needs attention on this server…" · TR "Bu sunucuda ilgi isteyen bir şey olup
    olmadığı kontrol ediliyor…";
  - when one could not be read (`dashboard.attentionUnread`), with Retry, which
    reads only the ones that failed: EN "Part of this server’s state could not be
    read, so this list may be incomplete. This does not mean something is wrong.
    Nothing was changed. Try again." · TR "Bu sunucunun durumunun bir bölümü
    okunamadı; bu yüzden bu liste eksik olabilir. Bu, bir şeyin yanlış olduğu
    anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin.";
  - when all four answered and nothing is listed (`dashboard.attentionNone`): EN
    "Nothing CelikPanel read needs action: certificates, the firewall and the
    installed components." · TR "CelikPanel’in okuduklarında işlem gerektiren bir
    şey yok: sertifikalar, güvenlik duvarı ve kurulu bileşenler.";
  - the same, when the component records are not current
    (`dashboard.attentionNoneUnchecked`): EN "No certificate or firewall setting
    needs action. The installed components were not checked recently." · TR
    "İşlem gerektiren bir sertifika ya da güvenlik duvarı ayarı yok. Kurulu
    bileşenlere yakın zamanda bakılmadı." What to do about it is in the "System
    services" card above the list, which says in that case that the component
    status is outdated and points to the Components page.
  - This is not the "all good" line that was removed earlier: it names what was
    read and does not speak for components nobody has checked lately.
  - The checking line is drawn only while nothing is listed. When an item is
    already listed and another read is still on its way, that is said beside
    the count (a small spinner whose label is the checking sentence), so no row
    appears in the list and then leaves it.
  - "Firewall is off" and "Turn on" exist only for an answer with `enabled:
    false`. An answer without a boolean `enabled`, or one that carries the
    Agent's `error`, is "could not be read".

**The DNS engine card: polling only reads (decision).** While a DNS engine change
is tracked, the card reads `GET /api/v1/dns/engine` every 3 s (15 s once the
change has recorded nothing new for two minutes). Until this entry the same
timer also sent `POST /api/v1/dns/engine/reconcile` when the exact operation's
`updated_at` was two minutes old, at most three times and a minute apart. That
request is still needed — it is what closes the saved record of an accepted
change whose worker is gone — but it is not a read: the server takes its change
lock, may rewrite the saved record, and writes `reconciled_operation` to the
audit log under the signed-in administrator. A timer sent it in a tab nobody may
have been looking at, while the screen said "Read-only checks continue every 15
seconds".

Two ways were considered: keep one automatic attempt and record it here as an
exception to "Polling and page refresh are reads" (above), or give the request to
the person at the screen. The second was chosen, because the request is attributed
to a person in the audit log and so should be that person's act, because an
exception to the polling rule would have to be remembered by every later reader
of this document, and because nothing is lost: the lock already told the owner to
stay on the page, and the change itself is never started again either way.

- The timer sends the request zero times. It can come from a button only:
  `Refresh state` on the card (off while a change is tracked, as before) and
  "Check now", which is in the lock while the change is stalled and on the card
  once the polling has stopped at its safety limit.
- "Check now" is offered in the lock only when the exact operation (same request
  and target) has recorded nothing new for two minutes; one request at a time;
  its answer is applied only to the same request and target, through the same
  check for a finished change as the polling.
- *Past the safety limit.* The polling stops after 31 minutes or 180 reads
  without a final result, and the lock is released (both as before). Until this
  entry the timer had by then sent its own requests; with none sent, and
  `Refresh state` off while the change is unresolved, nothing in the interface
  could have closed the saved record any more. So for an exact operation the
  card keeps offering the same "Check now" in that state, in a notice under the
  operation's progress. It replaces the sentence "Automatic tracking continues
  every 15 seconds", which is not true there. Reloading the page reads the
  state once and returns to this notice.
- Texts (`web/src/i18n/dnsEngine.ts`). First sentence (`dnsEngine.guard.reconcileDue`):
  EN "CelikPanel has not confirmed this change for {minutes} minutes: the server
  has recorded no new step in that time." · TR "CelikPanel bu değişikliği
  {minutes} dakikadır doğrulayamadı: sunucu bu sürede yeni bir adım kaydetmedi."
  Then (`dnsEngine.guard.reconcileOffer`):
  - EN: "CelikPanel keeps reading the state every 15 seconds and changes nothing
    by itself. As this server’s administrator you can choose Check now:
    CelikPanel then compares its saved record of this change with what the DNS
    service did, and closes the record if the change has ended. That is logged
    under your account and does not start the change again."
  - TR: "CelikPanel durumu 15 saniyede bir okumayı sürdürüyor ve kendiliğinden
    hiçbir şeyi değiştirmiyor. Bu sunucunun yöneticisi olarak Şimdi kontrol et’i
    seçebilirsiniz: CelikPanel o zaman bu değişikliğin kayıtlı kaydını DNS
    hizmetinin gerçekten yaptığıyla karşılaştırır ve değişiklik bittiyse kaydı
    kapatır. Bu işlem hesabınız adına denetim günlüğüne yazılır ve değişikliği
    yeniden başlatmaz."
  - Button (`dnsEngine.guard.checkNow`): EN "Check now" · TR "Şimdi kontrol et".
  - After a check that did not finish the change, the first sentence is replaced
    by (`dnsEngine.guard.reconcileChecked`) EN "Checked at {time}: the change has
    not finished yet." · TR "Saat {time} itibarıyla kontrol edildi: değişiklik
    henüz bitmedi."; by the server's own refusal when it refused; or by
    (`dnsEngine.guard.reconcileUnread`) EN "Checked at {time}, but the DNS state
    could not be read afterwards." · TR "Saat {time} itibarıyla kontrol edildi,
    ancak ardından DNS durumu okunamadı."
  - Past the safety limit, on the card: first sentence
    (`dnsEngine.guard.deadlineDue`) EN "CelikPanel has stopped checking this
    change by itself: it reached its safety limit without a final result." · TR
    "CelikPanel bu değişikliği kendiliğinden kontrol etmeyi bıraktı: kesin bir
    sonuç olmadan güvenlik sınırına ulaştı." Then
    (`dnsEngine.guard.deadlineOffer`), with the same button:
    - EN: "Nothing is changed by itself. As this server’s administrator you can
      choose Check now: CelikPanel then compares its saved record of this change
      with what the DNS service did, and closes the record if the change has
      ended. That is logged under your account and does not start the change
      again."
    - TR: "Kendiliğinden hiçbir şey değiştirilmez. Bu sunucunun yöneticisi olarak
      Şimdi kontrol et’i seçebilirsiniz: CelikPanel o zaman bu değişikliğin
      kayıtlı kaydını DNS hizmetinin gerçekten yaptığıyla karşılaştırır ve
      değişiklik bittiyse kaydı kapatır. Bu işlem hesabınız adına denetim
      günlüğüne yazılır ve değişikliği yeniden başlatmaz."
    - After a check there, the first sentence is replaced as in the lock
      (`reconcileChecked`, the server's refusal, or `reconcileUnread`).
  - `dnsEngine.guard.stalled` ("No new durable server phase has been recorded for
    two minutes…") is removed; the state without an exact operation
    (`dnsEngine.guard.awaitingStalled`) is unchanged and offers nothing, in the
    lock and past the safety limit.

**Four corrections in shared parts.**

- *A component operation that ends refreshes what is shown.* Screens that show
  the stored component records through the shared read keep an answer for half a
  minute, so a PostgreSQL or MariaDB page opened right after its install could
  list the earlier scan for up to 30 s. The tracker now tells that read to read
  again once per operation, at the point where it has verified the end with a
  fresh scan (success or failure), never from a poll; with no such screen open
  nothing is requested (`refreshRemote` in `web/src/lib/remote.ts`).
- *The dashboard's attention list keeps its place* (above): with none or one
  item nothing under it moves; each further item adds its own row. The box
  reserves the height of the tallest thing one answer can put in it: one line
  on a wide screen, three lines below the `lg` width, where the calm line wraps
  to three lines and one item to two; what it holds is centred in it. The two
  calm lines are of one length for that reason.
- *A disabled button no longer looks like a call to action.* It had a recessed
  fill, which in the dark theme was a navy block with a light label beside a
  light enabled button. A control that cannot be used has no fill and a dashed
  outline in the colour of its label, in both themes; a button that is working
  keeps the recessed fill and its spinner.
- *The card of a configuration editor keeps its height.* It was one line while
  the file was read and then the whole editor, pushing the raw files, the
  overview and the log down. The card takes the rest of the window in every
  state, so what stands under it starts below the fold before and after; the
  raw file editor reserves the height of its text area; on the MariaDB page the
  file chooser's place is kept while the files are read.

Also: the lock of a tracked operation can be taller than a phone's window (three
details, a message and an action). It scrolled nowhere and was cut at the top
and the bottom; it scrolls inside the window now.

**How it is kept from coming back.** `web/tests/remote-state-mounted-batch3.test.mjs`
mounts each of these screens with each read withheld four ways and requires that
no negative text is drawn, no changing control is enabled, and Retry sends only
reads; it also holds the single refresh of the shared read. The same file mounts
the dashboard with each of the four reads of the attention list withheld the
same four ways, and with the firewall answering the Agent's error with a 200:
no "Firewall is off", no "Turn on", no calm line, no request other than a read,
and a Retry that reads only what could not be read; it holds that a listed item
with another read on its way draws no checking row, and that the firewall's
decoder accepts "off" only as a boolean the Agent sent. It also mounts the real
DNS engine card over a change that has recorded nothing for four minutes: the
poll sends no request other than reads (the card as it was before this entry
fails here with the reconcile request among them), "Check now" pressed twice
sends one, a refusal is said in the lock, and past the safety limit the lock is
released, the loop reads no more and the card's notice sends the one request.
`component-operation-setup-scan-runtime` holds "once at a verified end, never at
an unverified one". `external-operation-lock-contract` pinned the bounds of the
automatic reconcile request; it now pins that the polling loop contains no such
request and no `fetch`, that the request has exactly two call sites
(`Refresh state` and the owner's check), that the owner's check is reachable
from two buttons only (the lock, and the card past the safety limit), that it
stays offered at the safety limit for an exact operation, and that it is
single-flight, tied to the exact operation and unable to reach the switch
request. `dashboard-truth-contract` holds the calm line's conditions and that no
answer is stored as the firewall's state without being decoded.
`button-disabled-contrast-contract` measures the disabled label on every surface
a button stands on, in every palette (before: on one), and the enabled primary
fill against the same surfaces: at least 3:1 in the product's light and dark
themes (lowest measured 7.8:1) and no lower than today's 2.87:1 in the imitation
skins, where the dashed outline carries the difference. The ratchet: 20 / 36 /
10 / 71 / 23 in 47 files before this batch, 8 / 28 / 10 / 57 / 19 in 40 files
after it.

**Not done.**

- 40 files still read the old way. Not reached in this batch, and
  untouched: the components list and the operation tracker's own reads
  (`ServiceList`, `ComponentOperation`), `ServiceShell`, the rest of the
  dashboard (system figures, the audit trail, the component summary's own read,
  the readiness poll), every domain panel on the list (`HostingTypePanel`,
  `DomainDNSManager`, backups, apps, logs, PHP, general, certificate overview,
  mail authentication), the two database dialogs, the server setup wizard, the
  store and add-ons pages, VPN, the audit log, the team page, the security audit
  card and the panel database page.
- Left on purpose, with the contract that pins them: `Layout` (the one bounded
  `fetch('/api/v1/panel/version'` and its admin-only reset in
  `layout-server-identity-contract`; `api.getServices().then(publishComponentCensus)`
  in `component-inventory-contract`), `SystemUpdateOperation` (the literal exact
  status and recovery reads in `system-update-outcome` and
  `panel-update-ui-contract`; fail-closed on purpose), `PanelUpdateCard`
  (`panel-update-card-mounted`), `DNSEngineCard`'s own status read
  (`dns-engine-ui-contract`), the dashboard's readiness read
  (`dashboard-firewall-confirmation-contract`). The access gates
  (`usePanelSession`, `LicenseOnboarding`, `RecoveryAccess`, `App`) already keep
  unknown apart from negative; only their raw reads are counted, and they were
  not moved.
- The dashboard stays on the list (1 / 3 / 0 / 4): its audit trail, system
  figures, readiness poll and its own read of the component records are not on
  the shared layer. The firewall panel of the Components page (`ServiceList`)
  still reads `GET /api/v1/firewall` on its own.
- The DNS engine card's `dnsEngine.guard.deadline` sentence is set on the lock
  at the safety limit, where the lock is released, so nobody sees it; that was
  so before this entry and is unchanged. What the owner sees there now is the
  card's notice with "Check now" (above), only for an exact operation.
- The mounted test of the DNS engine card covers the card's first poll (half a
  second after the state is read, where the old timer sent its request) and the
  owner's check; the later, slower polls are held by the source contract and
  were counted in the browser run.
- The php.ini editor is still English only and still confirms and reports a save
  with browser dialogs; only its read was changed.
- The attention list still moves when a second item arrives (each further item
  adds its own row), when a read fails (the notice is taller than one line),
  and on a screen narrower than 390 px if a calm line needs a fourth line.
- A could-not-check notice still does not say why the read failed.
- Not verified on a real server; one Chrome against a mock.

**Browser inspection of this batch (2026-10-09).** In a real, installed Chrome
against the loopback mock (`web/tools/browser-inspect`, scenarios `servicepages`,
`attention`, `dnssettings`, `dnsreconcile`, `editorheight`; every scenario of the
earlier batches was run again on the same build): desktop 1440×900 and phone
390×844, Turkish and English, light and dark.

- *Measured in all eight configurations.* No negative sentence was on screen in
  any checking or could-not-check state of these screens. Under the dashboard's
  attention list nothing moved between the checking state and the settled page
  (0 px): with nothing to list while the components are current, with nothing
  to list while they are not, with one item, and with an item already listed
  while another read was on its way. With the DNS change stalled, no reconcile
  request arrived in 34 s with nobody touching the page (two slow polls); each
  "Check now" sent exactly one. Past the safety limit the lock was not drawn,
  no request of any kind arrived in 20 s, the card did not say that tracking
  continues, and each "Check now" on the card sent exactly one. The raw files
  under the configuration editor's card started below the fold while the file
  was read and after it. Under the Dovecot figures nothing moved when the
  answer arrived. On the Fail2ban, Nginx and PHP pages nothing stands under the
  tab content, so there was nothing whose place could be measured.
- *Found by looking, or by measuring, and corrected.*
  - On a phone the attention list did push the page down when it arrived: by
    24 px with nothing to list and by 12 px with one item, because one line was
    reserved and the calm line takes three lines there and an item two. The
    box now reserves three lines below the `lg` width, and the calm line for
    components that are not current, which was about twice as long, was
    shortened to the length of the other.
  - On the dashboard, in the DNS settings and in the php.ini tab the
    could-not-check notice stood inside the section's own box, a box in a box;
    it stands by itself in all three.
  - The lock of the DNS change drew the component tracker's "connection
    interrupted" icon and "Reload page". That was the mock: it answered the
    tracker's read of the active component operation with a bare `null`, where
    the Panel always answers `{"operation": null}`
    (`cmd/panel/service_operations.go`). The mock answers the envelope now, and
    the lock shows the attention mark and no reload button.
  - After a check the lock read "…the change has not finished yet. It keeps
    reading the state…"; the second sentence names CelikPanel now.
  - Past the safety limit the card showed "Updating automatically" beside the
    notice that says it has stopped; the badge is not drawn there.
  - The lock of the DNS change could be taller than the phone's window and was
    cut at the top and the bottom; it scrolls inside the window.
  - The new Turkish sentences said "hapis" where the page says "hapishane".
  - Two figures of the scenarios were not measurements: "moved 0" on the
    Fail2ban, Nginx and PHP pages was the place of the page itself, and the
    Dovecot measurement found no element in English. The scenarios record a
    figure only for an element that was found, and fail otherwise.
- *Seen and not changed.* On a phone the Fail2ban tables scroll sideways, so
  "Unban" is off screen until the table is scrolled, as on the Domains and
  Databases tables. The lock's message is long and is set whole in the
  attention colour, the overlay's existing style. "Recent activity" on the
  dashboard shows "–" for an empty trail (not migrated). After a ban is lifted
  and the list cannot be read again, the toast says "IP unbanned" while the
  shared notice over the earlier list says "Nothing was changed": that sentence
  speaks of the read, as it does on the Databases page after a delete.
- *Not covered.* Any real server; Safari, Firefox, a screen reader, a touch
  device; the imitation skins; roles other than the administrator. The PowerDNS
  page's file list while it is read or could not be read, and the php.ini
  editor's save, are covered by the mounted test only. The "before" figures of
  this entry (118 px, 158 px on a phone) were not measured again.

#### Fourth batch: the panels of one domain, a change whose answer did not arrive, row actions on a phone (2026-10-09)

Source state with component tests and a browser inspection against a loopback
mock; no native run and no installed server. The same rule, applied to the
panels of one domain's page. It changes how reads and lost answers are shown in
the interface; no API, stored record, access gate or lifecycle changes, and no
acceptance item is closed. What needs the server is listed under "Needs the
server" below.

**What these panels showed before.**

- *DNS records.* A zone whose state could not be read was an empty state titled
  "DNS zone status could not be checked". A failed read of the records raised a
  toast and a red line, and left "No records yet" and "0 items total" under it.
  The DNSSEC card was absent until its read answered and then pushed the records
  down; an answer without `secured` was read as "not signed", with "Sign the
  zone" offered.
- *Hosting type.* A failed read left a spinner without end. The Node.js
  versions were an empty list when their read failed. The live application
  panel was drawn for the type picked in the form, not the saved one, so a type
  that was only picked polled an application that does not exist (409 every
  five seconds). Its two reads were swallowed: "Stopped" and "No log lines yet"
  for no answer, with Start offered.
- *PHP.* "Could not load PHP settings" in red, with no way to read again. The
  pool form filled every missing value with a default (`dynamic`, 5, 2, 1, 3,
  `www-data`) and saved it.
- *General settings.* "Could not load settings" in red, with no Retry.
- *Applications.* "No applications available" for a failed read.
- *Certificate card of the overview.* "Status unavailable … Open SSL/TLS for
  details", with no Retry and no word that this is not "no certificate".
- *Mail authentication.* A spinner without end after a failed read. A record
  state the screen did not know was drawn as "Missing".
- *Logs.* With auto-refresh on, every refused poll raised an error toast, every
  five seconds, and every poll replaced the lines with a page-sized spinner.
  "No log lines" for a failed first read.
- *Backups.* "No backups yet" for a failed read, or for an answer with an empty
  body. "No linked databases" and "Website files + 0 databases" when the
  databases could not be read.
- *Scheduled tasks.* An answer that did not carry the list was "No scheduled
  tasks".
- *Any change on these panels.* When the connection dropped, a generic error
  toast, and the control was enabled again at once.

**What they do now, beyond the three states.**

- *A change whose answer did not arrive* (`web/src/lib/lostAnswer.ts`,
  `ResultUnknown` in `web/src/components/ui.tsx`). The panel's changes on these
  screens carry no identity the server keeps, so a second request is a second
  change. When no answer arrives (the connection ends, a gateway answers 408,
  502, 503 or 504 with a page that is not the Panel's JSON, or an accepted
  answer cannot be read), the screen:
  1. sends nothing a second time;
  2. reads again what the change acts on, and only reads;
  3. keeps every control that changes or removes **off until that read has
     answered** (`holding`);
  4. shows one notice where the change was asked for, scrolled into view, in the
     attention colour (nothing is known to have failed). While the read is on
     its way it says so; when the read has answered it says at what time the
     state was read again, and it **stays until the person closes it** or a
     later change is answered. "Check again" only reads. If the read fails too,
     the notice says so and the controls stay off.
  A refusal the Panel itself sent, with any status, is the server's reason and
  is shown as before. This is not idempotency: whether the change was made is
  decided by the person looking at the state that was read again. Where that
  state cannot show the result (installing an application), the notice says
  where to look.
- *What a form does once the state was read again* (2026-10-10; `Question` in
  `web/src/lib/lostAnswer.ts`). A form whose answer was lost asks the state
  that was read again one question, and only looks: does it show the change?
  - It does: the form that sent the change is closed or emptied, so the same
    record is not one press from being saved twice. The notice says the change
    was saved, on the plain surface with a check mark instead of the attention
    one, offers only "Close", and stays until the person closes it.
  - It does not: what was typed stays (where a newer answer builds the form
    again, the sent values are put back over it), the changing controls come
    back, and the notice says that the state does not show the change, that it
    is therefore not known to have been saved, and that a server still working
    when the connection dropped can finish later. "Check again" reads and asks
    the question again; a later read that shows the change closes the form.
  - It cannot tell (the list gained a row that is not clearly this record, or
    the settings are neither the sent ones nor the earlier ones): the notice of
    the four steps above, and the person looks.
  - The state could not be read again: the controls stay off, as above.
  The forms that ask: a DNS record being added (a row that is new in the list,
  of the sent type and owner name, whose value is the sent one apart from what
  the server rewrites: the full owner name, quotes, a trailing dot, case); an
  alias being added (the alias in a list that did not have it); the redirect
  switch of the general settings; Apply of the hosting type (the type and its
  own fields as sent: saved; the same settings as before: not shown); the PHP
  version; the PHP pool. The changes that do not ask, because they have no
  typed form or because the state cannot show their result: deleting a record,
  publishing a zone, signing, start, stop and restart, installing an
  application, publishing a mail record, the DKIM key, clearing a log, and
  creating, restoring or deleting a backup. For those the person looks at the
  state that was read again, as before.
- *The certificate line under a domain's name* (2026-10-10, `DomainDetail`). The
  strip under the title reads the certificate itself, at the address and with
  the decoder of the overview card and the SSL/TLS tab, so the three share one
  request. It is being checked, could not be checked (with "Retry" beside the
  words, which only reads), or what the server said. After a refresh that
  failed the earlier answer stays. Before this it waited for the card or the
  tab to report, and said "checking status" without end after a read that
  failed and on every tab that mounts neither.
  A consequence for the SSL/TLS tab, measured in the browser run of 2026-10-10:
  the strip is on screen on every tab, so the answer is never dropped while the
  domain's page is open, and the tab no longer starts from nothing. Opened
  while the page's first read is on its way, it shows its checking line and
  shares that read. Opened later, it shows the answer the page already has as
  the earlier answer, says it is reading again, keeps its controls off, and
  sends one more read; before this change it showed the checking line alone
  and sent the only read. Nothing negative is shown that the server did not
  say, and a certificate request still works after that read. Whether an
  answer a few seconds old should be read again when the tab opens is not
  decided here.
- *A read that repeats* (`useRefreshEvery` in `web/src/lib/remote.ts`; the logs
  keep their own timer). A tick only reads, and not while the last read is still
  on its way. A read that fails raises nothing: the earlier answer stays under
  one notice that says when it was read, the controls that change something are
  off, and the next tick asks again; a good answer removes the notice.
- *DNS.* The server's 404 for this domain's zone is the one answer that says
  "no zone"; every other refusal is "could not check". The records are read only
  for a zone the server said exists. The DNSSEC card is in its place while it
  checks, with the room its usual answer takes, so the records below do not
  move.
- *Hosting type.* The form shows what the server sent; what is typed is kept
  beside the answer it was typed over, and after Apply the form shows the saved
  settings again. The live application panel follows the saved type.
- *PHP.* The pool form holds the server's values and no defaults; a newer answer
  builds it again.
- *One decoder per address.* A domain's databases are decoded in
  `web/src/lib/domainDatabases.ts` for the Databases and the Backups tab; the
  certificate card uses the decoder of the SSL/TLS tab.
- *Row actions on a phone.* A data table may be wider than a phone and scroll
  sideways inside its frame; what a row can do must not be the part that is off
  screen. The cell that holds a row's actions, and the header cell above it,
  carry the class `row-actions` (`web/src/index.css`): below the `sm` width
  (640 px) it stays at the table's trailing edge, opaque, with a hairline on its
  leading side, while the other columns scroll under it. Wider screens are
  unchanged. Applied to the DNS records, the Domains list, the Databases page
  (both tables) and the banned addresses of Fail2ban. In the DNS records the
  value column also has a least width: without it a phone squeezed a long value
  to one character a line and one row was two screens tall. A named action in
  that cell keeps its words on one line (2026-10-10; "Yasağı kaldır" broke into
  two at 390 px).

**The texts.** Keys are in `web/src/i18n` (`common.*` in the shell catalogue,
the rest in `screens`).

- *A change whose answer did not arrive, after the state was read again*
  - `common.resultUnknownRead`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. Nothing was sent a second time. What is shown
      here was read again at {time}: check it before repeating the action."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. Hiçbir şey ikinci kez gönderilmedi. Burada
      gösterilen, saat {time} itibarıyla yeniden okundu: işlemi yinelemeden
      önce ona bakın."
  - While that read is on its way: `common.resultUnknown` (second batch).
- *The same, when the form asked and the state that was read again shows the
  change* (2026-10-10)
  - `common.resultUnknownMade`
    - EN: "The connection dropped before the answer arrived, and nothing was
      sent a second time. What was read again at {time} shows the change, so it
      was saved. Nothing needs to be sent again."
    - TR: "Yanıt gelmeden bağlantı koptu ve hiçbir şey ikinci kez gönderilmedi.
      Saat {time} itibarıyla yeniden okunan durum değişikliği gösteriyor; yani
      kaydedildi. Hiçbir şeyin yeniden gönderilmesi gerekmiyor."
- *The same, when it does not show the change* (2026-10-10)
  - `common.resultUnknownNotMade`
    - EN: "The connection dropped before the answer arrived, and nothing was
      sent a second time. What was read again at {time} does not show the
      change, so it is not known to have been saved. What you entered is still
      here. If the server was still working when the connection dropped, the
      change can appear later: check again before sending it a second time."
    - TR: "Yanıt gelmeden bağlantı koptu ve hiçbir şey ikinci kez gönderilmedi.
      Saat {time} itibarıyla yeniden okunan durum değişikliği göstermiyor; bu
      yüzden kaydedildiği bilinmiyor. Girdikleriniz hâlâ burada. Bağlantı
      koptuğunda sunucu hâlâ çalışıyorduysa değişiklik sonradan görünebilir:
      ikinci kez göndermeden önce tekrar kontrol edin."
- *The same, when the state could not be read again either*
  - `common.resultUnknownUnread`
    - EN: "The connection dropped before the answer arrived, so it is not known
      whether the change was made. Nothing was sent a second time. The current
      state could not be read again either, so controls that change or remove
      something stay off. Check again."
    - TR: "Yanıt gelmeden bağlantı koptu; bu yüzden değişikliğin yapılıp
      yapılmadığı bilinmiyor. Hiçbir şey ikinci kez gönderilmedi. Güncel durum
      da yeniden okunamadı; bu yüzden bir şeyi değiştiren ya da kaldıran
      denetimler kapalı kalıyor. Tekrar kontrol edin."
  - `common.checkAgain`: EN "Check again" · TR "Tekrar kontrol et". Closing the
    notice: `common.close`, EN "Close" · TR "Kapat".
- *DNS: the zone could not be read*
  - `dns.zoneUnknown`
    - EN: "The DNS zone of {name} could not be read from the server, so its
      records are not shown as current. This does not mean the zone is missing.
      Nothing was changed. Try again."
    - TR: "{name} alan adının DNS bölgesi sunucudan okunamadı; bu yüzden
      kayıtları güncel diye gösterilmiyor. Bu, bölgenin olmadığı anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *DNS: the records*
  - `dns.records.checking`: EN "Reading this domain’s DNS records…" · TR "Bu alan
    adının DNS kayıtları okunuyor…"
  - `dns.records.unknown`
    - EN: "The DNS records of this domain could not be read from the server, so
      the list is not shown. This does not mean there are none. Nothing was
      changed. Try again."
    - TR: "Bu alan adının DNS kayıtları sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, kayıt olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *DNS: whether the zone is signed*
  - `dnssec.checking`: EN "Checking whether this zone is signed…" · TR "Bu
    bölgenin imzalı olup olmadığı kontrol ediliyor…"
  - `dnssec.unknown`
    - EN: "CelikPanel could not check whether this zone is signed, so signing is
      not offered. This does not mean it is unsigned. Nothing was changed. Try
      again."
    - TR: "CelikPanel bu bölgenin imzalı olup olmadığını kontrol edemedi; bu
      yüzden imzalama sunulmuyor. Bu, imzasız olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *Hosting type*
  - `hosting.checking`: EN "Reading this domain’s hosting settings…" · TR "Bu
    alan adının barındırma ayarları okunuyor…"
  - `hosting.unknown`
    - EN: "The hosting settings of this domain could not be read from the
      server, so they are not shown and cannot be changed here yet. Nothing was
      changed. Try again."
    - TR: "Bu alan adının barındırma ayarları sunucudan okunamadı; bu yüzden
      gösterilmiyor ve şimdilik buradan değiştirilemiyor. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `hosting.nodeChecking`: EN "Checking which Node.js versions are installed…"
    · TR "Kurulu Node.js sürümleri kontrol ediliyor…"
  - `hosting.nodeUnknown`
    - EN: "The installed Node.js versions could not be checked, so only the
      saved version is listed. Nothing was changed. Try again."
    - TR: "Kurulu Node.js sürümleri kontrol edilemedi; bu yüzden yalnız kayıtlı
      sürüm listeleniyor. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Hosting type: the live application*
  - `hosting.app.checking`: EN "Reading the application’s state…" · TR
    "Uygulamanın durumu okunuyor…"
  - `hosting.app.logsChecking`: EN "Reading the application’s logs…" · TR
    "Uygulamanın günlükleri okunuyor…"
  - `hosting.app.unknown`
    - EN: "The state and the logs of this application could not be read from
      the server. This does not mean it has stopped. Nothing was changed; start,
      stop and restart are off until the state can be read. CelikPanel keeps
      trying."
    - TR: "Bu uygulamanın durumu ve günlükleri sunucudan okunamadı. Bu,
      uygulamanın durduğu anlamına gelmez. Hiçbir şey değiştirilmedi; başlat,
      durdur ve yeniden başlat, durum okunana dek kapalıdır. CelikPanel denemeyi
      sürdürüyor."
  - `hosting.app.stale` (an earlier answer is still shown)
    - EN: "The application could not be read again just now, so what is shown is
      as it was at {time}. This does not mean it has stopped. Nothing was
      changed; start, stop and restart are off until the state can be read.
      CelikPanel keeps trying."
    - TR: "Uygulama az önce yeniden okunamadı; gösterilen, saat {time}
      itibarıyla olan hâlidir. Bu, uygulamanın durduğu anlamına gelmez. Hiçbir
      şey değiştirilmedi; başlat, durdur ve yeniden başlat, durum okunana dek
      kapalıdır. CelikPanel denemeyi sürdürüyor."
- *PHP*
  - `php.checking`: EN "Reading this domain’s PHP settings…" · TR "Bu alan
    adının PHP ayarları okunuyor…"
  - `php.unknown`
    - EN: "The PHP settings of this domain could not be read from the server, so
      they are not shown and cannot be changed here yet. Nothing was changed.
      Try again."
    - TR: "Bu alan adının PHP ayarları sunucudan okunamadı; bu yüzden
      gösterilmiyor ve şimdilik buradan değiştirilemiyor. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *General settings*
  - `general.checking`: EN "Reading this domain’s settings…" · TR "Bu alan
    adının ayarları okunuyor…"
  - `general.unknown`
    - EN: "The settings of this domain could not be read from the server, so
      they are not shown and cannot be changed here yet. This does not mean it
      has no aliases. Nothing was changed. Try again."
    - TR: "Bu alan adının ayarları sunucudan okunamadı; bu yüzden gösterilmiyor
      ve şimdilik buradan değiştirilemiyor. Bu, takma adı olmadığı anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
- *Applications*
  - `apps.checking`: EN "Reading the applications that can be installed…" · TR
    "Kurulabilecek uygulamalar okunuyor…"
  - `apps.unknown`
    - EN: "The list of applications could not be read from the server, so
      nothing is offered. This does not mean no application is available.
      Nothing was changed. Try again."
    - TR: "Uygulama listesi sunucudan okunamadı; bu yüzden hiçbir şey
      sunulmuyor. Bu, kullanılabilir uygulama olmadığı anlamına gelmez. Hiçbir
      şey değiştirilmedi. Tekrar deneyin."
  - `apps.resultUnknownWhere` (under the result-unknown notice)
    - EN: "This page cannot show whether the application was installed. Look at
      this domain’s Files and Databases before installing again."
    - TR: "Bu sayfa uygulamanın kurulup kurulmadığını gösteremez. Yeniden
      kurmadan önce bu alan adının Dosyalar ve Veritabanları bölümlerine bakın."
- *Certificate card of the overview*
  - `domain.overview.ssl.unavailableHint` (reworded; the card also has Retry now)
    - EN: "The certificate state could not be read from the server. This does
      not mean there is no certificate. Nothing was changed. Try again."
    - TR: "Sertifika durumu sunucudan okunamadı. Bu, sertifika olmadığı anlamına
      gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin."
  - `domain.overview.ssl.stale`: EN "This could not be read again just now; it
    is shown as it was at {time}." · TR "Bu, az önce yeniden okunamadı; saat
    {time} itibarıyla olan hâliyle gösteriliyor."
- *The certificate line under a domain's name* (2026-10-10)
  - `domain.info.sslUnknown`: EN "Could not be checked" · TR "Kontrol edilemedi",
    with `common.retry` beside it. While it is read:
    `domain.overview.ssl.checking`, EN "Checking status" · TR "Durum kontrol
    ediliyor".
- *Mail authentication*
  - `mailauth.checking`: EN "Checking this domain’s SPF, DKIM and DMARC records…"
    · TR "Bu alan adının SPF, DKIM ve DMARC kayıtları kontrol ediliyor…"
  - `mailauth.unknown`
    - EN: "The SPF, DKIM and DMARC records of this domain could not be checked,
      so their state is not shown and nothing can be published here yet. This
      does not mean they are missing. Nothing was changed. Try again."
    - TR: "Bu alan adının SPF, DKIM ve DMARC kayıtları kontrol edilemedi; bu
      yüzden durumları gösterilmiyor ve şimdilik buradan hiçbir şey
      yayımlanamıyor. Bu, kayıtların eksik olduğu anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
- *Logs*
  - `logs.checking`: EN "Reading the log…" · TR "Günlük okunuyor…"
  - `logs.unknown`
    - EN: "This log could not be read from the server, so no lines are shown.
      This does not mean the log is empty. Nothing was changed. Try again."
    - TR: "Bu günlük sunucudan okunamadı; bu yüzden satır gösterilmiyor. Bu,
      günlüğün boş olduğu anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar
      deneyin."
  - A poll that fails with lines on screen: `common.staleNotice` (first batch).
- *Backups*
  - `backup.checking`: EN "Reading this domain’s backups…" · TR "Bu alan adının
    yedekleri okunuyor…"
  - `backup.unknown`
    - EN: "The backups of this domain could not be read from the server, so the
      list is not shown. This does not mean there are none. Nothing was changed.
      Try again."
    - TR: "Bu alan adının yedekleri sunucudan okunamadı; bu yüzden liste
      gösterilmiyor. Bu, yedek olmadığı anlamına gelmez. Hiçbir şey
      değiştirilmedi. Tekrar deneyin."
  - `backup.databasesUnknown`
    - EN: "The databases linked to this domain could not be read, so a database
      or full backup cannot be made here yet. This does not mean there are none.
      Nothing was changed; a files backup is still available. Try again."
    - TR: "Bu alan adına bağlı veritabanları okunamadı; bu yüzden şimdilik
      buradan veritabanı yedeği ya da tam yedek alınamıyor. Bu, veritabanı
      olmadığı anlamına gelmez. Hiçbir şey değiştirilmedi; dosya yedeği yine
      alınabilir. Tekrar deneyin."
  - `backup.databasesNotRead` (in the picker): EN "Databases not read" · TR
    "Veritabanları okunamadı"
- Removed, because nothing shows them any more: `dns.zoneStatusUnavailable`,
  `dns.recordsLoadFailed`, `php.loadFailed`, `general.loadFailed`,
  `backup.databaseLoadError`, `backup.retry`.

**How it is kept from coming back.**

- *The ratchet.* Before this batch 8 / 28 / 10 / 57 / 19 in 40 files; after it
  8 / 25 / 6 / 42 / 12 in 31 files, pinned as the new ceilings (regenerated on
  2026-10-10 on the tree that also holds the settings-writes corrections: the
  same files and totals). Off the list:
  `DomainDNSManager`, `HostingTypePanel`, `DomainPHPSettings`,
  `DomainGeneralSettings`, `DomainAppsPanel`, `DomainSSLOverviewCard`,
  `MailAuthPanel`, `DomainBackupManager`, `DomainCronManager`.
  `DomainLogsViewer` stays with one raw read and nothing else:
  `TestDomainLogsViewerExposesHonestTimeFilterControlsAndMetadata`
  (`cmd/panel/domain_logs_frontend_test.go`) pins
  `parseDomainLogsResponse(await res.json())` in that file, so its read builds
  the same three states there.
- *The mounted test* (`web/tests/remote-state-mounted-batch4.test.mjs`, 113
  cases): fourteen reads, each withheld four ways; the known negatives; fifteen
  changes, each with its answer lost two ways (the connection ends; a gateway
  answers), requiring one request, a read-only re-read, the changing controls
  off until it answers, and a notice that stays; a refusal by the Panel that is
  not an unknown result; polls that fail (one notice, no toast, only reads); the
  poll that does not ask while a read is on its way; the forms that hold the
  server's values; the one place where something is kept in the page without
  being drawn; the `row-actions` rule and the tables that carry it. Since
  2026-10-10 the file has 129 cases: what the notice says after the re-read for
  each of the fifteen changes; the six forms that ask, each with a state that
  shows the change and one that does not, the later check that finds a record
  the server finished late, a new row that decides nothing, the server's
  rewriting of a record, and a list that could not be read again; six ways a
  crontab can fail to be read (entry of 2026-10-10 below); and one line for a
  named row action. The strip under a domain's name has its case in
  `web/tests/remote-state-mounted.test.mjs`: checking, could not be checked on
  the overview and on the DNS tab for each way a read fails, one request for
  the certificate, a Retry that only reads, and the server's "no certificate".
- *Pins moved with the code, not loosened*
  (`web/tests/additional-user-domain-ui-contract.test.mjs`): a team member's PHP
  versions are still the tenant-safe `available_versions` of this domain's own
  answer and exist only while that answer is known (was: cleared state before
  each load); the PHP panel sends and reads only through the shared layer; a
  read-only or externally managed zone is offered the read of its records and
  nothing that changes them, and each of the three DNS changes starts with that
  check; the decoder of a domain's databases is pinned in
  `lib/domainDatabases.ts`, and a second one in the component is refused. The
  fixture of the first mounted test answered the DNSSEC read with
  `{ enabled: false }`, which is not what the handler writes; it answers
  `{ secured, ds }` now.

**Needs the server (not done here; no Go was changed).**

- None of these changes had a request identity on 2026-10-09. Since 2026-10-10
  the manual backup and the restore carry one (D-029; the entry of that date
  below). For the others, until the server keeps one, a lost answer leaves the
  result to the person, as above:
  `POST /api/v1/domains/{id}/dns/records`,
  `DELETE /api/v1/domains/{id}/dns/records?id=`,
  `POST /api/v1/domains/{id}/dns/zone`, `POST /api/v1/domains/{id}/dnssec` (DNS
  tab); `PUT /api/v1/domains/{id}/hosting`,
  `POST /api/v1/domains/{id}/app/{start|stop|restart}` (Hosting type);
  `POST /api/v1/domains/{id}/php`, `POST /api/v1/domains/{id}/php/pool` (PHP);
  `POST /api/v1/domains/{id}/general`, `POST /api/v1/domains/{id}/aliases`,
  `DELETE /api/v1/domains/{id}/aliases/{alias}` (General);
  `POST /api/v1/domains/{id}/apps/install` (Applications; it also has no read
  that shows whether an application is installed);
  `POST /api/v1/domains/{id}/mail/auth/apply`,
  `POST /api/v1/domains/{id}/mail/auth/dkim` (Mail authentication);
  `DELETE /api/v1/domains/{id}/logs/{type}` (Logs);
  `POST /api/v1/domains/{id}/backups`,
  `POST /api/v1/domains/{id}/backups/restore`,
  `DELETE /api/v1/domains/{id}/backups?name=` (Backups). From the second batch,
  still with a toast and a re-read only: `POST`/`PUT`/`DELETE /api/v1/users…`,
  `POST /api/v1/users/{id}/impersonate`, `/api/v1/plans…` (Accounts) and the
  changes of a domain's Files. Not migrated yet, and not looked at here: the
  add-ons, the VPN peers and the team members (adding a VPN device carries an
  identity since 2026-10-10).
- A browser can send a change again by itself. In the browser run, when the
  mock only reset a connection that had carried an earlier request, Chrome
  sent the `POST` again without the page asking: one click, three arrivals.
  Nothing in the page can prevent that; only a request identity on the server
  can make the second arrival harmless.
- A change that never answers and never fails (the connection stays open)
  leaves its control busy; the page has no time limit for it.

**Not done.**

- 31 files still read the old way. In scope of this batch and not migrated:
  `ServiceList` (1 / 1 / 0 / 6 / 1), `ServiceShell` (0 / 2 / 2 / 2 / 2),
  `Layout` (1 / 2 / 0 / 1 / 0), `Dashboard` (1 / 3 / 0 / 4 / 0), `AddonsPage`
  (0 / 0 / 0 / 2 / 3), `StoreCatalogAdmin` (0 / 0 / 0 / 1 / 2), `AuditLogPage`
  (0 / 1 / 0 / 1 / 1), `VPNPage` (0 / 0 / 0 / 0 / 1), `TeamMembersPage`
  (0 / 0 / 0 / 0 / 1), `SystemSQLiteManager` (0 / 2 / 0 / 1 / 1),
  `SecurityAuditCard` (0 / 0 / 0 / 1 / 0), `AddDatabaseModalV2`
  (0 / 0 / 0 / 1 / 0), `DatabaseAccountStrip` (0 / 1 / 0 / 1 / 0). None of them
  was opened in this batch. Two of them have their readers held as source text
  by existing contract tests, which a migration has to move with the code:
  `Layout` by `layout-server-identity-contract` (exactly one
  `fetch('/api/v1/panel/version'` in the file and none in the build stamp), and
  `ServiceShell` by `component-inventory-contract`,
  `service-unobserved-state-contract` and
  `service-shell-install-confirmation-contract`.
- The accounts, plans and files changes of the second batch still answer a lost
  connection with a toast that leaves by itself; they were not moved to the
  notice that stays.
- The scheduled tasks keep their own state for the list and the version; only
  the proof of an empty list was added, and since 2026-10-10 the reason a
  crontab could not be read (entry of that date below). Their writes were not
  touched.
- On a wide screen the DNS card with a signed zone is taller than the room kept
  for it while it checks (the room is the unsigned card's), so the records move
  once for a signed zone.
- The row's hover tint does not reach under the fixed action cell on a phone.
- A could-not-check notice still does not say why the read failed, except
  where the server verified a cause: the scheduled tasks and the mail queue
  (entry of 2026-10-10 below).
- The changes that ask the re-read state no question (listed above) still leave
  the result to the person. No change on these panels had a request identity on
  that date; the backup and the restore have one since 2026-10-10.
- Not verified on a real server; one Chrome against a mock.

**Browser inspection of this batch (2026-10-09).** In a real, installed Chrome
against the loopback mock (`web/tools/browser-inspect`, scenarios `domaindns`,
`domainhosting`, `domainphp`, `domaingeneral`, `domainapps`, `domainmailauth`,
`domainlogs`, `domainbackups`, `sslcard`, `rowactions`; every scenario of the
earlier batches was run again on the same build): desktop 1440×900 and phone
390×844, Turkish and English, light and dark. 357 states in each of the eight
configurations, 81 of them of this batch; no scenario reported an error. A
scenario of this batch fails when a state it should record shows no checking
line, notice or result-unknown notice, when a place it should measure is not
found, or when a poll it should count did not run.

- *Measured in all eight configurations.* No negative sentence was on screen in
  any checking or could-not-check state of these panels, and no failed read
  raised a toast. Each of the six changes whose answer was lost (a DNS record
  added, hosting applied with a gateway answering, an alias removed, an
  application installed, a mail record published, a backup created) was sent by
  the page once and arrived at the mock once; its notice was inside the window
  when it appeared, the changing controls were off until the state had been
  read again, "Check again" sent only reads, and the notice left only with
  "Close". After the lost answers the re-read state showed what the mock had
  done: the new record once, the alias gone, and, for the backup the mock did
  not make, the same two rows. With the application's polls failing for twelve
  seconds (four requests) and the log's auto-refresh refused (two), there was
  one notice, no toast, the earlier state and lines stayed, start, stop,
  restart and clear were off, and every request was a read. The records table
  did not move when the signing state arrived (0 px). The certificate card did
  not change height between checking and "no certificate" (0 px); with the
  could-not-check sentence and Retry it is the same height on a wide screen and
  taller by 38 px (English) or 80 px (Turkish) on a phone. Every row action was
  inside the width of the screen and on top with the table scrolled to its
  start: the six of the DNS records, the three of a Domains row, the two of the
  Databases page and the two "Unban" of Fail2ban, on tables that do scroll
  sideways at 390 px; and the actions of the alias and backup rows, which do
  not. No page scrolled sideways.
- *Found by looking, or by measuring, and corrected.*
  - On a phone one DNS row with a long value was 1,982 px tall: the value column
    had been squeezed to one character a line. It has a least width now.
  - On a phone the DNSSEC card grew by 86 px when its answer arrived, with a
    fixed height reserved; it keeps the room of its usual answer now (0 px).
  - On a phone the certificate card grew by 19 px between checking and "no
    certificate"; its second line keeps two lines of room there.
  - The live application panel was photographed below the fold, and so were
    most states on a phone, where the tabs of a domain fill the first screen;
    the scenarios bring what a state is about into the window first.
  - The scenarios called the Turkish stop button ("Durdur") the negative
    "Durdu"; negatives are matched as whole words.
  - With a connection that was only reset, the mock received one click three
    times (above); it ends the connection with bytes that are not an answer.
- *Seen on 2026-10-09 and corrected on 2026-10-10.* After a lost answer the
  form that sent the change stayed open with what was typed, so the same record
  could be saved again once the list had been read: the form now asks the state
  that was read again (above). On a phone "Yasağı kaldır" broke into two lines
  in its fixed cell: one line now, 107×34 px. This has a cost, seen by
  comparing the screenshots of the two dates and not caught by the scenario: in
  Turkish the fixed cell is about 41 px wider than it was, and at 390 px the
  end of a full-length IPv6 address (about its last five characters) is now
  under that cell, where the whole address fitted before; nothing marks it as
  cut, and it is reached by scrolling the table sideways, like the jail column
  beside it. English is unchanged ("Unban" never broke). Whether the address
  should break onto two lines, or the action be shorter on a phone, is not
  decided here. The strip under a domain's name
  said "SSL: checking status" for as long as the certificate read had not
  succeeded, also after it failed: three states now.
- *Seen and not changed.* While mail authentication is read again after a lost
  answer, the earlier "Missing" stays on screen under the notice. A disabled
  card is faint in the dark theme. Between the Node.js version and the port
  note there is one empty line, kept for the checking line. On a phone the
  strip under a domain's name breaks into three rows and leaves a divider at
  the end of a row. On a phone the mail queue's table breaks an address inside
  a word (second batch). Three notes (`DomainDNSManager` for a team member,
  `DomainDatabaseManager`, `DomainDetail`) use an `info` colour the theme does
  not define, so they are drawn without a surface.
- *Run again on 2026-10-10* on the tree that also holds the settings-writes
  corrections, with the scenarios `lostforms` and `sslfact` added and `cron`,
  `mailqueue`, `dbconfig`, `domaindns`, `domainhosting` and `rowactions`
  extended: 396 states in each of the eight configurations, 39 of them new
  (17 of `lostforms`, 8 of `sslfact`, 7 of `mailqueue`, 4 of `cron`, 2 of
  `dbconfig`, 1 of `domainssl`), none of the 357 earlier ones missing. The full
  run reported one error, the same in all eight: `domainssl` stopped waiting
  after the certificate request. The cause was this change, not the mock: with
  the strip reading the certificate, the SSL/TLS tab opens over an answer the
  page already has (above), and the scenario pressed "Get certificate" while
  the tab's controls were off, so nothing was sent and three of its states
  were recorded under names that were no longer true. The scenario was
  corrected (it now opens the tab in both ways, presses only an enabled
  button, and fails unless the request arrives exactly once) and run again in
  all eight: no error, and its six earlier states have the facts they had
  before. `mailqueue` was run again in all eight after the correction of the
  unknown reload outcome (entry of 2026-10-10 below): no error. A comparison of
  the two builds chunk by chunk, ignoring content hashes, found the Postfix
  page to be the only code that differs between the build of the full run and
  the final one. Of the 357 earlier states, seven have other facts than on
  2026-10-09: the loopback port printed in one address; the mail queue's
  sentence and the label over the reload line, both from the settings-writes
  corrections; and the four states changed on purpose here (the saved DNS
  record with its form closed, twice; Apply shown as saved; the strip's Retry
  beside the card's). Measured in all eight: after a lost answer the notice
  was in the state the re-read called for (`made` with the form closed and the
  record listed once, `not-made` with the typed value still in the form and the
  save control back, `read` for the changes that ask nothing); every lost
  change was sent by the page once and arrived once; "Check again" sent only
  reads and found a record the mock added afterwards; a change shown as saved
  offered only "Close" and was not on the attention surface. The strip said it
  was checking, then what the server said; after a failed read it said "could
  not be checked" with Retry inside the width of the screen, on the overview
  and on the DNS tab, and Retry sent only reads. No row action's label was on
  more than one line. A new scenario fails when the notice is in another state
  than the one named, when the field it should read is not found, or when
  nothing was measured.
- *Not covered.* Any real server; Safari, Firefox, a screen reader, a touch
  device; the imitation skins; roles other than the administrator (a team
  member's view of these panels is covered by the mounted and contract tests
  only). A signed zone, a zone managed elsewhere, the scheduled tasks' list
  without `jobs`, a restore and the DKIM key were not in the browser run; they
  are covered by the mounted test.

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

### Settings writes after the first native measurement: a reload is verified, no cause is named that was not verified, and a failed reload says what the server holds (2026-10-10)

Source state with component tests; the native re-run on real services is
pending, and nothing was observed on an installed server. See the resilience
contract entry of the same date for what was measured and what changed. This
entry holds the sentences.

**The rule the texts follow.** A sentence states a cause only when the server
verified it. A verified failure, an unmet prerequisite and an unknown outcome
are three different answers: "Postfix's own check refuses its configuration"
(verified), "this server's /etc/cron.allow does not list the user" (a
prerequisite only the owner can change), "CelikPanel could not establish
whether Postfix took the saved values" (unknown). Where the server's own
program printed a line, it is shown with the sentence, bounded and with
password assignments blanked; it is never a sentence itself.

**Who acts.** The person at the screen, unless a text says "on the server" or
"the server owner": then the server owner, with the command in the text.
Nothing retries by itself. No text asks for a command that reports success
whatever happened: the Postfix recovery command is `sudo postfix reload`, not
`sudo systemctl reload postfix`, which on Ubuntu reloads a wrapper unit.

**How work resumes.** After the owner's correction: reload the page (mail
policy, configuration file), or try again (mail queue, scheduled tasks). A
policy that was written stays written and is what the form shows; a
configuration change that was not kept is still in the form, and the file on
the server is the previous one. For the mail policy the owner may also press
Save without changing a value: every accepted save ends with the verified
reload, so that save makes Postfix take the file, or says again why it did not
(below).

**Platform limitation.** On a server whose `/etc/cron.allow` does not list a
site user, CelikPanel can neither read nor change that user's scheduled tasks
(Debian's and Ubuntu's `crontab -u <user>` refuses the user even for root).
CelikPanel says so, changes nothing and does not edit `cron.allow`: whether a
site user may use cron on a hardened server is the owner's decision.

**The texts.** API sentences are the `error` field, in English only. Screen
sentences are catalogue entries: `err.*` in `web/src/i18n`, `mailpolicy.*`,
`postfix.*` and `dbconf.*` in `web/src/i18n/screens/server`, `cron.*` in
`web/src/i18n/screens`. `{service}` is the service's name, `{unit}` and
`<unit>` the systemd unit the Agent names, `{detail}` the server's own line.

**Mail policy save (`PUT /api/v1/mail/policy`).**

- `502 MAIL_POLICY_NOT_RELOADED`, reason `check`
  API: "The mail policy was saved to /etc/postfix/main.cf, but Postfix was not
  reloaded: Postfix's own check refuses its configuration as it is now. A
  running Postfix keeps the settings it had before, so the saved values are not
  in effect. What Postfix said is shown with this message; it can be about a
  line this page did not write. Nothing was rolled back. The server owner
  corrects that line, runs sudo postfix check until it prints no error, then
  runs sudo postfix reload. Reload this page afterwards; the saved values are
  the ones shown."
- `err.MAIL_POLICY_NOT_RELOADED.check`
  EN: "Saved to /etc/postfix/main.cf, but Postfix was not reloaded: its own
  check refuses the configuration. A running Postfix keeps the settings it had
  before, so the saved values are not in effect. The line it names is below and
  may be one this page did not write. Nothing was rolled back. On the server,
  correct that line, run sudo postfix check until it prints no error, then run
  sudo postfix reload. The values shown below are the saved ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ancak Postfix yeniden
  yüklenmedi: Postfix’in kendi denetimi yapılandırmayı reddediyor. Çalışan bir
  Postfix önceki ayarlarını korur; bu yüzden kaydedilen değerler yürürlükte
  değil. Adını verdiği satır aşağıda; bu sayfanın yazmadığı bir satır olabilir.
  Hiçbir şey geri alınmadı. Sunucuda o satırı düzeltin, hata yazmayana kadar
  sudo postfix check komutunu çalıştırın, sonra sudo postfix reload komutunu
  çalıştırın. Aşağıda gösterilen değerler kaydedilen değerlerdir."
- `502 MAIL_POLICY_NOT_RELOADED`, reason `reload`
  API: "The mail policy was saved to /etc/postfix/main.cf and Postfix's own
  check accepts the file, but the reload failed, so Postfix has not taken the
  saved values. What the reload said is shown with this message. Nothing was
  rolled back. The server owner runs sudo postfix reload on the server and reads
  what it prints. Reload this page afterwards; the saved values are the ones
  shown."
- `err.MAIL_POLICY_NOT_RELOADED.reload`
  EN: "Saved to /etc/postfix/main.cf, and Postfix’s own check accepts the file,
  but the reload failed, so Postfix has not taken the saved values. Nothing was
  rolled back. On the server, run sudo postfix reload and read what it prints.
  The values shown below are the saved ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ve Postfix’in kendi denetimi
  dosyayı kabul ediyor, ancak yeniden yükleme başarısız oldu; bu yüzden Postfix
  kaydedilen değerleri almadı. Hiçbir şey geri alınmadı. Sunucuda sudo postfix
  reload komutunu çalıştırın ve yazdığını okuyun. Aşağıda gösterilen değerler
  kaydedilen değerlerdir."
- `502 MAIL_POLICY_NOT_RELOADED`, reason `verify`
  API: "The mail policy was saved to /etc/postfix/main.cf, but Postfix was no
  longer running after the reload, so it has not taken the saved values and is
  not handling mail. Nothing was rolled back. The server owner runs sudo postfix
  check, then starts Postfix (sudo systemctl start postfix) and confirms it with
  sudo postfix status. Reload this page afterwards; the saved values are the
  ones shown."
- `err.MAIL_POLICY_NOT_RELOADED.verify`
  EN: "Saved to /etc/postfix/main.cf, but Postfix was no longer running after
  the reload, so it is not handling mail. Nothing was rolled back. On the
  server, run sudo postfix check, start Postfix (sudo systemctl start postfix)
  and confirm with sudo postfix status. The values shown below are the saved
  ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ancak yeniden yüklemeden sonra
  Postfix artık çalışmıyordu; bu yüzden posta işlemiyor. Hiçbir şey geri
  alınmadı. Sunucuda sudo postfix check komutunu çalıştırın, Postfix’i başlatın
  (sudo systemctl start postfix) ve sudo postfix status ile doğrulayın. Aşağıda
  gösterilen değerler kaydedilen değerlerdir."
- `502 MAIL_POLICY_RELOAD_UNKNOWN`
  API: "The mail policy was saved to /etc/postfix/main.cf, but CelikPanel could
  not establish whether Postfix took the saved values: a command that checks or
  reloads Postfix could not be run, or did not answer in time. This is not a
  verified failure, and Postfix may already be running with them. Nothing was
  rolled back. The server owner runs sudo postfix status and then sudo postfix
  reload on the server. Reload this page afterwards; the saved values are the
  ones shown."
- `err.MAIL_POLICY_RELOAD_UNKNOWN`
  EN: "Saved to /etc/postfix/main.cf, but CelikPanel could not establish whether
  Postfix took the saved values: a command that checks or reloads Postfix could
  not be run or did not answer in time. This is not a verified failure; Postfix
  may already be running with them. Nothing was rolled back. On the server, run
  sudo postfix status, then sudo postfix reload. The values shown below are the
  saved ones."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi ancak CelikPanel, Postfix’in
  kaydedilen değerleri alıp almadığını belirleyemedi: Postfix’i denetleyen ya da
  yeniden yükleyen bir komut çalıştırılamadı ya da zamanında yanıt vermedi. Bu
  doğrulanmış bir hata değildir; Postfix bu değerlerle çalışıyor olabilir.
  Hiçbir şey geri alınmadı. Sunucuda önce sudo postfix status, sonra sudo
  postfix reload komutunu çalıştırın. Aşağıda gösterilen değerler kaydedilen
  değerlerdir."
- On the screen, the two kinds of answer do not look alike (2026-10-10). A
  reload Postfix was verified not to have taken (`MAIL_POLICY_NOT_RELOADED`) is
  a failure after a change and stands on the failure surface. An outcome that
  could not be established (`MAIL_POLICY_RELOAD_UNKNOWN`) is not one, as its own
  sentence says, and stands on the attention surface with ink text. In the
  first form of this entry both were drawn through the same failure banner, so
  "this is not a verified failure" was written in red; that was found by
  looking at the browser record of 2026-10-10, not by a test, and the mounted
  test and the `mailqueue` scenario now check the surface of each.
- `mailpolicy.postfixSaid`
  EN: "Postfix said:"
  TR: "Postfix’in yanıtı:"
- `mailpolicy.observed`
  EN: "What CelikPanel observed:"
  TR: "CelikPanel’in gözlediği:"
- `mailpolicy.saved.notRunning`
  EN: "Saved to /etc/postfix/main.cf. Postfix is not running on this server, so
  there was nothing to reload; it reads these values when it starts."
  TR: "/etc/postfix/main.cf dosyasına kaydedildi. Postfix bu sunucuda
  çalışmıyor; bu yüzden yeniden yüklenecek bir şey yoktu. Bu değerleri
  başladığında okur."
- `mailpolicy.saved.unchanged`
  EN: "Nothing to save: the server already holds exactly these values."
  TR: "Kaydedilecek bir şey yok: sunucu zaten tam bu değerleri tutuyor."
- A save without a change (2026-10-10, after the first form of this entry).
  Until then such a save answered `200` / `unchanged` and asked Postfix
  nothing, so after "not reloaded" an owner who had corrected main.cf and
  pressed Save was told "nothing to save" while Postfix went on running the
  earlier values. Postfix cannot be asked which values a running master holds,
  and the Agent keeps no record of the earlier outcome, so every accepted save
  now ends with the same verified reload (Postfix's own check, `postfix
  status`, `postfix reload`, `postfix status`), also one that writes nothing.
  Nothing is written, no polling starts it, and a stopped Postfix is still left
  stopped. The answers:
  - `200`, `applied: unchanged_reloaded`: nothing was written; the running
    Postfix was reloaded and is still running.
  - `200`, `applied: unchanged`: nothing was written and Postfix is stopped, so
    there was nothing to reload.
  - `502 MAIL_POLICY_NOT_RELOADED` (reason `check`, `reload` or `verify`) or
    `502 MAIL_POLICY_RELOAD_UNKNOWN`, with the sentences above and the policy in
    the body: Postfix still did not take the file. This answer does not carry
    `mutation_applied` or `partial_success`, because this request changed
    nothing in main.cf, and no "written" line is added to the audit log.
- `mailpolicy.saved.unchangedReloaded`
  EN: "Nothing to save: the server already holds exactly these values. Postfix
  was reloaded with them and is running."
  TR: "Kaydedilecek bir şey yok: sunucu zaten tam bu değerleri tutuyor. Postfix
  bu değerlerle yeniden yüklendi ve çalışıyor."
- `mailpolicy.unreadable`
  EN: "The current mail policy could not be read from the server, so the
  settings are not shown and nothing can be saved here. Nothing was changed. Try
  again."
  TR: "Geçerli posta politikası sunucudan okunamadı; bu yüzden ayarlar
  gösterilmiyor ve buradan kayıt yapılamıyor. Hiçbir şey değiştirilmedi. Tekrar
  deneyin."

**Configuration save (`POST /api/v1/config`), `502 CONFIG_RELOAD_FAILED`.**

- reason `restored` (sentence unchanged)
  API: "The change was not kept: the service could not reload with the new file,
  so CelikPanel put the previous file back and the service is running with it.
  What the service said is below. Correct the setting and save again."
- reason `restored_unit_reload_failed`
  API: "The change was not kept, and the previous file is back in place. The
  service's systemd unit could not reload, with the new file and again with the
  previous one, so CelikPanel asked the server directly: it read the previous
  file again and is running with the settings it had before your change. What
  the unit's reload said is below. It failed with the previous file too, so the
  cause is not only this change. The server owner runs sudo systemctl reload
  <unit> on the server, corrects what it reports, and then saves the change here
  again."
- `dbconf.reloadFailed.restored_unit_reload_failed`
  EN: "The change was not kept, and the previous file is back in place. The
  systemd unit of {service} could not reload, with the new file and again with
  the previous one, so CelikPanel asked {service} directly: it read the previous
  file again and is running with the settings it had before your change. The
  reload failed with the previous file too, so the cause is not only this
  change. On the server, run sudo systemctl reload {unit} to see why, correct
  it, then save the change here again."
  TR: "Değişiklik tutulmadı ve önceki dosya yerine kondu. {service} hizmetinin
  systemd birimi yeni dosyayla da önceki dosyayla da yeniden yüklenemedi; bu
  yüzden CelikPanel doğrudan {service} hizmetine sordu: önceki dosyayı yeniden
  okudu ve değişikliğinizden önceki ayarlarla çalışıyor. Yeniden yükleme önceki
  dosyayla da başarısız olduğu için neden yalnız bu değişiklik değildir.
  Nedenini görmek için sunucuda sudo systemctl reload {unit} komutunu
  çalıştırın, düzeltin, sonra değişikliği buradan yeniden kaydedin."
- reason `restored_running_unknown`
  API: "The change was not kept, and the previous file is back in place. The
  service's systemd unit could not reload, with the new file and again with the
  previous one, and CelikPanel could not establish which settings the service is
  running with now: a reload that fails part-way may already have made it read
  the new file. What the unit's reload said is below. The server owner runs sudo
  systemctl reload <unit> on the server, corrects what it reports, and reloads
  this page."
- `dbconf.reloadFailed.restored_running_unknown`
  EN: "The change was not kept, and the previous file is back in place. The
  systemd unit of {service} could not reload, with the new file and again with
  the previous one, and CelikPanel could not establish which settings {service}
  is running with now: a reload that fails part-way may already have made it
  read the new file. On the server, run sudo systemctl reload {unit}, correct
  what it reports, then reload this page."
  TR: "Değişiklik tutulmadı ve önceki dosya yerine kondu. {service} hizmetinin
  systemd birimi yeni dosyayla da önceki dosyayla da yeniden yüklenemedi ve
  CelikPanel, {service} hizmetinin şu an hangi ayarlarla çalıştığını
  belirleyemedi: yarıda başarısız olan bir yeniden yükleme ona yeni dosyayı
  okutmuş olabilir. Sunucuda sudo systemctl reload {unit} komutunu çalıştırın,
  bildirdiğini düzeltin, sonra bu sayfayı yenileyin."
- reason `not_restored`
  API: "The service could not reload with the new file, and CelikPanel could not
  put the previous file back with certainty. The server owner checks the file on
  the server; the copy named below holds the previous file. Then reload the
  service (sudo systemctl reload <unit>) and reload this page."

**Scheduled tasks, `502 CURRENT_SETTINGS_UNREADABLE` / `scheduled_tasks`.**

- no `detail` token (no verified cause)
  API: "CelikPanel could not read this site user's scheduled tasks from the
  server, so the list is not shown and nothing was changed. This does not mean
  the user has no tasks. CelikPanel has not established why; what the server's
  crontab program said is shown with this message when it said anything. Reload
  the page to read the tasks again. If it keeps failing, the server owner runs
  sudo crontab -u <site user> -l on the server, which prints the same reason."
- `detail: cron_allow`
  API: "CelikPanel cannot read or change this site user's scheduled tasks: this
  server restricts crontab with /etc/cron.allow, and the user is not listed in
  it. Nothing was changed, and the tasks already on the server are untouched.
  While the server restricts crontab this way, CelikPanel cannot manage this
  user's tasks. The server owner adds the site user's name on its own line in
  /etc/cron.allow, then reloads this page."
- `cron.unknown.cron_allow`
  EN: "CelikPanel cannot read or change this domain’s scheduled tasks: this
  server restricts crontab with /etc/cron.allow, and the site’s system user is
  not listed in it. Nothing was changed; the tasks already on the server are
  untouched. While the server restricts crontab this way, CelikPanel cannot
  manage this user’s tasks. The server owner adds the user’s name on its own
  line in /etc/cron.allow, then this list can be read again."
  TR: "CelikPanel bu domain’in zamanlanmış görevlerini okuyamıyor ve
  değiştiremiyor: bu sunucu crontab kullanımını /etc/cron.allow ile kısıtlıyor
  ve sitenin sistem kullanıcısı o dosyada yok. Hiçbir şey değiştirilmedi;
  sunucudaki görevlere dokunulmadı. Sunucu crontab’ı bu şekilde kısıtladığı
  sürece CelikPanel bu kullanıcının görevlerini yönetemez. Sunucu sahibi
  kullanıcının adını /etc/cron.allow dosyasına ayrı bir satır olarak ekler;
  sonra bu liste yeniden okunabilir."
- `detail: cron_deny`
  API: "CelikPanel cannot read or change this site user's scheduled tasks:
  /etc/cron.deny on this server lists the user, so crontab refuses it. Nothing
  was changed, and the tasks already on the server are untouched. While the user
  is listed there, CelikPanel cannot manage this user's tasks. The server owner
  removes the site user's line from /etc/cron.deny, then reloads this page."
- `cron.unknown.cron_deny`
  EN: "CelikPanel cannot read or change this domain’s scheduled tasks:
  /etc/cron.deny on this server lists the site’s system user, so crontab refuses
  it. Nothing was changed; the tasks already on the server are untouched. The
  server owner removes the user’s line from /etc/cron.deny, then this list can
  be read again."
  TR: "CelikPanel bu domain’in zamanlanmış görevlerini okuyamıyor ve
  değiştiremiyor: bu sunucudaki /etc/cron.deny dosyası sitenin sistem
  kullanıcısını içeriyor; bu yüzden crontab onu reddediyor. Hiçbir şey
  değiştirilmedi; sunucudaki görevlere dokunulmadı. Sunucu sahibi kullanıcının
  satırını /etc/cron.deny dosyasından çıkarır; sonra bu liste yeniden
  okunabilir."
- `cron.unknown.said`
  EN: "The server’s crontab program said: {detail}"
  TR: "Sunucunun crontab programının yanıtı: {detail}"
- On the screen (2026-10-10, `DomainCronManager`; the answer's `detail` token is
  read by `web/src/lib/apiError.ts`). A cause the server verified
  (`cron_allow`, `cron_deny`) is the server owner's rule, not a failure: its
  sentence stands alone on the neutral surface with the plain information mark,
  announced politely, without the attention colour, and the line crontab
  printed is not repeated under it. Every other answer is the could-not-check
  notice with the neutral sentence `cron.unknown`, followed by
  `cron.unknown.said` when crontab printed a line; the line itself is in the
  mono face. A token the screen has no words for, and a refusal that is not
  this answer, get the neutral sentence. In every case the list is not shown,
  no task can be added or changed, and "Retry" reads again and only reads: it
  is how the list comes back after the owner changed `cron.allow` or
  `cron.deny`.
- `502 CURRENT_SETTINGS_UNREADABLE` for the other resources
  API: "CelikPanel could not read what is currently set on this server, so
  nothing is shown as a setting and nothing was changed. CelikPanel has not
  established why the read failed; when the server's own program printed a
  reason, it is shown with this message. Reload the page to read it again."

**Mail queue, `502 MAIL_QUEUE_UNREADABLE`.**

- no `reason` (no verified cause)
  API: "The mail queue could not be read, so it is not shown. This does not mean
  the queue is empty. Nothing was changed. CelikPanel has not established why;
  what Postfix's queue program said is shown with this message when it said
  anything. Try again. If it keeps failing, the server owner runs sudo postqueue
  -j on the server, which prints the same reason."
- `postfix.queue.unreadable`
  EN: "The mail queue could not be read, so it is not shown. This does not mean
  the queue is empty. Nothing was changed. Try again; if it keeps failing, run
  sudo postqueue -j on the server to see the reason."
  TR: "Mail kuyruğu okunamadı; bu yüzden gösterilmiyor. Bu, kuyruğun boş olduğu
  anlamına gelmez. Hiçbir şey değiştirilmedi. Tekrar deneyin; sorun sürerse
  nedeni görmek için sunucuda sudo postqueue -j komutunu çalıştırın."
- reason `postfix_config`
  API: "The mail queue could not be read because Postfix refuses its own
  configuration: a setting in /etc/postfix/main.cf or master.cf has an error,
  and Postfix's programs stop on it. What Postfix said about it is shown with
  this message. Nothing was changed, and this does not mean the queue is empty.
  The server owner corrects that setting, runs sudo postfix check until it
  prints no error, then reloads this page."
- `postfix.queue.unreadable.postfix_config`
  EN: "The mail queue could not be read because Postfix refuses its own
  configuration: a setting in /etc/postfix/main.cf or master.cf has an error,
  and Postfix’s programs stop on it. This does not mean the queue is empty.
  Nothing was changed. On the server, correct the setting Postfix names, run
  sudo postfix check until it prints no error, then try again."
  TR: "Mail kuyruğu okunamadı, çünkü Postfix kendi yapılandırmasını reddediyor:
  /etc/postfix/main.cf ya da master.cf içindeki bir ayar hatalı ve Postfix’in
  programları onda duruyor. Bu, kuyruğun boş olduğu anlamına gelmez. Hiçbir şey
  değiştirilmedi. Sunucuda Postfix’in adını verdiği ayarı düzeltin, hata
  yazmayana kadar sudo postfix check komutunu çalıştırın, sonra tekrar deneyin."
- `postfix.queue.said`
  EN: "Postfix said: {detail}"
  TR: "Postfix’in yanıtı: {detail}"

**Shown since 2026-10-10, and what is not.** The scheduled tasks screen shows
the verified cause and crontab's line (above); in the first form of this entry
the answer and the catalogue entries existed and the screen still showed its
one neutral sentence. A successful mail policy save that wrote and reloaded
Postfix keeps `mailpolicy.saved`. The entries `postfix.queue.unknown` and
`mailpolicy.unknown`, which told the owner to check that Postfix is running,
are no longer used by any screen. Not shown: a write to the scheduled tasks
that is refused because the crontab could not be read still answers with the
general sentence of `CURRENT_SETTINGS_UNREADABLE` in a toast. Neither change of
2026-10-10 was measured on a real service; in the browser run of that date
(fourth batch, above) the mail policy answers, the two configuration reload
answers and the four crontab answers were photographed against the mock. Seen
there and changed with the merge of 2026-10-10: the configuration reload answer
whose running settings are unknown (`restored_running_unknown`) stood on the
failure surface; it is an unknown, not a verified failure, and stands on the
attention surface now (`restored_unit_reload_failed`, where the server verified
its previous settings, stays on the failure surface). The line under both
answers was introduced as what the service said ("PostgreSQL says:") although
the unit's journal or the reload command printed it; it is introduced by
`dbconf.reloadSaid` now (EN: "Reported when {unit} was reloaded:" TR:
"{unit} yeniden yüklenirken bildirilen:"). Seen there and not
changed: the sentence for a not-reloaded answer that names no stage (an older
Agent) still names `sudo systemctl reload postfix`, against the rule at the top
of this entry.

**Not shown yet.** The scheduled tasks screen still shows its one neutral
sentence (`cron.unknown`); the `cron.unknown.*` entries above are in the
catalogue and the answer carries the cause and the line, but the screen does
not use them yet. A successful mail policy save that reloaded Postfix keeps
`mailpolicy.saved`. The entries `postfix.queue.unknown` and `mailpolicy.unknown`,
which told the owner to check that Postfix is running, are no longer used by
any screen.

### Service actions and mail certificate renewal: the answer is what the service shows, and an unknown result is said as unknown (2026-10-10)

Source state with component tests; the native measurement on real services is
pending, and nothing was observed on an installed server. See the resilience
contract entry of the same date for the paths and for what changed. This entry
holds the sentences.

**The rule the texts follow.** Start, Stop, Restart and Reload on the Services
page are answered with what was observed, not with the service manager's exit
status: on Ubuntu `postfix`, and on Debian and Ubuntu `postgresql`, that status
belongs to a unit that only groups the unit that runs the service. Three
answers exist. Done: the service was seen in the state that was asked for.
Verified failure: it was seen not to be, and the stage says where
(`check`: its own check refuses its configuration and nothing was sent;
`reload`, `start`, `stop`; `verify`: sent, and afterwards not in that state;
`command`: the service manager refused). Unknown: the action was sent and what
came of it could not be established. Unknown is never shown as done and never
as a failure.

**Who acts.** The server owner, on the server, with the one command the answer
carries (`vars.command`). For Postfix that is always one of Postfix's own
(`sudo postfix check`, `sudo postfix reload`, `sudo postfix status`), which
answer for the daemon on every platform; for another service it is `sudo
systemctl status <unit>` of the unit that runs it.

**How work resumes.** Nothing repeats by itself. After a verified failure the
owner corrects what the service names and repeats the action on the page.
After an unknown result the owner looks at the service's state first and
repeats the action only if it is still needed. A refused configuration stops
Start, Restart and Reload before anything is sent; Stop is always sent.

**The texts, Services page (`POST /api/v1/service/action`).** API sentences are
the `error` field, English only. The catalogue entries below are in
`web/src/i18n` since the merge of 2026-10-10 (the `err.*` ones in the shell
catalogue, the two `services.action.*` ones in `screens/server`) and are what
the screens show; `web/tests/service-action-outcome.test.mjs` compares them
with this entry. `{unit}` is the unit that was acted on,
`{command}` the command, `{detail}` the service's own line, `{owner_unit}` the
unit that runs the service when it is another one. Every API sentence except
the unknown one ends with: "The server owner runs the command shown to read the
service's own answer, corrects what it names, and then repeats this action
here; nothing repeats it automatically."

- `502 SERVICE_ACTION_FAILED`, reason `check`
  API: "Nothing was changed: the service's own check refuses its configuration,
  so the action was not carried out."
- `err.SERVICE_ACTION_FAILED.check`
  EN: "Nothing was changed: {unit} refuses its own configuration, so the action
  was not carried out. On the server, run {command} to see what it objects to,
  correct it, then repeat the action here."
  TR: "Hiçbir şey değiştirilmedi: {unit} kendi yapılandırmasını reddediyor; bu
  yüzden işlem yapılmadı. Sunucuda {command} komutunu çalıştırıp neye itiraz
  ettiğini görün, düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `reload`
  API: "The service was not reloaded and keeps running with the settings it
  had."
- `err.SERVICE_ACTION_FAILED.reload`
  EN: "{unit} was not reloaded and keeps running with the settings it had. On
  the server, run {command} to see why, correct it, then repeat the action
  here."
  TR: "{unit} yeniden yüklenmedi ve önceki ayarlarıyla çalışmayı sürdürüyor.
  Sunucuda {command} komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi
  burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `start`
  API: "The service did not start, or did not stay running."
- `err.SERVICE_ACTION_FAILED.start`
  EN: "{unit} did not start, or did not stay running. On the server, run
  {command} to see why, correct it, then repeat the action here."
  TR: "{unit} başlamadı ya da çalışır durumda kalmadı. Sunucuda {command}
  komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `stop`
  API: "The service did not stop: its daemon is still running."
- `err.SERVICE_ACTION_FAILED.stop`
  EN: "{unit} did not stop: it is still running. On the server, run {command}
  to see its state, then repeat the action here."
  TR: "{unit} durmadı: hâlâ çalışıyor. Sunucuda {command} komutuyla durumunu
  görün, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `verify`
  API: "The action was sent, but afterwards the service's daemon is not in the
  state that was asked for."
- `err.SERVICE_ACTION_FAILED.verify`
  EN: "The action was sent, but {unit} is not in the state that was asked for.
  On the server, run {command} to see its state, correct the cause, then repeat
  the action here."
  TR: "İşlem gönderildi ancak {unit} istenen durumda değil. Sunucuda {command}
  komutuyla durumunu görün, nedeni düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `command`
  API: "The server's service manager did not carry out the action."
- `err.SERVICE_ACTION_FAILED.command`
  EN: "The server's service manager did not carry out the action on {unit}. On
  the server, run {command} to see why, correct it, then repeat the action
  here."
  TR: "Sunucunun hizmet yöneticisi {unit} üzerindeki işlemi yapmadı. Sunucuda
  {command} komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada
  yineleyin."
- `502 SERVICE_ACTION_FAILED`, no reason (a stage this Panel does not know)
  API: "The action did not take effect."
- `err.SERVICE_ACTION_FAILED`
  EN: "The action on {unit} did not take effect. On the server, run {command}
  to see why, correct it, then repeat the action here."
  TR: "{unit} üzerindeki işlem etkili olmadı. Sunucuda {command} komutunu
  çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_UNKNOWN`
  API: "The action was sent, but what came of it could not be verified, so it
  is not reported as done. This is not a verified failure: the service may
  already be in the state that was asked for. The server owner runs the command
  shown to see the service's state, and repeats this action here only if it is
  still needed; nothing repeats it automatically."
- `err.SERVICE_ACTION_UNKNOWN`
  EN: "The action was sent, but what came of it could not be verified, so it is
  not shown as done. This is not a verified failure: {unit} may already be in
  the state you asked for. On the server, run {command} to see its state, and
  repeat the action here only if it is still needed."
  TR: "İşlem gönderildi ancak sonucu doğrulanamadı; bu yüzden yapıldı diye
  gösterilmiyor. Bu doğrulanmış bir hata değildir: {unit} istediğiniz duruma
  zaten gelmiş olabilir. Sunucuda {command} komutuyla durumunu görün; işlemi
  yalnız hâlâ gerekiyorsa burada yineleyin."
- `services.action.said` (shown under either answer when `vars.detail` is
  present)
  EN: "The service said: {detail}"
  TR: "Hizmetin yanıtı: {detail}"
- `services.action.ownerUnit` (when `vars.owner_unit` is present)
  EN: "The service itself runs as {owner_unit}; {unit} only groups it."
  TR: "Hizmetin kendisi {owner_unit} olarak çalışır; {unit} yalnız onu
  gruplar."

**The texts, mail certificate renewal.** No screen: the independent renewal
helper prints the sentence to its journal (`journalctl -u
celikpanel-mail-renewal.service`), the Agent writes it to its log. English
only. `<service>` is Postfix or Dovecot. They replace, for these two causes
only, the general "mail certificate activation paused" sentence.

- The service's listeners still present another certificate after its reload
  (verified):
  "mail certificate activation is not complete: <service> was asked to reload,
  but its listeners on this server still present another certificate than the
  selected one; the server owner runs `postfix reload` as root, reads what it
  prints and the service's log, and the renewal then retries the same
  operation, by itself up to its recorded limit and after that with the
  continuation command it prints; the renewed certificate stays selected, and
  Postfix/Dovecot settings and certificate evidence are preserved"
  (for Dovecot the command is `doveadm reload`).
- No listener of the service answered with TLS (unknown):
  "mail certificate activation is not confirmed: no TLS listener of <service>
  answered on this server, so which certificate it presents is unknown and
  nothing is reported as activated; the server owner checks that <service> is
  running and listening (`postfix status`, which answers for the daemon where
  `systemctl is-active postfix` may answer for a wrapper unit), starts it if it
  is stopped, and the renewal then retries the same operation, by itself up to
  its recorded limit and after that with the continuation command it prints;
  the renewed certificate stays selected, and Postfix/Dovecot settings and
  certificate evidence are preserved" (for Dovecot the check is `systemctl
  status dovecot`).

**A server already enrolled in independent renewal.** Its renewal helper is the
one it was enrolled with and does not print these sentences; on Ubuntu it can
record a renewal as activated although Postfix did not reload. Until the helper
can be moved to a newer one (designed, not implemented; resilience contract),
the owner compares after a renewal, on the server:

    openssl s_client -connect localhost:465 </dev/null 2>/dev/null | openssl x509 -noout -fingerprint -sha256
    openssl x509 -noout -fingerprint -sha256 -in /etc/ssl/celikpanel/_mail/host/current/fullchain.pem

The first line is the certificate Postfix presents, the second the one that is
installed. When they differ, `sudo postfix reload` makes Postfix take it and
prints what Postfix objects to if it cannot. Both lines only read.

**On screen (2026-10-10).** Every component's page (`ServiceShell`, the generic
page of `ComponentDetail` included) and the components list (`ServiceList`)
keep the answer on the page, above the content, until it is closed or another
action is taken: the sentence with the command set apart, the service's line
under `services.action.said`, `services.action.ownerUnit` where another unit
runs the service, and Close. A verified failure stands on the failure surface,
an unknown result on the attention surface. The state is read again under it,
because the action may have changed it. Before, both were toasts that left
after five seconds. An action that gets no answer at all is no longer a red
toast either.

**Not shown yet.** The mail certificate status on the Panel does not show an
open renewal's cause; it is in the helper's journal and the Agent's log.

### A change is sent once and answered once: the request-identity refusals (2026-10-10)

Source state with component tests; no installed server and no native run. See
D-029 and the resilience contract entry of the same date. Eight state-changing
routes now run a request once however often it arrives; these are the sentences
a screen shows when the answer is not the change's own result.

**Who acts, in every case below.** The person at the screen. Nothing is sent or
run again by itself, with one exception that changes nothing twice: after a
lost answer the page asks once more for the same answer, under the same
identity, and the server answers it from the first run.

**How the work goes on.** Reloading the page only reads. A change made again
after a reload is a new request with a new identity.

**The texts.** Keys are in the shell catalogue (`web/src/i18n/en.ts`,
`web/src/i18n/tr.ts`), because any screen can receive them. The server's own
English message for each code is the same sentence, for anything that reads the
API directly.

- *The page is older than the Panel (refused before any change; `428`).*
  - `err.REQUEST_ID_REQUIRED`:
    - EN: "This page was opened before CelikPanel was updated, so the server
      did not accept the change and nothing was changed. Reload the page, then
      make the change again."
    - TR: "Bu sayfa CelikPanel güncellenmeden önce açılmış; bu yüzden sunucu
      değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı yeniden
      yükleyin, sonra değişikliği yeniden yapın."
  - The server's message adds, for a client that is not the page: "(A client
    that is not the CelikPanel page sends the header X-CelikPanel-Request-Id:
    32 lowercase hexadecimal characters, a new value for each action.)"
- *The identifier was already used for something else (refused before any
  change; `409`).*
  - `err.REQUEST_ID_REUSED`:
    - EN: "This change was sent with an identifier the server already used for
      a different change, so it was not carried out. Reload the page, then make
      the change again."
    - TR: "Bu değişiklik, sunucunun başka bir değişiklik için zaten kullandığı
      bir kimlikle gönderildi; bu yüzden uygulanmadı. Sayfayı yeniden yükleyin,
      sonra değişikliği yeniden yapın."
- *The first arrival is still running (waiting; `409`).*
  - `err.REQUEST_IN_PROGRESS`:
    - EN: "This change is still running on the server. It was not started a
      second time. Wait a little, then reload the page to see the result; do
      not send it again."
    - TR: "Bu değişiklik sunucuda hâlâ sürüyor. İkinci kez başlatılmadı. Biraz
      bekleyin, sonra sonucu görmek için sayfayı yeniden yükleyin; değişikliği
      yeniden göndermeyin."
- *The Panel stopped while the change was running (unknown result; `409`).*
  - `err.REQUEST_OUTCOME_UNKNOWN`:
    - EN: "CelikPanel restarted or failed while this change was running, so it
      is not known whether the change was completed. It will not be run again
      by itself. Reload the page and check the current state; make the change
      again only if it is missing."
    - TR: "CelikPanel bu değişiklik sürerken yeniden başladı ya da hata verdi;
      bu yüzden değişikliğin tamamlanıp tamamlanmadığı bilinmiyor.
      Kendiliğinden yeniden çalıştırılmayacak. Sayfayı yeniden yükleyip mevcut
      durumu kontrol edin; değişikliği yalnızca eksikse yeniden yapın."
- *The change was made; its one-time result is not kept (known result; `409`).*
  - `err.REQUEST_COMPLETED_RESULT_NOT_RETAINED`:
    - EN: "This change was already made; it was not made a second time. Its
      result was shown only once and is not kept. Reload the page to see the
      current state; if you still need what was shown once (a password or a
      configuration file), create a new one."
    - TR: "Bu değişiklik zaten yapıldı; ikinci kez yapılmadı. Sonucu yalnızca
      bir kez gösterildi ve saklanmıyor. Mevcut durumu görmek için sayfayı
      yeniden yükleyin; bir kez gösterilene (parola ya da yapılandırma dosyası)
      hâlâ ihtiyacınız varsa yenisini oluşturun."
  - `err.REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed` (the first attempt ended
    with an error and that answer is not kept; a verified failure whose detail
    is gone):
    - EN: "This change already ended with an error, and that answer is not
      kept; it was not tried a second time. Reload the page and check the
      current state; make the change again only if it is missing."
    - TR: "Bu değişiklik daha önce hatayla sonuçlandı ve o yanıt saklanmıyor;
      ikinci kez denenmedi. Sayfayı yeniden yükleyip mevcut durumu kontrol
      edin; değişikliği yalnızca eksikse yeniden yapın."
  - On a database server's own account the screen treats the first of these two
    as the success it was: that answer carries no password (it is read with
    "Show password"), so nothing is missing.
- *Another restore of the same domain is running (refused before any change;
  `409`).*
  - `err.BACKUP_RESTORE_IN_PROGRESS`:
    - EN: "Another restore of this domain is still running, so this one was not
      started and changed nothing. Wait for it to finish and check the site;
      restore again only if it is still needed."
    - TR: "Bu alan adının başka bir geri yüklemesi hâlâ sürüyor; bu yüzden bu
      geri yükleme başlatılmadı ve hiçbir şeyi değiştirmedi. Bitmesini bekleyip
      siteyi kontrol edin; yalnızca hâlâ gerekiyorsa yeniden geri yükleyin."
- *cPanel import: the result is still not known after the second asking
  (unknown result). Changed text; key in `web/src/i18n/screens`.*
  - `import.unknown.body`:
    - EN: "This page did not get the result of the import: the answer from the
      server did not arrive, and asking once more for it did not bring the
      result either. The import may have run completely, in part or not at
      all, or may still be running. Asking again never starts it a second
      time, and starting it again is not offered until you have checked.
      Check whether {domain} is on this server now; checking only reads."
    - TR: "Bu sayfa içe aktarımın sonucunu alamadı: sunucunun yanıtı ulaşmadı ve
      yanıt bir kez daha istendiğinde de sonuç gelmedi. İçe aktarım tamamen,
      kısmen çalışmış ya da hiç çalışmamış olabilir; hâlâ sürüyor da olabilir.
      Yeniden sormak onu ikinci kez başlatmaz; siz kontrol edene dek yeniden
      başlatma da sunulmaz. {domain} alan adının şu an bu sunucuda olup
      olmadığını kontrol edin; kontrol yalnız okur."
  - "Start import again", offered only after the check found the domain absent,
    now sends the same request under the same identity: the server answers it
    from the first run if there was one, and runs it only if it never arrived.

**One behaviour for a result that is not known (merged with the fourth batch,
2026-10-10).** The request identity and the lost-answer handling of the fourth
batch (entry of 2026-10-09 above) were written side by side. Together they are
this.

- *What a lost answer is* is defined once (`answerWasLost` in
  `web/src/lib/requestIdentity.ts`): no answer at all, or an answer with status
  408, 429, 502, 503 or 504 that is not JSON, which is a gateway speaking in
  the Panel's place. The Panel's own refusal is JSON also when its status is
  one of these (an Agent that could not be reached is a 502 with a sentence);
  it is the answer, and it is shown. Before the merge the interceptor asked
  again for it too, and on the two routes whose answers are never stored the
  screen then showed "already ended with an error, and that answer is not kept"
  in place of the Panel's sentence.
- *On the eight routes* the interceptor asks once more, 1.5 seconds later,
  under the same identity. When that is answered, the screen shows the change's
  own result and nothing else.
- *When it is not answered either*, or when the Panel answers
  `REQUEST_OUTCOME_UNKNOWN` or `REQUEST_IN_PROGRESS`, the result is not known,
  and all eight screens do what the fourth batch does for a change without an
  identity: a notice in place on the attention surface, what the change acts on
  read again (a read only), and every control that changes or removes off until
  that read has answered. Nothing of it is a toast and nothing is drawn as a
  failure.
- *The notice says which happened.* For a change without an identity it still
  says that nothing was sent a second time (`common.resultUnknown*`,
  unchanged). For one of the eight it never says that. It has two sentences:
  what happened, then what the state that was read again shows. Once that state
  shows the change, one sentence is left and the form that sent it is closed.
- *A change found made by that read, whose result was shown to nobody* (a VPN
  device; a database with a new user) is said with the sentence of the
  status-only answer below, not with "it was made" alone.

**The texts of that notice.** Shell catalogue.

- *What happened.*
  - `common.lostAsked`:
    - EN: "No answer arrived for this change, and asking the server once more
      for the same answer brought none either, so it is not known whether the
      change was made. Asking again never makes the change a second time."
    - TR: "Bu değişikliğin yanıtı ulaşmadı; sunucudan aynı yanıt bir kez daha
      istendiğinde de gelmedi. Bu yüzden değişikliğin yapılıp yapılmadığı
      bilinmiyor. Yeniden sormak değişikliği asla ikinci kez yapmaz."
  - `common.lostInterrupted`:
    - EN: "CelikPanel restarted or failed while this change was running, so it
      is not known whether the change was completed. It will not be run again
      by itself."
    - TR: "CelikPanel bu değişiklik sürerken yeniden başladı ya da hata verdi;
      bu yüzden değişikliğin tamamlanıp tamamlanmadığı bilinmiyor.
      Kendiliğinden yeniden çalıştırılmayacak."
  - `common.lostRunning`:
    - EN: "This change is still running on the server, so its result is not
      known yet. It was not started a second time."
    - TR: "Bu değişiklik sunucuda hâlâ sürüyor; bu yüzden sonucu henüz
      bilinmiyor. İkinci kez başlatılmadı."

- *What the state that was read again shows.*
  - `common.lostStateReading`:
    - EN: "What is shown here is being read again. Controls that change or
      remove something stay off until it has been read."
    - TR: "Burada gösterilen yeniden okunuyor. Bir şeyi değiştiren ya da
      kaldıran denetimler, okuma bitene dek kapalı kalır."
  - `common.lostStateRead`:
    - EN: "What is shown here was read again at {time}. Check it before making
      the change again; if the change may still be running, check again in a
      little while."
    - TR: "Burada gösterilen, saat {time} itibarıyla yeniden okundu.
      Değişikliği yeniden yapmadan önce ona bakın; değişiklik hâlâ sürüyor
      olabilirse biraz sonra tekrar kontrol edin."
  - `common.lostStateUnread`:
    - EN: "The current state could not be read again, so controls that change
      or remove something stay off. Check again."
    - TR: "Güncel durum yeniden okunamadı; bu yüzden bir şeyi değiştiren ya da
      kaldıran denetimler kapalı kalıyor. Tekrar kontrol edin."
  - `common.lostStateNotMade`:
    - EN: "What was read again at {time} does not show the change, so it is not
      known to have been made. What you entered is still here. If the server is
      still working on it, the change can appear later: check again before
      sending it a second time."
    - TR: "Saat {time} itibarıyla yeniden okunan durum değişikliği göstermiyor;
      bu yüzden yapıldığı bilinmiyor. Girdikleriniz hâlâ burada. Sunucu hâlâ
      üzerinde çalışıyorsa değişiklik sonradan görünebilir: ikinci kez
      göndermeden önce tekrar kontrol edin."
  - `common.lostStateMade`:
    - EN: "The answer to this change did not reach this page, but what was read
      again at {time} shows the change, so it was made. Nothing needs to be
      sent again."
    - TR: "Bu değişikliğin yanıtı bu sayfaya ulaşmadı; ancak saat {time}
      itibarıyla yeniden okunan durum değişikliği gösteriyor, yani yapıldı.
      Hiçbir şeyin yeniden gönderilmesi gerekmiyor."

**A change that was made, whose one-time result is not kept (`409
REQUEST_COMPLETED_RESULT_NOT_RETAINED` without a reason).** Said in place, on
the attention surface, until the person closes it; the list is read again so
that what was made is there.

- *A VPN device.*
  - `vpn.configNotShown`:
    - EN: "The device {name} was added; it was not added a second time. Its
      configuration did not reach this page, and a configuration is shown only
      once and is not stored, so it cannot be shown again. If {name} is in the
      list below, remove it; then add the device again to get a new
      configuration."
    - TR: "{name} cihazı eklendi; ikinci kez eklenmedi. Yapılandırması bu
      sayfaya ulaşmadı; yapılandırma yalnızca bir kez gösterilir ve saklanmaz,
      bu yüzden yeniden gösterilemez. {name} aşağıdaki listedeyse onu kaldırın;
      sonra yeni bir yapılandırma almak için cihazı yeniden ekleyin."

- *A database on a database server, with a new user.*
  - `databases.passwordNotShown`:
    - EN: "The database {name} was created; it was not created a second time.
      The answer that carried the password of its user {user} did not reach
      this page and is not kept, so the password cannot be shown again. It is
      the password you entered in the form. If you no longer have it, set a new
      password for {user} on the database server itself; this page has no
      control for that yet."
    - TR: "{name} veritabanı oluşturuldu; ikinci kez oluşturulmadı. {user}
      kullanıcısının parolasını taşıyan yanıt bu sayfaya ulaşmadı ve
      saklanmıyor; bu yüzden parola yeniden gösterilemez. Parola, formda
      girdiğiniz paroladır. Artık elinizde değilse {user} için veritabanı
      sunucusunun kendisinde yeni bir parola belirleyin; bu sayfada bunun için
      henüz bir denetim yok."
  - The Panel has no control that sets a database user's password, and the
    sentence says so. D-029 names "a minted database password is set again" as
    a consequence; the control for it is not built.

- *The Panel's own account on a database engine:* the success it was, as above.
- *With the reason `failed`:*
  `err.REQUEST_COMPLETED_RESULT_NOT_RETAINED.failed`, as above.

**The import page** keeps its own notice, with the check of the domain. Its
body says which happened: `import.unknown.body` (above) when the second asking
brought no answer either, and

- `import.unknown.bodyRunning`:
  - EN: "The import is still running on the server, so this page does not have
    its result yet. It was not started a second time. Asking again never starts
    it a second time, and starting it again is not offered until you have
    checked. Wait a little, then check whether {domain} is on this server now;
    checking only reads."
  - TR: "İçe aktarım sunucuda hâlâ sürüyor; bu yüzden bu sayfa sonucunu henüz
    alamadı. İkinci kez başlatılmadı. Yeniden sormak onu ikinci kez başlatmaz;
    siz kontrol edene dek yeniden başlatma da sunulmaz. Biraz bekleyin, sonra
    {domain} alan adının şu an bu sunucuda olup olmadığını kontrol edin;
    kontrol yalnız okur."
- `import.unknown.bodyInterrupted`:
  - EN: "CelikPanel restarted or failed while the import was running, so it is
    not known whether it ran completely, in part or not at all. It will not be
    run again by itself, and starting it again is not offered until you have
    checked. Check whether {domain} is on this server now; checking only
    reads."
  - TR: "CelikPanel içe aktarım sürerken yeniden başladı ya da hata verdi; bu
    yüzden içe aktarımın tamamen mi, kısmen mi çalıştığı, yoksa hiç çalışmadığı
    bilinmiyor. Kendiliğinden yeniden çalıştırılmayacak; siz kontrol edene dek
    yeniden başlatma da sunulmaz. {domain} alan adının şu an bu sunucuda olup
    olmadığını kontrol edin; kontrol yalnız okur."

After `REQUEST_OUTCOME_UNKNOWN` a later start is a new request; in the other
two cases it asks for the same request again.

**Where each of the eight is drawn.** Backup and restore: the Backups panel of
a domain (`DomainBackupManager`). Certificate: the SSL/TLS tab
(`DomainSSLSettings`); the certificate is read again and the tab's controls are
off meanwhile. A domain's database: `DomainDatabaseManager`, which asks the
list whether it names the database. A database on a server: the dialog
(`AddDatabaseModalV2`) while it is open and the Databases page after it; it
asks both lists. The engine account: its strip (`DatabaseAccountStrip`). A VPN
device: the Devices tab (`VPNPage`), which asks the devices whether they name
it. Import: `ImportPage`.

**A page that predates the update (`428`).** It runs the old code: it sends no
header and has no entry for the code, so it shows the server's English
sentence, which ends with the reload. The entry `err.REQUEST_ID_REQUIRED` is
what the current page shows if it ever receives the code. The answer is a
refusal, not an unknown result, and nothing was changed. No button reloads the
page; the sentence asks for it.

**Limits.** The dialog for a database on a server and the form for a domain's
database are in English only, apart from these notices, as before. A refusal
they have no words of their own for is shown through the catalogue when its
code has an entry, else as the server's sentence. Inspected in a real Chrome
against a loopback mock that keeps the guard's contract (resilience contract,
entry of this date); not on a real Panel.

### After the second native measurement: a failed reload says only what was verified, an import says what it imported, a certificate failure says its kind (2026-10-11)

Source state with component tests; the native re-run is pending and nothing was
observed on an installed server. What was measured and what changed is in the
resilience contract entry of the same date. This entry keeps the sentences.

**The rule the texts follow.** A sentence says which settings a service runs
with only when the service was asked. A reload that the unit reported as failed
says nothing about that by itself: the command may have signalled the daemon
before it failed. A daemon that is not running is an unmet prerequisite, not a
failed reload. An import whose every step has ended is complete or a verified
partial result; it is never "pending". A certificate request that did not issue
names the kind of failure only when certbot's own output states it.

**Replaced.** The sentence of `err.SERVICE_ACTION_FAILED.reload` and of the
API's reason `reload` recorded in the entry of 2026-10-10 ("... and keeps
running with the settings it had") is no longer shown. The ones below are.

**A reload that failed (`POST /api/v1/service/action`).**

- `502 SERVICE_ACTION_FAILED`, reason `reload`
  API: "The service reported that the reload failed. CelikPanel cannot read
  from this service which settings it is running with now, so it says neither
  that it kept the settings it had nor that it took the files on disk. The
  server owner runs the command shown to read the service's own answer,
  corrects what it names, and then repeats this action here; nothing repeats it
  automatically."
- `err.SERVICE_ACTION_FAILED.reload`
  EN: "{unit} reported that the reload failed. CelikPanel cannot read from
  {unit} which settings it is running with now, so this page says neither that
  it kept the settings it had nor that it took the files on disk. On the
  server, run {command} to see why, correct it, then repeat the action here."
  TR: "{unit} yeniden yüklemenin başarısız olduğunu bildirdi. CelikPanel,
  {unit} hizmetinin şu an hangi ayarlarla çalıştığını ondan okuyamıyor; bu
  yüzden bu sayfa ne önceki ayarlarını koruduğunu ne de diskteki dosyaları
  aldığını söylüyor. Sunucuda {command} komutunu çalıştırıp nedenini görün,
  düzeltin, sonra işlemi burada yineleyin."
- `502 SERVICE_ACTION_FAILED`, reason `reload_reread` (PostgreSQL)
  API: "The unit reported the reload as failed, but PostgreSQL itself re-read
  its configuration files after it: the settings in the files on disk are in
  effect now, except those that need a restart. A step of the unit's own reload
  command failed after the server had been signalled. The server owner runs the
  command shown to see which step, and corrects it so that the next reload is
  reported as it went; the reload does not need to be repeated for these
  settings."
- `err.SERVICE_ACTION_FAILED.reload_reread`
  EN: "The reload of {unit} was reported as failed, but PostgreSQL itself
  re-read its configuration files after it: the settings in the files on disk
  are in effect now, except those that need a restart. A step of the unit’s own
  reload command failed after the server had been signalled. On the server, run
  {command} to see which step, and correct it so that the next reload is
  reported as it went. The reload does not need to be repeated for these
  settings."
  TR: "{unit} için yeniden yükleme başarısız diye bildirildi, ancak PostgreSQL
  yapılandırma dosyalarını bundan sonra kendisi yeniden okudu: diskteki
  dosyalardaki ayarlar, yeniden başlatma gerektirenler dışında, şu an
  yürürlükte. Birimin kendi yeniden yükleme komutunun bir adımı, sunucuya
  sinyal gönderildikten sonra başarısız oldu. Sunucuda {command} komutunu
  çalıştırıp hangi adım olduğunu görün ve sonraki yeniden yüklemenin olduğu
  gibi bildirilmesi için düzeltin. Bu ayarlar için yeniden yüklemeyi
  yinelemeniz gerekmez."
- `502 SERVICE_ACTION_FAILED`, reason `reload_not_reread` (PostgreSQL)
  API: "The reload failed and PostgreSQL did not re-read its configuration
  files: it is running with the settings it had before. The server owner runs
  the command shown to read the service's own answer, corrects what it names,
  and then repeats this action here; nothing repeats it automatically."
- `err.SERVICE_ACTION_FAILED.reload_not_reread`
  EN: "The reload of {unit} failed and PostgreSQL did not re-read its
  configuration files: it is running with the settings it had before. On the
  server, run {command} to see why, correct it, then repeat the action here."
  TR: "{unit} için yeniden yükleme başarısız oldu ve PostgreSQL yapılandırma
  dosyalarını yeniden okumadı: önceki ayarlarıyla çalışıyor. Sunucuda {command}
  komutunu çalıştırıp nedenini görün, düzeltin, sonra işlemi burada yineleyin."
- `409 SERVICE_ACTION_FAILED`, reason `not_running`
  API: "The service is not running, so there was nothing to reload and nothing
  was changed. If it should run, the server owner starts it with Start on this
  page; it reads its configuration files when it starts."
- `err.SERVICE_ACTION_FAILED.not_running`
  EN: "{unit} is not running, so there was nothing to reload and nothing was
  changed. If it should run, use Start here; it reads its configuration files
  when it starts. To see its state on the server, run {command}."
  TR: "{unit} çalışmıyor; bu yüzden yeniden yüklenecek bir şey yoktu ve hiçbir
  şey değiştirilmedi. Çalışması gerekiyorsa burada Başlat’ı kullanın; başlarken
  yapılandırma dosyalarını okur. Sunucudaki durumunu görmek için {command}
  komutunu çalıştırın."

**The cPanel import (`POST /api/v1/import/cpanel/inspect`, `.../apply`).**

- `200`, `status: partial`, `code: IMPORT_PARTIAL`, field `message`
  API: "The import ended with a part of the archive not imported, and it does
  not continue by itself. Imported: {imported}. Not imported: {not imported}.
  The domain {domain} was created and is kept; it is left marked as not
  finished. The reason of each part that was not imported is in its step below.
  The server owner either adds the missing parts by hand on the domain's own
  pages, or removes {domain} on the Domains page, corrects what the step names
  and imports the archive again; an import into a domain that already exists is
  refused, so nothing is imported twice."
- `502 IMPORT_SITE_NOT_CREATED`
  API: "The import did not start: the site for this domain could not be created
  on this server, so no file, mailbox, DNS record or database of the archive
  was imported. Whether a part of the new site itself was left behind is not
  known from this answer: open Domains to see whether the domain is listed. The
  server owner reads the step that failed on the server with sudo journalctl -u
  celikpanel-agent, corrects it, and starts the import again; nothing starts it
  again automatically."
- step `mail:<address>`, field `detail`
  API: "not imported: the archive holds no password for this mailbox"
- `import.mailPasswords.all`
  EN: "Each of these mailboxes has a password in the archive, and the import
  keeps it. The password itself is never shown."
  TR: "Bu posta kutularının her birinin arşivde bir parolası var ve içe aktarım
  onu korur. Parolanın kendisi hiçbir zaman gösterilmez."
- `import.mailPasswords.some`
  EN: "{kept} of {total} mailboxes have a password in the archive, and the
  import keeps it. The password itself is never shown. The archive holds no
  password for {missing}, so the import does not create them: create them on
  the domain’s mail page afterwards, with a new password."
  TR: "{total} posta kutusundan {kept} tanesinin arşivde parolası var ve içe
  aktarım onu korur. Parolanın kendisi hiçbir zaman gösterilmez. Arşivde
  şunların parolası yok, bu yüzden içe aktarım onları oluşturmaz: {missing}.
  Bunları sonradan alan adının posta sayfasında yeni bir parolayla oluşturun."
- `import.inspectUnreadable`
  EN: "The server’s answer about the archive could not be read, so there is no
  preview. Inspecting only reads the archive; nothing was changed. Try again."
  TR: "Sunucunun arşivle ilgili yanıtı okunamadı; bu yüzden önizleme yok.
  İnceleme arşivi yalnız okur; hiçbir şey değiştirilmedi. Tekrar deneyin."
- `import.result.complete`
  EN: "Every part you chose was imported, and {domain} is in service."
  TR: "Seçtiğiniz her parça içe aktarıldı ve {domain} hizmette."
- `import.partial.title`
  EN: "{domain} was imported in part"
  TR: "{domain} kısmen içe aktarıldı"
- `import.partial.body`
  EN: "The import has ended and does not continue by itself. The parts listed
  as not imported are missing; the others are on this server. {domain} was
  created and is kept, marked as not finished."
  TR: "İçe aktarım bitti ve kendiliğinden sürmez. İçe aktarılmadı diye
  listelenen parçalar eksik; diğerleri bu sunucuda. {domain} oluşturuldu ve
  korunuyor; tamamlanmadı olarak işaretli."
- `import.partial.unfinished`
  EN: "Every part was imported, but {domain} could not be marked as finished.
  The import has ended and does not continue by itself."
  TR: "Her parça içe aktarıldı, ancak {domain} tamamlandı olarak
  işaretlenemedi. İçe aktarım bitti ve kendiliğinden sürmez."
- `import.partial.imported`
  EN: "Imported"
  TR: "İçe aktarıldı"
- `import.partial.notImported`
  EN: "Not imported"
  TR: "İçe aktarılmadı"
- `import.partial.next`
  EN: "To finish, either add the missing parts by hand on the pages of
  {domain}, or remove {domain} on the Domains page, correct what each step
  below names, and import the archive again. An import into a domain that
  already exists is refused, so nothing is imported twice."
  TR: "Tamamlamak için ya eksik parçaları {domain} alan adının sayfalarında
  elle ekleyin ya da {domain} alan adını Alan Adları sayfasında kaldırın,
  aşağıdaki her adımın adını verdiği şeyi düzeltin ve arşivi yeniden içe
  aktarın. Zaten var olan bir alan adına içe aktarım reddedilir; bu yüzden
  hiçbir şey iki kez içe aktarılmaz."
- `import.partial.domains`
  EN: "Open Domains"
  TR: "Alan adlarını aç"
- `import.stepsTitle`
  EN: "Each step"
  TR: "Adım adım"
- `import.step.done`
  EN: "Imported"
  TR: "İçe aktarıldı"
- `import.step.notDone`
  EN: "Not imported"
  TR: "İçe aktarılmadı"
- `import.part.domain`
  EN: "Domain and site"
  TR: "Alan adı ve site"
- `import.part.files`
  EN: "Website files"
  TR: "Site dosyaları"
- `import.part.mail`
  EN: "Mail accounts"
  TR: "Posta hesapları"
- `import.part.mailbox`
  EN: "Mailbox {name}"
  TR: "{name} posta kutusu"
- `import.part.forwarders`
  EN: "Forwarders"
  TR: "Yönlendirmeler"
- `import.part.forwarder`
  EN: "Forwarder {name}"
  TR: "{name} yönlendirmesi"
- `import.part.dns`
  EN: "DNS records"
  TR: "DNS kayıtları"
- `import.part.databases`
  EN: "Databases"
  TR: "Veritabanları"
- `import.part.database`
  EN: "Database {name}"
  TR: "{name} veritabanı"
- `import.part.finalize`
  EN: "Marking the domain as finished"
  TR: "Alan adını tamamlandı olarak işaretleme"
- `import.detail.noPassword`
  EN: "Not imported: the archive holds no password for this mailbox. Create it
  on the domain’s mail page with a new password."
  TR: "İçe aktarılmadı: arşivde bu posta kutusunun parolası yok. Onu alan
  adının posta sayfasında yeni bir parolayla oluşturun."
- `import.siteNotCreated`
  EN: "The import did not start: the site for this domain could not be created
  on this server, so no file, mailbox, DNS record or database of the archive
  was imported. Whether a part of the new site itself was left behind is not
  known here: open Domains to see whether the domain is listed. On the server,
  sudo journalctl -u celikpanel-agent shows the step that failed; correct it,
  then start the import again. Nothing starts it again automatically."
  TR: "İçe aktarım başlamadı: bu alan adının sitesi bu sunucuda oluşturulamadı;
  bu yüzden arşivden hiçbir dosya, posta kutusu, DNS kaydı ya da veritabanı içe
  aktarılmadı. Yeni sitenin kendisinden bir parçanın geride kalıp kalmadığı
  burada bilinmiyor: alan adının listede olup olmadığını görmek için Alan
  Adları sayfasını açın. Sunucuda sudo journalctl -u celikpanel-agent komutu
  başarısız olan adımı gösterir; onu düzeltin, sonra içe aktarımı yeniden
  başlatın. Hiçbir şey onu kendiliğinden yeniden başlatmaz."

**A certificate request certbot did not fulfil (`POST /api/v1/domains/{id}/ssl/letsencrypt`).**

- `502 CERTIFICATE_ISSUE_FAILED`, reason `authority_unreachable`
  API: "No certificate was issued: this server could not reach the certificate
  authority, so no request was placed with it. The server owner checks that
  this server can open HTTPS connections to the internet (DNS resolution,
  outbound port 443, the system clock), then requests the certificate here
  again."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `validation`
  API: "No certificate was issued: the certificate authority could not validate
  one of the names. Each name of the site must resolve publicly to this server
  and answer on port 80 from the internet. The domain's owner corrects the DNS
  records (or the firewall in front of this server), waits until public DNS
  shows them, then requests the certificate here again."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `rate_limited`
  API: "No certificate was issued: the certificate authority refused the
  request because one of its limits was reached. A new request before the limit
  resets is refused the same way and counts against it. The line from certbot
  names the limit and, when the authority says so, when it resets; request the
  certificate here again after that time."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `timeout`
  API: "No certificate was issued: certbot did not finish within the time
  allowed and was stopped. Why it took that long is not known from this answer.
  The server owner reads /var/log/celikpanel/certbot/letsencrypt.log on the
  server, then requests the certificate here again."
- `502 CERTIFICATE_ISSUE_FAILED`, reason `tool`
  API: "No certificate was issued: certbot ended with an error. Which step
  failed is not classified here; the line from certbot says what it reported.
  The server owner reads /var/log/celikpanel/certbot/letsencrypt.log on the
  server, corrects what it names, then requests the certificate here again."
- then, when the site already had a certificate
  API: "The certificate this site already had is still in place and keeps
  serving."
- or, when it had none
  API: "The site has no certificate from this request and is served as before."
- and always last
  API: "Nothing asks again automatically."
- `ssl.issueFailure.authority_unreachable`
  EN: "No certificate was issued for {domain}: this server could not reach the
  certificate authority, so no request was placed with it. The site is served
  as before, with the certificate it already had if it had one. Check that this
  server can open HTTPS connections to the internet (DNS resolution, outbound
  port 443, the system clock), then request the certificate here again. Nothing
  asks again automatically."
  TR: "{domain} için sertifika çıkarılmadı: bu sunucu sertifika otoritesine
  ulaşamadı; bu yüzden otoriteye bir istek iletilmedi. Site eskisi gibi
  sunuluyor; önceden bir sertifikası varsa o yerinde duruyor. Bu sunucunun
  internete HTTPS bağlantısı açabildiğini denetleyin (DNS çözümleme, giden 443
  numaralı port, sistem saati), sonra sertifikayı buradan yeniden isteyin.
  Hiçbir şey kendiliğinden yeniden istemez."
- `ssl.issueFailure.validation`
  EN: "No certificate was issued for {domain}: the certificate authority could
  not validate one of the names. The site is served as before, with the
  certificate it already had if it had one. Each name must resolve publicly to
  this server and answer on port 80 from the internet. Correct the DNS records
  (or the firewall in front of this server), wait until public DNS shows them,
  then request the certificate here again. Nothing asks again automatically."
  TR: "{domain} için sertifika çıkarılmadı: sertifika otoritesi adlardan birini
  doğrulayamadı. Site eskisi gibi sunuluyor; önceden bir sertifikası varsa o
  yerinde duruyor. Her ad genel DNS’te bu sunucuya çözülmeli ve internetten 80
  numaralı portta yanıt vermelidir. DNS kayıtlarını (ya da bu sunucunun
  önündeki güvenlik duvarını) düzeltin, genel DNS onları gösterene dek
  bekleyin, sonra sertifikayı buradan yeniden isteyin. Hiçbir şey kendiliğinden
  yeniden istemez."
- `ssl.issueFailure.rate_limited`
  EN: "No certificate was issued for {domain}: the certificate authority
  refused the request because one of its limits was reached. The site is served
  as before, with the certificate it already had if it had one. A new request
  before the limit resets is refused the same way and counts against it.
  Request the certificate here again after the limit resets; nothing asks again
  automatically."
  TR: "{domain} için sertifika çıkarılmadı: sertifika otoritesi, sınırlarından
  birine ulaşıldığı için isteği reddetti. Site eskisi gibi sunuluyor; önceden
  bir sertifikası varsa o yerinde duruyor. Sınır sıfırlanmadan yapılan yeni bir
  istek aynı biçimde reddedilir ve sınıra sayılır. Sertifikayı sınır
  sıfırlandıktan sonra buradan yeniden isteyin; hiçbir şey kendiliğinden
  yeniden istemez."
- `ssl.issueFailure.timeout`
  EN: "No certificate was issued for {domain}: certbot did not finish within
  the time allowed and was stopped. Why it took that long is not known here.
  The site is served as before, with the certificate it already had if it had
  one. On the server, read /var/log/celikpanel/certbot/letsencrypt.log, then
  request the certificate here again. Nothing asks again automatically."
  TR: "{domain} için sertifika çıkarılmadı: certbot tanınan sürede bitmedi ve
  durduruldu. Neden o kadar sürdüğü burada bilinmiyor. Site eskisi gibi
  sunuluyor; önceden bir sertifikası varsa o yerinde duruyor. Sunucuda
  /var/log/celikpanel/certbot/letsencrypt.log dosyasını okuyun, sonra
  sertifikayı buradan yeniden isteyin. Hiçbir şey kendiliğinden yeniden
  istemez."
- `ssl.issueFailure.tool`
  EN: "No certificate was issued for {domain}: certbot ended with an error. The
  site is served as before, with the certificate it already had if it had one.
  On the server, read /var/log/celikpanel/certbot/letsencrypt.log, correct what
  it names, then request the certificate here again. Nothing asks again
  automatically."
  TR: "{domain} için sertifika çıkarılmadı: certbot hata ile sonlandı. Site
  eskisi gibi sunuluyor; önceden bir sertifikası varsa o yerinde duruyor.
  Sunucuda /var/log/celikpanel/certbot/letsencrypt.log dosyasını okuyun, adını
  verdiği şeyi düzeltin, sonra sertifikayı buradan yeniden isteyin. Hiçbir şey
  kendiliğinden yeniden istemez."
- `ssl.issueFailure.said`
  EN: "certbot reported: {detail}"
  TR: "certbot’un bildirdiği: {detail}"

**Where each is drawn.** The service-action sentences: the notice of a
component's page and of the components list (`ServiceActionNotice`). Those
screens send Start, Stop and Restart; a Reload reaches the Panel through its
API, so the four reload sentences are shown there only to an API client's user,
and in the browser record through a mocked answer. The import: the preview's
line under the mail accounts, the result (`ImportPage`), and a refusal of the
Panel, which now stays on the page (`ErrorBanner`) instead of leaving with a
toast. A step's own line (`detail`) is the server's English text, except the
one for a mailbox without a password. The certificate: a notice on the SSL/TLS
tab (`CertificateIssueNotice`) that stays until it is closed or the certificate
is requested again; certbot's line is shown to an administrator only.

**Limits.** The dialogs for a database and a database user still show the
password in an English toast when the server minted one; when the caller typed
it, nothing is shown, because the server no longer sends it back. Inspected in
a real Chrome against the loopback mock; not on a real Panel.

### After the final native round: a refused site says what was removed, a stopped service is not reloaded, an import names what it left out, a Stop says what it left, the update card says what already happened (2026-10-12)

Source state with component tests; the native re-check on Arch is pending and
nothing was observed on an installed server. What was measured and what changed
is in the resilience contract entry of the same date. This entry keeps the
sentences.

**The rule the texts follow.** An answer says that something was removed only
when the removal was confirmed, and says where to look when it was not; none of
them says that nothing at all is left on the server. A service that is not
running was not reloaded, whichever service it is. A success that leaves native
state the owner would not expect says so, and leaves that state as the service
manager recorded it. An entry of an archive that was not imported is listed by
its name, and it is not confused with a chosen part that is missing. A version
that already failed on this server is named as that before it is started again,
and it can still be started.

**Replaced.** The sentence of `panelUpdate.previousAttempt.recovered` recorded
in the entry of 2026-10-01 ("... and the server was returned to the previous
version. Starting it again repeats the same update unless the cause has been
fixed.") is no longer shown, and a version that was returned no longer has the
heading `panelUpdate.previousAttempt.title`. The ones below are. A site the web
server refused used to answer `500 INTERNAL` "internal server error".

**A site the web server refused (`POST /api/v1/domains/create`, `POST /api/v1/import/cpanel/apply`).**

`vars`: `domain`, `command` (`sudo nginx -t`). `details`: one line, nginx's
own, for an administrator only; it can name paths of the server.

- `502 SITE_WEB_SERVER_REFUSED`, reason `removed`
  API: "The site {domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it, so the site was never put into
  service. What had been created for it was removed again and the removal was
  confirmed: its web server configuration, its system account, its files and,
  for a PHP site, its PHP pool. nginx was reloaded with the configuration it
  had before. The server owner runs sudo nginx -t on the server. If it reports
  an error now, a file of the server's own nginx configuration is refused and
  is corrected first. If it passes, what nginx refused was in the configuration
  CelikPanel generated for this server; the line nginx printed names it and is
  shown to administrators. Then create the site again; nothing retries by
  itself."
- `domains.add.webServerRefused.removed`
  EN: "{domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it. What had been created for the site
  was removed again, and the removal was confirmed: its web server
  configuration, system account, files and, for a PHP site, PHP pool. nginx was
  reloaded with the configuration it had before. On the server, run {command}.
  If it reports an error, a file of the server’s own nginx configuration is
  refused; correct that first. If it passes, the refusal came from the
  configuration CelikPanel generated, and the line nginx printed names it
  (administrators see it below). Then create the site again; nothing retries by
  itself."
  TR: "{domain} oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için
  ürettiği yapılandırmayı reddetti. Site için oluşturulanlar yeniden kaldırıldı
  ve bu kaldırma doğrulandı: web sunucusu yapılandırması, sistem hesabı,
  dosyaları ve PHP sitesiyse PHP havuzu. nginx, önceki yapılandırmasıyla
  yeniden yüklendi. Sunucuda {command} komutunu çalıştırın. Bir hata
  bildiriyorsa sunucunun kendi nginx yapılandırmasındaki bir dosya
  reddediliyordur; önce onu düzeltin. Geçiyorsa ret CelikPanel’in ürettiği
  yapılandırmadan gelmiştir ve nginx’in yazdığı satır onu adlandırır
  (yöneticiler aşağıda görür). Ardından siteyi yeniden oluşturun; hiçbir şey
  kendiliğinden yeniden denemez."
- `502 SITE_WEB_SERVER_REFUSED`, reason `cleanup_unconfirmed`
  API: "The site {domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it, so the site was never put into
  service. nginx was reloaded with the configuration it had before. Removing
  what had been created for the site was not confirmed, so parts of it may
  remain on the server: open Domains and, if {domain} is listed there, delete
  it, which removes its parts. The server owner runs sudo nginx -t on the
  server. If it reports an error now, a file of the server's own nginx
  configuration is refused and is corrected first. If it passes, what nginx
  refused was in the configuration CelikPanel generated for this server; the
  line nginx printed names it and is shown to administrators. Then create the
  site again; nothing retries by itself."
- `domains.add.webServerRefused.unconfirmed`
  EN: "{domain} was not created: the web server (nginx) refused the
  configuration CelikPanel generated for it. nginx was reloaded with the
  configuration it had before. Removing what had been created for the site was
  not confirmed, so parts of it may remain on the server: open Domains and, if
  {domain} is listed there, delete it, which removes its parts. On the server,
  run {command}. If it reports an error, a file of the server’s own nginx
  configuration is refused; correct that first. If it passes, the refusal came
  from the configuration CelikPanel generated, and the line nginx printed names
  it (administrators see it below). Then create the site again; nothing retries
  by itself."
  TR: "{domain} oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için
  ürettiği yapılandırmayı reddetti. nginx, önceki yapılandırmasıyla yeniden
  yüklendi. Site için oluşturulanların kaldırıldığı doğrulanamadı; bu yüzden
  bir bölümü sunucuda kalmış olabilir: Alan Adları sayfasını açın ve {domain}
  orada listeleniyorsa silin; silme, parçalarını da kaldırır. Sunucuda
  {command} komutunu çalıştırın. Bir hata bildiriyorsa sunucunun kendi nginx
  yapılandırmasındaki bir dosya reddediliyordur; önce onu düzeltin. Geçiyorsa
  ret CelikPanel’in ürettiği yapılandırmadan gelmiştir ve nginx’in yazdığı
  satır onu adlandırır (yöneticiler aşağıda görür). Ardından siteyi yeniden
  oluşturun; hiçbir şey kendiliğinden yeniden denemez."
- `502 SITE_WEB_SERVER_REFUSED`, reason `import_removed`
  API: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. The site {domain} is the import's first step and
  it was not created: the web server (nginx) refused the configuration
  CelikPanel generated for it, so the site was never put into service. What had
  been created for it was removed again and the removal was confirmed: its web
  server configuration, its system account, its files and, for a PHP site, its
  PHP pool. nginx was reloaded with the configuration it had before. The server
  owner runs sudo nginx -t on the server. If it reports an error now, a file of
  the server's own nginx configuration is refused and is corrected first. If it
  passes, what nginx refused was in the configuration CelikPanel generated for
  this server; the line nginx printed names it and is shown to administrators.
  Then start the import again; nothing starts it again automatically."
- `import.webServerRefused.removed`
  EN: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. Its first step is the site {domain}, which was
  not created: the web server (nginx) refused the configuration CelikPanel
  generated for it. What had been created for the site was removed again, and
  the removal was confirmed: its web server configuration, system account,
  files and, for a PHP site, PHP pool. nginx was reloaded with the
  configuration it had before. On the server, run {command}. If it reports an
  error, a file of the server’s own nginx configuration is refused; correct
  that first. If it passes, the refusal came from the configuration CelikPanel
  generated, and the line nginx printed names it (administrators see it below).
  Then start the import again; nothing starts it again automatically."
  TR: "İçe aktarma başlamadı ve arşivden hiçbir dosya, posta kutusu, DNS kaydı
  ya da veritabanı içe aktarılmadı. İlk adımı {domain} sitesidir ve bu site
  oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için ürettiği
  yapılandırmayı reddetti. Site için oluşturulanlar yeniden kaldırıldı ve bu
  kaldırma doğrulandı: web sunucusu yapılandırması, sistem hesabı, dosyaları ve
  PHP sitesiyse PHP havuzu. nginx, önceki yapılandırmasıyla yeniden yüklendi.
  Sunucuda {command} komutunu çalıştırın. Bir hata bildiriyorsa sunucunun kendi
  nginx yapılandırmasındaki bir dosya reddediliyordur; önce onu düzeltin.
  Geçiyorsa ret CelikPanel’in ürettiği yapılandırmadan gelmiştir ve nginx’in
  yazdığı satır onu adlandırır (yöneticiler aşağıda görür). Ardından içe
  aktarmayı yeniden başlatın; hiçbir şey onu kendiliğinden yeniden başlatmaz."
- `502 SITE_WEB_SERVER_REFUSED`, reason `import_cleanup_unconfirmed`
  API: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. The site {domain} is the import's first step and
  it was not created: the web server (nginx) refused the configuration
  CelikPanel generated for it, so the site was never put into service. nginx
  was reloaded with the configuration it had before. Removing what had been
  created for the site was not confirmed, so parts of it may remain on the
  server: open Domains and, if {domain} is listed there, delete it, which
  removes its parts. The server owner runs sudo nginx -t on the server. If it
  reports an error now, a file of the server's own nginx configuration is
  refused and is corrected first. If it passes, what nginx refused was in the
  configuration CelikPanel generated for this server; the line nginx printed
  names it and is shown to administrators. Then start the import again; nothing
  starts it again automatically."
- `import.webServerRefused.unconfirmed`
  EN: "The import did not start, and no file, mailbox, DNS record or database
  of the archive was imported. Its first step is the site {domain}, which was
  not created: the web server (nginx) refused the configuration CelikPanel
  generated for it. nginx was reloaded with the configuration it had before.
  Removing what had been created for the site was not confirmed, so parts of it
  may remain on the server: open Domains and, if {domain} is listed there,
  delete it, which removes its parts. On the server, run {command}. If it
  reports an error, a file of the server’s own nginx configuration is refused;
  correct that first. If it passes, the refusal came from the configuration
  CelikPanel generated, and the line nginx printed names it (administrators see
  it below). Then start the import again; nothing starts it again
  automatically."
  TR: "İçe aktarma başlamadı ve arşivden hiçbir dosya, posta kutusu, DNS kaydı
  ya da veritabanı içe aktarılmadı. İlk adımı {domain} sitesidir ve bu site
  oluşturulmadı: web sunucusu (nginx), CelikPanel’in bu site için ürettiği
  yapılandırmayı reddetti. nginx, önceki yapılandırmasıyla yeniden yüklendi.
  Site için oluşturulanların kaldırıldığı doğrulanamadı; bu yüzden bir bölümü
  sunucuda kalmış olabilir: Alan Adları sayfasını açın ve {domain} orada
  listeleniyorsa silin; silme, parçalarını da kaldırır. Sunucuda {command}
  komutunu çalıştırın. Bir hata bildiriyorsa sunucunun kendi nginx
  yapılandırmasındaki bir dosya reddediliyordur; önce onu düzeltin. Geçiyorsa
  ret CelikPanel’in ürettiği yapılandırmadan gelmiştir ve nginx’in yazdığı
  satır onu adlandırır (yöneticiler aşağıda görür). Ardından içe aktarmayı
  yeniden başlatın; hiçbir şey onu kendiliğinden yeniden başlatmaz."

**A PHP version the server does not run (`POST /api/v1/domains/create`).**

`vars`: `version`, `installed`. Refused before anything is created. No screen
sends a PHP version with a new site, so this sentence reaches an API client
only and has no catalogue entry.

- `409 PHP_VERSION_NOT_INSTALLED`
  API: "Nothing was created: PHP {version} is not installed on this server.
  Installed: {installed}. Choose one of these versions and create the site
  again; another version can be used only after it is installed on this server
  (Services)."

**A reload of a service that is not running (`POST /api/v1/service/action`).**

No new sentence: `409 SERVICE_ACTION_FAILED`, reason `not_running`, and
`err.SERVICE_ACTION_FAILED.not_running`, as recorded in the entry of
2026-10-11, are now the answer for every unit that is `inactive` or `failed`
when a reload is asked (nginx, MariaDB, PHP-FPM, PostgreSQL and its wrapper),
not only for Postfix and Dovecot. `vars.detail` is what was read, for example
"nginx.service is inactive (dead); nothing was reloaded".

**A Stop that succeeded and left the unit marked as failed (`POST /api/v1/service/action`).**

The answer is the success it always was, with `note`: `code`, `reason`, `error`
and `vars` (`unit`, `failed_unit`, `result`, `command`, and `detail` when the
service's own check prints a line now). `command` is `sudo systemctl
reset-failed <failed_unit>`; CelikPanel does not run it.

- `200`, `note.code` `SERVICE_ACTION_NOTE`, `note.reason` `unit_marked_failed`
  API: "The service was stopped and is not running. systemd now shows its unit
  as failed, which it was not before the stop. That mark is systemd's own
  record of how the unit's stop went (with the result exit-code: a command of
  the unit exited with an error), and CelikPanel leaves it as it is. Start can
  be used from this state. To clear the mark without starting the service, the
  server owner runs the command shown."
- `services.action.note.unit_marked_failed`
  EN: "{unit} was stopped and is not running. systemd now shows {failed_unit}
  as failed (result: {result}), which it was not before the stop. That mark is
  systemd’s own record of how the unit’s stop went (with the result exit-code,
  a command of the unit exited with an error), and CelikPanel leaves it as it
  is. Start can be used from this state. To clear the mark without starting,
  run {command} on the server."
  TR: "{unit} durduruldu ve çalışmıyor. systemd şimdi {failed_unit} birimini
  failed (sonuç: {result}) olarak gösteriyor; durdurmadan önce öyle değildi. Bu
  işaret, systemd’nin birimin durdurulmasının nasıl geçtiğine dair kendi
  kaydıdır (sonuç exit-code ise birimin bir komutu hatayla çıkmıştır) ve
  CelikPanel onu olduğu gibi bırakır. Bu durumdan Başlat kullanılabilir.
  İşareti başlatmadan temizlemek için sunucuda {command} komutunu çalıştırın."
- `200`, `note.code` `SERVICE_ACTION_NOTE`, `note.reason` `unit_marked_failed_config`
  API: "The service was stopped and is not running. systemd now shows its unit
  as failed, which it was not before the stop. That mark is systemd's own
  record of how the unit's stop went (with the result exit-code: a command of
  the unit exited with an error), and CelikPanel leaves it as it is. The
  service's own check refuses its configuration at present, and the unit's stop
  command reads the same file; the service will not start until that is
  corrected. Start can be used from this state. To clear the mark without
  starting the service, the server owner runs the command shown."
- `services.action.note.unit_marked_failed_config`
  EN: "{unit} was stopped and is not running. systemd now shows {failed_unit}
  as failed (result: {result}), which it was not before the stop. That mark is
  systemd’s own record of how the unit’s stop went, and CelikPanel leaves it as
  it is. {unit} refuses its own configuration at present (its line is below),
  and the unit’s stop command reads the same file; it will not start until that
  is corrected. To clear the mark without starting, run {command} on the
  server."
  TR: "{unit} durduruldu ve çalışmıyor. systemd şimdi {failed_unit} birimini
  failed (sonuç: {result}) olarak gösteriyor; durdurmadan önce öyle değildi. Bu
  işaret, systemd’nin birimin durdurulmasının nasıl geçtiğine dair kendi
  kaydıdır ve CelikPanel onu olduğu gibi bırakır. {unit} şu an kendi
  yapılandırmasını reddediyor (satırı aşağıda) ve birimin durdurma komutu da
  aynı dosyayı okur; bu düzeltilene dek başlamaz. İşareti başlatmadan
  temizlemek için sunucuda {command} komutunu çalıştırın."

**A cPanel import that left archive entries out (`POST /api/v1/import/cpanel/apply`).**

An entry the archive names with an absolute path is a step of its own,
`member:<name>`, at most 20 of them, then one step `members:<n>` for the n that
are not listed. When nothing else is missing the domain is marked as finished.
The files step's own line also says how much of the archive is outside the site
folder, in the server's English: "2 files, 59 bytes. 5 other entries of the
archive are outside the site folder (homedir/public_html) and are not copied by
this step: homedir/mail (2), homedir/etc (1), mysql (1), homedir (1). The
databases, mailboxes, forwarders and DNS records are read from their own
entries by their own steps; mailbox contents and the other folders of the home
directory are not imported". A files step that is refused whole names the
entry: "unsupported cpmove site entry type:
cpmove-user/homedir/public_html/uploads is a symbolic link".

- step `member:<name>`, `detail`
  API: "not imported: the archive names this entry with an absolute path, and
  an import writes only below the site's own folder; nothing was written for
  it"
- `import.detail.absoluteMember`
  EN: "Not imported: the archive names this entry with an absolute path, and an
  import writes only inside the site’s own folder. Nothing was written for it."
  TR: "İçe aktarılmadı: arşiv bu girdiyi mutlak bir yolla adlandırıyor; içe
  aktarma yalnızca sitenin kendi klasörünün içine yazar. Bu girdi için hiçbir
  şey yazılmadı."
- `200`, `status: partial`, `domain_status: active`, `message`
  API: "The import ended and every part that was chosen was imported; {domain}
  is in service. Imported: {imported}. Not imported: {not imported}. These
  entries of the archive were refused by their names, and nothing was written
  for them; the reason of each is in its step below. If one of them is a file
  the site needs, add it with the file manager of {domain}. Importing the
  archive again refuses the same entries; nothing continues by itself."
- `import.partial.entriesBody`
  EN: "The import has ended. Everything you chose was imported, and {domain} is
  in service. The archive entries listed as not imported were refused by their
  names; nothing was written for them."
  TR: "İçe aktarma sona erdi. Seçtiğiniz her şey içe aktarıldı ve {domain}
  yayında. İçe aktarılmadı diye listelenen arşiv girdileri adları yüzünden
  reddedildi; onlar için hiçbir şey yazılmadı."
- `import.partial.entriesNext`
  EN: "Nothing more is needed for {domain}. If one of these entries is a file
  the site needs, add it with the site’s file manager. Importing the archive
  again refuses the same entries."
  TR: "{domain} için başka bir şey gerekmiyor. Bu girdilerden biri sitenin
  ihtiyaç duyduğu bir dosyaysa onu sitenin dosya yöneticisiyle ekleyin. Arşivi
  yeniden içe aktarmak aynı girdileri yine reddeder."
- `import.part.member`
  EN: "Archive entry {name}"
  TR: "Arşiv girdisi {name}"
- `import.part.moreMembers`
  EN: "{name} more archive entries"
  TR: "{name} arşiv girdisi daha"

**The update card, when the offered version was already tried here (`GET /api/v1/panel/update/check`, `previous_attempt`).**

Shown above the Start button, in this order: the heading, what happened and
what the server runs now, the recorded cause
(`panelUpdate.previousAttempt.cause`, unchanged) or that none was recorded, and
what starting it again does. An attempt that failed without a recorded return
keeps its heading and sentence of 2026-10-01 and now also says when no cause
was recorded. The Start button is never disabled by this notice and the version
is never hidden.

- `panelUpdate.previousAttempt.rolledBackTitle`
  EN: "This version was already tried on this server and rolled back"
  TR: "Bu sürüm bu sunucuda daha önce denendi ve geri alındı"
- `panelUpdate.previousAttempt.recovered`
  EN: "{version} was started here on {time}. The update did not complete, and
  the server was returned to {current}, which it runs now."
  TR: "{version} burada {time} tarihinde başlatıldı. Güncelleme tamamlanmadı ve
  sunucu {current} sürümüne döndürüldü; şu an onu çalıştırıyor."
- `panelUpdate.previousAttempt.noCause`
  EN: "The server recorded no more specific cause for that attempt."
  TR: "Sunucu o deneme için daha belirli bir neden kaydetmedi."
- `panelUpdate.previousAttempt.again`
  EN: "Starting it again runs the same update. If the cause was on this server
  and has been corrected, the result can differ; otherwise expect the same one.
  A corrected version, when it is published, is offered here as a newer
  version. The button below still starts {version}."
  TR: "Yeniden başlatmak aynı güncellemeyi çalıştırır. Neden bu sunucudaysa ve
  giderildiyse sonuç değişebilir; değilse aynı sonucu bekleyin. Düzeltilmiş bir
  sürüm yayımlandığında burada daha yeni bir sürüm olarak sunulur. Aşağıdaki
  düğme {version} sürümünü yine başlatır."

**Where each is drawn.** A refused site: the Add Domain dialog and the import
page (`ErrorBanner`), where it stays; nginx's line is drawn under the sentence,
in the face used for what a program printed. The note of a Stop: the notice of
a component's page and of the components list (`ServiceActionNotice`), on the
attention surface with `role="status"`, never on the failure surface; the
service's line and the command are set apart as in the other outcomes. The
import: the result's two lists and its steps (`ImportPage`). The update card:
`PanelUpdateCard`.

**Limits.** The Add Domain dialog creates a static site and never sends a PHP
version, so in the browser a refused site is reached through the import, or
through a mocked answer in the browser record. The same refusal during a change
of hosting type or a certificate change still answers its earlier error. The
files step's line about entries outside the site folder is the server's
English. Inspected in a real Chrome against the loopback mock; not on a real
Panel.
