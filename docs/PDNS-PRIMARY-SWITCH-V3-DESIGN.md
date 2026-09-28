# PowerDNS primary switch V3 evidence design (proposal)

Status: partial code and bounded disposable native fault/reboot evidence, 2026-09-28. P0.4 and P0.5 remain open. No installed-server change or complete product acceptance has been performed. The public paired-primary gate stays closed.

## Current narrow implementation boundary

The fresh SourceEngine-empty paired PowerDNS primary path now has a V3 intent/staged/native journal, separate source and observed native catalog serials, a Debian 13 amd64 PowerDNS 4.9.17 package/binary check, physical SQLite main/WAL/SHM observation, and explicit V3 state/ownership documents. The old V1/V2/V4 state and journal codecs remain separate. An owner command, `recover-dns-pdns-fresh-prestart --request-id`, uses the accepted request, release/host locks, worker exclusion, stopped cgroup and exact file/config proof to reverse only a target that has not started. A possibly started target keeps its journal and serving data. The command and code paths have package tests; one fresh paired-primary poststart fault trial and a management-disabled reboot have since run under the bounded scope below. The measured disposable PowerDNS primary to panel-free BIND secondary native result is retained under `deploy/e2e/dns-kill-matrix/evidence/pdns-master-bind-20260928`.

The pure V3 planner is now used by an Agent-mediated executor for the accepted request after a target may have started. Under the existing ledger, worker-exclusion and host-lock path, each checkpoint re-proves the Debian package and running executable, process start token, service topology, exact configuration, main/WAL/SHM SQL, local catalog and panel-free peer authority. It advances enable-intent to started, records the native observation, publishes the target state with a frozen-absence compare-and-swap and exact readback, then records verified and committed phases. FinalizeSwitch alone publishes ownership and archives the verified journal before the ledger becomes terminal. Changed or uncertain evidence retains the journal and does not stop DNS, write SQL or begin another mutation. Injected interruption/retry and drift package tests pass; the bounded native fault/reboot observation below does not establish the full acceptance matrix. The existing 45-second Agent recovery context can leave slow peer proof pending safely.

The owner-approved archival implementation retains the exact committed V3 journal before retiring its active pathname. It requires native receipt reproof, exact state and ownership, current target/peer authority, and absence of candidate/backup artifacts and their sidecars. Incomplete or changed evidence remains pending. The earlier approval rejection concerned discarding the committed evidence; that rejected edit was not applied. The owner subsequently approved keeping it as diagnostic history. Archiving alone does not establish the full product fault/reboot acceptance matrix or enable the public paired-primary gate. The later sections remain design targets; the narrowed fresh path preserves a started target for forward reconciliation rather than deleting its WAL/SHM in a poststart inverse.

## Completed journal archive — owner direction, 2026-09-28

The owner approved retaining completed recovery journals as diagnostic history,
rather than discarding them at finalization. The active record still has to leave
the pending-operation path after the exact operation has been verified. The
archive is historical evidence, not a backup, current-health proof, or authority
to repeat or reverse a mutation.

For the fresh V3 path, retain the canonical committed journal with its unchanged
wire schema and exact request identity in private storage. Publish, sync and
read back the archive before retiring the byte-identical active record. A retry
must accept only the same archive and must reprove the current native target.
Incomplete, failed, conflicting or owner-edited operations keep their active
evidence. An interrupted archive write must leave a retryable active record;
archival cannot stop DNS or alter its live SQLite database and sidecars.

This change addresses evidence lifecycle under D-025 invariants 1–4 and P0.4;
it does not establish native poststart fault/reboot acceptance or close P0.5.
For an interruption before active retirement, `Reconcile` re-verifies only an
already committed V3 request and returns it to finalization; it does not promote
any precommit phase. The dedicated Agent executor handles exact poststart
precommit phases under the accepted ledger and host lock. After active
retirement, recovery derives the archive name
from the accepted request and canonical V3 state digest, then re-verifies the
archived journal against current native SQL, ownership and local/peer authority.
An archive alone is insufficient. Publication uses an unnamed complete file,
file sync, no-replace link, directory sync and exact readback before active
retirement; filesystems without the required primitive refuse the operation.

Application compatibility now detects V3 journal, state and ownership evidence.
Until an explicit compatible Agent and independent recovery contract exists,
application replacement is refused without changing those files. The previous
separated-evidence marker cannot stand in for V3 support.

The first implementation does not prune historical archives. Per-record read
limits do not bound accumulated disk usage; a later bounded retention pass must
exclude active/unknown operations and records still needed by a nonterminal
ledger. Until that is implemented, do not claim automatic archive retention.

### Local validation of archival scope — 2026-09-28

Pinned Go 1.26.5 package tests passed for `internal/servicemutationledger`,
`internal/dnsenginerecovery`, `internal/dnsengineartifact`, `internal/pdnsnative`,
`internal/recoveryruntime`, `cmd/recovery` and `cmd/agent`. Go vet passed for the
changed ledger, recovery, application-compatibility and Agent packages.

