# Disposable native BIND inspector channel

This fixture tests the optional read-only proof channel in the exact
`bind__intent__after-write__paired-primary__peer-reachable` QEMU pair.
The Arch guest is the primary at `192.0.2.11`. The Debian guest is a
panel-free BIND secondary at `192.0.2.10`. It is **not** a production
enrollment tool and does not turn a response into an accepted deletion.

First start the existing paired cell, wait for both guests, and prepare
`native_bind_peer.py` as described in [README.md](README.md). Install and
run the primary's paired BIND baseline. Build the inspector for Linux from
this checkout; for example, in a Linux environment:

```sh
go build -o /var/tmp/bind-peer-inspect ./cmd/bind-peer-inspect
python3 deploy/e2e/dns-kill-matrix/native_bind_inspector_channel.py enroll \
  --work-root "$ROOT" --cell-id "$CELL" \
  --identity-file "$HOME/.ssh/id_ed25519" \
  --inspector /var/tmp/bind-peer-inspect --execute
```

Enrollment checks both exact guest markers and peer addresses before editing
either guest. It reads the secondary's host public key through the fixture's
management channel, creates a dedicated key, installs a root-owned native
policy, fixed SSH forced-command wrapper, no-argument sudo rule, and root-only
client credential on the primary. The only authorized client source is the
primary's peer-network IP. The secondary has no Panel or Agent. The helper
refuses to overwrite an existing local fixture key. The root-owned wrapper
checks the exact SSH original-command token **before** sudo clears that
environment variable. The channel rejects an extra command, an untrusted host
key, and a different client credential.

To supply the production Agent in this disposable guest with the same pinned
peer identity, run `agent-enroll` after `enroll` and before the deletion RPC:

```sh
python3 deploy/e2e/dns-kill-matrix/native_bind_inspector_channel.py agent-enroll \
  --work-root "$ROOT" --cell-id "$CELL" \
  --identity-file "$HOME/.ssh/id_ed25519" --execute
```

This writes the canonical v1 enrollment under
`/var/lib/celikpanel-agent-private/dns-peer-inspection-v1.json` and a root-only
client key under `dns-peer-inspection-keys/`. It refuses to replace either.
This fixture operation is not an enrollment procedure for an installed panel.

For a loaded member at catalog serial 1, `probe` should report
`member_state=present` and `native_state=loaded`; it must not report native
absence. After the fixture's separate native owner catalog edit transfers
serial 2 and unloads the member on the secondary, a fresh `probe` with
`--catalog-serial 2` and no `--catalog-member` should report
`transferred/absent/unloaded`. Supply fixture request and owner IDs and the
actual V3 qualifier. For example:

```sh
python3 deploy/e2e/dns-kill-matrix/native_bind_inspector_channel.py probe \
  --work-root "$ROOT" --cell-id "$CELL" \
  --identity-file "$HOME/.ssh/id_ed25519" \
  --request-id "$REQUEST_ID" --owner-id "$OWNER_ID" \
  --qualifier "$V3_QUALIFIER" --generation "$GENERATION" \
  --catalog-serial 2 --attempt 1 --execute
```

The probe sends a fresh 32-byte nonce through the Arch primary over direct
peer-network SSH with a pinned Debian host key. It checks canonical response
bytes, request digest, nonce, attempt, serial, member digest and zone. It
also requires refusal of an untrusted host key, altered original command and
wrong client credential. A positive response establishes only this native
channel observation. The helper deliberately prints
`accepted_deletion_proven=false` because current operation authority,
durable consume-once replay protection and the primary's source-bound
catalog/NoTransfer proof belong to the Agent.

The [dated disposable report](NATIVE-BIND-INSPECTOR-CHANNEL-20260926.md)
records the loaded-zone non-success result, three SSH denials, exact guest
markers and artifact hashes. Its later same-operation recovery reached a
terminal V3 deletion after the secondary transferred serial 2 and unloaded
the member. A terminal replay was refused, and the native BIND state survived
reboot with management disabled. These are bounded results for one cell.
Process-restart and owner-edit races, other engine combinations and the wider
Stage 2 matrix remain open. Do not use this fixture against an installed server.
