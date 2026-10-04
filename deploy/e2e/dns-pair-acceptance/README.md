# DNS pair product-flow acceptance driver

*Roadmap item 2 (P0.4/P0.5) harness. Written and tested offline on 2026-09-29
against source `66db850c`; updated offline for `6f2fb028` (server-side
PowerDNS-primary plan blocker) and the test-only acceptance license seam; first
native run `evidence/pair1-20260930/` (tested commit `aa6b9380`, every topology
stopped on harness defects); corrected offline on 2026-09-30 from that run (see
"Corrections after pair1"); second native run `evidence/pair2-20260930/`
(product `916e1577`, driver `13213343`) and corrected offline again the same day
(see "Corrections after pair2"). The pair2 corrections have not run natively:
nothing outside `evidence/` is native evidence.*

The kill matrix drives the Agent RPC with a panel-free peer. This driver
exercises what an owner does instead: the customer installer on **both**
servers, the first administrator, the license state, the server-setup wizard
API with DNS identity and topology, the Domains screens' zone lifecycle, the
owner enrollment the product asks for, a management-disabled reboot (D-022)
and a read-only return of management. It uses the same two disposable QEMU
guests as the kill matrix (`../dns-kill-matrix/fixture.py`, imported
unmodified): `debian13` at `192.0.2.10` and `arch` at `192.0.2.11` on the
isolated peer link.

## Current expected outcome (read this first)

| Topology | `--license-mode none` (customer archive) | `owner-key` or `acceptance-fixture` |
|---|---|---|
| any | **blocked-product at `license-*`** (blocker L1) | continues |
| `pdns-primary/bind-secondary` | — | **refused-by-product-gate** at `setup-review-primary`: the server plan carries `pdns_primary_switch_paused` while the gate is closed; the secondary's waiting guidance is still checked |
| `pdns-primary/bind-secondary`, gate open (`916e1577`) | — | pair2 t3: `setup-start-primary` failed inside the product (P-A); with this driver it now stops after `--unknown-state-limit-seconds` (default 300) as `failed`, cause `product`, with an "open-ended unknown" D-024 finding |
| `bind-primary/pdns-secondary` | — | full flow, including owner enrollment with `dns-peer-enroll --engine pdns` if the product asks for it; a build without `secondary_ready` ends `blocked-product` at `pair-ready` |
| `bind/bind` (control) | — | full flow, including owner enrollment with `dns-peer-enroll` if the product asks for it; a build without `secondary_ready` ends `blocked-product` at `pair-ready` |

The last two columns are what the code does, not observed results. An
`acceptance-fixture` run never evidences license behaviour (see below).

**D1, corrected after pair2 (the pair2 specification was wrong for the
primary).** The rule mirrors the product and its web client (Decision D,
2026-09-30): `secondary_ready` is serialized exactly on an active paired
secondary (`cmd/panel/dns_engine.go` ~108 `secondary_ready,omitempty`, filled at
~637-649), "the primary payload has no such key"
(`cmd/panel/dns_engine_test.go` ~2003), and the web client refuses a snapshot
that carries it anywhere else (`web/src/lib/dnsEngineContract.ts` ~383):

| Role | Ready when (`pair_contract.pair_readiness`) |
|---|---|
| primary | `pair_role == "primary"`, `pair_ready === true`, `secondary_ready` **absent**; carrying the key at all (any value) is a contract violation and fails |
| secondary | `pair_role == "secondary"`, `secondary_ready === true`, `pair_ready === false` |

Both also need `active_engine` = the role's engine, `topology: paired`,
`state: ready`, and a snapshot the web client accepts. A build that predates the
field (pair1, `aa6b9380`: no `secondary_ready` on an active paired secondary) is
reported as "this build does not report secondary readiness" with the verdict
`blocked-product`, not as a plain failure, and without waiting (the product
serializes the field on every active paired secondary, so its absence there is
final for that build). pair2's captures (`916e1577`) satisfy the rule on all
three readable Panels (`fixtures/pair2/`).

### Blocker L1: license activation cannot complete offline

