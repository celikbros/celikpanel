# Alpha52 Release Evidence

*Published: August 30, 2026 · [Türkçe](RELEASE-EVIDENCE-v0.1.0-alpha.52.tr.md)*

This record identifies the reviewed source and immutable public artifacts for
`v0.1.0-alpha.52`. Release publication alone is not installation proof; the
subsequent per-node evidence is recorded separately in
[LIVE-STATE-2026-08-30.md](LIVE-STATE-2026-08-30.md).

No credential, token, private key, customer data, private evidence path or raw
production log belongs in this document.

## 1. Evidence status

| Status | Meaning |
|---|---|
| VERIFIED | The stated repository, GitHub or portal evidence was checked |
| DECLARED | A tracked contract states the requirement; this record does not independently prove it |
| OPEN / REVERIFY | The required live or external evidence is not yet available |
| OUT-OF-REPO | The sanitized evidence reference and accountable owner remain in the approved external register |

## 2. Source and review identity

| Fact | Value | Status |
|---|---|---|
| Release | [v0.1.0-alpha.52](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.52) | VERIFIED |
| Release sequence | `52` | VERIFIED |
| Release commit | `adb25d8ec487dcb76dd95304a551d8cb37565115` | VERIFIED |
| Tag target | The annotated tag resolves to the exact release commit above | VERIFIED |
| Tag signature | The tag object is unsigned | VERIFIED; the tag is not privileged update authority |
| Tagged-release workflow | [run 33283088681](https://github.com/celikbros/celikpanel/actions/runs/33283088681), completed successfully | VERIFIED |
| Workflow head | tag `v0.1.0-alpha.52`, commit `adb25d8ec487dcb76dd95304a551d8cb37565115` | VERIFIED |
| Publication time | `2026-08-30T00:30:40Z` | VERIFIED GitHub release metadata |

The privileged update authority is the Ed25519-signed manifest v2, its
detached signature, the pinned public key and the monotonic sequence. The Git
tag, HTTPS, `latest.txt` and checksum files are not substitutes for that
authority.

## 3. Signed manifest identity

The published manifest contains these exact fields:

```text
format=celikpanel-release-manifest-v2
sequence=52
version=v0.1.0-alpha.52
commit=adb25d8ec487dcb76dd95304a551d8cb37565115
published_at=2026-08-30T00:19:13Z
os=linux
arch=amd64
archive=celikpanel-v0.1.0-alpha.52-linux-amd64.tar.gz
archive_sha256=9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304
archive_size=22672364
```

| Manifest fact | Result | Status |
|---|---|---|
| Format | `celikpanel-release-manifest-v2` | VERIFIED |
| Target | `linux/amd64` | VERIFIED |
| Authorized archive SHA-256 | `9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304` | VERIFIED |
| Authorized archive size | `22,672,364` bytes | VERIFIED |
| Detached signature size | 64 bytes | VERIFIED GitHub asset metadata |
| Signing authority | Exact tracked Ed25519 public key required by tagged-release CI | VERIFIED by successful fail-closed workflow; private key remains outside the repository |

## 4. Immutable GitHub asset inventory

The completed release contains exactly six public assets:

| Asset | Size | GitHub-reported digest |
|---|---:|---|
| `celikpanel-v0.1.0-alpha.52.tar.gz` | 22,672,364 | `sha256:9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304` |
| `celikpanel-v0.1.0-alpha.52.tar.gz.sha256` | 100 | `sha256:5b97485b851165647327b9b5a39247f6c3f40ed912c12fa00b563cded747b09d` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.tar.gz` | 22,672,364 | `sha256:9a604bf0f58855f53997a1adeb44a24cc76c4ff062fd8068ee6a66be66a28304` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.tar.gz.sha256` | 112 | `sha256:cd41d02c6cbee742678b93e55fe245a5d4945aa5c5a9cad9f1e7607bfc746a0a` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.release-manifest-v2` | 332 | `sha256:e6597d9b598f0ab17ab7341b1dcc3591cfd6c6c19e3e13d348201d88ac2c5cdf` |
| `celikpanel-v0.1.0-alpha.52-linux-amd64.release-manifest-v2.sig` | 64 | `sha256:7aeda55121828566931dcd8cf00a3c69dcd38626a044c515d0578d721576aaae` |

The generic and platform archives have the same size and SHA-256. The release
workflow also enforces the archive bootstrap, internal checksum, build identity
and reproducibility contracts documented in [release-signing.md](release-signing.md).

## 5. Download portal publication

The Alpha52 candidate was published through the tracked production publisher
and the resulting portal state was verified against the signed release.

| Gate | Result | Status |
|---|---|---|
| Tracked publisher used | `deploy/publish-download-portal.ps1` | VERIFIED |
| Portal target | Alpha52 / sequence 52 / exact signed linux-amd64 archive | VERIFIED |
| Candidate and signed-release identity | Version, commit, sequence, digest and size agree | VERIFIED |
| Public portal verification | Completed after the atomic exchange | VERIFIED |
| Previous release preservation | Required by the publisher contract | DECLARED; storage reference remains OUT-OF-REPO |

Portal verification does not prove either server installed Alpha52. The
installed updater, release floor and panel/agent identities must be observed
separately on each node.

## 6. Live rollout acceptance

The required Boston-first, Frankfurt-second panel rollout completed. The dated
live-state record supplies the authenticated product and bounded read-only host
evidence; this section summarizes it without replacing that evidence.

For each node, record all of the following after the user completes the signed
panel update:

1. update operation ID, start/end UTC, elapsed time and verified terminal result;
2. panel and agent version plus full commit, schema version and served UI asset;
3. active panel/agent units and bounded clean journals for the update window;
4. idle service, DNS and self-update operation state;
5. installed updater identity, release-sequence floor `52`, complete v6 snapshot
   identity and matching rollback helper;
6. current DNS engine, role and immutable pair identity;
7. catalog serial/membership, source-bound AXFR, UDP/TCP SOA and PairReady proof;
8. external HTTPS, TCP/53, UDP/53, delegation and glue checks; and
9. firewall, reboot-required and TLS observations without silently changing them.

| Node | Expected transition | Current evidence in this record | Status |
|---|---|---|---|
| Boston | Alpha51 → Alpha52, then host/release acceptance | Receipt `b6fd0052b2c4a04b117a753637d68798`; exact build `adb25d8ec487dcb76dd95304a551d8cb37565115`; floor 52; host/release acceptance passed | VERIFIED |
| Frankfurt | Alpha51 → Alpha52 only after Boston passes, then host/release acceptance | Receipt `b85dee68b54a01689333112ae8ccaa5f`; exact build `adb25d8ec487dcb76dd95304a551d8cb37565115`; floor 52; host/release acceptance passed | VERIFIED |
| Cross-node DNS | BIND primary and PowerDNS secondary remain an exact serving pair | Pre-zone catalog serial `1`, empty membership and source-bound AXFR in both directions pass | VERIFIED PRE-ZONE |

Both nodes additionally passed active-service, idle-ledger, contiguous schema
37, installed binary/UI equality, served-index equality and v6
snapshot/rollback checks. Both v6 snapshot source identities are `unknown`
despite receipts proving the prior Alpha51 commit; R-016 tracks this provenance
warning. The historical `LIVE-STATE-2026-08-29` record remains unchanged.

Parent delegation and glue are verified as `ns1.celikhost.com → 72.62.38.15`
and `ns2.celikhost.com → 2.25.80.4`, TTL `172800`; DS is absent. The child zone
has not been created. Direct UDP/TCP queries return `REFUSED`, AXFR returns
`NOTAUTH`, and public recursive resolution returns `SERVFAIL`.

## 7. Remaining release gates and risks

- Real disposable Debian 13, Ubuntu 24.04 and current Arch Linux
  install/update/rollback/reboot evidence remains governed by R-007. A successful
  two-node panel update does not by itself close that risk.
- Control-plane disaster recovery remains governed by R-003.
- Environment classification and customer-data status remain governed by R-005.
- Alpha52 installed identity is proven; R-004 remains partially mitigated until
  the R-016 snapshot provenance warning is corrected and reverified.
- Incident ownership and external escalation remain governed by R-014 even
  after a repository incident template is added.
- Public DNS cutover remains blocked under R-015 until the child zone and full
  post-zone authority matrix pass; delegation and glue already pass.

See [RISK-REGISTER.md](RISK-REGISTER.md) and
[INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md).

## 8. Acceptance rule

This release record proves the reviewed Alpha52 source, signed GitHub release
and verified portal publication. The linked dated record proves the bounded
two-node host/release acceptance and pre-zone DNS-pair checks stated above. It
does **not** prove owner-zone DNS acceptance, public child-zone authority,
disaster recovery or production readiness.
