# Existing native mail schedule loading and recovery: Debian BE

Source `e7c6623f8b1b188c3c0d0920c7ae418c85bbfd1a`, source-bound test binary
`6c8ee4871caeb37d734a19fc11d53c4849e015a80543459d039ce2f2898790f5`.
[Machine-readable evidence](MAIL-LOADED-BE.json).

The guarded Debian BE QEMU fixture used the actual hook/unit files, native
systemd manager and existing enabled/active independent mail timer. Management
binaries were absent. The controller held native release and host-mutation locks.
The exact-source Agent preservation test first verified the shared readiness
parser against the actual loaded schedule without changing the installed hook.

For operation `2dbbb7485fe846de98e972cf2fa654c1`:

1. Verified file publication and actual daemon-reload selected the candidate
   helper; SIGKILL interrupted the process before the loaded-state receipt.
2. A new process completed that same forward observation, inversely exchanged
   the files and actually reloaded the predecessor; a second SIGKILL interrupted
   rollback before its loaded-state receipt.
3. A third process completed the same rollback and reverified current native
   preferences, original file inodes and the predecessor's loaded command.

After each cut, an independent `systemctl show ExecStart` probe proved the actual
helper generation (candidate, predecessor, predecessor). Final native timer
preferences were unchanged, no daemon reload remained pending and no override
was loaded. Original file bytes/inodes/owner/mode/mtime, Postfix/Dovecot settings
and the operation ledger were preserved. CA/hostname-verified SMTP 587 and IMAP
993 continued to serve the same selected certificate.

This is existing-schedule native loading/inverse recovery with two process cuts.
It is not bootstrap enrollment, automatic recovery dispatch, application-update
rollback compatibility, reboot/power-loss acceptance or full P0.3/P0.5 completion.
No installed user panel was accessed or updated.
