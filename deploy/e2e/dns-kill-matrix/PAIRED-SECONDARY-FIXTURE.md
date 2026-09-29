# Paired-secondary fixture: panel-free native primary peer

*Design note, 2026-09-29. Groundwork for register rows 3, 5 and 17 of
[DNS-RECOVERY-ACCEPTANCE.md](../../../docs/DNS-RECOVERY-ACCEPTANCE.md) (item 2).
Nothing here is native evidence: no guest was started, nothing ran with
`--execute`, and no row changes state because of this note.*

## Why

Item 1 closed with rows 3 and 5 (fresh paired secondary, BIND and PowerDNS)
open because they could not be prepared: no peer script played a
panel-free native *primary*, and the trigger refused the fresh PowerDNS
paired-secondary manifest as the legacy reconfiguration shape. This note
records what now exists for both, what the harness owner still has to change
in `guest_bootstrap.py`, `guest_bootstrap.sh` and `run_cell.py`, and the pass
definition each cell needs before it can count.

Per D-022 the primary side uses only native packages, native configuration and
standard AXFR/NOTIFY. The guest under test never needs a remote CelikPanel or
its HTTPS API; the Panel's own secondary precheck is a single authoritative
catalog SOA query (`cmd/panel/server_setup_secondary_prerequisite.go:21-44`).

## Product facts the fixture has to meet

| Fact | Source | Consequence for the peer |
|---|---|---|
| A fresh BIND secondary reads the peer catalog before intent with the **BIND producer policy** | `cmd/agent/dns_engine_host.go:1405-1413` (`probeDNSCatalogAXFR`) | Every catalog record TTL 60; member owner `<sha224hex(member)>.zones.<catalog>` |
| A fresh PowerDNS secondary does the same | `cmd/agent/dns_engine_pdns_catalog.go:35-51` (`peerPDNSCatalog` → `probeDNSCatalogAXFR`), called from `dns_engine_pdns_switch.go:1654-1659` | Same format for both secondary engines |
| The reader is strict | `cmd/agent/dns_catalog_axfr.go:395-606` | Flags exactly QR\|AA; one question in the first message; no authority/additional; SOA `invalid. invalid.` with timers 60/30/3600/30 and serial > 0; exactly one apex NS `invalid.`; exactly one `version` TXT `"2"`; PTR members only; APL and every other type refused |
| The PowerDNS policy (TTL 0, 32-char base32hex labels) is selected only for a panel-managed PowerDNS primary reading **its own** producer | `dns_catalog_axfr.go:100-104,117-125`, `dns_engine_pdns_catalog.go:1044` | A native PowerDNS `PRODUCER` catalog is **refused** by a CelikPanel secondary. The PowerDNS flavour therefore serves the catalog as an ordinary `MASTER` zone with explicit rows equal to `binddns.CatalogZoneRecords` |
| Some secondary proofs AXFR from the guest's own pair address | `dns_engine_pdns_switch.go:975-989` (`probeDNSBoundCatalogAXFR` from `LocalIP`), `dns_catalog_axfr.go:226-247` | AXFR must be allowed from the guest's `192.0.2.x` address |
| Fresh PowerDNS paired secondary runs the V1 journal on certified Debian APT, retrieves catalog and members **before** `target-started` | `dns_engine_pdns_switch.go:2018-2023` (`retrievePDNSPairSecondaryZones`, `dns_engine_pdns_catalog.go:220-259`) | The primary must be serving and NOTIFY/AXFR-reachable before the operation starts |
| Zone-sync V3 recovery refuses secondaries | `cmd/agent/dns_engine_secondary_write_guard_test.go:201,405` | Row 17 is cut on a CelikPanel **primary**, not with this peer |

The catalog text is a byte-for-byte mirror of
`internal/binddns/pairing.go:163-196` (`renderCatalogZone`); the PowerDNS
rows mirror `pairing.go:97-143` (`CatalogZoneRecords`). The offline tests pin
both to the Go golden vectors (`pairing_test.go` empty catalog text and the
`example.test` SHA-224 label).

## What was built

