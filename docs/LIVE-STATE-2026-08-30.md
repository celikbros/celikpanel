# Live State — August 30, 2026

*[Türkçe](LIVE-STATE-2026-08-30.tr.md) · Dated handoff snapshot, not a monitoring feed*

This secret-free record updates the historical
[August 29 snapshot](LIVE-STATE-2026-08-29.md) with the bounded facts supplied
after the Alpha52 rollout. It does not infer facts that were not observed and
does not replace external operation receipts or registrar evidence.

## Evidence vocabulary

| Class | Meaning |
|---|---|
| VERIFIED | Direct product or bounded host evidence established the fact |
| VERIFIED PRE-ZONE | The fact was established before publishing an owner zone; it is not public-DNS proof |
| OPEN / REVERIFY | Required post-change or external evidence is absent |

## Alpha52 installation

The signed Alpha52 release and portal publication are documented in
[RELEASE-EVIDENCE-v0.1.0-alpha.52.md](RELEASE-EVIDENCE-v0.1.0-alpha.52.md).
Both hosts subsequently completed the normal signed panel update and returned
terminal success receipts. Receipt identifiers and the underlying sanitized
host evidence remain external handoff evidence and must be assigned a custodian.

| Host | Installed release | Update result | Status |
|---|---|---|---|
| Boston | `v0.1.0-alpha.52` | Server-authoritative terminal success receipt observed | VERIFIED |
| Frankfurt | `v0.1.0-alpha.52` | Server-authoritative terminal success receipt observed | VERIFIED |

This proves the bounded promotion result. It does not by itself prove every
schema, snapshot, unit, UI-asset or reboot field from the complete acceptance
matrix unless that field is present in the external receipt bundle.

The subsequent bounded host audit observed both panel and agent services active
at commit `adb25d8ec487dcb76dd95304a551d8cb37565115`, idle operation probes,
database schema `37`, release floor `52`, the expected transaction-marker
state, and matching served UI index/release/v6 checksums. One provenance caveat
remains: snapshot source identity was reported as unknown even though the
terminal receipts establish that both hosts entered the update from Alpha51.

## Authoritative DNS state before owner-zone publication

| Fact | Observed state | Status |
|---|---|---|
| Frankfurt engine and role | BIND primary | VERIFIED PRE-ZONE |
| Boston engine and role | PowerDNS secondary | VERIFIED PRE-ZONE |
| Mixed-engine pair | Pair identity and pre-zone catalog health matched | VERIFIED PRE-ZONE |
| Owner zone | `celikhost.com` is absent from both panel databases and DNS engines | OPEN / BLOCKER |
| Pre-zone catalog | Authoritative on both engines at serial `1`, with no member zones; source-bound catalog AXFR succeeds in both directions | VERIFIED PRE-ZONE |
| Owner-zone UDP/TCP and AXFR | Both engines return `REFUSED` to UDP/TCP SOA, NS and A queries and `NOTAUTH` to source-bound owner-zone AXFR | VERIFIED absent |
| Parent delegation | `.com` delegates to `ns1.celikhost.com` and `ns2.celikhost.com`, TTL `172800` | VERIFIED external observation |
| In-bailiwick registrar glue | `ns1.celikhost.com A 72.62.38.15` and `ns2.celikhost.com A 2.25.80.4`, TTL `172800` | VERIFIED external observation |
| Parent DS | No DS record is published; the `.com` denial is authenticated by NSEC3 | VERIFIED external observation |
| Public authoritative resolution | Recursive public resolution returns `SERVFAIL` because the delegated child zone is absent | OPEN / BLOCKER |

The pre-zone catalog result proves that the intended BIND-primary and
PowerDNS-secondary control-plane pairing was healthy before any owner zone was
published. It must not be promoted into a claim about public delegation,
post-zone serial equality, AXFR, authoritative AA/SOA answers or availability.

## Remaining acceptance boundary

Public DNS cutover remains blocked under [R-015](RISK-REGISTER.md#r-015--child-zone-and-public-authority-blocker).
Before that risk can close:

1. retain the registrar owner and backup assignment outside the repository;
2. publish the `celikhost.com` authoritative owner zone through the normal panel flow;
3. prove matching post-zone catalog membership and serials, source-bound AXFR,
   UDP/TCP SOA with AA, PairReady and public resolution from both engines; and
4. attach the bounded results to the external handoff evidence system.

Registrar delegation and exact IPv4 glue are already verified and are not
remaining cutover actions. Recheck them after owner-zone publication as
regression evidence; do not re-register them without independent evidence of a
registrar-side change.

Do not rewrite this snapshot when those steps complete. Create a new dated
record and link it from the risk register and handoff.

## Related evidence

- [Alpha52 release evidence](RELEASE-EVIDENCE-v0.1.0-alpha.52.md)
- [Update and DNS recovery incident](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md)
- [Risk register](RISK-REGISTER.md)
- [Historical August 29 live state](LIVE-STATE-2026-08-29.md)
