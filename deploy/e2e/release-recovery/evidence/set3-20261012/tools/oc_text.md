Good candidate G; an owner-fixable host cause: a lab process holds `127.0.0.1:2083` from the moment the updater stops
the old Panel, so the new Panel cannot bind, the update pauses after its three forward attempts, and the harness then
does what the product's text tells the owner (free the port, run the one printed retry once).

| | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- |
| Owner start | 08:19:12Z | 08:28:09Z |
| Forward attempts 1, 2, 3 (`update/completion`) | 08:21:12, 08:22:53, 08:24:35 | 08:30:08, 08:31:54, 08:33:36 |
| Pause read on the root CLI (`recovery_required/recovery_incomplete`, `automatic_recovery=paused_retry_limit`, `first_failure_code=panel_start_unverified`, `renewal_before_update=on`) | 08:26:17 | 08:35:19 |
| Port held, released by the owner before the retry | 422.9 s, 08:26:43 | 439.5 s, 08:35:58 |
| The printed command run once, exit 0 (`Recovery dispatch admitted: attempt=owner`, "Previous pending update finalized from verified snapshot") | finished 08:27:10 | finished 08:36:26 |
| End | `succeeded/update_verified`, `previous_failure=recovery_failed` | the same |
| Deferred mail work of the Panel started inside the retry | `panel[57191]` 08:26:55.76, attempt 1 completed 08:27:38.55 | `panel[79975]` 08:36:11.36, completed 08:36:54.63 |

The kind's 17 rules are as expected on both (`result.json` `kind.judged`): the panel log the text names shows `Failed
to start panel listener: listen tcp :2083: bind: address already in use`; the recovery journal prints the one-time
command `/usr/libexec/celikpanel/recovery recover --retry --snapshot <snapshot>`; Certbot's timer is back as before.
Root CLI at the pause, EN: "The update was applied, but the new version's panel did not come up, and completing the
update was retried to its limit. Read the panel log on the server: sudo journalctl -u celikpanel-panel -n 50. Retrying
repeats the same start until that cause is fixed. There is no supported return to the previous version from this
point. Automatic recovery used all three attempts without finishing, so the server may be between versions. The
server owner must act: read sudo journalctl -u celikpanel-release-recovery.service --no-pager -n 50, fix the cause it
names, then run the one-time same-operation retry command shown there. Checking status does not retry recovery.
Automatic certificate renewal (Certbot) was stopped for this update; the same recovery journal says whether it was
returned to how it was before the update or stays stopped until this operation finishes." The Turkish text is in each
cell's `steps/12-owner-continuation-required/step.json` (`views.cli.texts.tr`). These texts are the candidate's: the
forward completion is run by the candidate's recovery runner. The post-update facts (a)-(e) were not repeated in
these two cells.
