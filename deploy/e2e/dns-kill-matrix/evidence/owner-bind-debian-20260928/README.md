# Debian owner enrollment resume and archive packaging — 2026-09-28

D-025 invariants 1–4; P0.4/P0.5. No persisted schema changes, installed-panel updates or publication. The tools remain explicit owner actions; extracting an archive does not authorize a peer or change native services.

## Native Debian evidence

The existing exact disposable cell `pdns-switch__intent__after-write__paired-primary__peer-reachable` was recreated. Only its Debian13 guest was used for this enrollment trial, as secondary `192.0.2.10`, with reviewed primary `192.0.2.11` and catalog `catalog-c000020b.celikpanel.invalid`. The test checks the root-owned fixture marker before changing files. Debian packages: BIND `1:9.20.29-1~deb13u1`, OpenSSH `1:10.0p1-7+deb13u4`, sudo `1.9.16p2-3+deb13u2`. This was native BIND/OpenSSH without a running Panel or Agent.

`TestNativeOwnerEnrollmentResume`, compiled with `celikpanel_dns_owner_native`, passed in 0.50 seconds. The [raw output](native-trial.txt) records materialized backup-only and published-SSH checkpoints, restricted-account creation, completed repetition without changing the key, explicit revocation/reauthorization, preserved owner edit and BIND still active. The current stricter account/group and bounded lookup code was included. No actual process kill, power loss, transfer or deletion was performed in this enrollment trial.

The ordinary `dns-peer-enroll` and `bind-peer-inspect` executables came from `make dns-owner-tools` using Go 1.26.5, not an alternate fixture implementation. The [ordinary CLI check](owner-cli-status.txt) independently refused resume after the owner edit; the before/after SSH configuration digest remained `549a13768a565d58c26bdc8b8790538b152643cf47812e7213a4838f4c0df0d7`. The key remained revoked, the account had only its reserved primary group, and BIND was active. Binary hashes and package versions are retained in that output.

## Packaging evidence

The Makefile includes an additive `dns-owner-tools` directory in the ordinary release archive. Source-release staging builds the same two tools; prebuilt staging restores their executable bits only when the optional directory exists, preserving compatibility with older packages. Neither installer nor updater runs enrollment or replaces the independently enrolled inspector. There is no new archive or enrollment schema.

`deploy/test-dns-owner-tools-package.sh` passed using the real Makefile dist recipe, actual newly built DNS tools, and inert unrelated core payloads. It verified exact tool bytes, 0755 executable/0644 guide modes, archive reproducibility under umasks 022 and 077, checksum inclusion, no-argument usage and rejection of a tampered inspector. The test is included in the Go CI job. This is archive integration evidence, not a full candidate build, production signature, native update or rollback test. Shell syntax and the required privileged-launch audit also passed locally.

## Remaining acceptance

Arch and Debian now both have the described explicit resume checkpoints. Complete interruption/concurrent-edit coverage, coordinated rotation and removal, an ordinary end-to-end owner enrollment/valid peer proof on Debian, setup/API admission, independent DNS inverse and full release acceptance remain open. The public paired PowerDNS-primary gate stays closed. The disposable cell was stopped and removed after capture; base images and these small evidence files were preserved.
