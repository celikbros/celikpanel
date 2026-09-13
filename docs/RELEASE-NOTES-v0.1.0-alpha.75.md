# CelikPanel v0.1.0-alpha.75

*Prepare infrastructure DNS inside server setup · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.75.tr.md)*

The setup wizard can now prepare explicitly reviewed infrastructure DNS records
before checking public hostname resolution and requesting panel or mail TLS
certificates. An installed DNS engine alone no longer counts as evidence that a
hostname resolves. Administrators do not have to leave setup and create a tenant
domain to prepare these infrastructure records.

On a local primary, select the exact owner zone and review the minimal SOA, NS
and A records, including an optional peer panel hostname. Existing unrelated
records are preserved; ownership conflicts and detected native owner edits block
publication. No website, mailbox or tenant domain is created by this step.

Standard primary/secondary transfer remains independent of remote panel HTTPS.
If the primary catalog or public access records are unavailable, setup explains
what is missing and what to do next. Pending operations retain their identity;
confirmed failures require recovery and another reviewed plan.

## Continuing an incomplete setup

After publication, install **v0.1.0-alpha.75** yourself through **Settings →
CelikPanel updates** on each server. Publication does not update installed panels.

Completed service installations remain in place. Updating does not restart a
failed setup or rewrite its accepted plan. Open **Settings → Server setup** and
review a revised plan to include the new DNS preparation steps. Confirm the
owner zone, server addresses and optional peer panel hostname before starting.
Start the primary DNS preparation and then the secondary; do not wait for all
primary hosting steps or its certificate before starting the secondary.

Existing package-manager or server-operation conflicts still require their own
recovery. This release does not bypass update locks or claim to fix Boston's
separately reported Nginx busy failure.

## Validation and limits

Full local Linux suites passed: panel 1,258 top-level tests, agent 1,341 and DNS
wire 5. The final focused setup suite passed 104 top-level tests; frontend checks
passed 436 tests, production build and unchanged bundle budgets. Eight local
browser fixture views cover English/Turkish desktop/mobile setup and wait states.
These checks do not establish live Frankfurt/Boston DNS transfer or public CA
success. See [the implementation boundary](SERVER-SETUP-ACCESS-DNS.md).
