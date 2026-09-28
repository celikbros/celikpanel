# PowerDNS primary to panel-free PowerDNS secondary: fixture preparation

This is source-only fixture preparation for Stage 2, P0.4/P0.5 and D-025
invariants 1, 2, 3 and 6. It is **not native acceptance**. No disposable guest
was started for this slice, no DNS service or installed panel was changed, and
no PowerDNS switch was initiated. The separate native BIND-to-PowerDNS attempt
remains failed and retained in
[`NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md`](NATIVE-PDNS-BIND-PEER-STAGE2-20260927.md).

The new `native_pdns_pdns_pair.py` uses the existing exact Arch PowerDNS
`CONSUMER` fixture for preparation. Its observation requires the selected
disposable paired-primary manifest, both guest markers, and absence of Agent
and Panel executables on the secondary. The Debian witness requires native
`pdns.service` active, BIND inactive, a managed `pdns` primary state receipt,
the current catalog publication serial, exact `PRODUCER` and `NATIVE` SQLite
rows, one member A row, canonical catalog AXFR, and authoritative UDP/TCP A
answers. The Arch witness requires the exact `CONSUMER` and loaded `SLAVE`
rows, catalog membership, and authoritative UDP/TCP A answers. The wrapper
compares primary and secondary catalog serial/member sets. These observations
do not convert an Agent operation to succeeded or prove deletion; terminal
add/edit/delete, parentless loaded-zone absence, same-operation recovery and
management-disabled reboot still require a native trial on a consistent bundle.

Source SHA-256: wrapper
`b643744caffca9e4198202627767bf437e9a5ee9543bab83e6499b141ed5f972`,
primary witness
`20a9e7ddd2427cf28c5fb20b7386d9aabc6e0fe8aab48a29e61ff81296c61c02`,
tests
`dca85890de35718cf2c4440424d4da6ba571909c54ef8714c65094c5c3b6c086`.
The six focused source tests passed, including refusal for a BIND service or
stale BIND authority receipt, a DNS answer whose expected IP appears only in
a comment, a foreign fixture cell, and mismatched producer/consumer catalog
serials. Python compile and `git diff --check` passed. No persisted product
schema, recovery authority, native service ownership or installed server
changed. Stage 2 remains open.
