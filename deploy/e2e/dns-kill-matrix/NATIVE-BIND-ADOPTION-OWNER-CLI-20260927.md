# Running BIND adoption owner CLI - bounded native acceptance, 2026-09-27

**Result:** One bounded independent rollback of an interrupted running-BIND adoption passed on a disposable Debian 13 host. The original owner BIND configuration and static zone were restored while the same named process remained active. This does not close item 1 or P0.4. No installed server was changed.

## Scope and evidence identity

The cell exercised the selected protected command `recover-dns-bind-adoption --request-id 8b14693eaf9461f13a415d5e637b6d91 --lang en` against a pre-existing active owner-managed BIND service. This is rollback of running-BIND adoption: the V2 inverse journal carries `SourceBIND` evidence and excludes `SourcePDNS`. Source proof binds supported Debian static configuration and zone file content/metadata, inventory, and authoritative SOA serials over UDP and TCP. This SOA evidence does not prove the full zone RRset.

| Artifact | SHA-256 |
| --- | --- |
| Production Agent | `174aa2c5805169dca3a26337ed6d15fcb6b631304ae524b34692945c18133a16` |
| Tagged producer | `9f81a0d1d245fd0f77ed0e060f395a20c6fc8cfc12e1b85cb1db968beab53c5c` |
| Selected recovery binary | `c633810a027b8aa591435aeebf00dafc8c3233f73ba896f9ba7577dc269ec59c` |
| Runtime manifest | `74b8ca511b9ececc2761b7291857b6da7f63122a226ffdb36d0d276446c90fbd` |
| Final evidence archive | `aa296895caa0dd73bdaea881e64c7dbd6ab2ab52a3e676cfe0397014e2511707` |

Archive: `/var/tmp/cp-bind-adoption-inverse-20260927/native-owner-bind-inverse.tar.gz`. Supplemental final-stage receipts: `dns-final.json` SHA-256 `162260dfbe2be92942c76aae1c8ceac76dad8e195476283aa2ce41702a4590c5`; `final-readonly-probe.json` SHA-256 `75e1c764cac4adf81a9794b55bce90f942e3212d8f418695dda8ef8f82e9d0d8`. The previous archive is at `/var/tmp/cp-bind-adoption-inverse-20260927/provisional-before-install-receipt-fix/native-owner-bind-inverse.tar.gz`, SHA-256 `6c9dcc30c2edb7c96b43c287efce461b8586170b38116a3272ca4235484d5a05`; it is provisional and excluded from acceptance because it retained an adopted-present BIND installation ownership receipt. The fresh run verified the exact pending receipt state before retry. Artifact hashes are not signed-release provenance.

## Native result

For request `8b14693eaf9461f13a415d5e637b6d91`, the tagged Agent PID 2119 (start identity 64562) received a genuine SIGKILL at `rolling-back/after-write`, after the `target-started` precursor, at 2026-09-27T11:42:19.708105Z. It exited 137 (`kill_proven: true`). The retained V2 journal SHA-256 is `8d6f9a3b4b5ee181e7bc571775761942f9c0697220a7f503879368936c26a610`.

A same-serial owner edit changed the zone A record from `192.0.2.10` to `192.0.2.11` without reload. The protected CLI refused it with exit 3 because the source file differed from frozen evidence. The changed owner file (SHA-256 `cddbfe80f0356f3c70344fe25138b004b76ee97519644a4f089013f4dd3f4d67`), V2 journal, ledger, config files, exact pending install receipt (`8b8c78c24e7ddb2764cda5d6611711f18974788a49843ac2cb089f8fe3a3af28`) and named PID were unchanged across refusal. Named remained active; UDP/TCP authoritative checks still succeeded. The disposable harness restored only its injected edit.

The selected CLI then resumed the same request. An external observer killed it after the durable `rolled-back` checkpoint (journal SHA-256 `ec62449491ecb08e65ad0390f9941886c9cb5287cc9e6429361ca54823a9cdb2`); the CLI exited -9 after 7,228 ms. The same CLI request exited 0, retired the journal and wrote the expected terminal owner-rollback verdict: ledger status `failed`, phase `interrupted`, error `dns_engine_switch_rolled_back_by_owner_recovery`. The v1 ledger schema and v2 switch journal schema did not change in this trial. The terminal job was idle with no active lease.

At terminal recovery, the original owner zone hash was `16bd35accfc6ff7203289ab7a3f5b9803a607319bda4317a01d54b06e9ef159b`. `named` remained active/enabled at PID 2034 with start identity 64454. Agent and Panel were inactive but enabled; no reboot was run. After the durable `rolled-back` checkpoint and immediately before/after terminal retry, the DNS engine state receipt and BIND/PowerDNS engine-ownership and install-ownership receipts were absent. The pending install-ownership receipt remained present and unchanged across the earlier preterminal owner-edit refusal. Separate final UDP/TCP A checks returned AA=true and `192.0.2.10`.

The post-retirement historical retry exited 3, reported current health unknown, and made no effects; ledger and native state were unchanged. The original controller handoff result remains unverified because its matrix assertion expects forward convergence. The separate final read-only probe is a valid observation: it reports `rolled_back_source_active` with `converged=false`, as expected for rollback. Acceptance rests on the protected CLI plus separate native file/process/DNS checks, not the controller handoff.

## Limits

This is one bounded rollback of initial running-BIND adoption for a static default-view owner zone on Debian 13. It does not prove the full zone RRset, continuous-availability SLO, reboot/automatic boot recovery, non-Debian or paired variants, or signed provenance. PowerDNS switch/reinstall inverse paths and broader source/topology/platform coverage remain open. Overall item 1 and P0.4 remain open. Both disposable VMs were stopped and the validated cell overlays removed after evidence retention.

The final supplemental receipts are retained separately: `dns-final.json` SHA-256 `162260dfbe2be92942c76aae1c8ceac76dad8e195476283aa2ce41702a4590c5` and `final-readonly-probe.json` SHA-256 `75e1c764cac4adf81a9794b55bce90f942e3212d8f418695dda8ef8f82e9d0d8`.

## Verification

`go test ./cmd/agent` and `go vet ./cmd/agent` passed. `go test ./cmd/recovery ./internal/dnsenginerecovery` and corresponding `go vet` passed. The DNS kill-matrix Python guest suite passed 63 tests. These checks do not expand the native acceptance scope above.
