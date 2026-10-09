Good candidate; an owner-fixable host cause: a lab process holds `127.0.0.1:2083` from the moment the updater stops
the old Panel, so the new Panel cannot bind, the update pauses after its three forward attempts, and the harness then
does what the product's text tells the owner (free the port, run the one printed retry once). Times from each
`result.json` (`outcome.attempts`, `outcome.port_hold`, `outcome.final_status`) and
`S/13-owner-continuation-required/step.json`.

| | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- |
| Owner start | 19:08:17Z | 19:36:09Z |
| Forward attempts 1, 2, 3 (`update/completion`) | 19:10:06, 19:11:48, 19:13:29 | 19:38:05, 19:39:47, 19:41:38 |
| Pause read on the root CLI (`automatic_recovery: paused_retry_limit`); the Panel's recovery reader at that moment | step started 19:15:29; reader unavailable (the Panel is down) | step started 19:44:01; reader unavailable |
| Panel log the text names (`sudo journalctl -u celikpanel-panel -n 50`) | `Failed to start panel listener: listen tcp :2083: bind: address already in use` | the same |
| Renewal timer against before the update (`certbot.timer`) | recorded `on`, verdict `as-before` | the same |
| Port held; released by the owner before the retry | 433.9 s, released 19:15:53 | 471.0 s, released 19:44:23 |
| The printed command, run once: `/usr/libexec/celikpanel/recovery recover --retry --snapshot <snapshot>`, exit 0 (`Recovery dispatch admitted: attempt=owner`) | owner attempt 19:15:58, finished 19:16:40 | owner attempt 19:44:27, finished 19:44:52 |
| End | `succeeded/update_verified`, `previous_failure: recovery_failed`, read 19:16:40Z | the same, read 19:44:52Z |
| Site, SMTP, cron (`S/NN-verdicts`) | never interrupted | never interrupted |
| Deferred mail work of the Panel started inside the retry | `panel[57754]` 19:16:21, attempt 1 completed 19:17:05 | `panel[78472]` 19:44:36, completed 19:45:18 |
| The kind's 17 rules (`result.json` `kind.judged`) | `as-expected`, no finding, none unknown | the same |

Root CLI at the pause, EN (the candidate's text; `views.cli.texts`, Turkish beside it): "The update was applied, but
the new version's panel did not come up, and completing the update was retried to its limit. Read the panel log on
the server: sudo journalctl -u celikpanel-panel -n 50. Retrying repeats the same start until that cause is fixed.
There is no supported return to the previous version from this point. Automatic recovery used all three attempts
without finishing, so the server may be between versions. The server owner must act: read sudo journalctl -u
celikpanel-release-recovery.service --no-pager -n 50, fix the cause it names, then run the one-time same-operation
retry command shown there. Checking status does not retry recovery. ..." The post-update facts (a)-(e) are not part
of these two cells, as in set3.
