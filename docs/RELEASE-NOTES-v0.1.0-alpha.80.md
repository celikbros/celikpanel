# v0.1.0-alpha.80

[Türkçe](RELEASE-NOTES-v0.1.0-alpha.80.tr.md)

Ordinary BIND zone publication changes the current generation while preserving the engine acquisition record. Updates and setup finalization previously treated that legitimate difference as an ownership conflict. They now retain current publication state and verify the live generation and runtime configuration, while keeping acquisition identity, pair authority and catalog progression checks. Ownership records are not rewritten to conceal discrepancies.

The signed updater checks settled BIND compatibility before publishing a service start barrier, then repeats the check under the mutation lock before freezing coordinators. Existing recovery and migration paths retain their final checks. This early check reduces avoidable panel interruption; it is not a guarantee that every later installation failure is predictable.

Automatic rollback now parses Linux lock fields with variable whitespace, matching the update entrypoint. Previously, a valid inherited exclusive lock could be rejected and leave recovery waiting. Descriptor identity, exclusive ownership and independent exclusion checks remain enforced.

Regressions cover actual BIND record creation and editing, current-tree corruption and ownership changes, the early shell failure path, and both update and rollback lock entrypoints. The original Alpha79 code fails the reported BIND and lock cases. These are local Linux tests; they are not a complete two-server upgrade or hosted-workload audit.

The owner successfully restored Frankfurt using its verified pre-update snapshot, and an independent HTTPS check confirmed panel access. See [incident evidence](UPDATE-TLS-RECOVERY-INCIDENT.md). Install this release through CelikPanel's update interface. Publishing it does not update installed servers or resolve external mail PTR requirements.