Focused coverage includes interrupted partial archive write, publication before
directory sync, archive-before-active retirement, active-retired evidence,
conflicting/symlink/hardlink archives, owner-edited active bytes, artifact sidecars,
committed-only reconciliation without writes or inverse, precommit/foreign/native
failure refusal, and rejection of old application contracts against V3 evidence.
The Agent archive input test covers absent or malformed archives; it does not
substitute for a valid archive replay against a real native daemon and peer.
These are package tests with injected cuts, not product crash/reboot acceptance.

### Disposable V3 poststart trial — 2026-09-28

A separate [fresh V3 zone lifecycle trial](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md) exercised an exact poststart SIGKILL/recovery and four terminal child-zone operations (add, edit, delete, re-add) on a PowerDNS primary with a panel-free BIND secondary. Both native services retained authority after management-disabled reboots. Parentless deletion remains pending when a remote empty REFUSED cannot prove absence. The scoped local proof now requires the exact durable native V3 tombstone before and after locally bound UDP/TCP responses; peer proof remains independent. This changes no schema. D-025 invariants 2/4 and P0.4/P0.5 remain open for public admission, ordinary peer enrollment, other fault points and independent inverse.

The [retained native evidence](../deploy/e2e/dns-kill-matrix/evidence/pdns-v3-native-20260928/README.md) covers one fresh, paired `uninitialized` Debian PowerDNS primary with a panel-free Arch BIND secondary. The new Agent-mediated V3 forward path completed the exact interrupted request on its first clean recovery process; its active journal was archived and retired, state/ownership V3 receipts matched, and both native DNS servers stayed authoritative over UDP/TCP after a management-disabled primary reboot. The reader now admits the two exact observed catalog NS spellings while still requiring PowerDNS's SOA normalization and all other frozen record fields. No schema transition was introduced by that reader correction.

An earlier attempt reached a safe unknown result because the NS record stayed `invalid.`; later host reconciliation finished the same request without rewriting the terminal historical failure. The two SSH-launched cuts returned `255`; a third fresh run under systemd independently recorded `Result=signal` and `ExecMainStatus=9` (SIGKILL), then recovered the exact request on its first attempt. No shell exit `137` is claimed. This is a bounded P0.4/P0.5 advance, not an existing BIND-to-PowerDNS migration, owner-edit/independent-inverse test, full pair lifecycle, or public setup acceptance. The paired-primary gate remains closed.
### Existing managed BIND primary remains a separate contract

The fresh V3 trial starts with an uninitialized source. The V4 managed-BIND source adapter is standalone and explicitly rejects a pairing receipt; widening its topology check would not prove the loaded pair. Before any existing BIND primary is stopped for a PowerDNS-primary switch, a separate versioned source proof must bind the managed generation, exact ownership and configuration, pairing identity, loaded catalog/member state and the panel-free secondary's authority. The operation must reread that evidence immediately before stopping BIND. A matching file receipt alone does not prove what named is serving. The target then needs its own poststart PowerDNS native catalog/SQLite/process and peer proof, with explicit same-request recovery decisions. A future native adapter must attribute the BIND process and loaded zones, prove the pair and peer, and perform a fresh reread under the accepted mutation lock. Only then can same-request forward/inverse decisions and native fault trials be considered. No schema or production path for this separate existing-source migration is being added in the fresh V3 work. This separate existing-source migration remains gated until its own acceptance. The fresh paired-primary setup/RPC gate has separate open acceptance work; P0.4/P0.5 remain open.
## Observed failure and boundary

The retained [disposable native trial](../deploy/e2e/dns-kill-matrix/NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md) transferred a BIND primary catalog and member to a panel-free BIND secondary. Its BIND-to-PowerDNS primary switch then stopped with `PowerDNS producer contains noncanonical base records`; Agent rollback refused `PowerDNS rollback live database is not the staged target`. The source's catalog SOA was `invalid. invalid. 1 60 30 3600 30`. PowerDNS stored `invalid invalid 1790467209 60 30 3600 30` after starting. The exact staged candidate verifier is called on both the candidate and the live database (`cmd/agent/dns_engine_pdns.go`, `cmd/agent/dns_engine_pdns_switch.go`). The v1 journal records the source handoff serial, but no distinct native target serial or staged-file identity. Therefore neither accepting arbitrary new serials nor deleting the live database on a generic receipt match is safe. The failed native operation remains nonterminal evidence; it was not retried.

PowerDNS's [catalog-zone documentation](https://doc.powerdns.com/authoritative/catalog.html) describes daemon-generated producer content and serial updates. Its [SQL record conventions](https://doc.powerdns.com/authoritative/appendices/types.html) require domain-valued SOA/NS content without trailing dots. Native behavior, including any catalog metadata writes, must be captured and pinned on both supported host families before admitting a transformation.

## Versioned contract

