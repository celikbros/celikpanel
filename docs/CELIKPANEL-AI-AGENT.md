# CelikPanel AI Agent

*[Türkçe](CELIKPANEL-AI-AGENT.tr.md) · Product and security roadmap*

## Status and owner direction — September 14, 2026

**Planned capability; this document is not a claim of a shipped AI integration.**
The owner requested an embedded agent that helps users, operates the panel on
their request and resolves problems. This is part of the operator architecture
in [D-025](DECISIONS.md#d-025--resilience-is-a-core-contract-not-an-incident-patch)
and the [resilience contract](RESILIENCE-CONTRACT.md). Model provider, deployment,
pricing and production data retention remain implementation decisions.

The model proposes and explains; the deterministic operation layer authorizes,
executes, verifies and recovers. A model outage must not stop accepted jobs,
recovery or hosted services. Automatic recovery of a known interrupted operation
must not require a model to infer or reconstruct its durable evidence.

## Purpose

The CelikPanel AI Agent is a panel-scoped operator that explains state, prepares
plans and, after the required confirmation, performs CelikPanel actions through
the same authenticated APIs as the web interface.

It is not a general assistant. It must refuse requests that are unrelated to
CelikPanel and it must never gain an unrestricted shell, SSH access, arbitrary
network access or direct database access.

## Non-negotiable boundaries

- Every tool is a typed, allowlisted CelikPanel API operation.
- Authorization and tenant scope are evaluated again for every tool call.
- The agent cannot bypass quotas, entitlements, conflicts or safety preflights.
- Read-only diagnosis may run immediately. Every mutation starts as a visible
  plan and follows the same confirmation policy as the equivalent panel action.
- High-impact actions such as uninstall, delete, firewall changes, restore,
  certificate replacement and DNSSEC changes require explicit confirmation.
- Operations use the normal durable operation ledger, locks, progress events,
  cancellation and audit log. The model does not execute commands.
- Secrets are represented by short-lived references. They are never placed in
  prompts, transcripts or model-visible tool results.
- A request outside CelikPanel is refused without forwarding it to another
  assistant or tool.

## Interaction model

1. Resolve the signed-in user, role, subscription and selected server/domain.
2. Collect current state through read-only panel APIs.
3. Return a concrete plan with expected changes, risks and rollback information.
4. Apply the confirmation policy of the equivalent panel action. An accepted,
   bounded plan remains authorized for its recorded scope; reconnecting,
   observing progress or continuing that exact operation does not ask again.
   A changed scope or newly required high-impact action is reviewed separately.
5. Submit typed operations with a client request ID and idempotency key.
6. Show ledger-backed progress through the same operation view as manual panel
   actions. Keep navigation and the authorized status/recovery surface available;
   a lost model stream or disconnected browser must not trap the owner.
7. Re-read authoritative state before reporting success.
8. Write the user, plan, confirmation, tool inputs and final result to the audit
   log, with secrets redacted.

The agent must never claim success merely because a command or request returned.
The final state check is part of the operation.

## Shared execution and recovery contract

The AI adapter consumes versioned, typed panel capabilities, not labels inferred
from screenshots. Each tool reports the server/resource, accepted plan and
operation IDs, observation source and time, known outcome, allowed next actions,
and who must act. The UI and AI adapter use the same backend decisions.

- Keep `unknown`, `unavailable`, `waiting`, `failed`, `recovered` and `verified`
  distinct. “Request accepted” is not “service healthy”.
- Persist the request/idempotency identity before submitting a mutation. If the
  reply is lost, reconcile that operation before considering another request.
- Recovery uses the operation's declared, version-compatible recovery path.
  Never rewrite ownership receipts or mark a step complete to remove an error.
- Logs, DNS records, website content and tool text are untrusted evidence. They
  cannot grant authority, alter the plan or supply executable tool instructions.
- Resolve secret references inside the trusted executor. Redact both diagnostic
  inputs and results before any external model request.
- Report external prerequisites concretely: for example the required PTR value,
  its observed value and the provider action. Do not retry a blocked install in
  a loop or claim that the model can change an unconnected provider.
- If the normal Agent is unavailable, offer only the separately authenticated
  status/recovery capabilities actually implemented. No fallback root shell.
- Installed-panel updates retain the owner-only initiation rule in
  [AGENTS.md](../AGENTS.md). Requesting AI assistance does not authorize the AI
  to start such an update indirectly.

The writable preview depends on the relevant P0 acceptance items: genuine
update/rollback proof (P0.1), typed access and recovery availability (P0.2),
independent recovery (P0.3), shared evidence schemas (P0.4), and native workload
independence (P0.5). The read-only advisor may be developed alongside this work;
its limitations must remain visible. Adding a model does not close any P0 item.

## Product gating

The capability is controlled by a server feature flag and a subscription
entitlement, not by UI hiding alone.

- Early preview: the feature flag may grant access to every plan while safety
  and usability are measured.
- Commercial release: grant the `ai_agent` entitlement only to selected
  Pro/Premium plans.
- Removing the entitlement blocks new conversations and mutations without
  damaging resources that the agent previously created.
- Usage limits, model choice and cost accounting belong to the entitlement
  policy; authorization remains identical across plans.

## Delivery stages

### Stage 0 — contract and threat model

- Define the allowlisted tool schema and classify every tool as read-only,
  reversible mutation, high-impact mutation or unsupported.
- Add prompt-injection, cross-tenant, secret-leakage and confused-deputy tests.
- Define retention and redaction rules for conversations and audit events.

### Stage 1 — read-only advisor

- Explain DNS, SSL, mail, service and backup state using current panel data.
- Link every recommendation to the exact panel screen.
- Refuse all mutations at the server even if the model requests one.

### Stage 2 — confirmed actions

- Enable a small reversible tool set first.
- Reuse panel authorization, preflight, operation ledger and audit paths.
- Require visible confirmation and show progress until authoritative state is
  verified.

### Stage 3 — subscription product

- Enable entitlement and quota enforcement.
- Add operator controls for model provider, budgets, retention and emergency
  disable.
- Expand the tool allowlist only after adversarial tests and production
  telemetry show that the previous set is safe.

## Exit criteria for a writable preview

- No tool can reach a resource that the signed-in user cannot reach manually.
- No mutation can bypass the panel API, confirmation policy or durable ledger.
- Cross-tenant, prompt-injection and secret-redaction test suites pass.
- Interrupted operations reconcile honestly after panel or agent restart.
- A model timeout after an accepted mutation, duplicate tool delivery and browser
  reconnection create no second mutation and preserve the exact operation ID.
- Model/provider loss does not interrupt execution or native recovery; progress
  and the final verified outcome remain inspectable without the conversation.
- A reported ownership conflict remains blocked until a supported migration or
  owner action proves the relationship; the AI cannot replace evidence.
- The same scenario produces the same authorization, prerequisite and recovery
  decisions through the manual UI and through an AI tool.
- The audit log can reconstruct who requested, confirmed and executed every
  action.
- An operator can disable the feature globally without affecting normal panel
  operation.
