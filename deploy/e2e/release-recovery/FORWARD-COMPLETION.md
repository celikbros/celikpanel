# Native forward completion with candidate data missing

*September 15, 2026 · [Türkçe](FORWARD-COMPLETION.tr.md) · D-025 / P0.3*

This record accompanies the [source contract](../../../docs/RECOVERY-FORWARD-COMPLETION.md).
It distinguishes the first candidate from the later terminal-proof correction.
Neither trial closes P0.3 or the full native workload/fault matrix.

## Baseline and fault boundary

Each fresh Arch and Debian 13 QEMU guest installs genuine signed Alpha75
`5aa03fd5b6775b21834ff7b1ce0695d92f50ae93`. Installed and running old binaries are
checked before admission. The installed Agent prepares native BIND records;
A/SOA queries over UDP/TCP, bootstrap TLS, all 65 SQLite tables with WAL-aware
observations, and owner-modified coordinator unit beforeimages are recorded.
The database already has migrations 1–42. This proves readiness inspection, not
a new database-version migration. No customer server is a target.

The local candidate is built from its recorded source with Go 1.26.5. It is an
unsigned local archive, not signed Agent/UI admission. The registered controller
uses exact VM UUID/nonce/DMI, host process, operation, worker invocation, PID,
start time, cgroup, executable and native flock guards. There is one admitted
update per guest. The native worker has the product's OnFailure recovery
relationship; the test never starts recovery or runs owner rollback itself.

At `completion.pending`, an updater journal hint only locates the observation
window. Under an exact worker freeze, the test verifies the complete snapshot,
selected runtime, material v2 and independent strict database checker. It then
quarantines exactly three retained candidate files: `rollback.sh`, `bin/agent`
and `web/dist/index.html`. Originals and their hashes remain in the fixture's
quarantine; installed files, snapshot and recovery kit are not modified.
The exact updater cgroup receives one SIGKILL. A temporary loopback 2083 listener
only widens the checkpoint window and is released; it is not recovery proof.

## Trial O: first source, before the final proof correction

Candidate `c500908739a00540fcc4a3da64a1264b8dcdc27c`, tree
`a7a2af338a850ff5e31e0d3b6af258bce5aa1fa7`, archive SHA-256
`f7f2230029dad527bde299830a0aeba1967da51ec6d6922f38bd6970c2d90187`.
The controller is `c58773d59d0f4ae0d67a6f80d4274068b82f258d`; its later change
only closes the fixture listener when a post-bind observation raises an error.
Private evidence root: `/var/tmp/cp-release-drill-20260915-o/evidence/<node>`.

| Identity | Arch | Debian 13 |
|---|---|---|
| Operation | `131bcd11ab62d0c27ba42c708533c0e8` | `73b223fdad076ff01e6f8d99fe610552` |
| Sealed index SHA-256 | `38140a5ce5c868d31b5dc6e5b53bcc33a5935c4b4f0ac2212255dfa27f3316ff` | `d6d14217ee0be520a58edca358b72c3c05d9a8fc9de8dc170d5960b15b299947` |
| Outcome SHA-256 | `4a9c6a7f6cc16b274a57eab1182c0d358f8febd77fcc972cfcf42d77364d6492` | `da3ab3bf5bd938ab01749bc92c492491e9928b6e90926d3e76a57a491cca6d85` |

Both systems reached the proved database-ready completion checkpoint, lost the
three selected retained files, and killed the exact updater. Native journals tie
the worker's OnFailure event to the same recovery invocation and exact-snapshot
`Previous pending update finalized` message. Earlier timer/lock-busy starts are
recorded separately and do not count as completion. Final independent observation
found the target disk/running hashes, active Panel/Agent, no transaction markers,
129 verified snapshot files, unchanged 16-file material data and selected runtime,
and 293 surviving candidate files from the original 296-file inventory. The
three quarantined files were still absent in the final collection.

Authoritative DNS answers and served bootstrap TLS matched; loopback HTTPS
returned 200. Installed panel web files matched the candidate inventory. Live
coordinator units became the authorized candidate bytes, with native manager
reload state clear. The owner's previous unit bytes were preserved in the
verified snapshot; they were **not** preserved unchanged as live unit files.

All 65 database tables were compared without exclusions. Global comparison stays
**DIFFERENT**, solely in `metrics_samples`. A supplementary guest-side read-only
row comparison found all 18 Arch / 19 Debian snapshot rows unchanged and 15 later
rows added on each guest. The host reviewer verified the program and sealed
aggregate; private raw database rows were not exported for host replay.

Arch's first combined final collector exited 1 after saving snapshot/database
observations; its wrapper did not export the child stderr, so the cause remains
unknown. The original evidence is retained. A subsequent read-only observation
passed when the observer was rerun read-only; no repair or additional update/
recovery command was issued between reads. This is a later point observation,
not proof of uninterrupted availability. Both watcher logs retain
`thaw_exit=1` after SIGKILL; confirmed thaw is not claimed.

