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
