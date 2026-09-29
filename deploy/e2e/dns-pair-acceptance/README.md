# DNS pair product-flow acceptance driver

*Roadmap item 2 (P0.4/P0.5) harness. Written and tested offline on 2026-09-29
against source `66db850c`; updated offline for `6f2fb028` (server-side
PowerDNS-primary plan blocker) and the test-only acceptance license seam. No
guest was started and no run exists: nothing in this directory is native
evidence.*

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
| `bind-primary/pdns-secondary` | — | full flow; if parentless deletion needs peer proof, **blocked-product** at `zone-delete` (blocker E1) |
| `bind/bind` (control) | — | full flow, including owner enrollment with `dns-peer-enroll` if the product asks for it |

The last two columns are what the code does, not observed results. An
`acceptance-fixture` run never evidences license behaviour (see below).

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

### Blocker E1: no owner enrollment for a PowerDNS secondary

When a deletion is pending with `dns_peer_enrollment_required`, the product
text tells the primary's administrator to enroll the inspection channel for the
secondary engine. For PowerDNS there is no owner tool: `cmd/dns-peer-enroll`
installs only the BIND inspector (`main_linux.go:3-4`, `:99-120`),
`internal/pdnspeerenrollment` has a reader and no writer (`enrollment_linux.go:28`,
`:120`; compare `internal/dnspeerenrollment/owner_writer_linux.go:42-44`), and
`make dns-owner-tools` packages only `bind-peer-inspect` (`Makefile:67-71`). The
Agent emits the code at `cmd/agent/dns_engine_peer_pdns_linux.go:57,118`. The
driver records native state on both servers while pending and reports
`blocked-product`; it never fakes a parent zone or writes enrollment files.

Smallest change: an owner writer in `internal/pdnspeerenrollment` mirroring
`dnspeerenrollment/owner_writer_linux.go`, an engine selector on
`dns-peer-enroll secondary-install`/`primary-activate` that installs
`pdns-peer-inspect`, and packaging `pdns-peer-inspect` in `dns-owner-tools`.

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
| `guidance.py` | D-024: the UI's own guidance rules ported from `web/src` and resolved through `web/src/i18n` |
| `dns_checks.py` | Pure verdicts over probe output (present/absent/native/catalog) |
| `guest_probe.py` | Read-only guest helper streamed over SSH stdin (stdlib DNS client, `rndc`, PowerDNS SQLite, ledger digest, TLS leaf) |
| `guests.py` | SSH runner bound to the fixture plan; identity re-check before every mutating command; tunnel |
| `install_steps.py` | Command text for `install.sh` staging/credentials and `dns-peer-enroll` |
| `topology.py` | Topologies and OS placement |
| `evidence.py`, `redaction.py` | Create-new evidence tree, `result.json`, `SHA256SUMS`; secret redaction |
| `fakes.py`, `test_*.py` | Offline tests only |
| `scripts/*.sh` | Host scripts (build, prepare, run, teardown, offline tests) |

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
| pair ready | `GET /api/v1/dns/engine` until `topology=paired`, the expected `pair_role`, `pair_ready=true`, `state=ready`; then the setup operation until it settles |
| zone add / re-add | `POST /api/v1/domains/create {project_type: dnsonly, ssl_type: none}`, `GET /api/v1/domains`, `GET .../dns/zone`, `GET .../dns/records` |
| record add | `POST /api/v1/domains/{id}/dns/records` |
| record edit | `--edit-method ui-replace` (default): `DELETE .../dns/records?id=` then `POST`, which is all the Domains screen offers; `api-put`: the API-only `PUT .../dns/records` |
| zone delete | `DELETE /api/v1/domains/{id}`; on 202 the read-only `GET .../deletion-status`; for `dns_peer_enrollment_required` on a BIND secondary: owner enrollment, then the **same** `DELETE` exactly once, then read-only status polling |

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
one, and whether it is actionable. A blocked or pending state is **not
actionable** when it has no stable machine code, or when the UI would show only
generic or framing text (`setup.guide.unknown`, `domains.deletionPending`,
`setup.guide.failed` alone, an uncoded server string). That fails the step.

**Owner enrollment (BIND secondary).** As an owner would, over SSH as root, from
the `dns-owner-tools/` of the staged release: `primary-prepare`; the public key
is written root-only on the secondary; `secondary-install --inspector
.../bind-peer-inspect`; `secondary-host-key`, independently compared with the
digest of `/etc/ssh/ssh_host_ed25519_key.pub` read over the fixture's pinned
SSH channel and with the fixture `ssh-known-hosts` entry; `primary-activate`;
both status commands. The full transcript is retained. A digest mismatch stops
before activation.

**Independence (D-022).** Ledger digests on both guests, the Panel's view of
engine/pair/domains/setup, then `systemctl disable --now` of Panel and Agent on
both, an orderly reboot of both through `fixture.reboot_guest` (UUID/marker
checked, new boot ID required). After boot: management units inactive, the
engine unit active, the zone served authoritatively by both over UDP/TCP with
equal serials, native state present, ledger digests unchanged. Then
`systemctl enable --now` of Agent and Panel, login, and only reads: the Panel
must show the same engine, topology, role, `pair_ready` and domain list as
before, and the Agent ledger digest must not change (a start-up or read that
admitted a mutation fails the step).

