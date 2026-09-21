# Bound worker and native recovery acceptance

*P0.2 / D-025 invariants 2, 4 and 5. Disposable fixture only.*

This drill connects a genuine Go update worker's observation to the native
snapshot binding and the authenticated recovery reader. Earlier AB/AD drills
used a direct native bootstrap and cannot establish this association.

## Trust and operation boundaries

- `current_worker_baseline.py` installs a fresh unpublished Alpha81 artifact
  from commit `eb14273227340d811f1db8500e6886c5b23d7f24` using its unchanged
  installer. Its unchanged enrollment helper installs a new fixture public key
  and sequence floor. It refuses an existing installation and repeat intent.
- `worker_fixture_origin.py` generates a separate disposable signing key and
  CA. Only the registered guest trusts that CA and resolves `celikpanel.net`
  to its loopback fixture. The HTTPS server serves four fixed update paths;
  it never serves keys, licensing or arbitrary files. The private signing key
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
not native recovery acceptance. AF then exposed a production contract mismatch: runtime enrollment created the
shared `/usr/libexec/celikpanel` directory as `0700`; the next start-guard step
required `0755`. Its stopped guest and logs are retained separately. The fresh
installer now establishes the shared directory through the strict existing
contract before enrollment. Existing conflicting owner metadata remains unchanged;
private runtime directories remain `0700`. A real Bash-to-Go regression reproduces
the previous failure and verifies absent, existing `0755`, and conflicting `0700`
cases under a real inherited transaction lock. Schema and runtime ABI are unchanged.

Fresh AG will exercise this predecessor. Native results remain unconfirmed.

The full P0.1/P0.2/P0.3 matrix, production release-signature path, older-release
schema transitions and independent workload lifecycle requirements remain open.