Every authenticated Panel request except identity/license/update routes passes
`allowLicensedPanel` (`cmd/panel/middleware.go:172`, `cmd/panel/license.go:226-248`).
A license is only accepted when signed by the compiled key
(`internal/licensing/license.go:33`, used by `cmd/panel/license.go:25-39`) and is
re-verified against `https://celikpanel.net/account/` (`license.go:24`) at least
every `CheckInterval = time.Minute` (`license.go:28`, `Status` at `:168-193`,
synchronous refresh in `AccessStatus` at `:334-337`). Setup start also requires
`CanProvision` (`cmd/panel/server_setup_operations.go:534`). The installer
deliberately does not activate (`install.sh` "Licensing is activated by the
authenticated administrator in the web panel").

No accepted script edits the database to get past this. The only earlier
two-VM wizard run (`docs/validation/server-setup-profiles-20260911/fixtures/setup-dns-profile-drive-v2.py`)
replaced the production panel with a Go **test** daemon
(`cmd/panel/server_setup_profile_vm_test.go:44-111`) that injects a fixture
license in process and inserts the admin row and a session directly; it is
pinned to one old build commit and is not the customer path, so this driver
does not reuse it.

With `--license-mode none` (default) the driver logs in, records what the
locked Panel shows (`403 license_required`, its translated text and actor) and
stops with `blocked-product`. `--license-mode owner-key` activates through the
License screen's endpoint (`POST /api/v1/panel/license {"action":"activate"}`)
with one key per server; it contacts celikpanel.net from the guests, so it
also needs `--allow-license-service`. Only the owner can decide to do that.

### Test-only acceptance fixture license (`--license-mode acceptance-fixture`)

Owner-approved on 2026-09-30: a special build used only for experiments
accepts a fixture license; the customer release does not contain this code and
packaging refuses it; license policy does not change; celikpanel.net is not
contacted. L1 itself is unchanged for customer builds.

- **Build.** `scripts/build-dist.sh --acceptance-license COMMIT` runs the
  ordinary `make dist` (which must pass its own packaging checks), then derives
  a separate archive `celikpanel-<version>-acceptance-license.tar.gz` whose only
  changed binary is `bin/panel`, rebuilt with `-tags acceptance_license` and the
  same flags; the version reads `v0.0.0-pairaccept-acceptance-license.<commit>`
  and the root carries `ACCEPTANCE-LICENSE-BUILD.txt`. The script proves that
  the packaging guard refuses the derived tree before writing `dist.json`
  (`license_mode: acceptance-fixture`, `release: false`).
- **Seam.** `internal/licensing/acceptance_fixture.go` compiles only with the
  tag; ordinary builds compile `acceptance_off.go`, where `licensing.NewServer`
  is exactly `licensing.New` (no fixture, verifier, environment variable or file
  lookup). The tagged panel never contacts the license service and accepts one
  public fixture key (`CPK-acce57f1c7` followed by 54 zeros) only on a guest
  whose root-owned `/etc/celikpanel-dns-kill-matrix` marker (written by
  `fixture.py`) names the fixture schema, a cell and a node; the SMBIOS UUID is
  compared when readable (the panel service user normally cannot read it). The
  fixture receipt is `/var/lib/celikpanel/acceptance-fixture-license.json`,
  bound to machine, cell and node, re-verified locally on the customer schedule
  (one minute). Off a fixture guest it grants nothing.
- **What the owner sees.** `GET /api/v1/panel/license` keeps `state: active`
  (the web accepts no other positive state) and adds `license_kind:
  acceptance_fixture`, `license_label: "ACCEPTANCE FIXTURE — NOT FOR
  PRODUCTION"`, `license_service: "not contacted: acceptance test build"`,
  `acceptance_guest`, `acceptance_cell`, `acceptance_node`; `product` is
  `celikpanel-acceptance-fixture`. The License screen does not render these
  fields yet (a web change is needed for a visible label), so screenshots alone
  cannot tell the builds apart; the panel version and the recorded status can.
- **Driver.** Preflight refuses a customer archive in this mode and a tagged
  archive in any other mode. The license step reads the status, requires the
  label for this cell and node, posts the fixture key once through the License
  screen's endpoint, and records `license-status-before-<role>.json` and
  `license-status-<role>.json`. `result.json` carries `license_mode`,
  `native_evidence_scope` ("License behaviour is NOT evidenced by this run ...")
  and `license_status`.

### Corrections after pair1 (offline, 2026-09-30)

pair1 (`evidence/pair1-20260930/README.md`) never reached the zone lifecycle.
Its three run-copy patches are now part of the driver, and the harness defects
it exposed are corrected. None of this has run natively.

| Item | Change | Where |
|---|---|---|
| D1 pass rule | `pair-ready` and `management-return` apply the owner's rule per role through `pair_contract.pair_readiness`; the snapshot must also be one the web client accepts (`read_engine_snapshot`, mirroring `dnsEngineContract.ts` and the Panel's both-proofs refusal). Any other combination, including an absent field, fails and records the payload (`engine-ready-<role>.json`, `engine-return-<role>.json`). Both Panels are always read. | `pair_contract.py`, `Driver.wait_pair_readiness` |
| Fakes mirror the product | The fake Panel's DNS engine bodies are the captured pair1 bodies (`fixtures/pair1/`), not invented ones; the old fake returned `pair_ready: true` for a secondary, which the product never does. `api_secondary_ready=True` adds D1's field for tests of the later steps; it is an assumption about a later API, stated as such. `test_pair_contract.py` loads the captured payloads and asserts the readers accept them, the negative secondary `pair_ready: true` case, and every other D1 combination. | `fakes.py`, `fixtures/pair1/`, `test_pair_contract.py` |
| H1 run ID | `evidence.make_run_id` is the only producer (`<label>-yyyymmddthhmmssz`, lowercase) and is checked against `RUN_ID_RE`; run labels must be lowercase (`topology.RUN_LABEL_RE`). | `evidence.py`, `topology.py` |
| H2 PowerDNS owner enrollment | The old blocked-product E1 is removed: `dns-peer-enroll` accepts `--engine pdns` and the archive ships `dns-owner-tools/pdns-peer-inspect`. For a PowerDNS secondary every command gets `--engine pdns`; `secondary-install` gets `--inspector .../pdns-peer-inspect` and `--catalog-account`, read read-only from the secondary's PowerDNS `domains.account` of the catalog CONSUMER row (the probe now reports `account`). No single CONSUMER account stops the step before any owner command. | `install_steps.py`, `guest_probe.py`, `Driver._owner_enrollment` |
| H3 owner restart | When `install.sh` prints "RESTART THIS SERVER NOW", the driver records that text, restarts the guest as the owner would through `fixture.reboot_guest` (identity re-checked, new boot ID required), waits for the services and the TLS leaf, and records an owner step (`result.json` `owner_steps`). "A RESTART IS RECOMMENDED" is recorded only. | `install_steps.installer_restart_notice`, `Driver.owner_restart_after_install` |
| Generic errors are not guidance | `err.INTERNAL`, code `INTERNAL`, "internal server error", `setup.planFailed`, `setup.blocker.unknown`, an empty reason and a bare HTTP status fail D-024. A plan or start refusal records what the owner sees (EN/TR) and the `apiErrorText` view, then fails; each non-actionable state becomes a product finding with its payload. | `guidance.py`, `Driver.add_guidance`, `Driver.start_refused` |
| No waiting out a certain failure | A poll stops once the state is identical for 5 reads spanning `--stable-stop-seconds` (default 300; 0 disables) and is not a product-declared in-progress state (a DNS engine switch, a running setup step). The long timeouts apply only while the state keeps changing. | `panel_api.PanelClient.poll`, `pair_contract.engine_settled`/`setup_settled` |
| License service silence | Per Panel, read-only: no journal line may contain `celikpanel.net` or the refusing transport's error text; an acceptance-fixture run fails `collect` otherwise. | `license_service_journal_check` |
| Pending-deletion text for a PowerDNS secondary | When it lacks `--engine pdns` / `--catalog-account`, a D-024 observation is recorded; the run continues. | `Driver.pending_deletion_engine_observation` |

