# CelikPanel v0.1.0-alpha.73

*Actionable setup and service progress · [Türkçe](RELEASE-NOTES-v0.1.0-alpha.73.tr.md)*

Setup now shows the current requirement and next action before the step list. Primary and secondary DNS guidance identifies the reviewed peer and explains when to prepare the other server. Waiting for pair verification does not claim that the peer is offline. External DNS, optional record-management authorization, certificates, firewall checks, component preparation and CelikPanel license waits have separate guidance.

DNS readiness finishes required local checks before using a bounded budget for peer transfer verification. An unavailable peer no longer consumes the response budget and discards verified local installation, running-state and ownership facts. Missing or timed-out peer proof still does not grant primary publication or secondary transfer readiness.

Progress context comes from the accepted plan and its exact operation identity, not an editable draft. Reading progress does not write state, contact the agent or start another operation. Older responses remain usable without the optional context. Service installation failures remain visible while CelikPanel checks the resulting state; connection loss is shown as an unknown result rather than evidence that installation is still running. Refreshing or dismissing an error does not retry installation.

Validation: 421 frontend tests, production build and bundle checks, focused panel/agent Go tests and race checks, and 28 Turkish/English desktop/mobile browser scenarios. These are local regression and controlled browser checks, not a claim that this release has been installed or that every service adapter has been audited.

The [operation guidance requirement](OPERATION-GUIDANCE.md) applies across the product. This release does not implement arbitrary third-party license activation or validation adapters. The [owner-independence limits](OWNER-INDEPENDENCE.md) remain unchanged; native DNS transfer stays separate from optional panel record management.

After a version change, CelikPanel rechecks recorded operations. Remaining steps in a plan bound to the previous build may require a fresh review; the update does not automatically repeat recorded installations.

## Updating

Install **v0.1.0-alpha.73** yourself through **Settings → CelikPanel updates → Check for updates** on each server. Return to **Settings → Server setup** to inspect the recorded setup operation and follow its guidance. Release publication does not update installed panels or start server setup. The release package targets Linux amd64.
