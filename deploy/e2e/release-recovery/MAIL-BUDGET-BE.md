# Selected mail recovery attempt budget: Debian BE

Consumer source `f78d57fdf609389c005d5583e5ad9551c2ca2910`.
The real SIGKILL producer is the previously verified
`7dcd9fc72f2e543f974389f02ce36e7d81be7535` test artifact in disposable cell
`release-recovery__3688d130a0721786`. P0.3/P0.5, invariants 1, 2 and 4.
[Bounded evidence](MAIL-BUDGET-BE.json) is checked by
`python3 verify_mail_recovery_budget.py MAIL-BUDGET-BE.json`.

The producer selects a seventh fixture-CA source and is actually killed before
reload/completion. The independent helper is installed as a new immutable kit by
an explicit fixture action; predecessor kits remain retained. Installed Panel
and Agent binaries stay absent. No production enrollment path is claimed.

An explicit fixture-owner Dovecot unit override makes its native reload command
return failure while the real mail daemons remain active. The first helper
recovery records attempt 2 before reload and retains the same active intent and
queue on failure. A real reboot changes boot ID from
`1627ab8d-b4d0-4e30-9811-4f4b250de357` to
`47c9fa4d-0d12-4533-9b79-59a638fe4673`. The native timer automatically invokes the
same installed consumer and records failed recovery attempt 3. The original
operation ID is `37d0b8e49a8515f0adc6fd4b91f15d06` throughout.

Another automatic-mode invocation refuses at the budget boundary. A separate
explicit retry for a different request is also refused. Both preserve exact
ledger and queue bytes, native configuration and the two units' prior ExecReload
observations. The refusal includes the original request and owner continuation.
After explicit fixture-owner removal of the exact injected override (retained as
evidence), `--retry-selected` for the correct request admits one attempt, number
4. It reloads successfully, persists the same operation's completion and then
acknowledges the queue. Both trusted SMTP/IMAP listeners serve the expected leaf;
configuration bytes/modification times and foreign ledger history remain intact.

The existing canonical v1 ledger's attempt field is reserved with recovery
intent. There is no reset, new request, new privilege, or inferred historical
attempt count. Full Agent race tests passed (196.219 seconds), vet and helper-tag
policy tests passed; 44 mail evidence verifier tests pass with this record.

This proves the selected-operation independent retry boundary. It does not
prove production enrollment, earlier pre-selection retries, ordinary Agent
recovery policy, public ACME, power loss, or completion of P0.3/P0.5 in full.
