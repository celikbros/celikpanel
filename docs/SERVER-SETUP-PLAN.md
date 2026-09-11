# Guided server setup

*Approved product direction: September 10, 2026 · [Türkçe](SERVER-SETUP-PLAN.tr.md)*

**Status: approved source implementation and local validation completed; not released.**
See [implementation status and evidence](SERVER-SETUP-STATUS.md) for the current
boundary. The operator approved this direction after reviewing the activation,
Dashboard and Components screens.
This document records that agreement and the implementation acceptance criteria.
It does not authorize an assistant to update any installed panel; the user starts
every installed-panel update from CelikPanel itself, as required by [AGENTS.md](../AGENTS.md).

## September 11 follow-up: explicit guidance preference (Alpha66 release source)

After updating Alpha65, the operator could not find the setup entry or decline
the wizard. On first use, an administrator with a fresh or unassessed legacy
state now chooses **Set up with the wizard** or **I'll configure it myself**.
This is a short choice, not a claim that an upgraded host is empty. Completed
installations and existing in-progress operations retain their state.

The choice persists for the server across sessions. Manual mode suppresses the
automatic wizard and Dashboard invitation; **Settings → Server setup** remains
available to reopen it. Switching the preference does not complete setup, alter
its draft, remove readiness checks, change licensing, or run host operations.
An accepted active/waiting execution must be resolved before changing preference.
See [implementation and checks](SERVER-SETUP-GUIDANCE.md).

## September 11 follow-up: custom component selection (Alpha67 release source)

The operator approved customizing a purpose profile and a fifth **Custom setup**
choice inside the wizard. A dedicated Components step resolves dependencies,
explains conflicts and preserves installed software. Access, DNS, review and
verification remain in the same guided flow. Components remains the place for
later service administration. See the [customization contract and validation
boundary](SERVER-SETUP-CUSTOMIZATION.md).

## 1. Outcome and audience

A server administrator should reach a usable, secured environment without
interpreting a component catalogue or moving between unrelated settings pages.
The first question is the server's purpose. Use the existing CelikPanel visual
language, Turkish and English, responsive layouts and accessible controls.
Resellers, customers and additional users never receive host setup controls.

License activation and server setup are independent states:

| Observed server state | Destination after valid licensing |
|---|---|
| Confirmed fresh installation | Guided setup |
| Persisted incomplete setup | Resume the current step |
| Previously prepared installation | Dashboard |
| Existing installation with no setup record | Preserve access; assess read-only and offer setup without assuming the host is new |

Replacing a license, signing in again, restarting or updating must not reset
setup. An empty domain list alone is not evidence of a fresh installation.
Readiness failures on an established server must not force it through a new
installation or stop existing workloads.

## 2. Purpose profiles

| Choice | Intended outcome |
|---|---|
| Web hosting — recommended | Web and PHP hosting with the required database infrastructure; no mail by default |
| Web and mail hosting | Web hosting plus mailboxes, webmail and mail protection |
| Application server | The selected application's runtime, web access and required dependencies |
| DNS server | New authoritative DNS infrastructure or joining an existing setup as a secondary |

Profiles resolve to compatible offerings already supported on the actual host.
Customization is secondary; users need not select every package or install
unrelated tools. Do not expose an executable profile until its full lifecycle
has been implemented and verified. Profiles configure infrastructure, not a
customer's first website or mailbox. Those follow after infrastructure is ready.

## 3. DNS policy

Hosting setup must choose a DNS management model. A local authoritative DNS
engine is not universally required:

1. **Manage DNS here:** configure a supported local engine, nameserver identity
   and the required topology. Production readiness for this model includes
   independently hosted authoritative redundancy; two names on one host do not
   satisfy it.
2. **Use an existing CelikPanel DNS infrastructure:** establish authorized
   publishing/consuming relationships and verify the remote authority. A peer IP
   alone grants no management permission. A web host using remote DNS need not
   become a DNS secondary or install an authoritative engine.
3. **Use an external DNS provider:** show the exact records to create and verify
   them. Manual management is supported by the design; provider API credentials
   are optional automation, not a prerequisite.

Choose the infrastructure/default model during setup; verify each domain's
necessary records when that domain is published. An operating-system resolver
is not evidence of authoritative DNS readiness. A secondary serves transferred
DNS zones; it does not back up websites and is not automatically a publisher for
new locally created domains.

This approved direction supersedes D-009's blanket local-DNS product requirement.
The existing engine ownership, publication, topology, DNSSEC and recovery
invariants remain binding for local authority. Implement an explicit external
mode before relaxing guards; removing a frontend check alone is not this feature.
Existing domains retain their DNS ownership until an explicit supported migration.

## 4. Secure access and mandatory checks

The initial implementation uses a public panel FQDN, for example
`panel.example.com`, with a trusted certificate and verified automatic renewal.
A new domain purchase is not required. The starting self-signed certificate is
an entry/recovery mechanism, not evidence that public panel setup is complete.
The panel address, operating-system hostname, nameserver names and mail identity
are distinct; collecting one must not silently rewrite the others.

IP certificates are a possible later path, not a current CelikPanel capability.
Private-only deployments and their trust model require a separate explicit design;
they must not become an unchecked bypass of the public hosting profile.

