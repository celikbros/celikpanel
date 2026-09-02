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
| R-017 | High | OPEN | A production panel heartbeat deterministically poisons a DNS engine switch that installs packages |
| R-018 | Medium | OPEN | BIND mask preflight rejects a stock Arch root directory, so BIND cannot be reached on that image |
| R-019 | Medium | OPEN | An adopted external PowerDNS is not switch-ready for the BIND handoff it is expected to feed |
| R-020 | Low | OPEN | The `cmd/panel` race suite consumes 87 percent of its explicit 30-minute ceiling |
| R-021 | Low | RESOLVED / INVENTORY CORRECTED | Both hosts were rebuilt; identity is confirmed and the inventory now records Ubuntu and Debian 13. No Arch host remains in our inventory |
| R-022 | Critical | FIXED ON BRANCH / PROVEN ON DISPOSABLE VMS / NOT ON MAIN | `install.sh` sources the release transaction guard before any trusted release root exists, so a clean installation exits before it begins |
| R-023 | High | FIXED ON BRANCH / PROVEN ON DISPOSABLE VMS / NOT ON MAIN | `SKIP_ADMIN=1` on a fresh database leaves zero users, and the panel then exits by design, so the installer ends in a systemd restart loop |
| R-024 | Medium | FIXED ON BRANCH / PROVEN ON DISPOSABLE VMS / NOT ON MAIN | The installer discards `systemctl enable` failures and never syncs the enable links, so a fresh host can reboot with both units disabled |
| R-025 | Low | DECIDED / DOCUMENTATION CORRECTED ON BRANCH | The documented `git clone && sudo ./install.sh` journey contradicts the recovery foundation's refusal of user-owned ancestor directories |
| R-026 | High | OPEN / FIXED ON BRANCH, LIVE PROOF PENDING | PowerDNS switch rollback restored the backup main file underneath the discarded generation's WAL/SHM, leaving a malformed live database |
| R-027 | Low | DOCUMENTED LIMITATION | PowerDNS authority is certified for APT/Debian/systemd only, so Arch cannot adopt or switch to PowerDNS; Arch proofs must use BIND-only journeys |

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

### R-017 - A panel heartbeat cancels a package-installing DNS switch

- Evidence: S-1 kill-matrix run 8 (interim report SHA-256
  `85bde76952fe05ee7a7a47730b8242406bb5e0924aab739a1e8ea091b2798724`).
  `cmd/agent/dns_engine_rpc.go:1339-1361` marks the mutation finalizing before
  backend work, while the finalizing predicate at `:1597-1620,1648-1675`
  requires `WorkerPID == 0` and an empty worker identity.
  `cmd/agent/service_mutation_worker.go:120-154,202-244` durably registers the
  package worker, and `cmd/panel/service_mutation_agent.go:28,793-804`
  heartbeats every five seconds. A heartbeat landing inside that valid
  registered interval fails the predicate, poisons the manager, cancels the
  operation, and then denies rollback worker registration.
- Impact: a DNS engine switch that has to install packages - the ordinary case
  on a fresh host - can be cancelled by the panel's own liveness check.
  Conditioned on the overlap the rejection is deterministic, not intermittent.
  Run 8 left an install-ownership receipt and a leased worker identity with no
  DNS state and no journal: residue that is invisible to every existing check.
- Immediate control: none available in the product. Suppressing or delaying the
  heartbeat is not a control - the twenty-second lease
  (`cmd/agent/service_mutation_rpc.go:39`) expires through the same guard
  (`:1424-1443`), so the failure simply moves to lease expiry.
- Exit criteria: the finalizing predicate distinguishes a competing mutation
  from the owning mutation's own registered worker, a bounded worker-aware
  guard passes review, and a package-installing switch completes under a
  production-cadence heartbeat on a live fixture.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.
