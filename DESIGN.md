---
name: CelikPanel
description: A control tower for hosting operations — calm at rest, dense with data, unmissable when something is wrong.
colors:
  bg: "#F8FAFC"
  surface: "#FFFFFF"
  surface-2: "#F1F5F9"
  surface-3: "#E2E8F0"
  border: "#E2E8F0"
  border-strong: "#CBD5E1"
  fg: "#0F172A"
  fg-muted: "#475569"
  fg-subtle: "#94A3B8"
  primary: "#2563EB"
  primary-hover: "#1D4ED8"
  primary-fg: "#FFFFFF"
  success: "#16A34A"
  success-fg: "#FFFFFF"
  warning: "#CA8A04"
  warning-fg: "#020617"
  danger: "#DC2626"
  danger-fg: "#FFFFFF"
  sidebar-bg: "#1E293B"
  sidebar-fg: "#E2E8F0"
  sidebar-fg-muted: "#94A3B8"
  sidebar-heading: "#64748B"
  sidebar-hover: "#334155"
  sidebar-active: "#2563EB"
  sidebar-active-fg: "#FFFFFF"
  sidebar-border: "#334155"
typography:
  display:
    fontFamily: "var(--font-sans)"
    fontSize: "1.875rem"
    fontWeight: 600
    lineHeight: "2.25rem"
    letterSpacing: "-0.01em"
  headline:
    fontFamily: "var(--font-sans)"
    fontSize: "1.5rem"
    fontWeight: 600
    lineHeight: "2rem"
    letterSpacing: "-0.01em"
  title:
    fontFamily: "var(--font-sans)"
    fontSize: "1.125rem"
    fontWeight: 600
    lineHeight: "1.75rem"
    letterSpacing: "normal"
  body:
    fontFamily: "var(--font-sans)"
    fontSize: "0.875rem"
    fontWeight: 400
    lineHeight: "1.25rem"
    letterSpacing: "normal"
  label:
    fontFamily: "var(--font-sans)"
    fontSize: "0.75rem"
    fontWeight: 500
    lineHeight: "1rem"
    letterSpacing: "normal"
rounded:
  md: "0.375rem"
  lg: "0.5rem"
  xl: "0.75rem"
  2xl: "1rem"
  full: "9999px"
spacing:
  xs: "0.25rem"
  sm: "0.5rem"
  md: "0.75rem"
  lg: "1rem"
  xl: "1.5rem"
components:
  button-primary:
    backgroundColor: "{colors.primary}"
    textColor: "{colors.primary-fg}"
    typography: "{typography.label}"
    rounded: "{rounded.lg}"
    padding: "0.375rem 0.75rem"
  button-primary-hover:
    backgroundColor: "{colors.primary-hover}"
    textColor: "{colors.primary-fg}"
  button-secondary:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
    typography: "{typography.label}"
    rounded: "{rounded.lg}"
    padding: "0.375rem 0.75rem"
  button-secondary-hover:
    backgroundColor: "{colors.surface-2}"
    textColor: "{colors.fg}"
  button-danger:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.danger}"
    typography: "{typography.label}"
    rounded: "{rounded.lg}"
    padding: "0.375rem 0.75rem"
  card:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
    rounded: "{rounded.xl}"
  card-header:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
    typography: "{typography.label}"
    padding: "0.75rem 1rem"
  input:
    backgroundColor: "{colors.surface}"
    textColor: "{colors.fg}"
    typography: "{typography.body}"
    rounded: "{rounded.lg}"
    padding: "0.375rem 0.75rem"
  status-dot:
    backgroundColor: "{colors.success}"
    rounded: "{rounded.full}"
    size: "0.5rem"
  sidebar-item:
    backgroundColor: "{colors.sidebar-bg}"
    textColor: "{colors.sidebar-fg}"
    typography: "{typography.label}"
    rounded: "{rounded.lg}"
    padding: "0.5rem 0.75rem"
  sidebar-item-active:
    backgroundColor: "{colors.sidebar-active}"
    textColor: "{colors.sidebar-active-fg}"
---

# Design System: CelikPanel

## Overview

**Creative North Star: The Control Tower.**

A control tower is quiet most of the night. Its whole design exists so that when
something does happen, nobody has to look for it. That ordering is the system's
governing rule, and it resolves every conflict between the three qualities this
interface needs:

1. **At rest, be calm.** Steady state is quiet. Nothing pulses, nothing competes
   for attention, surfaces stay flat and neutral. Calm is not decoration — it is
   what makes an alarm legible. If everything is loud, nothing is.
2. **In use, be dense and exact.** This is a professional tool operated in long
   sessions on wide screens. Information density outranks whitespace; restraint
   in components buys room for data.
3. **On exception, be unmissable.** In-flight operations, degraded states,
   half-finished work and failures are **first-class design subjects, not
   afterthoughts.** A screen must always be able to say what state it is in, how
   long it has been there, and what the operator can do about it.

Rule three is the one that earns the metaphor. This product's documented history
is of operations that stop halfway and interfaces that cannot say so. A design
that cannot express "this is stuck, here is why, here is your way out" is not
finished, however elegant its steady state.

**Mood:** precise, calm, industrial, legible. The panel is instrumentation, not
furniture and not a brochure.

**Anti-references:** the crowded portal (nested tabs, everything visible at
once); the consumer dashboard (oversized hero metrics, decorative gradients,
celebratory illustration); and the "friendly" enterprise look that softens
danger into pastel.

### Open decisions — do not treat as settled

- **Competitor skins.** The product ships `plesk`, `aapanel` and `cpanel` skins
  alongside the default `celik`. Whether they are a durable commitment is
  **undecided**. This document describes the project's own palette only. Do not
  design *for* the competitor skins, and do not remove them.
- **Typeface identity.** The system uses Inter. It is a competent, neutral
  default rather than a chosen identity, and it is widely flagged as an overused
  interface face. Replacing it is an open product decision, not a defect to be
  fixed unilaterally. Until it is decided, keep referencing `--font-sans` and
  never hard-code a family.

## Colors

**The normative source is `web/src/index.css`,** where every colour is a
space-separated RGB triplet (`--primary: 37 99 235`) consumed through
`rgb(var(--primary) / <alpha-value>)` in `web/tailwind.config.js`. That form is
required for Tailwind's opacity modifiers (`bg-primary/10`) and is not itself a
valid standalone CSS colour, so the frontmatter above mirrors the same values in
hex for portability. **If they ever disagree, `index.css` wins** — update it
first, then this file.

**Light and dark are two value sets of the same names.** There is no separate
dark palette: `.dark` on `<html>` redefines the same tokens.

| Role | Light | Dark | Use |
|---|---|---|---|
| `bg` | `#F8FAFC` | `#020617` | The page ground. Never a card. |
| `surface` | `#FFFFFF` | `#0F172A` | Cards, panels, inputs, menus. |
| `surface-2` | `#F1F5F9` | `#1E293B` | Recessed areas, table headers, hover fills, track backgrounds. |
| `surface-3` | `#E2E8F0` | `#334155` | The deepest tonal step; use sparingly. |
| `border` | `#E2E8F0` | `#1E293B` | Default separation. |
| `border-strong` | `#CBD5E1` | `#334155` | Interactive edges: inputs, secondary buttons. |
| `fg` | `#0F172A` | `#F1F5F9` | Primary text. |
| `fg-muted` | `#475569` | `#94A3B8` | Secondary text, labels, icons. |
| `fg-subtle` | `#94A3B8` | `#64748B` | Placeholders, disabled, inactive dots. Never for meaning. |
| `primary` | `#2563EB` | `#3B82F6` | The single call to action, active nav, focus. |
| `success` | `#16A34A` | `#22C55E` | Healthy, running, complete. |
| `warning` | `#CA8A04` | `#EAB308` | Attention needed, degraded, approaching a limit. |
| `danger` | `#DC2626` | `#EF4444` | Failed, stopped, destructive. |

**Semantic colour is reserved for state.** `success`, `warning` and `danger`
carry meaning and must never be used decoratively — no coloured section headers,
no accent stripes, no tinted cards for visual variety. When these three appear,
the operator is entitled to conclude something is true about the system.

**The sidebar is a dark rail in both themes.** It has its own token set
(`sidebar-*`) and does not follow the light/dark swap. This is deliberate: the
navigation is chrome, the content area is the work, and a constant rail keeps
the boundary between them stable when the theme changes.

**Never pair a semantic colour with `fg-muted` text.** Each has a paired
foreground token (`success-fg`, `warning-fg`, `danger-fg`); use it. Note
`warning-fg` is near-black, because `warning` is a yellow that fails against
white.

