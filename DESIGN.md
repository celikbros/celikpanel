---
name: CelikPanel
description: Navy and white, a monochrome angular mark, Overpass typography and clearly inspectable product interfaces.
colors:
  paper: "#ffffff"
  paper-2: "#e9eff8"
  paper-3: "#dbe4f1"
  paper-lit: "#ffffff"
  rule: "#b4c2d8"
  rule-strong: "#7d8ea9"
  ink: "#0e1f3d"
  ink-2: "#2f4269"
  ink-3: "#55668a"
  board: "#0d2247"
  board-2: "#17356a"
  track: "#eef3fb"
  track-dim: "#6787bb"
  track-soft: "#b9cbe6"
  lamp-red: "#e04a3f"
  lamp-red-soft: "#ff9d94"
  lamp-amber: "#f5b301"
  lamp-green: "#35b877"
  lamp-off: "#22406f"
  red-ink: "#b3261e"
  amber-ink: "#8a6100"
  green-ink: "#1c7a45"
  lever: "#123a72"
  lever-fg: "#ffffff"
  lever-hover: "#0d2c58"
  panel-bg: "#f3f6fb"
  panel-surface-2: "#edf2f9"
  panel-surface-3: "#dfe7f2"
  panel-surface-subtle: "#f8fafd"
  panel-border: "#ccd6e6"
  panel-fg-subtle: "#4f5e7c"
  panel-scrim: "#081428"
  panel-warning-fg: "#140e00"
  panel-sidebar-heading: "#91aacd"
  panel-sidebar-hover: "#3064b2"
  panel-sidebar-border: "#4e74af"
  panel-dark-bg: "#060f1f"
  panel-dark-scrim: "#030a16"
  panel-dark-surface: "#142c54"
  panel-dark-surface-2: "#1e4078"
  panel-dark-surface-3: "#2a5391"
  panel-dark-surface-subtle: "#1a3764"
  panel-dark-border: "#5a87cd"
  panel-dark-border-strong: "#7d9fd6"
  panel-dark-fg-subtle: "#96afd2"
  panel-dark-primary: "#d2e2ff"
  panel-dark-primary-hover: "#ebf3ff"
  panel-dark-danger: "#f59188"
typography:
  display:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "clamp(2.65rem, 4.05vw, 4rem)"
    fontWeight: 750
    lineHeight: 1.07
    letterSpacing: "-0.035em"
  headline:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "clamp(1.9rem, 2.4vw, 2.75rem)"
    fontWeight: 700
    lineHeight: 1.16
    letterSpacing: "-0.025em"
  title:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "1.1875rem"
    fontWeight: 700
    lineHeight: 1.3
    letterSpacing: "-0.005em"
  lede:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "1.1875rem"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "normal"
  body:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "1.0625rem"
    fontWeight: 400
    lineHeight: 1.6
    letterSpacing: "normal"
    fontFeature: "'kern' 1, 'liga' 1"
  small:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "0.9375rem"
    fontWeight: 400
    lineHeight: 1.5
    letterSpacing: "normal"
  label:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "0.75rem"
    fontWeight: 700
    lineHeight: 1.3
    letterSpacing: "0.08em"
  label-mono:
    fontFamily: "'Overpass Mono', 'SFMono-Regular', Consolas, 'Liberation Mono', monospace"
    fontSize: "0.75rem"
    fontWeight: 400
    lineHeight: 1.3
    letterSpacing: "0.06em"
  code:
    fontFamily: "'Overpass Mono', 'SFMono-Regular', Consolas, 'Liberation Mono', monospace"
    fontSize: "0.8125rem"
    fontWeight: 400
    lineHeight: 1.55
    letterSpacing: "normal"
  figure:
    fontFamily: "'Overpass Mono', 'SFMono-Regular', Consolas, 'Liberation Mono', monospace"
    fontSize: "1.0625rem"
    fontWeight: 700
    lineHeight: 1.4
    fontVariation: "tabular-nums"
rounded:
  mark: "3px"
  sm: "4px"
  md: "6px"
  panel-md: "0.25rem"
  panel-lg: "0.375rem"
  panel-2xl: "0.5rem"
