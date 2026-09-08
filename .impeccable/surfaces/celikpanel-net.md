---
version: 1
slug: "celikpanel-net"
primary_target: "celikpanel.net"
related_targets: ["download-portal/index.html", "download-portal/technical.html"]
---

# celikpanel.net - surface brief

**Scope and mode.** The public homepage is Persuade: help a visitor understand the hosting panel, inspect it and choose a clean test installation. The technical route is Read: explain the operating model and dated evidence. The operating panel is outside this page brief.

**Audience and job.** Administrators, hosts and agencies evaluating a control panel for their own servers. The presentation supports the global audience in both Turkish and English.

## Accepted direction: show the product

The accepted redesign replaces the previous route-diagram hero while retaining navy and white, self-hosted Overpass and Overpass Mono, ruled information and state-only signal colours. The owner accepted five concrete recommendations:

1. A stronger monochrome angular mark and differentiated Celik/Panel wordmark, shared with the application and favicon.
2. Actual panel captures for the three selected views: Overview, Domains and Databases; selectable tabs and an enlarged inspection view. These are actual interface renders with explicitly labelled sample data, not live-server telemetry.
3. A shorter headline: "Sunucunuz. Kontrol sizde." / "Your server. Your control."
4. A shorter homepage, with detailed operation-model and evidence tables moved to `/technical.html`.
5. Alpha status beside the primary action and at installation, recommending a clean test server and stating that production readiness has not been reached.

**First viewport.** A short offer and installation action sit beside the tabbed panel capture. The screenshot carries the proof of what the product looks like; no imagined dashboard or railway illustration substitutes for it. Mobile stacks the message and capture while keeping every navigation destination visible.

**Page sequence.** Product and selected screens; compatibility; everyday hosting tasks; a navy section explaining control and verifiable changes; installation and release information; concise native FAQ disclosures; grouped support and technical links.

**Technical route.** A concise introduction qualifies dated measurements by their stated conditions, followed by the operation model, interlocking information and test evidence. These facts retain their source links and must not become universal uptime or performance guarantees. Homepage-to-technical and technical-to-install links keep the evaluation path connected.

## Interaction and responsive contract

- Three labelled tabs use selection state and keyboard navigation, and update the image, descriptive copy and accessible label in both languages.
- The "Enlarge view" action opens the current capture in a native dialog with a visible close action. At mobile widths the large image can scroll so fine UI detail remains readable.
- At 900px the navigation wraps into a full visible row; at 640px its four destinations form two columns. No destination depends on discovering a hidden horizontal scroller.
- Installation keeps the copy action visible while offering command inspection and pinned-release details through disclosures. Release links wait for valid manifest data.
- The screenshot caption and enlarged view identify sample data. Asset provenance belongs with the shipping captures; review images are verification artifacts, not product evidence.

## Durable constraints

Navy and white carry the page. Yellow, green and red mean state; yellow and red never share an edge. Navigation and installation use familiar patterns. The mark stays monochrome. No fabricated testimonials, customer logos or benchmark claims. Turkish and English stay parallel. Public assets and fonts are served locally under the site's CSP.

The old signal-box hero, its route-setting entrance animation, the former long homepage and hidden mobile navigation overflow are superseded for this surface. The technical page may still explain the interlocking metaphor where it clarifies the actual operation model. This page choice does not redesign the application's operating layout.

## Implementation and verification references

- `download-portal/index.html`, `technical.html`, `assets/site.css` and `assets/site.js` contain the accepted implementation; the later homepage rules in `site.css` take precedence over retained base rules.
- `download-portal/assets/favicon-v2.svg` and `web/src/components/BrandMark.tsx` carry the shared mark.
- `.impeccable/review/` holds the first review capture set; `.impeccable/review-final/` holds the final capture and interaction-check evidence. These references identify review artifacts without treating screenshot existence as an accessibility certification.
- `DESIGN.md` records shared normative tokens; `.impeccable/design.json` extends them with renderable primitives and responsive/motion metadata.
