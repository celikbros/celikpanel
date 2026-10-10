# set6 native run, 2026-10-09 (UTC): the release candidate `72b879eea` installed fresh on Arch, Debian 13 and Ubuntu 24.04, the Panel's own `Strict-Transport-Security` header read on the guest, and one good update per platform from the published v0.1.0-alpha.81

A measurement record. Disposable QEMU/KVM guests of the local `archlinux` WSL host; this folder is named with the
calendar date of the first cell (`date -u` on the host: `host/hostcheck-before.txt`, `host/progress.txt`; the first
cell started at 21:46:23Z).

**What ran.** Seven runs of six cells, one after the other, each on a new lab:

- **A. The candidate installed fresh** (cells `set6-arch`, `set6-debian13`, `set6-ubuntu`): an archive built from
  commit `72b879eeaf88477133cc01bae6a615e292c00181` is installed by its own installer on a new guest, its owner sets the
  server up, and set4's sections are run in set4's order with set4's code in the form of its run-b (the site's home
  and the socket really checked), the import section in set4b's form (what the answer lists), and, on Debian and
  Ubuntu, set4's Postfix Stop section as set4b corrected the product for it.
- **B. The header.** In every cell the response header `Strict-Transport-Security` of the running Panel is read on
  the guest, over HTTPS and over plain HTTP: twice in a fresh-install cell (after the setup, and after the last
  section), and in an update cell before the update (the published alpha.81 answers) and after it (the candidate
  answers).
- **C. One good update per platform** (cells `upd1-arch-good`, `upd1-debian13-good`, `upd1-ubuntu-good`): set5's
  good-update cells with set5's driver, steps and pass criteria, from the published tag `v0.1.0-alpha.81` (unpatched
  source, test-licence build) to the candidate labelled `v0.1.0-alpha.82 / 82`. **The other seven cells of set5 (the
  three migration-defect cells, the start-check cell, the two owner-continuation cells and the management-off cell)
  were not repeated.**

**What this is not.** No installed server was addressed by the harness: it holds no name or address of one, and none
appears in the raw files (the search is recorded in `host/installed-server-names-search.txt`). That no traffic of the
host or the guests reached an installed server, celikpanel.net, a licence service or a certificate authority was not
measured: the guests have outbound NAT ("Network" below) and no traffic was captured. Nothing was committed, pushed,
published or signed with a production key. Product code (`cmd/`, `internal/`, `web/`), `docs/` and `ROADMAP*` were not
edited by this run. Every `result.json` carries `native_evidence: false`. **It closes no P0 row.** Nothing was rendered
in a browser.

**Result in one sentence.** On the fresh-install cells every item of part A has a raw record with the expected answer
on every platform it applies to (the Postfix Stop is NOT-MEASURED on Arch, where there is no Postfix); in all fourteen
header readings (twelve of the six cells, two more in the repeated Arch run) every HTTPS answer of the candidate carries exactly `max-age=31536000`, every HTTPS answer of the
published alpha.81 carries `max-age=31536000; includeSubDomains`, and no answer to plain HTTP carries the header; the
three updates ended verified with the ledger at the released 43 and the `428` / `409` answers set5 measured. The one
check that did not pass in the whole run (of 433) is a harness rule of set3 that the candidate's documented answer no
longer satisfies (S6-H1, `fresh-install/set6-arch/run-a`); that cell was run again with the rule corrected, and the
first run is kept. No product failure was found.

## What was built from what, and how this folder shows which code was measured