spacing:
  hair: "0.5rem"
  tight: "0.75rem"
  base: "1rem"
  snug: "1.25rem"
  gap: "1.5rem"
  block: "2.5rem"
  band: "3.5rem"
  section-y: "clamp(3rem, 6vw, 5.5rem)"
  gutter: "1.25rem"
  measure: "66ch"
  container: "1180px"
components:
  button-primary:
    backgroundColor: "{colors.lever}"
    textColor: "{colors.lever-fg}"
    rounded: "{rounded.sm}"
    padding: "0.7rem 1.15rem"
    typography: "{typography.small}"
  button-primary-hover:
    backgroundColor: "{colors.lever-hover}"
    textColor: "{colors.lever-fg}"
  button-primary-on-board:
    backgroundColor: "{colors.track}"
    textColor: "{colors.board}"
    rounded: "{rounded.sm}"
    padding: "0.9rem 1.4rem"
  button-quiet:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    rounded: "{rounded.sm}"
    padding: "0.7rem 1.15rem"
  button-quiet-hover:
    backgroundColor: "{colors.paper-2}"
    textColor: "{colors.ink}"
  button-large:
    padding: "0.9rem 1.4rem"
    typography: "{typography.body}"
  language-switch-option:
    backgroundColor: "transparent"
    textColor: "{colors.ink-2}"
    padding: "0.5rem"
    size: "44px"
  language-switch-option-selected:
    backgroundColor: "{colors.paper-2}"
    textColor: "{colors.ink}"
  command-card:
    backgroundColor: "{colors.board}"
    textColor: "{colors.track}"
    rounded: "{rounded.md}"
    padding: "1rem"
    typography: "{typography.code}"
  copy-button:
    backgroundColor: "transparent"
    textColor: "{colors.track}"
    rounded: "{rounded.sm}"
    padding: "0.4rem 0.8rem"
    height: "44px"
  copy-button-copied:
    backgroundColor: "transparent"
    textColor: "{colors.lamp-green}"
  release-panel:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    padding: "1.5rem 0 0"
  table-scroll:
    backgroundColor: "{colors.paper-lit}"
    textColor: "{colors.ink}"
    width: "100%"
  compare-cell-ours:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    padding: "0.9rem 1rem"
  interlock-panel:
    backgroundColor: "{colors.board}"
    textColor: "{colors.track}"
    rounded: "{rounded.md}"
    padding: "1.5rem"
  mark-lock:
    backgroundColor: "{colors.red-ink}"
    rounded: "{rounded.mark}"
    size: "18px"
  mark-lock-on-board:
    backgroundColor: "{colors.lamp-red}"
    rounded: "{rounded.mark}"
    size: "18px"
  mark-free:
    backgroundColor: "transparent"
    rounded: "{rounded.mark}"
    size: "18px"
  usage-bar-track:
    backgroundColor: "{colors.panel-surface-2}"
    rounded: "{rounded.panel-md}"
    height: "0.5rem"
  usage-bar-fill:
    backgroundColor: "{colors.panel-fg-subtle}"
    rounded: "{rounded.panel-md}"
    height: "0.5rem"
  usage-bar-fill-alarm:
    backgroundColor: "{colors.red-ink}"
    rounded: "{rounded.panel-md}"
    height: "0.5rem"
  audit-field-pass:
    backgroundColor: "{colors.panel-surface-2}"
    textColor: "{colors.green-ink}"
    rounded: "{rounded.panel-lg}"
    padding: "0.75rem"
  audit-field-warning:
    backgroundColor: "{colors.panel-surface-2}"
    textColor: "{colors.amber-ink}"
    rounded: "{rounded.panel-lg}"
    padding: "0.75rem"
  audit-field-fail:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.red-ink}"
    rounded: "{rounded.panel-lg}"
    padding: "0.75rem"
  attention-row-failure:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    padding: "0.75rem 1rem"
  attention-row-warning:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    padding: "0.75rem 1rem"
  sidebar-item:
    backgroundColor: "transparent"
    textColor: "{colors.track-soft}"
    rounded: "{rounded.panel-lg}"
  sidebar-item-active:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.board}"
    rounded: "{rounded.panel-lg}"
  product-frame:
    backgroundColor: "{colors.paper-2}"
    rounded: "{rounded.md}"
    width: "100%"
  product-tab:
    backgroundColor: "transparent"
    textColor: "{colors.ink-3}"
    padding: "0 0.1rem 0.45rem"
    height: "48px"
  product-tab-selected:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
  panel-input:
    backgroundColor: "{colors.paper}"
    textColor: "{colors.ink}"
    rounded: "{rounded.panel-lg}"
    padding: "0.5rem 0.75rem"
  brand-mark:
    textColor: "{colors.ink}"
    size: "36px"
