# Store and entitlement operations

This document describes the database-backed Add-ons Store, its trust boundary,
and the operations available to a panel administrator.

## Storage model

The Store uses the existing CelikPanel SQLite database. It does not introduce a
separate catalog database or a third SQLite file.

The Store data model uses:

- new `store_offerings`: the typed offering catalog;
- new `store_offering_components`: the typed mapping between an offering and the
  managed components it requires;
- existing `subscription_entitlements`: the rights owned by a subscription;
- existing `service_scan_cache`: the most recent observed component state.

Operational decisions stay in typed columns and reviewed Go code. The
`metadata_json` column is presentation-only and may contain exactly:

- localized `name`;
- localized `description`;
- `icon`;
- `tags`.

Do not put shell commands, package recipes, SQL, filesystem paths, prices,
permissions, release flags, or component mappings in `metadata_json`.

## Offering state

The API keeps four independent state dimensions:

| Dimension | Examples | Meaning |
| --- | --- | --- |
| `release_state` | `available`, `coming_soon`, `retired` | Product lifecycle |
| `platform_state` | `supported`, `unsupported`, `blocked`, `unknown` | Whether the required components can be used on this host |
| `entitlement_state` | `included`, `owned`, `not_owned`, `suspended`, `expired` | Subscription right |
| `runtime_state` | `running`, `installed`, `not_installed`, `stopped`, `error`, `unknown`, `not_applicable` | Observed component state |

Clients must not infer one dimension from another. They should use
`primary_action.enabled` and `blocker_reason` to render the next action.

The compatibility fields `state`, `state_reason`, `action`, and `action_path`
are derived from this canonical state and exist for older clients.

## HTTP API

Catalog projection:

```text
GET /api/v1/store
GET /api/v1/store/{offering_id}
```

Supported query parameters:

- `subscription_id=<positive integer>` adds entitlement state after enforcing
  subscription visibility;
- `locale=en|tr` selects the top-level localized name and description.

Without `subscription_id`, the endpoint is discovery-only: grant-mode
offerings report `subscription_required`, expose no enabled action, and cannot
be acquired. Every allowed query parameter may appear at most once.

Unknown paths, methods, query parameters, locales, and offering IDs are
rejected. The complete bilingual metadata remains available in `metadata`.

Component topology is administrator-only. `component_ids` and `manage_path`
are empty for non-administrators; an administrator receives them only with a
safe authorized management action. New clients treat `primary_action` as
canonical and never revive a disabled action from compatibility fields.

Compatibility catalog:

```text
GET /api/v1/products
```

This endpoint is projected from the same Store tables. It deliberately returns
`monthly_price_cents: null`; CelikPanel does not invent billing data.

Entitlement operations:

```text
GET    /api/v1/subscriptions/{subscription_id}/entitlements
POST   /api/v1/subscriptions/{subscription_id}/entitlements
DELETE /api/v1/subscriptions/{subscription_id}/entitlements/{offering_id}
```

An administrator grants a right with:

```json
{
  "product_id": "vpn",
  "expires_at": "2027-01-01T00:00:00Z"
}
```

`expires_at` is optional. When present, it must be a future RFC 3339 timestamp.
Malformed or expired stored timestamps fail closed.

GET follows normal subscription visibility rules. POST and DELETE are
administrator-only until a typed reseller entitlement-pool model exists.
Resellers receive no hidden administrator action or `/services` management
path from the Store API.

Grant and revoke write the entitlement and its audit record in the same
transaction. Repeating the exact active grant with the same expiry remains a
read-only success even if the offering was retired or the scan cache became
stale after the first response was lost. A state-changing grant to a retired
offering is still rejected. A conditional upsert prevents concurrent exact
requests from creating duplicate audit events. Repeating a completed revoke is
also idempotent. An administrator can always revoke an existing grant even when
its release, platform, runtime, or scan-cache state is currently blocked.

Offering rows are lifecycle records and must not be physically deleted. Retire
an offering with `release_state = retired`; this preserves the lookup needed to
display, retry, audit, and revoke existing entitlements. Physical deletion can
leave an entitlement without a catalogue route and is unsupported.

## Installation boundary

Acquiring an offering records a subscription right only.

It never:

- installs or removes a package;
- starts, stops, or reconfigures a service;
- invokes the host agent;
- runs a component rescan;
- changes DNS, TLS, mail, or firewall settings.

Component installation and service management remain explicit administrator
operations on the Components page. This separation makes every host mutation
visible in the panel and keeps Store browsing safe.

## Scan cache and fail-closed behavior

The Store reads `service_scan_cache`; it does not start a live scan. A cache is
usable for 15 minutes. Missing, future-dated, stale, malformed, duplicate, or
legacy scan data produces an unknown/non-actionable platform state.

When the Store asks for a rescan:

1. Open **Components**.
2. Select **Rescan**.
3. Wait for the scan to finish.
4. Return to **Add-ons** and refresh the Store.

A grant that requires components is accepted only when a fresh cache verifies
that every required component is usable. The entitlement operation itself
still performs no installation and no agent call.

## Safe navigation

`manage_path` is a typed internal panel path, not arbitrary JSON. The database
constraint and backend validation both reject external URLs, protocol-relative
URLs, backslashes, control characters, and `.`/`..` traversal segments. Store
paths are canonical plain panel routes: percent-encoded and double-encoded
variants are rejected instead of relying on ambiguous repeated decoding.

The web client binds every catalog response to the subscription ID that was
actually loaded. Starting a load or receiving an error clears the prior items,
stale/aborted responses are ignored, and mutations use the loaded subscription
ID rather than a newly selected value. A catalog is rendered only while its
loaded ID matches the current selection.

The web client also uses only an enabled `primary_action`. Legacy
`action_path` or `manage_path` values cannot override that canonical action.

Valid examples:

```text
/domains
/services/wireguard
```

## Catalog maintenance

The migration seeds only honest states:

- released offerings are marked `available`;
- unfinished products are marked `coming_soon`;
- no fake prices are stored;
- component requirements are explicit typed rows.

To add or change an offering:

1. Add a new migration; do not edit a migration already deployed.
2. Set the typed lifecycle, entitlement mode, category, vendor, and safe
   internal management path.
3. Keep `metadata_json` presentation-only and provide both English and Turkish
   name and description.
4. Add each required component to `store_offering_components`.
5. Add migration, API, authorization, cache, and path-validation tests.
6. Verify `go test`, `go vet`, the web build, and the Store UI before release.

Direct SQLite editing is an emergency procedure, not the normal admin
interface. Use the Store and entitlement APIs so authorization, validation,
idempotency, and audit logging remain enforced.
