# Native mail file exchange and interrupted compensation: Debian BE

Source `02b56b715c5b32946f04df2638fe82076c455af5`, test binary
`04f1fa749c81723afaa8cef07400c64578ae77e9aaa53d8c1a9e2ff71a0c9644`.
[Machine-readable evidence](MAIL-FILES-BE.json).

The guarded disposable Debian BE fixture used the real `/etc` hook/service/timer
files and immutable native runtime, with both management binaries absent. The
controller held the actual native release and host-mutation locks. The additive
candidate kit was prepared before any file exchange. No installed user panel was
accessed or updated, and no production enrollment dispatcher was enabled.

The first driver invocation omitted the target environment variable because its
placeholder replacement also changed the variable name. The test refused before
capture or native mutation. That failure is retained. The corrected invocation
first proved every original native file unchanged and the capture journal empty.

The actual protocol then completed these boundaries for one operation:

1. Forward exchange of the native service and hook; SIGKILL immediately after
   the hook exchange, before that step's directory sync.
2. A new process persisted rollback intent and inversely exchanged the hook;
   a second SIGKILL interrupted compensation at that boundary.
3. A third process completed the same rollback, restored original native inodes,
   retained before-image/plan/stages, and refused to reverse the rollback direction.

Original inode, bytes, mode, owner and mtime were verified for all three native
files. Postfix/Dovecot configuration and the operation ledger were unchanged.
The timer remained enabled/active with no pending daemon reload. SMTP 587 and
IMAP 993 passed CA/hostname verification and served the same selected certificate.
Actual Certbot hook discovery found only the live hook and excluded the retained
staging directories; installed package source inspection confirms the file filter.

This closes only the real file-exchange/two-process-fault item. It does not prove
activation of a new loaded unit, bootstrap enrollment, whole-update integration,
reboot/power-loss recovery, owner removal or the complete P0.3/P0.5 matrix.
