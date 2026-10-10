**Result in one sentence.** All ten cells of the update from the published v0.1.0-alpha.81 to a candidate built from
`67b62cc0f` reached the end set3 measured on `cfa329676`, with the same verdict for every step, the same outcome and
final state, and the same value for every compared fact (`compare-with-set3.md`: no difference): verified on Debian
13, Ubuntu 24.04 and Arch; automatic return to alpha.81 with the ledger at the released 42 after a migration defect
with a second fault (three platforms) and after a failed start check (Debian); the owner's one printed retry
completing a paused update (Debian, Ubuntu); the workloads served with management off across a reboot (Debian). A PHP
site created by alpha.81 answers ten requests with the same status and body before the update, after it and after
its vhost was rendered again (Debian, Ubuntu). One Postfix Stop through the Panel with a refused `main.cf` answers
`200` with the note `unit_marked_failed_config`, leaves the unit `failed`, issues no `reset-failed`, and Start works
after `main.cf` is restored (Debian: `postfix.service`; Ubuntu: `postfix@-.service`). No step and no check failed;
nothing is NOT-MEASURED among the ten cells. Each cell ran once, with RAM-backed guest disks.
