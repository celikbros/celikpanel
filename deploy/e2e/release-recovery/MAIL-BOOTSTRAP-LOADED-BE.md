# Initial idle mail unit loading — native BE Arch evidence

The guarded, disposable BE Arch guest completed initial unit loading and exact
compensation after two real SIGKILLs, without installed management binaries.
This proves **idle loading**, not timer activation or completed enrollment.

Production source: `e3a9a19807e6a86845a2f6aea8d7a3769a44e7ba`.
Native test guard correction: `2c82acb39bd8f24c56bc4c9d7e7e8d0f7cae1a81`.
The second commit changes only the fixture identity check. The independent
runtime kit and preparation CLI remain from the first commit. Exact build,
manifest, final observations and retained refusal logs are in
[the evidence record](MAIL-BOOTSTRAP-LOADED-BE.json).

The fixture controller checked its registered QEMU PID/command, loopback SSH,
pinned host key, cloud-init nonce and DMI UUID before each command. It prepared
missing fixture directories and lock files separately; this preparation is not
production enrollment. There was no running mail workload in this Arch guest.
No Frankfurt/Boston or other installed-panel update was initiated.

1. Both native units and all three native files were positively absent. Fresh
   `systemctl show` returned code 4 with complete `not-found` observations.
2. The exact prepared kit, original absence and inode-bound file plan were
   retained under the native exclusive release/host locks.
3. After real `daemon-reload`, the driver was killed. Both exact fragments were
   loaded; the service was inactive/static and timer inactive/disabled. Native
   ExecStart pointed to the accepted independent helper generation.
4. Another process reconciled the same forward intent, inversely moved the exact
   files, reloaded real systemd and was killed again. Both units were absent.
5. A third process completed that inverse receipt for the same operation and
   before-image. Native hook/service/timer files and enablement link were absent.
   Management remained absent. No timer activation or renewal job occurred.

Operation: `8c662388f87f49718a70c7b3ac187add`.
Capture: `75e757ddbde675cc01dfe3c579510a0b08d58733905c2b49ede7bf0292fdde0a`.
Target: `3bf52eb8c0782bcb4bc4c3a7c28ef39ee95c58462aa8ec0a31dfbc3b16f2e71b`.

Two earlier preparation refusals remain recorded. First, the controller's tar
extraction under umask 077 created the source kit directory as 0700 instead of
its required 0755. Preparation refused before creating the installed kit. The
controller compared every source file with the retained archive before fixing
only its own extracted directory. Second, the test expected the unrelated setup
fixture marker; its guard refused before capture/native publication. The fixed
test uses the existing protected cloud-init release-recovery marker and UUID.
Neither refusal is counted as a successful fault trial.

Validation: shared kit and full independent recovery race suites passed
(1.037s / 383.340s); vet passed. Component tests cover 28 real process-kill/re-entry
boundaries across initially absent and legacy-hook layouts, simulated cache
mixtures, owner enablement/activity/override drift and unknown native results.
The native trial above adds real systemd cache behavior; it does not prove mail
workloads, reboot/power loss, production dispatch, timer enablement/start,
historical application rollback compatibility or whole-update acceptance.

Run `python3 verify_mail_bootstrap_loaded.py MAIL-BOOTSTRAP-LOADED-BE.json` to
check the bounded evidence. Retained native journals and both failed preparation
logs must not be removed or replayed as a new operation.
