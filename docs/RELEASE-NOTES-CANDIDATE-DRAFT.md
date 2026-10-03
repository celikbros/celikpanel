# Next release candidate — draft notes

[Türkçe](RELEASE-NOTES-CANDIDATE-DRAFT.tr.md)

*Draft, 2026-10-01. No version number is assigned and nothing is published. The owner
decides the version, the publication and every installed-panel update. Rename this
file to `RELEASE-NOTES-<version>.md` when the version is chosen.*

This candidate follows v0.1.0-alpha.80. It changes how DNS engine changes, paired
DNS servers and panel updates recover from interruption, and it states what was
measured and what was not.

## What changes for the server owner

**DNS engine changes recover on the same operation.** When a first BIND or
PowerDNS installation, a PowerDNS-to-BIND switch or the adoption of an existing
PowerDNS is interrupted, it is finished or undone on the same operation: by the
Agent at its next start, or by the owner command that the status output names.
Adopting an existing PowerDNS while the Agent is running is covered by component
tests only. Switching a serving BIND to PowerDNS has no
recovery contract and is refused in the Panel, in setup and in the Agent.

**Two paired servers work in three combinations.** BIND/BIND, BIND/PowerDNS and
PowerDNS/BIND as primary/secondary pass setup, zone add, record edit, zone delete,
re-add and a reboot with the Panel and Agent disabled while DNS keeps answering.
A new paired PowerDNS primary is now offered on a server that has no DNS engine
yet (Debian 13 only, with the measured PowerDNS version). Pairing uses standard zone transfer
and catalog zones; the secondary does not need a remote CelikPanel API.

**Zone deletion on a pair is proven on the secondary.** The Panel confirms that
the secondary dropped the zone before it reports the deletion complete. This needs
a one-time owner enrollment between the two servers (`dns-peer-enroll`, run on
both). Until then the
deletion waits and the screen says who must act, what to run and how the work
resumes. This also covers a top-level zone that has no parent zone on the server.

**A failed update returns or continues without a developer.** A candidate that
cannot migrate or cannot start its Panel is rejected before completion and the
previous release returns automatically, also after a reset or a killed recovery
process. A candidate that fails after completion is retried three times; then
recovery pauses, keeps the first cause, and prints one command the owner runs after
removing the cause. The update card, the root command
`/usr/libexec/celikpanel/recovery` and the recovery records describe the same
operation while the Panel is running; while it is stopped, only the root command
does.

**Hosted services do not depend on the Panel.** With the Panel and Agent disabled
across a reboot, the site, database, scheduled jobs, firewall rules and (on Debian)
SMTP kept working in the measured cells. If completing an update pauses after its
three automatic attempts, certificate renewal that the updater had stopped is
returned to its previous state while you remove the cause; the owner retry stops
it again until the update finishes.

**Setup says what is unsupported before it starts.** Mail on Arch is refused at
plan review. Native cron is a setup component. A hosting root that the web server
cannot traverse is reported with the path and the fix.

**Setup on Ubuntu no longer blocks itself.** On a standard Ubuntu 24.04 server
image, a package helper service (PackageKit) stays running idle for about five
minutes after every package operation. The previous release (v0.1.0-alpha.80)
took that for a package task in progress and stopped the next setup stage; in
our tests setup had to be started about seven times. The idle service is no
longer counted. A real package task still is: setup or the update then stops
with the message that the server's package manager is busy and to wait for it
to finish.

**Mail settings are re-applied after an update finishes.** If the new Panel
starts while the update still holds the server, it cannot re-publish the mail
certificates to the mail services or re-connect the mail filters at that moment.
It now retries by itself every 30 seconds, for up to 10 minutes after it starts,
and acts as soon as the server is free; it writes a line to the Panel's log
(`sudo journalctl -u celikpanel-panel`) when an attempt does something. Usually
nothing visible depends on this. It matters if a certificate activation was
interrupted by the update, or if mail delivery was being refused until the mail
filters were re-connected. If the server stays busy for the whole 10 minutes,
nothing is changed and mail keeps running: once the other task has finished,
restart the Panel with `sudo systemctl restart celikpanel-panel`, or use "Retry
activation" on the domain's SSL page.

