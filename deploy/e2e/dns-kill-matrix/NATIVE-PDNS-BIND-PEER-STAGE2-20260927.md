# PowerDNS primary to panel-free BIND secondary: blocked native trial

This is a bounded disposable QEMU observation, not a completed Stage 2 cell or
P0 acceptance claim. It did not touch installed servers. The relevant D-025
invariants are native DNS service independence, exact source-bound propagation,
and fail-closed recovery. No durable schema was changed by this fixture.

The exact `pdns-switch__intent__after-write__paired-primary__peer-reachable`
cell ran under `/var/tmp/cp-stage2-pdns-bind27`, isolated from other trials, with
Debian 13 `192.0.2.10` on SSH forward 2313 and Arch `192.0.2.11` on 2314;
peer port 23854. The image lock verification passed. Arch BIND 9.20.29 was
installed and configured as a panel-free RFC 9432 secondary. Its installed
Agent/Panel binaries were absent. The Debian bundle manifest SHA-256 was
`4059762531487fd9448dfc0de7f4d8749bc953b13f023402a4bb18627cd51d64`.
Installed Agent, Panel and trigger SHA-256 values were, respectively,
`aeaf8cae705315a7f66db25613db94d7b1340c2d4306e2af040b9032f5198dbc`,
`6624b18d6924b042cdd7ea1fa595b9004d15dfc7a3b8af631b9afa9921513a62`,
and `75391cdea92198f8a829d55e509446965b7384b3af16798a5824a7d0468512a1`.
These are captured 27d binaries, not a claim that later working-tree changes
were built into them. The new source-only fixture and test SHA-256 values were
`44f519d6e5d26f777070568e6978b929a5e6698d8ed48377faeb62c79e211a5a`
and `7bc833c161977f2cc36d844b1434bc72107f36f43f27135fb30d97011c74bed0`.

The initial production managed-BIND setup on Debian succeeded with request
`70243d8b5ad270f4c644c69d265c3ea1`. Before the cross-engine switch, the
fixture's exact `observe` passed: source and secondary catalog AXFR had serial
1 and member `s1-kill.test`; both native daemons returned authoritative SOA
and `www.s1-kill.test` A `192.0.2.10` over UDP and TCP. This proves the
panelless BIND peer can consume the initial BIND catalog, but says nothing yet
about PowerDNS production.

One untagged production Agent BIND-to-PowerDNS switch RPC then used request
`c532b1ea09cbfa3fdb650657bd78b949`, owner
`31201c6d5457538c1c7a8ec157c05b87`, qualifier
`dns-engine-switch/v1:sha256:c93693a14e44e0eaef8848ee4c1b6099e0d51e14d4743a06e45956846b863f75`.
It returned exit 1 without a terminal receipt. The Agent rejected
`verify PowerDNS pair catalog: PowerDNS producer contains noncanonical base records`.
The subsequent rollback proof rejected `PowerDNS rollback live database is not
the staged target`; the manager logged fail-closed ambiguous ledger state.
The exact journal remained `rolling-back`, ledger request remained `running`
at `leased`, and active engine receipt still identified BIND. No retry or
second DNS mutation was issued.

The read-only SQLite snapshot of the PowerDNS live database found a `PRODUCER`
row for `catalog-c000020a.celikpanel.invalid`, with NS `invalid.` and SOA
`invalid invalid 1790467209 60 30 3600 30` at TTL 60. Its `s1-kill.test`
member zone and A `192.0.2.10` were present. The canonical renderer in
`internal/binddns/pairing.go` expects an SOA shaped `invalid. invalid. <serial>
60 30 3600 30`; `cmd/agent/dns_engine_pdns_catalog.go`'s
`verifyPDNSProducerBaseTx` compares exact base records and emits the observed
rejection. In contrast, the Arch BIND secondary's retained catalog AXFR had
NS `invalid.`, SOA `invalid. invalid. 1 60 30 3600 30`, RFC 9432 version
TXT `"2"`, and one exact member PTR. Thus the PowerDNS live catalog's SOA
content and serial differed from the prior BIND source/peer base records.
The read-only snapshot cannot establish which component wrote the
noncanonical SOA. The candidate and pre-switch backup paths named in the
journal were absent when observed. `cmd/agent/dns_engine_pdns_switch.go`'s
`restorePDNSSwitchDatabase` invokes
`verifyPDNSSwitchDatabaseWithPrimaryCatalogSerial` on the live database before
removing it; that check emitted the staged-target mismatch. The exact verifier
subcondition has not yet been isolated, so no safe rollback correction is
inferred from this trial.

After the failure Debian `pdns.service`, `bind9.service`, and `named.service`
were inactive. Arch `named.service` remained active, retained catalog serial 1
and its member A response over UDP/TCP. Queries to Debian `192.0.2.10:53`
failed with connection refused. Thus the independent secondary still served
the prior DNS data, while the intended primary did not. The failed switch is
not PowerDNS-primary to BIND-secondary acceptance evidence.

Read-only logs, journal, ledger, SQLite row snapshot, service states and native
query responses are preserved under `/var/tmp/cp-stage2-pdns-bind27/evidence/`.
Representative SHA-256 values: `debian-agent-journal.json`
`5e1b73d2b9aeef69cbebd75d985265895ba494ac32b314a95627700edf3a6fad`,
`debian-dns-switch-journal.json`
`d48042c590a21f23062ad58c2bfe9d06112473762b02468688ce1adfc0be872a`,
`debian-mutation-ledger.json`
`7c55456183e234becaf97df4cbb7bd8ee70c48acd254ba37f90d415d48604281`,
`debian-pdns-db-readonly.json`
`5ce52083aed6dfd3ac092c79ad253bb5ddba83e9820c7cf513ee8902b7d370ec`,
`arch-catalog-axfr.json`
`0643df3ed75be1928be7d7b6d36a66ab83c701e3c76bbd2737e55cf7cfe0be5e`,
`arch-member-udp.json`
`cbc11f824252280ecda5e73b7074f0a750712f10f1f75533faacbfe4014a63b4`,
and `source-member-udp.json`
`a546d468a59cc1c7e356c46b827549e61576a60df446f07efa1dc1d652cbca93`.
Both guests were stopped by QMP `quit` with no fallback signal; their overlays
and evidence were preserved. No process for this trial root remained.
