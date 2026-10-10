# set4 native run, 2026-10-09 (UTC): what commit `557b554eb` ("Corrections from the final native round") does on real Arch, Debian 13 and Ubuntu 24.04 guests

A short, narrow re-measurement on disposable QEMU/KVM guests of the local `archlinux` WSL host. Before this run
nothing of `557b554eb` had been measured on a real system. This folder is named with the calendar date of the run
(`date -u` on the host: `host/hostcheck-before.txt`); the labels of set1 to set3 are round labels, not dates.

Everything installed was built from commit `557b554ebdb17dee4466205eef07d004d9f23d35` (run copies are
`git archive 557b554eb` plus the harness files listed below) and, for the update baseline, from the published tag
`v0.1.0-alpha.81` (the tag's unpatched source, built with the acceptance-test licence build tag). The harness has no
route to an installed server, and no name or address of one appears in the raw files (searched for `celikhost`,
`boston`, `frankfurt`, `2.25.80.4`, `72.62.38.15` and `185.95.0.123`: no hit in any raw file; `frankfurt` occurs only in
repository script names inside `harness-run-copy/runcopy-557b554eb-files.sha256`). This is an absence of names, not a
capture of the guests' traffic. Nothing was committed, pushed, published or signed
with a production key. Every `result.json` carries `native_evidence: false`. **It closes no P0 row.** Product code
(`cmd/`, `internal/`, `web/`), `docs/` and `ROADMAP*` were not edited by this run. While the run went on, another
session edited and committed other files of the same working tree (the branch head moved from `557b554eb` to
`e2be8af30`); no build and no cell read the working tree's product files, so none of that was measured.

**Result in one sentence.** Of the thirteen items of the brief, eleven have a raw record that shows the expected
answer on every platform asked for (items 1 to 9 and 11, and the raw answer of item 12); **item 10 fails on Ubuntu
24.04, in both runs**: Postfix's Stop answers `200` without the `note` although `postfix@-.service` ends `failed`
(it passes on Debian 13, in both runs); item 13 is a feasibility note. Three things were observed that the brief did not ask for and that an
owner would meet: a site name of 58 characters is refused by the stock nginx of all three platforms; the directory
prepared for certificate validation stays after a refused create and after a delete; and after a return to the
published alpha.81 the web build that is served holds none of the update card's new sentences.

## What was built from what

Two builds (`go1.26.5 linux/amd64`, `build/go-version.txt`), every archive `license_mode: acceptance-fixture`
(`build/<name>/`: artifact document, dist JSONs, fixture commits and patches, trees, build logs).

| Build | Role | Label / seq | Commit | Tree | Archive SHA-256 |
| --- | --- | --- | --- | --- | --- |
| `cur` (10:15:29-10:20:25Z) | baseline | v0.1.0-alpha.81 / 81 | 7fbbeaa6be50dd60aa225be02b71314c2e29aafb | 69c73fae0fd79473f3f1f2b631e47b351156c9ae | d2fcf5c98c194c36bec9dd0fe2442eecb6390ba3824842f879a77f9b5debebc5 |
|  | good | v0.1.0-alpha.82 / 82 | dc0841af9eb9023ead02393a70bf59968a74ae2f | 47ab6fd9a7a1d8c2024667104db4f1f0c6817ea7 | fa3f2e0f26fe669f6d364a4c82419424cb36459606942b8891d157951891c039 |
|  | defective | v0.1.0-alpha.82 / 82 | 52208d415f61041dd9ec27bdb9cbe0b2d5c7eac4 | 14bfa1e2cd9ee62a862eb469c35f2e275cde6e62 | ea8f81c844c6bf0e08ffb6c628448bac2da56d1060b2f5cd32f19f6947eecf01 |
|  | startcheck, realstart | v0.1.0-alpha.82 / 82 | built, never installed | | |
| `a81` (10:20:25-10:23:50Z) | baseline | v0.1.0-alpha.81 / 81 | a0beb7263d1f4ca72258f6b306f9111ba4e2a334 | b1dffa78bcaca9e3514e5b2bc42f0e2cf47dca2f | 3350ff44dad2bb699ab5ee0a112b58b7bfb7fa47aa0ebdd3dd3c2da73d080109 |
|  | good | v0.1.0-alpha.82 / 82 | a8bd6124a99390246c978281992e6eaf48fbb377 | 5d34c16735dacc0eb35d7edc2a9adff3f975a076 | 219de1b848a7eccc6caca1a8946051e89a3da1a56a2feac828cfa2cdf2c2bcb7 |
|  | defective | v0.1.0-alpha.82 / 82 | c911b3083cb53aa539e6c25b720b7b86c2e08644 | 00897db48a4f87b7df42991251826a42fac46bd5 | 99a00422844f0ec376494cd17686e397373318a971d968ad2de06f5877761b29 |
|  | startcheck | v0.1.0-alpha.82 / 82 | built, never installed | | |

- **`cur`**: `run-upd1.sh build 557b554eb`. Its baseline's tree `69c73fae...` is the tree of `557b554eb`
  (`build/cur/trees.txt`, `host/hostcheck-before.txt`): the three fresh-install cells ran the candidate source with
  the acceptance licence seam switched on at build time, labelled v0.1.0-alpha.81.
- **`a81`**: `run-upd1.sh build --baseline-ref v0.1.0-alpha.81 557b554eb`. The baseline is the tag's own commit: the published tag's unpatched source, built with the
  acceptance-test licence build tag (`panel_build_tags: acceptance_license`; `patched_files: []` in `build/a81/upd1-artifacts.json`).
  **Its archive was not built by this run**: it is the one set3 built (`/var/tmp/cp-pair-accept/dist/a0beb7263...-acceptance-license`,
  same SHA-256 `3350ff44...` as in set3's README), used unchanged; the build log says so
  (`build/a81/build.err.txt`, `BUILD-UPD1-REUSED`) and the artifact document marks it (`reused_dist`). The good
  and the defective candidate are `557b554eb` labelled as the next release and were built fresh.
- `build/cur-prove.json`, `build/a81-prove.json`: `run-upd1.sh prove` exited 0 for both documents
  (`build/prove.out.txt`): archive inventory, release policy and committed-source proof of every archive against
  its own commit's blobs, the reused one included. `build/a81/baseline-ref-proof.txt`: the baseline is the tag.
- `build/web-build-sentences.json` (host reading of the archives, `tools/webbuild.py`): the web build of every
  archive built from `557b554eb` holds each of the update card's new sentences in 1 file; the web build of the
  published alpha.81 archive holds none of them (it holds "This version already failed on this server").

## Harness changes (working tree, `deploy/e2e/release-recovery/` only)

| File | What |
| --- | --- |
| `build-upd1-artifacts.sh` (changed, +14 lines) | `UPD1_REUSE_DIST_OF_COMMIT=<full commit>`: the dist an earlier run left for exactly that commit is used unchanged instead of being built; said in the build log and in the artifact document. Needed because the builder of `557b554eb` stops when the dist builder refuses an existing directory, and this run may not remove set3's. Never implicit. |
| `set4_trial.py` (new) | The driver of this run. `Set4Trial` over set3's `RequestIdentityTrial` (cells `set4-arch`, `set4-debian13`, `set4-ubuntu`): sections M0, M1, M6, M2, M5, M10, M4. `Set4UpdateTrial` over `owner_update_trial.Trial`: the set3 update cells with one step added before the pre-update state (good cells), and one before the collection (good and rollback cells). No file of set3's drivers was changed. |
| `guest_set4_native.py` (new) | Guest helper: read-only readers (platform and packages, the nginx snippet, `nginx -t`, the files of nginx that name a domain, one GET on loopback, what exists on the server for a domain name, systemd's record of a unit with its journal since a moment, the lab's isolation from the guest's hosts file, the installed web builds) and two owner actions (upload one PHP page and one text file; rename nginx's own `fastcgi.conf` and rename it back). |
| `run-set4.sh` (new) | Wrapper: one cell per new lab, as `run-set2.sh` and `run-upd1.sh`. |

Run copies (`harness-run-copy/`: `runcopy-557b554eb-files.sha256`; per overlay `files.sha256`, `harness.diff`,
`differs-from-archive.txt`; job files `jobs/`; `queue.log`): `b` = the archive + the builder (both builds, `prove`);
`c` = `b` + the three new files; `d` = `c` + H43; `e` = `d` + H44; `f` = `e` + H45 = the working tree's four files
(SHA-256 equal, `harness-run-copy/overlay-f/files.sha256`). Offline suites on `c`, `d`, `e` and `f`, all OK (`build/offline-<copy>-*.txt`): `test_owner_update_trial` 191, `test_settings_writes_trial`
30, `test_request_identity_trial` 17, `test_recovery_candidate_archive` 11, `test_lab` 15, `test_guest_probe` 13;
the dry runs of the cells exited 0 and created no lab (`build/dry-*.json`). The new driver has no offline suite of
its own.

Harness defects found during the run (the edits are `tools/patch2.py`, `tools/patch3.py`, `tools/patch4.py`):

| Id | Defect | Handling | Runs |
| --- | --- | --- | --- |
| H43 | The step after the update expected the General settings save to change the vhost file. The candidate's Panel renders every hosted vhost when it starts (journal: "certificate startup reconcile: restored 2 hosted vhosts with one nginx validation and reload"), so the file had been rendered by the update itself and the save wrote the same bytes. | The rule now requires that the vhost in service is no longer the baseline's and reads the save from the file's inode and times and from nginx's journal. | `update-alpha81/upd1-debian13-good/run-a`: its one failed step is this (every recorded fact of it is the expected one); fixed from copy `d` (Ubuntu run-a, Debian run-b) |
| H44 | The reader of the installed web build looked for `web/dist/index.html`; the installer puts the build at `/opt/celikpanel/web`. The build the Panel serves was not read. | The reader finds both layouts and marks the directory the running Panel serves. | both `upd1-arch-defective/run-a` cells (their `served-web-build.json` reads only the lab's own source copy); fixed from copy `e` (`update-alpha81/upd1-arch-defective/run-b`) |
| H45 | The reader of what is left for a domain looked for the site's home under `subscriptions/<s>/domains/<id>`; the product's home is `subscriptions/<s>/sites/<id>`. The listing was empty before and after, so "the home is gone" and "no new site directory" passed without having measured anything. Also, after a delete on Debian and Ubuntu the socket was looked for under the pool's `/var/run/...` spelling while the listing spells it `/run/...`. Found while this README was written, from run-a's own records. | The listing reads `*/sites/*`; a delete must first have seen the home with the site; a refusal also names the home `useradd` logged and looks for it afterwards; the socket path is normalised. | **the three fresh-install cells run-a: their checks of items 1, 6 and 8 did not measure the site's home (nor, on Debian and Ubuntu, the socket after the delete)**; everything else of those cells stands. The three cells were run again from copy `f` (run-b), and the table below cites run-b for those items |

## Guests, isolation, disk

Base images (set3's image cache `/var/tmp/cp-v3n28/images`, read-only; digests from each cell's
`host/fixture-plan.json`): `Arch-Linux-x86_64-cloudimg-20260815.573966.qcow2` (sha256 `5d8be8d2...`),
`debian-13-genericcloud-amd64-20260826-2582.qcow2` (sha512 `184761b0...`),
`ubuntu-24.04-server-cloudimg-amd64-20260826.img` (sha256 `d0fe84bb...`). One new lab per cell, stopped by the
wrapper; the cells ran one after the other. Packages are the distributions' own, installed by the product's setup
during the cell (each cell's `steps/08-m0-prepare/native/*-platform.json`):

| | Arch | Debian 13 | Ubuntu 24.04.4 |
| --- | --- | --- | --- |
| kernel | 7.2.9-arch1-1 | 6.12.105+deb13-cloud-amd64 | 6.8.0-138-generic |
| nginx | 1.30.5-1 | 1.26.3-3+deb13u9 | 1.24.0-2ubuntu7.18 |
| PHP-FPM | php-fpm 8.5.11-1 (one unversioned unit) | php8.4-fpm 8.4.26-1~deb13u1 | php8.3-fpm 8.3.6-0ubuntu0.24.04.11 |
| MariaDB / PostgreSQL | 13.0.2-2 / 18.6-2 | 1:11.8.6-0+deb13u1 / 17.11-0+deb13u1 | 1:10.11.14-0ubuntu0.24.04.1 / 16.15-0ubuntu0.24.04.1 |
| Postfix | not installed (mail is not supported on this platform) | 3.10.13-0+deb13u1 | 3.8.6-1ubuntu0.1 |
| systemd | 262-1 | 257.13-1~deb13u1 | 255.4-1ubuntu8.17 |

Setup profile as in set3: Debian/Ubuntu purpose `web_mail` with `nginx, php-fpm, mariadb, postfix, dovecot,
roundcube, rspamd, postgresql`; Arch purpose `web` with `nginx, php-fpm, mariadb, postgresql`; DNS mode external.

**Name pinning, licence step and certificate routes (this is not network isolation).** The guests run on QEMU user
networking with outbound NAT (`host/fixture-plan.json` of each cell: `-netdev user,id=mgmt,hostfwd=...`, no
`restrict=on`), and packages are installed from the distributions' repositories during a cell. No file records
the guests' traffic, so nothing here shows that a guest could not reach the outside. What is established:
(1) name pinning in each guest's hosts file for the release origin: `steps/02-origin/step.json` of every cell,
`celikpanel.net` resolves to `127.0.0.1` only, where the lab's own fixture origin answers (`loopback_only: true`);
and, in the three fresh-install cells, for the two Let's Encrypt directory names (`steps/08-m0-prepare/section.json`
`isolation` and its two `*-isolation*.json` readings: they resolve to this guest's loopback only, asked with
`getent -s files hosts NAME`, which reads the hosts file only and sends no DNS question); the update cells do not add
the Let's Encrypt lines (as in set3); (2) the licence step was answered by the acceptance-test build without
contacting a licence service (`steps/05-license/step.json`, the Panel's own answer: `license_service: "not contacted:
acceptance test build"`); (3) no certificate or ACME route was called by any cell (no API exchange of any cell names
a certificate, SSL or ACME route; the only licence route used is the acceptance-fixture activation). In the five
Arch cells `steps/02-origin/step.json` prints `127.0.0.1 localhost` for `getent`, where Debian and Ubuntu print
`127.0.0.1 celikpanel.net`; the address is the same and Arch's later reading prints `celikpanel.net`, but the
difference is not explained here.

**Disk (Windows `C:`, PowerShell `Get-PSDrive C`; `host/c-drive.txt`, `host/c-drive-cells.txt`,
`host/c-drive-watch.txt`).** 76.5 GiB before the first cell; before the twelve cells, in their order: 76.5, 73.5,
72.9, 71.5, 69.1, 67.3, 79.4, 78.0, 77.0, 76.2, 74.4, 73.7; lowest of the readings taken every 30 s: 66.9 GiB.
Never under 40 GiB before a cell. The watcher's readings have no gap larger than 31 s from 10:29Z to the end
(331 readings), so the host did not stop executing during the cells; Windows power events were not collected
in this run.

## Cells

| Run (folder) | Lab | Copy | Wrapper (UTC) | Overall | Steps or sections not passed |
| --- | --- | --- | --- | --- | --- |
| set4-arch/run-a | s4-arch-a | c | 10:29:21-10:37:26 | `complete-for-review` | M10 skipped (no mail on this platform) |
| set4-debian13/run-a | s4-d13-a | c | 10:37:53-10:49:27 | `complete-for-review` | - |
| update-alpha81/upd1-debian13-good/run-a | s4-u9-d13-a | c | 10:49:31-11:04:46 | `failed` | set4-php-site-after-the-update: failed (H43, a harness rule) |
| set4-ubuntu/run-a | s4-ub-a | c | 11:04:52-11:28:01 | `failed` | M10-postfix-stop: failed (item 10, product) |
| update-alpha81/upd1-ubuntu-good/run-a | s4-u9-ub-a | d | 11:28:07-11:48:00 | `complete-for-review` | - |
| update-alpha81/upd1-arch-defective/run-a | s4-u12-arch-a | d | 11:48:04-11:58:50 | `complete-for-review` | - |
| update-candidate-baseline/upd1-arch-defective/run-a | s4-u12c-arch-a | d | 11:58:53-12:07:54 | `complete-for-review` | - |
| update-alpha81/upd1-debian13-good/run-b | s4-u9-d13-b | e | 12:08:22-12:20:41 | `complete-for-review` | - |
| update-alpha81/upd1-arch-defective/run-b | s4-u12-arch-b | e | 12:20:44-12:30:51 | `complete-for-review` | - |
| set4-arch/run-b | s4-arch-b | f | 12:35:55-12:43:26 | `complete-for-review` | M10 skipped (no mail on this platform) |
| set4-debian13/run-b | s4-d13-b | f | 12:43:29-12:53:50 | `complete-for-review` | - |
| set4-ubuntu/run-b | s4-ub-b | f | 12:53:54-13:13:08 | `failed` | M10-postfix-stop: failed (item 10, product; the same three checks as run-a) |

`checks-all.txt` lists every check of every cell with its verdict and file (generated, `tools/summary.py`);
`checks-not-passed.txt` the ones that did not pass; `facts.json` the values this README quotes (`tools/mkvalues.py`).

## Item x platform

A cell is PASS only when its request was sent and its answer and native readings are in the named file. Paths are
below this folder; `S/` is `steps/`. Every answer of the Panel was the same in run-a and run-b of a platform (status,
code, reason; `facts.json`); the sections below quote run-b where the two differ in what was read on the guest.

| Item | Arch | Debian 13 | Ubuntu 24.04 | Raw file (per platform folder `set4-<p>/run-b/`; run-a holds the same answers, with the limit of H45) |
| --- | --- | --- | --- | --- |
| 1 / 8. PHP site created through the Panel; `nginx -t`; a PHP page executed as the site's account; missing script 404; PATH_INFO; deleted, nothing left serving | PASS | PASS | PASS | `S/09-m1-php-site/section.json`, `native/`, `native-text/php-site-vhost.conf.txt`, `api/` |
| 2. cPanel-archive import of set3's fixture completes; the imported site serves | PASS | PASS (not asked) | PASS (not asked) | `S/11-m2-import/section.json` |
| 3. Recorded PHP version and PHP-FPM socket are the installed version's | PASS (8.5) | PASS (8.4, not asked) | PASS (8.3, not asked) | `S/09-m1-php-site/section.json` `php_version`; `S/11-m2-import/section.json` `php_version` |
| 4 / 11. Reload of a stopped nginx, MariaDB, PostgreSQL: 409 `not_running`, nothing sent to the unit | PASS | PASS | PASS | `S/14-m4-reload-stopped/section.json`, `journal/*-since-the-refused-reload.txt` |
| 5. Absolute-path member listed, import `partial`, the rest imported; the two declared deviations | PASS | PASS (not asked) | PASS (not asked) | `S/12-m5-import-absolute/section.json`; deviation 1 also `S/11-m2-import/section.json` |
| 6 / 11. Web server refuses a new site: `502 SITE_WEB_SERVER_REFUSED`, the answer against the guest | PASS (two readings) | PASS (two readings) | PASS (two readings) | `S/10-m6-site-refused/section.json`, `journal/` |
| 7. SHA-256 of the package's `snippets/fastcgi-php.conf` against the pinned one | the file does not exist | equal | equal | `S/08-m0-prepare/section.json` `nginx_php_snippet`, `native-text/nginx-snippets-fastcgi-php.conf.txt` |
| 9. Site created by published alpha.81, update to the candidate, vhost rendered again, same answers | not asked | PASS (`update-alpha81/upd1-debian13-good/run-b`; run-a: same facts, step failed by H43) | PASS (`update-alpha81/upd1-ubuntu-good/run-a`) | `S/08-set4-php-site-before-the-update/step.json`, `S/17-set4-php-site-after-the-update/step.json`, their `native/` and `native-text/` |
| 10. Postfix Stop while `postfix check` refuses `main.cf`: 200 with `note`, unit left `failed`, Start after the correction | NOT-MEASURED (no Postfix) | PASS (run-a and run-b) | **FAIL** (no `note`; run-a and run-b) | `S/13-m10-postfix-stop/section.json`, `journal/postfix-since-the-stop.txt`, `api/` |
| 12. Update check after an automatic return | recorded (three cells) | not run | not run | `update-alpha81/upd1-arch-defective/run-a` and `run-b`, `update-candidate-baseline/upd1-arch-defective/run-a`: `S/15-set4-update-check-after-the-return/update-check-raw.json.txt`, `step.json` (for `update-candidate-baseline/upd1-arch-defective/run-a` the folder is `S/14-set4-update-check-after-the-return/`) |
| 13. Mail-certificate renewal cell: feasibility only | - | - | - | the section "Item 13" below |

## What the raw files show, item by item

**Items 1 and 8 (PHP site).** `POST /api/v1/domains/create {"domain":"set4-php.test","project_type":"php","ssl_type":"none"}`
answers 200 with the domain's id on the three platforms. `nginx -t` exits 0 with the vhost in place. The vhost
names no file under `snippets/`; its PHP location is the six directives, then `fastcgi_pass`, `SCRIPT_FILENAME` and
`include fastcgi_params` (text kept in `native-text/php-site-vhost.conf.txt`). The owner uploaded `set4-probe.php`
and `set4-static.txt` (recorded as an owner action). Requests on the guest's loopback with the site's Host header:

| Request | Arch | Debian 13 | Ubuntu 24.04 |
| --- | --- | --- | --- |
| `/set4-probe.php` | 200, executed, PHP 8.5.11, `fpm-fcgi`, runs as `set4_php_test` | 200, 8.4.26, `set4_php_test` | 200, 8.3.6, `set4_php_test` |
| `/set4-missing.php` | 404 (nginx's own page) | 404 | 404 |
| `/set4-probe.php/tail.php` | 200, `PATH_INFO=[/tail.php]`, `SCRIPT_NAME=[/set4-probe.php]` | the same | the same |
| `/set4-probe.php/a/b/tail.php?q=1` | 200, `PATH_INFO=[/a/b/tail.php]` | the same | the same |
| `/set4-probe.php/extra/info` | 200, **not the probe**: the site's `index.php` | the same | the same |
| `/set4-static.txt` | 200, the file | 200 | 200 |
| `/set4-none.txt`, `/` | 200, the site's `index.php` | the same | the same |
| `/.env` | 403 | 403 | 403 |

PATH_INFO reaches PHP only when the request path itself ends in `.php` (the vhost's PHP location is `location ~ \.php$`);
a path such as `/set4-probe.php/extra/info` is taken by `location /` and handed to `/index.php`. Item 9 shows the same
answers for these requests with the vhost alpha.81 generated, so this is not a change of `557b554eb`. Whether it is
what the brief means by "PATH_INFO reaches PHP" is the owner's to judge; the table gives both forms.

After `DELETE /api/v1/domains/{id}` (200, `{"status":"deleted"}`), run-b: the domain is not listed, no `domains` or
`sites` row, no system account or group, the site's home `/var/www/celikpanel/subscriptions/2/sites/2` (seen with
the site) is no longer among the site directories, no file of nginx names the domain and `nginx -T` has no line
with it, no pool file, the socket is not in `/run/php*`, `nginx -t` exits 0, PHP-FPM active. The same requests then reach the
server's default site (Arch: the owner's static site answers 403 for `.php`; Debian and Ubuntu: 404), none executes
the probe. Left on the server: `audit_logs` rows that name the domain, and the directory
`/var/lib/celikpanel-agent/acme-http-01/subscriptions/2/domains/2` (see "Observations").

**Item 2 (import).** The fixture is set3's (`owner-cpmove-fixture`, the archive as tar writes it, with the
`homedir/public_html/` directory member). `POST /api/v1/import/cpanel/apply` answers 200, `status: active`,
`imported: [domain, files, dns, database:s4imp_app]` on Arch and with `mail, forwarders` on Debian and Ubuntu,
`not_imported: []`; the document root's files equal the archive's (names, sizes, digests), the database is on the
engine with 300 rows. `GET /` and `/index.html` with the site's Host header answer 200 with the archive's own index
page (body SHA-256 equal). The imported site is a PHP site; the same probe requests as in item 1 were asked of it,
with the same results (`imported-site` checks of that section).

**Item 3.** Arch: `sites.php_version` is `8.5`, `sites.php_fpm_socket` `/run/php-fpm/php8.5-fpm-site2.sock`, which is
the pool file's `listen` (`/etc/php/php-fpm.d/site2.conf`), the vhost's `fastcgi_pass` and an existing socket
(`http:http`, 0660); the page reports PHP 8.5.11; `GET /api/v1/domains` answers `php_version: "8.5"`. The imported
site: `8.5`, `php8.5-fpm-site6.sock`. Debian 13: `8.4`, `/var/run/php/php8.4-fpm-site2.sock`. Ubuntu: `8.3`,
`/var/run/php/php8.3-fpm-site2.sock`. On no platform is a version recorded that is not the installed one.

**Items 4 and 11 (reload of a stopped service).** Each service was stopped with the Panel's own Stop (200). Then
`POST /api/v1/service/action {"name":"<service>","action":"reload"}` answers `409 SERVICE_ACTION_FAILED`, reason
`not_running`, on the three platforms for nginx, MariaDB and PostgreSQL; `vars.detail` is, for example,
"nginx.service is inactive (dead); nothing was reloaded" and, for the PostgreSQL wrapper of Debian and Ubuntu,
"none of the units behind postgresql.service is running (postgresql@17-main.service inactive (dead)); nothing was
reloaded". Since the moment before the request: `journalctl -u <unit>` has no line for any unit of the service, PID
1 logged no line naming it, and `ActiveState`, `SubState`, `InvocationID`, the state timestamps, `ExecReload`,
`NRestarts` and `MainPID` of each unit are unchanged; the Agent's journal has the refusal ("ERROR service reload
nginx: failed not_running: ..."). Start brought each service back. Not measured: a unit in state `failed`, PHP-FPM,
Dovecot and Postfix (the last two were set3's).

**Item 5 (absolute-path member).** The archive holds set3's hostile member `/etc/set3-escape-absolute.txt`. The
answer, the same on the three platforms but for the parts mail adds: 200, `status: partial`, `code: IMPORT_PARTIAL`,
`domain_status: active`, `imported: [domain, files, (mail, forwarders,) dns, database:s4abs_app]`,
`not_imported: ["member:/etc/set3-escape-absolute.txt"]`; the step `member:/etc/set3-escape-absolute.txt` has
`ok: false` and the documented detail; `message` is the documented sentence ("The import ended and every part that
was chosen was imported; set4-absolute.test is in service. ... nothing continues by itself."). On the guest: the
document root's files equal the archive's site files, the database has its rows, the domain's row is `active`,
nothing whose name starts with the member's prefix exists outside the import directory, the site serves the
archive's index page. The two declared deviations are what the product answers: (1) the files step's line of both
imports says "6 other entries of the archive are outside the site folder (homedir/public_html) and are not copied
by this step: homedir/etc (2), mysql (2), cp (1), va (1). ..." and the import without a refused member is `active`;
(2) the result that misses only the refused entry has `domain_status: active`. Not measured: more than 20 refused
members (`members:<n>`), and the screen.

**Items 6 and 11 (a site the web server refuses).** Run right after item 1, while no PHP site existed. Two readings
per platform, both `POST /api/v1/domains/create` with `project_type: php`:

- *owner-moved-file*: the owner renamed nginx's own `/etc/nginx/fastcgi.conf` to `fastcgi.conf.set4-owner-moved`
  (owner action `owner-nginx-file`). `nginx -t` still exits 0 with the configuration in service (recorded before
  the request), because nothing in service reads that file; the generated PHP vhost includes it.
- *long-site-name*: no owner action; the site name is
  `set4-a-long-site-name-an-owner-could-plausibly-choose.test` (58 characters).

Both answer `502`, `code: SITE_WEB_SERVER_REFUSED`, `reason: removed`, `vars: {domain, command: "sudo nginx -t"}`,
`details` with one line of nginx, and `error` equal to the documented sentence, on the three platforms. The lines:
`[emerg] open() "/etc/nginx/fastcgi.conf" failed (2: No such file or directory) in /etc/nginx/sites-enabled/set4-refused.test.conf:31`
and `[emerg] could not build server_names_hash, you should increase server_names_hash_bucket_size: 64`.

What the answer says was removed, against the guest after the answer (`native` of each reading): no file of nginx
names the domain and `nginx -T` has no line with it (web server configuration); no account and no group of the
site's name, although the journal shows `useradd` made them (system account); the home `useradd` logged
(`/var/www/celikpanel/subscriptions/2/sites/3`, and `/sites/5` for the long name) is not among the site directories
afterwards, which are the one site that existed before (files; run-b only, H45); no pool file names the account and `/run/php*` has no new
socket (PHP pool); no `domains` or `sites` row, the row counts are unchanged, the autoincrement moved by one, the
domain is not listed. nginx is the same main process with a newer `ExecReload` time and `nginx -t` exits 0 (reloaded
with the configuration it had before). **Not removed, and not claimed removed by the answer:** the directory
`/var/lib/celikpanel-agent/acme-http-01/subscriptions/2/domains/<id>` that the Agent prepared for the site. The
reason `cleanup_unconfirmed` and the import's two reasons were not reached. After the owner renamed the file back,
the same request created the site (200), which was then deleted through the Panel.

**Item 7.** Debian 13 (`nginx-common` 1.26.3-3+deb13u9) and Ubuntu 24.04 (`nginx-common` 1.24.0-2ubuntu7.18): the
file is 423 bytes, owned by `nginx-common` (`dpkg -S`), SHA-256
`a9dd98bf9631d727f0a846a9c7f4fe6193468a714c782df26d5cc9a7756411f2`, which is `debianFastCGIPHPSnippetSHA256` in
`internal/services/nginx_php_handoff_test.go` at `557b554eb`. Arch: `/etc/nginx/snippets` does not exist.

**Item 9 (a site of the published alpha.81 across the update).** Published alpha.81 (`a0beb7263`, schema 42)
created `set4-upd-php.test` as a PHP site; its vhost's PHP location is `include snippets/fastcgi-php.conf;`, the
socket, `SCRIPT_FILENAME`, `include fastcgi_params;`. The ten requests of item 1 were asked. The owner started the
update to the candidate by the harness's owner path (`GET /api/v1/panel/update/check`, `POST .../update/start`); it
ended verified at v0.1.0-alpha.82 (`a8bd6124a`, schema 43; steps `owner-start` to `post-update-facts` passed).

- **The vhost was rendered again by the update itself.** Read after the update and before any owner action, the
  vhost file is already another file (Debian: SHA-256 `551945c2...` before, `83bd07de...` after): only the
  `include snippets/fastcgi-php.conf;` is gone; the six directives are in the file and `include fastcgi.conf;` and
  `include fastcgi_params;` remain; the product's journal has "certificate startup reconcile: restored 2 hosted
  vhosts with one nginx validation and reload" (`S/18-collect/journal-product.txt`). `nginx -t` exits 0.
- **The Panel action that renders it again:** the site's General settings saved unchanged
  (`GET /api/v1/domains/{id}/general`, then `POST` of the same `document_root`, `web_server`, `redirect_www`;
  `updateDomainGeneralSettings` applies the vhost). It answers 200 `{"status":"success"}`; the vhost file has a new
  inode and new times and the same bytes (Debian run-b: inode 141297 to 135271; Ubuntu: 262668 to 263653), nginx's
  journal has one reload at that moment, `nginx -t` exits 0.
- **Same answers.** For each of the ten requests the status and the body SHA-256 are equal before the update,
  after the update and after the save (`answers_before_the_update_and_after_it`,
  `answers_before_the_update_and_after_the_save` in the step's `step.json`), on Debian 13 (run-a and run-b) and on
  Ubuntu 24.04. The page is executed as `set4_upd_php_test` by PHP 8.4.26 (Debian) and 8.3.6 (Ubuntu) at each reading.

Debian run-a holds the same facts; its step is `failed` only by the rule H43 corrected afterwards, and it has no
inode reading of the save. Not measured: a site with a certificate, an owner-edited snippet file, Arch (alpha.81
cannot create a PHP site there).

**Item 10 (Postfix Stop while `postfix check` refuses `main.cf`).** The owner added the line
`default_process_limit = 200 # raised for the campaign` to `main.cf` (owner action); `postfix check` refuses it.
`POST /api/v1/service/action {"name":"postfix","action":"stop"}`:

- **Debian 13: PASS (run-a and run-b).** 200 `{"applied":"stopped","outcome":"verified","success":true,"note":{...}}` with
  `note.code: SERVICE_ACTION_NOTE`, `note.reason: unit_marked_failed_config`, `note.vars: {unit: postfix,
  failed_unit: postfix.service, result: exit-code, command: "sudo systemctl reset-failed postfix.service", detail:
  "postfix: fatal: bad numerical configuration: default_process_limit = 200 # raised for the campaign"}`, and the
  documented sentence. `postfix.service` was `active` before and is `failed` (result `exit-code`) after the answer
  and 8 seconds later; the master is gone; the product's journal names no `reset-failed`.
- **Ubuntu 24.04: FAIL, in run-a and in run-b.** 200 `{"applied":"stopped","outcome":"verified","success":true}`:
  **no `note`.** On the guest `postfix.service` (the wrapper) is `inactive`, result `success`, and `postfix@-.service` (the daemon's unit)
  is `failed`, result `exit-code`, after the answer and 8 seconds later; it was `active` before. The journal:
  `postfix@-.service` "Stopping" at 11:26:39.729, `postmulti` "fatal: bad numerical configuration" at .734,
  "Control process exited, code=exited, status=1/FAILURE" at 11:26:40.869, master "terminating on signal 15" at
  .872, "Failed with result 'exit-code'" at .876. So the unit an owner will see as failed is not named to them.
  The Agent's source does watch `postfix@-.service` (`cmd/agent/service_action_verify.go`, `stopWatchedUnits`);
  the journal's order is consistent with the unit being read once, before systemd marked it failed (the mark comes
  4 ms after the master ends). That cause is an inference from the timestamps, not a measurement. Run-b has the
  same order (13:11:37.169 "Stopping", 13:11:38.180 control process exited, .183 master terminating, .188 "Failed
  with result 'exit-code'") and the same answer.
  Raw: `set4-ubuntu/run-a/steps/13-m10-postfix-stop/section.json` (`postfix_stop`),
  `api/0149-post-api-v1-service-action.json`, `journal/postfix-since-the-stop.txt`,
  `native/097-postfix-units-after-stop.json`, `native/098-postfix-units-8s-after-stop.json`; and the same files
  under `set4-ubuntu/run-b/` (the exchange is `api/0117-post-api-v1-service-action.json`).

On both platforms, after the owner restored `main.cf`, Start answers 200 success, the master runs, no unit is
`failed`, and ports 25 and 587 answer. No `reset-failed` was issued on either: the product's journal names none, and
the unit was read `failed` at the stop (`native/097`), 8 s later (`native/098`) and immediately before Start
(`native/100`), in run-a and run-b. On Debian that unit is `postfix.service` (`postfix@-.service` `inactive`); on
Ubuntu it is `postfix@-.service` (`postfix.service` `inactive`).

**Item 12 (the update check after an automatic return).** Arch, the migrate-only defective candidate with the
second fault of set3 (SIGKILL at `runtime_verified`); each cell ended `recovered` / `rollback_verified`. The raw
answer of `GET /api/v1/panel/update/check` afterwards (`update-check-raw.json.txt`), published alpha.81 running:

```json
{"supported":true,"available":true,"current_version":"v0.1.0-alpha.81","current_commit":"a0beb7263d1f4ca72258f6b306f9111ba4e2a334","target":{"version":"v0.1.0-alpha.82","commit":"c911b3083cb53aa539e6c25b720b7b86c2e08644","sequence":"82","os":"linux","arch":"amd64","archive_sha256":"99a00422844f0ec376494cd17686e397373318a971d968ad2de06f5877761b29","archive_size":"65894854","published_at":"2026-10-09T11:49:18Z"},"previous_attempt":{"request_id":"80203f4d033d4a1a404e4265c3bb0673","phase":"recovered","finished_at":"2026-10-09T11:58:37Z"}}
```

With the candidate source as the running version (`update-candidate-baseline`: baseline `7fbbeaa6b` = the tree of
`557b554eb`, defective `52208d415`) the answer has the same shape:
`"previous_attempt":{"request_id":"06acf6c45f86201439f599fd34127534","phase":"recovered","finished_at":"2026-10-09T12:07:43Z"}`.

- The data the card's notice is built from is `previous_attempt`: `phase: recovered`, `finished_at`, the request's
  id, and **no `failure_code`** for this defect (the recovery reader of the same guest says
  `previous_failure: update_failed`). With `phase: recovered` the candidate's card chooses the heading
  `panelUpdate.previousAttempt.rolledBackTitle` and, without a `failure_code`, `...noCause`
  (`web/src/components/PanelUpdateCard.tsx` at `557b554eb`); the step records these sentences from the catalogue.
- The answer carries one time, `finished_at`. The card's source passes it as `{time}` to the sentence
  "{version} was started here on {time}. ..."; in the candidate-baseline cell the owner's start was at 12:06:13Z
  and `finished_at` is 12:07:43Z. Not rendered in a browser here.
- **After a return to the published alpha.81 the new heading cannot be shown:** the web build the running Panel
  serves there (`/opt/celikpanel/web`, 97 files read) holds none of the four new sentences (run-b,
  `native/001-served-web-build.json`); alpha.81's catalogue has "This version already failed on this server" and
  its earlier `recovered` sentence. In the candidate-baseline cell the served directory was not read (H44); the
  archive that was installed there holds each sentence in 1 file (`build/web-build-sentences.json`).
- Not measured: a return with a typed cause (the start-check candidate), a failed attempt without a return, the
  rendered card.

## Findings

**Fails (product).**

1. **Item 10, Ubuntu 24.04:** a Stop of Postfix that leaves `postfix@-.service` marked `failed` answers 200 without
   the `note`, in both runs. Request and answer above; raw files
   `set4-ubuntu/run-a/steps/13-m10-postfix-stop/section.json` and `set4-ubuntu/run-b/steps/13-m10-postfix-stop/section.json`.

**Observations (not judged as defects here; each is in a raw file).**

1. **A long site name is refused by the stock nginx of all three platforms.** With no owner action, a PHP site
   named with 58 characters answers `502 SITE_WEB_SERVER_REFUSED` / `removed`; nginx's line is "could not build
   server_names_hash, you should increase server_names_hash_bucket_size: 64". `sudo nginx -t` passes afterwards, so
   the sentence sends the owner to "what nginx refused was in the configuration CelikPanel generated"; creating the
   site again gives the same answer. The longest name that is accepted was not measured.
   (`S/10-m6-site-refused/section.json`, `refused.long-site-name`.)
2. **The certificate-validation directory outlives the site.** After a refused create, and after a delete through
   the Panel, `/var/lib/celikpanel-agent/acme-http-01/subscriptions/<s>/domains/<id>` is still there (three
   platforms). No answer claims it was removed; the product's own comment says a deletion does not remove it.
3. **PATH_INFO** reaches PHP only for request paths that end in `.php` (item 1); unchanged from alpha.81 (item 9).
4. **The update itself renders every hosted vhost** when the candidate's Panel starts (item 9). A site generated
   with the include is therefore rewritten without it by the update, before any owner action.
5. **The update card's time** is the attempt's end, in a sentence that says "started" (item 12).
6. **An import lists `dns` as imported although DNS was not chosen** (`do_dns: false`): the step `dns` is `ok: true`
   with "external DNS ownership preserved; verify provider records before publishing the site" and is in `imported`
   (items 2 and 5, three platforms). Not followed further.
7. **The Arch `getent` output differs.** In `steps/02-origin/step.json` of the five Arch cells the guest's `getent`
   answer for `celikpanel.net` is printed as `127.0.0.1 localhost`, in the Debian and Ubuntu cells as
   `127.0.0.1 celikpanel.net`; the address is `127.0.0.1` in all of them and the cause of the different name is not known.

## Deviations from the brief

- Items 2, 3 and 5 were also run on Debian 13 and Ubuntu 24.04, and items 4 and 6 on both of them (the brief asked
  for one): the cells run unattended and the sections are the same code.
- Item 6 has two readings instead of one; the second (a long name) needs no owner action.
- Item 6 ran before items 2 and 5, so that no PHP vhost in service read the file the owner moved.
- Item 9: the vhost had already been rendered by the update when the Panel action was sent (above). The action
  chosen is the General settings save. The comparison is of status and body SHA-256, ten requests.
- Item 12 was run three times (twice on the published baseline because of H44, once on the candidate source as the
  running version); the brief asked for one platform.
- The alpha.81 baseline archive is set3's, reused; the builder was changed to allow that explicitly.
- Five cells were repeated after a harness correction (run-b): the Debian update cell (H43), the Arch return cell
  (H44) and the three fresh-install cells (H45). The first runs are kept as they are; what each of them did not
  measure is said with its defect.
- Windows power events were not collected; the 30 s watcher has no gap.

## Not measured

Item 10 on Arch (no Postfix there). The reasons `cleanup_unconfirmed`, `import_removed` and
`import_cleanup_unconfirmed` of item 6. `409 PHP_VERSION_NOT_INSTALLED`. A reload of a unit in state `failed`, and
of PHP-FPM. More than 20 refused archive members. The other hostile members of set3 (`..`, symbolic link) under
this commit. A site with a certificate, the hosting-type change and the certificate change with a refused vhost.
Item 9 on Arch. A return with a typed cause. Anything on a screen: no browser was used, so no sentence of the Add
Domain dialog, the import page, the service notice or the update card was seen rendered; the catalogue texts
recorded beside the answers are looked up by key. The four things the commit message lists as not fixed (PHP
extension directories, the MariaDB settings path, `DetectInstalledPHPVersion`, a static-to-PHP change on Arch).
Every regression section of set3 other than the ones named here: set3's cells were not run again.

## Item 13: could a mail-certificate renewal cell be measured in this lab without a certificate authority?

Not built; from the source at `557b554eb` only.

*What the path needs.* The check itself (`verifyMailServiceServes`, `cmd/agent/mail_served_certificate.go`) dials
the service's own listeners on `127.0.0.1`, `::1` and the host's addresses (Postfix 465, 587, 25; Dovecot 993, 995,
143, 110), does STARTTLS where the port needs it, completes a handshake without verifying the chain, and compares
the leaf's SHA-256 with the selected certificate's, for up to 12 rounds 500 ms apart. That part needs no authority.
It is reached through the renewal entry: the deploy hook (`internal/mailrenewalkit/deploy-hook`) queues the lineage
named by `RENEWED_LINEAGE` when it is `/etc/letsencrypt/live/celikpanel-mail-*`, and the native timer's run
publishes and reloads. Before publication the pair is verified against the **system trust store**
(`mailhostartifact.VerifyRetainedPair(cert, key, domain, roots, now)`, `cmd/agent/mail_host_certificate_linux.go`),
and the certbot directory layout and ownership are checked (`certbot_source_ownership_linux.go`). So a bare
self-signed leaf dropped into `live/` would be refused before the handshake path is reached. A cell would need: a
key and a root made in the guest by `openssl`, the root added to the guest's trust store as a lab fixture (not an
owner action, and to be said as such), a leaf for the mail host name signed by it and laid out as certbot does
(`live/`, `archive/`, `renewal/`), and the hook run with `RENEWED_LINEAGE` as certbot runs it. A first managed mail
certificate must already be selected; the earlier lab records reached that state through root tests in a guest
(`mail_renewal_hook_vm_test.go`, `MAIL-CONTRACT-AZ`, `MAIL-HOOK-BE`), not through the Panel's API. Whether the Panel
offers a route that selects a mail host certificate without an issuance was not checked; if it does not, the cell
starts from a fixture state and says so. No name of an authority is asked for or contacted at any point.

*What it would prove.* On the packaged Postfix and Dovecot of Debian 13 and Ubuntu 24.04 (where the daemon's unit
differs: `postfix.service` and `postfix@-.service`): that after the helper's reload the listeners present the new
leaf and only then the renewal is recorded as complete; that with Postfix or Dovecot stopped the answer is "not
confirmed: no TLS listener answered" and nothing is reported as activated; that with a listener still presenting
the old leaf (for example an owner's `-o smtpd_tls_cert_file` in `master.cf`) the answer is "not complete", exit 1,
the pending renewal is retained and the retry budget counts; and what the journal and the root CLI say in each.

*What it would not prove.* Issuance or renewal by a real authority (the ACME order, HTTP-01 through this server's
nginx, certbot's own decision to renew, its timer starting the hook, account and rate limits); that a client
elsewhere trusts what is served (the handshake skips verification and stays on the host's own addresses, behind no
firewall or NAT); behaviour at real expiry; customer SNI certificates; power loss during publication; Arch (no
mail). And the trust decision before publication would have been exercised with a root that only the lab holds.

## Removals, leftovers, secrets

- Removed by this run, each listed in `host/removals.txt` after its cell's checksums were verified on the staged
  copy: the overlay disks of this run's twelve labs. Nothing else was removed. No directory of an earlier run was
  touched (`/var/tmp/cp-set1-run`, `cp-set2-run`, `cp-set3-run`, their labs, build clones and dist directories).
- Left on the WSL host, with sizes: `host/host-leftovers.txt` (the run directory `/var/tmp/cp-set4-run`, the twelve
  lab directories `/var/tmp/cp-release-drill-s4-*` without their overlay disks, the two build clones under
  `/var/tmp/cp-upd1-build/`, the eight dist directories this run built under `/var/tmp/cp-pair-accept/dist/`).
  No QEMU process and no job of this run is left running.
- `secret-scan.txt`: set3's scan over this folder (PEM private keys, the body lines of every lab key file, licence
  keys, WireGuard keys, secret-named JSON fields, hash-shaped credential values, password assignments). Its last
  line is the result. The drivers redact at collection time (set3's shape rules, unchanged). The new readers never
  read a column whose name is secret-shaped (`ssl_key_path` is recorded as "[not read: secret-shaped column]") and
  read one variable of the Panel's process environment, `CELIKPANEL_WEB_DIR`.
- `SHA256SUMS`: every file of this folder but itself; generated last and verified.

## Files

`README.md`; `checks-all.txt`, `checks-not-passed.txt`, `facts.json` (generated); `secret-scan.txt`; `SHA256SUMS`;
`build/` (the two builds, proofs, offline suites, dry runs, `web-build-sentences.json`); `harness-run-copy/`
(overlays, jobs, queue log); `host/` (host check, disk readings, progress, removals, leftovers);
`set4-arch/`, `set4-debian13/`, `set4-ubuntu/` (the fresh-install cells); `update-alpha81/` (items 9 and 12 from the
published tag); `update-candidate-baseline/` (item 12 with the candidate source as the running version);
`tools/` (every script this run used, the patch records included; `state.sh` is a read-only status reader the
coordinating session placed in the same scratch folder, not a script of this run). In `tools/mkcopy.sh`,
`tools/start-build.sh` and `tools/start-prove.sh` the local scratch-folder path prefix was shortened to
`<scratchpad>` after the run (see "Corrections after intake"); the scripts as run held the full local path.

## Corrections after intake (2026-10-09)

The intake check of this folder (after collection, before it was committed) led to the following changes. Nothing
else was altered; every other file is as collected.

1. **Redaction (15 places in 9 files, 3 values).** `transaction_token_sha256` in `observer-events.json` (2 places each)
   and `recovery-fault-events.jsonl` (line 4, 2 places each), and the same digest inside a sudo journal line of
   `journal-product.txt` (1 place each), in `update-alpha81/upd1-arch-defective/run-a/steps/16-collect/`,
   `update-alpha81/upd1-arch-defective/run-b/steps/16-collect/` and
   `update-candidate-baseline/upd1-arch-defective/run-a/steps/15-collect/`, were replaced by `[REDACTED-SHA256]`; the
   JSON files were parsed again and are valid. It is the digest of a one-shot update-transaction token of a destroyed
   lab guest. `set3-20261012` (already published, sealed) carries the same field unredacted and was not touched. The
   class is added to `secret-scan.txt` (the scan itself was not run again; its counts are those of collection).
2. **Per-run `SHA256SUMS`.** The three per-run `SHA256SUMS` of those cells had three lines each (the three redacted
   files) rewritten to the new checksums.
3. **README.** Line 9 (no installed server): reworded to what can be shown, with the search terms. The former
   "Isolation" paragraph (Guests, isolation, disk): retitled and reworded, it states QEMU user networking with outbound
   NAT and that name pinning, the licence-step answer and the absence of certificate routes are established, not
   network isolation. Item 9: the re-rendered vhost lacks only the `snippets/fastcgi-php.conf` include. Item 10: the
   readings of the failed unit are named by moment and unit. Baseline descriptions: "the published tag's unpatched
   source, built with the acceptance-test licence build tag". Item 12 row: the candidate-baseline cell's folder is
   `14-set4-update-check-after-the-return`. Observation 7 (Arch `getent` output) added. This section and the tools
   sentence above added.
4. **Tools.** The local scratch-folder prefix in `tools/mkcopy.sh`, `tools/start-build.sh` and `tools/start-prove.sh`
   was replaced by `<scratchpad>`. No other occurrence of the local user-profile path exists in this folder.
5. **`SHA256SUMS`** regenerated last (same format and ordering; it does not list itself).

Remaining gaps, unchanged: no capture of the guests' traffic; item 13 not built; nothing was rendered in a browser.

**Correction after publication (2026-10-10).** After this directory was published (2026-10-10), the operator's machine name was replaced by `<operator-host>` in the SSH public-key comments (and in the text that named it) of this directory: 21 occurrences in 12 files; the key material itself, a lab public key, is unchanged.
After this directory was published (2026-10-10), 3 digest values of the one-shot update-transaction token (3 in plain text in 3 files, as `transaction_token_sha256` values and as `.release-db-migrations/<digest>` directory names; 0 inside base64 `events_base64` text in 0 files) were replaced by `[REDACTED-SHA256]`.

The affected `SHA256SUMS` lines (per-run lists and this directory's list) were recomputed afterwards; nothing else in this directory was changed.
