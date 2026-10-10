## Part 2: the owner-started update from the published v0.1.0-alpha.81 to the candidate

`owner_update_trial.py`, fourteenth run of the kind, the first from the published **alpha.81**. In every cell the
tag's own installer installs the tag build (`version=v0.1.0-alpha.81 commit=a0beb7263...`, ledger 42), the owner logs
in, the acceptance licence is accepted without any licence service, the owner's setup runs to the isolated host's
`access_dns` wait (observed; no certificate step runs, so no certificate authority is asked), a site, a mailbox
(Debian, Ubuntu), a cron job and an owner database are seeded, and the owner starts the update to
`v0.1.0-alpha.82 / 82` from the Panel's own update route. The fixture origin on the guest's loopback serves the
candidate signed with the per-lab fixture key.

{{PART2_TABLE}}

Terminal checks of every complete run (`summary-per-cell.txt`, `steps/NN-terminal/step.json`): installed = running for
Panel and Agent with the expected identity (the candidate after a forward end, `v0.1.0-alpha.81 / a0beb7263...` after a
return), fresh owner login, timers and `table inet celikpanel_fw` equal, site marker, mailbox and SMTP on Debian and
Ubuntu, the seeded cron row. The site, SMTP and cron were never interrupted except by a cell's own VM reset or
orderly reboot (`steps/NN-verdicts/step.json`).

### Good update (Debian 13, Ubuntu 24.04, Arch): verified, and what the owner meets afterwards

The update is verified about a minute after the owner's start (Debian: start 07:38:08Z, `succeeded/update_verified`
07:39:07Z; Arch 07:41:10Z / 07:42:08Z; Ubuntu 07:46:13Z / 07:47:13Z). Panel down 20-35 s. **With an alpha.81 baseline the root
CLI and the Panel's recovery reader exist from the first second**: the first sample after the start already reads
`known running/update_running` on the CLI and `accepted` on the reader (the alpha.80 gaps of upd7/upd13 - no CLI for
about 10 s, then `observation_unavailable` - do not occur); while the Panel is down the CLI alone answers
(`steps/11-track/samples/`).

The facts measured after the verified update (step `post-update-facts`; generated from the cells, `part2-table.md`):

{{PART2_FACTS_GOOD}}

In words, the same on all three platforms:

- **(a)** The ledger is the released 43: 43 contiguous rows whose canonical digest equals the one
  `populated_database.py` pins for schema 43 (`a0b5c424...`), the schema SQL equals the pinned schema 43 (`48cbd3b4...`),
  `request_identities` exists with its 13 columns, `integrity_check` is `ok`.
- **(b)** `POST /api/v1/domains/{id}/backups` sent as a page opened before the update sends it (the session's own
  headers, **no** `X-CelikPanel-Request-Id`): **`428 REQUEST_ID_REQUIRED`** with the reload sentence; no archive was
  written and no `request_identities` row. The same request with the header: `200`, exactly one new archive whose name
  the answer carries, one row `done`.
  EN (API): "This page was opened before CelikPanel was updated, so the server did not accept the change and nothing
  was changed. Reload the page, then make the change again. (A client that is not the CelikPanel page sends the header
  X-CelikPanel-Request-Id: 32 lowercase hexadecimal characters, a new value for each action.)" Screen
  (`err.REQUEST_ID_REQUIRED`), EN: "This page was opened before CelikPanel was updated, so the server did not accept
  the change and nothing was changed. Reload the page, then make the change again." TR: "Bu sayfa CelikPanel
  güncellenmeden önce açılmış; bu yüzden sunucu değişikliği kabul etmedi ve hiçbir şey değiştirilmedi. Sayfayı
  yeniden yükleyin, sonra değişikliği yeniden yapın."
- **(c)** The writes that carry a version work, so Panel and Agent are one release: a scheduled task added (`ct1-`
  version read, sent, new version read back; the line is in the site account's crontab once), the server mail policy
  saved on Debian and Ubuntu (`mp1-`; `applied: reloaded`; `postconf -h smtpd_client_message_rate_limit` = `37`), the
  automatic backup schedule created (`bs1-`; read back enabled, weekly, full, 30). **The same three writes without a
  version - what a page opened before the update sends - answer `409 SETTINGS_VERSION_REQUIRED`** (`scheduled_tasks`,
  `mail_policy`, `backup_schedule`) and change nothing: "This request did not say which settings it was built from, so
  nothing was changed. Reload the page so it reads the current settings, then make the change again."
- **(d)** The Panel's deferred startup mail work completed in its first attempt, once (Debian `panel[37522]` started
  07:38:56.60, attempt line 07:39:38.30; Ubuntu `panel[55198]` 07:47:03.03 / 07:47:46.38), after the operation's
  terminal state, as in upd12/upd13. Arch has no mail stack.
