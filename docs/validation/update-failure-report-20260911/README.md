# Update failure diagnostics — 2026-09-11

Status: implemented and locally verified; included in the Alpha67 release source.

## Incident evidence and limits

Boston's user-initiated Alpha.65 to Alpha.66 update failed at 10:19:23 UTC.
The installed panel and agent identities remained Alpha.65, and their service
start times remained 07:17:26 and 07:17:25 UTC. No active/quiesce transaction
marker remained. Read-only idle probes subsequently passed; the panel operation
history and agent mutation ledger were empty.

The system journal records `apt-daily.service` starting at 10:19:21 UTC and
finishing at 10:22:51 UTC. This strongly supports a package-manager collision,
but does not prove the exact failed check: the older worker retained only the
updater's final generic cleanup message, losing the causal diagnostic.

No installed update, service restart, timer change, lock removal or state repair
was performed during diagnosis or this fix.

## Change

- Keep failed idle-probe output and the causal `die` reason across updater cleanup.
- Emit a bounded final summary containing cause and recovery outcome. Only an
  explicit package-manager conflict with an unchanged installation receives the
  localized retry instruction. Ambiguous or recovery-required outcomes do not.
- Keep the known package-manager summary within the existing installed panel's
  240-byte, path-free boundary, including the older agent's error prefix. The
  existing summary validation is not relaxed.
- Preserve the prior BIND inner-cause enrichment for other updater failures.
- Replace the unconditional claim of safe rollback with a neutral failure
  heading. Label acknowledgement “Close notification” / “Bildirimi kapat” and
  explain that it does not start an update.
- Keep the raw diagnostic in the agent's existing failed-request record. Generic
  diagnostics remain subject to the existing panel summary restrictions.

Package locks, admission checks, immutable idle proofs, transaction recovery,
and user-initiated update behavior are unchanged. This fixes reporting of a
safe refusal; it does not suppress or terminate the OS package manager.

## Verification

- `deploy/test-update-failure-report.sh`: real error/EXIT functions with isolated
  host-action stand-ins; causal failure survives cleanup, package conflict is
  refused, unsafe/completed states are not labeled unchanged, legacy size bound,
  stale-error clearing and multiline diagnostics.
- `deploy/test-update-quiesce-exit-contract.sh update.sh`: passed, including its
  deliberate fail-closed fault-injection scenarios.
- Release updater rollback and bootstrap update contracts: passed.
- Agent system-update, idle/package and bounded-output tests: passed. Additional
  summary precedence and BIND-cause regression tests passed.
- Panel legacy-summary boundary regression test: passed.
- Frontend suite: 359 tests passed; TypeScript and production build passed.
- Local Chrome fixture: TR/EN at 1440 and 390 pixels; localized cause and next
  action visible, no horizontal overflow or JavaScript errors, acknowledgement
  dismisses the result, and no API writes occur. Screenshots inspected for the
  Turkish mobile and English desktop cases. No production browser was used.

Local reproduction fixture and raw logs are kept in `.tmp-update-failure-*`.
The browser fixture's Alpha.67 target is illustrative, not a published release.