**Contrast is a requirement, not a preference.** WCAG 2.1 AA is the target:
4.5:1 for body text, 3:1 for large text and for the boundary of any control that
carries meaning. `fg-subtle` on `surface` is below AA for body copy — it is a
placeholder and disabled colour only, never the colour of information.

## Typography

One family, sourced from `--font-sans` so a skin can reshape it. The scale below
is Tailwind's default steps, restricted to the ones the product actually uses.

| Role | Size | Weight | Used for |
|---|---|---|---|
| `display` | 1.875rem / 30px | 600 | Rare. Page-level identity, empty-state headline. |
| `headline` | 1.5rem / 24px | 600 | Page titles, primary metric values. |
| `title` | 1.125rem / 18px | 600 | Section and card titles. |
| `body` | 0.875rem / 14px | 400 | The default for everything readable. |
| `label` | 0.75rem / 12px | 500 | Field labels, table cells, badges, metadata, nav. |

**This is a 12–14px interface.** Measured across the codebase, 854 of 929 size
declarations are `label` or `body`. That is correct for the density this product
needs — but it means **12px is the floor and there is nothing below it.** Never
introduce a smaller step; the next request for "one size smaller" is answered
with weight, colour, or spacing instead.

**Weight carries hierarchy, size does not.** Two working weights: 500 for labels
and controls, 600 for anything that titles something. Bold (700) is reserved for
values the operator scans for — a count, a status word, a hostname. Because the
size range is so narrow, weight and colour do most of the hierarchical work; a
screen that needs a new size step almost always needs better grouping instead.

**Numbers that share a column get `tabular-nums`.** Ports, sizes, counts,
durations, serials and IP addresses are compared vertically far more often than
they are read as prose.

## Layout

**Shell:** a fixed dark navigation rail plus a scrolling content area. The rail
is built from a single declarative registry (`web/src/nav.ts`) filtered by the
signed-in role — there is one shell for all four roles, never a per-role layout.

**Content is card-based.** A page is a stack or grid of cards, each owning one
subject, each with a header that names it. Cards do not nest. When a card needs
internal grouping, use a bordered row (`border-b border-border py-5`), not
another card.

**Density is the default.** Card padding is `1rem` horizontal, `0.75rem`
vertical in headers; rows separate with a hairline rather than a gap. Prefer
adding a row to a card over adding a card to the page.

**Responsive intent:** this is a desktop-first operator tool. Tables and
diagrams scroll horizontally inside their own container so the page body never
scrolls sideways. Below the tablet breakpoint the rail collapses and cards go
single-column; no layout should require horizontal panning to operate.

**A service that is not installed is not shown.** No empty screens, no disabled
menu items for absent services. This is a product principle with a layout
consequence: navigation length varies per server, and the design must not assume
a fixed set of sections.

## Elevation & Depth

**Philosophy: scaled layering.** Depth is primarily *tonal* — `surface`,
`surface-2`, `surface-3` plus borders — and shadow marks only the small number of
things that genuinely float above the page.

| Step | Shadow | Applies to |
|---|---|---|
| 0 — flat | none | Page ground, rows, inline groups, table headers. The default. |
| 1 — card | `0 1px 2px 0 rgb(0 0 0 / .04), 0 1px 3px 0 rgb(0 0 0 / .06)` | Cards resting on the page. |
| 2 — popover | `0 4px 6px -1px rgb(0 0 0 / .07), 0 2px 4px -2px rgb(0 0 0 / .06)` | Dropdowns, menus, tooltips. |
| 3 — modal | `0 20px 25px -5px rgb(0 0 0 / .10), 0 8px 10px -6px rgb(0 0 0 / .08)` | Dialogs and the operation overlay. |

Only step 1 exists in the code today (`shadow-card` in
`web/tailwind.config.js`); steps 2 and 3 are the defined targets for the next
components that need them. **Four steps is the whole vocabulary** — there is no
step 4, and a new component picks an existing step rather than inventing one.

**In dark mode, shadows barely read.** Depth there comes from the surface steps
and `border-strong`. Do not compensate by increasing shadow opacity in dark; step
up a surface tone or add a border instead.

**No glow, ever.** Coloured shadows, glowing borders and blurred halos are
banned in both themes.

## Shapes

| Token | Value | Applies to |
|---|---|---|
| `md` 0.375rem | small controls | Badges, chips, compact inputs. |
| `lg` 0.5rem | **the default** | Buttons, inputs, menu items, nav items. |
| `xl` 0.75rem | containers | Cards and panels. |
| `2xl` 1rem | large containers | Modals, full-width feature panels. |
| `full` | pills and dots | Status dots, progress tracks, avatars, count pills. |

