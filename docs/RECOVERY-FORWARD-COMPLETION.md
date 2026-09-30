# Independent update completion

*September 15, 2026 · [Türkçe](RECOVERY-FORWARD-COMPLETION.tr.md) · D-025 / P0.3*

This slice removes retained candidate data from the supported **complete v6
snapshot, pending update completion** path. It extends independent rollback and
runtime promotion; it does not close the full resilience matrix.

## Version transition

New preparation writes `celikpanel/recovery-material/v2`. The directory layout
remains `recovery-material/v1/<sha256(snapshot-name)>`: layout and record schema
are separate versions. Snapshot v6, exchange publication intent v2 and recovery protocol 1
remain unchanged. The only added comparison file is `libexec/get.sh`. Candidate
Panel/Agent/web payloads and install/rollback entrypoints are never copied into
the data kit or executed by the completion adapter.

Before stopping coordinators, the selected runtime must support the exact
`verify-material-support --layout snapshot-name-sha256-v1 --schema celikpanel/recovery-material/v2`
capability. Existing v1 records remain readable for rollback. They are not
rewritten or assigned missing v2 evidence.

For an unchanged bin or web tree, publication now seals the distinct
`celikpanel/recovery-resource-noop-intent/v1` record and its publication receipt.
It binds exact before/after identity without replacing the installed tree. It is
valid only for a material-v2 forward update, with no staged exchange and identical
before/after trees. The existing exchange intent v2 grammar is unchanged. Legacy
v1 no-op interpretation remains on its separately verified candidate path. For
that historical case, exit 6 verifies material and current eligibility for the
original candidate-based path; it cannot prove an exact publication identity
that the old writer never recorded. V2 completion never invents a missing no-op
record from semantic equality.

## Supported continuation

The runner admits only the same accepted update with a complete snapshot and
`completion.pending`, its matching scheduler marker, or the scheduler marker
alone. `completion-material-root --snapshot NAME` returns a verified v2 data
directory. Exit 3 means proven absence; exit 6 means a fully verified legacy v1
record. Only those two cases admit the historical intact candidate path.
Malformed, foreign, unreadable or missing material associated with publication
evidence cannot downgrade. A direct retained updater cannot bypass that rule.
This new updater needs the compatible selected reader even for a legacy direct
completion. A missing or older launcher is not proof of absence. Unchanged
historical retained updaters keep their separate historical behavior.

`verify-installed-completion --snapshot NAME` is read-only. For v2 material, it verifies bin/web
against the exact published intent and after-state, including filesystem
identity, metadata and content. A merely plausible target or equal file contents
are insufficient. Changed owner resources are preserved and recovery refuses.
The adapter also retains full snapshot, database, TLS, service unit, foundation,
native service-state and scheduler checks. It never applies a new payload or runs candidate
migrations. Before controlled service starts, the independent
`--check-completed-update-database-wal-aware` reader must verify the exact embedded
migration history, whole schema and idle queue on the same private WAL-aware
copy. Queue idleness alone does not prove database readiness.

A completion marker can precede database migration. It is **not** proof that the
database is ready. If the independent reader cannot verify it, the same operation
and its evidence remain available; this slice does not invent migration state.
The journal line `CELIKPANEL_UPDATE_CHECKPOINT database_verified_before_start` is
an observation, not a durable authority or a success result.

The pre-migration interruption window remains an explicit P0.3 gap. If a new
migration is required and the updater dies after publishing `completion.pending`
but before finishing that migration, the strict reader refuses forward completion.
The fixed owner recovery entrypoint selects the same completion path; direct
retained update/rollback entrypoints do not bypass its material-backed admission.
There is currently no supported automatic compensation or owner continuation for
that combination. The next checkpoint transition must distinguish database
readiness and prove candidate-independent continuation or snapshot compensation;
it must not relax this reader or infer readiness from the existing marker.

The material-v2 path rechecks installed payloads and saved service
runtime/enablement after controlled starts, before publishing the scheduler
obligation, and after scheduler restoration immediately before removing its last
marker. A failed check preserves the exact operation evidence and does not
publish success. A late scheduler-path failure does not restart coordinators.
These are completion-time observations, not a continuous-health guarantee.

Only after native runtime and scheduler restoration and exact marker cleanup
may the runner publish `succeeded / update_verified`. Polling cannot retry a
mutation. Owner recovery remains `sudo /usr/libexec/celikpanel/recovery recover`.
There is no new update command, license bypass or arbitrary command interface.

## Acceptance boundary

