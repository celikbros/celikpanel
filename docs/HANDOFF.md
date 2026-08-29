# Engineering Handoff

*Baseline: August 29, 2026 · [Türkçe](HANDOFF.tr.md)*

This is the entry point for an engineering team taking over CelikPanel. It
identifies the frozen source baseline, the documents that carry authority, the
minimum development and release workflow, and the information that must be
transferred outside the repository.

This file is not evidence that a server is healthy or current. Live facts are
recorded separately in [LIVE-STATE-2026-08-29.md](LIVE-STATE-2026-08-29.md).
Open technical and operational risks are tracked in
[RISK-REGISTER.md](RISK-REGISTER.md).

## 1. Frozen handoff baseline

| Item | Value | Status |
|---|---|---|
| Canonical source tag and release | [v0.1.0-alpha.51](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51) | VERIFIED handoff baseline |
| Canonical source commit | 45d01ffb29013b9457180072c3b25ab24d5ff7bd | VERIFIED handoff baseline |
| Current published binary | v0.1.0-alpha.51 | VERIFIED |
| Alpha51 tagged-release CI | Passed for the published release | VERIFIED |
| Alpha51 release assets | Exactly 6 immutable assets; official Ed25519 manifest and detached signature verified | VERIFIED |
| Manifest-authorized archive | SHA256 `57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`; 22,644,115 bytes | VERIFIED |
| Git tag signature | The Git tag is unsigned | VERIFIED; the Ed25519 manifest, not the tag signature, is update authority |
| Handoff documentation on `main` | [PR #74](https://github.com/celikbros/celikpanel/pull/74), merge commit `e29df589594b2b5929d067a0174ab98d8182e4b5` | VERIFIED; documentation-only, not part of the Alpha51 binary baseline |
| GitHub open pull requests | 5 drafts: [#69](https://github.com/celikbros/celikpanel/pull/69), [#70](https://github.com/celikbros/celikpanel/pull/70), [#71](https://github.com/celikbros/celikpanel/pull/71), [#72](https://github.com/celikbros/celikpanel/pull/72), [#73](https://github.com/celikbros/celikpanel/pull/73) | VERIFIED as of 2026-08-29; none is part of Alpha51 |
| Product lifecycle | Alpha / pre-release | REPOSITORY-DECLARED |
| Production readiness | Not production ready | REPOSITORY-DECLARED in SECURITY.md |
| Boston public panel recovery proof | HTTP 200; Alpha51 reported | VERIFIED handoff observation |
| Frankfurt current live version | Not proven in this handoff | UNKNOWN / REVERIFY |

A tag, source commit and public HTTP response do not prove the installed panel
commit, agent commit, database schema, DNS role, firewall state, certificate
state or rollback readiness. Do not infer those values. Complete the read-only
live-state checklist before any deployment or panel mutation.

Draft PR #69 covers migration DDL canonicalization. Draft PR #70 covers restart
acknowledgement UX and requires a product decision. Draft PR #71 preserves CI
duplicate-release validation work from `agent/ci-fast`. Draft PR #72 preserves
archival SSL/backup WIP from `agent/ssl-hostnames-hsts`; it is archival and must
not be merged as-is. Draft PR #73, head `archive/alpha35-portal-tooling` at
commit `0ef899f3cb96390c4ef3822f199eddc67bb0ee1f`, archives five unique Alpha35
portal scripts and an unpublished PR72-follow-up patch. It is archival and must
not be merged or executed as-is. This handoff makes no claim that every check
on any draft is green. PR #74 merged the documentation-only handoff into
`main`, putting `main` ahead of the Alpha51 source tag without creating or
replacing a published binary release.

## 2. Legal and authority boundary

CelikPanel is proprietary software owned by CELIKBROS. An incoming company,
contractor or engineer needs explicit written authorization from CELIKBROS
before receiving or using the source, release assets or operational access.
The authorization record belongs in the external handoff register, not in this
repository.

The panel user owns live panel changes. DNS, nameservers, DNSSEC, SSL, mail,
firewall, services, domains, users and databases are changed through the panel.
Deployment tooling may install or roll back reviewed CelikPanel artifacts, but
must not silently change panel settings. SSH is read-only for diagnosis except
for an explicitly reviewed product update, bootstrap or rollback procedure.

## 3. Sources of truth

Use this order when facts disagree:

1. The exact reviewed source commit, tests and packaged release contracts.
2. deploy/release-sequence-policy and the release-pinned
   download-portal/get.sh for source release identity.
3. docs/release-signing.md and the release contract tests for signed artifact
   construction and trust.
4. docs/OPERATIONS.md for operational ownership and rollout rules.
5. docs/DECISIONS.md for durable product and architecture decisions.
6. The dated live-state record for observations about actual servers.
7. ROADMAP.md and docs/AUTOPSY.md for planned work, debt and historical
   context; neither is proof of current runtime state.

The handoff package now on `main` reconciles the previously identified
source-document drift:

- docs/OPERATIONS.md and its Turkish pair now describe snapshot v6, rejection
  of v4/v5 by the current rollback path, and the matching historical-release
  boundary for older snapshots.
- README.md now identifies the Ed25519 signed-manifest flow as tagged-release
  update authority and confines GPG signing to optional local use.
- README, Roadmap, UI architecture and web onboarding are aligned with Alpha51,
  and the unused root create-vite scaffold is removed.

R-001, R-002 and R-010 are therefore closed on `main`. R-012 is also
closed/mitigated on `main` for the root scaffold and duplicate
worktree/debris cleanup: after unique work was preserved in PR #72 and PR #73,
the cleanup removed 109 registered duplicate worktrees, 105 stale local
branches, 56 stale remote branches, `.attic`, `.worktrees`,
`.claude/worktrees`, root `__pycache__`, and the temporary handoff worktree.
Only the primary registered worktree remained. Tracked `.design-sync` was
retained intentionally. The incoming team must still verify a fresh, clean
`main` checkout at acceptance. This repository cleanup made no live-server
change and is not live-state evidence. For binary release and rollback
authority, continue to use the immutable Alpha51 scripts and contract tests;
the documentation-only `main` changes do not replace them.

## 4. Runtime architecture

~~~text
Browser
  │ HTTPS :2083
  ▼
Panel — unprivileged celikpanel user
  │ SQLite state + sealed application secrets
  │ authenticated local Unix-socket RPC
  ▼
Agent — root process, celikpanel group
  │ sole host-mutation authority
  ▼
Managed services and host configuration
~~~

Important installed paths:

| Purpose | Path |
|---|---|
| Panel and agent binaries | /opt/celikpanel/bin/panel and /opt/celikpanel/bin/agent |
| Embedded web assets | /opt/celikpanel/web/ |
| Panel database | /var/lib/celikpanel/celikpanel.db |
| Panel encryption key | /var/lib/celikpanel/secret.key |
| Agent private state | /var/lib/celikpanel-agent-private/ |
| Agent RPC socket | /run/celikpanel/agent.sock |
| Agent RPC token | /etc/celikpanel/agent.token |
| Panel configuration | /etc/celikpanel/panel.env |
| Installed signed updater | /usr/libexec/celikpanel/get.sh |
| Release trust state | /var/lib/celikpanel-release-state/ |
| Immutable releases and update snapshots | /var/backups/celikpanel/ |

Paths identify responsibility, not permission to read or copy their contents.
Never place database rows, secret.key, agent.token, private keys, credentials or
raw production logs in a commit, issue, chat or handoff document.

## 5. Repository map

| Area | Responsibility |
|---|---|
| cmd/panel | HTTP server, UI serving, authentication, authorization and panel-side orchestration |
| cmd/agent | Privileged RPC daemon and host/service operations |
| internal/db/migrations | Ordered SQLite schema migrations |
| internal/transport | Panel-agent RPC contracts and Unix-socket transport |
| internal/core | Shared catalog and domain rules |
| internal/services | Service configuration and orchestration helpers |
| internal/secrets | Sealed-secret implementation |
| web | React/TypeScript user interface and UI contract tests |
| deploy | Release, recovery, signing, publication, systemd and contract tests |
| download-portal | Public bootstrap and download portal |
| docs | Decisions, operations, security boundaries and product contracts |
| .github/workflows | Required CI, package and tagged-release jobs |

## 6. Development and verification

The reviewed backend compiler is exactly Go 1.26.5 with automatic toolchain
download disabled. Tagged release CI uses Node 24.18.0. Use Linux or a
Linux-compatible environment for the shell release contracts; the production
portal publisher additionally has a Windows PowerShell 5.1 contract test.

Minimum local checks for an ordinary change:

~~~bash
make test vet web
cd web
npm ci --no-audit --no-fund
npm test
npm run build
~~~

The GitHub CI workflow is the complete gate. It adds Go formatting, build,
race-test shards, shell syntax, release/recovery contracts, web tests,
cross-compilation and reproducible packaging. A local subset passing is not a
substitute for a green workflow on the exact pushed commit.

Release packaging is release-engineering work, not the normal development
loop:

~~~bash
make dist VERSION=<exact-version> COMMIT=<full-commit> SOURCE_DATE_EPOCH=<commit-epoch>
~~~

Never build or publish a release from a dirty checkout. Never treat generated
bin, web/dist or dist content as source authority.

## 7. Change and review rules

- Work from a clean checkout of the canonical repository, not a copied
  alpha*-wt directory or an unregistered local worktree.
- Keep English and Turkish documentation in sync.
- Follow docs/CONVENTIONS.md for identifiers, UI strings and commit messages.
- Preserve the Panel/Agent privilege boundary. Request-provided paths, URLs,
  package facts or service identities must not become root authority.
- Add tests at the boundary that changed, including failure and recovery paths.
- A source-tree test is not live deployment evidence.
- Do not advance a Git tag, release sequence or download portal independently.
- Do not edit a live database, transaction marker, mutation journal, release
  floor or DNS engine state by hand.

## 8. Release and rollout handoff

The [Alpha51 GitHub release](https://github.com/celikbros/celikpanel/releases/tag/v0.1.0-alpha.51),
its tag/commit identity, tagged-release CI, exactly six immutable assets, and
the official Ed25519 manifest/signature are verified for this handoff. The
manifest-authorized archive is 22,644,115 bytes with SHA256
`57d0321a13388392872bc3aef9af62646e2d700c23a4e0305d479df1e80ff365`.
The six assets are the generic archive and checksum, Linux/amd64 archive and
checksum, signed manifest and detached signature.

The Git tag itself is unsigned. Installation/update authority comes from the
pinned Ed25519 trust anchor and verified signed manifest v2, not from a Git tag
signature. The documentation-only handoff is now merged and `main` is ahead of
the source tag, while the current published binary remains Alpha51.

Before the next release:

1. Review any subsequent handoff/documentation changes independently of
   binary-release identity; the Alpha51 reconciliation is already merged.
2. Prove that the new tag points to the reviewed main commit and CI is green.
3. Verify the exact six GitHub release assets and their checksums/signature.
4. Assemble and publish the portal candidate using the tracked publisher only.
5. Record sanitized release evidence and the immutable artifact digests.
6. Verify Boston first.
7. Touch Frankfurt only after every Boston gate passes.
8. Use only the matching immutable release rollback helper and v6 snapshot.

The current live versions, schema and rollback snapshots are not established by
this file. See the live-state record.

## 9. Access and secrets handoff

Actual account names, key locations, recovery codes, credentials, tokens,
provider identifiers and custodian names must be transferred in an approved
external password manager or access register. The repository records only the
required categories:

| Access category | Repository value |
|---|---|
| CELIKBROS written authorization | OUT-OF-REPO / ASSIGN CUSTODIAN |
| GitHub organization and repository administration | OUT-OF-REPO / ASSIGN CUSTODIAN |
| GitHub release-signing secret administration | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Monotonic release-sequence administration | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Ed25519 private-key custody and recovery copy | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Download portal host and publisher SSH identity | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Boston and Frankfurt VPS administration | OUT-OF-REPO / ASSIGN CUSTODIAN |
| celikpanel.net and celikhost.com registrar/DNS control | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Panel administrator accounts | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Backup encryption and recovery material | OUT-OF-REPO / ASSIGN CUSTODIAN |
| Security-reporting and incident escalation channel | OUT-OF-REPO / ASSIGN CUSTODIAN |

Grant dedicated accounts and SSH public keys; do not share passwords or private
keys. Record grant, review, rotation and revocation dates externally. Remove
departing-team access only after the incoming team has completed a verified
read-only inventory and a controlled access test.

## 10. Incoming-team first-day checklist

- Confirm written authorization and external custodian assignments.
- Clone a fresh checkout and prove tag, commit, origin and clean status.
- Review the verified Alpha51 CI/release evidence and the five draft PRs; do not
  treat any draft as part of the Alpha51 baseline. PR #72 and PR #73 are
  archival; do not merge either as-is, and do not execute PR #73 as-is.
- Read SECURITY.md, OPERATIONS.md, release-signing.md, DECISIONS.md and this
  handoff set before accessing a server.
- Complete every UNKNOWN / REVERIFY field in the live-state record read-only.
- Confirm whether Boston and Frankfurt are test, staging or production systems.
- Prove current backup coverage and whether secret.key and other control-plane
  keys are recoverable.
- Assign owners and target dates to every open risk outside the repository.
- Make no live mutation until the source baseline, live baseline and rollback
  route are independently proven.

## 11. Handoff completion criteria

The engineering handoff is complete only when:

- the shared repository checkout is clean and contains no duplicate worktree
  copies;
- the canonical tag, full commit, CI run and six release assets agree;
- operational documentation matches snapshot v6 and Ed25519 authority;
- both server inventories have dated read-only evidence;
- environment classification and current installed identities are explicit;
- a disaster-recovery decision and restore evidence exist;
- external access, legal authorization, custodians and escalation are assigned;
  and
- no secret value has entered the repository or handoff documents.
