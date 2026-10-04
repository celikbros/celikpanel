These bytes were emitted by the real Alpha81 Agent producer in source commit
45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00, built on ext4 with Go 1.26.5:
`newMailHostCertificateReceipt` + `canonicalMailHostCertificateReceipt`, and
`json.Marshal(mailHostRenewal{Lineage: mailHostCertLineageName(domain), ...})`.

Inputs are public test values: request ID 32 `a`, qualifier `mhc1:` + 64 `b`,
domain `mail.example.test`, leaf bytes `historical leaf DER fixture`. This tests
the historical evidence writer, not validity of a live certificate or renewal.
Receipt includes its canonical trailing newline; pending intentionally has none.
