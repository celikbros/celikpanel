# CelikPanel Roadmap

*Last updated: September 28, 2026 · [Türkçe](ROADMAP.tr.md)*

---

## The Constitution — Every Decision's Filter

Every feature, commit and design decision must satisfy these requirements.
They are obligations, not a claim that the present implementation meets them.
The [resilience contract and source audit](docs/RESILIENCE-CONTRACT.md) records
open P0 work, implementation order and the evidence required to close it (D-025).

### 1. Security and owner authority
- Authenticate management access and authorize each action for its exact resources. Use least privilege, local authenticated IPC, parameterized SQL and `crypto/rand` for secrets.
- The unprivileged Panel and privileged Agent remain separate. Normal privileged automation uses the authorized Agent API; supported owner recovery has a separate narrow contract. Neither an AI planner nor a recovery UI gains unrestricted root execution.
- The server owner retains native service administration. Detect owner changes and reconcile explicitly; never silently overwrite them to match cached intent (D-022).
- When evidence is uncertain, block the affected unsafe mutation. Preserve authenticated diagnostics and supported recovery without granting unverified privileges.

### 2. Continuity and recoverability
- Panel outage, license loss or removal must not stop hosted workloads or their native renewal, scheduling and boot mechanisms. Remaining dependencies must be eliminated and tested before claiming independence.
- Every mutation defines affected resources, read-only preflight, durable checkpoints, bounded retry/recovery and terminal proof. Permission or ownership normalization is also a mutation.
- Recovery must remain usable when the candidate release, ordinary Agent, application migration or license verifier fails. Mixed or unverified application state may block normal management; it must not erase the independent recovery path.
- Preserve the last verified usable state and recovery material until the candidate and its recovery compatibility are verified. Cleanup follows that proof.
- Automatic repair is a deterministic, idempotent continuation or compensation of the accepted operation. It does not invent missing evidence, undo later owner changes, bypass validation or launch a second unknown mutation.

### 3. Truthful state and shared contracts
- Separate owner intent, authority, published configuration, observation, execution, verification and recovery. Unknown is not absent, failed, expired or completed.
- Each durable artifact has one versioned producer/reader/restore contract with explicit supported transitions. Compare evidence according to its role; do not use whole-record equality where legitimate publication advances only part of a record.
- A completed installation step is historical execution evidence, not proof of present health. Report the current reason, responsible actor, next action and how the same operation resumes (D-024).
- The browser observes authoritative operation state. Refresh, reconnect and timeout never authorize duplicate work or imply completion.

### 4. Simplicity
- Give each routine task one clear user path. Share the underlying operation contract across the browser and supported owner recovery; a single screen is not a single point of recovery failure.
- Add features only for a concrete user need. Use safe defaults within the accepted scope and reveal advanced choices when needed.
- Keep unused service-specific navigation quiet. Keep installation discovery, actual conflicts and recovery actions visible when they help the user complete the task.
- Normal operation should be possible through the panel. Native owner administration and recovery remain supported; a manual rescue is evidence of an automation gap, not a reason to prohibit rescue.

### 5. Speed with evidence
- Targets remain API response under 100 ms, responsive interaction and a 60-second minimal installation. Record the measured platform and scope before claiming any target achieved.
- Keep the runtime small. The existing Panel/Agent privilege split, native workload services and an independent recovery mechanism take precedence over a one-binary slogan.
- Speed does not justify skipping validation, recoverable checkpoints or fault testing.

### 6. Flexibility and independence
- Use typed APIs, modular services and standard protocols; optional automation is separate from service operation.
- Backups and exports use standard formats. Management software does not own or hold the owner's data hostage.
- Standard DNS replication does not require a remote panel or its license. Separately authorized remote record management is optional.
- Installed-panel updates are initiated only by the owner in CelikPanel's update UI. Publication, diagnosis and supported rollback do not authorize assistant-side installation.

### The honesty and release rule

Tests, security review and documentation remain necessary. Lifecycle support
also requires complete fault-transition evidence in disposable native environments:
real previous-release state, failed update, actual automatic restoration, recovery
interruption/reboot, preserved owner changes, management recovery and workload
probes. Component tests, mocked service managers and a successful installation
alone do not prove that contract.

Every lifecycle change names its affected invariant, schema/version transition,
recovery behavior and acceptance evidence. Unmeasured or inconclusive results stay
open. A narrowly scoped incident correction may ship with its limits explicit;
it does not close foundational P0 work or justify unrelated feature expansion.
The exit matrix in D-025 must be implemented and passed before claiming resilient
operation. No system is promised to recover autonomously from every possible fault.


---

## Where We Are — September 26, 2026

**Current priority: finish D-025 architectural resilience before expanding the product.**
This review covers the local source through `f253d318` and the retained acceptance
reports. It is not a new inspection of Frankfurt/Boston, a published release, or
proof that these changes are installed. P0.1–P0.5 are all **partial**; none is closed.
The constitutional requirements and their identifiers remain unchanged.

### Evidence and remaining work

| Existing item | Implemented or demonstrated within a stated scope | Still required for closure |
|---|---|---|
| P0.1 — Native update/rollback | Real old-release restoration on Arch/Debian; genuine Alpha64 schema38 data used in later migration and recovery trials. [Evidence](deploy/e2e/release-recovery/ISOLATED-DATABASE.md). | Complete the supported update/fault/workload matrix and production-signed candidate admission; a successful selected checkpoint is not whole-update acceptance. |
| P0.2 — Access and truthful status | Independent authenticated recovery/status entrypoint; selected CLI, HTTP and browser terminal results agree. Native boot-wait retries retain the same operation. [Evidence](deploy/e2e/release-recovery/BOUND-WORKER.md). | Native waiting/error/reconnect cases, preservation of known failures, browser access during waits and the owner-initiated update path with production trust. |
| P0.3 — Independent recovery | Separately retained recovery code/data, isolated DB migration and atomic publication; selected cuts during rollback and reboot recover automatically with cut-time rows preserved. [Two-fault evidence](deploy/e2e/release-recovery/NATIVE-EXCHANGE-RECOVERY.md). | Remaining checkpoints, incomplete capture, metadata transitions, old-version compatibility and safe cleanup. Eventual recovery is not uninterrupted service or power-loss durability. |
| P0.4 — Shared DNS/TLS contracts | DNS acquisition/publication roles separated; shared TLS and DNS readers; independent DNS observation and selected Agent-mediated fault recovery. [Contract](docs/DNS-ENGINE-ARTIFACT.md). | Supported **Agent-independent DNS inverse execution**, native interruption/owner-edit acceptance, complete producer/restore transitions, and peer deletion proof when no authoritative parent is available. Dormant inverse code and read-only observation do not close this gap. |
| P0.5 — Native service independence | Scoped firewall/mail renewal and enrollment recovery; standalone PowerDNS and BIND/BIND pair serving after management-absent or management-disabled reboot, as specified in each report. [Mail evidence](deploy/e2e/release-recovery/MAIL-ENROLLMENT-MANAGEMENT-ABSENT-BE.json); [DNS evidence](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md). | Cross-engine DNS pair combinations, full setup/enrollment and old-application compatibility, and web/DB/mail/cron/renewal/firewall checks under every claimed management-absence/removal mode. Disabled management is not full removal. |

The detailed [acceptance register](docs/RESILIENCE-CONTRACT.md) retains failed
attempts and exact boundaries. New evidence updates an existing P0 item; it does
not create a replacement architecture plan.

### Latest DNS results and their limits