Required tests cover both schemas, all three late update marker topologies,
invalid material and receipts, missing candidate files, changed installed
resources, and root/lock/CLI boundaries. Native acceptance must kill the exact
updater after verified database preparation with retained candidate data absent,
then observe automatic completion and preserved workloads. Component tests alone
cannot establish that result.

Incomplete snapshot capture, unsupported database transitions, a new historical
rollback transaction, the full fault/reboot matrix, signed Agent admission,
metadata migrations and evidence cleanup remain open. [Native trials O and P](../deploy/e2e/release-recovery/FORWARD-COMPLETION.md)
record scoped automatic completion on fresh Arch and Debian 13 guests after
three retained candidate files were quarantined and the exact updater killed.
P exercises the final source with terminal rechecks. Its unchanged-schema,
bootstrap-TLS and inactive-scheduler limits remain explicit; no installed user
panel has been updated by this work.

The next source slice introduces [isolated database migration](RECOVERY-ISOLATED-DATABASE.md) with material v3. It keeps normal updates active until a separately migrated copy is verified and published. That slice has its own compatibility and scoped Q/R native acceptance record; the historical v2 behavior described here is not retroactively changed.

## Candidate panel start boundaries (2026-09-30)

D-025 invariants 2, 4 and 5 / P0.1, P0.2, P0.3. No schema or version
transition: snapshot v6, recovery material v3, marker grammar, kit protocol 1
and the v1 observation record are unchanged.

- **Before `completion.pending`.** After the isolated database is published and
  verified, the updater runs the installed candidate as the panel account with
  `panel --check-startup-readiness`. The check walks the normal start up to, but
  excluding, the listener and the Agent: data directory, database (private
  WAL-aware copy: exact embedded schema and migration history, idle queue, at
  least one user), secret key, license configuration, TLS settings and the
  served certificate/key pair, cookie/TLS posture, listen address and the web
  `index.html`. It writes nothing in the panel's state, TLS, web or
  configuration trees; the database proof uses the private copy under `/tmp`
  that the updater's idle checks already use. A failure prints one
  product-authored reason line and is a failure in phase `active` with code
  `candidate_panel_startup_check_failed`: the existing `update:active` recovery
  returns the server to the previous release automatically.
- **After the real start.** The single `is-active` is replaced by a bounded wait:
  eleven samples 0.5 s apart (at least 5 s, more than the unit's `RestartSec=3`)
  with one unchanged main PID and restart counter, and an anonymous loopback
  request to `/api/v1/panel/availability` that returns the panel's own coded
  `401 AUTH_REQUIRED`. In the normal update the request is pinned to the served
  public keys that the start check reported. The wait is bounded at 60 s and
  120 samples. A failure stays in `completion` with code `panel_start_unverified`
  and is completed forward by the existing recovery path.
- **Forward completion retries** use the same wait without a key pin, because
  this adapter never executes candidate code; only the panel's coded 401 over
  TLS on its own port counts.

These boundaries only move a detectable start failure before the point of no
return. They do not add a rollback after `completion.pending`. Remaining open:
a panel that passes the check, starts and fails later is completed forward with
no supported return to the previous release; there is no live browser status at
the panel's address while the panel is stopped (the owner SSH view in
[recovery access](RECOVERY-ACCESS.md) remains the fallback). Evidence: component
and contract tests only; native run pending.

## Certificate renewal while a completion waits (upd3 F2, 2026-10-01)

D-022, D-025 invariant 1 / P0.5, P0.3. No schema or version transition.

- **While attempts remain** the Certbot scheduler stays paused, as before; each
  forward attempt already runs with it paused.
- **After the last admitted attempt fails** (the third automatic one or an owner
  retry) in `update:completion`, `completion-scheduler` or `scheduler`, the
  recovery runner returns the scheduler to the state recorded in the snapshot's
  `panel-tls` ledger (`panel_tls_restore_certbot_scheduler`), once, under the
  release lock. A scheduler that already matches is left alone; a refused
  restore is reported in the recovery journal and not forced.
- **The same retry pauses it again** (`panel_tls_quiesce_certbot_scheduler`)
  directly after the pending snapshot is validated and before any coordinator is
  stopped. The quiesce proof requires the recorded enablement, so a later owner
  change stops the retry before it changes anything.
- **Why this is safe here:** forward completion validates only the snapshot copy
  and the installed release; it never compares or restores the live panel TLS
  tree, the pending activation or the deploy hook. A renewal between the pause
  and the retry therefore cannot make the retry fail or be undone by it. Rollback
  paths restore those files and keep renewal paused.

Evidence: component and contract tests only; native run pending. The standalone
`deploy/finalize-pending-update.sh` is unchanged and does not re-pause at start.
