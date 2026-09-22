# BE: native automatic mail continuation after reboot

Source `07cf2a1121262e54c86a98706ac33a5782cc318e`, local disposable Debian13
QEMU cell `release-recovery__3688d130a0721786`. The controller verifies the
registered QEMU process, loopback SSH endpoint, pinned key, protected guest marker
and DMI UUID. No production server or installed-panel update participated.

The existing capture/file/load protocol selected immutable renewal generation
`a4939ac8fcda2c9194f06de7fe5bdfec2955471d52d8398c391a92d849c6addb`, preserving the
enabled/active native timer preference. This was an explicit test-fixture
transition from the previous independent kit, not production bootstrap admission.
The selected helper SHA-256 is
`70d5bc97427328e17f9700d7bd94c98a211953966a9b18e842a1e585846daad9`.

The actual renewal process was killed after durable publication intent and before
selecting its staged certificate. Request `73de573861c1cbc1ea8c7e355929092c`
remained running at attempt 1; the old certificate still passed trusted SMTP/IMAP
probes. The timer was stopped for the cut but remained enabled. A guarded normal
VM reboot changed boot ID from `47c9fa4d-0d12-4533-9b79-59a638fe4673` to
`b8691874-0b1c-43d6-8fa3-ab32100cec0a`. No helper or service was manually started
after reboot. Native systemd started the timer and its installed one-shot helper;
the same request completed at attempt 2.

Panel and Agent management binaries were absent throughout. SMTP 587 STARTTLS and
IMAP 993 both passed CA and hostname verification with new leaf
`2de247fc73a06d582bb53c9cc90a1439d7da22da05f1491a381eeb8b20d6f5f7`.
The old selected generation, immutable before-image, unselected stage, owner mail
configuration and unrelated ledger jobs remained unchanged. The exact pending
queue was acknowledged and the timer remained enabled/active.

Two preparation failures are retained: a controller used a wrong lock filename,
then its placeholder substitution changed the trial-ID environment variable.
Both were rejected before native file selection. Separate continuation intents
used the genuine transaction lock and explicit trial identity. Neither failed
preparation counts as fault acceptance; no validation was relaxed.

[Public evidence](MAIL-UNSELECTED-BOOT-BE.json) includes bound kill/enrollment logs,
both boot identities, native journal events and artifact identities. Run
`verify_mail_unselected_boot.py` and its negative tests to check the evidence.

This closes this bounded P0.3/P0.5 normal-reboot and automatic-dispatch acceptance
case. It is not power-loss durability, production enrollment, old-application
rollback compatibility, cross-build active-request adoption, Arch mail acceptance
or the complete workload/fault matrix. The helper and test binary use the same
source identity; the prior independent kit had no pending work when replaced.
