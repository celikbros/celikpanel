# Security Policy

## Project status and supported versions

CelikPanel is currently a pre-release project and is not production ready.
Only the current `main` branch is considered for security fixes. Historical
commits, ad-hoc server bundles and locally modified builds are not supported
security releases.

The repository does not promise a response or remediation SLA yet. Security
reports are triaged according to impact and maintainer availability.

## Reporting a vulnerability

Do not disclose a suspected vulnerability in a public issue, discussion, pull
request, log paste or screenshot.

Use GitHub's private vulnerability reporting flow:

<https://github.com/celikbros/celikpanel/security/advisories/new>

If private reporting is unavailable, contact the repository owner through an
existing private channel and ask for a secure reporting channel without
including exploit details in the first message.

Include, where possible:

- the affected commit and operating system;
- the component and preconditions;
- minimal reproduction steps or a proof of concept;
- expected and observed impact;
- relevant logs with tokens, passwords, private keys, email addresses, IP
  addresses and customer data removed;
- whether the issue is already public or appears to be actively exploited.

## Safe research boundaries

- Test only systems and data you own or have written authorization to test.
- Do not probe CelikPanel installations, customer domains or live DNS
  infrastructure without explicit authorization.
- Do not perform denial-of-service, destructive, persistence, social
  engineering or data-exfiltration tests.
- Stop testing and report privately if you encounter credentials, personal data
  or access to another account.
- Never place a real secret in an issue or advisory. Revoke and rotate any
  secret that may have been exposed.

Maintainers will coordinate disclosure after a fix and a safe upgrade path are
available. Publishing details before that coordination can put installations
at risk.