**The rule of thumb: the bigger the box, the rounder the corner** — `lg` for
controls, `xl` for the card that holds them, `2xl` for the dialog that holds the
card. This keeps concentric corners visually parallel.

**Known gap — fix as you touch it.** `md`, `lg`, `xl` and `2xl` are wired to CSS
variables, so a skin can reshape them. Plain `rounded` (Tailwind's 0.25rem
default) and `rounded-full` are **not**, and the codebase currently has 52 uses
of `rounded` and 66 of `rounded-full` that therefore sit outside the token
system. `rounded-full` is legitimate — a pill is a pill at any scale — but bare
`rounded` is not: replace it with `rounded-md` when you touch a file that uses
it.

**Borders are hairlines.** 1px, `border` by default and `border-strong` where the
edge is interactive. Thick borders and one-sided accent stripes on cards are
banned: they are the most recognisable tell of generated UI, and here they would
also collide with the semantic-colour rule.

**Icons** are Lucide, sized `1rem` (`h-4 w-4`) beside `label` and `body` text,
with stroke weight driven by `--icon-stroke` so a skin can adjust it.

## Components

**Buttons.** One primary action per view; everything else is secondary or
danger. Primary is a filled `primary` block. Secondary is a `surface` fill with a
`border-strong` edge. Danger is *not* a filled red block — it is a `surface`
fill with `danger` text, escalating to a tinted background on hover. A
destructive action should look serious without shouting before it is chosen.

**Cards** are `surface` on `border`, radius `xl`, elevation 1, with an optional
header that carries an icon, a `label`-weight title and an action slot. The
header is separated by a hairline, not a fill.

**Inputs** sit on `surface` with a `border-strong` edge, radius `lg`. Focus is
`border-primary` plus a 2px `primary/30` ring — the ring is the accessible focus
indicator and must never be removed. Placeholders use `fg-subtle`; helper text
uses `fg-muted` at `label` size.

**Status dot** — a `full` 0.5rem dot: `success` when healthy, `fg-subtle` when
inactive. Never `danger` for merely "off"; a stopped service that is *supposed*
to be stopped is not an error.

**Usage bar** — a `full` track on `surface-2` with a fill that crosses from
`primary` to `warning` at 75% and to `danger` at 90%. The thresholds are part of
the component's meaning, not a per-instance choice.

**Operation states are components, not afterthoughts.** Any privileged operation
(installing a service, switching a DNS engine, applying an update) must render:
the stage it is in, how long it has been running, the request identifier, and —
on failure — the exact error code plus at least one action the operator can take.
Every operation UI needs four designed states: *running*, *succeeded*,
*failed-with-cause*, and *unreachable-backend*. The last is not a spinner. It
says the server is restarting or needs recovery, and it is dismissible.

**There is exactly one spinner and one tab underline.** Both belong in
`web/src/components/ui.tsx`. At the time of writing the spinner markup is
duplicated 61 times across 38 files and the tab underline 9 times; new code uses
the shared primitive.

**Empty states name the reason.** "No domains yet" plus the action that creates
one — never a bare empty table, and never an illustration.

## Do's and Don'ts

**Do**

- Reference semantic tokens (`bg-surface`, `text-fg-muted`, `border-border`) and
  let the theme and skin layers resolve them.
- Reserve `success` / `warning` / `danger` for actual system state.
- Use weight and colour before reaching for a new size step.
- Give every long-running operation a terminal state and an exit.
- Put digits that align in a column in `tabular-nums`.
- Keep the whole page operable from the keyboard, with a visible focus ring.

**Don't**

- Hard-code a hex colour, a font family, or a radius. If a value is worth using
  twice it is worth being a token; if it is used once, it is probably wrong.
- Put a thick or one-sided accent border on a card.
- Nest a card inside a card.
- Introduce a size step below 12px, or a fifth elevation step.
- Use a coloured glow, a gradient background, or a pulsing dot. A pulse implies
  live data; static status must not pretend to be live.
- Use `danger` to mean "off", or `fg-subtle` to carry information.
- Add a decorative icon tile above a heading, or a tracked uppercase eyebrow
  above every card title. Labels earn their place by naming something.
- Design for the competitor skins, or against them, while that decision is open.