- S-2 update: a review-only worker-aware guard was written and tested
  (`artifacts/s2-vm-acceptance/p1/`), then rejected by its own author as unsafe
  to land. Two ordering defects remain underneath it: a protected heartbeat
  returns before generic lease renewal, so a worker clear can advance
  `UpdatedAt` past the original twenty-second lease and leave
  `LeaseExpiresAt < UpdatedAt`; and `cmd.Wait()` reaps the child before
  `tracker.clear()` removes the durable worker, so a guard in that window sees
  a correctly dead worker and still rejects. Neither a live package install
  longer than the lease nor a deterministic reap-to-clear test has been run.
  The patch is not in HEAD and must not be landed as it stands.
- Fix implemented (31 August 2026, branch `fix/alpha52-handoff-acceptance`):
  the finalizing-interval proof now accepts the owning mutation's registered
  worker by shape — deliberately without a liveness probe, which removes the
  reap-to-clear window outright rather than tolerating it; a protected
  heartbeat renews the lease instead of returning early; and both durable
  worker transitions renew the lease as well, only while the job is Running,
  so the expired-cancelling proof keeps its ordering. Unit and linux
  integration tests reproduce the run-8 ledger shape (registered apt-get,
  stalled lease, heartbeat, expiry, cancellation, clear) and pass. The live
  proof — a package-installing switch under production five-second heartbeats
  on a real fixture — remains outstanding and keeps this entry OPEN.

### R-018 - BIND preflight rejects a stock Arch root directory

- Evidence: S-1 kill-matrix run 6. `cmd/agent/dns_engine_bind_mask_linux.go`
  applies the exact `bindManagedRootMode` expectation 0755 while walking the
  mask parent chain, including `/`; the official Arch image presents `/` as
  0555. The request cannot reach even the intent journal write. Other relevant
  parents were 0755, so this is a single specific expectation rather than broad
  fixture corruption.
- Impact: the BIND engine is unreachable on a stock Arch installation. Arch is a
  required acceptance distribution under OPERATIONS.md section 3, so this also
  blocks part of the release matrix.
- Immediate control: do not `chmod` the root directory to obtain a passing run.
  Doing so conceals the rejection and makes the fixture unrepresentative.
- Exit criteria: a written determination of whether 0755 on `/` is load-bearing
  for the mask policy or an expectation that is simply wrong for a normal Linux
  root, followed by the corresponding fix or documented distribution limit.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.
- S-2 determination: `0755` on `/` is **not** load-bearing for this operation.
  The mutation descends to a verified `/etc/systemd/system` and calls
  `systemctl mask`; it never creates, renames or removes an entry directly
  under `/`. The properties actually needed - root ownership, no unsafe ACL or
  symlink escape, search permission, no group/other write - hold at `0555` as
  well as `0755`. The defect is policy conflation: `bindManagedRootMode` serves
  both managed directories and pre-existing filesystem trust anchors. A fix
  should give ancestor trust anchors their own policy rather than loosen the
  managed-root constant. Existing tests force their synthetic mask parent to
  0755 and only assert rejection of 0700, so the stock Arch shape is uncovered.
- Fix implemented (1 September 2026, branch `fix/alpha52-handoff-acceptance`):
  pre-existing filesystem ancestors now have their own policy,
  `validateInheritedBINDAnchorFD`, separate from the exact-mode assertion used
  for directories this product creates. An inherited anchor must be a
  root-owned directory, free of group and other write, free of
  setuid/setgid/sticky, ACL-clean, and world-traversable. That accepts stock
  Arch's 0555 root and ordinary 0755 while still refusing every writable,
  special-bit, foreign-owned or non-traversable shape - including 0700, which
  an existing test already pinned as a refusal and which the first draft of
  this policy wrongly accepted until that test caught it. `/var/cache/bind/celikpanel`
  and every other directory the product creates keep exact 0755. Applied to all
  four ancestor walkers (mask parent, two managed-root walks, vendor unit
  path), which matters because the mask-parent proof is shared by the PowerDNS
  path too, so the Arch block was never BIND-only. Full and tagged agent suites
  pass on Debian 13 (WSL2). The live proof on a real stock Arch host remains
  outstanding and keeps this entry OPEN.

### R-019 - An adopted PowerDNS is not a switch-ready BIND source

- Evidence: S-1 kill-matrix run 5. Production `pdns-adopt` is deliberately
  read-only and therefore does not create CelikPanel's private synchronization
  tables, while the immediate BIND handoff verifies its source through
  `celikpanel_dns_zone_sync_v3_receipts`. The handoff failed because that table
  did not exist.
