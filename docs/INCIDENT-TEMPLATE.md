# Incident Report Template

*[Türkçe](INCIDENT-TEMPLATE.tr.md) · Copy this file; do not edit the template for a specific incident*

This template records a secret-free operational, release, DNS or security
incident. Real contacts, credentials, private evidence locations and customer
data remain in the approved external incident system.

## 1. Metadata

| Field | Value |
|---|---|
| Incident ID | `<external-id-or-secret-free-id>` |
| Title | `<short factual title>` |
| Status | Investigating / Mitigated / Monitoring / Closed |
| Severity | `<externally approved severity>` |
| Started at UTC | `<YYYY-MM-DDTHH:MM:SSZ>` |
| Detected at UTC | `<YYYY-MM-DDTHH:MM:SSZ>` |
| Mitigated at UTC | `<value or UNKNOWN>` |
| Closed at UTC | `<value or OPEN>` |
| Incident commander | OUT-OF-REPO / ASSIGN |
| Technical lead | OUT-OF-REPO / ASSIGN |
| Communications lead | OUT-OF-REPO / ASSIGN |
| External incident record | OUT-OF-REPO / ASSIGN REFERENCE |

## 2. Evidence vocabulary

- **VERIFIED:** preserved product output, immutable release metadata, reviewed
  code or a reproducible test proves the statement.
- **USER-OBSERVED:** an operator or customer reported or captured the symptom.
- **INFERRED:** consistent with evidence but not independently proved.
- **UNKNOWN:** sufficient evidence is unavailable.

Every material statement and timeline row must identify its evidence class.
Do not silently convert an inference into a fact.

## 3. Executive summary

Describe what failed, when it began, the affected customer journey and the
current verified state in no more than three short paragraphs. Do not declare
resolution merely because a patch or release exists.

## 4. Impact

| Subject | Result | Evidence class |
|---|---|---|
| Affected nodes/services | `<value>` | `<class>` |
| Affected releases/commits | `<value>` | `<class>` |
| Customer-visible impact | `<value>` | `<class>` |
| DNS/data availability | `<value or UNKNOWN>` | `<class>` |
| Data integrity or loss | `<value or UNKNOWN>` | `<class>` |
| Duration | `<value or UNKNOWN>` | `<class>` |

Record environment classification and customer-data status as UNKNOWN when
they have not been decided externally.

## 5. UTC timeline

| UTC time | Evidence class | Event | Evidence reference |
|---|---|---|---|
| `<timestamp>` | `<class>` | `<fact, observation or action>` | `<sanitized reference>` |

Use UTC, preserve ordering and distinguish the time an event occurred from the
time it was detected. Keep raw logs and private URLs outside the repository.

## 6. Detection and symptoms

- How the incident was detected.
- What the user saw.
- What the server authoritatively reported.
- Which signals were absent, misleading or delayed.
- Whether monitoring or support escalation fired.

## 7. Containment

List the exact bounded actions used to prevent further impact. State who
authorized each live mutation in the external record. Do not include ad-hoc
database edits, marker deletion, release-floor changes or daemon manipulation
as an accepted recovery procedure.

## 8. Technical diagnosis

### Confirmed root cause

Tie each cause to code, immutable release evidence, a reproducible test or
bounded live evidence.

### Contributing factors

Identify process, observability, test, documentation and ownership gaps that
increased impact without calling them the root cause.

### Rejected hypotheses

Record important hypotheses disproved by evidence so they are not repeated.

## 9. Recovery and data integrity

- Exact recovery path and authority.
- Snapshot/rollback identity without private paths or secrets.
- Panel, agent, UI, schema and service-state verification.
- DNS catalog, AXFR, UDP/TCP SOA and external resolution verification.
- Database integrity and whether any data loss is proven or remains UNKNOWN.
- Remaining degraded or unverified behavior.

## 10. Corrective actions

| ID | Action | Priority | Status | Acceptance evidence | Owner / target |
|---|---|---|---|---|---|
| `<id>` | `<bounded action>` | `<priority>` | OPEN | `<objective exit test>` | OUT-OF-REPO / ASSIGN |

Each action needs one accountable external owner, target date and objective
exit test. Publishing code is not the same as completing live acceptance.

## 11. Customer and stakeholder communication

Record the UTC time and sanitized subject of each notice, update and closure
message in the external incident system. Do not expose credentials, personal
data, exploit detail requiring private disclosure or unsupported guarantees.

## 12. Evidence index and redaction

| Evidence | Scope | Repository-safe result | External reference |
|---|---|---|---|
| `<item>` | `<bounded window/subject>` | `<sanitized conclusion>` | OUT-OF-REPO |

Never attach private keys, tokens, passwords, raw databases, unrestricted logs,
customer records or screenshots containing personal information.

## 13. Closure and retrospective

An incident closes only when:

- the terminal live state is independently verified;
- impact and data integrity are explicit, including UNKNOWN values;
- corrective actions are complete or formally transferred/risk-accepted;
- the risk register and dated live-state record are updated;
- external incident roles and acceptance are recorded; and
- follow-up learning is converted into tests, runbook rules or owned work.

| Approval | Value |
|---|---|
| Technical verification | OUT-OF-REPO / ASSIGN |
| Operational acceptance | OUT-OF-REPO / ASSIGN |
| Security review, when applicable | OUT-OF-REPO / ASSIGN |
| Closed at UTC | `<timestamp or OPEN>` |
