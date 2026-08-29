# Engineering and Operations Risk Register

*Baseline: August 29, 2026 · [Türkçe](RISK-REGISTER.tr.md)*

This register tracks known handoff gaps and the mitigations completed on
`docs/alpha51-engineering-handoff`. It contains no secret values or real
custodian names. Each owner, target date, acceptance and external evidence
reference must be assigned in the approved out-of-repository handoff system.

Five draft PRs are outside the Alpha51 baseline: [#69](https://github.com/celikbros/celikpanel/pull/69)
(migration DDL canonicalization), [#70](https://github.com/celikbros/celikpanel/pull/70)
(restart acknowledgement UX/product decision), [#71](https://github.com/celikbros/celikpanel/pull/71)
(CI duplicate-release validation from `agent/ci-fast`),
[#72](https://github.com/celikbros/celikpanel/pull/72) (archival SSL/backup WIP
from `agent/ssl-hostnames-hsts`) and [#73](https://github.com/celikbros/celikpanel/pull/73)
(five unique Alpha35 portal scripts plus an unpublished PR72-follow-up patch;
head `archive/alpha35-portal-tooling`, commit
`0ef899f3cb96390c4ef3822f199eddc67bb0ee1f`). PR #72 and PR #73 are archival.
Neither may be merged as-is, and PR #73 must not be executed as-is. No
all-checks-green claim is made for any draft.

## Status and severity

- OPEN: mitigation is incomplete.
- REVERIFY: the condition may have changed, but current evidence is absent.
- BLOCKER: do not perform the affected operation until the exit criteria pass.
- CLOSED ON HANDOFF BRANCH: the repository-only exit criteria are satisfied by
  this branch; verify review and merge before treating the closure as present
  on `main`.
- PARTIALLY MITIGATED / REVERIFY: a bounded component is fixed, but the
  remaining condition still requires acceptance evidence.
- Critical: can cause unrecoverable state, unsafe privileged operation or loss
  of release/rollback authority.
- High: can cause outage, security boundary failure or an unverifiable live
  deployment.
- Medium: materially increases error, drift or onboarding risk.

## Risk summary

| ID | Severity | Status | Risk |
|---|---|---|---|
| R-001 | Critical | CLOSED ON HANDOFF BRANCH | Operations now documents snapshot v6, current v4/v5 rejection and the historical-release boundary |
| R-002 | High | CLOSED ON HANDOFF BRANCH | README now separates optional local GPG use from canonical Ed25519 update authority |
| R-003 | Critical | OPEN / BLOCKER FOR REAL TENANTS | No proven full control-plane disaster backup and restore drill |
| R-004 | High | REVERIFY | Frankfurt live identity is unknown; Boston proof is only partial |
| R-005 | High | OPEN | Boston/Frankfurt environment classification conflicts with the not-production-ready policy |
| R-006 | High | OPEN | Route/role and API-contract debt remains at a security boundary |
| R-007 | High | OPEN | Required real-VM install/update/rollback/reboot evidence is not tracked in the handoff |
| R-008 | High | REVERIFY | Alpha51 GitHub release chain is verified; portal equality and installed release floors are not |
| R-009 | Medium | OPEN | External package/repository/CA endpoints can become stale without a live verification gate |
| R-010 | Medium | CLOSED ON HANDOFF BRANCH | Architecture, onboarding and implementation-status documents are reconciled with Alpha51 |
| R-011 | High | OPEN | Access, signing-key, provider and incident custodians are not assigned in the handoff |
| R-012 | Medium | CLOSED/MITIGATED ON HANDOFF BRANCH / CLEAN MAIN REVERIFY | Root scaffold, duplicate worktrees/branches and listed debris were removed; the incoming team must verify a clean `main` checkout |
| R-013 | Medium | OPEN | Browser golden-path, critical-endpoint and latency evidence remains incomplete |
| R-014 | Medium | OPEN | Incident response, escalation and postmortem ownership are not formally defined |

## Detailed risks

### R-001 — Snapshot contract documentation mismatch

- Evidence: this branch updates docs/OPERATIONS.md and its Turkish pair to
  snapshot v6, states that the current updater/rollback path rejects v4/v5, and
  limits an older snapshot to its matching immutable historical recovery
  release and rollback helper.
- Impact: an incoming operator can select an incompatible rollback procedure or
  believe a non-restorable snapshot is accepted.
- Closure basis: the English/Turkish runbooks now match the source contract.
  Until merge, immutable Alpha51 scripts and contract tests remain authority.
- Status: CLOSED ON HANDOFF BRANCH. This is documentation closure only; live or
  disposable restore evidence remains governed by R-003 and R-007.

### R-002 — Release-signing authority ambiguity

- Evidence: this branch makes README identify Ed25519 signed manifest v2,
  release sequence, pinned public key and exactly six assets as tagged-release
  update authority. Optional local GPG signing is explicitly non-authoritative.
- Impact: a team can publish integrity-only or optional artifacts while
  believing privileged update authority exists.
- Closure basis: README and release-signing guidance now make the authority
  boundary explicit; the Alpha51 official manifest/signature and six-asset set
  are verified.
- Status: CLOSED ON HANDOFF BRANCH. Portal/live equality remains tracked by
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

### R-004 — Incomplete live identity

- Evidence: Boston has a public HTTP 200 recovery observation and reports
  Alpha51, but panel/agent commits, schema and rollback state are not captured.
  Frankfurt's current version is not proven.
- Impact: rollout can mix panel, agent, schema, UI or peer DNS generations.
- Immediate control: no mutation based on assumed parity. Complete
  LIVE-STATE-2026-08-29.md read-only.
- Exit criteria: both nodes have dated panel/agent version and full commit,
  schema, UI asset, unit, operation-idle, release-floor and v6 snapshot proof.
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

### R-008 — Portal and installed Alpha51 equality not fully attested

- Evidence: the [Alpha51 GitHub release](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51),
  tag/commit identity, tagged-release CI, exactly six immutable assets and the
  official Ed25519 manifest/signature are verified. The manifest-authorized
  archive is 22,644,115 bytes with SHA256
  `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`.
  The Git tag itself is unsigned; it is not update authority. Portal bytes and
  either server's installed release-sequence floor remain unproven.
- Impact: the verified GitHub release can still differ from portal or installed
  state.
- Immediate control: use the official Ed25519 manifest as release authority and
  do not call either server current from the tag or Boston version string.
- Exit criteria: portal source/staged equality and both installed floors are
  recorded with bounded sanitized evidence against the verified release.
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

- Evidence: this branch aligns README with Alpha51 and the signed-update path,
  makes Roadmap metrics generated rather than hand-maintained, describes the
  role-aware web/src/nav.ts registry in both UI architecture files, and replaces
  the generic web/README.md template with product-specific onboarding.
- Impact: incoming engineers can select obsolete commands, misunderstand
  implemented behavior or rebuild scaffolding instead of the product.
- Closure basis: README, Roadmap status, architecture and web onboarding are
  reconciled with Alpha51; stale factual snapshots are dated or generated.
- Status: CLOSED ON HANDOFF BRANCH. Future facts must still be updated or
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

- Evidence: this branch removes the unused root package.json/package-lock.json
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
- Exit criteria: the cleanup is closed/mitigated on this branch; incoming-team
  evidence shows a clean `main` checkout with only intentional registered
  worktrees. Tracked `.design-sync` is not debris and remains intentionally.
- Status: CLOSED/MITIGATED ON HANDOFF BRANCH / CLEAN MAIN REVERIFY. This cleanup
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

- Evidence: SECURITY.md defines private reporting and docs/AUTOPSY.md records
  failures, but no on-call, severity, incident commander, escalation timeline
  or postmortem action-owner process is assigned.
- Impact: a DNS, release or security incident can stall or be handled through
  unsafe ad-hoc changes.
- Immediate control: preserve the panel-mutation and read-only SSH boundaries;
  use the external escalation channel once assigned.
- Exit criteria: externally assigned severity model, contacts, commander,
  communications path and postmortem/action tracking; repository contains a
  secret-free incident template.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

## Acceptance rule

Risk acceptance is an accountable business decision and belongs in the
external register. An OPEN/BLOCKER risk cannot be silently converted to
accepted by changing wording in this file. Close a risk only with its exit
criteria and dated evidence.
