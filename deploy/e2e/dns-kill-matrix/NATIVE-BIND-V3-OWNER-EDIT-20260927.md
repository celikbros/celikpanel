# Native BIND V3 owner-edit trial, 2026-09-27

## Scope and verdict

Two fresh Arch-primary/Debian-13-secondary QEMU pairs exercised the exact
`bind__intent__after-write__paired-primary__peer-reachable` fixture. The first
pair retained a nonconflicting owner comment outside CelikPanel's managed BIND
block and reached terminal deletion. That is useful tolerance evidence, but
does **not** close the owner-conflict acceptance item.

The second pair inserted a syntactically harmless comment *inside* the managed
block after a production V3 deletion had published locally and become pending
on peer proof. Same-identity recovery detected the modified block and did not
overwrite it or stop native DNS. It did **not** return the required reviewed
owner-edit pending state: the Agent poisoned its mutation manager and left the
durable V3 job `running/recovering` with an expired lease and its old
`dns_peer_inspection_unknown` reason. This is a **failed Stage 2/D-024 P0.4
acceptance cell**. No new deletion or retry was started after that result.

Both pairs used new qcow2 overlays from verified official image bases. The
nonconflicting pair root was `/var/tmp/cp-stage2-bind-owner27`, with host ports
2301/2302/23753. Its guests were stopped by fixture QMP after evidence capture;
the overlays remain. The conflict pair is retained at
`/var/tmp/cp-stage2-bind-owner-conflict27`, ports 2311/2312/23853, for exact
read-only diagnosis. Neither pair touched `/var/tmp/cp-native-inspector26`,
`/var/tmp/cp-stage2-pdns-peer27*`, Boston, Frankfurt, or an installed panel.

## Exact trial method

In each pair, `native_bind_peer.py` installed standard BIND on the panel-free
Debian secondary, and the Arch primary ran `guest_bootstrap.py`'s managed BIND
`intent/after-write` SIGKILL cell. The baseline passed with a real exit 137,
same-request convergence and 31 healthy stability samples. The native
inspector and Agent enrollment used an exact pinned SSH identity. Before the
production `rpc-delete-v3` call, only the disposable peer inspector's
`authorized_keys/celikpeer` file was temporarily moved aside, causing the
first V3 proof attempt to remain pending; it was restored before the owner
edit. Native named remained active throughout. The call produced one V3 job:

- request `14b0d5712bca0f77e7556b215e9c2bb2`, owner
  `09b5c4771c3831471d1357609f48217f`, qualifier
  `dns-zone-sync/v3:sha256:25853f7cede95221ec9895ddd6cddad865e8d3ef3eb1959c20fb57b571996c91`;
- attempt 1 `pending` at the exact `propagation-pending` phase with
  `error_code=dns_peer_inspection_unknown`;
- native secondary catalog serial 2, with `rndc zonestatus s1-kill.test`
  reporting no matching loaded zone. A DNS `REFUSED` reply was not used as
  absence proof.

The managed Arch path `/etc/named.conf` was proven before editing: root:named,
mode 0640, inode 77237, SHA-256
`d16ee58e0f99bf245c7994fbe767a3aa4da1ba4e39b5083cdaa30fdbe0214270`.
The second pair used the bounded guest-only
[`native_bind_owner_edit.py`](native_bind_owner_edit.py) helper, SHA-256
`af55ab86da95f2b0b21429445bcd1fa82b3ce8387392e460b67dfa95db9c7eea`.
It required the exact Arch fixture marker, this config preimage, and the sole
pending V3 job before inserting
`// owner-edit-stage2-managed-block-20260927` immediately after the managed
zone-block begin marker. It kept inode 77237 and yielded SHA-256
`4d5515f54996890e6d9f3430f22cfaed0e62b8257b4d47a0c1f953b81186cdb9`.
`named-checkconf` passed, and no reload was requested for the edit.

## Nonconflicting first pair

