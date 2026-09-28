# Local owner enrollment for optional BIND peer inspection

Optional Linux owner tools. Current source packaging includes `dns-owner-tools/dns-peer-enroll`, `bind-peer-inspect` and this guide in the release archive. This source change has not been published as a release. The secondary must already run native BIND, OpenSSH and sudo. This tool configures only an optional read-only deletion-proof channel. It does not configure AXFR, create an RNDC key, start a panel, or make a blocked DNS topology available.

Use the directory from a release whose signed archive has been verified through the normal CelikPanel distribution path. On a server without CelikPanel, the owner can extract the verified archive to a root-owned directory and use only `dns-owner-tools`; do not run `install.sh` to enroll a native DNS peer. Packaging does not install or run the tools, change SSH, enroll either host, or replace an already enrolled inspector. Existing inspector files outside CelikPanel remain subject to the explicit owner review described below.

For a local developer build, run `make dns-owner-tools GO=/absolute/path/to/the/reviewed/go`. The result is in `bin/dns-owner-tools`; it is an unsigned build, not a trusted published release. Run the CLI by absolute path (shown as `dns-peer-enroll` below for brevity). Supply the absolute path of the matching packaged `bind-peer-inspect` with `--inspector`.

The primary owner runs `primary-prepare`, then gives only the returned public key to the secondary owner. The secondary owner saves that reviewed public key in a root-owned regular file and supplies a reviewed inspector executable. Example documentation addresses:

```sh
sudo dns-peer-enroll secondary-install \
  --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 \
  --catalog catalog-c000020a.celikpanel.invalid \
  --primary-public-key /root/primary.pub \
  --inspector /root/bind-peer-inspect
sudo dns-peer-enroll secondary-host-key
```

The primary owner independently checks the returned Ed25519 host-key digest, then uses `primary-activate` with the prepared credential ID, reviewed primary/peer/catalog and `--host-key-sha256`. Both status commands report local configuration; only the exact DNS operation's authenticated observation can prove a deletion complete. An ordinary status poll does not restart a mutation.

If secondary installation was interrupted, run `secondary-status`, then explicitly run `secondary-resume` with the same flags as `secondary-install`. Exact staged policy, wrapper, binary and SSH configuration can be reused; a locked dedicated account can be reused only with its reserved primary group and no supplementary groups. The resume command accepts only the recorded original SSH file or its exact published replacement. It preserves owner changes and reports them for local review. An existing different authorized key is never replaced. If an authorized channel is now unverified, use `secondary-revoke` before reviewing it. Resume with no authorized key is an explicit authorization of the supplied public key, so do not resume an intentionally revoked enrollment unless reauthorization is intended.

`secondary-revoke` disables the fixed authorized key without stopping native DNS or removing owner SSH configuration and evidence. The primary owner also runs `primary-revoke`. No automatic file deletion, account removal or coordinated rotation is provided. Native tested boundaries and remaining limitations are recorded in the source-tree report `deploy/e2e/dns-kill-matrix/evidence/owner-bind-resume-20260928/README.md`.