**The customer archive carries no test material.** The release archive no longer
contains the acceptance harness, test scripts or retained evidence, and the
signing step refuses an archive that does.

## Limits of this release

These are known and deliberate. They are not hidden defects.

- **Measured platforms:** Debian 13 and Arch. On Ubuntu 24.04 only three things
  were tested: updating from v0.1.0-alpha.80 (with earlier builds of this
  candidate), first-time setup, and starting an update. The RHEL family remains a blocked preview.
- **Mail on Arch** is not supported.
- **BIND to PowerDNS** on a serving server is refused. A BIND secondary configured
  by an older release is not upgraded to the new secondary configuration
  automatically.
- **The first update from v0.1.0-alpha.80.** This path was measured on Debian 13
  and Ubuntu 24.04, from the alpha.80 source rebuilt with a test license, not
  from the signed archive; Arch was not measured. If this candidate fails and the server
  returns to alpha.80 automatically, the alpha.80 Panel cannot describe that: it
  shows a raw failure line and offers the same version again. The state is then
  read over SSH with `sudo /usr/libexec/celikpanel/recovery`. For the first
  seconds of the update that command does not exist yet.
- **A setup stage stopped by a real package task does not resume by itself.**
  Wait for the task to finish, review the plan and start setup again; services
  already installed are not installed again, and the stage that was stopped runs
  again.
- **Updating an Ubuntu server that still runs v0.1.0-alpha.80.** Its update
  card can show "Server changes are temporarily unavailable" while the idle
  package helper runs. Wait about five minutes, check again, then start the
  update. Nothing on the server is changed while the card says this. This was
  read from the alpha.80 code; it did not occur in a test.
- **No rollback after completion.** If the new Panel starts and fails later, the
  update is finished rather than undone.
- **While the Panel is stopped** its address shows no live recovery status; the
  owner reads it over SSH with the recovery command.
- **A paused rollback keeps certificate renewal paused** until the owner acts.
- **No panel removal path** is offered and no claim is made about removing
  CelikPanel. Only "management disabled" was measured.
- **Evidence scope:** disposable virtual machines on one laptop host, a test
  signing key, a loopback release origin and a test-only license; every update
  case ran once on the exact code of this candidate (Debian 13, Arch, Ubuntu
  24.04; the "Panel starts but fails later" case on Debian only; one case needed
  a second run after a test-harness fault); the mail re-apply was reached in 11
  Panels on Debian and Ubuntu (not in the paused case, the management-off cases
  or after a return to alpha.80), but only its first attempt, and no mail
  certificate was present; no power-loss test; no browser took part in the
  update runs. The
  production signing path, the real release origin and the license service were
  not exercised by these runs.
- **Untested messages:** four safety messages did not occur in any test run, so
  their on-screen wording is untested: the short second check of whether the
  Panel is busy before an update, the "update check refused" message, the cause
  of a failed pre-update snapshot, and the service start-limit message.
- **A misleading log line:** on Debian 13 without the `postfix-lmdb` package,
  whenever the Panel re-publishes the mail certificates, Postfix logs "fatal:
  unsupported map type: lmdb". The step still completes and mail keeps working
  with the hash map type; the line is noise.
- **Interface debts:** the Components page shows catalogue names in English; the
  cron Uninstall button is shown and refuses after confirmation; a visual browser
  pass over the changed screens is outstanding.
- **Open acceptance items:** the resilience checklist remains partly open. See
  the [resilience contract](RESILIENCE-CONTRACT.md) and the
  [DNS recovery register](DNS-RECOVERY-ACCEPTANCE.md).

## Before publication (owner decisions and remaining checks)

1. Choose the version number.
2. Run the packaging contract tests that need root (18 scripts) on a disposable
   machine or in CI; they were not run as root on the build host.
3. Build and sign with the production process and verify the signed archive and
   its recovery compatibility with v0.1.0-alpha.80.
4. Decide whether the vendor publishing tools (download-portal and membership
   scripts) stay in the customer archive.
5. Owner test on a disposable server before any installed panel is updated.

Install this release only through CelikPanel's update interface. Publishing it
does not update installed servers.
