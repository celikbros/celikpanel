# design-sync notları / notes

- `cssEntry` hash'lenmiş Vite çıktısını gösterir (`dist/assets/index-<hash>.css`).
  `web/` yeniden derlenince hash değişir ve converter glob desteklemediği için
  cssEntry'yi YENİ dosya adına güncellemek gerekir; bayat kalırsa
  "cssEntry: ... not found — skipped" der ve token/font CSS'i paketten düşer.
  / `cssEntry` points at the fingerprinted Vite output; after every `npm run
  build` in `web/`, update it to the new `dist/assets/index-<hash>.css` (the
  converter takes a literal path, no globs). Stale → silently skipped and the
  token/font CSS drops out of the bundle.
- Marka fontları self-hosted: `web/src/fonts/` (Inter değişken + Open Sans
  latin/latin-ext, SIL OFL). Vite `renderBuiltUrl` CSS içi url()'leri göreli
  yapar — `extractFonts` bunları `fonts/`'a bu sayede çıkarabilir. / Brand
  fonts are self-hosted in `web/src/fonts/`; Vite's `renderBuiltUrl` keeps
  CSS-internal url()s relative, which is what lets `extractFonts` lift them
  into `fonts/`.
