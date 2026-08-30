# Incident Report — Update and DNS Recovery Failures Beginning August 26, 2026

*Prepared: August 30, 2026 · [Türkçe](INCIDENT-2026-08-26-UPDATE-DNS-RECOVERY.tr.md)*

## 1. Document status

| Field | Value |
|---|---|
| Incident status | Alpha52 remediation and two-node live acceptance verified; incident remains OPEN for child-zone/public-authority proof and external ownership |
| Start | 2026-08-26 06:15 UTC, first preserved failed-update evidence |
| End | Not declared; closure requires the remaining criteria in section 10 |
| Severity | OUT-OF-REPO / ASSIGN under the approved severity model |
| Incident commander | OUT-OF-REPO / ASSIGN |
| Affected surfaces | Signed self-update, update rollback/recovery, authoritative DNS operations, operation-progress UI |
| Public release containing the consolidated remediation | [v0.1.0-alpha.52](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.52) |

This report is secret-free. It distinguishes evidence classes:

- **VERIFIED:** preserved product output, service state, immutable release
  metadata, reviewed code or a regression test demonstrates the fact.
- **USER-OBSERVED:** the operator supplied a screenshot or direct observation;
  it may not prove the server-side cause.
- **INFERRED:** a conclusion consistent with evidence but not independently
  proved; it must not be presented as fact.
- **UNKNOWN:** sufficient evidence is unavailable.

## 2. Executive summary

Several failures were experienced as one recurring problem: an operation could
stop or restart the panel, leave a recovery transaction pending, or continue
server-side while the browser exposed only a long-lived blocking overlay. The
product did not always publish and recover the authoritative terminal result
at the same boundary where host mutation ended. DNS and self-update also did
not share the complete host-plus-terminal-publication exclusion boundary.

The result was an unsafe operational experience even when fail-closed behavior
prevented a second mutation: the user could not reliably distinguish active
work, connection loss, verified rollback, or stale client state. Recovery then
required product-specific diagnosis that an ordinary customer could not be
expected to perform.

Alpha52 consolidates the source fixes and regression tests for terminal
publication, cross-subsystem admission, bounded client recovery and exact
abandon/tombstone behavior. The signed GitHub release and portal publication
are verified. Both nodes subsequently passed Alpha52 live acceptance. The
incident remains open because the `celikhost.com` child zone is absent, the
public-authority matrix fails, and external ownership/remaining actions are not
closed.

## 3. Impact

### Verified impact

- Frankfurt panel and agent were left stopped after the failed Alpha45 update
  and repeated rollback attempts.
- The installed Alpha45 binaries and release floor existed while the update
  receipt reported failure and transaction markers remained active.
- A later rollback restored Alpha44 binaries and the canonical database, but a
  package/mutation lock prevented the agent from starting and left
  `completion.pending`.
- The Alpha46 recovery path later finalized the pending rollback from verified
  provenance.

### User-observed impact

- DNS changes displayed long-running blocking overlays without actionable
  phase progress.
- A Frankfurt BIND attempt eventually displayed a verified rollback after an
  elapsed time of approximately 1 hour 16 minutes.
- A later Frankfurt BIND operation completed, while the Boston PowerDNS change
  remained in verification for an unexpectedly long period.
- A signed self-update displayed connection interruption and remained locked
  for more than ten minutes from the browser's perspective.

### Unknown or not proven

- No customer-data loss has been demonstrated.
- Continuous public DNS availability for every affected interval was not
  captured in a complete evidence set.
- The total affected-user count and business impact were not recorded.
- A complete clean-host disaster-recovery proof does not exist.

## 4. Sanitized UTC timeline

