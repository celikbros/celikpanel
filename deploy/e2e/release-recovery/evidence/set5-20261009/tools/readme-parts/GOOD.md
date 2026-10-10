The update is verified about a minute after the owner's start on each platform (`timeline.md`): Debian start
17:23:11Z, `succeeded/update_verified` read 17:24:10Z; Ubuntu 17:44:15Z / 17:45:16Z; Arch 18:11:09Z / 18:12:00Z. The
guest sampler (one probe every 5 s) has one Panel outage window per cell, inside the operation: 20-30 s (Debian,
Arch), 25-35 s (Ubuntu). Site and cron: `never-interrupted` on all three; SMTP: `never-interrupted` on Debian and
Ubuntu, not seeded on Arch (no mail there); DNS is not provided by these cells (`S/NN-verdicts/step.json`,
`workloads`). At the end
the identity files of Agent and Panel read `version=v0.1.0-alpha.82 commit=8c2250b05132fa91291a33bd23924d0aefd72617`
on all three (`S/NN-terminal/step.json`, `builds`).

The facts after the verified update (step `post-update-facts`; generated from the cells in `part2-table.md`, where
each cell's answers are quoted in full):

- **(a)** The ledger is the released 43: 43 contiguous rows whose digest equals the one `populated_database.py` pins
  for schema 43, the schema SQL equals the pinned schema 43, `request_identities` exists, `integrity_check` is `ok`
  (verdict `as-expected`, all five facts true, three platforms).
- **(b)** `POST /api/v1/domains/{id}/backups` sent as a page opened before the update sends it (no
  `X-CelikPanel-Request-Id`): `428 REQUEST_ID_REQUIRED` and no archive written. The same request with the header:
  `200`, one new archive (archives before / after the refusal / after the accepted request: 0, 0, 1), one
  `request_identities` row `done` (three platforms).
- **(c)** The versioned writes: a scheduled task (`ct1-` version read, sent, a new version read back; the line is in
  the site account's crontab once), the automatic backup schedule (`bs1-`), and on Debian and Ubuntu the server mail
  policy (`mp1-`; `postconf -h smtpd_client_message_rate_limit` = `37`). The same writes without a version answer
  `409 SETTINGS_VERSION_REQUIRED` (`scheduled_tasks`, `backup_schedule`, `mail_policy`) and change nothing. On Arch
  the mail policy is not measured (no mail stack there).
- **(d)** The Panel's deferred startup mail work completed in its first attempt, once: Debian `panel[37625]` started
  17:23:58.43, attempt line 17:24:40.42; Ubuntu `panel[55397]` 17:45:05.01 / 17:45:47.50. Arch: not measured (no mail
  stack).
- **(e)** The root CLI and the Panel's recovery reader both read `succeeded/update_verified`; the update card rendered
  from the candidate's own rules is judged `as-expected` (three platforms).

Every one of these values is equal to set3's for the same cell (`compare-with-set3.md`), the setting versions
(`bs1-...`, `ct1-...`, `mp1-...`) included.
