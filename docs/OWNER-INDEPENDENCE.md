# Owner-controlled service operation

*September 12, 2026 ? [Türkçe](OWNER-INDEPENDENCE.tr.md) ? D-022 implementation audit*

## Current boundary

The source change separates secondary DNS replication from optional remote
record-management automation. It does **not** certify removing CelikPanel and
all its directories from an installed hosting server. The audit below records
remaining dependencies; no installed server was changed by this work.

## DNS behavior

When a local secondary also hosts websites, applications or mail, the wizard
now offers two record-management choices:

- **I will manage records on the primary DNS server.** No remote panel URL,
  enrollment or credentials. BIND/PowerDNS retains its native secondary role.
  New hosted domains use external DNS instructions and verification; the owner
  creates their records on the primary through their preferred native tools.
- **Manage records through a CelikPanel connection.** The existing explicitly
  authorized connector publishes records on the primary. Its HTTPS endpoint
  belongs to management automation, not AXFR/IXFR/NOTIFY.

The manual choice is explicit in the reviewed draft and plan identity. Editing
it invalidates the previous review. A missing field in an already stored plan
keeps the Alpha71 connector behavior. The new editor defaults to manual when
no prior publisher endpoint exists; saved endpoints retain automatic selection.
Existing domain ownership is not rewritten. A pure DNS purpose keeps its local
DNS default. Manual secondary hosting still requires native pair and public
nameserver proof before hosting steps can continue.

The shipped replication implementation uses standard RFC 9432 version 2 catalog
zones to provision secondary members. A peer configured outside CelikPanel must
provide or consume the matching catalog and allow the peer's transfers. Merely
running BIND with unrelated zones is not sufficient. The expected catalog is
`catalog-<primary IPv4 bytes in lower-case hex>.celikpanel.invalid`; member labels
are lower-case SHA-224 of canonical zone names. See
[`internal/binddns/pairing.go`](../internal/binddns/pairing.go).
The generated transfer ACL is restricted to the reviewed peer. This change does
not add arbitrary catalog names, general zone adoption, TSIG enrollment or
reciprocal per-zone primary roles.

## Evidence

[Local validation](validation/native-dns-independence-20260912/README.md) covers
manual/automatic draft paths and a BIND-primary/PowerDNS-secondary pair. In two
disposable Debian 13 guests, both management services were stopped and their
standard binary paths removed by moving the binaries to a fixture evidence
directory. The primary was changed to a plain owner-written BIND configuration
with no CelikPanel include. Native catalog addition, record change, daemon
restarts and catalog removal propagated to PowerDNS without a panel API.
This establishes DNS operation for that tested pair, not full hosting removal
or whole-host reboot independence.

## Dependency audit and required follow-up

| Area | Observed dependency | Required resolution before removal support |
|---|---|---|
| DNS serving and transfer | Native named/pdns, native files/database and catalog; no panel process needed for tested replication | Retain DNS files/database, service accounts and daemon units. Detect owner edits before later panel writes. |
| Remote record automation | Requires the opted-in primary panel connector to create/change records | Optional; disable/remove credentials on explicit handover. Native existing DNS serving continues. |
| Mail host certificate renewal | Certbot deploy hook invokes `/opt/celikpanel/bin/agent --deploy-mail-host-certificate`; renewal is queued and deployed by the agent worker | Native Certbot deployment and service reload independent of the management agent, preserving atomic certificate publication, approved lineage and concurrent-change protections. |
| Firewall after host reboot | `celikpanel-firewall-restore.service` invokes the agent; persisted `firewall.nft` may be versioned JSON, not directly loadable nft syntax | Migrate to a native persisted ruleset and independent boot restore. Preserve other tables and reviewed SSH access; prove reboot and failed-restore behavior on supported platforms. |
| Node runtimes | Executables live below `/opt/celikpanel/runtimes` | Retain workload runtimes or migrate unit executable paths. Removing `/opt/celikpanel` recursively is not supported. |
| ACME challenge paths | `internal/hostingpath` uses `/var/lib/celikpanel-agent/acme-http-01`; website Certbot work is `/var/lib/celikpanel/certbot` | Retain or migrate challenge roots, lineage/work directories and native renewal schedules with a real renewal test. Directory names do not alone imply a running-agent dependency. |
| Panel HTTPS renewal | Its deploy hook invokes the agent for panel certificate material | Panel-only lineage/hook cleanup must be separate from shared website/mail renewal; do not disable Certbot globally. |
| Website/mail/database data and configurations | Native services exist, but not every generated path, mail map, auth source or scheduled deployment has been certified for removal | Produce an exact retention manifest and prove web requests, DB transactions, mail delivery/authentication, cron and renewal with both management binaries absent. |
| Tenant cron | `cmd/agent/cron_rpc.go` writes native user crontabs | Preserve users/homes/crontabs and inspect the job commands for owner-selected panel dependencies. Native cron must remain enabled. |

Relevant source: `cmd/agent/mail_host_certificate_linux.go`,
`cmd/agent/mail_host_certificate_renewal.go`,
`deploy/systemd/celikpanel-firewall-restore.service`,
`cmd/agent/firewall_rpc.go`, `cmd/agent/runtime_rpc.go`,
`cmd/agent/ssl_rpc.go`, `internal/hostingpath/path.go`.

Changing renewal or boot persistence is a separate migration with durability,
rollback and native acceptance requirements; replacing those paths with unchecked
shell copies would not meet D-022. No removal command is offered or certified by
this change. Panel removal must eventually be an owner-reviewed operation that
preserves all workload state and leaves a documented native administration path.
