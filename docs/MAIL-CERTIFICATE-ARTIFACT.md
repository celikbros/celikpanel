# Shared mail-host certificate artifact v1

D-025 invariants 1/3, P0.4 producer/reader agreement and P0.5 renewal foundation.
This is shared parsing and verification, not independent renewal completion.

`internal/mailhostartifact` owns the existing v1 receipt name, canonical JSON,
request/qualifier/domain/leaf validation, deterministic Certbot lineage and pending
renewal bytes. It imports only the Go standard library. Agent's actual producer,
reader, renewal queue and mutation-payload qualifier use the shared contract.
Filesystem ownership, publication locks and supervised service changes remain
in their existing callers; parsing evidence never grants mutation authority.

The historical receipt includes exactly schema, request ID, qualifier, domain
and leaf SHA256 followed by one newline. Pending bytes contain lineage and leaf
SHA256 without a newline. Duplicate/unknown fields, alternate representations,
trailing data, wrong schema, wrong purpose or malformed identities are refused.
Historical receipt-domain parsing is deliberately preserved; enrollment still
requires the existing stricter canonical-FQDN check.

Retained-pair verification preserves the historical within-lifetime certificate
chain check needed for old evidence. Current-pair verification checks at the
supplied current time: expired/not-yet-valid material is not a publishable renewal.
Both require matching private key, approved name and explicit non-nil trust roots.
The actual Agent retained reader now uses the shared verifier, while its current
health checks still separately reject expired certificates. Neither API performs
an ACME request, filesystem publication or service reload.

No on-disk migration or new version is introduced. Golden receipt and pending
files were emitted by the real Alpha81 source producer at commit
45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00, using public fixture inputs documented
beside them. The new shared producer and actual Agent adapter reproduce those
bytes exactly. These are schema fixtures, not live certificate issuance proof.

Validation: shared-package malformed-evidence and trust/time tests, Agent golden
producer/reader test, existing MailHost/MailTLS/PanelCert tests under race detection,
and vet. Existing incomplete-publication/recovery behavior remains unchanged.

Remaining P0.5: a native deployment/reload consumer, owner-approved lineage/config
binding, shared update exclusion, durable retry and recovery, hook migration,
retained helper/update rollback and real renewal with both management binaries
absent. The existing deploy hook still invokes Agent until that complete path is
implemented and accepted. No installed owner server has been changed.


## Shared descriptor reader

`internal/mailhoststore` now owns the actual retained-generation file reader.
Agent delegates current selection, receipt, domain and key/certificate file reads
to it. The caller still supplies a trusted parent descriptor, publication exclusion
and the shared certificate verifier with its explicit trust roots. The package
has no Agent, database, licensing, process execution or service dependency.

The existing no-symlink/beneath resolution, root/single-link/mode/size rules and
missing-versus-corrupt distinction are retained. File descriptor and named entry
identity/metadata are checked again after reads. A selected version directory
must itself be root-owned and not writable by group/others; a changed directory
authority is refused without normalization. Replacing `current` during
verification is refused without undoing the owner's replacement. The reader
returns public leaf evidence and receipt, not private key bytes, from its current
selection API. Lower-level internal file reads remain confined to the caller's
trusted descriptor.

Root ext4 tests cover original producer receipt reads, absent current selection,
missing receipt, public key mode, wrong owner/group, hardlinks, symlinks, FIFO,
wrong domain/leaf and replacing the owner's current selection during verification.
Existing Agent tests verify real certificate/key trust through the shared reader.
No on-disk version, publication, hook or update migration changes. This is a native
consumer prerequisite; it does not complete independent renewal or prove a whole
mail service restart/renewal flow.

## Shared publication exclusion

`internal/certpublishlock` now owns the existing `/run/celikpanel-panel-cert.lock`
flock protocol used by both panel and mail certificate publication. Agent delegates
to it with its historical wait behavior. Future native consumers can supply a
bounded context. This is exclusion only: it grants no issuance, service-config,
operation-ledger or renewal authority, and does not replace the outer mutation lock.

