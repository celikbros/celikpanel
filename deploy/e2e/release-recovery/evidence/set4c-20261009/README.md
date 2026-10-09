# set4c reading, 2026-10-09 (UTC): what the real postconf writes to each stream when main.cf makes it warn, and what the old rollback command does with what was kept

A measurement record of two readings. It is not a run of the product: no Agent, no Panel, no mail TLS change, no
certificate, no service. The question came from set4b: the Agent runs its mail commands with both output streams in
one buffer, and three more places than the one corrected there took the trimmed buffer of `postconf -h <name>` as a
value (the mail TLS snapshot that a rollback writes back, the read-back that compares, the expanded reading of the
alias repair).

**Where and how.** The local development guest `CelikPanel-S2-Debian` (WSL; Debian GNU/Linux 13, the guest the Go
tests run in), which has the distribution's Postfix package installed: `postfix 3.10.13-0+deb13u1`,
`/usr/sbin/postconf` (`reading/platform.txt`). Every command is `postconf -c <private directory>`: the private
directories are `/var/tmp/cp-set4c-run/conf-*` and `conf2-*`, each with a `main.cf` written by the script (nine lines of
its own, no file of the system copied; kept beside each case as `main.cf.txt` or `main.cf.before.txt`). The
system's Postfix configuration was not read for these answers and not changed; no service was started, stopped or
reloaded; nothing was contacted. No installed server was touched. It is a development guest, not one of the
disposable QEMU guests of set4b: nothing of the system was changed, so no disposable system was needed, and it is the
one place at hand where a real Postfix 3.10 is installed.

## Reading 1 (`reading/`, 16:13:59Z; `tools/reading.sh`)

Four cases (`clean`, `comment`, `unused`, `both`), each with postconf started by its bare name (`bare-*`) and by its
path (`path-*`), six commands each. Per command: `.argv`, `.stdout`, `.stderr`, `.combined` (both streams through one
pipe, the order a caller of `CombinedOutput` reads), `.exit`. Every exit status is 0.

| main.cf has | `-h smtpd_tls_cert_file` (set) | `-h tls_server_sni_maps` (not set) |
| --- | --- | --- |
| nothing unusual (`clean`) | the value line | one empty line |
| `default_process_limit = 200 # raised for the campaign` (`comment`) | the warning, then the value line | the warning, then the empty line |
| `campaign_note = raised for the campaign` (`unused`) | the value line, then the warning | the empty line, then the warning |

The warnings, in full: `<program>: warning: <dir>/main.cf: #comment after other text is not allowed: # raised for the
campa...` and `<program>: warning: <dir>/main.cf: unused parameter: campaign_note=raised for the campaign`, where
`<program>` is `postconf` or `/usr/sbin/postconf` as it was started. `-x -h alias_database` behaves as `-h` (the
expanded value `hash:/etc/aliases`, the warning in the same place). `-d mail_version` prints `mail_version = 3.10.13`
and no warning in any case. `-n` is kept for each case and is not discussed here.

`reading/restore/`: the text of both streams for `smtpd_tls_cert_file` (three lines there: this directory has no
`master.cf`, which adds a warning of its own, then the value, then the unused-parameter warning) handed to `postconf
-c <dir> -e smtpd_tls_cert_file=<that text>`: exit 1, `fatal: -e, -X, or -# accepts no multi-line input`, `main.cf`
unchanged (`main.cf.diff` is empty).

## Reading 2 (`reading-2/`, 16:14:59Z; `tools/reading2.sh`)

What the old snapshot kept (`strings.TrimSpace` of both streams, reproduced with Python's `strip()`), handed to
`postconf -c <dir> -e <name>=<kept text>` on a private `main.cf` that holds an unused parameter.

| Case | Kept text | `postconf -e` | `main.cf` |
| --- | --- | --- | --- |
| `unset-with-unused-parameter` (`tls_server_sni_maps`) | one line: the warning | exit 0 | gains `tls_server_sni_maps = /usr/sbin/postconf: warning: <dir>/main.cf: unused parameter: campaign_note=raised for the campaign` (`main.cf.diff`) |
| `set-with-unused-parameter` (`smtpd_tls_cert_file`) | two lines: the value, the warning | exit 1, `fatal: -e, -X, or -# accepts no multi-line input` | unchanged |

So a rollback that restored the kept text would have written the warning line into `main.cf` as the value of a
setting that had not been set, and would have failed to restore a setting that had been set.

## Not measured

A mail TLS change, its rollback, or a mail certificate publication with such a `main.cf` (they need a certificate and
a server; none was run). The Agent itself: these are readings of postconf. Postfix 3.8.6 of Ubuntu 24.04 for these
settings (set4b holds its warning for `queue_directory`, written first, like the `comment` case here). Arch's Postfix.
What Postfix does at start or at `postfix check` with the line that was written. `doveconf` and `dovecot --version`.
Any other warning of postconf's than these three (comment after text, unused parameter, missing `master.cf`).

## Verification of the correction (`verification/`)

Not native evidence: the logs of the checks on the working tree after the correction (commit `1f182a483` plus the
uncommitted correction), kept so that the numbers have their files. Go 1.26.5 in the same development guest:
`gofmt -l cmd internal` lists one file this work did not touch (`internal/recoveryruntime/bind_source_support_linux.go`,
as at `1f182a483`); `go vet` and `go build` for linux/amd64 and linux/arm64 exit 0 with no output;
`go test ./cmd/agent/...`: 4481 tests passed, 94 failed, 29 skipped, and its failing set (95 entries with the package)
is, name by name, that of the same run on an extracted copy of `1f182a483` (4473 passed, 94 failed); the eight new
tests are the difference (`go-base-agent-failset.txt.txt`, `go-final-agent-failset.txt.txt`). `go test ./cmd/panel/...
./internal/...`: 66 packages, 64 ok, 2 without tests, none failed; 6829 tests passed. `npm test` in `web/` (its tests
read the guidance documents): 1245 of 1245 passed. `go-v.log.txt`: the seven tests of
`cmd/agent/set4c_postconf_value_test.go` that existed at that moment, verbose, none skipped: the last of them runs the
real postconf of this guest against a private directory.

## Leftovers, secrets, files

- Left in the development guest: `/var/tmp/cp-set4c-run` (the private directories, the two readings and, for the
  comparison of failing tests, an extracted copy of commit `1f182a483`). Nothing was removed and nothing is running.
- `secret-scan.txt`: the scan of set3/set4/set4b over this folder; its last line is the result. The readings hold no
  credential: a main.cf of nine lines written by the script and what postconf printed for it.
- `SHA256SUMS`: every file of this folder but itself, generated last and verified. `tools/`: the scripts.
