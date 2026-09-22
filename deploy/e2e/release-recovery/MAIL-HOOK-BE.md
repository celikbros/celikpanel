# Native mail hook compatibility ? BE

Source `4277a1e32f338bf81b53e39c927f5c1aca6437f6`; guarded Debian13 QEMU with management binaries absent.
The actual certificate writer preserves a verified independent hook without
changing its bytes, inode or modification time. The actual loaded native service
and enabled timer match the immutable kit. An explicit fixture owner comment
causes refusal without rewriting the hook or changing mail config, units or the
mutation ledger. After the fixture owner removes exactly that comment, verification
passes again and trusted SMTP/IMAP serve the unchanged selected certificate.

An earlier source `3c268c27dfd77cd9cb73771486d231884340e711` rejected root-owned
mode-0700 hook directories retaining the producer's celikpanel group. That failed
observation is retained. The corrected read-only compatibility rule preserves
this metadata and keeps exact immutable-kit/file validation. No permission or
ownership normalization was used to make the test pass.

This proves writer compatibility, not native enrollment, full panel rollback or
power-loss recovery. [Safe raw evidence](MAIL-HOOK-BE.json).
Run `python3 verify_mail_hook.py MAIL-HOOK-BE.json`.
