---
name: CelikPanel
description: The signal box — printed rulebook stock, ink, hairline rules, and one dark illuminated route board where signal colours mean state and nothing else.
colors:
  paper: "#eef1ec"
  paper-2: "#e3e8e1"
  paper-3: "#d6ddd4"
  paper-lit: "#f6f8f4"
  rule: "#b7bfb5"
  rule-strong: "#8e978c"
  ink: "#10160f"
  ink-2: "#3a423a"
  ink-3: "#5a655a"
  board: "#0b1f17"
  board-2: "#12291f"
  track: "#e8ece7"
  track-dim: "#4f6a5c"
  track-soft: "#a3b8ad"
  lamp-red: "#d0102f"
  lamp-amber: "#f0a81a"
  lamp-green: "#34c273"
  lamp-off: "#2a3a31"
  red-ink: "#a20d26"
  amber-ink: "#7a5a08"
  green-ink: "#1e6f45"
  lever: "#10160f"
  lever-fg: "#f4f6f2"
  lever-hover: "#22302a"
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
  mark-lock:
    backgroundColor: "{colors.red-ink}"
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
---

# Design System: CelikPanel

## Overview

**Creative North Star: "The Signal Box"**

A railway signal box exists to refuse an unsafe combination. Ask the frame for a route and it physically locks every route that conflicts; you cannot pull the wrong lever. That is not a mood borrowed for atmosphere — it is what this product does. Preview and commit are route setting, the mutation policy is an interlocking table, and a refusal that names the blocker is the product's most convincing moment. The visual world is therefore the signal box's own materials: grey-green printed rulebook stock, dark green-black ink, hairline rules doing the work cards would do, and exactly one dark illuminated object per view where the machine speaks in lamps.

The register is serious, measured and slightly institutional — a working timetable, not a brochure. Density is moderate: generous type on a calm ground, information carried by ruled rows and real tables rather than by boxes and icons. Nothing is decorative that is not also load-bearing. The one authored moment on the page is the route board setting its routes on load; everything else is still.

Structure is deliberately conventional and expression is subordinate to it: top navigation reading left to right, one primary action where a primary action always is, sections in the order a buyer expects, no interaction anyone has to discover. The world lives in the materials and in the demonstration, never in the wayfinding.

Confirmed anti-references: the gradient hero with three icon cards; the black-on-white monospace "no nonsense" page; brushed metal, industrial plate and rivets (the literal reading of "çelik"); and the discarded violet/Open Sans card-grid identity this file previously described.

**Adoption status.** This system is implemented on both built surfaces: `download-portal/` (the public site, celikpanel.net) and the panel application under `web/`, whose default skin carries it through the token set in `web/src/index.css`. On the application the world adapts to Operate mode: the rail is the one dark object and the current item is lit rather than coloured, the primary action is the lever in ink, categories are never coloured because colour means state, and the three signal colours appear as inks on paper and as lamps on the dark theme. `web/gallery/` is a design harness for inspecting that skin without a server; it is not a product surface and never ships.

The three imitation skins (`[data-theme='plesk'|'aapanel'|'cpanel']`) are deliberate impressions of other products, kept so an operator arriving from one of them keeps their bearings. They inherit a shared classic base in the same file and are **outside** this system: detector findings about their Inter and Open Sans faces are sanctioned exceptions, recorded in `.impeccable/config.json`.

**Key Characteristics:**
- Grey-green rulebook paper and green-black ink; no pure white, no pure black.
- Signal red / amber / green reserved for state, on lamps, marks and status text only.
- One dark illuminated object per view (route board, command card, closing band).
- Hairline rules and real tables instead of cards, boxes and icon grids.
- Overpass and Overpass Mono, self-hosted; mono for anything measured.
- Flat by default: exactly one shadow, and only under the dark objects.
- One authored motion — the routes setting — and it has an end state for reduced motion.

## Colors

A printed working timetable: a grey-green stock, ink with a green cast, one dark board for the diagram, and three signal colours held in reserve.

