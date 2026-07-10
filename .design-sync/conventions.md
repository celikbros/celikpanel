# CelikPanel design system

The building blocks of the CelikPanel hosting control panel — a dense,
Plesk-style admin UI. Import components from `celikpanel-web`; each is a plain
presentational React component (no data fetching, no side effects).

Available components: `PageHeader`, `Card`, `Button`, `StatusDot`, `UsageBar`,
`SearchInput`, `FormSection`, `Field`, `ToggleRow`, `FormActions`,
`EmptyState`. `inputClass` is an exported string to spread onto your own
`<input>`/`<select>`.

## Setup

No provider or wrapper is required — the components read no context. The
design's stylesheet (`styles.css`) supplies everything: the color tokens plus
the compiled component CSS (`_ds_bundle.css`). Light theme is the default
(tokens on `:root`); a dark surface is opt-in by adding `class="dark"` (or
`data-theme="dark"`) on a root element.

## Styling idiom — utility classes over semantic tokens

Compose your own layout with Tailwind utility classes, and **always reach for a
semantic token class, never a raw hex color** — that is what keeps a design
on-brand and theme-correct. The real vocabulary (all present in the compiled
stylesheet):

| Purpose | Classes |
|---|---|
| Surfaces | `bg-surface`, `bg-surface-2` |
| Text | `text-fg` (primary), `text-fg-muted` (secondary), `text-fg-subtle` (faint) |
| Accent / primary action | `bg-primary` with `text-primary-fg` |
| Borders | `border-border`, `border-border-strong` |
| Status / feedback | `bg-success`, `bg-danger`, `bg-warning` |

Standard Tailwind spacing, sizing and radius utilities (`gap-3`, `p-4`,
`text-sm`, `rounded-lg`, `shadow-card`) are used freely alongside these.

## Where the truth lives

- Colors, tokens and compiled component styles: `_ds/celikpanel-web/styles.css`
  (and the `_ds_bundle.css` it imports). Read it before inventing any color.
- Each component's API is its `<Name>.d.ts`; usage notes and examples are in
  `<Name>.prompt.md`.

## One idiomatic composition

```jsx
import { Card, Button, StatusDot } from 'celikpanel-web';
import { Server } from 'lucide-react';

<div className="bg-surface-2 p-6">
  <Card title="Services" icon={Server} action={<Button variant="secondary">Rescan</Button>}>
    <div className="flex items-center justify-between px-4 py-3 text-sm text-fg">
      <span className="flex items-center gap-2">
        <StatusDot ok /> Nginx — running
      </span>
      <Button variant="danger">Stop</Button>
    </div>
  </Card>
</div>
```

Icons come from `lucide-react` (passed to `icon={}` props). One filled
`primary` Button per view; everything else is `secondary` or `danger`.
