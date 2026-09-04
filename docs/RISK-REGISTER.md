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
| R-003 | Critical | OPEN / BLOCKER FOR REAL TENANTS / ARCHIVE, RESTORE AND ENGINE REINSTALL PROVEN ON WSL / REAL VM PENDING | The panel archives and restores its own control plane (slices 1 and 2); the first WSL drill restored a fresh host in 23 s but the restored host cannot reinstall its DNS engine |
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
| R-018 | Medium | FIXED ON BRANCH (FIVE LAYERS) / LIVE PROOF OF THE FIFTH PENDING | The Arch BIND path was never wired end to end: root-anchor rule, managed root, config ownership, stock options and journal shape each assumed Debian; all five now follow the pacman package and a fresh Arch host reaches a serving BIND |
| R-019 | Medium | OPEN | An adopted external PowerDNS is not switch-ready for the BIND handoff it is expected to feed |
| R-020 | Low | CLOSED ON MAIN / EVERY SHARD MEASURED UNDER THE LINE | The CI race shard for `D` ran at 88 percent of its 8-minute ceiling on `main`; the local 30-minute single-process run sits at 80 percent |
| R-021 | Low | RESOLVED / INVENTORY CORRECTED | Both hosts were rebuilt; identity is confirmed and the inventory now records Ubuntu and Debian 13. No Arch host remains in our inventory |
| R-022 | Critical | FIXED ON BRANCH / PROVEN ON DISPOSABLE VMS / NOT ON MAIN | `install.sh` sources the release transaction guard before any trusted release root exists, so a clean installation exits before it begins |
| R-023 | High | FIXED ON BRANCH / PROVEN ON DISPOSABLE VMS / NOT ON MAIN | `SKIP_ADMIN=1` on a fresh database leaves zero users, and the panel then exits by design, so the installer ends in a systemd restart loop |
| R-024 | Medium | FIXED ON BRANCH / PROVEN ON DISPOSABLE VMS / NOT ON MAIN | The installer discards `systemctl enable` failures and never syncs the enable links, so a fresh host can reboot with both units disabled |
| R-025 | Low | DECIDED / DOCUMENTATION CORRECTED ON BRANCH | The documented `git clone && sudo ./install.sh` journey contradicts the recovery foundation's refusal of user-owned ancestor directories |
| R-026 | High | FIXED ON BRANCH / LIVE PROOF PENDING | PowerDNS switch rollback restored the backup main file underneath the discarded generation's WAL/SHM, leaving a malformed live database |
| R-027 | Low | DOCUMENTED LIMITATION | PowerDNS authority is certified for APT/Debian/systemd only, so Arch cannot adopt or switch to PowerDNS; Arch proofs must use BIND-only journeys |
| R-028 | High | FIXED ON BRANCH / LIVE PROOF PENDING | The active-BIND proof inside a BIND-to-PowerDNS switch refused the pdns.service mask the switch's own install guard had just created, so every switch that had to install PowerDNS failed at its source proof |
| R-029 | High | FIXED ON BRANCH / LIVE PROOF PENDING | DNS identity staging on a host that has never run an engine refused because zones were pending, which every zone on such a host is by construction; adding a domain before setting up DNS made the first engine install unreachable |
| R-030 | Medium | FIXED ON BRANCH | A read-only mutation status RPC failed outright when the agent's ledger could not be brought up or was poisoned at startup, so a probe could not tell a dead agent from one that is serving and refusing |
| R-031 | High | FIXED ON BRANCH / LIVE PROOF PENDING | Rolling back a BIND-to-PowerDNS switch re-enabled the bind9.service alias before named.service, which cannot succeed on APT hosts; the source BIND never came back and the recovery poisoned the ledger |
| R-032 | High | FIXED ON BRANCH / LIVE PROOF PENDING | Returning to an engine the host had used before, with the switch interrupted after package install, was read as the half-finished handover shape because the former engine's stranded ownership receipt is never retired; recovery poisoned the ledger on an ordinary operator action |
| R-033 | High | FIXED ON BRANCH / LIVE PROOF PENDING | A first DNS engine install that failed after package install on a host with no state left an install receipt the abort proof called inconsistent, so the ledger was poisoned on the very first DNS action and stayed poisoned on every boot |
| R-034 | High | FIXED ON BRANCH / LIVE PROOF PENDING | Every WireGuard config apply fails because the staged file name is not a valid interface name for `wg-quick strip`; the failed rollback then poisons the host's mutation manager with no API way out |
| R-035 | Medium | OPEN / DESIGN | The firewall cannot be enabled on a host without a discoverable sshd, and the product cannot install one; such hosts never get `firewall.nft` |
| R-036 | Medium | OPEN / DESIGN | The mail profile refuses a host whose OS hostname is not a fully qualified name, and nothing in the product sets or explains the hostname |
| R-037 | Medium | GUARDED ON BRANCH / A SECOND EMBED WAS AFFECTED AND IS FIXED | A Windows working copy checked out before `.gitattributes` keeps CRLF, so a locally built panel embeds CRLF migrations and refuses every database a released panel created |
| R-038 | Critical | STOPPED SHAPE FIXED AND PROVEN LIVE / RUNNING SHAPE IS R-039 / ROLLBACK PROOF OWED | A host that already carries the DNS engine's packages can now adopt that engine through the panel with an explicit acknowledgement, when the engine is stopped; a running unmanaged engine is still refused and is tracked as R-039 |
| R-039 | High | FIXED AND PROVEN LIVE / REAL VM PENDING | Adopting a DNS server that is running and unmanaged needs a durable pre-intent record: the agent must stop a service it does not own before its proofs run, and a crash in that window would leave the operator's DNS stopped with nothing to recover from |
| R-040 | High | FIXED / IN REVIEW (PR #80) / BROWSER ROUND OWED | The service list reports a host it has never scanned as "not installed": no observation and no service serialise to the same answer |
| R-041 | Medium | FIXED ON BRANCH / GUARDED | The web contract does not decode the `reinstall_active` action the panel already returns, so the reinstall the restored host needs shows as an invalid preview in the browser |
| R-042 | High | FIXED AND PROVEN LIVE / REAL VM PENDING | A hand-configured authoritative BIND is refused: the generation will not write into an options block that already sets recursion, allow-recursion, allow-query-cache or allow-transfer, and an authoritative server almost always sets the first of those |
| R-043 | High | FIXED AND PROVEN LIVE AT TWO CRASH POINTS / A THIRD IS R-045 | A crash during a running takeover is recovered by the switch rollback, which stops units - so the recovery would stop the DNS server the takeover promised never to interrupt |
| R-044 | Medium | FOUND / NOT YET FIXED | A BIND configured with `view` blocks is not understood by the takeover: a recursion set inside a view silently overrides the panel's options, and zones outside views fail late in the config check |
| R-045 | High | FOUND / CAUSE NOT YET DETERMINED | A takeover crashed after its target was verified but before it was finalized is neither finalized nor rolled back: the recovery cannot find the generation pointer, fails closed and holds the ledger |

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
- Status (3 September 2026): scoped on the integration branch. Today only
  per-domain backups exist; nothing captures the panel's own state (SQLite,
  `secret.key`, agent private state, DKIM and WireGuard keys, panel TLS,
  firewall snapshot). `docs/DISASTER-RECOVERY.md` records the exact inventory,
  the consistency mechanism (the update path's online WAL-aware SQLite copy),
  the key rule (backup key separate from `secret.key`, shown once), the restore
  entry point and the drill. Implementation follows in that order; the WSL
  drill precedes the real-VM drill.
- Drill (3 September 2026, WSL, `docs/DISASTER-RECOVERY.md` §6): host A rebuilt
  from 68b83cc, archive of 10 members taken with the services running; host B
  a brand-new guest restored through the installer hook in 23 s (disaster to
  serving 1 min 58 s, archive age 5 min 30 s, nothing lost). Proven through
  the panel on B: old administrator password, secret key and fingerprint,
  DKIM private and public key, domain list, engine state at the same epoch,
  served TLS certificate. Failed: the DNS infrastructure screen reports BIND
  active and degraded and refuses the install (`target_already_active`,
  `source_degraded`), the commit answers "preview expired" for a preview that
  was never registered, and the restored service scan cache shows host A's
  BIND as running. Fix in progress: an honest "reinstall the active DNS
  server" path, an honest blocked-preview answer, and a scan cache that does
  not survive a restore.
- Closed since (3 September 2026): the restored host now reinstalls the DNS
  server it already owns, through the panel, at the same epoch and ownership
  (`reinstall_active`); on host B the commit took 10 s and the zone answered
  the same SOA and DKIM record as host A. The blocked-preview answer, the
  restored component scan and the kept-configuration line are fixed with it.
  Two agent defects the live attempt exposed are fixed and covered: the
  journal validator demanded a second unit set for a same-engine mutation,
  and the abort proof read the reinstall's own install receipt as a
  contradiction and poisoned the ledger.
- Still owed: the same run on a disposable real VM; a stored database
  password on host A so a sealed ciphertext is actually opened on host B;
  the VPN, firewall and mail members once R-034, R-035 and R-036 allow them.
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
- S-9 T1 (3 September 2026, real Arch cloud VM under KVM, root `/` 0555,
  candidate fe6c2c9): identity staged 200, preview 200 with `action=install`,
  `blockers=[]`, `pending_zone_count=1`; the commit installed BIND through
  pacman, built the generation under `/var/named/celikpanel`, enabled and
  started the unit, then failed at `verify started BIND vendor unit: ss
  returned a non-canonical DNS listener peer endpoint`
  (`parseCanonicalDNSPort53ListenerRow`, `dns_engine_legacy_guard.go`), rolled
  back cleanly (`DNS_ENGINE_CHANGE_NOT_COMMITTED`, no hold). The four earlier
  layers are proven on the real VM; the inherited-anchor walk was traversed
  (the harness recorded `null` only because it asserts that field on a full
  pass). Fifth layer: the listener verifier rejects the peer column shape of
  Arch's iproute2 `ss` output. Fix pending, from the captured output.
- Fifth layer fixed on branch (3 September 2026): the listener proof now
  accepts every spelling iproute2 uses for an absent peer (`*:*`,
  `0.0.0.0:*`, `[::]:*`) as a closed set, and refuses shapes it never emits.
  The rejected row was not retained in the campaign evidence, so the spelling
  is reconstructed; the Arch VM cell is rerun to prove it.
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
- Live proof (3 September 2026, WSL2 Arch guest with `/` at 0555, driven
  through the public API): after R-029's third layer and R-033 the first BIND
  switch reached the host and exposed, one behind the other, four more
  Debian assumptions in the pacman path. (1) The managed-root walk only knew
  `/var/cache/bind`; nothing prepared `/var/named/celikpanel`, so the
  publisher refused the vendor parent - now a pacman walk proves `/var/named`
  is the bind package's root:named directory, hardens it in place to 1770 (the
  sticky bit APT gets from dpkg-statoverride) and creates the managed root
  root:root 0755. (2) The config owner contract assumed `/etc/named.conf` is
  root:root 0644; Arch ships it root:named 0640 - the contract now follows the
  layout and resolves the `named` group. (3) Arch's stock options carry
  `allow-recursion { 127.0.0.1; ::1; }` and `allow-transfer { none; }`, which
  the managed block refused as operator-owned; exactly those two stock lines
  are superseded, any other value still refuses. (4) The switch journal
  demanded 0644 root:root for the pacman single-file set; it now follows the
  layout. Result: preview with zero blockers, commit 200 in 9 s, engine ready
  at epoch 1, the zone authoritative over UDP and TCP, still serving after a
  distro restart and after a full WSL VM reboot. `pacman -Qkk bind` reports
  `/var/named (Permissions mismatch)` afterwards; that is the sticky hardening
  and is expected. The real-VM proof (S-9 T1) remains the exit criterion.

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
- S-9 T2 positive (3 September 2026): the pre-intent kill landed
  (`kill_proven=true`, exit 137) and the ordinary agent returned; the first
  status probe matched the ordered answer
  (`agent_restarted_before_dns_engine_switch_commit`), then the driver timed
  out in its 120 s panel-readiness loop and aborted before retrieving the raw
  bodies. UNVERIFIED by the campaign's own rule; the cell is rerun with the
  bodies captured before evaluation. The R-019 external-PowerDNS-to-BIND
  cell and the T4 matrix (15 journeys, 3 reboot journeys, 0 failures) passed
  on the same candidate.
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
- S-7 live proof (2 September 2026, manifest
  `413d67aa28cca17c7e67912f5e911a3a24481b70b388c3e0e74659706b31c283`): the
  worker-bearing heartbeat fix held - the pre-intent wedge cell passed with the
  first same-identity retry returning 0 in 14 s and the host converging; the
  public hold contract held - the fsync-EIO cell returned 503
  `DNS_ENGINE_MUTATIONS_HELD` with details `["ledger_ambiguous"]` in 8 s with
  no internal text; the Debian adoption-to-BIND journey and the full S-5 matrix
  stayed green. Still unmeasured: the Boston negative, whose setup chain hit
  R-028 at the BIND-to-PowerDNS step. R-019 stays OPEN for that one cell.
