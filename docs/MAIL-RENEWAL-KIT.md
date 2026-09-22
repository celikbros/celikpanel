# Independent mail renewal kit

P0.5 owner independence; P0.3/P0.4 versioned code and evidence compatibility.
This offline artifact is separate from installation authority. `make
mail-renewal-runtime` uses the reviewed compiler, clean build environment, helper
build tag and explicit build identity. Default release packaging and installed
Certbot hooks are unchanged until enrollment/migration acceptance is implemented.

The v1 manifest binds the exact helper, service, timer and Certbot deploy hook.
Its immutable generation is derived from the helper and original template bytes;
rendered service/hook paths point to that exact generation under
`/usr/libexec/celikpanel/mail-renewal/<generation>/renew`. The manifest declares
ledger v1, accepted plan v1 and host receipt v1. Unknown versions, substitutions,
noncanonical JSON, added files and unsupported unit templates are refused.
A digest is identity, not a signing trust root; production admission still needs
the authorized release/enrollment boundary.

The hook only queues the selected managed source. A native oneshot processes
pending work, with a 180-second invocation bound and whole-cgroup termination.
The timer checks after boot and five minutes after each invocation, adding jitter.
There is no Panel, licensing or management Agent service dependency, and the unit
does not start native mail services. Filesystem protection permits certificate
publication only inside the managed host directory under `/etc`; native Postfix
and Dovecot configuration remains read-only. Retained group and durable evidence
are still necessary. An unknown/interrupted operation remains pending instead of
being treated as a new job or silently cleared by periodic polling.

The offline builder checks real parents, bounded single-link regular files, exact
inventory and modes. It verifies a staged artifact before an atomic publication;
a changed recognized build is exchanged and its predecessor retained. Unknown
owner content is preserved. Failed stages are retained rather than recursively
removed. No installed unit or hook is edited or enabled by this build command.

Contract and builder race tests and vet pass. Native service sandbox, hook,
scheduled boot renewal and production enrollment/rollback evidence are separate
acceptance items; artifact tests alone do not establish them. Independent recovery
of interrupted renewal and the full P0.5 workload matrix remain open.