| Time | Evidence class | Event |
|---|---|---|
| 2026-08-26 06:15:16–06:15:17 | VERIFIED | Alpha44→Alpha45 update stopped the agent and panel; the panel received SIGSTOP and then SIGKILL, and both units ended inactive/failed. |
| 2026-08-26 06:15:26 | VERIFIED | Update receipt recorded `failed`; Alpha45 panel/agent binaries and floor 45 were present, while `active` and `transaction.lock` remained. |
| 2026-08-26 10:33 | VERIFIED | First preserved rollback retry stopped with “panel TLS compatibility state could not be restored”; services remained stopped. |
| 2026-08-26 10:42 | VERIFIED | A second retry restored more state but rejected the canonical database parent metadata as neither secure-normal nor recoverable-quarantine. |
| 2026-08-26 11:16 | VERIFIED | The canonical database and Alpha44 binaries were restored; agent startup failed because the host package/mutation lock was busy, leaving `completion.pending` and both services stopped. |
| 2026-08-26 13:14 | VERIFIED | The Alpha46 reviewed recovery finalized the pending rollback using dual verified provenance and instructed a fresh normal update. |
| Later operator session | USER-OBSERVED | A DNS operation remained “in progress,” then surfaced a rollback with approximately 1 hour 16 minutes elapsed and no sufficiently useful live phase view. |
| Later operator session | USER-OBSERVED | Frankfurt BIND later committed in about 27 seconds; Boston PowerDNS subsequently remained in verification behind a navigation lock. |
| Later operator session | USER-OBSERVED | A signed self-update connection interruption remained client-visible as an unresolved operation for more than ten minutes. |
| 2026-08-30 00:30 | VERIFIED | Alpha52 tagged workflow completed successfully and published the consolidated remediation release. |
| 2026-08-30 later | VERIFIED | Boston and Frankfurt produced terminal-success Alpha52 receipts for exact build `adb25d8ec487dcb76dd95304a551d8cb37565115` and passed bounded host/release acceptance. |
| 2026-08-30 later | VERIFIED | Pre-zone Frankfurt BIND-primary/Boston PowerDNS-secondary catalog serial `1` and source-bound AXFR passed in both directions. Parent delegation/glue passed; the absent child zone returned `REFUSED`, `NOTAUTH` and public `SERVFAIL`. |

Raw logs, private paths, operator accounts and credentials remain outside the
repository. Evidence references must be assigned in the approved incident
register.

## 5. Technical causes

This was not a single timeout. The reviewed failure chain contained several
distinct boundary defects.

### 5.1 Update rollback and recovery coupling

The failed update crossed the service-stop boundary before every compatibility
and restore precondition could be reconfirmed. Subsequent recovery encountered
strict metadata and mutation-lock checks. Those checks correctly refused to
guess, but the product left an ordinary installation unavailable and required
specialized recovery knowledge.

### 5.2 Host completion and terminal publication were separate windows

DNS host mutation could finish and release its host lock before the final
durable service-operation receipt was published. Another subsystem could see
an idle ledger during that window. This violated the intended invariant that a
second privileged mutation cannot enter until the first operation's terminal
state is durable and observable.

### 5.3 Cross-subsystem self-update admission was incomplete

Self-update admission acquired the host lock but did not also acquire the DNS
terminal-publication lock. A goroutine regression test reproduced the race:
self-update could acquire its lease after DNS released the host lock but before
DNS published its terminal receipt.

### 5.4 Client ambiguity was treated as an indefinitely active operation

Connection interruption, repeated not-found responses and a restarted panel
did not have one bounded, server-authoritative abandonment contract. The UI
therefore retained a full-screen navigation lock without being able to prove
whether the operation was active, terminal, missing or superseded.

### 5.5 Late-start protection was incomplete

Without an exact durable tombstone, an abandoned request identity could be
accepted later by a delayed start path. Client-only cleanup would therefore
have been unsafe: it could hide an operation that still had authority to begin.

## 6. Why existing safeguards did not prevent the incident

- Component-level tests did not cover the host-release/terminal-publication
  interleaving across DNS and self-update.
- Fail-closed recovery protected state but did not provide a bounded customer
  recovery route or adequate progress evidence.
- Browser state carried too much responsibility for deciding whether the
  operation was still authoritative.
- Live two-node acceptance, disposable-VM rollback/reboot evidence and an
  incident owner/escalation model were incomplete.
- The operation UI initially presented success or retry messages before the
  authoritative final state was available, inviting additional clicks.

## 7. Alpha52 remediation

