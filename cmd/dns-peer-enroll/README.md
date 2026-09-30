# Local owner enrollment for optional BIND or PowerDNS peer inspection

Optional Linux owner tools. Current source packaging includes `dns-owner-tools/dns-peer-enroll`, `bind-peer-inspect`, `pdns-peer-inspect` and this guide in the release archive. This source change has not been published as a release. The secondary must already run native BIND (`named.service`) or PowerDNS (`pdns.service`), OpenSSH and sudo. This tool configures only an optional read-only deletion-proof channel. It does not configure AXFR, create an RNDC key (a BIND installed by CelikPanel creates one at install when the package did not; a panel-free BIND relies on the package or the owner), start a panel, or make a blocked DNS topology available.

Use the directory from a release whose signed archive has been verified through the normal CelikPanel distribution path. On a server without CelikPanel, the owner can extract the verified archive to a root-owned directory and use only `dns-owner-tools`; do not run `install.sh` to enroll a native DNS peer. Packaging does not install or run the tools, change SSH, enroll either host, or replace an already enrolled inspector. Existing inspector files outside CelikPanel remain subject to the explicit owner review described below.

For a local developer build, run `make dns-owner-tools GO=/absolute/path/to/the/reviewed/go`. The result is in `bin/dns-owner-tools`; it is an unsigned build, not a trusted published release. Run the CLI by absolute path (shown as `dns-peer-enroll` below for brevity). Supply the absolute path of the matching packaged `bind-peer-inspect` (BIND) or `pdns-peer-inspect` (PowerDNS) with `--inspector`.

Every subcommand accepts `--engine bind|pdns`. The default is `bind`, so the BIND commands below are unchanged. For a PowerDNS secondary, pass `--engine pdns` to every command on both hosts. A primary holds one engine's enrollment at a time and a secondary hosts one engine's channel: preparing or activating one engine is refused while the other engine's enrollment exists, and a secondary install or resume is refused while the other engine's channel files or retained evidence exist. Revoke and review the other engine first.

The primary owner runs `primary-prepare`, then gives only the returned public key to the secondary owner. The secondary owner saves that reviewed public key in a root-owned regular file and supplies a reviewed inspector executable. Example documentation addresses:

```sh
sudo dns-peer-enroll secondary-install \
  --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 \
  --catalog catalog-c000020a.celikpanel.invalid \
  --primary-public-key /root/primary.pub \
  --inspector /root/bind-peer-inspect
sudo dns-peer-enroll secondary-host-key
```

On a PowerDNS secondary, also give the `account` value stored for the catalog CONSUMER zone. The inspector compares it with PowerDNS's own catalog record and fails closed on a mismatch:

```sh
sudo dns-peer-enroll secondary-install --engine pdns \
  --primary-ip 192.0.2.10 --peer-ip 192.0.2.11 \
  --catalog catalog-c000020a.celikpanel.invalid \
  --catalog-account celikpanel-peer-catalog-v1 \
  --primary-public-key /root/primary.pub \
  --inspector /root/pdns-peer-inspect
sudo dns-peer-enroll secondary-host-key --engine pdns
```

PowerDNS files live under `/etc/pdns-peer-inspector`, `/etc/ssh/pdns-peer-inspector`, `/usr/local/libexec/celikpanel-pdns-peer-inspect*`, `/etc/sudoers.d/celikpanel-pdns-peer-inspector` and `/var/lib/pdns-peer-inspector`; the locked `celikpeer` account name is shared with BIND.

The primary owner independently checks the returned Ed25519 host-key digest, then uses `primary-activate` with the prepared credential ID, reviewed primary/peer/catalog and `--host-key-sha256`. No subcommand tests live authentication. The primary Agent makes the first authenticated read-only exchange only while reconciling an admitted pending deletion and reports its result on that operation. Both status commands report local configuration; only the exact DNS operation's authenticated observation can prove a deletion complete. An ordinary status poll does not restart a mutation.

If secondary installation was interrupted, run `secondary-status`, then explicitly run `secondary-resume` with the same flags as `secondary-install`. Exact staged policy, wrapper, binary and SSH configuration can be reused; a locked dedicated account can be reused only with its reserved primary group and no supplementary groups. The resume command accepts only the recorded original SSH file or its exact published replacement. It preserves owner changes and reports them for local review. An existing different authorized key is never replaced. If an authorized channel is now unverified, use `secondary-revoke` before reviewing it. Resume with no authorized key is an explicit authorization of the supplied public key, so do not resume an intentionally revoked enrollment unless reauthorization is intended.

`secondary-revoke` disables the fixed authorized key without stopping native DNS. For BIND it keeps owner SSH configuration and evidence unchanged. For PowerDNS it then restores the recorded original `sshd_config` and reloads OpenSSH only if the live file is exactly the recorded publication; an owner-edited file is left as is and the output names the `Include` line to remove. The original copy, receipt and inspector files are kept. The primary owner also runs `primary-revoke`. No automatic file deletion, account removal or coordinated rotation is provided. Native tested boundaries and remaining limitations are recorded in the source-tree report `deploy/e2e/dns-kill-matrix/evidence/owner-bind-resume-20260928/README.md`. The PowerDNS owner path has component tests only; no native PowerDNS owner enrollment has been run.