### Corrections after pair2 (offline, 2026-09-30)

pair2 (`evidence/pair2-20260930/README.md`) stopped t1/t2 on the wrong primary
rule (H-A) and t3 inside the product (P-A), and showed two D-024 states the
driver rated actionable (P-B, P-C). None of this has run natively.

| Item | Change | Where |
|---|---|---|
| H-A primary rule | The corrected D1 above; the reader also mirrors `dnsEngineContract.ts` ~383 (a `secondary_ready` outside an active paired secondary is refused). A secondary without the field is `blocked-product` ("this build does not report secondary readiness") and ends the wait at once (`pc.readiness_final`). Stale "this build does not serialize it" texts are gone. | `pair_contract.py`, `Driver.wait_pair_readiness`, `Driver.pair_ready`, `Driver.management_return` |
| Captured pair2 fixtures | `fixtures/pair2/`: engine snapshots of the BIND primary (t1 debian13), BIND secondary (t1 arch) and PowerDNS secondary (t2 debian13); t3's first running read and its first `server_setup_reconciling` read; t1's `license_required` read; the active fixture license status. Minimal, redacted, source path recorded. The fake Panel serves the pair2 bodies by default (`api_secondary_ready=True`) and the pair1 bodies otherwise; a ready paired PowerDNS primary was never captured, so it is derived from the BIND primary. | `fixtures/pair2/`, `fakes.py`, `test_pair_contract.py` |
| P-B time bound on a coded unknown state | A setup state whose error code is in `guidance.UNKNOWN_STATE_CODES` (see below) may last `--unknown-state-limit-seconds` (default 300) continuously; then the poll stops (`stop: unknown-limit`), the payload is written to `open-ended-unknown-<role>.json`, a finding `d024-open-ended-unknown-<step>-<node>` (kind `product`, principle D-024) records the code, the EN/TR title and texts shown, first observation, duration, reads and limit, and the step fails with `cause: product` (`result.json` `overall_cause: product` when that is the only failure cause). Applies to `setup-start-*`, the setup wait inside `pair-ready` and `setup-secondary-before-primary`. Classification is by code, never by wording. | `panel_api.PanelClient.poll` (`bounded`, `bound_seconds`), `Driver.open_ended_unknown` |
| P-B contradictory guidance | The role text deliberately lets the owner start the secondary while the primary's setup is still progressing, so an ordinary `running` or `pending` DNS step is only recorded (step check `peer_start_guidance_observations`, with the DNS step status and code). A D-024 finding `d024-contradictory-peer-start-<step>-<node>` (kind `product`; recorded only, the run continues) is raised only when the shown role guidance instructs starting the secondary against this server while the primary's `dns` step is in a state where the secondary cannot succeed (`guidance.dns_blocking_state`): the step `failed`; a listed unknown state (`UNKNOWN_STATE_CODES`); `waiting` with a code at the DNS step (phase `dns`/`dns_readiness`, or the current step is the `dns` step); a rolled-back result (`server_setup_dns_rolled_back`). Every observation keeps the DNS step status, error code and the blocking reason. Matched by text key: the keys `setupExecutionGuidance` picks for `dns_role === 'primary'` are read from the product's `lib/serverSetupGuidance.ts` at run time and intersected with `guidance.PEER_START_INSTRUCTION_KEYS` (`setup.guide.startSecondary`); a build that no longer shows it records `not-used-by-this-build`. | `guidance.role_guidance_keys`, `guidance.peer_start_keys`, `Driver.check_peer_start_guidance` |
| P-C transient license wait | Every setup read showing `license_required` (error code, or `waiting` at phase `license`; also an HTTP body with that code) is recorded with timestamps and the `GET /api/v1/panel/license` status read immediately after it through the same read-only poll view (`result.json` `license_required_observations`, step check of the same name). If that status is `active`, a finding `d024-license-required-while-active-<step>-<node>` (kind `product`, D-024, "setup reported license required while the license was active") collects the observations; the run is not failed on it and continues. | `Driver._setup_reader`, `Driver.license_required_seen` |
| Texts from the product commit | Every guidance text, the wizard `codeKey` map and the role keys come from `Config.web_src`: the driver's own `web/src` only when its checkout is exactly `--dist-commit` with an unmodified `web/src`; otherwise `--product-web-src <dir>` is required, must contain `i18n/`, `components/ServerSetup.tsx`, `lib/serverSetupGuidance.ts` and a `PRODUCT-COMMIT` marker naming `--dist-commit`. A driver extracted with `git archive` (no repository) always needs it. `build-dist.sh` exports `<dist>/product-web-src/` with the marker and records it in `dist.json`; `run-topology.sh` passes it; `export-product-web-src.sh` adds it to a dist built earlier. `run.json`/`result.json` record the source (`config.web_src`, `web_src_provenance`). | `pair_acceptance.resolve_product_web_src`, `scripts/*.sh` |
| Evidence in the dist | `build-dist.sh` records how many archive entries lie under any `evidence/` directory and under `deploy/e2e/` (entries, files, directories, first 10 paths) in `<dist>/archive-evidence-entries.json` and as counts in `dist.json` `archive_observation`. Observation only; nothing is refused on it (pair2's archive shipped kill-matrix evidence; the product is being fixed separately). | `scripts/build-dist.sh` |

**Unknown-state codes** (`guidance.UNKNOWN_STATE_CODES`, data): currently only
`server_setup_reconciling`, whose wizard text (`setup.guide.confirm`, title
`setup.guide.confirmTitle`) says the result is not confirmed and "Do not start
it again", with no actor and no other next action. Add a code only after
reading what the wizard shows for it; a code is counted only while the setup
status is not `succeeded`/`failed`.

Correction to pair1 P1's wording: for a failed plan, `ServerSetup.tsx`
(`review()`, `if (!response.ok) throw new Error(t('setup.planFailed'))`) shows
`setup.planFailed` ("The plan could not be verified. Check the inputs and try
reviewing it again." / "Plan doğrulanamadı. Bilgileri kontrol edip planı yeniden
inceleyin."), not `err.INTERNAL`; it never reads the body. `err.INTERNAL` is
what `apiErrorText` would show for that body elsewhere. Both are generic; the
driver records both.

### Finding (updated for 6f2fb028): the PowerDNS-primary gate is server-side

Earlier sources refused a paired PowerDNS primary only in the wizard's client
rule. Since 6f2fb028, `POST /api/v1/setup/plan` returns the blocker
`pdns_primary_switch_paused` (`can_start: false`) while the gate is closed, and
`startServerSetupDNS` refuses with the same code before staging any DNS
identity; `ServerSetup.tsx` only maps the code to `setup.pdnsPrimaryPaused` in
its `codeKey` table. The driver submits the plan as the UI does and, when the
plan carries that blocker: reads the wizard's `codeKey` table from the shipped
source at run time (`guidance.wizard_code_keys`; an unmapped code falls back to
the generic `setup.blocker.unknown` and fails D-024), records the exact EN/TR
text under `setup.planBlocked` and the DNS engine card's
`dnsEngine.blocker.pdnsPrimarySwitchPaused` text, then re-reads
`GET /api/v1/setup` and `GET /api/v1/dns/engine` and fails the step if the
refused plan changed the saved draft, revision or status, or any DNS identity
field (`refused_plan_left_nothing`). Otherwise the verdict is
`refused-by-product-gate` and no plan is started. A plan without the blocker
(gate open in a later build) proceeds normally.

## Files

| File | Role |
|---|---|
| `pair_acceptance.py` | CLI (`plan`, `run`) and the step orchestrator (`Driver`) |
| `panel_api.py` | Panel client: session cookie, `Origin` CSRF, pinned TLS leaf, capture-time redaction, read-only polling |
| `pair_contract.py` | DNS engine card contract (web client rules) and the D1 pair readiness rule |
| `guidance.py` | D-024: the UI's own guidance rules ported from `web/src` and resolved through `web/src/i18n` |
| `dns_checks.py` | Pure verdicts over probe output (present/absent/native/catalog) |
| `guest_probe.py` | Read-only guest helper streamed over SSH stdin (stdlib DNS client, `rndc`, PowerDNS SQLite, ledger digest, TLS leaf) |
| `guests.py` | SSH runner bound to the fixture plan; identity re-check before every mutating command; tunnel |
| `install_steps.py` | Command text for `install.sh` staging/credentials and `dns-peer-enroll` |
| `topology.py` | Topologies and OS placement |
| `evidence.py`, `redaction.py` | Create-new evidence tree, `result.json`, `SHA256SUMS`; secret redaction |
| `fakes.py`, `test_*.py`, `fixtures/pair1/`, `fixtures/pair2/` | Offline tests only; the fixtures are minimal copies of captured pair1 and pair2 exchanges |
| `scripts/*.sh` | Host scripts (build, product `web/src` export, prepare, run, teardown, offline tests) |

## Design

**Install path.** The real `install.sh` from a local `make dist` archive,
staged exactly like the accepted S-4 first-admin harness
(`artifacts/s4-first-admin/harness/acceptance.py` `direct_stage`,
`upload_credentials`, `direct_install`): the archive streams over SSH into a
root-only `/var/backups/celikpanel/pair-accept/` chain (the installer refuses
writable ancestors), the first administrator arrives as a root-only
`CELIKPANEL_ADMIN_CREDENTIALS_FILE` that the installer consumes, and a prebuilt
tree without `.git` is not rebuilt. Prerequisites come from the guests' normal
distribution mirrors (`apt`/`pacman -Syu`), as for a customer.
`guest_bootstrap.py install` was not used because it copies binaries and
deliberately never runs the installer (`../dns-kill-matrix/README.md`
"Guest bootstrap"). The admin password is random, held in memory, sent only on
stdin, registered with the redactor and never written.

**Panel access.** A loopback SSH port forward to the guest's `127.0.0.1:2083`.
The self-signed leaf is pinned to the SHA-256 the guest itself reports over
the fixture SSH channel. Unsafe methods send `Origin: https://127.0.0.1:<port>`
as a browser does (`cmd/panel/security.go:49-90`); there is no CSRF token.

**API sequence** (same endpoints as `web/src`):

| Step | Calls |
|---|---|
| login | `POST /api/v1/auth/login`, `GET /api/v1/auth/me`, `GET /api/v1/panel/availability` |
| license | `GET /api/v1/license/access`; locked: `GET /api/v1/setup` → 403; owner-key: `POST /api/v1/panel/license`; acceptance-fixture: `GET /api/v1/panel/license`, `POST` with the fixture key once, both GETs again |
| setup review | `GET /api/v1/setup`, `PUT /api/v1/setup/guidance` (guided), `GET /api/v1/dns/engine`, `PUT /api/v1/setup` (purpose `dns`, `dns_mode` `local`, engine, role, `ns1`/`ns2`/`peer_ns`, `local_ip`/`peer_ip`, `panel_domain`; optional `infrastructure_dns`), `POST /api/v1/setup/plan`; on a gate blocker the read-only `GET /api/v1/setup` and `GET /api/v1/dns/engine` again |
| setup start | `POST /api/v1/setup/start` **once** with a fixed `request_id`, then `GET /api/v1/setup/operation?request_id=` every 3 s (ServerSetup.tsx). The secondary starts only after the primary's `dns` step succeeded, as in the accepted precedent |
| pair ready | `GET /api/v1/dns/engine` on both Panels until D1 holds for the role (with `active_engine`, `topology=paired`, `state=ready`); a stable snapshot that does not satisfy it stops early, a secondary without `secondary_ready` at once; then the setup operation until it settles. A `license_required` setup read is followed by one `GET /api/v1/panel/license` |
| zone add / re-add | `POST /api/v1/domains/create {project_type: dnsonly, ssl_type: none}`, `GET /api/v1/domains`, `GET .../dns/zone`, `GET .../dns/records` |
| record add | `POST /api/v1/domains/{id}/dns/records` |
| record edit | `--edit-method ui-replace` (default): `DELETE .../dns/records?id=` then `POST`, which is all the Domains screen offers; `api-put`: the API-only `PUT .../dns/records` |
| zone delete | `DELETE /api/v1/domains/{id}`; on 202 the read-only `GET .../deletion-status`; for `dns_peer_enrollment_required`: owner enrollment for the secondary's engine, then the **same** `DELETE` exactly once, then read-only status polling |

**No duplicate mutation from polling.** Every poll gets a `ReadOnlyView`
(GET only) and the client refuses any unsafe method while a poll runs
(`PollMutationError`, recorded as `refused-by-driver`). A lost mutation
response is recorded as an unknown outcome and reconciled by reads of the
exact `request_id`; the driver never re-POSTs it.

**Guidance (D-024).** For every distinct waiting/failed/pending state the
driver reconstructs what the UI shows from the same payload: `apiErrorText`
(`err.<code>.<reason>` → `err.<code>` → server message),
`setupExecutionGuidance` (ported rule for rule), `readDomainDeletionOutcome` +
`pendingMessage`, and the wizard selection rule. It records the state class
(verified failure / unmet prerequisite / pending / unknown / progress), code,
reason, the English and Turkish text, the reviewed actor where the code names
one, and whether it is actionable. A blocked, failed or pending state is **not
actionable** when it has no stable machine code, carries only the generic code
`INTERNAL` ("internal server error"), is a bare HTTP status, or when the UI
would show only generic or framing text (`err.INTERNAL`, `setup.planFailed`,
`setup.blocker.unknown`, `setup.guide.unknown`, `domains.deletionPending` for an
empty or unreviewed reason, `setup.guide.failed` alone, an uncoded server
string). That fails the step and adds a product finding (`kind: product`) with
the shown text and the payload. A non-OK plan records `setup.planFailed` (what
the wizard shows) plus the `apiErrorText` view; a non-OK start records
`setup.reconnecting`/`setup.uncertain` (or `setup.conflict` for the review
codes), reconciles the exact request once by a read and stops.

Per-read actionability cannot see a state that is specific but endless or
that contradicts its own verified cause (pair2 P-B, P-C). Three further checks
cover what pair2 showed: a listed unknown code is bounded in time (the
open-ended unknown finding fails the step, cause `product`); a primary telling
the owner to start the secondary while its own DNS step failed, is unknown,
waits at the DNS step or was rolled back is a recorded finding (ordinary
progress is not);
a `license_required` read is paired with the license status read right after it
and recorded as a product finding when that status is active. The last two do
not fail the run.

**Owner enrollment (BIND or PowerDNS secondary).** As an owner would, over SSH
as root, from the `dns-owner-tools/` of the staged release
(`cmd/dns-peer-enroll/README.md`): `primary-prepare`; the public key is written
root-only on the secondary; `secondary-install --inspector
.../bind-peer-inspect` (BIND) or `secondary-install --engine pdns
--catalog-account <account> --inspector .../pdns-peer-inspect` (PowerDNS; the
account is the catalog CONSUMER row's `domains.account`, read read-only from
the secondary's PowerDNS database while the deletion is pending);
`secondary-host-key`, independently compared with the digest of
`/etc/ssh/ssh_host_ed25519_key.pub` read over the fixture's pinned SSH channel
and with the fixture `ssh-known-hosts` entry; `primary-activate`; both status
commands. A PowerDNS secondary gets `--engine pdns` on every command on both
hosts. The full transcript is retained and the enrollment is recorded as an
owner step. A digest mismatch stops before activation. If the pending-deletion
text for a PowerDNS secondary does not name `--engine pdns` or
`--catalog-account`, a D-024 observation is recorded (not a failure; wording is
the owner's decision).

**Independence (D-022).** Ledger digests on both guests, the Panel's view of
engine/pair/domains/setup, then `systemctl disable --now` of Panel and Agent on
both, an orderly reboot of both through `fixture.reboot_guest` (UUID/marker
checked, new boot ID required). After boot: management units inactive, the
engine unit active, the zone served authoritatively by both over UDP/TCP with
equal serials, native state present, ledger digests unchanged. Then
`systemctl enable --now` of Agent and Panel, login, and only reads: D1 must hold
again on both Panels, the Panel must show the same engine, topology, role,
`pair_ready`, `secondary_ready` and domain list as before, and the Agent ledger
digest must not change (a start-up or read that admitted a mutation fails the
step).

## Pass definitions

| Step | Passes when |
|---|---|
| preflight | both QEMU processes of this cell are alive, both guests report the plan's SMBIOS UUID and marker, neither has a CelikPanel layout, the dist archive's digest/root/commit/tree match |
| install-* | staging, credentials upload, `install.sh` (exit 0), `install.complete` present, credentials consumed, Agent and Panel active and enabled, TLS leaf readable; if `install.sh` printed "RESTART THIS SERVER NOW": the owner restart (fixture reboot, new boot ID) is recorded as an owner step and the services and TLS leaf are back |
| login-* | 200, session cookie, `/auth/me` role `admin` |
| license-* | license usable (`can_use_panel`); locked → `blocked-product` with actionable guidance; acceptance-fixture: the status names the fixture label, this cell and node, `state: active` |
| setup-review-* | fresh setup, draft saved, plan HTTP 200 and `can_start`; plan blocker `pdns_primary_switch_paused` → `refused-by-product-gate` with the exact shown text, only if the refused plan left draft and DNS identity unchanged; a non-OK plan or other blockers fail with the shown text, and fail D-024 when that text is generic |
| setup-start-* | one start (a non-OK start fails with the shown text); the plan's `dns` step succeeded; every waiting/failed state actionable; an unchanged non-running state stops the wait early; a listed unknown state lasting `--unknown-state-limit-seconds` fails with cause `product` (open-ended unknown) |
| pair-ready | D1 on both Panels, from the snapshot the web client accepts: **primary** `active_engine` = its engine, `topology: paired`, `state: ready`, `pair_role: primary`, `pair_ready === true`, `secondary_ready` absent (present with any value: contract violation, fails); **secondary** the same with `pair_role: secondary`, `secondary_ready === true`, `pair_ready === false`. A secondary without `secondary_ready` and nothing else wrong: `blocked-product` ("this build does not report secondary readiness"), unless another check failed. Anything else fails with the payload; a snapshot identical for the stable window fails early. Then setup settles as succeeded or waiting at a non-DNS phase (public hostname/certificate); no DNS/firewall/service step failed; an open-ended unknown fails with cause `product` |
| zone-add / zone-readd | domain listed with a zone; from both guests, both servers answer SOA and NS authoritatively over UDP and TCP with one serial; the Panel's A/AAAA records are served; native state present on both; catalog lists the member. Re-add: new domain ID and no old record |
| record-add / record-edit | publication 200; the exact A RRset on both servers, both transports; serial advanced |
| zone-delete | a terminal 200 (directly, or after owner enrollment and one retry of the same deletion); the Panel no longer lists it; no authoritative answer from either server; **and** native absence on both (BIND: exact `rndc zonestatus` "no matching zone ... in any view" and no zone file; PowerDNS: no `domains` row and not in `pdns_control list-zones`); **and** the primary's catalog no longer lists the member. DNS REFUSED alone never passes |
| independence-reboot | as described above |
| management-return | as described above; D1 as at `pair-ready` (an unreported `secondary_ready` is `blocked-product`) |
| collect | always runs; journald of Panel/Agent/named/bind9/pdns (all boots), versions, unit state and ledger digest, only for guests whose identity was verified; per Panel the license-service journal check (acceptance-fixture: any line with `celikpanel.net` or "the acceptance test build never contacts the license service" fails the step) |

Overall: any `failed` → `failed`; else any `blocked-product` →
`blocked-product`; else a gate refusal → `refused-by-product-gate` (a valid,
truthful outcome); else a step that never ran → `incomplete`; else `passed`.
For `failed`, `overall_cause` is `product` when every failed step carries
`cause: product` (a verified product defect such as an open-ended unknown),
`unclassified` when none does, `mixed` otherwise; `failure_causes` lists them.
Findings (`kind: product`, principle D-024) never change the overall verdict by
themselves; the open-ended unknown does because it also fails its step.

## Evidence

`<evidence-root>/<run-label>-<UTC timestamp>/`, create-new only:
`run.json`, `steps/NN-<step>/api/NNNN-<method>-<path>.json` (redacted
request/response pairs), `dns-*.json`, `native-*.json`, `catalog-*.json`,
`plan-*.json`, `execution-*.json`, `open-ended-unknown-<role>.json`, `engine-ready-<role>.json`,
`engine-return-<role>.json`, `owner-enrollment-transcript.json`,
`installer-restart-notice.txt`, `owner-restart-after-install.json`,
`ledger-*.json`, `panel-truth-*.json`, `install-sh.txt`, `guests/<node>/…`
(including `license-service-journal-check.json`),
`result.json` (schema `celikpanel/dns-pair-acceptance-result/v1`, per-step
verdict/reason/cause/guidance/checks, `overall_cause`, `failure_causes`,
`findings`, `product_blockers`, `owner_steps`, `license_service_journal_check`,
`license_required_observations`, `native_evidence: false` until
a reviewer says otherwise, `license_mode`, `native_evidence_scope`, and for
acceptance-fixture runs `license_status`) and `SHA256SUMS`. The run directory is
`<label>-<yyyymmddthhmmssz>` (lowercase).
Cookies, CSRF-like headers, passwords, license keys, tokens and private keys are
redacted before capture; a registered secret that survives redaction aborts
the write. `evidence.verify_sums(<dir>)` re-hashes a run.

## Running on the `archlinux` WSL host

All commands are scripts (no inline `$`). From Windows PowerShell:

```powershell
# 0. Offline tests (no guest)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/offline-tests.sh"

# 1. Build web/dist for the commit on Windows first (cd web; npm run build), then the dist archive
#    (also writes <dist>/product-web-src/ and archive-evidence-entries.json):
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh" <commit>

# 2. Guests for one topology and run label (fixture.py, unmodified)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/prepare-guests.sh" bind-primary/pdns-secondary auto r1

# 3. Dry run, then the run (license mode none: expected to stop at blocker L1)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" bind-primary/pdns-secondary auto r1 /var/tmp/cp-pair-accept/dist/<commit>/dist.json

# 4. Copy the evidence directory out, then remove the cell
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/teardown.sh" bind-primary/pdns-secondary auto r1
```

Acceptance fixture license (test-only; the run does not evidence licensing):

```powershell
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh" --acceptance-license <commit>
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" bind/bind debian13 r1 /var/tmp/cp-pair-accept/dist/<commit>-acceptance-license/dist.json --license-mode acceptance-fixture
```

### Re-running the three topologies against a product build `<commit>`

The driver runs from this directory (the scripts' default `CELIKPANEL_REPO` is
the Windows checkout under `/mnt/c`); the product archive comes from
`git archive <commit>` and the guidance texts from that commit's `web/src`
(`<dist>/product-web-src`, passed by `run-topology.sh`). `<commit>` is the full
product commit under test; `<dist>` is
`/var/tmp/cp-pair-accept/dist/<commit>-acceptance-license`. Build `web/dist` for
`<commit>` first (pair2 README "Builds"). `build-dist.sh` refuses to reuse an
existing `<dist>`: for a commit already built before this change (pair2's
`916e1577933f37fb168818242befe02e01ce2122`), run only
`export-product-web-src.sh <commit> <dist>` instead of step 1. Labels must differ from
the cells left on the host (pair1: r1, r2; pair2: p2t1, p2t2, p2t3). Placement
as in pair1/pair2: t1 BIND debian13 / BIND arch, t2 BIND arch / PowerDNS
debian13, t3 PowerDNS debian13 / BIND arch.

```powershell
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/offline-tests.sh"
# 1. product archive and its web/src (or, for an existing <dist>: export-product-web-src.sh <commit> <dist>)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh" --acceptance-license <commit>
# t1 control, BIND/BIND
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/prepare-guests.sh" bind/bind debian13 p3t1
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" bind/bind debian13 p3t1 <dist>/dist.json --license-mode acceptance-fixture
# t2 BIND primary / PowerDNS secondary
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/prepare-guests.sh" bind-primary/pdns-secondary auto p3t2
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" bind-primary/pdns-secondary auto p3t2 <dist>/dist.json --license-mode acceptance-fixture
# t3 PowerDNS primary / BIND secondary
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/prepare-guests.sh" pdns-primary/bind-secondary auto p3t3
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" pdns-primary/bind-secondary auto p3t3 <dist>/dist.json --license-mode acceptance-fixture
```

Copy each run's evidence directory out of `/var/tmp/cp-pair-accept/evidence/`
before `teardown.sh <topology> <placement> <label>`. Expected with the pair2
product (`916e1577`): t1 and t2 pass `pair-ready` (all three captures satisfy
the corrected D1) and continue into the zone lifecycle; t3 stops at
`setup-start-primary` about `--unknown-state-limit-seconds` after the first
`server_setup_reconciling` read, `failed`, cause `product` (P-A remains
unfixed in that build). A build before `secondary_ready` (up to `aa6b9380`)
ends t1/t2 `blocked-product` at `pair-ready`. `--stable-stop-seconds N` changes
the stable window (0 restores waiting for the full `--setup-timeout`);
`--unknown-state-limit-seconds N` changes the unknown-state bound (must be
positive).

Other topologies: `pdns-primary/bind-secondary auto rN` and the control
`bind/bind arch rN` or `bind/bind debian13 rN` (placement is explicit for the
control). Extra driver flags go after `dist.json` in `run-topology.sh`, for
example `--edit-method api-put` or `--infrastructure-dns`. Owner-key licensing
(owner decision only) adds `--license-mode owner-key --allow-license-service
--license-key-file-primary FILE --license-key-file-secondary FILE`; key files
are read once and never written anywhere.

`python3 pair_acceptance.py plan …` prints the cell ID, fixture commands and
the full step/API plan without contacting anything.

## Safety

- Dry-run by default; `run --execute` is required to act.
- Guests are reached only through the fixture plan's loopback SSH ports and
  per-cell known-hosts. Before every mutating guest command the guest must
  report the plan's SMBIOS UUID, schema, cell ID and node (the `fixture.py
  reboot` check). A guest that fails the check is not touched, not even to
  collect logs.
- The cell ID is derived from topology, placement and run label
  (`pair-accept__<engine>-<node>__<engine>-<node>__<label>`); the fixture plan
  must match it.
- No installed server, no release publication, no panel update, no remote
  CelikPanel API, no database edit. Network use beyond loopback is the guests'
  package mirrors (and celikpanel.net only with the explicit owner-key flags).
- `fixture.py fetch` downloads the pinned cloud images on the host; link
  already verified images into the work root instead if they exist.

## What this driver does not prove

- Anything, until a run's evidence is retained and reviewed. It adds no passed
  cell to the kill matrix or the acceptance register.
- License behaviour, in an `acceptance-fixture` run: activation, renewal,
  expiry, rejection and the license service are replaced by the fixture seam.
- Interruption/kill cuts inside zone-sync or setup (row 17), BIND→PowerDNS
  switching (refused by D-026) or PowerDNS primary support (gated).
- Public hostname DNS, the panel certificate and setup completion: on the
  isolated link setup stays waiting at `access_dns`; the driver records that
  guidance and reports it as a finding, not as DNS pair failure.
- Signed-release installation, installed Frankfurt/Boston behaviour, Ubuntu or
  other placements (PowerDNS is Debian-only; the fixture has one Debian and one
  Arch guest), panel/agent removal, or long-running stability.
- That the guidance text is good English/Turkish, or mobile/keyboard UI
  behaviour: the driver checks that a specific translated message exists for a
  stable code, and records the text for human review.
- The actor column is filled only for codes whose owner the product text
  names; other entries say "stated in text".
- The license service's silence beyond the journal. The acceptance seam's
  refused-attempt counter (`licensing.AcceptanceLicenseServiceDialAttempts`)
  lives only inside the Panel process and no API or log exposes it; the driver
  cannot read it and Go was not changed for it. The journal check proves only
  that no line names `celikpanel.net` or the refusing transport's error: an
  attempt whose error was never logged leaves no line.
- The later steps against a real Panel: offline they run on the pair2
  captures, but no run has reached `zone-add` natively. A ready paired PowerDNS
  primary snapshot was never captured (pair1: refused; pair2: failed inside the
  product); the fake derives it from the BIND primary.
- That the unknown-state list is complete: only `server_setup_reconciling` is
  listed; other codes that mean "result not known yet" are not bounded until
  someone reads their wizard text and adds them. The bound is continuous time
  in one listed state as seen by the driver's polls (a failed read neither
  starts nor ends a span).
- That every contradictory text is caught: the check covers one key list for
  the primary (`setup.guide.startSecondary`) and only the blocking DNS states
  listed above. A code that blocks the DNS step without being a failure, a
  listed unknown code, a DNS-step wait or the rolled-back code is not caught;
  ordinary `running`/`pending` reads (pair2 t1/t2 showed the same text there,
  as the product intends) are recorded without a finding.
- The license mechanism behind P-C: the status read after a `license_required`
  read is itself an HTTP request, which by the pair2 reading refreshes the
  verification; the finding shows the contradiction the owner saw, not its
  cause.
- That `--product-web-src` is the product's: the driver checks the marker, not
  the file contents; `build-dist.sh` and `export-product-web-src.sh` write it
  from `git archive <commit>`, never from a working tree.
- The PowerDNS owner enrollment path (H2) natively: pair1 never reached
  `zone-delete`, and `dns-peer-enroll --engine pdns` has component tests only.
- A good stable-state window: 5 minutes is the owner's example. A product
  state that is legitimately unchanged longer (for example a slow catalog
  transfer with an identical snapshot) would be stopped early; the recorded
  `poll` block (`stop`, `identical_reads`, `stable_seconds`) shows when that
  happened.

## Offline tests

`scripts/offline-tests.sh` (126 tests; Python 3.13.5 in WSL
`CelikPanel-S2-Debian`, 2026-09-30): redaction and the
evidence writer (including refusal to write a surviving secret and
`SHA256SUMS` tamper detection); topology/OS placement; the API client against
scripted responses (Origin/cookie handling, TOTP refusal, gate refusal body,
lost responses, poll-mutation refusal); guidance reconstruction for the license
lock, the gate text, a failed operation with and without guidance, the
secondary waiting for its primary, pending deletions with and without a
reviewed reason; the probe's DNS wire parser, a real loopback UDP/TCP/AXFR stub,
`rndc`/PowerDNS native classification and the ledger digest; guest identity
refusal before any command, probe streaming and tunnel arguments; and full step
sequences against a fake Panel/guest world (control pass, API-PUT edit, license
block, owner-key activation, acceptance-fixture activation and evidence scope,
acceptance archive/label refusals, the server-side PowerDNS-primary plan
blocker, a refused plan that leaves changes behind, the gate opened, D-024
failure, ledger change on management return, unverified guest untouched).
Added after pair1: the captured pair1 payloads (`fixtures/pair1/`); a secondary
reporting `pair_ready: true`; a changing state waited for beyond the stable
window; the exact pair1 plan 500 `INTERNAL`; a start 5xx; the installer's
restart notices; PowerDNS- and BIND-secondary enrollment; the license-service
journal check; generic-text guidance; poll stable stop versus change and
in-progress; the probe's `domains.account`; the lowercase run ID and label.
Added after pair2: the pair2 captures (`fixtures/pair2/`) are accepted by the
reader and pass the corrected D1 for the BIND primary and both secondaries; the
pair1 build is `blocked-product` at once (primary passes, secondary "does not
report secondary readiness"); a primary carrying `secondary_ready` (true or
false) is a contract violation; the fakes equal both capture sets; the poll's
unknown-state bound (stops while the value changes; a clear read restarts the
span; a failed read does not); t3's captured reconciling read ends
`setup-start-primary` failed, cause `product`, with the open-ended unknown
finding (EN/TR text, payload file, duration), a configurable limit and a
reconciling state that resolves within it passing; the contradictory
start-secondary finding with its step statuses, and none when the product's
`serverSetupGuidance.ts` no longer shows that key; the contradictory
check's narrowing (ordinary running read and pending step: no finding; t3's
reconciling read, a failed and a rolled-back DNS step: finding; a wait at the
DNS step versus a license wait before it); t1's captured
`license_required` read with an active license (finding, run passes) and with
an unverified one (recorded, no finding); `--product-web-src` resolution
(differing or unknown driver commit, modified `web/src`, marker mismatch or
missing, missing files, same clean commit); `--unknown-state-limit-seconds`
validation; the build script's archive observation run on a synthetic archive
and its `web/src` export and hand-over to `run-topology.sh`.