Two builds (`go1.26.5 linux/amd64`, `build/go-version.txt`; one job, 21:33:52Z to 21:42:58Z, `build/build-job.*.txt`),
every archive `license_mode: acceptance-fixture` (`build/<name>/`: the artifacts document, the dist JSONs and their
build logs, the fixture commits and patches, the trees, the builder's logs).

| Build | Role | Label / seq | Commit | Tree | Archive SHA-256 | Installed by |
| --- | --- | --- | --- | --- | --- | --- |
| `cur` (21:33:52-21:39:10Z) | baseline | v0.1.0-alpha.81 / 81 | 2159bb202d7bf69fe650c95910c6506a47f730c9 | a7563a6c342f8e0ad4fd714a3e40a770614b9647 | c81bf6b337c0460054e0ab96aa21d853dc2c8ff8945563207b3c2a8865768b0b | the four fresh-install runs |
|  | good, defective, startcheck, realstart | v0.1.0-alpha.82 / 82 | built because the artifacts document needs them | | | never installed |
| `a81` (21:39:12-21:42:58Z) | baseline | v0.1.0-alpha.81 / 81 | a0beb7263d1f4ca72258f6b306f9111ba4e2a334 | b1dffa78bcaca9e3514e5b2bc42f0e2cf47dca2f | 3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109 | the three update cells, first (built by set3; reused unchanged) |
|  | good | v0.1.0-alpha.82 / 82 | 9cb2339f10a0445f26ad7ddcc6058a4c359133f3 | f48cfe7b09245185bad982d39ffb4c2e1cad35a2 | 86efd86a0cffcf5c8956ab55ce32144ec1ac52da77fb8f3c69a350cc185f149e | the three update cells, by the update |
|  | defective, startcheck | v0.1.0-alpha.82 / 82 | built because the artifacts document needs them | | | never installed |

- **The candidate, fresh (`cur`).** `run-upd1.sh build 72b879eea`. Its baseline `2159bb202` is an empty fixture commit
  over `72b879eea`: the tree is the tree of `72b879eea`, `a7563a6c342f8e0ad4fd714a3e40a770614b9647`
  (`build/cur/trees.txt`: "files that differ between the source 72b879eea and this build's baseline: 0";
  `build/cur/fixture-patches.diff`, first block empty; `host/hostcheck-before.txt` for the tree id of `72b879eea`).
  It is the candidate source with the acceptance-test licence seam switched on at build time, **labelled
  v0.1.0-alpha.81 / 81**, because `72b879eea` still carries that release policy. The Panel of a fresh-install cell
  therefore names itself `v0.1.0-alpha.81`, commit `2159bb202...`, schema 43 (the `panel` field of every header
  reading). A fresh install of an archive labelled v0.1.0-alpha.82 was not measured (set4 measured the same way).
- **The candidate, as the update's target (`a81`, good).** `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 72b879eea`.
  The good candidate is `72b879eea` plus one file, the release policy `deploy/release-sequence-policy`
  (`v0.1.0-alpha.82 / 82`, previous `81 / v0.1.0-alpha.81 / a0beb7263...`); the diff is in
  `build/a81/fixture-patches.diff`, the commits in `build/a81/fixture-commits.txt`. The fixture commits exist only in
  the builders' disposable clones (`/var/tmp/cp-upd1-build/20261009t213352z/repo`, `.../20261009t213912z/repo`); no
  commit object was made in the working repository.
- **The baseline of the update cells: the published tag's unpatched source, test-licence build.** The tag's own
  commit, no fixture commit and no patched file (`patched_files: []` in `build/a81/upd1-artifacts.json`;
  `build/a81/baseline-ref-proof.txt`: `git diff --name-status tag..baseline` is empty, 35 listed entries each
  `identical`, 0 `DIFFERENT`, no licensing package in the Agent's package closure). **Its archive was not built by this
  run**: it is the one set3 built (`/var/tmp/cp-pair-accept/dist/a0beb7263...-acceptance-license`), used read-only;
  its SHA-256 read on the host before the build (`host/hostcheck-before.txt`) is `3350ff44...`, the value in set3's,
  set4's and set5's READMEs; the builder says so (`build/a81/build.err.txt`, `BUILD-UPD1-REUSED`) and the artifacts
  document marks it (`reused_dist`). It is not the signed release archive.
- **`72b879eea` against set5's candidate and against the branch head.** `git log 67b62cc0f..72b879eea -- cmd internal
  web` is one commit, `72b879eea` itself, and the files of `cmd`, `internal` and `web` that differ are
  `cmd/panel/security.go` and `cmd/panel/security_test.go` (`build/cur/trees.txt`). The branch head was `72b879eea`
  at the start and at the end of the run (`host/hostcheck-before.txt`, `host/working-tree-status.txt`). While the run
  went on, another session modified other files of the same working tree (`host/working-tree-status.txt`:
  `README.md`, `README.tr.md`, `deploy/release-sequence-policy`, three `deploy/test-*.sh`, `download-portal/get.sh`,
  two release-notes files); none is under `cmd/`, `internal/` or `web/`, and no build and no cell read the working
  tree's product files: the builds and the run copies are `git archive 72b879eea`.
- **How a cell shows what it ran.** `build/cur-prove.json`, `build/a81-prove.json` (`run-upd1.sh prove`, exit 0 for
  both documents, twice: `host/progress.txt` at 21:46Z, `build/afterbuild-c.out.txt`): the inventory, the release
  policy and the committed-source proof of each archive against its own commit's blobs, the reused one included. Each
  cell's `result.json` names its archives by commit and SHA-256 (`artifacts`); its `steps/02-preflight/preflight.json`
  repeats the proof for the archives it uses; each header reading holds the answer of the running Panel's own version
  route (`panel`: version, commit, schema); in an update cell `steps/NN-terminal/step.json` (`builds`) holds the
  identity files of the installed Agent and Panel as read on the guest at the end (`update-cells.md`).

## Harness changes (working tree, `deploy/e2e/release-recovery/` only)

| File | What |
| --- | --- |
| `set6_trial.py` (new) | The driver of this run. `Set6Trial` over set4b's `Set4bTrial` (itself set4's `Set4Trial`): the fresh-install cells; set4's sections in set4's order, `M2` in set4b's form, the header section `B` twice, and set4's `M5` judged with the list rule of the candidate (S6-H1). `Set6UpdateTrial` over set5's `Set5UpdateTrial`: the three good-update cells with the header reading before and after the update. Both: the first step `set6-name-pinning` and the last measuring step `set6-name-pinning-at-the-end`. `test_set6_trial.py` pins that no section of the earlier drivers and no step of the update driver is replaced. |
| `set6_redact.py` (new) | set5's token-digest rules unchanged, and the same rules inside base64 text: every run of 40 or more base64 characters that decodes strictly to UTF-8 text is decoded (three levels), redacted and encoded again; a JSON object changed that way gets a member `set6_base64_redaction` that names the `*_sha256` members of the object which no longer match the stored text. Used in front of the driver's redactor, in the sweep before a result is written, and at staging. |
| `run-set6.sh` (new) | Wrapper, as `run-set5.sh`: one cell per new lab; `SET6_DISK_GATE` is asked immediately before the guests start. |
| `test_set6_trial.py` (new) | 25 offline tests: the pinning verdict by both lookup paths, the preflight that knows the lab's own origin line, the header judgement (a missing route class or an unanswered request is never a pass), the base64 pass, the list rule, the composition. |
| `worker_fixture_origin.py` (changed, +16 -6) | The fixture origin's guest provisioning refused a hosts file that already maps `celikpanel.net`. It now accepts exactly one line, `127.0.0.1 celikpanel.net # disposable CelikPanel lab pin before provisioning`, when nothing else maps the name, and then does not write its own line; any other mapping is refused as before. Needed because the name is pinned in the first step of every cell. Without that line in the hosts file nothing changes. |

No other harness file was changed; `lab.py`'s opt-in of set5 (`CELIKPANEL_LAB_LINK_BASE_IMAGES=1`) was used as it is.

Run copies (`harness-run-copy/`: per overlay `files.sha256`, `harness.diff`, `differs-from-archive.txt`,
`runcopy-against-commit.txt`; `pristine-files.sha256` once, `pristine-files-compared.txt`; job files `jobs/`;
`queue.log`). A run copy is `git archive 72b879eea` without the retained evidence of earlier runs under
`deploy/e2e/{release-recovery,dns-kill-matrix,dns-pair-acceptance}/evidence/` (except `evidence/upd1-20261001`, which
four offline tests read), as in set5, plus the listed harness files of the working tree. `a` = the archive alone (the
two builds); `b` = `a` + the five files (the first proofs and dry runs, and `set6-arch/run-a`); `c` = `b` with S6-H1
corrected = the working tree's five files (SHA-256 equal, `host/working-tree-against-copy-c.txt`): the six other runs,
the second proofs and dry runs. `PYTHONDONTWRITEBYTECODE=1`.

Offline suites. On copies `b` and `c` each of `test_set6_trial` (24, then 25), `test_set5_trial` 10,
`test_owner_update_trial` 191 (3 skipped), `test_settings_writes_trial` 30, `test_request_identity_trial` 17,
`test_recovery_candidate_archive` 11, `test_lab` 15, `test_guest_probe` 13, `test_set4b_trial` 8,
`test_worker_fixture_origin` 15 ends OK (`build/offline-<copy>-*.txt`). The whole suite (`python3 -m unittest discover
-s deploy/e2e/release-recovery -p 'test_*.py'`): on copy `a`, before any change, 986 tests, 5 errors
(`build/suite-a.txt`); on copy `c` 1011 tests, the same 5 errors by name (`build/suite-c.txt`,
`build/suite-a-notok.txt` and `build/suite-c-notok.txt` are equal; copy `b`: 1010 tests, the same 5). Each of the five ends
in a failed `git -C <run copy> ...` call with exit status 128 (`build/suite-a.txt`); a run copy is an archive, not a
repository (the same errors stood on copy `a` before any change). The 25 new tests
are `test_set6_trial.py`. The dry run of each of the six cells exited 0 and created no lab on copies `b` and `c`
(`build/dry-<copy>-*.json`, `*.rc.txt`). `worker_fixture_origin.provision` with the lab's line has no offline test: it
needs a marked guest; it ran in all seven runs (`steps/03-origin/step.json`, `provision.hosts_mapping`).

Harness defects found during the run (product code was never changed):

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| S6-H1 | set4's `M5` section judges `imported` / `not_imported` with set3's rule (`request_identity_trial.partial_lists`: every `ok` step is imported). Since `1f182a483` the product lists a step that ended without an error and imported nothing under `left_out` (`cmd/panel/import_handlers.go`: "A part is in exactly one of the three lists"); set4b measured that on `M2` only, and `M5` had not been run on a candidate with `left_out` before. The `dns` step (`ok`, `state: left_to_owner`) is in `left_out`, the old rule expected it in `imported`, and the check failed although the answer is the documented one. | `Set6Trial` runs set4's `M5` with the rule of the candidate (not ok: `not_imported`; ok with a `state`: `left_out`; ok without: `imported`) and adds two checks on `left_out`. set4's and set3's files are unchanged. | `fresh-install/set6-arch/run-a`: `M5-import-absolute` failed by this rule (1 check of 10; its other nine checks and every other section of that run passed). Corrected from copy `c`: `set6-arch/run-b` and all later runs. |

No check of this run was found to have passed without measuring; what each check of part A read is quoted in
`part-a-readings.md`, and the header judgement refuses a reading that lacks a route class or an answer.

## Guests, images, packages

Base images (the image cache `/var/tmp/cp-v3n28/images` of earlier runs, read-only; digests from each run's
`host/fixture-plan.json`): `Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2` (sha256 `5d8be8d2...`),
`debian-13-genericcloud-amd64-20260826-2582.qcow2` (sha512 `184761b0...`),
`ubuntu-24.04-server-cloudimg-amd64-20260826.img` (sha256 `d0fe84bb...`): the images of set3, set4 and set5. One new lab per run, stopped by
the wrapper. A Debian or Arch cell starts both guests of the pair lab and works on one; an Ubuntu cell starts one
guest. Each guest: 2 CPUs, 3072 MB, KVM, a 24 GB qcow2 overlay over the base image (`cache=none`), QEMU 11.1.1 (`host/hostcheck-before.txt`;
the WSL kernel version is not recorded in this folder). The overlays were on the WSL disk (not in RAM, unlike set5); the lab's
base image is a hard link to the cached image (`lab.py`'s opt-in of set5).

Packages are the distributions' own, installed by the product's setup during the cell from the distributions'
repositories. Read on the guest at the end of each run (`packages.md`):

| | Arch | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- | --- |
| OS | Arch Linux | Debian GNU/Linux 13 (trixie) | Ubuntu 24.04.4 LTS |
| kernel | 7.2.9-arch1-1 | 6.12.105+deb13-cloud-amd64 | 6.8.0-138-generic |
| certbot | 5.8.0-1 | 4.0.0-2+deb13u1 | 2.9.0-1 |
| cron | 1.7.2-2 | 3.0pl1-197 | 3.0pl1-184ubuntu2 |
| cronie | 1.7.2-2 | - | - |
| dovecot-core | - | 1:2.4.1+dfsg1-6+deb13u7 | 1:2.3.21+dfsg1-2ubuntu6.5 |
| mariadb | 13.0.2-2 | - | - |
| mariadb-server | 13.0.2-2 | 1:11.8.6-0+deb13u1 | 1:10.11.14-0ubuntu0.24.04.1 |
| nginx | 1.30.5-1 | 1.26.3-3+deb13u9 | 1.24.0-2ubuntu7.18 |
| nginx-common | - | 1.26.3-3+deb13u9 | 1.24.0-2ubuntu7.18 |
| openssl | 3.6.5-1 | 3.5.7-1~deb13u2 | 3.0.13-0ubuntu3.15 |
| php | 8.5.11-1 | - | - |
| php-fpm | 8.5.11-1 | 2:8.4+96 | 2:8.3+93ubuntu2 |
| php8.3-fpm | - | - | 8.3.6-0ubuntu0.24.04.11 |
| php8.4-fpm | - | 8.4.26-1~deb13u1 | - |
| postfix | - | 3.10.13-0+deb13u1 | 3.8.6-1ubuntu0.1 |
| postgresql | 18.6-2 | 17+278 | 16+257build1.1 |
| postgresql-16 | - | - | 16.15-0ubuntu0.24.04.1 |
| postgresql-17 | - | 17.11-0+deb13u1 | - |
| rspamd | - | 3.12.1-1 | 3.8.1-1ubuntu3 |
| systemd | 262-1 | 257.13-1~deb13u1 | 255.4-1ubuntu8.17 |

Setup profile as in set3 to set5: Debian and Ubuntu purpose `web_mail`, Arch purpose `web`; DNS mode external. The
owner's setup runs to the wait at `access_dns` (`server_setup_access_dns_required`, a finding line of every
`result.json`); the certificate steps of the wizard are not reached. The finding line says "on an isolated host": that
is the harness's wording for a guest without a public name, not a statement that the guest was cut off from the network.

## Network: QEMU user networking with outbound NAT, and name pinning (this is not network isolation)

The guests run on QEMU user networking with outbound NAT (`host/fixture-plan.json` of each run: `-netdev
user,id=mgmt,hostfwd=tcp:127.0.0.1:<port>-:22`, no `restrict=on`), and the distributions' packages are fetched from
their repositories during a cell. No file records the guests' traffic, so nothing here shows that a guest could not
reach the outside, and nothing here shows what it reached. systemd-resolved's own `Total Transactions` counter
(`resolvectl statistics`) rose during every run (below), and was already above zero when the names were pinned (26 on
Arch, 6 on Debian, 8 on Ubuntu, 4 timeouts on Arch and Debian: `name-pinning.json`, `resolved_statistics_*`); which
names were asked, by what, and to which server is not recorded. What is established, per run (`pinning.md`, generated
from the step files):

1. **Five names, in the first step, before any product path exists.** `steps/01-set6-name-pinning/`
   (`name-pinning.json`, `step.json`) appends to the guest's hosts file the release origin's name `celikpanel.net`
   (as `127.0.0.1` only: the lab's fixture origin listens on `127.0.0.1:443`) and the four certificate-authority
   directory names set5 pinned (`acme-v02.api.letsencrypt.org`, `acme-staging-v02.api.letsencrypt.org`,
   `acme.zerossl.com`, `dv.acme-v02.api.pki.goog`, each as `127.0.0.1` and `::1`). The step refuses to write when one
   of `/opt/celikpanel`, `/etc/celikpanel`, `/var/lib/celikpanel`, `/usr/libexec/celikpanel` exists; in all seven runs
   none existed (`product_paths_present: []`), 20 to 59 s after the guest's boot. `preflight` and everything after it
   need this step. This is lab preparation, not an owner action.
2. **Read back by two paths.** First `getent -s files ahosts NAME` and `getent -s files hosts NAME` (the hosts file
   only; no DNS question is sent): every address of every name is the guest's own loopback, in all seven runs. Then,
   only after that reading and 4 s after the write, `getent ahosts NAME` and `getent hosts NAME`, the lookup order of
   the guest's own `nsswitch.conf`: every address of every name is again the guest's loopback, in all seven runs, at
   the pinning and at the end of the run. The `hosts:` line is `mymachines resolve [!UNAVAIL=return] files myhostname
   dns` on Arch (`resolve` before `files`), `files myhostname resolve [!UNAVAIL=return] dns` on Debian 13 and `files
   dns` on Ubuntu 24.04. On Arch the default path prints the names under the canonical name `localhost` (`::1
   localhost` for a certificate-authority name, `127.0.0.1 localhost` for `celikpanel.net`), and `getent ahosts
   celikpanel.net` answers `::1` as well as `127.0.0.1` there although the hosts line names `127.0.0.1` only (set4's
   observation 7, now with the raw answers: `pinning.md`, second part). systemd-resolved runs on all three; its
   counter `Total Transactions` (`resolvectl statistics`) was the same before and after the ten default-path lookups
   of the step in every run (Arch 26 and 26, Debian 6 and 6, Ubuntu 8 and 8), that is, those ten lookups added no
   transaction to its count. That counter is not a capture of traffic, and it was not zero before the pin was written
   (the guests had made lookups while booting, before the first step).
3. **The origin step** (`steps/03-origin/step.json`, `origin_check`: `loopback_only: true`, HTTP 200) finds the name
   already mapped; the fixture's provisioning keeps the lab's line (`provision.hosts_mapping`: "the lab pre-pin line
   of the first step, kept") and `preflight` records the line it saw
   (`steps/02-preflight/step.json`, `set6_origin_name_lines_in_the_hosts_file`).
4. **At the end** (`steps/NN-set6-name-pinning-at-the-end/`): the five names by both paths again, the boot id (an Arch
   cell ends in another boot than it began: the Arch installer demands a restart), certbot's directories. In every run
   certbot is installed by then (`/usr/bin/certbot`) and has no account directory, no lineage and no renewal
   configuration (`/etc/letsencrypt/{accounts,live,renewal}` do not exist); `/var/log/letsencrypt` does not exist
   (Debian, Ubuntu) or is empty (Arch). The same reading shows the resolver's counter at the end of the run: Arch 50 (both
   fresh runs) and 46 (update), Debian 146 and 169, Ubuntu 250 and 243, against 26, 6 and 8 at the pinning: the guests
   did ask DNS questions during the cells.
5. **The licence step** was answered, by the Panel's own account, without a licence service
   (`steps/NN-license/step.json`: `license_service: "not contacted: acceptance test build"`).
6. **Certificate routes.** The drivers call no certificate or ACME route; the routes this run adds are the ones of the
   header reading, all on the guest's own loopback.

In the fresh-install cells set4's own `M0` section runs unchanged after that: it appends the two Let's Encrypt names a
second time (they are then in the hosts file twice, `name-pinning-at-the-end.json`, `hosts_lines`), and its
`before_the_acme_lines` reading shows them pinned already.

| Run | Pinned at (guest), after boot | Product paths present then | nsswitch `hosts:` | All five names loopback-only: hosts file / default path | resolved transactions before -> after | At the end: hosts file / default path | Boot id changed | certbot directories at the end |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `fresh-install/set6-arch/run-a` | 2026-10-09T21:47:33Z, 58.86 s (passed) | [] | `hosts: mymachines resolve [!UNAVAIL=return] files myhostname dns` | True / True | 26 -> 26 | True / True (passed) | True | letsencrypt: []; accounts: absent; live: absent; renewal: absent |
| `fresh-install/set6-arch/run-b` | 2026-10-09T21:59:49Z, 56.76 s (passed) | [] | `hosts: mymachines resolve [!UNAVAIL=return] files myhostname dns` | True / True | 26 -> 26 | True / True (passed) | True | letsencrypt: []; accounts: absent; live: absent; renewal: absent |
| `fresh-install/set6-debian13/run-a` | 2026-10-09T22:09:24Z, 58.17 s (passed) | [] | `hosts:          files myhostname resolve [!UNAVAIL=return] dns` | True / True | 6 -> 6 | True / True (passed) | False | letsencrypt: absent; accounts: absent; live: absent; renewal: absent |
| `fresh-install/set6-ubuntu/run-a` | 2026-10-09T22:20:04Z, 20.15 s (passed) | [] | `hosts:          files dns` | True / True | 8 -> 8 | True / True (passed) | False | letsencrypt: absent; accounts: absent; live: absent; renewal: absent |
| `update-alpha81/upd1-arch-good/run-a` | 2026-10-09T22:39:21Z, 55.16 s (passed) | [] | `hosts: mymachines resolve [!UNAVAIL=return] files myhostname dns` | True / True | 26 -> 26 | True / True (passed) | True | letsencrypt: []; accounts: absent; live: absent; renewal: absent |
| `update-alpha81/upd1-debian13-good/run-a` | 2026-10-09T22:49:26Z, 58.0 s (passed) | [] | `hosts:          files myhostname resolve [!UNAVAIL=return] dns` | True / True | 6 -> 6 | True / True (passed) | False | letsencrypt: absent; accounts: absent; live: absent; renewal: absent |
| `update-alpha81/upd1-ubuntu-good/run-a` | 2026-10-09T23:02:10Z, 22.39 s (passed) | [] | `hosts:          files dns` | True / True | 8 -> 8 | True / True (passed) | False | letsencrypt: absent; accounts: absent; live: absent; renewal: absent |

## Disk, host power

**The rule.** Before every guest start the newest Windows `C:` reading (PowerShell `Get-PSDrive C`, written every 30 s
by `tools/cwatch.ps1`; WSL cannot run PowerShell on this host) had to be younger than 120 s and at least 40 GiB
(42 949 672 960 bytes). `tools/gate.sh` was asked twice per run: by the queue before the lab was prepared and by the
wrapper immediately before the guests started (`host/c-drive-cells.txt`; per run `host/c-drive-gate.txt`). All 14
decisions were `allowed=yes`.

**Readings (GiB).** 166.44 before any work (21:26Z, the session's first command; noted in `host/c-drive.txt`
afterwards, not written by a script at that moment); 166.13 at the watcher's first reading (21:33:18Z). At the gate
before the seven guest starts, in their order: 170.11, 168.05, 167.98, 167.97, 167.97, 167.97, 167.90. Lowest and
highest of the watcher's readings: 161.05 and 173.66 (`host/c-drive-watch.txt`). Never under 40 GiB. The reading moved
by several GiB in both directions while no cell of this run wrote that much (one cell writes about 2 GB of overlay,
removed after staging); another session was working on the same machine, and what part of any change is this run's was
not established. 167.82 after the last cell was staged (23:28:24Z, `host/c-drive.txt`).

**Host power.** `tools/keepawake.ps1` held a process-level keep-awake request from 21:33:20Z to 23:28:22Z
(`host/keepawake.log`) and one idle WSL session was held (`tools/hold.sh`). The host's System log
(`host/sleep-events.txt`, Kernel-Power): no sleep entry (id 42) in the window; "entering modern standby" (506) at
21:38:17Z, during the build, and no "leaving" (507) after it up to the reading at 23:28:24Z. By that log all seven runs lie inside
one modern-standby interval. What the run's own clocks say (`host/watch-gaps.txt`): the Windows watcher wrote its line
every 30 s from 21:33:18Z to 23:28:05Z with no gap over 31 s; inside each run's wrapper window the largest gap between
two readings is 31 s; no step of any run starts more than 1 s after the step before it ended. So for each of the seven
runs: no pause of the host's execution is seen inside it, and no measured step ran after a wake from a pause. As in
set3 and set5, "modern standby" in the log did not stop execution on this host; what that state changes for a running
workload other than pausing it was not measured.

## Cells

| Run (folder) | Lab | Copy | Wrapper (UTC) | Overall | Steps not passed |
| --- | --- | --- | --- | --- | --- |
| fresh-install/set6-arch/run-a | s6-arch-fresh-a | b | 21:46:23-21:56:39 | `failed` | M5-import-absolute: failed; M10-postfix-stop: skipped |
| fresh-install/set6-arch/run-b | s6-arch-fresh-b | c | 21:58:42-22:07:55 | `complete-for-review` | M10-postfix-stop: skipped |
| fresh-install/set6-debian13/run-a | s6-d13-fresh-a | c | 22:08:20-22:19:09 | `complete-for-review` | - |
| fresh-install/set6-ubuntu/run-a | s6-ub-fresh-a | c | 22:19:39-22:37:46 | `complete-for-review` | - |
| update-alpha81/upd1-arch-good/run-a | s6-arch-good-a | c | 22:38:17-22:48:07 | `complete-for-review` | owner-continuation (required): skipped |
| update-alpha81/upd1-debian13-good/run-a | s6-d13-good-a | c | 22:48:22-23:01:16 | `complete-for-review` | owner-continuation (required): skipped |
| update-alpha81/upd1-ubuntu-good/run-a | s6-ub-good-a | c | 23:01:43-23:21:11 | `complete-for-review` | owner-continuation (required): skipped |

`checks-all.txt` lists every step, section and check of every run with its verdict and file (generated,
`tools/summary.py`): 433 checks, 432 PASS, 1 FAIL (S6-H1). `checks-not-passed.txt` lists what is not `passed` or
`observed`: that one check and its step; `M10-postfix-stop: skipped` in the two Arch fresh runs (no Postfix on Arch);
`owner-continuation (required): skipped` in the three update cells (set5's step, which runs only when an update
pauses).

## Part A: the candidate installed fresh, item x platform

A cell is PASS only when its request was sent and its answer and native readings are in the named file; the number is
how many checks stand behind it. Arch is `run-b`; `run-a` holds the same answers and readings for every item (its `A10`
has 9 of 10 checks PASS and the one S6-H1 failure). Generated: `part-a-table.md`; what each check read, quoted from the
section files: `part-a-readings.md`.

| Item | Arch (`set6-arch/run-b`) | Debian 13 (`set6-debian13/run-a`) | Ubuntu 24.04 (`set6-ubuntu/run-a`) | Raw file (under `fresh-install/<cell>/<run>/steps/`) |
| --- | --- | --- | --- | --- |
| A1. PHP site created through the Panel (200 with the new domain's id) | PASS (1) | PASS (1) | PASS (1) | `11-m1-php-site/section.json` |
| A2. `nginx -t` passes with the site's vhost; the vhost's PHP location is the generated one | PASS (2) | PASS (2) | PASS (2) | `11-m1-php-site/section.json` |
| A3. a PHP page is executed by PHP-FPM as the site's own account | PASS (2) | PASS (2) | PASS (2) | `11-m1-php-site/section.json` |
| A4. a PHP script that does not exist answers 404 | PASS (1) | PASS (1) | PASS (1) | `11-m1-php-site/section.json` |
| A5. PATH_INFO reaches PHP | PASS (1) | PASS (1) | PASS (1) | `11-m1-php-site/section.json` |
| A6. the site deleted through the Panel: nothing left serving, the site's home seen with the site and gone after | PASS (2) | PASS (2) | PASS (2) | `11-m1-php-site/section.json` |
| A7. the recorded PHP version and PHP-FPM socket are the installed version's (the created site and the imported site) | PASS (4) | PASS (4) | PASS (4) | `11-m1-php-site/section.json`, `13-m2-import/section.json` |
| A8. the cPanel-archive import completes and the imported site serves | PASS (11) | PASS (11) | PASS (11) | `13-m2-import/section.json` |
| A9. the import's answer: `imported`, `not_imported`, `left_out`, each step's `state` | PASS (4) | PASS (4) | PASS (4) | `13-m2-import/section.json` |
| A10. an absolute-path archive member is listed, the import is `partial`, the rest imported | PASS (12) | PASS (12) | PASS (12) | `14-m5-import-absolute/section.json` |
| A11. a new site the web server refuses: `502 SITE_WEB_SERVER_REFUSED`, what the answer says was removed against the guest | PASS (13) | PASS (13) | PASS (13) | `12-m6-site-refused/section.json` |
| A12. Reload of a stopped nginx: 409 `not_running`, nothing sent to the unit | PASS (7) | PASS (7) | PASS (7) | `16-m4-reload-stopped/section.json` |
| A13. Reload of a stopped MariaDB: 409 `not_running`, nothing sent to the unit | PASS (7) | PASS (7) | PASS (7) | `16-m4-reload-stopped/section.json` |
| A14. Reload of a stopped PostgreSQL: 409 `not_running`, nothing sent to the unit | PASS (7) | PASS (7) | PASS (7) | `16-m4-reload-stopped/section.json` |
| A15. Postfix Stop while `postfix check` refuses `main.cf`: 200 with the note `unit_marked_failed_config` naming the failed unit, unit still `failed`, no `reset-failed`, Start after `main.cf` is restored | NOT-MEASURED (skipped: mail is not supported on this platform) | PASS (11) | PASS (11) | `15-m10-postfix-stop/section.json` |

What the raw files show, item by item (`S/` is `steps/` of the run named in the column head):

- **A1 to A6 (PHP site).** `POST /api/v1/domains/create {"domain":"set4-php.test","project_type":"php","ssl_type":"none"}`
  answers 200 with the domain's id. `nginx -t` exits 0 with the vhost; its PHP location is the six directives, then
  `fastcgi_pass`, `SCRIPT_FILENAME` and `include fastcgi_params`. The owner's probe page (an owner action, recorded as
  such) is executed by PHP-FPM as `set4_php_test`: PHP 8.5.11 (Arch), 8.4.26 (Debian), 8.3.6 (Ubuntu). A script that
  does not exist answers 404; `/set4-probe.php/tail.php` gives `PATH_INFO=/tail.php`, `SCRIPT_NAME=/set4-probe.php`.
  After `DELETE /api/v1/domains/{id}` (200, `{"status":"deleted"}`): the domain is not listed, no domain or site row,
  no account, **the site's home `/var/www/celikpanel/subscriptions/2/sites/2` was among the site directories with the
  site (`site_home_existed_with_the_site: true`) and is not among them afterwards (`site_home_left: false`)**, no file
  of nginx names the domain, no pool file, the site's socket is not among the sockets listed afterwards
  (`socket_left: false`; the listing afterwards is `/run/php-fpm/php-fpm.sock` on Arch and the three package files
  under `/run/php/` on Debian and Ubuntu), `nginx -t` exits 0, PHP-FPM active.
- **A7 (recorded PHP version and socket).** Recorded `8.5`, `/run/php-fpm/php8.5-fpm-site2.sock` (Arch); `8.4`,
  `/var/run/php/php8.4-fpm-site2.sock` (Debian); `8.3`, `/var/run/php/php8.3-fpm-site2.sock` (Ubuntu): each is the
  pool's `listen`, the vhost's `fastcgi_pass` and an existing socket (`kind: socket`, mode 0660; on Debian and Ubuntu
  the listing spells it `/run/php/...`), and the version is the one that runs the page. The same two checks pass for
  the imported site.
- **A8, A9 (import).** `POST /api/v1/import/cpanel/apply` (`do_dns: false`) answers 200, `status: active`,
  `not_imported: []`, `left_out: ["dns"]`, `imported: [domain, files, database:s4imp_app]` on Arch and `[domain, files,
  mail, forwarders, database:s4imp_app]` on Debian and Ubuntu. Per step: `domain`, `files`, (`mail`, `forwarders`,)
  `database:s4imp_app` are `ok` without a `state`; `dns` is `ok` with `state: left_to_owner` and the detail "external
  DNS ownership preserved; verify provider records before publishing the site". Every step is in exactly one of the
  three lists. The document root's files equal the archive's, the database is on the engine with its rows, and `GET /`
  and `/index.html` with the site's Host header answer 200 with the archive's own index page (body SHA-256 equal).
- **A10 (absolute-path member).** 200, `status: partial`, `code: IMPORT_PARTIAL`, `domain_status: active`,
  `not_imported: ["member:/etc/set3-escape-absolute.txt"]`, `left_out: ["dns"]`, `imported` as in A8 with
  `database:s4abs_app`; the member's step is `ok: false` with the documented detail, the message is the documented
  sentence, nothing whose name starts with the member's prefix exists outside the import directory
  (`entries_outside: []`), the site serves the archive's index page.
- **A11 (a site the web server refuses).** Two readings per platform, as in set4 (the owner moved nginx's own
  `fastcgi.conf` away; a 58-character site name with no owner action). Both answer `502`, `code:
  SITE_WEB_SERVER_REFUSED`, `reason: removed`, `vars: {domain, command: "sudo nginx -t"}`, one line of nginx in
  `details`, and the documented sentence. What the answer says was removed, against the guest: no file of nginx names
  the domain, no account and no group, no pool file and no new socket, no domain or site row; **the home `useradd`
  logged (`/var/www/celikpanel/subscriptions/2/sites/3`, and `/sites/5` for the long name) is not among the site
  directories afterwards, which are the one site that existed before**. nginx is the same main process with a newer
  reload time and `nginx -t` exits 0. Not removed, and not claimed removed by the answer: the certificate-validation
  directory `/var/lib/celikpanel-agent/acme-http-01/subscriptions/2/domains/<id>`.
- **A12 to A14 (Reload of a stopped service).** After the Panel's own Stop, `POST /api/v1/service/action
  {"name":"<service>","action":"reload"}` answers `409 SERVICE_ACTION_FAILED`, reason `not_running`, with `vars.detail`
  "nginx.service is inactive (dead); nothing was reloaded" (and, for PostgreSQL on Debian and Ubuntu, "none of the
  units behind postgresql.service is running (postgresql@17-main.service inactive (dead)); nothing was reloaded",
  `@16-main` on Ubuntu). No journal line of any unit of the service and none of systemd about it since the request;
  systemd's record of each unit unchanged. Start brought each service back.
- **A15 (Postfix Stop, Debian and Ubuntu).** The owner added `default_process_limit = 200 # raised for the campaign`
  to `main.cf` (owner action); `postfix check` refuses it. `POST /api/v1/service/action
  {"name":"postfix","action":"stop"}` answers `200 {"applied":"stopped","outcome":"verified","success":true,"note":{...}}`
  with `note.code: SERVICE_ACTION_NOTE`, `note.reason: unit_marked_failed_config`, `vars.result: exit-code`,
  `vars.detail: "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign"`,
  and the documented sentence. **Debian 13:** `vars.failed_unit: postfix.service`, `command: sudo systemctl
  reset-failed postfix.service`; `postfix.service` `active` before, `failed` after the answer and 8 s later
  (`postfix@-.service` `inactive` throughout). **Ubuntu 24.04:** `vars.failed_unit: postfix@-.service`, `command: sudo
  systemctl reset-failed postfix@-.service`; both units `active` before; `postfix@-.service` `failed` and
  `postfix.service` `inactive` after the answer and 8 s later. On both: the master is gone after the answer, the
  product's journal of the window names no `reset-failed`, and after the owner restored `main.cf` Start answers 200,
  the master runs, no unit is `failed`, ports 25 and 587 answer. 11 of 11 checks on each
  (`fresh-install/set6-debian13/run-a/steps/15-m10-postfix-stop/section.json`,
  `fresh-install/set6-ubuntu/run-a/steps/15-m10-postfix-stop/section.json`, their `api/`, `native/`,
  `journal/postfix-since-the-stop.txt`). One Stop per platform here; set4b's three further Stops under a kernel trace
  were not repeated.

## Part B: the Panel's `Strict-Transport-Security` header

Read on the guest, as root, by `set6_trial.HEADER_SCRIPT` (Python `http.client` over TLS to `127.0.0.1:2083`, the
certificate not verified, its leaf's SHA-256 recorded; plain HTTP over a raw socket). Each cell below is `status ·
header value(s)` exactly as the answer carried them. The raw file of a reading (`headers-<when>.json`, and the same as
text in `headers-<when>.txt`) holds every answer with its status line and all its headers; `Set-Cookie` and `Cookie`
values are replaced by `[REDACTED]` on the guest (no answer of these readings set a cookie). The owner's session
cookie reaches the guest inside the script text on the SSH channel's input, never in an argument list, and is not
printed. Generated: `part-b-headers.md`.

**Fresh-install cells: the candidate.**

| Request | Arch, fresh, after setup | Arch, fresh, at the end | Debian 13, fresh, after setup | Debian 13, fresh, at the end | Ubuntu 24.04, fresh, after setup | Ubuntu 24.04, fresh, at the end |
| --- | --- | --- | --- | --- | --- | --- |
| `spa-root`: GET https://127.0.0.1:2083/ | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` |
| `spa-unknown-page`: GET https://127.0.0.1:2083/set6-no-such-page | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` |
| `spa-asset`: GET https://127.0.0.1:2083/assets/app-U1Dtbfwf.js | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` |
| `api-public`: GET https://127.0.0.1:2083/api/v1/panel/access-address | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` |
| `api-version-as-owner`: GET https://127.0.0.1:2083/api/v1/panel/version (as owner) | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` |
| `api-domains-as-owner`: GET https://127.0.0.1:2083/api/v1/domains (as owner) | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` | 200 · `max-age=31536000` |
| `api-domains-no-login`: GET https://127.0.0.1:2083/api/v1/domains | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` |
| `api-unknown-no-login`: GET https://127.0.0.1:2083/api/v1/set6-no-such-route | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` | 401 · `max-age=31536000` |
| `api-post-without-origin`: POST https://127.0.0.1:2083/api/v1/set6-no-such-route | 403 · `max-age=31536000` | 403 · `max-age=31536000` | 403 · `max-age=31536000` | 403 · `max-age=31536000` | 403 · `max-age=31536000` | 403 · `max-age=31536000` |
| `api-unknown-as-owner`: GET https://127.0.0.1:2083/api/v1/set6-no-such-route (as owner) | 404 · `max-age=31536000` | 404 · `max-age=31536000` | 404 · `max-age=31536000` | 404 · `max-age=31536000` | 404 · `max-age=31536000` | 404 · `max-age=31536000` |
| `api-options-preflight`: OPTIONS https://127.0.0.1:2083/api/v1/domains | 204 · `max-age=31536000` | 204 · `max-age=31536000` | 204 · `max-age=31536000` | 204 · `max-age=31536000` | 204 · `max-age=31536000` | 204 · `max-age=31536000` |
| `plain-http-to-the-panel-port`: GET http://127.0.0.1:2083/ | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) |
| `plain-http-to-the-panel-port-api`: GET http://127.0.0.1:2083/api/v1/panel/access-address | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) |
| `plain-http-port-80-panel-host-name`: GET http://127.0.0.1:80/ | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) |
| `plain-http-port-80-address`: GET http://127.0.0.1:80/ | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) |

**Update cells: before the update the published alpha.81 answers, after it the candidate.**

| Request | Arch, update, before the update | Arch, update, after the update | Debian 13, update, before the update | Debian 13, update, after the update | Ubuntu 24.04, update, before the update | Ubuntu 24.04, update, after the update |
| --- | --- | --- | --- | --- | --- | --- |
| `spa-root`: GET https://127.0.0.1:2083/ | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` |
| `spa-unknown-page`: GET https://127.0.0.1:2083/set6-no-such-page | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` |
| `spa-asset`: GET https://127.0.0.1:2083/assets/app-DbPgCiop.js before the update, /assets/app-U1Dtbfwf.js after it (each reading asked the fingerprinted file of the build it was reading) | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` |
| `api-public`: GET https://127.0.0.1:2083/api/v1/panel/access-address | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` |
| `api-version-as-owner`: GET https://127.0.0.1:2083/api/v1/panel/version (as owner) | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` |
| `api-domains-as-owner`: GET https://127.0.0.1:2083/api/v1/domains (as owner) | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` | 200 · `max-age=31536000; includeSubDomains` | 200 · `max-age=31536000` |
| `api-domains-no-login`: GET https://127.0.0.1:2083/api/v1/domains | 401 · `max-age=31536000; includeSubDomains` | 401 · `max-age=31536000` | 401 · `max-age=31536000; includeSubDomains` | 401 · `max-age=31536000` | 401 · `max-age=31536000; includeSubDomains` | 401 · `max-age=31536000` |
| `api-unknown-no-login`: GET https://127.0.0.1:2083/api/v1/set6-no-such-route | 401 · `max-age=31536000; includeSubDomains` | 401 · `max-age=31536000` | 401 · `max-age=31536000; includeSubDomains` | 401 · `max-age=31536000` | 401 · `max-age=31536000; includeSubDomains` | 401 · `max-age=31536000` |
| `api-post-without-origin`: POST https://127.0.0.1:2083/api/v1/set6-no-such-route | 403 · `max-age=31536000; includeSubDomains` | 403 · `max-age=31536000` | 403 · `max-age=31536000; includeSubDomains` | 403 · `max-age=31536000` | 403 · `max-age=31536000; includeSubDomains` | 403 · `max-age=31536000` |
| `api-unknown-as-owner`: GET https://127.0.0.1:2083/api/v1/set6-no-such-route (as owner) | 404 · `max-age=31536000; includeSubDomains` | 404 · `max-age=31536000` | 404 · `max-age=31536000; includeSubDomains` | 404 · `max-age=31536000` | 404 · `max-age=31536000; includeSubDomains` | 404 · `max-age=31536000` |
| `api-options-preflight`: OPTIONS https://127.0.0.1:2083/api/v1/domains | 204 · `max-age=31536000; includeSubDomains` | 204 · `max-age=31536000` | 204 · `max-age=31536000; includeSubDomains` | 204 · `max-age=31536000` | 204 · `max-age=31536000; includeSubDomains` | 204 · `max-age=31536000` |
| `plain-http-to-the-panel-port`: GET http://127.0.0.1:2083/ | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) |
| `plain-http-to-the-panel-port-api`: GET http://127.0.0.1:2083/api/v1/panel/access-address | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) | `HTTP/1.0 400 Bad Request` · (no such header) |
| `plain-http-port-80-panel-host-name`: GET http://127.0.0.1:80/ | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) |
| `plain-http-port-80-address`: GET http://127.0.0.1:80/ | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.30.5` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) | `HTTP/1.1 200 OK`, Server `nginx/1.24.0 (Ubuntu)` · (no such header) |

**The readings in short** (the twelve of the six cells' latest runs, and the two of `set6-arch/run-a`).

| Reading | Run | Panel that answered (its own version route) | HTTPS answers read | with exactly the expected value | expected value | plain-HTTP answers carrying the header | checks | raw file |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Arch, fresh, after setup | `fresh-install/set6-arch/run-b` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-arch/run-b/steps/10-b-headers-after-setup/headers-after-setup.json` |
| Arch, fresh, at the end | `fresh-install/set6-arch/run-b` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-arch/run-b/steps/17-b-headers-at-the-end/headers-at-the-end.json` |
| Debian 13, fresh, after setup | `fresh-install/set6-debian13/run-a` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-debian13/run-a/steps/10-b-headers-after-setup/headers-after-setup.json` |
| Debian 13, fresh, at the end | `fresh-install/set6-debian13/run-a` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-debian13/run-a/steps/17-b-headers-at-the-end/headers-at-the-end.json` |
| Ubuntu 24.04, fresh, after setup | `fresh-install/set6-ubuntu/run-a` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-ubuntu/run-a/steps/10-b-headers-after-setup/headers-after-setup.json` |
| Ubuntu 24.04, fresh, at the end | `fresh-install/set6-ubuntu/run-a` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-ubuntu/run-a/steps/17-b-headers-at-the-end/headers-at-the-end.json` |
| Arch, fresh, after setup (the earlier run, run-a) | `fresh-install/set6-arch/run-a` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-arch/run-a/steps/10-b-headers-after-setup/headers-after-setup.json` |
| Arch, fresh, at the end (the earlier run, run-a) | `fresh-install/set6-arch/run-a` | v0.1.0-alpha.81 `2159bb202d7b` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `fresh-install/set6-arch/run-a/steps/17-b-headers-at-the-end/headers-at-the-end.json` |
| Arch, update, before the update | `update-alpha81/upd1-arch-good/run-a` | v0.1.0-alpha.81 `a0beb7263d1f` schema 42 | 11 | 11 | `max-age=31536000; includeSubDomains` | 0 of 4 answered (4 sent) | PASS (4 of 4) | `update-alpha81/upd1-arch-good/run-a/steps/09-set6-headers-before-the-update/headers-before-the-update.json` |
| Arch, update, after the update | `update-alpha81/upd1-arch-good/run-a` | v0.1.0-alpha.82 `9cb2339f10a0` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `update-alpha81/upd1-arch-good/run-a/steps/17-set6-headers-after-the-update/headers-after-the-update.json` |
| Debian 13, update, before the update | `update-alpha81/upd1-debian13-good/run-a` | v0.1.0-alpha.81 `a0beb7263d1f` schema 42 | 11 | 11 | `max-age=31536000; includeSubDomains` | 0 of 4 answered (4 sent) | PASS (4 of 4) | `update-alpha81/upd1-debian13-good/run-a/steps/10-set6-headers-before-the-update/headers-before-the-update.json` |
| Debian 13, update, after the update | `update-alpha81/upd1-debian13-good/run-a` | v0.1.0-alpha.82 `9cb2339f10a0` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `update-alpha81/upd1-debian13-good/run-a/steps/20-set6-headers-after-the-update/headers-after-the-update.json` |
| Ubuntu 24.04, update, before the update | `update-alpha81/upd1-ubuntu-good/run-a` | v0.1.0-alpha.81 `a0beb7263d1f` schema 42 | 11 | 11 | `max-age=31536000; includeSubDomains` | 0 of 4 answered (4 sent) | PASS (4 of 4) | `update-alpha81/upd1-ubuntu-good/run-a/steps/10-set6-headers-before-the-update/headers-before-the-update.json` |
| Ubuntu 24.04, update, after the update | `update-alpha81/upd1-ubuntu-good/run-a` | v0.1.0-alpha.82 `9cb2339f10a0` schema 43 | 11 | 11 | `max-age=31536000` | 0 of 4 answered (4 sent) | PASS (5 of 5) | `update-alpha81/upd1-ubuntu-good/run-a/steps/20-set6-headers-after-the-update/headers-after-the-update.json` |

- **Over HTTPS.** The eleven requests cover the SPA root (200), an unknown page (the SPA's fallback, 200), one
  fingerprinted asset (200), the public API route `GET /api/v1/panel/access-address` (200), two API routes with the
  owner's session (200), two without it (401 `AUTH_REQUIRED`), a `POST` without an `Origin` to a route that does not
  exist (403 "cross-origin request blocked", refused before any handler), the same unknown route with the session
  (404), and an `OPTIONS` preflight (204). In the eleven readings of the candidate (eight in the fresh-install cells, the two of
  `set6-arch/run-a` included, and three after an update) every one of the eleven answers carries one `Strict-Transport-Security` line whose value is exactly
  `max-age=31536000`: no `includeSubDomains`, no `preload`. In the three readings of the published alpha.81 every one of the eleven carries exactly `max-age=31536000;
  includeSubDomains`. The Panel that answered is named by its own version route in each reading (commit and schema in
  the table above).
- **Over plain HTTP.** The Panel's process listens on one TCP port, `*:2083` (`listening_tcp_of_the_panel_process` in
  every reading), and serves TLS there. A plain-HTTP request to that port is answered `HTTP/1.0 400 Bad Request` with
  the body "Client sent an HTTP request to an HTTPS server." and no header at all: a refusal, not a redirect, in every
  reading, alpha.81 and candidate alike. Port 80 on the guest is the web server's, not the Panel's (`0.0.0.0:80` and
  `[::]:80` belong to nginx): a request there, with the Panel's host name or the address as `Host`, is answered `200`
  by nginx with a site's page or nginx's own page, without the header. Port 443 on the guest's loopback is the lab's
  fixture origin, not the product. No plain-HTTP answer of any reading carries the header.
- **The same header in the driver's own exchanges** (`hsts-in-recorded-exchanges.md`; not the reading above: the
  answers the driver's HTTPS client got on the WSL host through the SSH forward, as each `steps/*/api/*.json` holds
  them):

| Run | Part of the run | Exchanges | Header value: count | Status codes seen |
| --- | --- | --- | --- | --- |
| `fresh-install/set6-arch/run-a` | the whole run (the candidate installed fresh) | 84 | `max-age=31536000`: 84 | 200: 78, 202: 1, 409: 3, 502: 2 |
| `fresh-install/set6-arch/run-b` | the whole run (the candidate installed fresh) | 82 | `max-age=31536000`: 82 | 200: 76, 202: 1, 409: 3, 502: 2 |
| `fresh-install/set6-debian13/run-a` | the whole run (the candidate installed fresh) | 125 | `max-age=31536000`: 125 | 200: 119, 202: 1, 409: 3, 502: 2 |
| `fresh-install/set6-ubuntu/run-a` | the whole run (the candidate installed fresh) | 124 | `max-age=31536000`: 124 | 200: 118, 202: 1, 409: 3, 502: 2 |
| `update-alpha81/upd1-arch-good/run-a` | before the owner's start (the published alpha.81) | 58 | `max-age=31536000; includeSubDomains`: 58 | 200: 57, 202: 1 |
| `update-alpha81/upd1-arch-good/run-a` | the owner's start and the tracking of the update (either Panel may answer) | 23 | `(no answer)`: 5; `max-age=31536000`: 6; `max-age=31536000; includeSubDomains`: 12 | 200: 17, 202: 1 |
| `update-alpha81/upd1-arch-good/run-a` | after the update ended | 21 | `max-age=31536000`: 21 | 200: 19, 409: 2 |
| `update-alpha81/upd1-debian13-good/run-a` | before the owner's start (the published alpha.81) | 100 | `max-age=31536000; includeSubDomains`: 100 | 200: 99, 202: 1 |
| `update-alpha81/upd1-debian13-good/run-a` | the owner's start and the tracking of the update (either Panel may answer) | 23 | `(no answer)`: 5; `max-age=31536000`: 4; `max-age=31536000; includeSubDomains`: 14 | 200: 17, 202: 1 |
| `update-alpha81/upd1-debian13-good/run-a` | after the update ended | 32 | `max-age=31536000`: 32 | 200: 29, 409: 3 |
| `update-alpha81/upd1-ubuntu-good/run-a` | before the owner's start (the published alpha.81) | 127 | `max-age=31536000; includeSubDomains`: 127 | 200: 126, 202: 1 |
| `update-alpha81/upd1-ubuntu-good/run-a` | the owner's start and the tracking of the update (either Panel may answer) | 21 | `(no answer)`: 5; `max-age=31536000`: 4; `max-age=31536000; includeSubDomains`: 12 | 200: 15, 202: 1 |
| `update-alpha81/upd1-ubuntu-good/run-a` | after the update ended | 32 | `max-age=31536000`: 32 | 200: 29, 409: 3 |

  In the fresh-install runs every recorded answer carries `max-age=31536000`; in an update cell every answer before
  the owner's start carries the old value, every answer after the update ended the new one, and during the update
  both occur, in that order of time.

What this does not show: a browser. Whether a browser that holds the wider rule replaces it at its next visit, as
D-030 says by RFC 6797, was not measured. The header was read on the Panel's bootstrap certificate (the setup waits at
`access_dns`, so no certificate for a name was issued); the code path does not depend on the certificate, but a Panel
with a managed certificate was not read.

## Part C: the three good-update cells

set5's cells, driver and criteria; each ran once. Generated: `update-cells.md` (the table, and per run the answers in
full with their files).

| Run | Wrapper (UTC) | Overall | Outcome | Final state | Ledger after the update | Page loaded before the update: backup without the request id | Versioned writes without a version | Installed at the end | Header before / after | Steps not passed |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `update-alpha81/upd1-arch-good/run-a` | 22:38:17-22:48:07 | `complete-for-review` | update-verified | succeeded/update_verified | schema 43, 43 rows, as-expected | 428 `REQUEST_ID_REQUIRED`; archives [0, 0, 1]; with it 200 | backup_schedule: 409 `SETTINGS_VERSION_REQUIRED`; cron: 409 `SETTINGS_VERSION_REQUIRED`; not measured: mail_policy | agent: version=v0.1.0-alpha.82 commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3; panel: version=v0.1.0-alpha.82 commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3 | PASS / PASS | owner-continuation (required): skipped |
| `update-alpha81/upd1-debian13-good/run-a` | 22:48:22-23:01:16 | `complete-for-review` | update-verified | succeeded/update_verified | schema 43, 43 rows, as-expected | 428 `REQUEST_ID_REQUIRED`; archives [0, 0, 1]; with it 200 | backup_schedule: 409 `SETTINGS_VERSION_REQUIRED`; cron: 409 `SETTINGS_VERSION_REQUIRED`; mail_policy: 409 `SETTINGS_VERSION_REQUIRED` | agent: version=v0.1.0-alpha.82 commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3; panel: version=v0.1.0-alpha.82 commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3 | PASS / PASS | owner-continuation (required): skipped |
| `update-alpha81/upd1-ubuntu-good/run-a` | 23:01:43-23:21:11 | `complete-for-review` | update-verified | succeeded/update_verified | schema 43, 43 rows, as-expected | 428 `REQUEST_ID_REQUIRED`; archives [0, 0, 1]; with it 200 | backup_schedule: 409 `SETTINGS_VERSION_REQUIRED`; cron: 409 `SETTINGS_VERSION_REQUIRED`; mail_policy: 409 `SETTINGS_VERSION_REQUIRED` | agent: version=v0.1.0-alpha.82 commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3; panel: version=v0.1.0-alpha.82 commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3 | PASS / PASS | owner-continuation (required): skipped |

- **Verified.** Each update ended `succeeded / update_verified` (`result.json`, `outcome.final_status`) less than a
  minute after the owner's start: Arch started 22:47:03Z, read verified 22:47:51Z; Debian 22:58:10Z / 22:59:07Z;
  Ubuntu 23:18:10Z / 23:19:04Z. At the end the identity files of Agent and Panel read `version=v0.1.0-alpha.82
  commit=9cb2339f10a0445f26ad7ddcc6058a4c359133f3` on all three (`steps/NN-terminal/step.json`, `builds`).
- **(a) The ledger is the released 43**: 43 rows, schema 43, verdict `as-expected`, the five facts true (the ledger
  and schema digests equal the pinned ones, `request_identities` exists, `integrity_check` ok), on all three.
- **(b) A page loaded before the update.** `POST /api/v1/domains/{id}/backups` without `X-CelikPanel-Request-Id`:
  `428 REQUEST_ID_REQUIRED` and no archive written; with the header `200` and one new archive (archives before, after
  the refusal, after the accepted request: 0, 0, 1), on all three.
- **(c) Versioned writes without a version**: `409 SETTINGS_VERSION_REQUIRED` for the scheduled task (reason
  `scheduled_tasks`) and the backup schedule (`backup_schedule`) on all three, and for the server mail policy
  (`mail_policy`) on Debian and Ubuntu; the same writes with the version answer 200. On Arch the mail policy is not
  measured (no mail stack there).
- **Workloads during the update** (`steps/NN-verdicts/step.json`): site and cron `never-interrupted` on all three,
  SMTP `never-interrupted` on Debian and Ubuntu (not seeded on Arch), the Panel `down-only-during-transaction` with
  one outage window of 15 to 25 s (Arch, Ubuntu) and 20 to 30 s (Debian) by the guest sampler. The guests' disks were
  on the WSL disk, as in set3 and set4; set5's were in RAM, so durations are not compared with set5's.
- **Also in the Debian and Ubuntu cells, because set5's cells hold them**: set4's item 9 (a PHP site created by
  alpha.81, ten requests with the same status and body before the update, after it and after the vhost was rendered
  again; both steps `passed`), and after the cell's own verdicts one Postfix Stop on the candidate the update
  installed (`steps/23-m10-postfix-stop/section.json`): `200` with the note `unit_marked_failed_config`, `failed_unit`
  `postfix.service` (Debian) and `postfix@-.service` (Ubuntu), the unit `failed` after the answer and 8 s later, no
  `reset-failed`, Start works after `main.cf` is restored; 11 of 11 checks on each.

Not compared with set5 value by value: no comparison tool was run in this run. The outcome (`update-verified`), the
final state (`succeeded/update_verified`) and the overall (`complete-for-review`) are the ones set5's README states for
its three good cells; the verdict of every step was not compared.

## Findings

**Fails (product).** None.

**Fails (harness).** S6-H1, above: one check of `fresh-install/set6-arch/run-a`
(`steps/14-m5-import-absolute/section.json`: the request is `POST /api/v1/import/cpanel/apply` for
`set4-absolute.test`; the answer is 200, `status: partial`, `imported: ["domain","files","database:s4abs_app"]`,
`not_imported: ["member:/etc/set3-escape-absolute.txt"]`; the check's own expectation was `imported:
["domain","files","dns","database:s4abs_app"]`). The answer is the one the candidate's source documents; the cell was
repeated (`run-b`) and passes there with the same answer.

**Observations (not judged as defects here; each is in a raw file).**

1. **On Arch the default lookup path answers `::1` for `celikpanel.net`** although the hosts line names `127.0.0.1`
   only (`pinning.md`): the name shares `127.0.0.1` with `localhost`, and nss-resolve answers with `localhost`'s
   addresses. The fixture origin listens on `127.0.0.1:443` only, so a client that tries `::1` first is refused there
   and falls back; the updates on Arch ended verified. Loopback either way.
2. **A plain-HTTP request to the Panel's port gets Go's fixed `400` text, not a redirect** to HTTPS (part B). Whether
   an owner who types the address without `https://` should be redirected is not judged here; alpha.81 answers the
   same.
3. Seen again, unchanged from set4 and set5 (`part-a-readings.md`): a 58-character site name is refused by the stock
   nginx of all three platforms (`could not build server_names_hash`); the certificate-validation directory outlives
   a refused create and a delete; a missing static file of a PHP site answers 200 with the body of `/`
   (`missing_static: 200`); PATH_INFO reaches PHP only for request paths that end in `.php`.
4. **The WSL host has a rule that removes old files of `/var/tmp`** (`tmpfiles.d`: `q /var/tmp 1777 root root 30d`,
   run by `systemd-tmpfiles-clean.service`; the journal since the WSL host's start shows one run, 18 minutes after the
   start: `host/var-tmp-before-after.txt`). One old file that was in `/var/tmp` before the run is not there after it, and
   the likely cause is that rule (an inference, see "Removals" below). Not a product matter; it concerns what earlier
   runs left under `/var/tmp` on purpose (the entry `/var/tmp/cp-install-vm` is dated Sep 8 in
   `host/hostcheck-before.txt`; its contents were not listed).
5. **Setup stops at `access_dns`** in every run (a finding line of every `result.json`), as in set3 to set5: the
   guests have no public name. On Arch alpha.81's setup is run with purpose `web` in the update cell (its finding
   line says `web_mail` was not attempted on the published baseline, which, as earlier sets recorded, accepts the plan but
   fails at `05-mail_profile`; that failure was not measured in this run).

## Deviations from the brief

1. **The fresh-install archive is labelled v0.1.0-alpha.81 / 81**, not alpha.82: it is the builder's baseline role
   over `72b879eea`, tree equal to the commit's (set4's method). A fresh install of an archive labelled as the next
   release was not measured.
2. **`set6-arch` ran twice** (S6-H1): `run-a` from copy `b`, `run-b` from copy `c`. The first run is kept as it is.
3. **`celikpanel.net` is pinned as `127.0.0.1` only**, the certificate-authority names as `127.0.0.1` and `::1`; and
   a shared harness file (`worker_fixture_origin.py`) was changed so that the name could be pinned in the first step.
4. **The default lookup path was asked on all three platforms**, not on Arch only, and systemd-resolved's transaction
   counter was read around it (not asked for).
5. **The header was read on more routes than the four asked for** (eleven over HTTPS, four over plain HTTP), twice in
   each fresh-install cell, and the driver's own recorded exchanges were counted as a second source.
6. **Item 9 and one Postfix Stop also ran in the Debian and Ubuntu update cells**: set5's cells were used as they are.
7. **Base images were hard links** to the cached images (`lab.py`'s opt-in of set5); the overlays were disk-backed.
8. **More was removed than the overlay disks**, all of it created by this run and each removal listed
   (`host/removals.txt`, `host/removals-build.txt`): each lab's hard links to the cached base images, and the `src`
   directories of the eight dist directories these builds made (after the second proof; 0.84 GB each).
9. **"40 GiB"** was applied as 42 949 672 960 bytes.
10. **set5's comparison with set3 was not repeated**, and no value-by-value comparison of the three update cells with
    set5's was generated.
11. The first proofs and dry runs (copy `b`, 21:46Z) printed to the session only; their result files are in `build/`
    (`dry-b-*`), their exit lines in `host/progress.txt`. The second ones (copy `c`) are in `build/afterbuild-c.out.txt`.

## Not measured

- Anything on a screen: no browser was used; no sentence was seen rendered, and the header's effect in a browser
  (D-030: the wider rule replaced at the next visit) was not measured.
- The header on a Panel with a managed certificate or reached by a host name; over HTTP/2; on the recovery surface
  while the startup gate is closed; on a `/dbtool` proxy route; on a `5xx` answer of the Panel (the `502` answers of
  A11 are in the driver's recorded exchanges with the header, not in the guest reading).
- A fresh install of an archive labelled v0.1.0-alpha.82, and the signed release path: the archives are test-licence
  builds signed by a per-lab fixture key and served by a fixture origin on the guest's loopback; no production key, no
  real origin.
- The Postfix Stop on Arch (no Postfix there); more than one Stop per cell; set4b's kernel trace; the notes
  `unit_not_settled` and `unit_state_not_read`; Dovecot or any other unit.
- The reasons `cleanup_unconfirmed`, `import_removed` and `import_cleanup_unconfirmed`; `409
  PHP_VERSION_NOT_INSTALLED`; a reload of a unit in state `failed`, and of PHP-FPM; more than 20 refused archive
  members; the other hostile members of set3 (`..`, symbolic link); the import states `not_chosen`, `none_in_archive`
  and `none_imported`; a site with a certificate.
- set5's seven other cells (automatic return after a migration defect on three platforms and after a failed start
  check, the owner's retry of a paused update on two platforms, management off across a reboot); item 9 on Arch;
  set4's item 12; set3's Part 1.
- The guests' traffic: not captured. That no certificate authority, no licence service and no host of celikpanel.net
  was contacted rests on the pinned names (read by both lookup paths), on the Panel's own answer at the licence step
  and on the routes the drivers call, not on a capture. The names the guests asked DNS for during package
  installation are not recorded.
- Whether the host slept inside a cell is answered from the host's event log and the watcher's readings, not from a
  hardware power record.
- More than one run per cell (except `set6-arch`).

## Removals, leftovers, secrets

- **Removed by this run, each listed** (`host/removals.txt`, `host/removals-build.txt`): the overlay disks of this
  run's seven labs after each run's checksums were verified on the staged copy (12 disks; a Debian or Arch lab has
  two); the labs' hard links to the cached base images (the cached files stay, with the link counts they had:
  `host/host-leftovers.txt`); the `src` directories of the eight dist directories these builds made. Nothing else was
  removed. No directory or file of an earlier run was removed or changed; the alpha.81 dist directory of set3, the
  image cache, `/var/tmp/cp-install-vm`, `/var/tmp/cp-v3n28` and `/var/tmp/cp-pair-accept/work` were read only or not
  touched (`host/hostcheck-before.txt` lists `/var/tmp` before the run, `host/host-leftovers.txt` what is there
  after). No shared directory was chmod'ed.
- **One file that was in `/var/tmp` before the run is not there after it, and this run did not remove it**
  (`host/var-tmp-before-after.txt`): `celikpanel-license-entry60-panel` (29 484 711 bytes, dated Sep 9 23:59 local, in
  `host/hostcheck-before.txt`). No script of this run names it. The WSL host's own cleaner of temporary directories
  (`systemd-tmpfiles-clean.service`; `tmpfiles.d`: `q /var/tmp 1777 root root 30d`) ran from 21:48:25Z to 21:48:55Z, 18
  minutes after the WSL host's start and during `set6-arch/run-a`; the file had become 30 days old about 49 minutes
  before. That the cleaner removed it is an inference from the rule, the times and the absence of any other actor in
  this run's scripts, not a record of the removal itself. The same rule applies to every file under `/var/tmp` whose
  access, change and modification times are all older than 30 days, the kept directories included (observation 4).
- **Left on the WSL host, with sizes** (`host/host-leftovers.txt`): the run directory `/var/tmp/cp-set6-run` (three
  run copies, logs); the seven lab directories `/var/tmp/cp-release-drill-s6-*` without disks or images (the lab's key,
  plan, evidence and its copy of the fixture origin's files); the two builders' clones
  `/var/tmp/cp-upd1-build/20261009t213352z` and `20261009t213912z` (1.1 GB each); the eight dist directories these
  builds made under `/var/tmp/cp-pair-accept/dist/` (67 MB each). Two `__pycache__` directories are in run copies
  (written when the staging imported `set5_redact`; the cells ran with `PYTHONDONTWRITEBYTECODE=1`). The same file
  lists the processes, mounts and listening ports at the end: no QEMU process, no job process and no held session of
  this run, no tmpfs mount, no listening port of a lab.
- **Left in the repository's working tree**: this folder; the four new harness files and the change to
  `worker_fixture_origin.py`. Nothing was committed or pushed; the branch head is where it was
  (`host/working-tree-status.txt`).
- **No installed server.** `host/installed-server-names-search.txt`: the terms set4's README lists (`celikhost`,
  `boston`, `frankfurt`, `2.25.80.4`, `72.62.38.15`, `185.95.0.123`) searched in every file of this folder outside the
  README and its source: `frankfurt` occurs only in the names of five repository files (two recovery scripts and three test files) inside
  `harness-run-copy/pristine-files.sha256` (the file list of the commit's archive), the others nowhere. An absence of
  names, not a capture of traffic.
- **Secrets.** Redaction is at collection time: the pair redactor, set3's shape rules, set5's token-digest rules and
  set6's base64 pass in front of them, one more pass over every file of a run before its result is written (per run
  `set6-redaction-sweep.json`, and `set5-redaction-sweep.json` in the update cells; counts only), and one over the
  staged copy with the values of the lab's raw records known, plain and inside base64 text
  (`host/digest-sweep-at-staging.json` of each run: 0 files changed in every run). In each update cell one digest of
  the update-transaction token (the directory name under `.release-db-migrations/` in a sudo journal line) was
  replaced by `[REDACTED-SHA256]` when the file was written. No host-side record with `events_base64` exists in this
  run (the lab's reset tool writes it in the VM-reset cells, which were not run), so the base64 pass had nothing to
  replace (`base64_runs_replaced: 0` in every report) and class 10 of the scan nothing to decode (`base64 text: 0
  decoded text(s)`). Because a search that finds nothing to search shows nothing, a positive control on a made-up
  record is kept in `host/secret-scan-control.txt`: the scan finds the two digests inside its base64 text (exit 1), the
  sweep replaces them and marks the object with the digest member that no longer matches, and the scan then finds
  none (exit 0). The same behaviour is pinned by `test_set6_trial.py`.
  `secret-scan.txt` (its last line is the result) lists its ten classes at the top; class 10 decodes every base64 run
  of 40 or more characters that holds UTF-8 text, three levels deep, and searches the decoded text with the rules of
  classes 1, 2, 3, 5, 6, 7 and 8. The owner's session cookie was handed to the guest for the header reading (above);
  it is registered with the driver's redactor, the guest script never prints it, and the scan's class 8 finds no
  `Cookie` or `Set-Cookie` header with a value other than `[REDACTED]`. The lab nonce (the random identity of a
  destroyed lab guest) is in the records as in every earlier set and is not treated as a secret.

## Files

`README.md` (its text is written by hand in `tools/README.source.md`; `tools/mkreadme.py` places the tables that only
repeat values of the runs); generated by `tools/summary.py`: `cells.md`, `part-a-table.md`, `part-a-readings.md`,
`part-b-headers.md`, `hsts-in-recorded-exchanges.md`, `update-cells.md`, `pinning.md`, `packages.md`,
`checks-all.txt`, `checks-not-passed.txt`, `facts.json`; `secret-scan.txt`; `SHA256SUMS` (every file of this folder
but itself; generated last and verified); `build/` (the two builds, proofs, offline suites, dry runs);
`harness-run-copy/` (overlays, job files, queue and staging logs); `host/` (host check, disk readings, power events,
watcher gaps, progress, removals, leftovers, working-tree status); `fresh-install/<cell>/run-<x>/` and
`update-alpha81/<cell>/run-<x>/` (the raw run of each cell: `result.json`, `steps/`, the driver's `SHA256SUMS`, the
sweep reports, and `host/` with the wrapper's output, the job file, the lab's plan and the host-side records);
`tools/` (every script this run used; the local scratch-folder prefix in them is replaced by `<scratchpad>` at
staging, the scripts as run held the full path). Text files have LF line ends; `build/*/build.err.txt` are the
builder's raw logs and hold bare carriage returns inside vite's progress lines, as the same files of set3 to set5 do.


## Corrections after intake (2026-10-10 UTC)

An independent reader checked this folder against its own raw files before it was committed (read at 2026-10-10T00:02Z,
`date -u` on the Windows host; the folder name stays the UTC date of the first cell, 2026-10-09). Covered: every
`SHA256SUMS` (root and seven per-run files) with `sha256sum -c`; a fresh scan of every file for private keys, crypt and
Dovecot/SCRAM hashes, WireGuard keys, passwords, tokens, licence keys, `Cookie` / `Set-Cookie` / `Authorization` values
(every JSON and JSONL file parsed: all 894 header pairs of those names hold `[REDACTED]`), base64 runs of 80 or more
characters (none decodes to text; no `events_base64` exists) and every 32/40/64-hex string by the key it stands under;
the five names and both lookup paths of every run's pinning step, recomputed from the address lines; the header table
recomputed from all fourteen `headers-*.json` (and cross-checked with the `.txt` dumps: 210 requests, one
`Strict-Transport-Security` line per HTTPS answer, none per plain-HTTP answer, no `preload`); the recorded-exchange
table recomputed from the `api/*.json` files; the part A cells (sections, request and answer files, native readings);
part C (ledger, `428`, `409`, outcome); the trees (the good candidate's tree `f48cfe7b...` reproduced by applying
`build/a81/fixture-patches.diff` to the tree of `72b879eea`, which is `a7563a6c...`); the gate, watcher and power
files. No secret-class value was found and nothing was redacted. No local user path occurs in the folder
(no `<scratchpad>` marker was needed either). Changes made to the text:

1. Part B, update table (`README.md`, `part-b-headers.md`): the `spa-asset` row named one asset for all six columns;
   the readings before the update asked `/assets/app-DbPgCiop.js` and the readings after it `/assets/app-U1Dtbfwf.js`
   (200 each, `body_bytes` 224073 and 238308). The row label now says so.
2. Part B, "Over HTTPS": "In the nine readings of the candidate (six ... three ...; and in the two of `set6-arch/run-a`)"
   counted wrongly; it is eleven readings of the candidate (eight in the fresh-install cells, the two of
   `set6-arch/run-a` included, and three after an update).
3. Network: the resolver's counter was described as "lookups it sent to a DNS server" and as showing that the lookups of
   the pinning step sent none. The file shows the `Total Transactions` counter only (Cache Hits 0, Cache Misses
   close to the total). The text now says the ten lookups added no transaction, and that the counter stood at 26
   (Arch), 6 (Debian) and 8 (Ubuntu) when the names were pinned, with 4 timeouts on Arch and Debian, that is, the guests
   had made lookups while booting, before the first step; which names, and to which server, is not recorded. The
   sentence "as package installation needs" was an inference and was removed.
4. Guests: "WSL kernel `6.18.33.2-microsoft-standard-WSL2`" appears in no file of this folder; the text now says the
   WSL kernel version is not recorded here.
5. Observation 4: the timer ("15 minutes after each start of the WSL host and then daily") is in no file; the text now
   states what `host/var-tmp-before-after.txt` shows (the `30d` rule, one cleaner run 18 minutes after the host's start)
   and keeps the disappearance of `celikpanel-license-entry60-panel` an inference; the Sep 8 entry
   `/var/tmp/cp-install-vm` is no longer called the image cache (the image cache of this run is
   `/var/tmp/cp-v3n28/images`).
6. "No installed server": `frankfurt` occurs in five file names inside `pristine-files.sha256` (two recovery scripts,
   three test files), not three scripts.
7. Observation 5 (Arch update cell): the finding line says `web_mail` was not attempted on the published baseline; that
   it fails at `05-mail_profile` is what earlier sets recorded and was not measured here.
8. Offline suites: the five errors of the whole-suite run are named by cause (a failed `git -C <run copy>` call with
   exit status 128; a run copy is an archive, not a repository); the same five stood on copy `a` before any change.
9. Part C: "the verdict of every step ... are the ones set5's README states" said more than was compared. Only the
   outcome (`update-verified`), the final state (`succeeded/update_verified`) and the overall (`complete-for-review`)
   are stated as equal to set5's; step verdicts were not compared.

Left as it is, and why:

- Statements about the working tree and the branch head (Harness changes, "Left in the repository's working tree")
  describe the time of the run. At intake the repository's HEAD is `85aa05272` (parent `1421429f6`, which committed the
  four new harness files and the change to `worker_fixture_origin.py`; their SHA-256 values equal the ones in
  `host/working-tree-against-copy-c.txt`). `git diff 72b879eea 85aa05272 -- cmd internal web` is empty.
- `tools/README.source.md` is the source text the README was generated from and keeps the sentences corrected above;
  the scripts in `tools/` are kept as they ran.
- `PrivateTmp` and the unit `phpsessionclean.service` are shown as `[REDACTED]` in some native records (presumably a name-based
  redaction rule matched them; neither is a secret, so the value is lost, not leaked).
- `host/fixture-plan.json` of every run holds the owner's SSH public key and its comment `root@<operator-host>` (cloud-init
  user-data of the guest). A public key is not a secret; earlier committed sets hold the same line.
- The seven per-run `SHA256SUMS` list the run's `result.json`, `steps/` and sweep reports, not its `host/` directory
  or themselves; the root `SHA256SUMS` lists every file but itself.
- `du -sh` reports about 18 MB (disk blocks); the files add up to 11 665 188 bytes. No file is over 1 MB; the
  largest is `harness-run-copy/pristine-files.sha256` (644 033 bytes). No archive, image or binary is in the folder.
- Not confirmed and not refuted: that no guest traffic reached an outside host (not captured); the WSL kernel version;
  the interval of the WSL host's cleaner timer; the cause of the five whole-suite errors beyond the failed `git` call.

**Correction after publication (2026-10-10).** After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 13 occurrences in 8 files; the key material itself, a lab public key, is unchanged.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
