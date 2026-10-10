"""set3: the hand-written placeholders of the README (readme_values.json). usage: mkvalues.py [SCAN_TEXT_FILE]"""
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
scan = open(sys.argv[1], encoding="utf-8").read().strip() if len(sys.argv) > 1 else "TODO"

values = {
    "ONE_SENTENCE": (
        "on Debian 13 and Ubuntu 24.04 every correction of the second round is measured as made (S1, P3, P3b, P4, P5, "
        "O9, O10, O11, O14; every set2 section still passes; the only checks that did not pass in the first runs are "
        "the harness defect H40, and the Debian re-run after its fix passes completely); on Arch S1, O9, O10 and O14 "
        "pass and **one candidate product defect remains: a PHP site still cannot be created there** (P5b: the nginx "
        "vhost template includes a snippet file that only Debian's and Ubuntu's nginx packages ship), so no import "
        "starts, and a second Arch reading with that one file placed by the owner passes every section; and in Part 2 "
        "**every cell of the update from the published v0.1.0-alpha.81 reached its expected end** - verified on "
        "Debian 13, Ubuntu 24.04 and Arch with the ledger at the released 43, the guard and the versioned writes "
        "working and refusing what a page opened before the update sends, the deferred mail work done and the CLI and "
        "the card in agreement; automatic return to alpha.81 with the ledger at the released 42 and the data intact "
        "after a migration defect with a second fault (three platforms) and after a failed start check (Debian), with "
        "alpha.81's own recovery reader, outcome card and root CLI telling the owner what happened; the owner's one "
        "printed retry completing a paused update (Debian, Ubuntu); and the workloads served with management off "
        "across a reboot (Debian)."),
    "HARNESS_EXTRA": (
        "| H42 | `build-upd1-artifacts.sh`: a dist build that fails inside `$(build ...)` does not stop the builder "
        "(bash does not carry `errexit` into the substitution). In the rebuild `a81` the dist builder refused the tag "
        "commit (\"refusing to reuse .../dist/a0beb7263...-acceptance-license\", the directory `a81-first` had made 16 "
        "minutes earlier) and the builder went on with that existing archive. | Not changed in this run. The result is "
        "the intended one here (the same commit built by the same dist script; `prove` verified the archive against the "
        "tag's blobs, `build/a81-prove.json`), but the builder should say so or stop. | build `a81`; no cell affected |"),
    "UB_CLIENT": "`Ver 15.1 Distrib 10.11.14-MariaDB`",
    "ARCH_CLIENT": "client 15.2",
    "ARCH_SECOND": (
        "Not the item's result, and not a re-run for a harness defect: the same cell on a new guest, with **one recorded "
        "owner action** before C6b (`SET3_OWNER_NGINX_PHP_SNIPPET=1`, helper mode `owner-nginx-php-snippet`): the owner "
        "creates `/etc/nginx/snippets/` and places `fastcgi-php.conf` with the text Debian's nginx package ships (SHA-256 "
        "`a9dd98bf...`; Arch's own `/etc/nginx/fastcgi.conf`, which it includes, exists). It was run to learn what lies "
        "behind P5b, which the contract left open (\"whether a PHP page executes on Arch under the packaged unit's "
        "hardening, and whether `/run/php-fpm` is where a site's socket may live there\").\n\n"
        "With that one file in place **every section of the cell passed** (`complete-for-review`):\n\n"
        "- `POST /api/v1/domains/create` (PHP): `200`. `GET /set3-probe.php` through nginx: `200`, "
        "`set3-php-executed:42:8.5.11:fpm-fcgi:set3_php_test` (`X-Powered-By: PHP/8.5.11`): PHP executed under the packaged "
        "`php-fpm.service` (`/usr/lib/systemd/system/php-fpm.service`, `ProtectSystem=full`, `ProtectHome=no`), as the site's "
        "own account.\n"
        "- Native: pool file `/etc/php/php-fpm.d/site2.conf` (`root:celikpanel 0644`; `user = set3_php_test`, "
        "`listen.owner = http`, `listen.group = http`, `listen.mode = 0660`); socket "
        "**`/run/php-fpm/php8.3-fpm-site2.sock`**, `http:http 0660`; `/run/php-fpm` is `root:root 0755`. The socket's "
        "name says `8.3` on a host whose only PHP is 8.5.11 (observation O21).\n"
        "- `DELETE /api/v1/domains/{id}`: `200`; not listed after 1.1 s; no domain row, no account, no pool file, no "
        "socket; the page answers `403`; `php-fpm.service` active.\n"
        "- The import completes: the archive with the directory member `200` / `active` with the document root's files "
        "equal to the archive's; the sequential, concurrent and dropped arrivals pass (i, ii, iii, iv, v); the `..` and "
        "the symbolic-link archives answer `200` / `partial` / `IMPORT_PARTIAL` with `not_imported: [files]`, nothing "
        "outside; the absolute member is left out. O9, O10, O14 and the 35 rows as in run-a.\n\n"
        "So on this Arch guest the one missing file is the whole of P5b; nothing else stopped a PHP site or an import."),
    "P5B_SECOND": (
        "Second reading (rid3-arch run-b, with `/etc/nginx/snippets/fastcgi-php.conf` placed by the owner by hand, "
        "recorded as an owner action): the same create answers `200`, the PHP page executes under the packaged "
        "`php-fpm.service`, the site is deleted cleanly and every import completes. The missing file is the whole "
        "defect on this guest. Evidence: `rid3-arch/run-b/steps/15-c6b-php-site/section.json`."),
    "OBS_EXTRA": (
        "- **O21. On Arch the site's PHP version is the fallback literal `8.3`** although the host's only PHP is 8.5.11: "
        "the pool's socket is `/run/php-fpm/php8.3-fpm-site2.sock` (rid3-arch run-b). "
        "Consistent with `cmd/panel/domain_handlers.go:415-418` (when `services.DetectInstalledPHPVersion()` answers "
        "nothing the handler takes `\"8.3\"`); where the value came from was not traced further. The site works; the "
        "name is not the host's version. Seen only in the second reading (run-a never got that far)."),
    "INCOMPAT_EXTRA": "",
    "HARNESS_REFS": " and H42",
    "PART2_OTHER": open(os.path.join(HERE, "part2_other.md"), encoding="utf-8").read().replace(
        "{{OC_TEXT}}", open(os.path.join(HERE, "oc_text.md"), encoding="utf-8").read().strip()).strip(),
    "SCAN": scan,
}
values.update(json.load(open(os.path.join(HERE, "readme_values.extra.json"), encoding="utf-8")) if os.path.exists(os.path.join(HERE, "readme_values.extra.json")) else {})
json.dump(values, open(os.path.join(HERE, "readme_values.json"), "w", encoding="utf-8"), indent=1, ensure_ascii=False)
print("values written:", ", ".join(k for k, v in values.items() if v == "TODO") or "no TODO left")
