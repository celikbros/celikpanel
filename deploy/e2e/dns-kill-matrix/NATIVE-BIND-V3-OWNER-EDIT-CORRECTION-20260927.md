# Native BIND V3 owner-edit pending correction, 2026-09-27

## Bounded result

The [previous managed-block owner-edit trial](NATIVE-BIND-V3-OWNER-EDIT-20260927.md)
detected a conflict but left the exact accepted V3 deletion `running` and its
mutation manager poisoned. A fresh disposable Arch-primary/Debian-13-secondary
BIND pair tested the corrected Agent. One same-identity recovery call returned
the deletion to durable `pending/propagation-pending` with
`error_code=dns_peer_owner_edit_unknown`, no active request or lease, and the
owner's BIND config bytes unchanged. Native BIND continued serving catalog
serial 2 on both guests. Explicit owner reconciliation then removed only the injected
comment. One more same-identity recovery reached terminal publication and retired
the challenge. This passes the controlled between-attempt owner-conflict and
reconciliation cell. Other concurrent effects, terminal replay refusal, reboot
and the full Stage 2/P0.4/P0.5 matrix remain open.

This trial used only `/var/tmp/cp-stage2-bind-owner-fix27`, distinct QEMU host
ports 2321/2322/23953, and new qcow2 overlays backed by copies of the
verified official Debian 13 and Arch images. The prior two owner-edit roots,
`/var/tmp/cp-native-inspector26`, other Stage 2 roots, Boston, Frankfurt, and
all installed panels were untouched. The peer remained panel-free. The pair was QMP-stopped after terminal proof; its overlays and raw evidence
remain. After the pending owner-edit verdict, the only further mutation was
explicit single-comment reconciliation and one same-ID recovery.

## Candidate and baseline

The focused Go tests
`TestPendingExactBINDV3OwnerEditRequiresCommittedDeletionIdentity` and
`TestDNSZoneV3NativePendingReasonPersistsAcrossLedgerReload` passed locally
before this native run. The corrected disposable Agent binary SHA-256 was
`fd0fa56d1c882bb44e02e86bbe43f3fb21e7cb98fa725c528af62f870721d6a9`,
verified again at `/opt/celikpanel/bin/agent` in the Arch guest. Its tagged
fault binary SHA-256 was
`37074a0a6ccf3deafc3dafbf8efab842603e55fb8f6c5decc202bda3e5fbc6a8`.
The trigger and peer inspector hashes were
`5c5c2130201f97cd191e65a5165be99bd8ba6663a86d758ce4bbdafe2efd5716`
and `9b83db38b1cf92366d0675811d702d9bd44df37e23759f5802a6749684236721`.
These were built/copied from a shared dirty checkout, not a signed release.

The exact `bind__intent__after-write__paired-primary__peer-reachable` baseline
passed a real SIGKILL exit 137, same-request Agent convergence, native peer
transfer and 31 healthy stability samples. The sealed baseline `result.json`
SHA-256 was `4cfef7daf3c456b9772d2be3c17f8c0094bad15cf73185bf3bce03dbc54690ca`;
`kill-proof.json` SHA-256 was
`d8da8d4297bdf4ede2e02c7320312f637c40b66c708c5b387901e5163acb074a`.
Before V3 deletion, `rndc zonestatus s1-kill.test` on the secondary showed a
loaded secondary zone at serial 2026083101, and both native named services
were active.

The native inspector was enrolled with a pinned peer host-key digest
`7cc55729ea7ae3d86dec4b7ab1bfc9548032539b04bde360b8b896964a9d0a80`.
The production Agent enrollment record SHA-256 was
`219b4b7cf612971ecb8531800411e5c324e25f3a8db97ed79dd2ae39208a3d1a`.
Only the disposable secondary inspector's authorized key was temporarily
moved aside for the initial proof attempt, then restored before owner edit.
This forced the first production `rpc-delete-v3` call to leave the exact
deletion pending with `dns_peer_inspection_unknown`. No second deletion was
started. The native secondary catalog advanced to serial 2 and unloaded
`s1-kill.test`; `REFUSED` was not counted as absence proof.

## Owner edit and exact recovery

The pending deletion identity was request
`14b0d5712bca0f77e7556b215e9c2bb2`, owner
`09b5c4771c3831471d1357609f48217f`, qualifier
`dns-zone-sync/v3:sha256:25853f7cede95221ec9895ddd6cddad865e8d3ef3eb1959c20fb57b571996c91`.
Attempt 1 was durable `pending/propagation-pending`, with no active request and
retired lease. The clean primary `/etc/named.conf` SHA-256 was
`d16ee58e0f99bf245c7994fbe767a3aa4da1ba4e39b5083cdaa30fdbe0214270`.