- Impact: an operator who adopts an existing external PowerDNS and later
  switches to BIND through the panel may be unable to complete that switch. The
  customer-facing consequence is not yet confirmed and is the first thing to
  establish.
- Immediate control: none. Do not hand-write the private tables; that would
  invalidate any baseline built on top of them.
- Exit criteria: the customer-facing consequence is established in writing;
  if reachable through the panel, adoption either produces a switch-ready
  source through unchanged `Agent.ConfigurePowerDNSSQLite` and
  `Agent.SyncDNSZoneV3`, or the handoff stops requiring what adoption cannot
  provide.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.
- S-2 determination: the consequence is customer-facing and confirmed. An
  administrator can adopt a valid existing PowerDNS, see it become the managed
  active engine, and then be permanently unable to switch it to BIND; every
  unchanged retry fails before the intent journal while PowerDNS keeps serving.
  Fail-closed, no outage, but the advertised engine-handover operation is
  unavailable and the customer is stranded on PowerDNS. A later single-zone
  mutation can lazily create the schema and one receipt but cannot reliably
  repair every adopted zone. Adoption tests mask this because their "external"
  fixture calls the normal initializer and pre-creates the private receipt
  schema. Warrants an engineering defect and a support known-issue entry.
- Diagnosis and fix (1 September 2026, branch `fix/alpha52-handoff-acceptance`).
  S-5 attempt 7 proved the adoption half works and the BIND switch does not.
  A five-lens adversarial investigation established three independent causes,
  all now fixed:
  (1) **Masked BIND is not "stopped".** The product masks named.service and
  bind9.service so a package manager cannot start BIND behind its back, then
  its own PowerDNS-active proof accepted only "not-found" or
  "loaded"+"disabled" — never "masked". A switch on any host where BIND had
  been installed could not establish its own source, and the same predicate
  gates PowerDNS zone writes, so the residue blocked those too. A masked and
  inactive unit now counts as stopped; masked is stronger than disabled, not
  weaker.
  (2) **A five-second failure became a thirty-minute silence.** After the agent
  failed and poisoned itself, the panel entered a terminal reconcile whose
  budget for a DNS engine switch is thirty minutes on a context deliberately
  detached from the caller, polling every 250 ms. `Agent.ServiceMutationStatus`
  was the one manager entry point that never consulted the health guard, so it
  kept truthfully reporting the frozen job as running. The status response now
  carries the stable `MutationHold` code and the wait stops the moment the
  agent says it is held.
  (3) **The wedge was permanent.** A switch that installed packages and failed
  before writing any journal left an install receipt whose only classification
  was "error". Startup recovery turned that into a poisoned mutation manager,
  and because the poison is recomputed from durable state nothing ever changes,
  every boot reproduced it: DNS served, the panel answered, and the host could
  never accept another mutation. Where the active state and its own ownership
  receipt agree and the target owns nothing, authority provably never moved;
  that is now classified as recoverable and the ledger job fails cleanly. A
  target that owns authority while another engine is active — the Boston shape
  — still fails closed.
  Each fix has a test that reproduces the incident's exact error string on the
  unfixed tree. Full and tagged agent suites pass on Debian 13 (WSL2). R-019
  stays OPEN until a live adoption-to-BIND switch completes on a real VM.
- S-6 live proof (2 September 2026): the three-cause fix met real machines.
  Cause 1 held completely - the Debian 13 external-PowerDNS adoption-to-BIND
  journey passed end to end for the first time in eight attempts. Cause 3 held
  in substance - the pre-intent wedge no longer poisons, the job fails cleanly
  with `agent_restarted_before_dns_engine_switch_commit`, a reboot changes
  nothing, and the retry converges the host - but the first retry's caller was
  told 75, which traced to the kill-matrix trigger's heartbeat contract
  demanding `WorkerPID == 0` mid-switch, the pre-R-017 mistake transplanted
  into the harness; fixed, with a test reproducing the exact S-6 error string.
  Cause 2 held on latency (30 minutes to 5.5 seconds, agent-error-to-first-byte
  9.8 ms) but the public body did not name the hold; it now returns
  `DNS_ENGINE_MUTATIONS_HELD` with the hold code as its detail. Still
  unproven: the Boston negative (both production setup chains failed before the
  measured cell, one of them exposing R-026) and Arch, which cannot enter the
  journey at all (R-027). R-019 stays OPEN for the Boston negative and a
  clean-caller retry on a real host.