| File | Role |
|---|---|
| `native_primary_peer.py` | Controller-side provisioner. `prepare` / `observe`, `--engine {bind,pdns}`, dry-run by default, `--execute` to act. Same argument style and guest-marker/absence checks as `native_bind_peer.py` and `native_pdns_bind_peer.py`. |
| `native_primary_peer_probe.py` | Peer-side, read-only, stdlib-only probe (copied to the peer and removed after the run). Contains a port of the Agent's catalog AXFR reader. |
| `test_native_primary_peer.py` | 22 offline tests (see below). |

What `prepare` does, only after the guest marker names **the peer node** of
the selected cell and the peer has no `/opt/celikpanel/bin/{agent,panel}`, no
active `named`/`bind9`/`pdns` unit and no leftover fixture file or database:

- **BIND**: installs `bind9` (Debian, package default masked during install)
  or `bind` (Arch); writes a native `named.conf` with two `type primary` zones
  (the catalog and `s1-kill.test`) from zone files; `named-checkconf`,
  `named-checkzone` for both zones; enables and restarts `named.service`.
- **PowerDNS**: installs `pdns-server pdns-backend-sqlite3` (Debian, masked
  during install) or `powerdns` (Arch); writes a native `pdns.conf`
  (`primary=yes`, `secondary=no`, gsqlite3, no include-dir, no API or
  webserver); creates the SQLite database from the packaged schema and seeds
  two `MASTER` domains with `notified_serial` NULL so PowerDNS notifies on
  start; enables and restarts `pdns.service`.
- Both: AXFR allowed only to the guest under test and the peer's own loopback
  (the probe); NOTIFY only to the guest (`notify explicit` + `also-notify`;
  `also-notify` + `only-notify=<guest>/32`). Listens on `127.0.0.1` and the
  peer address only. The member zone is exactly what
  `guest_bootstrap.bind_scenario(role="paired-primary")` publishes for the
  node playing primary: SOA `ns1.s1-kill.test hostmaster.s1-kill.test
  2026083101 10800 3600 604800 3600`, NS ns1/ns2, `ns1` and `www` A → primary,
  `ns2` A → guest. Catalog serial 1, member set `{s1-kill.test}`.

`observe` copies the probe, runs it once with `sudo`, deletes it, and checks
the JSON identity. The probe (read-only) requires the engine unit active and
the other engine inactive and no management binaries; AXFRs the catalog from
loopback and parses it with the ported Agent rules; checks authoritative UDP
and TCP SOA for each member and the catalog SOA; reads `www` A; filters
`journalctl -b -u <unit>` to transfer/NOTIFY lines for the catalog and
members; reports `transfers_to_secondary` per zone and SHA-256 of the native
configuration and zone files (owner-change detection). With
`--require-secondary-transfer` it fails unless the log names an outgoing
transfer of both the catalog and the member to the guest.

Usage (after both guests pass `wait-ssh`, **before** preparing the guest):

```sh
python3 deploy/e2e/dns-kill-matrix/native_primary_peer.py prepare --engine bind \
  --work-root "$ROOT" --cell-id "$CELL" --identity-file "$HOME/.ssh/id_ed25519" --execute
python3 deploy/e2e/dns-kill-matrix/native_primary_peer.py observe --engine bind \
  --work-root "$ROOT" --cell-id "$CELL" --identity-file "$HOME/.ssh/id_ed25519" \
  --require-secondary-transfer --execute
```

Accepted cells: role `paired-secondary`, driver `bind` or `pdns-switch`,
placement policy `driver-specific` or `uninitialized-permitted-noncritical`;
`pdns-switch` requires the guest on Debian 13. The peer is always
`placement.dns_peer_host`; addresses must be Debian `192.0.2.10`, Arch
`192.0.2.11`. Both primary flavours are accepted for either secondary engine.

Offline tests (WSL `CelikPanel-S2-Debian`, Python 3.13.5): 22 passed. They
cover the Go golden vectors; zone text and PowerDNS seed rows turned into DNS
wire messages (single and split transfers, name compression) accepted by the
ported reader; a native PowerDNS `PRODUCER` catalog refused by the BIND policy
and accepted by the PowerDNS policy (parity with
`TestPowerDNSCatalogAXFRProducerIsExplicitAndBounded`); the Go negative cases
(serial, timers, MNAME/RNAME, NS target, quoted version, TTL, class, hash
label, APL, duplicates, missing records, AA/RD/question/authority/additional);
member zone equality with `bind_scenario`; config address sets and
transfer/NOTIFY restriction for both engines on both OSes; all 56 supported
manifest cells resolving the opposite guest; refusal of paired-primary,
standalone, reconfiguration, managed-source and misplaced cells; foreign or
guest-side marker refusing before any mutation; dry-run printing every step
without executing.

