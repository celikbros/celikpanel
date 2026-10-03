# roottests-20261003: every `deploy/test-*.sh` once as root on the candidate `48d264d5`, in a disposable Debian 13 QEMU guest

Commit `4a7701d5` and the closing limits of `docs/RESILIENCE-CONTRACT.md` item 3 record that the `deploy/test-*.sh`
contract tests needing root or a real `/tmp` were never run as root for this candidate. This run executes **all 54**
`deploy/test-*.sh` of `48d264d5` once as an unprivileged user (to re-derive which ones need root) and once as root,
each in a fresh extraction of the candidate archive, inside one disposable QEMU/KVM guest.

No installed server (Boston, Frankfurt, any) was touched or contacted. `celikpanel.net` and every licence service were
not contacted: before the first test the guest was restarted with QEMU user networking `restrict=on` (no outbound
packets; only the loopback SSH forward), proven by `guest/host-provision-and-netcheck.log` (outbound IP and host gateway
unreachable, `celikpanel.net` does not resolve). The tests that mention the origin use their own fake `curl`/fixture
mechanisms. No production signing key, no commit, push or git config change. **It closes no P0 row**; it is component
contract evidence only, not native update or automatic-rollback acceptance.

**Result in one sentence:** as root, 49 of 54 passed on the first run; the 5 failures are all environmental (none is a
product defect) - 3 fixtures execute files under `/run`, which Debian 13 mounts `noexec`; 2 tests need a prerequisite the
CI job supplies (`make dns-owner-tools`; git history with the pinned tag) - and each of the 5 passed on its one re-run
with exactly that prerequisite supplied, so all 54 have a passing root run on `48d264d5`.

## Guest provenance

