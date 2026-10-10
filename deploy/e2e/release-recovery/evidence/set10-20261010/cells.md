| Cell | Debian 13 | Ubuntu 24.04 | Arch | raw (Debian 13) |
| --- | --- | --- | --- | --- |
| S0 | FAIL | FAIL | FAIL | `debian13/run-c/driver/steps/09-s0-sites/section.json` |
| h | PASS | PASS | PASS | `debian13/run-c/driver/steps/10-h-i-unchanged-start/section.json` |
| i | PASS | PASS | PASS | `debian13/run-c/driver/steps/10-h-i-unchanged-start/section.json` |
| a1 | PASS | PASS | PASS | `debian13/run-c/driver/steps/11-a-owner-edit/section.json` |
| a2 | PASS | PASS | PASS | `debian13/run-c/driver/steps/11-a-owner-edit/section.json` |
| a3 | PASS | PASS | PASS | `debian13/run-c/driver/steps/11-a-owner-edit/section.json` |
| b1 | PASS | PASS | PASS | `debian13/run-c/driver/steps/12-b-owner-edit/section.json` |
| b2 | PASS | PASS | PASS | `debian13/run-c/driver/steps/12-b-owner-edit/section.json` |
| b3 | PASS | PASS | PASS | `debian13/run-c/driver/steps/12-b-owner-edit/section.json` |
| c1 | PASS | PASS | PASS | `debian13/run-c/driver/steps/13-c-owner-edit/section.json` |
| c2 | PASS | PASS | PASS | `debian13/run-c/driver/steps/13-c-owner-edit/section.json` |
| c3 | PASS | PASS | PASS | `debian13/run-c/driver/steps/13-c-owner-edit/section.json` |
| d1 | PASS | PASS | PASS | `debian13/run-c/driver/steps/14-d-owner-edit/section.json` |
| d2 | PASS | PASS | PASS | `debian13/run-c/driver/steps/14-d-owner-edit/section.json` |
| d3 | PASS | PASS | PASS | `debian13/run-c/driver/steps/14-d-owner-edit/section.json` |
| keep | PASS | PASS | PASS | `debian13/run-c/driver/steps/15-keep/section.json` |
| j-kept | PASS | PASS | PASS | `debian13/run-c/driver/steps/16-take-j/section.json` |
| take | PASS | PASS | PASS | `debian13/run-c/driver/steps/16-take-j/section.json` |
| take-stale | PASS | PASS | PASS | `debian13/run-c/driver/steps/17-take-stale/section.json` |
| take-refused | PASS | PASS | PASS | `debian13/run-c/driver/steps/18-take-refused/section.json` |
| e | PASS | PASS | PASS | `debian13/run-c/driver/steps/19-e-removed/section.json` |
| e-recreate | PASS | PASS | PASS | `debian13/run-c/driver/steps/19-e-removed/section.json` |
| f | NOT-MEASURED | NOT-MEASURED | NOT-MEASURED | `debian13/run-c/driver/steps/20-f-include-dir/section.json` |
| g | FAIL | FAIL | FAIL | `debian13/run-c/driver/steps/21-g-immutable/section.json` |
| g-resume | PASS | PASS | PASS | `debian13/run-c/driver/steps/21-g-immutable/section.json` |
| k | PASS | PASS | PASS | `debian13/run-c/driver/steps/22-k-symlink/section.json` |
| l | PASS | PASS | PASS | `debian13/run-c/driver/steps/23-l-delete/section.json` |
| B-before | PASS | PASS | PASS | `debian13/update-defective/driver/steps/09-set10-sites-before-the-update/section.json` |
| B-forward | PASS | PASS | PASS | `debian13/update-good/driver/steps/18-set10-site-files-after-the-update/section.json` |
| B-db-restore | PASS | PASS | PASS | `debian13/update-good/driver/steps/27-set10-database-restore-from-before-the-migration/section.json` |
| B-return | PASS | PASS | PASS | `debian13/update-defective/driver/steps/17-set10-site-files-after-the-update/section.json` |

- Debian 13 S0 FAIL: set10-a.test: the include directory exists, is empty and 0755; set10-b.test: the include directory exists, is empty and 0755; set1-owner.test: the include directory exists, is empty and 0755 (`debian13/run-c/driver/steps/09-s0-sites/section.json`)
- Debian 13 f NOT-MEASURED: a PHP version switch keeps the owner's file (`debian13/run-c/driver/steps/20-f-include-dir/section.json`)
- Debian 13 g FAIL: site-config for A names the reason (unreadable/write_refused or EPERM) (`debian13/run-c/driver/steps/21-g-immutable/section.json`)
- Ubuntu 24.04 S0 FAIL: set10-a.test: the include directory exists, is empty and 0755; set10-b.test: the include directory exists, is empty and 0755; set1-owner.test: the include directory exists, is empty and 0755 (`ubuntu/run-a/driver/steps/09-s0-sites/section.json`)
- Ubuntu 24.04 f NOT-MEASURED: a PHP version switch keeps the owner's file (`ubuntu/run-a/driver/steps/20-f-include-dir/section.json`)
- Ubuntu 24.04 g FAIL: site-config for A names the reason (unreadable/write_refused or EPERM) (`ubuntu/run-a/driver/steps/21-g-immutable/section.json`)
- Arch S0 FAIL: set10-a.test: the include directory exists, is empty and 0755; set10-b.test: the include directory exists, is empty and 0755; set1-owner.test: the include directory exists, is empty and 0755 (`arch/run-a/driver/steps/09-s0-sites/section.json`)
- Arch f NOT-MEASURED: a PHP version switch keeps the owner's file (`arch/run-a/driver/steps/20-f-include-dir/section.json`)
- Arch g FAIL: site-config for A names the reason (unreadable/write_refused or EPERM) (`arch/run-a/driver/steps/21-g-immutable/section.json`)

Counts recomputed at intake (2026-10-11) from the rows above: 31 cells per platform x 3 = 93; PASS 84, FAIL 6 (S0 and g on each platform), NOT-MEASURED 3 (f on each platform).
f is NOT-MEASURED as a cell: 4 of its 5 expectations PASS, the PHP version switch is not measured (one PHP version per guest).
