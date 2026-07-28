# CelikPanel AI Agent

*[Türkçe](CELIKPANEL-AI-AGENT.tr.md) · Product and security roadmap*

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
4. Ask for confirmation when the plan contains mutations.
5. Submit typed operations with a client request ID and idempotency key.
6. Stream ledger-backed progress to the same page-wide operation overlay used
   by manual panel actions.
7. Re-read authoritative state before reporting success.
8. Write the user, plan, confirmation, tool inputs and final result to the audit
   log, with secrets redacted.

The agent must never claim success merely because a command or request returned.
The final state check is part of the operation.

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
- The audit log can reconstruct who requested, confirmed and executed every
  action.
- An operator can disable the feature globally without affecting normal panel
  operation.
