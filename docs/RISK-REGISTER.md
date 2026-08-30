# Engineering and Operations Risk Register

*Baseline: August 30, 2026 · [Türkçe](RISK-REGISTER.tr.md)*

This register tracks known handoff gaps and the mitigations merged into `main`
through the [Alpha52 release](RELEASE-EVIDENCE-v0.1.0-alpha.52.md). It contains no secret values or real
custodian names. Each owner, target date, acceptance and external evidence
reference must be assigned in the approved out-of-repository handoff system.

PRs [#69](https://github.com/celikbros/celikpanel/pull/69),
[#70](https://github.com/celikbros/celikpanel/pull/70) and
[#71](https://github.com/celikbros/celikpanel/pull/71) are closed and
superseded. The only intentionally retained non-main remote heads are
`agent/ssl-hostnames-hsts` from archival PR
[#72](https://github.com/celikbros/celikpanel/pull/72) and
`archive/alpha35-portal-tooling` from archival PR
[#73](https://github.com/celikbros/celikpanel/pull/73). Neither may be merged
or executed as-is. There are no open pull requests at this baseline.

## Status and severity

- OPEN: mitigation is incomplete.
- REVERIFY: the condition may have changed, but current evidence is absent.
- BLOCKER: do not perform the affected operation until the exit criteria pass.
- CLOSED ON MAIN: the repository-only exit criteria are satisfied on `main`;
  this status is not live-server evidence.
- PARTIALLY MITIGATED / REVERIFY: a bounded component is fixed, but the
  remaining condition still requires acceptance evidence.
- PARTIALLY MITIGATED / OPEN: repository controls exist, but an accountable
  external assignment or acceptance condition remains open.
- Critical: can cause unrecoverable state, unsafe privileged operation or loss
  of release/rollback authority.
- High: can cause outage, security boundary failure or an unverifiable live
  deployment.
- Medium: materially increases error, drift or onboarding risk.

## Risk summary

| ID | Severity | Status | Risk |
|---|---|---|---|
| R-001 | Critical | CLOSED ON MAIN | Operations now documents snapshot v6, current v4/v5 rejection and the historical-release boundary |
| R-002 | High | CLOSED ON MAIN | README now separates optional local GPG use from canonical Ed25519 update authority |
| R-003 | Critical | OPEN / BLOCKER FOR REAL TENANTS | No proven full control-plane disaster backup and restore drill |
| R-004 | High | PARTIALLY MITIGATED / REVERIFY | Both hosts run exact Alpha52 with terminal receipts and full acceptance; snapshot source provenance remains `unknown` |
| R-005 | High | OPEN | Boston/Frankfurt environment classification conflicts with the not-production-ready policy |
| R-006 | High | OPEN | Route/role and API-contract debt remains at a security boundary |
| R-007 | High | OPEN | Required real-VM install/update/rollback/reboot evidence is not tracked in the handoff |
| R-008 | High | PARTIALLY MITIGATED / REVERIFY | Alpha52 receipts and the pre-zone mixed-engine catalog pair are verified; owner-zone and post-zone authority proof remain absent |
| R-009 | Medium | OPEN | External package/repository/CA endpoints can become stale without a live verification gate |
| R-010 | Medium | CLOSED ON MAIN | Architecture, onboarding and implementation-status documents are reconciled through Alpha52 |
| R-011 | High | OPEN | Access, signing-key, provider and incident custodians are not assigned in the handoff |
| R-012 | Medium | CLOSED/MITIGATED ON MAIN / INCOMING CLEAN CHECKOUT REVERIFY | Root scaffold, duplicate worktrees/branches and listed debris were removed; the incoming team must verify a clean `main` checkout |
| R-013 | Medium | OPEN | Browser golden-path, critical-endpoint and latency evidence remains incomplete |
| R-014 | Medium | PARTIALLY MITIGATED / OPEN | Incident template and first incident record exist; external response ownership is not assigned |
| R-015 | High | OPEN / BLOCKER FOR PUBLIC DNS CUTOVER | Parent delegation and glue are verified; the `celikhost.com` child zone and public authority are absent |
| R-016 | Medium | OPEN / PROVENANCE WARNING | Both valid v6 snapshots encode an `unknown` source identity although terminal receipts prove the prior Alpha51 commit |

## Detailed risks

### R-001 — Snapshot contract documentation mismatch

- Evidence: `main` updates docs/OPERATIONS.md and its Turkish pair to
  snapshot v6, states that the current updater/rollback path rejects v4/v5, and
  limits an older snapshot to its matching immutable historical recovery
  release and rollback helper.
- Impact: an incoming operator can select an incompatible rollback procedure or
  believe a non-restorable snapshot is accepted.
- Closure basis: the English/Turkish runbooks now match the source contract.
  Immutable Alpha52 scripts and contract tests remain binary authority.
- Status: CLOSED ON MAIN. This is documentation closure only; live or
  disposable restore evidence remains governed by R-003 and R-007.

### R-002 — Release-signing authority ambiguity

- Evidence: `main` makes README identify Ed25519 signed manifest v2,
  release sequence, pinned public key and exactly six assets as tagged-release
  update authority. Optional local GPG signing is explicitly non-authoritative.
- Impact: a team can publish integrity-only or optional artifacts while
  believing privileged update authority exists.
- Closure basis: README and release-signing guidance now make the authority
  boundary explicit; the Alpha52 official manifest/signature and six-asset set
  are verified.
- Status: CLOSED ON MAIN. Portal/live equality remains tracked by
  R-008 and is not implied by this documentation closure.

### R-003 — Control-plane disaster recovery is unproven

- Evidence: ROADMAP.md places panel-state disaster backup and restore drills in
  future work and states that losing secret.key makes sealed secrets
  irrecoverable. The handoff contains no successful clean-host restore proof.
- Impact: loss of a node can preserve domain files yet permanently lose
  credentials, DKIM/WireGuard material, panel identity or manageable state.
- Immediate control: do not onboard real tenants or claim disaster recovery.
  Preserve existing authorized backups without copying their contents into the
  repository.
- Exit criteria: a versioned, encrypted backup includes the SQLite state,
  secret.key, relevant control-plane keys and certificates; retention and
  off-host storage are defined; a clean-host restore drill proves service and
  cryptographic identity recovery; RPO/RTO are accepted externally.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-004 — Alpha52 live promotion proven; residual acceptance must be retained

- Evidence: the [Alpha52 release record](RELEASE-EVIDENCE-v0.1.0-alpha.52.md)
  and [dated live state](LIVE-STATE-2026-08-30.md) prove terminal receipts on
  both nodes, exact build `adb25d8ec487dcb76dd95304a551d8cb37565115`, active
  services, idle ledgers, contiguous schema 37, floor 52, byte-equal installed
  artifacts/served UI and verified v6 snapshot/rollback helpers.
- Impact: this rollout no longer presents unproven live-version parity, but the
  evidence must remain attributable and repeatable after later releases.
- Immediate control: preserve the dated record and exact receipts; do not infer
  future host state from a tag or portal. Keep
  [LIVE-STATE-2026-08-29.md](LIVE-STATE-2026-08-29.md) historical.
- Exit criteria: close only after R-016's snapshot provenance warning is fixed
  in a later reviewed release and that fix is proven by a new normal panel
  update without weakening the already-passed Alpha52 acceptance.
- Status: PARTIALLY MITIGATED / REVERIFY. Alpha52 live identity is proven; the
  residual provenance condition is tracked separately under R-016.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-005 — Environment and readiness ambiguity

- Evidence: SECURITY.md says not production ready; Operations uses production
  rollout language; Roadmap also calls Boston and Frankfurt test servers.
- Impact: customer data or availability expectations can be placed on an Alpha
  system without an explicit risk decision.
- Immediate control: treat both nodes as unclassified and the product as
  pre-release.
- Exit criteria: each node is classified as test, staging or production;
  customer-data status, change authority, monitoring and acceptance criteria
  are recorded externally; public wording matches the decision.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-006 — API and authorization contract debt

- Evidence: docs/AUTOPSY.md leaves OpenAPI/generated client work and the
  route-plus-role table/matrix incomplete. Current tests reduce risk but do not
  close the declared structural debt.
- Impact: a new route or frontend call can drift from tenant or role
  authorization expectations.
- Immediate control: require explicit backend authorization review and negative
  role tests for every changed endpoint.
- Exit criteria: one route/role registry, complete role-by-endpoint matrix
  tests, generated API contract/client and removal of duplicate handwritten
  authority.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-007 — Real-environment release evidence gap

- Evidence: Operations requires disposable Debian 13, Ubuntu 24.04 and current
  Arch Linux install/update/rollback/reboot evidence for boot-critical changes.
  The handoff contains no complete evidence set. deploy/e2e/rhel9 is explicitly
  only a blocked smoke probe, not successful-install certification.
- Impact: mocks and contract tests can pass while packaging, systemd, firewall
  or reboot behavior fails on a real host.
- Immediate control: changes in those areas remain deployment-blocked without
  the required VM evidence.
- Exit criteria: sanitized evidence for every required OS and state is linked
  externally to the exact commit and artifact digest.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-008 — Alpha52 live authority proven; post-zone DNS acceptance remains

- Evidence: the [Alpha52 release record](RELEASE-EVIDENCE-v0.1.0-alpha.52.md)
  and [dated live state](LIVE-STATE-2026-08-30.md) prove both exact installations
  and the pre-zone pair: Frankfurt BIND primary, Boston PowerDNS secondary,
  equal empty catalog serial `1` and source-bound AXFR in both directions.
  Parent delegation and glue are verified, but the owner zone is absent.
- Impact: the control-plane pair is healthy but cannot authoritatively serve
  `celikhost.com`; a claim of public DNS readiness would be false.
- Immediate control: preserve the verified mixed pair and create the child zone
  only through the normal panel flow. Do not bypass the failed public checks.
- Exit criteria: after child-zone publication, both engines prove equal catalog
  membership/serials, source-bound AXFR and UDP/TCP AA/SOA, and independent
  recursive resolvers stop returning `SERVFAIL`.
- Status: PARTIALLY MITIGATED / REVERIFY. Installation and pre-zone pairing pass;
  post-zone acceptance remains open under R-015.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-009 — External endpoint freshness

- Evidence: docs/AUTOPSY.md records a dead ACME endpoint reaching the product
  and leaves periodic live verification as an open rule.
- Impact: an apparently supported CA, repository or integration can fail only
  during a customer operation.
- Immediate control: manually verify affected official endpoints before a
  release that depends on them.
- Exit criteria: bounded scheduled checks cover catalog/registry external URLs,
  distinguish outage from permanent removal and create an actionable alert.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-010 — Documentation and onboarding drift

- Evidence: `main` aligns README with Alpha52 and the signed-update path,
  makes Roadmap metrics generated rather than hand-maintained, describes the
  role-aware web/src/nav.ts registry in both UI architecture files, and replaces
  the generic web/README.md template with product-specific onboarding.
- Impact: incoming engineers can select obsolete commands, misunderstand
  implemented behavior or rebuild scaffolding instead of the product.
- Closure basis: README, Roadmap status, architecture and web onboarding are
  reconciled through Alpha52; stale factual snapshots are dated or generated.
- Status: CLOSED ON MAIN. Future facts must still be updated or
  generated when their source changes.

### R-011 — Custodian and access continuity

- Evidence: repository policy correctly excludes secrets but does not assign
  custodians for GitHub, signing, release sequence, portal, VPS, registrar/DNS,
  backups or incident escalation.
- Impact: the new team can be unable to release or recover, or shared access can
  remain active without accountability.
- Immediate control: transfer only dedicated accounts and public keys through
  the approved external system. Do not add values here.
- Exit criteria: each category in HANDOFF.md has a named external custodian,
  backup custodian, grant/review/revoke dates and a tested access path.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-012 — Wrong-tree and repository clutter risk

- Evidence: `main` removes the unused root package.json/package-lock.json
  create-vite scaffold. After unique artifacts were preserved in PR #72 and PR
  #73, cleanup removed 109 registered duplicate worktrees, 105 stale local
  branches, 56 stale remote branches, `.attic`, `.worktrees`,
  `.claude/worktrees`, root `__pycache__`, and the temporary handoff worktree.
  Only the primary registered worktree remained. Tracked `.design-sync` was
  retained intentionally.
- Impact: changes can be made in the wrong tree or accidental copies can be
  committed and reviewed as product code.
- Immediate control: the incoming team must use a fresh `main` checkout and
  verify git status and worktree list before work begins.
- Exit criteria: the cleanup is closed/mitigated on `main`; incoming-team
  evidence shows a clean `main` checkout with only intentional registered
  worktrees. Tracked `.design-sync` is not debris and remains intentionally.
- Status: CLOSED/MITIGATED ON MAIN / INCOMING CLEAN CHECKOUT REVERIFY. This cleanup
  changed no live server and supplies no live-runtime evidence.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-013 — Golden-path and latency evidence

- Evidence: docs/AUTOPSY.md leaves browser render, critical endpoint smoke and
  the under-100-ms measurement incomplete.
- Impact: compilation and unit contracts can pass while the customer journey or
  performance objective regresses.
- Immediate control: require targeted UI contract tests and manual acceptance
  for affected journeys.
- Exit criteria: CI boots the real panel, exercises critical authenticated
  journeys, records bounded browser evidence and measures the stated latency
  objective.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-014 — Incident response ownership

- Evidence: SECURITY.md defines private reporting. The secret-free
  [incident template](INCIDENT-TEMPLATE.md) and the
  [August 26 incident record](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.md) now
  provide evidence vocabulary, timeline, recovery, corrective-action and
  closure structure. No on-call, severity owner, incident commander,
  escalation timeline or postmortem action owner is assigned externally.
- Impact: a DNS, release or security incident can stall or be handled through
  unsafe ad-hoc changes.
- Immediate control: preserve the panel-mutation and read-only SSH boundaries;
  use the external escalation channel once assigned.
- Exit criteria: externally assigned severity model, contacts, commander,
  communications path and postmortem/action tracking; one drill or real incident
  demonstrates acknowledgement, handoff and action-owner closure using the
  repository template.
- Status: PARTIALLY MITIGATED / OPEN. The repository template is complete, but
  it cannot substitute for accountable external owner assignment.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-015 — Child-zone and public-authority blocker

- Evidence: parent delegation and exact glue are verified externally:
  `ns1.celikhost.com → 72.62.38.15` and `ns2.celikhost.com → 2.25.80.4`, TTL
  `172800`; DS is absent. The [dated live state](LIVE-STATE-2026-08-30.md) records
  that the child zone has not been created: direct UDP/TCP queries are
  `REFUSED`, AXFR is `NOTAUTH`, and public recursive resolution is `SERVFAIL`.
- Impact: resolvers reach the delegated servers but receive no authoritative
  `celikhost.com` zone, so public resolution remains unavailable.
- Immediate control: do not cut public DNS traffic over to this pair or claim
  public authoritative availability. Do not place registrar credentials in the
  repository.
- Exit criteria: the child zone is published through the normal panel flow;
  both engines prove matching post-zone catalog membership/serials,
  source-bound AXFR and AA/SOA over UDP/TCP; independent resolvers prove public
  answers through the already-verified delegation and glue.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-016 — Snapshot source identity is `unknown`

- Evidence: both Alpha52 terminal receipts identify the expected prior Alpha51
  commit `45d01ffb29013b9457180072c3b25ab24d5ff7bd`, while both verified v6 snapshot
  directory identities use `from-unknown-to-adb25d8ec487-*`. Snapshot checksums,
  target identity and rollback helpers all passed.
- Impact: rollback material is intact, but the snapshot path alone cannot prove
  the source build, weakening forensic provenance and automated handoff checks.
- Immediate control: retain the terminal receipt beside snapshot evidence and
  do not rename, rewrite or reconstruct a live snapshot.
- Exit criteria: a reviewed release records the verified installed source
  version/commit in the next v6 snapshot identity and a normal panel update
  proves the corrected provenance without regression.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

## Acceptance rule

Risk acceptance is an accountable business decision and belongs in the
external register. An OPEN/BLOCKER risk cannot be silently converted to
accepted by changing wording in this file. Close a risk only with its exit
criteria and dated evidence.
