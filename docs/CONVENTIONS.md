# Conventions — Language & Naming

*Project standard · July 3, 2026 · [Türkçe](CONVENTIONS.tr.md)*

CelikPanel serves a Turkish-first audience on an internationally-readable codebase. This document is the single source of truth for how language is used across the project. It is binding: new work follows it, and reviews enforce it.

---

## The rule in one line

**Technical names are English. Every explanation and all content is bilingual (Turkish + English). The product itself is multilingual, with Turkish and English as its two primary languages.**

---

## 1. Technical names → English only

Anything a machine reads is written in English, with no exceptions:

- File and directory names
- Database table and column names
- Function, method, variable, type, and constant names
- API routes and JSON field names
- Configuration keys and environment variables

Rationale: code is an international standard surface. Keeping identifiers in English keeps the codebase portable, greppable, and readable by any contributor.

## 2. Explanations & content → bilingual (TR + EN)

Everything a human reads for understanding exists in both languages:

- **Documentation** — kept as parallel files: `X.md` (English) and `X.tr.md` (Turkish). The two are kept in sync; neither is a second-class citizen.
- **Code comments** — when a comment is warranted (to state intent or a constraint, never to narrate the obvious), it is written English first, then Turkish. We do not add noise comments just to have something to translate.

Git commit messages are the one practical exception: they are written in a single language (Turkish, the team's working language) to keep history readable, and never carry AI co-author trailers.

Every commit credits the team with exactly these co-author trailers:

```
Co-Authored-By: Mehmet Ömer Efe Çelik <293130995+momerefe@users.noreply.github.com>
Co-Authored-By: Alperen Çelik <89036584+celikalperen@users.noreply.github.com>
```

## 3. The product → multilingual, TR + EN primary

The panel UI is fully internationalized:

- No hardcoded natural-language text in components. Every user-facing string comes from a translation key.
- Two primary locales ship and are always kept complete: **`tr`** and **`en`**.
- The architecture supports adding further locales later without code changes — just new message catalogs.
- Locale is chosen per user (with a sensible default) and remembered.

This is infrastructure, scheduled in the roadmap (Phase 1), not a Phase 0 feature — but the convention applies to any new UI string written from now on.

---

## Quick reference

| Thing | Language |
|---|---|
| File / directory names | English |
| DB tables & columns | English |
| Functions, variables, types | English |
| API routes & JSON fields | English |
| Config keys, env vars | English |
| Documentation | Bilingual (`.md` + `.tr.md`) |
| Code comments (when present) | Bilingual (EN, then TR) |
| UI strings | i18n keys → `tr` + `en` (+ more) |
| Commit messages | Turkish, single language |