- **(e)** The root CLI and the Panel's recovery reader both read `succeeded/update_verified`; the update card rendered
  from the candidate's own rules shows the verified update (judged `as-expected`). CLI, EN: "The update completed and
  the new version was verified when it finished. Nothing else is needed on the server. Open the panel to check that
  everything works now."

### Migration defect with a second fault: automatic return to alpha.81 (Debian 13, Ubuntu 24.04: VM reset at `payload_restored`; Arch: SIGKILL at `runtime_verified`)

Debian: owner start 07:50:11Z; the candidate's offline migration fails (`CELIKPANEL_UPDATE_FAILURE code=update_failed
state=recovery_required reason=offline panel database migration failed; its original database and work evidence are
preserved`); attempt 1 (`update/active`, rollback) 07:50:59; QMP `system_reset` once at `payload_restored` (new boot,
SSH back after 8.8 s); attempt 2 (`rollback/active`) 07:52:04; `recovered/rollback_verified`, read 07:52:24. Arch:
attempt 1 07:51:49, the recovery child killed at `runtime_verified`, attempt 2 (`rollback/completion`) 07:52:38,
verified 07:52:52. Ubuntu: start 08:06:12, attempt 1 08:07:05, reset (SSH back after 10.6 s), attempt 2 08:08:12,
verified 08:08:36.

After the return (step `post-return-facts`, every defective cell alike):

{{PART2_FACTS_BACK}}

- **alpha.81's Panel and Agent serve**: installed = running = `version=v0.1.0-alpha.81 commit=a0beb7263...`;
  `GET /api/v1/panel/version` answers `schema_version: 42`.
- **The ledger is the released 42** (42 contiguous rows, digest `40756295...` = the pinned one; schema SQL = the pinned
  schema 42 `2754d89b...`), **`request_identities` does not exist**, `integrity_check` `ok`. The owner's data is
  intact: all 65 tables compared with the pre-update digests, `equal-except-volatile` (`metrics_samples`,
  `server_setup_executions`), schema digest equal; the seeded domain, mailbox and cron rows are listed.
  In this kind the candidate migrates an **isolated copy** and fails before the database is published, so the live
  database is never at 43; the return of a database that **was** published at 43 is the start-check cell below.
- **alpha.81 has the recovery reader and the outcome card; this is no longer the alpha.80 limitation** (upd7 F1/upd13
  F3). `GET /api/v1/recovery/status` on the returned alpha.81 Panel answers `200` `recovered/rollback_verified`,
  `previous_failure: update_failed`; the root CLI reads the same; the three views agree.
  Update card, rendered from the alpha.81 build's own rules and catalogues (EN): "Update not completed; previous
  version restored || The update to v0.1.0-alpha.82 did not complete. The server was returned to v0.1.0-alpha.81
  automatically and is running it now. || Cause: the update failed before it completed; the server recorded no more
  specific cause. || Nothing needs to be done on the server. Do not start the update to v0.1.0-alpha.82 again until a
  corrected version is published. If the server's message names a problem on this server, fix it first. || Nothing
  resumes by itself: the server keeps running v0.1.0-alpha.81. When a newer version is published, "Check for updates"
  offers it. || The server reported: reviewed updater failed: exit status 1: offline panel database migration failed;
  its original database and work evidence are preserved". The Turkish card is in each cell's
  `steps/NN-post-return-facts/step.json` (`views.update_card.texts.tr`).
  Recovery screen (EN): "Rollback verified || The server verified restoration of the previous release. Reload
  CelikPanel to check panel access. || Previously recorded failure: Update failed".
  Root CLI (EN): "The update did not complete, and the server was returned automatically to the version it ran
  before; that restoration was verified. Nothing needs to be done on the server. Do not start the same version again
  until a corrected version is published; the panel's update page shows what is known about the cause." (TR:
  "Güncelleme tamamlanmadı ve sunucu otomatik olarak güncellemeden önce çalıştırdığı sürüme döndürüldü; bu geri dönüş
  doğrulandı. ..." in `views.cli_text.tr`.)
- Recorded beside it (observation O18): the raw status of the update itself is `failed` with the updater's line as
  `summary`; `GET /api/v1/panel/update/check` still answers `available: true` for the same defective
  `v0.1.0-alpha.82` (with `previous_attempt: {phase: recovered}`), and the card's own sentence tells the owner not to
  start it again; the release floor file stays at `sequence=82 / v0.1.0-alpha.82` after the return, as after every
  earlier rollback.
- The restored alpha.81 Panel's deferred mail work completed in its first attempt (Debian `panel[4733]` 07:52:18.72 /
  07:52:57.50; Ubuntu `panel[6243]` 08:08:29.26 / 08:09:09.13): alpha.81 has the retry.

{{PART2_OTHER}}
