# set4: the README's second pass, after the three fresh-install cells were repeated (H45). One-off; kept as a record.
import io
import os

p = os.path.join(os.path.dirname(os.path.abspath(__file__)), "README.md")
s = io.open(p, encoding="utf-8").read()


def rep(old, new, count=1):
    global s
    assert s.count(old) == count, (s.count(old), old[:70])
    s = s.replace(old, new)


rep("""answer on every platform asked for (items 1 to 9 and 11, and the raw answer of item 12); **item 10 fails on Ubuntu
24.04**: Postfix's Stop answers `200` without the `note` although `postfix@-.service` ends `failed` (it passes on
Debian 13); item 13 is a feasibility note.""",
    """answer on every platform asked for (items 1 to 9 and 11, and the raw answer of item 12); **item 10 fails on Ubuntu
24.04, in both runs**: Postfix's Stop answers `200` without the `note` although `postfix@-.service` ends `failed`
(it passes on Debian 13, in both runs); item 13 is a feasibility note.""")

rep("""`c` = `b` + the three new files; `d` = `c` + H43; `e` = `d` + H44 = the working tree's four files. Offline suites on
`c`, `d` and `e`, all OK""",
    """`c` = `b` + the three new files; `d` = `c` + H43; `e` = `d` + H44; `f` = `e` + H45 = the working tree's four files
(SHA-256 equal, `harness-run-copy/overlay-f/files.sha256`). Offline suites on `c`, `d`, `e` and `f`, all OK""")

rep("""Harness defects found during the run (the edits are `tools/patch2.py`, `tools/patch3.py`):""",
    """Harness defects found during the run (the edits are `tools/patch2.py`, `tools/patch3.py`, `tools/patch4.py`):""")

rep("""(their `served-web-build.json` reads only the lab's own source copy); fixed from copy `e` (`update-alpha81/upd1-arch-defective/run-b`) |
""",
    """(their `served-web-build.json` reads only the lab's own source copy); fixed from copy `e` (`update-alpha81/upd1-arch-defective/run-b`) |
| H45 | The reader of what is left for a domain looked for the site's home under `subscriptions/<s>/domains/<id>`; the product's home is `subscriptions/<s>/sites/<id>`. The listing was empty before and after, so "the home is gone" and "no new site directory" passed without having measured anything. Also, after a delete on Debian and Ubuntu the socket was looked for under the pool's `/var/run/...` spelling while the listing spells it `/run/...`. Found while this README was written, from run-a's own records. | The listing reads `*/sites/*`; a delete must first have seen the home with the site; a refusal also names the home `useradd` logged and looks for it afterwards; the socket path is normalised. | **the three fresh-install cells run-a: their checks of items 1, 6 and 8 did not measure the site's home (nor, on Debian and Ubuntu, the socket after the delete)**; everything else of those cells stands. The three cells were run again from copy `f` (run-b), and the table below cites run-b for those items |
""")

rep("""`host/c-drive-watch.txt`).** 76.5 GiB before the first cell; before each cell 76.5, 73.5, 72.9, 71.5, 69.1, 67.3,
79.4, 78.0 and (last cell) the reading in `host/c-drive-cells.txt`; lowest of the readings taken every 30 s: 66.9
GiB. Never under 40 GiB before a cell. The watcher's readings have no gap larger than 31 s from 10:29Z to the end, so
the host did not stop executing during the cells; Windows power events were not collected in this run.""",
    """`host/c-drive-watch.txt`).** 76.5 GiB before the first cell; before the twelve cells, in their order: 76.5, 73.5,
72.9, 71.5, 69.1, 67.3, 79.4, 78.0, 77.0, 76.2, 74.4, 73.7; lowest of the readings taken every 30 s: @@LOWEST@@ GiB.
Never under 40 GiB before a cell. The watcher's readings have no gap larger than @@GAP@@ s from 10:29Z to the end
(@@READINGS@@ readings), so the host did not stop executing during the cells; Windows power events were not collected
in this run.""")

rep("""| update-alpha81/upd1-arch-defective/run-b | s4-u12-arch-b | e | 12:20:44-12:30:51 | `complete-for-review` | - |
""",
    """| update-alpha81/upd1-arch-defective/run-b | s4-u12-arch-b | e | 12:20:44-12:30:51 | `complete-for-review` | - |
| set4-arch/run-b | s4-arch-b | f | 12:35:55-12:43:26 | `complete-for-review` | M10 skipped (no mail on this platform) |
| set4-debian13/run-b | s4-d13-b | f | 12:43:29-12:53:50 | `complete-for-review` | - |
| set4-ubuntu/run-b | s4-ub-b | f | 12:53:54-13:13:08 | `failed` | M10-postfix-stop: failed (item 10, product; the same three checks as run-a) |
""")

rep("""| Item | Arch | Debian 13 | Ubuntu 24.04 | Raw file (per platform folder `set4-<p>/run-a/`) |""",
    """| Item | Arch | Debian 13 | Ubuntu 24.04 | Raw file (per platform folder `set4-<p>/run-b/`; run-a holds the same answers, with the limit of H45) |""")

