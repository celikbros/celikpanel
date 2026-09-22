# Independent firewall unit: native update and automatic rollback

D-025 invariants 1, 3, 4 / P0.3 and P0.5. [Machine-readable evidence](FIREWALL-UPDATE-AS-AT-AU.json)
records two **disposable Debian 13** trials. Source `8043605929111685b0c3d239076c2afe049081aa`
adds the packaged independent unit and destination-helper verification. Local,
unpublished fixture commit `971e60b0f4aba47106c64cae220d5a0a32619c0f` changes only
the test release policy to sequence 82 with exact Alpha81 predecessor
`45dfc265bfd7997e9a0e0d39b0ebf60a57f58c00`.

The archive SHA256 is `c15eecf925aaa82f76cc8799094bcec7814151a681bd8007d9b89025107d4aa4`.
Both guests use new isolated fixture signing keys and a guest-loopback HTTPS
origin. The real installed Agent verifies the signature, sequence floor and
archive and starts its ordinary detached worker. No production signing key,
release tag, browser admission or installed owner server is involved. The host
pins QEMU process, SSH key and nonce; the guest pins DMI UUID and systemd identity.
The archive reader reconstructs artifact-v1 from the committed template rather
than exempting arbitrary generated files from source proof.

## Successful update and boot: AS

The genuine previous-version installation and its binary/service checks passed.
A fixture v2 policy allows TCP 22/2083, UDP 53 and preserves a separate native
`celikpanel_lab_other` table. Operation `ce86b00cb23ff0d0ce21dc7024d2bf13` completed
through the actual update body with `update_verified`; both management services
and HTTPS returned. The exact independent helper and unit were published and
both policy and native tables remained unchanged.

The AS baseline firewall unit was disabled, as the snapshot confirms. Update
preserved that owner state. For the subsequent boot experiment, the fixture
explicitly enabled the unit **after** update and recorded this separate action.
The boot `3e0923de-710c-4c23-95b3-c5a01ac37761` verified the native helper,
unchanged tables/policy and fresh SSH/HTTPS. This does not claim automatic
activation of a previously disabled boot unit.

## Actual interruption and automatic rollback: AT

The second genuine Alpha81 baseline had its firewall boot unit explicitly enabled
before update. A read-only checkpoint observer waited for operation
`64c20d1647537aaf5a51d8ba81a8aa70` to publish **both candidate binaries and the
candidate firewall unit with its exact retained helper**. It verified the complete
snapshot, production observation/binding, original worker executable, PID start,
invocation, cgroup and exclusive transaction ownership while freezing that exact
worker. Only then did it send SIGKILL to that worker's cgroup.

The native independent recovery service automatically restored the original
application payload and Agent-backed firewall unit. No controller rollback call
or product-receipt edit was made. The same operation reached `rollback_verified`
while preserving `previous_failure=update_failed`. The new independent helper
remained intact for later use; policy, both native tables and boot enablement
were preserved. Panel, Agent and HTTPS returned. Boot
`835caff0-2ec9-4a1b-9cef-0f1369a95e75` again verified the restored state.

## Evidence and limits

`verify_firewall_update.py` checks the combined evidence; negative tests reject
stale boot IDs, different operations/guests, false completion, erased failure,
missing or altered helpers, wrong unit/payload, policy/table changes and lost
HTTPS. The fault observer's tests reject incorrect checkpoint identities and
changed helper bytes before any kill. Neither reader has product mutation authority.

The first candidate build had an archive/version-policy mismatch and was refused
before any update start. AS also had an initial malformed unrelated-table fixture;
its failed text was retained and corrected before admission. Neither is counted
as successful evidence. All private logs/keys/nonce remain in the lab roots;
public evidence contains bounded hashes, operation and VM/boot identities only.
All three labs were stopped with disks and evidence retained.

Open: production-trust/browser admission, successful forward update/boot on Arch, power loss, damaged
helper emergency-console recovery, native owner-edit races, semantic policy-reader
version migrations, direct source-checkout install, and the remaining independent
renewal/workload matrix. Earlier A/B/A tests establish management-absent boots
for the bundled unit; these trials establish the actual update/automatic rollback
body. Together they remain bounded evidence, not complete P0.5 or removal support.


## Arch automatic rollback and boot: AU

The same candidate and genuine Alpha81 baseline passed the exact candidate-unit
publication SIGKILL boundary on Arch. Operation
`ac21d74e16738afbcc3d090ebdc7ebca` reached `rollback_verified` automatically at
2026-09-22T02:27:20Z, preserving `previous_failure=update_failed`. The original
application binaries and Agent-backed firewall unit were restored. The immutable
new helper remained, both native tables and policy bytes were unchanged, and
Panel/Agent/HTTPS returned. A separate boot
`1bdd5053-f8a2-44bb-9f13-86ecfec34207` preserved that verified result and enabled
firewall state. The combined evidence validator now checks three actual boots.

Two preliminary conditions are retained rather than counted as product success:
the fresh fixture had updated its kernel package while running the older kernel,
so nftables required a normal reboot before admission. Later the Alpha81 Agent
refused admission with a cached `host package-manager policy could not be verified`
result; independent read-only detection passed. Restarting only that fixture Agent
allowed ordinary preview to pass. A new explicit operation was then admitted;
the earlier observer expired without killing anything. No product receipt, signing
floor or admission check was changed. The cached transient platform refusal is a
separate source correction, not resolved by this acceptance result. This Arch
case proves interrupted update/automatic rollback/boot, not successful forward
update or production browser admission.
