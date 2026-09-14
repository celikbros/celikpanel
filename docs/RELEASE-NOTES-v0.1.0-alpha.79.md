# v0.1.0-alpha.79

[Türkçe](RELEASE-NOTES-v0.1.0-alpha.79.tr.md)

Panel updates now preserve the certificate issuance receipt retained in managed TLS version directories. Alpha78 rejected this legitimate fourth file, including in an older certificate directory after renewal, and could stop an update during snapshot capture after stopping the panel and agent.

The snapshot validator accepts only the named, nonempty receipt with root:root ownership, mode 0600, one link and a maximum size of 1024 bytes. Snapshot manifests and restoration preserve its exact bytes and metadata. Unknown entries and unsafe receipts remain rejected.

Validation includes the real Go issuance and renewal writers followed by the shell snapshot and restore path, unsafe receipt cases, recovery transaction tests, and the narrowly scoped owner-operated Frankfurt recovery helper. The old validator fails the real-writer regression. These checks are not a complete two-server upgrade or hosted-workload audit.

Frankfurt was recovered to its existing Alpha75 installation before this release; no update was installed by recovery. The incident-specific helper must not be reused as a general recovery command. See [incident evidence](UPDATE-TLS-RECOVERY-INCIDENT.md).

Install updates through CelikPanel's update interface. This release does not resolve external mail PTR prerequisites or reset setup plans.