**Not verified natively:** package installation, `named-checkzone` on the
catalog, PowerDNS serving `invalid.` SOA/NS from SQL rows with AA set on AXFR,
PowerDNS log format (`AXFR-out zone '…', client '…'`) at `loglevel=6`, and
NOTIFY on start. The first native run must confirm each and retain the probe
output.

## Trigger: fresh install versus legacy reconfiguration

**Product answer.** The product distinguishes a fresh paired-secondary
PowerDNS install from the legacy secondary reconfiguration by **live host
state, not by the manifest**. The manifests are identical.

- Panel action selection, `cmd/panel/dns_engine.go:1062-1105`
  (`dnsEngineAction`): `install` when PowerDNS is not installed (1070-1071);
  `reconfigure` only when PowerDNS is installed, running and managed, no
  engine is active, topology is paired and `Revision > 0` (1086-1100);
  `adopt` for the same runtime at revision 0. `reconfigure` is further
  limited to secondary, zero zones (1352-1362).
- Both `install` and `reconfigure` become mode `switch`
  (`cmd/panel/dns_engine_post_commit.go:46-55`) and the same manifest from
  `buildDNSEngineManifest` (`dns_engine.go:1532-1706`): source `""`/0, target
  PowerDNS epoch 1, paired secondary, zero zones.
- Agent classification, `cmd/agent/dns_engine_pdns_switch.go:81-106`
  (`classifyPDNSPairSecondarySource`) called from `dns_engine_host.go:2006`:
  no state receipt and no BIND required; `pdns.service` active →
  reconfiguration (then managed-config, sole-authority, unsigned and
  empty-database proofs), inactive → fresh. The kill-matrix fault driver
  follows it: `pdns-switch` for fresh, `pdns-secondary-reconfigure` for
  reconfiguration (`dns_engine_pdns_switch.go:1640-1644`). Recovery repeats
  the distinction from the journal's target-unit preimage
  (`cmd/agent/dns_engine_recovery.go:619-625`).
- The product **can** start a fresh paired-secondary PowerDNS install today:
  server setup (`cmd/panel/setup_dns_operations.go:167-212`: no DNS
  runtime installed, secondary precheck at 172, action must be `install` at 209) and
  the DNS card with PowerDNS absent. The blockers do not stop it:
  `pdns_primary_switch_paused` is primary-only and
  `bind_source_pdns_switch_unsupported` needs an active BIND source
  (`dns_engine.go:1219-1233`).

So the fresh install is not the reconfigure path; the matrix's
`pdns-switch__*__paired-secondary__*` cells are exactly it (with D-026 the
managed-BIND source is refused for every topology).

**Trigger change** (`cmd/dns-kill-matrix-trigger/main.go`):
`validateDriverManifest` now admits the reconfiguration-shaped manifest under
`pdns-switch` only through `freshPDNSPairSecondaryInstall`: provenance
`uninitialized`, the exact zero-zone epoch-1 paired-secondary shape, revision
0. An `uninitialized` paired-secondary manifest that is not exactly that shape
is refused. Unchanged: `legacy-pdns-secondary` stays exclusive to
`pdns-secondary-reconfigure`; `uninitialized` is refused under that driver;
the identity receipt binds driver and fixture, so a retry cannot swap them.
New tests in `fresh_pdns_secondary_test.go`; `go vet`, `gofmt -l` clean and
the whole package passes in WSL.

Note: the Panel carries its own revision (normally ≥ 1 after the DNS identity
is staged) in `source_revision`; the harness `uninitialized` contract keeps 0.
The Agent does not classify on the revision, but it is part of the manifest
qualifier, so harness and Panel requests are not byte-identical.

## Integration steps for the harness owner

The files below are outside this change. In order:

