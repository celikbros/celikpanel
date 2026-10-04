# Arch forward update and same-process startup observation: AV

D-025 invariants 1/2/3/4; P0.2/P0.3/P0.5 scoped acceptance.
[Machine-readable evidence](PLATFORM-UPDATE-AV.json) is checked by
`verify_platform_update.py`; negative tests reject changed processes, false
completion, wrong operations and lost owner firewall state.

A new nonce/DMI/SSH/QEMU-bound disposable Arch VM installed genuine predecessor
`45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00` (Alpha81). Installation upgraded the
kernel from the cloud image's running 7.1.8-arch1-3 to installed 7.2.6-arch2-1.
An orderly fixture reboot selected the installed kernel before firewall setup.
The historical baseline Agent was explicitly restarted after systemd was ready
to avoid its known cached-platform refusal. That baseline precondition is not
counted as evidence of same-process recovery in the corrected candidate.

Source `b60da4a18b29fde0c6d7e875285378bb73c1573e` was built as fixture commit
`435c9a901a13fdf64b39d87572b64687fbafbb3f`; its only fixture change is the release
sequence policy (82, exact Alpha81 predecessor). Archive SHA256:
`942ef20536e4f301c477665efb6fc8abb199ac11976dbe8df0520cfa6be7f17b`.
Isolated signing trust and loopback HTTPS origin exercise the actual authenticated
Agent Check/Start API and detached worker. No production tag, key or owner server
is involved. This does not certify browser update-start admission.

Operation `eeb7d67fb3c0449e098d0f6ec9d56d83` reached `update_verified` at
2026-09-22T03:19:35Z. Exact candidate binaries and independent firewall unit/helper
were published. Existing enabled state, saved policy and unrelated native table
were preserved. Agent/Panel were active and HTTPS returned 200.

For the subsequent orderly reboot, a disposable oneshot unit delayed
`multi-user.target` for 180 seconds. It changed no product service or operation
receipt. The private fixture origin was available across boot. The read-only
`--mode check` driver validates QEMU nonce/DMI and authenticates the local Agent;
it cannot create an update request or persist a review. The observation collector
checks Agent process identity before and after that RPC.

In boot `28d2e988-ac43-472e-a169-1cd63731bd84`, Agent PID 444, start ticks 710,
invocation `fe9ce54e0cdc4d3b89a59ebdaaa127e1` first returned `supported=false` with
specific starting guidance. After the native barrier completed, **the same process**
returned `supported=true`, no error and no new available update. No Agent restart,
receipt repair or repeated Start occurred. Independent terminal status, exact
firewall artifacts, policy/tables and HTTPS also passed after this distinct boot.

This closes successful Arch forward update/boot for this fixture and the scoped
native stale-platform-observation acceptance. It does not close P0.2/P0.3/P0.5,
production trust/UI admission, every checkpoint/power-loss boundary, failed-helper
console recovery or the remaining independent workload/renewal matrix.
