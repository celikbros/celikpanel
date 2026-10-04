# Independent firewall generations: Debian AR native evidence

This is bounded P0.5 native boot/retention evidence. It is not an installed-panel
update, automatic application rollback or complete workload-independence result.
No Frankfurt/Boston server was accessed. Only the registered disposable AR QEMU
Debian 13 guest was used, with pinned SSH identity, nonce, DMI UUID and systemd
checks. The lab was repurposed after its earlier recovery acceptance.

## Material and preparation

[FIREWALL-GENERATIONS-AR.json](FIREWALL-GENERATIONS-AR.json) records source commits,
Go 1.26.5 build settings, exact helper/unit/manifest hashes and three observations.
The two generations are stripped and unstripped builds of the **same audited
helper source**, not different semantic policy-reader versions. The preparation
CLI is the actual compiled recovery entry, SHA256
`3e69ea2e78d639033b247345df71b1ea02171d6e87d04937230dee4ca081d405`.

Both payloads were prepared by `prepare-firewall-runtime` under the inherited
exclusive native release lock, before active transaction markers existed. The
normal Panel and Agent executables were already absent and services disabled.
Preparation left the installed unit, saved policy and both kernel tables unchanged.
It retained both generations below `/usr/libexec/celikpanel/firewall/`.

The fixture then installed the exact bundled native unit, including its real
hardening, network-pre ordering, shared exclusion directory and independent
ExecStartPre/ExecStart paths. This is a controlled fixture unit transition;
normal installer/update unit activation is not established by this exercise.

## Three successful native boots

1. Generation A: `2a255340de0cef05addfdd4e8dd36a1197bec2c370c69eda512c3d250c4c8de2`.
   Boot `826a7ca6-3666-4f57-a6f6-c276767d9db5`.
2. Generation B: `1fdf7a12c11905e044686098369dab470752b2ef4faab4a1dbb89f6584835393`.
   Boot `3cde9576-e469-4fc8-ac5b-a2422dbca914`.
3. Restore the exact retained A unit and reboot again:
   `59019748-ff2d-4d9a-ac3f-538ba3dc8e0f`.

Each observation shows the actual unit active/exited/success and enabled,
management services inactive/disabled, both management executables absent,
unchanged root-owned helper bytes/modes for both generations, unchanged saved
policy, restored default-drop rules allowing SSH 22/panel 2083 and UDP 53, and
unchanged unrelated `celikpanel_lab_other` table. Collection required a fresh
pinned/guarded SSH connection after each reboot. Returning to A did not remove B.

The first preliminary A boot passed the native unit and SSH checks, but the
unrelated table fixture had not been enabled independently of the old fixture
unit. That incomplete observation is retained privately as `preliminary-a.txt`;
it is not one of the three successful results. The unrelated fixture service was
explicitly enabled before repeating A and proceeding to B/A. No consumer or
publication validation was weakened to make the fixture pass.

## Verification and limits

The read-only `guest_firewall_generation.py` collects the bounded observations.
`verify_firewall_generations.py` verifies the committed record; its tests reject
missing/stale boots, management dependency, failed unit state, changed policy or
other table, missing/changed retained helper and foreign guest identity. These
checks do not convert recorded observations into current live health.

Open: normal signed UI update admission and exact automatic rollback body with
this unit, power-loss durability, damaged-helper boot/console recovery, owner
edits across actual update/rollback, semantic reader migrations, Arch counterpart
and other workload/renewal combinations. Installed-unit activation remains a
separate source/integration step. The prior Agent-backed installed unit remains
the product default until that step is implemented and validated.
