# Native owner-reviewed mail recovery cleanup: retained AY

The stopped AY fixture was restarted through the existing QEMU/SSH/nonce/DMI
lab guard, preserving its native certificate, owner-selected earlier generation,
real Postfix/Dovecot and unrelated pending renewal. New source
`299d0a786ca5b4db9c787bc675d41ec4e84df99c` was archived privately and compiled into
an opt-in Agent test binary, SHA256
`af4f4488273b3d4ee8d98a5a1f88ccc4675943692fe97b736b9c2b0a48b2efb8`.

The test acquired a real durable mutation and host step, prepared an uncommitted
certificate generation, then explicitly added an owner-note file. The actual
`reconcilePersistedMailHostCertificateHostAt` path refused cleanup; note, key,
certificate and receipt remained. Both native TLS listeners still served the
owner's selected leaf. The fixture then explicitly removed its owner-note,
representing owner resolution, and retried the same operation. Only its exact
unselected generation was removed. The test explicitly finished that abandoned
operation as failed; cleanup did not mean a successful certificate publication.
The unrelated pending renewal bytes and served leaf remained unchanged.

This is a real-filesystem/native-daemon test of controlled uncommitted-stage
recovery, not SIGKILL, power loss, startup-dispatch recovery, or proof of a prior
failed attempt's UI history. No ACME request, service reload, new publication,
installed Panel or installed Agent was needed for this cleanup. The volatile
management test-lock directory was explicitly recreated before the test.

[Public bounded evidence](MAIL-CLEANUP-AY.json) binds the retained AY record,
executed binary, new boot and exact operation. Recheck with:

```sh
python3 deploy/e2e/release-recovery/verify_mail_contract.py deploy/e2e/release-recovery/MAIL-CLEANUP-AY.json --base deploy/e2e/release-recovery/MAIL-CONTRACT-AY.json
python3 deploy/e2e/release-recovery/test_verify_mail_contract.py
```

The seven verifier tests include both native renewal records and cleanup,
plus rejection of changed scope, binary, base fixture, false success and changed
served leaf. This is recorded-evidence consistency, not host attestation. No
private keys or nonces are published. AY was stopped again with disks/evidence
retained. Installed owner servers were not touched. P0.4 interrupted-cleanup
and P0.5 independent renewal remain open.
