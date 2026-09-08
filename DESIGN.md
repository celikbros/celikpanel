---
name: CelikPanel
description: The signal box — navy and white, hairline rules, one dark illuminated board where the machine speaks, and signal colours that mean state and nothing else.
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
    fontSize: "clamp(2.125rem, 1.4rem + 2.6vw, 3.375rem)"
    fontWeight: 700
    lineHeight: 1.06
    letterSpacing: "-0.02em"
  headline:
    fontFamily: "Overpass, 'Segoe UI', 'Helvetica Neue', Arial, sans-serif"
    fontSize: "clamp(1.625rem, 1.2rem + 1.4vw, 2.25rem)"
    fontWeight: 700
    lineHeight: 1.12
    letterSpacing: "-0.015em"
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
    padding: "0.4rem 0.7rem"
  language-switch-option-selected:
    backgroundColor: "{colors.lever}"
    textColor: "{colors.lever-fg}"
  route-board:
    backgroundColor: "{colors.board}"
    textColor: "{colors.track}"
    rounded: "{rounded.md}"
    width: "100%"
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
    padding: "0.35rem 0.75rem"
  copy-button-copied:
    backgroundColor: "transparent"
    textColor: "{colors.lamp-green}"
  release-panel:
    backgroundColor: "{colors.paper-lit}"
    textColor: "{colors.ink}"
    rounded: "{rounded.md}"
    padding: "1.25rem 1.5rem 1.5rem"
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
  support-row:
    backgroundColor: "transparent"
    textColor: "{colors.ink}"
    padding: "1.1rem 0"
  support-row-hover:
    backgroundColor: "{colors.paper-2}"
    textColor: "{colors.ink}"
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
---

# Design System: CelikPanel

## Overview

**Creative North Star: "The Signal Box"**

A railway signal box exists to refuse an unsafe combination. Ask the frame for a route and it physically locks every route that conflicts; you cannot pull the wrong lever. That is not a mood borrowed for atmosphere — it is what this product does. Preview and commit are route setting, the mutation policy is an interlocking table, and a refusal that names the blocker is the product's most convincing moment. The visual world is therefore the signal box's own materials: white paper, navy ink, hairline navy rules doing the work cards would do, and exactly one dark navy object per view where the machine speaks in lamps.

Navy and white carry everything. The signal colours — yellow, green, red — are held in reserve for state, and yellow is never set beside red, because two alarm colours arguing at one edge is how a reader stops trusting either. The register is serious, measured and slightly institutional: a working timetable, not a brochure. Density is moderate — generous type on a calm ground, information carried by ruled rows and real tables rather than by boxes and icons. Nothing is decorative that is not also load-bearing. The one authored moment on the page is the route board setting its routes on load; everything else is still.

Structure is deliberately conventional and expression is subordinate to it: top navigation reading left to right, one primary action where a primary action always is, sections in the order a buyer expects, no interaction anyone has to discover. The world lives in the materials and in the demonstration, never in the wayfinding.

Confirmed anti-references: the gradient hero with three icon cards; the black-on-white monospace "no nonsense" page; brushed metal, industrial plate and rivets (the literal reading of "çelik"); the discarded violet/Open Sans card-grid identity this file once described; and the grey-green rulebook palette (`#eef1ec` paper over `#0b1f17` board) this file described before it — rejected outright by the product owner, who set navy and white as dominant, yellow in some places, a little green and a little red, and yellow never beside red.

**Adoption status.** This system is implemented on both built surfaces and both now carry the navy-and-white palette. `download-portal/` (the public site, celikpanel.net) holds the canonical hex token set in `download-portal/assets/site.css`; the panel application under `web/` holds the same world as semantic RGB-triplet tokens in `web/src/index.css`, bound to Tailwind names in `web/tailwind.config.js`. The panel's tokens are the site's colours under job names: `--fg` is the site's ink, `--primary` is the lever, `--sidebar-bg` is the board, `--success` / `--warning` / `--danger` are the three signal inks, and after dark they become the three lamps. On the application the world adapts to Operate mode: the rail is the one dark object and the current item is lit white rather than coloured, the primary action is the lever in navy, categories are never coloured because colour means state, and in dark mode the whole application moves onto the navy. `web/gallery/` is a design harness for inspecting that skin without a server; it is not a product surface and never ships.

The three imitation skins (`[data-theme='plesk'|'aapanel'|'cpanel']`) are deliberate impressions of other products, kept so an operator arriving from one of them keeps their bearings. They share a classic slate-and-blue base declared once above them in `web/src/index.css`, and they are **outside** this system by decision: their neutrals, their accents (Plesk sky, aaPanel green, cPanel orange) and their Inter and Open Sans faces are not tokens of this design system and must never be borrowed by a product surface. Detector findings against those blocks are sanctioned exceptions, recorded in `.impeccable/config.json`.

