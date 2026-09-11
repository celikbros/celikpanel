# Mail host certificate validation — 2026-09-11

*[Türkçe](README.tr.md)*

This evidence covers the distinct server mail certificate lifecycle used by the
`web_mail` setup profile. It does not establish full-profile or public ACME
acceptance.

## Disposable Debian fixture

The test ran on the isolated Debian 13 QEMU fixture at localhost SSH port 2241.
It required the fixture marker and refused to run if a panel binary existed.
No installed customer panel was updated. The fixture contained real Postfix
and Dovecot, a test-only CA in the system trust store, and the exact test hostname
`mail.setup.celikpanel.test`. The VM was stopped after verification.

The test used the production protected Certbot-source reader, immutable host
certificate generation, durable mutation commit, daemon configuration/reload,
and queued renewal export. It opened real SMTP submission STARTTLS on 587 and
IMAPS on 993, checked system trust and hostname, and compared the exact served
leaf digest. It did not send mail or authenticate a mailbox.

- [Initial publication](initial-publication-before-queue-fix.log) proved the
  first trusted certificate on both listeners, then exposed a renewal queue
  ownership mismatch. This log intentionally retains the failing result.
- The writer and remover now follow the same service-group ownership contract
  as the protected queue reader. A regression uses a nonzero service GID.
- [Renewal after the fix](debian13-renewal.log) passed real daemon convergence
  to the second certificate, preserved fallback certificate bytes, emptied
  the queue, and accepted an identical deploy-hook replay without another
  certificate change. No panel or license manager participated.
- [Final readback](debian13-receipt.log) matched the exact terminal mutation
  receipt to the protected current-generation receipt and the actual listener
  certificate. Renewal request: `404c857313331b277931e62abda1fcce`.

The first and renewed SHA-256 leaf digests are recorded in the renewal log.
The full original fixture test was not rerun from an empty VM after the fix;
the same fixture resumed its pending second generation and received a separate
final receipt check. The final test binary digest is in
[acceptance-binary.sha256](acceptance-binary.sha256).

## Failed-configuration regression

A failed mail TLS intent journal could previously be mistaken for the last
successful hostname/SNI configuration during unattended renewal. Successful
normal or restart convergence now persists a separate protected committed
snapshot. Failed proposals cannot replace it; host publication checks its
identity after mutation admission and before staging. The snapshot remains
usable when old mutation history is pruned.

The independent regression reproduces both failure without any earlier success
and failure after a successful configuration, before and after manager restart:
[before the fix](snapshot-regression-before-fix.log),
[after the fix, with race detection](snapshot-regression-after-fix.log).

## Final source checks

- [Agent and host-command tests](final-agent-hostcmd.log).
- [Mail lifecycle and closed-contract race tests](final-mail-race.log).
- [Vet for agent, panel, and host commands](final-vet.log).
- [Relevant final source hashes](source.sha256).

These checks follow the earlier full repository test/vet run. The temporary CA
fixture does not prove public ACME issuance, a real Certbot timer renewal,
Internet mail delivery, DNS/PTR prerequisites, or complete `web_mail` setup.
Those require their own external prerequisites and acceptance evidence.