## Pass definitions

| Step | Passes when |
|---|---|
| preflight | both QEMU processes of this cell are alive, both guests report the plan's SMBIOS UUID and marker, neither has a CelikPanel layout, the dist archive's digest/root/commit/tree match |
| install-* | staging, credentials upload, `install.sh` (exit 0), `install.complete` present, credentials consumed, Agent and Panel active and enabled, TLS leaf readable |
| login-* | 200, session cookie, `/auth/me` role `admin` |
| license-* | license usable (`can_use_panel`); locked → `blocked-product` with actionable guidance; acceptance-fixture: the status names the fixture label, this cell and node, `state: active` |
| setup-review-* | fresh setup, draft saved, plan `can_start`; plan blocker `pdns_primary_switch_paused` → `refused-by-product-gate` with the exact shown text, only if the refused plan left draft and DNS identity unchanged |
| setup-start-* | one start; the plan's `dns` step succeeded; every waiting/failed state actionable |
| pair-ready | both Panels show the paired engine ready; setup settles as succeeded or waiting at a non-DNS phase (public hostname/certificate); no DNS/firewall/service step failed |
| zone-add / zone-readd | domain listed with a zone; from both guests, both servers answer SOA and NS authoritatively over UDP and TCP with one serial; the Panel's A/AAAA records are served; native state present on both; catalog lists the member. Re-add: new domain ID and no old record |
| record-add / record-edit | publication 200; the exact A RRset on both servers, both transports; serial advanced |
| zone-delete | a terminal 200 (directly, or after owner enrollment and one retry of the same deletion); the Panel no longer lists it; no authoritative answer from either server; **and** native absence on both (BIND: exact `rndc zonestatus` "no matching zone ... in any view" and no zone file; PowerDNS: no `domains` row and not in `pdns_control list-zones`); **and** the primary's catalog no longer lists the member. DNS REFUSED alone never passes |
| independence-reboot | as described above |
| management-return | as described above |
| collect | always runs; journald of Panel/Agent/named/bind9/pdns (all boots), versions, unit state and ledger digest, only for guests whose identity was verified |

Overall: any `failed` → `failed`; else any `blocked-product` →
`blocked-product`; else a gate refusal → `refused-by-product-gate` (a valid,
truthful outcome); else a step that never ran → `incomplete`; else `passed`.

## Evidence

`<evidence-root>/<run-label>-<UTC timestamp>/`, create-new only:
`run.json`, `steps/NN-<step>/api/NNNN-<method>-<path>.json` (redacted
request/response pairs), `dns-*.json`, `native-*.json`, `catalog-*.json`,
`plan-*.json`, `execution-*.json`, `owner-enrollment-transcript.json`,
`ledger-*.json`, `panel-truth-*.json`, `install-sh.txt`, `guests/<node>/…`,
`result.json` (schema `celikpanel/dns-pair-acceptance-result/v1`, per-step
verdict/reason/guidance/checks, `findings`, `product_blockers`,
`native_evidence: false` until a reviewer says otherwise, `license_mode`,
`native_evidence_scope`, and for acceptance-fixture runs `license_status`) and
`SHA256SUMS`.
Cookies, CSRF-like headers, passwords, license keys, tokens and private keys are
redacted before capture; a registered secret that survives redaction aborts
the write. `evidence.verify_sums(<dir>)` re-hashes a run.

## Running on the `archlinux` WSL host

All commands are scripts (no inline `$`). From Windows PowerShell:

```powershell
# 0. Offline tests (no guest)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/offline-tests.sh"

# 1. Build web/dist for the commit on Windows first (cd web; npm run build), then the dist archive:
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh" 66db850c331bbda0eaef83cb9cfd8f322ba8174c

# 2. Guests for one topology and run label (fixture.py, unmodified)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/prepare-guests.sh" bind-primary/pdns-secondary auto r1

# 3. Dry run, then the run (license mode none: expected to stop at blocker L1)
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" bind-primary/pdns-secondary auto r1 /var/tmp/cp-pair-accept/dist/66db850c331bbda0eaef83cb9cfd8f322ba8174c/dist.json

# 4. Copy the evidence directory out, then remove the cell
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/teardown.sh" bind-primary/pdns-secondary auto r1
```

Acceptance fixture license (test-only; the run does not evidence licensing):

```powershell
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/build-dist.sh" --acceptance-license <commit>
wsl.exe -d archlinux -- bash "/mnt/c/CELIKBROS PROJECTS/celikpanel/deploy/e2e/dns-pair-acceptance/scripts/run-topology.sh" bind/bind debian13 r1 /var/tmp/cp-pair-accept/dist/<commit>-acceptance-license/dist.json --license-mode acceptance-fixture
```

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

## Offline tests

`scripts/offline-tests.sh` (76 tests; Python 3.13 re-run 2026-09-29): redaction and the
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
blocker, a refused plan that leaves changes behind, the gate opened, PowerDNS-secondary
enrollment blocker, BIND-secondary enrollment with exactly one retry, D-024
failure, ledger change on management return, unverified guest untouched).
