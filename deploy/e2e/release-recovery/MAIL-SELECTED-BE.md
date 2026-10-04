# Independent selected-renewal recovery: Debian BE

Source `7dcd9fc72f2e543f974389f02ce36e7d81be7535`, cell
`release-recovery__3688d130a0721786`. P0.3/P0.5; invariants 1, 2 and 4.
The existing v1 ledger, selected receipt and pending-source contracts are unchanged.
[Machine-readable evidence](MAIL-SELECTED-BE.json) is checked by
`python3 verify_mail_selected_recovery.py MAIL-SELECTED-BE.json`.

Two real SIGKILL trials stop the scoped producer after durable certificate
selection and before reload/terminal completion. In each case Dovecot still
served the previous leaf while SMTP had read the selected new leaf. This
observed split demonstrates why selected material alone cannot acknowledge
completion. Native Postfix/Dovecot configuration bytes and modification times
were preserved; installed management binaries were absent throughout.

The first trial explicitly stops Dovecot as a fixture-owner action. The separate
helper refuses recovery without starting it or changing ledger/queue bytes.
After explicit owner restart, another helper process reloads the two services,
completes the original request `be1377388f780358bbd3f5424c1abd07`, preserves all
foreign history and acknowledges the queue. Both trusted listeners serve the
new leaf. No ordinary Agent recovery constructor is invoked.

The second trial repeats the actual kill with a different source and request,
then reboots with the native timer enabled but stopped. Boot changes from
`be151cee-97ee-4d8c-b4b4-05abfa475e0f` to
`1627ab8d-b4d0-4e30-9811-4f4b250de357`. The installed immutable kit's timer starts
its sandboxed helper automatically and completes the same retained request
`123206fc903e1e20a2cd34e99b408def`. Both listeners, queue acknowledgement,
foreign history, original configuration and the executed helper digest match.
The previous immutable kit remains retained.

The initial observer incorrectly probed SMTPS/465; this fixture uses
STARTTLS/587. The original kill was not repeated: its unit journal and durable
checkpoint were read again through the correct protocol. An early postboot
observation before the timer fired also remained inconclusive; the later
record proves actual timer invocation and completion. Private raw observations
remain under the guarded lab root, including those unsuccessful probes.

Full Agent race tests passed (195.760 seconds), vet and helper-tag tests passed.
Kit installation here is an explicit disposable fixture action. Production
enrollment/migration/removal, pre-selection recovery, bounded automatic retry,
public ACME and power-loss acceptance remain open. This is neither a production
server update nor closure of the full P0 plan.
