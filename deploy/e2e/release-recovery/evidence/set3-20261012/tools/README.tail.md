## Findings

### Candidate product defects

- **P5b. Arch: a PHP site still cannot be created, so every cPanel import still fails there; the pool is no longer
  the cause, the nginx vhost is.** `internal/services/templates/nginx/vhost.conf.tmpl:16` includes
  `snippets/fastcgi-php.conf`, a file that Debian's and Ubuntu's nginx packages ship (`nginx-common`) and Arch's nginx
  package does not (it has no `/etc/nginx/snippets`).
  Sequence (rid3-arch run-a): setup with purpose `web` (nginx, php-fpm, mariadb, postgresql: every step succeeded);
  `POST /api/v1/domains/create` `{"domain": "set3-php.test", "project_type": "php", "ssl_type": "none"}`. The Agent
  creates the site account and the pool (now under `/etc/php/php-fpm.d/`; `php-fpm`'s configuration test passes),
  then logs `CreateSite set3-php.test: nginx vhost activation: nginx validation failed: ... [emerg] open()
  "/etc/nginx/snippets/fastcgi-php.conf" failed (2: No such file or directory) in
  /etc/nginx/sites-enabled/set3-php.test.conf:27 ... rollback restored and reloaded the previous vhost`; the account is
  removed again. Answers: the Domains page's create **`500 INTERNAL` "internal server error"**
  (`cmd/panel/domain_handlers.go:487-498`: the orchestrator's error goes to `writeServerError`;
  `internal/services/site_orchestrator.go:246`), with no step and no next action; `POST /api/v1/import/cpanel/apply`
  the new **`502 IMPORT_SITE_NOT_CREATED`** on every arrival, nothing imported. Native after it: no domain row, no
  site account, no pool file of the site, PHP-FPM and nginx still active.
  Evidence: `rid3-arch/run-a/steps/15-c6b-php-site/section.json`, `steps/16-c7-import/section.json`,
  `steps/20-collect/journal-product.txt` (07:30:32Z, 07:30:36Z).
  {{P5B_SECOND}}

### Observations (not judged as defects)

- **O16. A Reload of the stopped PostgreSQL wrapper (and of a stopped nginx) is still answered `502` / `command`**
  with systemd's line ("postgresql.service is not active, cannot reload."), not `409 not_running`: systemd refuses the
  wrapper's reload before the Agent's own "no running unit behind it" reading applies
  (`cmd/agent/service_action_verify.go`, the `command` stage at about line 416 comes before `verifyWrapperAction`).
  The answer is truthful; it is only not the one the contract's sentence names for a wrapper. Debian 13 and Ubuntu
  24.04, `set3-*/run-a/steps/15-s8-service-actions/section.json` (`actions`).
- **O17. An archive member with an absolute path is left out silently.** It is not site payload, so the files step
  skips it, imports the rest and the answer is `200` / `active` without a word about the member. Nothing was written
  at the path. `rid3-debian13`, `rid3-ubuntu` `steps/NN-c7b-import-answers/section.json` (`hostile.absolute`).
- **O18. After an automatic return the update check still offers the same version** (`available: true`, with
  `previous_attempt: {phase: recovered}`); only the card's and the CLI's sentence say not to start it again. The
  release floor file stays at the candidate's sequence after the return. Every defective cell,
  `steps/NN-post-return-facts/step.json`.
- **O19. Postfix is left `failed` after a Stop with a refused `main.cf`** (Debian: `postfix.service` `failed`,
  `Result=exit-code`; Ubuntu: `postfix@-.service`), although the answer is now a truthful success (the master is gone).
  Unchanged from set2's O8 apart from the answer.
- **O20. On Arch the published alpha.81's `web_mail` plan is not attempted by the harness** (as for alpha.80, H18: the
  tag accepts the plan and fails at `05-mail_profile`); the Arch update cells ran with purpose `web`.
- The preview's `site_bytes`, `dns_zones` and `forwarders` were not judged. O2 of set1 and O6, O7, O12, O13 of set2 are
  unchanged, as the contract says.
{{OBS_EXTRA}}

### Incompatibility with v0.1.0-alpha.81

None was found in the measured paths. A page opened before the update is refused on the eight guarded routes (`428
REQUEST_ID_REQUIRED`) and on every versioned write (`409 SETTINGS_VERSION_REQUIRED`) until it is reloaded: that is the
designed behaviour, measured in every good cell, and it changes nothing on the server. After a return to alpha.81 the
owner has the recovery reader, the outcome card and the root CLI; the alpha.80 limitations (upd7 F1, F2; upd13 F3, F4)
do not apply.
{{INCOMPAT_EXTRA}}

### Platform limitations

