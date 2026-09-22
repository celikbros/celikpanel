# Application compatibility with independent mail renewal

P0.3/P0.4/P0.5; owner continuity and versioned evidence. Implementation scope:
current-source release declaration, read-only application admission, atomic
application publication and exact inverse. Production native enrollment is still
separate. Installed-panel updates remain owner initiated in CelikPanel.

## Problem and contract

Historical management Agents may replace the independent Certbot hook when
issuing a mail certificate. A running timer or a newer version label cannot
establish compatibility. Executing an unknown capability flag on an old Agent
is unsafe because its CLI may start the normal daemon.

`internal/agentnativecontract` owns `celikpanel-agent-native-contract/v1`.
`bin/agent-native-contract.json` binds the full source commit, exact Agent SHA-256
and `preserve-independent-v1` policy. The reviewed current-source build producer
emits it immediately after building the normal Agent. It rejects tagged helpers
and arbitrary non-Go inputs. It never runs the inspected binary. Source and
prebuilt packaging carry the same artifact.

The declaration is a release compatibility claim, not a signature or proof that
arbitrary code behaves correctly. Outer authenticated release provenance and
verified snapshot admission remain mandatory. Never manufacture a declaration
for historical or installed bytes to overcome refusal. A missing old declaration
means unverified compatibility, not permission to disable renewal.

## Admission and restoration

`recovery verify-agent-native-contract --bin <candidate>/bin` always verifies the
new candidate. `recovery verify-mail-application --bin <target>/bin` reads current
native ownership and exact target files. Descriptors bind parent/file identity,
bytes and protected metadata. Replaced paths, unexpected links, altered units,
missing helper generations, unknown hooks and mixed native enrollment refuse.

A genuinely absent hook or the exact supported legacy hook permits a historical
baseline only if the fixed native units and timer enablement link are absent.
An independent hook requires a matching Agent declaration. Observation creates
no receipts, does not execute Agents and changes no workload state.

Update admission verifies both candidate and rollback baseline before coordinator
downtime and repeats under exclusion. Rollback verifies the snapshot before
stopping coordinators and again after acquiring the final common mutation lock.
Unsupported rollback preserves the same operation, snapshot and native renewal;
it does not silently downgrade the hook or invent authority.

The bin resource publisher exchanges Panel, Agent and the optional declaration
together. Owner tools remain preserved. The declaration is data (0644), not an
executable. Exact snapshot restoration restores the old declaration or its
absence. Existing snapshots and recovery-material v3 records without a declaration
retain their prior meaning; no evidence is rewritten. New material describes
its declaration as part of the complete target tree. Earlier executors cannot be
assumed to support this addition; normal runtime preparation/promotion precedes
publication. A pending transition is never migrated by editing its evidence.

## Evidence and remaining boundary

Scoped race tests cover canonical evidence, exact binary binding, protected
metadata, late owner replacement, native ownership, CLI refusal and the build
producer. Actual freshly compiled management and renewal-helper binaries exercise
producer admission. Resource publication tests include real process SIGKILL,
same-operation retries, exact inverse with and without a prior declaration,
owner edits, and recovery when the candidate directory is unavailable. Shell
fixtures execute preflight refusal propagation and test lifecycle ordering.

These are component/build/process-interruption proofs. They do not establish
whole native update/automatic rollback, initial production enrollment, power-loss
durability, or all historical producer transitions. Separate native evidence is
required before closing those acceptance items. The preserved-hook management
implementation and independent renewal continue to need their existing tests.

Recorded local checks: `go test -race` passed for agentnativecontract,
recoveryruntime (486.893 s), recoverypublication (12.742 s), cmd/recovery and the
build producer; `go vet` passed for those packages. Real management/helper build
admission passed separately. Prebuilt/source bootstrap, recovery runtime/resource,
installer toolchain and native-contract shell checks passed. These results do not
substitute for the separately scoped native acceptance described above.


[BE native compatibility evidence](../deploy/e2e/release-recovery/AGENT-COMPATIBILITY-BE.md)
now verifies read-only admission beside running independent Debian mail, absent
native enrollment on Arch, and private atomic-resource SIGKILL/inverse cases on
both kernels. This evidence preserves the stated production enrollment and whole
application rollback limits.