- A [current-source BIND/PowerDNS native trial](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PDNS-CURRENT-SOURCE-SMOKE-20260927.md) verified initial transfer from managed BIND on Debian to panel-free PowerDNS on Arch. SOA, NS and selected A answers matched over UDP/TCP before and after both guests rebooted; the actual Panel and Agent units stayed disabled. The bundle was local and unsigned. Later record changes, the reverse topology, installed servers and update recovery remain untested here; P0.4/P0.5 stay open.
- A [disposable PowerDNS-primary/BIND-secondary native trial](deploy/e2e/dns-kill-matrix/evidence/pdns-master-bind-20260928/contract.json) transferred a `MASTER` member and its catalog from Debian PowerDNS 4.9.17 to panel-free Arch BIND. Both answered the same SOA/A over UDP/TCP. A separate [daemon-transition measurement](deploy/e2e/dns-kill-matrix/evidence/pdns-native-transform-20260927/contract.json) found PowerDNS-generated SOA/serial, `CATALOG-HASH`, WAL/SHM and a different catalog PTR owner. The producer-aware AXFR parser has focused tests, but the product switch, reboot and independent rollback were not exercised; the paired-primary gate remains closed.
- The PowerDNS-primary path in a DNS pair remains blocked in setup and engine preview before an Agent mutation is claimed. The [failed native switch](deploy/e2e/dns-kill-matrix/NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md) found a changed PowerDNS producer catalog; [PowerDNS documents](https://doc.powerdns.com/authoritative/catalog.html) automatic serial advances. V1 cannot prove that live target or safely roll it back. [V3 code](docs/PDNS-PRIMARY-SWITCH-V3-DESIGN.md) now separates the source and native serials, records exact target evidence, and supports a protected prestart owner rollback. An Agent-mediated, same-request poststart forward path now has checkpoint and drift package tests. Owner-approved committed-journal archiving is implemented in source; its durable publication and active-record retirement have focused interruption tests. Product fault/reboot acceptance remains open. V3 evidence also refuses application replacement until an explicit compatible Agent/recovery contract exists; the old evidence-policy marker is insufficient. These new schemas are not shipped or installed. The safety gate stays closed; P0.4/P0.5 remain open.
- A [fresh paired PowerDNS-primary V3 native fault trial](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-native-20260928/README.md) used an uninitialized Debian primary and panel-free Arch BIND secondary. After the `target-enable-intent` process cut, the first separate same-request recovery reached a terminal `succeeded` ledger receipt, archived its V3 journal, and preserved UDP/TCP authority across a management-disabled primary reboot. An earlier run safely retained an unknown result when Debian PowerDNS kept catalog NS RDATA unchanged while normalizing SOA; later host reconciliation preserved that historical failure. The two SSH-launched cuts returned `255`; a third fresh run independently recorded `Result=signal`, `ExecMainStatus=9` (SIGKILL) under systemd and again recovered the exact request on its first attempt. No shell exit `137` is claimed. Existing BIND-to-PowerDNS migration, owner edits, independent inverse, later cuts and full pair lifecycle remain untested; the public gate and P0.4/P0.5 remain open.
- [Fresh PowerDNS V3 prestart inverse](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md) now has one independent, manifest-verified owner-CLI recovery after a real SIGKILL before target start on disposable Debian. Two earlier fresh attempts refused without effects and exposed overly strict masked-unit cgroup and loopback-stub listener checks; both were narrowed with focused regressions. The clean attempt restored the pre-switch inactive/masked PowerDNS state, removed the candidate database and active journal, retained a terminal rollback verdict across a management-disabled reboot, and left the original switch job correctly failed. The native exact-request status trial found a missing volatile lock after reboot; the journal-free terminal-ledger path now has focused tests and a separate disposable Debian reboot check using that same canonical historical ledger, without a producer/inverse replay or installed kit. Later cuts, migration and public admission remain open; P0.4/P0.5 stay partial.
- Separate future engine-migration gap: moving an existing paired managed BIND primary to PowerDNS is not established by the fresh V3 path or standalone V4 adapter. It needs its own paired source/native target proof and recovery trials. This is distinct from accepting a fresh PowerDNS-primary/BIND-secondary setup.
- Immediate two-topology acceptance path: the disposable BIND-primary/PowerDNS-secondary pair has initial authority, terminal edit, owner-enrolled terminal delete/re-add, and management-disabled reboot evidence, but ordinary owner enrollment and fault cells remain open. A [fresh PowerDNS-primary/BIND-secondary V3 trial](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-zone-20260928/README.md) now has a real target-enable-intent SIGKILL with separate same-request recovery, terminal add/edit/delete/re-add for an authoritative child zone, and both native services answering after management-disabled reboots. In that trial, a parentless deletion correctly remained pending without authenticated peer-native absence proof. Public setup/RPC admission, ordinary owner enrollment, later fault cells, owner edits and independent inverse remain open; P0.4/P0.5 and the paired-primary gate are not complete.
- An [owner-operated BIND inspector enrollment trial](deploy/e2e/dns-kill-matrix/evidence/owner-bind-enrollment-20260928/README.md) exercised local primary/secondary enrollment, fixed-command SSH authentication, reboot persistence, explicit revocation and native BIND survival on disposable Debian/Arch guests. The final secondary state is configured, not a proven deletion response. A valid transferred-catalog challenge, product host-key pin, interrupted-install cleanup/rotation, native Debian secondary and packaging remain open; P0.4/P0.5 stay partial.
- A later [owner-enrolled PowerDNS-primary/BIND-secondary parentless deletion trial](deploy/e2e/dns-kill-matrix/evidence/pdns-bind-owner-proof-20260928/README.md) used the pinned product SSH transport and native transferred-catalog/BIND-zone observation. The actual V3 Agent deletion reached a terminal durable receipt; after secondary reboot the catalog was empty and the zone unloaded with management absent. This closes that earlier experiment only. Product packaging/admission, other cuts and independent inverse remain open; P0.4/P0.5 stay partial.
- [Explicit owner enrollment resume](deploy/e2e/dns-kill-matrix/evidence/owner-bind-resume-20260928/README.md) now reuses exact staged BIND inspector files and the restricted account. A disposable Arch trial completed materialized SSH interruption states, repeated a completed enrollment without replacing its key, reauthorized after explicit revocation, and preserved an owner SSH edit while refusing continuation. Native BIND stayed active. This is not a process-kill/power-loss trial; Debian, remaining cuts, cleanup/rotation and product packaging remain open.
- [Debian owner resume and archive integration](deploy/e2e/dns-kill-matrix/evidence/owner-bind-debian-20260928/README.md) passed the same materialized resume checkpoints with the current strict account checks on native Debian BIND/OpenSSH. The ordinary CLI preserved an owner SSH edit and kept the key revoked with BIND active. Source packaging now carries optional owner tools without running enrollment; the real dist recipe passed modes, checksum/tamper and umask reproducibility checks with actual tools and inert unrelated payloads. This closes these Debian checkpoints and source archive integration, not full enrollment, public admission, a signed release or independent inverse.
- The independent fresh-PowerDNS V3 executable checker incorrectly expected `unix.Stat_t` from `os.Lstat`/`File.Stat`, which actually return `syscall.Stat_t` on Linux. A real root-owned-file regression reproduced rejection of valid metadata; the corrected checker now passes the complete recovery package tests and rejects changed content, wrong owner/mode, special permission bits and symlink/hardlink inputs. The root-only temporary-file regression is wired into CI. No schema or recovery decision changed; this is a file-proof correction, not native independent recovery acceptance.
- [Managed BIND V3 deletion](deploy/e2e/dns-kill-matrix/NATIVE-BIND-V3-DELETION-TERMINAL-20260926.md) reached a verified terminal result with a parent-authoritative, panel-free secondary. Native removal survived reboot. Without that parent proof, deletion remains pending; `REFUSED` alone never proves absence.
- [Paired target-staged/after-write](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PAIR-TARGET-STAGED-20260926.md) passed a real SIGKILL, same-request Agent recovery, secondary transfer and subsequent management-disabled reboot. Reboot observations are documented separately from its sealed fault archive.
- [PowerDNS owner-edit refusal](deploy/e2e/dns-kill-matrix/NATIVE-PDNS-OWNER-EDIT-20260925.md) preserved a post-kill owner edit, the journal and native serving. It proves bounded refusal, not concurrent-edit safety at every effect or an independent inverse.
- Standalone managed BIND to initially inactive and disabled PowerDNS now has V4 frozen-candidate and protected owner-CLI rollback source code for pre-activation cuts. The owner command can first record an exact rollback decision for an interrupted intent, target-staged or source-stopped operation; it then replays the same request. Source code also handles an enabled but inactive pre-start target only with a durable V4 enable-intent checkpoint. The producer is not wired, and no native trial or post-start or committed recovery is accepted; see [the exact scope](docs/DNS-ENGINE-ARTIFACT.md#standalone-bind-to-powerdns-v4-recovery-code-scope-2026-09-27). P0.4 remains open.
- The [DNS inventory](deploy/e2e/dns-kill-matrix/README.md) has 510 raw combinations: 268 applicable/runnable and 242 explicit N/A. **268 is not a passed-test count or a ready-fixture count.** Missing managed-BIND and legacy-secondary fixture producers, later paired phases and unexecuted cells remain open. Repeating a measured cell does not increase phase coverage.
- The September 12 cross-engine removal claim was corrected: its negative reply was insufficient evidence. New BIND/BIND success does not retroactively close BIND/PowerDNS acceptance.

### Next work, in dependency order

| Order | Work within the existing plan | Exit evidence |
|---|---|---|
| 1 | P0.4: four bounded Debian protected owner-CLI paths have native evidence (external PowerDNS adoption, PowerDNS-to-BIND rollback, one running-BIND adoption rollback, and one fresh PowerDNS V3 prestart inverse). V4 source code separately covers a narrow pre-activation PowerDNS-target rollback, but its producer is unwired and it has no native trial; post-start/committed recovery, reinstall inverse paths and broader acceptance remain open. Item 1/P0.4 are not complete. | [Running-BIND adoption](deploy/e2e/dns-kill-matrix/NATIVE-BIND-ADOPTION-OWNER-CLI-20260927.md), [PowerDNS-to-BIND rollback](deploy/e2e/dns-kill-matrix/NATIVE-BIND-PROTECTED-OWNER-CLI-20260927.md), [PowerDNS adoption](deploy/e2e/dns-kill-matrix/NATIVE-PDNS-PROTECTED-OWNER-CLI-20260926.md), and [fresh PowerDNS V3 prestart inverse](deploy/e2e/dns-kill-matrix/evidence/pdns-v3-prestart-20260928/README.md). Detailed trial limits are in each report. |
| 2 | P0.4/P0.5: complete missing source/peer fixtures and practical deletion verification without requiring a parent zone or a remote panel. | Real supported primary/secondary combinations prove add/edit/delete, native loaded-zone state and reboot; uncertainty gives an actionable same-operation recovery path. Do not reclassify fixture gaps as N/A. |
| 3 | P0.1–P0.5: close the remaining end-to-end update, access, schema, TLS/enrollment and workload matrix. | Owner UI update admission, failed candidate, automatic rollback, a second recovery fault, authenticated guidance and preserved native workloads agree on one operation. Each claimed platform/version combination has retained evidence. |
| 4 | Release review of the exact candidate and a short owner test path. | All required acceptance items are closed, or a deliberately scoped release states its still-open limits. Verify signed artifacts and recovery compatibility; only the user starts installed-panel updates. |

Each slice must identify the open acceptance it closes before more tests are added.
Repeat a passing trial only after a relevant change or to resolve a named uncertainty.
After retaining and checking evidence, stop disposable guests and remove their
verified temporary overlays; preserve recovery material and owner data.

**Completion date:** not established by current evidence. Remaining implementation
and fixture gaps prevent an honest percentage or fixed finish date. Re-estimate
after the independent DNS path and missing fixture scope are verified; completion
requires the existing acceptance gates, not a growing count of small commits.

### AI assistant milestone — after the operation/recovery foundation

The user-requested AI assistant remains planned: explain observed state and help
the user carry out authorized panel actions through the same typed, scoped
operation APIs. It must show the plan, honor permissions, retain the operation ID
and report verified results. Acceptance must include denied permissions, unavailable
services, interrupted requests and retry without duplicate effects. It receives
neither unrestricted root execution nor authority to invent missing evidence,
bypass licensing or initiate installed-panel updates. AI integration is not a
substitute for deterministic recovery and is not claimed implemented by this review.

---

## The Version Ladder

The version ladder below is a historical record of earlier milestones. Its past
golden-path results and architecture assessments do not establish compliance
with the September 14 resilience contract; that acceptance remains open.

Destination: **v1.0 — a panel a stranger can install on a clean VPS in minutes,
run a real hosting business on, and trust.** Everything below is a stone on that road.

The July 17 update worked three new requirements into the ladder — not a vague "later", but step by step:
1. **In-panel help/tips:** foundation stones in v0.2.5 (as natural extensions of the existing debt items),
   user-facing content in v0.3, help center + wizard + palette in v0.6–0.9.
2. **Reseller + customer as first-class experiences:** the door-opening slice in v0.2.5/B1, the body in
   v0.3 (including collection powers), `additional_user` as a real feature in v0.35.
3. **Free-tier subscription/plan system:** data model + period/cancel/suspend machine + payments ledger +
   reseller pools in v0.3, "every promise enforced or deleted" honesty in v0.35, the payment-provider
   decision (refunds/chargebacks included) in v0.5, self-signup + abuse brakes in v0.6–0.9.

### ✅ v0.0 — Takeover *(July 3, 2026)*
~23k lines inherited: architecture sound (Panel + root Agent, SQLite) but insecure
(open TCP agent, SQL injection, no authentication) and a UI full of fake data.
Decision made: continue, no rewrite.

### ✅ v0.1 — Secure Core + Proven Golden Path *(July 3–10, 2026 — historical v0.1.0 milestone)*
Eight days, four fronts, all pushed:
- **Security (Phase 0):** agent behind Unix socket + token · session identity (argon2id) + 2FA/TOTP ·
  SQL injection cleanup · CSRF/headers/rate limit · gosec highs closed · leaked passwords neutralized.
- **Hosting core (Phases 1–3):** domain types (php/static/node/proxy/redirect) + subdomains ·
  real auto-renewing SSL · authoritative DNS (PowerDNS/SQLite sync, DNSSEC, DANE) ·
  full mail stack (TLS+SNI, authenticated submission 587/465, DKIM signing, server policy,
  deliverability health screen) · databases v2 · one-click WordPress · cPanel importer v1 ·
  accounts (admin/reseller/customer, plans, quotas, impersonation) · entitlements + WireGuard VPN ·
  scheduled backups with retention · audit log · firewall (default-deny) · service uninstall ·
  unattended security patches · managed vendor repos (PGDG version choice).
- **Operations (Phase 2):** `install.sh` (one command → login screen) · snapshotting `update.sh` · `rollback.sh` ·
  systemd units · **golden path proven end-to-end on Ubuntu**: clean install → domain → HTTPS →
  own DNS answering the world → DKIM-signed mail in Gmail's INBOX.
- **Product & design:** Plesk-density UI, light/dark, TR+EN · design system on claude.ai/design
  (design loop: describe → agent draws with real components → filter → ship) · self-hosted brand fonts ·
  the new scale on every page · live dashboard with a setup journey.
- **Alpha operating model (D-008):** the operator drives the panel like a real customer; every wall they
  hit becomes a product fix. ~20 real bugs found and shipped this way in two days.

**Exit criterion met:** golden path proven end-to-end (Ubuntu) · the panel carries its own updates · the alpha model works.

### 🔶 v0.2 — Alpha Complete: The Debian Re-Proof *(historical milestone; current priority above)*
The same golden path, re-proven on the production VPS (Debian 13) **entirely with panel clicks**:
- ✅ Panel-only install (zero extra packages) · ✅ PowerDNS installed from the panel ·
  ✅ honest management page (config visibility, working repair)
- ⏳ Next clicks: auto-repair → first domain → panel Let's Encrypt certificate →
  DS record at the registrar → web server + live site → mail stack → **Gmail INBOX from Debian**
- Remaining alpha rough edges as they surface + `autodiscover` (mail client auto-config)
- External blocker: the domain suspension at the registrar (operator's task)

**Exit criterion:** a visitor can open `https://celikpanel.cloud` and mail sent from it lands in
Gmail's INBOX — every configuring click in the panel, none in a shell.

### 🩺 v0.2.5 — Debt Payment (the Autopsy Prescription) + Foundation Stones
The July 11, 2026 forensic audit ([AUTOPSY](docs/AUTOPSY.md)) surfaced live breaks and structural debt;
decision: **refactor, not rewrite.** B0 (stop the bleeding: dead TypeID constants, broken Databases page
for non-admins, dead code) closed the same day. The rest in order: **B1** one API (v2→v1, tenant scope
from auth, OpenAPI + generated client) · **B2** route+authz table · **B3** the catalog as sole owner of
service knowledge · **B4** UI discipline (one Button/fmtBytes/modal) · **B5** golden-path smoke CI.

The foundation stones of the three new requirements are laid on this step **not as separate work but as
natural extensions of B1–B5** (added later, they would all break a second time):
- **B1 addendum — the error contract:** all error bodies move to `{code, message, hint?, action?}`.
  `code` is a machine-readable constant (e.g. `DNS_SERVER_REQUIRED`), `hint` resolves via i18n keys in
  the frontend, `action` can be an in-panel link ("Install PowerDNS" → /services). Every deliberate
  refusal is coded: the D-009 409, quota 409s, conflict groups, the entitlement 402. One `ErrorBanner`
  component in the frontend; the error body is defined in the OpenAPI schema — the generated client is
  born right once.
- **B1 addendum — opening the way for Databases self-service:** server registration stays admin;
  DB/user CRUD endpoints open to customer+reseller under tenant scope; the temporary admin lock in
  `nav.ts` lifts; the phpMyAdmin proxy verifies ownership. (The real precondition of v0.3 — this is why
  the hard constraint exists.)
- **B2 addendum — fail-closed roles:** a request whose user record can't be read never proceeds with an
  empty role (today `middleware.go` continues with Role=''). On top of the route+authz table, a
  **role×endpoint matrix test**: with the `--demo` seed accounts, every (endpoint × admin/reseller/
  customer/anonymous) cell is verified against the expected 200/403/404; an endpoint not in the table
  fails the test.
- **B3 addendum — Setup Journey honesty:** journey steps read real service state from the catalog
  (installed + enabled + running), not package presence. Field proof already exists: the dormant bind in
  Hostinger's Arch image counted as "DNS installed: Done" (July 16). That scenario enters B5 smoke as a
  regression.
- **B3 addendum — catalog kinds + an "installed-first" default (Jul 20, D-010):** `ManagedService` gains
  `Kind` (service/runtime/tool) and `Role`; php-fpm and a new **node** entry become `Kind=runtime`,
  phpMyAdmin/phpPgAdmin become `Kind=tool`. Row rendering branches on `Kind` and the
  `Daemonless = len(SystemNames)==0` heuristic is deleted (it marks three different things today).
  **Versions are not rows; they live inside the row** (a version drawer) — list explosion is cut at its
  source. The Services page becomes installed-first: "hide not installed" defaults ON, categories
  collapsed, search always spans the whole catalog and overrides both. No separate `/runtimes` or
  `/apps` page.
- **B3 addendum — versions first-class + Node declared in the catalog:** one agent contract,
  `Agent.ListServiceInstances(id)`, returns Version/Unit/Path/Managed/SizeBytes per instance
  (`DetectInstalledPHPVersions` and `ListNodeVersions` are its first two implementations); the
  `extractVersion` switch and the `"default"` sentinel go. **The Node.js capability already exists in the
  code** (`runtime_rpc.go`, `app_rpc.go`) but is not declared in the catalog — this is visibility work,
  not new code. The node entry declares its web-server need via `Requires` (the reverse-proxy requirement,
  today expressible nowhere, becomes declarative).
- **B3 addendum — real multi-PHP (Sury):** D-002 claimed "✅ built" but only half was — detection and
  per-site selection work, multi-version INSTALLATION does not (`php-fpm` has no `Repo`, and no line in
  the codebase mentions `sury`). The existing `ManagedRepo` mechanism (PGDG) is applied to php-fpm:
  side-by-side `php8.x-fpm` becomes installable from the panel on Debian/Ubuntu; on Arch the panel
  honestly says "the distro's single version". The "a picker with nothing to pick" state ends.
- **B3 addendum — one address for runtime installs + an ownership ledger:** `AdminNodeInstall`
  (HostingTypePanel) is deleted; install/remove lives only in the Services version drawer (version
  endpoints parameterized — no Hestia 5050). The free-text semver box goes; the agent fetches the LTS
  list (3-5 named choices). The "system interpreter" escape hatch is removed — the panel runs only what
  it installed. A `site_runtimes` ledger + `RuntimeUsage`/`Dependents`: a version/service in use cannot
  be removed, returning a coded refusal (`RUNTIME_IN_USE`, `SERVICE_HAS_DEPENDENTS`) with the blocking
  site list (today php-fpm can be removed in one click while 40 sites use it).
- **B3 addendum — one source for project types + Node selectable at creation:** `CreationProjectTypes`
  (3 types) and `validProjectTypes` (5 types) derive from one `ProjectTypes` table. Add Domain gains a
  "Node.js application" card (domain + version + start command; the port is automatic). Preflight checks
  read from that table; the web-server requirement for node also returns a coded refusal **before
  persisting** — today's asymmetry ("disable the button for PHP, but for Node save and blow up in the
  agent") becomes impossible at code level. Running apps are counted under the Node runtime row
  ("3 apps · 1 failing") and link to their domain.
- **B4 addendum — the help layer's atom:** one Tooltip/InfoTip component in `ui.tsx` (HelpCircle +
  i18n'd explanation, keyboard accessible); the existing 6 Info callouts and ≥10 critical "what is
  this?" fields (DNSSEC DS, DKIM, catch-all, SNI…) migrate to it. The one obvious way to add a new tip
  is this component.
- **B4 addendum — i18n discipline:** a lint that catches bare strings in JSX (the "Coming soon..." in
  App.tsx goes; the vsftpd placeholder either becomes an honest i18n'd EmptyState or drops from nav) ·
  an en.ts/tr.ts key-parity check (`tools/check-i18n`) in CI — a missing key can't silently fall back
  to English.
- **B5 addendum — the framework-name CI gate (D-011):** grepping Go sources for
  `laravel|symfony|django|nextjs|ghost` must return zero matches (i18n strings excluded). A rule living only
  in Markdown will not survive two years; once an enum constant / DB column / API value / systemd unit name
  carries a framework name, the catalog is implicitly born and becomes irreversible because it persists in the DB.
- **Version singularity + CHANGELOG:** annotated git tag (first candidate v0.2.0) · the version is baked
  into both binaries via `-ldflags`, served from `/api/v1/panel/version`, and the hard-coded "v0.1.0" in
  Layout.tsx is deleted · CHANGELOG.md + CHANGELOG.tr.md start in Keep-a-Changelog format; `update.sh`
  prints "changes: CHANGELOG.md" on exit.

**Exit criterion:** AUTOPSY B1–B5 closed · the role×endpoint matrix runs in CI and no endpoint returns
200 for anonymous/empty-role · all deliberate 4xx checks are coded and ErrorBanner translates them ·
`panel --version`, the UI footer and the git tag say the same string · user-visible non-i18n English
strings in web: 0 (lint in CI) · as customer: create DB → open phpMyAdmin → accessing someone else's DB
is 404 (all three in B5 smoke) · **on a clean server the Services page shows at most 4 rows** (nothing
uninstalled is drawn) · the only way to install a Node version in the panel is Services
(`AdminNodeInstall` is absent from the codebase) · Add Domain creates a working Node site in one form (no
"create static first, then switch type" step) · on Debian a second PHP version installs from the panel and
is assigned to a site (Sury), while on Arch the same screen honestly says "the distro's single version" ·
attempting to remove an in-use PHP version/service returns a coded refusal listing the blocking sites ·
the project-type list lives in one file; every criterion verified on both test servers.
**Hard constraint: v0.3 cannot start before B1 is done.**

### 🚨 v0.2.6 — The Trust Floor *(new step, 25 Jul 2026)*
The code audit run while preparing this roadmap found **three live data-loss / dishonesty defects**.
They come before every other feature: a panel cannot be sold until someone can entrust a paying
customer's website to it. All three were verified in the code, not guessed.

**Three holes in the floor:**
1. **A "Full" backup contains no databases.** `createFullBackup` falls through to the files backup,
   with its own comment admitting it (*"For now, just backup files"*). An operator who takes a "Full"
   backup before a risky change cannot get the database back — silent data loss dressed as a safety
   net. For the same reason, restoring a `full_` archive returns only files.
2. **Restore can overwrite the wrong database.** The target name is derived by splitting the file
   name on underscores and taking the first part: a backup of `wp_site1` is restored into a database
   called `wp`. Underscores are common in database names, so this means **overwriting another
   customer's data**.
3. **Install never checks whether the service started.** The result of `systemctl enable --now` is
   discarded and success is reported unconditionally. The constitution's first rule — "installed
   means working" — is not measured at the one moment that decides it.

**Four structural gaps in the floor:**
4. **Template fixes never reach existing sites** (config drift, AUTOPSY section C). Two closed
   security findings — `.env` served as plain text and PHP source offered as a download — are still
   live on every site created before the fix. There is no "regenerate every vhost" path.
5. **Four site settings never reach the server.** Document root, www/https redirect, HSTS/force-HTTPS
   and domain aliases are written to the database and never to nginx. The operator changes a setting,
   the screen confirms it, and the server keeps serving the old one.
6. **Removing SSL does not regenerate the vhost** — it arms an outage for the next reload.
7. **Apache takes the web-server seat with no Apache writer.** The only vhost generator is nginx's
   (`internal/services/nginx_generator.go`; the template directory contains only `nginx/`). Installing
   Apache blackholes port 80. Until the writer exists, the row must refuse honestly.

**Exit criteria:** a "Full" archive opened by hand CONTAINS a database dump and every archive carries
its own manifest · the restore target is read from the manifest instead of guessed from the file name
(an underscored name lands in the right database on both servers) · a snapshot is taken before every
restore · install does not say "installed" when the start failed, and shows the journal tail as its
reason · the four site settings changed in the panel are measurable on the server · `nginx -t` passes
after SSL removal · one vhost writer remains and a "regenerate every site" action fixes existing sites
too (both old security findings close on pre-existing sites) · the Apache row is refused with a coded
reason while no writer exists · CI boots a real panel and runs the smoke scripts for these criteria.
**Hard constraint: v0.3 cannot start before this step is done.**

### v0.3 — Multi-Tenant Reality
Selling to more than one tenant without embarrassment. Four legs: customer and reseller can live on
their own; the plan/subscription machine can express "free tier + paid plan" **from entry to exit**
(cancellation as first-class as purchase); the essentials a cPanel emigrant looks for in week one are in
place; production trust is done before the first real tenant.

**Customer and reseller first-class:**
- The customer sees their own subscription: `GET /api/v1/my/subscription` (on B1 tenant scope) — plan
  name, quotas, live usage (domain/DB/mail counts + measured disk). A "My plan" card on the Dashboard:
  usage bars, warning color above 80%, an "Upgrade" button. A 409 quota error is consistent with the
  numbers on screen.
- Password recovery: single-use, 15-minute, argon2id-hashed token; mail from the panel's own MTA.
  E-mail verification arrives — no reset is sent to an unverified address.
- Invitation flow: password optional at user creation; without one the account opens "pending" and a
  first-password link goes by mail. A reseller never sees or relays a customer's password on any channel.
- Password change and reset drop all of the target's other sessions (completing the "leaked password
  neutralized" promise — today an open session survives a password change).
- Impersonation is accountable: `impersonate.start/stop` written to audit_logs; actions under
  impersonation carry an `acting_as` field marking the real operator; a persistent "Viewing as X — exit"
  bar at the top of the panel.
- **Reseller collection powers:** a reseller can call suspend/resume, "mark paid" and plan change for
  subscriptions in their own tree (B1 tenant scope filters); all of it lands in audit with `acting_as`.
  The reseller also sees their customer's subscription + payment state. A reseller who can't cut off a
  non-paying customer forwards collections to the operator — "reseller first-class" is half a promise
  without collections.
- Role-aware onboarding (the existing journey card pattern, no library): for the customer "first domain →
  SSL → first mailbox → connect your client"; for the reseller "plan → first customer → subscription".
  Tracks live completion, disappears when done.
- Per-page descriptions: one-two sentences TR+EN for 12 routes + 8 domain tabs (`pages.<id>.desc`).
  A "Why?" explanation at the 5 constraint-producing spots (D-002, D-003, D-009, conflict groups, pkg
  support) — texts consistent with the DECISIONS records; a constraint never hits as an unexplained wall.

**The plan and subscription machine (the free tier's foundation):**
- Prices on plans: `service_plans` gains `price_cents, currency, billing_period, is_free, is_public,
  sort_order, vat_included`. The admin defines "Free — 1 domain, 0₺" and "Pro — 10 domains, X₺/mo" from
  the panel; product prices move from code constants to the DB. The customer sees their plan's name and price.
- **Offering ledger (D-017 / the missing link in D-014):** `plan_offerings` uses one canonical namespace:
  `component:<id>[:<version>]`, `integration:acme:<id>`, and `product:<id>`. Selector and action endpoints
  must enforce the caller's effective offering set, with coded `NOT_OFFERED` refusals and audit records.
  Existing plain Store IDs need an explicit backward-compatible data/API migration; do not rename them ad hoc.
- **The period model** (the answer to "I upgraded — what am I paying?"): subscriptions gain
  `current_period_start/end`; the rule is the simplest honest one and goes to DECISIONS — an upgrade
  starts a new period immediately (no proration, declared openly); downgrades and cancellations apply at
  period end; `expires_at` derives from the period edge. The v0.5 webhook extends these fields — no
  provider integration is built on top of an undefined term.
- Subscription suspension produces real effect (today `status`/`expires_at` are dead fields): on a
  suspended/expired subscription new resources are 403, vhosts flip to a reversible "account suspended"
  page, mail delivery stops (mailboxes are not deleted). Subscriptions past `expires_at` are flipped to
  expired by a daily loop + a grace-period field. A manual "mark paid" button drives the same machine —
  the payment-provider decision is v0.5; the machine works from day one.
- **Cancellation is first-class** (the subscription form of "data is never held hostage"):
  customer-triggered "cancel at period end" (`cancel_at_period_end`); automatic downgrade to Free at
  period end. If usage exceeds the Free quota the machine doesn't deadlock: **forced-downgrade mode** —
  nothing is deleted, the excess is frozen (new resources 403 + excess vhosts get the suspension page) +
  a list of the overflowing resources + an X-day wind-down period + a notification mail.
  `subscription.cancel` lands in audit. (The same mode drives the v0.6 trial expiry — an unattended
  downgrade can't hit a 409 and stay Pro forever.)
- Plan change (upgrades are monetization's main flow): `PUT /api/v1/subscriptions/{id}/plan` — quotas
  are re-copied from the plan; on a **human** downgrade, if current usage > new quota then 409 + the list
  of overflowing resources; in **unattended/forced** mode, the freeze rule above. `subscription.plan_change`
  in audit. A customer's "Upgrade" request initially produces a notification to the operator.
- **The payments ledger** (even manual mode leaves a trace): a `payments` table
  (subscription_id, amount_cents, currency, period, method=manual|provider, marked_by, created_at).
  "Mark paid" writes to it; the v0.5 webhook feeds the same table. The customer sees a payment history
  under the "My plan" card + a printable simple receipt. "What did I pay, what did I get" is answered
  from the panel.
- **Time-based notifications** (the cheapest collections tool): mail from the panel's own MTA to the
  customer (and the reseller, in reseller scenarios) at `expires_at`−7/−3/−1 days and at grace start; a
  "why + how to reopen" mail at suspension. The customer learns of the suspension from their inbox, not
  from their visitors.
- **The reseller pool is a plan type** (a quota with a commercial life): `reseller_pools`
  (max_customers, total disk/domain/DB) binds to the reseller's plan — pool sizes + price are written in
  the reseller plan; the reseller version of the "Upgrade" flow grows the pool. When a subscription
  opens, the reseller tree's total commitment is compared to the pool; on overflow 409 + remaining-pool
  message; a usage bar in Users. **The chain rule** (to DECISIONS): if a reseller is suspended, new
  resources in their tree are 403, but existing customer sites/mail live until the end of grace — an
  innocent end-customer is not blacked out instantly for their reseller's debt.
- Reseller-owned plans: the dead `service_plans.owner_id` comes alive — a reseller builds their own
  plans (quotas can't exceed their pool), sees global + own plans in the list, assigns only to their own
  customers; "apply to subscribers" copying respects owner scope.
- **Licensed products and the resale chain (D-012, Jul 20 — third parties in scope from the start):** an
  entitlement becomes a **pool** like disk; the `reseller_pools` pattern extends to products (admin quota →
  reseller → customer; the admin may also sell directly to a customer). The product definition gains
  `license_model {server|seat}` + `seat_unit {mailbox|site|subscription|server}` — under *seat*,
  over-allocation is real money and a licence breach, so the pool is enforced hard (a coded refusal, not a
  warning). The licence key is sealed with A4's `enc:v1`. A price stops being one number (vendor→admin,
  admin→reseller, reseller→customer; the same shape as reseller-owned plans). Visibility follows
  entitlement: if the reseller did not buy it, their customers never see it (a reseller may switch on a
  "buy" prompt). An entitlement cannot be sold for a product that is not installed → coded refusal.
  Revocation follows the SAME rule as subscription suspension (new allocations 403, existing usage lives to
  the end of grace, no data deleted). **The honesty limit is stated in the UI and the docs:** the panel
  enforces only its own allocation records; the vendor may count differently (a reconciliation view is
  shown, but the invoice is the vendor's truth) and **the right to sublicense is between the operator and
  the vendor** — the panel does not verify it and does not claim to.
- The billing ledger: `plan.create/update/delete`, `subscription.plan_change`, `subscription.cancel`,
  `subscription.suspend/resume`, `quota.exceeded` audit events — "when and by whom did this quota
  change" is a dispute question; it is never unrecorded.

**Hosting essentials (a cPanel emigrant's first week):**
- FTP (vsftpd) end-to-end — with criteria: per-domain accounts, chroot to the site user's docroot,
  **FTPS required** (plain FTP refused — security is the default). Proof: connect with FileZilla →
  upload → the live site changes; an escape attempt from chroot fails.
- Webmail (Roundcube) — with criteria: one-click install from the catalog; `webmail.<domain>` vhost +
  Let's Encrypt + Dovecot wiring automatic. Proof: a mailbox created in the panel sends mail to Gmail
  from webmail and reads the reply, without ever touching a shell. (Roundcube is not "for the panel" but
  "a service the panel installs" — the no-external-dependency rule is untouched.)
- File manager polish — three measurable items: (1) upload-and-extract zip/tar.gz + compress-and-download
  selection, (2) in-place text editing (ownership/permissions preserved), (3) view/change permissions
  (warning at 777). Proof: a WordPress theme zip installed via the file manager alone shows on the site.
- Noisy-neighbor brake: a systemd slice per site/subscription (CPUWeight + MemoryMax as plan fields;
  a field only arrives together with its enforcement — no dead fields are born). PHP-FPM pools and
  `celikapp-*` units attach to their slice; no CloudLinux license required. Proof: with one tenant
  running an infinite PHP loop, the neighbor site opens in <1 s.
- OS-level disk enforcement (the ROLES deferrals) · the cPanel importer proven with a **real** customer
  archive (DB users included) · WordPress Toolkit depth (updates, hardening, clone/staging).
- **Framework hosting primitives (D-011, Jul 20):** the four real blockers to hosting
  Laravel/Symfony/Django without a catalog entry — none of them framework-specific, all of them missing
  *generic* capabilities: (1) **docroot subdirectory selection** — pinned to `public_html` today; the
  dropdown lists PATH values, not framework names (`(root) | public | public_html`). (2) **A run-as-site-user
  command endpoint** — to run `composer install`, `artisan migrate`, `npm ci` (today `composer` appears zero
  times in the codebase; the user is pushed to SSH). Streamed output, timeout, audited. (3) **Long-running
  processes (queue workers)** — three independent blocks today: the `RunAsUser: "www-data"` constant, the
  `req.Port <= 0` rejection, the `project_type == "node"` lock; the `celikapp-*` unit abstraction must also
  carry a portless, site-user worker. (4) **Site cron** — the scheduler is not a separate concept, it is an
  ordinary crontab line. On top of these, a **preset**: a button that PRE-FILLS the form with a framework's
  defaults (not a type, not an installer); it is subject to D-011's structural purity test — a preset needing
  one new field or one `if framework ==` branch is rejected, and passing 3 presets opens a strategy debate.
- **DNS provider abstraction (D-009 re-weighing + Jul 18 operator decision):** DNS must be a CHOICE, not
  an imposition. The panel already computes the full record set and writes it to one place (its own
  PowerDNS); that "writer" becomes pluggable — three backends, operator's choice (possibly per domain):
  (1) **Self-hosted PowerDNS** (default, zero-dependency: for those who want everything in one box — the
  panel is authoritative, ns1/ns2 are this server);
  (2) **Cloudflare-class managed DNS** (the operator provides a DNS-edit-scoped API token; the panel writes
  the SAME record set to the provider's API instead of PowerDNS SQL; creates the zone if missing). **The
  recommended path for security** and the operator's Jul 18 observation: :53 isn't exposed on the box,
  Cloudflare absorbs DDoS, the single point of failure is gone. The honest core counterpart of Plesk's
  "Cloudflare DNS Integration" extension;
  (3) **External/manual** (the panel writes nothing; a "enter these records" list + the live verification
  from mail-auth). Everything downstream (mail-auth records, HTTP-01 certs, the panel hostname) works
  unchanged — only the writer differs, the computed record set is identical. **The ONE un-automatable step
  is stated honestly:** the registrar's nameserver delegation (pointing celikhost.com's NS at Cloudflare
  or at this server) exists in no provider API; the panel shows + verifies it, the human makes that one
  click at the registrar. The decision (which backends, which default, the recommendation text) goes to
  DECISIONS; the abstraction seam + paths (1) and (3) in v0.3; the (2) Cloudflare backend in v0.4 (see the
  managed DNS backend).
- **Panel identity — guided hostname + certificate (Jul 18 field gap):** making the panel's own name
  (e.g. `boston.celikhost.com`) resolve was NOT designed — on the test servers a non-operator hand (a
  record added from ANOTHER server's panel) closed it; that is the hidden manual step D-008 forbids. The
  panel must handle its own hostname with the same three honest paths as adding a domain: (a) **if this
  panel serves the parent zone itself** → one-click write the A record into its own zone (single-server,
  the generalization of the zone template seeding its own FQDN); (b) **DNS is external / on another
  server** → show "add this A record: `<host>` → `<IP>`" and wait, via the live DNS check from mail-auth,
  until it resolves, then offer the certificate; (c) **already resolves** → straight to the certificate.
  The certificate flow (v0.2) sits on top of this pre-step — the "install.sh → login → real certificate"
  chain no longer has a manual DNS gap. Cross-server auto-registration (registering a sibling's name in
  the zone-authority server) belongs deliberately to the multi-server feature (post-1.0) — it needs an
  inter-panel trust model; until then path (b) serves N servers honestly.

**Production trust (required before the first tenant):**
- Secret encryption pulled forward: A4's proven `enc:v1` mechanism extends to TOTP secrets and the
  private keys the panel stores (DKIM, WireGuard); legacy rows sealed idempotently at boot. (Only the
  external-audit verification remains in v0.5 — the first tenant's 2FA secret doesn't wait months in
  plain text.)
- Per-tenant rate limits: expensive endpoints (certificate issuance, backup trigger, import, bulk DNS
  writes) protected by subscription-keyed limits; a Let's Encrypt failed-attempt counter + an honest
  "approaching the LE limit" warning — one tenant's loop can't block everyone's certificates.
- Migration discipline (expand/contract): a destructive schema change splits across two releases (add +
  double-write in N, remove in N+1) — rollback loses no data. Two CI tests: the full chain onto a clean
  DB + the current chain onto a populated v(N−1) fixture; `rollback.sh` reports the number of rows
  written after the snapshot and asks for explicit confirmation; the sentence "rollback loses changes
  made after the snapshot" is documented.
- CI security gates: `gosec` and `govulncheck` on every PR; networked dependency audits require explicit
  operator authorisation because they disclose package names and versions to the configured registry; exceptions are
  `#nosec` + reason. (The v0.5 external audit is met with these gates' monthly green history.)
- A written promotion ritual (OPERATIONS.md): (1) CI green → (2) `update.sh` + golden-path smoke on both
  test servers (boston/Debian, frankfurt/Arch) → (3) production. Channels become explicit: main=edge
  (test servers), tag=stable (production runs only tagged commits).
- `release.yml`: on a `v*` tag push, `make dist` in CI → SHA256SUMS → tarball+checksum+CHANGELOG section
  automatically to the GitHub Release.

**Exit criterion:** one reseller + two customers run **one week self-service** — password reset,
invitations, quota views, DB, FTP, webmail included; operator touches: zero · the admin defines Free +
Pro plans from the panel, one subscription moves to Pro in a single call, the audit record lands · a
cancelling Pro customer automatically drops to Free at period end; resources over quota frozen and
listed, no data deleted · a subscription near expiry receives the −7/−3/−1 mails; a suspended one
receives the "why + how to reopen" mail · every "mark paid" lands in the payments ledger and the
customer sees their receipt in the panel · a suspended subscription serves the suspension page within
60 s; resuming loses zero data · a reseller with a 10 GB pool cannot open two 6+6 GB subscriptions (the
second is 409); a reseller suspends their non-paying customer and reopens them with "mark paid" on their
own · a real cPanel account migrates in one click · the external-DNS decision is recorded in DECISIONS ·
the v0.3.0 tag produced a downloadable release without a human hand.

### v0.35 — Plan Honesty: No Dead Fields
A short step. Every promise that exists in schema/catalog but is not enforced is either enforced or
deleted — the honesty rule's plan form. The paid tier isn't "done" until every line of a sold plan is real:
- `bandwidth_quota_mb`: enforce or remove. Enforcement: a subscription-level monthly counter from usage
  measurement (reset at period start); nginx logs count only web traffic — that mail/FTP are excluded is
  honestly written in the plan text.
- The overflow policy — the single answer to "when does the limit bite": per plan
  `enforcement ∈ {block_new, notify, suspend_writes}`; mail to customer + operator at the 80% and 100%
  thresholds; a quota row in the Dashboard's "Needs attention".
- Mailbox quota: `mailbox_quota_mb` + the Dovecot quota plugin (the existing file-based pattern,
  idempotent full-state push). `business_email` raises this limit — the product's first real gate.
- Product gates: every product listed in Addons is either wired to at least one `requireEntitlement`
  gate or cannot be purchased. `extra_ip` plumbing stays "coming soon" until v0.5; `firewall` leaves the
  catalog until a customer-visible feature exists. No "for sale" product that works without purchase remains.
  **`app_installer` is brought back to reality (D-011):** today's "WordPress *and other apps*" sells a
  one-entry list as a plural plan feature — it shows sales an empty bucket to fill, and once the list is bound
  to a plan feature, deleting an entry becomes a contract breach. The product becomes "One-click WordPress
  install"; the plural wording goes.
- `additional_user` becomes a real feature: bound to a customer account (`parent_id`), resource-scoped
  permissions via `user_permissions` (domain list + file/mail sub-permissions), its own login. (The CHECK
  widening and the honesty of the dead role branches in the frontend happen in v0.2.5/B2.)
- The user-detail view: one screen with a customer's subscriptions + quota usage, domains, entitlements,
  last login (`users.last_login_at`) and last 10 audit rows — "why is this customer getting 409" doesn't
  tour four screens. Admin sees all; a reseller sees their own tree (the screen of v0.3's collection powers).
- Cron reliability: per job, last run time + exit code + a tail of the last output; on a failing job,
  mail to the domain owner (the existing mail stack, no new dependency).

**Exit criterion:** not one dead quota/state field between schema and enforcement (proven with a
field-by-field checklist) · a test subscription reaching 80% gets mail within 5 minutes; at 100% the
policy applies · the 101st MB into a 100 MB mailbox is refused with "Quota exceeded" · every Addons
product gated or unpurchasable · an additional user sees only the permitted domain's tabs · a cron
deliberately exiting 1 shows red in the list and lands in its owner's INBOX.

### v0.4 — Operational Trust
What the operator needs at 3 a.m.:
- Monitoring + alerts (service down, disk full, certificate error → mail/webhook) · log viewer in the panel
- **Metric history (from the Plesk comparison, Jul 17):** today's cards show an instant value, not a
  story. A lightweight sampler in the agent — CPU/RAM/disk/traffic every N seconds into a SQLite ring
  table, old data auto-downsampled (NO external dependency: no Prometheus/Grafana, the constitution
  holds). Sparklines on the dashboard cards (cards stay quiet); clicking a card opens a 24-hour / 7-day
  detail chart. Alert thresholds read the same data — charts and alarms are two faces of one substrate.
  Multi-location external uptime monitoring is deliberately out of scope: the honest answer is the
  heartbeat + "use UptimeRobot/360".
- **The alert channel's own health:** alerts go via two independent channels, mail AND webhook (if mail
  can't enter the queue it falls to webhook) + an outward heartbeat — the panel pings an external
  endpoint the operator chose every N minutes; when it stops, the alarm rings FROM OUTSIDE. The most
  likely failure is the death of the channel that carries the alert.
- Remote backup targets (S3/FTP) + restore drills as a product feature — **with two hardenings:**
  (1) **Panel-state disaster backup:** SQLite (consistent copy via the online backup API) + `secret.key` +
  DKIM/WireGuard keys + panel certificates in one archive, to the remote target with the same retention
  as domain backups. Losing `secret.key` = every sealed secret irrecoverable; "domains in backup but the
  panel's brain missing" is not accepted. (2) **Client-side encryption:** every archive leaving for a
  remote target is encrypted in the panel; the backup key is kept separate from `secret.key` and shown
  once at setup — the "backups unreadable without the key" honesty is written in the UI.
- **Customer self-service restore:** from the backup list, full-site / single dir-file / DB dump
  restores — with a confirmation modal + audit record. The most expensive answer to "will YOU solve
  every user's problem?" is restore; at 3 a.m. the customer reverts their own mistake themselves.
- **The secondary-DNS truth:** today ns1/ns2 point at the same machine (a single point of failure); a
  secondary PowerDNS (AXFR) on a cheap second VPS, or honest documentation (added July 11)
- **Managed DNS backend (Cloudflare-class) — the concrete provider for v0.3's abstraction:** a "DNS
  provider" choice in Settings; the Cloudflare backend = a scoped API token + zone auto-creation + a
  second writer on the `syncZoneToDNS` seam (same record set, different target). This is also the
  cleanest answer to the secondary-DNS single-point problem: handing DNS wholly to Cloudflare is simpler
  and safer than standing up AXFR on a second VPS. The panel never opens :53; by "security is the
  default" it RECOMMENDS this path but does not impose it (self-hosted PowerDNS stays the zero-dependency
  default for those who want it). The registrar NS delegation is shown honestly as a manual step +
  verified. This is NOT an external dependency of the panel itself — it is an operator-chosen optional
  backend (like the MariaDB↔PostgreSQL choice); the token is the operator's, and the panel runs fully
  offline.
- One-click panel self-update in the UI (update.sh's front end) · WebSocket live notifications
- **The update chain hardens:** the release-binary channel becomes primary — `update.sh` downloads the
  release tarball, **verifies a signature** (minisign/cosign; public key baked into install.sh, rotation
  plan written), verifies SHA256, skips the build step. No Go/Node toolchain needed on production
  (attack surface + OOM on small VPSes + bit-level drift close all at once); `--from-source` remains for
  dev/test servers. On verification failure: stay on the current version + "Needs attention" + audit record.
- **Post-update self-check:** an automatic smoke at the end — panel HTTP 200 + login render + agent
  socket ping + `PRAGMA quick_check`; if anything remains the output says "rollback.sh recommended", and
  the one-click flow offers a rollback button.
- **The failure matrix — proof that "panel dead, hosting alive":** panel process killed → web/DNS/mail
  keep serving (measured by test), the panel comes back via `Restart=on-failure`; a "break glass"
  runbook (diagnosis steps over SSH when the panel won't start) is written into D-008 as the emergency
  exception.
- **CelikPanel→CelikPanel migration:** an account-level export archive (domains + docroot + DB dump +
  maildir + DNS zone + DKIM key + subscription/quota metadata in one signed tar); on the target server
  the cPanel importer's inspect→confirm→apply flow recognizes this format too. The full form of "data is
  never held hostage"; changing servers is hosting routine.
- **Visitor statistics (minimal, deliberately bounded):** traffic measurement already reads the nginx
  access log; the same pass extracts daily hits / unique IPs / top 10 pages / top 10 referrers into a
  card on the domain Overview. Full analytics (sessions, geo, real-time) is out of scope — the honest
  answer: "put Plausible/GA on your site".
- **Audit integrity:** write failures are counted and surface in "Needs attention" (without blocking the
  action, but never silent); configurable audit retention + pruning.
- **Active sessions:** a session list in Settings (created, last used, IP) + single/bulk termination;
  an admin can drop a target's sessions from Users.
- **Clean start:** the panel marks catalog services it did not install itself as "foreign" (any catalog
  service with no `service.install` audit-log entry) and offers removal with the operator's consent —
  adopt or evict, the mirror of the Import philosophy. Field proof (July 16): Hostinger's Arch image
  shipped a dormant bind; the setup journey counted "DNS installed: Done". Builds on B3.
- **Dashboard SSL/TLS summary** (from the Plesk comparison, July 17): expiring-soon / valid /
  no-certificate counts in one card — the "certificate silently dies in 90 days" class becomes visible
  on the panel's face
- **Dashboard Mail Queue card** (from the Plesk comparison, July 17): Total/Deferred/Held plus one-click
  queue clear — the first place an operator looks at 3 a.m.
- **Self-diagnosis:** the operator's July 17 question is the design bar — "will YOU solve every user's
  problem?" The panel must check by itself the classes we diagnosed by hand: does the DNS delegation
  actually point at this server, **does the panel's own hostname resolve to THIS server** (Jul 18: the
  boston.celikhost.com record lived in frankfurt's zone, boston had no idea — that coupling was
  invisible), does the certificate renewal timer actually run, can the service config actually start the
  engine. A finding = a "Needs attention" row + one-click repair (the honest counterpart of Plesk's
  Repair Kit)

- **Catalogue widening — Python, databases on Arch, the migration lever (25 Jul 2026):**
  - **Python as a runtime.** There is **no Python in the panel today** — not a catalogue entry, not a
    line of code; the comment in `hosting_handlers.go` says "so go/python can be added without a table
    rebuild", but the entry was never opened. The three primitives Node uses already exist and are
    largely generic: a per-site systemd unit (`ApplyAppUnit`/`ControlAppUnit`/`AppUnitStatus`/
    `AppUnitLogs`), the reverse proxy in front of it, and the version drawer. Python adds three things:
    a **per-site virtual environment (venv)**, an **application-server choice** (gunicorn/uvicorn — the
    WSGI vs ASGI split is a product decision and cannot be hidden), and a "Python Application" card in
    Add Domain. **No use of the system python3:** the "system interpreter" leak deliberately removed
    for Node is not reopened here — the panel only runs what it installed itself. Django/Flask/FastAPI
    hosting is the equivalent of cPanel's "Setup Python App" and the place most free panels do badly;
    it is the largest market widening that requires no new concept.
  - **Databases on Arch.** MariaDB and PostgreSQL are not offered on Arch at all, because the catalogue
    has no "post-install initialisation" field (there is no way to express `initdb` /
    `mariadb-install-db`). **One catalogue field** opens both components, closes two cells in the
    distro matrix, and makes one-click WordPress possible on Arch. This is the measurable test of the
    "no OS lock-in" claim.
  - **DNS zone import/export (+ CAA).** Nobody switches panels without a path in; zone transfer is the
    cheapest migration lever, and owning our own authoritative DNS makes it structurally easier here.

  **Exit criteria:** on both distros a Python version installs from the panel and a single Add Domain
  form brings up a working Django/FastAPI site (venv per site, unit running, reverse proxy correct) ·
  MariaDB AND PostgreSQL install from the panel on Arch and the first database is created · those
  three cells in the distro matrix are no longer empty · a cPanel zone file imports and the domain
  answers queries correctly.

**Exit criterion:** a killed service alerts within a minute; a server with its plug pulled produces an
**external** alarm within 5 minutes · the restore drill's definition: on a clean VPS, `install.sh` +
panel-state restore + domain backups bring the full server up and DKIM-signed mail lands in the INBOX
**with the same keys** · no object in the remote store is unencrypted · a version update succeeds on a
production VPS with no Go/Node installed; a tarball with a broken signature is refused and lands in
audit · a customer restores their deleted `wp-config.php` from the panel on their own · every cell of
the failure matrix drilled at least once.

### v0.5 — Security Depth
- The WAF decision (ModSecurity or an honest alternative) · deep fail2ban integration · scheduled ClamAV site scans
- External-audit verification of secret encryption (implementation done in v0.3: TOTP + DKIM + WG keys under `enc:v1`)
- External security audit · dedicated IP plumbing (the sellable `extra_ip` — the gate marked "coming soon" since v0.35 opens)
- **API token management:** named tokens per user (crypto/rand, only the hash in the DB, shown once,
  scope: read-only/full, revocation); token'd requests pass the tenant-scope filter and land in audit.
  The "API-first" promise cannot be kept with an API unusable from curl.
- **SFTP/SSH key management:** the customer adds/removes public keys for the site user (ledger in the
  panel, the agent writes `authorized_keys` with full-state push); password login and shell **off by
  default** (internal-sftp + chroot docroot); `access.ssh_key.add` in audit. This is the path for
  agencies beyond plain FTP and for CI/rsync/git-hook flows; an in-panel terminal stays deliberately
  out of scope.
- **2FA depth:** 8 single-use recovery codes at enablement (argon2id-hashed, shown once); a "use a
  recovery code" branch at login; a "2FA required for admin/reseller" setting — when required, a user
  without 2FA is locked to the setup screen at first login.
- **The payment-integration decision record (D-0xx):** provider class = hosted-checkout /
  Merchant-of-Record (the Stripe Checkout / Paddle / iyzico class — card data never enters the panel,
  PCI scope zero); the panel is only a **webhook consumer**: a subscription↔provider-customer mapping
  table + one signature-verified, idempotent webhook endpoint (a ledger of processed event_ids) +
  `payment.event` audit. The event contract has three classes: "paid" → the period extends (a row in the
  payments ledger), "unpaid" → grace → suspension (the v0.3 machine), **"refund/chargeback"** — refund:
  the period's extension is reverted (`expires_at` shortens) + `payment.refund` audit + operator
  notification; chargeback: automatic suspension + a "Needs attention" row (a chargeback is also an
  abuse signal). Invoice PDFs are links from the provider. The manual "mark paid" mode remains for the
  provider-less operator; **in the first release the provider integration is operator-plane only —
  reseller collections continue via the manual flow (deliberate and written).** Constitution constraint:
  one binary + SQLite — payments are never embedded in the core.

**Exit criterion:** the external audit yields no high-severity findings · a customer buys and uses a
dedicated IP from the panel · the single curl example in the docs opens a domain with a token; a revoked
token is 401 immediately · no plain secret is grep-able in the DB dump or dataDir · a forged webhook
event extends `expires_at` on the test server; the same event a second time is a no-op; a refund event
reverts the extension · login with a recovery code succeeds once, 403 the second time; with enforcement
on, a reseller without 2FA cannot log in · a keyed customer sees only their own docroot over sftp; a
shell attempt is refused.

### v0.6 – v0.9 — Beta Program
- OpenAPI documentation (proof of the API-first promise) · admin and user guides TR+EN
- **Help center + deep links:** a static TR+EN site generated from markdown under docs/ (simple
  generator, no external dependency enters the panel); a help icon in every panel page's header going to
  the doc page via a stable slug (`pages.<id>` → `/help/<lang>/<slug>`); a broken-slug test in B5 CI.
  Documentation not linked from the panel is born dead.
- **Browser first-boot:** when no admin exists in the DB the panel enters a one-time first-boot mode —
  the admin is created from the browser with a single-use token produced by `install.sh`; the mode closes
  permanently once an admin exists. The "return to the terminal, run a CLI" step in the middle of the
  "install.sh → login screen" promise disappears; placed in this release, not rushed, so that no
  unprotected first-setup endpoint ever exists.
- **Self-signup + abuse brakes** (the free tier's scaling path): an e-mail-verified signup form —
  **default OFF** (security is the default), the operator who enables it binds it to a free plan;
  a disposable-domain list + per-IP signup rate limit. Brakes: an hourly outbound-mail cap on the free
  plan (on the mail_policy pattern), an optional 24-hour send-hold on new accounts, resource-creation
  rate limits. Trials: `trial_days` per plan → `expires_at` fills automatically; on expiry the account
  drops to free **in forced-downgrade mode** (the v0.3 rule — an unattended transition can't hit a 409).
  One bad customer can burn the IP reputation earned with the deliverability screen — signup does not
  open without brakes.
- **Command palette:** Ctrl+K — nav + domain list + service pages in one fuzzy search; navigation only
  in the first release (no action execution — the security surface doesn't grow), a "?" shortcut list
  beside it; dependency-free, respects the role filter. It would be a luxury on today's 12 routes; the
  right moment for a beta operator's daily efficiency is here.
- **SUPPORT.md (TR+EN):** before 1.0 only the latest minor is supported, security fixes are patched onto
  the latest minor; the upgrade path is "from any release to the newest, the migration chain proven with
  fixture tests"; at v1.0 it widens to N−1. Published together with the beta invitation — the answer is
  not improvised in the moment.
- **KVKK/GDPR minimums:** data export = the customer-triggered form of the migration archive; account
  deletion covers, beyond the DB cascade, the maildir + docroot + the account's backup archives and
  produces a "what was deleted" report; log/audit retention configurable and documented.
- Real external beta users; their walls become fixes (the alpha model, scaled)
- Performance targets measured and enforced (<100 ms API)
- **The license/business-model decision** (suggestion: open core) → repo visibility accordingly. With
  the decision, two commitments go to DECISIONS: (1) **the two-plane separation** — the operator
  charging their own customers (a panel feature) and CelikPanel charging the operator (a license) are
  separate planes; no table/endpoint of the first ever carries telemetry/license checks for the second,
  and the panel works fully offline. (2) **Price positioning:** the price is per server and **never**
  scales with hosted accounts/domains — cPanel's post-2019 per-account model triggered the largest
  migration in panel history; announced late, beta arrives with the "this too will cPanel-ify" suspicion.
- **The "Why CelikPanel" document** (docs/WHY.tr.md + WHY.md): three columns (CelikPanel/cPanel/Plesk) —
  install footprint, default security, one binary vs a forest of services, TR-first, the price
  principle; plus a reasoned confession of deliberate gaps (every refusal links to a DECISIONS record).
  We do the comparison ourselves, honestly, before competitor marketing does.
- **Light brand customization:** a global "panel name + logo + accent color" setting (one table row;
  login + sidebar + mail templates read one source). Per-reseller branding is post-1.0.

**Exit criterion:** ≥3 external operators run real sites for ≥1 month; documentation answers their
questions before we do · on a clean VPS an admin is created and logged in from the browser without
returning to SSH; a token-less/second attempt is 403 · no account can be opened with an unverified
e-mail; a free account is limited at the N+1st mail in an hour · every panel page has a working doc
link, the broken-slug test is in CI · the beta announcement shipped with the price principle and the
WHY page · SUPPORT.md is live and the v0.3.0→current jump test is green in CI.

### 🎯 v1.0 — General Availability
- Clean VPS → a working panel in minutes, documented, self-updating
- "Domain → live site" 100 times in a row without error (the Phase 1 promise, now in CI)
- Migration from cPanel proven repeatedly · pricing/license published

**Exit criterion:** a stranger with no help beyond the documentation reaches, on a clean VPS, from
install to a live HTTPS site and mail landing in the INBOX, on their own · "domain → live site" 100/100
in CI · at least one real cPanel migration and one CelikPanel→CelikPanel move proven in production ·
the pricing/license page and the SUPPORT policy are published.

### Post-1.0 — horizon *(demand persists, fantasies don't)*
Each only with real demand, in keeping with the deliberate non-goals:
- Multi-server (an inter-panel trust model + **sibling-server DNS auto-registration**: a new CelikPanel
  server registers its own hostname A record with the other CelikPanel that is the zone authority for the
  brand domain, using an operator-provided API token — the productized form of today's manual
  boston.celikhost.com step; v0.3's path (b) external-DNS covers N servers until this lands) · a BSD
  agent backend · billing integrations (WHMCS etc.)
- **Plesk/DirectAdmin importers** — only once real demand exists (≥5 concrete migration requests); the
  cPanel importer's inspect→confirm→apply pattern is reused. Until then the honest answer is documented:
  "Coming from Plesk? A manual migration guide, for now."
- **Per-reseller white-label** (custom login domain, reseller logo) — light global branding is in
  v0.6–0.9; per-reseller only if reseller sales demand it.
- **A hosted service** (CelikPanel itself renting VPS+panel) — not rejected outright like the
  marketplace, but not designed now; if it opens, it carries not one line of telemetry/phone-home into
  the self-hosted product's architecture.
- A marketplace **never** (the AltaVista mistake).

---

## Deliberate Non-Goals

Simplicity is being able to say no. These are absent **on purpose** — and most of the refusals are the
product itself:

- ❌ **A Docker/container layer** — the target market is classic hosting; native is correct. It is also
  the competitive sentence: a Plesk install brings hundreds of packages and dozens of services;
  CelikPanel brings two binaries. That difference is not a gap — it is the product.
- ❌ **A management screen for every conceivable service** — what isn't installed is invisible.
- ❌ **Theme/skin markets, portal showcases, a plugin marketplace** — the AltaVista mistake; moreover the
  no-third-party-code guarantee is the natural consequence of "only the Panel reaches the root Agent."
  A plugin market sells that guarantee away.
- ❌ **The general app-catalog race (Softaculous's 400+ entries)** — a shallow-wide catalog is a
  maintenance black hole and demand is overwhelmingly WordPress. The catalog grows one entry at a time
  only when (a) proof of real demand and (b) WordPress-quality end-to-end install (official tarball,
  verification, full configuration) hold **together**. Position: deeper than anyone on WordPress,
  deliberately narrow on the catalog.
- ❌ **Full visitor analytics** (sessions, geo, real-time) — the minimal log card in v0.4 suffices; for
  more, the honest answer: put Plausible/GA on the site. An AWStats clone fails the simplicity filter.
- ❌ **An in-panel terminal** — attack surface and simplicity; SFTP/SSH key management (v0.5) serves the
  legitimate need.
- ❌ **Multi-server / cluster (for now)** — no distributed-system dreams before one server is flawless.
- ❌ **External dependencies for the panel itself** (Redis, external DB, message queue) — one binary +
  SQLite stays. Even payments are not embedded in the core; the panel is only a webhook consumer (the
  v0.5 decision).
- ❌ **Telemetry / phone-home** — the panel makes no outbound request of its own volition (the
  operator-configured heartbeat and inbound webhooks excepted). Whatever the license model, this does
  not change.
- ❌ **BSD support (for now)** — **but the option is deliberately preserved, and never as a fork.** The
  panel↔agent RPC contract is OS-neutral by design: the panel (HTTP/SQLite/UI/business logic) is
  portable Go that already cross-compiles to FreeBSD; only the agent's "hands" (systemd/apt/nftables)
  are Linux-specific. If real demand arises (e.g. a Linux trust crisis pushing hosts to BSD), the move
  is a BSD agent backend behind the same RPC surface — work measured in weeks, one product. There will
  never be two CelikPanels. The discipline that keeps it cheap: new agent features keep the "what" (RPC
  surface) separate from the "how" (exec calls) — the code is already written that way.
  *(Decision: July 8, 2026.)*

---

## Historical Snapshot — August 29, 2026

**Version:** now single-sourced — version and commit are linked into BOTH binaries, served from
`/api/v1/panel/version`, read back by the panel footer, and a panel/agent build mismatch raises a
warning. The hand-typed "v0.1.0" literal is gone. Both test servers verified on the same commit.
**Current live identity:** the earlier two-server matching-commit observation is
historical evidence, not proof of either server's current state. Reverify live
values in [the dated live-state record](docs/LIVE-STATE-2026-08-29.md).
**Position on the ladder:** v0.2 in progress; many v0.2.5 items closed along the way; **a new v0.2.6
step now sits in front of v0.3** because of the three data-loss defects above.

**Live breakages closed in the second half of July (all proven on both servers):** the spam filter
actually filters now (single-owner milter chain; a GTUBE test was rejected on both distros) · Postfix
on Arch was rejecting EVERY incoming message (the lookup-table type is now discovered:
lmdb/hash/btree/texthash) · WireGuard actually works after install · the agent no longer trusts
request-supplied paths or URLs (path allow-list + symlink refusal; the repo key comes from its own
catalogue) · the config editor writes → validates → **rolls back** (nginx/postfix/dovecot/apache
validators) · seat conflicts are enforced in the agent (the UI blocked them, the API did not) · an
alias unit no longer counts as installed · the update script no longer installs a stale build.

**New capabilities:** a **Help** button on every management page (bespoke content for 21 components
plus three generic fallbacks by kind, all bilingual; a page without help is structurally impossible) ·
the ban on empty management pages (D-019) · the distro support matrix is **generated from the
catalogue** and a guard test fails when it goes stale · a "not offered on this distro" badge · the
monitoring page · Valkey added to the catalogue (the real cost of a new component: 2 source files,
zero Go code).

**Debt status:** the generated OpenAPI client, one declarative route/authz table, UI consolidation and
measured end-to-end latency remain open. D-017 defines canonical offering identities, but persisted Store
rows still use plain legacy IDs; adoption requires an explicit data/API compatibility migration. CI is no
longer compile-only: it covers Go formatting/build/vet/test/race, shell and repository contracts, the
locked web build and dependency gate, and reproducible release artifacts.

**Repository snapshot:** run `bash tools/repo-metrics.sh` against the exact
commit whenever a measurement is needed. The output is generated source-tree
evidence and is intentionally not copied into this roadmap as hand-maintained
line, file, route, migration or command-site counts. Record the command output
with its full commit in review or release evidence. It never proves either
server's deployed build.


---

## How This Document Is Updated

- **Every significant decision lands here via a commit.** The reasoning goes to
  [DECISIONS](docs/DECISIONS.md), the debt to [AUTOPSY](docs/AUTOPSY.md); this file records only the
  place on the ladder and the criterion.
- A new request **cannot enter as "later"**: it is either written onto a version step with a measurable
  exit criterion, or added to Deliberate Non-Goals with a reason. There is no third way.
- The next step does not start before the exit criterion is met; if a criterion must change, the change
  is committed together with its reason (no silent dilution).
- Every edit refreshes the "Last updated" date and the "Where We Are" section; the Turkish original
  (ROADMAP.tr.md) and this mirror update in the same commit.
