# Bound worker and native recovery acceptance

*P0.2 / D-025 invariants 2, 4 and 5. Disposable fixture only.*

This drill connects a genuine Go update worker's observation to the native
snapshot binding and the authenticated recovery reader. Earlier AB/AD drills
used a direct native bootstrap and cannot establish this association.

## Trust and operation boundaries

- `current_worker_baseline.py` installs a fresh unpublished Alpha81 artifact
  using its unchanged installer. The fresh AJ baseline identity is pinned below.
  Its unchanged enrollment helper installs a new fixture public key
  and sequence floor. It refuses an existing installation and repeat intent.
- `worker_fixture_origin.py` generates a separate disposable signing key and
  CA. Only the registered guest trusts that CA and resolves `celikpanel.net`
  to its loopback fixture. The HTTPS server serves five fixed update paths:
  latest version, manifest, signature, archive and archive checksum. It never
  serves keys, licensing or arbitrary files. The private signing key
  stays on the lab host. This tests fixture-trusted signed admission, not the
  production signing or distribution system.
- A distinct committed Alpha82 artifact enters through the existing Agent RPC
  driver, actual `StartSystemUpdate`, the actual Go worker and unchanged signed
  updater. No accepted state, observation, binding, floor or success is written
  by the fault injector.
- `guest_bound_worker.py` verifies the actual worker command, cgroup, executable
  hash/build, complete native snapshot, predecessor/candidate hashes and exact
  request/target/token/snapshot binding before killing that update unit. It
  never signals ordinary Panel/Agent services or edits workload data.
- The optional recovery reboot uses the existing exact native checkpoint and
  a separate host admission based on the genuine driver start receipt. A missed
  checkpoint stays inconclusive; no fabricated direct-bootstrap intent is used.
- Read-only root CLI and authenticated HTTP/browser observations concern the
  same request. A browser operation hint is not mutation authority. Where the
  initiating UI is not exercised, full browser update admission is not claimed.

Only new nonce/DMI/QEMU-registered labs are accepted. No existing owner panel,
production credential or production signing key belongs in this drill.
Installed-panel updates remain owner-initiated through CelikPanel.

## Preparation evidence

2026-09-21 trial AE stopped before update admission: the initial fixture extractor
created package directories as `0700` under its private `0077` umask. The genuine
installer correctly rejected `runtime_unsafe_metadata`. Both guests were stopped;
failed disk/log evidence remains at `/var/tmp/cp-release-drill-20260921-ae`.

The extractor now preserves exact package modes. A real Alpha81 archive check
verified 377 entry modes and 353 file hashes. This is fixture correction evidence,
not native recovery acceptance.

AF then exposed a production contract mismatch: runtime enrollment created the
shared `/usr/libexec/celikpanel` directory as `0700`; the next start-guard step
required `0755`. Its stopped guest and logs are retained separately. The fresh
installer now establishes the shared directory through the strict existing
contract before enrollment. Existing conflicting owner metadata remains unchanged;
private runtime directories remain `0700`. A real Bash-to-Go regression reproduces
the previous failure and verifies absent, existing `0755`, and conflicting `0700`
cases under a real inherited transaction lock. Schema and runtime ABI are unchanged.

AG completed fresh native installation and the real DNS fixture. The fixture RPC
driver then rejected the Alpha82 target before calling update RPC. This proves
preparation, not actual update-worker or recovery acceptance.

AH admitted one genuine update, request `72772a3f867c35ed0118a58930626e3d`.
The worker failed while downloading the missing archive-checksum route with HTTP
404. It did not reach the candidate checkpoint; no injected kill or recovery reboot
occurred. The actual root recovery CLI and authenticated HTTP reader reported
`failed` / `update_failed` for that exact request. The authenticated read returned
HTTP 200; the anonymous read returned 401. These observations prove visibility of
this known failure, not successful recovery. The fixture origin now includes the
fifth checksum route. The host reboot helper also checks terminal update-kill
evidence before waiting for a subordinate recovery handoff that might never exist.

AI admitted request `b679a4a758619ab22c6339439643a6ef`. Although the archives and
binaries were labelled Alpha81 and Alpha82, both packaged release policies still
claimed sequence 80. The installed foundation belonged to a different commit at
that same sequence, so the genuine monotonic preflight correctly refused the
candidate. No injected fault occurred. This was inconsistent fixture release
metadata; neither a successful update nor native rollback is established.

AG, AH and AI guests are stopped. Their registered disks and private evidence are
preserved at `/var/tmp/cp-release-drill-20260921-ag`,
`/var/tmp/cp-release-drill-20260921-ah` and
`/var/tmp/cp-release-drill-20260921-ai`. They are not reused for another start.

## AJ release identities

AJ used `/var/tmp/cp-release-drill-20260921-aj`. Two real Git fixture
commits align the binary/archive version, packaged release policy and bootstrap
version/sequence. They are isolated fixture commits, not production release tags;
the production branch's release policy and production signing trust are unchanged.

