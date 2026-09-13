# Access DNS preparation in server setup

*Working-tree implementation note: 13 September 2026 · [Türkçe](SERVER-SETUP-ACCESS-DNS.tr.md)*

**Not released; not verified on the installed Frankfurt/Boston pair.** This note
records the bounded correction to the [approved setup flow](SERVER-SETUP-PLAN.md).
It is not publication evidence or permission to update an installed panel.
The user continues to initiate every installed-panel update in CelikPanel.

## Problem and resulting behavior

An installed authoritative DNS engine does not establish that the panel hostname
has usable DNS records. The previous sequence could finish engine installation
and attempt ACME before the hostname's owner zone existed. Sending the owner out
of setup to create a domain did not meet the approved guided-setup outcome.

New reviewed plans separate three operations: prepare authorized infrastructure
records, verify the service hostname through public DNS, then obtain its
certificate. The same public DNS prerequisite applies before mail-host TLS when
mail is selected. Public checks do not accept `/etc/hosts` as DNS evidence and do
not start ACME or edit records while waiting. An absent AAAA record is not itself
a requirement to enable IPv6; published addresses must match the supported
server identity.

## Explicit owner zone and minimal records

The local primary may prepare a zone only when the owner explicitly selects its
exact name in the wizard. The plan previews each required record as an addition
or an existing record to keep. A panel FQDN alone is not authorization to take
over its inferred parent zone.

The preparation contains only:

- The zone SOA and the reviewed pair's NS records.
- A records for nameservers inside that zone, using each server's own reviewed IP.
- The panel hostname A record and, when mail is selected, its mail hostname A record.
- An optional peer panel hostname A record using the reviewed peer IP.

Every prepared access hostname must belong to the selected zone. No tenant,
website, mailbox, application apex, `www`, MX, SPF or DMARC is created by this
infrastructure step. Existing unrelated records, TTLs and priorities remain
unchanged. A supported local zone is identified through its domain ownership
or its prior infrastructure ownership receipt. Unknown ownership, external or
remote management, conflicting addresses, aliases, delegated children and
ambiguous authority block preparation with a reason to review.

## Primary BIND and secondary PowerDNS sequence

For the reviewed Frankfurt primary/Boston secondary arrangement:

1. Start the primary's native DNS setup. Start the secondary once the primary
   can provide its native catalog; do not wait for all primary hosting steps or
   its panel certificate to complete first.
2. The secondary configures its native transfer relationship. If it was started
   first, the unmet primary-catalog prerequisite identifies the primary and the
   action its owner must take. A failed admitted operation remains a failure and
   follows its recovery path; it is not silently restarted.
3. The primary prepares the reviewed infrastructure zone only after the native
   pair proof permits publication. The secondary receives its copy through the
   existing standard DNS transfer/catalog mechanisms.
4. Public hostname verification waits for correct authoritative records and
   delegation. The owner performs any registrar/glue or external-provider
   changes that the panel has no authorization to make.
5. Certificate issuance follows public DNS verification. Remaining hosting and
   final readiness checks retain their respective prerequisites.

Standard transfer does not require the other server's CelikPanel HTTPS API or
panel credentials. Optional remote record-management automation remains separate.
The DNS artifacts use the existing native service lifecycle; this correction does
not certify complete panel/agent removal or every workload's independence.

## Recovery and accepted-plan identity

The exact reviewed zone changes and server address are part of plan identity.
Record additions and an infrastructure ownership/preparation receipt commit in
one SQLite transaction. Publication reuses the existing V3 engine, epoch,
generation and durable mutation lease. Reopening the page or recovering a lost
response reconciles that operation instead of installing a second zone or
repeatedly advancing its SOA after verified completion.

Changes to ownership or records after review require another review. A verified
publication failure stays visible; a new reviewed operation can continue after
its cause is resolved. An unknown or pending publication retains its exact
identity. Previously accepted plans are not rewritten to insert these new steps.
Existing completed host changes remain in place when the owner reviews a revised
plan. No setup state poll is authorized to create an unrelated mutation.

## Evidence and supported limits

The infrastructure helper's 14 top-level tests and their subcases cover read-only
review, both native primary engine bindings, minimal records, existing MX/TTL/
priority preservation, ownership and alias/delegation conflicts, no writes before
pair readiness, immutable identity, prepared-transaction crash recovery, exact
pending-lease reconciliation, known failure/new review, and build-change handling.
The combined local command passed:

```text
go test ./cmd/panel -run '^(TestServerSetupInfrastructureDNS|TestDNSZoneV3)' -count=1
```

These are local SQLite/RPC fixtures and existing V3 regression tests. They are
not a real-daemon, public CA, release, or installed-server validation record.

The bounded preparation refuses existing DNSSEC evidence rather than modifying
keys, signatures or signed-zone policy through an unsigned bootstrap operation.
Existing external/remote DNS ownership stays unchanged. Overlapping owner zones
or child delegations need explicit reconciliation. Arbitrary legacy adopted
native PowerDNS configurations and their owner-edit detection have not been
fully audited by this correction; no claim of safe automatic adoption is made.
A panel-wide license/routing audit and all operation adapters remain outside this
change. [D-021](DECISIONS.md), [D-022](OWNER-INDEPENDENCE.md) and
[D-024](OPERATION-GUIDANCE.md) continue to apply.

Additional local validation passed: the full Linux panel (1,258 top-level tests),
agent (1,341) and DNS wire (5) package suites; the focused final setup suite
(104); all 436 frontend tests; production TypeScript/Vite build and unchanged
bundle budgets; eight desktop/mobile English/Turkish browser fixture views.
These checks used local source copies and test fixtures, not the installed servers.

For PowerDNS databases created by the V3 switch path, native publication also
checks the previous management receipt against the current records, zone type
and zone presence before a higher-generation sync or delete. Unreceipted name
collisions, owner edits, missing owned zones and unexpected recreation are
rejected without changing records, catalog or receipts. General native metadata
and explicit legacy adoption remain separate audit boundaries.
