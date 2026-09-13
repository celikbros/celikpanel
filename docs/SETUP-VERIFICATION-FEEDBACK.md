# Setup verification feedback

Alpha76 release source, 2026-09-13. Installed panels are updated by their owner.

## Problem

The final verify step was a no-op that received a succeeded receipt before the
completion checks ran. A waiting setup could therefore show every step as
Completed. The manual check refreshed evidence but gave no visible result when
the requirement remained unmet. Older execution checks could obscure the fresh
manual response.

## Behavior

Final verification remains pending until fresh readiness checks and the guarded
setup completion both succeed. Waiting executions written by older versions
have their premature verification receipt normalized; succeeded service steps
are preserved. Plan revision permits a pending final verification only after all
installation steps succeeded and no child operation belongs to that final step.

The UI also handles older receipts and distinguishes installation completion
from setup readiness. Check requirements again shows progress and an adjacent,
announced result for unmet requirements, unknown evidence, transport failure,
or passed checks awaiting final completion. Its fresh checks take precedence
over older execution evidence. A succeeded execution alone cannot declare the
server ready. These read requests do not install services or change DNS/PTR.

## Validation

- Linux Go 1.26.5: all cmd/panel TestServerSetup tests passed, including legacy
  receipt recovery, preservation of child installations and plan revision guards.
- Frontend: 442 tests passed; production build and bundle budgets passed.
- Local Chrome with mocked APIs: EN/TR, 1440/390 pixels, four check outcomes
  (16 cases), no horizontal overflow, JS errors, writes or external requests.
- Screenshots reviewed for mobile unmet requirements and desktop unknown checks.

No installed panel was updated or reconfigured. A mail_identity requirement
needs evidence from the actual mail hostname and matching forward/reverse DNS;
this UI correction does not prove or repair that requirement.

## Read-only incident evidence

On 2026-09-13, DNS queries through 1.1.1.1 returned:
- 72.62.38.15 PTR: server1.celikhost.com.
- mail.frankfurt.celikhost.com A: 72.62.38.15.

The saved wizard mail hostname is mail.frankfurt.celikhost.com. This is a concrete
reverse-DNS mismatch for setupMailHostIdentityReady. It does not establish whether
other checks also fail. The server owner's provider must apply a PTR change;
no DNS records were changed by this investigation.
