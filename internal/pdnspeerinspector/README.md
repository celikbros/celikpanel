# PowerDNS native peer deletion inspector (source-only)

This package implements a read-only, fail-closed observation for a PowerDNS
CONSUMER secondary. It does not enroll an SSH key, install a command, change
PowerDNS, call an Agent, or mark a DNS V3 operation complete. The pending 27d
request remains pending. The affected D-025/P0.4 invariant is that an owner-run
native secondary must keep serving independently, while a parentless deleted
zone cannot be called terminal from a `REFUSED` DNS reply alone.

The new `pdnspeerproof` v1 wire schemas are separate from BIND's v1 schemas.
They bind the exact V3 request/owner IDs, generation and qualifier, primary
and peer addresses, pinned peer SSH identity digest, catalog serial and member
digest, deleted name, random nonce, attempt, and expiry. `Verify` requires an
authenticated peer and a durable consume-once callback. No existing BIND
challenge, enrollment, journal or ledger schema is reinterpreted. A later
Agent integration needs an explicit new owner enrollment artifact/version,
fixed forced command, current-operation and lock rechecks, source-bound AXFR
and negative-zone probes, and crash-safe journal handling. Until those are
reviewed, deletion must remain pending.

`OwnerPolicyReader` reads only `/etc/pdns-peer-inspector/policy.json`, under
root-owned directories. The exact canonical JSON pins primary IP, peer IP,
catalog, and catalog account; symlinks, hardlinks, permissive modes and edits
are rejected. The native reader supports only the narrowly reviewed Arch
fixture configuration, a single `gsqlite3` backend and fixed database path.
It ties an active `pdns.service` PID/start time and executable to UDP/TCP
listeners, the opened SQLite inode, and an owned local control socket. It
reads the exact CONSUMER and catalog records from SQLite. A fixed,
read-only `LIST-ZONES` request uses a direct Unix stream. Before sending it,
`SO_PEERCRED` on that same connected descriptor must match the verified
`pdns.service` PID; the executable, start ticks and daemon-owned listener
inode are checked before and after the exchange. The catalog must be listed
by the running daemon. A deleted zone must be absent in both SQLite and the
daemon list. Two complete reads under stable process/config/database and
owner policy are required. Unknown or contradictory observations cannot
verify a deletion.

A read-only probe of the preserved 27d Arch guest measured PowerDNS 5.1.4.
`pdns_control --socket-dir=/run/pdns list-zones` returned one canonical
catalog name followed by `All zonecount: 1`. The control socket was
`/run/pdns/pdns.controlsocket`, inode 7914, owned by `pdns.service` PID 414
through fd 4. That PID had `/usr/bin/pdns_server` with the reviewed vendor
arguments, UDP/TCP listeners on 192.0.2.11 and 127.0.0.1, and open SQLite
and WAL descriptors. The catalog CONSUMER row had serial 2 with no member.
Evidence is sealed in `/var/tmp/cp-stage2-pdns-peer27d/evidence/inspector-compatibility.json`
(SHA-256 `bb8d94d8de47ad6ded4d6fa861c40cc9ab72d1bc7daa0ee8abf4af78cc05fb84`).
The source parser now accepts only this measured zone-count framing. Inspector
SQL queries pin their own open SQLite connection to the same device/inode as
the daemon's open database descriptors, before and after the read; an owner
rename/restore to another inode fails closed. Configuration is read through
a no-follow descriptor under root-controlled ancestors and rechecked against
the final path; a swap or in-place owner edit fails closed. The 27d guests were QMP-stopped
again after observation, with evidence and the pending delete ledger preserved.

The earlier external `pdns_control` pathname-swap gap is closed in this
source package with same-descriptor `SO_PEERCRED`. PowerDNS 5.1.4 upstream
`DynMessenger::send` writes a newline-terminated command and reads until
EOF; `DynListener` accepts that line and returns the handler output on the
same connection. `DLListZones` calls `getAllDomains` and returns canonical
names plus `All zonecount: N`. The inspector sends only the fixed
`LIST-ZONES\n` command. A foreign PID receives no command; a swapped
listener inode fails closed. The disposable 27d Arch guest independently
confirmed the direct protocol: service PID 418, control socket inode 7850
on daemon fd 4, same-connection peer credentials PID/UID/GID
418/969/969, and only the catalog in the response. Evidence is
`/var/tmp/cp-stage2-pdns-peer27d/evidence/inspector-control-same-fd.json`
(SHA-256 `cb2c5a308a3ee3aa60858bfbacf9c5194351211e5204884ae20bfd25814ea1d9`).
Both 27d guests were gracefully QMP-stopped again. This is a native
compatibility probe of the protocol, not an end-to-end installed wrapper or
accepted V3 deletion proof.

A later, bounded 27d trial built the actual Linux `cmd/pdns-peer-inspect`
CLI from current source and staged a temporary root-owned policy only in the
disposable Arch guest. The first invocation failed closed: while its direct
control connection was open, Linux listed both the listening and accepted
Unix sockets under the same pathname. The listener parser treated those two
rows as ambiguous. A diagnostic Go overlay captured the exact error without
changing the shipped generic CLI error. The parser now selects only the
`Flags=00010000`, `Type=0001`, `St=01` listener, while rejecting duplicate or
unreviewed listener rows. A regression uses the actual native row shapes.

The corrected CLI returned `catalog_state=transferred`,
`member_state=absent`, and `native_state=unloaded` for catalog serial 2 and
empty-member digest
`fa808a7260202ff69dc6a832a174df900cc2ba85aa2b8653b2f8f178757ec685`.
Its response digest matched the exact canonical request. Changing only the
temporary owner policy's catalog account produced exit 1 with no response;
the original policy was restored, then all temporary guest policy and
binaries were hash-checked and removed. Both 27d guests were gracefully
QMP-stopped. Initial failed output, successful response, negative control,
source and binary hashes are sealed in
`/var/tmp/cp-stage2-pdns-peer27d/evidence/inspector-cli-manifest.json`
(SHA-256 `3dac587e4a101c1413de60e68f64574a686e8d85eb6cfcdf79b6fcec8fd411bb`).
The probe supplied deletion generation 3 but did not independently attest
that generation from the Agent journal. The package still has no installed
owner policy, reviewed one-command root wrapper, owner-enrolled SSH channel,
or Agent integration. Thus this native CLI result is a component observation,
not an authenticated consumed proof or terminal production V3 acceptance.
The pending 27d ledger was not retried. Other PowerDNS versions and control
output formats remain pending compatibility review; unknown framing is not
treated as absence.

References: [PowerDNS 5.1.4 DynMessenger](https://github.com/PowerDNS/pdns/blob/a063013d3dd7bdaeaef9bc1ceae1f1cb8d91ee90/pdns/dynmessenger.cc),
[DynListener](https://github.com/PowerDNS/pdns/blob/a063013d3dd7bdaeaef9bc1ceae1f1cb8d91ee90/pdns/dynlistener.cc),
[DLListZones](https://github.com/PowerDNS/pdns/blob/a063013d3dd7bdaeaef9bc1ceae1f1cb8d91ee90/pdns/dynhandler.cc),
[PowerDNS control command](https://doc.powerdns.com/authoritative/manpages/pdns_control.1.html),
[PowerDNS socket-dir setting](https://doc.powerdns.com/authoritative/settings.html#socket-dir).
