# Control-plane disaster recovery

Status: **design, 3 September 2026** (risk register R-003). Nothing described
here ships yet; this document is the contract the implementation and the drill
will be measured against. Engineering documents are English only (D-022).

## 1. The problem in one sentence

Today the product backs up domains. It does not back up itself. A host that dies
keeps its domain archives and loses the panel: the SQLite database, the key
that seals every stored secret, the DNS engine receipts and mutation ledger, the
DKIM and VPN keys, the panel's own TLS material. The roadmap already states the
consequence: losing `secret.key` makes every sealed secret irrecoverable, and
"domains in backup but the panel's brain missing" is not accepted.

## 2. Inventory: what the panel's brain is

Every path below is read on the live host, verified by owner and mode as the
product already verifies it, and copied into one archive. Nothing else on the
host is control-plane state.

| Component | Path | Owner / mode today | Why it is irreplaceable |
| --- | --- | --- | --- |
| Panel database | `/var/lib/celikpanel/celikpanel.db` (+ `-wal`, `-shm` while running) | celikpanel:celikpanel 0600 | Every domain, user, plan, zone, certificate record, audit log |
| Secret key | `/var/lib/celikpanel/secret.key` | celikpanel:celikpanel 0600 | Seals every secret stored in the database; lost key = unreadable secrets |
| Panel configuration | `/etc/celikpanel/panel.env` | root:celikpanel 0640 | Listen address, data directory, feature switches |
| Agent token | `/etc/celikpanel/agent.token` | root:celikpanel 0640 | Panel-to-agent authentication |
| Agent private state | `/var/lib/celikpanel-agent-private/` (ledger `service-mutations.json`, `dns-engine-state.json`, `dns-engine-ownership-*.json`, `dns-engine-install-ownership-*.json`, firewall and mail journals, panel-certificate activation state) | root:celikpanel 0700 dir, 0600 files | The durable truth of which DNS engine owns the host and what was mid-flight; without it the restored host cannot prove its own engine |
| DKIM keys | `/var/lib/celikpanel-dkim/keys/` | root:opendkim 0750, keys 0640 | Published in DNS; losing them breaks every domain's mail signing |
| WireGuard | `/etc/wireguard/` | root:root 0700 | VPN identity and peers |
| Panel TLS | `/var/lib/celikpanel/tls/` | celikpanel:celikpanel 0700 | The panel's own certificate and key; the "protected initial certificate" |
| Firewall snapshot | `/etc/celikpanel/firewall.nft` | root:root 0600 | The exact ruleset restored on boot |

Excluded on purpose: `/var/lib/celikpanel-agent-private/system-sqlite-snapshots`
(transient, 5-minute TTL), `/var/backups/celikpanel/update-snapshots` and
`recovery-snapshots` (release transaction artefacts with their own lifecycle),
domain content and domain backups (already covered by domain backups), and
anything a package manager reinstalls (binaries, vendor units, vendor configs
the product rewrites deterministically from the database).

## 3. Consistency: how the database is copied while the panel runs

The update path already has the primitive: `copySQLiteDatabaseOnline` in
`cmd/panel/service_operation_snapshot.go` opens the source read-only in WAL
mode and produces a standalone copy that is then normalised, schema-verified
against the shipped migrations and integrity-checked
(`createServiceOperationSnapshotWithCopyAndVerify`). The disaster archive uses
exactly that function; it does not invent a second copy mechanism. The copy is
taken first, then the small files, then the copy is verified once more against
the live `sqlite_master` so a migration that landed between the two reads is
detected and the archive is retried, not shipped.

## 4. The archive

- One file: `celikpanel-control-plane-<host>-<UTC timestamp>.cpbak`.
- Inside, before encryption: `manifest.json` (schema version, panel version and
  commit, host name, creation time, and for every member its path, owner, mode,
  size and SHA-256), the members under their absolute paths, and
  `manifest.sha256`.