- Residual, deliberately not changed (1 September 2026):
  `validateDNSEngineStateSnapshot` judges a persisted journal snapshot's
  recorded GID against `serviceMutationRequiredOwnerGID`, which is re-derived
  from `/etc/group` once per process. A durable record judged against a
  runtime-derived expectation is the same shape as cause 3 above, and a
  celikpanel GID renumber between journal write and validation would invalidate
  every persisted journal at once. Decoupling it was attempted and reverted:
  `TestDNSEngineSwitchJournalRequiresExactServiceOwnerForState` caught that the
  check also rejects a snapshot taken from a state file whose ownership was
  wrong at capture time, which is worth keeping, and the brick scenario needs
  both a long-stuck journal and a GID renumber — and stuck journals are what
  cause 3's fix removes. The clean repair is to record the expected GID in the
  journal and validate against that, which is a durable-schema change with
  migration consequences and is not urgent.

### R-020 - The panel race suite is close to its timeout ceiling

- Evidence: `go test ./cmd/panel/ -race -count=1 -timeout 30m` returned `ok` in
  1574.958s of an 1800s ceiling on Debian 13 WSL2 at commit
  `f243304d1aadc94c0f26342d2d3270902ad43d4b`, leaving 225.042s of margin.
- Impact: on a slower host the suite exceeds the ceiling and fails as a timeout
  rather than as a test failure, which reads as a hang and costs disproportionate
  investigation time.
- Immediate control: keep the explicit `-timeout 30m`; the default would be
  worse. Report the elapsed time in every acceptance run so the trend is visible.
- Exit criteria: either the suite's runtime is reduced, or the ceiling is raised
  with a recorded rationale and a measured margin on the slowest supported
  acceptance host.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-021 - Deployment host identity does not match the inventory

- Evidence: on 31 August 2026 both deployment targets presented an SSH host key
  that differs from the recorded one, and an SSH banner naming an operating
  system the inventory does not record: `2.25.80.4` is recorded as Debian 13 and
  reports Ubuntu, `72.62.38.15` is recorded as Arch and reports Debian 13. No
  Arch host is currently visible. The observed fingerprints are held by the
  operator and are deliberately not recorded here, because an unverified
  fingerprint written into this register would later be mistaken for a trusted
  baseline.
- Impact: the identity of both production-shaped hosts is unestablished. A host
  key change is consistent with a rebuild, but an operating system change is
  not explained by the clean-server rule alone, and the alternative explanation
  is that the addresses now resolve to different machines. Separately, the
  required Arch acceptance host is unaccounted for.
- Immediate control: no deployment, no update and no configuration change on
  either address. Read-only diagnosis only, and only after the operator has
  confirmed the presented host keys out of band. Acceptance VMs must be
  disposable machines under the team's own control.
- Exit criteria: the operator confirms each host key against provider console
  or on-host evidence, the recorded operating system for each address is
  corrected or the address is retired, and an Arch acceptance host exists.
- Resolution (1 September 2026): the operator confirmed both hosts were rebuilt,
  which explains all three changes at once — new SSH host keys, new operating
  systems, and new provider-injected login keys. Identity is now established on
  evidence rather than assumption: the operator re-added the deploy key through
  the provider console, which binds to the real machine, and the host key each
  server reports from inside
  (`/etc/ssh/ssh_host_ed25519_key.pub`) matches the key presented on the wire
  exactly — `SHA256:8Zje…` for `2.25.80.4` (hostname `boston`, Ubuntu 24.04.4)
  and `SHA256:DV/e…` for `72.62.38.15` (hostname `frankfurt`, Debian 13).