**Key Characteristics:**
- Navy and white dominant: white grounds, navy ink, navy hairlines, navy primary action.
- One deep navy board per view (route board, interlocking table, command card, closing band, the panel's rail).
- Signal yellow / green / red reserved for state — and yellow never shares an edge with red.
- Two yellows: a dark amber for text, a true yellow for marks. Never interchanged.
- Hairline rules and real tables instead of cards, boxes and icon grids.
- Overpass and Overpass Mono, self-hosted; mono for anything measured.
- Flat by default: exactly one shadow, and only under the dark objects.
- One authored motion — the routes setting — and it has an end state for reduced motion.

## Colors

A printed working timetable in navy on white: white stock, navy ink, one deep navy board for the diagram, and three signal colours held in reserve and kept apart from each other.

### Primary
- **Lever Navy** (`--lever` #123a72): the primary action, everywhere one exists — the hero button, the closing band, the language switch's selected side, and the panel's `--primary`. It is the only saturated navy that fills a shape.
- **Lever Navy, Pressed** (`--lever-hover` #0d2c58): the hover and active state of the same handle. In the panel's dark theme the primary inverts instead, to a pale navy-white (`--panel-dark-primary` #d2e2ff on a `--board` face).

### Secondary — the signal lamps (dark grounds only)
- **Signal Green / Clear** (`--lamp-green` #35b877): a lamp that reads clear. On the board, on the live dot, as the confirmed state of the copy button, and as the panel's dark-theme `--success`.
- **Signal Yellow / Attention** (`--lamp-amber` #f5b301): waiting, in progress, not yet decided — and the true yellow of every *mark* on either surface, where it is also the panel's `--warning-mark`.
- **Signal Red / Stop** (`--lamp-red` #e04a3f): stopped, locked, refused. Board lamps, the locked route stroke, the lock mark's stroke.
- **Signal Red, Soft** (`--lamp-red-soft` #ff9d94): the same stop as *text* on a `--board` ground, where #e04a3f is too dark to read. The board's refusal sentence uses it; the panel's dark-theme `--danger` (#f59188) is the same idea.

### Tertiary — the signal inks (white grounds only)
- **Red Ink** (`--red-ink` #b3261e): stop, dark enough to be text or a mark on paper (≥ 4.5:1). Every interlocking lock mark, the release panel's error state, and the panel's light `--danger`.
- **Amber Ink** (`--amber-ink` #8a6100): attention as text on paper. The release panel while it is reading the manifest, and the panel's light `--warning`. It is a dark yellow-brown, not a yellow, and that is deliberate: see The Two Yellows Rule.
- **Green Ink** (`--green-ink` #1c7a45): clear as text on paper — the release panel's "ready" line and its dot, and the panel's light `--success`.
- **Warning Face** (`--panel-warning-fg` #140e00): the near-black used *on* a true-yellow mark when a yellow field carries a word. The only ink permitted on `--lamp-amber`.

### Neutral — paper
- **Paper** (`--paper` #ffffff): the page ground. White, unqualified. Everywhere the reader reads.
- **Lit Paper** (`--paper-lit` #ffffff): the ground under scrollable table regions, sticky first columns and the release panel — the paper that must stay legible while a scroll shadow crosses it. It resolves to the same white as `--paper` today; it stays a separate name because its job is different and a future tint belongs there, not on the page ground.
- **Paper, Second Pull** (`--paper-2` #e9eff8): the one tonal band that separates a section (proof, why) and the hover ground for quiet buttons and support rows. The panel's `--surface-2` (#edf2f9) is the same step.
- **Paper, Third Pull** (`--paper-3` #dbe4f1): the deepest paper tone, for a nested band. The panel's `--surface-3` (#dfe7f2) is the same step and carries the selection highlight.
- **Panel Ground** (`--panel-bg` #f3f6fb): the application's page ground behind its white surfaces — the one place the system uses a tinted ground rather than white, so a white card reads as a card without a shadow.
- **Hairline** (`--rule` #b4c2d8): every ordinary divider — rows, sections, table cells, the footer seam. The panel's `--border` (#ccd6e6) is the same hairline one step lighter for denser rows.
- **Hairline, Strong** (`--rule-strong` #7d8ea9): the structural edge — a table's outer border, a quiet button's stroke, the head rule above a table body, the break between severities in the attention list. Shared verbatim with the panel's `--border-strong`.
- **Ink** (`--ink` #0e1f3d): body text, headings, the focus ring, the panel's `--fg`.
- **Ink, Second** (`--ink-2` #2f4269): ledes, supporting paragraphs, nav links at rest, the panel's `--fg-muted`.
- **Ink, Third** (`--ink-3` #55668a): labels, units, timestamps, the quiet metadata layer. The panel's `--fg-subtle` (#4f5e7c) is the same layer, and it is also what fills a meter that has nothing to report.
- **Scrim** (`--panel-scrim` #081428): the dimmed ground behind a modal dialog, at partial alpha. Navy, never black.

### Neutral — the board
- **Board Navy** (`--board` #0d2247): the ground of every illuminated object — the route board, the interlocking table, the command cards, the closing band, the audiences section, and the panel's rail in both light and dark. Also the brand mark's body and the browser selection highlight on the site.
- **Board Seam** (`--board-2` #17356a): the hairline inside a dark object — head/body separators, borders, hover ground for controls on the board. The panel's `--surface-2` after dark (#1e4078) is the same step, and the rail's own hover is a step brighter at `--panel-sidebar-hover` (#3064b2).
- **Track** (`--track` #eef3fb): the light on a dark object — text, set routes, the panel's `--sidebar-fg` and its dark-theme `--fg`.
- **Track Soft** (`--track-soft` #b9cbe6): secondary state text on a dark ground; the panel's `--sidebar-fg-muted` and dark `--fg-muted`.
- **Track Dim** (`--track-dim` #6787bb): unset track lines on the diagram — present but not carrying a route. Adjacent: `--panel-sidebar-heading` (#91aacd) for group headings on the rail and `--panel-sidebar-border` (#4e74af) for its edge.
- **Lamp Off** (`--lamp-off` #22406f): an unlit lamp. The resting state every lamp animates out of.

### Named Rules

**The No-Adjacency Rule.** Yellow and red never share an edge. Not in a meter, not in a legend, not in two rows of the same list, not in adjacent chips. Where both severities exist in one place the surface must either separate them (a rule, a gap, a break in the list), rank them (failures first, warnings after), or drop one of the two colours entirely and say that state in words. This is enforced in three built places: **`UsageBar`** (`web/src/components/ui.tsx`) has no yellow band at all — a meter is `--fg-subtle` navy until it is a problem and `--danger` red past 90%, and the 75–90 range is reported by the number printed beside the bar, not by a colour; **`SecurityAuditCard`** (`web/src/components/SecurityAuditCard.tsx`) gives a coloured *field* only to a failure (`border-danger/30 bg-danger/10`) — pass and warning sit on the same neutral `bg-surface-2` field and speak through their icon, their coloured glyph and their words, so a red field never abuts a yellow one; **the dashboard attention list** (`web/src/components/Dashboard.tsx`) sorts every `danger` item above every warning and, at the single index where the severity changes, breaks the list with `mt-2 border-t-2 border-t-border-strong`. A new surface that wants both colours adopts one of those three devices; it does not invent a fourth.

**The Two Yellows Rule.** The system carries two yellows and they are not interchangeable. `--amber-ink` (#8a6100) is the yellow for **text**: a dark yellow-brown that clears 4.5:1 on white. `--lamp-amber` / `--warning-mark` (#f5b301) is the yellow for **marks**: bars, dots, icon fills, the lamp on the board. The split exists because a true yellow cannot carry text on white at an honest ratio, and darkening the mark to fix that would stop it reading as a signal lamp. Text takes the ink; marks take the lamp; a word set on a yellow mark takes `--panel-warning-fg` (#140e00). After dark, where the ground is navy, `--warning` and `--warning-mark` collapse to the same #f5b301, because a lamp is drawn to be read against a dark ground and that condition is finally met.

**The Signal Reserve Rule.** Yellow, green and red mean attention, clear and stopped. They are never a category colour, never a brand accent, never a button fill, never decoration. If a colour on a surface is not reporting the state of something real, it is not a signal colour. The comparison table is the test case: it compares two ways of working and uses no ticks and no crosses, because a comparison is not a state — the right-hand column is lit in `--paper` and set in `--ink` behind a `--rule-strong` edge, and that is the whole device.

**The Ink-On-Paper Rule.** The bright lamps (`--lamp-*`) exist only on `--board` grounds. Any signal shown on white uses the `-ink` variant, which clears 4.5:1 against `--paper`. A lamp colour on paper is a bug, and paper ink on the board is the same bug facing the other way — which is why the board's refusal sentence uses `--lamp-red-soft` and not `--red-ink`.

**The Never-Colour-Alone Rule.** A signal colour is always accompanied by a word or an accessible name — "kilitli / locked", "açık / clear", the refusal sentence, the audit finding's own text. Hue is a second channel, never the only one. This is also what makes The No-Adjacency Rule survivable: dropping a colour costs nothing when the word was always there.

**The One Dark Object Rule.** A dark surface belongs to the machine: the route board, the interlocking table, a command the visitor will paste, the closing band, the panel's rail. Two dark objects competing in one viewport is a composition error; a dark object used as ornament is a world error. The panel's dark theme is not an exception — there the whole application is the dark ground, and the rail distinguishes itself by staying `--board` while the surfaces around it lift to `--panel-dark-surface` (#142c54).

### Derived values

Every remaining colour literal in the two stylesheets is one of these tokens at an alpha, and nothing else appears:

- `rgba(14, 31, 61, 0.35)` — `--ink` at 35%: the board-seat shadow.
- `rgba(14, 31, 61, 0.45)` / `rgba(14, 31, 61, 0)` — `--ink` at 45% and 0%: the sticky column's scrolled shadow, and the two radial edge fades that mark a scrollable table region.
- `#ffffff` / `rgba(255, 255, 255, 0)` in `.table-scroll` — `--paper` opaque and transparent: the two linear masks that hide those fades when the region is not scrolled. Written as literals because a gradient stop pair must resolve in one declaration.
- `rgba(238, 243, 251, 0.18)` — `--track` at 18%: the ring around an unlit lamp on the board.
- `rgba(238, 243, 251, 0.35)` — `--track` at 35%: the hairline edge of a control sitting on the board (the copy button).
- `rgba(23, 53, 106, 0)` — `--board-2` at 0%: the transparent end of the two masks that hide the interlocking table's edge fades when its region is not scrolled. The board's own answer to the `--paper` masks above.
- `rgba(0, 0, 0, 0.55)` / `rgba(0, 0, 0, 0)` — the edge fades of a scrollable region **on a dark ground**. This is the system's only black, and it is a shade rather than a colour: a navy shade is invisible against `--board-2`, and only something darker than the board can read as an edge. Black is permitted here and nowhere else.
- `rgba(53, 184, 119, 0.18)` — `--lamp-green` at 18%: the halo around the live dot, and the only alpha a signal colour is allowed on the site.
- `color-mix(in srgb, var(--paper) 92%, transparent)` — the sticky header's ground, so the page shows through it under blur.
- In the panel, Tailwind's `/<alpha>` modifiers over the same tokens: `bg-primary/10` for an icon tile, `bg-warning/15` and `bg-warning/10` for a warning field, `bg-danger/10` with `border-danger/30` for a failure field. Alpha is the only way a signal colour becomes a field.

## Typography

**Display / Body Font:** Overpass (fallback: Segoe UI, Helvetica Neue, Arial, sans-serif)
**Label / Mono Font:** Overpass Mono (fallback: SFMono-Regular, Consolas, Liberation Mono, monospace)

**Character:** Overpass descends from the FHWA road-sign alphabet — drawn to be read at speed, in weather, by people who are not looking at typography. It is plain without being neutral: slightly condensed, high-legibility, faintly official. Its mono is the instrument face: anything measured, machine-owned or literal is set in it. Both are self-hosted as woff2 with `unicode-range` splits (latin, latin-ext for ğ ş İ) and `font-display: swap`; licences ship beside the files on both surfaces. Inter and Open Sans exist in `web/src/fonts/` for the three imitation skins only and are not faces of this system.

### Hierarchy
- **Display** (700, `clamp(2.125rem, 1.4rem + 2.6vw, 3.375rem)`, 1.06, -0.02em): the page's one h1. Balanced wrap, capped at 18ch so it always breaks into a shape.
- **Headline** (700, `clamp(1.625rem, 1.2rem + 1.4vw, 2.25rem)`, 1.12, -0.015em): section headings. Capped at 30ch.
- **Title** (700, 1.1875rem, 1.3, -0.005em): step headings, table titles, card heads.
- **Lede** (400, 1.1875rem, 1.55, `--ink-2`): the sentence under a heading. One per section, never two.
- **Body** (400, 1.0625rem, 1.6): running text, capped at `--measure` (66ch).
- **Small** (400, 0.9375rem): table cells, list rows, notes, nav links, secondary descriptions.
- **Label** (700, 0.75rem, 0.08em, uppercase, `--ink-3`): column names in sans — hero facts, release panel fields, ledger head, the per-column labels the comparison table falls back to below 640px.
- **Label, mono** (0.75rem, 0.06em, uppercase, `--ink-3`): machine-ish labels — the demo tag, support row kinds, board state text, the scroll hint.
- **Code** (0.8125rem, 1.55, `--track` on board): install commands, hashes, commits, dates.
- **Figure** (700, mono, `tabular-nums`): every measured value in the ledger; right-aligned, never wrapped.

### Named Rules

**The No Kicker Rule.** Nothing sits above a heading. No eyebrow, no kicker, no small uppercase category line stacked over an h1 or h2. Labels are columns in a table, terms in a definition list, or inline beside a row — never a hat.

**The Mono Means Measured Rule.** Overpass Mono marks what a machine produced or owns: numbers, hashes, versions, commands, state words, step numerals, timestamps. Prose is never mono. A number a human wrote as an estimate is not set in mono, because it is not a measurement.

**The Number Carries The Band Rule.** Where a colour has been withheld to keep yellow away from red, the value must be legible as text. A meter is always accompanied by its number — "18.4 GB / 40 GB", "72%" — set in mono beside or under the bar. The bar is the glance; the number is the answer.

**The Middle Dot Rule.** U+00B7 renders flush against its right neighbour in Overpass (bad right side bearing). Use it only in Overpass Mono, where it is correctly spaced. In the sans face, separate with a comma or a bracket.

**The Paired String Rule.** The site is authored in Turkish and switched in place: every visible string has an entry in the English map in `site.js`, and translation happens on bound text nodes, not by swapping markup. A new string without its pair is an unfinished string. Dynamic text the release reader owns is explicitly excluded from the map and gets its words from the manifest or from `uiText`.

## Layout

A single centred column: `width: min(100% - 2.5rem, 1180px)`, so the gutter is 1.25rem a side and the measure never exceeds 66ch for running text. Vertical rhythm is one value: sections are `clamp(3rem, 6vw, 5.5rem)` tall top and bottom, and are separated by a 1px `--rule` hairline rather than by space alone. Inside a section, the recurring steps are 0.5 / 0.75 / 1 / 1.25 / 1.5rem for local grouping, 2.5rem to open a block, and 3.5rem to start a new subject inside the same section.

Composition is two-column asymmetric where a claim faces its evidence — hero `1fr / 1.1fr`, install and release `0.9fr / 1.1fr`, with a `clamp(2rem, 5vw, 4.5rem)` gutter — and full-width ruled rows everywhere else. Multi-item content is a grid of ruled rows or a real table: three columns for the steps and the audiences, two for the feature list, the FAQ, the manages list and the interlock notes.

Breakpoints are few and each does one job: **900px** collapses every two-column grid to one and wraps the header (the nav becomes a horizontally scrolling row with its scrollbar hidden and a paper fade on its right edge); **800px** collapses the steps, the feature list, the FAQ and the two-column definition lists; **640px** relaxes the sticky column, tightens the support row, reveals the scroll hint, and turns the three-column comparison table into one block per situation with each answer under its own column name; **560px** drops the header CTA and makes hero and closing buttons full-width.

### Named Rules

**The Ruled List Rule.** Grouped content is a ruled list, a definition list or a table. Not cards. A hairline top border, one hairline between rows, and the type does the rest. If a layout needs a box to be legible, the hierarchy is wrong.

**The Scrollable Table Rule.** A table wider than its column stays a table. It goes inside `.table-scroll`: focusable (`tabindex="0"`), named (`role="region"` with `aria-labelledby`), painted with left/right scroll shadows, snapping whole columns (`scroll-snap-type: x proximity`), and — below 640px — announced by a visible sentence, "Tablo yana kayar." / "The table scrolls sideways." The first column is sticky, and the table uses `border-collapse: separate` because sticky cells bleed through collapsed borders. A `.is-scrolled` class, set from the region's `scrollLeft`, adds a drop shadow to the pinned column so the reader can see they have moved. A table that stops being a table below 640px (the comparison) drops the region's border and hint with it, rather than leaving an empty frame.

## Elevation & Depth

The system is flat. Depth is carried by tone and by hairlines: white against the tinted panel ground against the navy board, separated by 1px `--rule` or `--rule-strong`. There is exactly one shadow token in the stylesheet and it exists to seat the dark objects on the paper, not to lift the interface.

### Shadow Vocabulary
- **Board seat** (`box-shadow: 0 18px 40px -22px rgba(14, 31, 61, 0.35)`): a wide, low-opacity, downward-cast navy shadow under a `--board` object — the route board and the command cards. Nothing else uses it.
- **Sticky column edge** (`1px 0 0 var(--rule-strong)`, gaining `8px 0 12px -6px rgba(14, 31, 61, 0.45)` when `.is-scrolled`): not elevation but a scroll signal, painted on the one edge where a left-edge cue can still appear. On a board ground the same edge is `1px 0 0 var(--track-dim)` and its region's fades are black at 55%, because navy on navy shows nothing.

### Named Rules

**The One Shadow Rule.** Surfaces are flat. The only shadow is the board seat, and only a `--board` object may cast it. Paper panels (the release panel, table regions, the panel application's cards) get a `--rule-strong` or `--border` stroke instead — never a shadow, never both.

## Shapes

Corners are almost square: 4px (`--radius`) on anything you click on the site — buttons, the language switch, the copy button, the skip link, the pinned-command disclosure — and 6px on the large dark or panelled objects (route board, command card, release panel), so the big surfaces read as slightly softer plates than the controls on them. The panel runs the same idea on its own scale: `--radius-md` 0.25rem for controls, `--radius-lg` and `--radius-xl` 0.375rem for cards and fields, `--radius-2xl` 0.5rem for the largest containers. Marks in the interlocking table are 18px squares with a 3px corner; the focus ring's own radius follows the control it surrounds.

Borders carry the form language, not fills: 1.5px on buttons (so a quiet button and a primary button occupy identical space), 1px `--rule` between rows and cells, 1px `--rule-strong` around a table region or a paper panel, 2px `--rule-strong` where a list breaks between severities, 1px `--board-2` inside a dark object. Circles are reserved for lamps — the board's 9px signal lamps, the 7–8px live and status dots, the brand mark's two lamps — and are the only fully round shapes in the system.

The build carries one fully-rounded pill (999px) on the demonstration tag beside the interlocking table, and the panel uses `rounded-full` for its count badge and its status dots. Treat the pill as a shape for a count or a lamp only; do not promote it to a general container.

## Components

### Buttons
- **Character:** a button is a lever — a plain navy handle, no gradient, no glow, no icon.
- **Shape:** nearly square (4px), 1.5px border always present so variants share a footprint.
- **Primary ("lever"):** `--lever` ground, `--lever-fg` white type, 700 weight, 0.7rem 1.15rem; `--lever-hover` on hover. `.button-large` (0.9rem 1.4rem, 1.0625rem) is the hero and closing-band size.
- **Quiet:** transparent ground, `--ink` type, `--rule-strong` stroke; `--paper-2` ground on hover.
- **On a board ground:** the primary inverts — `--track` ground, `--board` type, going to pure white on hover. This is the only inversion in the system.
- **Hover / focus:** 160ms `--ease-out` (`cubic-bezier(0.16, 1, 0.3, 1)`) on background, border and colour only. Never transform, never scale. Focus is the global ring: `2px solid var(--ink)` (`var(--fg)` in the panel), 3px offset.
- **Disabled:** `aria-disabled="true"` → 0.55 opacity, pointer events off. Links that are not yet real (before the release manifest loads) also drop out of the tab order and gain their href when the data arrives.

### Route Board (signature)
The server drawn the way a signal box draws its track: a `--board` figure with a mono head (name, live dot), an SVG diagram, and a refusal sentence below a `--board-2` seam. Track lines are 2px `--track-dim`; a set route is 4px `--track`; a refused route is 4px `--lamp-red` dashed `6 8`. Lamps are 9px circles ringed in `--track` at 18%, `--lamp-off` at rest, taking `.is-clear` / `.is-caution` / `.is-stop`. A lock mark sits on the junction that blocks the route: a `--board-2` square stroked in `--lamp-red` with a `--track` shackle. Route labels are sans 15px/600 in `--track`; state words are mono 12px uppercase in `--track-soft`. The refusal paragraph is the point of the component: it names what is locked and why, in the product's own words, with the blocker in `--lamp-red-soft`.

**The One Authored Moment Rule.** The page has exactly one animation, and it is here: on load the routes set — lamps run off → yellow → green (`lamp-set`, 1.1s, staggered 220ms / 420ms), the set route draws in (`route-draw`, 900ms via `stroke-dashoffset`), the stop lamp lights at 900ms (`lamp-stop`), and the lock mark lands at 1250ms (`lock-land`, scale 1.8 → 0.92 → 1). The yellow appears only as a passing frame inside `lamp-set`, and never at rest beside the red lamp — the sequence obeys The No-Adjacency Rule in time as well as in space, because the stop lamp does not light until the clearing lamps have finished being yellow. All four keyframes live inside `@media (prefers-reduced-motion: no-preference)`; with reduced motion the board renders its end state immediately, fully readable. Any future motion must pass the same test: it must have an end state that is the whole message.

### Tables
- **Interlocking table:** rows are requests, columns are conditions, cells are marks — and the whole thing is a dark object, a `--board` panel with a 6px radius and 1.5rem of padding, because the interlocking is the machine talking. The table itself sits on `--board-2` inside its scroll region: `border-collapse: separate`, `min-width: 720px`, cells 0.75rem/0.85rem ruled in `--track-dim`, each cell a scroll-snap point so a sideways drag lands on a whole column. Head cells are 0.8125rem/700 `--track-soft`, bottom-aligned so long column names stack cleanly. Row headers are 600 `--track`, `white-space: nowrap`, sticky at `left: 0` on `--board-2` with a `--track-dim` edge. A locked cell carries an 18px `--lamp-red` square — the lamp, not the ink, because the ground is the board; a free cell carries an 18px square outlined in `--track-dim` with no fill. Both marks have accessible names ("kilitli"/"locked", "serbest"/"free") that switch with the language. On paper the same marks use `--red-ink` and `--rule-strong`.
- **Ledger table:** the evidence table. `border-collapse: collapse`, `min-width: 760px`, uppercase mono-scale head in `--ink-3` over a `--rule-strong` rule. The value column is mono, 700, 1.0625rem, right-aligned, `nowrap`; the date column is mono 0.8125rem `--ink-3`.
- **Comparison table:** three columns — the situation, what usually happens, what happens here. No ticks, no crosses and no colour: the middle column recedes to `--ink-3` and the right-hand column is lit on `--paper` in `--ink` behind a 2px `--rule-strong` left edge. Below 640px it stops being a table and becomes one block per situation.
- All three live inside `.table-scroll` (see The Scrollable Table Rule) and are followed by their `.scroll-hint`.

### Command Card
A `--board` article with a 6px radius, a head row (title + copy button) over a `--board-2` seam, and a focusable `<pre>` of mono 0.8125rem `--track`. The copy button is a hairline outline in `--track` at 35% on transparent, filling to `--board-2` on hover; on success it takes `.copied` and turns its border and text `--lamp-green` for 1800ms while an `aria-live` region announces the result in the current language. Failure swaps the label to "Select text" rather than pretending it worked. A scroll hint above the code is set in the sans face on the board's own ground, so it never reads as a comment line of the command.

### Release Panel
A `--paper-lit` panel with a `--rule-strong` border, 6px radius: an uppercase sans label, a mono 1.5rem version number, a status line with a dot, a ruled definition list (`9rem` term column) for date, commit and SHA-256, and two buttons. Its whole state is expressed through the signal inks on the one status dot and its text: `aria-busy="true"` → `--amber-ink` text with a `--lamp-amber` dot; loaded → `--green-ink`; `.release-error` → `--red-ink`, with the failure message in the date field. Only one of those three states is ever on screen, which is why the panel can use yellow and red at all. The panel never shows a green it has not verified.

### Support Row
A full-width link laid out as three columns (`6.5rem` mono kind label / text / affordance), ruled top and bottom, hovering to `--paper-2` with the affordance sliding 4px right. Below 640px the label column narrows to 4rem and the affordance is hidden.

### Navigation
Sticky header on a 92% `--paper` ground (`color-mix`) with a `--rule` bottom edge and a slight backdrop blur; 4rem minimum height. Links are 0.9375rem/600 `--ink-2` with a transparent 2px bottom border that becomes `--rule-strong` on hover. The language switch is a bordered two-button group sharing one 4px radius, the selected side taking the `--lever` fill and driven by `aria-pressed`. The header CTA is the same lever button at nav scale. Below 900px the nav drops to its own full-width row and scrolls horizontally behind a paper fade; below 560px the CTA is hidden because the hero's primary action is one screen away.

### Panel Rail
The application's one dark object: a `--sidebar-bg` (#0d2247) column in both light and dark, group headings in `--panel-sidebar-heading`, items in `--track-soft`, hover to `--panel-sidebar-hover`, and the current item **lit** — a white ground with `--board` type — rather than coloured. Colour on the rail would compete with the signal colours in the content, so the rail has none.

### Meter (UsageBar)
The system's canonical demonstration of The No-Adjacency Rule. A 0.5rem `--surface-2` track with a `--fg-subtle` navy fill and a 0.25rem radius; past 90% the fill becomes `--danger`. There is no yellow band and there must not be one: a meter is navy until it is a problem, then red. Everything between 75 and 90 is reported by the number set beside the bar (see The Number Carries The Band Rule). Every call site pairs the bar with that number — "used / total", "n / limit MB".

### Audit Field (SecurityAuditCard)
A row per check, each a bordered field. Only a **failure** gets a coloured field: `border-danger/30 bg-danger/10` with `--danger` type. Pass and warning share the neutral `bg-surface-2` field on a `--border-strong` stroke and are told apart by their icon and their words — a green check for pass, a yellow triangle for warning — so a red field is never edge to edge with a yellow one. An unknown result gets the quietest field of all — `--panel-surface-subtle` (#f8fafd) under `--fg-muted` type — because "we could not read this" is not a state to colour.

### Attention List
The dashboard's problem list, and the third enforcement. Items are sorted so every failure precedes every warning; each row carries its severity in the icon colour only (`--danger` or `--warning`), never as a field; and at the one index where the severity changes the list breaks with a 2px `--border-strong` rule and a 0.5rem gap. The count badge above it is a single warning-tinted pill, because a count is one state, not two.

### Footer
No dark band and no columns of links: brand mark, name, one sentence, an inline link row, and a mono bottom line separated by a `--rule` hairline. The closing call-to-action band above it is the last `--board` object on the page.

## Do's and Don'ts

### Do:
- **Do** keep navy and white dominant. Colour a thing only when the thing has a state.
- **Do** separate, rank, or drop — the three sanctioned answers when yellow and red would meet. Separate them with a rule and a gap, rank failures above warnings, or remove one colour and say that state in words.
- **Do** use `--amber-ink` (#8a6100) for yellow *text* and `--warning-mark` / `--lamp-amber` (#f5b301) for yellow *marks*, and never the other way round.
- **Do** use the `-ink` signal variants (`--red-ink`, `--amber-ink`, `--green-ink`) for any state shown on white, and the `--lamp-*` variants only on `--board`.
- **Do** print the number beside any meter whose colour has been withheld.
- **Do** put every measured number on paper in mono with tabular figures, beside the condition it was measured under and the date it was measured (the ledger's four columns: measure, value, condition, date).
- **Do** reach for a ruled list, a definition list or a real table before any container — the manages list, the feature list, the FAQ and the support rows are all the same device at different densities.
- **Do** pair every signal colour with a word or an accessible name.
- **Do** wrap any table that can overflow in `.table-scroll` — focusable, named, scroll-shadowed, column-snapped, sticky first column, `border-collapse: separate`, and the visible "Tablo yana kayar." / "The table scrolls sideways." hint below 640px.
- **Do** self-host fonts under `/assets/fonts/` (site) and `src/fonts/` (panel) with `unicode-range` splits and ship their licences beside them; the site's CSP is `default-src 'none'` with `font-src 'self'`, and the panel must render fully offline.
- **Do** keep structure conventional: top nav left to right, one primary action in the ordinary place, sections in the order a buyer expects. Ease of use outranks expression.
- **Do** give every new visible string its English pair in `site.js`, and let the text-node walker do the switching.
- **Do** give any new motion a reduced-motion end state that carries the whole message on its own.

### Don't:
- **Don't** place yellow beside red — not in a meter's bands, not in adjacent list rows, not in two chips in a row, not in a legend. This is the owner's rule and it has no exceptions.
- **Don't** set text in `--lamp-amber` (#f5b301) on a white ground; it cannot clear 4.5:1. Use `--amber-ink`.
- **Don't** darken `--warning-mark` to make it "safe for text" — that is what `--amber-ink` is for, and a darkened mark stops reading as a lamp.
- **Don't** use yellow, green or red as a category colour, a brand accent, a button fill or decoration. They report state or they do not appear.
- **Don't** put a lamp colour on paper, or paper ink on the board.
- **Don't** reintroduce the grey-green palette, or any green as a ground, a neutral or a page tint. Green is a lamp; navy is the world.
- **Don't** borrow anything from the three imitation skins — their neutrals, their accents, Inter or Open Sans. They exist to look like other products and are outside this system.
- **Don't** stack a kicker, eyebrow or small uppercase category line above a heading. Labels are columns or inline terms.
- **Don't** build an icon-card grid, a feature tile wall, a fake dashboard screenshot, or a gradient hero. Those are the confirmed rut.
- **Don't** invent a metric, a logo, a testimonial or a customer count. Every number on a surface is measured and dated, or it is cut.
- **Don't** use the middle dot (·) in the sans face — it renders flush in Overpass. Use a comma or brackets; keep the dot for Overpass Mono.
- **Don't** add a second shadow, or put the board seat shadow under a paper surface. Panels get a `--rule-strong` border.
- **Don't** place two dark objects in the same viewport, or use a dark ground for anything the machine does not own.
- **Don't** add inline `style` attributes or inline `<script>` on the public site; the CSP forbids them, and no asset may come from a CDN.
- **Don't** animate transform or scale on interactive elements. Transitions are 160ms `--ease-out` on colour, background and border only.
- **Don't** show a success state the system has not verified — an unproven step reports as unfinished, not as green.
</content>