- S-8 (3 September 2026, manifest `746228bdc2c01ecda8fbb65067ddb29a0dea9e740694ce461ecff1c50b11c568`): the Boston setup chain passed every
  step - empty, BIND epoch 1, PowerDNS epoch 2 in 21.5 s, 12.2 s and 2.8 s -
  which is R-028 proven live. The measured negative cell then returned 2 with
  a null proof from the harness controller, so the exact refusal is still
  unmeasured. T2 positive proved the pre-intent kill and the restart, then the
  harness stopped at its secret catalog before the retry. T3 passed again.
  R-019 stays OPEN for the Boston negative only.
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
- Closed 4 September 2026 (PR #82), by measurement twice over. The first
  split carved the D group in two, and its run put those four shards at 202,
  182, 115 and 113 s - and A-through-C at 258 s, which had run at 191 s
  before. Nothing had regressed: the control-plane tests written that week
  were 40 of that shard's 147 and they landed there. So the rule was applied
  again, C carved twice with the control-plane tests taking their own shard,
  which is where the next growth goes.
- Measured on the final layout, all fourteen shards: N-Q 226, L-M 213,
  S-except-service 209, DNS-except-engine-and-zone 197, E-K 196, T-Z 189,
  DNS engine 176, control plane 167, R 158, C-except-control-plane 157, DNS
  zone 115, D-except-DNS 113, service 99, A-and-B 91. Every one under the
  240 s exit line; the highest, N-Q, is the next candidate if it grows.
- The shards stay disjoint and exhaustive: proven against all 1058 tests the
  toolchain discovers plus the boundary names, each falling into exactly one
  shard. The contract test pins fourteen patterns and skips and rejects the
  shard names of both superseded layouts, so neither can come back whole.
- What keeps it closed: the entry's rule is that a shard over the line is
  split and the ceiling is never raised, and the CI comment now carries the
  measured history so the next person can see that each split answered a
  measurement rather than a guess.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.
- Measured again (2 September 2026): the single-process run took 1448 s of
  1800 s (80 percent) on a 16-core Debian 13 WSL2 guest on its own ext4 at
  `e8c73f16`; 998 top-level tests, none using `t.Parallel`, so the package is
  serial and bound by fsync and the race detector, not by cores. The real
  exposure was in CI, not locally: the `panel-race` job is sharded seven ways
  with `-timeout=8m` per shard, and on `main` run 33297192912 the `D` shard's
  test step took 421 s, 88 percent of the ceiling (430-435 s job time in two
  consecutive runs). `N-R` ran 328 s, `H-M` 308 s, `S` 248 s. Per-test timings
  from the same tree put 77 percent of `D` in `TestDNS*`.
- Fix on branch: ten shards instead of seven. `D` is split into
  `^TestDNS(Engine|Zone)` and the rest of `D`
  (`-run '^TestD' -skip '^TestDNS(Engine|Zone)'`), `S` into `^TestService` and
  the rest, `E-G` and `H-M` regrouped as `E-K` and `L-M`, `N-R` as `N-Q` and
  `R`. A pair keeps the letter pattern on the complement and skips the
  carved-out prefix, so coverage is exhaustive by construction: every one of
  the 998 measured names lands in exactly one shard, an empty `-skip ''` skips
  nothing (verified on go1.26.5), and `deploy/test-go-toolchain-contract.sh`
  pins all ten patterns and skips. Projected worst shard about 220 s, 46
  percent, using the per-letter CI/WSL ratios measured on that run. The ceiling
  stays at 8 minutes: it is a hang detector, not a budget, and the rule now
  written into the workflow is to split any shard whose measured step exceeds
  half of it.
- Exit criteria (updated): the branch's first CI run shows every
  `Race-test panel boundaries` step under 240 s. The local single-process
  ceiling stays at 30 minutes with the elapsed time reported per acceptance
  run.
- Measured (3 September 2026, PR #78, run 33733419870): all ten shards
  green; `Race-test panel boundaries` step times were D-except-DNS 257 s,
  DNS engine and zone 248 s, N-Q 226 s, L-M 208 s, E-K 206 s, S-except-service
  201 s, A-C 191 s, T-Z 190 s, R 151 s, Service 97 s. Two shards sit above the
  240 s exit line by seconds. The rule stands: those two are split next, the
  ceiling is not raised. Not a merge blocker for PR #78.

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
- S-7 live evidence (2 September 2026): the deliberate target-started failure
  proved the fix's cleanup on a real host - after rollback the live PowerDNS
  database was the exact preimage with `integrity_check` ok and no WAL, SHM,
  candidate or staging file beside it, and the job ended failed/interrupted
  with `dns_engine_switch_rolled_back_after_restart`. Two things stop this from
  closing: the ordered cell's first status probe after the ordinary restart
  failed with an RPC error the harness discarded, and the converged state was
  observed only in a non-counting diagnostic, so "the next switch is accepted"
  is still unproven. The probe failure led to the status RPC change recorded
  under R-030.
- S-8 (3 September 2026): the probe spoke (R-030) and the ordered failure
  is now named: rollback recovery failed re-enabling the source BIND (R-031).
  The database cleanup held again. R-026 closes with T5 after R-031.
- S-9 T5 (3 September 2026, Debian 13 VM, candidate fe6c2c9, agent code
  identical to the current branch): the killed switch rolled back cleanly
  (R-031 proven: `named.service` with the `bind9.service` alias active and
  enabled, PowerDNS stopped, the private snapshot restored byte-equal, no
  residue, no hold). The first subsequent switch then ended with `agent
  rejection "DNS engine switch reached its verified target but finalization
  did not complete"` and the hold `finalize active DNS engine switch:
  committed DNS engine switch has no exact install or active ownership
  provenance` (`exactCommittedDNSEngineProvenanceOnHost`). Cause: the
  rollback removed the target's receipts but left its packages installed, so
  the retry installed nothing, wrote no install-ownership receipt, and had no
  active receipt either. Fix pending: a switch that adopts already-present
  target packages must record that adoption as provenance exactly as an
  install does. The harness preinstalled the packages, which is the same
  condition any already-installed host presents.
- Correction to what the T5 note said (3 September 2026): the rollback did
  not remove the target's receipts. The evidence
  (`t5-proof.json`, `pdns_preinstall`) shows the harness had installed the
  target packages before the measured switch, so the first switch already
  found nothing missing and never wrote a receipt. The defect is therefore
  not retry-specific and R-038 records its real scope.
- Fixed on branch (3 September 2026) together with R-038: an adoption is an
  installation with an empty missing set, recorded by the same constructor in
  the same receipt with this mutation's identity.
- Scope note (3 September 2026): the adoption provenance closes the agent
  half only. The live run in R-038 shows the panel refuses a host with
  preinstalled packages before any mutation is dispatched, so the end-to-end
  flow is still broken and R-038 carries it.
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

### R-028 - The BIND-to-PowerDNS switch refuses the mask it created

- Evidence: S-7, Boston negative attempt 1 (manifest SHA-256
  `413d67aa28cca17c7e67912f5e911a3a24481b70b388c3e0e74659706b31c283`). The
  fresh chain completed empty-to-BIND with rc 0; the production BIND-to-PowerDNS
  setup then returned rc 1 after 27 s. The bounded agent journal shows the
  PowerDNS packages being installed at 10:01:37 and, two seconds later,
  `DNS engine switch to pdns at epoch 2 failed: pdns.service is not exactly
  absent or loaded, inactive, and disabled`; the host shape capture shows
  `pdns.service LoadState=masked ActiveState=inactive`. The switch installs
  PowerDNS under a persistent mask (`dns_engine_pdns_install.go`) precisely so
  the package manager cannot start it early, then proves its active BIND source
  with `verifyExactActiveBINDUnitStates`, whose PowerDNS clause accepted only
  absent or loaded+inactive+disabled. The product refused the state it had just
  created - the mirror image of R-019's second cause.
- Impact: on any host where PowerDNS is not already installed, a switch from
  BIND to PowerDNS cannot pass its own source proof. The Boston negative, the
  safety-critical half of R-019's cause 3, could not be measured because its
  setup chain runs exactly this switch.
- Fix on branch (2 September 2026): the PowerDNS clause of
  `verifyExactActiveBINDUnitStates` accepts masked-and-inactive, the same
  relaxation `exactStoppedBIND` received for R-019; a masked-but-active or
  masked-but-activating PowerDNS is still refused. A test reproduces the S-7
  error string verbatim on the unfixed tree.
- Exit criteria: the Boston setup chain (empty, BIND epoch 1, PowerDNS epoch 2)
  returns 0 at every step on a real VM and the measured negative cell runs.
- S-8 (3 September 2026): proven live - the Boston chain's epoch-2
  BIND-to-PowerDNS setup returned 0 in 12.2 s on Debian 13. Only the measured
  negative cell remains, and that is R-019's.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-029 - Fresh-host identity staging is deadlocked by its own pending zones

- Evidence: S-7, T1 Arch BIND-only attempt 1 (same manifest). A fresh Arch
  guest at epoch 0 with one seeded zone (`status=pending`, generation 4) and
  no engine installed sent `PUT /api/v1/settings/dns-setup` with a standalone
  identity and received `409 DNS_ENGINE_WORKFLOW_REQUIRED`; no switch was
  attempted. The refusal is `stageDNSIdentityLocked`'s publication gate
  (`hasDNSPublicationPending`), which counts every zone whose applied
  generation lags its desired one. On a host that has never run an engine that
  is every zone: nothing exists that could apply them. The first engine install
  requires staged identity (`TestDNSEngineFirstInstallRequiresStagedDNSIdentity`),
  so the ordinary order - add a domain, then set up DNS - could not reach the
  install at all. The same gate is repeated inside the staging transaction.
- Impact: any fresh install where a domain exists before DNS is set up cannot
  install its first DNS engine through the panel. The only way out was deleting
  the zones. Not Arch-specific: the same path refuses on every distribution.
  Correction (3 September 2026, measured on a fresh Debian 13 host): the
  public domain-create route itself refuses on a host with no active engine
  (`409 DNS_SERVER_REQUIRED`, "choose and activate BIND or PowerDNS first"),
  so "add a domain, then set up DNS" cannot happen through the panel on a
  fresh install. The deadlock is real for hosts whose zones predate the first
  engine - upgraded installs, imports, restores - which is the shape S-8's T1
  seeded directly. The fix stands; the sentence above overstated who reaches
  it.
  Note that this is not the R-018 proof: the inherited-anchor walk on Arch was
  never reached, so R-018 stays unproven on a real Arch `/`.
- Fix on branch (2 September 2026): for the `fresh` staging kind (no engine has
  ever run, neither engine running) the gate checks only publications in
  flight - zone rows carrying a live publication lease or rows in
  `dns_zone_engine_leases` - and no longer counts zones that are pending
  because nothing could apply them. Adoption kinds keep the stricter gate. The
  staging transaction receives the kind and applies the same rule. Tests cover
  the fresh path through to a first-install preview with zero blockers, the
  in-flight refusal, and the unchanged adoption refusal; the fresh test is red
  on the unfixed tree with the S-7 status and code.
- Second layer, same defect: the engine-switch preview added the blocker
  `pending_zone_sync` whenever pending zones existed and the action was not
  an adoption, so after staging the T1 harness would have been refused at
  preview with `blockers == [pending_zone_sync]`. The manifest already
  publishes every zone at its desired generation and the commit marks them
  applied, so on a source-less first install the blocker protected nothing.
  It now applies only when a source engine is active, where pending means
  the source has not caught up. The fresh test walks staging through to a
  first-install preview with zero blockers.
- Third layer, found live on Arch and reproduced on Debian (3 September
  2026): with a pre-existing zone and no engine, the snapshot asked the agent
  for the zone's DNSSEC status; the agent answers "unavailable because
  PowerDNS is not the active engine" whenever PowerDNS is not active, the
  presentation became `degraded`, and the preview refused with
  `dnssec_unsupported`, `target_unavailable` and `source_degraded` at once -
  the exact trio S-8's T1 observed and could not explain. A host with no
  PowerDNS installed is no longer probed; an installed, not-yet-adopted
  PowerDNS still is. After this fix the Arch preview passed with zero
  blockers on the live guest.
- Exit criteria: T1 re-run on current Arch reaches the BIND switch commit, and
  the reboot postcondition holds; that run is also the first live R-018 proof.
- S-8 (3 September 2026): identity staging returned 200 on a fresh Arch
  guest with a pending zone - the first layer is proven live. The preview then
  reported `dnssec_unsupported`, `target_unavailable` and `source_degraded`,
  which is the shape the panel produces when the agent's DNS backend
  readiness RPC fails; the raw body and the Arch agent journal were not
  retained, so the cause is unknown and the second layer is unproven. The
  next run must capture the agent journal at preview time.
- Screen side (3 September 2026): the add-domain dialog and the empty domain
  list told a fresh host to "choose and activate BIND or PowerDNS in Settings"
  and then sent it to the Services page, which refuses to install a DNS
  engine (`DNS_ENGINE_WORKFLOW_REQUIRED`). Both now open the DNS
  infrastructure section, and both say which half is missing: no engine
  ("Choose a DNS engine") or an engine without staged identity ("Configure
  the DNS pair"), instead of one sentence for both.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-030 - A status probe cannot see an agent whose ledger never came up

- Evidence: S-7, T5 attempt 3 (same manifest). After the target-started
  SIGKILL the ordinary agent started, its socket accepted a connection, and the
  first `Agent.ServiceMutationStatus` call returned an RPC error; the harness
  probe printed only `agent status RPC failed` and discarded the error, and the
  post-failure diagnostic captured only the following boot's journal, so the
  ordered failure's cause is not in the evidence. The following boot is
  explained: the fixture's mutation lock lived under `/run/celikpanel-s7-t5`,
  which nothing recreates after a reboot, and the agent logged
  `DEGRADED service-mutations: ... lstat /run/celikpanel-s7-t5: no such file or
  directory` and served. Production is not exposed to that trap: the shipped
  unit declares `RuntimeDirectory=celikpanel` and the lock lives beneath it.
  What the product did wrong in both boots is the same: `ServiceMutationStatus`
  returned the cached bring-up error instead of answering, so the only
  read-only view of the agent's state was an opaque failure.
- Impact: any caller - the panel, an operator probe, an acceptance harness -
  gets "RPC failed" from an agent that is alive and refusing by design, and
  cannot distinguish it from a crash. Every mutation is already refused in
  that state; the defect is only in what the status call says.
- Fix on branch (2 September 2026): `ServiceMutationStatus` answers with the
  hold code when the manager is unavailable (`ledger_unavailable`) or came up
  poisoned (`ledger_ambiguous`, with its job when there is one); host-busy is
  still reported as busy. No internal text crosses the socket. Tests cover all
  three shapes.
- Residual, recorded not fixed: a non-transient bring-up failure is cached for
  the life of the process, so the DEGRADED state clears only on restart even
  after its cause is repaired; whether startup recovery should be retried on a
  later RPC is a design decision for the ledger, not a patch.
- Exit criteria: T5 re-run with a probe that prints the RPC error or the hold,
  the ordered run's agent journal captured beside it, and the required next
  switch accepted.
- S-8 (3 September 2026): proven live - the first probe after ordinary
  recovery returned structured JSON with `mutation_hold: ledger_ambiguous`
  and the frozen job, which is what made R-031 diagnosable.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-031 - Rollback re-enables the BIND alias before the unit it aliases

- Evidence: S-8, T5 attempt 2 (manifest SHA-256 `746228bdc2c01ecda8fbb65067ddb29a0dea9e740694ce461ecff1c50b11c568`). After the deliberate
  target-started SIGKILL the ordinary agent restarted and its boot-0 journal
  reads: `recover DNS engine switch host transaction: systemctl enable
  bind9.service did not reach the required state; command: exit status 1;
  output: Failed to enable unit: Unit bind9.service does not exist; readback:
  load=not-found active=inactive unit-file=`. The first status probe answered
  (R-030 held) with `mutation_hold: ledger_ambiguous` and the job still
  `running`/`leased`. The rollback restores the source units from
  `journal.SourceUnitsBefore`, which `dnsUnitStateMapSnapshots` writes sorted
  by name; `bind9.service` sorts before `named.service`. On APT hosts
  bind9.service is only an `Alias=` of named.service - the symlink exists
  while named.service is enabled and `systemctl disable named.service`
  removes it - so enabling the alias before the unit cannot succeed.
- Impact: every rolled-back BIND-to-PowerDNS switch on Debian and Ubuntu ends
  with no engine serving and a poisoned ledger. This is the failure R-026's
  live proof ran into; the R-026 database cleanup itself worked (exact
  preimage, `integrity_check` ok, no sidecars).
- Fix on branch (3 September 2026): the restore orders snapshots so that the
  alias comes after the unit it aliases; enabling the alias afterwards is an
  exact no-op readback. A test with a Debian-faithful fake systemd (alias
  exists only while named.service is enabled) reproduces the S-8 failure on
  the unfixed tree and passes with the fix in both journal orders.
- Exit criteria: T5 on a real Debian 13 host - rollback leaves BIND active and
  enabled, the job ends failed/interrupted with a clean code, and the next
  switch is accepted.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-032 - A former engine's stranded ownership receipt poisons the switch back

- Evidence: S-8, Boston attempt 3 (manifest SHA-256
  `746228bdc2c01ecda8fbb65067ddb29a0dea9e740694ce461ecff1c50b11c568`) and
  the team's S-9 preflight. The setup chain empty -> BIND epoch 1 -> PowerDNS
  epoch 2 -> purge inactive BIND completed with rc 0 and left "historical BIND
  ownership retained": `dns-engine-ownership-bind.json` from epoch 1 stays on
  disk because, by design, nothing retires ownership receipts
  (`supersededDNSEngineOwnership` documents exactly this). A production BIND
  switch killed at the pre-intent window on that host then meets the
  journal-free provenance check with an install receipt AND a target
  ownership receipt present, which R-019's relaxation deliberately kept
  fail-closed as "the Boston shape". The result is the R-019 wedge again:
  `ledger_ambiguous`, job `running`/`leased`, recomputed from durable state on
  every boot.
- Impact: any host that ever ran engine A, moved to engine B, and has a switch
  back to A interrupted after package install is wedged until someone deletes
  a JSON file over SSH. That is an ordinary operator path (try PowerDNS, go
  back to BIND), not an attack shape.
- Fix on branch (3 September 2026): the epoch tells the two cases apart by
  construction. A target ownership receipt OLDER than the active state is
  history and is treated like an absent receipt (clean failure, retry
  converges); a receipt at the same or a newer epoch is a receipt from ahead of
  the committed state and still fails closed. Linux tests stage the
  BIND(1) -> PowerDNS(2) -> BIND host and prove: older receipt recoverable,
  equal and newer receipts refused; the recoverable case fails on the unfixed
  tree with `journal-free DNS engine target retains transitional install
  ownership`.
- Consequence for the Boston negative: the cell as ordered in S-7/S-8/S-9
  (plant the epoch-1 receipt) would now pass through as residue, which is
  correct. The safety-critical negative must plant a receipt at the target's
  epoch (state epoch + 1); the positive half must show the historical receipt
  converging. S-9 addendum 2 redefines the cell accordingly.
- Exit criteria: on a real Debian 13 host, BIND -> PowerDNS -> (BIND switch
  killed pre-intent) recovers with a clean failed job and a converging retry;
  the same host with a planted target-epoch receipt fails closed with
  `ledger_ambiguous`.
- S-9 Boston (3 September 2026): four rehearsal attempts, none reached the
  product; the driver failed in its own bootstrap (a 6-vs-5 unpack), on stale
  agent-socket handling, and finally in a cell-tuple guard that rejects the
  historical and foreign modes addendum 2 introduced. No live proof of the
  historical-ownership fix exists yet; the foreign-receipt negative has never
  been attempted. The harness is repaired next and both modes run.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-033 - A failed first install poisons a fresh host

- Evidence: live, 3 September 2026, on a fresh Arch guest (root `/` 0555)
  driven through the public API with the `0eaf3a5…` candidate plus the R-029
  third-layer panel fix. Preview passed with zero blockers; the commit
  returned `503 DNS_ENGINE_MUTATIONS_HELD` with `ledger_ambiguous` in 4 s.
  The agent journal: `DNS engine switch failure could not prove a pre-commit
  abort: BIND directory is unsafe: /var/named`, then `reprove DNS engine
  switch abort: finalized DNS engine provenance is inconsistent without active
  state`, then `service mutation manager is fail-closed after an ambiguous
  ledger write`. The host held `dns-engine-install-ownership-bind.json` and
  nothing else. `exactFinalizedDNSEngineSwitchProvenanceOnHost` treated any
  receipt without an active state as a contradiction, including the install
  receipt that every failed-after-install first switch leaves behind.
- Impact: on any distribution, the very first DNS engine install that fails
  after package install wedges the host: every mutation refused,
  `ledger_ambiguous` recomputed from the same receipt on every boot, no way out
  short of deleting a file over SSH. The R-019 wedge on a fresh host's first
  DNS action. Debian never showed it only because its first install never
  failed.
- Fix on branch (3 September 2026): an ownership receipt without state is
  still a contradiction; an install receipt alone is residue - nothing ever
  served - and recovery fails the job cleanly so the retry adopts the installed
  packages. Linux tests cover both; the residue case is red on the unfixed tree
  with the exact "inconsistent without active state" line.
- Exit criteria: on the Arch guest, restarting the fixed agent clears the hold
  and fails the job cleanly; the retried switch proceeds to the R-018 walk.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-034 - A WireGuard apply can never succeed, and its failure wedges the host

- Evidence: live, 3 September 2026, R-003 drill host A (Debian 13 guest,
  branch 68b83cc) driven through the public API. `POST /api/v1/service/install
  {"service_id":"wireguard"}` installed the packages, then the job failed in
  `syncing` with `VPN peer sync rollback could not prove the previous host
  state: wg-quick strip failed`. Reproduced by hand: `wg-quick strip` on the
  canonical `wg0.conf` path exits 0; on the staged temp name
  `.wg0.conf.tmp-XXXXXXXXX.conf` it exits 1 with `The config file must be a
  valid interface name, followed by .conf` (the basename exceeds the 15
  character interface-name limit). After the failure
  `/api/v1/host-mutation-readiness` returned `HOST_MUTATION_BUSY`, the
  firewall apply returned `409 service_operation_busy`, and an agent restart
  re-hit the same strip during startup reconcile.
- Impact: VPN cannot be enabled on any host, and the first attempt blocks
  every other mutation on that host until someone edits files over SSH. The
  R-019 wedge reached from the VPN path.
- Fix (in progress on the branch): `wg-quick` only ever sees a file whose
  basename is `wg0.conf`; the staged validation copy lives under a private
  directory instead of carrying the random suffix in its name. Recovery of an
  already-poisoned host: the poison is an in-memory field of the mutation
  manager, never persisted; a restart rebuilds the manager, replays the
  persisted VPN job through the same apply, and with the fixed strip that
  replay now finishes instead of re-poisoning. One agent restart is still
  required on a host poisoned before the fix.
- Exit criteria: VPN install and one peer apply succeed through the API on a
  fresh guest; the poisoned-host recovery answer is recorded here.
- Fixed on branch (3 September 2026): `wg-quick strip` runs on a private
  0700 copy named `wg0.conf`; the durable stage keeps its name, its atomic
  rename and its recovery discovery. The linux fake `wg-quick` now enforces
  the real basename rule, so the whole VPN suite is a regression guard on the
  exec path, and it passes on a Debian guest as root, including the commit
  rollback poison test. Live proof on a fresh guest is still owed.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-035 - No sshd, no firewall

- Evidence: live, 3 September 2026, drill host A. `POST /api/v1/firewall
  {"enabled":true}` returned `409 SSH listener discovery failed; firewall was
  not changed: no verified listening sshd port was found`. The guest has only
  `openssh-client`; the managed-service catalog has no ssh entry, so the panel
  cannot install one, and `/etc/celikpanel/firewall.nft` never exists.
- Impact: the escape-hatch proof is right for a real server, but a host
  without sshd (containers, some VPS images, every WSL guest) can never enable
  the firewall, and the screen only says discovery failed.
- Decision needed: either an explicit operator acknowledgement path ("this
  host has no SSH; enable anyway") or a plain refusal that says the host is
  unsupported for the firewall, recorded in DECISIONS.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-036 - The mail profile needs a fully qualified hostname nobody can set

- Evidence: live, 3 September 2026, drill host A. `POST
  /api/v1/service/profile/install {"profile_id":"core-mail"}` accepted (202)
  and failed in `profile/core-mail/preflight` with
  `mail_profile_server_hostname_invalid`; the guest's hostname is a bare
  machine name. There is no hostname endpoint and the DNS identity settings do
  not feed the mail hostname.
- Impact: on any host whose OS hostname is not an FQDN the mail stack cannot
  be installed, and the operator is told the hostname is invalid without a way
  to fix it from the panel. DKIM keys are unaffected (they are generated in
  Go through `/domains/{id}/mail/auth/dkim`).
- Decision needed: derive the mail hostname from the panel's own identity
  (nameserver/host settings) or add a hostname setting; not silently.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-037 - A local build can refuse a released panel's database

- Evidence: 3 September 2026, during the R-003 drill. A panel built straight
  from this Windows checkout refused the restored database with `migration
  integrity mismatch for version 1: ledger has .../ff31f0b6..., embedded
  release has .../0a01e17f...`. Cause: 102 tracked files, the migrations
  among them, were checked out before `.gitattributes` pinned `eol=lf` and
  kept their CRLF bytes, while git tracks LF and released binaries embed
  what git tracks. The repository content was never wrong.
- Impact: any developer whose checkout predates the attributes file builds a
  panel that cannot read a production database, and the failure names a
  migration hash rather than the real cause. `gofmt -l` also reports those
  files forever, which trains everyone to ignore it.
- Repaired (3 September 2026): the working copy was re-checked out; nothing
  in the repository changed. Five files remain committed with CRLF
  (`LICENSE`, `NOTICE`, two `.gitignore`, one nginx template) and are left
  as they are.
- Guard pending: the release job, or a cheap contract test, should fail when
  an embedded migration's bytes differ from the tracked bytes, so this can
  never be diagnosed as a database problem again.
- Guarded on branch (3 September 2026): two tests walk the real embedded
  content and refuse a carriage return, naming the file, the offset and the
  repair; they run wherever `go test ./...` runs, including on the machine
  that can produce the defect, which a Linux-only CI check never could. The
  runtime mismatch message now carries the same diagnosis when it fires. The
  repair it prints is verified: `git checkout --` and `git checkout-index -f`
  are both no-ops here because git believes the file is unmodified, so the
  index entry has to be dropped first.
- The guard found a second, live case on its first run: `.gitattributes`
  pinned `*.sql` but never `*.tmpl`, so **every** Windows checkout - not only
  ones predating the file - embedded the nginx vhost template with CRLF and
  wrote vhost files that differ byte for byte from a released panel's. The
  attribute is added and the working copy renormalized; tracked bytes never
  changed. Those two embeds are the only `go:embed` sites in the product.
- Note until this branch merges: a fresh Windows clone of `main` still gets a
  CRLF template and the new test fails there. That is the guard working, not
  a new defect.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-038 - A pre-installed DNS engine could never be activated

- Evidence: the S-9 T5 cell, 3 September 2026, and the code path behind it.
  Each switch builds the set of missing packages and branches on it. The
  install branch writes an install-ownership receipt before installing; the
  skip branch called a handoff helper written for R-028/R-029 that rebinds an
  existing receipt and returned success without writing anything when there
  was none (`cmd/agent/dns_engine_ownership.go`). Finalization then refused
  with `committed DNS engine switch has no exact install or active ownership
  provenance`, and the mutation manager failed closed. The receipt could not
  even be expressed: the constructor and the validator both required a
  non-empty missing set.
- Impact: any host that already carries the target's packages - a provider
  image with bind9, a rebuilt host, an operator who installed a package by
  hand, a rolled-back attempt - can never activate that engine through the
  panel, and the failure wedges the host's mutations rather than explaining
  itself. This is the first-install path on a very ordinary rented server,
  not an edge case; the acceptance campaign found it only because its
  fixture preinstalled the packages.
- Fix on branch (3 September 2026): adopting already-present packages is
  recorded as provenance by the same constructor and receipt as an install,
  with this mutation's manifest qualifier, request id and owner id; the kind
  is derived from the missing set and validation refuses the two disagreeing
  in either direction. No finalization rule was relaxed: a foreign or stale
  receipt is still refused and a missing receipt is still unacceptable. An
  install receipt still encodes to the bytes a released agent wrote.
- Exit criteria: on a real VM whose target packages are preinstalled, the
  first switch to that engine finalizes through the panel and the host stays
  mutable; the rolled-back retry from the T5 cell converges.
- Live proof, 3 September 2026, Debian 13 guest reset to a fresh host with
  `bind9 1:9.20.26-1~deb13u1` preinstalled by hand, run twice: once on the
  commit before the adoption fix and once on the branch head. **The two runs
  are byte-identical** and neither reaches the agent. With `named` as apt
  leaves it (running), identity staging is refused outright with
  `409 DNS_ENGINE_WORKFLOW_REQUIRED`. With `named` stopped and disabled and
  the packages still present, identity stages, then the preview returns
  `action: switch` with the blockers `target_unavailable` and
  `unmanaged_dns_detected`, and the commit answers `400 invalid DNS engine
  switch request`. The agent journal shows no switch, no finalization and no
  hold; no receipt is ever written, because no mutation is ever dispatched.
  The host stays mutable, so this is a clean dead end rather than a wedge.
- Why: for APT BIND the panel's readiness package list and the agent's
  provenance package list are the same single list, so "the panel says
  installed" and "the agent's missing set is empty" are one condition, and
  the panel refuses exactly that condition. `cmd/panel/dns_engine.go` raises
  `unmanaged_dns_detected` whenever the target is installed and not managed,
  regardless of whether it is running, and `Managed` can never be true on a
  host the panel has never owned; the action stays `switch` instead of
  `install` because the target is installed. `cmd/panel/dns_setup.go`
  additionally blocks identity staging while any engine is running.
- There is no escape hatch: `/dns/engine/reconcile` answers
  `{"reconciled":false}`, and both `/service/install` and `/service/uninstall`
  for BIND answer `409 DNS_ENGINE_WORKFLOW_REQUIRED` pointing back at the DNS
  infrastructure screen. The only way out is to purge the package over SSH,
  which the product forbids as a matter of policy.
- The agent-side fix (adoption provenance) is real and stays: it is the
  second half of the answer, at a layer the panel currently never reaches.
- What the product needs, decided 3 September 2026: an explicit, informed
  takeover. When the target engine is installed but unmanaged and no engine
  is active, the preview must offer `adopt_unmanaged` instead of refusing,
  telling the operator in plain words that this server already runs a DNS
  server the panel did not install, that adopting it replaces its
  configuration with the panel's, and that anything it serves today which the
  panel does not know about will stop being served. The commit proceeds only
  with that acknowledgement, snapshots what it is replacing so a rollback
  restores it exactly, installs nothing, and lands on the agent's adoption
  provenance. Identity staging must be allowed while an unmanaged engine is
  running, since it only writes settings.
- Exit criteria: on a fresh host carrying preinstalled BIND packages, in both
  the running and the stopped shape, the operator activates BIND through the
  panel alone, the zone answers, and a rollback restores the configuration the
  host had before. Proven on a real VM.
- Fixed and proven live for the stopped shape (3 September 2026, Debian 13
  guest, bind9 1:9.20.26 preinstalled, named stopped and disabled): the
  preview returns `adopt_unmanaged` with no blockers and its own
  acknowledgement; the commit is refused without it (`400 adoption
  acknowledgement is required`, nothing changed) and accepted with it in 10 s;
  BIND ends active and owning port 53 at epoch 1, state ready, the host still
  mutable, the durable ownership receipt naming that mutation, the package
  manager log showing nothing was installed, and a new domain's zone
  answering its SOA authoritatively. The takeover reuses the first-install
  transaction unchanged, so the manifest, the snapshot row and the agent
  dispatch are identical to a first install. Identity staging needed no
  change: the stopped shape already qualified as fresh.
- Still owed here: the rollback proof - that a takeover which fails restores
  the configuration the host had before it. The snapshot machinery already
  captures it (it is content-based, so it covers a vendor config the panel
  never wrote), but the restore has not been exercised on this path.
- The running shape is not this entry any more; it is R-039, and a test pins
  the refusal so it cannot be reached by accident.
- Corrected 4 September 2026: the stopped-shape takeover also keeps the
  zones the server already answers. The copy shipped with it said they would
  stop being served, which is a loss that does not happen - the generation is
  additive. A false cost is worse than a stated one: it invites the operator
  to refuse a safe change or to rescue zones that were never at risk. Both
  shapes now say what actually happens, and a test refuses the false claim.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-039 - Adopting a running DNS server needs a durable pre-intent record

- Evidence: 3 September 2026, established from the code while implementing
  R-038's takeover. With the unmanaged engine stopped and disabled the agent
  already accepts the adoption end to end and needs no change. With it
  running, `proveBINDTargetNotServing` has no accepting branch for an active
  unit and the port 53 pre-mutation guard refuses the foreign listener. Both
  are correct and are ring-fenced.
- Why it is not a small change: making the running shape work means the agent
  stops and seals units the panel does not own, and it must do that before
  those proofs run. The earliest durable record today is the switch intent,
  written well after the install and seal step; the install guard keeps its
  before-state in memory only. Today a crash in that window is harmless
  because nothing was serving. During an adoption it would leave the
  operator's DNS stopped and masked with no recovery record - a new
  fail-closed hole, in the code whose whole purpose is not to have one.
- What it needs: a durable pre-intent journal phase with its own recovery
  handling, and the authority to seal an unmanaged engine bound into the
  mutation manifest as its own mode, so it cannot be spent by any other
  operation. That reaches roughly a dozen mode checks in the agent and the
  panel.
- Until then: the product tells the operator plainly that the DNS server
  running on this host must be stopped before it can be adopted, instead of
  refusing without saying why. R-038 covers the stopped shape.
- Design corrected, 4 September 2026. The earlier analysis assumed the agent
  would have to stop and seal an engine it does not own before its proofs
  could run, which would need a durable pre-intent record it does not have.
  The product already contains the better answer: PowerDNS has an adoption
  path (`adoptPDNS`, `cmd/agent/dns_engine_pdns_adopt.go`) that never stops
  or starts the unit. It writes its intent journal first, captures the
  existing configuration, proves at mutation time that the configuration is
  still exactly what it captured, and only then replaces it in place. There
  is no window in which the host is not serving, so there is nothing for a
  pre-intent record to recover.
- What R-039 is, therefore: the same shape for BIND, against a configuration
  the panel did not write. Adoption has its own evidence path, distinct from
  the switch proofs - so `proveBINDTargetNotServing` and the port 53
  pre-mutation guard are not relaxed and not reached; they are the wrong
  proofs for an operation that is not a switch. The snapshot machinery is
  content-based and already captures a vendor configuration, which the
  stopped-shape takeover proved on 3 September.
- What still has to be decided in the implementation: whether the running
  engine is reloaded or restarted after its configuration is replaced
  (reload keeps the host answering throughout and is preferred if the engine
  supports it for the change being made), and what the product does when the
  foreign configuration turns out to reference zone files or includes that
  the panel's generation will orphan - refuse and name them, or take them
  under the same acknowledgement.
- The acknowledgement, the plain-language warning and the panel gate are the
  ones R-038 already ships; this entry extends them to `Running == true`
  rather than inventing a second dialogue.
- Exit criteria unchanged: on a fresh host carrying a running, unmanaged
  BIND, the operator adopts it through the panel alone, the zone answers
  throughout, and a rollback restores the configuration the host had before.
  Proven on a real VM.
- Fixed and proven live, 4 September 2026, on a Debian 13 guest carrying a
  hand-installed BIND that was answering a zone of its own. Through the panel
  only: identity staged while that server was running (a refusal before this
  change), preview `adopt_unmanaged` with no blockers and the running
  impacts, the commit refused without its acknowledgement and accepted with
  it in 12.3 s, BIND active and managed at epoch 1. **Measured: 2508 queries
  of the server's own zone across the whole commit, none unanswered**, and
  the unit's main process id unchanged - the design's claim that the host
  never stops answering is measured, not asserted.
- Reload, not restart: everything the panel's generation writes is re-read on
  reload, and the reload proves it by requiring the same main process id
  afterwards, or the adoption fails and restores.
- The foreign zones are kept, not dropped, and this corrected what shipped
  the day before: the generation adds an options block and an include and
  deletes no zone declaration, so nothing is orphaned. The only case that
  cannot survive is a name collision with a zone the panel publishes; it is
  named before anything is touched, and one hidden behind an include is
  caught by the config check before the reload, costing a restore and no
  outage. Both fired live.
- Rollback proven: a forced failure restored both configuration files at
  their exact pre-adoption digests with the server still answering.
- The switch proofs were neither called nor relaxed. Two other proofs gained
  an adoption branch rather than a relaxation: the empty-source rule, and the
  panel's post-failure runtime check, which now accepts nothing running or
  only an unmanaged target running, and still refuses a target that came back
  managed.
- Owed: the same run on a real VM.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-040 - "I have never looked" is reported as "it is not installed"

- Evidence: 3 September 2026, live on the R-038 guest. For the same host at
  the same moment `GET /api/v1/dns/engine` reported bind `installed: true`
  while `GET /api/v1/managed-services` reported `is_installed: false`,
  `status: "not_installed"`, `scanned_at: null`. Root cause: the managed
  services handler never probes; it reads the component scan cache, and when
  there is no row it still ships the catalogue built from an empty
  observation, so every service serialises as not installed. The DNS engine
  surface probes the host live on every request.
- Impact: the screen a person opens to see what is on their server states as
  fact something the product has never checked, and it contradicts another
  screen in the same session. On a restored or freshly installed host, where
  there is no scan yet, everything reads as absent.
- A second, real divergence to settle with it: even with a fresh scan the two
  surfaces ask different questions - the component scan decides from systemd
  units, the DNS engine surface from the package database. A masked engine,
  which the DNS workflow deliberately creates, can legitimately read
  differently on the two.
- Fix: the wire must distinguish "not observed" from "not installed", and the
  screen must say "not checked yet" and offer the check. The existing test
  that asserts the catalogue is served with a null scan time currently
  blesses the defect and has to change with it.
- Fixed (4 September 2026, PR #80): `is_installed` is `true | false | null`
  with `status: "unknown"` beside it, and unobserved rows withhold the
  conflict and requirement claims they were reading off an installed set that
  is unknown rather than empty. A component added to the catalogue after the
  last scan reads unknown too - the same defect, more quietly. `null` is the
  only shape an un-updated browser cannot misread: its decoder requires a
  boolean, so it refuses the payload and falls into the existing fail-closed
  state instead of rendering a fabricated inventory.
- The screens follow: the list shows one neutral notice and no per-row
  actions that would guess; the dashboard and sidebar show no count at all
  for an unobserved host, and count only what is known when the host is
  partly observed; the per-service page has three answers instead of two and
  offers the check rather than an install, which was the worst instance -
  a wrong action rather than a wrong number. Opening a page does not probe
  the host.
- Two quieter folds went with it: "running" was read off a status of
  "unknown" and reported stopped, and the detail panels drew missing units,
  versions and configuration files as facts about a component nobody had
  looked at.
- The two surfaces still answer different questions on purpose. The component
  scan decides from systemd units and that same function feeds firewall
  policy, so widening it to accept package presence would open ports on a
  host whose units the panel cannot see. The payload now names which question
  it answered instead, and the label is the agent's own branch selector, so
  it cannot claim a probe that did not run.
- Owed: a browser round on a real panel. No dev guest was available the day
  this landed, so the proof is types, tests and bundle budgets, not a screen
  someone looked at.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-041 - The browser cannot decode an action the API already returns

- Evidence: 3 September 2026. The panel returns the `reinstall_active`
  preview action shipped the same day, and the web contract's action list
  does not include it, so the preview decodes to null and the dialog reports
  an invalid preview. The action was proven live through the API, not through
  the browser, which is exactly how this survived.
- Impact: the one path a restored host has to bring its DNS server back is
  unreachable for anyone using the panel the way a customer does.
- Fix: decode the shipped action, and pin the action set with a test that
  fails when the API can return something the browser cannot render, so this
  class cannot recur.
- Fixed (3 September 2026): the preview action list is one exported list the
  type derives from, so the union and the runtime check cannot drift, and a
  test parses the panel's own action function and fails the build when the
  API can return an action the browser cannot decode. Listing the action was
  not enough on its own: the decoder also tied the acknowledgement to the
  presence of a source, which a reinstall has, so it would still have failed.
- The lesson, recorded because it will recur: an API proof is not a product
  proof. The reinstall was exercised through the API the same morning and
  reached the browser as an invalid preview.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-042 - The takeover refuses the server it was built for

- Evidence: 4 September 2026, derived from `managedBINDOptions` while proving
  R-039, not exercised live - the guest was staged with the distribution's
  stock options so the main proof would run.
- What happens: the panel's BIND generation refuses an `options {}` block that
  already defines `recursion`, `allow-recursion`, `allow-query-cache` or
  `allow-transfer` outside CelikPanel's own markers. The refusal is right in
  principle: the product will not silently overwrite a directive the operator
  set. But an authoritative BIND configured by hand almost always carries
  `recursion no;`, which is exactly the server the takeover exists to adopt.
  So the common real host is refused, with a message about ownership markers
  that says nothing about what to do.
- Impact: R-038 and R-039 close the takeover for a server whose options block
  is stock, and leave it closed for one an administrator has actually
  configured. That is the wrong half of the population.
- What it needs: the takeover reads those directives as part of what it is
  replacing, shows the operator the values it found and the values it will
  set, and takes them under the same acknowledgement - the snapshot already
  makes the rollback exact. A refusal, if any survives, must name the
  directive and the file and say what to do.
- Exit criteria: a host carrying an authoritative BIND with `recursion no;`
  and its own zones is adopted through the panel, its zones keep answering,
  and a rollback restores its options block exactly.
- Fixed 4 September 2026: consent replaces the refusal, and it is shown
  before it is asked for. The preview reports each managed directive the
  server already sets - the value found, its file and line, and the value
  CelikPanel will set, marked unchanged when they are the same so the screen
  never counts a change that is not one - inside the takeover panel above the
  acknowledgement that already exists. No second consent: this is part of the
  same act. The value the screen promises and the value the file gets come
  from one function, so they cannot drift, and a contract test parses the
  agent's own directive list to fail the build on drift.
- Only consent moved. The takeover authority is derived from the fact the
  panel used - no durable CelikPanel authority over BIND - read before any
  receipt of ours exists; every other path keeps the exclusive rule. Recovery
  of a failed takeover prepares under the same authority, because otherwise
  it would refuse itself and wedge a host whose DNS is still answering.
  Surviving refusals name the directive, its value, the file, the line and
  where to go; three constructs genuinely cannot be adopted and say so.
- Proven live on a hand-written authoritative configuration (`recursion no`,
  an `allow-transfer` to a peer, its own zone): the list named recursion
  unchanged and allow-transfer a change, the commit was refused without the
  acknowledgement and accepted with it in 11.3 s, and **1137 queries of the
  operator's own zone went unanswered zero times**. A forced failure restored
  both files at their exact digests with 1155 more queries unanswered zero
  times; the retry committed in 9.4 s.
- Owed: the same run on a real VM.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-043 - The recovery from a crashed takeover stops the server the takeover protects

- Evidence: 4 September 2026, found while proving R-042, from the code, not
  exercised live. The in-process rollback of a running takeover is correct
  and was measured: a forced failure restored both files byte for byte with
  the server answering throughout. But a crash leaves the journal to be
  recovered at agent start, and `rollbackDNSSwitchJournal`'s BIND branch is
  the *switch* rollback, which stops units - not the adoption rollback, which
  does not.
- Impact: the takeover's whole promise is that the host never stops
  answering. A machine that loses power mid-takeover would come back with the
  operator's DNS stopped, which is worse than the failure it is recovering
  from, and on a host the panel does not yet own.
- What it needs: the recovery path must dispatch on the journal's mode, as
  the mutation paths already do, and a running takeover's journal must
  recover through the adoption rollback. The journal already carries the mode.
- Exit criteria: a takeover killed between its config write and its reload is
  recovered at agent start with the server still answering, proven by the
  same query-loop measurement the in-process rollback used.
- Fixed 4 September 2026. The recovery classifies the journal before it acts.
  The takeover's wire mode is a switch, so the durable fact that separates the
  shapes is the target unit preimage the journal froze: a first install and
  the stopped half begin with the unit inactive, a running takeover begins
  with it active. A preimage that cannot be read fails closed naming what it
  found. One path serves every crash point: the running server is re-proved
  freshly, since a crash gives it a new process id; the configuration on disk
  must be either what the takeover found or what it wrote; then the existing
  adoption rollback restores, reloads and verifies, stopping nothing.
- The panel held the mirror of the same defect, and it wedged live: closing a
  rolled-back switch demanded a sealed, not-serving target, which a
  takeover's rollback never leaves. It now accepts the sealed shape or the
  restored takeover, proved as its own mirror, and returns both refusals when
  neither holds.
- Proven live at two crash points, the agent killed at an exact injected
  boundary using the product's own kill-matrix facility: the configuration
  restored to the digests the host had before, the ledger released, the host
  adoptable again, and **1335, 1429 and 1321 queries of the operator's own
  zone unanswered zero times** across the crashes and the recoveries. The
  server's main process id never changed and it was never restarted.
- Ten recovery sites were audited; two needed the change, one is no longer
  reached for a takeover, and the rest already funnel through the fixed
  dispatch. Owed: the same run on a real VM.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-044 - A server configured with views is adopted without understanding it

- Evidence: 4 September 2026, found while proving R-042, from the code and
  BIND's own rules; the guest was configured without views so the main proof
  would run.
- What happens: the takeover reads and replaces directives in the `options {}`
  block. BIND allows the same directives inside a `view`, where they take
  precedence, so a `recursion yes;` in a view would silently survive a
  takeover that reported recursion as managed. Separately, BIND requires every
  zone to live inside a view once any view exists, so the panel's generated
  zones fail late, in the configuration check - a restore and no outage, but a
  refusal that arrives after the work rather than before it.
- Impact: on a host with views the panel would either report a setting it does
  not actually control, or refuse at the last moment without having said the
  server was unsupported.
- What it needs: detect views before the preview, and either refuse by name
  with what the operator can do, or place the panel's zones and directives
  inside the view that answers for them. The first is honest and small; the
  second is the real feature and belongs with the zone-placement work.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

### R-045 - The one crash point that is neither finished nor undone

- Evidence: 4 September 2026, measured five times while proving R-043, on the
  drill guest with the product's own fault injection. Crashing a running
  takeover at the `target-verified` boundary - the work done, the finalization
  not yet written - leaves the recovery logging `verified DNS engine target no
  longer matches its journal: file does not exist` and poisoning the ledger.
- What holds at that boundary, and matters: the recovery still never stopped
  the server. Its main process id was unchanged, it was never restarted, and
  1460 queries of the operator's own zone went unanswered zero times. The
  refusal also names what it found rather than guessing. What does not hold:
  the ledger is held, and the takeover is neither finalized nor rolled back,
  so the operator needs a hand to get out.
- Cause, as far as it was traced: the generation pointer the recovery reads is
  gone by then. A millisecond watch put its creation at 14:27:03.855, the
  reload at 14:27:05, the boundary marker at 14:27:06.958 and the pointer's
  removal at 14:27:06.966 - eight milliseconds after the marker, by the agent
  itself while it was being stopped, before the kill. A normal takeover creates
  that pointer and keeps it. Whether the injection perturbs the run (at that
  boundary the process is still alive and its own failure path can execute,
  which a real power loss could not do) or whether this is genuine product
  behaviour was not determined, and is not guessed at here.
- It is not caused by the R-043 change: nothing in that change runs during a
  takeover's apply; the dispatch, the adoption recovery and the seal proof are
  reachable only from recovery and from the panel's evidence call.
- What it needs: determine which of the two it is, with the injection removed
  from the question - kill the process from outside at the same boundary, or
  cut power to a VM. Then either the recovery finalizes a verified takeover
  from what the journal already holds, or the apply stops removing a pointer
  it still needs.
- Exit criteria: a takeover killed at the verified boundary comes back either
  finalized or rolled back, with the server answering throughout and the
  ledger free.
- Owner / target / evidence: OUT-OF-REPO / ASSIGN.

## Acceptance rule

Risk acceptance is an accountable business decision and belongs in the
external register. An OPEN/BLOCKER risk cannot be silently converted to
accepted by changing wording in this file. Close a risk only with its exit
criteria and dated evidence.
