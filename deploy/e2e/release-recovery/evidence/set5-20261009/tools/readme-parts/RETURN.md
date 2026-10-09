Four cells, each ending `recovered/rollback_verified` with alpha.81's Agent and Panel installed and running
(`version=v0.1.0-alpha.81 commit=a0beb7263...`, `S/NN-terminal/step.json`). Times from each `result.json`
(`outcome.attempts`, `outcome.reboot`) and `timeline.md`:

| | Debian 13, migration defect | Ubuntu 24.04, migration defect | Arch, migration defect | Debian 13, start check |
| --- | --- | --- | --- | --- |
| Owner start | 17:58:14Z | 18:29:16Z | 18:42:07Z | 18:54:11Z |
| Failure line of the update (`journal-product.txt`) | `code=update_failed state=recovery_required reason=offline panel database migration failed; its original database and work evidence are preserved`, 17:58:59 | the same, 18:30:01 | the same, 18:42:41 | `code=candidate_panel_startup_check_failed state=recovery_required reason=new panel start check failed before completion: panel startup check failed: tls_pair_invalid ...`, 18:55:05 |
| Attempt 1 (`update/active`, rollback) | 17:59:01 | 18:30:02 | 18:42:42 | 18:55:07 |
| Second fault | QMP `system_reset` once at `payload_restored`; new boot, SSH back after 8.8 s | the same; 11.0 s | the recovery child killed (SIGKILL) at `runtime_verified` (`recovery_fault_events`: armed, freeze, checkpoint verified, kill sent, released) | QMP `system_reset` once at `payload_restored`; new boot, SSH back after 8.5 s |
| Attempt 2 | `rollback/active`, 18:00:05 | `rollback/active`, 18:31:10 | `rollback/completion`, 18:43:31 | `rollback/active`, 18:56:10 |
| `recovered/rollback_verified` read | 18:00:32Z | 18:31:30Z | 18:43:46Z | 18:56:31Z, `failure_code: candidate_panel_startup_check_failed` kept |
| Panel outage windows (sampler, s) | 105-115, cause host reset | 104-114, cause host reset | 30-40 and 5-15 | 109-119, cause host reset |
| Site, SMTP (`S/NN-verdicts`) | interrupted only by the host reset | interrupted only by the host reset | site never interrupted; no mail | interrupted only by the host reset |
| Cron | never interrupted | never interrupted | never interrupted | never interrupted |

After the return (step `post-return-facts`, every cell; quoted in full in `part2-table.md`):

- **alpha.81 serves**: `GET /api/v1/panel/version` answers `200`, `v0.1.0-alpha.81`, `schema_version: 42`, Agent and
  Panel of one commit.
- **The ledger is the released 42** (42 rows, verdict `as-expected`, the five facts true: the ledger and schema digests
  equal the pinned ones, `request_identities` does not exist, `integrity_check` `ok`), and the database equals its
  pre-update digest except the two volatile tables (`equal-except-volatile`: `metrics_samples`,
  `server_setup_executions`; 65 tables compared, schema equal). In the start-check cell this is the return of a
  database that the candidate's updater had published at 43 before its start check failed; as in set3, the live
  ledger was not read in between.
- **Three views agree**: root CLI `recovered/rollback_verified` with previous failure `update_failed`; the Panel's
  recovery reader `200`, the same pair; the update card rendered from alpha.81's own rules, state `rolled_back`:
  "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not complete. The server was
  returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the update failed before it completed;
  the server recorded no more specific cause. || ... || The server reported: reviewed updater failed: exit status 1:
  offline panel database migration failed; its original database and work evidence are preserved". In the
  start-check cell the cause line is "Cause: the new version's panel failed its start check before anything was
  switched on."
- **Recorded beside it, as in set3 (its observation O18)**: the raw update status is `failed` with the updater's line
  as `summary`; `GET /api/v1/panel/update/check` still answers `available: true` for the same `v0.1.0-alpha.82` with
  `previous_attempt.phase: recovered`; the release floor file stays at `sequence=82 / v0.1.0-alpha.82` after the
  return.
- The restored alpha.81 Panel's deferred mail work completed in its first attempt (Debian migration cell `panel[5231]`
  18:00:26 / 18:01:04; Ubuntu `panel[6235]` 18:31:24 / 18:32:03; Debian start-check cell `panel[4738]` 18:56:25 /
  18:57:03). Arch has no mail stack.
- **Start check: the kind's 12 rules are all as expected** (`result.json` `kind.judged`: `rolled-back`,
  `status-failure-code`, `update-failure-line`, `fixture-reason`, `sidecar`, `no-completion-marker`,
  `rollback-dispatch`, `update-card`, `old-release-running`, `database-equal`, `cli-returned-text`, `web-catalogue`; no
  finding, none unknown).

Every compared value of these four cells equals set3's (`compare-with-set3.md`): the order and kind of the recovery
attempts, the reset, the ledger and schema digests, the views, the card's English text, the CLI's sentences in
English and Turkish, the update check and the version after the return.
