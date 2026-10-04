# BE native shared parent and automatic timer invocation

Source `9e4c2a628508f00925e545352be615328ba0c76d`, private parent/v1,
P0.3/P0.5 invariants 1, 2, 4. Guarded disposable QEMU Arch cell
`release-recovery__3688d130a0721786`. [Raw evidence](MAIL-PARENT-BE.json) is checked
by `verify_mail_parent.py`, including a fresh native invocation timestamp newer
than the trial start, source-bound artifacts and matching operation/capture IDs.

The controller preserved the prior fixture wants directory and retained evidence
under `/etc/systemd/system/.mail-parent-before-native-trial`, then proved the
fixed wants path absent. Product code staged, recorded and atomically published
that missing parent as root:root 0755. No owner metadata was normalized.
The group and empty private ledger parent were already prepared by the previous
fixture trial; this does not prove their production initialization.

The native timer started and automatically invoked the exact independent helper.
The invocation completed with exit 0, idle state and no pending work, while both
installed management binaries remained absent. SIGKILL after actual timer start
and again after actual timer stop were reconciled by fresh processes using the
same operation. Ordered inverse removed the recorded link and restored native
file/unit absence. The newly published shared parent and prior archived fixture
parent retained their exact inodes. No private ledger or pending job was created.

This adds missing-wants publication and actual automatic **no-work** success.
It does not establish certificate renewal, service delivery/authentication,
production dispatch, private-state bootstrap, application update/rollback,
historical Agent compatibility, reboot or power-loss acceptance. Earlier failed
fixture evidence remains in [MAIL-ACTIVITY-BE](MAIL-ACTIVITY-BE.md).
