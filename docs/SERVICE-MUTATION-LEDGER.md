# Shared service mutation ledger contract

P0.3/P0.4: one historical v1 producer/reader contract, with no persisted migration.
`internal/servicemutationledger` now owns canonical decoding/encoding, the full
bidirectional active-pointer and job invariants, and all eight direct-publication
phase codecs. Actual Agent writers, initial-ledger creation, normal readers and
the separately built recovery checker delegate to it. The transport job shape
remains shared; old phase/status names and bytes are preserved.

The decoder rejects unknown versions/fields, noncanonical or trailing JSON,
inconsistent owner/request/worker identities, lifecycle times, active pointers,
and unsupported status. A direct mutation reporting success must carry its exact
kind, target, request and payload-bound publication receipt. DNS V3 propagation
pending retains its existing special status semantics. Expired/orphaned leases
do not become idle. Historical success remains execution evidence, not authority
to repeat a mutation or evidence of present native health.

The writer now applies the same schema and 1 MiB bound before staging files;
previously a writer could publish bytes that its bounded reader could not load.
Failure preserves the previous readable durable file. No automatic truncation,
repair, retry or owner metadata normalization is introduced. Callers retain their
existing durable failure and recovery behavior.

This package accepts bytes, not file paths. It does not establish file trust,
lock ownership, absence of other journals, process/package-manager idleness,
renewal enrollment, workload health or mutation authority. Those proofs remain
at their existing host boundaries. A future independent renewal helper must use
the same full evidence contract; importing a decoder alone is insufficient.

Compatibility fixtures came from the actual Alpha81 source producer at
`45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`: initializer, real manager begin/failed
finish, and original published-phase formatters plus writer using synthetic jobs.
Those publication fixtures certify bytes only, not actual native publication.
New tests round-trip them and run the current Agent writer against the exact
bytes, reject forged successes and cross-field conflicts, and preserve disk
contents when an oversized write is refused. Native independent renewal and the
complete interruption/automatic-restoration matrix remain open.

Validation at this source stage: shared package and full Agent `go test -race`
pass (Agent 197.597 s), including the actual separately compiled recovery checker;
`go vet` passes. Native retained-host readback is the next acceptance check.

[Subsequent AY native acceptance](../deploy/e2e/release-recovery/MAIL-LEDGER-AY.md)
passes on that exact source: standalone production checker, retained native
ledger, real lock contention/inheritance, unchanged state and trusted native
SMTP/IMAP leaf. It does not close independent renewal or crash recovery.

## Trusted Linux evidence reader

`servicemutationledger.ReadFile` is also used by the actual Agent and standalone
recovery checker for the ledger, renewal queue and existing mutation journals.
The caller supplies the already established numeric UID/GID; the reader does
not infer an identity from the file or recreate missing accounts/directories.
Only a missing final name in an existing trusted 0700 directory is absence.
Missing/untrusted parent paths are uncertainty, and unsafe evidence is retained.

All symlink components are refused with openat2 (no weaker fallback). The final
file must remain a bounded regular, single-link 0600 file with the accepted
owner. Reads are nonblocking for substituted FIFOs. Open-file and named-file
identity, permissions, ownership, links, size, mtime and ctime are checked after
reading; the parent path is reopened and compared to the pinned directory.
Observed replacement or change returns no accepted bytes. No repair or metadata
normalization happens. Other actors respecting the publication/host lease remain
serialized; this is not a guarantee against a root operator changing state after
the final check or return.

The historical JSON/receipt schema is unchanged. Supported state directories
already require private 0700 metadata. Invalid paths/symlinks and nonprivate
parents previously reachable by the low-level reader now require owner review.
The separate recoverable-initial-stage reader and its narrowly authorized
initializer repair remain unchanged. This does not implement native renewal
binding, a new mutation executor or automatic recovery from missing evidence.

The pre-ledger idle proof keeps the historical empty root:root mkdir residue
separate: a pinned, empty-directory proof accepts no alternate-owner file and is
repeated after other readiness probes. An owner-created record during that window
is preserved and rejected. The strict reader is never relaxed for that exception.
Shared root filesystem/race tests, the full Agent race suite (196.211 s), the
production standalone checker tests and vet pass on this source.
