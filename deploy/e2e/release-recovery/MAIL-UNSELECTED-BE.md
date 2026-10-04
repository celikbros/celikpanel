# BE: interrupted unselected mail renewal

Local disposable Debian13 QEMU cell `release-recovery__3688d130a0721786`.
Source `c93d2be8b90bd5c4fdefd9111ec74dfcf51054f6` includes admission source
`3b47f17` and recovery source `9181ffc`. Helper SHA-256
`1f37d15f2b2f80836e3249774c2a25225932a881fdef88205c98789db8316af6`;
test binary SHA-256
`d7a09ed1e845fa0021c8e24ea4ca50d1bf566c99ced3827f213cdc2167f23603`.

The controller verified the live local QEMU process/arguments, its loopback
`127.0.0.1:2841` management port, pinned SSH host key, protected cloud-init marker
and guest DMI UUID before each entry. No production server participated. Native
Postfix/Dovecot and the fixture CA remained in use; `/opt/celikpanel/bin/agent`
and `panel` were absent. The regular renewal timer was paused for deterministic
fault injection and restored at the end; this is fresh-process helper acceptance,
not proof that the new helper was automatically dispatched by that timer.

| Cut | Exact request | Successful execution attempt |
|---|---|---|
| Durable before-image, before active ledger | `d8c65d49030e94b258b9cdd07cf87d7a` | 1 |
| Active lease, before publication | `b8e1ecf645e92e24fdb5a177a0eb05a5` | 2 |
| Durable publication intent, before selecting staged generation; then another SIGKILL during interruption-result publication | `318bcc071cafcfcfdd3ea6c3235db59d` | 2 |

Four native systemd journals confirm actual `SIGKILL`, including interruption of
recovery itself. Three new source-bound helper processes exited successfully.
Before continuation, the selected material's inode/metadata inventory and trusted
SMTP/IMAP leaf remained unchanged. After continuation, SMTP 587 STARTTLS and IMAP
993 passed CA and hostname verification with each new leaf. The before-images
were byte-identical, unrelated ledger jobs and native configuration were unchanged,
and the exact unselected staged generation survived recovery. The final leaf is
`3c0bf4e30c71bb7b289bd41fffa6ff8c39e5f996ecedcfc1c037c71fba4aa100`.

Two preparation failures are retained and excluded from acceptance. The first
refused to overwrite previously existing source8 test material before queuing or
admitting work. The next test binary used the wrong linker symbol and recorded
`unknown` build identity: its before-image and actual kill were preserved, with
no active ledger job, but are not claimed as source-bound acceptance. The corrected
fixture refuses an unbound build before admission and links the actual tested
Agent package symbol. The already prepared source9 was reused; no conflicting
receipt was overwritten or adopted. The accepted trials use fresh trial IDs.

The public record is [MAIL-UNSELECTED-BE.json](MAIL-UNSELECTED-BE.json).
`verify_mail_unselected.py` validates scope, source/binary binding, four kill
journals, exact continuation requests, attempt counts, material chain, retained
stage evidence and trusted listener results. Its negative tests reject missing
preservation, changed budgets, missing kills and broader unsupported claims.
Private controller/guest journals remain retained in the disposable lab.

This advances P0.3/P0.5. It does not prove reboot/power-loss continuation,
cross-build adoption, missing historical before-image recovery, cancelled or
unknown phase recovery, initial production enrollment, old-application rollback
compatibility, Arch mail support or the complete native update/workload matrix.

The exact accepted source also passed the complete Agent race suite (199.293s),
plus mailrenewalintent, mailhoststore and servicemutationledger race suites.
The public mail evidence verifier suite passed 90 tests.
