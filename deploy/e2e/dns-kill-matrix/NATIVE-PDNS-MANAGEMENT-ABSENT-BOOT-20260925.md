# Native PowerDNS serving after management-absent reboot — 2026-09-25

This is a bounded P0.5 / D-025 invariants 1 and 6 acceptance record on a **fresh disposable Debian 13 guest**, with a separate disposable Arch controller. It tests native DNS serving through one orderly reboot after the management processes are stopped and their standard executable paths are absent. It does not certify removing all CelikPanel files from a hosting server.

## Boundary and result

The fixture adopted an existing external PowerDNS authority with one active parent zone and one frozen deleted child. The intent/after-write injection killed the switch process with exit 137. The independent quiesced reader then verified the active parent and 1/1 deleted child over authoritative UDP and TCP. The ordinary Agent retried the **same request** and reached `target_converged`; 31/31 post-recovery samples were healthy.

For the boot slice, a source-built native Go DNS probe passed the active parent SOA and strict negative deleted-child SOA cases over UDP and TCP. Both `celikpanel-panel.service` and `celikpanel-agent.service` were disabled and stopped. Their executables were moved from the normal `/opt/celikpanel/bin/{panel,agent}` paths to `.offline` names within the **disposable guest**. The guest was rebooted. Its boot ID changed from `068b4a24-0229-463b-b4ac-541a71495687` to `21e6eea2-41e3-46fd-85ce-a9730fafa430`. On the new boot, those management units were inactive, the normal executable paths were absent, and native `pdns.service` was enabled and active (MainPID 644). The same direct UDP/TCP DNS probe passed again.

The observed native configuration hashes after reboot were:

- `/etc/powerdns/pdns.conf`: `8b46927e48be83b2f0afc38efabdc2e2237daa5281b1a07ea44989ffa41f262a`
- `/etc/powerdns/pdns.d/celikpanel.conf`: `1d2fe25bde12a21bca513d8eefef617116601da8586e6a5ad2ffe4f3dd4914ca`

The corrected ordinary Agent executable was SHA-256 `48896be2fd1de217001023d9d56aaa02800a4d0e66e725f0163a7a8024c3fe19`. The test probe executable was SHA-256 `993f659f019acd8928a54a39647446019d0a129482115dd7c7f4949195305d32`.

## Evidence and limits

Preserved local bundle: `/var/tmp/cp-pdns-absence25/evidence-final.tar.gz`, SHA-256 `75ed9a31c37c2d782a3ce8809c352e3ab840e37ac814b46424498c045e5b8c59`. It contains the nested adoption-cell evidence, before/after direct DNS probe output, boot IDs, management/service state, artifact hashes, and the exact disposable runner. Both guests were stopped and the test cell torn down after collection.

No persisted DNS state, journal, ledger or receipt schema was changed by this trial. Recovery was same-request Agent-mediated forward convergence before the reboot; no independent inverse was executed or exposed. The normal management executable paths were absent during reboot, but their bytes and other CelikPanel files remained on the guest. This proves one standalone managed PowerDNS serving slice through an orderly reboot. It does **not** prove paired primary/secondary transfer, other hosted workloads, certificate renewal, owner-edit handling, crash/power-loss behavior, complete panel removal, or the full P0.5 matrix. P0.4 and P0.5 remain open.