The same pathname and flock mechanism interoperate with historical Agents. New
locks are private root-owned files. Existing root-owned single-link mode0600 files
keep their original group, including the Agent's celikpanel group. Existing wrong
permissions/ownership/types are refused without Fchmod or other normalization.
Nonblocking open avoids hanging on a substituted FIFO; cancellable lock acquisition
never runs the callback after an observed cancellation. The selected descriptor
and named entry are checked again after acquiring the lock; replacing the name
while a waiter holds the old descriptor cannot authorize publication through the
old inode. The fixed `/run` directory identity is also rechecked.

There is no persisted schema or migration. An altered lock requires the owner to
inspect the named resource and resolve the conflicting change before explicitly
retrying. Do not delete a lock held by another publisher: that creates two locks.
Unknown authority does not permit deleting the owner's file or changing its mode.
Root ext4 tests cover cancellation, action failure/reuse, replaced inode, unsafe
metadata/types with no normalization, historical private group and exclusion
against a separate process using plain historical flock. Killing that test holder
releases the kernel lock and a subsequent caller proceeds. Agent mail/panel TLS
race tests and the standalone recovery-checker build retain compatibility.

Native deployment/reload, durable renewal recovery and hook migration remain open.
This shared lock does not by itself establish independent certificate renewal.

## Shared native Certbot source reader

`internal/certbotsource` now owns the existing live/archive filesystem reader.
Both Agent panel and mail source paths use it. It accepts only the two established
managed lineage namespaces, confines resolution beneath the trusted source root,
checks directory/link/file authority and requires matching archive revisions.
Native private keys remain owner-readable and inaccessible to group/others;
missing openat2 support fails closed. Reads neither repair metadata nor invoke
Certbot. Actual domain/purpose approval, chain/key verification, current validity
and the existing 24-hour minimum source lifetime stay in the Agent caller.
Future native consumers must perform those checks too before publication.

The internal reader configuration retains the existing Agent test seams; production
uses `/etc/letsencrypt`, UID/GID zero and the native openat2 syscall. This is not an
RPC-selectable path or privilege override. The reader returns private bytes only
to trusted in-process consumers; they must never be logged or exposed as diagnostics.
No lineage, receipt or on-disk schema migration is introduced.

Validation: the existing actual-Agent trust-chain, wrong intermediate, server-auth,
time, missing secure syscall, unsafe link/metadata and revision/key mismatch tests
now exercise the shared reader. Additional root native-filesystem tests cover both
managed namespaces, invalid lineage/configuration, unchanged source metadata and
owner-modified key refusal without normalization. MailHost/MailTLS/PanelCert and
Certbot source-ownership tests pass under race detection; vet and the standalone
recovery Agent checker build pass. Native renewal deployment remains open.


## Native shared-contract acceptance

[AX native acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-AX.md) exercised
the actual shared artifact, descriptor reader, publication lock and Certbot
source reader through real Postfix/Dovecot convergence and renewal, then an
orderly reboot. Exact receipts and trusted SMTP/IMAP leafs agreed. Historical
success no longer clears a queued renewal when the owner selected another
certificate; the real-daemon owner-drift test preserves both facts. No schema
migration was introduced. This does not complete independent renewal: the test
still invokes Agent code and the production deploy hook still needs Agent.


## Shared accepted mail TLS plan

`internal/mailtlsartifact` now owns the existing v1 accepted Postfix/Dovecot TLS
plan (`mail-tls-sync-journal.json` and retained `mail-tls-committed.json`). Agent's
actual producer, current reader, recovery reader and equality checks delegate to
it. It validates the request identity, complete canonical payload and its existing
`mail-tls-sync/v1:sha256:` qualifier, including immutable SNI snapshot paths.
Unknown/duplicate fields, trailing bytes, altered host/root/SNI and alternate
empty representations remain rejected. Validation does not normalize caller intent.

