# Actionable operation guidance

*Product requirement recorded September 13, 2026 · [Türkçe](OPERATION-GUIDANCE.tr.md)*

**Status: required across the product; implementation and audit are incomplete.**
The current source change covers setup progress with accepted-plan DNS role
context, existing panel-license states, and generic service-operation errors. It
does not certify every operation or provide third-party product-license adapters.

## Requirement

Every program, service, runtime and integration managed by CelikPanel must explain
its current operation, any prerequisite or failure, and the user's next action.
This applies to installation, configuration, updates, verification, recovery and
other supported lifecycle operations, both inside the wizard and on management
pages. A spinner, a generic error, or a message hidden below a long operation list
is insufficient when the user can or must act.

The primary message must identify:

1. What is happening or what is preventing progress, with the affected component,
   server or integration named when known.
2. Who must act and where: this server's administrator, the other server's owner,
   a DNS provider, a software vendor, or a credential/entitlement administrator.
3. The concrete next action, using reviewed names and addresses or verified
   observations where available.
4. What happens afterward: automatic checking, explicit verification, an existing
   recovery action, or a new review after a resolved failure. Promise automatic
   continuation only when the operation implements it.

Present that message and action before the detailed step list. Keep diagnostic
information available without forcing users to interpret internal codes. Turkish
and English messages, mobile layout and keyboard access are required.

## Truth and operation identity

Separate an observed failure from an unmet prerequisite and an unknown result.
An unreachable verification service does not prove that a license is invalid.
A DNS pair that has not passed verification does not prove that the other daemon
is uninstalled. Show the last verified observation and its time when available;
do not invent a diagnosis, progress percentage or completion estimate.

Use the accepted operation's immutable plan and exact child identities for
execution guidance. A later editable draft or an unrelated operation must not
change the explanation of an accepted job. Polling and page refresh are reads;
they must not install components, retry mutations or launch a duplicate job.

Keep a known failure visible while its outcome or recovery is being verified.
Explain that verification separately. Clear or supersede a failure only with
matching newer evidence, not merely because a poll started or a spinner appeared.
Lost responses and uncertain outcomes require reconciliation of the original
operation before a duplicate start or retry can be offered.

## Licensing, credentials and external dependencies

The rule includes programs supplied by other vendors, not just CelikPanel.
When an adapter supports a dependency it must distinguish its meaningful states,
including missing, rejected, expired or revoked licenses; verification-service
unavailability; missing or rejected credentials; insufficient permission;
connection failure; and unsupported or unknown results. Each supported state
needs a stable machine-readable code and a specific translated explanation and
next action. A generic error is an honest fallback for an unclassified result,
not evidence that all these integrations are implemented.

Request secrets through the appropriate protected flow. Never include license
keys, passwords, tokens, private keys or credential-bearing URLs in progress
messages, logs, audit details or diagnostics. Translate and sanitize vendor
responses; a raw response may contain secrets. An action to obtain or renew a
license must not purchase one or disclose credentials automatically.

License and credential checks must not silently stop or remove unrelated hosted
workloads. CelikPanel's own license loss restricts panel use while existing
services continue. Third-party vendor behavior must be reported accurately;
CelikPanel cannot claim control over a vendor's independent license enforcement.

## DNS examples and ordering

- A primary configured with a secondary should identify the expected secondary
  role, nameserver and IP from the reviewed plan. If that peer is not yet prepared,
  direct its owner to prepare it without waiting for this server's entire setup
  to finish. Initial DNS verification can already depend on the peer.
- A secondary should identify its primary and required transfer relationship.
  If the primary is not available, explain the missing prerequisite. If the child
  operation has already failed, show its actual recovery state instead of
  promising that starting the primary will automatically repair every failure.
- If both servers are configured, direct the user to relevant public records,
  addresses, DNS access and transfer permissions. Do not repeatedly prescribe an
  installation that is already complete.
- External DNS shows the records and provider-side action. Standard native DNS
  transfer must not require another CelikPanel or its HTTPS API. Optional remote
  record-management authorization is a separate step with separate guidance.

## Acceptance and remaining scope

For each supported flow, verify normal progress, user-action waiting, failure,
unknown evidence, recovery and completion. Cover reversed server setup order,
reload/reconnect, wrong/missing credentials, relevant license states, unreachable
providers and preserved exact operation identity. Adapter-specific cases must
pass before that adapter is claimed to support them.

The present setup change adds readonly context from the validated accepted plan,
role-aware DNS guidance, panel-license guidance and a service-error fallback.
Detailed vendor-license integration and an exhaustive audit of all lifecycle
screens remain future work. This document makes those requirements durable; it
is not a completed implementation claim.

[D-024](DECISIONS.md), [D-021 and the setup plan](SERVER-SETUP-PLAN.md), and
[D-022 owner independence](OWNER-INDEPENDENCE.md) apply together. This guidance
requirement does not authorize assistant-side live changes or panel updates.
The user must continue to initiate installed-panel updates in CelikPanel as
specified in [AGENTS.md](../AGENTS.md).
