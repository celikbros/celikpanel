Cells `upd1-debian13-good` and `upd1-ubuntu-good`, set4's two steps unchanged
(`S/09-set4-php-site-before-the-update/`, `S/18-set4-php-site-after-the-update/`, each with `step.json`, one `native/`
file per request and moment, and `native-text/vhost-*.conf.txt`); generated view `item9.md`. Both steps `passed` on
both platforms.

- **Before the update.** The installed baseline (`GET /api/v1/panel/version`: `v0.1.0-alpha.81`, commit
  `a0beb726...`) creates the PHP site `set4-upd-php.test` through `POST /api/v1/domains/create` (200). The owner
  uploads one PHP page and one text file (owner action, recorded as such). The vhost alpha.81 generated includes
  `snippets/fastcgi-php.conf` and `fastcgi_params`; `nginx -t` exits 0; the page is executed as the site's own account
  `set4_upd_php_test` by PHP 8.4.26 (Debian) and 8.3.6 (Ubuntu).
- **After the verified update, before any owner action.** The vhost file is no longer the baseline's (SHA-256
  `551945c2...` to `83bd07de...` on Debian, `1a392835...` to `4cc697cf...` on Ubuntu): the candidate's Panel rendered
  it when it started (`S/19-collect/journal-product.txt`: "certificate startup reconcile: restored 2 hosted vhosts with
  one nginx validation and reload", Debian 17:23:58Z, Ubuntu 17:45:05Z). It no longer includes the snippet; its PHP
  location holds the six directives set4 names, then `fastcgi_pass`, `SCRIPT_FILENAME` and `include fastcgi_params;`.
  `nginx -t` exits 0.
- **After the vhost was rendered again by an owner's action.** The site's General settings are saved unchanged (`GET`
  then `POST /api/v1/domains/{id}/general`, 200 `{"status":"success"}`). The file has the same bytes, a new inode and
  new times (Debian inode 141382 to 141387, Ubuntu 262669 to 263478), and nginx's journal has one reload at that
  moment. `nginx -t` exits 0; the page is still executed as `set4_upd_php_test`.
- **The ten requests.** For each of them the HTTP status and the SHA-256 of the body are equal before the update,
  after it and after the save, on both platforms (20 rows `True | True` in `item9.md`): the PHP page, the page with a
  query, the three PATH_INFO forms and `/` answer 200, the missing script 404, the dot file 403, the text file 200.
  A missing static file (`/set4-none.txt`) answers 200 with the body of `/` at all three moments, on both
  platforms: the same before and after, so not a change of this update; whether that answer is intended was not
  looked into.

This is what set4 measured on `557b554eb` (its item 9), again with the same answers; the two later product commits
that touch other code (`e2be8af30`, `1f182a483`) and `67b62cc0f` did not change it. Not measured: a site with a
certificate, an owner-edited snippet file, Arch (alpha.81 cannot create a PHP site there).
