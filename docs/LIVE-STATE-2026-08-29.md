# Live State — August 29, 2026

*[Türkçe](LIVE-STATE-2026-08-29.tr.md) · Handoff snapshot, not a monitoring feed*

This document separates verified observations from declared topology and
unknown live facts. It contains no credentials, tokens, private keys, customer
data or raw logs.

## Status vocabulary

| Status | Meaning |
|---|---|
| VERIFIED | Evidence was available to the handoff preparation on the stated date |
| DECLARED | A repository contract or runbook states the value; live state was not independently proven |
| UNKNOWN / REVERIFY | No sufficient current evidence is included; collect it read-only before relying on it |
| OUT-OF-REPO | Evidence or ownership record must remain in the approved external handoff system |

Source identity and live installed identity are different facts. Never copy a
source tag or commit into a server row unless the panel and agent independently
report and prove that exact identity.

## 1. Source-control baseline

| Fact | Value | Status |
|---|---|---|
| Canonical source tag and release | [v0.1.0-alpha.51](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51) | VERIFIED |
| Canonical source commit | 45d01ffb29013b9457180072c3b25ab24d5ff7bd | VERIFIED |
| Release sequence encoded by the source | 51 | VERIFIED in source |
| Current published binary release | v0.1.0-alpha.51 | VERIFIED |
| Alpha51 tagged-release CI | Passed for the published release | VERIFIED |
| GitHub release asset count | Exactly 6 immutable assets | VERIFIED |
| Official release authority | Ed25519 signed manifest v2 and detached signature verified | VERIFIED |
| Manifest-authorized archive | SHA256 `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`; 22,644,115 bytes | VERIFIED |
| Git tag signature | Unsigned | VERIFIED; the tag signature is not update authority |
| Handoff documentation on `main` | [PR #74](https://github.com/celikbros/celikpanel/pull/74), merge commit `e29df589594b2b5929d067a0174ab98d8182e4b5` | VERIFIED; documentation-only, not part of the Alpha51 binary baseline |
| GitHub open pull requests | 5 drafts: [#69](https://github.com/celikbros/celikpanel/pull/69), [#70](https://github.com/celikbros/celikpanel/pull/70), [#71](https://github.com/celikbros/celikpanel/pull/71), [#72](https://github.com/celikbros/celikpanel/pull/72), [#73](https://github.com/celikbros/celikpanel/pull/73) | VERIFIED as of 2026-08-29; none is part of Alpha51 |
| Download portal serves the exact signed Alpha51 asset set | Not proven here | UNKNOWN / REVERIFY |
| Handoff cleanup result | Duplicate worktrees/branches and listed debris removed; only the primary registered worktree remained | VERIFIED handoff observation |
| Incoming clean `main` checkout | Not yet verified by the incoming team | UNKNOWN / REVERIFY |

Draft PR #69 covers migration DDL canonicalization. Draft PR #70 covers restart
acknowledgement UX and requires a product decision. Draft PR #71 preserves CI
duplicate-release validation from `agent/ci-fast`. Draft PR #72 preserves
archival SSL/backup WIP from `agent/ssl-hostnames-hsts`; it is archival and must
not be merged as-is. Draft PR #73, head `archive/alpha35-portal-tooling` at
commit `0ef899f3cb96390c4ef3822f199eddc67bb0ee1f`, archives five unique Alpha35
portal scripts and an unpublished PR72-follow-up patch. It is archival and must
not be merged or executed as-is. No all-checks-green claim is made for any
draft. PR #74 merged the documentation-only handoff into `main`, so `main` is
ahead of the Alpha51 source tag while the published binary remains Alpha51
until a separate release is produced.

After unique work was preserved in PR #72 and PR #73, repository cleanup
removed 109 registered duplicate worktrees, 105 stale local branches, 56 stale
remote branches, `.attic`, `.worktrees`, `.claude/worktrees`, root
`__pycache__`, and the temporary handoff worktree. Only the primary registered
worktree remained. Tracked `.design-sync` was retained intentionally. This was
repository cleanup only: it changed no live server and proves no DNS or service
state. The incoming team must still verify a fresh, clean `main` checkout.

## 2. Environment classification

SECURITY.md declares the product pre-release and not production ready.
OPERATIONS.md uses production rollout language for Boston and Frankfurt, while
ROADMAP.md also describes them as test servers. Their operational class and
whether they contain customer data must be decided explicitly outside the
repository.

| Environment fact | Value | Status |
|---|---|---|
| Product lifecycle | Alpha / pre-release | DECLARED |
| Product production readiness | Not production ready | DECLARED |
| Boston class: test, staging or production | Not settled | UNKNOWN / REVERIFY |
| Frankfurt class: test, staging or production | Not settled | UNKNOWN / REVERIFY |
| Customer or personal data present on either node | Not established | UNKNOWN / REVERIFY |
| Current DNS and live-service state on both nodes | Not collected beyond the partial Boston observation below | UNKNOWN / REVERIFY |

## 3. Declared two-node topology

The following rows reproduce the frozen operational topology. They do not prove
that the live hosts currently match it.

| Node | Address | Declared rollout/DNS role | Declared operating system | Live proof |
|---|---|---|---|---|
| boston.celikhost.com | 2.25.80.4 | First rollout target; NS2; directional PowerDNS secondary | Ubuntu 24.04 | Partially verified below |
| frankfurt.celikhost.com | 72.62.38.15 | Second rollout target; NS1; direct BIND primary | Debian 13 | UNKNOWN / REVERIFY |

## 4. Boston

| Fact | Value | Status |
|---|---|---|
| Public panel recovery response | HTTP 200 | VERIFIED handoff observation |
| Reported live version | v0.1.0-alpha.51 | VERIFIED handoff observation |
| Panel full commit | Not captured | UNKNOWN / REVERIFY |
| Agent version and full commit | Not captured | UNKNOWN / REVERIFY |
| Panel/agent build match | Not captured | UNKNOWN / REVERIFY |
| Database schema version and contiguous migration proof | Not captured | UNKNOWN / REVERIFY |
| Operating system and package ecosystem | Ubuntu 24.04 is declared, not live-proven here | DECLARED / REVERIFY |
| celikpanel-panel unit | Not captured | UNKNOWN / REVERIFY |
| celikpanel-agent unit | Not captured | UNKNOWN / REVERIFY |
| Served UI asset belongs to Alpha51 | Not captured | UNKNOWN / REVERIFY |
| Panel TLS hostname, issuer and expiry | Not captured | UNKNOWN / REVERIFY |
| DNS engine | PowerDNS secondary is declared, not live-proven here | DECLARED / REVERIFY |
| Secondary local-write refusal | Not captured | UNKNOWN / REVERIFY |
| Catalog/member AXFR and UDP/TCP SOA proof | Not captured | UNKNOWN / REVERIFY |
| PairReady | Not captured | UNKNOWN / REVERIFY |
| TCP/53 and UDP/53 reachability | Not captured | UNKNOWN / REVERIFY |
| Firewall saved-policy and boot-restore state | Not captured | UNKNOWN / REVERIFY |
| Release sequence floor | Not captured | UNKNOWN / REVERIFY |
| Latest complete v6 update snapshot | Not captured | UNKNOWN / REVERIFY |
| Last successful rollback or restore drill | Not captured | UNKNOWN / REVERIFY |
| Panel-state disaster backup including secret.key and control-plane keys | Not proven | UNKNOWN / REVERIFY |

The verified HTTP 200 and version string do not prove any other row.

## 5. Frankfurt

| Fact | Value | Status |
|---|---|---|
| Public panel response | Not captured | UNKNOWN / REVERIFY |
| Current live version | Not captured | UNKNOWN / REVERIFY |
| Panel full commit | Not captured | UNKNOWN / REVERIFY |
| Agent version and full commit | Not captured | UNKNOWN / REVERIFY |
| Panel/agent build match | Not captured | UNKNOWN / REVERIFY |
| Database schema version and contiguous migration proof | Not captured | UNKNOWN / REVERIFY |
| Operating system and package ecosystem | Debian 13 is declared, not live-proven here | DECLARED / REVERIFY |
| celikpanel-panel unit | Not captured | UNKNOWN / REVERIFY |
| celikpanel-agent unit | Not captured | UNKNOWN / REVERIFY |
| Served UI asset identity | Not captured | UNKNOWN / REVERIFY |
| Panel TLS hostname, issuer and expiry | Not captured | UNKNOWN / REVERIFY |
| DNS engine | BIND primary is declared, not live-proven here | DECLARED / REVERIFY |
| Catalog serial and membership | Not captured | UNKNOWN / REVERIFY |
| Peer source-bound AXFR and UDP/TCP SOA proof | Not captured | UNKNOWN / REVERIFY |
| PairReady and local zone-write readiness | Not captured | UNKNOWN / REVERIFY |
| TCP/53 and UDP/53 reachability | Not captured | UNKNOWN / REVERIFY |
| Firewall saved-policy and boot-restore state | Not captured | UNKNOWN / REVERIFY |
| Release sequence floor | Not captured | UNKNOWN / REVERIFY |
| Latest complete v6 update snapshot | Not captured | UNKNOWN / REVERIFY |
| Last successful rollback or restore drill | Not captured | UNKNOWN / REVERIFY |
| Panel-state disaster backup including secret.key and control-plane keys | Not proven | UNKNOWN / REVERIFY |

Do not infer Frankfurt's installed version from the source baseline, Boston, a
DNS response, a browser cache or a previous release note.

## 6. Required read-only collection

Collect the following without changing panel settings, units, packages, DNS,
firewall, files or databases:

1. UTC timestamp, operator identity and approved evidence location.
2. Authenticated panel version response containing panel version/commit, agent
   version/commit and schema version.
3. Served UI asset identity for the same release.
4. Host OS and package ecosystem.
5. celikpanel-panel and celikpanel-agent active state plus bounded journal
   review for the evidence window.
6. Read-only service-operation and private mutation-ledger idle proof.
7. DNS engine, immutable pair identity, role, catalog serial/membership,
   source-bound AXFR, UDP/TCP SOA and PairReady proof.
8. External TCP/UDP port checks and TLS certificate metadata.
9. Saved firewall-policy and boot-restore ownership state.
10. Release sequence floor and exact installed updater identity without
    exposing key material.
11. Latest complete v6 snapshot identity and matching rollback helper.
12. Backup coverage, retention, last successful restore drill and whether
    secret.key, DKIM/WireGuard keys and panel certificates are recoverable.

Prefer authenticated read-only product views. Use SSH only when authorized and
only for read-only evidence the product cannot expose. Redact tokens, passwords,
private keys, email addresses, customer data and unnecessary IP/user data
before storing evidence.

## 7. Evidence record

Actual URLs containing private paths, account names, SSH identities, screenshots
with personal data and raw logs remain OUT-OF-REPO.

| Collected at UTC | Subject | Result | Evidence reference |
|---|---|---|---|
| 2026-08-29 | Canonical source tag and commit | Alpha51 / 45d01ffb29013b9457180072c3b25ab24d5ff7bd | Repository handoff baseline |
| 2026-08-29 | Alpha51 release chain | Release/tag identity, tagged-release CI, 6 assets and official Ed25519 manifest/signature verified; archive SHA256 `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`, 22,644,115 bytes; Git tag unsigned | [GitHub release](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51) |
| 2026-08-29 | GitHub open pull requests | 5 drafts: #69 migration DDL canonicalization; #70 restart acknowledgement UX/product decision; #71 CI duplicate-release validation (`agent/ci-fast`); #72 archival SSL/backup WIP (`agent/ssl-hostnames-hsts`); #73 five unique Alpha35 portal scripts plus unpublished PR72-follow-up patch (`archive/alpha35-portal-tooling`, `0ef899f3cb96390c4ef3822f199eddc67bb0ee1f`) | None is in Alpha51; do not infer all CI checks are green; #72/#73 are archival; do not merge either as-is or execute #73 as-is |
| 2026-08-29 | Repository cleanup | 109 registered duplicate worktrees, 105 stale local branches, 56 stale remote branches and listed debris removed; only primary registered worktree remained; tracked `.design-sync` retained intentionally | Repository-only observation; incoming clean `main` checkout remains REVERIFY |
| 2026-08-29 | Boston public panel recovery | HTTP 200; Alpha51 reported | OUT-OF-REPO evidence reference to be assigned |
| Not collected | Boston complete runtime inventory | UNKNOWN / REVERIFY | OUT-OF-REPO evidence reference to be assigned |
| Not collected | Frankfurt complete runtime inventory | UNKNOWN / REVERIFY | OUT-OF-REPO evidence reference to be assigned |

## 8. Acceptance rule

No server is considered current, paired, rollback-ready or production-ready
until every relevant UNKNOWN / REVERIFY row has dated evidence and an external
custodian. Updating this document must never include a secret value.