---

# Design System: CelikPanel

## Overview

**Creative North Star: "The Signal Box"**

The retained identity is navy on white, legible Overpass type, precise rules and colour that communicates state. Its character is calm, direct and technically credible. The metaphor informs the product's careful operation model; it does not prescribe a railway diagram or a particular hero composition on every surface.

The public site now leads with the actual product interface and a stronger monochrome mark. This accepted implementation supersedes the former illustrated route-board homepage. Its page strategy, selected screens and technical-page split are scoped in `.impeccable/surfaces/celikpanel-net.md`. The shared visual system remains applicable to the operating panel without making the public site's spacing or storytelling mandatory there.

**Source authority.** The frontmatter is normative. Public-site primitives are extracted from `download-portal/assets/site.css`; its later homepage rules override earlier base declarations. Panel primitives retain their scoped `panel-` names, with semantic RGB-triplet source tokens in `web/src/index.css` and bindings in `web/tailwind.config.js`. `web/gallery/` is a capture harness, not a shipping product route. The `plesk`, `aapanel` and `cpanel` imitation skins remain outside this identity; their existence and removal are separate product decisions.

**Key Characteristics:**
- Navy and white dominate; signal colours report state.
- A monochrome angular C and panel mark connects the public site, application and favicon.
- Overpass carries human language; Overpass Mono carries literal machine values.
- Ruled rows, clear hierarchy and modest corners keep dense information legible.
- Product screenshots are actual interface captures, with sample data explicitly identified.
- Layout and disclosure adapt to the task and viewport; navigation stays visible.

## Colors

White grounds, cool navy-tinted surfaces and navy text establish the identity. Existing primitive names remain stable even where their original metaphor is no longer the page composition.

### Primary

**Lever Navy** fills primary actions; **Lever Navy, Pressed** is their hover colour. White is the action face. The language selector now uses a pale neutral selected ground rather than the primary fill. The brand mark uses the current text colour, allowing navy on white and a light mark in the panel rail.

### Secondary

**Signal Green / Clear**, **Signal Yellow / Attention** and **Signal Red / Stop** are status marks and dark-ground accents for verified states. Soft red supports readable error text on navy. These colours are not brand accents or feature-category colours.

### Tertiary

**Green Ink**, **Amber Ink** and **Red Ink** are the readable status-text variants on light grounds. The panel's warning face supplies dark text when a true-yellow field carries words.

### Neutral

**Paper** and **Lit Paper** are white; the second and third paper tones separate bands and nested surfaces. **Ink**, **Ink, Second** and **Ink, Third** express heading/body, supporting text and metadata. **Hairline** and **Hairline, Strong** divide and enclose content. **Board Navy**, **Board Seam**, **Track**, **Track Soft** and **Track Dim** serve dark sections, commands and technical tables.

The `panel-` primitives apply only to the application: tinted page ground, white surfaces, denser borders, navy sidebar and the corresponding dark-theme surfaces. Keep the application's semantic mappings intact rather than substituting public-page tokens into its component CSS.

**The State Colour Rule.** Yellow, green and red communicate state and must have words, symbols or accessible names alongside them. Never use them for decoration or category identity.

**The No-Adjacency Rule.** Yellow and red never share an edge. Separate them with a gap or rule, rank failures above warnings, or express one state without its colour.

**The Two Yellows Rule.** Use Amber Ink for warning text on white and Signal Yellow for warning marks. A yellow mark is not a substitute for readable text.

## Typography

**Display / Body Font:** Overpass, with Segoe UI, Helvetica Neue, Arial and sans-serif fallbacks.

