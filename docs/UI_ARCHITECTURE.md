# Frontend Architecture — Roles & Layout

*Design document · July 3, 2026 · [Türkçe](UI_ARCHITECTURE.tr.md)*

This answers one question: with four user roles (Administrator, Reseller, Customer, Additional User — see [ROLES.md](ROLES.md)), how is the UI structured? Separate apps? A master page per role? Or one inherited shell?

---

## Decision: one inherited shell, driven by capabilities

**There is a single application shell. Navigation, routes and actions are rendered from the signed-in user's role and capabilities. There are no separate apps and no hand-maintained per-role master pages.**

```
        ┌───────────────────────────────┐
        │   AppShell (one layout)       │
        │  ┌─────────┬────────────────┐ │
        │  │ Sidebar │  Page outlet   │ │
        │  │ (built  │  (shared       │ │
        │  │  from   │   feature      │ │
        │  │  caps)  │   components)  │ │
        │  └─────────┴────────────────┘ │
        └───────────────────────────────┘
                    ▲
                    │ reads
        ┌───────────────────────────┐
        │ AuthContext: { role,      │
        │   capabilities[] }        │
        └───────────────────────────┘
```

The signed-in user carries a **capability set**. The sidebar, the routes and the in-page action buttons are all derived from that set. An admin's shell shows Services, Users and server tools; a customer's identical shell shows only their own domains, databases and mail; an additional user's shell shows only the slices their parent delegated. Same shell, same components — different capabilities in, different UI out.

---

## Why not the other two options

### ❌ Completely separate pages / apps (the cPanel model)
cPanel physically splits WHM (admin/reseller) from cPanel (end user) — two interfaces, two codebases' worth of duplication. That is the *opposite* of our [constitution](../ROADMAP.md): it means a domain screen written twice, a bug fixed twice, two mental models to learn. Rejected.

### ❌ A master page per role
Four parallel layouts (AdminLayout, ResellerLayout, CustomerLayout, UserLayout) look tidy at first but drift immediately: a change to the header, the notification bell, the theme, or the shell must be made four times and inevitably gets made in three. It also can't express "a customer with one extra capability" without a fifth layout. Rejected.

### ✅ One inherited shell, capability-driven
- **Simplicity (Google principle):** one shell, one mental model, one place to change the frame. The *same* domain-management component serves a customer managing their site and an admin inspecting it.
- **Consistency with the backend:** [ROLES.md](ROLES.md) already says *"the UI hides what the API would reject."* This architecture is that sentence made real — the capability set the UI renders from is the same one the backend enforces.
- **Additional users fall out for free:** they are just a customer shell with a smaller capability set. No new layout, no new pages.
- **Extensible:** a future "distributor" tier or a new capability is a data change (new entries in the capability set), not a new app.

---

## How it works

1. **AuthContext** — on load, `GET /api/v1/auth/me` returns `{ username, role, capabilities }`. This lives in a React context at the root, above the router.
2. **Navigation registry** — a single declarative list maps each nav item / route to the capability it requires. The sidebar is built by filtering this list against the user's capabilities. Nothing is hardcoded per role.
3. **Route guards** — each route checks its required capability. A customer who types `/services` is redirected, not shown a broken page. (Defense in depth — the API would reject the calls anyway.)
4. **Action gating** — in-page buttons ("Delete domain", "Issue SSL") check capabilities the same way, so a read-only additional user sees data without the mutating controls.
5. **The backend stays the source of truth.** The UI gating is for usability, never for security; every request is still authorized server-side (see [ROLES.md](ROLES.md) enforcement section). A tampered frontend gains nothing.

---

## What each role sees (same shell, different nav)

| Section | Admin | Reseller | Customer | Additional User |
|---|:---:|:---:|:---:|:---:|
| Server dashboard (CPU/RAM/disk) | ✅ | — | — | — |
| Services (install/start/stop) | ✅ | — | — | — |
| Users & resellers | ✅ | own customers | — | — |
| Resource pool / plans | ✅ | ✅ | — | — |
| Domains & sites | all | their customers' | own | delegated |
| Databases · Mail · DNS · Files | all | their customers' | own | delegated slices |
| Account settings | ✅ | ✅ | ✅ | limited |

---

## Theming (decided)

The panel supports **both light and dark themes**, user-switchable via a theme toggle. The default follows the operating system preference (`prefers-color-scheme`), falling back to light. Mechanically: the existing `web/src/theme.ts` tokens are wired to CSS custom properties, and both themes are two value sets of the *same* variables — components reference the variables, never hardcoded colors. This is skin over the shell; it changes no structure.

## Scope boundary

This document is about **structure** (the bones): one shell, capability-driven rendering. Beyond the theming decision above, it stays silent on the finer **visual design** (typography scale, spacing rhythm, component styling). Those are chosen when the redesign work begins and layer on top of this architecture without changing it — a redesign repaints the shell, it does not re-architect it.

The current code does **not** yet follow this: navigation is hardcoded in `Layout.tsx` and is identical for everyone. Adopting this architecture is the frontend half of realizing [ROLES.md](ROLES.md), and is scheduled alongside the authorization/ownership work.
