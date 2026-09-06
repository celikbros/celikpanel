# Three-distribution acceptance, 6 September 2026

What was run, on what, and what it proved. Written from the run, not from
intent: where a thing was not tested it says so.

## What was under test

| | |
| --- | --- |
| Commit | `5827b587a808399193b88c5798c3b1380aa36765` |
| Tree | `4b07454c8a87365b6576eec759f38336b4ca02f2` |
| Version | `v0.1.0-alpha.52-128-g5827b58` |
| Tarball | `celikpanel-v0.1.0-alpha.52-128-g5827b58.tar.gz` |
| Tarball sha256 | `0a637111e721144917045dd3ee6c4fe5938d7d243dfbeb53ebd9ec9bff1138df` |

Built reproducibly with `SOURCE_DATE_EPOCH=1788600000` and Go 1.26.5, from a
`git archive` of that tree whose sha256 was verified before use.

## The machines

Three fresh cloud images, each a new qcow2 overlay on a verified base. Nothing
was touched by hand on any of them before the installer ran; every change after
that went through the panel's API, never a shell.

| Host | Distribution | Kernel at install | Base image verified by |
| --- | --- | --- | --- |
| acc-debian | Debian GNU/Linux 13 (trixie) | 6.12.105+deb13-cloud-amd64 | sha512 against the harness lock |
| acc-ubuntu | Ubuntu 24.04.4 LTS | 6.8.0-138-generic | sha256 against Canonical's `SHA256SUMS` |
| acc-arch | Arch Linux | 7.1.8-arch1-3 | sha256 against the harness lock |

Ubuntu had not been run at all this cycle. That is why it was included, and it
is where the run found the most.

## Install

All three: `install.sh` exited 0, 38 migrations applied, panel and agent both
**active and enabled**, `GET /login` → 200, and `release.commit` on disk equal
to the commit above.

**Arch said what it had to say.** The installer replaced the running kernel and
told the operator so, in both languages, naming the exact kernel and what would
fail until a restart:

> This server is running kernel 7.1.8-arch1-3, whose modules are no longer on
> disk: the prerequisite step upgraded the kernel and replaced them. Until this
> server is restarted it cannot load nftables or WireGuard, so turning the
> firewall on or installing the VPN will fail. Everything else is installed and
> running.

The machine was then restarted, as the product asked, and came back on
7.2.3-arch1-3 with the panel and agent running.

## The database chapter

Thirteen checks, every mutation through the panel API. The shell was used only
to read - to confirm the operator's own way in still worked, and that no secret
reached a log.

| Check | Debian | Ubuntu | Arch |
| --- | --- | --- | --- |
| MariaDB installed through the panel | 60 s | see note | 30 s |
| The engine registered itself, nobody added it | pass | pass | pass |
| **The panel opened an account of its own, with nobody asking** | pass | pass | pass |
| **A database and its user were created** | pass | pass | pass |
| The panel lists the database back | pass | pass | pass |
| **The operator's root still opens the engine over the unix socket, with no password** | pass | pass | pass |
| An administrator can read the password the panel holds | pass | pass | pass |
| The engine accepts the password the panel stored | pass | pass | pass |
| The password is in no response, no journal line, no log | pass | pass | pass |
| Reading the password left exactly one audit entry | pass | pass | pass |
| A new password works on the engine and the panel holds that one | pass | pass | pass |
| Rotating did not disturb the operator's root | pass | pass | pass |
| **Restart: panel, account, credential and the operator's root all survive** | 5/5 | 5/5 | 5/5 |

The four rows in bold are the ones that could not have passed a week ago:
creating a database on a registered server was impossible (R-051), the engine
refused the empty credential the panel held (R-053), the panel had no account
of its own (R-057), and on a fresh server none of it was reachable at all
(R-067).

**The Ubuntu note.** One check failed there, and it is a fault in the test, not
the product: the script asked for a second MariaDB install while the first was
still running, and the panel refused it. That refusal is correct. The twelve
checks that follow it all passed on the same machine.

## What the run found

**R-067, the reason this run existed.** On a freshly installed server the whole
database chapter was unreachable, and the page said something false about it.
Found on the first machine within minutes; fixed the same day; the run above is
against the fixed build. The register entry has the detail.

**R-068, left open.** On Ubuntu, `packagekitd` and `unattended-upgrades` hold
the package manager intermittently for the first minutes after boot, so an
install through the panel is refused - correctly. What is not right is the
sentence: the agent computes exactly which of three reasons it is, and the
operator is told one line that covers all three. Recorded, not fixed.

## What this run did not test

Said plainly, because a list of passes with no list of gaps is a highlight
reel.

- **Nothing was seen in a browser.** No browser runs where this was driven, so
  every check above is an API check. R-065's screen - the account on the
  database server card - has not been looked at by a person. That is still owed.
- PostgreSQL. Only MariaDB was installed and exercised; the PostgreSQL side of
  the same code paths is covered by tests and by nothing on a real machine.
- Mail, DNS and the firewall on these three machines. DNS and the firewall have
  their own live proofs from 5 September on other machines; they were not
  re-run here.
- An engine on another machine. The panel cannot register one at all (R-066),
  so there was nothing to test.
- Upgrade over an existing install. Every machine here was clean.

## Evidence

On the lab host, under `/root/accept/evidence/`:

```
facts-acc-{debian,ubuntu,arch}.txt      the machine as it came up
install-acc-{debian,ubuntu,arch}.log    the full installer output
accept-db-acc-{debian,ubuntu,arch}.log  the thirteen checks
accept-survive-acc-{debian,ubuntu,arch}.log   before and after the restart
```
