# Customizing guided server setup

Status: included in the Alpha67 release source; local validation complete.
This document records the September 11, 2026 follow-up to
[guided server setup](SERVER-SETUP-PLAN.md) and D-021. It does not describe a
released capability or authorize updating an installed panel.

## Approved flow

An administrator can start with a purpose profile and customize its component
selection, or choose a custom installation directly. The wizard keeps the
selection, access and DNS requirements, concrete review, execution and
verification together. Opening Components is not a prerequisite for preparing
a custom installation.

Profiles provide defaults. The selected components determine the actual plan,
including dependencies, service conflicts, runtime version requirements and
mail checks. Changing a selection invalidates the previous review.

Components remains the place for later component administration. The wizard
and Components use the same managed-service catalogue, supported-host rules,
dependency roles and conflict groups. Setup adds no arbitrary package or shell
command input.

Manual setup is a navigation preference. Administrators can reopen the wizard
at **Settings > Server setup** (`/settings?section=setup`); its route is `/setup`.
Reopening does not reset the saved draft or erase completed work.

## Initial selectable scope

Only services with an existing reviewed lifecycle are candidates, subject to
the actual host's capability checks:

| Category | Candidate choices | Additional constraints |
| --- | --- | --- |
| Web and applications | Nginx, PHP-FPM, Node.js | Node requires a web server and an exact reviewed runtime version. |
| Databases and tools | MariaDB, PostgreSQL, phpMyAdmin, phpPgAdmin | Web tools add their database, web server and PHP dependencies; phpPgAdmin is unavailable where its lifecycle is unsupported. |
| Mail | Postfix and Dovecot, Rspamd, Roundcube | Postfix and Dovecot form a working mail pair. Selections resolve to the existing Core Mail, Webmail and Spam-Protected Mail operations. |
| Cache | Redis, Valkey, Memcached | Redis and Valkey occupy the same service role and cannot be selected together. |
| Security | Fail2ban | Uses the existing managed-service lifecycle. |

The public panel's trusted HTTPS, renewal and firewall requirements remain
mandatory and are explained separately from optional components. Their
required tooling is included in the reviewed plan.

DNS engines remain part of the dedicated DNS step. They cannot be installed
as ordinary component checkboxes. Local, existing authorized CelikPanel DNS,
and external DNS retain their ownership and verification rules. A DNS-only
secondary may serve transferred zones; a hosting configuration must have a
verified publishing path. Choosing external DNS never silently adds a local
authoritative engine.

Apache, Exim and vsftpd remain unavailable while their integrations are
explicitly disabled. The first custom-selection implementation does not add
vendor repositories or arbitrary PHP/PostgreSQL version choices. ClamAV remains
in Components until setup can verify its helper service through an available
read-only observation. Components requiring additional workflows are not made selectable merely because their
packages appear in the catalogue.

## Preservation and operation invariants

- Dependencies are resolved before review. Automatically required selections
  identify their reason; mutually dependent mail components are presented as
  one understandable choice rather than checkboxes that cannot be cleared.
- Existing components are preserved. Clearing a wizard selection does not
  uninstall software, delete data, stop a service or remove an existing
  firewall allowance. Removal remains an explicit component operation.
- The firewall plan retains current management access and the ports required
  by both installed and newly selected services. Local-only databases and
  caches do not gain public ports.
- Mail selections use the existing durable profile operations and require
  the relevant profile receipts, mail identity, trusted renewable TLS and
  delivery checks. A generic installed-package flag is not mail readiness.
- Node completion verifies the selected version, not merely another installed
  Node version. PHP and PostgreSQL retain their actual service-unit checks.
- Draft changes use revision checks. The exact reviewed selection and resolved
  operations are bound to plan identity; retries and reconnects reconcile the
  saved operation without duplicating it.
- Existing saved plans must remain readable and retain their original identity
  and preset meaning after this feature is installed. Optional new fields must
  not change the serialization of older plans.
- Licensing, setup history, guidance preference and current service health
  remain independent. No incomplete setup check stops an existing workload.
- The user starts every installed-panel update from CelikPanel. Release
  publication and installation on Boston or Frankfurt are separate actions.

## Validation coverage

Local regression and browser checks cover the following acceptance criteria:

1. Every default profile retains its existing resolved operations. Legacy
   serialized plans still load with their original hashes.
2. Custom selections persist across reload, reject unknown or unsupported
   choices, and resolve dependencies deterministically. Equivalent reordered
   input produces the same selection; a material change requires a new review.
3. A minimal web selection, database tool selection, Node selection and each
   supported mail combination produce dependency-ordered executable plans.
   Empty custom choices cannot produce false readiness.
4. Selected and already-installed conflicts fail before mutation. Deselected
   installed services receive no uninstall or restart step and retain ports.
5. DNS-only secondary and hosting DNS prerequisites remain distinct. External
   and existing DNS do not introduce a local DNS installation.
6. Missing selected services, wrong Node versions, stopped PHP or database
   units and incomplete mail proofs prevent completion. Components whose helpers
   cannot be observed are unavailable for selection.
7. Administration, licensing, concurrent-operation admission, durable retries
   and active-operation draft protection still apply to custom selections.
8. Turkish and English UI, keyboard interaction, mobile layout, dependency
   explanations, installed-component preservation and wizard re-entry are
   checked against the implemented interface.

Evidence is recorded in [the validation report](validation/server-setup-customization-20260911/README.md).
The new selection combinations were exercised through local tests and a mocked
browser API, not a new disposable-VM installation matrix. They reuse the existing
audited lifecycle operations; this record does not extend the earlier real-profile
validation to every new combination. Already installed database web tools are
checked through package and required service evidence; no new HTTP application
health probe is claimed.