| Remediation | Source/review result | Live result |
|---|---|---|
| Composite service-mutation acquisition follows host → terminal-publication order | Implemented and regression-tested | PARTIALLY VERIFIED by successful bounded live operations; concurrency race not deliberately recreated live |
| Self-update cannot enter during the DNS terminal-publication window | Reproducing test failed before the fix and passes after it, including race mode | PARTIALLY VERIFIED; no conflicting live operation observed |
| DNS terminal receipt retains publication exclusion until durable final state | Implemented and tested | VERIFIED by terminal Alpha52 receipts on both nodes |
| DNS startup upgrades exact recoverable legacy states and retries guarded expiry | Implemented and tested | OPEN / REVERIFY |
| Self-update abandonment requires exact full identity and server authority | Implemented and tested | OPEN / REVERIFY |
| Durable tombstone rejects a delayed start for an abandoned identity | Implemented and tested | OPEN / REVERIFY |
| Ambiguous timeout, authorization or 5xx responses remain pending; exact terminal failure alone releases client state | Implemented and tested | OPEN / REVERIFY |
| Update and DNS overlays expose operation identity and preserve navigation lock until a verified final result | Implemented and web-tested | OPEN / REVERIFY |
| Alpha52 signed GitHub release and portal publication | VERIFIED in the Alpha52 release record | Exact build installed and accepted on both live nodes |

The exact release identity and artifact evidence are in
[RELEASE-EVIDENCE-v0.1.0-alpha.52.md](RELEASE-EVIDENCE-v0.1.0-alpha.52.md).

## 8. Customer-safe recovery rule

1. Do not start a second DNS or update operation.
2. Keep the exact operation ID and UTC time visible; do not expose credentials.
3. Allow automatic status retries while the server reports a non-terminal
   authoritative operation.
4. If the connection is interrupted, do not infer failure or success from the
   browser. Reconnect to the same operation.
5. Use the product's exact abandon action only when the server accepts the full
   identity and publishes the failure/tombstone result.
6. Never ask a customer to edit the database, transaction markers, mutation
   journal, release floor or DNS daemon state.
7. SSH remains read-only diagnosis unless an immutable, reviewed release
   recovery procedure explicitly authorizes a bounded mutation.

Support must request only sanitized version/commit, operation ID, terminal
phase, unit state and bounded journal evidence. Passwords, private keys, tokens,
raw databases and unrestricted logs are never support artifacts.

## 9. Corrective actions

| ID | Action | Status | Acceptance evidence | Owner / target |
|---|---|---|---|---|
| IR-001 | Publish the composite DNS/self-update lock and terminal-receipt fixes | COMPLETE IN ALPHA52 SOURCE/RELEASE | Exact commit, green CI and signed release | OUT-OF-REPO / ASSIGN |
| IR-002 | Validate Alpha52 signed update on Boston, then Frankfurt | COMPLETE / SNAPSHOT PROVENANCE WARNING R-016 | Per-node receipts, exact build, floor 52, units, idle state, artifacts and v6 rollback proof | OUT-OF-REPO / ASSIGN |
| IR-003 | Reprove BIND-primary/PowerDNS-secondary catalog and public DNS after both updates | PARTIAL / BLOCKED UNDER R-015 | Pre-zone AXFR/catalog and delegation/glue pass; child-zone/post-zone public proof remains | OUT-OF-REPO / ASSIGN |
| IR-004 | Preserve one browser golden-path record for update interruption/reconnect and DNS completion | OPEN | Authenticated bounded browser evidence tied to Alpha52 | OUT-OF-REPO / ASSIGN |
| IR-005 | Complete real disposable Debian, Ubuntu and Arch install/update/rollback/reboot gates | OPEN under R-007 | Evidence tied to exact artifact digest | OUT-OF-REPO / ASSIGN |
| IR-006 | Complete clean-host control-plane restore drill | OPEN/BLOCKER under R-003 | Encrypted backup and verified restore with accepted RPO/RTO | OUT-OF-REPO / ASSIGN |
| IR-007 | Assign severity, incident commander, on-call, communications and action owners externally | OPEN under R-014 | Approved external incident register | OUT-OF-REPO / ASSIGN |
| IR-008 | Adopt and exercise the bilingual incident template | PARTIALLY MITIGATED IN REPOSITORY | Reviewed incident exercise and accountable sign-off | OUT-OF-REPO / ASSIGN |

## 10. Closure criteria

This incident may be marked closed only when:

- Boston and Frankfurt independently retain the already-passed Alpha52 panel,
  agent, UI, schema, floor and operation-idle evidence;
- the child zone is published and the required post-zone DNS pair/public
  resolution matrix passes;
- no update or DNS request remains ambiguously active, missing or restartable;
- the bounded browser reconnect/terminal behavior is accepted;
- every remaining corrective action is complete, explicitly risk-accepted by
  an accountable external owner, or transferred with a target date; and
- the new dated live-state record and risk register cite sanitized evidence.

Release publication alone does not close the incident.
