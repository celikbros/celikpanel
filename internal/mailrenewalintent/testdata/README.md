# Prior native producer identity

Public leaf DER and bounded nonsecret job fields were read from the guarded BE
Debian fixture after the earlier native failed-renewal trial. Producer source:
`bbd81cc2d108fd0d79fd7fba6b144571bc6800e3`; helper SHA-256:
`a99cb644dd235e2dfe477768994167f1ea0f99ac4db050f95c973f06e56d2102`.
See `deploy/e2e/release-recovery/MAIL-FAILED-BUDGET-BE.json` for original execution.
The current public leaf SHA-256 matches that recorded trial. No private key was
read or copied. The test compares the shared identity helper with an actual prior
producer's request, owner and qualifier, rather than reimplementing its formula.
This does not establish before-image migration for that historical operation.
