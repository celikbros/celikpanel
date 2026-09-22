# Initial mail timer enablement — native BE Arch evidence

The guarded disposable BE Arch guest proved native timer enablement and ordered
compensation after two real process kills. The timer remained inactive throughout;
this is not evidence of running renewal or complete production enrollment.

Source: `ddb25e88fe0462039e455e66b9ccb7ae59a063ad`.
[Exact artifacts, bounded log and native observations](MAIL-ENABLE-BE.json).

The controller verified registered QEMU process/arguments, loopback SSH, pinned
host key, cloud-init identity and matching DMI UUID. Management binaries were
absent. It prepared an empty protected `timers.target.wants` directory separately
because the bare Arch guest lacked one. This is fixture preparation; production
creation/recovery of that parent is not established by this trial.

The accepted initial file/load plan was completed before enablement. A staged
symlink identity and durable enablement plan bound the subsequent native change.
After publication and actual daemon-reload, SIGKILL stopped the first process.
Native systemd showed the exact inactive/static service and enabled/inactive timer.

A second process reused the same plan, recorded inverse intent, moved the exact
link back and reloaded systemd, then was also killed. The timer was verified
disabled/inactive; the unit files were still present at that cut. The third
process completed the enablement inverse first, then inversely restored the
native files and reloaded systemd. Both units, native files and enablement link
were absent. The protected wants parent retained its inode and metadata; its
private stage/link evidence remained retained. No timer was started.

Operation: `33a42c0d7f244dad97a5d88ef20d8cce`.
Capture: `b2994da89287dedb28e68cdc8e6284e024e012d3b9a25c5c25f8c1a79369e5a7`.
Target: `aa985178411de49f48242e13109f0e8f93587a9a04f40c4192d854bc7be41ab6`.

Related file/load/enable race tests passed in 251.982s. Final enablement review
suite passed in 43.246s with vet; the isolated same-target inode replacement test
also passed after removing an unrelated extra-inventory refusal from its fixture.
Nineteen process-kill boundaries cover staging, plan publication, both rename/
reload directions, inverse intent and terminal record durability. Component tests
also preserve owner links/preferences and unknown native state, and reject file
compensation before the exact enablement inverse. No installed server was changed.

Open scope: parent creation, timer activity, historical application rollback,
production dispatch, mail workloads, reboot/power-loss and whole-update acceptance.
Run `python3 verify_mail_enable.py MAIL-ENABLE-BE.json` to verify this bounded
record. Initial idle load evidence remains separate in
[MAIL-BOOTSTRAP-LOADED-BE.md](MAIL-BOOTSTRAP-LOADED-BE.md).