V1 historical switch journals remain readable under their existing strict rules; no field is silently reinterpreted. V2 is already reserved for the BIND inverse plan. Reserve V3 only for a paired PowerDNS **primary** switch, with a canonical codec, explicit version gate and no V1/V2 target-evidence smuggling. An unsupported V3 version or missing proof is `unknown/recovery required`, never a successful switch or a generic legacy rollback.

At intent, freeze exact request/owner/manifest/source state, original PowerDNS database existence and hash, original config/unit snapshots and source catalog serial. After candidate SQL integrity and exact zone/manifest receipts pass, but **before** stopping BIND, publish a staged-target plan: canonical candidate path, descriptor-derived device/inode, size and SHA-256, owner/mode, SQL schema/content commitment and expected PowerDNS-native effect class. Verify journal readback and reject changed or substituted plan fields at every checkpoint. The staged candidate uses PowerDNS's SQL RDATA representation; staged verification still requires the exact frozen source catalog serial.

After candidate rename and PowerDNS start, prove the same target inode, supported daemon/package and DB identity, exact SQL zone/account/manifest rows, producer membership, allowed SOA/NS and catalog-metadata transformation, authoritative local UDP/TCP and catalog AXFR, and secondary convergence. Record the observed native catalog serial separately from the frozen **source** serial. The target state receipt and all future exact-request reads bind the native serial. An increased serial alone is never proof. If the daemon transformation cannot be fully characterized for a supported package/version, the operation stays unknown and must not remove an unproved database.

SQLite WAL, SHM and rollback-journal files are part of this plan. Before staging, require a checkpointed candidate and no sidecars. Once PowerDNS has run, identify main and sidecar generations under a stopped-daemon/cgroup proof and a pinned parent directory. A sidecar cannot be discarded merely because its pathname resembles the target. An unrecognized sidecar, foreign inode, changed table/row, owner edit or uncertain checkpoint leaves the journal intact for owner recovery.

## Recovery and destructive effects

Recovery first excludes the exact accepted worker and holds the release/host locks. It reads the secured V3 journal, ledger and original source identity, then classifies one of: intent with no target; staged candidate only; source stopped with the candidate still staged; candidate renamed but not started; native target started; verified/committed target; or unknown/foreign state. Every observation is repeated around any effect. The independent recovery binary must read V3 explicitly; old binaries must refuse it before mutation.

For rollback, prove PowerDNS is stopped and its cgroup empty. If the original database is already live and exactly matches its frozen before-image, leave it in place. If the candidate was activated, its **same** staged inode and complete staged or reviewed native SQL/sidecar contract must be proved before quarantining/removing that exact target. Only then restore the exact frozen backup, config, state and units. If the pre-switch database was absent, restore absence only after the same target proof. Recheck BIND's source identity, local authoritative UDP/TCP answers and catalog/peer state before a terminal rolled-back ledger verdict or journal retirement. Any failure leaves a nonterminal journal and an actionable owner-recovery reason; it cannot be relabeled success. A crash after a destructive effect must reconcile the same request from the next durable checkpoint without deleting a replacement file.

Required file effects to review separately are: candidate publication, live-to-backup rename, candidate-to-live rename, PowerDNS stop, exact target quarantine/unlink, target sidecar handling, backup-to-live rename, config/state restoration and BIND restart. Each needs a before/after-cut test and a no-foreign-path proof. The rejected draft did not meet this bar and is not present in source.

## Acceptance evidence

- Codec and checkpoint tests: historical V1 golden bytes, V2 BIND inverse compatibility, V3 canonical wire/phase progression, source/native serial separation, immutable staged identity, wrong request, mixed-version and unknown-version refusal.
- Pure SQL tests: exact staged catalog, characterized native SOA/NS/metadata rewrite, full zone and receipt equality, member add/delete, serial exhaustion, foreign row/account and same-inode owner edits. No comparator accepts an arbitrary raised serial.
- Linux filesystem tests: symlink/hardlink/path swap, inode replacement, same-byte replacement, WAL/SHM alteration, backup missing/changed, crash immediately before/after every rename/checkpoint, and a second fault during rollback. Demonstrate preservation of foreign files and prior source data.
- Fresh disposable Debian and Arch trials with a panel-free BIND secondary: exact catalog/member AXFR and authoritative UDP/TCP on each side before switch, after native target, after management-disabled reboot and after forced rollback. Capture daemon/DB inode, SQL rows, sidecars, systemd process/cgroup state, journal/ledger, build hashes and QMP shutdown. A separate owner-edit cut must stay nonterminal without deleting the edited target. Preserve the prior failed trial as regression evidence.
- Only after both successful and failure/recovery native cells, evaluate the selected P0.4/P0.5 subcase. Do not infer complete Stage 2, full native update resilience or installed-server readiness from component tests.

No installed CelikPanel update or live DNS change is part of this design. The owner remains the actor for updates in the panel UI and for any installed-server recovery.