rep("""| 10. Postfix Stop while `postfix check` refuses `main.cf`: 200 with `note`, unit left `failed`, Start after the correction | NOT-MEASURED (no Postfix) | PASS | **FAIL** (no `note`) |""",
    """| 10. Postfix Stop while `postfix check` refuses `main.cf`: 200 with `note`, unit left `failed`, Start after the correction | NOT-MEASURED (no Postfix) | PASS (run-a and run-b) | **FAIL** (no `note`; run-a and run-b) |""")

rep("""A cell is PASS only when its request was sent and its answer and native readings are in the named file. Paths are
below this folder; `S/` is `steps/`.""",
    """A cell is PASS only when its request was sent and its answer and native readings are in the named file. Paths are
below this folder; `S/` is `steps/`. Every answer of the Panel was the same in run-a and run-b of a platform (status,
code, reason; `facts.json`); the sections below quote run-b where the two differ in what was read on the guest.""")

rep("""After `DELETE /api/v1/domains/{id}` (200, `{"status":"deleted"}`): the domain is not listed, no `domains` or
`sites` row, no system account or group, the site's home is gone, no file of nginx names the domain and `nginx -T`
has no line with it, no pool file, no socket, `nginx -t` exits 0, PHP-FPM active.""",
    """After `DELETE /api/v1/domains/{id}` (200, `{"status":"deleted"}`), run-b: the domain is not listed, no `domains` or
`sites` row, no system account or group, the site's home `/var/www/celikpanel/subscriptions/2/sites/2` (seen with
the site) is no longer among the site directories, no file of nginx names the domain and `nginx -T` has no line
with it, no pool file, the socket is not in `/run/php*`, `nginx -t` exits 0, PHP-FPM active.""")

rep("""site's name, although the journal shows `useradd` made them (system account); no new directory under
`/var/www/celikpanel/subscriptions/*/sites` (files); no pool file names the account""",
    """site's name, although the journal shows `useradd` made them (system account); the home `useradd` logged
(`/var/www/celikpanel/subscriptions/2/sites/3`, and `/sites/5` for the long name) is not among the site directories
afterwards, which are the one site that existed before (files; run-b only, H45); no pool file names the account""")

rep("""- **Ubuntu 24.04: FAIL.** 200 `{"applied":"stopped","outcome":"verified","success":true}`: **no `note`.** On the
  guest""",
    """- **Ubuntu 24.04: FAIL, in run-a and in run-b.** 200 `{"applied":"stopped","outcome":"verified","success":true}`:
  **no `note`.** On the guest""")

rep("""  4 ms after the master ends). That cause is an inference from the timestamps, not a measurement.
  Raw: `set4-ubuntu/run-a/steps/13-m10-postfix-stop/section.json` (`postfix_stop`),
  `api/0149-post-api-v1-service-action.json`, `journal/postfix-since-the-stop.txt`,
  `native/097-postfix-units-after-stop.json`, `native/098-postfix-units-8s-after-stop.json`.""",
    """  4 ms after the master ends). That cause is an inference from the timestamps, not a measurement. Run-b has the
  same order (13:11:37.169 "Stopping", 13:11:38.180 control process exited, .183 master terminating, .188 "Failed
  with result 'exit-code'") and the same answer.
  Raw: `set4-ubuntu/run-a/steps/13-m10-postfix-stop/section.json` (`postfix_stop`),
  `api/0149-post-api-v1-service-action.json`, `journal/postfix-since-the-stop.txt`,
  `native/097-postfix-units-after-stop.json`, `native/098-postfix-units-8s-after-stop.json`; and the same files
  under `set4-ubuntu/run-b/` (the exchange is `api/0117-post-api-v1-service-action.json`).""")

rep("""- **Debian 13: PASS.** 200""", """- **Debian 13: PASS (run-a and run-b).** 200""")

rep("""1. **Item 10, Ubuntu 24.04:** a Stop of Postfix that leaves `postfix@-.service` marked `failed` answers 200 without
   the `note`. Request and answer above; raw file `set4-ubuntu/run-a/steps/13-m10-postfix-stop/section.json`.""",
    """1. **Item 10, Ubuntu 24.04:** a Stop of Postfix that leaves `postfix@-.service` marked `failed` answers 200 without
   the `note`, in both runs. Request and answer above; raw files
   `set4-ubuntu/run-a/steps/13-m10-postfix-stop/section.json` and `set4-ubuntu/run-b/steps/13-m10-postfix-stop/section.json`.""")

rep("""- Two cells were repeated after a harness correction (run-b); the first runs are kept as they are.""",
    """- Five cells were repeated after a harness correction (run-b): the Debian update cell (H43), the Arch return cell
  (H44) and the three fresh-install cells (H45). The first runs are kept as they are; what each of them did not
  measure is said with its defect.""")

rep("""copy: the overlay disks of this run's nine labs.""", """copy: the overlay disks of this run's twelve labs.""")
rep("""(the run directory `/var/tmp/cp-set4-run`, the nine
  lab directories""", """(the run directory `/var/tmp/cp-set4-run`, the twelve
  lab directories""")
rep("""`tools/` (every script this run used, the two patch records included).""",
    """`tools/` (every script this run used, the patch records included).""")
io.open(p, "w", encoding="utf-8", newline="\n").write(s)
print("README edited")