- Encrypted with a **backup key that is not `secret.key`**. The key is generated
  once when the feature is enabled, shown to the operator once, never stored on
  the host in plaintext, and never written to the database. The screen says in
  plain words: "Without this key the archive cannot be opened, by us or by
  anyone." Envelope (D-023, decided 3 September 2026): the product generates
  a random 256-bit key and prints it once as `cpk1-…` (Crockford base32 in
  4-character groups); the archive key is derived from it with argon2id
  (parameters in the archive header, random 16-byte salt) and the payload is
  AES-256-GCM in 64 KiB chunks with the age STREAM nonce scheme, the header
  as associated data. argon2id and AES-GCM are already in the product; no
  new dependency. A wrong key or a flipped bit fails before any member is
  placed.
- Written under `/var/backups/celikpanel/control-plane/` as root 0600, fsynced,
  then optionally pushed to the same remote target and retention as domain
  backups (v2; see §7).

## 5. Restore

Restore is a fresh-host operation and nothing else. It runs before the panel
has any state of its own, so there is never a merge and never a second identity.

1. Install the same or a newer release on a clean host through `install.sh`.
2. Slice 1 (on the branch): three one-shot modes of the panel binary, run as
   root: `--generate-control-plane-key`,
   `--create-control-plane-archive=<path> --control-plane-key-file=-` and
   `--restore-control-plane-archive=<path> --control-plane-key-file=-`. The
   key arrives on stdin with the same discipline as the first-administrator
   credentials (root-only regular file or a bounded pipe).
   Slice 2 provides the archive and the backup key at first run: the install script
   accepts `CELIKPANEL_RESTORE_ARCHIVE=/absolute/root-only/file` and reads the
   key the same way it reads the first-administrator credentials today
   (root-only file, inherited on stdin, consumed); the panel's first-run screen
   offers the same choice for operators who install interactively.
3. The panel binary performs the restore itself, as root, with both services
   stopped: verify the manifest, verify every member's digest, refuse an archive
   whose schema is newer than the binary, place each member with its recorded
   owner and mode, run the migration check against the restored database, then
   start the agent and the panel.
4. After start the agent's normal startup recovery runs against the restored
   private state. That is deliberate: a host restored mid-switch must be
   treated exactly as a host that rebooted mid-switch, and R-019, R-031, R-032
   and R-033 have made that path honest.
5. What restore does **not** do: reinstall DNS engines or mail servers. The
   restored database knows which engine owned the host; the first thing the
   operator sees is the DNS infrastructure screen saying so and offering the
   install. Domain content is restored from domain backups afterwards through
   the existing flow.

## 6. The drill (exit criteria for R-003)

Run first on a WSL2 guest, then on a disposable real VM by the team.

1. Fresh host A: install, add an administrator, activate BIND, create a domain
   with mail (so DKIM keys and a sealed secret exist), enable the VPN, take a
   control-plane archive.
2. Destroy A. Fresh host B with the same release: restore from the archive.
3. Prove on B, through the panel only: the administrator logs in with the old
   password; the sealed secret decrypts (the DKIM private key and a stored
   database password read back identical); `dns-engine-state.json` names BIND
   at the same epoch; the DNS infrastructure screen offers the install and the
   install completes; the zone answers with the same DKIM record; the VPN peer
   list is intact; the panel TLS fingerprint is unchanged.
4. Record RPO (archive age) and RTO (wall time from install start to serving)
   as measured, not estimated.

## 7. Not in this version

Remote targets and retention for the control-plane archive (they reuse the
domain-backup target once that target exists), scheduled control-plane
archives (v1 is on demand plus one before every update), and key rotation.
Each is a register entry of its own when it starts.

## 8. Open decisions

- D-023 is settled (see §4); the generated key is typed or pasted into the
  fresh host, nothing else is needed.
- Whether the pre-update release snapshot should simply become a control-plane
  archive, retiring one of two mechanisms.