**Label / Mono Font:** Overpass Mono, with SFMono-Regular, Consolas, Liberation Mono and monospace fallbacks.

Both families are self-hosted with Latin and Latin Extended subsets and `font-display: swap`. Keep Turkish glyph support and the adjacent licence files. Competitor-skin Inter and Open Sans are not part of this identity.

### Hierarchy

The frontmatter display and headline roles describe the current landing-page headings. Its hero uses a heavier, tightly spaced display with a short balanced measure; technical-page headings retain their own responsive scale. Body and small text provide a stable reading rhythm, with muted supporting text under headings. The wordmark uses heavier Celik lettering and a lighter Panel suffix at the same size. Mono belongs to versions, commands, hashes, dates and measured values.

**The Mono Means Measured Rule.** Use mono for literal machine values and code; use sans for explanatory prose. Sample values in product captures remain labelled as sample data.

**The Paired String Rule.** Maintain Turkish and English for visible text and accessible labels. The public site supports explicit `data-en` and label attributes alongside its existing text-node map; dynamic product descriptions and release states use the current language as well.

## Layout

Public pages use a centred container with an observed maximum width of 1180px and 1.25rem side gutters. On the homepage, the hero and benefit sections pair a narrower text column with a wider product or content column. The hero ratio is 0.8 to 1.35 with a responsive gap; the trust and installation sections use their own near-equal two-column grids. Section spacing varies with content rather than following a single fixed section token. Technical evidence retains full-width tables and ruled sequences.

At 1100px the header and hero spacing tighten. At 900px the hero, benefits introduction and FAQ stack; the navigation wraps into a visible full-width row. At 640px navigation becomes two columns with every destination visible, the benefit list becomes one column, and trust, installation and footer groups stack. The small-screen header CTA still hides below 560px; the hero action remains available. Legacy 800px rules continue to serve technical-page grids. These are public-site breakpoints, not new requirements for the application's shell.

Product captures maintain their 1440:950 proportions. The enlarged view uses a native dialog limited to 94vw and 94dvh; below 640px its image remains 1100px wide inside a scrollable area so the actual UI can be inspected.

**The Scrollable Table Rule.** Preserve real table semantics for evidence that needs them. Wider tables use a named, focusable scroll region, visible small-screen scroll hint and sticky first column; the comparison table may become labelled blocks at its existing breakpoint.

## Elevation & Depth

The current homepage is flat. Product frames use a thin border, command surfaces have no shadow, and release details sit below a top rule without an enclosing card. A navy trust band provides tonal separation. The modal uses a navy translucent backdrop. This replaces the former page-wide prescription of exactly one illuminated board and one board shadow.

Technical tables retain scroll-edge shadows to communicate overflow, including the stronger edge on a scrolled sticky column. The legacy board-seat token remains in source for legacy board styling; it is not a default for new homepage components. The panel uses its existing tonal layers and restrained borders.

Buttons retain colour, background and border transitions at 160ms with the source easing curve. Screenshot selection is immediate; the homepage does not require the retired route-setting animation. Reduced-motion preferences retain the complete message without animated reveal.

## Shapes

Controls use modest corners, with slightly softer large frames and the preview dialog. The primitive radius scale is recorded above. Release information and ruled lists remain open on the page. The angular monochrome brand mark is one current-colour SVG path on a 32-unit viewBox; use the shared geometry from `web/src/components/BrandMark.tsx` and `download-portal/assets/favicon-v2.svg`, rather than redrawing it or adding signal lamps.

## Components

### Buttons and links

The primary button uses navy with a white face, a restrained radius and a darker hover fill. Public buttons have a minimum height of 46px. Quiet buttons retain an outline; text links underline their label and enlarge the underline on hover. Focus is a visible two-pixel outline with an offset, inverted to a light outline on dark surfaces. Copy controls have a minimum 44px height and show the result through their label plus a live announcement.

### Navigation and language selection

The header is sticky, opaque white, with a bottom rule and no backdrop blur. Its desktop minimum height is 88px. The monochrome mark and weighted wordmark anchor the left edge. Navigation links remain visible when the header wraps; mobile navigation does not depend on a hidden horizontal scroll area. Language buttons provide 44px targets, `aria-pressed` selection and a pale selected background. The footer now has grouped link columns, which wrap and stack at smaller widths.