| Item | Value |
| --- | --- |
| Host | `archlinux` WSL2 distribution on the Windows build host, QEMU 11.1.1, KVM (`guest/host-lab-provenance.txt`) |
| Lab | one node built with the release-recovery lab code of the candidate itself (`lab.py` + `dns-kill-matrix/fixture.py` from the run copy; `harness/rtlab.py`): lab root `/var/tmp/cp-release-drill-roottests-20261003`, cell `release-recovery__46b6f9fda2076a0f`, VM UUID `b47d7928-e14c-513d-a965-77d5e4377672`, identity marker `/etc/celikpanel-release-recovery-lab` from cloud-init, lab guest guard before every privileged command |
| Image | pinned `debian-13-genericcloud-amd64-20260826-2582.qcow2` (SHA-512 `184761b0...bf34003`, 340262912 bytes, verified against `images.lock.json` before use) |
| Guest | Debian 13.6 (trixie), kernel `6.12.105+deb13-cloud-amd64`, systemd 257.13, 8 vCPU, 8 GiB, overlay virtual size **20 GiB** (4.12 GiB used at the end) |
| Packages added | `git make php-cli php-sqlite3 nodejs python3 openssl curl ca-certificates tar xz-utils nftables iproute2 sqlite3 dash file gpg rsync bsdextrautils` (install.sh's Debian set + what the tests call; full `dpkg` list in `guest/guest-provenance.txt`) |
| Go | pinned `go1.26.5 linux/amd64` copied from the host's `/opt/celikpanel-test-toolchains/go1.26.5/go` (tarball SHA-256 `7ff79274...ffaa9`) to the same path in the guest; module cache filled per user with `go mod download all` **before** the network was cut; tests ran with `GOTOOLCHAIN=local GOPROXY=off LANG=C.UTF-8` |
| Source | `git archive 48d264d5` without line-ending conversion, SHA-256 `dec4e366ad6e38081f66d20f5fced961e1a96122ce21a3b951942f6bbaa17547`; every one of 47,448 files equals its committed blob id (`harness/archive-verification.txt`); `git archive HEAD` of a clone inside the guest produced the same SHA-256 (`logs/rerun/test-release-sequence-policy.log`) |
| Runner | `harness/run-pass.sh` as a transient systemd service (`systemd-run`), tests in sorted order, each in a fresh extraction, `timeout -k 30 1800 bash deploy/<test>.sh`, cwd = extraction root, stdin `/dev/null`; unprivileged pass as `rtuser` (no sudo) 21:00:10-21:05:54Z, root pass 21:08:48-21:17:32Z (2026-10-03 UTC) |

## Root run per test

Exit code and duration (seconds) of the root pass; the unprivileged column shows how the same test behaved without root
(`refused` = non-zero exit; `SKIP` = exit 0 with root-only cases skipped, fully or partly); re-run = the one root re-run
after supplying the named guest prerequisite.

| Test | Root exit | Root s | Unprivileged | Re-run (root) | Classification / first failure line |
| --- | --- | --- | --- | --- | --- |
| test-abort-pre-mutation-active-update-behavior | 0 | 0.1 | 1 (refused) | - | pass |
| test-abort-pre-mutation-active-update-contract | 0 | 0.2 | 0 | - | pass |
| test-agent-native-contract | 0 | 0.3 | 0 | - | pass |
| test-bootstrap-update-contract | 0 | 50.1 | 0 (SKIP part) | - | pass |
| test-dns-owner-tools-package | **1** | 0.0 | 1 | 0 (2.9) | test prerequisite the guest lacked: built owner tools (CI runs `make dns-owner-tools` first in the same job) - `Run make dns-owner-tools first` |
| test-download-command-contract | 0 | 0.4 | 0 | - | pass |
| test-download-portal-contract | 0 | 0.4 | 0 | - | pass |
| test-download-portal-operation-selection | 0 | 1.6 | 1 (refused) | - | pass |
| test-finalize-pending-rollback-contract | 0 | 0.1 | 0 | - | pass |
| test-finalize-pending-update-contract | 0 | 0.7 | 0 | - | pass |
| test-first-install-resume-contract | 0 | 6.3 | 1 (refused) | - | pass |
| test-first-install-signed-trust-contract | 0 | 0.2 | 0 | - | pass |
| test-fresh-install-entry-boundary-contract | 0 | 6.3 | 1 (refused) | - | pass |
| test-go-toolchain-contract | 0 | 5.4 | 0 | - | pass |
| test-go-toolchain-migration-contract | 0 | 0.2 | 1 (refused) | - | pass |
| test-install-kernel-reboot-contract | 0 | 0.2 | 0 | - | pass |
| test-install-localization-contract | 0 | 0.1 | 0 | - | pass |
| test-install-toolchain-contract | 0 | 0.3 | 0 | - | pass |
| test-isolated-database-migration | 0 | 0.2 | 0 | - | pass |
| test-membership | 0 | 8.9 | 0 | - | pass |
| test-package-activity-rule-contract | 0 | 0.1 | 0 | - | pass |
| test-panel-tls-snapshot-contract | 0 | 11.3 | 0 (SKIP) | - | pass |
| test-prebuilt-update-contract | 0 | 1.2 | 1 (refused) | - | pass |
| test-recover-active-update-database-contract | 0 | 0.4 | 0 | - | pass |
| test-recover-frankfurt-alpha78-runner | 0 | 2.4 | 1 (refused) | - | pass |
| test-recovery-renewal-pause-contract | 0 | 0.3 | 0 | - | pass |
| test-recovery-resource-shell-contract | 0 | 0.1 | 0 | - | pass |
| test-recovery-runtime-shell-contract | 0 | 36.6 | 0 (SKIP) | - | pass |
| test-release-acceptance-license-guard | 0 | 33.0 | 0 | - | pass |
| test-release-content-guard | 0 | 7.4 | 0 | - | pass |
| test-release-foundation-root-contract | 0 | 0.3 | 1 (refused) | - | pass |
| test-release-publish-contract | 0 | 0.1 | 0 | - | pass |
| test-release-recovery-contract | **1** | 0.1 | 1 (refused) | 0 (47.0) | test assumes an exec-permitted `/run`; Debian 13 mounts `/run` `noexec` - `celikpanel-release-recovery.service: Command /usr/libexec/celikpanel/release-recovery is not executable: Permission denied` (fixture root `mktemp -d /run/...`, then `systemd-analyze verify --root`) |
| test-release-recovery-observation | 0 | 4.3 | 0 (SKIP) | - | pass |
| test-release-recovery-rollback-handoff | **1** | 0.4 | 1 (refused) | 0 (4.0) | same `/run` `noexec` assumption - `!! retained recovery release executables are unsafe` (the runner's `[[ -x ]]` is false on a noexec mount even for root) |
| test-release-sequence-policy | **1** | 0.1 | 1 | 0 (0.1) | test environment: needs git history with tag `v0.1.0-alpha.79` (CI checks out with `fetch-depth: 0`); an archive copy has none - `fatal: not a git repository ...` / `historical tag is not available: v0.1.0-alpha.79` |
| test-release-transaction-guard | 0 | 20.8 | 0 (SKIP) | - | pass |
| test-release-unit-transition | 0 | 23.3 | 0 (SKIP) | - | pass |
| test-release-updater-rollback-contract | 0 | 5.8 | 0 (SKIP part) | - | pass |
| test-rollback-journal-commit | 0 | 0.1 | 0 | - | pass |
| test-rollback-ledger-preservation | 0 | 6.5 | 0 (SKIP) | - | pass |
| test-rollback-material-admission | **1** | 5.1 | 0 (SKIP) | 0 (8.0) | same `/run` `noexec` assumption - `.../33-existing-missing-absent/usr/libexec/celikpanel/recovery: Permission denied` then `FAIL: case 33 rejected supported admission` |
| test-rollback-reboot-runtime | 0 | 2.5 | 0 (SKIP) | - | pass |
| test-schema17-bridge-contract | 0 | 0.2 | 0 | - | pass |
| test-signed-release-enrollment-contract | 0 | 8.4 | 1 (refused) | - | pass |
| test-signed-release-manifest-contract | 0 | 7.0 | 1 (refused) | - | pass |
| test-unit-start-limit-contract | 0 | 0.4 | 0 | - | pass |
| test-update-enrollment-preflight | 0 | 0.1 | 0 | - | pass |
| test-update-failure-report | 0 | 0.2 | 0 | - | pass |
| test-update-panel-start-readiness | 0 | 3.5 | 0 (SKIP part) | - | pass |
| test-update-preflight-refusal-contract | 0 | 0.2 | 0 | - | pass |
| test-update-quiesce-capture | 0 | 0.3 | 0 | - | pass |
| test-update-quiesce-exit-contract | 0 | 0.6 | 0 | - | pass |
| test-update-recovery-lock | 0 | 0.5 | 0 (SKIP) | - | pass |

Counts: root pass **49 pass / 5 fail** (0 product defect, 3 test assumption the guest lacked, 2 test prerequisite/environment);
root re-run **5 / 5 pass**. Unprivileged pass: 40 exit 0 / 14 exit non-zero.

## Which tests need root (re-derived)

- **12 refuse or fail without root** (unprivileged exit 1): abort-pre-mutation-active-update-behavior (`staged panel
  database metadata or bounded size is unsafe`), download-portal-operation-selection, first-install-resume,
  fresh-install-entry-boundary, go-toolchain-migration, prebuilt-update, recover-frankfurt-alpha78-runner,
  release-foundation-root, release-recovery-contract, release-recovery-rollback-handoff, signed-release-enrollment,
  signed-release-manifest (`trusted first-enrollment minimum was rejected`; CI runs it under `sudo`).
- **12 exit 0 without root but print `SKIP`** for their root-only cases: fully - panel-tls-snapshot,
  recovery-runtime-shell, release-recovery-observation, release-transaction-guard, release-unit-transition,
  rollback-ledger-preservation, rollback-material-admission, rollback-reboot-runtime, update-recovery-lock; partly -
  bootstrap-update-contract (apply-only snapshot fixture, recovery lock), release-updater-rollback-contract (ledger
  preservation), update-panel-start-readiness (panel.env ownership cases). An unprivileged "pass" of these is not a run
  of their root cases.
- The two remaining unprivileged failures (dns-owner-tools-package, release-sequence-policy) are not root-related.
- finalize-pending-update-contract and update-quiesce-exit-contract (named in the request) need neither root nor
  anything absent here: they passed unprivileged and as root in this guest. All root runs printed no `SKIP`.

## The re-runs (`logs/rerun/`, `harness/rerun.sh`)

One re-run each, as root, in a fresh extraction, with only the named prerequisite added; no test or product file changed.

- `/run` exec: `mount -o remount,exec /run` for the three `/run` fixtures, restored to `noexec` afterwards
  (`run-mount-before/during/after.txt`). Production paths are not under `/run`; the fixtures place their fake roots there.
  Whether the CI runner's `/run` permits exec was not checked here.
- `make dns-owner-tools` in the extraction, then the test (make exit 0, test passed).
- A clone of a git bundle (candidate history + `refs/tags/v0.1.0-alpha.79`, made on the Windows host with
  `git bundle create`, no ref or config change) detached at `48d264d5`; tree `9456cd27...`, `git status` clean; the
  test passed (`79 -> 80; historical bootstrap sequence 79`).

## Harness defect found and corrected during the run (attempt 1)

The first archive was produced by `git archive` on the Windows host, whose `core.autocrlf=true` converted the 1,077
`text=auto` files without an `eol=lf` rule to CRLF (e.g. `deploy/recovery/agent-checker.sources`). Both passes ran on
that copy first; its root pass had 7 failures, two of them (`release-updater-rollback-contract`,
`rollback-ledger-preservation`: `malformed import path "cmd/agent/recovery_check_entry.go\r"`) caused purely by the
conversion. That attempt is **not** a run of the candidate; its result tables and failing root logs are kept in
`logs/attempt1-crlf/` for transparency. The archive was rebuilt with `git -c core.autocrlf=false -c core.eol=lf archive`,
verified blob by blob, and both passes were repeated from scratch on it (the results above). Test residue left in `/tmp`
by attempt 1 was removed before the repeat; the root pass's per-test footprint (`logs/root/*.footprint`, files changed
under `/etc /var/lib /opt /usr/local /run/systemd /srv`, depth 4) lists only systemd runtime files, the timesync clock
and the `/var/lib` directory itself (an entry created and removed again by some tests); no file under `/etc` or a new
`/var/lib` entry remained.

## Files

`logs/{root,unpriv,rerun}/` per-test logs, `results.tsv` (name, exit, seconds), `pass-info.txt`; `logs/attempt1-crlf/`;
`guest/` provenance, apt log, host setup/provision logs with the network check; `harness/` every script used and the
archive verification; `secret-scan.txt`; `SHA256SUMS` over every other file here.

The guest was stopped after collection and its lab root (overlay, seed, key) and the host work directory were removed.
