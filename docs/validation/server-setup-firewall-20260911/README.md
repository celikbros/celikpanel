# Server setup firewall acceptance — 2026-09-11

*[Türkçe](README.tr.md)*

Passed on fresh, disposable QEMU images for Debian 13, Ubuntu 24.04 and Arch
Linux. All targets used localhost-only SSH forwarding. No installed panel was
updated; the fixtures contain the candidate privileged agent and a guarded Go
test executable, with no panel application binary.

The test exercises `runServerSetupFirewall` with a signed test-only license and
its actual authenticated agent RPC, then the panel's ordinary Turn off handler.
The driver is `deploy/test-server-setup-firewall-vm.py` and the gated test is
`TestServerSetupDisposableVMFirewall` in `cmd/panel/server_setup_vm_test.go`.

| Evidence | Debian 13 | Ubuntu 24.04 | Arch Linux |
|---|---|---|---|
| No snapshot: disabled before and after real reboot | Passed | Passed | Passed |
| Reviewed setup enables and saves the firewall | Passed | Passed | Passed |
| Same child request reconciles without a second mutation | Passed | Passed | Passed |
| Saved policy restored unchanged after real reboot | Passed | Passed | Passed |
| Turn off removes snapshot and disables restore unit | Passed | Passed | Passed |
| Following reboot remains off | Passed | Passed | Passed |
| SSH reconnects after each actual reboot | Passed | Passed | Passed |

Each OS directory contains the successful stage logs, before/after boot IDs,
candidate binary SHA-256 hashes and saved-policy SHA-256. The exact saved snapshot
hash is `789f4fbf006df6db821614b137c85e73541fea968ab34e0cbf8e1bb30a1d733b`.

The real test exposed an initial retry defect: automatically protected SSH ports
appeared in the live policy and incorrectly invalidated an identical reviewed
request. The runner now reconciles its exact succeeded receipt first and treats
freshly proven SSH ports separately from new service-policy requirements. The
successful evidence was collected after rebuilding that correction.

Arch also required a preparatory reboot after the package update replaced its
kernel modules. The existing kernel-readiness guard correctly refused nftables
before that reboot; no guard was bypassed.

This is a firewall/setup-child acceptance result. It does not claim end-to-end
ACME issuance, all purpose profiles, remote DNS pairing, a full fresh installer,
installed-panel updates or release rollback acceptance. Those have separate
requirements before publication.
