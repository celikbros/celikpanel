# Separate mail renewal executable

P0.5 owner independence; P0.3/P0.4 exact durable operation and owner authority.
The `celikpanel_mail_renewal` Linux build of the shared Agent package has a
compile-time separate entry. It exposes only `--queue <managed-mail-lineage>`,
`--process-pending` and root-only `--inspect-build-identity`. Ordinary Agent CLI,
RPC registration, sockets, Panel/licensing startup and background workers are
unreachable from this entry. This is not a renamed ordinary Agent invocation.

The executable shares the actual scoped renewal manager, canonical ledger,
strict readers, host/publication exclusion, tracked command supervisor,
immutable certificate publisher, reload-only convergence and acknowledgement.
There is no second implementation or schema migration. A retained active/unknown
operation is preserved; this entry does not call general startup recovery.
Initial issuance, certificate authority issuance, installation and native mail
configuration are outside its authority. `--queue` only accepts the already
selected managed host lineage and its validated Certbot source.

Both entry and supervisor reject development, path/fault-injection and loader
environment overrides. Native lookup uses a fixed system PATH. The supervisor
still proves its inherited host-lock descriptor and additionally restricts the
executable directories, exact read-only mail probes and reloads of the two fixed
mail units. It cannot start stopped services, write postconf, compile SNI maps,
run a shell or invoke updater modes. General Agent and fault-test init hooks do
not perform their side effects in this build. Ordinary Agent builds retain their
existing startup and supervisor behavior.

Build for local acceptance with:

```sh
go build -tags celikpanel_mail_renewal -o mail-renewal ./cmd/agent
```

This change does not install that binary or alter existing Certbot hooks, units,
update artifacts or owner services. Existing accepted native configuration,
private mutation state and its group identity, common runtime locks and native
Certbot source are still required. Managed removal must retain those prerequisites
or perform a reviewed compatible ownership migration. Packaging, owner enrollment,
independent boot-time runtime initialization, hook replacement, old-Agent rollback
compatibility, bounded retry/recovery and the complete absence matrix remain open.
No claim of automatic recovery of every interrupted renewal is made.

Full ordinary Agent race tests passed in 195.267 s; ordinary vet and helper-tag
entry tests/build passed. Native VM acceptance is recorded separately after
execution. Unit tests reject unrelated CLI modes, environment redirection,
wrong locks, configuration writers, arbitrary programs and broader systemd calls.
An attempted local-host executable smoke was refused by automatic approval
review because it included process-pending; no local renewal was run. Executable
negative/positive acceptance is confined to guarded disposable QEMU guests.


[BD native acceptance](../deploy/e2e/release-recovery/MAIL-EXECUTOR-BD.md) now
verifies separate-executable renewal, exact completion/queue replay, protected
entry refusals and native mail after reboot. It also records the unresolved
missing-runtime refusal after reboot; that is preserved evidence, not successful
boot renewal. Native enrollment and ownership/runtime migration remain open.