Independent host-only review rehashed all 36 Arch and 37 Debian sealed files,
checked the eight-event operation chain and all 26 outcome checks. This is a
scoped **PASS for c500908**. Both guests were stopped through registered guards;
disks and private evidence remain retained.

## Terminal-proof correction and separate acceptance

Source review found that the last full installed proof preceded panel startup.
An extracted real completion tail reproduced a late owner web edit followed by
exit 0 and a completion message. That failing result is retained in
`/var/tmp/cp-terminal-proof-red-xgtd7gr4/runtime-shell-contract.log`.

Commit `0c338822faf7ad3ca450bb4534aecdeccb3f26fd` adds material-v2 terminal
rechecks after controlled starts and before consuming the last durable marker.
Eight real shell-tail cases prove refusal and retained evidence for late payload,
runtime and enablement changes, plus clean completion and scheduler-only paths.
They do not prove native owner-change acceptance. Final shell evidence is in
`/var/tmp/cp-terminal-proof-final-4syjdexe/` and
`/var/tmp/cp-forward-final-shell.5gUb8y78/`.

Trial O cannot establish that changed source. Trial P below uses fresh guests.

## Trial P: corrected source completes through native failure recovery

Candidate and controller are `0c338822faf7ad3ca450bb4534aecdeccb3f26fd`, tree
`a95db6fbe0a670f3d6732d54163a51eceea5e5bb`. Archive SHA-256:
`cc9c9ac5a5172243ede22d2f4c7f3be8ec92e3e08f12255aee36ebea1573afbc`.
Private evidence root: `/var/tmp/cp-release-drill-20260915-p/evidence/<node>`.

| Identity | Arch | Debian 13 |
|---|---|---|
| Operation | `ef61e2b627c1937204818678e5b72e2a` | `1f8eaf6e94a503e1384fbee8eab0c261` |
| Sealed index SHA-256 | `0919fd639cb51c30559fb5f439b0cd7cde9d84bd78a5519cd45259d244df12a1` | `5e5f7fe931701b21d80efd3c07e9d680819d34b8bcb97ebeb4583e40b510b5bb` |
| Outcome SHA-256 | `48134596e1940b117ff3d101a8dc761595cc12a9f0680b4e05f690074f67ac89` | `09b1060a8c8ef83b4904f097c553663618a2bdf72f410ad8bd336eb147e74388` |

Both exact eight-event fault chains reached verified database-ready completion,
quarantined the same three retained files, and killed only the updater. Native
OnFailure recovery finalized the same snapshots at `23:36:38.846819Z` on Arch and
`23:36:48.795966Z` on Debian on September 14 UTC (September 15 in Istanbul).
The final recovery invocations are `a67a647d1e02442fa0e681a1f24b9212` and
`1586fad75a24498bb407d37a5b2078df`, respectively. No owner recovery or second
update was issued.

Final proof verified active target disk/running executables, exact candidate web
files, all 129 snapshot files, unchanged 16-file material, the same selected
runtime `b9cf85f8d6a7d60459af7bd8cef8fe252905a3db8b8bb62b96b9178afdbdc2e6`,
293 surviving retained files and absence of the exact three quarantined files.
All four transaction markers were absent. DNS/TLS point observations matched;
loopback HTTPS returned 200. Unit beforeimage/live-candidate distinctions and
absent/inactive Certbot scheduler limits are the same as trial O.

The all-table comparison again remains **DIFFERENT**, solely in metrics. The
supplementary native comparison retained all 11 Arch / 12 Debian snapshot rows
with zero changed or missing rows, and six later additions per guest. This is
the same aggregate-only evidence boundary, not host raw-row replay.

A temporary host collector initially failed label validation with
`AttributeError: p_collect_local has no attribute re`, before any guest command.
Its trace/history is retained. Importing `re` directly corrected only that host
collector; product, fixture and operation were unchanged. Actual guest snapshot
and final-proof reads returned 0 with stdout/stderr saved separately. The killed
worker's thaw exit 1 remains recorded and is not called confirmed thaw.

The sealed record contains 47 Arch and 48 Debian files and 28 scoped checks per
system. This establishes **scoped forward-completion PASS for 0c33882** under the
fault above. The final marker checks ran on the corrected source; intentional
late owner edits remain covered by the shell regressions, not by this native
trial. Independent host-only review verified every sealed file and operation
chain. Both guests were stopped through their registered guards; disks and
private evidence remain retained.

## Remaining limits

Certbot timers were absent/inactive: active renewal scheduling was not tested.
TLS evidence covers bootstrap self-signed continuity and point HTTPS probes, not
public trust, issuance or renewal. Native peer DNS transfer, hosted website HTTP,
mail, database workloads, browser recovery UI and continuous availability remain
unmeasured here. Only three retained files were removed, not every candidate copy.
No native unchanged-payload/no-op or late owner-edit fault is claimed.

The pre-migration completion window still has no supported independent
continuation/compensation; see the explicit source-contract gap. Incomplete
snapshot capture, new-token historical rollback, recovery interruption/reboot
combinations, metadata transitions, signed admission and evidence cleanup remain
open. No installed user panel update or release publication was performed by
these tests.
