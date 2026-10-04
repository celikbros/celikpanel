# BE: composite native mail enrollment and compensation

Source `298ae2c34754d9ce219081475ec35fc0a5eee24a`; guarded local Arch QEMU
`release-recovery__3688d130a0721786`, SSH loopback only. No installed owner panel
was updated. Management binaries remained absent. [Machine evidence](MAIL-ENROLLMENT-BE.json)
is checked by `verify_mail_enrollment.py` and negative-evidence tests.

The test enrolled the exact independent helper generation
`d42f02a6fa81f4fbfbd13f1a5daa8b29ec46474b82dae742594c5828e538998f`
through the new composite executor, with both actual inherited native locks and
a protected test intent. Operation `6be0a33f3fef07e1497a5c235a1d7402` was killed
with SIGKILL immediately after native timer start. A fresh process verified the
same forward operation. Explicit compensation was then killed after the native
enablement link moved back to its stage. A fresh process verified the exact inverse.

The fixture exercised real systemd, native files, daemon observations and timer
activity. Final observations prove both fixed units absent/inactive, hook and live
enablement link absent, management still absent, and the original shared wants
parent retained. Its existing entries remain; the additional exact recorded stage
and returned link are intentionally retained for recovery evidence. Current inode
and target checks verify that retained link. This is not an extra live enablement.

Two preparation-only attempts were rejected before this native trial: the first
used a source directory name outside the CLI contract; the second used 0700 where
the immutable kit requires 0755. Both are preserved. The third controller completed
all four native phases but its final whole-directory inventory equality incorrectly
rejected the intentionally retained stage. A separate read-only audit checked
that exact recorded stage against the earlier parent inventory and verified the
final native state. That failed controller is retained too; it is not relabelled
as a successful initial run. The phase return-code assertions had passed before
its final inventory assertion, and the original phase logs are preserved.

This adds combined native execution proof to 88 local process cuts. The fixture
has no real mail workload, reboot or power-loss trial, authenticated production
admission or cross-invocation production fence. Its protected test intent is not
a shipping dispatcher. Those enrollment and P0 acceptance boundaries remain open.