- Corrected inventory: `2.25.80.4` is boston, Ubuntu 24.04.4, PowerDNS
  secondary. `72.62.38.15` is frankfurt, Debian 13, BIND primary. The recorded
  mixed-engine pair survived the rebuild and matches the durable state receipts
  on both hosts.
- Health at resolution: CelikPanel installed and complete on both
  (`/etc/celikpanel/install.complete`, binaries dated 30 August), panel and
  agent active, no active mutation request, no DEGRADED or fail-closed entry in
  fourteen days, and zero restarts on all four units. Boston's former unbounded
  restart loop did not survive the rebuild.
- Remaining, tracked here rather than as a blocker: our inventory no longer
  contains an Arch host. That does not block the OPERATIONS.md section 3 matrix,
  which runs on disposable QEMU/KVM guests built from an official Arch cloud
  image, not on these two servers. It does mean there is no long-lived Arch
  host to observe, and the R-018 inherited-anchor fix has still never met a real
  Arch machine outside that disposable matrix.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-022 - Fresh installation cannot start

- Evidence: S-2 acceptance, disposable Debian 13 and Arch VMs, commit
  `f243304d1aadc94c0f26342d2d3270902ad43d4b`. Both exited 1 with the single
  error `trusted release root is missing while sourcing release guard`, and
  `/opt/celikpanel`, `/var/lib/celikpanel`, `/etc/celikpanel` and both unit
  files remained absent. Acceptance report SHA-256
  `7126f122e815ddda59ba7d8dd060b74c937c0bb7ab61d9a18ec93734d9a46eb3`.
  `install.sh:128` computes `SRC` and `:131` sources
  `deploy/release-transaction-guard.sh`, whose check at `:16-20` accepts only
  `TRUSTED_RELEASE_ROOT` or `CELIKPANEL_TRUSTED_RELEASE_ROOT`. The fresh path
  first assigns `TRUSTED_RELEASE_ROOT=$SRC` at `install.sh:519`, inside
  `prepare_fresh_release_transaction_foundation()`, which is reached far later.
  `set -euo pipefail` at `:23` turns the guard's `return 1` into an exit.
  `download-portal/get.sh:1057-1063` hands the installer six
  `CELIKPANEL_FIRST_INSTALL_*` values and neither root variable; the file
  contains no reference to either.
- Scope: not introduced by the Alpha53 candidate. Base commit `0a5e849` carries
  the identical ordering at the same line numbers, and the ordering dates to
  `45d01ff` (Alpha51 recovery hardening, 28 August 2026). The three files are
  byte-unchanged across the candidate branch.
- Impact: no new customer can install the product by any documented route -
  neither the public `get.sh` journey nor the direct `install.sh` invocation.
  It went unnoticed because both live hosts reached Alpha52 through an
  Alpha51 update; the existing Alpha52 evidence covers updates, not a clean
  installation. Update and rollback paths are unaffected: they export
  `CELIKPANEL_TRUSTED_RELEASE_ROOT` before invoking the installer
  (`bootstrap-update.sh:389`).
- Immediate control: none. Do not instruct anyone to export a trusted-root
  variable by hand as a workaround; the guard exists to reject a root the
  caller has not verified, and hand-setting it defeats exactly the property it
  protects.
- Exit criteria: a fresh installation completes on Debian 13, Ubuntu 24.04 and
  current Arch from both the public signed `get.sh` journey and the documented
  direct invocation, with the guard still rejecting a release root the caller
  has not verified; and an acceptance test covers the bootstrap-to-installer
  entry boundary, which static and extracted-function tests did not reach.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.
- S-3 update: the entry-boundary defect itself is repaired and proven on local
  branch `fix/r-022-fresh-install` (commit `7fd5b30b`, parent exactly
  `0a5e8495…`): the signed journey now passes the verified extraction root
  from `get.sh`, a direct non-apply invocation falls back to its canonical
  installer directory, apply-only still refuses a missing inherited root, and
  the guard file is byte-identical at base and candidate. A new cross-script
  entry-boundary contract test fails on the base and passes with the fix, and
  runs in CI. The branch is unpushed and awaits operator review. Fresh install
  as a whole remains broken behind it - see R-023 - so this entry stays OPEN
  until the fix lands on `main` and a complete green install exists.

