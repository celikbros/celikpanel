### Start check (Debian 13): the return of a database that had been published at 43

Candidate S: the shared TLS preparation of the Panel fails, so the read-only start check fails **after** the
candidate's updater has migrated an isolated copy and published it as the live database (`update.sh:2296-2299`:
`panel --migrate-only`, then `publish-update-database`; the start check and the code
`candidate_panel_startup_check_failed` come after it, `update.sh:2380-2384`). Owner start 08:04:14Z; failure line
`code=candidate_panel_startup_check_failed state=recovery_required reason=new panel start check failed before
completion: panel startup check failed: tls_pair_invalid ...` 08:05:06; attempt 1 (`update/active`, rollback) 08:05:08;
VM reset at `payload_restored` (SSH back after 8.6 s); attempt 2 (`rollback/active`) 08:06:12;
`recovered/rollback_verified` 08:06:32, `failure_code` kept. The kind's 12 rules are all as expected (`result.json`
`kind.judged`, among them `rollback-dispatch`, `no-completion-marker`, `database-equal`).

After the return: **the ledger is the released 42, `request_identities` does not exist, and all 65 tables equal the
pre-update digests** except the two volatile ones (`equal-except-volatile`, schema digest equal): the pre-update
snapshot is what serves, and the owner's rows (domain, mailbox, cron) are listed. alpha.81's card names the typed
cause, EN: "Update not completed; previous version restored || The update to v0.1.0-alpha.82 did not complete. The
server was returned to v0.1.0-alpha.81 automatically and is running it now. || Cause: the new version's panel failed
its start check before anything was switched on. || Nothing needs to be done on the server. Do not start the update to
v0.1.0-alpha.82 again until a corrected version is published. ..."; recovery screen: "Rollback verified || The new
version's panel failed its start check before anything was switched on, so the server was returned to the previous
version automatically. The previous version keeps running and nothing needs to be done on the server. When you
report this, include the reason line shown for this update on the update page. || Previously recorded failure: The
new version's panel failed its start check"; root CLI: "The new version's panel failed its start check before
anything was switched on, so the server was returned to the previous version automatically. The previous version
keeps running. Nothing needs to be done on the server. Do not start the same version again until a corrected version
is published. When you report this, include the reason line shown for this update on the panel's update page."

Limit: the live ledger was not read between the publication and the return (the candidate updater's own lines are not
in the collected journals); that the database was at 43 in between follows from the updater's order and the recorded
failure code, not from a reading.

### Owner continuation (Debian 13, Ubuntu 24.04)

{{OC_TEXT}}

### Management off across a reboot (Debian 13)

After the verified update (08:04:13Z) the owner runs `sudo systemctl disable --now celikpanel-panel.service
celikpanel-agent.service` and `sudo systemctl reboot` (new boot, SSH back after 3.8 s). For the 185 s window with both
units `disabled` and `inactive`: the site, the owner's database row and SMTP are served from the first sample of the
new boot (8.0 s after boot), cron wrote 3 stamps in the boot, `certbot.timer` stayed `enabled`/`active`,
`table inet celikpanel_fw` is present and equal, nothing needed the Panel. `systemctl enable --now` brings management
back: the Panel answers `starting` at 08:08:00Z and `ready` at 08:08:12Z (5 reads), login works, no difference. The
kind's 11 rules are as expected. The post-update facts (a)-(e) were not repeated in this cell.