- The isolated lab has no certificate authority: the Let's Encrypt route was measured for its guard and for its typed
  failure only. Setup stops at `access_dns`: no panel or mail certificate, no mail enrollment.
- Mail is not supported on Arch: no mailbox login, no groups A and B there, no deferred mail work.
- This host can enter modern standby despite a process-level keep-awake request (it did once, before the cells), and
  WSL cannot start Windows programs here, so the `C:` readings come from a Windows-side watcher.

### Harness

H40 and H41 above{{HARNESS_REFS}}. The checks that did not pass are listed, with their detail, in
`checks-not-passed.txt`: on Debian 13 and Ubuntu 24.04 (run-a) the two H40 checks only; on Arch run-a the checks that
see P5b.

## What this run does NOT prove

- Nothing about an installed server, a production licence, production signing, the real release origin or the signed
  alpha.81 archive: the baseline is the tag's source built here with the acceptance licence seam switched on.
- The browser: the driver sends what the screens send; no screen was rendered. Cards and screens are rendered by the
  driver from the build's own rules and catalogues; Turkish sentences are catalogue lookups.
- One run per cell and per sequence. Repeatability is not shown.
- S1: a real cPanel archive (the fixture is the lab's minimal one; mailbox contents are not migrated by the product);
  other hash schemes than sha512-crypt; a mailbox without a password in the archive (`has_password: false`) was not
  built. The scan for hash-shaped values is a scan for the shapes listed in `owner_update_trial.HASH_SHAPED`.
- P3: `reload_reread` and `reload_not_reread` on anything but PostgreSQL's packaged instance unit with the two hooks
  described; `not_running` for a wrapper (O16); Dovecot or nginx with a refused configuration.
- P4: a files step that fails for another reason than a refused member (a full disk, an I/O error); a hard link or a
  device member; the import's own time limit.
- P5: nothing on Arch beyond the failed creation in run-a; what run-b shows is a reading with an owner's file in
  place, not the product's behaviour on a stock Arch host. A PHP version switch, a second PHP version, SSL on the PHP
  site were not run on any platform.
- O9: the other kinds (`authority_refused`, a rate limit, `timeout`, `tool`) and an issuance that succeeds. O11: the
  other DNS waits (`primary_dns`, `infrastructure_dns`), which still refuse by design. O14: a server upgraded in place
  (a version already recorded stays until the Panel's account is provisioned again).
- Part 2: real start, power loss, a second fault during the owner's retry, a cause the owner cannot remove, an update
  started with a page that is still open (the refusals were measured by sending what such a page sends, after the
  update), an update while guarded requests are running, rows of `request_identities` that exist before a return
  (the published alpha.81 cannot write any), the start check and management-off on Ubuntu and Arch, owner
  continuation on Arch.
- That the candidate's `request_identities` table is harmless to lose at a restore is shown only in the sense that the
  returned alpha.81 serves with its ledger at 42; no guarded request was in flight at the return.

## Removals, leftovers, secrets

- Removed, after each cell's evidence was staged and its checksums verified on the staged copy (`host/removals.txt`):
  the overlay disks of this run's labs. Nothing else, and nothing that existed before the run.
- Left on the WSL host (`host/host-leftovers.txt`): `/var/tmp/cp-set3-run` (run copies `harness-p`, `-u`, `-a` to `-e`,
  a mutable development copy `harness-dev` that no cell used, logs, queue), this run's labs without their overlays
  (`/var/tmp/cp-release-drill-set3-*`, `-rid3-*`, `-u14-*`: base image copies, keys, evidence), the three build clones
  `/var/tmp/cp-upd1-build/20261009t061555z`, `...t064831z`, `...t070418z`, and the dist directories of this run's
  fixture commits and of the tag commit under `/var/tmp/cp-pair-accept/dist/`. No QEMU and no job process was running
  at the end; HEAD and the local git configuration are unchanged; nothing was committed.
- `secret-scan.txt` (`tools/secretscan.py`, over every file before hashing): {{SCAN}}
- Root `SHA256SUMS` covers every file; the longest repository-relative path is under 240 characters.

## Files

`build/` (three builds, proofs, dry runs, offline logs), `harness-run-copy/` (run-copy hashes, overlays with their
diffs, jobs, queue), `tools/` (the scripts of this run; no secret), `host/` (host check, `C:` readings, removals,
leftovers, keep-awake log, power events), one folder per run with the driver's evidence unchanged (its own
`SHA256SUMS` verified when staged) and `host/`; Part 2 under `part2-alpha81/`. Generated: `summary-per-cell.txt`,
`part1-table.md`, `part2-table.md`, `checks-not-passed.txt`, `texts-en-tr.md`, `native-facts.json`,
`sampler-gaps.txt`, `secret-scan.txt`, `SHA256SUMS`.