- Status (2 September 2026): the entry-boundary fix (`7fd5b30b`) is on the
  integration branch `fix/alpha52-handoff-acceptance`. Proven on disposable
  Debian 13, Ubuntu 24.04 and Arch guests in S-4 and S-5 (both entry paths, the
  R-022 contract test red on base and green with the fix, and running in CI).
  Not on `main`; the blocker ends when the branch lands.

### R-023 - A skipped first admin turns into a panel restart loop

- Evidence: S-3 acceptance, six valid fresh-install journeys on disposable
  Debian 13, Ubuntu 24.04 and Arch VMs (report SHA-256
  `7126…` superseded; S-3 manifest
  `815bf4adbf71c89f505d09293d3020b1964171485f900ab20e28089ec33eec09`). All six
  failed with one deterministic chain: `install.sh:1403` returns from
  `ensure_first_administrator` immediately under `SKIP_ADMIN=1`; migration
  `006_drop_placeholder_admin.sql` removes the placeholder administrator; the
  panel counts zero users and refuses to serve wide open
  (`cmd/panel/main.go` zero-user gate); `Restart=on-failure` turns that refusal
  into a restart loop; the installer exits before writing
  `/etc/celikpanel/install.complete`.
- Scope: pre-existing on `main`; present since the zero-user gate (3 July 2026)
  and migration 006 coexist with `SKIP_ADMIN`. Not introduced by any candidate
  branch. The zero-user refusal itself is correct and must stay: a panel with
  no users must not serve.
- Impact: there is no working non-interactive fresh installation. `SKIP_ADMIN=1`
  is a documented installer option, the apply-only contract requires it
  (`install.sh` apply-only validation), and the historical deployment recipe
  used it. The interactive journey depends on `--create-admin`, which reads the
  terminal (`cmd/panel/admin_cli.go:27,133`) and has no non-interactive mode,
  so automation cannot create the first administrator at all. The interactive
  TTY journey is unproven.
- Immediate control: do not use `SKIP_ADMIN=1` for a fresh installation. After
  an affected install, run `--create-admin` as the service user and restart the
  panel; the loop clears once one user exists.
- Exit criteria: a fresh install with `SKIP_ADMIN=1` refuses early - before any
  host mutation - with a clear message instead of ending in a restart loop; a
  documented, safe non-interactive first-administrator mechanism exists for
  automation; the interactive journey is proven on a real TTY; and the six S-3
  journeys pass end to end.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

- Status (2 September 2026): the admission property (no host mutation unless a
  usable administrator is guaranteed), the credentials-over-stdin path and the
  live-WAL-tolerant probe (`7fbc4149`, S-5 `975a5e26`) are on the integration
  branch. Proven in S-5 on disposable VMs: 15/15 admission journeys, 45/45
  refusal cells, 72/72 coverage records, and the interactive TTY path on all
  three distributions. Not on `main`.

### R-024 - Unit enablement is fail-open and unsynced

- Evidence: S-3, Arch signed-public journey. After a diagnostic reboot both
  units were disabled and inactive, while Debian and Ubuntu brought the agent
  back. `install.sh` discards errors from both `systemctl enable` calls
  (`>/dev/null 2>&1 || true`) and proves only same-boot activity; the enable
  links are never explicitly synced before the panel failure, and the harness
  stopped the VM abruptly. The evidence proves persistence loss and fail-open
  enable handling; it cannot distinguish a failed enable from an unsynced link.
- Impact: a fresh host can reboot into nothing serving, with the installer
  having reported the services as running.
- Immediate control: after any install, verify `systemctl is-enabled` for both
  units before trusting a reboot.
- Exit criteria: enable failures are fatal to the installer, the links are
  durably synced before success is reported, and a reboot test proves both
  units return on every supported distribution.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

- Status (2 September 2026): `systemctl enable` failures are fatal and the
  enable state is synced before success is reported (S-4, `7fbc4149`), proven
  by 3/3 reboots on Debian 13, Ubuntu 24.04 and Arch in S-4 and 0/3 again in
  S-5 and S-6. On the integration branch; not on `main`.

### R-025 - The clone-and-install claim contradicts the root-chain policy