| Requirement | Enforcement point |
|---|---|
| Trusted panel HTTPS and working renewal | Before marking initial public setup complete |
| Firewall policy preserving panel/SSH access and allowing required services | Before marking initial setup complete |
| Installed, healthy and compatible dependencies | Before enabling the corresponding feature |
| Correct domain DNS and website certificate | Before reporting the corresponding HTTPS site as published |
| Mail identity, TLS, necessary DNS/authentication records and delivery prerequisites | Before reporting the corresponding mail service/domain as ready |

Mail preparation includes SPF, DKIM and DMARC; reverse DNS and connectivity must
be checked where relevant. Separate host readiness from per-domain readiness.
An external PTR or registrar change remains an explicit operator task when the
panel has no authorized integration to perform it.

Checks are enforced on the server, not only through disabled buttons. Missing
mail prerequisites do not block web hosting. Recovery/configuration access stays
available when a check fails. No check automatically stops existing websites,
mail, databases, scheduled work or certificate renewal.

## 5. User flow and operation contract

**Purpose → Panel address and DNS method → Review plan → Start setup → Verify result**

- Read current state automatically using bounded read-only checks. Never install
  software, rewrite settings or start a scan with configuration side effects just
  because the page opened.
- Present one primary action per step. Show only relevant inputs. Keep advanced
  details available without turning the first screen into Components.
- Before mutation, show the concrete components, configuration changes, access
  rules and outstanding external tasks. The user starts the reviewed plan.
- Use durable, resumable operations with exact plan identity and dependency order.
  Page refresh, reconnect or agent/panel restart must neither lose the operation
  nor execute it twice. Conflicting operations must not run concurrently.
  A lost HTTP response does not prove failure; reconcile the saved operation
  before retrying. Unmanaged configurations and existing customer data are never
  overwritten or deleted by automatic recovery.
- Reuse existing audited service/DNS/certificate operations. Setup does not gain a
  new arbitrary-command path to the privileged agent.
- Show honest stages, results and actionable failures. External DNS propagation
  is a waiting state, not a guessed success or a percentage countdown.
- Changes to material inputs invalidate the previous review. Recovery must
  distinguish completed, not-started and uncertain steps; do not promise that a
  package installation can always be fully undone.
- Report completion only from fresh evidence. End with one purpose-specific next
  action: add a first site, deploy an application or connect/manage DNS.

Afterward the Dashboard summarizes the selected role, existing resources and
actionable issues. It must not keep showing a fresh-server checklist once setup
has completed. Components remains available for later administration.

## 6. Implementation order

1. **Persisted state and routing:** fresh-install provenance, conservative upgrade
   behavior, admin-only setup APIs, resumable drafts and independent license gating.
2. **Readiness and DNS models:** evidence schema; local/existing/external ownership;
   replace D-009 gates consistently across domain, capability and mail paths.
3. **Secure-access workflow:** panel address, trusted TLS/renewal and safe firewall
   changes using existing operation machinery.
4. **Purpose plans and execution:** supported offering resolution, exact plan
   review, durable orchestration, recovery and purpose-specific verification.
5. **Guided UI and Dashboard:** bilingual steps, progress, external actions,
   resume/error states and a single useful completion action.
6. **Validation and publication:** meet the acceptance cases below, document the
   supported paths, then publish a reviewed release. Users update their panels.

These are implementation dependencies, not permission to ship a wizard that
claims unsupported profiles or treats incomplete checks as ready.

## 7. Acceptance cases

- Fresh, resumed, established and ambiguous legacy installations route correctly;
  reactivation never causes reinstallation or resets an existing setup.
- All non-admin roles are denied host setup APIs; licensing remains independently
  enforced and license loss does not stop already running workloads.
- Every offered purpose completes on its supported host capabilities, including
  required dependency and mutually exclusive service handling.
- Local DNS, verified existing DNS and manually managed external DNS each have a
  complete working path. External DNS does not trigger a local DNS installation.
- Primary/secondary responsibilities, authorization, redundancy and publication
  are tested; a configured but unreachable or stale peer is not marked ready.
- Invalid/mismatched/expired panel certificates, failed renewal, stale evidence,
  incorrect DNS and missing mail prerequisites cannot produce false completion.
- Firewall application preserves current management access. Applicable real-VM
  reboot and recovery gates in [OPERATIONS.md](OPERATIONS.md) must pass.
- Refresh, repeated clicks, logout, concurrent sessions, restart and interruption
  cannot duplicate an installation or discard a committed operation.
- Existing workloads/configuration survive setup assessment and unrelated
  failures. A changed profile never silently uninstalls or resets live services.
- Turkish/English, desktop/mobile, keyboard navigation and actionable errors are
  verified against the actual UI. No success requires a hidden extra button.

## 8. Baseline at approval

At approval, Dashboard contained `StartGuide`; there was no persisted general
purpose wizard. Mail profiles provide an existing review/execution foundation.
Domain creation currently requires local DNS under D-009; detection of an external
nameserver is not an external-provider management mode. Panel certificate input
uses `CanonicalFQDN` and rejects IP addresses.

This historical baseline does not describe the current working tree; see
[implementation status](SERVER-SETUP-STATUS.md).

Relevant sources: `web/src/components/StartGuide.tsx`,
`web/src/components/AddDomainModal.tsx`, `cmd/panel/mail_profiles.go`,
`cmd/panel/dns_engine.go`, `cmd/panel/domain_connection.go`,
`cmd/panel/panel_cert_handler.go`, `internal/hostname/hostname.go`.
