Cell `upd1-debian13-mgmt-off-reboot` (`S/15-management-off/` to `S/18-management-return/`; `result.json` `kind`). After
the verified update (`succeeded/update_verified` read 19:57:03Z) the owner runs `sudo systemctl disable --now
celikpanel-panel.service celikpanel-agent.service` (exit 0; both units `disabled` and `inactive`) and `sudo systemctl
reboot` (a new boot id; SSH back after 8.3 s). For the 185 s window of the new boot with both units `disabled` and
`inactive` (38 samples, one every 5 s; `workload-management-off-after-reboot.json`):

- the owner's database row and SMTP are served from the first sample, 7.8 s after boot; the site's first sample
  fails and it is served from the second, 12.8 s after boot, with no failure afterwards (set3's cell reads the same
  way: one failing sample before the first good one, at 13.0 s);
- cron wrote 3 stamps in the boot, no stall;
- `certbot.timer` stayed `enabled` / `active` (verdict `as-before`);
- `table inet celikpanel_fw` is present and equal to before;
- nothing needed the Panel (`needed_panel: []`).

`sudo systemctl enable --now celikpanel-agent.service celikpanel-panel.service` brings management back: the Panel
answers `starting` at 20:00:51Z and `ready` at 20:01:06Z (6 reads), a fresh login works, and the owner's state
(domains, cron, mailbox, database, version, update and recovery status) shows no difference from before
(`differences: []`). The kind's 11 rules are `as-expected`, no finding, none unknown. The update part of the cell:
site, SMTP and cron never interrupted, the Panel down only during the transaction (`S/20-verdicts/step.json`, judged on
the samples up to management-off). The post-update facts (a)-(e) are not part of this cell, as in set3.