- Evidence: S-3. `install.sh:1715-1721` documents that
  `git clone && sudo ./install.sh` works on a stock system, while the release
  recovery foundation rejects a release below a user-owned ancestor - which is
  exactly what a clone under `/home/<user>` is. The S-3 harness hit this
  refusal and had to restage under root-owned protected storage.
- Impact: the documented developer journey fails on the policy the product
  itself enforces. Whichever is right, the other must change.
- Immediate control: stage direct installs below a root-owned directory.
- Exit criteria: either the documentation drops the home-directory claim and
  states the staging requirement, or the policy deliberately admits a defined
  fresh-install staging shape; decided in writing, not by drift.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-026 - PowerDNS rollback mixes database generations

- Evidence: S-6, Boston negative attempt 2 (report SHA-256
  `d15ab8ac…` superseded by the S-6 manifest
  `b5105623daa3fcebe444b3e6e293a0f6e9a5b3e7431ad0df35479d619d7e1701`).
  The production rollback of a BIND-to-PowerDNS switch stopped PowerDNS,
  removed the live main file and renamed the backup into place, but never
  touched the `-wal` and `-shm` files the target had created against the
  candidate database. SQLite then replayed a WAL from a different generation
  into the restored main file; the summary records
  `live_database_generation_mixed_with_retained_wal: true` and
  `malformed_database: true`, the setup job stayed `running`/`leased` with a
  `rolling-back` journal, and the mutation manager poisoned. The forward path
  already refuses sidecars at staging (`requireNoPDNSDatabaseSidecars`);
  rollback had no counterpart.
- Impact: any rolled-back switch into PowerDNS can leave the host with a
  malformed live database and a wedged ledger - the R-019 shape entered from
  the other side.
- Fix on branch (2 September 2026, `fix/alpha52-handoff-acceptance`):
  `restorePDNSDatabase` now removes the discarded generation's `-journal`,
  `-wal` and `-shm` before the backup is renamed in, and also in the
  already-restored branch. A test reproduces the incident's exact refusal
  (`PowerDNS database has an unresolved SQLite sidecar`) on the unfixed tree.
- Exit criteria: a deliberately failed BIND-to-PowerDNS switch on a real VM
  rolls back to a database that opens cleanly, the ledger job fails cleanly,
  and a subsequent switch is accepted.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-027 - PowerDNS authority is APT-only by design

- Evidence: `certifyAPTPDNSCapabilities` (`cmd/agent/dns_engine_pdns_unit.go`)
  requires `PackageManager == APT`, `DistroFamily == Debian` and systemd, and
  `docs/DISTRO-SUPPORT.md` lists no pacman mapping for PowerDNS. S-6 Arch
  attempt 2 confirmed the consequence: a stock repository PowerDNS serving
  three zones was classified unmanaged and adoption returned
  `DNS_ENGINE_WORKFLOW_REQUIRED`.
- Impact: not a defect, a boundary. It means the R-019 adoption journey cannot
  run on Arch at all, and the R-018 inherited-anchor fix can only be proven on
  Arch through a BIND-only journey (fresh host to BIND), never through
  PowerDNS adoption.
- Immediate control: none needed. Do not attempt to relax the certification
  without a pacman provenance model equivalent to the APT one.
- Exit criteria: either a certified pacman PowerDNS profile with the same
  provenance guarantees, or the limitation stated in customer-facing
  documentation. Either is acceptable; silence is not.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

- Decision (2 September 2026): the root-chain policy stays. A release below a
  user-owned ancestor is exactly the thing the recovery foundation exists to
  refuse, and no documentation claim outranks that. The installer comment that
  promised `git clone && sudo ./install.sh` from a stock system now states the
  real requirement - stage the checkout below a root-owned directory - and the
  recovery foundation's refusal already names where to stage (S-4). A raw
  checkout still lacks `bin/panel` and builds from source; that is unchanged.

## Acceptance rule

Risk acceptance is an accountable business decision and belongs in the
external register. An OPEN/BLOCKER risk cannot be silently converted to
accepted by changing wording in this file. Close a risk only with its exit
criteria and dated evidence.