### Product tabs and enlarged viewing

The product tabs are text controls with a strong navy underline on the selected tab. Roving tabindex, Left/Right arrows, Home and End expose the three views to keyboard users. The selected screenshot, description, accessible label and enlarged image follow the selected view and language. A bordered frame holds the capture; its caption explicitly distinguishes the real interface from its sample data. A separate labelled control opens the native dialog, with a visible close action and native Escape handling.

### Command and release information

The command is a navy surface with a ruled header, visible copy action and a native disclosure for inspecting the command. A separate disclosure contains the pinned release. Release identity sits below a top divider; commit and hash details are optional disclosure content. Loading, ready and failure remain honest signal states. Download actions are unavailable until the manifest resolves; no success colour should imply an unverified result.

### Technical tables

Operation-model, interlock and dated evidence tables live on the technical page. Retain row/column headers, readable mono figures, conditions and dates. Restrict technical-page table density to that reading task rather than using it as homepage decoration.

### Membership license controls

The account surface in `portal-membership/view.php` retains the navy and white Overpass styling from `download-portal/account/member.css`. License entries are open, ruled rows with identity and status beside the UTC expiry date; action links sit below. At the existing 640px breakpoint the rows stack into one column and form buttons fill the available width. The reviewed Turkish and English views cover desktop (1440px) and mobile (390px).

The summary states that a license lasts one year from creation. Each row offers password-confirmed key viewing with a copy action, or an existing-key save form for older keys that are not yet available to view. The server / IP change form explains that the same key and expiry date are preserved. Key replacement remains a separate action with its own explanation and confirmation. Keep these choices distinct in labels and form headings. Revealed keys appear in a bordered white block with wrapping mono text and a copy-result status announcement.

### Panel components (existing scope)

The application rail stays navy in light and dark themes; its active item is light with navy text. The shared input uses a strong border, surface background, compact padding and a primary-colour focus border.

License access uses the existing navy and white panel controls in a focused page without management navigation. The installer creates the local administrator in the terminal without requiring a license. After authentication, every role waits for a positive server access decision before management pages mount. Missing, expired, invalid or unverifiable licenses show activation and renewal for the server administrator; reseller, customer and additional-user accounts see a message to contact their administrator. Password change and sign-out remain accessible. Pasted keys, show/hide, localized errors and successful activation reuse the License panel. There is no Explore bypass. Authenticated management API requests are denied while locked; existing services and scheduled jobs continue independently. The root update tracker pauses its browser overlay while license access is locked without cancelling the server operation.

UsageBar stays navy until the recorded danger threshold, with no yellow range; print the value beside it. SecurityAuditCard gives a tinted field to failure while pass and warning share neutral surfaces and distinct symbols. Dashboard attention items rank failures above warnings with a divider and gap at the severity boundary. These panel-specific implementations retain the No-Adjacency Rule without imposing the public site's composition on operating screens.

## Do's and Don'ts

### Do:

- **Do** keep navy and white dominant and reserve signal colours for state.
- **Do** reuse the monochrome brand geometry across the site and application.
- **Do** use actual interface captures and label sample data explicitly.
- **Do** preserve visible navigation, keyboard controls, focus indication and bilingual labels.
- **Do** keep source conditions and dates beside technical measurements.
- **Do** pair status colours and usage meters with readable words or numbers.
- **Do** keep panel tokens and competitor-skin exceptions scoped to the application.
- **Do** self-host fonts and keep their licences and Turkish glyph coverage.

### Don't:

- **Don't** put yellow beside red or use signal colours as decoration.
- **Don't** present sample screenshots as live server evidence or measured customer outcomes.
- **Don't** restore the retired route-board hero as a global design requirement.
- **Don't** hide mobile navigation destinations behind an unannounced horizontal overflow.
- **Don't** turn the homepage into the technical evidence tables; use the technical route.
- **Don't** invent testimonials, customers, metrics or a production-readiness claim.
- **Don't** add shadows to the current homepage's command or release surfaces.
- **Don't** import competitor-skin fonts or colours into CelikPanel's own identity.
