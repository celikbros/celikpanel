# CelikPanel v0.1.0-alpha.78

[Türkçe](RELEASE-NOTES-v0.1.0-alpha.78.tr.md)

Updates now accept a strictly validated managed panel certificate tree, with or
without the original bootstrap certificate pair. Previously this supported
layout was rejected after the panel and agent had stopped. Certificate bytes,
ownership, modes and links remain unchanged; unsafe layouts still block updates.

The automatic recovery entry point now parses Linux lock information by fields.
Variable column spacing no longer rejects a valid inherited exclusive lock.
Descriptor ownership, inode identity and independent exclusion checks remain.

The incident-specific Frankfurt recovery tool and tests are retained for audit.
It is not a general repair command and must not be rerun on the recovered server.
Its readiness check now waits for the exact executable and stable service PID.

Validation covers atomic and mixed TLS layouts, rejection without mutation,
six real Linux lock cases, and eighteen incident recovery/startup scenarios.
Existing bootstrap update and recovery contracts are included. These checks do
not establish that every installed-server configuration has been tested.

Owners initiate installed-panel updates from the panel update interface.
See the [incident record](UPDATE-TLS-RECOVERY-INCIDENT.md) for evidence and limits.