### Primary
- **Signal Green / Clear** (`--lamp-green` #34c273): a lamp that reads clear. Only on the dark board, on the live dot, and as the confirmed state of the copy button.
- **Signal Amber / Caution** (`--lamp-amber` #f0a81a): waiting, in progress, not yet decided. On the board's lamps and on the release panel's busy dot.
- **Signal Red / Stop** (`--lamp-red` #d0102f): stopped, locked, refused. On the board's lamps, the locked route stroke and the lock mark.

### Secondary
- **Red Ink** (`--red-ink` #a20d26): the same stop, dark enough to be text or a mark on paper (≥ 4.5:1). Every interlocking lock mark and the release panel's error state.
- **Amber Ink** (`--amber-ink` #7a5a08): caution as text on paper — the release panel while it is reading the manifest.
- **Green Ink** (`--green-ink` #1e6f45): clear as text on paper — the release panel's "ready" line and its dot.

### Tertiary
- **Board Green-Black** (`--board` #0b1f17): the ground of every illuminated object — the route board, the command cards, the closing band. Also the brand mark's body and the browser selection highlight.
- **Board Seam** (`--board-2` #12291f): the hairline inside a dark object — head/body separators, borders, hover ground for controls on the board.
- **Track** (`--track` #e8ece7) / **Track Dim** (`--track-dim` #4f6a5c) / **Track Soft** (`--track-soft` #a3b8ad): the three levels of light on a dark object — text and set routes, unset track lines, secondary state text.
- **Lamp Off** (`--lamp-off` #2a3a31): an unlit lamp. The resting state every lamp animates out of.

### Neutral
- **Rulebook Paper** (`--paper` #eef1ec): the page ground. Everywhere the reader reads.
- **Stock, Second Pull** (`--paper-2` #e3e8e1): the one tonal band that separates a section (the proof section) and the hover ground for quiet buttons and support rows.
- **Stock, Third Pull** (`--paper-3` #d6ddd4): the deepest paper tone; available for a nested band, currently held in reserve.
- **Lit Paper** (`--paper-lit` #f6f8f4): the slightly brighter stock under scrollable table regions, sticky first columns and the release panel — the paper that must stay legible while a shadow crosses it. *Build note: this value is currently a literal repeated in four rules in `site.css` rather than a `:root` custom property; lift it to one when the file is next touched.*
- **Hairline** (`--rule` #b7bfb5): every ordinary divider — rows, sections, table cells, the footer seam.
- **Hairline, Strong** (`--rule-strong` #8e978c): the structural edge — a table's outer border, a quiet button's stroke, the head rule above a table body.
- **Ink** (`--ink` #10160f): body text, headings, the focus ring.
- **Ink, Second** (`--ink-2` #3a423a): ledes, supporting paragraphs, nav links at rest.
- **Ink, Third** (`--ink-3` #5a655a): labels, units, timestamps, the quiet metadata layer.
- **Lever** (`--lever` #10160f) / **Lever Face** (`--lever-fg` #f4f6f2) / **Lever Hover** (`--lever-hover` #22302a): the primary button's dark handle, its type, and its hover.

### Named Rules

**The Signal Reserve Rule.** Red, amber and green mean stopped, waiting and clear. They are never a category colour, never a brand accent, never a button fill, never decoration. If a colour on a surface is not reporting the state of something real, it is not a signal colour.

**The Ink-On-Paper Rule.** The bright lamps (`--lamp-*`) exist only on `--board` grounds. Any signal shown on paper uses the `-ink` variant, which clears 4.5:1 against `--paper` and `--paper-lit`. A lamp colour on paper is a bug.

**The Never-Colour-Alone Rule.** A signal colour is always accompanied by a word or an accessible name — "kilitli / locked", "açık / clear", the refusal sentence. Hue is a second channel, never the only one.

**The One Dark Object Rule.** A dark surface belongs to the machine: the route board, a command the visitor will paste, the closing band. Two dark objects competing in one viewport is a composition error; a dark object used as ornament is a world error.

## Typography

**Display / Body Font:** Overpass (fallback: Segoe UI, Helvetica Neue, Arial, sans-serif)
**Label / Mono Font:** Overpass Mono (fallback: SFMono-Regular, Consolas, Liberation Mono, monospace)

**Character:** Overpass descends from the FHWA road-sign alphabet — drawn to be read at speed, in weather, by people who are not looking at typography. It is plain without being neutral: slightly condensed, high-legibility, faintly official. Its mono is the instrument face: anything measured, machine-owned or literal is set in it. Both are self-hosted as woff2 with `unicode-range` splits (latin, latin-ext for ğ ş İ) and `font-display: swap`; licences ship beside the files.

### Hierarchy
- **Display** (700, `clamp(2.125rem, 1.4rem + 2.6vw, 3.375rem)`, 1.06, -0.02em): the page's one h1. Balanced wrap, capped at 18ch so it always breaks into a shape.
- **Headline** (700, `clamp(1.625rem, 1.2rem + 1.4vw, 2.25rem)`, 1.12, -0.015em): section headings. Capped at 30ch.
- **Title** (700, 1.1875rem, 1.3, -0.005em): step headings, table titles, card heads.
- **Lede** (400, 1.1875rem, 1.55, `--ink-2`): the sentence under a heading. One per section, never two.
- **Body** (400, 1.0625rem, 1.6): running text, capped at `--measure` (66ch).
- **Small** (400, 0.9375rem): table cells, list rows, notes, nav links, secondary descriptions.
- **Label** (700, 0.75rem, 0.08em, uppercase, `--ink-3`): column names in sans — hero facts, release panel fields, ledger head.
- **Label, mono** (0.75rem, 0.06em, uppercase, `--ink-3`): machine-ish labels — the demo tag, support row kinds, board state text, the scroll hint.
- **Code** (0.8125rem, 1.55, `--track` on board): install commands, hashes, commits, dates.
- **Figure** (700, mono, `tabular-nums`): every measured value in the ledger; right-aligned, never wrapped.

### Named Rules

**The No Kicker Rule.** Nothing sits above a heading. No eyebrow, no kicker, no small uppercase category line stacked over an h1 or h2. Labels are columns in a table, terms in a definition list, or inline beside a row — never a hat.

**The Mono Means Measured Rule.** Overpass Mono marks what a machine produced or owns: numbers, hashes, versions, commands, state words, step numerals, timestamps. Prose is never mono. A number a human wrote as an estimate is not set in mono, because it is not a measurement.

**The Middle Dot Rule.** U+00B7 renders flush against its right neighbour in Overpass (bad right side bearing). Use it only in Overpass Mono, where it is correctly spaced. In the sans face, separate with a comma or a bracket.

**The Paired String Rule.** The site is authored in Turkish and switched in place: every visible string has an entry in the English map in `site.js`, and translation happens on bound text nodes, not by swapping markup. A new string without its pair is an unfinished string. Dynamic text the release reader owns is explicitly excluded from the map and gets its words from the manifest or from `uiText`.

## Layout

A single centred column: `width: min(100% - 2.5rem, 1180px)`, so the gutter is 1.25rem a side and the measure never exceeds 66ch for running text. Vertical rhythm is one value: sections are `clamp(3rem, 6vw, 5.5rem)` tall top and bottom, and are separated by a 1px `--rule` hairline rather than by space alone. Inside a section, the recurring steps are 0.5 / 0.75 / 1 / 1.25 / 1.5rem for local grouping, 2.5rem to open a block, and 3.5rem to start a new subject inside the same section.

Composition is two-column asymmetric where a claim faces its evidence — hero `1fr / 1.1fr`, install and release `0.9fr / 1.1fr`, with a `clamp(2rem, 5vw, 4.5rem)` gutter — and full-width ruled rows everywhere else. Multi-item content is a grid of ruled rows or a real table: three columns for the steps, two for the manages list and the interlock notes.

Breakpoints are few and each does one job: **900px** collapses every two-column grid to one and wraps the header (the nav becomes a horizontally scrolling row with its scrollbar hidden); **800px** collapses the steps and the two-column definition lists; **640px** relaxes the sticky column, tightens the support row and reveals the scroll hint; **560px** drops the header CTA and makes hero and closing buttons full-width.

### Named Rules

**The Ruled List Rule.** Grouped content is a ruled list, a definition list or a table. Not cards. A hairline top border, one hairline between rows, and the type does the rest. If a layout needs a box to be legible, the hierarchy is wrong.

**The Scrollable Table Rule.** A table wider than its column stays a table. It goes inside `.table-scroll`: focusable (`tabindex="0"`), named (`role="region"` with `aria-labelledby`), painted with left/right scroll shadows, and — below 640px — announced by a visible sentence, "Tablo yana kayar." / "The table scrolls sideways." The first column is sticky, and the table uses `border-collapse: separate` because sticky cells bleed through collapsed borders. A `.is-scrolled` class, set from the region's `scrollLeft`, adds a drop shadow to the pinned column so the reader can see they have moved.

## Elevation & Depth

The system is flat. Depth is carried by tone and by hairlines: paper against lit paper against the dark board, separated by 1px `--rule` or `--rule-strong`. There is exactly one shadow token in the stylesheet and it exists to seat the dark objects on the paper, not to lift the interface.

### Shadow Vocabulary
- **Board seat** (`box-shadow: 0 18px 40px -22px rgba(16, 22, 15, 0.35)`): a wide, low-opacity, downward-cast shadow under a `--board` object — the route board and the command cards. Nothing else uses it.
- **Sticky column edge** (`1px 0 0 var(--rule-strong)`, gaining `8px 0 12px -6px rgba(16, 22, 15, 0.45)` when `.is-scrolled`): not elevation but a scroll signal, painted on the one edge where a left-edge cue can still appear.

### Named Rules

**The One Shadow Rule.** Surfaces are flat. The only shadow is the board seat, and only a `--board` object may cast it. Paper panels (the release panel, table regions) get a `--rule-strong` border instead — never a shadow, never both.

## Shapes

Corners are almost square: 4px (`--radius`) on anything you click — buttons, the language switch, the copy button, the skip link — and 6px on the large dark or panelled objects (route board, command card, release panel), so the big surfaces read as slightly softer plates than the controls on them. Marks in the interlocking table are 18px squares with a 3px corner; the focus ring's own radius is 2px.

Borders carry the form language, not fills: 1.5px on buttons (so a quiet button and a primary button occupy identical space), 1px `--rule` between rows and cells, 1px `--rule-strong` around a table region or a paper panel, 1px `--board-2` inside a dark object. Circles are reserved for lamps — the board's 9px signal lamps, the 7–8px live and status dots, the brand mark's two lamps — and are the only fully round shapes in the system.

The build carries one fully-rounded pill (999px) on the demonstration tag beside the interlocking table. Treat it as a one-off that has not been promoted: do not add pills.

## Components

### Buttons
- **Character:** a button is a lever — a plain dark handle, no gradient, no glow, no icon.
- **Shape:** nearly square (4px), 1.5px border always present so variants share a footprint.
- **Primary ("lever"):** `--lever` ground, `--lever-fg` type, 700 weight, 0.7rem 1.15rem; `--lever-hover` on hover. `.button-large` (0.9rem 1.4rem, 1.0625rem) is the hero and closing-band size.
- **Quiet:** transparent ground, `--ink` type, `--rule-strong` stroke; `--paper-2` ground on hover.
- **On a board ground:** the primary inverts — `--track` ground, `--board` type, going to white on hover. This is the only inversion in the system.
- **Hover / focus:** 160ms `--ease-out` (`cubic-bezier(0.16, 1, 0.3, 1)`) on background, border and colour only. Never transform, never scale. Focus is the global ring: `2px solid var(--ink)`, 3px offset.
- **Disabled:** `aria-disabled="true"` → 0.55 opacity, pointer events off. Links that are not yet real (before the release manifest loads) also drop out of the tab order and gain their href when the data arrives.

### Route Board (signature)
The server drawn the way a signal box draws its track: a `--board` figure with a mono head (name, live dot), an SVG diagram, and a refusal sentence below a `--board-2` seam. Track lines are 2px `--track-dim`; a set route is 4px `--track`; a refused route is 4px `--lamp-red` dashed `6 8`. Lamps are 9px circles, `--lamp-off` at rest, taking `.is-clear` / `.is-caution` / `.is-stop`. A lock mark sits on the junction that blocks the route. Route labels are sans 15px/600 in `--track`; state words are mono 12px uppercase in `--track-soft`. The refusal paragraph is the point of the component: it names what is locked and why, in the product's own words.

**The One Authored Moment Rule.** The page has exactly one animation, and it is here: on load the routes set — lamps run off → amber → green (`lamp-set`, 1.1s, staggered 220ms / 420ms), the set route draws in (`route-draw`, 900ms via `stroke-dashoffset`), the stop lamp lights at 900ms (`lamp-stop`), and the lock mark lands at 1250ms (`lock-land`, scale 1.8 → 0.92 → 1). All four keyframes live inside `@media (prefers-reduced-motion: no-preference)`; with reduced motion the board renders its end state immediately, fully readable. Any future motion must pass the same test: it must have an end state that is the whole message.

### Tables
- **Interlocking table:** rows are requests, columns are conditions, cells are marks. `border-collapse: separate`, `min-width: 720px`, cells 0.75rem/0.85rem with `--rule` bottom and right borders. Head cells are 0.8125rem/700 `--ink-2`, bottom-aligned so long column names stack cleanly. Row headers are 600, `white-space: nowrap`, sticky at `left: 0` on `--paper-lit`. A locked cell carries an 18px `--red-ink` square; a free cell carries an 18px square outlined in `--rule-strong` with no fill. Both marks have accessible names ("kilitli"/"locked", "serbest"/"free") that switch with the language.
- **Ledger table:** the evidence table. `border-collapse: collapse`, `min-width: 760px`, uppercase mono-scale head in `--ink-3` over a `--rule-strong` rule. The value column is mono, 700, 1.0625rem, right-aligned, `nowrap`; the date column is mono 0.8125rem `--ink-3`.
- Both live inside `.table-scroll` (see The Scrollable Table Rule) and are followed by their `.scroll-hint`.

### Command Card
A `--board` article with a 6px radius, a head row (title + copy button) over a `--board-2` seam, and a focusable `<pre>` of mono 0.8125rem `--track`. The copy button is a hairline outline in `rgba(232,236,231,0.35)` on transparent, filling to `--board-2` on hover; on success it takes `.copied` and turns its border and text `--lamp-green` for 1800ms while an `aria-live` region announces the result in the current language. Failure swaps the label to "Select text" rather than pretending it worked.

### Release Panel
A `--paper-lit` panel with a `--rule-strong` border, 6px radius: an uppercase sans label, a mono 1.5rem version number, a status line with a dot, a ruled definition list (`9rem` term column) for date, commit and SHA-256, and two buttons. Its whole state is expressed through the signal inks on the one status dot and its text: `aria-busy="true"` → `--amber-ink` text with an amber dot; loaded → `--green-ink`; `.release-error` → `--red-ink`, with the failure message in the date field. The panel never shows a green it has not verified.

### Support Row
A full-width link laid out as three columns (`6.5rem` mono kind label / text / affordance), ruled top and bottom, hovering to `--paper-2` with the affordance sliding 4px right. Below 640px the label column narrows to 4.5rem.

### Navigation
Sticky header on a 92% `--paper` ground (`color-mix`) with a `--rule` bottom edge and a slight backdrop blur; 4rem minimum height. Links are 0.9375rem/600 `--ink-2` with a transparent 2px bottom border that becomes `--rule-strong` on hover. The language switch is a bordered two-button group sharing one 4px radius, the selected side taking the `--lever` fill and driven by `aria-pressed`. The header CTA is the same lever button at nav scale. Below 900px the nav drops to its own full-width row and scrolls horizontally; below 560px the CTA is hidden because the hero's primary action is one screen away.

### Footer
No dark band and no columns of links: brand mark, name, one sentence, an inline link row, and a mono bottom line separated by a `--rule` hairline. The closing call-to-action band above it is the last `--board` object on the page.

## Do's and Don'ts

### Do:
- **Do** put every measured number on paper in mono with tabular figures, beside the condition it was measured under and the date it was measured (the ledger's four columns: measure, value, condition, date).
- **Do** reach for a ruled list, a definition list or a real table before any container — the manages list, the plain lists and the support rows are all the same device at different densities.
- **Do** use the `-ink` signal variants (`--red-ink` #a20d26, `--amber-ink` #7a5a08, `--green-ink` #1e6f45) for any state shown on paper, and the `--lamp-*` variants only on `--board`.
- **Do** pair every signal colour with a word or an accessible name.
- **Do** wrap any table that can overflow in `.table-scroll` — focusable, named, scroll-shadowed, sticky first column, `border-collapse: separate`, and the visible "Tablo yana kayar." / "The table scrolls sideways." hint below 640px.
- **Do** self-host fonts under `/assets/fonts/` with `unicode-range` splits and ship their licences beside them; the CSP is `default-src 'none'` with `font-src 'self'`.
- **Do** keep structure conventional: top nav left to right, one primary action in the ordinary place, sections in the order a buyer expects. Ease of use outranks expression.
- **Do** give every new visible string its English pair in `site.js`, and let the text-node walker do the switching.
- **Do** give any new motion a reduced-motion end state that carries the whole message on its own.

### Don't:
- **Don't** use red, amber or green as a category colour, a brand accent, a button fill or decoration. They report state or they do not appear.
- **Don't** put a lamp colour on paper, or paper ink on the board.
- **Don't** stack a kicker, eyebrow or small uppercase category line above a heading. Labels are columns or inline terms.
- **Don't** build an icon-card grid, a feature tile wall, a fake dashboard screenshot, or a gradient hero. Those are the confirmed rut.
- **Don't** invent a metric, a logo, a testimonial or a customer count. Every number on a surface is measured and dated, or it is cut.
- **Don't** use the middle dot (·) in the sans face — it renders flush in Overpass. Use a comma or brackets; keep the dot for Overpass Mono.
- **Don't** add a second shadow, or put the board seat shadow under a paper surface. Panels get a `--rule-strong` border.
- **Don't** place two dark objects in the same viewport, or use a dark ground for anything the machine does not own.
- **Don't** add inline `style` attributes or inline `<script>` on the public site; the CSP forbids them, and no asset may come from a CDN.
- **Don't** animate transform or scale on interactive elements. Transitions are 160ms `--ease-out` on colour, background and border only.
- **Don't** show a success state the system has not verified — an unproven step reports as unfinished, not as green.
