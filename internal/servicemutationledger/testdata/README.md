# Actual Alpha81 ledger producer fixtures

Generated from source archive `45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00` using
its original Agent test harness on a private Linux filesystem. Empty bytes came
from `initializeServiceMutationLedger`; running and failed bytes came from the
real manager `begin` / `finish` methods with a fixed clock. Published records use
synthetic job identities/payloads and the original per-kind phase formatter plus
`writeLocked` producer. No host operation was actually published by those fixtures.
They establish persisted producer/reader compatibility, not service health,
cryptographic authority, automatic recovery or native update acceptance.

Canonical files deliberately have no trailing newline. They are not hand-written
examples or output of the new shared encoder. Shared decoder/encoder round trips
and the current actual Agent producer must preserve their exact bytes.