| Fixture | Version / policy sequence | Real source commit | Archive SHA-256 |
| --- | --- | --- | --- |
| B, fresh baseline | `v0.1.0-alpha.81` / `81` | `45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00` | `13e1463712915b92ed38a3905ab048bd3ad128f2a0c570b9bcc74161cf7645bf` |
| C, candidate | `v0.1.0-alpha.82` / `82` | `9bdb27473f0341fc1ab47cb72d1269839ca0ddee` | `7c3db8658033dcce0765db5703202157c285460d53d09cdb73ff2e132e78efea` |

B's policy names previous sequence 80 / `v0.1.0-alpha.80`, whose peeled release
commit is `bd14d97efc5cfd19acd70ddf0edb9c6343317e2b`. C names previous sequence
81 / `v0.1.0-alpha.81` and B's exact commit. Both archives are built from these
committed source trees; packaged static bytes are checked against real Git blobs.
The native installer publishes the foundation, and unchanged enrollment publishes
the fixture trust floor. The harness does not repair those records by hand or
weaken same-sequence conflict detection. An automatic rollback may retain a newer
monotonic foundation while restoring the predecessor payload.

## AJ observed native outcome

On 2026-09-21, genuine request `ac1f33778fe5eb107fc212bb96f1907b` reached the
candidate-installed checkpoint with an exact worker/request/target/snapshot/token
binding. The fault injector killed only that verified update unit. Native recovery
reached `payload_restored`; the separately authorized QMP reset interrupted that
recovery once. A different boot ID was observed, and native boot recovery completed
the rollback. At `2026-09-21T17:42:56Z`, the persisted observation was `recovered`
with `terminal_proof=rollback_verified` and `previous_failure=update_failed`.

The root recovery CLI reported `recovering` while the panel HTTP endpoint was
unavailable, both before the reset and during the subsequent boot. Once HTTP
returned, CLI and authenticated HTTP 200 agreed on that exact request and terminal
proof; anonymous HTTP returned 401. The earlier update failure remained recorded.
The observed CLI samples had no `waiting_for` hint. Native waiting-guidance display
is still unproved by AJ.

The saved comparison reports `scoped-checks-passed`: the active transaction descriptor was
absent; Panel and Agent were active with installed and running hashes matching
baseline B; web content and the public TLS fingerprint were restored. Loopback
authoritative A and SOA answers were preserved over UDP and TCP. This is not proof
of external DNS delegation, complete DNS ownership, or uninterrupted hosted
workloads. Final readback retained **both sequence floor 82 and foundation 82 /
commit C**, while the restored running payload was B / Alpha81; the floor was not
restored to 81.

Database acceptance uses the verified bound snapshot. Schema and table inventory
match, and all 61 compared table contents match. The earlier baseline comparison
also matches within that scope. Content equality excludes `audit_logs`,
`metrics_samples`, `sessions` and `sqlite_sequence` which can change with authentication/background writers and did differ. Their
individual counters and rows were not accepted as unchanged. No compared table differed unexpectedly; whole-database
digests differed, and total database equality is not claimed.

The separate real-browser reader check passed EN/TR at widths 1440 and 390,
including reload: four cases, eight saved screenshots, no page errors or horizontal
overflow recorded. It used genuine authenticated native HTTP, with no API mocks.
Only lazy `SystemUpdateOperation` JavaScript requests were aborted. An explicit
local operation hint used the ID and target tuple from the genuine RPC
review/acceptance; this is not proof that the browser initiated the update. These
were post-reboot terminal-reader checks, not a transient boot-wait display test.
Bootstrap/browser certificate trust validation through the tunnel is not claimed.

Both AJ guests are now stopped; registered disks and private evidence remain at
`/var/tmp/cp-release-drill-20260921-aj`. The native evidence directory is
`evidence/debian13` below that root; browser evidence is in repository-local
`.tmp-portal-review/bound-native-browser-aj`. The comparison's 13 input-file
hashes and the browser results' eight screenshot hashes were checked read-only.

| Evidence file | SHA-256 |
| --- | --- |
| `evidence/debian13/bound-native-comparison.json` | `57fe316a572e5e9b1bbd837fb5eb408b057b83b8ce030fdc100c23a7b16f9744` |
| `evidence/debian13/bound-final-floor-foundation.txt` | `5b72ded8b176ef10f1ba8af5b165c4bc7c8b0e801034416a579cc97512a33ce2` |
| `.tmp-portal-review/bound-native-browser-aj/results.json` | `a33a3a2c57a1f07471e21c4f6d030ee0f8ce5829cae62b56ec5c71f6c481d3d5` |

AJ establishes this scoped real-worker → interrupted native recovery → boot
rollback → exact CLI/HTTP/browser terminal-reader association. The full
P0.1/P0.2/P0.3 matrix, other crash boundaries, production release-signature path,
older-release schema transitions, native wait display, browser update-start
admission and independent workload lifecycle requirements remain open.

A sanitized, machine-readable [AJ evidence summary](BOUND-WORKER-AJ.json) records the scoped result, source identities and evidence digests without credentials or private lab identity material.