The same bounded [guest-only edit fixture](native_bind_owner_edit.py), SHA-256
`af55ab86da95f2b0b21429445bcd1fa82b3ce8387392e460b67dfa95db9c7eea`,
checked the exact guest marker, sole pending V3 job, root-owned mode-0640
config, and expected preimage. It inserted
`// owner-edit-stage2-managed-block-20260927` immediately after the managed
zone-block begin marker, preserving inode 77236. The config SHA-256 became
`4d5515f54996890e6d9f3430f22cfaed0e62b8257b4d47a0c1f953b81186cdb9`.
`named-checkconf` passed. No BIND reload was requested merely for this edit;
native primary catalog SOA still answered at serial 2.

Exactly one `rpc-delete-v3-recover` invocation then used the saved scenario
and measured identity receipt. It exited 0 and reported
`outcome=pending_exact_operation`, the same request/owner/qualifier, exact
`commit/dns-zone-sync/v3/propagation-pending/...` phase, and zero heartbeats.
The durable ledger v1 held only the completed BIND switch and this V3 job.
The V3 job advanced to attempt 2 but returned to `status=pending`, with
`error_code=dns_peer_owner_edit_unknown`, no `active_request_id`, and
`lease_expires_at=0001-01-01T00:00:00Z`. Its SHA-256 was
`5525e68d0f9c72a8e8cb039901bf0521b6ab51c9594f67ced0c6d9ed4ee09a94`.
Agent log recorded `DNS zone V3 recovery remains pending for s1-kill.test:
dns_peer_owner_edit_unknown`; it did not report a poisoned mutation manager.
The unchanged owner config hash and comment were read back after recovery.

At `2026-09-26T23:37:09Z`, primary Agent and native named were active; the
peer's native named was active without Panel/Agent binaries. Peer
`rndc zonestatus` showed secondary catalog serial 2 and no matching loaded
`s1-kill.test` zone. Both primary and peer answered the catalog SOA at serial
2 over UDP and TCP. The primary state remained
`celikpanel-dns-engine-state/v2`, BIND epoch 1, primary role, with generation
`0c40230554b76c43dbd580aa06e048202c02db0f90c88dc56cf24ac72dc2dac3`
and state SHA-256
`79884c1232752c77b8709aa5d23c52e4821de16254ab550a677f896a54d01200`.
The source and result use the existing ledger v1 and state v2 schemas; no
schema migration, independent recovery authority or native service ownership
changed.

## Explicit owner reconciliation and terminal result

The guest-only native_bind_owner_reconcile.py helper, SHA-256
b66fbad01eca6f2458e7667aa15319ef0d2a64d09d717a533f1220c6d990d613,
required the exact Arch fixture marker, the sole idle pending V3 job with
dns_peer_owner_edit_unknown, and the edited full-file config hash
4d5515f54996890e6d9f3430f22cfaed0e62b8257b4d47a0c1f953b81186cdb9.
It removed only the injected comment after the managed block's begin marker.
Before writing, it proved that the resulting whole file would have the original
canonical SHA-256
d16ee58e0f99bf245c7994fbe767a3aa4da1ba4e39b5083cdaa30fdbe0214270.
This full-file equality also proves bytes outside the managed span were
preserved. Inode 77236 and mode 0640 remained; readback matched.
named-checkconf, active named and primary authoritative catalog SOA serial 2
were checked before recovery.

Exactly one later rpc-delete-v3-recover call used the same scenario and
measured identity receipt. It returned verified_published, job_status=succeeded,
and the exact commit/dns-zone-sync/v3/published phase. The durable ledger held
only the original completed switch and one V3 deletion job, now attempt 3
succeeded, with no active request, no error code and a retired lease
(0001-01-01T00:00:00Z). Its SHA-256 was
83b064167d47d77d12785029251072b7965a7fc5470cd93874ac2703ad586d62.
The exact /var/lib/celikpanel-agent-private/dns-peer-challenge-v1.json journal
was absent after terminal publication. The primary config stayed at its original
full-file SHA-256. Both native named services remained active, the peer catalog
remained serial 2 with the deleted member unloaded, and primary and peer each
answered authoritative catalog SOA serial 2 over UDP and TCP. No duplicate V3
job appeared. Post-terminal replay refusal was not tested.

The change affects constitutional invariants 1/2/3 and P0.4: an owner edit
inside the managed BIND block blocks acceptance of a committed V3 deletion,
keeps the exact operation available for owner review, and leaves native DNS
available. This cell then proves explicit owner reconciliation permits terminal
recovery of the same accepted deletion. It does not prove an Agent-independent
inverse or complete panel removal.