### (a) Fresh paired-secondary BIND (row 3)

1. `guest_bootstrap.py`
   - `validate_bind_cell`: accept `role == "paired-secondary"` with
     `source_fixture == "uninitialized"` on either node for policies
     `driver-specific` / `uninitialized-permitted-noncritical`. Start with
     `EARLY_UNINITIALIZED_PHASES | {"target-verified"}` (as for standalone);
     `committed` and `rolling-back` need their own pass definitions first.
     Keep `managed-pdns-required` refused (no managed PowerDNS secondary
     source producer).
   - `bind_scenario(role="paired-secondary", node=…)`: topology `paired`,
     `pair_role` `secondary`, `local_ip` guest, `local_ns`
     `ns2.s1-kill.test`, `peer_ip` peer, `peer_ns` `ns1.s1-kill.test`
     (the Panel's mapping, `cmd/panel/dns_engine.go:1498-1502`),
     **`zones: []`** (a secondary holds no local live zones,
     `dns_engine.go:1690-1696`).
   - Document the order: `native_primary_peer.py prepare` first, then the
     guest's `install`, `prepare-bind`, `run-prepared`.
2. `guest_bootstrap.sh`: the uninitialized source proof must accept the
   paired-secondary scenario (zero zones, secondary identity) and record, read
   only, that the peer catalog SOA is authoritative from the guest before
   launch. `dns_address` stays the guest's peer address; the expected
   `www.s1-kill.test` A is the **primary's** address.
3. `run_cell.py`: `validate_source_scenario` already accepts the role; check
   that no path assumes a non-empty zone list or a local A answer. Run
   `native_primary_peer.py observe` before the kill and after recovery and
   retain both outputs.
4. **Pass definition** (peer-reachable first): exit 137 at the named
   boundary with a V1 journal under fault driver `bind` and `pair_role`
   `secondary`; after the Agent restart the **same request** converges
   (forward, or rollback for the pre-start cuts where the Agent chooses it)
   with no second operation; the secondary answers `s1-kill.test` SOA and
   `www` A authoritatively over UDP and TCP with the primary's serial and
   address; the probe shows the catalog and member transferred to the guest;
   the peer's config digests are unchanged; reboot of the guest with
   `celikpanel-agent`/`celikpanel-panel` disabled keeps it answering. The
   unreachable variants need a pass definition derived from the paired
   `Reconcile` behaviour first; do not run them on a guess.

### (b) Fresh paired-secondary PowerDNS (row 5)

1. `guest_bootstrap.py`
   - `validate_pdns_switch_cell`: accept `paired-secondary` on `debian13`
     with `source_fixture == "uninitialized"` only. Enable phases only after
     checking that the paired-secondary V1 path writes each one (it shares
     `switchToPDNSOnCertifiedProfile` with the fresh standalone path, which
     writes all nine); rollback phases use the tagged `target-staged`
     precursor (`dns_engine_kill_matrix_linux.go:386-391`).
   - `pdns_switch_scenario(role="paired-secondary", source_fixture="uninitialized")`:
     same identity fields as (a), `zones: []`, target epoch 1, revision 0.
     This is accepted by the trigger now.
2. `guest_bootstrap.sh`: the uninitialized PowerDNS source proof must prove
   `pdns.service` **inactive** and no PowerDNS database. If PowerDNS is
   running, the Agent classifies the request as reconfiguration and journals
   under `pdns-secondary-reconfigure`, and the boundary marker will not match.
3. `run_cell.py`: expect fault driver `pdns-switch`, V1 schema, `pair_role`
   `secondary`, target `pdns`, mode `switch`. Retain peer observations as in (a).
4. **Pass definition**: as (a), plus on the guest the SQLite state the
   product writes: one `CONSUMER` row for the peer catalog with `master` =
   primary and account `celikpanel-peer-catalog-v1`
   (`dns_engine_pdns_catalog.go:54-96`), and the member loaded as `SLAVE`
   with `master` = primary and `catalog` = the catalog name.

### (c) One zone-sync interruption cell on a pair (row 17)

Zone add/edit/delete runs on the **primary**, and `RecoverDNSZoneV3`
refuses a secondary, so this cell uses a CelikPanel BIND primary with the
existing panel-free native secondary (`native_bind_peer.py`; for a PowerDNS
consumer `native_pdns_peer.py`), not the new peer. Base: the pair of
`bind__intent__after-write__paired-primary__peer-reachable` brought to a
committed switch without a cut, as in the V3 deletion trial.

1. **Missing product/test hook.** The Agent's kill hook covers only the
   engine-switch journal writer (`dns_engine_kill_matrix_linux.go`). There
   is no tag-gated boundary inside zone-sync V3. It belongs to the Go
   recovery work (not this change): a tagged hook at the V3 ledger phase
   writes (`applied`, `propagation-pending`, `recovering`, `published`;
   `internal/servicemutationledger`, `cmd/agent/dns_zone_sync_v3_phase_contract.go`),
   before- and after-write, with the same marker/ready-fd contract as the
   switch hook. An externally timed SIGKILL is racy and is not a boundary
   proof.
2. **Harness pieces that exist.** `rpc-delete-v3` / `rpc-delete-v3-recover`
   (`zone_delete_trial.go`) and `rpc-pdns-peer-v3` /
   `rpc-pdns-peer-v3-recover` run one exact V3 request and resume only that
   identity; they never issue a second `SyncDNSZoneV3`. Add an `add`/`edit`
   request builder of the same shape, or generalise the delete one.
3. **Manifest.** `manifest.json` has no zone-sync driver. Run it as a named
   trial (like the V3 deletion trial) rather than widening the switch matrix.
4. **Pass definition**: exit 137 at the named V3 boundary; Agent restart;
   the same request recovered through `RecoverDNSZoneV3` to `published`,
   or held `propagation-pending` with its typed code (deletion without owner
   proof), never a second operation; for add/edit both native servers answer
   the new SOA serial and record authoritatively over UDP/TCP; for delete,
   absence proven by `native_bind_peer.py --authoritative-parent` or an
   enrolled inspector, not by REFUSED; the typed owner-edit conflict still
   refuses; both guests rebooted with management disabled keep serving the
   recovered state.

## Harness integration status (2026-09-29)

Steps (a) and (b) above are now implemented in the harness; step (c) is not.
Offline tests only: nothing ran natively and no register row changes state.
See [Fresh paired-secondary cells](README.md#fresh-paired-secondary-cells-rows-3-and-5)
for the commands and the full pass definition.

- `guest_bootstrap.py`: `validate_bind_cell` / `validate_pdns_switch_cell`
  admit `paired-secondary` with `uninitialized` only, peer-reachable only:
  BIND `pre-intent`, `intent`, `target-staged`, `target-verified` (7 cells,
  either kill host); PowerDNS every phase on Debian 13 (17 cells). The
  scenario is `paired_secondary_scenario` (zero zones, ns2 local / ns1 peer).
  `--peer-engine {bind,pdns}` is required for these cells; `prepare-*` runs
  `native_primary_peer.py prepare` and a baseline `observe` before preparing
  the guest, and `run-prepared` observes the peer before the controller and
  with `--require-secondary-transfer` after it, writing `peer-verdict.json`.
- `guest_bootstrap.sh`: exact secondary scenario check, empty-source proof,
  and a read-only authoritative UDP+TCP catalog SOA check against the peer
  from the guest; PowerDNS additionally proves no database exists.
- `run_cell.py`: pre-mutation refusal of unadmitted secondaries, the same
  catalog check live before launch, and the pass definition (target
  convergence; primary serial and `www` A over UDP+TCP from the guest; the
  PowerDNS CONSUMER row and loaded member). Optional reboot of the secondary
  with management disabled (`--disable-management-before-reboot`).
- Unreachable-peer cells remain refused until they have their own pass
  definition.

## Cell IDs

**Covered by the peer (a), BIND paired secondary, 22 cells:**
`bind__pre-intent__paired-secondary__peer-{reachable,unreachable}`, and for
each of `intent`, `target-staged`, `target-verified`, `committed`,
`rolling-back`: `bind__<phase>__{before,after}-write__paired-secondary__peer-{reachable,unreachable}`.
First batch proposed: the eight `pre-intent`/`intent`/`target-staged`/`target-verified`
peer-reachable cells.

**Covered by the peer (b), PowerDNS paired secondary, 34 cells:**
`pdns-switch__pre-intent__paired-secondary__peer-{reachable,unreachable}` and
`pdns-switch__<phase>__{before,after}-write__paired-secondary__peer-{reachable,unreachable}`
for `intent`, `target-staged`, `source-stopped`, `target-started`,
`target-verified`, `committed`, `rolling-back`, `rolled-back`.

**Out of scope here:**

- 12 BIND paired-secondary `managed-pdns-required` cells
  (`source-stopped`, `target-started`, `rolled-back` × edges × reachability):
  they need a managed PowerDNS *secondary* source producer on the guest
  (row 8, PowerDNS → BIND paired).
- 34 `pdns-secondary-reconfigure__*__paired-secondary__*` cells: they need a
  `legacy-pdns-secondary` source producer (running managed PowerDNS,
  revision ≥ 1); still harness-blocked. The peer script refuses them today;
  admitting them later needs only the driver added to `SUPPORTED_DRIVERS`,
  since `readLegacyPDNSPeerCatalogAuthority` reads the same BIND-policy
  catalog.
- `pdns-adopt` and `signed-update-finalize` paired-secondary cells are n/a in
  the manifest.
- Row 17 is not a manifest cell (see (c)).

## PowerDNS paired primary gate (`pdns_primary_switch_paused`, row 6)

Still missing before the gate can open:

1. An **owner-edit cut** on the V3 fresh PowerDNS primary (the typed conflict
   refusing, owner bytes kept).
2. Either an **after-start inverse** or an explicit, recorded
   **forward-only policy** for the post-start cuts (today: owner CLI
   `recover-dns-pdns-fresh-prestart` before start; Agent forward only after).
3. **Acceptance through the public RPC**: Panel setup / DNS card →
   `SwitchDNSEngineV1`, not the tagged native tests.
4. A product gap found while preparing this note: a CelikPanel secondary
   (either engine) reads the peer catalog with the BIND producer policy
   (`dns_engine_host.go:1411`, `dns_engine_pdns_catalog.go:47`), while a
   CelikPanel PowerDNS primary publishes a native `PRODUCER` catalog (only SOA
   and NS stored, `dns_engine_pdns_catalog.go:646,849-870`), whose members
   PowerDNS emits with base32hex labels and TTL 0. By the Go tests
   (`TestPowerDNSCatalogAXFRProducerIsExplicitAndBounded`) such a catalog is
   refused under the BIND policy. So a CelikPanel-to-CelikPanel pair with a
   PowerDNS primary would refuse the secondary install with "paired primary
   catalog is unavailable". Panel-free native secondaries were used in every
   trial so far, so none exposed this. Inferred from source and tests, not
   observed natively; it needs a product decision (secondary parser selection,
   or explicit records on the PowerDNS primary) before opening the gate for
   CelikPanel pairs.
5. Ordinary owner enrollment for parentless peer deletion (open since the
   V3 zone trial).

Harness pieces that exist: `native_pdns_bind_peer.py` (Arch BIND consumer
of a PowerDNS producer, parses with the PowerDNS policy),
`native_pdns_peer.py` / `native_pdns_peer_probe.py` (Arch PowerDNS consumer),
`native_pdns_pdns_pair.py` + `native_pdns_pdns_primary_probe.py`, trigger
`rpc-pdns-peer-v3[-recover]`, the tagged native tests
`TestNativeFreshPDNSPrimaryV3*` (`cmd/agent/dns_engine_pdns_v3_*native_trial_linux_test.go`),
the owner CLI `recover-dns-pdns-fresh-prestart`, and the evidence in
`evidence/pdns-v3-{native,prestart,zone,recorded-status}-20260928/`.
`validate_pdns_switch_cell` still limits a fresh paired primary to `intent`.
Worth checking before reuse: `native_pdns_pdns_primary_probe.py` parses the
product's `PRODUCER` catalog with the default `bind` policy of
`parse_catalog_axfr`, which requires SHA-224 labels; if PowerDNS emits
base32hex labels there (as the Go test fixture says 4.9.17 does), that probe
cannot pass as written. Not run here.
