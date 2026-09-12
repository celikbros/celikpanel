# Hosting on a secondary DNS server

Implementation record, 12 September 2026. This describes the working-tree
implementation; it is not a release or installed-server update record.

## Supported topology

A CelikPanel node can serve secondary authoritative DNS and host websites,
applications and mail at the same time. The local DNS role and the location
where hosting records are edited are separate choices.

For the requested pair:

| Server | Local DNS | Hosting DNS publication |
| --- | --- | --- |
| Frankfurt | BIND primary, `ns1.celikhost.com`, `72.62.38.15` | Writes its own zones and accepts scoped publication from Boston |
| Boston | PowerDNS secondary, `ns2.celikhost.com`, `2.25.80.4` | Sends changes for its hosting zones to the authorized Frankfurt panel |

Boston's sites and mail remain on Boston. Their records point to Boston as
appropriate, even though Frankfurt maintains the primary DNS zone. Boston's
local DNS engine receives the secondary copies through the existing paired
DNS transfer machinery. A DNS secondary does not replicate website files,
databases or mailboxes.

The publication connection can manage only the zones owned by that connection.
Existing domains retain their recorded DNS ownership. Selecting this mode does
not import or take over existing primary zones.

This implementation uses one publishing primary for the pair. It does not add
reciprocal per-zone primary roles, multiple primaries or arbitrary DNS clusters.
Those require a separate change to the DNS engine's topology and ownership
model. BIND and PowerDNS remain the supported local engines.

## One wizard execution

1. Choose a hosting purpose or custom components. In Access and DNS, select
   **This server**, **Secondary**, the local engine and the matching pair's
   nameservers and IP addresses. Enter the primary's HTTPS panel address.
2. Review the complete plan, including the required publishing connection.
   Reviewing or opening the wizard creates no connection and starts no services.
3. Start the primary's reviewed setup, then the secondary's. Setup prepares the
   local DNS, required access components, firewall and trusted panel HTTPS before
   waiting for the pair. A selected Nginx is prepared before certificate issuance
   so it does not invalidate standalone certificate renewal afterward.
4. The secondary pauses at **Connect to the primary DNS server**. On the primary,
   open **Settings → DNS infrastructure**, create a DNS publishing enrollment
   code, and enter it in the secondary's wizard. Settings remains available while
   setup is waiting, including through the Access and recovery links.
5. After the connection is verified, select **Use this connection and continue
   setup**. The same saved execution continues its hosting and mail steps.
   No second wizard run or manual edit of the DNS default is necessary.
6. Completion requires fresh local secondary, remote publishing, service,
   firewall, certificate and renewal checks. Missing external prerequisites
   remain visible as waiting states.

The primary endpoint must have trusted HTTPS. Public DNS records and the
nameserver pair must resolve correctly. Mail identity and its certificates,
PTR and domain mail records still have their own prerequisites; adding mail
does not rename the operating system to the mail address.

## Durable authorization and recovery

The draft records the reviewed `dns_publisher_endpoint`. The plan includes a
`dns_publisher` step only when local secondary DNS and services that publish
hosting DNS are selected. A primary hosting plan can wait at `dns_readiness`
after secure access has been prepared.

`POST /api/v1/setup/publisher` binds a connection to an exact execution and plan.
It requires an administrator and current setup admission. Fresh proof must match
the reviewed endpoint, ordered nameservers and both primary and secondary IPs;
local secondary readiness is independently checked.

The immutable, nonsecret receipt is separate from the runner's execution JSON.
A repeated identical confirmation resumes the same operation; another
connection cannot replace its receipt. A lost HTTP reply triggers reconciliation
of that execution rather than a new setup or new authorization grant. A revised
plan prevents a late response from the old runner from changing DNS defaults.

Only after this check does setup save the remote publication default for new
hosting domains. The completed local DNS step is not rerun on restart. Final
verification checks both the bound remote publisher and the local secondary.
Pair identity is an opt-in addition to the remote protocol so older clients
retain their existing response shape.

Safe Edit plan recovery is available at DNS waiting gates only when preceding
steps are complete, later steps have not started, and there are no unresolved
service, agent or DNS children. Completed host changes are retained and assessed
by the replacement plan; recovery does not silently uninstall them.

## Validation and rollout boundary

See [secondary hosting acceptance](validation/secondary-hosting-20260912/README.md)
for the final source tests, real two-VM DNS results and their limits. Controlled
VM DNS/ACME fixtures do not prove public delegation, public CA issuance or
Internet mail delivery.

Installed panels are updated only by the user through CelikPanel's update
interface. Source implementation, local VM acceptance and release publication
do not authorize updating Frankfurt, Boston or another installed instance.
