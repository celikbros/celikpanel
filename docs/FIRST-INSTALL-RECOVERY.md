# First-install recovery

[Türkçe](FIRST-INSTALL-RECOVERY.tr.md)

If SSH or a provider's browser terminal disconnects during first installation,
reconnect and run the same public installation command. Terminal loss can still
interrupt the process. The installer now retains authenticated release metadata
so a repeat invocation can safely finish the original version.

The pending record is `/var/lib/celikpanel-release-state/install.pending`, a
root-owned directory with mode 0700. It contains only the signed manifest,
signature and pinned public key, each a single-link regular file with mode 0600.
Administrator credentials are never copied into this record. Publication is
atomic and synchronized to disk while the persistent update lock is held,
before archive download and installation changes.

Recovery verifies the signature, exact manifest encoding, platform, release
floor, permissions and directory identity. It downloads and verifies the same
immutable archive again, then replays the installer's existing idempotent setup.
It does not skip arbitrary steps based on a progress counter. Database content,
existing configuration and an already-created administrator are preserved.

An active installation holds the persistent lock and rejects a second command.
Before replaying setup against active unfinished services, recovery holds the
agent's shared mutation lock, checks its durable ledger, stops the panel and
checks the panel operation queue before stopping the agent. Busy or inconsistent
state blocks replay. A pending operation discovered after panel shutdown can
leave the unfinished panel stopped; the operation must settle before retry.

An empty root-owned completion marker is required after installer success.
The pending record is then removed. Re-running the default command on a complete
layout reports completion instead of updating or reinstalling; updates remain
in the authenticated panel update flow.

Alpha55 was released before pending records existed. Its bounded compatibility
path accepts only the exact original signed Alpha55 release and rollback floor,
byte-identical installed panel and agent, an inactive panel, zero login-capable
administrators and the initial agent ledger. Unknown or conflicting partial
layouts remain refused. This path preserves existing database contents; zero
administrators alone is not treated as proof of an empty database.

The September 8 Frankfurt SSH journal recorded `Disconnected by application`
from the provider terminal client. It establishes which side ended the session,
but does not establish why that client disconnected. The host stayed up, the
agent had started, and administrator creation and panel startup were unfinished.
The recovery change addresses that interrupted-install state; it does not claim
to prevent failures in an external browser terminal.
