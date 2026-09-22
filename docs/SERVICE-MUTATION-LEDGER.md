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
