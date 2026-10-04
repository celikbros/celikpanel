# BD: separate native mail renewal executable

Source `77e71cbde4080dcd69eaa87961a3b00d8f4b15d3`;
helper SHA-256 `9aa59dd3650d4c70fee4e6e6112ef1d4226f9dff8075e3a65ba8565a6cf45cf4`.
[Recorded evidence](MAIL-EXECUTOR-BD.json) has a dedicated verifier.

A fresh guarded Debian 13 QEMU fixture prepared accepted native mail configuration
and two fixture-CA Certbot source generations. The setup test published the first
leaf, then exited before queuing or renewing the second. Separate native units
ran only the independently compiled `mail-renewal --queue` and `--process-pending`.
Both succeeded. No installed Panel or Agent executable existed or participated.
Real SMTP/IMAP listeners presented the second trusted leaf. Native configuration
bytes and mtimes stayed unchanged. Exact published receipt and successful ledger
job matched; pending work was removed. Same-source replay acknowledged completion
without changing the canonical ledger.

Negative executable trials in that guarded guest refused ordinary updater and
initialization modes, arbitrary supervisor shell execution, and development path
overrides before any config/ledger/selection change or init directory creation.
The initially proposed local-host smoke was not run after automatic review denied
its potential host mutation; only the isolated guest executable trial ran.

After orderly reboot, native SMTP/IMAP still served the new trusted leaf before
any management runtime was recreated. The volatile `/run/celikpanel` directory was
absent. This exposed an explicit remaining dependency: a queued same-source replay
then refused acknowledgement, preserving queue, ledger and native configuration.
Independent boot-time runtime preparation and owner enrollment remain open.

This proves one separate-executable native renewal, not production packaging,
public ACME, a deployed independent Certbot hook/timer, group-removal compatibility,
independent interrupted-operation recovery, power loss or the full absence matrix.
The helper uses retained shared native identity and private evidence initialized
by setup. P0.3/P0.5 remain partial. Full normal Agent race passed in 195.267 s;
helper-tag entry tests, build and normal vet passed. Original logs retain the
Postfix spool resolv.conf warning. Evidence checks consistency, not attestation.

No installed owner panel changed. Lab state, original logs and intents are retained.