This is an extraction of the historical byte contract, not a schema migration.
Actual Alpha81 producer output at `45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`
provides empty/SNI golden files; both Agent and shared encoder reproduce them.
Race tests cover those fixtures, changed meaning and current Agent mail host/TLS
paths; vet also runs. The prior AX native trial predates this extraction and is
not represented as native proof of this new build.

A syntactically valid plan is accepted intent, not proof of present daemon state,
owner authorization for a new helper, or completed publication. Reading it cannot
start a mutation. Agent still owns filesystem security, its operation ledger,
recovery execution and service reload. P0.4 sharing advances; P0.5 still needs the
independent deployment transaction, owner binding and real removal/reboot proof.


## Shared immutable publication

`internal/mailhoststore.StageMaterialAt` now prepares and publishes the actual
Agent's mail certificate generation, using the same v1 receipt, filenames,
root:root metadata, immutable version naming and fsync ordering as the historical
producer. The caller authenticates the parent path, holds the outer mutation and
publication locks, and verifies current certificate trust/lifetime. The primitive
duplicates the supplied descriptor and never grants new mutation authority or
claims native service convergence. No on-disk schema migration is introduced.

Material is checked before directory preparation and bounded by the reader's PEM
limit. The stage owns its input buffers. Before activation it rechecks generation
authority, exact contents and the original current-link identity; observed owner
changes stop this publication. Close preserves a selected/published generation,
and refuses destructive cleanup of changed staged material or unexpected files.
A successful rename followed by failed fsync remains a published/uncertain result
for the durable caller to reconcile. This is not a filesystem compare-and-swap
against a root administrator ignoring all locks: the final check-to-rename race
and abrupt-power-loss matrix are not certified here. At this extraction boundary, persisted recovery cleanup still remained in Agent.
The separately audited shared cleanup is documented below.

Root ext4 race tests cover actual producer-to-reader agreement, unpublished
cleanup, owner-selected preservation, changed key/mode/directory/extra file,
replaced current symlink, unsafe parent refusal without normalization and retained
publication uncertainty. Existing Agent mail/TLS regressions and vet pass.
[AY native acceptance](../deploy/e2e/release-recovery/MAIL-CONTRACT-AY.md) then
verifies this exact shared producer and accepted-plan reader with real renewal,
Postfix/Dovecot reload, trusted handshakes, orderly boot and owner-drift replay.
The existing Agent durable commit gate and service convergence remain in charge.


## Recovery cleanup shares the artifact contract

Agent's persisted recovery cleanup now delegates to
`mailhoststore.RemoveUnselectedExactAt`. It requires the exact receipt, full
retained-pair validation with the caller's trust roots, matching domain/leaf,
exactly the four historical files, an unselected generation, and stable directory
and file identities across verification. A receipt match alone cannot discard
owner-modified contents. Current selection, extra files, corrupted material or
an observed concurrent write leave the generation intact for owner review. The
existing operation identity and recovery caller still determine whether cleanup
is authorized; this API is not permission to remove arbitrary retained versions.

Schema v1 and recovery phases stay unchanged. The fsynced removal still names
only the four fixed files and the exact version directory; no recursive deletion
or metadata normalization is introduced. Root race tests include owner-key edits,
extra files, selected generations, wrong operation identity and a valid-key write
after cryptographic verification begins. Existing Agent MailHost/MailTLS race
tests and vet pass. AY predates this recovery cleanup change: it is not native
interrupted-recovery proof. That checkpoint, partial staging failures and a root
operator bypassing locks remain outside this bounded acceptance.


[Retained AY cleanup acceptance](../deploy/e2e/release-recovery/MAIL-CLEANUP-AY.md)
subsequently exercises the new cleanup through the actual Agent recovery helper,
real mutation lease, native certificate material and running SMTP/IMAP services.
An owner-note prevents removal; explicit owner resolution permits same-operation
cleanup without changing the selected certificate or unrelated pending renewal.
The abandoned operation is explicitly finished failed. This closes the bounded
controlled-stage native check, not crash/startup dispatch or power-loss cleanup.
