# Native mail shared-contract acceptance: AX

D-025 invariants 1/2/3, P0.4 shared artifact agreement and P0.5 workload continuity.
Recorded 2026-09-22 in the guarded disposable Debian 13 QEMU cell AX.
No installed owner server was accessed or updated.

Source `9a07aa9fbcff6647dddb42de359cf68d4ceaa724` was archived into a private
ext4 directory, compiled with its exact build commit, and staged through the lab
QEMU/SSH/nonce/DMI guard. The test binary SHA256 and bounded public native journals
are retained in [MAIL-CONTRACT-AX.json](MAIL-CONTRACT-AX.json). Recheck with:

```sh
python3 deploy/e2e/release-recovery/verify_mail_contract.py deploy/e2e/release-recovery/MAIL-CONTRACT-AX.json
python3 deploy/e2e/release-recovery/test_verify_mail_contract.py
```

The verifier checks recorded evidence consistency; it is not cryptographic host
attestation. Private disks and original evidence remain in the lab directory.
No private certificate material or lab nonce is included in the public record.

## Executed transitions

1. Native apt installed Postfix 3.10.13, Dovecot 2.4.1 and Certbot 4.0.0. The
   test keeps real package-manager and host-readiness probes. Its Agent runs as
   root with Group=celikpanel; synthetic Certbot output explicitly models native
   root:root ownership, rather than weakening the source reader.
2. The real Agent CLI initialized the empty fixture mutation ledger. The native
   Go acceptance test converged Postfix/Dovecot, published the first mail host
   generation, then deployed a second generation through the real Agent renewal
   path. SMTP submission STARTTLS and IMAPS performed system-trusted handshakes
   with the exact new leaf. The fallback certificate stayed unchanged and the
   fulfilled pending queue was removed.
3. A fresh process checked the exact successful mutation ID, published phase,
   immutable receipt, current leaf, empty pending queue and both listeners.
4. An orderly reboot changed boot ID. Native enabled Postfix/Dovecot returned
   active and served the same renewed certificate before any test process ran.
   Neither `/opt/celikpanel/bin/panel` nor `/opt/celikpanel/bin/agent` existed.
   Only `/run/celikpanel` was recreated afterward for the management test lock;
   this is explicit fixture setup, not native mail recovery. A fresh test process
   then repeated the receipt and trusted listener assertions.
5. The owner-drift test explicitly selected the retained older valid generation
   and reloaded the daemons. Replaying the queued newer source was refused with
   owner-review guidance. The selected generation and queue bytes stayed intact,
   the historical succeeded job remained succeeded, and both native listeners
   continued serving the owner's choice. No automatic overwrite or duplicate
   issuance occurred.

## Defect corrected

Previously, if the computed renewal request already had a succeeded job, the
Agent cleared the pending queue even though it had already observed a different
current leaf. Historical completion is not present convergence. That branch now
preserves the pending queue and owner selection, and requires owner review.
Receipt v1, pending representation and request identity are unchanged. This is
same-build exact-operation replay protection; it does not establish a complete
cross-version owner-drift enrollment contract for a future independent helper.

## Preliminary failures retained

AW was stopped without being relabelled a success. Its first attempt correctly
refused an uninitialized fixture ledger. After real initialization, its next
attempt correctly refused synthetic Certbot source inherited as root:celikpanel.
AX uses fresh state with these fixture preconditions established explicitly.
The native journals also report a Postfix spool resolv.conf ownership warning;
TLS handshakes and daemon checks passed, but this trial does not certify general
mail delivery, authentication or resolver-copy ownership.

## Limits and remaining work

The fixture CA is locally trusted and produces Certbot-layout files; no public
ACME issuance was performed. Renewal executes actual Agent code in a test binary;
it is **not** an independent native renewal helper, nor an invocation of the
installed Certbot deploy hook. No power loss, failed reload, owner edit race,
Arch mail acceptance, whole panel removal or update/rollback of a retained mail
helper was exercised. P0.5 remains open for owner-approved binding, durable
independent retry/recovery, hook migration and management-binary-absent renewal.
Local shared-package and Agent mail/TLS/source regressions passed with race
detection, followed by vet. Those tests complement the native evidence rather
than replacing the remaining lifecycle acceptance.