The first pair instead appended `//owner-edit-stage2-20260927` after the
managed zone-block end marker. Its config changed from the same preimage to
SHA-256 `bc8ca05dc712f1902f740f21cbd61737f41ef57da34cf85fc15f5450bd66abf5`.
`named-checkconf` passed, and same-identity `rpc-delete-v3-recover` returned
`verified_published/succeeded` at attempt 2. The owner comment and hash were
unchanged afterward; native named stayed active and the peer member remained
unloaded. The exact ledger contained the prior completed switch and one V3
deletion job. The source guard regenerates expected config from the current
file and deliberately preserves text outside the managed block, so this result
is consistent with tolerating an independent, nonconflicting owner edit.

## Managed-block conflict result

After the in-block edit, both primary and peer native BIND catalog SOA queries
returned authoritative serial 2 over UDP and TCP. The primary and secondary
`named.service` units remained active. The Debian secondary still had catalog
serial 2, reported no matching `s1-kill.test` zone, and had no Panel or Agent.
Primary `named-checkconf -p` SHA-256 was
`ed37043b02ea0c58770808a888a06ac4ff0a0f6e2c25d2508fd138fa3998274c`.
The owner config remained at SHA-256
`4d5515f54996890e6d9f3430f22cfaed0e62b8257b4d47a0c1f953b81186cdb9`
after the same-identity recovery call: no Agent rewrite was observed.

The recovery trigger returned exit 1,
`outcome=unverified_exact_operation`, `job_status=running`, and the exact
`commit/dns-zone-sync/v3/recovering/...` phase. The durable ledger v1 had only
the prior completed switch and this V3 job, now attempt 2 with
`active_request_id=14b0d5712bca0f77e7556b215e9c2bb2`, lease expiry
`2026-09-26T23:11:40.772571651Z`, deadline
`2026-09-26T23:56:20.617843484Z`, and the stale
`error_code=dns_peer_inspection_unknown`. Its SHA-256 was
`1b281342534a9dd7249d63328e136da167391e2a99c9b448ec313d32e854d7b7`.
The Agent log said `prepare BIND zone include: existing CelikPanel BIND zone
include was modified`, followed by `service mutation manager is fail-closed
after an ambiguous ledger write`. The exact pending owner-edit code and lease
retirement were not observed. No duplicate V3 request or peer mutation was
launched, but terminal replay refusal was not tested in this failed cell.

The primary's active state remained `celikpanel-dns-engine-state/v2`, BIND
epoch 1, primary role, generation
`0c40230554b76c43dbd580aa06e048202c02db0f90c88dc56cf24ac72dc2dac3`,
catalog serial 2. State SHA-256 was
`79884c1232752c77b8709aa5d23c52e4821de16254ab550a677f896a54d01200`;
the current generation pointer resolved to the same generation. The switch
journal was absent. Agent binary SHA-256 was
`0a5f0da8387966ee016ec79bd95b96548ad434ded3804d5931f9078f507d7f6c`.
The second baseline result and kill-proof SHA-256 values were
`1fa7d34159ff238ad8c3d419c0ac234aa4223d674c9138950fd9bac5b66e1638`
and `5224b2b13cf1635ad5d2f6fa8d36b278ae4e9a96e9608a9616508882bb4c17d6`.

## Source boundary and remaining correction

`hostDNSEngineBackend.RecoverZone` calls
`verifyManagedBINDRuntimeConfigExact` during its local BIND preflight in
`cmd/agent/dns_engine_host.go`, before
`completeManagedBINDV3PropagationForState` and the optional native peer proof.
The modified managed block fails `managedBINDZoneInclude` there. This ordinary
error bypasses `verifyEnrolledBINDPeerDeletion`'s reviewed
`DNSPeerPendingOwnerEditUnknown` wrapper. `RecoverDNSZoneV3` in
`cmd/agent/dns_engine_rpc.go` treats every non-pending error as ambiguity and
poisons the manager, leaving the accepted job running.

A correction must classify this *specific*, verified owner-config conflict for
an exact committed BIND V3 deletion as a reviewed pending owner-edit state and
durably return the same job from recovering to propagation-pending. Other
missing, malformed or contradictory receipts must retain their fail-closed
behavior. A fresh native cell must then prove the pending reason, preserved
owner bytes, no duplicate effects and normal same-identity recovery after
explicit owner reconciliation. This report does not claim Stage 2, P0.4 or
P0.5 completion; it changes no persisted schema or native service ownership.
