# Shared host mutation exclusion

*September 22, 2026 - D-025 invariants 1-4; P0.3/P0.4/P0.5 groundwork*

`internal/hostmutationlock` supplies the actual Agent's comparison-only lock
acquisition, idle observation and inherited-lock proof. The independent recovery
Agent checker builds from those same production sources. The historical
`/run/celikpanel/service-mutation.lock` flock identity and established numeric
UID/GID remain unchanged. The outer host lease is acquired before certificate
publication or ledger publication; this change introduces no new lock order.

The reader pins a nonsymlink parent with openat2, opens the existing file with
O_NONBLOCK, verifies an empty single-link 0600 file and exact owner, and rechecks
the parent and named file against their descriptors after acquiring the flock.
An owner-replaced FIFO cannot hang observation. A changed path, nonempty file,
unsafe mode or owner is refused without rewriting evidence. No weaker syscall
fallback is provided. Legacy/custom symlink paths must be reviewed by the owner;
the canonical /run path requires no migration.

An inherited descriptor must already own an exclusive flock on its exact open
file description, and exclude a separate opener. Naming an unlocked descriptor
for a file another process holds is insufficient. The proof neither obtains an
unheld lease nor releases the inherited lease. It rechecks identity after proof.

`AcquireExisting` fails on missing evidence; `ProbeIdle` retains the historical
pre-initialization meaning of an absent file in an existing trusted parent.
Neither a successful probe nor a held lease proves ledger idleness, present
service health, publication authority, package-manager idleness or permission to
retry an unknown operation. Those existing caller checks remain mandatory.
Agent maps shared contention back to its existing typed host-busy result.

There is no on-disk schema change, ownership normalization, new initialization,
renewal enrollment or new hook. The historical Agent writer and its bounded
first-create-residue recovery remain unchanged. A future native renewal consumer
must retain the explicitly accepted numeric identity and recreate volatile
runtime infrastructure under a separately reviewed contract; it cannot guess
ownership from an absent CelikPanel account or use another lock. The kernel lease
alone also cannot detect a root administrator replacing the path after the final
check while ignoring all participating locks.

Validation uses a private native Linux filesystem: race tests exercise two
processes, SIGKILL/reacquisition, actual Agent writer/shared reader exclusion in
both directions, inherited/unheld descriptors, FIFO refusal under a subprocess
deadline, missing evidence, symlinks, hardlinks, special modes, wrong ownership,
nonempty data and parent/file replacement during acquisition. The complete Agent
test suite passes under race detection; vet also passes. The independently built
production recovery checker also passes canonical/inherited, active-ledger,
malformed/schema and FIFO cases. These are component/standalone-process checks,
not a new disposable-VM update/rollback or management-absent renewal acceptance.
P0.3-P0.5 remain partial. No installed owner panel was changed.
